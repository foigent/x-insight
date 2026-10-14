#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DELIVERY_DIR="${1:-}"
DESTINATION="${2:-}"

if [ -z "$DELIVERY_DIR" ]; then
  echo "usage: scripts/verify-owner-supervised-trial-release.sh DELIVERY_DIR [ABSOLUTE_DESTINATION]" >&2
  exit 1
fi
case "$DELIVERY_DIR" in
  /*) ;;
  *) DELIVERY_DIR="$PWD/$DELIVERY_DIR" ;;
esac
if [ ! -d "$DELIVERY_DIR" ]; then
  echo "trial delivery directory is missing: $DELIVERY_DIR" >&2
  exit 1
fi
if [ "$(uname -s)" != "Linux" ] || [ "$(uname -m)" != "x86_64" ]; then
  echo "owner-supervised trial executable verification requires Linux x86_64" >&2
  exit 1
fi
if [ -z "$DESTINATION" ]; then
  VERIFY_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/xinsight-owner-trial-verify.XXXXXX")"
  DESTINATION="$VERIFY_ROOT/payload"
fi
case "$DESTINATION" in
  /*) ;;
  *)
    echo "verification destination must be an absolute path" >&2
    exit 1
    ;;
esac

IFS=$'\t' read -r ARTIFACT_NAME BOOTSTRAP_NAME TRUST_STORE_NAME < <(
  python3 - "$DELIVERY_DIR" "$(git -C "$ROOT" rev-parse HEAD)" <<'PY'
import hashlib
import json
import pathlib
import re
import sys

delivery = pathlib.Path(sys.argv[1])
expected_commit = sys.argv[2]
record_path = delivery / "owner-supervised-trial-release.json"
build_path = delivery / "build-result.json"
checksum_path = delivery / "SHA256SUMS"
marker_path = delivery / "owner-supervised-trial.txt"
trust_path = delivery / "trusted-release-keys.json"
for path in (record_path, build_path, checksum_path, marker_path, trust_path):
    if not path.is_file():
        raise SystemExit(f"required trial delivery file is missing: {path.name}")

record = json.loads(record_path.read_text(encoding="utf-8"))
build = json.loads(build_path.read_text(encoding="utf-8"))
trust = json.loads(trust_path.read_text(encoding="utf-8"))
if record.get("schema_version") != "xinsight.owner-supervised-trial-release.v1":
    raise SystemExit("trial record schema_version mismatch")
if record.get("classification") != "owner_supervised_trial":
    raise SystemExit("trial record classification mismatch")
product = record.get("product", {})
if product.get("name") != "x-insight" or product.get("commit") != expected_commit:
    raise SystemExit("trial record does not match the checked-out exact commit")
if build.get("commit") != expected_commit or build.get("product_version") != product.get("version"):
    raise SystemExit("portable build result does not match the trial record")

artifact_name = record.get("artifact", {}).get("path", "")
bootstrap_name = record.get("bootstrap", {}).get("path", "")
if not re.fullmatch(r"xinsight-v[0-9A-Za-z.-]+-linux-amd64-[a-f0-9]{40}\.tar\.gz", artifact_name):
    raise SystemExit("trial artifact filename is invalid")
if bootstrap_name != f"xinsight-release-linux-amd64-{expected_commit}":
    raise SystemExit("trial bootstrap filename is invalid")
if build.get("output") != artifact_name:
    raise SystemExit("portable build result output is not the trial artifact basename")

artifact = delivery / artifact_name
bootstrap = delivery / bootstrap_name
if not artifact.is_file() or not bootstrap.is_file():
    raise SystemExit("trial artifact or bootstrap verifier is missing")

def ref(path):
    return "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()

for key, path in (("artifact", artifact), ("bootstrap", bootstrap), ("trust_store", trust_path)):
    item = record.get(key, {})
    if item.get("sha256") != ref(path) or item.get("size_bytes") != path.stat().st_size:
        raise SystemExit(f"trial {key} hash or size mismatch")
if build.get("bundle_sha256") != record["artifact"]["sha256"]:
    raise SystemExit("signed build result and artifact hash differ")
keys = trust.get("keys", [])
if trust.get("schema_version") != "xinsight.trusted-release-keys.v1" or len(keys) != 1:
    raise SystemExit("trial trust store must contain exactly one key")
if keys[0].get("key_id") != build.get("key_id") or keys[0].get("status") != "active":
    raise SystemExit("trial trust store key does not match the signed build")
if record.get("trust_store", {}).get("key_id") != build.get("key_id"):
    raise SystemExit("trial record trust key does not match the signed build")

source_ci = record.get("source_ci", {})
required_ci = {
    "workflow_name": "CI",
    "event": "push",
    "head_branch": "main",
    "head_sha": expected_commit,
    "conclusion": "success",
}
for field, expected in required_ci.items():
    if source_ci.get(field) != expected:
        raise SystemExit(f"trial source CI {field} mismatch")
source_workflow = record.get("source_workflow", {})
expected_workflow_url = (
    f"https://github.com/foigent/x-insight/actions/runs/{source_workflow.get('run_id', '')}"
)
if (
    source_workflow.get("repository") != "foigent/x-insight"
    or source_workflow.get("ref") != "refs/heads/main"
    or source_workflow.get("url") != expected_workflow_url
):
    raise SystemExit("trial source workflow is not exact main")

supervision = record.get("supervision", {})
expected_supervision = {
    "install_owners": ["product_owner", "codex"],
    "customer_self_upgrade": False,
    "third_party_install": False,
    "long_lived_private_key": False,
    "private_key_retained": False,
    "independent_hash_check_required": True,
}
if supervision != expected_supervision:
    raise SystemExit("trial supervision boundary mismatch")
if record.get("claims") != {
    "release_authority": "owner_supervised_trial_only",
    "target_verified": False,
    "external_integration_verified": False,
    "production_verified": False,
}:
    raise SystemExit("trial claim boundary mismatch")

expected_files = {
    artifact_name,
    bootstrap_name,
    "build-result.json",
    "trusted-release-keys.json",
    "owner-supervised-trial-release.json",
    "owner-supervised-trial.txt",
    "SHA256SUMS",
}
delivery_entries = list(delivery.iterdir())
if any(path.is_symlink() for path in delivery_entries):
    raise SystemExit("trial delivery must not contain symbolic links")
actual_files = {path.name for path in delivery_entries if path.is_file()}
if actual_files != expected_files or any(path.is_dir() for path in delivery_entries):
    raise SystemExit(f"trial delivery file set mismatch: {sorted(actual_files)}")

checksum_entries = {}
for line in checksum_path.read_text(encoding="utf-8").splitlines():
    match = re.fullmatch(r"([a-f0-9]{64})  ([A-Za-z0-9][A-Za-z0-9._-]{0,255})", line)
    if not match or match.group(2) in checksum_entries:
        raise SystemExit("invalid SHA256SUMS entry")
    checksum_entries[match.group(2)] = match.group(1)
checksum_files = expected_files - {"SHA256SUMS"}
if set(checksum_entries) != checksum_files:
    raise SystemExit("SHA256SUMS file set mismatch")
for name, expected in checksum_entries.items():
    actual = hashlib.sha256((delivery / name).read_bytes()).hexdigest()
    if actual != expected:
        raise SystemExit(f"SHA256SUMS mismatch for {name}")

for path in (build_path, record_path, trust_path):
    if "private_key_base64" in path.read_text(encoding="utf-8"):
        raise SystemExit(f"private key field found in delivery: {path.name}")

print(artifact_name, bootstrap_name, "trusted-release-keys.json", sep="\t")
PY
)

ARTIFACT="$DELIVERY_DIR/$ARTIFACT_NAME"
BOOTSTRAP="$DELIVERY_DIR/$BOOTSTRAP_NAME"
TRUST_STORE="$DELIVERY_DIR/$TRUST_STORE_NAME"
chmod 0755 "$BOOTSTRAP"
XINSIGHT_RELEASE_TOOL="$BOOTSTRAP" \
  "$ROOT/scripts/verify-signed-release.sh" \
  "$ARTIFACT" \
  "$TRUST_STORE" \
  "$DESTINATION"

echo "owner_supervised_trial_verification=pass"
echo "owner_supervised_trial_delivery=$DELIVERY_DIR"
echo "owner_supervised_trial_payload=$DESTINATION"
