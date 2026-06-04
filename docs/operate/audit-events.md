# Audit events (agent → control plane)

**Status:** Alpha
**Last Updated:** 2026-06-04

Pull-mode clusters report what happened on the cluster by posting batched events to `POST /api/agent/events`. vWorkspace Server stores high-signal agent events as durable `vws.audit.entry` records (`vws_audit` module) and mirrors deploy, backup, and credential-rotate outcomes to the organization's Discuss audit channel. Control-plane user actions (operation approve, app deploy/update) are recorded separately via `log_user_action` on the server; they do not use this wire format.

This document is the operator-side contract: event shape, kinds, which reconcilers emit them, and how that lines up with server ingest. For metrics, logs, and Kubernetes `Event` objects, see [observability.md](observability.md). For the HTTP endpoint and batching defaults, see [../connectivity/job-protocol.md](../connectivity/job-protocol.md#post-apagentevents) and [../connectivity/pull-mode.md](../connectivity/pull-mode.md#status-reporting).

## Transport and batching

| Property | Value |
|----------|--------|
| Endpoint | `POST /api/agent/events` on `Cluster.spec.controlPlaneBaseUrl` |
| Auth | Cluster bootstrap bearer token (same as job poll) |
| Success | `204 No Content` |
| Implementation | `internal/agent/EventBatcher`, `internal/agent/HTTPClient.PostEvents` |
| Default batch | Flush every 1s or when 100 events are queued |
| Buffer | Bounded (default 1000); overflow drops oldest entries and sets `Cluster.status.conditions[BufferOverflow=True]` |

Reconcilers call `StatusReporter` after each successful status write. Failed posts re-queue the batch and drive `vworkspace_operator_connectivity_state{mode="pull"}` toward reconnecting ([observability.md](observability.md)).

## Wire shape

Each event in the request body matches `internal/agent.Event`:

```json
{
  "events": [
    {
      "eventKey": "condition/apps.vworkspace.io/v1alpha1/ApplicationInstance/org-myteam/nextcloud-myteam/8c9e5a3d-2e9c-4c8a-9c0a-1e3a4b5c6d7e/Ready/True@7",
      "kind": "ConditionTransition",
      "resourceRef": {
        "apiVersion": "apps.vworkspace.io/v1alpha1",
        "kind": "ApplicationInstance",
        "namespace": "org-myteam",
        "name": "nextcloud-myteam",
        "uid": "8c9e5a3d-2e9c-4c8a-9c0a-1e3a4b5c6d7e",
        "generation": 7
      },
      "conditions": [
        {
          "type": "Ready",
          "status": "True",
          "reason": "HelmReleaseReady",
          "message": "Release reconciliation succeeded.",
          "lastTransitionTime": "2026-06-04T10:01:30Z"
        }
      ],
      "timestamp": "2026-06-04T10:01:30Z"
    }
  ]
}
```

| Field | Meaning |
|-------|---------|
| `eventKey` | Stable idempotency key. Condition transitions use `ConditionEventKey` (`condition/{apiVersion}/{kind}/{namespace}/{name}/{uid}/{type}/{status}@{generation}`). Direct audit kinds use `{kind}/{uid}/{conditionType}` when conditions are present. |
| `kind` | Event taxonomy (see below). |
| `resourceRef` | The CR the event is about (`ApplicationInstance`, `Operation`, or `Cluster`). UID and generation are set when available. |
| `conditions` | One or more Kubernetes-style condition objects (`type`, `status`, `reason`, `message`, `lastTransitionTime`). |
| `timestamp` | UTC time the operator enqueued the event (RFC3339). |

The control plane deduplicates on `eventKey` before ingest. Replay after reconnect is safe.

## Event kinds

### `ConditionTransition` (primary path)

Emitted when `status.conditions` change on an owned resource (`ReportConditionTransitions` in `internal/agent/reporter.go`). Controllers wired today:

| Controller | `resourceRef.kind` |
|------------|-------------------|
| ApplicationInstance | `ApplicationInstance` |
| Operation | `Operation` |
| Cluster | `Cluster` |

Only changed conditions are sent (status, reason, or message differ from the previous reconcile).

**Server ingest (`vws_audit`).** A `ConditionTransition` creates a durable audit entry when:

- `resourceRef.kind` is `Operation`, `ApplicationInstance`, or `Cluster`, and
- at least one condition in the payload has `(type, status)` in the high-signal set: `Succeeded/True`, `Failed/True`, `Running/True`, `Cancelled/True`, `Blocked/True`, `Connected/True`, `Disconnected/True`.

The audit summary is built from `type`, `reason`, and `message` on the matching condition. Operation lifecycle outcomes are usually observed this way—for example `Operation` with `type: Succeeded`, `status: True`, `reason: VeleroBackupCompleted`—rather than as a separate top-level `kind`.

Discuss posts (deploy, backup, credential rotate) key off **condition `reason`** values on ingested entries; see [server `vws_audit` constants](https://github.com/vworkspace-io/vworkspace-server/blob/main/addons/vws_audit/models/vws_audit_constants.py) (`DISCUSS_DEPLOY_REASONS`, `DISCUSS_BACKUP_REASONS`, `DISCUSS_CREDENTIAL_REASONS`). Operator Kubernetes event reasons in [observability.md](observability.md) and condition vocabulary in [../api/conditions.md](../api/conditions.md) are the source for those strings.

### Direct audit kinds (`ReportAudit`)

Direct kinds use the same JSON shape but set `kind` to the action name and attach one or more conditions under `conditions[]`. Server ingest requires `resourceRef.kind` to match the kind (see server `DIRECT_AUDIT_KIND_RESOURCE_KINDS`).

| `kind` | Required `resourceRef.kind` | Operator emission today |
|--------|----------------------------|-------------------------|
| `CredentialRotated` | `Cluster` | Yes — after successful `POST /api/agent/credentials/rotate` (`cluster_controller.go`) |
| `OperationAccepted` | `Operation` | Wire contract; server ingests; prefer `ConditionTransition` on `Accepted` until operator calls `ReportAudit` |
| `OperationBlocked` | `Operation` | Wire contract; server ingests |
| `OperationRunning` | `Operation` | Wire contract; server ingests |
| `OperationSucceeded` | `Operation` | Wire contract; server ingests |
| `OperationFailed` | `Operation` | Wire contract; server ingests |

`ReportAudit` is the extension point for lifecycle events that should appear in the audit trail without piggybacking on a condition-type transition. New direct kinds must be added in lockstep on the server (`vws_audit` constants and ingest rules).

### Connectivity and heartbeat (not durable audit)

| `kind` | Purpose | `vws_audit` durable entry |
|--------|---------|---------------------------|
| `ClusterHeartbeat` | Periodic snapshot of `Cluster.status.conditions` (`HTTPClient.Heartbeat`) | No — domain/status sync only |
| *(buffer overflow)* | Reflected on `Cluster` as `BufferOverflow=True`; not a separate outbound `kind` | No — recover via condition snapshot on reconnect |

Older drafts referred to a generic `Audit` or `BufferOverflow` event kind; the implemented protocol uses `ConditionTransition` / direct kinds above and the `BufferOverflow` **condition** on `Cluster`.

## Alignment with control-plane user actions

Server-side user actions are **not** posted by the operator. They are recorded with `source=user` and kinds such as `OperationApproved`, `Deploy`, `AppUpdate`, and `CredentialRotate` (admin/UI hooks). Agent events remain `source=agent`.

| Source | Examples | Path |
|--------|----------|------|
| Agent (this doc) | Condition transitions, `CredentialRotated`, future `ReportAudit` operation kinds | `POST /api/agent/events` |
| User (control plane) | Approve operation, deploy app, update app from UI | `log_user_action` in `vws_audit` |

Intent ("admin requested backup") and actuation ("Velero backup completed") therefore appear as separate audit rows with different `source` values; both are visible in the organization audit log and Discuss timeline ([../connectivity/pull-mode.md](../connectivity/pull-mode.md#security)).

## Condition and reason reference

For a full closed-set vocabulary of condition types and reasons on each CRD, see [../api/conditions.md](../api/conditions.md). Kubernetes `Event` reasons in [observability.md](observability.md) align with audit payloads where the same reconcile step emits both.

High-signal examples commonly ingested into `vws_audit` and Discuss:

| Area | Example condition | Example `reason` (Discuss-eligible where noted) |
|------|-------------------|--------------------------------------------------|
| Application | `Ready=True` | `HelmReleaseReady`, `HelmReleaseUpgraded` (deploy) |
| Application | `Degraded=True` | `HelmReleaseUpgradeFailed` (deploy) |
| Operation | `Succeeded=True` | `VeleroBackupCompleted`, `VeleroRestoreCompleted` (backup) |
| Operation | `Failed=True` | `VeleroBackupFailed`, `VeleroRestoreFailed` (backup) |
| Cluster | `Authenticated=True` | `CredentialRotated` (credential; also direct kind) |

## Implementation map

```
Reconciler status write
    → StatusReporter.ReportConditionTransitions / ReportAudit
        → EventBatcher.Enqueue
            → EventBatcher.Flush → POST /api/agent/events
                → vws.agent.event (server)
                    → vws.audit.entry (vws_audit ingest)
                    → Discuss channel (high-signal reasons)
```

Code references: `internal/agent/events.go` (`ConditionTransitionEvent`), `internal/agent/reporter.go`, `internal/controller/applicationinstance_controller.go`, `internal/controller/operation_controller.go`, `internal/controller/cluster_controller.go`.

## Related material

- [observability.md](observability.md) — Metrics, structured logs, Kubernetes events, buffer gauge.
- [../connectivity/job-protocol.md](../connectivity/job-protocol.md) — Full `POST /api/agent/events` section.
- [../connectivity/pull-mode.md](../connectivity/pull-mode.md) — When the agent reporter is enabled and offline behavior.
- Server: [addons/vws_audit/readme/AGENT_EVENT_KINDS.rst](https://github.com/vworkspace-io/vworkspace-server/blob/main/addons/vws_audit/readme/AGENT_EVENT_KINDS.rst) — Ingest rules and deferred items.
- Server: [addons/vws_audit/models/vws_audit_constants.py](https://github.com/vworkspace-io/vworkspace-server/blob/main/addons/vws_audit/models/vws_audit_constants.py) — Kind and Discuss reason constants shared with this doc.
