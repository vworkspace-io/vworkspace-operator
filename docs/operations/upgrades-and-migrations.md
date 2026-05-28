# Upgrades and migrations

**Status:** Alpha
**Last Updated:** 2026-05-28

This document explains how an `ApplicationInstance` is upgraded, when an upgrade needs to be wrapped in an `Operation` of `type: Migration`, how rollback works, and how the catalog's forbidden-version policy is enforced. The engine-level references are in [engines/helm-hooks.md](engines/helm-hooks.md) (chart hooks during upgrade), [engines/argo-workflows.md](engines/argo-workflows.md) (multi-step migrations), and [engines/velero.md](engines/velero.md) (pre-upgrade backup).

The principle is the same as for backup: the operator orchestrates and reports; the chart and the Flux Helm Controller execute. The operator's job is to make the right `HelmRelease` desired-state change appear, observe the chart's reconcile, and surface success or failure on `ApplicationInstance.status`. Most upgrades are exactly that. Migrations exist for the cases where a chart bump is not enough.

## The default path: bump `chart.version`

For the overwhelming majority of upgrades, the only change is the chart version on the `ApplicationInstance`:

```yaml
apiVersion: apps.vworkspace.io/v1alpha1
kind: ApplicationInstance
metadata:
  name: nextcloud-myteam
  namespace: org-myteam
spec:
  chart:
    sourceType: oci
    url: oci://registry.example.com/charts
    name: nextcloud
    version: "7.0.0"   # was "6.6.0"
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

When this CR is applied (in any connectivity mode), the operator:

1. Records the new `spec.generation` and sets `Reconciling=True`.
2. Updates the underlying `HelmRelease.spec.chart.spec.version` to `7.0.0` via server-side apply.
3. The Flux Helm Controller reconciles the `HelmRelease`: it fetches the new chart, runs Helm's upgrade, executes the chart's own `pre-upgrade` and `post-upgrade` hooks (which are how Nextcloud, Mattermost, OnlyOffice, Vaultwarden, and most production-grade charts implement their migrations), and writes the result to `HelmRelease.status.conditions`.
4. The operator reads `HelmRelease.status`, maps it onto `ApplicationInstance.status.conditions` (`Reconciling=True` → `Ready=False/Upgrading` → `Ready=True/Upgraded`), and emits Kubernetes events on every transition.

This path does not need an `Operation`. The chart-version field on the `ApplicationInstance` is the upgrade request; everything that happens downstream is the Helm Controller's job. The status contract is the same one used for any other reconcile.

### Values changes during upgrade

`spec.values` changes are reconciled the same way: a values change updates `HelmRelease.spec.values` (or the referenced Secret / ConfigMap), Flux re-renders, and the chart's normal lifecycle runs. The operator does not differentiate "chart bump" from "values change" in its reconcile loop; both are just generations of the same desired-state CR.

## When to use `Operation` of `type: Migration`

The default path is right when the chart's own pre/post-upgrade hooks are enough. Use an `Operation` of `type: Migration` when one or more of the following is true:

- The upgrade requires a **multi-step preflight or postflight** outside the chart's hook scope: a CSI snapshot of the data PV before the chart bump, a verification step that hits an external endpoint, an explicit unquiesce, a coordinated change in another namespace.
- The upgrade requires **rollback automation tied to a verification step**: "if the post-upgrade verify fails, restore from the snapshot taken before the bump".
- The upgrade requires an **approval gate or maintenance window** that should be visible in the API as a `Blocked=True/OutsideMaintenanceWindow` condition rather than as a wait inside a chart hook.
- The upgrade is **destructive or one-way** (a database engine swap, a chart that changes its persistent-volume layout) and the operator wants the operation to be visible as a distinct CR with a finite outcome.

The migration in [engines/argo-workflows.md](engines/argo-workflows.md) is the canonical example: prechecks → quiesce → snapshot → migrate (bump `chart.version`) → verify → unquiesce, with rollback-from-snapshot if verify fails. The `Operation` carries the run; the `ApplicationInstance.spec.chart.version` only changes when the workflow's `bump-chart-version` step runs.

Note the inversion of who modifies the `ApplicationInstance`: in the default path, a human or Odoo applies the new version directly; in the migration path, the workflow modifies the `ApplicationInstance` partway through its DAG, and the operator's normal reconcile picks up the change. This is intentional. The `Operation` does not bypass the operator's reconcile; it sequences it inside a larger plan.

## Rolling back via Flux

When an upgrade fails in the default path, the Flux Helm Controller can be told to roll back to the previous release revision. Flux exposes this via the `HelmRelease`'s `spec.upgrade.remediation` and `spec.upgrade.rollback` settings, which the operator configures on every materialized `HelmRelease`:

```yaml
spec:
  upgrade:
    remediation:
      retries: 1
      remediateLastFailure: true
    cleanupOnFail: true
  rollback:
    timeout: 5m
    cleanupOnFail: true
    recreate: false
