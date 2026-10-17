package litellm

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
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
	binding connector.ManifestBinding
}

func NewProfile(binding connector.ManifestBinding) (*Profile, error) {
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
		return connector.NormalizedItem{}, profileError(
			"foigent.litellm.profile_uninitialized",
			"LiteLLM projection profile is unavailable",
		)
	}
	if err := ctx.Err(); err != nil {
		return connector.NormalizedItem{}, connector.NewConnectorError(
			connector.ErrorInternal,
			"foigent.litellm.projection_interrupted",
			connector.PhaseNormalize,
			"normalize_item",
			true,
			"LiteLLM projection was interrupted",
			connector.RecoveryAutomaticRetry,
		)
	}
	key := profile.ProfileKey()
	if envelope.EventType != key.EventType ||
		envelope.SchemaRef != key.SchemaRef ||
		envelope.SchemaVersion != key.SchemaVersion ||
		envelope.ConnectorID != profile.binding.Connector.ConnectorID ||
		envelope.ConnectorVersion != profile.binding.Connector.ConnectorVersion ||
		envelope.ProducerProduct != "litellm" {
		return connector.NormalizedItem{}, profileError(
			"foigent.litellm.binding_mismatch",
			"Event Envelope does not match the LiteLLM profile",
		)
	}
	facts, err := parseEnvelopeFacts(envelope)
	if err != nil {
		return connector.NormalizedItem{}, profileError(
			"foigent.litellm.envelope_semantics_invalid",
			"LiteLLM Event Envelope semantics are invalid",
		)
	}
	ingestedAt := projectionContext.IngestedAt.UTC()
	if ingestedAt.IsZero() {
		ingestedAt = time.Now().UTC()
	}
	events, err := buildM0Events(profile.binding, envelope, facts, ingestedAt)
	if err != nil {
		return connector.NormalizedItem{}, profileError(
			"foigent.litellm.m0_projection_failed",
			"LiteLLM M0 projection failed",
		)
	}
	terminal := facts.TerminalAt.UTC()
	item := connector.NormalizedItem{
		ProfileID: profile.binding.Projection.ProfileID,
		Envelope:  envelope,
		Events:    events,
		Provenance: investigation.ImportProvenance{
			SourceSystemID:      profile.binding.Projection.SourceSystemID,
			AdapterManifestID:   profile.binding.Projection.AdapterManifestID,
			IntegrationMode:     investigation.IntegrationConnectorPull,
			ProofClass:          investigation.ProofOperatorApproved,
			AssuranceMode:       investigation.AssuranceContractedPull,
			SourcePayloadSHA256: envelope.SourceHash,
			SourceTerminalAt:    &terminal,
			SourceRevision:      envelope.SourceRevision,
			ImportedRevision:    "1",
			TimelineComplete:    true,
		},
	}
	if err := connector.ValidateNormalizedItem(item); err != nil {
		return connector.NormalizedItem{}, profileError(
			"foigent.litellm.normalized_item_invalid",
			"LiteLLM normalized Evidence item is invalid",
		)
	}
	return item, nil
}

type envelopeFacts struct {
	RequestID        string
	Status           string
	TerminalAt       time.Time
	Model            string
	ModelGroup       string
	Provider         string
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
	CacheStatus      string
	ReportedSpend    string
	GapClaims        []string
}

