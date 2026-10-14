# P2-2/P2-3 Investigation UI Local Evidence

Status: Complete; Local and GitHub CI verified; PR #31 merged
Date: 2026-07-13
Owner: Codex
Branch: `codex/prd2-p2-2-investigation-ui`
Base: `b754100a4eee9024707fe9c189c71ee579966a04`

## Claim Boundary

This record verifies the X-Insight-owned P2-2/P2-3 investigation UI through
owner Local checks and GitHub CI. It is not independent acceptance. It does not
claim PostgreSQL 16 Target, current UniRouter live-feed conformance, field
usability, External integration or Production verification.

The implementation remains internal, single-deployment-tenant and
loopback-only. It requests allowlisted metadata DTOs only and has no raw
prompt/response viewer. UniRouter does not call or wait for this UI.

## Implemented Product Workflow

- Embedded same-origin `zh-CN` execution search and execution-detail routes.
- Exact or prefix request ID, time, source, outcome and `has_gaps` filters.
- Fixed-`as_of` cursor navigation with page counts rather than an invented
  total result count.
- Execution proof/assurance/freshness summary with generated/imported/search
  timestamps, revisions and source/import watermarks.
- Evidence-event timeline ordered by the API chronology. A parent appears only
  when the source declared it and the API resolved it; unresolved parents say
  that causality is not inferred.
- Selected-event allowlisted correlation, identity, capability, decision,
  usage, outcome and payload-policy detail.
- Field-group provenance and accessible `direct`, `derived`, `declared`,
  `suppressed`, `gap`, `unknown` and `not_applicable` labels.
- Source system, adapter manifest, schema, source type/version/event ID,
  source ref, event/source hash and fidelity inspection.
- Independent capture-gap collection with confirmed type, affected claim,
  reason, recommendation and separately labelled candidate types.
- Loading, empty, stable-error, source-list-degraded and narrow-browser states.
- Conservative `synthetic_fixture`, `operator_approved_integration` and
  `unclassified` copy; no badge is promoted from source name or UI context.

## Evidence Classes Used

| Evidence | Purpose | Classification and limit |
| --- | --- | --- |
| Deterministic browser/component fixture | Exercise every semantic state, reversed/equal timestamps, parallel events, resolved/unresolved parents, stable errors and a hostile extra `raw_source_excerpt` field | Explicitly synthetic; proves UI semantics only, not an integration |
| `xinsight_prd2_integrity_20260713` | Exercise the embedded UI through the real Go API and local PostgreSQL 18 | Local synthetic/unclassified development data; not PostgreSQL 16 or Target |
| `xinsight_gatea` | Read-only source of the 12 retained normalized events and five gaps from the two approved Phase 1 Gate A cycles | Previously approved historical UniRouter evidence; not a new live integration run |
| `xinsight_prd2_gatea_ui_20260713` | Explicit investigation copy of `xinsight_gatea`, upgraded from migrations 1-9 to 1-11 | Local derived database for product proof; the Phase 1 source database was not migrated or rewritten |

The original `xinsight_gatea` database was checked after the copy and remained
at nine migrations with 12 events and five gaps. The PRD2 copy received only
migrations 10 and 11; because those legacy events predate immutable
`import_provenance`, the API and UI correctly classify them as `unclassified`.
The historical approval remains in Phase 1 evidence records, but the UI does
not infer a machine proof class retroactively.

## Executable Verification

### Exact frontend toolchain

The pinned toolchain was invoked through fixed packages rather than the host's
default Node installation:

```text
node v22.23.1
npm 10.9.8
TypeScript check: pass
OpenAPI six-path validation: pass
Vitest: 6/6 pass
npm audit --audit-level=high: 0 vulnerabilities
locked license allowlist: 177 entries pass
deterministic Playwright: 2/2 pass
```

The browser suite runs one 1440x1000 Chromium project and one 390x844 narrow
Chromium project. It uses normal pointer interactions, not forced clicks or
scripted DOM activation.

Two consecutive exact-toolchain builds produced identical committed files:

```text
ccf2f55f89c56868ace72bf7c564a2f6052aeace2b054e6a763969ec8e42a4d3  internal/webui/dist/index.html
1974402d92634c4de39bb1c8982fc7b0dac95dfef30f4f7ff07b98c1749e7dde  internal/webui/dist/assets/index-VS_5ofXC.js
79a86076d35189a6d4bfc5b6d3223661b589b640aea1976b1913aea8476c1593  internal/webui/dist/assets/index-pndZqYsI.css
```

The code/UI commit `bde01a0260ff166fe969aba1ff0fd65468f93353` also
passed the repository's commit-addressed Linux release builder and verifier on
the host Go `1.25.0` toolchain:

```text
artifact_sha256=sha256:2969da919cc36515b60908a284c5c71930565fef19dcf066af201b6a29df08e6
manifest_sha256=sha256:9a2d640b4fc68caa5b1a25fa4799aef40df4b1f8624191478a0678deacc2c841
file_count=56
```

This local artifact is owner self-verification, not the CI or Target artifact.

### Component and deterministic browser coverage

Component tests verified:

- bounded filter serialization and cursor preservation;
- all seven semantic labels and proof/freshness text;
- chronology order versus exactly two declared-parent displays across four
  nodes, including equal times, a reversed source clock and an unresolved
  parent;
