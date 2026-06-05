#!/usr/bin/env bash
# Print commands and env for Pull-mode dev against a live vWorkspace Server (docker-compose).
# Does not start the server — see vworkspace-server docs/development/DEV_ENVIRONMENT.md
# and hack/dev-integration.sh for the server-side seed (CLUSTER_ID UUID + registration token).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

PORT="${VWORKSPACE_SERVER_PORT:-8069}"
CLUSTER_SLUG="${VWORKSPACE_CLUSTER_SLUG:-dev-integration}"
CLUSTER_CR_NAME="${VWORKSPACE_CLUSTER_NAME:-cluster-local}"
NAMESPACE="${VWORKSPACE_NAMESPACE:-vworkspace-system}"
REG_TOKEN="${VWORKSPACE_REGISTRATION_TOKEN:-}"
SERVER_ROOT="${VWORKSPACE_SERVER_ROOT:-$ROOT/../vworkspace-server}"
COMPOSE="${COMPOSE:-docker compose}"
ODOO_SVC="${ODOO_SVC:-odoo}"
DB_NAME="${ODOO_DB_NAME:-vworkspace}"

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

# shellcheck source=lib/kind-gateway.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/kind-gateway.sh"

# Resolve server-issued cluster UUID (spec.clusterId / register --cluster-id).
# Slug (e.g. dev-integration) is NOT valid for POST /api/agent/register when clusterId is sent.
resolve_cluster_uuid() {
  if [[ -n "${VWORKSPACE_CLUSTER_ID:-}" ]]; then
    echo "${VWORKSPACE_CLUSTER_ID}"
    return 0
  fi
  if [[ ! -d "${SERVER_ROOT}" ]]; then
    return 1
  fi
  if ! curl -fsS "http://127.0.0.1:${PORT}/web/health" >/dev/null 2>&1; then
    return 1
  fi
  local out
  if ! out="$(
    cd "${SERVER_ROOT}" && ${COMPOSE} run --rm -T "${ODOO_SVC}" odoo shell -d "${DB_NAME}" --no-http 2>/dev/null <<PY
cluster = env["vws.cluster"].search([("slug", "=", ${CLUSTER_SLUG@Q})], limit=1)
if not cluster:
    raise SystemExit("no cluster with slug ${CLUSTER_SLUG@Q}")
print(cluster.cluster_id)
PY
  )"; then
    return 1
  fi
  out="$(echo "${out}" | tr -d '[:space:]')"
  if [[ -z "${out}" ]]; then
    return 1
  fi
  echo "${out}"
}

CLUSTER_UUID=""
if CLUSTER_UUID="$(resolve_cluster_uuid)"; then
  :
else
  CLUSTER_UUID=""
fi

register_cluster_id_flag() {
  if [[ -n "${CLUSTER_UUID}" ]]; then
    echo "       --cluster-id ${CLUSTER_UUID} \\"
  else
    echo "       # --cluster-id <server-uuid>   # required when token is bound to a cluster; see VWORKSPACE_CLUSTER_ID"
  fi
}

cluster_cr_cluster_id_line() {
  if [[ -n "${CLUSTER_UUID}" ]]; then
    echo "      clusterId: ${CLUSTER_UUID}"
  else
    echo "      clusterId: <server-issued-uuid>   # NOT the slug (${CLUSTER_SLUG})"
  fi
}

echo "=== vWorkspace Server (real control plane) — dev hints ==="
echo ""
echo "1. Start vWorkspace Server (separate clone):"
echo "     cd ../vworkspace-server   # or your checkout path"
echo "     cp .env.example .env && make up && make init-db"
echo "     ./hack/dev-integration.sh   # prints CLUSTER_ID=<uuid> and REGISTRATION_TOKEN"
echo "     Open http://127.0.0.1:${PORT} — or issue a token in Cluster Registry."
echo "     Guide: https://github.com/vworkspace-io/vworkspace-server/blob/main/docs/development/DEV_ENVIRONMENT.md"
echo ""
echo "   clusterId vs slug:"
echo "     - Server slug (registry name): ${CLUSTER_SLUG}"
echo "     - Agent API clusterId (UUID):  ${CLUSTER_UUID:-<set VWORKSPACE_CLUSTER_ID or run server dev-integration.sh>}"
echo "     - Kubernetes Cluster.metadata.name: ${CLUSTER_CR_NAME}"
echo ""
HOST_URL="http://127.0.0.1:${PORT}"
KIND_URL="$(kind_control_plane_url "${PORT}" 2>/dev/null || echo "http://<kind-host-gateway>:${PORT}")"
echo "2. Control plane base URL:"
echo "     Host-run operator / register (always):  ${HOST_URL}"
echo "       export CONTROL_PLANE_BASE_URL=${HOST_URL}"
echo "     kind pods on Linux (in-cluster only):   ${KIND_URL}"
echo "     (Do not use the kind gateway URL for host-run go run — use 127.0.0.1.)"
echo ""
echo "3. Bootstrap order (host-run — registration before agent secret exists):"
echo "     kubectl create namespace ${NAMESPACE} --dry-run=client -o yaml | kubectl apply -f -"
echo "     # Terminal A: manager WITHOUT agent (Cluster controller registers the token):"
echo "     go run ./cmd/main.go"
echo "     # Terminal B: apply Cluster CR or register (while Terminal A is running):"
echo "     kubectl apply -f cluster.yaml   # spec.controlPlaneBaseUrl must be ${HOST_URL}"
echo "     # After Connected=True and secret ${NAMESPACE}/vworkspace-agent-credentials exists:"
echo "     # Stop Terminal A; restart WITH --agent-enabled=true (see step 6)."
echo ""
echo "4. Register or apply Cluster CR (Cluster controller must be running — step 3):"
if [[ -n "${REG_TOKEN}" ]]; then
  echo "     export VWORKSPACE_CLUSTER_ID=${CLUSTER_UUID:-<uuid-from-server-seed>}"
  echo "     go run ./cmd/main.go register \\"
  echo "       --control-plane-endpoint \"\${CONTROL_PLANE_BASE_URL}\" \\"
  echo "       --token \"${REG_TOKEN}\" \\"
  echo "       --cluster-name ${CLUSTER_CR_NAME} \\"
  register_cluster_id_flag
