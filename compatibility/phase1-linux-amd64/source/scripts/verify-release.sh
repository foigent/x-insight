#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: scripts/verify-release.sh path/to/xinsight-release-linux-amd64-<commit>.tar.gz" >&2
  exit 2
fi

ARCHIVE_PATH="$1"

python3 - "$ARCHIVE_PATH" <<'PY'
import hashlib
import json
import os
import pathlib
import stat
import sys
import tarfile

archive = pathlib.Path(sys.argv[1]).resolve()
if not archive.is_file():
    raise SystemExit(f"release archive not found: {archive}")


def sha256_bytes(data: bytes) -> str:
    return "sha256:" + hashlib.sha256(data).hexdigest()


artifact_sha256 = sha256_bytes(archive.read_bytes())
work_root = pathlib.Path(os.environ.get("RUNNER_TEMP", "/tmp")) / f"xinsight-release-verify-{archive.stem}-{os.getpid()}"
if work_root.exists():
    raise SystemExit(f"verification directory already exists: {work_root}")
work_root.mkdir(parents=True)

with tarfile.open(archive, "r:gz") as tar:
    members = tar.getmembers()
    if not members:
        raise SystemExit("release archive is empty")
    top_dirs = set()
    for member in members:
        parts = pathlib.PurePosixPath(member.name).parts
        if not parts or pathlib.PurePosixPath(member.name).is_absolute() or ".." in parts:
            raise SystemExit(f"unsafe archive path: {member.name}")
        if ".git" in parts:
            raise SystemExit(f"forbidden .git path in archive: {member.name}")
        top_dirs.add(parts[0])
        if not (member.isfile() or member.isdir()):
            raise SystemExit(f"unsupported archive member type: {member.name}")
    if len(top_dirs) != 1:
        raise SystemExit(f"archive must contain exactly one top-level release directory, got {sorted(top_dirs)}")
    member_names = [member.name.rstrip("/") for member in members]
    if len(member_names) != len(set(member_names)):
        raise SystemExit("archive contains duplicate member paths")
    release_name = next(iter(top_dirs))
    tar.extractall(work_root)

release_dir = work_root / release_name
manifest_path = release_dir / "release-manifest.json"
checksums_path = release_dir / "SHA256SUMS"
if not manifest_path.is_file():
    raise SystemExit("missing release-manifest.json")
if not checksums_path.is_file():
    raise SystemExit("missing SHA256SUMS")

manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
if manifest.get("schema_version") != "xinsight.release-manifest.v1":
    raise SystemExit(f"unexpected manifest schema: {manifest.get('schema_version')}")
if manifest.get("project") != "x-insight":
    raise SystemExit(f"unexpected project: {manifest.get('project')}")

target = manifest.get("target", {})
if target.get("os") != "linux" or target.get("arch") != "amd64" or target.get("cgo_enabled") is not False:
    raise SystemExit(f"unexpected target: {target}")

go_version = manifest.get("toolchain", {}).get("go_version")
if not isinstance(go_version, str) or not go_version.startswith("go"):
    raise SystemExit(f"missing or invalid Go toolchain identity: {go_version}")

expected_go_version = os.environ.get("XINSIGHT_EXPECT_GO_VERSION")
if expected_go_version and go_version != expected_go_version:
    raise SystemExit(f"manifest Go version {go_version} does not match expected {expected_go_version}")

expected_commit = os.environ.get("XINSIGHT_EXPECT_COMMIT")
if expected_commit and manifest.get("commit") != expected_commit:
    raise SystemExit(f"manifest commit {manifest.get('commit')} does not match expected {expected_commit}")

actual_files = sorted(path.relative_to(release_dir).as_posix() for path in release_dir.rglob("*") if path.is_file())
manifest_files = sorted(entry["path"] for entry in manifest.get("files", []))
expected_files = sorted(manifest_files + ["release-manifest.json", "SHA256SUMS"])
if actual_files != expected_files:
    raise SystemExit(
        "release file list mismatch\n"
        f"actual={actual_files}\n"
        f"expected={expected_files}"
    )

expected_archive_files = {f"{release_name}/{path}" for path in expected_files}
actual_archive_files = {member.name.rstrip("/") for member in members if member.isfile()}
if actual_archive_files != expected_archive_files:
    raise SystemExit(
        "archive file entries mismatch\n"
        f"actual={sorted(actual_archive_files)}\n"
        f"expected={sorted(expected_archive_files)}"
    )

expected_archive_dirs = {release_name}
for rel in expected_files:
    parent = pathlib.PurePosixPath(rel).parent
    while parent != pathlib.PurePosixPath("."):
        expected_archive_dirs.add(f"{release_name}/{parent.as_posix()}")
        parent = parent.parent
