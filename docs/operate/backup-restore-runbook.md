# Backup and restore runbook

**Status:** Alpha
**Last Updated:** 2026-05-30

This is a worked example that walks an operator through requesting a backup of an `ApplicationInstance`, watching the result, verifying the Velero artifact, restoring into a fresh namespace, and validating the restore. The narrative is in [../operations/backups-and-restores.md](../operations/backups-and-restores.md); this document is the concrete `kubectl` and verification path.

The example uses `nextcloud-myteam` in the `org-myteam` namespace, the same target used elsewhere in the docs. Substitute names freely.

## Preconditions

Before running the procedure, confirm:

```
# Cluster is connected, controllers are healthy
kubectl get cluster -n vworkspace-system -o jsonpath='{.items[0].status.conditions[?(@.type=="Connected")].status}'
# expected: True

# Velero is running and has a BackupStorageLocation
kubectl get backupstoragelocation -n velero
kubectl get backupstoragelocation -n velero -o jsonpath='{.items[0].status.phase}'
# expected: Available

# The target ApplicationInstance is Ready and declares Velero capability
kubectl get applicationinstance -n org-myteam nextcloud-myteam \
  -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}{"\n"}{.metadata.annotations.ops\.vworkspace\.io/backup}'
# expected:
# True
# velero

# The CSI snapshot class exists if PV snapshots are requested
kubectl get volumesnapshotclass csi-rbd
```

If any check fails, see [troubleshooting.md](troubleshooting.md) first.

## Part 1: request a backup

Create the `Operation` CR.

```
cat <<'EOF' | kubectl apply -f -
apiVersion: ops.vworkspace.io/v1alpha1
kind: Operation
metadata:
  name: nextcloud-myteam-backup-2026-05-28
  namespace: org-myteam
  labels:
    app.vworkspace.io/managed-by: human
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
EOF
```

Watch the operation as it progresses through `Pending → Running → Succeeded`:

```
kubectl get operation -n org-myteam nextcloud-myteam-backup-2026-05-28 \
  -o jsonpath='{.status.phase}{"\n"}'
# Pending

kubectl get operation -n org-myteam nextcloud-myteam-backup-2026-05-28 \
  -w -o jsonpath='{.status.phase} {.status.conditions[?(@.type=="Running")].reason}{"\n"}'
# Running VeleroBackupInProgress
# Succeeded VeleroBackupSucceeded
```

The `Succeeded` phase is the signal the operator has accepted Velero's terminal state.

## Part 2: verify the Velero `Backup`

```
# The Backup name matches the Operation name
kubectl get backup -n velero nextcloud-myteam-backup-2026-05-28

# Velero's authoritative status
kubectl get backup -n velero nextcloud-myteam-backup-2026-05-28 -o yaml \
  | yq '.status'
```

Expected:

```yaml
status:
  phase: Completed
  startTimestamp: "2026-05-28T10:00:00Z"
  completionTimestamp: "2026-05-28T10:07:13Z"
  progress:
    totalItems: 412
    itemsBackedUp: 412
  warnings: 0
  errors: 0
```

The same data is mirrored onto `Operation.status.outputs`:

```
kubectl get operation -n org-myteam nextcloud-myteam-backup-2026-05-28 \
  -o jsonpath='{.status.outputs}{"\n"}'
# {"backupName":"nextcloud-myteam-backup-2026-05-28","backupItemCount":"412","backupSizeBytes":"19744974438","storageLocation":"aws-primary"}
```

The Backup's contents in object storage are now the artifact you would restore from. Velero exposes them via `velero backup logs ...` (if the Velero CLI is installed); the same logs are available indirectly:

```
kubectl logs -n velero deploy/velero \
  | grep nextcloud-myteam-backup-2026-05-28 | tail -50
```

## Part 3: restore into a fresh namespace

Create a new namespace, mark it managed, and request the restore.

```
# Create the target namespace
kubectl create namespace org-myteam-staging
kubectl label namespace org-myteam-staging app.vworkspace.io/managed-by=vworkspace

# Apply the Restore Operation
cat <<'EOF' | kubectl apply -f -
apiVersion: ops.vworkspace.io/v1alpha1
kind: Operation
metadata:
  name: nextcloud-myteam-restore-2026-05-28
  namespace: org-myteam-staging
  labels:
    app.vworkspace.io/managed-by: human
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
EOF
```

Watch:

```
kubectl get operation -n org-myteam-staging nextcloud-myteam-restore-2026-05-28 \
  -w -o jsonpath='{.status.phase} {.status.conditions[?(@.type=="Running")].reason}{"\n"}'
# Running VeleroRestoreInProgress
# Succeeded VeleroRestoreCompleted
```

The Velero Restore object:

```
kubectl get restore -n velero nextcloud-myteam-restore-2026-05-28 -o yaml | yq '.status'
```

Expected:

```yaml
status:
  phase: Completed
  startTimestamp: "2026-05-28T10:30:00Z"
  completionTimestamp: "2026-05-28T10:35:42Z"
  warnings: 1
  errors: 0
  progress:
    totalItems: 412
    itemsRestored: 412
```

A `warnings` count above zero is not necessarily a failure; Velero often warns on cluster-scoped resources that already exist (StorageClass, etc.). Read `kubectl describe restore -n velero <name>` to see what it warned about.

## Part 4: validate the restored application

The restore created the chart's resources in `org-myteam-staging`. Two checks: the operator's view (was the `ApplicationInstance` recreated and is it `Ready`?) and the application's view (does it actually work?).

### Operator's view

```
# The ApplicationInstance landed in the staging namespace
kubectl get applicationinstance -n org-myteam-staging
# nextcloud-myteam   Ready=True ...

kubectl get helmrelease -n org-myteam-staging
# nextcloud-myteam   Ready=True ...

kubectl get pods -n org-myteam-staging
# All pods Running
```

If `ApplicationInstance.status.conditions[Ready]=False`, the restored chart did not converge. The most common reason is a chart value referencing the original namespace (a `secretRef` qualified by namespace, or an internal DNS name with the old namespace). Fix `ApplicationInstance.spec.values` and let the chart re-reconcile.

### Application's view

```
# The ingress hostname
kubectl get ingress -n org-myteam-staging
# nextcloud-myteam   files.myteam.example.com

# If the host should be different for staging, update spec.values:
kubectl patch applicationinstance -n org-myteam-staging nextcloud-myteam --type=merge \
  -p '{"spec":{"values":{"inline":{"ingress":{"host":"files-staging.myteam.example.com"}}}}}'

# Visit the URL (from a host that can reach it). Log in, open a file, verify the
# expected user accounts are present, sample directory listings against the
# expected file tree.
curl -sI https://files-staging.myteam.example.com | head -1
# HTTP/2 200
```

For Nextcloud specifically, you might run a small `occ` check:

```
kubectl exec -n org-myteam-staging deploy/nextcloud-myteam -- php occ status
kubectl exec -n org-myteam-staging deploy/nextcloud-myteam -- php occ user:list | head
```

For other charts, the validation step is the chart's own "is this working?" probe — the chart's documentation will name it.

## Part 5: clean up the staging restore (if temporary)

If the staging namespace was created for validation only, tear it down:

```
# Pause reconcile on the staging ApplicationInstance so the operator does not
# try to recreate resources as you delete them
kubectl annotate applicationinstance -n org-myteam-staging nextcloud-myteam \
  apps.vworkspace.io/reconcile=disabled --overwrite

# Delete the ApplicationInstance and the namespace
kubectl delete applicationinstance -n org-myteam-staging nextcloud-myteam
kubectl delete namespace org-myteam-staging
```

The original `org-myteam`/`nextcloud-myteam` is unaffected.

## Common pitfalls (with the matching symptoms)

These are abbreviated; the long-form discussion is in [../operations/backups-and-restores.md](../operations/backups-and-restores.md).

- **`Restore` warns about missing StorageClass.** The source cluster used a StorageClass the target cluster does not have. Pass `parameters.storageClassMapping` or install the StorageClass.
- **`Restore` succeeds but the application's Pods are pending.** The PVCs are still binding because the CSI driver is re-provisioning from snapshots. `kubectl describe pvc` will show the binding event.
- **`Restore` succeeds but TLS certificates are not re-issued.** cert-manager needs to see the new Ingress and request a fresh cert; this can take a minute or two. If it never happens, check `kubectl get certificaterequest -n org-myteam-staging`.
- **Restored application logs in with the wrong configuration.** The chart's rendered Secret may reference the original namespace's external-secrets target. Re-render values, force a sync, or set `spec.values.inline.ingress.host` to the staging hostname so the chart's reconcile produces a fresh config.

## Related material

- [../operations/backups-and-restores.md](../operations/backups-and-restores.md) — Narrative for the same procedure with more context.
- [../operations/engines/velero.md](../operations/engines/velero.md) — Velero engine reference.
- [troubleshooting.md](troubleshooting.md) — When something other than the happy path happens.
