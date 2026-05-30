# Day-2 operations

**Status:** Alpha
**Last Updated:** 2026-05-30

Installing an application is the easy part. The hard part is everything that happens after: backups, restores, upgrades, migrations, rollbacks, configuration changes that need a maintenance window, and recovery from incidents. `vworkspace-operator` treats day-2 operations as **first-class resources**, expressed as `Operation` CRs (`ops.vworkspace.io/v1alpha1`) and executed by the appropriate proven controller. There is no separate scripting layer in Odoo, no SSH-into-a-cluster runbook, no per-application Python in the operator.

The mechanism is intentionally small: one CRD, six engines, a capability metadata model that tells Odoo which operations make sense for each application. The result is that an operator (the person) can ask "back up Nextcloud", "upgrade Mattermost", "restore Gitea to last night", and the system knows exactly which engine to use, which inputs to pass, and which conditions to wait on.

The full spec/status reference for `Operation` is in [../api/operation.md](../api/operation.md). This page explains the *model*: how operations are templated, how engines are selected, and why the operator never contains application-specific lifecycle code.

## The five execution patterns

Every `Operation` resolves to one of five execution patterns. The `engine` field in `Operation.spec` names which one. The reconciliation flow for each pattern is in [reconciliation-model.md](reconciliation-model.md#operation--engine).

### Pattern 1: Kubernetes `Job` (`engine: job`)

A one-shot `Job` runs an image (commonly `velero`, `kubectl`, `helm`, `pg_dump`, or a chart-provided utility image) with a service account scoped to the target namespace. The operator constructs the `PodSpec` from `parameters` and waits for the `Job` to reach a terminal state.

- **When to use.** One-shot tasks that are safe and predictable: export configuration, dump a database to object storage, run a chart hook job manually, execute a maintenance command inside a pod.
- **Why.** Jobs are the smallest possible building block. They give us containers, service accounts, namespace scoping, and `kubectl logs`, with no additional controller dependency.

### Pattern 2: Argo Workflows (`engine: workflow`)

The operator creates an `argoproj.io/Workflow` from a `WorkflowTemplate` named in `parameters`. Argo Workflows provides DAG orchestration, conditional steps, retries with policy, timeouts, and durable artifact storage.

- **When to use.** Multi-step operations with branching logic, robust retries, or a need for a durable audit trail of intermediate outputs. Typical examples: "prechecks → quiesce → snapshot → verify → unquiesce", or "restore PV → wait for app to start → run smoke test → flip ingress".
- **Why.** The operator does not need to implement orchestration when a well-supported controller already does it. Workflows that the operator drives can be inspected, retried, and replayed without operator changes.

### Pattern 3: Velero (`engine: velero`)

The operator creates a `velero.io/Backup` or `velero.io/Restore` resource. Velero handles namespace-scoped backup and restore, including PV data via its own integration with CSI snapshots or restic.

- **When to use.** Standardized namespace-scoped backup and restore. The default backup engine in the vWorkspace bundle.
- **Why.** Velero is the production-grade backup controller for Kubernetes namespaces. Reimplementing any part of it would be a bad use of the project's time.

### Pattern 4: CSI snapshots and VolSync (`engine: volsync`)

The operator creates either a `VolumeSnapshot` (CSI snapshot controller) or a VolSync `ReplicationSource`/`ReplicationDestination` for PV-level snapshots and replication. The operator tracks readiness and aggregates conditions into the `Operation` status.

- **When to use.** Storage-centric backup and restore with strong RPO/RTO requirements, off-cluster replication of volumes, or scenarios where a chart-level Velero backup is too coarse.
- **Why.** Storage replication has its own well-developed controllers; the operator orchestrates them rather than re-implementing them.

### Pattern 5: Chart-provided hooks (`engine: helm` and `engine: helmHookJob`)

Many charts already embed `helm.sh/hook` jobs for migrations and pre/post upgrade tasks. The operator does not duplicate these. Two engines surface them.

- `engine: helm` — the operator drives an upgrade by patching the `HelmRelease` (typically `spec.chart.version` or `spec.values`). The chart's hooks run as part of the upgrade, observed via the Helm controller. The operator never executes `helm upgrade` itself.
- `engine: helmHookJob` — the operator triggers a *named* chart-provided hook by creating the chart's hook `Job` directly. This is the escape hatch for "run the migration job the chart already declares, but on demand."

- **When to use.** Upgrades and migrations where the chart is authoritative; never as a place to add per-application logic.
- **Why.** Application-aware steps belong in the chart. The operator's job is to ask the chart to run them.

## Templated workflows, not bespoke code

Every operation is a template plus parameters. The catalog entry in Odoo declares, for each application:

- The set of operation types the application supports (`Backup`, `Restore`, `Upgrade`, `Migration`, `RunCommand`, `Runbook`).
- For each type, the engine that executes it (one of the six above).
- The input schema (allowed parameters).
- The RBAC profile required for execution.
- Preconditions (e.g., target `ApplicationInstance` must be `Ready=True`, must not be currently upgrading).
- Target selectors (labels or annotations on `ApplicationInstance` or `HelmRelease`).

When a human in Odoo (or the AI assistant on their behalf) asks for "a backup of Nextcloud", Odoo picks the matching template, fills in the parameters, and emits an `Operation` resource via the active connectivity mode. The operator on the cluster validates the request against its admission webhook, picks the engine the template named, and runs it. The operator does not need to know what Nextcloud is. The catalog entry plus the template did all the application-specific reasoning at design time.

This is why the operator can support new applications by editing the catalog in Odoo, not by changing the operator's code.

## Capability metadata

`ApplicationInstance` resources carry a small set of annotations that declare which day-2 operations the instance supports and which engine should execute each. They are the runtime expression of the catalog's templates.

- `ops.vworkspace.io/backup=velero`
- `ops.vworkspace.io/restore=velero`
- `ops.vworkspace.io/upgrade=helm`
- `ops.vworkspace.io/quiesce=exec` *(optional; only if the chart exposes a quiesce hook or known endpoint)*

The full vocabulary, including the semantics and which values are allowed, is in [../api/labels-and-annotations.md](../api/labels-and-annotations.md).

Where capability annotations come from:

- **Primarily from the catalog entry in Odoo.** Curated metadata, version-controlled with the rest of the platform.
- **Optionally overridden by chart annotations** when the project controls packaging of the chart.
- **Never inferred** by inspecting Deployments or Pods. There is no "app-specific detection logic" anywhere in the operator; that would be exactly the per-app code the design forbids.

## Integration with chart hooks

The rules of thumb for what kind of engine to pick for a given verb:

- For **upgrades and migrations**, prefer chart-provided hooks. Chart maintainers know the migration semantics; the operator should just trigger them.
- For **backup and restore**, prefer external standardized engines — Velero by default, CSI snapshots or VolSync when storage-level granularity is needed. Avoid chart-specific backup scripts; they vary too widely between charts to express coherently in the catalog.
- When a chart **does** provide a built-in backup or migration job, treat it as a generic engine (`engine: helmHookJob`) that triggers a named hook. It is still a chart-provided hook; the operator just exposes it as a triggerable verb.

The bright line — repeated wherever this question comes up — is that the operator never contains code that knows what application it is dealing with. The catalog, the chart, and the templates know. The operator wires.

## What this buys

Modeling day-2 work this way has three concrete consequences.

- **Audit and observability come for free.** Every operation is a Kubernetes resource. `kubectl describe operation/...` shows the full history. The Pull-mode status endpoint streams condition transitions back to Odoo. The Discuss audit channel sees every accepted, succeeded, and failed operation. There is no separate runbook tool to keep in sync.
- **New applications are catalog work, not operator work.** Adding a new app to the curated catalog means writing its catalog entry (chart coordinates, capability annotations, default values, supported operations). It does not mean shipping a new operator version.
- **The operator stays small.** Six engines is the entire surface area. Six engines covers every operation Odoo currently asks for and every operation the roadmap envisions. If a future operation needs something new, the right move is almost always to write a new template against an existing engine, not to add a seventh.
