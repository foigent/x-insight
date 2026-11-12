#!/usr/bin/env python3
"""Reconstruct this run's CI-signed candidate from the published FPV22 bundle."""

import gzip
import hashlib
import json
import os
from pathlib import Path
import platform
import resource
import subprocess
import sys
import tarfile
import time

MAX_RAW = 64 << 20
MAX_PATCH = 32 << 20
BASIS_COMMIT = 'bd4503a13a46156f8361e56730abef96cc5628bd'
BASIS_VERSION = '0.2.0-dev.fpv22'
BASIS_SHA = 'sha256:c8dcd509eaff17cbe8dcbd4fca40da9f19d2f37501195150c0cb9b01d1478c1d'
TARGET_VERSION = '0.2.0-dev.fpv23'


def require(condition, message):
    if not condition:
        raise ValueError(message)


def identity(path):
    digest = hashlib.sha256()
    with path.open('rb') as source:
        size = os.fstat(source.fileno()).st_size
        for chunk in iter(lambda: source.read(1 << 20), b''):
            digest.update(chunk)
    return {'bytes': size, 'sha256': 'sha256:' + digest.hexdigest()}


def write_json(path, value):
    with path.open('x') as output:
        json.dump(value, output, sort_keys=True)
        output.write('\n')


def copy_bounded(source, destination, limit):
    remaining = limit
    with destination.open('xb') as output:
        while chunk := source.read(min(1 << 20, remaining + 1)):
            require(len(chunk) <= remaining, 'delta input exceeds its byte limit')
            output.write(chunk)
            remaining -= len(chunk)
    require(remaining < limit, 'empty delta input')


def unpack_inputs(bundle, version, raw, sidecars=None):
    limits = {'payload.tar.gz': MAX_RAW}
    if sidecars is not None:
        limits.update({'release-manifest.json': 1 << 20, 'release-manifest.sig': 16 << 10})
    prefix = f'xinsight-v{version}-linux-amd64/'
    seen = set()
    with tarfile.open(bundle, 'r|gz') as archive:
        for member in archive:
            name = member.name.removeprefix(prefix)
            if not member.name.startswith(prefix) or name not in limits:
                continue
            require(member.isfile() and name not in seen and 0 < member.size <= limits[name],
                    'invalid signed envelope member')
            seen.add(name)
            with archive.extractfile(member) as contents:
                if name == 'payload.tar.gz':
                    with gzip.GzipFile(fileobj=contents) as payload:
                        copy_bounded(payload, raw, MAX_RAW)
                else:
                    copy_bounded(contents, sidecars / name, limits[name])
    require(seen == limits.keys(), 'signed envelope member missing')


def verify(tool, artifact, trust, commit, version):
    result = subprocess.check_output([
        tool, 'verify', '--artifact', artifact, '--trust-store', trust,
        '--expected-commit', commit, '--expected-version', version,
    ], timeout=60)
    return json.loads(result)


