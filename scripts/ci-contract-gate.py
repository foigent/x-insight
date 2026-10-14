#!/usr/bin/env python3
"""Validate that the CI jobs executed exactly the route selected for the event."""

from __future__ import annotations

import argparse
import json
import os
import pathlib
import sys


RESULTS = {"success", "failure", "cancelled", "skipped"}


def direct_fpv9_required(event: str, safe_docs_only: bool, same_repository: bool, product_version: str) -> bool:
    return (
        event == "pull_request"
        and not safe_docs_only
        and same_repository
        and product_version == "0.2.0-dev.fpv11"
    )


def managed_fpv11_required(event: str, safe_docs_only: bool, same_repository: bool, product_version: str) -> bool:
    return event == "pull_request" and not safe_docs_only and same_repository and product_version == "0.2.0-dev.fpv12"


def current_release_required(event: str, safe_docs_only: bool, same_repository: bool) -> bool:
    # These jobs use ephemeral CI keys and produce explicitly non-publishable
    # artifacts. A fork needs the same installation/signature checks as a local
    # branch; repository identity does not confer release authority here.
    return event == "pull_request" and not safe_docs_only


def qualification_route_from_environment(managed: bool = False) -> bool:
    config_path = pathlib.Path(__file__).resolve().parents[1] / "release/d-validation-config.json"
    config = json.loads(config_path.read_text(encoding="utf-8"))
    version = config["product_version"]
    if not isinstance(version, str) or not version:
        raise ValueError("missing product version for qualification routing")
    if version == "0.2.0-dev.fpv11" and config["database_schema"]["current"] != "43":
        raise ValueError("FPV11 qualification requires schema43 metadata")
    if version == "0.2.0-dev.fpv12" and config["database_schema"]["current"] != "44":
        raise ValueError("FPV12 qualification requires schema44 metadata")
    repository = os.environ["GITHUB_REPOSITORY"]
    if not repository:
        raise ValueError("missing repository identity for qualification routing")
    select = managed_fpv11_required if managed else direct_fpv9_required
    return select(
        os.environ["EVENT_NAME"],
        parse_bool(os.environ["SAFE_DOCS_ONLY"]),
        repository == os.environ["PR_HEAD_REPOSITORY"],
        version,
    )


def current_release_route_from_environment() -> bool:
    repository = os.environ["GITHUB_REPOSITORY"]
    if not repository:
        raise ValueError("missing repository identity for qualification routing")
    return current_release_required(
        os.environ["EVENT_NAME"], parse_bool(os.environ["SAFE_DOCS_ONLY"]),
        repository == os.environ["PR_HEAD_REPOSITORY"],
    )


def expected_results(event: str, safe_docs_only: bool, direct_required: bool = False, managed_required: bool = False, controller_required: bool = False) -> dict[str, str]:
    expected = {
        "frontend": "skipped",
        "backend": "skipped",
        "admin_rotation": "skipped",
        "signed_release": "skipped",
        "fresh_install": "skipped",
        "direct_fpv9": "skipped",
        "managed_fpv11": "skipped",
        "p3_component": "skipped" if safe_docs_only else "success",
    }
    if event == "pull_request" and not safe_docs_only:
        expected["frontend"] = "success"
        expected["backend"] = "success"
        expected["admin_rotation"] = "success"
        if controller_required:
            expected["signed_release"] = "success"
            expected["fresh_install"] = "success"
    elif event == "push" and not safe_docs_only:
        expected["signed_release"] = "success"
        expected["fresh_install"] = "success"
    elif event not in {"pull_request", "push"}:
        raise ValueError(f"unsupported CI event: {event!r}")
    if direct_required:
        if event != "pull_request" or safe_docs_only:
            raise ValueError("exact FPV9 qualification is limited to release-relevant PRs")
        expected["direct_fpv9"] = "success"
    if managed_required:
        if event != "pull_request" or safe_docs_only or direct_required:
            raise ValueError("managed FPV11 qualification requires its separate release-relevant PR route")
        expected["managed_fpv11"] = "success"
    return expected


