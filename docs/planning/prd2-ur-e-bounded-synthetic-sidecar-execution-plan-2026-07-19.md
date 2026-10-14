# PRD2 UR-E Bounded Synthetic Sidecar Execution Plan

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

Status: Joint decision accepted and binding; exact Stage A/Stage A.5/Stage B
synthetic sidecar scenario execution-owner verified

Prepared: 2026-07-19

Decision owner: User

X-Insight execution owner: Codex

Current work risk: R2 evidence/status publication; no further Target write is
active

Executed Stage B risk: R4 named non-production Target write, constrained to the
single accepted artifact and completed after every conditional gate passed

## Outcome

Prove one exact producer-to-consumer path without expanding the accepted
operator-mediated sidecar boundary:

1. UniRouter generates one genuinely new, manifest-bound, metadata-only
   synthetic `unirouter.prd5.xinsight.snapshot.v2` artifact from a clean,
   qualified producer checkout.
2. The artifact is transferred through one approved private operator path.
3. X-Insight imports that exact artifact once and then replays the identical
   bytes and SHA-256 on the named non-production Target.
4. X-Insight verifies normalized persistence, receipt, bounded API and real UI
   readback for only the scenarios actually contained in the artifact.

If every gate passes, the maximum permitted result is:

> External integration verified — execution-owner verified, exact manifest-
> bound synthetic sidecar scenario only.

This is not independent acceptance, field usability or Production evidence.

## Current Verified Planning Baseline

| Item | Planning fact | Treatment |
| --- | --- | --- |
| Binding transport | Operator-mediated `xinsight.import-receipt.v1` sidecar | Remains unchanged |
| Live-feed slice | Deferred by the accepted 2026-07-18 decision | Not reopened |
| X-Insight planning main at plan creation | `f87d28d682898ff43f32360b67a83366282f17d2` | Historical docs identity; Stage A.5 execution base was clean `480f5d8f...` |
| X-Insight deployable rc.6 source | `0ca502f99181173e3542592ae5a018a5bfc43e0c` | Reverified unchanged before and after Stage B |
| Intended Target | `launch-advisor-20260710` / `REDACTED_TARGET_INSTANCE_ID` | Named non-production Target only |
| UniRouter response-authoring baseline | docs-only main `f96fb0394a8dd523aed82e2163bf314711c366bd` | Historical observation; not a permanent generation identity |
| UniRouter U0 planning main | docs-only main `c30f9a67588eab4c13486bf08512c8f4bea01ffd` | Historical U0 checkpoint; superseded for generation by the clean correction main |
| UniRouter correction main | docs-only main `9a94d54975017de3b6b209d47f561af32c072178` | Reverified as the clean Stage A generation-checkout base; complete diff from producer authority was `docs/**` only |
| UniRouter producer code identity | `5176654d7d320b56c1c136f39b3debb8156ece4b` | Retained because the complete generation-HEAD diff contained `docs/**` paths exclusively |
| UniRouter producer PR head / required CI | `be2d5f5d1cd6d322728b3d7c9aa8a5a347650563` / `29640791281` successful | Retained; no non-`docs/**` producer change was present |
| Corrected owner-authored response | SHA-256 `12857bd7a37436608461d22caf3c9f1fb8dbfac7274f446384349f488dba8cee` | Verified response and publication identity; not an artifact |
| Binding joint decision | activated `2026-07-19 12:15:26 CST`; SHA-256 `c674b04f719b64d9746f2afb4f9d5020d7f49fe96af5d1254a83e7cd911595a1` | Activation authorized Stage A; later execution followed the decision's sequential gates |
| Previously committed X-Insight fixture SHA-256 | `93bc4c8448f1f838668f9dab10cbba070f0df550665b16d73bb494066fe8d6a1` | Forbidden as the new artifact |
| Previously recorded later UniRouter artifact SHA-256 | `94c8db5610037e87c18e196aecdf40ce27888e9375c14f139beb8c7953dfa4a5` | Forbidden as the new artifact |

