# kind-gateway.sh — resolve control plane URL reachable from kind pods on Linux.
# Source from operator/server hack scripts; do not execute directly.

# kind_host_gateway prints the Docker bridge gateway for the kind network.
# Prefers IPv4 (IPAM index 1 on dual-stack); falls back to index 0 (often IPv6).
kind_host_gateway() {
  if ! command -v docker >/dev/null 2>&1; then
    return 1
  fi
  local net="${KIND_DOCKER_NETWORK:-kind}"
  local gw
  gw="$(docker network inspect "${net}" -f '{{(index .IPAM.Config 1).Gateway}}' 2>/dev/null || true)"
  if [[ -z "${gw}" || "${gw}" == "<no value>" ]]; then
    gw="$(docker network inspect "${net}" -f '{{(index .IPAM.Config 0).Gateway}}' 2>/dev/null || true)"
  fi
  if [[ -z "${gw}" || "${gw}" == "<no value>" ]]; then
    return 1
  fi
  echo "${gw}"
}

# kind_control_plane_url prints http://<gateway>:<port> with IPv6 bracketed for curl.
kind_control_plane_url() {
  local port="${1:-8069}"
  local gateway
  gateway="$(kind_host_gateway || true)"
  if [[ -z "${gateway}" ]]; then
    echo "http://<kind-host-gateway>:${port}"
    return 1
  fi
  if [[ "${gateway}" == *:* ]]; then
    echo "http://[${gateway}]:${port}"
  else
    echo "http://${gateway}:${port}"
  fi
}
