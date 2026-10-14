# Gate A Check Command

Status: implemented on `feature/gate-a-snapshot-intake`
Date: 2026-07-10

## Scope

This checkpoint adds:

```sh
go run ./cmd/xinsight-importer unirouter gate-a-check --snapshot <snapshot.json>
```

The command produces one JSON intake report for a UniRouter sanitized snapshot.

Without `--persist`, it runs:

- snapshot discovery;
- schema-backed snapshot import;
- duplicate/no-op rerun validation;
- request ID listing;
- remaining input reminder for Postgres readback.

With `--persist`, it additionally runs:

- accepted event persistence into X-Insight Postgres;
- event readback by `request_id`;
- gap readback by `request_id`;
- summary of normalized event types, source event types, gap types, and candidate gap types.

## Why This Matters

The first real UniRouter snapshot should not require manual stitching across four commands. The check command keeps the Gate A evidence loop repeatable while preserving the same conservative boundaries:

- no broad UniRouter export;
- no raw prompt/response;
- no X-Insight runtime dependency inside UniRouter;
- no claim that fixture validation equals real controlled snapshot acceptance.

## Local Verification

Commands run on 2026-07-10:

```sh
go test ./cmd/xinsight-importer ./internal/adapters/unirouter ./internal/db
go run ./cmd/xinsight-importer unirouter gate-a-check --snapshot fixtures/unirouter_snapshot/model_permission_denied.json
go run ./cmd/xinsight-importer unirouter gate-a-check --snapshot fixtures/unirouter_snapshot/missing_trace_gap.json
```

The local fixture run returned:

- `status=validated_in_memory`;
- one request ID, `req-auth-denied-001`;
- `accepted=2`;
- `rejected=0`;
- `quarantined=0`;
- `rerun_duplicate_noop=2`.

The missing-trace fixture also returned `status=validated_in_memory`, `accepted=2`, `rejected=0`, and `quarantined=0`.

The CI workflow now also runs `gate-a-check --persist` against the model-permission denial fixture after migrations and persistent imports.

## Remaining Gate A Work

1. Receive approved real controlled UniRouter request IDs.
2. Receive a sanitized combined snapshot and handoff note.
3. Run `gate-a-check --persist` against that real snapshot.
4. Save the readback comparison and field gaps as Gate A acceptance evidence.
