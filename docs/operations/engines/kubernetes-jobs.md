# Engine: Kubernetes Jobs

**Status:** Alpha
**Last Updated:** 2026-05-30

The Kubernetes `Job` engine is the operator's choice for one-shot, portable tasks that do not warrant a workflow DAG. It is the lowest-common-denominator engine: any cluster has a `batch/v1` `Job` controller, no third-party install is required, and the resulting `Pod` is a familiar troubleshooting surface. The operator materializes a `batch/v1` `Job`, watches its status, and reports completion or failure back onto the `Operation`.

This document covers when to pick the Job engine, how an `Operation` materializes a `Job`, a complete worked example (`pg_dump`-style export), how service accounts are scoped, and how Job status maps back onto `Operation.status.conditions`.

## When to use the Job engine

Pick the Job engine when:

- The task is **one container, one command**, and the failure mode "Pod failed → retry up to N times → give up" is appropriate.
- The task is **portable** across clusters. Every conformant Kubernetes cluster runs `batch/v1`.
- The task should **not require Argo Workflows** to be installed on the cluster.
- The task is naturally expressed as a CLI run: `pg_dump`, `kubectl`, `helm`, `restic`, a custom diagnostic tool.

Prefer a different engine when:

- The task is **multi-step or branching**. Use Argo Workflows instead — see [argo-workflows.md](argo-workflows.md).
- The task is a **namespace backup or restore**. Use the Velero engine instead — see [velero.md](velero.md).
- The task is the **chart's own hook job**. Use the Helm Hook Job engine instead — see [helm-hooks.md](helm-hooks.md).

## How an `Operation` materializes a `Job`

When the reconciler admits an `Operation` of `engine: job`, it:

1. Resolves the operation template's `inputSchema` and validates `Operation.spec.parameters`. The parameter set is intentionally small (image, command, args, environment, mounted Secrets and ConfigMaps, optional PVC mount, optional active deadline).
2. Constructs a `batch/v1` `Job` in the target's namespace, with `spec.template.spec.serviceAccountName` set to a namespace-scoped service account chosen by the operation template's `rbacProfile`. The operator's own service account is **not** used to run the workload.
3. Sets ownership labels (`app.vworkspace.io/managed-by`, `app.vworkspace.io/cluster-id`, `ops.vworkspace.io/operation`) on the `Job`, sets `spec.backoffLimit` and `spec.activeDeadlineSeconds` from the request, and writes `spec.ttlSecondsAfterFinished` so the `Job` is garbage-collected after the `Operation` records its result.
4. Watches the `Job` and rewrites `Operation.status.conditions`, `Operation.status.phase`, and `Operation.status.outputs.logsRef` (pointing at the most recent `Pod` for the `Job`) on each transition.

The operator does not modify the `Job` after creation. Cancellation deletes the `Job` (and the controller propagates the deletion to the `Pod`).

## Worked example: `pg_dump` export

This example runs a `pg_dump`-style export against a PostgreSQL service inside the `org-myteam` namespace and stores the dump in object storage via `aws s3 cp`. The credentials come from existing Kubernetes Secrets in the same namespace; nothing is inlined into the `Operation`.

### The `Operation`

```yaml
apiVersion: ops.vworkspace.io/v1alpha1
kind: Operation
metadata:
  name: nextcloud-myteam-pgdump-2026-05-28
  namespace: org-myteam
spec:
  targetRef:
    apiVersion: apps.vworkspace.io/v1alpha1
    kind: ApplicationInstance
    name: nextcloud-myteam
  type: RunCommand
  engine: job
  parameters:
    image: ghcr.io/vworkspace-io/op-pgtools:0.0.0
    command: ["/bin/sh", "-c"]
    args:
      - |
        set -euo pipefail
        pg_dump -Fc -h ${PG_HOST} -U ${PG_USER} ${PG_DB} > /tmp/dump.pgc
        aws s3 cp /tmp/dump.pgc s3://backups.example.com/nextcloud/dump-$(date -u +%FT%TZ).pgc
    env:
      - name: PG_HOST
        value: nextcloud-myteam-postgresql
      - name: PG_DB
        value: nextcloud
      - name: PG_USER
        valueFrom:
          secretKeyRef:
            name: nextcloud-myteam-postgresql
            key: username
      - name: PGPASSWORD
        valueFrom:
          secretKeyRef:
            name: nextcloud-myteam-postgresql
            key: password
      - name: AWS_ACCESS_KEY_ID
        valueFrom:
          secretKeyRef:
            name: backup-bucket-creds
            key: AWS_ACCESS_KEY_ID
      - name: AWS_SECRET_ACCESS_KEY
        valueFrom:
          secretKeyRef:
            name: backup-bucket-creds
            key: AWS_SECRET_ACCESS_KEY
    activeDeadlineSeconds: 1800
    backoffLimit: 2
```

### The materialized `Job`

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: nextcloud-myteam-pgdump-2026-05-28
  namespace: org-myteam
  labels:
    app.vworkspace.io/managed-by: vworkspace-operator
    app.vworkspace.io/cluster-id: cluster-prod-1
    ops.vworkspace.io/operation: 1a2b...
