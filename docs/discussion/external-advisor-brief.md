# X-Insight External Advisor Brief

Status: advisor discussion brief, not PRD
Created: 2026-07-07

## How to Use This Brief

This brief is intended to be forwarded to external advisors for critical review.

Please ask advisors not to provide generic encouragement. The goal is to find:

- wrong assumptions
- missing categories
- architectural dead ends
- weak commercial logic
- better product sequencing
- technical constraints we may be underestimating
- existing competitors we are misreading

## Project Background

X-Insight is an early-stage product concept being explored as an open-source project with possible future commercial services.

The starting observation is that enterprise AI and AI agents are moving from "chat and content generation" toward "enterprise execution":

- accessing internal data
- calling tools
- invoking APIs
- querying databases
- operating across business systems
- acting on behalf of humans, applications, and other agents
- creating side effects such as tickets, emails, updates, approvals, or external communications

As this happens, enterprises may face a structural governance gap:

> They cannot clearly see, identify, explain, control, audit, or prove what AI/Agents actually did across models, tools, data, applications, and business workflows.

This problem may not yet be fully named by customers. The goal is not to invent artificial demand, but to reveal a governance problem that becomes inevitable as AI/Agent adoption deepens.

## Existing Related Products and Why They May Be Insufficient

The market already has adjacent products. X-Insight is not being imagined in an empty market.

### LLM Observability and Evaluation

Examples:

- Langfuse
- LangSmith
- Arize Phoenix / OpenInference
- Braintrust
- AgentOps
- Datadog LLM Observability

Strength:

- traces, spans, sessions, prompt debugging, evals, latency/cost/quality analysis

Potential limitation:

- often developer/debugging oriented
- observation does not automatically equal governance
- identity, business intent, policy decisions, evidence, and side effects are not always first-class governance primitives

### AI / LLM Gateways

Examples:

- UniRouter
- LiteLLM
- one-api / new-api
- Portkey
- Cloudflare AI Gateway
- Kong AI Gateway

Strength:

- model routing, API keys, quota, budgets, logs, provider abstraction, guardrails

Potential limitation:

- model gateway only sees model traffic
- tool calls, RAG retrieval, MCP calls, database queries, internal API writes, and business side effects may happen outside the gateway

### Agent IAM / Identity

Examples:

- Microsoft Entra Agent ID
- Auth0 / Okta agent identity patterns
- WorkOS FGA

Strength:

- identity lifecycle, delegated access, OBO, token governance, access review

Potential limitation:

- identity systems can say who was allowed, but not necessarily reconstruct what happened in the AI execution chain

### MCP / Agent Gateways and Guardrails

Examples:

- agentgateway
- Invariant Guardrails
- Snyk Agent Scan
- Lakera / Prompt Security / HiddenLayer / Protect AI style security products

Strength:

- closer to tool and agent action paths
- useful for prompt injection, tool-call risk, MCP security, detector verdicts

Potential limitation:

- MCP is not all enterprise execution
- guardrail/security verdicts are not the same as a full evidence-backed execution governance ledger

## Current Working Category

The current proposed category is:

> AI Execution Governance Plane

Alternative wedge names being considered:

- AI/Agent Flight Recorder
- AI Execution Ledger
- AI Data Access Governance
- Agent Runtime Governance

Current thesis:

> X-Insight should define a gateway-neutral execution governance layer that connects actor identity, business intent, run lineage, model calls, tool calls, data access, internal API actions, policy decisions, evidence, outcomes, and side effects.

## Proposed Product North Star

X-Insight should answer a compound enterprise governance question:

> Under this business intent, which human/app/agent performed which model/tool/data/API actions against which resources, what policy decisions allowed or denied them, what evidence supports the result, and what side effects occurred?

This is broader than:

- "Which model was called?"
- "How much did it cost?"
- "Which prompt failed?"
- "Was this output safe?"
- "Which identity had permission?"

## Proposed First Integration

The first reference integration is expected to be UniRouter because it is controlled by the team and can be deeply instrumented.

However, X-Insight should not become a UniRouter plugin.

The desired architecture is:

- X-Insight core owns event schema, evidence ledger, policy-decision records, UI, and adapter contracts.
- UniRouter is the first event emitter and optional enforcement point.
- Later adapters should support OpenAI-compatible gateways, LiteLLM, one-api/new-api, OpenTelemetry/OpenInference, MCP/tool-call systems, SDKs, IAM/IdP systems, internal API gateways, approval systems, and security detectors.

## Candidate First "Aha" Moment

Current candidate:

> A human-readable end-to-end AI/Agent execution map.

Instead of showing raw trace rows or JSON-like spans, the UI should show an end-to-end execution graph/story:

- who initiated the run
- what business intent was declared or inferred
- which model was called
- which tool/data/API/resource was accessed
- what the agent did at each step
- what policy decision occurred
- what evidence supported the step
- whether the action was allowed, blocked, redacted, escalated, or approved
- what business side effect occurred

Each node should be clickable and answer:

- What happened?
- Why did it happen?
- Who/what did it?
- What data/resource was touched?
- Was it allowed?
- What evidence exists?
- What was the result?

This may overlap visually with existing trace-tree/session-replay products, but the intended difference is that X-Insight's graph is governance and evidence oriented, not only developer-debugging oriented.

## Candidate Core Event Concepts

The current proposed AI Execution Ontology is intentionally small:

- Actor: human, application, service, agent, sub-agent
- Owner: team, person, tenant, sponsor, service owner
- Intent: declared/inferred/corrected business purpose
- Run: execution session or workflow instance
- Capability: model, tool, MCP server, data source, API, workflow, browser/computer-use action
- Action: read, retrieve, generate, transform, call, execute, write, send, approve, delegate
- Resource: data object, document, table, customer, ticket, account, file, system, API, external recipient
- Policy Decision: allow, deny, redact, require approval, degrade, sandbox, observe-only, escalate
- Evidence: prompt reference, retrieval reference, tool result reference, policy reason, detector verdict, approval record, output reference
- Outcome: success, failure, blocked, escalated, side effect committed, partial, degraded
- Delegation: on-behalf-of, parent agent, sub-agent, approval chain

## Open Question: Minimal vs More Fields

The current concern:

- If too few fields are captured early, future governance and replay may become impossible.
- If too many fields are mandatory early, integrations become heavy, adoption slows, privacy risk grows, and most data will be incomplete or low quality.

Current candidate principle:

> Small stable mandatory core + rich optional context + extension fields + evidence references + schema versioning.

Advisor question:

- What fields must be mandatory in v1?
- Which fields should be optional but strongly recommended?
- Which fields should live in extensible attributes?
- How should partial or unknown identity/intent/resource data be represented?

## Evidence and Privacy Question

X-Insight may become a sensitive evidence store.

Candidate capture modes:

- metadata only
- hash only
- redacted content
- sampled full content
- full content for selected systems
- customer-managed evidence storage

Advisor questions:

- What should never be stored by default?
- How should raw prompt/response/tool-result visibility be controlled?
- Should evidence be tamper-evident?
- How should customers prove an event happened without exposing sensitive content to X-Insight operators?

## Ontology Boundary Question

X-Insight should not start by rebuilding a full enterprise ontology or Palantir-style business object model.

Candidate approach:

- start with AI Execution Ontology
- derive resource/action/process graphs from runtime events
- clearly label facts versus inferences
- allow business users to correct inferred semantics over time

Advisor questions:

- Is this boundary correct?
- What is the smallest useful ontology?
- Where does execution ontology end and enterprise ontology begin?
- How can the product avoid becoming a consulting-heavy customization project?

## Enforcement Question

Current staged approach:

- Phase 1: observe and record
- Phase 2: async alert and policy simulation
- Phase 3: selective inline enforcement
- Phase 4: multi-enforcement-point policy orchestration

Advisor questions:

- Is observe-first the right adoption path?
- When should X-Insight block actions?
- What should happen if X-Insight is unavailable?
- Should failure mode be fail-open or fail-closed by risk tier?
- Which enforcement points are practical first: UniRouter, SDK, MCP broker, sidecar, internal API gateway, or approval workflow?

## Open Source and Commercial Boundary

Current candidate open-source core:

- event schema
- collector
- ingestion API
- adapter SDK
- UniRouter adapter
- OpenAI-compatible gateway adapter
- OpenTelemetry/OpenInference mapping
- MCP/tool-call adapter basics
- basic evidence ledger
- basic trace/execution UI
- basic redaction/capture modes
- policy decision interface
- local/self-host deployment

Candidate commercial layer:

- managed cloud
- enterprise deployment support
- HA/multi-region operations
- advanced SSO/RBAC/SCIM
- enterprise connectors
- advanced retention/legal hold/customer-managed keys
- industry policy packs
- compliance evidence reports
- SIEM/SOC integrations
- governance workflows
- support/SLA

Advisor questions:

- Is this open-source/commercial boundary credible?
- Should the core license be Apache 2.0, AGPL, BSL/source-available, or something else?
- How should the project avoid being cloned while still encouraging adapter ecosystem adoption?
- Which paid features would enterprises accept without feeling the open-source core is crippled?

## Candidate Technical Direction

Current conservative direction:

- Backend/control plane: Go
- Frontend: TypeScript + React
- Metadata store: Postgres first
- Evidence objects: S3-compatible object storage
- High-volume analytics: ClickHouse later when justified
- Local deployment: Docker Compose
- Enterprise deployment: Kubernetes/Helm later
- AI analysis workers: optional Python later

Advisor questions:

- Is Go + React + Postgres-first a reasonable early architecture?
- Should ClickHouse be introduced earlier?
- Should the system use OpenTelemetry as the primary data model or only as interoperability?
- Should MCP be first-class or adapter-level?
- What technical choices would create avoidable long-term debt?

## Specific Advisor Questions

Please answer directly and critically:

1. Is "AI Execution Governance Plane" a valid category thesis, or is it too abstract/too early?
2. What existing product already comes closest to this vision?
3. What is the strongest reason this product might fail?
4. Is the first "aha" moment of an end-to-end human-readable AI/Agent execution map compelling enough?
5. What should be the minimum event contract for v1?
6. What data should X-Insight never store by default?
7. How should X-Insight distinguish facts from inferred business semantics?
8. Should the first version be observe-only, policy simulation, or limited enforcement?
9. Is UniRouter the right first integration, or should the first reference integration be more broadly recognizable such as LiteLLM, OpenTelemetry/OpenInference, or MCP?
10. What should be open-source, and what can be commercial?
11. What license would you recommend and why?
12. What phased roadmap would you propose from vision to PRD1, PRD2, and PRD3?
13. What assumption in this brief is most likely wrong?
14. What important user, buyer, or deployment constraint is missing?
15. If you were building this, what would you deliberately not build in the first year?

