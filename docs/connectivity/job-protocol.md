# Pull-mode job protocol

**Status:** Alpha — version `application/vnd.vworkspace.agent.v1+json`.
**Last Updated:** 2026-05-30

This page is the wire-level HTTP contract between the operator (in Pull mode) and Odoo. It specifies the endpoints, the request and response shapes, the authentication model, the error codes, and the versioning convention. The conceptual treatment is in [pull-mode.md](pull-mode.md); the sequence diagram is in [../diagrams/pull-mode-sequence.txt](../diagrams/pull-mode-sequence.txt).

All endpoints live under `/api/agent/` on the Odoo host the operator is registered against. All requests and responses are JSON unless otherwise noted. All bodies are encoded UTF-8.

## Authentication and versioning

Every request carries two headers:

- `Authorization: Bearer <token>` — the long-lived bootstrap credential the operator received during registration. The token is bound to one cluster identity; server-side authorization rejects any request for or about a different cluster.
- `Accept: application/vnd.vworkspace.agent.v1+json` — the media type the operator understands. Future major versions will use a different media type (`v2+json`, etc.). Odoo may serve multiple media types at once; the operator declares which one it speaks.

`Content-Type: application/vnd.vworkspace.agent.v1+json` is set on every request with a body. Responses use the same content type.

Optional client mTLS may be configured at the TLS layer; it does not change the application-layer protocol.

## Endpoints

### `GET /api/agent/jobs`

Long-poll for jobs targeted at the calling cluster.

**Query parameters.**

- `cluster` (required) — the cluster identity. Must match the cluster identity bound to the bearer token; server returns `403` if it does not.
- `wait` (optional, default `30`, max `300`) — the number of seconds to hold the connection open if no jobs are immediately available. The server returns `200 OK` with an empty `jobs` array if the wait elapses.

**Success response: `200 OK`.**

Body:

```json
{
  "jobs": [
    {
      "id": "j-01HXYZA1B2C3",
      "kind": "apply",
      "payload": {
        "apiVersion": "apps.vworkspace.io/v1alpha1",
        "kind": "ApplicationInstance",
        "metadata": {
          "name": "nextcloud-myteam",
          "namespace": "org-myteam",
          "labels": {
            "app.vworkspace.io/managed-by": "odoo",
            "app.vworkspace.io/cluster-id": "cluster-prod-1"
          }
        },
        "spec": { "...": "..." }
      },
      "idempotencyKey": "applicationinstance/org-myteam/nextcloud-myteam@7",
      "createdAt": "2026-05-28T10:00:00Z",
      "expiresAt": "2026-05-28T10:15:00Z",
      "signature": "base64-detached-signature-over-payload"
    }
  ]
}
```

**Field semantics.**

