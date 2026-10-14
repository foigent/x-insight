# Runtime Contract Capability Status

Status: Current implementation facts
Original runtime audit: 2026-07-10 at `9172633e257f521682026442363869b26c81af5e`
Planning-status review: 2026-07-13 at
`a8c25167202d0dd9180d9437f44dcdb25fca48fc`
Implementation-kickoff review: 2026-07-13 at
`76f3f0ebe9e3a83598fe17af5067fe9ccd003789`
P2-0 merged baseline: 2026-07-13 at
`4b6dca44a0947c22bd4a6059a0c96fbb475c4da8`
P2-1 merged baseline: 2026-07-13 at
`b754100a4eee9024707fe9c189c71ee579966a04`
P2-2/P2-3 merged baseline: 2026-07-13 at
`582e9692fc0d35f2e93c4105a891e09207bb5f8d`
Mock pull closure merged baseline: 2026-07-13 at
`cc084b396a50aa032f74e8eb8d4dd5d127d468a6`
P2-4 merged baseline: 2026-07-14 through PR #35 at
`7d56a36fcce255be152621a39c48645be6d7549c`
Owner-supervised first-customer authority accepted: 2026-07-14; exact-main
workflow merged through PR #37 as
`3884f6d7c9a8de04bb558c41975f44f3c511c671`; exact-main CI and delivery
evidence verified
Supported-profile crash/recovery baseline: PRs #40 through #42 merged as
`a77ba1de2064d6b297c1010de708019e46fd61cb`; exact-main run `29301064665`
and post-recovery owner-supervised workflow `29301384798` passed
Named-Target rc.5 baseline: PR #49 merged as
`27e05df26ff7976301a245f9120176e6ca70430d`; exact-main CI `29348263612`,
owner-supervised workflow `29348552251`, Target upgrade/application rollback/
re-upgrade and real search-to-gap browser proof passed on 2026-07-14
Snapshot v2 sidecar merged baseline: PR #52 source
`a5f64c011b032a670845009be1f40d796a47d629`, merged as
`0462e698775488d86e1263debf64990239464340`; candidate
`0.2.0-rc.6/manual-r6`. The exact synthetic UniRouter producer artifact passed
Local import, PostgreSQL first-write/replay, SQL, search/event API and actual
loopback UI readback. PR CI `29645951982` and exact-main CI `29646094307`
passed.
Named-Target rc.6 baseline: prerequisite PR #54 merged exact main
`0ca502f99181173e3542592ae5a018a5bfc43e0c`; exact-main CI `29648815418`
and owner-supervised workflow `29648976737` passed. The named Target completed
the in-place rc.5-to-rc.6 upgrade, backup/activation proof and non-mutating
rollback check. The exact synthetic v2 artifact then passed Target first-write,
identical replay, API and real browser/manual proof on 2026-07-19 as a fixture
rehearsal; that run was not External integration.
UR-E External-integration baseline: the separate fresh manifest-bound artifact
SHA-256 `412c9b641c...` passed gated Target first-write, identical replay,
SQL/API and real UI reconciliation on 2026-07-19. The exact one-call/zero-trace/
one-gap scenario is execution-owner External integration verified. Field
usability and Production remain pending or out of scope.
Internal-real IP-policy compatibility baseline: the User-authorized UniRouter
Linux-first producer adaptation merged through PR #225, tested head
`c3930e3086299bc0ea3c0727b81262de85c501b1`, CI run `29711162577` attempt 2
and main `d43fef6eed92323da0880f0f6e0e8d35e388e8e0`. This X-Insight revision
implements the matching strict two-boolean preflight contract without a schema,
migration or new artifact field. No B2 source read, artifact, import, Target or
Production action follows from this implementation evidence.
Completed-execution pull V1 reopen: the User accepted the complete minimum
slice on 2026-07-25. X-Insight Local implementation is complete on
`codex/completed-execution-feed-v1`, bound to UniRouter commit
`0fe054cb7b5e2b1b8081b0eb8dd454a669772804` and inner schema SHA-256
`4ae2897d4f8470c38624dfe21c54d72eaf6d995d2c9578c4420c94d244424fb2`.
The complete Local Go/frontend/browser suite plus focused contract,
cursor/replay/failure, transport/auth, lifecycle and read-model tests pass.
PR CI, merge, deployment, live activation, bounded readback, Target, External
integration and Production remain separate.

