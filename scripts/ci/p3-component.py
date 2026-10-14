#!/usr/bin/env python3
"""One disposable P3 component/browser run. No deployment or installed qualification.

check: read-only source/binary admission, no network, services, accounts or files.
build: compile exact admitted sources into a fresh private output.
run: separately approved Actions VM only; owns three exact container IDs and one run.
"""
from __future__ import annotations

import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import platform
import pty
import re
import secrets
import select
import shutil
import signal
import stat
import subprocess
import sys
import tarfile
import termios
import time

REPO = Path(__file__).resolve().parents[2]
ACTIONS = 'synthetic-db-redis,http,credentials,source-binding,resource-authorization,human-review-grant,secret-cleanup'
PG_IMAGE = 'postgres:16.14-alpine3.24@sha256:57c72fd2a128e416c7fcc499958864df5301e940bca0a56f58fddf30ffc07777'
REDIS_IMAGE = 'redis@sha256:ca0acbb137c1dc3339c8b147a58fd6f42775d4599327b50e7b116c23de501af2'
BINARY_NAMES = ('xi-api.test', 'ur-producer.test', 'xinsight-importer', 'xinsightctl')
CHECKS = ('exact_receipt_identity', 'ordinary_login', 'displayed_money_and_exact_request',
          'displayed_same_request_runtime_and_resource', 'foreign_scope_denied_in_page',
          'revocation_removes_old_reading')
HARNESS_STAGES = ('fixture_admission', 'database_identity', 'fresh_paths', 'formal_accounts',
                  'producer_start', 'production_fixture', 'import_chain', 'ordinary_read',
                  'partial_read', 'browser_ready', 'unclassified')
FAILURE_CLASSES = ('unknown', 'diagnostic_truncated', 'source_authorization', 'source_contract',
                   'source_scope', 'resource_contract', 'resource_conflict', 'postgres_error',
                   'postgres_too_many_connections')
# Exact existing assertion labels, never arbitrary Go error text or locations.
FAILURE_CHECKS = {
    'UR/XI exact terminal money equality': 'terminal_money_equality',
    'administrator store': 'administrator_store', 'consumer store': 'consumer_store',
    'completed checkpoint store': 'completed_checkpoint_store',
    'source credential input': 'source_credential', 'source transport': 'source_transport',
    'source registration': 'source_registration', 'source preflight': 'source_preflight',
    'source enable': 'source_enable', 'managed profiles': 'managed_profiles',
    'durable source bootstrap': 'source_bootstrap_durable', 'source worker healthy': 'source_worker_health',
    'close P3 source initialization store': 'source_store_close',
    'importer connection headroom before resource CLI': 'importer_headroom',
    'importer identity': 'resource_importer_identity', 'resource database binding': 'resource_database',
    'resource host binding': 'resource_host', 'resource bundle binding': 'resource_bundle',
    'resource registration': 'resource_registration', 'resource enable': 'resource_enable',
    'resource check receipt': 'resource_check_receipt', 'resource import receipt': 'resource_import_receipt',
    'formal CLI command succeeded': 'formal_cli', 'formal grant exact scope': 'formal_grant_scope',
    'formal grant/audit checkpoint': 'formal_grant_checkpoint', 'grant audit before': 'formal_grant_audit_before',
    'grant check receipt': 'formal_grant_check_receipt',
    'grant check has no DB/audit/filesystem writes': 'formal_grant_check_side_effects',
    'formal grant apply revision': 'formal_grant_apply_receipt',
    'formal grant show matches committed state': 'formal_grant_show_receipt',
    'grant show has no DB/audit/filesystem writes': 'formal_grant_show_side_effects',
    'grant committed root audit': 'formal_grant_audit_after',
}
CLI_STEPS = {**{f'source-bootstrap-{i}': f'source_bootstrap_{i}' for i in range(8)},
             'importer-version': 'resource_importer_version', 'resource-check': 'resource_check',
             'resource-import': 'resource_import'}
FAILURE_CHECKS.update({'CLI receipt encoding: ' + name: step for name, step in CLI_STEPS.items()})
FORMAL_STEPS = ('formal_validate', 'formal_grant_check', 'formal_grant_apply', 'formal_grant_show', 'unclassified')
WAIT_STEPS = {'producer ready': 'producer_ready', 'completed bootstrap': 'completed_bootstrap',
              'producer first terminal preflight': 'producer_terminal',
              'same request economic and source facts': 'same_request_import'}


class Refused(Exception):
    pass


def require(value, code):
    if not value:
        raise Refused(code)


