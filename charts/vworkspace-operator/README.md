# vworkspace-operator Helm chart

Alpha scaffold for installing the operator, CRDs, and RBAC via Helm (Phase 1d-b).

## Install from repository checkout

```bash
helm install vworkspace-operator ./charts/vworkspace-operator \
  -n vworkspace-system \
  --create-namespace \
  --set image.tag=latest \
  --set agent.enabled=true \
  --set agent.odooBaseURL=http://mock-odoo:8080
```

Validate rendering without applying:

```bash
helm template vworkspace-operator ./charts/vworkspace-operator \
  --namespace vworkspace-system
```

## Values

| Key | Default | Description |
|-----|---------|-------------|
| `image.repository` | `docker.io/vworkspace/vworkspace-operator` | Operator image |
| `image.tag` | Chart `appVersion` | Image tag |
| `agent.enabled` | `false` | Enable Pull-mode job poller |
| `agent.odooBaseURL` | `https://odoo.example.org` | Odoo or mock Odoo base URL |
| `crds.install` | `true` | Install CRDs from `crds/` |

See [values.yaml](values.yaml) for the full list.

## Notes

- This chart installs **only** the vworkspace-operator controller. Flux, Velero, cert-manager, and other prerequisites remain separate ([prerequisites.md](../../docs/install/prerequisites.md)).
- CRD files are copied from `config/crd/bases/` during chart maintenance; run `make manifests` before refreshing chart CRDs.
