# First ECS Deployment Reconciliation

Status: Historical first-deployment reconciliation; superseded by final target result
Date: 2026-07-10
Operator: Atlas
Execution owner reconciliation: Codex
Target: named X-Insight non-production ECS host

## Result

Atlas completed the first X-Insight deployment through the approved private
management path. The service is operational and the main functional checks
passed. The deployment is not yet promoted to `Target verified` because several
runbook requirements require correction or stronger evidence.

No raw cloud resource ID, IP address, credential, DSN, or secret is recorded in
this project report.

## Reconciled Facts

| Area | Confirmed result |
| --- | --- |
| Network | A dedicated security group is attached, the inherited broad group is detached, and cloud control-plane readback shows zero inbound rules. Cloud Assistant remained available. |
| Compute | The target matches the 4 vCPU, 8 GiB, Ubuntu 22.04 non-production baseline. |
| Data disk | A 50 GiB ext4 data disk is mounted at `/data/x-insight`; use is approximately 1%. The disk is unencrypted, deletes with the instance, and has no snapshot policy, so fixture/synthetic data only is allowed. |
| PostgreSQL | PostgreSQL 16 is healthy, persists under the data disk, and publishes only `127.0.0.1:5432`. The reported immutable digest and local image identity were independently reconciled. |
| Source provenance | Exact source commit `4e1883503084a31cf685f924660a416d2871adfb` was transferred in verified chunks and built with Go `1.25.12`. |
| Binaries | `xinsight-server` SHA-256 `95ea4030331f0a566d4e094211d91cebd4683a3b62b3010c857bedbf4b0e7129`; `xinsight-importer` SHA-256 `0cfa9ca61192ac999028472211f81316bfbdcec2eaba7e72bc959b14cdb85d62`. |
| Migrations | First run applied migrations 000001 through 000009; the second run applied zero. Database readback shows nine migration rows. |
| Runtime | `xinsight.service` runs as the non-login `xinsight` user with the required systemd hardening and binds only `127.0.0.1:8080`. |
| Health | `/healthz` and `/readyz` return the expected healthy/ready results. |
| Synthetic evidence | The approved fixture persisted two events and two capture gaps; rerun produced two duplicate/no-op outcomes; execution readback returned the same counts. |
| Restart | Service restart preserved readiness and readback. |
| Logs | Three deployment log files are retained on target. Reconciled directory digest: `32e0f9234fe4ec68b90fbc6fc4f8ec8a223660e4284360ed3a2efbe32a514ea5`. |
| Boundary | No customer data, raw prompt/response, real Gate A artifact, UniRouter database access, external provider call, public API, or additional runtime dependency was introduced. |

## Transfer Evidence

The exact source archive was divided into 48 binary parts of 8192 bytes. All 48
transfer results succeeded on the first attempt, and the reconstructed archive
SHA-256 matched the approved source archive. The full manifest remains on the
target; the project report does not independently attest every part hash because
the operator summary did not enumerate those checks. No public listener, SSH
rule, target GitHub credential, OSS service, or paid transfer service was
retained.

This validates the bounded Cloud Assistant chunking procedure, but it remains a
source-build fallback. Phase 1 requires replacing it with a CI-built,
hash-addressed release artifact.

## Acceptance Gaps

| Gap | Evidence | Required correction |
| --- | --- | --- |
| Host firewall | Independent target check reports UFW inactive. | Enable default-deny inbound and allow outbound; verify Cloud Assistant, loopback listeners, health, and database readiness before and after. |
| Environment file mode | `/etc/x-insight/xinsight.env` is `root:xinsight` mode `0640`; the runbook requires root-owned mode `0600`. | Change to `root:root` mode `0600`, daemon-reload/restart, and verify readiness without displaying content. |
| Effective mount option | The operator reported `noatime`, while effective `findmnt` output is `rw,relatime`. | Reconcile `/etc/fstab`, safely remount with `noatime`, and prove the effective option after service/database recovery. |
| Operator report completeness | The original report omitted the actual retained-log digest. | Use the reconciled digest above and include the final-pass digest directly. |
| First-deployment rollback | Restart was tested, but the first-deployment rollback behavior was not explicitly evidenced. | Record that rollback is stop/disable only X-Insight, with no down migration, database drop, disk deletion, or storage reformat. A destructive rollback test is not required. |
| Release reproducibility | The source archive was built on target. | Deploy the exact CI-built Phase 1 artifact after Codex publishes it and verify its manifest and component hashes. |

## Status Judgment

Atlas's Phase A-E execution is accepted as completed work. The phrase "all
phases passed" is not accepted as final project status because the runbook and
effective target state differ in the listed areas.

The prepared
[Phase 1 Final Target Pass](../requests/2026-07-10-phase-1-final-target-pass.md)
was subsequently completed by Codex after the User reassigned operator
ownership. The current project claim is recorded in the
[final target result](2026-07-10-phase-1-final-target-pass-result.md); this file
remains the historical first-deployment record.
