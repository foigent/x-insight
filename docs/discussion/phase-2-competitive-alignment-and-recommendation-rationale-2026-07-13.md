# Phase 2 Competitive Alignment And Recommendation Rationale

Status: recommendation rationale; B0-D01 through B0-D08 defaults accepted by
the User on 2026-07-13; implementation remains pending
Date: 2026-07-13
Owner: Codex
Decision owner: User
Original analysis baseline: X-Insight
`e43add8e0184ba1eb059484053bc5e9d9f4cf6ed`
Decision-status update baseline:
`b287874e544fa96ff0af9751ea6dda57d05e9cf6`

## Purpose And Claim Discipline

This report preserves the first Phase 2 competitive-alignment result and
explains recommendations 2 through 8. It separates:

- current X-Insight implementation facts;
- already accepted Phase 2 product and deployment boundaries;
- competitor-validated interaction patterns;
- X-Insight-specific product and evidence-integrity judgments; and
- the 2026-07-13 User decision accepting B0-D01 through B0-D08 as product
  direction while leaving implementation evidence outstanding.

It is not an implementation authorization, implementation-completion or
production-readiness claim and is no reason to reopen Phase 1. An explicit
implementation start is still required after the remaining engineering
contract freeze. Competitor statements below were checked against official
product documentation on 2026-07-13. Absence means “not found in the official
material reviewed,” not proof that a competitor can never provide the
capability.

The accepted defaults are recorded authoritatively in the
[Batch 0 decision register](../planning/phase-2-batch-0-scope-and-decision-register-2026-07-13.md).
Acceptance selects product semantics; it does not mean the API, UI, schema or
tests exist. The later PRD2-M1 update selects React within the accepted embedded
TypeScript boundary; exact dependency versions still freeze in the first UI
code change.

## Answer First

The first product wedge should remain an operator investigation console rather
than a generic analytics dashboard. The strongest competitive parity targets
are bounded execution search, a hierarchical timeline, selected-span/event
detail, and source context. The strongest X-Insight differentiation targets are
field-group provenance, explicit capture gaps, and honest separation of real
integration evidence from synthetic semantic coverage.

Recommendations 2–8 are therefore not one copied competitor blueprint:

- 2, 5, 6, and 7 are primarily derived from X-Insight's evidence contract and
  current storage/runtime facts;
- 3 is primarily a consequence of the accepted loopback-only security posture;
- 4 borrows the proven “find a trace, then investigate” workflow while
  deliberately exposing a smaller, bounded filter surface; and
- 8 combines common TypeScript UI practice with an X-Insight-specific
  single-release and same-origin deployment choice.

## Competitor Baseline

Eight products are useful comparison points because they overlap the accepted
X-Insight wedge while representing different product strategies.

