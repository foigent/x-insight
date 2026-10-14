# PRD2 Single-Node Upgrade And Rollback Contract

Status: accepted PRD2 architecture contract; implementation merged through PR
#35 at `7d56a36fcce255be152621a39c48645be6d7549c` with Local, exact signed CI and
fresh/legacy clean-VM evidence; owner-supervised first-customer authority
accepted; supported-profile SIGKILL/recovery and post-recovery exact-main
delivery verified; exact rc.6 publication, named-Target upgrade and
non-mutating rollback readiness verified. Literal power loss, counterpart
decision on X-Insight's proposed, non-binding `UR-E` External-integration gate
and `N=1` field usability remain pending
Prepared: 2026-07-13
Status reconciliation: 2026-07-19
Decision owner: User
Execution owner: Codex
Fact baseline: X-Insight `a8c25167202d0dd9180d9437f44dcdb25fca48fc`

The acceptance rows below preserve their implementation-time checkpoints.
Later exact rc.5 lifecycle proof and exact rc.6 Target refresh are recorded in
[the current rc.6 named-Target evidence](../evidence/prd2/p2-4-rc6-named-target-v2-sidecar-and-ui-evidence-2026-07-19.md); they do not rewrite the
historical bridge failures or establish External integration/Production.

## Answer First

The simplest safe user upgrade is one explicit command operating on a verified
release artifact:

```text
sudo xinsightctl upgrade --artifact /path/to/xinsight-v0.2.0-linux-amd64.tar.gz
```

That command must not be a thin “copy binary and restart” wrapper. It must
preflight compatibility, serialize upgrade activity, verify the artifact,
create and validate a database backup, stage an immutable release, apply
forward-only compatible migrations, switch the `current` pointer atomically,
restart and run version/schema/listener/read-path health checks. It writes a
sanitized receipt and never deletes an old release or backup automatically.

For PRD2, support one qualified profile: Ubuntu 22.04 LTS x86_64, systemd for
X-Insight, a pinned local Docker/Compose PostgreSQL 16 image, one X-Insight node
and a declared maintenance window. The first managed transition is one exact,
verified legacy commit/artifact to the first SemVer release; later releases
support N-1 to N only. Containerized X-Insight, zero-downtime, HA/DR,
PostgreSQL major-version upgrades and arbitrary cross-version jumps remain out
of scope.

Application rollback may reactivate the previous immutable release only while
the new database schema is declared backward compatible. Database rollback is
not an automatic down migration; it is an explicit stop-and-restore operation
from a verified backup. This separation avoids pretending that irreversible
data migrations can be undone safely by changing a symlink.

## 2026-07-14 Implementation Status

Merged PR #35 implements the project-owned lifecycle rather than only this design:

- Ed25519 signed release manifest v2, trust/revocation checks, exclusive verify/
  extract and deterministic payload/envelope generation;
- root-only `xinsightctl` install/adopt/upgrade/application rollback/
  restore-to-new-database plus status/recover/resume/limited abort;
- fsynced durable journals/receipts, atomic release/credential links, strict
  legacy artifact allowlisting, checksum/dirty migration ledger and shared/
  exclusive maintenance locks;
- distinct reader/importer/admin credential files and loopback-fail-closed
  systemd/PostgreSQL 16 Compose templates; and
- release-matched Chinese manual, compatibility matrix and release notes.

Local phase-boundary/fault tests cover fresh install, exact legacy transition,
N-1→N→rollback→re-upgrade, restore proof failure and resume. GitHub Actions run
`29278281737` also passed exact-toolchain signed double-build, a fresh install on
one clean Ubuntu 22.04/PostgreSQL 16 VM and exact Phase 1 legacy adopt/upgrade/
application rollback/re-upgrade/isolated restore on another. Both executable
host results are `ci_clean_vm_not_target`.

