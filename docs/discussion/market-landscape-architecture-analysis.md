# Market Landscape and Architecture Analysis

Status: discussion analysis, not PRD
Created: 2026-07-07

## Purpose

This document analyzes adjacent products around LLM observability, AI gateways, agent identity, MCP security, guardrails, and AI security platforms.

The goal is not to prove that X-Insight has no competitors. The goal is to understand each product category's architectural starting point, strengths, natural limits, and expansion cost.

## High-Level Judgment

There are already many adjacent products. The market is not empty.

The important distinction is that most products start from one of these architectural centers:

- LLM observability and evaluation
- AI/LLM gateway
- OpenTelemetry-style telemetry substrate
- Agent identity / IAM
- MCP or agent connectivity proxy
- AI security / guardrails
- Model and AI supply-chain security

Each starting point creates product gravity. It determines what the system sees naturally, what it can enforce synchronously, who buys it, and what will be expensive to add later.

The stronger X-Insight opportunity is not to build another product in one existing box. It is to define a gateway-neutral execution governance layer that connects:

- actor identity
- business intent
- run/session lineage
- model calls
- tool and MCP calls
- data access
- internal API actions
- policy decisions
- evidence and replay
- outcomes and side effects

This is still a hypothesis, not a product conclusion.

## Category 1: LLM Observability and Evaluation Platforms

Examples:

- Langfuse
- Arize Phoenix / OpenInference
- LangSmith
- Braintrust
- AgentOps
- Datadog LLM/Agent Observability

Representative sources:

- Langfuse overview: https://langfuse.com/docs
- Langfuse self-hosting architecture: https://langfuse.com/self-hosting
- Phoenix overview: https://arize.com/docs/phoenix
- OpenInference: https://arize-ai.github.io/openinference/
- LangSmith observability: https://docs.langchain.com/langsmith/observability
- Braintrust docs: https://www.braintrust.dev/docs
- AgentOps docs: https://docs.agentops.ai/v2/introduction
- Datadog Agent Observability: https://docs.datadoghq.com/llm_observability/

### Architectural Starting Point

These systems usually start from SDK instrumentation, tracing, datasets, evals, prompt management, and production monitoring.

Typical architecture:

- application SDKs or framework integrations
- trace/span model
- session/user grouping
- prompt and completion logging
- RAG/tool call spans when instrumented
- analytics store such as ClickHouse or similar columnar storage
- blob/object store for large event payloads
- evaluation datasets and scoring workflows
- dashboards for debugging, quality, latency, cost, and regression

Langfuse is a good example of production-grade self-hosted observability architecture: app containers, storage components, optional LLM gateway/API, ClickHouse/Postgres/Redis/blob storage, queued ingestion, and API-key caching.

Phoenix/OpenInference is a good example of an OpenTelemetry-compatible tracing and evaluation direction.

### Strengths

- Best at developer debugging and AI quality iteration.
- Good trace UX for prompts, completions, RAG, tool calls, and agent steps.
- Strong eval workflow: turn production traces into datasets and regression tests.
- Usually easier to adopt by AI application teams.
- Self-hosting exists for several open-source products.
- Natural fit for "why did this answer fail?" and "which prompt/model/regression caused this?"

### Weaknesses

- Observation is not enforcement.
- Data quality depends on instrumentation coverage. If the app or framework does not emit spans, the platform does not know what happened.
- Identity is usually user/session/team metadata, not a full enterprise identity/delegation chain.
- Business intent is usually inferred from tags or app context, not a first-class policy dimension.
- Policy decisions are not normally the central data model.
- Tool/data/API side effects may be represented as spans, but not necessarily as governed actions with resources, decisions, and durable evidence.
- Full prompt/completion capture creates privacy, compliance, and retention risk.

### Natural Expansion Difficulty

It is relatively easy for these products to add more dashboards, evals, prompt management, and integrations.

It is harder to become a runtime governance system because enforcement requires being in a control path:

- gateway
- SDK wrapper
- sidecar
- MCP/tool proxy
- internal API proxy
- policy decision service

If the core trace schema is not governance-ready, retrofitting policy replay later is expensive. A span that says "tool called send_email" is weaker than an event that records actor, intent, resource, risk, policy decision, approval, result, and side effect.

### X-Insight Implication

Do not compete head-on as "another Langfuse."

X-Insight should import/export traces through OpenTelemetry/OpenInference where possible, but its differentiated schema should focus on governance semantics:

- actor
- intent
- capability
- action
- resource
- decision
- evidence
- outcome
- delegation

