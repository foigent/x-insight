# P2-4 RC.5 Named Target Lifecycle And UI Evidence

- Date: 2026-07-14
- Execution owner: Codex
- Classification: execution-owner Target operation and browser evidence; not
  independent acceptance
- Target: named non-production Alibaba Cloud ECS; resource identifiers are not
  committed to this public project record
- Exact product source: `27e05df26ff7976301a245f9120176e6ca70430d`
- Product/manual: `0.2.0-rc.5` / `0.2.0-rc.5/manual-r5`

## Outcome First

The exact owner-supervised `0.2.0-rc.5` release passed strict publication,
Target-side verification, upgrade, application rollback, re-upgrade, explicit
service restart and real browser investigation on the named non-production
host. X-Insight and PostgreSQL remain loopback-only. No security-group ingress
rule was added.

This promotes the exact rc.5 artifact to **Target verified** only for the
accepted owner-supervised, single-host, loopback and approved-metadata scope.
It does not promote External integration, the preferred live UniRouter feed,
field usability, Production, customer self-upgrade or a durable commercial
publisher identity.

The published `owner-supervised-trial-release.json` correctly records
`target_verified=false`, because publication occurred before Target execution.
That signed seven-file delivery is immutable. This append-only post-release
record supplies the later Target result instead of rewriting the package or
its release-time claims.

## Corrective Chain

The first three named-Target candidates each failed closed and were preserved:

1. rc.1 exposed the actual Phase 1 PostgreSQL data-root and legacy-role bridge
   mismatch before adoption.
2. rc.2 completed adoption but exposed process-`umask 077` mode inheritance at
   `stage_release`; bounded abort restored Phase 1 before migration or
   activation.
3. rc.4, from main `447ca00`, reached only read-only upgrade preflight. Its new
   verifier incorrectly required the later managed-launcher drop-in inside the
   exact historical rc.3 previous bundle. No operation, backup, migration or
   activation was created, and the Target remained on stable Phase 1.

