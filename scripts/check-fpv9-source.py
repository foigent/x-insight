#!/usr/bin/env python3
"""Read-only admission of the exact FPV9 source inputs and optional frozen binaries."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess

ROOT = Path(__file__).resolve().parent.parent
MANIFEST = ROOT / 'compatibility/fpv9-linux-amd64/source-manifest.json'
MANIFEST_SHA256 = '0d3a1e2be3c6d702e6395ef1ad6d2945e250028956e32305243af6307c886e31'
ORIGIN = '8ee764accbda21f21b489e01a4ab7751571beed4'

def require(value, code):
    if not value:
        raise ValueError(code)

def checked_regular(path):
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1, 'input_not_regular')
    return info

def check(source, binaries=None, allow_private_worktree=False):
    require(source.is_absolute() and not source.is_symlink() and source.is_dir(), 'source_directory')
    checked_regular(MANIFEST)
    body = MANIFEST.read_bytes()
    require(hashlib.sha256(body).hexdigest() == MANIFEST_SHA256, 'manifest_digest')
    manifest = json.loads(body)
    require(manifest['schema_version'] == 'xinsight.fpv9-source-input.v1' and manifest['origin_commit'] == ORIGIN, 'manifest_origin')
    expected = {item['path']: item for item in manifest['files']}
    private = allow_private_worktree and (source / '.git').exists()
    if private:
        def git(*args):
            return subprocess.check_output(['git', '--no-optional-locks', '-C', str(source), '-c', 'core.fsmonitor=false', *args], stderr=subprocess.DEVNULL)
        require(git('rev-parse', 'HEAD').decode().strip() == ORIGIN, 'private_origin')
        require(not git('status', '--porcelain=v1', '--untracked-files=all'), 'private_source_dirty')
    else:
        actual = set()
        allowed_dirs = {str(parent) for name in expected for parent in Path(name).parents if str(parent) != '.'}
        for directory, dirs, names in os.walk(source, followlinks=False):
            for name in dirs:
                path = Path(directory) / name
                require(not path.is_symlink() and str(path.relative_to(source)) in allowed_dirs, 'source_extra_or_link')
            for name in names:
                path = Path(directory) / name
                checked_regular(path)
                actual.add(str(path.relative_to(source)))
        require(actual == set(expected), 'source_file_set')
    for name, item in expected.items():
        relative = Path(name)
        require(not relative.is_absolute() and '..' not in relative.parts, 'source_path')
        path = source
        for part in relative.parts:
            path = path / part
            require(not path.is_symlink(), 'source_link')
        before = checked_regular(path)
        require((before.st_mode & 0o111) == 0 and before.st_size == item['bytes'], 'source_mode_size')
        require(hashlib.sha256(path.read_bytes()).hexdigest() == item['sha256'], 'source_digest')
        after = path.lstat()
        require((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns) == (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns), 'source_changed')
    if binaries is not None:
        require(binaries.is_absolute() and not binaries.is_symlink() and binaries.is_dir(), 'binary_directory')
        for name, digest in manifest['frozen_binaries'].items():
            path = binaries / name
            info = checked_regular(path)
            require(info.st_mode & 0o111 and hashlib.sha256(path.read_bytes()).hexdigest() == digest, 'frozen_binary_digest')
    return {'schema_version': 'xinsight.fpv9-source-check.v1', 'origin_commit': ORIGIN,
            'selected_files': len(expected), 'frozen_binaries_checked': binaries is not None,
            'private_worktree': private, 'passed': True}

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('source', type=Path)
    parser.add_argument('--binaries', type=Path)
    parser.add_argument('--allow-private-worktree', action='store_true')
    args = parser.parse_args()
    try:
        print(json.dumps(check(args.source, args.binaries, args.allow_private_worktree), sort_keys=True))
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        code = str(error) if isinstance(error, ValueError) else 'input_read_failed'
        raise SystemExit('FPV9 source refused: ' + code)

if __name__ == '__main__':
    main()
