# Receiver-Generated Feed Credential V1 — Local Evidence

Date: 2026-07-26

Work item: `FV-REAL-FEED-V1-RECEIVER-CREDENTIAL-XINSIGHT-20260726`

Classification: Local implementation and verification only. This record is
not Target credential preparation, activation, feed traffic, API/UI readback
or Production evidence.

## Authority and base

- authoritative `ACTIVE-EXCHANGES.md` SHA-256:
  `f8ef2ca331990b0aa1933d0ea61a4b92f5fd717af43110e9c7b1547e04fdf48b`;
- final accepted receiver-generated credential decision SHA-256:
  `c07be727e036b135c78a30a5c0571678f6d6330fc9e1cc6ad23da9a4955c0138`;
- exact implementation base:
  `f7c27a8e158e98194f233c2901aeb7337b1dbd43`
  (tree `c74f54ac279c3277a5c0880eb0d22ed3d452ae00`);
- initial branch `codex/receiver-generated-feed-credential`, merged through
  PR #71 as `7cd7814a8e0de1af0f16f9854d18ba55b08a65a7`
  (tree `7c327e2355b1496e1458477302f7ade17dacc0f8`); and
- corrective branch `codex/feed-credential-auth-preflight` from that exact
  merge after the material activation-barrier review finding arrived. No
  release or deployment occurred between merge and correction.

Before repository implementation, the unused RSA receiver was terminally
retired: its exact Development private slot was destroyed, the three exact
local public/handoff artifacts were individually removed, no ciphertext
existed, and the worker remained inactive/disabled with the live key absent.
No RSA artifact was reused by this implementation.

## Implemented boundary

- root-only `xinsightctl feed-credential prepare|status|activate|abort`;
- 32 `crypto/rand` token bytes encoded as exact `ur-xf1-` plus 64 lowercase
  hexadecimal characters;
- independent 16-byte lowercase hexadecimal operation ID;
- exact six-field
  `unirouter.xinsight.feed-credential-registration.v1` public bundle;
- verifier SHA-256 over
  `unirouter.xinsight.feed-credential.v1` + one NUL byte + exact raw token;
- exclusive/no-follow staged file, root `0600`, regular/single-link checks,
  durable strict nonsecret state and exact operation/verifier binding;
- resumable prepare, abort-before-replace and activate-after-replace/restart
  failure boundaries;
- staged-token authentication preflight using the exact nonsecret worker
  runtime config and existing fixed-endpoint HTTP source semantics; only the
  strict authenticated `completed_execution_feed.connection_paused`,
  `retryable=true` `503` envelope may cross the replacement barrier;
- `401`, `403`, `200`, malformed envelope and transport failure preserve the
  old live file, `.next` and prepared operation state; and
- atomic live-file replacement after that proof and only then
  `systemctl restart xinsight-feed.service`;
- exact schema packaged as a required rc.9 release payload file; and
- rc.9/manual-r9 docs and clean-host/fresh-install qualification assertions
  that run only absent-status and keep live/staged key/state absent.

The fixed zero-token golden vector is:

- key prefix: `ur-xf1-0000000000000`;
- verifier:
  `d14985e8cedc38985529b846e34f152448401b2b2e5e28db0f64bd8727c1ff07`.

The live HTTP source's endpoint, transport policy and Fetch behavior are
unchanged; it now exposes the bounded paused-authentication proof used by
hostctl. No completed-execution DTO, page contract, cursor, high-watermark,
EvidenceStore, PostgreSQL migration or importer protocol changed.

## Local verification

Passed:

- `go vet ./...`;
- `go test ./... -count=1`, including loopback `httptest` cases outside the
  filesystem/network sandbox;
- `go test -race ./... -count=1`, under the same Local loopback allowance;
- focused hostctl lifecycle tests for entropy/grammar/vector, strict public
  output, no raw-token leakage, symlink/hard-link refusal, partial-write
  recovery, prepare/abort/replace/restart crash recovery, wrong binding,
  exact paused-503 initial activation, rotation 401 preservation, unit-active
  non-acceptance, atomic initial activation and rotation;
- pullfeed restart-after-permanent-401 regression proving unchanged durable
  cursor followed by recovery from that same cursor;
- `go run ./cmd/xinsight-importer fixtures run`: 11/11;
- CI change-router self-test: 9/9;
- CI contract-gate self-test: 8/8;
- frontend TypeScript, component tests: 2 files/8 tests, OpenAPI check,
  locked-license check and production build;
- frontend Chromium desktop+narrow E2E: 4/4;
- deterministic manual regeneration;
- shell syntax for signed-release and both Ubuntu qualification scripts;
- release/config/schema JSON parsing, `go mod tidy -diff`, Go formatting and
  `git diff --check`.

The local npm advisory request was not sent because the execution safety
reviewer rejected third-party dependency-metadata egress. Required GitHub CI
remains the authoritative audit and exact Go 1.25.11/PostgreSQL 16 gate.

## Unconsumed runtime state

No Development `feed-credential prepare` was executed. No receiver-generated
runtime token, public registration, active/staged principal, worker start,
poll, request/provider attempt, database mutation, broad backfill, customer
data or Production action was consumed. Tooling deployment must remain
dormant/key-absent until the later Coordinator activation baton.
