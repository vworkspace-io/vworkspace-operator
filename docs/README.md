# vworkspace-operator documentation

**Status:** Alpha — APIs are at `v1alpha1` and may evolve.
**Last Updated:** 2026-05-28

This directory is the canonical documentation for `vworkspace-operator`, the cluster-side Kubernetes operator that an Odoo-based [vWorkspace](https://github.com/vworkspace-io/vworkspace) control plane drives to install, upgrade, back up, and otherwise operate applications running in a Kubernetes cluster. The repository [README](../README.md) is the short orientation; everything below is the long-form reference.

## How to read these docs

Start at [concepts/overview.md](concepts/overview.md) to understand what the operator is and the three layers it sits between (Odoo intent, operator orchestration, third-party controllers). From there, either:

- Take the **try-it path** — jump to the [install index](install/README.md) (quickstart and cluster bootstrap), bring up an operator on a test cluster, and come back to the concepts once it is running; or
- Take the **architecture-first path** — read [connectivity/modes-overview.md](connectivity/modes-overview.md) to understand how Odoo reaches the cluster (Pull by default, Push or GitOps when appropriate), then dive into the [API reference](api/README.md) for the two CRDs the operator owns.

Both paths converge in the same place: a small, predictable contract between Odoo and one operator per cluster, expressed as `ApplicationInstance` (`apps.vworkspace.io/v1alpha1`) and `Operation` (`ops.vworkspace.io/v1alpha1`).

## Concepts

- [concepts/overview.md](concepts/overview.md) — What the operator is, the three layers, goals and non-goals.
- [concepts/architecture.md](concepts/architecture.md) — Components, why one operator per cluster, control and data flow.
- [concepts/crds.md](concepts/crds.md) — A guided tour of the two CRDs, with small examples.
- [concepts/reconciliation-model.md](concepts/reconciliation-model.md) — How `ApplicationInstance` and `Operation` are reconciled.
- [concepts/helm-first.md](concepts/helm-first.md) — Why Helm-first, and why Flux Helm Controller is the default engine.
- [concepts/day-2-operations.md](concepts/day-2-operations.md) — Backups, restores, upgrades, migrations, and runbooks as `Operation` resources.
- [concepts/multi-cluster-and-multi-org.md](concepts/multi-cluster-and-multi-org.md) — One operator per cluster, one Odoo per organization, isolation guarantees.
- [concepts/glossary.md](concepts/glossary.md) — Definitions for the vocabulary used across these documents.

## Connectivity

- [connectivity/README.md](connectivity/README.md) — Chapter index. Pull is the default; Push is for in-cluster Odoo; GitOps is for regulated change control.
- [connectivity/modes-overview.md](connectivity/modes-overview.md) — Side-by-side comparison of Push, Pull, GitOps, and Hybrid.
- [connectivity/pull-mode.md](connectivity/pull-mode.md) — Deep dive on the default mode.
- [connectivity/push-mode.md](connectivity/push-mode.md) — When Odoo writes CRDs directly via the K8s API.
- [connectivity/gitops-mode.md](connectivity/gitops-mode.md) — Odoo renders manifests to Git; the cluster pulls Git.
- [connectivity/job-protocol.md](connectivity/job-protocol.md) — Wire-level HTTP contract for Pull mode.

## API reference

- [api/README.md](api/README.md) — CRDs at a glance.
- [api/application-instance.md](api/application-instance.md) — `ApplicationInstance` spec, status, and validation rules.
- [api/operation.md](api/operation.md) — `Operation` spec, status, and admission rules.
- [api/conditions.md](api/conditions.md) — Standard condition types and reasons.
- [api/labels-and-annotations.md](api/labels-and-annotations.md) — Well-known labels and capability annotations the operator reads and writes.

## Operations

- [operations/README.md](operations/README.md) — Operation templates and engines. The five execution patterns (Job, Argo Workflows, Velero, CSI snapshots / VolSync, Helm hooks).

## Security

- [security/README.md](security/README.md) — RBAC, secrets, authentication, threat model, and operation-safety controls.

## Install

- [install/README.md](install/README.md) — Index for installation topics, including quickstart, cluster bootstrap, supported Kubernetes distributions, and air-gapped installs.

## Operate

- [operate/README.md](operate/README.md) — Day-to-day operation of the operator itself.
- [operate/upgrades.md](operate/upgrades.md) — Upgrading the operator, version-skew policy, conversion webhooks.

## Development

- [development/README.md](development/README.md) — Building, testing, releasing, and project layout for contributors.

## ADRs

- [adr/README.md](adr/README.md) — Architecture Decision Records. Records of the choices behind the operator.

## RFCs

- [rfcs/README.md](rfcs/README.md) — Requests for Comments. In-flight design proposals before they become ADRs.

## Diagrams

- [diagrams/README.md](diagrams/README.md) — Index of the ASCII diagrams used in this documentation.
- [diagrams/architecture.txt](diagrams/architecture.txt) — Odoo control plane fanning out across clusters.
- [diagrams/pull-mode-sequence.txt](diagrams/pull-mode-sequence.txt) — End-to-end sequence for a Pull-mode apply.

## Reference

The single source of truth for design decisions is the parent project's design note: [`docs/technical/ODOO_K8S_APPLICATION_MANAGER_OPERATOR.md`](https://github.com/vworkspace-io/vworkspace/blob/main/docs/technical/ODOO_K8S_APPLICATION_MANAGER_OPERATOR.md). The product framing lives in [`docs/PRODUCT_VISION.md`](https://github.com/vworkspace-io/vworkspace/blob/main/docs/PRODUCT_VISION.md) and the timeline in [`docs/ROADMAP.md`](https://github.com/vworkspace-io/vworkspace/blob/main/docs/ROADMAP.md). The documentation in this repository is meant to be consistent with those, but is the operational reference for anyone running or contributing to `vworkspace-operator`.
