# Observability

**Status:** Alpha
**Last Updated:** 2026-06-04 (hub #6 spoke 1 — scrape + catalog)

`vworkspace-operator` emits the signals an operator (the person) needs to answer "is this cluster healthy and what happened on it recently". The four surfaces are Prometheus metrics, structured JSON logs, Kubernetes events on every condition transition, and an audit-event stream posted to the control plane's `POST /api/agent/events`. Each is documented below with the concrete metric names, log fields, and endpoints.

The principle is "the cluster is the source of truth for what is happening; vWorkspace Server is the place a human reads the summary". Everything the operator publishes locally to Kubernetes is replicated, coalesced, into the audit stream the AI assistant in Discuss watches; nothing important happens on the cluster without leaving a record both places.

## Prometheus metrics

### Endpoints

| Install path | Metrics bind address | Path | TLS |
|--------------|----------------------|------|-----|
| Kustomize (`make deploy`) | `:8443` via `config/default/manager_metrics_patch.yaml` | `/metrics` | HTTPS (controller-runtime secure metrics; RBAC-filtered) |
| Helm chart (default) | `0` (disabled) | — | — |
| Helm (enable) | `--set manager.metricsBindAddress=:8443` | `/metrics` | HTTPS when non-zero bind address |

Health probes stay on `:8081` (`/healthz`, `/readyz`). Do not confuse probe port `8081` with the metrics listener.

When `metrics-bind-address` is `0`, the process does not listen for scrapes. Production clusters should enable metrics and scrape via Prometheus Operator or an equivalent agent.

### Operator metrics (`vworkspace_operator_*`)

Registered in `internal/agent/metrics.go` (Pull-mode agent and credential signals):

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `vworkspace_operator_pull_job_lag_seconds` | Gauge | (none) | Age (seconds) of the oldest Pull-mode job fetched but not yet applied. Persistent non-zero values suggest a stuck applier loop. |
| `vworkspace_operator_connectivity_state` | Gauge | `mode=<pull\|push\|gitops>` | `1` connected, `0` reconnecting, `-1` disconnected. Feeds `Cluster.status.conditions[Connected]`. |
| `vworkspace_operator_applied_jobs_total` | Counter | (none) | Pull-mode jobs applied successfully. |
| `vworkspace_operator_event_buffer_occupancy` | Gauge | (none) | Outbound event buffer depth (Pull mode). Updated on enqueue, flush, and requeue (`internal/agent/events.go`). |
| `vworkspace_operator_credential_age_seconds` | Gauge | (none) | Seconds since the bootstrap credentials Secret was last updated or rotated. |

**Planned (later hub #6 spokes):** `vworkspace_operator_operation_total`, `vworkspace_operator_managed_namespaces`, and dedicated reconcile outcome counters. Until then, use controller-runtime metrics below for reconcile latency and errors.

### Controller-runtime metrics (reconcile and workqueue)

The manager registers the standard controller-runtime registry. Use these names in Grafana/Prometheus (controller names are lowercased CRD kinds):

| Metric | Type | Labels | Use |
|--------|------|--------|-----|
| `controller_runtime_reconcile_total` | Counter | `controller`, `result` | Reconcile volume and outcome per controller. |
| `controller_runtime_reconcile_errors_total` | Counter | `controller` | Non-success reconciles. |
| `controller_runtime_reconcile_time_seconds` | Histogram | `controller` | Reconcile wall time (use for latency SLOs). |
| `workqueue_depth` | Gauge | `name` | Backlog per workqueue. |
| `workqueue_adds_total`, `workqueue_retries_total` | Counter | `name` | Queue churn. |
| `rest_client_requests_total` | Counter | `code`, `method` | Kubernetes API pressure. |

We do not rename these series; kube-prometheus, Grafana mixins, and upstream dashboards work unchanged.

### Prometheus scrape

#### Kustomize + Prometheus Operator

1. Confirm metrics are enabled (default overlay already patches `--metrics-bind-address=:8443` and adds `controller-manager-metrics-service` on port `8443`).
2. Install [Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator) in the cluster (or use an existing kube-prometheus stack).
3. In `config/default/kustomization.yaml`, uncomment the `[PROMETHEUS]` line to include `../prometheus` (ServiceMonitor `controller-manager-metrics-monitor`).
4. For production TLS on the metrics Service, follow the `[METRICS-WITH-CERTS]` / `[CERTMANAGER]` comments in the same file and `config/prometheus/monitor_tls_patch.yaml`.
5. Apply: `make deploy IMG=<your-operator-image>`.

The ServiceMonitor scrapes `https` on Service port `https` → pod `8443`, path `/metrics`, with the pod service account token (see `config/prometheus/monitor.yaml`).

#### Helm

```bash
helm upgrade --install vworkspace-operator ./charts/vworkspace-operator \
  -n vworkspace-system \
  --set manager.metricsBindAddress=:8443
```

The chart does not ship a metrics `Service` by default. Either add one in your platform repo (port `8443`, name `https`) or scrape via `PodMonitor` on the manager pod. Match endpoint scheme `https`, port `8443`, and bearer token auth — same contract as the kustomize scaffold.

#### Manual verification (no Prometheus Operator)

```bash
NS=vworkspace-operator-system   # Helm: vworkspace-system (or your release namespace)
SA=vworkspace-operator-controller-manager
TOKEN=$(kubectl -n "${NS}" create token "${SA}")

# Kustomize (make deploy): metrics Service exists after namePrefix
kubectl -n "${NS}" port-forward svc/vworkspace-operator-controller-manager-metrics-service 8443:8443

# Helm (metrics enabled, no metrics Service): port-forward the manager pod instead
# POD=$(kubectl -n "${NS}" get pod -l control-plane=controller-manager -o jsonpath='{.items[0].metadata.name}')
# kubectl -n "${NS}" port-forward pod/"${POD}" 8443:8443

curl -sk --header "Authorization: Bearer ${TOKEN}" https://127.0.0.1:8443/metrics | grep -E '^vworkspace_operator_|^controller_runtime_reconcile'
```

Expect `vworkspace_operator_*` series when Pull-mode agent is enabled; connectivity and buffer gauges matter most for control-plane-linked clusters.

#### RBAC

Secure metrics use controller-runtime's authentication/authorization filter. Prometheus needs a `ClusterRole` that can `GET /metrics` non-resource URL and a `ClusterRoleBinding` to the scrape ServiceAccount.

After `make deploy`, Kustomize applies `namePrefix: vworkspace-operator-` (`config/default/kustomization.yaml`), so the deployed ClusterRole is **`vworkspace-operator-metrics-reader`** (source manifest `metrics-reader` in `config/rbac/metrics_reader_role.yaml`). Bindings must reference that prefixed name — e.g. e2e creates `vworkspace-operator-metrics-binding` with `--clusterrole=vworkspace-operator-metrics-reader` (`test/e2e/e2e_test.go`).

### Recommended SLOs (starting point)

| SLO | PromQL / rule (starting point) |
|-----|--------------------------------|
| 99% of `ApplicationInstance` reconciles complete in under 1s | `histogram_quantile(0.99, sum by (le) (rate(controller_runtime_reconcile_time_seconds_bucket{controller="applicationinstance"}[5m]))) < 1` |
| 99% of `Backup` operations succeed within 30 minutes | Track via Operation CR phase / `OperationSucceeded` events until `vworkspace_operator_operation_total` exists; catalog default is 30m wall clock. |
| Pull-job lag stays under 60s | `vworkspace_operator_pull_job_lag_seconds < 60` |
| Connectivity is healthy over 5m (Pull mode) | `avg_over_time(vworkspace_operator_connectivity_state{mode="pull"}[5m]) > 0.99` |

Tune thresholds per organization. Connectivity and pull-job lag SLOs apply when `agent.enabled=true`; GitOps-only clusters rely more on `controller_runtime_*` and Kubernetes events.

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

## Event volume and backpressure

Pull-mode clusters can emit many `ConditionTransition` events during churn (Helm upgrades, operation lifecycles, connectivity flaps). The operator bounds outbound volume so a slow or unavailable control plane cannot exhaust memory on the cluster.

| Signal | What to watch |
|--------|----------------|
| `vworkspace_operator_event_buffer_occupancy` | Sustained values near the buffer capacity (default 1000) while `vworkspace_operator_connectivity_state{mode="pull"}` is `0` or `-1` — events are queuing faster than they flush. |
| `vworkspace_operator_connectivity_state` | Drops to `0` (reconnecting) or `-1` (disconnected) during outages; audit posts stall until the link recovers. |
| `Cluster.status.conditions[BufferOverflow]` | `True` with `reason=EventBufferFull` — the buffer dropped oldest events; the Discuss/audit timeline may have gaps until reconnect. |

**Starting alerts (tune per org):**

- `vworkspace_operator_event_buffer_occupancy > 800` for 5m while connectivity is not `1`.
- `max_over_time((cluster_status_bufferoverflow == 1)[1h])` or equivalent on the `BufferOverflow` condition via your `Cluster` status exporter.

**Recovery.** Restore connectivity to `Cluster.spec.controlPlaneBaseUrl` (see [troubleshooting.md](troubleshooting.md#clusterstatusconditionsbufferoverflowtrue-reason-eventbufferfull)). The batcher drains on successful `POST /api/agent/events`; `BufferOverflow` clears with `reason=BufferDrained`. Dropped events are not replayed — high-signal outcomes may reappear on the next condition transition. Server-side ingest and Discuss rules: [audit-events.md](audit-events.md); hub coordination: [vworkspace Phase 2 observability epic](https://github.com/vworkspace-io/vworkspace/issues/6).

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