## Purpose

This record answers the durable-runtime questions required by UniRouter without
turning roadmap intent into an implemented contract. Public/stable means a
versioned interface with an explicit compatibility commitment; the current
development endpoints and CLI are not yet public/stable unless stated below.

The User accepted Phase 2 B0-D01 through B0-D34 defaults and the exact PRD2-M1
plan, authorizing X-Insight-owned implementation on 2026-07-13. Merged P2-0
provides the engineering contract, embedded `zh-CN` foundation shell,
identity/cursor/problem primitives and a disabled/mock-only pull consumer.
Merged P2-1 implements the bounded investigation read API, migration/import
provenance and resource envelope. Merged P2-2/P2-3 implement the complete
bounded investigation UI with Local and CI evidence. The deterministic mock
consumer now has Local activation/revision/replay/no-raw/failure-isolation and
real PostgreSQL search-visibility evidence plus PostgreSQL 16 GitHub CI.
Merged PR #35 implements the managed single-node lifecycle, signed-v2 release
qualification and release-matched Chinese product manual. It has Local evidence
plus exact signed and separate clean-VM fresh/legacy paths. PRs #40 through #42
then added all-operation durable-phase coverage and an external process-group
SIGKILL sample. Exact-main run `29301064665` completed the interrupted
upgrade's same-operation recover/resume, later rollback/re-upgrade/restore and
the separate fresh install. These CI results remain explicitly non-publishable
and classified `ci_clean_vm_not_target`; literal power loss and the named
Target were not tested. B0-D32 adds a separate
`owner_supervised_trial` workflow: it binds exact current `main` to a successful
matching push CI, signs with a one-run key, removes the private key before
upload and publishes a strict workflow/CI/hash record. Post-recovery workflow
`29301384798` accepted exact source run `29301064665` and published its strict
seven-file delivery. X-IAM and Tool Gateway remain unimplemented. B0-D33 later
supplied one bounded, operator-mediated fresh synthetic sidecar External-
integration proof; it did not add automatic transport or a public/stable
contract. B0-D34 later authorized one narrow source-policy adaptation and
receiver compatibility chain. The accepted 2026-07-25 reopen supersedes the
live-feed deferral for the exact completed-execution V1 slice only; it does not
reopen the retired artifact/import route or authorize customer/Production data.
See the
[Batch 0 register](../planning/phase-2-batch-0-scope-and-decision-register-2026-07-13.md)
and [PRD2-M1 plan](../planning/prd2-m1-unirouter-field-validation-implementation-plan-2026-07-13.md).
The exact intended API/UI/toolchain boundary is recorded in the
[P2-0 engineering contract](prd2-p2-0-api-ui-engineering-contract-2026-07-13.md);
that record is not itself implementation evidence.

## Durable Import Job And Deep Link

| Question | Current fact |
| --- | --- |
| Persisted import-job resource | **Not implemented.** There is no import-jobs table, lifecycle, retention rule, or job API. |
| `import_job_id` | **Not implemented.** X-Insight does not generate or return this field. |
| HTTPS deep link | **Not implemented as a cross-project contract.** A same-origin P2-0 foundation shell now exists, but no durable import job or authorized UniRouter execution/job URL exists. |
| Tenant scope and guess resistance | **Not applicable yet.** Tenant isolation and authorization have not been implemented. UniRouter should not store a fabricated job ID or URL. |

The current importer is a synchronous CLI operation. A durable job model must
be designed with tenant ownership, lifecycle, retry, cancellation, retention,
and audit semantics before a deep link is exposed.

## Receipt Transport And Idempotency

The only supported receipt transport is an exclusive-create sidecar JSON file
written by `xinsight-importer unirouter gate-a-check --receipt-output <path>`.
There is no receipt HTTP API, authentication method, timeout/retry contract,
transport status-code registry, or automated UniRouter delivery.

The canonical receipt schema is
[`xinsight.import-receipt.v1`](../../schemas/integration/import-receipt.schema.json)
and is represented in code by
[`internal/exchange/receipt.go`](../../internal/exchange/receipt.go). It has:

- result states `validated`, `persisted`, and `rejected`;
- validation, duplicate/no-op, quarantine, persistence-conflict, and readback
  counts;
- warning codes `approval_manifest_hash_missing`,
  `persistence_not_requested`, `source_capture_gaps_present`, and
  `source_exchange_id_missing`.

