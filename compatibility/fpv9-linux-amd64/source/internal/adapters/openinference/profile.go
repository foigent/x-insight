package openinference

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/evidence"
	"github.com/foigent/x-insight/internal/investigation"
)

type Profile struct {
	binding connector.SourceNeutralManifestBinding
}

func NewProfile(binding connector.SourceNeutralManifestBinding) (*Profile, error) {
	if err := validateBinding(binding); err != nil {
		return nil, err
	}
	return &Profile{binding: binding}, nil
}

func (profile *Profile) ProfileKey() connector.ProfileKey {
	if profile == nil {
		return connector.ProfileKey{}
	}
	return connector.ProfileKey{
		ProfileID:     profile.binding.Projection.ProfileID,
		EventType:     profile.binding.Connector.EventType,
		SchemaRef:     profile.binding.Connector.SchemaRef,
		SchemaVersion: profile.binding.Connector.SchemaVersion,
	}
}

func (profile *Profile) Normalize(
	ctx context.Context,
	envelope connector.ValidatedEnvelope,
	projectionContext connector.ProjectionContext,
) (connector.NormalizedItem, *connector.ConnectorError) {
	if profile == nil {
		return connector.NormalizedItem{}, adapterError(
			connector.ErrorInternal,
			"foigent.openinference.profile_uninitialized",
			connector.PhaseNormalize,
			"normalize_item",
			"OpenInference profile is unavailable",
		)
	}
	if err := ctx.Err(); err != nil {
		return connector.NormalizedItem{}, connector.NewConnectorError(
			connector.ErrorInternal,
			"foigent.openinference.projection_interrupted",
			connector.PhaseNormalize,
			"normalize_item",
			true,
			"OpenInference projection was interrupted",
			connector.RecoveryAutomaticRetry,
		)
	}
	if envelope.EventType != profile.binding.Connector.EventType ||
		envelope.SchemaRef != profile.binding.Connector.SchemaRef ||
		envelope.SchemaVersion != profile.binding.Connector.SchemaVersion ||
		envelope.ConnectorID != profile.binding.Connector.ConnectorID ||
		envelope.ConnectorVersion != profile.binding.Connector.ConnectorVersion ||
		envelope.ProducerProduct != "openinference" {
		return connector.NormalizedItem{}, adapterError(
			connector.ErrorContract,
			"foigent.openinference.binding_mismatch",
			connector.PhaseNormalize,
			"normalize_item",
			"Event Envelope does not match the OpenInference profile",
		)
	}
	facts, err := envelopeFactsFrom(envelope)
	if err != nil {
		return connector.NormalizedItem{}, adapterError(
			connector.ErrorContract,
			"foigent.openinference.envelope_semantics_invalid",
			connector.PhaseNormalize,
			"normalize_item",
			"OpenInference Event Envelope semantics are invalid",
		)
	}
	ingestedAt := projectionContext.IngestedAt.UTC()
	if ingestedAt.IsZero() {
		ingestedAt = time.Now().UTC()
	}
	events, err := buildM0Events(profile.binding, envelope, facts, ingestedAt)
	if err != nil {
		return connector.NormalizedItem{}, adapterError(
			connector.ErrorContract,
			"foigent.openinference.m0_projection_failed",
			connector.PhaseNormalize,
			"normalize_item",
			"OpenInference M0 projection failed",
		)
	}
	terminal := facts.TerminalAt
	item := connector.NormalizedItem{
		ProfileID: profile.binding.Projection.ProfileID,
		Envelope:  envelope,
		Events:    events,
		Provenance: investigation.ImportProvenance{
			SourceSystemID:      profile.binding.Projection.SourceSystemID,
			AdapterManifestID:   profile.binding.Projection.AdapterManifestID,
			IntegrationMode:     investigation.IntegrationBoundedOffline,
			ProofClass:          investigation.ProofOperatorApproved,
			AssuranceMode:       investigation.AssuranceBoundedOffline,
			SourcePayloadSHA256: envelope.SourceHash,
			SourceTerminalAt:    &terminal,
			SourceRevision:      envelope.SourceRevision,
			ImportedRevision:    "1",
			TimelineComplete:    false,
		},
	}
	if err := connector.ValidateNormalizedItem(item); err != nil {
		return connector.NormalizedItem{}, adapterError(
			connector.ErrorContract,
			"foigent.openinference.normalized_item_invalid",
			connector.PhaseNormalize,
			"normalize_item",
			"OpenInference normalized Evidence item is invalid",
		)
	}
	return item, nil
}

