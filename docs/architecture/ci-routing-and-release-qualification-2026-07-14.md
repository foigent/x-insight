# CI Routing And Release Qualification Contract

Status: release-relevant PR/main, safe-docs-only PR/main and owner-supervised
source guard CI verified
Date: 2026-07-14
Owner: Codex
Scope: X-Insight only

## Decision

X-Insight uses one `CI` workflow with event- and path-aware job routing. Pull
requests prove that the proposed source is safe to merge. A release-relevant
push to `main` proves that the exact merged commit can be packaged, upgraded,
recovered and freshly installed. The same expensive suite is not repeated on
both events.

The existing protected check name `Backend Tests` remains stable. It becomes a
small aggregate gate over the event-specific jobs, so branch protection does
not need a risky transition and a docs-only pull request does not need a
PostgreSQL service merely to produce the required check.

## Route Matrix

| Event and change class | Frontend | Backend/API | Signed/legacy qualification | Fresh install | Protected aggregate gate |
|---|---:|---:|---:|---:|---:|
| Pull request, release-relevant | required | required | skipped | skipped | required |
| Pull request, safe docs only | skipped | skipped | skipped | skipped | required |
| `main` push, release-relevant | skipped | skipped | required | required | required |
| `main` push, safe docs only | skipped | skipped | skipped | skipped | required |

The aggregate gate fails if a required job was skipped or failed, or if a job
ran on an event where it was required to be skipped. A green workflow therefore
cannot be produced by an accidental all-skipped route.

## Conservative Path Boundary

Only these paths are `safe-docs-only`:

- `README.md`; and
- files under `docs/`, except `docs/manual/`.

`docs/manual/` is release-relevant because it generates the offline manual
embedded in the product. Workflow files, scripts, schemas, release metadata,
generated web assets and every product/test path are release-relevant. Empty or
unrecognized comparisons fail closed into the release-relevant route. Rename
detection is disabled for classification so both the removed and added path are
examined.

The workflow intentionally uses a job-level route rather than top-level
`paths-ignore`. A top-level skipped workflow would not publish the protected
check and could leave a pull request permanently pending.

## Release Qualification Boundary

The former standalone legacy-release job is folded into `Signed V2 Release
Qualification`. This keeps current legacy compatibility, signed double-build,
exact Phase 1 reproduction and legacy transition/recovery proof while avoiding
a second checkout, runner and Go cache lifecycle. `Fresh Install Qualification`
remains a separate clean Ubuntu VM because combining it with the upgrade VM
would weaken the clean-install claim.

The main-only signed/legacy job also owns the bounded supported-host crash
sample. On its disposable Ubuntu 22.04 VM, an external `systemctl` wrapper first
completes the real service stop, then blocks before `xinsightctl` can mark that
phase complete. The harness verifies the exact signed control binary plus its
isolated PID/PGID/session and sends `SIGKILL` to only that process group. It then
proves that the active journal is readable, no final receipt exists, `recover`
offers only the persisted actions, and `resume` completes the same operation and
restores readiness. The wrapper refuses use outside an explicitly disposable
GitHub Actions VM and no crash hook is compiled into the product binary.

This is deliberately layered with deterministic Local/PR tests that inject a
failure after every durable phase of install, adopt, upgrade, rollback and
restore. The single external `SIGKILL` sample proves real process death at one
representative after-action/before-journal-completion boundary; it does not
claim literal host power loss, a named Target or Production resilience.

The owner-supervised trial workflow must validate all of the following for the
exact current `main` commit:

1. the named source run is a successful `CI` push run on `main`;
2. `Backend Tests`, the aggregate contract gate, succeeded;
3. `Signed V2 Release Qualification` succeeded; and
4. `Fresh Install Qualification` succeeded.

Consequently, a green safe-docs-only main run is not release qualification and
cannot authorize an owner-supervised artifact.

## Concurrency And Cache Policy

- New commits cancel stale runs only for the same pull request. Main runs are
  never cancelled because their release evidence is commit-specific.
- Go module/build caches and npm download caches remain toolchain-managed.
- Signed qualification now uses the locked frontend npm cache.
- Playwright Chromium downloads use a lockfile-keyed cache, but the install
  command still checks host dependencies every run.
- GitHub-maintained actions use the first major release that defaults to the
  Node 24 action runtime: checkout v5, setup-node v5, setup-go v6, cache v5,
  upload-artifact v6 and download-artifact v7. These workflows run only on
  GitHub-hosted Ubuntu runners, satisfying their runner `2.327.1` minimum.
- PostgreSQL state, generated product assets and release artifacts are never
  restored as caches. They are rebuilt or recreated to preserve deterministic
  and clean-host evidence.

