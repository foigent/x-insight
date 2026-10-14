# PRD1 Readiness Review v0.1

Status: readiness review, not final approval
Created: 2026-07-08

## Purpose

This document reviews whether the current PRD1 candidate materials are ready to move from discussion into execution.

It does not replace [PRD1 Draft v0.1 Candidate](./prd1-draft-v0.1-candidate.md). It classifies the draft into:

- `Locked`: stable enough for PRD1 implementation planning.
- `Candidate`: directionally right, but should still be validated before final PRD lock.
- `Open`: unresolved or evidence-dependent.

## Summary Judgment

PRD1 is ready to start M0/M1 implementation planning.

PRD1 is not ready for final release-scope lock.

Reason:

- the evidence-spine architecture is sufficiently converged;
- the UniRouter first integration boundary is clear enough to start;
- the first implementation package is deliberately narrow;
- remaining uncertainty is mostly downstream of real data import, LiteLLM validation, UI rendering, payload policy review, and open-source/commercial packaging.

The right next move is not more abstract category discussion. The right next move is to prove Gate A backend evidence with UniRouter data.

## Locked For M0/M1

These decisions are stable enough to start implementation planning.

| Area | Decision | Why Locked |
| --- | --- | --- |
| PRD1 wedge | AI execution evidence, not generic observability. | This is the core differentiation and has survived market/advisor/internal review. |
| Product boundary | X-Insight integrates with gateways; it does not replace them. | Competing as another gateway would collapse the product into a commodity category. |
| First deep integration | UniRouter first through X-Insight-side DB/API import. | It gives controlled data, debugging access, and avoids request-path dependency. |
| First external credibility path | LiteLLM early, but after M0/M1, through real callback plus spend-log import. | It proves X-Insight is not a UniRouter plugin without slowing Gate A. |
| First architecture spine | Native evidence envelope, adapter manifest, source refs, idempotency, capture gaps, Postgres store. | Without this spine, adapters become isolated demos. |
| Store | Postgres first. | PRD1 needs relational correctness and local/on-prem simplicity more than high-volume analytics. |
| Analytics | ClickHouse later. | Data volume and query shape are not yet proven. |
| Protocol posture | Native JSON/HTTP evidence API first; OTel/OpenInference as compatibility bridge later. | Governance-specific fields should not be constrained by external tracing schemas too early. |
| Privacy default | Raw prompt/response off by default. | Required to avoid turning X-Insight into a sensitive-data liability. |
| Payload model | Governed capture modes, not a boolean switch. | Enterprise on-prem customers may need optional capture, but it must be auditable and policy-controlled. |
| Enforcement posture | Observe-first plus historical policy simulation. | Inline blocking before evidence trust creates adoption and operational risk. |
| First code package | M0/M1 only: schemas, migrations, validator, fixtures, UniRouter importer, read APIs. | It proves the foundation without front-loading UI, LiteLLM, policy engine, or extra adapters. |
| M0/M1 exclusions | No React UI, LiteLLM adapter, policy simulation, evidence pack, payload object store, UniRouter outbox, OTel backend, one-api/new-api. | Prevents scope explosion before Gate A. |
| UniRouter dependency | No synchronous dependency from UniRouter request path to X-Insight. | Failure isolation is mandatory. |
| First product object | `Execution`. | Accepted as flexible enough for a gateway request now and multi-step agent run later. |
| First lookup key | Request ID. | Current gateways expose request-level identity more reliably than full agent-run identity. |

## Candidate But Not Final

These are strong enough to keep in the PRD1 draft, but should remain adjustable before final PRD lock.

| Area | Candidate Direction | What Must Validate It |
| --- | --- | --- |
| Product language | "AI Execution Evidence Plane" for PRD1. | Check whether early users understand it better than "Flight Recorder", "Evidence Ledger", or "Audit Ledger". |
| M2 UI structure | Execution search -> detail -> timeline -> node detail -> gaps -> payload state -> diagnostics. | Must render real UniRouter imported data without looking artificial or too trace-like. |
| Timeline evidence labels | `direct`, `derived`, `suppressed`, `gap`, optionally `declared`/`unknown`. | Must be understandable in UI and map cleanly from real source data. |
| LiteLLM fidelity | F1/F2, not F3. | Real callback/spend data must confirm which fields are reliably available. |
| Evidence pack | Compact execution-level view/export. | Needs real operator use: audit handoff, incident review, or governance review. |
| Policy simulation | Simple structured rules over historical evidence. | Needs two concrete rules that are meaningful across UniRouter and LiteLLM. |
| UniRouter F3 field list | trace/span/decision/capture/actor/payload/evidence structural fields. | Real import should confirm which gaps hurt timeline and policy simulation most. |
| Open-source core | schema, collector, adapter manifest, basic store/UI, fixtures, reference adapters. | Must be reconciled with license and commercial packaging. |
| Commercial layer | enterprise RBAC, SSO, audit-on-read, retention, advanced policy, premium connectors, HA/managed service. | Needs packaging discussion and buyer validation. |