func parseEnvelopeFacts(envelope connector.ValidatedEnvelope) (envelopeFacts, error) {
	payload := envelope.Payload()
	if !exactKeys(payload, []string{
		"terminal", "usage", "reported_money", "timeline_complete", "capture_gaps",
	}, nil) {
		return envelopeFacts{}, errors.New("payload shape is invalid")
	}
	terminal, ok := payload["terminal"].(map[string]any)
	if !ok || !exactKeys(terminal, []string{"status"}, nil) {
		return envelopeFacts{}, errors.New("terminal shape is invalid")
	}
	status, ok := terminal["status"].(string)
	if !ok || (status != "succeeded" && status != "failed") {
		return envelopeFacts{}, errors.New("terminal status is invalid")
	}
	usage, ok := payload["usage"].(map[string]any)
	if !ok || !exactKeys(usage, []string{
		"model", "prompt_tokens", "completion_tokens", "total_tokens",
		"source", "authority", "finality", "contract_version", "cache_status",
	}, []string{"model_group", "provider"}) {
		return envelopeFacts{}, errors.New("usage shape is invalid")
	}
	model, _ := usage["model"].(string)
	promptText, _ := usage["prompt_tokens"].(string)
	completionText, _ := usage["completion_tokens"].(string)
	totalText, _ := usage["total_tokens"].(string)
	cacheStatus, _ := usage["cache_status"].(string)
	prompt, promptErr := parseCanonicalInt64(promptText)
	completion, completionErr := parseCanonicalInt64(completionText)
	total, totalErr := parseCanonicalInt64(totalText)
	if !validOpaque(model, 1, 200) ||
		promptErr != nil || completionErr != nil || totalErr != nil ||
		prompt > total || completion > total-prompt || prompt+completion != total ||
		usage["source"] != "litellm_spend_log" ||
		usage["authority"] != "source_reported" ||
		usage["finality"] != "finalized_after_delay" ||
		usage["contract_version"] != SourceSchemaV1 ||
		(cacheStatus != "hit" && cacheStatus != "miss" && cacheStatus != "unknown") {
		return envelopeFacts{}, errors.New("usage semantics are invalid")
	}
	modelGroup, _ := usage["model_group"].(string)
	provider, _ := usage["provider"].(string)
	if (modelGroup != "" && !validOpaque(modelGroup, 1, 200)) ||
		(provider != "" && !validOpaque(provider, 1, 100)) {
		return envelopeFacts{}, errors.New("usage opaque fields are invalid")
	}
	money, ok := payload["reported_money"].(map[string]any)
	if !ok || !exactKeys(money, []string{
		"schema", "currency", "amount", "source", "authority",
	}, nil) {
		return envelopeFacts{}, errors.New("reported_money shape is invalid")
	}
	spend, _ := money["amount"].(string)
	canonicalSpend, _, validSpend := CanonicalReportedDecimal(spend)
	if money["schema"] != "decimal-currency.v1" ||
		money["currency"] != "USD" ||
		money["source"] != "litellm_spend_log" ||
		money["authority"] != "source_reported" ||
		!validSpend || canonicalSpend != spend ||
		payload["timeline_complete"] != true {
		return envelopeFacts{}, errors.New("reported_money semantics are invalid")
	}
	terminalAt, err := parseEnvelopeTerminalTime(envelope)
	if err != nil {
		return envelopeFacts{}, err
	}
	gaps, ok := payload["capture_gaps"].([]any)
	if !ok || len(gaps) != 4 {
		return envelopeFacts{}, errors.New("capture_gaps shape is invalid")
	}
	expectedClaims := []string{
		"billing_authority",
		"business_outcome",
		"downstream_side_effects",
		"routing_decision",
	}
	for index, rawGap := range gaps {
		gap, ok := rawGap.(map[string]any)
		if !ok || !validateGap(gap, envelope, expectedClaims[index], terminalAt) {
			return envelopeFacts{}, errors.New("capture_gap semantics are invalid")
		}
	}
	return envelopeFacts{
		RequestID:        envelope.EventID,
		Status:           status,
		TerminalAt:       terminalAt,
		Model:            model,
		ModelGroup:       modelGroup,
		Provider:         provider,
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      total,
		CacheStatus:      cacheStatus,
		ReportedSpend:    spend,
		GapClaims:        expectedClaims,
	}, nil
}

