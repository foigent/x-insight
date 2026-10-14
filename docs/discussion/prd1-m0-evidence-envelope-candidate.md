# PRD1 M0 Evidence Envelope Candidate

Status: M0 engineering contract candidate, not final PRD
Created: 2026-07-08

## Purpose

This document narrows the M0 evidence envelope required before X-Insight starts adapter implementation.

It is derived from:

- `Evidence Contract v0.1 Working Draft`
- `PRD1 Minimal Data Model`
- `PRD1 Candidate Scope`
- `PRD1 Engineering Milestones Candidate`
- `UniRouter / X-Insight Integration Boundary`
- `Gateway Adapter Mapping Matrix v0.1`

The goal is to define the minimum event contract that lets UniRouter and LiteLLM evidence enter the same product model without making UniRouter, LiteLLM, OpenTelemetry, or any future runtime the schema owner.

Related implementation detail:

- [PRD1 M0 Schema And Indexes Candidate](./prd1-m0-schema-indexes-candidate.md)

## Core Judgment

PRD1 should not try to capture every possible AI/Agent fact.

PRD1 must capture enough structure to answer:

1. What happened?
2. Where did this fact come from?
3. Is it directly observed, derived, suppressed, or missing?
4. Who or what caused the action?
5. What capability was invoked?
6. Was there a decision, usage, cost, outcome, or payload policy state?
7. What would need to be instrumented to prove the missing parts?

If the envelope cannot answer these, X-Insight collapses into logs. If the envelope tries to answer everything, PRD1 collapses under schema complexity.

## M0 Envelope Shape

The PRD1 event record should be a stable top-level envelope plus JSON field groups.

Recommended table:

- `evidence_events`

Top-level fields should be stable, queryable, and used for ingestion, filtering, correlation, and audit.

Field groups should remain JSONB in PRD1:

- `correlation`
- `identity`
- `capability`
- `decision`
- `usage`
- `outcome`
- `payload_policy`
- `field_provenance`
- `raw_source_excerpt`

Implementation note:

- `raw_source_excerpt` is a JSONB column on `evidence_events`, but it should follow the same disciplined field-group rules as the other JSONB groups.

This is not arbitrary JSON storage. Each field group has a defined minimum shape.

## Top-Level Required Fields

| Field | Required | Type | Reason |
| --- | --- | --- | --- |
| `id` | Yes | UUID | X-Insight event ID. |
| `schema_version` | Yes | string | Allows contract evolution. |
| `source_system_id` | Yes | UUID | Links event to a registered source. |
| `adapter_manifest_id` | Yes | UUID | Links event to adapter/version/fidelity declaration. |
| `source_type` | Yes | enum/string | `unirouter`, `litellm`, `one-api`, `new-api`, `otel`, `sdk`, etc. |
| `source_version` | Optional but expected | string | Needed for adapter compatibility and bug diagnosis. |
| `source_event_id` | Yes when available | string | Source row ID, callback ID, span ID, or log ID. |
| `source_event_type` | Optional but expected | string | Original source event type, if different from normalized `event_type`. |
| `source_ref` | Yes | string | Stable human/debug pointer to source evidence. |
| `idempotency_key` | Yes | string | Prevents duplicate events across retries/imports. |
| `import_cursor` | Optional | string | Adapter cursor/checkpoint reference. |
| `event_type` | Yes | string | Normalized event type. |
| `stage` | Yes | enum/string | `gateway`, `routing`, `provider`, `billing`, `usage`, `policy`, `payload`, `gap`, `simulation`, etc. |
| `severity` | Yes | enum | `info`, `warning`, `error`. |
| `capture_mode` | Yes | enum | How this event was captured: `native`, `callback`, `db_import`, `api_import`, `otel`, `sdk`, `mcp`, `derived`, `manual`. |
| `occurred_at` | Yes when source has it | timestamp | When the source event happened. |
| `observed_at` | Optional | timestamp | When the source observed it, if different. |
| `ingested_at` | Yes | timestamp | When X-Insight ingested it. |
| `request_id` | Nullable projection | string | Duplicated from `correlation.request_id` for first timeline queries. |
| `trace_id` | Nullable projection | string | Duplicated from `correlation.trace_id` when available. |
| `span_id` | Nullable projection | string | Duplicated from `correlation.span_id` when available. |
| `org_ref` | Nullable projection | string | Duplicated from identity for filtering across source types. |
| `project_ref` | Nullable projection | string | Duplicated from identity for filtering across source types. |
| `payload_capture_mode` | Yes | enum | Duplicated from `payload_policy.payload_capture_mode` for filtering and UI. |
| `event_hash` | Yes | string | X-Insight-generated hash of canonical normalized event content, used for duplicate conflict detection. |
| `source_event_hash` | Optional | string | Source-provided integrity hash if available. |
| `raw_source_excerpt` | Optional | JSONB | Small allowlisted source metadata only, never raw prompt/response by default. |

