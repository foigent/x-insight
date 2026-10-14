# Gateway Adapter Mapping Matrix v0.1

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

Status: working matrix, not PRD
Created: 2026-07-08

## Purpose

This document maps the `Evidence Contract v0.1` direction to real integration surfaces across UniRouter, LiteLLM, new-api, one-api, and OpenTelemetry/OpenInference.

The goal is not to claim full support. The goal is to make evidence quality explicit:

- what a source can produce directly;
- what X-Insight can derive with reasonable confidence;
- what must be represented as a capture gap;
- what should be deferred to SDK, MCP, OTel/OpenInference, or business-system instrumentation.

This matrix should be used before PRD1 to prevent two mistakes:

- designing a data model from one gateway's schema;
- presenting a human-readable execution graph that the collected evidence cannot actually prove.

## Source Baseline

Local and remote source checks used for this matrix:

| Source | Checked state | Notes |
| --- | --- | --- |
| UniRouter | Local repo `PRIVATE_HOME/Desktop/projects/UniRouter`, deep-inspected at HEAD `59721d7d4284101ad8f2e9a1a6abcd1d7f1a8ba5`; light recheck at `f0d34e1` on 2026-07-08 | Current controlled reference source. Relevant request trace/call log/admin trace surfaces still present at light recheck. |
| one-api | Local and remote HEAD `8df4a2670b98266bd287c698243fff327d9748cf` | Remote HEAD rechecked on 2026-07-08. |
| new-api | Remote HEAD `6ce7305cd36f16506fb6a2c3c524a5a318539ba7` | `origin/main:model/log.go` checked after fetch. |
| LiteLLM | Local snapshot `ed66ee312cba4c5e07cd51da9b80842a5c8abc76`; remote HEAD `b8248a21d2ac0c102d49c0db5702a48cdf661bb9` | Full fetch was slow and interrupted; implementation must rerun field extraction against current HEAD. |
| OpenTelemetry GenAI | OpenTelemetry GenAI semantic-conventions repository checked on 2026-07-08 | Treat as standards bridge, not product schema owner. |
| OpenInference | OpenInference semantic conventions checked on 2026-07-08 | Treat as standards bridge and import/export mapping target. |

Important caveat:

This matrix is a planning artifact. Before implementation, adapters should generate source-specific field manifests from the exact source version being supported.

## Fidelity Levels

X-Insight should not describe integrations as simply "supported" or "not supported."

| Level | Name | Meaning | PRD1 interpretation |
| --- | --- | --- | --- |
| F0 | Capture gap only | Source cannot provide the field or event with useful confidence. | Show gap explicitly. |
| F1 | Historical import | Source provides logs, spend rows, or admin APIs after the fact. | Useful for evidence packs, timelines, cost, and basic accountability. |
| F2 | Real-time adapter | Source provides callback, webhook, event export, CDC, or OTel spans close to execution time. | Useful for near-real-time evidence and policy simulation, still not full governance. |
| F3 | Native evidence emitter | Source emits X-Insight evidence semantics directly, including capture mode, decision records, and gap states. | Reference implementation quality. |

Current likely fidelity:

| Source | Current fidelity | PRD1 target | Why |
| --- | --- | --- | --- |
| UniRouter | F2 now, F3 target | F3 | It already has request trace events and is controlled, but needs explicit trace/span, capture mode, decision, and evidence fields. |
| LiteLLM | F1 via spend logs, F2 via callback/OTel | F2 | It has rich spend, metadata, callback, OTel, team, user, session, MCP/tool-adjacent, and agent fields, but not X-Insight governance semantics. |
| new-api | F1 medium | F1 | It has request and upstream request correlation plus `other`, but not native trace, decision, or capture-gap semantics. |
| one-api | F1 low | F1 | It has useful request/cost/token logs, but no observed upstream request ID, trace model, or metadata extension comparable to new-api. |
| OTel/OpenInference | F2 bridge | Reserved, optionally partial PRD1 | Strong for spans and ecosystem neutrality; incomplete for enterprise governance unless identity, decision, and policy attributes are added. |

## Adapter Summary

### UniRouter

Recommended integration:

- native event emitter;
- historical import from `request_trace_events`, call logs, and audit logs;
- later optional export to OTel/OpenInference.

Directly available today:

- `request_id`;
- org/project/API key/owner/key type context;
- request trace events: `event_type`, `stage`, `severity`, `model`, `requested_model`, `provider`, `channel`, `status_code`, `latency_ms`, `metadata`, `created_at`;
- call-log usage, cost, status, latency, streaming, usage source, usage confidence, and TTFT where present;
- audit log context and project usage RBAC;
- metadata-only privacy posture.

