# Gate A Provenance And Assurance Boundary

Status: working decision after UniRouter Batch 1T review
Date: 2026-07-10

## Decision

UniRouter Batch 1T is compatible with the current X-Insight Gate A importer and
receipt contract. It can proceed to Canary and then to one real controlled run.

No additional envelope field should block that run. The first real artifact is
now more valuable than extending a pre-runtime contract without evidence.

This acceptance is deliberately limited to a controlled dev/test exchange. It
does not make the current file-based chain a production-grade authorization or
attestation system.

## What The Current Chain Proves

| Claim | Current evidence | Assurance |
|-------|------------------|-----------|
| The approval manifest bytes did not change | UniRouter exact-byte `approval_manifest_sha256` | cryptographic content integrity |
| The received snapshot bytes did not change | X-Insight-computed `artifact.sha256` | cryptographic content integrity |
| A receipt refers to the same exchange artifact | `(exchange_id, artifact.sha256)` | deterministic reconciliation |
| The selected rows came from a declared source label | snapshot `source_instance` plus operator handoff | declared/operator-attested, not independently verified |
| The request IDs were authorized for the named scenarios | human authorization worksheet plus operator procedure | process evidence, not yet machine-linked authorization proof |
| The receipt was produced by the intended X-Insight deployment | shared-folder custody and operator procedure | operational trust, not signed attestation |

These distinctions matter because a hash proves integrity of the bytes it
covers. It does not prove that an approver had authority, that a DSN pointed to
the declared environment, or that the receipt came from a trusted deployment.

## Gate A Acceptance Conditions

The first controlled run is acceptable when all of the following are true:

- Canary passes UniRouter Batch 1T;
- the User authorizes the exact non-production environment, operator, two
  scenarios, metadata-only data classes, and temporary retention;
- the operator confirms the deployed version contains Batch 1P model-permission
  evidence;
- requests contain synthetic non-sensitive content;
- the handoff includes the exact authorization record, manifest, snapshot, and
  generated note without secrets;
- X-Insight validates, persists, reads back, and returns the receipt sidecar;
- any missing or unverified identity remains explicit rather than being
  upgraded to a verified claim.

## Production Evolution, Not Gate A Scope

Before production automation or a Data Exchange Control UI treats the chain as
authoritative, add:

1. A durable authorization record with an immutable `authorization_id` or hash,
   actor reference, approval time, reason, scope, expiry, retention, and safety
   profile.
2. A machine link from the approval manifest and exchange record to that
   authorization record.
3. A stable source-instance identity anchored in source configuration or
   storage, with an assurance level such as `declared`, `operator_verified`, or
   `cryptographically_attested`.
4. Authenticated delivery and receipt return, such as mTLS plus deployment
   identity, or signed artifacts and receipts.
5. An append-only exchange ledger that records generation, preview, download,
   delivery, receipt, re-delivery, expiry, and deletion.

These are future control-plane requirements. Adding them before the first real
snapshot would increase design certainty only on paper and delay the evidence
needed to judge the next architecture step.

## Immediate Next Step

Complete Canary for UniRouter Batch 1T, obtain explicit controlled-run
authorization, generate exactly two synthetic requests, and execute the real
snapshot -> X-Insight persist/readback -> receipt loop.
