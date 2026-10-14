# PRD1 Draft v0.1 Candidate

Status: candidate PRD draft, not final PRD
Created: 2026-07-08

## Purpose

This document consolidates the current PRD1 discussion into one reviewable candidate draft.

It is not a final PRD. It is a structured convergence document used to test whether the current scope, architecture, user value, and implementation sequence are coherent enough to begin PRD1 execution.

Primary inputs:

- [PRD1 Candidate Scope](./prd1-candidate-scope.md)
- [PRD1 Engineering Milestones Candidate](./prd1-engineering-milestones-candidate.md)
- [PRD1 M0 Evidence Envelope Candidate](./prd1-m0-evidence-envelope-candidate.md)
- [PRD1 M0 Schema And Indexes Candidate](./prd1-m0-schema-indexes-candidate.md)
- [PRD1 M0/M1 First Implementation Package Candidate](./prd1-m0-m1-first-implementation-package-candidate.md)
- [PRD1 M2 Timeline UI Information Architecture Candidate](./prd1-m2-timeline-ui-information-architecture-candidate.md)
- [PRD1 M2 Wireframe And Component Candidate](./prd1-m2-wireframe-component-candidate.md)
- [UniRouter / X-Insight Integration Boundary](./unirouter-xinsight-integration-boundary.md)
- [Gateway Adapter Mapping Matrix v0.1](./gateway-adapter-mapping-matrix-v0.1.md)
- [External Feedback Round 1 Analysis](./external-feedback-round1-analysis.md)

## Product Thesis

PRD1 should make X-Insight a runnable AI execution evidence plane for enterprise gateway and agent-adjacent usage.

X-Insight should not compete as another model gateway. UniRouter, LiteLLM, one-api, new-api, and cloud AI gateway products already cover routing, OpenAI-compatible APIs, model abstraction, quotas, budgets, call logs, and cost dashboards.

The differentiated thesis is:

> Enterprises do not only need model traffic logs. They need auditable evidence chains for AI executions: what happened, who or what initiated it, which system made each decision, what evidence is direct, what is derived, what is hidden by policy, and what is missing.

For PRD1, this should be framed as:

> AI Execution Evidence Plane.

The longer-term category hypothesis can remain "AI Execution Governance Plane", but PRD1 should avoid selling a governance platform before the evidence layer is real.

## Problem

Enterprise AI and agent usage is moving faster than enterprise governance systems can observe.

Current gaps:

- gateway logs show request records, but not a human-readable execution chain;
- traces often expose data structures for engineers, not a clear operator view;
- identity, delegation, model routing, policy decision, usage, and outcome are fragmented across systems;
- prompt and response payloads are sensitive, so raw storage cannot be the default answer;
- gateway-only evidence cannot see downstream tools, business side effects, or application context;
- missing evidence is rarely shown as a first-class product object;
- policy enforcement is risky before evidence quality is trusted.

PRD1 should not claim to solve all of agent governance. It should solve the first hard problem:

> Turn available execution records into trustworthy, source-referenced, human-readable evidence, while making visibility gaps explicit.

## Primary Users

Daily operators:

- AI platform owner;
- enterprise IT operator responsible for internal AI gateway usage;
- platform engineer responsible for AI gateway reliability, cost, routing, usage accountability, and governance readiness.

Key stakeholders:

- CISO and security governance;
- compliance and audit;
- business application owner;
- finance and procurement.

PRD1 should serve the daily operator first. Security, compliance, and finance value should come through evidence packs, capture-gap reports, payload policy state, and historical simulation, not through a heavy security console.

## First Value Proposition

The first value proposition is not:

- better tracing;
- another dashboard;
- another gateway;
- generic model observability.

The first value proposition is:

> I can inspect an AI execution, understand what happened, see which facts are proven, see what is missing, and test whether a governance rule would have flagged it.

This value requires five visible product concepts:

- execution timeline;
- evidence provenance;
- direct / derived / suppressed / gap labeling;
- payload capture policy;
- historical policy simulation.

## One-Sentence PRD1 Scope

