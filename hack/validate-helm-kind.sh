#!/usr/bin/env bash
# Validate the in-repo Helm chart on a kind cluster.
# Requires: kind, helm, kubectl, docker (unless USE_PUBLISHED_IMAGE=1).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

KIND_CLUSTER="${KIND_CLUSTER:-vworkspace-operator-helm-validate}"
NAMESPACE="${NAMESPACE:-vworkspace-system}"
RELEASE="${RELEASE:-vworkspace-operator}"
CHART="${CHART:-./charts/vworkspace-operator}"
INSTALL_FLUX_CRDS="${INSTALL_FLUX_CRDS:-false}"
FLUX_VERSION="${FLUX_VERSION:-v2.4.0}"
DELETE_CLUSTER="${DELETE_CLUSTER:-true}"
USE_PUBLISHED_IMAGE="${USE_PUBLISHED_IMAGE:-0}"

CREATED_CLUSTER=false
HELM_INSTALLED=false
FLUX_CRDS_INSTALLED=false

log() {
  echo "[validate-helm-kind] $*"
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: required command not found: $1" >&2
    exit 1
  fi
}

cleanup() {
  local code=$?
  if [[ "${HELM_INSTALLED}" == "true" ]]; then
    log "uninstalling Helm release ${RELEASE}"
    helm uninstall "${RELEASE}" -n "${NAMESPACE}" --wait --timeout 120s 2>/dev/null || true
  fi
  if [[ "${FLUX_CRDS_INSTALLED}" == "true" ]]; then
    log "removing Flux CRDs installed for validation"
    kubectl delete -f "https://github.com/fluxcd/flux2/releases/download/${FLUX_VERSION}/crds/core.yaml" \
      --ignore-not-found --wait=false 2>/dev/null || true
  fi
  if [[ "${CREATED_CLUSTER}" == "true" && "${DELETE_CLUSTER}" == "true" ]]; then
    log "deleting kind cluster ${KIND_CLUSTER}"
    kind delete cluster --name "${KIND_CLUSTER}" 2>/dev/null || true
  fi
  if [[ "${code}" -ne 0 ]]; then
    log "validation failed (exit ${code})"
  fi
}
trap cleanup EXIT

require_cmd kind
require_cmd helm
require_cmd kubectl

if [[ "${USE_PUBLISHED_IMAGE}" != "1" ]]; then
  require_cmd docker
fi

if kind get clusters 2>/dev/null | grep -qx "${KIND_CLUSTER}"; then
  log "using existing kind cluster ${KIND_CLUSTER}"
else
  log "creating kind cluster ${KIND_CLUSTER}"
  kind create cluster --name "${KIND_CLUSTER}"
  CREATED_CLUSTER=true
fi

IMAGE_REPO="vworkspace/vworkspace-operator"
if [[ "${USE_PUBLISHED_IMAGE}" == "1" ]]; then
  IMAGE_TAG="${IMAGE_TAG:-latest}"
  log "using published image ${IMAGE_REPO}:${IMAGE_TAG}"
else
  IMAGE_TAG="${IMAGE_TAG:-helm-validate}"
  IMG="${IMAGE_REPO}:${IMAGE_TAG}"
  log "building and loading local image ${IMG}"
  make docker-build "IMG=${IMG}"
  kind load docker-image "${IMG}" --name "${KIND_CLUSTER}"
fi

log "installing Helm chart from ${CHART}"
helm upgrade --install "${RELEASE}" "${CHART}" \
  -n "${NAMESPACE}" \
  --create-namespace \
  --set "image.repository=${IMAGE_REPO}" \
  --set "image.tag=${IMAGE_TAG}" \
  --wait \
  --timeout 3m
HELM_INSTALLED=true

log "waiting for operator deployment in ${NAMESPACE}"
kubectl -n "${NAMESPACE}" wait --for=condition=Available \
  deployment -l "app.kubernetes.io/instance=${RELEASE}" --timeout=180s

log "checking vWorkspace CRDs"
kubectl get crd applicationinstances.apps.vworkspace.io clusters.ops.vworkspace.io operations.ops.vworkspace.io

if [[ "${INSTALL_FLUX_CRDS}" == "true" ]]; then
  log "installing minimal Flux CRDs (${FLUX_VERSION}) for HelmRelease support"
  kubectl apply -f "https://github.com/fluxcd/flux2/releases/download/${FLUX_VERSION}/crds/core.yaml"
  FLUX_CRDS_INSTALLED=true
  kubectl get crd helmreleases.helm.toolkit.fluxcd.io helmcharts.source.toolkit.fluxcd.io
fi

log "Helm kind validation succeeded"
