# UniRouter / X-Insight Integration Boundary

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

Status: integration boundary analysis, not PRD
Created: 2026-07-08
UniRouter repo inspected: `PRIVATE_HOME/Desktop/projects/UniRouter`
UniRouter HEAD inspected: `59721d7d4284101ad8f2e9a1a6abcd1d7f1a8ba5`
UniRouter light recheck: `f0d34e1` on 2026-07-08; relevant request trace/call log/admin trace surfaces still present.

## Purpose

This document answers four questions:

1. What does UniRouter already support for X-Insight integration?
2. What can X-Insight use immediately without UniRouter changes?
3. What should UniRouter add to support high-fidelity PRD1 integration?
4. Where is the product and technical boundary between UniRouter and X-Insight?

## Executive Judgment

UniRouter already has a strong metadata evidence foundation, but it does not yet have a native X-Insight integration.

Current UniRouter can support:

- request-level evidence discovery;
- historical import from database tables;
- per-request trace inspection through existing admin API;
- metadata-only event timelines for gateway/routing/provider/billing/logging.

Current UniRouter does not yet support:

- X-Insight native event emission;
- cursor-based bulk trace export API;
- trace/span IDs;
- explicit decision records;
- capture-gap semantics;
- evidence refs;
- event hashes;
- payload policy integration;
- async outbox delivery to an external evidence collector.

Therefore the recommended path is:

1. Use current UniRouter DB/API for the first evidence discovery loop.
2. Add a thin UniRouter-to-X-Insight adapter on the X-Insight side.
3. Add structural fields to UniRouter trace events.
4. Add a non-blocking UniRouter outbox/exporter for native X-Insight events.

Do not put X-Insight policy simulation, timeline construction, capture-gap reasoning, or evidence-pack UI inside UniRouter.

## What UniRouter Supports Today

### 1. Metadata-Only Request Trace Events

Verified source:

- `PRIVATE_HOME/Desktop/projects/UniRouter/migrations/000017_prd4_request_trace_events.up.sql`
- `PRIVATE_HOME/Desktop/projects/UniRouter/internal/gateway/trace.go`

Current table:

- `id`
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

Current behavior:

- metadata-only;
- deterministic request-level sampling;
- no prompt/response/secrets;
- org/project/key/owner context captured from request context;
- metadata allowlist enforced by `internal/traceutil/metadata.go`.

Current metadata allowlist includes:

- billing and cost: `billing_status`, `charged_cost_cents`, `cost_cents`, `freeze_amount_cents`, `settle_cancelled`;
- usage: `prompt_tokens`, `completion_tokens`, `total_tokens`, `estimated_tokens`, `usage_source`, `usage_confidence`;
- routing: `route_count`, `route_index`, `mapped_model`;
- streaming/performance: `stream`, `is_stream`, `ttft_ms`;
- errors/failures: `error_code`, `failure_kind`.

Judgment:

This is already enough for a strong first X-Insight UniRouter evidence timeline. It is not enough for F3 fidelity without structural fields.

### 2. Gateway Event Coverage

Verified source:

- `PRIVATE_HOME/Desktop/projects/UniRouter/internal/gateway/relay.go`

Current trace events include:

- `gateway.request.accepted`
- `billing.freeze.failed`
- `billing.freeze.rejected`
- `billing.freeze.succeeded`
- `routing.no_route`
- `routing.route_selected`
- `provider.key_decrypt_failed`
- `provider.adapter_unknown`
- `provider.request_build_failed`
- `provider.request_failed`
- `provider.response_rejected`
- `provider.response_accepted`
- `provider.response_parse_failed`
- `billing.settle.failed`
- `billing.settle.succeeded`
- `logging.call_log.recorded`

Judgment:

This is good coverage for gateway execution. It can support the first human-readable timeline.

Known gap:

Some early auth/model-permission failures may happen before trace emission or without explicit decision fields. These should be fixed if X-Insight needs complete decision evidence.

### 3. Call Logs

Verified sources:

