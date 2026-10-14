# Phase 2 Batch 0 Scope And Decision Register

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

Status: D01-D33 product/boundary/execution defaults accepted; P2-0 through P2-4,
exact rc.6 publication and named-Target synthetic v2 proof verified; the
2026-07-18 decision defers the whole live UniRouter feed slice from the first
formal version; the bounded `UR-E` joint decision is binding with P0=0/P1=0 and
its exact manifest-bound synthetic sidecar scenario has execution-owner
External-integration evidence. `N=1` field evidence remains pending.
Prepared: 2026-07-13
Decision update: 2026-07-19
Status reconciliation: 2026-07-19
Decision owner: User
Execution owner: Codex
X-Insight baseline for this update:
`a8c25167202d0dd9180d9437f44dcdb25fca48fc`
Implementation-kickoff baseline:
`76f3f0ebe9e3a83598fe17af5067fe9ccd003789`
P2-0 merged baseline:
`4b6dca44a0947c22bd4a6059a0c96fbb475c4da8`
P2-1 merged baseline:
`b754100a4eee9024707fe9c189c71ee579966a04`
P2-2/P2-3 merged baseline:
`582e9692fc0d35f2e93c4105a891e09207bb5f8d`

## Purpose

This document unifies the already accepted Phase 2 product priority with the
2026-07-13 competitive-alignment and UniRouter / X-IAM / X-Insight 100x
architecture-audit tasks. It distinguishes the product/engineering decisions
that gate Batch 1 from later authority and innovation decisions that gate only
the dependent expansion.

On 2026-07-13 the User accepted the recommended boundary defaults in B0-D01
through B0-D31. That acceptance freezes the product and architecture direction
represented by those rows; acceptance alone is not implementation evidence.
P2-0 has frozen and implemented its X-Insight-owned contract/toolchain
foundation. Merged P2-1 implements the bounded API, schema projection,
provenance and resource controls. Merged P2-2/P2-3 implement the investigation
UI with Local and CI evidence. X-IAM, Tool Gateway, semantic analysis and
feedback paths remain unimplemented. High-impact action classes,
spool failure policy,
semantic/privacy contracts and whether a Tool Gateway is built, reused or
deferred remain explicitly unresolved. The historical X-Insight pull-feed
direction produced a disabled/mock consumer and contract proposal, but the User
deferred the whole live slice on 2026-07-18. It is no longer a first-version
gate and authorizes no UniRouter, X-IAM or Tool Gateway change.

On 2026-07-19 the User accepted B0-D33 on the X-Insight/requesting side. It
defines one fresh manifest-bound synthetic sidecar proof, not a new transport.
UniRouter provided its owner-authored response and planning links through PR
#221, then replaced the unsafe macOS `install -m 0600` receiver copy with the
pinned exclusive-create mechanism through PR #222 / routed CI `29672169049` /
main `9a94d549...`. X-Insight verified the completed response SHA and mechanism
identities. The shared decision became `Decision accepted` / `Binding` at
`2026-07-19 12:15:26 CST`. UniRouter subsequently completed Stage A, and
X-Insight passed Stage A.5 plus the bounded Stage B Target first-write/replay,
API, real UI and final reconciliation gates for the exact contained scenario.

## Fact Baseline Accepted Into This Register

- X-Insight Phase 1 is closed. All seven gates are Complete and there is no new
  verified P0/P1 requiring it to reopen.
- The accepted first product priority is X-Insight-owned execution search,
  execution detail/evidence timeline, selected-node detail, source/provenance
  inspection and capture-gap inspection.
- The deployment posture remains one internal deployment tenant, loopback-only
  X-Insight/PostgreSQL, tunnel or approved internal proxy, fixture/approved
  sanitized metadata, and no raw prompt/response.
- The only currently implemented and jointly accepted UniRouter runtime
  boundary remains operator-mediated sidecar import/receipt. The whole live
  completed-execution feed slice is deferred from the first formal version; its
  candidate remains future re-entry material, not a pending gate. Schema/source-
  type reservation is not an adapter.
- X-IAM is currently research material, not a runtime dependency or third
  deployed component.
- UniRouter's reviewed main contains Agent capability, ancestor budget,
  distributed admission and internal outbox work, but no external X-Insight
  transport or 100x/HA proof.
- At the planning baseline X-Insight had no frontend. P2-0 now embeds a
  foundation shell. Merged P2-1 implements bounded execution search, detail,
  timeline, event, gap and source APIs with cursor paging. Merged P2-2/P2-3
  implement the corresponding feature UI.

## Unified Scope

### Batch 0 Decision Scope

1. Freeze the first operator persona and investigation job.
2. Decide recommendations 2–8: execution identity, access posture, filters,
   timeline truth rule, provenance labels, real/synthetic proof and frontend
   packaging/framework.
3. Define the bounded API and UI contract before implementation.
4. Define safety prerequisites: input/page limits, cursor semantics, DB/HTTP
   backpressure, error categories and sensitive-data boundary.
