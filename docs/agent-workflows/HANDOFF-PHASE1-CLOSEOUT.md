# Phase 1 Closeout Handoff

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

Status: Complete; retained as historical closeout instructions
Date: 2026-07-10
Next owner: None for Phase 1

## Objective

This handoff defined the conditions used to close X-Insight Phase 1: an
architecture/function audit, reproducible CI-built release, authorized target
pass, and independent project reconciliation.

Phase 1 means **Evidence Spine And UniRouter Controlled Loop**. It does not mean
the broader PRD1 candidate is complete.

## Read First

1. [`docs/planning/phase-1-closeout-gate.md`](../planning/phase-1-closeout-gate.md)
2. [`docs/architecture/runtime-contract-capability-status.md`](../architecture/runtime-contract-capability-status.md)
3. [`docs/ops/atlas/responses/2026-07-10-ecs-deployment-report.md`](../ops/atlas/responses/2026-07-10-ecs-deployment-report.md)
4. [`docs/ops/atlas/requests/2026-07-10-phase-1-final-target-pass.md`](../ops/atlas/requests/2026-07-10-phase-1-final-target-pass.md)
5. [`docs/ops/deployment/alibaba-cloud-single-node-target-runbook.md`](../ops/deployment/alibaba-cloud-single-node-target-runbook.md)
6. [`docs/discussion/prd1-candidate-scope.md`](../discussion/prd1-candidate-scope.md)
7. [`docs/discussion/prd1-engineering-milestones-candidate.md`](../discussion/prd1-engineering-milestones-candidate.md)
8. [`docs/agent-workflows/AGENT-PROTOCOL.md`](AGENT-PROTOCOL.md)
9. [`docs/agent-workflows/EXECUTION-RECORD.md`](EXECUTION-RECORD.md)

Cross-project input:

- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/requests/2026-07-10-unirouter-prd5-runtime-contract-required-information.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/responses/2026-07-10-x-insight-prd5-runtime-contract-required-information-response.md`

## Confirmed State

- M0 schemas, migrations, validator, fixtures, hashing, sensitive-content
  controls, persistent event idempotency, minimal read APIs, and receipt schema
  are implemented.
- Two controlled UniRouter exchanges completed local X-Insight persistence,
  duplicate/no-op rerun, readback, and receipt reconciliation.
- The first X-Insight ECS deployment is operational on source commit
  `4e1883503084a31cf685f924660a416d2871adfb`.
- Independent checks confirm health/readiness, nine migrations, synthetic
  persistence/readback, loopback listeners, service restart, and binary hashes.
- The final target pass corrected the host firewall, environment-file mode,
  effective mount option, release/systemd layout, and rollback/log evidence.
- Exact CI artifact commit `b9e5811cc53a5cb06614956e66a1c54756617f0a`
  is active and independently reconciled. Phase 1 is complete with no
  unresolved P0/P1.
- X-Insight-only Atlas communication now belongs under `docs/ops/atlas/`.
  UniRouter/X-Insight interfaces and evidence exchanges remain in the shared
  cooperation folder.

## Completed Codex Batch

### 1. Architecture And Function Audit

Inspect current source, schemas, migrations, APIs, CI, deployment, and candidate
product documents. Produce a prioritized report that covers:

- module boundaries and dependency direction;
- whether the common evidence model is genuinely source-neutral;
- schema evolution, adapter isolation, idempotency, provenance, and capture-gap
  semantics;
- transaction, failure, concurrency, memory/size, and data-integrity risks;
- security, tenancy, retention, deletion, audit, and raw-payload boundaries;
- API lifecycle, compatibility, pagination, auth, and error-contract gaps;
- release/deployment reproducibility and operability;
- which Phase 2 functions create product value versus platform plumbing;
- P0/P1 findings that block Phase 1 closure and a prioritized Phase 2 backlog.

Do not implement the broader PRD1 candidate during this audit. Fix only a P0/P1
issue that actually blocks a trustworthy Phase 1 release; record the rest.

### 2. Reproducible Release Artifact

Add a CI path that builds static Linux/amd64 `xinsight-server` and
`xinsight-importer` binaries and packages:

```text
bin/xinsight-server
bin/xinsight-importer
migrations/
schemas/
fixtures/
release-manifest.json
SHA256SUMS
```

Requirements:

- deterministic layout and sorted checksum generation;
- exact commit, Go version, target OS/architecture, file list, and component
  hashes in the manifest;
- no `.git`, credentials, DSNs, local files, or raw Gate A artifacts;
- CI verifies archive contents and all checksums before upload;
- artifact identity is recorded in the execution record and Atlas request.

Run local focused checks and full Go regression. GitHub CI with PostgreSQL is the
required database-backed result.

### 3. Authorized Target Execution

The User reassigned the prepared Atlas pass to Codex. Codex corrected the
target, deployed the exact CI artifact, retained full target logs, and recorded
the sanitized result under `docs/ops/atlas/responses/`.

### 4. Final Reconciliation

Codex independently re-read cloud and target facts after operator execution,
verified the retained-log checksum manifest, updated the Phase 1 gate, and
closed only after every requirement and P0/P1 disposition was complete.
`Target verified` and `Phase 1 complete` are evidence-backed project
decisions, not inherited from an operator heading.

## Stop Conditions

Stop before deployment if the artifact identity is incomplete, CI did not pass,
the target differs from the approved non-production host, rollback cannot be
preserved, or a request would require public ingress/target GitHub credentials.

Stop closure if any P0/P1 architecture finding remains, target state conflicts
with the runbook, fixture/readback evidence fails, or the data boundary expands.