- `PRIVATE_HOME/Desktop/projects/UniRouter/migrations/000001_init_schema.up.sql`
- `PRIVATE_HOME/Desktop/projects/UniRouter/migrations/000007_prd_v15_alignment.up.sql`
- `PRIVATE_HOME/Desktop/projects/UniRouter/migrations/000009_prd2_project_foundation.up.sql`
- `PRIVATE_HOME/Desktop/projects/UniRouter/migrations/000012_prd2_token_metering_v2.up.sql`
- `PRIVATE_HOME/Desktop/projects/UniRouter/migrations/000016_prd4_key_owner_usage.up.sql`
- `PRIVATE_HOME/Desktop/projects/UniRouter/internal/gateway/relay.go`

Current call-log evidence includes:

- `request_id`
- `org_id`
- `project_id`
- `api_key_id`
- `owner_user_id`
- `key_type`
- `model`
- `requested_model`
- `provider`
- `channel`
- `prompt_tokens`
- `completion_tokens`
- `total_tokens`
- `cost_cents`
- `latency_ms`
- `ttft_ms`
- `status_code`
- `upstream_status_code`
- `upstream_error`
- `usage_source`
- `usage_confidence`
- `estimated_tokens`
- `is_stream`
- `ip_address`
- `created_at`

Judgment:

This is a strong usage/cost/outcome anchor for X-Insight.

### 4. Admin Trace API

Verified sources:

- `PRIVATE_HOME/Desktop/projects/UniRouter/internal/admin/log_handlers.go`
- `PRIVATE_HOME/Desktop/projects/UniRouter/docs/tech-ref/05-openapi-spec.yaml`

Existing endpoint:

- `GET /api/v1/logs/{id}/trace`

Current behavior:

- reads call log by log ID;
- checks project usage read permission;
- loads trace events by `request_id`, `org_id`, and `project_id`;
- returns ordered request trace events;
- sanitizes metadata again before response.

Judgment:

Good for manual trace inspection and a first API-based demo. Not enough for efficient X-Insight bulk ingestion because it is per call-log ID and not cursor-based.

### 5. Notification/Webhook Foundation

Verified sources:

- `PRIVATE_HOME/Desktop/projects/UniRouter/migrations/000014_prd2_notification_event_foundation.up.sql`
- `PRIVATE_HOME/Desktop/projects/UniRouter/internal/billing/notifications.go`
- `PRIVATE_HOME/Desktop/projects/UniRouter/internal/admin/notification_handlers.go`

Current capability:

- notification events;
- webhook routes;
- delivery state;
- retry due deliveries;
- event type filters.

Judgment:

This should not be used directly as the X-Insight evidence pipe.

Reason:

- it is semantically a notification system;
- it is not built for high-cardinality request evidence;
- it does not carry X-Insight evidence envelope semantics;
- request traces can be much higher volume than notifications.

However, its delivery design is useful as a reference for an X-Insight outbox/exporter.

## What X-Insight Can Use Immediately

### Mode A: Direct Database Import

Supported now:

- yes, if X-Insight can read UniRouter database.

X-Insight can import:

- `request_trace_events`;
- `call_logs`;
- `audit_logs`;
- possibly `notification_events` for later operational context.

Pros:

- no UniRouter code changes;
- fastest evidence discovery loop;
- best for internal development because UniRouter is controlled;
- can reconstruct first timeline.

Cons:

- tight database coupling;
- not ideal as public integration;
- source schema changes can break adapter;
- no real-time delivery semantics;
- not enough for F3 without structural fields.

Recommended use:

- internal first demo and field discovery;
- not the long-term public integration pattern.

### Mode B: Existing API Pull

Supported now:

- partially.

X-Insight can use:

- logs list/detail APIs;
- `GET /api/v1/logs/{id}/trace`.

Pros:

- respects UniRouter RBAC;
- no database credentials;
- good for low-volume demos.

Cons:

- trace API is per log ID;
- no bulk cursor endpoint;
- current log list response does not expose every field X-Insight wants;
- inefficient for continuous ingestion.

Recommended use:

- demo fallback;
- not the primary ingestion path for PRD1 if DB access is available.

### Mode C: Existing Notification Webhook

Supported now:

- technically yes for notification events, not for request evidence.

Recommendation:

- do not use as primary X-Insight integration.

Use only as:

- reference design for delivery retries;
- possible future source for UniRouter operational events.

## What UniRouter Should Add

### Update 1: Structural Trace Fields

Add to `request_trace_events` or a successor event table:

- `trace_id`
- `span_id`
- `parent_span_id`
- `schema_version`
- `source_component`
- `capture_mode`
- `evidence_ref`
- `source_event_hash`