5. Define a measurable 1x workload, Batch 1 performance budgets and failure
   behavior without claiming 100x readiness.
6. Decide whether X-IAM remains research-only and record the candidate future
   authority split without creating a synchronous dependency.
7. Record which scale improvements are safe Batch 1 prerequisites and which
   remain decision-gated architecture expansion.
8. Preserve the mock-only freshness/reconciliation model as Local engineering
   evidence; the 2026-07-18 deferral removes any automatic first-version
   terminal-to-search `p95 <= 10s` commitment.
9. Freeze the first field proof as `N=1` owner-operated technical validation,
   the supported host profile, the fixture-then-sanitized sequence, the
   session-plus-seven-day trial retention default and Simplified Chinese UI
   copy.

### Batch 1 Proposed Product Scope

1. Bounded execution search/list API over persisted normalized evidence.
2. Keyset cursor pagination, stable error contract and hard input/page limits.
3. Source-qualified opaque execution identity with request ID retained as
   source-visible metadata.
4. Query-time, paged timeline derived from evidence events; one event per node;
   chronology and declared causality kept separate.
5. Same-origin execution search UI, execution timeline and selected-node/source
   detail.
6. Multi-label provenance and capture-gap panel.
7. Real approved UniRouter evidence proof plus clearly labeled synthetic
   semantic-state coverage.
8. Proportionate importer, database and HTTP resource bounds needed for this
   product slice.
9. Local and CI acceptance evidence, with Target/external/Production status
   reported separately.
10. The existing default-disabled/mock completed-execution consumer retained
    as Local engineering evidence; its live half is deferred as a whole and is
    not a current Batch 1 gate.

## Non-Goals

The following stay outside Batch 0 and proposed Batch 1 unless the User later
accepts a versioned change:

- LiteLLM, New API, one-api or broad adapter work;
- policy simulation or inline enforcement;
- UniRouter push exporter, automatic receipt transport or X-Insight callback;
- durable import jobs, retry/cancel/resume or partial-progress semantics;
- public collectors, adapter SDK or marketplace;
- raw prompt/response or governed raw payload storage;
- public or multi-tenant SaaS, public API exposure, HA/DR or multi-region;
- ClickHouse, Kafka, NATS, Redis Streams, object storage or graph database by
  default;
- X-IAM implementation, remote per-request PDP or synchronous X-IAM dependency;
- broad historical backfill, in-flight token/event streaming, SSE/WebSocket or
  a claim that partial timeline evidence is complete;
- Production verified or a 100x capacity claim; and
- Phase 1 reopening or deletion of retained receipts, hashes, sanitized
  evidence or persisted normalized evidence. The two time-bounded Gate A
  snapshot copies were separately cleaned after their retention deadline.

## Priority And Dependency Matrix

| Order | Capability/decision | Priority | Depends on | Unlocks |
| --- | --- | --- | --- | --- |
| 0 | Operator persona and investigation job | P1 | User decision | Search fields, empty states and acceptance narrative |
| 1 | Composite opaque execution identity | P1 | Source semantics; collision tests | Stable URLs, cursors and cross-source safety |
| 2 | Fixed search filters and sort/cursor contract | P1 | Existing schema/index review | Bounded list API and UI |
| 3 | Error, input and resource-limit contract | P1 | Performance budget | Safe API dependency and overload behavior |
| 4 | Timeline chronology/causality truth rule | P1 | Evidence event/parent semantics | Timeline API and node rendering |
| 5 | Provenance/gap presentation | P1 | Existing provenance enum and real fixture inventory | Differentiated investigation UI |
| 6 | Real versus synthetic proof matrix | P1 | Approved sanitized evidence | Truthful end-to-end claim |
| 7 | Frontend packaging and framework | P1 | Same-origin deployment decision | UI implementation/build/release changes |
| 8 | 1x workload, SLO and failure matrix | P1 before scale claim | Measured baseline | Hard limits and later 10x/100x gate |
| 9 | Authenticated proxy/multi-user access | P2 for current posture; P0 before trust expansion | Identity/RBAC/audit contract | Non-loopback or shared users |
| 10 | Completed-execution pull feed | Deferred from first formal version; Local mock retained | New User re-entry decision for the whole slice plus a jointly accepted UniRouter feed/auth/cursor contract | Possible later near-real-time visibility without gateway blocking |
| 11 | Durable jobs/broad transport/HA | Decision-gated P2 | Cross-project and data-lifecycle contracts | Continuous/high-volume operation beyond PRD2 |

P1 here means required for the proposed Phase 2 product slice or its honest
acceptance. It does not mean Phase 1 is incomplete.

## API Boundary Proposal

### Execution Search/List

Accepted and implemented product-level shape; the contract below summarizes
the bounded P2-1 API rather than reopening its frozen implementation details:

