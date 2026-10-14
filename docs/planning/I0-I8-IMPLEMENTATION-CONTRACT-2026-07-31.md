# X-Insight I0–I8 Implementation Contract

Date: `2026-07-31`

Status: `LOCAL_RELEASE_CANDIDATE_REVIEW`; the User selected release identity
`0.3.0-rc.1` and product-repository Foigent Protocol `v2.0.0` adoption.
External release, authority repair and runtime gates remain closed for branch
`codex/i0-i8-main-20260801`.

Evidence boundary: this contract authorizes local source changes and local
verification only. It does not authorize push, pull request, merge, release,
deployment, credentials, traffic, Feed activation, Target database mutation,
or a Production claim.

## 1. Baseline

The implementation starts from exact commit
`3cec861024d1f85b30abab17c5207b0f7b8cb8c8` and tree
`d309e845b6c168ff3a501b5421db1202b24600df`.

The final integration review resumed from exact commit
`cec8a17c9d42827d661df0287da7a59443c91e2f` and tree
`3bbafa90e0cea1307bdbf789cbb1f1a9c33e24e4` in the dedicated worktree. The
original dirty checkout and the separate Phase 0–4 worktree were read-only.

The existing M0 `m0.1` Evidence identity, event hash, provenance, Capture Gap,
source reference, idempotency and read-model semantics are compatibility
constraints. Product-local workspace records may reference Evidence; they may
not mutate or impersonate Evidence.

## 2. Product Result

X-Insight is an evidence-custody and investigation product for AI execution
systems. It must help a human or Agent answer:

1. what happened;
2. who initiated or performed each observed action;
3. which systems and explicit relationships were involved;
4. where the execution ended;
5. which statements are directly observed, source-declared, deterministically
   derived, or missing;
6. which claims remain unprovable;
7. which immutable Evidence IDs and hashes support the answer.

The default reading order is:

`outcome story → causal path → completeness → evidence → chronology`.

## 3. Architecture

```text
Source Connectors
  → immutable M0 Evidence / Provenance / Capture Gaps
  → deterministic investigation projection
  → versioned Read API
  → shared human and Agent read contract

Product-local Workspace Store
  → cases / findings / annotations / saved views / identities / feedback
  → references M0 identities, never rewrites M0

Discovery Layer
  → bounded, evidence-linked hypotheses
  → deterministic support score + abstention + limitations
  → never promoted to M0 fact or automatic enforcement
```

PostgreSQL remains the only database. No graph database, queue, general policy
engine, raw prompt store, or LLM-generated causal edge is introduced.

Browser Session authentication and Agent bearer authentication are separate
entry mechanisms. They converge only after authentication onto the same
permissions and deterministic read response.

## 4. Increment Contracts

### I0 — Task and visual contract

- Freeze the A+C precision-workbench and evidence-dossier visual system.
- Use deterministic fixtures for direct success, recovered fallback, parallel
  attempts, missing parent, terminal-only and bounded 200-node views.
- Test the three-minute task: find execution, explain outcome/path/unknowns,
  cite Evidence, and add it to a Case.

### I1 — Investigation Inbox and Execution Explorer

- `/` and `/inbox` are bounded recent-work entry points, not trend dashboards.
- `/executions` is the complete Explorer with URL-addressable filters and a
  fixed `as_of`.
- Empty, loading, stale, partial, unauthorized and unavailable are distinct.

### I2 — Execution Story and causal DAG

- The story names start actor/system, action, explicit path, end state and
  unknowns.
- DAG edges come only from API `edges[]`.
- Chronological adjacency never becomes causality.
- Missing parents and orphans are visible; a Capture Gap is not rendered as an
  execution step.
- Selection is shared between Story, DAG, Chronology and Evidence drawer.

### I3 — Completeness and Connector Trust

- Source, Import, Projection and Causal completeness are four separate axes.
- Freshness and mutable delivery/checkpoint state are read-only operational
  facts outside M0 hashes and event-linked provenance.