Why:

These fields turn request logs into evidence events. Without them, X-Insight must infer too much.

### Update 2: Explicit Decision Fields

Add:

- `decision_kind`
- `decision_result`
- `decision_reason`
- `policy_ref`
- `rule_ref`

Candidate mappings:

| UniRouter event | decision kind | decision result |
| --- | --- | --- |
| `billing.freeze.rejected` | `billing` | `rejected` |
| `billing.freeze.succeeded` | `billing` | `allowed` |
| `routing.route_selected` | `routing` | `selected` |
| `routing.no_route` | `routing` | `failed` |
| `provider.response_rejected` | `provider` | `rejected` |
| model not allowed | `access` | `rejected` |

Why:

X-Insight policy simulation and evidence UI need to distinguish direct source decisions from derived interpretations.

### Update 3: Actor Fields

Current fields are strong but gateway-shaped:

- `owner_user_id`
- `api_key_id`
- `key_type`
- `org_id`
- `project_id`

Add or expose:

- `actor_type`
- `actor_ref`
- `owner_ref`
- optional `on_behalf_of`

Why:

X-Insight should not have to infer whether a request came from a human key, service key, or future agent identity.

### Update 4: Payload Policy State

Add event-level metadata or fields:

- `payload_capture_mode`
- `prompt_capture_mode`
- `response_capture_mode`
- `payload_ref`
- `redaction_state`

Default:

- raw prompt/response off;
- metadata-only.

Why:

The UI should show `suppressed`, `reference_only`, `redacted`, or `available`, not a blank payload area.

### Update 5: Bulk Export API

Add an API like:

- `GET /api/v1/xinsight/events?since_id=...&limit=...`
- or `GET /api/v1/request-trace-events?cursor=...`

Minimum behavior:

- cursor-based;
- scoped by org/project;
- RBAC protected;
- includes call-log join fields needed by X-Insight;
- returns source version and schema version;
- never includes prompt/response by default.

Why:

This avoids requiring X-Insight to have direct DB credentials.

### Update 6: Native X-Insight Outbox / Exporter

Add an async, non-blocking delivery path:

- `xinsight_event_outbox` table or durable local queue;
- background worker;
- batch delivery to X-Insight collector;
- retry/backoff;
- dead-letter state;
- delivery metrics;
- config flags.

Suggested config:

- `XINSIGHT_EXPORT_ENABLED`
- `XINSIGHT_COLLECTOR_URL`
- `XINSIGHT_COLLECTOR_TOKEN`
- `XINSIGHT_EXPORT_BATCH_SIZE`
- `XINSIGHT_EXPORT_TIMEOUT_MS`
- `XINSIGHT_EXPORT_MAX_RETRIES`
- `XINSIGHT_EXPORT_REDACTION_PROFILE`

Non-negotiable rule:

UniRouter request handling must not depend on X-Insight collector availability.

Why:

X-Insight is governance/evidence infrastructure. It must not become a runtime availability dependency for the model gateway in PRD1.

## Recommended Integration Sequence

### Step 1: X-Insight DB Import Adapter

Use current UniRouter tables:

- `request_trace_events`;
- `call_logs`;
- `audit_logs`.

Purpose:

- validate evidence model;
- build first timeline;
- identify missing fields with real data.

No UniRouter changes required.

### Step 2: UniRouter Structural Field Migration

Add trace/span/decision/capture/evidence fields.

Purpose:

- move from F2-ish import to F3 reference emitter.

### Step 3: Cursor Export API

Add API pull with cursor.

Purpose:

- avoid DB coupling;
- support on-prem deployments where direct DB access is undesirable.

### Step 4: Native Outbox Exporter

Add async push to X-Insight collector.

Purpose:

- real-time evidence;
- production-grade integration;
- clean separation between UniRouter runtime and X-Insight collector.

## Boundary: What UniRouter Owns

UniRouter owns source facts inside the gateway boundary:

- request accepted;
- authenticated API key context;
- org/project/key/owner/key type;
- requested model;
- model access result when checked;
- resolved model;
- route candidates and selected route;
- provider/channel;
- billing freeze/settle/cancel;
- provider request/response status;
- usage/cost/latency;
- call-log and audit-log source refs;
- payload capture mode for gateway-visible prompt/response;
- metadata safety and redaction before emission.

