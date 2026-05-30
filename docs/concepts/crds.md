# CRDs at a glance

**Status:** Alpha — both CRDs are at `v1alpha1`.
**Last Updated:** 2026-05-30

The operator owns exactly two CRDs. Everything Odoo asks the cluster to do, in every connectivity mode, is expressed as one of these two resources. The complete spec/status reference for each lives next door:

- [../api/application-instance.md](../api/application-instance.md) — full reference for `ApplicationInstance`.
- [../api/operation.md](../api/operation.md) — full reference for `Operation`.

This page is the guided tour, with the smallest examples that make sense.

## `ApplicationInstance` (`apps.vworkspace.io/v1alpha1`)

`ApplicationInstance` represents the intent "this application should exist in this namespace with these chart parameters", without encoding any per-application internals. The operator materializes a `HelmRelease` (and, where needed, the upstream `HelmRepository` or `OCIRepository`) from the `spec` and aggregates the resulting `HelmRelease` conditions back into `status.conditions`.

Smaller-than-real example:

```yaml
apiVersion: apps.vworkspace.io/v1alpha1
kind: ApplicationInstance
metadata:
  name: nextcloud-myteam
  namespace: org-myteam
  labels:
    app.vworkspace.io/managed-by: control-plane
    app.vworkspace.io/cluster-id: cluster-prod-1
spec:
  appRef:
    catalogId: nextcloud
  chart:
    sourceType: oci
    url: oci://registry.example.com/charts
    name: nextcloud
    version: "6.6.0"
  release:
    name: nextcloud-myteam
    namespace: org-myteam
  values:
    source: inline
    inline:
      ingress:
        enabled: true
        host: files.myteam.example.com
```

What the operator reads:

- `appRef.catalogId` ties the instance to a curated control plane catalog entry that carries capability metadata (which day-2 operations apply, see [day-2-operations.md](day-2-operations.md)).
- `chart.sourceType` is one of the chart sources the admission webhook allows (`oci` or `helm`); `url`, `name`, and `version` identify the artifact.
- `release.name` and `release.namespace` go straight into the materialized `HelmRelease`.
- `values.source` is `inline`, `secretRef`, or `configMapRef`. The latter two let chart values reference data the operator does not itself need to read.

What the operator writes back in `status` (abbreviated):

- `conditions[]` — `Ready`, `Reconciling`, `Degraded`, `Suspended`, `Blocked`, `Deleting`. The standard Kubernetes condition vocabulary. See [../api/conditions.md](../api/conditions.md) for reasons.
- `helmReleaseRef` — the name of the materialized `HelmRelease`.
- `lastAppliedChart` — the chart coordinates the operator most recently materialized.
- `endpoints[]` — derived URLs and hosts, populated when the chart's ingress and service outputs are visible.

The complete field reference is in [../api/application-instance.md](../api/application-instance.md).

## `Operation` (`ops.vworkspace.io/v1alpha1`)

`Operation` is a single generic CRD for day-2 actions. It carries a `type` (the verb: `Backup`, `Restore`, `Upgrade`, `Migration`, `RunCommand`, `Runbook`), an `engine` (the executor: `velero`, `workflow`, `job`, `helm`, `volsync`, `helmHookJob`), a reference to the target `ApplicationInstance`, an open `parameters` object, and optional `approvals`. The operator picks the matching engine and materializes whatever resource that engine reads.

Smaller-than-real example:

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
    retention: "30d"
    snapshotClassName: "csi-rbd"
```

What the operator does with this resource:

- Validates the request against the admission webhook (allowed `type` per namespace, no conflicting concurrent `Operation`, approvals satisfied if the catalog entry requires them).
- Creates a `velero.io/Backup` (because `engine` is `velero`), wiring `parameters` into Velero's spec.
- Watches the Velero resource and aggregates its conditions into `Operation.status.conditions` (`Accepted`, `Running`, `Succeeded`, `Failed`, `Cancelled`, `Blocked` — see [../api/conditions.md](../api/conditions.md)).
- Populates `status.outputs.backupName` so Odoo and the audit log can reference the Velero resource by name.

The complete field reference is in [../api/operation.md](../api/operation.md).

## Two CRDs, not twenty

It is tempting to model every verb as its own CRD: `Backup`, `Restore`, `Migration`, `Upgrade`, `Rollback`, `Runbook`. Doing so gives clearer per-verb RBAC granularity and lets each CRD evolve its own schema independently. We chose the single generic `Operation` CRD anyway, for three reasons.

1. **A small API surface is the product.** The operator promises "two CRDs and a status vocabulary." Multiplying CRDs would erode the property that you can read the API reference end-to-end in an hour and understand the whole system. The complexity does not go away — it migrates into the human's head, then into the AI's prompts, then into the documentation.
2. **The verbs share most of their machinery.** Every day-2 operation needs the same target reference, the same approvals story, the same conditions, the same output set, the same admission rules. Splitting them into N CRDs duplicates that machinery N times. Keeping them in one CRD with a discriminating `type` field keeps the machinery in one place.
3. **RBAC granularity is preserved at the right layer.** A validating admission webhook in the operator gates which `type` values are allowed in which namespaces, and which require approvals. This is more flexible than per-verb CRDs (it can be changed without an API migration) and gives the same end result for permission management.

The same reasoning leads to the operator owning **only** `ApplicationInstance` and `Operation`. The downstream Kubernetes resources (`HelmRelease`, `Backup`, `Workflow`, ...) are owned by the controllers that already speak those vocabularies; the operator does not invent its own wrappers.

For the day-2 patterns these two CRDs unlock, see [day-2-operations.md](day-2-operations.md). For how each CRD is reconciled, see [reconciliation-model.md](reconciliation-model.md).
