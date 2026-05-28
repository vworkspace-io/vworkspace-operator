# Engine: Argo Workflows

**Status:** Alpha
**Last Updated:** 2026-05-28

Argo Workflows is the operator's engine for multi-step, branching, retry-aware day-2 work. The operator does not embed a workflow engine of its own; it materializes a `workflows.argoproj.io/v1alpha1` `Workflow`, lets the Argo controller run it, and aggregates the resulting status onto the matching `Operation`. The most common use is `type: Migration`, but the same engine backs operator-defined runbooks (`type: Runbook`) and any operation that benefits from a DAG.

This document covers when to pick Argo Workflows, how an `Operation` becomes a `Workflow`, a complete worked example (prechecks → quiesce → snapshot → verify → unquiesce), and how Workflow status maps back onto `Operation.status.conditions`.

## When to use Argo Workflows

Pick Argo Workflows when:

- The operation has **multiple steps with explicit ordering** (and possibly branching). Migrations of the "quiesce → snapshot → migrate → verify → unquiesce" shape are the canonical example.
- The operation needs **per-step retries, timeouts, or artifacts** that a one-shot `Job` cannot express cleanly.
- The operation must produce a **durable, queryable run record** for the audit trail beyond what `Operation.status` carries.
- The team already operates Argo Workflows for application-side workflows and wants one runtime instead of two.

Prefer a different engine when:

- The operation is **one-shot and portable**, with no branching. Use the `Job` engine instead — see [kubernetes-jobs.md](kubernetes-jobs.md).
- The operation is **namespace backup or restore**. Use the Velero engine instead — see [velero.md](velero.md).
- The operation is the **chart's own migration hook**. Use the Helm Hook Job engine instead — see [helm-hooks.md](helm-hooks.md).

The operator does not pre-require Argo Workflows on every cluster. If a cluster has not installed it, `Operation` requests with `engine: workflow` are rejected at admission with reason `EngineNotInstalled`.

## How an `Operation` materializes a `Workflow`

When the reconciler admits an `Operation` of `engine: workflow`, it:

1. Resolves the named `WorkflowTemplate` (a cluster-scoped `WorkflowTemplate` resource the operator ships with its bundle, or one the organization has installed). The template name is part of the operation template's `inputSchema` (commonly `parameters.template`).
2. Constructs a `Workflow` in the target namespace, referencing the template via `workflowTemplateRef`, with arguments populated from `Operation.spec.parameters` after schema validation.
3. Sets ownership labels (`app.vworkspace.io/managed-by`, `app.vworkspace.io/cluster-id`, `ops.vworkspace.io/operation`) on the `Workflow` and on the `Workflow.spec.serviceAccountName` is set to a least-privileged service account scoped to the target namespace (see [../../security/rbac.md](../../security/rbac.md)).
4. Watches the `Workflow` and rewrites `Operation.status.conditions`, `Operation.status.phase`, and `Operation.status.outputs` (notably `logsRef` pointing at the Workflow run) on every phase transition.

The operator does not modify the `Workflow` after creation. Suspending and resuming is forwarded through the Argo CLI or by editing `Workflow.spec.suspend`; the operator surfaces those transitions in its own status without trying to drive them.

## Worked example: a migration DAG

This example migrates an `ApplicationInstance` named `nextcloud-myteam` across a breaking minor version that requires a multi-step DAG. The DAG is prechecks → quiesce → snapshot → migrate → verify → unquiesce.

### The `Operation`

```yaml
apiVersion: ops.vworkspace.io/v1alpha1
kind: Operation
metadata:
  name: nextcloud-myteam-migrate-2026-05-28
  namespace: org-myteam
spec:
  targetRef:
    apiVersion: apps.vworkspace.io/v1alpha1
    kind: ApplicationInstance
    name: nextcloud-myteam
  type: Migration
  engine: workflow
  parameters:
    template: app-migration-with-snapshot
    targetChartVersion: "7.0.0"
    snapshotClassName: csi-rbd
    timeoutSeconds: 1800
    failureAction: rollback
```

### The `WorkflowTemplate`

The operator ships a small library of `WorkflowTemplate` resources in `config/workflows/`. Here is the `app-migration-with-snapshot` template referenced above, edited for brevity:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: app-migration-with-snapshot
  namespace: vworkspace-system
