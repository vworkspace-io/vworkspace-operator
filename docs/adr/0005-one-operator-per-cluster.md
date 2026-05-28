# 0005 — One operator per cluster

**Status:** Accepted (2026-05-28)

## Context

The vWorkspace platform manages multiple clusters from a single Odoo control plane. The product's audience includes single-cluster homelab operators and multi-cluster organizations alike — a small business with one production cluster, a school with one staging and one production, an agency with one cluster per client engagement.

A reasonable initial design would be a single, centralized operator that manages many clusters. The operator would hold credentials for each cluster's API server (or run as a multi-cluster controller via `kubeconfig`-per-cluster), reconcile CRDs that live in one place, and produce a unified status surface. This is the shape of fleet-management tools like Cluster API's management cluster pattern.

A second design is one operator per cluster: each cluster runs its own copy of `vworkspace-operator`, reconciles CRDs on its own API server, and reports status to Odoo over the connectivity channel ([0003](0003-pull-mode-as-default-connectivity.md)). The operator is unaware of other clusters; it manages only the API it is running against.

The trade-offs:

| Property                              | Centralized (one operator, many clusters)                                              | One per cluster                                                                  |
|---------------------------------------|----------------------------------------------------------------------------------------|----------------------------------------------------------------------------------|
| Credential surface                    | The operator holds kubeconfigs for every cluster it manages. A compromise reaches them all. | Each cluster holds only its own credentials. A compromise is bounded.            |
| Failure isolation                      | Operator failure affects every cluster it manages.                                      | Operator failure on one cluster does not affect others.                          |
| Network requirements                   | Operator needs reachability to every cluster's API.                                     | Each cluster only needs outbound to Odoo (Pull) or inbound from Odoo (Push).      |
| Odoo-down behavior                     | Centralized operator typically lives in or near Odoo; an Odoo-region outage is severe.  | Each cluster reconciles autonomously when Odoo is unreachable.                   |
| Per-cluster policy                     | Multi-tenancy inside the operator. Complex to get right; easy to leak across tenants.    | Each cluster has its own RBAC, its own admission webhook, its own policy.        |
| Operator upgrade blast radius          | Bumping the operator's version touches every cluster simultaneously.                    | Each cluster upgrades independently; staged rollouts are natural.                  |
| Multi-cluster operations (e.g., DR migration) | Centralized operator can orchestrate cross-cluster work directly.                       | Cross-cluster work must be coordinated by Odoo, which is the right place anyway.  |
| Resource cost                         | One operator pod; minimal cluster-side footprint.                                       | One operator pod per cluster; the bundle is ~0.5 CPU and ~512 MiB of memory.     |

The vWorkspace ethos (`PRODUCT_VISION.md`) prioritizes operator sovereignty: the person running the cluster owns the data and the credentials. A centralized operator inverts that — Odoo (or wherever the centralized operator lives) holds the credentials for every cluster, and the cluster operator must trust the central party. This is exactly the credential direction that Pull mode (ADR 0003) avoids; running a centralized operator would re-introduce the problem on the operator side.

A centralized operator also couples cluster reconciliation to the operator's availability. If the centralized operator is down or partitioned from a cluster, that cluster stops reconciling new intent. Self-hosted clusters in regulated environments — clinics, schools, NGOs — cannot accept that coupling: their cluster must continue serving applications regardless of the management plane's reachability.

The resource cost of one-operator-per-cluster is real (an extra deployment per cluster) but small in absolute terms. Cluster footprint dominates: any cluster running vWorkspace applications is already running Flux, cert-manager, external-secrets, Velero, and the application workloads themselves; adding the operator is a rounding error.

Multi-cluster operations (a DR migration that copies an application from cluster A to cluster B) are not lost in the one-per-cluster model; they are coordinated by Odoo. Odoo issues two `Operation` CRs (one on each cluster) and watches their statuses. The orchestration logic lives in Odoo, where it belongs — Odoo is the place a human declares intent — not in a cluster-local operator that happens to have access to two clusters.

## Decision

Each Kubernetes cluster runs **exactly one** instance of `vworkspace-operator`. The operator manages only the cluster it is running on. It does not hold credentials for any other cluster. Cross-cluster coordination is Odoo's job.

Concretely:

- The bundle's Helm chart installs one operator deployment with `replicas: 1` (plus leader election for safe rolling upgrade). It does not support a multi-cluster controller mode.
- The operator's `Cluster` CR is a singleton per cluster, named after the cluster's identity. There is no list of clusters in the operator's API; the cluster knows itself.
- The operator's RBAC ([../security/rbac.md](../security/rbac.md)) is scoped to its own cluster. There is no `kubeconfig` mounted into the operator pod for a different cluster.
- Multi-cluster operations are expressed as multiple `Operation` CRs, one per cluster, coordinated by Odoo.

This decision is recorded here as a constraint that other decisions inherit. The connectivity model (ADR 0003) and the CRD design (ADR 0004) both assume one operator per cluster; relaxing this would require revisiting both.

## Consequences

**Bounded blast radius.** A compromised operator pod compromises one cluster, not a fleet. The credential blast radius is one cluster's bootstrap credential ([../security/authentication.md](../security/authentication.md)), not a kubeconfig collection.

**Cluster autonomy.** The cluster's reconciliation loop runs whether Odoo is up or not. Applications stay up during Odoo outages. New intent waits until Odoo is reachable; existing desired state continues to reconcile. This is the explicit goal of the Pull-mode design.

**One operator version per cluster.** Each cluster has its own operator version. Staged rollouts and per-cluster pinning ([../operate/upgrades.md](../operate/upgrades.md)) are natural. An operator upgrade on one cluster does not affect any other.

**Per-cluster RBAC and policy.** Each cluster has its own RBAC, its own admission webhook, its own allowed-namespaces list, its own allowed-operation-templates list. Multi-tenancy inside the operator is not a concern; the cluster is the tenancy boundary.

**Odoo manages many clusters.** The fan-out from "one human declares intent" to "many clusters reconcile" lives in Odoo. The Cluster Registry in Odoo is the place an operator sees the fleet; the operator on each cluster is unaware of the others.

**Cross-cluster work is coordinated centrally.** A DR migration between two clusters is two `Operation` CRs (on the source and the destination), watched by Odoo. The cluster-local operators do not talk to each other; they only talk to Odoo. This is appropriate: cross-cluster coordination is a control-plane concern, not a controller concern.

**Resource cost per cluster.** Each cluster runs the operator and the bundled controllers (~0.5 CPU, ~512 MiB memory). For a fleet of hundreds of clusters, this footprint multiplied is non-trivial but justified by the security and reliability properties above.

**This ADR pairs with 0003 and 0004.** Pull mode (0003) is the connectivity model that lets one-operator-per-cluster work over outbound HTTPS. The two CRDs (0004) are the contract Odoo and each cluster operator share; they are the same two CRDs on every cluster, so Odoo's code is uniform across the fleet.

**Revisiting requires a new ADR.** A future direction in which the project supports a multi-cluster controller (for example, for orgs that explicitly prefer a centralized model and accept the credential trade-off) would land as a new ADR superseding this one. We expect this not to happen in the foreseeable future; the project's audience is self-hosted by design.
