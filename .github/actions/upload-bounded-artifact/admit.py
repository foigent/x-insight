#!/usr/bin/env python3
"""Admit the CI allowlist and snapshot exactly the bytes passed to upload-artifact."""

import fnmatch
import os
from pathlib import Path
import re
import stat
import sys
import tempfile

MIB = 1 << 20
KIB = 1 << 10
# Per archive, including a conservative allowance for ZIP expansion.
# The workflow matrix sets the number of installed archives.
# scripts/test-ci-artifact-bounds.py checks the combined PR and main ceiling.
LIMITS = {
    'signed': (96 * MIB, {'pull_request': 7, 'push': 14}),
    'coverage': (4 * MIB, {'pull_request': 7}),
    'installed': (128 * KIB, {'pull_request': 14, 'push': 30}),
    'p3': (16 * KIB, {'pull_request': 14, 'push': 30}),
    'protocol': (128 * KIB, {'pull_request': 14, 'push': 30, 'workflow_dispatch': 14}),
}


def check_path(path, root):
    """Return lstat without following symlinks within either trusted root."""
    relative = path.relative_to(root)
    current = root
    for index, part in enumerate(relative.parts):
        current = current / part
        try:
            entry = current.lstat()
        except FileNotFoundError:
            return None
        if stat.S_ISLNK(entry.st_mode):
            raise ValueError('symlink in artifact allowlist')
        if index + 1 < len(relative.parts) and not stat.S_ISDIR(entry.st_mode):
            raise ValueError('invalid artifact path parent')
    return current.lstat()


def allowed_files(patterns, workspace, runner):
    """Match the file-only subset of the upload-artifact path syntax we use."""
    search_roots = set()
    files = {}
    for pattern in patterns.splitlines():
        pattern = pattern.strip()
        if not pattern:
            continue
        path = Path(pattern)
        if not path.is_absolute():
            path = workspace / path
        root = next((root for root in (runner, workspace) if path.is_relative_to(root)), None)
        if root is None:
            raise ValueError('artifact allowlist must stay inside the runner or workspace')
        parts = path.relative_to(root).parts
        if (not parts or any(part.startswith('.') for part in parts)
                or any(not re.fullmatch(r'[a-zA-Z0-9_. -]+', part) for part in parts[:-1])
                or not re.fullmatch(r'[a-zA-Z0-9_. *-]+', parts[-1])
                or '**' in parts[-1]):
            raise ValueError('unsupported artifact allowlist pattern')
        wildcard = '*' in path.name
        search_roots.add(path.parent if wildcard else path)
        if wildcard:
            parent = check_path(path.parent, root)
            if parent is None:
                continue
            if not stat.S_ISDIR(parent.st_mode):
                raise ValueError('artifact wildcard parent is not a directory')
            candidates = (child for child in path.parent.iterdir()
                          if not child.name.startswith('.') and fnmatch.fnmatchcase(child.name, path.name))
        else:
            candidates = (path,)
        for child in candidates:
            entry = check_path(child, root)
            if entry is None:
                continue
            if not stat.S_ISREG(entry.st_mode) or not re.fullmatch(r'[a-zA-Z0-9_. -]+', child.name):
                raise ValueError('artifact match is not a supported regular file')
            files[child] = entry
    if not search_roots:
        raise ValueError('empty artifact allowlist')
    archive_root = Path(os.path.commonpath(search_roots))
    if len(files) == 1 and archive_root in files:
        archive_root = archive_root.parent
    return archive_root, files


def zip_bound(files, root):
    # Use zlib's conservative deflateBound fixed-block formula plus space
    # for local and central ZIP headers, descriptors and extra fields.
    # Count both copies of each entry name and reserve the end records.
    total = 4096
    for path, info in files.items():
        size = info.st_size
        name_bytes = len(path.relative_to(root).as_posix().encode('utf-8'))
        total += size + (size >> 3) + (size >> 8) + (size >> 9) + 1024 + 2 * name_bytes
    return total


def fingerprint(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def snapshot_file(path, target, expected):
    # O_NOFOLLOW and O_NONBLOCK also reject a late replacement by a link or
    # special file without waiting for a writer on a pipe.
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(descriptor, 'rb') as source:
        if fingerprint(os.fstat(source.fileno())) != fingerprint(expected):
            raise ValueError('artifact file changed before staging')
        remaining = expected.st_size
        target.parent.mkdir(parents=True, exist_ok=True)
        with target.open('xb') as destination:
            while chunk := source.read(min(1 << 20, remaining + 1)):
                if len(chunk) > remaining:
                    raise ValueError('artifact file grew while staging')
                destination.write(chunk)
                remaining -= len(chunk)
        if remaining or fingerprint(os.fstat(source.fileno())) != fingerprint(expected):
            raise ValueError('artifact file changed while staging')


def stage(patterns, workspace, runner, kind, missing, event):
    if kind not in LIMITS or missing not in {'warn', 'error'}:
        raise ValueError('unknown artifact class or missing-file policy')
    limit, retention = LIMITS[kind]
    if event not in retention:
        raise ValueError(f'unsupported workflow event for {kind} artifact')
    days = retention[event]
    root, files = allowed_files(patterns, workspace, runner)
    if not files:
        if missing == 'error':
            raise ValueError('required artifact allowlist matched no files')
        print('::warning::Artifact allowlist matched no files')
        return None, days
    names = {path.relative_to(root).as_posix().lower() for path in files}
    if len(names) != len(files):
        raise ValueError('artifact entry names collide')
    size = sum(info.st_size for info in files.values())
    bound = zip_bound(files, root)
    if bound > limit:
        raise ValueError(f'{kind} artifact exceeds byte ceiling: {size} source bytes, {bound} archive bound, limit {limit}')
    directory = Path(tempfile.mkdtemp(prefix='bounded-artifact-', dir=runner))
    for path, info in sorted(files.items()):
        snapshot_file(path, directory / path.relative_to(root), info)
    _, current = allowed_files(patterns, workspace, runner)
    if {path: fingerprint(info) for path, info in current.items()} != {
        path: fingerprint(info) for path, info in files.items()
    }:
        raise ValueError('artifact allowlist changed while staging')
    print(f'{kind} artifact: {len(files)} files, {size} source bytes, {bound}/{limit} archive bytes, {days} retention days')
    return directory, days


def main():
    os.umask(0o077)
    # These are requested lifetimes. The provider may clamp them; delivery
    # review checks the recorded expiry and locally archived evidence.
    directory, days = stage(
        os.environ['ARTIFACT_PATHS'],
        Path(os.environ['GITHUB_WORKSPACE']).resolve(strict=True),
        Path(os.environ['RUNNER_TEMP']).resolve(strict=True),
        os.environ['ARTIFACT_KIND'], os.environ['ARTIFACT_MISSING'], os.environ['GITHUB_EVENT_NAME'],
    )
    with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
        output.write(f'directory={directory or ""}\nretention-days={days}\n')


if __name__ == '__main__':
    try:
        main()
    except (OSError, KeyError, ValueError) as error:
        sys.exit(f'Artifact admission failed: {error}')
