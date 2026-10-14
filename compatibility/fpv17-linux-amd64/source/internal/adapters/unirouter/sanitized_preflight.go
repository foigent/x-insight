package unirouter

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

const (
	DefaultSanitizedPreflightMaxRequestIDs = 20
	DefaultSanitizedPreflightMaxEvents     = 200
	sanitizedApprovalManifestSchema        = "unirouter.prd5.xinsight.approval.v1"
	strictIPHandling                       = "raw IP addresses are not exported; only ip_address_was_present and ip_address_redacted boolean fields are retained"
	historicalIPHandling                   = "raw IP addresses are not exported; only ip_address_was_present is retained"
)

var (
	lowerHexSHA256Pattern    = regexp.MustCompile(`^(?:sha256:)?[a-f0-9]{64}$`)
	rawLowerHexSHA256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	opaqueTokenPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)
	codeValuePattern         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:=,+-]{0,255}$`)
	identifierValuePattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/=,+-]{0,255}$`)
	emailValuePattern        = regexp.MustCompile(`(?i)[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}`)
	phoneValuePattern        = regexp.MustCompile(`(?:^|[^0-9])1[3-9][0-9]{9}(?:$|[^0-9])`)
	genericPhonePattern      = regexp.MustCompile(`(?:^|[^A-Za-z0-9])(?:\+[0-9]{1,3}[ .-]?)?(?:\([0-9]{2,4}\)[ .-]?|[0-9]{2,4}[ .-])[0-9]{3,4}[ .-][0-9]{4}(?:$|[^A-Za-z0-9])`)
	ipv4ValuePattern         = regexp.MustCompile(`(?:^|[^0-9])(?:(?:25[0-5]|2[0-4][0-9]|1?[0-9]?[0-9])\.){3}(?:25[0-5]|2[0-4][0-9]|1?[0-9]?[0-9])(?:$|[^0-9])`)
	credentialValuePattern   = regexp.MustCompile(
		`(?i)(?:\bbearer\s+|\bbasic\s+|\b(?:api[_-]?key|authorization|password|secret|access[_-]?token|refresh[_-]?token)\s*[:=]|-----BEGIN [A-Z ]+PRIVATE KEY-----|\bsk-[A-Za-z0-9_-]{12,})`,
	)
)

var approvedSnapshotNotes = map[string]string{
	"request_id_join": "request_id is the intended join key between call_logs and request_trace_events",
	"missing_rows":    "missing call logs or trace events are represented as capture gaps and should not be fabricated by importers",
	"payload_policy":  "UniRouter raw prompt/response capture remains disabled for this snapshot path",
	"money_contract":  "cost_micros is canonical non-negative integer micro-CNY text; currency=CNY, amount_scale=1000000, money_schema=micro-cny.v1; cost_cents is absent",
}

// This mirrors the bounded scalar key contract in UniRouter's
// internal/traceutil metadata sanitizer. New producer keys fail closed until
// both projects review the versioned intake boundary.
var approvedSanitizedMetadataKeys = map[string]bool{
	"actual_cost_micros":                true,
	"amount_scale":                      true,
	"auth_failure_cooldown_seconds":     true,
	"billing_status":                    true,
	"cache_creation_cost_cents":         true,
	"cache_creation_cost_micros":        true,
	"cache_creation_ratio":              true,
	"cache_creation_tokens":             true,
	"cache_hit":                         true,
	"cache_read_cost_cents":             true,
	"cache_read_cost_micros":            true,
	"cache_read_ratio":                  true,
	"cache_read_tokens":                 true,
	"cache_scope":                       true,
	"cache_usage_authoritative":         true,
	"cache_usage_schema":                true,
	"cache_usage_status":                true,
	"charged_cost_cents":                true,
	"charged_cost_micros":               true,
	"circuit_breaker_half_open_percent": true,
	"circuit_breaker_recover_seconds":   true,
	"circuit_breaker_threshold":         true,
	"completion_tokens":                 true,
	"cost_cents":                        true,
	"cost_micros":                       true,
	"currency":                          true,
	"effective_config_version":          true,
	"error_code":                        true,
	"estimated_tokens":                  true,
	"failure_kind":                      true,
	"freeze_amount_cents":               true,
	"freeze_amount_micros":              true,
	"guardrail_action":                  true,
	"guardrail_categories":              true,
	"guardrail_decision":                true,
	"guardrail_degraded":                true,
	"guardrail_detector_ref":            true,
	"guardrail_detector_version":        true,
	"guardrail_failure_mode":            true,
	"guardrail_finding_fingerprints":    true,
	"guardrail_mandatory":               true,
	"guardrail_match_count":             true,
	"guardrail_mode":                    true,
	"guardrail_outcome":                 true,
	"guardrail_policy_ref":              true,
	"guardrail_policy_set_version":      true,
	"guardrail_policy_version":          true,
	"guardrail_transformed":             true,
	"is_stream":                         true,
	"limit_policy_chain":                true,
	"limit_policy_decision_count":       true,
	"limit_policy_enforce_count":        true,
	"limit_policy_reason":               true,
	"limit_policy_result":               true,
	"limit_policy_set_version":          true,
	"limit_policy_shadow_count":         true,
	"mapped_model":                      true,
	"max_retry_count":                   true,
	"money_schema":                      true,
	"payload_capture_mode":              true,
	"payload_policy_reason":             true,
	"payload_ref_present":               true,
	"prepaid_topup_cents":               true,
	"prepaid_topup_micros":              true,
	"pricing_version":                   true,
	"projected_charge_micros":           true,
	"prompt_tokens":                     true,
	"provider_key_failure_threshold":    true,
	"rate_limit_cooldown_seconds":       true,
	"raw_payload_stored":                true,
	"reserved_amount_micros":            true,
	"retry_decision":                    true,
	"retryable":                         true,
	"route_count":                       true,
	"route_index":                       true,
	"routing_candidate_count":           true,
	"routing_config_version":            true,
	"routing_excluded_count":            true,
	"routing_exclusion_reasons":         true,
	"routing_policy_version":            true,
	"routing_priority":                  true,
	"routing_requires_probe":            true,
	"routing_selection_reason":          true,
	"routing_snapshot_degraded":         true,
	"routing_snapshot_source":           true,
	"routing_snapshot_version":          true,
	"routing_weight":                    true,
	"settle_cancelled":                  true,
	"stream":                            true,
	"stream_max_sse_line_bytes":         true,
	"stream_policy_version":             true,
	"total_tokens":                      true,
	"ttft_ms":                           true,
	"uncached_prompt_tokens":            true,
	"usage_confidence":                  true,
	"usage_source":                      true,
}

