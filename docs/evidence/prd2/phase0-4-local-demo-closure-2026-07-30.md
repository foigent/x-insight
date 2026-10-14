# Phase 0–4 Local Demo Closure

Date: `2026-07-30`

Work ID: `XI-UR-PHASE0-4-LOCAL-DEMO-20260730`

Evidence class: Local implementation and Local verification only. This record
is not CI, Target, External integration, Production, release, or deployment
evidence.

## Closure Result

| Phase | Local result | Reader-visible outcome |
| --- | --- | --- |
| 0 — baseline and authority | Closed | Clean X-Insight and UniRouter worktrees were used; protected dirty checkouts were not modified. |
| 1 — investigation projection | Closed | One bounded deterministic projection selects `causal_trace`, `chronology`, or `terminal_snapshot`; it never invents causal edges or uses an LLM. |
| 2 — source evidence | Closed | UniRouter emits a bounded immutable V2 terminal bundle after its outbox barrier and declares completeness or explicit gaps. |
| 3 — ingest and Read API | Closed | X-Insight strictly validates both pinned digests, imports and replays six V2 facts idempotently, persists them, and serves the projection API. |
| 4 — access and customer-demo UI | Closed for Local | Independent product login, Session/RBAC boundary, Investigation/Chronology/Proof views, stage coverage, Trace rail, Evidence inspector, gaps, citation, exact usage and cost are implemented. |

## Demo Evidence And Expected Story

The committed demo fixture is
`fixtures/unirouter_execution_evidence/recovered_fallback.v2.json`. It is
explicitly labeled `synthetic_fixture`; the UI renders
`Demo data · synthetic`.

The source-backed story contains five Trace facts and one Call Log:

1. `provider-a` is selected.
2. `provider-a` fails with HTTP `502`.
3. a retry is scheduled.
4. `provider-b` is selected as fallback.
5. billing settles.
6. the call completes successfully with `11` tokens, `140 ms`, and exact cost
   `17 micro-CNY`.

Four causal edges come only from source-declared parent spans. The neutral
dashed rail is chronological, not causal. The final recovered-fallback claim
requires an ordered failure, retry, fallback, and later success on the same
fallback provider, plus complete source, import, and projection integrity.

The initial customer-facing view answers the outcome first, then shows all
seven requested stages: identity, routing, provider, retry/fallback, billing,
usage, and logging. The selected failed Provider node exposes normalized facts
and an Evidence citation without raw prompt, response, upstream error, token,
credential, or IP content.

## Independent Login Boundary

- `/login` is a product-owned page, not a proxy login skin.
- HTTP renders the page but disables username, password, and submit; it sends no
  credentials.
- HTTPS login, secure host-only Session cookie, same-origin enforcement, CSRF,
  logout, Session list/revoke, role checks, expiry, and restart invalidation
  have backend/component coverage.
- No default account or Web account creation is introduced.

This Local closure does not claim a successful browser login through a named
Target HTTPS origin. rc.14 adds one explicit opt-in deployment contract:
the product server remains on loopback HTTP, a same-host TLS terminator connects
from loopback, and the application accepts only one pinned HTTPS external
origin with exact forwarded proto/host. Arbitrary trusted-proxy headers, direct
public application/DB listeners, HTTP login, and a proxy-owned login page remain
unsupported. A real field-demo login still requires Target certificate,
network, cookie, authentication, readback, and renewal evidence.

## Cross-Repository Contract And Drift Control

The two products do not background-sync project files and do not use the
coordination folder as a third source of truth.

- UniRouter owns the profile and bundle schema.
- X-Insight owns only the consumer pin and normalization/projection code.
- Every bundle carries the exact profile digest and bundle-schema digest.
- X-Insight rejects either digest mismatch, unknown fields, contradictory
  completeness, undeclared observed facts, uppercase hashes, duplicate facts,
  over-bound bundles, or forbidden raw-content fields.

Current handshake:

- profile:
  `sha256:834bbaae05936af76abc4608ab16547581656e415c7d73f79d5d55ef6eca9547`
- bundle schema:
  `sha256:e47c5e074e3b7de8c16be9615c5ea073348017c58e07b9ba0566e2a66185f33a`

