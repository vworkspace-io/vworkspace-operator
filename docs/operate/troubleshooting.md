# Troubleshooting

**Status:** Alpha
**Last Updated:** 2026-05-30

This document maps common failure modes to the conditions and reasons the operator publishes, with the `kubectl` commands to investigate each. The patterns are organized by where the symptom shows up: the `Cluster` CR, an `ApplicationInstance`, or an `Operation`. The deeper diagnostic surfaces — structured logs, metrics, the `HelmRelease`, the engine-specific child resource — are covered as supporting evidence.

For each symptom the structure is: the condition you see, what it means, the investigation commands, and the resolution (or the next document to read).

## `Cluster` symptoms

### `Cluster.status.conditions[Connected]=False`, reason `OdooUnreachable`

**Meaning.** The operator cannot reach `Cluster.spec.odooBaseUrl`. Pull-mode jobs are not being fetched; outbound audit events are buffering locally.

**Investigate.**

```
kubectl get cluster -n vworkspace-system <name> -o yaml | grep -A20 conditions:
kubectl logs -n vworkspace-system deploy/vworkspace-app-operator --tail=200 \
  | grep -E '(odoo|proxy|http)'
kubectl exec -n vworkspace-system deploy/vworkspace-app-operator -- \
  wget -O- -q https://workspace.example.org/healthz || echo FAIL
```

Check:

- DNS resolution from the operator pod (`nslookup workspace.example.org` inside the pod).
- Outbound firewall to the Odoo host's IP and port.
- The `Cluster.spec (egress proxy — see pull-mode docs)` block matches the environment's proxy.
- The proxy's CA bundle is mounted via `operator.extraCaBundles` (see [../install/offline-and-airgapped.md](../install/offline-and-airgapped.md)).

**Resolve.** Fix DNS / firewall / proxy. The operator retries with backoff; no operator restart is needed once the path is open.

### `Cluster.status.conditions[Connected]=False`, reason `RegistrationTokenInvalid`

**Meaning.** The one-time token was rejected — expired, already-used, or wrong hash.

**Investigate.** `kubectl get cluster -n vworkspace-system <name> -o yaml` shows the reason. The operator's log line for the rejection is at `warn` level with `msg="registration failed"`.

**Resolve.** Re-issue the registration token in Odoo (Cluster Registry → Issue registration token), re-apply the `Cluster` CR with the new token, or run the CLI helper. The fresh token must arrive at the operator within its TTL.

### `Cluster.status.conditions[Authenticated]=False`, reason `CredentialRevoked`

**Meaning.** The long-lived credential is no longer accepted by Odoo (revoked from the control-plane side, or rotation failed and the grace period expired).

**Investigate.**

```
kubectl get secret -n vworkspace-system vworkspace-operator-credentials -o yaml
kubectl get events -n vworkspace-system | grep -i credential
```

**Resolve.** If control plane intentionally revoked the credential (admin action, suspected leak), re-register with a fresh token. If the rotation failed silently, re-register with a fresh token and audit the rotation logs (look for `vworkspace_operator_credential_age_seconds` flatlined for longer than the rotation interval).

### `Cluster.status.conditions[ControllersHealthy]=False`, reason `ControllerMissing` or `ControllerDegraded`

**Meaning.** One of the bundled controllers (Flux, Velero, cert-manager, external-secrets) is missing or not reconciling. The condition message names the controller.

**Investigate.**

```
kubectl get pods -A | grep -E '(helm-controller|source-controller|velero|cert-manager|external-secrets)'
kubectl get deploy -A | grep -E '(helm-controller|source-controller|velero|cert-manager|external-secrets)'
kubectl describe deploy -n velero velero
```

**Resolve.** If the controller is missing, `helm upgrade vworkspace-app-operator` (re-install the bundle); a deleted bundled controller is restored by the chart. If the controller is degraded, inspect its own logs and events — the operator's job is to report, not to fix upstream controller bugs.

### `Cluster.status.conditions[Disconnected]=True`

**Meaning.** Connectivity has been failing for the configured grace period (default 5 minutes). The operator is not pulling new jobs; the cluster continues to reconcile the last known desired state.

**Investigate and resolve.** Same as `Connected=False/OdooUnreachable` above. The `Disconnected` condition is sticky to make the failure visible in dashboards; it clears once `Connected=True` has held for the recovery threshold (default 1 minute).

## `ApplicationInstance` symptoms

### `ApplicationInstance` stuck in `Reconciling=True`

**Meaning.** The operator is waiting for the underlying `HelmRelease` to settle. Most commonly the chart's reconcile is in progress or failing.

**Investigate.**

