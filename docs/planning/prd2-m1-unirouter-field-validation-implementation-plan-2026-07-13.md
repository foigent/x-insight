# PRD2-M1 UniRouter Execution Investigation Field Proof

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

Status: exact implementation/release plan and kickoff defaults accepted;
P2-0 through P2-4 merged; exact rc.6 has Local, signed CI, fresh/legacy clean-VM,
owner-supervised publication, named-Target upgrade/rollback readiness and exact
v2 synthetic sidecar/API/UI evidence. The live feed slice is deferred; P2-5
controlled-sidecar External integration is execution-owner verified for the
exact manifest-bound synthetic scenario; the accepted `N=1` field tasks remain
pending. B0-D33 `UR-E` is accepted, binding and complete for that contained
one-call/zero-trace/one-gap scenario only. B0-D34 authorizes X-Insight-owned
preparation for one internal, non-customer real-metadata batch; no such source
artifact has yet been generated or imported.
Prepared: 2026-07-13
Accepted: 2026-07-13
Decision owner: User
Execution owner: Codex
X-Insight planning baseline: `a8c25167202d0dd9180d9437f44dcdb25fca48fc`
X-Insight implementation-kickoff baseline:
`76f3f0ebe9e3a83598fe17af5067fe9ccd003789`
UniRouter identity reconciliation: historical planning observation
`f9925779f1da7681b07282be5d500773a687d3b9`; response observation baseline
`f96fb0394a8dd523aed82e2163bf314711c366bd`, U0 planning main `c30f9a6...` and
correction main `9a94d54975017de3b6b209d47f561af32c072178` are docs-only; qualified
producer code identity is `5176654d7d320b56c1c136f39b3debb8156ece4b`
with required CI `29640791281`. These are planning/execution identities, not an
X-Insight integration or Production claim.

## Executive Summary

PRD2-M1 should deliver one complete, field-testable operator workflow before
X-Insight participates in a field trial beside UniRouter. It does not block or
define UniRouter's own release schedule:

> Starting with a UniRouter request or source identifier, an operator can find
> the corresponding X-Insight execution, explain the ordered evidence without
> inventing causality, inspect a selected node's safe source/provenance facts,
> identify capture gaps and clearly state what is known, unknown or suppressed.

The first version includes all accepted investigation capabilities: execution search,
execution detail, evidence timeline, selected-node/source/provenance detail and
capture-gap inspection. It uses TypeScript and React compiled into same-origin
static assets embedded in the Go server. It remains an internal, single-
deployment-tenant, loopback-only product accessed through an approved tunnel.

The first formal version's field proof uses controlled, operator-mediated
sidecar export/import. A live X-Insight pull feed is deferred as a whole. It
does not depend on X-Insight from UniRouter's request path and does not add
UniRouter push, automatic receipt transport, in-flight streaming, deep links,
X-IAM, Tool Gateway, raw payload capture, multi-tenant SaaS or Production
claims. The existing disabled/mock consumer remains Local engineering evidence,
not a live transport or current field gate. Re-entry requires a new decision
accepting the whole minimum slice and a jointly versioned contract.

Two operational deliverables are part of initial field readiness rather than
postponed polish:

1. a single-node, versioned and rollback-aware upgrade workflow; and
2. a release-matched, offline-capable Simplified Chinese product manual.

The plan is organized by readiness gates rather than an invented calendar.
Once the field date, host profile and data-custody parameters are confirmed,
the gates can be mapped to dates without changing product scope.

## 2026-07-13 Acceptance And Field Clarifications

The User accepted this exact PRD2-M1 implementation/release plan and authorized
Codex to continue X-Insight-owned work through a genuine product-decision or
UniRouter-cooperation boundary. Acceptance by itself is not implementation
evidence; current delivery state is recorded separately below.

“Joint field window” means the same planned customer/operator test period as
UniRouter, so one approved UniRouter request/evidence artifact can be examined
through X-Insight while the relevant people are available. It does **not** mean:

- UniRouter must wait for X-Insight to release;
- the two products share a process or database;
- an accepted product direction is already an implemented live transport; or
- unmerged UniRouter work becomes an X-Insight dependency.

The two products retain independent readiness gates. X-Insight joins the common
field session only after `XI-R`. The accepted `UR-E` decision authorized one
exact sidecar artifact; its evidence has now been imported and recorded, so the
current External-integration result applies only to that manifest-bound
synthetic scenario. It does not make either product wait for unrelated work or
replace the separate `N=1` field gate.

On 2026-07-19 the User accepted the requesting-side B0-D33 defaults in the
[UR-E bounded synthetic execution plan](prd2-ur-e-bounded-synthetic-sidecar-execution-plan-2026-07-19.md).
The plan defines one conditional decision with UniRouter Stage A generation/
handoff followed by X-Insight Stage B Target proof: first-write uses one
genuinely new manifest-bound artifact and replay uses the identical bytes and
SHA-256. Both authoritative planning links and the exclusive-create correction
are merged through independent PR/CI. The joint decision became accepted and
binding at `2026-07-19 12:15:26 CST`; UniRouter completed Stage A and X-Insight
then passed Stage A.5 and Stage B for the exact contained scenario.

The human field participant is the User/product decision owner (`N=1`). Codex
is the single implementation owner, test facilitator and evidence recorder;
Codex is not counted as a human usability participant or independent acceptance
reviewer. Customer IT supplies required host/access facts when requested but is
not a mandatory usability participant. Results are an owner-operated technical
field proof, not a statistically generalizable study.

The same acceptance freezes the supported field host profile, the fixture then
sanitized-data sequence, retention through the field session plus seven days,
Simplified Chinese product copy, and a mock-only diagnostic completed-execution
search visibility measurement. The 2026-07-18 deferral removes an automatic
first-version `p95 <= 10s` field commitment. Timeline evidence may reconcile later, but
the product must expose revision/watermark and `syncing`, `current`, `stale` or
`gap` state; it must never present partial evidence as complete.

On 2026-07-14 the User accepted the first-customer release authority. The User
and Codex perform every installation and upgrade, and the customer does not
self-upgrade. PRD2 therefore uses a manually triggered
`owner_supervised_trial` workflow: it accepts only exact current `main` with a
successful matching push CI run, generates one ephemeral Ed25519 key, builds
the existing signed-v2 bundle, removes the private key before upload and emits
only the artifact, bootstrap verifier, public trust store, checksums and a
workflow/CI-bound record. The User never creates, stores or uses a key. This is
not a persistent commercial publisher identity; a second customer, delegated
installer or unattended upgrade is the explicit durable-authority revisit
trigger.

