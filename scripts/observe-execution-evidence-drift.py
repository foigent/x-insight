#!/usr/bin/env python3
"""Read-only observer for UniRouter Execution Evidence authority and drift."""

from __future__ import annotations

import argparse
import base64
import datetime as dt
import hashlib
import json
import os
import pathlib
import re
import subprocess
import sys
import tempfile
import urllib.error
import urllib.parse
import urllib.request
from typing import Any


AUTHORITY_PATHS = (
    ".well-known/execution-evidence-authority.json",
    "contracts/execution-evidence/AUTHORITY.json",
    "contracts/source-evidence/v1/execution-evidence-authority.json",
)
UNRELEASED_PATHS = (
    "contracts/source-evidence/v1/unirouter.execution-evidence-v2.compatibility-pin.json",
    "internal/endpointprotocol/artifacts/manifests/anthropic-messages-text-tools.v3.json",
    "internal/endpointprotocol/artifacts/manifests/openai-responses-text.v2.json",
    "internal/executionevidence/public_evidence.go",
)


class ObserverError(ValueError):
    pass


class ObserverAccessError(ObserverError):
    def __init__(self, message: str, http_status: int | None = None) -> None:
        super().__init__(message)
        self.http_status = http_status


class ObserverTransportError(ObserverError):
    def __init__(self, message: str, http_status: int | None = None) -> None:
        super().__init__(message)
        self.http_status = http_status


class ContractObservationError(ObserverError):
    pass


def sha256_bytes(body: bytes) -> str:
    return "sha256:" + hashlib.sha256(body).hexdigest()


class GitHubAPI:
    def __init__(self, repository: str, token: str | None = None) -> None:
        self.repository = repository
        self.token = token

    def _get(self, suffix: str, *, missing_ok: bool = False) -> Any:
        url = f"https://api.github.com/repos/{self.repository}{suffix}"
        headers = {
            "Accept": "application/vnd.github+json",
            "X-GitHub-Api-Version": "2022-11-28",
            "User-Agent": "xinsight-execution-evidence-drift-observer/1",
        }
        if self.token:
            headers["Authorization"] = f"Bearer {self.token}"
        request = urllib.request.Request(url, headers=headers)
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                try:
                    return json.load(response)
                except (json.JSONDecodeError, UnicodeDecodeError) as error:
                    raise ObserverTransportError(
                        "GitHub API returned an invalid response"
                    ) from error
        except urllib.error.HTTPError as error:
            if missing_ok and error.code == 404:
                return None
            if error.code >= 500:
                raise ObserverTransportError(
                    "GitHub API was unavailable", error.code
                ) from error
            raise ObserverAccessError(
                "GitHub repository content was not accessible", error.code
            ) from error
        except (urllib.error.URLError, TimeoutError) as error:
            raise ObserverTransportError("GitHub API transport failed") from error

    def repository_metadata(self) -> dict[str, Any]:
        return self._get("")

    def commit(self, ref: str) -> dict[str, str]:
        encoded = urllib.parse.quote(ref, safe="")
        payload = self._get(f"/commits/{encoded}")
        return {"commit": payload["sha"], "tree": payload["commit"]["tree"]["sha"]}

    def content_access_probe(self, tree: str) -> None:
        self._get(f"/git/trees/{urllib.parse.quote(tree, safe='')}")

    def file(self, path: str, ref: str, *, missing_ok: bool = False) -> bytes | None:
        encoded_path = "/".join(urllib.parse.quote(part, safe="") for part in path.split("/"))
        encoded_ref = urllib.parse.quote(ref, safe="")
        payload = self._get(
            f"/contents/{encoded_path}?ref={encoded_ref}", missing_ok=missing_ok
        )
        if payload is None:
            return None
        if payload.get("type") != "file" or payload.get("encoding") != "base64":
            raise ContractObservationError(
                f"GitHub content is not a base64 file: {path}@{ref}"
            )
        try:
            return base64.b64decode(payload["content"], validate=False)
        except (KeyError, ValueError) as error:
            raise ContractObservationError(
                f"invalid GitHub content encoding: {path}@{ref}"
            ) from error

    def releases(self) -> list[dict[str, Any]]:
        releases: list[dict[str, Any]] = []
        page = 1
        while True:
            batch = self._get(f"/releases?per_page=100&page={page}")
            releases.extend(batch)
            if len(batch) < 100:
                return releases
            page += 1