UniRouter may emit:

- direct gateway evidence events;
- source references;
- event hashes;
- payload references if payload policy enables them.

UniRouter should not own:

- cross-source evidence timeline UI;
- capture-gap reasoning beyond its own source boundary;
- historical policy simulation;
- X-Insight evidence packs;
- adapter fidelity labels for other systems;
- business outcome inference;
- downstream tool/API/database side-effect claims;
- X-Insight retention/legal-hold workflows beyond payload capture source facts.

## Boundary: What X-Insight Owns

X-Insight owns:

- evidence contract;
- adapter manifests;
- collector API;
- event store;
- source registry;
- field provenance;
- direct / derived / gap / suppressed labeling;
- timeline and evidence-pack construction;
- capture-gap engine;
- policy simulation;
- payload policy registry and display;
- adapter conformance tests;
- cross-source correlation;
- future OTel/MCP/SDK/business-system integrations.

X-Insight should not own:

- UniRouter routing;
- UniRouter billing correctness;
- UniRouter provider failover;
- UniRouter synchronous request success;
- UniRouter API key enforcement;
- raw prompt/response capture inside UniRouter unless explicitly enabled by UniRouter-side policy.

## Runtime Dependency Boundary

PRD1 rule:

> X-Insight must not be in UniRouter's synchronous correctness path.

Allowed:

- async event export;
- DB/API import;
- outbox retry;
- best-effort near-real-time delivery;
- status/health reporting.

Not allowed in PRD1:

- blocking a request because X-Insight collector is down;
- making billing settlement wait for X-Insight;
- making provider failover depend on X-Insight;
- live policy enforcement from X-Insight.

Later exception:

Selective enforcement can be added only when:

- enforcement point is controlled;
- failure mode is explicit;
- customers opt in;
- there is a local policy cache;
- audit and rollback behavior are defined.

## Payload Boundary

Default:

- UniRouter emits metadata-only evidence.

Optional enterprise mode:

- UniRouter may capture prompt/response under explicit payload policy.

But:

- capture mode must be scoped;
- raw content must not be sent by default;
- payload refs are preferred over inline content;
- reads must be audited in X-Insight if content is stored or exposed;
- UniRouter must not leak secrets/API keys/raw tokens into evidence events.

## Integration Mode Matrix

| Mode | Supported by UniRouter today | Good for PRD1 | Requires UniRouter update | Fidelity |
| --- | --- | --- | --- | --- |
| DB import from trace/call/audit tables | Yes | Yes, for internal build | No | F1/F2 |
| Existing API pull via logs and trace endpoint | Partial | Demo fallback | Bulk API needed | F1/F2 |
| Notification webhook | Only notification events | No for main evidence | Yes if repurposed, not recommended | F1 |
| Native X-Insight event emitter | No | Yes, target | Yes | F3 |
| Async outbox exporter | No | Yes, production target | Yes | F3 |
| OTel export | No observed | Later | Yes | F2 bridge |
| Prompt/response payload refs | No observed | Optional later | Yes | Depends on policy |

## Concrete Recommendation

For PRD1 candidate:

1. Start with X-Insight `adapter-unirouter` DB import against current UniRouter tables.
2. Build the first timeline from real `request_trace_events` + `call_logs`.
3. Use this to prove UI, evidence refs, field provenance, capture gaps, and policy simulation.
4. In parallel, prepare UniRouter structural migration for trace/span/decision/capture/evidence fields.
5. Add native outbox/exporter only after the event contract stabilizes from real imported data.

Why not build native exporter first:

- exporter design will be wrong if the evidence contract has not been tested against real data;
- direct DB import is faster and lower risk;
- UniRouter should not take runtime risk before X-Insight proves useful.

Why not stop at DB import:

- DB import is too coupled for a clean open-source/product story;
- public enterprise integration should not require DB credentials;
- F3 reference integration needs source-emitted semantics, not only downstream inference.

## PRD1 Boundary Decision

The PRD1 implementation boundary should be:

- X-Insight implements `adapter-unirouter` import first.
- UniRouter adds the smallest structural fields needed for F3 evidence.
- UniRouter does not yet become dependent on X-Insight.
- Native exporter/outbox is a PRD1 stretch or PRD1.5 item unless the first demo requires real-time push.

This keeps momentum while preserving the correct long-term architecture.
