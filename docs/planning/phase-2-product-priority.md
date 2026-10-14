# Phase 2 Product Priority

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

Status: Priority and historical Batch 0 decisions retained; P2-0 through P2-4,
mock-pull closure, exact rc.6 publication, named-Target synthetic v2 proof and
the bounded `UR-E` sidecar External-integration scenario are verified. The
User's 2026-07-25 accepted reopen supersedes the 2026-07-18 live-feed deferral
for one exact completed-execution pull V1. X-Insight Local implementation and
verification are complete; PR CI, merge, deployment, live activation, Target,
External integration and Production remain separate.
Decision date: 2026-07-11
Status reconciliation: 2026-07-25
Decision owner: User
Execution owner: Codex
Phase 1 baseline: `97d58e7ddbe9`

## Accepted Priority

The first X-Insight Phase 2 product priority is an X-Insight-owned operator
investigation UI with:

1. execution search;
2. execution detail and evidence timeline;
3. selected-node detail;
4. source and provenance inspection;
5. capture-gap inspection.

This decision selected the product-value direction; the later PRD2-M1 plan now
accepts the bounded UI/API delivery shape. Neither acceptance is a
production/public deployment claim or implementation-completion claim.

## Product Boundary With UniRouter

UniRouter owns outbound approval, artifact/delivery ledger, and sidecar receipt
reconciliation in its Data Exchange Control surface. X-Insight owns downstream
evidence normalization and investigation, including the execution timeline,
node/source detail, capture gaps, and later read-side audit.

The production-intended cross-project runtime boundary is now an asynchronous
X-Insight pull from UniRouter's transactional sanitized completed-execution
outbox. The accepted V1 is a jointly versioned, bounded private HTTP feed with
one X-Insight worker, durable cursor/high-watermark state and existing
source-neutral evidence persistence. It must not introduce a synchronous
UniRouter dependency, UniRouter push exporter, direct cross-database read,
automatic receipt transport, durable job/deep-link contract or gateway
blocking path.

The operator-mediated `xinsight.import-receipt.v1` sidecar remains valid
historical and explicit test/investigation tooling, but it is not live
transport. The one-off snapshot producer/import custody chain is retired as
the primary integration route and must not be recreated as a replacement
harness.

On 2026-07-19 the User accepted the X-Insight requesting-side defaults for one
fresh, manifest-bound, synthetic snapshot-v2 proof. That default requires a new
request/source/time/exchange/artifact identity, separates the fixed UniRouter
producer code identity from the later clean generation HEAD, imports the new
artifact once and replays the identical bytes once, and promotes only the exact
scenario inventory actually present. It is recorded as B0-D33 and in the
[UR-E bounded synthetic execution plan](prd2-ur-e-bounded-synthetic-sidecar-execution-plan-2026-07-19.md).
UniRouter returned `accept`, published its owner-authored response and planning
chain, then published the exclusive-create correction through PR #222 / routed
CI `29672169049` / main `9a94d549...`. X-Insight verified the final response
SHA and mechanism identities. The designated shared writer atomically recorded
both planning chains, `Decision accepted` and `Binding` at
`2026-07-19 12:15:26 CST`. At activation this reached Stage A only. UniRouter
later completed the exact artifact/handoff and X-Insight completed the
conditional Target first-write/replay/API/real UI proof described below.

## Accepted Initial Delivery Posture

The User accepted this posture on 2026-07-11:

- one internal deployment tenant rather than public multi-tenant SaaS;
- X-Insight service and PostgreSQL remain loopback-only;
- operator access uses an approved tunnel or internal reverse proxy;
- only repository fixtures and approved sanitized metadata are used;
- no raw prompt/response capture.

This posture gets the investigation workflow in front of operators without
making public API security and SaaS tenancy prerequisites for the first product
proof. Internal deployment is not a permanent exemption from security controls.
The already verified target remains fixture/synthetic-only. Before it stores
approved sanitized metadata, PRD2 requires new qualification for encrypted
data/backups, custody, restore, retention owner and trial exit/cleanup evidence.
Before non-loopback/shared-principal access or evidence beyond the specifically
approved sanitized set, X-Insight must additionally add an explicit identity
boundary, RBAC, retention/deletion behavior, audit-on-read/export, TLS and
tenant ownership semantics.

