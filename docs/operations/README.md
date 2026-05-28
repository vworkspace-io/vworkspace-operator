# Operations

**Status:** Alpha — APIs are at `v1alpha1` and may evolve.
**Last Updated:** 2026-05-28

This chapter explains how day-2 work — backups, restores, upgrades, migrations, run-commands, and runbooks — is modeled and executed by `vworkspace-operator`. The single shape that carries every day-2 action is the `Operation` custom resource (`ops.vworkspace.io/v1alpha1`); the operator translates each `Operation` into resources owned by a small set of proven third-party controllers, observes their progress, and reports back through a stable condition contract.

The operating principle is the same as for application deployment: the operator orchestrates and reports, third-party controllers execute. Velero owns backup and restore primitives; Argo Workflows owns multi-step DAGs; the Kubernetes Job controller owns one-shot tasks; the CSI snapshot controller and VolSync own storage-level snapshots and replication; the Helm engine and the chart's own `helm.sh/hook` jobs own upgrade-time migrations. The operator's job is to make the right resource appear in the cluster, with the right inputs, against the right target, and to translate the result back into `Operation.status.conditions` that Odoo and the AI assistant can reason about uniformly.

## Read in order

1. [operation-templates.md](operation-templates.md) — The template and capability model. How an operation type, an engine, an input schema, an RBAC profile, and a set of preconditions combine into a runnable `Operation`. Where capability metadata on an `ApplicationInstance` comes from and why it is curated, not inferred.
2. [engines/velero.md](engines/velero.md) — Backup and restore via Velero. When to use it, the materialized `velero.io/Backup` or `velero.io/Restore`, and how Velero phases map back to `Operation.status.conditions`.
3. [engines/argo-workflows.md](engines/argo-workflows.md) — Multi-step DAGs via Argo Workflows. When to use it, the materialized `workflows.argoproj.io/v1alpha1` `Workflow`, and a worked migration example.
4. [engines/kubernetes-jobs.md](engines/kubernetes-jobs.md) — One-shot portable tasks via the Kubernetes `Job` controller. When to use it, the materialized `batch/v1` `Job`, and how service accounts are scoped.
5. [engines/csi-snapshots-volsync.md](engines/csi-snapshots-volsync.md) — Storage-centric snapshots and replication. CSI snapshots versus VolSync replication, RPO/RTO trade-offs, and a worked `VolumeSnapshot` example.
6. [engines/helm-hooks.md](engines/helm-hooks.md) — Triggering chart-provided `helm.sh/hook` jobs for upgrade-time migrations through the `helmHookJob` engine, without duplicating chart-internal logic.
7. [backups-and-restores.md](backups-and-restores.md) — End-to-end backup and restore narrative: request, retention, restoring into another namespace, validation, and the pitfalls worth knowing.
8. [upgrades-and-migrations.md](upgrades-and-migrations.md) — Bumping `chart.version` on an `ApplicationInstance`, when to escalate to an `Operation` of `type: Migration`, rolling back via Flux, and the forbidden-version policy.

## How an `Operation` becomes work

A request enters the cluster as an `Operation` CR (`ops.vworkspace.io/v1alpha1`) that names a target `ApplicationInstance`, picks a `type` (Backup, Restore, Upgrade, Migration, RunCommand, Runbook), and picks an `engine`. The operator's reconciler:

1. Validates the request against the matching operation template (allowed types per namespace, parameter schema, target capabilities declared via `ops.vworkspace.io/*` annotations on the `ApplicationInstance`).
2. Resolves any preconditions (target `Ready=True`, no conflicting operation in flight, prerequisite secrets present).
3. Materializes engine-specific child resources owned by the `Operation` (a `velero.io/Backup`, a `workflows.argoproj.io/v1alpha1` `Workflow`, a `batch/v1` `Job`, a `snapshot.storage.k8s.io/VolumeSnapshot`, or a chart-defined hook `Job`).
4. Watches those child resources, aggregates their status into `Operation.status.conditions` and `Operation.status.outputs`, emits Kubernetes events on every condition transition, and forwards a coalesced event stream back to Odoo over the active connectivity mode.

The cluster is the source of truth for what is actually happening; Odoo is the source of truth for what was asked for. The contract between them is the `Operation` CR, its status, and the event stream described in [../operate/observability.md](../operate/observability.md).

## What this chapter does not cover

The operator's own observability surface (Prometheus metrics, structured logs, audit events) is documented in [../operate/observability.md](../operate/observability.md). The CRD spec and status fields are in [../api/operation.md](../api/operation.md). The RBAC model that gates which `Operation` types are allowed in which namespace is in [../security/rbac.md](../security/rbac.md). The reasoning behind a single `Operation` CRD instead of one CRD per verb is recorded as [ADR 0004](../adr/0004-two-crds-applicationinstance-and-operation.md).
