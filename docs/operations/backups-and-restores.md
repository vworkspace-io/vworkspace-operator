# Backups and restores

**Status:** Alpha
**Last Updated:** 2026-05-30

This document is the end-to-end narrative for backing up and restoring an `ApplicationInstance`. It is not a reference: it walks through what an operator does, what the cluster does in response, where artifacts live, how retention works, how to restore into a different namespace, how to validate the restored application, and the pitfalls worth knowing about before they bite.

The engine-level reference for Velero is in [engines/velero.md](engines/velero.md); for CSI snapshots and VolSync, in [engines/csi-snapshots-volsync.md](engines/csi-snapshots-volsync.md). A worked runbook with `kubectl` commands is in [../operate/backup-restore-runbook.md](../operate/backup-restore-runbook.md).

## Requesting a backup

There are two ways to request a backup:

1. **From Odoo.** The operator (the person) opens the application's record in the vWorkspace control plane, clicks "Back up", and confirms. Odoo creates an `Operation` intent, the connectivity layer delivers it to the cluster (Pull, Push, or GitOps), and the operator materializes it as a CR. This is the path the AI assistant uses on the operator's behalf.
2. **From `kubectl` directly.** A platform engineer with the right RBAC can apply an `Operation` CR directly:

```yaml
apiVersion: ops.vworkspace.io/v1alpha1
kind: Operation
metadata:
  name: nextcloud-myteam-backup-2026-05-28
  namespace: org-myteam
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
```

