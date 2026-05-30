# 0002 — Helm-first via Flux HelmRelease

**Status:** Accepted (2026-05-28)

## Context

`vworkspace-operator` is a Kubernetes operator whose job is to deploy and operate applications on a cluster on behalf of an vWorkspace Server control plane. The applications in vWorkspace's catalog are complex: Nextcloud has its own pre- and post-upgrade hooks for occ migrations; OnlyOffice integration is encoded in chart values; Mattermost migrations are baked into chart hooks. These applications are not new; they have been deployed by hand and by automation for years, and the upstream charts have absorbed years of operational learning.

The operator must put these applications on clusters and operate them through their lifecycle (install, upgrade, rollback, drift remediation, status reporting). Three options were considered:

1. **Embed Helm directly.** The operator runs `helm upgrade --install` (via the Helm SDK or an embedded helm binary). The operator owns release history, drift detection, rollback semantics, retry policy, chart-source authentication, and upgrade windows.
2. **Delegate to an existing Helm controller.** The operator materializes the controller's CRD (Flux's `HelmRelease`, Argo CD's `Application`, or both) and reads its status. The controller owns the Helm lifecycle; the operator owns orchestration.
3. **Hybrid.** Default to a Helm controller; allow direct Helm in narrow cases (air-gapped edge clusters with no controller).

Option 1 is the easiest to start. It is also the most expensive over time: every problem the existing Helm controllers solved (concurrent reconciles, chart-source auth, history truncation, status conditions, GitOps compatibility) re-emerges as code in the operator. The operator's surface area grows; the team becomes a Helm-engine team in addition to an orchestration team.

Option 3 sounds flexible but commits us to maintaining two execution paths and testing each.

Option 2 is the right shape: outsource Helm reconciliation to a controller that already does it well, and keep the operator focused on orchestration and day-2 operations. Within option 2, two candidates exist:

- **Flux Helm Controller (`HelmRelease`).** Purpose-built for Helm reconciliation. Native condition contract. Native drift remediation. Well-understood rollback semantics. Strong ecosystem fit (the wider Flux ecosystem includes Source Controller, Kustomize Controller, image automation, etc.).
- **Argo CD (`Application`).** Mature UI, strong adoption among teams already standardized on Argo CD, Git-first design that pairs naturally with GitOps.

Argo CD's strongest path is "the Git repo is the desired state"; values overrides via `Application` and day-2 operations on Argo Applications require more conventions. The operator's intent — an `ApplicationInstance` CRD whose `spec.values` carries chart values inline, via Secret, or via ConfigMap — fits Flux's model directly. The operator would have to bridge two control planes (its own CRDs and Argo's `Application`) to use Argo as the primary engine; bridging is doable, but it is rework.

Importantly, the operator's `ApplicationInstance` does not require a particular engine: the `helmengine.Engine` interface in `internal/helmengine/` is a stable internal boundary. Choosing Flux as the default does not foreclose Argo CD; it sequences the work.

## Decision

We will use **Flux Helm Controller `HelmRelease`** as the default Helm engine for `vworkspace-operator`. The operator's `ApplicationInstance` reconciler materializes a `HelmRelease` (and the `HelmRepository` or `OCIRepository` that backs it). Flux reconciles the Helm lifecycle. The operator observes the `HelmRelease.status.conditions` and maps them onto `ApplicationInstance.status.conditions` per the conditions contract in [../api/conditions.md](../api/conditions.md).

The operator does not embed Helm itself. There is no `helm upgrade --install` codepath in the operator's binary. The operator does not duplicate Flux's drift remediation, rollback, or release-history logic.

The operator's bundle installs Flux's Helm Controller and Source Controller as part of the default install ([../install/quickstart.md](../install/quickstart.md)). Operators who already run Flux can opt out of the bundle's Flux install via chart values.

We will support **Argo CD `Application` as an adapter** in a future milestone, behind the same `helmengine.Engine` interface. The adapter lives under `internal/helmengine/argocd/` (see [../development/project-layout.md](../development/project-layout.md)). Adopting Argo as the engine is a per-cluster choice; the operator's CRDs and reconciler do not change.

We **rule out the "operator embeds Helm" option.** Even in air-gapped or edge clusters, the bundle's Flux install is small (two controllers, a few hundred MiB of memory) and is worth the dependency.

## Consequences

**Operator surface area stays small.** The operator does not own a Helm-engine implementation. Bug fixes to Helm's lifecycle land in Flux, not here. We can focus on the operator's actual job: CRD orchestration, day-2 operations, status mapping, and the Odoo connectivity loop.

**Flux is a hard dependency.** Every cluster the operator runs on needs Flux's Helm Controller and Source Controller. The bundle includes them, so for most operators this is invisible; for operators who run a separate Flux install, the operator works against it without duplication.

**A clear ownership boundary.** Flux owns the `HelmRelease`; the operator owns the `ApplicationInstance` and the chart-version field on it. The interface between the two is the `helm.toolkit.fluxcd.io/v2` CRD set. If Flux upstream changes that CRD, the operator follows; the operator does not own the contract.

**Status mapping discipline.** The `ApplicationInstance.status.conditions` reasons mirror Flux's `HelmRelease.status.conditions` reasons verbatim where there is a direct correspondence (`InstallFailed`, `UpgradeFailed`, `ReconciliationFailed`). This keeps the operator honest: it does not invent a new vocabulary on top of Flux's; it relays Flux's.

**Future-proof for Argo CD.** The `helmengine.Engine` interface preserves the option to add an Argo CD adapter without changing the operator's CRDs or reconciler. The adapter is later work; it is not on the critical path for v1.

**A new ADR will be required to switch defaults.** Flux is the default for the foreseeable future. If the project ever decides to make Argo CD the default — for example, because the user base shifts heavily Argo-ward — that decision lands as a new ADR superseding this one.

**Helm-first is now real.** The architectural principle "the operator does not re-implement chart internals" has a concrete enforcement mechanism: there is no chart-internal codepath in the operator. Anyone proposing to add one is proposing to violate this ADR; the PR description must explain why the new path cannot be in the chart instead.
