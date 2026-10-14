# X-Insight 0.3.0-rc.7 Checkpoint Epoch Local Evidence

Status: `LOCAL_CANDIDATE_VERIFIED`
CI: `NOT_RUN_FOR_THIS_EXACT_COMMIT`
Target: `NOT_DEPLOYED`
External integration: `NOT_RUN`
Production: `NOT_AUTHORIZED`

## Verified implementation

- database schema 20 adds one prepared operation per binding, immutable epoch history and an atomic current-state rebind;
- both completed-execution and LiteLLM poll-start writes reject a prepared operation for their exact binding;
- root CLI accepts raw Cursor only through a clean absolute, single-link, process-owner/group, `0400`/`0600`,
  regular request file opened with `O_NOFOLLOW` and revalidates descriptor metadata; nested duplicate keys, unknown
  fields and trailing JSON fail closed;
- Connector read model and OpenAPI expose checkpoint `epoch` and optional `epoch_operation_id`, while only Cursor and
  high-watermark digests are returned;
- rc.7 release payload requires the product-local prepare Schema and database schema 20;
- existing M0, Event/hash/idempotency, Gap and event-linked Provenance semantics were not changed.

## Local verification

- Go `1.25.11`: `go test ./... -count=1` — PASS, including host lifecycle tests (`internal/hostctl` 179 s).
- Go `1.25.11`: `go vet ./...` — PASS.
- Go race: checkpoint epoch, host control, both managed workers, database and CLI packages — PASS; the exact
  post-audit checkpoint/CLI rerun also passed.
- PostgreSQL `18.3` isolated loopback database:
  `TestPostgresEpochTransitionArchivesAndAtomicallyRebindsCheckpoint` — PASS. It proves prepared poll gating,
  duplicate prepare conflict, immutable history, atomic epoch 1 to 2 rebind, Connector trust epoch/digest projection,
  exact activate idempotency, abort-before-barrier, stale-state rejection, active-state epoch-drift refusal and
  down-migration refusal after evidence.
- PostgreSQL version readiness: the wider database suite correctly rejects local PostgreSQL 18 where product support
  requires PostgreSQL 16; this is a verified fail-closed boundary, not PostgreSQL 16 qualification.
- Frontend exact Node `22.23.1` / npm `10.9.8`: TypeScript PASS; Vitest 26/26; OpenAPI 37 required paths;
  177 locked license entries; npm audit 0 vulnerabilities; production build PASS.
- Playwright `1.61.1`: 18/18 Chromium E2E PASS across desktop, laptop and narrow profiles.
- deterministic frontend build: two consecutive embedded dist tree digests both
  `fa5a6128af156a4cb27ac2cc8417deac8f40c5ce076bc3a7b76a910815bfdd53`.
- deterministic manual build: two consecutive embedded manual tree digests both
  `caf7d26f026b53ad1f111188eea3057808a7d25ee06d6c7b0bc066f6a01e531b`.
- CI router/contract/release-identity self-tests: 11/11, 8/8 and 8/8 — PASS.
- `scripts/verify-phase2c-reference-runtime.sh` — PASS.
- `scripts/verify-phase2c-litellm-runtime.sh` — PASS.
- shared protocol Consumer Conformance Kit against exact `v2.0.2` — PASS: 83 cases / 29 classes and exact product pin.
- UniRouter focused Go suites for provenance passport, Reference Connector shadow and event connector packages — PASS.

## Evidence boundary

The local PostgreSQL integration proof used PostgreSQL 18.3 only for SQL atomicity; exact PostgreSQL 16 qualification
remains CI/Target work. This candidate has not been pushed, reviewed in PR, built as a signed artifact, deployed,
or exercised against live UniRouter. No ECS service, Feed, credential, traffic, Cursor/Checkpoint, product database or
original user checkout was changed.

The prior first integration remains terminal evidence and is not reopened. The second campaign requires an exact new
Source Instance and checkpoint epoch; historical credentials, Cursor or campaign authority cannot be reused.