These are execution-owner self-tests, not independent acceptance. PRs #40
through #42 add 53 durable phase cases across install/adopt/upgrade/rollback/
restore. Exact-main run `29301064665` also passed an external process-group
SIGKILL during upgrade, same-operation recover/resume, later lifecycle steps and
the separate fresh install; workflow `29301384798` produced the matching
owner-supervised delivery. Literal host power loss, named Target and manual-
guided User tasks remain separate gates. No Production claim is made.

## 2026-07-14 First-Customer Authority Decision

The User and Codex perform every first-customer installation and upgrade, and
the customer cannot self-upgrade. PRD2 therefore does not require the User to
create, store or operate a long-lived offline Ed25519 key.

The accepted `owner_supervised_trial` path keeps the signed-v2 bundle but moves
all key handling behind a manually triggered GitHub workflow. The workflow:

1. accepts only the exact current `main` commit and its successful matching
   `CI` push run;
2. generates one ephemeral Ed25519 identity inside the ephemeral runner;
3. builds the normal signed-v2 artifact and bootstrap verifier;
4. removes the exact private-key file before assembling any delivery; and
5. uploads a strict set containing only artifact, bootstrap verifier, public
   trust store, portable build result, trial-boundary note, checksums and a
   workflow/CI-bound record.

At installation time Codex compares the GitHub run, commit and SHA-256 values,
then repeats those checks on the target before mutation. This provides a
bounded supervised bootstrap; it is not a durable publisher identity because
the ephemeral key cannot authorize a later version. A second customer,
delegated installer or unattended/self-service upgrade requires a separately
accepted persistent KMS/HSM, keyless or offline signing authority, including
recovery and revocation.

## Verified Current State

The repository already provides useful Phase 1 release primitives:

- static Linux/amd64 server and importer binaries;
- a commit-addressed tar artifact with manifest and SHA-256 checks;
- migrations, schemas and fixtures in the release;
- a verifier for layout, hashes, target and forbidden content; and
- a proven target pattern using immutable release directories and a `current`
  symlink.

Those primitives are not yet a product upgrade system:

- there is no SemVer/tag/GitHub Release contract or product `--version`;
- the manifest has no application/docs/config/database compatibility ranges or
  migration checksums;
- migrations support forward `up` execution but have no checksum ledger,
  dirty-state marker or upgrade/advisory lock contract;
- `/readyz` proves database ping only, not application/schema/config
  compatibility;
- the server's development default listens on `:8080`; loopback safety currently
  depends on explicit target service arguments;
- there is no customer installer/upgrader, backup/restore drill or supported
  systemd/Compose package; and
- current release verification excludes documentation from the artifact.

Consequently, “immutable release activation worked once on a non-production
target” must remain separate from “a user can safely self-upgrade.”

## Supported Profile And Version Policy

### PRD2 Support Envelope

| Dimension | PRD2 supported value |
| --- | --- |
| OS/CPU | Ubuntu 22.04 LTS, Linux/amd64 |
| Runtime | systemd, non-login `xinsight` service user |
| Database | PostgreSQL 16 from the approved pinned OCI image/Compose profile, loopback-only; matched PG16 backup/restore client |
| Topology | One X-Insight node; one internal deployment tenant |
| Network | X-Insight `127.0.0.1:8080`; PostgreSQL `127.0.0.1:5432`; tunnel or approved private path |
| Upgrade mode | Supervised maintenance window; one upgrade at a time |
| Supported path | First transition: an allowlisted exact legacy commit/artifact to first SemVer; later: immediately previous supported release (`N-1`) to `N` |
| First candidate | Recommend `v0.2.0-rc.1`; field candidate `v0.2.0` after gates pass |

Fixture/synthetic data may use the prior target storage posture. Approved real
sanitized metadata requires a newly qualified encrypted data volume and
encrypted backup/key-custody path; the prior target does not satisfy that gate.

The actual user-site host must be checked against this envelope. A different
distribution, CPU architecture, container-only environment or air-gap changes
the qualified release matrix; it is not assumed compatible because the Go
binary is static.

### Compatibility Rules

