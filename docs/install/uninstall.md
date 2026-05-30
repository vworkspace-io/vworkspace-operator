# Uninstall

**Status:** Alpha
**Last Updated:** 2026-05-30

This document is the order of operations for cleanly removing `vworkspace-operator` from a cluster. The defining property of the procedure is that the operator's removal must not silently lose data the operator was responsible for protecting: PVs, Velero backups, application records in Odoo. The procedure pauses reconciliation, optionally triggers a final backup, deletes the `Cluster` CR (which lets Odoo revoke the credential), and only then `helm uninstall`s the bundle.

The reverse procedure — reinstalling on the same cluster with the same identity — is a special case of the [quickstart](quickstart.md): a fresh install plus a new registration token. The old cluster identity in Odoo is reused only if its credential is still valid; otherwise the cluster gets a new identity and the operator pulls jobs against the new one.

## Order of operations

The supported uninstall is the following five steps, in order. Skipping a step leaves the cluster in a state that is harder to clean up later.

### Step 1: pause reconciliation on `ApplicationInstance` resources

The operator and Flux are the reconcilers; if you uninstall them with `ApplicationInstance` resources still present, the next install will see those resources and immediately attempt to reconcile them — including against a catalog version that may have moved on. Pause first.

```
kubectl get applicationinstances -A -o json \
  | jq -r '.items[] | "\(.metadata.namespace) \(.metadata.name)"' \
  | while read ns name; do
      kubectl annotate applicationinstance -n "$ns" "$name" \
        apps.vworkspace.io/reconcile=disabled --overwrite
    done
```

The operator's reconciler observes the annotation and sets `ApplicationInstance.status.conditions[Suspended]=True/ReconcilePaused`. The underlying `HelmRelease` continues to exist; Flux still reconciles it (drift detection), but the operator does not push new changes from `ApplicationInstance.spec` and does not delete the `HelmRelease` if you delete the `ApplicationInstance`.

If you also want Flux to stop touching the `HelmRelease`:

```
kubectl get helmreleases -A -o name | xargs -I{} kubectl patch {} \
  --type=merge -p '{"spec":{"suspend":true}}'
```

This is the right move when you intend to migrate the cluster's applications somewhere else and want to be sure nothing changes while the migration runs.

### Step 2: optionally trigger a final backup

If you intend the uninstall to be permanent, take a final backup of every application you might ever want to restore:

```
kubectl get applicationinstances -A -o json \
  | jq -r '.items[] | "\(.metadata.namespace) \(.metadata.name)"' \
  | while read ns name; do
      cat <<EOF | kubectl apply -f -
apiVersion: ops.vworkspace.io/v1alpha1
kind: Operation
metadata:
  name: ${name}-final-backup
  namespace: ${ns}
spec:
  targetRef:
    apiVersion: apps.vworkspace.io/v1alpha1
    kind: ApplicationInstance
    name: ${name}
  type: Backup
  engine: velero
  parameters:
    storageLocation: aws-primary
    snapshotVolumes: true
    csiSnapshotClassName: csi-rbd
    ttl: 8760h
EOF
    done
```

`ttl: 8760h` is one year; pick a TTL that matches your retention policy. The final-backup phase finishes when every `Operation.status.phase` is `Succeeded` (or `Failed` for applications without backup capability).

The backups continue to live in the configured `BackupStorageLocation` after the operator is uninstalled. Velero is uninstalled along with the bundle in step 5; the artifacts in object storage are unaffected by the uninstall. Restoring later requires reinstalling Velero (or another compatible tool) and pointing it at the same `BackupStorageLocation`.

### Step 3: delete the `Cluster` CR

Deleting the `Cluster` CR is the signal the operator uses to revoke the credential server-side in Odoo. The operator's `Cluster` finalizer:

1. Marks the cluster `Decommissioning` in Odoo via `POST /api/agent/cluster/decommission`.
2. Awaits Odoo's acknowledgment (which invalidates the bootstrap credential and the keypair).
3. Removes the finalizer; the CR is garbage-collected.

```
kubectl delete cluster -n vworkspace-system cluster-prod-1
```

If the operator cannot reach Odoo (network gone, Odoo down), the finalizer blocks. Two ways out:

- Restore Odoo connectivity and let the finalizer drain naturally (recommended).
- Force-remove the finalizer (`kubectl patch cluster ... --type=merge -p '{"metadata":{"finalizers":null}}'`) and revoke the credential manually in Odoo. Use only when Odoo is permanently gone.