PR [#49](https://github.com/foigent/x-insight-private-history/pull/49) fixed the rc.4 P1 by
binding required bundle entries to the signed manifest's product version. Only
exact historical `0.2.0-rc.3` may omit the later managed-launcher drop-in;
current and unknown versions fail closed. Historical pack/verify/extract,
current/unknown rejection and host-lifecycle rollback tests cover the rule.
The fix merged as `27e05df26ff7976301a245f9120176e6ca70430d`.

This is a Phase 2 release-compatibility P1 and does not reopen Phase 1.

## Exact CI And Delivery Identity

| Item | Exact value |
| --- | --- |
| PR #49 branch commit | `c8aa4ce81f95502a63f991844ab6c64ff59bb02e` |
| Merged main | `27e05df26ff7976301a245f9120176e6ca70430d` |
| Exact-main CI | [29348263612](https://github.com/foigent/x-insight-private-history/actions/runs/29348263612), success |
| Owner-supervised workflow | [29348552251](https://github.com/foigent/x-insight-private-history/actions/runs/29348552251), success |
| GitHub artifact ID | `8317137184` |
| Signed bundle SHA-256 | `7e951542cf96fc2dbb3d71ccfe5c50fabadf4e1fd0fa8760fe973fd81279afa4` |
| Signed manifest SHA-256 | `fa252cf75721f7c0ec670c8f662427d6c39c38ac2e5963550fae10b8a2d459c2` |
| Signed payload SHA-256 | `9ca13dc5eff092f4559fe48366840b5f37c56866f796ac638cf55612e1d16f76` |
| Bootstrap SHA-256 | `c0f0bf896ba900aba64974de6671b0d7ffea81bbf6a7587d876a08f8cb46d64e` |
| Public trust-store SHA-256 | `75bbcd8dcb977aac47fded3ef8fb62a36fa695a914e274e08237f51f8bac8fa0` |
| One-run key ID | `ed25519:sha256:460283e3871a5016c0a92677aad01ec90a0d5cc792812cde0f8f352b7169edfa` |
| `SHA256SUMS` SHA-256 | `fd1191fb4645faa72d71ea335803181de91634e754f274fba307cc2f6b32e943` |
| Owner metadata SHA-256 | `f9d5838b9b2389959a61e807057012cbb3be81857f2d7ea7812e0fa968038070` |

The downloaded delivery contained exactly seven allowed root files. All
checksums passed. The one-run private key was absent and was not retained. Main
CI used the optimized main route: signed qualification and fresh install ran;
frontend/backend source jobs correctly skipped. The manual workflow accepted
only the exact successful main push run.

## Target Trust Transition And Preflight

The Target began on exact Phase 1
`b9e5811cc53a5cb06614956e66a1c54756617f0a`, with exact rc.3 as the preserved
previous bundle. A bounded transition trust store contained only the rc.3 and
rc.5 public keys. The rc.5 verifier successfully verified both exact bundles,
directly exercising the historical-compatibility correction before mutation.

`upgrade --check` then passed all nine checks. The active trust store was
narrowed to the rc.5-only public key after the verified rollback window. The
prior rc.3-only and transition stores remain preserved under explicit root-only
paths; they were not deleted or used as standing future release authority.

## Upgrade, Rollback And Re-Upgrade

| Stage | Exact operation | Result |
| --- | --- | --- |
| Upgrade | `20260714T162253Z-0d50273196ab1db4` | Completed; schema 11, rc.5/manual-r5 active, backup and all activation proofs passed |
| Application rollback | `20260714T162401Z-56fbcf9f2fae84ba` | Completed; Phase 1 reactivated, no down migration executed, evidence fingerprint preserved |
| Re-upgrade | `20260714T162511Z-f3fe1ef845a7bf5d` | Completed; rc.5/manual-r5 active again, all proofs passed |

The final re-upgrade journal SHA-256 is
`6aa69a273d5ed31dac99c3139f644a0be30308d82a4e5d82bb485b7496f1722e`;
the final receipt SHA-256 is
`2ea22e2c7271d3fa6d8a69db6d1ad8cad81e19f33bb7c0343a6ea79d5ce22115`.
The persisted evidence-integrity fingerprint remained
`3842d9a42f1981c3578c444fdba160f20d5a5cc8f5a84a136242ef3cf1bf5666`
through upgrade, rollback and re-upgrade.

The PostgreSQL 16 container ID and `StartedAt` value remained unchanged across
the complete window. X-Insight and PostgreSQL continued to listen only on
`127.0.0.1:8080` and `127.0.0.1:5432`. The final service was explicitly
restarted and returned rc.5 readiness with database schema 11 and manual-r5.

## Investigation API And Browser Proof

The final fixed-snapshot API proof passed bounded search, cursor behavior,
detail, evidence-derived timeline, selected event, source/provenance, capture
gaps, stable problem responses and no-raw response checks.

The first Alibaba Cloud Session Manager CLI forward was reached by the browser
through `127.0.0.1:18080`; Session Manager was enabled at the account
management-plane level because it had been disabled, and no inbound
security-group rule was added. A later socket-scope check found that the tested
macOS CLI had actually bound `*:<local-port>`, so the direct CLI session was
stopped and is not approved for continued use. A corrected two-layer path then
passed: `ali-instance-cli ssh` acted only as the Session Manager stdio proxy,
while OpenSSH explicitly bound
`127.0.0.1:18080 -> Target 127.0.0.1:8080`. Socket inspection showed only the
OpenSSH loopback listener, readiness and UI HTML passed, and the verification
tunnel was stopped. See the append-only
[Target access listener correction](p2-4-target-access-listener-correction-2026-07-14.md).

The real browser verification completed during the first bounded session and
proved the actual embedded Target UI rather than a source or mocked browser
test:

- the default recent-24-hour search truthfully returned an empty state because
  the retained examples are older;
- a bounded search from local time `2026-07-09T00:00` returned two executions,
  including `req-auth-denied-001` and `req-gap-001`;
- selecting `req-auth-denied-001` preserved the search `as_of` and opened its
  detail route;
- two persisted evidence events rendered in deterministic chronology;
- selecting each node changed the allowlisted node detail and field-group
  provenance without creating a new fact source;
- source type/version/ref/hash/schema/fidelity rendered for the selected event;
- two capture gaps rendered independently, including confirmed
  `permission_denied` and `not_available_from_source` evidence;
- `direct`, `derived`, `declared`, `suppressed`, `gap`, `unknown` and
  `not_applicable` remained visible text plus canonical codes; and
- `/manual/zh-CN/` rendered the same-origin rc.5/manual-r5 offline manual.

The retained historical sidecar execution correctly remains
`proof_class=unclassified`, `assurance=unclassified`, `sync_state=gap` and
`timeline_complete=false`. Its existence proves the bounded Target product
read path, not a current live UniRouter feed or Production truth.

The capture-gap request experienced one transient tunnel/network error during
the first browser load. The same endpoint passed direct bounded read and the UI
retry completed with both gaps. No data or service mutation occurred. This is
an observed P2 operational/tunnel condition, not evidence of data loss.

## Access And Login Boundary

Rc.5 intentionally has no X-Insight username/password screen. Under the
accepted single-principal trial posture, possession of the approved management
tunnel plus its cloud and SSH credentials is the access boundary. The approved
current path uses Session Manager as the OpenSSH stdio proxy and an explicit
OpenSSH `-L 127.0.0.1:18080:127.0.0.1:8080` bind. The operator first verifies
the exact loopback listener, then opens `http://127.0.0.1:18080/executions`
while the tunnel is active. Direct `ali-instance-cli portforward` is prohibited
for this tested macOS CLI version because its wildcard bind fails the runbook.
The Target service itself remains unreachable from the public IP.

A customer-managed internal reverse proxy may later add identity and
authorization without changing the X-Insight listener. Multi-user product
auth, X-IAM integration and audit-on-read remain outside PRD2-M1 and must not be
implied by this Target result.

## Status Matrix

| Layer | Status after this record | Exact boundary |
| --- | --- | --- |
| Local | Verified | execution-owner self-tests for rc.5 source and package |
| CI | Verified | exact main `27e05df...`, signed/legacy/fresh-host qualification |
| Supported-profile VM | Verified | clean install and lifecycle qualification; not the named Target |
| Target | **Verified** | exact rc.5 artifact, named non-production host, loopback owner-supervised approved-metadata scope |
| External integration | Pending | no current jointly accepted/implemented completed-execution feed and no fresh UR-E replay in this batch |
| Field usability | Pending | accepted `N=1` User task script has not been run |
| Production | Not verified / out of scope | no public, multi-tenant, HA/DR, Production security or SLO claim |

## Findings And Remaining Gate

- P0: none observed.
- P1: rc.4 historical-bundle verification defect, fixed and closed by rc.5
  with Local, CI and Target proof. The later direct-CLI wildcard-listener
  finding is also closed for the current operator workflow by terminating that
  session, prohibiting the command and verifying the explicit OpenSSH
  loopback-only replacement.
- P2: one retryable management-tunnel read interruption; UI recovery passed.
- Remaining X-Insight product/release blocker: none for the exact accepted
  Target scope.
- Next cross-project gate: UniRouter supplies a newly authorized bounded
  sidecar artifact from merged/CI-passing code or accepts and implements the
  versioned completed-execution pull contract.
- Next human gate: the User performs the accepted manual-guided `N=1` field
  investigation tasks during the joint window.

The two temporary Phase 1 snapshot JSON artifacts remain due for exact-path
cleanup on 2026-07-17. They were not touched on 2026-07-14, and persisted
normalized evidence, receipts, hashes and sanitized evidence remain outside
that file-cleanup authorization.

## Governance Correction — 2026-07-19

The preceding `Next cross-project gate` bullet is preserved as the original
2026-07-14 wording; it recorded an X-Insight proposal, not an accepted or
authorized UniRouter task. The completed-execution pull contract was later
deferred as a whole. The separate `UR-E` request is Open and non-binding, and
authorizes no artifact generation or delivery. The current authoritative
disposition is recorded in
[UR-E Cross-Project Governance Correction](ur-e-cross-project-governance-correction-2026-07-19.md).