```text
GET /api/v1/executions
  ?request_id=<exact-or-prefix-bounded>
  &source_system_id=<opaque-id>
  &outcome=<accepted-enum>
  &has_gaps=<true|false>
  &from=<RFC3339>
  &to=<RFC3339>
  &limit=<bounded>
  &cursor=<opaque-server-token>
```

Rules:

- default and maximum limits are explicit; values over the maximum return a
  stable validation error rather than silently clamp;
- the cursor is opaque, versioned and bound to filters, sort direction and a
  fixed `as_of` boundary;
- sort is deterministic and has a unique tie-breaker;
- list items are narrow summaries and never include raw payload;
- `page_count`, `has_more` and an optional separately computed `total` must not
  confuse a truncated page length with the full count; and
- accepted filters receive explicit projection/index support. Arbitrary JSONB
  or full-text queries are out of scope.

### Execution Detail And Timeline

Accepted product-level resource split; exact DTO and paging schema remain to
freeze before implementation:

```text
GET /api/v1/executions/{execution_id}
GET /api/v1/executions/{execution_id}/timeline?limit=...&cursor=...
GET /api/v1/executions/{execution_id}/gaps?limit=...&cursor=...
```

Rules:

- `execution_id` is server-owned and source-qualified; raw `request_id` remains
  display/search metadata;
- one normalized evidence event produces one timeline node;
- chronology uses a stable time/sequence/ID key; parent edges are source-
  declared or contract-derived and never inferred from proximity;
- node details expose safe normalized field groups, source reference, adapter
  fidelity and provenance, never raw prompt/response; and
- gaps paginate independently and summaries remain accurate beyond one page.

### Stable Error Contract

Candidate categories:

| HTTP | Code | Meaning |
| --- | --- | --- |
| 400 | `invalid_argument` | Malformed filter, time range, enum or limit. |
| 404 | `execution_not_found` | Opaque execution does not exist in the current deployment. |
| 409 | `cursor_mismatch` | Cursor version/filter/as-of contract does not match the request. |
| 410 | `cursor_expired` | Cursor refers to a no-longer-supported snapshot/window. |
| 429 | `request_rate_limited` | Accepted request-rate boundary exceeded. |
| 503 | `temporarily_overloaded` | DB/concurrency capacity is saturated; retry is bounded and explicit. |
| 500 | `internal_error` | Stable public code and correlation ID; no raw internal error string. |

Exact status/code choices remain a Batch 0 decision. The invariant is that the
API never exposes `err.Error()` as its compatibility contract.

## UI Boundary Proposal

The first UI has two product routes and one shared detail surface:

1. **Execution Search** — five filters, deterministic result list, loading,
   empty, validation and overload states.
2. **Execution Detail** — paged timeline with explicit chronology and declared
   parent hierarchy.
3. **Selected Node Detail** — source reference, normalized safe fields,
   field-group provenance, suppression/unknown states and capture-gap linkage.

No generic KPI dashboard, policy editor, agent inventory, eval suite, raw
payload viewer or cross-product control console enters Batch 1.

The selected packaging is TypeScript + React compiled to static assets embedded
in the Go server. The exact React/toolchain versions, router, state/query
library and test tooling must be frozen before dependency files are added.

## Schema And Fact-Source Boundary

- Persisted normalized evidence remains the fact source.
- UI nodes, search summaries and future projections are derived/read models and
  must be rebuildable.
- Do not create a “UI node” ledger table merely to render the timeline.
- Execution identity may add indexed projection columns or a mapping table, but
  cannot rewrite historical source identifiers or fabricate run grouping.
- Preserve existing source refs, canonical hashes, provenance and capture-gap
  evidence.
- A source-type/schema placeholder is not an implemented adapter.
- Any future parent/run/session field needs source semantics, compatibility and
  fixture coverage before it becomes authoritative.
- No raw prompt/response, bearer token, secret or sensitive payload is added.

## Cross-Product Authority Boundary

| Concern | Current owner | Candidate future owner | Batch 1 rule |
| --- | --- | --- | --- |
| Model gateway auth/admission/routing/billing | UniRouter | UniRouter | No change or synchronous dependency. |
| Human/workload root identity | Current source/enterprise IdP context; no X-IAM runtime | Enterprise IdP/workload identity, optionally coordinated by a future X-IAM control plane | Record metadata only when contracted; do not build X-IAM. |
| Tool/data/export final side effect | Actual action owner | Same action owner as PEP | X-Insight cannot grant authority. |
| Evidence normalization/investigation/gaps | X-Insight | X-Insight | Advisory/read-only; sidecar is current fallback, completed-execution pull is conditional on a joint contract. |
| Policy publication/epoch/revocation | Not unified across three products | Decision pending | Do not introduce OPA/Cedar or a second truth source yet. |

If X-IAM is later built, the recommended shape is a control plane publishing
signed/versioned identity and policy snapshots. It must not become a remote
per-request PDP or X-Insight data store.

## Security, Data And Compatibility Risks

