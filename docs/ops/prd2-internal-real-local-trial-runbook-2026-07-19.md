# PRD2 Internal-Real Local Trial Runbook

Status: Prepared; no trial database, credential, artifact, import or cleanup
operation has been executed

Date: 2026-07-19

Owner/custodian: User/product owner

Execution owner: Codex

## Boundary

This runbook receives one future, binding, internal-real metadata-only
UniRouter snapshot. It does not receive customer data or raw content and does
not modify the existing X-Insight Alibaba Cloud Target.

Verified local prerequisites:

- FileVault: `On`;
- PostgreSQL: Homebrew 16.14 installed at
  `/opt/homebrew/opt/postgresql@16`;
- X-Insight remains loopback-only; and
- artifact/database/DSN/backup paths will be outside Git and the shared
  cooperation directory.

No path below is authorized for creation until the UniRouter response and exact
activation decision bind the actual source, manifest and custody.

## Planned Private Layout

Use one new mode-`0700` trial root under the custodian's FileVault-protected
`~/Library/Application Support/X-Insight/trials/` directory. Bind its exact
timestamped basename in the activation decision before creation.

The root contains distinct private children for:

- PostgreSQL 16 data;
- role-specific DSN files;
- manifest and snapshot artifact;
- import/preflight reports and receipts; and
- backup/restore proof.

Every file path is exact, mode `0600` where applicable and absent before its
exclusive create. The exact paths and hashes belong in private execution logs;
the repository/shared evidence records only sanitized identities and results.

## Stage L0 — Read-Only Preflight

Before creating the trial database:

1. verify current X-Insight/UniRouter commits and worktrees;
2. verify FileVault remains on and PostgreSQL 16 remains the selected binary;
3. verify the manifest/artifact exact byte lengths and SHA-256 values;
4. verify private file type, owner, permissions and single-link identity;
5. run `unirouter preflight-sanitized` against the exact manifest and snapshot
   with 20-request/200-event limits;
6. inspect every finding path and the private field/value inventory; and
7. stop unless the report is exactly `pass` and all decision-bound source
   identities/counts match.

The preflight never uses a database DSN and never persists data.

## Stage L1 — Isolated PostgreSQL 16

Create a new PostgreSQL 16 cluster on an unused loopback port; do not reuse or
upgrade the currently running PostgreSQL 18 development service. Configure:

- `listen_addresses = '127.0.0.1'`;
- one dedicated database;
- separate admin/importer/reader roles with generated SCRAM credentials;
- least privilege matching the existing release role contract;
- role DSNs in separate absolute mode-`0600` files; and
- PostgreSQL logs that do not print DSNs or imported values.

Run X-Insight migrations through the admin credential twice. The first result
must reach schema 11; the second must apply zero migrations. Record an empty
database fingerprint before import.

## Stage L2 — Backup And Restore Qualification

Before import, create one exact custom-format empty-baseline backup and restore
it into one exact scratch database. Verify the catalog and schema ledger, then
retain both identities as restore proof. Do not drop either database or delete
any file automatically; cleanup remains a later explicit custodian action.

## Stage L3 — First Write And Replay

Run the normal importer only after Stage L0-L2 pass:

1. run `gate-a-check` with the same exact `--approval-manifest`; its in-process
   preflight-to-import SHA binding must pass with zero reject/quarantine;
2. first persistence using the dedicated importer DSN file;
3. bounded SQL/read API reconciliation;
4. identical-byte replay with the same artifact SHA-256;
5. prove replay adds/changes zero rows and returns duplicate/no-op; and
6. freeze receipts, reports and post-import database fingerprint.

Any conflict, unexpected duplicate, count drift or artifact mutation stops the
batch.

## Stage L4 — UI Proof

Start one local X-Insight server against the dedicated reader DSN file on
`127.0.0.1:8080`. Do not run a second public/cross-origin UI. Verify every
approved request through:

- `/executions` search;
- execution detail and timeline;
- selected-node detail;
- source/provenance inspection; and
- capture-gap state.

The UI must expose no raw source excerpt or blocked field/value. Compare actual
UI/API facts to the private artifact and sanitized receipt; do not infer an
unobserved scenario.

## Stage L5 — Stop, Retain And Exit

Stop the local trial server and PostgreSQL cluster after evidence capture.
Retain the exact database, artifact and backup through validation closure plus
seven calendar days. Then the custodian chooses handoff or controlled
retirement.

There is no automatic deletion authority. Before retirement, inventory every
copy and exact path/hash. Each file deletion, if later authorized, is one exact
path at a time. Database/directory retirement that would require recursive or
bulk deletion is performed manually by the User under the repository deletion
rule. Add new cleanup evidence; never rewrite historical evidence.

## Claims

Passing this runbook can support only:

> Internal-real External integration verified — execution-owner verified,
> exact approved non-customer metadata sidecar batch only.

It is not independent acceptance, customer validation, named-Target or
Production verification.

## Related Records

- [Internal-Real Metadata Validation Plan](../planning/prd2-internal-real-metadata-validation-plan-2026-07-19.md)
- [Single-Node Target Runbook](deployment/alibaba-cloud-single-node-target-runbook.md)
- [Trial Exit Manual](../manual/zh-CN/admin/05-trial-exit-retention-and-handoff.md)
