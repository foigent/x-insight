# PRD2 P2-4 Release, Upgrade And Manual Local/CI Evidence

Status: implementation merged through PR #35; Local, exact signed CI and two
clean-VM paths verified; named Target, field authority, External integration and
Production remain Pending
Date: 2026-07-14
Execution owner: Codex
Branch: `codex/prd2-p2-4a-release-foundation`
Base: `main` / `origin/main` at `a0194e7`
Merged implementation baseline: `7d56a36fcce255be152621a39c48645be6d7549c`

## Outcome

The merged X-Insight-owned P2-4 implementation covers the code paths and CI-hosted Linux
qualification that can be completed before a named Target or UniRouter window:

- signed release configuration/manifest/trust/revocation and deterministic
  pack/verify/exclusive-extract;
- immutable host lifecycle with durable install/adopt/upgrade/rollback/restore
  journals and receipts;
- PostgreSQL 16 schema 11 compatibility, migration checksum/dirty ledger,
  maintenance serialization and reader/importer/admin separation;
- loopback-fail-closed systemd and pinned PostgreSQL Compose templates;
- release-matched `zh-CN` source/offline manual and product-help route; and
- exact-toolchain Ubuntu 22.04 signed-v2 CI workflow using an ephemeral,
  explicitly non-publishable key.

This record is owner self-verification. It is not independent acceptance and
does not claim a field/customer installation.

## Truth Boundary

| State | Result |
| --- | --- |
| Local code/unit/component/browser | Pass |
| Exact Go 1.25.11 / Node 22.23.1 / npm 10.9.8 signed build | Pass; run `29278281737` |
| Fresh Ubuntu 22.04 + PostgreSQL 16 Compose install | Pass on dedicated CI clean VM |
| Exact Phase 1 installed legacy → first SemVer → rollback → re-upgrade → isolated restore | Pass on separate CI clean VM |
| Supported-host SIGKILL/power-loss sample | Local phase-boundary/fault tests pass; real VM sample not run |
| Named Target | Not run |
| UniRouter External integration | Not required for P2-4; pending joint contract/window |
| Production | Not run / not in scope |

Local release-host toolchain is Go 1.25.0, Node 23.11.0 and npm 10.9.2. It was
deliberately not used to claim the frozen signed release build.

## Exact CI And Clean-VM Evidence