The tuple `(exchange_id, artifact.sha256)` is echoed in a receipt and P2-1
provenance-enabled imports persist those values for evidence classification,
but the tuple is **not an X-Insight idempotency key**. Current event persistence
enforces uniqueness on
`(source_system_id, adapter_manifest_id, idempotency_key)`:

- same idempotency key and same canonical event hash: `duplicate_noop`;
- same idempotency key and different canonical event hash:
  `idempotency_conflict`;
- schema or sensitive-content failure: rejected or quarantined by the importer;
- transient transport status: **not defined**, because no network receipt
  transport exists.

Receipt signatures, MACs, or another authenticity mechanism are **not
implemented**. Artifact and manifest hashes provide integrity correlation, not
sender authenticity.

## UniRouter snapshot v2 money compatibility

The current merged candidate accepts both historical
`unirouter.prd5.xinsight.snapshot.v1` cents and the v2 exact money tuple:

- `cost_micros`: canonical 1–19 digit non-negative decimal string;
- `currency=CNY`;
- `amount_scale=1000000`;
- `money_schema=micro-cny.v1`.

The two forms are mutually exclusive. Partial tuples, explicit nulls,
`cost_cents` plus v2 money, unknown snapshot fields and unknown schema
versions fail closed. Exact micro-CNY remains text through normalized evidence,
JSONB, API and UI; it is never converted through floating point.

Mapper identity is versioned separately: historical v1 keeps
`22222222-2222-2222-2222-222222222222`; v2 uses
`22222222-2222-4222-8222-222222222223`. This prevents a same-source-row v2
event from colliding with or being attributed to the v1 mapper. The source
registry advances from v1 to v2; later v1 or versionless historical replay does
not downgrade v2. Unknown non-empty source-version transitions and explicit
reuse of a known manifest ID for a different known mapper version fail closed.

The Local and named-Target proofs use the exact synthetic UniRouter producer
artifact, SHA-256
`93bc4c8448f1f838668f9dab10cbba070f0df550665b16d73bb494066fe8d6a1`.
First CLI persistence inserted 2 events including 1 capture gap; identical
replay returned 2 duplicate/no-op and 0 conflicts. The event API and actual
loopback UI read back `"7"` as
`CNY 0.000007 (7 micro-CNY)`. PR and exact-main CI passed. Exact rc.6 then
received an owner-supervised publication and passed the named Target's
in-place rc.5-to-rc.6 upgrade, first-write/replay/API and real UI/manual proof.
This is Target verification for one exact synthetic fixture, not External
integration or Production. The CI signing identities remain explicitly
non-publishable; only workflow `29648976737` is the matching supervised
delivery.

## Internal-Real Two-Boolean IP Compatibility

The new strict internal-real path freezes this exact declaration:

```text
raw IP addresses are not exported; only ip_address_was_present and ip_address_redacted boolean fields are retained
```

Strict preflight accepts `false/false` and `true/true`. It blocks
`true/false` with `raw_ip_redaction_not_proven` and `false/true` with
`raw_ip_presence_not_proven` before normalized-event construction or
persistence. The historical declaration
`raw IP addresses are not exported; only ip_address_was_present is retained`
remains accepted only when every call-log row is `false/false`; it is rejected
for `true/true` and either mismatch.

Snapshot decoding continues to fail closed on an unknown raw `ip_address`
field or any unapproved alias and does not echo the raw value. The call-log
normalized source excerpt persists only `ip_address_was_present` and
`ip_address_redacted`. The general manifest-free historical importer is not
tightened: the authorized internal-real CLI path requires successful manifest
preflight and binds the exact preflight artifact SHA to the import bytes before
persistence. `internal/adapters/unirouter/import_snapshot.go` therefore remains
unchanged after required static review.

This is a narrow receiver compatibility boundary, not a source or artifact
result. It does not prove the source IP was deleted or modified, does not prove
the database reader principal lacks capability to select the base IP column,
and the two booleans are not general DLP proof. B2a, B2b, B3, Target,
Production and live-feed states are unchanged.

## Completed-Execution Feed Reopen Versus Historical Deferral

On 2026-07-18 the User accepted deferral, rather than permanent rejection, of
the complete `xinsight.completed-execution-feed.v1` slice from the first formal
version. That boundary was binding until the User accepted the complete V1
reopen package on 2026-07-25. The reopen replaces sidecar-only as the intended
future runtime path but does not rewrite historical sidecar evidence or turn
the old artifact route into live transport.