type envelopeFacts struct {
	EventID          string
	Status           string
	TerminalAt       time.Time
	TraceID          string
	SpanID           string
	ParentSpanID     string
	SessionID        string
	SpanName         string
	Model            string
	Provider         string
	PromptTokens     *int64
	CompletionTokens *int64
	TotalTokens      *int64
	ServiceName      string
	ActorType        string
	ActorRef         string
	Gaps             []gapFact
}

type gapFact struct {
	AffectedClaim string
	ReasonCode    string
}

func envelopeFactsFrom(envelope connector.ValidatedEnvelope) (envelopeFacts, error) {
	var raw map[string]any
	if err := json.Unmarshal(envelope.CanonicalBytes(), &raw); err != nil {
		return envelopeFacts{}, err
	}
	timeGroup, _ := raw["time"].(map[string]any)
	occurred, _ := timeGroup["occurred_at"].(string)
	terminal, _ := timeGroup["terminal_at"].(string)
	terminalAt, err := time.Parse(microsecondTimeLayout, terminal)
	if err != nil || occurred != terminal {
		return envelopeFacts{}, invalidEnvelope("terminal time")
	}
	actor, _ := raw["actor"].(map[string]any)
	actorType, _ := actor["type"].(string)
	actorRef, _ := actor["ref"].(string)
	payload := envelope.Payload()
	terminalGroup, ok := payload["terminal"].(map[string]any)
	if !ok {
		return envelopeFacts{}, invalidEnvelope("terminal")
	}
	status, _ := terminalGroup["status"].(string)
	if status != "succeeded" && status != "failed" {
		return envelopeFacts{}, invalidEnvelope("terminal status")
	}
	span, _ := payload["span"].(map[string]any)
	if span["kind"] != "LLM" {
		return envelopeFacts{}, invalidEnvelope("span kind")
	}
	spanName, _ := span["name"].(string)
	trace, _ := payload["trace"].(map[string]any)
	traceID, _ := trace["trace_id"].(string)
	spanID, _ := trace["span_id"].(string)
	parentSpanID, _ := trace["parent_span_id"].(string)
	sessionID, _ := trace["session_id"].(string)
	modelGroup, _ := payload["model"].(map[string]any)
	model, _ := modelGroup["name"].(string)
	provider, _ := modelGroup["provider"].(string)
	if !traceIDPattern.MatchString(traceID) ||
		!spanIDPattern.MatchString(spanID) ||
		(parentSpanID != "" && !spanIDPattern.MatchString(parentSpanID)) ||
		!validOpaque(spanName, 1, 240) ||
		!validOpaque(model, 1, 240) {
		return envelopeFacts{}, invalidEnvelope("trace or model")
	}
	usage, _ := payload["usage"].(map[string]any)
	prompt, err := optionalCounter(usage, "prompt_tokens")
	if err != nil {
		return envelopeFacts{}, err
	}
	completion, err := optionalCounter(usage, "completion_tokens")
	if err != nil {
		return envelopeFacts{}, err
	}
	total, err := optionalCounter(usage, "total_tokens")
	if err != nil {
		return envelopeFacts{}, err
	}
	if prompt != nil && completion != nil && total != nil &&
		(*prompt > *total || *completion > *total-*prompt || *prompt+*completion != *total) {
		return envelopeFacts{}, invalidEnvelope("token counters")
	}
	serviceName := ""
	if service, ok := payload["service"].(map[string]any); ok {
		serviceName, _ = service["name"].(string)
	}
	rawGaps, ok := payload["capture_gaps"].([]any)
	if !ok || len(rawGaps) < 4 {
		return envelopeFacts{}, invalidEnvelope("capture gaps")
	}
	gaps := make([]gapFact, 0, len(rawGaps))
	seen := map[string]struct{}{}
	for _, rawGap := range rawGaps {
		gap, ok := rawGap.(map[string]any)
		if !ok || gap["gap_type"] != "not_available_from_source" || gap["severity"] != "warning" {
			return envelopeFacts{}, invalidEnvelope("capture gap shape")
		}
		claim, _ := gap["affected_claim"].(string)
		reason, _ := gap["reason_code"].(string)
		if claim == "" || reason == "" {
			return envelopeFacts{}, invalidEnvelope("capture gap identity")
		}
		if _, duplicate := seen[claim]; duplicate {
			return envelopeFacts{}, invalidEnvelope("duplicate capture gap")
		}
		seen[claim] = struct{}{}
		gaps = append(gaps, gapFact{AffectedClaim: claim, ReasonCode: reason})
	}
	if payload["timeline_complete"] != false {
		return envelopeFacts{}, invalidEnvelope("timeline completeness")
	}
	return envelopeFacts{
		EventID: envelope.EventID, Status: status, TerminalAt: terminalAt,
		TraceID: traceID, SpanID: spanID, ParentSpanID: parentSpanID, SessionID: sessionID,
		SpanName: spanName, Model: model, Provider: provider,
		PromptTokens: prompt, CompletionTokens: completion, TotalTokens: total,
		ServiceName: serviceName, ActorType: actorType, ActorRef: actorRef, Gaps: gaps,
	}, nil
}