Public or shared multi-tenant exposure is a later architecture decision. It
requires enforced tenant isolation through an accepted row, schema, database,
or deployment boundary; authenticated APIs and UI sessions; guess-resistant
identifiers; custody/audit controls; abuse limits; and a stable
compatibility/error contract before exposure.

## First Slice Boundary

The first implementation slice should prove the existing evidence spine is
useful to a human operator:

- add bounded execution search/list support over persisted evidence;
- derive timeline nodes from evidence events at query time;
- preserve direct, derived, declared, suppressed, gap, and unknown labels;
- show source references and adapter fidelity without raw payload;
- show capture gaps as explicit evidence limitations;
- render the current approved UniRouter evidence end to end.

At the implementation-kickoff baseline, the current read APIs are request-ID
lookups only. P2-0 freezes bounded search, cursor pagination and the error
contract before feature UI depends on new endpoints. Merged P2-1 now implements
those endpoints, and merged P2-2/P2-3 consume them in the feature UI with Local
and CI evidence.

The first slice excludes LiteLLM, New API, one-api, policy simulation, durable
import jobs, public collectors, adapter SDK work, governed payload storage, and
production hardening unless separately accepted.

## 2026-07-13 Batch 0 Decision Update

The User accepted B0-D01 through B0-D08 on 2026-07-13. They select:

- a platform/on-call investigator persona;
- source-qualified opaque execution identity with source request ID visible;
- tunnel-first access;
- five bounded filters;
- one-event/one-node timeline truth with chronology separated from causality;
- multi-label provenance;
- real-versus-synthetic proof separation; and
- TypeScript + React static assets embedded in the Go release, with exact
  dependencies frozen before code is added.

These are accepted product decisions. P2-0 through P2-3 are now merged with
Local and CI evidence. This implementation status is separate from the still-
pending field release and human proof. The User
also accepted the recommended B0-D09 through B0-D22 identity, Tool Gateway,
cache, evidence and feedback boundaries on 2026-07-13. Those decisions remain
unimplemented; exact SLOs, action classes, spool behavior, semantic/privacy
contracts and Tool Gateway build/reuse/defer choice remain open parameters.
They gate only the runtime expansion that depends on them and do not block the
bounded D01-D08 search/timeline slice. Existing non-goals forbid that expansion
by default.

The User then accepted B0-D23 through B0-D31: `N=1` owner-operated field proof,
the Ubuntu 22.04/private-host deployment profile, fixture-then-sanitized data,
session-plus-seven-day trial retention, an X-Insight-pulled completed-execution
feed with sidecar fallback, diagnostic `p95 <= 10s` search visibility,
truthful timeline revision/watermark states, activation-point-only ingestion
and Simplified Chinese product copy. The X-Insight-owned portions are
authorized. At that time the live feed remained a proposed cross-project
contract and could not be activated. On 2026-07-18 the User superseded the
executable live-feed portion of D27/D28/D30 for the first formal version: the
whole slice is deferred, no automatic `p95 <= 10s` commitment applies, and
sidecar remains binding.

On 2026-07-14 the User accepted B0-D32 for the first customer: all installation
and upgrade work remains supervised by the User and Codex, so PRD2 does not
create or ask the User to custody a long-lived offline Ed25519 private key. A
dedicated manual GitHub workflow may generate a one-run key, sign the existing
v2 bundle, remove that private key before upload, and deliver only the artifact,
bootstrap verifier, public trust store, checksums and a record bound to exact
current `main` plus its successful push CI. This is an
`owner_supervised_trial`, not a persistent publisher identity or customer
self-upgrade channel. A second customer, delegated installer or unattended
upgrade reopens the durable signing-authority decision.

On 2026-07-19 the User accepted B0-D33 on the X-Insight/requesting side. The
counterpart response, correction and atomic activation later made the exact
bounded decision binding. It freezes a conditional two-stage proof rather than
a new transport: UniRouter
first generates and privately hands off one genuinely new metadata-only
synthetic artifact from a clean qualified checkout; after exact artifact and
Target identities pass the conditional gate, X-Insight performs one first-
write and one identical-byte replay. Normalized synthetic evidence, receipts
and hashes remain retained. The maximum later claim is execution-owner
External integration for the exact manifest-bound scenario only. The ordinary
macOS `install -m 0600` P1 was replaced by a pinned exclusive-create mechanism,
verified by X-Insight and published through UniRouter PR #222. UniRouter then
generated and handed off the exact fresh artifact. X-Insight passed the
conditional Stage A.5 gate and completed one named-Target first-write, one
identical-byte replay, bounded API and real UI proof. D33 is now complete for
the contained one-call/zero-trace/one-gap scenario only; live feed, field
usability, customer/sanitized-real data and Production remain unproved.