Required for PRD1 implementation:

- `id`
- `schema_version`
- `source_system_id`
- `adapter_manifest_id`
- `source_type`
- `source_ref`
- `idempotency_key`
- `event_type`
- `stage`
- `severity`
- `capture_mode`
- `payload_capture_mode`
- `event_hash`
- `ingested_at`

Required when source provides it:

- `source_version`
- `source_event_id`
- `source_event_type`
- `import_cursor`
- `occurred_at`
- `observed_at`
- `request_id`
- `trace_id`
- `span_id`
- `org_ref`
- `project_ref`
- `source_event_hash`

Why this split:

- strict enough to support correctness and deduplication;
- flexible enough to ingest current UniRouter tables and LiteLLM callback/spend data without blocking on perfect source fields.

## Top-Level Projection Rule

Canonical values should remain inside field groups, but PRD1 should denormalize a few values into top-level columns for query and UI performance.

Required projections:

- `request_id`
- `trace_id`
- `span_id`
- `org_ref`
- `project_ref`
- `payload_capture_mode`

Why:

- request search and timeline view need simple filters;
- payload state is a primary UI and policy-simulation dimension;
- Postgres JSONB can handle this, but PRD1 should avoid making every core query depend on deep JSON expressions;
- these projections do not make the model rigid because canonical group values still live in JSONB.

Projection rules:

- adapter writes the JSONB group and projected column together;
- validator checks consistency when both exist;
- if a projected value is unavailable, keep it null and represent the missing claim through provenance/gaps when it matters;
- projections must not include secrets or raw payload content.

## Event Type And Stage Naming

`stage` should be a controlled enum.

M0 stage values:

- `gateway`
- `identity`
- `routing`
- `provider`
- `billing`
- `usage`
- `logging`
- `policy`
- `payload`
- `gap`
- `simulation`
- `external`
- `unknown`

`event_type` should be normalized but extensible.

Recommended pattern:

```text
<stage>.<subject>.<result_or_action>
```

Examples:

```text
gateway.request.accepted
gateway.request.rejected
identity.actor.resolved
routing.route.selected
routing.route.unavailable
provider.request.attempted
provider.request.failed
provider.response.accepted
provider.response.rejected
billing.freeze.succeeded
billing.freeze.rejected
billing.settle.succeeded
billing.settle.failed
usage.tokens.recorded
logging.call_log.recorded
payload.policy.applied
gap.capture.detected
simulation.policy.matched
simulation.policy.not_matched
```

Rules:

- normalized `event_type` should not be source-specific if a common meaning exists;
- source-specific event names should be preserved in `source_event_type`;
- new adapter-specific normalized event types are allowed only if declared in the adapter manifest;
- UI copy should not expose raw event names as the primary human explanation.

## Source Reference Format

`source_ref` should be stable, readable, and adapter-specific.

Recommended format:

```text
<source_type>:<source_instance>:<entity_or_table>:<source_event_id>
```

