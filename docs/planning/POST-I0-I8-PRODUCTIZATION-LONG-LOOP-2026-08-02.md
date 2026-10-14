# X-Insight Post-I0–I8 Productization Long Loop

Status: `LOCAL_IMPLEMENTATION_VERIFIED_PR_PENDING`

Date: 2026-08-02

Owner: X-Insight product task

Progress at this revision:

- L0–L9: completed and locally verified;
- L10: exact branch commit, PR-head CI, merge identity and main CI remain open;
- L11: intentionally blocked on a separate User runtime decision.

## 1. Exact baseline and boundary

- Product repository: `foigent/x-insight`
- Baseline commit: `31af6771189d283dac068872582d6b7dcf685356`
- Baseline tree: `f14e544c9fb9ec0911ca7a7cd94aa01162a9bb04`
- Local implementation branch: `codex/product-long-loop-20260802`
- Shared protocol baseline: immutable Foigent Event Connector Protocol `v2.0.2`
- I0–I8 status: implemented and closed as the current functional baseline. This loop does not reopen their accepted data semantics.
- UniRouter Development integration: closed. This loop does not create traffic, credentials, Cursor/Checkpoint mutations, database mutations, deployment changes, or a new connector campaign.

The product order remains:

1. outcome story;
2. source-declared causal path;
3. completeness and explicit gaps;
4. cited evidence;
5. chronology and technical proof.

The shared Event Envelope is still connector input rather than an M0 row. Delivery state remains outside normalized event identity, hashes, event-linked provenance, and product investigation claims.

## 2. L0 verified baseline

The following facts were revalidated from the exact baseline before product changes:

- frontend unit tests: 21/21 passed;
- TypeScript and project checks: passed;
- OpenAPI path check: 36/36 required paths present;
- frontend license inventory: 177 entries validated;
- production frontend build: passed without tracked-source drift;
- exact-main GitHub CI run `30712854633`: success, including Signed V2 and Fresh Install qualification;
- browser console after bounded mock audit: no product console errors;
- page-level horizontal overflow: absent at 1440, 1024, and 390 CSS pixels on the audited routes;
- real rendered audit used synthetic, metadata-only fixtures; it did not use credentials, raw prompts, real traffic, or live connector state.

Rendered routes audited at 1440px include Inbox, Explorer, Execution Story, Causal graph, Cases, Case dossier, Connectors, Discovery, and Agent Access. Explorer was additionally audited at 1024px, and Execution/Causal graph at 390px.

## 3. Verified product gaps

### P0 — correctness and primary-task usability

1. Abort/cancellation can be rendered as a user-facing request failure in development Strict Mode. Cases and Connectors displayed `请求已取消` while their successful response was already visible. Cancellation caused by effect teardown must never become a product error state.
2. At 1024px the Explorer table keeps page-level width but compresses or clips the evidence-status content. The result must remain fully readable without relying on a hidden horizontal-scroll affordance.
3. At 390px the sidebar becomes a narrow horizontal navigation strip with partially hidden destinations. The page does not overflow, but navigation discovery and target size are not acceptable.
4. Execution investigation repeats outcome, proof class, assurance mode, completeness, counts, and stage coverage across several stacked blocks. This obscures the answer to “who initiated it, what happened, which systems handled it, and where it ended”.
5. The source-declared causal graph is semantically correct but visually collapses into a thin horizontal strip. It must make the path and selected node legible while preserving the rule that temporal adjacency never creates a causal edge.

### P1 — hierarchy, density, and investigation efficiency

1. The primary navigation exposes ten peer destinations and mixes English labels, English helper text, and Chinese page content. Investigation, analysis, trust, and administration need explicit groups and consistent visible language.
2. Inbox cards leave large unused areas with sparse data and do not make the next best action sufficiently obvious.
3. Explorer gives filters and Saved View creation too much persistent vertical space. Canonical codes, translated labels, evidence badges, and the separate legend create avoidable visual noise.
4. Case dossier is structurally sound but long: editable Findings, immutable citations, Gaps, scope, notes, and sealing compete on one page. Immutable proof must remain visible while editing controls become calmer and more task-oriented.
5. Discovery and Agent Access use oversized headings and full-width panels for sparse content. Their state boundary is correct, but the density does not match the amount or priority of information.
6. IDs and SHA-256 values are allowed to dominate citation cards. Full identities must remain copyable and inspectable without becoming the primary visual content.

### P2 — polish and scale preparation

1. Empty, loading, degraded, partial, and error states need one visual grammar across all pages.
2. Keyboard focus, skip navigation, semantic regions, form labels, reduced motion, contrast, and touch targets need a consolidated audit after layout changes.
3. The shell needs a predictable compact mode for 1024px and an explicit mobile menu for 390px.
4. Local Playwright audit output needs to remain outside tracked product changes.

