# X-Insight Adapter Integration Strategy

Status: discussion draft, not PRD
Created: 2026-07-08

Source freshness:

- UniRouter deep-inspected at HEAD `59721d7d4284101ad8f2e9a1a6abcd1d7f1a8ba5`; light recheck on 2026-07-08 at `f0d34e1` confirmed relevant request trace/call log/admin trace surfaces still present.
- one-api remote HEAD checked on 2026-07-08: `8df4a2670b98266bd287c698243fff327d9748cf`
- new-api remote HEAD checked on 2026-07-08: `6ce7305cd36f16506fb6a2c3c524a5a318539ba7`
- LiteLLM remote HEAD checked on 2026-07-08: `b8248a21d2ac0c102d49c0db5702a48cdf661bb9`
- The detailed field mapping in `Evidence Contract v0.1 Working Draft` was based on a 2026-07-07 local snapshot; rerun source extraction before implementation.
- The newer `Gateway Adapter Mapping Matrix v0.1` records the latest source baseline and the LiteLLM fetch caveat.

## Purpose

This document defines how X-Insight should integrate with UniRouter, new-api, one-api, LiteLLM, and future gateway/runtime systems.

The central principle:

> X-Insight owns the evidence contract. Adapters translate external systems into that contract with explicit fidelity and capture gaps.

## Strategic Position

X-Insight should not become another model gateway and should not require customers to replace their existing gateway.

The integration architecture should support:

- deep native integration for UniRouter;
- low-friction import for one-api/new-api;
- callback and OpenTelemetry-style integration for LiteLLM;
- future SDK, MCP, and business-system instrumentation.

## Agreed Direction As Of 2026-07-08

These points are current discussion consensus, not final PRD commitments:

- Define adapter fidelity levels instead of saying each gateway is simply "supported."
- Build a `Gateway Adapter Mapping Matrix v0.1` before PRD1.
- Current matrix: [Gateway Adapter Mapping Matrix v0.1](./gateway-adapter-mapping-matrix-v0.1.md).
- Current demo definition: [PRD1 Dual Demo Definition](./prd1-dual-demo-definition.md).
- Current minimal data model: [PRD1 Minimal Data Model](./prd1-minimal-data-model.md).
- Current PRD1 scope candidate: [PRD1 Candidate Scope](./prd1-candidate-scope.md).
- Current UniRouter boundary: [UniRouter / X-Insight Integration Boundary](./unirouter-xinsight-integration-boundary.md).
- Completed pre-PRD artifact sequence: `Gateway Adapter Mapping Matrix v0.1`, `PRD1 Dual Demo Definition`, then `PRD1 Minimal Data Model`.
- Use UniRouter native integration first for speed and high-fidelity internal validation.
- Add LiteLLM early for external credibility and to avoid UniRouter-only positioning.
- PRD1 LiteLLM adapter should include a real callback endpoint plus spend-log import, not only a demo stub.
- Support new-api and one-api through import adapters with explicit fidelity labels.
- Keep PRD1 observe-first: evidence, timeline, capture gaps, and policy simulation.
- PRD1 primary operators should be AI platform owners and enterprise IT; CISO/security teams are important stakeholders but not the first daily operator.
- PRD1 should include lightweight policy simulation UI against historical evidence, not a full policy-authoring system.
- Do not include broad synchronous enforcement in PRD1.
- Do not store prompt/response by default.
- Allow on-prem enterprises to opt into payload storage only with retention, encryption, RBAC, audit-on-read, redaction, and export/deletion controls.
- Treat prompt/response enablement as payload policy with scoped capture modes, not as a single boolean switch.
- Pre-allocate the event-store architecture, but do not force a heavy analytics database before the data volume requires it.
- Position X-Insight as an AI Execution Evidence / Governance Plane, not AI Gateway Observability.
- The first value proposition should be: turn enterprise AI/Agent execution into auditable evidence chains and explicitly show what is visible, what is not visible, and why.
- Open-source boundaries should favor open schemas, adapter SDKs, reference adapters, and examples; commercial candidates include enterprise RBAC, audit-on-read, retention controls, advanced policy simulation, and premium connectors.
- First demo direction should be dual-track:
  - UniRouter deep demo to prove high-fidelity evidence and controlled end-to-end execution context.
  - LiteLLM compatibility demo to prove ecosystem openness and avoid UniRouter-plugin positioning.

