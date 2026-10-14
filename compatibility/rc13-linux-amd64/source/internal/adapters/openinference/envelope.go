package openinference

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/foigent/x-insight/internal/connector"
)

func BuildEnvelope(
	source SanitizedRecord,
	context MappingContext,
	binding connector.SourceNeutralManifestBinding,
) ([]byte, connector.ValidatedEnvelope, *connector.ConnectorError) {
	if err := validateBinding(binding); err != nil {
		return nil, connector.ValidatedEnvelope{}, adapterError(
			connector.ErrorConfiguration,
			"foigent.openinference.binding_invalid",
			connector.PhaseValidate,
			"validate_binding",
			"OpenInference projection binding is invalid",
		)
	}
	if err := validateMappingContext(context); err != nil {
		return nil, connector.ValidatedEnvelope{}, adapterError(
			connector.ErrorConfiguration,
			"foigent.openinference.mapping_context_invalid",
			connector.PhaseValidate,
			"validate_mapping_context",
			"OpenInference mapping context is invalid",
		)
	}
	recomputed, err := canonicalSourceRecord(source.Record)
	if err != nil ||
		!bytes.Equal(recomputed.CanonicalBytes, source.CanonicalBytes) ||
		recomputed.SourceHash != source.SourceHash {
		return nil, connector.ValidatedEnvelope{}, adapterError(
			connector.ErrorContract,
			"foigent.openinference.source_identity_mismatch",
			connector.PhaseValidate,
			"validate_source",
			"OpenInference Source record identity is invalid",
		)
	}
	tenant := ""
	if context.TenantRef != nil {
		tenant = *context.TenantRef
	}
	eventID := source.Record.TraceID + ":" + source.Record.SpanID
	idempotency, err := connector.EventIdempotencyKey(
		"openinference",
		context.ProducerInstance,
		tenant,
		context.OrgRef,
		context.ProjectRef,
		"observability.span.completed",
		eventID,
	)
	if err != nil {
		return nil, connector.ValidatedEnvelope{}, adapterError(
			connector.ErrorContract,
			"foigent.openinference.identity_invalid",
			connector.PhaseValidate,
			"build_identity",
			"OpenInference Event identity is invalid",
		)
	}
	scope := map[string]any{
		"org_ref": context.OrgRef, "project_ref": context.ProjectRef,
	}
	if context.TenantRef != nil {
		scope["tenant_ref"] = *context.TenantRef
	}
	actor := map[string]any{"type": "system"}
	if source.Record.ActorRef != "" {
		actor = map[string]any{"type": "user", "ref": source.Record.ActorRef}
	}
	correlation := map[string]any{
		"request_ref": eventID,
		"trace_ref":   source.Record.TraceID,
		"span_ref":    source.Record.SpanID,
	}
	usage := map[string]any{}
	if source.Record.PromptTokens != "" {
		usage["prompt_tokens"] = source.Record.PromptTokens
	}
	if source.Record.CompletionTokens != "" {
		usage["completion_tokens"] = source.Record.CompletionTokens
	}
	if source.Record.TotalTokens != "" {
		usage["total_tokens"] = source.Record.TotalTokens
	}
	trace := map[string]any{
		"trace_id": source.Record.TraceID,
		"span_id":  source.Record.SpanID,
	}
	if source.Record.ParentSpanID != "" {
		trace["parent_span_id"] = source.Record.ParentSpanID
	}
	if source.Record.SessionID != "" {
		trace["session_id"] = source.Record.SessionID
	}
	model := map[string]any{"name": source.Record.Model}
	if source.Record.Provider != "" {
		model["provider"] = source.Record.Provider
	}
	payload := map[string]any{
		"terminal":          map[string]any{"status": source.Record.Status},
		"span":              map[string]any{"kind": source.Record.SpanKind, "name": source.Record.SpanName},
		"trace":             trace,
		"model":             model,
		"usage":             usage,
		"timeline_complete": false,
		"capture_gaps":      captureGapClaims(source.Record),
	}
	if source.Record.ServiceName != "" {
		payload["service"] = map[string]any{"name": source.Record.ServiceName}
	}
	hashInput := map[string]any{
		"spec_version":   connector.EventEnvelopeVersionV1,
		"event_id":       eventID,
		"event_type":     "observability.span.completed",
		"schema_ref":     "foigent://events/observability/openinference-span-completed/v1",
		"schema_version": "1",
		"producer": map[string]any{
			"product":           "openinference",
			"instance":          context.ProducerInstance,
			"connector_id":      ConnectorID,
			"connector_version": ConnectorVersion,
		},
		"scope":       scope,
		"subject":     map[string]any{"type": "span", "ref": source.Record.SpanID},
		"actor":       actor,
		"time":        map[string]any{"occurred_at": source.Record.EndTime, "terminal_at": source.Record.EndTime},
		"correlation": correlation,
		"identity": map[string]any{
			"idempotency_key": idempotency,
			"source_revision": "1",
		},
		"integrity": map[string]any{"source_hash": source.SourceHash},
		"data_policy": map[string]any{
			"payload_capture_mode": "metadata_only",
			"raw_payload_present":  false,
			"redaction_profile":    "openinference.metadata-allowlist.v1",
		},
		"provenance": map[string]any{
			"adapter_fidelity": "F2",
			"field_groups": map[string]any{
				"actor":                     proof(source.Record.ActorRef != ""),
				"correlation":               "direct",
				"data_policy":               "declared",
				"identity":                  "derived",
				"payload.capture_gaps":      "derived",
				"payload.model":             "direct",
				"payload.span":              "direct",
				"payload.timeline_complete": "declared",
				"payload.trace":             "direct",
				"payload.usage":             proof(len(usage) > 0),
				"scope":                     "declared",
				"subject":                   "derived",
				"time.occurred_at":          "derived",
				"time.terminal_at":          "direct",
			},
		},
		"payload": payload,
	}
	return connector.BuildCanonicalEnvelope(hashInput, binding)
}

