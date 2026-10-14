# XI-A0 Local delivery — acceptance remains blocked

2026-09-09. Owner: task `01a08361-94fa-73a3-9a71-f2e2507f08c4`.
Base: `e393e258140b798b3b8479d7db29b9c46db0d407`; branch
`codex/xi-a0-reading-chain`; sole product worktree
`/private/tmp/xinsight-xi-a0-20260909`.

## Outcome and limits

The existing official V1 → decoder/adapter → EvidenceStore transaction →
economic projection → fixed reading → typed production HTTP → embedded React
chain is implemented and locally exercised. **A0 is not accepted:** 300k reading
creation does not meet p95 ≤ 5000ms, and the production API's existing 3-second
query deadline rejects a 70k creation in the observed test. Fast reads of an
already-created reading do not establish usable creation at that scale.

No push, PR, merge, signed release, deployment, real Feed/model/customer API,
real credential change, user-data deletion, or UR communication occurred.
D-37-01 remains undecided. No Target/External/Production or CI claim is made.

## What this continuation changed

- Added historical schema23→24 single/duplicate/conflict tests, exact operation
  replay after later saved-filter revisions, real HTTP money/paging/revocation
  and capacity checks, actual reading backup/restore and invalid-archive tests.
- Fixed saved-filter creation/update to use the displayed reading's applied
  configuration, not unapplied control edits.
- Fixed dark-theme stylesheet ordering and shell/navigation text contrast;
  verified 1440/1920/1024 × light/dark through the embedded product.
- Preserved `period_precedes_capture_start` independently of profile gaps or
  disconnection; current source observations remain separate from frozen reading
  coverage. Neither opaque watermarks nor recent receipt time prove completeness.
- Regenerated schema24 from PostgreSQL16 under `xinsight_admin`. The inherited
  generated contract had local `m4` function ownership and correctly failed the
  real role test. No historical schema23 contract or release identity was edited.
- Added all six schema24 tables to the existing clean-baseline inventory. This
  keeps preflight complete; actual DATA_RESET still needs its separate authority.
  Updated the legacy fixed-count test to the current inventory.
- Fixed an existing July fixture test whose end time drifted with wall clock
  beyond the existing 31-day API window. The production limit remains unchanged.
- Copied existing dependency files into this isolated worktree, preserving the
  inherited node_modules symlink as `frontend/node_modules.predecessor-link`.
  No dependency version or lockfile change; no new tool installation.

## A0 evidence matrix

Evidence paths below are relative to the worktree. `output/xi-a0/evidence-binding.json`
binds source and evidence bytes; tests are owner self-verification, not the
supervisor's independent acceptance.

| Criterion | Local evidence / remaining boundary |
|---|---|
| A0-01 | `workbench-final-race.log`, `browser-delivery.log`: official settled fixture mapped and persisted, 17 charged versus 23 calculated micros in one request; UI shows ¥0.000017 / ¥0.000023, aggregate 270 / 342 separately. |
| A0-02 | DB tests prove replay no-op, distinct sources, conflicting online transaction rejection, mapper identity drift rejection; schema23 historical distinct-adapter duplicate collapses once and conflicting content rolls back migration. |
| A0-03 | Store and real HTTP tests iterate full contribution pages; rows sum to denominator, `other=0` for full mode, `unassigned_scope=reading_constant`; unassigned cost-project totals, exact large integer sums, series and detail tested. UI appends pages without adding constants repeatedly. |
| A0-04 | Store/API cross-user and project denial, no-detail grant, current revocation/expiry, late arrival old/new reading; actual backup/restore preserves old membership/interpretation refs and restored current revocation. Legacy Evidence permissions remain separate. |
| A0-05 | Store concurrent creation, conflicting idempotency content, stable create K1→update K2→replay K1, update replay, stale revision, cross-owner rejection. Production browser saves/updates/reopens same basis/scope; CSRF and legacy permission gates covered. |
| A0-06 | `coverage-states.log`: not-connected, observed-with-gaps, disconnected, fixed historical coverage versus live observation; restricted grants hide whole-source state. Actor/application/purpose/cost-project remain unknown/unavailable. |
| A0-07 | Strict DTO/query parsing, unsupported contract, CSRF, mapped Problem statuses and correlation; real 500001 HTTP422 and unchanged DB counts; OpenAPI validator and frontend transport tests. |
| A0-08 | `role-restore.log`, `reading-restore.log`, schema24 generation: clean migrations and 23→24, exact duplicate/conflict rollback, actual PG16 least privilege, historical rc10 independent policy, schema drift/restore negatives. Old rc.13 pack of 24 is rejected by the existing pack preflight, no release produced. |
| A0-09 | `browser-delivery.log`, `output/playwright/xi-a0-delivery/`: production TLS handler + embedded UI, contribution→detail→return→save/update/reopen, six viewport/theme combinations, no horizontal document overflow. Screenshots reviewed for readability. |
| A0-10 | Whole-repository and frontend regressions recorded below. Local shared bridge/golden/neutral profile tests only; remote cross-language GitHub action remains unrun. Shared pin, fixture contracts and incubation eligibility remain unchanged. |
| A0-11 | Production harness seed is official V1 fixture adapted into EvidenceStore, not API constants. Separate capacity test directly seeds synthetic economic rows and is explicitly not decoder throughput evidence. Store, HTTP, browser/mock layers are distinguished. |
| A0-12 | This is only A0 Local work, not A/A1/B/C/D/X or the whole 13-item product. FA-01/GX-01 and partial FA-02/US-01/US-02 gain local implementation; CT-01/02, US-03, SE-01/02/03, FA-03/04 and remaining attribution/source/parent acceptance stay open. |

