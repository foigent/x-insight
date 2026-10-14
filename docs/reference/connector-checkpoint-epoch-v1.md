# Connector Checkpoint Epoch V1

`xinsight.connector-checkpoint-epoch-plan.v1` is the root-operated transition
contract for moving one source-neutral Connector binding to a new delivery
epoch. It exists to prevent a new integration campaign from silently reusing a
prior campaign's producer instance, Cursor, retry state, or counters.

This is delivery control state. It never enters an M0 event row, normalized
event hash, idempotency identity, item/event-linked provenance, or Capture Gap
identity.

## Safety invariants

A prepare or activation is accepted only when all of the following are true:

- the selected managed worker unit exists and is both inactive and disabled;
- the product-local Source and Adapter identities are active and type-compatible;
- the durable worker state is `idle`/`inactive`;
- Cursor equals high-watermark at a quiescent page boundary;
- `last_error` is empty and `consecutive_failures` is zero;
- every received item is already classified as committed or duplicate-noop;
- the next producer/source instance differs from the current instance;
- the next Cursor equals the next high-watermark;
- there is no other prepared transition for the binding.

While an operation is prepared, both managed workers reject a poll-start state
transition for that exact Source/Adapter/Cursor binding. This database predicate
and the row lock close the race left by a systemd disabled check: a concurrent
manual start either reaches the state row first and makes prepare fail, or reaches
it after prepare and cannot begin polling.

`prepare` stores an exact active-state SHA-256 and returns a redacted plan. Raw
Cursor/high-watermark values stay in the root/admin delivery plane; reports
only expose their SHA-256 values. `activate` requires the exact operation ID and
plan SHA-256, locks both plan and active state, verifies that the state still
matches, archives the entire old epoch, and changes the active row in one
serializable PostgreSQL transaction.

The archive is append-only. PostgreSQL rejects UPDATE and DELETE against
`connector_checkpoint_epoch_history`. The current row receives the next epoch,
new stable source instance and new quiescent Cursor/high-watermark, while poll
timestamps, errors, retry state and counters reset to their initial values.

An interrupted activation is safe to retry: before the transaction commits,
the old epoch remains active; after commit, the exact activate command returns
the already-activated result. A prepared operation may be aborted until the
activation barrier is crossed. An activated operation cannot be aborted.

## CLI

Use only the already-verified target `xinsightctl` and exact product-local
identities. Raw Cursor values never enter argv. Put the request in an absolute,
single-link, root-owned regular file with mode `0400` or `0600`; nested duplicate
keys, unknown fields, trailing JSON, mismatched worker/cursor profiles and
per-profile boundary limits fail closed. The shipped product-local Schema is
`schemas/integration/connector-checkpoint-epoch-prepare-v1.schema.json`; the Go
decoder remains the runtime authority and additionally enforces byte limits,
UTF-8 and exact Cursor=high-watermark equality.

```text
sudo /opt/x-insight/current/bin/xinsightctl connector-epoch status \
  --source-system-id SOURCE_UUID \
  --adapter-manifest-id ADAPTER_UUID \
  --cursor-key unirouter.completed-execution.v1 \
  --worker-unit xinsight-feed.service

{
  "schema_version": "xinsight.connector-checkpoint-epoch-prepare.v1",
  "binding": {
    "source_system_id": "SOURCE_UUID",
    "adapter_manifest_id": "ADAPTER_UUID",
    "cursor_key": "unirouter.completed-execution.v1"
  },
  "worker_unit": "xinsight-feed.service",
  "next_source_instance": "NEW_STABLE_INSTANCE",
  "next_cursor": "EXACT_QUIESCENT_CURSOR",
  "next_high_watermark": "EXACT_QUIESCENT_CURSOR",
  "authorization_ref": "WORK_ITEM_OR_DECISION_REF"
}

sudo /opt/x-insight/current/bin/xinsightctl connector-epoch prepare \
  --request-file /root/xinsight-second-integration-epoch.json

sudo /opt/x-insight/current/bin/xinsightctl connector-epoch activate \
  --operation-id EXACT_OPERATION_ID \
  --plan-sha256 sha256:EXACT_PLAN_DIGEST
```

Use `status --operation-id EXACT_OPERATION_ID` to recover state after an
interruption. Use `abort` with the same operation and plan binding only while
the operation remains prepared.

Preparing or activating an epoch does not start or enable a worker, register a
source-side credential, reset UniRouter, create traffic, or authorize a second
integration campaign. Those remain separate owner-controlled gates.
