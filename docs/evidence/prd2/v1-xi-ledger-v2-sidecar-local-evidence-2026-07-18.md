# UniRouter snapshot v2 sidecar local evidence

Date: 2026-07-18 (Asia/Shanghai)

Work ID: `XI-V1-LEDGER-V2-SIDECAR-20260718`

Status: X-Insight implementation merged; Local, PR CI and main CI verified with
synthetic metadata-only data. Publication, rc.6 Target, External integration
and Production remain pending

## Baseline and boundary

- Repository baseline: `main == origin/main ==
  a0c7ab9cf46ba126e090a9694ba66ed0f49a2f7f`.
- Existing branch: `codex/v1-xi-ledger-v2-money`.
- The pre-existing uncommitted v2 money work was preserved and reviewed in
  place.
- Source commit `a5f64c011b032a670845009be1f40d796a47d629`
  passed PR #52 CI run `29645951982`; PR #52 was squash-merged as
  `0462e698775488d86e1263debf64990239464340`. Exact-main push CI run
  `29646094307` passed.
- Because this is new runtime code after immutable rc.5, the candidate release
  identity is `0.2.0-rc.6` / `0.2.0-rc.6/manual-r6`; exact rc.5 is an accepted
  upgrade source. Rc.5 Target evidence does not promote rc.6.
- The accepted transport remains the operator-mediated
  `xinsight.import-receipt.v1` sidecar.
- Pull feed remains default-disabled/mock-only; the whole live-feed slice is
  deferred, not permanently rejected, from the first formal version.
- No Target, Production, customer data, endpoint, token, automatic polling or
  new retention behavior was used or added.

## Reviewed compatibility corrections

1. The v2 fields remain exact text/metadata through importer, M0 schema, JSONB,
   read model, OpenAPI, TypeScript and UI `BigInt` formatting.
2. V1 cents, v2 micros, partial/mixed tuples, explicit nulls and unknown fields
   are distinguished and validated strictly.
3. The prior candidate reused the historical adapter manifest ID even though
   the mapper and normalized event changed. V1 now keeps
   `22222222-2222-2222-2222-222222222222`; v2 uses
   `22222222-2222-4222-8222-222222222223`. PostgreSQL regressions prove
   same-source-row v1/v2 concurrent upserts converge to v2, sequential v1 then
   v2 does not conflict, and both replays are no-op.
4. Source registry version advances from v1 to v2. A later valid v1 replay or a
   historical event with no source version does not downgrade v2; an unknown
   non-empty UniRouter source version fails before SQL for a fresh, versionless
   or known v1/v2 registry row. Reusing a known manifest ID for a different
   known mapper version also fails closed.
5. `BuildSnapshotEvents` now validates and returns an error for malformed
   snapshots instead of dereferencing incomplete v2 fields.
6. OpenAPI mirrors the M0 cents/micros exclusivity and complete-v2-tuple rule.

## Exact producer artifact

The committed golden fixture
`fixtures/unirouter_snapshot/micro_money_v2_generated.json` is byte-for-byte
the actual synthetic UniRouter producer output:

| Property | Value |
| --- | --- |
| bytes | 3611 |
| SHA-256 | `93bc4c8448f1f838668f9dab10cbba070f0df550665b16d73bb494066fe8d6a1` |
| schema | `unirouter.prd5.xinsight.snapshot.v2` |
| request ID | `req-xi-v2-micro-7-synthetic` |
| cost | JSON string `"7"` |
| currency / scale / schema | `CNY` / `1000000` / `micro-cny.v1` |
| content safety | no raw prompt, raw response or API key |

It contains one usage row and one explicit generated capture gap because no
trace row exists. That gap is evidence, not a fabricated trace.

## Actual CLI persistence and replay

A fresh isolated local PostgreSQL database was initialized through the actual
X-Insight migration CLI. The actual `gate-a-check --persist` command then ran
twice against the exact artifact.

| Metric | First run | Identical replay |
| --- | ---: | ---: |
| in-memory accepted | 2 | 2 |
| in-memory rerun duplicate/no-op | 2 | 2 |
| persisted inserted | 2 | 0 |
| persisted duplicate/no-op | 0 | 2 |
| persistence conflict | 0 | 0 |
| readback events | 2 | 2 |
| readback capture gaps | 1 | 1 |

