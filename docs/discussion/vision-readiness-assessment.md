# X-Insight Vision Readiness Assessment

Status: convergence assessment, not PRD
Created: 2026-07-07

## Question

Have the discussions covered enough ground to start converging into a concrete product vision and phased PRDs, or are there still critical questions that must be discussed first?

## Short Answer

The discussion is ready to converge into a concrete Vision v0.1 and phased roadmap.

It is not ready for a full implementation PRD yet.

The right next artifact should be:

> X-Insight Vision v0.1 and Phased Landing Path

This should define the category, inevitability thesis, product north star, phased value expansion, non-goals, and PRD gates.

## What Is Already Covered Enough

### 1. Category Direction

Current convergence:

- X-Insight should not be another model gateway.
- X-Insight should not be generic LLM observability.
- X-Insight should not be narrow Agent IAM.
- X-Insight should aim at AI/Agent execution governance.

Working category:

> AI Execution Governance Plane

### 2. Structural Problem

Current convergence:

AI/Agents are moving from content generation to enterprise execution.

They will:

- access enterprise data
- call tools
- invoke APIs
- operate across systems
- act on behalf of humans, apps, and other agents
- create business side effects

Enterprises currently lack a coherent way to see, identify, explain, control, and prove this behavior.

### 3. Product Difference from Existing Categories

Current convergence:

- Gateways control model traffic but miss many tool/data/API actions.
- Observability tools see traces but do not naturally govern execution.
- IAM systems manage identity but do not reconstruct what happened.
- MCP gateways are close to tools but too protocol-specific if used as the whole product.
- Guardrail/security tools detect risk but do not create full execution evidence chains.

X-Insight should connect identity, intent, flow, action, resource, policy decision, evidence, and outcome.

### 4. Architectural Direction

Current convergence:

- gateway-neutral core
- UniRouter as first reference integration
- governance-ready event schema from day one
- adapter-based ingestion
- OpenTelemetry/OpenInference compatibility, but not defined by them
- MCP/tool-call support, but not MCP-only
- policy decision interface before one policy engine
- privacy-aware evidence storage
- observe/simulate before broad inline enforcement

### 5. Future Optionality

Current convergence:

Do not predict the winning model API, agent framework, MCP variant, identity protocol, or policy language.

Instead, preserve:

- execution context
- event schema versioning
- adapter boundaries
- evidence references
- capture modes
- policy decision records
- control-plane/data-plane separation

### 6. Initial Technical Bias

Current candidate direction:

- Go backend/control plane
- TypeScript/React frontend
- Postgres first
- object storage for evidence
- ClickHouse later when event analytics volume justifies it
- Docker Compose for local/dev
- Kubernetes/Helm later for enterprise deployment

This is enough as a candidate, not yet a final architecture spec.

### 7. Open-Source / Commercial Boundary

Current candidate direction:

Open-source core should include:

- schema
- collector
- ingestion API
- core adapters
- basic evidence UI
- policy decision interface
- local/self-host deployment

Commercial value can come from:

- managed cloud
- enterprise connectors
- advanced SSO/RBAC/SCIM
- HA operations
- policy/evidence packs
- compliance reporting
- SIEM/SOC integrations
- private support and SLA

## What Is Still Not Discussed Deeply Enough

These are not reasons to delay Vision v0.1. They are reasons not to write a detailed PRD yet.

### 1. The Category-Creation Thesis Must Be Sharper

Current direction is right, but the category story needs a stronger inevitable-future argument.

Needed:

- Why AI/Agent execution governance becomes unavoidable.
- Why this problem is not solved by gateway, IAM, SIEM, APM, or AI observability.
- What customers currently fail to see.
- What moment makes them realize they need this category.

### 2. The First "Aha" Demonstration Is Not Yet Chosen

This is critical.

The first demo should make the hidden problem visible.

Candidates:

- AI used multiple systems to answer a question and crossed a data boundary.
- Agent used a tool/API under a shared credential, making accountability impossible.
- A run produced a correct answer through an invalid or non-compliant process.
- A model gateway showed a normal request, while X-Insight revealed downstream tool/data actions.
- A policy change replay showed which historical AI runs would now be blocked.

Without this, the product risks becoming abstract governance language.

### 3. The Minimum Event Contract Needs a Hard Line

We have a candidate field list, but not a locked minimum.

This must be decided before PRD1 because it affects every adapter, database table, UI, replay feature, and policy engine.

