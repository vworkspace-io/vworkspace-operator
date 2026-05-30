#!/usr/bin/env bash
# Start mock control plane and print environment for running the operator with Pull-mode enabled.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

ADDR="${MOCK_CONTROL_PLANE_ADDR:-:8080}"
REG_TOKEN="${MOCK_CONTROL_PLANE_REGISTRATION_TOKEN:-dev-registration-token}"
CLUSTER_ID="${MOCK_CONTROL_PLANE_CLUSTER_ID:-cluster-dev-1}"

echo "Starting mock control plane on ${ADDR} (cluster=${CLUSTER_ID})..."
go run ./test/mockcontrolplane/cmd/mockcontrolplane \
  -addr "${ADDR}" \
  -registration-token "${REG_TOKEN}" \
  -cluster-id "${CLUSTER_ID}" &
MOCK_PID=$!
trap 'kill "${MOCK_PID}" 2>/dev/null || true' EXIT

BASE_URL="http://127.0.0.1${ADDR#:}"
if [[ "${ADDR}" != :* ]]; then
  BASE_URL="http://${ADDR}"
fi

sleep 0.5
echo ""
echo "Mock control plane listening at ${BASE_URL}"
echo ""
echo "Register and persist credentials (writes Secret in current kube context):"
echo "  go run ./cmd/main.go register \\"
echo "    --control-plane-endpoint ${BASE_URL} \\"
echo "    --registration-token ${REG_TOKEN} \\"
echo "    --cluster-id ${CLUSTER_ID}"
echo ""
echo "Or run the operator with agent flags after registration:"
echo "  export CONTROL_PLANE_BASE_URL=${BASE_URL}"
echo "  export VWORKSPACE_CLUSTER_ID=${CLUSTER_ID}"
echo "  export VWORKSPACE_AGENT_TOKEN=<token from register>"
echo "  make run -- --agent-enabled=true --control-plane-base-url=\${CONTROL_PLANE_BASE_URL}"
echo ""
echo "Integration tests (no cluster required):"
echo "  go test ./test/integration/... -count=1"
echo ""
wait "${MOCK_PID}"
