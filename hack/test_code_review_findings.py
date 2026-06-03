#!/usr/bin/env python3
"""Unit tests for hack/code_review_findings.py"""

import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from code_review_findings import parse_blocking_severities


class ParseBlockingSeveritiesTests(unittest.TestCase):
    def test_no_findings_section(self) -> None:
        self.assertEqual(parse_blocking_severities("## Summary\nok\n", "major"), [])

    def test_no_actionable_findings_with_period(self) -> None:
        body = """## Summary
ok

## Findings
_No actionable findings._

## Test plan
- run tests
"""
        self.assertEqual(parse_blocking_severities(body, "critical,major,minor"), [])

    def test_no_actionable_findings_without_period(self) -> None:
        body = """## Findings
_No actionable findings_

## Test plan
- t
"""
        self.assertEqual(parse_blocking_severities(body, "major"), [])

    def test_major_heading_blocks(self) -> None:
        body = """## Findings
### [major] Example issue
- **Severity:** major
- **File:** `internal/controller/foo.go`
- **Evidence:** line 1
- **Issue:** bad
- **Suggested fix:** fix it

## Test plan
- verify
"""
        self.assertEqual(
            parse_blocking_severities(body, "critical,major,minor"), ["major"]
        )

    def test_critical_and_minor(self) -> None:
        body = """## Findings
### [critical] Blocker
- **Severity:** critical

### [minor] Small
- **Severity:** minor

## Test plan
"""
        self.assertEqual(
            parse_blocking_severities(body, "critical,major,minor"),
            ["critical", "minor"],
        )

    def test_nit_does_not_block_by_default(self) -> None:
        body = """## Findings
### [nit] Style
- **Severity:** nit

## Test plan
"""
        self.assertEqual(parse_blocking_severities(body, "critical,major,minor"), [])

    def test_nit_blocks_when_configured(self) -> None:
        body = """## Findings
### [nit] Style
- **Severity:** nit

## Test plan
"""
        self.assertEqual(parse_blocking_severities(body, "nit"), ["nit"])

    def test_none_disables_gating(self) -> None:
        body = """## Findings
### [major] Still here

## Test plan
"""
        self.assertEqual(parse_blocking_severities(body, "none"), [])

    def test_evidence_line_does_not_false_positive(self) -> None:
        """Evidence mentioning **major** must not count without a finding header."""
        body = """## Findings
_No actionable findings._

## Test plan
"""
        # Inject a line that used to trip the loose third regex
        body = body.replace(
            "_No actionable findings._",
            "- **Evidence:** treat as **major** — example only\n_No actionable findings._",
        )
        self.assertEqual(parse_blocking_severities(body, "major"), [])

    def test_severity_line_without_heading(self) -> None:
        body = """## Findings
- **Severity:** major

## Test plan
"""
        self.assertEqual(parse_blocking_severities(body, "major"), ["major"])

    def test_quoted_heading_in_suggested_fix_not_counted(self) -> None:
        body = """## Findings
### [minor] Real issue
- **Severity:** minor
- **File:** `x.go`
- **Evidence:** ok
- **Issue:** ok
- **Suggested fix:** use format:
### [major] Example only

## Test plan
"""
        self.assertEqual(
            parse_blocking_severities(body, "critical,major,minor"), ["minor"]
        )

    def test_nested_structured_example_in_suggested_fix_not_counted(self) -> None:
        body = """## Findings
### [minor] Real issue
- **Severity:** minor
- **File:** `x.go`
- **Evidence:** ok
- **Issue:** ok
- **Suggested fix:** follow this template:
### [major] Example only
- **Severity:** major
- **File:** `y.go`
- **Evidence:** ex
- **Issue:** ex

## Test plan
"""
        self.assertEqual(
            parse_blocking_severities(body, "critical,major,minor"), ["minor"]
        )

    def test_adjacent_findings_without_blank_line(self) -> None:
        body = """## Findings
### [critical] Blocker
- **Severity:** critical
### [minor] Small
- **Severity:** minor

## Test plan
"""
        self.assertEqual(
            parse_blocking_severities(body, "critical,major,minor"),
            ["critical", "minor"],
        )

    def test_second_finding_after_suggested_fix_still_counts(self) -> None:
        body = """## Findings
### [minor] First
- **Severity:** minor
- **File:** `a.go`
- **Issue:** i
- **Suggested fix:** do x

### [major] Second
- **Severity:** major
- **File:** `b.go`
- **Evidence:** second evidence
- **Issue:** second issue

## Test plan
"""
        self.assertEqual(
            parse_blocking_severities(body, "critical,major,minor"),
            ["major", "minor"],
        )

    def test_example_after_blank_line_under_suggested_fix_not_counted(self) -> None:
        body = """## Findings
### [minor] Parser edge case
- **Severity:** minor
- **File:** `internal/controller/x.go`
- **Issue:** false positive
- **Suggested fix:** use this shape:

### [major] Add validation
- **Severity:** major

## Test plan
"""
        self.assertEqual(
            parse_blocking_severities(body, "critical,major,minor"), ["minor"]
        )

    def test_severity_only_example_in_suggested_fix_not_counted(self) -> None:
        body = """## Findings
### [minor] Parser edge case
- **Severity:** minor
- **File:** `hack/code_review_findings.py`
- **Issue:** false positive
- **Suggested fix:** use this shape:
### [major] Add validation
- **Severity:** major

## Test plan
"""
        self.assertEqual(
            parse_blocking_severities(body, "critical,major,minor"), ["minor"]
        )

    def test_concrete_example_in_suggested_fix_not_counted(self) -> None:
        body = """## Findings
### [minor] Parser edge case
- **Severity:** minor
- **File:** `hack/code_review_findings.py`
- **Issue:** false positive
- **Suggested fix:** use this shape:
### [major] Add validation
- **Severity:** major
- **File:** `internal/controller/x.go`
- **Evidence:** nested
- **Issue:** quoted

## Test plan
"""
        self.assertEqual(
            parse_blocking_severities(body, "critical,major,minor"), ["minor"]
        )

    def test_test_plan_heading_stops_findings_section(self) -> None:
        body = """## Findings
### [major] X
- **Severity:** major

## Test Plan
- **Severity:** major

"""
        self.assertEqual(parse_blocking_severities(body, "major"), ["major"])


class CLITests(unittest.TestCase):
    def test_cli_exits_zero_when_blocking(self) -> None:
        script = Path(__file__).resolve().parent / "code_review_findings.py"
        with tempfile.NamedTemporaryFile("w", suffix=".md", delete=False) as f:
            f.write(
                "## Findings\n### [major] X\n- **Severity:** major\n\n## Test plan\n"
            )
            path = f.name
        try:
            env = {**os.environ, "REVIEW_FAIL_SEVERITIES": "major"}
            proc = subprocess.run(
                [sys.executable, str(script), path],
                env=env,
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertEqual(proc.returncode, 0)
            self.assertEqual(proc.stdout.strip(), "major")
        finally:
            Path(path).unlink(missing_ok=True)


if __name__ == "__main__":
    unittest.main()
