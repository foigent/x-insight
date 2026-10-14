# PRD1 M2 Timeline UI Information Architecture Candidate

Status: M2 UI information architecture candidate, not final PRD
Created: 2026-07-08

The User selected this capability family as the first Phase 2 product priority
on 2026-07-11. Detailed information architecture and implementation scope remain
candidate material until separately accepted. See
[`docs/planning/phase-2-product-priority.md`](../planning/phase-2-product-priority.md).

## Purpose

This document narrows the PRD1 timeline UI information architecture.

It is derived from:

- `PRD1 Candidate Scope`
- `PRD1 Dual Demo Definition`
- `PRD1 Minimal Data Model`
- `PRD1 Engineering Milestones Candidate`
- `PRD1 M0 Evidence Envelope Candidate`
- `PRD1 M0 Schema And Indexes Candidate`

The goal is to prove that the evidence envelope can become a human-readable execution evidence chain, not just a better log table.

## Core Judgment

The first UI should be an operator investigation console.

It should not be:

- a generic metrics dashboard;
- a full graph canvas;
- a security incident console;
- a policy authoring workspace;
- an ontology browser;
- a raw trace/log viewer.

The first user should be able to answer:

1. What happened in this AI execution?
2. Which facts are direct evidence?
3. Which facts are derived?
4. What is suppressed by payload policy?
5. What evidence is missing?
6. What would a policy simulation have flagged?
7. What source record proves or limits each claim?

## PRD1 Screen Map

### M2 Must-Have Screens

1. Execution search/list.
2. Execution detail with timeline.
3. Node detail panel.
4. Capture-gap panel.
5. Payload policy state display.

### M5 Adds

1. Policy simulation result panel.
2. Matched rule details.
3. Missing-evidence explanation for simulation confidence.

### M6 Adds

1. Evidence pack view.
2. Basic copy/export evidence summary.
3. Release/demo fixture entry points.

### Not PRD1

- generic dashboard homepage;
- graph canvas;
- actor inventory;
- policy editor;
- alert center;
- payload body viewer;
- compliance report builder;
- SIEM/SOC workflow;
- business ontology explorer.

## Primary Navigation Model

PRD1 should open to execution search/list, not a dashboard.

Reason:

- the user's pain point is investigation and explanation;
- dashboards push the product toward observability;
- execution search immediately tests whether the evidence model is usable.

Minimum navigation:

```text
Executions
Sources
Import Diagnostics
```

Optional later:

```text
Policies
Evidence Packs
Settings
```

M2 should not require a full sidebar product shell if that delays the execution detail view.

## Execution Search / List

Purpose:

Find a request/session/execution and enter the detail view.

Minimum columns:

| Column | Reason |
| --- | --- |
| Time | Start of investigation. |
| Execution / Request ID | Primary lookup key. |
| Source | UniRouter, LiteLLM, etc. |
| Fidelity | F1/F2/F3 honesty. |
| Actor / Project | Accountability. |
| Capability | Model/provider/model group. |
| Outcome | Success/failure/unknown. |
| Usage | Tokens/cost/latency when available. |
| Payload State | `none`, `metadata_only`, `redacted`, etc. |
| Gaps | Count or highest severity. |
| Simulation | M5 result summary, optional in M2. |

Minimum filters:

- request ID;
- time range;
- source type/system;
- org/project;
- actor/owner when available;
- model/provider;
- outcome/severity;
- payload capture mode;
- has capture gaps.

M2 can defer:

- saved searches;
- complex query builder;
- full-text search;
- dashboard charts.

## Execution Detail Layout

Recommended desktop layout:

```text
Header summary
------------------------------------------------------------
Timeline / evidence chain       Selected node detail
Timeline / evidence chain       Selected node detail
Timeline / evidence chain       Gaps / simulation side panel
```

Mobile or narrow layout:

```text
Header summary
Timeline
Selected node detail
Gaps / simulation
```

M2 rule:

- the timeline is the main object;
- details explain selected nodes;
- side panels summarize gaps and policy simulation;
- no panel should require reading raw JSON to understand the execution.

## Header Summary

Purpose:

Give the operator a one-screen answer before drilling into nodes.

Minimum header fields:

| Field | Reason |
| --- | --- |
| Execution ref | Request/session identity. |
| Source | UniRouter/LiteLLM/source system. |
| Adapter fidelity | Prevent false completeness claims. |
| Time range | When execution happened. |
| Actor/project/org | Accountability. |
| Capability summary | Requested/resolved model, provider, model group. |
| Outcome | Success/failure/unknown. |
| Usage summary | Tokens/cost/latency when available. |
| Payload state | Raw off, metadata-only, redacted, reference-only, etc. |
| Evidence visibility summary | Counts of direct/derived/suppressed/gap facts. |
| Simulation summary | M5, optional placeholder in M2. |

