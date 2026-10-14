# PRD2 UR-E Exact Synthetic External Integration Evidence

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

Work ID: `XI-PRD2-UR-E-BOUNDED-SYNTHETIC-20260719`

Owner: Codex

Date: 2026-07-19 (Asia/Shanghai)

Evidence class: execution-owner Target, API and real UI verification; not
independent acceptance

## Outcome

The accepted UR-E decision completed its exact manifest-bound synthetic
sidecar scenario:

> **External integration verified — execution-owner verified, exact manifest-
> bound synthetic sidecar scenario only.**

The proved inventory is one new synthetic call log with non-zero token/cost
usage, zero trace events and one confirmed `not_available_from_source` trace
capture gap. It does not prove a trace-present or denial scenario.

This evidence does not claim independent acceptance, the manual-guided `N=1`
field task, customer or sanitized-real data, a live feed, automatic freshness,
Production, or completion of the whole UniRouter/X-Insight integration.

## Binding Inputs And Producer Identity

| Item | Exact identity |
| --- | --- |
| Binding decision | activated `2026-07-19 12:15:26 CST`; SHA-256 `c674b04f719b64d9746f2afb4f9d5020d7f49fe96af5d1254a83e7cd911595a1` |
| Corrected UniRouter response | SHA-256 `12857bd7a37436608461d22caf3c9f1fb8dbfac7274f446384349f488dba8cee` |
| Producer code authority | `5176654d7d320b56c1c136f39b3debb8156ece4b`; PR head `be2d5f5d...`; required CI `29640791281` |
| Stage A evidence publication | branch `codex/ur-e-stage-a`; commit `640e58f...`; PR #223; docs-only CI `29674988916`; merge `df3e41d...` |
| Request | `req-ur-e-v2-synthetic-20260719-001` |
| Source instance | `unirouter-ur-e-20260719-001` |
| Source time | `2026-07-19T04:57:16.877007Z` |
| Exchange | `uxchg_3a8559f2405c1351f16964235408fb89` |
| Generated time | `2026-07-19T05:01:28.276835Z` |
| Manifest | 412 bytes; SHA-256 `b1c096df2e709520cbf7b2a7b5b94989b2fcacad295ea42a3deede46d1aff1f6` |
| Snapshot v2 | 3985 bytes; SHA-256 `412c9b641cb6b6451a8b66f3ff808ef29750a7f3bedafdef93a473b66f641e7e` |
| Frozen sanitized handoff | SHA-256 `88dc35e019b649e78f59e3dc6e0d76c6c3bb6a896fd2dbce44634a652b7cba35` |

The snapshot differs from the previously committed `93bc4c...` fixture and
the later historical `94c8db...` artifact. The producer artifact is metadata-
only and contains no raw prompt, raw response, API key, raw upstream error,
customer identifier or raw network address. Its exact cost tuple is `314159`
micro-CNY, currency `CNY`, scale `1000000`; `cost_cents` is absent.

The shared handoff is
`PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/evidence/2026-07-19-prd2-ur-e-bounded-synthetic-sidecar-handoff.md`.
It contains sanitized identities/results only, not artifact bytes, a DSN or a
credential.

## Target And Safety Boundary

The named non-production Target was `launch-advisor-20260710` /
`REDACTED_TARGET_INSTANCE_ID`. Immediate preflight reverified:

- managed X-Insight `0.2.0-rc.6` at source
  `0ca502f99181173e3542592ae5a018a5bfc43e0c`;
- systemd active and `/readyz` ready;
- only loopback X-Insight and PostgreSQL listeners;
- PostgreSQL `16.14`, 11 clean migrations and unchanged runtime identity;
- pre-import counts of 6 evidence events, 4 gaps, 2 provenance rows and 1
  source system; and
- no existing row for the request/source/exchange/manifest/artifact plus all
  six decision-bound Target evidence paths absent.

No deployment, configuration, credential or release change was made. Target
access used the already approved Alibaba Cloud Session Manager/OpenSSH path.
The manifest and snapshot were transferred one at a time with `scp -p`, never
through the shared cooperation folder.

After the manifest transfer, the snapshot absence precheck failed before any
snapshot `scp` because generated single-quoted Python list literals conflicted
with the remote command's POSIX single quoting. It returned `1`; its bounded
392-byte stderr had SHA-256
`cf45aefc461f3180c2ef0bb5a4f2eba247da3be71972fc427432492e8c85f658`.
Read-only diagnosis proved the manifest exact and the snapshot plus four
outputs still absent. Correcting only that read-only quoting allowed the sole
snapshot transfer. Therefore there was one manifest transfer and one snapshot
transfer; no snapshot-transfer retry, import or database write occurred on
the failed precheck path.

Both Target inputs were then regular, root-owned, mode-`0600`, single-link
files with exact approved length and SHA. All four output paths remained absent
until the importer created them exclusively.

## First-Write And Replay

The locked Stage B harness was invoked once through an isolated in-memory
`/usr/bin/python3 -I -B -S -c` wrapper after exact length/hash reconstruction:

| Control input | Exact identity |
| --- | --- |
| Harness | 54377 bytes; SHA-256 `9bed795dd63217a4633aa66a2ffe1399bed858c9e37b464926331095ae7e4fb0` |
| Fixed wrapper | 16609 bytes; SHA-256 `2de491af43c493828b97abc655ca4f68c0f9df8772a3f97bb2c26d71c9780c00` |
| Remote command | 16640 bytes; SHA-256 `a389db4b22ce812af15b35e604de43e3e58524e84ebf677ccf4688ede9938871` |

No harness file, stdin payload or seventh Target evidence path was created.
In-memory dry-run validation passed before persistence.