- `id` — a stable, opaque job ID. The same ID is used in the `ack`, `status`, and `result` endpoints.
- `kind` — one of `apply`, `delete`, or `intent`. `apply` carries a rendered Kubernetes object; `delete` carries a reference (`apiVersion`, `kind`, `name`, `namespace`) for deletion; `intent` carries a higher-level structured record (see [pull-mode.md](pull-mode.md#payload-shapes)).
- `payload` — for `apply`, a full manifest. For `delete`, an object reference. For `intent`, a structured intent record.
- `idempotencyKey` — a stable key that the operator uses to de-duplicate replays. For object-shaped payloads the convention is `<kind>/<namespace>/<name>@<generation>` (or a stable hash for intent payloads). Re-delivering a job with the same `idempotencyKey` produces a no-op.
- `createdAt` / `expiresAt` — RFC 3339 timestamps. The operator should skip jobs whose `expiresAt` is in the past and post a `result` with `outcome: noop` and `error: "expired"`.
- `signature` (optional) — present when signed-payload mode is enabled. A detached signature over the canonical JSON encoding of `payload`, made with Odoo's signing key for this cluster. The operator verifies before applying.

### `POST /api/agent/jobs/{jobId}/ack`

Acknowledge receipt of a job. The operator MUST call `ack` before applying. `ack` tells Odoo the operator intends to act on the job and authorizes Odoo to stop returning it from subsequent long-polls.

**Request body.** None (empty).

**Success response: `204 No Content`.**

### `POST /api/agent/jobs/{jobId}/status`

Post interim status for an in-flight job. Used for progress reporting (e.g., the `HelmRelease` is `Reconciling`). May be called any number of times between `ack` and `result`.

**Request body.**

```json
{
  "phase": "Reconciling",
  "conditions": [
    {
      "type": "Reconciling",
      "status": "True",
      "reason": "HelmReleaseUpgrading",
      "message": "Flux is upgrading nextcloud-myteam to chart version 6.6.0",
      "lastTransitionTime": "2026-05-28T10:00:05Z"
    }
  ],
  "message": "Flux is upgrading nextcloud-myteam to chart version 6.6.0",
  "timestamp": "2026-05-28T10:00:05Z"
}
```

**Success response: `204 No Content`.**

### `POST /api/agent/jobs/{jobId}/result`

Post the terminal result of a job. Must be called exactly once per job. After Odoo acknowledges `result`, the job is closed; further updates on the resource flow through `POST /api/agent/events`.

**Request body.**

```json
{
  "outcome": "succeeded",
  "appliedRef": {
    "apiVersion": "apps.vworkspace.io/v1alpha1",
    "kind": "ApplicationInstance",
    "namespace": "org-myteam",
    "name": "nextcloud-myteam",
    "uid": "8c9e5a3d-2e9c-4c8a-9c0a-1e3a4b5c6d7e",
    "generation": 7
  },
  "timestamp": "2026-05-28T10:00:15Z"
}
```

`outcome` is one of:

- `succeeded` — the job was applied and the resource reached `Ready=True` (for `apply`) or was deleted (for `delete`).
- `failed` — the job could not be applied or the resulting resource entered a terminal failure condition. `error` MUST be populated.
- `noop` — the job was already applied at this `generation` (idempotent replay) or was already expired. `error` is optional.
- `conflict` — the apply failed because another field manager holds a field the operator must own. `error` describes the conflicting field manager and field path.

A failed example:

```json
{
  "outcome": "failed",
  "error": "HelmReleaseFailed: chart repository unreachable: dial tcp 10.0.0.1:443: i/o timeout",
  "appliedRef": {
    "apiVersion": "apps.vworkspace.io/v1alpha1",
    "kind": "ApplicationInstance",
    "namespace": "org-myteam",
    "name": "nextcloud-myteam",
    "uid": "8c9e5a3d-2e9c-4c8a-9c0a-1e3a4b5c6d7e",
    "generation": 7
  },
  "timestamp": "2026-05-28T10:01:00Z"
}
```

**Success response: `204 No Content`.**

### `POST /api/agent/events`

Post a batched set of status, condition, and audit events that are not tied to a specific job. The canonical example is a `HelmRelease` that has just flipped to `Degraded` because the chart repository became temporarily unreachable, with no in-flight job to attribute that to.

**Request body.**

```json
{
  "events": [
    {
      "kind": "ConditionTransition",
      "resourceRef": {
        "apiVersion": "apps.vworkspace.io/v1alpha1",
        "kind": "ApplicationInstance",
        "namespace": "org-myteam",
        "name": "nextcloud-myteam",
        "uid": "8c9e5a3d-2e9c-4c8a-9c0a-1e3a4b5c6d7e"
      },
      "conditions": [
        {
          "type": "Ready",
          "status": "True",
          "reason": "HelmReleaseReady",
          "message": "Release reconciliation succeeded.",
          "lastTransitionTime": "2026-05-28T10:01:30Z"
        }
      ],
      "timestamp": "2026-05-28T10:01:30Z"
    },
    {
      "kind": "ClusterHeartbeat",
      "resourceRef": {
        "apiVersion": "ops.vworkspace.io/v1alpha1",
        "kind": "Cluster",
        "name": "cluster-prod-1"
      },
      "conditions": [
        {
          "type": "ControllersHealthy",
          "status": "True",
          "reason": "AllControllersReady",
          "message": "Flux, Velero, and cert-manager all report Ready.",
          "lastTransitionTime": "2026-05-28T10:01:30Z"
        }
      ],
      "timestamp": "2026-05-28T10:01:30Z"
    }
  ]
}
```

**Field semantics.**

- `events[]` — an array of events. The operator batches up to a configurable size threshold (default 100) and time threshold (default one second), whichever is reached first. Empty batches are not sent.
- `kind` — the event kind. Standard kinds include `ConditionTransition` (a condition on an owned resource changed), `ClusterHeartbeat` (a periodic snapshot of the operator's own `Cluster` status), `Audit` (a notable action, e.g. an `Operation` was admitted or rejected), `BufferOverflow` (the operator's outbound buffer dropped events during a disconnect).
- `resourceRef` — the object the event is about. UID is included where possible to disambiguate recreated objects.

**Success response: `204 No Content`.**

## Error responses

All error responses share the shape:

```json
{
  "error": "short_machine_readable_code",
  "message": "human-readable explanation",
  "requestId": "rq-01HXYZA1B2C3"
}
```

Standard status codes:

- `401 Unauthorized` — missing, malformed, or expired bearer token. The operator should attempt one credential refresh; if the refresh also returns `401`, the operator surfaces `Authenticated=False` on its `Cluster` status and stops polling until an admin intervenes.
- `403 Forbidden` — the token is valid, but the request references a cluster identity the token is not authorized for, an unknown organization, or a forbidden operation. Typical causes: `cluster` query parameter does not match the token's cluster identity; the cluster's identity record has been disabled. The operator does not retry; it surfaces the condition and waits for re-registration.
- `404 Not Found` — the referenced `jobId` does not exist (already closed, never existed, or expired and pruned). The operator treats this as a no-op for the affected job. For other `404` cases (unknown endpoint), the operator surfaces a configuration error.
- `409 Conflict` — the `result` for this job has already been recorded. The operator treats this as success and proceeds. May also be returned by `ack` if the job is already in a closed state.
- `429 Too Many Requests` — rate limit hit. The response carries a `Retry-After` header (seconds) that the operator MUST honor. The operator's poll loop backs off exponentially within the limits the server suggests.
- `5xx` — server error. The operator retries with exponential backoff and jitter, never faster than once per second. Persistent `5xx` errors surface as `Connected=False` on the `Cluster` status.

## Payload signing and encryption

The protocol supports two optional security layers above TLS, configured at the cluster identity level.

- **Signed payloads.** When enabled, every job in `GET /api/agent/jobs` includes a `signature` field — a detached signature over the canonical JSON encoding of `payload`, made with Odoo's signing key for this cluster. The operator verifies the signature before applying; verification failure is logged and reported as a job result with `outcome: failed` and an explicit `error`. The signing key is bound to the cluster identity record in Odoo; rotating it is a server-side operation.
- **Encrypted payloads.** When enabled, `payload` is replaced with `encryptedPayload`, a JWE encrypted to the cluster's public key. The cluster decrypts at apply time. This is useful when payloads carry chart values that include secret material. The operator never logs decrypted material; only the apply outcome is logged.

Signing and encryption are independent: a cluster can enable signing, encryption, both, or neither. The default deployment enables neither (TLS plus the bearer token model is considered sufficient); regulated deployments often enable both.

## Versioning

The protocol is versioned via the media type `application/vnd.vworkspace.agent.v1+json`. Backwards-compatible changes (new optional fields, new endpoints) extend `v1`. Backwards-incompatible changes mint a new media type (`v2+json`). Odoo can serve multiple media types simultaneously, allowing fleet-wide rollouts of the operator at different paces per cluster.

Within `v1`, fields the operator does not recognize MUST be ignored. Fields Odoo does not recognize on incoming requests are returned as `400 Bad Request` only when they violate a documented constraint (e.g., an unknown `outcome` value); arbitrary additional fields are tolerated for forward compatibility.

The full conceptual treatment of why the protocol is shaped this way is in [pull-mode.md](pull-mode.md).
