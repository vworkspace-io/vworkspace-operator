# Quickstart

**Status:** Alpha
**Last Updated:** 2026-06-06

This is the supported install path. Two commands install the operator, three more verify it, and the rest of this document describes what to check if something goes wrong. The complete bootstrap procedure (including issuing the registration token in Odoo) is in [cluster-bootstrap.md](cluster-bootstrap.md).

Before you start, work through [prerequisites.md](prerequisites.md). The quickstart assumes you have a running Kubernetes cluster, `cluster-admin` access via `kubectl`, an control plane URL you can reach, and the ability to issue a one-time registration token in Odoo.

The version number `0.0.6` below matches the latest tagged release; pick the version you need from [GitHub Releases](https://github.com/vworkspace-io/vworkspace-operator/releases).

## Step 1: install the operator bundle

### Option A — Helm chart (GitHub Release)

No repository clone required:

```
helm upgrade --install vworkspace-operator \
  https://github.com/vworkspace-io/vworkspace-operator/releases/download/v0.0.6/vworkspace-operator-0.0.6.tgz \
  --version 0.0.6 \
  -n vworkspace-system \
  --create-namespace \
  --set image.tag=v0.0.6
```

### Option B — kubectl manifests (GitHub Release)

```
kubectl apply -f https://github.com/vworkspace-io/vworkspace-operator/releases/download/v0.0.6/crds.yaml
kubectl apply -f https://github.com/vworkspace-io/vworkspace-operator/releases/download/v0.0.6/operator.yaml
```

Manifests are rendered from the same Helm chart as Option A (operator-only defaults; no Flux/Velero bundle).

### Option C — Helm chart (in-repo checkout)

From a clone of this repository (tested on kind — see [helm.md](helm.md)):

```
helm install vworkspace-operator ./charts/vworkspace-operator \
  -n vworkspace-system \
  --create-namespace \
  --set image.repository=vworkspace/vworkspace-operator \
  --set image.tag=latest
```

Connectivity is **not** a Helm value — no `agent.enabled` or `controlPlaneBaseUrl` flags. The operator idles until you apply a token `Secret` and `Cluster` CR in Step 2.

Wait until the operator pod reports `Ready`:

```
kubectl -n vworkspace-system rollout status deploy/vworkspace-operator --timeout=180s
```

Full values reference, upgrade path, and kind validation: [helm.md](helm.md).
See [charts/vworkspace-operator/README.md](https://github.com/vworkspace-io/vworkspace-operator/blob/main/charts/vworkspace-operator/README.md) for chart maintainer notes.

### Option: real vWorkspace Server

For local development against the [vWorkspace Server](https://github.com/vworkspace-io/vworkspace-server) docker-compose stack (not the in-repo mock):

1. Start the server: `make up && make init-db` in the server repo ([DEV_ENVIRONMENT.md](https://github.com/vworkspace-io/vworkspace-server/blob/main/docs/development/DEV_ENVIRONMENT.md)).
2. Run `./hack/dev-integration.sh` for `CLUSTER_ID`, `REGISTRATION_TOKEN`, and bootstrap manifests under `hack/out/cluster-bootstrap/` (or `./hack/render-cluster-bootstrap.sh` with env vars).
3. Set `CONTROL_PLANE_BASE_URL` to the URL **reachable from operator pods** (kind on Linux needs the host gateway IP — see [../development/real-control-plane.md](../development/real-control-plane.md)), then apply manifests in Step 2.

Use the mock control plane instead when you do not need Odoo: [../development/mock-control-plane.md](../development/mock-control-plane.md).

The Helm release name (`vworkspace-operator`) and the namespace (`vworkspace-system`) are conventions. The chart works with any release name; the namespace must match `Cluster.spec.namespace` if you change it.

Wait until the operator pod reports `Ready` (Options A–C):

```
kubectl -n vworkspace-system rollout status deploy/vworkspace-app-operator --timeout=180s
```

## Step 2: connect the cluster (declarative golden path)

Generate a one-time registration token in Odoo (Cluster Registry → New Cluster → Issue registration token), then apply a token `Secret` and a `Cluster` CR. Example templates ship at `charts/vworkspace-operator/examples/cluster-bootstrap/`; the server repo can render the same shape via [`hack/render-cluster-bootstrap.sh`](https://github.com/vworkspace-io/vworkspace-server/blob/main/hack/render-cluster-bootstrap.sh).

```
kubectl apply -f charts/vworkspace-operator/examples/cluster-bootstrap/registration-token.secret.yaml
kubectl apply -f charts/vworkspace-operator/examples/cluster-bootstrap/cluster.yaml
kubectl get cluster cluster-prod-1 -w
```

Or inline (replace placeholders; use the UUID from Cluster Registry for `clusterId` when the token is cluster-bound):

```
cat <<'EOF' | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: cluster-prod-1-registration
  namespace: vworkspace-system
type: Opaque
stringData:
  registrationToken: <one-time-token>
---
apiVersion: ops.vworkspace.io/v1alpha1
kind: Cluster
metadata:
  name: cluster-prod-1
spec:
  controlPlaneEndpoint: https://workspace.example.org
  clusterId: <server-issued-uuid>
  registrationTokenSecretRef:
    name: cluster-prod-1-registration
    key: registrationToken
EOF
```

The `Cluster` reconciler exchanges the token for a long-lived bootstrap credential ([../security/authentication.md](../security/authentication.md)), writes `Secret/vworkspace-agent-credentials`, and the Pull-mode agent starts automatically — no second `helm upgrade`, no Deployment patch.

Full procedure and validation: [cluster-bootstrap.md](cluster-bootstrap.md#step-4-connect-the-cluster-declarative-golden-path).

### Break-glass: register CLI

For debugging only — not the supported path. Use the Deployment name from Step 1 (`vworkspace-operator` for Option A, `vworkspace-app-operator` for Option B):

```
kubectl -n vworkspace-system exec deploy/<operator-deployment> -- \
  /manager register \
    --token=<one-time-token> \
    --control-plane-endpoint=https://workspace.example.org \
    --cluster-name=cluster-prod-1 \
    --cluster-id=<server-issued-uuid>
```

See [cluster-bootstrap.md#break-glass-register-cli](cluster-bootstrap.md#break-glass-register-cli).

## Step 3: validate

The operator publishes its overall health on the `Cluster` CR's `status.conditions[Connected]`. The expected steady-state output:

```
kubectl get cluster cluster-prod-1 -o jsonpath='{.status.conditions[?(@.type=="Connected")]}'
{"type":"Connected","status":"True","reason":"ControlPlaneReachable","message":"Last successful round-trip 4s ago"}
```

If `status` is `True`, the operator is online and pulling jobs. Three other useful checks:

```
# CRDs registered
kubectl get crd applicationinstances.apps.vworkspace.io operations.ops.vworkspace.io

# Bundled controllers running
kubectl get pods -n vworkspace-system
kubectl get pods -n velero
kubectl get pods -n cert-manager
kubectl get pods -n external-secrets

# Cluster's overall readiness from the operator's perspective
kubectl get cluster cluster-prod-1 -o yaml
```

`Cluster.status` aggregates all the prerequisites the operator checks at startup (CRDs present, RBAC present, bundled controllers reconciling). A missing prerequisite shows up as `Cluster.status.conditions[ControllerMissing]=True` with an actionable message.

## Step 4: first deploy

With the cluster connected, you can create your first `ApplicationInstance` from Odoo. Open the Workspace Hub in Odoo, click "Deploy app", pick an entry from the catalog (Nextcloud is a good first choice), confirm, and watch the resulting CR appear:

```
kubectl get applicationinstance -A
kubectl get helmreleases -A
```

**Contract-only (Flux CRDs, no controllers):** common on the Phase 1 kind path (`INSTALL_FLUX_CRDS=true` in [helm.md](helm.md)). The operator creates `ApplicationInstance` and `HelmRelease` objects; the control-plane instance may stay `deploying` and `Ready` may not appear — that is expected. See [cluster-bootstrap.md#flux-contract-only-vs-full-reconcile](cluster-bootstrap.md#flux-contract-only-vs-full-reconcile).

**Full reconcile (Flux controllers running):** install the operator bundle with bundled Flux ([prerequisites.md](prerequisites.md#controllers-installed-by-the-operators-bundle)) or add controllers in dev ([helm.md#optional-flux-controllers-for-ready](helm.md#optional-flux-controllers-for-ready)). Then watch:

```
kubectl get applicationinstance -A -w
kubectl get helmreleases -A -w
```

Within a few minutes (depending on chart size and image pull time), the application's URL appears in the Workspace Hub. The `ApplicationInstance` reports `Ready=True`, the underlying `HelmRelease` reports `Ready=True`, and you can visit the URL.

If you prefer to apply a CR directly without vWorkspace Server (useful for the first sanity check), the following works once the cluster is registered:

```
cat <<'EOF' | kubectl apply -f -
apiVersion: apps.vworkspace.io/v1alpha1
kind: ApplicationInstance
metadata:
  name: nextcloud-myteam
  namespace: org-myteam
  labels:
    app.vworkspace.io/managed-by: control-plane
    app.vworkspace.io/cluster-id: cluster-prod-1
  annotations:
    ops.vworkspace.io/backup: velero
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
EOF
```

The `org-myteam` namespace must exist and carry the label `app.vworkspace.io/managed-by=vworkspace` so the operator is willing to manage it; create it (`kubectl create ns org-myteam && kubectl label ns org-myteam app.vworkspace.io/managed-by=vworkspace`) if it doesn't.

## Enabling admission webhooks

Validating webhooks are optional and off by default. Enable them when you need namespace operation allow-lists, concurrent-operation guards, and inline-secret rejection on `ApplicationInstance` values.

**Helm:**

```
helm upgrade vworkspace-operator ./charts/vworkspace-operator \
  -n vworkspace-system \
  --reuse-values \
  --set webhooks.enabled=true
```

**Kustomize:** uncomment the `[WEBHOOK]` sections in `config/default/kustomization.yaml` and `config/crd/kustomization.yaml`, apply cert-manager (or mount a TLS Secret at `/tmp/k8s-webhook-server/serving-certs`), and run the manager with `--webhooks-enabled=true`.

**TLS:** the webhook server listens on port `9443` and expects `tls.crt` and `tls.key` in `--webhook-cert-path` (default `/tmp/k8s-webhook-server/serving-certs`). For development, cert-manager's `Certificate` resource under `config/certmanager/` is the supported path; self-signed certs work on kind if the `ValidatingWebhookConfiguration` CA bundle matches.

**Namespace policy:** annotate a namespace to restrict operation types, for example `ops.vworkspace.io/allowed-types: Backup,Restore,Upgrade`.

## Troubleshooting

If `Cluster.status.conditions[Connected]` is `False`:

- `ControlPlaneUnreachable`: the operator cannot reach `Cluster.spec.controlPlaneEndpoint`. Check DNS, the egress firewall, and the proxy (`Cluster.spec (egress proxy — see pull-mode docs)`) if you set one.
- `RegistrationTokenInvalid`: the token is expired, already-used, or does not match Odoo's expected hash. Re-issue and try again.
- `CredentialMissing`: the operator started but the bootstrap credential Secret has been deleted. Re-run the registration step.
- `ControlPlaneAuthenticationFailed`: the credential is present but control plane rejected it. Likely the credential was revoked from the control-plane side; re-register.

If `ApplicationInstance` is stuck in `Reconciling=True`:

- If `kubectl get helmrelease -A` shows objects but `kubectl get deploy -A | grep helm-controller` is empty, you have CRDs only — install Flux controllers ([cluster-bootstrap.md#flux-contract-only-vs-full-reconcile](cluster-bootstrap.md#flux-contract-only-vs-full-reconcile)) before expecting `Ready`.
- Check the underlying `HelmRelease`: `kubectl get helmrelease -A`. If Flux reports `Ready=False`, the chart's own reconcile is failing; `kubectl describe helmrelease` shows the reason.
- Check the chart source: `kubectl get helmrepository,ocirepository -A`. If the source cannot be fetched, Flux will surface the reason.
- Check the operator's logs: `kubectl logs -n vworkspace-system deploy/<operator-deployment>` (same Deployment name as Step 1). The operator's structured logs include the `application_instance` name on every relevant line.

More detail is in [../operate/troubleshooting.md](../operate/troubleshooting.md).

## Related material

- [prerequisites.md](prerequisites.md) — Before you run `helm install`.
- [cluster-bootstrap.md](cluster-bootstrap.md) — The longer-form bootstrap including how to issue the token in Odoo.
- [kubernetes-distros.md](kubernetes-distros.md) — Distro-specific gotchas.
- [offline-and-airgapped.md](offline-and-airgapped.md) — Air-gapped installs.
- [../operate/troubleshooting.md](../operate/troubleshooting.md) — Common failures and the commands to diagnose them.
