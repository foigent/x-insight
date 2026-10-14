# Phase 1 Final Target Pass Result

Status: Target verified; Phase 1 reconciliation complete
Date: 2026-07-10
Operator: Codex
Execution owner: Codex
Target: named X-Insight non-production ECS host

## Ownership

The User explicitly reassigned the remaining target work from Atlas to Codex.
Codex used the prepared final-pass request as the authorized checklist and
performed the target mutation, acceptance, and independent reconciliation. No
Atlas result or project-status promotion is implied.

## Exact Release

| Field | Verified value |
| --- | --- |
| Git commit | `b9e5811cc53a5cb06614956e66a1c54756617f0a` |
| GitHub Actions run | `29109235304` |
| Artifact filename | `xinsight-release-linux-amd64-b9e5811cc53a5cb06614956e66a1c54756617f0a.tar.gz` |
| Actions wrapper SHA-256 | `sha256:c1acdf6b4f5a755322f0ae75f70051dea584421684fecc66db37856394ddc8f2` |
| Artifact SHA-256 | `sha256:3e3794338d690fe5b12de46600e17d455baeceab0b79300af260baf78d0346b1` |
| Manifest SHA-256 | `sha256:aaa53c2af46f59c328ff3271fa518a20bb0551fd7bdc02a0463ff925a43a6bc1` |
| Go toolchain | `go1.25.11` |
| Target | Linux/amd64, static, CGO disabled |
| Server binary SHA-256 | `541d9890ebdcfce2ab5aadd61cabebb41c344384d95b05632617e3f5fa085c6a` |
| Importer binary SHA-256 | `950ce9d1e01423a47f64582afe7485aab6cda4cd920d7edcdf8e4d89f7da0ff0` |

The downloaded Actions wrapper matched the GitHub-reported digest. Local and
target verification confirmed the exact commit, toolchain, 52-file content
set, 51 component checksums, 11 explicit release directory entries, normalized
root ownership, modes, commit-timestamp mtimes, and forbidden-content checks.

## Execution Timeline

| UTC | Purpose | Exit/result |
| --- | --- | --- |
| 15:57 | Read-only target, service, database, listener, migration, and synthetic readback preflight | PASS |
| 15:58 | Environment-file correction and immediate readiness probe | Probe exited 7 before startup settled; recovery restored readiness; file correction remained valid |
| 15:59 | UFW default-deny inbound/default-allow outbound plus post-change Cloud Assistant probe | PASS |
| 16:00 | Bounded X-Insight/PostgreSQL stop, effective `noatime` remount, and recovery | PASS |
| 16:18 | Original artifact transfer/reconstruction | Hashes passed; activation stopped when floating Go patch selection was identified |
| 16:24 | PR #16 main CI with exact Go `1.25.11` | PASS, run `29107272996` |
| 16:46 | First exact-toolchain activation | Failed on source-build unit's old root-binary path; old symlink restored, readiness 200 |
| 16:49 | Unit-layout drop-in activation | Failed because file-only tar extraction created `0700` directories; drop-in disabled, old symlink restored, readiness 200 |
| 16:57 | PR #17 main CI with explicit directory metadata | PASS, run `29109235304` |
| 17:17 | Final artifact reconstruction, metadata/component verification, fixtures, and two migration runs | PASS |
| 17:19 | Final systemd drop-in, atomic symlink activation, restart, and readback | PASS |
| 17:21 | Full acceptance command | Stopped before persistence because importer was launched outside the release working directory |
| 17:22 | Corrected synthetic persistence, duplicate/no-op, API readback, and restart | PASS |
| 17:23 | Independent cloud/target reconciliation and log-manifest verification | PASS |

The failed attempts did not run down migrations, drop a database, delete or
reformat storage, or leave a failed release active.

## Final Target State

| Area | Reconciled result |
| --- | --- |
| Compute | Approved non-production 4 vCPU, 8 GiB Ubuntu 22.04 target |
| Cloud access | Cloud Assistant active before and after changes |
| Network | One dedicated security group with zero rules; UFW active with default-deny inbound; X-Insight and PostgreSQL listen only on their loopback ports |
| Secret file | `/etc/x-insight/xinsight.env` is `root:root` mode `0600`; content was never printed |
| Data disk | 50 GiB ext4, approximately 1% used, UUID-backed fstab entry, effective `noatime` |
| Storage boundary | Unencrypted, delete-with-instance, and no automated snapshot policy; fixtures/synthetic metadata only |
| PostgreSQL | PostgreSQL 16 Alpine immutable digest and local image ID reconciled; healthy, restart-enabled, data under the dedicated data disk, loopback-only |
| Migrations | Nine versions present; both final artifact migration runs applied zero |
| Release | `/opt/x-insight/current` points to the exact final commit; source-build release remains available for symlink rollback |
| Service | Non-login `xinsight` identity, systemd hardening active, release-layout drop-in active, health/readiness 200 |
| Synthetic loop | New approved fixture inserted 2 events and 1 gap; rerun produced 2 duplicate/no-op outcomes |
| Readback | New request reads 2 events/1 gap; prior authorization-denied request remains 2 events/2 gaps |
| Restart | Readiness and both readback paths remained correct after restart |

## Logs And Cleanup

Full command logs remain root-owned mode `0600` under:

`/var/log/x-insight/deployments/20260710T155638Z-codex-phase1-final/`

The directory is mode `0700`. Its sorted 12-file checksum manifest is
`LOG-SHA256SUMS`, SHA-256
`37b9d81679c74ad0ac4e8b0d5e11d5f85319c4ea2f0f07289f09ed2615398690`.

The prior release, two rejected release/staging attempts, final transfer parts,
UFW backups, and disabled failed drop-in are retained. No bulk cleanup was
performed. They consume negligible capacity at the current approximately 1%
data-disk use and preserve the audit/rollback trail.

## Boundary Confirmation

No credential, DSN, raw prompt/response, real Gate A artifact, customer data,
public API, target GitHub credential, paid provider call, or unapproved runtime
dependency was introduced. The target remains fixture/synthetic-data only.

## Judgment

Every applicable requirement in the current single-node target runbook is
satisfied. The valid project claim is `Target verified`. This result does not
support `Production verified`: authentication/RBAC, retention/deletion/audit,
encrypted storage, snapshot/restore proof, HA/DR, and public API security remain
outside Phase 1.
