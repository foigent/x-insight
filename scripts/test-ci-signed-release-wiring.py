#!/usr/bin/env python3
"""Check current, frozen legacy and exact unmanaged predecessor CI inputs."""

from pathlib import Path
import ast
import copy
import json
import os
import re
import subprocess
import tempfile

WORKFLOW = Path(__file__).resolve().parents[1] / ".github/workflows/ci.yml"
REPRODUCTION = Path(__file__).resolve().parent / "reproduce-direct-fpv9.sh"
RC13_COMMIT = "8bcea5bebf6f0f0796391dfe8c62fdf7a43519eb"


def require(condition: bool, message: str) -> None:
    if not condition:
        raise ValueError(message)


def job(source: str, name: str) -> str:
    match = re.search(rf"^  {re.escape(name)}:\n(.*?)(?=^  [a-z_][a-z_0-9]*:\n|\Z)", source, re.M | re.S)
    require(match is not None, f"missing job: {name}")
    return match.group(1)


def step(source: str, name: str) -> str:
    match = re.search(rf"^      - name: {re.escape(name)}\n(.*?)(?=^      - name: |\Z)", source, re.M | re.S)
    require(match is not None, f"missing step: {name}")
    return match.group(1)


def check(source: str) -> None:
    frontend = job(source, "frontend")
    install_npm = step(frontend, "Install declared npm toolchain")
    require('npm install --global "$(node -p "require(\'./package.json\').packageManager")"' in install_npm,
            "frontend must install the npm version declared by packageManager")
    require(frontend.index("Set up Node") < frontend.index("Install declared npm toolchain") <
            frontend.index("Install locked dependencies") < frontend.index("Check TypeScript"),
            "frontend npm toolchain must be installed before npm ci and checks")
    backend = job(source, "backend_validation")
    require("bash scripts/verify-protected-runtime.sh" in step(backend, "Check release workflow scripts"),
            "backend preflight must execute both protected runtime gates before long tests")
    require("XINSIGHT_REQUIRE_POSTGRES_TESTS: '1'" in step(backend, "Run tests"),
            "mandatory database qualification must reject missing PostgreSQL configuration")
    lifecycle = step(backend, "Prove connector control credential lifecycle on PostgreSQL 16")
    require("XINSIGHT_REQUIRE_POSTGRES_TESTS: '1'" in lifecycle and
            "TestControlCredential(LifecyclePostgres16InitialRotateConfirmRecover|TerminalFencesPostgres16|V1InitialLineagePostgres16|Schema44ExpiredPredecessorPostgres16)" in lifecycle and
            "TestControlCredentialReadOnlyPreflightAcrossSchema43To45" in lifecycle,
            "mandatory credential lifecycle PostgreSQL regressions are not wired")
    signed = job(source, "signed_release")
    fresh = job(source, "fresh_install")
    changes = job(source, "changes")
    gate = job(source, "ci_gate")
    for runtime in (signed, fresh):
        require("needs.changes.outputs.current_release_required == 'true'" in runtime and
                "github.event.pull_request.head.repo.full_name == github.repository" not in runtime,
                "synthetic signed/runtime qualification must include fork PRs")
    require("permissions:\n  contents: read" in source and "pull_request_target:" not in source,
            "public CI must use read-only pull_request jobs")
    require("secrets." not in source and "create-github-app-token" not in source,
            "public CI must not require maintainer secrets")
    for qualified in (backend, signed):
        authority = step(qualified, "Checkout immutable Neutral D1 qualification authority")
        require("ref: db10730d64a9302887ad59c53921e98c757927e7" in authority and
                "persist-credentials: false" in authority,
                "public Neutral D1 checkout must retain its immutable identity")
        require("GH_TOKEN: ${{ github.token }}" in step(qualified, "Acquire and qualify immutable Neutral D1 release"),
                "public release download must use the read-only job token")
    require("go run ./cmd/xinsight-release keygen" in step(signed, "Generate ephemeral non-publishable CI trust identity"),
            "CI must use an ephemeral qualification key")
    require("current_release_required: ${{ steps.qualification.outputs.current_release_required }}" in changes and
            "CURRENT_RELEASE_REQUIRED: ${{ needs.changes.outputs.current_release_required }}" in gate,
            "current qualification route is missing from the aggregate gate")
    builds = step(signed, "Build signed candidate twice")
    for candidate in ("a", "b"):
        require(
            f'scripts/build-signed-release.sh "${{RUNNER_TEMP}}/xinsight-signed-{candidate}" --profile=development-d' in builds,
            f"current candidate {candidate} must use development-d",
        )
    require(builds.count("scripts/build-signed-release.sh") == 2, "current build count changed")

    historical = step(signed, "Rebuild and verify frozen rc13 signed candidate")
    for required in (
        f"XINSIGHT_RC13_COMMIT: {RC13_COMMIT}",
        'python3 scripts/reproduce-signed-predecessor-source.py --build rc13',
        '"$GITHUB_WORKSPACE/compatibility/rc13-linux-amd64/source" "$rc13_source" "${RUNNER_TEMP}/rc13-signed"',
        'test "$commit" = "$XINSIGHT_RC13_COMMIT"',
        'python3 scripts/reproduce-signed-predecessor-source.py --verify rc13 "$rc13_source"',
        '"$artifact" "${RUNNER_TEMP}/ci-trusted-release-keys.json"',
        '"${RUNNER_TEMP}/verified-rc13-payload"',
    ):
        require(required in historical, f"frozen rc13 binding missing: {required}")
    require('git worktree' not in historical and 'git -C "$rc13_source"' not in historical,
            "frozen rc13 must use admitted bundled inputs without private ancestry")
    phase1 = step(signed, "Reproduce and verify exact Phase 1 legacy artifact")
    for binding in ('python3 scripts/reproduce-phase1-source.py',
                    '"$PWD/compatibility/phase1-linux-amd64/source"',
                    '"${RUNNER_TEMP}/phase1-source" "${RUNNER_TEMP}/phase1-dist"',
                    'sha256:3e3794338d690fe5b12de46600e17d455baeceab0b79300af260baf78d0346b1',
                    'XINSIGHT_EXPECT_GO_VERSION: go1.25.11'):
        require(binding in phase1, "Phase 1 exact source/package binding missing: " + binding)
    require("go-version: '1.25.11'" in step(signed, "Set up exact historical Phase 1 Go") and
            signed.index("Set up exact historical Phase 1 Go") < signed.index("Reproduce and verify exact Phase 1 legacy artifact") <
            signed.index("Rebuild and verify frozen rc13 signed candidate"),
            "Phase 1 and RC13 must retain their original Go1.25.11 toolchain")
    require(
        signed.index("Rebuild and verify frozen rc13 signed candidate")
        < signed.index("Restore current Go after historical reproduction")
        < signed.index("Verify signature, extract payload, binaries and offline manual"),
        "frozen Go 1.25.11 must be restored to current Go 1.25.12 before current verification",
    )

    current_verify = step(signed, "Verify signature, extract payload, binaries and offline manual")
    require("--profile=development-d" in current_verify, "current candidate verifier profile missing")
    require("xinsight-signed-a/build-result.json" in current_verify, "current candidate identity missing")
    host = step(signed, "Qualify Phase 1 to frozen rc13 transition and recovery on clean Ubuntu VM")
    for required in (
        "rc13-signed/build-result.json",
        '"${RUNNER_TEMP}/rc13-source/scripts/qualify-ubuntu22-host.sh"',
        '"${RUNNER_TEMP}/verified-rc13-payload"',
        '"${RUNNER_TEMP}/ci-trusted-release-keys.json"',
        "phase1-dist/xinsight-release-linux-amd64-b9e5811cc53a5cb06614956e66a1c54756617f0a.tar.gz",
    ):
        require(required in host, f"historical qualification input missing: {required}")
    require("verified-signed-payload" not in host, "historical host check consumed current candidate")

    upload = step(signed, "Upload explicitly non-publishable CI qualification artifact")
    for required in ("xinsight-signed-a/build-result.json", "rc13-signed/build-result.json", "verified-rc13-payload.campaign-authority.verify.json", "xinsight-host-qualification/qualification-result.json"):
        require(required in upload, f"CI evidence path missing: {required}")
    fresh_verify = step(fresh, "Verify and extract exact signed candidate")
    require("xinsight-signed-a/build-result.json" in fresh_verify, "fresh install must consume current candidate")
    require("--profile=development-d" in fresh_verify, "fresh install verifier profile missing")
    require("rc13-signed" not in fresh_verify, "fresh install consumed historical candidate")
    fresh_script = (WORKFLOW.parent.parent.parent / "scripts/qualify-ubuntu22-fresh-install.sh").read_text()
    for binding in ("/usr/bin/nsenter --target", "controller-old-mount-proof.json", "controller-old-context.json",
                    "controller-current-mount-proof.json", "controller-manager-call.txt",
                    "--bounding-set=-all", "--no-new-privs"):
        require(binding in fresh_script and binding in fresh if binding.endswith((".json", ".txt")) else binding in fresh_script,
                f"current controller namespace qualification missing {binding}")
    for line in fresh_script.splitlines():
        if '/usr/bin/nsenter --target' in line:
            require('--mount --root --wd --' in line,
                    'mount probe retained caller root/cwd outside the service namespace')
    checkout = step(fresh, 'Checkout fixed UR installed-service fixture')
    require('ref: ef52a29f2d463a8c5b136f9f18fdd975c88a0c26' in checkout and 'persist-credentials: false' in checkout,
            'installed UR fixture must use a fixed commit without persisted token')
    require('test "$(git -C dist/ci-installed-ur rev-parse HEAD)" = ef52a29f2d463a8c5b136f9f18fdd975c88a0c26' in
            step(fresh, 'Verify exact UR fixture identity'),
            'installed UR fixture checkout lacks exact commit readback')
    require('qualify-ubuntu22-installed-joint.sh' in step(fresh, 'Qualify installed controller and consumer lifecycle'),
            'fresh install does not run the installed business lifecycle')
    require('lifecycle: [new_install, adopt_existing, fpv17_upgrade]' in fresh and 'fail-fast: false' in fresh,
            'new installation, restored predecessor and FPV17 upgrade need independent VMs')
    predecessor = step(fresh, 'Reproduce and install exact FPV14 predecessor on clean VM')
    require("if: matrix.lifecycle == 'adopt_existing'" in predecessor and
            'prepare-ci-fpv14-predecessor.sh' in predecessor and
            "p['old_source']+'/scripts/qualify-ubuntu22-fresh-install.sh'" in predecessor,
            'restored predecessor must install its own exact artifact and service units')
    fpv17 = step(fresh, 'Reproduce and install exact FPV17 predecessor on clean VM')
    require("if: matrix.lifecycle == 'fpv17_upgrade'" in fpv17 and
            'prepare-ci-fpv17-predecessor.sh' in fpv17 and
            "p['old_source']+'/scripts/qualify-ubuntu22-fresh-install.sh'" in fpv17,
            'FPV17 lane must install its independently rebuilt CI-only predecessor')
    prestate = step(fresh, 'Qualify FPV17 to FPV19 installed upgrade and source continuation')
    require("if: matrix.lifecycle == 'fpv17_upgrade'" in prestate and
            'XINSIGHT_CI_UPGRADE_PRESTATE: "1"' in prestate and
            'qualify-ubuntu22-installed-joint.sh' in prestate,
            'FPV17 lane must seed real installed history and body without revocation')
    require('qualify-ubuntu22-fpv17-upgrade.sh' not in prestate,
            'upgrade must execute inside the live UR fixture, not after it exits')
    require("matrix.lifecycle != 'fpv17_upgrade'" in step(fresh, 'Qualify installed controller and consumer lifecycle'),
            'existing FPV14 failure/recovery lane must remain distinct')
    upload = step(fresh, 'Upload explicitly non-publishable fresh-install evidence')
    require('if: always()' in upload and 'xinsight-installed-joint-qualification/stage-receipts.json' in upload,
            'failed installed lifecycle loses safe stage receipts')
    require('xinsight-fpv17-to-fpv19-qualification/qualification-failure.json' in upload,
            'FPV17 upgrade failure loses fixed stage and exit code')
    require('xinsight-fpv17-to-fpv19-qualification/qualification-result.json' in upload,
            'FPV17 same-schema qualification receipt is not uploaded')
    for unsafe in ('xinsight-fpv15-joint-private', '*.log', 'nginx-error.log', 'joint-private.log'):
        require(unsafe not in upload, 'private fixture output must not become artifact: ' + unsafe)