Receipt envelope: `xinsight.import-receipt.v1`.

- first receipt SHA-256:
  `f98b6b1f47a79e798febd107707580c91bf9b0c015f4da0e947b130c89c926b8`;
- replay receipt SHA-256:
  `185c6ba9af73beaef5c0d0adc42596900422db194e784698d83e837e973350c1`;
- both files were `0600`;
- warnings correctly identify the synthetic artifact's missing approval
  manifest and explicit source capture gap.

Direct SQL proved:

```text
source_system_id=11111111-1111-1111-1111-111111111111
adapter_manifest_id=22222222-2222-4222-8222-222222222223
source_version=unirouter.prd5.xinsight.snapshot.v2
cost_micros=7
currency=CNY
amount_scale=1000000
money_schema=micro-cny.v1
jsonb_typeof(cost_micros)=string
cost_cents_present=false
event_count=2
capture_gap_count=1
```

## Search, API and UI

The actual loopback server returned one execution for the exact request/source,
with 2 evidence events and 1 gap. Event detail returned exact
`"cost_micros":"7"`, v2 source version and v2 adapter manifest; it omitted
`raw_source_excerpt`.

The mock-backed Chromium desktop/narrow Playwright suite passed 4/4. The
PostgreSQL-backed real-API Chromium suite passed 1/1, selected the real usage
event, made no raw API request, reported no browser error and observed:

```text
精确成本（micro-CNY）  CNY 0.000007 (7 micro-CNY)
币种                  CNY
金额缩放因子          1,000,000
金额语义版本          micro-cny.v1
```

The final temporary server listened only on `127.0.0.1:18083`. `/healthz` returned
200. `/readyz` correctly returned 503 with `db:unsupported` because the local
Homebrew database was PostgreSQL 18, outside the supported PostgreSQL 16
profile. This run therefore proves Local compatibility only; PostgreSQL 16
qualification remains a CI gate and this result is not Target evidence.

## Verification

- focused Go adapter/DB/API/importer/pullfeed packages: pass;
- PostgreSQL-backed v1-to-v2 first-write/replay/search/API regression: pass;
- OpenAPI validation and TypeScript check: pass;
- component suite: 6/6 pass;
- mock Chromium desktop/narrow suite: 4/4 pass;
- PostgreSQL-backed real-API Chromium suite: 1/1 pass;
- golden fixture hash equals the producer output: pass;
- uncached full Go race/coverage suite (`-count=1`): pass at 65.1% statement
  coverage; the
  dedicated destructive PostgreSQL 16 host-role qualification was intentionally
  skipped on the local PostgreSQL 18 service and then passed in PR CI;
- `git diff --check`: pass.

## CI verification

- PR run `29645951982`: success for pinned frontend, Chromium, backend/API,
  PostgreSQL 16 host-role/backup/isolated-restore, fixture, first/replay v2 CLI
  import, read smoke and contract gate;
- squash merge: PR #52 to
  `0462e698775488d86e1263debf64990239464340`;
- exact-main run `29646094307`: success for deterministic signed candidate,
  exact Phase 1 transition/recovery on a clean VM, fresh install on a second
  clean VM and final event-specific contract gate;
- PR correctly skipped signed/fresh qualification; main correctly skipped the
  already-complete frontend/backend jobs.

These CI artifacts use ephemeral non-publishable identities. This is CI
qualification, not owner-supervised publication or rc.6 Target evidence.
Main CI covered the exact Phase 1 artifact-to-rc.6 transition on a clean VM and
a fresh rc.6 install on a second clean VM; it did not exercise the existing
named Target's in-place rc.5-to-rc.6 path. Before changing that host, the exact
rc.5-to-rc.6 preflight and upgrade must pass as a separate Target gate.

## Status classification

| Layer | Result |
| --- | --- |
| Implemented | Merged to `main` as `0462e698775488d86e1263debf64990239464340` |
| Local verified | Yes |
| CI verified | Yes; PR `29645951982`, main `29646094307` |
| Target verified | No for rc.6; exact rc.5 remains historical Target evidence |
| External integration | Not closed; synthetic Local sidecar compatibility only |
| Production verified | No |

All task-owned local test databases, the temporary coverage profile and the two
receipt files were removed by exact identity after their hashes and bounded
results were recorded. Repository fixtures, normalized evidence and durable
hashes are retained.