Examples:

```text
unirouter:prod-main:request_trace_events:12345
unirouter:prod-main:call_logs:98765
litellm:team-gateway:callback:req-abc
litellm:team-gateway:spend_logs:row-456
otel:collector-a:span:trace/span
```

Rules:

- It must not contain secrets.
- It must not contain raw prompt/response.
- It must be stable enough for evidence pack review.
- It does not need to be globally meaningful outside the source and adapter.

## Idempotency Key

Every adapter must generate an `idempotency_key`.

Recommended shape:

```text
<adapter_id>:<source_system_id>:<source_ref>:<event_type>:<occurred_at_or_source_sequence>
```

If a source has a durable event ID:

- use source event ID.

If a source only has a row ID:

- use table/entity plus row ID.

If a source only has a callback without durable ID:

- hash stable source request ID, event type, and timestamp bucket.

M0 decision:

- duplicate prevention is a PRD1 must-have.
- exactly-once delivery is not a PRD1 requirement.
- at-least-once ingestion plus idempotent storage is the right boundary.

## Import Cursor

`import_cursor` is adapter-owned state.

It may be:

- source row ID;
- timestamp plus row ID;
- callback delivery ID;
- spend-log offset;
- OTel span checkpoint;
- adapter-specific opaque token.

Rules:

- X-Insight stores cursor state per source plus adapter.
- Evidence events may copy the cursor used for debugability.
- The cursor is not the business identity of the event.
- Replaying an import range must not duplicate events because idempotency still applies.

## Field Groups

### Correlation

Purpose:

Connect gateway calls, retries, sessions, traces, and future agent runs.

Minimum shape:

| Field | Required | Notes |
| --- | --- | --- |
| `request_id` | Yes when source has it | Current primary correlation key. |
| `upstream_request_id` | Optional | Provider-side request correlation. |
| `trace_id` | Reserved | Needed for future graph/span model and F3 sources. |
| `span_id` | Reserved | Needed for parent/child execution model. |
| `parent_span_id` | Reserved | Needed for multi-step agent execution. |
| `session_id` | Optional | Useful for LiteLLM and future agents. |
| `run_id` | Optional | Future SDK/MCP/business workflow. |
| `parent_event_id` | Optional | Fallback chain when spans are missing. |

M0 rule:

- Require `request_id` when available.
- Reserve trace/span fields from day one.
- Do not force all sources into OTel semantics.

### Identity

Purpose:

Prevent anonymous execution evidence.

Minimum shape:

| Field | Required | Notes |
| --- | --- | --- |
| `org_id` | Yes when source has it | Enterprise boundary. |
| `project_id` | Yes when source has it | Workload/application boundary. |
| `actor_type` | Optional but reserved | `human`, `service`, `agent`, `api_key`, `system`, `unknown`. |
| `actor_ref` | Optional but reserved | Stable actor pointer. |
| `owner_ref` | Optional | Owner of key, app, agent, or workload. |
| `api_key_ref` | Optional | Redacted/stable key reference, never secret. |
| `key_type` | Optional | Shared/personal/service/etc. |
| `team_ref` | Optional | LiteLLM and enterprise team mapping. |
| `end_user_ref` | Optional | App-level end user, when safely provided. |

M0 rule:

- Missing identity is not just null. It should create or contribute to a capture gap if the UI or simulation needs it.

### Capability

Purpose:

Represent what capability was invoked without assuming every capability is a model call.

Minimum shape:

| Field | Required | Notes |
| --- | --- | --- |
| `capability_type` | Yes | `model_call`, `tool_call`, `data_access`, `api_call`, `policy_check`, `unknown`. |
| `requested_model` | Optional | User/client requested model. |
| `resolved_model` | Optional | Gateway/provider-resolved model. |
| `provider` | Optional | Provider name. |
| `channel` | Optional | Gateway/provider channel. |
| `model_group` | Optional | LiteLLM model group. |
| `operation` | Optional | Future tool/API/data operation. |
| `resource_ref` | Optional | Future data/API/resource pointer. |