After the `Cluster` CR is gone, the operator stops pulling jobs from Odoo; the bootstrap credential Secret remains until step 5.

### Step 4: delete remaining CRs (optional)

If you intend to wipe the cluster, delete the remaining `ApplicationInstance` and `Operation` CRs before uninstalling the operator. This lets Flux delete the `HelmRelease`s and the chart's rendered resources cleanly:

```
kubectl get applicationinstances -A -o name | xargs -r kubectl delete --wait=true
kubectl get operations -A -o name | xargs -r kubectl delete --wait=true
```

If you keep the CRs and only uninstall the operator's bundle, the CRs will remain in etcd as orphaned objects until you either reinstall the operator or delete them manually. Flux will continue to reconcile the `HelmRelease`s as long as Flux is installed; once Flux is uninstalled in step 5, the chart's rendered resources persist as orphans (Kubernetes does not garbage-collect resources whose controller has gone away unless they have explicit owner references).

If you want to **keep the applications running** while removing the operator (a "decommissioning the management layer, not the apps" scenario), skip this step. The applications continue to run; the cluster simply no longer has an operator or Flux reconciling them.

### Step 5: `helm uninstall`

```
helm uninstall vworkspace-app-operator -n vworkspace-system
```

This removes the operator deployment, its ServiceAccount, the bundled controllers (Flux, cert-manager, external-secrets, Velero), and the chart-managed CRDs **if** the chart's `keepCrds=false` value is set (the default in v1alpha1 is `keepCrds=true`, so the CRDs stay; deleting CRDs cascades to every CR of those kinds and is destructive).

To remove the CRDs explicitly (after step 4 has cleaned the CRs):

```
kubectl delete crd applicationinstances.apps.vworkspace.io
kubectl delete crd operations.ops.vworkspace.io
kubectl delete crd clusters.ops.vworkspace.io
```

To remove the operator namespace:

```
kubectl delete namespace vworkspace-system
```

(`helm uninstall` leaves the namespace because `--create-namespace` was used at install time.)

## What persists after uninstall

The following persist by default. Removing each is a separate, deliberate action:

| Resource                                                       | Persists?                                                | How to fully purge                                                                                       |
|----------------------------------------------------------------|----------------------------------------------------------|-----------------------------------------------------------------------------------------------------------|
| Velero backups in `BackupStorageLocation` (object storage)     | Yes (object storage is not touched by the uninstall).    | Delete the bucket / prefix manually after confirming no further restore needs.                            |
| CSI snapshots in the storage driver's snapshot store           | Depends on the driver and the `VolumeSnapshotClass.deletionPolicy`. With `Retain`, they persist; with `Delete`, they are removed when their VolumeSnapshot CR is deleted (which happens in step 4). | Delete via the storage driver's tooling.                                                                  |
| PersistentVolumes from applications                              | Yes, unless their PVCs were deleted in step 4 and their `reclaimPolicy: Delete` triggered the driver. | If `reclaimPolicy: Retain` is set, the PVs need to be deleted manually; storage on the backend is not touched until the driver-specific delete runs. |
| The operator's bootstrap credential `Secret`                     | Yes, until you delete `vworkspace-system`.                | Deleting the namespace removes it.                                                                       |
| The cluster's identity record in Odoo                            | Yes, but the credential is invalidated.                  | Delete the cluster row in Odoo's Cluster Registry.                                                       |
| Audit events in Odoo                                              | Yes, as part of the audit log.                            | Standard Odoo data retention applies; the operator does not push deletes for past audit events.           |

The intent of these defaults is "do not lose data on uninstall". A fully-purge procedure is a separate, explicit decision an operator makes when they are certain.

## Reinstalling on the same cluster

The fastest path to a working operator on a cluster that previously had one:

1. `kubectl get cluster -n vworkspace-system` — confirm no `Cluster` CR remains. If one is leftover (e.g., from a forced finalizer removal), delete it.
2. Re-run [quickstart.md](quickstart.md) — `helm install`, generate a new registration token in Odoo, exchange it.
3. The new install will discover existing `ApplicationInstance` and `Operation` CRs (if you kept them) and resume reconciliation. The pause annotation from step 1 keeps them paused; remove it once you've validated the cluster's state matches Odoo's expectations.

## Related material

- [quickstart.md](quickstart.md) — Reinstall path.
- [../operate/troubleshooting.md](../operate/troubleshooting.md) — What to do when something is stuck.
- [../security/authentication.md](../security/authentication.md) — Credential revocation semantics on the control-plane side.