The current implementation adds one managed, writer-capable X-Insight pull
worker, fixed-endpoint HTTP source, feed-only bearer authentication,
strict byte receiver, durable PostgreSQL activation/cursor/high-watermark/
status and at-least-once cursor-after-page persistence. It is bound to
`unirouter.completed-execution.v1` revision `1` and preserves X-Insight
normalization ownership.

The accepted credential boundary is receiver-generated: root
`xinsightctl feed-credential` creates the 256-bit raw token only on the
X-Insight target and emits a strict public prefix/verifier/operation bundle.
Exclusive staged-file checks, durable nonsecret state and exact
operation/verifier binding make prepare/atomic activate/abort and rotation
recoverable without transferring the raw token. mTLS, RSA envelope transport,
browser/clipboard handoff and a cloud secret manager are not V1 dependencies.

Activation-forward eligibility is defined by the source instance and opaque
bootstrap cursor/high-watermark. Source/page timestamps are validated as
metadata only. A nonempty page must advance the cursor; a post-activation empty
page must not. Per-item evidence provenance does not claim the page-next cursor
or high-watermark. A crash before the separate durable cursor transaction
replays already persisted items as duplicate/no-op.

The deterministic mock remains Local engineering evidence but is no longer a
substitute for the accepted live implementation. The worker is installed
dormant. Receiver-credential tooling release/deployment must keep the worker
inactive/disabled and live/staged keys absent; prepare/registration,
activation and actual API/UI readback remain separately sequenced. UniRouter
remains independent of X-Insight availability and broad historical backfill
remains unauthorized.

## Retention, Deletion, And Custody

| Capability | Current fact |
| --- | --- |
| Evidence retention | **General policy not decided or enforced.** PRD2 separately accepts a field-trial-only rule for its dedicated sanitized database/backups: field session plus seven days, then explicit handoff or controlled retirement; no lifecycle job exists. |
| Import-job retention | **Not applicable; resource not implemented.** |
| Receipt retention | **Not decided.** Sidecar custody remains with the operator/source workflow. |
| Audit-record retention | **Not decided.** |
| Downstream deletion API | **Not implemented.** |
| Immutable deletion receipt | **Not implemented.** |
| Audit of preview/read/download/export/delete | **Not implemented.** The current read APIs do not authenticate or emit audit-on-read evidence. |

The non-production target may hold repository fixtures/synthetic metadata and
an explicitly approved sanitized-metadata subset admitted through the
controlled sidecar path. It may not hold raw prompt/response or a broadly
described customer dataset. Any future payload storage requires explicit
retention, encryption, access and audit controls.

## Target Runtime

Codex completed the Phase 1 non-production deployment and reconciliation on
exact CI artifact commit
`b9e5811cc53a5cb06614956e66a1c54756617f0a`. The dedicated security group has
zero rules, UFW defaults to deny incoming, the service/database remain
loopback-only, the environment file is `0600`, and the data mount is effective
`noatime`. Nine deployed migrations, first-insert plus duplicate/no-op synthetic
imports, execution/events/gaps readback, and restart preservation passed.
That Phase 1 status remains closed.

Exact owner-supervised rc.5 from
`27e05df26ff7976301a245f9120176e6ca70430d` is now also **Target verified** on
the same named non-production host for the accepted loopback and approved-
metadata scope. Upgrade, schema 11 activation, application rollback without a
down migration, re-upgrade, restart, readiness, bounded search, detail,
evidence-derived timeline, selected-node/source/provenance, capture gaps and
the rc.5/manual-r5 Chinese manual passed. The PostgreSQL 16 container and
persisted evidence fingerprint were preserved across the lifecycle. The first
Alibaba Cloud Session Manager CLI forward was later found to have a local
wildcard bind and was stopped. The approved replacement uses Session Manager
only as the OpenSSH stdio proxy plus an explicit OpenSSH loopback forward;
listener, readiness and UI checks passed, and security-group ingress remained
empty. See the
[Target access listener correction](../evidence/prd2/p2-4-target-access-listener-correction-2026-07-14.md).

