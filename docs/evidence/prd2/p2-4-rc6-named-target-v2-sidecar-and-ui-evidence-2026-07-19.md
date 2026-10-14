# P2-4 RC.6 Named Target, V2 Sidecar And UI Evidence

- Date: 2026-07-19 (Asia/Shanghai; Target operations used UTC on 2026-07-18)
- Work ID: `XI-PRD2-RC6-TARGET-20260718`
- Execution owner: Codex
- Classification: execution-owner Target operation and browser evidence; not
  independent acceptance
- Target: named non-production Alibaba Cloud ECS; resource identifiers remain
  in the private execution record rather than this public project evidence
- Exact product source: `0ca502f99181173e3542592ae5a018a5bfc43e0c`
- Product/manual: `0.2.0-rc.6` / `0.2.0-rc.6/manual-r6`

## Outcome First

The exact owner-supervised rc.6 delivery passed same-commit publication,
Target-side verification, the in-place rc.5-to-rc.6 upgrade, backup validation,
activation proof and a non-mutating previous-release rollback check. The exact
synthetic UniRouter v2 artifact then passed first persistence, identical replay,
readback, API and real embedded-browser proof on that Target.

This promotes exact rc.6 to **Target verified** only for the accepted named
non-production, owner-supervised, single-host, loopback and synthetic-fixture
scope. It also promotes the v2 sidecar consumer path from Local/CI to Target for
that exact synthetic artifact. It does not establish External integration,
customer-site usability, a live completed-execution feed, Production,
customer self-upgrade or a durable commercial publisher identity.

The immutable publication metadata correctly records
`target_verified=false`, because it was created before Target execution. This
append-only record supplies the later Target result; the signed delivery and
its release-time claims were not rewritten.

## Exact CI And Delivery Identity

