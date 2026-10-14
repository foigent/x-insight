# Completed-Execution Pull V1 Runtime Contract

Status: production-intended implementation contract; Local implementation in
progress on 2026-07-25. Deployment and live activation are separately
sequenced.

## Purpose And Ownership

X-Insight asynchronously pulls terminal execution metadata from a UniRouter
transactional outbox. UniRouter remains independent of X-Insight availability.
X-Insight owns normalization, durable consumption state, idempotent evidence
persistence and investigation readback.

V1 deliberately excludes a public push collector, direct UniRouter database
access, PostgreSQL replication/CDC, broad backfill, late revisions,
pre-billing rejection, customer data and multiple active workers.

## Accepted Wire Identity

The outer page is `xinsight.completed-execution-feed.v1`. Each item embeds
exact bytes for:

- schema `unirouter.completed-execution.v1`;
- immutable revision `1`;
- one terminal `settled` or `cancelled` execution;
- `timeline_complete=true`;
- the fixed explicit capture-gap set; and
- metadata only, with no artifact, approval-manifest, exchange or sidecar
  fields.

The receiver is bound to UniRouter commit
`0fe054cb7b5e2b1b8081b0eb8dd454a669772804`, tree
`e0d18932d112d4d7fd9c09cef5a3b47787404b72`, and these immutable files:

| File | SHA-256 | Bytes |
| --- | --- | ---: |
| `unirouter.completed-execution.v1.schema.json` | `4ae2897d4f8470c38624dfe21c54d72eaf6d995d2c9578c4420c94d244424fb2` | 4089 |
| `valid-settled.json` | `5e9eda0a2630d52c5589d38b8ec480604c47c2cc77391b75a57b2a159db6dd27` | 1196 |
| `valid-cancelled.json` | `31cbfeb23968c07d158b591c28efb3dd40cffba1fd6d1fd4202f43d63bfe9f2d` | 1201 |
| `redaction-rejected.json` | `c23fd255c139f94e729ecca70622d6e6341cf7a257c21fbccf592964b0e3853e` | 1227 |
| `error-cursor-expired.json` | `9f85ec830e0e61d0ac75656a980793d6d5c4020265d42ed192693659e1540437` | 162 |

Money values are canonical 1–19 digit non-negative micro-CNY strings; 20
digits fail closed. Source DTOs are decoded only at the adapter boundary and
mapped into X-Insight's source-neutral normalized evidence model.

## Activation And Cursor Invariants

Activation performs one fetch with an empty input cursor. Only an empty,
`has_more=false` bootstrap page is accepted. Its nonempty opaque cursor,
high-watermark, source instance and authorization reference are written to
`adapter_import_states`.

Starting a new integration campaign must not delete or overwrite this row by
hand. Schema 20 adds the source-neutral `connector-epoch` lifecycle documented
in [Connector Checkpoint Epoch V1](connector-checkpoint-epoch-v1.md). It archives
the complete old delivery epoch and atomically rebinds the active row only at a
stopped, error-free Cursor/high-watermark boundary. The transition does not
change completed-execution DTOs, M0 projection, event identity/hash,
item-linked provenance, or existing Evidence rows.

After activation:

- every page must match the activated source instance;
- a nonempty page must return a different `next_cursor`;
- an empty page must return the current cursor;
- an empty `has_more=true` page therefore fails either the no-progress or
  empty-advance invariant;
- terminal and page timestamps must be well-formed metadata, but are never
  compared with the activation time to decide eligibility; and
- delayed materialization is valid when an execution became terminal before
  activation but entered the outbox after the activation cursor.

The opaque source cursor/high-watermark, not cross-host clock ordering, defines
activation-forward consumption.

## At-Least-Once Commit

Each page is processed in source order:

1. validate the outer item and strict inner bytes;
2. map and persist all normalized events for one item through the existing
   `EvidenceStore` transaction;
3. require either all inserted or all duplicate/no-op; conflicts and partial
   mixes fail closed;
4. repeat for every item; and
5. advance the durable cursor in a separate state transaction only when every
   item converged.

Partial-page persistence is permitted. If the process stops before
`CommitPage`, the durable cursor remains unchanged and the page is replayed.
Previously committed events then converge as duplicate/no-op. V1 does not
claim an atomic transaction across all evidence writes and the cursor row.

Per-item provenance records the source payload SHA-256, source terminal time,
revision and contracted-pull classification. It leaves both
`source_watermark` and `import_watermark` empty because page high-watermark and
next cursor are not durable item identity. `adapter_import_states` is the
exclusive cursor/high-watermark authority.

The outer `terminal_available_at` and immutable inner
`terminal.available_at` must represent the same instant after UTC
normalization at PostgreSQL microsecond precision. A historical
cross-representation residual below one microsecond is accepted without
rewriting the inner bytes or SHA-256; a residual of one microsecond or more
fails closed. RFC 3339 shape, terminal semantics and all other identity
bindings remain strict.

## Transport And Credential Boundary

The source client uses one fixed endpoint, a bounded body/page size, bounded
connect/header/request timeouts, identity encoding, no environment proxy and
no redirects. A feed-only bearer service key is read from a restricted file or
systemd credential; it is never accepted on the command line or logged.

The raw bearer token is generated only on the X-Insight target by root
`xinsightctl feed-credential prepare`, using 32 `crypto/rand` bytes and exact
`ur-xf1-` plus 64 lowercase hexadecimal grammar. UniRouter receives only the
strict six-field
`unirouter.xinsight.feed-credential-registration.v1` bundle: format, prefix,
domain-separated verifier and independent operation ID. The staged `.next`
file is exclusive-created, regular, root-owned `0600`, single-link and
no-follow; durable nonsecret state binds exact operation/verifier. Activation
first uses the staged token in process memory against the exact configured
source and requires the strict
`completed_execution_feed.connection_paused` retryable `503` envelope. Only
that proof may cross the atomic live-file replacement barrier and restart the
worker; `401`, `403`, `200`, malformed responses and transport failures leave
the old live file plus `.next` intact. A crash before final state can only
resume with the same public binding; abort is permitted only before the
replacement barrier. Rotation keeps the old token active through prepare and
follows paused stage → staged-token authenticated paused 503 → atomic local
switch/restart → promote/revoke/resume.

HTTPS is mandatory by default. Private-tunnel HTTP is available only when
`XINSIGHT_UNIROUTER_FEED_ALLOW_PRIVATE_TUNNEL_HTTP=true` is explicitly set and
the fixed literal endpoint is loopback/RFC1918/Tailscale/ULA. Address class
alone is not tunnel proof; the deployment baton must verify the authenticated
tunnel before enabling this setting.

## Process And Lifecycle

`xinsight-importer unirouter pull-feed` is the single writer-capable long-lived
worker. `xinsight-server` remains reader-only and loopback-only. The worker has
durable `inactive`, `idle`, `polling`, `backoff`, `error` and `stopping`
status plus safe counters/error codes.

Install places the worker unit but leaves it disabled and stopped. Upgrade,
rollback, restore, recovery and bounded abort preserve the prior
present/active/enabled state, stop the worker before importer-role gating and
restore it only after migrations, application verification and importer login
succeed. Release qualification does not run feed-credential prepare and proves
live/staged key plus operation state absent while the worker remains dormant.
V1 has no leader-election or HA framework.

## Evidence Claims

Local persistence completion time is not an HTTP/API readback measurement.
Target and External-integration acceptance require a later bounded readback
against the exact deployed builds and source. Local tests, CI, Target,
External integration and Production are recorded separately.