| Risk | Current control | Required before expansion |
| --- | --- | --- |
| Unauthenticated read surface | Loopback-only service, tunnel, sanitized metadata | Trusted identity, RBAC, tenant ownership, audit-on-read/export and TLS before non-loopback/multi-principal data. |
| Cross-source ID collision | Current raw request lookup is insufficient | Source-qualified opaque execution identity and collision fixtures. |
| Silent evidence truncation | Current 1,000 cap is unsafe for UI truth | Cursor, explicit limits, `has_more` and accurate counts. |
| Causal misrepresentation | Evidence times and source refs only | Explicit parent semantics; deterministic chronology with no inferred causality. |
| Query/JSONB amplification | Narrow current APIs | Fixed filters, narrow DTOs, measured indexes, pool/query/HTTP bounds. |
| Import memory/transaction amplification | Controlled fixture snapshots | Byte/event/event-size/concurrency caps; streaming/durable jobs remain gated. |
| Sensitive-data scope creep | Sensitive-key controls and metadata-only posture | Continue no raw; every new field/source undergoes data classification and fixtures. |
| Version drift | Current sidecar v1 boundary | Versioned API/cursor/error/schema contracts and compatibility fixtures. |
| Auth/evidence avalanche at 100x | UniRouter bounded capability/budgets; X-Insight async | Workload definition, hot-root/tenant tests, evidence lanes and failure/epoch contract before 100x claim. |

## P0/P1/P2 Judgment

### P0

- No newly verified P0 exists in the current Phase 1-closed, decision-only,
  loopback/fixture/manual-sidecar runtime posture.
- Before a persistent PRD2 field release, artifact trust/target checks,
  loopback listener, schema/migration/shared-writer-lock/crash recovery,
  backup/restore, truth-preserving UI and source-collision safety are P0 gates.
  If approved real sanitized metadata is used, encrypted storage/backups,
  custody, retention/trial exit and exact cleanup evidence are additional P0
  gates. These are conditional release requirements, not current implemented
  controls or newly discovered Phase 1 defects.
- Before any 100x/HA/Production claim, early overload isolation, authority and
  billing correctness, non-synchronous X-IAM/X-Insight boundaries, mandatory
  evidence survivability and selected DB/Redis failover/restore become release
  P0 gates.

### P1 For The Proposed Batch 1

- engineering freeze, implementation and executable evidence for the accepted
  B0-D01 through B0-D08 product decisions;
- bounded execution/cursor/error identity;
- importer/DB/HTTP hard resource limits;
- chronology-versus-causality semantics;
- real/synthetic proof separation;
- a reproducible 1x workload and performance/failure budget; and
- Local/CI evidence that does not overclaim Target or Production status.

### P2 Or Separately Gated

- additional filters, saved searches, eval/replay and projections;
- authenticated multi-user proxy while loopback/sanitized posture remains;
- durable jobs, push/in-flight/broad external transport and general
  retention/deletion automation;
- partitioning, read replicas, external queue, ClickHouse and HA/DR;
- X-IAM runtime, policy engine selection, graph store and multi-tenant SaaS.

## Batch 0 Acceptance Matrix

Rows D01-D31 were accepted by the User on 2026-07-13, D32 on 2026-07-14 and
D33 on the X-Insight/requesting side on 2026-07-19 as bounded product,
architecture or execution defaults. UniRouter's owner-authored response,
planning publication and exclusive-create correction are complete, and D33 is
now an accepted binding cross-project decision with completed execution-owner
evidence for its exact manifest-bound synthetic scenario. Decision acceptance
alone remains distinct from runtime evidence; D33's runtime result is supported
by the separately linked executable evidence.
On 2026-07-19 the User additionally authorized the X-Insight-owned preparation
in D34 for one internal, non-customer real-metadata batch. D34 does not yet
authorize UniRouter inventory/export or artifact/Target writes; those remain
behind an owner-authored response and an exact binding activation decision.
“Accepted” below means accepted product decision, not implemented or verified
runtime behavior. A qualifier in a row remains an unresolved parameter or
implementation choice and must not be inferred from acceptance of the boundary.

