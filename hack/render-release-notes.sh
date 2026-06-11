#!/usr/bin/env bash
# Render customer-facing GitHub Release notes for a version.
#
# Reads the matching section from CHANGELOG.md and wraps it with install commands,
# asset descriptions, and doc links (NetBird-style: readable without cloning the repo).
#
# Usage:
#   VERSION=0.0.7 ./hack/render-release-notes.sh
#   ./hack/render-release-notes.sh 0.0.7
#   ./hack/render-release-notes.sh 0.0.7 --print-title
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

GITHUB_REPOSITORY="${GITHUB_REPOSITORY:-vworkspace-io/vworkspace-operator}"
PRINT_TITLE=false

resolve_version() {
  local arg="${1:-}"
  if [[ -n "${VERSION:-}" ]]; then
    printf '%s\n' "${VERSION#v}"
    return
  fi
  if [[ -n "${arg}" && "${arg}" != --* ]]; then
    printf '%s\n' "${arg#v}"
    return
  fi
  echo "error: set VERSION or pass version as first argument" >&2
  exit 1
}

for arg in "$@"; do
  case "${arg}" in
    --print-title) PRINT_TITLE=true ;;
  esac
done

VERSION="$(resolve_version "${1:-}")"
TAG="v${VERSION}"
CHART_TGZ="vworkspace-operator-${VERSION}.tgz"
BASE="https://github.com/${GITHUB_REPOSITORY}"
DL="${BASE}/releases/download/${TAG}"
DOCS="${BASE}/blob/${TAG}/docs/install/helm.md#install-from-github-release"
CHANGELOG_LINK="${BASE}/blob/${TAG}/CHANGELOG.md"
IMAGE="docker.io/vworkspace/vworkspace-operator:${TAG}"

extract_changelog_body() {
  local ver="$1"
  awk -v ver="${ver}" '
    $0 ~ "^## \\[" ver "\\]" { found=1; next }
    found && $0 ~ "^## \\[" { exit }
    found { print }
  ' CHANGELOG.md
}

changelog_body="$(extract_changelog_body "${VERSION}")"

summary=""
changes=""
if [[ -n "${changelog_body}" ]]; then
  summary="$(printf '%s\n' "${changelog_body}" | awk '
    /^### / { exit }
    /^[[:space:]]*$/ { next }
    { print; exit }
  ')"
  changes="$(printf '%s\n' "${changelog_body}" | awk '
    BEGIN { in_section=0 }
    /^### / { in_section=1 }
    in_section { print }
  ')"
fi

if [[ -z "${summary}" ]]; then
  summary="Helm chart and kubectl manifests for \`${TAG}\`. Container image: \`${IMAGE}\`."
fi

release_title() {
  local subtitle
  if [[ "${summary}" == *" — "* ]]; then
    subtitle="${summary%% — *}"
  else
    subtitle="$(printf '%s' "${summary}" | cut -c1-72)"
  fi
  subtitle="${subtitle%.}"
  printf '%s — %s\n' "${TAG}" "${subtitle}"
}

if [[ "${PRINT_TITLE}" == "true" ]]; then
  release_title
  exit 0
fi

cat <<EOF
${summary}

Container image: \`${IMAGE}\`

## Install

**Helm** (operator + CRDs + RBAC):

\`\`\`bash
helm upgrade --install vworkspace-operator \\
  ${DL}/${CHART_TGZ} \\
  --version ${VERSION} \\
  -n vworkspace-system \\
  --create-namespace
\`\`\`

**kubectl** (no Helm — apply CRDs first):

\`\`\`bash
kubectl apply -f ${DL}/crds.yaml
kubectl apply -f ${DL}/operator.yaml
kubectl -n vworkspace-system wait --for=condition=Available \\
  deployment/vworkspace-operator --timeout=300s
\`\`\`

Install guide: [docs/install/helm.md](${DOCS})

## Release assets

| File | Purpose |
|------|---------|
| \`${CHART_TGZ}\` | Helm chart (operator, CRDs, RBAC) |
| \`crds.yaml\` | CRDs only — for kubectl or GitOps bootstrap |
| \`operator.yaml\` | Namespace + operator Deployment (CRDs excluded) |
| \`SHA256SUMS\` | Checksums for the files above |

## What's changed

EOF

if [[ -n "${changes}" ]]; then
  printf '%s\n' "${changes}"
else
  cat <<EOF
See the [full changelog](${CHANGELOG_LINK}) for this release.
EOF
fi

printf '\n**Full changelog:** [%s](%s)\n' "CHANGELOG.md" "${CHANGELOG_LINK}"
