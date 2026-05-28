# Labels and annotations

**Status:** Alpha
**Last Updated:** 2026-05-28

`vworkspace-operator` reads and writes a small set of well-known labels and annotations to express ownership, cluster identity, and per-application day-2 capabilities. This page documents what each one means, where the operator reads it from, and what it writes back.

The vocabulary is intentionally small. The labels are stable and machine-parseable; the annotations carry the configuration the operator cannot infer from the chart.

## Labels

Labels are used for selection and ownership. The operator reads all three, writes the first two on every resource it owns, and writes the third on downstream resources tied to a specific `ApplicationInstance`.

### `app.vworkspace.io/managed-by`

**Required on `ApplicationInstance` and `Operation`. Required on namespaces the operator is allowed to act in (as `managed-by=vworkspace`, see below for the namespace gate exception). Optional on downstream resources, where the operator writes it for clarity.**

| Value      | Meaning |
|------------|---------|
| `odoo`     | Odoo is the upstream source of intent for this resource. The operator only acts on `ApplicationInstance` and `Operation` resources carrying this label. |
| `vworkspace` | Used on namespaces as the namespace gate (see below). |

Resources without `app.vworkspace.io/managed-by=odoo` are ignored by the operator's reconcilers. This is the bright line that distinguishes "things Odoo asked for" from "things a human or another system created."

### `app.vworkspace.io/cluster-id`

**Required on `ApplicationInstance` and `Operation`. Written by the operator on every downstream resource it creates.**

| Value      | Meaning |
|------------|---------|
| string     | The stable cluster identity the operator was registered under (e.g., `cluster-prod-1`). |

The admission webhook rejects `ApplicationInstance` or `Operation` resources whose `app.vworkspace.io/cluster-id` does not match the operator's own cluster identity. This catches misrouted jobs (a job intended for cluster A delivered to cluster B) and surfaces them as admission errors rather than silently applying them.

In Pull mode, the cluster ID is also asserted server-side on every request to `/api/agent/jobs`; in Push and GitOps modes the label is the only enforcement. Either way the label is the in-cluster record of who this resource is for.

### `app.vworkspace.io/application-instance`

**Optional on `ApplicationInstance` (it would be redundant with `metadata.name`). Written by the operator on every downstream resource tied to a specific `ApplicationInstance` (`HelmRelease`, `OCIRepository`, `HelmRepository`) and on every `Operation` that targets that instance.**

| Value      | Meaning |
|------------|---------|
| string     | The `metadata.name` of the source `ApplicationInstance`. |

This label is the primary key for "show me everything tied to this application instance":

```
kubectl get all -l app.vworkspace.io/application-instance=nextcloud-myteam -A
```

returns the `HelmRelease`, the chart-source resource, any in-flight `Operation` resources, and any downstream operation resources (`Backup`, `Workflow`, `Job`, ...) for the named instance. The operator relies on this label to map informer events back to the source CR.

## Namespace gate

A namespace is opted into operator management by carrying the label:

```
metadata:
  labels:
    managed-by: vworkspace
```

This is a *label*, not an annotation, and the key is bare `managed-by` (not `app.vworkspace.io/managed-by`). It exists because RBAC permissions to act in a namespace are most naturally expressed against labels, and because making a namespace operator-managed is an explicit local opt-in by the cluster admin — not something Odoo can do by writing a CR.

- **Required.** The operator's RBAC profile gives it permissions only in namespaces matching `managed-by=vworkspace`. Apply attempts in other namespaces are rejected by the admission webhook with reason `NamespaceNotGated`.
- **Set by.** The cluster admin or the install bundle. The operator does not create, label, or relabel namespaces itself.

If you also see `app.vworkspace.io/managed-by=odoo` on a namespace, treat it as informational (some operators set it to reflect that Odoo is the source of intent for the namespace); the namespace gate that actually grants the operator permissions is `managed-by=vworkspace`.

## Annotations

Annotations carry per-resource configuration. The operator reads capability annotations on `ApplicationInstance` to know which day-2 operations the instance supports and which engine should execute each.

### Capability annotations: `ops.vworkspace.io/<capability>`

**Read on `ApplicationInstance`. Populated by Odoo at creation time from the catalog entry's metadata.**

The format is one annotation per capability, with the value naming the engine that executes it:

- `ops.vworkspace.io/backup=<engine>` — declares the engine used for `Operation` `type: Backup` against this instance.
- `ops.vworkspace.io/restore=<engine>` — declares the engine used for `Operation` `type: Restore` against this instance.
- `ops.vworkspace.io/upgrade=<engine>` — declares the engine used for `Operation` `type: Upgrade` against this instance.
- `ops.vworkspace.io/quiesce=<engine>` — *optional.* Declares an engine that knows how to quiesce the application (typically `exec` against a chart-provided endpoint or hook). Used as a precondition step inside multi-step backup workflows.

The allowed values for `<engine>` are the same enum as `Operation.spec.engine`: `velero`, `workflow`, `job`, `helm`, `volsync`, `helmHookJob`. The admission webhook on `Operation` validates that the requested `(type, engine)` matches the corresponding capability annotation on the target `ApplicationInstance`.

Where capability annotations come from (recap from [../concepts/day-2-operations.md](../concepts/day-2-operations.md#capability-metadata)):

- **Primarily from the catalog entry in Odoo.** Curated metadata maintained alongside the rest of the platform.
- **Optionally overridden by chart annotations** when the project controls packaging of the chart.
- **Never inferred** by inspecting Deployments or Pods. There is no "app-specific detection logic" anywhere in the operator.

### Other annotations (operator-internal)

The operator also writes a small number of internal annotations on downstream resources. These are stable but not part of the public API surface — they exist to support reconciliation and may evolve between alpha releases without a deprecation note. They are listed here for transparency:

- `vworkspace.io/applied-intent-hash` — opaque hash of the intent that produced the resource. Used for idempotency on intent-shaped Pull-mode payloads.
- `vworkspace.io/last-applied-generation` — `metadata.generation` of the source CR when this resource was last reconciled. Used to detect stale downstream state.

These are not configuration; do not edit them by hand.

## Required vs optional summary

| Item                                              | Where                              | Required? |
|---------------------------------------------------|------------------------------------|-----------|
| `app.vworkspace.io/managed-by=odoo` (label)        | `ApplicationInstance`, `Operation` | Required  |
| `app.vworkspace.io/cluster-id=<id>` (label)        | `ApplicationInstance`, `Operation` | Required  |
| `app.vworkspace.io/application-instance=<name>` (label) | Downstream resources of an `ApplicationInstance` | Written by operator |
| `managed-by=vworkspace` (label on namespace)       | Operator-managed namespaces        | Required  |
| `ops.vworkspace.io/backup=<engine>` (annotation)   | `ApplicationInstance`              | Required only for instances that support Backup |
| `ops.vworkspace.io/restore=<engine>` (annotation)  | `ApplicationInstance`              | Required only for instances that support Restore |
| `ops.vworkspace.io/upgrade=<engine>` (annotation)  | `ApplicationInstance`              | Required only for instances that support Upgrade |
| `ops.vworkspace.io/quiesce=<engine>` (annotation)  | `ApplicationInstance`              | Optional  |

The set is deliberately small. Anything not on this list is either not part of the operator's API or is operator-internal and may change.
