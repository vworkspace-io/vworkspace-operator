# vworkspace-operator Helm chart

Install the operator, CRDs, and RBAC via Helm. User-facing install guide: [docs/install/helm.md](../../docs/install/helm.md).

## Install from repository checkout

```bash
helm install vworkspace-operator ./charts/vworkspace-operator \
  -n vworkspace-system \
  --create-namespace \
  --set image.tag=latest \
  --set agent.enabled=true \
  --set agent.odooBaseUrl=http://mock-odoo:8080
```

Validate rendering without applying:

```bash
helm template vworkspace-operator ./charts/vworkspace-operator \
  --namespace vworkspace-system
```

Validate on kind:

```bash
./hack/validate-helm-kind.sh
```

## Values

| Key | Default | Description |
|-----|---------|-------------|
| `image.repository` | `vworkspace/vworkspace-operator` | Operator image |
| `image.tag` | Chart `appVersion` | Image tag |
| `agent.enabled` | `false` | Enable Pull-mode job poller |
| `agent.odooBaseUrl` | `https://odoo.example.org` | Odoo or mock Odoo base URL |
| `agent.credentialsSecret` | `vworkspace-agent-credentials` | Secret for agent bearer token |
| `crds.install` | `true` | Install CRDs from `crds/` |

See [values.yaml](values.yaml) for the full list.

## Notes

- This chart installs **only** the vworkspace-operator controller. Flux, Velero, cert-manager, and other prerequisites remain separate ([prerequisites.md](../../docs/install/prerequisites.md)).
- CRD files are copied from `config/crd/bases/` during chart maintenance; run `make manifests` before refreshing chart CRDs.
- Post-install hints are rendered in `templates/NOTES.txt`.