def qualify(signed_dir, output, runner, target_commit):
    prestate = json.loads((runner / 'account-upgrade-ci-predecessor.json').read_text())
    require((prestate['old_commit'], prestate['old_product'], prestate['old_schema']) ==
            (BASIS_COMMIT, BASIS_VERSION, '47'), 'FPV22 prestate identity mismatch')
    basis = Path(prestate['old_artifact'])
    trust = Path(prestate['combined_trust'])
    require(basis == Path('/opt/xinsight-ci-artifacts/fpv22.tar.gz'), 'FPV22 basis is outside managed fixture staging')
    result = json.loads((signed_dir / 'build-result.json').read_text())
    require((result['commit'], result['product_version']) == (target_commit, TARGET_VERSION), 'candidate run identity mismatch')
    target = signed_dir / f'xinsight-v{TARGET_VERSION}-linux-amd64-{target_commit}.tar.gz'
    tool = signed_dir / 'bootstrap' / f'xinsight-release-linux-amd64-{target_commit}'
    basis_identity = identity(basis)
    target_identity = identity(target)
    require(basis_identity == {'bytes': 22374819, 'sha256': BASIS_SHA}, 'published FPV22 artifact differs')
    require(0 < target_identity['bytes'] <= MAX_RAW and target_identity['sha256'] == result['bundle_sha256'],
            'candidate bundle differs from CI build result')
    verified_basis = verify(tool, basis, trust, BASIS_COMMIT, BASIS_VERSION)
    verified_target = verify(tool, target, trust, target_commit, TARGET_VERSION)
    require(verified_basis['bundle_sha256'] == BASIS_SHA and
            all(verified_target[key] == result[key] for key in ('bundle_sha256', 'manifest_sha256', 'payload_sha256')),
            'verified artifact identity mismatch')

    output.mkdir(mode=0o700)
    basis_raw, target_raw = output / 'basis.tar', output / 'target.tar'
    unpack_inputs(basis, BASIS_VERSION, basis_raw)
    unpack_inputs(target, TARGET_VERSION, target_raw, output)
    manifest, signature = output / 'release-manifest.json', output / 'release-manifest.sig'
    patch, plan = output / 'payload.zst', output / 'delta.json'
    subprocess.run(['zstd', '-3', '--no-progress', f'--patch-from={basis_raw}', target_raw, '-o', patch],
                   check=True, timeout=120)
    require(0 < patch.stat().st_size <= MAX_PATCH, 'delta patch is outside the profile')
    write_json(plan, {
        'schema_version': 'xinsight.release-delta.v1', 'codec': 'zstd-raw-tar-v1',
        'target_os': 'linux', 'target_arch': 'amd64',
        'basis': {'commit': BASIS_COMMIT, 'version': BASIS_VERSION, 'bundle': basis_identity, 'raw_tar': identity(basis_raw)},
        'target': {'commit': target_commit, 'version': TARGET_VERSION, 'bundle': target_identity, 'raw_tar': identity(target_raw)},
        'patch': identity(patch), 'manifest': identity(manifest), 'signature': identity(signature),
    })

    # Run the transported copy of the same CI bootstrap. Its trust comes from
    # the existing CI artifact and the exact raw bytes verified by this job.
    compressed = output / 'bootstrap.gz'
    with compressed.open('xb') as stream:
        subprocess.run(['gzip', '-n', '-9', '-c', tool], stdout=stream, check=True, timeout=60)
    bootstrap = output / 'xinsight-release'
    tool_identity = identity(tool)
    with gzip.open(compressed, 'rb') as stream:
        copy_bounded(stream, bootstrap, tool_identity['bytes'])
    require(identity(bootstrap) == tool_identity, 'transported bootstrap differs from the CI artifact')
    bootstrap.chmod(0o700)
    rebuilt = output / 'reconstructed.tar.gz'
    start = time.monotonic()
    subprocess.run([
        bootstrap, 'materialize-delta', '--plan', plan, '--plan-sha256', identity(plan)['sha256'],
        '--basis', basis, '--patch', patch, '--manifest', manifest, '--signature', signature,
        '--trust-store', trust, '--output', rebuilt,
    ], check=True, timeout=120)
    elapsed = round(time.monotonic() - start, 3)
    subprocess.run(['cmp', target, rebuilt], check=True, timeout=60)
    require(verify(bootstrap, rebuilt, trust, target_commit, TARGET_VERSION) == verified_target,
            'reconstructed artifact did not pass ordinary verification')
    write_json(output / 'qualification-result.json', {
        'schema_version': 'xinsight.ci-fpv22-delta-result.v1', 'classification': 'ci_synthetic_not_target',
        'state': 'passed', 'basis_commit': BASIS_COMMIT, 'basis': basis_identity,
        'target_commit': target_commit, 'reconstructed': target_identity,
        'target_raw_tar_bytes': target_raw.stat().st_size, 'patch': identity(patch),
        'bootstrap': tool_identity, 'gzip_bootstrap': identity(compressed),
        'zstd_producer': subprocess.check_output(['zstd', '--version'], text=True, timeout=10).strip(),
        'materialize_seconds': elapsed, 'max_child_rss_bytes': resource.getrusage(resource.RUSAGE_CHILDREN).ru_maxrss * 1024,
        'byte_equal_and_ordinary_verifier_passed': True,
    })


def main():
    if (len(sys.argv) != 3 or os.environ.get('GITHUB_ACTIONS') != 'true'
            or os.environ.get('XINSIGHT_CI_EPHEMERAL_HOST') != '1'
            or os.geteuid() == 0 or (sys.platform, platform.machine()) != ('linux', 'x86_64')):
        raise ValueError('usage on disposable Linux Actions VM: qualify-ci-fpv22-delta.py SIGNED_CI_DIR NEW_OUTPUT')
    runner = Path(os.environ['RUNNER_TEMP']).resolve(strict=True)
    signed_dir, output = (Path(argument).resolve() for argument in sys.argv[1:])
    require(signed_dir.is_relative_to(runner) and output.parent == runner, 'CI inputs or output outside runner staging')
    require(not output.exists(), 'delta output already exists')
    commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()
    require(commit == os.environ['GITHUB_SHA'], 'checked-out CI commit differs from run identity')
    qualify(signed_dir, output, runner, commit)


if __name__ == '__main__':
    try:
        main()
    except (ValueError, KeyError, OSError, subprocess.SubprocessError) as error:
        raise SystemExit(f'FPV22 delta qualification: {error}') from error
