# vworkspace-operator

> Status: Alpha — APIs may change. The CRDs are at `v1alpha1` and the project is pre-1.0. Pin versions in production and read the [CHANGELOG](CHANGELOG.md) before upgrading.

`vworkspace-operator` is the cluster-side Kubernetes operator that an Odoo-based [vWorkspace](https://github.com/vworkspace-io/vworkspace) control plane drives to install, upgrade, back up, and otherwise operate applications running in a Kubernetes cluster. It is Helm-first: every application is deployed through an upstream Helm chart, reconciled by [Flux Helm Controller](https://fluxcd.io) by default, and described in the cluster by a small pair of custom resources — `ApplicationInstance` and `Operation`. Day-2 work (backups, restores, upgrades, migrations) is modeled as Kubernetes resources reconciled by the operator and proven third-party controllers (Velero, Argo Workflows, CSI snapshot controller, VolSync), never as imperative scripts on the Odoo side.

It is intended for the same audience as the rest of the vWorkspace project: self-hosting organizations, schools, clinics, NGOs, small businesses, homelab operators, and the platform engineers tired of stitching together one-off install scripts. The operator is the piece that lives in your cluster, owns its own reconciliation loop, and keeps your applications healthy whether Odoo is reachable today or not.

## Who this is for

- Operators running their own Kubernetes cluster (k3s, Talos, Harvester, managed K8s, or a single-node Docker host with embedded k3s) who want a coherent, AGPL-licensed control plane for their workloads.
- vWorkspace deployments that need to manage one or more clusters from a single Odoo install without exposing the cluster API to the public internet and without Odoo holding a kubeconfig.
- Platform teams that want a small, predictable CRD surface (`ApplicationInstance`, `Operation`) instead of dozens of bespoke per-app controllers.

## How it relates to vWorkspace

The vWorkspace control plane (an Odoo 19 application) is the place a human or the AI assistant declares intent: "deploy Nextcloud here", "back this up", "upgrade that". `vworkspace-operator` is what makes those declarations real on a specific cluster. The two layers talk over a deliberately narrow contract — described in [docs/connectivity/](docs/connectivity/) — that supports three modes:

- **Pull** (default). The cluster initiates outbound HTTPS to Odoo, fetches jobs, applies them locally, and reports status. No inbound ports, no kubeconfig held by Odoo. The right choice for self-hosted clusters behind NAT or a firewall, regulated edges, and multi-tenant SaaS topologies.
- **Push**. Odoo writes the operator's CRDs to the cluster API directly via server-side apply. The simplest choice when Odoo and the cluster share a trusted network — most commonly when Odoo is installed *into* the cluster it manages.
- **GitOps**. Odoo commits the operator's CRDs to a Git repository; Flux or Argo CD on the cluster syncs them. The right choice when change control must flow through Git.

The same CRDs and the same in-cluster reconciliation loop are used in every mode; only the transport changes.

## Feature highlights

- **Helm-first.** Application deployment logic stays in upstream Helm charts. The operator does not re-implement chart internals.
- **Two CRDs, not twenty.** `ApplicationInstance` describes desired application state; `Operation` describes a day-2 action against an application instance. RBAC enforces what kinds of operations are allowed per namespace.
- **Pull connectivity by default.** Designed for clusters that should never need to expose the K8s API. Push and GitOps adapters are supported.
- **Day-2 operations as first-class resources.** Backups (Velero, CSI snapshots, VolSync), upgrades, migrations, and runbooks are reconciled, audited, and reportable.
- **One operator per cluster.** Blast radius bounded by design. An Odoo outage does not take down running applications; a cluster outage does not affect other clusters.
- **Cluster-local audit and observability.** Prometheus metrics, structured JSON logs, Kubernetes events on every condition transition, and an audit stream back to Odoo's Discuss channel.

## Quick install

See [docs/install/quickstart.md](docs/install/quickstart.md) for the supported install path. The short version, once the project has its first tagged release, will look like a single Helm install plus a one-line cluster-registration step that exchanges a one-time token for a long-lived bootstrap credential. The bundle installs the operator, Flux Helm Controller, cert-manager, external-secrets, and Velero on a stock Kubernetes cluster.

## Documentation

Detailed documentation lives under [docs/](docs/README.md):

- [Concepts](docs/concepts/README.md) — what the operator does and why.
- [Connectivity](docs/connectivity/README.md) — Push, Pull, GitOps in depth.
- [API reference](docs/api/README.md) — full spec/status reference for both CRDs.
- [Operations](docs/operations/README.md) — operation templates and engines.
- [Security](docs/security/README.md) — RBAC, secrets, authentication, threat model.
- [Install](docs/install/README.md) — prerequisites, quickstart, cluster bootstrap, air-gapped.
- [Operate](docs/operate/README.md) — observability, upgrades, troubleshooting, runbooks.
- [Development](docs/development/README.md) — build, test, release, project layout.
- [ADRs](docs/adr/README.md) — recorded architecture decisions.

## Project status

This is an early-stage open-source project. The CRDs are at `v1alpha1`; the API may evolve, but breaking changes will go through a conversion webhook and a deprecation window of at least one minor release (see [docs/operate/upgrades.md](docs/operate/upgrades.md)). The first public release is targeted for Q1 2027 in line with the parent project's [ROADMAP](https://github.com/vworkspace-io/vworkspace/blob/main/docs/ROADMAP.md); see this repository's [ROADMAP.md](ROADMAP.md) for operator-specific milestones.

## License

`vworkspace-operator` is licensed under the GNU Affero General Public License v3.0. The full text is in [LICENSE](LICENSE). The license choice is consistent with the umbrella vWorkspace project and is intended to protect operators and the community from closed-source repackaging.

## Community

- [Code of Conduct](CODE_OF_CONDUCT.md) — Contributor Covenant v2.1.
- [Contributing](CONTRIBUTING.md) — how to file issues, propose changes, and get a PR merged.
- [Security policy](SECURITY.md) — how to report vulnerabilities privately.
- [Governance](GOVERNANCE.md) — how decisions are made.
- [Support](SUPPORT.md) — where to get help.

## Upstream

This repository is a sub-repository of the vWorkspace project. The upstream parent project is at [https://github.com/vworkspace-io/vworkspace](https://github.com/vworkspace-io/vworkspace) (placeholder organization `vworkspace-io`). See the parent project's [PRODUCT_VISION](https://github.com/vworkspace-io/vworkspace/blob/main/docs/PRODUCT_VISION.md) for the broader product framing.
