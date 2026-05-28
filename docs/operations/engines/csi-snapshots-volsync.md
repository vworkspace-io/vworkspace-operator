# Engine: CSI snapshots and VolSync

**Status:** Alpha
**Last Updated:** 2026-05-28

For storage-centric work — PV-level snapshots and PV-level replication — the operator integrates with the CSI snapshot controller (snapshots) and VolSync (replication). The two engines share this chapter because they are storage-level primitives: neither captures Kubernetes objects, both rely on the underlying StorageClass and a CSI driver, and the choice between them is primarily a function of the required RPO/RTO targets.

This document covers when to pick CSI snapshots versus VolSync, how an `Operation` materializes the relevant CRDs, a worked example for each, status mapping, and the RPO/RTO trade-offs.

## When to use each engine

| Question                                                            | CSI snapshot          | VolSync                         |
|---------------------------------------------------------------------|------------------------|---------------------------------|
| Capture a single PV at a point in time?                              | Yes (engine: snapshot)| Indirectly (sync at interval)   |
| Replicate a PV continuously to another cluster or storage?           | No                    | Yes (engine: volsync)            |
| Capture Kubernetes objects (Secrets, ConfigMaps, etc.) along with PV?| No (use Velero)       | No (use Velero)                  |
| Crash-consistent vs application-consistent?                          | Crash; app-consistent via quiesce hook | Async replication; consistency follows the engine (Restic, Kopia, rsync, rclone) |
| Achievable RPO                                                       | Equals the snapshot cadence | Minutes (Restic), seconds (rclone over fast storage) |
| Achievable RTO                                                       | Reattach time of the cloned PV | Restore time of the replicated snapshot / repo |
| Storage requirements                                                 | CSI driver with snapshot support, `VolumeSnapshotClass` installed | CSI driver with `VolumeSnapshotClass` (most VolSync flows snapshot first), plus a remote backend |
| Cross-cluster                                                         | No (snapshots live where the PV lives) | Yes (the entire point of VolSync) |

In short: CSI snapshots answer "freeze this volume now and let me roll back to it"; VolSync answers "keep this volume mirrored to somewhere else with a continuous lag I can tolerate".

The Velero engine ([velero.md](velero.md)) is complementary: Velero captures Kubernetes objects and can drive CSI snapshots underneath. If the work is "back up an application", use Velero; if the work is "I want a volume-level snapshot I can mount as a sibling PVC", use the CSI snapshot engine; if the work is "this PV must be continuously mirrored to a DR location", use VolSync.

## How an `Operation` materializes a `VolumeSnapshot`

When the reconciler admits an `Operation` of `engine: snapshot`, it:

1. Resolves the target `ApplicationInstance` and the PVCs it owns (declared via `spec.persistence` in the operation template, typically a single named PVC such as `data-<release>`).
2. Constructs a `snapshot.storage.k8s.io/VolumeSnapshot` in the target's namespace, with `spec.source.persistentVolumeClaimName` set to the named PVC and `spec.volumeSnapshotClassName` set to the requested class.
3. Sets ownership labels (`app.vworkspace.io/managed-by`, `app.vworkspace.io/cluster-id`, `ops.vworkspace.io/operation`) on the `VolumeSnapshot`.
4. Watches the `VolumeSnapshot.status.readyToUse` and `VolumeSnapshot.status.restoreSize` and rewrites `Operation.status` on each transition.

When a quiesce hook is advertised on the `ApplicationInstance` (`ops.vworkspace.io/quiesce: exec`), the operator can optionally invoke it before creating the `VolumeSnapshot` and reverse it after `readyToUse=true`. The hook is opt-in and is described as part of the operation template's `parameters.quiesce` block.

## Worked example: CSI snapshot

A `VolumeSnapshot` of the Nextcloud data PVC, taken as a pre-migration safety net (also visible inline in the Argo Workflow example in [argo-workflows.md](argo-workflows.md)):

### The `Operation`

```yaml
apiVersion: ops.vworkspace.io/v1alpha1
kind: Operation
metadata:
  name: nextcloud-myteam-snapshot-2026-05-28
  namespace: org-myteam
spec:
  targetRef:
    apiVersion: apps.vworkspace.io/v1alpha1
    kind: ApplicationInstance
    name: nextcloud-myteam
  type: Backup
  engine: snapshot
  parameters:
    pvc: data-nextcloud-myteam
    volumeSnapshotClassName: csi-rbd
    quiesce:
      enabled: true
      timeoutSeconds: 60
```