def check_direct_fpv9(source: str, reproduction: str | None = None) -> None:
    if reproduction is None:
        reproduction = REPRODUCTION.read_text(encoding="utf-8")
    direct = job(source, "direct_fpv9_upgrade")
    gate = job(source, "ci_gate")
    changes = job(source, "changes")
    require("if: needs.changes.outputs.direct_fpv9_required == 'true'" in direct,
            "exact predecessor qualification must use the checked version/event route")
    require("--write-direct-fpv9-route" in changes,
            "missing exact predecessor route selection")
    for binding in (
        "- direct_fpv9_upgrade",
        "DIRECT_FPV9_RESULT: ${{ needs.direct_fpv9_upgrade.result }}",
        "DIRECT_FPV9_REQUIRED: ${{ needs.changes.outputs.direct_fpv9_required }}",
        "PR_HEAD_REPOSITORY: ${{ github.event.pull_request.head.repo.full_name }}",
    ):
        require(binding in gate, f"qualification aggregate binding missing: {binding}")
    require("secrets." not in direct and "create-github-app-token" not in direct,
            "exact predecessor qualification must not use private repository secrets")
    old = step(direct, "Reproduce exact unmanaged FPV9 binaries")
    backend = job(source, "backend_validation")
    unit_inputs = step(backend, "Reproduce exact FPV9 unit-test inputs")
    require('scripts/reproduce-direct-fpv9.sh "${RUNNER_TEMP}/direct-fpv9-source" "${RUNNER_TEMP}/direct-fpv9-bin"' in old,
            "host qualification must use shared exact reproduction")
    require('scripts/reproduce-direct-fpv9.sh "${RUNNER_TEMP}/unit-fpv9-source" "${RUNNER_TEMP}/unit-fpv9-bin"' in unit_inputs,
            "backend qualification must reproduce its exact unit-test inputs")
    require("fetch-depth: 0" in step(backend, "Checkout code"), "backend must retain complete CI comparison history")
    require("XI_FPV9_REPRO_DIR: ${{ runner.temp }}/unit-fpv9-bin" in step(backend, "Run tests"),
            "exact resume tests must receive reproduced binaries instead of skipping")
    for identity in (
        "8ee764accbda21f21b489e01a4ab7751571beed4",
        "BuildTime=2026-09-27T06:20:00Z",
        "80573c95149f5b9827b06f264f8d70e436a29ea1d8e93ddb6f1f46a75f4866d7",
        "c6b71ded9aa3fac35c8099aba222e13edb8772a77616f6ab3bb53094cbbcfda8",
        "hashlib.sha256",
    ):
        require(identity in reproduction, f"exact predecessor identity missing: {identity}")
    target = step(direct, "Build and verify FPV11 candidate for exact predecessor")
    require(target.count("--profile=development-d") == 2, "candidate build and verification profiles differ")
    require('test "$commit" = "$GITHUB_SHA"' in target, "candidate commit is not checked")
    runtime = step(direct, "Qualify exact FPV9 adoption and recovery")
    require("XINSIGHT_CI_EPHEMERAL_HOST: '1'" in runtime, "disposable VM guard missing")
    for argument in (
        'scripts/qualify-ubuntu22-direct-fpv9.sh "$artifact"',
        '"${RUNNER_TEMP}/direct-fpv9-payload"',
        '"${RUNNER_TEMP}/direct-fpv9-trust/trusted-release-keys.json"',
        '"${RUNNER_TEMP}/direct-fpv9-source"',
        '"${RUNNER_TEMP}/direct-fpv9-bin"',
    ):
        require(argument in runtime, f"qualification input missing: {argument}")
    upload = step(direct, "Upload allowlisted FPV9 qualification evidence")
    require("if: always()" in upload, "failed qualification must retain safe diagnostics")
    require("qualification-result.json" in upload, "qualification result is not retained")
    require("stage-result.json" in upload, "qualification phase diagnostics are not retained")
    for excluded in ("*.json", "*.dump", "signing-key.json", "postgres.env", ".dsn", "**"):
        require(excluded not in upload, f"unsafe qualification upload path: {excluded}")


def check_managed_fpv11(source: str, qualification: str | None = None) -> None:
    managed = job(source, "managed_fpv11_upgrade")
    changes = job(source, "changes")
    gate = job(source, "ci_gate")
    require("managed_fpv11_required: ${{ steps.qualification.outputs.managed_fpv11_required }}" in changes,
            "missing managed qualification route output")
    require("if: needs.changes.outputs.managed_fpv11_required == 'true'" in managed,
            "managed qualification must use its version-specific checked route")
    for binding in ("- managed_fpv11_upgrade", "MANAGED_FPV11_RESULT: ${{ needs.managed_fpv11_upgrade.result }}", "MANAGED_FPV11_REQUIRED: ${{ needs.changes.outputs.managed_fpv11_required }}"):
        require(binding in gate, f"missing required managed aggregate binding: {binding}")
    require("secrets." not in managed and "create-github-app-token" not in managed,
            "managed qualification must not acquire private credentials")
    preflight_name = "Check exact published managed predecessor source"
    preflight = step(managed, preflight_name)
    for binding in ('python3 scripts/reproduce-signed-predecessor-source.py --check-source-directory fpv11 "$GITHUB_WORKSPACE/compatibility/fpv11-linux-amd64/source"',
                    'open("compatibility/fpv11-linux-amd64/source/release/d-validation-config.json")',
                    'c["product_version"]=="0.2.0-dev.fpv11"',
                    'c["database_schema"]=={"current":"43","min":"43","max":"43"}'):
        require(binding in preflight, f"exact available predecessor preflight missing: {binding}")
    require(managed.index(preflight_name) < managed.index("Set up Go") < managed.index("Generate isolated non-publishable qualification identity"),
            "predecessor availability must be checked before expensive or immutable preparation")
    build = step(managed, "Build and verify current FPV12 and frozen managed FPV11")
    for binding in ('scripts/build-signed-release.sh "${RUNNER_TEMP}/managed-fpv11-signed" --profile=development-d',
                    'test "$commit" = "$GITHUB_SHA"',
                    'python3 scripts/reproduce-signed-predecessor-source.py --build fpv11',
                    '"$GITHUB_WORKSPACE/compatibility/fpv11-linux-amd64/source" "$old_source" "${RUNNER_TEMP}/managed-fpv11-old-signed"',
                    'python3 scripts/reproduce-signed-predecessor-source.py --verify fpv11 "$old_source" "$old_artifact"',
                    'test "$old_commit" = 9c58e07f011918c4a0aed2f876ce4baa9fb12b5e',
                    'managed-fpv11-old-signed/build-result.json', 'managed-fpv11-old-payload',
                    'scripts/verify-signed-release.sh'):
        require(binding in build, f"managed signed predecessor/candidate binding missing: {binding}")
    runtime = step(managed, "Qualify managed FPV11 upgrade and recovery")
    require("XINSIGHT_CI_EPHEMERAL_HOST: '1'" in runtime, "managed disposable host guard missing")
    for binding in ('scripts/qualify-ubuntu22-managed-fpv11.sh "$artifact"', '"$old_artifact"',
                    '"${RUNNER_TEMP}/managed-fpv11-old-payload"', '"${RUNNER_TEMP}/managed-fpv11-source"'):
        require(binding in runtime, f"managed runtime input missing: {binding}")
    require("qualify-ubuntu22-direct-fpv9.sh" not in managed, "normal managed upgrade used unmanaged conversion")
    require('git worktree' not in build and 'git cat-file' not in preflight and 'git show' not in preflight,
            "managed predecessor must not require private Git ancestry")
    if qualification is None:
        qualification = (WORKFLOW.parent.parent.parent / "scripts/qualify-ubuntu22-managed-fpv11.sh").read_text()
    require('python3 "$ROOT/scripts/reproduce-signed-predecessor-source.py" --check-built-source-directory fpv11 "$OLD_SOURCE"' in qualification and
            "'predecessor_commit':'9c58e07f011918c4a0aed2f876ce4baa9fb12b5e'" in qualification and
            "'classification':'ci_synthetic_not_target'" in qualification,
            "managed predecessor input and CI-only receipt identity disagree")
    require('--backup "$BACKUP_PATH"' not in qualification and 'ci-review-body.key' not in qualification,
            "managed current custody still uses raw dump restore or a substitute CI body key")
    for binding in ('--recovery-bundle "$BACKUP_PATH"', '--recovery-bundle "$PRIVATE_BACKUP"',
                    'backup --output "$PRIVATE_BACKUP" --check', 'pg importer-is-nologin',
                    'custody-after-upgrade.txt', '--check-loaded', 'after-private-restore-check.json'):
        require(binding in qualification, f"managed original custody/backup qualification missing: {binding}")
    upload = step(managed, "Upload allowlisted managed FPV11 qualification evidence")
    require("if: always()" in upload and "qualification-result.json" in upload and "linux-credential-proof.json" in upload and "linux-reader-credential-proof.json" in upload,
            "managed qualification evidence missing")
    require("xinsight-managed-fpv11-qualification/service-failure-diagnostic.json" in upload and
            "trap qualification_exit EXIT" in qualification,
            "managed failure diagnostic must be retained on its original qualification route")
    for excluded in ("*.json", "*.dump", "signing-key.json", "postgres.env", ".dsn", "**"):
        require(excluded not in upload, f"unsafe managed upload path: {excluded}")


