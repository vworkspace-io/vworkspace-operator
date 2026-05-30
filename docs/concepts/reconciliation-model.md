# Reconciliation model

**Status:** Alpha
**Last Updated:** 2026-05-30

This page describes how the operator turns its two CRDs into actual work. The full spec/status reference for each CRD is in [../api/application-instance.md](../api/application-instance.md) and [../api/operation.md](../api/operation.md). The picture this page is filling in is "what happens after Odoo's intent has materialized in the cluster's API server", regardless of which connectivity mode brought it there.

## The shape of the loop

The operator is a controller-runtime application with one reconciler per CRD plus a small set of source watchers. The pattern is the standard Kubernetes watch-queue-reconcile loop:

1. A watch on `ApplicationInstance`, `Operation`, and the downstream CRDs the operator created (`HelmRelease`, `Backup`, `Workflow`, `Job`, `VolumeSnapshot`, `Cluster`) emits an event whenever any of those resources change.
2. The event is mapped to a work item keyed by the *source* CR (an `ApplicationInstance` or `Operation`) and placed on a work queue.
3. A reconciler pulls the work item, reads the current desired state from the source CR's `spec`, reads the current actual state from the downstream CR's `status`, decides whether any change is needed, and either applies it or records the lack of need.
4. Status is written back via server-side apply on the source CR's `status` subresource, conditions are updated, and a Kubernetes `Event` is emitted for any condition transition.

Reconcilers are level-triggered. They do not rely on having seen every intermediate event. If the operator restarts and rebuilds its informers, the next reconcile sees the current truth and converges from there.

## `ApplicationInstance` → `HelmRelease`

For an `ApplicationInstance`, the reconciler does three things.

1. **Materialize the chart source.** If `spec.chart.sourceType` is `oci`, the reconciler ensures an `OCIRepository` exists in the cluster pointing at `spec.chart.url`. If `helm`, an `HelmRepository`. Both are owned resources and are server-side applied under the operator's field manager.
2. **Materialize the `HelmRelease`.** The reconciler renders a Flux `HelmRelease` whose `spec.chart.spec.chart` is `spec.chart.name`, whose version is `spec.chart.version`, whose values come from `spec.values` (inline, from a referenced `Secret`, or from a referenced `ConfigMap`), and whose target namespace is `spec.release.namespace`. The resulting `HelmRelease` is owned by the `ApplicationInstance` via an `ownerReference` and labeled with `app.vworkspace.io/application-instance=<name>`.
3. **Aggregate status.** The reconciler watches the materialized `HelmRelease` and translates its native Flux conditions into the operator's standard vocabulary. A `Ready=True` on the `HelmRelease` becomes `Ready=True` on the `ApplicationInstance`. A `Released=False` with reason `UpgradeFailed` becomes `Degraded=True` with reason `HelmReleaseFailed` and the underlying message preserved. A reconcile in progress becomes `Reconciling=True`.

Flux owns the Helm lifecycle from there: chart fetch, template, install or upgrade, drift remediation, rollback. The operator does not call `helm` itself, does not maintain Helm history, and does not parse rendered manifests. The chart's outputs (Deployments, Services, Ingresses, Jobs, ...) are watched only at the `HelmRelease` aggregate level.

## `Operation` → engine

For an `Operation`, the reconciler dispatches on `spec.engine` and, for each engine, creates and watches the appropriate downstream resource.

- **`velero`** — creates a `velero.io/Backup` or `velero.io/Restore` in the target namespace. Velero takes over.
- **`workflow`** — creates an `argoproj.io/Workflow` from a template referenced by `parameters.workflowTemplate`. Argo Workflows takes over.
- **`job`** — creates a Kubernetes `Job` from a templated `PodSpec` plus parameters. The Job controller takes over.
- **`helm`** — patches the existing `HelmRelease` (typically `spec.chart.version` or `spec.values`) to drive an upgrade or rollback. Flux takes over.
- **`volsync`** — creates a `volsync.backube/ReplicationSource` or `ReplicationDestination`. VolSync takes over.
- **`helmHookJob`** — invokes a named chart-provided hook by creating the chart's hook `Job` directly (the operator does not embed app-specific logic; it asks Helm to run a hook the chart already declares).

