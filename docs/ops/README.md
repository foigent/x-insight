# X-Insight Operations

This directory contains project-owned operational requirements and runbooks.
These documents are authoritative for X-Insight deployment behavior. X-Insight-
only operator tasks and reconciled reports live under `atlas/`; cross-project
UniRouter/X-Insight contracts and evidence remain in the shared cooperation
workspace.

## Deployment

- [Alibaba Cloud single-node target runbook](deployment/alibaba-cloud-single-node-target-runbook.md)
  defines the current non-production host, network, storage, PostgreSQL,
  release, verification, evidence, and rollback requirements.

## PRD2 Field Operations

- [PRD2 single-node upgrade and rollback contract](../architecture/prd2-single-node-upgrade-and-rollback-contract-2026-07-13.md)
  defines the implemented managed upgrade, backup, application rollback,
  database restore and first-customer owner-supervised release boundary. The
  exact rc.6 supervised workflow artifact and named-Target synthetic v2 proof
  are verified; counterpart decision on X-Insight's proposed, non-binding
  `UR-E` External-integration gate and `N=1` field usability remain separate.
- [Owner-supervised exact-main delivery evidence](../evidence/prd2/p2-4-owner-supervised-trial-main-delivery-evidence-2026-07-14.md)
  records the original exact-main/CI/workflow/artifact IDs and hashes without
  making a Target or Production claim.
- [Named Target pre-adoption blocker evidence](../evidence/prd2/p2-4-named-target-pre-adoption-blocker-evidence-2026-07-14.md)
  records the fail-closed `0.2.0-rc.1` Target preflight and the verified P1
  PostgreSQL deployment-profile mismatch.
- [Named Target restrictive-umask blocker evidence](../evidence/prd2/p2-4-named-target-restrictive-umask-blocker-evidence-2026-07-14.md)
  records the corrected rc.2 bridge/adoption, pre-migration release-staging
  failure, bounded abort and rc.3 resume gate.
- [Phase 1 Target adoption bridge](../architecture/prd2-phase1-target-adoption-bridge-2026-07-14.md)
  defines the corrected canonical storage/credential transition and its
  executable acceptance gates.
- [Supported-host SIGKILL recovery evidence](../evidence/prd2/p2-4-supported-host-sigkill-recovery-evidence-2026-07-14.md)
  records the exact signed process-death/recover/resume path and post-recovery
  supervised delivery. That record closes the supported CI profile only; the
  later exact rc.6 named-Target result is separate, while literal power loss
  and manual-guided User tasks remain untested.
- [RC.6 named Target, v2 sidecar and UI evidence](../evidence/prd2/p2-4-rc6-named-target-v2-sidecar-and-ui-evidence-2026-07-19.md)
  records the exact rc.6 in-place Target upgrade, non-mutating rollback
  readiness and synthetic v2 sidecar/API/UI/manual proof without promoting
  External integration or Production.

## CI And Release Qualification

- [CI routing and release qualification contract](../architecture/ci-routing-and-release-qualification-2026-07-14.md)
  separates pull-request source validation from exact-main release
  qualification, defines the conservative docs-only route, and preserves the
  protected aggregate check.

## Infrastructure Coordination

- [Atlas infrastructure coordination](atlas/README.md) routes X-Insight-only
  deployment requests, operator responses, and project reconciliation.
- [First ECS deployment reconciliation](atlas/responses/2026-07-10-ecs-deployment-report.md)
  records the operational result and the corrections required before
  `Target verified`.
- [Phase 1 final target pass](atlas/requests/2026-07-10-phase-1-final-target-pass.md)
  records the completed checklist and exact CI release identity.
- [Phase 1 final target result](atlas/responses/2026-07-10-phase-1-final-target-pass-result.md)
  records the Codex-operated corrections, deployment, rollback evidence,
  acceptance, and final reconciliation that established `Target verified`.

## Evidence Boundary

An operator report is not project acceptance by itself. Target facts become
X-Insight evidence only after the execution owner reconciles the report against
the applicable runbook, source commit, test results, runtime state, and data
boundary.
