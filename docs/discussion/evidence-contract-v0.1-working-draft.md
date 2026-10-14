# X-Insight Evidence Contract v0.1 Working Draft

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

Status: discussion draft, not PRD
Created: 2026-07-07

## Purpose

This document defines a candidate evidence contract for X-Insight.

The contract is intentionally not tied to one gateway schema. UniRouter, one-api, new-api, LiteLLM, OpenTelemetry, MCP, and future SDKs should map into this contract with different fidelity levels.

The goal is to make X-Insight capable of saying:

- what is directly observed;
- what is declared by an upstream system;
- what is inferred by X-Insight;
- what is missing and must be represented as a capture gap.

## Design Position

The contract should not be a copy of UniRouter, one-api, new-api, or LiteLLM log tables.

Reason:

- gateway logs are built for operations, billing, and troubleshooting;
- X-Insight needs evidence, identity, decision, outcome, and governance semantics;
- future agent/tool/business-system evidence will not fit cleanly into model-gateway-only tables;
- seamless integration should mean loss-aware adapters, not reducing the product to the lowest common denominator.

## Core Objects

### 1. Evidence Event

This is the atomic record X-Insight ingests.

Required fields:

- `event_id`
- `schema_version`
- `source_system`
- `source_instance`
- `source_component`
- `event_type`
- `event_time`
- `observed_at`
- `severity`
- `status`
- `stage`
- `capture_mode`
- `capture_gap`
- `confidence`
- `raw_ref`

Recommended meanings:

- `source_system`: `unirouter`, `one-api`, `new-api`, `litellm`, `otel`, `mcp`, `sdk`, `business_system`.
- `capture_mode`: `native`, `adapter`, `callback`, `db_import`, `api_import`, `otel`, `sdk`, `manual`.
- `capture_gap`: `none`, `not_instrumented`, `not_available_from_source`, `redacted`, `sampled_out`, `permission_denied`, `retention_expired`, `provider_not_returned`, `inferred_only`.
- `confidence`: `observed`, `declared`, `derived`, `inferred`, `corrected`.

Why:

- It prevents X-Insight from overstating visibility.
- It lets weak sources like one-api logs and strong sources like native UniRouter events coexist.

### 2. Execution Correlation

This connects individual gateway/model calls into runs, sessions, and spans.

Required fields:

- `run_id`
- `trace_id`
- `span_id`
- `parent_span_id`
- `request_id`
- `upstream_request_id`

Optional but important:

- `session_id`
- `conversation_id`
- `parent_request_id`
- `idempotency_key`
- `batch_id`

Why:

- `request_id` is enough for one model call.
- AI agent execution needs run/span semantics across multiple model calls, tools, retries, approvals, and business-system actions.

### 3. Identity Context

This answers who or what caused the action.

Required fields:

- `org_id`
- `project_id`
- `actor_type`
- `actor_ref`
- `api_key_ref`
- `key_type`
- `owner_ref`

Optional but important:

- `user_id`
- `team_id`
- `organization_id`
- `service_account_id`
- `on_behalf_of`
- `auth_method`
- `requester_ip_address`
- `user_agent`

Why:

- Enterprise governance needs attribution beyond a token string.
- API keys, service keys, personal keys, and on-behalf-of flows create different governance responsibilities.

### 4. Capability Invocation

This answers what capability was used.

Required fields:

- `capability_type`
- `protocol`
- `endpoint`
- `call_type`
- `requested_model`
- `resolved_model`
- `provider`
- `channel`
- `model_group`
- `model_id`
- `is_stream`

Optional but important:

- `api_base`
- `region`
- `deployment_id`
- `adapter`
- `mcp_server`
- `mcp_tool`
- `agent_id`
- `tool_name`
- `tool_namespace`

Why:

- Model gateways know models and providers.
- Agent governance also needs tools, MCP servers, browser actions, and future non-model capabilities.

### 5. Decision Record

