# X-Insight A+C UI System V1

Date: `2026-07-31`

Status: implementation design contract.

`A+C` combines a precision investigation workbench with a print-first evidence
dossier. It is intentionally calm, dense and explicit. It must not resemble a
gateway admin console, a generic telemetry dashboard or a science-fiction
graph.

## 1. Shell

- Light neutral canvas; deep ink text; thin cool-gray boundaries.
- Compact 216 px desktop navigation rail, a 56 px context bar, a fluid primary
  canvas and a 360–420 px Evidence drawer.
- At narrow widths the navigation becomes a top row, the drawer becomes an
  in-flow panel and no document-level horizontal scroll is allowed.
- No glass, glow or decorative dark sidebar. The Posture home may use one
  restrained dark priority panel with a tonal gradient; it must carry real
  Alert/Gap state and may not become decorative dashboard chrome.

## 2. Core Screens

### Cross-system Posture Home (`/inbox`)

Above the fold:

1. page purpose and one bounded `as_of` scope;
2. open product Alerts and exact execution/Gap counts;
3. four independent coverage axes with their own numerator, denominator and
   limitation;
4. source-declared runtime path for the selected Alert execution;
5. fixed-window series, source context and explicit Connector trust state.

Global counts and series come from the dedicated Posture aggregate, never from
the first page of Execution search. MaaS operational metrics are context, not a
duplicated second control-plane truth. Connector delivery state stays current
and separate from immutable Evidence aggregates. An Alert without an associated
execution must render an unlinked state; it may not borrow another execution's
Trace to fill the canvas.

### Execution Explorer

The filter bar is stable and URL-addressable. Results emphasize outcome,
decisive path, source/proof class, time, provider/model, completeness and gaps.
The table may become cards at 390 px but must preserve the same information.

### Execution Story

Above the fold:

1. one-sentence outcome;
2. `who → did what → systems/path → end`;
3. decisive failure/recovery;
4. Source/Import/Projection/Causal completeness;
5. exact duration, tokens and micro-CNY where observed;
6. explicit unknowns.

Tabs are `Story`, `Causal graph`, `Chronology`, and `Proof`. Selection persists
across tabs and controls one Evidence drawer.

## 3. Semantic Language

Every semantic state uses text, icon/shape and line/fill treatment; color alone
is insufficient.

| State | Label | Visual contract |
| --- | --- | --- |
| Direct | Observed | filled square, solid border |
| Declared | Source-declared | outlined diamond, solid relationship |
| Derived | Deterministic | ring, dotted internal accent |
| Missing | Gap / Unknown | open break mark, amber dashed boundary |

Red is reserved for observed failure. Amber means incomplete/attention, not
failure. Blue indicates selected Evidence or a stable citation.

## 4. Design Tokens

```text
canvas        #f6f7f8
surface       #ffffff
surface-muted #f1f3f5
ink           #16202a
ink-muted     #5d6975
border        #d9dee3
border-strong #b9c2ca
evidence      #2457d6
evidence-soft #edf2ff
gap           #a96512
gap-soft      #fff6e7
failure       #b42318
success       #18794e
```

Business text uses the system sans stack. Monospace is limited to immutable
IDs, hashes, timestamps, schema versions and exact machine values.

Spacing follows a 4 px base. Main content uses 12/16/24/32 px rhythm. Cards use
8 px corners; buttons and inputs use 6 px corners. Shadows are optional and
must remain subtle enough that boundaries still work in print.

## 5. Evidence Dossier

Evidence Pack sections are stable:

1. cover and sealed revision;
2. scope and redaction policy;
3. findings;
4. execution stories;
5. completeness and Gaps;
6. citations;
7. provenance appendix;
8. verification digest.

Print CSS uses white backgrounds, black/gray text, visible link targets and
page-break controls. Interactive-only controls are omitted. JSON and HTML
exports describe the same sealed revision.

## 6. Interaction and Accessibility

- Core tasks are keyboard complete.
- Focus is always visible.
- Tables have headers and captions; graph nodes are mirrored by an accessible
  ordered list.
- Graph edges are not the only carrier of relationship meaning.
- Loading preserves layout; empty, partial, unavailable and unauthorized use
  different copy and semantics.
- Motion is functional and respects `prefers-reduced-motion`.

## 7. Prohibited Shortcuts

- no invented causal edge or auto-arranged implication of one;
- no completeness represented by one global percentage;
- no Gap rendered as an observed step;
- no `0` substituted for missing tokens, cost, latency or comparison values;
- no trend chart from the four-record compatibility baseline;
- no raw prompt/response content added to make the UI appear richer.

