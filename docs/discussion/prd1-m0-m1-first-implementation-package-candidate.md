# PRD1 M0/M1 First Implementation Package Candidate

Status: first implementation package candidate, not final sprint plan
Created: 2026-07-08

## Purpose

This document defines the first implementation package for X-Insight PRD1.

It converts the current planning artifacts into a code-start boundary:

- what to build first;
- what must be accepted before moving to M2;
- what must not be pulled into the first package;
- what UniRouter integration uses now without requiring UniRouter changes.

This is not the final PRD and not a full sprint plan. It is the recommended first code package if implementation starts.

## Inputs

- `PRD1 Engineering Milestones Candidate`
- `PRD1 M0 Evidence Envelope Candidate`
- `PRD1 M0 Schema And Indexes Candidate`
- `UniRouter / X-Insight Integration Boundary`
- `PRD1 M2 Timeline UI Information Architecture Candidate`
- `PRD1 M2 Wireframe And Component Candidate`
- [PRD1 M0/M1 Implementation Kickoff](./prd1-m0-m1-implementation-kickoff.md)

## Core Judgment

The first implementation package should prove the backend evidence spine and one real UniRouter import loop.

It should not start with:

- polished frontend;
- dashboard;
- graph canvas;
- LiteLLM;
- UniRouter native outbox;
- OpenTelemetry backend;
- policy simulation;
- payload content storage.

Reason:

If M0/M1 cannot ingest, validate, dedupe, quarantine, and normalize real UniRouter evidence, then M2 UI and M3 LiteLLM work will either be fake or unstable.

## Package Boundary

### Included

M0:

- project skeleton for backend service;
- schema files;
- enum registry;
- Postgres migrations;
- validator;
- canonical event hash;
- sensitive-key detector;
- quarantine path;
- conformance fixtures and runner;
- seed source/adapter manifests for UniRouter fixture data.

M1:

- UniRouter source registration;
- UniRouter current-table DB importer;
- import cursor state;
- idempotent event insertion;
- current `request_trace_events` mapping;
- current `call_logs` mapping for usage/cost/latency;
- basic capture-gap generation;
- import diagnostics.

Minimal API:

- health check;
- source registration/list;
- adapter manifest list;
- execution lookup by request ID;
- event list for request ID;
- capture gaps for request ID;
- import diagnostics/quarantine list.

### Excluded

- React frontend;
- materialized timeline nodes;
- LiteLLM adapter;
- policy simulation;
- evidence pack;
- payload object storage;
- payload read audit;
- advanced RBAC;
- multi-tenant SaaS;
- UniRouter schema migration;
- UniRouter cursor export API;
- UniRouter async outbox/exporter;
- OTel/OpenInference ingestion;
- one-api/new-api adapters.

## Recommended Implementation Order

### P0: Backend Skeleton

Goal:

Create a runnable local service with predictable structure.

Deliver:

- Go module;
- config loader;
- structured logging;
- Postgres connection;
- migration runner or migration convention;
- health endpoint;
- basic integration-test harness.

Candidate tasks:

| Ticket | Description | Acceptance |
| --- | --- | --- |
| XIN-IMP-000 | Create backend module and local config convention. | Service starts locally and reads DB config. |
| XIN-IMP-001 | Add Postgres connection and health endpoint. | Health endpoint verifies process and DB connectivity separately. |
| XIN-IMP-002 | Add migration convention. | Empty DB can migrate to latest schema. |

Do not do:

- auth system;
- frontend shell;
- deployment packaging beyond local/dev.

### P1: M0 Schema Files And Enum Registry

Goal:

Create the implementation source of truth for the evidence envelope.

Deliver:

- `schemas/m0/evidence-event.schema.json`;
- field-group schemas;
- adapter manifest schema;
- source system schema;
- capture gap schema;
- quarantine schema;
- enum registry.

Candidate tasks:

| Ticket | Description | Acceptance |
| --- | --- | --- |
| XIN-IMP-010 | Add M0 JSON Schema file tree. | Schema files match the candidate schema/index docs. |
| XIN-IMP-011 | Add enum registry. | Enum tests reject invalid stage/capture/provenance/gap values. |
| XIN-IMP-012 | Add sample valid/invalid fixtures. | Fixture runner can load fixtures without DB. |