| Operation | Result | Receipt SHA-256 | Report SHA-256 |
| --- | --- | --- | --- |
| First-write | `committed_exact`; inserted 2 events and 1 gap; duplicate 0; conflicts 0 | `bb7853986bba5e1deca2806fe3364a7d051f71ab62c87799517dabe96fb090f5` | `a8a2fd5703bb2379c29cd52741fb466d8795ee0d77128dea72b75029eb70b920` |
| Identical replay | `unchanged_exact`; inserted 0; duplicate 2; conflicts 0 | `464c865e7e287428ad93e83daabc909045e440cf5552e3334240a822382b09d1` | `9454968b13bc17438bad8a956fed15c669f1848b8c90efb771ad4a1a0ecdace9` |

Both importer subprocesses returned `0`, wrote no stderr and fsynced their
receipts/reports. Counts changed only as expected: events `6 -> 8`, gaps
`4 -> 5`, provenance `2 -> 3`, provenance links `2 -> 4`; adapters remained
`2`, sources remained `1`, and replay changed nothing.

The harness compared every prior row and the expected new rows across all ten
rc.6 business tables plus `schema_migrations`. The final canonical fingerprint
was `sha256:f0ff86f85620492691ebcd5ff9da27ece06cd9efbfa64ff0e0aae3ada4508e2f`
and was identical after replay. This is stronger than checking aggregate counts
alone.

## API And Real UI Readback

The bounded APIs returned exactly two events and one gap for the approved
request:

- proof class `operator_approved_integration`;
- assurance `operator_attested_sidecar`;
- integration mode `sidecar`, sync state `gap`, timeline incomplete;
- usage 11 input, 5 output and 16 total tokens;
- cost `314159` micro-CNY, `CNY`, scale `1000000`;
- confirmed gap `not_available_from_source` for `request_trace_events`, with
  candidates `sampled_out`, `retention_expired` and `not_instrumented`; and
- no raw prompt/response exposure.

The owner opened the actual Target product through a temporary loopback SSH
tunnel. Exact request search returned one execution. The Chinese UI displayed
the execution identity, operator-approved sidecar proof, truthful gap/not-
Production state, source/import revisions and watermarks, two evidence-derived
timeline nodes, exact usage/cost, source/provenance hashes and the linked
capture-gap panel. Selecting the gap node changed selected-node detail to the
gap evidence and preserved `gap`/`unknown` labels. The legend exposed
`direct`, `derived`, `declared`, `suppressed`, `gap`, `unknown` and
`not_applicable`. No raw-payload viewer was present.

Retained local browser evidence:

| View | Bytes | SHA-256 |
| --- | ---: | --- |
| Search snapshot | 7585 | `a05f9c1c0162770ae9176f02ef22345c3ed096733b7847b93a131360a1d95007` |
| Detail snapshot | 21320 | `b73a91eb33dd2d0b5769c3646f65e64d874e4e7f70708ee24e4157ec01e18e1c` |
| Gap-selected snapshot | 17072 | `a5f0f3817aa4eced6fd6460a54c80e7a42ce903e1b344949a7542f85cac48301` |
| Gap-selected screenshot | 177573 | `813ac0c862b1d3a1b2c9be598b3a7eb7652240066b62ea8395ee51b3c3cef27a` |

The browser and temporary tunnel were closed, and the local tunnel port was
verified free.

## Final Reconciliation And Retention

A separate in-memory read-only reconciliation program, 24166 bytes with
SHA-256 `50adfc80a1250f7f0797d8d77448167915ba516a7dab04ae894b56286633f5cd`,
reverified all six evidence files, exact post-import counts/fingerprints/target
rows, systemd active state and readiness. Release, service configuration,
PostgreSQL identity/version and loopback-only listeners remained unchanged.

The manifest, snapshot, first-write receipt/report and replay receipt/report
are retained on the Target as exact root-owned mode-`0600`, single-link files.
Their review time is `2026-07-20 23:59:59 +0800`, not automatic deletion
authority. Persisted normalized synthetic evidence is durable PRD2 proof and
is not a temporary cleanup candidate. No file, row, artifact, receipt, report
or evidence item was deleted.

## Verification Classification

| Layer | Result |
| --- | --- |
| Local producer/receiver identity | Verified by execution owner |
| UniRouter publication CI | Verified for the cited Stage A docs-only publication and retained producer required CI |
| X-Insight named Target | Verified for the exact rc.6 synthetic scenario |
| External integration | Verified by execution owner for this exact manifest-bound synthetic sidecar scenario only |
| Independent acceptance | Not performed |
| `N=1` field usability | Pending |
| Customer/sanitized-real data | Not authorized or used |
| Live feed / automatic freshness | Deferred and not implemented by this proof |
| Production | Not verified |

Three independent read-only reviews passed the locked Stage B gate with P0=0
and P1=0 before execution. They reviewed static boundary/acceptance conditions
only; they did not connect to Target, transfer bytes, import, inspect the real
UI or independently accept the runtime result. All Target and UI verification
above is execution-owner evidence.

## Related Records

- [PRD2 UR-E Bounded Synthetic Sidecar Execution Plan](../../planning/prd2-ur-e-bounded-synthetic-sidecar-execution-plan-2026-07-19.md)
- [UR-E Joint Activation Evidence](ur-e-joint-activation-2026-07-19.md)
- [Runtime Contract Capability Status](../../architecture/runtime-contract-capability-status.md)
- [PRD2-M1 Field Proof Plan](../../planning/prd2-m1-unirouter-field-validation-implementation-plan-2026-07-13.md)
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-19-prd2-ur-e-bounded-synthetic-sidecar-execution.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/evidence/2026-07-19-prd2-ur-e-bounded-synthetic-sidecar-handoff.md`