On 2026-07-19 the User authorized the next X-Insight-owned preparation: one
bounded internal-real, non-customer development metadata batch before customer
field data. B0-D34 limits it to one manifest-bound v2 artifact, at most 20
request IDs/200 source rows from one org, value-aware preflight and a dedicated
encrypted local PostgreSQL 16 trial database. It does not authorize UniRouter
source export by itself and does not write the existing fixture/synthetic-only
named Target. The exact plan is
[PRD2 Internal-Real UniRouter Metadata Validation Plan](prd2-internal-real-metadata-validation-plan-2026-07-19.md).

On 2026-07-20 the User authorized a narrow adaptation for one exact designated
`public.call_logs.ip_address` source column. The database may evaluate presence
but must not return the raw string to the producer; artifact and X-Insight
retain only `ip_address_was_present` and `ip_address_redacted`. UniRouter PR
#225 merged the Linux-first producer adaptation as main `d43fef6e...` with
required CI run `29711162577` attempt 2. This X-Insight revision adds the exact
dual-boolean declaration/state matrix and preserves the historical declaration
only for `false/false`. It adds no schema or field. The adaptation does not
prove the source IP was deleted or modified, does not prove the database
principal cannot select the base column, and the two booleans are not general
DLP proof. No source row/value, artifact, import, Target or Production action
is part of this compatibility step.

On 2026-07-25 the User accepted the complete minimum
`xinsight.completed-execution-feed.v1` reopen package. UniRouter owns one
sanitized transactional completed-execution outbox and service-key protected
bounded read endpoint. X-Insight owns one fixed-endpoint pull worker, strict
byte receiver for immutable `unirouter.completed-execution.v1` revision `1`,
durable source-instance/activation/cursor/high-watermark state, normalized
evidence persistence, replay convergence, status and investigation readback.

The V1 delivery contract is at least once, not exactly once. Every item commits
through the existing idempotent persistence API; the durable page cursor moves
in a later transaction only after all items converge. Partial-page persistence
is valid and a crash replays earlier items as duplicate/no-op. Timestamp values
are metadata only: activation-forward eligibility comes from the source's
opaque bootstrap cursor. Post-activation empty pages cannot advance the cursor
and nonempty pages cannot keep it unchanged.

The worker is installed dormant and uses a separate importer credential plus
feed-only service key. HTTPS is the default; cleartext private-tunnel HTTP is
an explicit false-by-default deployment setting and requires actual tunnel
evidence. The current implementation does not authorize deployment, customer
data, Production, multiple workers/HA, late revisions, pre-billing rejection
or broad backfill. See the
[runtime contract](../reference/completed-execution-pull-v1.md).

## PRD2-M1 Initial Field Validation

The User requested an initial X-Insight version that can be field-tested beside
UniRouter with all accepted investigation UI capabilities. The implementation
plan is now recorded in
[PRD2-M1 UniRouter Execution Investigation Field Proof](prd2-m1-unirouter-field-validation-implementation-plan-2026-07-13.md).

The accepted release plan is gate-driven rather than assigned an invented date.
It includes:

- bounded search/detail/timeline/node/source/provenance/gap APIs and all
  accepted investigation UI capabilities;
- TypeScript + React embedded same-origin in the Go server;
- field-group provenance plus event source ref/hash traceability;
- completed-execution pull against a deterministic Local mock only in the
  original PRD2-M1 package; the 2026-07-25 accepted reopen separately
  supersedes that historical live deferral for exact V1;
- a single-node versioned upgrade/rollback/restore contract; and
- a release-matched offline Simplified Chinese product manual.

