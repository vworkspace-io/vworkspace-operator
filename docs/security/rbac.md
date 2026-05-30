# RBAC

**Status:** Alpha
**Last Updated:** 2026-05-30

`vworkspace-operator` runs with a deliberately small set of cluster-wide permissions and a deliberately narrow set of namespace-scoped permissions. This document describes that RBAC model, why it is shaped that way, and what the YAML actually looks like. The accompanying reasoning about why the operator is not `cluster-admin` and why Velero / external-secrets / the workflow runner have their own service accounts is in [least-privilege.md](least-privilege.md).

## Model in one sentence

The operator's own `ClusterRole` covers exactly its own CRDs (`apps.vworkspace.io` and `ops.vworkspace.io`); a namespace-scoped `Role` per managed namespace grants the operator the rights it needs to materialize child resources owned by third-party controllers (Flux's `HelmRelease`, Velero's `Backup`/`Restore`, Argo's `Workflow`, the Job controller's `Job`, the CSI snapshot controller's `VolumeSnapshot`, VolSync's `ReplicationSource`/`ReplicationDestination`); a `RoleBinding` per managed namespace binds the operator's service account to that `Role`. Nothing else.

The "managed namespace" predicate is the label `app.vworkspace.io/managed-by=vworkspace`. The operator only operates in namespaces carrying that label, and the `RoleBinding`s only exist in those namespaces. Adding a managed namespace is a deliberate one-line cluster operation; the operator does not auto-grant itself rights into newly-appearing namespaces.

## Why not `cluster-admin`

`cluster-admin` is the easiest RBAC the operator could ship with, and it would be wrong. The blast-radius reasoning is in [least-privilege.md](least-privilege.md); the short version is: an operator that holds `cluster-admin` is a single high-value target that, if compromised, can read every Secret in the cluster, modify every workload, install arbitrary admission webhooks, and exfiltrate data the cluster owner did not opt into. The operator's job — applying a small set of CRDs and watching their downstream effects — does not require any of that. The default install therefore refuses `cluster-admin`; an operator who wants it can grant it, but they are doing so against the project's recommendation.

## The operator's `ClusterRole`

The operator's only cluster-scoped rights are the right to own its own CRDs and the standard small set of `events`/`leases` rights any controller-runtime operator needs:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: vworkspace-app-operator
  labels:
    app.kubernetes.io/name: vworkspace-app-operator
    app.kubernetes.io/managed-by: helm
