# Multi-cluster and multi-organization

**Status:** Alpha
**Last Updated:** 2026-05-30

vWorkspace is built around two related properties: **one vWorkspace Server install per organization**, and **one operator per cluster**. This page explains what those properties mean, why both are deliberate, and how they combine into the isolation guarantees the project offers. The companion connectivity chapter ([../connectivity/README.md](../connectivity/README.md)) describes the transport that makes this model work in practice.

## One vWorkspace Server per organization

A vWorkspace install serves a single organization — a small business, a school, a clinic, an NGO, a homelab, an agency. There is no managed multi-tenant SaaS layer baked into the product. There is no `vworkspace_platform` module. The organization owns its database, its identity material, its catalog, its audit log, its upgrade cadence, and its hardware.

This is the self-hosted ethos described in the parent project's [PRODUCT_VISION.md](https://github.com/vworkspace-io/vworkspace/blob/main/docs/PRODUCT_VISION.md). The operator inherits it directly: every credential, every audit trail, and every cluster identity exists inside the operator's own organization. Nothing leaks upward to a central party.

(An opt-in hosted offering, where a single vWorkspace Server install serves many customer organizations, is possible without changing the operator. The cluster identity model already scopes every job to a single cluster, and clusters are scoped to a single organization at the Odoo level; see [Multi-tenant isolation in Pull mode](../connectivity/pull-mode.md#multi-tenant-isolation). The operator itself does not need to know whether it is the only customer of an vWorkspace Server install or one of many.)

## One operator per cluster

Each cluster runs exactly one operator. The cluster, not Odoo, is the source of truth for what is actually running there. The operator owns the in-cluster CRDs, drives the in-cluster controllers, and reports status back to the control plane through whichever connectivity mode is configured.

Four properties come out of this constraint.

- **Bounded blast radius.** A misconfigured operator, a compromised credential, or a botched upgrade on cluster A cannot reach cluster B. The cluster boundary is the trust boundary.
- **Independence from Odoo.** Once intent has been applied as a CR in the cluster's API server, the in-cluster reconciliation loop continues without vWorkspace Server. If Odoo is offline for an hour, a day, or a week, applications keep running and recovering. New intent is paused; the reconcile of existing intent is not.
- **Local RBAC.** Each cluster owns its own RBAC. The operator's permissions are scoped to namespaces labeled `managed-by=vworkspace`. There is no central party that can grant a cluster more permissions than it has locally agreed to.
- **Independent upgrade cadence.** Each operator is itself a workload reconciled by Flux. Different clusters can run different operator versions within the documented version-skew window, without coordination.

The flip side is that there is no in-cluster cross-cluster coordination. The operator does not know about other clusters and does not try to. Cross-cluster orchestration ("migrate Nextcloud from cluster A to cluster B") is expressed in Odoo as a sequence of per-cluster `Operation` requests, not as a single resource on either cluster.

## Cluster identity

Each cluster has a stable identity record in Odoo, established once at registration time:

- A globally unique cluster ID (e.g. `cluster-prod-1`).
- A display name and owning organization.
- A public key, used when signed-payload mode is enabled.
- The set of namespaces the operator is allowed to act in.
- The set of catalog entries (applications) the cluster is allowed to run.
- The set of operation engines and operation types the cluster is allowed to execute.

The cluster holds the corresponding outbound credential (a long-lived, rotateable bearer token, optionally paired with a client certificate). the control plane does not hold a kubeconfig for the cluster in the default Pull-mode deployment. The credential surface is intentionally one-sided: the cluster authenticates *to* Odoo, not the other way around. The exception is Push mode, where Odoo does hold a kubeconfig or service-account token — used when both sides share a trusted network. See [../connectivity/push-mode.md](../connectivity/push-mode.md).

Every resource the operator writes carries `app.vworkspace.io/cluster-id=<id>`. Every status event the operator posts carries the same identifier. Odoo enforces server-side that a given credential can only fetch jobs for, or post status for, the cluster identity it was issued against. The operator's [job protocol](../connectivity/job-protocol.md) treats cluster identity as a first-class authorization scope on every request.

## How Odoo fans out across clusters

Odoo addresses many clusters at once by emitting per-cluster jobs. The fanout is conceptually simple:

1. A human (or the AI assistant in Discuss) declares intent. Sometimes that intent is "deploy Nextcloud on cluster-prod-1." Sometimes it is "back up Mattermost on every production cluster." Sometimes it is a fleet-wide operator upgrade.
2. Odoo expands the intent against the cluster registry. A single fleet-wide request becomes one job per matching cluster, keyed by cluster ID.
3. Each job lands in the per-cluster job queue. Each cluster pulls its own jobs over its own outbound connection (or has its jobs delivered by Push or rendered to Git in the other connectivity modes).
4. Each operator applies its jobs locally, reconciles its CRs, drives its controllers, and reports status back.
5. Odoo aggregates the per-cluster outcomes into a fleet-wide view. The Cluster Registry surface shows which clusters succeeded, which are still working, and which need attention.

The fanout is in Odoo. The reconciliation is per cluster. The operator's job is *only* the per-cluster half; it does not loop over the fleet.

## Mixed connectivity in one fleet

Modes are not mutually exclusive at the fleet level. A common posture for a vWorkspace install is:

- **Push** for the cluster Odoo lives in (same network, no NAT, no firewall in the way).
- **Pull** for tenant or edge clusters that sit behind NAT.
- **GitOps** for one or two regulated clusters where the customer requires Git-mediated change.

The CRDs and operator logic do not change. Only the transport per cluster changes. The mode is a property of the cluster identity record in Odoo and of how the operator was registered; reconciliation, status, and the in-cluster behaviour are otherwise indistinguishable.

## Isolation guarantees

The combined model produces a small, explicit set of isolation guarantees.

- **Across clusters.** A compromise of one cluster's outbound token grants the attacker exactly that cluster's job stream. They cannot fetch jobs for, or post status for, any other cluster. They cannot push new intent into any cluster (only Odoo signs jobs). They cannot read any other cluster's data — Odoo never relays one cluster's data to another.
- **Across namespaces inside a cluster.** The operator's RBAC is scoped to namespaces labeled `managed-by=vworkspace`. Other namespaces on the same cluster — system namespaces, customer-managed namespaces, sandbox namespaces — are unreachable from the operator's service account.
- **Across organizations (in a hypothetical hosted Odoo).** Cluster identities are scoped to a single organization. Server-side authorization rejects any cross-organization read on every request. Cross-org access does not exist; it is rejected by construction.

## The self-hosted ethos

What this model is really expressing is that vWorkspace is built for a world where the organization running it owns its own data, its own credentials, and its own clusters. The platform's job is not to be a hyperscaler in miniature; it is to make running your own infrastructure coherent. The operator inherits that posture in three concrete ways:

- The cluster, not Odoo, is the source of truth for what is running.
- The credential flow is outbound from the cluster by default. the control plane authenticates clusters that contact it; it does not — unless explicitly configured otherwise — authenticate to clusters.
- A failure of Odoo does not take down running applications. A failure of one cluster does not take down any other.

Those properties are what let the same code base serve a homelab on a single-node k3s VM, a small business on a three-node cluster behind their office firewall, a regulated clinic at the network edge, and a small fleet of clusters managed by an agency on behalf of multiple customers — without any of those deployments having to compromise on isolation, sovereignty, or operational independence.