spec:
  entrypoint: migrate
  arguments:
    parameters:
      - { name: targetName }
      - { name: targetNamespace }
      - { name: targetChartVersion }
      - { name: snapshotClassName }
      - { name: failureAction, value: rollback }
  templates:
    - name: migrate
      dag:
        tasks:
          - name: prechecks
            template: run-prechecks
          - name: quiesce
            template: invoke-quiesce
            dependencies: [prechecks]
          - name: snapshot
            template: take-snapshot
            dependencies: [quiesce]
          - name: migrate
            template: bump-chart-version
            dependencies: [snapshot]
          - name: verify
            template: run-postchecks
            dependencies: [migrate]
          - name: unquiesce
            template: invoke-unquiesce
            dependencies: [verify]
            when: "{{tasks.verify.outputs.result}} == ok"
          - name: rollback
            template: rollback-from-snapshot
            dependencies: [verify]
            when: "{{tasks.verify.outputs.result}} != ok && {{workflow.parameters.failureAction}} == rollback"
    - name: run-prechecks
      script:
        image: ghcr.io/vworkspace-io/op-prechecks:0.0.0
        command: [sh]
        source: |
          /opt/prechecks/run.sh \
            --target {{workflow.parameters.targetName}} \
            --namespace {{workflow.parameters.targetNamespace}}
    - name: invoke-quiesce
      script:
        image: ghcr.io/vworkspace-io/op-quiesce:0.0.0
        command: [sh]
        source: |
          kubectl annotate applicationinstance/{{workflow.parameters.targetName}} \
            ops.vworkspace.io/quiesce=requested --overwrite
    - name: take-snapshot
      resource:
        action: create
        successCondition: status.readyToUse == true
        manifest: |
          apiVersion: snapshot.storage.k8s.io/v1
          kind: VolumeSnapshot
          metadata:
            name: pre-migrate-{{workflow.uid}}
            namespace: {{workflow.parameters.targetNamespace}}
          spec:
            volumeSnapshotClassName: {{workflow.parameters.snapshotClassName}}
            source:
              persistentVolumeClaimName: data-{{workflow.parameters.targetName}}
    - name: bump-chart-version
      script:
        image: ghcr.io/vworkspace-io/op-helm:0.0.0
        command: [sh]
        source: |
          kubectl patch applicationinstance/{{workflow.parameters.targetName}} \
            --type=merge -p '{"spec":{"chart":{"version":"{{workflow.parameters.targetChartVersion}}"}}}'
    - name: run-postchecks
      script:
        image: ghcr.io/vworkspace-io/op-prechecks:0.0.0
        command: [sh]
        source: |
          /opt/postchecks/run.sh \
            --target {{workflow.parameters.targetName}} \
            --namespace {{workflow.parameters.targetNamespace}}
    - name: invoke-unquiesce
      script:
        image: ghcr.io/vworkspace-io/op-quiesce:0.0.0
        command: [sh]
        source: |
          kubectl annotate applicationinstance/{{workflow.parameters.targetName}} \
            ops.vworkspace.io/quiesce- --overwrite
    - name: rollback-from-snapshot
      script:
        image: ghcr.io/vworkspace-io/op-volsnap:0.0.0
        command: [sh]
        source: |
          /opt/rollback/from-snapshot.sh pre-migrate-{{workflow.uid}}
```

### The materialized `Workflow`

The operator creates a single `Workflow` referencing the template and carrying the operation parameters as arguments:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  name: nextcloud-myteam-migrate-2026-05-28
  namespace: org-myteam
  labels:
    app.vworkspace.io/managed-by: vworkspace-operator
    app.vworkspace.io/cluster-id: cluster-prod-1
    ops.vworkspace.io/operation: 9d33...
spec:
  workflowTemplateRef:
    name: app-migration-with-snapshot
    clusterScope: false
  serviceAccountName: vworkspace-operation-runner
  arguments:
    parameters:
      - { name: targetName,         value: nextcloud-myteam }
      - { name: targetNamespace,    value: org-myteam }
      - { name: targetChartVersion, value: "7.0.0" }
      - { name: snapshotClassName,  value: csi-rbd }
      - { name: failureAction,      value: rollback }
  activeDeadlineSeconds: 1800
```

## Status mapping

The operator follows `Workflow.status.phase` and `Workflow.status.nodes[]`:

| Argo phase    | `Operation.status.phase` | Conditions                                                                                                                                             |
|---------------|--------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------|
| `Pending`     | `Pending`                | `Accepted=True/TemplateValidated`, `Running=False/Pending`.                                                                                             |
| `Running`     | `Running`                | `Running=True/WorkflowInProgress`. The most recently active node name is mirrored into the condition message (for example, `Node: take-snapshot`).      |
| `Succeeded`   | `Succeeded`              | `Running=False`, `Succeeded=True/WorkflowSucceeded`. `outputs.logsRef` points at the `Workflow` for `kubectl logs` and the Argo UI; `outputs.runId` is the `Workflow.metadata.uid`. |
| `Failed`      | `Failed`                 | `Failed=True/WorkflowFailed`. The first failing node's message is mirrored into the condition message; the full DAG remains queryable via the `Workflow`. |
| `Error`       | `Failed`                 | `Failed=True/WorkflowError`. Used for engine-level errors (template not found, argument missing) as opposed to step failures.                            |
| `Suspended`   | `Running` (suspended)    | `Running=True/WorkflowSuspended` plus `Blocked=True/AwaitingResume`. The operator does not auto-resume; a human or Odoo must act.                       |

Per-step granularity is not pushed into `Operation.status.conditions`; following 12 conditions to follow a 12-step DAG would be noisy. The operator publishes a single rolled-up summary, with `outputs.logsRef` and the `Workflow` itself as the place to drill down. The full mapping vocabulary is in [../../api/conditions.md](../../api/conditions.md).

## Practical notes

- The operator installs a small set of `WorkflowTemplate` resources under `vworkspace-system` (`app-migration-with-snapshot`, `pg-dump-export`, `tenant-prewarm`). Organizations can install their own templates; the admission webhook only checks that a `WorkflowTemplate` of the named name exists in a namespace the operation template is allowed to reference.
- Argo Workflows requires a workflow controller and (optionally) a workflow archive. The operator does not provision these; the cluster bootstrap doc ([../../install/cluster-bootstrap.md](../../install/cluster-bootstrap.md)) lists Argo Workflows as an optional add-on.
- Long-running workflows (>30 min) are supported; the operator's reconcile loop is event-driven and does not depend on the workflow returning quickly.
- The service account `vworkspace-operation-runner` is namespace-scoped and only gets the rights it needs per template. The RBAC model is documented in [../../security/least-privilege.md](../../security/least-privilege.md).

## Related material

- [../operation-templates.md](../operation-templates.md) — How `WorkflowTemplate` names are wired into operation templates.
- [../upgrades-and-migrations.md](../upgrades-and-migrations.md) — When a migration warrants a workflow versus a chart hook.
- [../../api/operation.md](../../api/operation.md) — Full `Operation` field reference.
- [../../security/least-privilege.md](../../security/least-privilege.md) — Why the workflow runner is not the operator's own service account.
