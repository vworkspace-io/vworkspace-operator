# vworkspace-operator documentation

**Status:** Alpha — APIs are at `v1alpha1` and may evolve.  
**Last Updated:** 2026-05-30

This directory is the canonical documentation for **vworkspace-operator**, the cluster-side Kubernetes operator driven by **[vWorkspace Server](https://github.com/vworkspace-io/vworkspace-server)** (the control plane). The repository [README](../README.md) is the short orientation; this index is suitable for browsing on GitHub or as the nav source for [GitHub Pages](publication.md).

## How to read these docs

Start at [concepts/overview.md](concepts/overview.md) to understand the three layers: control plane intent, operator orchestration, and third-party controllers. Then either:

- **Try-it path** — [install/quickstart.md](install/quickstart.md) → register the cluster → read connectivity docs; or
- **Architecture-first path** — [connectivity/modes-overview.md](connectivity/modes-overview.md) → [api/README.md](api/README.md).

Both paths converge on the same contract: `ApplicationInstance` and `Operation` CRDs, one operator per cluster.

## Concepts

- [concepts/overview.md](concepts/overview.md) — Goals, non-goals, three-layer model
- [concepts/architecture.md](concepts/architecture.md) — Components and data flow
- [concepts/crds.md](concepts/crds.md) — Guided tour of the CRDs
- [concepts/reconciliation-model.md](concepts/reconciliation-model.md) — Reconciliation loops
- [concepts/helm-first.md](concepts/helm-first.md) — Helm-first and Flux default
- [concepts/day-2-operations.md](concepts/day-2-operations.md) — Backups, upgrades, runbooks
- [concepts/multi-cluster-and-multi-org.md](concepts/multi-cluster-and-multi-org.md) — Isolation model
- [concepts/glossary.md](concepts/glossary.md) — Terminology

## Connectivity

- [connectivity/README.md](connectivity/README.md) — Index (Pull default, Push, GitOps)
- [connectivity/modes-overview.md](connectivity/modes-overview.md) — Mode comparison
- [connectivity/pull-mode.md](connectivity/pull-mode.md) — Default outbound Pull mode
- [connectivity/push-mode.md](connectivity/push-mode.md) — Direct CRD apply
- [connectivity/gitops-mode.md](connectivity/gitops-mode.md) — Git-mediated changes
- [connectivity/job-protocol.md](connectivity/job-protocol.md) — `/api/agent/*` wire format

## API reference

- [api/README.md](api/README.md) — CRDs at a glance
- [api/application-instance.md](api/application-instance.md) — `ApplicationInstance`
- [api/operation.md](api/operation.md) — `Operation`
- [api/conditions.md](api/conditions.md) — Condition types and reasons
- [api/labels-and-annotations.md](api/labels-and-annotations.md) — Well-known metadata

## Operations

- [operations/README.md](operations/README.md) — Operation templates and engines

## Security

- [security/README.md](security/README.md) — RBAC, secrets, authentication, threat model

## Install

- [install/README.md](install/README.md) — Quickstart, Helm, bootstrap, air-gapped

## Operate

- [operate/README.md](operate/README.md) — Observability, upgrades, troubleshooting

## Development

- [development/README.md](development/README.md) — Build, test, release
- [development/mock-control-plane.md](development/mock-control-plane.md) — In-repo mock server for Pull mode
- [development/IMPLEMENTATION_GUIDE.md](development/IMPLEMENTATION_GUIDE.md) — Phase handoff for contributors

## ADRs and RFCs

- [adr/README.md](adr/README.md) — Architecture Decision Records
- [rfcs/README.md](rfcs/README.md) — Requests for Comments

## Diagrams

- [diagrams/README.md](diagrams/README.md) — ASCII architecture and sequence diagrams

## Publishing

- [publication.md](publication.md) — GitHub Pages / MkDocs setup

## Upstream design references

- Parent design note: [ODOO_K8S_APPLICATION_MANAGER_OPERATOR.md](https://github.com/vworkspace-io/vworkspace/blob/main/docs/technical/ODOO_K8S_APPLICATION_MANAGER_OPERATOR.md) (historical filename; describes the operator/control-plane split)
- Control plane product: [vworkspace-server](https://github.com/vworkspace-io/vworkspace-server)
- Product vision: [PRODUCT_VISION.md](https://github.com/vworkspace-io/vworkspace/blob/main/docs/PRODUCT_VISION.md)