M0 rule:

- For PRD1, model calls are the main capability.
- The field group must not make future tools/data/API side effects impossible.

### Decision

Purpose:

Represent source decisions and simulation decisions without pretending that all adapters can emit governance decisions.

Minimum shape:

| Field | Required | Notes |
| --- | --- | --- |
| `decision_kind` | Required for decision events | `admission`, `auth`, `quota`, `budget`, `routing`, `billing`, `access`, `provider`, `payload_capture`, `trace_sampling`, `retry`, `failover`, `policy`, `policy_simulation`, `unknown`. |
| `decision_result` | Required for decision events | `allowed`, `rejected`, `selected`, `failed`, `skipped`, `simulated_match`, `simulated_no_match`, `unknown`. |
| `decision_reason` | Optional | Human-readable reason or normalized code. |
| `policy_ref` | Optional | Source or simulated policy. |
| `rule_ref` | Optional | Source or simulated rule. |
| `decision_inputs_ref` | Optional | Evidence fields used. |

M0 rule:

- If UniRouter currently emits `billing.freeze.rejected`, X-Insight may derive a decision field, but provenance must say `derived`.
- If UniRouter emits `auth.model_not_allowed`, X-Insight preserves it as `source_event_type` and normalizes it to `auth.model.rejected` with direct auth decision provenance.
- Do not claim source-native policy decision unless emitted by the source.

### Usage

Purpose:

Support cost, token, latency, and basic operational accountability.

Minimum shape:

| Field | Required | Notes |
| --- | --- | --- |
| `prompt_tokens` | Optional | If source provides it. |
| `completion_tokens` | Optional | If source provides it. |
| `total_tokens` | Optional | If source provides it. |
| `cost_cents` | Optional | UniRouter style. |
| `spend` | Optional | LiteLLM style; normalize carefully. |
| `currency` | Optional | Required if monetary values leave source-native units. |
| `latency_ms` | Optional | End-to-end or stage-specific. |
| `ttft_ms` | Optional | Streaming first-token latency. |
| `usage_source` | Optional | Source of usage values. |
| `usage_confidence` | Optional | Source confidence or estimate. |

M0 rule:

- Keep source-native usage in allowlisted metadata if normalization is ambiguous.
- Do not invent cost if source does not provide enough evidence.

### Outcome

Purpose:

Represent whether the event succeeded, failed, or remains unknown.

Minimum shape:

| Field | Required | Notes |
| --- | --- | --- |
| `status` | Optional | `success`, `failure`, `partial`, `unknown`. |
| `status_code` | Optional | HTTP/source status. |
| `error_code` | Optional | Normalized or source error code. |
| `error_message_ref` | Optional | Redacted/reference only if sensitive. |
| `failure_kind` | Optional | Timeout, provider error, billing rejection, policy rejection, etc. |

M0 rule:

- Error details must be sanitized.
- Provider error payloads must not become raw payload storage by accident.

### Payload Policy

Purpose:

Show what happened to prompt/response/tool-result content without storing raw content by default.

Minimum shape:

| Field | Required | Notes |
| --- | --- | --- |
| `payload_capture_mode` | Yes | `none`, `metadata_only`, `hash_only`, `redacted`, `reference_only`, `full_content`. |
| `prompt_capture_mode` | Optional | If different from response/tool mode. |
| `response_capture_mode` | Optional | If different from prompt/tool mode. |
| `payload_ref` | Optional | Reference to payload object/customer storage. |
| `payload_hash` | Optional | Hash/HMAC when enabled. |
| `redaction_state` | Optional | `not_applicable`, `not_redacted`, `redacted`, `failed`, `unknown`. |
| `retention_policy_ref` | Optional | Required if payload ref/content exists. |

M0 rule:

- `payload_capture_mode` is required even when it is `none`.
- Raw prompt/response stays off by default.
- Blank payload UI is wrong; it should say suppressed, metadata-only, redacted, reference-only, or available.

### Field Provenance

Purpose:

Make UI claims honest.

PRD1 should use field-group provenance, not per-scalar provenance.

Allowed values:

- `direct`
- `derived`
- `declared`
- `suppressed`
- `gap`
- `not_applicable`
- `unknown`

Minimum keys:

```json
{
  "correlation": "direct",
  "identity": "direct",
  "capability": "direct",
  "decision": "derived",
  "usage": "direct",
  "outcome": "direct",
  "payload_policy": "declared",
  "business_outcome": "gap",
  "tool_side_effect": "gap"
}
```

M0 rule:

- Every timeline node must have field-group provenance.
- If the UI displays a claim, the event or derived projection must say whether that claim is direct, derived, suppressed, or gap.
- Store canonical provenance on `evidence_events`.
- Timeline nodes may copy provenance for rendering, but they must be treated as projections, not source of truth.

## Capture Gap Object

Capture gaps should be stored separately from `evidence_events` and linked back to the event, request, or timeline.

Recommended table:

- `capture_gaps`

Minimum fields:

| Field | Required | Notes |
| --- | --- | --- |
| `id` | Yes | Gap ID. |
| `event_id` | Optional | Related event. |
| `request_id` | Optional | Related request when event is missing. |
| `source_system_id` | Yes | Source where gap was detected. |
| `adapter_manifest_id` | Yes | Adapter that declared/detected it. |
| `gap_type` | Yes | Enumerated gap. |
| `affected_claim` | Yes | Human-readable claim that cannot be proven. |
| `gap_reason` | Yes | Why the evidence is missing. |
| `severity` | Yes | `info`, `warning`, `critical`. |
| `recommended_instrumentation` | Optional | SDK/MCP/OTel/business adapter/gateway update. |
| `created_at` | Yes | Gap creation time. |

Minimum gap types:

- `not_instrumented`
- `not_available_from_source`
- `suppressed_by_payload_policy`
- `redacted`
- `sampled_out`
- `permission_denied`
- `retention_expired`
- `provider_not_returned`
- `inferred_only`
- `business_outcome_unknown`
- `tool_side_effect_unknown`
- `identity_unknown`
- `decision_not_emitted`

M0 rule:

- A missing fact that affects a human claim must be a capture gap, not an empty field.

## Adapter Manifest M0 Shape

Recommended table:

- `adapter_manifests`

Minimum fields:

| Field | Required | Notes |
| --- | --- | --- |
| `id` | Yes | Manifest ID. |
| `adapter_id` | Yes | Stable adapter name. |
| `adapter_version` | Yes | Adapter version. |
| `source_type` | Yes | UniRouter, LiteLLM, etc. |
| `supported_source_versions` | Yes | Range/list or exact checked version. |
| `fidelity_level` | Yes | F0/F1/F2/F3. |
| `ingestion_modes` | Yes | `db_import`, `api_import`, `callback`, `otel`, etc. |
| `direct_field_groups` | Yes | Field groups source can emit directly. |
| `derived_field_groups` | Yes | Field groups adapter derives. |
| `known_capture_gaps` | Yes | Known missing evidence. |
| `payload_behavior` | Yes | Prompt/response/tool payload defaults and supported modes. |
| `idempotency_strategy` | Yes | How event keys are generated. |
| `cursor_strategy` | Yes if importer | How import progress is tracked. |
| `created_at` | Yes | Manifest creation time. |

Fidelity labels:

| Level | Meaning |
| --- | --- |
| F0 | Manual or fixture-only. Not production evidence. |
| F1 | Historical import from logs/tables. Useful but limited. |
| F2 | Callback/webhook/OTel/near-real-time adapter. Better timing, still not full source semantics. |
| F3 | Source-native evidence emitter with explicit trace/span/decision/capture/evidence fields. |

