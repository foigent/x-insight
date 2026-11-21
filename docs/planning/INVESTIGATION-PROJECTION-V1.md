# Investigation Projection V1

Status: locally implemented candidate for Phase 1–4 acceptance. This document
does not authorize push, merge, release, deployment, credential creation, or
real external traffic.

## Product decisions

1. X-Insight may search, read, and cite Evidence. It does not replay requests,
   mutate routing, or expose provider credentials.
2. V1 covers identity, routing, provider attempts, retry/fallback, billing,
   usage, and logging. Tool/MCP/multi-Agent stages remain out of scope until a
   real source contract exists.
3. Human and Agent readers consume the same deterministic projection.
4. Customer-demo data may be deterministic synthetic Evidence only when the UI
   labels its proof class and source truthfully.

## Read API

`GET /api/v1/executions/{execution_id}/investigation?as_of={RFC3339Nano}`

The fixed `as_of` snapshot is required for a reproducible deep link. A response
contains at most 200 Evidence nodes and 100 capture gaps. Larger executions
return `integrity.truncated=true` and
`integrity.projection_complete=false`; complete-chain claims are suppressed.

Core fields:

- `schema_version=xinsight.investigation-projection.v1`
- `presentation_mode`
- `summary_claims[]`, each with exact `evidence_event_ids`
- `stage_coverage[]`
- `nodes[]`
- `edges[]`, each with `edge_basis`
- `gaps[]`
- `freshness`
- `integrity`

The projection never includes raw prompt, raw response, raw upstream error,
raw IP, authorization headers, API keys, credentials, or
`raw_source_excerpt`.

## Evidence revisions

In the schema 48 development candidate, a fresh investigation returns an
opaque `evidence_revision` alongside its UTC `as_of`. Use both values to
reopen the same committed evidence. Search, timeline, and gap cursors carry
the revision together with their existing filter and ordering bindings.
Citations and Case execution references include the revision when present.

The version bounds evidence events, recorded gaps, import provenance and
the links between provenance and events. Connector health, gap resolution,
reader grants and access to original bodies retain their own current
authorization and lifecycle. Workbench and Runtime readings retain their
own membership and cutoff. A revision remains usable after a logical
restore that retains it, and an unknown revision returns
`evidence_revision_unavailable` with HTTP 410.

A link containing only `as_of` retains its existing 24-hour time-filter
behavior. A later commit can still appear under the timestamp. Timestamp-only
Case references already stored in the database stay timestamp-only. A new
Case execution request without a version pins the evidence visible at
submission and reuses the first adoption when the same request is retried.
The scoped Review flow continues to store the exact selected evidence and gap
identities. Its paired Workbench and Runtime references keep their own rules.

The migration adds a baseline revision for predecessor rows without rewriting
evidence. Each subsequent import publishes its UUID and order in the same
transaction as its new data. Order is allocated under a transaction-scoped
lock after the import writes. UUIDs and publication order survive
`pg_dump` / `pg_restore` independently of database transaction IDs.
Keep the published FPV25 schema 47 profile and release authority intact.
For development qualification, compare the existing `/readyz` schema and
migration-set digest against the generated schema 48 contract.

## Presentation modes

- `causal_trace`: at least one parent edge resolves from a source-declared
  `parent_span_id`.
- `chronology`: multiple operational events exist but no declared parent edge
  resolves.
- `terminal_snapshot`: the source retained only terminal/usage/billing/gap
  facts. The UI must say that this is not a trace.

Chronological adjacency, matching timestamps, shared providers, and matching
request/trace IDs never create an edge.

## Deterministic claims

V1 claim codes are fixed rules:

- `provider_failed_then_retry_scheduled`
- `primary_provider_failed_then_fallback_succeeded`
- `execution_completed_successfully`

The recovered-fallback claim requires provider failure, retry scheduling,
fallback selection, and a later successful terminal/call-log node for the same
fallback provider, in that order. It is emitted only when both the source
profile and bounded projection are complete. An LLM is not called to generate
core claims.

## Stage coverage

The ordered stages are:

1. `identity`
2. `routing`
3. `provider`
4. `retry_fallback`
5. `billing`
6. `usage`
7. `logging`

Each stage is exactly one of:

- `present`: at least one Evidence event directly supports the stage.
- `gap`: an explicit capture gap affects the stage and no supporting event is
  present.
- `not_applicable`: a complete source profile proves the stage did not apply.
- `unknown`: the available Evidence cannot establish any stronger state.

`timeline_complete` is retained as source-profile input; it is not presented as
global completeness. The response separately reports source-profile, import,
projection, and causal coverage facts.

## Human UI acceptance

The default post-login execution view is Investigation:

- outcome and recovered-fallback headline are readable without opening a node;
- proof class, data origin, gaps, duration, token count, and exact cost remain
  visible;
- the stage strip exposes present/gap/not-applicable/unknown;
- provider attempts read as an execution path, not equal-weight cards;
- selecting a node opens allowlisted Facts, Evidence citation, and attached
  Gaps;
- Chronology and Proof remain available as secondary views;
- terminal-only evidence displays “terminal snapshot / source did not provide
  trace”;
- keyboard, focus, narrow-screen, loading, empty, error, and stale states remain
  operable.

The primary demo acceptance fixture is the metadata-only
`fixtures/unirouter_execution_evidence/recovered_fallback.v2.json` bundle. It
carries the pinned UniRouter profile and bundle-schema digests and must remain
visibly classified as synthetic fixture data.