Exact owner-supervised rc.6 from
`0ca502f99181173e3542592ae5a018a5bfc43e0c` is now also **Target verified**
on that host for the loopback synthetic-fixture boundary. Operation
`20260718T220854Z-1f5aed3059a2daac` completed the in-place rc.5-to-rc.6
upgrade, verified backup and activation, and passed a non-mutating previous-
release rollback check. The same exact v2 fixture used in Local/CI then passed
first-write, identical replay, bounded API and real search/timeline/node/
source/gap/manual browser proof. The PostgreSQL container remained unchanged;
active rc.5+rc.6 transition trust is retained only while the rollback window
remains open. See the
[rc.6 named Target, v2 sidecar and UI evidence](../evidence/prd2/p2-4-rc6-named-target-v2-sidecar-and-ui-evidence-2026-07-19.md).

The signed rc.5 and rc.6 release-time metadata says `target_verified=false`
because each delivery was produced before its Target execution; later append-
only evidence is the status authority and signed packages are not rewritten.
The rc.6 v2 execution is explicitly `synthetic_fixture`/`gap` and does not
establish a current live UniRouter feed or fresh External integration. It is
not `Production verified`; storage encryption, customer-site backup/restore
custody, auth/RBAC, retention/deletion/audit, HA/DR and public API security
remain unresolved. See the
[Phase 1 final target result](../ops/atlas/responses/2026-07-10-phase-1-final-target-pass-result.md)
and
[rc.5 named Target lifecycle/UI evidence](../evidence/prd2/p2-4-rc5-named-target-lifecycle-and-ui-evidence-2026-07-14.md).

A later, separately authorized UR-E execution supplied the fresh External-
integration proof that the fixture rehearsal did not. Its manifest-bound
snapshot contained one usage call, zero trace events and one confirmed missing-
trace gap. First-write inserted exactly two events and one gap; replay inserted
nothing and produced two duplicates with zero conflicts. Bounded API and the
actual Target UI matched persistence. This proves only that exact synthetic
sidecar scenario and does not alter the live-feed, customer-data or Production
boundaries. See the
[UR-E exact synthetic External Integration evidence](../evidence/prd2/prd2-ur-e-exact-synthetic-external-integration-evidence-2026-07-19.md).

## Implemented Surfaces

### PR #35 merged P2-4 surfaces

- signed release manifest/envelope v2 with Ed25519 trust/revocation, deterministic
  pack, verify-before-exclusive-extract and an explicitly out-of-band bootstrap
  trust boundary;
- checksum/dirty migration ledger, maintenance locks, distinct database role
  credentials and loopback-fail-closed deployment templates;
- root-only, durable-journaled install/adopt/upgrade/application rollback/
  restore/status/recover/resume/limited-abort lifecycle;
- release-matched 12-chapter Simplified Chinese source manual, deterministic
  offline assets, same-origin `/manual/zh-CN/` route and product help link; and
- a separate manually triggered first-customer packaging path that requires
  exact current `main` and successful matching push CI, uses a one-run Ed25519
  key, removes that exact private-key file before delivery, publishes a strict
  seven-file `owner_supervised_trial` set and explicitly keeps Target, External
  integration and Production false.

Local code/component/browser/fault tests and exact signed/clean-VM CI evidence
exist for these merged surfaces. The fresh, legacy and supported-profile
SIGKILL/recovery lifecycle results remain CI qualification only. Exact rc.5
subsequently passed the separately recorded named-Target lifecycle and real UI
proof. Manual-guided User tasks and Production remain pending or out of scope.
The current exact-scenario External-integration result comes only from the
separately authorized UR-E evidence above and must not be inferred from release
or fixture Target proof.

See [owner-supervised exact-main delivery evidence](../evidence/prd2/p2-4-owner-supervised-trial-main-delivery-evidence-2026-07-14.md).
See also [supported-host SIGKILL recovery evidence](../evidence/prd2/p2-4-supported-host-sigkill-recovery-evidence-2026-07-14.md).
See also [rc.5 named Target lifecycle and UI evidence](../evidence/prd2/p2-4-rc5-named-target-lifecycle-and-ui-evidence-2026-07-14.md).

### Implemented development surfaces

- an embedded, same-origin `zh-CN` React investigation application at
  `/executions` and `/executions/{execution_id}`, with CSP/permissions/
  referrer/frame/content-type security headers and SPA fallback restricted
  away from API/health paths;
- bounded execution filters and fixed-snapshot cursor navigation; execution
  proof/freshness summary; evidence-derived chronology; declared-parent-only
  causality; selected event/source/provenance detail; independent confirmed/
  candidate capture-gap inspection; and loading/empty/error/degraded states;