M0 rule:

- Adapters should not be described as simply "supported."
- They should declare what they can prove and what remains a gap.

## Raw Source Excerpt Rules

`raw_source_excerpt` is only for small, allowlisted source metadata.

Allowed examples:

- provider name;
- channel name;
- status code;
- latency;
- token counts;
- source error code;
- safe request IDs;
- source event type;
- usage confidence.

Disallowed by default:

- prompt text;
- response text;
- tool result bodies;
- retrieved document chunks;
- API keys;
- authorization headers;
- customer secrets;
- full provider error bodies if they may contain prompt/response.

M0 rule:

- If a field may be sensitive, put it behind payload policy or omit it.
- Do not rely on UI hiding as the privacy boundary.

## UniRouter M0 Mapping

Current UniRouter can start as F1/F2 through X-Insight-side DB/API import.

Initial mapping:

| X-Insight field/group | UniRouter current source |
| --- | --- |
| `source_type` | `unirouter` |
| `source_event_id` | `request_trace_events.id` when importing trace event; `call_logs.id` for call-log event. |
| `source_ref` | `unirouter:<instance>:request_trace_events:<id>` or `unirouter:<instance>:call_logs:<id>`. |
| `event_type` | `request_trace_events.event_type`. |
| `stage` | `request_trace_events.stage`. |
| `severity` | `request_trace_events.severity`. |
| `capture_mode` | `db_import` or `api_import`. |
| `correlation.request_id` | `request_trace_events.request_id` / `call_logs.request_id`. |
| `identity.org_id` | `org_id`. |
| `identity.project_id` | `project_id`. |
| `identity.api_key_ref` | `api_key_id` as reference, never secret. |
| `identity.owner_ref` | `owner_user_id` when present. |
| `identity.key_type` | `key_type`. |
| `capability.requested_model` | `requested_model`. |
| `capability.resolved_model` | `model`. |
| `capability.provider` | `provider`. |
| `capability.channel` | `channel`. |
| `usage` | call log token/cost/latency/usage fields. |
| `outcome.status_code` | trace/call-log status fields. |
| `payload_policy.payload_capture_mode` | `metadata_only` or `none` until UniRouter emits payload state. |
| `field_provenance.decision` | `derived` for current routing/billing/provider event-type inference. |

Current UniRouter gaps:

- no native X-Insight emitter;
- no cursor bulk export API;
- no trace/span IDs;
- no explicit decision fields;
- no event-level payload policy fields;
- no evidence refs/event hash.

M0 implication:

- Gate A can start without UniRouter changes.
- Gate B/F3 cannot be claimed until UniRouter emits or exposes structural fields.

## LiteLLM M0 Mapping

LiteLLM should start as F1/F2 through callback and spend-log import.

Initial mapping:

| X-Insight field/group | LiteLLM source |
| --- | --- |
| `source_type` | `litellm` |
| `source_event_id` | callback request ID/delivery ID or spend-log row ID. |
| `source_ref` | `litellm:<instance>:callback:<id>` or `litellm:<instance>:spend_logs:<id>`. |
| `capture_mode` | `callback` or `db_import`/`api_import` for spend logs. |
| `correlation.request_id` | LiteLLM request ID when available. |
| `correlation.session_id` | LiteLLM session ID when available. |
| `identity.team_ref` | LiteLLM team ID/name when available. |
| `identity.end_user_ref` | End-user ID when safely provided. |
| `capability.requested_model` | model requested/model group. |
| `capability.provider` | provider if available. |
| `usage` | tokens, spend, latency, cache fields. |
| `payload_policy.payload_capture_mode` | `none` or `metadata_only` by default. |
| `field_provenance.decision` | `gap` unless emitted in metadata/callback. |

Expected LiteLLM gaps:

- routing/billing/policy decisions may not be directly visible;
- downstream tool side effects are unknown unless instrumented;
- business outcome is unknown;
- prompt/response content exists in some LiteLLM paths but must not be imported by default.

