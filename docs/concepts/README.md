# Concepts

**Last Updated:** 2026-05-30

This chapter explains what `vworkspace-operator` is, why it is shaped the way it is, and which problems it deliberately does not try to solve. It is the place to start if you are new to the project. The connectivity chapter ([../connectivity/README.md](../connectivity/README.md)) and the API reference ([../api/README.md](../api/README.md)) build on the vocabulary established here.

The concepts are intentionally short and stable. Implementation detail lives elsewhere — the API reference for spec and status fields, the connectivity chapter for transport, the operations chapter for day-2 mechanics, and ADRs for the rationale behind individual choices.

## Read in order

1. [overview.md](overview.md) — What the operator is, the three layers it sits between, goals and non-goals.
2. [architecture.md](architecture.md) — Components, the one-operator-per-cluster property, and the control and data flow.
3. [crds.md](crds.md) — The two CRDs (`ApplicationInstance`, `Operation`) at a glance, with small examples.
4. [reconciliation-model.md](reconciliation-model.md) — How those CRDs are reconciled into Helm releases and operation runs.
5. [helm-first.md](helm-first.md) — Why deployment logic lives in upstream Helm charts and why Flux Helm Controller is the default engine.
6. [day-2-operations.md](day-2-operations.md) — How backups, restores, upgrades, migrations, and runbooks are modeled.
7. [multi-cluster-and-multi-org.md](multi-cluster-and-multi-org.md) — One operator per cluster, one vWorkspace Server per organization, and the isolation guarantees.
8. [glossary.md](glossary.md) — Definitions for the terms used across these documents.

If you would rather see the architecture first and the prose later, the picture is at [../diagrams/architecture.txt](../diagrams/architecture.txt).