## Category 2: AI/LLM Gateways

Examples:

- LiteLLM
- Portkey
- Cloudflare AI Gateway
- Kong AI Gateway
- one-api
- new-api
- UniRouter

Representative sources:

- LiteLLM AI Gateway: https://docs.litellm.ai/docs/simple_proxy
- Portkey docs: https://portkey.ai/docs/introduction/what-is-portkey
- Cloudflare AI Gateway: https://developers.cloudflare.com/ai-gateway/
- Kong AI Gateway: https://developer.konghq.com/ai-gateway/
- one-api README: https://github.com/songquanpeng/one-api/blob/main/README.en.md
- new-api README: https://github.com/QuantumNous/new-api

### Architectural Starting Point

These products start from a model traffic control point.

Typical architecture:

- OpenAI-compatible API proxy
- provider adapter layer
- model routing
- provider failover and load balancing
- virtual keys and API-key management
- per-user/team/org rate limits and budgets
- spend tracking
- request logs and metrics
- prompt/response transformations
- guardrail plugins
- admin UI
- optional Redis/Postgres/config store
- sometimes edge-worker or API-gateway plugin architecture

Kong's direction is API gateway plus AI-specific plugins. Cloudflare's direction is edge-hosted AI traffic control. LiteLLM is an OpenAI-compatible proxy plus SDK/gateway. Portkey combines gateway, observability, guardrails, prompt controls, and edge deployment.

one-api/new-api are closer to practical API relay, quota, channel, billing, and private deployment needs.

### Strengths

- Very practical adoption path: change base URL and API key.
- Strong central control for model traffic.
- Cost, budget, quota, routing, key management, and provider abstraction are natural.
- Good fit for platform engineering and MaaS teams.
- Runtime enforcement is possible for model requests and responses.
- Can add guardrail checks before and after LLM calls.
- Fits UniRouter as an early X-Insight reference integration.

### Weaknesses

- Gateway only sees traffic that passes through it.
- Model-call visibility is not the same as agent execution visibility.
- Tool calls, MCP calls, DB queries, RAG retrieval, browser actions, and internal API writes may happen outside the gateway.
- Business intent is usually absent unless upstream apps send it.
- API key/user/team identity is not enough for agent delegation chains.
- Gateway logs are often request-centric, not run-centric.
- Policy tends to be rate/cost/provider/content oriented, not business action oriented.

### Natural Expansion Difficulty

Gateways can naturally expand into:

- routing
- caching
- token budgets
- provider abstraction
- prompt/response guardrails
- basic audit
- some MCP/tool routing if they add an agent proxy path

It is harder for a pure model gateway to become an execution governance plane because the most important enterprise risk happens after or outside the model call:

- reading sensitive records
- querying internal systems
- calling tools
- writing tickets
- sending emails
- modifying CRM/ERP/OA data
- delegating work to sub-agents

To cross this boundary, the gateway must either become a broader agent/tool/data gateway or integrate with one. That is a significant architectural and organizational shift.

### X-Insight Implication

UniRouter is a strong first integration point, but X-Insight must not become a UniRouter plugin.

Correct shape:

- UniRouter adapter emits governance events.
- X-Insight core owns event schema, evidence ledger, policy-decision records, and adapters.
- Later adapters support LiteLLM, one-api/new-api, OpenAI-compatible gateways, OTel/OpenInference, and MCP/tool proxies.

## Category 3: OpenTelemetry and AI Telemetry Substrates

Examples:

- OpenTelemetry GenAI semantic conventions
- OpenInference
- OpenLLMetry / Traceloop

Representative sources:

- OpenTelemetry GenAI attributes: https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/
- OpenTelemetry GenAI blog: https://opentelemetry.io/blog/2026/genai-observability/
- OpenInference: https://arize-ai.github.io/openinference/
- OpenLLMetry: https://github.com/traceloop/openllmetry

### Architectural Starting Point

These projects are not complete governance products. They provide conventions, instrumentation, exporters, collectors, and interoperability.

Typical architecture:

- language SDKs
- instrumentation wrappers for LLM SDKs and frameworks
- trace/span attributes
- OTLP exporters
- collector pipeline
- backend-neutral ingestion

### Strengths

- Avoids vendor lock-in.
- Fits existing enterprise observability stacks.
- Good baseline for tracing model calls, tools, RAG retrieval, tokens, latency, provider metadata, and errors.
- Can connect to Datadog, Honeycomb, Grafana, Phoenix, and other backends.
- Strong ecosystem momentum.