| Product | Officially documented strength used in this analysis | What X-Insight should learn | What X-Insight should not copy in Batch 1 |
| --- | --- | --- | --- |
| LangSmith | Trace filtering supports structured fields, metadata and a query syntax ([official filter documentation](https://docs.langchain.com/langsmith/filter-traces-in-application)). | Fast execution discovery and saved investigative context are mature expectations. | A broad query language before a stable bounded API/index contract. |
| Langfuse | Trace observation includes traces, observations, sessions, users and metadata; it also documents full-text search ([overview](https://langfuse.com/docs/observability/overview), [search](https://langfuse.com/docs/observability/features/full-text-search)). | Search and drill-down should feel like one workflow; self-hosting is credible product posture. | Raw-content search, ClickHouse or split ingestion/UI services before X-Insight has volume evidence and data authorization. |
| Braintrust | Trace detail exposes hierarchy, timing, origin and span-level data, with structured filtering ([trace examination](https://www.braintrust.dev/docs/observe/examine-traces), [filtering](https://www.braintrust.dev/docs/observe/filter)). | Hierarchy plus a selected-node panel is the correct investigation grammar. | Treating display hierarchy or timing proximity as causal truth when the source did not declare a parent edge. |
| Arize Phoenix | Open-source tracing, evaluation and experiment workflows are presented as an integrated observability surface ([official documentation](https://arize.com/docs/phoenix)). | Trace evidence should later connect to evaluation without corrupting the source ledger. | Pulling evals, datasets and experiments into the first product slice. |
| Datadog LLM Observability | Querying combines attributes, facets, measures and saved views in an operational monitoring product ([official querying documentation](https://docs.datadoghq.com/llm_observability/monitoring/querying/)). | Operators will eventually expect mature filtering and operational correlation. | Recreating a general-purpose monitoring/metrics platform before proving the evidence investigation wedge. |
| HoneyHive | The UI organizes tracing around sessions, traces and navigation flows ([official UI flows](https://docs.honeyhive.ai/v2/tracing/ui-flows)). | Session/run navigation and trace drill-down are useful mental models. | Assuming every source supplies a complete session or agent hierarchy. |
| Galileo | Sessions group related traces and provide a higher-level analysis unit ([official sessions overview](https://docs.galileo.ai/concepts/logging/sessions/sessions-overview)). | A future execution/run identity must be distinct from individual request identifiers. | Promoting `session_id` to authoritative execution identity before its source semantics are contracted. |
| Helicone | Sessions group request sequences in a gateway-oriented observability workflow ([official sessions documentation](https://docs.helicone.ai/features/sessions)). | Lightweight grouping can make request-heavy systems investigable. | Centering X-Insight on gateway request logs or raw payload capture. |

## Recommendation 2 — Composite, Opaque Execution Identity

**Recommendation.** Use a server-owned opaque execution identifier whose
minimum collision boundary includes `source_system` and the source request/run
identifier. Use “Execution” as the product term. Display the original
`request_id` as a source identifier, not as the globally unique URL key.

**Why.** The current read surface is keyed by a bare request ID. Request IDs are
not guaranteed unique across multiple source systems, and a future agent run can
contain many requests. A source-qualified identity prevents accidental merging
of unrelated evidence while leaving room to add a contracted run/session scope
later. Langfuse, HoneyHive, Galileo and Helicone validate the usefulness of
trace/session grouping; the collision rule itself is an X-Insight storage and
evidence-integrity requirement.

**Benefits.** Collision safety; guess-resistant URLs when random opaque IDs are
used; a stable UI term; future migration from request-level to run-level
grouping without redefining what the source request ID means.

**Costs and risks.** More complex URLs, cursors and migrations; operators need
the source ID displayed clearly; an incorrect early definition of “execution”
could still group events that have no real causal relationship.

**Do not overclaim.** Batch 1 should not infer a cross-request run merely from
timestamps or shared metadata. Until a versioned run-identity contract exists,
the safe execution scope remains source system plus source request.

**Revisit trigger.** A source provides a versioned, stable run/workflow ID and
parent semantics, or a second adapter demonstrates that request scope is
insufficient.

## Recommendation 3 — Tunnel First; Proxy Only With An Identity Contract

**Recommendation.** Keep the accepted tunnel-first, loopback-only posture for
the first proof. Permit an internal reverse proxy only after its trusted SSO or
identity-header contract, header stripping, TLS termination, audit fields and
failure behavior are explicit.

**Why.** X-Insight currently has no authenticated product session, RBAC,
tenant-isolation enforcement or audit-on-read. A reverse proxy is not an
identity system by itself. This recommendation follows the accepted deployment
boundary, not a competitor UI choice.

**Benefits.** Small attack surface; no premature custom authentication service;
same-origin access; faster use of fixture and approved sanitized metadata.

**Costs and risks.** Tunnel friction; weak per-user attribution; no shared
multi-user workflow; proxy rollout becomes a real security project rather than
a configuration toggle.

**Revisit trigger.** More than one trust domain or user needs access, the server
binds beyond loopback, or non-fixture enterprise metadata is approved.

## Recommendation 4 — Five Bounded MVP Filters

**Recommendation.** Start with request/source identifier, time range, source,
outcome and `has_gaps`. Do not introduce a general query DSL, raw-content search
or unbounded metadata filters in Batch 1.

**Why.** LangSmith, Langfuse, Braintrust and Datadog show that rich search is
valuable. X-Insight does not yet have the stable cursor/error contract,
projection columns, expression indexes or data authorization needed for their
full search surface. Five fixed filters cover the first operator questions and
can be mapped to bounded, explainable SQL.

**Benefits.** Predictable query plans; a small stable API; safer pagination;
clear empty/error states; measurable acceptance tests.

**Costs and risks.** Less exploratory discovery; no arbitrary metadata or
full-text search; advanced users may need to know the source ID or time window.

**Revisit trigger.** Query telemetry shows repeated unmet searches and
`EXPLAIN (ANALYZE, BUFFERS)` plus authorization rules justify each new indexed
filter.

## Recommendation 5 — One Evidence Event, One Node; Causality Is Explicit

**Recommendation.** Derive one timeline node from each persisted evidence
event. Show declared parent/child relationships separately from chronological
order. When timestamps tie, apply a deterministic tie-breaker but make no
causal claim.

**Why.** Braintrust and HoneyHive validate the usefulness of hierarchical trace
and selected-node investigation. X-Insight's evidence model requires a stricter
rule: a UI node cannot become a new fact source or combine several source facts
into an apparently atomic action. Parallel subagents, clock skew and identical
timestamps make “nearby means caused by” unsafe.

**Benefits.** Auditable one-to-one provenance; deterministic replay; no hidden
fact synthesis; selected-node detail can point to the exact normalized event.

**Costs and risks.** Long executions become visually dense; the UI will need
pagination, collapse and later virtualization; users may initially expect a
cleaner but less truthful inferred tree.

**Revisit trigger.** Load evidence proves that a rebuildable read projection is
needed. Any projection must remain derivable and deletable, never the ledger of
record.

## Recommendation 6 — Multi-Label Provenance; `not_applicable` Is Field Detail

**Recommendation.** A node can show multiple provenance labels across its field
groups: `direct`, `derived`, `declared`, `suppressed`, `gap`, and `unknown`.
Keep `not_applicable` in field-level detail rather than presenting it as a top
quality chip.

**Why.** A normalized event can contain direct source identity, derived outcome,
declared adapter metadata and suppressed payload fields at the same time. A
single “quality” badge would erase that distinction. `not_applicable` says a
field does not apply; it is not evidence quality. No equivalent unified
field-provenance plus capture-gap taxonomy was found in the official competitor
material reviewed, so this is primarily X-Insight product design rather than a
copy.

**Benefits.** Honest fidelity; faster source inspection; visible suppression
and uncertainty; a defensible product distinction.

**Costs and risks.** More visual complexity; label combinations require a clear
legend and accessible colors; users may incorrectly treat `derived` as lower
quality in every context.

**Revisit trigger.** Usability testing shows excessive cognitive load. The
fallback should summarize labels while preserving complete field-level detail,
not collapse them into one false score.

## Recommendation 7 — Real Integration Proof Plus Synthetic Semantic Coverage

**Recommendation.** Use current approved UniRouter evidence for the real
end-to-end product proof. Claim only the labels and stages actually present in
that evidence. Use named synthetic fixtures for `suppressed`, `unknown` and
other UI-state coverage not present in the real snapshot.

**Why.** Current approved UniRouter evidence demonstrates only a subset of the
full provenance enum. Calling a synthetic fixture “integration evidence” would
manufacture capability; omitting the states would leave UI behavior untested.
This is evidence and test discipline, not a competitor feature.

**Benefits.** Reproducible UI tests; truthful integration claims; full semantic
state coverage; no need for raw prompt/response or new source integrations.

**Costs and risks.** Two evidence classes must be labeled and maintained;
synthetic coverage does not prove source fidelity or production behavior; the
real demo may look less feature-rich.

**Revisit trigger.** A newly approved sanitized source snapshot proves another
state end to end. Local, CI, Target, external-integration and Production status
must still remain separate.

## Recommendation 8 — Same-Origin TypeScript UI Embedded In The Go Release

**Recommendation at analysis time.** Use a TypeScript frontend built to static
assets and embed those assets in the Go server/release. React was the candidate
component framework when this rationale was first written. The later PRD2-M1
decision selects React; exact package/tool versions remain pending the first
UI code change.

**Why.** A TypeScript component UI is conventional for trace consoles and has a
broad contributor ecosystem. Embedding the compiled assets fits the accepted
single internal deployment: one artifact, one origin, no browser-to-API CORS
contract and no second public service. This exact packaging choice is
project-specific, not copied from one competitor.

**Benefits.** One release and rollback unit; same-origin security boundary;
works through a tunnel; simple target operations; backend and UI contract can
be tested together.

**Costs and risks.** Frontend and backend release cadence are coupled; binary
size and build complexity grow; independent CDN/UI scaling is unavailable;
framework upgrades can create supply-chain and maintenance work.

**Revisit trigger.** The UI needs an independent delivery cadence, multiple API
deployments, public CDN distribution or horizontal scale separate from the Go
service.

## Feature Priority Matrix

| Band | Recommended capability | Reason now / later | Status after this report |
| --- | --- | --- | --- |
| MVP | Bounded execution list/search API, opaque composite execution identity, keyset cursor, stable errors and hard input/page limits | Required before any UI can depend on the API | Product direction accepted; not implemented |
| MVP | Execution search UI with the five accepted filters | Establishes the first operator workflow | Product direction and React selected; exact dependencies still to freeze |
| MVP | Query-derived paged timeline, one event per node, selected-node/source detail | Competitive parity without a second fact source | Product direction accepted; not implemented |
| MVP | Multi-label provenance and capture-gap panel | Highest differentiated product value | Product direction accepted; real versus synthetic coverage must remain explicit |
| MVP | Same-origin TypeScript + React frontend in the Go release | Matches loopback/tunnel deployment | Packaging/framework selected; exact packages pending |
| MVP | Import byte/event caps, DB pool/query/HTTP limits and explicit truncation/cursor behavior | Prevents the UI slice from amplifying existing unbounded paths | Recommended prerequisite; exact limits pending measurement |
| Advanced | Saved searches, additional indexed metadata filters and cross-execution comparison | Useful after real search telemetry | Deferred and evidence-gated |
| Advanced | Evaluation overlays, reusable datasets and replay/simulation | Competitors show value, but it expands beyond evidence investigation | Explicitly outside first slice |
| Advanced | Authenticated multi-user access, RBAC and audit-on-read | Required before trust boundary expands | Decision-gated security work |
| Advanced | Async continuous ingestion, durable import jobs and rebuildable read projections | Needed only after a jointly accepted transport/job contract and load evidence | Not authorized by sidecar v1 |
| Long-tail | Multi-tenant SaaS, public collectors, HA/DR, multi-region, ClickHouse/object-storage tiers | Production-scale platform work | Non-goal for current Phase 2 slice |
| Long-tail | Adapter SDK/marketplace, broad gateway catalog, policy simulation/enforcement | Ecosystem/control-plane expansion | Non-goal unless separately accepted |

## Current, Recommended, And Pending

| State | Fact |
| --- | --- |
| Implemented at analysis baseline | Go server, PostgreSQL evidence spine, importer CLI, fixture/conformance flow and read-only request-ID APIs; there was no frontend. P2-0 later added an embedded foundation shell and engineering primitives, not the feature-complete investigation UI. |
| Accepted boundary | Operator investigation priority; internal single-deployment tenant; loopback-only server/DB; tunnel or approved internal proxy; sanitized metadata only; no raw prompt/response. |
| Recommended next implementation | The bounded API, timeline derivation and same-origin investigation UI described in the MVP rows above. |
| Accepted product decisions | B0-D01-D08: first operator persona, opaque source-qualified identity, tunnel-first posture, five bounded filters, one-event/one-node truth, multi-label provenance, real/synthetic proof split and same-origin embedded TypeScript packaging. |
| Pending engineering freeze | Exact opaque-ID encoding, filter comparison rules, cursor/error schemas and numeric limits; React versus another component framework before dependency changes. |
| Pending scale decision | A measurable 1x workload, SLOs, hard limits, retention/capacity posture and failure-mode contract. |
| Explicitly not authorized | Automatic UniRouter exporter/receipt transport, durable import jobs, raw storage, public/multi-tenant deployment, HA/DR or a synchronous X-IAM dependency. |

## Accepted Decision Record

The User accepted all eight recommended defaults on 2026-07-13:

1. platform/on-call operator investigating a known or suspected execution;
2. source-qualified opaque execution identity with original request ID visible;
3. tunnel-first access and a later identity contract for reverse proxy use;
4. request/source ID, time, source, outcome and `has_gaps` filters only;
5. one event/one node and chronology separated from causality;
6. multi-label provenance with `not_applicable` in field detail;
7. real UniRouter evidence plus clearly named synthetic semantic coverage; and
8. TypeScript static assets embedded in the Go release, with React still the
   candidate component framework to freeze before implementation.

Executable acceptance evidence in the Batch 0 register remains mandatory.

## Related Project Records

- [Phase 2 Product Priority](../planning/phase-2-product-priority.md)
- [Phase 2 Batch 0 Scope And Decision Register](../planning/phase-2-batch-0-scope-and-decision-register-2026-07-13.md)
- [M2 Timeline UI Information Architecture Candidate](prd1-m2-timeline-ui-information-architecture-candidate.md)
- [M2 Wireframe And Component Candidate](prd1-m2-wireframe-component-candidate.md)
- [Runtime Contract Capability Status](../architecture/runtime-contract-capability-status.md)