def check_managed_private_backup_path() -> None:
    root = WORKFLOW.parent.parent.parent
    qualification = (root / 'scripts/qualify-ubuntu22-managed-fpv11.sh').read_text()
    paths = (root / 'internal/hostctl/paths.go').read_text()
    backup_dir = re.search(r'BackupsDir:\s*mapPath\("([^"]+)"\)', paths)[1]
    upload = step(job(WORKFLOW.read_text(), 'managed_fpv11_upgrade'), 'Upload allowlisted managed FPV11 qualification evidence')

    def validate(script: str, artifact_step: str) -> None:
        match = re.search(r'^PRIVATE_BACKUP="([^"]+)"$', script, re.M)
        require(match is not None and str(Path(match[1]).parent) == backup_dir,
                'private backup must use the installed root-owned backup directory')
        require('\nsudo test ! -e "$PRIVATE_BACKUP"\n' in script and
                '\nsudo test ! -L "$PRIVATE_BACKUP"\n' in script,
                'backup check absence must be verified as root, including dangling links')
        require(match[1] not in artifact_step and '*.tar' not in artifact_step,
                'private backup must not enter qualification uploads')

    validate(qualification, upload)
    for invalid, invalid_upload in (
        (qualification.replace(backup_dir + '/ci-private-managed-fpv12.tar', '$QUAL_ROOT/private-managed-backup.tar'), upload),
        (qualification.replace('sudo test ! -e "$PRIVATE_BACKUP"', 'test ! -e "$PRIVATE_BACKUP"'), upload),
        (qualification.replace('sudo test ! -L "$PRIVATE_BACKUP"', 'test ! -L "$PRIVATE_BACKUP"'), upload),
        (qualification, upload + '\n' + backup_dir + '/ci-private-managed-fpv12.tar'),
    ):
        try:
            validate(invalid, invalid_upload)
        except ValueError:
            continue
        raise ValueError('unsafe private backup fixture or upload was accepted')