actual_archive_dirs = {member.name.rstrip("/") for member in members if member.isdir()}
if actual_archive_dirs != expected_archive_dirs:
    raise SystemExit(
        "archive directory entries mismatch\n"
        f"actual={sorted(actual_archive_dirs)}\n"
        f"expected={sorted(expected_archive_dirs)}"
    )

source_date_epoch = manifest.get("source_date_epoch")
if not isinstance(source_date_epoch, int):
    raise SystemExit(f"missing or invalid source_date_epoch: {source_date_epoch}")
for member in members:
    name = member.name.rstrip("/")
    if member.uid != 0 or member.gid != 0 or member.uname != "root" or member.gname != "root":
        raise SystemExit(f"unexpected archive ownership metadata for {name}")
    if member.mtime != source_date_epoch:
        raise SystemExit(f"unexpected archive mtime for {name}: {member.mtime}")
    rel = pathlib.PurePosixPath(name).relative_to(release_name).as_posix()
    expected_mode = 0o755 if member.isdir() or rel.startswith("bin/") else 0o644
    if member.mode != expected_mode:
        raise SystemExit(f"unexpected archive mode for {name}: {oct(member.mode)}")

for path in release_dir.rglob("*"):
    rel = path.relative_to(release_dir).as_posix()
    expected_mode = 0o755 if path.is_dir() or rel.startswith("bin/") else 0o644
    actual_mode = stat.S_IMODE(path.stat().st_mode)
    if actual_mode != expected_mode:
        raise SystemExit(f"unexpected extracted mode for {rel}: {oct(actual_mode)}")

required_paths = {
    "bin/xinsight-server",
    "bin/xinsight-importer",
    "release-manifest.json",
    "SHA256SUMS",
}
missing_required = sorted(required_paths - set(actual_files))
if missing_required:
    raise SystemExit(f"missing required files: {missing_required}")

for required_dir in ["migrations", "schemas", "fixtures"]:
    if not any(path.startswith(required_dir + "/") for path in actual_files):
        raise SystemExit(f"release component directory has no files: {required_dir}")

for path in actual_files:
    parts = pathlib.PurePosixPath(path).parts
    if ".git" in parts or path.startswith("docs/") or "UniRouter-X-Insight Cooperation" in path:
        raise SystemExit(f"forbidden path in release: {path}")

for entry in manifest.get("files", []):
    rel = entry["path"]
    path = release_dir / rel
    data = path.read_bytes()
    if entry.get("sha256") != sha256_bytes(data):
        raise SystemExit(f"manifest hash mismatch for {rel}")
    if entry.get("size_bytes") != len(data):
        raise SystemExit(f"manifest size mismatch for {rel}")

checksum_entries = {}
for line_no, line in enumerate(checksums_path.read_text(encoding="utf-8").splitlines(), start=1):
    if not line.strip():
        continue
    try:
        digest, rel = line.split("  ", 1)
    except ValueError as exc:
        raise SystemExit(f"invalid SHA256SUMS line {line_no}: {line}") from exc
    checksum_entries[rel] = digest

expected_checksum_paths = sorted(path for path in actual_files if path != "SHA256SUMS")
if sorted(checksum_entries) != expected_checksum_paths:
    raise SystemExit(
        "SHA256SUMS file list mismatch\n"
        f"actual={sorted(checksum_entries)}\n"
        f"expected={expected_checksum_paths}"
    )
for rel, digest in checksum_entries.items():
    actual_digest = hashlib.sha256((release_dir / rel).read_bytes()).hexdigest()
    if actual_digest != digest:
        raise SystemExit(f"SHA256SUMS mismatch for {rel}")

text_forbidden = [
    "postgres://",
    "mysql://",
    "mongodb://",
    "redis://",
    "BEGIN RSA PRIVATE KEY",
    "BEGIN OPENSSH PRIVATE KEY",
    "BEGIN PRIVATE KEY",
]
for rel in actual_files:
    if rel.startswith("bin/"):
        continue
    data = (release_dir / rel).read_bytes()
    text = data.decode("utf-8", errors="ignore")
    for needle in text_forbidden:
        if needle in text:
            raise SystemExit(f"forbidden secret/DSN-looking content in {rel}: {needle}")

manifest_sha256 = sha256_bytes(manifest_path.read_bytes())
print(f"archive={archive}")
print(f"release_name={release_name}")
print(f"commit={manifest.get('commit')}")
print(f"artifact_sha256={artifact_sha256}")
print(f"manifest_sha256={manifest_sha256}")
print(f"file_count={len(actual_files)}")
print(f"verified_extract_dir={work_root}")
PY