Needs UniRouter additions for F3:

- `trace_id`, `span_id`, `parent_span_id`;
- `schema_version`, `source_component`, `capture_mode`;
- `capture_gap`, `evidence_ref`, `source_event_hash`;
- explicit `decision_kind`, `decision_result`, `decision_reason`;
- explicit `actor_type`, `actor_ref`, `owner_ref`, and optional `on_behalf_of`;
- optional `session_id`, `agent_id`, `mcp_tool`, and business intent fields when available.

Judgment:

UniRouter is the right first deep integration for product construction, but it must not become the schema owner. The X-Insight schema should treat UniRouter as the highest-fidelity reference emitter, not as the universal shape of enterprise AI execution.

### LiteLLM

Recommended integration:

- spend-log import from `LiteLLM_SpendLogs`;
- real X-Insight callback endpoint;
- optional OTel/OpenInference bridge;
- metadata mapping for trace/session/business context if users pass it through LiteLLM.

Directly useful fields from the local schema snapshot:

- `request_id`, `call_type`;
- hashed `api_key`;
- `spend`, `total_tokens`, `prompt_tokens`, `completion_tokens`;
- `startTime`, `endTime`, `request_duration_ms`, `completionStartTime`;
- `model`, `model_id`, `model_group`, `custom_llm_provider`, `api_base`;
- `user`, `team_id`, `organization_id`, `end_user`, `requester_ip_address`;
- `metadata`, `request_tags`;
- `cache_hit`, `cache_key`;
- `session_id`, `mcp_namespaced_tool_name`, `agent_id`;
- `messages`, `response`, `proxy_server_request`.

Privacy judgment:

`messages`, `response`, and `proxy_server_request` are valuable for opt-in enterprise evidence, but they should not be required or enabled by default in PRD1. X-Insight should import payload references or redacted metadata first, and only store payloads under explicit on-prem controls.

Gap:

LiteLLM can expose rich AI gateway context, but it does not automatically prove downstream tool execution, business side effects, approval chains, or governance decisions unless those are emitted through metadata, callbacks, OTel spans, SDK events, or MCP/business-system adapters.

Judgment:

LiteLLM should be the first public credibility adapter, but not a dependency. X-Insight should maintain a real LiteLLM test/demo path while letting customers connect from whatever gateway they already use.

### new-api

Recommended integration:

- DB/API import adapter first;
- optional native patch or plugin only if there is user demand.

Directly useful current fields:

- `request_id`;
- `upstream_request_id`;
- `user_id`, `username`;
- `token_id`, `token_name`;
- `model_name`;
- `channel`, `channel_name`;
- `group`, `ip`;
- `quota`, `prompt_tokens`, `completion_tokens`;
- `use_time`, `is_stream`;
- `other`;
- ClickHouse log table support for `request_id`, `upstream_request_id`, and `other`.

Gap:

- no observed native run/span model;
- no explicit decision record;
- no governed capture-gap semantics;
- `other` is useful but not a stable evidence contract.

Judgment:

new-api is meaningfully better than one-api for X-Insight because of `upstream_request_id` and `other`. It should be labeled medium-fidelity historical import, not full governance.

### one-api

Recommended integration:

- low-fidelity DB/API import;
- optional fork/patch only if a customer or community contributor needs higher fidelity.

Directly useful current fields:

- `request_id`;
- `user_id`, `username`;
- `token_name`;
- `model_name`;
- `channel`;
- `quota`, `prompt_tokens`, `completion_tokens`;
- `elapsed_time`;
- `is_stream`;
- `system_prompt_reset`;
- `content` and log `type`.

Gap:

- no observed first-class upstream request ID in the log schema;
- no observed governed metadata JSON field comparable to new-api `other`;
- no native trace/span model;
- no decision record;
- no capture-gap semantics.

Judgment:

one-api should be supported for compatibility and historical visibility, but it should not influence the X-Insight evidence contract. For one-api users, X-Insight must be honest that it can show request/cost/accountability evidence, not full AI/Agent execution governance.

### OpenTelemetry / OpenInference

Recommended integration:

- reserve an OTel/OpenInference adapter path in PRD1 architecture;
- do not force full OTel backend scope into the first implementation unless it is needed for the public demo;
- map OTel/OpenInference spans into X-Insight evidence events, but keep X-Insight governance fields as first-class concepts.

Directly useful:

- `trace_id`, `span_id`, `parent_span_id`;
- span kind/name/status/timing;
- GenAI model/provider/request/response/usage attributes when instrumented;
- tool-call attributes where instrumentation emits them;
- OpenInference-style prompt, response, retrieval, tool, and embedding attributes when enabled.

