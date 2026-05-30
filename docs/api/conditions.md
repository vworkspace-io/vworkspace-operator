# Conditions

**Status:** Alpha
**Last Updated:** 2026-05-28

The operator emits standard Kubernetes condition objects on three places:

- `ApplicationInstance.status.conditions[]` — application reconciliation state.
- `Operation.status.conditions[]` — day-2 operation state.
- The operator's own `Cluster.status.conditions[]` — connectivity to Odoo, authentication, and the presence/health of the cluster's required controllers.

Every condition has the standard fields `type`, `status` (`True` / `False` / `Unknown`), `reason` (a short PascalCase machine-readable code), `message` (a human-readable explanation), `lastTransitionTime`, and (where helpful) `observedGeneration`. The `reason` vocabulary below is the closed set the operator currently emits; additional reasons may be added in later alphas, and the operator does not remove a documented reason without a deprecation note.

## `ApplicationInstance`

### `Ready`

`Ready=True` means the application is available — its `HelmRelease` reports `Ready=True` and any optional generic readiness checks pass. `Ready=False` is the default during install and during failures.

| `status` | Reasons | Meaning |
|----------|---------|---------|
| True     | `HelmReleaseReady` | The underlying `HelmRelease` reports `Ready=True` and the operator is satisfied. |
| True     | `HelmReleaseReadyWithWarnings` | Ready, but a non-fatal warning was raised (e.g., a deprecated values key). The `message` carries the warning text. |
| False    | `HelmReleaseFailed` | The `HelmRelease` failed (install error, upgrade error, drift remediation error). `message` carries the underlying error. |
| False    | `ChartUnreachable` | The chart source (`OCIRepository` or `HelmRepository`) is not currently reachable. |
| False    | `WaitingForCertificate` | The chart's ingress references a cert-manager `Certificate` that has not yet been issued. |
| False    | `WaitingForSecret` | The chart references a `Secret` (typically via external-secrets) that has not yet been synced. |
| False    | `Installing` | First-time install in progress. |
| False    | `Upgrading` | An upgrade is in progress. |
| Unknown  | `Reconciling` | The operator has not yet observed enough of the downstream state to decide. Usually transient. |

### `Reconciling`

`Reconciling=True` means the operator is actively driving the resource toward the desired state. `Reconciling=False` is the steady state.

| `status` | Reasons | Meaning |
|----------|---------|---------|
| True     | `HelmReleaseUpgrading` | The materialized `HelmRelease` is in the middle of an upgrade. |
| True     | `HelmReleaseInstalling` | First-time install. |
| True     | `ChartSourceSyncing` | The chart source resource is fetching the artifact for a new version. |
| False    | `Stable` | No reconciliation in flight. |

### `Degraded`

`Degraded=True` is the loud signal that something needs attention. A `Degraded=True` resource may still have `Ready=True` (degraded-but-running) or may not.

| `status` | Reasons | Meaning |
|----------|---------|---------|
| True     | `HelmReleaseDegraded` | Flux reports the release as `Released=False` after a previously successful install. |
| True     | `ChartUnreachable` | Persistent inability to reach the chart source. |
| True     | `EndpointUnreachable` | A generic readiness check failed (HTTP probe, TCP probe). |
| True     | `Drifted` | Drift detection found a mismatch between desired and live state. The operator reports; it does not auto-remediate. |
| False    | `Recovered` | The condition cleared. |

### `Suspended`

`Suspended=True` means reconciliation is paused, either manually or by a maintenance window in the catalog.

| `status` | Reasons | Meaning |
|----------|---------|---------|
| True     | `MaintenanceWindow` | The catalog policy says this application is in a maintenance window. |
| True     | `ManuallySuspended` | An admin or Odoo policy explicitly suspended reconciliation. |
| False    | `Active` | Reconciliation is active. |

### `Blocked`

`Blocked=True` means reconciliation cannot proceed until a prerequisite is satisfied.

| `status` | Reasons | Meaning |
|----------|---------|---------|
| True     | `AwaitingApproval` | A required approval has not been provided. |
| True     | `MissingChartSource` | The configured chart source does not exist or is not allowed. |
| True     | `MissingDependencies` | A required controller (cert-manager, external-secrets, ...) is not present. |
| True     | `PolicyDenied` | Cluster-side policy rejected the desired state. |
| False    | `Unblocked` | The blocker cleared. |

### `Deleting`

`Deleting=True` means uninstall is in progress. Set when the resource has a deletion timestamp; cleared when the finalizer is removed.

| `status` | Reasons | Meaning |
|----------|---------|---------|
| True     | `Uninstalling` | The operator is removing the `HelmRelease` and waiting for Helm to clean up resources. |
| True     | `WaitingForChildResources` | Some chart-produced resources have not yet terminated. |

## `Operation`

### `Accepted`

`Accepted=True` means the operator validated the request and queued it for execution. Set as soon as the admission webhook is satisfied.

| `status` | Reasons | Meaning |
|----------|---------|---------|
| True     | `AdmissionPassed` | The admission webhook accepted the resource and the operator has it queued. |
| False    | `ValidationFailed` | The operator could not parse or validate the request (typically transient, e.g., the target was deleted between admission and reconcile). |

