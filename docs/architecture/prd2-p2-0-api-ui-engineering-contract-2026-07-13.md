# PRD2 P2-0 API And UI Engineering Contract

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

Status: P2-0 engineering freeze, P2-1 backend and P2-2/P2-3 feature UI merged
Prepared: 2026-07-13
Product decision owner: User
Execution owner: Codex
Implementation-kickoff baseline:
`76f3f0ebe9e3a83598fe17af5067fe9ccd003789`
P2-0 merged baseline:
`4b6dca44a0947c22bd4a6059a0c96fbb475c4da8`
P2-1 merged baseline:
`b754100a4eee9024707fe9c189c71ee579966a04`
P2-2/P2-3 merged baseline:
`582e9692fc0d35f2e93c4105a891e09207bb5f8d`

Current amendment (accepted 2026-07-18): the whole live completed-execution
feed slice is deferred from the first formal version. Any pull lifecycle,
cursor, retry or latency language retained below is a historical future
contract candidate, not an active release requirement. The default-disabled
mock consumer remains test scaffolding only; the operator-mediated sidecar is
the binding integration path.

## Purpose

This contract turns the accepted B0-D01 through B0-D08 and B0-D23 through
B0-D31 product defaults into exact X-Insight-owned implementation choices. It
defines the first stable PRD2 read surface, resource envelope, freshness truth
model and frontend toolchain before feature UI code depends on them.

P2-0's contract, primitives, exact frontend toolchain, same-origin shell and
disabled/mock consumer are merged. P2-1 delivered the six bounded API paths,
schema projection, provenance and resource bounds; P2-2/P2-3 consume them in a
Local- and CI-verified merged feature UI. This contract does not replace the accepted
2026-07-11 sidecar-only cross-project runtime decision. The live completed-
execution feed is deferred under the accepted 2026-07-18 decision; its
default-disabled mock consumer and candidate boundary do not constitute an
enabled integration or adapter.

The machine-readable API source is
[`api/openapi/prd2-v1.yaml`](../../api/openapi/prd2-v1.yaml). If prose and that
file disagree, implementation stops until both are reconciled in one change.

## Product And Fact-Source Invariants

- Persisted normalized evidence events, provenance, source references and
  capture gaps remain the fact source.
- Execution summaries and timeline nodes are rebuildable read models. They are
  not a second evidence ledger.
- One persisted evidence event produces one timeline node.
- Chronology never implies causality. Only an explicit, supported source parent
  reference may create a declared edge.
- Raw prompts, responses, messages, authorization headers, credentials and
  unapproved content are never returned or persisted by this slice.
- A source-type/schema reservation is not an adapter.
- Search visibility does not mean the timeline is complete.
- An opaque cursor or execution ID is not authorization. The service remains
  loopback-only under the accepted single-principal tunnel posture.

## Stable Browser And API Routes

### Browser routes

| Route | Purpose | Stability |
| --- | --- | --- |
| `/executions` | Execution search and bounded results | PRD2 product route |
| `/executions/{execution_id}` | Execution detail, timeline, source and gaps | PRD2 product route |
| `/manual/zh-CN/` | Release-matched offline product manual | P2-4 route; not implemented in P2-0 |

The browser may encode selected event state in a query parameter. These routes
are same-origin product navigation, not a UniRouter durable-job/deep-link
contract.

### API routes

| Method/path | Purpose |
| --- | --- |
| `GET /api/v1/executions` | Bounded execution search/list |
| `GET /api/v1/executions/{execution_id}` | Execution summary/detail |
| `GET /api/v1/executions/{execution_id}/timeline` | Paged evidence-derived nodes |
| `GET /api/v1/executions/{execution_id}/events/{event_id}` | One safe event detail inside its execution |
| `GET /api/v1/executions/{execution_id}/gaps` | Independently paged capture gaps |
| `GET /api/v1/sources` | Bounded allowlisted source options for search |

Legacy request-ID development routes remain temporarily available during
P2-1 migration, are marked deprecated, and receive no stable compatibility
promise. They are removed only in a separately recorded compatibility change.

## Execution Identity

The public execution ID is deterministic and source-qualified:

```text
xe1_ + lower_hex(
  SHA-256(
    UTF-8("xinsight-execution-v1" + NUL +
          canonical_lowercase_source_system_uuid + NUL +
          exact_request_id)
  )
)
```