PR #35 merged the P2-4 implementation. Local lifecycle,
manual-route/component/browser and deterministic manual generation checks pass.
GitHub run `29278281737` also passes exact signed-v2 double-build, fresh install
on one clean Ubuntu 22.04/PostgreSQL 16 VM and exact Phase 1 legacy adopt/
upgrade/application rollback/re-upgrade/isolated restore on another. Both
results are `ci_clean_vm_not_target`; main push run `29279421355` repeated all
five jobs successfully. PR #37 then merged the owner-supervised path and CI
routing as `3884f6d7c9a8de04bb558c41975f44f3c511c671`; exact-main run
`29298660052` and supervised workflow `29298897133` passed. PRs #40 through #42
subsequently merged all-operation durable-phase tests plus the external
supported-profile SIGKILL harness. Exact-main run `29301064665` passed
same-operation recover/resume and the later lifecycle/fresh-install paths;
post-recovery supervised workflow `29301384798` published the exact bounded
trial delivery. Literal power loss remains untested; named-Target rc.5 was later
verified separately. Exact rc.6 from `0ca502f...` subsequently passed
same-commit publication, in-place rc.5-to-rc.6 Target upgrade, non-mutating
rollback readiness and exact synthetic v2 sidecar/API/UI/manual proof. No
External, field-usability or Production claim follows from these runs.

The human field proof is `N=1`: the User/product owner. Codex facilitates and
records but is not a human participant or independent reviewer; customer IT
supplies host/access facts when requested. Accepted defaults are Ubuntu 22.04,
local pinned PostgreSQL 16 Compose, approved private artifact upload without a
full air-gap bundle, and fixture smoke. The binding `UR-E` decision adds only
one fresh metadata-only synthetic proof. Any later approved sanitized-real data
set remains a separate field input requiring its own data/custody authorization
and separately applicable decision, plus a dedicated trial database and
explicit retention through the field
session plus seven days before handoff or controlled retirement. Customer IT
host confirmation, the joint UniRouter/X-Insight field period and any later
approved sanitized-real field input remain external inputs. In-flight
streaming, broad
historical auto-backfill, multi-user/public exposure and Production
verification remain outside PRD2-M1.

Current implementation evidence is recorded in
[P2-1 local backend and 1x diagnostic](../evidence/prd2/p2-1-local-backend-and-1x-diagnostic-2026-07-13.md)
and
[P2-2/P2-3 investigation UI Local evidence](../evidence/prd2/p2-2-p2-3-investigation-ui-local-evidence-2026-07-13.md).
The merged P2-4 implementation is recorded in
[P2-4 release/upgrade/manual Local and CI evidence](../evidence/prd2/p2-4-release-upgrade-manual-local-evidence-2026-07-14.md).
The exact first-customer delivery is recorded in
[P2-4 owner-supervised trial main delivery evidence](../evidence/prd2/p2-4-owner-supervised-trial-main-delivery-evidence-2026-07-14.md).
The supported-profile process-death/recovery and latest exact delivery are
recorded in
[P2-4 supported-host SIGKILL recovery evidence](../evidence/prd2/p2-4-supported-host-sigkill-recovery-evidence-2026-07-14.md).
The deterministic completed-execution mock closure is recorded in
[P2-0 mock pull closure Local evidence](../evidence/prd2/p2-0-mock-pull-closure-local-evidence-2026-07-13.md).
The UI has locally replayed the retained approved Gate A success and denial
narratives through a migrated copy. That is historical product-read evidence,
not a new live UniRouter integration or human field result.

The following records explain the competitive rationale, audit the proposed
three-product architecture under Agent fanout/100x hypotheses, and define the
new authority and innovation candidates:

- [Competitive Alignment And Recommendation Rationale](../discussion/phase-2-competitive-alignment-and-recommendation-rationale-2026-07-13.md)
- [UniRouter / X-IAM / X-Insight Scalability Anti-Pattern Audit](../discussion/unirouter-x-iam-x-insight-scalability-antipattern-audit-2026-07-13.md)
- [Phase 2 Batch 0 Scope And Decision Register](phase-2-batch-0-scope-and-decision-register-2026-07-13.md)
- [X-IAM, Tool Gateway And Cache Authority Boundary](../architecture/phase-2-xiam-tool-gateway-cache-authority-boundary-candidate-2026-07-13.md)
- [X-Insight Innovation And Golden Boundary Matrix](../discussion/phase-2-xinsight-innovation-and-golden-boundary-2026-07-13.md)

The two new architecture/innovation records contain accepted architecture
boundary defaults but unimplemented innovation/runtime candidates. They do not
create X-IAM or Tool Gateway, replace the currently binding sidecar contract,
authorize raw/semantic evidence, shadow provider calls, route write-back,
100x/HA infrastructure or expand the first slice. The accepted D01-D08 rows do
not constitute implementation-completion evidence.