| ID | Decision | Default/result | Decision status | Executable evidence still required |
| --- | --- | --- | --- | --- |
| B0-D01 | First persona/job | Platform or on-call operator investigating a known/suspected execution | **Accepted; bounded API/UI workflow Local verified; human field proof pending** | Historical approved Gate A success/denial narratives pass search-to-gap/source locally; repeat with newly approved field evidence. |
| B0-D02 | Execution identity | Opaque source-qualified execution ID; request ID remains visible | **Accepted; backend merged; opaque UI URLs and fixed-snapshot browser path Local verified** | Repeat in release/field evidence. |
| B0-D03 | Access posture | Tunnel first; proxy only after trusted identity contract | **Accepted; server loopback enforcement and same-origin shell implemented** | Server/DB listeners remain loopback; browser path is same-origin. |
| B0-D04 | Search filters | Request/source ID, time, source, outcome, `has_gaps` only | **Accepted; backend/UI merged; all bounded filters and cursor browser flow Local/CI verified; Local PostgreSQL proves a late row with backdated `ingested_at` stays outside the fixed `as_of` snapshot and a later snapshot sees it** | Repeat only with newly approved field evidence; no additional X-Insight cursor-contract test remains. |
| B0-D05 | Timeline truth | One event/one node; chronology separate from declared causality | **Accepted; backend/UI merged; equal/reversed/parallel and explicit/unresolved-parent cases Local/CI verified** | Field narrative with newly approved evidence remains. |
| B0-D06 | Provenance presentation | Multi-label direct/derived/declared/suppressed/gap/unknown; `not_applicable` in field detail | **Accepted; all seven states have accessible Local/CI component/browser coverage** | Leaf-field provenance requires a future schema decision. |
| B0-D07 | Proof classes | Real approved UniRouter subset plus explicitly synthetic semantic fixtures | **Accepted; backend merged; legacy Gate A rows remain `unclassified`; fresh UR-E synthetic sidecar proof is exact-scenario External integration** | Approved sanitized-real field evidence remains separate and pending. |
| B0-D08 | Frontend packaging | TypeScript + React static assets embedded in Go; exact dependency versions freeze in the first UI code change | **Accepted; feature UI embedded and exact-toolchain deterministic Local/CI build verified; signed-v2 double-build, fresh/legacy clean-VM paths, supported-profile SIGKILL/recovery, exact owner-supervised rc.6 delivery, named-Target fixture proof and fresh UR-E real-UI proof verified** | `N=1` field evidence remains. |
| B0-D09 | X-IAM role | Research/contract only for now; no third runtime | **Accepted 2026-07-13; not implemented** | No X-IAM code/dependency or remote PDP in PRD2-M1. |
| B0-D10 | Failure contract | Identity/tenant/hard budget fail closed; signed LKG control state within TTL; analytics degraded with explicit gap | **Accepted 2026-07-13; not implemented; exact TTL/SLO pending** | Fault tests verify each dependency's behavior and no X-Insight outage blocks requests. |
| B0-D11 | Evidence data | Metadata only, no raw; mandatory versus diagnostic classes designed before continuous transport | **Accepted 2026-07-13; not implemented; class mapping pending** | Sensitive detector/fixtures pass; mandatory receipts are unsampled in stress tests. |
| B0-D12 | 100x requirement | Define and measure 1x before selecting 100x infrastructure | **Accepted; 100k/1m/200k sequential Local PostgreSQL 18 diagnostic recorded; supported PostgreSQL 16 host and 8-concurrent run pending** | Reproducible harness and supported-profile metrics; no offered-load/429-based 100x claim. |

B0-D08 selected React within the embedded same-origin packaging boundary. P2-0
implemented and CI-verified the exact dependency lock, shell and Go embedding;
merged P2-2/P2-3 implement the feature UI with Local and CI evidence.

## Round 3 Architecture And Innovation Decision Inputs

The 2026-07-13 identity/tool/cache and innovation analysis introduced the
following decisions. The User accepted their recommended boundary defaults on
2026-07-13. Acceptance does not add the dependent runtime capability.

| ID | Candidate decision | Recommended default | Status |
| --- | --- | --- | --- |
| B0-D13 | X-IAM runtime boundary | Do not implement now; if justified later, use an independent identity/policy control plane. Packaging/infrastructure may be shared initially, but process, database, keys and authorization authority remain isolated and shared-infrastructure failures are explicit. | **Accepted boundary; not implemented** |
| B0-D14 | Tool PEP boundary | The true sink/action owner contains the PEP; a Tool Gateway is one possible logically independent deployment and is never part of X-IAM or X-Insight. | **Accepted boundary; build/reuse/defer pending** |
| B0-D15 | Contextual action authorization | Identity/policy/resource authorities supply inputs; an action-owner-local PDP computes contextual authorization and the sink PEP enforces. X-Insight stores historical decisions; any future preview is non-binding. | **Accepted boundary; not implemented** |
| B0-D16 | Identity roots | Enterprise IdP owns human identity, an accepted workload authority owns workload identity, a tenant directory owns membership, and resource owners own resource facts. Future X-IAM may federate but not replace them; UniRouter authority stays audience-scoped. | **Accepted boundary; not implemented** |
| B0-D17 | Revocation semantics/SLO | Workflow revoke rejects new admission after committed shared-hard-state read-after-write; in-flight cancellation is separate. Other domains measure authority commit through every required PEP's rejection. UniRouter's current 15-second capability root cache is only a candidate input, not a universal SLO. | **Accepted boundary; exact SLO pending** |
| B0-D18 | Mandatory receipt failure | A future high-impact action requires action-owner-local durable acceptance of its mandatory receipt; X-Insight delivery stays asynchronous. | **Accepted boundary; action classes and spool/disk/outbox behavior pending** |
| B0-D19 | Semantic evidence mode | Begin metadata-only structural analysis; source-local signed semantic assertions and any governed raw-content mode each require a later privacy/versioned contract. A signature proves issuer/integrity, not semantic correctness. | **Accepted boundary; later contracts pending** |
| B0-D20 | Route feedback authority | X-Insight may create a versioned route proposal with evidence/manifest digest; only UniRouter's route authority approves, canaries, activates and rolls back under a later contract. | **Accepted boundary; no write-back implemented** |
| B0-D21 | Policy feedback authority | X-Insight may create a versioned policy proposal; the relevant tenant policy or resource authority approves, and the sink owner enforces only its own domain. | **Accepted boundary; no write-back implemented** |
| B0-D22 | Cache invalidation/cleanup | Authority commits/publishes an epoch/tombstone; a PEP rejects old entries only after observation within an accepted SLO. Cache owner performs physical cleanup. A future X-Insight preview cannot clear another system's cache. | **Accepted boundary; exact SLO/transport pending** |