At plan creation the X-Insight Target state had not yet been reverified. The
later Stage A.5/Stage B evidence reverified it before and after execution. The
UniRouter worktree observed at plan creation had unrelated uncommitted
documentation changes and those bytes remain excluded. The correction
publication produced clean main `9a94d549...`; Stage A reverified that clean
generation checkout before the seed/producer operation and later published its
sanitized evidence through commit `640e58f...`, PR #223, docs-only CI
`29674988916` and merge `df3e41d...`.

## U0 Response And Activation Checkpoint

UniRouter's owner-authored response covers all ten requested items. Its PR #221
head `325ad63438b7c09255793f0dfabc53450c3eb16c` passed routed CI
`29670357845` and merged as
`c30f9a67588eab4c13486bf08512c8f4bea01ffd`; local UniRouter `main`,
`origin/main` and the clean worktree match that identity. The producer remains
qualified by PR #219 / CI `29640791281`, and the complete diff from
`5176654d...` to current main contains only `docs/**`.

The initial destination-absence check followed by macOS `install -m 0600` was
rejected as a P1 because it did not atomically enforce no-overwrite. UniRouter
published the pinned exclusive-create correction through PR #222, head
`85b2614...`, routed CI `29672169049` and clean main `9a94d549...`. X-Insight
verified the response's final SHA, pinned interpreter/mechanism identities, a
generic successful copy and a fail-closed `EEXIST` case. The one exact temporary
probe file was individually removed and verified absent. This is execution-
owner verification, not independent runtime acceptance.

The shared writer atomically activated the decision at
`2026-07-19 12:15:26 CST`. P0=0 and P1=0. No seed, manifest, artifact, handoff,
transfer or Target operation occurred during correction or activation.

## Scope

- exactly one new synthetic snapshot-v2 artifact;
- exactly one new manifest, request ID, source-row time, `generated_at`, source
  instance and UniRouter-source-created (producer-generated) exchange ID;
- one explicit scenario inventory, initially expected to be one usage row plus
  one missing-trace capture gap;
- one first-write and one identical-artifact replay on the named Target;
- receipt, SQL/persistence, bounded API and real UI evidence;
- immutable hashes, sanitized evidence and an explicit retention/cleanup
  disposition; and
- a single-active-writer baton: UniRouter repository Stage A first, then an
  explicit handoff to X-Insight repository Stage B, with no concurrent writers.

## Non-Goals

- no UniRouter or X-Insight product-code change;
- no endpoint, live feed, polling, cursor protocol or freshness SLO;
- no exporter, automatic receipt transport, durable import job or deep link;
- no customer or sanitized-real data, raw prompt/response, credential, secret,
  raw identifier, raw IP or raw upstream error;
- no denial or trace-present claim unless such a scenario is separately
  authorized and actually present;
- no second artifact, alternate Target or Production environment;
- no X-Insight Target write outside Stage B's exact mode-`0600` manifest/
  artifact paths, exact receipt/report paths, first-write and identical replay;
- no X-IAM, Tool Gateway, public/multi-tenant access, HA/DR or Phase 1 reopen;
  and
- no deletion contract for normalized X-Insight evidence.

## Activation Gate — Satisfied

No generation, transfer or Target write was authorized until all of these
became true. They were satisfied at activation; only Stage A was reachable at
that checkpoint. The later sequential gates are recorded in the completion
evidence:

1. X-Insight publishes the links in this planning chain through its current
   independent branch, PR, required CI and merged main; the decision records
   that merge SHA.
2. UniRouter creates its owner-authored formal response at
   `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/responses/2026-07-19-unirouter-prd2-v2-sidecar-external-integration-artifact-response.md`
   and accepts this exact bounded scope.
3. UniRouter links the same proposed-decision path as pending activation from
   its authoritative planning/status chain through its own branch, PR, required
   CI and merged main; the decision records that merge SHA.
4. UniRouter publishes an owner-authored amended response satisfying the
   exclusive-create correction through a new branch, PR, required CI and merged
   main. The response records those publication identities; X-Insight
   externally calculates the completed response SHA-256 and verifies the exact
   exclusive-create behavior and evidence before activation.
