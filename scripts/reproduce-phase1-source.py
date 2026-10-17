#!/usr/bin/env python3
"""Admit original Phase 1 inputs without private Git; reproduce only on Linux."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shlex
import shutil
import stat
import subprocess

ROOT = Path(__file__).resolve().parent.parent
MANIFEST = ROOT / 'compatibility/phase1-linux-amd64/source-manifest.json'
MANIFEST_SHA256 = '08cccecc3f30743aadb46f704d9e4cdb602bb471952c3f7e37af89e399de630c'
ORIGIN = 'b9e5811cc53a5cb06614956e66a1c54756617f0a'
FROZEN_ARTIFACT = '3e3794338d690fe5b12de46600e17d455baeceab0b79300af260baf78d0346b1'
FROZEN_MANIFEST = 'aaa53c2af46f59c328ff3271fa518a20bb0551fd7bdc02a0463ff925a43a6bc1'

def require(condition, code):
    if not condition: raise ValueError(code)

def sha(path): return hashlib.sha256(path.read_bytes()).hexdigest()

def check(source):
    require(source.is_absolute() and not source.is_symlink() and source.is_dir(), 'source_directory')
    info = MANIFEST.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and sha(MANIFEST) == MANIFEST_SHA256, 'manifest_digest')
    manifest = json.loads(MANIFEST.read_bytes())
    require(manifest['origin_commit'] == ORIGIN and manifest['required_go'] == 'go1.25.11' and manifest['role'] == 'phase1', 'manifest_origin')
    expected = {f['path']: f for f in manifest['files']}
    allowed_dirs = {str(p) for name in expected for p in Path(name).parents if str(p) != '.'}
    actual = set()
    for directory, dirs, names in os.walk(source, followlinks=False):
        for name in dirs:
            path = Path(directory) / name
            require(not path.is_symlink() and str(path.relative_to(source)) in allowed_dirs, 'source_extra_or_link')
        for name in names: actual.add(str((Path(directory) / name).relative_to(source)))
    require(actual == set(expected), 'source_file_set')
    for name, f in expected.items():
        path = source / name
        before = path.lstat()
        require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1, 'source_not_regular')
        require(bool(before.st_mode & 0o111) == (f['git_mode'] == '100755') and before.st_size == f['bytes'], 'source_mode_size')
        require(sha(path) == f['sha256'], 'source_digest')
        after = path.lstat()
        require((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns) == (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns), 'source_changed')
    return manifest

def recipe(source, manifest):
    text = (source / 'scripts/build-release.sh').read_text()
    old_root = 'ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"'
    require(text.count(old_root) == 1, 'builder_root')
    text = text.replace(old_root, 'ROOT="$(pwd)"')
    changes = {'git rev-parse HEAD': 'printf %s ' + shlex.quote(ORIGIN),
               'git status --porcelain': "printf ''",
               'git show -s --format=%ct "$COMMIT"': 'printf %s ' + str(manifest['origin_source_date_epoch']),
               'git ls-files -z migrations schemas fixtures': r"printf '%s\0' " + ' '.join(shlex.quote(p) for p in manifest['package_input_paths'])}
    for old, new in changes.items():
        require(text.count(old) == 1, 'builder_git_shape')
        text = text.replace(old, new)
    # Only admitted original-source metadata replaces Git operations. The
    # actual platform.system()/machine() fields remain untouched.
    require('$(git ' not in text, 'builder_git_remaining')
    return text

def fresh(path):
    require(path.is_absolute() and not path.exists() and not path.is_symlink() and path.resolve() == path, 'output_not_fresh')

def build(source, output_source, output_dist):
    manifest = check(source)
    require(platform.system() == 'Linux' and platform.machine() in ['x86_64', 'amd64'], 'native_linux_amd64_required')
    require(not output_source.is_relative_to(source) and not output_dist.is_relative_to(source), 'output_inside_input')
    fresh(output_source); fresh(output_dist)
    require(output_source != output_dist and not output_source.is_relative_to(output_dist) and not output_dist.is_relative_to(output_source), 'output_overlap')
    env = dict(os.environ, GOTOOLCHAIN='local', GOFLAGS='-mod=readonly', XINSIGHT_EXPECT_COMMIT=ORIGIN, XINSIGHT_EXPECT_GO_VERSION='go1.25.11')
    require(subprocess.check_output(['go', 'env', 'GOVERSION'], env=env).decode().strip() == 'go1.25.11', 'Go_toolchain')
    shutil.copytree(source, output_source)
    check(output_source)
    text = recipe(output_source, manifest)
    subprocess.run(['bash', '-c', text, 'original-phase1-source-recipe', str(output_dist)], cwd=output_source, env=env, check=True)
    name = 'xinsight-release-linux-amd64-' + ORIGIN
    artifact = output_dist / (name + '.tar.gz')
    release_manifest = output_dist / name / 'release-manifest.json'
    require(sha(artifact) == FROZEN_ARTIFACT and sha(release_manifest) == FROZEN_MANIFEST, 'frozen_legacy_identity')
    subprocess.run(['bash', str(output_source / 'scripts/verify-release.sh'), str(artifact)], cwd=output_source, env=env, check=True)
    check(output_source)
    print(json.dumps({'origin_commit': ORIGIN, 'artifact_sha256': FROZEN_ARTIFACT, 'manifest_sha256': FROZEN_MANIFEST, 'passed': True}, sort_keys=True))

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check-source-directory', type=Path)
    parser.add_argument('paths', nargs='*', type=Path)
    args = parser.parse_args()
    try:
        if args.check_source_directory is not None:
            require(not args.paths, 'arguments')
            m = check(args.check_source_directory)
            print(json.dumps({'origin_commit': ORIGIN, 'selected_files': len(m['files']), 'passed': True}, sort_keys=True))
        else:
            require(len(args.paths) == 3, 'arguments')
            build(*args.paths)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        code = str(error) if isinstance(error, ValueError) else 'input_or_builder_failed'
        raise SystemExit('Phase1 source refused: ' + code)

if __name__ == '__main__': main()