func buildM0Events(
	binding connector.ManifestBinding,
	envelope connector.ValidatedEnvelope,
	facts envelopeFacts,
	ingestedAt time.Time,
) ([]map[string]any, error) {
	occurredAt := facts.TerminalAt.UTC().Format(time.RFC3339Nano)
	ingested := ingestedAt.UTC().Format(time.RFC3339Nano)
	orgRef := "org-" + envelope.Scope.OrgRef
	projectRef := "project-" + envelope.Scope.ProjectRef
	mainSourceRef := litellmSourceReference(
		envelope.ProducerInstance,
		facts.RequestID,
	)
	eventType := "model.request.succeeded"
	severity := "info"
	outcomeStatus := "success"
	if facts.Status == "failed" {
		eventType = "model.request.failed"
		severity = "warning"
		outcomeStatus = "failure"
	}
	main := baseM0Event(
		binding,
		envelope,
		mainSourceRef,
		facts.RequestID,
		"litellm_spend_log",
		eventType,
		"usage",
		severity,
		occurredAt,
		ingested,
	)
	main["request_id"] = facts.RequestID
	main["org_ref"] = orgRef
	main["project_ref"] = projectRef
	main["payload_capture_mode"] = "metadata_only"
	main["correlation"] = map[string]any{"request_id": facts.RequestID}
	main["identity"] = map[string]any{
		"org_id":     orgRef,
		"project_id": projectRef,
		"actor_type": "system",
	}
	capabilityExtensions := map[string]any{}
	if facts.ModelGroup != "" {
		capabilityExtensions["model_group"] = facts.ModelGroup
	}
	if facts.Provider != "" {
		capabilityExtensions["provider"] = facts.Provider
	}
	main["capability"] = compact(map[string]any{
		"capability_type": "model_call",
		"model":           facts.Model,
		"extensions":      capabilityExtensions,
	})
	main["usage"] = map[string]any{
		"prompt_tokens":     facts.PromptTokens,
		"completion_tokens": facts.CompletionTokens,
		"total_tokens":      facts.TotalTokens,
		"usage_source":      "litellm_spend_log",
		"usage_confidence":  "source_reported",
		"extensions": map[string]any{
			"cache_status": facts.CacheStatus,
			"source_reported_money": map[string]any{
				"schema":    "decimal-currency.v1",
				"currency":  "USD",
				"amount":    facts.ReportedSpend,
				"source":    "litellm_spend_log",
				"authority": "source_reported",
			},
		},
	}
	main["outcome"] = map[string]any{
		"outcome_status": outcomeStatus,
		"extensions":     map[string]any{"terminal_status": facts.Status},
	}
	main["payload_policy"] = map[string]any{
		"payload_capture_mode": "metadata_only",
		"redaction_state":      "not_applicable",
	}
	main["field_provenance"] = map[string]any{
		"correlation":    "direct",
		"identity":       "derived",
		"capability":     "direct",
		"usage":          "direct",
		"outcome":        "direct",
		"payload_policy": "declared",
	}
	main["raw_source_excerpt"] = compact(map[string]any{
		"terminal_status": facts.Status,
		"usage_source":    "litellm_spend_log",
		"cache_status":    facts.CacheStatus,
		"model_group":     facts.ModelGroup,
		"provider":        facts.Provider,
	})

	events := make([]map[string]any, 0, 5)
	events = append(events, main)
	for _, claim := range facts.GapClaims {
		sourceEventID := facts.RequestID + ":gap:" + claim
		sourceRef := mainSourceRef + ":gap:" + claim
		gap := baseM0Event(
			binding,
			envelope,
			sourceRef,
			sourceEventID,
			"litellm_spend_log_capture_gap",
			"gap.capture.detected",
			"gap",
			"warning",
			occurredAt,
			ingested,
		)
		gap["request_id"] = facts.RequestID
		gap["org_ref"] = orgRef
		gap["project_ref"] = projectRef
		gap["payload_capture_mode"] = "none"
		gap["correlation"] = map[string]any{"request_id": facts.RequestID}
		gap["identity"] = map[string]any{
			"org_id":     orgRef,
			"project_id": projectRef,
			"actor_type": "system",
		}
		gap["payload_policy"] = map[string]any{
			"payload_capture_mode": "none",
			"redaction_state":      "not_applicable",
		}
		gap["field_provenance"] = map[string]any{
			"correlation":    "derived",
			"identity":       "derived",
			"payload_policy": "declared",
			"extensions":     map[string]any{"capture_gap": "declared"},
		}
		gap["raw_source_excerpt"] = map[string]any{
			"capture_gap": "not_available_from_source",
			"gap_source":  claim,
			"reason":      "not_exposed_by_litellm_spend_logs_v1",
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
	binding connector.ManifestBinding,
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
	identitySeed := strings.Join([]string{
		"xinsight-litellm-event-v1",
		binding.Projection.SourceSystemID,
		binding.Projection.AdapterManifestID,
		sourceRef,
		eventType,
		sourceEventID,
	}, "\x00")
	eventID := deterministicUUID(identitySeed)
	return map[string]any{
		"id":                  eventID,
		"schema_version":      "m0.1",
		"source_system_id":    binding.Projection.SourceSystemID,
		"adapter_manifest_id": binding.Projection.AdapterManifestID,
		"source_type":         "litellm",
		"source_version":      SourceSchemaV1,
		"source_event_id":     sourceEventID,
		"source_event_type":   sourceEventType,
		"source_event_hash":   envelope.SourceHash,
		"source_ref":          sourceRef,
		"idempotency_key": fmt.Sprintf(
			"adapter-litellm-v1:%s:%s",
			binding.Projection.SourceSystemID,
			eventID,
		),
		"import_cursor": sourceEventID,
		"event_type":    eventType,
		"stage":         stage,
		"severity":      severity,
		"capture_mode":  "api_import",
		"occurred_at":   occurredAt,
		"observed_at":   occurredAt,
		"ingested_at":   ingestedAt,
	}
}

func litellmSourceReference(producerInstance, requestID string) string {
	return "litellm:v1:instance:" +
		base64.RawURLEncoding.EncodeToString([]byte(producerInstance)) +
		":request:" +
		base64.RawURLEncoding.EncodeToString([]byte(requestID))
}

func parseEnvelopeTerminalTime(envelope connector.ValidatedEnvelope) (time.Time, error) {
	var raw map[string]any
	if err := jsonUnmarshal(envelope.CanonicalBytes(), &raw); err != nil {
		return time.Time{}, err
	}
	timeGroup, ok := raw["time"].(map[string]any)
	if !ok || !exactKeys(timeGroup, []string{"occurred_at", "terminal_at"}, nil) {
		return time.Time{}, errors.New("Envelope time shape is invalid")
	}
	occurred, _ := timeGroup["occurred_at"].(string)
	terminal, _ := timeGroup["terminal_at"].(string)
	if occurred != terminal {
		return time.Time{}, errors.New("Envelope occurred_at is not terminal alias")
	}
	parsed, err := time.Parse(microsecondTimeLayout, terminal)
	if err != nil {
		return time.Time{}, errors.New("Envelope terminal time is invalid")
	}
	return parsed, nil
}

func validateGap(
	gap map[string]any,
	envelope connector.ValidatedEnvelope,
	claim string,
	terminalAt time.Time,
) bool {
	if !exactKeys(gap, []string{
		"gap_id", "parent_event_id", "scope", "correlation", "affected_claim",
		"gap_type", "reason_code", "severity", "observed_at",
		"payload_capture_mode", "field_provenance",
	}, nil) {
		return false
	}
	scope, scopeOK := gap["scope"].(map[string]any)
	correlation, correlationOK := gap["correlation"].(map[string]any)
	provenance, provenanceOK := gap["field_provenance"].(map[string]any)
	expectedProvenance := map[string]string{
		"affected_claim":       "declared",
		"reason_code":          "declared",
		"gap_id":               "derived",
		"parent_event_id":      "derived",
		"scope":                "derived",
		"correlation":          "derived",
		"observed_at":          "derived",
		"gap_type":             "declared",
		"severity":             "declared",
		"payload_capture_mode": "declared",
	}
	if !scopeOK || !correlationOK || !provenanceOK ||
		!gapScopeMatchesEnvelope(scope, envelope.Scope) ||
		correlation["request_ref"] != envelope.EventID ||
		gap["gap_id"] != envelope.EventID+":gap:"+claim ||
		gap["parent_event_id"] != envelope.EventID ||
		gap["affected_claim"] != claim ||
		gap["gap_type"] != "not_available_from_source" ||
		gap["reason_code"] != "not_exposed_by_litellm_spend_logs_v1" ||
		gap["severity"] != "warning" ||
		gap["observed_at"] != terminalAt.Format(microsecondTimeLayout) ||
		gap["payload_capture_mode"] != "none" ||
		len(provenance) != len(expectedProvenance) {
		return false
	}
	for field, value := range expectedProvenance {
		if provenance[field] != value {
			return false
		}
	}
	return true
}

func gapScopeMatchesEnvelope(
	scope map[string]any,
	parent connector.EnvelopeScope,
) bool {
	required := []string{"org_ref", "project_ref"}
	optional := []string{}
	if parent.TenantRef != nil {
		required = append(required, "tenant_ref")
	}
	if !exactKeys(scope, required, optional) ||
		scope["org_ref"] != parent.OrgRef ||
		scope["project_ref"] != parent.ProjectRef {
		return false
	}
	if parent.TenantRef == nil {
		return true
	}
	return scope["tenant_ref"] == *parent.TenantRef
}

func exactKeys(object map[string]any, required, optional []string) bool {
	allowed := make(map[string]struct{}, len(required)+len(optional))
	for _, key := range required {
		allowed[key] = struct{}{}
		if _, exists := object[key]; !exists {
			return false
		}
	}
	for _, key := range optional {
		allowed[key] = struct{}{}
	}
	for key := range object {
		if _, ok := allowed[key]; !ok {
			return false
		}
	}
	return true
}

func deterministicUUID(input string) string {
	sum := sha256.Sum256([]byte(input))
	value := sum[:16]
	value[6] = (value[6] & 0x0f) | 0x50
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%s-%s-%s-%s-%s",
		hex.EncodeToString(value[0:4]),
		hex.EncodeToString(value[4:6]),
		hex.EncodeToString(value[6:8]),
		hex.EncodeToString(value[8:10]),
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

func profileError(code, message string) *connector.ConnectorError {
	return connector.NewConnectorError(
		connector.ErrorContract,
		code,
		connector.PhaseNormalize,
		"normalize_item",
		false,
		message,
		connector.RecoveryOperatorRepair,
	)
}

func jsonUnmarshal(payload []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	return decoder.Decode(target)
}

var _ connector.ItemNormalizer = (*Profile)(nil)
