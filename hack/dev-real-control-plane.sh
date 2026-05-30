#!/usr/bin/env bash
# Print commands and env for Pull-mode dev against a live vWorkspace Server (docker-compose).
# Does not start the server — see vworkspace-server docs/development/DEV_ENVIRONMENT.md.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

PORT="${VWORKSPACE_SERVER_PORT:-8069}"
CLUSTER_ID="${VWORKSPACE_CLUSTER_ID:-cluster-dev-1}"
CLUSTER_NAME="${VWORKSPACE_CLUSTER_NAME:-${CLUSTER_ID}}"
NAMESPACE="${VWORKSPACE_NAMESPACE:-vworkspace-system}"
REG_TOKEN="${VWORKSPACE_REGISTRATION_TOKEN:-}"

# Default control plane URL: reachable from the machine running this script.
# For operator pods inside kind on Linux, override with the host gateway (see below).
default_control_plane_url() {
  if [[ -n "${CONTROL_PLANE_BASE_URL:-}" ]]; then
    echo "${CONTROL_PLANE_BASE_URL}"
    return
  fi
  if [[ "$(uname -s)" == "Darwin" ]]; then
    echo "http://host.docker.internal:${PORT}"
    return
  fi
  # Linux: server on the host, operator on host → localhost
  echo "http://127.0.0.1:${PORT}"
}

BASE_URL="$(default_control_plane_url)"
BASE_URL="${BASE_URL%/}"

kind_host_gateway() {
  if ! command -v docker >/dev/null 2>&1; then
    return 1
  fi
  local net="${KIND_DOCKER_NETWORK:-kind}"
  docker network inspect "${net}" -f '{{(index .IPAM.Config 0).Gateway}}' 2>/dev/null || true
}

echo "=== vWorkspace Server (real control plane) — dev hints ==="
echo ""
echo "1. Start vWorkspace Server (separate clone):"
echo "     cd ../vworkspace-server   # or your checkout path"
echo "     cp .env.example .env && make up && make init-db"
echo "     Open http://127.0.0.1:${PORT} — issue a registration token in Cluster Registry."
echo "     Guide: https://github.com/vworkspace-io/vworkspace-server/blob/main/docs/development/DEV_ENVIRONMENT.md"
echo ""
echo "2. Control plane base URL (this machine / host-run operator):"
echo "     export CONTROL_PLANE_BASE_URL=${BASE_URL}"
echo ""
echo "3. kind on Linux — pods must reach the host, not 127.0.0.1 inside the pod:"
GATEWAY="$(kind_host_gateway || true)"
if [[ -n "${GATEWAY}" ]]; then
  echo "     Host gateway on docker network 'kind': ${GATEWAY}"
  echo "     export CONTROL_PLANE_BASE_URL=http://${GATEWAY}:${PORT}"
  echo "     (macOS kind often works with host.docker.internal; Linux usually needs the gateway IP.)"
else
  echo "     export CONTROL_PLANE_BASE_URL=http://<host-gateway-ip>:${PORT}"
  echo "     Find gateway: docker network inspect kind -f '{{(index .IPAM.Config 0).Gateway}}'"
  echo "     Or add extra_hosts host.docker.internal:host-gateway in kind cluster config."
fi
echo ""
echo "4. Register (writes Cluster CR + credentials Secret in current kube context):"
if [[ -n "${REG_TOKEN}" ]]; then
  echo "     go run ./cmd/main.go register \\"
  echo "       --control-plane-endpoint \"\${CONTROL_PLANE_BASE_URL}\" \\"
  echo "       --token \"${REG_TOKEN}\" \\"
  echo "       --cluster-name ${CLUSTER_NAME} \\"
  echo "       --namespace ${NAMESPACE}"
else
  echo "     export VWORKSPACE_REGISTRATION_TOKEN=<one-time-token-from-server>"
  echo "     go run ./cmd/main.go register \\"
  echo "       --control-plane-endpoint \"\${CONTROL_PLANE_BASE_URL}\" \\"
  echo "       --token \"\${VWORKSPACE_REGISTRATION_TOKEN}\" \\"
  echo "       --cluster-name ${CLUSTER_NAME} \\"
  echo "       --namespace ${NAMESPACE}"
fi
echo ""
echo "     In-cluster (after helm install):"
echo "     kubectl -n ${NAMESPACE} exec deploy/vworkspace-operator -- \\"
echo "       /manager register \\"
echo "         --control-plane-endpoint \"\${CONTROL_PLANE_BASE_URL}\" \\"
echo "         --token \"\${VWORKSPACE_REGISTRATION_TOKEN}\" \\"
echo "         --cluster-name ${CLUSTER_NAME}"
echo ""
echo "5. Run operator with Pull-mode agent (local make run):"
echo "     make install   # if CRDs not on cluster"
echo "     make run -- \\"
echo "       --agent-enabled=true \\"
echo "       --control-plane-base-url=\"\${CONTROL_PLANE_BASE_URL}\" \\"
echo "       --agent-credentials-secret=vworkspace-agent-credentials"
echo ""
echo "     Helm (in-cluster):"
echo "     helm upgrade vworkspace-operator ./charts/vworkspace-operator \\"
echo "       -n ${NAMESPACE} --reuse-values \\"
echo "       --set agent.enabled=true \\"
echo "       --set agent.controlPlaneBaseUrl=\"\${CONTROL_PLANE_BASE_URL}\""
echo ""
echo "6. Integration test against live server (optional, not CI by default):"
echo "     export CONTROL_PLANE_BASE_URL=\${CONTROL_PLANE_BASE_URL}"
echo "     export VWORKSPACE_REGISTRATION_TOKEN=<fresh-one-time-token>"
echo "     go test -tags=integration ./test/integration/... -run TestRealControlPlane -count=1 -v"
echo ""
echo "Docs: docs/development/real-control-plane.md"