- source/hash/schema/fidelity and confirmed/candidate gap display;
- hostile response data containing `TOP-SECRET-HOSTILE-PROMPT` is not rendered
  and no API request contains `raw`;
- source-list failure degrades independently from execution search;
- empty results state that absence is not completeness;
- a stable overload problem maps to safe Chinese copy and correlation ID
  without rendering the server title; and
- loading state before a fixed snapshot resolves.

The deterministic browser test repeated the search-to-node path on desktop and
narrow viewports. It also asserted that the document has no viewport-wide
horizontal overflow after node selection.

### Real Go API and PostgreSQL browser coverage

The reusable real-API Playwright test passed once against the local integrity
database, then against both approved historical Gate A narratives:

1. `gatea-model-denied-b1w-283194e67a0c3ff9`: two persisted events;
   `auth.model.rejected`; confirmed `permission_denied` and
   `not_available_from_source` gaps; candidate and associated gap-event/source
   inspection passed.
2. `gatea-success-bfcd47b2b0679f8d`: eight persisted events beginning with
   gateway admission; confirmed `provider_not_returned` limitation associated
   with the provider event; event/source inspection passed.

Both paths began at the UI search form, used the opaque `xe1_` detail URL with
a fixed `as_of`, opened timeline and gap content, selected an associated event,
and inspected source ref/hash/schema/fidelity. The browser recorded no console
or page errors and no API path containing `raw`.

### Go/evidence-spine regression

```text
go vet ./...                         pass
go test -count=1 ./...               pass
go test -race -count=1 ./...         pass
go run ./cmd/xinsight-importer fixtures run   11/11 pass
```

These commands ran without a PostgreSQL DSN; DB-backed Go integration was
covered by P2-1 Local evidence and reran on GitHub's PostgreSQL 16 service. The
real-browser paths above separately exercised the Go API against local
PostgreSQL 18.

### GitHub delivery

PR [#31](https://github.com/foigent/x-insight-private-history/pull/31) passed all jobs in
Actions run `29258723199`:

- `Frontend Tests`: exact locked install, TypeScript/OpenAPI, 6/6 component
  tests, zero high-severity audit findings, 177 license entries, deterministic
  assets and 2/2 Chromium desktop/narrow tests;
- `Backend Tests`: PostgreSQL 16 migrations/integration, Go test/race/vet and
  11/11 fixture conformance; and
- `Linux Release Artifact`: Go `1.25.11` build/verification for PR merge ref
  `9c4479de22d07345ac7906eb4ffc064fda3ac207`, artifact SHA-256
  `3771175f280190a8749060c109684504e336f118c6c23a943aceea7f091e1148`
  and manifest SHA-256
  `ac79968d751c9f7e05d55fac61b88d0fc46fa230ee73578a96e94581e0712085`.

The PR was squash-merged to `main` as
`582e9692fc0d35f2e93c4105a891e09207bb5f8d`. The CI archive is evidence for the
PR merge ref, not a promoted field release for the squash commit; P2-4 owns the
SemVer field artifact and upgrade qualification.

## P2-2/P2-3 Exit Matrix

| Exit requirement | Local result | Evidence |
| --- | --- | --- |
| Five accepted filters and fixed-snapshot paging | Pass | Component URL/cursor assertions and deterministic browser request assertions |
| Search to selected node | Pass | Desktop, narrow and real-API browser paths |
| Chronology does not invent causality | Pass | Equal/reversed/parallel fixture; only explicit resolved/unresolved parent displays |
| Source/provenance/gap inspection | Pass | Component, deterministic browser and real PostgreSQL browser paths |
| Seven semantic states accessible | Pass | Component plus desktop/narrow browser coverage with text and canonical codes |
| Proof/freshness truth | Pass | Synthetic, unclassified and gap/current vocabulary; no retroactive Gate A proof promotion |
| No raw/hostile boundary | Pass | Extra hostile response field omitted; allowlist note and API-request inventory |
| Loading/empty/error/degraded | Pass | Six component tests |
| Embedded deterministic build | Pass | Repeated exact-toolchain SHA-256 comparison |
| GitHub CI and merged main | Pass | PR #31, Actions `29258723199`, squash commit `582e9692fc0d35f2e93c4105a891e09207bb5f8d` |

## Remaining Work And Stops

- P2-2/P2-3 are delivered by merged PR #31; their product status remains
  separate from field release and usability evidence.
- PostgreSQL 16 field-host qualification, release artifact, managed upgrade,
  restore drill and Chinese manual belong to P2-4.
- The User's `N=1` human field investigation has not run.
- A newly approved sanitized metadata artifact and customer host confirmation
  remain field inputs; the historical Gate A proof above is not a new intake.
- Default-disabled/mock completed-execution pull closure remains X-Insight-owned
  work. Live endpoint/auth/cursor/revision implementation stops until UniRouter
  accepts a versioned contract.
- Target, External integration and Production remain unverified for PRD2.

## Traceability

- [PRD2-M1 implementation plan](../../planning/prd2-m1-unirouter-field-validation-implementation-plan-2026-07-13.md)
- [Phase 2 Batch 0 register](../../planning/phase-2-batch-0-scope-and-decision-register-2026-07-13.md)
- [P2-1 local backend and 1x diagnostic](p2-1-local-backend-and-1x-diagnostic-2026-07-13.md)
- [Runtime capability status](../../architecture/runtime-contract-capability-status.md)
- [Execution record](../../agent-workflows/EXECUTION-RECORD.md)