## 4. Non-goals for L0–L10

- no new Event, Connector, M0, Gap, provenance, identity, hash, canonicalization, Receipt, Cursor, or Checkpoint semantics;
- no raw prompt/response viewer;
- no speculative graph edges or inference presented as source fact;
- no Kafka, NATS, CDC, plugin marketplace, public code-execution runtime, or universal privileged connector process;
- no new external connector or source-specific branch in the core analysis engine;
- no production multi-tenant/IAM redesign;
- no target deployment, live credentials, traffic, database migration, worker enablement, or live replay before L11 authorization;
- no reopening of closed UniRouter integration work or completed shared-protocol release work.

## 5. Long-loop execution plan

### L1 — freeze the gap ledger and acceptance matrix

Deliverables:

- this exact baseline and boundary;
- P0/P1/P2 gap ledger;
- route-by-route acceptance matrix;
- explicit preservation requirements for I0–I8 semantics.

Exit gate:

- every planned UI change maps to a verified gap or a cross-page design primitive;
- no planned item requires a new product semantic decision.

### L2 — product shell and information architecture

Deliverables:

- group navigation into investigation, analysis, trust, and administration rather than ten peers;
- consistent Chinese primary labels with canonical English names only where they aid expert recognition;
- desktop sidebar, compact 1024px shell, and explicit 390px menu;
- consolidated typography, spacing, border, badge, status, focus, and motion tokens;
- preserve signed-in identity, metadata-only boundary, role, manual link, and skip navigation.

Acceptance:

- all routes remain directly reachable;
- exactly one active navigation state is exposed to assistive technology;
- mobile navigation does not depend on an undiscoverable horizontal strip;
- no page-level horizontal overflow at 390/1024/1440.

### L3 — Inbox and Execution Explorer

Deliverables:

- make Inbox answer “what needs attention and why” before secondary summaries;
- balance sparse cards and make next actions explicit;
- compact Explorer filters without reducing supported query semantics;
- move Saved View creation into a secondary action/disclosure;
- use a responsive result card/list below the table breakpoint;
- show human-readable evidence state first while keeping canonical codes accessible;
- make zero recorded gaps neutral and explicit rather than visually warning-like;
- move the evidence legend into contextual help.

Acceptance:

- failure, success, gaps, proof class, assurance mode, and current/stale state remain distinguishable;
- no filter, `as_of`, source, idempotency, or saved-view semantic changes;
- two audited results are fully readable at 1024px and 390px.

### L4 — Execution investigation workspace

Deliverables:

- one answer-first execution header showing initiator, action, systems/path, outcome, and terminal point;
- remove duplicate badges when proof class and assurance mode resolve to the same visible value;
- keep Story, Causal graph, Chronology, and Proof as distinct views with a clearer selected state;
- increase causal-path legibility and node selection without inventing edges;
- keep completeness and unknown/unprovable boundaries visible but non-repetitive;
- make the Evidence inspector grouped, sticky where useful, and progressively disclose technical identities;
- preserve Add-to-Case citations with fixed `as_of`, event hash, source ref, and related Gaps.

Acceptance:

- a reviewer can identify “who → did what → through which systems → final result” in the first viewport on desktop;
- causal graph uses only API-declared edges and exposes unresolved parents;
- chronology never implies causation;
- raw content remains unavailable;
- Story remains meaningful when graph, gaps, tokens, cost, or parent information is absent.

### L5 — Case and Evidence Pack workflow

Deliverables:

- make Case scope, Findings, citations/limitations, notes, and sealing form an explicit workflow;
- separate immutable citations from editable investigation text;
- reduce full hash/ID dominance while preserving copy and inspection;
- keep manual statements limited to `hypothesis` or `unknown` classifications;
- preserve sealing confirmation and content-boundary warning;
- retain exact JSON/HTML Evidence Pack downloads for sealed revisions.

Acceptance:

- Findings cannot silently become evidence-backed or deterministic;
- every cited Gap remains a first-class limitation;
- sealed revision identity remains immutable and visible;
- Case work never mutates M0 Evidence.

### L6 — secondary workspaces

Deliverables:

- Sessions: clarify source-declared session grouping and its coverage boundary;
- Compare: keep missing/partial/unavailable distinct from zero and equal;
- Connectors: distinguish source/manifest fidelity from mutable worker/import/checkpoint state;
- Discovery: present candidates, evidence support, limitations, and abstention as reviewable output rather than fact;
- Agent Access and Access Sessions: separate read-only identities from human sessions and make revoke scope explicit;
- harmonize density and page hierarchy with the primary investigation workspace.

Acceptance:

- no mutable delivery state enters Evidence identity;
- no hypothesis is presented as system fact or automatic enforcement;
- revocation remains explicit and confirmation-bound;
- role and permission boundaries remain enforced by existing APIs.

