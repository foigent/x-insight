#!/usr/bin/env bash
set -euo pipefail
# Normal signed managed FPV11/schema43 -> FPV12/schema44. The exact unmanaged
# FPV9 -> FPV11 script remains historical and is never relabelled/reused here.
CHECK_ONLY=false
if [ "${1:-}" = --check-inputs ]; then CHECK_ONLY=true; shift; fi
if [ "$#" -ne 6 ]; then
 echo "usage: qualify-ubuntu22-managed-fpv11.sh [--check-inputs] TARGET_ARTIFACT TARGET_PAYLOAD TRUST_STORE OLD_ARTIFACT OLD_PAYLOAD OLD_SOURCE" >&2; exit 2
fi
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TARGET_ARTIFACT="$1"; TARGET_PAYLOAD="$2"; TRUST_STORE="$3"
OLD_ARTIFACT="$4"; OLD_PAYLOAD="$5"; OLD_SOURCE="$6"
for input in "$@"; do case "$input" in /*) ;; *) echo "absolute inputs required" >&2; exit 1 ;; esac; done
test -f "$TARGET_ARTIFACT" && test -f "$OLD_ARTIFACT" && test -f "$TRUST_STORE"
test -d "$TARGET_PAYLOAD/bin" && test -d "$OLD_PAYLOAD/bin" && test -d "$OLD_SOURCE/migrations"
python3 "$ROOT/scripts/reproduce-signed-predecessor-source.py" --check-built-source-directory fpv11 "$OLD_SOURCE"
python3 - "$TARGET_PAYLOAD/release/compatibility-matrix.json" "$OLD_PAYLOAD/release/compatibility-matrix.json" <<'PYINPUT'
import json,sys
current,old=(json.load(open(p)) for p in sys.argv[1:])
assert (current['product_version'],current['database_schema'])==('0.2.0-dev.fpv12','44')
assert (old['product_version'],old['database_schema'])==('0.2.0-dev.fpv11','43')
assert old['product_version'] in current['supported_upgrade_from_versions']
PYINPUT
if "$CHECK_ONLY"; then echo 'managed_fpv11_inputs=checked_no_mutation'; exit 0; fi
if [ "${GITHUB_ACTIONS:-}" != true ] || [ "${XINSIGHT_CI_EPHEMERAL_HOST:-}" != 1 ] || [ "$(id -u)" -eq 0 ]; then
 echo "qualification requires unprivileged caller on explicit disposable Actions VM" >&2; exit 1
fi
# Use the runner's existing tools; fail before installing the predecessor.
for tool in /usr/bin/nsenter /usr/bin/setpriv /usr/bin/env /usr/bin/timeout; do
 test -x "$tool" || { echo 'managed credential probe tools unavailable' >&2; exit 1; }
done
QUAL_ROOT="${RUNNER_TEMP:?}/xinsight-managed-fpv11-qualification"
test ! -e "$QUAL_ROOT"; mkdir -m 0700 "$QUAL_ROOT"
CTL="$TARGET_PAYLOAD/bin/xinsightctl"
CTL_PATH="/usr/lib/postgresql/16/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
# Failure evidence stays within this disposable qualification VM. Export only
# selected systemd properties and fixed error categories, never raw journal,
# ExecStart/environment, credential files, SQL errors, or product bodies.
QUAL_STAGE=predecessor_install
QUAL_STARTED="$(date +%s)"
qualification_exit() {
 local result="$?"
 trap - EXIT
 set +e # Diagnostic failure must never replace the qualification exit status.
 if [ "$result" -ne 0 ]; then
  sudo python3 - "$QUAL_STAGE" "$result" "$QUAL_STARTED" > "$QUAL_ROOT/service-failure-diagnostic.json" <<'PYDIAG'
import json, os, re, subprocess, sys

UNITS = ('xinsight.service', 'xinsight-connector-controller.service', 'xinsight-source-facts.service')
PROPERTIES = ('LoadState', 'ActiveState', 'SubState', 'Result', 'ExecMainCode', 'ExecMainStatus', 'MainPID', 'NRestarts', 'ExecStartPre', 'ExecStartPost')
LIMIT = 128 * 1024
PROBE_PANICS = {
    'isolated root loaded-key check required': 'probe_root_loaded_check_context_invalid',
    'real managed paths required': 'probe_managed_paths_invalid',
    'isolated real Linux host publisher required': 'probe_publisher_context_invalid',
    'exact public partition input required': 'probe_partition_input_invalid',
    'public partition input unavailable': 'probe_partition_input_unavailable',
    'credential proof requires isolated systemd non-root service': 'probe_consumer_context_invalid',
    'missing actual systemd credentials directory': 'probe_credentials_directory_missing',
    'exact actual service consumer required': 'probe_consumer_argument_invalid',
    'actual consumer credential directory required': 'probe_credentials_directory_mismatch',
    'actual workspace credential rejected': 'probe_workspace_credential_rejected',
    'automatic original root binding required': 'probe_original_binding_invalid',
    'consumer read root custody credential directly': 'probe_direct_root_custody_access_allowed',
    'expected exact root-owned managed partition': 'probe_managed_partition_invalid',
    'worker read root source credential directly': 'probe_direct_root_source_access_allowed',
    'worker read root managed credential directly': 'probe_direct_root_managed_access_allowed',
    'consumer receipt write failed': 'probe_receipt_write_failed',
}
SAFE_ERRORS = {
    'review body key file is invalid': 'review_body_key_invalid',
    'review body key id and file must be configured together': 'review_body_binding_incomplete',
    'connector deployment identity is invalid': 'connector_identity_invalid',
    'connector installation configuration is invalid': 'connector_configuration_invalid',
    'managed release marker cannot be inspected': 'launcher_release_marker_unavailable',
    'managed release marker is not a regular file': 'launcher_release_marker_invalid',
}
STATE_VALUES = {
    'LoadState': {'loaded', 'not-found', 'bad-setting', 'error', 'masked', 'merged', 'stub'},
    'ActiveState': {'active', 'reloading', 'inactive', 'failed', 'activating', 'deactivating', 'maintenance'},
    'SubState': {'dead', 'running', 'exited', 'failed', 'start-pre', 'start', 'start-post', 'auto-restart', 'auto-restart-queued', 'stop', 'stop-sigterm', 'stop-sigkill', 'stop-post', 'final-sigterm', 'final-sigkill', 'reload', 'cleaning', 'condition'},
    'Result': {'success', 'resources', 'timeout', 'exit-code', 'signal', 'core-dump', 'watchdog', 'start-limit-hit', 'oom-kill', 'protocol', 'exec-condition', 'assert'},
}

def bounded_command(args):
    process = None
    try:
        process = subprocess.Popen(['/usr/bin/timeout', '4s', *args], stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
        raw = process.stdout.read(LIMIT + 1)
        truncated = len(raw) > LIMIT
        if truncated:
            process.terminate()
        try:
            code = process.wait(timeout=1)
        except subprocess.TimeoutExpired:
            process.kill()
            code = process.wait()
        return raw[:LIMIT].decode('utf-8', errors='replace'), code, truncated
    except (OSError, subprocess.SubprocessError):
        return '', None, False
    finally:
        if process is not None:
            if process.poll() is None:
                process.kill()
                process.wait()
            process.stdout.close()

def selected_properties(raw):
    result = {}
    for line in raw.splitlines():
        key, sep, value = line.partition('=')
        if not sep or key not in PROPERTIES:
            continue
        if key in ('ExecStartPre', 'ExecStartPost'):
            exits = []
            for block in re.findall(r'\{[^{}]*\}', value):
                status = re.search(r'code=(exited|killed|dumped)\s*;\s*status=([0-9]+)', block)
                if not status:
                    continue
                validator = 'other'
                if key == 'ExecStartPre':
                    for name in ('connector-target-config', 'review-body-config'):
                        if 'xinsightctl ' + name + ' validate' in block:
                            validator = name
                else:
                    probe = re.search(r'argv\[\]=/usr/local/bin/xinsight-ci-managed-credential-proof --(reader|worker)(?:\s*;)', block)
                    if probe:
                        validator = 'credential-probe-' + probe[1]
                exits.append({'validator': validator, 'code': status[1], 'status': int(status[2])})
            result['startup_command_exits' if key == 'ExecStartPre' else 'post_start_command_exits'] = exits
        elif key in ('ExecMainCode', 'ExecMainStatus', 'MainPID', 'NRestarts'):
            result[key] = int(value) if re.fullmatch(r'[0-9]{1,12}', value) else None
        else:
            result[key] = value if value in STATE_VALUES[key] else 'unavailable'
    return result

def error_category(message):
    # Classify only actual emitted errors. Unrecognized text is never echoed or
    # treated as a diagnosed cause; database errors may contain credentials.
    for unit in UNITS:
        if message.startswith(unit + ': '):
            message = message[len(unit) + 2:]
            break
    if message.startswith('panic: '):
        return PROBE_PANICS.get(message[len('panic: '):])
    for prefix in ('xinsight-server: ', 'xinsight-launcher: ', 'xinsightctl: '):
        if message.startswith(prefix):
            error = message[len(prefix):]
            if error in SAFE_ERRORS:
                return SAFE_ERRORS[error]
            if error.startswith('load release migration identity: '):
                return 'release_migration_identity_load_failed'
            if error.startswith('failed to connect to '):
                if 'password authentication failed' in error:
                    return 'postgres_password_authentication_failed'
                return 'postgres_connection_failed'
            if prefix == 'xinsightctl: ':
                return 'startup_validator_error_unclassified'
            return prefix.split(':')[0] + '_error_unclassified'
    for text, category in (
        ('Failed to load environment files:', 'systemd_environment_file_failed'),
        ('Failed to set up credentials:', 'systemd_credential_setup_failed'),
        ('Main process exited,', 'systemd_main_process_exit'),
        ('Control process exited,', 'systemd_control_process_exit'),
        ('Failed with result ', 'systemd_unit_failed'),
    ):
        if message.startswith(text):
            return category
    step = re.match(r'Failed at step ([A-Z_]+) ', message)
    if step and step[1] in ('EXEC', 'CREDENTIALS', 'CHDIR', 'USER', 'GROUP', 'NAMESPACE'):
        return 'systemd_step_' + step[1].lower() + '_failed'
    return None

def probe_callsite(message):
    match = re.fullmatch(r'\s+(?:/[A-Za-z0-9._-]+)*/ci-managed-source-credential-proof\.go:([1-9][0-9]{0,5})(?: \+0x[0-9a-f]{1,16})?\s*', message)
    return int(match[1]) if match else None