- Every release states exact `supported_upgrade_from_versions` and signed
  `supported_legacy_sources` objects (empty after the first transition). Each
  legacy object binds commit, artifact SHA-256, manifest SHA-256 and expected
  install/schema/config/service-layout identity. It also states supported
  application, database schema, config schema, API, evidence and receipt
  versions.
- An unsupported jump fails before service/database mutation and names the
  required intermediate release.
- Database migrations are expand-first and compatible with both N-1 and N
  application binaries during the documented rollback window.
- Destructive contract/cleanup migrations require a later release after the
  rollback window and explicit operator confirmation; they are not bundled
  with the introducing release.
- No automatic PostgreSQL major-version upgrade is attempted.

## User Workflow

### First Managed Adoption

Existing Phase 1-style installations need a one-time, strict adoption step:

```text
sudo xinsightctl adopt --check
sudo xinsightctl adopt
```

`adopt --check` reads the current release pointer, service unit, configuration
path, listener, database/schema version and file ownership without mutation.
`adopt` creates a managed install-state record only after every current fact can
be pinned to a target-manifest `supported_legacy_sources` entry. A commit alone
is insufficient. It does not fabricate a SemVer or digest for an unknown binary.

Before the first field release, one real legacy source artifact must be frozen,
retrieved and qualified. The default candidate is the final Phase 1 deployed
artifact commit `b9e5811cc53a5cb06614956e66a1c54756617f0a`; it is not an
accepted upgrade source until its exact archive digest, manifest, current
database/config state and legacy-to-target compatibility test pass. If that
artifact cannot be recovered and verified, first-release adoption is blocked;
the project must not relabel an unknown binary as `v0.1.x`.

### Fresh Install

```text
sudo xinsightctl install --check \
  --artifact /path/to/xinsight-v0.2.0-linux-amd64.tar.gz
sudo xinsightctl install \
  --artifact /path/to/xinsight-v0.2.0-linux-amd64.tar.gz
```

The single supported install path verifies the same trust/platform contract,
requires the qualified PostgreSQL 16 prerequisite and loopback listener,
creates root-owned config/service templates without embedding a DSN, stages an
immutable release, applies first migrations under the migration lock, starts
the service and proves compatibility/listener/representative read. It refuses
an existing unmanaged installation and routes that case to `adopt`.

Install uses the same durable operation journal before it creates a role,
database object, config, service or release directory. Its phases are target/
prerequisite verification, journal creation, role/config/service staging,
database bootstrap/migration, immutable release activation, service start and
proof. Each phase defines idempotent resume or explicit abort behavior; failure
never infers success from a partially created file/database. `adopt`, rollback
and restore use the same journal contract for every mutation as well.

### Preflight

```text
sudo xinsightctl upgrade --check \
  --artifact /path/to/xinsight-v0.2.0-linux-amd64.tar.gz
```

Preflight is read-only and reports:

- current and target version/commit;
- artifact digest/signature/trusted release index result;
- OS/architecture and supported upgrade-path result;
- current config/schema/API/evidence compatibility;
- migration checksum/drift/dirty-state result;
- disk required for staged release plus backup;
- service/database ownership and listener safety;
- backup target and restore-verification policy; and
- exact actions that the mutating command will perform.

### Upgrade State Machine

