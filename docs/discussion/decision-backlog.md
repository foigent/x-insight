# X-Insight Decision Backlog

Status: discussion backlog, not PRD
Created: 2026-07-07

## Purpose

This document records the major decisions that still need deeper discussion before X-Insight becomes a PRD or implementation plan.

The goal is to separate:

- decisions that must be made early because they create long-term architecture gravity
- decisions that can remain pluggable or deferred
- decisions that should be validated through the first reference integration

## 2026-07-13 Phase 2 Routing Note

The current accepted priority and deployment posture are recorded in
[Phase 2 Product Priority](../planning/phase-2-product-priority.md). The earlier
stack, UI and architecture sections in this backlog remain candidate discussion
material; they are not automatically accepted by the Phase 2 priority decision.

The active decision register is
[Phase 2 Batch 0 Scope And Decision Register](../planning/phase-2-batch-0-scope-and-decision-register-2026-07-13.md),
supported by the
[competitive-alignment rationale](./phase-2-competitive-alignment-and-recommendation-rationale-2026-07-13.md)
and the
[three-product scalability audit](./unirouter-x-iam-x-insight-scalability-antipattern-audit-2026-07-13.md).
The User accepted B0-D01 through B0-D31 on 2026-07-13 as product/architecture
boundary defaults; acceptance alone is not implementation evidence. P2-0
through P2-3 are merged with Local and CI evidence, as the current execution/
runtime records state. High-impact action classes, spool
behavior, semantic/privacy contracts and Tool Gateway build/reuse/defer choice
remain open parameters.

On 2026-07-14 the User accepted B0-D32: the first customer uses an
owner-supervised release assembled from exact current `main` plus successful
matching push CI. Its one-run Ed25519 private-key file is removed before delivery
and is not retained or uploaded. The User handles no key and the customer
cannot self-upgrade. This closes the
current custody decision without creating a durable commercial signing
authority. A second customer, delegated installer or unattended upgrade is the
explicit revisit trigger.

The follow-on
[X-IAM, Tool Gateway and cache authority boundary](../architecture/phase-2-xiam-tool-gateway-cache-authority-boundary-candidate-2026-07-13.md)
and
[X-Insight innovation and Golden Boundary matrix](./phase-2-xinsight-innovation-and-golden-boundary-2026-07-13.md)
define B0-D13 through B0-D22. Their accepted boundary defaults use an
independent future X-IAM control plane, a sink-local action-owner PEP with Tool
Gateway as one deployment option, local contextual authorization,
observed-epoch cache invalidation and proposal-only X-Insight feedback. None is
implemented runtime scope.

In particular, embedded TypeScript + React packaging and Simplified Chinese
product copy are accepted. The historical completed-execution pull direction
produced a disabled/mock Local consumer, diagnostic measurement and explicit
revision/watermark states. On 2026-07-18 the User deferred the whole live-feed
slice from the first formal version, including automatic `p95 <= 10s`; it is no
longer a pending first-version gate and cannot be implemented piecemeal.
UniRouter push/automatic receipt transport, X-IAM or Tool Gateway runtime,
semantic-content custody, shadow provider calls, automatic route/policy
write-back, OPA/Cedar, ClickHouse, an external queue, durable jobs and HA/DR
remain candidate or deferred. Current implemented/jointly accepted
UniRouter/X-Insight integration remains operator-mediated sidecar-only.

The active field-version implementation plan is
[PRD2-M1 UniRouter Execution Investigation Field Proof](../planning/prd2-m1-unirouter-field-validation-implementation-plan-2026-07-13.md).
The exact plan and implementation-kickoff defaults are accepted; P2-0 through
P2-4 are merged and the exact owner-supervised rc.5 is named-Target verified.
It adds no synchronous cross-project runtime dependency. Joint field date,
customer-IT confirmation of the accepted host profile and one newly approved
bounded UniRouter sidecar artifact remain external inputs. The dedicated
sanitized trial database retention default is accepted as field session plus
seven days before explicit handoff or controlled retirement.

