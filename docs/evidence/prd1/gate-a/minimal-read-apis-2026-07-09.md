# Minimal Read APIs

Status: implemented on `feature/minimal-read-apis`
Date: 2026-07-09

## Scope

This checkpoint adds the first source-neutral read APIs over persisted evidence:

- `GET /api/v1/executions/{request_id}`;
- `GET /api/v1/executions/{request_id}/events`;
- `GET /api/v1/executions/{request_id}/gaps`;
- `/readyz` now checks Postgres when `XINSIGHT_POSTGRES_DSN` is configured.

The implementation intentionally does not add UI, RBAC, policy simulation, payload object storage, or a UniRouter runtime dependency.

## Implementation

- `internal/db.EvidenceStore` now reads persisted events and capture gaps by `request_id`.
- `internal/api` owns HTTP routing and JSON responses.
- `cmd/xinsight-server serve` wires the API handler to Postgres only when `XINSIGHT_POSTGRES_DSN` is set.
- `capture_gaps` now stores optional `candidate_gap_types`, matching the UniRouter proposed `candidate_capture_gaps` contract without treating candidates as root-cause facts.

## Verification

Local checks run on 2026-07-09:

```text
go test ./...
go vet ./...
go test -race ./...
go run ./cmd/xinsight-importer fixtures run
go run ./cmd/xinsight-importer unirouter import-snapshot --snapshot fixtures/unirouter_snapshot/missing_trace_gap.json
```

Results:

```text
ok  	github.com/foigent/x-insight/internal/adapters/unirouter
ok  	github.com/foigent/x-insight/internal/api
ok  	github.com/foigent/x-insight/internal/db
ok  	github.com/foigent/x-insight/internal/evidence
```

The local Postgres smoke path was skipped because `XINSIGHT_POSTGRES_DSN` was not configured in this shell. The DB integration test remains present and CI should run it through the workflow Postgres service; it persists a sanitized UniRouter snapshot, reads events by request ID, reads capture gaps, and verifies `candidate_gap_types`.

The CI workflow now also starts `xinsight-server` after persistent import and checks:

- `/readyz`;
- `/api/v1/executions/req-gap-001`;
- `/api/v1/executions/req-gap-001/gaps`;
- exact `candidate_gap_types` preservation on the persisted capture gap.

## Boundary

These APIs expose X-Insight normalized evidence. UniRouter remains the source of runtime/source facts. X-Insight does not ask UniRouter to infer cross-system agent/tool/business nodes, and does not make UniRouter depend on X-Insight in the request path.

## Remaining Gate A Work

1. Import a real controlled UniRouter sanitized snapshot from the shared environment.
2. Exercise the read APIs against that real imported data.
3. Compare visible events, gaps, and candidate gaps against UniRouter's handoff note.
4. Decide whether the next integration step should be read-only DB/view, snapshot automation, or exporter/outbox.
