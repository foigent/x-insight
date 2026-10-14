#!/usr/bin/env python3
"""Offline admission, cleanup and evidence tests; no services or credentials."""
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location('component', Path(__file__).with_name('p3-component.py'))
p3 = importlib.util.module_from_spec(spec)
spec.loader.exec_module(p3)


class Admission(unittest.TestCase):
    def test_unbound_pair_refuses(self):
        value = {'schema_version': 'xinsight.p3-component-pair.v1', 'unirouter_repository': 'foigent/unirouter'}
        for revision in (None, 'main', 'a' * 39, 'a' * 41, 'A' * 40):
            with self.subTest(revision=revision), patch.object(p3, 'read', return_value=dict(value, unirouter_commit=revision)):
                with self.assertRaisesRegex(p3.Refused, 'pair_not_bound'):
                    p3.pair()

    def test_exact_pair(self):
        value = {'schema_version': 'xinsight.p3-component-pair.v1', 'unirouter_repository': 'foigent/unirouter', 'unirouter_commit': 'a' * 40}
        with patch.object(p3, 'read', return_value=value):
            self.assertEqual(p3.pair(), 'a' * 40)
        with patch.object(p3, 'read', return_value=dict(value, unirouter_repository='other/unirouter')):
            with self.assertRaisesRegex(p3.Refused, 'pair_contract'):
                p3.pair()

    def test_dirty_and_wrong_source_refuse(self):
        for outputs, reason in (([b'b' * 40], 'source_commit_mismatch'), ([b'a' * 40, b'?? untracked'], 'source_dirty')):
            with patch.object(p3, 'capture', side_effect=outputs), self.assertRaisesRegex(p3.Refused, reason):
                p3.commit(Path('/fixture'), 'a' * 40)

    def test_actual_source_check_preserves_git_index(self):
        # Mocked binary admission cannot observe Git's optional index refresh.
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            def git(*args):
                return subprocess.check_output(['git', *args], cwd=root, stderr=subprocess.DEVNULL)
            git('init', '--quiet')
            source = root / 'synthetic.txt'; source.write_text('synthetic fixture\n')
            git('add', '--', source.name)
            git('-c', 'user.name=P3 Offline Fixture', '-c', 'user.email=p3@example.invalid',
                '-c', 'commit.gpgsign=false', 'commit', '--quiet', '-m', 'Synthetic fixture')
            head = git('rev-parse', 'HEAD').decode().strip()
            index = root / '.git/index'
            def snapshot():
                return index.read_bytes(), index.stat().st_mtime_ns
            before = snapshot()
            st = source.stat()
            os.utime(source, ns=(st.st_atime_ns, st.st_mtime_ns + 2_000_000_000))
            self.assertEqual(p3.commit(root, head), head)
            self.assertEqual(snapshot(), before)
            with self.assertRaisesRegex(p3.Refused, 'source_commit_mismatch'):
                p3.commit(root, '0' * 40)
            self.assertEqual(snapshot(), before)
            source.write_text('changed synthetic fixture\n')
            with self.assertRaisesRegex(p3.Refused, 'source_dirty'):
                p3.commit(root, head)
            self.assertEqual(snapshot(), before)

    def test_binary_admission_read_only_and_tamper(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            identity = {'xi_commit': 'a' * 40, 'ur_commit': 'b' * 40}
            for name in p3.BINARY_NAMES:
                (root / name).write_bytes(name.encode())
                (root / name).chmod(0o700)
            manifest = {'schema_version': 'xinsight.p3-build.v1', **identity, 'goos': 'linux', 'goarch': 'amd64',
                        'binaries': {name: p3.sha(root / name) for name in p3.BINARY_NAMES}}
            (root / 'build.json').write_text(json.dumps(manifest))
            before = {p.name: (p.read_bytes(), p.stat().st_mtime_ns) for p in root.iterdir()}
            with patch.object(p3, 'check_sources', return_value=identity):
                self.assertEqual(p3.check_build(SimpleNamespace(build=root)), manifest)
                self.assertEqual(before, {p.name: (p.read_bytes(), p.stat().st_mtime_ns) for p in root.iterdir()})
                (root / p3.BINARY_NAMES[0]).write_bytes(b'changed')
                with self.assertRaisesRegex(p3.Refused, 'binary_digest_mismatch'):
                    p3.check_build(SimpleNamespace(build=root))

    def test_run_refuses_before_writes(self):
        with patch.object(p3, 'check_build', side_effect=p3.Refused('unbound')), patch.object(Path, 'mkdir') as mkdir:
            with self.assertRaisesRegex(p3.Refused, 'unbound'):
                p3.run(SimpleNamespace())
            mkdir.assert_not_called()
        with patch.object(p3, 'check_build', return_value={}), patch.object(p3.os, 'geteuid', return_value=501), patch.object(Path, 'mkdir') as mkdir:
            with self.assertRaisesRegex(p3.Refused, 'root_linux_required'):
                p3.run(SimpleNamespace())
            mkdir.assert_not_called()

    def test_export_rejects_traversal_and_links(self):
        for name, kind in (('../escape', tarfile.REGTYPE), ('link', tarfile.SYMTYPE), ('hard', tarfile.LNKTYPE)):
            data = io.BytesIO()
            with tarfile.open(fileobj=data, mode='w') as archive:
                item = tarfile.TarInfo(name); item.type = kind; item.linkname = '/outside'
                archive.addfile(item)
            with tempfile.TemporaryDirectory() as directory, patch.object(p3, 'capture', return_value=data.getvalue()):
                with self.assertRaisesRegex(p3.Refused, 'source_archive_member'):
                    p3.export_source(Path('/fixture'), 'a' * 40, Path(directory) / 'export')


class PostgresReadiness(unittest.TestCase):
    def identity(self, **updates):
        value = {'database': 'xi_p3_base', 'user': 'xinsight_admin', 'database_oid': '12345',
                 'system_id': '1234567890123456789', 'public_tables': 0}
        return dict(value, **updates)

    def result(self, value):
        return SimpleNamespace(returncode=0, stdout=json.dumps(value).encode(), stderr=b'')

    def test_bootstrap_unavailable_then_real_authenticated_identity(self):
        early = SimpleNamespace(returncode=2, stdout=b'', stderr=b'synthetic private connection detail')
        with patch.object(p3.subprocess, 'run', side_effect=[early, self.result(self.identity())]) as run, patch.object(p3.time, 'sleep'):
            result = p3.postgres_identity('a' * 64, 'xi_p3_base', 'synthetic-password', {'PATH': '/usr/bin:/bin'})
        self.assertEqual(result['database'], 'xi_p3_base')
        self.assertNotIn('public_tables', result)
        self.assertEqual(run.call_count, 2)
        argv = run.call_args.args[0]
        self.assertNotIn('pg_isready', argv)
        self.assertEqual(argv[argv.index('-h') + 1], '127.0.0.1')
        self.assertEqual(argv[argv.index('-d') + 1], 'xi_p3_base')
        self.assertNotIn('synthetic-password', argv)
        self.assertEqual(run.call_args.kwargs['env']['PGPASSWORD'], 'synthetic-password')
        self.assertLessEqual(run.call_args.kwargs['timeout'], 10)

    def test_legacy_socket_ready_can_precede_target_database(self):
        unavailable = SimpleNamespace(returncode=2, stdout=b'', stderr=b'synthetic database unavailable')
        ready = SimpleNamespace(returncode=0, stdout=b'accepting connections', stderr=b'')
        # Reproduce the old decision: socket readiness succeeds although the
        # immediately following target-database query is unavailable.
        with patch.object(p3.subprocess, 'run', side_effect=[ready, unavailable]):
            old = p3.subprocess.run(['docker', 'exec', 'a' * 64, 'pg_isready', '-U', 'xinsight_admin', '-d', 'xi_p3_base'])
            self.assertEqual(old.returncode, 0)
            with self.assertRaisesRegex(p3.Refused, '^command_failed$'):
                p3.capture(['docker', 'exec', 'a' * 64, 'psql', '-XAt', '-U', 'xinsight_admin', '-d', 'xi_p3_base'])
        # The replacement stays before admission until a real authenticated
        # TCP identity query succeeds; it never consults that socket status.
        with patch.object(p3.subprocess, 'run', side_effect=[unavailable, self.result(self.identity())]) as run, patch.object(p3.time, 'sleep'):
            self.assertEqual(p3.postgres_identity('a' * 64, 'xi_p3_base', 'synthetic-password', {})['database'], 'xi_p3_base')
            self.assertTrue(all('pg_isready' not in call.args[0] for call in run.call_args_list))

    def test_wrong_database_or_role_refuses_without_retry(self):
        for value in (self.identity(database='other'), self.identity(user='other')):
            with patch.object(p3.subprocess, 'run', return_value=self.result(value)) as run, self.assertRaisesRegex(p3.Refused, 'postgres_identity_mismatch'):
                p3.postgres_identity('a' * 64, 'xi_p3_base', 'synthetic-password', {})
            self.assertEqual(run.call_count, 1)

    def test_existing_tables_refuse(self):
        with patch.object(p3.subprocess, 'run', return_value=self.result(self.identity(public_tables=1))), self.assertRaisesRegex(p3.Refused, 'base_not_empty'):
            p3.postgres_identity('a' * 64, 'xi_p3_base', 'synthetic-password', {})

    def test_bad_identity_receipts_refuse_without_raw_detail(self):
        for raw in (b'synthetic private error', b'[]', json.dumps(self.identity(system_id='private')).encode()):
            probe = SimpleNamespace(returncode=0, stdout=raw, stderr=b'')
            with patch.object(p3.subprocess, 'run', return_value=probe), self.assertRaisesRegex(p3.Refused, '^postgres_identity_contract$'):
                p3.postgres_identity('a' * 64, 'xi_p3_base', 'synthetic-password', {})

    def test_readiness_deadline_is_bounded(self):
        with patch.object(p3.time, 'monotonic', side_effect=[0, 61]), patch.object(p3.subprocess, 'run') as run, self.assertRaisesRegex(p3.Refused, 'postgres_ready_timeout'):
            p3.postgres_identity('a' * 64, 'xi_p3_base', 'synthetic-password', {})
        run.assert_not_called()


class Evidence(unittest.TestCase):
    def valid(self):
        return {'schema_version': 'xinsight.p3-browser.v1', 'stage': 'complete', 'passed': True,
                'checks': list(p3.CHECKS), 'request_sha256': 'a' * 64, 'source_sha256': 'b' * 64,
                'charged_micros': '0', 'calculated_micros': '123'}

    def test_allowlist_drops_private_fields(self):
        value = self.valid()
        self.assertEqual(p3.safe_browser(dict(value, password='private', error='private', dom='private')), value)

    def test_incomplete_or_malformed_pass_refuses(self):
        invalid = [[], {}, dict(self.valid(), checks=[]), dict(self.valid(), stage='ordinary_login'),
                   dict(self.valid(), checks=list(p3.CHECKS) + [p3.CHECKS[0]]),
                   dict(self.valid(), request_sha256='not-digest'), dict(self.valid(), calculated_micros=0),
                   dict(self.valid(), passed='true')]
        missing = self.valid(); del missing['source_sha256']; invalid.append(missing)
        for value in invalid:
            with self.subTest(value=value), self.assertRaises(p3.Refused):
                p3.safe_browser(value)

    def test_partial_timeout_wait_is_closed_and_cannot_claim_pass(self):
        partial = {'schema_version': 'xinsight.p3-browser.v1', 'stage': 'ordinary_login',
                   'passed': False, 'checks': ['exact_receipt_identity'], 'wait': 'navigation'}
        self.assertEqual(p3.safe_browser(partial), partial)
        self.assertEqual(p3.safe_browser(dict(partial, password='synthetic-private', dom='synthetic-private')), partial)
        for waiting in ('https://synthetic-private.invalid', 'synthetic-password', [], {}, None):
            with self.subTest(waiting=waiting), self.assertRaisesRegex(p3.Refused, '^browser_wait$'):
                p3.safe_browser(dict(partial, wait=waiting))
        with self.assertRaisesRegex(p3.Refused, '^incomplete_browser_pass$'):
            p3.safe_browser(dict(self.valid(), wait='navigation'))
        self.assertEqual(p3.safe_browser(dict(self.valid(), wait='not_waiting')), dict(self.valid(), wait='not_waiting'))

    def test_bad_browser_receipt_still_has_closed_failure(self):
        for body in ('[]', '{broken', json.dumps(dict(self.valid(), request_sha256='private'))):
            with tempfile.TemporaryDirectory() as directory:
                root = Path(directory); output = root / 'private'; safe = root / 'safe'
                output.mkdir(); safe.mkdir(); (output / 'browser-result.json').write_text(body)
                state = {'passed': True}
                p3.publish_results(output, safe, state)
                self.assertEqual(json.loads((safe / 'qualification-result.json').read_text()),
                                 {'passed': False, 'failure_code': 'browser_receipt_invalid'})
                self.assertFalse((safe / 'browser-result.json').exists())


class HarnessDiagnostics(unittest.TestCase):
    def test_only_exact_closed_markers_leave_private_log(self):
        with tempfile.TemporaryDirectory() as directory:
            log = Path(directory) / 'private.log'
            log.write_bytes(b'private password\nP3_HARNESS_STAGE=producer_start\n'
                            b'private assertion P3_HARNESS_STAGE=ordinary_read\n'
                            b'P3_HARNESS_STAGE=private-password\n'
                            b'P3_HARNESS_STAGE=import_chain\n')
            self.assertEqual(p3.closed_harness_stage(log), 'import_chain')
            log.write_bytes(b'private password\nP3_HARNESS_STAGE=unknown-private-detail\n')
            self.assertEqual(p3.closed_harness_stage(log), 'not_reported')
            log.write_bytes(b'P3_HARNESS_STAGE=producer_start\n' + b'x' * 8192)
            self.assertEqual(p3.closed_harness_stage(log), 'diagnostic_truncated')

    def test_actual_early_child_exit_is_distinct_and_never_exports_log(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); log = root / 'private.log'
            with log.open('wb') as out:
                process = subprocess.Popen([sys.executable, '-c',
                    'print("private-password"); print("P3_HARNESS_STAGE=production_fixture"); raise SystemExit(7)'],
                    stdout=out, stderr=out)
                process.wait(timeout=10)
            # The old combined predicate loses both the actual exit and stage.
            with self.assertRaisesRegex(p3.Refused, '^harness_ready_failed$'):
                p3.require(process.poll() is None, 'harness_ready_failed')
            state = {}
            with self.assertRaisesRegex(p3.Refused, '^harness_exited_before_ready$'):
                p3.wait_harness(process, root, log, state)
            self.assertEqual(state, {'harness_exit_code': 7, 'harness_stage': 'production_fixture',
                                     'harness_failure': {'kind': 'unclassified', 'step': 'unclassified'}})
            self.assertNotIn('private-password', json.dumps(state))

    def test_zero_exit_without_chain_still_refuses(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); log = root / 'private.log'; log.write_bytes(b'')
            state = {}
            with self.assertRaisesRegex(p3.Refused, '^harness_exited_before_ready$'):
                p3.wait_harness(Mock(poll=Mock(return_value=0)), root, log, state)
            self.assertEqual(state, {'harness_exit_code': 0, 'harness_stage': 'not_reported',
                                     'harness_failure': {'kind': 'unclassified', 'step': 'unclassified'}})

    def test_original_deadline_and_ready_receipt(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); log = root / 'private.log'
            log.write_bytes(b'P3_HARNESS_STAGE=producer_start\n')
            state = {}
            with patch.object(p3.time, 'monotonic', side_effect=[0, 300]), self.assertRaisesRegex(p3.Refused, '^harness_ready_timeout$'):
                p3.wait_harness(Mock(poll=Mock(return_value=None)), root, log, state)
            self.assertEqual(state, {'harness_stage': 'producer_start',
                                     'harness_failure': {'kind': 'unclassified', 'step': 'unclassified'}})
            (root / 'p3-chain.json').write_text('{}')
            state = {}; process = Mock()
            p3.wait_harness(process, root, log, state)
            process.poll.assert_not_called()
            self.assertEqual(state, {})


class FailureProjection(unittest.TestCase):
    secret = 'synthetic-private-dsn-account-token-and-body'

    def receipt(self, stage, **updates):
        return dict({'stage': stage, 'binary': '/private/' + self.secret,
                     'binary_sha256': self.secret, 'exit_code': 1,
                     'context_timeout': False, 'context_cancelled': False, 'stop_requested': False,
                     'diagnostic': {'error_class': 'postgres_error', 'sqlstate': '42501',
                                    'stderr': self.secret}, 'safe_receipt': {'body': self.secret}}, **updates)

    def write(self, root, name, value):
        path = root / name
        path.write_text(json.dumps(value))
        path.chmod(0o600)
        return path

    def project(self, root, log_body=b'private raw text'):
        log = root / 'harness.private.log'; log.write_bytes(log_body)
        value = p3.harness_failure_projection(root, log)
        self.assertNotIn(self.secret, json.dumps(value))
        self.assertNotIn('/private/', json.dumps(value))
        return value

    def test_each_existing_cli_step_retains_category_not_private_fields(self):
        for name, step in p3.CLI_STEPS.items():
            with self.subTest(name=name), tempfile.TemporaryDirectory() as directory:
                root = Path(directory); self.write(root, name + '-failure.json', self.receipt(name))
                value = self.project(root)
                self.assertEqual(value['kind'], 'cli'); self.assertEqual(value['step'], step)
                self.assertEqual(value['sqlstate'], '42501')
                self.assertEqual(set(value), {'kind', 'step', 'exit_code', 'context_timeout',
                                             'context_cancelled', 'stop_requested', 'error_class', 'sqlstate'})

    def test_formal_grant_check_and_noncli_preflight(self):
        for name, raw, step in [('formal-cli-failure.json', self.receipt('formal_grant_check'), 'formal_grant_check'),
                                ('check-failure.json', {'step': 'source_preflight', 'diagnostic': {'error_class': 'source_scope'},
                                                        'context_timeout': False, 'context_cancelled': False,
                                                        'error': self.secret}, 'source_preflight')]:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as directory:
                root = Path(directory); self.write(root, name, raw)
                self.assertEqual(self.project(root)['step'], step)

    def test_actual_child_exit_projects_then_publishes_only_safe_json(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); output = root / 'private'; safe = root / 'safe'; output.mkdir(); safe.mkdir()
            raw = self.receipt('source-bootstrap-3'); raw['exit_code'] = 7
            log = root / 'harness.private.log'
            code = 'import json,pathlib; p=pathlib.Path(' + repr(str(output / 'source-bootstrap-3-failure.json')) + '); p.write_text(' + repr(json.dumps(raw)) + '); p.chmod(0o600); print(' + repr(self.secret) + '); print("P3_HARNESS_STAGE=import_chain"); raise SystemExit(7)'
            with log.open('wb') as target:
                child = subprocess.Popen([sys.executable, '-c', code], stdout=target, stderr=target)
                child.wait(timeout=10)
            state = {'passed': False, 'containers_stopped': True, 'owner_secret_files_removed': True}
            with self.assertRaisesRegex(p3.Refused, '^harness_exited_before_ready$'):
                p3.wait_harness(child, output, log, state)
            self.assertEqual(state['harness_failure']['step'], 'source_bootstrap_3')
            self.assertEqual(state['harness_failure']['exit_code'], 7)
            p3.publish_results(output, safe, state)
            self.assertEqual([x.name for x in safe.iterdir()], ['qualification-result.json'])
            exported = json.loads((safe / 'qualification-result.json').read_text())
            self.assertFalse(exported['passed']); self.assertTrue(exported['containers_stopped'])
            self.assertNotIn(self.secret, json.dumps(exported))

    def test_child_start_exit_and_wait_roles_are_closed(self):
        for index, role in enumerate(('producer', 'completed_feed', 'source_worker')):
            for prefix, stage in [('child-start-failure', 'child_start'), ('child-failure', 'unexpected_child_exit')]:
                with self.subTest(index=index, prefix=prefix), tempfile.TemporaryDirectory() as directory:
                    root = Path(directory); self.write(root, f'{prefix}-{index}.json', self.receipt(stage))
                    self.assertEqual(self.project(root)['step'], role)
        for stage, step in p3.WAIT_STEPS.items():
            for reason in ('timeout', 'cancelled', 'stop_requested'):
                with self.subTest(stage=stage, reason=reason), tempfile.TemporaryDirectory() as directory:
                    root = Path(directory)
                    self.write(root, 'wait-failure.json', {'stage': stage, 'reason': reason, 'request_id': self.secret,
                                                         'producer_states_after_drain': '/private/' + self.secret})
                    value = self.project(root)
                    self.assertEqual(value['step'], step); self.assertEqual(value['context_timeout'], reason == 'timeout')

    def test_each_noncli_assertion_label_and_injected_markers(self):
        for label, step in p3.FAILURE_CHECKS.items():
            with self.subTest(label=label), tempfile.TemporaryDirectory() as directory:
                self.assertEqual(self.project(Path(directory), ('    fixture_test.go:123: P3 fixture: ' + label + '\n').encode()),
                                 {'kind': 'assertion', 'step': step})
        for line in [b'raw body P3 fixture: source preflight', b'    fixture.go:1: P3 fixture: source preflight secret',
                     b'/private/fixture.go:1: P3 fixture: source preflight', b'P3_HARNESS_CHECK=source_preflight']:
            with tempfile.TemporaryDirectory() as directory:
                self.assertEqual(self.project(Path(directory), line)['kind'], 'unclassified')

    def test_malformed_oversized_linked_and_duplicate_json_stays_closed(self):
        bad = [b'[]', b'{broken', b'x' * 8193, b'{"stage":"source-bootstrap-0","stage":"private"}',
               json.dumps(self.receipt('source-bootstrap-0', exit_code=True)).encode(),
               json.dumps(self.receipt('source-bootstrap-0', context_timeout='private')).encode(),
               json.dumps(self.receipt('source-bootstrap-0', diagnostic={'error_class': self.secret})).encode(),
               json.dumps(self.receipt('source-bootstrap-0', diagnostic={'error_class': 'postgres_error', 'sqlstate': self.secret})).encode()]
        for body in bad:
            with self.subTest(body=body[:20]), tempfile.TemporaryDirectory() as directory:
                root = Path(directory); path = root / 'source-bootstrap-0-failure.json'; path.write_bytes(body); path.chmod(0o600)
                self.assertEqual(self.project(root), {'kind': 'invalid_diagnostic', 'step': 'unclassified'})
        for kind in ('symlink', 'hardlink', 'fifo', 'mode'):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as directory:
                root = Path(directory); target = self.write(root, 'outside', self.receipt('source-bootstrap-0'))
                path = root / 'source-bootstrap-0-failure.json'
                if kind == 'symlink': path.symlink_to(target)
                elif kind == 'hardlink': os.link(target, path)
                elif kind == 'fifo': os.mkfifo(path, 0o600)
                else: path.write_bytes(target.read_bytes()); path.chmod(0o644)
                self.assertEqual(self.project(root)['kind'], 'invalid_diagnostic')

    def test_conflicting_receipts_and_cleanup_receipts_not_misattributed(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.write(root, 'child-stop-0.json', self.receipt('child_stop'))
            self.assertEqual(self.project(root)['kind'], 'unclassified')
            self.write(root, 'resource-check-failure.json', self.receipt('resource-check'))
            self.write(root, 'formal-cli-failure.json', self.receipt('formal_grant_check'))
            self.assertEqual(self.project(root), {'kind': 'ambiguous', 'step': 'unclassified'})

    def test_separate_receipt_survives_large_log_without_exporting_it(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.assertTrue(self.project(root, b'x' * 8193)['diagnostic_truncated'])
            self.write(root, 'check-failure.json', {'step': 'resource_registration', 'diagnostic': {'error_class': 'postgres_error', 'sqlstate': '23505'}, 'context_timeout': False, 'context_cancelled': False})
            self.assertEqual(self.project(root, b'x' * 8193)['step'], 'resource_registration')


class BrowserLifecycle(unittest.TestCase):
    secret = 'synthetic-private-error-not-for-export'

    def runner(self, stage, **updates):
        return dict({'schema_version': 'xinsight.p3-runner.v1', 'stage': stage,
                     'test_started': stage != 'runner_initialization',
                     'input_completed': stage in ('test_body', 'complete'),
                     'body_started': stage in ('test_body', 'complete'),
                     'timed_out': False, 'global_error_reported': False}, **updates)

    def test_real_process_faults_retain_only_closed_phases(self):
        for stage, category in [('runner_initialization', 'configuration_or_collection_unknown'),
                                ('fixture', 'fixture'), ('input_receipts', 'input_receipts'),
                                ('test_body', 'test_body')]:
            with self.subTest(stage=stage), tempfile.TemporaryDirectory() as directory:
                root = Path(directory); state = {}
                value = dict(self.runner(stage), error=self.secret, path='/private/' + self.secret)
                code = 'import json,pathlib; p=pathlib.Path(' + repr(str(root / 'browser-runner.json')) + '); p.write_text(' + repr(json.dumps(value)) + '); p.chmod(0o600); print(' + repr(self.secret) + '); raise SystemExit(7)'
                with (root / 'private.log').open('wb') as log:
                    self.assertEqual(p3.browser_run([sys.executable, '-c', code], root, {}, log, output=root, state=state), 7)
                result = state['browser_execution']
                self.assertEqual(result['category'], category); self.assertEqual(result['exit_code'], 7)
                self.assertNotIn(self.secret, json.dumps(result))
                safe = root / 'safe'; safe.mkdir(); p3.publish_results(root, safe, dict(state, passed=False))
                self.assertEqual([p.name for p in safe.iterdir()], ['qualification-result.json'])

    def test_actual_start_failure_and_unreported_exit_remain_distinct(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); state = {}
            with self.assertRaises(FileNotFoundError):
                p3.browser_run([root / 'absent'], root, {}, subprocess.DEVNULL, output=root, state=state)
            self.assertEqual(state['browser_execution']['category'], 'process_start')
            self.assertFalse(state['browser_execution']['process_started'])
            self.assertEqual(p3.browser_run([sys.executable, '-c', 'raise SystemExit(3)'], root, {}, subprocess.DEVNULL, output=root, state=state), 3)
            self.assertEqual(state['browser_execution']['category'], 'startup_or_configuration_unknown')
            self.assertFalse(state['browser_execution']['reporter_loaded'])

    def test_actual_timeout_joins_owned_process_and_preserves_timeout(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); state = {}
            with self.assertRaises(subprocess.TimeoutExpired):
                p3.browser_run([sys.executable, '-c', 'import time;time.sleep(30)'], root, {}, subprocess.DEVNULL, output=root, state=state, timeout=0.1)
            self.assertEqual(state['browser_execution']['category'], 'timeout')
            self.assertTrue(state['browser_execution']['timed_out'])
            self.assertIsInstance(state['browser_execution']['exit_code'], int)

    def test_zero_exit_does_not_hide_missing_browser_receipt(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); state = {}
            self.assertEqual(p3.browser_run([sys.executable, '-c', 'pass'], root, {}, subprocess.DEVNULL, output=root, state=state), 0)
            self.assertEqual(state['browser_execution']['category'], 'receipt_missing')
            self.assertFalse(state['browser_execution']['browser_receipt_present'])

    def test_runner_invalid_or_replaced_input_stays_closed(self):
        bad = [b'{broken', b'[]', b'x' * 8193,
               json.dumps(self.runner('private')).encode(),
               json.dumps(self.runner('fixture', body_started='private')).encode(),
               b'{"schema_version":"xinsight.p3-runner.v1","stage":"fixture","stage":"private"}']
        for body in bad:
            with self.subTest(body=body[:15]), tempfile.TemporaryDirectory() as directory:
                root = Path(directory); path = root / 'browser-runner.json'; path.write_bytes(body); path.chmod(0o600)
                self.assertEqual(p3.browser_execution_projection(root, True, 1, False)['category'], 'invalid_diagnostic')
        for kind in ('symlink', 'hardlink', 'fifo', 'mode'):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as directory:
                root = Path(directory); target = root / 'target'; target.write_text(json.dumps(self.runner('fixture'))); target.chmod(0o600)
                path = root / 'browser-runner.json'
                if kind == 'symlink': path.symlink_to(target)
                elif kind == 'hardlink': os.link(target, path)
                elif kind == 'fifo': os.mkfifo(path, 0o600)
                else: path.write_bytes(target.read_bytes()); path.chmod(0o644)
                self.assertEqual(p3.browser_execution_projection(root, True, 1, False)['category'], 'invalid_diagnostic')

    def test_input_failure_browser_receipt_is_allowed_but_never_passes(self):
        value = {'schema_version': 'xinsight.p3-browser.v1', 'stage': 'input_receipts', 'checks': [], 'passed': False}
        self.assertEqual(p3.safe_browser(dict(value, error=self.secret)), value)
        with self.assertRaises(p3.Refused):
            p3.safe_browser(dict(value, passed=True))


class ProducerMoneyFinalization(unittest.TestCase):
    def receipts(self):
        chain = {'request_id': 'synthetic-request', 'source_system_id': 'synthetic-source',
                 'source_instance': 'synthetic-instance', 'org_id': '1', 'project_id': '2'}
        producer = {'snapshot': {'request_id': chain['request_id'], 'money': {'charged_cost_micros': '0', 'calculated_cost_micros': '123'}},
                    'snapshot_sha256': 'sha256:' + 'a' * 64, 'cost_source_snapshot_sha256': 'sha256:' + 'a' * 64}
        browser = dict(Evidence().valid(), request_sha256=p3.hashlib.sha256(chain['request_id'].encode()).hexdigest(),
                       source_sha256=p3.hashlib.sha256('/'.join(chain[k] for k in ('source_system_id','source_instance','org_id','project_id')).encode()).hexdigest())
        return chain, producer, browser

    def test_final_authoritative_receipt_required_and_equal_to_displayed_amounts(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); output = root / 'xi'; output.mkdir(); (root / 'ur').mkdir()
            chain, producer, browser = self.receipts(); p3.write(output / 'p3-chain.json', chain)
            # The live producer has not finalized yet: the old browser read
            # fails. After joining the owner, its actual receipt is required.
            with self.assertRaisesRegex(p3.Refused, '^producer_money_receipt_invalid$'):
                p3.verify_producer_money(output, browser)
            p3.write(root / 'ur/economic.json', producer)
            p3.verify_producer_money(output, browser)
            for key in ('charged_micros', 'calculated_micros', 'request_sha256', 'source_sha256'):
                wrong = dict(browser); wrong[key] = '999' if key.endswith('micros') else 'b' * 64
                with self.subTest(key=key), self.assertRaises(p3.Refused):
                    p3.verify_producer_money(output, wrong)
            producer['cost_source_snapshot_sha256'] = 'sha256:' + 'b' * 64
            (root / 'ur/economic.json').write_text(json.dumps(producer))
            with self.assertRaisesRegex(p3.Refused, '^producer_economic_binding$'):
                p3.verify_producer_money(output, browser)


    def test_exact_producer_sha256_reference_required_even_when_both_fields_match(self):
        # The fixed UR BuildSnapshot/source-facts contract emits sha256:<64 lowercase hex>.
        # Matching malformed identities must not weaken the economic/source binding.
        invalid = (None, 123, [], 'a' * 64, 'SHA256:' + 'a' * 64,
                   'sha512:' + 'a' * 64, 'digest:sha256:' + 'a' * 64,
                   'sha256:' + 'a' * 63, 'sha256:' + 'a' * 65,
                   'sha256:' + 'A' * 64, 'sha256:' + 'a' * 63 + 'g',
                   'sha256:' + 'a' * 64 + '\n')
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); output = root / 'xi'; output.mkdir(); (root / 'ur').mkdir()
            chain, producer, browser = self.receipts()
            p3.write(output / 'p3-chain.json', chain)
            for value in invalid:
                producer['snapshot_sha256'] = value
                producer['cost_source_snapshot_sha256'] = value
                (root / 'ur/economic.json').write_text(json.dumps(producer))
                with self.subTest(value=value), self.assertRaisesRegex(p3.Refused, '^producer_economic_binding$'):
                    p3.verify_producer_money(output, browser)


class HarnessFinalization(unittest.TestCase):
    secret = 'synthetic-private-closing-error-not-for-export'

    def exercise(self, scenario):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); output = root / 'xi'; output.mkdir(); (root / 'ur').mkdir()
            chain, producer, browser = ProducerMoneyFinalization().receipts()
            p3.write(output / 'p3-chain.json', chain); p3.write(output / 'browser-result.json', browser)
            state = {'passed': True}; log_path = root / 'harness.private.log'
            # An actual owned process models the post-page shutdown protocol;
            # browser JSON is synthetic fault input, not UI acceptance evidence.
            code = """import json,pathlib,time,sys
root=pathlib.Path(sys.argv[1]);scenario=sys.argv[2];producer=json.loads(sys.argv[3])
def write(path,value):
 path.write_text(json.dumps(value));path.chmod(0o600)
print('P3_HARNESS_STAGE=browser_ready',flush=True)
(root/'ready').touch()
if scenario=='after_browser_exit':raise SystemExit(0)
while not (root/'xi/stop').exists():time.sleep(0.01)
if scenario=='shutdown_timeout':
 write(root/'xi/check-failure.json',{'step':'resource_registration','diagnostic':{'error_class':'unknown'},'context_timeout':False,'context_cancelled':False,'error':'synthetic-private-closing-error-not-for-export'})
 time.sleep(30)
if scenario=='shutdown_nonzero':
 print('synthetic-private-closing-error-not-for-export'+'x'*16384,flush=True)
 print('    fixture_test.go:1220: P3 fixture: UR/XI exact terminal money equality',flush=True)
 raise SystemExit(7)
write(root/'xi/children-stopped.json',{'waited':True,'successful':scenario!='children_failed'})
if scenario=='children_failed':
 write(root/'xi/child-stop-0.json',{'stage':'child_stop','exit_code':9,'context_timeout':False,'context_cancelled':True,'stop_requested':True,'binary':'synthetic-private-closing-error-not-for-export'})
if scenario=='money_failed':producer['snapshot']['money']['calculated_cost_micros']='999'
write(root/'ur/economic.json',producer)
"""
            with log_path.open('wb') as log:
                process = subprocess.Popen([sys.executable, '-c', code, str(root), scenario, json.dumps(producer)], stdout=log, stderr=log)
                try:
                    import time
                    until = time.monotonic() + 5
                    while not (root / 'ready').exists():
                        self.assertIsNone(process.poll()); self.assertLess(time.monotonic(), until); time.sleep(0.01)
                    if scenario == 'after_browser_exit': process.wait(timeout=5)
                    try:
                        p3.finish_harness(process, output, browser, state, 'synthetic-run', timeout=0.1 if scenario=='shutdown_timeout' else 5)
                    except p3.Refused as error:
                        state['failure_code'] = str(error)
                    finally:
                        if process.poll() is None:
                            process.terminate(); process.wait(timeout=5)
                    p3.retain_harness_failure(process, output, log_path, state)
                finally:
                    if process.poll() is None: process.kill(); process.wait(timeout=5)
            self.assertFalse(state['passed'])
            self.assertNotIn(self.secret, json.dumps(state))
            safe = root / 'safe'; safe.mkdir(); p3.publish_results(output, safe, state)
            saved = json.loads((safe / 'qualification-result.json').read_text())
            self.assertFalse(saved['passed']); self.assertTrue(json.loads((safe / 'browser-result.json').read_text())['passed'])
            self.assertNotIn(self.secret, json.dumps(saved))
            return state

    def test_page_pass_cannot_cover_harness_already_exited(self):
        state = self.exercise('after_browser_exit')
        self.assertEqual(state['failure_code'], 'harness_exited_after_browser')
        self.assertEqual(state['harness_exit_code'], 0); self.assertEqual(state['harness_phase'], 'browser_active')

    def test_nonzero_shutdown_retains_final_go_money_assertion_from_bounded_tail(self):
        state = self.exercise('shutdown_nonzero')
        self.assertEqual(state['failure_code'], 'harness_shutdown_failed'); self.assertEqual(state['harness_exit_code'], 7)
        self.assertEqual(state['harness_failure']['step'], 'terminal_money_equality')
        self.assertTrue(state['harness_failure']['diagnostic_truncated'])

    def test_shutdown_timeout_retains_existing_private_failure(self):
        state = self.exercise('shutdown_timeout')
        self.assertEqual(state['failure_code'], 'harness_shutdown_timeout'); self.assertTrue(state['harness_timed_out'])
        self.assertEqual(state['harness_phase'], 'shutdown'); self.assertEqual(state['harness_failure']['step'], 'resource_registration')

    def test_page_pass_cannot_cover_failed_child_join(self):
        state = self.exercise('children_failed')
        self.assertEqual(state['failure_code'], 'children_not_joined'); self.assertEqual(state['harness_phase'], 'children_join')
        self.assertEqual(state['harness_failure']['kind'], 'child_exit'); self.assertEqual(state['harness_failure']['step'], 'producer')
        self.assertEqual(state['harness_failure']['exit_code'], 9)

    def test_page_pass_cannot_cover_final_producer_money_mismatch(self):
        state = self.exercise('money_failed')
        self.assertEqual(state['failure_code'], 'producer_money_binding'); self.assertEqual(state['harness_phase'], 'money_binding')
        self.assertNotIn('producer_money_binding_verified', state)

    def test_successful_finalization_still_requires_caller_acceptance(self):
        state = self.exercise('success')
        self.assertNotIn('failure_code', state); self.assertTrue(state['producer_money_binding_verified'])
        self.assertEqual(state['harness_phase'], 'complete'); self.assertEqual(state['harness_exit_code'], 0)
        self.assertNotIn('harness_failure', state)

    def test_actual_harness_exit_during_browser_terminates_owned_browser(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); state = {'passed': False, 'harness_phase': 'browser_active'}; log_path = root / 'private.log'
            with log_path.open('wb') as log:
                harness = subprocess.Popen([sys.executable, '-c', 'import time;print("P3_HARNESS_STAGE=browser_ready",flush=True);time.sleep(0.1);raise SystemExit(7)'], stdout=log, stderr=log)
                try:
                    with self.assertRaisesRegex(p3.Refused, '^harness_exited_during_browser$'):
                        p3.browser_run([sys.executable, '-c', 'import time;time.sleep(30)'], root, {}, log, output=root, state=state, timeout=5, harness=harness)
                    harness.wait(timeout=5); state['failure_code']='harness_exited_during_browser'
                    p3.retain_harness_failure(harness, root, log_path, state)
                    self.assertEqual(state['harness_exit_code'], 7); self.assertEqual(state['harness_stage'], 'browser_ready')
                    self.assertIsInstance(state['browser_execution']['exit_code'], int)
                    self.assertFalse(state['browser_execution']['timed_out'])
                finally:
                    if harness.poll() is None: harness.kill(); harness.wait(timeout=5)

    def test_malformed_final_request_is_a_closed_refusal(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory); output=root/'xi';output.mkdir();(root/'ur').mkdir()
            chain,producer,browser=ProducerMoneyFinalization().receipts()
            chain['request_id']=123;producer['snapshot']['request_id']=123
            p3.write(output/'p3-chain.json',chain);p3.write(root/'ur/economic.json',producer)
            with self.assertRaisesRegex(p3.Refused,'^producer_request_binding$'):
                p3.verify_producer_money(output,browser)


class Cleanup(unittest.TestCase):
    def test_container_start_error_has_safe_specific_code_and_retains_identity(self):
        with tempfile.TemporaryDirectory() as directory:
            containers = []
            with patch.object(p3, 'capture', side_effect=p3.Refused('command_failed')), self.assertRaisesRegex(p3.Refused, '^xi_container_start_failed$'):
                p3.start_container(Path(directory), 'xi', 'p3-public-test', [], {}, containers)
            self.assertEqual(containers, [Path(directory) / 'xi.cid'])

    def test_browser_timeout_stops_owned_process_group(self):
        process = Mock(pid=12345)
        process.wait.side_effect = [subprocess.TimeoutExpired('fixture', 180), 0]
        process.poll.return_value = None
        with patch.object(p3.subprocess, 'Popen', return_value=process) as start, patch.object(p3.os, 'killpg') as stop:
            with self.assertRaises(subprocess.TimeoutExpired):
                p3.browser_run(['fixture'], Path('/fixture'), {}, None)
            self.assertTrue(start.call_args.kwargs['start_new_session'])
            stop.assert_called_once_with(12345, p3.signal.SIGTERM)

    def test_create_timeout_retains_exact_container_identity(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); containers = []
            def timed_out(argv, **_):
                Path(argv[argv.index('--cidfile') + 1]).write_text('a' * 64)
                raise subprocess.TimeoutExpired('docker', 120)
            with patch.object(p3, 'capture', side_effect=timed_out), self.assertRaises(subprocess.TimeoutExpired):
                p3.start_container(root, 'xi', 'p3-public-test', [], {}, containers)
            self.assertEqual(containers, [root / 'xi.cid'])
            self.assertEqual(containers[0].read_text(), 'a' * 64)

    def test_cleanup_rejects_unowned_container(self):
        with tempfile.TemporaryDirectory() as directory:
            cid = Path(directory) / 'xi.cid'; cid.write_text('a' * 64)
            fake_stat = SimpleNamespace(st_mode=0o100600, st_uid=0, st_nlink=1, st_size=64)
            with patch.object(Path, 'lstat', return_value=fake_stat), patch.object(p3, 'capture', return_value=b'other-run') as call:
                self.assertFalse(p3.stop_containers([cid], 'p3-public-test'))
                self.assertEqual(call.call_count, 1)
                self.assertEqual(call.call_args.args[0][1], 'inspect')

    def test_owned_cleanup_uses_exact_cid(self):
        with tempfile.TemporaryDirectory() as directory:
            cid = Path(directory) / 'xi.cid'; cid.write_text('a' * 64)
            fake_stat = SimpleNamespace(st_mode=0o100600, st_uid=0, st_nlink=1, st_size=64)
            with patch.object(Path, 'lstat', return_value=fake_stat), patch.object(p3, 'capture', side_effect=[b'p3-public-test', b'']) as call:
                self.assertTrue(p3.stop_containers([cid], 'p3-public-test'))
                self.assertEqual(call.call_args.args[0], ['docker', 'stop', '--time', '20', 'a' * 64])

    def test_secret_replacement_refuses_removal(self):
        with tempfile.TemporaryDirectory() as directory:
            file = Path(directory) / 'synthetic'; file.write_text('not a credential')
            record = (file, file.stat().st_dev, file.stat().st_ino, p3.sha(file))
            file.write_text('different fixture')
            original = file.lstat()
            owned_stat = SimpleNamespace(st_mode=original.st_mode, st_nlink=1, st_uid=0,
                                         st_dev=original.st_dev, st_ino=original.st_ino)
            with patch.object(Path, 'lstat', return_value=owned_stat), patch.object(Path, 'unlink') as unlink, self.assertRaisesRegex(p3.Refused, 'secret_cleanup_boundary'):
                p3.remove_owned_secret(record)
            unlink.assert_not_called()


if __name__ == '__main__':
    unittest.main()