## PRD2 Implementation-Kickoff Decisions

The User accepted the following bounded defaults on 2026-07-13 and authorized
Codex to implement all X-Insight-owned work until a real product decision or
UniRouter cooperation is required. “Accepted” is a product decision, not a
runtime or external-integration claim.

| ID | Decision | Accepted default | Status and executable boundary |
| --- | --- | --- | --- |
| B0-D23 | Field proof participants | `N=1`: the User/product owner performs the human investigation task. Codex implements, facilitates and records, but is neither a human participant nor an independent acceptance reviewer. Customer IT supplies host/access facts when requested rather than becoming a required usability participant. | **Accepted; field proof not run** |
| B0-D24 | Supported field host | Ubuntu 22.04 LTS x86_64; one private host; dedicated pinned PostgreSQL 16 Compose; X-Insight under systemd; both listeners loopback-only; approved private artifact upload; not fully air-gapped. | **Accepted product profile; customer-host confirmation and new Target qualification still required** |
| B0-D25 | Field data sequence | Fixture-only smoke first, then one newly approved sanitized UniRouter metadata set in a dedicated trial database; never raw prompt/response or secrets. | **Accepted; UR-E synthetic proof complete, but sanitized-real field input still requires separate authorization/custody qualification** |
| B0-D26 | Trial retention | Retain the dedicated sanitized trial database and its related backups through the field session plus seven calendar days; then the named custodian explicitly chooses handoff or controlled retirement. No automatic, recursive, wildcard or bulk deletion. | **Accepted; lifecycle automation remains out of scope** |
| B0-D27 | Integration direction | X-Insight pulls a bounded feed of completed UniRouter executions. The current sidecar remains the manual fallback. UniRouter never waits on or calls X-Insight, and X-Insight failure never blocks routing. | **Historical accepted direction; whole live slice deferred on 2026-07-18; strict mock remains Local-only and sidecar is binding** |
| B0-D28 | Search freshness target | A completed execution should become searchable at diagnostic `p95 <= 10s`, measured from the UniRouter terminal-availability timestamp to successful X-Insight search visibility. This is a field target, not a Production SLA. | **Historical mock target retained as Local evidence; automatic first-version commitment removed by deferral** |
| B0-D29 | Timeline reconciliation truth | Search visibility may precede full timeline convergence. API/UI must expose `syncing`, `current`, `stale` or `gap` state, a revision and source/import watermarks; partial evidence must never be labelled complete. | **Accepted; backend/UI merged and Local/CI verified; gap-bearing imports remain explicitly incomplete** |
| B0-D30 | Activation and backfill | The pull feed starts at an explicitly approved activation cursor/time and covers new completed executions. It performs no broad historical auto-backfill. Historical proof uses retained normalized evidence or an explicitly approved bounded sidecar artifact. | **Historical direction; live activation deferred with the whole slice and not a first-version gate** |
| B0-D31 | First UI language | Product navigation, labels, help and error explanations are Simplified Chinese (`zh-CN`). Canonical protocol names, JSON fields, CLI flags and stable error codes remain English. No general i18n framework enters PRD2. | **Accepted; feature UI and PR #35 release-matched manual merged with Local and packaged clean-VM CI evidence; manual-guided User field task pending** |
| B0-D32 | First-customer release authority | Use an `owner_supervised_trial` release generated only from exact current `main` after its successful push CI. A one-run ephemeral Ed25519 key signs the existing v2 bundle; its exact private-key file is removed before upload and is not retained. Delivery contains the artifact, bootstrap verifier, public trust store, checksums and workflow/CI-bound record. The User and Codex perform every install/upgrade; the customer cannot self-upgrade. | **Accepted; exact rc.6 main `0ca502f...`, source CI `29648815418`, workflow `29648976737`, strict delivery hashes and named-Target upgrade/rollback-readiness proof passed; durable publisher authority remains deferred before second-customer/self-service use.** Current first-customer supervised authority has executable evidence; a second customer, delegated install or unattended upgrade requires a new durable-authority decision and evidence. |
| B0-D33 | UR-E bounded synthetic sidecar execution | One genuinely new manifest-bound metadata-only synthetic snapshot-v2 artifact, with fixed producer-code and clean-generation identities recorded separately; one private handoff; one named-Target first-write plus identical-byte replay; promotion limited to the actual scenario inventory; normalized synthetic evidence, receipts and hashes retained. | **Accepted, binding and execution-owner verified for the exact manifest-bound one-call/zero-trace/one-gap synthetic scenario.** Freshness/custody, pre-existence, first-write, identical replay, receipt/report, SQL/API/real UI and retention disposition all passed. This is not independent acceptance, `N=1`, live feed, customer data or Production. |
| B0-D34 | Internal-real pre-field metadata proof | Prepare one manifest-bound snapshot-v2 batch containing `1..20` project-owned, non-customer request IDs and at most 200 source rows from one org; run value-aware no-persist preflight; use a dedicated encrypted local PostgreSQL 16 trial database; preserve sidecar-only transport and exact first-write/replay/API/UI/retention evidence. | **Accepted; receiver preparation merged; narrow raw-IP policy adaptation merged in UniRouter and matching X-Insight compatibility implemented in this revision.** B2a readiness and B2b bounded inventory remain separately gated. No internal-real artifact, transfer, import or Target write exists; B3 remains unauthorized and the named Target remains fixture/synthetic-only. |

