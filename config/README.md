# config/

This directory will hold the Kubebuilder-generated kustomize layout once the Phase 1 Go scaffold lands (see [ROADMAP.md](../ROADMAP.md)).

The intended structure follows the upstream Kubebuilder convention:

- `config/crd/` — `CustomResourceDefinition` manifests for `ApplicationInstance` and `Operation`, plus the conversion webhook configuration.
- `config/rbac/` — `ClusterRole`, `Role`, and `RoleBinding` manifests granting the operator the least-privilege permissions described in [docs/security/rbac.md](../docs/security/rbac.md).
- `config/manager/` — the operator `Deployment` and its supporting resources.
- `config/webhook/` — admission and conversion webhook server manifests.
- `config/default/` — the top-level kustomization that composes the above into a working install.
- `config/samples/` — example `ApplicationInstance` and `Operation` manifests for trying the operator quickly.

This directory is intentionally a placeholder until the Go scaffold is committed. The Helm chart packaged for end users is generated from these manifests; see [docs/development/release-process.md](../docs/development/release-process.md) for the release pipeline.

If you are looking for usage examples now, see [docs/api/application-instance.md](../docs/api/application-instance.md) and [docs/api/operation.md](../docs/api/operation.md).