5. One designated shared-decision writer atomically updates the shared decision
   `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-19-prd2-ur-e-bounded-synthetic-sidecar-execution.md`
   from `Decision proposed`/`Not binding` to `Decision accepted`/`Binding`,
   records the corrected response input, both planning merge SHAs and the
   correction publication identities, and reconciles the shared root plus
   `decisions/README.md` Proposed/Accepted indexes without weakening any
   condition.
6. The designated UniRouter Stage A execution owner has exclusive write
   ownership of the UniRouter repository and its Stage A evidence. After a
   completed handoff, the designated X-Insight execution owner takes the baton
   for Stage B. At no time may multiple agents write either repository or the
   shared decision concurrently.

An Open request, response, shared evidence record or this X-Insight plan alone
does not activate the work. The recorded atomic `Decision accepted` / `Binding`
transition is the activation mechanism.

## Stage A — UniRouter Responsibilities

### U0 — Formal Acceptance And Planning Chain — Complete

UniRouter completed the accepted U0 sequence. The retained requirements were:

1. answer `accept`, `decline` or `defer` in the required UniRouter-owned
   response; do not append X-Insight-authored facts to an older UniRouter
   response;
2. state its current main, the producer code identity, producer PR head and
   required CI run separately;
3. confirm that generation will record the complete
   `git diff --name-status 5176654d... <generation-head>` inventory; any non-
   `docs/**` changed path requires a new producer code identity and successful
   required CI before generation;
4. state the isolated synthetic seed/database, producer command/version and
   private transfer/custody mechanism without writing a DSN or secret;
5. link the joint decision candidate from `docs/planning/PRD5-BACKLOG.md`,
   `docs/PROJECT-HANDOFF.md` and `docs/ops/CURRENT-STATUS.md`, explicitly marked
   pending joint activation, through an independent branch, PR, required CI and
   merged main; and
6. provide the response and merged-main/CI identities, then hand control back
   so one designated shared-decision writer can perform the atomic acceptance/
   binding update before any artifact bytes are generated.

### U1 — Fresh Artifact Generation

After activation, the UniRouter execution owner must:

1. use a clean current-main checkout or isolated clean worktree;
2. create one exact `unirouter.prd5.xinsight.approval.v1` manifest with a new
   source instance, new concrete synthetic request ID and one scenario label;
3. seed a task-owned isolated PostgreSQL database with only the approved
   metadata-only synthetic row, using a source time after decision activation;
4. use a canonical non-negative `cost_micros` value other than the historical
   fixture value `"7"`;
5. if the scenario is usage plus missing trace, emit the approved usage row and
   deliberately omit trace rows so the gap is truthful;
6. run the existing producer and record exact current main, producer code,
   producer PR head, required CI, command, toolchain and complete
   `git diff --name-status 5176654d... <generation-head>` inventory. If any
   changed path is outside `docs/**`, stop unless a new producer code identity
   and successful required CI are supplied;
7. bind the exact manifest SHA-256, generate a fresh exchange ID and
   `generated_at`, write the artifact as mode `0600`, and record byte size and
   artifact SHA-256;
8. fail closed if the artifact SHA-256 equals `93bc4c...`, `94c8db...` or any
   other previously recorded artifact;
9. scan/classify the output for the excluded raw/customer/secret classes and
   record the exact call-log, trace, gap and scenario inventory; and
10. retain artifact bytes outside the shared cooperation folder and hand them
    over only through the approved private operator path to one exact non-
    Target operator-local receiver path.

## Stage A.5 — Sequential Handoff And Read-Only Receiver Preflight

The exact sanitized handoff record is:

`PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/evidence/2026-07-19-prd2-ur-e-bounded-synthetic-sidecar-handoff.md`

It is created only after Stage A and must not contain artifact bytes, a DSN,
credential, secret or raw payload:

1. the UniRouter owner writes only the producer/transfer half, including the
   manifest/artifact/producer/scenario identities, exact sender path and exact
   non-Target operator-local receiver path, sets
   `producer_complete_receiver_pending`, and releases the shared-file writer
   baton;