B0-D32 intentionally distinguishes release integrity from a durable commercial
publisher identity. The GitHub workflow identity, exact-main commit, successful
matching main CI and independently rechecked SHA-256 values provide the
supervised bootstrap for the first customer. The ephemeral public key cannot be
used to authorize a later release. Before a second customer, delegated
installer or unattended/self-service upgrade, the User must accept a persistent
KMS/HSM, keyless or offline authority and its recovery/revocation process.

B0-D33 used one conditional two-stage joint decision. UniRouter generated and
handed off exactly one fresh synthetic artifact; the handoff bound its request,
manifest, producer, scenario, custody and SHA-256 identities. X-Insight passed
the receiver/Target gate, wrote that artifact once to the named non-production
Target and replayed the identical bytes once. SQL/API/real UI and final
reconciliation matched the exact one-call/zero-trace/one-gap inventory. The
result is execution-owner External integration for that exact manifest-bound
synthetic scenario, not independent acceptance, field usability, live
freshness, customer/sanitized-real data or Production. Another artifact still
requires new authority.

B0-D34 now includes one User-authorized source exception, not a general IP
policy change. Only the designated `public.call_logs.ip_address` column in one
exact project-owned, non-customer, non-Production current-schema source may be
evaluated inside the database for presence. The raw string must not cross the
query-result boundary; artifact and X-Insight retain only the two boolean
proofs. The strict path accepts `false/false` and `true/true` and rejects both
mismatches before persistence. Historical declaration compatibility is limited
to `false/false`. This neither proves source deletion/modification nor general
DLP, and it does not prove the database principal lacks base-column read
capability. B2a reads no source row/value; B2b remains one later bounded
aggregate transaction and cannot activate B3.

## Proposed Implementation Order

### Batch 0A — Accepted Product Direction; Implementation Contract Freeze

B0-D01 through B0-D08 and B0-D23 through B0-D32 are accepted and implemented
or tracked under their existing evidence boundaries. B0-D33 is accepted,
binding and complete for its exact manifest-bound synthetic scenario after the
sequential Stage A, Stage A.5 and Stage B gates. B0-D34 is accepted only for
its bounded preparation and narrow source-policy chain; B2a, B2b and any later
artifact/import activation remain separate gates. For the historical feature
path, freeze the remaining
engineering details: opaque-ID encoding/versioning, exact filter comparison
semantics, cursor/error schemas, numeric limits, UI information architecture
and component-framework dependencies. The live portion of B0-D27 is deferred
from the first formal version and cannot be implemented piecemeal.

### Batch 0B — Accepted Scale And Authority Boundaries; Parameters Deferred

B0-D09 through B0-D22 are accepted as boundary defaults. They do not block the
bounded Batch 1A/1B slice unless that implementation touches their dependency;
the existing non-goals forbid such expansion by default. D10/D12 resource and
failure measurements remain required before Batch 1 completion. Exact SLOs,
action classes, spool semantics, privacy/versioned contracts and Tool Gateway
implementation choice remain decision-gated. No cross-project runtime change
is authorized.

### Batch 1A — Bounded Backend Foundation

Execution identity/search/list, keyset cursor, error DTO, exact input limits,
narrow SQL projections, measured indexes and importer/DB/query/HTTP bounds were
delivered by merged P2-1 PR #30 after Local and PostgreSQL 16 CI verification.

### Batch 1B — Investigation UI

The selected TypeScript component application, embedded static assets,
search/timeline/node/source/gap views and empty/loading/error/degraded states
were delivered by merged PR #31 after Local and GitHub CI verification.

### Batch 1C — Product And Scale Evidence

