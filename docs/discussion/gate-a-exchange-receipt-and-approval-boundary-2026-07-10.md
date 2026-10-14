# Gate A Exchange Receipt And Approval Boundary

Status: working decision
Date: 2026-07-10

## Assessment

UniRouter's proposed Data Exchange Control surface has the correct product
boundary: UniRouter should own outbound authorization and delivery history,
while X-Insight should own normalized evidence, investigation, policy
simulation, and downstream evidence operations.

Three corrections are required before treating the design as a durable
integration contract.

## 1. Approval Must Describe Meaning, Not Only An Opaque ID

Exact request-ID approval is appropriate when selecting an existing real
request. It is insufficient for a controlled test run because the approver
cannot understand the data scope from an opaque identifier alone.

Use two approval modes:

| Mode | Human approves | How request IDs are obtained |
|------|----------------|------------------------------|
| Existing evidence selection | exact request IDs plus visible sanitized metadata summary | selected from a request detail/log view |
| Controlled scenario run | scenario definitions, data classes, source instance, safety profile, retention, and operator | the authorized run creates marked requests and seals their returned IDs into the export manifest |

Controlled-run ID capture must be limited to the authorized run marker. It must
not become broad request discovery.

## 2. Each System Audits The Data It Stores

The boundary is not "all read audit belongs to X-Insight." It is:

- UniRouter audits approval, snapshot generation, preview, download, delivery,
  re-delivery, and deletion of artifacts held by UniRouter;
- X-Insight audits import, evidence read, evidence export, payload access,
  retention, legal hold, and deletion inside X-Insight.

This prevents an audit gap when a UniRouter artifact is downloaded before it is
ever imported by X-Insight.

## 3. Exchange Identity Must Be Explicit

The source snapshot should carry:

- `exchange_id`: immutable source-created identity for one outbound exchange;
- `approval_manifest_sha256`: hash of the exact approval manifest used;
- `schema_version`, `source_instance`, `generated_at`, and selected request IDs.

X-Insight computes the artifact SHA-256 after receipt. The artifact cannot
reliably contain its own hash because that would be self-referential.

For Gate A, `(exchange_id, artifact.sha256)` is the reconciliation key. A later
durable import-job service may add `import_job_id` and deep links, but those
identifiers must not be invented by a synchronous CLI that does not persist an
import job.

## Import Receipt v1

X-Insight now defines `xinsight.import-receipt.v1` with:

- source system and instance;
- optional source `exchange_id`;
- artifact schema version, SHA-256, generation time, and optional approval
  manifest SHA-256;
- result status: `validated`, `persisted`, or `rejected`;
- input, accepted, duplicate, rejected, quarantined, persistence, capture-gap,
  and readback counts;
- machine-readable warning codes;
- X-Insight check time.

Current warning codes deliberately reveal missing reconciliation context and
whether validation remained in memory. A source-supplied manifest hash is
echoed as provenance; it is not cryptographic proof of approval unless the
manifest or a future signed attestation is independently verifiable.

## Status Boundary

UniRouter may show its own lifecycle states such as `draft`, `approved`,
`generated`, `handed_off`, and `cancelled`. It may show the latest X-Insight
receipt summary as `not_received`, `validated`, `persisted`, `rejected`, or
`receipt_outdated`.

Per-event validation, quarantine reasons, idempotency conflicts, capture-gap
interpretation, evidence reads, investigations, and policy simulation remain
X-Insight details. A receipt is a reconciliation summary, not a mirror of the
X-Insight internal state machine.

## Immediate Sequence

1. Complete Gate A with two controlled scenarios and a real sanitized snapshot.
2. Add `exchange_id` and `approval_manifest_sha256` to UniRouter's snapshot
   envelope before building a receipt UI.
3. Return the X-Insight receipt as a sidecar JSON artifact.
4. Use the resulting field and workflow evidence to decide whether a minimal
   Data Exchange Control UI is justified.
5. Defer exporter/outbox, signed receipts, and durable import jobs until the
   file-based loop proves concrete operational needs.