The matrix does not override the failed creation performance gate.

## Verification layers and retained exclusions

- Final Go1.25.11/darwin-arm64 run: `go test -p 1 -v -race
  -coverprofile=output/xi-a0/go-coverage.out ./...` passes, followed by
  `go vet ./...` and fixture conformance11/11. `go-full-final.log` records the
  complete run; the temporary PG instance was stopped successfully.
- Coverage combines the actual full-suite and separate same-source,
  Go1.25.11 role/restore profiles using `merge-coverage.py`: total63.4%,
  API54.8%, DB70.8%, hostctl64.2%, workspace46.3%; all unchanged floors pass.
  This is combined Local test coverage, not a CI result.

- Frontend: Node22.23.1/npm10.9.8, full component coverage and build pass;
  statements71.73%, branches61.60%, functions72.19%, lines72.61%.
  OpenAPI50 paths and licenses195 locked packages pass. No dependency changes.
- Browser: 18 legacy mock cases pass; separate production TLS/embedded-UI
  workflow passes with all six 1024/1440/1920 light/dark screenshots inspected.
- PG16.14: isolated role/restore qualification passes, generated schema24
  matches committed development contract byte-for-byte. Actual reading archive
  restore, invalid archive, current revoke and historical coverage checks pass.
- Protected saved-project files: all49 baseline hashes match after verification;
  see `protected-files-verification.json`. Shared/release contracts were not edited.
- Runtime embedded asset manifest exactly matches the committed manifest when
  built with installed Go1.25.11. Earlier Go1.25.0 coverage command hit missing
  `covdata`; its nonzero result remains in `go-full-go1250-covdata.log`.

Opt-in tests requiring the immutable external UniRouter source-evidence package,
N1 release assets, final legacy artifact, campaign integration or administrative
rotation environment were not represented by this full-suite invocation. The
Workbench browser/performance and role/restore opt-ins were run separately as
listed above. No remote protocol CI, npm network audit, signed artifact, Linux
host qualification, Target, External Integration or Production result is claimed.

## Performance evidence and decision

Three inherited failure files are unchanged:

| File | 300k reading p95 / samples |
|---|---|
| local-performance-baseline-indexed.json | 22495ms / 20 |
| local-performance-no-dimension-indexes.json | 24722ms / 20 |
| local-performance-wide-phase-diagnostic.json | 19848ms / 20 |

First reading phases: insert 14945ms, digest 123ms, coverage 556ms, eligible
count 44ms. EXPLAIN INSERT 16614.580ms; its input SELECT about 774.8ms;
four FK triggers total about 6762ms. Node timings are inclusive and must not be
added as independent elapsed stages. This supports write/constraint work as
major cost, not a proof that wide copying is the unique cause.

The inherited 500000-member creation took 29527ms. Its 500001 test checked the
Store error/header rollback. The new HTTP capacity test additionally establishes:

- 10 distinct authenticated subjects and 10 fixed readings, 70000 members each;
- 600 TLS HTTP calls over about30s, ~19.95RPS, zero errors;
- request p95 76ms; scheduled-to-complete p95 77ms;
- readings were prepared through Store, **not successful production HTTP creation**;
- Store `ErrTooLarge` and HTTP422 `workbench_reading_too_large` with correlation;
- before/after counts identical: reading10, member700000, saved-filter0,
  operation0, economic-fact500001, Evidence60.

See `real-http-performance.json` and `real-http-verified.log`. Earlier
`real-http.log` records the real creation503; subsequent harness bugs (shared
httptest client cookie jar and JSON cursor reuse) were repaired in the tests,
not by weakening product authorization or rate limits.

No measured low-risk query candidate presently justifies a promise of ≤5s:
removing dimension indexes was worse, and removing the measured SELECT cost
alone cannot close the gap. No more speculative full 20-sample reruns were made.
Persistence, constraints, triggers, membership structure, and production API
limits are unchanged. See the separate blocked-design note for any proposed
storage redesign; its approval pause is still effective.

## Technical debt and compatibility

| Item | Result and retained boundary |
|---|---|
| TD-01 permission/preferences | Separate workbench capabilities, current grants and owned operation journal. Legacy investigation/saved-view/Evidence routes retain their permissions. New functionality is not claimed as removal of a pre-existing vulnerability. |
| TD-02 money/transport | Canonical decimal/numeric + big.Int stays backend authority; legacy GET delegates to shared authenticated JSON/Problem transport. Tests retain headers, no-store, same-origin credentials, AbortSignal, global401 and errors; POST/PUT keep CSRF. Old duplicate GET transport implementation is removed. |
| TD-03 host permissions | Current SQL arrays derive from the same Go grant table lists. Real PG role/restore negatives pass. rc10 historical UPDATE list stays independent because source preflight must recognize that exact released policy; retire only when supported rc10 upgrade/restore is explicitly retired with migration evidence. |
| TD-04 source/hash boundaries | V1 adapter, source snapshot hash, Envelope hash, M0 event hash and independent foigent bridge remain distinct. Remove only after an approved replacement covers actual callers and historical verification. No eligibility promotion or duplicate source authority. |

Remaining debt: reading creation write amplification/capacity; fixed-reading
retention requires a separately scoped supported operational lifecycle (no
unsolicited purge was added); legacy styles retain their broader compatibility
surface. A1 explanations must be versioned, never a mutable mapping joined into
old readings. No claim of zero debt or complete legacy retirement.

## R3 review checklist

- [x] Same Evidence transaction and source business identity; no adapter/epoch billing identity.
- [x] Exact charged/calculated separation and large-integer aggregation.
- [x] Fixed membership/interpretation, paging constants, late facts require new reading.
- [x] Current grant, ownership, detail permission, CSRF and operation journal negatives.
- [x] Schema24 real PG generation, owner binding, clean/upgrade/rollback and role/restore negatives.
- [x] Historical release/shared contract bytes preserved; rc13/schema24 fails closed.
- [x] Production handler/browser evidence separated from Store and mock checks.
- [ ] Creation performance and usable high-volume HTTP creation — **blocking**.
- [ ] Supervisor independent whole-delivery acceptance — pending.
- [ ] CI/Artifact/Target/External/Production — not authorized/executed here.

## Reproduction and safe recovery

Use an explicitly dedicated local PG16 DSN. Focused tests:
`go test -race ./internal/db ./internal/api -run '^TestWorkbench' -count=1 -v`;
`XINSIGHT_PG_BIN` enables actual pg_dump/pg_restore coverage. Capacity tests are
opt-in (`XINSIGHT_A0_HTTP_PERFORMANCE=1`), and should use fresh output paths.
The committed helpers under `output/xi-a0/` create their own PG clusters and stop
them; they are not target runbooks. Browser harness and config use a fresh
absolute output directory and the exact ready.json URL; create its stop marker
when browser verification completes.

Do not apply migration24 down to user data as a rollback shortcut. No existing
service was migrated. A future real rollback needs a separately authorized
backup/restore-to-new-database plan and compatible binary/schema pairing.
