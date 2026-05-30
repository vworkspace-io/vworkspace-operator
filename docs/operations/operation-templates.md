# Operation templates and capabilities

**Status:** Alpha
**Last Updated:** 2026-05-30

`vworkspace-operator` runs day-2 actions without encoding app-specific logic. The mechanism is a small, generic operation-template model coupled to capability metadata declared on each `ApplicationInstance`. The template says "an operation of this shape can be requested"; the capability says "this particular application is willing to be operated on this way, using this engine". The operator combines the two to validate a request and pick the right engine.

This document defines the template/capability model, where capabilities come from, and how they line up with the `Operation` CRD documented in [../api/operation.md](../api/operation.md).

## Why a template/capability split

The alternative — a separate CRD per verb (`Backup`, `Restore`, `Upgrade`, `Migration`, `RunCommand`, `Runbook`) — multiplies the API surface and copies the same condition contract across CRDs. We keep a single `Operation` CRD (recorded in [ADR 0004](../adr/0004-two-crds-applicationinstance-and-operation.md)) and let templates restrict which combinations of `type`, `engine`, target, and parameters are runnable. This keeps RBAC, audit, and status mapping uniform across every day-2 verb.

The template/capability split also keeps app-specific behavior out of the operator. The operator does not "know" how to back up Nextcloud differently from WordPress; it knows that an `ApplicationInstance` annotated `ops.vworkspace.io/backup=velero` can run a Backup template that materializes a `velero.io/Backup`. The control plane catalog curates what is annotated and which template applies; the operator stays generic.

## Anatomy of an operation template

A template is a server-side declaration that the operator and the control plane catalog both understand. It binds the following:

| Field            | Meaning                                                                                                                                                                                                                  |
|------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `type`           | One of `Backup`, `Restore`, `Upgrade`, `Migration`, `RunCommand`, `Runbook`. Maps 1:1 onto `Operation.spec.type`.                                                                                                          |
| `engine`         | One of `velero`, `workflow`, `job`, `helm`, `helmHookJob`, `volsync`, `snapshot`. Maps 1:1 onto `Operation.spec.engine`.                                                                                                  |
| `inputSchema`    | JSON Schema describing the allowed `Operation.spec.parameters`. The admission webhook rejects requests that do not validate.                                                                                              |
| `rbacProfile`    | Name of an in-cluster `Role`/`RoleBinding` set the operator must hold in the target namespace for this combination of `type` and `engine`. See [../security/rbac.md](../security/rbac.md).                                |
| `preconditions`  | A small declarative list (target `Ready=True`, no conflicting `Operation` in flight, named `Secret` exists, target generation matches). The reconciler evaluates these before materializing any child resource.           |
| `targetSelectors`| Label and annotation selectors that constrain which `ApplicationInstance` resources the template applies to. The capability annotations described below are the primary selector.                                          |

Templates are not themselves stored as CRDs in the cluster; they are part of the control plane catalog plus the operator's compiled-in defaults. The operator ships with a small built-in set (Velero Backup, Velero Restore, Helm Upgrade, Helm Hook Migration, Kubernetes Job RunCommand, Argo Workflows Runbook) and the control plane catalog can extend or constrain that set per organization.

## Capability metadata on `ApplicationInstance`

Capabilities are declared as well-known annotations on the `ApplicationInstance` resource. The annotation key states the verb; the annotation value names the engine that is willing to execute it. The operator reads the annotation; it never tries to detect the capability by inspecting Pods, Deployments, or chart contents.

