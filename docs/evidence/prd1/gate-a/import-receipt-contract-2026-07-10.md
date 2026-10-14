# Gate A Import Receipt Contract

Date: 2026-07-10
Status: implemented and fixture-validated; real controlled snapshot pending

## Implemented

- Added the source-neutral `xinsight.import-receipt.v1` JSON Schema.
- Added snapshot SHA-256 calculation to `unirouter gate-a-check`.
- Added optional snapshot envelope fields `exchange_id` and
  `approval_manifest_sha256`.
- Added `--receipt-output <path>` for a sanitized JSON sidecar receipt.
- Added stable result statuses and warning codes.
- Added schema, persistence-summary, readback-summary, hash, and overwrite
  refusal tests.
- Added CI receipt generation and readback assertions on Postgres-backed import.

## Evidence Boundary

The current repository fixtures do not contain a source exchange identity or
approval manifest hash. Their receipts therefore report
`source_exchange_id_missing` and `approval_manifest_hash_missing`. This is an
intentional integration-gap signal, not evidence that UniRouter already emits
those fields.

Fixture and CI checks prove the X-Insight receipt implementation. Gate A still
requires a real controlled UniRouter snapshot and must not be declared complete
from fixture results.

## Commands

```sh
go test ./cmd/xinsight-importer ./internal/adapters/unirouter
go run ./cmd/xinsight-importer unirouter gate-a-check \
  --snapshot fixtures/unirouter_snapshot/model_permission_denied.json \
  --receipt-output /tmp/xinsight-import-receipt.json
```

Postgres-backed verification remains:

```sh
XINSIGHT_POSTGRES_DSN='<x-insight-postgres-dsn>' \
  go run ./cmd/xinsight-importer unirouter gate-a-check \
  --snapshot '<controlled-unirouter-snapshot.json>' \
  --persist \
  --receipt-output '<controlled-unirouter-snapshot.receipt.json>'
```
