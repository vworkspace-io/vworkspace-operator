# Quickstart

**Status:** Alpha
**Last Updated:** 2026-05-29

This is the supported install path. Two commands install the operator, three more verify it, and the rest of this document describes what to check if something goes wrong. The complete bootstrap procedure (including issuing the registration token in Odoo) is in [cluster-bootstrap.md](cluster-bootstrap.md).

Before you start, work through [prerequisites.md](prerequisites.md). The quickstart assumes you have a running Kubernetes cluster, `cluster-admin` access via `kubectl`, an Odoo URL you can reach, and the ability to issue a one-time registration token in Odoo.

The version number `0.0.0` below is a placeholder for the chart version you actually install; substitute the real one from the project's releases page when the operator has its first tagged release.

## Step 1: install the operator bundle

```
helm install vworkspace-app-operator \
  oci://registry.example.com/charts/vworkspace-app-operator \
  --version 0.0.0 \
  -n vworkspace-system \
  --create-namespace
```

This installs the operator, its CRDs, its RBAC, and the bundled controllers (Flux Helm Controller, Source Controller, cert-manager, external-secrets, Velero). It does not install Argo Workflows or an ingress controller; bring those yourself when you need them ([prerequisites.md](prerequisites.md)).

The Helm release name (`vworkspace-app-operator`) and the namespace (`vworkspace-system`) are conventions. The chart works with any release name; the namespace must match `Cluster.spec.namespace` if you change it.

Wait until the operator's pod reports `Ready`:

```
kubectl -n vworkspace-system rollout status deploy/vworkspace-app-operator --timeout=180s
```

## Step 2: register the cluster with Odoo

The operator needs to know who it is. Generate a one-time registration token in Odoo (Cluster Registry → New Cluster → Issue registration token) and exchange it via either the CLI helper:

```
kubectl -n vworkspace-system exec deploy/vworkspace-app-operator \
  -- vworkspace-app-operator register --token=<one-time-token>
```

…or by applying a `Cluster` CR that carries the token:

```
cat <<'EOF' | kubectl apply -f -
apiVersion: ops.vworkspace.io/v1alpha1
kind: Cluster
metadata:
  name: cluster-prod-1
  namespace: vworkspace-system
spec:
  connectivityMode: pull
  odoo:
    endpoint: https://workspace.example.org
  registration:
    token: <one-time-token>
EOF
```

The two paths produce identical results. The CLI helper is convenient for terminals; the CR path is convenient for GitOps and for the air-gapped install where the token is delivered out of band.

The operator's `Cluster` reconciler exchanges the token for a long-lived bootstrap credential ([../security/authentication.md](../security/authentication.md)), persists it in a Kubernetes `Secret`, and begins pulling jobs when Pull-mode is enabled.

### Enable the Pull-mode agent

Create a Secret with the bootstrap credential (or set equivalent flags on the Deployment):

```bash
kubectl -n vworkspace-system create secret generic vworkspace-agent-credentials \
  --from-literal=odoo-base-url=https://workspace.example.org \
  --from-literal=cluster-id=cluster-prod-1 \
  --from-literal=token='<long-lived-token>'
```

Patch the operator Deployment to enable the agent loop:

```bash
kubectl -n vworkspace-system patch deploy controller-manager \
  --type=json -p='[
    {"op":"replace","path":"/spec/template/spec/containers/0/args","value":[
      "--leader-elect",
      "--health-probe-bind-address=:8081",
      "--agent-enabled=true",
      "--agent-credentials-secret=vworkspace-agent-credentials"
    ]}
  ]'
```

See [../connectivity/pull-mode.md](../connectivity/pull-mode.md) for the full flag and Secret key reference.

Install the published image with kustomize:

```bash
make deploy IMG=docker.io/vworkspace/vworkspace-operator:latest
```

See [container-images.md](container-images.md) for Docker Hub tags and CI publishing.

## Step 3: validate

The operator publishes its overall health on the `Cluster` CR's `status.conditions[Connected]`. The expected steady-state output:

```
kubectl get cluster -n vworkspace-system cluster-prod-1 -o jsonpath='{.status.conditions[?(@.type=="Connected")]}'
{"type":"Connected","status":"True","reason":"OdooReachable","message":"Last successful round-trip 4s ago"}
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
kubectl get cluster -n vworkspace-system cluster-prod-1 -o yaml
```

`Cluster.status` aggregates all the prerequisites the operator checks at startup (CRDs present, RBAC present, bundled controllers reconciling). A missing prerequisite shows up as `Cluster.status.conditions[ControllerMissing]=True` with an actionable message.

## Step 4: first deploy

With the cluster connected, you can create your first `ApplicationInstance` from Odoo. Open the Workspace Hub in Odoo, click "Deploy app", pick an entry from the catalog (Nextcloud is a good first choice), confirm, and watch the resulting CR appear:

```
kubectl get applicationinstance -A -w
```

Within a few minutes (depending on chart size and image pull time), the application's URL appears in the Workspace Hub. The `ApplicationInstance` reports `Ready=True`, the underlying `HelmRelease` reports `Ready=True`, and you can visit the URL.

If you prefer to apply a CR directly without Odoo (useful for the first sanity check), the following works once the cluster is registered:

```
cat <<'EOF' | kubectl apply -f -
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

## Troubleshooting

If `Cluster.status.conditions[Connected]` is `False`:

- `OdooUnreachable`: the operator cannot reach `Cluster.spec.odoo.endpoint`. Check DNS, the egress firewall, and the proxy (`Cluster.spec.odoo.proxy`) if you set one.
- `RegistrationTokenInvalid`: the token is expired, already-used, or does not match Odoo's expected hash. Re-issue and try again.
- `CredentialMissing`: the operator started but the bootstrap credential Secret has been deleted. Re-run the registration step.
- `OdooAuthenticationFailed`: the credential is present but Odoo rejected it. Likely the credential was revoked from the Odoo side; re-register.

If `ApplicationInstance` is stuck in `Reconciling=True`:

- Check the underlying `HelmRelease`: `kubectl get helmrelease -A`. If Flux reports `Ready=False`, the chart's own reconcile is failing; `kubectl describe helmrelease` shows the reason.
- Check the chart source: `kubectl get helmrepository,ocirepository -A`. If the source cannot be fetched, Flux will surface the reason.
- Check the operator's logs: `kubectl logs -n vworkspace-system deploy/vworkspace-app-operator`. The operator's structured logs include the `application_instance` name on every relevant line.

More detail is in [../operate/troubleshooting.md](../operate/troubleshooting.md).

## Related material

- [prerequisites.md](prerequisites.md) — Before you run `helm install`.
- [cluster-bootstrap.md](cluster-bootstrap.md) — The longer-form bootstrap including how to issue the token in Odoo.
- [kubernetes-distros.md](kubernetes-distros.md) — Distro-specific gotchas.
- [offline-and-airgapped.md](offline-and-airgapped.md) — Air-gapped installs.
- [../operate/troubleshooting.md](../operate/troubleshooting.md) — Common failures and the commands to diagnose them.