spec:
  backoffLimit: 2
  activeDeadlineSeconds: 1800
  ttlSecondsAfterFinished: 86400
  template:
    metadata:
      labels:
        app.vworkspace.io/managed-by: vworkspace-operator
        ops.vworkspace.io/operation: 1a2b...
    spec:
      serviceAccountName: vworkspace-operation-runner
      restartPolicy: Never
      containers:
        - name: runner
          image: ghcr.io/vworkspace-io/op-pgtools:0.0.0
          command: ["/bin/sh", "-c"]
          args:
            - |
              set -euo pipefail
              pg_dump -Fc -h ${PG_HOST} -U ${PG_USER} ${PG_DB} > /tmp/dump.pgc
              aws s3 cp /tmp/dump.pgc s3://backups.example.com/nextcloud/dump-$(date -u +%FT%TZ).pgc
          env:
            - { name: PG_HOST, value: nextcloud-myteam-postgresql }
            - { name: PG_DB,   value: nextcloud }
            - name: PG_USER
              valueFrom: { secretKeyRef: { name: nextcloud-myteam-postgresql, key: username } }
            - name: PGPASSWORD
              valueFrom: { secretKeyRef: { name: nextcloud-myteam-postgresql, key: password } }
            - name: AWS_ACCESS_KEY_ID
              valueFrom: { secretKeyRef: { name: backup-bucket-creds, key: AWS_ACCESS_KEY_ID } }
            - name: AWS_SECRET_ACCESS_KEY
              valueFrom: { secretKeyRef: { name: backup-bucket-creds, key: AWS_SECRET_ACCESS_KEY } }
          securityContext:
            allowPrivilegeEscalation: false
            runAsNonRoot: true
            runAsUser: 65532
            capabilities:
              drop: ["ALL"]
            readOnlyRootFilesystem: true
            seccompProfile:
              type: RuntimeDefault
```

The `securityContext` block is filled in by the operator's mutating admission webhook so every Job materialized through this engine runs under restricted PSA defaults, regardless of the request.

## Service account scoping

The Job engine deliberately does **not** run with the operator's own service account. Instead, every namespace where Job-engine operations are allowed has a `vworkspace-operation-runner` `ServiceAccount` plus a namespace-scoped `Role`/`RoleBinding` that grants only the verbs the operation template declares. The operator's controller manager only has the right to `create`, `get`, `list`, `watch`, and `delete` Jobs in those namespaces; it does not borrow the runner's permissions.

| Operation template          | Runner ServiceAccount permissions in the target namespace                                                       |
|-----------------------------|------------------------------------------------------------------------------------------------------------------|
| `runCommand.job` (default)  | `get/list/watch` on `pods`, `pods/log`. `get` on `secrets` and `configmaps` named in the parameter set.          |
| `pgdump.job`                | The default set plus `get` on the named PostgreSQL service.                                                       |
| `restic.job`                | The default set plus `get/list/watch` on `persistentvolumeclaims` and `get` on the bucket-credentials Secret.     |

The full RBAC reference is in [../../security/rbac.md](../../security/rbac.md); the rationale for runner-separate-from-operator is in [../../security/least-privilege.md](../../security/least-privilege.md). The short version: a runaway Job cannot read CRDs, cannot list cluster-wide Secrets, and cannot escalate into the operator's reconciler.

## Status mapping

The operator follows `Job.status.conditions[]` and `Job.status.active/succeeded/failed`:

| Job state                                  | `Operation.status.phase` | Conditions                                                                                                                          |
|--------------------------------------------|--------------------------|-------------------------------------------------------------------------------------------------------------------------------------|
| No Pod yet, `active=0`                     | `Pending`                | `Accepted=True/TemplateValidated`, `Running=False/Pending`.                                                                          |
| `active>=1`                                | `Running`                | `Running=True/JobPodActive`. `outputs.logsRef` points at the active Pod for live `kubectl logs`.                                     |
| `Complete=True`                            | `Succeeded`              | `Running=False`, `Succeeded=True/JobSucceeded`. `outputs.succeeded="1"`.                                                              |
| `Failed=True` (backoff exhausted)          | `Failed`                 | `Failed=True/JobFailedBackoffExceeded`. The terminating Pod's exit code and last log lines are mirrored into the condition message.  |
| `Failed=True` with reason `DeadlineExceeded` | `Failed`               | `Failed=True/JobActiveDeadlineExceeded`.                                                                                              |
| External deletion                           | `Failed` (cancelled)    | `Cancelled=True/ExternalDeletion` if the parent `Operation` did not request the deletion; otherwise `Cancelled=True/CancelledByUser`. |

If the Job's Pod is being restarted by `restartPolicy: OnFailure`, the operator continues to publish `Running=True`; the rolling failures count only when the Job's `backoffLimit` is exhausted.

## Practical notes

- The Job engine respects Pod Security Admission ("restricted") in every namespace where vWorkspace operations are allowed. Operations that require a privileged container will be rejected at admission rather than at runtime; that is intentional. If the operation truly needs privilege (extremely rare), it should be modeled as a workflow with its own SCC scoping, not as a generic Job.
- The Job's `ttlSecondsAfterFinished` defaults to 24 hours; the `Operation` records the run independently, so the Pod and Job objects are reclaimed shortly after the operator records the result.
- The operator's mutating webhook always sets `restartPolicy: Never` on the Pod template, regardless of the request. The Job controller handles retries via `backoffLimit`.
- The Job's `Pod` writes to stdout/stderr only; no `emptyDir` volume is mounted for "results" by default. Operations that need to produce an artifact upload it to an external store inside the run, as the worked example does.

## Related material

- [../operation-templates.md](../operation-templates.md) — How `runCommand.job` and similar templates are defined.
- [../../security/least-privilege.md](../../security/least-privilege.md) — Why the runner ServiceAccount is distinct from the operator.
- [../../security/rbac.md](../../security/rbac.md) — Concrete `Role`/`RoleBinding` examples.
- [../../api/operation.md](../../api/operation.md) — Full `Operation` field reference.
