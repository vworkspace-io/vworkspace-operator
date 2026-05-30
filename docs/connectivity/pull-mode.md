# Pull mode

**Status:** Alpha — default connectivity mode.
**Last Updated:** 2026-05-30

Pull mode is the default. The operator initiates an outbound HTTPS connection to Odoo, fetches jobs targeted at its own cluster identity, applies them to its own API server, and reports status back over the same outbound channel. Odoo never opens a socket to the cluster and never holds a kubeconfig. The cluster holds an outbound bearer token (and optionally a client certificate); Odoo holds the cluster's identity record.

This page is the conceptual treatment. The wire-level HTTP contract is in [job-protocol.md](job-protocol.md). The sequence diagram is in [../diagrams/pull-mode-sequence.txt](../diagrams/pull-mode-sequence.txt).

## Why and when

Pull is the only mode that lets vWorkspace honor "the operator owns the data and the network" without forcing the operator to expose the cluster API to Odoo. Concretely:

- **Behind NAT or firewall.** Most self-hosted, homelab, and small-business clusters have no inbound public address. Pull turns the cluster's outbound HTTPS into the only required path.
- **Air-gapped or regulated edges.** A clinic, a school, or a regulated SMB can keep the cluster off the public internet and still receive intent through a single approved outbound destination (Odoo).
- **Multi-tenant SaaS with untrusted clusters.** A hosted vWorkspace operator can manage many customer clusters without ever holding their kubeconfigs. A compromised Odoo cannot directly drive a customer cluster.
- **Simpler credential model.** The cluster holds one outbound token. Odoo holds an identity row for the cluster and the public material needed to verify it. Rotating credentials is a one-side operation.

Pull is *not* a good fit when Odoo is co-located with the cluster on a trusted network — in that case Push is simpler (see [push-mode.md](push-mode.md)) — or when change control must flow through Git (see [gitops-mode.md](gitops-mode.md)).

## Protocol options

The transport between the operator and Odoo is intentionally pluggable. The trade-offs:

- **HTTP long-poll (default).** `GET /api/agent/jobs?cluster=X&wait=30s`. Trivial to operate, friendly to corporate proxies, no special middleware. This is the default shipped with the operator. It is the mode the [job protocol](job-protocol.md) is specified against.
- **gRPC bidirectional stream.** Lower latency, lower overhead at high job rates. Requires HTTP/2 end-to-end; harder through some corporate proxies.
- **Server-Sent Events (SSE).** A reasonable middle ground: one long-lived HTTPS connection, no WebSocket upgrade dance, good proxy compatibility.
- **MQTT or NATS.** Useful for fleets where Odoo is fronted by a message broker. Adds infrastructure but is well-suited to very large fleets and bursty job rates.
- **Webhooks with poll fallback.** Odoo posts to a cluster webhook *if* the cluster has an inbound address; the operator falls back to long-poll otherwise. An optimization, not the foundation.

The default ships as HTTP long-poll plus a separate batched HTTPS status endpoint. The other transports are adapters behind the same job interface. New transports are added by implementing the same job semantics; the operator's reconciliation logic does not change.

## Authentication and identity

Pull-mode auth is a small, well-scoped credential set, with all of the long-lived material on the cluster.