var approvedGapReasons = map[string]bool{
	"no call_logs row matched this request_id in the selected snapshot":                                                                                        true,
	"no request_trace_events row matched this request_id and no call_logs row is present in the selected snapshot":                                             true,
	"call_logs row exists but no request_trace_events row matched this request_id; possible trace sampling, trace retention cleanup, or non-instrumented path": true,
}

const approvedPayloadCaptureReason = "raw prompt/response export is disabled for PRD5 Gate A snapshots"

type SanitizedPreflightOptions struct {
	MaxRequestIDs int
	MaxEvents     int
}

type sanitizedApprovalManifest struct {
	SchemaVersion  string                             `json:"schema_version"`
	SourceInstance string                             `json:"source_instance"`
	Requests       []sanitizedApprovalManifestRequest `json:"requests"`
}

type sanitizedApprovalManifestRequest struct {
	ScenarioLabel  string `json:"scenario_label"`
	RequestID      string `json:"request_id"`
	ApprovalStatus string `json:"approval_status"`
	Required       bool   `json:"required"`
	Notes          string `json:"notes,omitempty"`
}

type SanitizedPreflightFinding struct {
	Path string `json:"path"`
	Code string `json:"code"`
}

type SanitizedPreflightReport struct {
	Status                 string                      `json:"status"`
	ArtifactSHA256         string                      `json:"artifact_sha256"`
	ApprovalManifestSHA256 string                      `json:"approval_manifest_sha256"`
	RequestIDs             int                         `json:"request_ids"`
	CallLogs               int                         `json:"call_logs"`
	TraceEvents            int                         `json:"request_trace_events"`
	CaptureGaps            int                         `json:"capture_gaps"`
	EventsTotal            int                         `json:"events_total"`
	SingleOrg              bool                        `json:"single_org"`
	PrivateArtifact        bool                        `json:"private_artifact"`
	PrivateManifest        bool                        `json:"private_manifest"`
	MetadataKeyCount       int                         `json:"metadata_key_count"`
	Findings               []SanitizedPreflightFinding `json:"findings,omitempty"`
}

