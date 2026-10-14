#!/usr/bin/env python3

from __future__ import annotations

import argparse
from pathlib import Path


MINIMUM_PERCENT = {
    "total": 63.0,
    "github.com/foigent/x-insight/internal/api": 53.0,
    "github.com/foigent/x-insight/internal/db": 69.0,
    "github.com/foigent/x-insight/internal/hostctl": 64.0,
    "github.com/foigent/x-insight/internal/workspace": 46.0,
}


def parse_profile(payload: str) -> dict[str, tuple[int, int]]:
    totals: dict[str, list[int]] = {"total": [0, 0]}
    lines = payload.splitlines()
    if not lines or not lines[0].startswith("mode: "):
        raise ValueError("coverage profile is missing its mode header")
    for line in lines[1:]:
        fields = line.split()
        if len(fields) != 3:
            raise ValueError(f"invalid coverage row: {line}")
        source, statements_text, count_text = fields
        filename = source.split(":", 1)[0]
        package = filename.rsplit("/", 1)[0]
        statements = int(statements_text)
        count = int(count_text)
        covered = statements if count > 0 else 0
        for key in ("total", package):
            bucket = totals.setdefault(key, [0, 0])
            bucket[0] += covered
            bucket[1] += statements
    return {key: (value[0], value[1]) for key, value in totals.items()}


def percentage(value: tuple[int, int]) -> float:
    covered, total = value
    if total == 0:
        return 0.0
    return covered * 100.0 / total


def verify(payload: str) -> list[str]:
    coverage = parse_profile(payload)
    failures: list[str] = []
    for package, minimum in MINIMUM_PERCENT.items():
        actual = percentage(coverage.get(package, (0, 0)))
        print(f"go_coverage package={package} actual={actual:.1f}% minimum={minimum:.1f}%")
        if actual + 1e-9 < minimum:
            failures.append(f"{package}: {actual:.1f}% < {minimum:.1f}%")
    return failures


def self_test() -> None:
    payload = "\n".join(
        [
            "mode: atomic",
            "github.com/foigent/x-insight/internal/api/a.go:1.1,2.1 3 1",
            "github.com/foigent/x-insight/internal/api/a.go:3.1,4.1 1 0",
        ]
    )
    parsed = parse_profile(payload)
    assert parsed["total"] == (3, 4)
    assert parsed["github.com/foigent/x-insight/internal/api"] == (3, 4)
    assert percentage(parsed["total"]) == 75.0
    try:
        parse_profile("not a profile")
    except ValueError:
        pass
    else:
        raise AssertionError("invalid profile was accepted")
    print("check-go-coverage self-test passed")


def main() -> int:
    parser = argparse.ArgumentParser(description="enforce the X-Insight Go coverage non-regression floor")
    parser.add_argument("profile", nargs="?", type=Path)
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args()
    if args.self_test:
        self_test()
        return 0
    if args.profile is None:
        parser.error("profile is required unless --self-test is used")
    failures = verify(args.profile.read_text(encoding="utf-8"))
    if failures:
        for failure in failures:
            print(f"go_coverage_failure {failure}")
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