This answers what decision was made, by which rule or component.

Required fields:

- `decision_kind`
- `decision_result`
- `decision_reason`
- `decision_component`

Optional but important:

- `policy_id`
- `policy_version`
- `rule_id`
- `evaluated_subject`
- `evaluated_resource`
- `evaluated_action`
- `candidate_count`
- `selected_candidate`
- `fallback_index`
- `budget_state`
- `quota_state`

Candidate `decision_kind` values:

- `auth`
- `model_access`
- `budget_precheck`
- `billing_freeze`
- `routing`
- `provider_selection`
- `provider_rejection`
- `safety`
- `rate_limit`
- `data_access`
- `human_approval`
- `tool_permission`
- `policy_simulation`

Why:

- A trace tells what happened.
- A decision record explains why the system allowed, blocked, routed, retried, downgraded, or escalated.

### 6. Usage And Cost

This answers what was consumed and charged.

Required fields:

- `prompt_tokens`
- `completion_tokens`
- `total_tokens`
- `cost`
- `cost_unit`
- `usage_source`
- `usage_confidence`

Optional but important:

- `estimated_tokens`
- `cached_tokens`
- `cache_hit`
- `cache_key`
- `ttft_ms`
- `latency_ms`
- `request_duration_ms`
- `completion_start_time`
- `spend`
- `quota`
- `currency`

Why:

- Cost is one of the few places where model-gateway products already have strong data.
- Usage confidence matters because provider-reported, locally estimated, and recovered partial usage are not equal evidence.

### 7. Evidence Reference

This answers where the proof can be inspected.

Required fields:

- `evidence_ref`
- `evidence_type`
- `storage_backend`
- `storage_key`
- `hash`
- `redaction_profile`
- `retention_class`

Optional but important:

- `encryption_key_ref`
- `access_policy`
- `audit_on_read`
- `content_availability`
- `export_job_id`
- `legal_hold`

Why:

- X-Insight should not store all raw content by default.
- Enterprises should be able to opt into controlled evidence storage, retention, audit-on-read, and export.

### 8. Payload Policy

This records privacy and content-handling state.

Required fields:

- `prompt_store_mode`
- `response_store_mode`
- `redaction_state`
- `payload_hash_available`

Optional but important:

- `pii_scan_state`
- `secret_scan_state`
- `sensitive_label`
- `customer_controlled_key`
- `read_audit_required`

Why:

- Enterprise on-prem deployments should let customers choose storage policy.
- The product must still distinguish not stored by policy from not captured because the source cannot provide it.

### 9. Outcome

This answers what happened as a result.

Required fields:

- `outcome_status`
- `status_code`
- `error_code`
- `failure_kind`
- `final_state`

Optional but important:

- `retry_count`
- `provider_status`
- `side_effect_status`
- `business_result_ref`
- `settlement_status`
- `log_persisted`

Why:

- Model call success is not the same as business task success.
- PRD1 should avoid pretending gateway success proves external side effects.

### 10. Capture Gap

This is first-class, not a footnote.

Required fields:

- `gap_type`
- `missing_source`
- `reason`
- `impact`
- `required_instrumentation`

Optional but important:

- `recommended_adapter`
- `owner`
- `priority`
- `can_be_fixed_by_gateway`
- `can_be_fixed_by_sdk`
- `can_be_fixed_by_business_system`

Why:

- X-Insight's credibility depends on explicitly showing visibility boundaries.
- Capture gaps are also future expansion opportunities.

## Gateway Compatibility Review

Source check date: 2026-07-07

Local source snapshots:

- UniRouter: `PRIVATE_HOME/Desktop/projects/UniRouter`
- one-api: `songquanpeng/one-api` HEAD `8df4a2670b98266bd287c698243fff327d9748cf`
- new-api: `QuantumNous/new-api` HEAD `8bc4bf1d6b1fd7d117100edadcf4257d3a4eb479`
- LiteLLM: `BerriAI/litellm` HEAD `ed66ee312cba4c5e07cd51da9b80842a5c8abc76`