def validate(
    event: str,
    safe_docs_only: bool,
    classification: str,
    changes_result: str,
    actual: dict[str, str],
    direct_required: bool = False,
    managed_required: bool = False,
    controller_required: bool = False,
) -> None:
    if changes_result != "success":
        raise ValueError(f"change classification did not succeed: {changes_result!r}")
    expected_classification = (
        "safe-docs-only" if safe_docs_only else "release-relevant"
    )
    if classification != expected_classification:
        raise ValueError(
            f"classification={classification!r}, expected {expected_classification!r}"
        )
    invalid_results = {
        name: result for name, result in actual.items() if result not in RESULTS
    }
    if invalid_results:
        raise ValueError(f"unrecognized job results: {invalid_results}")

    if controller_required and (event != "pull_request" or safe_docs_only):
        raise ValueError("current signed and fresh-install qualification requires its release-relevant PR route")
    expected = expected_results(event, safe_docs_only, direct_required, managed_required, controller_required)
    if actual.keys() != expected.keys():
        raise ValueError(f"missing or unexpected CI results: {sorted(actual)}")
    mismatches = {
        job: {"actual": actual[job], "expected": result}
        for job, result in expected.items()
        if actual[job] != result
    }
    if mismatches:
        raise ValueError(f"CI route mismatch: {mismatches}; all results={actual}")


def self_test() -> None:
    passing = [
        (
            "pull_request",
            False,
            {
                "frontend": "success",
                "backend": "success",
                "admin_rotation": "success",
                "signed_release": "skipped",
                "fresh_install": "skipped",
            },
        ),
        (
            "pull_request",
            True,
            {
                "frontend": "skipped",
                "backend": "skipped",
                "admin_rotation": "skipped",
                "signed_release": "skipped",
                "fresh_install": "skipped",
            },
        ),
        (
            "push",
            False,
            {
                "frontend": "skipped",
                "backend": "skipped",
                "admin_rotation": "skipped",
                "signed_release": "success",
                "fresh_install": "success",
            },
        ),
        (
            "push",
            True,
            {
                "frontend": "skipped",
                "backend": "skipped",
                "admin_rotation": "skipped",
                "signed_release": "skipped",
                "fresh_install": "skipped",
            },
        ),
    ]
    for event, docs_only, actual in passing:
        actual["direct_fpv9"] = "skipped"
        actual["managed_fpv11"] = "skipped"
        actual["p3_component"] = "skipped" if docs_only else "success"
        classification = "safe-docs-only" if docs_only else "release-relevant"
        validate(event, docs_only, classification, "success", actual)

    rejected = [
        ("pull_request", False, "release-relevant", "failure", passing[0][2]),
        (
            "pull_request",
            False,
            "release-relevant",
            "success",
            {
                "frontend": "skipped",
                "backend": "success",
                "admin_rotation": "success",
                "signed_release": "skipped",
                "fresh_install": "skipped",
            },
        ),
        (
            "push",
            False,
            "release-relevant",
            "success",
            {
                "frontend": "skipped",
                "backend": "skipped",
                "admin_rotation": "skipped",
                "signed_release": "success",
                "fresh_install": "skipped",
            },
        ),
        ("pull_request", True, "release-relevant", "success", passing[1][2]),
    ]
    for event, docs_only, classification, changes_result, actual in rejected:
        actual["direct_fpv9"] = "skipped"
        actual["managed_fpv11"] = "skipped"
        try:
            validate(event, docs_only, classification, changes_result, actual)
        except ValueError:
            continue
        raise AssertionError(
            f"expected route rejection: event={event}, docs_only={docs_only}, actual={actual}"
        )
    route_cases = [
        ("pull_request", False, True, "0.2.0-dev.fpv11", True),
        ("pull_request", True, True, "0.2.0-dev.fpv11", False),
        ("pull_request", False, False, "0.2.0-dev.fpv11", False),
        ("pull_request", False, True, "0.2.0-dev.fpv12", False),
        ("push", False, True, "0.2.0-dev.fpv11", False),
    ]
    for event, docs, same_repo, version, required in route_cases:
        assert direct_fpv9_required(event, docs, same_repo, version) == required
    qualified = dict(passing[0][2], direct_fpv9="success")
    validate("pull_request", False, "release-relevant", "success", qualified, True)
    for result in ("skipped", "failure", "cancelled"):
        try:
            validate("pull_request", False, "release-relevant", "success",
                     dict(qualified, direct_fpv9=result), True)
        except ValueError:
            continue
        raise AssertionError(f"required qualification incorrectly accepted {result}")
    try:
        validate("pull_request", False, "release-relevant", "success", qualified, False)
    except ValueError:
        pass
    else:
        raise AssertionError("unexpected qualification was accepted outside its route")
    for event, docs, same_repo, version, required in route_cases:
        assert managed_fpv11_required(event, docs, same_repo, version) == (event == "pull_request" and not docs and same_repo and version == "0.2.0-dev.fpv12")
    managed = dict(passing[0][2], managed_fpv11="success")
    validate("pull_request", False, "release-relevant", "success", managed, False, True)
    for result in ("skipped", "failure", "cancelled"):
        try:
            validate("pull_request", False, "release-relevant", "success", dict(managed, managed_fpv11=result), False, True)
        except ValueError:
            continue
        raise AssertionError(f"required managed qualification accepted {result}")
    assert current_release_required("pull_request", False, True)
    assert current_release_required("pull_request", False, False)
    assert not current_release_required("pull_request", True, True)
    controller = dict(passing[0][2], signed_release="success", fresh_install="success")
    validate("pull_request", False, "release-relevant", "success", controller, controller_required=True)
    validate("pull_request", False, "release-relevant", "success", dict(controller, direct_fpv9="success"), direct_required=True, controller_required=True)
    for same_repository in (True, False):
        required = current_release_required("pull_request", False, same_repository)
        for job in ("frontend", "backend", "admin_rotation", "signed_release", "fresh_install", "p3_component"):
            for result in ("skipped", "failure", "cancelled"):
                try:
                    validate("pull_request", False, "release-relevant", "success",
                             dict(controller, **{job: result}), controller_required=required)
                except ValueError:
                    continue
                raise AssertionError(f"required current qualification accepted {job}={result}, same_repository={same_repository}")
    print("ci-contract-gate self-test: fork, same-repository and main routes passed")


