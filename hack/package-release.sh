#!/usr/bin/env bash
# Package the Helm chart and kubectl install manifests for a tagged release.
# Run locally: VERSION=0.0.6 ./hack/package-release.sh
# CI sets VERSION from the git tag (v0.0.6 -> 0.0.6).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

CHART_SRC="${CHART_SRC:-charts/vworkspace-operator}"
OUTPUT_DIR="${OUTPUT_DIR:-dist/release}"
NAMESPACE="${NAMESPACE:-vworkspace-system}"
RELEASE_NAME="${RELEASE_NAME:-vworkspace-operator}"

resolve_version() {
  if [[ -n "${VERSION:-}" ]]; then
    printf '%s\n' "${VERSION#v}"
    return
  fi
  local tag
  tag="$(git describe --exact-match --tags HEAD 2>/dev/null || true)"
  if [[ -n "${tag}" ]]; then
    printf '%s\n' "${tag#v}"
    return
  fi
  echo "error: set VERSION (e.g. 0.0.6) or run from an annotated release tag" >&2
  exit 1
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: required command not found: $1" >&2
    exit 1
  fi
}

require_cmd helm
require_cmd git

VERSION="$(resolve_version)"
IMAGE_TAG="v${VERSION}"
CHART_VERSION="${VERSION}"
APP_VERSION="${VERSION}"
CHART_NAME="vworkspace-operator"
CHART_TGZ="${CHART_NAME}-${CHART_VERSION}.tgz"

log() {
  echo "[package-release] $*"
}

STAGING="$(mktemp -d)"
cleanup() {
  rm -rf "${STAGING}"
}
trap cleanup EXIT

log "packaging ${CHART_NAME} chart version ${CHART_VERSION} (image tag ${IMAGE_TAG})"
mkdir -p "${OUTPUT_DIR}"
cp -a "${CHART_SRC}/." "${STAGING}/"

# Patch chart metadata for this release without modifying the working tree.
sed -i \
  -e "s/^version:.*/version: ${CHART_VERSION}/" \
  -e "s/^appVersion:.*/appVersion: \"${APP_VERSION}\"/" \
  "${STAGING}/Chart.yaml"

helm dependency update "${STAGING}" >/dev/null
helm lint "${STAGING}" --set "image.tag=${IMAGE_TAG}"

helm package "${STAGING}" \
  --version "${CHART_VERSION}" \
  --app-version "${APP_VERSION}" \
  --destination "${OUTPUT_DIR}"

log "rendering kubectl manifests"
CRDS_OUT="${OUTPUT_DIR}/crds.yaml"
OPERATOR_OUT="${OUTPUT_DIR}/operator.yaml"

{
  shopt -s nullglob
  first=true
  for crd in "${STAGING}/files/crds/"*.yaml; do
    if [[ "${first}" == "true" ]]; then
      first=false
    else
      printf '\n---\n'
    fi
    cat "${crd}"
  done
} > "${CRDS_OUT}"

{
  cat <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: ${NAMESPACE}
  labels:
    app.kubernetes.io/name: vworkspace-operator
    app.kubernetes.io/managed-by: kubectl
EOF
  printf '\n---\n'
  helm template "${RELEASE_NAME}" "${STAGING}" \
    --namespace "${NAMESPACE}" \
    --set "crds.install=false" \
    --set "image.tag=${IMAGE_TAG}"
} > "${OPERATOR_OUT}"

(
  cd "${OUTPUT_DIR}"
  sha256sum "${CHART_TGZ}" crds.yaml operator.yaml > SHA256SUMS
)

log "artifacts in ${OUTPUT_DIR}:"
ls -la "${OUTPUT_DIR}/${CHART_TGZ}" "${CRDS_OUT}" "${OPERATOR_OUT}" "${OUTPUT_DIR}/SHA256SUMS"
