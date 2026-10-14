# PRD2 Workspace credential bridge contract

Status: `0.3.0-rc.3` local release-candidate contract. Target qualification is pending.

## Problem

`0.2.0-rc.3` through `0.2.0-rc.15` and the exact Phase 1 legacy source use a valid
three-role credential set: reader, importer, and admin. Workspace support introduced a fourth
least-privilege role and `postgres-workspace.dsn`. The initial rc.2 target controller loaded the
new four-file layout before it evaluated the declared upgrade source, so a valid rc.15 target could
not reach preflight even though rc.15 was present in the signed upgrade allowlist.

This is a release-controller compatibility defect. It must not be worked around by manually adding
a DSN or by weakening the four-role steady-state boundary.

## Accepted source boundary

The three-role parser is available only after the controller has verified one of these exact source
identities:

- a managed `0.2.0-rc.3` through `0.2.0-rc.15` release accepted by the target manifest; or
- an exact legacy artifact accepted by the target manifest and its immutable artifact/manifest
  digests.

Every other source requires a strict four-file credential set. A present but invalid
`postgres-workspace.dsn` never falls back to the legacy parser. `0.3.0-rc.2` is a four-role source and
therefore does not use the bridge.

## Read-only preflight

`adopt --check` and `upgrade --check` do not create a password, directory, role, grant, symlink,
journal, backup, or database row. They validate:

- the absolute credential-set link and managed directory boundary;
- the exact reader/importer/admin role names, one loopback database endpoint, and three distinct
  passwords;
- PostgreSQL 16, source identity, migration ledger prefix, role boundary, host tools, and capacity;
- absence of a pre-existing `xinsight_workspace` role, so the bridge never takes over unexplained
  cluster-global role state;
- the target artifact, trust, access credential, and declared upgrade path.

The report exposes `workspace_bridge_required` and lists the successor credential set and
least-privilege Workspace role as required mutating actions.

## Mutating sequence

The bridge is owned by the verified target controller and runs only inside the existing root-only
operation lock and durable journal.

1. Capture and stop supported workers, stop the application, gate importer login, and prove no
   competing role sessions.
2. For upgrade, create and verify the owner-only database backup before the role boundary changes.
3. Record the deterministic successor set path in the journal.
4. Create a `0700` staging directory and four `0600` files. Reader/importer/admin DSNs are copied
   byte-for-byte; only a 256-bit random Workspace password and DSN are generated.
5. Atomically rename the complete staging directory to its immutable successor path.
6. Create or harden `xinsight_workspace`, apply the current least-privilege grants, verify all role
   connections and privileges, then atomically switch `/etc/x-insight/database/current`.
7. Continue checksum migrations, reapply post-migration grants, activate the immutable application
   release, prove readiness and Evidence fingerprint stability, and write the sanitized receipt.

The source credential set is never edited or deleted. DSN/password values never enter the journal,
receipt, error, log, artifact, or repository.

## Interruption and recovery

Staging and boundary expansion are idempotent. A resumed operation accepts only the exact journaled
source or successor set; any third link target or inherited DSN drift fails closed.

Once `expand_workspace_database_boundary` starts, role state may have changed. It is therefore a
database barrier: `recover` and `resume` remain available, while `abort` is absent and independently
rejected. This avoids a receipt that falsely claims the credential/database state returned to its
pre-operation form.

## Preserved semantics

The bridge does not add a migration or change API, Evidence, M0 identity/hash, Connector data,
Gap/Provenance, Cursor/Checkpoint, operational receipts, access accounts, Feed credentials, or the
A+C product UI. Feed and Connector runtime activation remain separate decisions.