PRD1 should build a runnable X-Insight evidence plane that ingests UniRouter and LiteLLM execution evidence into a common event model, renders a human-readable evidence timeline with direct / derived / gap / suppressed labels, supports governed payload policy with raw content off by default, and runs lightweight historical policy simulation.

## Scope Lock Candidate

### Must Have

1. Core evidence plane

- source registry;
- adapter manifest format;
- normalized evidence event envelope;
- collector API and validator;
- Postgres event store;
- source references and adapter version preservation;
- idempotent ingestion keys and adapter import cursors;
- capture-gap records;
- basic local/on-prem deployment path.

2. UniRouter deep reference path

- X-Insight-side DB/API import against current UniRouter tables first;
- mapping for request trace events, call logs, audit context, routing, provider, billing, usage, logging, and errors;
- source references back to UniRouter rows or APIs;
- derived capture gaps for facts UniRouter cannot currently emit;
- timeline rendering from at least one real or controlled UniRouter request;
- no dependency from UniRouter request handling to X-Insight availability.

3. LiteLLM ecosystem path

- real LiteLLM callback ingestion;
- spend-log import with a minimal cursor/backfill path;
- adapter manifest and fidelity declaration;
- mapping for request ID, model, provider/model group, user/team/org/end user where available, token usage, spend, latency, cache, and status;
- payload exclusion by default;
- capture-gap generation for missing decisions, policy state, business outcome, and tool side effects;
- fixture-backed conformance tests from real LiteLLM-shaped data;
- a demo path that runs without UniRouter.

4. Operator workflow

- request/execution search;
- human-readable execution timeline;
- node detail panel;
- direct / derived / gap / suppressed labels;
- source reference display;
- capture-gap panel;
- payload policy state display;
- evidence pack view for one request or session.

5. Lightweight governance loop

- governed payload policy records with raw content off by default;
- capture modes at least at the model level: `none`, `metadata_only`, `hash_only`, `redacted`, `reference_only`, `full_content`;
- historical policy simulation with simple structured rules;
- simulation results showing matched executions, fields used, and missing evidence;
- at least two rules running across UniRouter and LiteLLM evidence.

### Should Have If It Does Not Delay Gates

- UniRouter cursor/export API for cleaner import;
- source health and adapter status view;
- JSON evidence export;
- extra policy simulation rules;
- usage and cost rollups derived from evidence;
- OTel/OpenInference field reservation;
- source-provided `source_event_hash` if cheap enough.

### Later Or PRD1.5

- UniRouter native async outbox/exporter;
- full OpenTelemetry/OpenInference backend;
- one-api/new-api production adapters;
- LangChain/LlamaIndex/CrewAI native adapters;
- MCP/tool governance;
- SDK-based downstream side-effect capture;
- ClickHouse or analytics store;
- full payload object storage;
- enterprise RBAC, SSO, audit-on-read, legal hold, and retention automation;
- OPA/Rego, Cedar, or full policy engine integration;
- alerting and live enforcement.

### Won't Do In PRD1

- raw prompt/response storage by default;
- gateway replacement;
- X-Insight in the synchronous request path;
- generic observability dashboard as the main product;
- ontology-first enterprise business object modeling;
- complete agent governance claims;
- downstream side-effect claims without source evidence.

## Release Gates

### Gate A: Internal UniRouter Evidence Loop

Acceptance:

- a real or controlled UniRouter request is imported through current tables or APIs;
- X-Insight renders a readable timeline with source refs and capture gaps;
- no UniRouter request path depends on X-Insight.

This gate proves that the evidence spine can ingest and normalize real gateway evidence.

### Gate B: UniRouter High-Fidelity Reference Readiness

Acceptance:

- X-Insight can consume planned UniRouter structural fields;
- UniRouter has a clear minimal migration plan for trace/span/decision/capture/evidence fields;
- F3 fidelity claims are not made until these fields exist or are emitted through an equivalent path.

This gate separates internal construction speed from public fidelity claims.

### Gate C: External Gateway Credibility

Acceptance:

- a LiteLLM request is ingested through callback;
- a LiteLLM spend record is imported through the importer;
- the same timeline UI works without UniRouter;
- LiteLLM is labeled F1/F2 honestly, not presented as F3.

This gate proves that X-Insight is not a UniRouter plugin.

### Gate D: Governance Value

Acceptance:

- payload policy state is visible and raw content is off by default;
- at least two historical simulation rules run over both sources;
- simulation output explains matched evidence and missing evidence.

This gate proves that evidence can support governance without inline enforcement.

### Gate E: Runnable Release

Acceptance:

- local/on-prem deployment starts cleanly;
- fixtures and conformance checks are included;
- evidence pack view can be generated for at least one UniRouter and one LiteLLM execution;
- release is useful without private knowledge of the development environment.

This gate is the minimum bar before public open-source release is meaningful.

## Functional Requirements

| ID | Requirement | PRD1 Acceptance |
| --- | --- | --- |
| FR1 | Source registry | Register UniRouter and LiteLLM sources with type, version, owner, deployment ref, status, and fidelity level. |
| FR2 | Adapter manifests | Store adapter version, supported event types, field mappings, fidelity limits, payload behavior, and known gaps. |
| FR3 | Evidence ingestion | Accept normalized evidence events, validate schema/enums, generate canonical event hash, and preserve source refs. |
| FR4 | Idempotency and cursoring | Avoid duplicate events by source/adapter/idempotency key and track adapter import states. |
| FR5 | Quarantine | Put invalid, risky, or idempotency-conflicting events into quarantine instead of silently accepting them. |
| FR6 | UniRouter importer | Import current UniRouter request trace events and call logs without requiring UniRouter code changes for Gate A. |
| FR7 | LiteLLM adapter | Ingest callback events and spend records through real adapter paths, not only static demo data. |
| FR8 | Capture gaps | Generate visible capture-gap records for missing identity, decision, policy, outcome, payload, and side-effect evidence. |
| FR9 | Execution search | Search by request ID first and open an execution detail view. |
| FR10 | Execution timeline | Render a human-readable sequence of derived timeline nodes from evidence events. |
| FR11 | Node detail | Show source refs, fidelity, field provenance, raw-source excerpt policy, and payload policy state per node. |
| FR12 | Payload policy | Default raw payload capture to off, while allowing on-prem governed capture modes. |
| FR13 | Policy simulation | Run simple structured historical rules over stored evidence and explain matched/missing fields. |
| FR14 | Evidence pack | Generate a compact evidence view/export for at least one execution. |
| FR15 | Import diagnostics | Show adapter health, cursor position, import errors, quarantine counts, and capture-gap summary. |

## Nonfunctional Requirements

PRD1 should be:

- local/on-prem runnable;
- Postgres-first;
- safe when raw prompt/response capture is disabled;
- independent of the synchronous model request path;
- adapter-version-aware;
- source-reference-preserving;
- explicit about missing evidence;
- testable through fixtures and conformance checks;
- compatible with future OTel/OpenInference mapping without making those protocols the core schema owner.

PRD1 should not require:

- ClickHouse;
- Kafka/NATS/Redis Streams;
- OPA/Rego;
- graph database;
- cloud managed control plane;
- UniRouter code changes for Gate A.

## Data And Architecture

### Core Architecture

The PRD1 architecture should have four layers:

1. Source systems: UniRouter, LiteLLM, and later one-api/new-api, OTel/OpenInference, SDK, MCP, business systems.
2. Adapters: source-specific importers and callback receivers that produce normalized evidence events.
3. Evidence store: Postgres tables for sources, adapter manifests, import states, evidence events, refs, capture gaps, payload policies, and quarantine.
4. Operator experience: execution search, timeline, node detail, gap view, payload state, diagnostics, simulation, and evidence pack.

### Event Contract

M0 uses:

- stable top-level event envelope;
- JSON field groups for correlation, identity, capability, decision, usage, outcome, payload policy, field provenance, and raw source excerpt;
- top-level query projections for `request_id`, `trace_id`, `span_id`, `org_ref`, `project_ref`, and `payload_capture_mode`;
- X-Insight-generated `event_hash`;
- optional source-provided `source_event_hash`;
- adapter-owned idempotency keys;
- source refs as first-class evidence references.

