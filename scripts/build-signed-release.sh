#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PROFILE=rc13
PREPARE_ONLY=0
DRY_RUN=0
OUT_DIR=""
usage() {
  echo "usage: scripts/build-signed-release.sh [OUT_DIR] [--profile=rc13|development-d] [--prepare-only] [--dry-run]" >&2
}
for argument in "$@"; do
  case "$argument" in
    --profile=rc13|--profile=development-d) PROFILE="${argument#--profile=}" ;;
    --prepare-only) PREPARE_ONLY=1 ;;
    --dry-run) DRY_RUN=1 ;;
    --*) usage; exit 1 ;;
    *)
      if [ -n "$OUT_DIR" ] || [ -z "$argument" ]; then usage; exit 1; fi
      OUT_DIR="$argument"
      ;;
  esac
done
OUT_DIR="${OUT_DIR:-$ROOT/dist/signed}"
CONFIG_PATH=release/release-config.json
MATRIX_PATH=release/compatibility-matrix.json
NOTES_PATH=release/release-notes.md
CONTRACT_PATH=internal/hostctl/clean_baseline_schema_contract.json
if [ "$PROFILE" = development-d ]; then
  CONFIG_PATH=release/d-validation-config.json
  MATRIX_PATH=release/d-validation-compatibility-matrix.json
  NOTES_PATH=release/d-validation-notes.md
  CONTRACT_PATH=internal/hostctl/development_schema_contract.json
fi

