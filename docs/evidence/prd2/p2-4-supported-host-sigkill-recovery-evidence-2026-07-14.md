# PRD2 P2-4 Supported-Host SIGKILL Recovery Evidence

Status: X-Insight-owned supported-profile crash/recovery and exact trial delivery
verified in CI; named Target, manual-guided User tasks and live UniRouter evidence
remain separate gates
Date: 2026-07-14
Execution owner: Codex
Implementation PRs: [#40](https://github.com/foigent/x-insight/pull/40),
[#41](https://github.com/foigent/x-insight/pull/41) and
[#42](https://github.com/foigent/x-insight/pull/42)
Qualified product commit: `a77ba1de2064d6b297c1010de708019e46fd61cb`

## Outcome And Claim Boundary

The release-relevant main run
[`29301064665`](https://github.com/foigent/x-insight/actions/runs/29301064665)
passed the accepted Ubuntu 22.04 x86_64/PostgreSQL 16 qualification after an
external process-group `SIGKILL` interrupted the exact signed `xinsightctl`
during `upgrade/stop_service`. The durable journal remained readable, no early
receipt existed, and `recover` plus `resume` completed the same operation and
restored readiness. Later rollback, re-upgrade and isolated restore steps also
passed, followed by fresh installation on a separate clean VM.

This closes the X-Insight-owned supported-profile abrupt-process-death sample.
It is CI evidence classified `ci_clean_vm_not_target`, not literal host power
loss, a customer-host run, `Target verified`, External integration, Production
or independent acceptance. The execution owner's Local and CI checks are
self-verification.

## Implemented Recovery Proof

- `scripts/ci-systemctl-sigkill-wrapper.sh` is a CI-only external wrapper. On an
  explicitly disposable GitHub-hosted Ubuntu runner, it completes the real
  service stop, atomically publishes a root-owned read-only PID marker, and
  blocks before the control process can record phase completion.
- `scripts/qualify-ubuntu22-host.sh` verifies the exact signed control binary,
  isolated PID/PGID/session and marker ownership/mode, then sends `SIGKILL` only
  to that process group. No crash hook is compiled into the product.
- The harness requires exit status 137, inspects the persisted operation before
  recovery, verifies the documented allowed actions, resumes the same operation
  ID, and confirms the receipt/readiness/final managed state.
- Durable phase tests now cover every operation family: 12 install, 7 adopt, 14
  upgrade, 9 application rollback and 11 restore phase boundaries, for 53
  recovery cases in total.
- Tests construct only a synthetic, allowlisted legacy artifact. They do not
  change the frozen Phase 1 artifact or evidence.

## Local Verification

| Check | Result |
|---|---|
| `go test ./...` | Pass; `internal/hostctl` completed in 70.398 seconds |
| `go vet ./...` | Pass |
| `actionlint` | Pass with v1.7.7 |
| Shell syntax and disposable-runner guards | Pass |
| CI path-router self-test | 9/9 cases pass |
| CI aggregate-gate self-test | 8/8 cases pass |
| Durable lifecycle phase cases | 53/53 pass across all five operation families |

## Pull Request And Main Execution Chain

| Change | PR evidence | Main evidence | Disposition |
|---|---|---|---|
| External SIGKILL proof and all-operation phase coverage | PR #40 commit `eab757fdbc18bde2dc7f17ca970570bdeb4f5025`; run `29300250955` passed | Merged `dcdc2c3ce14998cb4ff17ad46b1329f8340d9119`; run `29300398720` failed | The real kill/journal path was reached, but the root-created marker was mode `0600` and therefore unreadable by the runner harness. CI correctly blocked publication. |
| Atomic root-to-runner marker handoff | PR #41 commit `bda5b25a1aeddc438385093ae2592057b284b926`; run `29300603526` passed | Merged `e9afd27051c59fdb7f172acf7c660618d895c632`; run `29300748994` failed | Marker publication was fixed to root-owned mode `0444`. The expected `wait=137` then triggered the global shell `ERR` trap before recover/resume. CI again correctly blocked publication. |
| Expected SIGKILL wait-status capture | PR #42 commit `1073ce98d893de0ff34fbf478e8e01aa84dadcb6`; run `29300910033` passed | Merged `a77ba1de2064d6b297c1010de708019e46fd61cb`; run `29301064665` passed | The nonzero wait is captured inside an explicit conditional and asserted as 137 without bypassing unexpected errors. Full qualification completed. |

All three release-relevant PR runs used the optimized PR route: Frontend and
Backend/API passed, the two main-only release jobs skipped, and the protected
aggregate passed. The final PR run completed Frontend in 1 minute 18 seconds,
Backend/API in 2 minutes 24 seconds and the aggregate in 7 seconds.

The final exact-main run used the complementary route:

- Change Classification: success in 3 seconds;
- Frontend and Backend/API: skipped;
- Signed V2 Release Qualification: success in 2 minutes 8 seconds;
- Fresh Install Qualification: success in 57 seconds; and
- protected aggregate `Backend Tests`: success in 4 seconds.

These timings describe individual GitHub jobs and are not an SLO.

## Exact Crash And Resume Facts

| Field | Observed value |
|---|---|
| Operation ID | `20260714T022818Z-d85b405d049550f2` |
| Interrupted operation/phase | `upgrade` / `stop_service` |
| Completed phases before resume | `[]` |
| Allowed actions before resume | `recover`, `resume` |
| Killed process exit status | `137` |
| Early final receipt | absent |
| Resume result | `completed` using the same operation ID |
| Readiness after resume | true |
| Final managed state | true; no active journal |
| Final product/schema | `0.2.0-rc.1` / `11` |
| Host-result classification | `ci_clean_vm_not_target` |

The signed qualification artifact retained the exact executable evidence:

| Member | SHA-256 |
|---|---|
| `_temp/xinsight-host-qualification/sigkill-recover.json` | `08aea52959a56066cff39daea735d3d785eb3d38c0af8bedc92a45f07a65f3b5` |
| `_temp/xinsight-host-qualification/sigkill-recovery-result.json` | `d9d1acc72ed67c3531dbf07ef4b9eb697ca4bfebe889fec92153ed93ceac48ff` |
| `_temp/xinsight-host-qualification/upgrade-receipt.json` | `d92efd8c92222b9bd182f3b9bc428c7cfb2f356fcbb3702e280d50a46aed72e8` |
| `_temp/xinsight-host-qualification/qualification-result.json` | `df8e7596ad32d7e18f19b30c299844fbd62fec6fd99c29728d7c7402d78e27a7` |
| `_temp/xinsight-host-qualification/final-status.json` | `d6a629ed74adf0cf092771e68bdf1c67b2e282898891ea62e5c5cdbd86f3cebc` |

## Main Qualification Artifacts

| Artifact | Exact record |
|---|---|
| Signed/legacy/SIGKILL qualification | ID `8298562373`; `ci-non-publishable-xinsight-signed-v2-a77ba1de2064d6b297c1010de708019e46fd61cb`; API digest `sha256:36a421d5f93363010e3bf35ed2a875727daa600417a7c4aff2f1b8d5bb8b54cf`; 45,925,973 bytes; expires `2026-10-12T02:26:18Z` |
| Separate fresh-install qualification | ID `8298576091`; `ci-non-publishable-xinsight-fresh-install-a77ba1de2064d6b297c1010de708019e46fd61cb`; API digest `sha256:b6af7a592871e156e5f67d0393013571b7c1a0bb47b55ae524d0b8984e4c4176`; 9,514 bytes; expires `2026-10-12T02:26:18Z` |

Both are explicitly non-publishable CI artifacts.

## Exact Owner-Supervised Trial Delivery

While `a77ba1de2064d6b297c1010de708019e46fd61cb` was still exact current
`main`, manual workflow
[`29301384798`](https://github.com/foigent/x-insight/actions/runs/29301384798)
accepted source CI `29301064665`, rechecked all three required qualification
jobs and published the bounded first-customer delivery in 49 seconds.

| Field | Exact value |
|---|---|
| GitHub artifact ID | `8298646384` |
| Artifact name | `owner-supervised-trial-xinsight-a77ba1de2064d6b297c1010de708019e46fd61cb` |
| Artifact ZIP size | `31,549,525` bytes |
| Artifact API/ZIP digest | `sha256:3d6f4f19ae3b4c386399083b59088b7c14c362677a83c3b627982487f63206ee` |
| Created / expires | `2026-07-14T02:34:50Z` / `2026-10-12T02:34:01Z` |
| Product/manual | `0.2.0-rc.1` / `0.2.0-rc.1/manual-r1` |
| One-run public key ID | `ed25519:sha256:f6605237f7ef16d22d7568ed245b5042fb5ef64085057ffe0f292559ac7644d9` |
| Signed manifest / payload | `9aae785c7a8ecf3aea7fd114959199be9c101ea09098492ebe68ec0d5315d450` / `3611f1da18dd2358a1bb6f930ecb7c8fcf8bbcf48c94a06238e50d8f0fa38d79` |

The downloaded ZIP contained exactly seven root-level files. `SHA256SUMS`
covered the other six exactly:

| File | SHA-256 |
|---|---|
| `build-result.json` | `6548f4ee837082605eb36ef4a839d573fc0ea3cbef345389fdb88f48748ad228` |
| `owner-supervised-trial-release.json` | `ef1f7449876736ea273d81a88e1e13bef41c27640b1a60a0396fdeec139169cc` |
| `owner-supervised-trial.txt` | `3ccb97d46b6205a87dc6a344e5611e6d22240b371a011615a8d330be7b227344` |
| `trusted-release-keys.json` | `8acdbdf05024b2443093c0e2696636f64c556070903342663662ddee67689c8a` |
| `xinsight-release-linux-amd64-a77ba1de2064d6b297c1010de708019e46fd61cb` | `b7548891b2c9098e08d8d3f3a1dddb9327762c17cb0fba9c7ee535a816f6a193` |
| `xinsight-v0.2.0-rc.1-linux-amd64-a77ba1de2064d6b297c1010de708019e46fd61cb.tar.gz` | `485ac8bc06a2f75f9a3e54aa410eb691e4b3c30a884bc294d1cd9e5953231544` |

The release record states `private_key_retained=false`,
`long_lived_private_key=false`, `target_verified=false`,
`external_integration_verified=false` and `production_verified=false`.

Codex downloaded this artifact to the one explicit path
`/tmp/xinsight-owner-supervised-a77ba1de-29301384798.zip`, matched the ZIP
digest, tested the archive and seven-file boundary, inspected the metadata and
hash list, then deleted that single path and confirmed it absent. No recursive,
wildcard or batch deletion was used.

## P0 / P1 / P2 And Remaining Gates

- **Closed X-Insight P0:** supported-profile real process-death/recovery evidence
  now exists on the exact signed release. The two failed main runs exposed
  harness defects and correctly prevented a false green result.
- **Remaining P0 before customer-host mutation:** obtain the named Target host,
  access and maintenance-window facts; recheck the exact delivery and hashes on
  that host; preserve loopback-only/systemd/PostgreSQL 16
  boundaries; and authorize any sanitized customer metadata before use.
- **Remaining P1 for PRD2 field proof:** execute the manual-guided install,
  rollback/restore and investigation tasks with the User; record `N=1` results;
  and ingest one newly approved `UR-E` sidecar artifact or a jointly accepted
  completed-execution feed item.
- **P2/later:** literal host power-loss qualification, self-service upgrades,
  a durable commercial signing authority, HA/DR and multi-node/fleet recovery.

The next work is therefore external-gated rather than another X-Insight-only
feature batch: named customer Target facts/access, User field tasks, and either
an approved UniRouter sidecar artifact or the jointly accepted live feed plus
integration window.

No new verified Phase 1 P0/P1 was found, so Phase 1 remains closed. The two
temporary Phase 1 snapshots remain governed by their separate 2026-07-17
exact-path cleanup obligation and were not touched.

## Governance Correction — 2026-07-19

The preceding `UR-E`/feed wording is retained as the original 2026-07-14
checkpoint. It described future X-Insight inputs, not an accepted UniRouter
obligation. UniRouter had not accepted artifact generation or delivery; the
separate `UR-E` request is Open and non-binding. The completed-execution live
feed was later deferred as a whole. See
[UR-E Cross-Project Governance Correction](ur-e-cross-project-governance-correction-2026-07-19.md).
