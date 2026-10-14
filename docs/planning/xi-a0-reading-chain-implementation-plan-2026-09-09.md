# XI-A0 Cost Reading Chain Implementation Plan

Status: approved for isolated Local implementation on 2026-09-09. This plan
does not authorize push, pull request, merge, release, deployment, real source
access, credential changes, or UniRouter modification.

## Outcome

Deliver an X-Insight-owned cost reading chain over already validated and
persisted `unirouter.completed-execution.v1` evidence:

1. persist the rebuildable economic projection in the same transaction as its
   Evidence events;
2. create an authorization-bound, expiring reading with fixed membership;
3. expose exact charged and calculated-reference micro-CNY, series,
   contribution, detail, source-observation, and saved-filter APIs;
4. render the real data through the production API handler and embedded React
   UI; and
5. preserve the V1 contract and all existing Evidence, investigation, access,
   connector, and release identities.

The delivery is Local only. It makes no Target, External Integration,
Production, runtime-authority, or UniRouter release claim.

## Contract decisions

- Economic identity is `source_system_id + source_version + source_ref`.
  Adapter identity and delivery epoch are not billing identity. Exact replay is
  a no-op; conflicting immutable source content fails the Evidence transaction.
- Money stays canonical decimal text and PostgreSQL `numeric`; float and
  `int64` aggregation are forbidden. The official 17-micro fixture must remain
  exact through database, API, and UI.
- A reading uses repeatable-read membership, records authorization and
  interpretation revisions, and never absorbs late facts. A new reading is
  required after late arrival. Expiry, ownership, and current authorization are
  checked on every read.
- Summary, series, contribution, and detail views read only the fixed members.
  Top-N rows plus `other` plus `unassigned` close to one denominator. Full
  pagination has `other=0`; `unassigned` and the denominator are identical
  reading-level constants on every page and must be counted once by clients.
- Current completed-execution V1 has an opaque cursor/high-watermark and no
  source-owned time-bounded completeness assertion. Capture time, last receive
  time, source checkpoint, and projection state are displayed as separate
  observations. They never imply period completeness. Known counts and amounts
  remain visible and are explicitly incomplete relative to the source period.
- Source-wide health and gap state are `scope_limited_unknown` when a grant is
  project-restricted. This does not hide the authorized reading members or
  their known amounts.
- V1 `usage.model` and `project_id` support model and source-project display.
  Cost-project, account, application, actor, and business-purpose attribution
  remain unavailable or unknown; no value is inferred.
- Saved-filter idempotency is operation-scoped and stable after later entity
  revisions. Ownership, optimistic revision, CSRF, and database capability
  grants are enforced.

## Implementation sequence

1. Converge schema 24 migration, projection, reading queries, access rules,
   host-role grants, strict request/query decoding, complete OpenAPI DTOs and
   Problem responses, and UI behavior.
2. Verify clean 1..24 and 23..24 PostgreSQL 16 migration paths, exact duplicate
   backfill, conflicting backfill rollback, role boundaries, transaction
   rollback, and a schema-24 contract generated from the real database.
3. Run the official V1 fixture through adapter, EvidenceStore, projection,
   reading, production API handler, embedded UI, and browser. Cover replay,
   adapter change, source distinction, late arrival, revocation, cross-user and
   project denial, detail denial, saved-filter replay/concurrency, and all
   documented Problem cases.
4. Exercise 300,000 30-day members, 70,000 7-day members with ten concurrent
   readers at a 20 requests/second short peak, one real 500,000-member reading,
   a 500,001-member rejection with no partial state, query plans, storage, and
   at least twenty Local commit-to-new-reading visibility samples. Runbook
   thresholds remain diagnostic targets, not SLA claims.
5. Run all affected and whole-repository Go, PostgreSQL, frontend, OpenAPI,
   conformance, generated-asset, and diff checks. Commit one coherent local
   change and return exact commit/tree plus A0-01..12 evidence.

## Version and release boundary

Schema 24 is the current `0.2.0-dev` Local development identity. Existing
rc.13 release config, compatibility matrix, schema-23 contract, campaign
authority, and signed-release identity are historical and remain byte-stable.
The schema-24 runtime must use a schema-24 generated contract. Any attempt to
package current schema-24 code as the existing rc.13/schema-23 release must
fail closed; compatibility and checksum checks are not weakened. No release is
generated or signed in this batch.

## Technical-debt accounting

Two pre-existing duplications are in scope:

- derive current host-role SQL table arrays from the existing Go grant lists,
  while retaining the rc.10 historical constant until that compatibility path
  is retired; and
- route legacy frontend GET calls through the existing authenticated JSON /
  Problem transport while preserving headers, cache, credentials, abort, 401,
  and error behavior.

New reading invariants and fixes to this batch's draft code are product work,
not claimed as removed legacy debt. Final delivery separately lists resolved,
compatibility-retained, and outstanding debt with regression evidence.

## Stop conditions

Stop for a decision only if completion would require changing completed-
execution V1, rewriting historical release identity, weakening authorization or
evidence boundaries, accessing real private sources or credentials, contacting
UniRouter, deploying, or reducing the accepted A0 result. Ordinary local
implementation and test failures are resolved within this plan.
