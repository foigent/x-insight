#!/usr/bin/env bash
set -euo pipefail
# Continue the normal fresh-install qualification on its disposable VM. The
# installed services remain the released binaries and units; only identities,
# TLS termination and the UR test source belong to this synthetic fixture.
if [ "$#" -ne 2 ] || [ "${GITHUB_ACTIONS:-}" != true ] || [ "${XINSIGHT_CI_EPHEMERAL_HOST:-}" != 1 ] || [ "$(id -u)" -eq 0 ]; then
  echo 'usage: disposable Actions VM: qualify-ubuntu22-installed-joint.sh ABSOLUTE_UR_CHECKOUT new_install|adopt_existing|fpv17_upgrade_prestate' >&2
  exit 2
fi
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
UR_CHECKOUT="$1"
MODE="$2"
case "$MODE" in new_install|adopt_existing|fpv17_upgrade_prestate) ;; *) exit 2 ;; esac
case "$UR_CHECKOUT" in /*) ;; *) echo 'absolute UR checkout required' >&2; exit 2 ;; esac
test -f "$UR_CHECKOUT/internal/admin/xinsight_joint_http_integration_test.go"
test -x /usr/sbin/nginx
test -x /opt/x-insight/current/bin/xinsightctl
test -f "${RUNNER_TEMP:?}/xinsight-fresh-install-qualification/qualification-result.json"
PRIVATE="$RUNNER_TEMP/xinsight-fpv15-joint-private"
EVIDENCE="$RUNNER_TEMP/xinsight-installed-joint-qualification"
test ! -e "$PRIVATE" && test ! -e "$EVIDENCE"
mkdir -m 0700 "$EVIDENCE"
STAGE=build
qualification_exit() {
  result="$?"
  trap - EXIT
  set +e
  # No raw application journal, environment, test log or credential is exported.
  python3 - "$EVIDENCE" "$STAGE" "$result" "$PRIVATE" <<'PY'
import json, pathlib, re, subprocess, sys
root, stage, code, private = pathlib.Path(sys.argv[1]), sys.argv[2], int(sys.argv[3]), pathlib.Path(sys.argv[4])
def credential_failure_counts(lines):
    # The product emits only these fixed fields. Revalidate them at the artifact
    # boundary; never copy a journal line, exception, path or request identifier.
    phases = {'client': {'dial','deadline','write','read','decode'},
              'recovery': {'validate','host_lock','host_operation','host_authorize',
                           'seed_file','seed_integrity','store_callback'}}
    categories = {'client': {'internal','context_deadline','context_cancelled','socket_missing',
                            'connection_refused','connection_closed','response_oversized','invalid_response'},
                  'recovery': {'internal','context_deadline','context_cancelled','operation_busy',
                               'unauthorized','conflict','expired','file_permission','file_missing','database_sqlstate'}}
    counts = {}
    for line in lines:
        match = re.search(r'connector credential (client|recovery) action=([a-z_]+) phase=([a-z_]+) category=([a-z_]+)(?: sqlstate=([A-Z0-9]{5}|none))?$', line)
        if not match: continue
        boundary, action, phase, category, sqlstate = match.groups()
        if (boundary == 'client' and sqlstate is not None) or (boundary == 'recovery' and sqlstate is None):
            continue
        sqlstate = sqlstate or 'none'
        if (action not in {'issue','resolve','rotate','recover','query','confirm'} or
                phase not in phases[boundary] or category not in categories[boundary] or
                sqlstate not in {'none','23505','40001','42501','55P03','57014'}):
            continue
        if boundary == 'recovery' and (action != 'issue' or ((category == 'database_sqlstate') != (sqlstate != 'none'))):
            continue
        key = (boundary,action,phase,category,sqlstate)
        counts[key] = counts.get(key,0)+1
    return [{'boundary':k[0],'action':k[1],'phase':k[2],'category':k[3],
             'sqlstate':k[4],'count':n} for k,n in sorted(counts.items())]
def setup_failure_counts(lines):
    phases = {'client': {'dial','deadline','write','read','decode','response'},
              'status': {'read_record','read_policy'}}
    categories = {'client': {'internal','context_deadline','context_cancelled','socket_missing',
                            'connection_refused','connection_closed','response_oversized',
                            'invalid_response','controller_unavailable'},
                  'status': {'internal','not_found','conflict','context_deadline',
                             'context_cancelled','database_sqlstate'}}
    counts = {}
    for line in lines:
        match = re.search(r'connector setup (client|status) action=(status) phase=([a-z_]+) category=([a-z_]+)(?: sqlstate=([A-Z0-9]{5}|none))?$', line)
        if not match: continue
        boundary, action, phase, category, sqlstate = match.groups()
        if (boundary == 'client' and sqlstate is not None) or (boundary == 'status' and sqlstate is None):
            continue
        sqlstate = sqlstate or 'none'
        if (phase not in phases[boundary] or category not in categories[boundary] or
                sqlstate not in {'none','23505','40001','42501','55P03','57014'}):
            continue
        if boundary == 'status' and ((category == 'database_sqlstate') != (sqlstate != 'none')):
            continue
        key = (boundary,action,phase,category,sqlstate)
        counts[key] = counts.get(key,0)+1
    return [{'boundary':k[0],'action':k[1],'phase':k[2],'category':k[3],
             'sqlstate':k[4],'count':n} for k,n in sorted(counts.items())]
services = {}
for unit in ('xinsight.service','xinsight-connector-controller.service','xinsight-source-facts.service','xinsight-feed.service'):
    p = subprocess.run(['sudo','-n','systemctl','show',unit,'--property=ActiveState','--property=Result','--property=ExecMainStatus'], capture_output=True, text=True, timeout=5)
    values = {}
    for line in p.stdout.splitlines():
        k, _, v = line.partition('=')
        if k=='ExecMainStatus' and v.isdigit(): values[k]=int(v)
        elif k=='ActiveState' and v in ('active','inactive','failed','activating','deactivating','reloading'): values[k]=v
        elif k=='Result' and v in ('success','exit-code','signal','resources','timeout','start-limit-hit','protocol','exec-condition'): values[k]=v
    services[unit]=values
record={'schema_version':'xinsight.ci-installed-qualification.v1','classification':'ci_synthetic_not_target',
        'state':'passed' if code==0 else 'failed','stage':stage if re.fullmatch('[a-z_]{1,40}',stage) else 'unknown',
        'exit_code':code,'services':services}
if code != 0:
    # Journal text stays in memory. Only fixed categories and bounded counts
    # leave this disposable host; messages may contain paths or credentials.
    categories = {
        'controller_maintenance_pending': re.compile(r'xinsightctl: controller maintenance pending:'),
        'managed_installation_pending': re.compile(r'managed installation .*:', re.I),
        'paused_feed_authentication_failed': re.compile(r'staged feed credential did not authenticate against the paused source'),
        'custody_release_invalid': re.compile(r'custody_load_release_invalid'),
        'custody_source_inactive': re.compile(r'custody_source_inactive|configured source worker must be active'),
        'custody_key_required': re.compile(r'restore_original_review_body_key_required'),
        'home_artifact_inaccessible': re.compile(r'/home/runner/[^\s]*.*(?:permission denied|no such file or directory)', re.I),
        'deadline_exceeded': re.compile(r'context deadline exceeded', re.I),
        'review_body_expiry_failed': re.compile(r'review body expiry failed:'),
        'publish_machine_configuration': re.compile(r'publish machine configuration:'),
        'load_machine_runtime': re.compile(r'load machine runtime:'),
        'ur_machine_runtime_pending': re.compile(r'UR machine runtime awaits a new controller invocation'),
        'qualify_original_managed_project': re.compile(r'qualify original managed project:'),
        'configure_new_managed_project': re.compile(r'configure new managed project:'),
        'prepare_managed_facts_custody': re.compile(r'prepare managed facts custody:'),
        'register_managed_facts_source': re.compile(r'register managed facts source:'),
        'managed_content_preflight': re.compile(r'managed content preflight:'),
        'source_body_capability_pending': re.compile(r'source body capability pending'),
        'source_pull_persisted': re.compile(r'"state"\s*:\s*"persisted"'),
        'source_pull_waiting_or_failed': re.compile(r'"state"\s*:\s*"waiting_or_failed"'),
        'body_consumer_proof_stale': re.compile(r'body consumer proof is stale'),
        'permission_denied': re.compile(r'permission denied', re.I),
        'operation_not_permitted': re.compile(r'operation not permitted', re.I),
        'read_only_file_system': re.compile(r'read.only file system', re.I),
        'certificate_untrusted': re.compile(r'certificate signed by unknown authority|certificate verify failed|unable to get local issuer certificate', re.I),
        'connection_refused': re.compile(r'connection refused', re.I),
    }
    diagnostics = {}
    for unit in ('xinsight.service', 'xinsight-connector-controller.service', 'xinsight-source-facts.service', 'xinsight-feed.service'):
        try:
            journal = subprocess.run(
                ['sudo', '-n', 'journalctl', '--boot=0', '--unit='+unit,
                 '--lines=120', '--no-pager', '--output=cat'],
                capture_output=True, text=True, timeout=5)
        except (OSError, subprocess.TimeoutExpired):
            diagnostics[unit] = {'availability': 'unavailable'}
            continue
        if journal.returncode != 0:
            diagnostics[unit] = {'availability': 'unavailable'}
            continue
        lines = journal.stdout.splitlines()[-120:]
        counts = {name: sum(bool(pattern.search(line)) for line in lines)
                  for name, pattern in categories.items()}
        diagnostics[unit] = {'availability': 'available', 'lines_scanned': len(lines),
                             'categories': {name: count for name, count in counts.items() if count}}
        credential_failures = credential_failure_counts(lines)
        if credential_failures:
            diagnostics[unit]['credential_failures'] = credential_failures
        setup_failures = setup_failure_counts(lines)
        if setup_failures:
            diagnostics[unit]['setup_failures'] = setup_failures
    record['journal_diagnostics'] = diagnostics
    try:
        path = private/'runtime-diagnostic.json'
        if path.stat().st_size <= 4096:
            raw = json.loads(path.read_text())
            if isinstance(raw, dict) and raw.get('schema_version') == 'xinsight.ci-installed-runtime.v1':
                safe = {'schema_version': 'xinsight.ci-installed-runtime.v1'}
                status = raw.get('failure_http_status')
                if type(status) is int and 400 <= status <= 599:
                    safe['failure_http_status'] = status
                if raw.get('failure_http_category') in ('bad_request', 'unauthenticated', 'forbidden',
                        'not_found', 'conflict', 'throttled', 'server_error', 'unexpected_status'):
                    safe['failure_http_category'] = raw['failure_http_category']
                if raw.get('failure_problem_code') in ('unclassified', 'invalid_connector_request',
                        'connector_not_found', 'connector_state_conflict',
                        'connector_credential_recovery_expired', 'connector_control_unavailable',
                        'setup_unavailable', 'invalid_setup', 'setup_changed', 'recovery_denied', 'setup_not_found'):
                    safe['failure_problem_code'] = raw['failure_problem_code']
                for key in ('setup_available', 'phases_available', 'sources_available', 'setup_applied', 'content_progress_available',
                            'credential_available', 'credential_current_expired', 'credential_current_revoked',
                            'credential_identity_expired', 'credential_identity_revoked'):
                    if type(raw.get(key)) is bool:
                        safe[key] = raw[key]
                for key in ('phases_pending', 'phases_completed', 'phases_failed', 'machines_delivered',
                            'active_streams', 'active_contents', 'accepted_bodies',
                            'credential_prepared', 'credential_committed', 'credential_superseded',
                            'content_attention', 'content_success', 'content_manifests', 'expected_manifests',
                            'copy_attempts', 'copy_retry', 'copy_policy_excluded', 'copy_scope_lost', 'copy_integrity_gap',
                            'copy_expired', 'copy_identity_pending', 'copy_binding_pending', 'legacy_copy_pending',
                            'content_pages_pending', 'content_coverage_unknown', 'content_capture_health_unknown', 'content_other_gaps'):
                    value = raw.get(key)
                    if type(value) is int and 0 <= value <= 1000000:
                        safe[key] = value
                if raw.get('installation_state') in ('none', 'unclassified', 'pending', 'abnormal',
                                                     'connected', 'paused', 'disconnected'):
                    safe['installation_state'] = raw['installation_state']
                if raw.get('setup_error') in ('none', 'unclassified', 'custody_loading',
                                              'custody_revision_changed', 'xi_actor_permission_changed'):
                    safe['setup_error'] = raw['setup_error']
                source_errors = ('none', 'unclassified', 'profile_not_authorized', 'source_paused',
                        'staged_credential_requires_pause', 'cursor_or_epoch_conflict', 'cursor_expired',
                        'source_authorization_denied', 'scope_changed', 'invalid_content_page',
                        'source_fact_conflict', 'source_unavailable', 'restore_requires_authorization',
                        'invalid_capabilities', 'invalid_page')
                if raw.get('content_error') in source_errors:
                    safe['content_error'] = raw['content_error']
                if isinstance(raw.get('source_errors'), dict):
                    safe['source_errors'] = {key: value for key, value in raw['source_errors'].items()
                        if key in source_errors and type(value) is int and 0 <= value <= 1000000}
                record['runtime_diagnostic'] = safe
    except (OSError, ValueError, TypeError):
        pass
(root/'qualification-result.json').write_text(json.dumps(record,sort_keys=True)+'\n')
print(json.dumps(record,sort_keys=True))
PY
  exit "$result"
}
trap qualification_exit EXIT
go build -o "$EVIDENCE/fixture" "$ROOT/scripts/ci-fpv15-joint-fixture"
if [ "$MODE" = adopt_existing ]; then
  go test -tags=integration -c -o "$EVIDENCE/original-prestate.test" ./internal/db
fi
(cd "$UR_CHECKOUT"; go test -tags=integration -c -o "$EVIDENCE/ur-joint.test" ./internal/admin)

STAGE=fixture
# A benchmark-only local address is outside the controller's original allow
# list. This forces the product to publish and actually load its exact /32.
sudo ip address add 198.18.0.2/32 dev lo
printf '{"ur_receiver_ip":"198.18.0.2"}\n' | "$EVIDENCE/fixture" > "$EVIDENCE/fixture-result.json"
sudo install -m 0600 "$PRIVATE/access-accounts.json" /etc/x-insight/access-accounts.json
sudo /opt/x-insight/current/bin/xinsightctl access validate --file /etc/x-insight/access-accounts.json > "$EVIDENCE/access-validation.json"
sudo install -m 0644 "$PRIVATE/ur-receiver-ca.pem" /usr/local/share/ca-certificates/xinsight-ci-ur.crt
sudo install -m 0644 "$PRIVATE/xi-proxy-ca.pem" /usr/local/share/ca-certificates/xinsight-ci-proxy.crt
sudo update-ca-certificates > "$PRIVATE/ca-update.log" 2>&1
BODY_INGRESS_LOCATION="$(python3 - "$ROOT/deploy/xinsight-nginx-ip.conf.example" <<'PY'
import pathlib, sys
lines=pathlib.Path(sys.argv[1]).read_text().splitlines(keepends=True)
start=[i for i,line in enumerate(lines) if line == '    location = /api/v1/work-event-ingest/contents {\n']
if len(start) != 1: raise SystemExit('missing or duplicate review-body Nginx location')
end=next((i for i in range(start[0]+1,len(lines)) if lines[i] == '    }\n'),None)
if end is None: raise SystemExit('unterminated review-body Nginx location')
block=''.join(lines[start[0]:end+1])
for required in ('client_max_body_size 32m;', 'proxy_request_buffering off;',
                 'proxy_pass http://127.0.0.1:8080;', 'proxy_set_header X-Forwarded-Proto https;'):
    if required not in block: raise SystemExit('review-body Nginx location is incomplete')
sys.stdout.write(block)
PY
)"
cat > "$PRIVATE/nginx.conf" <<NGINX
pid $PRIVATE/nginx.pid;
error_log $PRIVATE/nginx-error.log;
events {}
http {
  access_log off;
  server {
    listen 127.0.0.1:9443 ssl;
    ssl_certificate $PRIVATE/xi-proxy.pem;
    ssl_certificate_key $PRIVATE/xi-proxy.key;
    client_max_body_size 8k;
$BODY_INGRESS_LOCATION
    location / {
      proxy_pass http://127.0.0.1:8080;
      proxy_set_header Host 127.0.0.1:9443;
      proxy_set_header X-Forwarded-Proto https;
      proxy_set_header X-Forwarded-Host 127.0.0.1:9443;
      proxy_set_header X-Forwarded-For \$remote_addr;
    }
  }
}
NGINX
chmod 0600 "$PRIVATE/nginx.conf"
sudo /usr/sbin/nginx -t -c "$PRIVATE/nginx.conf" > "$PRIVATE/nginx-check.log" 2>&1
sudo /usr/sbin/nginx -c "$PRIVATE/nginx.conf" > "$PRIVATE/nginx-start.log" 2>&1
printf 'XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback\nXINSIGHT_EXTERNAL_ORIGIN=https://127.0.0.1:9443\n' | sudo tee /etc/x-insight/trusted-tls-proxy.env >/dev/null
sudo chmod 0644 /etc/x-insight/trusted-tls-proxy.env
sudo /opt/x-insight/current/bin/xinsightctl connector-target-config set --unirouter-origin https://198.18.0.2:9444
sudo systemctl restart xinsight-connector-controller.service xinsight.service

STAGE=prepare_input
sudo install -m 0600 -o "$(id -u)" -g "$(id -g)" /etc/x-insight/database/current/postgres-admin.dsn "$PRIVATE/xi-admin.dsn"
sudo /opt/x-insight/current/bin/xinsightctl status > "$PRIVATE/status.json"
python3 - "$PRIVATE" "$MODE" "$EVIDENCE" <<'PY'
import json, pathlib, re, subprocess, sys, time
root=pathlib.Path(sys.argv[1]); mode=sys.argv[2]; evidence=pathlib.Path(sys.argv[3]); m=json.loads((root/'manifest.json').read_text())
# Read the generated public instance identity without moving any secret into
# process arguments or artifact output.
p=subprocess.run(['sudo','-n','cat','/etc/x-insight/xinsight-connector-id.env'],capture_output=True,text=True,check=True)
ids=re.findall(r'^XINSIGHT_CONNECTOR_DEPLOYMENT_ID=(xid_[A-Za-z0-9_-]{22})$',p.stdout,re.M)
if len(ids)!=1: raise SystemExit('installed deployment identity unavailable')
import socket
with socket.socket(socket.AF_INET,socket.SOCK_DGRAM) as s:
    s.connect(('198.18.0.2',9444)); source=s.getsockname()[0]
if source!='198.18.0.2': raise SystemExit('unexpected local receiver route')
driver_mode='new_install' if mode=='fpv17_upgrade_prestate' else mode
input={'schema_version':'xinsight.ci-installed-joint.v1','mode':driver_mode,'future_project':driver_mode=='new_install',
       'xi_origin':'https://127.0.0.1:9443','xi_deployment_id':ids[0],
       'ur_listen':'198.18.0.2:9444','receiver_allowed_ip':source+'/32',
       'tls_cert_file':m['ur_cert_file'],'tls_key_file':m['ur_key_file'],'tls_ca_file':m['ur_ca_file'],
       'xi_ca_file':m['xi_ca_file'],'xi_admin_username':'joint.admin','xi_admin_password_file':m['admin_password_file'],
       'xi_reader_username':'joint.reader','xi_reader_password_file':m['reader_password_file'],
       'xi_reader_subject_id':m['reader_subject_id'],'xi_admin_dsn_file':str(root/'xi-admin.dsn'),
       'timeout_seconds':1200 if mode=='fpv17_upgrade_prestate' else 600}
path=root/'installed-input.json'
with path.open('x') as f: json.dump(input,f)
path.chmod(0o600)
if mode=='adopt_existing':
    old=json.loads((root.parent/'fpv14-ci-predecessor.json').read_text())
    inputs={k:old[k] for k in ('schema_version','candidate_ctl','candidate_artifact')}
    inputs['helper']=str(evidence/'original-prestate.test')
    with (root/'predecessor-inputs.json').open('x') as f: json.dump(inputs,f)
    (root/'predecessor-inputs.json').chmod(0o600)
deadline=time.monotonic()+30
while True:
    ready=subprocess.run(['curl','--silent','--fail','--cacert',m['xi_ca_file'],'https://127.0.0.1:9443/readyz'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    if ready.returncode==0: break
    if time.monotonic()>deadline: raise SystemExit('HTTPS service not ready')
    time.sleep(.5)
def body_probe(path,size):
    try:
        check=subprocess.run(['curl','--silent','--show-error','--max-time','20',
            '--cacert',m['xi_ca_file'],'--header','Content-Type: application/octet-stream',
            '--data-binary','@-','--output','/dev/null','--write-out','%{http_code}',
            'https://127.0.0.1:9443'+path],input=b'A'*size,capture_output=True,timeout=25)
    except subprocess.TimeoutExpired:
        return {'request_bytes':size,'http_status':None,'curl_exit':'timeout'}
    raw=check.stdout.decode('ascii',errors='ignore')
    status=int(raw) if re.fullmatch(r'[1-5][0-9]{2}',raw) else None
    return {'request_bytes':size,'http_status':status,'curl_exit':check.returncode}
probes={
    'review_body_16k':body_probe('/api/v1/work-event-ingest/contents',16384),
    'review_body_32m':body_probe('/api/v1/work-event-ingest/contents',32*1024*1024),
    'review_body_over_32m':body_probe('/api/v1/work-event-ingest/contents',32*1024*1024+1),
    'other_endpoint_16k':body_probe('/api/v1/work-event-ingest/items',16384),
}
receipt={'schema_version':'xinsight.ci-nginx-body-ingress.v1','stage':'proxy_body_boundary',
    'synthetic_only':True,'probes':probes}
(evidence/'nginx-body-ingress.json').write_text(json.dumps(receipt,sort_keys=True)+'\n')
expected={'review_body_16k':401,'review_body_32m':401,
    'review_body_over_32m':413,'other_endpoint_16k':413}
if any(probes[name]['http_status']!=status or probes[name]['curl_exit']!=0
       for name,status in expected.items()):
    raise SystemExit('Nginx body ingress boundary mismatch; see nginx-body-ingress.json')
PY

STAGE=installed_joint
# The inherited token used for checkout is not propagated to this test. The
# only database secret is read from the protected fixture file into the child
# environment. UR creates and removes its own uniquely named test databases.
python3 - "$ROOT" "$UR_CHECKOUT" "$PRIVATE" "$EVIDENCE" <<'PY'
import json, os, pathlib, re, subprocess, sys
def installed_joint_passed(returncode, mode, receipts):
    if returncode != 0 or mode not in ('new_install', 'adopt_existing', 'fpv17_upgrade_prestate'):
        return False
    if mode == 'fpv17_upgrade_prestate':
        required = {'future_project', 'service_restart', 'upgrade_authority', 'formal_upgrade',
                    'upgrade_continuity', 'upgrade_completed'}
    else:
        required = {'service_restart', 'credential_lifecycle_baseline',
                'credential_continuous_rotation', 'credential_lost_prepare',
                'credential_lost_confirm', 'credential_offline_expiry',
                'credential_expired_prepared', 'credential_revocation_boundary'}
        required.add('preserved_history' if mode == 'adopt_existing' else 'future_project')
    stages, results = set(), []
    for receipt in receipts:
        match = re.fullmatch(r'xinsight\.ci-installed-joint-(stage|result)\.v1/([a-z_]{1,40})/(passed|failed)/[0-9]{1,7}/([a-z_]{0,60})', receipt)
        if not match:
            return False
        kind, stage, state, category = match.groups()
        if state != 'passed' or category != ('none' if kind == 'result' else ''):
            return False
        if kind == 'stage':
            stages.add(stage)
        else:
            results.append(receipt)
    # A result is the driver's completion contract; the last business stage
    # may change as the lifecycle grows. Partial stage success is insufficient.
    return len(results) == 1 and receipts[-1] == results[0] and required <= stages

xi,ur,root,out=map(pathlib.Path,sys.argv[1:])
from urllib.parse import urlsplit,urlunsplit
u=urlsplit((root/'xi-admin.dsn').read_text().strip())
if u.hostname!='127.0.0.1' or u.port!=5432 or u.username!='xinsight_admin': raise SystemExit('unexpected isolated DB')
env={k:v for k,v in os.environ.items() if k in ('PATH','HOME','GOCACHE','GOMODCACHE','GOPATH','GOTOOLCHAIN','SSL_CERT_FILE')}
env.update(GITHUB_ACTIONS='true',XINSIGHT_CI_EPHEMERAL_HOST='1',XINSIGHT_CI_INSTALLED_JOINT='1',
           REQUIRE_TEST_DATABASE='1',XI_JOINT_TEST_CHECKOUT=str(xi),
           TEST_DATABASE_URL=urlunsplit((u.scheme,u.netloc,'/uniroute_test',u.query,'')))
if json.loads((root/'installed-input.json').read_text())['mode'] == 'new_install' and os.environ.get('XINSIGHT_CI_UPGRADE_PRESTATE') == '1':
    request={'schema_version':'xinsight.ci-fpv17-installed-upgrade-request.v1',
             'script':str(xi/'scripts/qualify-ubuntu22-fpv17-upgrade.sh'),
             'candidate_artifact':'/opt/xinsight-ci-artifacts/candidate.tar.gz',
             'candidate_payload':str(pathlib.Path(os.environ['RUNNER_TEMP'])/'fresh-install-payload')}
    path=root/'fpv17-upgrade-request.json'
    with path.open('x') as f: json.dump(request,f)
    path.chmod(0o600)
with (root/'installed-input.json').open('rb') as source, (root/'joint-private.log').open('xb') as log:
    os.chmod(root/'joint-private.log',0o600)
    upgrade=os.environ.get('XINSIGHT_CI_UPGRADE_PRESTATE')=='1'
    result=subprocess.run([str(out/'ur-joint.test'),'-test.run=^TestXInsightInstalledXIIntegration$','-test.v',
                           '-test.timeout='+('22m' if upgrade else '12m')],
                          cwd=ur/'internal/admin',env=env,stdin=source,stdout=log,stderr=log,
                          timeout=1340 if upgrade else 740)
raw=(root/'joint-private.log').read_text(errors='replace')
receipts=re.findall(r'xinsight\.ci-installed-joint-(?:stage|result)\.v1/[a-z_]{1,40}/(?:passed|failed)/[0-9]{1,7}/[a-z_]{0,60}',raw)
# Preserve the producer's bounded omission counts alongside its safe receipts.
# A longer run or an invalid private line must not silently erase diagnostics.
omissions={key:max((int(v) for v in re.findall(r'\b'+key+r'=([0-9]{1,7})\b',raw)),default=0)
           for key in ('truncated_valid_receipts','invalid_rows_omitted')}
# These are literal stages already emitted by the UR fixture. Do not copy its
# free-form error or raw output, and do not infer that publication happened.
ur_wait='none'
for label,stage in (('Source C enrollment or existing C binding','content_binding'),('real Source A page','source_bootstrap')):
    if 'installed joint failed before '+label+' category=' in raw:
        ur_wait=stage
ur_failure='none'
for category in ('xi_driver_exit','ur_control_failed','deadline'):
    if 'category='+category+' receipts=' in raw:
        ur_failure=category
# The UR fixture may fail an assertion after the XI child has emitted its
# terminal passed receipt. Export only bounded source line numbers, never the
# assertion text or private test log, so that this case remains diagnosable.
fixture_assertion_lines=sorted({int(line) for line in re.findall(r'xinsight_joint_http_integration_test\.go:([1-9][0-9]{0,3})',raw)})[:8] if result.returncode else []
safe={'receipts':receipts,'ur_wait':ur_wait,'ur_failure':ur_failure,'fixture_assertion_lines':fixture_assertion_lines,**omissions}
(out/'stage-receipts.json').write_text(json.dumps({'schema_version':'xinsight.ci-installed-stages.v1',**safe},sort_keys=True)+'\n')
print(json.dumps({'stage':'installed_joint','exit_code':result.returncode,**safe},sort_keys=True))
mode='fpv17_upgrade_prestate' if os.environ.get('XINSIGHT_CI_UPGRADE_PRESTATE') == '1' else json.loads((root/'installed-input.json').read_text())['mode']
if not installed_joint_passed(result.returncode, mode, receipts):
    raise SystemExit('installed joint failed; see safe stage receipts')
PY
STAGE=complete