| Step | Required behavior | Failure behavior |
| --- | --- | --- |
| 1. Serialize | Acquire host operation lock and the stable PostgreSQL exclusive maintenance lock. Every PRD2 migrator/import writer must use the same namespace and acquire the compatible shared/exclusive lock. For legacy writers, stop them, prove no process/session is active and temporarily revoke the importer role's write/connection capability. | Fail before mutation if exclusivity cannot be proved. An advisory lock alone is insufficient. |
| 2. Journal | Create an atomic, fsynced operation journal with operation ID, before-state, target, locks and intended phases before the first mutation. | Fail before mutation if durable journal creation/readback fails. |
| 3. Verify state | Read managed install state and current manifest; verify no unknown drift. | Fail closed with stable diagnostic and operation ID. |
| 4. Verify target | Check trusted digest/signature/key state, exact version/legacy path, target, disk, config and migration checksums. | Reject artifact; do not unpack into active path. |
| 5. Stage | Extract into a new immutable `/opt/x-insight/releases/<version>` directory with safe ownership/modes. | Leave current release active; record staging result in journal. |
| 6. Back up | Run PostgreSQL custom-format backup with a pinned/matched PG16 client, hash it, enforce owner-only mode, and—when approved real metadata is used—encrypt it under the accepted key/custody policy; record metadata without secrets. | Abort before migration/activation. |
| 7. Validate backup | Verify archive structure, expected database metadata and hash; PRD2 release qualification additionally restores it in an isolated database. | Abort field release if qualification restore fails. |
| 8. Migrate | Apply checksum-verified forward migrations under the same exclusive database maintenance lock and durable migration ledger. | Stop; journal the exact committed prefix. Follow recover/restore decision; never guess or run automatic down migrations. |
| 9. Activate | Atomically repoint `current`, reload systemd and restart the service. | Reactivate the exact previous release only when compatibility metadata permits. |
| 10. Prove | Check product/version/schema readiness, loopback listeners and a representative bounded read. | Mark operation failed in journal and follow application-rollback/restore decision tree. |
| 11. Record | Finalize the journal and write a sanitized upgrade receipt with before/after release IDs, hashes, migration IDs, backup ref/hash and check results. | Receipt failure is reported; it must not erase the journal or upgrade outcome. |

No step removes prior releases, backups, receipts or evidence. Retention cleanup
is a separate, explicit, one-file-at-a-time operator action under the repository
deletion rules.

### Database Role And Maintenance-Lock Protocol

P2-0 must freeze separate least-privilege roles for read service, importer and
migrator/backup administration. The importer holds the named shared maintenance
lock on one pinned connection for the full persistence transaction; migration,
upgrade and restore hold the exclusive form on one pinned connection for the
full operation. The lock namespace/key and acquisition order are a versioned
constant tested across every writer entrypoint.

Existing legacy binaries do not participate in this protocol. The first
managed transition therefore stops the service, proves there is no importer
process/session, and temporarily removes the legacy write role's ability to
start writes before acquiring exclusivity. If the current single role cannot be
separated and verified safely, adoption stops and an administrator must perform
the documented role split first.

### Crash-Recovery Journal

The operation receipt is an outcome record, not a recovery mechanism. The
root-owned operation journal must be atomically replaced and fsynced before and
after every mutating phase. It records no DSN/secret/raw evidence, but includes
the exact release IDs/digests, database/schema state, backup reference/hash,
migration committed prefix, symlink before/after target and phase status.

`xinsightctl status`, `recover`, `resume` and `abort` read this journal. Each
command has a phase-specific allowlist: for example, `abort` may remove no file
automatically and may reactivate only the exact compatible previous release;
`resume` re-verifies every external fact before continuing. Abrupt-failure
tests must cover every durable phase boundary and prove that the tool never
infers a state from a missing final receipt.

The executable proof is layered rather than pretending one CI mechanism is a
literal power cut. Deterministic tests inject a failure after every durable
phase of install, adopt, upgrade, rollback and restore and require same-operation
resume. The main-only disposable Ubuntu 22.04 qualification additionally sends
an external process-group `SIGKILL` to the exact signed `xinsightctl` after the
real service-stop action but before phase completion is recorded. It requires a
readable active journal, an absent final receipt, bounded `recover` actions and
successful `resume`/readiness. The kill harness is external, PID/PGID/session-
bounded and CI-guarded; the product binary contains no crash backdoor. This is a
supported-profile CI sample, not literal power-loss, named Target or Production
evidence.

## Rollback And Restore

### Application Rollback

```text
sudo xinsightctl rollback --previous
```

Allowed only when:

- target manifest declares the exact prior installed release ID—version,
  commit and artifact digest—compatible with the current
  expanded database schema and config;