def service_diagnostic(unit, since):
    raw, code, truncated = bounded_command(['systemctl', 'show', unit, '--no-pager', *['--property=' + p for p in PROPERTIES]])
    result = {'unit': unit, 'properties': selected_properties(raw), 'state_command_exit': code, 'state_truncated': truncated}
    raw, code, truncated = bounded_command(['journalctl', '--unit=' + unit, '--since=@' + since, '--no-pager', '--output=json', '--lines=40'])
    categories, seen, unknown, callsites = {}, 0, 0, set()
    for line in raw.splitlines():
        try:
            entry = json.loads(line)
        except (ValueError, TypeError):
            unknown += 1
            continue
        if not isinstance(entry, dict):
            unknown += 1
            continue
        message = entry.get('MESSAGE')
        if not isinstance(message, str):
            unknown += 1
            continue
        seen += 1
        callsite = probe_callsite(message)
        if callsite is not None:
            callsites.add(callsite)
            continue
        category = error_category(message)
        if category:
            categories[category] = categories.get(category, 0) + 1
        else:
            unknown += 1
    result.update(error_categories=categories, probe_callsites=sorted(callsites), journal_messages_seen=seen, unclassified_messages=unknown, journal_command_exit=code, journal_truncated=truncated)
    return result

if __name__ == '__main__':
    if os.geteuid() != 0 or len(sys.argv) != 4 or not re.fullmatch(r'[a-z_]{1,40}', sys.argv[1]) or not all(re.fullmatch(r'[0-9]{1,12}', x) for x in sys.argv[2:]):
        raise SystemExit(2)
    print(json.dumps({'schema_version': 'xinsight.ci-managed-service-failure.v1', 'classification': 'ci_synthetic_not_target', 'stage': sys.argv[1], 'qualification_exit': int(sys.argv[2]), 'journal_window': 'since_qualification_start_last_40_messages_per_unit', 'services': [service_diagnostic(unit, sys.argv[3]) for unit in UNITS]}, indent=2, sort_keys=True))
