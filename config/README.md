# config/

Kustomize manifests for installing the vworkspace-operator and its CRDs.

## Layout

| Directory | Contents |
|-----------|----------|
| `crd/bases/` | Generated `CustomResourceDefinition` YAML for `ApplicationInstance`, `Operation`, and `Cluster`. Regenerate with `make manifests`. |
| `crd/kustomization.yaml` | Kustomize entrypoint for CRD install (`make install`). |
| `rbac/` | Generated `ClusterRole` / bindings for the manager ServiceAccount. |
| `manager/` | Operator `Deployment` (manager container, probes, flags). |
| `default/` | Top-level install overlay consumed by `make deploy`. |
| `prometheus/` | Optional `ServiceMonitor` scaffold (disabled in default kustomization). Enable per [docs/operate/observability.md](../docs/operate/observability.md#prometheus-scrape). |
| `network-policy/` | Metrics network policy scaffold. |
| `samples/` | Example CRs for docs and manual testing. |

## Typical commands

```bash
make install          # CRDs only
make deploy IMG=...   # CRDs + operator Deployment
make undeploy
make uninstall        # remove CRDs
```

`make deploy` installs the operator only. It does **not** install Flux, Velero, cert-manager, or external-secrets. Install those via the project Helm bundle documented in [docs/install/quickstart.md](../docs/install/quickstart.md).

## Samples

- `config/samples/apps_v1alpha1_applicationinstance.yaml` — example application.
- `config/samples/ops_v1alpha1_operation.yaml` — Velero backup example and Cluster stub.

## Regenerating

After editing kubebuilder markers under `api/` or RBAC comments on reconcilers:

```bash
make manifests generate
./hack/verify-generated.sh
```

See [docs/development/build-and-test.md](../docs/development/build-and-test.md).
