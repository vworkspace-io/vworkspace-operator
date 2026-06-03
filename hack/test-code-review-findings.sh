#!/usr/bin/env bash
# Unit tests for PR review findings severity parsing.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${ROOT}"

python3 -m unittest -v test_code_review_findings.py
