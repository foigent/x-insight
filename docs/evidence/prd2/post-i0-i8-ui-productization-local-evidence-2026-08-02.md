# Post-I0–I8 UI Productization Local Evidence

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

Status: `LOCAL_VERIFIED_PR_AND_RUNTIME_NOT_PROVEN`

Date: 2026-08-02

## 1. Identity and scope

- Repository: `foigent/x-insight`
- Clean implementation worktree: `/private/tmp/xinsight-product-long-loop-20260802`
- Branch: `codex/product-long-loop-20260802`
- Starting commit: `31af6771189d283dac068872582d6b7dcf685356`
- Starting tree: `f14e544c9fb9ec0911ca7a7cd94aa01162a9bb04`
- Shared protocol baseline: immutable `foigent/event-connector-spec` `v2.0.2`
- Product/manual candidate identity retained: `0.3.0-rc.2` and
  `0.3.0-rc.2/manual-r2`

The original dirty checkout at `PRIVATE_HOME/Desktop/projects/X-Insight` was not
used for implementation and was not modified by this loop. The product delta
is limited to presentation, request teardown handling, tests, generated web
assets, manual text, release notes and productization evidence. No Event,
Connector, M0, Gap, provenance, identity, hash, canonicalization, Receipt,
Cursor or Checkpoint semantic was changed.

## 2. Implemented product delta

- regrouped the ten peer destinations into investigation, analysis, trust and
  access, with an explicit narrow-screen menu;
- made Inbox and Execution Explorer responsive and answer-first;
- changed the execution Story to lead with initiator, human-readable action,
  source-declared systems/path and terminal outcome;
- made causal relationships legible on desktop and exposed the same declared
  relationships as an accessible ordered list on narrow screens;
- reduced repeated assurance/proof/completeness labels while keeping technical
  identities and coverage limitations inspectable;
- separated immutable Case citations from editable Findings and quieted full
  hash/ID presentation without removing copyable values;
- separated Connector source contract, mutable delivery state and Checkpoint
  summary;
- normalized abort teardown handling so development effect cleanup does not
  render a false product error;
- added consistent responsive, focus, touch-target, reduced-motion, loading,
  empty, partial and degraded-state presentation;
- updated the Chinese manual, compatibility matrix, design contract, release
  notes and bounded customer acceptance script.

No supporting backend endpoint or data-model change was needed.

## 3. Local verification passed

### Frontend and contract gates

- exact toolchain: Node `22.23.1`, npm `10.9.8`;
- TypeScript project check: passed;
- Vitest: 2 files, 22/22 tests passed;
- OpenAPI consumer check: 36/36 required paths present;
- locked dependency license inventory: 177 entries validated;
- npm dependency audit: 0 known vulnerabilities at the time of this Local run;
- production build and generated-asset reference validator: passed;
- generated manual and web distribution: 20/20 files were byte-identical
  before and after a second complete regeneration;
- Playwright: 15/15 passed across desktop, 1024px laptop and 390px narrow
  projects.

### Go, compatibility and release-facing gates

- Go formatting check: passed;
- repository CI router self-tests: 11 passed;
- contract gate self-tests: 8 passed;
- release qualification identity self-tests: 8 passed;
- shell syntax validation for the bounded release/qualification scripts:
  passed;
- `go vet ./...`: passed;
- `go test -p 1 -race -coverprofile=/private/tmp/xinsight-coverage-20260802.out ./...`:
  passed for all runnable packages when executed outside the filesystem/network
  sandbox required by local `httptest` listeners;
- shared fixture conformance: 11/11 passed;
- Reference Connector runtime verifier: passed against repository fixtures;
- LiteLLM runtime verifier: passed against repository fixtures;
- the original checkout remained preserved and unrelated changes were not
  incorporated.

### Bounded rendered audit

Synthetic metadata-only browser fixtures were reviewed for Inbox, Explorer,
Execution Story, Causal graph, Cases, Case dossier, Connectors, Discovery and
Agent Access. Explorer was also reviewed at 1024px; execution Story, causal
relationships and the explicit navigation menu were reviewed at 390px.

- page `scrollWidth` equalled `clientWidth` at 1440, 1024 and 390 CSS pixels;
- no product console error was observed in the bounded routes;
- browser output remains ignored under `output/playwright/` and is not part of
  the release payload.

## 4. Not executed or not proven locally

- PostgreSQL-specific tests that require `XINSIGHT_POSTGRES_DSN` were skipped
  because no approved Local PostgreSQL DSN was present;
- Docker/Compose qualification was not run because Docker is unavailable on
  this machine;
- no signed release artifact is claimed before an exact commit exists;
- no PR-head CI, main CI, target deployment, target browser acceptance,
  external Connector integration, real traffic or Production behavior is
  claimed by this record.

The missing Local PostgreSQL and Docker environments must be covered by the
repository PR/main workflows before L10 can close. A CI pass is still not a
deployment or Production proof.

## 5. Runtime boundary

This loop performed no deployment, credential or permission change, public
listener or security-group change, traffic generation, Connector/worker
activation, replay, Cursor/Checkpoint mutation or database mutation. The
completed-execution Feed remains outside this Local productization run.

Any named-target deployment or real Connector study remains an L11 User
decision with an exact environment, fixture/traffic scope, rollback and
evidence boundary.