def capture(argv, *, cwd=None, env=None, data=None, timeout=60):
    # Neither a command's stderr nor arbitrary exception text is public evidence.
    result = subprocess.run([str(x) for x in argv], cwd=cwd, env=env, input=data,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
    require(result.returncode == 0, 'command_failed')
    return result.stdout


def sha(path):
    with Path(path).open('rb') as source:
        result = hashlib.sha256()
        for block in iter(lambda: source.read(1024 * 1024), b''):
            result.update(block)
        return result.hexdigest()


def read(path):
    require(Path(path).stat().st_size <= 4 * 1024 * 1024, 'receipt_too_large')
    return json.loads(Path(path).read_text())


def write(path, value):
    with Path(path).open('x', encoding='utf8') as target:
        os.chmod(path, 0o600)
        json.dump(value, target, sort_keys=True)
        target.write('\n')


def commit(path, expected):
    require(isinstance(expected, str) and re.fullmatch('[0-9a-f]{40}', expected), 'exact_commit_required')
    command = ['git', '--no-optional-locks', '-c', 'safe.directory=' + str(path)]
    require(capture(command + ['rev-parse', 'HEAD'], cwd=path).decode().strip() == expected, 'source_commit_mismatch')
    require(not capture(command + ['status', '--porcelain', '--untracked-files=all'], cwd=path), 'source_dirty')
    return expected


def pair():
    data = read(REPO / 'scripts/ci/p3-component-pair.json')
    require(data.get('schema_version') == 'xinsight.p3-component-pair.v1'
            and data.get('unirouter_repository') == 'foigent/unirouter', 'pair_contract')
    value = data.get('unirouter_commit')
    require(isinstance(value, str) and re.fullmatch('[0-9a-f]{40}', value), 'pair_not_bound')
    return value


def check_sources(args):
    return {'xi_commit': commit(REPO, args.xi_commit), 'ur_commit': commit(args.ur, pair())}


def check_build(args):
    identity = check_sources(args)
    manifest = read(args.build / 'build.json')
    require(manifest.get('schema_version') == 'xinsight.p3-build.v1', 'build_contract')
    require(all(manifest.get(k) == v for k, v in identity.items()), 'build_source_mismatch')
    require(manifest.get('goos') == 'linux' and manifest.get('goarch') == 'amd64', 'linux_native_binary_required')
    require(set(manifest.get('binaries', {})) == set(BINARY_NAMES), 'binary_set')
    for name, digest in manifest['binaries'].items():
        p = args.build / name
        st = p.lstat()
        require(stat.S_ISREG(st.st_mode) and st.st_nlink == 1 and not st.st_mode & 0o022, 'binary_file_boundary')
        require(sha(p) == digest, 'binary_digest_mismatch')
    return manifest


def build(args):
    identity = check_sources(args)
    require(platform.system() == 'Linux' and platform.machine() == 'x86_64', 'native_linux_build_required')
    require(not args.build.exists(), 'build_output_not_fresh')
    require(capture(['go', 'version']).decode().startswith('go version go1.25.12 '), 'go_toolchain')
    args.build.mkdir(mode=0o700)
    sources = args.build / 'sources'
    sources.mkdir(mode=0o700)
    export_source(REPO, identity['xi_commit'], sources / 'xi')
    export_source(args.ur, identity['ur_commit'], sources / 'ur')
    env = {k: v for k, v in os.environ.items() if k in ('PATH', 'HOME', 'GOCACHE', 'GOMODCACHE', 'GOPATH')}
    env.update(GOOS='linux', GOARCH='amd64', CGO_ENABLED='0', GOTOOLCHAIN='local')
    commands = [
        (sources / 'xi', 'xi-api.test', ['go', 'test', '-c', '-trimpath', './internal/api']),
        # The pinned UR test helper locates migrations through runtime.Caller.
        # Retain this private binary's exact exported source path: build/sources
        # remains present during run. It is never part of the uploaded receipts.
        (sources / 'ur', 'ur-producer.test', ['go', 'test', '-tags=integration', '-c', './cmd/uniroute']),
        (sources / 'xi', 'xinsight-importer', ['go', 'build', '-trimpath', './cmd/xinsight-importer']),
        (sources / 'xi', 'xinsightctl', ['go', 'build', '-trimpath', './cmd/xinsightctl']),
    ]
    for cwd, name, command in commands:
        capture(command[:-1] + ['-o', str(args.build / name), command[-1]], cwd=cwd, env=env, timeout=600)
    check_sources(args)
    write(args.build / 'build.json', {'schema_version': 'xinsight.p3-build.v1', **identity,
          'goos': 'linux', 'goarch': 'amd64', 'toolchain': 'go1.25.12',
          'binaries': {name: sha(args.build / name) for name in BINARY_NAMES}})


def protected(path):
    for current in (path, *path.parents):
        st = current.lstat()
        require(stat.S_ISDIR(st.st_mode) and st.st_uid == 0 and not st.st_mode & 0o022,
                'protected_ancestry_required')


def export_source(source, revision, destination):
    data = capture(['git', '-c', 'safe.directory=' + str(source), 'archive', '--format=tar', revision], cwd=source)
    destination.mkdir(mode=0o700)
    with tarfile.open(fileobj=io.BytesIO(data)) as archive:
        for item in archive:
            path = Path(item.name)
            require(not path.is_absolute() and '..' not in path.parts and (item.isdir() or item.isfile()), 'source_archive_member')
            target = destination / path
            if item.isdir():
                target.mkdir(mode=0o700, parents=True, exist_ok=True)
            else:
                target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
                with target.open('xb') as out:
                    shutil.copyfileobj(archive.extractfile(item), out)
                target.chmod(0o700 if item.mode & 0o111 else 0o600)


def password_hash(ctl, password):
    """Use the real CLI's hidden terminal prompts; no test bypass or stdin pipe."""
    master, slave = pty.openpty()
    settings = termios.tcgetattr(slave)
    settings[3] &= ~termios.ECHO
    termios.tcsetattr(slave, termios.TCSANOW, settings)
    process = subprocess.Popen([str(ctl), 'access', 'hash-password'], stdin=slave,
                               stdout=subprocess.PIPE, stderr=slave, env={'PATH': '/usr/bin:/bin'})
    pending = b''
    try:
        for prompt in (b'Password: ', b'Confirm password: '):
            until = time.monotonic() + 15
            while prompt not in pending:
                require(process.poll() is None and time.monotonic() < until, 'tty_prompt_failed')
                ready, _, _ = select.select([master], [], [], 1)
                if ready:
                    pending += os.read(master, 4096)
                    require(len(pending) < 8192, 'tty_prompt_boundary')
            pending = pending.split(prompt, 1)[1]
            require(not termios.tcgetattr(slave)[3] & termios.ECHO, 'tty_echo_enabled')
            os.write(master, password.encode() + b'\n')
        raw, _ = process.communicate(timeout=30)
        require(process.returncode == 0 and password.encode() not in raw, 'password_cli_failed')
        value = raw.decode().strip()
        require(value.startswith('$argon2id$'), 'password_hash_contract')
        return value
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()
        os.close(master)
        os.close(slave)


def remember_secret(path, owned):
    st = path.lstat()
    owned.append((path, st.st_dev, st.st_ino, sha(path)))


def remove_owned_secret(record):
    path, device, inode, digest = record
    st = path.lstat()
    require(stat.S_ISREG(st.st_mode) and st.st_nlink == 1 and st.st_uid == 0 and
            st.st_dev == device and st.st_ino == inode and sha(path) == digest, 'secret_cleanup_boundary')
    path.unlink()


def prepare_accounts(ctl, directory, run, owned):
    directory.mkdir(mode=0o700)
    accounts, logins = [], {}
    for kind in ('reviewer', 'reader'):
        password = secrets.token_urlsafe(32)
        subject = capture([ctl, 'access', 'generate-subject'], env={'PATH': '/usr/bin:/bin'}).decode().strip()
        require(re.fullmatch('xs_[0-9a-f]{32}', subject), 'subject_contract')
        username = 'p3.' + kind
        accounts.append({'subject_id': subject, 'username': username, 'disabled': False,
                         'role': 'workbench_investigation_' + kind, 'password_hash': password_hash(ctl, password)})
        logins[kind] = {'username': username, 'password': password}
    write(directory / 'access-accounts.json', {'schema_version': 'xinsight.local-accounts.v3', 'accounts': accounts})
    remember_secret(directory / 'access-accounts.json', owned)
    before = sha(directory / 'access-accounts.json')
    checked = json.loads(capture([ctl, 'access', 'validate', '--file', directory / 'access-accounts.json'], env={'PATH': '/usr/bin:/bin'}))
    require(checked['account_count'] == 2 and checked['workbench_investigation_reviewer_count'] == 1
            and checked['workbench_investigation_reader_count'] == 1 and before == sha(directory / 'access-accounts.json'), 'account_validation')
    write(directory / 'logins.json', logins)
    remember_secret(directory / 'logins.json', owned)
    write(directory / 'prepared.json', {'run': run, 'ctl_sha256': sha(ctl), 'generate_subject_passed': True,
                                      'hash_password_tty_passed': True, 'validate_passed': True})


def safe_browser(raw):
    require(isinstance(raw, dict), 'browser_receipt_contract')
    require(raw.get('schema_version') == 'xinsight.p3-browser.v1', 'browser_receipt_contract')
    stage = raw.get('stage')
    require(stage in ('input_receipts', 'receipt_binding', 'ordinary_login', 'cost_page', 'runtime_and_resources',
                      'foreign_scope_denied', 'revoked_reading', 'complete'), 'browser_stage')
    waiting = raw.get('wait', 'not_waiting')
    require(waiting in ('foreign_response', 'group_control', 'login_session', 'login_submit', 'money_control', 'money_display', 'navigation', 'not_waiting', 'password_control', 'project_control', 'reading_identifier', 'reading_ready', 'reading_submit', 'request_detail', 'resource_detail', 'revocation_commit', 'revoked_response', 'runtime_controls', 'runtime_detail', 'scope_cleared', 'source_control', 'username_control'), 'browser_wait')
    checks = raw.get('checks')
    require(isinstance(checks, list) and all(x in CHECKS for x in checks) and len(set(checks)) == len(checks), 'browser_checks')
    require(type(raw.get('passed')) is bool, 'browser_status')
    if raw['passed']:
        require(waiting == 'not_waiting', 'incomplete_browser_pass')
        require(checks == list(CHECKS) and stage == 'complete', 'incomplete_browser_pass')
        require(all(key in raw for key in ('request_sha256', 'source_sha256', 'charged_micros', 'calculated_micros')), 'missing_browser_binding')
    result = {'schema_version': raw['schema_version'], 'stage': stage, 'passed': raw['passed'], 'checks': checks}
    if 'wait' in raw:
        result['wait'] = waiting
    for key in ('request_sha256', 'source_sha256'):
        if key in raw:
            require(isinstance(raw[key], str) and re.fullmatch('[0-9a-f]{64}', raw[key]), 'browser_digest')
            result[key] = raw[key]
    for key in ('charged_micros', 'calculated_micros'):
        if key in raw:
            require(isinstance(raw[key], str) and re.fullmatch('-?[0-9]{1,30}', raw[key]), 'browser_money')
            result[key] = raw[key]
    return result


def postgres_identity(cid, database, password, docker_env):
    # The image's initialization server accepts Unix socket connections before
    # setup finishes. Admit only the real target database through authenticated
    # TCP; pg_isready alone does not establish either database or role validity.
    sql = "SELECT json_build_object('database',current_database(),'user',current_user,'database_oid',(SELECT oid::text FROM pg_database WHERE datname=current_database()),'system_id',system_identifier::text,'public_tables',(SELECT count(*) FROM pg_tables WHERE schemaname='public')) FROM pg_control_system()"
    env = dict(docker_env, PGPASSWORD=password, PGCONNECT_TIMEOUT='3')
    argv = ['docker', 'exec', '-e', 'PGPASSWORD', '-e', 'PGCONNECT_TIMEOUT', cid,
            'psql', '-XAt', '-w', '-v', 'ON_ERROR_STOP=1', '-h', '127.0.0.1', '-p', '5432',
            '-U', 'xinsight_admin', '-d', database, '-c', sql]
    until = time.monotonic() + 60
    while True:
        remaining = until - time.monotonic()
        require(remaining > 0, 'postgres_ready_timeout')
        try:
            probe = subprocess.run(argv, env=env, capture_output=True, timeout=min(10, remaining))
        except subprocess.TimeoutExpired:
            probe = None
        if probe is not None and probe.returncode == 0:
            try:
                value = json.loads(probe.stdout)
            except (ValueError, TypeError):
                raise Refused('postgres_identity_contract') from None
            require(isinstance(value, dict) and set(value) ==
                    {'database', 'user', 'database_oid', 'system_id', 'public_tables'}, 'postgres_identity_contract')
            require(value['database'] == database and value['user'] == 'xinsight_admin', 'postgres_identity_mismatch')
            require(all(isinstance(value[k], str) and re.fullmatch('[1-9][0-9]{0,19}', value[k])
                        for k in ('database_oid', 'system_id')), 'postgres_identity_contract')
            require(type(value['public_tables']) is int and value.pop('public_tables') == 0, 'base_not_empty')
            return value
        remaining = until - time.monotonic()
        require(remaining > 0, 'postgres_ready_timeout')
        time.sleep(min(1, remaining))


def start_container(root, kind, run_id, options, env, containers):
    require(kind in ('xi', 'ur', 'redis'), 'container_kind')
    # Docker owns the cidfile, including when the client exits after creation.
    cidfile = root / (kind + '.cid')
    require(not cidfile.exists(), 'container_identity_not_fresh')
    containers.append(cidfile)
    try:
        raw = capture(['docker', 'run', '--detach', '--rm', '--cidfile', cidfile,
                       '--label', 'foigent.p3.run=' + run_id, '--name', run_id + '-' + kind,
                       *options], env=env, timeout=120).decode().strip()
    except Refused:
        raise Refused(kind + '_container_start_failed') from None
    require(re.fullmatch('[0-9a-f]{64}', raw) and cidfile.read_text().strip() == raw, 'container_identity')
    return raw


def stop_containers(containers, run_id):
    stopped = True
    for cidfile in reversed(containers):
        try:
            if not cidfile.exists():
                # A failed create without an identity cannot certify cleanup.
                stopped = False
                continue
            st = cidfile.lstat()
            require(stat.S_ISREG(st.st_mode) and st.st_uid == 0 and st.st_nlink == 1
                    and st.st_size <= 65, 'container_identity_file')
            cid = cidfile.read_text().strip()
            require(re.fullmatch('[0-9a-f]{64}', cid), 'container_identity')
            label = capture(['docker', 'inspect', '--format', '{{index .Config.Labels "foigent.p3.run"}}', cid])
            require(label.decode().strip() == run_id, 'container_owner_mismatch')
            capture(['docker', 'stop', '--time', '20', cid], timeout=30)
        except (OSError, Refused, subprocess.SubprocessError):
            stopped = False
    return stopped


def publish_results(output, safe, state):
    # Invalid or incomplete private browser output must still produce a closed
    # failure receipt. Arbitrary parser errors and extra fields never escape.
    try:
        if (output / 'browser-result.json').exists():
            write(safe / 'browser-result.json', safe_browser(read(output / 'browser-result.json')))
    except (OSError, ValueError, KeyError, TypeError, Refused):
        state['passed'] = False
        state['failure_code'] = 'browser_receipt_invalid'
    write(safe / 'qualification-result.json', state)


def browser_execution_projection(output, process_started, exit_code, timed_out):
    result = {'stage': 'not_reported', 'category': 'startup_or_configuration_unknown',
              'process_started': process_started, 'reporter_loaded': False,
              'test_started': False, 'input_completed': False, 'body_started': False,
              'timed_out': timed_out, 'global_error_reported': False,
              'browser_receipt_present': (output / 'browser-result.json').is_file()}
    if type(exit_code) is int and -127 <= exit_code <= 255:
        result['exit_code'] = exit_code
    try:
        raw = private_failure(output / 'browser-runner.json')
        if raw is not None:
            require(raw.get('schema_version') == 'xinsight.p3-runner.v1', 'runner_contract')
            require(raw.get('stage') in ('runner_initialization', 'fixture', 'input_receipts', 'test_body', 'complete'), 'runner_stage')
            for key in ('test_started', 'input_completed', 'body_started', 'timed_out', 'global_error_reported'):
                require(type(raw.get(key)) is bool, 'runner_boolean')
                result[key] = raw[key]
            result['timed_out'] = result['timed_out'] or timed_out
            result['stage'] = raw['stage']; result['reporter_loaded'] = True
            if not raw['test_started']:
                result['category'] = 'configuration_or_collection_unknown'
            elif raw['stage'] == 'input_receipts' and not raw['input_completed']:
                result['category'] = 'input_receipts'
            elif not raw['body_started']:
                result['category'] = 'fixture'
            else:
                result['category'] = 'test_body'
        if not process_started:
            result['category'] = 'process_start'
        elif result['timed_out']:
            result['category'] = 'timeout'
        elif exit_code == 0:
            result['category'] = 'complete' if result['browser_receipt_present'] else 'receipt_missing'
    except (OSError, ValueError, KeyError, TypeError, Refused):
        result['stage'] = 'not_reported'; result['category'] = 'invalid_diagnostic'
    return result


def verify_producer_money(output, browser):
    # This file is produced only by UR's deferred finalization. Never demand it
    # while the browser still needs the live harness; join the owners first.
    try:
        producer = read(output.parent / 'ur/economic.json')
        chain = read(output / 'p3-chain.json')
        snapshot = producer['snapshot']; money = snapshot['money']
        require(isinstance(chain['request_id'], str) and isinstance(snapshot['request_id'], str), 'producer_request_binding')
        require(snapshot['request_id'] == chain['request_id']
                and hashlib.sha256(chain['request_id'].encode()).hexdigest() == browser['request_sha256'], 'producer_request_binding')
        scope = '/'.join(chain[key] for key in ('source_system_id', 'source_instance', 'org_id', 'project_id'))
        require(hashlib.sha256(scope.encode()).hexdigest() == browser['source_sha256'], 'producer_source_binding')
        require(isinstance(producer['snapshot_sha256'], str)
                and re.fullmatch('sha256:[0-9a-f]{64}', producer['snapshot_sha256'])
                and producer['snapshot_sha256'] == producer['cost_source_snapshot_sha256'], 'producer_economic_binding')
        require(money['charged_cost_micros'] == browser['charged_micros']
                and money['calculated_cost_micros'] == browser['calculated_micros'], 'producer_money_binding')
    except (OSError, ValueError, KeyError, TypeError):
        raise Refused('producer_money_receipt_invalid') from None


def browser_run(argv, cwd, env, log, *, output=None, state=None, timeout=180, harness=None):
    process = None
    timed_out = False
    try:
        process = subprocess.Popen(argv, cwd=cwd, env=env, stdout=log, stderr=log, start_new_session=True)
        if harness is None:
            return process.wait(timeout=timeout)
        until = time.monotonic() + timeout
        while True:
            require(harness.poll() is None, 'harness_exited_during_browser')
            remaining = until - time.monotonic()
            if remaining <= 0:
                raise subprocess.TimeoutExpired(argv, timeout)
            try:
                return process.wait(timeout=min(1, remaining))
            except subprocess.TimeoutExpired:
                if time.monotonic() >= until:
                    raise subprocess.TimeoutExpired(argv, timeout) from None
    except subprocess.TimeoutExpired:
        timed_out = True
        raise
    finally:
        if process is not None and process.poll() is None:
            # The process group was created by this invocation; include Chromium
            # children when a timeout or cancellation interrupts Playwright.
            try:
                os.killpg(process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=10)
        if output is not None and state is not None:
            state['browser_execution'] = browser_execution_projection(output, process is not None,
                                                                       process.returncode if process is not None else None, timed_out)


def closed_harness_stage(log_path):
    # Keep raw test assertions private, even when they contain a known marker.
    with log_path.open('rb') as source:
        raw = source.read(8193)
    if len(raw) > 8192:
        return 'diagnostic_truncated'
    stages = []
    for line in raw.splitlines():
        for stage in HARNESS_STAGES:
            if line == b'P3_HARNESS_STAGE=' + stage.encode('ascii'):
                stages.append(stage)
    return stages[-1] if stages else 'not_reported'


def private_failure(path):
    # Fixed filenames only. Refuse replacement links, foreign owners and
    # unbounded/duplicate-key JSON; invalid diagnostics cannot change admission.
    def unique(pairs):
        value = {}
        for key, item in pairs:
            if key in value:
                raise ValueError('duplicate diagnostic key')
            value[key] = item
        return value
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    except FileNotFoundError:
        return None
    with os.fdopen(fd, 'rb') as source:
        st = os.fstat(source.fileno())
        require(stat.S_ISREG(st.st_mode) and st.st_uid == os.geteuid() and st.st_nlink == 1
                and stat.S_IMODE(st.st_mode) == 0o600 and st.st_size <= 8192, 'diagnostic_file_boundary')
        body = source.read(8193)
    require(len(body) <= 8192, 'diagnostic_size')
    value = json.loads(body, object_pairs_hook=unique)
    require(isinstance(value, dict), 'diagnostic_object')
    return value


def safe_process_failure(raw, kind, step):
    require(type(raw.get('exit_code')) is int and -127 <= raw['exit_code'] <= 255, 'diagnostic_exit')
    result = {'kind': kind, 'step': step, 'exit_code': raw['exit_code']}
    for key in ('context_timeout', 'context_cancelled', 'stop_requested'):
        require(type(raw.get(key)) is bool, 'diagnostic_boolean')
        result[key] = raw[key]
    return result


def safe_error_failure(raw, result):
    diagnostic = raw.get('diagnostic')
    require(isinstance(diagnostic, dict) and diagnostic.get('error_class') in FAILURE_CLASSES, 'diagnostic_class')
    result['error_class'] = diagnostic['error_class']
    if 'sqlstate' in diagnostic:
        code = diagnostic['sqlstate']
        require(isinstance(code, str) and re.fullmatch('[0-9A-Z]{5}', code)
                and result['error_class'] in ('postgres_error', 'postgres_too_many_connections'), 'diagnostic_sqlstate')
        result['sqlstate'] = code
    return result


def harness_failure_projection(output, log_path, *, include_stopped_children=False):
    candidates = []
    try:
        for name, step in CLI_STEPS.items():
            raw = private_failure(output / (name + '-failure.json'))
            if raw is not None:
                require(raw.get('stage') == name, 'diagnostic_cli_step')
                candidates.append(safe_error_failure(raw, safe_process_failure(raw, 'cli', step)))
        raw = private_failure(output / 'formal-cli-failure.json')
        if raw is not None:
            require(raw.get('stage') in FORMAL_STEPS, 'diagnostic_formal_step')
            candidates.append(safe_error_failure(raw, safe_process_failure(raw, 'cli', raw['stage'])))
        raw = private_failure(output / 'check-failure.json')
        if raw is not None:
            require(raw.get('step') in FAILURE_CHECKS.values(), 'diagnostic_check')
            result = {'kind': 'assertion', 'step': raw['step']}
            for key in ('context_timeout', 'context_cancelled'):
                require(type(raw.get(key)) is bool, 'diagnostic_boolean')
                result[key] = raw[key]
            candidates.append(safe_error_failure(raw, result))
        for index, role in enumerate(('producer', 'completed_feed', 'source_worker')):
            for prefix, stage, kind in (('child-start-failure', 'child_start', 'child_start'),
                                        ('child-failure', 'unexpected_child_exit', 'child_exit')):
                raw = private_failure(output / f'{prefix}-{index}.json')
                if raw is not None:
                    require(raw.get('stage') == stage, 'diagnostic_child_stage')
                    candidates.append(safe_process_failure(raw, kind, role))
        raw = private_failure(output / 'wait-failure.json')
        if raw is not None:
            require(raw.get('stage') in WAIT_STEPS and raw.get('reason') in ('timeout', 'cancelled', 'stop_requested'), 'diagnostic_wait')
            candidates.append({'kind': 'wait', 'step': WAIT_STEPS[raw['stage']],
                               'context_timeout': raw['reason'] == 'timeout',
                               'context_cancelled': raw['reason'] == 'cancelled',
                               'stop_requested': raw['reason'] == 'stop_requested'})
        if len(candidates) > 1:
            return {'kind': 'ambiguous', 'step': 'unclassified'}
        if candidates:
            return candidates[0]
        with log_path.open('rb') as source:
            raw = source.read(8193)
            truncated = len(raw) > 8192
            if truncated:
                # Shutdown assertions occur at the end. Drop the partial first
                # line of a bounded tail; never export arbitrary log text.
                source.seek(-8192, os.SEEK_END)
                tail = source.read(8192)
                raw = tail.split(b'\n', 1)[1] if b'\n' in tail else b''
        # Testing's decorated assertion line must match completely. A marker
        # embedded in a path, quoted error or raw body never becomes a step.
        checks = []
        for line in raw.decode('utf8', errors='replace').splitlines():
            match = re.fullmatch(r'\s+[A-Za-z0-9_.]+\.go:[0-9]+: P3 fixture: (.+)', line)
            if match and match[1] in FAILURE_CHECKS:
                checks.append(FAILURE_CHECKS[match[1]])
        result = {'kind': 'assertion', 'step': checks[-1]} if len(checks) == 1 else {'kind': 'unclassified', 'step': 'unclassified'}
        if not checks and include_stopped_children:
            stopped = []
            for index, role in enumerate(('producer', 'completed_feed', 'source_worker')):
                raw = private_failure(output / f'child-stop-{index}.json')
                if raw is not None:
                    require(raw.get('stage') == 'child_stop', 'diagnostic_child_stop')
                    child = safe_process_failure(raw, 'child_exit', role)
                    if child['exit_code'] != 0:
                        stopped.append(child)
            if len(stopped) == 1:
                result = stopped[0]
            elif len(stopped) > 1:
                result = {'kind': 'ambiguous', 'step': 'unclassified'}
        if truncated:
            result['diagnostic_truncated'] = True
        return result
    except (OSError, ValueError, TypeError, Refused):
        return {'kind': 'invalid_diagnostic', 'step': 'unclassified'}



def finish_harness(harness, output, browser, state, run_id, *, timeout=60):
    # A page pass is provisional until every owner and financial binding passes.
    state['passed'] = False
    state['harness_phase'] = 'browser_active'
    require(harness.poll() is None, 'harness_exited_after_browser')
    state['harness_phase'] = 'shutdown'
    write(output / 'stop', {'run': run_id})
    try:
        exit_code = harness.wait(timeout=timeout)
    except subprocess.TimeoutExpired:
        state['harness_timed_out'] = True
        raise Refused('harness_shutdown_timeout') from None
    state['harness_exit_code'] = exit_code if type(exit_code) is int and -127 <= exit_code <= 255 else -1
    require(exit_code == 0, 'harness_shutdown_failed')
    state['harness_phase'] = 'children_join'
    try:
        children = read(output / 'children-stopped.json')
        require(isinstance(children, dict), 'children_receipt_invalid')
    except (OSError, ValueError, KeyError, TypeError):
        raise Refused('children_receipt_invalid') from None
    require(children.get('waited') is True and children.get('successful') is True, 'children_not_joined')
    state['harness_phase'] = 'money_binding'
    verify_producer_money(output, browser)
    state['producer_money_binding_verified'] = True
    state['harness_phase'] = 'complete'


def retain_harness_failure(harness, output, log_path, state):
    if harness is None:
        return
    exit_code = harness.poll()
    if type(exit_code) is int and -127 <= exit_code <= 255:
        state['harness_exit_code'] = exit_code
    code = state.get('failure_code', '')
    if not state.get('passed') and (exit_code not in (None, 0)
                                  or code.startswith(('harness_', 'children_', 'producer_'))
                                  or state.get('cleanup_incomplete')):
        state['harness_stage'] = closed_harness_stage(log_path)
        state['harness_failure'] = harness_failure_projection(output, log_path, include_stopped_children=True)


def wait_harness(harness, output, log_path, state):
    until = time.monotonic() + 300
    while not (output / 'p3-chain.json').exists():
        exit_code = harness.poll()
        if exit_code is not None:
            state['harness_exit_code'] = exit_code if type(exit_code) is int and -127 <= exit_code <= 255 else -1
            state['harness_stage'] = closed_harness_stage(log_path)
            state['harness_failure'] = harness_failure_projection(output, log_path)
            raise Refused('harness_exited_before_ready')
        if time.monotonic() >= until:
            state['harness_stage'] = closed_harness_stage(log_path)
            state['harness_failure'] = harness_failure_projection(output, log_path)
            raise Refused('harness_ready_timeout')
        time.sleep(1)


def run(args):
    manifest = check_build(args)  # Read-only admission happens before any mutation.
    require(os.geteuid() == 0 and platform.system() == 'Linux' and platform.machine() == 'x86_64', 'root_linux_required')
    require(os.environ.get('GITHUB_ACTIONS') == 'true' and os.environ.get('XI_P3_CI_APPROVED_ACTIONS') == ACTIONS, 'approved_actions_vm_required')
    run_id = os.environ.get('XI_P3_CI_APPROVED_RUN', '')
    require(re.fullmatch('p3-public-[a-z0-9][a-z0-9-]{0,63}', run_id), 'exact_run_required')
    root = Path('/var/lib/foigent-validation') / run_id
    require(args.build.is_absolute() and args.safe == args.build.parent / 'p3-component-safe', 'safe_output_boundary')
    require(args.node.is_absolute() and args.node.is_file() and args.browsers.is_absolute() and args.browsers.is_dir(), 'browser_toolchain_required')
    require(not root.exists() and not root.is_symlink() and not args.safe.exists() and not args.safe.is_symlink(), 'fresh_run_required')
    protected(root.parent if root.parent.exists() else root.parent.parent)
    root.parent.mkdir(mode=0o755, exist_ok=True)
    root.mkdir(mode=0o700)
    protected(root)
    args.safe.mkdir(mode=0o700)
    state = {'schema_version': 'xinsight.p3-component-result.v1', 'passed': False, 'stage': 'prepare',
             'xi_commit': manifest['xi_commit'], 'ur_commit': manifest['ur_commit'],
             'binaries': manifest['binaries'], 'images': {'postgres': PG_IMAGE, 'redis': REDIS_IMAGE},
             'installed_services_qualified': False, 'containers_stopped': False}
    containers, secret_files = [], []
    harness = None
    output = root / 'evidence' / run_id / 'xi'
    def cancelled(_signum, _frame):
        raise Refused('run_cancelled')
    prior_signals = {s: signal.signal(s, cancelled) for s in (signal.SIGINT, signal.SIGTERM)}
    try:
        for name in ('sources', 'candidate', 'credentials', 'evidence'):
            (root / name).mkdir(mode=0o700)
        (root / 'credentials/xi-product-test').mkdir(mode=0o700)
        export_source(REPO, manifest['xi_commit'], root / 'sources/xi')
        export_source(args.ur, manifest['ur_commit'], root / 'sources/ur')
        for name in BINARY_NAMES:
            target = root / 'candidate' / name
            shutil.copyfile(args.build / name, target)
            target.chmod(0o700)
            require(sha(target) == manifest['binaries'][name], 'staged_digest_mismatch')
        account_dir = root / 'credentials/xi-product-test' / (run_id + '-accounts')
        prepare_accounts(root / 'candidate/xinsightctl', account_dir, run_id, secret_files)
        state['stage'] = 'isolated_services'
        docker_env = {'PATH': '/usr/bin:/bin', 'HOME': '/root'}
        identities, dsns = {}, {}
        for kind, port in (('xi', 58134), ('ur', 58132)):
            state['stage'] = kind + '_postgres_create'
            password = secrets.token_hex(24)
            env = dict(docker_env, POSTGRES_PASSWORD=password)
            cid = start_container(root, kind, run_id, ['--memory', '1g', '--tmpfs', '/var/lib/postgresql/data:rw,size=768m',
                           '-p', f'127.0.0.1:{port}:5432', '-e', 'POSTGRES_PASSWORD', '-e', 'POSTGRES_USER=xinsight_admin',
                           '-e', f'POSTGRES_DB={kind}_p3_base', PG_IMAGE], env, containers)
            state['stage'] = kind + '_postgres_identity'
            value = postgres_identity(cid, kind + '_p3_base', password, docker_env)
            identities[kind] = dict(value, host='127.0.0.1', port=port)
            dsns[kind] = f'postgres://xinsight_admin:{password}@127.0.0.1:{port}/{kind}_p3_base?sslmode=disable'
        require(identities['xi']['system_id'] != identities['ur']['system_id'], 'clusters_not_distinct')
        write(root / 'xi-database-scope.json', identities['xi'])
        write(root / 'database-identities.json', identities)
        redis_password = secrets.token_hex(24)
        redis_config = root / 'redis.conf'
        with redis_config.open('x') as target:
            redis_config.chmod(0o600)
            target.write(f'bind 0.0.0.0\nport 6379\nsave ""\nappendonly no\nrequirepass {redis_password}\n')
        remember_secret(redis_config, secret_files)
        state['stage'] = 'redis_create'
        cid = start_container(root, 'redis', run_id, ['--memory', '128m', '--tmpfs', '/data:rw,size=32m',
                       '-p', '127.0.0.1:58379:6379', '--mount', f'type=bind,src={redis_config},dst=/p3.conf,readonly',
                       '--user', '0', '--entrypoint', 'redis-server', REDIS_IMAGE, '/p3.conf'], docker_env, containers)
        # The only database is a new container; authenticate without argv secrets.
        state['stage'] = 'redis_ready'
        until = time.monotonic() + 30
        while True:
            probe = subprocess.run(['docker', 'exec', '-e', 'REDISCLI_AUTH', cid, 'redis-cli', '-n', '15', 'DBSIZE'],
                                   env=dict(docker_env, REDISCLI_AUTH=redis_password), capture_output=True, timeout=10)
            if probe.returncode == 0 and probe.stdout.strip() == b'0':
                break
            require(time.monotonic() < until, 'redis_empty_ready_timeout')
            time.sleep(1)
        env = {'PATH': '/usr/bin:/bin', 'HOME': '/root', 'P3_PORTABLE': '1', 'P3_APPROVED_ROOT': str(root),
               'P3_APPROVED_RUN': run_id, 'P3_APPROVED_ACTIONS': ACTIONS, 'P3_XI_DATABASE_PORT': '58134',
               'P3_UR_DATABASE_PORT': '58132', 'P3_REDIS_PORT': '58379', 'XI_P3_INTEROP': '1',
               'XINSIGHT_SCOPED_BROWSER': '1', 'XI_FORMAL_ACCOUNTS': '1',
               'XINSIGHT_REQUIRE_POSTGRES_TESTS': '1', 'XINSIGHT_RESPONSIBILITY_LOCAL_TEST': '1',
               'XINSIGHT_LOCAL_TEST_DATABASE_SCOPE': str(root / 'xi-database-scope.json'),
               'XINSIGHT_POSTGRES_DSN': dsns['xi'], 'TEST_DATABASE_URL': dsns['ur'], 'REDIS_PASSWORD': redis_password,
               'P3_CREDENTIAL_DIR': str(root / 'credentials/xi-product-test' / run_id),
               'XINSIGHT_SCOPED_BROWSER_OUTPUT': str(output)}
        for kind in ('xi', 'ur'):
            env[kind.upper() + '_P3_BASE_DATABASE'] = identities[kind]['database']
            env[kind.upper() + '_P3_DATABASE_OID'] = identities[kind]['database_oid']
            env[kind.upper() + '_P3_SYSTEM_ID'] = identities[kind]['system_id']
        for key, name in [('P3_UR_TEST_BINARY', 'ur-producer.test'), ('P3_XI_IMPORTER', 'xinsight-importer'), ('P3_XI_CTL', 'xinsightctl')]:
            env[key] = str(root / 'candidate' / name)
            env[key + '_SHA256'] = manifest['binaries'][name]
        state['stage'] = 'p3_harness'
        with (root / 'harness.private.log').open('xb') as log:
            os.chmod(log.name, 0o600)
            harness = subprocess.Popen([root / 'candidate/xi-api.test', '-test.run=^TestScopedWorkbenchBrowserHarness$', '-test.timeout=12m'],
                                       cwd=root / 'sources/xi/internal/api', env=env, stdout=log, stderr=log)
            wait_harness(harness, output, root / 'harness.private.log', state)
            url = read(output / 'ready.json')['base_url']
            require(re.fullmatch(r'https://127\.0\.0\.1:[0-9]+', url), 'browser_url_boundary')
            state['stage'] = 'p3_browser'
            state['harness_phase'] = 'browser_active'
            browser_env = {'PATH': str(args.node.parent) + ':/usr/bin:/bin', 'HOME': '/root',
                           'PLAYWRIGHT_BROWSERS_PATH': str(args.browsers),
                           'XINSIGHT_P3_BROWSER_URL': url, 'XINSIGHT_P3_RECEIPTS': str(output),
                           'XINSIGHT_P3_LOGIN_FILE': str(account_dir / 'logins.json'),
                           'XINSIGHT_P3_BROWSER_PRIVATE_OUTPUT': str(root / 'browser-private'),
                           'XINSIGHT_P3_RUNNER_RECEIPT': str(output / 'browser-runner.json')}
            browser_status = browser_run([args.node, REPO / 'frontend/node_modules/@playwright/test/cli.js', 'test', '--config', 'playwright.p3.config.ts'],
                                         REPO / 'frontend', browser_env, log, output=output, state=state, harness=harness)
            require(browser_status == 0, 'browser_failed')
            approved = safe_browser(read(output / 'browser-result.json'))
            require(approved['passed'], 'browser_did_not_pass')
            finish_harness(harness, output, approved, state, run_id)
            state['passed'] = True
            state['stage'] = 'complete'
    except Refused as error:
        state['failure_code'] = str(error)
        raise
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError):
        state['failure_code'] = 'bounded_operation_failed'
        raise
    finally:
        for sig in prior_signals:
            signal.signal(sig, signal.SIG_IGN)
        if harness is not None and harness.poll() is None:
            try:
                if output.is_dir() and not (output / 'stop').exists():
                    write(output / 'stop', {'run': run_id})
                harness.wait(timeout=45)
            except (OSError, subprocess.TimeoutExpired):
                harness.send_signal(signal.SIGINT)
                try:
                    harness.wait(timeout=30)
                except subprocess.TimeoutExpired:
                    state['cleanup_incomplete'] = True
        stopped = stop_containers(containers, run_id)
        state['containers_stopped'] = stopped
        # Exact files created by this invocation only; retain every other artifact.
        state['owner_secret_files_removed'] = True
        for record in secret_files:
            try:
                require(harness is None or harness.poll() is not None, 'secret_owner_not_stopped')
                remove_owned_secret(record)
            except (OSError, Refused):
                state['owner_secret_files_removed'] = False
        state['passed'] = state['passed'] and stopped and state['owner_secret_files_removed'] and not state.get('cleanup_incomplete', False)
        # Cleanup can complete an existing private failure receipt. Preserve
        # late owner/money failures as well as pre-browser failures.
        retain_harness_failure(harness, output, root / 'harness.private.log', state)
        try:
            publish_results(output, args.safe, state)
        finally:
            for sig, previous in prior_signals.items():
                signal.signal(sig, previous)
    require(state['passed'], 'qualification_incomplete')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('pair', 'build', 'check', 'run'))
    parser.add_argument('--ur', type=Path)
    parser.add_argument('--xi-commit')
    parser.add_argument('--build', type=Path)
    parser.add_argument('--safe', type=Path)
    parser.add_argument('--node', type=Path)
    parser.add_argument('--browsers', type=Path)
    args = parser.parse_args()
    if args.mode == 'pair':
        print(pair())
    elif args.mode == 'build':
        build(args)
    elif args.mode == 'check':
        check_build(args)
        print('P3 read-only source/binary preflight passed; no runtime actions performed')
    else:
        run(args)


if __name__ == '__main__':
    try:
        main()
    except Refused as error:
        print('P3 refused: ' + str(error), file=sys.stderr)
        sys.exit(1)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError):
        # No exception message: it may contain a driver error, path or credential.
        print('P3 failed: bounded local operation error', file=sys.stderr)
        sys.exit(1)