rules:
  - apiGroups: ["apps.vworkspace.io"]
    resources: ["applicationinstances", "applicationinstances/status", "applicationinstances/finalizers"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["ops.vworkspace.io"]
    resources: ["operations", "operations/status", "operations/finalizers", "clusters", "clusters/status"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["apiextensions.k8s.io"]
    resources: ["customresourcedefinitions"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["namespaces"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["create", "patch"]
  - apiGroups: ["coordination.k8s.io"]
    resources: ["leases"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

That is the entire cluster-scoped surface. The operator does not have `get` on `secrets` cluster-wide, does not have `create` on `clusterrolebindings`, and does not have any verb on `pods`, `services`, `deployments`, `helmreleases`, `backups`, `workflows`, or `jobs` at the cluster scope. Anything it needs to do with those resources happens through the namespace-scoped `Role` documented next.

The matching `ClusterRoleBinding`:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: vworkspace-app-operator
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: vworkspace-app-operator
subjects:
  - kind: ServiceAccount
    name: vworkspace-app-operator
    namespace: vworkspace-system
```

## The namespace-scoped `Role`

In every namespace labeled `app.vworkspace.io/managed-by=vworkspace`, the operator holds a `Role` that grants it the verbs it needs to materialize child resources. The bundle ships one `Role` per managed namespace, applied by the operator's `Cluster` reconciler when a namespace is added to the operator's allow-list (a list maintained on the `Cluster` CR's status). The shape:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: vworkspace-app-operator
  namespace: org-myteam
  labels:
    app.kubernetes.io/name: vworkspace-app-operator
    app.kubernetes.io/managed-by: vworkspace
rules:
  # Flux Helm engine
  - apiGroups: ["helm.toolkit.fluxcd.io"]
    resources: ["helmreleases"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["source.toolkit.fluxcd.io"]
    resources: ["helmrepositories", "ocirepositories"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  # Velero (the operator references Backups/Restores by name in the velero namespace;
  # this Role only covers what the operator itself reads inside the target namespace)
  - apiGroups: [""]
    resources: ["persistentvolumeclaims"]
    verbs: ["get", "list", "watch"]
  # Argo Workflows
  - apiGroups: ["argoproj.io"]
    resources: ["workflows", "workflowtemplates"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  # Kubernetes Jobs (Job engine)
  - apiGroups: ["batch"]
    resources: ["jobs"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["pods", "pods/log"]
    verbs: ["get", "list", "watch"]
  # CSI snapshots
  - apiGroups: ["snapshot.storage.k8s.io"]
    resources: ["volumesnapshots"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  # VolSync
  - apiGroups: ["volsync.backube"]
    resources: ["replicationsources", "replicationdestinations"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  # Secrets and ConfigMaps that the operator references from ApplicationInstance.spec.values
  # (secretRef / configMapRef pointers). The operator never lists all secrets in the namespace;
  # it only `get`s the named one it was told to read.
  - apiGroups: [""]
    resources: ["secrets", "configmaps"]
    verbs: ["get"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["create", "patch"]
```

And the binding:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: vworkspace-app-operator
  namespace: org-myteam
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: vworkspace-app-operator
subjects:
  - kind: ServiceAccount
    name: vworkspace-app-operator
    namespace: vworkspace-system
```

A few things to note:

- The operator gets `get` on `secrets`, not `list` or `watch`. It can read a Secret by name (the name comes from `ApplicationInstance.spec.values.secretRef.name`); it cannot enumerate all Secrets in the namespace.
- The operator does not have `create` on `secrets`. It does not need to. Secrets are created either by chart-rendered objects (via the Helm engine, which is its own service account), by `external-secrets`, or by a human operator.
- The operator does not have any verbs on `Pods` beyond `get`/`list`/`watch` for read-only Pod and Pod-log inspection (used to surface the most recent Job/Workflow Pod under `Operation.status.outputs.logsRef`).
- The operator does not have `exec` on Pods. Quiesce hooks run by the Argo Workflows engine; the workflow runner has its own service account.

## The Velero namespace

Velero runs in its own namespace (commonly `velero` or `vworkspace-system`). The operator needs `create`/`get`/`list`/`watch`/`delete` on `velero.io/Backup` and `velero.io/Restore` in that namespace; it does not need any of Velero's own permissions. A separate small `Role` and `RoleBinding` apply, scoped to that namespace:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: vworkspace-app-operator-velero
  namespace: velero
rules:
  - apiGroups: ["velero.io"]
    resources: ["backups", "restores", "backupstoragelocations", "volumesnapshotlocations"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: vworkspace-app-operator-velero
  namespace: velero
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: vworkspace-app-operator-velero
subjects:
  - kind: ServiceAccount
    name: vworkspace-app-operator
    namespace: vworkspace-system
```

`BackupStorageLocation` and `VolumeSnapshotLocation` are read-only most of the time; the operator does not modify them after the cluster bootstrap step. The verbs include `update`/`patch`/`delete` for the cluster-bootstrap path that lets the operator install a default `BackupStorageLocation` from the bundle, but the running reconciler does not exercise them under steady state.

## Operation runner service accounts

The Job engine and the Argo Workflows engine do not run their workloads as the operator's service account. Each operation template names an `rbacProfile`; the bundle installs a per-namespace `ServiceAccount` named after the profile (`vworkspace-operation-runner` for the default Job and Workflow profiles), along with a `Role` granting only what the template needs. The operator's `Role` only has the right to `create` Jobs and Workflows; the Pods inside them run as the runner SA. The full split is described in [least-privilege.md](least-privilege.md).

## Adding a managed namespace

A namespace becomes a managed namespace by carrying the label `app.vworkspace.io/managed-by=vworkspace`. The operator's `Cluster` reconciler reacts by:

1. Listing namespaces that match the label.
2. Reconciling the per-namespace `Role`, `RoleBinding`, and any runner `ServiceAccount`/`Role`/`RoleBinding`s named by the operation templates the namespace is authorized to run.
3. Recording the managed namespaces in `Cluster.status.managedNamespaces[]`.

Removing the label removes the namespace from the operator's reconciler set on the next pass; the per-namespace `RoleBinding` is garbage-collected. Any `ApplicationInstance` or `Operation` CRs that remain in the un-labeled namespace are no longer reconciled. (Deletion of CRs in un-labeled namespaces requires a human to clean up, by design.)

## RBAC for `Operation` admission

A separate dimension of RBAC is "which `Operation` types are allowed in which namespace". This is enforced by the operator's validating admission webhook, not by Kubernetes RBAC, because the question is not "can this principal create an `Operation`" but "is this combination of `type`, `engine`, and target permitted by the operation template the namespace is authorized to run". The template/capability model is documented in [../operations/operation-templates.md](../operations/operation-templates.md).

A summary: a namespace's `Cluster.status.managedNamespaces[].allowedOperationTemplates[]` list constrains what `Operation` CRs can be admitted. If a request names a template not in the list, the webhook rejects with reason `OperationTemplateNotAllowedInNamespace`.

## Verifying RBAC on a running cluster

Useful one-liners during install or audit:

```
kubectl auth can-i create helmreleases.helm.toolkit.fluxcd.io \
  --as=system:serviceaccount:vworkspace-system:vworkspace-app-operator -n org-myteam
# expected: yes

kubectl auth can-i list secrets -A \
  --as=system:serviceaccount:vworkspace-system:vworkspace-app-operator
# expected: no

kubectl auth can-i create clusterrolebindings.rbac.authorization.k8s.io \
  --as=system:serviceaccount:vworkspace-system:vworkspace-app-operator
# expected: no
```

If any of these answer "yes" beyond the expected set, the RBAC has drifted from the bundle's defaults. Review the active `ClusterRoleBinding`s on the `vworkspace-app-operator` ServiceAccount and reconcile to the chart.

## Related material

- [least-privilege.md](least-privilege.md) — Why the operator is not `cluster-admin`; service-account separation.
- [secrets-handling.md](secrets-handling.md) — Why `get` on `secrets` is enough and `list` is not.
- [../operations/operation-templates.md](../operations/operation-templates.md) — `rbacProfile` per template and the admission rules around `Operation` types.