Decision:

- Use JSON Schema Draft 2020-12 unless the selected Go validator makes that impractical.

Do not do:

- source-specific JSON schemas inside the main evidence event schema;
- per-scalar provenance;
- graph schema.

### P2: M0 Postgres Migrations

Goal:

Persist the evidence spine.

Deliver tables:

- `source_systems`
- `adapter_manifests`
- `adapter_import_states`
- `evidence_events`
- `evidence_refs`
- `capture_gaps`
- `payload_policies`
- `ingestion_quarantine`

Candidate tasks:

| Ticket | Description | Acceptance |
| --- | --- | --- |
| XIN-IMP-020 | Add source and adapter tables. | Unique constraints and status fields work. |
| XIN-IMP-021 | Add evidence event table. | Required top-level columns, JSONB groups, unique idempotency key, and indexes exist. |
| XIN-IMP-022 | Add refs/gaps/payload-policy/quarantine tables. | Tables and indexes match M0 candidate. |
| XIN-IMP-023 | Add adapter import state table. | Cursor state can be upserted by source/adapter/cursor key. |

Important:

`event_hash` is X-Insight-generated canonical hash.

`source_event_hash` is optional source-provided integrity data.

Do not do:

- `payload_objects`;
- `payload_read_audits`;
- simulation tables;
- materialized `timeline_nodes`.

### P3: Validator, Canonical Hash, Sensitive-Key Detector

Goal:

Make ingestion strict enough to be trustworthy.

Deliver:

- JSON Schema validation;
- enum validation;
- top-level projection consistency validation;
- canonical normalized event hash;
- sensitive-key detector;
- accept/duplicate/quarantine/reject result model.

Candidate tasks:

| Ticket | Description | Acceptance |
| --- | --- | --- |
| XIN-IMP-030 | Implement schema validator. | Valid fixtures pass; invalid fixtures fail with useful errors. |
| XIN-IMP-031 | Implement canonical event hashing. | Same normalized event yields same hash; DB timestamp changes do not alter hash. |
| XIN-IMP-032 | Implement sensitive-key detector. | Raw prompt/response/secrets in raw excerpt are rejected or quarantined. |
| XIN-IMP-033 | Implement ingestion result model. | Outcomes include accepted, duplicate no-op, quarantined, rejected. |
| XIN-IMP-034 | Implement idempotency conflict behavior. | Same idempotency/different hash keeps original and writes quarantine. |

Do not do:

- semantic DLP;
- LLM-based redaction;
- payload body storage.

### P4: M0 Conformance Fixtures And Runner

Goal:

Prove the contract before importing real source data.

Required fixtures:

1. valid UniRouter request trace event;
2. valid UniRouter call-log-derived usage event;
3. valid LiteLLM callback event;
4. valid LiteLLM spend-log event;
5. invalid event missing `source_ref`;
6. invalid event missing `idempotency_key`;
7. invalid event with raw prompt in `raw_source_excerpt`;
8. duplicate same idempotency and same hash;
9. duplicate same idempotency and different hash;
10. event with safe source-specific extensions.

Candidate tasks:

| Ticket | Description | Acceptance |
| --- | --- | --- |
| XIN-IMP-040 | Add fixture directory and runner. | Runner reports accepted/duplicate/quarantined/rejected. |
| XIN-IMP-041 | Add UniRouter fixture pair. | UniRouter-shaped trace and call-log events validate. |
| XIN-IMP-042 | Add LiteLLM fixture pair. | LiteLLM-shaped callback and spend events validate without implementing adapter. |
| XIN-IMP-043 | Add negative fixtures. | Missing refs, missing idempotency, and unsafe payload fail correctly. |

Why include LiteLLM fixtures in M0:

The first package should not implement LiteLLM, but the evidence schema must already prove it is not UniRouter-shaped.

### P5: UniRouter Current-Table Importer

Goal:

Import real or controlled UniRouter evidence without changing UniRouter.

Data sources:

- `request_trace_events`
- `call_logs`
- relevant audit context only if easy and safe

Minimum importer behavior:

- register UniRouter source;
- register `adapter-unirouter` manifest;
- connect to UniRouter database read-only if possible;
- import trace rows by cursor;
- join/reference call logs by `request_id`;
- generate X-Insight evidence events;
- generate source refs;
- generate idempotency keys;
- generate event hashes;
- upsert import cursor;
- quarantine invalid or risky rows;
- generate capture gaps.

Candidate tasks:

| Ticket | Description | Acceptance |
| --- | --- | --- |
| XIN-IMP-050 | Add UniRouter source config. | X-Insight can store UniRouter source metadata without secrets in source refs. |
| XIN-IMP-051 | Add adapter-unirouter manifest. | Manifest declares F1/F2, DB import, direct/derived/gap groups. |
| XIN-IMP-052 | Implement trace event importer. | Imports current `request_trace_events` rows into `evidence_events`. |
| XIN-IMP-053 | Implement call-log usage join/reference. | Usage/cost/latency fields appear when matching call log exists. |
| XIN-IMP-054 | Implement cursor state. | Re-running importer does not duplicate events. |
| XIN-IMP-055 | Implement UniRouter capture-gap generation. | Missing decision, payload state, business outcome, and tool side effects become gaps. |
| XIN-IMP-056 | Implement import diagnostics. | Bad rows enter quarantine with safe reason/excerpt. |

M1 source refs:

```text
unirouter:<instance>:request_trace_events:<id>
unirouter:<instance>:call_logs:<id>
```

M1 idempotency key:

```text
adapter-unirouter:<source_system_id>:<source_ref>:<event_type>:<occurred_at_or_row_id>
```

M1 capture mode:

- `db_import` for direct DB import;
- `api_import` only if using existing per-log trace API fallback.

Do not do:

- require UniRouter schema changes;
- require UniRouter exporter/outbox;
- mutate UniRouter data;
- import prompt/response;
- bypass sensitive-key detector.

### P6: Minimal Read APIs For M2

Goal:

Expose enough API for M2 UI and manual verification.

Deliver:

- execution lookup by request ID;
- evidence events for request ID;
- capture gaps for request ID;
- source and adapter manifest list;
- import diagnostics/quarantine list.

Candidate tasks:

| Ticket | Description | Acceptance |
| --- | --- | --- |
| XIN-IMP-060 | Add execution lookup endpoint. | Request ID returns source/execution summary. |
| XIN-IMP-061 | Add events-by-request endpoint. | Returns sanitized evidence events in timeline order. |
| XIN-IMP-062 | Add gaps-by-request endpoint. | Returns capture gaps with affected claim and recommendation. |
| XIN-IMP-063 | Add source/manifest endpoints. | UI can show source and fidelity labels. |
| XIN-IMP-064 | Add quarantine diagnostics endpoint. | Operators can inspect safe quarantine records. |

Do not do:

- full timeline projection API;
- policy simulation API;
- evidence pack export;
- auth/RBAC beyond local/dev if it delays Gate A.

## Gate A Acceptance

The first package is complete when:

1. Empty Postgres migrates to M0 schema.
2. Schema fixtures pass/fail correctly.
3. Validator detects unsafe raw prompt/response in raw excerpts.
4. Duplicate same hash becomes no-op.
5. Duplicate same idempotency with different hash goes to quarantine.
6. UniRouter source and adapter manifest can be registered.
7. At least one real or controlled UniRouter request imports from current tables.
8. Re-running import does not duplicate events.
9. Imported events preserve source refs.
10. Capture gaps are generated for missing decision/payload/business/tool evidence.
11. Read APIs can return events and gaps for the imported request ID.

This is Gate A backend acceptance.

It is not M2 UI acceptance.

## Test Strategy

Minimum tests:

- schema fixture validation;
- enum validation;
- sensitive-key detector;
- canonical hash determinism;
- idempotency duplicate/no-op;
- idempotency conflict quarantine;
- migration smoke;
- UniRouter importer mapping using fixture rows;
- import cursor re-run no-duplicate test;
- read API request lookup test.

