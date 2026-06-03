#!/usr/bin/env bash
# Run Cursor Agent on a PR diff and post/update a single review comment on GitHub.
set -euo pipefail

REVIEW_MARKER='<!-- vworkspace-agent-review:canonical:v1 -->'
MODEL="${CURSOR_REVIEW_MODEL:-composer-2.5}"
DIFF_MAX_BYTES=200000
HISTORY_MAX_BYTES="${REVIEW_HISTORY_MAX_BYTES:-12000}"
REVIEW_DIFF_TRUNCATED=0
REVIEW_FAILED=0
# Comma-separated severities in ## Findings that fail the workflow (set to "none" to disable).
REVIEW_FAIL_SEVERITIES="${REVIEW_FAIL_SEVERITIES:-critical,major,minor}"

die() {
  echo "::error::$*" >&2
  exit 1
}

require_env() {
  local name="$1"
  if [ -z "${!name:-}" ]; then
    die "${name} is not set"
  fi
}

ensure_cursor_agent() {
  if command -v cursor-agent >/dev/null 2>&1; then
    return 0
  fi
  if ! command -v curl >/dev/null 2>&1; then
    die "curl is required to install cursor-agent"
  fi
  echo "Installing Cursor CLI..."
  curl -fsS https://cursor.com/install | bash
  export PATH="${HOME}/.local/bin:${PATH}"
  command -v cursor-agent >/dev/null 2>&1 || die "cursor-agent not found after install"
}

ensure_gh() {
  command -v gh >/dev/null 2>&1 || die "gh CLI is required on the runner"
  gh auth status >/dev/null 2>&1 || true
}

write_diff() {
  local pr_number="$1"
  local diff_file="$2"
  gh pr diff "${pr_number}" >"${diff_file}"
  local lines
  lines=$(wc -l <"${diff_file}" | tr -d ' ')
  echo "PR #${pr_number} diff: ${lines} lines"
  if [ "${lines}" -eq 0 ]; then
    die "PR diff is empty; nothing to review"
  fi
}

validate_review_output() {
  local out_file="$1"
  local reason=""
  if [ ! -s "${out_file}" ]; then
    reason="empty"
  else
    local first_line
    first_line=$(head -n1 "${out_file}" | tr -d '\r')
    if [[ "${first_line}" =~ ^@[~/] ]] || [[ "${first_line}" =~ \.wrapped$ ]]; then
      reason="file reference: ${first_line}"
    else
      local bytes
      bytes=$(wc -c <"${out_file}" | tr -d ' ')
      if [ "${bytes}" -lt 80 ]; then
        reason="too short (${bytes} bytes)"
      elif ! grep -qE '^##[[:space:]]+(Summary|Findings)' "${out_file}"; then
        reason="missing ## Summary or ## Findings"
      fi
    fi
  fi
  if [ -n "${reason}" ]; then
    echo "::warning::Review output invalid: ${reason}" >&2
    return 1
  fi
  return 0
}