### Weaknesses

- Standardization is not a product.
- It does not decide policy.
- It does not create evidence retention strategy.
- It does not solve identity, intent, data access governance, approval, or action side effects by itself.
- Semantics evolve; depending too tightly on any immature convention can cause churn.
- Raw telemetry tends to optimize for observability, not legal/audit-grade evidence.

### Natural Expansion Difficulty

OpenTelemetry can represent more attributes and events, but it will not become a governance product by itself. Governance needs:

- policy decision records
- identity/delegation context
- resource/action semantics
- approval evidence
- retention/redaction modes
- explainability
- replay/simulation
- workflow UI

### X-Insight Implication

X-Insight should be OTel-compatible but not OTel-defined.

Use OpenTelemetry/OpenInference as interoperability layers. Define X-Insight governance semantics above them.

## Category 4: Agent Identity / IAM Platforms

Examples:

- Microsoft Entra Agent ID
- Microsoft Agent 365
- Okta/Auth0 for AI Agents
- WorkOS FGA and agent identity patterns

Representative sources:

- Microsoft Entra Agent ID docs: https://learn.microsoft.com/en-us/entra/agent-id/
- Microsoft Entra agent identity governance: https://learn.microsoft.com/en-us/entra/id-governance/agent-id-governance-overview
- Auth0 for AI Agents: https://auth0.com/docs/get-started/auth0-for-ai-agents
- WorkOS FGA for AI agents: https://workos.com/blog/agents-need-authorization-not-just-authentication

### Architectural Starting Point

These products start from identity lifecycle, authentication, authorization, OAuth/OIDC, access governance, FGA, token vaults, and enterprise IAM integration.

Typical architecture:

- agent identities as first-class accounts or applications
- owner/sponsor assignment
- lifecycle governance
- access reviews
- conditional access
- OAuth/OIDC token issuance
- token exchange or on-behalf-of flows
- scoped credentials
- policy enforcement at identity/resource access boundaries

### Strengths

- Strong enterprise trust and buyer fit.
- Natural integration with HR, directory, SSO, SCIM, access review, and conditional access workflows.
- Good at lifecycle, ownership, revocation, and credential governance.
- "On behalf of user" and scoped delegated access are concrete and important.
- Likely to become default identity substrate in many enterprises.

### Weaknesses

- Identity events are not execution traces.
- Knowing that an agent had permission does not explain what it actually did, why, with what evidence, and what side effect happened.
- Business intent and runtime behavior still need instrumentation.
- Downstream tools and internal systems must enforce or report resource-level access.
- If the agent uses a shared credential or bypass path, IAM visibility weakens.
- IAM systems may not model LLM prompts, RAG retrieval, tool result content, or agent reasoning traces.

### Natural Expansion Difficulty

IAM vendors can expand into agent lifecycle and access governance. They are likely to own parts of agent identity.

It is harder for them to become full AI execution recorders unless they instrument or proxy the runtime/tool/data path. Identity control is necessary but not sufficient.

### X-Insight Implication

Do not compete with Entra/Auth0/Okta/WorkOS as the source of identity.

X-Insight should integrate with them and consume:

- user identity
- agent identity
- app/service identity
- token claims
- group/role context
- owner/sponsor
- lifecycle state
- delegation/OBO chain

Then X-Insight records what happened during execution.

## Category 5: MCP / Agent Connectivity / Agent Data Plane

Examples:

- agentgateway
- Invariant Guardrails / Invariant Gateway
- Snyk Agent Scan
- mcp-scan-style scanners

Representative sources:

- agentgateway docs: https://agentgateway.dev/docs/standalone/main/about/introduction/
- agentgateway GitHub: https://github.com/agentgateway/agentgateway
- Invariant Guardrails GitHub: https://github.com/invariantlabs-ai/invariant
- Invariant Guardrails: https://invariantlabs.ai/guardrails
- Snyk Agent Scan: https://github.com/snyk/agent-scan

### Architectural Starting Point

These products start near agent-to-tool, agent-to-agent, and MCP/A2A communication.

Typical architecture:

- MCP proxy/gateway
- A2A proxy/gateway
- LLM gateway features
- tool federation
- RBAC/ABAC policy
- guardrails before/after tool/model calls
- OpenTelemetry export
- static or dynamic MCP scanning
- tool manifest analysis
- local agent configuration scanning
- stateful session handling

