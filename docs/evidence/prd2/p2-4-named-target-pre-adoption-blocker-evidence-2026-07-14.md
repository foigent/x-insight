# P2-4 Named Target Pre-Adoption Blocker Evidence

- Date: 2026-07-14
- Owner: Codex
- Classification: execution-owner Target preflight and bounded preparation;
  not independent acceptance
- Target: named non-production Alibaba Cloud ECS, resource identifiers sanitized

## Outcome First

The first `0.2.0-rc.1` owner-supervised delivery passed Target-side transfer,
hash, Ed25519 signature, payload, binary, schema, manual and exact Phase 1
legacy-artifact checks. It was **not adopted or activated**.

`xinsightctl adopt --check` and subsequent storage inspection exposed a P1
compatibility gap between the signed candidate and the actual final Phase 1
Target: the active PostgreSQL 16.14 cluster is rooted at
`/data/x-insight/postgres` with `PGDATA=/var/lib/postgresql/data`, while the
candidate Compose would use `/var/lib/x-insight/postgres` with nested
`PGDATA=/var/lib/postgresql/data/pgdata`. Recreating PostgreSQL from that
candidate could initialize an empty cluster. Execution stopped before database
role creation, service stop, lifecycle adoption or migration.

## Exact Source Identities

| Item | Exact value |
| --- | --- |
| Project `main` at preflight | `713bce9a900a00df645a056d497b5aecdaf76126` |
| Product-bearing release source | `a77ba1de2064d6b297c1010de708019e46fd61cb` |
| Source CI | `29301064665`, success |
| Owner-supervised workflow | `29301384798`, success |
| Owner delivery artifact | `8298646384` |
| Owner Actions ZIP SHA-256 | `3d6f4f19ae3b4c386399083b59088b7c14c362677a83c3b627982487f63206ee` |
| Signed bundle SHA-256 | `485ac8bc06a2f75f9a3e54aa410eb691e4b3c30a884bc294d1cd9e5953231544` |
| Bootstrap SHA-256 | `b7548891b2c9098e08d8d3f3a1dddb9327762c17cb0fba9c7ee535a816f6a193` |
| Public trust-store SHA-256 | `8acdbdf05024b2443093c0e2696636f64c556070903342663662ddee67689c8a` |
| Trust key ID | `ed25519:sha256:f6605237f7ef16d22d7568ed245b5042fb5ef64085057ffe0f292559ac7644d9` |
| Exact legacy artifact | `b9e5811cc53a5cb06614956e66a1c54756617f0a` |
| Legacy artifact SHA-256 | `3e3794338d690fe5b12de46600e17d455baeceab0b79300af260baf78d0346b1` |
| Legacy Actions ZIP SHA-256 | `c1acdf6b4f5a755322f0ae75f70051dea584421684fecc66db37856394ddc8f2` |

The two Actions artifacts were fetched outbound through short-lived scoped
download URLs executed by Cloud Assistant. No security-group ingress rule,
standing SSH access, GitHub PAT or deploy key was added to the Target.

## Target-Side Verification

| Gate | Result | Evidence summary |
| --- | --- | --- |
| Strict owner delivery set | Pass | exactly seven files; all `SHA256SUMS` entries pass |
| Actions ZIP identity | Pass | both downloaded ZIP hashes match GitHub artifact API digests |
| Bootstrap verification | Pass | Linux amd64, commit `a77ba1de...`, version `0.2.0-rc.1` |
| Signed extraction | Pass on a new destination | payload modes `0755/0644`; failed restrictive-umask destination retained |
| Runtime identities | Pass | four payload binaries plus external bootstrap match product/manual/schema |
| Legacy allowlist | Pass | exact final Phase 1 archive, manifest and schema-9 migration prefix |
| Chinese manual | Pass | `0.2.0-rc.1/manual-r1` present in the signed payload |

The first extraction attempt inherited operator-set `umask 077`; the verifier
correctly rejected the resulting `0700/0600` tree. A second new destination was
extracted with the release-required modes. A later wrapper check incorrectly
looked for the external bootstrap inside the four-binary payload; that wrapper
error did not invalidate the successful signed extraction. The restrictive-mode
destination and the later verified staging paths remain preserved; no deletion
was used to conceal either failed attempt.