## 8. Productization amendment — 2026-08-02

The first implementation is refined without changing its Evidence semantics:

- primary navigation is grouped as investigation, analysis, trust/access and
  help; Chinese task names lead and canonical English names remain secondary;
- widths below the desktop rail use an explicit menu button and drawer rather
  than a clipped horizontal navigation strip; interactive mobile targets are
  at least 44 px;
- Explorer preserves table semantics but renders each row as a labeled card at
  the compact breakpoint, so source, outcome, evidence state, payload and Gaps
  remain readable without document-level horizontal scrolling;
- Story begins with a four-step journey: initiator, action, systems on the
  source-declared path and terminal point. Missing identity or relationships
  are shown as unknown rather than inferred;
- complete coverage, semantic legends, Saved View creation and Finding editing
  use progressive disclosure. Incomplete axes and recorded Gaps stay open by
  default;
- desktop causal graph receives the full workbench width; narrow screens use
  the equivalent ordered relationship list instead of shrinking the SVG into
  an unreadable strip;
- Connector cards keep source contract, mutable delivery state and Checkpoint
  summary in separate columns; delivery state never looks like Evidence;
- long event identities and hashes stay copyable behind a short technical
  reference instead of dominating Case citations;
- request cancellation caused by React effect teardown is silent. Genuine API
  failures, partial reads and authorization failures retain their contracted
  visible states.

This amendment does not add endpoints, data fields, causal edges or product
claims. It is a presentation and request-lifecycle correction over the same
`0.3.0-rc.3` local candidate.

## 9. Posture home amendment — 2026-08-04

`/inbox` is promoted from a recent-work sample to a cross-system investigation
Posture home. This amendment adds `xinsight.posture.v1` and does not change M0:

- all Evidence aggregates share one bounded `as_of`/`from`/`to` scope in a
  read-only repeatable-read transaction;
- open Alert counts are product analysis records. Their `observed_at` is window
  bounded, while current state is not presented as historical state replay;
- Direct time, Identity, Outcome and Causal coverage remain separate axes; no
  average or synthetic health score is permitted;
- Trace paths use only source-declared parent relationships in the existing
  Trace Journey projection. Chronology never creates an edge;
- configured Connector sources remain visible when the evidence window has no
  matching execution, but mutable trust/checkpoint state is visually and
  semantically separate from fixed execution counts;
- risk rows drill into existing Execution, Case, Discovery and Connector
  surfaces. The homepage does not create a second investigation model;
- 14/12/10/9 px is the supported Posture type ladder for body, controls,
  metadata and technical boundaries. No product text uses the former 7 px
  micro-labels;
- navigation symbols use one stroke-icon family, and the compact layout has no
  document-level horizontal overflow. Long runtime paths may scroll inside
  their explicitly bounded canvas.

The earlier four-record compatibility seed remains test evidence, not a source
for global rates or trend claims.

## 10. Shared workbench hierarchy amendment — 2026-10-09

The accepted September desktop design (07, DESK-01—06 in 32, 36 and v11) and
the October 9 page-by-page review (47 §7) supersede section 9 **as the layout
priority for `/inbox`**. Section 9 remains the contract for the investigation
Posture and its evidence semantics; it is not a reason to put its diagnostic
cards ahead of the shared workbench summary.

- `/inbox` leads with a compact, horizontal summary, then trend and matters
  needing attention, followed by comparable model, purpose and cost groups.
  Recent records and source limitations remain reachable below. Work activity
  is a compact entry to its exact source/run detail, not a replacement for the
  economic or runtime reading.
- Usage, runtime/capabilities, execution search/details, cases and cost keep
  their existing routes, authorization and reading identities. Their first
  viewport prioritizes the customer's question and next action; long coverage
  explanations, opaque IDs, hashes and canonical codes are available in
  disclosures. Missing, unauthorized, unknown and observed zero remain distinct.
- A purpose contribution is offered only when the frozen reading includes that
  dimension. A missing dimension never becomes an observed zero or an account
  wide denial. A selected detail, expired reading or verified revocation is not
  silently substituted with a new reading.
- Work activity may group events only by explicit same-source run identity;
  only current-revision, verified request identifiers navigate to the runtime
  reading. Time proximity, filename extension and co-presence in a run do not
  establish a purpose, resource effect or causal edge. The older system/data
  resource panel remains explicitly limited to synthetic internal records.

This amendment uses the existing React application and APIs. It neither adds
an automatic purpose classifier nor promotes the old Posture aggregates into a
second source of economic or runtime truth. Browser, database, CI and Target
acceptance remain separate evidence layers.
