# PRD1 M0/M1 Implementation Kickoff

Status: implementation kickoff candidate, not completed implementation
Created: 2026-07-08

## Purpose

This document turns [PRD1 Readiness Review v0.1](./prd1-readiness-review-v0.1.md) and [PRD1 M0/M1 First Implementation Package Candidate](./prd1-m0-m1-first-implementation-package-candidate.md) into a concrete first-code-start plan.

It is not the full PRD1 plan. It only covers M0/M1:

- evidence schema;
- migrations;
- validator;
- canonical hashing;
- fixtures;
- UniRouter current-table importer;
- minimal read APIs;
- Gate A backend acceptance.

## Kickoff Judgment

Start with the backend evidence spine.

Do not start with the frontend.

Reason:

The first uncertainty is not whether the UI can be drawn. The first uncertainty is whether current UniRouter records can be converted into trustworthy, idempotent, source-referenced X-Insight evidence events without changing UniRouter.

If that is true, M2 UI and M3 LiteLLM work have a real foundation.

If that is false, UI work will hide the core product risk.

## Non-Negotiable Boundaries

M0/M1 must not include:

- React UI;
- graph canvas;
- generic dashboard;
- LiteLLM adapter implementation;
- one-api/new-api adapters;
- OTel/OpenInference ingestion;
- policy simulation;
- evidence pack;
- prompt/response payload storage;
- payload object store;
- payload read audit;
- enterprise RBAC;
- UniRouter schema migration;
- UniRouter outbox/exporter;
- UniRouter request-path dependency.

M0/M1 must preserve:

- raw prompt/response off by default;
- source refs for every accepted event;
- idempotency for every imported event;
- X-Insight-generated `event_hash`;
- quarantine for unsafe or conflicting input;
- capture gaps for missing evidence;
- read-only UniRouter access where possible.

## Target Outcome

M0/M1 is done when Gate A backend acceptance is demonstrated:

1. Empty Postgres migrates to M0 schema.
2. Valid and invalid fixtures behave as expected.
3. Unsafe raw prompt/response excerpts are rejected or quarantined.
4. Duplicate same idempotency/same hash is a no-op.
5. Duplicate same idempotency/different hash creates quarantine.
6. UniRouter source and `adapter-unirouter` manifest are registered.
7. At least one real or controlled UniRouter request imports from current tables.
8. Re-running import does not duplicate events.
9. Imported events preserve source refs.
10. Capture gaps are generated for missing decision/payload/business/tool evidence.
11. Read APIs return events and gaps for the imported request ID.

This is backend acceptance only. It is not M2 UI acceptance.

## Proposed Repository Shape

The first code shape should be boring and inspectable.

```text
cmd/
  xinsight-server/
  xinsight-importer/
internal/
  api/
  config/
  db/
  evidence/
  ingest/
  adapters/
    unirouter/
  quarantine/
  store/
migrations/
schemas/
  m0/
fixtures/
  m0/
docs/
  evidence/
    prd1/
      gate-a/
```

### `cmd/xinsight-server`

Owns:

- health endpoint;
- minimal read APIs;
- migration command only if the chosen migration approach makes that clean.

Does not own:

- background import scheduling;
- policy simulation;
- frontend serving.

### `cmd/xinsight-importer`

Owns:

- one-shot UniRouter import command;
- batch import mode;
- dry-run mode if cheap;
- safe import diagnostics output.

Reason:

For M1, a CLI importer is easier to test and reason about than a daemon. A scheduler can come later.

### `internal/evidence`

Owns:

- evidence event structs;
- schema validation boundary;
- enum validation;
- canonical event hash;
- top-level projection consistency checks;
- sensitive-key detector integration.

Does not own:

- source-specific SQL;
- timeline projection;
- UI shaping.

### `internal/adapters/unirouter`

Owns:

- UniRouter table queries;
- row-to-evidence mapping;
- source refs;
- idempotency keys;
- capture-gap generation for UniRouter-specific missing evidence;
- adapter manifest seed.

Does not own:

- X-Insight core schema decisions;
- UniRouter schema changes;
- UI timeline rendering.

### `internal/store`

Owns:

- Postgres writes and reads;
- idempotency conflict handling;
- quarantine insertions;
- adapter import state.

Does not own:

- source-specific mapping logic;
- policy simulation.

## Schema File Tree

M0 should start with this file tree:

```text
schemas/m0/
  evidence-event.schema.json
  adapter-manifest.schema.json
  source-system.schema.json
  capture-gap.schema.json
  quarantine-record.schema.json
  field-groups/
    correlation.schema.json
    identity.schema.json
    capability.schema.json
    decision.schema.json
    usage.schema.json
    outcome.schema.json
    payload-policy.schema.json
    field-provenance.schema.json
    raw-source-excerpt.schema.json
  registry/
    enums.json
```

M0 should not create:

- source-specific event schemas as core schema;
- graph schema;
- policy language schema;
- payload object schema;
- timeline node schema.

## Migration File Tree

Use simple numbered SQL migrations.

Candidate shape:

```text
migrations/
  000001_create_source_systems.up.sql
  000001_create_source_systems.down.sql
  000002_create_adapter_manifests.up.sql
  000002_create_adapter_manifests.down.sql
  000003_create_adapter_import_states.up.sql
  000003_create_adapter_import_states.down.sql
  000004_create_evidence_events.up.sql
  000004_create_evidence_events.down.sql
  000005_create_evidence_refs.up.sql
  000005_create_evidence_refs.down.sql
  000006_create_capture_gaps.up.sql
  000006_create_capture_gaps.down.sql
  000007_create_payload_policies.up.sql
  000007_create_payload_policies.down.sql
  000008_create_ingestion_quarantine.up.sql
  000008_create_ingestion_quarantine.down.sql
```

Do not create M0 tables for:

- `payload_objects`;
- `payload_read_audits`;
- `policy_simulations`;
- `timeline_nodes`;
- ClickHouse projections.

## Initial Postgres Tables

Required:

- `source_systems`;
- `adapter_manifests`;
- `adapter_import_states`;
- `evidence_events`;
- `evidence_refs`;
- `capture_gaps`;
- `payload_policies`;
- `ingestion_quarantine`.

Important constraints:

- unique source key for `source_systems`;
- unique adapter ID/version for `adapter_manifests`;
- unique cursor key per source/adapter for `adapter_import_states`;
- unique `(source_system_id, adapter_manifest_id, idempotency_key)` for `evidence_events`;
- indexed `request_id`;
- indexed `trace_id`;
- indexed `span_id`;
- indexed `org_ref`;
- indexed `project_ref`;
- indexed `payload_capture_mode`;
- indexed `occurred_at`;
- indexed quarantine status and reason.

## Fixture Categories

M0 fixtures should be readable by future adapter contributors.

Required fixture categories:

1. UniRouter trace event accepted.
2. UniRouter call-log usage accepted.
3. LiteLLM callback accepted.
4. LiteLLM spend record accepted.
5. Missing `source_ref` rejected.
6. Missing `idempotency_key` rejected.
7. Raw prompt/response in raw-source excerpt quarantined or rejected.
8. Duplicate same idempotency and same hash no-op.
9. Duplicate same idempotency and different hash quarantined.
10. Safe source-specific extension accepted.

Why include LiteLLM fixtures before LiteLLM adapter:

The evidence model must prove it is not UniRouter-shaped. Fixtures are enough for M0; adapter code waits until M3.

## UniRouter Import Discovery Checklist

Before writing importer code, verify the current UniRouter runtime/database shape.

Check:

- actual database type and DSN shape used by the target UniRouter environment;
- `request_trace_events` table exists;
- `call_logs` table exists;
- primary key type for both tables;
- cursor-safe ordering field: preferably numeric `id`, otherwise `created_at` plus `id`;
- `request_id` consistency across trace events and call logs;
- nullability of org/project/key/owner fields;
- `event_type`, `stage`, and `severity` value distribution;
- metadata JSON shape and actual keys present;
- whether metadata remains sanitized in real rows;
- whether any prompt/response-like content exists in trace metadata or call logs;
- whether call logs can join reliably by `request_id`;
- whether audit logs add useful context without adding unsafe payload risk;
- whether existing admin trace API can be a fallback for specific request lookup;
- whether read-only DB credentials can be used.

Discovery outputs:

- table/column snapshot;
- sample safe row excerpts;
- mapping notes;
- import cursor decision;
- source-ref format confirmation;
- capture-gap list after real rows are inspected.

## UniRouter M1 Mapping Rules

M1 should map current UniRouter data conservatively.

Source refs:

```text
unirouter:<instance>:request_trace_events:<id>
unirouter:<instance>:call_logs:<id>
```