class SnapshotAPI:
    def __init__(self, snapshot: dict[str, Any]) -> None:
        self.snapshot = snapshot

    def repository_metadata(self) -> dict[str, Any]:
        return self.snapshot["repository"]

    def commit(self, ref: str) -> dict[str, str]:
        if ref in {"main", self.snapshot["repository"].get("default_branch")}:
            return self.snapshot["main_identity"]
        try:
            return self.snapshot["ref_identities"][ref]
        except KeyError as error:
            raise ObserverError(f"snapshot ref is unresolvable: {ref}") from error

    def content_access_probe(self, tree: str) -> None:
        known_trees = {self.snapshot["main_identity"]["tree"]}
        known_trees.update(
            identity["tree"] for identity in self.snapshot["ref_identities"].values()
        )
        if tree not in known_trees:
            raise ObserverError(f"snapshot tree is unresolvable: {tree}")

    def file(self, path: str, ref: str, *, missing_ok: bool = False) -> bytes | None:
        key = f"{ref}:{path}"
        files = self.snapshot.get("files", {})
        if key not in files:
            if missing_ok:
                return None
            raise ObserverError(f"snapshot file is unresolvable: {key}")
        return files[key].encode("utf-8")

    def releases(self) -> list[dict[str, Any]]:
        return self.snapshot.get("releases", [])


