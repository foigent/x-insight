# Phase 1 Closeout Architecture And Function Audit

Status: Complete; all discovered P1 findings fixed and reconciled
Date: 2026-07-10
Owner: Codex
Baseline reviewed: `5c2e1ece59e521cae049a95dc21690ce6eac11c0`

## Scope

This audit covers current source, schemas, migrations, APIs, CI, deployment
runbook, Atlas handoff files, and PRD1 candidate product documents. It evaluates
the Phase 1 closeout boundary only: **Evidence Spine And UniRouter Controlled
Loop**. It does not accept the broader PRD1 candidate as complete.

## Blocking Decision

| Severity | Finding | Phase 1 decision |
| --- | --- | --- |
| P0 | None found. | No P0 blocker. |
| P1 | CI-built Linux/amd64 release artifact did not exist. The target deployment was built from source on the host, so P1-G4 could not close and Atlas could not deploy a hash-addressed artifact. | Blocking before this change. Fixed by `scripts/build-release.sh`, `scripts/verify-release.sh`, and the CI `Linux Release Artifact` job. |
| P1 | CI selected the floating Go `1.25` line instead of an exact patch, so rebuilding the same commit later could silently change binaries. | Found during Codex target preflight. Blocking exact release reproducibility; fixed by pinning CI to Go `1.25.11` and making release verification enforce that identity. |
| P1 | The release tar contained file entries only. Under a restrictive extraction umask, intermediate directories became `0700`, so the non-root service could not execute `bin/xinsight-server`. | Found during target activation and rolled back safely. Blocking deployability; fixed by explicit deterministic directory entries plus verifier checks for paths, ownership, modes, and mtimes. |
| P1 | The source-build systemd unit expected a root-level binary while the release contract places binaries under `bin/`. | Found during activation and rolled back safely. Fixed with a target drop-in that uses `current/bin/xinsight-server`; restart/readback passed. |
| P1 | Final target pass for host firewall, secret-file mode, effective mount options, retained logs/rollback, and exact CI artifact deployment. | Complete. Codex performed the User-reassigned pass and independent reconciliation; no unresolved discrepancy remains. |

No other P0/P1 finding was identified that requires code implementation before
the bounded Phase 1 release artifact and final target pass.

## Source-Neutral Evidence Model

The common evidence model is source-neutral enough for Phase 1:

- `schemas/m0/evidence-event.schema.json` accepts multiple `source_type`
  values and uses common field groups for correlation, identity, capability,
  decision, usage, outcome, payload policy, provenance, and raw source excerpts.
- The database stores normalized event fields plus JSONB field groups and does
  not require UniRouter-only trace or decision fields.
- `internal/evidence` validates schemas, projection consistency, event hashing,
  sensitive-content quarantine, and idempotency without importing UniRouter
  packages.
- UniRouter-specific logic is isolated under `internal/adapters/unirouter` and
  the CLI command path.

Boundary: LiteLLM, one-api, New API, OTel/OpenInference, MCP, SDK, and business
systems are schema/source-type reservations or fixtures only. They are not
implemented adapters and must not be described as such.

## Module And Dependency Boundaries

Current dependency direction is acceptable for M0/M1:

- `cmd/` wires CLI/server commands to internal packages.
- `internal/evidence` owns schema validation, hashing, fixtures, projection
  consistency, and sensitive detection.
- `internal/db` owns migrations, event persistence, duplicate/no-op conflict
  handling, and readback.
- `internal/api` exposes read-only execution/event/gap surfaces over a store
  interface.
- `internal/adapters/unirouter` owns UniRouter snapshot discovery and mapping.
- `internal/exchange` owns the import receipt shape.

Risk: `internal/db.ensureDependencies` currently seeds a UniRouter-flavored
adapter manifest for persisted imports. This is acceptable while the only
persisting adapter is UniRouter, but Phase 2 should move manifest registration
behind adapter-owned metadata instead of hard-coding manifest content in the DB
package.

## Schema Evolution And Adapter Isolation

The migrations are append-only and currently nine forward migrations apply
idempotently in sequence. Schema reservations for import states, payload
policies, quarantine, and capture-gap candidates are acceptable because current
docs label them as not implemented workflows.

P2 risks:

- No compatibility registry for evidence schema versions or adapter manifest
  versions.
- No down-migration operating policy beyond "do not run destructive rollback."
- No migration advisory lock; two concurrent migration runners could race on
  the same version. The current runbook invokes migrations sequentially on a
  single node, so this is not a Phase 1 blocker.
- No adapter SDK/contract tests beyond fixtures and the UniRouter importer.

## Idempotency, Transactions, Concurrency, And Data Integrity

Current persistence enforces the documented key
`(source_system_id, adapter_manifest_id, idempotency_key)`:

- same key and hash becomes `duplicate_noop`;
- same key and different hash becomes `idempotency_conflict`;
- accepted event plus capture-gap insertion happens in one transaction;
- duplicate capture gaps are keyed by deterministic IDs.