Idempotency key:

```text
adapter-unirouter:<source_system_id>:<source_ref>:<event_type>:<occurred_at_or_row_id>
```

Capture mode:

- `db_import` for direct DB import;
- `api_import` only if existing admin trace API is used as fallback.

Minimum direct fields:

- `request_id`;
- `org_ref`;
- `project_ref`;
- `actor_ref` from API key/owner fields when available;
- `model`;
- `requested_model`;
- `provider`;
- `channel`;
- `stage`;
- `event_type`;
- `status_code`;
- `latency_ms`;
- usage and cost from call logs where available;
- occurred time.

Minimum derived gaps:

- no trace/span correlation;
- no explicit policy decision record unless present in event metadata;
- no payload capture policy state unless UniRouter adds or exposes it;
- no business outcome;
- no tool side effects;
- no downstream application action.

## Minimal API Surface

M0/M1 read APIs should be enough for manual verification and future M2.

Required endpoints:

- `GET /healthz`
- `GET /readyz`
- `GET /api/v1/sources`
- `GET /api/v1/adapter-manifests`
- `GET /api/v1/executions/{request_id}`
- `GET /api/v1/executions/{request_id}/events`
- `GET /api/v1/executions/{request_id}/gaps`
- `GET /api/v1/imports`
- `GET /api/v1/quarantine`

Do not add:

- policy simulation endpoint;
- evidence pack export endpoint;
- UI-specific timeline projection endpoint;
- auth/RBAC endpoint unless local/dev safety requires minimal protection.

## Command Targets

Exact commands can change during implementation, but the first package should expose these operations:

```text
xinsight-server migrate up
xinsight-server serve
xinsight-importer fixtures run
xinsight-importer unirouter discover
xinsight-importer unirouter import --once
xinsight-importer unirouter import --dry-run
```

If the final CLI shape differs, it must still support the same operations.

## Gate A Evidence To Save

Gate A should produce a small evidence note under:

```text
docs/evidence/prd1/gate-a/
```

The note should include:

- migration command and result;
- fixture runner command and result;
- UniRouter discovery summary;
- importer command and result;
- imported request ID;
- event count;
- capture-gap count;
- duplicate re-run result;
- example sanitized events response;
- example sanitized gaps response;
- quarantine count and reasons;
- explicit statement that raw prompt/response was not imported.

Do not include:

- secrets;
- DSNs;
- raw prompts;
- raw responses;
- customer data;
- private tokens.

## Implementation Order

Recommended order:

1. Create Go module and local config.
2. Add migrations and DB connection.
3. Add schema files and enum registry.
4. Add validator and fixture runner.
5. Add canonical event hash.
6. Add sensitive-key detector and quarantine model.
7. Add source/adapter seed logic.
8. Add UniRouter discovery command.
9. Add UniRouter importer.
10. Add minimal read APIs.
11. Run Gate A backend acceptance.

Do not invert this order by starting with UI.

## Boundary With UniRouter

X-Insight owns:

- adapter manifest;
- import cursor;
- evidence normalization;
- idempotency;
- event hash;
- capture-gap generation;
- quarantine;
- read APIs;
- later timeline and policy simulation.

UniRouter owns:

- current gateway request processing;
- current trace/call log generation;
- future structural trace/span/decision/capture fields;
- future non-blocking outbox/exporter if needed.

M0/M1 must not require UniRouter changes.

F3 fidelity later may require UniRouter changes.

## First Implementation Risks

| Risk | Control |
| --- | --- |
| UniRouter rows lack enough fields for useful timeline | Generate explicit capture gaps and stop F3 claims. |
| Importer leaks payload-like content | Sensitive-key detector plus quarantine, no raw payload import. |
| Idempotency is unstable | Base keys on source refs and row IDs where possible. |
| Core schema becomes UniRouter-shaped | Include LiteLLM fixtures in M0 and keep source-specific data in extension groups. |
| CLI importer becomes production architecture by accident | Treat it as M1 proof path; scheduler/outbox comes later. |
| API overbuild | Only expose read APIs needed for Gate A and future M2. |

## Current Recommendation

Proceed to M0/M1 implementation from this kickoff note.

The first coding checkpoint should be:

> Empty Postgres migrates, schemas validate fixtures, and unsafe raw payload fixtures are quarantined or rejected.

Only after that should UniRouter importer code begin.