## Implementation Progress

- P2-0 contract/toolchain/same-origin foundation was Local and CI verified and
  squash-merged in PR [#29](https://github.com/foigent/x-insight/pull/29) as
  `4b6dca44a0947c22bd4a6059a0c96fbb475c4da8`.
- P2-1 migration, provenance, bounded investigation APIs and resource limits
  passed Local and PostgreSQL 16 CI verification and squash-merged through PR
  [#30](https://github.com/foigent/x-insight/pull/30) as
  `b754100a4eee9024707fe9c189c71ee579966a04`.
- The P2-1 diagnostic loaded 100,000 executions, 1,000,000 events and 200,000
  gaps on local PostgreSQL 18. Warm sequential search p95 was `247.093 ms` after
  fixing a generic-plan regression. This is not PostgreSQL 16 VM qualification,
  Target, concurrent-load or Production evidence.
- P2-2/P2-3 feature UI passed deterministic semantic tests, real Go API/
  PostgreSQL browser tests, local historical Gate A success/denial product
  reads and GitHub CI. PR
  [#31](https://github.com/foigent/x-insight/pull/31) squash-merged as
  `582e9692fc0d35f2e93c4105a891e09207bb5f8d`. P2-4 release/upgrade/manual
  subsequently merged through PR #35; the `N=1` P2-5 field proof remains
  pending. Live pull transport is deferred as a whole from the first formal
  version; re-entry requires a new User decision and a joint versioned
  contract.
- P2-4 GitHub run
  [29278281737](https://github.com/foigent/x-insight/actions/runs/29278281737)
  passed exact-toolchain signed double-build, exact Phase 1 artifact
  reproduction, legacy adopt/upgrade/application rollback/re-upgrade/isolated
  restore on one clean Ubuntu 22.04/PostgreSQL 16 VM and fresh install plus
  synthetic import/readback on a second. Both host results are
  `ci_clean_vm_not_target`; they do not qualify the named field host.
- The accepted owner-supervised trial release path is implemented as a
  separate `workflow_dispatch` gate. It leaves ordinary PR/push artifacts
  explicitly non-publishable, requires exact current `main` plus successful
  matching push CI, excludes the ephemeral private key from its strict delivery
  file set and records Target/External/Production as false. PRs #40 through #42
  then added all-operation durable recovery cases and the supported-profile
  external SIGKILL proof. Recovery-capable exact main
  `a77ba1de2064d6b297c1010de708019e46fd61cb`, source CI `29301064665` and
  supervised workflow `29301384798` passed.
- The named-Target corrective chain then closed through rc.5. PR #49 merged
  exact main `27e05df26ff7976301a245f9120176e6ca70430d`; main CI `29348263612`
  and owner workflow `29348552251` passed. The exact rc.5 artifact passed
  Target upgrade, application rollback, re-upgrade, explicit restart and a
  real browser search-to-detail/timeline/node/provenance/gap/manual proof over
  Alibaba Cloud Session Manager. Post-proof inspection rejected the direct
  CLI's wildcard local listener; a replacement using Session Manager only as
  an OpenSSH stdio proxy plus an explicit OpenSSH loopback bind passed. This
  closes the P2-4 named-Target gate for the accepted owner-supervised
  approved-metadata scope, not External integration, field usability or
  Production.
- Exact rc.6 then closed the current X-Insight release/Target refresh. PR #54
  merged exact main `0ca502f99181173e3542592ae5a018a5bfc43e0c`; main CI
  `29648815418` and owner workflow `29648976737` passed. The named Target
  completed the in-place rc.5-to-rc.6 upgrade, backup/activation proof and
  non-mutating rollback check. The exact v2 synthetic fixture then passed
  first-write, identical replay, API and real browser/manual proof. This adds
  Target evidence for the synthetic v2 sidecar path; it does not close
  External integration, `N=1` usability or Production.

Detailed owner evidence:
[P2-1 local backend and 1x diagnostic](../evidence/prd2/p2-1-local-backend-and-1x-diagnostic-2026-07-13.md)
and
[P2-2/P2-3 investigation UI Local evidence](../evidence/prd2/p2-2-p2-3-investigation-ui-local-evidence-2026-07-13.md),
[P2-4 release/upgrade/manual Local and CI evidence](../evidence/prd2/p2-4-release-upgrade-manual-local-evidence-2026-07-14.md),
and
[P2-4 owner-supervised trial Local evidence](../evidence/prd2/p2-4-owner-supervised-trial-release-local-evidence-2026-07-14.md).
Exact main delivery evidence is recorded in
[P2-4 owner-supervised trial main delivery evidence](../evidence/prd2/p2-4-owner-supervised-trial-main-delivery-evidence-2026-07-14.md).
The latest supported-profile recovery and exact delivery are recorded in
[P2-4 supported-host SIGKILL recovery evidence](../evidence/prd2/p2-4-supported-host-sigkill-recovery-evidence-2026-07-14.md).
The exact Target release lifecycle and UI result is recorded in
[P2-4 rc.5 named Target lifecycle and UI evidence](../evidence/prd2/p2-4-rc5-named-target-lifecycle-and-ui-evidence-2026-07-14.md).
The current rc.6 refresh and exact v2 Target result is recorded in
[P2-4 rc.6 named Target, v2 sidecar and UI evidence](../evidence/prd2/p2-4-rc6-named-target-v2-sidecar-and-ui-evidence-2026-07-19.md).

## Problem And User

### Primary User

The primary user is a UniRouter/X-Insight platform operator or on-call engineer
investigating a known or suspected execution. Security and compliance staff may
observe the trial, but PRD2-M1 is not yet a full compliance workspace.

### Current Problem

The current X-Insight server can read one execution by raw request ID and expose
event/gap pages for development. It has no execution list/search contract,
source-qualified opaque identity, stable cursor/error contract or frontend.
Operators therefore cannot complete the accepted investigation job through a
product UI, and current page lengths can be mistaken for complete event/gap
counts.

### Product Outcome

The initial field version is successful when an operator can complete the
investigation loop from approved UniRouter evidence and can distinguish:

- direct facts from derived or source-declared facts;
- chronological ordering from declared parent/causal relationships;
- suppressed, capture-gap, unknown and `not_applicable` states; and
- real approved integration evidence from synthetic semantic-state fixtures.

## Scope

### UI Surfaces Required For PRD2-M1

| Surface | Required behavior | Explicitly excluded |
| --- | --- | --- |
| Execution Search | Request/source identifier, time, source, outcome and `has_gaps` filters; stable paging; loading, empty, invalid, expired/mismatched cursor, rate-limit, overload and unavailable states | Saved searches, dashboards, free-form JSONB/full-text search |
| Execution Detail | Opaque execution identity, visible source request ID, source, time, frozen outcome, accurate full counts, payload mode, import-provenance proof class, source-generated/imported timestamps, fixed `as_of`, revision/watermarks and explicit `syncing`/`current`/`stale`/`gap` state | Raw payload viewer, editable facts or an implied complete/live claim when evidence is partial |
| Evidence Timeline | One evidence event per node; deterministic chronology; declared parent edge shown separately; pagination and has-more state | UI-created fact ledger, proximity-inferred causality |
| Selected-Node Detail | Safe correlation, identity, capability, decision, usage, outcome and payload-policy field groups; explicit suppressed/unknown/`not_applicable` values | Unbounded JSON dump, raw prompt/response, secrets |
| Source And Provenance Inspection | Source ref/hash/schema/source/fidelity, field-group provenance and linked evidence/gaps | Promising leaf-field traceability without a new versioned schema |
| Capture-Gap Panel | Independent paging; confirmed/candidate gap, claim, reason, severity, recommendation and evidence link; explicit no-gap/unloaded distinction | Treating missing data as proof that an action did not occur |

Every product route must support keyboard use, visible focus, accessible names,
non-color-only state labels and narrow/mobile-width degradation sufficient for
an operator using an approved tunnel.

### API Contract Required For The UI

The engineering freeze should produce versioned DTO/schema tests for:

```text
GET /api/v1/executions
GET /api/v1/executions/{execution_id}
GET /api/v1/executions/{execution_id}/timeline
GET /api/v1/executions/{execution_id}/events/{event_id}
GET /api/v1/executions/{execution_id}/gaps
```

A bounded source-options/bootstrap endpoint may be added only if the search UI
cannot obtain its fixed options safely from the list response or static
configuration. It must not become a general metadata enumeration API.

Contract invariants:

- the public execution identity is opaque and source-qualified;
- the source request ID remains visible and searchable but is not a globally
  unique URL key;
- the cursor is opaque, versioned, bound to canonical filters/sort and a fixed
  `as_of`, with a unique tie-breaker;
- values above hard limits return a stable validation error rather than being
  silently clamped;
- public errors use stable codes, correlation IDs and bounded retry guidance,
  never raw internal error strings;
- list/detail/timeline/gap DTOs are narrow, bounded and metadata-only;
- list/detail responses expose safe source-generated/imported timestamps,
  fixed `as_of`, integration mode, revision, source/import watermarks and sync
  state; they never imply that absence means a current live-system fact or that
  searchable means timeline-complete;
- page count and `has_more` never masquerade as full event/gap totals; and
- an event ID is resolved only inside its execution boundary.

The exact default/maximum page sizes, prefix semantics, time-window maximum,
cursor retention and rate/concurrency limits are engineering-freeze items.
They must be measured against the recorded 1x workload before release and may
not be inferred from the current development API's silent 1,000-item cap.

### Schema And Read-Model Boundary

- Persisted normalized evidence events, provenance and capture gaps remain the
  fact source.
- Search summaries and timeline nodes are rebuildable projections/read models,
  not a second fact ledger.
- Source-qualified execution identity must prevent two sources with the same
  request ID from merging.
- Outcome aggregation and full-count semantics are frozen and fixture-tested
  before the UI labels them.
- Provenance acceptance is field-group traceability plus event source
  ref/hash traceability. Leaf-field traceability requires an explicit future
  schema change and must not be promised by this release.
- `candidate_gap_types` must have one compatible JSON-schema/DB/API meaning
  before the capture-gap UI depends on it.
- Add a minimal immutable import-provenance record with an accepted enum such
  as `operator_approved_integration` or `synthetic_fixture`, opaque source instance,
  exchange ID when present, artifact/approval-manifest hashes, source-generated
  time, imported time and an assurance mode of
  `operator_attested_sidecar`, `contracted_pull` or `synthetic_fixture`. This is
  the fact source for proof/freshness UI; it is not a durable import job and has
  no retry/lifecycle semantics. The fixed UI/API wording states that the source
  instance is declared/operator-attested for sidecar data, the current sidecar
  receipt is unsigned and hashes prove exact-byte integrity correlation—not
  sender or deployment authenticity. A future live pull may use only the
  authenticity semantics jointly accepted in its versioned contract.
- P2-0 freezes an allowlist for source, source-options and adapter/fidelity DTOs.
  `source_systems.metadata`, `deployment_ref`, `owner_ref` and every unclassified
  field are excluded. Event detail still proves event membership in execution.
- Import byte count, event count, individual event size and concurrency receive
  hard rejection limits. Durable import jobs remain out of scope.

### Frontend Engineering Default

PRD2-M1 selects TypeScript + React for the component application, built as
static assets and embedded in the Go binary/release for same-origin service.
The first UI code change must lock exact Node/package versions and record:

- package manager and lockfile;
- router/query/state choices kept to the smallest justified set;
- unit/component test runner;
- browser end-to-end test path;
- deterministic asset generation and Go embedding; and
- dependency/license/security checks.

The framework does not authorize a separate public frontend host, CDN or
cross-origin API. PRD2 product copy is Simplified Chinese (`zh-CN`); canonical
protocol names, JSON fields, CLI flags and stable error codes remain English.
No general i18n framework is added in this release.

## Initial Field Deployment Profile

### Supported Trial Mode

- one internal deployment tenant;
- one Ubuntu 22.04 LTS x86_64 host, 4 vCPU / 8 GiB baseline;
- PostgreSQL 16 and X-Insight bound only to loopback;
- systemd-managed X-Insight service;
- sequential or supervised operator access over an approved tunnel;
- fixtures/synthetic metadata only on the already verified target; approved
  sanitized UniRouter metadata only on a newly qualified field target;
- no raw prompt/response; the live pull slice is deferred from the first formal
  version, its mock is Local-only evidence, and sidecar is the binding field
  path.

The host/network/service baseline matches the prior non-production target
profile. PRD2 binaries/UI, approved real-metadata use, upgrade/restore, manual
and field usability require new Target qualification. The existing target has
no storage-encryption/snapshot/retention proof and remains fixture/synthetic-
only. A real-metadata field target is P0-blocked until encrypted data/backup
custody, restore, retention owner, trial exit and exact cleanup evidence are
accepted and executable. A different OS, architecture, container-only runtime
or air-gapped delivery process also requires qualification.

### What OS, PostgreSQL Shape And Air-Gapped Mean

| Term | Plain-language meaning | Recommended PRD2 default | What changes if different |
| --- | --- | --- | --- |
| OS | The operating system installed on the field server, comparable to Windows/macOS on a laptop. | Ubuntu 22.04 LTS on x86_64 because that is the already exercised host baseline. | Rocky/Anolis/another Linux or ARM is not assumed broken, but needs a separate install/service qualification. |
| PostgreSQL shape | Where and how X-Insight's database runs and who owns backup/restore. | One pinned PostgreSQL 16 Docker/Compose container on the same private host; X-Insight itself runs under systemd. The database is dedicated to X-Insight and is not UniRouter's database. | A customer-managed remote database adds network/TLS/account/backup ownership. Sharing UniRouter's database is rejected. |
| Fully air-gapped | The host cannot reach the public Internet and installation dependencies cannot be fetched directly; all binaries, images, trust keys and manuals must be carried in through an approved offline path. | **Not fully air-gapped.** No public listener and no runtime Internet dependency, but IT may upload the signed release and pinned PostgreSQL image through an approved private path. | A true air-gap needs an additional offline dependency/image/SBOM/license bundle and clean-host qualification. It does not change the product UI. |

This default keeps the current security posture without adding an offline
installer ecosystem to PRD2. “No public exposure” and “fully air-gapped” are
different: a loopback-only internal server can still receive an approved file
through a tunnel or internal artifact path.

### What Fixture-Only And Sanitized Metadata Mean

| Mode | Contents | What it proves | Limitation/risk |
| --- | --- | --- | --- |
| Fixture-only | Repository-created fake/synthetic executions with no customer facts. | Installation, API/UI behavior, pagination, labels, error states and safety tests. | It cannot prove that real UniRouter evidence is useful or mapped faithfully. |
| Approved sanitized metadata | A narrowly approved real UniRouter evidence subset: opaque request/source IDs, timestamps, model/routing/decision/outcome fields, token/cost counts, capture gaps and artifact/source hashes. | Real mapping fidelity, External integration and actual investigation value. | It is still customer operational metadata and therefore needs a dedicated trial database, custodian, encrypted storage/backup, retention and explicit exit. |

Sanitized metadata never includes raw prompts, responses, messages, API keys,
authorization headers, secrets, raw user content, direct personal identifiers
or unapproved raw IP/upstream error text.

PRD2 does not choose one mode forever. The recommended minimal sequence is:

1. fixture-only smoke proof first;
2. one fresh metadata-only synthetic artifact under the binding `UR-E` decision
   for the exact bounded External-integration proof; then
3. one separately approved, non-customer internal-real development metadata
   batch for pre-field mapping/UI value proof; then
4. only under customer-specific data/custody authorization and a separately
   applicable decision, one approved customer sanitized-real metadata set for
   later field validation. Neither real-data class is authorized by `UR-E`.

The internal-real receiver preparation and exact bounds are recorded in
[PRD2 Internal-Real UniRouter Metadata Validation Plan](prd2-internal-real-metadata-validation-plan-2026-07-19.md).
The existing named Target remains fixture/synthetic-only; the first internal-
real proof uses a dedicated FileVault-encrypted local PostgreSQL 16 trial
database and does not create a second product service.

The accepted simple retention default is: keep the dedicated sanitized trial
database through the field session plus seven calendar days, then the named
custodian explicitly chooses handoff or controlled retirement. There is no
automatic expiry or bulk file deletion. This decision does not create general
evidence-retention automation or authorize deletion of Phase 1 evidence.

### Approved Real-Metadata Qualification

If the User selects approved sanitized UniRouter metadata, PRD2 uses a new,
dedicated trial database rather than adding it to the existing Phase 1
fixture/synthetic target database. Before import, record the custodian, exact
approved artifact hashes/data classes, encrypted data/backup locations and
keys, access path, retention/exit decision and restore proof.

At trial exit, the custodian chooses one explicit outcome: hand off the exact
database/backup under a new custody receipt, or authorize a controlled
retirement. Retirement stops all writers, preserves required sanitized
receipts/hashes/cleanup evidence, verifies the exact dedicated database and
then executes the accepted database retirement procedure. Any backup/artifact
file cleanup names and verifies one exact path at a time; no wildcard,
recursive, directory or automatic expiry deletion is allowed. Phase 1 retained
normalized evidence and receipts are outside this trial-retirement scope.

### Integration Levels

| Level | PRD2-M1 status | Meaning |
| --- | --- | --- |
| A — controlled sidecar | Required fallback | An operator exports an approved bounded UniRouter snapshot, imports it, verifies a receipt and investigates through X-Insight. It remains the only currently implemented/jointly accepted path. |
| B — completed-execution pull | Deferred from first formal version | Existing disabled/mock code remains Local engineering evidence. Live activation and individual transport pieces are not current PRD2 gates. |
| B+ — in-flight/push/broad streaming | Not authorized | UniRouter push/exporter, SSE/WebSocket/token streaming, automatic receipt transport and broad backfill remain outside PRD2. |
| C — shared multi-user/public | Not authorized | Requires trusted identity, sessions, RBAC, tenant isolation, audit-on-read/export, TLS and abuse controls. |

## Implementation Batches And Gates

### P2-0 — Contract And Toolchain Freeze

Deliverables:

- execution/search/timeline/node/gap OpenAPI or equivalent schema contracts;
- opaque-ID, cursor, error, limit, outcome and count semantics;
- React/TypeScript build/test/embedding skeleton;
- recorded 1x workload and performance/failure budget;
- completed-execution pull candidate, default-disabled/mock consumer contract,
  activation watermark, revision/sync state and diagnostic freshness
  measurement, retained as Local-only evidence after live-feed deferral;
- field data classification and access/retention record; for any approved real
  metadata, encrypted data/backup custody, restore, retention owner and an
  explicit non-automatic trial-exit/cleanup receipt path; and
- release signing trust root/key-state, exact legacy upgrade source and
  supported PostgreSQL deployment/tool profile; and
- first release/manual version contract.

Exit gate: all UI states can be mapped to bounded API states; no unresolved
identity, causality, count, proof/assurance/freshness, raw-data, custody/
retention/trial-exit, release-trust or upgrade-compatibility ambiguity enters
implementation.

Delivery status: satisfied by merged P2-0 PR #29. Live pull activation remains
outside this gate and pending joint acceptance. The follow-up deterministic
mock closure now has Local activation/source, revision/replay/completeness,
no-raw, page-failure and real PostgreSQL search-visibility evidence plus
PostgreSQL 16 GitHub CI; it merged through PR #33.

### P2-1 — Bounded Backend Foundation

Deliverables:

- source-qualified execution identity and collision-safe migration;
- minimal immutable import-provenance/freshness record and safe DTO allowlists;
- bounded execution search/list and detail APIs;
- fixed-`as_of` keyset cursor and stable error DTO;
- timeline, event detail and independently paged gaps;
- narrow SQL projections and measured indexes;
- importer, DB pool, query and HTTP resource limits; and
- schema/golden/property/fault tests.

The X-Insight-owned consumer remains disabled against real endpoints in P2-1.
Its transport adapter is completed only after UniRouter accepts the joint
contract; no guessed URL/auth/cursor enters source.

Exit gate: same-ID cross-source fixtures do not merge, paging has no duplicate
or omission under its contract, errors leak no internal details, and overload
fails with a stable bounded response.

Delivery status: delivered by merged PR #30 after Local, PostgreSQL 16 CI and
release-artifact verification. PostgreSQL 16 field-host qualification remains
separate from CI.

### P2-2 — Search, Detail And Timeline UI

Deliverables:

- same-origin application shell and navigation;
- all five search filters and deterministic result paging;
- execution summary/detail header;
- event timeline with chronology/declared-parent separation;
- selected-node drawer/panel; and
- loading/empty/error/degraded/accessibility states.

Exit gate: browser tests complete search-to-node investigation and demonstrate
that equal/reversed timestamps and parallel children do not invent causality.

Delivery status: delivered by merged PR #31 after Local and GitHub CI
component, desktop/narrow browser and real Go API/PostgreSQL search-to-node
evidence. Deterministic semantic fixtures proved reversed/equal/parallel cases.

### P2-3 — Source, Provenance, Gap And Robustness Completion

Deliverables:

- field-group source/provenance display;
- direct, derived, declared, suppressed, gap, unknown and `not_applicable` labels;
- source ref/hash/schema/fidelity inspection;
- independent capture-gap panel paging and candidate/confirmed distinction;
- approved-integration versus synthetic-fixture badges backed by import
  provenance, plus generated/imported/`as_of`, revision, source/import
  watermark and `syncing`/`current`/`stale`/`gap` freshness states;
- no-raw/sensitive regression and hostile-boundary UI cases.

Exit gate: every semantic state has accessible component/browser coverage and
traceable evidence; the UI can state “unknown” without filling the gap.

Delivery status: delivered by merged PR #31. Local and GitHub CI verified all
seven semantic states, source/hash/schema/fidelity, proof/freshness, confirmed/
candidate gap and hostile no-raw cases. Historical approved Gate A success/
denial evidence was read through a migrated PRD2 copy while correctly remaining
`unclassified` without immutable provenance.

### P2-4 — Field Release, Upgrade And Chinese Manual

Implementation status (2026-07-19): **complete for the accepted X-Insight-owned
release and named-Target scope.** PR #35 merged the
implementation and Local evidence for signed release primitives, durable host
lifecycle, exact legacy allowlisting, role/config/service hardening and the
release-matched offline Chinese manual. Signed-v2 GitHub CI, fresh install and
the exact legacy lifecycle now pass on separate clean supported-profile VMs.
PRs #40 through #42 add 53 all-operation durable-phase recovery cases and a
real external process-group SIGKILL sample. Exact-main run `29301064665`
completed same-operation recover/resume plus the later lifecycle and fresh-
install paths; workflow `29301384798` produced the matching owner-supervised
delivery. Rc.1 through rc.4 exposed and safely contained three Target-only
compatibility defects; rc.5 from `27e05df...` passed exact-main CI,
owner-supervised publication and the named Target's upgrade, application
rollback, re-upgrade, restart and real product UI/manual proof. Rc.6 from
`0ca502f...` then passed its exact-main CI and supervised publication, the same
named Target's in-place rc.5-to-rc.6 upgrade, backup/activation and non-mutating
rollback check, plus exact v2 synthetic first-write/replay/API/UI/manual proof.
Literal host power loss remains untested. This status is execution-owner
evidence, not independent acceptance, External integration, field usability
or Production.

Deliverables:

- SemVer release candidate with versioned manifest and detached trust proof;
- managed install/adopt/preflight/upgrade/application-rollback workflow;
- backup verification and isolated PostgreSQL restore drill;
- loopback-fail-closed service/config templates;
- release-matched Simplified Chinese source manual and offline HTML; and
- compatibility matrix and release notes.

Exit gate: the exact qualified legacy artifact can be adopted/upgraded to the
first SemVer release and investigated; later releases prove N-1→N. The exact
previous application can be reactivated against a compatible expanded schema,
and the documented restore-to-new-database path succeeds in an isolated
environment. No automatic file, backup or evidence cleanup is included.

### P2-5 — Controlled Integration And Field Usability Proof

Deliverables:

- historical success/denial UI read proof may use retained normalized evidence,
  receipts and hashes after the temporary JSON artifacts expire; it is not a
  fresh External integration replay;
- the specifically authorized fresh metadata-only synthetic artifact under the
  binding `UR-E` gate imported through the accepted sidecar loop for the
  current External integration claim;
- named synthetic fixtures only for semantic states not present in approved
  real evidence;
- operator task script, observation sheet and sanitized result record;
- Local, CI, VM qualification, Target, External integration and Production
  states recorded separately; field usability is a separately named pilot
  metric, not a verified runtime state; and
- findings triaged as P0/P1/P2 without reopening Phase 1 absent a new verified
  Phase 1 P0/P1.

Exit gate: no sensitive/raw leak, no invented causality, and operators can
locate an execution, explain its outcome/evidence, distinguish fact from gap
and identify X-Insight's incremental value over the UniRouter Logs surface.

Current status (2026-07-19): the earlier rc.6 fixture rehearsal remains only
Target evidence. The separate bounded cross-project `UR-E` decision completed
one fresh manifest-bound synthetic artifact, named-Target first-write,
identical replay, SQL/API/real UI and retention reconciliation. This closes the
exact synthetic External-integration scenario only. The manual-guided `N=1`
User tasks remain pending; the earlier fixture rehearsal is not counted as that
field gate.

## Priority Matrix

| Priority | Required item | Why |
| --- | --- | --- |
| P0 before any field release | No raw/secret leakage; source collision isolation; stable bounded queries/errors; loopback-only activation; migration/backup/restore/crash-recovery safety; exact-main owner-supervised artifact/workflow/hash and Target verification; truth-preserving chronology/causality; proof-class fact source; and real-metadata storage/custody/retention/exit controls when used | A failure creates data exposure, installs an unverified artifact, produces false investigation conclusions or leaves an unrecoverable customer installation. |
| P1 for PRD2-M1 | All accepted investigation UI capabilities; accessible semantic labels; complete paging/count semantics; versioned release/upgrade; Chinese manual; controlled real-evidence task proof | Without these, the requested initial product cannot be meaningfully used or maintained at the field site. |
| P2/later | Saved search, dashboard, import diagnostics product surface, multi-user auth proxy, push/in-flight/broad transport, durable jobs, advanced eval/graph features | Useful expansion, but not necessary to prove the first investigation workflow under the accepted posture. |

P0 here is a PRD2 release classification. It does not mean a new Phase 1 P0
has been discovered.

## Acceptance Matrix And Executable Evidence

| ID | Acceptance | Evidence |
| --- | --- | --- |
| PRD2-A01 | Search contract is bounded and collision-safe. | API schema/golden tests, source-collision fixture, filter/cursor property tests and query plan evidence. |
| PRD2-A02 | Counts and paging are truthful. | More-than-one-page event/gap fixtures; exact full count/page count/has-more assertions. |
| PRD2-A03 | Timeline does not invent causality. | Equal/reversed clock, parallel child, explicit/no-parent fixtures plus browser assertions. |
| PRD2-A04 | Node/source/provenance facts are traceable. | Field-group provenance and event source ref/hash readback; unknown/suppressed/`not_applicable` cases; source/adapter DTO allowlist tests. |
| PRD2-A05 | Capture gaps are explicit. | Confirmed, candidate, absent and unloaded cases with independent paging. |
| PRD2-A06 | API and UI fail safely. | Limit/rate/timeout/pool/error tests, correlation IDs, no raw internal errors, accessible error states. |
| PRD2-A07 | Import is bounded and metadata-only. | Byte/event/event-size/concurrency rejection tests, sensitive-key/no-raw fixtures and duplicate/conflict storm. |
| PRD2-A08 | UI is complete and same-origin. | Component tests, browser search-to-gap flow, deterministic embedded assets and release verification. |
| PRD2-A09 | Upgrade is recoverable. | Exact legacy-to-first-SemVer then later N-1-to-N tests, shared writer lock, durable phase journal/power-loss recovery, migration re-run/drift/concurrency/fault tests, backup verification and isolated restore proof. |
| PRD2-A10 | Chinese manual matches the release. | Version/link checks, clean-host install/upgrade task execution and offline HTML inspection. |
| PRD2-A11 | Integration proof/freshness/assurance claim is bounded. | Import-provenance links approved sanitized artifact/receipt hashes; synthetic cases are separate; generated/imported/`as_of` and sync state are verified; sidecar wording states operator-attested source, unsigned receipt and integrity-correlation-only hashes. |
| PRD2-A12 | Field usability is demonstrated. | Actual participant count/roles and per-task completion/time/correctness/help outcomes, findings and no-go decision. |
| PRD2-A13 | Completed-execution visibility is fast without lying about timeline completeness. | Mock/local revision/replay/failure-isolation and diagnostic freshness evidence remain retained; the accepted live-feed deferral removes external measurement from the first-version gate. |
| PRD2-A14 | The first-customer release is supervised without a User-managed private key. | Manual workflow accepts exact current `main` plus successful matching push CI, emits the strict seven-file delivery, verifies signed-v2 before extract, proves the ephemeral private key is absent, and records Target/External/Production as false. |

The accepted human field group is `N=1`: the User/product decision owner owns
the investigation/value tasks. Codex facilitates and records but is not
counted and does not provide independent acceptance. Customer IT is asked for
host, access or policy facts when needed but is not required as a usability
participant. The ten-minute task target remains diagnostic, not a Production
usability/SLA claim; no population percentage is inferred from one person.

### Field Task Matrix

| Task | Recorded result |
| --- | --- |
| Copy an approved Request ID from committed-main UniRouter Logs/Data Exchange and locate the X-Insight execution. | Completion, time, correctness and help required. |
| State the outcome and cite the exact event/source evidence. | Correct/incorrect conclusion and cited IDs. |
| Separate chronology from a source-declared parent edge. | Correct distinction; any invented causality is a no-go. |
| Find one direct/declared fact and one gap/unknown, with source. | Correct label/source and help required. |
| Name one conclusion the current evidence cannot support. | Correct abstention versus guessed answer. |
| Explain what X-Insight answers beyond UniRouter Logs. | Named incremental value; “none” makes the product-value gate no-go. |

## UniRouter Dependency And Synchronization

PRD2-M1 does not require X-Insight to wait for all current UniRouter roadmap
work. X-Insight first completes its API, UI, schema, mock consumer, release,
upgrade and manual work independently. The minimum external evidence is the
already approved sidecar-compatible success and denial normalized evidence for
historical UI coverage. The two temporary snapshot JSON files were hash-
verified and deleted by exact path on 2026-07-18 and must not be assumed
available for PRD2 replay. Current unmerged UniRouter
worktree content is explicitly excluded. The binding External-integration gate
requires one new versioned artifact from qualified producer code and a clean
generation HEAD, plus the accepted decision and exact custody authorization.
Both sides now accept the bounded B0-D33 scope, the exclusive-create P1 is
closed, and the exact Stage A handoff, Stage A.5 preflight and Stage B proof
have passed. Any later artifact cannot
silently change the X-Insight schema or transport boundary. The sidecar path is
the binding first-version field option; the whole live-feed slice is deferred.
If a later authorized artifact does not contain both success and
denial, the report limits the fresh integration claim accordingly and uses
retained normalized/synthetic evidence only for separately labeled UI coverage.

Define two independent readiness doors before jointly choosing field date `T0`:

1. `XI-R`: code/schema/browser checks pass Local and CI; install/upgrade/
   rollback/restore/listener pass supported-profile VM qualification; a bounded
   supported-host crash/recovery sample passes; the exact owner-supervised
   release workflow and hash record pass; that exact release passes new Target
   qualification; field users/data mode/custodian/retention/exit are named; and
   no unresolved PRD2 P0 remains.
2. Binding `UR-E`: the response, both planning links, correction publication,
   atomic accepted/binding transition, one new manifest-bound Stage A artifact,
   exact-identity handoff, read-only Stage A.5 preflight and Stage B first-write/
   identical replay/API/UI reconciliation are complete for the contained
   scenario.
3. The exact synthetic External-integration sidecar import/readback has run.
   The remaining coordination action is to confirm the separate joint field
   window for the `N=1` task. Neither door blocks UniRouter's own unrelated
   field release.

As of exact rc.6, the X-Insight release/Target portion of `XI-R` is current and
complete, including the exact v2 synthetic sidecar/API/UI Target rehearsal.
The customer-site host/custody confirmation and manual-guided `N=1` tasks
remain field-entry/P2-5 work. `UR-E` is complete for its fresh manifest-bound
one-call/zero-trace/one-gap synthetic scenario; that result is separate from
the earlier fixture Target rehearsal and from the still-pending field task.

This gate-based synchronization avoids inventing a delivery date from an
independent UniRouter iteration.

### Evidence-Layer Matrix

| Layer | Required proof | Must not be claimed |
| --- | --- | --- |
| Local | Go/schema/component/browser unit and developer DB tests | Supported host or external integration |
| CI | Reproducible automated tests, build, artifact and VM-capable jobs available in CI | A real systemd host/field site merely because unit tests pass |
| VM qualification | Clean supported-profile install, legacy upgrade, listener, crash recovery and isolated restore | The named user Target |
| Target | Exact release/manifest/manual, host/storage/network/access and representative read on the named field host | External UniRouter integration or Production |
| External integration | Approved artifact/hash/receipt/import/readback or accepted feed/revision/readback | Field usability or Production transport |
| Field pilot metric | Per-task operator results and incremental value | A protocol-level verified state or Production SLA |
| Production | Not in PRD2-M1 | Any inference from the layers above |

### Recommended Relative Calendar

For one execution owner and a frozen scope, use this planning baseline; it is a
forecast to validate after P2-0, not a committed delivery date:

| Relative window | Primary gate |
| --- | --- |
| `T0-8w` to `T0-7w` | P2-0 contract/toolchain/workload/data-custody freeze |
| `T0-7w` to `T0-5w` | P2-1 bounded backend, migrations and fault evidence |
| `T0-5w` to `T0-3w` | P2-2 complete search/detail/timeline/node UI |
| `T0-3w` to `T0-2w` | P2-3 source/provenance/gap and robustness closure |
| `T0-2w` to `T0-1w` | P2-4 release/upgrade/manual qualification |
| `T0-1w` to `T0` | P2-5 target rehearsal, external integration and field script |

If the UniRouter field window is less than eight weeks away, do not silently
drop source-collision safety, no-raw controls, chronology truth or any requested
UI capability. The explicit choices are to move the joint trial or reduce it to
an owner-operated engineering demonstration on a disposable fixture-only
environment that leaves no customer installation. Release/workflow/hash
verification, listener, migration, backup/restore, crash recovery and real-data
custody P0 gates are never waivable. The recommendation is to move the joint
trial rather than leave a real user with an unsafe or unmaintainable install.

## Fresh-Controlled Product-Value Proof Owner Preflight — 2026-07-21

The User accepted a replacement mainline in the shared
`PRD2 Fresh-Controlled Product-Value Proof Authorization` at
`PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-21-prd2-fresh-controlled-product-value-proof-authorization.md`,
SHA-256
`6d8e32f33c59bb10799c629b0d3c929b6ff4980f520c074252d48f70c3628eb3`.
The identity-ambiguous historical ECS path is now
`historical_backfill_deferred`; this does not prove that source absent, empty or
data-insufficient. The fresh exchange remains `Not binding` until both owner
responses, planning merges and coordinator activation pass.

X-Insight accepts the bounded `FV-X-FIXTURE-CHECK` scope. Its exact owner
response is
`PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/responses/2026-07-21-x-insight-prd2-fresh-controlled-value-proof-acceptance-fixture-response.md`.
The synthetic `fresh_value_recovered_fallback.json` check establishes:

- a failed provider attempt remains a normalized `failure` timeline event;
- the unique later call-log event is normalized `success`;
- the current execution projection nevertheless reports `failure` because the
  accepted P2-0 contract and SQL apply `failure > partial > success > unknown`
  across all events;
- route/retry/failover event types and structured decision fields are available
  through normalized event detail when the producer emits them as first-class
  source fields; and
- `is_stream`, route index, retry count/index and fallback metadata remain only
  in restricted `raw_source_excerpt` and are correctly absent from the public
  API/UI allowlist. Stream visibility is therefore an explicit product gap, not
  an authorized expansion.

No outcome fix is implemented by this owner preflight. Changing recovered
fallback from the currently contracted worst-event result to top-level final
request success would change the meaning of the public `outcome` projection.
The shared authorization explicitly requires `DECISION_REQUIRED` when the
correct outcome is a product choice and prohibits an unapproved API-contract
change. The smallest pending decision is:

1. retain worst-observed-event outcome and exclude recovered fallback from the
   current value gate; or
2. define UniRouter execution outcome as the unique call-log final outcome when
   one exists, retain current precedence only when no call log exists, and
   authorize the narrow SQL/contract regression update while preserving every
   failed-attempt event.

X-Insight recommends option 2 because the field task asks the User to state the
final execution outcome, while failed attempts remain independently inspectable
in the evidence timeline. Until the User decides, `P0=0`, `P1=1`, `P2=1` and
`FV-CONTROLLED-TRAFFIC` remains blocked.

The Local fixture used a dedicated PostgreSQL 18 database named
`xinsight_fv_fixture_20260721`; it contains synthetic fixture rows only, is
retained pending disposition, and is not the future isolated import database.
Custody remains with the X-Insight execution owner and its first review deadline
is `2026-07-28`; no deletion is authorized. Any future real one-artifact import
must still follow the prepared
[Internal-Real Local Trial Runbook](../ops/prd2-internal-real-local-trial-runbook-2026-07-19.md):
exact private custody and preflight first, a newly bound PostgreSQL 16 cluster
and dedicated roles, empty fingerprint, migration rerun, backup/restore proof,
then one first-write and one identical-byte replay. None of those future
database/artifact steps ran in this fixture check.

### Unique Call-Log Outcome Follow-Up — 2026-07-21

The User selected option 2 in the shared
`PRD2 Fresh-Controlled Existing-ECS And Final-Outcome Authorization` at
`PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-21-prd2-fresh-controlled-existing-ecs-and-final-outcome-authorization.md`,
SHA-256
`d0464491727493939ad788698171f16474c6298db851502e04db77758315043f`.
X-Insight declares `scope_acceptance=accept_exact` for
`FV-X-UNIQUE-CALL-LOG-OUTCOME`. Its required owner response is
`PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/responses/2026-07-21-x-insight-prd2-fresh-controlled-unique-call-log-outcome-implementation-response.md`.

The accepted projection is intentionally narrow:

- when every normalized event in an execution is UniRouter evidence and exactly
  one has `source_event_type=call_log`, that event's normalized outcome is the
  public execution outcome;
- with zero or multiple call logs, or for a non-UniRouter execution, the
  existing `failure > partial > success > unknown` precedence remains; and
- failed attempts, retry, fallback, gaps and every event-level outcome remain
  unchanged and timeline-visible.

The implementation changes only the query-time outcome rollup plus focused
DB/API/UI regressions and directly affected contract/status text. It adds no
schema, migration, persisted field, enum, API shape, artifact/manifest field,
receiver policy, stream/route metadata visibility or dedicated recovery UI.
The `expected_observation` text embedded in the immutable
`fresh_value_recovered_fallback.json` fixture records the pre-Decision-B
worst-event baseline; this follow-up supersedes that observation without
rewriting the historical fixture bytes or any persisted evidence.

Historical compatibility risk is explicit: previously persisted recovered
UniRouter executions with one successful call log may move from the `failure`
filter to the `success` filter after deployment even though their evidence rows
do not change. A cursor's `as_of` freezes eligible data, not projection code, so
an in-flight traversal should restart after this semantic upgrade. This named
query-time reclassification is accepted; broader historical behavior changes
are not.

The outcome-choice P1 is resolved only after the focused implementation is
merged and its response is externally hashed with zero unresolved P0/P1.
`is_stream` remains a non-blocking P2 visibility gap. Cross-project Binding,
controlled traffic, source access, artifact/import, N=1 acceptance, Target and
Production remain inactive pending the separate activation gates.

## Non-Goals

- no UniRouter code or dependency synchronization;
- no X-IAM or Tool Gateway runtime;
- no UniRouter push exporter, automatic receipt transport, in-flight stream,
  durable import jobs or first-version live pull transport;
- no deep-link contract from UniRouter;
- no LiteLLM, New API, one-api, adapter SDK or public collector;
- no raw prompt/response, governed raw payload or semantic-content custody;
- no dashboard, policy simulator, case management or saved search;
- no public/multi-tenant SaaS, HA/DR, multi-region or Production verification;
- no shadow provider calls, route/policy write-back or 100x capacity claim;
- no broad historical auto-backfill; and
- no automatic deletion of releases, backups, evidence or retained snapshots.

## Remaining PRD2 External Confirmations

The first-customer authority decision is accepted and no longer asks the User
to custody a key. Two bounded external inputs remain; they do not expand
PRD2:

1. the desired UniRouter/X-Insight joint field period; `T0` is confirmed only
   after `XI-R`, while the binding `UR-E` exact synthetic proof is already
   complete—not by an unmerged UniRouter milestone; and
2. customer IT confirmation that the accepted Ubuntu 22.04 + local pinned
   PostgreSQL 16 Compose + private artifact upload/non-air-gapped profile matches
   the actual host.

The binding UR-E decision completed one new manifest-bound v2 sidecar artifact
from qualified producer code and a clean generation checkout, one private
transfer, one Target first-write and one identical-byte replay plus API/real UI
reconciliation. Live-feed acceptance is not a first-version prerequisite.

Sequential/supervised single-principal tunnel access and the two-stage fixture
then sanitized-evidence sequence and field-session-plus-seven-day retention are
accepted PRD2 defaults. Multi-user access,
managed remote PostgreSQL and full air-gap packaging are not silently added.
Named-Target upgrade/rollback-readiness validation is complete for exact rc.6.
The manual-guided User tasks and exact customer-site host/custody confirmation
remain X-Insight pre-trial conditions for a persistent customer-site
installation. The completed UR-E proof creates no obligation beyond its exact
bounded Stage A/Stage A.5/Stage B sequence.

## Related Records

- [Phase 2 Product Priority](phase-2-product-priority.md)
- [Phase 2 Batch 0 Scope And Decision Register](phase-2-batch-0-scope-and-decision-register-2026-07-13.md)
- [PRD2 P2-0 API And UI Engineering Contract](../architecture/prd2-p2-0-api-ui-engineering-contract-2026-07-13.md)
- [PRD2 Single-Node Upgrade And Rollback Contract](../architecture/prd2-single-node-upgrade-and-rollback-contract-2026-07-13.md)
- [PRD2 Simplified Chinese Product Manual Delivery Plan](prd2-zh-cn-product-manual-delivery-plan-2026-07-13.md)
- [UR-E Cross-Project Governance Correction](../evidence/prd2/ur-e-cross-project-governance-correction-2026-07-19.md)
- [UR-E Joint Activation Evidence](../evidence/prd2/ur-e-joint-activation-2026-07-19.md)
- [UR-E Exact Synthetic External Integration Evidence](../evidence/prd2/prd2-ur-e-exact-synthetic-external-integration-evidence-2026-07-19.md)
- [PRD2 UR-E Bounded Synthetic Sidecar Execution Plan](prd2-ur-e-bounded-synthetic-sidecar-execution-plan-2026-07-19.md)
- [PRD2 Internal-Real UniRouter Metadata Validation Plan](prd2-internal-real-metadata-validation-plan-2026-07-19.md)
- [Runtime Contract Capability Status](../architecture/runtime-contract-capability-status.md)
- [M2 Timeline UI Information Architecture Candidate](../discussion/prd1-m2-timeline-ui-information-architecture-candidate.md)
- [M2 Wireframe And Component Candidate](../discussion/prd1-m2-wireframe-component-candidate.md)
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/requests/2026-07-19-x-insight-prd2-v2-sidecar-external-integration-artifact-request.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-19-prd2-ur-e-bounded-synthetic-sidecar-execution.md`