### Store

PRD1 starts with Postgres.

Reasons:

- schema review is easier;
- local/on-prem deployment is simpler;
- PRD1 query volume is not yet proven to require ClickHouse;
- JSONB gives enough flexibility for early adapter variance;
- future analytics sinks can be added when data volume and query shape are real.

## UI/UX Scope

The first UI should be an operator investigation console, not a dashboard.

Primary navigation:

- `Executions`;
- `Sources`;
- `Import Diagnostics`.

Primary product object:

- `Execution`.

Request ID remains the first lookup key because gateways currently expose request-level identity more reliably than full agent-run identity.

M2 build order:

1. execution search table;
2. execution detail header;
3. derived timeline;
4. node detail;
5. gap panel;
6. payload state;
7. import diagnostics.

Do not start with:

- graph canvas;
- landing page;
- generic metrics dashboard;
- SIEM console;
- full policy authoring UI.

## Integration Strategy

### UniRouter

Role:

- first deep internal integration;
- fastest path to real data, debugging, and high-fidelity evolution;
- reference path for F3 fidelity once structural fields exist.

PRD1 integration mode:

- X-Insight-side DB/API import first;
- no UniRouter request-path dependency;
- UniRouter native outbox/exporter is PRD1 stretch or PRD1.5.

Current UniRouter gaps to close for F3:

- `trace_id`;
- `span_id`;
- `parent_span_id`;
- `schema_version`;
- `source_component`;
- `capture_mode`;
- `decision_kind`;
- `decision_result`;
- `decision_reason`;
- optional `policy_ref` and `rule_ref`;
- `actor_type`;
- `actor_ref`;
- `payload_capture_mode`;
- `payload_ref`;
- `evidence_ref`;
- optional `source_event_hash`.

### LiteLLM

Role:

- first external ecosystem credibility integration.

PRD1 integration mode:

- real callback endpoint;
- spend-log import;
- F1/F2 fidelity label;
- fixture-backed conformance;
- no customer requirement to deploy LiteLLM.

### one-api And new-api

Role:

- important later gateway compatibility targets;
- not required for PRD1 release unless strategic pressure changes.

Reason:

PRD1 already has one controlled deep integration and one recognizable external integration. Adding more gateway adapters before the evidence spine is stable risks breadth without credibility.

### OTel/OpenInference

Role:

- compatibility bridge and future ecosystem path;
- not PRD1 core schema owner.

Reason:

The native evidence envelope must represent governance-specific concepts such as capture gaps, payload policy state, source fidelity, and policy simulation fields. These can map to OTel/OpenInference later, but should not be constrained by them in PRD1.

## Open Source And Commercial Boundary Candidate

Public open-source release should wait until the product is runnable and proves its positioning. Publishing empty scaffolding is not a meaningful ecosystem event.

Likely open-source core:

- evidence envelope and JSON schemas;
- adapter manifest spec;
- collector API;
- basic Postgres evidence store;
- basic execution timeline UI;
- conformance fixtures;
- reference UniRouter and LiteLLM adapters, if licensing and repo boundaries allow;
- adapter SDK or adapter authoring guide.

Likely commercial or enterprise layer:

- enterprise RBAC;
- SSO/SCIM;
- audit-on-read;
- retention automation;
- legal hold;
- customer-managed key integrations;
- advanced policy simulation;
- premium business-system connectors;
- SIEM/SOC integrations;
- HA deployment templates;
- managed service.

Unresolved:

- final license;
- whether commercial code lives in the same repo or separate repo;
- whether UniRouter-specific advanced integration remains open or commercial;
- public launch timing.

## First Implementation Package

The first code package should be M0/M1, not the full PRD1 release.

Included:

- Go backend skeleton;
- local config convention;
- Postgres connection and migrations;
- M0 JSON schemas and enum registry;
- validator;
- canonical event hash;
- sensitive-key detector;
- quarantine path;
- conformance fixtures and runner;
- seed source/adapter manifests;
- UniRouter current-table DB importer;
- import cursor state;
- idempotent event insertion;
- current `request_trace_events` mapping;
- current `call_logs` mapping;
- basic capture-gap generation;
- minimal read APIs.

Excluded from first package:

- React frontend;
- materialized timeline nodes;
- LiteLLM adapter;
- policy simulation;
- evidence pack;
- payload object storage;
- payload read audit;
- advanced RBAC;
- multi-tenant SaaS;
- UniRouter schema migration;
- UniRouter cursor export API;
- UniRouter async outbox/exporter;
- OTel/OpenInference ingestion;
- one-api/new-api adapters.

Implementation order:

1. P0 backend skeleton.
2. P1 M0 schema files and enum registry.
3. P2 M0 Postgres migrations.
4. P3 validator, canonical hash, sensitive-key detector.
5. P4 fixtures and conformance runner.
6. P5 UniRouter current-table importer.
7. P6 minimal read APIs.

## Risks And Controls

| Risk | Control |
| --- | --- |
| Product is perceived as UniRouter plugin | Gate C requires LiteLLM callback and spend import with the same UI. |
| Product becomes another observability dashboard | UI centers on evidence, provenance, gaps, payload policy, and simulation. |
| Privacy risk from raw payload capture | Raw prompt/response off by default; capture mode, retention, encryption, RBAC, audit-on-read, redaction, export/delete, and legal hold must be explicit before full capture. |
| Scope explosion | M0/M1 package excludes UI, LiteLLM, policy simulation, evidence pack, payload store, outbox, OTel, and extra adapters. |
| Fake demo | Gate A and Gate C require real or controlled source execution evidence, not only hand-authored examples. |
| Schema too rigid | Stable top-level envelope plus JSON field groups. |
| Schema too loose | JSON Schema validation, enum registry, idempotency conflict handling, source refs, and quarantine. |
| Gateway-only visibility overclaims | Capture gaps are first-class product output. |
| Early enforcement damages trust | PRD1 is observe-first with historical policy simulation only. |

## Kill Criteria

PRD1 should be reconsidered or narrowed if:

- real UniRouter import cannot produce a useful execution view without extensive manual curation;
- LiteLLM compatibility cannot produce a credible external gateway loop;
- users only perceive the product as logs/cost dashboard;
- capture gaps are too broad to make the evidence view useful;
- payload policy cannot be made safe enough for on-prem enterprise use;
- first implementation requires X-Insight in the synchronous request path.

## Remaining Decisions

PRD1-blocking or near-blocking:

- exact JSON Schema file contents during implementation;
- exact Go libraries and migration tooling;
- exact migration SQL;
- exact UniRouter DB/API import query shape after testing against the current environment;
- first UI wireframe adjustments after real imported UniRouter data exists.

Important but not PRD1-blocking:

- final open-source license;
- commercial packaging boundary;
- one-api/new-api implementation order;
- full OTel/OpenInference ingestion timing;
- ClickHouse threshold;
- OPA/Cedar/full policy engine timing;
- native UniRouter async outbox/exporter timing;
- final product name and category language.

## PRD1 Should Not Be Final Until

This draft should not become a final PRD until these are reviewed:

1. Gate A evidence loop is proven or the UniRouter import plan is adjusted.
2. LiteLLM adapter path is validated against real callback and spend-log shapes.
3. M2 UI can render real evidence without pretending missing evidence exists.
4. Payload policy behavior is reviewed from security and compliance angles.
5. Historical policy simulation rules are concrete enough to prove governance value.
6. Open-source and commercial boundaries are at least directionally agreed.

## Current Recommendation

Use this document as the PRD1 review base.

The next execution step should not be full product buildout. It should be the M0/M1 first implementation package:

- evidence schema;
- migrations;
- validator;
- fixtures;
- UniRouter current-table importer;
- minimal read APIs;
- Gate A backend acceptance.

M2 UI, LiteLLM, policy simulation, and evidence pack should follow only after M0/M1 proves that the evidence spine works on real UniRouter data.
