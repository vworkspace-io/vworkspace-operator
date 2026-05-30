# Modes overview

**Status:** Alpha
**Last Updated:** 2026-05-30

The operator supports three connectivity modes — **Push**, **Pull**, and **GitOps** — and a **Hybrid** posture where different clusters in the same vWorkspace install run different modes. The choice of mode determines how Odoo's intent reaches the cluster and how cluster status reaches Odoo. It does **not** change the CRDs (`ApplicationInstance`, `Operation`), the in-cluster reconciliation loop, the resources Flux or Velero reconcile, or the status vocabulary Odoo sees. Only the transport changes.

This page is the side-by-side comparison. The deep dives are in [pull-mode.md](pull-mode.md), [push-mode.md](push-mode.md), and [gitops-mode.md](gitops-mode.md).

## The three modes

### Push

the control plane authenticates to the cluster's Kubernetes API directly and writes `ApplicationInstance` and `Operation` resources via server-side apply. The operator and the rest of the in-cluster controllers reconcile as usual. Status is read either by Odoo polling the Kubernetes API or by the cluster pushing condition snapshots back to the control plane over the same status channel Pull mode uses.

Push is the simplest mode when the control plane and the cluster live in the same network domain — most commonly when vWorkspace Server is installed into the same cluster it manages, or when both sit behind the same VPN. The trade-off is that Odoo holds a kubeconfig or service-account token per cluster. That credential surface is acceptable when the cluster boundary is also the organization's trust boundary, but undesirable in multi-tenant or multi-organization deployments.

See [push-mode.md](push-mode.md).

### Pull (default)

The operator initiates an outbound connection to the control plane, fetches a stream or queue of jobs targeted at its own cluster identity, applies them to its own API server, and reports status back over the same outbound channel. Odoo never opens a socket to the cluster and never holds a kubeconfig. The cluster holds an outbound bearer token (and optionally a client certificate); Odoo holds the cluster's identity record and the public material needed to verify it.

Pull is the default because it works in the topologies vWorkspace is built for: self-hosted clusters behind NAT, regulated edges with no inbound public address, single-node k3s VMs at the office, and any deployment where the operator wants Odoo's credential surface to be as small as possible. An control plane outage does not take down running applications; the cluster simply pauses new intent and resumes when the connection comes back.

See [pull-mode.md](pull-mode.md) and the wire contract in [job-protocol.md](job-protocol.md).

### GitOps

the control plane renders the desired state of each cluster as a set of manifests — typically the same `ApplicationInstance` and `Operation` resources used in the other modes, plus optional `HelmRelease` resources — and commits them to a Git repository. Flux or Argo CD on the cluster pulls Git, applies what is there, and reports status either back through Git annotations or through the same status channel used in Pull mode.

GitOps is the right choice when change control must flow through Git: regulated organizations, multi-team approval workflows, audit regimes that require every change to land in a reviewed commit, and orgs that already treat Git as the system of record. The trade-off is the additional Git infrastructure and the latency of the commit-and-sync cycle.

See [gitops-mode.md](gitops-mode.md).

### Hybrid

Modes are not mutually exclusive at the fleet level. A vWorkspace install can run Push for the cluster Odoo lives in, Pull for tenant or edge clusters that sit behind NAT, and GitOps for one or two regulated clusters where the customer requires Git-mediated change. The CRDs and operator logic do not change; only the transport per cluster changes.

The mode used by a given cluster is a property of its identity record in Odoo and of how its operator was registered. The operator itself does not need to know how other clusters are configured. The common posture is "Pull by default; Push for the in-cluster install; GitOps when the customer asks for it."

## Comparison

| Mode    | Connectivity direction                       | Credentials location                                                       | Offline behavior                                                                              | Latency                                              | Audit                                                            | Complexity                                                | Best fit                                                                       |
|---------|----------------------------------------------|----------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------|------------------------------------------------------|------------------------------------------------------------------|-----------------------------------------------------------|--------------------------------------------------------------------------------|
| Push    | Odoo → Cluster K8s API (inbound to cluster)  | Odoo holds a per-cluster kubeconfig / token. Cluster holds none for Odoo.  | Cluster keeps reconciling last applied desired state; no new intent until Odoo reaches it.    | Low. Apply is synchronous from the control plane's perspective.   | Centralized in Odoo plus the cluster's own Kubernetes audit log. | Lowest when network is flat; rises sharply with NAT/firewalls. | In-cluster vWorkspace Server install; trusted, dedicated network domain.                    |
| Pull    | Cluster → Odoo (outbound from cluster)       | Cluster holds an outbound, scoped token. the control plane holds no kubeconfig.         | Cluster keeps reconciling last desired state; operator queues status updates until reconnect. | Slightly higher (poll interval or stream re-attach). | Centralized in Odoo; mirrored locally on the cluster.            | Modest control-plane code; far simpler at the edge.       | Multi-org, self-hosted, edge, NAT, regulated, untrusted-cluster SaaS.          |
| GitOps  | Odoo → Git; Cluster → Git                    | Odoo holds Git write credential; cluster holds Git read credential.        | Cluster keeps reconciling last Git revision indefinitely.                                     | Highest (commit + sync interval).                    | Git history is the audit trail; Flux/Argo emit events.           | Highest; introduces Git as an additional system.          | Regulated change control, multi-team approvals, GitOps-first orgs.             |

## What is the same across modes

Worth re-stating, because it is the heart of the design: the cluster does the same work no matter which mode is in use.

- Odoo's intent always arrives as either an `ApplicationInstance` or an `Operation` resource in the cluster's API server.
- The operator reconciles those resources in exactly the same way (see [../concepts/reconciliation-model.md](../concepts/reconciliation-model.md)).
- Flux Helm Controller reconciles the resulting `HelmRelease` in exactly the same way.
- Velero, Argo Workflows, the CSI snapshot controller, and the other ops controllers are driven in exactly the same way.
- Status comes back as the same condition vocabulary (see [../api/conditions.md](../api/conditions.md)).

The mode question is therefore an integration question, not an architectural one. Pick the mode that matches the network topology, the credential model, and the change-control requirements of the deployment, and the rest of the operator's behaviour does not depend on the choice.
