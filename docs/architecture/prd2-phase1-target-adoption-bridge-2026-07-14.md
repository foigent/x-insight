# PRD2 Phase 1 Target Adoption Bridge

- Status: bridge completed by the later rc.5 lifecycle proof and retained by
  the exact rc.6 named-Target refresh. The `0.2.0-rc.2` restrictive-umask
  failure and then-pending rc.3 gates below remain historical evidence, not
  current status
- Status reconciliation: 2026-07-19
- Owner: X-Insight
- Scope: first-customer, owner-supervised, exact final Phase 1 single-node Target

## Decision

The canonical PostgreSQL deployment identity is:

| Field | Required value |
| --- | --- |
| Image | `postgres:16.14-alpine3.24` at the release-pinned digest |
| Host data root | `/data/x-insight/postgres` |
| Container data root / `PGDATA` | `/var/lib/postgresql/data` |
| Container name | `xinsight-postgres` |
| Listener | `127.0.0.1:5432` only |
| Managed bootstrap role | `xinsight_admin` |
| Runtime roles | `xinsight_reader`, `xinsight_importer` |
| Managed Compose | `/etc/x-insight/postgres.compose.yml` |
| Managed environment | `/etc/x-insight/postgres.env`, root-owned `0600` |

This chooses the project Target runbook and the actual final Phase 1 Target over
the rejected `0.2.0-rc.1` alternate `/var/lib/x-insight/postgres` plus nested
`pgdata/` layout. Existing database files are not moved or copied.

## Why This Is The Minimum Safe Bridge

- Pointing a new Compose file at another host path or nested `PGDATA` can appear
  healthy while serving an empty database. Readiness alone cannot distinguish
  that from a valid migration without evidence/readback checks.
- Reusing legacy `xinsight` as every managed role would defeat reader/importer
  least privilege, backup/restore identity and future auditability.
- Demoting the old superuser during the same window would break the exact
  Phase 1 application/container rollback path. It remains bounded legacy debt,
  not the new steady-state runtime credential.
- Rebuilding the PostgreSQL container is not required for the application
  adoption. Staging compatible managed Compose now allows a later, separate
  maintenance operation after the application rollback window.

## Ordered State Transition

```text
exact Phase 1 + legacy xinsight role + old Compose
  -> verify identity/listeners/data root
  -> normalize only verified legacy migration path if needed
  -> install PG16 clients
  -> custom backup + SHA-256 + pg_restore list
  -> create root-only versioned credential files and postgres.env
  -> create/verify xinsight_admin through the verified legacy superuser
  -> adopt --check
  -> adopt (journal creates/hardens reader/importer and stops legacy service)
  -> upgrade --check
  -> upgrade (backup, schema 9->11, runtime/config/Compose activation)
  -> readiness/UI/manual/evidence/restart/rollback checks
```

At no point may old and new PostgreSQL containers run simultaneously against
the same data root.

## Hard Stops

- current release, legacy archive, manifest or installed file set differs;
- PostgreSQL is not exact major 16 or the image/data root/listener is uncertain;
- raw/customer data is present on the unencrypted trial disk;
- legacy environment contains anything other than the accepted two-key layout;
- backup hash or `pg_restore --list` fails;
- `xinsight_admin` already exists with unknown identity;
- any credential is printed, passed as a process argument or copied to Git;
- managed Compose does not match the canonical table above;
- `adopt --check` is not fully supported;
- an incomplete journal requires `recover`/`resume`/bounded `abort`.

## Compatibility And Rollback

- Application rollback remains pointer-only; no down migration is run.
- Keep the old release, old Compose, password-file secret, legacy superuser and
  all backups throughout the declared application rollback window.
- The managed application uses reader/importer/admin credential files after
  upgrade. The still-running legacy PostgreSQL container may retain its old
  initialization environment because initialized data is authoritative.
- Recreating the database container from managed Compose, demoting the old
  superuser and removing old secret/config files are later maintenance batches.
  Each requires exact paths, a fresh backup and separate evidence; none is
  implied by Target application acceptance.

## Acceptance Matrix At The RC.2 Failure Checkpoint

This table intentionally preserves the state observed when rc.2 failed. The
later passing rc.5 lifecycle and rc.6 refresh are append-only follow-up facts;
see the
[current rc.6 named-Target evidence](../evidence/prd2/p2-4-rc6-named-target-v2-sidecar-and-ui-evidence-2026-07-19.md).

| ID | Required executable evidence | Status at that checkpoint |
| --- | --- | --- |
| BR-A01 | deploy Compose declares canonical image, name, host root, root `PGDATA` and loopback port | Passed exact-main CI `29335953131` |
| BR-A02 | host lifecycle path reports `/data/x-insight/postgres` | Passed exact-main CI `29335953131` |
| BR-A03 | fresh Ubuntu qualification installs on canonical root | Passed exact-main CI `29335953131` |
| BR-A04 | legacy qualification starts old `xinsight` + password-file shape, creates backed-up admin bridge, then completes adopt/upgrade | Passed exact-main CI `29335953131` |
| BR-A05 | rollback/re-upgrade/isolated restore preserve representative evidence | Passed exact-main CI `29335953131` |
| BR-A06 | exact-main owner-supervised corrected delivery passes strict seven-file publication | `0.2.0-rc.2` workflow `29336389137` passed; new `0.2.0-rc.3` delivery pending |
| BR-A07 | named Target re-verifies artifact and completes adopt/upgrade without alternate/empty cluster | credential bridge/adopt passed; rc.2 upgrade failed before migration on inherited `0700/0600` release modes, then abort restored Phase 1; rc.3 rerun pending |
| BR-A08 | named Target UI/manual/restart/listener/fixture readback pass | Pending |

No row promotes External integration or Production.

## Restrictive-Umask Target Finding

The named Target runs owner commands with `umask 077`. `0.2.0-rc.2` correctly
verified before mutation and completed the credential bridge plus durable
legacy adoption, but its upgrade-created release tree inherited `0700/0600`.
`VerifyExtractedPayload` rejected that tree during `stage_release`; no backup,
database migration or activation occurred. Bounded `abort` restored role login
state and the exact Phase 1 service. The failed release tree, journal and
receipts remain preserved.

`0.2.0-rc.3` made payload extraction and newly written lifecycle/runtime files
set their contract modes explicitly. At that checkpoint, a process-level
`umask 077` regression had to pass locally, in exact-main qualification and on
the named Target before BR-A07 or BR-A08 could close. Their later closure is
referenced above rather than rewriting this failure checkpoint.

## Non-Goals

- no PostgreSQL major upgrade, HA/DR, online dual-cluster migration or zero
  downtime;
- no automatic deletion, directory move or recursive ownership change;
- no customer self-upgrade, retained trial signing key or second-customer
  release authority;
- no UniRouter runtime change and no live-feed claim.