| Annotation                          | Meaning                                                                              | Example value      |
|-------------------------------------|--------------------------------------------------------------------------------------|--------------------|
| `ops.vworkspace.io/backup`          | This application can be backed up by the named engine.                               | `velero`           |
| `ops.vworkspace.io/restore`         | This application can be restored by the named engine.                                | `velero`           |
| `ops.vworkspace.io/upgrade`         | This application's upgrade path is owned by the named engine.                        | `helm`             |
| `ops.vworkspace.io/migration`       | This application supports a chart-provided migration hook (or workflow).             | `helmHookJob`      |
| `ops.vworkspace.io/snapshot`        | This application's persistent volumes are snapshottable by the named engine.         | `snapshot`         |
| `ops.vworkspace.io/replicate`       | This application's persistent volumes can be replicated by the named engine.         | `volsync`          |
| `ops.vworkspace.io/quiesce`         | The chart exposes a quiesce hook the engine can invoke before snapshot/backup.       | `exec`             |
| `ops.vworkspace.io/runbook`         | An operator-defined runbook (Argo `WorkflowTemplate`) applies to this application.   | `workflow`         |

A minimal `ApplicationInstance` declaring its capabilities:

```yaml
apiVersion: apps.vworkspace.io/v1alpha1
kind: ApplicationInstance
metadata:
  name: nextcloud-myteam
  namespace: org-myteam
  labels:
    app.vworkspace.io/managed-by: control-plane
    app.vworkspace.io/cluster-id: cluster-prod-1
  annotations:
    ops.vworkspace.io/backup: velero
    ops.vworkspace.io/restore: velero
    ops.vworkspace.io/upgrade: helm
    ops.vworkspace.io/migration: helmHookJob
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
```

When an `Operation` of `type: Backup` and `engine: velero` is requested against `nextcloud-myteam`, the admission webhook checks `ops.vworkspace.io/backup: velero` is present, looks up the Velero Backup template, validates `spec.parameters` against its `inputSchema`, evaluates preconditions, and only then admits the request. Requesting `type: Backup` and `engine: volsync` against this same `ApplicationInstance` would be rejected because the capability does not advertise VolSync.

## Where capabilities come from

Capabilities are curated, not inferred. The order of precedence is:

1. **control plane catalog (primary).** Each catalog entry in the vWorkspace Server control plane carries the capability set that should be projected onto an `ApplicationInstance` created from it. The operator reads the catalog entry as part of the apply (Pull mode), or Odoo stamps the annotations directly when it server-side-applies the CR (Push mode), or the rendered manifest in Git already contains them (GitOps mode).
2. **Chart annotations (secondary).** If the chart's `Chart.yaml` declares vWorkspace annotations under its own `annotations:` block (for example, a chart maintained by the vWorkspace project itself), the operator may copy them onto the `ApplicationInstance` at apply time. This is opt-in per catalog entry and is used only for charts whose maintainers have explicitly modeled the capability contract.
3. **Never from Pods or Deployments.** The operator does not introspect rendered chart objects to guess what a chart can do. Doing so would couple the operator to app-internal naming conventions and silently misclassify capabilities when charts change.

The practical consequence is that adding a new application to the vWorkspace catalog means describing its capabilities once in the control plane catalog entry, not writing app-specific operator code. Adding a new operation engine (for example, an OnlyOffice-aware migration runner) means writing one Argo `WorkflowTemplate` and one template entry, not extending the operator.

## Preconditions

Preconditions are evaluated before any child resource is created. The standard set, evaluated in order, is:

| Precondition                | Failure mode                                                                                                              |
|-----------------------------|---------------------------------------------------------------------------------------------------------------------------|
| `TargetExists`              | `Operation.status.conditions[Accepted]=False` with reason `TargetNotFound`. No child resource is created.                  |
| `TargetReady`               | If `Operation.spec.policy.requireReady=true` (default for destructive verbs), wait for the target's `Ready=True` condition. Surfaced as `Blocked=True` with reason `TargetNotReady`. |
| `CapabilityAdvertised`      | `Operation.status.conditions[Accepted]=False` with reason `CapabilityMissing` when the matching `ops.vworkspace.io/*` annotation is absent or names a different engine.         |
| `NoConflictingOperation`    | While another `Operation` is `Running` on the same target with an overlapping verb class, the new one is held in `Blocked=True` with reason `ConflictingOperation`.            |
| `RequiredSecretsPresent`    | Secrets named in `spec.parameters` (Velero `BackupStorageLocation` credential, restore-time decryption key) must resolve. Otherwise `Blocked=True`, reason `MissingSecret`.    |
| `MaintenanceWindow`         | If the catalog entry constrains the operation to a maintenance window, `Blocked=True` with reason `OutsideMaintenanceWindow` until the window opens.                            |

