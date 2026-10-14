# UniRouter Discovery Command Check

Status: partial Gate A evidence, discovery complete; local snapshot import tracked separately
Date: 2026-07-09

## Scope

This note covers the first UniRouter discovery checkpoint:

- `xinsight-importer unirouter discover`;
- snapshot discovery from a sanitized UniRouter PRD5-shaped JSON file;
- optional read-only Postgres structure discovery path;
- safety summary for raw prompt/response and unsafe keys;
- local tests for normal and unsafe snapshot handling.

This does not prove UniRouter evidence import by itself. Local snapshot import is tracked in `unirouter-snapshot-importer-2026-07-09.md`.

## Commands Run

```text
go vet ./...
go test ./...
go run ./cmd/xinsight-importer fixtures run
go run ./cmd/xinsight-importer unirouter discover --snapshot fixtures/unirouter_snapshot/minimal.json
```

## Results

`go vet ./...` passed.

`go test ./...` passed:

```text
?   	github.com/foigent/x-insight/cmd/xinsight-importer	[no test files]
?   	github.com/foigent/x-insight/cmd/xinsight-server	[no test files]
ok  	github.com/foigent/x-insight/internal/adapters/unirouter
?   	github.com/foigent/x-insight/internal/db	[no test files]
ok  	github.com/foigent/x-insight/internal/evidence
```

Fixture runner passed:

```text
summary total=10 passed=10 failed=0
```

Discovery command passed against `fixtures/unirouter_snapshot/minimal.json`.

The discovery report included:

- `source=unirouter`;
- `mode=snapshot`;
- summaries for `request_trace_events` and `call_logs`;
- field names from the sanitized snapshot;
- row count `1` for each source table;
- safety flags for raw prompt, raw response, API key inclusion, and unsafe keys;
- next actions for importer mapping review.

## Current Boundary

The command is intentionally read-only.

It does not:

- import rows into `evidence_events`;
- create capture-gap rows;
- infer missing fields;
- persist raw prompt/response content;
- expose a UI/API timeline.

## Remaining Gate A Work

1. Run discovery against the controlled UniRouter development environment or an exported PRD5 snapshot.
2. Compare actual UniRouter fields against the X-Insight evidence schema.
3. Run the snapshot importer against a real controlled UniRouter snapshot.
4. Add database persistence for imported events.
5. Re-run persistent import and prove idempotency.
6. Expose imported events and derived capture gaps through read APIs.
