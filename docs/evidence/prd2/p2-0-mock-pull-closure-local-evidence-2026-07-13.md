# PRD2 Completed-Execution Mock Pull Closure Evidence

Status: Complete; Local and GitHub CI owner verification; PR merged
Date: 2026-07-13
Execution owner: Codex
Base: `main` at `2e83cf7d78e22c6b60af0450b0140de50102687c`
Branch: `codex/prd2-mock-pull-closure`

## Scope And Claim Boundary

This record closes the X-Insight-owned deterministic mock portion of
PRD2-A13. It does not implement or activate a UniRouter endpoint, transport,
authentication method, durable polling job or cross-project compatibility
promise. The accepted sidecar remains the only binding transport; live pull
remains stopped at the joint-contract gate.

The mock consumer state is process-local test state. Restart-durable cursor and
revision persistence is intentionally not represented as implemented because
its storage, replay window, reset and authentication behavior depends on the
still-proposed joint feed contract.

## Executable Behaviors

The deterministic mock now proves:

- an exact source instance, activation timestamp, non-empty activation cursor
  and authorization reference are required;
- pages from another source, pre-activation pages/items and a `has_more` page
  that cannot advance its cursor fail before cursor mutation;
- response, cursor, watermark, identity, revision and snapshot byte bounds are
  strict, unknown response fields are rejected and the snapshot hash covers
  the exact snapshot bytes;
- each mock snapshot declares exactly one request ID matching the feed item,
  carries an explicit false no-raw/no-response/no-key safety declaration and
  passes the existing sensitive-key detector;
- the same request/revision/hash/terminal/completeness tuple replays as
  `duplicate_noop`; an altered same revision, unseen older revision, changed
  terminal timestamp or complete-to-incomplete transition fails closed;
- accepted revisions are increasing within a page before any item commits;
- cursor/high-watermark advancement occurs only after every page item is
  accepted; if a later item fails, prior accepted items are no-ops on the
  in-process retry and the old cursor is retained until the retry completes;
- concurrent polls are rejected; and
- partial timelines are counted explicitly rather than presented as complete.

## Local PostgreSQL Search Proof

The focused test used the dedicated local database
`xinsight_prd2_pullmock_20260713` on PostgreSQL 18. It consumed one bounded,
sanitized, gap-bearing UniRouter-shaped fixture through the mock consumer,
persisted normalized events and immutable synthetic provenance, and then
queried the real execution search projection before returning
`search_visible_at`.

Observed focused result:

```text
mock terminal-to-search p95=2.011092s target=10s
```

The search result preserved revision `1`, matching source/import watermark,
`timeline_complete=false` and `sync_state=gap`. Replaying the same feed item
did not call the committer again. This is Local diagnostic evidence on
PostgreSQL 18, not a Production SLA, PostgreSQL 16 field qualification or live
UniRouter measurement.

The database is retained as named local test state. No database, evidence,
snapshot, release or backup was deleted. The two Phase 1 temporary Gate A JSON
snapshots remain untouched before their 2026-07-17 review date.

## Local Verification

Owner self-verification on 2026-07-13:

- focused mock pull tests with the real PostgreSQL search projection: pass;
- focused evidence safety and pull tests under the Go race detector: pass;
- `go test -count=1 -race ./...` with the dedicated database: pass;
- `go vet ./...`: pass;
- fixture conformance: 11/11 pass; and
- `gofmt`/`git diff --check`: pass.

These Local checks are Codex execution-owner checks, not independent
acceptance. Target, External integration, field usability and Production are
recorded separately.

GitHub Actions run `29260630135` passed `Frontend Tests`, PostgreSQL 16
`Backend Tests` and Go 1.25.11 `Linux Release Artifact`. The PR merge-ref
release used commit `8dfdb58bf88dc9d371e1fb7478a424e1c96cb539`, artifact SHA-256
`87a23331cce6cf012470dba2e5232158ffc78ee86e6ac915392ffdb9ecfcd02e`
and manifest SHA-256
`635e20968f1fbcbe99abb708040a8695e61e9af93c25165a2ba75171536ee189`.
That CI archive identifies a PR merge ref; it is not a promoted SemVer field
artifact.

PR [#33](https://github.com/foigent/x-insight-private-history/pull/33) passed a second complete
Actions run `29260908364` after the documentation evidence update and
squash-merged to `main` as
`cc084b396a50aa032f74e8eb8d4dd5d127d468a6`.

## Remaining Stop

X-Insight cannot implement the live source adapter without UniRouter accepting
the candidate's exact endpoint/envelope, terminal event, cursor/replay/reset,
authentication/rotation, overload and activation semantics. No guessed value
is added by this closure.
