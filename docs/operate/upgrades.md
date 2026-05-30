# Upgrading the operator

**Status:** Alpha — APIs are at `v1alpha1` and may evolve.
**Last Updated:** 2026-05-30

This document covers upgrading the operator itself. Application upgrades — bumping `ApplicationInstance.spec.chart.version` — are a different topic and are documented in [../operations/upgrades-and-migrations.md](../operations/upgrades-and-migrations.md).

The operator is itself a workload, and it ships through the same machinery as the applications it manages. Each cluster runs one operator version; Odoo's Cluster Registry records which version that is. The upgrade is a Helm upgrade of the bundle's chart (operator + bundled controllers), reconciled by Flux on the cluster. Failure recovery is Flux rollback. CRD evolution goes through a conversion webhook and a deprecation window.

## Channels

The operator publishes three channels:

| Channel    | Audience                                                                                                      | Update cadence                                                                  |
|------------|---------------------------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------|
| `stable`    | The default. Production clusters; clusters that prioritize predictability over new features.                  | Slow. Promoted from `candidate` after a soak period and per the release process. |
| `candidate` | Pre-production staging clusters; operators willing to run weeks ahead of stable.                              | Every few weeks. Subject to revert if a regression is found.                     |
| `edge`      | Development clusters; the project's own CI; contributors testing pre-release behavior.                        | On merge to `main` (approximately).                                              |

Channels are properties of the Helm chart repository: each chart version is tagged with the channel it belongs to. A cluster on the `stable` channel sees stable chart versions only; a cluster on `edge` sees every published version. The chart's `Chart.yaml` includes a `vworkspace.io/channel` annotation that the operator surfaces in `Cluster.status.operatorChannel`.

The default channel for a newly registered cluster is `stable`. Switching channels is a deliberate operator decision (Odoo Cluster Registry → cluster → change channel), not a chart bump.

## Per-cluster version pinning

A cluster can pin to a specific version (`v1.4.2`) instead of following a channel:

```yaml
apiVersion: ops.vworkspace.io/v1alpha1
kind: Cluster
metadata:
  name: cluster-prod-1
  namespace: vworkspace-system
spec:
  upgrade:
    channel: stable
    pinnedVersion: v1.4.2     # takes precedence over channel
```

When `pinnedVersion` is set, Odoo and the cluster do not auto-upgrade across the channel; chart bumps within the same pinned version (patches, e.g., `v1.4.2-hotfix.1`) are still applied if Odoo publishes one. Pinning is the way to hold a cluster while a known issue is investigated, while a manual maintenance window is scheduled, or while a regulated change-control process is in flight.

Unpinning is symmetric: clear `pinnedVersion`, and the cluster follows the channel again at the next reconcile.

## Staged rollouts

A platform team that manages many clusters typically does not roll a new version to all of them simultaneously. Odoo's Cluster Registry supports staged rollouts as a fleet-level concern:

- **Waves.** Clusters belong to a wave (`dev`, `internal`, `customer-A`, `customer-B`, ...). A new chart version is promoted wave by wave on a schedule the platform team controls.
- **Burn-in.** A wave can specify a burn-in window (e.g., 24 hours on `dev` before promotion to `internal`). The Cluster Registry tracks the in-wave deployment time and refuses to promote earlier than the window allows.
- **Per-wave version pinning.** A wave can be pinned to a specific version while the rest of the fleet has moved on. This is useful when a customer-facing cluster needs to stay on an older version while an internal one tests a candidate.

The waves are an control-plane-side scheduling concern; the operator simply consumes the version it is told to run. From the cluster's perspective, the chart version changes when the control plane writes a new value into Flux's `HelmRelease` for the operator's bundle (Pull mode) or commits it to the watched Git repo (GitOps mode) or applies it directly (Push mode).

## The upgrade itself

Operator upgrades use the same Helm path that applications use. The operator's own Helm chart is reconciled by Flux on the cluster; chart version changes are applied by Flux's Helm Controller, just like any other chart. Concretely:

1. The cluster's Flux instance reconciles a `HelmRelease` named `vworkspace-app-operator` in the `flux-system` (or `vworkspace-system`) namespace. The `HelmRelease.spec.chart.spec.version` is the version the cluster is asked to run.
2. Odoo (or a human running `helm upgrade` in Push mode) updates that field to the new version.
3. Flux fetches the new chart, runs Helm's upgrade, the operator's deployment is rolled, and Flux observes the new revision.
4. The new operator pod starts, runs its self-checks (CRDs present, RBAC present, controllers reachable), and reports its new version on `Cluster.status.operatorVersion`. The audit event `OperatorUpgraded` is emitted.

A failed upgrade triggers Flux's remediation (`spec.upgrade.remediation.remediateLastFailure: true`), which rolls the chart back to the previous revision. The operator's status reflects the rollback; the audit event `OperatorUpgradeFailed` carries the failure reason and the rollback outcome.

## CRD compatibility window