- Every Gap can name affected claims and a resolution state without ceasing to
  be first-class Evidence.
- Unprovable Action Map states what cannot be established and why.

### I4 — Case and Evidence Pack

- Case, Finding and Annotation are product-local records with actor/time
  provenance.
- Case links executions, Evidence and Gaps by stable identity.
- Sealing creates an immutable revision digest.
- V1 export is deterministic JSON plus print-first HTML. It never imports raw
  source payload fields or mutable delivery state automatically. User-authored
  Case narrative and immutable source-ref snapshots are included verbatim, so
  the UI must warn investigators not to paste restricted source content.
- No automatic deletion or binding retention promise is introduced.

### I5 — Human and Agent same truth

- Root-created service identities use opaque, hashed bearer credentials with
  expiry, revoke, rate limit and audit.
- Browser cookies are rejected as Agent credentials; bearer tokens are never
  browser Session cookies.
- Agent scope is search/read/cite only.
- REST is authoritative. Any MCP surface is a thin read-only adapter.

### I6 — Session, Compare and Saved Views

- A Session/Thread exists only when an authoritative source identity exists.
- Time proximity does not create a Session.
- Compare preserves `missing` as distinct from numeric zero.
- Saved Views persist bounded Explorer filters and presentation choices, not
  result snapshots.

### I7 — Standard external parity

- Feedback/Evaluation, Alert-to-Case, Prompt/App/Policy version dimensions,
  OTel/OpenInference input compatibility and generic OIDC configuration seams
  are supported without turning X-Insight into an evaluator, gateway, prompt
  platform or IdP.
- A product-local, metadata-only OpenInference parser, deterministic projection
  and compatibility fixture are implemented without changing M0 semantics.
- The product-wide CI pin adopts immutable Foigent Protocol `v2.0.0` while
  existing UniRouter/LiteLLM runtime bindings stay fixed at their verified v1
  identities. OpenInference uses a Cursor-free source-neutral v2 product
  binding plus `connector_bounded_offline`/`bounded_offline_import` provenance.
  Runtime activation remains fail-closed because the immutable authority
  Manifest embeds a stale projection digest, a durable bounded-import receipt
  runtime is absent, and runtime authorization was not granted.

### I8 — Known/Unknown discovery

- Coverage, sequence, topic, anomaly, coalition and authority-drift outputs are
  hypotheses.
- Every hypothesis has a deterministic support score (not a calibrated probability), evidence references, limitations and an
  abstention reason when minimum evidence is absent.
- No hypothesis mutates Evidence, closes a Gap, changes routing or enforces a
  policy.

## 5. Quality Gates

Each increment must pass its focused tests before the next increment becomes
the active integration target. Final local closure requires:

- Go unit, component and PostgreSQL tests;
- migration identity and role-boundary checks;
- frontend typecheck, unit tests and production build;
- OpenAPI contract validation;
- deterministic fixture and Evidence Pack byte checks;
- desktop and 390 px browser flows;
- accessibility and keyboard checks for core tasks;
- `go test -race ./...`;
- `git diff --check`;
- explicit verification that the original dirty checkout, completed Phase 0–4
  worktree and Target runtime were not mutated.

## 6. Decisions Deferred To A Real Boundary

The following require a later User decision because they change external state
or binding product policy:

- push/PR/merge/release/deployment;
- real Connector traffic, credentials or Feed activation;
- binding Case retention/deletion policy;
- named OIDC provider and tenant configuration;
- correction and immutable re-release of the Foigent v2 OpenInference Manifest
  projection digest, followed by a new exact product pin;
- OpenInference durable bounded-import receipt/runtime implementation and its
  separate activation decision;
- paid model/provider traffic;
- automatic action or enforcement based on discovery hypotheses.

## 7. Local Closure Evidence

All nine local increment contracts are implemented:

| Increment | Local closure |
| --- | --- |
| I0 | A+C design contract, deterministic fixture tasks and three-viewport interaction checks |
| I1 | Investigation Inbox, fixed-snapshot Explorer, Saved Views and honest partial/empty/error states |
| I2 | outcome-first Story, source-declared DAG, separate Chronology, shared Evidence selection and explicit unresolved parents |
| I3 | four completeness axes, Connector Trust, first-class Gap resolution and Unprovable Action Map |
| I4 | Case/Findings/annotations, immutable Evidence/Gap citations, deterministic sealed JSON and print-first HTML |
| I5 | separately authenticated, expiring and revocable service identities over the same bounded read API |
| I6 | source-declared Sessions, fixed-snapshot Compare with Missing distinct from zero and Saved Views |
| I7 | feedback, Alert-to-Case, version dimensions, generic OIDC seam and product-local OpenInference parser/projection candidate |
| I8 | six bounded hypothesis classes, deterministic run identity, support score/limitations and mandatory abstention |

I7 product adoption and runtime activation are separate. The product pin now
verifies exact Foigent `v2.0.0`; the OpenInference local projection preserves
its five M0 IDs/hashes and four Gap Events while using bounded-offline
provenance. Activation remains correctly fail-closed at the separately listed
authority-reference, durable-receipt and runtime-authorization gates.

Final local verification:

- `go test -p 1 ./... -count=1`: pass against a fresh loopback PostgreSQL 16
  database with schema 18;
- `go test -p 1 -race ./... -count=1`: pass against a separate fresh
  loopback PostgreSQL 16 database;
- the dedicated PostgreSQL 16 host qualification passed role hardening,
  column-scoped importer plus workspace/reader least privilege,
  immutable-Evidence forbidden-write proofs, backup, forward migration and
  isolated restore. Its first run correctly refused the
  ambient non-v16 `pg_dump`; the accepted run pinned the PostgreSQL 16 tools;
- the workspace persistence qualification passed Case replay/conflict,
  Finding-scoped Evidence and Gap links, sealed-pack byte identity, Agent
  revoke, Gap resolution, Saved Views, Discovery replay and UTC normalization;
- `go vet ./...`, `go mod verify`, `go build ./cmd/...`: pass;
- both Phase 2C static runtime gates pass for protected source digests,
  migrations, single-writer boundaries, LiteLLM and fail-closed OpenInference;
- exact Node `22.23.1` / npm `10.9.8`: TypeScript pass, `21/21` component tests
  pass, offline high-severity audit reports `0` vulnerabilities;
- OpenAPI validator: pass for `36` required paths; dependency-license
  validator: pass for `177` locked package entries;
- deterministic production build: pass with exact referenced assets
  `index-B_-b64TK.js` and `index-DdhWUA0e.css`;
- Playwright Chromium: `12/12` pass across desktop, laptop and 390 px,
  including fixed-snapshot Compare with `Partial` distinct from `Missing` and
  no document-level overflow;
- JSON parsing and `git diff --check`: pass.

These are Local/CI-equivalent proofs, not Target or Production proofs. No
Target HTTPS login, external Connector, Production load or deployment claim is
made.

Preservation check at closure:

- original dirty checkout remains at commit
  `c27b39f67b0e72de7f60975ff8b0e94dfc05520f`, tree
  `16a4930f014c35531b01cc4bd44c25bd29a53ae8`, with its pre-existing dirty
  files preserved;
- the separate Phase 0–4 worktree is clean at current commit
  `cd4e5f320734b8abb11b7e7f936df7aab0b54c02`, tree
  `ccc60e517d269d1228a523d81e0783a6509e9e16`. It has four follow-on
  CI/adapter commits after the old handoff baseline; this review performed no
  writes in that worktree;
- no push, pull request, merge, release, deployment, credentials, Feed
  activation, external traffic or Target database mutation was performed.
