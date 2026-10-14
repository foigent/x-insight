# P2-4 Named Target Restrictive-Umask Blocker Evidence

- Date: 2026-07-14
- Owner: Codex
- Classification: execution-owner Target operation evidence; not independent
  acceptance
- Target: named non-production Alibaba Cloud ECS, resource identifiers
  sanitized

## Outcome First

The corrected `0.2.0-rc.2` storage/credential bridge passed PR CI, exact-main
release qualification, owner-supervised publication and Target-side artifact
verification. Target credential preparation and durable legacy adoption also
completed.

The subsequent upgrade failed closed during `stage_release` because the
Cloud Assistant root shell used restrictive `umask 077`: the new release root,
`bin/` directory and binaries inherited `0700` instead of the signed release
contract's `0755`. No release backup, database migration or activation had
started. The lifecycle tool allowed a bounded abort, which restored role login
state and the exact Phase 1 service at schema 9.

This is a verified P1 product defect. `0.2.0-rc.2` gains no Target status. The
failed release tree, journal, receipts and staging directories remain
preserved.

## Exact Source And Delivery Identities

| Item | Exact value |
| --- | --- |
| Corrected main | `894d72560ab93ae3fcc200ab0cb76ec1fc55068f` |
| PR source CI | `29335667662`, success |
| Exact-main CI | `29335953131`, success |
| Owner-supervised workflow | `29336389137`, success |
| Owner delivery artifact | `8312130217` |
| Owner Actions ZIP digest | `sha256:6d6553ab3d4e652934c7f00a02da8d1359216cb1fc96fad142df7eadcda7d435` |
| Signed bundle SHA-256 | `96288524d9ec0c061a66771b3144e250653813eba1cce6d0fc674659c51d2384` |
| Signed payload SHA-256 | `e773ad4786651d8b8918e31e5d66ed517e271c5ad7ed17b89bfbacdca2e69af0` |
| Signed manifest SHA-256 | `fa82e456de1280db82c6b230f6cd6e2973b3354cd337dfce97d5e0727253d612` |
| Bootstrap SHA-256 | `3c18b900f47b130cd2aa9862657c8c27acf931aa48d0bf8bdcf63f59b46890cf` |
| Public trust-store SHA-256 | `cc9d439f66be467d4572de1b638cac6b3e0ffdaf0c36a82a1f32f1ad68cbda4c` |
| One-run key ID | `ed25519:sha256:dc4eaed111421c2173a41e3f4804eb9e48c5d72325519aab897e2d7dd2bf4278` |
| Exact Phase 1 commit | `b9e5811cc53a5cb06614956e66a1c54756617f0a` |
| Exact legacy artifact SHA-256 | `3e3794338d690fe5b12de46600e17d455baeceab0b79300af260baf78d0346b1` |

The execution owner downloaded the artifact into one explicit local directory,
verified the exact seven-file set and `SHA256SUMS`, and independently ran the
portable release verifier. The Linux bootstrap correctly refused execution on
macOS and was then verified on the Linux Target.

## Corrected CI Gates

Exact-main run `29335953131` passed:

- reproducible signed-v2 build and signature/extraction checks;
- exact final Phase 1 legacy artifact reproduction;
- actual legacy role/password-file/storage bridge;
- adopt, schema 9-to-11 upgrade, external SIGKILL recovery, rollback,
  re-upgrade and isolated restore; and
- a separate clean Ubuntu 22.04 x86_64 fresh installation.

These are supported-profile CI facts. They did not pre-authorize a Target
success or cover a Cloud Assistant shell's restrictive umask.

## Target Preparation And Adoption

- Owner delivery path:
  `/data/x-insight/owner-supervised/20260714-rc2-894d725-attempt2`.
  A first staging path that lacked host `unzip` support remains preserved.
- Target-side delivery, signature, payload, binary, manual, canonical Compose
  and exact legacy allowlist checks passed.
