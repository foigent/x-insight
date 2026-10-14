# PRD1 M0 Schema And Indexes Candidate

Status: M0 engineering detail candidate, not final PRD
Created: 2026-07-08

## Purpose

This document turns the `PRD1 M0 Evidence Envelope Candidate` into implementable M0 engineering defaults:

- JSON Schema file boundaries;
- enum registry;
- initial Postgres tables and indexes;
- idempotency conflict handling;
- sensitive-key detection for `raw_source_excerpt` and quarantine.

Timeline UI information architecture is covered separately in [PRD1 M2 Timeline UI Information Architecture Candidate](./prd1-m2-timeline-ui-information-architecture-candidate.md).

## Core Judgment

M0 should be strict at the envelope boundary and flexible inside controlled field groups.

The implementation should reject three failure modes:

1. Arbitrary JSON blobs that no adapter can reliably validate.
2. Over-normalized tables that force every new source to require migrations.
3. Silent acceptance of unsafe payload or duplicate-conflicting evidence.

The correct PRD1 posture is:

- stable top-level columns for query, source reference, idempotency, and safety;
- disciplined JSON Schema validation for field groups;
- adapter manifests that declare fidelity and gaps;
- quarantine for invalid or risky events;
- Postgres indexes only for the first workflows, not generic analytics.

## Schema Package Layout

Recommended repo path once implementation starts:

```text
schemas/m0/
  common/
    enums.schema.json
    provenance.schema.json
  evidence-event.schema.json
  adapter-manifest.schema.json
  source-system.schema.json
  capture-gap.schema.json
  ingestion-quarantine.schema.json
  field-groups/
    correlation.schema.json
    identity.schema.json
    capability.schema.json
    decision.schema.json
    usage.schema.json
    outcome.schema.json
    payload-policy.schema.json
    raw-source-excerpt.schema.json
```

M0 recommendation:

- Use JSON Schema Draft 2020-12 or a Go library that fully supports the selected schema draft.
- Keep schemas versioned with the evidence schema version.
- Do not embed source-specific schemas directly into the evidence event schema.
- Source-specific fields must live in field-group `extensions` or in adapter-specific fixtures.

## Strictness Rules

Top-level evidence event:

- no unknown top-level fields;
- all required M0 fields must be present;
- projected columns must match canonical JSON field groups when both exist;
- source refs and idempotency keys must be non-empty;
- payload capture mode must be present even when set to `none`;
- raw source excerpts must pass sensitive-key detection.

Field groups:

- known keys are validated;
- source-specific values go under `extensions`;
- `extensions` keys should be namespaced, for example `unirouter.route_index` or `litellm.cache_hit`;
- `extensions` must still pass sensitive-key detection;
- field groups should have size limits.

Suggested size limits:

| Object | M0 limit | Reason |
| --- | --- | --- |
| `raw_source_excerpt` | 16 KB serialized | Prevent payload creep. |
| Each field group | 32 KB serialized | Enough for metadata, not content storage. |
| Whole event body | 128 KB serialized | M0 is evidence metadata, not blob storage. |
| Quarantine safe excerpt | 16 KB serialized | Debuggable but not a payload sink. |

If these limits are too small for a real source, that is a signal to create payload references or source-specific evidence refs, not to enlarge event rows casually.

## Enum Registry

### Source Type

M0 values:

- `unirouter`
- `litellm`
- `one-api`
- `new-api`
- `otel`
- `openinference`
- `sdk`
- `mcp`
- `business_system`
- `manual`
- `unknown`

PRD1 implementation should only ship UniRouter and LiteLLM adapters as active paths. Other source types are reserved to avoid future rename churn.

### Fidelity Level

- `F0`: manual or fixture-only, not production evidence.
- `F1`: historical import from logs/tables.
- `F2`: callback/webhook/OTel/near-real-time adapter.
- `F3`: source-native evidence emitter with explicit trace/span/decision/capture/evidence fields.

### Stage

M0 values:

- `gateway`
- `auth`
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

### Event Type

M0 should have a seeded registry, but allow adapter-manifest-declared additions.

Seed values:

- `gateway.request.accepted`
- `gateway.request.rejected`
- `auth.model.rejected`
- `identity.actor.resolved`
- `identity.actor.unresolved`
- `routing.route.selected`
- `routing.route.unavailable`
- `provider.request.attempted`
- `provider.request.failed`
- `provider.response.accepted`
- `provider.response.rejected`
- `provider.response.parse_failed`
- `billing.freeze.succeeded`
- `billing.freeze.rejected`
- `billing.freeze.failed`
- `billing.settle.succeeded`
- `billing.settle.failed`
- `usage.tokens.recorded`
- `usage.cost.recorded`
- `usage.latency.recorded`
- `logging.call_log.recorded`
- `payload.policy.applied`
- `gap.capture.detected`
- `simulation.policy.matched`
- `simulation.policy.not_matched`
- `external.callback.received`
- `source.record.imported`