## Decision Tiers

### Tier 1: Decide Early, Hard to Change Later

These decisions shape the product's long-term ability to evolve.

- execution event schema
- identity and delegation envelope
- business intent representation
- evidence reference and capture modes
- control-plane / data-plane boundary
- adapter contract
- policy decision interface
- tenancy and isolation model
- open-source core boundary
- privacy and retention posture

### Tier 2: Keep Pluggable, Avoid Premature Lock-In

These can start with one implementation but should remain replaceable.

- policy engine: OPA/Rego, Cedar, custom webhooks, IAM/FGA adapters
- telemetry protocol: OpenTelemetry, OpenInference, custom ingestion API
- queue/event bus: Postgres queue, Redis Streams, NATS, Kafka
- analytics store: Postgres, ClickHouse, object store, later data lake export
- identity provider integration: Entra, Auth0/Okta, WorkOS, Keycloak, custom OIDC
- security detector integrations: Lakera, Prompt Security, HiddenLayer, custom classifiers
- graph/ontology backend: relational first, graph projection later

### Tier 3: Can Be Deferred Until Product Shape Is Clearer

These matter, but choosing too early may add unnecessary weight.

- full enterprise ontology
- graph database as primary store
- complex multi-region SaaS architecture
- marketplace for adapters
- advanced compliance report generation
- complete SIEM/SOC workflow
- fine-grained pricing and packaging
- public cloud managed service operations

## Product Boundary

Open questions:

- Is X-Insight first an open-source developer/platform tool, or an enterprise governance product with an open-source core?
- Is the first daily user platform engineering, security engineering, AI application teams, or compliance/audit?
- What is the first problem that makes adoption obvious: trace replay, cross-system data access, cost attribution, policy simulation, or tool-call governance?
- Should X-Insight stay strictly gateway-neutral, or should UniRouter get deeper first-party functionality?

Candidate current judgment:

- X-Insight should be built as an open-source governance plane with a strong local/self-hosted core.
- UniRouter can be the first reference integration, but not the center of the architecture.

## Open Source and Commercial Boundary

Open questions:

- Which parts must be open-source for adoption and trust?
- Which parts can be commercial without making the open-source project feel crippled?
- Should the license be permissive, copyleft, open-core, source-available, or BSL-style?
- Is the business model support/enterprise deployment first, managed cloud first, or enterprise connectors/policy packs first?

Candidate open-source core:

- execution event schema
- ingestion API
- collector
- UniRouter adapter
- OpenAI-compatible gateway adapter
- OpenTelemetry/OpenInference import/export mapping
- basic evidence ledger
- local/self-hosted UI for traces, actors, runs, tool/data actions, and decisions
- basic redaction/capture modes
- policy decision interface
- simple webhook/OPA adapter
- SDK/adapter development kit
- compatibility tests for adapters

Candidate commercial or paid-service layer:

- managed cloud
- enterprise deployment support
- HA/multi-region operations
- advanced SSO/RBAC/SCIM
- enterprise connectors to SaaS/business systems
- advanced retention, legal hold, and customer-managed keys
- policy packs for regulated industries
- advanced compliance evidence reports
- SIEM/SOC integrations
- approval workflows and governance workspaces
- premium support and SLA
- private connector certification

Current caution:

- Do not hide the schema, collector, core adapters, or basic evidence UI behind a commercial wall. Those are needed for trust and ecosystem adoption.
- Commercial value should come from operational trust, enterprise integration depth, policy/evidence packs, managed service, and support.

## Technical Stack

Open questions:

- Should the core collector/control plane be Go, Rust, TypeScript, or Python?
- Should the UI be a standalone frontend or shipped with the backend as one deployable package?
- Should X-Insight prioritize low-latency inline enforcement or async observation first?
- How much polyglot complexity can the project carry at 1-2 person scale?