PYDIAG
  local diagnostic_result="$?"
  if [ "$diagnostic_result" -ne 0 ]; then
   printf '{"schema_version":"xinsight.ci-managed-service-failure.v1","classification":"ci_synthetic_not_target","qualification_exit":%s,"diagnostic_exit":%s,"diagnostic":"unavailable"}\n' "$result" "$diagnostic_result" > "$QUAL_ROOT/service-failure-diagnostic.json"
  fi
  if [ -s "$QUAL_ROOT/service-failure-diagnostic.json" ]; then cat "$QUAL_ROOT/service-failure-diagnostic.json" >&2; fi
 fi
 exit "$result"
}
trap qualification_exit EXIT
# ExecStartPost lacks EXEC_WRITE_CREDENTIALS in systemd 249. Inspect the live
# main process instead; this proves its mount/UID view, not product key loading.
# The independent original --check-loaded remains the in-memory authority.
run_credential_probe() {
 sudo env GITHUB_ACTIONS=true XINSIGHT_CI_EPHEMERAL_HOST=1 python3 - "$1" <<'PYPROBE'
import grp, json, os, pwd, re, signal, subprocess, sys

CONSUMERS = {
    'xinsight.service': ('--reader', 'xinsight-server'),
    'xinsight-source-facts.service': ('--worker', 'xinsight-importer'),
}
LIMIT = 128 * 1024
RECEIPT_LIMIT = 16 * 1024
PROCESS_CHECKS = ('uid_matches', 'gid_matches', 'no_new_privileges', 'effective_caps_empty',
                  'permitted_caps_empty', 'ambient_caps_empty', 'executable_readable', 'executable_matches')
IDENTITY_STAGES = ('before_probe_identity', 'after_probe_identity', 'after_receipt_identity')
EXECUTABLE_KINDS = ('current_importer', 'current_server', 'managed_launcher', 'systemd_startup', 'other', 'unavailable')

class ProbeFailure(Exception):
    def __init__(self, category, code=None, callsites=(), truncated=False, checks=None, executable_kind=None, stage=None, receipt=None):
        self.safe = dict(category=category, exit_code=code, probe_callsites=sorted(set(callsites)), truncated=truncated)
        if checks is not None:
            self.safe['process_checks'] = {key: checks[key] for key in PROCESS_CHECKS if type(checks.get(key)) is bool}
        if executable_kind in EXECUTABLE_KINDS:
            self.safe['executable_kind'] = executable_kind
        if stage in IDENTITY_STAGES:
            self.safe['stage'] = stage
        if receipt is not None:
            self.safe['probe_receipt'] = receipt

def bounded_probe_command(args, pass_fds=()):
    process = subprocess.Popen(['/usr/bin/timeout', '--kill-after=1s', '4s', *args],
                               stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                               env={}, pass_fds=pass_fds, start_new_session=True)
    try:
        raw = process.stdout.read(LIMIT + 1)
        truncated = len(raw) > LIMIT
        if truncated:
            os.killpg(process.pid, signal.SIGKILL)
        code = process.wait(timeout=1)
        return raw[:LIMIT].decode('utf-8'), code, truncated
    finally:
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait()
        process.stdout.close()

def live_identity(unit, uid, gid, executable, stage='before_probe_identity'):
    raw, code, truncated = bounded_probe_command([
        '/usr/bin/systemctl', 'show', unit, '--no-pager',
        '--property=ActiveState,MainPID,InvocationID,User,Group'])
    values = dict(line.split('=', 1) for line in raw.splitlines() if '=' in line)
    if (code != 0 or truncated or values.get('ActiveState') != 'active' or
        values.get('User') != 'xinsight' or values.get('Group') != 'xinsight' or
        not re.fullmatch(r'[1-9][0-9]{0,9}', values.get('MainPID', '')) or
        not re.fullmatch(r'[0-9a-f]{32}', values.get('InvocationID', ''))):
        raise ProbeFailure('live_service_identity_invalid', stage=stage)
    pid = int(values['MainPID'])
    proc = '/proc/' + str(pid)
    with open(proc + '/status') as source:
        status = dict(line.strip().split(':', 1) for line in source if ':' in line)
    checks = dict(uid_matches=status.get('Uid', '').split() == [str(uid)] * 4,
                  gid_matches=status.get('Gid', '').split() == [str(gid)] * 4,
                  no_new_privileges=status.get('NoNewPrivs', '').strip() == '1',
                  effective_caps_empty=bool(re.fullmatch(r'0{16}', status.get('CapEff', '').strip())),
                  permitted_caps_empty=bool(re.fullmatch(r'0{16}', status.get('CapPrm', '').strip())),
                  ambient_caps_empty=bool(re.fullmatch(r'0{16}', status.get('CapAmb', '').strip())))
    try:
        observed_executable = os.readlink(proc + '/exe')
        checks['executable_readable'] = True
    except OSError:
        observed_executable = None
        checks['executable_readable'] = False
    checks['executable_matches'] = observed_executable == executable
    executable_kind = 'unavailable'
    if observed_executable is not None:
        # Only finite labels survive. Never emit the observed or expected path.
        executable_kind = {
            os.path.realpath('/opt/x-insight/current/bin/xinsight-importer'): 'current_importer',
            os.path.realpath('/opt/x-insight/current/bin/xinsight-server'): 'current_server',
            os.path.realpath('/opt/x-insight/control/xinsight-launcher'): 'managed_launcher',
            '/lib/systemd/systemd': 'systemd_startup',
            '/usr/lib/systemd/systemd': 'systemd_startup',
        }.get(observed_executable, 'other')
    if not all(checks.values()):
        raise ProbeFailure('live_process_identity_invalid', checks=checks, executable_kind=executable_kind, stage=stage)
    with open(proc + '/stat') as source:
        fields = source.read().rsplit(')', 1)[1].split()
    if len(fields) < 20 or not re.fullmatch(r'[1-9][0-9]{0,19}', fields[19]):
        raise ProbeFailure('live_process_start_time_invalid', stage=stage)
    namespace = os.stat(proc + '/ns/mnt')
    return dict(main_pid=pid, invocation_id=values['InvocationID'], uid=uid, gid=gid,
                start_ticks=int(fields[19]), mount_namespace_device=namespace.st_dev,
                mount_namespace_inode=namespace.st_ino)

def strict_receipt(raw):
    if not isinstance(raw, str) or len(raw.encode('utf-8')) > RECEIPT_LIMIT:
        raise ProbeFailure('probe_receipt_invalid')
    def unique_fields(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError('duplicate field')
            result[key] = value
        return result
    def invalid_constant(value):
        raise ValueError('invalid JSON constant')
    try:
        return json.loads(raw, object_pairs_hook=unique_fields, parse_constant=invalid_constant)
    except (ValueError, RecursionError):
        raise ProbeFailure('probe_receipt_invalid')

def project_probe_receipt(unit, raw, owner):
    invalid = {'status': 'invalid'}
    if (unit not in CONSUMERS or not isinstance(raw, dict) or
        raw.get('schema_version') != 'xinsight.ci-managed-credential-proof.v1' or
        raw.get('classification') != 'ci_linux_synthetic_no_source_call' or
        raw.get('consumer') != unit or type(raw.get('uid')) is not int or
        not 0 < raw['uid'] == owner <= 2**32-1 or raw.get('review_body_key') not in ('loaded', 'rejected')):
        return invalid
    metadata = raw.get('review_body_credential')
    if not isinstance(metadata, dict) or metadata.get('form') not in ('unavailable', 'unsupported', 'root_0440_acl_candidate', 'service_owned_0400_native_candidate'):
        return invalid
    safe = {'form': metadata['form']}
    for field in ('regular', 'read_only'):
        if field in metadata:
            if type(metadata[field]) is not bool:
                return invalid
            safe[field] = metadata[field]
    for field in ('uid', 'gid', 'links'):
        if field in metadata:
            if type(metadata[field]) is not int or not 0 <= metadata[field] <= 2**32-1:
                return invalid
            safe[field] = metadata[field]
    if 'mode' in metadata:
        if not isinstance(metadata['mode'], str) or not re.fullmatch(r'[0-7]{4}', metadata['mode']):
            return invalid
        safe['mode'] = metadata['mode']
    result = {'status': 'present', 'current_invocation_verified': False,
              'uid': raw['uid'], 'review_body_key': raw['review_body_key'], 'review_body_credential': safe}
    for field, allowed in (('workspace_credential', ('loaded',)), ('direct_root_key_access', ('rejected',)), ('systemd_credentials', ('loaded',))):
        if field in raw:
            if raw[field] not in allowed:
                return invalid
            result[field] = raw[field]
    return result

def rejected_receipt(raw, unit, uid):
    # The Go producer writes one fixed JSON line before panic on key rejection.
    # The existing pipe also captures stderr; never echo it or accept this as a
    # successful proof. Only the same execution's bounded first JSON line is
    # eligible for closed diagnostic projection.
    try:
        proof = strict_receipt(raw.split('\n', 1)[0])
        if not isinstance(proof, dict) or proof.get('review_body_key') != 'rejected':
            return {'status': 'invalid'}
        return project_probe_receipt(unit, proof, uid)
    except ProbeFailure:
        return {'status': 'invalid'}

def loaded_receipt(raw, unit, uid):
    # Success requires exactly one JSON document from this execution's pipe;
    # any stderr/trailing text, duplicate field or oversized receipt rejects it.
    proof = strict_receipt(raw)
    exact = dict(schema_version='xinsight.ci-managed-credential-proof.v1',
                 classification='ci_linux_synthetic_no_source_call', consumer=unit, uid=uid,
                 review_body_key='loaded', workspace_credential='loaded',
                 direct_root_key_access='rejected', systemd_credentials='loaded')
    if not isinstance(proof, dict) or any(type(proof.get(k)) is not type(v) or proof[k] != v for k, v in exact.items()):
        raise ProbeFailure('probe_receipt_invalid')
    metadata = proof.get('review_body_credential')
    if not isinstance(metadata, dict):
        raise ProbeFailure('probe_receipt_invalid')
    form = metadata.get('form')
    owner, mode = (0, '0440') if form == 'root_0440_acl_candidate' else (uid, '0400')
    safe_metadata = dict(form=form, regular=True, read_only=True, uid=owner, gid=0, mode=mode, links=1)
    if (form not in ('root_0440_acl_candidate', 'service_owned_0400_native_candidate') or
        any(type(metadata.get(k)) is not type(v) or metadata[k] != v for k, v in safe_metadata.items())):
        raise ProbeFailure('probe_receipt_invalid')
    exact['review_body_credential'] = safe_metadata
    if unit == 'xinsight-source-facts.service':
        for k, value in dict(root_owned_config=True, original_indexed_keys=9, managed_partition_keys=1).items():
            if type(proof.get(k)) is not type(value) or proof[k] != value:
                raise ProbeFailure('probe_receipt_invalid')
            exact[k] = value
    return exact

def run_probe(unit):
    if unit not in CONSUMERS:
        raise ProbeFailure('probe_consumer_invalid')
    uid, gid = pwd.getpwnam('xinsight').pw_uid, grp.getgrnam('xinsight').gr_gid
    if uid <= 0 or gid <= 0:
        raise ProbeFailure('probe_account_invalid')
    mode, binary = CONSUMERS[unit]
    executable = os.path.realpath('/opt/x-insight/current/bin/' + binary)
    before = live_identity(unit, uid, gid, executable)
    namespace_fd = os.open('/proc/' + str(before['main_pid']) + '/ns/mnt', os.O_RDONLY)
    try:
        namespace = os.fstat(namespace_fd)
        if (namespace.st_dev, namespace.st_ino) != (before['mount_namespace_device'], before['mount_namespace_inode']):
            raise ProbeFailure('probe_namespace_changed')
        raw, code, truncated = bounded_probe_command([
            '/usr/bin/nsenter', '--mount=/proc/self/fd/' + str(namespace_fd), '--',
            '/usr/bin/setpriv', '--reuid=' + str(uid), '--regid=' + str(gid), '--clear-groups',
            '--no-new-privs', '--inh-caps=-all', '--ambient-caps=-all', '--bounding-set=-all',
            '/usr/bin/env', '-i', 'GITHUB_ACTIONS=true', 'XINSIGHT_CI_EPHEMERAL_HOST=1',
            'CREDENTIALS_DIRECTORY=/run/credentials/' + unit,
            'XINSIGHT_REVIEW_BODY_SOURCE_KEY_FILE=/etc/x-insight/review-body.key',
            '/usr/local/bin/xinsight-ci-managed-credential-proof', mode], (namespace_fd,))
        # Never export raw stderr, paths, addresses, or arbitrary panic text.
        callsites = []
        for line in raw.splitlines()[:40]:
            match = re.fullmatch(r'\s+(?:/[A-Za-z0-9._-]+)*/ci-managed-source-credential-proof\.go:([1-9][0-9]{0,5})(?: \+0x[0-9a-f]{1,16})?\s*', line)
            if match:
                callsites.append(int(match[1]))
        try:
            after = live_identity(unit, uid, gid, executable, 'after_probe_identity')
        except ProbeFailure as error:
            raise ProbeFailure(error.safe['category'], code, callsites, truncated, checks=error.safe.get('process_checks'), executable_kind=error.safe.get('executable_kind'), stage=error.safe.get('stage'))
        except (OSError, ValueError, KeyError, IndexError):
            raise ProbeFailure('probe_context_unavailable', code, callsites, truncated, stage='after_probe_identity')
        if after != before:
            raise ProbeFailure('probe_process_changed', code, callsites, truncated, stage='after_probe_identity')
        if code != 0 or truncated:
            raise ProbeFailure('credential_probe_failed', code, callsites, truncated,
                               receipt=rejected_receipt(raw, unit, uid) if not truncated else {'status': 'invalid'})
        proof = loaded_receipt(raw, unit, uid)
        if live_identity(unit, uid, gid, executable, 'after_receipt_identity') != before:
            raise ProbeFailure('probe_process_changed', stage='after_receipt_identity')
        proof['probe_context'] = dict(before, current_invocation_verified=True,
                                      scope='main_process_mount_and_uid_view')
        return proof
    finally:
        os.close(namespace_fd)

if __name__ == '__main__':
    try:
        if (os.geteuid() != 0 or os.environ.get('GITHUB_ACTIONS') != 'true' or
            os.environ.get('XINSIGHT_CI_EPHEMERAL_HOST') != '1' or
            os.environ.get('XINSIGHTCTL_TEST_ROOT') or len(sys.argv) != 2):
            raise ProbeFailure('probe_orchestrator_context_invalid')
        print(json.dumps(run_probe(sys.argv[1]), sort_keys=True))
    except (ProbeFailure, OSError, ValueError, TypeError, KeyError, IndexError, RecursionError, subprocess.SubprocessError) as error:
        safe = error.safe if isinstance(error, ProbeFailure) else ProbeFailure('probe_context_unavailable').safe
        safe.setdefault('consumer', sys.argv[1] if len(sys.argv) == 2 and sys.argv[1] in CONSUMERS else 'not_selected')
        safe.setdefault('stage', 'unavailable')
        print(json.dumps(dict(schema_version='xinsight.ci-managed-credential-probe-failure.v1', **safe), sort_keys=True), file=sys.stderr)
        raise SystemExit(1)
PYPROBE
}
# Reuse the exact predecessor's genuine managed installer and its old checks.
"$OLD_SOURCE/scripts/qualify-ubuntu22-fresh-install.sh" "$OLD_ARTIFACT" "$OLD_PAYLOAD" "$TRUST_STORE" > "$QUAL_ROOT/predecessor-install.txt"
sudo env PATH="$CTL_PATH" "$CTL" status > "$QUAL_ROOT/predecessor-status.json"
python3 - "$QUAL_ROOT/predecessor-status.json" <<'PYOLD'
import json,sys
s=json.load(open(sys.argv[1])); assert s['managed'] and s['install_state']['current']['product_version']=='0.2.0-dev.fpv11'
assert s['install_state']['database_schema_current']=='43'
PYOLD
# Use the task-created database's existing owner-only credential. Never export
# a DSN/password in argv, receipts or logs.
pg() { sudo env GITHUB_ACTIONS=true XINSIGHT_CI_EPHEMERAL_HOST=1 XINSIGHT_ADMIN_POSTGRES_DSN_FILE=/etc/x-insight/database/current/postgres-admin.dsn python3 "$ROOT/scripts/ci-managed-pg.py" "$@"; }
compare_restored_snapshot() {
 python3 - "$1" "$2" <<'PYRESTORED'
import copy, json, sys

def verify_restored_snapshot(before, after):
    fields = {'streams', 'content', 'manifests', 'receipts', 'evidence'}
    if not isinstance(before, dict) or not isinstance(after, dict) or set(before) != fields or set(after) != fields:
        raise ValueError('restored snapshot has unexpected fields')
    expected = copy.deepcopy(before)
    # Match SuspendRestoredSourceFacts/Contents: pause current authority;
    # preserve revoked registrations and every recorded identity/checkpoint/fact.
    for stream in expected['streams']:
        if stream[6] != 'revoked':
            stream[6] = 'paused'
    if expected['content'] is not None and expected['content'][7] != 'revoked':
        expected['content'][7] = 'paused'
    changed = sorted(field for field in fields if expected[field] != after[field])
    if changed:
        raise ValueError('restored snapshot violated preservation or paused authority: ' + ','.join(changed))

if __name__ == '__main__':
    try:
        verify_restored_snapshot(*(json.load(open(path)) for path in sys.argv[1:]))
    except (ValueError, TypeError, IndexError, KeyError) as error:
        raise SystemExit(str(error))
    print('restored_snapshot=history_preserved_authority_paused')
PYRESTORED
}
SOURCE_ID=11111111-1111-4111-8111-111111111111
STREAM_ID=22222222-2222-4222-8222-222222222222
COLLECTION_ID=7996279c-df2b-4c0d-aaba-7c2a3b3d510b
SOURCE_IP=203.0.113.10
sudo ip -4 route add blackhole "$SOURCE_IP/32"
pg seed > "$QUAL_ROOT/predecessor-seed.txt"
go build -o "$QUAL_ROOT/content-seed" "$ROOT/scripts/ci-direct-fpv9-source-roles.go"
sudo env GITHUB_ACTIONS=true XINSIGHT_CI_EPHEMERAL_HOST=1 XINSIGHT_ADMIN_POSTGRES_DSN_FILE=/etc/x-insight/database/current/postgres-admin.dsn "$QUAL_ROOT/content-seed" seed-content "$SOURCE_ID" "$COLLECTION_ID" > "$QUAL_ROOT/predecessor-content.txt"
sudo install -m 0600 /dev/null /etc/x-insight/ci-source.key
sudo install -m 0600 /dev/null /etc/x-insight/ci-content.key
for key in ci-source.key ci-content.key; do printf 'ci-only-%s\n' "$(openssl rand -hex 24)" | sudo tee "/etc/x-insight/$key" >/dev/null; done
python3 "$ROOT/scripts/ci-managed-pg.py" config > "$QUAL_ROOT/source-facts.json"
sudo install -m 0644 "$QUAL_ROOT/source-facts.json" /etc/x-insight/source-facts.json
sudo install -d -m 0755 /etc/systemd/system/xinsight-source-facts.service.d
{ printf '[Service]\n'; for index in $(seq 0 7); do printf 'LoadCredential=source-profile-%s.key:/etc/x-insight/ci-source.key\n' "$index"; done; printf 'LoadCredential=source-profile-8.key:/etc/x-insight/ci-content.key\n'; } | sudo tee /etc/systemd/system/xinsight-source-facts.service.d/10-source-credentials.conf >/dev/null
sudo systemctl daemon-reload
pg snapshot > "$QUAL_ROOT/before.json"
sudo sha256sum /etc/x-insight/access-accounts.json /etc/x-insight/source-facts.json /etc/x-insight/ci-source.key /etc/x-insight/ci-content.key > "$QUAL_ROOT/before-files.sha256"
test ! -e /etc/x-insight/review-body.key && test ! -e /etc/x-insight/xinsight-review-body.env
QUAL_STAGE=upgrade_check
sudo env PATH="$CTL_PATH" "$CTL" upgrade --check --artifact "$TARGET_ARTIFACT" > "$QUAL_ROOT/upgrade-check.json"
test ! -e /etc/x-insight/review-body.key && test ! -e /etc/x-insight/xinsight-review-body.env
pg snapshot > "$QUAL_ROOT/after-check.json"; cmp "$QUAL_ROOT/before.json" "$QUAL_ROOT/after-check.json"
sudo sha256sum /etc/x-insight/access-accounts.json /etc/x-insight/source-facts.json /etc/x-insight/ci-source.key /etc/x-insight/ci-content.key > "$QUAL_ROOT/after-check-files.sha256"
cmp "$QUAL_ROOT/before-files.sha256" "$QUAL_ROOT/after-check-files.sha256"
QUAL_STAGE=sigkill_upgrade
# Reuse the existing exact SIGKILL systemctl seam, not test-root injection.
CRASH_BIN="$QUAL_ROOT/sigkill-bin"; CRASH_MARKER="$QUAL_ROOT/sigkill-ready.pid"
mkdir -m 0700 "$CRASH_BIN"; ln -s "$ROOT/scripts/ci-systemctl-sigkill-wrapper.sh" "$CRASH_BIN/systemctl"
sudo env GITHUB_ACTIONS=true XINSIGHT_CI_EPHEMERAL_HOST=1 RUNNER_TEMP="$RUNNER_TEMP" XINSIGHT_CI_SIGKILL_ACTION=restart_service_reload XINSIGHT_CI_SIGKILL_MARKER="$CRASH_MARKER" XINSIGHT_CI_EXPECTED_CTL="$CTL" PATH="$CRASH_BIN:$CTL_PATH" setsid --wait "$CTL" upgrade --artifact "$TARGET_ARTIFACT" > "$QUAL_ROOT/upgrade-command.txt" 2>&1 &
CRASH_JOB_PID="$!"
for attempt in $(seq 1 200); do
 if [ -s "$CRASH_MARKER" ]; then break; fi
 if ! kill -0 "$CRASH_JOB_PID" 2>/dev/null; then echo 'upgrade exited before crash barrier' >&2; exit 1; fi
 if [ "$attempt" -eq 200 ]; then echo 'existing crash barrier timed out' >&2; exit 1; fi
 sleep 0.1
done
CRASH_CTL_PID="$(tr -d '[:space:]' < "$CRASH_MARKER")"
[[ "$CRASH_CTL_PID" =~ ^[1-9][0-9]*$ ]]
test "$(stat -c '%u:%a' "$CRASH_MARKER")" = 0:444
test "$(sudo readlink -f "/proc/$CRASH_CTL_PID/exe")" = "$(readlink -f "$CTL")"
CRASH_PGID="$(ps -o pgid= -p "$CRASH_CTL_PID" | tr -d '[:space:]')"
test "$CRASH_PGID" = "$CRASH_CTL_PID" && test "$CRASH_PGID" != "$(ps -o pgid= -p "$$" | tr -d '[:space:]')"
sudo /usr/bin/kill -KILL -- "-$CRASH_PGID"
if wait "$CRASH_JOB_PID"; then echo 'SIGKILL did not interrupt upgrade' >&2; exit 1; fi
QUAL_STAGE=recover
sudo env PATH="$CTL_PATH" "$CTL" recover > "$QUAL_ROOT/recover.json"
python3 - "$QUAL_ROOT/recover.json" <<'PYRECOVER'
import json,sys
j=json.load(open(sys.argv[1]))['active_journal']; assert j['kind']=='upgrade' and j['status']=='active'
assert 'migrate_database' in j['completed_phases'] and set(j['allowed_actions'])=={'recover','resume'}
PYRECOVER
QUAL_STAGE=resume
sudo env PATH="$CTL_PATH" "$CTL" resume > "$QUAL_ROOT/upgrade-receipt.json"
QUAL_STAGE=post_upgrade_qualification
pg snapshot > "$QUAL_ROOT/after-upgrade.json"; cmp "$QUAL_ROOT/before.json" "$QUAL_ROOT/after-upgrade.json"
# Upgrade itself created the original custody once, before restarting consumers.
sudo env PATH="$CTL_PATH" "$CTL" review-body-config validate
sudo stat -c '%i:%s:%a:%u:%Y:%Z' /etc/x-insight/review-body.key /etc/x-insight/xinsight-review-body.env > "$QUAL_ROOT/custody-after-upgrade.txt"
test "$(sudo stat -c '%a:%u' /etc/x-insight/review-body.key)" = 600:0
sudo sha256sum /etc/x-insight/access-accounts.json /etc/x-insight/source-facts.json /etc/x-insight/ci-source.key /etc/x-insight/ci-content.key > "$QUAL_ROOT/after-upgrade-files.sha256"
cmp "$QUAL_ROOT/before-files.sha256" "$QUAL_ROOT/after-upgrade-files.sha256"
# The current signed profile forbids application rollback before subsequent
# target-capability and schema guards; verify that first rejection is read-only.
if sudo env PATH="$CTL_PATH" "$CTL" rollback --check --previous > "$QUAL_ROOT/rollback.txt" 2>&1; then echo 'application-policy-forbidden rollback accepted' >&2; exit 1; fi
grep -F 'current release does not declare application rollback compatibility' "$QUAL_ROOT/rollback.txt" >/dev/null
pg snapshot > "$QUAL_ROOT/after-rollback-check.json"; cmp "$QUAL_ROOT/after-upgrade.json" "$QUAL_ROOT/after-rollback-check.json"
sudo stat -c '%i:%s:%a:%u:%Y:%Z' /etc/x-insight/review-body.key /etc/x-insight/xinsight-review-body.env > "$QUAL_ROOT/custody-after-rollback-check.txt"
cmp "$QUAL_ROOT/custody-after-upgrade.txt" "$QUAL_ROOT/custody-after-rollback-check.txt"
sudo sha256sum /etc/x-insight/access-accounts.json /etc/x-insight/source-facts.json /etc/x-insight/ci-source.key /etc/x-insight/ci-content.key > "$QUAL_ROOT/after-rollback-check-files.sha256"
cmp "$QUAL_ROOT/before-files.sha256" "$QUAL_ROOT/after-rollback-check-files.sha256"
BACKUP_PATH="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["backup_path"])' "$QUAL_ROOT/upgrade-receipt.json")"
BACKUP_SHA="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["backup_sha256"])' "$QUAL_ROOT/upgrade-receipt.json")"
sudo env PATH="$CTL_PATH" "$CTL" restore --check --recovery-bundle "$BACKUP_PATH" --recovery-bundle-sha256 "$BACKUP_SHA" --new-database xinsight_restore_managed_fpv11_ci > "$QUAL_ROOT/restore-check.json"
pg snapshot > "$QUAL_ROOT/after-restore-check.json"; cmp "$QUAL_ROOT/before.json" "$QUAL_ROOT/after-restore-check.json"
sudo stat -c '%i:%s:%a:%u:%Y:%Z' /etc/x-insight/review-body.key /etc/x-insight/xinsight-review-body.env > "$QUAL_ROOT/custody-after-check.txt"
cmp "$QUAL_ROOT/custody-after-upgrade.txt" "$QUAL_ROOT/custody-after-check.txt"
sudo env PATH="$CTL_PATH" "$CTL" restore --recovery-bundle "$BACKUP_PATH" --recovery-bundle-sha256 "$BACKUP_SHA" --new-database xinsight_restore_managed_fpv11_ci --reason-code field_validation > "$QUAL_ROOT/restore-receipt.json"
pg snapshot > "$QUAL_ROOT/after-restore.json"; compare_restored_snapshot "$QUAL_ROOT/before.json" "$QUAL_ROOT/after-restore.json"
# Linux credential proof is independent of successful/no-op source calls.
# Public root-owned configs, original index keys and additive stable UUID keys
# are loaded by the actual non-root systemd credential/filesystem boundaries.
pg seed-partition > "$QUAL_ROOT/partition-seed.txt"
sudo install -d -m 0755 /etc/x-insight/source-facts.d
MANAGED_KEY=/etc/x-insight/source-managed-33333333-3333-4333-8333-333333333333.key
printf 'ci-only-%s\n' "$(openssl rand -hex 24)" | sudo tee "$MANAGED_KEY" >/dev/null
sudo chmod 0600 "$MANAGED_KEY"
python3 "$ROOT/scripts/ci-managed-pg.py" partition > "$QUAL_ROOT/partition.json"
go build -o "$QUAL_ROOT/credential-proof" "$ROOT/scripts/ci-managed-source-credential-proof.go"
sudo install -m 0755 "$QUAL_ROOT/credential-proof" /usr/local/bin/xinsight-ci-managed-credential-proof
# Formal production entries preserve automatic custody and publish both root
# machine purposes. Synthetic machine keys remain in this disposable VM.
# Reuse the actual automatically generated binding. Never substitute a CI key.
sudo env PATH="$CTL_PATH" "$CTL" review-body-config validate
sudo stat -c '%i:%s:%a:%u:%Y:%Z' /etc/x-insight/review-body.key /etc/x-insight/xinsight-review-body.env > "$QUAL_ROOT/custody-after-restore.txt"
cmp "$QUAL_ROOT/custody-after-upgrade.txt" "$QUAL_ROOT/custody-after-restore.txt"
printf 'XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback\nXINSIGHT_EXTERNAL_ORIGIN=https://127.0.0.1\n' | sudo tee /etc/x-insight/trusted-tls-proxy.env >/dev/null
sudo chmod 0644 /etc/x-insight/trusted-tls-proxy.env
sudo env PATH="$CTL_PATH" "$CTL" connector-target-config set --unirouter-origin "https://$SOURCE_IP"
for purpose in policy enrollment; do
 prefix=ur-xpm1; if [ "$purpose" = enrollment ]; then prefix=ur-xse1; fi
 key="/etc/x-insight/ci-machine-$purpose.key"
 printf '%s-%s\n' "$prefix" "$(openssl rand -hex 32)" | sudo tee "$key" >/dev/null
 sudo chmod 0600 "$key"
 sudo env PATH="$CTL_PATH" "$CTL" connector-target-config "set-$purpose" --installation-id xci_AAAAAAAAAAAAAAAAAAAAAA --source-instance ci-source --org-id 2 --credential-file "$key" --check
 sudo env PATH="$CTL_PATH" "$CTL" connector-target-config "set-$purpose" --installation-id xci_AAAAAAAAAAAAAAAAAAAAAA --source-instance ci-source --org-id 2 --credential-file "$key"
done
sudo env PATH="$CTL_PATH" "$CTL" connector-target-config validate
# Probe receipts use the existing capture pipe. No CI service drop-in or
# consumer-writable receipt directory enters the managed backup/restore state.
QUAL_STAGE=initial_consumers
sudo systemctl daemon-reload
sudo systemctl restart xinsight.service xinsight-connector-controller.service
sudo env GITHUB_ACTIONS=true XINSIGHT_CI_EPHEMERAL_HOST=1 PATH="$CTL_PATH" /usr/local/bin/xinsight-ci-managed-credential-proof --publish-partition "$QUAL_ROOT/partition.json" > "$QUAL_ROOT/source-publish-receipt.txt"
sudo systemctl is-active --quiet xinsight-source-facts.service
# Active is not exec-ready for Type=simple. Prove both original loaded consumers
# before observing their native namespace; retain the fresh post-probe check.
sudo env GITHUB_ACTIONS=true XINSIGHT_CI_EPHEMERAL_HOST=1 PATH="$CTL_PATH" /usr/local/bin/xinsight-ci-managed-credential-proof --wait-loaded > /dev/null
run_credential_probe xinsight-source-facts.service > "$QUAL_ROOT/linux-credential-proof.json"
run_credential_probe xinsight.service > "$QUAL_ROOT/linux-reader-credential-proof.json"
# Verify actual main/worker in-memory proofs against the original root binding
# and their current systemd InvocationID/MainPID, without emitting key material.
sudo env GITHUB_ACTIONS=true XINSIGHT_CI_EPHEMERAL_HOST=1 PATH="$CTL_PATH" /usr/local/bin/xinsight-ci-managed-credential-proof --check-loaded > "$QUAL_ROOT/linux-loaded-custody-proof.txt"
# Exercise the formal private bundle command while units are intentionally stopped.
# Check must preserve DB, files and service state; Backup must restore NOLOGIN
# and stopped states instead of silently enabling the deployment.
sudo systemctl stop xinsight-source-facts.service xinsight-connector-controller.service xinsight.service
pg importer-nologin > "$QUAL_ROOT/importer-paused.txt"
# Private bundles include the original key and use the installed root-owned
# backup directory; runner-owned qualification output is only for safe receipts.
PRIVATE_BACKUP="/var/backups/x-insight/ci-private-managed-fpv12.tar"
pg snapshot > "$QUAL_ROOT/before-private-backup-check.json"
QUAL_STAGE=private_backup_check
sudo env PATH="$CTL_PATH" "$CTL" backup --output "$PRIVATE_BACKUP" --check > "$QUAL_ROOT/private-backup-check.json"
sudo test ! -e "$PRIVATE_BACKUP"
sudo test ! -L "$PRIVATE_BACKUP"
pg snapshot > "$QUAL_ROOT/after-private-backup-check.json"; cmp "$QUAL_ROOT/before-private-backup-check.json" "$QUAL_ROOT/after-private-backup-check.json"
QUAL_STAGE=private_backup
sudo env PATH="$CTL_PATH" "$CTL" backup --output "$PRIVATE_BACKUP" > "$QUAL_ROOT/private-backup-receipt.json"
for unit in xinsight.service xinsight-connector-controller.service xinsight-source-facts.service; do
 test "$(sudo systemctl show "$unit" --property ActiveState --value)" = inactive
done
pg importer-is-nologin > "$QUAL_ROOT/importer-still-paused.txt"
PRIVATE_SHA="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["backup_sha256"])' "$QUAL_ROOT/private-backup-receipt.json")"
QUAL_STAGE=private_restore_check
sudo env PATH="$CTL_PATH" "$CTL" restore --recovery-bundle "$PRIVATE_BACKUP" --recovery-bundle-sha256 "$PRIVATE_SHA" --new-database xinsight_restore_private_fpv12_ci --check > "$QUAL_ROOT/private-restore-check.json"
pg snapshot > "$QUAL_ROOT/after-private-restore-check.json"; cmp "$QUAL_ROOT/before-private-backup-check.json" "$QUAL_ROOT/after-private-restore-check.json"
pg importer-is-nologin > "$QUAL_ROOT/importer-after-restore-check.txt"
for unit in xinsight.service xinsight-connector-controller.service xinsight-source-facts.service; do
 test "$(sudo systemctl show "$unit" --property ActiveState --value)" = inactive
done
sudo stat -c '%i:%s:%a:%u:%Y:%Z' /etc/x-insight/review-body.key /etc/x-insight/xinsight-review-body.env > "$QUAL_ROOT/custody-after-private-check.txt"
cmp "$QUAL_ROOT/custody-after-upgrade.txt" "$QUAL_ROOT/custody-after-private-check.txt"
QUAL_STAGE=private_restore
sudo env PATH="$CTL_PATH" "$CTL" restore --recovery-bundle "$PRIVATE_BACKUP" --recovery-bundle-sha256 "$PRIVATE_SHA" --new-database xinsight_restore_private_fpv12_ci --reason-code field_validation > "$QUAL_ROOT/private-restore-receipt.json"
pg snapshot > "$QUAL_ROOT/after-private-restore.json"; compare_restored_snapshot "$QUAL_ROOT/before-private-backup-check.json" "$QUAL_ROOT/after-private-restore.json"
QUAL_STAGE=restored_consumers
sudo systemctl restart xinsight.service xinsight-source-facts.service
sudo env GITHUB_ACTIONS=true XINSIGHT_CI_EPHEMERAL_HOST=1 PATH="$CTL_PATH" /usr/local/bin/xinsight-ci-managed-credential-proof --wait-loaded > /dev/null
run_credential_probe xinsight-source-facts.service > "$QUAL_ROOT/linux-credential-proof.json"
run_credential_probe xinsight.service > "$QUAL_ROOT/linux-reader-credential-proof.json"
sudo env GITHUB_ACTIONS=true XINSIGHT_CI_EPHEMERAL_HOST=1 PATH="$CTL_PATH" /usr/local/bin/xinsight-ci-managed-credential-proof --check-loaded > "$QUAL_ROOT/linux-restored-custody-proof.txt"
QUAL_STAGE=final_preservation
sudo stat -c '%i:%s:%a:%u:%Y:%Z' /etc/x-insight/review-body.key /etc/x-insight/xinsight-review-body.env > "$QUAL_ROOT/custody-final.txt"
cmp "$QUAL_ROOT/custody-after-upgrade.txt" "$QUAL_ROOT/custody-final.txt"
sudo sha256sum /etc/x-insight/source-facts.json /etc/x-insight/ci-source.key /etc/x-insight/ci-content.key > "$QUAL_ROOT/final-source-files.sha256"
tail -n 3 "$QUAL_ROOT/before-files.sha256" > "$QUAL_ROOT/original-source-files.sha256"
cmp "$QUAL_ROOT/original-source-files.sha256" "$QUAL_ROOT/final-source-files.sha256"
sudo env PATH="$CTL_PATH" "$CTL" status > "$QUAL_ROOT/final-status.json"
python3 - "$QUAL_ROOT" <<'PYRESULT' > "$QUAL_ROOT/qualification-result.json"
import json,pathlib,sys
p=pathlib.Path(sys.argv[1]); s=json.load(open(p/'final-status.json')); proof=json.load(open(p/'linux-credential-proof.json')); reader=json.load(open(p/'linux-reader-credential-proof.json'))
assert s['managed'] and s['install_state']['current']['product_version']=='0.2.0-dev.fpv12' and s['install_state']['database_schema_current']=='44'
assert proof['systemd_credentials']=='loaded' and proof['direct_root_key_access']=='rejected'
assert proof['original_indexed_keys']==9 and proof['managed_partition_keys']==1
assert all(c['review_body_key']=='loaded' and c['workspace_credential']=='loaded' and c['uid']>0 and c['probe_context']['current_invocation_verified'] for c in (proof,reader))
proof['reader']=reader
print(json.dumps({'schema_version':'xinsight.ci-managed-fpv11-upgrade.v1','classification':'ci_synthetic_not_target','predecessor_commit':'9c58e07f011918c4a0aed2f876ce4baa9fb12b5e','source':'0.2.0-dev.fpv11','target':'0.2.0-dev.fpv12','schema':'43->44','upgrade_check':'no_source_state_or_root_key_mutation','sigkill_resume':'completed','forward_restore':'completed','private_backup_restore':'completed','backup_stopped_state_and_nologin':'preserved','automatic_custody':'original_once_loaded_both_units','application_policy_rollback':'rejected_before_target_and_schema_guards','rollback_check':'no_source_state_or_custody_mutation','original_stream_content_history':'preserved','restored_stream_content_authority':'paused_requires_authorization','linux_systemd_credentials':proof,'source_discovery_joint':'separate_required'},indent=2,sort_keys=True))
PYRESULT
echo 'ci_managed_fpv11_upgrade=pass'