## Open Before Coding M0/M1

There are no unresolved product-level blockers for starting M0/M1.

The open items before coding are implementation details:

- exact JSON Schema file contents;
- exact Go JSON Schema validation library;
- exact migration runner or migration convention;
- exact migration SQL;
- exact deterministic canonical JSON implementation;
- exact UniRouter DB/API import query shape against the current environment;
- exact fixture format and expected-result runner.

These should be decided during implementation because they are better answered by code and test feedback than by more abstract discussion.

## Open Before Final PRD1 Lock

These should not block M0/M1, but they should block final PRD1 release lock.

| Area | Why It Blocks Final Lock |
| --- | --- |
| Gate A evidence | Without real UniRouter import, the product may still be a theory. |
| Gate C LiteLLM evidence | Without LiteLLM callback/spend import, public ecosystem credibility is weak. |
| UI proof | The first UI must be human-readable, not just a prettier trace dump. |
| Payload policy review | On-prem optional capture needs retention, encryption, audit-on-read, RBAC, redaction, export/delete, and legal hold posture. |
| Policy simulation rules | Simulation must prove governance value rather than feel disconnected from evidence. |
| License | Public open-source release requires a deliberate license decision. |
| Commercial boundary | Need enough clarity to avoid open-source confusion or future repo split pain. |
| Evidence pack format | Needs a concrete use case: audit, incident, compliance, or platform operation. |

## Explicit Non-Reopen Items For PRD1

These should not be reopened unless implementation evidence proves the current decision wrong.

- Do not make X-Insight a gateway.
- Do not require replacing UniRouter, LiteLLM, one-api, or new-api.
- Do not put X-Insight in the synchronous model request path for PRD1.
- Do not store raw prompt/response by default.
- Do not start with a graph canvas or generic dashboard.
- Do not make OTel/OpenInference the core schema owner.
- Do not require OPA/Rego or Cedar in PRD1.
- Do not introduce ClickHouse before real data volume and query pressure justify it.
- Do not claim full agent governance from gateway-only evidence.
- Do not claim downstream side-effect visibility without SDK/MCP/business-system evidence.

## Readiness By Layer

| Layer | Readiness | Assessment |
| --- | --- | --- |
| Product thesis | Ready for PRD1 candidate | Evidence-plane wedge is coherent. Final naming can wait. |
| User/buyer framing | Ready for PRD1 candidate | Daily operator first, CISO/compliance as stakeholder is the right starting balance. |
| Evidence model | Ready for M0 | Needs exact schema files, but conceptual model is stable enough. |
| Storage | Ready for M0 | Postgres-first is the right starting choice. |
| UniRouter integration | Ready for M1 | DB/API import can start; F3 fields remain parallel UniRouter track. |
| LiteLLM integration | Candidate | Required for release credibility, but should follow M0/M1. |
| UI | Candidate | Information architecture is good enough, but final shape depends on imported data. |
| Policy simulation | Candidate | Direction is right; concrete rules still need definition. |
| Payload governance | Candidate | Default is clear; enterprise controls need deeper design before release. |
| Open source | Candidate | Direction is clear; license and commercial split remain open. |
| Commercial strategy | Open | Needs later packaging/buyer discussion, not M0 blocker. |

## M0/M1 Go Criteria

Start M0/M1 if all are true:

1. The team accepts that first implementation is backend evidence spine only.
2. UniRouter access is available for current table/API import testing.
3. Raw payload storage remains disabled by default.
4. Gate A backend acceptance is the first hard checkpoint.
5. UI, LiteLLM, simulation, evidence pack, and extra adapters stay out of the first package.

Current assessment:

> Go for M0/M1.

## M0/M1 Stop Criteria

Stop or re-scope M0/M1 if any of these happen:

- current UniRouter records cannot be mapped into useful evidence events without hand-authored interpretation;
- idempotency cannot be made deterministic from available source records;
- capture gaps are so broad that the first execution view would be misleading;
- sensitive payload data appears in source records in ways the importer cannot safely suppress or quarantine;
- the importer requires UniRouter runtime changes before any useful Gate A evidence can be produced.

## Final PRD1 Go Criteria

Do not final-lock PRD1 until:

1. Gate A is demonstrated with real or controlled UniRouter evidence.
2. Gate C is demonstrated with LiteLLM callback and spend import.
3. M2 UI renders real evidence and gaps clearly.
4. Payload policy is reviewed as an enterprise on-prem control surface.
5. Two historical simulation rules are concrete and useful.
6. Open-source and commercial boundaries are directionally agreed.

## Recommended Next Work

The next work package should be an implementation kickoff note for M0/M1, not another high-level vision document.

It should define:

- repository/module layout;
- exact package boundaries;
- first migration files;
- schema file tree;
- UniRouter import query discovery checklist;
- fixture categories;
- Gate A backend acceptance evidence;
- what not to build in the first code pass.

After that note, implementation can start without pretending PRD1 is fully final.