GitHub Actions run
[`29278281737`](https://github.com/foigent/x-insight-private-history/actions/runs/29278281737)
passed all five jobs. The branch head was
`180d6aad7f09852b4d1a179a3a950f2cdf6f690d`; the pull-request merge ref built
inside Actions was `0f82712ebb3d498d3eed6c64fdf4697b4de37801`.
The distinction matters: neither identity is a promoted field release.

PR #35 squash-merged as
`7d56a36fcce255be152621a39c48645be6d7549c`. Main push run
[`29279421355`](https://github.com/foigent/x-insight-private-history/actions/runs/29279421355)
then repeated all five jobs successfully. The hashes below intentionally bind
the already recorded PR qualification run; each CI run creates a new ephemeral
key and non-publishable artifact, and no CI artifact was promoted as a field
release.

The ephemeral-key signed candidate was rebuilt twice and matched byte for byte:

| Evidence | SHA-256 |
| --- | --- |
| Signed bundle / reproducible output | `7fbef7fbfac319384b5d9241df62b35cba6a0476f14671c7186fa30d239d9eef` |
| Signed manifest | `05aa430064d045644384c6db03ec63e2a56aab0ddce524cca6d2e4458a69dc81` |
| Payload | `825baba4aea92113e6119feac7808d75da8c51e5edbf3d071082d48afcdb3d69` |
| External bootstrap verifier | `0f80bda2c50ebb46038a4b8cfeb2e3ed43b3f56e54bcae851e442496ba400989` |
| Manual source | `ba776572e96aa5f7132fb8e023777cd3d1130edbb7252ecbc205b5e91d37f581` |
| Uploaded signed qualification archive | `bb3036dba76155d10f73223afd3062de79427913af96417f7b56409ff7f57b92` |
| Uploaded fresh-install evidence archive | `867d214aa99974923f012c0c7299f5980813c5698042dd45f84cfb1944737646` |

The first clean GitHub-hosted Ubuntu 22.04 VM reproduced the exact Phase 1
artifact at SHA-256
`3e3794338d690fe5b12de46600e17d455baeceab0b79300af260baf78d0346b1`,
started its installed legacy systemd service, then completed `adopt`, `upgrade`,
application rollback without down migration, re-upgrade and isolated restore.
The final schema was 11, the restored two-event denial execution was readable,
and X-Insight/PostgreSQL stayed on `127.0.0.1:8080`/`127.0.0.1:5432`.

The second clean VM downloaded that exact signed candidate, independently
verified/extracted it, passed `install --check`, installed schema 11 with three
distinct database roles, started systemd, imported/read back the two-event
synthetic denial fixture, served the matching offline manual and retained only
the two approved product/database loopback listeners. Its result schema is
`xinsight.ci-fresh-install-qualification.v1`; the legacy path uses
`xinsight.ci-host-qualification.v1`. Both explicitly classify the result as
`ci_clean_vm_not_target`. GitHub retains the uploaded artifacts until
2026-10-11; this record retains their result fields and digests without storing
keys, DSNs or raw content in the repository.

## Qualification Findings And Disposition

| Finding exposed by executable qualification | Classification | Disposition |
| --- | --- | --- |
| Pinned PostgreSQL bind mount started as root-owned and the image could not initialize `PGDATA`. | P1 pre-field install blocker | Derive nonzero numeric `postgres` UID/GID from the already pinned image, apply one exact non-recursive directory `chown`, document it, and pass both clean-VM paths. |
| The historical Phase 1 importer does not support the later `--receipt-output` flag. | P1 qualification/compatibility defect | Honor the Phase 1 stdout report contract instead of retrofitting the legacy binary; exact legacy transition passes. |
| Immediate post-restart listener/readiness checks raced normal systemd startup. | P1 recoverability defect | Add bounded 10-second listener and HTTP readiness polling with immediate failure on a public listener; unit/race and clean-VM paths pass. |
| A legacy application rollback could not be re-upgraded because the managed credential bridge was rejected as legacy drift. | P1 rollback-window defect | Accept only the exact managed bridge when a previously verified v2 identity, launcher, unit, Compose and managed environment all match; rollback→re-upgrade passes. |
| The manual/fresh qualifier used a non-existent importer DSN environment variable. | P1 manual execution defect | Use the implemented `XINSIGHT_IMPORTER_POSTGRES_DSN_FILE` contract and execute the corrected command through fresh-install qualification. |

No new verified Phase 1 P0/P1 was found; these are PRD2 release-path findings
and do not reopen Phase 1. The GitHub Action Node 20→24 messages are upstream
action-runtime deprecation warnings, separate from the frozen Node 22.23.1
product build toolchain.

## Release And Lifecycle Evidence

- Full `go test ./...`: pass.
- Focused release/launcher/web UI packages: pass.
- `go vet ./...` and focused race tests for hostctl/releasebundle/legacyrelease/
  release/launcher pass on the merged implementation and remain required in CI.
- Exact recovered Phase 1 artifact accepted only at commit
  `b9e5811cc53a5cb06614956e66a1c54756617f0a`, artifact SHA-256
  `3e3794338d690fe5b12de46600e17d455baeceab0b79300af260baf78d0346b1`
  and manifest SHA-256
  `aaa53c2af46f59c328ff3271fa518a20bb0551fd7bdc02a0463ff925a43a6bc1`.
- Lifecycle tests cover fresh install, exact legacy adoption/transition,
  N-1→N, application rollback without down migration, re-upgrade over expanded
  schema, restore-to-new-database, restore proof failure credential reversion,
  receipt consistency and every durable phase resume boundary.
- Releasebundle tests cover deterministic Ed25519 envelope/payload bytes,
  wrong/revoked trust, tampering, symlink/private-key-mode rejection, bounded
  archive extraction and exact file/mode verification.

The build workflow uses the recorded Node/npm versions to regenerate the manual
and frontend from locked sources, refuses any difference from committed assets,
then produces four signed-payload runtime binaries and one external bootstrap
`xinsight-release`. Two isolated builds use the same commit/source epoch/key and
must match byte-for-byte. The CI artifact name and included warning state that
its ephemeral-key output is not publishable.

## Manual And UI Evidence

- 12 Markdown chapters implement version/boundary, concepts, install, security,
  upgrade/recovery, trial exit, investigation, provenance/gaps, CLI/API/errors,
  diagnostics and compatibility content.
- `node scripts/build-manual.mjs` run twice produced identical HTML/CSS/SVG
  hashes. The generated source digest also binds release config and the
  compatibility matrix.
- `npm run check`, six Vitest component tests and `npm run build`: pass.
- Four Playwright desktop/narrow flows: pass. They cover investigation and
  product-help → same-origin `/manual/zh-CN/`, exact product/manual meta identity,
  no off-origin request and no horizontal page overflow.
- A headed browser inspection at desktop and 390px found and corrected an
  initial footer overflow and missing favicon; the final page has no console
  error and `scrollWidth == clientWidth` at 390px.
- Go web tests prove GET/HEAD routing, CSP/security headers, manual identity,
  embedded local CSS/icon and no external HTML dependency.

## Acceptance Classification

| Gate | Current classification | Missing executable evidence |
| --- | --- | --- |
| UP-A01 fresh install | CI clean-VM pass | Named Target/manual-guided task remains |
| UP-A02 legacy/N-1 upgrade | Exact legacy CI clean-VM pass | Later real N-1 release when one exists |
| UP-A03 rollback/restore | CI clean-VM pass | Named Target/manual-guided task remains |
| UP-A04 crash recovery | Local phase-boundary pass | Supported VM SIGKILL/power-loss sample |
| UP-A10 release includes manual/matrix/notes | Exact signed CI pass | Publishable field release after authority decision |
| MAN-A01/A02/A03 admin tasks | Not run | Owner-operated supported VM tasks using only manual |
| MAN-A04 investigation task | Product browser path pass | User field task |
| MAN-A05 labels | Local pass | Field narrative confirmation |
| MAN-A06 offline manual | Packaged CI clean-VM pass | User field task remains |
| MAN-A07 release identity/reproducibility | Exact signed CI double-build pass | Publishable field identity remains |
| MAN-A08/A09 no leak/links | Signed payload CI pass | Field artifact recheck remains |
| MAN-A11 UI help | Packaged CI clean-VM pass | User field task remains |
| MAN-A12 publisher authenticity | Code path pass | Real field key/trust/bootstrap receipt |
| MAN-A14 independent blind test | Not run by design | Future participant uninvolved in authorship |

## P0 / P1 / P2

- P0 before any field artifact: choose the real signing authority/key custody,
  distribute the public trust identity and authenticate the first bootstrap
  verifier through an out-of-band receipt; generate and qualify the exact
  publishable artifact on the named Target. Exact signed CI and the two clean-
  VM functional paths now pass, but they do not supply field authority.
- P1 before persistent user-site trial: repeat the qualified legacy transition
  with the exact publishable artifact on the named Target, run the manual-guided
  install/rollback/restore and User investigation tasks, and resolve any manual
  defect requiring author improvisation.
- P2/later: self-service OS package/repository, KMS/HSM signing integration,
  fleet upgrade, in-product version display and independent manual blind test.

## Pending Product Decision: Field Release Authority

Recommended first-field default: one owner-controlled offline Ed25519 release
key, never present in GitHub CI or on the customer host; public trust JSON and
bootstrap SHA-256 are frozen in an approved release record and the User confirms
their fingerprint through a second channel before Codex installs. This fits the
accepted owner-operated `N=1` trial and current file-key implementation.

Benefits: smallest implementation, no cloud signing dependency, straightforward
revocation/rotation record. Costs: a manual ceremony, one primary custodian and
an unencrypted private-key file while the pack command uses it; it is not a
scalable self-service release system.

Later production default: KMS/HSM-backed signing with workload identity,
auditable approvals and no exportable raw key. Benefits are stronger custody,
rotation and audit. Costs are new provider/runtime dependencies and a signer
interface not implemented in PRD2.

Customer-specific keys are not recommended for the first trial: they improve
customer authority isolation but multiply build/trust/rotation/support paths
before multi-tenant or fleet requirements exist.

No real field key, trust store or publishable artifact will be generated until
the User accepts a custody/distribution choice.