func optionalCounter(group map[string]any, key string) (*int64, error) {
	value, exists := group[key]
	if !exists {
		return nil, nil
	}
	text, ok := value.(string)
	if !ok {
		return nil, invalidEnvelope("counter encoding")
	}
	parsed, err := parseCanonicalInt64(text)
	if err != nil {
		return nil, invalidEnvelope("counter range")
	}
	return &parsed, nil
}

func buildM0Events(
	binding connector.SourceNeutralManifestBinding,
	envelope connector.ValidatedEnvelope,
	facts envelopeFacts,
	ingestedAt time.Time,
) ([]map[string]any, error) {
	occurred := facts.TerminalAt.Format(time.RFC3339Nano)
	ingested := ingestedAt.Format(time.RFC3339Nano)
	sourceRef := sourceReference(envelope.ProducerInstance, facts.TraceID, facts.SpanID)
	eventType := "model.request.succeeded"
	severity := "info"
	outcome := "success"
	if facts.Status == "failed" {
		eventType = "model.request.failed"
		severity = "error"
		outcome = "failure"
	}
	main := baseM0Event(binding, envelope, sourceRef, facts.SpanID, "openinference_llm_span", eventType, "provider", severity, occurred, ingested)
	main["request_id"] = facts.EventID
	main["trace_id"] = facts.TraceID
	main["span_id"] = facts.SpanID
	main["org_ref"] = "org-" + envelope.Scope.OrgRef
	main["project_ref"] = "project-" + envelope.Scope.ProjectRef
	main["correlation"] = compact(map[string]any{
		"request_id":     facts.EventID,
		"trace_id":       facts.TraceID,
		"span_id":        facts.SpanID,
		"parent_span_id": facts.ParentSpanID,
		"session_id":     facts.SessionID,
	})
	m0ActorType := facts.ActorType
	if m0ActorType == "user" {
		m0ActorType = "human"
	}
	main["identity"] = compact(map[string]any{
		"actor_type": m0ActorType,
		"actor_ref":  facts.ActorRef,
		"org_id":     "org-" + envelope.Scope.OrgRef,
		"project_id": "project-" + envelope.Scope.ProjectRef,
	})
	main["capability"] = compact(map[string]any{
		"capability_type": "model_call",
		"target_ref":      facts.SpanName,
		"model":           facts.Model,
		"extensions": compact(map[string]any{
			"provider":     facts.Provider,
			"service_name": facts.ServiceName,
			"span_kind":    "LLM",
		}),
	})
	usage := map[string]any{
		"usage_source":     "openinference_span",
		"usage_confidence": "source_reported",
	}
	if facts.PromptTokens != nil {
		usage["prompt_tokens"] = *facts.PromptTokens
	}
	if facts.CompletionTokens != nil {
		usage["completion_tokens"] = *facts.CompletionTokens
	}
	if facts.TotalTokens != nil {
		usage["total_tokens"] = *facts.TotalTokens
	}
	main["usage"] = usage
	main["outcome"] = map[string]any{"outcome_status": outcome}
	main["payload_policy"] = map[string]any{
		"payload_capture_mode": "metadata_only",
		"redaction_state":      "not_applicable",
	}
	main["field_provenance"] = map[string]any{
		"correlation": "direct", "identity": proof(facts.ActorRef != ""),
		"capability": "direct", "decision": "gap",
		"usage": proof(facts.TotalTokens != nil), "outcome": "direct",
		"payload_policy": "declared",
	}
	main["raw_source_excerpt"] = compact(map[string]any{
		"span_kind": "LLM", "span_name": facts.SpanName, "model": facts.Model,
		"provider": facts.Provider, "service_name": facts.ServiceName,
	})
	events := []map[string]any{main}
	for _, gapFact := range facts.Gaps {
		gapSourceID := facts.SpanID + ":gap:" + gapFact.AffectedClaim
		gap := baseM0Event(
			binding, envelope, sourceRef+":gap:"+gapFact.AffectedClaim,
			gapSourceID, "openinference_capture_gap",
			"gap.capture.detected", "gap", "warning", occurred, ingested,
		)
		gap["request_id"] = facts.EventID
		gap["trace_id"] = facts.TraceID
		gap["span_id"] = facts.SpanID
		gap["org_ref"] = "org-" + envelope.Scope.OrgRef
		gap["project_ref"] = "project-" + envelope.Scope.ProjectRef
		gap["payload_capture_mode"] = "none"
		gap["correlation"] = compact(map[string]any{
			"request_id": facts.EventID, "trace_id": facts.TraceID, "span_id": facts.SpanID,
		})
		gap["payload_policy"] = map[string]any{
			"payload_capture_mode": "none", "redaction_state": "not_applicable",
		}
		gap["field_provenance"] = map[string]any{
			"correlation": "derived", "payload_policy": "declared",
			"extensions": map[string]any{"capture_gap": "derived"},
		}
		gap["raw_source_excerpt"] = map[string]any{
			"capture_gap":  "not_available_from_source",
			"gap_source":   gapFact.AffectedClaim,
			"reason":       gapFact.ReasonCode,
			"parent_event": main["id"],
		}
		events = append(events, gap)
	}
	for _, event := range events {
		hash, err := evidence.GenerateEventHash(event)
		if err != nil {
			return nil, err
		}
		event["event_hash"] = hash
	}
	return events, nil
}