Freshness note:

- On 2026-07-08, remote HEAD check showed one-api still at `8df4a2670b98266bd287c698243fff327d9748cf`.
- On 2026-07-08, new-api remote HEAD had moved to `6ce7305cd36f16506fb6a2c3c524a5a318539ba7`.
- On 2026-07-08, LiteLLM remote HEAD had moved to `a43f128a7444ce4687f07d3f90b2fe5cae86e447`.
- Later on 2026-07-08, LiteLLM remote HEAD had moved again to `b8248a21d2ac0c102d49c0db5702a48cdf661bb9`; see `Gateway Adapter Mapping Matrix v0.1` for the latest planning baseline.
- Therefore the compatibility review below is a verified 2026-07-07 snapshot, not a permanent implementation contract. Re-run adapter field extraction before implementation.

### UniRouter

Current strength:

- Has `request_trace_events`.
- Has request-level event stages.
- Has org/project/key/owner/key type context.
- Has requested model, resolved model, provider, channel, status, latency, token/cost metadata.
- Has metadata allowlisting.
- Has privacy policy for prompt/response storage.
- Has request trace API and permission checks.

Missing for X-Insight native fidelity:

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
- `policy_id`
- `policy_version`
- `rule_id`
- `actor_type`
- `actor_ref`
- `on_behalf_of`
- `session_id`
- `mcp_tool`
- `agent_id`

Recommendation:

- UniRouter should become the native high-fidelity integration.
- Add only structural evidence fields first; do not start by storing raw prompt/response.

### one-api

Observed log fields:

- `user_id`
- `created_at`
- `type`
- `content`
- `username`
- `token_name`
- `model_name`
- `quota`
- `prompt_tokens`
- `completion_tokens`
- `channel`
- `request_id`
- `elapsed_time`
- `is_stream`
- `system_prompt_reset`

Current strength:

- Has request id.
- Has user/token/model/channel/quota/token usage.
- Simple to ingest as historical operational logs.

Current gap:

- No first-class upstream request id observed in the log schema.
- No generic JSON `other` metadata field on logs observed.
- No native trace/span model observed.
- No decision taxonomy.
- No capture gap/evidence reference semantics.

Recommendation:

- Treat one-api as low-fidelity `db_import` / `api_import`.
- Do not make one-api's log schema shape the X-Insight contract.
- If deeper integration is needed, use a fork/plugin/patch that emits X-Insight evidence events.

### new-api

Observed log fields:

- `user_id`
- `created_at`
- `type`
- `content`
- `username`
- `token_name`
- `model_name`
- `quota`
- `prompt_tokens`
- `completion_tokens`
- `use_time`
- `is_stream`
- `channel`
- `channel_name`
- `token_id`
- `group`
- `ip`
- `request_id`
- `upstream_request_id`
- `other`

Observed storage behavior:

- ClickHouse log table includes `request_id` and `upstream_request_id`.
- Log APIs support filtering by `request_id` and `upstream_request_id`.
- Upstream `X-Oneapi-Request-Id` is captured into `X-Upstream-Request-Id` context and then logged.

Current strength:

- Better than one-api for X-Insight because it has upstream request correlation.
- `other` JSON gives an extension surface.
- ClickHouse log support makes large log ingestion more plausible.

Current gap:

- No native run/span model observed.
- No capture mode/capture gap semantics.
- No first-class decision record.
- No evidence reference model.
- `other` is flexible but not a governed contract.

Recommendation:

- Treat new-api as medium-fidelity integration.
- Prefer a native adapter that reads logs plus `other`.
- If the project accepts plugins or custom patches, add X-Insight event emission rather than relying only on SQL extraction.

### LiteLLM

Observed spend log fields:

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

Observed integration style:

- Supports spend tracking for keys, users, and teams.
- Supports callbacks such as Langfuse, OpenTelemetry, S3, generic API, LangSmith, DataDog, and others.
- Generates a unique call id for request tracking.
- Allows metadata such as tags and trace identifiers to be passed in request bodies.

Current strength:

- Richer than one-api/new-api in callback and observability ecosystem.
- Has fields closer to agents/MCP, such as `mcp_namespaced_tool_name`, `agent_id`, `session_id`.
- Likely best third-party target for a high-fidelity adapter.

Current gap:

- Spend logs are still not the same as governance evidence.
- Decision records and capture gaps are not first-class X-Insight semantics.
- Prompt/response storage fields exist, but using them creates privacy and retention obligations.
- DB schema and callbacks are LiteLLM-specific; they should map into X-Insight, not become the X-Insight contract.

Recommendation:

- Support LiteLLM through two paths:
  - historical import from `LiteLLM_SpendLogs`;
  - real-time X-Insight callback / OpenTelemetry adapter.
- Do not require prompt/response storage for PRD1.

## Integration Fidelity Levels

### Level 0: Historical Import

Source:

- log tables;
- spend tables;
- admin APIs.

Works for:

- one-api;
- new-api;
- LiteLLM spend logs;
- UniRouter call logs / trace events.

Limit:

- weak causality;
- no real-time policy;
- many fields are missing or inferred.

### Level 1: Real-Time Adapter

Source:

- webhook;
- callback;
- event export;
- CDC;
- polling plus cursor.

Works for:

- LiteLLM callbacks;
- UniRouter native events;
- possible new-api adapter;
- possible one-api fork/patch.

Limit:

- still mostly gateway-visible activity.

### Level 2: Native Evidence SDK / Instrumentation

Source:

- X-Insight SDK;
- OpenTelemetry spans;
- MCP instrumentation;
- business-system event emitters;
- agent runtime hooks.

Works for:

- business intent;
- tool calls;
- side effects;
- approvals;
- on-behalf-of chains;
- business resource access.

Limit:

- requires application/runtime adoption.

## Direct, Derived, And Future Sources

### Produced Directly By UniRouter

- request received/accepted;
- org/project/key/user/owner attribution;
- requested model;
- resolved model;
- selected provider/channel;
- routing failure and provider failure;
- billing precheck/freeze/settle;
- token usage;
- cost;
- status code;
- latency;
- call log persisted;
- audit log context;
- metadata-only real upstream evidence.

Why:

- UniRouter is the gateway and billing boundary.
- These facts are observed inside its control plane.

### Derived By X-Insight

- normalized timeline;
- execution graph;
- actor/capability/action/resource taxonomy;
- decision summaries;
- evidence pack;
- human-readable explanation;
- risk labels;
- missing evidence/capture gap report;
- anomaly indicators;
- policy simulation candidates.

Why:

- These are semantic products assembled from lower-level events.
- They should be marked as `derived` or `inferred` unless directly emitted by a source.

### Requires Future SDK / MCP / Business-System Instrumentation

- declared business intent;
- final business task result;
- tool call arguments and results;
- MCP server-side execution;
- browser/computer-use actions;
- database writes in application code;
- SaaS API side effects;
- human approval workflow;
- on-behalf-of chain;
- business resource identity;
- customer-managed raw payload evidence references.

Why:

- Model gateways cannot see what happens after a model response is used by an application or agent runtime.
- X-Insight must represent this as a capture gap until instrumented.

## Recommendation Before PRD1

Before selecting the first public demo, produce two concrete artifacts:

1. `Evidence Contract v0.1`
2. `Gateway Adapter Mapping Matrix v0.1`

Then PRD1 can define:

- UniRouter native ingestion;
- one-api/new-api/LiteLLM compatibility import;
- human-readable execution timeline;
- evidence pack;
- capture-gap report;
- no raw prompt/response storage by default.

This keeps the product from becoming a trace dashboard while still making the first version buildable.
