# Offline and air-gapped installs

**Status:** Alpha
**Last Updated:** 2026-05-30

This document covers installing the operator on a cluster that does not have direct internet access. The four operational levers are: mirror the container images into a registry the cluster can reach; mirror the Helm charts into an OCI chart registry the cluster can reach; transfer the registration token via out-of-band means; and configure HTTP/HTTPS proxy on the operator's outbound Pull-mode connection (when the cluster reaches the internet only through a forward proxy).

The procedure differs between two real-world topologies:

- **Air-gapped.** No outbound path of any kind. The cluster talks only to the on-prem registry mirror and the on-prem Odoo. Out-of-band file transfer (USB, internal SFTP) moves images and charts into the mirror.
- **Behind a corporate proxy.** Outbound HTTP/HTTPS goes through a forward proxy. The cluster can reach Odoo and chart sources, but only via the proxy. Images can still be pulled directly if the proxy allows registry traffic; many corporate proxies do not, in which case a mirror is still needed.

The same operator and the same chart work in both topologies; the difference is what the chart values point at and how the registration token is delivered.

## Step 1: image mirroring

The bundle pulls the following container images at install time. Mirror each into a registry the cluster can reach (`registry.internal.example.com` is a placeholder).

| Component                   | Upstream image                                                      | Mirror as                                                                |
|-----------------------------|---------------------------------------------------------------------|--------------------------------------------------------------------------|
| Operator                    | `ghcr.io/vworkspace-io/vworkspace-app-operator:<tag>`                | `registry.internal.example.com/vworkspace-io/vworkspace-app-operator:<tag>` |
| Operator CLI / sidecars     | `ghcr.io/vworkspace-io/op-pgtools:<tag>` and similar (see operations chapter) | `registry.internal.example.com/vworkspace-io/op-pgtools:<tag>`              |
| Flux Helm Controller        | `ghcr.io/fluxcd/helm-controller:<tag>`                              | `registry.internal.example.com/fluxcd/helm-controller:<tag>`              |
| Flux Source Controller      | `ghcr.io/fluxcd/source-controller:<tag>`                            | `registry.internal.example.com/fluxcd/source-controller:<tag>`             |
| Velero                      | `velero/velero:<tag>`                                                | `registry.internal.example.com/velero/velero:<tag>`                          |
| Velero node agent / plugin  | `velero/velero-plugin-for-aws:<tag>` (or the relevant provider plugin) | `registry.internal.example.com/velero/velero-plugin-for-aws:<tag>`           |
| cert-manager controller     | `quay.io/jetstack/cert-manager-controller:<tag>`                     | `registry.internal.example.com/jetstack/cert-manager-controller:<tag>`        |
| cert-manager webhook        | `quay.io/jetstack/cert-manager-webhook:<tag>`                        | `registry.internal.example.com/jetstack/cert-manager-webhook:<tag>`           |
| cert-manager cainjector     | `quay.io/jetstack/cert-manager-cainjector:<tag>`                     | `registry.internal.example.com/jetstack/cert-manager-cainjector:<tag>`        |
| external-secrets            | `ghcr.io/external-secrets/external-secrets:<tag>`                    | `registry.internal.example.com/external-secrets/external-secrets:<tag>`       |

The exact tags are recorded in the chart's `values.yaml` for the version you install; pin them at install time and mirror those exact tags. The chart accepts an `images` block that overrides the image registry on every controller in one place:

```yaml
images:
  registry: registry.internal.example.com
```

Mirror once, then run `helm install` with that override. Don't rely on `docker pull` re-tagging — the chart's `Deployment`s reference the configured registry directly.

For pulls from the internal registry that require credentials, the chart accepts an `imagePullSecrets` reference; provide a `Secret` of type `kubernetes.io/dockerconfigjson` in `vworkspace-system` and pass its name in the chart values.

## Step 2: OCI chart mirroring

The operator's own chart and the catalog charts the operator installs are referenced as OCI artifacts. Mirror each into an OCI chart registry the cluster can reach.

The operator's bundle chart:

```
helm pull oci://ghcr.io/vworkspace-io/charts/vworkspace-app-operator --version 0.0.0
helm push vworkspace-app-operator-0.0.0.tgz oci://registry.internal.example.com/charts
```

(The chart version and the source registry vary; `0.0.0` is the placeholder.)

Application charts from the vWorkspace catalog (or upstream charts referenced by the catalog):

```
helm pull oci://registry.example.com/charts/nextcloud --version 6.6.0
helm push nextcloud-6.6.0.tgz oci://registry.internal.example.com/charts
```

The catalog Odoo distributes carries the URL of the chart source per chart. Either configure the catalog to point at your mirror, or use a `HelmRepository` / `OCIRepository` rewrite (Flux supports both). The cleanest path for production is to point the catalog at the mirror so every cluster Odoo manages uses the same internal chart source.

