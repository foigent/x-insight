# PRD1 Candidate Scope

Status: candidate scope, not final PRD
Created: 2026-07-08

## Purpose

This document turns the previous pre-PRD artifacts into a candidate PRD1 boundary.

Inputs:

- `Gateway Adapter Mapping Matrix v0.1`
- `PRD1 Dual Demo Definition`
- `PRD1 Minimal Data Model`
- `UniRouter / X-Insight Integration Boundary`
- current discussion consensus in `AI/Agent Governance Plane Discussion Notes`

This is not the final PRD. It is a scope candidate for discussion and implementation planning.

Related engineering breakdown:

- [PRD1 Draft v0.1 Candidate](./prd1-draft-v0.1-candidate.md)
- [PRD1 Engineering Milestones Candidate](./prd1-engineering-milestones-candidate.md)
- [PRD1 M0 Evidence Envelope Candidate](./prd1-m0-evidence-envelope-candidate.md)
- [PRD1 M0 Schema And Indexes Candidate](./prd1-m0-schema-indexes-candidate.md)
- [PRD1 M0/M1 First Implementation Package Candidate](./prd1-m0-m1-first-implementation-package-candidate.md)
- [PRD1 M2 Timeline UI Information Architecture Candidate](./prd1-m2-timeline-ui-information-architecture-candidate.md)
- [PRD1 M2 Wireframe And Component Candidate](./prd1-m2-wireframe-component-candidate.md)

## One-Sentence Candidate Scope

PRD1 should build a runnable X-Insight evidence plane that ingests UniRouter and LiteLLM execution evidence into a common event model, renders a human-readable evidence timeline with direct / derived / gap / suppressed labels, supports governed payload policy with raw content off by default, and runs lightweight historical policy simulation.

## Product Position For PRD1

Do not position PRD1 as:

- AI observability dashboard;
- model gateway;
- full agent governance platform;
- OTel backend;
- security guardrail product;
- enterprise ontology product.

Position PRD1 as:

> AI Execution Evidence Plane for enterprise gateways and agent-adjacent execution.

Near-term description:

> X-Insight turns AI gateway execution into auditable evidence chains, shows what is visible and missing, and lets operators test governance rules against historical evidence without replacing their gateway.

## Primary User

Daily operator:

- AI platform owner;
- enterprise IT operator responsible for internal AI gateway usage;
- platform engineer responsible for AI gateway reliability, cost, routing, usage accountability, and governance readiness.

Important stakeholders:

- CISO/security governance;
- compliance/audit;
- business application owner;
- finance/procurement for cost attribution.

PRD1 should be designed for the daily operator first. CISO and compliance value should appear through evidence packs, policy simulation, and capture-gap reporting, not through a heavy security console.

## Product Wedge

The wedge is not "better traces."

The wedge is:

- evidence chain;
- identity and capability attribution;
- direct / derived / gap labeling;
- governed payload policy;
- lightweight policy simulation;
- adapter fidelity transparency.

The strongest first aha:

> I can inspect an AI execution, understand what happened, see which facts are proven, see what is missing, and test whether a governance rule would have flagged it.

## PRD1 Must Prove

PRD1 succeeds only if it proves all of these:

1. X-Insight can ingest events from a controlled gateway and an external gateway into the same evidence model.
2. A human can read an execution timeline without looking at raw logs.
3. Each important claim is labeled as direct, derived, suppressed, or gap.
4. Gateway-only visibility gaps are visible, not hidden.
5. Raw prompt/response storage is not required.
6. Prompt/response capture can be governed when enabled.
7. Policy simulation can evaluate historical evidence without live enforcement.
8. UniRouter deep integration does not leak UniRouter-specific assumptions into the core model.
9. LiteLLM compatibility proves ecosystem openness without making LiteLLM mandatory.

## Scope Lock Candidate v0.1

This section is the current narrowing proposal. It should be used for implementation planning, but it is still not the final PRD.

### Cut Line

PRD1 is accepted when X-Insight can explain real AI gateway execution as evidence.