case "$OUT_DIR" in
  /*) ;;
  *) OUT_DIR="$ROOT/$OUT_DIR" ;;
esac

if [ -e "$OUT_DIR" ] || [ -L "$OUT_DIR" ]; then
  echo "signed release output already exists: $OUT_DIR" >&2
  exit 1
fi

# Preparation and dry-run never inspect the signing-key environment or file.
if [ "$PREPARE_ONLY" -eq 0 ] && [ "$DRY_RUN" -eq 0 ]; then
  SIGNING_KEY="${XINSIGHT_RELEASE_SIGNING_KEY:-}"
  case "$SIGNING_KEY" in
    /*) ;;
    *) echo "XINSIGHT_RELEASE_SIGNING_KEY must be an absolute path" >&2; exit 1 ;;
  esac
fi

cd "$ROOT"
COMMIT="$(git rev-parse HEAD)"
if [ -n "$(GIT_OPTIONAL_LOCKS=0 git status --porcelain)" ]; then
  echo "signed release builds require a clean working tree" >&2
  exit 1
fi

GO_VERSION="$(GOTOOLCHAIN=local go env GOVERSION)"
EXPECTED_GO_VERSION="${XINSIGHT_EXPECT_GO_VERSION:-go1.25.12}"
if [ "$GO_VERSION" != "$EXPECTED_GO_VERSION" ]; then
  echo "Go toolchain mismatch: got $GO_VERSION, expected $EXPECTED_GO_VERSION" >&2
  exit 1
fi

PRODUCT_VERSION="$(python3 -c 'import json, sys; print(json.load(open(sys.argv[1], encoding="utf-8"))["product_version"])' "$CONFIG_PATH")"
MANUAL_VERSION="$(python3 -c 'import json, sys; print(json.load(open(sys.argv[1], encoding="utf-8"))["manual"]["version"])' "$CONFIG_PATH")"
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

# Read-only identity preflight. This is not signature or runtime qualification.
python3 - "$CONFIG_PATH" "$MATRIX_PATH" "$CONTRACT_PATH" <<'PY_PREFLIGHT'
import json
import pathlib
import sys

config, matrix, contract = [json.loads(pathlib.Path(p).read_text()) for p in sys.argv[1:]]
legacy_identity_fields = ("release_id", "commit", "artifact_sha256", "manifest_sha256", "database_schema", "application_database_schema_min", "application_database_schema_max", "compatible_config_schemas")
def legacy_identities(items):
    # The compatibility matrix carries a documented identity summary plus qualification notes.
    return [{key: item[key] for key in legacy_identity_fields} for item in items]
checks = [
    config["product_version"] == matrix["product_version"],
    config["manual"]["version"] == matrix["manual_version"],
    config["supported_upgrade_from_versions"] == matrix["supported_upgrade_from_versions"],
    legacy_identities(config["supported_legacy_sources"]) == legacy_identities(matrix["supported_legacy_sources"]),
    all(config["database_schema"][key] == str(contract["database_schema"]) for key in ("current", "min", "max")),
    str(matrix["database_schema"]) == str(contract["database_schema"]),
    all(config["application_compatibility"][key] == str(contract["database_schema"]) for key in ("database_schema_min", "database_schema_max")),
    int(contract["database_schema"]) == len(list(pathlib.Path("migrations").glob("*.up.sql"))),
    all(config["schema_contract"][key] == matrix["schema_contract"][key] == contract[key]
        for key in ("schema_version", "semantic_sha256", "migration_set_sha256")),
]
if not all(checks):
    raise SystemExit("release config, matrix, schema contract or migration count differs")
PY_PREFLIGHT
bash scripts/verify-protected-runtime.sh
if [ "$DRY_RUN" -eq 1 ]; then
  echo "dry_run=preflight_only profile=$PROFILE product_version=$PRODUCT_VERSION commit=$COMMIT"
  echo "generated_assets_verified=false payload_built=false signed=false"
  exit 0
fi

node scripts/build-manual.mjs "--profile=$PROFILE"
(
  cd frontend
  if [ "$PREPARE_ONLY" -eq 1 ]; then
    # Use pre-provisioned dependencies; preparation does not install packages.
    npm --offline run build
  else
    npm ci
    npm run build
  fi
)
if [ -n "$(git status --porcelain -- internal/webui/manual internal/webui/dist)" ]; then
  echo "generated manual or frontend assets differ from the committed release sources" >&2
  git status --short -- internal/webui/manual internal/webui/dist >&2
  exit 1
fi

python3 scripts/collect-third-party-notices.py --check

SOURCE_DATE_EPOCH="$(git show -s --format=%ct "$COMMIT")"
BUILD_TIME="$(python3 -c 'import datetime, sys; print(datetime.datetime.fromtimestamp(int(sys.argv[1]), datetime.timezone.utc).isoformat().replace("+00:00", "Z"))' "$SOURCE_DATE_EPOCH")"
PAYLOAD_DIR="$OUT_DIR/payload"
ARTIFACT="$OUT_DIR/xinsight-v${PRODUCT_VERSION}-linux-amd64-${COMMIT}.tar.gz"
BOOTSTRAP_DIR="$OUT_DIR/bootstrap"
BOOTSTRAP_RELEASE_TOOL="$BOOTSTRAP_DIR/xinsight-release-linux-amd64-${COMMIT}"

mkdir -p "$PAYLOAD_DIR/bin" "$PAYLOAD_DIR/migrations" "$PAYLOAD_DIR/schemas" "$PAYLOAD_DIR/fixtures"
mkdir -p "$PAYLOAD_DIR/manual/zh-CN" "$PAYLOAD_DIR/release" "$PAYLOAD_DIR/deploy"
mkdir -p "$BOOTSTRAP_DIR"

if [ "$PREPARE_ONLY" -eq 1 ]; then
  export GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local
fi
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

manual_languages=(zh-CN)
if [ "$PROFILE" = development-d ]; then manual_languages+=(en-US); fi
for language in "${manual_languages[@]}"; do
  mkdir -p "$PAYLOAD_DIR/manual/$language"
  for name in index.html manual.css manual-icon.svg; do
    cp "internal/webui/manual/$language/$name" "$PAYLOAD_DIR/manual/$language/$name"
  done
done
cp LICENSE NOTICE THIRD_PARTY_NOTICES.md "$PAYLOAD_DIR/"
cp -R licenses "$PAYLOAD_DIR/licenses"

cp "$MATRIX_PATH" "$PAYLOAD_DIR/release/compatibility-matrix.json"
cp "$NOTES_PATH" "$PAYLOAD_DIR/release/release-notes.md"
cp "$CONTRACT_PATH" "$PAYLOAD_DIR/release/schema-contract.json"
cp deploy/xinsight.service "$PAYLOAD_DIR/deploy/xinsight.service"
cp deploy/xinsight-connector-controller.service "$PAYLOAD_DIR/deploy/xinsight-connector-controller.service"
cp deploy/xinsight-feed.service "$PAYLOAD_DIR/deploy/xinsight-feed.service"
cp deploy/xinsight-source-facts.service "$PAYLOAD_DIR/deploy/xinsight-source-facts.service"
cp deploy/source-facts.json.example "$PAYLOAD_DIR/deploy/source-facts.json.example"
cp deploy/source-facts-offline.authorization.json.example "$PAYLOAD_DIR/deploy/source-facts-offline.authorization.json.example"
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


if [ "$PROFILE" = development-d ]; then
  mkdir -p "$PAYLOAD_DIR/schemas/internal"
  cp internal/sourcefacts/contract/resource-access.schema.json "$PAYLOAD_DIR/schemas/internal/resource-access.schema.json"
  cp deploy/source-budgets-offline.authorization.json.example "$PAYLOAD_DIR/deploy/source-budgets-offline.authorization.json.example"
fi
if [ "$PREPARE_ONLY" -eq 1 ]; then
  cp "$CONFIG_PATH" "$OUT_DIR/release-config.json"
  python3 - "$OUT_DIR" "$PROFILE" "$PRODUCT_VERSION" "$COMMIT" "$GO_VERSION" "$NODE_VERSION" "$NPM_VERSION" <<'PY_PREPARED'
import hashlib
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
files = [p for base in (root / "payload", root / "bootstrap") for p in base.rglob("*") if p.is_file()]
files.append(root / "release-config.json")
result = {
    "status": "unsigned_preparation", "signed": False, "installable": False,
    "profile": sys.argv[2], "product_version": sys.argv[3], "commit": sys.argv[4],
    "build_toolchain": dict(zip(("go", "node", "npm"), sys.argv[5:])),
    "files": [{"path": str(p.relative_to(root)), "sha256": hashlib.sha256(p.read_bytes()).hexdigest(),
               "size_bytes": p.stat().st_size, "mode": format(p.stat().st_mode & 0o777, "04o")}
              for p in sorted(files)],
}
(root / "prepare-result.json").write_text(json.dumps(result, indent=2) + "\n")
print("unsigned_payload=" + str(root / "payload"))
print("signed=false installable=false")
PY_PREPARED
  echo "bootstrap_release_tool=$BOOTSTRAP_RELEASE_TOOL"
  exit 0
fi

go run ./cmd/xinsight-release pack \
  --payload "$PAYLOAD_DIR" \
  --config "$ROOT/$CONFIG_PATH" \
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