## Step 3: transferring the registration token

The one-time registration token is short, opaque, and time-bounded. In an air-gapped install, you cannot copy it through the network. Options, from most operationally simple to most secure:

- **Out-of-band SFTP.** The Odoo administrator copies the token to an on-prem file share the cluster admin can read.
- **USB key with audit.** Print the token to a sealed container; the cluster admin types or scans it on the cluster's bastion host.
- **Sneakernet manifest.** The Odoo administrator generates a fully-formed `Cluster` CR manifest (with the token embedded) and hand-delivers it. The cluster admin `kubectl apply -f` it.

The operator does not require any specific transport for the token; the only requirements are that the token reach the operator before its TTL expires and that nothing else gets a copy along the way. Once the operator exchanges the token, the long-lived credential lives only in the cluster's Secret store; the registration token is destroyed.

If the token's TTL is too short for your operational reality (a 24-hour default is the bundle's default), the Odoo administrator can configure a longer TTL per organization. The trade-off is risk window: a longer TTL is a larger window where a stolen, unused token is usable.

## Step 4: configuring the outbound proxy

Some "air-gapped" deployments are actually "behind a corporate proxy", which is operationally different. The operator's Pull-mode outbound HTTPS to the control plane can be sent through a forward proxy by configuring the `Cluster` CR:

```yaml
apiVersion: ops.vworkspace.io/v1alpha1
kind: Cluster
metadata:
  name: cluster-prod-1
  namespace: vworkspace-system
spec:
  connectivityMode: pull
  odoo:
    endpoint: https://workspace.example.org
    proxy:
      httpProxy: http://proxy.internal.example.com:3128
      httpsProxy: http://proxy.internal.example.com:3128
      noProxy: "10.0.0.0/8,.svc.cluster.local,.cluster.local"
  registration:
    token: <one-time-token>
```

The same `proxy` block is applied by the operator to the bundled controllers via their Helm values (the chart sets `HTTPS_PROXY`/`HTTP_PROXY`/`NO_PROXY` env vars on the operator pod and on the bundled controllers when `cluster.proxy.applyToBundledControllers=true`, which is the default).

The `noProxy` value must include cluster-internal DNS (`.svc.cluster.local`, `.cluster.local`) so in-cluster traffic is not routed through the proxy.

If the corporate proxy MITMs HTTPS (which is increasingly common), the operator must trust the proxy's CA bundle. Provide it as a `ConfigMap` in `vworkspace-system`:

```
kubectl create configmap -n vworkspace-system corporate-ca \
  --from-file=ca.crt=/path/to/internal-ca-bundle.pem
```

And reference it from the chart values:

```yaml
operator:
  extraCaBundles:
    - name: corporate-ca
      key: ca.crt
```

The operator mounts the ConfigMap and adds the bundle to its in-process root certificate set on startup.

## Step 5: install with mirrored values

The install command itself does not change; the values do:

```
helm install vworkspace-app-operator \
  oci://registry.internal.example.com/charts/vworkspace-app-operator \
  --version 0.0.0 \
  -n vworkspace-system \
  --create-namespace \
  --values airgapped-values.yaml
```

Where `airgapped-values.yaml` carries the registry override, the imagePullSecret name, the proxy block, and the extra CA bundle reference.

## Verifying the install

Two checks specific to the air-gapped/proxy case (the generic checks are in [quickstart.md](quickstart.md)):

```
# Confirm no image pulls reach the internet
kubectl get events -A | grep -i 'Pulling image'
# Every "Pulling image" line should reference registry.internal.example.com.

# Confirm the operator's outbound traffic is using the proxy
kubectl logs -n vworkspace-system deploy/vworkspace-app-operator | grep -i 'proxy'
```

## Updating images and charts

When you upgrade the operator or its bundled controllers in an air-gapped install, the procedure is: mirror the new images, mirror the new chart, then `helm upgrade`. The chart's CRD changes (if any) go through the same conversion-webhook discipline described in [../operate/upgrades.md](../operate/upgrades.md); the air-gapped install does not change the upgrade contract.

For catalog applications, the catalog Odoo distributes is itself a thing you can pin; when a new chart version is added to the catalog, mirror it into the internal OCI registry before the catalog directs clusters at it.

## Related material

- [prerequisites.md](prerequisites.md) — General prerequisites.
- [quickstart.md](quickstart.md) — The unmodified install path.
- [cluster-bootstrap.md](cluster-bootstrap.md) — Full bootstrap; the token-delivery step is where this document substitutes the out-of-band procedure.
- [../security/authentication.md](../security/authentication.md) — Credential lifecycle (the same one as for connected installs).