agentgateway is especially relevant because it explicitly treats MCP and A2A as different from traditional stateless HTTP gateways and focuses on stateful sessions, agent/tool/LLM connectivity, security, governance, and observability.

Invariant is especially relevant because it provides rule-based guardrails over LLM/MCP powered applications and can evaluate flows between tool calls.

### Strengths

- Closer to actual agent actions than a pure LLM gateway.
- Can see tool definitions, tool calls, MCP sessions, and sometimes A2A exchanges.
- More natural point for policy over tools and side effects.
- Better fit for "agent does something" than "model returns text."
- Strong early signal that tool-call security is becoming its own category.

### Weaknesses

- MCP and A2A are important but not the whole enterprise integration universe.
- Many tools are still direct REST, database, browser, queue, workflow engine, SDK, or internal API calls.
- Protocols are evolving quickly.
- A protocol gateway may know the tool call but not the surrounding business process.
- Identity propagation across host/client/server/tool/resource is still hard.
- Governance can become tool-protocol-specific instead of enterprise-execution-specific.

### Natural Expansion Difficulty

This category can expand toward execution governance more naturally than model-only gateways because it is closer to actions.

The main difficulty is scope:

- non-MCP tools
- legacy systems
- internal APIs
- data warehouses
- SaaS permissions
- browser/computer-use agents
- business intent and outcome modeling

### X-Insight Implication

MCP/tool-call governance is likely one of the strongest X-Insight integration directions.

But X-Insight should not be MCP-only. It should define a generic capability/action/resource model and let MCP be one adapter.

## Category 6: AI Security and Guardrail Platforms

Examples:

- Lakera / Check Point AI Security
- Prompt Security / SentinelOne
- HiddenLayer
- Protect AI
- Palo Alto AI Runtime Security

Representative sources:

- Lakera prompt defense: https://docs.lakera.ai/docs/prompt-defense
- Lakera quickstart: https://docs.lakera.ai/docs/quickstart
- Prompt Security: https://www.prompt.security/
- SentinelOne Prompt Security: https://www.sentinelone.com/platform/securing-ai-prompt/
- HiddenLayer docs: https://docs.hiddenlayer.ai/
- Protect AI: https://protectai.com/
- Protect AI ModelScan: https://protectai.com/modelscan

### Architectural Starting Point

These products start from security detection, guardrails, AI risk management, model scanning, red teaming, prompt injection defense, data leakage prevention, and shadow AI monitoring.

Typical architecture:

- classifier or rule-based prompt/response inspection
- data leakage and PII detectors
- browser/proxy/API integrations
- shadow AI discovery
- runtime attack detection
- model scanning and supply-chain scanning
- red-team test harnesses
- security dashboards and incident workflows

### Strengths

- Clear CISO/security buyer.
- Stronger threat intelligence and security framing.
- Better at prompt injection, jailbreak, PII leakage, unsafe content, malicious model artifacts, and shadow AI discovery.
- Some products already cover workforce AI, custom apps, code assistants, and agentic risks.
- Security teams understand guardrails, scanning, and detection.

### Weaknesses

- Many controls are classifier gates or scanning workflows, not full execution governance.
- False positives and false negatives are unavoidable.
- Blocking bad text is not the same as authorizing a business action.
- Security verdicts alone do not create a replayable business evidence chain.
- Some products are SaaS/security-stack-first, which can limit open-source adoption and on-prem flexibility.
- They may not expose enough event semantics for downstream execution ontology or policy replay.

### Natural Expansion Difficulty

These products can expand into runtime controls and agent security.

It is harder to become a full governance ledger unless they capture:

- actor identity
- business intent
- tool/data/API action
- policy decision
- approval
- evidence references
- outcome and side effects

Security products can say "this was risky." X-Insight should be able to say "this actor, under this intent, accessed this resource through this capability, the policy allowed/denied it for these reasons, and here is the evidence."

### X-Insight Implication

Do not try to out-Lakera Lakera or out-security HiddenLayer.

Integrate with guardrail/security engines as policy or detector plugins. Use their verdicts as evidence, not as the whole product.

## Architectural Dead Ends to Avoid

### 1. Request-Centric Logging

If the core unit is only an LLM request, the product will struggle with agent runs.

Needed unit:

- run/session
- parent-child execution
- model/tool/data/API events
- side effects
- outcome

### 2. Gateway-Only Visibility

If all visibility depends on one model gateway, the product is blind to:

- tool calls outside the gateway
- RAG retrieval
- internal API access
- MCP sessions outside the gateway
- browser/computer-use actions
- direct provider calls
- shadow AI