def parse_bool(value: str) -> bool:
    if value not in {"true", "false"}:
        raise ValueError(f"expected true or false, got {value!r}")
    return value == "true"


def run_from_environment() -> None:
    actual = {
        "frontend": os.environ["FRONTEND_RESULT"],
        "backend": os.environ["BACKEND_RESULT"],
        "admin_rotation": os.environ["ADMIN_ROTATION_RESULT"],
        "signed_release": os.environ["SIGNED_RELEASE_RESULT"],
        "fresh_install": os.environ["FRESH_INSTALL_RESULT"],
        "direct_fpv9": os.environ["DIRECT_FPV9_RESULT"],
        "managed_fpv11": os.environ["MANAGED_FPV11_RESULT"],
        "p3_component": os.environ["P3_COMPONENT_RESULT"],
    }
    required = qualification_route_from_environment()
    if parse_bool(os.environ["DIRECT_FPV9_REQUIRED"]) != required:
        raise ValueError("reported FPV9 qualification route differs from event and release metadata")
    managed_required = qualification_route_from_environment(managed=True)
    if parse_bool(os.environ["MANAGED_FPV11_REQUIRED"]) != managed_required:
        raise ValueError("reported managed FPV11 route differs from event and release metadata")
    controller_required = current_release_route_from_environment()
    if parse_bool(os.environ["CURRENT_RELEASE_REQUIRED"]) != controller_required:
        raise ValueError("reported current release route differs from event classification")
    validate(
        os.environ["EVENT_NAME"],
        parse_bool(os.environ["SAFE_DOCS_ONLY"]),
        os.environ["CHANGE_CLASSIFICATION"],
        os.environ["CHANGES_RESULT"],
        actual,
        required,
        managed_required,
        controller_required,
    )
    classification = os.environ["CHANGE_CLASSIFICATION"]
    print(f"classification={classification}")
    print(f"validated_results={actual}")

    summary_path = os.environ.get("GITHUB_STEP_SUMMARY")
    if summary_path:
        with pathlib.Path(summary_path).open("a", encoding="utf-8") as summary:
            summary.write("### CI aggregate gate\n\n")
            summary.write(f"- Classification: `{classification}`\n")
            summary.write(f"- Validated results: `{actual}`\n")


def main() -> int:
    parser = argparse.ArgumentParser()
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument("--self-test", action="store_true")
    modes.add_argument("--write-direct-fpv9-route", action="store_true")
    args = parser.parse_args()
    if args.self_test:
        self_test()
    elif args.write_direct_fpv9_route:
        required = str(qualification_route_from_environment()).lower()
        with pathlib.Path(os.environ["GITHUB_OUTPUT"]).open("a", encoding="utf-8") as output:
            output.write(f"direct_fpv9_required={required}\n")
            output.write(f"managed_fpv11_required={str(qualification_route_from_environment(managed=True)).lower()}\n")
            output.write(f"current_release_required={str(current_release_route_from_environment()).lower()}\n")
        print(f"direct_fpv9_required={required}")
    else:
        run_from_environment()
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (KeyError, OSError, ValueError) as error:
        raise SystemExit(f"ci-contract-gate: {error}") from error