- The prior system trust store was preserved before the owner-supervised rc.2
  trust store was atomically activated.
- The pre-bridge custom backup remains
  `/data/x-insight/backups/xinsight-pre-prd2-admin-bridge-20260714.dump`, mode
  `0600`, SHA-256
  `8e40fd408825ee6af6842c3b22551d7295c9ed9ac1f9e629dd908b8f4edfd054`,
  with 71 `pg_restore --list` entries.
- The versioned root-only credential set is
  `/etc/x-insight/database/sets/prd2-adopt-20260714-894d725`. The bridge created
  only `xinsight_admin`; durable adopt created/hardened reader/importer and
  applied their grants.
- `adopt` operation `20260714T133920Z-477373b9e079a228` completed. The resulting
  state is managed legacy, current Phase 1 release, database schema 9 and no
  incomplete adoption journal.

No credential value, raw prompt/response or customer data entered this record.
The run remains fixture/synthetic-only on an unencrypted trial disk.

## Upgrade Failure Boundary

Upgrade operation `20260714T133925Z-e1a466a0206b23f1` completed only:

1. `stop_service`; and
2. `exclusive_maintenance`.

It then failed `stage_release` with `invalid X-Insight release bundle: extracted
release root`. Inspection established:

- failed release directory:
  `/opt/x-insight/releases/v0.2.0-rc.2-894d72560ab9-e773ad478665`;
- release root mode: `0700`;
- `bin/` and signed executables: `0700`; and
- expected signed contract: directories and `bin/` executables `0755`, regular
  payload files `0644`.

The artifact bytes and manifest were unchanged. The mismatch arose because
`os.Mkdir`/`os.OpenFile` creation modes were filtered by the process umask and
the lifecycle path did not explicitly restore the contract modes before
verification.

No backup phase, schema migration, runtime-file replacement, current-pointer
activation or service start for rc.2 occurred. The current pointer and schema
ledger therefore remained Phase 1/schema 9.

## Bounded Abort And Restored Service

`xinsightctl abort` was permitted because the operation had crossed neither the
database-migration nor activation barrier. Post-abort checks confirmed:

- `xinsight_importer`, `xinsight_reader` and `xinsight_admin` login state was
  restored;
- `/opt/x-insight/current` still points to exact Phase 1
  `b9e5811cc53a5cb06614956e66a1c54756617f0a`;
- the migration ledger remains nine rows;
- `xinsight.service` is enabled, active and ready;
- X-Insight and PostgreSQL still listen only on `127.0.0.1:8080` and
  `127.0.0.1:5432`; and
- the failed operation, release tree and Target staging evidence remain
  present.

No recursive, wildcard or bulk deletion was used.

## Corrective Requirement And Resume Gate

The next candidate is `0.2.0-rc.3`, not a republished rc.2. It must:

1. explicitly set the release root, payload directories and payload file modes
   after creation, independent of `umask`;
2. explicitly set newly copied systemd/Compose/runtime file modes and newly
   created lifecycle directory modes;
3. pass regression tests that run extraction and full lifecycle install under
   process-level `umask 077`;
4. pass a new PR, exact-main CI and owner-supervised one-run signing workflow;
5. use a new Target staging path and reverify the exact delivery/trust
   identities; and
6. run `upgrade --check` and `upgrade` directly from managed legacy. `adopt`
   must not be repeated.

The first three corrective gates now pass locally: extraction and complete
install were exercised under process-level `umask 077`; full Go and race tests,
frontend/unit/browser/manual checks, fixture conformance, workflow lint and CI
router/gate self-tests pass. This is execution-owner self-verification, not
PR/main CI or Target acceptance. Gates 4 through 6 remain open.

- P0: none observed.
- P1: release/runtime permissions inherited operator umask, blocking safe
  Target activation.
- P2: failed rc.2 release/staging files remain intentionally retained until a
  separately authorized exact-path cleanup decision.

This evidence does not promote External integration, customer acceptance or
Production.
