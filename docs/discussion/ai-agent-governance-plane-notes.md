# AI/Agent Governance Plane Discussion Notes

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

Status: discussion notes, not PRD
Created: 2026-07-07

## Purpose

This document captures current judgments, hypotheses, disagreements, and open questions while discussing a possible open-source product around enterprise AI and AI agent governance.

It should not be treated as a PRD yet. A future PRD should only be derived from this document after enough stable product, architecture, and user-problem consensus has formed.

## Current Working Thesis

The product should not become another model gateway. Existing products such as UniRouter, one-api, new-api, LiteLLM, Cloudflare AI Gateway, and similar systems already cover a large part of the model gateway surface: OpenAI-compatible APIs, model routing, provider abstraction, key management, quota, cost tracking, logs, budgets, and operational dashboards.

The more differentiated opportunity is a pluggable AI/Agent Governance Plane that can integrate with existing model gateways, agent runtimes, application SDKs, RAG systems, MCP/tool brokers, and internal enterprise systems.

The core product question is:

> How can an enterprise see, identify, control, audit, and evolve all AI and AI agent behavior across models, tools, data, applications, and business workflows?

## Source Reviews

- [X-IAM Source Review for X-Insight](./x-iam-source-review-for-x-insight.md): review notes from `PRIVATE_HOME/Desktop/projects/X-IAM`, including consultant discussion rounds, verified research lists, policy/identity/observability/MCP/ontology references, and reliability caveats.
- [Market Landscape and Architecture Analysis](./market-landscape-architecture-analysis.md): current adjacent-product analysis across LLM observability, AI gateways, OpenTelemetry/OpenInference, agent IAM, MCP/agent gateways, and AI security/guardrails.
- [X-Insight Decision Backlog](./decision-backlog.md): open decisions around technology stack, frontend/backend architecture, storage, protocols, open-source/commercial boundary, enforcement, privacy, and first integrations.
- [X-Insight Vision Readiness Assessment](./vision-readiness-assessment.md): assessment of whether discussion is ready to converge into a concrete Vision v0.1 and phased landing path, plus remaining PRD-gate questions.
- [X-Insight External Advisor Brief](./external-advisor-brief.md): forwardable project background, context, hypotheses, and specific questions for external advisors to critique.
- [External Feedback Round 1 Analysis](./external-feedback-round1-analysis.md): synthesis of 9 external advisor responses, including consensus, disputed recommendations, claims requiring verification, and product-direction changes.
- [UniRouter Evidence Discovery Plan](./unirouter-evidence-discovery-plan.md): concrete next-discussion plan that maps current UniRouter trace/event/audit/privacy capabilities to X-Insight's evidence model and identifies capture gaps.
- [Evidence Contract v0.1 Working Draft](./evidence-contract-v0.1-working-draft.md): candidate object/field contract plus UniRouter, one-api, new-api, and LiteLLM compatibility review.
- [Adapter Integration Strategy](./adapter-integration-strategy.md): proposed integration modes and sequencing for UniRouter, LiteLLM, new-api, one-api, OTel, SDK, MCP, and business-system instrumentation.
- [Gateway Adapter Mapping Matrix v0.1](./gateway-adapter-mapping-matrix-v0.1.md): field and evidence-fidelity matrix for UniRouter, LiteLLM, new-api, one-api, and OTel/OpenInference before PRD1.
- [PRD1 Dual Demo Definition](./prd1-dual-demo-definition.md): pre-PRD definition of the UniRouter deep evidence demo and LiteLLM ecosystem compatibility demo.
- [PRD1 Minimal Data Model](./prd1-minimal-data-model.md): pre-PRD data model for source systems, adapters, evidence events, payload policy, capture gaps, and policy simulation.
- [PRD1 Candidate Scope](./prd1-candidate-scope.md): candidate implementation boundary for the first runnable X-Insight release, still not a final PRD.
- [PRD1 Draft v0.1 Candidate](./prd1-draft-v0.1-candidate.md): consolidated candidate PRD draft that pulls product thesis, scope lock, release gates, requirements, architecture, integration strategy, risk controls, and first implementation package into one reviewable document.
- [PRD1 Readiness Review v0.1](./prd1-readiness-review-v0.1.md): Locked / Candidate / Open review of the PRD1 draft, including M0/M1 go criteria, final PRD1 lock criteria, and stop criteria.
- [PRD1 Engineering Milestones Candidate](./prd1-engineering-milestones-candidate.md): candidate Gate A-E engineering path, ticket families, UniRouter coordination track, and release evidence sequence.
- [PRD1 M0 Evidence Envelope Candidate](./prd1-m0-evidence-envelope-candidate.md): narrowed M0 event envelope, field groups, source refs, idempotency/cursor rules, adapter manifest shape, and UniRouter/LiteLLM mappings.
- [PRD1 M0 Schema And Indexes Candidate](./prd1-m0-schema-indexes-candidate.md): M0 JSON Schema boundaries, enum registry, Postgres indexes, idempotency conflict handling, and sensitive-key detector baseline.
- [PRD1 M0/M1 First Implementation Package Candidate](./prd1-m0-m1-first-implementation-package-candidate.md): first code package boundary for schemas, migrations, validator, fixtures, UniRouter current-table importer, read APIs, and Gate A backend acceptance.
- [PRD1 M0/M1 Implementation Kickoff](./prd1-m0-m1-implementation-kickoff.md): concrete first-code-start plan covering repo shape, schema and migration file trees, UniRouter import discovery, M1 mapping rules, minimal APIs, Gate A evidence, and non-negotiable exclusions.
- [PRD1 M2 Timeline UI Information Architecture Candidate](./prd1-m2-timeline-ui-information-architecture-candidate.md): M2 execution search, timeline, node detail, capture-gap panel, payload state, and UI acceptance criteria.
- [PRD1 M2 Wireframe And Component Candidate](./prd1-m2-wireframe-component-candidate.md): M2 terminology, execution search/detail component breakdown, import diagnostics, empty/error states, and build order.
- [UniRouter / X-Insight Integration Boundary](./unirouter-xinsight-integration-boundary.md): verified UniRouter integration baseline, required UniRouter updates, and ownership boundaries between UniRouter and X-Insight.
- [X-IAM Reference Copies](../reference/x-iam/README.md): selected X-IAM planning and research materials copied into the X-Insight project with provenance and caveats.
- [Phase 2 Competitive Alignment And Recommendation Rationale](./phase-2-competitive-alignment-and-recommendation-rationale-2026-07-13.md): official-source comparison of eight investigation/observability products, explicit rationale and trade-offs for recommendations 2–8, and an MVP/advanced/long-tail matrix.
- [UniRouter / X-IAM / X-Insight Scalability Anti-Pattern Audit](./unirouter-x-iam-x-insight-scalability-antipattern-audit-2026-07-13.md): four-quadrant audit of the verified current boundary and conditional 100x/Agent-fanout failure modes; X-IAM is treated as research/candidate control plane, not current runtime fact.
- [Phase 2 Batch 0 Scope And Decision Register](../planning/phase-2-batch-0-scope-and-decision-register-2026-07-13.md): D01-D31 accepted product/boundary defaults, P2-0 foundation status, still-required implementation evidence, and the joint UniRouter feed boundary.
- [PRD2-M1 UniRouter Execution Investigation Field Proof](../planning/prd2-m1-unirouter-field-validation-implementation-plan-2026-07-13.md): proposed gate-based initial field-version implementation plan with all accepted investigation capabilities, upgrade and Chinese manual delivery.
- [Phase 2 X-IAM, Tool Gateway And Cache Authority Boundary](../architecture/phase-2-xiam-tool-gateway-cache-authority-boundary-candidate-2026-07-13.md): accepted boundary separation of identity/policy control, model/tool enforcement, live-entitlement checks and cache invalidation; exact parameters and X-IAM/Tool Gateway runtime remain unimplemented.
- [Phase 2 X-Insight Innovation And Golden Boundary Matrix](./phase-2-xinsight-innovation-and-golden-boundary-2026-07-13.md): four-quadrant analysis, hard latency/compute/lifecycle rules, semantic coordination graph, shadow evaluation feedback and additional evidence-to-control feature candidates.