## Phase 1 Cross-Project Follow-Up

UniRouter accepted the current v1 runtime boundary on 2026-07-11. It requests no
additional Phase 1 code, schema, API, deployment, or data exchange work.

The two temporary Gate A snapshots were inventoried, hash-verified and deleted
by exact path on 2026-07-18. The durable shared cleanup record explicitly notes
the one-day delay; receipts, hashes, sanitized evidence and persisted normalized
evidence remain retained. This completed post-close custody operation does not
reopen Phase 1.

## Source Records

- [Phase 1 Closeout Gate](phase-1-closeout-gate.md)
- [Phase 1 Architecture And Function Audit](../architecture/phase-1-closeout-audit-2026-07-10.md)
- [M2 Timeline UI Information Architecture Candidate](../discussion/prd1-m2-timeline-ui-information-architecture-candidate.md)
- [M2 Wireframe And Component Candidate](../discussion/prd1-m2-wireframe-component-candidate.md)
- [Competitive Alignment And Recommendation Rationale](../discussion/phase-2-competitive-alignment-and-recommendation-rationale-2026-07-13.md)
- [Three-Product Scalability Anti-Pattern Audit](../discussion/unirouter-x-iam-x-insight-scalability-antipattern-audit-2026-07-13.md)
- [Phase 2 Batch 0 Scope And Decision Register](phase-2-batch-0-scope-and-decision-register-2026-07-13.md)
- [PRD2-M1 UniRouter Execution Investigation Field Proof](prd2-m1-unirouter-field-validation-implementation-plan-2026-07-13.md)
- [PRD2 P2-0 API And UI Engineering Contract](../architecture/prd2-p2-0-api-ui-engineering-contract-2026-07-13.md)
- [P2-2/P2-3 Investigation UI Local Evidence](../evidence/prd2/p2-2-p2-3-investigation-ui-local-evidence-2026-07-13.md)
- [P2-4 RC.6 Named Target, V2 Sidecar And UI Evidence](../evidence/prd2/p2-4-rc6-named-target-v2-sidecar-and-ui-evidence-2026-07-19.md)
- [UR-E Cross-Project Governance Correction](../evidence/prd2/ur-e-cross-project-governance-correction-2026-07-19.md)
- [UR-E Joint Activation Evidence](../evidence/prd2/ur-e-joint-activation-2026-07-19.md)
- [UR-E Exact Synthetic External Integration Evidence](../evidence/prd2/prd2-ur-e-exact-synthetic-external-integration-evidence-2026-07-19.md)
- [PRD2 UR-E Bounded Synthetic Sidecar Execution Plan](prd2-ur-e-bounded-synthetic-sidecar-execution-plan-2026-07-19.md)
- [PRD2 Internal-Real UniRouter Metadata Validation Plan](prd2-internal-real-metadata-validation-plan-2026-07-19.md)
- [PRD2 Single-Node Upgrade And Rollback Contract](../architecture/prd2-single-node-upgrade-and-rollback-contract-2026-07-13.md)
- [PRD2 Simplified Chinese Product Manual Delivery Plan](prd2-zh-cn-product-manual-delivery-plan-2026-07-13.md)
- [X-IAM, Tool Gateway And Cache Authority Boundary](../architecture/phase-2-xiam-tool-gateway-cache-authority-boundary-candidate-2026-07-13.md)
- [X-Insight Innovation And Golden Boundary Matrix](../discussion/phase-2-xinsight-innovation-and-golden-boundary-2026-07-13.md)
- [Runtime Contract Capability Status](../architecture/runtime-contract-capability-status.md)
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-11-prd5-runtime-contract-v1-boundary.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-11-gate-a-second-snapshot-retention.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/requests/2026-07-19-x-insight-prd2-v2-sidecar-external-integration-artifact-request.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-19-prd2-ur-e-bounded-synthetic-sidecar-execution.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-20-prd2-internal-real-raw-ip-policy-adaptation-b2-retry-authorization.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/requests/2026-07-20-x-insight-prd2-internal-real-raw-ip-policy-adaptation-b2-retry-request.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/responses/2026-07-20-unirouter-prd2-internal-real-raw-ip-policy-adaptation-implementation-response.md`
