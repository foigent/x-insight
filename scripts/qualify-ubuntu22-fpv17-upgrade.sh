#!/usr/bin/env bash
set -euo pipefail
if [ "$#" -ne 2 ] || [ "${GITHUB_ACTIONS:-}" != true ] || [ "${XINSIGHT_CI_EPHEMERAL_HOST:-}" != 1 ] || [ "$(id -u)" -eq 0 ]; then
  echo 'usage: disposable Actions VM: qualify-ubuntu22-fpv17-upgrade.sh CANDIDATE_ARTIFACT CANDIDATE_PAYLOAD' >&2
  exit 2
fi
for path in "$@"; do case "$path" in /*) ;; *) exit 2 ;; esac; done
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CANDIDATE_ARTIFACT="$1"
CANDIDATE_PAYLOAD="$2"
CTL="$CANDIDATE_PAYLOAD/bin/xinsightctl"
QUAL="${RUNNER_TEMP:?}/xinsight-fpv17-to-fpv20-qualification"
test ! -e "$QUAL"
mkdir -m 0700 "$QUAL"
umask 077
STAGE=preflight
RESTORE_WORKERS=0
qualification_exit() {
  rc=$?
  trap - EXIT
  if [ "$RESTORE_WORKERS" = 1 ]; then
    sudo systemctl start xinsight-feed.service xinsight-source-facts.service >/dev/null 2>&1 || true
  fi
  if [ "$rc" -ne 0 ]; then
    python3 - "$QUAL/qualification-failure.json" "$STAGE" "$rc" <<'PY'
import json, pathlib, sys
path, stage, code = pathlib.Path(sys.argv[1]), sys.argv[2], int(sys.argv[3])
assert stage in {'preflight','check_snapshot','check','upgrade','verify'} and 0 < code < 256
path.write_text(json.dumps({'schema_version':'xinsight.ci-fpv17-to-fpv20-upgrade-failure.v1',
                            'stage':stage,'exit_code':code},sort_keys=True)+'\n')
PY
  fi
  exit "$rc"
}
trap qualification_exit EXIT
test -f "$CANDIDATE_ARTIFACT" && test -x "$CTL"
test -f "$RUNNER_TEMP/fpv17-ci-predecessor.json"
CTL_PATH=/usr/lib/postgresql/16/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
units=(xinsight-feed.service xinsight-source-facts.service)
for unit in "${units[@]}"; do
  test "$(systemctl is-active "$unit")" = active
done
snapshot() {
  sudo env GITHUB_ACTIONS=true XINSIGHT_CI_EPHEMERAL_HOST=1 \
    python3 "$ROOT/scripts/ci-fpv17-upgrade-snapshot.py" > "$1"
}
# Only the read-only check's equality window is quiesced. Restore both
# workers before the actual upgrade so hostctl must preserve active service.
sudo systemctl stop "${units[@]}"
RESTORE_WORKERS=1
STAGE=check_snapshot
snapshot "$QUAL/before-private.json"
python3 - "$QUAL/before-private.json" "$RUNNER_TEMP/fpv17-ci-predecessor.json" <<'PY'
import json, pathlib, sys
before=json.loads(pathlib.Path(sys.argv[1]).read_text())
predecessor=json.loads(pathlib.Path(sys.argv[2]).read_text())
current=before['status']['install_state']['current']
assert predecessor['schema_version']=='xinsight.ci-fpv17-predecessor.v1'
assert before['status']['managed'] and before['status'].get('active_journal') is None
assert current['product_version']=='0.2.0-dev.fpv17'
assert current['commit']=='164582cab0e7f169c265c2494d04920c61af833d'
assert current['database_schema']=='45' and before['status']['install_state']['database_schema_current']=='45'
assert before['release']==current['release_dir']
assert int(before['database']['schema_migrations'].split(':',1)[0])==45
for table in ('source_fact_documents','source_fact_streams','source_content_manifests',
              'review_body_operations','workbench_access_grants'):
    assert int(before['database'][table].split(':',1)[0])>0, table
PY
STAGE=check
sudo env PATH="$CTL_PATH" "$CTL" upgrade --check --artifact "$CANDIDATE_ARTIFACT" > "$QUAL/upgrade-check-private.json"
snapshot "$QUAL/after-check-private.json"
python3 - "$QUAL/before-private.json" "$QUAL/after-check-private.json" "$QUAL/upgrade-check-private.json" <<'PY'
import json, pathlib, sys
before,after,check=[json.loads(pathlib.Path(p).read_text()) for p in sys.argv[1:]]
assert before==after, 'upgrade --check changed installed state, database, key, grants, config or process'
assert check['operation']=='upgrade' and check['supported'] and check['mutating'] is False
assert check['current']['product_version']=='0.2.0-dev.fpv17'
assert check['target']['product_version']=='0.2.0-dev.fpv20'
assert check['current']['database_schema']=='45' and check['target']['database_schema']=='46'
assert any(c['name']=='control_credential_lifecycle' and c['status']=='pass' for c in check['checks'])
PY
sudo systemctl start "${units[@]}"
for unit in "${units[@]}"; do test "$(systemctl is-active "$unit")" = active; done
RESTORE_WORKERS=0
for unit in "${units[@]}"; do
  invocation="$(systemctl show "$unit" --property=InvocationID --value)"
  test -n "$invocation"
  printf '%s\n' "$invocation" > "$QUAL/$unit.before-upgrade-invocation"
done
STAGE=upgrade
sudo env PATH="$CTL_PATH" "$CTL" upgrade --artifact "$CANDIDATE_ARTIFACT" > "$QUAL/upgrade-receipt-private.json"
STAGE=verify
snapshot "$QUAL/after-upgrade-private.json"
sudo /opt/x-insight/current/bin/xinsightctl review-body-config validate > "$QUAL/review-body-validate-private.json"
for unit in "${units[@]}"; do
  test "$(systemctl is-active "$unit")" = active
  test "$(systemctl show "$unit" --property=InvocationID --value)" != "$(cat "$QUAL/$unit.before-upgrade-invocation")"
done
python3 - "$QUAL/before-private.json" "$QUAL/after-upgrade-private.json" "$QUAL/upgrade-check-private.json" "$QUAL/upgrade-receipt-private.json" > "$QUAL/qualification-result.json" <<'PY'
import collections, json, pathlib, sys
before,after,check,receipt=[json.loads(pathlib.Path(p).read_text()) for p in sys.argv[1:]]
old=before['status']['install_state']['current']; new=after['status']['install_state']['current']
assert receipt['kind']=='upgrade' and receipt['status']=='completed'
assert receipt['before']['commit']==old['commit'] and receipt['after']['commit']==check['target']['commit']
assert after['status']['managed'] and after['status'].get('active_journal') is None
assert new['product_version']=='0.2.0-dev.fpv20' and new['commit']==check['target']['commit']
assert new['database_schema']==after['status']['install_state']['database_schema_current']=='46'
assert after['status']['install_state']['previous']['commit']==old['commit']
assert after['release']==new['release_dir']!=before['release']
assert after['server_invocation']!=before['server_invocation']
assert after['server_exe']==after['release']+'/bin/xinsight-server'
assert after['worker_exes']=={unit:after['release']+'/bin/xinsight-importer'
                              for unit in ('xinsight-feed.service','xinsight-source-facts.service')}
# Poll metadata may advance while the workers are active. Compare immutable
# rows exactly and active cursor counts without demanding timestamp equality.
stable=('schema_migrations','workbench_access_grants',
        'work_event_collections','work_event_documents',
        'connector_installation_source_bindings','installation_identity',
        'table_acl','role_boundary')
assert all(after['database'][table]==before['database'][table] for table in stable)
for table, rows in before['immutable_rows'].items():
    assert not (collections.Counter(rows)-collections.Counter(after['immutable_rows'][table])), table
for table in ('source_fact_streams','source_content_collections','adapter_import_states'):
    assert int(after['database'][table].split(':',1)[0])==int(before['database'][table].split(':',1)[0])
old_fact,new_fact=before['progress']['source_fact'],after['progress']['source_fact']
old_content,new_content=before['progress']['source_content'],after['progress']['source_content']
old_feed,new_feed=before['progress']['economic_feed'],after['progress']['economic_feed']
assert [r[0] for r in old_fact]==[r[0] for r in new_fact]
assert [r[0] for r in old_content]==[r[0] for r in new_content]
assert [r[0] for r in old_feed]==[r[0] for r in new_feed]
for previous,current in zip(old_content,new_content):
    assert int(current[2])>=int(previous[2]) and int(current[3])>=int(previous[3])
    if int(current[2])==int(previous[2]):
        assert current[1]==previous[1]
if old_fact!=new_fact:
    assert len(after['immutable_rows']['source_fact_documents'])>len(before['immutable_rows']['source_fact_documents'])
if old_content!=new_content:
    assert len(after['immutable_rows']['source_content_manifests'])>len(before['immutable_rows']['source_content_manifests']) or all(int(c[2])==int(p[2]) for p,c in zip(old_content,new_content))
if old_feed!=new_feed:
    assert len(after['immutable_rows']['workbench_economic_facts'])>len(before['immutable_rows']['workbench_economic_facts'])
assert after['files']==before['files'], 'original review-body key or host config changed'
assert any(c['name']=='representative_read' and c['status']=='pass' for c in receipt['checks'])
print(json.dumps({'schema_version':'xinsight.ci-fpv17-to-fpv20-upgrade.v1',
                  'classification':'ci_synthetic_not_target','state':'passed',
                  'predecessor':'0.2.0-dev.fpv17','target':'0.2.0-dev.fpv20',
                  'database_schema':'46','check_no_side_effects':True,
                  'history_cursors_ciphertext_grants_preserved':True,
                  'review_body_key_preserved':True,'new_server_invocation_verified':True,
                  'active_workers_restored_by_upgrade':True,
                  'target_commit':new['commit']},sort_keys=True))
PY
