# Real vWorkspace Server (control plane) development

**Status:** Alpha — Phase 3 integration path.
**Last Updated:** 2026-05-30

Use the [vWorkspace Server](https://github.com/vworkspace-io/vworkspace-server) docker-compose stack when you need to validate Pull-mode against the real agent API instead of the in-repo [mock control plane](mock-control-plane.md).

## Prerequisites

- vWorkspace Server running locally ([DEV_ENVIRONMENT.md](https://github.com/vworkspace-io/vworkspace-server/blob/main/docs/development/DEV_ENVIRONMENT.md)): `make up && make init-db`, default URL `http://127.0.0.1:8069`.
- A Kubernetes context (kind/k3s) if you register via `manager register` or deploy the operator in-cluster.
- A one-time registration token from **Cluster Registry → Issue registration token** in the server UI.

Agent API reference (server repo): [agent-api.md](https://github.com/vworkspace-io/vworkspace-server/blob/main/docs/connectivity/agent-api.md).

## Quick hints script

```bash
./hack/dev-real-control-plane.sh
```

Set `VWORKSPACE_REGISTRATION_TOKEN` before running to print a complete `register` command. Override `CONTROL_PLANE_BASE_URL` if the default host URL is wrong for your setup.

| Variable | Default | Purpose |
|----------|---------|---------|
| `CONTROL_PLANE_BASE_URL` | `http://127.0.0.1:8069` (Linux host) / `http://host.docker.internal:8069` (macOS) | Control plane base URL |
| `VWORKSPACE_SERVER_PORT` | `8069` | Odoo HTTP port in docker-compose |
| `VWORKSPACE_REGISTRATION_TOKEN` | (empty) | One-time token for printed register command |
| `VWORKSPACE_CLUSTER_NAME` | `cluster-dev-1` | Cluster CR name / register `--cluster-name` |

## Networking: kind on Linux

Pods inside a kind cluster cannot reach `127.0.0.1` on your workstation — that is the pod loopback, not the host.

| Where the operator runs | Control plane URL |
|-------------------------|-------------------|
| `make run` on the host | `http://127.0.0.1:8069` |
| Operator pod in kind (Linux) | `http://<docker-bridge-gateway>:8069` — often `docker network inspect kind -f '{{(index .IPAM.Config 0).Gateway}}'` |
| Operator pod in kind (macOS) | `http://host.docker.internal:8069` often works |

Set Helm `agent.controlPlaneBaseUrl` or `--control-plane-base-url` to the URL **as seen from the operator process**, not from your laptop browser.

## Register and enable Pull-mode

**CLI (local kube context):**

```bash
export CONTROL_PLANE_BASE_URL=http://127.0.0.1:8069
export VWORKSPACE_REGISTRATION_TOKEN=vwksp-reg-...

go run ./cmd/main.go register \
  --control-plane-endpoint "${CONTROL_PLANE_BASE_URL}" \
  --token "${VWORKSPACE_REGISTRATION_TOKEN}" \
  --cluster-name cluster-dev-1 \
  --namespace vworkspace-system
```

**Operator with agent loop:**

```bash
make run -- \
  --agent-enabled=true \
  --control-plane-base-url="${CONTROL_PLANE_BASE_URL}" \
  --agent-credentials-secret=vworkspace-agent-credentials
```

**Helm on kind** — see [quickstart.md](../install/quickstart.md#option-real-vworkspace-server) and [pull-mode.md](../connectivity/pull-mode.md#registration-with-vworkspace-server).

## Integration test (live server)

Not run in default CI. Requires a fresh registration token (single-use).

```bash
export CONTROL_PLANE_BASE_URL=http://127.0.0.1:8069
export VWORKSPACE_REGISTRATION_TOKEN=vwksp-reg-...
go test -tags=integration ./test/integration/... -run TestRealControlPlane -count=1 -v
```

## Golden path (with Platform)

End-to-end validation with a server-deployed app job is coordinated outside this repo:

1. Deploy operator on kind with Flux CRDs (`INSTALL_FLUX_CRDS=true` in [validate-helm-kind.sh](https://github.com/vworkspace-io/vworkspace-operator/blob/main/hack/validate-helm-kind.sh)).
2. Register against server docker-compose.
3. Enqueue deploy from server UI or test helper; confirm `ApplicationInstance` + `HelmRelease`.
4. Confirm `POST /api/agent/events` updates visible on the server.

## Related

- [mock-control-plane.md](mock-control-plane.md) — CI and offline dev without server.
- [IMPLEMENTATION_GUIDE.md](IMPLEMENTATION_GUIDE.md) — Phase 3 checklist.
- [pull-mode.md](../connectivity/pull-mode.md) — Pull-mode protocol and flags.
