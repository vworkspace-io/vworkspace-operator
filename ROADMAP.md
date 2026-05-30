# Roadmap

**Last Updated:** 2026-05-30
**Audience:** Maintainers, contributors, operators planning to adopt vworkspace-operator.
**Scope:** This document covers the operator only. For the broader product roadmap, see the parent project's [ROADMAP.md](https://github.com/vworkspace-io/vworkspace/blob/main/docs/ROADMAP.md).

## Principles

The operator is built incrementally. We would rather ship a small, predictable surface that works reliably than a large surface that mostly works. Phases below describe the intended shape; exact dates are honest estimates and will move.

## Phase 0 — Scaffold (Q2 2026, done)

- Documentation set under [docs/](docs/README.md): concepts, connectivity, API reference, operations, security, install, operate, development.
- Project governance: [LICENSE](LICENSE) (AGPL-3.0), [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md), [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md), [GOVERNANCE.md](GOVERNANCE.md), [MAINTAINERS.md](MAINTAINERS.md), [SUPPORT.md](SUPPORT.md), [CHANGELOG.md](CHANGELOG.md).
- ADRs for the irreversible early decisions: Helm-first via Flux HelmRelease, Pull mode as default, two CRDs only, one operator per cluster. See [docs/adr/](docs/adr/README.md).
- Issue and pull request templates under [.github/](.github/).

## Phase 1 — MVP, `v0.0.x` (Q3 2026, in progress)

The first working operator. Tagged as `v0.0.x` pre-releases; not yet recommended for production use.

- [x] Kubebuilder skeleton with controller-runtime; layout in [docs/development/project-layout.md](docs/development/project-layout.md).
- [x] `ApplicationInstance` (`apps.vworkspace.io/v1alpha1`) and `Operation` (`ops.vworkspace.io/v1alpha1`) CRDs. See [docs/api/](docs/api/README.md). `Cluster` CR added for connectivity status.
- [x] Flux `HelmRelease` as the Helm engine (MVP adapter). `ApplicationInstance` reconciled into `HelmRelease`.
- [x] Two operation engines: `helm` (chart version / values bump) and `velero` (namespace-scoped backup and restore stubs).
- [~] Pull-mode transport: HTTP client and heartbeat; job applier loop not complete. See [docs/connectivity/pull-mode.md](docs/connectivity/pull-mode.md).
- [ ] Push-mode transport for in-cluster vWorkspace Server installs.
- [~] Minimum RBAC generated; namespace isolation enforcement via webhook still pending.
- [ ] Bootstrap on k3s, Talos, and single-node Docker documented end-to-end in [docs/install/cluster-bootstrap.md](docs/install/cluster-bootstrap.md).

Continuations tracked in [docs/development/IMPLEMENTATION_GUIDE.md](docs/development/IMPLEMENTATION_GUIDE.md).

Exit criteria: A user can install the operator on a fresh k3s cluster, register it with a vWorkspace Server instance, deploy Nextcloud through the control plane, take a Velero backup, and restore it, all via the documented commands.

## Phase 2 — Beta, `v0.1.x` (Q4 2026)

Operational maturity. Suitable for early adopters who can tolerate API evolution.

- Operation templates and capability metadata model. See [docs/operations/operation-templates.md](docs/operations/operation-templates.md).
- Admission webhook that gates `Operation.spec.type` per namespace and enforces concurrency rules (no restore during upgrade; no two upgrades on the same release).
- Cluster identity and registration tokens, with rotation. See [docs/security/authentication.md](docs/security/authentication.md).
- Full observability surface: Prometheus metrics, structured JSON logs with the documented fields, Kubernetes events on every condition transition, audit events posted back to the control plane audit log. See [docs/operate/observability.md](docs/operate/observability.md).
- Conversion webhook scaffolding for forthcoming CRD evolution.

Exit criteria: Multiple operators are running stably on independent clusters from a single vWorkspace Server install, with full audit and observability.

## Phase 3 — Public release, `v0.2` (Q1 2027)

Aligned with the parent project's first public release.

- Argo Workflows engine for multi-step operations (e.g., quiesce → snapshot → verify → unquiesce). See [docs/operations/engines/argo-workflows.md](docs/operations/engines/argo-workflows.md).
- CSI snapshot and VolSync engines for storage-centric backup and replication. See [docs/operations/engines/csi-snapshots-volsync.md](docs/operations/engines/csi-snapshots-volsync.md).
- mTLS and signed payloads for Pull mode.
- Per-cluster operator version pinning, channel subscription (`stable`, `candidate`, `edge`), and staged rollouts coordinated from vWorkspace Server. See [docs/operate/upgrades.md](docs/operate/upgrades.md).
- GitOps adapter (Flux Kustomize or Argo CD `Application`) for orgs that require Git-mediated change control.

Exit criteria: First tagged public release with signed images, signed Helm chart (OCI), and a published security disclosure policy.

## Phase 4 — `v1.0` (Q2 2027 and later)

API stability.

- Promote CRDs to `v1` with conversion webhooks from `v1alpha1`. Documented deprecation window of at least one minor release.
- Argo CD `Application` adapter as a full peer to Flux.
- Policy controls in the admission webhook: allowed chart sources, version constraints, maintenance windows, mandatory approvals for high-risk operations.
- Full upgrade matrix tested in CI.

Exit criteria: Tagged `v1.0.0` with an explicit API stability commitment matching the project's [governance](GOVERNANCE.md) and [security policy](SECURITY.md).

## Out of scope (for now)

- Reimplementation of any chart's internal logic. We do not own Nextcloud's upgrade path; the chart does.
- A bespoke Helm engine. We use Flux Helm Controller by default and may add an Argo CD adapter; we will not write our own.
- Cluster provisioning. The operator runs on a cluster; it does not create the cluster.

## Related

- Parent project roadmap: [vworkspace ROADMAP.md](https://github.com/vworkspace-io/vworkspace/blob/main/docs/ROADMAP.md).
- Source-of-truth design document: [ODOO_K8S_APPLICATION_MANAGER_OPERATOR.md](https://github.com/vworkspace-io/vworkspace/blob/main/docs/technical/ODOO_K8S_APPLICATION_MANAGER_OPERATOR.md).
