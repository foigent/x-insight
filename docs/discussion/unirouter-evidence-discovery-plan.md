# UniRouter Evidence Discovery Plan

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

Status: concrete discussion plan, not PRD
Created: 2026-07-07
Source repo inspected: `PRIVATE_HOME/Desktop/projects/UniRouter`

## Purpose

This document turns the next X-Insight discussion into a concrete UniRouter-based evidence discovery loop.

The goal is not to define the first public demo yet. The goal is to inspect what UniRouter can already emit, what it can cleanly add, what X-Insight can ingest, and where capture gaps must be represented honestly.

## Why This Should Be The Next Discussion

UniRouter is the fastest product-construction flywheel for X-Insight because:

- it is controlled by the same team;
- it is already a real enterprise MaaS/model-gateway product;
- it has existing org/project/key/user/model/provider/cost/audit/log/trace semantics;
- it has data and a runnable environment;
- it can be enhanced when X-Insight needs upstream context that would be awkward to infer downstream.

This does not mean X-Insight should be publicly positioned as a UniRouter plugin.

Updated strategy:

- UniRouter-first for internal construction and reality testing.
- OpenTelemetry/OpenInference/LiteLLM/OpenAI-compatible paths for public credibility and future ecosystem.

## Verified UniRouter Baseline

### 1. PRD4 already defines flow/event/trace foundation

Source:

- `PRIVATE_HOME/Desktop/projects/UniRouter/docs/PRD-Phase4.md`

Relevant verified points:

- `request_id` is the minimum correlation key across call logs, billing, audit, reconciliation, and troubleshooting.
- Future `trace_id`/span semantics are explicitly planned without requiring OpenTelemetry in the first PRD4 batch.
- High-level request stages include gateway received, auth checked, budget prechecked, billing frozen, model resolved, channel selected, provider called, usage parsed, billing settled/cancelled, call log persisted, audit written, and response returned.
- Trace data must not store prompts, responses, full API keys, upstream secrets, or raw tokens.
- Missing trace data must be represented as unavailable or partial, not fabricated.

X-Insight implication:

- UniRouter already has a request-flow concept close to X-Insight's early evidence model.
- The first X-Insight event ingestion can be request-centric, but must preserve a path to run/span/action semantics.

### 2. `request_trace_events` already exists

Sources:

- `PRIVATE_HOME/Desktop/projects/UniRouter/migrations/000017_prd4_request_trace_events.up.sql`
- `PRIVATE_HOME/Desktop/projects/UniRouter/internal/gateway/trace.go`

Existing table fields:

- `request_id`
- `org_id`
- `project_id`
- `api_key_id`
- `owner_user_id`
- `key_type`
- `event_type`
- `stage`
- `severity`
- `model`
- `requested_model`
- `provider`
- `channel`
- `status_code`
- `latency_ms`
- `metadata`
- `created_at`

Existing behavior:

- trace events are sampled through `traceEventSampleRate`;
- metadata is allowlisted;
- string metadata is truncated;
- prompt/response/secrets are not included by design;
- org/project/key/owner context is captured from request context.

X-Insight implication:

- This is a strong first event source.
- It already maps to actor, capability, action, outcome, timing, and evidence metadata.
- It lacks some X-Insight concepts such as explicit `capture_mode`, `evidence_ref`, `decision_kind`, `trace_id`, `span_id`, and `capture_gap`.

### 3. Request trace read API already exists

Source:

- `PRIVATE_HOME/Desktop/projects/UniRouter/internal/admin/log_handlers.go`

Existing behavior:

- `GetRequestTrace` loads trace events by call-log request ID.
- It enforces project usage read permission.
- It returns ordered events with event type, stage, severity, model, provider, channel, status code, latency, metadata, and timestamp.

X-Insight implication:

- X-Insight can initially ingest from database tables or an API-shaped export.
- Access control semantics already exist in UniRouter and should inform X-Insight's evidence access model.

### 4. Prompt/response storage policy is already privacy-aligned

Source:

- `PRIVATE_HOME/Desktop/projects/UniRouter/docs/tech-ref/19-prompt-response-storage-policy.md`

Relevant verified points:

- plaintext prompt/response storage is disabled by default;
- request troubleshooting is metadata-only;
- future plaintext storage requires explicit approval, retention, encryption, redaction, RBAC, audit-on-read, customer controls, export controls, and incident response;
- reads must be audited before content is returned.

X-Insight implication:

- This aligns with X-Insight's on-premises and enterprise-controlled evidence posture.
- X-Insight should inherit the same philosophy: safe defaults, customer-controlled opt-in, and audited reads.

### 5. PlungeIn port contract maps well to X-Insight

Source:

- `PRIVATE_HOME/Desktop/projects/UniRouter/docs/tech-ref/22-prd4-plungein-port-contract.md`

Existing port classes:

- Observe
- Act
- Enrich
- Decide
- Mutate

PRD4 boundaries:

- Observe is allowed for request traces, call logs, audit metadata, runtime status, metrics summaries, and notification events.
- Act is allowed with guardrails for notifications, webhooks, deep links, export jobs, and operator-visible evidence.
- Enrich is limited and non-authoritative.
- Decide is advisory only.
- Mutate is deferred.

X-Insight implication:

- PRD1 X-Insight should primarily use Observe.
- Evidence export can use guarded Act.
- Classifications and risk annotations can use non-authoritative Enrich.
- Shadow policy simulation can use advisory Decide.
- Mutate/enforcement should not be part of the first integration.

