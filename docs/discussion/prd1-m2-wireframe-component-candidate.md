# PRD1 M2 Wireframe And Component Candidate

Status: M2 wireframe/component candidate, not final UI spec
Created: 2026-07-08

## Purpose

This document narrows the first UI terminology and component breakdown for the M2 timeline experience.

It builds on:

- `PRD1 M2 Timeline UI Information Architecture Candidate`
- `PRD1 M0 Evidence Envelope Candidate`
- `PRD1 M0 Schema And Indexes Candidate`
- `PRD1 Engineering Milestones Candidate`

The goal is not visual design. The goal is to make the first implementation coherent enough that the UI proves the evidence model.

## Terminology Decision Candidate

Use `Execution` as the primary product object.

Why:

- `Request` is too gateway-specific and too narrow for future agent/tool/business workflows.
- `Run` is common in agent frameworks but may feel too developer-framework-specific.
- `Execution` can contain one gateway request now and a multi-step agent run later.
- Operators can still search by request ID, trace ID, session ID, or run ID.

M2 wording:

- Navigation: `Executions`
- Page title: `Execution Detail`
- Search field label: `Request, trace, session, or execution ID`
- Header field: `Execution Ref`
- Node language: `Evidence Node`

Avoid in PRD1:

- `Incident`
- `Case`
- `Trace`
- `Workflow`
- `Ontology`
- `Graph`

Reason:

Those terms pull the product toward security case management, observability, BPM, or ontology platforms before PRD1 proves evidence value.

## First View: Executions

Purpose:

Find an execution and understand evidence quality before entering detail.

Recommended layout:

```text
Toolbar: source / time / search / filters
Summary strip: total / gaps / suppressed payload / failed executions
Table: execution rows
```

### Toolbar Components

Required:

- source selector;
- time range selector;
- search input;
- filter button;
- refresh button.

Search supports:

- request ID;
- trace ID;
- session ID;
- execution ref;
- source ref.

M2 should not include:

- saved search;
- natural-language query;
- dashboard builder;
- complex query DSL.

### Summary Strip

Required summary counters:

- executions in selected range;
- executions with gaps;
- executions with suppressed payload;
- failed or warning executions;
- sources connected.

Important:

These are navigation aids, not a dashboard. Do not add charts before execution detail works.

### Execution Table

Minimum columns:

| Column | M2 behavior |
| --- | --- |
| Time | Sort descending by default. |
| Execution Ref | Shows request ID first; opens detail. |
| Source | Source system and adapter fidelity. |
| Actor / Project | Best available identity. |
| Capability | Requested/resolved model or model group. |
| Outcome | Success/failure/warning/unknown. |
| Usage | Tokens/cost/latency when available. |
| Payload | Capture mode chip. |
| Evidence | Direct/derived/suppressed/gap counts. |
| Gaps | Count and highest severity. |

Row states:

- normal;
- warning;
- failed;
- has critical gap;
- quarantined import related, if surfaced.

Click behavior:

- row opens execution detail;
- source/fidelity chip opens source/adapter detail later, not required in M2;
- gap count can deep-link to gaps section in detail.

## Second View: Execution Detail

Purpose:

Make one AI execution understandable without raw logs.

Recommended desktop layout:

```text
Execution header
Evidence visibility strip
------------------------------------------------------------
Timeline                       Node detail
Timeline                       Node detail
Timeline                       Gaps / simulation / payload
```

M2 implementation can use two columns:

- main column: timeline;
- side column: selected node detail plus gaps.

Three-column layout can wait if implementation complexity is high.

## Execution Header Component

Required fields:

- execution ref;
- request ID;
- source system;
- adapter fidelity;
- time range;
- actor/project/org;
- requested model;
- resolved model/provider;
- outcome;
- latency;
- cost/tokens;
- payload capture mode.

Badges:

- `F1`, `F2`, or `F3`;
- `Direct evidence`;
- `Derived evidence`;
- `Gaps`;
- `Payload suppressed` or current payload mode;
- `Simulation result` when M5 exists.

M2 rule:

- Use counts, not a single trust score.
- If a value is missing, show the gap or provenance state instead of a blank.

## Evidence Visibility Strip

Purpose:

Show evidence quality at a glance.

Recommended counters:

- direct facts;
- derived facts;
- declared facts;
- suppressed facts;
- gaps;
- unknown facts.

Click behavior:

- clicking `Gaps` scrolls or filters to gaps;
- clicking `Suppressed` highlights payload/policy nodes;
- clicking `Derived` highlights derived nodes.

Do not call this a score.

## Timeline Component

Required behavior:

- vertical chronological evidence chain;
- grouped by stage when useful;
- selected node state;
- evidence label on every node;
- status icon or label;
- timestamp;
- short human-readable title;
- one-line summary;
- source count/ref indicator;
- gap indicator;
- payload indicator where relevant.

Minimum node titles:

- Request accepted
- Identity resolved
- Route selected
- Provider request attempted
- Provider response accepted
- Billing freeze succeeded
- Billing settled
- Usage recorded
- Call log recorded
- Payload policy applied
- Capture gap detected
- Policy simulation matched

LiteLLM-specific examples:

- LiteLLM request observed
- Team/user mapped
- Model invocation recorded
- Spend recorded
- Session linked
- Capture gap detected

M2 should allow a node to aggregate multiple events only when:

- they share the same stage;
- aggregation does not hide provenance differences;
- the detail panel still lists all source event IDs.

## Node Detail Component

Required sections:

1. Summary
2. Evidence
3. Provenance
4. Source refs
5. Related gaps
6. Payload state
7. Safe metadata

Default open section:

- Summary.

Always visible:

- evidence label;
- adapter fidelity;
- source ref count;
- payload state if relevant;
- gap count if relevant.

Source refs:

- visible in detail;
- not shown on every timeline row unless compact.

Safe metadata:

- collapsed by default;
- sanitized `raw_source_excerpt` only;
- never raw prompt/response.

## Capture Gap Component

Required display:

- gap type;
- affected claim;
- reason;
- severity;
- source/adapter;
- recommended instrumentation.

Severity labels:

- `info`;
- `warning`;
- `critical`.

M2 behavior:

- gap panel filters timeline nodes;
- each gap links to related node/event if available;
- global execution gaps can appear even without a single event ID.

## Payload State Component

Required states:

- `none`
- `metadata_only`
- `hash_only`
- `redacted`
- `reference_only`
- `full_content`

Display rule:

- show payload state in header and node detail;
- do not show a blank prompt/response area;
- do not implement full content viewer in M2;
- if payload is unavailable by policy, say so directly.

## Simulation Slot

M2 should reserve a component slot for M5.

Before M5:

- show no simulation run, or "No simulation run selected" in implementation language.

After M5:

- show matched/not matched;
- rule name/version;
- evidence fields used;
- missing evidence limiting confidence;
- link to nodes/events.

Do not imply live blocking.

## Import Diagnostics

Recommended minimal M2 navigation item:

- `Import Diagnostics`

Purpose:

Make quarantine visible during adapter setup.

Minimum table:

| Column | Purpose |
| --- | --- |
| Time | When quarantined. |
| Source | Source system if known. |
| Adapter | Adapter manifest if known. |
| Reason | Missing ref, invalid enum, secret detected, etc. |
| Severity | Warning/error/critical. |
| Safe excerpt | Sanitized preview. |

M2 should not make quarantine part of the execution timeline unless explicitly linked from an import error.

## Empty And Error States

Execution search empty:

- no executions in selected time range;
- source connected but no events;
- import not configured.

Execution detail missing:

- execution not found;
- events quarantined;
- source ref no longer accessible;
- permission denied.

Timeline partial:

- show available nodes;
- show capture gaps for missing expected claims;
- do not pretend completeness.

Payload unavailable:

- show payload state;
- show policy/source reason if available.

## M2 Component Acceptance Criteria

The first UI component implementation passes if:

1. Execution search opens one real UniRouter execution.
2. Execution detail renders derived timeline nodes from evidence events.
3. Every node has an evidence/provenance label.
4. Node detail shows source refs without exposing raw payload.
5. Payload state appears in header and node detail.
6. Capture gaps are visible and linked to affected claims.
7. LiteLLM execution can render in the same structure with lower fidelity labels.
8. Import diagnostics can show quarantined events without turning them into evidence.

It fails if:

- the first screen is a metrics dashboard;
- timeline rows are just raw log lines;
- missing evidence appears as blank fields;
- source refs are impossible to inspect;
- payload state is hidden;
- UniRouter and LiteLLM require different UI structures.

## Open Questions For User Judgment

1. Should we lock `Execution` as the product term for PRD1?
   - Recommendation: yes.
2. Should `Import Diagnostics` appear in the main nav in PRD1?
   - Recommendation: yes, because adapter setup will fail in real deployments and users need to know why.
3. Should evidence visibility counters appear in the header?
   - Recommendation: yes, but never as a single trust score.

## Current Recommendation

Use this as the first wireframe/component candidate.

Build order:

1. Execution search table.
2. Execution detail header.
3. Derived timeline.
4. Node detail.
5. Gap panel.
6. Payload state.
7. Import diagnostics.

Do not build dashboards until this path works on real UniRouter and LiteLLM evidence.