def check_managed_namespace_probe() -> None:
    qualification = (WORKFLOW.parent.parent.parent / 'scripts/qualify-ubuntu22-managed-fpv11.sh').read_text()
    match = re.search(r"<<'PYPROBE'\n(.*?)\nPYPROBE", qualification, re.S)
    require(match is not None, 'live namespace probe missing')
    namespace = {'__name__': 'offline_namespace_probe_test'}
    exec(compile(match[1], 'managed namespace probe', 'exec'), namespace)
    # No Linux service, namespace, credentials, or external command is invoked.
    from types import SimpleNamespace
    import io
    import stat
    from unittest.mock import patch
    failure = namespace['ProbeFailure']
    real_loaded = namespace['loaded_receipt']
    secret = 'secret-marker-DSN-token-key-body'
    uid, gid = 997, 997
    state = dict(main_pid=123, invocation_id='a'*32, uid=uid, gid=gid, start_ticks=234,
                 mount_namespace_device=4, mount_namespace_inode=5)
    require(set(namespace['CONSUMERS']) == {'xinsight.service', 'xinsight-source-facts.service'},
            'probe widened consumer boundary')
    require('ExecStartPost=/usr/local/bin/xinsight-ci-managed-credential-proof' not in qualification and
            all(retired not in qualification for retired in ('ReadWritePaths=', '90-ci-managed-proof.conf', '/var/lib/x-insight/ci-managed-proof', 'def open_receipt(')) and
            qualification.index('for tool in /usr/bin/nsenter /usr/bin/setpriv') < qualification.index('"$OLD_SOURCE/scripts/qualify-ubuntu22-fresh-install.sh"'),
            'probe still uses control-process namespace or tool preflight occurs too late')
    require(qualification.count('run_credential_probe xinsight-source-facts.service > "$QUAL_ROOT/linux-credential-proof.json"') == 2 and
            qualification.count('run_credential_probe xinsight.service > "$QUAL_ROOT/linux-reader-credential-proof.json"') == 2 and
            qualification.count('/usr/local/bin/xinsight-ci-managed-credential-proof --check-loaded >') == 2,
            'initial/restored current proofs or original loaded authority missing')
    def rejects(call, category=None):
        try:
            call()
        except failure as error:
            require(category is None or error.safe['category'] == category, 'wrong finite probe failure category')
            require(secret not in json.dumps(error.safe), 'probe failure leaked arbitrary stderr')
            return error.safe
        raise AssertionError('invalid namespace probe accepted')
    # Verify kernel identity parsing (comm may itself contain right parentheses).
    systemd = 'ActiveState=active\nMainPID=123\nInvocationID='+'a'*32+'\nUser=xinsight\nGroup=xinsight\n'
    status = 'Uid:\t997 997 997 997\nGid:\t997 997 997 997\nNoNewPrivs:\t1\nCapEff:\t0000000000000000\nCapPrm:\t0000000000000000\nCapAmb:\t0000000000000000\n'
    proc_stat = '123 (process (with) spaces)) S '+' '.join(['0']*18+['234'])
    executable = '/opt/x-insight/releases/current/bin/xinsight-server'
    def file_input(path):
        return io.StringIO(status if path.endswith('/status') else proc_stat)
    with patch('builtins.open', side_effect=file_input), patch.object(namespace['os'], 'readlink', return_value=executable), patch.object(namespace['os'], 'stat', return_value=SimpleNamespace(st_dev=4, st_ino=5)):
        namespace['bounded_probe_command'] = lambda args: (systemd, 0, False)
        require(namespace['live_identity']('xinsight.service', uid, gid, executable) == state, 'exact proc identity parsing failed')
        for old, new, category in [('ActiveState=active', 'ActiveState=failed', 'live_service_identity_invalid'),
                                   ('MainPID=123', 'MainPID=0', 'live_service_identity_invalid'),
                                   ('User=xinsight', 'User=root', 'live_service_identity_invalid'),
                                   ('InvocationID='+'a'*32, 'InvocationID='+secret, 'live_service_identity_invalid')]:
            namespace['bounded_probe_command'] = lambda args, value=systemd.replace(old, new): (value, 0, False)
            rejects(lambda: namespace['live_identity']('xinsight.service', uid, gid, executable), category)
        namespace['bounded_probe_command'] = lambda args: (systemd, 0, False)
        for old, new, failed in [('Uid:\t997 997 997 997', 'Uid:\t0 0 0 0', 'uid_matches'),
                                  ('Gid:\t997 997 997 997', 'Gid:\t0 0 0 0', 'gid_matches'),
                                  ('NoNewPrivs:\t1', 'NoNewPrivs:\t0', 'no_new_privileges'),
                                  ('CapEff:\t0000000000000000', 'CapEff:\t0000000000000001', 'effective_caps_empty'),
                                  ('CapPrm:\t0000000000000000', 'CapPrm:\t0000000000000001', 'permitted_caps_empty'),
                                  ('CapAmb:\t0000000000000000', 'CapAmb:\t0000000000000001', 'ambient_caps_empty')]:
            with patch('builtins.open', side_effect=lambda path, value=status.replace(old, new): io.StringIO(value if path.endswith('/status') else proc_stat)):
                safe = rejects(lambda: namespace['live_identity']('xinsight.service', uid, gid, executable), 'live_process_identity_invalid')
                require(safe['process_checks'] == {field:field != failed for field in namespace['PROCESS_CHECKS']} and
                        safe['stage'] == 'before_probe_identity' and all(type(value) is bool for value in safe['process_checks'].values()),
                        'individual failing process check not identified as closed booleans')
        known_executable = '/opt/x-insight/current/bin/xinsight-importer'
        for observed, kind in [(known_executable, 'current_importer'), ('/opt/x-insight/current/bin/xinsight-server', 'current_server'),
                               ('/opt/x-insight/control/xinsight-launcher', 'managed_launcher'), ('/lib/systemd/systemd', 'systemd_startup'),
                               ('/usr/lib/systemd/systemd', 'systemd_startup'), ('/wrong/'+secret, 'other')]:
            with patch.object(namespace['os'], 'readlink', return_value=observed), patch.object(namespace['os'].path, 'realpath', side_effect=lambda path:path):
                safe = rejects(lambda: namespace['live_identity']('xinsight.service', uid, gid, executable, 'after_receipt_identity'), 'live_process_identity_invalid')
                require(safe['executable_kind'] == kind and safe['stage'] == 'after_receipt_identity' and
                        safe['process_checks'] == {field:field != 'executable_matches' for field in namespace['PROCESS_CHECKS']} and
                        observed not in json.dumps(safe), 'exe projection leaked path or mislabeled exact known executable')
        with patch.object(namespace['os'], 'readlink', side_effect=OSError(secret)):
            safe = rejects(lambda: namespace['live_identity']('xinsight.service', uid, gid, executable), 'live_process_identity_invalid')
            require(safe['executable_kind'] == 'unavailable' and not safe['process_checks']['executable_readable'] and
                    not safe['process_checks']['executable_matches'], 'unreadable exe emitted raw error or false match')
        safe = failure('live_process_identity_invalid', checks={'uid_matches':False, 'gid_matches':secret, 'unknown':secret}, executable_kind=secret, stage=secret).safe
        require(safe.get('process_checks') == {'uid_matches':False} and 'executable_kind' not in safe and 'stage' not in safe and
                secret not in json.dumps(safe), 'unknown diagnostic fields or enum text leaked')
    # Capture belongs to this execution; no service-writable receipt file exists.
    metadata = dict(form='service_owned_0400_native_candidate', regular=True, read_only=True, uid=uid, gid=0, mode='0400', links=1, secret=secret)
    base = dict(schema_version='xinsight.ci-managed-credential-proof.v1', classification='ci_linux_synthetic_no_source_call',
                consumer='xinsight.service', uid=uid, review_body_key='loaded', workspace_credential='loaded',
                direct_root_key_access='rejected', systemd_credentials='loaded', review_body_credential=metadata, secret=secret)
    worker = dict(base, consumer='xinsight-source-facts.service', root_owned_config=True, original_indexed_keys=9, managed_partition_keys=1)
    namespace['loaded_receipt'] = real_loaded
    for unit, (mode, binary) in namespace['CONSUMERS'].items():
        namespace['live_identity'] = lambda *args: dict(state)
        receipt = json.dumps(base if unit == 'xinsight.service' else worker)+'\n'
        calls, closed = [], []
        def command(args, pass_fds=()):
            calls.append((args, pass_fds))
            return receipt, 0, False
        namespace['bounded_probe_command'] = command
        with patch.object(namespace['pwd'], 'getpwnam', return_value=SimpleNamespace(pw_uid=uid)), patch.object(namespace['grp'], 'getgrnam', return_value=SimpleNamespace(gr_gid=gid)), patch.object(namespace['os'], 'open', return_value=10) as opened, patch.object(namespace['os'], 'fstat', return_value=SimpleNamespace(st_dev=4, st_ino=5)), patch.object(namespace['os'], 'close', side_effect=closed.append):
            proof = namespace['run_probe'](unit)
            require(proof['probe_context'] == dict(state, current_invocation_verified=True, scope='main_process_mount_and_uid_view') and
                    secret not in json.dumps(proof), 'successful capture omitted exact process binding or leaked unknown fields')
            require(opened.call_count == 1 and opened.call_args.args[0] == '/proc/123/ns/mnt' and closed == [10],
                    'probe retained disk receipt access or lost namespace FD lifetime')
            args, fds = calls[0]
            require(fds == (10,) and args[:3] == ['/usr/bin/nsenter', '--mount=/proc/self/fd/10', '--'] and
                    all(arg in args for arg in ('/usr/bin/setpriv', '--reuid=997', '--regid=997', '--clear-groups', '--no-new-privs',
                                                '--inh-caps=-all', '--ambient-caps=-all', '--bounding-set=-all', '/usr/bin/env', '-i')) and
                    args[-1] == mode and 'CREDENTIALS_DIRECTORY=/run/credentials/'+unit in args and
                    'XINSIGHT_REVIEW_BODY_SOURCE_KEY_FILE=/etc/x-insight/review-body.key' in args,
                    'probe lost exact namespace/drop-privileges/clean-environment/original-binding argv')
            for field, value in [('main_pid', 124), ('invocation_id', 'b'*32), ('uid', 998), ('gid', 998),
                                 ('start_ticks', 235), ('mount_namespace_inode', 6), ('mount_namespace_device', 6)]:
                for stage in ('after_probe_identity', 'after_receipt_identity'):
                    reads = iter((state, dict(state, **{field: value})) if stage == 'after_probe_identity' else
                                 (state, state, dict(state, **{field: value})))
                    namespace['live_identity'] = lambda *args: next(reads)
                    safe = rejects(lambda: namespace['run_probe'](unit), 'probe_process_changed')
                    require(safe['stage'] == stage, 'identity drift was not bound to precise phase')
            identity_calls = []
            def checked_identity(*args):
                identity_calls.append(args)
                if len(identity_calls) == 1:
                    return dict(state)
                raise failure('live_process_identity_invalid', checks={'uid_matches':False}, executable_kind='systemd_startup', stage='after_probe_identity')
            namespace['live_identity'] = checked_identity
            namespace['bounded_probe_command'] = lambda *args: ('\t/home/runner/scripts/ci-managed-source-credential-proof.go:106 +0xabc', 2, False)
            safe = rejects(lambda: namespace['run_probe'](unit), 'live_process_identity_invalid')
            require(safe['process_checks'] == {'uid_matches':False} and safe['executable_kind'] == 'systemd_startup' and
                    safe['stage'] == 'after_probe_identity' and safe['exit_code'] == 2 and safe['probe_callsites'] == [106] and
                    identity_calls[-1][-1] == 'after_probe_identity', 'post-probe failure lost precise checks/stage or original exit/callsite')
            namespace['live_identity'] = lambda *args: dict(state)
            message = 'panic: '+secret+'\n\t/home/runner/scripts/ci-managed-source-credential-proof.go:106 +0xabcdef\n\t/private/'+secret+'/other.go:107 +0xabcdef\n'
            for raw, code, truncated in [(message, 2, False), (receipt, 2, False), (receipt, 124, True), (message, 124, True)]:
                namespace['bounded_probe_command'] = lambda *args: (raw, code, truncated)
                safe = rejects(lambda: namespace['run_probe'](unit), 'credential_probe_failed')
                require(safe['exit_code'] == code and safe['truncated'] == truncated and secret not in json.dumps(safe),
                        'nonzero or oversized output accepted, or raw stderr leaked')
            rejected = dict(base, consumer=unit, review_body_key='rejected')
            namespace['bounded_probe_command'] = lambda *args: (json.dumps(rejected)+'\n'+message, 2, False)
            safe = rejects(lambda: namespace['run_probe'](unit), 'credential_probe_failed')
            require(safe['probe_callsites'] == [106] and safe['probe_receipt']['review_body_key'] == 'rejected' and
                    not safe['probe_receipt']['current_invocation_verified'] and
                    safe['probe_receipt']['review_body_credential']['form'] == metadata['form'],
                    'same-execution rejected key metadata was lost or promoted to proof')
            for raw in ('', receipt+message, 'prefix '+receipt, receipt+receipt, '{'+secret, '['*2000+']'*2000,
                        'x'*(namespace['RECEIPT_LIMIT']+1), receipt.replace('"uid": 997', '"uid": 0'),
                        receipt.replace('"consumer": "'+unit+'"', '"consumer": "other.service"')):
                namespace['bounded_probe_command'] = lambda *args: (raw, 0, False)
                rejects(lambda: namespace['run_probe'](unit), 'probe_receipt_invalid')
            namespace['bounded_probe_command'] = command
            with patch.object(namespace['os'], 'fstat', return_value=SimpleNamespace(st_dev=4, st_ino=9)):
                rejects(lambda: namespace['run_probe'](unit), 'probe_namespace_changed')
        rejects(lambda: namespace['run_probe']('other.service'), 'probe_consumer_invalid')
    # Actual bounded strict JSON parsing and closed successful projection.
    def loaded(value, unit='xinsight.service'):
        return real_loaded(value if isinstance(value, str) else json.dumps(value), unit, uid)
    require(secret not in json.dumps(loaded(base)), 'success receipt projected unknown secrets')
    acl = dict(base, review_body_credential=dict(metadata, form='root_0440_acl_candidate', uid=0, mode='0440'))
    require(loaded(acl)['review_body_credential']['uid'] == 0, 'original root ACL receipt form rejected')
    require(loaded(worker, worker['consumer'])['original_indexed_keys'] == 9, 'complete nine-key worker receipt lost')
    for field, value in [('review_body_key', 'rejected'), ('uid', 0), ('uid', True), ('consumer', 'other.service'),
                         ('workspace_credential', secret), ('direct_root_key_access', 'allowed')]:
        rejects(lambda: loaded(dict(base, **{field: value})), 'probe_receipt_invalid')
    for field, value in [('read_only', False), ('regular', False), ('mode', '0444'), ('uid', 0), ('gid', 997), ('links', 2)]:
        rejects(lambda: loaded(dict(base, review_body_credential=dict(metadata, **{field: value}))), 'probe_receipt_invalid')
    for field, value in [('original_indexed_keys', 8), ('managed_partition_keys', 0), ('root_owned_config', False)]:
        rejects(lambda: loaded(dict(worker, **{field: value}), worker['consumer']), 'probe_receipt_invalid')
    for raw in ('x'*(namespace['RECEIPT_LIMIT']+1), '{'+secret, '['*2000+']'*2000,
                json.dumps(base).replace('"uid": 997', '"uid": 0, "uid": 997'),
                json.dumps(base).replace('"regular": true', '"regular": false, "regular": true'),
                json.dumps(base).replace('"secret": "'+secret+'"', '"secret": NaN'),
                json.dumps(base)+'\n'+secret, json.dumps(base)+json.dumps(base),
                json.dumps(dict(base, secret='非'*(namespace['RECEIPT_LIMIT']//3)))):
        rejects(lambda: loaded(raw), 'probe_receipt_invalid')
    # Rejected receipts are diagnostic-only; every field remains typed/closed.
    project = namespace['project_probe_receipt']
    rejected = dict(base, review_body_key='rejected')
    for field, value in [('schema_version', secret), ('classification', secret), ('consumer', secret),
                         ('uid', True), ('uid', -1), ('uid', uid+1), ('review_body_key', secret),
                         ('workspace_credential', secret), ('direct_root_key_access', 'allowed'), ('systemd_credentials', secret),
                         ('review_body_credential', None)]:
        require(project('xinsight.service', dict(rejected, **{field:value}), uid) == {'status':'invalid'}, 'invalid rejected receipt accepted')
    for field, value in [('form', secret), ('mode', secret), ('mode', '9999'), ('uid', True), ('gid', -1),
                         ('links', 2**40), ('regular', secret), ('read_only', 1)]:
        require(project('xinsight.service', dict(rejected, review_body_credential=dict(metadata, **{field:value})), uid) == {'status':'invalid'},
                'invalid rejected permission metadata accepted')
    require(project('xinsight.service', dict(rejected, review_body_credential={'form':'unavailable'}), uid)['status'] == 'present',
            'actual unavailable rejected metadata form lost')
    for euid, environment, argv in [(0, {}, ['probe', 'xinsight.service']),
                                     (501, {'GITHUB_ACTIONS':'true', 'XINSIGHT_CI_EPHEMERAL_HOST':'1'}, ['probe', 'xinsight.service']),
                                     (0, {'GITHUB_ACTIONS':'true', 'XINSIGHT_CI_EPHEMERAL_HOST':'1', 'XINSIGHTCTL_TEST_ROOT':secret}, ['probe', 'xinsight.service']),
                                     (0, {'GITHUB_ACTIONS':'true', 'XINSIGHT_CI_EPHEMERAL_HOST':'1'}, ['probe'])]:
        with patch('os.geteuid', return_value=euid), patch.dict(os.environ, environment, clear=True), patch('sys.argv', argv), patch('sys.stderr', new_callable=io.StringIO) as errors, patch('sys.stdout', new_callable=io.StringIO) as output:
            try:
                exec(compile(match[1], 'managed namespace probe context', 'exec'), {'__name__':'__main__'})
            except SystemExit as error:
                require(error.code == 1 and not output.getvalue() and secret not in errors.getvalue() and
                        json.loads(errors.getvalue())['category'] == 'probe_orchestrator_context_invalid', 'orchestrator context failed open or leaked input')
            else:
                raise AssertionError('invalid orchestrator context accepted')
    for unit in namespace['CONSUMERS']:
        # Run the actual __main__ error serialization while substituting only run_probe.
        code = match[1][:match[1].index("if __name__ == '__main__':")]
        diagnostic = {'__name__':'__main__'}
        exec(compile(code, 'managed probe diagnostic definitions', 'exec'), diagnostic)
        def failed_probe(consumer):
            raise diagnostic['ProbeFailure']('live_process_identity_invalid', checks={'uid_matches':False, 'secret':secret},
                                             executable_kind='systemd_startup', stage='before_probe_identity')
        diagnostic['run_probe'] = failed_probe
        with patch('os.geteuid', return_value=0), patch.dict(os.environ, {'GITHUB_ACTIONS':'true', 'XINSIGHT_CI_EPHEMERAL_HOST':'1'}, clear=True), patch('sys.argv', ['probe',unit]), patch('sys.stderr', new_callable=io.StringIO) as errors, patch('sys.stdout', new_callable=io.StringIO) as output:
            try:
                exec(compile(match[1][match[1].index("if __name__ == '__main__':"):], 'managed probe diagnostic main', 'exec'), diagnostic)
            except SystemExit as error:
                result = json.loads(errors.getvalue())
                require(error.code == 1 and result['consumer'] == unit and result['stage'] == 'before_probe_identity' and
                        result['process_checks'] == {'uid_matches':False} and result['executable_kind'] == 'systemd_startup' and
                        secret not in errors.getvalue() and not output.getvalue(), 'actual failure serialization lost consumer/phase or leaked sensitive data')
            else:
                raise AssertionError('invalid process identity was accepted')
    # The real subprocess wrapper receives only fixed argv, no inherited env;
    # oversized output is killed in its own process group, never a real service.
    class Process:
        pid = 123456
        def __init__(self, data): self.stdout = io.BytesIO(data)
        def wait(self, timeout=None): return 0
        def poll(self): return 0
    wrapper_namespace = {'__name__': 'offline_namespace_probe_wrapper_test'}
    exec(compile(match[1], 'managed namespace probe', 'exec'), wrapper_namespace)
    for output, truncated in ((b'ok', False), (b'x'*(namespace['LIMIT']+1), True)):
        with patch.object(wrapper_namespace['subprocess'], 'Popen', return_value=Process(output)) as spawn, patch.object(wrapper_namespace['os'], 'killpg') as kill:
            raw, code, observed = wrapper_namespace['bounded_probe_command'](['/fixed-probe'], (10,))
            require(observed == truncated and len(raw) <= namespace['LIMIT'] and code == 0, 'probe output cap lost')
            require(spawn.call_args.args[0][:3] == ['/usr/bin/timeout', '--kill-after=1s', '4s'] and
                    spawn.call_args.kwargs['env'] == {} and spawn.call_args.kwargs['pass_fds'] == (10,) and
                    spawn.call_args.kwargs['start_new_session'], 'probe subprocess inherits environment or lacks bounded execution')
            require(kill.called == truncated, 'probe killed outside oversize process group condition')
    with patch.object(wrapper_namespace['subprocess'], 'Popen', return_value=Process(b'\xff')):
        try:
            wrapper_namespace['bounded_probe_command'](['/fixed-probe'], (10,))
        except UnicodeDecodeError:
            pass
        else:
            raise AssertionError('invalid UTF-8 stdout was replaced and accepted')


def check_managed_loaded_readiness() -> None:
    scripts = WORKFLOW.parent.parent.parent / 'scripts'
    qualification = (scripts/'qualify-ubuntu22-managed-fpv11.sh').read_text()
    probe = (scripts/'ci-managed-source-credential-proof.go').read_text()
    host = (scripts.parent/'internal/hostctl/host.go').read_text()
    attempts = int(re.search(r'activationProofAttempts\s*=\s*([0-9]+)', host)[1])
    interval = int(re.search(r'activationProofInterval\s*=\s*([0-9]+)\s*\*\s*time.Millisecond', host)[1])
    budget = int(re.search(r'context.WithTimeout\(context.Background\(\), ([0-9]+)\*time.Second\)', probe)[1])
    match = re.search(r'(func waitLoadedConsumers\(ctx context.Context, check func\(context.Context\) error\) error \{\n.*?^\})', probe, re.M | re.S)
    require(match is not None, 'original CI loaded-proof bounded wait missing')
    wait_interval = int(re.search(r'time.NewTicker\(([0-9]+) \* time.Millisecond\)', match[1])[1])
    require(wait_interval == interval and budget*1000 == (attempts-1)*interval == 10000,
            'CI loaded wait drifted from existing formal 10s/500ms boundary')
    loaded = probe[probe.index('if len(os.Args) == 2 && (os.Args[1] == "--check-loaded"'):probe.index('if len(os.Args) == 4 && os.Args[1] == "--check-config"')]
    require('os.Args[1] == "--wait-loaded"' in loaded and 'waitLoadedConsumers(ctx, e.ReviewBodyLoaded)' in loaded and
            'err = e.ReviewBodyLoaded(context.Background())' in loaded and 'EnsureReviewBodyLoaded' not in probe and
            'panic(err)' not in loaded and 'loadedProofFailure(os.Args[1], err, waitContextErr)' in loaded and
            'loadedProofFailure(os.Args[1], errors.New("CI loaded proof engine unavailable"), nil)' in loaded,
            'wait bypasses original checker, mutates host, changes single post-check, or leaks errors')
    for begin, end in [('sudo systemctl restart xinsight.service xinsight-connector-controller.service', '# Exercise the formal private bundle command'),
                       ('sudo systemctl restart xinsight.service xinsight-source-facts.service', 'sudo stat -c')]:
        start = qualification.index(begin)
        phase = qualification[start:qualification.index(end, start)]
        ordered = ['--wait-loaded > /dev/null', 'run_credential_probe xinsight-source-facts.service',
                   'run_credential_probe xinsight.service', '--check-loaded > "$QUAL_ROOT/']
        require(all(phase.count(token) == 1 for token in ordered) and
                sorted(phase.index(token) for token in ordered) == [phase.index(token) for token in ordered],
                'initial/private-restored lifecycle lacks pre-readiness or original post-proof ordering')
    # Execute only the actual tiny Go standard-library wait helper. No Engine,
    # systemd, namespace, credential, HTTP or database code enters this fixture.
    formatter = re.search(r'(func loadedProofFailure\(mode string, err, contextErr error\) string \{\n.*?^\})', probe, re.M | re.S)
    require(formatter is not None, 'fixed loaded-proof failure projection missing')
    writer = re.search(r'(func writeProof\(proof map\[string\]any\) \{\n.*?^\})', probe, re.M | re.S)
    require(writer is not None and 'json.NewEncoder(os.Stdout).Encode(proof)' in writer[1] and
            all(retired not in probe for retired in ('/var/lib/x-insight/ci-managed-proof', 'os.CreateTemp(', 'os.Rename(', 'receipt :=', 'writeProof(receipt,')),
            'Go receipt producer retained writable host path or lost stdout output')
    harness = '''package main
import ("context"; "encoding/json"; "errors"; "fmt"; "os"; "strings"; "time")
''' + formatter[1] + '\n' + match[1] + '\n' + writer[1] + '''
func require(ok bool) { if !ok { fmt.Println("loaded_wait_regression=failed"); os.Exit(1) } }
func main() {
    proof := map[string]any{"schema_version":"xinsight.ci-managed-credential-proof.v1", "classification":"ci_linux_synthetic_no_source_call",
        "consumer":"xinsight.service", "uid":997, "review_body_key":"loaded", "workspace_credential":"loaded",
        "direct_root_key_access":"rejected", "systemd_credentials":"loaded",
        "review_body_credential":map[string]any{"form":"service_owned_0400_native_candidate", "regular":true, "read_only":true,
            "uid":997, "gid":0, "mode":"0400", "links":1}}
    writeProof(proof)
    proof["consumer"] = "xinsight-source-facts.service"
    proof["root_owned_config"], proof["original_indexed_keys"], proof["managed_partition_keys"] = true, 9, 1
    writeProof(proof)
    // Exercise the actual writer's finite failure without any host file access.
    original := os.Stdout
    read, closed, err := os.Pipe(); require(err == nil)
    require(closed.Close() == nil); os.Stdout = closed
    panicked := false
    func() { defer func() { panicked = recover() == "consumer receipt write failed" }(); writeProof(proof) }()
    os.Stdout = original; require(read.Close() == nil && panicked)
    calls := 0
    err = waitLoadedConsumers(context.Background(), func(context.Context) error { calls++; return nil })
    require(err == nil && calls == 1)
    pending := errors.New("synthetic-private-proof-error-do-not-export")
    ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
    calls = 0
    err = waitLoadedConsumers(ctx, func(context.Context) error { calls++; if calls < 2 { return pending }; return nil })
    cancel()
    require(err == nil && calls == 2)
    ctx, cancel = context.WithTimeout(context.Background(), 35*time.Millisecond)
    calls = 0
    err = waitLoadedConsumers(ctx, func(context.Context) error { calls++; return pending })
    require(err == pending && calls == 1 && ctx.Err() == context.DeadlineExceeded)
    cancel()
    ctx, cancel = context.WithCancel(context.Background()); cancel()
    calls = 0
    err = waitLoadedConsumers(ctx, func(context.Context) error { calls++; return nil })
    require(err == context.Canceled && calls == 0)
    ctx, cancel = context.WithCancel(context.Background())
    err = waitLoadedConsumers(ctx, func(context.Context) error { cancel(); return pending })
    require(err == pending)
    ctx, cancel = context.WithCancel(context.Background())
    err = waitLoadedConsumers(ctx, func(context.Context) error { cancel(); return nil })
    require(err == context.Canceled)
    fixed := map[string]string{
        "body consumer is not active":"inactive", "body consumer is not running":"not_running",
        "body consumer proof is stale":"stale_proof", "body consumer loaded a different key":"key_mismatch",
        "reader body capability pending":"reader_pending", "source body capability pending":"worker_pending",
        "body consumer restarted during readback":"restarted", "restore_original_review_body_key_required":"binding_missing",
        "CI loaded proof engine unavailable":"engine_unavailable",
    }
    for text, category := range fixed {
        require(loadedProofFailure("--check-loaded", errors.New(text), nil) == "actual_loaded_custody_proof=failed stage=check termination=failed reason="+category)
        require(loadedProofFailure("--wait-loaded", errors.New(text), context.DeadlineExceeded) == "actual_loaded_custody_proof=failed stage=wait termination=deadline reason="+category)
        require(strings.HasSuffix(loadedProofFailure("--wait-loaded", errors.New(text+" secret-DSN-token-key-body"), context.Canceled), "termination=cancelled reason=unknown"))
    }
    secret := "secret-DSN-token-key-body"
    require(loadedProofFailure(secret, errors.New(secret), nil) == "actual_loaded_custody_proof=failed stage=check termination=failed reason=unknown")
    require(loadedProofFailure("--wait-loaded", fmt.Errorf("%s: %w", secret, context.DeadlineExceeded), nil) == "actual_loaded_custody_proof=failed stage=wait termination=deadline reason=unknown")
    fmt.Println("loaded_wait_regression=pass")
}
'''
    with tempfile.TemporaryDirectory(prefix='xi-ci-loaded-wait-test-') as directory:
        path = Path(directory)/'main.go'; path.write_text(harness)
        result = subprocess.run(['go', 'run', str(path)], cwd=directory,
                                env=dict(os.environ, GOTOOLCHAIN='local', GOPROXY='off', GOSUMDB='off'),
                                capture_output=True, text=True, timeout=30)
        lines = result.stdout.splitlines()
        require(result.returncode == 0 and len(lines) == 3 and lines[-1] == 'loaded_wait_regression=pass' and
                not result.stderr, 'actual CI Go stdout writer or wait helper regression failed')
        parser = {'__name__':'offline_go_stdout_receipt_test'}
        exec(compile(re.search(r"<<'PYPROBE'\n(.*?)\nPYPROBE", qualification, re.S)[1], 'actual Go receipt projection', 'exec'), parser)
        for raw, unit in zip(lines[:2], parser['CONSUMERS']):
            proof = parser['loaded_receipt'](raw, unit, 997)
            require(proof['consumer'] == unit and proof['uid'] == 997 and 'probe_context' not in proof,
                    'actual Go stdout writer/strict Python projector roundtrip changed fixed receipt')


def check_managed_failure_diagnostic() -> None:
    qualification = (WORKFLOW.parent.parent.parent / "scripts/qualify-ubuntu22-managed-fpv11.sh").read_text()
    match = re.search(r"<<'PYDIAG'\n(.*?)\nPYDIAG", qualification, re.S)
    require(match is not None, "managed diagnostic formatter missing")
    namespace = {"__name__": "offline_diagnostic_test"}
    exec(compile(match[1], "managed failure diagnostic", "exec"), namespace)
    # Exercise the real formatter without systemd, service actions or credentials.
    secret = "secret-marker-DSN-token-key-body"
    properties = ("ActiveState=failed\nSubState=auto-restart\nResult=exit-code\nExecMainCode=1\nExecMainStatus=1\nMainPID=0\nNRestarts=3\n"
                  "Environment=" + secret + "\nExecStart=" + secret + "\n"
                  "ExecStartPre={ argv[]=/usr/bin/env -i /opt/x-insight/current/bin/xinsightctl review-body-config validate " + secret + "; code=exited ; status=1 }\n"
                  "ExecStartPost={ argv[]=/usr/local/bin/xinsight-ci-managed-credential-proof --reader ; code=exited ; status=2 }\n")
    journal = "\n".join(json.dumps({"MESSAGE": message, "extra": secret}) for message in (
        "xinsight-server: review body key file is invalid",
        "xinsight-server: failed to connect to postgres://" + secret + ": connection refused",
        "xinsight-server: " + secret,
        "xinsight-launcher: " + secret,
        "xinsightctl: " + secret,
        "xinsight.service: Failed at step CREDENTIALS spawning " + secret,
        "Main process exited, code=exited, status=1/FAILURE",
        "Failed with result 'exit-code'.",
        "\t/home/runner/work/x-insight/x-insight/scripts/ci-managed-source-credential-proof.go:109 +0xabcdef",
        secret,
    ))
    calls = []
    def command(args):
        calls.append(args)
        return (properties if args[0] == "systemctl" else journal), 0, False
    namespace['bounded_command'] = command
    result = namespace['service_diagnostic']('xinsight.service', '1791016185')
    encoded = json.dumps(result)
    require(secret not in encoded and 'postgres://' not in encoded and 'Environment' not in encoded,
            "managed diagnostic exported raw sensitive data")
    require(result['properties']['ExecMainStatus'] == 1 and result['properties']['startup_command_exits'] ==
            [{'validator': 'review-body-config', 'code': 'exited', 'status': 1}], "actual systemd exit status lost")
    require(result['properties']['post_start_command_exits'] ==
            [{'validator': 'credential-probe-reader', 'code': 'exited', 'status': 2}] and 'probe_receipt' not in result,
            "actual control exit lost or retired disk receipt still read")
    require(result['probe_callsites'] == [109] and 'abcdef' not in encoded and '/home/runner' not in encoded,
            'finite probe callsite missing or raw stack leaked')
    for message in ('\t/private/'+secret+'/other.go:109 +0xabcdef',
                    '\t/private/ci-managed-source-credential-proof.go:1234567 +0xabcdef',
                    '\t/private/ci-managed-source-credential-proof.go:109 '+secret,
                    '\t/private/ci-managed-source-credential-proof.go:0 +0xabcdef',
                    '\t/private/ci-managed-source-credential-proof.go:109 +0x'+'a'*17):
        require(namespace['probe_callsite'](message) is None, 'unknown or unbounded stack accepted')
    probe_source = (WORKFLOW.parent.parent.parent / 'scripts/ci-managed-source-credential-proof.go').read_text()
    require(set(namespace['PROBE_PANICS']) == set(re.findall(r'panic\("([^"\n]*)"\)', probe_source)),
            'diagnostic probe categories do not match current fixed producer panics')
    for message, category in namespace['PROBE_PANICS'].items():
        require(namespace['error_category']('panic: '+message) == category and
                namespace['error_category']('xinsight.service: panic: '+message) == category and
                namespace['error_category']('panic: '+message+' '+secret) is None,
                'fixed probe error mapping is missing or echoes unknown exception')
    require(namespace['error_category']('panic: '+secret) is None, 'unknown probe exception accepted')
    require(result['error_categories']['review_body_key_invalid'] == 1 and result['error_categories']['postgres_connection_failed'] == 1 and
            result['error_categories']['systemd_step_credentials_failed'] == 1 and result['unclassified_messages'] == 1,
            "managed diagnostic categories did not reflect supplied service failures")
    require(namespace['selected_properties']('ActiveState='+secret+'\nExecMainStatus='+secret)['ActiveState'] == 'unavailable' and
            namespace['selected_properties']('ExecMainStatus='+secret)['ExecMainStatus'] is None,
            "unexpected property value exported")
    require(calls[0][:3] == ['systemctl', 'show', 'xinsight.service'] and '--lines=40' in calls[1] and '--since=@1791016185' in calls[1],
            "diagnostic lost exact unit/current qualification/bounded journal inputs")
    require(namespace['UNITS'] == ('xinsight.service', 'xinsight-connector-controller.service', 'xinsight-source-facts.service') and namespace['LIMIT'] == 128*1024,
            "diagnostic expanded beyond original exact services")
    namespace['bounded_command'] = lambda args: (properties if args[0] == 'systemctl' else journal+'\n{'+secret, 124, True)
    partial = namespace['service_diagnostic']('xinsight.service', '1791016185')
    require(partial['journal_truncated'] and partial['journal_command_exit'] == 124 and partial['unclassified_messages'] == 2 and
            secret not in json.dumps(partial), "truncated diagnostic lost explicit status or leaked partial message")
    require(qualification.index("if \"$CHECK_ONLY\"; then") < qualification.index('trap qualification_exit EXIT'),
            "check-inputs can invoke failure diagnostic")
    require(all(retired not in match[1] for retired in ('PROBE_RECEIPTS', 'def probe_receipt(', 'os.open(', 'project_probe_receipt')),
            'service diagnostic retained consumer disk receipt channel')
    start = qualification.index('QUAL_STAGE=predecessor_install\n')
    end = qualification.index('trap qualification_exit EXIT\n', start) + len('trap qualification_exit EXIT\n')
    trap = qualification[start:end]
    with tempfile.TemporaryDirectory(prefix='xi-managed-diagnostic-test-') as directory:
        root = Path(directory)
        sudo = root / 'sudo'
        cat = root / 'cat'
        # Exercise the original shell trap. The stub never invokes Python,
        # systemd, credentials or services, even when diagnostic capture fails.
        for diagnostic_exit, qualification_exit, cat_exit in ((0, 0, 0), (0, 17, 0), (65, 17, 0), (0, 17, 66)):
            sudo.write_text('#!/bin/sh\nprintf \'{"schema_version":"offline-trap-proof"}\\n\'\nexit '+str(diagnostic_exit)+'\n')
            sudo.chmod(0o700)
            cat.write_text('#!/bin/sh\n'+('exit '+str(cat_exit)+'\n' if cat_exit else 'exec /bin/cat "$@"\n'))
            cat.chmod(0o700)
            output = root / f'{diagnostic_exit}-{qualification_exit}-{cat_exit}'
            output.mkdir(mode=0o700)
            script = 'set -euo pipefail\nQUAL_ROOT="$1"\n'+trap+'exit '+str(qualification_exit)+'\n'
            run = subprocess.run(['bash', '-c', script, 'trap-proof', str(output)],
                                 env=dict(os.environ, PATH=str(root)+':'+os.environ['PATH']),
                                 capture_output=True, text=True, timeout=10)
            require(run.returncode == qualification_exit, 'diagnostic replaced original qualification exit status')
            diagnostic = output / 'service-failure-diagnostic.json'
            if qualification_exit == 0:
                require(not diagnostic.exists(), 'successful qualification collected a failure diagnostic')
            elif diagnostic_exit:
                receipt = json.loads(diagnostic.read_text())
                require(receipt['diagnostic_exit'] == 65 and receipt['qualification_exit'] == 17 and receipt['diagnostic'] == 'unavailable',
                        'failed diagnostic did not preserve explicit qualification failure')
            else:
                require(json.loads(diagnostic.read_text())['schema_version'] == 'offline-trap-proof', 'original failure trap did not collect diagnostic')


def check_managed_restored_snapshot() -> None:
    qualification = (WORKFLOW.parent.parent.parent / "scripts/qualify-ubuntu22-managed-fpv11.sh").read_text()
    match = re.search(r"<<'PYRESTORED'\n(.*?)\nPYRESTORED", qualification, re.S)
    require(match is not None, "managed restored snapshot verifier missing")
    namespace = {"__name__": "offline_restored_snapshot_test"}
    exec(compile(match[1], "managed restored snapshot", "exec"), namespace)
    verify = namespace['verify_restored_snapshot']
    before = {'streams': [['stream', 'instance', '2', '86', 'profile', '1', 'active', 'cursor', 'hwm']],
              'content': ['instance', '2', '86', 'credential', 'hash', '2', '1', 'active', 'cursor', '3', '3'],
              'manifests': [[3, 'fact', 'digest', 'reference']], 'receipts': 1, 'evidence': 2}
    after = copy.deepcopy(before)
    after['streams'][0][6] = after['content'][7] = 'paused'
    verify(before, after)
    require(before['streams'][0][6] == before['content'][7] == 'active', "verifier modified input snapshot")
    verify(after, after)  # A later private restore keeps already-paused authority paused.
    revoked = copy.deepcopy(before)
    revoked['streams'][0][6] = revoked['content'][7] = 'revoked'
    verify(revoked, revoked)
    # Every original recorded column except status must retain its exact value.
    bad_snapshots = []
    for field, row, status in [('streams', before['streams'][0], 6), ('content', before['content'], 7)]:
        for index in range(len(row)):
            bad = copy.deepcopy(after)
            target = bad['streams'][0] if field == 'streams' else bad['content']
            target[index] = 'active' if index == status else 'unexpected'
            bad_snapshots.append(bad)
    for field in ('manifests', 'receipts', 'evidence', 'streams', 'content'):
        bad = copy.deepcopy(after)
        bad[field] = None
        bad_snapshots.append(bad)
    bad = copy.deepcopy(after)
    bad['unexpected'] = True
    bad_snapshots.append(bad)
    for bad in bad_snapshots:
        try:
            verify(before, bad)
        except (ValueError, TypeError, IndexError, KeyError):
            continue
        raise AssertionError('restored data change or missing pause was accepted')
    try:
        verify(revoked, after)
    except ValueError:
        pass
    else:
        raise AssertionError('restored revoked authority changed')
    require(qualification.count('; compare_restored_snapshot ') == 2,
            'both mutating restores require exact preservation and paused authority')
    for comparison in ('cmp "$QUAL_ROOT/before.json" "$QUAL_ROOT/after-restore-check.json"',
                       'cmp "$QUAL_ROOT/before-private-backup-check.json" "$QUAL_ROOT/after-private-restore-check.json"'):
        require(comparison in qualification, 'restore check lost no-mutation assertion')


def expect_rejected(source: str, old: str, new: str) -> None:
    require(old in source, f"negative fixture cannot find: {old}")
    try:
        check(source.replace(old, new, 1))
    except ValueError:
        return
    raise AssertionError(f"negative fixture was accepted: {old}")


def check_capless_root_references(source: str) -> None:
    probe = step(job(source, "backend_validation"), "Verify actual capless root host reference writers")
    require("if:" not in probe, "capless regression must execute in the existing backend job")
    for required in (
        "go test -c", "sudo mktemp -d /run/xi-hostctl-credential-XXXXXXXX",
        'sudo install -o 0 -g 0 -m 0700', '"$capless_root/hostctl.test"',
        'sudo install -o "$(id -u)"', '--reuid=0', '--regid="$(id -g)"',
        '--clear-groups', '--no-new-privs', '--inh-caps=-all', '--ambient-caps=-all', '--bounding-set=-all',
        'env -i PATH=/usr/bin:/bin', 'XINSIGHT_CAPLESS_REFERENCE_TEST=1',
        "-test.run='^TestCaplessRoot(ManagedReferenceWriters|WorkerLoadedProof)$'", '-test.count=1',
        "'legacy-worker',0o700,0o600", "'group-worker',0o750,0o640",
        "'systemd-analyze','condition'", "source unit rejects managed partitions",
    ):
        require(required in probe, f"capless Linux regression missing required boundary: {required}")
    require('/tmp/' not in probe and '--test-root' not in probe, "capless ownership check cannot use unsafe/test-root ancestry")



def check_installed_failure_diagnostic() -> None:
    qualification = WORKFLOW.parents[2] / 'scripts/qualify-ubuntu22-installed-joint.sh'
    contents = qualification.read_text()
    blocks = re.findall(r"<<'PY'\n(.*?)\nPY", contents, re.S)
    require(bool(blocks), 'installed qualification diagnostic block missing')
    tree = ast.parse(blocks[0])
    problem_filters = [node.comparators[0] for node in ast.walk(tree)
                       if isinstance(node, ast.Compare) and len(node.ops) == 1 and
                       isinstance(node.ops[0], ast.In) and isinstance(node.left, ast.Call) and
                       isinstance(node.left.func, ast.Attribute) and node.left.func.attr == 'get' and
                       len(node.left.args) == 1 and isinstance(node.left.args[0], ast.Constant) and
                       node.left.args[0].value == 'failure_problem_code']
    require(len(problem_filters) == 1, 'safe HTTP problem filter missing or duplicated')
    allowed = ast.literal_eval(problem_filters[0])
    for code in ('setup_unavailable', 'invalid_setup', 'setup_changed', 'recovery_denied', 'setup_not_found'):
        require(code in allowed, 'UR fixed error envelope cannot reach failure evidence: ' + code)
    functions = [node for node in tree.body
                 if isinstance(node, ast.FunctionDef) and
                 node.name in {'credential_failure_counts', 'setup_failure_counts'}]
    require(len(functions) == 2, 'controller diagnostic filter missing')
    namespace = {'re': re}
    # Execute the real pure filter, never the surrounding journal/systemctl code.
    exec(compile(ast.Module(body=functions, type_ignores=[]), str(qualification), 'exec'), namespace)
    classify = namespace['credential_failure_counts']
    client = 'connector credential client action=issue phase=dial category=connection_refused'
    recovery = 'connector credential recovery action=issue phase=store_callback category=database_sqlstate sqlstate=42501'
    require(classify([client, client, recovery]) == [
        {'boundary': 'client', 'action': 'issue', 'phase': 'dial',
         'category': 'connection_refused', 'sqlstate': 'none', 'count': 2},
        {'boundary': 'recovery', 'action': 'issue', 'phase': 'store_callback',
         'category': 'database_sqlstate', 'sqlstate': '42501', 'count': 1},
    ], 'fixed credential diagnostic fields were lost')
    for unsafe in (
        client + ' private-password', client + ' sqlstate=42501',
        client.replace('action=issue', 'action=secret'),
        client.replace('phase=dial', 'phase=store_callback'),
        client.replace('category=connection_refused', 'category=file_permission'),
        recovery.replace('action=issue', 'action=rotate'),
        recovery.replace('phase=store_callback', 'phase=read'),
        recovery.replace('category=database_sqlstate', 'category=connection_refused'),
        recovery.replace('sqlstate=42501', 'sqlstate=none'),
        recovery.replace('sqlstate=42501', 'sqlstate=SECRET'),
        recovery.replace(' sqlstate=42501', ''),
    ):
        require(classify([unsafe]) == [], 'untrusted or cross-boundary journal fields accepted')

    classify = namespace['setup_failure_counts']
    client = 'connector setup client action=status phase=dial category=connection_refused'
    owner = 'connector setup status action=status phase=read_record category=database_sqlstate sqlstate=42501'
    require(classify([client, owner, owner]) == [
        {'boundary': 'client', 'action': 'status', 'phase': 'dial',
         'category': 'connection_refused', 'sqlstate': 'none', 'count': 1},
        {'boundary': 'status', 'action': 'status', 'phase': 'read_record',
         'category': 'database_sqlstate', 'sqlstate': '42501', 'count': 2},
    ], 'setup failure boundary and safe SQLSTATE were lost')
    for unsafe in (
        client + ' private-password', client + ' sqlstate=none',
        client.replace('action=status', 'action=issue'),
        client.replace('phase=dial', 'phase=read_record'),
        client.replace('category=connection_refused', 'category=database_sqlstate'),
        owner.replace('phase=read_record', 'phase=dial'),
        owner.replace('category=database_sqlstate', 'category=controller_unavailable'),
        owner.replace('sqlstate=42501', 'sqlstate=none'),
        owner.replace('sqlstate=42501', 'sqlstate=SECRET'),
        owner.replace(' sqlstate=42501', ''),
    ):
        require(classify([unsafe]) == [], 'private or cross-boundary setup diagnostic accepted')


def check_installed_result_contract() -> None:
    qualification = WORKFLOW.parents[2] / 'scripts/qualify-ubuntu22-installed-joint.sh'
    contents = qualification.read_text()
    blocks = re.findall(r"<<'PY'\n(.*?)\nPY", contents, re.S)
    nodes = [node for block in blocks for node in ast.parse(block).body
             if isinstance(node, ast.FunctionDef) and node.name == 'installed_joint_passed']
    require(len(nodes) == 1, 'installed result evaluator missing or duplicated')
    namespace = {'re': re}
    exec(compile(ast.Module(body=nodes, type_ignores=[]), str(qualification), 'exec'), namespace)
    evaluate = namespace['installed_joint_passed']
    require('if not installed_joint_passed(result.returncode, mode, receipts):' in contents,
            'qualification must use the tested completion evaluator')
    stages = ('service_restart', 'credential_lifecycle_baseline', 'credential_continuous_rotation',
              'credential_lost_prepare', 'credential_lost_confirm', 'credential_offline_expiry',
              'credential_expired_prepared', 'credential_revocation_boundary')
    def row(stage, kind='stage', state='passed', category=''):
        return f'xinsight.ci-installed-joint-{kind}.v1/{stage}/{state}/1/{category}'
    for mode, prestate in (('new_install', 'future_project'), ('adopt_existing', 'preserved_history')):
        receipts = [row(prestate)] + [row(stage) for stage in stages]
        final = row('credential_revocation_boundary', 'result', category='none')
        complete = receipts + [final]
        require(evaluate(0, mode, complete), 'complete lifecycle result rejected: ' + mode)
        require(evaluate(0, mode, receipts + [row('additional_check'),
                row('additional_check', 'result', category='none')]),
                'completion must not depend on the last business stage name')
        for bad in ([], receipts, complete + [final], [final] + receipts,
                    [row(prestate), row(prestate, 'result', category='none')],
                    receipts + [row('credential_revocation_boundary', 'result', 'failed', 'denied')],
                    receipts + [final.replace('/none', '/private_error')],
                    receipts + [row('unexpected', state='failed', category='denied'), final]):
            require(not evaluate(0, mode, bad), 'partial, failed or ambiguous lifecycle accepted')
        for missing in receipts:
            require(not evaluate(0, mode, [s for s in complete if s != missing]),
                    'missing required lifecycle phase accepted: ' + missing)
        require(not evaluate(1, mode, complete), 'failed child process accepted')
        require(not evaluate(0, 'unknown', complete), 'unknown installation mode accepted')
    prestate_receipts = [row('future_project'), row('service_restart'), row('upgrade_authority'), row('formal_upgrade'),
                         row('upgrade_continuity'), row('upgrade_completed')]
    prestate_complete = prestate_receipts + [row('upgrade_completed', 'result', category='none')]
    require(evaluate(0, 'fpv17_upgrade_prestate', prestate_complete), 'healthy FPV17 prestate rejected')
    for bad in (prestate_receipts, prestate_complete[:-2] + prestate_complete[-1:],
                prestate_complete + [prestate_complete[-1]]):
        require(not evaluate(0, 'fpv17_upgrade_prestate', bad), 'partial FPV17 prestate accepted')
    require(not evaluate(1, 'fpv17_upgrade_prestate', prestate_complete), 'failed FPV17 child accepted')


def check_fpv17_upgrade_lane() -> None:
    root = WORKFLOW.parents[2]
    predecessor = (root / 'scripts/prepare-ci-fpv17-predecessor.sh').read_text()
    qualifier = (root / 'scripts/qualify-ubuntu22-fpv17-upgrade.sh').read_text()
    snapshot = (root / 'scripts/ci-fpv17-upgrade-snapshot.py').read_text()
    joint = (root / 'scripts/qualify-ubuntu22-installed-joint.sh').read_text()
    driver = (root / 'scripts/ci-fpv15-installed-joint/main.go').read_text()
    for required in ('164582cab0e7f169c265c2494d04920c61af833d',
                     "('0.2.0-dev.fpv17','45')", "('0.2.0-dev.fpv19','45')", 'xinsight.ci-fpv17-predecessor.v1',
                     'ci-trust', 'old_source'):
        require(required in predecessor, 'FPV17 CI-only predecessor binding missing: ' + required)
    for required in ('upgrade --check --artifact', 'upgrade --artifact',
                     "before==after", "after['files']==before['files']",
                     'active_workers_restored_by_upgrade',
                     'before-upgrade-invocation', 'qualification-failure.json',
                     "after['server_invocation']!=before['server_invocation']"):
        require(required in qualifier, 'same-schema installed upgrade proof missing: ' + required)
    for required in ('review-body.key', 'schema_migrations', 'source_fact_streams',
                     'source_content_collections', 'review_body_operations',
                     'workbench_access_grants', 'server_exe', 'worker_exes'):
        require(required in snapshot, 'FPV17 snapshot omits a required boundary: ' + required)
    require("'upgrade_authority'" in joint and "'formal_upgrade'" in joint and "'upgrade_continuity'" in joint and
            'fpv17-upgrade-request.json' in joint and
            'readFPV17UpgradeRequest(input.Installed.XIAdminDSNFile)' in driver and
            'd.establishUpgradeAuthority(ctx)' in driver and
            'd.lifecycleFixture(ctx, "publish", nil, "")' in driver and
            'd.awaitEconomicFeedRead(ctx, feedSince)' in driver and
            'fpv17-upgrade-private.log' in driver,
            'upgrade must retain live UR source through new content and feed read')
    require('e118b5754ee396b21be32db4b47f90d8252cd585ee6e673ca2e91f395ed098ee' not in predecessor,
            'CI-only predecessor must not impersonate Target r2 bytes')


if __name__ == "__main__":
    source = WORKFLOW.read_text(encoding="utf-8")
    check(source)
    check_installed_failure_diagnostic()
    check_installed_result_contract()
    check_fpv17_upgrade_lane()
    check_capless_root_references(source)
    for required in ('--bounding-set=-all', 'XINSIGHT_CAPLESS_REFERENCE_TEST=1', '-test.count=1'):
        try:
            check_capless_root_references(source.replace(required, '', 1))
        except ValueError:
            continue
        raise AssertionError(f'capless regression disabled without rejection: {required}')
    check_managed_namespace_probe()
    check_managed_loaded_readiness()
    check_direct_fpv9(source)
    check_managed_fpv11(source)
    check_managed_failure_diagnostic()
    check_managed_restored_snapshot()
    check_managed_private_backup_path()
    for old, new in [("      - direct_fpv9_upgrade\n", ""), ("XI_FPV9_REPRO_DIR:", "UNUSED_FPV9_REPRO_DIR:"), ("xinsight-direct-fpv9-qualification/qualification-result.json", "xinsight-direct-fpv9-qualification/*.json")]:
        require(old in source, f"missing test mutation target: {old}")
        try:
            check_direct_fpv9(source.replace(old, new, 1))
        except ValueError:
            continue
        raise AssertionError(f"unsafe qualification wiring accepted: {old}")
    for old, new in [("      - managed_fpv11_upgrade\n", ""), ("MANAGED_FPV11_REQUIRED:", "UNUSED_MANAGED_REQUIRED:"),
                     ('--check-source-directory fpv11 "$GITHUB_WORKSPACE/compatibility/fpv11-linux-amd64/source"', 'true'),
                     ('--build fpv11', '--build fpv14'),
                     ('--verify fpv11 "$old_source"', '--verify fpv14 "$old_source"'),
                     ('c["product_version"]=="0.2.0-dev.fpv11"', 'c["product_version"]=="unqualified"'),
                     ('c["database_schema"]=={"current":"43","min":"43","max":"43"}', 'True'),
                     ('test "$old_commit" = 9c58e07f011918c4a0aed2f876ce4baa9fb12b5e', 'test "$old_commit" = unpinned'),
                     ('xinsight-managed-fpv11-qualification/linux-credential-proof.json', 'xinsight-managed-fpv11-qualification/*.json'),
                     ('xinsight-managed-fpv11-qualification/service-failure-diagnostic.json', 'xinsight-managed-fpv11-qualification/service-failure.txt')]:
        require(old in source, f"missing managed mutation target: {old}")
        try:
            check_managed_fpv11(source.replace(old, new, 1))
        except ValueError:
            continue
        raise AssertionError(f"unsafe managed qualification accepted: {old}")
    qualification = (WORKFLOW.parents[2] / 'scripts/qualify-ubuntu22-managed-fpv11.sh').read_text()
    for old, new in [('--check-built-source-directory fpv11 "$OLD_SOURCE"', 'true'),
                     ('--check-built-source-directory fpv11', '--check-built-source-directory fpv14')]:
        require(old in qualification, "missing managed source-admission mutation target")
        try:
            check_managed_fpv11(source, qualification.replace(old, new, 1))
        except ValueError:
            continue
        raise AssertionError("unadmitted managed predecessor source accepted")
    reproduction = REPRODUCTION.read_text(encoding="utf-8")
    old_digest = "80573c95149f5b9827b06f264f8d70e436a29ea1d8e93ddb6f1f46a75f4866d7"
    require(old_digest in reproduction, "missing exact reproduction digest")
    try:
        check_direct_fpv9(source, reproduction.replace(old_digest, "unpinned"))
    except ValueError:
        pass
    else:
        raise AssertionError("unpinned exact predecessor reproduction accepted")
    expect_rejected(source, 'xinsight-signed-a" --profile=development-d', 'xinsight-signed-a"')
    expect_rejected(source, 'npm install --global "$(node -p "require(\'./package.json\').packageManager")"', 'npm --version')
    expect_rejected(source, 'bash scripts/verify-protected-runtime.sh', 'true')
    expect_rejected(source, "XINSIGHT_REQUIRE_POSTGRES_TESTS: '1'", "XINSIGHT_REQUIRE_POSTGRES_TESTS: '0'")
    expect_rejected(source, 'lifecycle: [new_install, adopt_existing, fpv17_upgrade]', 'lifecycle: [new_install, adopt_existing]')
    expect_rejected(source, "permissions:\n  contents: read", "permissions:\n  contents: write")
    expect_rejected(source, "GH_TOKEN: ${{ github.token }}", "GH_TOKEN: ${{ secrets.MAINTAINER_TOKEN }}")
    expect_rejected(source, "ref: db10730d64a9302887ad59c53921e98c757927e7", "ref: main")
    expect_rejected(source, "needs.changes.outputs.current_release_required == 'true'))", "needs.changes.outputs.current_release_required == 'true' && github.event.pull_request.head.repo.full_name == github.repository))")
    expect_rejected(source, "p['old_source']+'/scripts/qualify-ubuntu22-fresh-install.sh'", "'scripts/qualify-ubuntu22-fresh-install.sh'")
    expect_rejected(source, f"XINSIGHT_RC13_COMMIT: {RC13_COMMIT}", "XINSIGHT_RC13_COMMIT: unpinned")
    expect_rejected(source, '--build rc13', '--build fpv11')
    expect_rejected(source, '--verify rc13 "$rc13_source"', '--verify fpv11 "$rc13_source"')
    expect_rejected(source, '"$GITHUB_WORKSPACE/compatibility/rc13-linux-amd64/source"', '"$GITHUB_WORKSPACE/compatibility/fpv11-linux-amd64/source"')
    expect_rejected(source, 'python3 scripts/reproduce-phase1-source.py', 'true')
    expect_rejected(source, 'sha256:3e3794338d690fe5b12de46600e17d455baeceab0b79300af260baf78d0346b1', 'sha256:unpinned')
    expect_rejected(source, '"${RUNNER_TEMP}/verified-rc13-payload" \\', '"${RUNNER_TEMP}/verified-signed-payload" \\')
    expect_rejected(source, '"${RUNNER_TEMP}/fresh-install-payload" \\\n            --profile=development-d', '"${RUNNER_TEMP}/fresh-install-payload"')
    print("current development-d, frozen rc13 and exact FPV9 CI wiring: pass")
