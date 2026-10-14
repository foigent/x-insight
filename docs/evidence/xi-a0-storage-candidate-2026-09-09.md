# XI-A0 approved storage candidate — Local delivery, A0 not accepted

Base `6306c44505fc2fff9173a23c7c103b536ed43061`, tree
`85ba61a351f5b876b8aa5a74a3ea58d06358c953`; sole owner task
`01a08361-94fa-73a3-9a71-f2e2507f08c4`, worktree
`/private/tmp/xinsight-xi-a0-20260909`, branch `codex/xi-a0-reading-chain`.

The user's explicit approval of the reviewed isolated storage design supersedes
its earlier approval pause. This is an approved experiment, not A0 acceptance.
The original 5000ms creation criterion, usable production HTTP, 20×300k samples,
500k/500001 boundaries, ten subjects and reading load remain in force.

## Implemented candidate

- Schema25 preserves schema24 bytes and every pre-existing fact ID and selected
  Evidence reference. Migration locks the relevant tables, checks all fifteen
  removed member fields, the A0 interpretation, exact membership digest/count
  and ordinals, then adds the selected Evidence hash to the fact. Members retain
  exactly reading_id, ordinal and economic_fact_id, both FKs and both uniqueness
  constraints. Money, filtering, sorting and the membership hash definition do
  not change.
- F0 is immutable under ordinary UPDATE/DELETE/TRUNCATE, including replica mode;
  an ALWAYS statement trigger is covered by the development schema contract.
  Superuser DDL remains a trusted administrative boundary, not tamper-proofing.
- Queries join the anchored F0; no latest mapping or mutable attribution join.
  Only the explicit A0 interpretation is supported; unknown revisions fail
  closed. Account/application/purpose remain unknown, cost project unavailable.
  Current authorization is still checked. Series aggregates one joined scan and
  fills zero buckets without losing the last partial bucket or hourly detail.
- Physical INSERT input uses fact tuple order to reduce random FK heap access;
  persisted ordinals still use (occurred_at,fact_id). The baseline query and this
  variant were separately measured; neither met the creation criterion.
- Actual workspace-role testing exposed the prior pgcrypto.digest EXECUTE denial.
  Runtime membership hashing now uses pg_catalog.sha256 over the identical UTF-8
  bytes. No runtime privilege was expanded; small real-role HTTP creation passes.
- Restores preserve exact Evidence/F0/header/member/grant contents. Host restore
  additionally validates anchored hashes, member existence/count/digest/order,
  interpretation and guard activation before returning. It never reconstructs
  identities from raw Evidence. Schema25 down explicitly refuses an in-place
  downgrade; the reviewed rollback is exact predecessor backup to a new database
  with its matching binary. No real upgrade/reset/maintenance was performed.

## Measured failures retained

| Candidate | 20×300k p95 | 500k creation | Evidence |
| --- | ---: | ---: | --- |
| Narrow member, original physical order | 10095ms | 17445ms | output/xi-a0-storage/store-performance.json |
| Narrow member, tuple physical order | 9660ms | 16628ms | output/xi-a0-storage/store-locality-performance.json |

These are Store diagnostics using the admin connection, not HTTP acceptance.
They preceded the equivalent runtime SHA-256 function substitution. The first
variant's first INSERT was13336ms; tuple ordering reduced it to5793ms, but full
creation still fails. Separate EXPLAIN timings are inclusive and must not be
added to phase timings. No durability, constraint, SQL/HTTP deadline or load
reduction was used. The inherited three failed wide-snapshot reports remain
byte-identical.

## Current HTTP qualification

`http-period-performance.json`: production TLS handler and actual importer /
workspace / reader database roles; workspace is non-superuser, statement_timeout
10s, and has no fact UPDATE privilege. Ten 7-day readings with exactly70000
members each were created through HTTP. 600 scheduled reads across ten subjects,
20RPS for30s: request p95=94ms, scheduled-to-complete p95=95ms,
achieved19.939RPS including final response drain, zero read errors. A 7-day hourly
series also returned200. The bounded job queue preserves all offered requests
when readers fall behind; latency includes that queue.

