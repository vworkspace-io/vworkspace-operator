# Operate

**Status:** Alpha — APIs are at `v1alpha1` and may evolve.
**Last Updated:** 2026-06-04

This chapter is the day-to-day operation reference for `vworkspace-operator` once it is installed and connected to an vWorkspace Server control plane. It covers what the operator emits (metrics, structured logs, Kubernetes events, audit events to Odoo, the `Cluster` CR's status surface), how the operator itself is upgraded, how to troubleshoot common failure modes, and a worked runbook for the most-requested day-2 task: backup and restore.

The chapter is meant to be readable in any order. A new operator usually reads observability first (to know what their dashboards should be showing), then troubleshooting (so they recognize the signals), then upgrades (when their first chart bump arrives), then the runbook (the first time someone says "we need a restore").

## Read in order

1. [observability.md](observability.md) — Prometheus metrics, structured log fields, Kubernetes events on every condition transition, audit events posted to Odoo, the `/healthz` and `/readyz` endpoints, and the `Cluster` CR's role as the cluster's overall health surface.
2. [audit-events.md](audit-events.md) — Agent event kinds (`ConditionTransition`, direct kinds), wire shape, and alignment with server `vws_audit` ingest and Discuss.
3. [upgrades.md](upgrades.md) — Upgrading the operator itself. Channels (`stable`, `candidate`, `edge`), per-cluster version pinning, staged rollouts, conversion webhooks for CRD evolution, Flux rollback for failure recovery, compatibility-matrix template.
4. [troubleshooting.md](troubleshooting.md) — Common failure modes mapped to conditions and reasons, with the `kubectl` commands to investigate each.
5. [backup-restore-runbook.md](backup-restore-runbook.md) — A complete worked example: request a backup via `Operation`, watch its status, verify the Velero `Backup`, restore into a fresh namespace, validate.

## What "operating" means here

The operator is itself a workload that needs operating. The questions this chapter answers are:

- Is the operator healthy right now, and how do I know?
- Is the operator reaching Odoo? When was the last successful round trip?
- What version of the operator is running on this cluster, and what version should it be?
- An `ApplicationInstance` is stuck — how do I find out why?
- An `Operation` is `Failed` — what do I look at?
- I need to restore an application from a backup — what is the exact procedure?

The reference answers are spread across the documents in this chapter. The condensed answer to the first three is "look at `Cluster.status`"; the answers to the last three are in [troubleshooting.md](troubleshooting.md) and [backup-restore-runbook.md](backup-restore-runbook.md).

## Where to look first

When the AI assistant in Odoo says "something is degraded on cluster X", the operator's response (in priority order):

| Question                                                  | First place to look                                                                                         |
|-----------------------------------------------------------|--------------------------------------------------------------------------------------------------------------|
| Is the cluster reaching Odoo?                              | `kubectl get cluster -n vworkspace-system <name> -o yaml` — read `status.conditions[Connected]`.             |
| Is the operator pod itself healthy?                        | `kubectl get pods -n vworkspace-system` and `/healthz` / `/readyz` ([observability.md](observability.md)).   |
| Is a specific `ApplicationInstance` healthy?               | `kubectl describe applicationinstance -n <ns> <name>` and its underlying `HelmRelease`.                       |
| Is a specific `Operation` running, blocked, or failed?     | `kubectl describe operation -n <ns> <name>` and the engine-specific child resource it created.                 |
| Why is the operator emitting more events than usual?       | The operator's own metrics — `vworkspace_operator_reconcile_total`, `vworkspace_operator_operation_total`. |

The structured logs are the deepest source of truth. The `Cluster` CR's status is the most summarized. Everything in between is the chapter's content.

## Related material

- [../security/README.md](../security/README.md) — Security posture (operating includes operating securely).
- [../operations/README.md](../operations/README.md) — Where the operator's actuated work is described, before it shows up as a problem.
- [../install/uninstall.md](../install/uninstall.md) — When operating ends.
