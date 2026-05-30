# Secrets handling

**Status:** Alpha
**Last Updated:** 2026-05-30

Application charts almost always need credentials: a database password, an S3 access key, an SMTP password, an OIDC client secret. How those credentials reach the chart matters: inline secret material in CRDs is a recurring source of leaks (audit logs, GitOps Git histories, JSON dumps in incident tickets); references to Kubernetes `Secret` objects, populated by `external-secrets` or by a human operator out of band, are safe. This document describes how `vworkspace-operator` handles secret-bearing chart values, what its admission webhook rejects, and how the Pull-mode transport handles payloads that contain secrets.

## The rule

Chart values that contain credentials, tokens, certificates, or other secret material **must not** be inlined into `ApplicationInstance.spec.values.inline`. Use one of the two supported references instead:

| Pattern                             | Where the secret lives                                                                        | How the chart sees it                                              |
|-------------------------------------|-----------------------------------------------------------------------------------------------|--------------------------------------------------------------------|
| `external-secrets`                  | An `ExternalSecret` in the application's namespace, pulling from Vault / AWS / GCP / Azure / Akeyless / an on-prem store. | The chart references the target `Secret` by name (e.g., via a `secretKeyRef` in a chart value).         |
| `secretRef` / `configMapRef`        | A `Secret` (or `ConfigMap`) in the application's namespace, created out of band by a human operator or a sealed-secrets controller. | `ApplicationInstance.spec.values.secretRef` (or `configMapRef`) points at it; the operator passes the data through to the Helm engine as values.              |

Both patterns let the secret live where Kubernetes RBAC controls access to it, not in the CRD that Odoo, Flux, and the operator all stream around.

## What "inline" means

`ApplicationInstance.spec.values.inline` is a YAML object passed directly to the Helm engine as values. It is the right place for non-secret configuration:

```yaml
spec:
  values:
    source: inline
    inline:
      ingress:
        enabled: true
        host: files.myteam.example.com
      replicaCount: 3
      service:
        type: ClusterIP
```

It is the wrong place for anything that looks like a secret:

```yaml
spec:
  values:
    source: inline
    inline:
      postgresql:
        auth:
          password: "hunter2"          # rejected by the webhook
          existingSecret: ""            # incomplete; better to reference an ExternalSecret target
      smtp:
        password: "MySmtpPassw0rd!"    # rejected by the webhook
      s3:
        accessKey: "AKIAEXAMPLEEXAMPLE"  # rejected
        secretKey: "..."                # rejected
```

The validating admission webhook flags inline material that matches a placeholder rule set described below and rejects the request with reason `InlineSecretMaterialRejected`. The webhook is enforced for every connectivity mode; an Odoo Push apply, a Pull-mode job payload, and a GitOps-rendered manifest are all evaluated the same way.

## The placeholder rule set

The webhook does not try to be a perfect secret detector. It rejects on a small, intentionally loud set of patterns. The current placeholder rule set:

| Rule key                                      | Matches                                                                                                                          |
|-----------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------|
| `key.endsWith(password)` (case-insensitive)    | Any value whose path's leaf key ends in `password` (e.g., `postgresql.auth.password`, `smtp.password`, `redis.auth.password`).    |
| `key.endsWith(secret)` (case-insensitive)      | Any value whose leaf key ends in `secret` (e.g., `oidc.clientSecret`, `webhook.secret`).                                          |
| `key in {accessKey, secretKey, apiKey, token}`| Leaf keys frequently used for AWS-style credentials, generic API keys, or bearer tokens.                                          |
| `value.matches(^-----BEGIN .*PRIVATE KEY-----)` | PEM-encoded private keys in any field.                                                                                            |
| `value.matches(^[A-Za-z0-9+/=]{40,}$)` if path matches the above keys | Long base64-like values under suspicious keys.                                                                  |

A value that matches but is empty (`postgresql.auth.password: ""`) is admitted; an empty string is not a secret. A value that matches and is `"<set via externalSecret>"` is also admitted as a recognized placeholder.

The rule set is documented as "placeholder" because we expect to tighten it. The webhook is not the only line of defense; chart authors and control plane catalog curators are also expected not to define values whose names invite secrets to be inlined. The rule set is the safety net.

## Recommended pattern: external-secrets

The recommended pattern is to model every secret as an `ExternalSecret` in the application's namespace that synchronizes from a backing store the operator controls (Vault, AWS Secrets Manager, GCP Secret Manager, Azure Key Vault, on-prem MySQL-backed store, etc.). The chart references the resulting `Secret` by name; `ApplicationInstance.spec.values.inline` references the chart-value key that names the Secret, not the secret itself.

