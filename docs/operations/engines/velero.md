# Engine: Velero

**Status:** Alpha
**Last Updated:** 2026-05-30

Velero is the operator's default engine for namespace-scoped backup and restore. The operator does not call the Velero binary, mount its config, or shell out to `velero backup create`; it materializes Velero's own CRDs (`velero.io/Backup` and `velero.io/Restore`), watches their status, and aggregates that status onto the matching `Operation`. Velero remains the controller; the operator is a well-behaved client.

This document covers when to pick Velero, how an `Operation` becomes a `velero.io/Backup` or `velero.io/Restore`, a complete worked example for each, and how Velero's status maps back onto `Operation.status.conditions`.

## When to use Velero

Pick Velero when:

- You want **namespace-scoped backup and restore** with a familiar, well-supported control plane.
- You want a **single backup artifact** that captures both Kubernetes objects (Secrets, ConfigMaps, ServiceAccounts, PVCs, the chart's own rendered objects) and the contents of attached persistent volumes — the latter either via the CSI snapshot path or via the Restic / Kopia file-system path that Velero supports.
- You want **restore into the same namespace, into a renamed namespace, or into a different cluster** (the last is out of scope for the operator's reconciler but is a property of the Velero artifact itself).
- You have a **`BackupStorageLocation`** configured on the cluster (S3-compatible object storage, Azure Blob, GCS, MinIO, or on-prem equivalents).

Prefer a different engine when:

- The work is a **single-volume snapshot** with no need to capture Kubernetes objects. Use the CSI snapshot engine instead — see [csi-snapshots-volsync.md](csi-snapshots-volsync.md).
- The work is a **continuous-replication** RPO requirement that Velero's periodic backups cannot meet. Use VolSync replication instead — see [csi-snapshots-volsync.md](csi-snapshots-volsync.md).
- The work is **an upgrade-time migration** the chart already encodes. Use the Helm Hook Job engine instead — see [helm-hooks.md](helm-hooks.md).

## How an `Operation` materializes a `velero.io/Backup`

When the reconciler admits an `Operation` of `type: Backup` with `engine: velero`, it:

1. Resolves the target `ApplicationInstance` and the namespace it lives in.
2. Constructs a `velero.io/Backup` in the same namespace as the operator's Velero install (commonly `velero` or `vworkspace-system`), with an `includedNamespaces` list of exactly one entry — the target's namespace.
3. Sets ownership labels (`app.vworkspace.io/managed-by: vworkspace-operator`, `app.vworkspace.io/cluster-id: <id>`, `ops.vworkspace.io/operation: <Operation UID>`) on the `Backup` so it can be reconciled idempotently and audited.
4. Sets the field manager to a deterministic, operator-specific value so server-side apply does not race with humans editing the resource.
5. Watches the `Backup` and rewrites `Operation.status.conditions` and `Operation.status.outputs` on every phase transition.

The operator does not modify the `Backup` after creation. Cancellation is implemented by deleting the `Backup` and updating the `Operation` accordingly; Velero respects deletion of in-progress backups.

## Worked example: backup

A complete Backup operation against an `ApplicationInstance` named `nextcloud-myteam` in the `org-myteam` namespace:

```yaml
apiVersion: ops.vworkspace.io/v1alpha1
kind: Operation
metadata:
  name: nextcloud-myteam-backup-2026-05-28
  namespace: org-myteam
  labels:
    app.vworkspace.io/managed-by: control-plane
    app.vworkspace.io/cluster-id: cluster-prod-1
spec:
  targetRef:
    apiVersion: apps.vworkspace.io/v1alpha1
    kind: ApplicationInstance
    name: nextcloud-myteam
  type: Backup
  engine: velero
  parameters:
    storageLocation: aws-primary
    snapshotVolumes: true
    csiSnapshotClassName: csi-rbd
    ttl: 720h
    includedResources:
      - "*"
    excludedResources:
      - events
      - events.events.k8s.io
```

The operator materializes the following `velero.io/Backup` (truncated to the fields that change with the request):

```yaml
apiVersion: velero.io/v1
kind: Backup
metadata:
  name: nextcloud-myteam-backup-2026-05-28
  namespace: velero
  labels:
    app.vworkspace.io/managed-by: vworkspace-operator
    app.vworkspace.io/cluster-id: cluster-prod-1
    ops.vworkspace.io/operation: 6b62...
    ops.vworkspace.io/target: nextcloud-myteam
spec:
  storageLocation: aws-primary
  snapshotVolumes: true
  csiSnapshotClassName: csi-rbd
  ttl: 720h
  includedNamespaces:
    - org-myteam
  includedResources:
    - "*"
  excludedResources:
    - events
    - events.events.k8s.io
  defaultVolumesToFsBackup: false
```

The corresponding `Operation.status` after Velero completes the backup:

```yaml
status:
  phase: Succeeded
  startedAt: "2026-05-28T10:00:00Z"
  finishedAt: "2026-05-28T10:07:13Z"
  conditions:
    - type: Accepted
      status: "True"
      reason: TemplateValidated
      lastTransitionTime: "2026-05-28T10:00:00Z"
    - type: Running
      status: "False"
      reason: VeleroBackupSucceeded
      lastTransitionTime: "2026-05-28T10:07:13Z"
    - type: Succeeded
      status: "True"
      reason: VeleroBackupCompleted
      message: "velero.io/Backup completed; 412 items, 18.4 GiB"
      lastTransitionTime: "2026-05-28T10:07:13Z"
  outputs:
    backupName: nextcloud-myteam-backup-2026-05-28
    backupItemCount: "412"
    backupSizeBytes: "19744974438"
    storageLocation: aws-primary
```

## Worked example: restore

A restore that recreates the application in a fresh namespace (`org-myteam-staging`) from the backup above:

```yaml
apiVersion: ops.vworkspace.io/v1alpha1
kind: Operation
metadata:
  name: nextcloud-myteam-restore-2026-05-28
  namespace: org-myteam-staging
spec:
  targetRef:
    apiVersion: apps.vworkspace.io/v1alpha1
    kind: ApplicationInstance
    name: nextcloud-myteam
  type: Restore
  engine: velero
  parameters:
    backupName: nextcloud-myteam-backup-2026-05-28
    namespaceMapping:
      org-myteam: org-myteam-staging
    restorePVs: true
    existingResourcePolicy: none
```

The materialized `velero.io/Restore`:

```yaml
apiVersion: velero.io/v1
kind: Restore
metadata:
  name: nextcloud-myteam-restore-2026-05-28
  namespace: velero
  labels:
    app.vworkspace.io/managed-by: vworkspace-operator
    app.vworkspace.io/cluster-id: cluster-prod-1
    ops.vworkspace.io/operation: 7c4f...
spec:
  backupName: nextcloud-myteam-backup-2026-05-28
  namespaceMapping:
    org-myteam: org-myteam-staging
  restorePVs: true
  existingResourcePolicy: none
  includedResources:
    - "*"
```

The Restore narrative — preconditions, validation steps, and pitfalls (storage class compatibility, secret references that point at the original namespace) — is covered in [../backups-and-restores.md](../backups-and-restores.md).

## Status mapping

Velero's `Backup.status.phase` and `Restore.status.phase` are the primary inputs. The mapping is:

| Velero phase                        | `Operation.status.phase` | Condition transitions                                                                                                                            |
|-------------------------------------|--------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------|
| `New`                               | `Pending`                | `Accepted=True/TemplateValidated`, `Running=False/Pending`.                                                                                       |
| `InProgress`                        | `Running`                | `Running=True/VeleroBackupInProgress` (or `VeleroRestoreInProgress`).                                                                              |
| `Completed`                         | `Succeeded`              | `Running=False/VeleroBackupSucceeded`, `Succeeded=True/VeleroBackupCompleted`. `outputs` populated with backup name and item/size counts.        |
| `PartiallyFailed`                   | `Succeeded` (degraded)   | `Succeeded=True/VeleroBackupPartiallyFailed`, `Degraded=True/PartialFailure`. The full list of warnings is mirrored into `outputs.warnings[]`.   |
| `Failed`                            | `Failed`                 | `Failed=True/VeleroBackupFailed`, `Running=False`. Velero's failure reason is mirrored into the condition message verbatim, truncated to 1 KiB.  |
| `FailedValidation`                  | `Failed`                 | `Accepted=False/VeleroBackupValidationFailed`. No child resource transitions beyond Velero's validation failure.                                  |
| `Deleting`                          | `Failed` (cancelled)     | `Cancelled=True/CancelledByUser` once the parent `Operation` was the cause; otherwise `Failed=True/ExternalDeletion`.                            |

The operator does not invent new phases for Velero work; everything it knows about progress comes from Velero's own status. The full condition reason vocabulary lives in [../../api/conditions.md](../../api/conditions.md).

## Practical notes

- The Velero install ships as part of the operator's bundled Helm chart. The chart sets up the `velero` namespace, the controller deployment, the CSI snapshot plugin, and an empty `BackupStorageLocation` that operators fill in during cluster bootstrap. See [../../install/cluster-bootstrap.md](../../install/cluster-bootstrap.md) for the step-by-step.
- Velero schedules (`velero.io/Schedule`) are not currently modeled as `Operation` resources. Recurring backups are expressed in Odoo as a recurring intent and materialized as discrete `Operation` instances on each fire; the cluster-side artifact is one-off Backups, never a Schedule. This may change in a future RFC.
- Backups that include PVs depend on a `VolumeSnapshotClass` being installed on the cluster. The operator validates the named class exists before admitting the `Operation`; missing classes surface as `Blocked=True/MissingVolumeSnapshotClass`.
- Velero's Restic / Kopia path is supported but not the default. Use the CSI path whenever the storage class supports snapshots; fall back to Restic / Kopia only when it does not.

## Related material

- [../operation-templates.md](../operation-templates.md) — How the Backup and Restore templates are defined.
- [../backups-and-restores.md](../backups-and-restores.md) — End-to-end backup and restore narrative.
- [../../api/operation.md](../../api/operation.md) — Full `Operation` field reference.
- [../../operate/backup-restore-runbook.md](../../operate/backup-restore-runbook.md) — Step-by-step runbook with verification.
