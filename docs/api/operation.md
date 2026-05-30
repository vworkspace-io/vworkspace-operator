# `Operation`

**Group/Version/Kind:** `ops.vworkspace.io/v1alpha1` `Operation`
**Scope:** Namespaced
**Status:** Alpha
**Last Updated:** 2026-05-30

## Overview

`Operation` is a single generic CRD for day-2 actions against an `ApplicationInstance`. It carries a `type` (the verb: `Backup`, `Restore`, `Upgrade`, `Migration`, `RunCommand`, `Runbook`), an `engine` (the executor: `velero`, `workflow`, `job`, `helm`, `volsync`, `helmHookJob`), a reference to the target application, an open `parameters` object, and optional `approvals`. The operator picks the engine, materializes the matching downstream resource (a `velero.io/Backup`, an `argoproj.io/Workflow`, a Kubernetes `Job`, ...), watches it, and aggregates conditions back.

The conceptual treatment is in [../concepts/day-2-operations.md](../concepts/day-2-operations.md). The reasoning behind a single generic CRD rather than one CRD per verb is in [../concepts/crds.md](../concepts/crds.md#two-crds-not-twenty).

## Example

```yaml
apiVersion: ops.vworkspace.io/v1alpha1
kind: Operation
metadata:
  name: nextcloud-myteam-backup-2026-05-28
  namespace: org-myteam
  labels:
    app.vworkspace.io/managed-by: odoo
    app.vworkspace.io/cluster-id: cluster-prod-1
    app.vworkspace.io/application-instance: nextcloud-myteam
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
status:
  phase: Running
  startedAt: "2026-05-28T10:00:00Z"
  finishedAt: null
  conditions: []
  outputs:
    backupName: "velero-backup-xyz"
```

## `spec`

### `targetRef`

| Field                       | Type   | Required | Description |
|-----------------------------|--------|----------|-------------|
| `targetRef.apiVersion`      | string | Yes      | The `apiVersion` of the target resource. Currently `apps.vworkspace.io/v1alpha1`. |
| `targetRef.kind`            | string | Yes      | The `kind` of the target resource. Currently `ApplicationInstance`. |
| `targetRef.name`            | string | Yes      | The name of the target `ApplicationInstance`. Must live in the same namespace as the `Operation`. |

The admission webhook rejects `targetRef` values that point at non-existent or cross-namespace targets.

### `type`

The verb. Enum:

- `Backup` — produce a point-in-time backup of the target.
- `Restore` — restore the target from a prior backup.
- `Upgrade` — change the chart version or values of the target's underlying release.
- `Migration` — run a structured migration (typically via Argo Workflows or a chart hook).
- `RunCommand` — execute a constrained command inside the target (typically via a Kubernetes `Job`).
- `Runbook` — execute a curated multi-step runbook (typically via Argo Workflows).

### `engine`

The executor for this operation. Enum:

- `velero` — drive a `velero.io/Backup` or `velero.io/Restore`.
- `workflow` — drive an `argoproj.io/Workflow`.
- `job` — drive a Kubernetes `Job`.
- `helm` — drive an upgrade by patching the underlying `HelmRelease`.
- `volsync` — drive a `volsync.backube/ReplicationSource` or `ReplicationDestination`.
- `helmHookJob` — invoke a named chart-provided hook by creating the chart's hook `Job` directly.

The combination of `type` and `engine` must be consistent with the target `ApplicationInstance`'s capability annotations (`ops.vworkspace.io/<capability>=<engine>`). For example, `type: Backup` with `engine: velero` requires `ops.vworkspace.io/backup=velero` on the target. The admission webhook rejects mismatches.

### `parameters`

An open object whose schema is determined by the catalog template that produced this `Operation`. Examples by engine:

- `velero` backup: `retention`, `snapshotClassName`, `storageLocation`, `includeNamespaces`.
- `velero` restore: `backupName`, `namespaceMapping`, `restorePVs`.
- `workflow`: `workflowTemplate`, `workflowTemplateParameters`.
- `job`: `image`, `command`, `args`, `env`, `serviceAccountName`, `timeoutSeconds`.
- `helm`: `targetVersion`, `valuesPatch`.
- `volsync`: `source`, `destination`, `schedule`, `copyMethod`.
- `helmHookJob`: `hookName`.

The operator validates `parameters` against the engine's schema at apply time and produces a `Blocked` condition with an explicit reason if the parameters are missing or malformed.

### `approvals` *(optional)*

| Field                       | Type    | Required | Description |
|-----------------------------|---------|----------|-------------|
| `approvals.required`        | bool    | No       | Whether this operation requires an explicit approval claim before running. Catalog templates set this; admins can also set it explicitly. |
| `approvals.claim`           | string  | No       | An opaque approval claim issued by Odoo (typically a short-lived signed token) attesting that the human approval workflow has been satisfied. The admission webhook verifies the claim and rejects unsigned or expired claims. |
| `approvals.approvedBy`      | string  | No       | Human-readable identifier for the approver. Populated by Odoo at claim-issuance time and preserved for the audit log. |
| `approvals.approvedAt`      | string  | No       | RFC 3339 timestamp of the approval. |

When `approvals.required` is true and `approvals.claim` is absent or invalid, the resource is admitted with a `Blocked` condition (reason `AwaitingApproval`) and the operator does not start the operation. Once a valid claim is supplied, the operator proceeds.

## `status`

| Field                       | Type     | Description |
|-----------------------------|----------|-------------|
| `status.phase`              | enum     | One of `Pending`, `Running`, `Succeeded`, `Failed`, `Cancelled`. A high-level summary; per-transition detail lives in `status.conditions`. |
| `status.startedAt`          | string   | RFC 3339 timestamp of the moment the operator started running the engine. Empty until the operation leaves `Pending`. |
| `status.finishedAt`         | string   | RFC 3339 timestamp of the terminal transition. Empty until the operation reaches `Succeeded`, `Failed`, or `Cancelled`. |
| `status.conditions[]`       | []Condition | Standard Kubernetes condition objects. The type vocabulary is `Accepted`, `Running`, `Succeeded`, `Failed`, `Cancelled`, `Blocked`; see [conditions.md](conditions.md). |
| `status.outputs`            | object   | Engine-specific output references. Common keys: `backupName`, `restoreName`, `workflowName`, `jobName`, `replicationName`. The operator only writes keys relevant to the engine. |
| `status.logsRef`            | object   | Optional reference to where the engine's logs are persisted. Common shape: `{ kind, name, namespace }` for a `Workflow` whose pod logs are addressable, or `{ url }` for an external log store. |

## Conditions

See [conditions.md](conditions.md) for the full list of condition types and reasons emitted on `Operation.status.conditions[]`.

## Admission rules

The operator ships a validating admission webhook (`validate.operations.ops.vworkspace.io`). The rules:

- **Allowed `type` per namespace.** Each namespace labeled `managed-by=vworkspace` carries a configurable list of allowed operation types. The webhook rejects operations whose `type` is not allowed in the namespace. Typical examples: `Restore` and `Migration` are allowed only in non-production namespaces by default, with production namespaces opted in explicitly.
- **Engine/type consistency.** The combination of `type` and `engine` must match a catalog template the cluster knows about, and must match the target `ApplicationInstance`'s capability annotation for that capability.
- **Target existence.** `targetRef.name` must resolve to an existing `ApplicationInstance` in the same namespace. Cross-namespace targets are not allowed.
- **Concurrency rules.** Conflicting operations on the same target are rejected. The default conflict matrix:
  - No two `Upgrade` operations on the same target.
  - No `Restore` while an `Upgrade` is in progress.
  - No `Backup` while a `Restore` is in progress, and vice versa.
  - `RunCommand` and `Runbook` operations are concurrent-safe by default but can be marked exclusive in the catalog template if needed.
  Concurrency conflicts are admitted with `Blocked=True` (reason `ConflictingOperation`) rather than rejected outright, so the conflict is visible and the operation can proceed automatically once the conflicting one finishes.
- **Approvals.** If the catalog template for `(type, engine, target catalog entry)` requires approval, `approvals.required` must be true and `approvals.claim` must be a valid Odoo-issued claim. The webhook verifies the claim's signature and expiry.
- **Immutability of identity.** `spec.targetRef`, `spec.type`, and `spec.engine` are immutable after creation. Editing any of them would change the meaning of an in-flight operation. The right pattern is to cancel the current `Operation` (set an annotation or delete it) and create a new one.

The webhook errs on the side of admitting with a `Blocked` condition where possible, rather than rejecting outright, so the audit trail reflects what was attempted and why it did not proceed.