### The materialized `VolumeSnapshot`

```yaml
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: nextcloud-myteam-snapshot-2026-05-28
  namespace: org-myteam
  labels:
    app.vworkspace.io/managed-by: vworkspace-operator
    app.vworkspace.io/cluster-id: cluster-prod-1
    ops.vworkspace.io/operation: 2c4d...
spec:
  volumeSnapshotClassName: csi-rbd
  source:
    persistentVolumeClaimName: data-nextcloud-myteam
```

### The `Operation.status` after completion

```yaml
status:
  phase: Succeeded
  startedAt: "2026-05-28T11:00:00Z"
  finishedAt: "2026-05-28T11:00:14Z"
  conditions:
    - type: Accepted
      status: "True"
      reason: TemplateValidated
    - type: Succeeded
      status: "True"
      reason: VolumeSnapshotReadyToUse
      message: "VolumeSnapshot is ReadyToUse; restoreSize 250Gi"
  outputs:
    volumeSnapshotName: nextcloud-myteam-snapshot-2026-05-28
    volumeSnapshotContentName: snapcontent-...
    restoreSize: "250Gi"
```

## Status mapping (CSI snapshot)

| `VolumeSnapshot.status`                          | `Operation.status.phase` | Conditions                                                                                              |
|--------------------------------------------------|--------------------------|----------------------------------------------------------------------------------------------------------|
| no `status` yet                                  | `Pending`                | `Accepted=True/TemplateValidated`, `Running=False/Pending`.                                              |
| `readyToUse: false`, `error` not set             | `Running`                | `Running=True/VolumeSnapshotInProgress`.                                                                  |
| `readyToUse: true`                               | `Succeeded`              | `Succeeded=True/VolumeSnapshotReadyToUse`. `outputs.volumeSnapshotName`, `outputs.restoreSize` populated. |
| `error.message` set                              | `Failed`                 | `Failed=True/VolumeSnapshotFailed`. Message is mirrored verbatim.                                         |
| Source PVC missing                                | `Failed`                 | `Failed=True/SourcePvcNotFound` (caught at admission where possible).                                     |

## VolSync: when and how

VolSync is the right tool when "have a copy elsewhere" is a continuous requirement, not a point-in-time event. Concretely:

- Replicating a PV to a remote object store (Restic or Kopia repository) on a schedule.
- Replicating a PV to a different cluster's PVC (RsyncTLS or rclone-based) so a warm-standby application can be brought up quickly.
- Restoring an application by pointing a fresh PVC at a VolSync `ReplicationDestination` that already has the data.

The operator integrates VolSync by materializing `volsync.backube/ReplicationSource` and `volsync.backube/ReplicationDestination` resources on the relevant clusters. Both sides are expressed as `Operation` resources in the operator's own model:

### `ReplicationSource` example (origin cluster)

```yaml
apiVersion: ops.vworkspace.io/v1alpha1
kind: Operation
metadata:
  name: nextcloud-myteam-replicate-2026-05-28
  namespace: org-myteam
spec:
  targetRef:
    apiVersion: apps.vworkspace.io/v1alpha1
    kind: ApplicationInstance
    name: nextcloud-myteam
  type: Backup
  engine: volsync
  parameters:
    direction: source
    pvc: data-nextcloud-myteam
    schedule: "*/15 * * * *"
    repository:
      secretName: restic-myteam
      type: restic
    retain:
      hourly: 24
      daily: 7
      weekly: 4
```

The materialized `ReplicationSource`:

```yaml
apiVersion: volsync.backube/v1alpha1
kind: ReplicationSource
metadata:
  name: nextcloud-myteam-replicate-2026-05-28
  namespace: org-myteam
  labels:
    app.vworkspace.io/managed-by: vworkspace-operator
    app.vworkspace.io/cluster-id: cluster-prod-1
    ops.vworkspace.io/operation: 3e5f...
spec:
  sourcePVC: data-nextcloud-myteam
  trigger:
    schedule: "*/15 * * * *"
  restic:
    repository: restic-myteam
    copyMethod: Snapshot
    volumeSnapshotClassName: csi-rbd
    retain:
      hourly: 24
      daily: 7
      weekly: 4
```

