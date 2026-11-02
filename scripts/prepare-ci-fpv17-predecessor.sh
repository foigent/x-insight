#!/usr/bin/env bash
set -euo pipefail
if [ "$#" -ne 3 ] || [ "${GITHUB_ACTIONS:-}" != true ] || [ "${XINSIGHT_CI_EPHEMERAL_HOST:-}" != 1 ] || [ "$(id -u)" -eq 0 ]; then
  echo 'usage: disposable Actions VM: prepare-ci-fpv17-predecessor.sh CANDIDATE_ARTIFACT CANDIDATE_PAYLOAD CANDIDATE_TRUST' >&2
  exit 2
fi
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
for path in "$@"; do case "$path" in /*) ;; *) exit 2 ;; esac; done
PREDECESSOR="${XINSIGHT_CI_ACCOUNT_PREDECESSOR:-fpv17}"
case "$PREDECESSOR" in
  fpv17) OLD_COMMIT=164582cab0e7f169c265c2494d04920c61af833d ;;
  fpv20) OLD_COMMIT=08435e0949dfae0ae65497d2e12ca356f6d820f0 ;;
  *) exit 2 ;;
esac
OLD_SOURCE="${RUNNER_TEMP:?}/installed-${PREDECESSOR}-source"
OLD_SIGNED="$RUNNER_TEMP/installed-${PREDECESSOR}-signed"
OLD_PAYLOAD="$RUNNER_TEMP/installed-${PREDECESSOR}-payload"
CI_TRUST="$RUNNER_TEMP/xinsight-${PREDECESSOR}-ci-trust"
test ! -e "$OLD_SOURCE" && test ! -e "$CI_TRUST"
if [ "$PREDECESSOR" = fpv17 ]; then
  python3 "$ROOT/scripts/reproduce-signed-predecessor-source.py" --check-source-directory fpv17 "$ROOT/compatibility/fpv17-linux-amd64/source"
fi
mkdir -m 0700 "$CI_TRUST"
if [ "$PREDECESSOR" = fpv17 ]; then
(cd "$ROOT"; go run ./cmd/xinsight-release keygen --private-key "$CI_TRUST/signing-key.json" --trust-store "$CI_TRUST/old-trust.json" --created-at 2000-01-01T00:00:00Z)
  XINSIGHT_RELEASE_SIGNING_KEY="$CI_TRUST/signing-key.json" \
    python3 "$ROOT/scripts/reproduce-signed-predecessor-source.py" --build fpv17 "$ROOT/compatibility/fpv17-linux-amd64/source" "$OLD_SOURCE" "$OLD_SIGNED"
  OLD_ARTIFACT="$OLD_SIGNED/xinsight-v0.2.0-dev.fpv17-linux-amd64-$OLD_COMMIT.tar.gz"
  XINSIGHT_RELEASE_TOOL="$OLD_SIGNED/bootstrap/xinsight-release-linux-amd64-$OLD_COMMIT" \
    python3 "$ROOT/scripts/reproduce-signed-predecessor-source.py" --verify fpv17 "$OLD_SOURCE" "$OLD_ARTIFACT" "$CI_TRUST/old-trust.json" "$OLD_PAYLOAD"
else
  # Reuse the exact already-published, owner-signed FPV20 release installed on
  # the original host. Download only its three fixed public inputs; no rebuild.
  mkdir -m 0700 "$OLD_SOURCE" "$OLD_SIGNED"
  test "$(git -C "$ROOT" rev-parse "$OLD_COMMIT^{tree}")" = bd463c9100cf5d6915c7bcbaf4d03e0e499de5c3
  git -C "$ROOT" archive "$OLD_COMMIT" | tar -x -C "$OLD_SOURCE"
  gh release download v0.2.0-dev.fpv20 --repo foigent/x-insight --dir "$OLD_SIGNED" \
    --pattern "xinsight-v0.2.0-dev.fpv20-linux-amd64-$OLD_COMMIT.tar.gz" \
    --pattern "xinsight-release-linux-amd64-$OLD_COMMIT" --pattern trusted-release-keys.json
  OLD_ARTIFACT="$OLD_SIGNED/xinsight-v0.2.0-dev.fpv20-linux-amd64-$OLD_COMMIT.tar.gz"
  OLD_BOOTSTRAP="$OLD_SIGNED/xinsight-release-linux-amd64-$OLD_COMMIT"
  test "$(sha256sum "$OLD_ARTIFACT" | cut -d ' ' -f1)" = 8bef119b796dac52eadc267717e3ec20ee112c0cf9a33dfd9f05b2129580685f
  test "$(sha256sum "$OLD_BOOTSTRAP" | cut -d ' ' -f1)" = e928b71ad83c5c83f2d83c37cbe7def1273197d0858ec874102e941ccc9bb5cb
  test "$(sha256sum "$OLD_SIGNED/trusted-release-keys.json" | cut -d ' ' -f1)" = be67a9b1502f79784d14688338d1d731b338d92807d9764bb6311855ce8cc8dd
  cp "$OLD_SIGNED/trusted-release-keys.json" "$CI_TRUST/old-trust.json"
  chmod 0700 "$OLD_BOOTSTRAP"
  XINSIGHT_RELEASE_TOOL="$OLD_BOOTSTRAP" \
    "$OLD_SOURCE/scripts/verify-signed-release.sh" "$OLD_ARTIFACT" "$CI_TRUST/old-trust.json" "$OLD_PAYLOAD" --profile=development-d
fi
test -f "$OLD_ARTIFACT"
# Preserve the original package bytes in a root-owned path readable from the
# installed controller's strict namespace; do not change the frozen old source.
STAGED_OLD_ARTIFACT=/opt/xinsight-ci-artifacts/${PREDECESSOR}.tar.gz
sudo test ! -e "$STAGED_OLD_ARTIFACT"
sudo test ! -L "$STAGED_OLD_ARTIFACT"
sudo install -o root -g root -m 0444 "$OLD_ARTIFACT" "$STAGED_OLD_ARTIFACT"
test "$(sha256sum "$OLD_ARTIFACT" | cut -d ' ' -f1)" = "$(sha256sum "$STAGED_OLD_ARTIFACT" | cut -d ' ' -f1)"
OLD_ARTIFACT="$STAGED_OLD_ARTIFACT"
python3 - "$1" "$2" "$3" "$OLD_SOURCE" "$OLD_ARTIFACT" "$OLD_PAYLOAD" "$CI_TRUST" "$OLD_COMMIT" <<'PY'
import json,os,pathlib,sys
artifact,payload,trust,source,old_artifact,old_payload,private=map(pathlib.Path,sys.argv[1:8])
old_commit=sys.argv[8]
current=json.loads(trust.read_text()); old=json.loads((private/'old-trust.json').read_text())
assert current['schema_version']==old['schema_version']=='xinsight.trusted-release-keys.v1'
keys=current['keys']+old['keys']; assert len({k['key_id'] for k in keys})==len(keys)
combined=private/'combined-trust.json'
with combined.open('x') as f: json.dump(dict(schema_version=current['schema_version'],keys=keys),f)
old_matrix=json.loads((old_payload/'release/compatibility-matrix.json').read_text())
new_matrix=json.loads((payload/'release/compatibility-matrix.json').read_text())
assert (old_matrix['product_version'],old_matrix['database_schema']) in {('0.2.0-dev.fpv17','45'),('0.2.0-dev.fpv20','46')}
assert (new_matrix['product_version'],new_matrix['database_schema'])==('0.2.0-dev.fpv21','47')
assert old_matrix['product_version'] in new_matrix['supported_upgrade_from_versions']
record=dict(schema_version='xinsight.ci-account-upgrade-predecessor.v1',old_commit=old_commit,old_product=old_matrix['product_version'],old_schema=old_matrix['database_schema'],old_source=str(source),old_artifact=str(old_artifact),old_payload=str(old_payload),
            combined_trust=str(combined),candidate_ctl=str(payload/'bin/xinsightctl'),candidate_artifact=str(artifact))
with (pathlib.Path(os.environ['RUNNER_TEMP'])/'account-upgrade-ci-predecessor.json').open('x') as f: json.dump(record,f)
PY
# It is no longer needed after the immutable CI-only artifact is verified.
if [ "$PREDECESSOR" = fpv17 ]; then
python3 - "$CI_TRUST/signing-key.json" <<'PY'
from pathlib import Path
import sys
p=Path(sys.argv[1]); assert p.is_file() and not p.is_symlink(); p.unlink()
PY
fi