func PreflightSanitizedSnapshot(path string, manifestPath string, options SanitizedPreflightOptions) (SanitizedPreflightReport, error) {
	options, err := normalizeSanitizedPreflightOptions(options)
	if err != nil {
		return SanitizedPreflightReport{}, err
	}
	data, privateArtifact, err := readSanitizedSnapshotBytes(path)
	if err != nil {
		return SanitizedPreflightReport{}, err
	}
	snapshot, artifactSHA256, err := loadSnapshotArtifactBytes(data)
	if err != nil {
		return SanitizedPreflightReport{}, fmt.Errorf("sanitized snapshot decode or contract validation failed")
	}
	manifestData, privateManifest, err := readSanitizedSnapshotBytes(manifestPath)
	if err != nil {
		return SanitizedPreflightReport{}, err
	}
	manifest, manifestSHA256, err := loadSanitizedApprovalManifest(manifestData)
	if err != nil {
		return SanitizedPreflightReport{}, fmt.Errorf("approval manifest decode failed")
	}

	report := SanitizedPreflightReport{
		Status:                 "blocked",
		ArtifactSHA256:         artifactSHA256,
		ApprovalManifestSHA256: manifestSHA256,
		RequestIDs:             len(snapshot.RequestIDs),
		CallLogs:               len(snapshot.CallLogs),
		TraceEvents:            len(snapshot.TraceEvents),
		CaptureGaps:            len(snapshot.CaptureGaps),
		EventsTotal:            len(snapshot.CallLogs) + len(snapshot.TraceEvents) + len(snapshot.CaptureGaps),
		PrivateArtifact:        privateArtifact,
		PrivateManifest:        privateManifest,
	}

	findings := newSanitizedFindingSet()
	validateSnapshotRequiredFields(data, findings)
	if !privateArtifact {
		findings.add("$", "artifact_permissions_not_private")
	}
	if !privateManifest {
		findings.add("$manifest", "manifest_permissions_not_private")
	}
	if manifest.SchemaVersion != sanitizedApprovalManifestSchema {
		findings.add("$manifest.schema_version", "approval_manifest_schema_required")
	}
	if manifest.SourceInstance != snapshot.SourceInstance {
		findings.add("$manifest.source_instance", "manifest_source_instance_mismatch")
	}
	if !opaqueTokenPattern.MatchString(manifest.SourceInstance) || manifest.SourceInstance == "unknown" {
		findings.add("$manifest.source_instance", "bounded_source_instance_required")
	}
	validateSensitiveValue("$manifest.source_instance", manifest.SourceInstance, findings)
	if snapshot.ApprovalManifestSHA256 != manifestSHA256 {
		findings.add("$.approval_manifest_sha256", "approval_manifest_sha256_mismatch")
	}
	if snapshot.SchemaVersion != SnapshotSchemaV2 {
		findings.add("$.schema_version", "snapshot_v2_required")
	}
	if snapshot.Source != "unirouter" {
		findings.add("$.source", "unirouter_source_required")
	}
	if !opaqueTokenPattern.MatchString(snapshot.SourceInstance) || snapshot.SourceInstance == "unknown" {
		findings.add("$.source_instance", "bounded_source_instance_required")
	}
	validateSensitiveValue("$.source_instance", snapshot.SourceInstance, findings)
	if !opaqueTokenPattern.MatchString(snapshot.ExchangeID) {
		findings.add("$.exchange_id", "opaque_exchange_id_required")
	}
	validateSensitiveValue("$.exchange_id", snapshot.ExchangeID, findings)
	if !lowerHexSHA256Pattern.MatchString(snapshot.ApprovalManifestSHA256) {
		findings.add("$.approval_manifest_sha256", "approval_manifest_sha256_required")
	}
	if snapshot.Safety.SelectionMode != "approved_request_id_manifest" {
		findings.add("$.safety.selection_mode", "approval_manifest_selection_required")
	}
	if snapshot.Safety.RawPromptsIncluded || snapshot.Safety.RawResponsesIncluded || snapshot.Safety.APIKeysIncluded {
		findings.add("$.safety", "unsafe_content_flag_set")
	}
	if err := validateSnapshotSafe(snapshot); err != nil {
		findings.add("$", "unsafe_snapshot_content")
	}
	historicalIPDeclaration := false
	switch snapshot.Safety.IPHandling {
	case strictIPHandling:
	case historicalIPHandling:
		historicalIPDeclaration = true
	default:
		findings.add("$.safety.ip_handling", "unapproved_ip_handling")
	}
	if historicalIPDeclaration {
		for _, row := range snapshot.CallLogs {
			if row.IPAddressWasPresent || row.IPAddressRedacted {
				findings.add("$.safety.ip_handling", "dual_boolean_ip_handling_required")
				break
			}
		}
	}
	if snapshot.Safety.ErrorHandling != "raw upstream_error text is not exported; only upstream_error_present is retained" {
		findings.add("$.safety.error_handling", "unapproved_error_handling")
	}
	if snapshot.Safety.MetadataHandling != "request_trace_events.metadata is re-sanitized through internal/traceutil.SanitizeMetadata before export" {
		findings.add("$.safety.metadata_handling", "unapproved_metadata_handling")
	}
	if _, err := time.Parse(time.RFC3339Nano, snapshot.GeneratedAt); err != nil {
		findings.add("$.generated_at", "invalid_generated_at")
	}

	if len(snapshot.RequestIDs) == 0 {
		findings.add("$.request_ids", "request_ids_required")
	}
	if len(snapshot.RequestIDs) > options.MaxRequestIDs {
		findings.add("$.request_ids", "request_id_limit_exceeded")
	}
	if report.EventsTotal > options.MaxEvents {
		findings.add("$", "event_limit_exceeded")
	}
	if len(snapshot.CallLogs)+len(snapshot.TraceEvents) == 0 {
		findings.add("$", "source_rows_required")
	}
	validateDeclaredCounts(snapshot, findings)

	manifestRequests := map[string]string{}
	manifestLabels := map[string]bool{}
	if len(manifest.Requests) == 0 {
		findings.add("$manifest.requests", "approved_manifest_requests_required")
	}
	if len(manifest.Requests) > options.MaxRequestIDs {
		findings.add("$manifest.requests", "request_id_limit_exceeded")
	}
	for index, row := range manifest.Requests {
		base := fmt.Sprintf("$manifest.requests[%d]", index)
		validateCodeValue(base+".scenario_label", row.ScenarioLabel, findings)
		validateOpaqueValue(base+".request_id", row.RequestID, findings)
		approved := row.ApprovalStatus == "approved"
		if !approved {
			findings.add(base+".approval_status", "approved_status_required")
		}
		if !row.Required {
			findings.add(base+".required", "required_true_required")
		}
		if row.Notes != "" {
			findings.add(base+".notes", "manifest_notes_not_approved")
		}
		if !approved {
			continue
		}
		if _, exists := manifestRequests[row.RequestID]; exists {
			findings.add(base+".request_id", "duplicate_manifest_request_id")
		}
		if manifestLabels[row.ScenarioLabel] {
			findings.add(base+".scenario_label", "duplicate_manifest_scenario_label")
		}
		manifestRequests[row.RequestID] = row.ScenarioLabel
		manifestLabels[row.ScenarioLabel] = true
	}

	approvedRequests := map[string]bool{}
	requestIndexes := map[string]int{}
	for index, requestID := range snapshot.RequestIDs {
		path := fmt.Sprintf("$.request_ids[%d]", index)
		validateOpaqueValue(path, requestID, findings)
		if approvedRequests[requestID] {
			findings.add(path, "duplicate_request_id")
		}
		approvedRequests[requestID] = true
		if _, exists := requestIndexes[requestID]; !exists {
			requestIndexes[requestID] = index
		}
		if _, approved := manifestRequests[requestID]; !approved {
			findings.add(path, "request_not_manifest_approved")
		}
	}
	if len(approvedRequests) != len(manifestRequests) {
		findings.add("$.request_ids", "manifest_request_set_mismatch")
	}

	scenarioRequests := map[string]bool{}
	scenarioLabels := map[string]bool{}
	for index, scenario := range snapshot.Scenarios {
		base := fmt.Sprintf("$.scenarios[%d]", index)
		validateCodeValue(base+".label", scenario.Label, findings)
		validateOpaqueValue(base+".request_id", scenario.RequestID, findings)
		if scenarioLabels[scenario.Label] {
			findings.add(base+".label", "duplicate_scenario_label")
		}
		if scenarioRequests[scenario.RequestID] {
			findings.add(base+".request_id", "duplicate_scenario_request_id")
		}
		if !approvedRequests[scenario.RequestID] {
			findings.add(base+".request_id", "scenario_request_not_approved")
		}
		if manifestLabel, approved := manifestRequests[scenario.RequestID]; !approved || manifestLabel != scenario.Label {
			findings.add(base, "manifest_scenario_mismatch")
		}
		scenarioLabels[scenario.Label] = true
		scenarioRequests[scenario.RequestID] = true
	}
	if len(snapshot.Scenarios) != len(snapshot.RequestIDs) {
		findings.add("$.scenarios", "one_scenario_per_request_required")
	}

	orgs := map[int64]bool{}
	requestOrgs := map[string]map[int64]bool{}
	callLogsPerRequest := map[string]int{}
	for index, row := range snapshot.CallLogs {
		base := fmt.Sprintf("$.call_logs[%d]", index)
		validateSourceRowIdentity(base, "call_logs", row.ID, row.RequestID, row.SourceRef, row.SourceEventHash, snapshot.SourceInstance, approvedRequests, findings)
		orgs[row.OrgID] = true
		recordRequestOrg(requestOrgs, row.RequestID, row.OrgID)
		callLogsPerRequest[row.RequestID]++
		validatePositiveRowIdentity(base, row.ID, row.OrgID, row.ProjectID, row.APIKeyID, row.OwnerUserID, findings)
		validateOptionalCodeString(base+".key_type", row.KeyType, findings)
		validateOptionalIdentifierString(base+".model", row.Model, findings)
		validateOptionalIdentifierString(base+".requested_model", row.RequestedModel, findings)
		validateOptionalIdentifierString(base+".provider", row.Provider, findings)
		validateOptionalIdentifierString(base+".channel", row.Channel, findings)
		validateOptionalCodeString(base+".usage_source", row.UsageSource, findings)
		validateOptionalCodeString(base+".usage_confidence", row.UsageConfidence, findings)
		validateTimestamp(base+".created_at", row.CreatedAt, findings)
		validateOptionalOpaqueValue(base+".trace_id", row.TraceID, findings)
		validateOptionalOpaqueValue(base+".session_id", row.SessionID, findings)
		validateOptionalOpaqueValue(base+".upstream_request_id", row.UpstreamRequestID, findings)
		validateOptionalCodeValue(base+".evidence_ref", row.EvidenceRef, findings)
		validateOptionalCodeValue(base+".actor_type", row.ActorType, findings)
		validateOptionalCodeValue(base+".actor_ref", row.ActorRef, findings)
		validateOptionalCodeValue(base+".on_behalf_of", row.OnBehalfOf, findings)
		if row.PayloadRef != nil && *row.PayloadRef != "" {
			findings.add(base+".payload_ref", "payload_reference_not_approved")
		}
		if row.PayloadCaptureMode != "metadata_only" && row.PayloadCaptureMode != "hash_only" && row.PayloadCaptureMode != "none" {
			findings.add(base+".payload_capture_mode", "payload_capture_mode_not_approved")
		}
		if row.PayloadCaptureReason != approvedPayloadCaptureReason {
			findings.add(base+".payload_capture_reason", "unapproved_free_text")
		}
		validateCodeValue(base+".capture_mode", row.DerivedCaptureMode, findings)
		validateCodeValue(base+".schema_version", row.DerivedSchemaVersion, findings)
		validateCodeValue(base+".source_component", row.DerivedSourceComponent, findings)
		if row.IPAddressWasPresent && !row.IPAddressRedacted {
			findings.add(base+".ip_address_redacted", "raw_ip_redaction_not_proven")
		}
		if !row.IPAddressWasPresent && row.IPAddressRedacted {
			findings.add(base+".ip_address_was_present", "raw_ip_presence_not_proven")
		}
	}
	for _, count := range callLogsPerRequest {
		if count > 1 {
			findings.add("$.call_logs", "multiple_call_logs_for_request")
		}
	}

	metadataKeys := map[string]bool{}
	for index, row := range snapshot.TraceEvents {
		base := fmt.Sprintf("$.request_trace_events[%d]", index)
		validateSourceRowIdentity(base, "request_trace_events", row.ID, row.RequestID, row.SourceRef, row.SourceEventHash, snapshot.SourceInstance, approvedRequests, findings)
		orgs[row.OrgID] = true
		recordRequestOrg(requestOrgs, row.RequestID, row.OrgID)
		validatePositiveRowIdentity(base, row.ID, row.OrgID, row.ProjectID, row.APIKeyID, row.OwnerUserID, findings)
		validateOptionalCodeString(base+".key_type", row.KeyType, findings)
		validateCodeValue(base+".event_type", row.EventType, findings)
		validateCodeValue(base+".stage", row.Stage, findings)
		validateCodeValue(base+".severity", row.Severity, findings)
		validateOptionalIdentifierString(base+".model", row.Model, findings)
		validateOptionalIdentifierString(base+".requested_model", row.RequestedModel, findings)
		validateOptionalIdentifierString(base+".provider", row.Provider, findings)
		validateOptionalIdentifierString(base+".channel", row.Channel, findings)
		validateTimestamp(base+".created_at", row.CreatedAt, findings)
		validateOptionalOpaqueValue(base+".trace_id", row.TraceID, findings)
		validateOptionalOpaqueValue(base+".span_id", row.SpanID, findings)
		validateOptionalOpaqueValue(base+".parent_span_id", row.ParentSpanID, findings)
		validateOptionalOpaqueValue(base+".session_id", row.SessionID, findings)
		validateOptionalOpaqueValue(base+".upstream_request_id", row.UpstreamRequestID, findings)
		validateOptionalCodeValue(base+".capture_gap", row.CaptureGap, findings)
		validateOptionalCodeValue(base+".evidence_ref", row.EvidenceRef, findings)
		validateOptionalCodeValue(base+".decision_kind", row.DecisionKind, findings)
		validateOptionalCodeValue(base+".decision_result", row.DecisionResult, findings)
		validateOptionalCodeValue(base+".decision_reason", row.DecisionReason, findings)
		validateOptionalCodeValue(base+".actor_type", row.ActorType, findings)
		validateOptionalCodeValue(base+".actor_ref", row.ActorRef, findings)
		validateOptionalCodeValue(base+".on_behalf_of", row.OnBehalfOf, findings)
		if row.PayloadRef != nil && *row.PayloadRef != "" {
			findings.add(base+".payload_ref", "payload_reference_not_approved")
		}
		if row.PayloadCaptureMode != "metadata_only" && row.PayloadCaptureMode != "hash_only" && row.PayloadCaptureMode != "none" {
			findings.add(base+".payload_capture_mode", "payload_capture_mode_not_approved")
		}
		if row.PayloadCaptureReason != approvedPayloadCaptureReason {
			findings.add(base+".payload_capture_reason", "unapproved_free_text")
		}
		validateCodeValue(base+".capture_mode", row.DerivedCaptureMode, findings)
		validateCodeValue(base+".schema_version", row.DerivedSchemaVersion, findings)
		validateCodeValue(base+".source_component", row.DerivedSourceComponent, findings)
		actualMetadataKeys := sortedMapKeys(row.Metadata)
		for _, key := range actualMetadataKeys {
			metadataKeys[key] = true
		}
		if !equalStrings(actualMetadataKeys, row.MetadataKeys) {
			findings.add(base+".metadata_keys", "metadata_key_inventory_mismatch")
		}
		scanApprovedMetadata(base+".metadata", row.Metadata, findings)
	}
	for requestID := range approvedRequests {
		requestPath := fmt.Sprintf("$.request_ids[%d]", requestIndexes[requestID])
		if len(requestOrgs[requestID]) == 0 {
			findings.add(requestPath, "source_row_required_for_org_binding")
		} else if len(requestOrgs[requestID]) != 1 {
			findings.add(requestPath, "single_org_per_request_required")
		}
	}

	for index, gap := range snapshot.CaptureGaps {
		base := fmt.Sprintf("$.capture_gaps[%d]", index)
		validateOpaqueValue(base+".request_id", gap.RequestID, findings)
		if !approvedRequests[gap.RequestID] {
			findings.add(base+".request_id", "gap_request_not_approved")
		}
		validateCodeValue(base+".source", gap.Source, findings)
		validateCodeValue(base+".capture_gap", gap.Gap, findings)
		for candidateIndex, candidate := range gap.CandidateCaptureGaps {
			validateCodeValue(fmt.Sprintf("%s.candidate_capture_gaps[%d]", base, candidateIndex), candidate, findings)
		}
		if !gap.Generated || !approvedGapReasons[gap.Reason] {
			findings.add(base+".reason", "unapproved_free_text")
		}
	}

	validateSnapshotNotes(snapshot.Notes, findings)
	report.SingleOrg = len(orgs) == 1
	if len(orgs) != 1 {
		findings.add("$", "single_org_source_rows_required")
	}
	report.MetadataKeyCount = len(metadataKeys)
	report.Findings = findings.sorted()
	if len(report.Findings) == 0 {
		report.Status = "pass"
	}
	return report, nil
}

