# M0 Foundation Check

Status: partial Gate A evidence, not Gate A completion
Date: 2026-07-08

## Scope

This historical evidence note covers the first M0 foundation checkpoint:

- Go module skeleton;
- M0 JSON Schema files;
- M0 Postgres migration files;
- fixture conformance runner;
- canonical event hash generation;
- sensitive raw payload detection;
- idempotency duplicate and conflict behavior;
- migration runner entrypoint.

It does not prove UniRouter discovery or import by itself. UniRouter discovery is tracked in a later evidence note.

## Commands Run

```text
go test ./...
go run ./cmd/xinsight-importer fixtures run
go run ./cmd/xinsight-server migrate up
```

## Results

`go test ./...` passed:

```text
?    github.com/foigent/x-insight/cmd/xinsight-importer [no test files]
?    github.com/foigent/x-insight/cmd/xinsight-server [no test files]
?    github.com/foigent/x-insight/internal/db [no test files]
ok   github.com/foigent/x-insight/internal/evidence
```

Fixture runner passed:

```text
summary total=10 passed=10 failed=0
```

Covered fixture outcomes:

- accepted UniRouter trace event;
- accepted UniRouter call-log usage event;
- accepted LiteLLM callback-shaped event;
- accepted LiteLLM spend-shaped event;
- rejected missing `source_ref`;
- rejected missing `idempotency_key`;
- quarantined raw prompt in `raw_source_excerpt`;
- duplicate same idempotency and same hash becomes `duplicate_noop`;
- duplicate same idempotency and different hash becomes `idempotency_conflict`;
- safe source-specific extension accepted.

Migration runner result:

```text
xinsight-server: XINSIGHT_POSTGRES_DSN is required
```

This is expected because no local Postgres DSN was configured for this run.

## Files Created

Code:

- `cmd/xinsight-importer/main.go`
- `cmd/xinsight-server/main.go`
- `internal/db/migrate.go`
- `internal/evidence/fixture.go`
- `internal/evidence/hash.go`
- `internal/evidence/ingest.go`
- `internal/evidence/projection.go`
- `internal/evidence/schema.go`
- `internal/evidence/sensitive.go`
- `internal/evidence/fixture_test.go`

Schemas and fixtures:

- `schemas/m0/`
- `fixtures/m0/`

Migrations:

- `migrations/000001_create_source_systems.*.sql`
- `migrations/000002_create_adapter_manifests.*.sql`
- `migrations/000003_create_adapter_import_states.*.sql`
- `migrations/000004_create_evidence_events.*.sql`
- `migrations/000005_create_evidence_refs.*.sql`
- `migrations/000006_create_capture_gaps.*.sql`
- `migrations/000007_create_payload_policies.*.sql`
- `migrations/000008_create_ingestion_quarantine.*.sql`

## Current Gap

Gate A is not complete.

Remaining Gate A backend work:

1. Run migrations against an empty Postgres database.
2. Verify current UniRouter table shape in the target environment.
3. Run the local snapshot importer against a real controlled UniRouter snapshot.
4. Run persistent import against a real controlled UniRouter snapshot.
5. Return imported events and capture gaps through read APIs.

## Payload Safety

Raw prompt/response is not stored by default.

The first sensitive-key detector quarantines `raw_source_excerpt` fields whose keys look like raw prompt/response or secret-bearing content.

This is a baseline guard, not a complete DLP system.