## Does X-Insight Require Deploying LiteLLM?

No.

X-Insight should not require a customer to deploy LiteLLM if they already use UniRouter, new-api, one-api, a custom gateway, or direct SDK calls.

There are three separate cases:

1. Development and validation:
   - X-Insight should maintain a local or CI LiteLLM test instance.
   - This proves the adapter against real LiteLLM behavior instead of static fixtures only.

2. Public demo and ecosystem credibility:
   - X-Insight should provide a LiteLLM demo stack because LiteLLM is recognizable to the target ecosystem.
   - This is a go-to-market signal, not a product dependency.

3. Customer deployment:
   - If the customer already runs LiteLLM, X-Insight connects to it.
   - If the customer does not run LiteLLM, X-Insight should not force adoption.
   - X-Insight should use the customer's existing source: UniRouter, new-api, one-api, OTel, SDK, MCP, or business-system adapters.

Reason:

- Making LiteLLM mandatory would turn X-Insight into an ecosystem add-on instead of an independent governance plane.
- Not supporting LiteLLM early would weaken public credibility.
- The correct balance is "LiteLLM supported early, not required."

## Integration Modes

### Mode 1: Native Event Emitter

Best for:

- UniRouter;
- future first-party components;
- deeply controlled customer integrations.

How it works:

- The source system emits X-Insight evidence events directly.
- Events follow `Evidence Contract v0.1`.
- Events are async and non-blocking by default.
- A local queue or retry buffer prevents X-Insight outages from breaking gateway traffic.

Why:

- Highest fidelity.
- Cleanest support for `trace_id`, `span_id`, `decision_record`, `capture_gap`, and `evidence_ref`.
- Best reference implementation for what "good evidence" means.

Risk:

- If only UniRouter has this path, the market may see X-Insight as a UniRouter plugin.

Recommendation:

- Use UniRouter native emission for internal product construction.
- Publicly describe it as one reference adapter, not the product center.

### Mode 2: Database / API Import Adapter

Best for:

- one-api;
- new-api;
- historical UniRouter data;
- LiteLLM spend logs.

How it works:

- Adapter reads source database tables or admin APIs.
- Adapter maintains a cursor or watermark.
- Adapter maps rows into X-Insight evidence events.
- Missing fields are marked as `capture_gap`, not silently invented.

Why:

- Lowest adoption friction.
- Useful for installed customer systems.
- Supports historical evidence and migration.

Risk:

- Weak causality.
- No real-time enforcement.
- Field meaning varies by product and version.

Recommendation:

- Use this as compatibility import, not the gold standard.

### Mode 3: Callback / Webhook Adapter

Best for:

- LiteLLM;
- future gateway products with callback hooks;
- customer internal gateways.

How it works:

- Source calls an X-Insight callback on request success, failure, or policy event.
- Callback payload maps to evidence events.
- Adapter can enrich with source metadata and local lookup.

Why:

- Higher freshness than DB import.
- Better for public ecosystem integration.
- LiteLLM already has a rich callback culture.

Risk:

- Callback payloads may omit internal decisions.
- Callback failures must not break model traffic unless explicitly configured.

Recommendation:

- Make LiteLLM callback adapter one of the first public compatibility paths.

### Mode 4: OpenTelemetry / OpenInference Bridge

Best for:

- public credibility;
- existing enterprise observability environments;
- agent runtimes and frameworks that already emit spans.

How it works:

- X-Insight accepts OTLP or maps from OpenTelemetry GenAI semantic conventions.
- OTel spans become evidence events with `capture_mode=otel`.
- X-Insight adds governance semantics on top: decision records, evidence refs, gap reporting, and policy simulation.

Why:

- Avoids building private adapters for every framework.
- Reduces "not another proprietary dashboard" objections.

Risk:

- OTel is strong for spans, weaker for governance semantics unless attributes are carefully normalized.

Recommendation:

- Treat OTel as an ingestion spine, not a complete product definition.

### Mode 5: SDK / MCP / Business-System Instrumentation

Best for:

- tool calls;
- side effects;
- database writes;
- SaaS API calls;
- human approvals;
- declared business intent.

How it works:

- SDKs or MCP wrappers emit evidence for actions that gateways cannot see.
- Business systems declare resource/action/outcome semantics.
- These events connect to gateway requests through `trace_id`, `span_id`, `session_id`, or explicit correlation IDs.

Why:

- This is required for real agent governance.
- Gateways cannot know what happens after model output is consumed by application code.

Risk:

- Requires adoption inside application/runtime/tool layers.

Recommendation:

- Do not make SDK/MCP mandatory for PRD1, but reserve the contract and UI for its arrival.

## Product-Specific Integration View

### UniRouter

Integration level:

- Native high-fidelity event emitter first.
- Optional historical import from existing trace/call/audit tables.

What UniRouter should emit:

- request accepted;
- auth/key/project/org context;
- model resolved;
- routing candidates and selected route;
- billing freeze/settle decisions;
- provider request outcome;
- token/cost usage;
- log/audit persistence;
- explicit missing evidence states.

Fields UniRouter should add before or during X-Insight PRD1:

- `trace_id`
- `span_id`
- `parent_span_id`
- `schema_version`
- `source_component`
- `capture_mode`
- `capture_gap`
- `evidence_ref`
- `upstream_request_id`
- `decision_kind`
- `decision_result`
- `decision_reason`
- `actor_type`
- `actor_ref`
- `session_id`

Why:

- UniRouter is controlled and can become the reference implementation for the evidence contract.
- These fields are structural; they unlock future multi-step and cross-system runs.

### new-api

Integration level:

- Medium-fidelity DB/API import first.
- Optional callback/native patch later if the ecosystem accepts it.

Useful current fields from verified snapshot:

- `request_id`
- `upstream_request_id`
- `user_id`
- `token_id`
- `token_name`
- `model_name`
- `channel`
- `channel_name`
- `group`
- `ip`
- `quota`
- `prompt_tokens`
- `completion_tokens`
- `use_time`
- `is_stream`
- `other`

Why:

- `upstream_request_id` and `other` make it meaningfully better than one-api for X-Insight.
- ClickHouse log support makes large historical import more plausible.

Risk:

- `other` is flexible but not a governed evidence schema.
- No native decision record or capture-gap semantics observed.

### one-api

Integration level:

- Low-fidelity DB/API import.
- Optional fork/patch if users need high fidelity.

Useful current fields from verified snapshot:

- `request_id`
- `user_id`
- `username`
- `token_name`
- `model_name`
- `channel`
- `quota`
- `prompt_tokens`
- `completion_tokens`
- `elapsed_time`
- `is_stream`
- `system_prompt_reset`

Why:

- Good enough for request/cost visibility and historical import.
- Not enough for deep execution governance.

Risk:

- No first-class upstream request ID observed in the log schema.
- No generic governed metadata field observed in logs.

### LiteLLM

Integration level:

- High-value public adapter.
- Support both spend-log import and callback/OTel route.

Useful current fields from verified snapshot:

- `request_id`
- `call_type`
- hashed `api_key`
- `spend`
- `total_tokens`
- `prompt_tokens`
- `completion_tokens`
- `startTime`
- `endTime`
- `request_duration_ms`
- `completionStartTime`
- `model`
- `model_id`
- `model_group`
- `custom_llm_provider`
- `api_base`
- `user`
- `metadata`
- `cache_hit`
- `cache_key`
- `request_tags`
- `team_id`
- `organization_id`
- `end_user`
- `requester_ip_address`
- `messages`
- `response`
- `session_id`
- `status`
- `mcp_namespaced_tool_name`
- `agent_id`
- `proxy_server_request`

Why:

- It is the strongest third-party gateway target because it already has callbacks, spend logs, teams, metadata, OTel-style integrations, MCP/agent-adjacent fields, and broad recognition.

Risk:

- Prompt/response storage fields create privacy obligations and should not be required.
- LiteLLM's schema should map into X-Insight, not define X-Insight.

## Adapter Package Shape

Recommended components:

- `collector`: X-Insight ingestion API, validates evidence events.
- `adapter-sdk`: shared library for mapping, validation, retries, cursoring, and redaction.
- `adapter-unirouter`: native reference adapter.
- `adapter-litellm`: callback plus spend-log import.
- `adapter-newapi`: DB/API import.
- `adapter-oneapi`: DB/API import.
- `adapter-otel`: OTLP/OpenInference bridge.

Each adapter should include:

- adapter manifest;
- source version compatibility;
- field mapping table;
- fidelity level;
- capture-gap matrix;
- privacy behavior;
- conformance tests;
- sample payloads;
- failure-mode documentation.

## Adapter Manifest

Every adapter should declare:

- `adapter_id`
- `source_system`
- `source_versions`
- `integration_modes`
- `supported_objects`
- `required_permissions`
- `stores_prompt`
- `stores_response`
- `supports_backfill`
- `supports_realtime`
- `supports_decision_records`
- `supports_upstream_request_id`
- `supports_span_correlation`
- `known_capture_gaps`

Why:

- This prevents false claims of "full support".
- It lets users understand what evidence quality they get from each integration.

## Implementation Sequence

### Step 1: Evidence Contract And Adapter SDK

Deliver:

- evidence event validator;
- mapping helpers;
- redaction helpers;
- capture-gap helpers;
- test fixtures.

Reason:

- Without this, every adapter will invent its own semantics.

### Step 2: UniRouter Native Adapter

Deliver:

- native event emitter;
- async delivery;
- local retry buffer;
- historical import from trace/call/audit tables;
- first execution timeline.

Reason:

- Fastest path to real data and end-to-end UI.

### Step 3: LiteLLM Adapter

Deliver:

- spend-log importer;
- real callback endpoint;
- metadata mapping;
- OTel bridge if practical.

Reason:

- Strongest public signal that X-Insight is not UniRouter-only.

### Step 4: new-api And one-api Importers

Deliver:

- DB/API import adapters;
- mapping matrices;
- explicit low/medium fidelity labels.

Reason:

- These are practical open-source gateway ecosystems, but not all fields are equally rich.

### Step 5: OTel / MCP / SDK Expansion

Deliver:

- OTLP ingestion;
- MCP tool-event mapping;
- SDK instrumentation for side effects and business resources.

Reason:

- Required for real agent governance beyond gateway visibility.

## Recurring Lessons From Similar Product Discussions

I cannot use or disclose other users' private conversations. The following are generalized lessons from recurring product and architecture patterns in this problem space:

- Teams often start with a beautiful trace graph, then discover the data cannot prove what the graph claims.
- "Observability" and "governance" are frequently confused; governance needs decisions, identity, evidence, and accountability, not only logs.
- Gateway-only products overclaim agent visibility because they cannot see downstream tool side effects.
- Raw prompt/response storage is attractive for demos but becomes a privacy, retention, and procurement problem.
- Ontology-first projects tend to overbuild before there is reliable event evidence.
- Adapter ecosystems fail when compatibility is described as binary; evidence fidelity must be explicit.
- Enforcement should usually come after observe/evidence/policy simulation unless the enforcement point is fully controlled.
- Public credibility improves when the product supports at least one mainstream ecosystem path, not only a first-party integration.

## Recommendation

For X-Insight:

1. Build UniRouter native integration first for product construction.
2. Build LiteLLM adapter early for public credibility.
3. Build new-api and one-api importers as compatibility paths.
4. Make OTel/OpenInference a strategic ingestion path.
5. Treat SDK/MCP/business-system instrumentation as the route to true agent-side-effect governance.

This sequencing preserves speed without trapping the product inside UniRouter.
