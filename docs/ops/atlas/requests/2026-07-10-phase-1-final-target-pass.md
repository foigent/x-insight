# Phase 1 Final Target Pass

Status: Completed by Codex; Atlas assignment superseded by User
Date: 2026-07-10
From: X-Insight / Codex
To: Codex
Risk: R4 authorized non-production deployment and target verification

## Purpose

This checklist governed one combined final pass after Codex merged the Phase 1
release artifact:

1. correct the bounded infrastructure discrepancies from the first deployment;
2. deploy the exact CI-built artifact as a new immutable release;
3. run complete target acceptance and return sanitized evidence.

The User explicitly reassigned execution from Atlas to Codex. The completed,
reconciled result is recorded in the
[final target pass result](../responses/2026-07-10-phase-1-final-target-pass-result.md).

## Artifact Fields

| Field | Required value |
| --- | --- |
| Git commit | `b9e5811cc53a5cb06614956e66a1c54756617f0a` |
| GitHub Actions run | `29109235304` |
| Artifact filename | `xinsight-release-linux-amd64-b9e5811cc53a5cb06614956e66a1c54756617f0a.tar.gz` |
| Artifact SHA-256 | `sha256:3e3794338d690fe5b12de46600e17d455baeceab0b79300af260baf78d0346b1` |
| Manifest SHA-256 | `sha256:aaa53c2af46f59c328ff3271fa518a20bb0551fd7bdc02a0463ff925a43a6bc1` |

Stop if any field remains `PENDING`, the repository commit has not passed CI,
or the locally downloaded artifact does not match the recorded SHA-256.

## Authorized Scope

The User authorized Codex to operate the named X-Insight non-production ECS
host for X-Insight deployment and checking. This request does not authorize
production/customer data, raw prompt/response capture, public ingress, a target
GitHub credential, paid model/provider calls, disk replacement/deletion,
database dropping, down migrations, or broad cleanup.

## Part A: Correct Existing Host State

1. Verify Cloud Assistant before changes and confirm control-plane rollback for
   the dedicated security group remains available.
2. Change `/etc/x-insight/xinsight.env` to `root:root` mode `0600` without
   printing its content. Reload and restart X-Insight; verify readiness.
3. Enable the host firewall with default deny incoming and allow outgoing. Do
   not add public SSH or application rules. Verify Cloud Assistant before and
   after, and verify the effective listeners remain only
   `127.0.0.1:8080` and `127.0.0.1:5432` for X-Insight/PostgreSQL.
4. Reconcile `/etc/fstab` to `defaults,nofail,noatime`. To change effective
   mount options, stop X-Insight and its dedicated PostgreSQL container, remount
   `/data/x-insight` with `noatime`, then restart PostgreSQL and X-Insight.
   Verify database health, readiness, and existing fixture readback. Stop on any
   mount, data, or database uncertainty; do not reformat or repair destructively.
5. Record the first-deployment rollback procedure: on application failure stop
   and disable only X-Insight. Do not execute down migrations, drop databases,
   delete disks, or reformat storage. A destructive rollback test is not needed.

## Part B: Deploy Exact CI Artifact

1. Download the named GitHub Actions artifact on an authorized local operator
   machine; do not add GitHub credentials to the target.
2. Verify the artifact SHA-256 and manifest before transfer.
3. Transfer through Cloud Assistant. If size requires chunking, use the 8 KiB
   binary-part procedure in the project runbook.
4. Verify the reconstructed artifact and every component checksum before
   extraction.
5. Extract into `/opt/x-insight/releases/<full-commit>/`; verify it contains
   exactly the manifest, checksums, both binaries, migrations, schemas, and
   fixtures declared by CI.
6. Preserve the prior `/opt/x-insight/current` target. Run pre-activation checks,
   migrations twice, then switch the symlink atomically and restart the service.
7. On application failure, restore the prior symlink and restart. Never roll
   back a migration destructively.

## Part C: Acceptance

Run and record all requirements in the
[single-node target runbook](../../deployment/alibaba-cloud-single-node-target-runbook.md),
including:

- exact commit, artifact/manifest/component hashes, and toolchain identity;
- dedicated security-group membership with no inbound rules and active host
  firewall default-deny inbound;
- effective `noatime` mount and disk lifecycle/encryption/snapshot gaps;
- PostgreSQL immutable digest/local image ID, health, persistence, and loopback
  listener;
- first and idempotent second migration run;
- fixture conformance and approved synthetic persistence, duplicate/no-op, and
  execution/events/gaps readback;
- service restart with readiness/readback preserved;
- absence of secrets, raw payload, real Gate A data, customer data, external
  calls, public APIs, and unapproved dependencies.

## Result

The sanitized result is recorded at:

`docs/ops/atlas/responses/2026-07-10-phase-1-final-target-pass-result.md`

It includes commands by purpose, UTC timings, exit status, concise results, the
retained-log manifest SHA-256, and every discrepancy without raw IDs, IPs,
credentials, DSNs, secrets, or full successful logs.

Codex performed both operator execution and an explicit subsequent
cloud/target reconciliation before changing the Phase 1 status.
