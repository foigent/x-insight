# XI-A0 performance decision — design only, not implementation approval

The automatic approval rejection at 2026-09-08 22:44 UTC remains binding:
“broad migration redesign … removes many snapshot columns, adds an immutable
database trigger, and changes core storage semantics”. This continuation has
not applied or indirectly retried immutable economic facts + narrow membership.

## Current recommendation

Review the completed Local evidence before deciding the storage change. Keep
5-second/300k target and current load. Existing query-only evidence does not
support meeting it. Raising the 3-second API deadline by itself would expose
slow creation rather than solve the failed target; it is not implemented.

If this design is selected, the supervisor must obtain informed approval for
the exact patch before any implementation, including its immutable trigger,
field removal, grants and restoration behavior. Approval to resume A0 is not
approval for this rejected design.

## Concrete shape requiring review

- Economic fact adds first accepted Evidence `event_hash`, populated from its
  existing `evidence_event_id`; retain source_event_hash separately. Fact ID and
  `(source_system_id, source_version, source_ref)` stay stable. Adapter and epoch
  remain excluded from economic identity.
- Membership would keep `reading_id`, `ordinal`, `economic_fact_id`. Removed
  values (Evidence ID/hash, source ID/version/hash, request/execution ID,
  model/source-project/cost-project, terminal status/time, both amounts/currency)
  must all be retrieved from an explicitly immutable fact revision.
- INSERT-only immutable-fact enforcement would deny UPDATE/DELETE for every
  supported path. This changes today's rebuildable projection model: rebuilding
  cannot delete/reinsert referenced IDs or silently change first Evidence refs.
- All summary/series/contribution/detail/digest/coverage queries must use the
  same fact revision. Late facts still require new membership; current grants
  are rechecked independently on each request.
- A1 corrections must create versioned interpretation data bound by reading,
  not update economic facts or join current unversioned attribution. Until
  that binding is specified, do not remove an explanatory snapshot field.

## Required verification before acceptance

1. Compare every removed field before/after on official fixture, replay, distinct
   sources and mapper identities, conflicting facts, arbitrary precision money.
2. Race late insertion/current revoke with old/new readings and page/detail
   traversal; prove stable membership/interpretation digests and references.
3. Prove importer/workspace/reader/admin enforcement and explicit failure on
   forbidden fact mutation. Preserve M0/Envelope/source hash separation.
4. Test clean24 and supported historical upgrade, duplicate/conflict rollback,
   backup/restore, partially missing references, and projection recovery. Preserve
   fact IDs; reject inconsistent restore rather than silently serve changed data.
5. Repeat 20×300k, real500k, 500001 zero-residual, production HTTP creation and
   10-reader/20RPS reads with durability/FKs enabled. No performance guarantee is
   made in advance.
6. Regenerate development contract from actual PG16; preserve rc13/schema23
   historical bytes and run full compatibility/old-path regression.

## Recovery and authority

No released migration identity has changed. A candidate must be implemented in
this isolated branch only after approval, retain the pre-change diff/commit and
all failure evidence, and use disposable synthetic databases. For a future
real installation, an irreversible data conversion needs backup identity,
restore-to-new-DB verification, compatible binary pairing and an approved
maintenance/availability policy; D-37-01 is unresolved. No user DB reset or
online conversion is authorized by this note.
