# 0003 — Pull mode as default connectivity

**Status:** Accepted (2026-05-28)

## Context

The operator and the vWorkspace Server control plane need a way to exchange intent (Odoo → cluster) and status (cluster → Odoo). Three transport models are credible:

- **Push.** the control plane authenticates to the cluster's Kubernetes API and writes CRDs via server-side apply. Status is observed by Odoo polling or by the cluster posting back.
- **Pull.** The operator opens an outbound connection to Odoo, fetches jobs, applies them locally, and reports status back over the same outbound channel.
- **GitOps.** the control plane writes manifests into a Git repository; the cluster's Flux or Argo CD pulls Git and applies; status flows back through Git annotations or through a separate status channel.

The decision is not "which one to support" — the project supports all three behind the same in-cluster CRDs — but "which one to make the default for the bundled install and the recommended documentation path".

Each mode has a credential surface and a network surface:

| Mode    | Credentials Odoo holds                          | Credentials cluster holds                       | Network direction               |
|---------|--------------------------------------------------|--------------------------------------------------|---------------------------------|
| Push    | Per-cluster kubeconfig or scoped SA token.       | None for Odoo.                                   | Odoo → Cluster K8s API (inbound to cluster). |
| Pull    | Per-cluster identity record + optional public key. | Outbound bearer token (and optionally a client cert). | Cluster → Odoo (outbound from cluster). |
| GitOps  | Git write credential.                            | Git read credential.                              | Both sides talk to Git.          |

The vWorkspace product principles (see [PRODUCT_VISION.md](https://github.com/vworkspace-io/vworkspace/blob/main/docs/PRODUCT_VISION.md)) center on self-hosting: a small business behind NAT, a clinic on a regulated edge, a school in a constrained network, a homelab operator who refuses to expose the cluster API. Push is the wrong default for these: it requires inbound network access to the cluster API and forces Odoo to hold a kubeconfig per cluster — a credential that, in the typical vWorkspace deployment, leaks the wrong direction. The operator who owns the cluster does not want a central party (Odoo) to be the single point that, if compromised, gives an attacker `kubectl` on every cluster.

GitOps is appealing for organizations whose change control must flow through Git, but it adds Git as a hard dependency and shifts the audit trail into Git history. For most self-hosted vWorkspace installs, that is one system more than they want to run.

Pull addresses the self-hosting case directly: only outbound HTTPS is required on the cluster; the control plane holds no kubeconfig; rotating credentials is a one-side operation; an control plane outage does not bring down the cluster (the cluster continues reconciling). The only operational cost is the cluster-side job-fetch loop, which is a few hundred lines of well-understood code (long-polling, batched status, bounded buffer).

A second consideration: even in deployments where Odoo and the cluster are colocated (vWorkspace Server installed *into* the cluster it manages), Push works without issue. Picking Pull as the default does not foreclose Push for these cases; the operator supports both.

A third consideration: the cluster reconciliation loop should not be coupled to the control plane's availability. Pull mode's offline-tolerance is the right shape: the cluster keeps reconciling the last known desired state when Odoo is unreachable. Push could in principle have the same property (the cluster's CRDs persist on the cluster API regardless of Odoo), but in practice Push implementations end up centralizing more in Odoo because the temptation to "just sync from the control plane on every reconcile" is real.

## Decision

We will make **Pull mode the default connectivity model** for `vworkspace-operator`.

Concretely:

- The bundled install's `Cluster` CR has `spec.connectivityMode: pull` unless explicitly overridden.
- The default transport for Pull is HTTP long-poll plus a separate batched HTTPS status endpoint (`POST /api/agent/events`). Other transports (gRPC, SSE, MQTT, NATS) plug in behind the same job interface.
- The credentials Odoo holds for a Pull-mode cluster are an identity record and a public key (when signed/encrypted payloads are enabled). the control plane does not hold a kubeconfig.
- The cluster holds a long-lived, rotateable bootstrap credential (and optional mTLS material) in a Kubernetes `Secret` scoped to `vworkspace-system`.
- Push mode remains supported for in-cluster vWorkspace Server installs. GitOps mode remains supported for orgs that require Git-mediated change control. The same CRDs and the same in-cluster reconciliation loop are used in every mode; only the transport changes.

The credential lifecycle (one-time registration token, exchange for long-lived credential, rotation, optional mTLS, optional signed/encrypted payloads) is documented in [../security/authentication.md](../security/authentication.md).

The default documentation path — installer, quickstart, bootstrap, troubleshooting — assumes Pull. The other two modes are documented under [../install/cluster-bootstrap.md](../install/cluster-bootstrap.md) as variations.

## Consequences

**The cluster's network requirements shrink to outbound HTTPS.** No inbound port on the cluster API needs to be exposed. Self-hosted clusters behind NAT or a corporate firewall work out of the box. This is the largest reason Pull is the default.

**Odoo's credential surface stays minimal.** Odoo holds a per-cluster identity, not a kubeconfig. A compromised Odoo can issue jobs that the cluster will admit (and run through the cluster's admission webhook), but it cannot directly kubectl-into the cluster. The compromise is bounded by the admission webhook's `Operation` type allow-list and by the placeholder rule set that rejects inline secret material ([../security/secrets-handling.md](../security/secrets-handling.md)).

**The operator carries a job-fetch client.** This is new code (long-polling, ack/status/result endpoints, batched event sender, bounded local buffer for offline tolerance, credential rotation). It is documented under [../development/project-layout.md](../development/project-layout.md) (`internal/agent/`). The maintenance cost is real but bounded.

**An control plane outage does not take down a cluster.** Applications stay up. Scheduled operations continue. The operator queues outbound events; once Odoo returns, it flushes them and resumes pulling jobs. This is a property of Pull mode that we want and would lose if we made Push the default.

**Two more modes to support.** Push and GitOps remain in the codebase. The CRDs and the in-cluster reconciler are the same; the transport differs. We must continue to test all three. The cost is bounded by the fact that the three transports share a single `internal/agent` interface.

**Server-side authorization on Odoo.** Pull places the access-control burden on the Odoo `/api/agent/*` endpoints. Each cluster's bootstrap credential is scoped to that cluster's ID; cross-cluster reads return 403. This is the bulk of the control-plane-side security work for connectivity.

**Future moves.** Optional mTLS, optional signed payloads, and optional encrypted payloads (described in [../security/authentication.md](../security/authentication.md)) layer on top of Pull. Push mode does not get the same layering naturally — kubeconfigs are the kubeconfigs, mTLS to the K8s API is a different conversation. The Pull-mode default lets us add those layers without re-architecting the transport.

**Documentation defaults.** The README, the quickstart, and the bootstrap guide all assume Pull. Push and GitOps are documented as variations in [../install/cluster-bootstrap.md](../install/cluster-bootstrap.md). A reader who needs Push knows to look there; a reader who is operating a self-hosted cluster finds the right thing first.