Candidate conservative stack:

- Backend/control plane: Go
- Frontend: TypeScript + React
- Local/development packaging: Docker Compose
- Metadata store: Postgres
- Event analytics store: Postgres first, ClickHouse when event volume justifies it
- Evidence blob store: S3-compatible object storage, with local filesystem/minio for dev
- Cache/job coordination: Redis optional, not required on day one
- Async event bus: start simple, leave adapter for NATS/Kafka later
- AI analysis workers: Python optional later, not core runtime dependency

Reasoning:

- Go fits UniRouter integration, infrastructure-style services, static binaries, and operational simplicity.
- TypeScript/React fits an open-source admin/trace UI and broad contributor pool.
- Python is useful for AI analysis/eval workers, but making it the core control plane can complicate deployment and latency-sensitive enforcement.
- Rust may be attractive for high-performance proxy/data-plane work, but it may be too heavy before the control-plane schema and adoption path are proven.

## Frontend and User Experience

Open questions:

- Is the first UI a dashboard, an investigation console, or a governance workspace?
- What is the first "aha" screen?
- Should the UI optimize for developers debugging traces or security teams investigating policy violations?

Candidate first UI surfaces:

- runs/session timeline
- actor and agent inventory
- model/tool/data/API action timeline
- policy decision log
- blocked/allowed/escalated events
- evidence viewer with capture-mode awareness
- cost and latency overview
- search/filter by actor, intent, capability, resource, policy, and outcome

Current caution:

- Avoid building a generic analytics dashboard first.
- The UI should expose execution, decision, and evidence, not only metrics.

## Data Model and Storage

Open questions:

- What is the minimum normalized relational model?
- Which parts are append-only events?
- Which parts are derived projections?
- Should graph be stored directly or derived later?
- How long is raw evidence retained?

Candidate model split:

- append-only events: execution events, decisions, evidence references
- normalized entities: actors, agents, capabilities, resources, policies, tenants, workspaces
- derived projections: run timelines, actor behavior, resource access graph, cost summaries
- evidence objects: external/blob references with hash, capture mode, retention policy

Candidate storage path:

- Postgres for MVP metadata and moderate event volume
- Object storage for evidence payloads and exports
- ClickHouse for high-volume event analytics when needed
- Graph projection later, not graph database first

## Protocols and Interoperability

Open questions:

- What is the primary ingestion protocol?
- How closely should X-Insight align with OpenTelemetry GenAI and OpenInference?
- How should MCP/A2A/tool-call events map into the generic capability/action/resource model?
- Should X-Insight define its own event envelope?

Candidate protocol stance:

- Native ingestion API: JSON over HTTP first
- Streaming/batch ingestion: support batch events early
- Telemetry interoperability: OpenTelemetry/OpenInference import/export mapping
- Gateway integration: OpenAI-compatible event adapter
- Tool integration: MCP adapter, but generic tool/action event model underneath
- Identity integration: OIDC/JWT claims ingestion, not custom identity protocol first
- Policy integration: decision webhook plus OPA adapter first, Cedar later

Current caution:

- Do not make OpenTelemetry the entire product schema.
- Do not make MCP the entire product schema.
- X-Insight should define a governance event envelope and map external protocols into it.

## Enforcement Model

Open questions:

- Is the first product observe-only, inline enforcement, or policy simulation?
- Which enforcement point is first: UniRouter, SDK, MCP broker, sidecar, or internal API gateway?
- How should sync policy checks avoid unacceptable latency?
- What happens when X-Insight is down?

Candidate staged model:

- Stage 1: observe and record
- Stage 2: async alert and policy simulation
- Stage 3: inline deny/redact/require approval for selected low-latency paths
- Stage 4: multi-enforcement-point policy orchestration

Candidate decisions to model:

- allow
- deny
- redact
- require approval
- degrade
- sandbox
- observe only
- escalate