func readSanitizedSnapshotBytes(path string) ([]byte, bool, error) {
	return readSanitizedSnapshotBytesWithHook(path, nil)
}

func readSanitizedSnapshotBytesWithHook(path string, betweenReads func() error) ([]byte, bool, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, false, fmt.Errorf("sanitized input path must be absolute and clean")
	}
	preOpenInfo, err := os.Lstat(path)
	if err != nil {
		return nil, false, err
	}
	if !preOpenInfo.Mode().IsRegular() {
		return nil, false, fmt.Errorf("sanitized input must be one stable regular file")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, false, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, false, fmt.Errorf("sanitized input file descriptor is unavailable")
	}
	defer file.Close()

	openedInfo, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, false, err
	}
	if !openedInfo.Mode().IsRegular() || !pathInfo.Mode().IsRegular() ||
		!os.SameFile(preOpenInfo, openedInfo) || !os.SameFile(openedInfo, pathInfo) {
		return nil, false, fmt.Errorf("sanitized input must be one stable regular file")
	}
	preOpenState, err := sanitizedFileStateFromInfo(preOpenInfo)
	if err != nil {
		return nil, false, err
	}
	openedState, err := sanitizedFileStateFromInfo(openedInfo)
	if err != nil {
		return nil, false, err
	}
	pathState, err := sanitizedFileStateFromInfo(pathInfo)
	if err != nil {
		return nil, false, err
	}
	if !preOpenState.equal(openedState) || !openedState.equal(pathState) {
		return nil, false, fmt.Errorf("sanitized input changed before read")
	}
	if openedState.nlink != 1 {
		return nil, false, fmt.Errorf("sanitized input must have exactly one hard link")
	}
	if openedInfo.Size() > MaxSnapshotBytes {
		return nil, false, fmt.Errorf("sanitized input exceeds %d bytes", MaxSnapshotBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxSnapshotBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > MaxSnapshotBytes {
		return nil, false, fmt.Errorf("sanitized input exceeds %d bytes", MaxSnapshotBytes)
	}
	if int64(len(data)) != openedState.size {
		return nil, false, fmt.Errorf("sanitized input changed during read")
	}
	if betweenReads != nil {
		if err := betweenReads(); err != nil {
			return nil, false, err
		}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, false, err
	}
	expectedDigest := sha256.Sum256(data)
	verificationDigest := sha256.New()
	verifiedBytes, err := io.Copy(verificationDigest, io.LimitReader(file, MaxSnapshotBytes+1))
	if err != nil {
		return nil, false, err
	}
	if verifiedBytes != int64(len(data)) || !bytes.Equal(expectedDigest[:], verificationDigest.Sum(nil)) {
		return nil, false, fmt.Errorf("sanitized input changed during read")
	}
	openedAfterInfo, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	pathAfterInfo, err := os.Lstat(path)
	if err != nil {
		return nil, false, err
	}
	if !openedAfterInfo.Mode().IsRegular() || !pathAfterInfo.Mode().IsRegular() ||
		!os.SameFile(openedInfo, openedAfterInfo) || !os.SameFile(openedAfterInfo, pathAfterInfo) {
		return nil, false, fmt.Errorf("sanitized input changed during read")
	}
	openedAfterState, err := sanitizedFileStateFromInfo(openedAfterInfo)
	if err != nil {
		return nil, false, err
	}
	pathAfterState, err := sanitizedFileStateFromInfo(pathAfterInfo)
	if err != nil {
		return nil, false, err
	}
	if !openedState.equal(openedAfterState) || !openedAfterState.equal(pathAfterState) {
		return nil, false, fmt.Errorf("sanitized input changed during read")
	}
	permissions := openedState.mode.Perm()
	private := (permissions == 0o400 || permissions == 0o600) && openedState.uid == uint32(os.Geteuid())
	return data, private, nil
}

type sanitizedFileState struct {
	size        int64
	mode        os.FileMode
	modifiedAt  time.Time
	uid         uint32
	nlink       uint64
	changeSec   int64
	changeNSec  int64
	changeKnown bool
}

func sanitizedFileStateFromInfo(info os.FileInfo) (sanitizedFileState, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return sanitizedFileState{}, fmt.Errorf("sanitized input file identity is unavailable")
	}
	changeSec, changeNSec, changeKnown := statChangeTime(stat)
	return sanitizedFileState{
		size:        info.Size(),
		mode:        info.Mode(),
		modifiedAt:  info.ModTime(),
		uid:         stat.Uid,
		nlink:       uint64(stat.Nlink),
		changeSec:   changeSec,
		changeNSec:  changeNSec,
		changeKnown: changeKnown,
	}, nil
}