## Important Judgments So Far

### 1. Model gateways are becoming infrastructure commodities

Model gateway capabilities are necessary but increasingly insufficient as product differentiation.

Common gateway capabilities include:

- Unified API surface
- Multi-provider routing
- API key and token management
- Cost and token accounting
- Rate limits and budgets
- Provider fallback and load balancing
- Basic request logs and metrics
- Basic guardrail hooks

These are useful foundations, but competing directly here risks becoming a feature race against established open-source gateway projects.

### 2. The product should integrate with gateways instead of replacing them

The product should be able to sit above, beside, or behind existing gateways.

Potential integration modes:

- Gateway plugin or callback
- Reverse proxy sidecar
- SDK instrumentation
- OpenTelemetry-compatible exporter/collector
- Webhook/event ingestion
- Agent runtime adapter
- MCP/tool gateway adapter

This keeps adoption lightweight and makes the product useful to teams already using UniRouter, one-api, new-api, LiteLLM, or custom internal gateways.

### 3. Identity is more important than request logging

Traditional gateways usually identify API keys, users, teams, channels, or models.

Enterprise AI governance needs richer identity:

- Human actor
- Application actor
- Agent actor
- Sub-agent actor
- Workflow/run identity
- Business project or workspace
- Data domain
- Tool/capability identity
- Delegation chain
- Approval chain