Contract:

- regex: `^xe1_[a-f0-9]{64}$`;
- the full 256-bit digest is retained; it is never truncated;
- request ID comparison is byte-exact valid UTF-8 after rejecting
  leading/trailing whitespace and Unicode control characters; it is not
  case-folded or Unicode-normalized;
- source system UUID is part of the digest, so equal request IDs from different
  sources cannot merge;
- the source request ID remains safe display/search metadata; and
- changing this algorithm requires a new prefix/version and compatibility
  mapping. Existing `xe1_` values never change in place.

The hash provides collision-resistant opaque naming, not identity
authorization, authenticity or secret protection.

## Search Contract

### Parameters

| Parameter | Rule |
| --- | --- |
| `request_id` | Optional exact value, 1–256 valid UTF-8 bytes; surrounding whitespace and Unicode control characters are rejected; mutually exclusive with `request_id_prefix`. |
| `request_id_prefix` | Optional byte-prefix, 3–128 valid UTF-8 bytes; surrounding whitespace and Unicode control characters are rejected; `%`, `_` and `\\` are literal; mutually exclusive with `request_id`. |
| `source_system_id` | Optional canonical UUID exact match. |
| `outcome` | Optional enum: `success`, `failure`, `partial`, `unknown`. |
| `has_gaps` | Optional strict lowercase `true` or `false`. |
| `from` / `to` | Optional RFC3339 timestamps. Default `to=as_of`; default `from=to-24h`; `from < to`; maximum span 31 days. |
| `limit` | Default 50; integer range 1–100. Values outside the range are rejected, never clamped. |
| `cursor` | Opaque token, maximum 2,048 bytes. When present, all explicit filters must match the cursor. |

The five UI filter groups are identifier (exact/prefix mode), time, source,
outcome and capture-gap state. No arbitrary JSONB, regex, full-text or payload
query enters PRD2.

### Time and outcome

- Search time uses the execution's latest effective evidence time inside the
  fixed snapshot. Effective event time is
  `COALESCE(occurred_at, observed_at, ingested_at)`.
- `as_of` is server-selected UTC and bounds rows by both declared ingestion and
  database visibility: `evidence_events.ingested_at <= as_of`,
  `evidence_events.created_at <= as_of` and
  `capture_gaps.created_at <= as_of`. The database fence prevents a row inserted
  later with a backdated `ingested_at` from entering an existing traversal.
- An execution outcome is a deterministic projection of normalized
  `outcome.outcome_status`. For an execution whose normalized events are all
  from `source_type=unirouter`, exactly one event with
  `source_event_type=call_log` supplies the public execution outcome. Every
  other event keeps its own outcome and remains timeline-visible. With zero or
  multiple call-log events, or for a non-UniRouter execution, precedence remains
  `failure`, `partial`, `success`, then `unknown`; the projection does not guess
  terminal authority. A rejected decision remains visible in event/decision
  detail; it is not a new unsupported outcome enum.
- Outcome is computed at query time. Deploying the unique-call-log rule may
  reclassify previously stored recovered UniRouter executions from `failure` to
  `success` and move them between outcome-filter result sets without modifying
  evidence rows. A cursor fixes data visibility at `as_of`, not the deployed
  projection code; clients should start a new traversal after this semantic
  upgrade. The response shape and outcome enum are unchanged.
- A search item with any bounded capture-gap row at `as_of` has
  `has_gaps=true`. Full gap count is computed for detail, not inferred from a
  page length.

### Ordering and response

Search order is always:

```text
last_effective_at DESC, execution_id DESC
```

Response shape is `items` plus `page`. `page.count` is the number in this page;
`page.has_more` and `page.next_cursor` describe continuation. Search does not
return a potentially expensive global total. Detail returns exact full event
and gap counts at its `as_of`.

## Cursor Contract

Each cursor is unpadded base64url of strict canonical JSON with:

- version `xinsight.cursor.v1`;
- endpoint kind (`execution_search`, `timeline` or `gaps`);
- server-selected `as_of`;
- SHA-256 of canonical filters and sort;
- the last unique ordering tuple; and
- execution ID for execution-scoped pages.