func (state sanitizedFileState) equal(other sanitizedFileState) bool {
	if state.size != other.size || state.mode != other.mode || !state.modifiedAt.Equal(other.modifiedAt) ||
		state.uid != other.uid || state.nlink != other.nlink || state.changeKnown != other.changeKnown {
		return false
	}
	return !state.changeKnown || (state.changeSec == other.changeSec && state.changeNSec == other.changeNSec)
}

func statChangeTime(stat *syscall.Stat_t) (int64, int64, bool) {
	value := reflect.ValueOf(stat)
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	for _, name := range []string{"Ctim", "Ctimespec"} {
		field := value.FieldByName(name)
		if !field.IsValid() || field.Kind() != reflect.Struct {
			continue
		}
		seconds := field.FieldByName("Sec")
		nanoseconds := field.FieldByName("Nsec")
		if seconds.IsValid() && nanoseconds.IsValid() && seconds.CanInt() && nanoseconds.CanInt() {
			return seconds.Int(), nanoseconds.Int(), true
		}
	}
	return 0, 0, false
}

func loadSanitizedApprovalManifest(data []byte) (sanitizedApprovalManifest, string, error) {
	var manifest sanitizedApprovalManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return sanitizedApprovalManifest{}, "", err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return sanitizedApprovalManifest{}, "", fmt.Errorf("approval manifest must contain exactly one JSON object")
	}
	digest := sha256.Sum256(data)
	return manifest, fmt.Sprintf("sha256:%x", digest), nil
}

