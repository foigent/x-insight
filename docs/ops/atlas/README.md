# Atlas Infrastructure Coordination

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

This directory is the X-Insight-owned communication surface for Atlas or any
later infrastructure operator working only on X-Insight.

Use:

- `requests/` for bounded executable infrastructure tasks and checks;
- `responses/` for sanitized operator results and execution-owner
  reconciliation;
- the project deployment runbooks for stable requirements.

The shared `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE` folder
is reserved for interfaces, evidence exchanges, decisions, and questions that
cross the UniRouter/X-Insight boundary. X-Insight-only host deployment,
security, storage, database, release, and runtime verification do not belong
there.

## Current Work

- [Phase 1 Final Target Pass](requests/2026-07-10-phase-1-final-target-pass.md)
- [First ECS Deployment Reconciliation](responses/2026-07-10-ecs-deployment-report.md)
- [Phase 1 Final Target Result](responses/2026-07-10-phase-1-final-target-pass-result.md)
- [Alibaba Cloud Single-Node Target Runbook](../deployment/alibaba-cloud-single-node-target-runbook.md)

## Data Boundary

Do not place secrets, DSNs, passwords, raw IP addresses, cloud resource IDs,
raw prompt/response content, or customer data in these files. Full sanitized
command logs remain on the target; project records contain concise results,
hashes, and unresolved gaps.
