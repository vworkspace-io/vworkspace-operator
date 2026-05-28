# Push mode

**Status:** Alpha
**Last Updated:** 2026-05-28

In Push mode, Odoo authenticates directly to the cluster's Kubernetes API and writes the operator's CRDs — `ApplicationInstance` (`apps.vworkspace.io/v1alpha1`) and `Operation` (`ops.vworkspace.io/v1alpha1`) — using server-side apply. The operator and the rest of the in-cluster controllers reconcile those resources exactly as in any other mode. Status either flows back via Odoo polling the K8s API, or via the same outbound status channel used by Pull mode.

Push is the simplest mode when Odoo and the cluster share a trusted network. It is the right default in two narrow scenarios: when Odoo is installed *into* the same cluster it manages (the most common in-cluster topology), and when both sides sit behind the same VPN or in the same datacenter. It is not the right default for self-hosted clusters behind NAT, regulated edges, or multi-tenant deployments — those want [Pull mode](pull-mode.md).

## When Push is the right choice

- **Odoo is co-resident with the cluster it manages.** A vWorkspace install where the Odoo `Deployment` and the `vworkspace-operator` `Deployment` live in the same cluster, talking to the same Kubernetes API. There is no network boundary between them; Odoo already has cluster-internal connectivity, and a service-account token scoped to the operator's CRDs is the natural credential.
- **Odoo and the cluster share a trusted network.** Two adjacent clusters on the same VPN, the same Tailscale tailnet, the same private VLAN. The cluster API server is reachable from Odoo's network, the operator running it trusts that network, and the credential surface is acceptable.
- **A small, dedicated fleet.** A platform team running a handful of clusters within their own infrastructure, where holding a kubeconfig per cluster is operationally fine. Push gives them the lowest latency (apply is synchronous from Odoo's perspective) and the simplest mental model.

If any of "behind NAT", "behind a customer firewall", "edge cluster", "regulated change control", "multi-organization SaaS", or "Odoo should never hold a kubeconfig" applies, the right answer is Pull (or GitOps), not Push.

## Authentication

Odoo holds the cluster-side credential in Push mode. There are two supported forms.

- **Service-account token (recommended).** During cluster bootstrap, a `ServiceAccount` named `vworkspace-control-plane` is created in the operator's namespace. A long-lived projected token (or a bound token with a renewal loop) is granted RBAC scoped to the operator's CRD groups: `apps.vworkspace.io` and `ops.vworkspace.io`. The token gets `create`, `get`, `list`, `watch`, `patch`, `update`, `delete` on `ApplicationInstance` and `Operation` resources in the namespaces labeled `managed-by=vworkspace`, plus `get`/`list`/`watch` on `Cluster` for status, and *no other permissions*. Odoo stores this token, scoped to this single cluster, in its platform database.
- **Per-cluster kubeconfig.** Equivalent power, more general format. A kubeconfig with `current-context` set to the cluster, embedding the service-account token described above. Useful when Odoo's tooling already speaks `kubeconfig` (for example, when the same code path is used to call `kubectl` for diagnostics).

Both forms point at the same RBAC profile. The operator's admission webhook still validates every write: even a misconfigured Odoo cannot apply CRs into namespaces that are not labeled `managed-by=vworkspace`, cannot use disallowed `Operation` types in a given namespace, and cannot bypass approvals on operations the catalog marks as requiring them.

mTLS to the API server is configurable. If the cluster issues client certificates for control-plane components, Odoo can present one in addition to the bearer token.

## Server-side apply

Odoo's writes use server-side apply (SSA) with a stable field manager — typically `vworkspace-odoo` (distinct from the operator's own field manager). The benefits are:

- **Field ownership.** Fields Odoo owns are clearly attributable. Fields the operator owns on a downstream resource (`HelmRelease`, `Backup`, ...) are not affected by Odoo's writes.
- **Conflict surfacing.** If a human admin or another control plane writes a conflicting field, the SSA conflict is visible in Odoo's audit log rather than being silently overwritten.
- **Idempotency.** Re-applying the same manifest is a no-op. Odoo can retry on transient errors without worrying about over-writing.

Server-side apply is also the same mechanism the operator itself uses for the downstream resources it materializes. Push mode does not introduce a different write model; it just changes who is doing the applying for the top-level CRDs.

## Status

Push mode has two ways to surface status back to Odoo. They are not mutually exclusive.

- **Watch the K8s API.** Odoo establishes a watch on `ApplicationInstance` and `Operation` in the cluster (the same RBAC profile that allows apply also allows watch), and reacts to status changes as they occur. This is the lowest-latency path. It requires Odoo to maintain a long-lived connection to the cluster API server for each cluster it watches — acceptable for a small co-resident fleet, less acceptable for many clusters.
- **Push back via the status endpoint.** The operator can also be configured to post status events to the same `POST /api/agent/events` endpoint Pull mode uses. This is useful when Odoo prefers a uniform status pipeline regardless of mode, or when the watch connection is unreliable.

In a hybrid fleet (some clusters Push, some Pull), funneling all status through the `POST /api/agent/events` endpoint keeps Odoo's ingest path uniform.

## Trade-offs vs Pull

- **Connectivity.** Push requires Odoo to reach the cluster API server. NAT, firewalls, and provider boundaries make this hard outside a flat trusted network. Pull only needs outbound HTTPS from the cluster.
- **Credentials.** Push gives Odoo a powerful credential (write access to the operator's CRDs in the cluster). Pull gives Odoo an identity record and the cluster holds an outbound token. Pull's credential surface is strictly smaller.
- **Blast radius of an Odoo compromise.** A compromise of an Odoo holding kubeconfigs is materially worse than a compromise of an Odoo that only signs jobs. In Pull, an attacker who takes over Odoo can still only push intent, which still has to be applied locally by an operator that may verify signed payloads. In Push, an attacker has the cluster's CRD-scoped write API at their fingertips.
- **Operational independence.** In both modes, an Odoo outage does not stop in-cluster reconciliation; the operator keeps converging to the last applied desired state. Push catches up faster on reconnect (Odoo just resumes its watch). Pull catches up via re-pulling the current job set.
- **Latency.** Push has lower latency for apply (synchronous from Odoo's perspective). Pull has slightly higher latency (the next long-poll cycle). For human-driven work this is rarely the deciding factor.
- **Complexity.** Push is the simplest mode when network is flat; complexity rises sharply when network is not flat. Pull's complexity is roughly constant across topologies.

Push mode is supported because it is the right answer in a real and common deployment topology (in-cluster Odoo). It is not the default because that topology is not the only one the project is built for.

## Migrating between modes

Push and Pull use the same CRDs and the same in-cluster reconciliation loop. Migrating a cluster from one to the other is an Odoo-side change plus a credential rotation: register the cluster in the target mode, revoke the credential it no longer needs, and the operator's behavior does not change. The in-cluster state is preserved across the migration because it is the same CRDs in the same API server before and after.
