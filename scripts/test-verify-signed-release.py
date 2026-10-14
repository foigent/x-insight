#!/usr/bin/env python3
"""Offline verifier-entry fixtures: execute every shell/Python check, stub Linux tools.

No signing key, real signature, credential, service or database is acquired here.
Cryptographic integrity remains covered by internal/releasebundle's Go fixtures.
"""
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import unittest

SOURCE = Path(__file__).resolve().parents[1]
COMMIT = '1' * 40
STUB = r'''#!/usr/bin/env python3
import hashlib, json, os, pathlib, shutil, sys
name = pathlib.Path(sys.argv[0]).name
args = sys.argv[1:]
f = pathlib.Path(os.environ['FIXTURE'])
with (f / 'calls.jsonl').open('a') as log:
    log.write(json.dumps([name, *args]) + '\n')
def read(p): return json.loads(p.read_text())
def ref(raw): return 'sha256:' + hashlib.sha256(raw).hexdigest()
def identity():
    value = read(f / 'identity.json')
    value.update(read(f / 'runtime-overrides.json').get(name, {}))
    return value
if name == 'git':
    if args != ['rev-parse', 'HEAD']: sys.exit('unexpected git call')
    print('1' * 40)
elif name == 'uname':
    print('Linux' if args == ['-s'] else 'x86_64')
elif name == 'go':
    sys.exit('unexpected compiler/network path')
elif name == 'release-tool':
    if args == ['version', '--json']:
        print(json.dumps(identity())); sys.exit(0)
    verb = args[0]
    if os.environ.get('REJECT_STAGE') == ' '.join(args[:2] if verb == 'campaign-authority' else args[:1]):
        sys.exit('fixture release verifier rejected stage')
    def flag(key): return args[args.index(key) + 1]
    expected = read(f / 'manifest.json')
    if flag('--expected-commit') != expected['commit'] or flag('--expected-version') != expected['product_version']:
        sys.exit('fixture exact identity rejected')
    artifact = pathlib.Path(flag('--artifact'))
    seal = read(f / 'seal.json')
    if ref(artifact.read_bytes()) != seal['artifact']:
        sys.exit('fixture envelope integrity rejected')
    for rel, digest in seal['payload'].items():
        if ref((f / 'payload' / rel).read_bytes()) != digest:
            sys.exit('fixture payload integrity rejected')
    result = read(f / 'verify-result.json')
    if verb == 'extract':
        shutil.copytree(f / 'payload', flag('--destination'))
        if os.environ.get('EXTRACT_DRIFT'): result['manifest_sha256'] = 'sha256:' + '0' * 64
    elif verb == 'campaign-authority':
        if args[1] == 'derive':
            target = pathlib.Path(flag('--output-directory')); target.mkdir()
            for filename in ('xinsight.release-identity.v1.json', 'xinsight.signed-campaign-package-consumer-verifier.v1.json'):
                (target / filename).write_text('{}')
        elif args[1] != 'verify': sys.exit('unexpected campaign verb')
    elif verb != 'verify': sys.exit('unexpected release verb')
    print(json.dumps(result))
else:
    if args == ['version', '--json']: print(json.dumps(identity()))
    elif args == ['webui-manifest', '--json']: print((f / 'runtime-webui.json').read_text(), end='')
    elif args == ['schema-contract', 'expected', '--raw']: print((f / 'runtime-contract.json').read_text(), end='')
    elif args == ['feed-credential', 'status']:
        print(json.dumps({'schema_version': 'xinsight.feed-credential-status.v1', 'status': 'absent', 'active_file': {'present': False}, 'staged_file': {'present': False}}))
    else: sys.exit('unexpected runtime call')
'''


def sha(raw):
    return 'sha256:' + hashlib.sha256(raw).hexdigest()


class VerifyEntryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='xi-verify-entry-test-')
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.root = self.base / 'source'
        self.fixture = self.base / 'fixture'
        self.fixture.mkdir()
        self.payload = self.fixture / 'payload'
        self.payload.mkdir()
        for rel in ('scripts/verify-signed-release.sh', 'scripts/validate_managed_environment.py',
                    'frontend/package.json', 'release/release-config.json', 'release/compatibility-matrix.json',
                    'release/d-validation-config.json', 'release/d-validation-compatibility-matrix.json',
                    'internal/hostctl/clean_baseline_schema_contract.json', 'internal/hostctl/development_schema_contract.json'):
            p = self.root / rel
            p.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(SOURCE / rel, p)
        shutil.copytree(SOURCE / 'internal/webui/dist', self.root / 'internal/webui/dist')
        for name in ('LICENSE', 'NOTICE', 'THIRD_PARTY_NOTICES.md'):
            shutil.copy2(SOURCE / name, self.root / name)
            shutil.copy2(SOURCE / name, self.payload / name)
        shutil.copytree(SOURCE / 'licenses', self.root / 'licenses')
        shutil.copytree(SOURCE / 'licenses', self.payload / 'licenses')
        shutil.copytree(SOURCE / 'deploy', self.payload / 'deploy')
        for rel in ('schemas/access/local-accounts-v1.schema.json', 'schemas/integration/feed-credential-registration-v1.schema.json'):
            p = self.payload / rel
            p.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(SOURCE / rel, p)
        self.bin = self.base / 'bin'
        self.bin.mkdir()
        for name in ('git', 'uname', 'go', 'release-tool'):
            p = self.bin / name
            p.write_text(STUB); p.chmod(0o755)
        for name in ('xinsight-server', 'xinsight-importer', 'xinsightctl', 'xinsight-launcher'):
            p = self.payload / 'bin' / name
            p.parent.mkdir(exist_ok=True)
            p.write_text(STUB); p.chmod(0o755)
        self.artifact = self.fixture / 'unsigned-envelope-fixture.tar.gz'
        self.trust = self.fixture / 'public-trust-fixture.json'
        self.trust.write_text('{}')
        self.out = self.base / 'verified-payload'
        self.env = {'PATH': str(self.bin) + os.pathsep + os.environ['PATH'], 'FIXTURE': str(self.fixture),
                    'XINSIGHT_RELEASE_TOOL': str(self.bin / 'release-tool'), 'PYTHONDONTWRITEBYTECODE': '1'}
        self.configure('development-d')

    def put(self, path, value):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(value))

    def configure(self, profile):
        d = profile == 'development-d'
        config = json.loads((self.root / ('release/d-validation-config.json' if d else 'release/release-config.json')).read_text())
        matrix = self.root / ('release/d-validation-compatibility-matrix.json' if d else 'release/compatibility-matrix.json')
        contract = self.root / ('internal/hostctl/development_schema_contract.json' if d else 'internal/hostctl/clean_baseline_schema_contract.json')
        self.manifest = {k: v for k, v in config.items() if k not in ('schema_version', 'trust_reference')}
        # Match releasebundle.Pack's nil empty-slice encoding exactly.
        for field in ('supported_upgrade_from_versions', 'supported_legacy_sources'):
            if not self.manifest[field]: self.manifest[field] = None
        self.manifest.update(commit=COMMIT, build_toolchain={'go_version': 'go1.25.12', 'node_version': 'v22.23.1', 'npm_version': '10.9.8'})
        identity = {'product_name': 'x-insight', 'product_version': config['product_version'], 'commit': COMMIT,
                    'database_schema_min': config['database_schema']['min'], 'database_schema_max': config['database_schema']['max'],
                    'config_schema_version': config['config_schema']['current'], 'api_version': config['api_versions'][0],
                    'access_api_version': config['api_versions'][1], 'manual_version': config['manual']['version'], 'go_version': 'go1.25.12'}
        self.put(self.fixture / 'identity.json', identity)
        self.put(self.fixture / 'runtime-overrides.json', {})
        (self.payload / 'release').mkdir(exist_ok=True)
        shutil.copy2(matrix, self.payload / 'release/compatibility-matrix.json')
        shutil.copy2(contract, self.payload / 'release/schema-contract.json')
        shutil.copy2(contract, self.fixture / 'runtime-contract.json')
        shutil.copy2(self.root / 'internal/webui/dist/asset-manifest.json', self.fixture / 'runtime-webui.json')
        for language in (['en-US', 'zh-CN'] if d else ['zh-CN']):
            manual = self.payload / 'manual' / language
            manual.mkdir(parents=True, exist_ok=True)
            (manual / 'index.html').write_text(f'<meta name="xinsight-product-version" content="{config["product_version"]}"><meta name="xinsight-manual-version" content="{config["manual"]["version"]}"><a href="#ok">OK</a><p id="ok">Fixture</p>')
            (manual / 'manual.css').write_text('body { color: black; }')
        self.seal()

    def seal(self):
        self.put(self.fixture / 'manifest.json', self.manifest)
        raw = (self.fixture / 'manifest.json').read_bytes()
        with tarfile.open(self.artifact, 'w:gz') as archive:
            info = tarfile.TarInfo('fixture/release-manifest.json'); info.size = len(raw)
            archive.addfile(info, io.BytesIO(raw))
        self.put(self.fixture / 'verify-result.json', {'release_name': 'fixture', 'manifest_sha256': sha(raw),
                 'bundle_sha256': sha(self.artifact.read_bytes()), 'product_version': self.manifest['product_version'], 'commit': COMMIT})
        self.put(self.fixture / 'seal.json', {'artifact': sha(self.artifact.read_bytes()),
                 'payload': {str(p.relative_to(self.payload)): sha(p.read_bytes()) for p in self.payload.rglob('*') if p.is_file()}})

    def run_entry(self, *args):
        return subprocess.run(['bash', str(self.root / 'scripts/verify-signed-release.sh'), str(self.artifact), str(self.trust), str(self.out), *args],
                              env=self.env, capture_output=True, text=True, timeout=20)

    def assert_rejected(self, result, reason):
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn(reason, result.stderr)
        self.assertNotIn('verified_payload=', result.stdout)

    def test_development_d_runs_all_checks(self):
        result = self.run_entry('--profile=development-d')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('go_version=go1.25.12', result.stdout)
        calls = [json.loads(line) for line in (self.fixture / 'calls.jsonl').read_text().splitlines()]
        for prefix in (['release-tool', 'verify'], ['release-tool', 'extract'], ['release-tool', 'campaign-authority', 'derive'], ['release-tool', 'campaign-authority', 'verify'], ['release-tool', 'version', '--json'], ['xinsightctl', 'feed-credential', 'status']):
            self.assertTrue(any(call[:len(prefix)] == prefix for call in calls), prefix)
        for name in ('xinsight-server', 'xinsight-importer', 'xinsightctl', 'xinsight-launcher'):
            self.assertIn([name, 'version', '--json'], calls)

    def test_default_remains_rc13(self):
        self.configure('rc13')
        result = self.run_entry()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('product_version=0.3.0-rc.13', result.stdout)
        self.out = self.base / 'legacy-identity-drift'
        path = self.root / 'release/compatibility-matrix.json'
        matrix = json.loads(path.read_text())
        matrix['supported_legacy_sources'][0]['artifact_sha256'] = 'sha256:' + '0' * 64
        self.put(path, matrix)
        self.assert_rejected(self.run_entry(), 'selected profile config, matrix or schema contract differs')
        self.assertFalse(self.out.exists())


    def test_wrong_profile_and_unknown_options_reject_before_extraction(self):
        for args in ([], ['--profile=rc13'], ['--profile=unknown'], ['unexpected-fourth-path']):
            with self.subTest(args=args):
                result = self.run_entry(*args)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(self.out.exists())

    def test_signed_toolchain_mismatch_rejects_before_extraction(self):
        for field, value in (('go_version', 'go1.25.11'), ('node_version', 'v23.11.0'), ('npm_version', '10.9.2')):
            with self.subTest(field=field):
                original = self.manifest['build_toolchain'][field]
                self.manifest['build_toolchain'][field] = value; self.seal()
                self.assert_rejected(self.run_entry('--profile=development-d'), 'signed manifest build toolchain differs')
                self.assertFalse(self.out.exists())
                self.manifest['build_toolchain'][field] = original

    def test_runtime_go_mismatch_rejects_each_binary(self):
        # Each run gets its own exact, previously nonexistent extraction directory.
        for name in ('xinsight-server', 'xinsight-importer', 'xinsightctl', 'xinsight-launcher', 'release-tool'):
            with self.subTest(name=name):
                self.out = self.base / ('runtime-' + name)
                self.put(self.fixture / 'runtime-overrides.json', {name: {'go_version': 'go1.25.11'}})
                self.assert_rejected(self.run_entry('--profile=development-d'), 'go_version=')

    def test_manifest_metadata_mismatch_rejects_before_extraction(self):
        self.manifest['supported_upgrade_from_versions'] = []
        self.seal()
        self.assert_rejected(self.run_entry('--profile=development-d'), 'signed manifest differs from selected profile')
        self.assertFalse(self.out.exists())

    def test_profile_matrix_and_contract_drift_reject_before_extraction(self):
        for rel, field, value in (('release/d-validation-compatibility-matrix.json', 'supported_upgrade_from_versions', []),
                                  ('internal/hostctl/development_schema_contract.json', 'database_schema', 38)):
            with self.subTest(rel=rel):
                p = self.root / rel; before = p.read_bytes()
                obj = json.loads(before); obj[field] = value; self.put(p, obj)
                self.assert_rejected(self.run_entry('--profile=development-d'), 'selected profile')
                self.assertFalse(self.out.exists()); p.write_bytes(before)

    def test_verified_manifest_digest_binding_rejects(self):
        result_path = self.fixture / 'verify-result.json'
        obj = json.loads(result_path.read_text()); obj['manifest_sha256'] = 'sha256:' + '0' * 64
        self.put(result_path, obj)
        self.assert_rejected(self.run_entry('--profile=development-d'), 'manifest differs from signature-verified bytes')
        self.assertFalse(self.out.exists())

    def test_corrupt_envelope_or_payload_stops_at_verification(self):
        for target in (self.artifact, self.payload / 'bin/xinsight-importer'):
            with self.subTest(target=target):
                self.seal(); target.write_bytes(target.read_bytes() + b'corrupt')
                self.assert_rejected(self.run_entry('--profile=development-d'), 'integrity rejected')
                self.assertFalse(self.out.exists())

    def test_extract_identity_drift_rejects_before_runtime(self):
        self.env['EXTRACT_DRIFT'] = '1'
        self.assert_rejected(self.run_entry('--profile=development-d'), 'extracted release differs')
        self.assertNotIn('"version"', (self.fixture / 'calls.jsonl').read_text())

    def test_campaign_rejection_is_never_skipped(self):
        for stage in ('campaign-authority derive', 'campaign-authority verify'):
            with self.subTest(stage=stage):
                self.out = self.base / stage.replace(' ', '-')
                self.env['REJECT_STAGE'] = stage
                self.assert_rejected(self.run_entry('--profile=development-d'), 'rejected stage')

    def test_payload_checks_remain_active_for_d(self):
        mutations = (
            ('release/compatibility-matrix.json', lambda p: p.write_text('{}'), 'compatibility matrix differs'),
            ('release/schema-contract.json', lambda p: p.write_text('{}'), 'schema contracts differ'),
            ('manual/zh-CN/index.html', lambda p: p.write_text('<p>wrong manual</p>'), 'manual product version mismatch'),
            ('deploy/xinsight.service', lambda p: p.write_text(p.read_text().replace('User=xinsight', 'User=wrong')), 'managed server unit is missing'),
        )
        for i, (rel, mutate, reason) in enumerate(mutations):
            with self.subTest(rel=rel):
                self.out = self.base / f'payload-{i}'
                p = self.payload / rel; original = p.read_bytes(); mutate(p); self.seal()
                self.assert_rejected(self.run_entry('--profile=development-d'), reason)
                p.write_bytes(original)

    def test_runtime_webui_drift_rejects(self):
        (self.fixture / 'runtime-webui.json').write_text('{}')
        self.assert_rejected(self.run_entry('--profile=development-d'), 'runtime web UI manifest differs')

    def test_existing_destination_is_preserved(self):
        self.out.mkdir(); keep = self.out / 'keep'; keep.write_text('preserve')
        self.assertNotEqual(self.run_entry('--profile=development-d').returncode, 0)
        self.assertEqual(keep.read_text(), 'preserve')

    def test_historical_signed_toolchain_needs_exact_expectation(self):
        self.configure('rc13')
        self.manifest['build_toolchain']['go_version'] = 'go1.25.11'
        identity = json.loads((self.fixture / 'identity.json').read_text())
        identity['go_version'] = 'go1.25.11'
        self.put(self.fixture / 'identity.json', identity)
        self.seal()
        self.assert_rejected(self.run_entry('--profile=development-d'), 'exact identity rejected')
        self.assertFalse(self.out.exists())
        self.assert_rejected(self.run_entry(), 'signed manifest build toolchain differs')
        self.assertFalse(self.out.exists())
        self.env['XINSIGHT_EXPECT_GO_VERSION'] = 'go1.25.11'
        result = self.run_entry()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('go_version=go1.25.11', result.stdout)

    def test_signed_legacy_identity_cannot_drift(self):
        self.configure('rc13')
        legacy = self.manifest['supported_legacy_sources'][0]
        for field in ('commit', 'artifact_sha256', 'manifest_sha256'):
            with self.subTest(field=field):
                original = legacy[field]
                legacy[field] = '0' * 40 if field == 'commit' else 'sha256:' + '0' * 64
                self.seal()
                self.assert_rejected(self.run_entry(), 'signed manifest differs from selected profile: supported_legacy_sources')
                self.assertFalse(self.out.exists())
                legacy[field] = original


if __name__ == '__main__':
    unittest.main()