2. the X-Insight owner then recomputes the receiver SHA-256 and performs only
   read-only Target/release/database/pre-existence checks, binds the exact
   Target mode-`0600` manifest/artifact paths plus receipt/report paths, appends
   the receiver half and sets either `stage_b_gate_passed` or `stopped`; and
3. no Target import is reachable until `stage_b_gate_passed`. A stopped record
   cannot be replaced with another artifact under this decision.

## Conditional Artifact Gate

Stage B is authorized for the generated artifact only when the exact Stage A.5
handoff record binds all of the following:

- manifest bytes and SHA-256;
- new request ID, source instance, source-row time, `generated_at` and exchange
  ID;
- generation checkout, producer source, PR head and required CI identities;
- artifact schema, byte length, mode and SHA-256;
- actual call-log, trace, gap and scenario inventory;
- producer-side no-raw/no-secret classification;
- exact sender path, transfer mechanism and exact non-Target operator-local
  receiver path;
- receiver-recomputed SHA-256 equal to the sender SHA-256;
- exact future Target mode-`0600` manifest/artifact paths and receipt/report
  paths;
- current X-Insight Target, deployed release/source and database identity; and
- a source-qualified lookup proving the new request does not already exist in
  the Target database before first-write.

Any missing or mismatched field stops the work. The artifact must not be
replaced or regenerated under the same decision. First-write uses this new
artifact; replay intentionally reuses the exact same bytes and SHA-256.

## Stage B — X-Insight Responsibilities

After the conditional gate passes, Codex as the X-Insight execution owner may:

1. verify the named Target, installed rc.6 identity, loopback listeners,
   database and approved operator access path without changing configuration;
2. record pre-import counts/fingerprint and the absence of the new
   source-qualified request;
3. place the exact manifest/artifact bytes at the decision-bound Target
   mode-`0600` paths and reverify SHA-256;
4. perform one import of the exact artifact, writing receipt/report output only
   at the decision-bound paths;
5. replay the identical artifact, again using only decision-bound receipt/
   report paths, and prove duplicate/no-op behavior with zero
   new inserts and zero conflicts;
6. verify only the expected normalized events and gaps through SQL/persistence,
   bounded API and real UI search/detail/timeline/node/source/provenance/gap
   paths;
7. reconcile first-write, replay and final evidence fingerprints; and
8. record exact retention and cleanup dispositions for every named temporary
   file.

Stage B does not authorize deployment, configuration changes, another artifact
or data outside the exact synthetic inventory.

## Acceptance Matrix

| Gate | Required executable evidence | Fail/stop condition |
| --- | --- | --- |
| Governance | UniRouter-owned response; both authoritative planning links merged through independent PR/CI; both merge SHAs; atomic joint `Decision accepted` + `Binding` + response/root/decisions-index reconciliation | Any element missing, unmerged link, split state, owner ambiguous or conditions weakened |
| Producer identity | Clean generation HEAD; complete diff against `5176654d...` contains only `docs/**`, preserving CI `29640791281`, or a newly qualified producer source/CI pair | Dirty checkout, any non-docs change without new CI, missing diff inventory or docs-only HEAD presented as producer identity |
| Freshness | New request/source/time/exchange/manifest/artifact identities; non-`7` cost; SHA differs from both prior artifacts | Reused fixture, reused producer artifact, pre-activation source time or pre-existing Target request |
| Data safety | Isolated synthetic seed and bounded no-raw/no-secret classification | Customer/sanitized-real data, raw payload, credential, secret or unapproved identifier |
| Transfer | Exact sender/non-Target-local receiver/Target paths, mode `0600` and equal SHA-256 before import | Shared-folder artifact bytes, unknown custody/path, wrong mode or hash mismatch |
| First-write | Expected event/gap inserts, receipt, persistence and fingerprint | Unexpected row, conflict or missing expected evidence |
| Replay | Same bytes/SHA, duplicate/no-op only, zero new inserts/conflicts | Regenerated replay or any additional mutation |
| Product readback | Bounded API and real UI show only the contained scenario with truthful gap/provenance labels | Unsupported scenario claim or UI/API/persistence mismatch |
| Retention | Normalized synthetic evidence, receipts, hashes and sanitized evidence retained; every named temporary file has a disposition | Blanket deletion, unknown-path deletion or normalized-evidence deletion |
| Claim | Exact qualified External-integration sentence and identity inventory | “integration complete”, independent acceptance, field, live-feed or Production language |