For30-day creation, all20 attempts with300000 members and the actual500000-member
attempt returned503 at the unchanged3-second HTTP deadline, with no retained
header/member transaction. Thus there is **no successful creation p95**; the3s
failure latency is not a performance pass. 500001 returns HTTP422 /
workbench_reading_too_large and Store ErrTooLarge. All six before/after counts
match: [10,700000,0,0,500001,60]. Since high-volume HTTP creation fails, a full
300k reading's subsequent real-HTTP30-day series/detail capacity cannot be
accepted. Small-fixture30-day browser semantics are a separate result.

`http-performance.json` retains the preceding one-hour-concentrated capacity
fixture (p95=93ms, same21 creation failures). It is not substituted for the final
7-day/30-day distribution. Both use the current built-in SHA-256 implementation.
Fresh evidence remains under output/xi-a0-storage. Role/restore, whole-Go and
production-browser qualification results are recorded below. No CI,
Artifact, Target, External Integration or Production claim is made.

A0 remains blocked by creation performance. A1/B/C/D/X and the overall thirteen
product goals remain outside this iteration. D-37-01 is undecided. Original49
protected files and the predecessor dependency symlink are preserved. No push,
PR, merge, release, deploy, live source/model/customer API, real credential or
permission change, user-data deletion, or UR contact occurred.

## Acceptance and compatibility review map

| Items | Candidate evidence / limitation |
| --- | --- |
| A0-01 / A0-02 | Exact integer money and original economic identity remain; projection replay keeps the selected Evidence and rejects changed content. The transaction rolls back conflicting Evidence. |
| A0-03 | All queries use the same frozen membership; tuple input ordering does not change ordinal/cursors. Migration compares all fifteen former snapshot fields. Creation performance is blocking. |
| A0-04 | Live grant/owner/expiry checks remain; explicit A0 interpretation dispatch rejects unknown revisions. Old schema24 reading money and all member fields survive conversion. |
| A0-05 | Saved-filter operation journal, optimistic revisions and replay behavior remain, with focused regression. |
| A0-06 | Stored coverage remains fixed; live source observation remains separate and restricted scope does not expose whole-source metadata. |
| A0-07 | Actual-role HTTP covers strict transport, CSRF, Problems, revocation and cancellation rollback. Built-in SHA-256 fixes a real permission failure without expanding grants. |
| A0-08 | New schema25 contract, ordinary immutable guards, exact backup/readback and corrupted binding/unknown interpretation rejection. Historical signed schema23 and migration24 bytes unchanged. |
| A0-09 / A0-10 | Previously accepted UI source/assets are unchanged. Current SQL/roles are subject to fresh production-browser and whole-Go regression below. CI remains unrun. |
| A0-11 | Mapper/EvidenceStore correctness fixtures are separate from bulk synthetic capacity rows; the latter intentionally reuse Evidence references and are not decoder throughput evidence. |
| A0-12 | Failed Local A0 candidate; no A1/B/C/D/X or parent completion. |

TD-01 retains current authorization and preference boundaries; TD-02 retains
canonical integer money and shared transport, with no new HTTP schema; TD-03
retains historical role/release policies and qualifies schema25 separately;
TD-04 keeps source/Envelope/M0 hashes distinct. Wide runtime copies and their
three dimension indexes are removed only in this isolated candidate; historical
migration24 remains necessary. Retention and real migration maintenance are not
implemented here. The candidate is not approved for rollout while A0 fails.

R3 review must verify selected IDs/hash and all fields, large and zero money,
ordinal/cursor invariance, unknown interpretation rejection, live authorization,
immutable guard modes, missing/corrupted restore bindings, current and historical
schema/role identities, actual HTTP failures as well as successes, and the
original49 protection readback. Owner tests are not independent acceptance;
manager independent review and all external evidence layers remain separate.