Without this identity layer, observability data becomes difficult to attribute, govern, or audit.

### 4. Business intent should become a first-class governance dimension

Enterprise AI usage is usually not generic chat. It normally exists inside a business intent, such as customer support, sales proposal generation, compliance review, incident response, financial analysis, procurement, HR, legal review, or internal operations.

Therefore, policy should not only ask:

> Can this user call this model?

It should ask:

> Under this business intent, can this actor access this data, use this model, call this tool, generate this output, or execute this action?

This is likely one of the product's strongest possible differentiators.

### 5. The product should not try to rebuild a full enterprise ontology

Palantir-style ontology is powerful because it connects enterprise objects, relationships, actions, and operational write-back. But a full enterprise ontology or FDE-style implementation can easily become heavy, consultant-driven, and slow to adapt.

For this product, a more realistic direction is a lightweight AI Execution Ontology.

Candidate primitives:

- Actor: human, app, agent, sub-agent
- Intent: task purpose and business context
- Run: execution session or workflow instance
- Capability: model, tool, data source, API, action
- Boundary: permission, budget, data policy, region, approval requirement
- Evidence: prompt, retrieved document, tool call, tool result, output, policy decision
- Outcome: success, failure, blocked, degraded, approved, escalated, written back

This is not ERP, not a full business ontology, and not a direct Palantir clone. It is a minimal operational semantic layer for governing AI execution.

### 6. Policy should govern actions, not just models

The highest-risk enterprise AI behavior often happens outside the model call.

Examples:

- Reading sensitive customer, employee, legal, or financial data
- Running internal APIs
- Querying databases
- Creating or modifying records
- Sending email or messages
- Opening tickets
- Changing configuration
- Calling MCP tools
- Exporting information to external systems

Policy should therefore focus on action autonomy levels, for example:

- observe: inspect only
- draft: generate draft without side effects
- recommend: provide advice
- prepare: prepare action but require confirmation
- execute_internal: perform internal write action
- execute_external: send externally or affect third parties
- delegate: authorize another agent or workflow to continue

### 7. Evidence chain and replay may become a defensible capability

Basic observability answers latency, token, cost, and error questions.

Enterprise AI governance needs stronger questions:

- Why did the agent reach this result?
- Which data did it see?
- Which tools did it call?
- Which policy decision allowed or denied each step?
- Which human or system approved a risky step?
- Which output came from retrieved evidence versus model inference?
- Could the same run be replayed or simulated under a newer policy?

This creates a combined audit ledger, black-box recorder, and runtime permission system for AI/agent behavior.

## Tentative Evolution Path

### Phase A: Gateway Overlay

Integrate with existing gateways and collect normalized AI request events, usage, cost, model metadata, user/key/team mapping, and basic traces.

### Phase B: Agent Run Ledger

Move from single request logs to full agent run traces: LLM calls, RAG retrieval, tool calls, MCP calls, human approvals, outputs, and write-back events.

### Phase C: Intent-Aware Policy

Evaluate policy using actor, intent, data, tool, action, risk level, environment, and approval state.

### Phase D: Enterprise AI Control Plane

Provide a cross-system control plane for AI assets, identities, costs, permissions, policies, risk, evidence, and incident response.

### Phase E: AI-Native Operating Layer

Potentially evolve into a lightweight operating layer for enterprise AI behavior, allowing existing systems to be safely called, observed, authorized, and adapted by agents.

## Current Non-Goals

These are not permanent decisions, but they prevent premature scope expansion during discussion:

- Do not write a PRD yet.
- Do not position the product as a replacement for UniRouter, LiteLLM, one-api, or new-api.
- Do not start from a full enterprise ontology.
- Do not assume ERP-style customization is the right implementation model.
- Do not assume observability alone is enough.
- Do not define business/legal/commercial feasibility yet unless explicitly needed.

## Open Questions

### Product Positioning

- Is the product best described as AI Governance Plane, Agent Control Plane, AI Execution Ledger, Intent-Aware Policy Engine, or something else?
- Should the first user be platform engineering, security/compliance, AI application teams, or business operations owners?
- Is the sharpest wedge observability, identity, policy, audit, or agent runtime safety?

