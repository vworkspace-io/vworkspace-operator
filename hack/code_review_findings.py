#!/usr/bin/env python3
"""Parse ## Findings from Cursor PR review markdown for CI severity gating."""

from __future__ import annotations

import os
import re
import sys
from pathlib import Path

_FINDING_HEADING = re.compile(
    r"^### \[(critical|major|minor|nit)\]", re.IGNORECASE | re.MULTILINE
)
_HEADING_LINE = re.compile(r"^### \[(critical|major|minor|nit)\]", re.IGNORECASE)
_SEVERITY_LINE = re.compile(
    r"^-\s+\*\*Severity:\*\*\s*(critical|major|minor|nit)\b",
    re.IGNORECASE,
)
_SUGGESTED_FIX_LINE = re.compile(r"^-\s+\*\*Suggested fix:\*\*", re.IGNORECASE)
_FILE_LINE = re.compile(r"^-\s+\*\*File:\*\*", re.IGNORECASE)


def _is_immediate_after_suggested_fix(lines: list[str], heading_index: int) -> bool:
    """True when ### immediately follows the Suggested fix line (no blank line)."""
    j = heading_index - 1
    if j >= 0 and not lines[j].strip():
        return False
    return j >= 0 and _SUGGESTED_FIX_LINE.match(lines[j])


def _incomplete_example_block(lines: list[str], start: int) -> bool:
    """True when a ### block lacks a File bullet (likely an illustrative snippet)."""
    window = lines[start : start + 12]
    return not any(_FILE_LINE.match(ln) for ln in window)


def _skip_example_block(lines: list[str], start: int) -> int:
    """Skip a nested ### [severity] example block."""
    i = start
    if i >= len(lines) or not _HEADING_LINE.match(lines[i]):
        return i + 1
    i += 1
    while i < len(lines):
        if _HEADING_LINE.match(lines[i]):
            break
        if lines[i].strip() == "":
            return i + 1
        i += 1
    return i


def _collect_findings(body: str) -> list[str]:
    """Walk Findings body; only start a new finding at a top-level ### heading."""
    found: list[str] = []
    lines = body.splitlines()
    block_complete = True
    after_suggested_fix_pending = False
    current_sev: str | None = None
    has_sev = False
    i = 0

    def flush_current() -> None:
        nonlocal current_sev, has_sev, block_complete
        if current_sev and has_sev:
            found.append(current_sev)
        current_sev = None
        has_sev = False
        block_complete = True

    while i < len(lines):
        line = lines[i]
        hm = _HEADING_LINE.match(line)
        if hm:
            if after_suggested_fix_pending and (
                _is_immediate_after_suggested_fix(lines, i)
                or _incomplete_example_block(lines, i)
            ):
                i = _skip_example_block(lines, i)
                after_suggested_fix_pending = False
                continue
            after_suggested_fix_pending = False
            if not block_complete and current_sev and has_sev:
                flush_current()
            if block_complete:
                if current_sev and has_sev:
                    found.append(current_sev)
                current_sev = hm.group(1).lower()
                has_sev = False
                block_complete = False
            i += 1
            continue

        if current_sev is not None and not block_complete:
            sm = _SEVERITY_LINE.match(line)
            if sm and sm.group(1).lower() == current_sev:
                has_sev = True
            elif _SUGGESTED_FIX_LINE.match(line):
                if has_sev:
                    flush_current()
                after_suggested_fix_pending = True
            elif line.strip() == "" and has_sev:
                block_complete = True
        i += 1

    if current_sev and has_sev:
        found.append(current_sev)

    return found


def _orphan_severity_lines(text: str) -> list[str]:
    """Severity bullets before the first ### heading."""
    found: list[str] = []
    for line in text.splitlines():
        if _HEADING_LINE.match(line):
            break
        m = re.match(
            r"^-\s+\*\*Severity:\*\*\s*(critical|major|minor|nit)\b",
            line,
            re.IGNORECASE,
        )
        if m:
            found.append(m.group(1).lower())
    return found


def parse_blocking_severities(text: str, fail_severities: str) -> list[str]:
    """Return sorted severities that should fail CI, or [] if none / gating disabled."""
    config = fail_severities.strip().lower()
    if not config or config == "none":
        return []

    fail_on = {s.strip() for s in config.split(",") if s.strip()}
    match = re.search(
        r"^## Findings\s*\n(.*?)(?=^## |\Z)",
        text,
        re.MULTILINE | re.DOTALL | re.IGNORECASE,
    )
    if not match:
        return []

    body = match.group(1)
    found = _collect_findings(body)
    found.extend(_orphan_severity_lines(body))

    if re.search(r"_no actionable findings_\.?", body, re.IGNORECASE):
        if not _FINDING_HEADING.search(body) and not found:
            return []

    return sorted({s for s in found if s in fail_on})


def main() -> None:
    if len(sys.argv) != 2:
        print("usage: code_review_findings.py <review.md>", file=sys.stderr)
        sys.exit(2)

    text = Path(sys.argv[1]).read_text(encoding="utf-8")
    config = os.environ.get("REVIEW_FAIL_SEVERITIES", "critical,major,minor")
    blocking = parse_blocking_severities(text, config)
    if blocking:
        print(",".join(blocking))
        sys.exit(0)
    sys.exit(1)


if __name__ == "__main__":
    main()