## Verified Repository And GitHub Facts

On 2026-07-14, `main` protection was read through the GitHub API and had strict
required status checks, admin enforcement, and the GitHub Actions-bound
`Backend Tests` context. Force pushes and branch deletion were disabled. This
change preserves that context rather than mutating protection settings.

PR #37 run `29287222439`, attempt 3, proved the pre-routing full suite passes
after GitHub billing was restored: Frontend, Backend, current legacy release,
signed-v2/upgrade/recovery and second-VM fresh install all succeeded. That run
is evidence for the existing implementation, not verification of the new
routing contract. The event/change-class executions recorded below subsequently
closed that routing evidence.

## Acceptance Evidence

| Check | Required result |
|---|---|
| Router self-test | safe docs, manual, workflow, source, mixed, empty and traversal cases pass |
| Current PR comparison | classified release-relevant |
| Workflow syntax | both workflow YAML files parse; GitHub accepts and schedules the routed jobs |
| Release-relevant PR | Frontend and Backend/API pass; release jobs skip; aggregate `Backend Tests` passes |
| Release-relevant main | PR test jobs skip; signed/legacy and fresh-install pass; aggregate gate passes |
| Supported-host crash sample | exact signed `xinsightctl` is externally killed as an isolated process group; durable recover/resume completes the same operation |
| Safe-docs-only PR follow-up | expensive jobs skip and protected aggregate gate succeeds |
| Owner-supervised source guard | rejects a main run unless both release qualification jobs and aggregate gate succeeded |

Local checks are execution-owner self-verification, not independent acceptance.
No Local or CI result is a Target, External integration or Production claim.

## 2026-07-14 Executed Result

- Release-relevant PR run `29298496412`: classification, Frontend,
  Backend/API and aggregate gate passed; Signed/Fresh skipped.
- Exact-main run `29298660052`: classification, merged legacy/signed and
  Fresh Install passed; Frontend/Backend/API skipped; aggregate gate passed.
- Owner-supervised run `29298897133` accepted that exact main CI and published
  the strict delivery. Its artifact identity and hashes are recorded in
  [main delivery evidence](../evidence/prd2/p2-4-owner-supervised-trial-main-delivery-evidence-2026-07-14.md).
- Safe-docs-only PR #38 run `29299294613`: classification and aggregate gate
  passed; Frontend, Backend/API, Signed and Fresh Install all skipped.
- Safe-docs-only main run `29299387213`: classification and aggregate gate
  passed; Frontend, Backend/API, Signed and Fresh Install all skipped.
- PR #40 run `29300250955` passed the release-relevant PR route. Its exact-main
  run `29300398720` then correctly failed because the root-created SIGKILL PID
  marker was not readable by the runner harness; no release was authorized.
- PR #41 run `29300603526` passed after atomic root-owned mode-`0444` marker
  publication. Exact-main run `29300748994` correctly failed because the
  expected killed-process wait status 137 invoked the global shell error trap
  before recover/resume; no release was authorized.
- PR #42 run `29300910033` passed after explicitly capturing and asserting the
  expected wait status. Exact-main run `29301064665` then passed classification
  in 3 seconds, Signed/legacy/SIGKILL qualification in 2 minutes 8 seconds,
  Fresh Install in 57 seconds and the aggregate in 4 seconds; Frontend and
  Backend/API skipped.
- Owner-supervised run `29301384798` accepted exact source run `29301064665`,
  rechecked current main and published the strict post-recovery delivery in 49
  seconds. Exact artifact and crash hashes are recorded in
  [supported-host SIGKILL recovery evidence](../evidence/prd2/p2-4-supported-host-sigkill-recovery-evidence-2026-07-14.md).
- PR #49 passed the release-relevant PR route. Its exact-main rc.5 run
  `29348263612` then passed classification, Signed/legacy qualification in 2
  minutes 10 seconds, Fresh Install in 56 seconds and the aggregate gate;
  Frontend and Backend/API correctly skipped. Owner-supervised run
  `29348552251` accepted only that exact successful main run and published the
  strict rc.5 delivery in 55 seconds. The later named-Target result is recorded
  separately in
  [rc.5 Target lifecycle/UI evidence](../evidence/prd2/p2-4-rc5-named-target-lifecycle-and-ui-evidence-2026-07-14.md).

All four event/change-class matrix rows now have actual GitHub evidence. The
safe-docs-only main run remains ineligible for owner-supervised packaging
because its Signed and Fresh Install job conclusions are `skipped`, not
`success`. The two failed crash-harness main runs also demonstrate that release
qualification blocks incomplete evidence rather than turning an expected
process death into a false green result.
