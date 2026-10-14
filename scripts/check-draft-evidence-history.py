#!/usr/bin/env python3
"""Protect committed Draft Evidence fixtures/manifests against history rewrite."""

from __future__ import annotations

import argparse
import hashlib
import json
import pathlib
import subprocess
import sys
import tempfile


CONTRACT_ROOT = pathlib.PurePosixPath(
    "contracts/execution-evidence/incubation/unirouter-consumer-candidate"
)
VERSIONS_ROOT = CONTRACT_ROOT / "versions"
ZERO_SHA = "0" * 40


class HistoryError(ValueError):
    pass


def git(repo: pathlib.Path, *args: str) -> str:
    completed = subprocess.run(
        ["git", "-C", str(repo), *args],
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    return completed.stdout.strip()


def require_commit(repo: pathlib.Path, revision: str, label: str) -> None:
    if revision == ZERO_SHA:
        raise HistoryError(f"{label} predecessor is the all-zero SHA")
    try:
        git(repo, "cat-file", "-e", f"{revision}^{{commit}}")
    except subprocess.CalledProcessError as error:
        raise HistoryError(
            f"{label} predecessor commit is unavailable: {revision}"
        ) from error


def comparison_base(
    repo: pathlib.Path, event: str, base: str, head: str
) -> tuple[str, str]:
    require_commit(repo, head, "head")
    require_commit(repo, base, "push" if event == "push" else "pull_request")
    if event == "pull_request":
        try:
            base = git(repo, "merge-base", base, head)
        except subprocess.CalledProcessError as error:
            raise HistoryError("pull_request merge-base is unavailable") from error
        require_commit(repo, base, "pull_request merge-base")
    return base, head


def changed_status(repo: pathlib.Path, base: str, head: str) -> list[tuple[str, str]]:
    output = git(
        repo,
        "diff",
        "--name-status",
        "--no-renames",
        base,
        head,
        "--",
        str(CONTRACT_ROOT),
    )
    changes: list[tuple[str, str]] = []
    for line in output.splitlines():
        if not line:
            continue
        status, path = line.split("\t", 1)
        changes.append((status, path))
    return changes


def version_existed(repo: pathlib.Path, base: str, candidate: pathlib.PurePosixPath) -> bool:
    relative = candidate.relative_to(VERSIONS_ROOT)
    if len(relative.parts) < 2:
        return True
    version_root = VERSIONS_ROOT / relative.parts[0]
    return bool(git(repo, "ls-tree", "-r", "--name-only", base, "--", str(version_root)))


def validate_immutable_history(
    repo: pathlib.Path, event: str, base: str, head: str
) -> None:
    base, head = comparison_base(repo, event, base, head)
    violations: list[str] = []
    for status, path in changed_status(repo, base, head):
        candidate = pathlib.PurePosixPath(path)
        if not candidate.is_relative_to(VERSIONS_ROOT):
            continue
        if status != "A" or version_existed(repo, base, candidate):
            violations.append(f"{status}\t{path}")
    if violations:
        raise HistoryError(
            "historical Draft fixture/manifest bytes are immutable: "
            + ", ".join(violations)
        )


def sha256_file(path: pathlib.Path) -> str:
    return "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()


def version_key(path: pathlib.Path) -> tuple[int, ...]:
    try:
        return tuple(int(part) for part in path.name.split("."))
    except ValueError as error:
        raise HistoryError(f"invalid Draft version directory: {path.name}") from error


def validate_manifest_chain(repo: pathlib.Path) -> None:
    root = repo / pathlib.Path(str(CONTRACT_ROOT))
    versions_root = root / "versions"
    manifests = sorted(
        versions_root.glob("*/draft-manifest.json"),
        key=lambda path: version_key(path.parent),
    )
    if not manifests:
        raise HistoryError("no versioned Draft manifest exists")
    previous_digest: str | None = None
    for index, manifest_path in enumerate(manifests):
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
        declared = manifest.get("previous_manifest_sha256")
        if index == 0:
            if declared is not None:
                raise HistoryError(
                    "first Draft manifest must omit previous_manifest_sha256"
                )
        elif declared != previous_digest:
            raise HistoryError(
                f"{manifest_path} previous_manifest_sha256={declared!r}, "
                f"expected {previous_digest!r}"
            )
        previous_digest = sha256_file(manifest_path)

    pointer_path = root / "latest.json"
    pointer = json.loads(pointer_path.read_text(encoding="utf-8"))
    manifest_relative = pointer.get("manifest_path")
    if not isinstance(manifest_relative, str):
        raise HistoryError("latest manifest_path is required")
    resolved = (root / manifest_relative).resolve()
    if not resolved.is_relative_to(root.resolve()) or not resolved.is_file():
        raise HistoryError("latest manifest_path is unsafe or missing")
    if pointer.get("manifest_content_sha256") != sha256_file(resolved):
        raise HistoryError("latest manifest_content_sha256 does not match raw bytes")
    if pointer.get("runtime_eligible") is not False:
        raise HistoryError("latest pointer must remain runtime_eligible=false")
    if pointer.get("compatibility_commitment") != "none":
        raise HistoryError("latest pointer must remain compatibility_commitment=none")


def validate(
    repo: pathlib.Path,
    event: str,
    base: str | None = None,
    head: str | None = None,
) -> None:
    validate_manifest_chain(repo)
    if event == "current":
        return
    if base is None or head is None:
        raise HistoryError(f"{event} requires base and head")
    validate_immutable_history(repo, event, base, head)


def commit_all(repo: pathlib.Path, message: str) -> str:
    git(repo, "add", ".")
    git(repo, "commit", "-m", message)
    return git(repo, "rev-parse", "HEAD")


def write_initial(repo: pathlib.Path) -> str:
    version = repo / pathlib.Path(str(VERSIONS_ROOT)) / "0.1.0"
    fixture = version / "fixtures" / "accept" / "a.json"
    fixture.parent.mkdir(parents=True)
    fixture.write_text('{"fixture":"a"}\n', encoding="utf-8")
    manifest = version / "draft-manifest.json"
    manifest.write_text('{"draft_version":"0.1.0-draft"}\n', encoding="utf-8")
    root = repo / pathlib.Path(str(CONTRACT_ROOT))
    (root / "latest.json").write_text(
        json.dumps(
            {
                "manifest_path": "versions/0.1.0/draft-manifest.json",
                "manifest_content_sha256": sha256_file(manifest),
                "runtime_eligible": False,
                "compatibility_commitment": "none",
            },
            sort_keys=True,
        )
        + "\n",
        encoding="utf-8",
    )
    return commit_all(repo, "initial")


def new_repo() -> tuple[tempfile.TemporaryDirectory[str], pathlib.Path, str]:
    temporary = tempfile.TemporaryDirectory(prefix="draft-history-")
    repo = pathlib.Path(temporary.name)
    git(repo, "init", "-q")
    git(repo, "config", "user.email", "draft-history@example.invalid")
    git(repo, "config", "user.name", "Draft History Test")
    initial = write_initial(repo)
    return temporary, repo, initial


def expect_rejected(action, label: str) -> None:
    try:
        action()
    except HistoryError:
        return
    raise AssertionError(f"{label} unexpectedly passed")


def self_test() -> None:
    cases = 0
    for label, mutation in [
        (
            "fixture modification",
            lambda repo: (
                repo
                / pathlib.Path(str(VERSIONS_ROOT))
                / "0.1.0/fixtures/accept/a.json"
            ).write_text('{"fixture":"changed"}\n', encoding="utf-8"),
        ),
        (
            "fixture rename",
            lambda repo: (
                repo
                / pathlib.Path(str(VERSIONS_ROOT))
                / "0.1.0/fixtures/accept/a.json"
            ).rename(
                repo
                / pathlib.Path(str(VERSIONS_ROOT))
                / "0.1.0/fixtures/accept/b.json"
            ),
        ),
        (
            "fixture deletion",
            lambda repo: (
                repo
                / pathlib.Path(str(VERSIONS_ROOT))
                / "0.1.0/fixtures/accept/a.json"
            ).unlink(),
        ),
        (
            "manifest overwrite",
            lambda repo: (
                repo
                / pathlib.Path(str(VERSIONS_ROOT))
                / "0.1.0/draft-manifest.json"
            ).write_text('{"draft_version":"overwritten"}\n', encoding="utf-8"),
        ),
    ]:
        temporary, repo, base = new_repo()
        with temporary:
            mutation(repo)
            head = commit_all(repo, label)
            expect_rejected(
                lambda: validate_immutable_history(repo, "push", base, head), label
            )
            cases += 1

    temporary, repo, base = new_repo()
    with temporary:
        previous = repo / pathlib.Path(str(VERSIONS_ROOT)) / "0.1.0/draft-manifest.json"
        next_version = repo / pathlib.Path(str(VERSIONS_ROOT)) / "0.2.0"
        (next_version / "fixtures/accept").mkdir(parents=True)
        (next_version / "fixtures/accept/b.json").write_text(
            '{"fixture":"b"}\n', encoding="utf-8"
        )
        next_manifest = next_version / "draft-manifest.json"
        next_manifest.write_text(
            json.dumps(
                {
                    "draft_version": "0.2.0-draft",
                    "previous_manifest_sha256": sha256_file(previous),
                },
                sort_keys=True,
            )
            + "\n",
            encoding="utf-8",
        )
        latest = repo / pathlib.Path(str(CONTRACT_ROOT)) / "latest.json"
        latest.write_text(
            json.dumps(
                {
                    "manifest_path": "versions/0.2.0/draft-manifest.json",
                    "manifest_content_sha256": sha256_file(next_manifest),
                    "runtime_eligible": False,
                    "compatibility_commitment": "none",
                },
                sort_keys=True,
            )
            + "\n",
            encoding="utf-8",
        )
        head = commit_all(repo, "new version")
        validate(repo, "push", base, head)
        cases += 1

    temporary, repo, base = new_repo()
    with temporary:
        latest = repo / pathlib.Path(str(CONTRACT_ROOT)) / "latest.json"
        pointer = json.loads(latest.read_text(encoding="utf-8"))
        pointer["observed_at"] = "2026-08-31T00:00:00Z"
        latest.write_text(json.dumps(pointer, sort_keys=True) + "\n", encoding="utf-8")
        head = commit_all(repo, "latest pointer")
        validate(repo, "push", base, head)
        cases += 1

    temporary, repo, head = new_repo()
    with temporary:
        expect_rejected(
            lambda: validate_immutable_history(repo, "push", "f" * 40, head),
            "missing push predecessor",
        )
        cases += 1

    temporary, repo, base = new_repo()
    with temporary:
        git(repo, "checkout", "-q", "-b", "feature")
        addition = (
            repo
            / pathlib.Path(str(VERSIONS_ROOT))
            / "0.1.0/fixtures/accept/new.json"
        )
        addition.write_text('{"fixture":"new"}\n', encoding="utf-8")
        head = commit_all(repo, "pr addition")
        expect_rejected(
            lambda: validate(repo, "pull_request", base, head),
            "addition to historical version",
        )
        cases += 1

    print(f"draft-evidence-history self-test: {cases} cases passed")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--self-test", action="store_true")
    parser.add_argument("--repository-root", type=pathlib.Path, default=pathlib.Path("."))
    parser.add_argument("--event", choices=("current", "pull_request", "push"), default="current")
    parser.add_argument("--base")
    parser.add_argument("--head")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if args.self_test:
        self_test()
        return 0
    validate(args.repository_root.resolve(), args.event, args.base, args.head)
    print(f"draft-evidence-history: {args.event} validation passed")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (HistoryError, OSError, json.JSONDecodeError, subprocess.CalledProcessError) as error:
        raise SystemExit(f"draft-evidence-history: {error}") from error