Rules:

- normalized event type uses `<stage>.<subject>.<result_or_action>`;
- raw source event type is stored separately as `source_event_type`;
- adapter-specific normalized event types must be declared in the adapter manifest;
- UI should translate event types into human-readable labels rather than exposing raw strings as the main copy.

### Severity

- `info`
- `warning`
- `error`

Do not add `critical` to evidence event severity in M0. Use capture-gap or quarantine severity for critical operator handling. This keeps event severity aligned with current UniRouter trace severity.

### Capture Mode

- `native`
- `callback`
- `db_import`
- `api_import`
- `otel`
- `openinference`
- `sdk`
- `mcp`
- `derived`
- `manual`

### Payload Capture Mode

- `none`
- `metadata_only`
- `hash_only`
- `redacted`
- `reference_only`
- `full_content`

M0 default:

- `none` or `metadata_only`.

M0 should not implement full content storage unless a real on-prem demo requires it.

### Provenance State

- `direct`
- `derived`
- `declared`
- `suppressed`
- `gap`
- `not_applicable`
- `unknown`

Rules:

- `direct`: emitted by source or source row.
- `derived`: normalized/inferred by adapter from source facts.
- `declared`: supplied by configuration or manifest, not runtime observation.
- `suppressed`: intentionally unavailable due to policy.
- `gap`: missing and affects a claim.
- `not_applicable`: not meaningful for this event.
- `unknown`: not enough information yet; use sparingly because it is less useful than a capture gap.

### Gap Type

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

### Capability Type

- `model_call`
- `tool_call`
- `data_access`
- `api_call`
- `policy_check`
- `unknown`

M0 mainly uses `model_call`, but the enum must not block agent/tool evolution.

### Decision Kind

- `routing`
- `auth`
- `quota`
- `budget`
- `billing`
- `access`
- `provider`
- `payload_capture`
- `trace_sampling`
- `retry`
- `failover`
- `policy`
- `policy_simulation`
- `unknown`

### Decision Result

- `allowed`
- `rejected`
- `selected`
- `failed`
- `skipped`
- `simulated_match`
- `simulated_no_match`
- `unknown`

### Outcome Status

- `success`
- `failure`
- `partial`
- `unknown`

### Redaction State

- `not_applicable`
- `not_redacted`
- `redacted`
- `failed`
- `unknown`

### Quarantine Reason

- `missing_source_ref`
- `missing_idempotency_key`
- `unknown_adapter_manifest`
- `unsupported_source_type`
- `disallowed_payload_content`
- `secret_detected`
- `invalid_enum`
- `schema_validation_failed`
- `source_identity_missing`
- `idempotency_conflict`
- `source_ref_conflict`
- `size_limit_exceeded`

## JSON Schema Boundaries

### `evidence-event.schema.json`

Required top-level fields:

- `id`
- `schema_version`
- `source_system_id`
- `adapter_manifest_id`
- `source_type`
- `source_ref`
- `idempotency_key`
- `event_hash`
- `event_type`
- `stage`
- `severity`
- `capture_mode`
- `payload_capture_mode`
- `ingested_at`
- `correlation`
- `payload_policy`
- `field_provenance`

Optional but expected when source provides them:

- `source_version`
- `source_event_id`
- `source_event_type`
- `source_event_hash`
- `import_cursor`
- `occurred_at`
- `observed_at`
- `request_id`
- `trace_id`
- `span_id`
- `org_ref`
- `project_ref`
- `raw_source_excerpt`

Validation rules:

- top-level `request_id`, `trace_id`, `span_id`, `org_ref`, `project_ref`, and `payload_capture_mode` must match their canonical field-group values when both are present;
- `payload_policy.payload_capture_mode` must match top-level `payload_capture_mode`;
- `event_hash` is generated by X-Insight from canonical normalized content;
- source-provided hash, if any, should be stored as `source_event_hash`, not confused with X-Insight `event_hash`.

### Field Group Schemas

M0 should require these field groups:

- `correlation`
- `payload_policy`
- `field_provenance`

M0 should allow these groups to be empty objects when not applicable, but validate shape when present:

- `identity`
- `capability`
- `decision`
- `usage`
- `outcome`
- `raw_source_excerpt`

Reason:

This lets a pure gap event or quarantine-related source event stay valid without pretending every event contains full identity/capability/usage.

### `adapter-manifest.schema.json`

Required fields:

- `adapter_id`
- `adapter_version`
- `source_type`
- `supported_source_versions`
- `fidelity_level`
- `ingestion_modes`
- `direct_field_groups`
- `derived_field_groups`
- `known_capture_gaps`
- `payload_behavior`
- `idempotency_strategy`
- `cursor_strategy` when importer
- `declared_event_types`