func validateSnapshotRequiredFields(data []byte, findings *sanitizedFindingSet) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		findings.add("$", "snapshot_json_required_fields_unavailable")
		return
	}
	for _, field := range []string{
		"schema_version", "exchange_id", "approval_manifest_sha256", "generated_at",
		"source", "source_instance", "request_ids", "scenarios", "safety", "counts",
		"capture_gaps", "call_logs", "request_trace_events", "notes",
	} {
		if _, ok := raw[field]; !ok {
			findings.add("$."+field, "required_snapshot_field_missing")
		}
	}
	var safety map[string]json.RawMessage
	if err := json.Unmarshal(raw["safety"], &safety); err != nil {
		findings.add("$.safety", "safety_object_required")
	} else {
		for _, field := range []string{
			"selection_mode", "raw_prompts_included", "raw_responses_included",
			"api_keys_included", "ip_handling", "error_handling", "metadata_handling",
		} {
			if _, ok := safety[field]; !ok {
				findings.add("$.safety", "required_safety_field_missing")
			}
		}
	}
	var counts map[string]json.RawMessage
	if err := json.Unmarshal(raw["counts"], &counts); err != nil {
		findings.add("$.counts", "counts_object_required")
	} else {
		for _, field := range []string{"requested_request_ids", "call_logs", "request_trace_events", "capture_gaps"} {
			if _, ok := counts[field]; !ok {
				findings.add("$.counts", "required_count_field_missing")
			}
		}
	}
}