A concrete example for Nextcloud's database password, using Vault as the backing store:

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: nextcloud-postgresql
  namespace: org-myteam
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: org-myteam-vault
    kind: SecretStore
  target:
    name: nextcloud-postgresql
    creationPolicy: Owner
  data:
    - secretKey: password
      remoteRef:
        key: secret/data/myteam/nextcloud
        property: db_password
```

The `ApplicationInstance` references the resulting Secret by name in values:

```yaml
apiVersion: apps.vworkspace.io/v1alpha1
kind: ApplicationInstance
metadata:
  name: nextcloud-myteam
  namespace: org-myteam
  annotations:
    ops.vworkspace.io/backup: velero
spec:
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
      postgresql:
        auth:
          existingSecret: nextcloud-postgresql
          secretKeys:
            adminPasswordKey: password
            userPasswordKey: password
```

The `password` field is never named in the `ApplicationInstance`. The webhook admits the request. external-secrets creates the `nextcloud-postgresql` Secret in `org-myteam` from Vault. The chart reads from `existingSecret` and never knows where the value came from.

## Alternative pattern: `secretRef` / `configMapRef`

When external-secrets is not available (small installs, dev clusters, situations where the secret is managed by a human out of band), the operator supports passing a `Secret` or `ConfigMap` as the values payload:

```yaml
spec:
  values:
    source: secretRef
    secretRef:
      name: nextcloud-myteam-values
      key: values.yaml
```

The named `Secret` in the same namespace contains a `values.yaml` key whose data is a full Helm values document. The operator reads it (`get`-by-name; see [rbac.md](rbac.md)) and passes it through to the Helm engine as values. The `Secret` does not need to live in Git; the operator's source of truth is the cluster.

The same shape works for non-secret values via `configMapRef`. Mixing the two is supported: `spec.values.source: merge` accepts an inline tree plus a reference, with the reference taking precedence over inline. The webhook still runs its placeholder rule set on the merged result before admission.

## Pull-mode payload signing and encryption

In Pull mode, the operator's job-fetch endpoint may return job payloads that contain a server-side-apply manifest of an `ApplicationInstance`. If the chart values include credentials and external-secrets is not in play, the manifest will contain a `secretRef`/`configMapRef` to a Secret the operator must already have, or the payload itself must be encrypted to the cluster's public key.

The two protections are:

- **Signed payloads.** Each job payload may carry an control-plane-side signature over its canonical form. The operator verifies the signature before applying. Signature verification protects against a tampered relay between the operator and Odoo, and against a compromised message broker if one is in the path. Signing keys belong to Odoo; the cluster holds Odoo's public key.
- **Encrypted payloads.** When the payload contains chart values that include credentials (an admission the operator can detect by the same placeholder rule set as above), the payload may be encrypted to the cluster's public key. Odoo encrypts on send; the operator decrypts on receive. The cluster's private key is in `vworkspace-system/vworkspace-operator-keypair` and is rotated through the same procedure as the Odoo bootstrap credential.

Signing and encryption are independent toggles in the operator's `Cluster` CR (`spec.security.requireSignedPayloads`, `spec.security.requireEncryptedSecretPayloads`). The default in v1alpha1 is "warn but admit"; an organization that ships secrets through Pull-mode payloads is expected to turn both on for production clusters. The full configuration is described in [authentication.md](authentication.md).

## What the operator does not log

The operator's structured logger redacts any field whose path or key matches the placeholder rule set, replacing the value with `[REDACTED]`. This applies to:

- Chart values that the operator unmarshals into a Go struct before calling the Helm engine.
- Condition messages mirrored from `HelmRelease.status.conditions` (in case Flux ever logs a chart value).
- Audit events sent to the control plane's `POST /api/agent/events`.
- Kubernetes `Event` messages emitted by the operator.

Two places this does not apply:

- The raw `Secret` object data is never read or logged by the operator beyond passing the bytes to the Helm engine. The Helm engine itself is the only component that resolves the values.
- Velero's own logs may include some values. That is Velero's concern; the operator does not control Velero's logging.

## Practical checks

To verify the secrets-handling posture of a running cluster:

```
# No inline material that smells like a secret on any ApplicationInstance
kubectl get applicationinstances -A -o yaml | grep -E '(password|secret|apiKey|token):' \
  | grep -v 'existingSecret\|secretRef\|secretKeyRef'

# external-secrets controllers running and reconciling
kubectl get pods -n external-secrets

# The operator's keypair Secret exists if signing is required
kubectl get secret -n vworkspace-system vworkspace-operator-keypair
```

## Related material

- [authentication.md](authentication.md) — Where signing and encryption keys live and how they rotate.
- [least-privilege.md](least-privilege.md) — Why the operator's `get`-by-name on Secrets is the minimum it needs.
- [rbac.md](rbac.md) — RBAC verbs on `secrets` (only `get`, not `list` or `watch`).
- [../api/application-instance.md](../api/application-instance.md) — `spec.values` field reference.