Must answer:

- Which fields are mandatory?
- Which can be unknown?
- How are partial events represented?
- How are parent/child runs connected?
- What is the stable identity of an actor, agent, capability, resource, and action?
- How does schema versioning work?

### 4. Evidence and Privacy Defaults Need a Stronger Position

We have candidate capture modes, but not default policy.

Must answer:

- What is never stored by default?
- What is stored as metadata?
- What is hashed?
- What is redacted?
- Who can view raw evidence?
- How does customer-managed evidence storage work?
- Is evidence tamper-evident?

This is not just security. It is adoption trust.

### 5. The Ontology Boundary Needs More Precision

We agree not to build a full enterprise ontology first.

But we still need to define:

- what AI Execution Ontology includes
- what it explicitly excludes
- how runtime traces become a resource/action graph
- when a graph edge is fact versus inference
- how business users correct inferred semantics

Without this, the product can drift into either shallow logs or heavyweight consulting.

### 6. Enforcement Philosophy Needs a Failure Model

We agree to observe/simulate first and enforce selectively later.

Still missing:

- What happens if X-Insight is down?
- Is fail-open or fail-closed acceptable by risk tier?
- Which actions can be blocked inline?
- Which should only alert or require approval?
- What latency budget is acceptable?

This matters before positioning X-Insight as a control plane.

### 7. The Buyer/User/Champion Map Needs Vision-Level Definition

This should not reduce the product to today's stated customer demands.

But Vision v0.1 still needs to say who will recognize the latent problem first.

Likely roles:

- AI platform/MaaS owner
- security engineering
- enterprise architecture
- data governance
- internal audit/compliance
- AI application teams

Need clarify:

- who installs it
- who uses it daily
- who approves budget
- who benefits from evidence reports
- who fears production blocking

### 8. Open-Source License and Ecosystem Strategy Are Still Open

Open-source boundary is discussed, but not license strategy.

Need discuss:

- permissive license versus copyleft versus BSL/source-available
- risk of cloud vendors copying
- whether adapter ecosystem is more important than monetizing core
- how to avoid open-core distrust

### 9. Phased PRD Gates Are Not Yet Defined

We have a phase direction, but not gate criteria.

Need define for each phase:

- what new problem becomes visible
- what evidence proves the phase works
- what integration is required
- what must not be built yet
- what future phase it enables

## Recommended Next Step

Do not keep broad discussion open-ended.

Do not write a full PRD yet.

Start the convergence artifact:

> X-Insight Vision v0.1 and Phased Landing Path

Suggested structure:

1. Fundamental thesis
2. Category definition
3. Why this becomes inevitable
4. Why existing product categories are insufficient
5. Target future state
6. Product principles
7. Core execution primitives
8. First "aha" demonstration candidates
9. Phased roadmap
10. Non-goals
11. PRD gates and unresolved hard questions

## Current Judgment

The discussion has covered most foundational dimensions.

The remaining work should be convergence, not more broad exploration.

However, before implementation PRD, the product needs a sharper answer to:

1. What is the first customer-visible "aha" moment?
2. What is the minimum event contract?
3. What are the evidence/privacy defaults?
4. Where exactly is the ontology boundary?
5. What is the enforcement failure model?
6. What is the open-source license/ecosystem strategy?

## Update After External Advisor Round 1

Round-1 external feedback strengthens the readiness-to-converge conclusion, but changes the recommended convergence framing.

Updated convergence framing:

> X-Insight Vision v0.1 should define "AI Execution Governance Plane" as the long-term category hypothesis, while PRD1 should be framed as "Agent Execution Flight Recorder" or "AI Execution Evidence Ledger."

Key changes:

- Do not use "Governance Plane" as the PRD1 product name or first wedge.
- Do not treat a human-readable execution map as the whole aha moment.
- Combine execution map, evidence pack, risk query, policy simulation, and capture-gap visibility.
- Make OpenTelemetry/OpenInference first-class from PRD1.
- Keep UniRouter as a deep reference integration, not the only public-facing first integration.
- Add capture-gap semantics as a first-class product concept.
- Keep PRD1 observe-first and defer broad inline enforcement.
- Resolve license strategy before public launch.
- Avoid "North Star" as default wording in future docs; use concrete long-term category hypothesis and staged product boundaries instead.
- Do not over-specify the first public demo before a UniRouter evidence discovery loop shows what the system can actually capture.
