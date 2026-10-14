# Completed-Execution Feed V1 Local Implementation Evidence

Date: 2026-07-25

Status: implementation candidate on
`codex/completed-execution-feed-v1`; complete Local source, frontend and
browser verification passes. PR CI, merge, deployment, live activation,
Target, External integration and Production are recorded separately and are
not yet claimed by this record.

## Authority And Baseline

- Work item:
  `FV-REAL-FEED-V1-XINSIGHT-IMPLEMENTATION-20260725`.
- Starting `origin/main`:
  `3adc06698df99016709acce8f1b8a897b69cf36e`.
- Accepted reopen decision SHA-256:
  `fb05cb118e7ea737896cfbe18f0cc1ff41d3201566af27f2f3485373385e23c1`.
- Accepted UniRouter source commit:
  `0fe054cb7b5e2b1b8081b0eb8dd454a669772804`.
- Accepted inner schema SHA-256:
  `4ae2897d4f8470c38624dfe21c54d72eaf6d995d2c9578c4420c94d244424fb2`.

The historical one-off snapshot producer/import route is not reused as the
live transport. No artifact read/import, external source connection, real
database selection or deployment occurred during this Local implementation.

## Implemented Scope

- strict byte receiver and source-neutral mapper for immutable
  `unirouter.completed-execution.v1` revision `1`;
- exact vendored schema and four golden fixtures with recorded source hashes;
- fixed-endpoint source client with no environment proxy, no redirects,
  bounded time/body/header limits, typed safe errors and feed-only bearer
  credential;
- HTTPS by default, with an explicit false-by-default private-tunnel HTTP
  setting;
- durable activation/source-instance/cursor/high-watermark/status/counters in
  `adapter_import_states`;
- per-item idempotent evidence transactions followed by a separate
  cursor-after-page state transaction;
- strict no-progress, empty-page advancement, source mismatch, conflict,
  partial convergence and cursor-expiry failure behavior;
- live provenance that carries source payload identity without sidecar
  artifact/manifest/exchange semantics or per-item page cursor/high-watermark
  claims;
- one separate writer-capable systemd worker with upgrade/rollback/restore/
  recovery stop-and-restore handling; and
- release identity `0.2.0-rc.7`, database schema `13` and manual
  `0.2.0-rc.7/manual-r7`.

## Correctness Regressions

Focused tests demonstrate:

- a terminal timestamp before activation is accepted when the item appears
  after the opaque bootstrap cursor;
- a nonempty page with `next_cursor == current_cursor` fails closed;
- a post-activation empty page cannot advance the durable cursor;
- when item 1 persists and item 2 fails, `CommitPage` is not called, the
  durable cursor stays unchanged and item provenance does not claim the
  page cursor or high-watermark;
- replay after a crash converges through duplicate/no-op;
- source clock skew does not invalidate otherwise valid normalized evidence;
  and
- private-tunnel HTTP is rejected unless the exact runtime setting is enabled.

The loopback HTTP source tests require local ephemeral listeners. They passed
with the test process allowed to bind only its temporary loopback server.

## Local Verification

- Go `1.25.12` with module network disabled:
  `go test ./... -count=1`, `go vet ./...` and `go mod tidy -diff` passed.
- Existing fixture conformance: 11/11 passed.
- Frontend local toolchain Node `23.11.0` / npm `10.9.2`:
  Vitest 8/8, TypeScript, OpenAPI, 177 locked-license entries and the
  production Vite build passed.
- Playwright Chromium desktop/narrow investigation coverage: 4/4 passed.
- CI change-router and contract-gate self-tests: 9/9 and 8/8 passed.
- Release/manual checks: shell syntax passed for the changed qualification and
  signed-release scripts; the generated manual is deterministic at source
  SHA-256
  `2bc48066360b60ede73080418a4cbc5897d625aacb2058568031c49b18565e16`.
- All five copied UniRouter schema/fixture byte counts and SHA-256 identities
  match the accepted handoff.

The Local Node/npm versions differ from the release-pinned Node `22.23.1` /
npm `10.9.8`. Exact pinned-toolchain asset determinism, dependency audit,
PostgreSQL integration/race tests and signed release qualification remain PR or
exact-main CI gates.

## Unverified In This Record

- no PostgreSQL integration case was executed against a real Local database;
- no signed release artifact was built or installed;
- no service key, endpoint or tunnel was bound;
- no worker process was activated;
- no UniRouter page was fetched;
- no investigation HTTP/API readback was measured; and
- no Target, External-integration or Production claim exists.

Required PostgreSQL service tests and release gates remain part of PR CI.
Deployment and live fault/replay/readback tests require the later joint
deployment baton after both repositories merge.