## Fail-Closed Adoption Findings

1. The exact two-key legacy `/etc/x-insight/xinsight.env` was mode `0600` and
   root-owned, but its migration directory referenced old release
   `4e188350...` while current is final Phase 1 `b9e5811...`.
2. After exact identity/readiness checks, only that path was atomically
   normalized to `/opt/x-insight/current/migrations`. The before SHA-256 was
   `d6d50f6ea97f46a0adb5fbf238794cef83dc690b35647b75375192b5c83fd9cf`;
   the after SHA-256 is
   `83247b99629c96169df4da5a9d38af0401ded89ad24a9ffc6143bbdad70b5ddd`.
   The running process was not restarted and `/readyz` remained HTTP 200.
3. The next `adopt --check` stopped because
   `/etc/x-insight/database/current` did not exist. No credential directory or
   database role was created.
4. Non-secret DSN inspection confirmed the actual Phase 1 admin is legacy
   `xinsight` on `127.0.0.1:5432/xinsight`; Target SQL inspection confirmed it
   is the current superuser. Managed PRD2 requires `xinsight_admin`,
   `xinsight_reader` and `xinsight_importer`.

## Bounded Preparation Completed

- Installed host PostgreSQL client `16.14-1.pgdg22.04+1` from the official PGDG
  Jammy repository. The downloaded repository key fingerprint is
  `B97B0AFCAA1A47F044F244A07FCC7D46ACCC4CF8`; `pg_dump`, `pg_restore`,
  `createdb` and `psql` all report 16.14.
- Verified the running database as PostgreSQL `16.14`, approximately 8.7 MB,
  nine public tables, loopback-only and backed by the exact expected image ID
  `sha256:57c72fd2a128e416c7fcc499958864df5301e940bca0a56f58fddf30ffc07777`.
- Created owner-only custom-format backup
  `/data/x-insight/backups/xinsight-pre-prd2-admin-bridge-20260714.dump` before
  any role bridge. SHA-256 is
  `8e40fd408825ee6af6842c3b22551d7295c9ed9ac1f9e629dd908b8f4edfd054`,
  size is 35,167 bytes, and `pg_restore --list` returned 71 archive entries.
- Security-group ingress remains empty; X-Insight and PostgreSQL remain on
  `127.0.0.1:8080` and `127.0.0.1:5432`.

These preparation actions do not make the Target verified. The disk remains
unencrypted and the run remains fixture/synthetic-only.

## P1 Storage Mismatch

| Dimension | Active final Phase 1 Target | Signed `0.2.0-rc.1` candidate | Risk |
| --- | --- | --- | --- |
| Host source | `/data/x-insight/postgres` | `/var/lib/x-insight/postgres` | candidate points away from the real cluster |
| Container target | `/var/lib/postgresql/data` | `/var/lib/postgresql/data` | same mount point alone is insufficient |
| `PGDATA` | `/var/lib/postgresql/data` | `/var/lib/postgresql/data/pgdata` | candidate can initialize a new empty subdirectory |
| Container name | `xinsight-postgres` | Compose-generated | future supervised handoff is ambiguous |
| Initial DB role | `xinsight` + password file | `xinsight_admin` + env file | requires explicit backed-up credential bridge |

- P0: none observed.
- P1: candidate storage/PGDATA mismatch and unqualified real legacy role bridge.
- P2: the legacy `xinsight` superuser and old Compose/secret file remain until
  the application rollback window closes; later demotion/cleanup is a separate
  exact-path maintenance decision.

## Disposition And Resume Gate

`0.2.0-rc.1` is rejected for this Target and gains no Target status. The
corrective candidate must:

1. use `/data/x-insight/postgres`, root
   `PGDATA=/var/lib/postgresql/data` and `container_name: xinsight-postgres`;
2. qualify the actual legacy `xinsight`/password-file/data-root shape through a
   backed-up three-role bridge, adopt, upgrade, rollback, re-upgrade and restore;
3. publish a new version/commit/trust store through exact-main CI and the
   owner-supervised workflow;
4. pass a fresh Target-side seven-file/hash/signature/preflight sequence before
   database role creation or `adopt` resumes.