The "Snapshot" copy method has VolSync use a CSI snapshot under the hood, so the running application keeps writing while the snapshot is replicated. That is the single most important reason to enable a CSI snapshot class on the cluster even when the headline use case is replication, not snapshot-as-product.

## Status mapping (VolSync)

The operator follows `ReplicationSource.status.lastSyncTime`, `lastSyncDuration`, and `conditions[]`:

| `ReplicationSource.status`                                  | `Operation.status.phase`     | Conditions                                                                                                |
|-------------------------------------------------------------|------------------------------|----------------------------------------------------------------------------------------------------------|
| First sync pending                                          | `Running`                    | `Running=True/VolSyncFirstSyncInProgress`.                                                                |
| `Synchronizing=True`                                        | `Running`                    | `Running=True/VolSyncSynchronizing`.                                                                       |
| Recent successful sync (`lastSyncTime` within `schedule`)   | `Succeeded` (recurring)      | `Running=True/VolSyncIdle`, `Succeeded=True/VolSyncLastSyncSucceeded`. `outputs.lastSyncTime` populated.   |
| `conditions[Reconciled].status=False`                       | `Degraded`                   | `Degraded=True/VolSyncDegraded`. Message mirrors VolSync's reason.                                         |
| Suspended                                                    | `Running` (suspended)       | `Blocked=True/AwaitingResume`.                                                                             |

Because a `ReplicationSource` is a recurring resource, the parent `Operation` does not finalize after a single sync. The operator treats it as a long-lived recurring operation; the `Succeeded` condition reflects "the most recent sync window completed".

## RPO and RTO

RPO and RTO are properties of the storage layer and the cadence, not the operator. The framework below is the language we use to reason about a given application's data-protection posture.

| Posture                                            | Mechanism                                                                       | Typical RPO         | Typical RTO                            |
|----------------------------------------------------|---------------------------------------------------------------------------------|---------------------|----------------------------------------|
| Periodic Velero backup                              | `velero.io/Backup` on a schedule with CSI snapshots                              | Equal to schedule (1h–24h common) | Restore time of the Backup (minutes for object restore; longer for PV restore depending on driver) |
| Manual snapshot before risky change                 | `Operation` `engine: snapshot` ad hoc                                            | Equal to "right before the change" | Reattach the snapshot as a PVC; depends on driver |
| Recurring VolSync to remote repo                    | `ReplicationSource` with `schedule`                                              | Minutes (lower bound is the CSI snapshot rate) | Time to restore a snapshot from the remote repo |
| Continuous VolSync RsyncTLS to a warm-standby PVC   | `ReplicationSource` + `ReplicationDestination`                                   | Seconds–single-digit minutes | Time to point a fresh application at the destination PVC |

The operator does not promise an RPO or RTO; the choice of engine and parameters does. The Odoo catalog publishes recommended defaults per application (Nextcloud: hourly Velero + nightly VolSync; OnlyOffice: nightly VolSync only; WordPress: hourly VolSync). Organizations can override the defaults per `ApplicationInstance`.

## Practical notes

- The `VolumeSnapshotClass` must exist and be marked as the default for backups (the operator picks a class explicitly via `parameters.volumeSnapshotClassName`, but the cluster bootstrap doc encourages a default class). Missing classes are caught at admission with `Blocked=True/MissingVolumeSnapshotClass`.
- VolSync is **optional** on the cluster. `Operation` requests with `engine: volsync` are admission-rejected if VolSync is not installed.
- Restoring from a CSI snapshot is a `type: Restore`, `engine: snapshot` operation; it materializes a new PVC with `dataSource` pointing at the snapshot and waits for the new PVC to bind. The application then needs to be reconfigured to use the new PVC, which is typically done by editing the `ApplicationInstance.spec.values` accordingly.
- Snapshot lifecycle (TTL, garbage collection) is enforced by the CSI driver and the `VolumeSnapshotClass.deletionPolicy`. The operator does not delete snapshots on the user's behalf unless an explicit `type: Delete` `engine: snapshot` operation requests it.

## Related material

- [velero.md](velero.md) — Velero engine; the right tool when Kubernetes objects must travel alongside the PV.
- [../operation-templates.md](../operation-templates.md) — How `snapshot.snapshot` and `volsync.volsync` templates are defined.
- [../backups-and-restores.md](../backups-and-restores.md) — End-to-end backup-and-restore narrative.
- [../../install/prerequisites.md](../../install/prerequisites.md) — Cluster prerequisites for snapshots and replication.
