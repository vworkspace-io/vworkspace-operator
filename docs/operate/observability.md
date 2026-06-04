# Observability

**Status:** Alpha
**Last Updated:** 2026-06-04

`vworkspace-operator` emits the signals an operator (the person) needs to answer "is this cluster healthy and what happened on it recently". The four surfaces are Prometheus metrics, structured JSON logs, Kubernetes events on every condition transition, and an audit-event stream posted to the control plane's `POST /api/agent/events`. Each is documented below with the concrete metric names, log fields, and endpoints.

The principle is "the cluster is the source of truth for what is happening; vWorkspace Server is the place a human reads the summary". Everything the operator publishes locally to Kubernetes is replicated, coalesced, into the audit stream the AI assistant in Discuss watches; nothing important happens on the cluster without leaving a record both places.

## Prometheus metrics

The operator exposes metrics on `:8080/metrics` (the controller-runtime default). All metrics are labeled with `controller`, `result` (where applicable), and operator-specific labels described below.

### Operator-specific metrics

| Metric                                                  | Type      | Labels                                                                                          | Meaning                                                                                                                          |
|---------------------------------------------------------|-----------|-------------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------|
| `vworkspace_operator_reconcile_total`                   | Counter   | `controller=<applicationinstance|operation|cluster>`, `result=<requeue|success|error>`           | Reconciles per controller and outcome.                                                                                            |
| `vworkspace_operator_reconcile_duration_seconds`        | Histogram | `controller`                                                                                    | Reconcile wall time. Buckets: 5ms, 10ms, 25ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s, 10s.                                       |
| `vworkspace_operator_operation_total`                   | Counter   | `type=<Backup|Restore|Upgrade|Migration|RunCommand|Runbook>`, `engine=<velero|workflow|job|helm|helmHookJob|volsync|snapshot>`, `outcome=<succeeded|failed|cancelled|blocked>` | Operation completions, useful for SLO calculations per verb and per engine.                                                       |
| `vworkspace_operator_pull_job_lag_seconds`              | Gauge     | (none)                                                                                          | Age (seconds) of the oldest job the operator has fetched but not yet applied. A persistent non-zero gauge indicates a stuck loop. |
| `vworkspace_operator_connectivity_state`                | Gauge     | `mode=<pull|push|gitops>`                                                                       | `1` connected, `0` reconnecting, `-1` disconnected. The same signal feeds `Cluster.status.conditions[Connected]`.                  |
| `vworkspace_operator_applied_jobs_total`                | Counter   | (none)                                                                                          | Pull-mode jobs applied successfully (`internal/agent/metrics.go`).                                                                  |
| `vworkspace_operator_event_buffer_occupancy`            | Gauge     | (none)                                                                                          | Current depth of the outbound event buffer (Pull mode). Updated by `internal/agent/events.go` on enqueue, flush, and requeue. |
| `vworkspace_operator_managed_namespaces`                | Gauge     | (none)                                                                                          | Count of namespaces carrying `app.vworkspace.io/managed-by=vworkspace`.                                                            |
| `vworkspace_operator_credential_age_seconds`            | Gauge     | (none)                                                                                          | Age of the current bootstrap credential in seconds since the credentials Secret was last updated or rotated (`internal/agent/metrics.go`, updated on load/persist/rotation). |

### Standard controller-runtime metrics

The controller-runtime library contributes the usual workqueue, controller, and webhook metrics: `controller_runtime_reconcile_total`, `controller_runtime_reconcile_errors_total`, `controller_runtime_reconcile_time_seconds`, `workqueue_depth`, `workqueue_adds_total`, `workqueue_retries_total`, `workqueue_work_duration_seconds`, `rest_client_requests_total`, etc. We do not override these names; standard tooling and dashboards work unchanged.

### Recommended SLOs (starting point)

| SLO                                                              | Target                                                                                                                 |
|------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------|
| 99% of `reconcile`s complete in under 1s for `applicationinstance`. | `histogram_quantile(0.99, sum by (le) (rate(vworkspace_operator_reconcile_duration_seconds_bucket{controller="applicationinstance"}[5m]))) < 1` |
| 99% of `Backup` operations succeed within 30 minutes (catalog default). | Track via `vworkspace_operator_operation_total{type="Backup",outcome="succeeded"} / sum(vworkspace_operator_operation_total{type="Backup"})`. |
| Pull-job lag stays under 60s.                                    | `vworkspace_operator_pull_job_lag_seconds < 60`.                                                                       |
| Connectivity state is `1` (connected) over the last 5 minutes.   | `avg_over_time(vworkspace_operator_connectivity_state[5m]) > 0.99`.                                                    |