Do not use a single "trust score" in PRD1.

Reason:

A score hides why evidence is weak. Counts and labels are more honest and more useful for early enterprise evaluation.

## Timeline Model

Timeline nodes should be derived from `evidence_events`, not stored as the source of truth.

M2 recommendation:

- build timeline at query time first;
- materialize timeline nodes only if performance requires it;
- every node must retain source event IDs and field-group provenance.

Minimum node types:

| Node type | Purpose |
| --- | --- |
| `gateway` | Request accepted/rejected at gateway boundary. |
| `identity` | Actor, key, owner, org, project context. |
| `routing` | Model/route/provider/channel selection. |
| `provider` | Provider request/response events. |
| `billing` | Freeze/settle/cost decisions. |
| `usage` | Token/cost/latency recording. |
| `payload` | Payload capture policy state. |
| `gap` | Missing evidence. |
| `simulation` | Historical policy simulation output. |
| `external` | Callback/import/source-specific event when not otherwise mapped. |

Minimum node fields:

| Field | Reason |
| --- | --- |
| `node_id` | UI identity. |
| `execution_ref` | Request/session/trace/run reference. |
| `event_ids` | Source evidence events. |
| `node_type` | Human grouping. |
| `title` | Human-readable node title. |
| `timestamp` | Ordering. |
| `status` | Success/failure/unknown. |
| `evidence_strength` | Direct/derived/declared/suppressed/gap. |
| `source_refs` | Audit/debug path. |
| `summary_fields` | Small display fields only. |
| `gap_count` | Node-linked gaps. |
| `payload_capture_mode` | If relevant. |

## Timeline Ordering

Default ordering:

1. `occurred_at`
2. source order/cursor when available
3. stage order fallback
4. event ID fallback

Stage fallback order:

1. `gateway`
2. `identity`
3. `routing`
4. `billing`
5. `provider`
6. `usage`
7. `logging`
8. `payload`
9. `gap`
10. `simulation`
11. `external`
12. `unknown`

Reason:

Some sources will not provide perfect timestamps. PRD1 still needs a stable, understandable order.

## Evidence Labels

The UI should display field-group provenance, not just event status.

Labels:

- `Direct`
- `Derived`
- `Declared`
- `Suppressed`
- `Gap`
- `Unknown`

Rules:

- `Direct` means source-observed evidence.
- `Derived` means X-Insight or adapter interpretation.
- `Declared` means config/manifest/policy declaration, not runtime observation.
- `Suppressed` means intentionally hidden by payload policy.
- `Gap` means the claim cannot be proven with current evidence.
- `Unknown` should be rare; prefer concrete gap types.

Do not rely on color alone. Labels must be readable as text.

## Selected Node Detail

Purpose:

Explain exactly what the selected node claims and what proves it.

Minimum sections:

1. Summary
2. Evidence
3. Provenance
4. Source references
5. Related gaps
6. Payload state
7. Raw safe metadata

### Summary

Human-readable fields:

- title;
- stage;
- node type;
- timestamp;
- outcome/status;
- important capability/identity/usage values.

### Evidence

Show:

- direct event count;
- derived field groups;
- source refs;
- adapter manifest/fidelity;
- capture mode.

### Provenance

Show field-group labels:

- correlation;
- identity;
- capability;
- decision;
- usage;
- outcome;
- payload policy;
- business outcome;
- tool side effect.

### Source References

Show:

- source system;
- source ref;
- source event type;
- source event time;
- adapter version.

Do not expose secrets or raw payload.

### Related Gaps

Show:

- gap type;
- affected claim;
- reason;
- severity;
- recommended instrumentation.

### Payload State

Show:

- payload capture mode;
- prompt/response capture modes if available;
- redaction state;
- retention policy ref if relevant;
- payload ref only when policy allows.

Never show a blank payload area. Blank reads as product failure. The UI should explicitly say `suppressed`, `metadata_only`, `redacted`, `reference_only`, `available`, or `not_applicable`.

### Raw Safe Metadata

Show only `raw_source_excerpt` after sensitive-key filtering.

This is for debugging, not the primary UI.

## Capture-Gap Panel

Purpose:

Make missing evidence a product object, not a hidden null.

Minimum panel groups:

- identity gaps;
- decision gaps;
- payload gaps;
- business outcome gaps;
- tool side-effect gaps;
- source limitation gaps;
- retention/permission gaps.

Each gap should show:

- affected claim;
- gap type;
- severity;
- why it is missing;
- which adapter/source detected it;
- recommended instrumentation.

Important:

The panel should not shame weak sources. It should explain fidelity honestly.

LiteLLM with F1/F2 evidence should show more gaps than UniRouter F3 target. That is correct.