Unknown/missing/duplicate fields, invalid base64, non-canonical types and tokens
over 2,048 bytes are rejected. A cursor is a compatibility token, not a bearer
credential or tamper-proof authorization artifact. It is valid for 24 hours
from `as_of` on the single-node PRD2 service:

- malformed token: `400 invalid_cursor`;
- filter, endpoint or execution mismatch: `409 cursor_mismatch`;
- age over 24 hours: `410 cursor_expired`.

The fixed `as_of` prevents newly imported rows from creating page duplicates or
omissions inside one traversal. Restarting a search receives a new snapshot.

Execution-scoped reads accept an optional `as_of` RFC3339 UTC value. The UI
uses the search page/detail `as_of` for detail, first timeline page, event detail
and first gap page so counts and nodes describe the same investigation snapshot.
If omitted, the server chooses current UTC. A client-supplied value must be no
more than one minute in the future and no older than 24 hours; older snapshots
return `410 cursor_expired`. A paging cursor's `as_of` must match any explicit
value or return `409 cursor_mismatch`.

## Detail, Timeline, Event And Gap Semantics

### Execution detail

Detail returns the exact source-qualified identity, safe source/request fields,
projected outcome, first/latest effective time, exact event/gap counts, payload
mode summary, proof class, integration mode and freshness state. It never
returns source-system metadata, deployment/owner refs or unbounded JSON.

### Timeline

- default limit 100, maximum 200;
- ascending order by effective time, `ingested_at`, then event UUID;
- event UUID is the stable node ID;
- parent ID is emitted only when the source contract explicitly declares a
  parent that resolves inside the same execution;
- missing, cross-execution or unsupported parent references are represented as
  unknown/gap evidence, never inferred; and
- page count, `has_more` and exact `total_event_count` are separate fields.

### Event detail

An event is resolved only by `(execution_id, event_id)`. The DTO contains an
allowlisted set of correlation, identity, capability, decision, usage, outcome,
payload-policy and field-provenance groups plus source ref/hash/schema/fidelity.
`raw_source_excerpt` is never returned even when the stored object is empty.

### Capture gaps

- default limit 50, maximum 100;
- ascending order by `created_at`, then gap UUID;
- exact `total_gap_count` is separate from page count;
- `candidate_gap_types` means ordered, unique candidate explanations for a
  generated `not_available_from_source` gap; an empty list means no candidate
  was asserted, not that no gap exists; and
- confirmed and candidate states are rendered separately.

## Provenance And Freshness Vocabulary

Field-group provenance uses the existing exact enum:

| Code | Chinese UI meaning |
| --- | --- |
| `direct` | 来源直接记录 |
| `derived` | X-Insight 按已声明规则派生 |
| `declared` | 来源或策略声明 |
| `suppressed` | 因策略而抑制 |
| `gap` | 已知采集缺口 |
| `unknown` | 当前证据无法确定 |
| `not_applicable` | 该字段不适用（只在字段明细中使用） |

Freshness has two independent dimensions:

- `integration_mode`: `sidecar`, `completed_execution_pull`, or
  `synthetic_fixture`;
- `sync_state`: `syncing`, `current`, `stale`, or `gap`.

`current` means complete for the displayed source revision/artifact—not a claim
that the underlying live system can never change. `syncing` means search
visibility exists but the advertised timeline revision is not fully imported.
`stale` means the source/import watermark is older than the contracted check
window or a sidecar has no live freshness assertion. `gap` means the source or
import explicitly declares unavailable evidence; retry alone cannot fill it.

Every detail/list item exposes, when known:

- source terminal/generated time;
- imported/search-visible time;
- source revision and imported revision;
- source and import watermarks;
- timeline-complete boolean; and
- last successful sync time.

Unknown fields stay absent/unknown. The UI never fabricates a timestamp or
revision.

## Stable Error Contract

All non-2xx API responses use `application/problem+json`:

```json
{
  "type": "urn:xinsight:error:invalid_argument",
  "title": "Invalid request argument",
  "status": 400,
  "code": "invalid_argument",
  "correlation_id": "01J...",
  "retryable": false,
  "fields": [
    {"name": "limit", "reason": "must_be_between_1_and_100"}
  ]
}
```