Preconditions are reported as the `Blocked` condition on the `Operation`, so callers (Odoo, the AI assistant, a human running `kubectl describe`) see exactly why an operation is waiting. The reconciler retries with backoff and clears the `Blocked` condition once preconditions are satisfied.

## Target selectors

A template restricts the `ApplicationInstance` resources it may target. The two selector styles are:

- **Capability selector (default).** "Targets must annotate `ops.vworkspace.io/<verb>: <engine>`". This is implicit in every built-in template and is what most operators rely on.
- **Label selector (advanced).** Catalog templates may further restrict targets by label (for example, `tier in (production, staging)`). This is the hook by which an organization can declare "the destructive-migration template is allowed only on `tier=staging` applications until the runbook has been signed off".

Selectors are evaluated by the admission webhook; the API server returns a clear rejection message when a request cannot be admitted.

## RBAC profiles

Each template names an `rbacProfile`. The profile is a stable identifier — `backup.velero`, `restore.velero`, `upgrade.helm`, `migration.helmHookJob`, `runCommand.job`, `runbook.workflow` — that the operator's namespace-scoped `RoleBinding`s grant. The RBAC model and the binding examples live in [../security/rbac.md](../security/rbac.md); the relevant property for templates is that gaining the ability to run a new operation type in a namespace is a one-line `RoleBinding` change, not an operator redeploy.

## How templates map onto status

Every template lands on the same condition contract. The full set of condition types and reasons is in [../api/conditions.md](../api/conditions.md); the abbreviated shape is:

```yaml
status:
  phase: Running
  startedAt: "2026-05-28T10:00:00Z"
  finishedAt: null
  conditions:
    - type: Accepted
      status: "True"
      reason: TemplateValidated
      message: "Velero Backup template; preconditions satisfied"
    - type: Running
      status: "True"
      reason: VeleroBackupInProgress
      message: "velero.io/Backup velero-backup-xyz is InProgress"
  outputs:
    backupName: velero-backup-xyz
```

Engine-specific reasons (`VeleroBackupInProgress`, `WorkflowNodeFailed`, `HelmHookJobSucceeded`, `VolumeSnapshotReadyToUse`) are surfaced verbatim under `Operation.status.conditions[].reason`. This is the seam where engine-specific behavior surfaces without leaking into the operator's own logic.

## Adding a new template

New templates land in three places, in this order:

1. **Catalog entry in Odoo.** Declares the verb, engine, parameter schema, and which catalog applications advertise the matching capability.
2. **Operator built-in (optional).** If the engine is one of the seven the operator already integrates with, no operator change is required. If it is a new engine (for example, integrating Restic directly), an engine adapter must be added under `internal/engines/` as described in [../development/project-layout.md](../development/project-layout.md).
3. **RBAC profile.** A new entry under `config/rbac/` granting the operator the rights it needs in target namespaces.

The first two are independent; the third is the safety belt. A template that would require RBAC the operator does not hold is rejected with reason `RbacProfileMissing` rather than failing partway through execution.

## Related material

- [../api/operation.md](../api/operation.md) — `Operation` CRD fields and validation.
- [../api/labels-and-annotations.md](../api/labels-and-annotations.md) — Well-known labels and capability annotations.
- [../security/rbac.md](../security/rbac.md) — RBAC profiles and least-privilege rationale.
- [../adr/0004-two-crds-applicationinstance-and-operation.md](../adr/0004-two-crds-applicationinstance-and-operation.md) — Why a single `Operation` CRD with `type` and `engine`.
