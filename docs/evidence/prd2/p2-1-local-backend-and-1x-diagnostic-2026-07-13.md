# PRD2 P2-1 Local Backend And 1x Diagnostic Evidence

Status: Local owner self-verification; GitHub PR/CI pending
Date: 2026-07-13
Execution owner: Codex
Base: `main` at `4b6dca44a0947c22bd4a6059a0c96fbb475c4da8`
Branch: `codex/prd2-p2-1-investigation-api`

## Scope And State Boundary

This record covers only the X-Insight-owned P2-1 backend:

- source-qualified execution identity and immutable import-provenance linkage;
- bounded execution search/detail/timeline/event/gap/source APIs;
- fixed-`as_of` keyset cursors and stable errors;
- HTTP/query/pool/import/writer resource limits; and
- a Local diagnostic at the accepted 1x reference shape.

It is not independent acceptance. It does not establish Target, External
integration, field usability or Production status. No UniRouter repository,
endpoint, credential or runtime was changed. The completed-execution consumer
remains disabled/mock-only.

## Fact-Source And Migration Result

Migrations `000010` and `000011` add:

- immutable `import_provenance` rows;
- nullable event linkage to that record;
- a rebuildable `xe1_` execution-ID read projection derived from
  `(source_system_id, exact request_id)`;
- execution/snapshot/effective-time/gap indexes; and
- checks that prevent a request-bearing event from carrying a malformed
  projected ID; and
- an append-only provenance/event association so duplicate/no-op observations
  retain their newer artifact watermark without overwriting the event fact's
  first provenance link.

New snapshot persistence writes proof class, assurance mode, integration mode,
artifact hash, revision/watermark and completeness facts. Operator-approved
classification requires both artifact and approval-manifest SHA-256 values and
an exchange ID. Fixture imports are explicitly synthetic. Existing rows that
have no immutable provenance link remain `unclassified`; they are never
silently promoted to approved evidence.

Snapshot preparation now reads one bounded artifact for validation, event
mapping and SHA-256 provenance. Persistence consumes those already validated,
hashed events instead of reopening the path. Gate A discovery rechecks the
bounded file hash and stops if the path changed while preparing the report.
This removes a validation/hash/persistence time-of-check/time-of-use mismatch.

The writer uses PostgreSQL transaction advisory-lock key
`6308648141524501081`. A contending writer fails before partial persistence.
The event batch is preflighted before the transaction: at most 10,000 events
and 256 KiB encoded bytes per normalized event.

PostgreSQL migration `0 -> 10` plus `10 -> 11`, fixture persistence and
read-model tests passed. A separate empty database then applied all eleven
migrations from `0 -> 11` in 0.77 seconds. The gap-bearing fixture persisted two
events, one immutable provenance record and two provenance/event associations;
`timeline_complete` remained false. A direct SQL/Go identity-equivalence check
reported zero mismatches, and importer output did not serialize the internal
prepared-event collection.

## Correctness Evidence

Executable Go/PostgreSQL tests prove:

- equal request IDs and identical source refs from two source UUIDs produce
  distinct event IDs and opaque executions; adapter-qualified IDs are also
  distinct, while the accepted legacy default source/adapter pair retains its
  prior deterministic event IDs for duplicate/no-op compatibility;
- exact, prefix, source, outcome and gap filters remain bounded;
- search, timeline and gap keyset pages do not repeat the last item;
- a row inserted after `as_of` remains outside that snapshot even when its
  declared `ingested_at` is backdated; a later snapshot sees it and downgrades
  mixed/unlinked proof to `unclassified`;
- page count, exact event/gap totals and `has_more` remain distinct;
- event detail is resolved only inside its execution and never selects
  `raw_source_excerpt`;
- a source-declared parent span resolves even when timestamps are reversed;
- a chronological sibling without a parent declaration receives no inferred
  edge;
- fixture and operator-approved proof construction cannot be conflated; and
- a later operator-approved duplicate/no-op artifact becomes the current
  watermark and approved proof only through immutable event associations; an
  uncovered late event downgrades the whole execution to `unclassified`;
