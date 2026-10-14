# X-Insight Connector Control V2

X-Insight owns `xinsight.connector-control-operation.v2` and
`xinsight.connector-control-result.v2`. JSON field names and identity bytes are
case-sensitive. Unknown, duplicate, cross-phase, whitespace-normalized, or
secret-bearing fields fail closed.

## Canonical operation request authority

Every request has exactly: `schema_version`, `operation_id`, `installation_id`,
`project_id`, `event_scope`, `desired_state`, `kind`, `phase`, optional
`predecessor_operation_id` plus `predecessor_result_sha256`, and `request`.
The fixed values are schema `xinsight.connector-control-operation.v2`, kind
`connect`, desired state `active`, and event scope
`["model.request.completed"]`. `desired_state` remains the overall installation
goal; `required_source_state` and result `source_state` carry the intermediate
paused/paused/active phase terminals. Phase 1 omits both predecessor fields. Phases 2
and 3 require both and bind the immediately preceding terminal result.

| phase | exact `request` fields |
| --- | --- |
| `prepare_paused_source` | `feed_schema`=`unirouter.completed-execution.v1`; `cursor_key`=`unirouter.completed-execution.v1`; `required_source_state`=`paused` |
| `register_receiver` | `source_instance`; positive integer `consumer_epoch`; `checkpoint_authority_sha256`; exact six-field `receiver_registration`; exact `network_binding`; `required_source_state`=`paused` |
| `resume_source` | `source_instance`; `receiver_registration_sha256`; `network_binding_sha256`; `paused_auth_receipt_sha256`; `dormant_credential_receipt_sha256`; `required_source_state`=`active`; `required_receiver_state`=`active`; `readiness`=`paused_receiver_authenticated` |

`receiver_registration` has exactly `schema_version`, `token_format`,
`verifier_scheme`, `key_prefix`, `verifier_sha256`, and `operation_id`. It is
public verifier metadata only. `network_binding` has exactly `transport=https`,
`route=/api/v1/integrations/xinsight/completed-executions`, and
`binding_sha256`; it never carries an operator-supplied URL.

## Canonical terminal result

Every result has exactly `schema_version`, `operation_id`, `installation_id`,
`phase`, `status`, `result` for `completed` or `reason_code` for `failed`, and
`observed_at`. The schema is `xinsight.connector-control-result.v2`.

| phase | exact completed `result` fields |
| --- | --- |
| `prepare_paused_source` | `source_instance`; non-empty `cursor`; equal non-empty `high_watermark`; `source_state`=`paused`; `readiness`=`paused_source_cursor_ready` |
| `register_receiver` | same `source_instance`; `receiver_registration_sha256` equal to the canonical exact six-field request object; `network_binding_sha256` equal to request `network_binding.binding_sha256`; `source_state`=`paused`; `receiver_state`=`feed_staged`; `readiness`=`paused_receiver_registered` |
| `resume_source` | same `source_instance`; `source_state`=`active`; `receiver_state`=`active`; `readiness`=`source_resumed_receiver_active` |

The receiver bearer is never a field. Cursor/high-watermark appear only in the
phase-1 management result and the X checkpoint lifecycle, not in UI or evidence.
Phase 2 must reuse phase 1's exact Source. Phase 3 must reuse phase 2's exact
Source, receiver-registration digest, and network-binding digest.
