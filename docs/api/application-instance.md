# `ApplicationInstance`

**Group/Version/Kind:** `apps.vworkspace.io/v1alpha1` `ApplicationInstance`
**Scope:** Namespaced
**Status:** Alpha
**Last Updated:** 2026-05-30

## Overview

`ApplicationInstance` represents the intent "this application should exist in this namespace, deployed from this chart, with these chart parameters." The operator materializes a Flux `HelmRelease` (and the matching chart-source resource, an `OCIRepository` or `HelmRepository`) from the spec, watches it, and aggregates its conditions back into `status.conditions`. App-specific deployment logic stays in the upstream chart; the operator does not know what a Deployment or Service produced by the chart is for.

The reconciliation model is documented in [../concepts/reconciliation-model.md](../concepts/reconciliation-model.md). The shorter conceptual tour is in [../concepts/crds.md](../concepts/crds.md). The reason this is a Helm-first CRD is in [../concepts/helm-first.md](../concepts/helm-first.md).

## Example

```yaml
apiVersion: apps.vworkspace.io/v1alpha1
kind: ApplicationInstance
metadata:
  name: nextcloud-myteam
  namespace: org-myteam
  labels:
    app.vworkspace.io/managed-by: odoo
    app.vworkspace.io/cluster-id: cluster-prod-1
  annotations:
    ops.vworkspace.io/backup: velero
    ops.vworkspace.io/restore: velero
    ops.vworkspace.io/upgrade: helm
spec:
  appRef:
    catalogId: nextcloud
  chart:
    sourceType: oci
    url: oci://registry.example.com/charts
    name: nextcloud
    version: "6.6.0"
  release:
    name: nextcloud-myteam
    namespace: org-myteam
  values:
    source: inline
    inline:
      ingress:
        enabled: true
        host: files.myteam.example.com
      integrations:
        externalSecrets:
          enabled: true
        certManager:
          enabled: true
status:
  observedGeneration: 3
  conditions: []
  helmReleaseRef:
    name: nextcloud-myteam
```

The labels and annotations are documented in [labels-and-annotations.md](labels-and-annotations.md). The condition vocabulary is in [conditions.md](conditions.md).

## `spec`

### `appRef`

| Field            | Type   | Required | Description |
|------------------|--------|----------|-------------|
| `appRef.catalogId` | string | Yes      | Stable identifier for the catalog entry this instance corresponds to (e.g., `nextcloud`, `mattermost`, `vaultwarden`). The catalog entry lives in Odoo and carries the curated capability metadata. The operator does not look the catalog up at apply time; the value is recorded for fleet-wide queries and for the audit channel. |

### `chart`

Identifies the Helm chart to deploy.

| Field                | Type   | Required | Description |
|----------------------|--------|----------|-------------|
| `chart.sourceType`   | enum   | Yes      | One of `oci` (an OCI registry hosting Helm charts) or `helm` (a classic Helm chart repository). The admission webhook rejects any other value. |
| `chart.url`          | string | Yes      | The base URL of the chart source. For `oci`, an `oci://` URL pointing at the registry path (e.g., `oci://registry.example.com/charts`). For `helm`, an `https://` URL pointing at the chart repository index. |
| `chart.name`         | string | Yes      | The chart name within the source. |
| `chart.version`      | string | Yes      | Exact chart version (semver). Version ranges are not allowed; the catalog is responsible for resolving "latest" or "recommended" to a concrete version before emitting the `ApplicationInstance`. |

### `release`

Identifies the Helm release the operator will materialize.

| Field                | Type   | Required | Description |
|----------------------|--------|----------|-------------|
| `release.name`       | string | Yes      | The Helm release name. Used as the `metadata.name` of the materialized `HelmRelease` and, conventionally, as the prefix for chart-generated objects. |
| `release.namespace`  | string | Yes      | The target namespace for the release. Must match `metadata.namespace` on the `ApplicationInstance` itself. The admission webhook rejects mismatches. |

### `values`

Specifies how chart values are supplied to the release.

| Field                  | Type   | Required | Description |
|------------------------|--------|----------|-------------|
| `values.source`        | enum   | Yes      | One of `inline`, `secretRef`, or `configMapRef`. Determines which of the sibling fields is read. |
| `values.inline`        | object | Conditional | Required when `values.source` is `inline`. Free-form YAML mapping passed verbatim to the chart as values. |
| `values.secretRef`     | object | Conditional | Required when `values.source` is `secretRef`. `{ name, key }` referencing a `Secret` in the same namespace; the key holds the values document (YAML). |
| `values.configMapRef`  | object | Conditional | Required when `values.source` is `configMapRef`. `{ name, key }` referencing a `ConfigMap` in the same namespace; the key holds the values document (YAML). |