```

The flow:

1. Flux applies the upgrade. The chart's `pre-upgrade` hook runs. If it fails, the upgrade fails.
2. With `remediateLastFailure: true`, Flux automatically rolls the release back to the previous successful revision. The chart's `post-rollback` hook (if any) runs.
3. The operator reads the `HelmRelease.status` change, sets `ApplicationInstance.status.conditions[Ready]=True/RolledBack` and `Degraded=True/UpgradeFailed`. The Kubernetes event log records "UpgradeFailed; RolledBack to revision N-1".
4. The `ApplicationInstance.spec.chart.version` is still the new version — the desired state has not changed; Flux has reconciled the actual state back to the previous revision. The operator's `Degraded=True` condition is the signal that the human (or AI assistant in Odoo) should reduce `spec.chart.version` back to the old value before trying again, or fix whatever made the upgrade fail.

For migrations driven by an `Operation` workflow, the workflow's rollback step is the rollback path — typically restoring from the pre-migrate VolumeSnapshot — and the operator marks the `Operation` as `Failed=True/RolledBack` rather than `Succeeded`.

Manual rollback by editing `ApplicationInstance.spec.chart.version` back to the previous value works too; Flux treats it as just another upgrade.

## Forbidden-version policy

Some chart versions are known-bad in vWorkspace's testing matrix (corrupted releases, regressions caught after publication, charts whose dependencies pin incompatible images). The Odoo catalog publishes a "forbidden versions" list per chart, and the operator enforces it via its validating admission webhook.

The enforcement model:

| Catalog signal                                | Webhook behavior                                                                                                                                                |
|-----------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Version on the forbidden list                  | Reject the create/update of the `ApplicationInstance` with reason `ChartVersionForbidden` and an explanatory message pointing at the catalog note.              |
| Version on the deprecated list, not forbidden  | Admit, but set `Degraded=True/ChartVersionDeprecated` on the next reconcile. The Odoo audit stream records the deprecation warning.                              |
| Version off the catalog's allowed range entirely | Reject with reason `ChartVersionOutsideAllowedRange`. Catalogs can set min/max constraints (`>=6.5.0, <7.0.0`).                                                  |
| Version on the recommended list                | Admit; no annotation.                                                                                                                                            |
| Catalog unreachable at admission time          | Admit (fail-open) and set `Blocked=True/CatalogUnreachable` on the next reconcile. The operator does not block app upgrades on Odoo availability.                |

The forbidden-version list is delivered to the operator as part of the catalog payload (in Pull mode), as catalog data that Odoo pushes alongside the `ApplicationInstance` CR (Push mode), or as a versioned manifest in the watched Git repo (GitOps mode). The cluster caches the list and re-evaluates on every admission; the list does not need to be checked over the network at admission time, which keeps the webhook latency bounded and the cluster reconciling under network partition.

Bypass: an admin can force a forbidden version by setting the annotation `apps.vworkspace.io/override-forbidden: "true"` on the `ApplicationInstance` along with a justification annotation. The webhook still admits, but the operator emits a high-severity audit event to Odoo and sets `Degraded=True/ForbiddenVersionOverride`. This is a deliberate safety valve, not the recommended path.

## Practical notes

- **The operator does not auto-upgrade applications.** A chart bump only happens when the `ApplicationInstance.spec.chart.version` changes. The catalog publishes "this version is now recommended"; deciding when to bump is the operator's (or the AI assistant's, with confirmation) call. Auto-upgrade may be revisited later as an opt-in catalog property.
- **Maintenance windows** are enforced on `Operation` resources (migrations, run-commands) but not on plain `ApplicationInstance` edits. If you need a maintenance window for a plain chart bump, use the `Operation` `type: Migration` path; otherwise the chart bump is reconciled as soon as the operator sees it.
- **Pre-upgrade backups** are recommended for production data-bearing applications. The catalog can express this as "Backup operation is suggested before an Upgrade operation on this application class". The Odoo control plane surfaces the suggestion; the operator does not refuse the upgrade if no recent Backup exists.
- **Helm rollback limits.** Flux can roll back as long as the previous release revision is still in Helm's history. Aggressive `historyMax` settings on the chart can truncate that history; the operator's `HelmRelease` defaults set `historyMax: 10`, which is a reasonable balance between storage and useful history.

## Related material

- [engines/helm-hooks.md](engines/helm-hooks.md) — How chart-provided hooks are invoked and observed.
- [engines/argo-workflows.md](engines/argo-workflows.md) — Multi-step migration workflows.
- [engines/velero.md](engines/velero.md) — Pre-upgrade backups.
- [../api/application-instance.md](../api/application-instance.md) — `ApplicationInstance` spec and status.
- [../operate/upgrades.md](../operate/upgrades.md) — Upgrading the operator itself (a different topic; the chart-version field there is the operator's own).