- operator-approved provenance requires an exchange ID at both importer and
  persistence boundaries and through a database constraint; a reused
  provenance UUID is rejected unless all immutable fact fields match the
  existing record;
- malformed/duplicate/unknown inputs, cursor mismatch/expiry, URL/rate/
  concurrency limits and internal store failures return stable bounded
  problems without backend details; and
- writer-lock contention returns before any dependency or evidence row is
  partially persisted; and
- all snapshot readers and the artifact hasher reject files above 64 MiB before
  decode/persistence, while prepared event hashes match the same artifact read.

The local HTTP/PostgreSQL smoke for the sanitized denial fixture returned:

- search: one source-qualified execution, `synthetic_fixture`, `sync_state=gap`;
- detail: two evidence events, two capture gaps, outcome `failure`;
- timeline: exact total two;
- event: allowlisted source hash with no raw excerpt field; and
- gaps: exact total two.

A separate gap-bearing snapshot persistence smoke recorded
`timeline_complete=false`; the API therefore cannot pair an explicit capture
gap with a complete-timeline assertion.

All serialized API timestamps were checked as UTC `Z`, independent of the
database/session timezone.

## 1x Diagnostic

Local-only dataset:

| Dimension | Value |
| --- | ---: |
| Executions | 100,000 |
| Evidence events | 1,000,000 |
| Capture gaps | 200,000 |
| Events per execution | 10 |
| Gaps per execution | 2 |
| Source | one synthetic UniRouter-shaped source |
| Host/database | local macOS development host, PostgreSQL 18 |

This local PostgreSQL 18 run tests query behavior and does not replace the
required PostgreSQL 16 VM/field qualification.

The first implementation computed SHA-256 execution identity inside every
read query. Under 1x, queries crossed the three-second deadline and returned
the intended bounded 503. Repeated search also exposed generic-plan regression
from optional `OR` predicates. The implementation was corrected by:

1. persisting the deterministic ID as a rebuildable indexed read projection;
2. using that index for execution-scoped reads; and
3. generating fixed-shape SQL containing only filters actually supplied.

Warm 20-request sequential sample after the final database-visibility fence:

| Endpoint | p50 | p95 | Maximum | Diagnostic budget |
| --- | ---: | ---: | ---: | ---: |
| unfiltered bounded search, limit 50 | 192 ms | 247 ms | 247 ms | p95 <= 750 ms; p99 <= 2 s |
| detail | 2.2 ms | 4.2 ms | 6.7 ms | p95 <= 500 ms |
| timeline, limit 100 | 2.6 ms | 3.6 ms | 8.3 ms | p95 <= 750 ms |
| gaps, limit 50 | 2.6 ms | 4.2 ms | 5.7 ms | p95 <= 750 ms |

All requests in the recorded series returned HTTP 200. Gap samples were paced
at 60 ms to remain inside the documented process rate. An immediately preceding
unpaced fourth series exceeded the shared 20 request/s budget and returned the
expected bounded HTTP 429 rather than increasing work. This is a small
diagnostic sample, not a statistically sufficient SLA proof.

## Migration Cost Input For P2-4

On the same 1,000,000-event local database:

| One-time operation | Elapsed |
| --- | ---: |
| execution-ID backfill | 80.47 s |
| constraint add/validation | 47.00 s |
| execution-ID index build | 4.69 s |
| provenance/event association backfill and indexes on the same legacy dataset | approximately 24 s |

The migration is transactional and therefore belongs inside a planned
maintenance window. P2-4 preflight must estimate row count/disk headroom and
must not claim online/no-downtime upgrade. The supported PostgreSQL 16 VM still
requires its own timed migration, backup and restore evidence.

## Verification Commands

```sh
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
go run ./cmd/xinsight-importer fixtures run
XINSIGHT_POSTGRES_DSN='<local test DSN>' go test -count=1 -v ./internal/db
npm --prefix frontend run check:openapi
```

The two temporary Phase 1 Gate A JSON snapshots were not read, changed or
deleted. Their controlled retirement date remains 2026-07-17.