- visible direct/derived/declared/suppressed/gap/unknown/`not_applicable`
  semantics, conservative approved/synthetic/unclassified proof copy and no
  raw payload viewer;
- loopback-only listener enforcement by default and for explicit server
  addresses;
- P2-0 execution-ID, cursor and `application/problem+json` primitives, with
  legacy read failures no longer returning raw store errors;
- P2-1 immutable import-provenance records and event links, rebuildable
  persisted source-qualified execution IDs and deterministic legacy backfill;
- bounded `GET /api/v1/executions` search with exact request ID or prefix,
  source, time, outcome and `has_gaps` filters plus fixed-`as_of` keyset paging;
- bounded execution detail, timeline, selected-event, independently paged gap
  and source-option APIs over allowlisted DTOs;
- explicit `unclassified` proof/assurance fallback and
  `syncing`/`current`/`stale`/`gap` freshness projection; and
- bounded snapshot/event intake, a deployment-wide persistence writer lock,
  capped DB pool and bounded API URI/rate/concurrency/deadline behavior;
- a strict, default-disabled/mock-only completed-execution feed consumer with
  explicit activation/source binding, no-raw single-execution envelope checks,
  monotonic revision/replay/completeness handling, page/cursor failure
  isolation and diagnostic search-visibility measurement; this historical mock
  remains test-only;
- a separate production-intended `xinsight-importer unirouter pull-feed`
  worker with strict HTTPS-by-default source transport, durable cursor state,
  existing-store idempotent writes, cursor-after-page replay convergence and
  lifecycle stop/drain/restart wiring; Local persistence time is not claimed as
  measured API readback;
- UniRouter sanitized snapshot discovery, mapping, validation, persistence,
  readback, and receipt generation through the local CLI;
- strict manifest-bound internal-real preflight with exact dual-boolean IP
  declaration/state validation, fixed no-value finding codes and historical
  `false/false` declaration compatibility;
- optional read-only UniRouter Postgres structure discovery; this is not a
  continuous importer;
- `GET /healthz` and `GET /readyz`;
- `GET /api/v1/executions/{request_id}`;
- `GET /api/v1/executions/{request_id}/events`;
- `GET /api/v1/executions/{request_id}/gaps`.

The three legacy raw-request-ID development paths still default to `limit=200`
and clamp at `1000`; they have no cursor or stable compatibility commitment.
The P2-1 OpenAPI paths use the narrower documented per-route limits and stable
problem/cursor contract, but still have no authentication or tenant
authorization and must remain loopback-only. Merged P2-2/P2-3 connect those
paths to the feature UI; this does not authorize non-loopback exposure.

### Represented but not implemented as adapters

- LiteLLM has M0 fixture coverage only; no callback or spend-log adapter exists.
- New API and one-api are schema/source-type reservations only.
- OpenTelemetry/OpenInference, MCP, SDK, and business-system evidence are schema
  reservations only.
- No public collector API, adapter SDK, exporter/outbox, or continuous source
  integration exists.

## Versions, Compatibility, And Limits

| Item | Current fact |
| --- | --- |
| Receipt schema | `xinsight.import-receipt.v1`; exact schema validation is implemented. |
| Evidence schema | Current UniRouter adapter emits `m0.1`; the event schema accepts a non-empty version string rather than a fixed supported-version registry. |
| Snapshot schema | Supplied by each controlled artifact and preserved in the receipt; no general compatibility matrix exists. |
| Internal-real IP proof | New strict snapshots require the exact dual-boolean declaration; only `false/false` and `true/true` pass. Historical declaration compatibility is `false/false` only. Raw IP fields remain unknown-field rejected. |
| Compatibility guarantee | Merged P2-1 implements the frozen internal PRD2 v1 read contract; merged P2-2/P2-3 consume it. No public API, source-adapter or cross-project compatibility promise exists. |
| Deprecation policy | **Not decided.** |
| Import batch/count/byte limit | Snapshot maximum 64 MiB; 1,000 request IDs; 10,000 normalized events; 10,000 capture gaps; 256 KiB per normalized event; failure precedes persistence. |
| Read API limit | Search default 50/max 100; timeline default 100/max 200; gaps default 50/max 100; sources default 50/max 100. Legacy event/gap paths remain default 200/max 1000. |
| API runtime envelope | URI 8 KiB; process rate 20/s burst 40; concurrency 32; handler/query deadline 3 s; PostgreSQL pool max open 10/max idle 5. |
| Receipt file behavior | Exclusive create; an existing output path is not overwritten. |
| Frontend toolchain | Node `22.23.1`, npm `10.9.8`, React `19.2.7`, TypeScript `7.0.2`, Vite `8.1.4`; exact lockfile committed. |
| Pull consumer mode | Historical in-process `mock` remains test-only. The production-intended path is a separate dormant `xinsight-feed.service` using `xinsight-importer unirouter pull-feed`; activation requires deployment-time endpoint, tunnel, credential and database binding. |