The SLOs are starting points; an organization is expected to tune them to its own targets.

## Structured logs

The operator's logger is the controller-runtime logger (zap under the hood), configured to emit JSON. Every log line carries a stable set of fields:

| Field                  | Type   | When set                                                                                                                       |
|------------------------|--------|--------------------------------------------------------------------------------------------------------------------------------|
| `level`                | string | `info`, `warn`, `error`, `debug`.                                                                                              |
| `ts`                   | string | RFC3339 timestamp.                                                                                                              |
| `msg`                  | string | The message.                                                                                                                    |
| `cluster_id`           | string | The cluster's identity (`Cluster.metadata.name`). Always set.                                                                   |
| `org_id`               | string | The owning organization identity. Always set.                                                                                   |
| `namespace`            | string | The namespace the reconcile is operating in. Set on namespaced reconciles.                                                     |
| `application_instance` | string | `ApplicationInstance` name. Set in the ApplicationInstance reconciler.                                                          |
| `operation_id`         | string | `Operation.metadata.uid`. Set in the Operation reconciler.                                                                     |
| `operator_version`     | string | The operator binary's version (semver, e.g., `v0.4.2`). Always set.                                                            |
| `controller`           | string | Which controller emitted the line (`applicationinstance`, `operation`, `cluster`).                                              |
| `reconcileID`          | string | Per-reconcile correlation id.                                                                                                  |

Log levels in practice:

- `info`: every condition transition, every external write (Helm Release apply, Velero Backup create, Workflow create, Job create), reconciliation start and end, audit-event send.
- `warn`: retryable failures (control plane unreachable but bootstrap credential intact), admission warnings, operations entering `Blocked`.
- `error`: non-retryable failures, panics caught by controller-runtime, credential rotation failures.
- `debug`: per-step reconciliation traces; off by default. Enabled with `--zap-log-level=debug`.