### L7 — cross-cutting product quality

Deliverables:

- eliminate abort teardown false errors and normalize request-state handling;
- shared loading, empty, degraded, partial, and safe-error components;
- keyboard and focus-order audit;
- 44px mobile targets where interaction requires touch;
- contrast and reduced-motion audit;
- stable layouts at 390, 768/1024, 1280, and 1440 widths;
- bounded performance check for initial shell and investigation route.

Acceptance:

- no stale request overwrites a newer page state;
- abort is silent, authorization failure logs out or fails closed as already contracted, and real server errors remain visible;
- no browser console errors in the bounded acceptance scenario;
- no page-level horizontal overflow at required widths.

### L8 — minimum supporting API/code corrections

Only implement backend or API changes proven necessary for the UI closure. Candidate work currently includes request-state correctness and presentation helpers; there is no accepted need for a new endpoint or data semantic.

Acceptance:

- OpenAPI remains compatible;
- existing evidence identity/hash/provenance/gap fixtures remain byte-stable;
- full Go, frontend, Postgres-backed, protocol-pin, and compatibility checks pass.

### L9 — release-facing documentation and evidence

Deliverables:

- update release identity only after the product delta is final;
- update Chinese product manual and route descriptions;
- create a bounded customer acceptance script covering failure/gap, recovered fallback, Case citation/seal, Connector degraded state, Discovery abstention, and Agent read access;
- record before/after screenshots as local review evidence without committing generated browser artifacts unless explicitly required;
- state exactly what was locally tested versus target-validated.

### L10 — repository and CI closure

Deliverables:

- run formatting, type checks, unit tests, race tests, Postgres-backed suites, frontend build, browser acceptance, license checks, OpenAPI checks, protocol v2.0.2 pin/conformance, and release/install checks as applicable;
- inspect the complete diff for semantic drift and unrelated changes;
- commit intentionally on the product branch;
- push and open a focused PR only under existing product-repository authorization;
- require exact-head CI success;
- merge only after the exact reviewed tree is proven unchanged;
- verify exact main commit/tree and terminal main CI.

### L11 — first material external decision

L11 is not automatically authorized by this contract. Stop and ask the User to choose whether to:

1. deploy the reviewed product build to the named non-production target;
2. run only synthetic target acceptance;
3. additionally authorize a bounded real Connector/traffic study;
4. defer all runtime work and keep the result repository-only.

Any choice involving credentials, traffic, worker activation, Cursor/Checkpoint, database mutation, security-group changes, public listeners, deployment, or production-like evidence needs exact scope and rollback criteria before execution.

## 6. Route acceptance matrix

| Route | Primary user question | Required preserved truth | Productization gate |
| --- | --- | --- | --- |
| Inbox | What needs attention now? | bounded snapshot; partial reads remain explicit | next action visible without scanning all cards |
| Executions | Which execution should I investigate? | exact filters, `as_of`, outcome, gaps, proof/assurance | readable at 390/1024/1440 |
| Execution Story | What happened end to end? | outcome-first, evidence-cited, no invented claims | initiator/action/path/end in first desktop viewport |
| Causal graph | Which declared actions caused or parented which? | declared edges only; unresolved parents explicit | legible path and node selection |
| Chronology | In what order were facts observed? | time order is not causation | distinct visual language from causal graph |
| Proof | What immutable evidence supports the story? | event ID/hash/source ref/provenance/gaps | full identity inspectable without dominating layout |
| Cases | What investigations are active? | writable records separate from M0 | no false cancellation/error state |
| Case dossier | What can be concluded and what remains limited? | finding classification, immutable citations, first-class Gaps | edit/review/seal workflow is explicit |
| Sessions | Which events belong to a declared session? | source-declared scope only | coverage boundary visible |
| Compare | What differs and what is unavailable? | missing is not zero; partial is not complete | unavailable comparisons cannot look equal |
| Connectors | Can this source be trusted and is delivery current? | manifest/fidelity separate from mutable delivery | state layers visually separated |
| Discovery | What should a reviewer investigate next? | hypotheses/abstention, evidence and limitations | never presented as fact or enforcement |
| Access Sessions | Which human sessions exist and can be revoked? | current role and revoke boundary | destructive intent requires confirmation |
| Agent Access | Which bounded read identities exist? | separate, expiring credentials; token never displayed again | permission and expiry visible; revoke explicit |

## 7. Completion rule

L0–L10 are complete only when implementation, relevant tests, bounded browser verification, exact PR-head CI, and exact main CI all agree. A locally rendered page is not target evidence; a passing PR is not deployment evidence; a target synthetic test is not real-traffic or production evidence.

If implementation exposes a material semantic or product choice—rather than a reversible presentation decision—work stops at that exact point with evidence, options, trade-offs, and a recommendation.
