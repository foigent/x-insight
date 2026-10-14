# UniRouter Persistent Snapshot Import Check

Status: partial Gate A evidence, Postgres persistence path implemented but real controlled snapshot and read APIs not complete
Date: 2026-07-09

## Scope

This note covers the first Postgres-backed UniRouter snapshot import checkpoint:

- `xinsight-importer unirouter import-snapshot --persist`;
- dependency seeding for `source_systems` and `adapter_manifests`;
- insertion into `evidence_events`;
- idempotency handling through `(source_system_id, adapter_manifest_id, idempotency_key)`;
- same-hash re-import as `duplicate_noop`;
- different-hash conflict detection as `idempotency_conflict`;
- gap event persistence into `capture_gaps`;
- CI coverage using the Postgres service.

This does not prove read APIs or real UniRouter controlled snapshot import yet.

## Commands

Local non-DB checks:

```text
go vet ./...
go test ./...
go run ./cmd/xinsight-importer fixtures run
go run ./cmd/xinsight-importer unirouter import-snapshot --snapshot fixtures/unirouter_snapshot/missing_trace_gap.json
go test -v ./internal/db
```

Postgres-backed command shape:

```text
go run ./cmd/xinsight-server migrate up
go run ./cmd/xinsight-importer unirouter import-snapshot --snapshot fixtures/unirouter_snapshot/missing_trace_gap.json --persist
go run ./cmd/xinsight-importer unirouter import-snapshot --snapshot fixtures/unirouter_snapshot/missing_trace_gap.json --persist
```

## Expected Persistent Behavior

Local `go test -v ./internal/db` skips the DB integration test when
`XINSIGHT_POSTGRES_DSN` is not set. GitHub CI sets this DSN through its
Postgres service and is expected to execute the persistent path.

First persistent import:

```text
events_total=2
inserted=2
capture_gaps_inserted=1
idempotency_conflict=0
```

Second persistent import of the same snapshot:

```text
events_total=2
duplicate_noop=2
inserted=0
idempotency_conflict=0
```

## Boundary

The persistent importer still does not:

- connect to UniRouter live databases;
- require UniRouter runtime dependency on X-Insight;
- expose read APIs;
- render execution timelines;
- store raw prompt/response payloads;
- perform policy simulation.

## Remaining Gate A Work

1. Run discovery, local import, and persistent import against a real controlled UniRouter snapshot.
2. Add minimal read APIs for imported request events and gaps.
3. Save Gate A acceptance evidence from the real controlled snapshot run.
