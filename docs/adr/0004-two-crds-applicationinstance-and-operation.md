# 0004 — Two CRDs: ApplicationInstance and Operation

**Status:** Accepted (2026-05-28)

## Context

The operator's day-2 surface includes backup, restore, upgrade, migration, run-command, and operator-defined runbooks. The natural first design is one CRD per verb: `Backup`, `Restore`, `Upgrade`, `Migration`, `RunCommand`, `Runbook`. Each CRD has its own spec shape, its own status, its own admission rules, its own RBAC verbs.

This approach has appealing properties:

- **RBAC granularity per verb.** A `Role` that allows `create` on `Backup` does not allow `create` on `Restore`. Kubernetes RBAC encodes the safety policy directly.
- **Schema clarity.** Each CRD's spec only contains fields relevant to that verb.
- **Discoverability.** `kubectl explain backup` and `kubectl explain restore` show distinct, focused documentation.

It also has costs:

- **API surface explosion.** Six verbs means six CRDs, each with its own status contract, each with its own admission webhook configuration, each with its own conversion-webhook story when the API evolves. Adding a seventh verb is adding a seventh CRD.
- **Duplicated status contract.** Every verb wants the same condition vocabulary: `Accepted`, `Running`, `Succeeded`, `Failed`, `Cancelled`, `Blocked`. Six near-identical condition implementations is a maintenance hazard.
- **Cross-verb coordination is awkward.** A migration that wants to take a backup first is two CRs of two different kinds, related only by ownership references. Querying "all operations on `nextcloud-myteam` right now" requires listing six kinds and filtering.
- **Audit and observability fragmentation.** Metrics labels include the CRD kind, so dashboards have six panels per metric. The audit-event stream to Odoo carries six event types per verb.

A single CRD with `spec.type` and `spec.engine` flips these costs. The verb is a field, not a kind. A single condition contract, a single admission webhook, a single audit-event shape, a single status field set. The schema in `spec.parameters` is per-verb but does not require a new CRD kind; the admission webhook validates `parameters` against the operation template's input schema.

The objection to a generic CRD is "we lose RBAC granularity per verb". This objection is real but addressable. Kubernetes RBAC operates on kinds and verbs, not on field values; we cannot say "this role can create Operations of type=Backup but not type=Restore" with `kubectl create role`. The right place to enforce that is the operator's validating admission webhook, which has full access to the request body and can apply organization policy ("the `org-myteam` namespace is allowed to create Operations of type Backup, Restore, and Upgrade, but not Migration"). The webhook's policy data is configurable per namespace via `Cluster.status.managedNamespaces[].allowedOperationTemplates[]`.

This is not unique to vWorkspace: the same pattern is used by tools like Velero (one `Backup` CRD plus a `Restore` CRD that references it, with phase-gating in the controller's admission), Argo Workflows (one `Workflow` CRD whose semantics vary by template), and Knative Serving (one `Service` whose `spec.template` can carry different runtime shapes). The generic CRD with controller-level admission is a well-trodden pattern.

The second consideration is `ApplicationInstance`. The application-state CRD is different in shape from operations: it represents desired state ("this chart should be installed with these values"), it has a long-lived lifecycle (it exists for as long as the application does), and it is reconciled continuously against Flux's `HelmRelease`. Combining it with `Operation` would conflate "the thing exists" with "a verb is being performed on the thing", and break the natural CR lifecycle.

So the right shape is **two CRDs**: `ApplicationInstance` for desired state and `Operation` for verbs. Inside `Operation`, the verb is `spec.type`.

## Decision

The operator owns **two CRDs**:

- `apps.vworkspace.io/v1alpha1/ApplicationInstance` — desired application state. References a chart and a values input; materializes a `HelmRelease`; reconciled continuously.
- `ops.vworkspace.io/v1alpha1/Operation` — a day-2 verb. References a target `ApplicationInstance`; carries `spec.type` (Backup, Restore, Upgrade, Migration, RunCommand, Runbook) and `spec.engine` (velero, workflow, job, helm, helmHookJob, volsync, snapshot); has a finite lifecycle.

There are **not** separate `Backup`, `Restore`, `Upgrade`, `Migration`, `RunCommand`, `Runbook` CRDs. New verbs are added by extending the `Operation.spec.type` enum and the operation-template catalog, not by adding a CRD.

RBAC across verbs is enforced by the operator's validating admission webhook. The webhook reads the target namespace's `allowedOperationTemplates[]` from `Cluster.status.managedNamespaces[]` and rejects requests for templates the namespace is not authorized to run. The admission rules live under `internal/admission/operation_webhook.go` (planned location; see [../development/project-layout.md](../development/project-layout.md)).

A third CRD, `Cluster`, exists in `ops.vworkspace.io/v1alpha1` as the cluster's identity and overall-health record. It is small (one per cluster), not a CR per verb, and not in scope for this ADR.

## Consequences

**Smaller API surface.** Two CRDs to install, two to maintain, two to evolve through conversion webhooks. Adding a new verb does not add CRD surface; it adds a new value in the `type` enum and a new operation template.

**One status contract for all verbs.** `Operation.status.conditions` uses one vocabulary (`Accepted`, `Running`, `Succeeded`, `Failed`, `Cancelled`, `Blocked`) across every verb. Engine-specific reasons (`VeleroBackupSucceeded`, `WorkflowFailed`, `HookNotFound`) surface in the `reason` field. Dashboards, logs, and audit-event consumers learn one shape.

**RBAC granularity via the webhook, not via Kubernetes verbs.** Organizations that want "namespace X can do Backups but not Restores" configure that policy on `Cluster.status.managedNamespaces[].allowedOperationTemplates[]`. The webhook enforces it. This is a deliberate trade: Kubernetes RBAC is no longer the only safety belt, but the webhook is more expressive and covers parameter-level policy (e.g., "this namespace can request Velero backups but not VolSync backups").

**Cleaner cross-verb coordination.** A migration that needs a backup first is an `Operation` of `type: Migration` and `engine: workflow` whose workflow's first step creates a child `Operation` of `type: Backup`. The relationship is owner references and labels (`ops.vworkspace.io/parent-operation: <uid>`), not cross-CRD navigation.

**Operation templates carry the schema.** Each (type, engine) combination has an operation template with an `inputSchema`. The webhook validates `Operation.spec.parameters` against it; `kubectl explain operation` is necessarily generic but the templates' schemas are documented in [../operations/operation-templates.md](../operations/operation-templates.md) and the per-engine docs ([../operations/engines/](../operations/engines/)).

**Discoverability via templates, not kinds.** `kubectl get operation` lists all day-2 actions across a namespace, regardless of verb. Filtering by verb is `kubectl get operation -o json | jq '.items[] | select(.spec.type=="Backup")'`. Operators get used to this quickly; the trade-off is acceptable.

**Future-proof for new engines.** Adding a new engine for an existing verb (e.g., a Restic-based backup engine) is one new value in the `engine` enum and one new engine adapter under `internal/engines/`. No new CRD.

**A new ADR is required to split.** If experience shows the single-CRD design becomes unwieldy (e.g., the parameter schema across verbs grows unmanageable), the project can introduce per-verb CRDs in a future major version. This ADR would be superseded; the conversion-webhook discipline in [../development/release-process.md](../development/release-process.md) would govern the migration.