else
  echo "     export VWORKSPACE_REGISTRATION_TOKEN=<one-time-token-from-server>"
  echo "     export VWORKSPACE_CLUSTER_ID=<uuid-from-server-dev-integration-or-ui>"
  echo "     go run ./cmd/main.go register \\"
  echo "       --control-plane-endpoint \"\${CONTROL_PLANE_BASE_URL}\" \\"
  echo "       --token \"\${VWORKSPACE_REGISTRATION_TOKEN}\" \\"
  echo "       --cluster-name ${CLUSTER_CR_NAME} \\"
  register_cluster_id_flag
fi
echo ""
echo "     Or apply Cluster CR directly (spec.clusterId must be the server UUID):"
echo "     cat <<'YAML' | kubectl apply -f -"
echo "     apiVersion: ops.vworkspace.io/v1alpha1"
echo "     kind: Cluster"
echo "     metadata:"
echo "       name: ${CLUSTER_CR_NAME}"
echo "     spec:"
cluster_cr_cluster_id_line
echo "       controlPlaneBaseUrl: \"\${CONTROL_PLANE_BASE_URL}\""
echo "       registrationToken: \"\${VWORKSPACE_REGISTRATION_TOKEN}\""
echo "     YAML"
echo ""
echo "     In-cluster (after helm install):"
echo "     kubectl -n ${NAMESPACE} exec deploy/vworkspace-operator -- \\"
echo "       /manager register \\"
echo "         --control-plane-endpoint \"\${CONTROL_PLANE_BASE_URL}\" \\"
echo "         --token \"\${VWORKSPACE_REGISTRATION_TOKEN}\" \\"
echo "         --cluster-name ${CLUSTER_CR_NAME} \\"
register_cluster_id_flag
echo ""
echo "5. No ApplicationInstance CRs after register?"
echo "     Server job may be stuck delivered/succeeded (kind recreated). Re-enqueue:"
echo "       cd ../vworkspace-server && ./hack/redeploy-dev-instance.sh"
echo ""
echo "6. Run operator with Pull-mode agent (only AFTER registration + credentials secret):"
echo "     make install   # if CRDs not on cluster"
echo "     export CONTROL_PLANE_BASE_URL=${HOST_URL}"
echo "     go run ./cmd/main.go \\"
echo "       --agent-enabled=true \\"
echo "       --control-plane-base-url=\"\${CONTROL_PLANE_BASE_URL}\" \\"
echo "       --agent-credentials-secret=vworkspace-agent-credentials \\"
echo "       --agent-credentials-namespace=${NAMESPACE}"
echo ""
echo "     Helm (in-cluster):"
echo "     helm upgrade vworkspace-operator ./charts/vworkspace-operator \\"
echo "       -n ${NAMESPACE} --reuse-values \\"
echo "       --set agent.enabled=true \\"
echo "       --set agent.controlPlaneBaseUrl=\"\${CONTROL_PLANE_BASE_URL}\""
echo ""
echo "7. Integration test against live server (optional, not CI by default):"
echo "     export CONTROL_PLANE_BASE_URL=\${CONTROL_PLANE_BASE_URL}"
echo "     export VWORKSPACE_REGISTRATION_TOKEN=<fresh-one-time-token>"
echo "     export VWORKSPACE_CLUSTER_ID=<server-uuid>"
echo "     go test -tags=integration ./test/integration/... -run TestRealControlPlane -count=1 -v"
echo ""
echo "Docs: docs/development/real-control-plane.md"
echo "Server companion: https://github.com/vworkspace-io/vworkspace-server/issues/9"