### Architecture

- What is the minimum event schema that can support identity, intent, flow, policy, and evidence without becoming too heavy?
- Should OpenTelemetry GenAI conventions be treated as the base telemetry layer, with product-specific governance extensions?
- How should the system integrate with gateways that cannot be modified?
- Where should runtime enforcement happen: gateway, SDK, sidecar, agent runtime, MCP broker, or policy decision service?

### Ontology and Semantics

- What is the minimum useful AI Execution Ontology?
- How much business context must be explicit for policy to work?
- Can business intent be inferred from workflow context, or must it be declared by the caller?
- How can the model avoid becoming a slow FDE/consulting implementation project?

### Policy

- What policy language or policy model is appropriate?
- Should policies be written by developers, security teams, business owners, or generated from templates?
- How should human approval, exception handling, and temporary access be represented?
- How should policy decisions be made explainable and auditable?

### Open Source Strategy

- Which part should be open-source core?
- Which integrations should be built first?
- How can the project avoid being perceived as another gateway?
- What reference integrations would make the positioning obvious?

## Candidate One-Sentence Positioning

Working draft:

> A pluggable AI/Agent Governance Plane that gives enterprises identity, intent, policy, observability, and evidence replay across model gateways, agent runtimes, tools, data sources, and business workflows.

This sentence is not final.

## Discussion Log

Historical/superseded notice: entries below preserve what was believed or
recommended at the recorded date. Current implementation and Phase 1 closure
are governed by [Runtime Contract Capability Status](../architecture/runtime-contract-capability-status.md)
and [Phase 1 Closeout Gate](../planning/phase-1-closeout-gate.md). In particular,
the older recommendation to run one more Gate A snapshot was completed by the
Batch 1W cycle and is not current pending work.

### 2026-07-07

Initial judgments captured:

- Gateway-only differentiation is weak.
- Integration-first strategy is important because enterprises may already use UniRouter, one-api, new-api, LiteLLM, or custom gateways.
- The strongest product direction may be identity/intent-first governance rather than observability-first dashboards.
- A lightweight AI Execution Ontology may be more practical than full enterprise ontology or ERP-style customization.
- Runtime policy should govern actions and autonomy levels, not only model access.
- Evidence chain, audit ledger, and replay/simulation may become core defensible capabilities.

X-IAM source review added:

- Reviewed the X-IAM discussion/research folder and recorded transferable source notes separately.
- Strongest imported judgment: X-Insight should not inherit the old "Agent IAM" market narrative directly. Identity is necessary infrastructure, but the sharper product edge is AI/Agent execution governance, especially cross-system data-access governance and evidence replay.
- Important caution: X-IAM contained multiple AI-consultant source hallucinations and unverified statistics, so future claims should preserve a source reliability tier.

Narrative, first integration, moat, and market-scan candidate judgments:

- Narrative should probably evolve in layers rather than pick one static category too early.
- The near-term open-source wedge can be "AI/Agent Flight Recorder" because it is concrete, easy to demo, and maps to gateway/runtime integration work.
- The enterprise-value wedge should move toward "AI Data Access Governance" because the high-value risk is not model usage itself, but AI/agent access to data, tools, APIs, and side-effectful business actions.
- The long-term category hypothesis remains "AI Execution Governance Plane": identity, intent, flow, policy, evidence, and replay across gateways, agent runtimes, MCP/tool calls, data systems, and business workflows. This phrase is internal framing, not a product name to repeat for its own sake.
- UniRouter is the right first reference integration because it is under our control and can expose deeper hooks, but X-Insight must be architected as a gateway-neutral collector, schema, policy, and adapter system. Otherwise it will be perceived as a UniRouter plugin.
- After UniRouter, the next integration should likely be a generic OpenAI-compatible event adapter and then a mainstream external gateway/runtime path such as LiteLLM, OpenTelemetry/OpenInference, or MCP/tool-call adapters.
- The moat is unlikely to be source code or a basic dashboard. More plausible moats are event/evidence schema adoption, connector quality, policy/evidence templates, deployment trust, accumulated customer integration context, enterprise-grade privacy controls, compatibility testing, and an open-source ecosystem around adapters and policy packs.
- Current market check suggests many adjacent products exist: LLM observability/evaluation platforms, AI gateways, Agent IAM, MCP gateways/security scanners, and guardrail tools. The open gap is not "no one is doing this"; the gap is a coherent gateway-neutral execution governance layer that connects identity, intent, tool/data actions, policy decisions, and replayable evidence.

Future optionality candidate judgments:

- Do not try to predict which agent framework, MCP variant, identity pattern, policy language, or model API will win. Preserve optionality through a stable execution event model and adapter boundaries.
- The most important future-proofing decision is not a feature; it is preserving execution context: actor, owner, intent, run lineage, capability, action, resource, policy decision, evidence reference, outcome, side effect, and delegation chain.
- Capture modes must be designed from the start: metadata-only, hash-only, redacted, sampled full content, full content for selected systems, and customer-managed evidence storage. This protects both privacy and future analytics.
- SDKs and adapters should be thin event emitters and optional enforcement hooks, not the core product. X-Insight should be able to ingest from gateways, SDKs, OpenTelemetry/OpenInference, MCP/tool brokers, internal API gateways, IAM systems, approval systems, and security detectors.
- Policy should be represented as a decision interface before choosing one policy language. OPA/Rego, Cedar, custom webhooks, IAM/FGA systems, guardrail verdicts, and human approvals should all be possible decision providers.
- Evidence references should be content-addressable and replay-oriented where possible. Future value may come from policy replay, simulation, incident reconstruction, compliance evidence, and workflow/process discovery.
- The product should avoid hard-coding today's model API shapes. Events should describe capabilities and actions, not only chat completions.
- The system should treat business intent as declared first, inferred second, and corrected over time. LLM inference can help classify intent, but should not become the authority for high-risk policy decisions.
- Future agent ecosystems may become browser/computer-use, multi-agent, local-device, edge, or autonomous workflow heavy. The event model should not assume all actions are HTTP requests or MCP calls.
- The safest long-term architecture is a control-plane/data-plane separation: X-Insight owns schema, policy decisions, evidence ledger, replay, and governance UI; enforcement can happen in gateways, SDKs, sidecars, MCP brokers, internal API proxies, or approval workflows.

Business-first discussion principle:

- Future discussion should not be limited to current explicit customer demand. X-Insight is a vision-led product discussion: identify a structural problem that enterprises may not yet name clearly, then narrow it into phased PRDs.
- User pain should include latent and inevitable pain, not only today's stated procurement requirement.
- Technical decisions should be translated into business consequences. For example, "event schema" means whether the customer can later prove what happened; "capture mode" means whether the customer can use the product without leaking sensitive data; "adapter boundary" means whether the product can enter existing enterprise systems without forcing replacement.
- The main discussion loop should be: vision thesis -> why this problem will become unavoidable -> what hidden risk/current blind spot customers have not yet named -> what product capability makes the problem visible -> what phased PRD proves and expands the category -> what technical choice preserves future expansion.
- The product should avoid both extremes: a non-technical governance story with no enforceable data foundation, and a technical observability system with no urgent business reason to buy.
- The goal is not to manufacture demand artificially. The goal is to reveal an already-emerging governance gap caused by AI/Agent adoption and provide a practical path for enterprises to recognize, measure, and control it.

External advisor round 1 synthesis:

- The long-term category hypothesis can remain "AI Execution Governance Plane", but PRD1 should not use that as the primary market/product framing.
- PRD1 should converge around "Agent Execution Flight Recorder", "AI Execution Evidence Ledger", or "Agent Runtime Audit Ledger".
- A human-readable execution map is necessary but insufficient. The stronger first aha should combine execution map, evidence pack, risk query, policy simulation, and explicit capture-gap visibility.
- Gateway-only capture is a serious architectural risk. UniRouter remains valuable as a deep internal reference integration, but public positioning should include OpenTelemetry/OpenInference and likely LiteLLM/OpenAI-compatible ingestion from the beginning.
- Evidence/privacy defaults must be strict: metadata/hash/HMAC/reference by default, raw prompt/response/tool-result/database content only by explicit opt-in or customer-managed evidence storage.
- Observed facts, declared context, inferred semantics, and human corrections must be separate data layers. LLM-inferred intent should not be treated as fact.
- PRD1 should be observe-first. Shadow policy simulation and async alerts are plausible early additions, but broad inline enforcement should be later and opt-in.
- Capture gaps must be explicit. Unknown actor/resource/intent/side effect should not be hidden as nulls or silently presented as complete evidence.
- License strategy remains unresolved. There is broad support for Apache 2.0 on schema, SDK, adapters, and protocol surfaces; core server/UI licensing needs further decision.

UniRouter / X-Insight dual-flywheel clarification:

- UniRouter is not only an integration target. Because it is an internally built MaaS/model-gateway product with controllable environment and data, it can act as the fastest product-construction flywheel for X-Insight.
- If X-Insight needs execution context that is hard or awkward to collect inside X-Insight itself, UniRouter can be enhanced to emit richer upstream context, route metadata, policy decisions, trace IDs, team/user/workspace context, provider/model/fallback data, and selected governance events.
- This does not invalidate the external advisor warning. The warning is about public positioning and ecosystem credibility, not about internal construction speed.
- Updated distinction: UniRouter-first for internal build and proof; OpenTelemetry/OpenInference/LiteLLM-compatible from early public positioning.
- A useful open-source release is not "day-one code published." It should be a runnable product with a real reference integration, sample data, and a demo path. Early internal build speed can matter more than premature public repo visibility.
- The risk to manage: do not let UniRouter-specific assumptions leak into X-Insight's native governance event envelope. UniRouter should be an emitter/reference implementation, not the schema owner.

Terminology and convergence corrections:

- Avoid "North Star" as a default term. It is strategy-consulting jargon and can become SEO/GEO-influenced language if repeated without concrete product meaning.
- Use "long-term category hypothesis" or "ultimate product boundary" instead. The real question is not the label, but whether the product can eventually govern AI/Agent execution across models, tools, data, APIs, approvals, and side effects.
- Do not lock the first demo scenario too early. A narrow demo can overfit the product before the foundation is understood.
- Better next step: build a UniRouter-based evidence discovery loop first, then choose the first public demo based on what the system can actually see, what it cannot see, and which capture gaps are strategically important.
- For a 2B/on-premises product, evidence storage policy should be enterprise-controlled. X-Insight should provide safe defaults and configurable capture modes, not make irreversible data-retention decisions on behalf of customers.
- Public open-source release should wait until the product is runnable and compatible enough to prove its positioning, likely with UniRouter plus one-api/new-api/LiteLLM/OpenAI-compatible or OpenTelemetry/OpenInference paths. Publishing code before the product can run is not a meaningful ecosystem event.

Adapter and PRD1 boundary consensus as of 2026-07-08:

- Adapter support should be described by fidelity level, not as a binary "supported / unsupported" claim.
- Completed concrete artifact: `Gateway Adapter Mapping Matrix v0.1`.
- Completed concrete artifact: `PRD1 Dual Demo Definition`.
- Completed concrete artifact: `PRD1 Minimal Data Model`.
- Completed concrete artifact: `PRD1 Candidate Scope`.
- Completed concrete artifact: `PRD1 Draft v0.1 Candidate`.
- Completed concrete artifact: `PRD1 Readiness Review v0.1`.
- Completed concrete artifact: `PRD1 Engineering Milestones Candidate`.
- Completed concrete artifact: `PRD1 M0 Evidence Envelope Candidate`.
- Completed concrete artifact: `PRD1 M0 Schema And Indexes Candidate`.
- Completed concrete artifact: `PRD1 M0/M1 First Implementation Package Candidate`.
- Completed concrete artifact: `PRD1 M0/M1 Implementation Kickoff`.
- Completed concrete artifact: `PRD1 M2 Timeline UI Information Architecture Candidate`.
- Completed concrete artifact: `PRD1 M2 Wireframe And Component Candidate`.
- Completed concrete artifact: `UniRouter / X-Insight Integration Boundary`.
- `PRD1 Candidate Scope` now includes `Scope Lock Candidate v0.1`: Must / Should / Later / Won't boundaries plus Gate A/B/C/D/E release gates.
- Completed pre-PRD artifact sequence: `Gateway Adapter Mapping Matrix v0.1`, `PRD1 Dual Demo Definition`, then `PRD1 Minimal Data Model`.
- UniRouter should be the first deep native integration for controlled product construction.
- LiteLLM should be supported early for public ecosystem credibility, but X-Insight should not require customers to deploy LiteLLM.
- PRD1 LiteLLM support should mean a real callback endpoint plus spend-log import, not only a demo callback stub.
- PRD1's first integration mode with UniRouter should be X-Insight-side DB/API import. UniRouter native outbox/exporter remains PRD1 stretch or PRD1.5 unless the first public demo requires real-time push.
- PRD1 should be gated by evidence loops rather than a single preselected story: UniRouter internal loop, UniRouter high-fidelity readiness, LiteLLM external credibility loop, governance loop, and runnable release hygiene.
- PRD1 engineering should start with M0 evidence contract/storage spine and M1 UniRouter no-change evidence loop. A polished frontend, native UniRouter exporter, OTel backend, and full policy engine should not lead the implementation.
- UniRouter structural fields should run as a parallel F3 readiness track after real import exposes field gaps; they should not block Gate A.
- The decision backlog now distinguishes PRD1 planning decisions that are narrowed enough for implementation planning from still-open but non-blocking questions such as license, commercial packaging, one-api/new-api order, and full OTel/OpenInference ingestion.
- M0 evidence envelope should use a stable top-level event envelope plus disciplined JSON field groups for correlation, identity, capability, decision, usage, outcome, payload policy, field provenance, and raw source excerpt. Source refs, idempotency, payload capture mode, and field-group provenance are PRD1 must-haves.
- M0 should denormalize `request_id`, `trace_id`, `span_id`, `org_ref`, `project_ref`, and `payload_capture_mode` as top-level query projections while keeping canonical values in JSON field groups. Canonical provenance lives on `evidence_events`; timeline nodes are projections. Invalid or risky events go to minimal quarantine.
- M0 schema/index decisions are now narrowed: seed enum registry, JSON Schema file boundaries, initial Postgres indexes, idempotency conflict handling, and sensitive-key detector baseline are captured. `event_hash` should be generated by X-Insight from canonical normalized content.
- First implementation package should include M0 schemas/migrations/validator/fixtures plus M1 UniRouter current-table importer and minimal read APIs. It should exclude React UI, LiteLLM adapter, policy simulation, evidence pack, payload object storage, UniRouter outbox/exporter, OTel backend, and one-api/new-api adapters.
- First implementation defaults are narrowed: boring Go libraries, simple numbered SQL migrations, SHA-256 over deterministic canonical JSON, env-based local config, read-only UniRouter DB credentials where possible, and YAML/JSON fixtures with expected outcomes.
- M2 first UI should be an operator investigation console: execution search, execution detail, derived timeline, node detail, capture-gap panel, payload state display, source/fidelity labels. Do not start with a dashboard or graph canvas.
- M2 terminology/component candidate recommends `Execution` as the product object, with request ID as the first lookup key. User accepted this direction on 2026-07-08; keep changeable only before final PRD lock. First build order: execution search table, execution detail header, derived timeline, node detail, gap panel, payload state, import diagnostics.
- PRD1 should stay observe-first: evidence, timeline, capture gap, and policy simulation.
- Readiness review conclusion: PRD1 is ready to start M0/M1 implementation planning, but not ready for final release-scope lock.
- M0/M1 implementation kickoff now defines the first-code-start boundary: backend evidence spine first, no UI/LiteLLM/policy simulation/evidence pack/outbox/OTel/extra adapters in the first package, UniRouter import discovery before importer code, and Gate A evidence saved under `docs/evidence/prd1/gate-a/`.
- First M0 foundation code checkpoint is now present: Go module skeleton, M0 schemas, migration SQL files, fixture runner, JSON Schema validation, canonical event hashing, sensitive raw payload detection, idempotency duplicate/conflict behavior, and migration runner entrypoint. Evidence note: [M0 Foundation Check](../evidence/prd1/gate-a/m0-foundation-check-2026-07-08.md).
- PRD1's first daily operators should be AI platform owners and enterprise IT, with CISO/security teams as key stakeholders rather than the first operator.
- The first value proposition should emphasize auditable execution evidence chains and explicit visibility boundaries, not generic AI observability.
- PRD1 policy simulation should be lightweight and historical-evidence-based, not a full policy-authoring system.
- Event-store architecture should be pre-allocated; Postgres can be the starting point, with ClickHouse or similar analytics storage introduced when data volume and query shape justify it.
- Open-source boundaries should favor open schemas, adapter SDKs, reference adapters, and examples; commercial boundaries can later include enterprise RBAC, audit-on-read, retention controls, advanced policy simulation, and premium connectors.
- First demo should be dual-track: UniRouter deep demo for high-fidelity evidence, plus LiteLLM compatibility demo for public ecosystem credibility.
- Broad synchronous enforcement, default prompt/response storage, and ontology-first product design should remain out of PRD1.
- Prompt/response should be configurable for on-prem enterprise use, but it must be modeled as governed payload policy rather than a simple boolean switch: capture mode, scope, retention, encryption, RBAC, audit-on-read, redaction, export/delete, and legal hold must be explicit.
- UniRouter currently supports metadata-only request trace events, call logs, audit logs, and per-request trace API; X-Insight can start with DB/API import, but F3 fidelity requires UniRouter structural fields and eventually a non-blocking outbox/exporter.
- Minimal read APIs are now the right Gate A next step after persistence: `/api/v1/executions/{request_id}`, `/events`, and `/gaps` should prove imported evidence can be consumed before UI work starts. Evidence note: [Minimal Read APIs](../evidence/prd1/gate-a/minimal-read-apis-2026-07-09.md).
- UniRouter boundary decision: UniRouter owns source facts and runtime behavior; X-Insight owns normalized evidence, capture-gap semantics, persistence, read APIs, UI, policy simulation, and adapter contracts. `candidate_capture_gaps` must remain candidate-only evidence, not definitive root cause. Working note: [UniRouter Boundary And Adapter Compatibility](./unirouter-boundary-and-adapter-compatibility-2026-07-09.md).
- UniRouter PRD5 Batch 1P model-permission denial should import as a terminal auth decision, not a generic external event: preserve `source_event_type=auth.model_not_allowed`, normalize to `auth.model.rejected`, keep `decision_kind=auth`, and expose `permission_denied` through capture gaps. Evidence note: [UniRouter Auth Decision Compatibility](../evidence/prd1/gate-a/unirouter-auth-decision-compat-2026-07-10.md).
- Gate A controlled snapshot intake is now operationalized: approved request IDs are the explicit safe export set, and `xinsight-importer unirouter gate-a-check` performs discovery, import validation, optional persistence, and request readback. Runbook: [Controlled Snapshot Intake Runbook](../evidence/prd1/gate-a/controlled-snapshot-intake-runbook-2026-07-10.md).
- Request-ID approval now needs two modes: exact-ID approval for existing evidence, and scenario-run authorization for newly generated controlled requests. An opaque request ID alone does not explain data scope to a human approver; controlled-run IDs should be sealed from an authorized run marker without enabling broad discovery.
- Data-exchange audit ownership follows custody: UniRouter audits approval plus preview/download/delivery/deletion of artifacts it holds; X-Insight audits import plus read/export/payload access/retention/deletion of evidence it holds.
- Gate A reconciliation should use a source-created `exchange_id` plus X-Insight-computed artifact SHA-256. `import_job_id` and deep links should wait for a durable import-job service rather than being invented by the synchronous CLI.
- X-Insight now emits `xinsight.import-receipt.v1` from `gate-a-check`, with optional source exchange/approval-manifest identity, artifact hash, validation/persistence/readback counts, and machine-readable warnings. Working decision: [Gate A Exchange Receipt And Approval Boundary](./gate-a-exchange-receipt-and-approval-boundary-2026-07-10.md). Evidence: [Gate A Import Receipt Contract](../evidence/prd1/gate-a/import-receipt-contract-2026-07-10.md).
- UniRouter Batch 1T exchange-envelope implementation is compatible with Gate A and should not be delayed by more pre-runtime fields. The first real artifact is now more valuable than further contract expansion.
- Assurance must remain explicit: exact-byte manifest and artifact hashes prove content integrity; `source_instance` is currently a declared/operator-attested label rather than independently verified instance identity; the shared authorization worksheet is process evidence rather than a machine-linked authorization record; the file-based receipt is not a signed deployment attestation.
- Production evolution should later add durable authorization identity, source-instance assurance, authenticated or signed delivery/receipts, and an append-only exchange ledger. These are not blockers for the controlled dev/test run. Working decision: [Gate A Provenance And Assurance Boundary](./gate-a-provenance-assurance-boundary-2026-07-10.md).
- Delivery workflow now uses one execution owner for implementation, tests, GitHub integration, and authorized non-production work. Canary/Sentinel/Atlas become optional independent review profiles rather than mandatory serial handoffs. CI, risk states, production/data authorization, evidence, rollback, and the distinction between self-test and independent review remain mandatory boundaries. Protocol: [X-Insight Single Execution Owner Protocol](../agent-workflows/AGENT-PROTOCOL.md).
- The first real UniRouter snapshot exposed a consumer-side sensitive-detector false positive: substring matching treated `completion_tokens` as raw completion content. X-Insight now distinguishes governed count/reference metadata from raw payload and credential keys while retaining default quarantine for prompt/completion/messages/request/response bodies and secrets.
- Real external snapshot loop result: 10 normalized events accepted, 0 rejected/quarantined, first Postgres persistence inserted 10, second persistence returned 10 duplicate/no-op, and readback returned 10 events plus 3 explicit gaps. Receipt status is `persisted`; artifact and manifest hashes reconcile.
- Gate A is externally verified for the immutable diagnostic snapshot in local non-production X-Insight, but not target/ECS verified. The denied request remains two `not_available_from_source` gaps, and the successful request retains one `provider_not_returned` gap. One more controlled snapshot after the UniRouter Batch 1W target fix is recommended before read-only view or exporter/outbox design. Evidence: [Real UniRouter Gate A Snapshot Import](../evidence/prd1/gate-a/real-unirouter-snapshot-import-2026-07-10.md).