P2 risks:

- The importer reads local snapshots into memory and has no file-size or event
  count limit.
- There is no durable import job, cancellation, retry, or partial-progress
  state machine.
- Concurrent duplicate import behavior relies on database uniqueness, which is
  acceptable, but high-contention importer tests do not yet exist.
- Evidence refs are migrated but not populated by the current importer.

## Provenance And Capture Gaps

Phase 1 provenance is adequate for controlled snapshots:

- source refs, source event IDs/types/hashes, source versions, and adapter
  manifest IDs are preserved when present;
- capture gaps are represented as explicit events and persisted gap rows;
- receipts echo exchange/artifact hashes but do not claim sender authenticity.

P2 risks:

- No receipt signature/MAC or network receipt transport.
- No stable source authenticity model beyond artifact hash correlation.
- No custody workflow for sidecar receipts.
- No continuous source cursor/outbox.

## Security, Tenancy, Retention, Deletion, And Audit

The current service is safe only under the documented loopback-only,
non-production boundary:

- raw prompt/response and secret-looking snapshot keys are refused or
  quarantined;
- the target runbook requires loopback-only service/database listeners and no
  public ingress;
- current APIs have no auth, tenant authorization, or audit-on-read and are
  documented as development surfaces.

P2 risks:

- No tenant model, RBAC, API auth, audit-on-read, or public API security.
- No retention lifecycle, deletion API, immutable deletion receipt, legal hold,
  or backup/restore proof.
- Payload policy rows exist but no enforcement/resolution lifecycle exists.
- Target disk encryption, snapshot policy, and HA/DR remain unproven; therefore
  fixture/synthetic data only.

These are not Phase 1 blockers because Phase 1 explicitly forbids production,
customer-data, public API, and raw-payload claims.

## API Lifecycle And Contract

Implemented APIs are limited to health/readiness and read-only execution,
events, and gaps by request ID. They include a bounded `limit` parameter but no
cursor pagination, auth, version-compatibility guarantee, or public error
contract.

P2 risks:

- Add cursor pagination before large event streams.
- Define stable API versioning and error response contracts before external
  clients depend on these endpoints.
- Add authentication and tenant checks before any non-loopback exposure.

## Operability And Release Reproducibility

Before this change, CI proved backend behavior but did not produce a deployable
release artifact. This blocked Phase 1 because Atlas needs a hash-addressed
artifact instead of target-side source builds.

This change adds:

- static Linux/amd64 `bin/xinsight-server` and `bin/xinsight-importer` builds;
- package layout with `migrations/`, `schemas/`, `fixtures/`,
  `release-manifest.json`, and `SHA256SUMS`;
- manifest metadata for commit, Go toolchain, target OS/arch, cgo state, and
  per-component hashes/sizes;
- an exact Go `1.25.11` CI pin and verifier assertion, preventing a later
  patch release from silently changing binaries for the same commit;
- explicit `0755` archive directory entries and normalized root ownership,
  file modes, and mtimes, verified before upload;
- deterministic tar/gzip metadata from the source commit timestamp;
- CI verification of archive paths, required files, manifest hashes,
  SHA256SUMS, Linux/amd64 target, and forbidden release contents.

P2 risks:

- No signed release or provenance attestation yet.
- No productized Docker/Compose bundle; target Compose/systemd remains
  operator-managed.
- No automated backup/restore or monitoring package.
- Current Actions versions emit a Node 20 runtime deprecation warning and
  should be upgraded in normal CI maintenance.

## Phase 2 Product Value Versus Platform Plumbing

High product-value Phase 2 work:

1. Execution search/detail timeline with direct, derived, gap, and suppressed
   labels.
2. Source references and capture-gap panels that make missing evidence visible.
3. LiteLLM callback and spend-log ingestion to prove non-UniRouter credibility.
4. Governed payload policy display and later controlled payload capture.
5. Lightweight historical policy simulation over persisted evidence.
6. Evidence-pack view/export for operator and audit handoff.

Platform plumbing that should follow only when it supports the product loop:

- adapter SDK and compatibility/deprecation registry;
- durable import jobs, transport retries, and deep links;
- public collector APIs and receipt authenticity;
- retention/deletion/audit custody workflows;
- signed artifacts, backup/restore, HA/DR, and production hardening;
- broad OTel/OpenInference/MCP/SDK/business-system adapters.

Phase 2 should not start with timeline, LiteLLM, policy simulation, or public
API work until Phase 1 target verification is complete.

## Closeout Conclusion

Exact-toolchain and directory-metadata fixes are merged, main CI passed, and
Codex deployed and reconciled the exact artifact. No P0 or unresolved P1
remains. Phase 1 is complete under the fixture/synthetic-only, loopback,
non-production boundary; the Phase 2 and production-hardening backlog above
remains open.