### 6. Real upstream evidence harness exists

Source:

- `PRIVATE_HOME/Desktop/projects/UniRouter/docs/tech-ref/23-prd4-real-upstream-evidence.md`

Existing behavior:

- guarded real-provider evidence path for OpenRouter-compatible and Anthropic-native adapter paths;
- preflight mode is safe and non-mutating;
- real-provider mode requires explicit approval;
- evidence excludes provider API keys, generated raw API keys, prompt, full response body, and raw secret data.

X-Insight implication:

- UniRouter already has a pattern for "approved target evidence".
- X-Insight can reuse this safety posture for evidence discovery and demo data.

## Proposed Discussion Agenda

### Topic 1: UniRouter To X-Insight Event Mapping

Map current UniRouter fields to X-Insight concepts.

| X-Insight concept | UniRouter current source | Current strength | Gap |
| --- | --- | --- | --- |
| Run / correlation | `request_id` | Strong for one model request | Needs future `trace_id`/`span_id` for multi-step runs |
| Actor | `owner_user_id`, `api_key_id`, `key_type`, org/project context | Good for gateway/API key attribution | Needs explicit actor type, service owner, on-behalf-of if available |
| Capability | `model`, `requested_model`, `provider`, `channel`, adapter route | Good for model/provider/channel capability | Needs generic capability model beyond model gateway |
| Action | `event_type`, `stage` | Good for gateway stages | Needs action taxonomy mapped to X-Insight verbs |
| Resource | model/channel/provider/project/key | Good for gateway resources | Weak for business resources touched outside gateway |
| Decision | billing reject, model not allowed, routing no route, provider rejected | Partial | Needs explicit decision record and reason taxonomy |
| Evidence | trace events, call logs, audit rows, real upstream evidence files | Good metadata evidence | Needs `evidence_ref`, integrity, capture mode |
| Outcome | status code, event severity, billing settle, call log recorded | Good for request outcome | Needs side-effect and capture-gap outcome types |

### Topic 2: What UniRouter Can Already Show

Current UniRouter should be able to support an early X-Insight view showing:

- request accepted;
- project/org/key/owner context;
- requested model versus mapped model;
- budget freeze success/failure/rejection;
- selected route count and route index;
- provider/channel chosen;
- provider request failure/rejection/acceptance;
- usage source and confidence;
- billing settlement success/failure;
- token usage and cost;
- call log persistence;
- missing or partial trace data.

This is already enough to build:

- a gateway execution timeline;
- a metadata-only evidence pack;
- a first capture-gap report;
- a policy-simulation placeholder using historical events.

### Topic 3: What UniRouter Should Add For X-Insight

Candidate low-risk additions:

- `trace_id`
- `span_id`
- `parent_span_id`
- `capture_mode`
- `capture_gap`
- `evidence_ref`
- `decision_kind`
- `decision_reason`
- `actor_type`
- `actor_ref`
- `owner_ref`
- `source_component`
- `schema_version`
- `source_event_hash` or batch integrity field

Candidate additions that need more caution:

- declared business intent;
- on-behalf-of chain;
- human approval events;
- tool-call metadata;
- external side-effect events;
- customer-managed evidence object references.

### Topic 4: What UniRouter Cannot Know

UniRouter cannot reliably know:

- internal business tool side effects outside the gateway;
- database writes inside application code;
- direct SaaS API calls by an agent after model output;
- browser/computer-use actions;
- MCP tool internals unless MCP traffic is instrumented;
- business resource semantics unless upstream apps declare them.

These should become explicit X-Insight `capture_gap` states, not hidden assumptions.

### Topic 5: First X-Insight Foundation Before First Demo Scenario

Before choosing the first public demo, build enough foundation to answer:

- What real execution chain does UniRouter data produce?
- Which nodes are human-readable?
- Which nodes are only technical stages?
- Which nodes are policy/decision points?
- Which nodes are missing business side effects?
- Which fields are necessary for an evidence pack?

Only after this should the first public demo scenario be chosen.

## Proposed Immediate Output

The next concrete artifact should be:

> X-Insight / UniRouter Evidence Contract v0.1

It should define:

1. event envelope fields;
2. UniRouter source tables/APIs;
3. event type mapping;
4. decision taxonomy;
5. capture modes;
6. evidence refs;
7. capture-gap states;
8. what UniRouter emits versus what X-Insight derives;
9. what should not be stored;
10. what remains impossible without app/tool/OTel/MCP instrumentation.

## Why This Is Better Than Picking A Demo Now

Picking a demo first risks overfitting the product to one story.

Evidence discovery first lets the first demo emerge from real product capability:

- if UniRouter data is already rich enough, the demo can be request-flow evidence and policy simulation;
- if the biggest gap is side effects, the demo should show capture-gap visibility honestly;
- if the biggest strength is billing/routing/accountability, the demo can show "who used what model, through which key/project, with what cost and route evidence";
- if tool/data actions are missing, PRD1 can avoid overclaiming full agent execution governance.

## Current Recommendation

Discuss and define `X-Insight / UniRouter Evidence Contract v0.1` before Vision v0.1 or PRD1.

Reason:

- the repository already has a real trace/event substrate;
- the product's first build can be grounded in actual UniRouter evidence rather than advisor abstractions;
- it will clarify what X-Insight can honestly prove in version 1;
- it will determine which future adapters are required for one-api/new-api/LiteLLM/OpenTelemetry/MCP compatibility.