The new bounds are implemented locally, not a throughput, HA, public-security
or Production support claim. Public compatibility and deprecation policy remain
explicitly undecided.

## Source Evidence

- [`schemas/integration/import-receipt.schema.json`](../../schemas/integration/import-receipt.schema.json)
- [`cmd/xinsight-importer/main.go`](../../cmd/xinsight-importer/main.go)
- [`cmd/xinsight-importer/main_test.go`](../../cmd/xinsight-importer/main_test.go)
- [`internal/adapters/unirouter/import_snapshot.go`](../../internal/adapters/unirouter/import_snapshot.go)
- [`internal/adapters/unirouter/import_snapshot_test.go`](../../internal/adapters/unirouter/import_snapshot_test.go)
- [`internal/adapters/unirouter/sanitized_preflight.go`](../../internal/adapters/unirouter/sanitized_preflight.go)
- [`internal/adapters/unirouter/sanitized_preflight_test.go`](../../internal/adapters/unirouter/sanitized_preflight_test.go)
- [`internal/db/evidence_store.go`](../../internal/db/evidence_store.go)
- [`internal/db/investigation_store.go`](../../internal/db/investigation_store.go)
- [`internal/investigation/model.go`](../../internal/investigation/model.go)
- [`internal/api/server.go`](../../internal/api/server.go)
- [`internal/api/investigation.go`](../../internal/api/investigation.go)
- [`internal/api/server_test.go`](../../internal/api/server_test.go)
- [`api/openapi/prd2-v1.yaml`](../../api/openapi/prd2-v1.yaml)
- [`internal/executionid/execution_id.go`](../../internal/executionid/execution_id.go)
- [`internal/pagination/cursor.go`](../../internal/pagination/cursor.go)
- [`internal/integration/pullfeed/feed.go`](../../internal/integration/pullfeed/feed.go)
- [`internal/integration/pullfeed/live_worker.go`](../../internal/integration/pullfeed/live_worker.go)
- [`internal/integration/pullfeed/http_source.go`](../../internal/integration/pullfeed/http_source.go)
- [`internal/integration/pullfeed/postgres_state.go`](../../internal/integration/pullfeed/postgres_state.go)
- [`internal/integration/pullfeed/evidence_committer.go`](../../internal/integration/pullfeed/evidence_committer.go)
- [Completed-Execution Pull V1 Runtime Contract](../reference/completed-execution-pull-v1.md)
- [Completed-Execution Feed V1 Local Implementation Evidence](../evidence/prd2/completed-execution-feed-v1-local-implementation-2026-07-25.md)
- [`internal/webui/webui.go`](../../internal/webui/webui.go)
- [`frontend/package.json`](../../frontend/package.json)
- [`frontend/src/ExecutionSearchPage.tsx`](../../frontend/src/ExecutionSearchPage.tsx)
- [`frontend/src/ExecutionDetailPage.tsx`](../../frontend/src/ExecutionDetailPage.tsx)
- [`frontend/e2e/investigation.spec.ts`](../../frontend/e2e/investigation.spec.ts)
- [`migrations/000004_create_evidence_events.up.sql`](../../migrations/000004_create_evidence_events.up.sql)
- [`migrations/000010_create_import_provenance.up.sql`](../../migrations/000010_create_import_provenance.up.sql)
- [`migrations/000011_create_import_provenance_events.up.sql`](../../migrations/000011_create_import_provenance_events.up.sql)
- [P2-1 local backend and 1x diagnostic](../evidence/prd2/p2-1-local-backend-and-1x-diagnostic-2026-07-13.md)
- [P2-2/P2-3 investigation UI Local evidence](../evidence/prd2/p2-2-p2-3-investigation-ui-local-evidence-2026-07-13.md)
- [`README.md`](../../README.md)
