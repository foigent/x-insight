#!/usr/bin/env python3
"""Read the release identity from an already-verified target payload."""

from __future__ import annotations

import argparse
import json
import pathlib
import re
import sys
from typing import Any


SCHEMA_VERSION = "xinsight.compatibility-matrix.v1"
SEMVER_PATTERN = re.compile(
    r"^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
    r"(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$"
)
MANUAL_VERSION_PATTERN = re.compile(
    r"^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?/manual-r[1-9][0-9]*$"
)


def validate_identity(matrix: Any) -> tuple[str, str]:
    if not isinstance(matrix, dict):
        raise ValueError("compatibility matrix must be a JSON object")
    if matrix.get("schema_version") != SCHEMA_VERSION:
        raise ValueError("compatibility matrix schema identity mismatch")

    product_version = matrix.get("product_version")
    manual_version = matrix.get("manual_version")
    if not isinstance(product_version, str) or not SEMVER_PATTERN.fullmatch(
        product_version
    ):
        raise ValueError("compatibility matrix product_version is invalid")
    if (
        not isinstance(manual_version, str)
        or not MANUAL_VERSION_PATTERN.fullmatch(manual_version)
        or not manual_version.startswith(product_version + "/")
    ):
        raise ValueError("compatibility matrix manual_version is invalid")
    return product_version, manual_version


def load_identity(path: pathlib.Path) -> tuple[str, str]:
    matrix = json.loads(path.read_text(encoding="utf-8"))
    return validate_identity(matrix)


def self_test() -> None:
    passing = [
        {
            "schema_version": SCHEMA_VERSION,
            "product_version": "0.3.0-rc.2",
            "manual_version": "0.3.0-rc.2/manual-r2",
        },
        {
            "schema_version": SCHEMA_VERSION,
            "product_version": "1.2.3",
            "manual_version": "1.2.3/manual-r1",
        },
    ]
    for matrix in passing:
        product_version, manual_version = validate_identity(matrix)
        if product_version != matrix["product_version"]:
            raise AssertionError("product version changed during validation")
        if manual_version != matrix["manual_version"]:
            raise AssertionError("manual version changed during validation")

    rejected = [
        [],
        {
            "schema_version": "xinsight.compatibility-matrix.v2",
            "product_version": "0.3.0-rc.2",
            "manual_version": "0.3.0-rc.2/manual-r2",
        },
        {
            "schema_version": SCHEMA_VERSION,
            "product_version": "03.0.0",
            "manual_version": "03.0.0/manual-r1",
        },
        {
            "schema_version": SCHEMA_VERSION,
            "product_version": "0.3.0-rc.2",
            "manual_version": "0.3.0-rc.1/manual-r2",
        },
        {
            "schema_version": SCHEMA_VERSION,
            "product_version": "0.3.0-rc.2",
            "manual_version": "0.3.0-rc.2/manual-r0",
        },
        {
            "schema_version": SCHEMA_VERSION,
            "product_version": None,
            "manual_version": "0.3.0-rc.2/manual-r2",
        },
    ]
    for matrix in rejected:
        try:
            validate_identity(matrix)
        except ValueError:
            continue
        raise AssertionError(f"expected release identity rejection: {matrix!r}")

    total = len(passing) + len(rejected)
    print(f"release-qualification-identity self-test: {total} cases passed")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--self-test", action="store_true")
    parser.add_argument("--matrix", type=pathlib.Path)
    parser.add_argument("--field", choices=("product_version", "manual_version"))
    args = parser.parse_args()

    if args.self_test:
        if args.matrix is not None or args.field is not None:
            parser.error("--self-test cannot be combined with --matrix or --field")
        self_test()
        return 0
    if args.matrix is None or args.field is None:
        parser.error("--matrix and --field are required unless --self-test is used")

    product_version, manual_version = load_identity(args.matrix)
    values = {
        "product_version": product_version,
        "manual_version": manual_version,
    }
    print(values[args.field])
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError) as error:
        raise SystemExit(f"release-qualification-identity: {error}") from error
