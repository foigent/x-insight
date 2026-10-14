#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${1:-$ROOT/dist/signed}"

case "$OUT_DIR" in
  /*) ;;
  *) OUT_DIR="$ROOT/$OUT_DIR" ;;
esac

if [ -e "$OUT_DIR" ]; then
  echo "signed release output already exists: $OUT_DIR" >&2
  exit 1
fi

SIGNING_KEY="${XINSIGHT_RELEASE_SIGNING_KEY:-}"
case "$SIGNING_KEY" in
  /*) ;;
  *)
    echo "XINSIGHT_RELEASE_SIGNING_KEY must be an absolute path" >&2
    exit 1
    ;;
esac

cd "$ROOT"
COMMIT="$(git rev-parse HEAD)"
if [ -n "$(git status --porcelain)" ]; then
  echo "signed release builds require a clean working tree" >&2
  exit 1
fi

GO_VERSION="$(go env GOVERSION)"
EXPECTED_GO_VERSION="${XINSIGHT_EXPECT_GO_VERSION:-go1.25.11}"
if [ "$GO_VERSION" != "$EXPECTED_GO_VERSION" ]; then
  echo "Go toolchain mismatch: got $GO_VERSION, expected $EXPECTED_GO_VERSION" >&2
  exit 1
fi

PRODUCT_VERSION="$(python3 -c 'import json; print(json.load(open("release/release-config.json", encoding="utf-8"))["product_version"])')"
MANUAL_VERSION="$(python3 -c 'import json; print(json.load(open("release/release-config.json", encoding="utf-8"))["manual"]["version"])')"
EXPECTED_NODE_VERSION="v$(python3 -c 'import json; print(json.load(open("frontend/package.json", encoding="utf-8"))["engines"]["node"])')"
EXPECTED_NPM_VERSION="$(python3 -c 'import json; print(json.load(open("frontend/package.json", encoding="utf-8"))["engines"]["npm"])')"
NODE_VERSION="$(node --version)"
NPM_VERSION="$(npm --version)"
if [ "$NODE_VERSION" != "$EXPECTED_NODE_VERSION" ]; then
  echo "Node toolchain mismatch: got $NODE_VERSION, expected $EXPECTED_NODE_VERSION" >&2
  exit 1
fi
if [ "$NPM_VERSION" != "$EXPECTED_NPM_VERSION" ]; then
  echo "npm toolchain mismatch: got $NPM_VERSION, expected $EXPECTED_NPM_VERSION" >&2
  exit 1
fi

node scripts/build-manual.mjs
(
  cd frontend
  npm ci
  npm run build
)
if [ -n "$(git status --porcelain -- internal/webui/manual/zh-CN internal/webui/dist)" ]; then
  echo "generated manual or frontend assets differ from the committed release sources" >&2
  git status --short -- internal/webui/manual/zh-CN internal/webui/dist >&2
  exit 1
fi

SOURCE_DATE_EPOCH="$(git show -s --format=%ct "$COMMIT")"
BUILD_TIME="$(python3 -c 'import datetime, sys; print(datetime.datetime.fromtimestamp(int(sys.argv[1]), datetime.timezone.utc).isoformat().replace("+00:00", "Z"))' "$SOURCE_DATE_EPOCH")"
PAYLOAD_DIR="$OUT_DIR/payload"
ARTIFACT="$OUT_DIR/xinsight-v${PRODUCT_VERSION}-linux-amd64-${COMMIT}.tar.gz"
BOOTSTRAP_DIR="$OUT_DIR/bootstrap"
BOOTSTRAP_RELEASE_TOOL="$BOOTSTRAP_DIR/xinsight-release-linux-amd64-${COMMIT}"

mkdir -p "$PAYLOAD_DIR/bin" "$PAYLOAD_DIR/migrations" "$PAYLOAD_DIR/schemas" "$PAYLOAD_DIR/fixtures"
mkdir -p "$PAYLOAD_DIR/manual/zh-CN" "$PAYLOAD_DIR/release" "$PAYLOAD_DIR/deploy"
mkdir -p "$BOOTSTRAP_DIR"

LDFLAGS="-buildid= -X github.com/foigent/x-insight/internal/buildinfo.ProductVersion=$PRODUCT_VERSION -X github.com/foigent/x-insight/internal/buildinfo.Commit=$COMMIT -X github.com/foigent/x-insight/internal/buildinfo.BuildTime=$BUILD_TIME -X github.com/foigent/x-insight/internal/buildinfo.GoVersion=$GO_VERSION -X github.com/foigent/x-insight/internal/buildinfo.ManualVersion=$MANUAL_VERSION"
for command in xinsight-server xinsight-importer xinsightctl xinsight-launcher; do
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -buildvcs=false -ldflags="$LDFLAGS" \
    -o "$PAYLOAD_DIR/bin/$command" "./cmd/$command"
done
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -buildvcs=false -ldflags="$LDFLAGS" \
  -o "$BOOTSTRAP_RELEASE_TOOL" ./cmd/xinsight-release

while IFS= read -r tracked_file; do
  mkdir -p "$PAYLOAD_DIR/$(dirname "$tracked_file")"
  cp "$tracked_file" "$PAYLOAD_DIR/$tracked_file"
done < <(git ls-files migrations schemas fixtures)

for manual_file in internal/webui/manual/zh-CN/index.html internal/webui/manual/zh-CN/manual.css internal/webui/manual/zh-CN/manual-icon.svg; do
  if [ ! -f "$manual_file" ]; then
    echo "generated manual is missing: $manual_file" >&2
    exit 1
  fi
  cp "$manual_file" "$PAYLOAD_DIR/manual/zh-CN/$(basename "$manual_file")"
done

cp release/compatibility-matrix.json "$PAYLOAD_DIR/release/compatibility-matrix.json"
cp release/release-notes.md "$PAYLOAD_DIR/release/release-notes.md"
cp internal/hostctl/clean_baseline_schema_contract.json "$PAYLOAD_DIR/release/schema-contract.json"
cp deploy/xinsight.service "$PAYLOAD_DIR/deploy/xinsight.service"
cp deploy/xinsight-connector-controller.service "$PAYLOAD_DIR/deploy/xinsight-connector-controller.service"
cp deploy/xinsight-feed.service "$PAYLOAD_DIR/deploy/xinsight-feed.service"
cp deploy/xinsight-litellm-feed.service "$PAYLOAD_DIR/deploy/xinsight-litellm-feed.service"
cp deploy/99-xinsight-managed-launcher.conf "$PAYLOAD_DIR/deploy/99-xinsight-managed-launcher.conf"
cp deploy/xinsight.env.example "$PAYLOAD_DIR/deploy/xinsight.env.example"
cp deploy/xinsight-connector.env.example "$PAYLOAD_DIR/deploy/xinsight-connector.env.example"
cp deploy/trusted-tls-proxy.env.example "$PAYLOAD_DIR/deploy/trusted-tls-proxy.env.example"
cp deploy/xinsight-nginx-ip.conf.example "$PAYLOAD_DIR/deploy/xinsight-nginx-ip.conf.example"
cp deploy/xinsight-feed.env.example "$PAYLOAD_DIR/deploy/xinsight-feed.env.example"
cp deploy/xinsight-litellm-feed.env.example "$PAYLOAD_DIR/deploy/xinsight-litellm-feed.env.example"
cp deploy/postgres.compose.yml "$PAYLOAD_DIR/deploy/postgres.compose.yml"
cp deploy/postgres.env.example "$PAYLOAD_DIR/deploy/postgres.env.example"

go run ./cmd/xinsight-release pack \
  --payload "$PAYLOAD_DIR" \
  --config "$ROOT/release/release-config.json" \
  --signing-key "$SIGNING_KEY" \
  --output "$ARTIFACT" \
  --commit "$COMMIT" \
  --source-date-epoch "$SOURCE_DATE_EPOCH" \
  --go-version "$GO_VERSION" \
  --node-version "$NODE_VERSION" \
  --npm-version "$NPM_VERSION" > "$OUT_DIR/build-result.json"

python3 - "$OUT_DIR/build-result.json" "$ARTIFACT" <<'PY'
import json
import pathlib
import sys

result = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
if pathlib.Path(result["output"]) != pathlib.Path(sys.argv[2]):
    raise SystemExit("release pack output path mismatch")
print(f"artifact={result['output']}")
print(f"bundle_sha256={result['bundle_sha256']}")
print(f"manifest_sha256={result['manifest_sha256']}")
print(f"product_version={result['product_version']}")
print(f"commit={result['commit']}")
PY
echo "bootstrap_release_tool=$BOOTSTRAP_RELEASE_TOOL"