| Item | Exact value |
| --- | --- |
| Prerequisite PR | [#54](https://github.com/foigent/x-insight/pull/54), merged |
| Merged exact main | `0ca502f99181173e3542592ae5a018a5bfc43e0c` |
| Exact-main CI | [29648815418](https://github.com/foigent/x-insight/actions/runs/29648815418), success |
| Owner-supervised workflow | [29648976737](https://github.com/foigent/x-insight/actions/runs/29648976737), success |
| GitHub artifact ID | `8430900609` |
| Downloaded workflow ZIP SHA-256 | `bd80cd65aa368acc9b1523d7bb68819e364f132e8e876652a595827b5fb623f3` |
| Signed bundle SHA-256 | `500e538d2162a8c716f08fdc5b5973e5b604e4c7d38915d8ddf630a8f3d45e2b` |
| Signed manifest SHA-256 | `90f16564efdc11a10e620373b1f2b9f3e23fadef5467da6a4daf06c04e673489` |
| Signed payload SHA-256 | `e70a437a7595ade386269171c844e0181c45a6f83dbb91afc1f9a20f44cc3502` |
| Bootstrap SHA-256 | `e7d6b298ef3df68a05e98edeab991e60631d9a6dbfe274214f233b9e2da5f99b` |
| Delivery trust-store SHA-256 | `83839907aae1bdb8648bfca0d2a204397d46a4f35592f189ba7a89d97a5e513e` |
| One-run key ID | `ed25519:sha256:5b5af0ab41a7653ef7e6cc66533de5fed89bfd84a684b9e63f5ef57dac7e60da` |

The downloaded artifact contained exactly the seven allowed root files. The
owner metadata binds exact current `main` to the successful exact-main CI run,
records `private_key_retained=false`, and leaves Target, External integration
and Production false at publication time.

## Named-Target Upgrade And Rollback Readiness

The upgrade started from exact rc.5
`27e05df26ff7976301a245f9120176e6ca70430d` and completed as operation
`20260718T220854Z-1f5aed3059a2daac`.

| Evidence | Exact result |
| --- | --- |
| Current after activation | rc.6 / `0ca502f...` / schema 11 / manual-r6 |
| Preserved previous | rc.5 / `27e05df...` / schema 11 / manual-r5 |
| Backup | `/var/backups/x-insight/20260718T220855Z-20260718T220854Z-1f5aed3059a2daac.dump` |
| Backup size / catalog | 51,530 bytes / 103 `pg_restore --list` entries |
| Backup SHA-256 | `dc0a9393fdab20a8b39d2b0f278d93ae9cf708d18866f153076212b698a82290` |
| Upgrade receipt and durable receipt SHA-256 | `397d4f837ec84fb9a7ea93483f5b2f7d1edac28065edfb0a1f031b12440ecba7` |
| Completed journal SHA-256 | `583bdcb1b443f7580b2130a0e07a1063d14108cf59e41790f8b71aaff028594f` |
| Active transition trust-store SHA-256 | `50b6202061b0349eefca39a83f48402aada0d731fe4f7537ee34a42884e80396` |

All eight upgrade checks passed: backup, compatibility readiness, evidence
integrity, listener, migration ledger, release signature, representative read
and service launcher. The non-mutating
`rollback --check --previous` path reported `supported=true`, `mutating=false`
and passed all five checks. No actual rollback or down migration was run in
this rc.6 operation.

The PostgreSQL 16 container remained exact ID
`ab529d5e2c0587db944208145880e9d82e07c2ff2c57ef92af5487deb23d5e85`
with unchanged `StartedAt=2026-07-10T16:00:15.367372173Z`. Final listeners were
exactly `127.0.0.1:8080` for X-Insight and `127.0.0.1:5432` for PostgreSQL.
Final readiness returned rc.6, schema 11, PostgreSQL major 16 and manual-r6.

The rc.5+rc.6 transition public trust remains intentionally active while the
accepted rollback window remains open. It was not narrowed in this operation.
That temporary compatibility state does not authorize future unrelated
releases.

The pre-upgrade historical evidence fingerprint remained
`sha256:3842d9a42f1981c3578c444fdba160f20d5a5cc8f5a84a136242ef3cf1bf5666`
through activation. It changed only after the separately bounded v2 synthetic
import added two expected evidence events.

## V2 Sidecar Execution Chain

The installed fixture was byte-for-byte the committed synthetic producer
artifact:

| Property | Exact value |
| --- | --- |
| Fixture SHA-256 | `93bc4c8448f1f838668f9dab10cbba070f0df550665b16d73bb494066fe8d6a1` |
| Schema | `unirouter.prd5.xinsight.snapshot.v2` |
| Request ID | `req-xi-v2-micro-7-synthetic` |
| Source instance | `local-synthetic-v2` |
| Money | `"7"` / `CNY` / `1000000` / `micro-cny.v1` |
| Safety | one synthetic usage row, one explicit gap, no raw prompt/response or API key |

The owner preserved the complete fail-closed attempt chain instead of deleting
or rewriting partial evidence:

1. Attempt 1 script SHA-256
   `bcaa89d7f7dc076f268e8d18931edec45c4b60f6346052be421a4992ab4974b6`
   stopped before import because its live-identity assertion incorrectly
   expected `inet_server_addr()` to equal the host-side loopback endpoint.
   Event count remained exactly 4.
2. A read-only diagnostic, SHA-256
   `4fae3d112201af360ccc67754349f72b2f8a4fb2dbb3fb6d0940c8de7795cd11`,
   proved the DSN endpoint remained `127.0.0.1:5432/xinsight`, while Docker NAT
   correctly exposed server/client addresses `172.18.0.2` / `172.18.0.1`.
   Reader/importer roles, read-only state, PostgreSQL `160014` and event count
   were also checked without printing passwords.
3. Attempt 2 script SHA-256
   `618a869e467dbed936749ab7b22ac3696ebfd876c247b06adc08839f64ed17ac`
   completed first import, identical replay and the post-import fingerprint.
   It then stopped before API GETs because Python's microsecond parser rejected
   a valid nine-digit Go RFC3339 timestamp. Event count was exactly 6; API and
   final validation outputs were absent.
4. Attempt 3 continuation SHA-256
   `d30248de0222cc46592a559fcaae2758c062854f287f3e2f5ef52c70a481652e`
   made the importer body unreachable, read the root-only attempt 2 artifacts,
   used an integer-nanosecond UTC parser, repeated fresh read-only status/role
   checks, issued only loopback API GETs and wrote the final validation.

The two false negatives were owner validation-harness defects, not product or
database failures. They did not cause a blind rerun, overwrite, evidence
deletion or unrecorded success. The continuation and its static read-only
review do not constitute independent Target acceptance.

## Import, Replay And API Result

| Metric | First import | Identical replay |
| --- | ---: | ---: |
| Persisted inserted | 2 | 0 |
| Persisted duplicate/no-op | 0 | 2 |
| Persistence conflict | 0 | 0 |
| Capture gaps inserted | 1 | 0 |
| Readback events | 2 | 2 |
| Readback capture gaps | 1 | 1 |

- First report SHA-256:
  `8eb4440479ae7205dc117dfb200cd19f6b9e056b0305c1622ace12b5e7361cc1`.
- First receipt SHA-256:
  `78b56f3488c3fd9cfb628b99034bc58b8f888cbcede8ba51d5bd4eb5c72194ad`.
- Replay report SHA-256:
  `f88e9559ed4382eba6b3715908a318b581ff14331213872b6f0010fc9b639d45`.
- Replay receipt SHA-256:
  `29c67263456ae6466295677924be4bea4d2ee1dee24705be2547b44b5a521efa`.
- Post-import fingerprint file SHA-256:
  `32054d8659526d6f0ebd725a41aeccd482494d016b32179e351dc44b3bcc8652`.
- Post-import evidence fingerprint: 6/6 events,
  `sha256:bb6a9c55c70e8f93c0f677badcf8973c3c4ed05866551b996962f3ea5883f328`.
- Final API-validation file SHA-256:
  `ab080088e2151eeb979017ec9efa5954621e193c222da06cfa6cce0b3db54654`.

Persistence results bound exact event types, source refs, request ID,
idempotency keys and canonical event hashes. First and replay hashes/keys were
identical; the API usage event hash was the exact first-persistence hash.

The loopback API returned execution
`xe1_1300177cc7ba046246ac802953e5c5ee0973b7e688d7e542772fbe0f7fc23ace`
with two evidence-derived timeline nodes and one capture gap. The usage event
retained `"cost_micros":"7"`, `CNY`, scale `1000000` and
`micro-cny.v1`; the UI/API did not reintroduce `cost_cents` or floating-point
money. Source system, v2 adapter manifest, source version/ref/hash and F2
fidelity were present. Field-group provenance was exact: correlation,
identity, capability and usage direct; outcome derived; payload policy
declared.

Freshness remained truth-bounded:

- `proof_class=synthetic_fixture` and `assurance_mode=synthetic_fixture`;
- `sync_state=gap` and `timeline_complete=false`;
- source/import revisions and watermarks bind the exact fixture SHA-256; and
- the explicit gap is `not_available_from_source` for
  `request_trace_events`, with `sampled_out`, `retention_expired` and
  `not_instrumented` retained only as unconfirmed candidates.

All retained Target evidence files are root-owned, mode `0600`, regular files
with one link. The validation explicitly records
`raw_prompt_response_exposed=false`, `external_integration_verified=false` and
`production_verified=false`.

## Real Browser And Manual Proof

Operator access used only the approved path: Alibaba Cloud Session Manager as
an OpenSSH stdio proxy plus explicit OpenSSH
`-L 127.0.0.1:18080:127.0.0.1:8080`. Before browser use, `lsof` showed exactly
one `ssh` listener on `127.0.0.1:18080`; no wildcard listener appeared.

The headed Chromium flow exercised the actual embedded Target UI:

- exact Request ID plus a bounded date range returned one synthetic execution,
  2 evidence events and 1 recorded gap;
- selecting the result preserved the fixed `as_of` and opened execution detail;
- the two-node timeline displayed usage then gap chronology without inventing
  a parent/causal edge;
- usage selected-node detail displayed
  `CNY 0.000007 (7 micro-CNY)`, token counts, source, hashes and exact
  field-group provenance;
- selecting the gap node switched selected-node detail and preserved its exact
  `capture_gap` source ref;
- the capture-gap panel showed one confirmed gap plus the three explicitly
  unconfirmed candidates;
- `direct`, `derived`, `declared`, `suppressed`, `gap`, `unknown` and
  `not_applicable` remained visible text/canonical semantics; and
- the same-origin manual rendered title
  `X-Insight 0.2.0-rc.6 简体中文产品手册`, product rc.6 and
  `0.2.0-rc.6/manual-r6`.

One deliberately invalid search used a `to` time after the server's `as_of`.
The server returned the stable 400 problem contract, and the UI showed
“筛选参数无效”, safe remediation text and a correlation ID. Changing `to` to
the current local minute produced the valid result; all valid search/detail/
timeline/gap/event requests returned 200. The absence of a client-side
future-time warning is a non-blocking P2 UX opportunity, not a data or contract
failure.

The visual viewport showed the actual rendered layout, not only an API or DOM
mock. The Playwright session and SSH tunnel were then closed. A final socket
check showed local port 18080 free.

## Status Matrix

| Layer | Status after this record | Exact boundary |
| --- | --- | --- |
| Local | Verified | execution-owner rc.6 source/package and v2 synthetic self-tests |
| CI | Verified | exact main `0ca502f...`; Backend, Signed V2 and Fresh Install success |
| Supported-profile VM | Verified | clean install and transition qualification; not the named Target |
| Target | **Verified** | exact rc.6, named non-production host, loopback owner-supervised synthetic-fixture scope |
| External integration | Pending | no newly authorized artifact from current merged/CI-passing UniRouter main was imported in this operation |
| Field usability | Pending | accepted manual-guided `N=1` User tasks have not been run |
| Production | Not verified / out of scope | no public, multi-tenant, HA/DR, Production security, load or SLO claim |

## Findings And Remaining Gates

- P0: none observed.
- P1: none open for the accepted rc.6 Target/synthetic scope.
- P2: the browser accepts a future local `to` value and relies on the stable
  server/UI error contract rather than warning before submit. The two owner
  harness false negatives are closed and retained above.
- Phase 1 remains closed; no new verifiable Phase 1 P0/P1 was found.
- The rc.5+rc.6 transition trust may be narrowed only after its rollback window
  is explicitly closed and the exact retained previous-release policy is
  rechecked.
- `UR-E` remains the next cross-project dependency: UniRouter must provide one
  newly approved, bounded v2 sidecar artifact from merged main and passing CI.
  X-Insight then runs the same import/receipt/readback/UI proof and may promote
  only the scenarios actually present in that artifact to External integration.
- The next human gate is the accepted manual-guided `N=1` investigation/value
  task during the joint field window, after customer-site host and data-custody
  facts are confirmed.
- The completed-execution live-feed slice remains deferred as a whole. No
  endpoint, credential, polling, exporter, receipt transport or cursor/replay
  contract was added through this Target work.

## Governance Correction — 2026-07-19

The preceding historical `UR-E` dependency bullet is preserved rather than
silently rewritten. It recorded X-Insight's proposed no-claim gate, not an
accepted UniRouter task. No dedicated request or joint decision then existed.
The later Open X-Insight request authorizes nothing; artifact generation or
delivery remains counterpart-decision pending until a UniRouter response,
accepted joint decision and links from both project planning chains exist. See
[UR-E Cross-Project Governance Correction](ur-e-cross-project-governance-correction-2026-07-19.md).