For every engine, the operator watches the downstream resource, aggregates its readiness or completion into `Operation.status.conditions` (`Accepted`, `Running`, `Succeeded`, `Failed`, `Cancelled`, `Blocked`), and populates `Operation.status.outputs` with the downstream resource name (`backupName`, `restoreName`, `workflowName`, `jobName`, `replicationName`, ...).

The detail of when to use which engine, and why operations are templated rather than per-application, is in [day-2-operations.md](day-2-operations.md).

## Idempotency

Every reconcile is required to be safe to repeat. Three rules enforce this.

- **Server-side apply with a stable field manager.** The operator uses a single `fieldManager` (e.g., `vworkspace-operator`) for every resource it writes. The Kubernetes API ensures the operator's field ownership is preserved across rewrites and that conflicting writes from a different field manager surface as a conflict instead of being silently overwritten. User edits to fields the operator does not own are not stomped.
- **Idempotency keys on every job.** In Pull mode, every job carries an `idempotencyKey` (typically `(uid, generation)` for object-shaped payloads or a stable hash for intent-shaped payloads). Re-delivering a job is a no-op. The same property holds for downstream resources: the operator's name and namespace selection is deterministic from the source CR, so re-reconciling produces the same `HelmRelease`, the same `Backup`, the same `Workflow`.
- **Generation gating.** The operator stamps `status.observedGeneration` to the `metadata.generation` of the source CR it last reconciled. Status writes that lag behind a newer generation are recognizable as stale.

## Ownership labels and field manager

Every resource the operator creates carries a small, consistent set of labels and is written under the operator's field manager. The well-known labels are documented in [../api/labels-and-annotations.md](../api/labels-and-annotations.md); the most important for the reconciliation model are:

- `app.vworkspace.io/managed-by=control-plane` — declares that Odoo is the upstream source of intent for this resource. Tooling can filter on it to distinguish operator-owned objects from anything else in the namespace.
- `app.vworkspace.io/cluster-id=<id>` — the cluster identity the operator was registered under. Useful for fleet-wide reporting from a central log or metrics store.
- `app.vworkspace.io/application-instance=<name>` — on resources downstream of an `ApplicationInstance`, names the source. Lets `kubectl get all -l app.vworkspace.io/application-instance=nextcloud-myteam` produce a coherent picture across `HelmRelease`, `Backup`, and other resources tied to the same instance.

Together with the field manager, these labels make ownership boundaries explicit. The operator owns labeled fields on labeled resources. A human admin can edit fields the operator does not own without triggering a fight; the operator can refuse to apply a job whose payload would conflict with a human's field ownership (see [drift, conflicts, and deduplication](../connectivity/pull-mode.md#drift-conflicts-and-deduplication)).

## Watch and queue model

The operator builds informers for the resources it owns (`ApplicationInstance`, `Operation`, `Cluster`) and the resources it watches (`HelmRelease`, `Backup`, `Restore`, `Workflow`, `Job`, `VolumeSnapshot`, `ReplicationSource`, `ReplicationDestination`). When a downstream resource changes, the operator's event handler maps it back to its source CR (using owner references and the `app.vworkspace.io/application-instance` label) and enqueues a reconcile.

The work queue is a rate-limited workqueue with exponential backoff on failure. The operator does not block on external systems while holding a queue slot: reconciles either apply state and return, or record the lack of need and return. Long-running operations (a Velero backup, an Argo Workflow) are *not* held by the reconciler; the reconciler creates the resource, returns, and reacts to the next informer event when the downstream controller updates its status.

The net effect is a small, well-behaved controller: bounded memory, predictable CPU, recoverable from a crash, and trivially observable through controller-runtime metrics. The interesting behaviour is in the resources it produces, not in the reconciler itself.