PRD1 is not accepted merely because it ingests logs, renders traces, or shows cost dashboards.

PRD1 should be rejected if it requires:

- raw prompt/response storage by default;
- customers to replace their gateway;
- X-Insight in the synchronous request path;
- a full policy language before historical simulation proves value;
- a business ontology before execution evidence is reliable.

### Must-Have Release Scope

#### 1. Core Evidence Plane

Must include:

- source registry;
- adapter manifest format;
- normalized evidence event envelope;
- collector API and validator;
- Postgres event store;
- source references and adapter version preservation;
- idempotent ingestion keys and adapter import cursors;
- capture-gap records;
- basic local/on-prem deployment path.

Reason:

Without this spine, UniRouter and LiteLLM become two custom demos rather than a product architecture.

#### 2. UniRouter Deep Reference Path

Must include:

- `adapter-unirouter` DB/API import against current UniRouter tables first;
- mapping for request trace events, call logs, audit context, routing, provider, billing, usage, logging, and errors;
- source references back to UniRouter rows or APIs;
- derived capture gaps for facts UniRouter cannot currently emit;
- timeline rendering from at least one real or controlled UniRouter request;
- no dependency from UniRouter request handling to X-Insight availability.

UniRouter structural fields are required before claiming F3 fidelity:

- `trace_id`;
- `span_id`;
- `parent_span_id`;
- `schema_version`;
- `source_component`;
- `capture_mode`;
- `decision_kind`;
- `decision_result`;
- `decision_reason`;
- `policy_ref` optional;
- `rule_ref` optional;
- `actor_type`;
- `actor_ref`;
- `payload_capture_mode`;
- `payload_ref`;
- `evidence_ref`;
- `source_event_hash` if cheap enough to add without delaying the release.

Boundary:

- DB/API import is enough for the first internal evidence loop.
- Structural fields are required for the high-fidelity reference story.
- Native outbox/exporter is not required for the first release unless real-time push becomes necessary.

#### 3. LiteLLM Ecosystem Path

Must include:

- real LiteLLM callback ingestion;
- spend-log import with a minimal cursor/backfill path;
- adapter manifest and fidelity declaration;
- mapping for request ID, model, provider/model group, user/team/org/end user where available, token usage, spend, latency, cache, and status;
- payload exclusion by default;
- capture-gap generation for missing decisions, policy state, business outcome, and tool side effects;
- fixture-backed conformance tests from real LiteLLM-shaped data;
- a demo path that runs without UniRouter.

Reason:

LiteLLM is not needed for internal construction speed. It is needed to prove that X-Insight is not a UniRouter plugin.

#### 4. Operator Workflow

Must include:

- request search;
- human-readable execution timeline;
- node detail panel;
- direct / derived / gap / suppressed labels;
- source reference display;
- capture-gap panel;
- payload policy state display;
- evidence pack view for one request or session.

Reason:

The product wedge is not the event store. The wedge is a human operator understanding what happened, what is proven, and what is missing.

#### 5. Lightweight Governance Loop

Must include:

- governed payload policy records with raw content off by default;
- capture modes at least at the model level: `none`, `metadata_only`, `hash_only`, `redacted`, `reference_only`, `full_content`;
- historical policy simulation with simple structured rules;
- simulation results showing matched executions, fields used, and missing evidence;
- at least two rules running across UniRouter and LiteLLM evidence.

Reason:

Evidence without governance action becomes observability. Governance without evidence becomes theater.

### Should-Have If It Does Not Delay Release Gates

These are valuable, but they should not block PRD1 if the must-have gates are met:

- UniRouter cursor-based export API;
- source health and adapter import status;
- event integrity hash for imported events;
- basic JSON export for evidence packs;
- more than two policy simulation rules;
- simple usage/cost rollups;
- minimal source credential reference handling beyond local deployment needs;
- OpenTelemetry/OpenInference field reservation in the schema and adapter manifest.

### Later / PRD1.5+

Explicitly later:

- UniRouter native async outbox/exporter;
- full OpenTelemetry/OpenInference ingestion;
- one-api and new-api production adapters;
- LangChain/LlamaIndex/CrewAI native adapters;
- MCP/tool-call governance;
- SDK instrumentation for tool side effects;
- ClickHouse analytics sink;
- full prompt/response object store;
- full enterprise RBAC;
- audit-on-read workflows;
- legal hold;
- retention automation;
- OPA/Rego or another full policy engine;
- async alerts;
- live enforcement.

### Won't In PRD1

PRD1 will not:

- store raw prompts/responses by default;
- position itself as a generic AI observability dashboard;
- position itself as a gateway replacement;
- require LiteLLM or UniRouter for all customers;
- build an ontology-first business model;
- claim complete agent governance;
- claim downstream business side effects without explicit evidence.

### Release Gates

Gate A: internal UniRouter evidence loop.

- A real or controlled UniRouter request is imported through current tables or APIs.
- X-Insight renders a readable timeline with source refs and capture gaps.
- No UniRouter request path depends on X-Insight.

Gate B: UniRouter high-fidelity reference readiness.

- X-Insight can consume the planned structural fields.
- UniRouter has a clear minimal migration plan for trace/span/decision/capture/evidence fields.
- F3 claims are not made until these fields exist or are emitted through an equivalent path.

Gate C: external gateway credibility.

- A LiteLLM request is ingested through callback.
- A LiteLLM spend record is imported through the importer.
- The same timeline UI works without UniRouter.
- LiteLLM is labeled F1/F2 honestly, not dressed up as F3.

Gate D: governance value.

- Payload policy state is visible and raw content is off by default.
- At least two historical simulation rules run over both sources.
- Simulation output explains matched evidence and missing evidence.

Gate E: runnable release.

- Local/on-prem deployment starts cleanly.
- Fixtures and conformance checks are included.
- The evidence pack view can be generated for at least one UniRouter and one LiteLLM execution.
- The release is useful without private knowledge of the development environment.

## Non-Goals

PRD1 should not include:

- broad synchronous enforcement;
- full policy authoring system;
- OPA/Rego requirement;
- full OTel backend;
- full OpenInference parity;
- full MCP/tool governance;
- automatic downstream side-effect discovery;
- LangChain/LlamaIndex/CrewAI native adapters;
- one-api/new-api parity demos;
- production SIEM integration;
- full enterprise RBAC system;
- full legal hold workflow;
- full prompt/response storage as default;
- ontology-first business object modeling;
- multi-tenant SaaS control plane.

These are future directions, not PRD1 blockers.

## Candidate Functional Scope

### 1. Source Registry

Deliver:

- register source systems;
- record source type, version, deployment reference, owner, and status;
- support UniRouter and LiteLLM sources first.

Why:

Evidence quality depends on source identity.

### 2. Adapter Manifest

Deliver:

- adapter manifest format;
- source version compatibility;
- fidelity level: F1, F2, F3;
- direct / derived / known gap declaration;
- privacy behavior declaration.

Why:

Prevents false "supported" claims.

### 3. Evidence Collector

Deliver:

- ingestion API for normalized evidence events;
- validator for required envelope fields;
- adapter version and source reference preservation;
- append-only event storage.

Why:

This is the product backbone.

### 4. UniRouter Native Adapter

Deliver:

- ingest UniRouter request trace events, starting with DB/API import from existing UniRouter tables;
- ingest or reference call logs and audit context;
- map gateway, routing, provider, billing, usage, logging events;
- support source references;
- generate capture gaps.

Required UniRouter-side additions for PRD1 candidate:

- `trace_id`;
- `span_id`;
- `parent_span_id`;
- `schema_version`;
- `source_component`;
- `capture_mode`;
- `payload_capture_mode`;
- `payload_ref`;
- `evidence_ref`;
- `decision_kind`;
- `decision_result`;
- `decision_reason`;
- `policy_ref` optional;
- `rule_ref` optional;
- `actor_type`;
- `actor_ref`;
- `source_event_hash` optional.

Reason:

UniRouter should become the F3 reference emitter.

Boundary:

- current UniRouter supports evidence discovery through metadata-only trace/call/audit data;
- native X-Insight push/export is not currently present;
- PRD1 should not put X-Insight in UniRouter's synchronous request path.

### 5. LiteLLM Adapter

Deliver:

- real callback ingestion endpoint;
- spend-log import path;
- map request ID, model, provider/model group, user/team/org/end user, token usage, spend, latency, cache, status;
- map session/agent/MCP-adjacent fields when present;
- payload exclusion by default;
- capture-gap generation;
- adapter manifest and conformance fixtures.

Sequencing recommendation:

1. Implement callback ingestion first.
2. Add spend-log import immediately after.

Reason:

Callback proves real-time compatibility. Spend import proves historical adoption.

### 6. Evidence Event Store

Deliver:

- Postgres-first schema;
- JSONB field groups under stable envelope;
- source refs;
- field-group provenance;
- capture mode;
- optional event hash;
- query path for request/session timeline.

Reason:

PRD1 needs correctness and flexibility more than large-scale analytics.

ClickHouse boundary:

- not required for PRD1;
- event envelope should be append-only and time-partitionable so ClickHouse can become an analytics sink later.

### 7. Human-Readable Timeline

Deliver:

- execution timeline view;
- node details;
- direct / derived / gap / suppressed labels;
- source reference display;
- identity, capability, decision, usage, payload policy, and outcome panels;
- same UI for UniRouter and LiteLLM events.

Reason:

The user's original pain point is that traces are data piles, not a human-readable end-to-end execution view.

Boundary:

- timeline first;
- full graph later.

### 8. Capture Gap Engine

Deliver:

- generate capture gaps from adapter manifest and missing fields;
- show gap type, affected claim, severity, and recommended instrumentation;
- include gaps such as business outcome unknown, tool side effect unknown, payload suppressed, not available from source.

Reason:

Capture gaps are a core differentiator.

### 9. Governed Payload Policy

Deliver:

- payload policy model;
- capture modes: `none`, `metadata_only`, `hash_only`, `redacted`, `reference_only`, `full_content`;
- raw content off by default;
- policy scope by org/project/source/adapter/capability/event type;
- retention, encryption key reference, audit-on-read, export/delete flags;
- UI state: suppressed, redacted, reference-only, available.

Candidate PRD1 boundary:

- implement payload policy records and UI labels;
- implement payload references and hash/redacted modes if needed for demo;
- do not require full prompt/response object storage unless a real demo/customer requires it.

Reason:

Enterprises need an option to enable prompt/response, but a simple boolean switch is unsafe and architecturally weak.

### 10. Lightweight Policy Simulation

Deliver:

- define simple structured historical rules;
- evaluate against existing evidence events;
- show matched requests and reasons;
- show fields used and missing evidence limiting confidence.

Initial rules:

- model allowlist violation;
- missing owner/project attribution;
- provider/channel outside allowed list;
- cost/spend threshold;
- payload capture disabled where policy expects capture.

Boundary:

- no live blocking;
- no full policy authoring system;
- no OPA/Rego dependency in PRD1.

Why:

Policy simulation turns evidence into governance value without needing enforcement infrastructure.

### 11. Evidence Pack

Deliver:

- request/session evidence summary;
- timeline nodes;
- source references;
- direct / derived / gap / suppressed summary;
- usage/cost summary;
- policy simulation summary;
- payload policy state.

Boundary:

- export can be simple in PRD1;
- advanced report builder can wait.

Why:

Evidence pack is the bridge from operator workflow to audit/compliance stakeholder value.

## Candidate Technical Baseline

### Backend

Recommended: Go.

Why:

- infrastructure/service style product;
- strong fit for adapters, collectors, validation, and on-prem deployment;
- aligns well with UniRouter integration;
- static binaries and operational simplicity;
- easier to keep resource usage predictable.

### Database

Recommended: Postgres first.

Why:

- source registry, adapter manifests, RBAC-like metadata, payload policy, retention metadata, audit reads, and policy simulation need relational correctness;
- JSONB handles heterogeneous adapter fields;
- on-prem deployment is simpler.