### 3. SDK-Only Visibility

SDK instrumentation is rich but incomplete. Adoption is uneven and enforcement is weak unless the SDK is mandatory.

### 4. Classifier-Only Guardrails

Classifiers help, but deterministic governance needs policy decisions over actors, resources, actions, approvals, and risk.

### 5. Identity-Only Governance

IAM answers who can authenticate and what token/scopes exist. It does not prove what happened in a multi-step AI execution.

### 6. MCP-Only Design

MCP is important but not universal. Enterprise AI will call legacy APIs, databases, SaaS systems, workflow engines, queues, files, and browsers.

### 7. Full-Content Capture by Default

Storing all prompts, completions, retrieval documents, and tool results creates privacy and liability risk.

X-Insight needs capture modes:

- metadata only
- hashes only
- redacted content
- sampled full content
- full content for selected systems
- customer-managed evidence storage

### 8. Dashboard-First Product Thinking

Dashboards are easy to clone. The durable value is the event model, evidence chain, policy interface, replay, and integration ecosystem.

### 9. Full Enterprise Ontology First

Starting with full business ontology modeling creates consulting drag.

Better approach:

- start with AI Execution Ontology
- derive process/resource graph from runtime events
- validate gradually
- avoid pretending early inferred graphs are truth

## Where the User's Hypothesis Seems Right

The intuition is mostly right:

> A product's architecture and first data model can make future governance capabilities either natural or extremely expensive.

Examples:

- A model gateway can naturally add budgets, routing, and prompt guardrails, but not automatically know downstream business actions.
- A tracing platform can naturally add evals and debug UX, but not automatically enforce access decisions.
- An IAM platform can naturally add agent lifecycle and credentials, but not automatically reconstruct execution evidence.
- An MCP gateway can naturally govern MCP tools, but not automatically cover all enterprise APIs and business systems.

The sharper formulation:

> Architecture does not fully determine product destiny, but it determines expansion cost.

Products can pivot, but they must pay costs in:

- data model changes
- customer migration
- integration surface
- buyer repositioning
- latency and reliability guarantees
- privacy and retention posture
- policy semantics
- ecosystem credibility

## X-Insight Design Constraints Implied by the Market

### 1. Gateway-Neutral Core

X-Insight should not assume UniRouter, LiteLLM, one-api, or new-api as the center.

It should define:

- event ingestion API
- adapter SDK
- normalized schema
- governance event model
- evidence store
- policy decision interface

### 2. Governance-Ready Event Schema from Day One

Even if the MVP only observes, the schema should include:

- actor
- owner
- tenant/workspace
- intent
- run/session
- parent event
- capability
- action
- resource
- policy decision
- detector verdicts
- evidence refs
- outcome
- side effect status

### 3. Multiple Integration Planes

Need adapters for:

- model gateways
- SDK/framework tracing
- OpenTelemetry/OpenInference
- MCP/tool gateways
- RAG/vector retrieval
- internal API gateways
- IAM/IdP systems
- approval systems

### 4. Policy Interface Before Policy Monopoly

Do not bind too early to one policy engine.

Support a decision interface that can connect to:

- OPA/Rego
- Cedar
- custom webhooks
- existing enterprise IAM/FGA systems
- AI security detectors
- manual approval workflows

### 5. Evidence Storage Must Be Privacy-Aware

Evidence should use references and capture modes, not raw full-content capture by default.

### 6. X-Insight Should Be Able to Feed Existing Tools

It should integrate with, not replace:

- Langfuse/Phoenix/Datadog for observability
- LiteLLM/UniRouter/Kong/Portkey for gateway traffic
- Entra/Auth0/Okta/WorkOS for identity
- Lakera/Prompt Security/HiddenLayer for detector verdicts
- SIEM/SOC tools for incidents

## Working Differentiation Statement

Adjacent products answer important but partial questions:

- Observability platforms: what happened inside the LLM app?
- Gateways: who called which model, how much did it cost, and how was traffic routed?
- IAM platforms: who or what identity is allowed to access something?
- MCP gateways: which agent called which tool through MCP/A2A?
- Guardrails: is this prompt/output/tool flow risky?

X-Insight should aim to answer the compound governance question:

> Under this business intent, which actor or agent performed which model/tool/data/API actions against which resources, what policy decisions allowed or denied them, what evidence supports the result, and what side effects occurred?

That question is harder to clone because it requires a coherent event model plus integrations across multiple control points.

