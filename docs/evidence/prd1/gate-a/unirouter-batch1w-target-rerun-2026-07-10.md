# UniRouter Batch 1W Target Rerun

Status: Target verified and externally persisted/read back
Date: 2026-07-10
Authorization: `UR-XI-GATE-A-20260710-02`

## Result

UniRouter `e823abc` containing Batch 1W `931400a` was deployed to the confirmed
existing Alibaba Cloud ECS development/test target. Stable and canary readiness
passed, both use the same new backend image, and the database migration version
remained `000018_prd5_structural_evidence_fields`.

One controlled model-permission denial returned HTTP `403` with the same
request ID supplied by the caller. UniRouter persisted:

- `event_type=auth.model_not_allowed`;
- `stage=auth`;
- `capture_gap=permission_denied`;
- `decision_kind=auth`;
- `decision_result=rejected`;
- `decision_reason=auth_error.model_not_allowed`.

The exact request produced zero call logs, billing freezes, transactions, and
provider-stage events. Temporary API-key IDs 36 and 37 are disabled.

## Artifact Reconciliation

| Artifact | Identity |
|---|---|
| Request ID | `gatea-model-denied-b1w-283194e67a0c3ff9` |
| Exchange | `uxchg_8deae233291214154815c336c9278983` |
| Approval manifest SHA-256 | `8cc3d3b54bd8c0220747c0f19cf49cb6f34bcb2a68373a0074ad60dea153c0f4` |
| Snapshot SHA-256 | `3c0b13241eb369ca096115f420e73d1ae35c3e6ac4b794cb997f56cf09c7339c` |
| Receipt SHA-256 | `16cbac6030f5f3e08f6df73593a5ef61c5395cde73e816d6e10f7f08ab81ea36` |
| Receipt status | `persisted` |

The snapshot contains one source trace event, zero call-log rows, and one
generated `call_logs/not_available_from_source` gap. X-Insight maps this to one
`auth.model.rejected` event and one `gap.capture.detected` event.

First Postgres persistence inserted both events. A second persistence run
returned two duplicate/no-op outcomes with zero conflict. Readback returned two
events and two gaps: the source decision's `permission_denied` classification
and the explicit missing-call-log gap.

## Assurance Boundary

- Raw prompt/response and credentials were not exported.
- No upstream provider request or paid model call occurred.
- The original Gate A snapshot and receipt were not modified.
- The result is UniRouter target verified plus local X-Insight persistence. It
  is not X-Insight ECS or production deployment evidence.
- A model denial before call-log creation can be correct behavior while still
  producing an explicit call-log source gap. The two facts must remain separate.

Shared artifacts:

- `evidence/gatea-b1w-snapshot-20260710.json`
- `evidence/gatea-b1w-handoff-note-20260710.md`
- `evidence/gatea-b1w-snapshot-20260710.receipt.json`
- `requests/gatea-b1w-approved-request-id-20260710.json`

## Operational Findings

The target's public security group and host port publication are materially
broader than least privilege. An unsolicited connection reached a temporary
high-port listener before the authorized upload client. Security-group and port
hardening is required, but must be handled separately with a verified access
path to avoid locking out the environment.

The live deployment directory is a historical rsync working tree rather than a
clean release checkout. This cycle used exact source manifests, artifact hashes,
runtime commit metadata, and retained rollback images. Future deployment design
should use immutable artifacts instead of treating the live directory as Git.