func normalizeSanitizedPreflightOptions(options SanitizedPreflightOptions) (SanitizedPreflightOptions, error) {
	if options.MaxRequestIDs == 0 {
		options.MaxRequestIDs = DefaultSanitizedPreflightMaxRequestIDs
	}
	if options.MaxEvents == 0 {
		options.MaxEvents = DefaultSanitizedPreflightMaxEvents
	}
	if options.MaxRequestIDs < 1 || options.MaxRequestIDs > DefaultSanitizedPreflightMaxRequestIDs {
		return SanitizedPreflightOptions{}, fmt.Errorf("max request IDs must be between 1 and %d", DefaultSanitizedPreflightMaxRequestIDs)
	}
	if options.MaxEvents < 1 || options.MaxEvents > DefaultSanitizedPreflightMaxEvents {
		return SanitizedPreflightOptions{}, fmt.Errorf("max events must be between 1 and %d", DefaultSanitizedPreflightMaxEvents)
	}
	return options, nil
}

func validateDeclaredCounts(snapshot SnapshotFile, findings *sanitizedFindingSet) {
	if snapshot.Counts.RequestedRequestIDs != len(snapshot.RequestIDs) {
		findings.add("$.counts.requested_request_ids", "declared_count_mismatch")
	}
	if snapshot.Counts.CallLogs != len(snapshot.CallLogs) {
		findings.add("$.counts.call_logs", "declared_count_mismatch")
	}
	if snapshot.Counts.TraceEvents != len(snapshot.TraceEvents) {
		findings.add("$.counts.request_trace_events", "declared_count_mismatch")
	}
	if snapshot.Counts.CaptureGaps != len(snapshot.CaptureGaps) {
		findings.add("$.counts.capture_gaps", "declared_count_mismatch")
	}
}

func validateSourceRowIdentity(base string, sourceTable string, rowID int64, requestID string, sourceRef string, sourceHash string, sourceInstance string, approvedRequests map[string]bool, findings *sanitizedFindingSet) {
	validateOpaqueValue(base+".request_id", requestID, findings)
	if !approvedRequests[requestID] {
		findings.add(base+".request_id", "source_row_request_not_approved")
	}
	expectedSourceRef := fmt.Sprintf("unirouter:%s:%s:%d", sourceInstance, sourceTable, rowID)
	if sourceRef != expectedSourceRef || !opaqueTokenPattern.MatchString(sourceRef) {
		findings.add(base+".source_ref", "invalid_source_ref")
	}
	validateSensitiveValue(base+".source_ref", sourceRef, findings)
	if !rawLowerHexSHA256Pattern.MatchString(sourceHash) {
		findings.add(base+".source_event_hash", "source_event_sha256_required")
	}
}

func recordRequestOrg(requestOrgs map[string]map[int64]bool, requestID string, orgID int64) {
	if requestOrgs[requestID] == nil {
		requestOrgs[requestID] = map[int64]bool{}
	}
	requestOrgs[requestID][orgID] = true
}

func validateOpaqueValue(path string, value string, findings *sanitizedFindingSet) {
	if !opaqueTokenPattern.MatchString(value) {
		findings.add(path, "opaque_value_required")
	}
	validateSensitiveValue(path, value, findings)
}

func validateCodeValue(path string, value string, findings *sanitizedFindingSet) {
	if !codeValuePattern.MatchString(value) {
		findings.add(path, "code_value_required")
	}
	validateSensitiveValue(path, value, findings)
}

func validateOptionalCodeString(path string, value string, findings *sanitizedFindingSet) {
	if value == "" {
		return
	}
	validateCodeValue(path, value, findings)
}

func validateIdentifierValue(path string, value string, findings *sanitizedFindingSet) {
	if !identifierValuePattern.MatchString(value) {
		findings.add(path, "identifier_value_required")
	}
	validateSensitiveValue(path, value, findings)
}