## M0 Validation Rules

An event is valid when:

- required top-level fields are present;
- `source_system_id` exists;
- `adapter_manifest_id` exists;
- `idempotency_key` is non-empty;
- `source_ref` is non-empty;
- `event_type`, `stage`, `severity`, and `capture_mode` are valid;
- top-level `payload_capture_mode` is present;
- `payload_policy.payload_capture_mode` is present;
- top-level `payload_capture_mode` matches `payload_policy.payload_capture_mode`;
- `field_provenance` exists for displayed field groups;
- no disallowed sensitive keys appear in `raw_source_excerpt`.

An event should be rejected or quarantined when:

- source reference is missing;
- idempotency key is missing;
- adapter manifest is unknown;
- raw prompt/response appears in raw metadata without payload policy;
- source type is unsupported and no manifest exists;
- event cannot be tied to any source identity.

## Quarantine Behavior

PRD1 should include a minimal quarantine path for bad or risky incoming events.

Recommended table:

- `ingestion_quarantine`

Minimum fields:

| Field | Required | Notes |
| --- | --- | --- |
| `id` | Yes | Quarantine record ID. |
| `source_system_id` | Optional | Present if source is known. |
| `adapter_manifest_id` | Optional | Present if adapter is known. |
| `reason` | Yes | Validation or safety reason. |
| `severity` | Yes | `warning`, `error`, `critical`. |
| `source_ref` | Optional | Source pointer if safe. |
| `idempotency_key` | Optional | If generated before failure. |
| `safe_excerpt` | Optional | Sanitized metadata only. |
| `created_at` | Yes | Quarantine time. |

Quarantine reasons:

- `missing_source_ref`
- `missing_idempotency_key`
- `unknown_adapter_manifest`
- `unsupported_source_type`
- `disallowed_payload_content`
- `secret_detected`
- `invalid_enum`
- `schema_validation_failed`
- `source_identity_missing`

M0 rule:

- Do not silently drop invalid events unless they are obvious duplicates.
- Do not store unsafe raw payload in quarantine.
- Quarantine is an operator/debug mechanism, not an evidence event.

## What Is Reserved But Not Required In M0

Reserved fields:

- `trace_id`
- `span_id`
- `parent_span_id`
- `run_id`
- `resource_ref`
- `decision_inputs_ref`
- `payload_hash`
- `retention_policy_ref`
- `source_event_hash`

Why reserve:

- Future agent/tool/business execution needs them.
- Adding names later is easy; retroactively giving them consistent semantics is hard.

Why not require:

- Current UniRouter and LiteLLM sources cannot always provide them.
- Requiring them would block the first evidence loop.

## What M0 Must Not Include

M0 should not include:

- full graph schema;
- full ontology model;
- raw prompt/response storage;
- OPA/Rego policy engine;
- live enforcement;
- ClickHouse dependency;
- OTel backend;
- native UniRouter outbox;
- one-api/new-api production adapters.

## PRD1 Blocking Decisions After This Envelope

This document narrows the field contract enough for implementation planning, but a few M0 decisions still need implementation-level detail:

1. Exact JSON Schema files for each field group.
2. Exact Go validation library and JSON Schema draft selection.
3. Exact migration SQL generated from the schema/index candidates.
4. Exact canonical JSON hashing implementation.
5. Exact UI terminology and first wireframe/component breakdown for execution search and detail.

These are engineering details, not strategic product questions.

## Current Recommendation

Use this envelope as the M0 implementation contract.

Do not start with a generic trace schema. Do not start with UniRouter table shape. Do not start with OTel span shape.

Start with an evidence event that can carry:

- source identity;
- source reference;
- adapter fidelity;
- idempotency;
- correlation;
- identity;
- capability;
- decision;
- usage;
- outcome;
- payload policy;
- field provenance;
- capture gaps.

That is the smallest defensible core for X-Insight PRD1.