Rules:

- adapter manifest must declare any event type outside the seeded registry;
- manifest must declare whether prompt/response/tool payload is unavailable, suppressed, reference-only, redacted, or full-content capable;
- manifest must declare known source gaps so UI can distinguish source limitation from adapter failure.

### `capture-gap.schema.json`

Required fields:

- `id`
- `source_system_id`
- `adapter_manifest_id`
- `gap_type`
- `affected_claim`
- `gap_reason`
- `severity`
- `created_at`

Optional:

- `event_id`
- `request_id`
- `recommended_instrumentation`

### `ingestion-quarantine.schema.json`

Required fields:

- `id`
- `reason`
- `severity`
- `created_at`

Optional:

- `source_system_id`
- `adapter_manifest_id`
- `source_ref`
- `idempotency_key`
- `safe_excerpt`

Rules:

- quarantine never stores raw prompt/response/tool-result content;
- quarantine should preserve enough safe context for operator debugging;
- quarantine is not evidence and should not appear as an execution timeline node unless explicitly opened from import diagnostics.

## Initial Postgres Tables

M0 tables:

- `source_systems`
- `adapter_manifests`
- `adapter_import_states`
- `evidence_events`
- `evidence_refs`
- `capture_gaps`
- `payload_policies`
- `ingestion_quarantine`

M0 can defer:

- `payload_objects`
- `payload_read_audits`
- `policy_simulation_runs`
- `policy_simulation_results`
- materialized `timeline_nodes`

Reason:

M0 must support ingestion, validation, dedupe, source refs, capture gaps, and payload policy state. It does not need payload bodies, audit-on-read, simulation storage, or materialized timeline yet.

## Initial Postgres Indexes

### `source_systems`

Recommended:

```sql
UNIQUE (source_type, instance_key)
INDEX (source_type, status)
```

`instance_key` should be a stable deployment/source identifier such as `prod-main`, `local-dev`, or a generated slug. Do not use a secret URL as an instance key.

### `adapter_manifests`

Recommended:

```sql
UNIQUE (adapter_id, adapter_version)
INDEX (source_type, fidelity_level)
```

### `adapter_import_states`

Recommended:

```sql
UNIQUE (source_system_id, adapter_manifest_id, cursor_key)
INDEX (source_system_id, updated_at DESC)
```

Minimum columns:

- `id`
- `source_system_id`
- `adapter_manifest_id`
- `cursor_key`
- `cursor_value`
- `last_imported_at`
- `last_success_at`
- `last_error`
- `updated_at`

### `evidence_events`

Recommended:

```sql
PRIMARY KEY (id)
UNIQUE (source_system_id, adapter_manifest_id, idempotency_key)
INDEX (source_system_id, occurred_at DESC, id DESC)
INDEX (request_id, occurred_at ASC, id ASC) WHERE request_id IS NOT NULL
INDEX (trace_id, span_id) WHERE trace_id IS NOT NULL
INDEX (org_ref, occurred_at DESC) WHERE org_ref IS NOT NULL
INDEX (project_ref, occurred_at DESC) WHERE project_ref IS NOT NULL
INDEX (stage, event_type, occurred_at DESC)
INDEX (payload_capture_mode, occurred_at DESC)
INDEX (severity, occurred_at DESC)
```

Optional after first query tests:

```sql
GIN (field_provenance jsonb_path_ops)
```

Do not add broad GIN indexes over every JSONB field group in M0. They add write cost and can encourage unplanned dashboard queries before the product has real usage patterns.

### `evidence_refs`

Recommended:

```sql
INDEX (event_id)
INDEX (source_system_id, evidence_type)
INDEX (source_ref)
```

### `capture_gaps`

Recommended:

```sql
INDEX (event_id) WHERE event_id IS NOT NULL
INDEX (request_id, created_at DESC) WHERE request_id IS NOT NULL
INDEX (source_system_id, gap_type, created_at DESC)
INDEX (severity, created_at DESC)
```

### `payload_policies`

Recommended:

```sql
INDEX (scope_type, scope_ref, enabled)
INDEX (payload_capture_mode)
```

### `ingestion_quarantine`

Recommended:

```sql
INDEX (source_system_id, created_at DESC) WHERE source_system_id IS NOT NULL
INDEX (adapter_manifest_id, created_at DESC) WHERE adapter_manifest_id IS NOT NULL
INDEX (reason, created_at DESC)
INDEX (severity, created_at DESC)
```

## Duplicate And Conflict Handling

M0 ingestion should be at-least-once with idempotent storage.

Unique key:

```sql
UNIQUE (source_system_id, adapter_manifest_id, idempotency_key)
```

Canonical hash:

- X-Insight must compute `event_hash` from normalized event content.
- Exclude `id`, `ingested_at`, import attempt metadata, and DB timestamps from the canonical hash.
- Include source refs, source event type, event type, stage, capture mode, projections, and field groups after sanitization.
- Use deterministic canonical JSON. JCS-style canonicalization is preferred; exact implementation can be chosen during coding.

Duplicate cases:

| Case | Action |
| --- | --- |
| Same idempotency key, same event hash | No-op. Update import state only. |
| Same idempotency key, different event hash | Keep original event immutable. Write quarantine record with `idempotency_conflict`. |
| Same source ref and event type, different idempotency key | Write quarantine or warning with `source_ref_conflict` unless adapter manifest declares this as valid. |
| Same request ID, different events | Accept. This is normal timeline behavior. |
| Corrected source data | Do not overwrite original event. Emit a future correction event or re-import with a new idempotency key if adapter manifest defines correction semantics. |

Why:

Evidence systems should avoid silent mutation. If X-Insight changes an event after it has been used for a timeline, evidence pack, or simulation, the audit model becomes weak.

## Sensitive-Key Detector

M0 should use a conservative key-based detector for `raw_source_excerpt`, field-group `extensions`, and quarantine `safe_excerpt`.

This is not a full DLP product. It is a hard safety guard against obvious payload and secret leakage.

### Exact Deny Keys

Case-insensitive exact key deny list:

- `authorization`
- `proxy-authorization`
- `cookie`
- `set-cookie`
- `api_key`
- `apikey`
- `x-api-key`
- `access_token`
- `refresh_token`
- `id_token`
- `client_secret`
- `secret`
- `password`
- `passphrase`
- `private_key`
- `credential`
- `credentials`
- `prompt`
- `prompt_text`
- `prompt_content`
- `messages`
- `message_content`
- `response`
- `response_text`
- `response_content`
- `completion`
- `completion_text`
- `input`
- `input_text`
- `output`
- `output_text`
- `request_body`
- `response_body`
- `body`
- `content`
- `tool_result`
- `tool_result_body`
- `retrieved_document`
- `retrieved_documents`
- `document_text`

### Pattern Deny Keys

Case-insensitive patterns:

```text
(^|[_-])(api[_-]?key|token|secret|password|credential|private[_-]?key)([_-]|$)
(^|[_-])(prompt|messages|completion|request[_-]?body|response[_-]?body|tool[_-]?result)([_-]|$)
(^|[_-])(document[_-]?text|retrieved[_-]?document)([_-]|$)
```

Important allowlist exceptions:

- `prompt_tokens`
- `completion_tokens`
- `total_tokens`
- `input_tokens`
- `output_tokens`
- `usage_source`
- `usage_confidence`
- `response_status_code`
- `request_id`

Reason:

Token counters are metadata and should not be blocked just because their key contains `prompt`, `completion`, `input`, or `output`.

### Value Rules

M0 should not attempt semantic DLP over values, but it should enforce:

- maximum string length per raw excerpt value, recommended 512 characters;
- no multiline large bodies in raw excerpt;
- no base64-looking long opaque blobs in raw excerpt;
- no nested object whose key fails deny checks;
- arrays only when scalar and small.

If a value is useful but risky:

- hash it;
- redact it;
- store a payload reference under governed payload policy;
- or create a capture gap.

## M0 Conformance Fixtures

M0 implementation should include these fixture categories:

1. valid UniRouter request trace event;
2. valid UniRouter call-log-derived usage event;
3. valid LiteLLM callback event;
4. valid LiteLLM spend-log event;
5. invalid event missing `source_ref`;
6. invalid event missing `idempotency_key`;
7. invalid event with raw prompt in `raw_source_excerpt`;
8. duplicate same idempotency and same hash;
9. duplicate same idempotency and different hash;
10. event with source-specific extension keys that pass validation.

The conformance runner should report:

- accepted;
- duplicate no-op;
- quarantined;
- rejected;
- validation errors.

## What This Resolves

This document resolves these previous PRD1-blocking questions:

- enum seed registry for M0;
- JSON Schema file boundaries;
- top-level projections for request/timeline filtering;
- initial Postgres indexes;
- duplicate conflict behavior;
- sensitive-key detector baseline;
- minimal quarantine behavior.

Remaining after this document:

- exact JSON Schema file contents during implementation;
- exact Go validation library selection;
- exact migration SQL;
- first wireframe/component implementation details for the M2 timeline UI.

These are now implementation tasks, not strategic product decisions.

## Current Recommendation

Use this document with `PRD1 M0 Evidence Envelope Candidate` as the M0 implementation boundary.

Do not start coding adapters until:

- schemas are created;
- seed enums are implemented;
- evidence event migrations exist;
- validator handles accept/duplicate/quarantine/reject outcomes;
- UniRouter and LiteLLM conformance fixtures pass.
