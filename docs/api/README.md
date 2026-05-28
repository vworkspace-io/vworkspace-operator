# API reference

**Status:** Alpha — CRDs are at `v1alpha1` and may evolve.
**Last Updated:** 2026-05-28

`vworkspace-operator` owns exactly two CRDs. Everything Odoo asks the cluster to do — install an application, upgrade it, back it up, restore it, run a runbook — is expressed as one of these resources. Both CRDs are at `v1alpha1`; breaking changes will go through a conversion webhook and a deprecation window of at least one minor release (see the parent project's [ROADMAP](https://github.com/vworkspace-io/vworkspace/blob/main/docs/ROADMAP.md)).

## Resources

- [application-instance.md](application-instance.md) — `apps.vworkspace.io/v1alpha1` `ApplicationInstance`. Desired state for one application in one namespace, deployed via Helm.
- [operation.md](operation.md) — `ops.vworkspace.io/v1alpha1` `Operation`. A day-2 action (backup, restore, upgrade, migration, runbook) against an `ApplicationInstance`.

## Shared vocabulary

- [conditions.md](conditions.md) — Standard condition types and reasons emitted on `ApplicationInstance`, `Operation`, and the operator's own `Cluster` status.
- [labels-and-annotations.md](labels-and-annotations.md) — Well-known labels the operator reads and writes (managed-by, cluster-id, application-instance) and capability annotations under `ops.vworkspace.io/*`.

## Stability

The CRDs and the surrounding API surface (labels, annotations, condition types and reasons, admission rules) are versioned together. The `v1alpha1` label is a deliberate signal: the project will iterate on the schema and the validation rules through several alphas before promoting to `v1beta1` and then `v1`. Pin operator versions in production and read the repository [CHANGELOG](../../CHANGELOG.md) before upgrading.

For the conceptual treatment of *why* the API surface is two CRDs and a status vocabulary, see [../concepts/crds.md](../concepts/crds.md).
