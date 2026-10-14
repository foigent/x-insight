# Phase 1 Closeout Gate

Status: Complete
Decision date: 2026-07-10
Owner: Codex

## Phase Definition

Phase 1 is **Evidence Spine And UniRouter Controlled Loop**.

It proves that X-Insight can accept minimized gateway evidence, normalize and
persist it with explicit provenance and capture gaps, read it back, produce a
machine-readable receipt, and run as an isolated non-production service.

Phase 1 is a foundation checkpoint. It is not the completion of the broader
PRD1 candidate, which also proposes a human-readable timeline, a real external
gateway adapter, payload-policy workflows, and historical policy simulation.

## Closure Claims

Closing Phase 1 permits these claims:

- the M0 evidence/storage spine is implemented and CI verified;
- two authorized UniRouter snapshot exchanges completed the controlled
  import/persistence/readback/receipt loop;
- X-Insight can run independently of UniRouter and is not in UniRouter's
  synchronous request path;
- a reproducible Linux release can be deployed and verified on the named
  non-production target;
- known missing capabilities are recorded without being represented as
  implemented.

Closing Phase 1 does not permit `Production verified`, complete agent
governance, external-gateway compatibility, public API security, frontend, HA,
or customer-data claims.

## Closure Gates

| Gate | Required evidence | Current state |
| --- | --- | --- |
| P1-G1 Evidence spine | Schemas, migrations, validator, fixtures, canonical hashing, sensitive-content controls, persistent idempotency, read APIs, and receipt contract pass repository CI | Complete |
| P1-G2 UniRouter controlled loop | Two approved sanitized exchanges persist, duplicate/no-op on rerun, read back, and reconcile by receipt without raw payload or provider calls | Complete |
| P1-G3 Target baseline | Dedicated zero-ingress security group, host firewall, loopback service/database, data-disk requirements, migrations, synthetic readback, restart, logs, and rollback evidence meet the project runbook | Complete; [final target result](../ops/atlas/responses/2026-07-10-phase-1-final-target-pass-result.md) |
| P1-G4 Reproducible release | CI builds a Linux/amd64 release containing both binaries, migrations, schemas, fixtures, manifest, and SHA-256 checksums; the exact artifact is deployed | Complete; exact artifact from commit `b9e5811cc53a5cb06614956e66a1c54756617f0a`, Actions run `29109235304` |
| P1-G5 Architecture/function audit | Current code and proposed product architecture are reviewed; no untriaged P0/P1 finding remains; Phase 2 work is prioritized without silently expanding Phase 1 | Complete |
| P1-G6 Cross-project truth | UniRouter's runtime-contract request is answered with implemented, not implemented, not decided, and runtime-pending facts | Complete |
| P1-G7 Final reconciliation | Exact CI commit, target artifact digest, runtime evidence, remaining gaps, and project status agree across the execution record and operations evidence | Complete; independent cloud/target readback and retained-log manifest verified |

Phase 1 closes only when every gate is `Complete`. An operator's pass statement
does not replace project reconciliation.

## Closeout Decision

All seven gates are complete. During target execution Codex found and fixed two
additional P1 release blockers: floating Go patch selection and missing archive
directory metadata. Two failed activation attempts restored the prior symlink
and readiness before the corrected artifact was activated.

The final target uses exact CI commit
`b9e5811cc53a5cb06614956e66a1c54756617f0a`. Cloud and target readback agree
with the release manifest, target runbook, synthetic persistence/duplicate
behavior, API readback, and retained-log checksum manifest. No P0 or unresolved
P1 remains.

Phase 1, **Evidence Spine And UniRouter Controlled Loop**, is closed. This does
not approve the broader PRD1 candidate or any production claim.

## Post-Close Operational Obligations

These data-custody items do not reopen Phase 1 implementation or its completed
gates:

- the first temporary Gate A snapshot must be deleted by `2026-07-17` under its
  approved seven-day retention rule; sanitized contracts, receipts, hashes, and
  reconciliation evidence may be retained;
- the second temporary Gate A snapshot artifact must also be deleted by
  `2026-07-17`; this does not authorize deletion of receipts, sanitized evidence
  records, or persisted normalized evidence.

## Phase 2 Candidate Backlog

These capabilities are intentionally outside the Phase 1 closeout:

The User selected the X-Insight-owned execution search, timeline, node detail,
source/provenance, and capture-gap UI as the first Phase 2 product priority on
2026-07-11. See [Phase 2 Product Priority](phase-2-product-priority.md). This
priority decision does not accept the remaining candidate backlog.

- human-readable execution search, timeline, node details, and gap/payload
  panels;
- durable import jobs and authorized deep links;
- authenticated receipt/ingestion transport and receipt authenticity;
- retention, deletion receipt, audit-on-read, and custody workflows;
- LiteLLM, New API, one-api, OpenTelemetry/OpenInference, MCP, SDK, and business-
  system adapters;
- adapter SDK and compatibility/deprecation policy;
- policy simulation;
- optional governed prompt/response storage;
- enterprise auth/RBAC and public API exposure;
- continuous UniRouter exporter/outbox;
- backup/restore, encrypted storage, HA/DR, and production readiness.

Phase 2 scope is not accepted merely because an item appears here. The
architecture/function audit must prioritize it against product value,
dependency order, and evidence risk.

## Related Records

- [Runtime Contract Capability Status](../architecture/runtime-contract-capability-status.md)
- [Phase 1 Closeout Architecture And Function Audit](../architecture/phase-1-closeout-audit-2026-07-10.md)
- [Phase 1 Final Target Pass](../ops/atlas/requests/2026-07-10-phase-1-final-target-pass.md)
- [Phase 1 Final Target Result](../ops/atlas/responses/2026-07-10-phase-1-final-target-pass-result.md)
- [Alibaba Cloud Single-Node Target Runbook](../ops/deployment/alibaba-cloud-single-node-target-runbook.md)
- [Phase 1 Closeout Handoff](../agent-workflows/HANDOFF-PHASE1-CLOSEOUT.md)
- [Phase 2 Product Priority](phase-2-product-priority.md)