`title` is bounded canonical English. The `zh-CN` UI maps by stable `code` and
field `reason`; it does not display raw backend/SQL errors. `detail` is omitted
unless it is an allowlisted bounded public explanation.

| HTTP | Code | Retryable |
| --- | --- | --- |
| 400 | `invalid_argument`, `invalid_cursor` | no |
| 404 | `execution_not_found`, `event_not_found`, `route_not_found` | no |
| 405 | `method_not_allowed` | no |
| 409 | `cursor_mismatch` | no |
| 410 | `cursor_expired` | no; restart search |
| 429 | `request_rate_limited` | yes, bounded `Retry-After` |
| 503 | `temporarily_overloaded`, `database_unavailable` | yes |
| 500 | `internal_error` | no automatic retry by UI |

Correlation IDs are `xc_` plus 128 bits of server-generated lowercase hex and
contain no source/request data.
No `err.Error()`, SQL text, DSN, path, host or stack trace enters the response.

## Resource And Failure Envelope

### HTTP/read service

| Control | P2-0 value |
| --- | --- |
| Listen default | `127.0.0.1:8080`; non-loopback requires a later accepted access profile |
| Request URL | maximum 8 KiB |
| Request headers | maximum 16 KiB |
| Read-header timeout | 5 seconds |
| API handler/query deadline | 3 seconds |
| Write timeout | 10 seconds |
| Idle timeout | 60 seconds |
| In-process API concurrency | 32; excess returns bounded 503 |
| Per-process rate | 20 requests/second, burst 40; excess returns 429 |
| PostgreSQL pool | max open 10, max idle 5, connection lifetime 30 minutes, idle time 5 minutes |

Health remains process-only; readiness proves DB connectivity and supported
schema compatibility. Static UI assets do not consume API concurrency permits.

### Bounded import

| Control | P2-0 value |
| --- | --- |
| Snapshot file | maximum 64 MiB before JSON decode |
| Source rows/events | maximum 10,000 per invocation |
| Capture gaps | maximum 10,000 per invocation |
| Request IDs | maximum 1,000 per invocation |
| Normalized event | maximum 256 KiB canonical JSON |
| Persistence writers | one per deployment under the versioned shared maintenance-lock protocol |

Limit failure occurs before partial persistence. The importer remains a
synchronous CLI, not a durable job. P2-1 adds executable boundary/fault tests.

### Failure behavior

- X-Insight unavailability never blocks or changes a UniRouter request.
- The deferred pull candidate would retain its last committed cursor after a
  failed poll; no live poller is activated by this contract.
- Search/UI show stale/gap state rather than replaying hidden retries forever.
- DB saturation fails bounded API requests with 503; it does not open
  unbounded goroutines/connections.
- Operator-mediated sidecar import remains the binding path for one explicitly
  approved artifact; it is not described as fallback from an active pull path.

## Diagnostic 1x Workload And Budgets

The P2-0 reference workload is deliberately explicit rather than a 100x claim:

| Dimension | 1x reference |
| --- | --- |
| Executions | 100,000 over 31 days |
| Evidence events | 1,000,000; median 10 and p99 200 per execution |
| Capture gaps | 200,000; p99 50 per execution |
| Sources | 10 |
| Concurrent UI/API reads | 8 sustained, 32 bounded peak |
| Host | Ubuntu 22.04 x86_64, 4 vCPU, 8 GiB |
| Database | dedicated loopback PostgreSQL 16 Compose profile |

Diagnostic Local/CI/VM targets after warm-up:

- execution search p95 <= 750 ms and p99 <= 2 s;
- detail p95 <= 500 ms;
- timeline/gap page p95 <= 750 ms;
- no page duplicate/omission at fixed `as_of`;
- completed execution terminal-availability to search visibility p95 <= 10 s
  remains a mock-only diagnostic and is not a current release gate; and
- zero unbounded queue, goroutine, connection or response growth at the hard
  concurrency limit.

The read-path targets are release diagnostics for the supported profile, not
Production SLAs. The deferred mock-feed target is retained only for engineering
comparison. Target and External measurements are separately recorded. A 10x
exploratory run may inform the next plan but cannot establish 100x or HA
readiness.

## Frontend And Packaging Freeze

Exact initial toolchain:

| Item | Version/choice |
| --- | --- |
| Node.js | `22.23.1` |
| npm | `10.9.8`; lockfile v3; `packageManager` pinned |
| TypeScript | `7.0.2`, strict mode |
| React / React DOM | `19.2.7` / `19.2.7` |
| Vite / React plugin | `8.1.4` / `6.0.3` |
| Vitest / jsdom | `4.1.10` / `29.1.1` |
| Testing Library | React `16.3.2`, DOM `10.4.1`, user-event `14.6.1` |
| Browser tests | Playwright `1.61.1` |
| OpenAPI validation | `@apidevtools/swagger-parser` `12.1.0` |
| Router/query/state | No third-party library in P2-0; History API and bounded native `fetch` wrapper |

The selection was verified against the package registry on 2026-07-13. Exact
dependencies live in `frontend/package-lock.json`; the lockfile, not this table,
is the install source.

Build output is committed under `internal/webui/dist/` so a clean Go checkout
can compile embedded assets without a pre-existing Node build. CI rebuilds the
assets and fails on a diff. The Go server serves them from `embed.FS`, uses SPA
fallback only for product routes, never intercepts `/api/`, `/healthz` or
`/readyz`, and applies immutable caching only to content-hashed assets.

The UI is `zh-CN` product copy. Protocol names, JSON fields, CLI flags and error
codes remain English. No general i18n framework, CDN, online font, remote image,
cross-origin API or separate frontend service is added.

## Deferred Pull-Feed Candidate Boundary

This section is retained for a future bilateral decision only. It does not
authorize a live endpoint, credential, automatic poller, activation cursor or
field latency gate in the first formal version.

The X-Insight-owned consumer interface freezes only these invariants:

- default disabled;
- source supplies completed execution identity, terminal-availability time,
  monotonically comparable revision, timeline-complete state and bounded
  sanitized metadata/evidence or an integrity-bound fetch reference;
- source cursor advances only after X-Insight commits every accepted item;
- replay of the same execution revision is idempotent;
- a later revision may add evidence but cannot silently rewrite an existing
  idempotency key with different canonical content;
- activation begins at an explicitly approved cursor/time and performs no
  broad historical auto-backfill;
- recommended mock poll interval is 2 seconds, page maximum 50 and decoded page
  maximum 8 MiB; failures back off from 1 to 30 seconds; and
- source/gateway behavior is unaffected by X-Insight failure.

The live URL, authentication method, source cursor encoding, revision type,
response envelope, integrity/authenticity proof, retention/custody and precise
terminal semantics remain in the shared proposal. They are intentionally not
guessed into executable production code.

## P2-0 Exit Evidence

- prose and OpenAPI contract reconciliation;
- identity/cursor/error golden tests;
- exact dependency lock and deterministic asset rebuild;
- component test proving the `zh-CN` shell and all four freshness labels;
- Go embed/static route tests and API route non-interception;
- mock consumer contract/failure-isolation tests;
- import/resource-boundary test plan mapped to P2-1;
- documentation link/status reconciliation; and
- Local and CI recorded separately, with no Target/External/Production claim.

P2-0 satisfied this exit gate in PR
[#29](https://github.com/foigent/x-insight/pull/29) and was squash-merged as
`4b6dca44a0947c22bd4a6059a0c96fbb475c4da8` after all three GitHub Actions jobs
passed. Later implementation evidence is recorded separately in
[P2-1 local backend and 1x diagnostic](../evidence/prd2/p2-1-local-backend-and-1x-diagnostic-2026-07-13.md)
and
[P2-2/P2-3 investigation UI Local evidence](../evidence/prd2/p2-2-p2-3-investigation-ui-local-evidence-2026-07-13.md).

## Related Records

- [Phase 2 Batch 0 Scope And Decision Register](../planning/phase-2-batch-0-scope-and-decision-register-2026-07-13.md)
- [PRD2-M1 Field Proof](../planning/prd2-m1-unirouter-field-validation-implementation-plan-2026-07-13.md)
- [Runtime Contract Capability Status](runtime-contract-capability-status.md)
- [PRD2 Single-Node Upgrade And Rollback Contract](prd2-single-node-upgrade-and-rollback-contract-2026-07-13.md)
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-11-prd5-runtime-contract-v1-boundary.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/contracts/2026-07-13-prd2-completed-execution-pull-feed-candidate.md`