```
kubectl get applicationinstance -n <ns> <name> -o yaml | grep -A40 status:
kubectl get helmrelease -n <ns>
kubectl describe helmrelease -n <ns> <name>
kubectl get helmrepository,ocirepository -n <ns>
kubectl describe helmrelease -n <ns> <name> | grep -A20 'Last Applied'
```

Check `HelmRelease.status.conditions`:

- `Ready=False, reason=InstallSucceeded`: the chart installed but did not converge. Look at the chart's own resources.
- `Ready=False, reason=UpgradeFailed`: the chart upgrade failed; look at the rendered Pods and chart hooks.
- `Ready=False, reason=ArtifactFailed`: Flux cannot fetch the chart. Check `HelmRepository`/`OCIRepository` and outbound to the chart source.

**Resolve.** Address the chart-side problem and let Flux re-reconcile, or adjust `ApplicationInstance.spec.values` and let the operator push the change.

### `ApplicationInstance.status.conditions[Blocked]=True`, reason `MissingSecret`

**Meaning.** `spec.values.secretRef` (or `external-secrets`'s target Secret) is not present yet.

**Investigate.**

```
kubectl get secret -n <ns>
kubectl get externalsecret -n <ns>
```

**Resolve.** Create the Secret, or wait for `external-secrets` to sync from the upstream store. The condition clears on the next reconcile.

### `ApplicationInstance.status.conditions[Degraded]=True`, reason `UpgradeFailed`

**Meaning.** A chart upgrade failed and Flux rolled it back. The application is running on the previous revision.

**Investigate.**

```
kubectl describe helmrelease -n <ns> <name>
kubectl logs -n <ns> -l app.kubernetes.io/name=<chart-name> --previous --tail=200
```

**Resolve.** Most often: the new chart version requires a values change. Adjust `spec.values`, optionally pin `spec.chart.version` back to the previous, and let the chart reconcile. The upgrade-and-migrations narrative is in [../operations/upgrades-and-migrations.md](../operations/upgrades-and-migrations.md).

### `ApplicationInstance.status.conditions[Suspended]=True`, reason `ReconcilePaused`

**Meaning.** Someone (or something) annotated the `ApplicationInstance` with `apps.vworkspace.io/reconcile=disabled`. The operator is not pushing changes.

**Investigate.** `kubectl get applicationinstance ... -o yaml | grep annotations -A5`.

**Resolve.** Remove the annotation if the pause was incidental (`kubectl annotate applicationinstance/<name> apps.vworkspace.io/reconcile-`). The uninstall procedure ([../install/uninstall.md](../install/uninstall.md)) sets the annotation intentionally; do not remove it during uninstall.

## `Operation` symptoms

### `Operation.status.conditions[Blocked]=True`, reason `TargetNotReady`

**Meaning.** The target `ApplicationInstance` is not `Ready=True` and the operation's policy requires readiness. The operator is waiting.

**Investigate.** Read the target's status (the `ApplicationInstance` symptoms section above).

**Resolve.** Make the target `Ready`, or set `Operation.spec.policy.requireReady: false` if the operation is intended to run against a degraded target (rare; usually only `type: RunCommand` diagnostic operations).

### `Operation.status.conditions[Blocked]=True`, reason `CapabilityMissing`

**Meaning.** The target `ApplicationInstance` does not advertise the capability the requested engine needs (e.g., `engine: velero` but the target lacks `ops.vworkspace.io/backup: velero`).

**Investigate.**

```
kubectl get applicationinstance -n <ns> <name> -o jsonpath='{.metadata.annotations}'
```

**Resolve.** Add the capability annotation (if appropriate for the application) or switch to an engine the application supports. The capability model is described in [../operations/operation-templates.md](../operations/operation-templates.md).

### `Operation.status.conditions[Blocked]=True`, reason `ConflictingOperation`

**Meaning.** Another `Operation` is `Running` on the same target with an overlapping verb class (you cannot run a Restore while a Migration is in flight, for example).

**Investigate.**

```
kubectl get operations -n <ns>
kubectl get operations -A -o json | jq '.items[] | select(.status.phase=="Running")'
```

**Resolve.** Wait for the conflicting operation to finish (or cancel it via `kubectl delete operation/<name>` if it is stuck and safe to cancel). The Blocked condition clears automatically.

### `Operation.status.conditions[Failed]=True`, reason `VeleroBackupFailed`

**Meaning.** Velero failed the underlying Backup. The condition message includes Velero's own reason.

**Investigate.**

```
kubectl get backup -n velero
kubectl describe backup -n velero <operation-name>
kubectl logs -n velero deploy/velero --tail=200
```

The Velero Backup's `status.failureReason` is the canonical source.

**Resolve.** Address the Velero-side problem (storage location credentials, snapshot class, etc.) and create a fresh `Operation`. Failed operations are not retried by the operator on their own; re-issue from the control plane or via `kubectl`. The engine reference is in [../operations/engines/velero.md](../operations/engines/velero.md).

### `Operation.status.conditions[Failed]=True`, reason `JobFailedBackoffExceeded`

**Meaning.** The Job-engine Pod failed enough times to exceed `backoffLimit`.

**Investigate.**

```
kubectl get pods -n <ns> -l ops.vworkspace.io/operation=<uid>
kubectl logs -n <ns> -l ops.vworkspace.io/operation=<uid> --previous --tail=200
```

The Pod's last `terminated.exitCode` and the last 200 log lines are usually enough.

**Resolve.** Fix the underlying command (image tag wrong, mounted secret wrong, target host unreachable) and re-issue. The Job-engine reference is in [../operations/engines/kubernetes-jobs.md](../operations/engines/kubernetes-jobs.md).

### `Operation.status.conditions[Failed]=True`, reason `WorkflowFailed`

**Meaning.** A node in the Argo Workflow failed. The condition message names the first failing node.

**Investigate.**

```
kubectl get workflow -n <ns>
kubectl describe workflow -n <ns> <name>
kubectl logs -n <ns> <pod-of-failing-node>
```

For Argo's UI (if installed): the workflow run ID is `Operation.status.outputs.runId`.

**Resolve.** Fix the failing step (the workflow template, the script source, the parameter) and re-issue. The workflow reference is in [../operations/engines/argo-workflows.md](../operations/engines/argo-workflows.md).

## Operator-level symptoms

### The operator pod CrashLoopBackOff

**Meaning.** The operator is failing to start. Most common causes: RBAC missing, CRDs missing, leader-election conflict, panic on startup.

**Investigate.**

```
kubectl get pods -n vworkspace-system
kubectl describe pod -n vworkspace-system -l app.kubernetes.io/name=vworkspace-app-operator
kubectl logs -n vworkspace-system -l app.kubernetes.io/name=vworkspace-app-operator --previous --tail=400
```

The previous container's logs include the panic message and stack if the failure is a startup panic. If the failure is RBAC, the message is `failed to list ...: forbidden`.

**Resolve.** RBAC: re-apply the bundle (`helm upgrade vworkspace-app-operator ...`). CRDs missing: re-apply the bundle; CRDs are part of the chart. Leader election: check `kubectl get lease -n vworkspace-system`; delete a stale lease if found.

### `controller_runtime_reconcile_errors_total` increasing

**Meaning.** Some controller is encountering errors faster than usual. Could be transient (Odoo briefly unreachable) or persistent (a webhook misconfiguration).

**Investigate.**

```
kubectl top pod -n vworkspace-system
kubectl logs -n vworkspace-system deploy/vworkspace-app-operator --tail=500 | grep level.error
```

Group by `controller` label in Prometheus to localize.

**Resolve.** Address the root cause. The operator's reconcile loop is event-driven and will recover once the cause is removed; no operator restart is required.

### Webhook calls failing

**Meaning.** The operator's admission or conversion webhook is unreachable. Symptom: any `kubectl apply` of an `ApplicationInstance` or `Operation` fails with `failed calling webhook ...`.

**Investigate.**

```
kubectl get validatingwebhookconfiguration | grep vworkspace
kubectl get mutatingwebhookconfiguration | grep vworkspace
kubectl get pod -n vworkspace-system -l app.kubernetes.io/name=vworkspace-app-operator
kubectl get svc -n vworkspace-system | grep webhook
```

**Resolve.** Make sure the operator pod is `Ready`. Check that the webhook's `clientConfig.caBundle` matches the operator's TLS cert (cert-manager rotates it; if cert-manager is missing, the bundle is stale). Restart the operator (`kubectl rollout restart deploy/vworkspace-app-operator -n vworkspace-system`) if rotation has flipped underneath it.

## When the symptom is not in this list

Three escalation paths:

1. **Increase log verbosity.** Edit the operator deployment (`--zap-log-level=debug`) and reproduce. The debug logs include per-step reconciliation traces.
2. **Open an issue.** The project's issue tracker is the right place for bugs and for "this is unexpected". Include `Cluster.status`, the relevant CR's status, the last few hundred log lines, and the operator version.
3. **Ask the AI assistant in Odoo Discuss.** The assistant has the same status surface you do plus the audit timeline; it can often correlate a current symptom with a recent change.

## Related material

- [observability.md](observability.md) — Metrics, logs, events, and audit stream — the signals that produced each condition above.
- [backup-restore-runbook.md](backup-restore-runbook.md) — A concrete walk-through for the most common day-2 task.
- [../api/conditions.md](../api/conditions.md) — Full vocabulary of condition types and reasons.