class GitHubCLI:
    """Use the already-authenticated gh CLI without exposing its token."""

    def __init__(self, repository: str) -> None:
        self.repository = repository

    def _get(self, suffix: str, *, missing_ok: bool = False) -> Any:
        completed = subprocess.run(
            ["gh", "api", f"repos/{self.repository}{suffix}"],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        if completed.returncode != 0:
            if missing_ok and "HTTP 404" in completed.stderr:
                return None
            match = re.search(r"HTTP ([0-9]{3})", completed.stderr)
            status = int(match.group(1)) if match is not None else None
            if status is not None and status < 500:
                raise ObserverAccessError("gh repository access failed", status)
            raise ObserverTransportError("gh API transport failed", status)
        try:
            return json.loads(completed.stdout)
        except json.JSONDecodeError as error:
            raise ObserverTransportError("gh API returned invalid JSON") from error

    def repository_metadata(self) -> dict[str, Any]:
        return self._get("")

    def commit(self, ref: str) -> dict[str, str]:
        encoded = urllib.parse.quote(ref, safe="")
        payload = self._get(f"/commits/{encoded}")
        return {"commit": payload["sha"], "tree": payload["commit"]["tree"]["sha"]}

    def content_access_probe(self, tree: str) -> None:
        self._get(f"/git/trees/{urllib.parse.quote(tree, safe='')}")

    def file(self, path: str, ref: str, *, missing_ok: bool = False) -> bytes | None:
        encoded_path = "/".join(urllib.parse.quote(part, safe="") for part in path.split("/"))
        encoded_ref = urllib.parse.quote(ref, safe="")
        payload = self._get(
            f"/contents/{encoded_path}?ref={encoded_ref}", missing_ok=missing_ok
        )
        if payload is None:
            return None
        if payload.get("type") != "file" or payload.get("encoding") != "base64":
            raise ContractObservationError(
                f"GitHub content is not a base64 file: {path}@{ref}"
            )
        try:
            return base64.b64decode(payload["content"], validate=False)
        except (KeyError, ValueError) as error:
            raise ContractObservationError(
                f"invalid GitHub content encoding: {path}@{ref}"
            ) from error

    def releases(self) -> list[dict[str, Any]]:
        releases: list[dict[str, Any]] = []
        page = 1
        while True:
            batch = self._get(f"/releases?per_page=100&page={page}")
            releases.extend(batch)
            if len(batch) < 100:
                return releases
            page += 1


def decode_json(body: bytes, label: str) -> dict[str, Any]:
    try:
        value = json.loads(body)
    except json.JSONDecodeError as error:
        raise ContractObservationError(f"invalid JSON for {label}: {error}") from error
    if not isinstance(value, dict):
        raise ContractObservationError(f"{label} must be a JSON object")
    return value


def discover_authority(
    api, main_identity: dict[str, str]
) -> tuple[
    str,
    dict[str, Any] | None,
    str | None,
    list[str],
    dict[str, str] | None,
]:
    attempted_paths: list[str] = []
    for path in AUTHORITY_PATHS:
        attempted_paths.append(path)
        body = api.file(path, main_identity["commit"], missing_ok=True)
        if body is None:
            continue
        try:
            descriptor = decode_json(body, path)
            required = {
                "authority_version",
                "mechanism",
                "manifest_path",
                "eligible_tag_pattern",
            }
            if not required.issubset(descriptor):
                missing = sorted(required - set(descriptor))
                raise ContractObservationError(
                    f"authority descriptor missing {missing}"
                )
            if descriptor["mechanism"] != "github_release_tagged_manifest":
                raise ContractObservationError("unsupported authority mechanism")
            re.compile(descriptor["eligible_tag_pattern"])
            return "resolved", descriptor, path, attempted_paths, None
        except (ContractObservationError, re.error, TypeError):
            return (
                "declared_but_unresolvable",
                None,
                path,
                attempted_paths,
                {"stage": "authority_descriptor", "path": path},
            )
    return "undefined", None, None, attempted_paths, None


def eligible_release(
    releases: list[dict[str, Any]], descriptor: dict[str, Any]
) -> dict[str, Any] | None:
    pattern = re.compile(descriptor["eligible_tag_pattern"])
    eligible = [
        release
        for release in releases
        if not release.get("draft")
        and not release.get("prerelease")
        and pattern.fullmatch(str(release.get("tag_name", "")))
    ]
    if not eligible:
        return None
    eligible.sort(key=lambda release: (release.get("published_at") or "", release["tag_name"]))
    return eligible[-1]


def resolve_release(api, descriptor: dict[str, Any], release: dict[str, Any]) -> dict[str, Any]:
    tag = release["tag_name"]
    identity = api.commit(tag)
    body = api.file(descriptor["manifest_path"], identity["commit"])
    if body is None:
        raise ContractObservationError("release manifest disappeared")
    manifest = decode_json(body, f"{descriptor['manifest_path']}@{tag}")
    declared_digest = manifest.get("release_digest")
    if declared_digest is not None and not re.fullmatch(
        r"sha256:[0-9a-f]{64}", str(declared_digest)
    ):
        raise ContractObservationError(
            "release manifest declared invalid release_digest"
        )
    return {
        "canonical_repository_url": None,
        "authority_mechanism": descriptor["mechanism"],
        "resolved_tag": tag,
        "resolved_ref": tag,
        "commit": identity["commit"],
        "tree": identity["tree"],
        "manifest_path": descriptor["manifest_path"],
        "declared_release_digest": declared_digest,
        "observed_content_sha256": sha256_bytes(body),
    }


def observe_unreleased(api, identity: dict[str, str], repository_url: str) -> list[dict[str, Any]]:
    observations: list[dict[str, Any]] = []
    for path in UNRELEASED_PATHS:
        body = api.file(path, identity["commit"], missing_ok=True)
        if body is None:
            continue
        observations.append(
            {
                "status": "unreleased_draft_observed",
                "canonical_repository_url": repository_url,
                "commit": identity["commit"],
                "tree": identity["tree"],
                "path": path,
                "observed_content_sha256": sha256_bytes(body),
            }
        )
    return observations


def compare_pin(
    pinned_manifest: dict[str, Any],
    release: dict[str, Any] | None,
    unreleased: list[dict[str, Any]],
) -> tuple[str, str]:
    pinned = {
        observation["path"]: observation["content_sha256"]
        for observation in pinned_manifest["source_context"]["observations"]
    }
    released_status = "not_applicable"
    if release is not None:
        expected = pinned.get(release["manifest_path"])
        released_status = (
            "pinned_contract_matches_latest_release"
            if expected == release["observed_content_sha256"]
            else "latest_released_contract_drift"
        )
    observed = {
        item["path"]: item["observed_content_sha256"] for item in unreleased
    }
    if not observed:
        unreleased_status = "no_unreleased_observation"
    elif all(pinned.get(path) == digest for path, digest in observed.items()):
        unreleased_status = "pinned_unreleased_observations_match"
    else:
        unreleased_status = "unreleased_only_drift_observed"
    return released_status, unreleased_status


def observe(api, pinned_manifest: dict[str, Any], observed_at: str) -> dict[str, Any]:
    repository = api.repository_metadata()
    repository_url = repository["html_url"]
    default_branch = repository.get("default_branch", "main")
    main_identity = api.commit(default_branch)
    api.content_access_probe(main_identity["tree"])
    (
        authority_status,
        descriptor,
        authority_path,
        attempted_paths,
        contract_failure,
    ) = discover_authority(api, main_identity)

    release_status = "no_eligible_release"
    resolved_release = None
    if authority_status == "resolved" and descriptor is not None:
        release = eligible_release(api.releases(), descriptor)
        if release is not None:
            try:
                resolved_release = resolve_release(api, descriptor, release)
                resolved_release["canonical_repository_url"] = repository_url
                release_status = "released_contract_resolved"
            except ContractObservationError:
                authority_status = "declared_but_unresolvable"
                release_status = "eligible_release_unresolvable"
                contract_failure = {
                    "stage": "release_manifest",
                    "path": descriptor["manifest_path"],
                }
    unreleased = observe_unreleased(api, main_identity, repository_url)
    released_drift, unreleased_drift = compare_pin(
        pinned_manifest, resolved_release, unreleased
    )
    return {
        "report_version": "xinsight.execution-evidence-authority-observer.v1",
        "observed_at": observed_at,
        "observation_status": (
            "contract_observation_failed"
            if contract_failure is not None
            else "observation_completed"
        ),
        "repository": {
            "immutable_repository_id": repository["id"],
            "canonical_repository_url": repository_url,
            "discovery_branch": default_branch,
            "resolved_commit": main_identity["commit"],
            "resolved_tree": main_identity["tree"],
            "content_access_status": "verified_by_tree_read",
        },
        "authority_discovery_scope": {"attempted_paths": attempted_paths},
        "authority_status": authority_status,
        "authority_mechanism": (
            descriptor["mechanism"] if descriptor is not None else "undefined"
        ),
        "authority_descriptor_path": authority_path,
        "release_status": release_status,
        "released_contract": resolved_release,
        "unreleased_observations": unreleased,
        "released_drift_status": released_drift,
        "unreleased_drift_status": unreleased_drift,
        "contract_observation_failure": contract_failure,
        "observer_side_effects": "none_read_only",
        "capability_support_nonclaim": "Observer discovery does not prove runtime, model capability, External Integration, Target, or Production support.",
    }


OBSERVATION_STATUSES = {
    "blocked_token_mint",
    "blocked_observer_access",
    "blocked_observer_transport",
    "contract_observation_failed",
    "observation_completed",
}


def failure_report(
    status: str,
    repository: str,
    observed_at: str,
    *,
    http_status: int | None = None,
    failure_stage: str,
) -> dict[str, Any]:
    if status not in OBSERVATION_STATUSES or status == "observation_completed":
        raise ValueError(f"invalid observation failure status: {status}")
    return {
        "report_version": "xinsight.execution-evidence-authority-observer.v1",
        "observed_at": observed_at,
        "observation_status": status,
        "target_repository": repository,
        "http_status": http_status,
        "failure_stage": failure_stage,
        "authority_discovery_scope": {"attempted_paths": []},
        "observer_side_effects": "none_read_only",
        "capability_support_nonclaim": "Observer failure does not prove contract, runtime, model capability, External Integration, Target, or Production behavior.",
    }


def write_report(path: pathlib.Path, report: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )


def run_observation(
    api, pinned_manifest: dict[str, Any], observed_at: str, repository: str
) -> dict[str, Any]:
    try:
        return observe(api, pinned_manifest, observed_at)
    except ObserverAccessError as error:
        return failure_report(
            "blocked_observer_access",
            repository,
            observed_at,
            http_status=error.http_status,
            failure_stage="repository_access",
        )
    except ObserverTransportError as error:
        return failure_report(
            "blocked_observer_transport",
            repository,
            observed_at,
            http_status=error.http_status,
            failure_stage="repository_transport",
        )
    except ContractObservationError:
        return failure_report(
            "contract_observation_failed",
            repository,
            observed_at,
            failure_stage="remote_contract_content",
        )


def finalize_workflow_report(
    path: pathlib.Path,
    repository: str,
    observed_at: str,
    token_outcome: str | None,
    observer_outcome: str,
    github_output: pathlib.Path | None,
) -> str:
    report: dict[str, Any] | None = None
    if path.is_file():
        try:
            value = json.loads(path.read_text(encoding="utf-8"))
            if isinstance(value, dict):
                report = value
        except (OSError, json.JSONDecodeError):
            report = None

    if token_outcome is not None and token_outcome != "success":
        report = failure_report(
            "blocked_token_mint",
            repository,
            observed_at,
            failure_stage="token_mint",
        )
    elif report is None:
        report = failure_report(
            "blocked_observer_access",
            repository,
            observed_at,
            failure_stage="observer_step_without_structured_report",
        )

    status = report.get("observation_status")
    if status not in OBSERVATION_STATUSES:
        report = failure_report(
            "blocked_observer_access",
            repository,
            observed_at,
            failure_stage="observer_report_status_invalid",
        )
        status = report["observation_status"]
    report["workflow_outcomes"] = {
        "token_mint": token_outcome if token_outcome is not None else "not_required",
        "observer": observer_outcome,
    }
    write_report(path, report)

    if github_output is not None:
        with github_output.open("a", encoding="utf-8") as output:
            output.write(f"observation_status={status}\n")
    return str(status)


def base_snapshot() -> dict[str, Any]:
    return {
        "repository": {
            "id": 1281228065,
            "html_url": "https://github.com/foigent/unirouter",
            "default_branch": "main",
        },
        "main_identity": {"commit": "a" * 40, "tree": "b" * 40},
        "ref_identities": {},
        "files": {
            f"{'a' * 40}:{UNRELEASED_PATHS[0]}": "draft bytes",
        },
        "releases": [],
    }


def pinned_for(body: str = "draft bytes", path: str = UNRELEASED_PATHS[0]) -> dict[str, Any]:
    return {
        "source_context": {
            "observations": [
                {"path": path, "content_sha256": sha256_bytes(body.encode("utf-8"))}
            ]
        }
    }


def self_test() -> None:
    observed_at = "2026-08-31T00:00:00Z"
    undefined = observe(SnapshotAPI(base_snapshot()), pinned_for(), observed_at)
    assert undefined["observation_status"] == "observation_completed"
    assert undefined["authority_status"] == "undefined"
    assert undefined["authority_discovery_scope"] == {
        "attempted_paths": list(AUTHORITY_PATHS)
    }
    assert undefined["release_status"] == "no_eligible_release"
    assert undefined["unreleased_drift_status"] == "pinned_unreleased_observations_match"

    descriptor = {
        "authority_version": "unirouter.execution-evidence-authority.v1",
        "mechanism": "github_release_tagged_manifest",
        "manifest_path": "contracts/execution-evidence/release-manifest.json",
        "eligible_tag_pattern": "execution-evidence-v[0-9]+\\.[0-9]+\\.[0-9]+",
    }
    declared = base_snapshot()
    declared["files"][f"{'a' * 40}:{AUTHORITY_PATHS[0]}"] = json.dumps(descriptor)
    no_release = observe(SnapshotAPI(declared), pinned_for(), observed_at)
    assert no_release["authority_status"] == "resolved"
    assert no_release["release_status"] == "no_eligible_release"

    malformed = base_snapshot()
    malformed["files"][f"{'a' * 40}:{AUTHORITY_PATHS[0]}"] = "{}"
    unresolvable = observe(SnapshotAPI(malformed), pinned_for(), observed_at)
    assert unresolvable["observation_status"] == "contract_observation_failed"
    assert unresolvable["authority_status"] == "declared_but_unresolvable"
    assert unresolvable["authority_discovery_scope"] == {
        "attempted_paths": [AUTHORITY_PATHS[0]]
    }

    released = declared
    tag = "execution-evidence-v1.0.0"
    released["ref_identities"][tag] = {"commit": "c" * 40, "tree": "d" * 40}
    released["releases"] = [
        {
            "tag_name": tag,
            "draft": False,
            "prerelease": False,
            "published_at": "2026-08-30T00:00:00Z",
        }
    ]
    release_body = json.dumps(
        {"release_digest": "sha256:" + "1" * 64}, sort_keys=True
    )
    release_path = descriptor["manifest_path"]
    released["files"][f"{'c' * 40}:{release_path}"] = release_body
    same = observe(
        SnapshotAPI(released), pinned_for(release_body, release_path), observed_at
    )
    assert same["release_status"] == "released_contract_resolved"
    assert same["released_contract"]["commit"] == "c" * 40
    assert same["released_drift_status"] == "pinned_contract_matches_latest_release"

    drift = observe(
        SnapshotAPI(released), pinned_for("different", release_path), observed_at
    )
    assert drift["released_drift_status"] == "latest_released_contract_drift"

    unreleased_drift_snapshot = base_snapshot()
    unreleased_drift_snapshot["files"][f"{'a' * 40}:{UNRELEASED_PATHS[0]}"] = "changed"
    unreleased_drift = observe(
        SnapshotAPI(unreleased_drift_snapshot), pinned_for(), observed_at
    )
    assert unreleased_drift["released_drift_status"] == "not_applicable"
    assert unreleased_drift["unreleased_drift_status"] == "unreleased_only_drift_observed"

    class FailingAPI:
        def __init__(self, error: ObserverError) -> None:
            self.error = error

        def repository_metadata(self) -> dict[str, Any]:
            raise self.error

    access = run_observation(
        FailingAPI(ObserverAccessError("blocked", 404)),
        pinned_for(),
        observed_at,
        "foigent/unirouter",
    )
    assert access["observation_status"] == "blocked_observer_access"
    assert access["http_status"] == 404
    transport = run_observation(
        FailingAPI(ObserverTransportError("blocked")),
        pinned_for(),
        observed_at,
        "foigent/unirouter",
    )
    assert transport["observation_status"] == "blocked_observer_transport"
    assert transport["http_status"] is None

    with tempfile.TemporaryDirectory(prefix="observer-finalizer-") as temporary:
        report_path = pathlib.Path(temporary) / "report.json"
        status = finalize_workflow_report(
            report_path,
            "foigent/unirouter",
            observed_at,
            "failure",
            "skipped",
            None,
        )
        assert status == "blocked_token_mint"
        finalized = json.loads(report_path.read_text(encoding="utf-8"))
        assert finalized["http_status"] is None
        assert finalized["workflow_outcomes"] == {
            "token_mint": "failure",
            "observer": "skipped",
        }
        report_path.unlink()
        assert finalize_workflow_report(report_path, "foigent/unirouter", observed_at,
                                        None, "failure", None) == "blocked_observer_access"
        write_report(report_path, run_observation(SnapshotAPI(base_snapshot()), pinned_for(), observed_at, "foigent/unirouter"))
        assert finalize_workflow_report(report_path, "foigent/unirouter", observed_at,
                                        None, "success", None) == "observation_completed"
        assert json.loads(report_path.read_text())["workflow_outcomes"]["token_mint"] == "not_required"

    print(
        "execution-evidence-drift-observer self-test: "
        "6 authority/drift cases and all 5 workflow states passed"
    )


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--self-test", action="store_true")
    parser.add_argument("--repository", default="foigent/unirouter")
    parser.add_argument("--pinned-manifest", type=pathlib.Path)
    parser.add_argument("--snapshot", type=pathlib.Path)
    parser.add_argument("--transport", choices=("urllib", "gh"), default="urllib")
    parser.add_argument("--output", type=pathlib.Path)
    parser.add_argument("--observed-at")
    parser.add_argument("--finalize-workflow-report", action="store_true")
    parser.add_argument("--token-outcome")
    parser.add_argument("--observer-outcome")
    parser.add_argument("--github-output", type=pathlib.Path)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if args.self_test:
        self_test()
        return 0
    observed_at = args.observed_at or dt.datetime.now(dt.timezone.utc).replace(
        microsecond=0
    ).isoformat().replace("+00:00", "Z")
    if args.finalize_workflow_report:
        if (
            args.output is None
            or args.observer_outcome is None
        ):
            raise ObserverError(
                "workflow finalizer requires --output and --observer-outcome"
            )
        status = finalize_workflow_report(
            args.output,
            args.repository,
            observed_at,
            args.token_outcome,
            args.observer_outcome,
            args.github_output,
        )
        print(json.dumps({"observation_status": status}, sort_keys=True))
        return 0
    if args.pinned_manifest is None or args.output is None:
        raise ObserverError("--pinned-manifest and --output are required")
    pinned_manifest = json.loads(
        args.pinned_manifest.read_text(encoding="utf-8")
    )
    if args.snapshot is not None:
        api = SnapshotAPI(json.loads(args.snapshot.read_text(encoding="utf-8")))
    elif args.transport == "gh":
        api = GitHubCLI(args.repository)
    else:
        api = GitHubAPI(args.repository, os.environ.get("GITHUB_TOKEN"))
    report = run_observation(api, pinned_manifest, observed_at, args.repository)
    write_report(args.output, report)
    print(json.dumps(report, sort_keys=True))
    return 0 if report["observation_status"] == "observation_completed" else 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (ObserverError, OSError, KeyError, TypeError, json.JSONDecodeError) as error:
        raise SystemExit(f"execution-evidence-drift-observer: {error}") from error