normalize_review_output() {
  local out_file="$1"
  local tmp="${out_file}.norm"
  awk '
    BEGIN { in_fence=0; started=0 }
    /^```/ {
      if (!started) { started=1; next }
      in_fence = !in_fence
      next
    }
    { if (!in_fence || started) print }
  ' "${out_file}" >"${tmp}"
  mv "${tmp}" "${out_file}"
}

run_review() {
  local diff_file="$1"
  local out_file="$2"
  local prompt
  prompt=$(cat <<'EOF'
You are reviewing a pull request for vworkspace-operator (Kubebuilder / controller-runtime Go operator).

The unified diff is in PR_DIFF.txt in the workspace (may be truncated). Read **only** PR_DIFF.txt unless you must interpret a symbol named in the diff—do not read other workspace files. Do not suggest edits outside the changed lines unless a change clearly breaks callers elsewhere.

Important:
- Review the code as it exists in this diff now. Do not report issues that the diff already fixes.
- Each finding must cite evidence from the diff (a symbol, hunk, or behavior you can point to). If you are unsure an issue still exists, omit it.
- Prefer fewer, high-confidence findings over speculative ones. False positives waste author time.
- For PRs that only change `hack/` CI scripts, `.github/workflows`, or code-review docs: do not report major/minor findings about markdown parsing heuristics unless the diff adds a failing test that proves a false positive or false negative.
- Do not output file paths with a leading @ (that is not valid review text). Write markdown only.

Focus on:
- Correctness and regressions in controllers, reconcilers, and admission webhooks under internal/
- CRD API changes (api/ops/v1alpha1, api/apps/v1alpha1): backward compatibility, validation, defaulting, status subresources
- Operation templates, engines, preconditions, and alignment with docs under docs/operations/
- RBAC (config/rbac), security (privilege escalation, unsafe client calls, secrets in manifests)
- Tests: envtest/webhook/unit coverage for new behavior or risky paths
- Compatibility with vworkspace-server pull-mode job protocol and Operation/ApplicationInstance contracts where relevant

Skip nitpicks already enforced by golangci-lint and CI (see .golangci.yaml, make lint).

Output format (markdown only; no outer code fence):

## Summary
One short paragraph on what this PR does and overall risk.

## Findings
For each remaining issue, use this structure (repeat per issue):

### [severity] Short title
- **Severity:** critical | major | minor | nit
- **File:** `path/to/file.go`
- **Evidence:** quote or paraphrase what in the diff shows the problem
- **Issue:** one sentence
- **Suggested fix:** one concrete sentence

If there are no actionable issues in the current diff, write exactly one bullet: _No actionable findings._

## Test plan
- Bullet list of what the author should verify manually.

Be concise.
EOF
)

  local diff_bytes
  diff_bytes=$(wc -c <"${diff_file}" | tr -d ' ')
  if [ "${diff_bytes}" -gt "${DIFF_MAX_BYTES}" ]; then
    REVIEW_DIFF_TRUNCATED=1
    head -c "${DIFF_MAX_BYTES}" "${diff_file}" >"${GITHUB_WORKSPACE}/PR_DIFF.txt"
    {
      echo ""
      echo "(Diff truncated to ${DIFF_MAX_BYTES} characters for review.)"
    } >>"${GITHUB_WORKSPACE}/PR_DIFF.txt"
  else
    cp "${diff_file}" "${GITHUB_WORKSPACE}/PR_DIFF.txt"
  fi

  cursor-agent \
    --api-key "${CURSOR_API_KEY}" \
    --print \
    --output-format text \
    --trust \
    --mode ask \
    --model "${MODEL}" \
    --workspace "${GITHUB_WORKSPACE}" \
    "${prompt}" >"${out_file}" 2>&1 || {
      local exit_code=$?
      if [ ! -s "${out_file}" ]; then
        echo "Cursor agent failed (exit ${exit_code})" >"${out_file}"
      fi
      return "${exit_code}"
    }
}

review_commit_short() {
  if [ -n "${REVIEW_COMMIT_SHA:-}" ]; then
    echo "${REVIEW_COMMIT_SHA:0:7}"
    return 0
  fi
  if git -C "${GITHUB_WORKSPACE}" rev-parse --short=7 HEAD 2>/dev/null; then
    return 0
  fi
  echo "unknown"
}

extract_commit_from_body() {
  local body="$1"
  if [[ "${body}" =~ \*\*Commit\*\*[[:space:]]*\|[[:space:]]*\`([0-9a-f]{7,40})\` ]]; then
    echo "${BASH_REMATCH[1]:0:7}"
    return 0
  fi
  if [[ "${body}" =~ \`([0-9a-f]{7,40})\` ]]; then
    echo "${BASH_REMATCH[1]:0:7}"
  fi
}

strip_body_for_history() {
  local body="$1"
  HISTORY_MAX_BYTES="${HISTORY_MAX_BYTES}" python3 -c '
import os
import sys

body = sys.stdin.read()
max_b = int(os.environ.get("HISTORY_MAX_BYTES", "12000"))
start = body.find("## Summary")
if start < 0:
    sys.exit(0)
text = body[start:]
while text.lstrip().startswith("<details>"):
    end = text.find("</details>")
    if end < 0:
        break
    text = text[end + len("</details>") :].lstrip()
if len(text) > max_b:
    text = (
        text[:max_b]
        + "\n\n_(Previous review truncated for GitHub comment size.)_\n"
    )
sys.stdout.write(text)
' <<<"${body}"
}

append_previous_review_history() {
  local wrapped_file="$1"
  local previous_body="$2"
  if [ -z "${previous_body}" ]; then
    return 0
  fi
  local prev_commit stripped
  prev_commit=$(extract_commit_from_body "${previous_body}")
  prev_commit="${prev_commit:-unknown}"
  stripped=$(strip_body_for_history "${previous_body}")
  if [ -z "${stripped}" ]; then
    return 0
  fi
  local history_file="${wrapped_file}.history"
  {
    echo "<details>"
    echo "<summary>Previous review (commit <code>${prev_commit}</code>)</summary>"
    echo ""
    echo "${stripped}"
    echo ""
    echo "</details>"
    echo ""
    echo "---"
    echo ""
  } >"${history_file}"
  local current
  current=$(cat "${wrapped_file}")
  {
    cat "${history_file}"
    printf '%s' "${current}"
  } >"${wrapped_file}.with-history"
  mv "${wrapped_file}.with-history" "${wrapped_file}"
  rm -f "${history_file}"
}

build_review_header() {
  local pr_number="$1"
  local commit_sha diff_row=""
  commit_sha=$(review_commit_short)
  if [ "${REVIEW_DIFF_TRUNCATED}" = "1" ]; then
    diff_row="| **Diff** | Truncated (first ${DIFF_MAX_BYTES} bytes); findings may not cover the full PR |
"
  fi
  cat <<EOF
## Cursor code review

${REVIEW_MARKER}

| | |
| --- | --- |
| **Commit** | \`${commit_sha}\` |
| **PR** | #${pr_number} |
| **Model** | \`${MODEL}\` |
${diff_row}
Automated review (updated on each push). To disable: add label \`skip-review\` or \`[skip review]\` in the PR title.

> **For agents:** Treat only **Findings** below as open work. Items in *Previous review* are historical unless the current diff still shows the problem.

---

EOF
}

find_review_comment() {
  local repo="$1"
  local pr_number="$2"
  gh api "repos/${repo}/issues/${pr_number}/comments" --paginate \
    --jq '[.[] | select(.body | startswith("## Cursor code review")) | select(.body | contains("vworkspace-agent-review"))] | sort_by(.id) | first | if . then "\(.id)\n\(.body)" else empty end'
}

dedupe_review_comments() {
  local repo="$1"
  local pr_number="$2"
  local canonical_id="$3"
  local dup_ids
  dup_ids=$(
    gh api "repos/${repo}/issues/${pr_number}/comments" --paginate \
      --jq ".[] | select(.body | startswith(\"## Cursor code review\")) | select(.body | contains(\"vworkspace-agent-review\")) | select(.id != ${canonical_id}) | .id"
  )
  local repair_body
  repair_body=$(cat <<EOF
> **Superseded** — duplicate automated review comment. See the canonical **Cursor code review** comment on this PR.
EOF
)
  local id
  while IFS= read -r id; do
    [ -n "${id}" ] || continue
    gh api -X PATCH "repos/${repo}/issues/comments/${id}" -f body="${repair_body}" >/dev/null
    echo "Superseded duplicate review comment ${id}"
  done <<<"${dup_ids}"
}

repair_orphan_review_comments() {
  local repo="$1"
  local pr_number="$2"
  local canonical_id="$3"
  local ids
  ids=$(
    gh api "repos/${repo}/issues/${pr_number}/comments" --paginate \
      --jq ".[] | select(.id != ${canonical_id}) | select(.body | test(\"^@[~/].*\\\\.wrapped$\")) | select(.user.type == \"Bot\" or .user.login == \"github-actions[bot]\") | .id"
  )
  if [ -z "${ids}" ]; then
    return 0
  fi
  local repair_body
  repair_body=$(cat <<EOF
> **Superseded** — an early automated run posted a file reference instead of review text. See the latest **Cursor code review** comment on this PR.
EOF
)
  local id
  while IFS= read -r id; do
    [ -n "${id}" ] || continue
    gh api -X PATCH "repos/${repo}/issues/comments/${id}" -f body="${repair_body}" >/dev/null
    echo "Repaired orphan review comment ${id}"
  done <<<"${ids}"
}

patch_comment_body() {
  local repo="$1"
  local comment_id="$2"
  local body_file="$3"
  if command -v python3 >/dev/null 2>&1; then
    python3 - "${body_file}" <<'PY' | gh api -X PATCH "repos/${repo}/issues/comments/${comment_id}" --input -
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
print(json.dumps({"body": path.read_text(encoding="utf-8")}))
PY
  else
    die "python3 is required to PATCH PR review comments"
  fi
}

post_comment_body() {
  local repo="$1"
  local pr_number="$2"
  local comment_id="$3"
  local body_file="$4"
  if [ -n "${comment_id}" ]; then
    patch_comment_body "${repo}" "${comment_id}" "${body_file}"
    echo "Updated review comment ${comment_id}"
  else
    gh pr comment "${pr_number}" --body-file "${body_file}"
    echo "Posted new review comment on PR #${pr_number}"
  fi
}

post_or_update_comment() {
  local pr_number="$1"
  local body_file="$2"
  local repo="${GITHUB_REPOSITORY}"
  local wrapped_file="${body_file}.wrapped"

  {
    build_review_header "${pr_number}"
    cat "${body_file}"
  } >"${wrapped_file}"

  local existing_id="" previous_body=""
  local found
  found=$(find_review_comment "${repo}" "${pr_number}" || true)
  if [ -n "${found}" ]; then
    existing_id=$(echo "${found}" | head -n1)
    previous_body=$(echo "${found}" | tail -n +2)
    append_previous_review_history "${wrapped_file}" "${previous_body}"
  fi

  post_comment_body "${repo}" "${pr_number}" "${existing_id}" "${wrapped_file}"

  local canonical_id="${existing_id}"
  if [ -z "${canonical_id}" ]; then
    canonical_id=$(
      gh api "repos/${repo}/issues/${pr_number}/comments" --paginate \
        --jq '[.[] | select(.body | startswith("## Cursor code review")) | select(.body | contains("vworkspace-agent-review"))] | sort_by(.id) | first | .id'
    )
  fi
  if [ -n "${canonical_id}" ]; then
    dedupe_review_comments "${repo}" "${pr_number}" "${canonical_id}"
    repair_orphan_review_comments "${repo}" "${pr_number}" "${canonical_id}"
  fi
}

HACK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Prints comma-separated blocking severities to stdout; exit 0 if any, 1 if none.
find_blocking_findings() {
  local review_file="$1"
  REVIEW_FAIL_SEVERITIES="${REVIEW_FAIL_SEVERITIES}" \
    python3 "${HACK_DIR}/code_review_findings.py" "${review_file}"
}

main() {
  require_env CURSOR_API_KEY
  require_env GITHUB_TOKEN
  require_env GITHUB_REPOSITORY
  require_env GITHUB_WORKSPACE

  local pr_number="${1:-}"
  if [ -z "${pr_number}" ]; then
    die "Usage: $0 <pr-number>"
  fi

  export GH_TOKEN="${GITHUB_TOKEN}"

  ensure_gh
  ensure_cursor_agent

  local diff_file="${GITHUB_WORKSPACE}/.pr-diff.txt"
  local review_file="${GITHUB_WORKSPACE}/.cursor-review.md"

  write_diff "${pr_number}" "${diff_file}"

  REVIEW_FAILED=0
  if ! run_review "${diff_file}" "${review_file}"; then
    REVIEW_FAILED=1
    {
      echo "## Summary"
      echo "The review agent did not complete successfully."
      echo
      echo "## Findings"
      echo "_No actionable findings (agent failed)._"
      echo
      echo "## Test plan"
      echo "- Check the workflow logs for this run."
      echo
      echo "<details><summary>Agent output</summary>"
      echo
      cat "${review_file}"
      echo
      echo "</details>"
    } >"${review_file}"
  else
    normalize_review_output "${review_file}"
    if ! validate_review_output "${review_file}"; then
      REVIEW_FAILED=1
      {
        echo "## Summary"
        echo "The review agent returned invalid output."
        echo
        echo "## Findings"
        echo "_No actionable findings (validation failed)._"
        echo
        echo "## Test plan"
        echo "- Re-run the workflow or inspect logs."
        echo
        echo "<details><summary>Raw agent output</summary>"
        echo
        cat "${review_file}"
        echo
        echo "</details>"
      } >"${review_file}.invalid"
      mv "${review_file}.invalid" "${review_file}"
    fi
  fi

  post_or_update_comment "${pr_number}" "${review_file}"

  local blocking=""
  if [ "${REVIEW_FAILED}" != "1" ]; then
    if blocking=$(find_blocking_findings "${review_file}"); then
      REVIEW_FAILED=1
      echo "::error::Cursor code review reported blocking finding(s): ${blocking} (see PR comment → Findings)" >&2
    fi
  fi

  rm -f "${diff_file}" "${GITHUB_WORKSPACE}/PR_DIFF.txt" \
    "${review_file}" "${review_file}.wrapped" 2>/dev/null || true

  if [ "${REVIEW_FAILED}" = "1" ]; then
    if [ -n "${blocking}" ]; then
      die "Code review has open ${blocking} finding(s); fix or push updates (see PR comment)"
    fi
    die "Code review agent failed or returned invalid output (see PR comment)"
  fi
}

main "$@"
