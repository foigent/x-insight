#!/usr/bin/env python3
"""Static regression checks for the Draft Gate and observer workflow contract."""

from __future__ import annotations

import argparse
import pathlib
import sys


def require(text: str, fragment: str, label: str) -> None:
    if fragment not in text:
        raise ValueError(f"{label} is missing {fragment!r}")


def forbid(text: str, fragment: str, label: str) -> None:
    if fragment in text:
        raise ValueError(f"{label} contains prohibited {fragment!r}")


def validate(repository_root: pathlib.Path) -> None:
    conformance_path = repository_root / ".github/workflows/foigent-protocol-conformance.yml"
    observer_path = repository_root / ".github/workflows/execution-evidence-drift-observer.yml"
    ci_path = repository_root / ".github/workflows/ci.yml"
    conformance = conformance_path.read_text(encoding="utf-8")
    observer = observer_path.read_text(encoding="utf-8")
    ci = ci_path.read_text(encoding="utf-8")

    for fragment in (
        "name: Foigent Protocol Conformance",
        "protocol-pin:",
        "name: X-Insight Protocol Pin",
        "permissions:\n  contents: read",
        "contracts/execution-evidence/incubation/**",
        "cmd/xinsight-draft-evidence-validator/**",
        "internal/draftevidence/**",
        "fetch-depth: 0",
        "go run ./cmd/xinsight-draft-evidence-validator",
        "--event pull_request --base \"$PR_BASE_SHA\" --head \"$HEAD_SHA\"",
        "--event push --base \"$PUSH_BASE_SHA\" --head \"$HEAD_SHA\"",
        "actions/upload-artifact@v6",
    ):
        require(conformance, fragment, "Foigent Protocol Conformance")

    for fragment in (
        "name: Execution Evidence Drift Observer",
        "schedule:",
        "workflow_dispatch:",
        "permissions:\n  contents: read",
        "name: Read-only Authority and Drift Observation",
        "GITHUB_TOKEN: ${{ github.token }}",
        "--repository foigent/unirouter",
        "--finalize-workflow-report",
        "steps.finalize-observation.outputs.observation_status",
        "name: Enforce observation completion",
        "execution-evidence-drift-observation.json",
        "actions/upload-artifact@v6",
    ):
        require(observer, fragment, "Execution Evidence Drift Observer")
    for fragment in (
        "pull_request:",
        "pull_request_target:",
        "secrets.",
        "create-github-app-token",
        "--token-outcome",
        "gh issue create",
        "gh pr create",
        "git push",
        "git commit",
    ):
        forbid(observer, fragment, "Execution Evidence Drift Observer")

    for fragment in (
        "python3 scripts/check-draft-evidence-history.py --self-test",
        "python3 scripts/check-draft-evidence-workflows.py --self-test",
        "python3 scripts/observe-execution-evidence-drift.py --self-test",
    ):
        require(ci, fragment, "CI script self-tests")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--self-test", action="store_true")
    parser.add_argument("--repository-root", type=pathlib.Path, default=pathlib.Path("."))
    args = parser.parse_args()
    validate(args.repository_root.resolve())
    print("draft-evidence-workflows: workflow contract passed")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError) as error:
        raise SystemExit(f"draft-evidence-workflows: {error}") from error
