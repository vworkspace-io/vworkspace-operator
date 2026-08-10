#!/usr/bin/env python3
"""Prepare PR diffs for Cursor code review when hunks exceed size limits."""

from __future__ import annotations

import re
import sys
from pathlib import Path

_HUNK_START = re.compile(r"^diff --git ", re.MULTILINE)
_PATH = re.compile(r"^diff --git a/(\S+)", re.MULTILINE)
_CRITICAL_SUFFIXES = ("seaweeds.seaweed.seaweedfs.com.yaml",)
_MAX_LOW_PRIORITY_HUNK_BYTES = 12_000


def _split_hunks(diff_text: str) -> list[str]:
    parts = _HUNK_START.split(diff_text)
    if not parts or not parts[0].strip():
        parts = parts[1:]
    return [f"diff --git {part}" if not part.startswith("diff --git ") else part for part in parts if part.strip()]


def _path(hunk: str) -> str:
    match = _PATH.search(hunk)
    return match.group(1) if match else ""


def _priority(path: str) -> tuple[int, str]:
    """Lower sort key = earlier in review diff."""
    lower = path.lower()
    if any(lower.endswith(suffix) for suffix in _CRITICAL_SUFFIXES):
        return (-1, path)
    if "/crds/" in lower and lower.endswith((".yaml", ".yml")):
        return (3, path)
    if lower.endswith(".lock"):
        return (2, path)
    if any(
        part in lower
        for part in (
            "/templates/",
            "/values",
            "/hack/",
            ".github/",
            "/internal/",
            "/api/",
            "chart.yaml",
            "readme.md",
            "/docs/",
        )
    ):
        return (0, path)
    return (1, path)


def _shrink_hunk(hunk: str, path: str) -> str:
    """Keep review diff readable while proving large vendored files exist."""
    encoded = hunk.encode("utf-8")
    if len(encoded) <= _MAX_LOW_PRIORITY_HUNK_BYTES:
        return hunk if hunk.endswith("\n") else hunk + "\n"

    lines = hunk.splitlines()
    keep = 36 if any(path.endswith(s) for s in _CRITICAL_SUFFIXES) else 24
    if len(lines) <= keep + 2:
        return hunk if hunk.endswith("\n") else hunk + "\n"

    omitted = len(lines) - keep
    body = "\n".join(lines[:keep])
    body += (
        f"\n... ({omitted} diff lines omitted from review preview; "
        f"full file `{path}` is in the PR)\n"
    )
    return body + "\n"


def truncate_diff(diff_text: str, max_bytes: int) -> tuple[str, bool]:
    """Return diff text capped at max_bytes, preferring non-vendored hunks."""
    if len(diff_text.encode("utf-8")) <= max_bytes:
        return diff_text, False

    hunks = sorted(_split_hunks(diff_text), key=lambda h: _priority(_path(h)))
    selected: list[str] = []
    used = 0
    truncated = False

    for hunk in hunks:
        path = _path(hunk)
        chunk = _shrink_hunk(hunk, path)
        size = len(chunk.encode("utf-8"))
        if used + size > max_bytes:
            truncated = True
            continue
        selected.append(chunk)
        used += size

    if not selected:
        # Fallback: first max_bytes of original diff.
        encoded = diff_text.encode("utf-8")[:max_bytes]
        return encoded.decode("utf-8", errors="ignore"), True

    body = "".join(selected)
    if truncated:
        body += (
            f"\n\n(Diff truncated to {max_bytes} bytes; "
            "vendored CRD YAML omitted in favor of templates, values, and hack changes.)\n"
        )
    return body, True


def main() -> None:
    if len(sys.argv) not in (3, 4):
        print(
            "usage: code_review_diff.py <input.diff> <output.diff> [max_bytes]",
            file=sys.stderr,
        )
        sys.exit(2)

    src = Path(sys.argv[1]).read_text(encoding="utf-8")
    max_bytes = int(sys.argv[3]) if len(sys.argv) == 4 else 200_000
    out, _ = truncate_diff(src, max_bytes)
    Path(sys.argv[2]).write_text(out, encoding="utf-8")


if __name__ == "__main__":
    main()