ClickHouse:

- reserve analytics sink boundary;
- do not require it for PRD1.

### Frontend

Recommended: TypeScript + React.

Why:

- timeline, inspection panels, filters, and evidence drilldown need rich interactive UI;
- existing ecosystem velocity is high;
- easier to build a polished operator console.

### Policy Engine

Recommended for PRD1: simple structured rule evaluator.

Why:

- lower implementation risk;
- easier for non-policy experts to understand;
- enough for historical simulation;
- can later map to OPA/Rego or another policy engine.

### Payload Storage

Recommended for PRD1:

- model payload policy;
- support reference/hash/redacted mode if needed;
- avoid full content storage unless specifically required.

Reason:

This keeps privacy risk controlled while preserving enterprise optionality.

## Candidate Data Tables

Required:

- `source_systems`
- `adapter_manifests`
- `evidence_events`
- `evidence_refs`
- `capture_gaps`
- `payload_policies`
- `policy_simulation_runs`
- `policy_simulation_results`

Conditional:

- `payload_objects`
- `payload_read_audits`

Optional derived/materialized:

- `timeline_nodes`
- `execution_summaries`
- `usage_daily_rollups`

## Candidate Demo Acceptance Criteria

### UniRouter Demo

Must show:

- real or controlled UniRouter request ingestion;
- at least gateway, identity, routing, provider, billing, usage, logging, and gap nodes;
- source refs for direct events;
- direct / derived / gap / suppressed labels;
- no raw prompt/response required;
- at least two policy simulation rules;
- evidence pack view.

### LiteLLM Demo

Must show:

- real LiteLLM callback ingestion;
- spend-log import with a minimal cursor/backfill path;
- model/provider/model group, user/team/org or equivalent context where available;
- token/spend/latency usage;
- capture gaps for missing decisions/business/tool side effects;
- same timeline UI as UniRouter;
- no UniRouter dependency.

### Shared

Must show:

- same evidence event model;
- adapter manifests;
- payload policy states;
- policy simulation output;
- source fidelity labels.

## Candidate Implementation Slices

### Slice 0: Schema And Contract

Deliver:

- evidence event schema;
- adapter manifest schema;
- Postgres migrations;
- validator;
- fixture format.

Exit criteria:

- valid UniRouter and LiteLLM fixture events pass validation;
- invalid events produce clear errors.

### Slice 1: UniRouter Evidence Loop

Deliver:

- UniRouter adapter using current DB/API import first;
- idempotent import path with adapter cursor/state;
- basic timeline from UniRouter request.

Exit criteria:

- one UniRouter request renders end-to-end timeline with source refs and capture gaps.

Stretch:

- UniRouter cursor export API if the imported evidence contract stabilizes quickly.

Explicitly later:

- async outbox/native exporter unless real-time push becomes necessary for the first public demo.

### Slice 2: Timeline UI

Deliver:

- request search;
- execution timeline;
- node detail panel;
- evidence strength labels;
- capture gap panel.

Exit criteria:

- operator can inspect a request without reading raw logs.

### Slice 3: LiteLLM Compatibility

Deliver:

- LiteLLM callback endpoint;
- adapter manifest;
- callback fixtures;
- spend-log importer with minimal cursor/backfill.

Exit criteria:

- one LiteLLM callback event appears in the same timeline UI with F1/F2 fidelity labels;
- one LiteLLM spend record imports through the importer;
- the demo path runs without UniRouter.

### Slice 4: Payload Policy

Deliver:

- payload policy records;
- capture mode display;
- default raw content off;
- source/adapter declaration of prompt/response behavior.

Exit criteria:

- UI clearly shows suppressed/reference/redacted/available states.

### Slice 5: Policy Simulation

Deliver:

- simple structured rule evaluator;
- simulation run/result storage;
- UI panel for matched events and missing evidence.

Exit criteria:

- at least two policies run over UniRouter and LiteLLM evidence.

### Slice 6: Evidence Pack

Deliver:

- evidence summary page;
- source refs;
- timeline summary;
- gap summary;
- policy simulation summary.

Exit criteria:

- operator can produce an audit-friendly view for one execution.

## Open-Source Boundary Candidate

Open source:

- evidence schema;
- adapter manifest format;
- collector API;
- adapter SDK;
- UniRouter reference adapter if strategically acceptable;
- LiteLLM adapter;
- basic Postgres store;
- basic timeline UI;
- basic policy simulation;
- sample fixtures and demo stack.

Commercial or later enterprise layer candidates:

- advanced RBAC;
- audit-on-read workflows;
- legal hold;
- retention automation;
- customer-managed keys;
- advanced policy simulation;
- premium connectors;
- SIEM exports;
- large-scale analytics sink;
- enterprise report packs.

Important:

Open-source value must be runnable. Publishing schemas alone is not enough.

## Major Risks

| Risk | Mitigation |
| --- | --- |
| Looks like UniRouter plugin | LiteLLM demo must run without UniRouter. |
| Looks like observability clone | Emphasize evidence strength, gaps, policy simulation, payload policy. |
| Scope explosion | Keep PRD1 to UniRouter + LiteLLM, not every gateway/runtime. |
| Payload privacy backlash | Raw content off by default; payload policy modeled. |
| Overbuilt policy system | Historical rule simulation only. |
| Weak UI differentiation | Timeline must be human-readable and evidence-driven, not log/table-first. |
| Data model too loose | Stable event envelope plus JSONB field groups, not arbitrary blobs. |
| Data model too rigid | Avoid one gateway schema and reserve future trace/span/tool/business context. |

## Kill Criteria

PRD1 direction should be reconsidered if:

- UniRouter data cannot produce a meaningful evidence chain even after structural fields are added;
- LiteLLM integration cannot produce enough evidence to prove gateway neutrality;
- UI cannot make direct / derived / gap distinctions understandable;
- policy simulation feels disconnected from evidence;
- payload policy becomes too heavy for first deployment;
- the product is perceived as logs plus dashboards rather than evidence and governance.

## What Should Come After PRD1

Likely PRD2 candidates:

- new-api / one-api import adapters;
- OTel/OpenInference ingestion;
- MCP/tool-call adapter;
- SDK instrumentation for side effects;
- async alerts;
- stronger policy engine;
- ClickHouse analytics sink;
- advanced payload storage controls;
- enterprise RBAC and audit workflows.

Do not pull these into PRD1 unless a specific customer or demo requires it.

## Current Candidate Decisions

| Question | Candidate decision |
| --- | --- |
| First deep integration | UniRouter |
| First public credibility integration | LiteLLM |
| First storage | Postgres |
| First analytics sink | Reserved, not required |
| First UI model | Timeline/evidence chain |
| First graph model | Later |
| First policy model | Historical simulation |
| First enforcement model | None, except future controlled hooks |
| Prompt/response default | Off |
| Prompt/response optionality | Governed payload policy |
| OTel/OpenInference | Reserved bridge, not PRD1 center |
| one-api/new-api | Compatibility path after PRD1 core or as fixture/import if cheap |
| UniRouter first integration mode | X-Insight-side DB/API import first |
| UniRouter native exporter/outbox | PRD1 stretch or PRD1.5 |
| LiteLLM minimum | Real callback ingestion plus spend-log import |
| PRD1 release model | Gate A/B/C/D/E, not a single hand-authored demo story |

## Candidate PRD1 Output

The candidate output is:

1. Runnable local/on-prem X-Insight service.
2. Postgres-backed evidence event store.
3. UniRouter deep reference adapter.
4. LiteLLM callback adapter and spend-log import path.
5. Evidence timeline UI.
6. Capture-gap view.
7. Payload policy model with raw content off by default.
8. Lightweight policy simulation.
9. Evidence pack view.
10. Adapter cursors/idempotent ingestion.
11. Demo fixtures, conformance checks, and setup guide.

This is enough to justify the first open-source release if it is stable and runnable.

It is not enough to claim full AI agent governance.
