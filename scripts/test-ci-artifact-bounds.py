#!/usr/bin/env python3
"""Exercise archive ceilings, runner staging and the real workflow allowlists."""

import importlib.util
import os
from pathlib import Path
import random
import re
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
import zipfile

ROOT = Path(__file__).resolve().parents[1]
ACTION = ROOT / '.github/actions/upload-bounded-artifact'
# Import the exact action implementation without writing to the source tree.
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location('artifact_admission', ACTION / 'admit.py')
admission = importlib.util.module_from_spec(spec)
spec.loader.exec_module(admission)

EXPECTED = {
    'Upload coverage': 'coverage',
    'Upload explicitly non-publishable CI qualification artifact': 'signed',
    'Upload explicitly non-publishable fresh-install evidence': 'installed',
    'Upload allowlisted FPV9 qualification evidence': 'installed',
    'Upload allowlisted managed FPV11 qualification evidence': 'installed',
    'Upload only sanitized P3 component receipts': 'p3',
    'Upload Execution Evidence Draft conformance report': 'protocol',
}

REQUESTED_DAYS = {
    'pull_request': {'signed': 7, 'coverage': 7, 'installed': 14, 'p3': 14, 'protocol': 14},
    'push': {'signed': 14, 'installed': 30, 'p3': 30, 'protocol': 30},
    'workflow_dispatch': {'protocol': 14},
}


def workflow_uploads():
    result = {}
    for name in ('ci.yml', 'foigent-protocol-conformance.yml'):
        source = (ROOT / '.github/workflows' / name).read_text()
        if re.search(r'uses:\s*actions/upload-artifact@', source):
            raise AssertionError('direct artifact upload bypasses admission')
        for step in re.split(r'^      - name: ', source, flags=re.M)[1:]:
            title, _, body = step.partition('\n')
            body = re.split(r'^  [a-zA-Z0-9_-]+:\s*$', body, maxsplit=1, flags=re.M)[0]
            if 'uses: ./.github/actions/upload-bounded-artifact' not in body:
                continue
            kind = re.search(r'^          kind: (\w+)$', body, re.M)[1]
            match = re.search(r'^          path: (.+)$', body, re.M)
            if match[1] == '|':
                patterns = []
                for line in body[match.end() + 1:].splitlines():
                    if not line.startswith(' ' * 12):
                        break
                    patterns.append(line.strip())
            else:
                patterns = [match[1]]
            missing = 'error' if 'if-no-files-found: error' in body else 'warn'
            if title in result:
                raise AssertionError('duplicate artifact upload')
            result[title] = (kind, patterns, missing)
    return result


class ArtifactBoundsTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='xi-artifact-bounds-test-')
        self.addCleanup(temporary.cleanup)
        self.work = Path(temporary.name).resolve() / 'work'
        self.repo = self.work / 'x-insight/x-insight'
        self.runner = self.work / '_temp'
        self.repo.mkdir(parents=True)
        self.runner.mkdir()

    def stage(self, paths, kind='protocol', missing='error', event='pull_request'):
        return admission.stage(paths, self.repo, self.runner, kind, missing, event)

    def invoke(self, paths, kind, event, output):
        env = dict(os.environ, ARTIFACT_PATHS=paths, ARTIFACT_KIND=kind, ARTIFACT_MISSING='error',
                   RUNNER_TEMP=str(self.runner), GITHUB_WORKSPACE=str(self.repo), GITHUB_OUTPUT=str(output),
                   GITHUB_EVENT_NAME=event, GITHUB_RETENTION_DAYS='1')
        return subprocess.run([sys.executable, str(ACTION / 'admit.py')], env=env,
                              text=True, capture_output=True, check=False)

    def test_actual_workflow_paths_and_member_names(self):
        uploads = workflow_uploads()
        self.assertEqual({title: data[0] for title, data in uploads.items()}, EXPECTED)
        for title, (kind, patterns, missing) in uploads.items():
            with self.subTest(step=title):
                expanded = [pattern.replace('${{ runner.temp }}', str(self.runner)) for pattern in patterns]
                source_files = []
                for pattern in expanded:
                    path = self.repo / pattern.replace('*', 'fixture')
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_bytes(path.name.encode())
                    source_files.append(path)
                snapshot, days = self.stage('\n'.join(expanded), kind, missing)
                self.assertEqual(days, REQUESTED_DAYS['pull_request'][kind])
                members = {file.relative_to(snapshot).as_posix(): file.read_bytes()
                           for file in snapshot.rglob('*') if file.is_file()}
                self.assertCountEqual(members.values(), [path.name.encode() for path in source_files])
                if kind == 'signed':
                    self.assertIn('_temp/xinsight-signed-a/build-result.json', members)
                    self.assertIn('_temp/ci-trusted-release-keys.json', members)
                    self.assertIn('x-insight/x-insight/release/ci-non-publishable.txt', members)
                if kind == 'coverage':
                    self.assertEqual(set(members), {'coverage.out'})
                if kind == 'protocol':
                    self.assertEqual(set(members), {'execution-evidence-draft-conformance.json'})

    def test_missing_optional_paths_preserve_failure_receipts(self):
        report = self.runner / 'failure.json'
        report.write_bytes(b'{"state":"failed"}')
        paths = f'{self.runner}/success.json\n{report}'
        directory, _ = self.stage(paths, 'installed')
        self.assertEqual((directory / 'failure.json').read_bytes(), report.read_bytes())
        self.assertFalse((directory / 'success.json').exists())
        with self.assertRaisesRegex(ValueError, 'matched no files'):
            self.stage(str(self.runner / 'missing.json'))
        self.assertIsNone(self.stage(str(self.runner / 'missing.json'), missing='warn')[0])

    def test_rejects_outside_links_directories_and_unsafe_patterns(self):
        outside = self.work / 'outside.json'
        outside.write_bytes(b'outside')
        (self.runner / 'link.json').symlink_to(outside)
        (self.runner / 'directory.json').mkdir()
        (self.runner / 'linked-parent').symlink_to(self.work, target_is_directory=True)
        for paths in (str(outside), str(self.runner / '../outside.json'), str(self.runner / 'link.json'),
                      str(self.runner / 'linked-parent/outside.json'), str(self.runner / 'directory.json'),
                      str(self.runner / '**/outside.json'), '!coverage.out', ''):
            with self.subTest(paths=paths), self.assertRaises(ValueError):
                self.stage(paths)

    def test_snapshot_has_only_checked_bytes(self):
        report = self.runner / 'report.json'
        report.write_bytes(b'checked bytes')
        (self.runner / '.hidden.json').write_bytes(b'excluded')
        directory, _ = self.stage(f'{self.runner}/*.json')
        report.write_bytes(b'changed original')
        (self.runner / 'late.json').write_bytes(b'late match')
        self.assertEqual([p.name for p in directory.iterdir()], ['report.json'])
        self.assertEqual((directory / 'report.json').read_bytes(), b'checked bytes')
        root, files = admission.allowed_files(str(report), self.repo, self.runner)
        report.write_bytes(b'changed after admission')
        with self.assertRaisesRegex(ValueError, 'changed before staging'):
            admission.snapshot_file(report, directory / 'new.json', files[report])
        copy = admission.snapshot_file

        def copy_with_late_match(path, target, info):
            copy(path, target, info)
            (self.runner / 'during-copy.json').touch()

        with patch.object(admission, 'snapshot_file', side_effect=copy_with_late_match):
            with self.assertRaisesRegex(ValueError, 'allowlist changed while staging'):
                self.stage(f'{self.runner}/*.json')

    def test_protocol_exact_boundary_and_guard_exit(self):
        report = self.runner / 'execution-evidence-draft-conformance.json'
        generator = random.Random(103)
        cap, _ = admission.LIMITS['protocol']
        source = generator.randbytes(cap)
        low, high = 0, len(source)
        while low + 1 < high:
            count = (low + high) // 2
            report.write_bytes(source[:count])
            bound = admission.zip_bound({report: report.stat()}, self.runner)
            if bound <= cap:
                low = count
            else:
                high = count
        report.write_bytes(source[:low])
        snapshot, _ = self.stage(str(report))
        archive = self.work / 'comparison.zip'
        with zipfile.ZipFile(archive, 'w', compression=zipfile.ZIP_DEFLATED, compresslevel=6) as output:
            output.write(snapshot / report.name, report.name)
        self.assertLessEqual(archive.stat().st_size, cap)
        report.write_bytes(source[:low + 1])
        output = self.work / 'github-output'
        before = set(self.runner.iterdir())
        result = self.invoke(str(report), 'protocol', 'pull_request', output)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('exceeds byte ceiling', result.stderr)
        self.assertEqual(set(self.runner.iterdir()), before)
        self.assertFalse(output.exists(), 'failed guard exported an upload path')

    def test_all_classes_reject_one_byte_above_ceiling(self):
        first, padding = self.runner / 'a.bin', self.runner / 'b.bin'
        patterns = f'{self.runner}/*.bin'

        def resize(path, size):
            with path.open('wb') as file:
                file.truncate(size)

        def current_bound():
            return admission.zip_bound({p: p.stat() for p in (first, padding)}, self.runner)

        for kind, (limit, _) in admission.LIMITS.items():
            with self.subTest(kind=kind):
                resize(padding, 0)
                low, high = 0, limit
                while low + 1 < high:
                    middle = (low + high) // 2
                    resize(first, middle)
                    if current_bound() <= limit:
                        low = middle
                    else:
                        high = middle
                resize(first, low)
                resize(padding, limit + 1 - current_bound())
                self.assertEqual(current_bound(), limit + 1)
                output = self.work / f'output-{kind}'
                before = set(self.runner.iterdir())
                result = self.invoke(patterns, kind, 'pull_request', output)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('exceeds byte ceiling', result.stderr)
                self.assertEqual(set(self.runner.iterdir()), before)
                self.assertFalse(output.exists())

    def test_retention_requests_and_event_scope(self):
        source = self.repo / 'fixture.out'
        source.write_bytes(b'fixture')
        for event, requests in REQUESTED_DAYS.items():
            for kind, days in requests.items():
                with self.subTest(kind=kind, event=event):
                    output = self.work / f'output-{event}-{kind}'
                    result = self.invoke(str(source), kind, event, output)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertIn(f'retention-days={days}', output.read_text().splitlines())
        for kind, event in (('coverage', 'push'), ('signed', 'workflow_dispatch'), ('protocol', 'schedule')):
            with self.subTest(kind=kind, event=event):
                output = self.work / f'output-{event}-{kind}'
                result = self.invoke(str(source), kind, event, output)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('unsupported workflow event', result.stderr)
                self.assertFalse(output.exists())

    def test_wiring_and_total_envelope(self):
        action = (ACTION / 'action.yml').read_text()
        self.assertEqual(action.count('uses: actions/upload-artifact@v6'), 1)
        self.assertLess(action.index('run: python3'), action.index('uses: actions/upload-artifact@v6'))
        self.assertIn("steps.admit.outcome == 'success' && steps.admit.outputs.directory != ''", action)
        self.assertIn('path: ${{ steps.admit.outputs.directory }}', action)
        self.assertIn('retention-days: ${{ steps.admit.outputs.retention-days }}', action)
        self.assertNotIn('continue-on-error:', action)
        ci = (ROOT / '.github/workflows/ci.yml').read_text()
        lifecycles = 'new_install, adopt_existing, fpv17_upgrade, fpv20_upgrade, fpv21_upgrade, fpv22_upgrade, fpv23_upgrade, fpv24_upgrade'
        self.assertIn(f'lifecycle: [{lifecycles}]', ci)
        native_lanes = ci.count('database_profile: native')
        self.assertEqual(native_lanes, 2)
        installed_archives = 2 * (len(lifecycles.split(', ')) + native_lanes)
        groups = {'signed': 2, 'coverage': 1, 'installed': installed_archives, 'p3': 2}
        total = sum(admission.LIMITS[kind][0] * count for kind, count in groups.items())
        self.assertEqual(sum(groups.values()), 25)
        self.assertEqual(total, 208175104)
        self.assertEqual(admission.LIMITS['protocol'][0] * 2, 262144)


if __name__ == '__main__':
    unittest.main(verbosity=2)
