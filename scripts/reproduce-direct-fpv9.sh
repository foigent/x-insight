#!/usr/bin/env bash
set -euo pipefail

# Reproduce the closed FPV9 Linux input for both unit and host qualification.
# This creates only a detached source worktree and unsigned program binaries.
if [ "$#" -ne 2 ]; then
  echo "usage: reproduce-direct-fpv9.sh OLD_SOURCE OLD_BIN" >&2
  exit 2
fi
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
old_source="$1"
old_bin="$2"
for destination in "$@"; do
  case "$destination" in /*) ;; *) echo "FPV9 output paths must be absolute" >&2; exit 2 ;; esac
  test ! -e "$destination" && test ! -L "$destination"
done
export CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOTOOLCHAIN=local GOFLAGS=-mod=readonly
test "$(go env GOVERSION)" = go1.25.12
git worktree add --detach "$old_source" 8ee764accbda21f21b489e01a4ab7751571beed4
test "$(git -C "$old_source" rev-parse HEAD)" = 8ee764accbda21f21b489e01a4ab7751571beed4
mkdir -m 0700 "$old_bin"
ldflags='-buildid= -X github.com/foigent/x-insight/internal/buildinfo.ProductVersion=0.2.0-dev.fpv9 -X github.com/foigent/x-insight/internal/buildinfo.Commit=8ee764accbda21f21b489e01a4ab7751571beed4 -X github.com/foigent/x-insight/internal/buildinfo.BuildTime=2026-09-27T06:20:00Z -X github.com/foigent/x-insight/internal/buildinfo.GoVersion=go1.25.12 -X github.com/foigent/x-insight/internal/buildinfo.ManualVersion=0.2.0-dev.fpv9/manual-r1'
for binary in xinsight-server xinsight-importer; do
  (cd "$old_source" && go build -trimpath -buildvcs=false -p 2 -ldflags "$ldflags" -o "$old_bin/$binary" "./cmd/$binary")
done
python3 - "$old_bin" <<'VERIFY'
import hashlib
from pathlib import Path
import sys
root = Path(sys.argv[1])
expected = {
    "xinsight-server": "80573c95149f5b9827b06f264f8d70e436a29ea1d8e93ddb6f1f46a75f4866d7",
    "xinsight-importer": "c6b71ded9aa3fac35c8099aba222e13edb8772a77616f6ab3bb53094cbbcfda8",
}
for name, digest in expected.items():
    actual = hashlib.sha256((root / name).read_bytes()).hexdigest()
    if actual != digest:
        raise SystemExit(f"exact FPV9 digest mismatch: {name}")
    print(f"exact FPV9 digest verified: {name}")
VERIFY