## Privacy, Security, and Compliance

Open questions:

- What content is safe to store by default?
- Who can view evidence?
- How are prompts, tool results, retrieved documents, and outputs redacted?
- Does X-Insight become a high-value sensitive data store?
- How does a customer prove audit evidence without exposing private content to X-Insight operators?

Candidate default:

- metadata and hashed references by default
- full content opt-in per tenant/system/capability
- redaction pipeline before persistence
- customer-managed evidence storage option
- strict RBAC around evidence viewing
- append-only audit log for policy/evidence access

## Deployment Model

Open questions:

- Self-hosted first or cloud first?
- Single binary, Docker Compose, Kubernetes Helm chart, or all of them?
- Should cloud be multi-tenant from the beginning?

Candidate path:

- local/dev: Docker Compose
- small team self-host: single backend + Postgres + object storage
- enterprise self-host: Kubernetes/Helm
- managed cloud later

Current caution:

- If enterprise trust is central, self-host/on-prem cannot be an afterthought.

## First Reference Integration

Open questions:

- What exactly should UniRouter emit?
- Should UniRouter enforce policy synchronously or only emit events first?
- What is the minimum integration that proves X-Insight is not just a gateway dashboard?

Candidate UniRouter event set:

- model request event
- model response event
- actor/key/team/workspace context
- route/provider/model context
- cost/token/latency
- policy decision if present
- trace/run/session ID propagation
- error/fallback events

Candidate next adapter:

- generic OpenAI-compatible gateway adapter
- OpenTelemetry/OpenInference mapper
- MCP/tool-call adapter

## Key Questions for Next Discussion

The list below is retained as the original discussion checklist. The PRD1 planning update after this section narrows several of these questions for PRD1 implementation planning without closing the longer-term decisions.

1. Should we choose Go + TypeScript as the default stack unless evidence pushes otherwise?
2. Is Postgres-first acceptable, with ClickHouse introduced only when event volume requires it?
3. Should the open-source core include the full event schema, collector, adapters, and basic evidence UI?
4. Should commercial value focus on enterprise operations/connectors/policy packs rather than closed core features?
5. Should X-Insight define a native governance event envelope and map OTel/OpenInference/MCP into it?
6. Should the first enforcement story be observe-only plus policy simulation, rather than synchronous blocking?
7. What is the first "must not lose" event field list?
8. What data should never be stored by default?

## PRD1 Planning Update As Of 2026-07-08

Historical snapshot: the implementation statements in this section describe
2026-07-08 planning and are superseded for current runtime truth by
[Runtime Contract Capability Status](../architecture/runtime-contract-capability-status.md).

The PRD1 candidate scope and engineering milestones have narrowed several decisions enough for implementation planning.

Current PRD1 decisions:

- Backend default: Go.
- Frontend default: TypeScript + React.
- Primary store: Postgres first.
- Analytics sink: ClickHouse later, not PRD1 required.
- Ingestion protocol: native JSON/HTTP collector first, with adapter manifests and source refs.
- Event model: X-Insight native evidence envelope first; OTel/OpenInference are compatibility bridges, not schema owners.
- M0 evidence envelope: stable top-level fields plus JSON field groups for correlation, identity, capability, decision, usage, outcome, payload policy, field provenance, and raw source excerpt.
- M0 must-haves: source refs, idempotency keys, payload capture mode, adapter manifest, field-group provenance, capture gaps.
- M0 query projections: denormalize `request_id`, `trace_id`, `span_id`, `org_ref`, `project_ref`, and `payload_capture_mode` as top-level columns while keeping canonical values in field groups.
- M0 provenance storage: canonical field-group provenance lives on `evidence_events`; timeline nodes may copy it only as derived projection data.
- M0 quarantine: invalid or risky incoming events should enter a minimal quarantine table, not be silently accepted or stored with unsafe raw payload.
- M0 schema/index details: seed enum registry, JSON Schema boundaries, initial Postgres indexes, idempotency conflict handling, and sensitive-key detector baseline are captured in `PRD1 M0 Schema And Indexes Candidate`.
- M0 event hash: X-Insight should generate `event_hash` from canonical normalized event content; source-provided hash is optional and separate.
- M0/M1 first implementation package: start with backend skeleton, schemas, migrations, validator, conformance fixtures, UniRouter current-table importer, and minimal read APIs. Explicitly exclude UI, LiteLLM adapter, policy simulation, evidence pack, payload object storage, UniRouter outbox/exporter, OTel backend, and one-api/new-api adapters.
- M0/M1 implementation defaults: use boring Go libraries, simple numbered SQL migrations, SHA-256 over deterministic canonical JSON, env-based local config, read-only UniRouter DB credentials where possible, and YAML/JSON fixtures with expected outcomes.
- M0/M1 implementation kickoff: first code should create backend evidence spine, schema/migration trees, fixture runner, UniRouter discovery command, UniRouter importer, and minimal read APIs. Gate A evidence should be saved under `docs/evidence/prd1/gate-a/`.
- M2 timeline UI IA: first UI is an operator investigation console with execution search, execution detail, derived timeline, node detail, capture-gap panel, payload state, and source/fidelity labels. Do not start with dashboard or graph canvas.
- M2 wireframe/component candidate: use `Execution` as product object, request ID as first lookup key, and build execution search table -> execution detail header -> derived timeline -> node detail -> gap panel -> payload state -> import diagnostics. User accepted this direction on 2026-07-08; keep changeable only before final PRD lock.
- PRD1 draft state: `PRD1 Draft v0.1 Candidate` now consolidates the current thesis, scope lock, release gates, functional requirements, data/architecture choices, integration strategy, risk controls, and first implementation package. It is a review base, not a final PRD.
- PRD1 readiness state: `PRD1 Readiness Review v0.1` classifies current decisions as Locked / Candidate / Open. Conclusion: start M0/M1 implementation planning; do not final-lock PRD1 release scope until Gate A, LiteLLM evidence, UI proof, payload policy, policy simulation, and open-source/commercial boundaries are validated.
- First deep integration: UniRouter through X-Insight-side DB/API import first.
- First external credibility integration: LiteLLM through real callback ingestion plus spend-log import.
- Enforcement model: observe-first plus historical policy simulation; no broad synchronous enforcement in PRD1.
- Policy engine: simple structured rule evaluator first; no OPA/Rego dependency in PRD1.
- Payload default: raw prompt/response off; payload capture represented through governed payload policy.

Remaining PRD1-blocking decisions:

- exact JSON Schema file contents during implementation;
- exact library/tool names chosen during implementation;
- exact migration SQL generated from the schema/index candidates;
- first implementation wireframes after real UniRouter imported data exists.

M0/M1 readiness:

- There are no unresolved product-level blockers to starting M0/M1.
- M0/M1 should stop or re-scope if current UniRouter records cannot produce useful evidence events, idempotency cannot be deterministic, capture gaps make the first view misleading, sensitive payload data cannot be suppressed/quarantined, or UniRouter runtime changes are required before any Gate A evidence can be produced.
- M0 foundation checkpoint existed at this historical point: schemas, migrations, fixture runner, schema validation, canonical hashing, sensitive payload quarantine, duplicate/no-op and idempotency conflict behavior, and migration runner entrypoint. At that time UniRouter import was not implemented; it is implemented now as the bounded synchronous CLI/sidecar development surface recorded in the runtime status.

Important but not PRD1-blocking yet:

- final open-source license;
- commercial packaging boundary;
- one-api/new-api implementation order;
- full OTel/OpenInference ingestion;
- ClickHouse threshold;
- OPA/Cedar/full policy engine timing;
- native UniRouter async outbox/exporter;
- product name and category language.

## Business-Readable Translation of Technical Decisions

