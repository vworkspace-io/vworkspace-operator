#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

make manifests generate

if ! git diff --exit-code -- config/crd config/rbac api internal charts/vworkspace-operator/files/crds; then
  echo "Generated files are out of date. Run 'make manifests generate' and commit the result." >&2
  exit 1
fi

echo "Generated artifacts are up to date."