- the prior release artifact and manifest still verify;
- no destructive/contract migration has passed its rollback barrier; and
- the operator confirms the known reason and maintenance window.

The command switches the immutable release pointer, restarts, proves version/
schema/listener/read health and writes a rollback receipt. It does not run down
migrations.

### Database Restore

PRD2 supports only restore-to-new-database with an atomic root-owned config
switch. In-place overwrite/replacement is deferred. Database restore is an
exceptional, explicit procedure:

1. stop X-Insight and all import writers;
2. select one exact verified backup path and hash;
3. create one exact new empty database with the documented owner, roles and
   extensions, using the pinned/matched PG16 restore client;
4. restore and verify schema, ownership, representative evidence hashes and
   bounded reads while the application remains stopped;
5. atomically replace the root-owned DSN/config reference, activate the matching
   application/config and prove readiness/readback;
6. if activation fails, atomically restore the old config reference and keep
   both databases stopped from new writes until the operator decides; and
7. preserve the old/failed database and receipts until custody rules authorize a
   specific cleanup.

It must never be triggered automatically by a readiness failure. The operator
needs to decide whether the failure is application-only, a compatible forward
migration issue or a true restore event.

## Release Manifest V2

The exact JSON schema is frozen in P2-0. At minimum it records:

```text
product_name
product_version
release_channel
commit
build_toolchain
target_os
target_arch
artifact_digest
signature/trust_reference
created_at
supported_upgrade_from_versions
supported_legacy_sources
application_compatibility
database_schema_current/min/max
migration_ids_and_checksums
config_schema_current/min/max
api_versions
evidence_schema_versions
receipt_schema_versions
manual_language_and_version
rollback_window/barriers
```

The repository uses Ed25519 detached signatures. For the accepted first-
customer trial, trusted-public-key distribution is bound to the exact manual
GitHub workflow, successful matching main CI and independently rechecked
hashes. A SHA-256 file copied through the same unverified channel would not
authenticate the publisher; the workflow/run identity is part of the bootstrap.
Later delegated or unattended releases require the durable authority described
above.

The local managed install-state record contains no DSN, password, token or raw
evidence. It references root-owned configuration and records only safe paths,
versions, hashes and last operation/receipt identifiers.

## Why PRD2 Does Not Apply Upgrades From The Product UI

The UI may display the current version, compatibility result and an exportable
preflight report, but PRD2 should keep the mutating operation in root-only
`xinsightctl`:

- systemd, release directories, root-owned configuration and backup paths need
  host privileges that the read-oriented web process should not hold;
- an offline terminal command works through the existing approved private path
  without a public update service;
- a stopped or unhealthy application can still be recovered by the host tool;
  an in-product updater disappears with the process it must repair; and
- command/receipt output is easier to attach to a supervised field change.

The cost is that an administrator still opens a terminal and supplies one exact
artifact path. A later managed installer, package repository or fleet service
can remove that step after signing, identity, retention and multi-installation
requirements exist. Giving the current web process root privileges merely to
offer an “Upgrade” button is not an acceptable simplification.

## Readiness And Listener Contract

PRD2 should separate:

- liveness: the process is running;
- dependency readiness: PostgreSQL can be reached;
- compatibility readiness: application, schema and config versions are
  mutually supported and migrations are not dirty; and
- product read proof: one bounded representative read succeeds.

The productized service template must pass `127.0.0.1:8080` explicitly and the
upgrader must fail if it observes an unapproved wildcard/non-loopback listener.
Changing the application default to loopback is recommended defense in depth,
but does not replace listener verification.

## CI And Release Qualification Matrix

