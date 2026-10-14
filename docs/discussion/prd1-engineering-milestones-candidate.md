# PRD1 Engineering Milestones Candidate

Status: engineering planning candidate, not final PRD
Created: 2026-07-08

## Purpose

This document turns `PRD1 Candidate Scope` and the Gate A/B/C/D/E release model into a candidate engineering path.

It is not a sprint plan yet. It is a way to test whether the candidate scope can become a realistic first runnable product without drifting into a generic observability platform, a UniRouter plugin, or a heavy governance suite.

Related M0 contract:

- [PRD1 M0 Evidence Envelope Candidate](./prd1-m0-evidence-envelope-candidate.md)
- [PRD1 M0 Schema And Indexes Candidate](./prd1-m0-schema-indexes-candidate.md)
- [PRD1 M0/M1 First Implementation Package Candidate](./prd1-m0-m1-first-implementation-package-candidate.md)

Related M2 information architecture:

- [PRD1 M2 Timeline UI Information Architecture Candidate](./prd1-m2-timeline-ui-information-architecture-candidate.md)
- [PRD1 M2 Wireframe And Component Candidate](./prd1-m2-wireframe-component-candidate.md)

## Planning Judgment

PRD1 should be built in this order:

1. Prove the common evidence contract.
2. Import real UniRouter evidence without changing UniRouter.
3. Build the human-readable timeline and capture-gap loop from that real evidence.
4. Add LiteLLM as the first external credibility adapter.
5. Add governed payload policy and historical policy simulation.
6. Package a runnable local/on-prem release with fixtures and conformance checks.

UniRouter structural fields should be prepared in parallel, but X-Insight should not wait for them to start. Gate B controls F3 claims, not Gate A construction.

## Critical Path

### M0: Evidence Contract And Storage Spine

Goal:

Create the shared product backbone before any adapter becomes the schema owner.

Must deliver:

- normalized evidence event schema;
- adapter manifest schema;
- source registry schema;
- Postgres migrations;
- ingestion validator;
- adapter cursor/idempotency model;
- fixture format;
- first UniRouter-shaped fixture;
- first LiteLLM-shaped fixture.

M0 contract:

- use the stable top-level envelope and field groups defined in `PRD1 M0 Evidence Envelope Candidate`;
- require source refs, idempotency keys, payload capture mode, and field-group provenance;
- generate X-Insight `event_hash` from canonical normalized event content;
- use the seed enums, schema boundaries, indexes, duplicate handling, and sensitive-key detector in `PRD1 M0 Schema And Indexes Candidate`;
- reserve trace/span/run/resource fields without requiring all sources to emit them.

Candidate tickets:

| Ticket | Owner | Description | Blocks |
| --- | --- | --- | --- |
| XIN-P1-001 | X-Insight | Define evidence event envelope and required field groups. | all ingestion |
| XIN-P1-002 | X-Insight | Define adapter manifest with fidelity, direct/derived/gap, and privacy behavior. | all adapters |
| XIN-P1-003 | X-Insight | Create Postgres migrations for source systems, adapter manifests, evidence events, evidence refs, capture gaps, payload policies, and simulation runs/results. | API/UI |
| XIN-P1-004 | X-Insight | Implement validator and fixture conformance runner. | adapter confidence |
| XIN-P1-005 | X-Insight | Define adapter cursor/state and idempotency keys. | DB/API imports |
| XIN-P1-006 | X-Insight | Define payload policy fields and capture states without raw payload storage. | payload UI, simulation |

Exit evidence:

- UniRouter and LiteLLM fixtures validate through the same runner.
- Invalid fixtures produce useful errors.
- The schema can represent direct, derived, gap, and suppressed facts.

Do not do:

- full graph model;
- OTel backend;
- raw payload object store;
- policy language.

### M1: UniRouter No-Change Evidence Loop

Goal:

Use current UniRouter data to produce the first real evidence timeline.

Must deliver:

- X-Insight-side `adapter-unirouter`;
- DB import against current `request_trace_events`, `call_logs`, and relevant audit context;
- optional fallback pull through existing per-log trace API for demos;
- source refs to UniRouter row/API origins;
- adapter cursor/state;
- idempotent re-import;
- mapping for gateway, routing, provider, billing, usage, logging, and errors;
- derived capture gaps for missing business outcome, tool side effect, payload state, and explicit decision semantics.

Candidate tickets:

| Ticket | Owner | Description | Blocks |
| --- | --- | --- | --- |
| XIN-P1-101 | X-Insight | Implement UniRouter source registration and connection config. | import |
| XIN-P1-102 | X-Insight | Implement current-table importer for `request_trace_events`. | timeline |
| XIN-P1-103 | X-Insight | Join/reference `call_logs` by request ID and source context. | usage/cost nodes |
| XIN-P1-104 | X-Insight | Import or reference relevant audit context where available. | identity/governance panels |
| XIN-P1-105 | X-Insight | Map UniRouter events into evidence event field groups. | UI |
| XIN-P1-106 | X-Insight | Generate capture gaps from current UniRouter limitations. | gap panel |
| XIN-P1-107 | X-Insight | Preserve source refs and import cursor state. | evidence pack |

Exit evidence:

- One real or controlled UniRouter request imports without hand-authored demo data.
- A request has source refs back to UniRouter evidence.
- Re-running the importer does not duplicate events.
- X-Insight can explain which facts are direct and which are derived.

Do not do:

- put X-Insight in UniRouter request path;
- require UniRouter code changes for Gate A;
- build native outbox first.

### M2: Timeline And Capture-Gap Operator View

Goal:

Turn imported evidence into a human-readable execution view.

Must deliver:

- request search;
- execution timeline;
- node detail panel;
- evidence strength labels;
- source reference display;
- capture-gap panel;
- payload policy state display placeholder;
- same visual model ready for LiteLLM.

M2 contract:

- start with execution search and execution detail, not a dashboard;
- derive timeline nodes from evidence events;
- show direct/derived/declared/suppressed/gap labels;
- show payload state explicitly, never as a blank panel;
- make source refs and adapter fidelity available in node detail;
- reserve policy simulation panel space for M5.
- use `Execution` as the product object unless later discussion changes it;
- build execution search table, execution detail header, derived timeline, node detail, gap panel, payload state, and import diagnostics in that order.

Candidate tickets:

| Ticket | Owner | Description | Blocks |
| --- | --- | --- | --- |
| XIN-P1-201 | X-Insight | Implement request/search API over evidence events. | UI |
| XIN-P1-202 | X-Insight | Build timeline projection from event groups. | UI |
| XIN-P1-203 | X-Insight | Build timeline UI with node details and labels. | demo |
| XIN-P1-204 | X-Insight | Build capture-gap panel with affected claim and recommendation. | governance loop |
| XIN-P1-205 | X-Insight | Show source refs and adapter/fidelity labels. | audit value |
| XIN-P1-206 | X-Insight | Add payload state labels: suppressed, metadata-only, reference-only, redacted, available. | payload policy |

Exit evidence:

- A human can inspect a UniRouter request without reading raw logs.
- The UI clearly separates direct facts, derived facts, gaps, and suppressed payloads.

Do not do:

- build full graph canvas;
- build generic dashboard pages before the request view is useful;
- hide missing evidence as empty fields.

### M3: LiteLLM External Credibility Path

Goal:

Prove X-Insight is gateway-neutral and not a UniRouter companion product.

Must deliver:

- `adapter-litellm`;
- real callback ingestion endpoint;
- spend-log import with minimal cursor/backfill;
- mapping for request ID, model, provider/model group, user/team/org/end user where available, usage, spend, latency, cache, and status;
- adapter manifest with F1/F2 fidelity declaration;
- payload exclusion by default;
- capture gaps for missing decisions, business outcome, and tool side effects;
- fixture-backed conformance tests from real LiteLLM-shaped data;
- demo path that runs without UniRouter.

Candidate tickets:

| Ticket | Owner | Description | Blocks |
| --- | --- | --- | --- |
| XIN-P1-301 | X-Insight | Implement LiteLLM source registration and adapter manifest. | ingestion |
| XIN-P1-302 | X-Insight | Implement callback ingestion endpoint. | external demo |
| XIN-P1-303 | X-Insight | Implement spend-log importer with cursor/backfill. | historical adoption |
| XIN-P1-304 | X-Insight | Map LiteLLM fields into evidence groups and fidelity labels. | timeline |
| XIN-P1-305 | X-Insight | Generate LiteLLM-specific capture gaps. | gap panel |
| XIN-P1-306 | X-Insight | Add LiteLLM demo/test stack or fixture runner. | release evidence |

Exit evidence:

- One LiteLLM callback event appears in the same timeline UI.
- One LiteLLM spend record imports through the importer.
- The demo path runs without UniRouter.
- LiteLLM is labeled F1/F2 honestly, not made to look F3.

Do not do:

- require customers to deploy LiteLLM unless LiteLLM is their chosen gateway;
- claim full decision visibility from LiteLLM unless emitted in metadata/callbacks;
- block UniRouter Gate A on LiteLLM work.

### M4: UniRouter F3 Readiness Track

Goal:

Make UniRouter capable of becoming the high-fidelity reference emitter without forcing production exporter complexity too early.

This is a parallel UniRouter track, not the X-Insight Gate A blocker.

Must deliver before F3 claim:

- minimal UniRouter migration or successor event shape for structural fields;
- explicit decision fields;
- actor fields;
- payload policy state fields;
- source schema/version fields;
- X-Insight adapter support for both current and enriched UniRouter events.

Candidate UniRouter tickets:

| Ticket | Owner | Description | Blocks |
| --- | --- | --- | --- |
| UR-XI-001 | UniRouter | Add trace/span/schema/source/capture/evidence fields or equivalent successor event table. | F3 claim |
| UR-XI-002 | UniRouter | Add explicit decision fields: kind, result, reason, optional policy/rule refs. | direct decision evidence |
| UR-XI-003 | UniRouter | Add actor fields: actor type/ref, owner ref, optional on-behalf-of. | identity clarity |
| UR-XI-004 | UniRouter | Add payload policy state fields without raw payload default. | payload UI |
| UR-XI-005 | X-Insight | Update adapter to consume both current and enriched UniRouter shapes. | smooth migration |

Should-have if cheap:

- cursor-based export API;
- event hash;
- source health endpoint.

Later:

- native async outbox/exporter;
- OTel export;
- prompt/response payload references beyond metadata state.

Exit evidence:

- X-Insight can consume enriched UniRouter events.
- F3 claim is backed by source-emitted structure, not only X-Insight inference.

### M5: Governed Payload Policy And Historical Simulation

Goal:

Turn evidence into governance value without live enforcement.

Must deliver:

- payload policy records;
- capture modes: `none`, `metadata_only`, `hash_only`, `redacted`, `reference_only`, `full_content`;
- policy scope by source/project/org/adapter/capability/event type where practical;
- raw prompt/response off by default;
- simple structured simulation rules;
- simulation runs/results storage;
- UI for matched executions, fields used, and missing evidence.

Candidate tickets:

| Ticket | Owner | Description | Blocks |
| --- | --- | --- | --- |
| XIN-P1-401 | X-Insight | Implement payload policy records and capture mode resolution. | payload UI |
| XIN-P1-402 | X-Insight | Display payload state on timeline and evidence pack. | operator workflow |
| XIN-P1-403 | X-Insight | Implement simple structured rule evaluator. | governance loop |
| XIN-P1-404 | X-Insight | Store simulation runs and results. | audit view |
| XIN-P1-405 | X-Insight | Add initial rules: model allowlist, missing owner/project, provider/channel restriction, spend threshold, payload expected but suppressed. | demo |
| XIN-P1-406 | X-Insight | Show missing evidence limiting simulation confidence. | differentiation |

Exit evidence:

- At least two simulation rules run over UniRouter and LiteLLM evidence.
- Results explain matched evidence and missing evidence.
- No raw prompt/response is required.

Do not do:

- OPA/Rego;
- live blocking;
- approval workflow;
- full policy authoring product.

### M6: Evidence Pack And Runnable Release

Goal:

Make PRD1 usable and credible as an open-source release candidate.

Must deliver:

- request/session evidence summary;
- timeline summary;
- source refs;
- direct/derived/gap/suppressed summary;
- usage/cost summary where available;
- payload policy state;
- policy simulation summary;
- local/on-prem setup guide;
- fixture runner;
- conformance checks;
- release evidence for one UniRouter and one LiteLLM execution.

Candidate tickets:

| Ticket | Owner | Description | Blocks |
| --- | --- | --- | --- |
| XIN-P1-501 | X-Insight | Build evidence pack view. | release |
| XIN-P1-502 | X-Insight | Add basic export or copyable evidence summary. | operator handoff |
| XIN-P1-503 | X-Insight | Add local/on-prem setup path. | release |
| XIN-P1-504 | X-Insight | Add demo fixtures and conformance scripts. | release evidence |
| XIN-P1-505 | X-Insight | Add release checklist for Gate A-E. | scope control |

Exit evidence:

- A new evaluator can run the product locally/on-prem with documented setup.
- The release can show one UniRouter execution and one LiteLLM execution.
- The product remains useful without private development-environment knowledge.

## Gate Mapping

| Gate | Required milestones |
| --- | --- |
| Gate A: internal UniRouter evidence loop | M0, M1, minimal M2 |
| Gate B: UniRouter high-fidelity readiness | M4 plus M1 adapter compatibility |
| Gate C: external gateway credibility | M3 plus M2 shared UI |
| Gate D: governance value | M5 plus M2 labels and gaps |
| Gate E: runnable release | M6 plus completed Gate A/C/D evidence |

## Engineering Sequence Recommendation

Recommended sequence:

1. M0 first.
2. M1 immediately after M0 has enough schema to ingest UniRouter.
3. M2 starts as soon as M1 can import one request.
4. M3 starts after M0 stabilizes enough that LiteLLM does not bend the schema.
5. M4 runs in parallel after M1 exposes the real UniRouter field gaps.
6. M5 starts after M2 can display evidence labels and gaps.
7. M6 starts only after Gate A and Gate C both work.

Reason:

Building UI before real UniRouter import risks a fake demo. Building UniRouter structural fields before import risks optimizing the wrong event shape. Building LiteLLM too late risks a UniRouter-only product. Building policy too early risks a governance product without evidence.

## Current Non-Decision Items

These do not need to block engineering planning:

- final open-source license;
- commercial packaging boundary;
- OTel/OpenInference ingestion timing;
- one-api/new-api implementation order;
- ClickHouse introduction threshold;
- final product name/category.

They should remain visible, but they should not reopen PRD1 scope unless they affect Gate A-E.

## Product Risk Check

| Risk | Engineering control |
| --- | --- |
| PRD1 becomes a UniRouter plugin | M3 must run without UniRouter. |
| PRD1 becomes observability clone | M2 and M5 must emphasize evidence strength, gaps, payload policy, and simulation. |
| PRD1 becomes governance theater | M5 must show which evidence supports or limits each simulation result. |
| PRD1 over-collects sensitive content | M0/M5 default raw payload off and represent suppression explicitly. |
| PRD1 overbuilds infrastructure | Postgres first; ClickHouse, OTel backend, outbox, and policy language later. |
| PRD1 has fake demo data | Gate A and Gate C require real or controlled source execution evidence. |

## Current Judgment

This is a workable PRD1 engineering path if the team is disciplined about the cut line:

- X-Insight owns evidence, gaps, payload policy, simulation, and UI.
- UniRouter owns source facts and can become the F3 reference emitter.
- LiteLLM proves external credibility.
- PRD1 does not claim full agent governance.

The first engineering decision should be to implement M0 and M1, not to start with a polished frontend or a native UniRouter exporter.

First implementation package:

- use [PRD1 M0/M1 First Implementation Package Candidate](./prd1-m0-m1-first-implementation-package-candidate.md) as the code-start boundary.