Sensitive values (chart-value secrets, the operator's own bootstrap credential) are redacted ([../security/secrets-handling.md](../security/secrets-handling.md)). The structured shape lets `kubectl logs ... | jq` and any log-aggregation backend slice and dice by cluster, org, namespace, application instance, or operation id without parsing strings.

## Kubernetes events

The operator emits a Kubernetes `Event` on every condition transition. This makes `kubectl describe applicationinstance/<name>` and `kubectl describe operation/<name>` informative without leaving the cluster. The events are:

| Event reason                       | Object                | When                                                                                                 |
|------------------------------------|-----------------------|------------------------------------------------------------------------------------------------------|
| `ReconcileStarted`                 | `ApplicationInstance` | The reconciler begins a generation.                                                                  |
| `ReconcileSucceeded`               | `ApplicationInstance` | A generation completes with `Ready=True`.                                                            |
| `ReconcileFailed`                  | `ApplicationInstance` | A generation completes with `Failed=True`. The message is the reason.                                |
| `HelmReleaseUpgraded`              | `ApplicationInstance` | The underlying `HelmRelease` transitions to a new revision.                                          |
| `OperationAccepted`                | `Operation`           | Validation passed; child resource creation imminent.                                                  |
| `OperationBlocked`                 | `Operation`           | A precondition is unmet; the message names the precondition.                                          |
| `OperationRunning`                 | `Operation`           | The child resource (Backup, Workflow, Job, ...) is running.                                            |
| `OperationSucceeded`               | `Operation`           | The child resource succeeded. Outputs are recorded on the Operation.                                  |
| `OperationFailed`                  | `Operation`           | The child resource failed. The message includes the engine-specific reason.                            |
| `ConnectivityConnected`            | `Cluster`             | The operator established or re-established the outbound connection to Odoo.                          |
| `ConnectivityLost`                 | `Cluster`             | The outbound connection has been failing for the configured grace period.                            |
| `CredentialRotated`                | `Cluster`             | The bootstrap credential was rotated.                                                                 |

Events are namespace-local for `ApplicationInstance` and `Operation`; the `Cluster` events are in `vworkspace-system`. Standard event-aggregation tools (Kubernetes' built-in event aggregation; Argo CD's event sources; observability backends that ingest events) work without modification.

## Audit events to Odoo

Significant cluster-side outcomes are also posted to the control plane's `POST /api/agent/events` (Pull mode) as batched `ConditionTransition` and direct audit kinds. The wire shape, kind taxonomy, idempotency keys, and alignment with server `vws_audit` ingest are documented in [audit-events.md](audit-events.md).

At a glance: reconcilers enqueue events via `StatusReporter`; the batcher flushes every second or at 100 events. Stable `eventKey` values deduplicate replay. When the link to the control plane is down, events queue in a bounded buffer (`vworkspace_operator_event_buffer_occupancy`) and flush on reconnect. Buffer overflow sets `Cluster.status.conditions[BufferOverflow=True]` (see [audit-events.md](audit-events.md#connectivity-and-heartbeat-not-durable-audit)).

Durable audit entries and high-signal Discuss posts are created on the server from ingested agent events; control-plane user actions (approve, deploy) are separate `log_user_action` rows. The human operator and the AI assistant share one organization timeline in Discuss.

## Health endpoints

The operator exposes two endpoints on `:8081`:

- `/healthz` — liveness. Returns `200 OK` while the process is running; `Kubernetes` restarts the pod if this fails.
- `/readyz` — readiness. Returns `200 OK` when the operator has loaded CRDs, validated RBAC, established (or re-established) the Odoo connection (Pull mode), and is ready to reconcile. Returns `503` otherwise; Kubernetes withholds traffic on this state. The body includes a small JSON summary of which sub-check failed.

A failing `/readyz` is a recoverable condition; a failing `/healthz` indicates a deeper bug worth filing an issue.

## The `Cluster` CR as the overall health surface

The `Cluster` CR is the single object an operator (or a script, or the AI assistant) reads to learn whether the cluster is operational. Its `status` summarizes everything else:

```yaml
status:
  observedGeneration: 4
  operatorVersion: v0.4.2
  fluxVersion: v2.3.0
  veleroVersion: v1.14.0
  managedNamespaces:
    - { name: org-myteam, allowedOperationTemplates: ["backup.velero", "restore.velero", "upgrade.helm"] }
  lastHeartbeat: "2026-05-28T10:07:13Z"
  conditions:
    - { type: Connected,              status: "True",  reason: ControlPlaneReachable,        lastTransitionTime: "2026-05-28T08:00:00Z", message: "Last successful round-trip 4s ago" }
    - { type: Authenticated,          status: "True",  reason: CredentialValid,      lastTransitionTime: "2026-05-28T08:00:00Z" }
    - { type: ControllersHealthy,     status: "True",  reason: AllControllersReady,  lastTransitionTime: "2026-05-28T08:00:00Z" }
    - { type: CRDsRegistered,         status: "True",  reason: SchemaMatches,        lastTransitionTime: "2026-05-28T08:00:00Z" }
    - { type: Disconnected,           status: "False", reason: NotApplicable }
```

When a problem occurs, the relevant condition flips. The AI assistant in Odoo reads the same status surface and produces the equivalent human-readable summary in Discuss.

## Putting it together: the four-pane overview

A working observability setup for a vWorkspace cluster has four panes:

1. **Cluster status pane.** `kubectl get cluster -n vworkspace-system -o yaml` (or its dashboard equivalent). Answers "is the operator healthy".
2. **Application status pane.** `kubectl get applicationinstance -A` plus a per-app drill-down. Answers "is each application healthy".
3. **Operation status pane.** `kubectl get operation -A` plus a per-op drill-down. Answers "what is happening right now and what happened recently".
4. **Operator metrics pane.** A small Grafana dashboard with the operator-specific metrics above. Answers "is the operator itself behaving".

Add to these the Odoo Discuss timeline for the organization, and the human operator and the AI assistant have a complete picture.

## Related material

- [audit-events.md](audit-events.md) — Agent event kinds, `eventKey` rules, and `vws_audit` ingest alignment.
- [troubleshooting.md](troubleshooting.md) — How to follow each of these signals to a root cause.
- [upgrades.md](upgrades.md) — Version-skew rules visible via `Cluster.status.operatorVersion`.
- [../security/threat-model.md](../security/threat-model.md) — Why structured logs redact secrets.
- [../api/conditions.md](../api/conditions.md) — Full condition reason vocabulary.