Manual evidence:

- command to run migrations;
- command to run fixture conformance;
- command to run UniRouter importer against controlled data;
- sample imported request ID;
- sample events/gaps JSON response.

## Implementation Risks

| Risk | Control |
| --- | --- |
| Schema overbuild | Only implement M0 tables and field groups. |
| Fake demo | Require real or controlled UniRouter table import. |
| UniRouter coupling | Adapter maps into X-Insight envelope; no UniRouter-shaped core tables. |
| Payload leakage | Sensitive-key detector plus payload state; no payload object store. |
| Duplicate drift | Idempotency key plus canonical hash plus quarantine. |
| UI blocked | Provide minimal read APIs for M2, not full frontend. |
| LiteLLM forgotten | Include LiteLLM M0 fixtures but defer adapter to M3. |

## What This Package Should Produce

Artifacts:

- schemas;
- migrations;
- validator;
- fixture runner;
- UniRouter importer;
- read APIs;
- test fixtures;
- Gate A evidence note.

Not artifacts:

- PRD;
- polished UI;
- public open-source release;
- enterprise deployment package.

## Remaining Decisions Before Coding

These are implementation choices, not product strategy:

1. Go JSON Schema validator library.
2. Migration tool or convention.
3. Canonical JSON hashing implementation.
4. Local config format for UniRouter DB connection.
5. Exact fixture file format.

## Recommended Implementation Defaults

These defaults are recommended unless implementation proves a concrete problem.

### Go JSON Schema Validator

Default:

- use a Go JSON Schema validator that supports Draft 2020-12, or the closest stable draft if library maturity is materially better.

Selection criteria:

- active maintenance;
- good error paths;
- no heavy runtime dependencies;
- usable in tests and production validator;
- supports references across local schema files.

Do not choose:

- a validator that only works through code generation;
- a validator that makes human-readable validation errors difficult;
- a library that forces a non-Go runtime into M0/M1.

### Migration Tool

Default:

- use simple numbered SQL migrations.

Acceptable tools:

- `golang-migrate`;
- an existing lightweight migration runner if the repo already has one when implementation starts.

Reason:

M0/M1 needs transparent schema review. SQL migrations are easier to inspect than ORM-generated schema drift.

Do not start with:

- ORM-first migrations;
- multi-database abstraction;
- ClickHouse migration support.

### Canonical JSON Hashing

Default:

- implement deterministic canonical JSON for normalized evidence events;
- sort object keys;
- use stable timestamp formatting;
- exclude `id`, `ingested_at`, DB timestamps, import attempt metadata, and non-evidence runtime fields;
- hash with SHA-256.

Hash output:

```text
sha256:<hex>
```

Reason:

This is enough for duplicate-conflict handling. It is not a cryptographic evidence notarization system.

### Local Config Format

Default:

- environment variables for service and DB config;
- optional local YAML/TOML only if it materially improves developer ergonomics.

Minimum config:

- X-Insight Postgres DSN;
- UniRouter read-only Postgres DSN;
- source instance key;
- adapter ID/version;
- import batch size;
- import cursor key;
- safe logging level.

Rules:

- do not store secrets in source refs;
- do not print DSNs with credentials;
- prefer read-only UniRouter DB credentials for M1.

### Fixture Format

Default:

- YAML or JSON fixture file with expected ingestion outcome.

Recommended shape:

```yaml
name: unirouter-trace-valid
expected: accepted
source_system:
  source_type: unirouter
adapter_manifest:
  adapter_id: adapter-unirouter
event:
  ...
```

Fixture categories should match `PRD1 M0 Schema And Indexes Candidate`.

Reason:

Fixtures should be understandable to adapter contributors, not only Go test authors.

## General Defaults

- choose boring Go libraries with active maintenance;
- avoid introducing a queue, ClickHouse, OTel backend, or JS runtime into M0/M1;
- keep all first-package commands runnable locally.

## Current Recommendation

Use this as the first implementation package.

Do not begin with M2 UI until:

- migrations exist;
- validator exists;
- fixtures pass;
- one UniRouter request imports;
- read APIs can return events and gaps for that request.