## Completed Local regression and evidence resolution

- `go-focused-04.log`: actual runtime roles, production HTTP small-money/paging/
  revocation/client-cancellation, migration24→25 all-field and corruption
  negatives, exact reading restore and binding rejection, immutable UPDATE /
  DELETE / TRUNCATE in origin and replica modes, and webui race pass.
  `TestWorkbenchFactKeepsSelectedEvidenceAcrossProjectionReplay` subsequently
  passes in the full Go run, preserving the entire F0 across a synthetic second
  Evidence reference and rolling back the changed-money transaction.
- `role-restore.log`: fresh PG16.14 least-privilege roles, supported historical
  upgrades, backup/isolated restore and schema drift negatives pass. Generated
  schema25 equals the committed development contract byte-for-byte:
  43 tables,507 columns,318 constraints,129 indexes,5 triggers,5 functions.
  Semantic SHA256 is fc5fd971a7b6492d6725d8c669e2f1b9f939aebc015cd27dbaa871af7fdd585e.
- `go-full-final.log`: full Go1.25.11 race/coverage invocation completed all
  packages. Its single failing test still expected db_schema=24..24; production
  correctly reported25..25. Only that test literal changed; `buildinfo-verified.log`
  records the affected package passing with race/coverage. The initial nonzero
  log is retained. This is full-suite evidence plus a targeted test-only repair,
  not a claim that the initial command returned zero. Other unchanged production
  source/package results are reused; `regression-resolution.json` binds the
  explicit resolution.
- `qualification-finish.log`: union of actual full-suite, role/restore and
  buildinfo profiles clears unchanged floors: total63.4%, API54.8%, DB70.8%,
  hostctl64.2%, workspace46.3%. `go vet ./...` and fixture11/11 pass.
- `frontend-coverage.log`:58 tests pass with statements72.73%, branches63.48%,
  functions73.05%, lines73.78%. Frontend code, lockfile and embedded assets are
  unchanged from6306c445; its build/OpenAPI/license and legacy18 browser checks
  remain applicable, not newly claimed executions.
- `browser-verified.log` and `output/playwright/xi-a0-storage-verified`: actual
  runtime roles and embedded production UI,3 tests pass in10.3s, including all24
  period/width/theme combinations, hourly detail, real52-row paging and recovery,
  real410 expiry and409 revocation. New30-day1024 dark screenshot was inspected;
  other unchanged layouts retain the earlier visual review plus fresh geometry
  assertions. All temporary browser/server and PG instances started in this
  continuation were stopped. Predecessor service/symlink remain untouched.
- `protection-check.json`: original49 and three inherited performance failure
  reports remain byte-identical. New failed logs are retained: stale series SQL
  alias, real-role pgcrypto permission failure, obsolete version test literal,
  both Store candidates and both HTTP capacity distributions.

Storage reports contain cumulative allocated member storage (including the
measurement sequence and rolled-back EXPLAIN allocation), not single-reading
cost: tuple-order candidate total1894563840 bytes, indexes1224081408 bytes.
A directly measured per-reading allocation delta was not recorded and must not
be inferred from these totals. Full300k HTTP downstream capacity remains blocked
by creation. No5-second pass or complete A0/parent acceptance is claimed.

The candidate and evidence are retained for manager independent review. It is
not a deployment/release recommendation. A change to the approved storage or
resource comparison assumptions requires a new technical decision; simply
extending HTTP timeout cannot turn the9.66s Store p95 into a5s pass. The real
maintenance/backup policy remains D-37-01, outside this isolated experiment.

Source/document diffs pass whitespace checks. The raw Vitest coverage table
retains its reporter-generated trailing spaces; this is the only staged
whitespace warning and its original log bytes are intentionally preserved.
