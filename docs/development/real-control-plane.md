# Real vWorkspace Server (control plane) development

**Status:** Alpha — Phase 3 integration path.
**Last Updated:** 2026-06-02

Use the [vWorkspace Server](https://github.com/vworkspace-io/vworkspace-server) docker-compose stack when you need to validate Pull-mode against the real agent API instead of the in-repo [mock control plane](mock-control-plane.md).

## Prerequisites

- vWorkspace Server running locally ([DEV_ENVIRONMENT.md](https://github.com/vworkspace-io/vworkspace-server/blob/main/docs/development/DEV_ENVIRONMENT.md)): `make up && make init-db`, default URL `http://127.0.0.1:8069`.
- A Kubernetes context (kind/k3s) if you register via `manager register` or deploy the operator in-cluster.
- A one-time registration token from **Cluster Registry → Issue registration token** in the server UI (or from `./hack/dev-integration.sh` in the server repo).

Agent API reference (server repo): [agent-api.md](https://github.com/vworkspace-io/vworkspace-server/blob/main/docs/connectivity/agent-api.md).

## clusterId (UUID) vs slug

vWorkspace Server assigns each cluster a **stable UUID** (`vws.cluster.cluster_id`). That value is what the Agent API calls `clusterId` on `POST /api/agent/register` and on `GET /api/agent/jobs?cluster=…`.

The **slug** (for example `dev-integration`) is a human-readable name in Cluster Registry. It is fine for `Cluster.metadata.name` in Kubernetes, but sending the slug as `spec.clusterId` or `register --cluster-id` causes **403 Forbidden** when the registration token is bound to a cluster.

| Concept | Example | Used for |
|---------|---------|----------|
| Server slug | `dev-integration` | UI, `VWORKSPACE_CLUSTER_SLUG`, optional K8s resource name |
| Server `clusterId` (UUID) | `a1b2c3d4-…` | `spec.clusterId`, `--cluster-id`, agent poll query |
| K8s `Cluster.metadata.name` | `cluster-local` | `kubectl get cluster`, `--cluster-name` |

Run the server seed script to print the UUID:

```bash
cd ../vworkspace-server
./hack/dev-integration.sh --skip-up   # if stack already up
# exports CLUSTER_ID=<uuid> and REGISTRATION_TOKEN in its output
```

## Quick hints script

```bash
./hack/dev-real-control-plane.sh
```

When the server stack is healthy and `../vworkspace-server` exists, the script resolves `CLUSTER_ID` from Odoo for `VWORKSPACE_CLUSTER_SLUG` (default `dev-integration`). Set `VWORKSPACE_REGISTRATION_TOKEN` before running to print a complete `register` command. Override `CONTROL_PLANE_BASE_URL` if the default host URL is wrong for your setup.

| Variable | Default | Purpose |
|----------|---------|---------|
| `CONTROL_PLANE_BASE_URL` | `http://127.0.0.1:8069` (Linux host) / `http://host.docker.internal:8069` (macOS) | Control plane base URL |
| `VWORKSPACE_SERVER_PORT` | `8069` | Odoo HTTP port in docker-compose |
| `VWORKSPACE_REGISTRATION_TOKEN` | (empty) | One-time token for printed register command |
| `VWORKSPACE_CLUSTER_ID` | (resolved from server when possible) | Server-issued UUID for `spec.clusterId` / `--cluster-id` |
| `VWORKSPACE_CLUSTER_SLUG` | `dev-integration` | Server registry slug used to look up UUID |
| `VWORKSPACE_CLUSTER_NAME` | `cluster-local` | Kubernetes `Cluster.metadata.name` / `register --cluster-name` |
| `VWORKSPACE_SERVER_ROOT` | `../vworkspace-server` | Checkout used to resolve UUID via Odoo shell |

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
export VWORKSPACE_CLUSTER_ID=<uuid-from-server-seed-or-ui>

go run ./cmd/main.go register \
  --control-plane-endpoint "${CONTROL_PLANE_BASE_URL}" \
  --token "${VWORKSPACE_REGISTRATION_TOKEN}" \
  --cluster-name cluster-local \
  --cluster-id "${VWORKSPACE_CLUSTER_ID}"
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

Not run in default CI. Requires a fresh registration token (single-use) and the server UUID for the token's cluster.

```bash
export CONTROL_PLANE_BASE_URL=http://127.0.0.1:8069
export VWORKSPACE_REGISTRATION_TOKEN=vwksp-reg-...
export VWORKSPACE_CLUSTER_ID=<server-uuid>
go test -tags=integration ./test/integration/... -run TestRealControlPlane -count=1 -v
```

## Golden path (with Platform)

End-to-end validation with a server-deployed app job is coordinated outside this repo. Two **Flux tiers** apply — see [cluster-bootstrap.md#flux-contract-only-vs-full-reconcile](../install/cluster-bootstrap.md#flux-contract-only-vs-full-reconcile):

| Step | Tier | Success signal |
|------|------|----------------|
| 1–3 below | **Contract-only** (default) | `ApplicationInstance` + `HelmRelease` exist; server instance may stay `deploying` |
| Optional 5 | **Full reconcile** | `ApplicationInstance` `Ready=True`; chart workloads running |

1. Server: `./hack/dev-integration.sh` — note `CLUSTER_ID` and `REGISTRATION_TOKEN`.
2. Operator: `./hack/dev-real-control-plane.sh` — register with UUID, enable agent on kind. For contract proof, bootstrap with `INSTALL_FLUX_CRDS=true` ([validate-helm-kind.sh](../../hack/validate-helm-kind.sh)) — **CRDs only**, not controller pods.
3. Poll jobs; confirm `ApplicationInstance` + `HelmRelease` CRs (`kubectl get applicationinstances,helmreleases -A`).
4. Confirm `POST /api/agent/events` updates visible on the server.
5. **(Optional)** Install Flux controllers for `Ready` in dev: `flux install` or the bundled chart — [helm.md#optional-flux-controllers-for-ready](../install/helm.md#optional-flux-controllers-for-ready).

Do not expect the server instance to become `ready` at step 3 unless step 5 (or the production Helm bundle) has installed `helm-controller` and `source-controller`.

Server-side seed and UUID contract: [vworkspace-server#9](https://github.com/vworkspace-io/vworkspace-server/issues/9).

## Related

- [mock-control-plane.md](mock-control-plane.md) — CI and offline dev without server.
- [IMPLEMENTATION_GUIDE.md](IMPLEMENTATION_GUIDE.md) — Phase 3 checklist.
- [pull-mode.md](../connectivity/pull-mode.md) — Pull-mode protocol and flags.