func validateOptionalIdentifierString(path string, value string, findings *sanitizedFindingSet) {
	if value == "" {
		return
	}
	validateIdentifierValue(path, value, findings)
}

func validateTimestamp(path string, value string, findings *sanitizedFindingSet) {
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		findings.add(path, "rfc3339_timestamp_required")
	}
}

func validatePositiveRowIdentity(base string, rowID int64, orgID int64, projectID *int64, apiKeyID *int64, ownerUserID *int64, findings *sanitizedFindingSet) {
	if rowID <= 0 {
		findings.add(base+".id", "positive_source_row_id_required")
	}
	if orgID <= 0 {
		findings.add(base+".org_id", "positive_org_id_required")
	}
	for path, value := range map[string]*int64{
		base + ".project_id":    projectID,
		base + ".api_key_id":    apiKeyID,
		base + ".owner_user_id": ownerUserID,
	} {
		if value != nil && *value <= 0 {
			findings.add(path, "positive_optional_id_required")
		}
	}
}

func validateOptionalOpaqueValue(path string, value *string, findings *sanitizedFindingSet) {
	if value == nil || *value == "" {
		return
	}
	validateOpaqueValue(path, *value, findings)
}

func validateOptionalCodeValue(path string, value *string, findings *sanitizedFindingSet) {
	if value == nil || *value == "" {
		return
	}
	validateCodeValue(path, *value, findings)
}

func validateSensitiveValue(path string, value string, findings *sanitizedFindingSet) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return
	}
	if emailValuePattern.MatchString(trimmed) {
		findings.add(path, "email_like_value")
	}
	if phoneValuePattern.MatchString(trimmed) {
		findings.add(path, "phone_like_value")
	}
	if genericPhonePattern.MatchString(trimmed) {
		findings.add(path, "phone_like_value")
	}
	if credentialValuePattern.MatchString(trimmed) {
		findings.add(path, "credential_like_value")
	}
	if strings.Contains(strings.ToLower(trimmed), "http://") || strings.Contains(strings.ToLower(trimmed), "https://") {
		findings.add(path, "url_like_value")
	}
	if ipv4ValuePattern.MatchString(trimmed) {
		findings.add(path, "ip_like_value")
	}
	for _, token := range strings.FieldsFunc(trimmed, func(r rune) bool {
		return !(r == ':' || r == '.' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F'))
	}) {
		if strings.Contains(token, ":") && net.ParseIP(strings.Trim(token, "[]")) != nil {
			findings.add(path, "ip_like_value")
			break
		}
	}
}

func scanApprovedMetadata(path string, metadata map[string]any, findings *sanitizedFindingSet) {
	keys := sortedMapKeys(metadata)
	for index, key := range keys {
		value := metadata[key]
		keyPath := fmt.Sprintf("%s[%d].key", path, index)
		valuePath := fmt.Sprintf("%s[%d].value", path, index)
		if !codeValuePattern.MatchString(key) {
			findings.add(keyPath, "metadata_key_not_code_like")
		}
		if !approvedSanitizedMetadataKeys[key] {
			findings.add(keyPath, "metadata_key_not_approved")
		}
		validateSensitiveValue(keyPath, key, findings)
		switch typed := value.(type) {
		case string:
			validateCodeValue(valuePath, typed, findings)
		case float64:
			validateNumericMetadataValue(valuePath, typed, findings)
		case float32:
			validateNumericMetadataValue(valuePath, float64(typed), findings)
		case int:
			validateNumericMetadataValue(valuePath, float64(typed), findings)
		case int32:
			validateNumericMetadataValue(valuePath, float64(typed), findings)
		case int64:
			validateNumericMetadataValue(valuePath, float64(typed), findings)
		case bool, nil:
		default:
			findings.add(valuePath, "metadata_scalar_required")
		}
	}
}

func validateNumericMetadataValue(path string, value float64, findings *sanitizedFindingSet) {
	absolute := math.Abs(value)
	if math.Trunc(absolute) == absolute && absolute >= 1_000_000_000 && absolute < 1_000_000_000_000_000 {
		findings.add(path, "numeric_identifier_like_value")
	}
}

func validateSnapshotNotes(notes map[string]any, findings *sanitizedFindingSet) {
	if len(notes) != len(approvedSnapshotNotes) {
		findings.add("$.notes", "approved_notes_inventory_required")
	}
	for key, expected := range approvedSnapshotNotes {
		value, ok := notes[key].(string)
		if !ok || value != expected {
			findings.add("$.notes."+key, "approved_note_value_required")
		}
	}
	for key := range notes {
		if _, ok := approvedSnapshotNotes[key]; !ok {
			findings.add("$.notes", "unapproved_note_key")
		}
	}
}

func sortedMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func equalStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

type sanitizedFindingSet map[string]SanitizedPreflightFinding

func newSanitizedFindingSet() *sanitizedFindingSet {
	set := sanitizedFindingSet{}
	return &set
}

func (s *sanitizedFindingSet) add(path string, code string) {
	key := path + "\x00" + code
	(*s)[key] = SanitizedPreflightFinding{Path: path, Code: code}
}

func (s *sanitizedFindingSet) sorted() []SanitizedPreflightFinding {
	findings := make([]SanitizedPreflightFinding, 0, len(*s))
	for _, finding := range *s {
		findings = append(findings, finding)
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Path == findings[j].Path {
			return findings[i].Code < findings[j].Code
		}
		return findings[i].Path < findings[j].Path
	})
	return findings
}