Gap:

- enterprise identity is not guaranteed;
- business intent is not guaranteed;
- policy decision semantics are not guaranteed;
- payload capture may be too sensitive for default storage;
- standards are useful transport/semantic layers, not a complete governance product.

Judgment:

OTel/OpenInference should be a strategic interoperability path. It should prevent gateway lock-in, but it should not replace the X-Insight Evidence Contract.

## Evidence Object Mapping

Legend:

- Direct: source observes and emits the fact.
- Derived: X-Insight can infer or normalize it from source data.
- Gap: missing or unprovable unless another source is added.
- Optional: available only if configured, passed in metadata, or enabled by the customer.

| Evidence object | UniRouter | LiteLLM | new-api | one-api | OTel/OpenInference | PRD1 judgment |
| --- | --- | --- | --- | --- | --- | --- |
| Evidence event envelope | Direct partial: request trace rows. Needs schema/capture fields. | Derived from callback/spend row. | Derived from log row. | Derived from log row. | Direct span envelope, mapped to event. | Required. Store source ref and adapter version. |
| Execution correlation | Direct `request_id`; needs trace/span. | Direct `request_id`, optional `session_id`, possible metadata trace IDs. | Direct `request_id` and `upstream_request_id`. | Direct `request_id`. | Direct trace/span/parent. | Required. `request_id` required; trace/span optional but reserved. |
| Identity context | Direct org/project/API key/owner/key type. | Direct user/team/org/end user/key hash. | Direct user/token/group/IP. | Direct user/token. | Optional attributes only. | Required, but field strength must be visible. |
| Capability invocation | Direct requested/resolved model, provider, channel, route. | Direct model/provider/model group/API base/call type. | Direct model/channel. | Direct model/channel. | Direct if GenAI attrs exist. | Required for timeline and evidence pack. |
| Decision record | Partial via billing/routing/provider event types; needs explicit decision fields. | Mostly gap unless metadata/callback emits decisions. | Gap, except coarse error/log type. | Gap, except coarse error/log type. | Optional custom attrs only. | PRD1 should support lightweight policy simulation, not claim source-native decisions unless emitted. |
| Usage/cost/latency | Direct tokens/cost/status/latency/usage source. | Direct tokens/spend/duration/cache/status. | Direct quota/tokens/use time/stream. | Direct quota/tokens/elapsed/stream. | Direct tokens/duration if instrumented; cost often missing. | Required. Good PRD1 anchor across all gateways. |
| Evidence reference | Partial via source rows and real upstream evidence harness; needs `evidence_ref`. | Direct row/callback source ref; payload refs optional. | Direct log row ref. | Direct log row ref. | Direct span ref. | Required as source reference; integrity can start simple. |
| Payload policy | Strong default: no prompt/response plaintext. | Payload fields exist; default should be no storage in X-Insight. | Treat content cautiously; do not depend on raw payload. | Treat content cautiously; do not depend on raw payload. | Payload attrs may exist; default should be no storage. | Required. Default off; on-prem opt-in later. |
| Outcome | Direct gateway/provider/billing/logging outcome. Business outcome gap. | Direct status and cost outcome. Business outcome gap. | Coarse outcome from log type/content/status-like data. | Coarse outcome from log type/content. | Direct span status; business outcome optional. | Required, but distinguish gateway outcome from business outcome. |
| Capture gap | Needs explicit event field, but X-Insight can derive current gaps. | Derived from adapter manifest and missing events. | Derived from adapter manifest and missing fields. | Derived from adapter manifest and missing fields. | Derived from missing identity/decision/business fields. | Required. This is a product differentiator, not just an error state. |

## PRD1 Minimal Common Evidence Shape

The matrix suggests PRD1 should not wait for a perfect schema, but it should require a small common shape:

| Field group | Minimum fields | Why |
| --- | --- | --- |
| Source | `source_system`, `source_version`, `adapter_id`, `adapter_version`, `source_ref` | Makes evidence auditable and adapter-specific. |
| Time | `occurred_at`, `observed_at`, optional `ingested_at` | Separates source time from ingestion time. |
| Correlation | `request_id`, optional `trace_id`, optional `span_id`, optional `parent_span_id`, optional `session_id` | Lets request timelines work now and execution graphs evolve later. |
| Identity | `actor_ref`, `actor_type`, optional `org_ref`, `project_ref`, `team_ref`, `api_key_ref`, `owner_ref` | Prevents traces from becoming anonymous telemetry. |
| Capability | `capability_type`, `capability_name`, optional `provider`, optional `channel`, optional `requested_model`, optional `resolved_model` | Supports model calls now and tool/business capabilities later. |
| Event semantics | `event_type`, `stage`, `severity`, `capture_mode`, `confidence` | Lets UI show what happened without pretending all fields are equal. |
| Outcome | `status`, `status_code`, `latency_ms`, optional `error_type`, optional `error_message_ref` | Supports readable timeline and troubleshooting. |
| Usage | `input_tokens`, `output_tokens`, `total_tokens`, `cost_amount`, `cost_unit`, `usage_source` | Common anchor across all gateways. |
| Decision | optional `decision_kind`, `decision_result`, `decision_reason`, `policy_ref` | Needed for policy simulation and future enforcement. |
| Payload policy | `payload_capture_mode`, `payload_ref`, `redaction_state`, `retention_policy_ref` | Avoids accidental prompt/response storage. |
| Gaps | `capture_gaps[]` | Makes missing evidence explicit and product-visible. |

## Direct, Derived, And Gap Rules

PRD1 should enforce these rules in code and UI:

1. A source-observed field is stored as `direct`.
2. A normalized or inferred field is stored as `derived`, with a derivation reason.
3. A missing field that affects a human claim is stored as a `capture_gap`.
4. The UI should label source strength instead of showing all nodes as equally proven.
5. Raw payload storage must never be required for the common evidence shape.

Examples:

- UniRouter `billing.freeze.rejected` can become a direct gateway decision event after UniRouter emits explicit decision fields.
- new-api `upstream_request_id` is direct upstream correlation, but any policy decision derived from it is not direct.
- one-api request logs can support cost/accountability views, but not full execution-chain claims.
- LiteLLM `session_id` and `agent_id` can link related activity, but tool side effects are still gaps unless tool execution is instrumented.
- OTel spans can produce a good execution graph, but identity and governance semantics remain gaps unless attributes are present.

## Adapter Manifest Requirements

Every adapter should ship a manifest with at least:

- `adapter_id`
- `source_system`
- `source_version_min`
- `source_version_max`
- `validated_source_commit`
- `integration_modes`
- `fidelity_level`
- `supported_objects`
- `direct_fields`
- `derived_fields`
- `capture_gaps`
- `stores_prompt`
- `stores_response`
- `supports_backfill`
- `supports_realtime`
- `supports_trace_span`
- `supports_upstream_request_id`
- `supports_decision_records`
- `supports_payload_references`
- `privacy_notes`

This is not bureaucratic metadata. It is how X-Insight prevents false claims about evidence quality.

## Product Judgments From The Matrix

1. UniRouter should drive the first deep build because it can be upgraded to F3.
2. LiteLLM should be the first external credibility adapter because it can reach F2 without forcing customers to replace their gateway.
3. new-api is worth supporting earlier than one-api if prioritization is needed, because `upstream_request_id` and `other` create better mapping potential.
4. one-api support should be honest low-fidelity compatibility.
5. OTel/OpenInference should be reserved as a strategic bridge, but PRD1 should not become an OTel backend project unless the first public demo requires it.
6. The first human-readable UI should show a request/evidence timeline first, not a full agent graph. A full graph is only justified when trace/span/tool/business events exist.
7. Capture-gap visibility is a differentiator. It turns "we cannot see that" from weakness into governance truth.

## Open Questions Before PRD1

These are the remaining questions that should be answered after this matrix, not before it:

1. Should PRD1 implement OTel/OpenInference ingestion, or only reserve schema/export compatibility?
   - Current recommendation: reserve the path and implement only if it materially strengthens the LiteLLM/public demo.
2. Should new-api and one-api import use database access, admin APIs, or both?
   - Current recommendation: define both in the adapter contract, but implement the fastest reliable path per source.
3. How strong should event integrity be in PRD1?
   - Current recommendation: source refs and event hashes are enough first; signed evidence chains can wait until there is customer demand.
4. Should payload storage appear in PRD1 at all?
   - Current recommendation: no default payload storage. On-prem optional payload references can be designed, but plaintext storage belongs behind enterprise controls.
5. Should the first demo be a policy violation scenario or an evidence-discovery scenario?
   - Current recommendation: evidence-discovery first, with lightweight historical policy simulation. Strong enforcement comes later.

## Source Links

- OpenTelemetry GenAI semantic conventions repository: https://github.com/open-telemetry/semantic-conventions-genai
- OpenTelemetry GenAI moved page: https://opentelemetry.io/docs/specs/semconv/gen-ai/
- OpenInference semantic conventions: https://github.com/Arize-ai/openinference/blob/main/spec/semantic_conventions.md
- OpenInference specification: https://arize-ai.github.io/openinference/spec/
