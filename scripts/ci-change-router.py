#!/usr/bin/env python3
"""Conservatively route X-Insight CI from the changed Git paths."""

from __future__ import annotations

import argparse
import json
import pathlib
import subprocess
import sys
from collections.abc import Iterable


ZERO_SHA = "0" * 40


def is_safe_documentation(path: str) -> bool:
    """Return true only for documentation that cannot affect a release payload."""
    candidate = pathlib.PurePosixPath(path)
    if (
        not path
        or path.startswith("/")
        or "\\" in path
        or ".." in candidate.parts
        or str(candidate) != path
    ):
        return False
    if path == "README.md":
        return True
    if not path.startswith("docs/"):
        return False
    return not (path == "docs/manual" or path.startswith("docs/manual/"))


def classify(paths: Iterable[str]) -> dict[str, object]:
    unique_paths = sorted(set(paths))
    safe_docs_only = bool(unique_paths) and all(
        is_safe_documentation(path) for path in unique_paths
    )
    return {
        "classification": "safe-docs-only" if safe_docs_only else "release-relevant",
        "safe_docs_only": safe_docs_only,
        "changed_count": len(unique_paths),
        "paths": unique_paths,
    }


def changed_paths(event: str, base: str, head: str) -> list[str]:
    if event not in {"pull_request", "push"}:
        raise ValueError(f"unsupported event: {event!r}")
    if len(head) != 40 or any(
        character not in "0123456789abcdef" for character in head
    ):
        raise ValueError("head must be a 40-character lowercase Git SHA")

    if base == ZERO_SHA:
        comparison = f"{head}^..{head}"
    else:
        if len(base) != 40 or any(
            character not in "0123456789abcdef" for character in base
        ):
            raise ValueError("base must be a 40-character lowercase Git SHA")
        separator = "..." if event == "pull_request" else ".."
        comparison = f"{base}{separator}{head}"

    completed = subprocess.run(
        ["git", "diff", "--name-only", "--no-renames", "-z", comparison],
        check=True,
        stdout=subprocess.PIPE,
    )
    return [
        path.decode("utf-8", errors="replace")
        for path in completed.stdout.split(b"\0")
        if path
    ]


def append_outputs(path: pathlib.Path, result: dict[str, object]) -> None:
    with path.open("a", encoding="utf-8") as output:
        output.write(f"classification={result['classification']}\n")
        output.write(f"safe_docs_only={str(result['safe_docs_only']).lower()}\n")
        output.write(f"changed_count={result['changed_count']}\n")


def append_summary(path: pathlib.Path, result: dict[str, object]) -> None:
    with path.open("a", encoding="utf-8") as summary:
        summary.write("### CI change route\n\n")
        summary.write(f"- Classification: `{result['classification']}`\n")
        summary.write(f"- Changed paths: `{result['changed_count']}`\n")
        summary.write(
            "- Expensive PR validation and main release qualification are "
            f"{'skipped' if result['safe_docs_only'] else 'required'}.\n"
        )


def self_test() -> None:
    cases = [
        (["README.md"], True),
        (["docs/planning/phase-2.md", "docs/evidence/result.md"], True),
        (["docs/manual/zh-CN/index.md"], False),
        (["docs/manual"], False),
        ([".github/workflows/ci.yml"], False),
        (["internal/api/server.go"], False),
        (["internal/access/accounts.go"], False),
        (["schemas/access/local-accounts-v1.schema.json"], False),
        (["docs/planning/phase-2.md", "frontend/src/App.tsx"], False),
        ([], False),
        (["docs/../internal/api/server.go"], False),
    ]
    for paths, expected in cases:
        actual = classify(paths)["safe_docs_only"]
        if actual is not expected:
            raise AssertionError(f"classify({paths!r})={actual!r}, expected {expected!r}")
    print(f"ci-change-router self-test: {len(cases)} cases passed")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--self-test", action="store_true")
    parser.add_argument("--event", choices=("pull_request", "push"))
    parser.add_argument("--base")
    parser.add_argument("--head")
    parser.add_argument("--github-output", type=pathlib.Path)
    parser.add_argument("--step-summary", type=pathlib.Path)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if args.self_test:
        self_test()
        return 0
    required = {
        "event": args.event,
        "base": args.base,
        "head": args.head,
        "github_output": args.github_output,
        "step_summary": args.step_summary,
    }
    missing = sorted(name for name, value in required.items() if value is None)
    if missing:
        raise SystemExit(f"missing required arguments: {', '.join(missing)}")

    result = classify(changed_paths(args.event, args.base, args.head))
    append_outputs(args.github_output, result)
    append_summary(args.step_summary, result)
    print(json.dumps(result, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        raise SystemExit(f"ci-change-router: {error}") from error
