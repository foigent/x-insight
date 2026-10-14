#!/usr/bin/env python3
"""Offline build-entry control-flow tests; tool stubs do not qualify artifacts."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

SOURCE = Path(__file__).resolve().parents[1]
# The real entry runs in an isolated fixture, never in the user's checkout.
STUB = '''#!/usr/bin/env python3
import json, os, pathlib, sys
name = pathlib.Path(sys.argv[0]).name
args = sys.argv[1:]
with open(os.environ['CALL_LOG'], 'a') as log:
    log.write(json.dumps([name, *args]) + '\\n')
if name == 'git':
    if args[:1] == ['rev-parse']: print('1' * 40)
    elif args[:1] == ['status']:
        if os.environ.get('DIRTY') or (len(args) > 2 and os.environ.get('GENERATED_DRIFT')): print(' M changed')
    elif args[:1] == ['show']: print('1700000000')
    elif args[:1] == ['ls-files']:
        for base in (a for a in args[1:] if a != '-z'):
            for p in sorted(pathlib.Path(base).rglob('*')):
                if p.is_file(): sys.stdout.write(str(p) + ('\\0' if '-z' in args else '\\n'))
elif name == 'go':
    if args == ['env', 'GOVERSION']: print(os.environ.get('GO_VERSION', 'go1.25.12'))
    elif args[:1] == ['build']:
        p = pathlib.Path(args[args.index('-o') + 1]); p.write_bytes(b'CONTROL_FLOW_TEST_STUB'); p.chmod(0o755)
    else: sys.exit('unexpected Go execution, including pack')
elif name == 'node':
    if args == ['--version']: print(os.environ.get('NODE_VERSION', 'v22.23.1'))
    elif args[:1] != ['scripts/build-manual.mjs']: sys.exit('unexpected node execution')
elif name == 'npm':
    if args == ['--version']: print('10.9.8')
    elif args != ['--offline', 'run', 'build']: sys.exit('dependency installation or unexpected npm execution')
'''


class BuildEntryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='xi-build-entry-test-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / 'source'
        self.root.mkdir()
        for rel in ['scripts/build-release.sh', 'scripts/verify-release.sh', 'scripts/build-signed-release.sh', 'release/d-validation-config.json',
                    'release/d-validation-compatibility-matrix.json', 'release/d-validation-notes.md']:
            target = self.root / rel
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(SOURCE / rel, target)
        # Exercise the real composition; stubs below only isolate each gate's exit.
        shutil.copyfile(SOURCE / 'scripts/verify-protected-runtime.sh', self.root / 'scripts/verify-protected-runtime.sh')
        for name in ('reference', 'litellm'):
            self.put(f'scripts/verify-phase2c-{name}-runtime.sh',
                     '#!/bin/sh\necho gate-' + name + ' >> "$CALL_LOG"\n'
                     'if [ "${REJECT_GATE:-}" = "' + name + '" ]; then\n'
                     '  echo "protected gate rejected: ' + name + '" >&2; exit 19\nfi\n')
        config = json.loads((self.root / 'release/d-validation-config.json').read_text())
        matrix = json.loads((self.root / 'release/d-validation-compatibility-matrix.json').read_text())
        current_schema = int(config['database_schema']['current'])
        self.current_schema = current_schema
        contract = {'database_schema': str(current_schema), **config['schema_contract']}
        self.put('internal/hostctl/development_schema_contract.json', json.dumps(contract))
        self.put('frontend/package.json', json.dumps({'engines': {'node': '22.23.1', 'npm': '10.9.8'}}))
        for i in range(current_schema): self.put(f'migrations/{i+1:06}_fixture.up.sql', '-- fixture\n')
        for rel in ['schemas/fixture.json', 'fixtures/fixture.json', 'internal/sourcefacts/contract/resource-access.schema.json']:
            self.put(rel, '{}')
        for language in ['en-US', 'zh-CN']:
            for name in ['index.html', 'manual.css', 'manual-icon.svg']:
                self.put('internal/webui/manual/' + language + '/' + name, 'fixture')
        for name in ['LICENSE', 'NOTICE', 'THIRD_PARTY_NOTICES.md', 'licenses/manifest.json', 'licenses/fixture/LICENSE']:
            self.put(name, 'public license fixture')
        self.put('scripts/collect-third-party-notices.py',
                 'import os, sys\n'
                 'with open(os.environ["CALL_LOG"], "a") as log: log.write("license-check\\n")\n'
                 'sys.exit(19 if os.environ.get("REJECT_NOTICES") else 0)\n')
        # Copy public examples only; no real configuration or credentials.
        script = (self.root / 'scripts/build-signed-release.sh').read_text()
        for line in script.splitlines():
            if line.lstrip().startswith('cp deploy/'):
                rel = line.split()[1]
                self.put(rel, 'public fixture')
        self.bin = Path(self.temp.name) / 'bin'; self.bin.mkdir()
        for name in ['git', 'go', 'node', 'npm']:
            p = self.bin / name; p.write_text(STUB); p.chmod(0o755)
        self.log = Path(self.temp.name) / 'calls.jsonl'
        self.out = Path(self.temp.name) / 'prepared'
        self.env = {'PATH': str(self.bin) + os.pathsep + os.environ['PATH'], 'CALL_LOG': str(self.log),
                    'XINSIGHT_RELEASE_SIGNING_KEY': 'must-not-be-read'}

    def put(self, rel, value):
        p = self.root / rel; p.parent.mkdir(parents=True, exist_ok=True); p.write_text(value)

    def run_entry(self, *args):
        return subprocess.run(['bash', str(self.root / 'scripts/build-signed-release.sh'), str(self.out), *args],
                              env=self.env, capture_output=True, text=True, timeout=15)

    def snapshot(self):
        return {str(p.relative_to(self.root)): hashlib.sha256(p.read_bytes()).hexdigest()
                for p in self.root.rglob('*') if p.is_file()}

    def test_dry_run_is_read_only_and_does_not_build(self):
        before = self.snapshot()
        result = self.run_entry('--profile=development-d', '--dry-run')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.snapshot(), before)
        self.assertFalse(self.out.exists())
        self.assertIn('generated_assets_verified=false payload_built=false signed=false', result.stdout)
        self.assertNotIn('"build"', self.log.read_text())
        self.assertIn('gate-reference', self.log.read_text())
        self.assertIn('gate-litellm', self.log.read_text())

    def test_each_protected_gate_blocks_all_build_modes(self):
        for name in ('reference', 'litellm'):
            for mode in (('--dry-run',), ('--prepare-only',), ()):
                with self.subTest(gate=name, mode=mode):
                    self.out = Path(self.temp.name) / (name + '-' + (mode[0] if mode else 'signed'))
                    self.env['REJECT_GATE'] = name
                    self.env['XINSIGHT_RELEASE_SIGNING_KEY'] = '/must-not-be-read'
                    self.log.write_text('')
                    result = self.run_entry('--profile=development-d', *mode)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn('protected gate rejected: ' + name, result.stderr)
                    self.assertNotIn('"build"', self.log.read_text())
                    self.assertNotIn('scripts/build-manual.mjs', self.log.read_text())
                    self.assertNotIn('"ci"', self.log.read_text())
                    self.assertFalse(self.out.exists())

    def test_notice_failure_prevents_payload(self):
        self.env['REJECT_NOTICES'] = '1'
        result = self.run_entry('--profile=development-d', '--prepare-only')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('license-check', self.log.read_text())
        self.assertFalse(self.out.exists())

    def test_prepare_stops_before_signing_and_uses_development_payload(self):
        result = self.run_entry('--profile=development-d', '--prepare-only')
        self.assertEqual(result.returncode, 0, result.stderr)
        prepared = json.loads((self.out / 'prepare-result.json').read_text())
        self.assertFalse(prepared['signed']); self.assertFalse(prepared['installable'])
        self.assertEqual(prepared['build_toolchain'], {'go': 'go1.25.12', 'node': 'v22.23.1', 'npm': '10.9.8'})
        self.assertEqual(prepared['product_version'], json.loads((self.root / 'release/d-validation-config.json').read_text())['product_version'])
        self.assertEqual(len(list((self.out / 'payload/bin').iterdir())), 4)
        for rel in ['release/schema-contract.json', 'schemas/internal/resource-access.schema.json',
                    'deploy/source-budgets-offline.authorization.json.example', 'LICENSE', 'NOTICE',
                    'THIRD_PARTY_NOTICES.md', 'licenses/fixture/LICENSE', 'licenses/manifest.json',
                    'manual/en-US/index.html', 'manual/zh-CN/index.html']:
            self.assertTrue((self.out / 'payload' / rel).is_file(), rel)
        for entry in prepared['files']:
            self.assertEqual(entry['sha256'], hashlib.sha256((self.out / entry['path']).read_bytes()).hexdigest())
        self.assertEqual((self.out / 'payload/release/compatibility-matrix.json').read_bytes(),
                         (self.root / 'release/d-validation-compatibility-matrix.json').read_bytes())
        self.assertFalse(list(self.out.glob('*.tar.gz')))
        self.assertFalse((self.out / 'build-result.json').exists())
        self.assertNotIn('"pack"', self.log.read_text())
        self.assertNotIn('"ci"', self.log.read_text())

    def test_default_remains_rc13(self):
        for i in range(24, self.current_schema + 1): (self.root / f'migrations/{i:06}_fixture.up.sql').unlink()
        for rel in ('release/release-config.json', 'release/compatibility-matrix.json', 'release/release-notes.md', 'internal/hostctl/clean_baseline_schema_contract.json'):
            self.put(rel, (SOURCE / rel).read_text())
        before = self.snapshot()
        result = self.run_entry('--dry-run')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('profile=rc13 product_version=0.3.0-rc.13', result.stdout)
        self.assertEqual(self.snapshot(), before)
        self.assertFalse(self.out.exists())
        self.assertNotIn('"build"', self.log.read_text())
        path = self.root / 'release/compatibility-matrix.json'
        matrix = json.loads(path.read_text())
        matrix['supported_legacy_sources'][0]['artifact_sha256'] = 'sha256:' + '0' * 64
        path.write_text(json.dumps(matrix))
        result = self.run_entry('--dry-run')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('schema contract or migration count differs', result.stderr)
        self.assertFalse(self.out.exists())
        self.put('release/compatibility-matrix.json', (SOURCE / 'release/compatibility-matrix.json').read_text())
        result = self.run_entry('--prepare-only')
        self.assertEqual(result.returncode, 0, result.stderr)
        prepared = json.loads((self.out / 'prepare-result.json').read_text())
        self.assertEqual(prepared['profile'], 'rc13')
        self.assertEqual(prepared['product_version'], '0.3.0-rc.13')
        self.assertEqual(prepared['build_toolchain']['go'], 'go1.25.12')
        self.assertFalse(prepared['signed']); self.assertFalse(prepared['installable'])
        self.assertEqual((self.out / 'payload/release/compatibility-matrix.json').read_bytes(),
                         (SOURCE / 'release/compatibility-matrix.json').read_bytes())
        self.assertEqual((self.out / 'payload/release/schema-contract.json').read_bytes(),
                         (SOURCE / 'internal/hostctl/clean_baseline_schema_contract.json').read_bytes())

    def test_signing_mode_still_requires_key(self):
        result = self.run_entry('--profile=development-d')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('SIGNING_KEY must be an absolute path', result.stderr)
        self.assertFalse(self.out.exists())

    def test_preflight_rejections_do_not_build(self):
        for setting, value, reason in [('DIRTY', '1', 'clean working tree'),
                                       ('GO_VERSION', 'go0.0.0', 'Go toolchain mismatch'),
                                       ('GO_VERSION', 'go1.25.11', 'Go toolchain mismatch'),
                                       ('NODE_VERSION', 'v0.0.0', 'Node toolchain mismatch')]:
            with self.subTest(setting=setting):
                self.env[setting] = value
                result = self.run_entry('--profile=development-d', '--prepare-only')
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(reason, result.stderr)
                self.assertFalse(self.out.exists())
                del self.env[setting]

    def test_generated_drift_prevents_payload(self):
        self.env['GENERATED_DRIFT'] = '1'
        result = self.run_entry('--profile=development-d', '--prepare-only')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('generated manual or frontend assets differ', result.stderr)
        self.assertFalse(self.out.exists())

    def test_mismatched_contract_fails_before_build(self):
        p = self.root / 'internal/hostctl/development_schema_contract.json'
        d = json.loads(p.read_text()); d['database_schema'] = '38'; p.write_text(json.dumps(d))
        result = self.run_entry('--profile=development-d', '--dry-run')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('schema contract or migration count differs', result.stderr)
        self.assertFalse(self.out.exists())

    def test_existing_output_and_unknown_options_fail(self):
        result = self.run_entry('--profile=unknown', '--dry-run')
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.out.exists())
        self.out.mkdir(); (self.out / 'keep').write_text('preserve')
        result = self.run_entry('--profile=development-d', '--prepare-only')
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual((self.out / 'keep').read_text(), 'preserve')

    def test_current_legacy_builder_pins_and_verifies_manifest(self):
        self.env['GO_VERSION'] = 'go1.25.11'
        command = ['bash', str(self.root / 'scripts/build-release.sh'), str(self.out)]
        result = subprocess.run(command, env=self.env, capture_output=True, text=True, timeout=15)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('Go toolchain mismatch', result.stderr)
        self.assertFalse(self.out.exists())
        self.assertNotIn('"build"', self.log.read_text())
        del self.env['GO_VERSION']
        result = subprocess.run(command, env=self.env, capture_output=True, text=True, timeout=15)
        self.assertEqual(result.returncode, 0, result.stderr)
        artifact = next(self.out.glob('*.tar.gz'))
        verify = ['bash', str(self.root / 'scripts/verify-release.sh'), str(artifact)]
        env = {**self.env, 'RUNNER_TEMP': self.temp.name, 'XINSIGHT_EXPECT_COMMIT': '1' * 40,
               'XINSIGHT_EXPECT_GO_VERSION': 'go1.25.12'}
        result = subprocess.run(verify, env=env, capture_output=True, text=True, timeout=15)
        self.assertEqual(result.returncode, 0, result.stderr)
        for key, value, reason in (('XINSIGHT_EXPECT_GO_VERSION', 'go1.25.11', 'manifest Go version'),
                                    ('XINSIGHT_EXPECT_COMMIT', '0' * 40, 'manifest commit')):
            with self.subTest(key=key):
                result = subprocess.run(verify, env={**env, key: value}, capture_output=True, text=True, timeout=15)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(reason, result.stderr)


class ProtectedRuntimeTests(unittest.TestCase):
    def test_real_gates_reject_stale_baselines_without_refreshing_them(self):
        import re
        with tempfile.TemporaryDirectory(prefix='xi-protected-runtime-test-') as directory:
            root = Path(directory)
            # Read-only source inputs; only the isolated gate copies are mutated.
            for entry in SOURCE.iterdir():
                if entry.name not in ('.git', 'scripts'):
                    (root / entry.name).symlink_to(entry, target_is_directory=entry.is_dir())
            (root / 'scripts').mkdir()
            for entry in (SOURCE / 'scripts').iterdir():
                (root / 'scripts' / entry.name).symlink_to(entry, target_is_directory=entry.is_dir())
            for name, pattern, replacement, reason in (
                ('reference', r'(?m)^[0-9a-f]{64} cmd/xinsightctl/main.go$',
                 '0' * 64 + ' cmd/xinsightctl/main.go', 'protected cmd/xinsightctl/main.go changed'),
                ('litellm', r'(?m)^if \[\[ "\$\{migration_digest\}" != "[0-9a-f]{64}" \]\]; then$',
                 'if [[ "${migration_digest}" != "' + '0' * 64 + '" ]]; then', 'migration tree changed'),
            ):
                with self.subTest(gate=name):
                    path = root / 'scripts' / f'verify-phase2c-{name}-runtime.sh'
                    original = path.read_text()
                    changed, count = re.subn(pattern, replacement, original)
                    self.assertEqual(count, 1)
                    path.unlink()  # Only our newly created symlink, never its source.
                    path.write_text(changed)
                    result = subprocess.run(['bash', str(root / 'scripts/verify-protected-runtime.sh')],
                                            capture_output=True, text=True, timeout=30)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn(reason, result.stderr)
                    self.assertEqual(path.read_text(), changed)
                    path.write_text(original)


class ToolchainPinTests(unittest.TestCase):
    def check_workflow(self, text, historical=False):
        import re
        expected = '1.25.12'
        setup = 'Set up exact historical Phase 1 Go'
        reproduction = 'Reproduce and verify exact Phase 1 legacy artifact'
        frozen = 'Rebuild and verify frozen rc13 signed candidate'
        restore = 'Restore current Go after historical reproduction'
        reader = 'Verify current reader against exact Phase 1 legacy artifact'
        chunks = re.split(r'^      - name: ', text, flags=re.M)[1:]
        steps = [(chunk.split('\n', 1)[0], chunk) for chunk in chunks]
        names = [name for name, _ in steps]
        # Exempt only the two bound steps, never arbitrary old pins elsewhere.
        for name, block in steps:
            pins = re.findall(r"^\s+go-version:\s*['\"]?([^'\"\s]+)", block, re.M)
            overrides = re.findall(r'^\s+XINSIGHT_EXPECT_GO_VERSION:\s*(\S+)', block, re.M)
            self.assertTrue(all(pin == ('1.25.11' if historical and name == setup else expected) for pin in pins), name)
            self.assertTrue(all(pin == ('go1.25.11' if historical and name == reproduction else 'go' + expected) for pin in overrides), name)
        all_pins = re.findall(r"^\s+go-version:\s*['\"]?([^'\"\s]+)", text, re.M)
        self.assertTrue(all_pins)
        all_overrides = re.findall(r'^\s+XINSIGHT_EXPECT_GO_VERSION:\s*(\S+)', text, re.M)
        # Also reject pins outside the named step blocks (e.g. job-wide overrides).
        step_pin_count = sum(len(re.findall(r'^\s+go-version:', block, re.M)) for _, block in steps)
        step_override_count = sum(len(re.findall(r'^\s+XINSIGHT_EXPECT_GO_VERSION:', block, re.M)) for _, block in steps)
        self.assertEqual(len(all_pins), step_pin_count)
        self.assertEqual(len(all_overrides), step_override_count)
        if historical:
            for name in (setup, reproduction, frozen, restore, reader): self.assertEqual(names.count(name), 1, name)
            i = names.index(reproduction)
            self.assertEqual(names[i-1:i+4], [setup, reproduction, frozen, restore, reader])
            blocks = dict(steps)
            for name, version in ((setup, '1.25.11'), (restore, expected)):
                self.assertIn('uses: actions/setup-go@v6', blocks[name])
                self.assertIn("go-version: '" + version + "'", blocks[name])
            self.assertEqual(all_pins.count('1.25.11'), 1)
            self.assertEqual(all_overrides.count('go1.25.11'), 1)
            block = blocks[reproduction]
            self.assertIn('XINSIGHT_EXPECT_COMMIT: b9e5811cc53a5cb06614956e66a1c54756617f0a', block)
            self.assertIn('XINSIGHT_EXPECT_GO_VERSION: go1.25.11', block)
            self.assertIn('git worktree add --detach "${RUNNER_TEMP}/phase1-source" "$XINSIGHT_EXPECT_COMMIT"', block)
            self.assertIn('"${RUNNER_TEMP}/phase1-source/scripts/build-release.sh" "${RUNNER_TEMP}/phase1-dist"', block)
            self.assertIn('= "sha256:3e3794338d690fe5b12de46600e17d455baeceab0b79300af260baf78d0346b1"', block)
            self.assertIn('"${RUNNER_TEMP}/phase1-source/scripts/verify-release.sh" "$legacy_artifact"', block)
            self.assertNotIn('go test', block)
            self.assertIn('xinsight-release-linux-amd64-b9e5811cc53a5cb06614956e66a1c54756617f0a.tar.gz', blocks[reader])
            self.assertIn('XINSIGHT_LEGACY_TEST_ARTIFACT="$legacy_artifact" go test ./internal/legacyrelease', blocks[reader])
            self.assertIn('XINSIGHT_RC13_COMMIT: 8bcea5bebf6f0f0796391dfe8c62fdf7a43519eb', blocks[frozen])
            self.assertIn('"$rc13_source/scripts/build-signed-release.sh"', blocks[frozen])
            self.assertIn('"$rc13_source/scripts/verify-signed-release.sh"', blocks[frozen])
        else:
            self.assertEqual(set(all_pins), {expected})

    def test_current_entry_and_workflow_pins_agree(self):
        import re
        for script in ('build-release.sh', 'build-signed-release.sh', 'verify-signed-release.sh'):
            text = (SOURCE / 'scripts' / script).read_text()
            pins = re.findall(r'XINSIGHT_EXPECT_GO_VERSION:-go([0-9.]+)', text)
            self.assertEqual(pins, ['1.25.12'], script)
        for workflow in ('ci.yml', 'owner-supervised-trial-release.yml', 'foigent-protocol-conformance.yml'):
            self.check_workflow((SOURCE / '.github/workflows' / workflow).read_text(), workflow == 'ci.yml')
        historical_identity = {
            'release_id': 'legacy-b9e5811-phase1-final',
            'commit': 'b9e5811cc53a5cb06614956e66a1c54756617f0a',
            'artifact_sha256': 'sha256:3e3794338d690fe5b12de46600e17d455baeceab0b79300af260baf78d0346b1',
            'manifest_sha256': 'sha256:aaa53c2af46f59c328ff3271fa518a20bb0551fd7bdc02a0463ff925a43a6bc1',
        }
        for rel in ('release/release-config.json', 'release/compatibility-matrix.json'):
            sources = json.loads((SOURCE / rel).read_text())['supported_legacy_sources']
            self.assertEqual(len(sources), 1, rel)
            self.assertEqual({key: sources[0][key] for key in historical_identity}, historical_identity, rel)
        package = json.loads((SOURCE / 'frontend/package.json').read_text())
        lock = json.loads((SOURCE / 'frontend/package-lock.json').read_text())
        self.assertEqual(package['engines'], {'node': '22.23.1', 'npm': '10.9.8'})
        self.assertEqual(lock['packages']['']['engines'], package['engines'])
        self.assertEqual(package['packageManager'], 'npm@10.9.8')

    def test_historical_exception_rejects_drift(self):
        text = (SOURCE / '.github/workflows/ci.yml').read_text()
        mutations = [
            text.replace('XINSIGHT_EXPECT_GO_VERSION: go1.25.11', 'XINSIGHT_EXPECT_GO_VERSION: go1.25.12'),
            text.replace("go-version: '1.25.11'", "go-version: '1.25.12'"),
            text.replace('b9e5811cc53a5cb06614956e66a1c54756617f0a', '0' * 40),
            text.replace('3e3794338d690fe5b12de46600e17d455baeceab0b79300af260baf78d0346b1', '0' * 64),
            text.replace('Restore current Go after historical reproduction', 'missing restore'),
            text.replace("go-version: '1.25.12'", "go-version: '1.25.11'", 1),
            text.replace('      - name: Reproduce and verify exact Phase 1 legacy artifact', '      - name: Unrelated intervening step\n        run: true\n\n      - name: Reproduce and verify exact Phase 1 legacy artifact'),
        ]
        for i, candidate in enumerate(mutations):
            with self.subTest(mutation=i), self.assertRaises(AssertionError):
                self.check_workflow(candidate, historical=True)


if __name__ == '__main__':
    unittest.main()
