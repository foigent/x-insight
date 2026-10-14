#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_ROOT="${1:-}"

if [ -z "$OUT_ROOT" ]; then
  echo "usage: scripts/build-owner-supervised-trial-release.sh ABSOLUTE_OR_REPO_RELATIVE_OUTPUT" >&2
  exit 1
fi
case "$OUT_ROOT" in
  /*) ;;
  *) OUT_ROOT="$ROOT/$OUT_ROOT" ;;
esac
if [ -e "$OUT_ROOT" ]; then
  echo "owner-supervised trial output already exists: $OUT_ROOT" >&2
  exit 1
fi
if [ "${XINSIGHT_TRIAL_CONFIRMATION:-}" != "OWNER_SUPERVISED_TRIAL" ]; then
  echo "XINSIGHT_TRIAL_CONFIRMATION must equal OWNER_SUPERVISED_TRIAL" >&2
  exit 1
fi

SOURCE_CI_JSON="${XINSIGHT_SOURCE_CI_JSON:-}"
case "$SOURCE_CI_JSON" in
  /*) ;;
  *)
    echo "XINSIGHT_SOURCE_CI_JSON must be an absolute path" >&2
    exit 1
    ;;
esac
if [ ! -f "$SOURCE_CI_JSON" ]; then
  echo "source CI record is missing: $SOURCE_CI_JSON" >&2
  exit 1
fi

for name in GITHUB_REPOSITORY GITHUB_RUN_ID GITHUB_RUN_ATTEMPT GITHUB_ACTOR GITHUB_SERVER_URL; do
  if [ -z "${!name:-}" ]; then
    echo "$name is required for an owner-supervised trial release" >&2
    exit 1
  fi
done
if [ "$GITHUB_REPOSITORY" != "foigent/x-insight" ] || [ "$GITHUB_SERVER_URL" != "https://github.com" ]; then
  echo "owner-supervised trial releases must originate from https://github.com/foigent/x-insight" >&2
  exit 1
fi
if [ "${GITHUB_REF:-}" != "refs/heads/main" ]; then
  echo "owner-supervised trial releases must run from refs/heads/main" >&2
  exit 1
fi

PRIVATE_KEY="${OUT_ROOT}.ephemeral-private-key.json"
TRUST_STAGING="${OUT_ROOT}.ephemeral-trust-store.json"
if [ -e "$PRIVATE_KEY" ] || [ -e "$TRUST_STAGING" ]; then
  echo "ephemeral key staging path already exists" >&2
  exit 1
fi

cleanup_ephemeral_files() {
  if [ -e "$PRIVATE_KEY" ]; then
    rm -- "$PRIVATE_KEY"
  fi
  if [ -e "$TRUST_STAGING" ]; then
    rm -- "$TRUST_STAGING"
  fi
}
trap cleanup_ephemeral_files EXIT

cd "$ROOT"
COMMIT="$(git rev-parse HEAD)"
if [ -n "$(git status --porcelain)" ]; then
  echo "owner-supervised trial releases require a clean working tree" >&2
  exit 1
fi
KEY_CREATED_AT="$(python3 - "$COMMIT" <<'PY'
import datetime
import subprocess
import sys

epoch = int(subprocess.check_output(["git", "show", "-s", "--format=%ct", sys.argv[1]], text=True).strip())
print(datetime.datetime.fromtimestamp(epoch, datetime.timezone.utc).isoformat().replace("+00:00", "Z"))
PY
)"

go run ./cmd/xinsight-release keygen \
  --private-key "$PRIVATE_KEY" \
  --trust-store "$TRUST_STAGING" \
  --created-at "$KEY_CREATED_AT" >/dev/null

BUILD_DIR="$OUT_ROOT/build"
XINSIGHT_RELEASE_SIGNING_KEY="$PRIVATE_KEY" scripts/build-signed-release.sh "$BUILD_DIR"

DELIVERY_DIR="$OUT_ROOT/delivery"
mkdir -p "$DELIVERY_DIR"
ARTIFACT="$(python3 - "$BUILD_DIR/build-result.json" <<'PY'
import json
import pathlib
import sys

result = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
print(result["output"])
PY
)"
case "$ARTIFACT" in
  "$BUILD_DIR"/*) ;;
  *)
    echo "signed build artifact escaped its output directory" >&2
    exit 1
    ;;
esac
BOOTSTRAP="$BUILD_DIR/bootstrap/xinsight-release-linux-amd64-$COMMIT"
if [ ! -f "$ARTIFACT" ] || [ ! -x "$BOOTSTRAP" ]; then
  echo "signed build artifact or bootstrap verifier is missing" >&2
  exit 1
fi

cp "$ARTIFACT" "$DELIVERY_DIR/$(basename "$ARTIFACT")"
cp "$BOOTSTRAP" "$DELIVERY_DIR/$(basename "$BOOTSTRAP")"
cp release/owner-supervised-trial.txt "$DELIVERY_DIR/owner-supervised-trial.txt"
mv "$TRUST_STAGING" "$DELIVERY_DIR/trusted-release-keys.json"
chmod 0644 "$DELIVERY_DIR/trusted-release-keys.json"

# The private key is removed before any delivery record or upload set exists.
# This is one explicit file path, never a wildcard or recursive cleanup.
rm -- "$PRIVATE_KEY"
if [ -e "$PRIVATE_KEY" ]; then
  echo "ephemeral signing key still exists after removal" >&2
  exit 1
fi

TRIAL_CREATED_AT="${XINSIGHT_TRIAL_CREATED_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
python3 - \
  "$BUILD_DIR/build-result.json" \
  "$SOURCE_CI_JSON" \
  "$DELIVERY_DIR" \
  "$TRIAL_CREATED_AT" \
  "$GITHUB_REPOSITORY" \
  "$GITHUB_RUN_ID" \
  "$GITHUB_RUN_ATTEMPT" \
  "$GITHUB_ACTOR" \
  "$GITHUB_SERVER_URL" <<'PY'
import datetime
import hashlib
import json
import pathlib
import sys

build_path = pathlib.Path(sys.argv[1])
source_ci_path = pathlib.Path(sys.argv[2])
delivery = pathlib.Path(sys.argv[3])
created_at = sys.argv[4]
repository, run_id, run_attempt, actor, server_url = sys.argv[5:10]

try:
    parsed_created_at = datetime.datetime.fromisoformat(created_at.replace("Z", "+00:00"))
except ValueError as error:
    raise SystemExit(f"invalid trial creation time: {error}")
if parsed_created_at.tzinfo is None:
    raise SystemExit("trial creation time must include a timezone")

build = json.loads(build_path.read_text(encoding="utf-8"))
source_ci = json.loads(source_ci_path.read_text(encoding="utf-8"))
commit = build.get("commit", "")
required_ci = {
    "workflowName": "CI",
    "event": "push",
    "headBranch": "main",
    "headSha": commit,
    "conclusion": "success",
}
for field, expected in required_ci.items():
    if source_ci.get(field) != expected:
        raise SystemExit(f"source CI {field}={source_ci.get(field)!r}, expected {expected!r}")
if not isinstance(source_ci.get("databaseId"), int) or source_ci["databaseId"] <= 0:
    raise SystemExit("source CI databaseId is invalid")
if not source_ci.get("url"):
    raise SystemExit("source CI URL is missing")

artifact_name = pathlib.Path(build["output"]).name
bootstrap_name = f"xinsight-release-linux-amd64-{commit}"
artifact = delivery / artifact_name
bootstrap = delivery / bootstrap_name
trust_store_path = delivery / "trusted-release-keys.json"
marker = delivery / "owner-supervised-trial.txt"
for path in (artifact, bootstrap, trust_store_path, marker):
    if not path.is_file():
        raise SystemExit(f"delivery input is missing: {path.name}")

trust_store = json.loads(trust_store_path.read_text(encoding="utf-8"))
keys = trust_store.get("keys", [])
if trust_store.get("schema_version") != "xinsight.trusted-release-keys.v1" or len(keys) != 1:
    raise SystemExit("trial trust store must contain exactly one key")
key = keys[0]
if key.get("key_id") != build.get("key_id") or key.get("status") != "active":
    raise SystemExit("trial trust key does not match the signed build")

def file_record(path):
    data = path.read_bytes()
    return {
        "path": path.name,
        "sha256": "sha256:" + hashlib.sha256(data).hexdigest(),
        "size_bytes": len(data),
    }

normalized_build = dict(build)
normalized_build["output"] = artifact_name
(delivery / "build-result.json").write_text(
    json.dumps(normalized_build, indent=2, sort_keys=True) + "\n",
    encoding="utf-8",
)

record = {
    "schema_version": "xinsight.owner-supervised-trial-release.v1",
    "classification": "owner_supervised_trial",
    "created_at": created_at,
    "product": {
        "name": "x-insight",
        "version": build["product_version"],
        "manual_version": json.loads(pathlib.Path("release/release-config.json").read_text(encoding="utf-8"))["manual"]["version"],
        "release_channel": "release_candidate",
        "commit": commit,
    },
    "artifact": file_record(artifact),
    "bootstrap": file_record(bootstrap),
    "trust_store": {
        **file_record(trust_store_path),
        "key_id": build["key_id"],
    },
    "source_workflow": {
        "repository": repository,
        "ref": "refs/heads/main",
        "run_id": run_id,
        "run_attempt": run_attempt,
        "actor": actor,
        "url": f"{server_url}/{repository}/actions/runs/{run_id}",
    },
    "source_ci": {
        "database_id": source_ci["databaseId"],
        "workflow_name": source_ci["workflowName"],
        "event": source_ci["event"],
        "head_branch": source_ci["headBranch"],
        "head_sha": source_ci["headSha"],
        "conclusion": source_ci["conclusion"],
        "url": source_ci["url"],
    },
    "supervision": {
        "install_owners": ["product_owner", "codex"],
        "customer_self_upgrade": False,
        "third_party_install": False,
        "long_lived_private_key": False,
        "private_key_retained": False,
        "independent_hash_check_required": True,
    },
    "claims": {
        "release_authority": "owner_supervised_trial_only",
        "target_verified": False,
        "external_integration_verified": False,
        "production_verified": False,
    },
}
record_path = delivery / "owner-supervised-trial-release.json"
record_path.write_text(json.dumps(record, indent=2, sort_keys=True) + "\n", encoding="utf-8")

checksum_paths = sorted(
    [artifact, bootstrap, trust_store_path, marker, delivery / "build-result.json", record_path],
    key=lambda path: path.name,
)
checksum_lines = [f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}" for path in checksum_paths]
(delivery / "SHA256SUMS").write_text("\n".join(checksum_lines) + "\n", encoding="utf-8")

print(f"trial_delivery={delivery}")
print(f"trial_artifact={artifact_name}")
print(f"trial_bundle_sha256={record['artifact']['sha256']}")
print(f"trial_trust_store_sha256={record['trust_store']['sha256']}")
print(f"trial_bootstrap_sha256={record['bootstrap']['sha256']}")
print(f"trial_source_ci_run={source_ci['databaseId']}")
print("trial_private_key_retained=false")
PY

echo "owner_supervised_trial_delivery=$DELIVERY_DIR"
