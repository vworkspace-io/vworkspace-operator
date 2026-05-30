# Engine: Helm hook jobs

**Status:** Alpha
**Last Updated:** 2026-05-30

Many Helm charts already ship migration logic as `helm.sh/hook` jobs that run pre-install, pre-upgrade, post-upgrade, or post-install. The operator's `helmHookJob` engine is the integration point that lets an `Operation` trigger one of those hooks by name without re-implementing the migration. The principle is the same as the rest of the operator: the chart is authoritative for app-internal behavior; the operator wires inputs and observes outputs.

This document covers when to pick the `helmHookJob` engine, why we do not duplicate chart-internal logic, how an `Operation` triggers a named hook job, a worked example for an upgrade-time migration, and how status maps back onto `Operation.status.conditions`.

## When to use the Helm Hook Job engine

Pick `helmHookJob` when:

- The chart **already encodes** the migration as a `helm.sh/hook` annotated `Job` (or `Pod`). Examples: schema migrations after a Postgres upgrade, search-index rebuilds, cache warmups.
- The migration is **bound to the upgrade event** and should run in the chart's own namespace using the chart's own service accounts and image versions.
- The migration is **idempotent** the way the chart's author meant it to be — the operator does not invent retry semantics that the chart did not contemplate.

Prefer a different engine when:

- The migration is **multi-step and crosses the chart boundary** (snapshot a PV, modify a CR in a different namespace, then upgrade). Use Argo Workflows instead — see [argo-workflows.md](argo-workflows.md).
- The migration is **chart-agnostic** (the same script runs against many applications). Use the Job engine instead — see [kubernetes-jobs.md](kubernetes-jobs.md).
- The work is **backup or restore**. Use Velero or the CSI snapshot engine — see [velero.md](velero.md) and [csi-snapshots-volsync.md](csi-snapshots-volsync.md).

## Why not duplicate chart-internal logic

Re-encoding what a chart already does is a tempting bug. Charts evolve: a maintainer adds a precondition, changes the image tag, or restructures the hook into two steps. If the operator carries a parallel implementation, it silently diverges over time and the next chart upgrade produces an outcome that neither the chart's author nor the operator's author intended. The safer model is:

- The chart is the source of truth for the migration.
- The operator triggers the chart's hook by name and lets the chart's hook run with the chart's own conventions.
- The operator's status reflects what the hook produced; it does not pre-interpret the hook's success or failure beyond "succeeded" vs "failed".

This keeps the operator portable across chart versions and across charts maintained by other teams. It also keeps `helmHookJob` honest about its scope: it triggers a hook, observes its `Job`, and reports the result.

## How an `Operation` triggers a hook

When the reconciler admits an `Operation` of `engine: helmHookJob`, it:

1. Resolves the target `ApplicationInstance` and the underlying `HelmRelease` (managed by the Flux Helm Controller).
2. Validates that the named hook (`parameters.hookName`) exists in the rendered release: the operator queries the live `HelmRelease.status.history` and the chart's rendered manifest. Hooks are discovered via the `helm.sh/hook` annotation on `Job` (and `Pod`) manifests.
3. Constructs a fresh `batch/v1` `Job` derived from the chart's hook `Job` template — same image, same command, same environment, same service account — but with a generated name, owner reference to the `Operation`, and ownership labels (`app.vworkspace.io/managed-by`, `ops.vworkspace.io/operation`).
4. Watches the `Job` and rewrites `Operation.status.conditions`, `Operation.status.phase`, and `Operation.status.outputs.logsRef` (pointing at the Pod) on each transition.

The operator does not invoke `helm hook` or trigger Helm to re-run the hook on its own. It clones the hook into a sibling `Job` so the operation is decoupled from the next chart upgrade.

## Worked example: an upgrade-time migration

This example triggers Nextcloud's `pre-upgrade` migration hook (`nextcloud-pre-upgrade`) as a stand-alone `Operation`, outside an upgrade. This is useful when an organization wants to dry-run the migration or when the chart's `pre-upgrade` hook is a long task whose timing should be decoupled from the chart bump.

### Discovering the hook name

```
$ kubectl get jobs -n org-myteam --selector helm.sh/hook=pre-upgrade
NAME                          COMPLETIONS   DURATION   AGE
nextcloud-myteam-pre-upgrade  0/1           ...        ...
```

The hook name as it appears in the chart's rendered manifest (the `metadata.name` of the Job) is what the operator references.

### The `Operation`

```yaml
apiVersion: ops.vworkspace.io/v1alpha1
kind: Operation
metadata:
  name: nextcloud-myteam-pre-upgrade-2026-05-28
  namespace: org-myteam
spec:
  targetRef:
    apiVersion: apps.vworkspace.io/v1alpha1
    kind: ApplicationInstance
    name: nextcloud-myteam
  type: Migration
  engine: helmHookJob
  parameters:
    hookName: nextcloud-myteam-pre-upgrade
    activeDeadlineSeconds: 1800
    backoffLimit: 1
```