## Policy Simulation Panel

M5 feature, but M2 should reserve the slot.

Minimum M5 display:

- simulation run name/version;
- rule result;
- matched fields;
- missing evidence limiting confidence;
- affected execution/node;
- source events used.

Rules:

- historical simulation must be labeled as simulation;
- do not imply the gateway actually blocked the request;
- simulation results should link back to evidence fields and gaps.

## Payload Policy Display

Payload state must be visible in both:

- header summary;
- selected node detail.

Minimum states:

- `none`
- `metadata_only`
- `hash_only`
- `redacted`
- `reference_only`
- `full_content`

UI meaning:

| State | Human meaning |
| --- | --- |
| `none` | Payload was not captured. |
| `metadata_only` | Only safe metadata is available. |
| `hash_only` | Content proof exists without readable content. |
| `redacted` | Redacted content may be available. |
| `reference_only` | Payload is stored externally or customer-managed. |
| `full_content` | Full payload exists under governed controls. |

M2 should not implement full payload reading. It only needs to display the state honestly.

## UniRouter Rendering

UniRouter timeline should be able to show:

- gateway request accepted;
- identity/key/project/owner attribution;
- billing freeze result;
- route selected or no route;
- provider request/response;
- usage/cost/latency;
- call log recorded;
- payload policy state;
- capture gaps;
- simulation placeholder/result.

Expected evidence quality:

- many fields direct from current trace/call-log rows;
- current decisions often derived from event type until UniRouter emits explicit decision fields;
- F3 label should wait for structural UniRouter fields.

## LiteLLM Rendering

LiteLLM timeline should use the same layout but show lower fidelity honestly.

It should be able to show:

- request observed through callback or spend log;
- team/user/end-user where available;
- model/model group/provider where available;
- usage/spend/latency/cache where available;
- session/agent IDs where available;
- payload policy state;
- capture gaps for missing routing, policy decision, business outcome, and tool side effects;
- simulation placeholder/result.

Expected evidence quality:

- F1 for spend-log import;
- F2 for callback;
- not F3 unless LiteLLM or a wrapper emits source-native governance semantics.

## API / Projection Implications

M2 likely needs these API shapes:

```text
GET /api/executions
GET /api/executions/{execution_ref}
GET /api/executions/{execution_ref}/timeline
GET /api/events/{event_id}
GET /api/executions/{execution_ref}/gaps
GET /api/executions/{execution_ref}/simulation-results
GET /api/import-diagnostics/quarantine
```

Implementation notes:

- `execution_ref` can be request ID first.
- Trace/session/run refs should be reserved but not required.
- Timeline API returns derived nodes plus source event IDs.
- Event detail API returns sanitized metadata only.
- Quarantine is import diagnostics, not evidence timeline.

## Acceptance Criteria

M2 succeeds if:

1. A real or controlled UniRouter request can be found from execution search.
2. The execution detail page renders a timeline without hand-authored demo nodes.
3. A user can identify direct, derived, suppressed, and gap facts without reading raw logs.
4. Selected node detail shows source refs and adapter fidelity.
5. Capture gaps show affected claim, reason, severity, and recommended instrumentation.
6. Payload state is visible and raw prompt/response is not required.
7. The same UI can render a LiteLLM execution with lower fidelity labels.
8. Timeline nodes are derived from evidence events and do not become source of truth.

M2 fails if:

- it looks like a log table with nicer styling;
- it hides missing evidence;
- it makes LiteLLM look as complete as UniRouter;
- it requires raw prompt/response to be useful;
- it starts with dashboards before execution detail works.

## Open Questions For Discussion

These need product judgment, not deep technical debate:

1. Should the UI label the primary object as `Execution`, `Request`, or `Run`?
   - Recommendation: use `Execution` in product language, support request ID as the first lookup key.
2. Should PRD1 show an evidence visibility summary in the header?
   - Recommendation: yes, as direct/derived/suppressed/gap counts, not a single score.
3. Should source refs be visible by default or behind a detail toggle?
   - Recommendation: visible in node detail, not cluttering every timeline row.
4. Should M2 include import diagnostics in the product nav?
   - Recommendation: yes, minimal `Import Diagnostics`, because quarantine is essential for operator trust during adapter setup.
5. Should the first public demo emphasize UniRouter high-fidelity or LiteLLM neutrality first?
   - Recommendation: show UniRouter first for depth, then LiteLLM in the same UI for neutrality.

## Current Recommendation

Use this document as the M2 information architecture candidate.

Build:

- execution search;
- execution detail;
- derived timeline;
- node detail;
- gap panel;
- payload state display;
- source/fidelity labels.

Reserve:

- policy simulation panel slot for M5;
- evidence pack for M6;
- graph view for later.

Do not start with a dashboard.