Run real approved UniRouter evidence through the product; run named synthetic
semantic fixtures; execute paging/collision/causality/load/fault cases; record
Local and CI separately. A Target run occurs only when separately in scope.

### Batch 1D — Historical/Deferred Completed-Execution Pull

The default-disabled consumer was implemented and verified against a
deterministic mock feed. That result remains Local engineering evidence only.
The whole live half is deferred from the first formal version; there is no
current endpoint/auth/cursor/polling subtask to implement. Future re-entry
requires a new User decision accepting the complete minimum slice and a jointly
versioned contract, while the sidecar boundary remains binding until then.

### Later Decision-Gated Batches

Push/in-flight/broad continuous transport, durable jobs, automated retention/
deletion, authenticated multi-user access, X-IAM control-plane implementation,
policy distribution, partitioning/external stores and HA/DR each require a new
accepted boundary.

The exact field implementation/release plan was accepted and X-Insight-owned
implementation authorized on 2026-07-13; it is maintained separately in
[PRD2-M1 UniRouter Execution Investigation Field Proof](prd2-m1-unirouter-field-validation-implementation-plan-2026-07-13.md).

## Required Evidence Before Batch 1 Is Complete

- API schema/golden/error compatibility tests.
- Boundary/property tests for filters, page size, cursor mismatch/expiry and
  fixed-`as_of` paging.
- Same request ID across source systems, one run with many requests, equal or
  reversed clocks and explicit/no parent-edge fixtures.
- Real-versus-synthetic evidence coverage inventory.
- Sensitive-key and no-raw regression.
- Import byte/event/concurrency rejection tests and duplicate/conflict storm.
- DB query-plan evidence for accepted filters; pool/timeout/overload behavior.
- UI component and browser end-to-end coverage for search, pagination,
  timeline, node/source/provenance/gaps and error/empty/degraded states.
- Go/static/release regression and deterministic asset packaging.
- Mock pull-feed conformance, failure-isolation, activation-watermark,
  revision/replay and diagnostic freshness measurement remain Local engineering
  evidence; no external feed result is a current first-version gate.
- 1x baseline plus bounded 10x exploratory run if useful; no 100x or Production
  claim without its full acceptance matrix.
- GitHub CI and merged-main evidence. Local owner verification must not be
  described as independent acceptance.

## Decision Inputs

- [Competitive Alignment And Recommendation Rationale](../discussion/phase-2-competitive-alignment-and-recommendation-rationale-2026-07-13.md)
- [Three-Product Scalability Anti-Pattern Audit](../discussion/unirouter-x-iam-x-insight-scalability-antipattern-audit-2026-07-13.md)
- [X-IAM, Tool Gateway And Cache Authority Boundary](../architecture/phase-2-xiam-tool-gateway-cache-authority-boundary-candidate-2026-07-13.md)
- [X-Insight Innovation And Golden Boundary Matrix](../discussion/phase-2-xinsight-innovation-and-golden-boundary-2026-07-13.md)
- [Phase 2 Product Priority](phase-2-product-priority.md)
- [PRD2-M1 UniRouter Execution Investigation Field Proof](prd2-m1-unirouter-field-validation-implementation-plan-2026-07-13.md)
- [PRD2 UR-E Bounded Synthetic Sidecar Execution Plan](prd2-ur-e-bounded-synthetic-sidecar-execution-plan-2026-07-19.md)
- [PRD2 Internal-Real UniRouter Metadata Validation Plan](prd2-internal-real-metadata-validation-plan-2026-07-19.md)
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-20-prd2-internal-real-raw-ip-policy-adaptation-b2-retry-authorization.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/requests/2026-07-20-x-insight-prd2-internal-real-raw-ip-policy-adaptation-b2-retry-request.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/responses/2026-07-20-unirouter-prd2-internal-real-raw-ip-policy-adaptation-implementation-response.md`
- [UR-E Joint Activation Evidence](../evidence/prd2/ur-e-joint-activation-2026-07-19.md)
- [UR-E Exact Synthetic External Integration Evidence](../evidence/prd2/prd2-ur-e-exact-synthetic-external-integration-evidence-2026-07-19.md)
- [PRD2 P2-0 API And UI Engineering Contract](../architecture/prd2-p2-0-api-ui-engineering-contract-2026-07-13.md)
- [Runtime Contract Capability Status](../architecture/runtime-contract-capability-status.md)
- [P2-4 RC.6 Named Target, V2 Sidecar And UI Evidence](../evidence/prd2/p2-4-rc6-named-target-v2-sidecar-and-ui-evidence-2026-07-19.md)
- [M2 Timeline UI Information Architecture Candidate](../discussion/prd1-m2-timeline-ui-information-architecture-candidate.md)
- [M2 Wireframe And Component Candidate](../discussion/prd1-m2-wireframe-component-candidate.md)
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/requests/2026-07-19-x-insight-prd2-v2-sidecar-external-integration-artifact-request.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-19-prd2-ur-e-bounded-synthetic-sidecar-execution.md`