### The materialized `Job`

The operator clones the chart's hook into a sibling Job named after the `Operation` (so `kubectl get jobs` clearly shows which run belongs to which `Operation`):

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: nextcloud-myteam-pre-upgrade-2026-05-28
  namespace: org-myteam
  labels:
    app.vworkspace.io/managed-by: vworkspace-operator
    app.vworkspace.io/cluster-id: cluster-prod-1
    ops.vworkspace.io/operation: 4f6a...
    ops.vworkspace.io/source-hook: nextcloud-myteam-pre-upgrade
  ownerReferences:
    - apiVersion: ops.vworkspace.io/v1alpha1
      kind: Operation
      name: nextcloud-myteam-pre-upgrade-2026-05-28
      uid: 4f6a-...
      controller: true
      blockOwnerDeletion: true
spec:
  backoffLimit: 1
  activeDeadlineSeconds: 1800
  ttlSecondsAfterFinished: 86400
  template:
    metadata:
      labels:
        ops.vworkspace.io/operation: 4f6a...
    spec:
      serviceAccountName: nextcloud-myteam
      restartPolicy: Never
      containers:
        - name: pre-upgrade
          image: nextcloud:29.0.4-fpm
          command: ["/bin/sh", "-c"]
          args:
            - php occ maintenance:repair --include-expensive
          env:
            - { name: NEXTCLOUD_ADMIN_USER, valueFrom: { secretKeyRef: { name: nextcloud-myteam, key: admin-user } } }
            - { name: NEXTCLOUD_ADMIN_PASSWORD, valueFrom: { secretKeyRef: { name: nextcloud-myteam, key: admin-password } } }
          volumeMounts:
            - { name: nextcloud-data, mountPath: /var/www/html }
      volumes:
        - name: nextcloud-data
          persistentVolumeClaim:
            claimName: data-nextcloud-myteam
```

The image, command, env, and volume mounts come from the chart's rendered hook Job; the operator does not invent any of them. The only fields the operator owns are the labels, owner references, name, `backoffLimit`, and `activeDeadlineSeconds`.

## Status mapping

`helmHookJob` reuses the Job engine's status mapping ([kubernetes-jobs.md](kubernetes-jobs.md)) with one addition: a `Blocked=True/HookNotFound` condition when the named hook is not present in the rendered release. The operator does not retry on `HookNotFound`; the condition stays until the request is cancelled or the chart is upgraded to a version that defines the hook.

| State                                             | `Operation.status.phase` | Conditions                                                                                                  |
|---------------------------------------------------|--------------------------|--------------------------------------------------------------------------------------------------------------|
| Hook not present in rendered release              | `Failed`                 | `Accepted=False/HookNotFound`. No `Job` is created.                                                          |
| `Job` running                                      | `Running`               | `Running=True/HelmHookJobActive`.                                                                            |
| `Job` complete                                     | `Succeeded`             | `Succeeded=True/HelmHookJobSucceeded`. `outputs.completionTime` populated.                                    |
| `Job` failed (backoff exhausted or deadline)       | `Failed`                | `Failed=True/HelmHookJobFailed`. Pod exit code and last log lines are mirrored into the message.              |
| Source hook is `pre-install` and the release does not yet exist | `Failed`     | `Accepted=False/HookRequiresRelease`. The operator only clones hooks from a released chart.                  |

## Practical notes

- The operator never edits the chart-originating Job in place. The chart's Helm Controller may overwrite it on the next reconcile; the cloned Job is owned by the `Operation` and is safe from that.
- Hooks annotated `helm.sh/hook-delete-policy: hook-succeeded` (or `before-hook-creation`) are still cloned safely; the operator's Job carries its own `ttlSecondsAfterFinished` and ignores Helm's delete policy.
- Hooks that depend on chart-rendered Secrets or ConfigMaps work as long as those Secrets/ConfigMaps are still present in the namespace at the time the `Operation` runs. They almost always are; the chart's own resources are reconciled by Flux.
- For upgrade-time migrations that should run **as part of an upgrade**, prefer letting Helm trigger the hook itself via the normal upgrade path described in [../upgrades-and-migrations.md](../upgrades-and-migrations.md). Use `helmHookJob` when the migration should be triggered **independently** of an upgrade.

## Related material

- [kubernetes-jobs.md](kubernetes-jobs.md) — Underlying Job semantics and security context defaults.
- [../upgrades-and-migrations.md](../upgrades-and-migrations.md) — How chart-hook migrations are invoked during a normal upgrade.
- [../operation-templates.md](../operation-templates.md) — How the `migration.helmHookJob` template is defined.
- [../../api/operation.md](../../api/operation.md) — Full `Operation` field reference.