## P0/P1/P2 Treatment

- P0: any customer/raw/secret material, wrong Target, unapproved mutation,
  artifact substitution or destructive cleanup. Stop immediately and do not
  publish a success claim.
- P1: missing formal acceptance, dirty/unqualified producer identity, reused
  artifact, hash mismatch, absent pre-existence proof, replay mutation or
  API/UI/persistence disagreement. The gate remains incomplete.
- P2: non-blocking evidence presentation or operator-runbook clarity issue that
  does not weaken identity, safety, data truth or the bounded claim. Record it
  without promoting the result beyond the passed scope.

## Retention And Cleanup

- Retain normalized synthetic events/gaps, import receipts, hashes and
  sanitized durable evidence as the PRD2 proof.
- Give each exact temporary seed, manifest, artifact, report and receipt-copy
  path an explicit `retained` or `deleted` result.
- Every retained temporary item also names its custodian and review/expiry time;
  no date implies no automatic deletion authority.
- A seed database/container/volume may be retired only under separately
  applicable authority bound to its exact identity. Single-file cleanup rules
  do not authorize database, container, volume or directory removal.
- No unknown future path has advance deletion authority.
- No recursive, wildcard, directory or multi-file cleanup is allowed. Each
  permitted deletion is a separately verified single-file operation under the
  applicable project rule.
- Do not delete normalized X-Insight evidence; PRD2 has no general downstream
  deletion contract.

## Completed Binding State And Next Baton

Both sides accepted the bounded scope, published their planning links and
completed the exclusive-create correction/verification. UniRouter Stage A
generated and handed off exactly one fresh metadata-only synthetic artifact.
X-Insight Stage A.5 bound the receiver, Target and six evidence paths; the
corrected Stage B gate then passed with three static read-only reviews reporting
P0=0/P1=0.

The X-Insight execution owner transferred the manifest and snapshot once each,
completed one exact first-write and one identical-byte replay, and reconciled
SQL, bounded API, real UI, file custody and final Target health. The exact
one-call/zero-trace/one-gap scenario is execution-owner External integration
verified. The next PRD2 baton is not another UR-E artifact: it is the separately
accepted manual-guided `N=1` field task when the customer/UniRouter field window
is available. Any second artifact, sanitized-real data, live feed or Production
claim requires its own applicable authority.

## Related Records

- [Phase 2 Product Priority](phase-2-product-priority.md)
- [Phase 2 Batch 0 Scope And Decision Register](phase-2-batch-0-scope-and-decision-register-2026-07-13.md)
- [PRD2-M1 UniRouter Execution Investigation Field Proof](prd2-m1-unirouter-field-validation-implementation-plan-2026-07-13.md)
- [Runtime Contract Capability Status](../architecture/runtime-contract-capability-status.md)
- [UR-E Cross-Project Governance Correction](../evidence/prd2/ur-e-cross-project-governance-correction-2026-07-19.md)
- [UR-E Joint Activation Evidence](../evidence/prd2/ur-e-joint-activation-2026-07-19.md)
- [UR-E Exact Synthetic External Integration Evidence](../evidence/prd2/prd2-ur-e-exact-synthetic-external-integration-evidence-2026-07-19.md)
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/requests/2026-07-19-x-insight-prd2-v2-sidecar-external-integration-artifact-request.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-19-prd2-ur-e-bounded-synthetic-sidecar-execution.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-11-prd5-runtime-contract-v1-boundary.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-18-prd2-completed-execution-live-feed-deferral.md`