- **One-time registration token.** During bootstrap, an Odoo administrator (or the AI assistant on the administrator's behalf) generates a single-use token bound to a not-yet-existing cluster identity. The operator presents this token on first connect.
- **Long-lived bootstrap credential.** Odoo exchanges the one-time token for a long-lived, rotateable bearer token (and optionally a client certificate). This credential is stored only on the cluster, in a Kubernetes `Secret` the operator reads at startup.

### Operator configuration (implemented)

Enable Pull-mode in the operator Deployment:

| Source | Keys / flags |
|--------|----------------|
| Flags | `--agent-enabled=true`, `--control-plane-base-url` (alias: `--control-plane-base-url`), `--cluster-id`, `--agent-token`, `--agent-poll-interval` (default `30s`) |
| Environment | `CONTROL_PLANE_BASE_URL`, `VWORKSPACE_CLUSTER_ID`, `VWORKSPACE_AGENT_TOKEN` |
| Secret (`--agent-credentials-secret`, default name `vworkspace-agent-credentials`) | `control-plane-base-url`, `cluster-id`, `token` |

Flag and environment values override Secret data when both are set. The operator uses field manager `vworkspace-agent` and sets labels `app.vworkspace.io/managed-by=odoo` and `app.vworkspace.io/cluster-id=<cluster-id>` on applied objects.

Container image: `docker.io/vworkspace/vworkspace-operator` — see [../install/container-images.md](../install/container-images.md).
- **Cluster identity.** Every cluster has a stable identity record in Odoo: ID, display name, owning organization, public key (if mTLS or signed payloads are enabled), allowed namespaces, allowed app catalog entries, allowed operation engines.
- **Optional mTLS.** Operators who can manage a small PKI may pin Odoo's certificate and present a client certificate, with the bearer token reduced to a session marker. The default does not require mTLS; the option exists.
- **Scoped tokens per cluster.** A token can only fetch jobs for its own cluster identity. Server-side authorization enforces this for every request. Cross-cluster reads are rejected by construction.

For the request shapes that carry these credentials, see [job-protocol.md](job-protocol.md#authentication-and-versioning).

## Job model

Odoo exposes a small, stable HTTP surface for the operator. The endpoints are namespaced under `/api/agent/`. The complete wire contract — request bodies, response codes, JSON shapes — is in [job-protocol.md](job-protocol.md). The summary:

- `GET /api/agent/jobs?cluster={id}&wait={seconds}` — long-poll for jobs targeted at this cluster.
- `POST /api/agent/jobs/{jobId}/ack` — the operator has received the job and intends to apply it.
- `POST /api/agent/jobs/{jobId}/status` — interim status (reconcile progress, condition transitions).
- `POST /api/agent/jobs/{jobId}/result` — terminal result (success, failure, idempotent no-op, conflict).
- `POST /api/agent/events` — batched status, condition, and audit events not tied to a specific job.

Each Pull-mode `job` is distinct from a Kubernetes `Job` resource. The vocabulary collision is unfortunate but the documentation is explicit wherever it matters. See [../concepts/glossary.md#job-pull-mode-sense](../concepts/glossary.md#job-pull-mode-sense).

## Payload shapes

Each job carries one of two payload shapes:

- **Rendered Kubernetes object (server-side apply).** A full manifest of an `ApplicationInstance`, `Operation`, or — in advanced cases — any other CR the operator is authorized to manage. The operator applies it with server-side apply, the standard ownership labels (`app.vworkspace.io/managed-by=odoo`, `app.vworkspace.io/cluster-id={id}`; see [../api/labels-and-annotations.md](../api/labels-and-annotations.md)), and the operator's stable field manager.
- **Intent (higher-level).** A small structured record (`ensure-application-instance`, `request-operation`, `delete-application-instance`, `rotate-operator-credentials`, ...) that the operator translates locally into one or more CRs.

Both shapes converge in the cluster: the operator's reconciler only ever sees its own CRs. Pull mode does not introduce a second reconciliation loop; it introduces a new way to materialize the CRs the operator already reconciles.

The choice between rendered-object and intent payloads is an control-plane-side optimization. The operator handles both. Rendered-object payloads are easier to debug (the manifest in the job is the manifest in the cluster); intent payloads are smaller and let Odoo evolve the CR's `spec` without coordinating with the operator.

## Reconciliation interaction

Pulled intents become local CRs. Once an `ApplicationInstance` exists in the cluster's API server, everything downstream is identical across modes:

- The operator reconciles `ApplicationInstance` and produces a `HelmRelease` (see [../concepts/reconciliation-model.md](../concepts/reconciliation-model.md)).
- Flux Helm Controller reconciles the `HelmRelease`.
- `Operation` CRs drive Velero, Argo Workflows, or another engine (see [../concepts/day-2-operations.md](../concepts/day-2-operations.md)).

This is the property that makes Pull non-invasive: the in-cluster behavior is unchanged. The only difference is who wrote the CR.

## Status reporting

The operator streams status back to the control plane over the same outbound channel. Three rules:

- **Idempotent.** Each event carries a stable `eventKey` built from resource identity (`apiVersion`, `kind`, namespace, name, UID), condition type/status, and object generation. Replay is safe; Odoo (and mock control plane) de-duplicates on `eventKey`.
- **Batched.** Events are coalesced by `internal/agent/EventBatcher` (default: flush every second or when the batch reaches 100 events). Reconcilers call `internal/agent/StatusReporter` after each successful status write; changed conditions are enqueued automatically.
- **Tolerant of disconnects.** Failed `POST /api/agent/events` calls re-queue the batch and set `vworkspace_operator_connectivity_state{mode="pull"}` to `0` (reconnecting). The buffer is bounded (default 1000 events); overflow drops oldest entries.

### Implemented behavior (Phase 2)

When `--agent-enabled=true`, `cmd/main.go` starts a shared `EventBatcher` goroutine and injects `StatusReporter` into the ApplicationInstance, Operation, and Cluster reconcilers. On each condition transition:

1. The reconciler compares previous and new `status.conditions`.
2. For each changed condition, `StatusReporter` enqueues a `ConditionTransition` event with `resourceRef`, condition type/reason/message, timestamp, and `eventKey`.
3. The batcher flushes to `POST /api/agent/events` on the configured control plane base URL.

Cluster credential rotation (`spec.rotateCredentials: true`) calls `POST /api/agent/credentials/rotate`, updates `Secret/vworkspace-agent-credentials`, clears the spec flag, and posts a `CredentialRotated` audit event.

When the agent is disabled (`--agent-enabled=false`), reconcilers use a no-op reporter; in-cluster reconciliation continues unchanged.

The interim `POST /api/agent/jobs/{jobId}/status` endpoint is for per-job progress (e.g., a `HelmRelease` flipped from `Reconciling` to `Ready`). The terminal `POST /api/agent/jobs/{jobId}/result` endpoint is for the final outcome (`succeeded`, `failed`, `noop`, `conflict`). After `result` is acknowledged, the job is closed; further updates on the same `ApplicationInstance` flow through `POST /api/agent/events`.

## Offline and disconnected behavior

If the link to the control plane is broken:

- The cluster continues reconciling the last known desired state. Applications stay up. Scheduled `Operation` CRs continue to run.
- The operator queues outbound events in a bounded buffer.
- Inbound intent is paused; the operator clearly reports `Disconnected` as a top-level condition on its own `Cluster` status object so the AI assistant in Odoo can surface it when the connection returns.
- Re-establishing the connection re-syncs status first (so Odoo's view of the cluster is current), then resumes pulling new jobs.

This is a deliberate property of Pull mode: an control plane outage does not take down a running tenant cluster. Buffer overflow during a long outage is recoverable — the operator re-emits the current condition snapshot for every owned resource on reconnect — but it does mean fine-grained transition history during the outage may be lost. That trade-off is documented; the alternative (unbounded queueing) is worse in every meaningful sense.

## Drift, conflicts, and deduplication

- **Idempotent keys.** Jobs are keyed by `(resource UID, generation)` for object-shaped payloads, or by a stable intent hash for intent-shaped payloads. Replays are no-ops.
- **Server-side apply with ownership.** The operator owns specific fields under its field manager; user edits to other fields are not stomped.
- **Conflict surfacing.** If a job cannot be applied because another field manager (or a human admin) holds a field, the operator reports the conflict instead of looping. The `outcome` posted to `POST /api/agent/jobs/{jobId}/result` is `conflict`, with a structured `error` describing the field manager and field path.
- **Drift detection.** Optional periodic drift checks compare desired CR specs against live state and report (do not automatically remediate) deviations as a `Drifted` condition.

## Security

- **Outbound-only firewall.** Cluster network admins only need to allow HTTPS egress to the control plane's hostname. No inbound rules. No NAT traversal. No reverse proxies.
- **Scoped tokens per cluster.** Server-side authorization rejects cross-cluster reads. A token issued for `cluster-prod-1` cannot fetch jobs for `cluster-prod-2`.
- **Signed payloads (optional).** Job payloads can be signed by Odoo; the operator verifies before applying. This protects against a compromised relay, message broker, or man-in-the-middle.
- **Encrypted payloads (optional).** For payloads containing chart values that include secret material, payloads can be encrypted to the cluster's public key. The operator decrypts at apply time and never logs decrypted material.
- **Audit on both sides.** Odoo audits intent ("admin X asked for backup Y on cluster Z at time T"); the cluster audits actuation ("operator created Velero Backup `v-…` at time T+ε"). Both flow into the control plane audit log for the organization.

## Multi-tenant isolation

In a single-tenant vWorkspace install — the default — all clusters belong to the one organization. In an opt-in hosted offering, one vWorkspace Server install may serve many customer organizations and many clusters. Pull mode handles this cleanly:

- Each cluster only ever sees jobs targeted at its own cluster identity.
- Odoo enforces this server-side on every request, not on a hopeful "the operator wouldn't ask" basis.
- Cluster identities are scoped to a single organization. Cross-organization reads are not possible by construction.

A compromise of one cluster's outbound token grants the attacker exactly that cluster's job stream — nothing else, on any other cluster, in any other organization. This is the property that lets one vWorkspace Server install (hypothetically) serve many customer clusters without holding their kubeconfigs and without one customer's incident becoming another customer's incident.