func baseM0Event(
	binding connector.SourceNeutralManifestBinding,
	envelope connector.ValidatedEnvelope,
	sourceRef string,
	sourceEventID string,
	sourceEventType string,
	eventType string,
	stage string,
	severity string,
	occurredAt string,
	ingestedAt string,
) map[string]any {
	seed := strings.Join([]string{
		"xinsight-openinference-event-v1",
		binding.Projection.SourceSystemID,
		binding.Projection.AdapterManifestID,
		sourceRef,
		eventType,
		sourceEventID,
	}, "\x00")
	eventID := deterministicUUID(seed)
	return map[string]any{
		"id": eventID, "schema_version": "m0.1",
		"source_system_id":    binding.Projection.SourceSystemID,
		"adapter_manifest_id": binding.Projection.AdapterManifestID,
		"source_type":         "openinference", "source_version": SourceSchemaV1,
		"source_event_id": sourceEventID, "source_event_type": sourceEventType,
		"source_event_hash": envelope.SourceHash, "source_ref": sourceRef,
		"idempotency_key": fmt.Sprintf(
			"adapter-openinference-v1:%s:%s",
			binding.Projection.SourceSystemID, eventID,
		),
		"import_cursor": sourceEventID, "event_type": eventType,
		"stage": stage, "severity": severity, "capture_mode": IngestionCaptureMode,
		"payload_capture_mode": "metadata_only",
		"occurred_at":          occurredAt, "observed_at": occurredAt, "ingested_at": ingestedAt,
	}
}

func deterministicUUID(input string) string {
	sum := sha256.Sum256([]byte(input))
	value := append([]byte(nil), sum[:16]...)
	value[6] = (value[6] & 0x0f) | 0x50
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%s-%s-%s-%s-%s",
		hex.EncodeToString(value[0:4]), hex.EncodeToString(value[4:6]),
		hex.EncodeToString(value[6:8]), hex.EncodeToString(value[8:10]),
		hex.EncodeToString(value[10:16]),
	)
}

func compact(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		switch typed := value.(type) {
		case nil:
			continue
		case string:
			if typed == "" {
				continue
			}
		case map[string]any:
			if len(typed) == 0 {
				continue
			}
		}
		output[key] = value
	}
	return output
}

var _ connector.ItemNormalizer = (*Profile)(nil)

var errInvalidOpenInferenceEnvelope = errors.New("invalid OpenInference Envelope")