func captureGapClaims(record SourceRecord) []any {
	claims := []map[string]string{
		{"affected_claim": "billing_authority", "reason_code": "not_exposed_by_openinference_span_v1"},
		{"affected_claim": "business_outcome", "reason_code": "not_exposed_by_openinference_span_v1"},
		{"affected_claim": "downstream_side_effects", "reason_code": "not_exposed_by_openinference_span_v1"},
		{"affected_claim": "routing_decision", "reason_code": "not_exposed_by_openinference_span_v1"},
	}
	if record.SessionID == "" {
		claims = append(claims, map[string]string{
			"affected_claim": "authoritative_session",
			"reason_code":    "session_id_not_observed",
		})
	}
	if record.PromptTokens == "" || record.CompletionTokens == "" || record.TotalTokens == "" {
		claims = append(claims, map[string]string{
			"affected_claim": "token_usage",
			"reason_code":    "complete_token_counters_not_observed",
		})
	}
	if record.MissingParent {
		claims = append(claims, map[string]string{
			"affected_claim": "causal_parent",
			"reason_code":    "declared_parent_not_in_bounded_batch",
		})
	}
	sortGapClaims(claims)
	output := make([]any, 0, len(claims))
	for _, claim := range claims {
		output = append(output, map[string]any{
			"affected_claim": claim["affected_claim"],
			"reason_code":    claim["reason_code"],
			"gap_type":       "not_available_from_source",
			"severity":       "warning",
		})
	}
	return output
}

func sortGapClaims(claims []map[string]string) {
	for left := 0; left < len(claims); left++ {
		for right := left + 1; right < len(claims); right++ {
			if claims[right]["affected_claim"] < claims[left]["affected_claim"] {
				claims[left], claims[right] = claims[right], claims[left]
			}
		}
	}
}

func proof(observed bool) string {
	if observed {
		return "direct"
	}
	return "gap"
}

func validateMappingContext(context MappingContext) error {
	if !validOpaque(context.ProducerInstance, 1, 128) ||
		!positiveDecimal(context.OrgRef) ||
		!positiveDecimal(context.ProjectRef) ||
		(context.TenantRef != nil && !validOpaque(*context.TenantRef, 1, 128)) {
		return errors.New("OpenInference mapping context is invalid")
	}
	return nil
}

func positiveDecimal(value string) bool {
	if value == "" || len(value) > 20 || value[0] == '0' {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func sourceReference(producerInstance, traceID, spanID string) string {
	return "openinference:v1:instance:" +
		base64.RawURLEncoding.EncodeToString([]byte(producerInstance)) +
		":trace:" + traceID + ":span:" + spanID
}

func adapterError(
	category connector.ErrorCategory,
	code string,
	phase connector.ErrorPhase,
	operation string,
	message string,
) *connector.ConnectorError {
	return connector.NewConnectorError(
		category, code, phase, operation, false, message, connector.RecoveryOperatorRepair,
	)
}

func invalidEnvelope(message string) error {
	return fmt.Errorf("OpenInference Envelope semantics are invalid: %s", message)
}