The operator never logs decrypted values material. When values are sourced from a `Secret`, the operator only reads the secret when reconciling the `HelmRelease` and does not include the content in events, conditions, or status fields.

### `integrations` *(optional)*

Convenience knobs that toggle commonly used chart features. These do not replace chart values; they are syntactic sugar that the operator translates into the corresponding values fragments on a per-catalog-entry basis.

| Field                                         | Type    | Default | Description |
|-----------------------------------------------|---------|---------|-------------|
| `integrations.externalSecrets.enabled`        | bool    | false   | Configure the chart to consume secrets through external-secrets. Requires external-secrets to be present on the cluster. |
| `integrations.certManager.enabled`            | bool    | false   | Configure the chart's ingress to request TLS certificates via cert-manager. Requires cert-manager to be present on the cluster. |
| `integrations.ingress.enabled`                | bool    | false   | Enable the chart's ingress definition. |
| `integrations.ingress.host`                   | string  | —       | Hostname for the ingress. Required when `integrations.ingress.enabled` is true. |

When both `integrations.*` and explicit chart values cover the same field, explicit values take precedence; the admission webhook reports a warning condition but does not reject the resource.

## `status`

The operator owns `status` exclusively under its field manager (typically `vworkspace-operator`). Clients should not write to `status` directly; the status subresource enforces this at the API server level.

| Field                        | Type     | Description |
|------------------------------|----------|-------------|
| `status.observedGeneration`  | int64    | The `metadata.generation` of the `ApplicationInstance` that produced the current `status`. Lets clients detect stale status. |
| `status.conditions[]`        | []Condition | Standard Kubernetes condition objects. The type vocabulary is `Ready`, `Reconciling`, `Degraded`, `Suspended`, `Blocked`, `Deleting`; see [conditions.md](conditions.md). |
| `status.helmReleaseRef`      | object   | `{ name, namespace }` of the materialized `HelmRelease`. Populated as soon as the `HelmRelease` exists. |
| `status.lastAppliedChart`    | object   | `{ sourceType, url, name, version }` snapshot of the chart coordinates the operator most recently materialized. Useful for distinguishing "what was asked" from "what is running" during a rolling upgrade. |
| `status.lastReconcileTime`   | string   | RFC 3339 timestamp of the last reconcile. |
| `status.endpoints[]`         | []Endpoint | URLs and hosts derived from the chart's ingress and service outputs. Each entry has `name`, `url`, optional `type` (e.g., `ingress`, `service`), and optional `notes`. Populated when the chart's outputs are visible to the operator's informers. |
| `status.inventory`           | object (optional) | Reference to an inventory object listing the resources the chart produced. Populated when inventory tracking is enabled at the operator level. |

## Conditions

See [conditions.md](conditions.md) for the full list of condition types, statuses, and reasons emitted on `ApplicationInstance.status.conditions[]`.

## Validation rules

The operator ships a validating admission webhook (`validate.applicationinstances.apps.vworkspace.io`). Key rules:

- **Allowed chart sources.** `chart.sourceType` must be `oci` or `helm`. Other values are rejected. The set of allowed `chart.url` prefixes is configurable per cluster — the admission webhook can be told that only specific registries or repositories are allowed, in which case any URL outside the allow-list is rejected.
- **Version constraints.** `chart.version` must be a concrete semver string (no ranges, no `latest`). Per-catalog-entry version constraints (allowed versions, forbidden versions, minimum versions) can be enforced by the webhook when the cluster has a copy of the catalog policy.
- **Namespace consistency.** `release.namespace` must equal `metadata.namespace`. The webhook rejects mismatches to avoid cross-namespace surprises.
- **Namespace gating.** The `ApplicationInstance` must live in a namespace labeled `managed-by=vworkspace`. The operator does not act on `ApplicationInstance` resources in other namespaces; the webhook makes this explicit by rejecting create/update in ungated namespaces.
- **Values exclusivity.** Exactly one of `values.inline`, `values.secretRef`, `values.configMapRef` is populated, matching `values.source`. The webhook rejects multiple or absent sources.
- **Immutability of release identity.** `release.name` and `release.namespace` are immutable after creation. Renaming a release in place would orphan the underlying `HelmRelease`; the right move is to delete and recreate the `ApplicationInstance`.

The webhook is strict by default. Soft warnings (deprecated chart source, deprecated integration shortcut) are surfaced as conditions on the resource rather than as admission rejections, so the operator can ship deprecations without breaking applies.