The two paths produce identical results; the difference is who creates the CR. The audit trail in Odoo records who clicked the button (path 1) or who applied the manifest (path 2, via the operator's audit-event stream).

## What Velero does

When the operator admits the request, it creates a `velero.io/Backup` named after the `Operation` and in the Velero install's namespace. Velero then:

1. Walks the included namespace (`org-myteam`) and serializes every Kubernetes object Velero is configured to back up — Secrets, ConfigMaps, ServiceAccounts, Roles, RoleBindings, Deployments, StatefulSets, Services, Ingresses, PVCs, custom resources, and so on. Cluster-scoped resources referenced from the namespace (StorageClass, ClusterRole, ClusterRoleBinding) are captured only if Velero is configured to include them.
2. For each PVC in the namespace, takes a CSI snapshot via the named `VolumeSnapshotClass`. The snapshot is created by the CSI driver and is what Velero references as the "volume backup" entry in the archive.
3. Uploads the resulting tarball to the `BackupStorageLocation` named in the request (`aws-primary` in the example above). The object key is derived from the Backup's name and the location's bucket layout.
4. Writes its progress to `Backup.status` (`phase`, `progress.totalItems`, `progress.itemsBackedUp`). The operator mirrors that progress onto the `Operation.status`.

The cluster does not retain a local copy of the tarball; the `BackupStorageLocation` is the artifact's home. The CSI snapshots may or may not persist locally depending on the driver and the `VolumeSnapshotClass.deletionPolicy`.

## Where backups live

A backup artifact has three locations:

| Location                                  | What lives there                                                                                              |
|-------------------------------------------|---------------------------------------------------------------------------------------------------------------|
| `BackupStorageLocation` (S3, GCS, MinIO, …) | The Kubernetes object archive (tarball) and the Velero metadata for each Backup.                              |
| The CSI driver's snapshot store            | The volume snapshots, scoped to the driver and its underlying storage (Ceph pool, ZFS dataset, cloud snapshot service). |
| The cluster API                             | The `velero.io/Backup` resource and the operator's `Operation` resource; both records, not data.              |

The first two are the data. The third is the index that connects an `Operation` to the data so a human or a script can find it later.

## Retention

Retention is controlled by Velero's `ttl` field on each Backup. A Backup with `ttl: 720h` (30 days) is deleted automatically by Velero 30 days after creation; the deletion removes both the tarball in object storage and the CSI snapshot references the Backup owns. The operator does not enforce its own retention on top of Velero; the Velero TTL is the contract.

Recurring backups are not modeled as a CRD in the operator's API today. The vWorkspace Server control plane drives a recurring schedule by emitting one `Operation` per fire (every hour, every night, every Sunday). Each `Operation` has its own `ttl`. The control plane catalog publishes recommended defaults per application class — for example, "hourly backups retained 24 hours; daily backups retained 30 days; weekly backups retained 12 weeks" — which an organization can override. A future RFC may introduce a `Schedule` CRD; for now, the operator stays simple.

## Restoring into the same namespace

The simplest restore replaces the contents of a namespace from a Backup created earlier:

```yaml
apiVersion: ops.vworkspace.io/v1alpha1
kind: Operation
metadata:
  name: nextcloud-myteam-restore-2026-05-28
  namespace: org-myteam
spec:
  targetRef:
    apiVersion: apps.vworkspace.io/v1alpha1
    kind: ApplicationInstance
    name: nextcloud-myteam
  type: Restore
  engine: velero
  parameters:
    backupName: nextcloud-myteam-backup-2026-05-28
    restorePVs: true
    existingResourcePolicy: update
```

The operator pauses reconciliation on the target `ApplicationInstance` (sets the `Suspended` condition) before the restore so Flux does not race the Restore by reconciling the chart while Velero is rewriting objects. After Velero reports `Completed`, the operator resumes reconciliation. The chart's own resources are now back at the state captured in the backup; the running application has been restarted by virtue of its workloads being recreated.

This restore is destructive within its scope: every object Velero captured is replaced. Operations that should not be replaced (a `Secret` an operator updated post-backup, an ingress IP allocated by the cluster) need either `existingResourcePolicy: none` (do not replace existing) or explicit exclusion in `parameters.excludedResources`.

## Restoring into a different namespace

Restoring into a different namespace is the right pattern for validating a backup ("does this artifact actually contain a working application?") and for "give me a copy of production data in staging" workflows.

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

Velero rewrites object namespaces in place during the restore. PVCs in the source namespace land in `org-myteam-staging`; the CSI driver provisions new PVs from the original snapshots. Services keep their names; the ingress host (if present) still points at `files.myteam.example.com`, which is rarely what you want for a staging restore — see "Pitfalls" below.

For the validation use case, the target namespace should be freshly created (`kubectl create namespace org-myteam-staging`) and labelled `app.vworkspace.io/managed-by: vworkspace` so the operator is willing to operate within it.

## Validating a restore

A successful Velero phase is necessary but not sufficient. The validation checklist:

1. `Operation.status.conditions[Succeeded]=True` and `outputs.warnings` is empty (or every warning is understood).
2. The restored `ApplicationInstance` reconciles cleanly — `Ready=True`, `Reconciling=False` — and the underlying `HelmRelease` reports `Ready=True`.
3. The application's URL is reachable. If the chart provisions an ingress with cert-manager, the certificate must be re-issued for the staging hostname.
4. Application-level checks: log in to the application, open a file, send a message, view a recent record. The operator does not run these for you; the runbook in [../operate/backup-restore-runbook.md](../operate/backup-restore-runbook.md) lists the commands and check-points.
5. If the restore is a disaster-recovery test (production → staging) and the goal is to flip production to staging, do not flip until after the validation passes.

If any of these fail, the artifact is suspect; trace the failure back to the Backup that produced it and root-cause before relying on that artifact.

## Pitfalls

The following bite often enough to merit calling out:

- **Storage class compatibility.** PVs in the Backup carry the StorageClass name from the source cluster. Restoring into a cluster (or a namespace) where that StorageClass does not exist will fail to bind. Either install the same StorageClass on the target or pass a `parameters.storageClassMapping` that translates source to target.
- **Secret references after restore.** Backups capture the Secret objects, but applications often reference secrets via `secretRef` in chart values — and those references may include namespace-qualified DNS names that the restore has not updated. After a cross-namespace restore, re-render the chart values (most cleanly by re-running the `ApplicationInstance` reconcile on the restored namespace) and verify the rendered Secrets match the chart's expectations.
- **External-secrets.** If chart values reference secrets via `external-secrets`, the `ExternalSecret` is captured but its target Secret may need to be re-synced. Force a sync (`kubectl annotate externalsecret -n org-myteam-staging <name> force-sync=$(date +%s)`) after a cross-namespace restore.
- **Ingress hostnames.** The restored Ingress points at the original hostname. For staging restores, edit `ApplicationInstance.spec.values.ingress.host` and let the chart reconcile a new Ingress; do not leave production and staging both claiming the same host.
- **cert-manager.** Certificates and CertificateRequests are captured, but a freshly-restored cluster may not have the ACME account or DNS-01 credentials needed to re-issue. Verify cert-manager's prerequisites before relying on the restored certificate.
- **Image pull credentials.** Pull secrets in the source namespace are captured. Pull secrets referenced from a different namespace, or pulled via a service-account-based mechanism, may not survive the restore. Confirm `imagePullSecrets` on the restored `ServiceAccount`s.
- **CSI driver readiness.** A Backup's CSI snapshots are only restorable on a cluster where the same CSI driver is installed and configured. Cross-cluster restores rely on object storage (the tarball portion) but also need the snapshot data, which is driver-specific. For DR scenarios, prefer VolSync replication — see [engines/csi-snapshots-volsync.md](engines/csi-snapshots-volsync.md).
- **Velero `BackupStorageLocation` availability.** A backup is only restorable if its storage location is reachable. Test cross-cluster restores by occasionally restoring from a backup taken on a different cluster; do not assume reachability until you have validated it.

## Related material

- [engines/velero.md](engines/velero.md) — Velero engine reference and the materialized CRDs.
- [engines/csi-snapshots-volsync.md](engines/csi-snapshots-volsync.md) — Snapshot-level alternative and DR-friendly replication.
- [../operate/backup-restore-runbook.md](../operate/backup-restore-runbook.md) — Step-by-step runbook with concrete commands.
- [../security/secrets-handling.md](../security/secrets-handling.md) — How secrets in chart values are handled across backup and restore.