Breaking CRD changes (a renamed field, a moved subresource, a removed value enum) go through a conversion webhook and a deprecation window:

| Step                                                  | Duration                                | Behavior                                                                                                                                                       |
|-------------------------------------------------------|-----------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Introduce the new API version (e.g., `v1beta1`).      | n/a                                     | The old version (`v1alpha1`) remains served; the new is introduced as `served: true, storage: false`.                                                          |
| Run both versions in parallel.                         | At least one minor release.             | The conversion webhook handles requests in either version. Catalog tooling and the operator both write the new version internally; clients can still read the old. |
| Promote the new version to `storage: true`.            | After at least one minor release.       | New stored objects are in the new version. The webhook converts existing objects on read until the next reconcile rewrites them.                                |
| Mark the old version `deprecated`.                     | n/a                                     | The API server logs a warning header on writes to the old version; the operator emits an audit event `CRDVersionDeprecated`.                                    |
| Remove the old version.                                | At least one minor release after deprecation. | Old version is no longer served; clients on the old version must upgrade.                                                                                       |

In practice, the operator's CRDs follow the same skew policy as Kubernetes' own: any operator version in the supported window can read any CR within the same API version. The supported window is "current and previous minor"; the bundle's release notes call out CRD changes explicitly.

The conversion webhook is hosted by the operator and is itself part of the bundle. It uses the operator's TLS material; conversion failures are visible in `Cluster.status.conditions[CRDsRegistered]` and in the operator's metrics (`controller_runtime_webhook_requests_total{webhook="conversion"}`).

## Failure recovery via Flux

Flux rollback handles the common cases:

- The new operator pod fails to become ready (`/readyz` returns 503 forever). Flux detects the failure, rolls the `HelmRelease` back to the previous revision, the old operator pod resumes. Audit event: `OperatorUpgradeFailed`.
- The new operator's CRDs are incompatible (the conversion webhook is missing or broken). The operator rejects existing CRs on admission; Flux observes the failure via `/readyz` and rolls back.
- The new bundled controllers (Flux, Velero, cert-manager, external-secrets) fail to start. The Helm upgrade reports `Reconciled=False`; Flux rolls back.

Cases Flux rollback does not handle automatically (these need human intervention):

- An upgrade that succeeded in installing but introduced a runtime bug visible only after some time. The audit stream surfaces the symptom; an operator manually edits the `HelmRelease.spec.chart.spec.version` back to the previous version.
- An upgrade that was applied while a destructive `Operation` was in flight. The bundle's release notes call out forbidden sequences; the admission webhook also blocks operator upgrades while an `Operation` of `type: Migration` or `type: Restore` is `Running`.

## Compatibility matrix

The release notes for each operator version include a compatibility matrix. The template:

| Operator version | CRD versions served | Min Kubernetes | Tested distros               | Min Flux | Min Velero | Min cert-manager | Min external-secrets |
|------------------|---------------------|----------------|------------------------------|----------|------------|------------------|----------------------|
| v0.4.2           | v1alpha1            | 1.28           | k3s 1.30, Talos 1.30, EKS 1.30 | v2.3    | v1.14      | v1.15            | v0.10                |
| v0.5.0           | v1alpha1, v1beta1   | 1.28           | k3s 1.30, Talos 1.30, EKS 1.30, AKS 1.30 | v2.3    | v1.14      | v1.15            | v0.10                |

The numbers above are illustrative; the canonical matrix lives in `CHANGELOG.md` for each release. The compatibility matrix is the contract Odoo's Cluster Registry uses to decide whether a particular chart version is admissible for a particular cluster (Kubernetes version, distro, existing controller versions).

## How to upgrade in practice

For a single cluster, the upgrade is:

1. Verify the cluster is healthy (`Cluster.status.conditions[Connected,Authenticated,ControllersHealthy] = True`). Do not upgrade an unhealthy cluster.
2. (Optional) Pin to the current version first (`spec.upgrade.pinnedVersion: <current>`), test the upgrade in dev, then unpin.
3. From Odoo (Cluster Registry → cluster → change version), or directly in the cluster (`helm upgrade vworkspace-app-operator oci://... --version <new>`), bump the version.
4. Watch `Cluster.status.operatorVersion` reflect the new value; the rollout finishes within a few minutes.
5. Confirm `Cluster.status.conditions[Connected,Authenticated,ControllersHealthy] = True`. If any flips, investigate via [troubleshooting.md](troubleshooting.md).

For a fleet, the same procedure happens per wave: dev clusters first, internal next, customer-facing last, with the burn-in windows the Cluster Registry enforces.

## Related material

- [observability.md](observability.md) — Metrics and audit events that confirm the upgrade is healthy.
- [troubleshooting.md](troubleshooting.md) — What to do when an upgrade leaves a condition unsatisfied.
- [../operations/upgrades-and-migrations.md](../operations/upgrades-and-migrations.md) — Application upgrades (a different topic).
- [../development/release-process.md](../development/release-process.md) — How the channels are published and the matrix is maintained.
