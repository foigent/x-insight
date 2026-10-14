# UniRouter Auth Decision Compatibility

Status: implemented on `feature/unirouter-auth-decision-compat`
Date: 2026-07-10

## Scope

This checkpoint makes X-Insight compatible with UniRouter PRD5 Batch 1P model-permission decision evidence.

UniRouter may emit a runtime trace event with:

- `source_event_type=auth.model_not_allowed`;
- `stage=auth`;
- `capture_gap=permission_denied`;
- `decision_kind=auth`;
- `decision_result=rejected`;
- `decision_reason=auth_error.model_not_allowed`;
- `status_code=403`.

X-Insight now maps that event into normalized evidence without losing the terminal auth-denial meaning:

- normalized `event_type=auth.model.rejected`;
- normalized `stage=auth`;
- `decision.decision_kind=auth`;
- `decision.decision_result=rejected`;
- `decision.decision_reason=auth_error.model_not_allowed`;
- persisted `capture_gaps.gap_type=permission_denied`.

The raw source event type remains preserved as `source_event_type=auth.model_not_allowed`.

## Implementation

- Added `auth` to M0 stage enum/schema.
- Expanded decision kind/result schemas to include the PRD5 decision vocabulary needed for auth and future quota/budget/policy decisions.
- Added a UniRouter fixture for `model_permission_denied`.
- Promoted source trace events with `raw_source_excerpt.capture_gap` into `capture_gaps`, not only top-level `gap.capture.detected` events.
- Extended CI read API smoke to verify the auth-denied execution and gap readback.

`capture_gap_events` in the importer report still counts only top-level snapshot `capture_gaps`. Persistent `capture_gaps` can be higher because explicit source-event gaps such as `permission_denied` are promoted into the read model.

## Boundary

This does not create a UniRouter runtime dependency. It also does not infer missing billing, routing, or provider events. It records the gateway-owned auth decision and the explicit `permission_denied` gap from UniRouter evidence.

## Remaining Work

1. Wait for UniRouter Batch 1P Canary/Sentinel completion before treating the contract as fully stable.
2. Import a real controlled snapshot containing an auth-denied request.
3. Compare the read API output against UniRouter's handoff note.
