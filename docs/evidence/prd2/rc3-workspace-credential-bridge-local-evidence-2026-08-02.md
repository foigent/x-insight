# rc.3 Workspace credential bridge — local evidence

Date: 2026-08-02

## Triggering Target fact

The named non-production ECS remained on `0.2.0-rc.15`, schema 15, with X-Insight active and both
Feed workers inactive/disabled. Its active database set contained only reader/importer/admin DSNs
and PostgreSQL had no `xinsight_workspace` role. The signed rc.2 target controller failed
`upgrade --check` before any operation journal, backup, migration, release switch, credential change,
or database change with a missing Workspace credential error. The temporary transition trust store
was restored to the prior exact bytes.

No secret value is recorded here. The Target failure remains a fail-closed defect proof, not a
successful rc.2 deployment or Target qualification.

## Root cause verified in source

- rc.15 `LoadDatabaseCredentials` requires reader/importer/admin only.
- rc.2 `LoadDatabaseCredentials` requires reader/workspace/importer/admin before upgrade database
  inspection.
- rc.2 declares rc.15 in `supported_upgrade_from_versions`.
- the former rc.14→rc.15 and later-target tests reused the current four-file fixture, so they did not
  represent an exact rc.15 host credential set.

## Implemented local candidate

`0.3.0-rc.3` adds the bounded bridge contract in
`docs/architecture/prd2-workspace-credential-bridge-contract-2026-08-02.md` and keeps schema 19.
The implementation includes:

- explicit old-source gating and no fallback for an invalid present Workspace file;
- rejection of an unexplained pre-existing cluster-global `xinsight_workspace` role on a
  three-credential bridge path;
- read-only bridge-aware adopt/upgrade preflight;
- immutable successor staging with inherited DSN preservation and a generated Workspace secret;
- journaled, idempotent role/grant/symlink expansion;
- fail-closed abort semantics after the database barrier;
- resume from the exact source or successor set;
- direct rc.2 four-role → rc.3 upgrade without a bridge;
- corrected current four-role install/manual boundaries.

## Verification completed for the local candidate worktree

- focused three-role loader, immutable successor, exact rc.15 bridge, interruption/resume, legacy
  adoption and rc.2 no-bridge tests: PASS; focused race run: PASS (`18.705s`);
- exact rc.15 preflight proves no journal, role, file or symlink mutation, reports
  `workspace_bridge_required`, orders verified backup before boundary expansion, and rejects an
  unexplained existing Workspace role: PASS;
- exact rc.15 three-role → rc.3 upgrade, uninterrupted: PASS;
- exact rc.15 three-role → rc.3 interruption after boundary expansion and resume: PASS;
- post-barrier journal omits abort and explicit abort is rejected: PASS;
- exact Phase 1 legacy three-role adoption bridge: PASS;
- rc.2 four-role → rc.3 without credential-set change, plus rc.2 missing-Workspace fail-closed
  negative: PASS;
- full `internal/hostctl` suite on the final source: PASS (`192.644s`);
- repository-wide `go test -p 1 -race ./...` on the final source: PASS; host lifecycle package
  completed in `203.619s`;
- repository-wide `go vet ./...`: PASS;
- fixture conformance: PASS (`11/11`);
- release workflow script self-tests and shell syntax checks: PASS;
- Phase 2C single-writer Reference runtime gate: PASS after intentionally advancing only the
  reviewed `internal/hostctl/database.go` protected digest; Connector/Event/M0 protected surfaces
  and migration-tree identities remain unchanged;
- Node `22.23.1` / npm `10.9.8`: TypeScript, 36-path OpenAPI, 22 component tests, 177 license
  entries, production build and dependency audit: PASS; audit found `0` vulnerabilities;
- Chromium desktop/laptop/narrow Playwright suite: PASS (`15/15`); release-matched manual focused
  rerun after the final wording update: PASS (`3/3`);
- embedded manual rebuild is deterministic and the final source digest is
  `sha256:ccbec14949dc48e25bc57593e80a0a6dc3938e426771438d4cfd0c7fcec8ace9`;
  frontend dist has no tracked-byte drift.

GitHub PR CI, signed artifact construction, Target rc.3 preflight, Target credential/database
mutation, deployment, metadata-only acceptance, External integration, and Production proof are not
claimed by this local evidence record.

## Runtime boundary

This implementation/evidence step made no Target, credential, database, service, listener, security
group, traffic, Feed, Cursor, or Checkpoint mutation. The Target continues to run rc.15 with the
pre-existing trust store and workers stopped.
