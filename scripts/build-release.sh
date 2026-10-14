#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${1:-dist}"

case "$OUT_DIR" in
  /*) ;;
  *) OUT_DIR="$ROOT/$OUT_DIR" ;;
esac

cd "$ROOT"

COMMIT="$(git rev-parse HEAD)"
if [ "${XINSIGHT_RELEASE_ALLOW_DIRTY:-0}" != "1" ]; then
  if [ -n "$(git status --porcelain)" ]; then
    echo "working tree is dirty; set XINSIGHT_RELEASE_ALLOW_DIRTY=1 for local throwaway builds" >&2
    exit 1
  fi
fi

GO_VERSION="$(go env GOVERSION)"
EXPECTED_GO_VERSION="${XINSIGHT_EXPECT_GO_VERSION:-go1.25.12}"
if [ "$GO_VERSION" != "$EXPECTED_GO_VERSION" ]; then
  echo "Go toolchain mismatch: got $GO_VERSION, expected $EXPECTED_GO_VERSION" >&2
  exit 1
fi
SOURCE_DATE_EPOCH="$(git show -s --format=%ct "$COMMIT")"
TARGET_OS="linux"
TARGET_ARCH="amd64"
RELEASE_NAME="xinsight-release-${TARGET_OS}-${TARGET_ARCH}-${COMMIT}"
STAGE_DIR="$OUT_DIR/$RELEASE_NAME"
ARCHIVE_PATH="$OUT_DIR/$RELEASE_NAME.tar.gz"

if [ -e "$STAGE_DIR" ]; then
  echo "release staging directory already exists: $STAGE_DIR" >&2
  exit 1
fi
if [ -e "$ARCHIVE_PATH" ]; then
  echo "release archive already exists: $ARCHIVE_PATH" >&2
  exit 1
fi

python3 scripts/collect-third-party-notices.py --check

mkdir -p "$STAGE_DIR/bin"
cp LICENSE NOTICE THIRD_PARTY_NOTICES.md "$STAGE_DIR/"
cp -R licenses "$STAGE_DIR/licenses"

CGO_ENABLED=0 GOOS="$TARGET_OS" GOARCH="$TARGET_ARCH" \
  go build -trimpath -buildvcs=false -ldflags="-buildid=" \
  -o "$STAGE_DIR/bin/xinsight-server" ./cmd/xinsight-server

CGO_ENABLED=0 GOOS="$TARGET_OS" GOARCH="$TARGET_ARCH" \
  go build -trimpath -buildvcs=false -ldflags="-buildid=" \
  -o "$STAGE_DIR/bin/xinsight-importer" ./cmd/xinsight-importer

mkdir -p "$STAGE_DIR/migrations" "$STAGE_DIR/schemas" "$STAGE_DIR/fixtures"
while IFS= read -r -d '' tracked_file; do
  mkdir -p "$STAGE_DIR/$(dirname "$tracked_file")"
  cp "$tracked_file" "$STAGE_DIR/$tracked_file"
done < <(git ls-files -z migrations schemas fixtures)

export ROOT
export COMMIT
export GO_VERSION
export SOURCE_DATE_EPOCH
export TARGET_OS
export TARGET_ARCH
export RELEASE_NAME
export STAGE_DIR
export ARCHIVE_PATH

python3 - <<'PY'
import gzip
import hashlib
import json
import os
import pathlib
import platform
import tarfile

stage = pathlib.Path(os.environ["STAGE_DIR"])
archive_path = pathlib.Path(os.environ["ARCHIVE_PATH"])
release_name = os.environ["RELEASE_NAME"]
source_date_epoch = int(os.environ["SOURCE_DATE_EPOCH"])

component_roots = ["bin", "migrations", "schemas", "fixtures", "licenses"]
legal_files = ["LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md"]
required_files = {
    "bin/xinsight-server",
    "bin/xinsight-importer",
}


def sha256_bytes(data: bytes) -> str:
    return "sha256:" + hashlib.sha256(data).hexdigest()


def rel_files(root: pathlib.Path) -> list[pathlib.Path]:
    return sorted((path for path in root.rglob("*") if path.is_file()), key=lambda p: p.as_posix())


def file_entry(path: pathlib.Path) -> dict:
    rel = path.relative_to(stage).as_posix()
    if ".git" in path.parts:
        raise SystemExit(f"refusing to package .git path: {rel}")
    data = path.read_bytes()
    return {
        "path": rel,
        "sha256": sha256_bytes(data),
        "size_bytes": len(data),
    }


component_entries: list[dict] = [file_entry(stage / name) for name in legal_files]
for component_root in component_roots:
    root = stage / component_root
    if not root.exists():
        raise SystemExit(f"missing release component directory: {component_root}")
    component_entries.extend(file_entry(path) for path in rel_files(root))

component_paths = {entry["path"] for entry in component_entries}
missing_required = sorted(required_files - component_paths)
if missing_required:
    raise SystemExit(f"missing required release files: {missing_required}")

for component_root in component_roots:
    if not any(path == component_root or path.startswith(component_root + "/") for path in component_paths):
        raise SystemExit(f"empty release component: {component_root}")

manifest = {
    "schema_version": "xinsight.release-manifest.v1",
    "project": "x-insight",
    "commit": os.environ["COMMIT"],
    "source_date_epoch": source_date_epoch,
    "toolchain": {
        "go_version": os.environ["GO_VERSION"],
    },
    "target": {
        "os": os.environ["TARGET_OS"],
        "arch": os.environ["TARGET_ARCH"],
        "cgo_enabled": False,
    },
    "builder": {
        "os": platform.system().lower(),
        "arch": platform.machine(),
    },
    "layout": {
        "top_level": component_roots + legal_files + ["release-manifest.json", "SHA256SUMS"],
    },
    "files": component_entries,
}

manifest_path = stage / "release-manifest.json"
manifest_path.write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")

checksum_paths = sorted(
    (path for path in stage.rglob("*") if path.is_file() and path.name != "SHA256SUMS"),
    key=lambda p: p.relative_to(stage).as_posix(),
)
checksum_lines = []
for path in checksum_paths:
    rel = path.relative_to(stage).as_posix()
    digest = hashlib.sha256(path.read_bytes()).hexdigest()
    checksum_lines.append(f"{digest}  {rel}\n")
(stage / "SHA256SUMS").write_text("".join(checksum_lines), encoding="utf-8")

for path in stage.rglob("*"):
    if path.is_dir():
        path.chmod(0o755)
    elif path.relative_to(stage).as_posix().startswith("bin/"):
        path.chmod(0o755)
    else:
        path.chmod(0o644)

archive_path.parent.mkdir(parents=True, exist_ok=True)
with archive_path.open("wb") as raw_file:
    with gzip.GzipFile(filename="", mode="wb", fileobj=raw_file, mtime=source_date_epoch) as gzip_file:
        with tarfile.open(fileobj=gzip_file, mode="w") as tar:
            root_info = tarfile.TarInfo(release_name)
            root_info.type = tarfile.DIRTYPE
            root_info.uid = 0
            root_info.gid = 0
            root_info.uname = "root"
            root_info.gname = "root"
            root_info.mtime = source_date_epoch
            root_info.mode = 0o755
            tar.addfile(root_info)

            package_paths = sorted(
                stage.rglob("*"),
                key=lambda p: p.relative_to(stage).as_posix(),
            )
            for path in package_paths:
                rel = path.relative_to(stage).as_posix()
                info = tar.gettarinfo(str(path), arcname=f"{release_name}/{rel}")
                info.uid = 0
                info.gid = 0
                info.uname = "root"
                info.gname = "root"
                info.mtime = source_date_epoch
                if path.is_dir():
                    info.mode = 0o755
                    tar.addfile(info)
                elif path.is_file():
                    info.mode = 0o755 if rel.startswith("bin/") else 0o644
                    with path.open("rb") as file_obj:
                        tar.addfile(info, file_obj)
                else:
                    raise SystemExit(f"unsupported release path type: {rel}")

archive_hash = sha256_bytes(archive_path.read_bytes())
manifest_hash = sha256_bytes(manifest_path.read_bytes())
print(f"release_archive={archive_path}")
print(f"artifact_sha256={archive_hash}")
print(f"manifest_sha256={manifest_hash}")
print(f"release_name={release_name}")
PY