### `Running`

`Running=True` means the engine is actively working. Set after the operator creates the engine's resource and observes it start.

| `status` | Reasons | Meaning |
|----------|---------|---------|
| True     | `EngineStarted` | The engine reports work in progress. |
| False    | `EngineNotStarted` | The engine has not yet started; usually because `Blocked` is true. |

### `Succeeded`

`Succeeded=True` is a terminal state.

| `status` | Reasons | Meaning |
|----------|---------|---------|
| True     | `EngineCompleted` | The engine reported a successful terminal outcome. |
| False    | `NotYetSucceeded` | Not yet succeeded; either still running or failed. |

### `Failed`

`Failed=True` is a terminal state.

| `status` | Reasons | Meaning |
|----------|---------|---------|
| True     | `EngineFailed` | The engine reported a terminal failure. `message` carries the engine's error. |
| True     | `Timeout` | The engine ran past the configured timeout. |
| True     | `PreconditionFailed` | A precondition (target not `Ready`, conflicting operation) was not satisfied within the configured wait window. |

### `Cancelled`

`Cancelled=True` is a terminal state.

| `status` | Reasons | Meaning |
|----------|---------|---------|
| True     | `UserCancelled` | An admin (or Odoo policy) cancelled the operation. |
| True     | `SupersededByOperation` | A newer operation supersedes this one (e.g., a fresh `Backup` while this one was waiting). |

### `Blocked`

`Blocked=True` means the engine cannot start until a prerequisite is satisfied. The operation remains in this condition until either the blocker clears or it is `Cancelled`.

| `status` | Reasons | Meaning |
|----------|---------|---------|
| True     | `ConflictingOperation` | Another operation on the same target is in flight; the admission webhook admitted with `Blocked=True` and the operator will retry when the other completes. |
| True     | `AwaitingApproval` | `spec.approvals.required` is true and the claim is missing or invalid. |
| True     | `TargetNotReady` | The target `ApplicationInstance` is not `Ready=True` and the operation requires it. |
| True     | `DependencyMissing` | A required controller (Velero, Argo Workflows) is not present on the cluster. |

## `Cluster` (operator self-status)

The operator maintains a `Cluster` resource that reports its own posture: how it relates to Odoo, whether it is authenticated, and whether the cluster's required controllers are healthy. This is the resource the Pull-mode `POST /api/agent/events` `ClusterHeartbeat` event reports against.

### `Connected`

`Connected=True` means the most recent outbound round-trip to Odoo succeeded within the heartbeat interval.

| `status` | Reasons | Meaning |
|----------|---------|---------|
| True     | `RoundTripOK` | Recent round-trip to Odoo succeeded. |
| False    | `OdooUnreachable` | Outbound HTTPS to Odoo is failing. `message` carries the underlying network error. |

### `Disconnected`

`Disconnected=True` is the headline condition the operator sets when no successful round-trip has occurred within the configured disconnect window. Pull-mode reconciliation continues; only new intent is paused. See [../connectivity/pull-mode.md](../connectivity/pull-mode.md#offline-and-disconnected-behavior).

| `status` | Reasons | Meaning |
|----------|---------|---------|
| True     | `NoRecentRoundTrip` | No successful contact with Odoo within the disconnect window. |
| False    | `Connected` | The connection is alive. |

### `Authenticated`

`Authenticated=True` means the cluster's bootstrap credential is valid.

| `status` | Reasons | Meaning |
|----------|---------|---------|
| True     | `CredentialAccepted` | The bearer token (and optional client certificate) is accepted by Odoo. |
| False    | `CredentialRejected` | Odoo returned `401`/`403`. Requires admin intervention; the operator stops polling and surfaces this condition. |
| False    | `CredentialExpired` | The token's expiry has been reached and rotation failed. |

### `ControllersHealthy`

`ControllersHealthy=True` means the third-party controllers the operator depends on are present and reporting healthy.

| `status` | Reasons | Meaning |
|----------|---------|---------|
| True     | `AllControllersReady` | Every required controller (`flux`, `cert-manager`, `external-secrets`, `velero`) reports ready. |
| False    | `ControllerMissing` | At least one required controller is not installed. `message` lists which. |
| False    | `ControllerDegraded` | At least one required controller reports unhealthy. `message` lists which and why. |

The operator surfaces missing or degraded controllers loudly: a `Cluster` with `ControllersHealthy=False` is a configuration problem the admin must address, and the AI assistant in Odoo can offer to install the missing pieces via a generated `Operation`.

### `BufferOverflow`

`BufferOverflow=True` means the outbound Pull-mode event buffer dropped events because Odoo was unreachable or not accepting posts and the buffer reached its capacity.

| `status` | Reasons | Meaning |
|----------|---------|---------|
| True     | `EventBufferFull` | The operator dropped one or more outbound events. The `message` includes the drop count and suggests verifying Odoo connectivity. |
| False    | `BufferDrained` | The buffer has drained successfully after a prior overflow. |
