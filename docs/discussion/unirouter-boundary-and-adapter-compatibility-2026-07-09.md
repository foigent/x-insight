# UniRouter Boundary And Adapter Compatibility

Status: working decision note
Date: 2026-07-09

## Position

X-Insight should continue to use UniRouter as the first deep integration because UniRouter is controlled, debuggable, and can expose fields that external gateways may not expose. But X-Insight must not become a UniRouter plugin. The durable product boundary is:

- UniRouter owns source facts and runtime behavior.
- X-Insight owns the evidence contract, normalization, capture-gap semantics, persistence, read APIs, UI, policy simulation, and cross-source adapter model.
- Adapters translate source-local facts into X-Insight evidence and must declare fidelity, direct fields, derived fields, and known gaps.

## Likely Friction Points

1. Source facts vs normalized evidence.

UniRouter may want source-local terms to pass through unchanged. X-Insight needs stable cross-source semantics. The adapter should preserve source details in provenance/excerpts while projecting only stable fields to the core envelope.

2. Candidate capture gaps.

UniRouter's `candidate_capture_gaps` are useful, but they are not facts. X-Insight should store them as candidate arrays for grouping and review, not as definitive root causes. This is now reflected in `capture_gaps.candidate_gap_types`.

3. Capture mode ownership.

UniRouter may report source capture mode such as `native`. X-Insight top-level `capture_mode` should describe X-Insight ingestion mode such as `db_import` or future `webhook`. UniRouter source capture mode should remain source metadata.

4. Span hierarchy.

UniRouter can expose gateway/request-local spans. X-Insight should not require UniRouter to model downstream tools, business APIs, approvals, databases, or SaaS side effects. Those need future SDK, OTel/OpenInference, MCP/tool, or business-system adapters.

5. Raw prompt/response.

UniRouter should not store or export raw prompt/response by default for X-Insight. X-Insight can later support governed on-prem payload capture, but it needs retention, encryption, RBAC, audit-on-read, redaction, export/delete, and legal-hold semantics.

6. Runtime dependency.

PRD1 should not make UniRouter synchronously depend on X-Insight. Snapshot import and later exporter/outbox should stay non-blocking.

7. Field/version drift.

UniRouter PRD5 fields and X-Insight's evidence envelope will evolve separately. The adapter manifest must become the compatibility surface: adapter version, source version, field provenance, direct/derived groups, and known capture gaps.

8. Product perception.

If only UniRouter works, the market will read X-Insight as a UniRouter extension. The core APIs, database model, event envelope, and adapter manifests must stay source-neutral, and a LiteLLM/OpenAI-compatible or OTel/OpenInference path should follow after the UniRouter loop is proven.

## Decisions Needed

1. Treat source-local capture mode and X-Insight ingestion mode as separate fields.
2. Keep `candidate_capture_gaps` as candidate-only evidence, never as root-cause facts without extra proof.
3. Keep PRD1 observe-first and non-blocking to UniRouter.
4. Require every adapter to publish a manifest with fidelity, direct/derived field groups, known gaps, and event types.
5. Do not add raw prompt/response capture until the governed payload-policy model is implemented.
6. Use read APIs and imported real snapshots to decide whether UniRouter next needs read-only DB/view, snapshot automation, or exporter/outbox.

## Current Compatibility Assessment

The current X-Insight architecture has the right compatibility spine for LiteLLM, new-api, one-api, and other gateway/runtime sources:

- top-level source identity: `source_type`, `source_version`, `source_ref`;
- adapter identity: `adapter_manifest_id`;
- immutable evidence events with idempotency keys and hashes;
- JSON field groups for correlation, identity, capability, decision, usage, outcome, payload policy, and provenance;
- explicit capture gaps;
- source-neutral read APIs under `/api/v1/executions/...`;
- fixture-level proof that the evidence model is not only UniRouter-shaped.

This is preparedness, not implemented compatibility. X-Insight cannot honestly claim support for LiteLLM, new-api, or one-api until each has:

- a current source-version field mapping;
- an adapter manifest;
- at least one fixture pair or controlled import path;
- capture-gap behavior;
- persistence tests;
- read API verification.

## Architecture Gaps To Close Before External Adapter Work

1. Move adapter manifest registration out of UniRouter-specific seeding logic.
2. Add a shared adapter interface/package boundary.
3. Add manifest files for source-specific adapters instead of only database seed defaults.
4. Refresh LiteLLM/new-api/one-api field mappings against current upstream versions before coding adapters.
5. Add ingestion modes beyond snapshot/import: callback/webhook, spend-log import, OTel/OpenInference bridge, and maybe MCP/tool events.
6. Add enum/version governance for capture gaps and event types.

## Recommendation

Proceed with the UniRouter real-snapshot closed loop first:

1. import a real sanitized UniRouter snapshot;
2. persist it;
3. read it through `/api/v1/executions/{request_id}`;
4. compare events and gaps with UniRouter's handoff note;
5. only then decide the next UniRouter integration mechanism.

In parallel, keep the public adapter narrative explicit: UniRouter is the deep reference integration, not the architecture boundary.
