# Overview

**Status:** Alpha
**Last Updated:** 2026-05-30

`vworkspace-operator` is the cluster-side half of vWorkspace. The other half is an Odoo 19 application — the vWorkspace control plane — where a human or the AI assistant in Discuss declares intent ("deploy Nextcloud in this namespace", "back this up nightly", "upgrade that"). The operator is the piece that turns that intent into actual Kubernetes resources on a specific cluster, owns its own reconciliation loop, and reports status back.

## The three layers

The design draws a hard line between three distinct concerns, and the operator sits in the middle.

1. **control plane intent.** A human (or the AI assistant on the human's behalf) declares what should exist: which applications, in which namespaces, at which chart versions, with which values; and which day-2 operations should run against them. Odoo is the source of truth for *what is wanted*. It is not a reconciler, it does not hold a kubeconfig, and it does not own per-resource state.
2. **Operator orchestration.** The operator on each cluster owns the in-cluster CRDs `ApplicationInstance` and `Operation`, translates them into the resources that proven third-party controllers understand (most importantly Flux `HelmRelease`), and aggregates status back into condition-shaped Kubernetes objects. It is the source of truth for *what the cluster is doing about it*.
3. **Third-party controllers.** Flux Helm Controller actually performs Helm reconciliation. Velero takes backups. The CSI snapshot controller takes snapshots. VolSync replicates volumes. Argo Workflows runs multi-step DAGs. cert-manager issues certificates. external-secrets pulls values from external secret stores. The cluster's ingress controller routes north-south traffic. These are the source of truth for *how Kubernetes actually does the work*.

The operator wires those layers together. It does not duplicate any of them. The reason for this layering, and the reason it is enforced by the API design rather than merely encouraged by convention, is in [architecture.md](architecture.md).

## Goals

- **Helm-first reconciliation.** Application deployment logic lives in upstream Helm charts. The operator does not re-implement Deployments, Services, Ingresses, or init Jobs that already exist in a chart. It also does not embed any per-application controller code — no Nextcloud upgrade logic, no Mattermost migration script. See [helm-first.md](helm-first.md).
- **Day-2 operations as first-class resources.** Backups, restores, upgrades, migrations, rollbacks, and runbooks are modeled as the single `Operation` CRD, reconciled by the operator and proven third-party controllers, not as imperative scripts on the control-plane side. See [day-2-operations.md](day-2-operations.md).
- **Multi-cluster, multi-organization.** Odoo can manage many clusters. Each cluster runs exactly one operator. The cluster — not Odoo — is the source of truth for what is actually running there. See [multi-cluster-and-multi-org.md](multi-cluster-and-multi-org.md).
- **Clear contracts between layers.** The contract between Odoo and the cluster is two CRDs and a small status/condition vocabulary. The contract between the operator and Flux is `HelmRelease`. The contract between the operator and Velero is `velero.io/Backup` and `velero.io/Restore`. Every contract is a documented, versioned API, not a private convention.
- **Self-hosted ethos.** The design must work on hardware-owned clusters behind a home or office firewall, on regulated edge clusters with no inbound access, on single-node k3s VMs, and on managed Kubernetes accounts, without forcing the operator to expose the cluster API to the public internet. This is what makes Pull mode the default (see [../connectivity/pull-mode.md](../connectivity/pull-mode.md)).

## Non-goals

- **No bespoke mini-Helm engine.** The operator does not maintain its own Helm history, drift remediation, or chart-source authentication. That is what `HelmRelease` is for.
- **No app-specific lifecycle code.** Upgrades, migrations, and integrations for individual applications (Nextcloud, Mattermost, Immich, OnlyOffice, ...) live in upstream charts and chart hooks. The operator drives them as generic chart-version bumps plus generic chart-hook execution; it does not contain per-application controller code, and it never will.
- **No continuous reconciliation in Odoo.** Reconciliation belongs in Kubernetes controllers. Odoo records intent and observes outcome; it does not run a control loop over in-cluster resources.
- **No kubeconfig in Odoo by default.** Holding a kubeconfig per cluster leaks the wrong credentials in the wrong direction for the typical vWorkspace deployment. the control plane authenticates *clusters that contact it*; it does not authenticate *to* clusters unless the operator explicitly opts into Push mode. See [../connectivity/modes-overview.md](../connectivity/modes-overview.md).

## What you get from this design

Because the operator is small and the surface area is two CRDs, the behaviour the operator must guarantee is small and testable: take an `ApplicationInstance`, produce the right `HelmRelease`; take an `Operation`, drive the right engine; map status back into conditions; never log secret material; never go beyond the namespaces it has been told it may touch. Everything else is upstream Kubernetes, upstream Helm, and upstream backup tooling, with the operator as the layer that knows how Odoo wants those things wired together.

That property — "the operator is the wiring, not the engine" — is what lets the project be ambitious about *integration* (apps, day-2 operations, multi-cluster, multi-org, connectivity modes) without being ambitious about *reinvention*. Every important moving part is already a well-tested controller in the Kubernetes ecosystem. The operator's job is to express Odoo's intent in a vocabulary those controllers already speak.