This section translates technical choices into business and go-to-market consequences.

### Go vs Other Backend Languages

Business question:

- Will enterprise customers trust this product as infrastructure?
- Can it be deployed simply in private environments?
- Can it sit close to gateways and policy enforcement points without operational complexity?

Candidate judgment:

- Go is a reasonable default because it is operationally simple, infrastructure-friendly, and aligned with UniRouter's likely integration path.
- Python should be used for optional AI analysis/evaluation workers, not the core control plane.
- Rust should be reserved for future high-performance proxy/data-plane needs, not chosen before the product boundary is proven.

### Postgres vs ClickHouse

Business question:

- Is the first product selling operational governance value or massive-scale analytics?
- Will early customers trust a simpler deployment more than a high-scale architecture?

Candidate judgment:

- Start with Postgres for metadata, entities, moderate event volume, and transactional correctness.
- Add ClickHouse when customers need high-volume event analytics: long retention, high cardinality filtering, heavy dashboards, or millions/billions of events.
- Do not make ClickHouse mandatory on day one unless scale is already the product's first promise.

Sales translation:

- Postgres-first reduces adoption friction.
- ClickHouse-later preserves enterprise scale story without forcing early complexity.

### OPA / Cedar / Custom Policy

Business question:

- Does the customer need to trust that a decision was made consistently and explainably?
- Does the customer already have a policy system?
- Are we selling "we decide everything" or "we record and orchestrate decisions"?

Candidate judgment:

- X-Insight should define a policy decision interface before betting on one policy language.
- OPA is likely practical as an early adapter because it is common and JSON-friendly.
- Cedar may be valuable later for authorization semantics and analyzability.
- Human approval and existing IAM/FGA decisions should also be valid decision sources.

Sales translation:

- The product should not force customers to throw away existing policy systems.
- X-Insight's value is recording, explaining, replaying, and coordinating decisions across systems.

### Native Event Schema vs OpenTelemetry/OpenInference

Business question:

- Are we building another observability backend, or a governance evidence layer?
- Can customers use existing observability tools while still getting X-Insight's governance semantics?

Candidate judgment:

- X-Insight should define a native governance event envelope.
- OpenTelemetry/OpenInference should be import/export compatibility layers, not the whole schema.

Sales translation:

- Compatibility helps adoption.
- A governance-specific schema creates differentiation.

### Observe-Only vs Inline Enforcement

Business question:

- Are customers ready to let X-Insight block production AI actions?
- What happens if the policy service is slow or unavailable?
- Is the first value "visibility" or "control"?

Candidate judgment:

- Start with observe-only and policy simulation for lower adoption risk.
- Add selective inline enforcement only where latency and failure behavior are well understood.

Sales translation:

- Observe-only is easier to sell and deploy.
- Enforcement creates stronger value but much higher trust requirements.

### Open Source vs Commercial Boundary

Business question:

- What must be open for trust and ecosystem adoption?
- What can be paid without making the open-source core feel fake?

Candidate judgment:

- Open-source: schema, collector, ingestion API, basic adapters, basic evidence UI, policy interface, local deployment.
- Commercial: managed service, enterprise connectors, advanced RBAC/SSO/SCIM, HA operations, compliance packs, policy packs, SLA, private support.

Sales translation:

- Open source wins trust and distribution.
- Commercial value should come from operating the system reliably inside real enterprises.

### Protocol Choices

Business question:

- Can X-Insight enter an enterprise without forcing customers to replace existing tools?
- Can it survive protocol churn in AI/agent ecosystems?

Candidate judgment:

- Native JSON/HTTP ingestion first.
- Batch ingestion early.
- OTel/OpenInference mapping.
- MCP adapter, but not MCP-only.
- OIDC/JWT identity ingestion.
- Webhook/OPA policy adapter first.

Sales translation:

- The product should integrate with the customer's current stack instead of requiring a platform migration.