This avoids silent file-copy drift: opening either development repository shows
that product's owned files, while runtime compatibility is decided by the
double digest rather than by same-name files being assumed equal.

## Passing Local Verification

### X-Insight Go And PostgreSQL 16.14

An isolated PostgreSQL `16.14` cluster was created at
`/private/tmp/xinsight-phase4-pg16.tJfnIy`, bound only to
`127.0.0.1:55441`, and stopped after testing. Final `pg_isready` returned
`no response`. The directory was retained under the no-bulk-delete rule.

The following sequential package matrix passed against its migrated database:

```text
go test -p 1 ./internal/api ./internal/db ./internal/adapters/unirouter \
  ./internal/investigation ./internal/buildinfo ./cmd/xinsight-importer \
  ./cmd/xinsightctl -count=1
```

This covered migration `000015`, schema-15 readiness on supported PostgreSQL,
strict V2 import, registry identity, persistence, duplicate replay, projection,
API response, exact usage/cost, restricted-field absence, and version output.

A separate local PostgreSQL `18.0.3` run returned `/readyz` `503 unsupported`,
as required by the PostgreSQL-16 product contract. It was not relabeled as a
passing supported-runtime result.

### UniRouter Source Runtime

The following focused matrix passed:

```text
go test ./internal/executionevidence ./internal/gateway ./internal/outbox \
  ./cmd/uniroute -count=1
```

The gateway integration creates a migrated PostgreSQL test database and covers
migration `000046`, close-marker ordering, prerequisite barrier, replay,
partial semantics, bundle immutability, and forbidden raw-value absence.

### Frontend, Contract, Build, And Browser

- `npm run check`: PASS.
- `npm test`: PASS, `13/13`.
- `npm run check:openapi`: PASS, `12` required Read/Access paths.
- `npm run build`: PASS, including embedded-dist reference validation.
- `npm run test:e2e`: PASS, `6/6` across Chromium desktop and narrow:
  recovered-fallback Investigation, independent HTTP-safe login, and the
  release-matched offline manual.
- The narrow manual initially exposed document-level table overflow. The
  renderer now contains wide tables in their own horizontal scrollers and clips
  overflow propagation at the chapter boundary; Playwright measured the
  document at `390/390` CSS pixels while retaining 13 internal table scrollers.
- Playwright CLI snapshot and visual inspection: PASS for the independent login
  and the desktop Trace workbench. The view showed the outcome, metrics,
  seven-stage coverage, all six Evidence nodes, four declared-parent edges, and
  the selected failure inspector without clipping or horizontal overflow.
- `go test -race ./... -count=1`: PASS across all Go packages.
- `git diff --check`: PASS at the evidence freeze.

## Deliberate Product Boundary

Migration `000015` makes the current development tree schema `15`, so
`internal/buildinfo` now reports `15..15`. Release preparation assigns the new
identity `0.2.0-rc.14`, manual `0.2.0-rc.14/manual-r14`, and exact schema-15
application compatibility. rc.13 remains historical schema-14 evidence and is
only an allowlisted upgrade source; it is not rewritten or presented as the
current build. The rc.14 manifest declares application rollback incompatible,
so returning to an older application requires the explicit isolated-restore
path rather than widening an old manifest.

Current and unknown release identities also fail closed unless the signed
payload contains the trusted loopback proxy environment and public-IP Nginx
templates. These templates do not make a deployment public by themselves and
do not constitute certificate, renewal, firewall, or Target login proof.

## Not Run And Remaining Evidence

- This Local record is not push, pull request, merge, tag, release, signed
  artifact, deployment, service mutation, Target database mutation, or
  Production evidence; those actions require their own exact records.
- No real provider request or External integration traffic occurred.
- This Local record contains no persistent Target product credential or real
  customer account and no named Target HTTPS browser login.
- The current source profile intentionally stops at identity, routing,
  provider, retry/fallback, billing, usage, and logging. Tool/MCP/multi-Agent
  steps remain absent until a real authoritative source exists.
- GitHub CI and clean-clone verification remain separate from Local evidence.

## Repository Safety

The original dirty X-Insight and UniRouter checkouts were not written. No
tracked file, directory, user data, credential, or external state was deleted.
Generated historical Web assets remain unreferenced rather than being
bulk-removed.