| ID | Required executable proof |
| --- | --- |
| UP-A01 | Fresh install on the supported profile reaches compatible readiness and loopback-only listeners. |
| UP-A02 | The exact allowlisted legacy artifact upgrades to the first SemVer; later N-1 upgrades to N; representative evidence hashes/read results are preserved. |
| UP-A03 | The exact previous/legacy binary operates on N expanded schema during the documented rollback window. |
| UP-A04 | Re-running migration applies zero changes; checksum drift and dirty state fail closed. |
| UP-A05 | Concurrent upgrade/import/migration attempts are serialized or rejected safely. |
| UP-A06 | Artifact tamper, wrong target, unsupported path, insufficient disk and unknown config fail before mutation. |
| UP-A07 | Failures injected before backup, after backup, during migration, before activation and after restart produce the documented state/receipt. |
| UP-A08 | Backup archive verifies and restores in an isolated PostgreSQL 16 environment; data/schema/hash checks pass. |
| UP-A09 | Application rollback succeeds only for a declared compatible release and never runs a down migration. |
| UP-A10 | Release artifact includes and verifies the matching Chinese manual, compatibility matrix and release notes. |
| UP-A11 | Deterministic abrupt-failure injection at every durable mutating phase leaves a readable journal and only documented recover/resume/abort paths; a supported-profile external SIGKILL sample proves real process death at one representative boundary. |
| UP-A12 | Importer/migrator/upgrader shared/exclusive locks serialize writes; legacy writer sessions/permissions block activation until exclusivity is proved. |
| UP-A13 | Fresh install on the single supported profile follows `install --check`/`install`, refuses unmanaged drift and leaves loopback-only compatible readiness. |
| UP-A14 | Phase-complete fault injection across install, adopt, upgrade, rollback and restore proves same-operation recovery, including role/config/database/service transitions; one external SIGKILL sample separately proves the after-action/before-phase-completion case without claiming literal host power loss. |
| UP-A15 | The owner-supervised trial workflow accepts only exact current `main` plus successful matching push CI, publishes the strict seven-file delivery, excludes the ephemeral private key and verifies the signed-v2 bundle before extraction. |

## Trade-Offs

| Choice | Benefit | Cost/limitation |
| --- | --- | --- |
| One supported profile | Small, testable compatibility surface for first users | Other distributions/topologies require qualification rather than “probably works.” |
| Supervised maintenance window | Simple consistency and rollback reasoning | Brief downtime; no zero-downtime claim. |
| Immutable releases + atomic pointer | Fast application activation/rollback and auditable binaries | Old releases consume disk until explicit cleanup. |
| Forward-only expand migrations | Safer data evolution and N-1 binary rollback window | Schema cleanup is delayed; migration discipline is stricter. |
| Mandatory backup before migration | Recoverability and field confidence | Upgrade takes longer and needs disk; a backup is not useful until restore is tested. |
| No automatic cleanup | Prevents accidental loss and follows project deletion controls | Operators need a later explicit retention workflow. |

## Non-Goals

- unattended auto-update or background update checks;
- package-manager repository, Kubernetes operator, containerized X-Insight or
  general installer fleet; the pinned local PostgreSQL 16 container remains
  part of the one supported profile;
- zero-downtime, active-active, HA/DR or multi-region upgrades;
- automatic PostgreSQL major-version changes;
- arbitrary downgrade/down-migration;
- recursive/wildcard/batch deletion of releases, backups or evidence; and
- Production verification from a single non-production upgrade drill.

## Related Records

- [PRD2-M1 Field Validation Implementation Plan](../planning/prd2-m1-unirouter-field-validation-implementation-plan-2026-07-13.md)
- [PRD2 Simplified Chinese Product Manual Delivery Plan](../planning/prd2-zh-cn-product-manual-delivery-plan-2026-07-13.md)
- [P2-4 Release, Upgrade And Manual Local/CI Evidence](../evidence/prd2/p2-4-release-upgrade-manual-local-evidence-2026-07-14.md)
- [P2-4 Owner-Supervised Exact-Main Delivery Evidence](../evidence/prd2/p2-4-owner-supervised-trial-main-delivery-evidence-2026-07-14.md)
- [Alibaba Cloud Single-Node Target Runbook](../ops/deployment/alibaba-cloud-single-node-target-runbook.md)
- [Phase 1 Closeout Architecture And Function Audit](phase-1-closeout-audit-2026-07-10.md)
