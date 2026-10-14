package unirouter

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/foigent/x-insight/internal/evidence"
)

const (
	ExecutionEvidenceProfileID                          = "unirouter.execution-evidence.v2"
	ExecutionEvidenceProfileManifestVersion             = "2.0.0"
	ExecutionEvidenceProfileManifestSHA256              = "sha256:834bbaae05936af76abc4608ab16547581656e415c7d73f79d5d55ef6eca9547"
	ExecutionEvidenceBundleSchema                       = "unirouter.execution-evidence.bundle.v2"
	ExecutionEvidenceBundleSchemaSHA256                 = "sha256:e47c5e074e3b7de8c16be9615c5ea073348017c58e07b9ba0566e2a66185f33a"
	DefaultExecutionEvidenceAdapterID                   = "22222222-2222-4222-8222-222222222225"
	ExecutionEvidenceSelectionModeCloseMarker           = "execution_evidence_close_marker"
	ExecutionEvidenceSelectionModeSignedCampaignPackage = "signed_campaign_package_frozen_scope"
	MaxExecutionEvidenceTraceEvents                     = 200
	MaxExecutionEvidenceCallLogs                        = 1
	MaxExecutionEvidenceGaps                            = 201
)

var executionEvidenceStages = []string{
	"identity",
	"routing",
	"provider",
	"retry_fallback",
	"billing",
	"usage",
	"logging",
}

type ExecutionEvidenceProfileRef struct {
	ID                 string `json:"id"`
	ManifestVersion    string `json:"manifest_version"`
	ManifestSHA256     string `json:"manifest_sha256"`
	BundleSchemaSHA256 string `json:"bundle_schema_sha256"`
}

type ExecutionEvidenceCompleteness struct {
	State                    string   `json:"state"`
	SourceProfileComplete    bool     `json:"source_profile_complete"`
	ExpectedTraceEventHashes []string `json:"expected_trace_event_hashes"`
	ExpectedCallLogHashes    []string `json:"expected_call_log_hashes"`
	ObservedTraceEventHashes []string `json:"observed_trace_event_hashes"`
	ObservedCallLogHashes    []string `json:"observed_call_log_hashes"`
	MissingEventHashes       []string `json:"missing_event_hashes"`
}

type ExecutionEvidenceStageCoverage struct {
	Stage             string   `json:"stage"`
	Status            string   `json:"status"`
	SourceEventHashes []string `json:"source_event_hashes"`
}

type ExecutionEvidenceTraceFact struct {
	SourceRef          string         `json:"source_ref"`
	SourceEventHash    string         `json:"source_event_hash"`
	EventType          string         `json:"event_type"`
	Stage              string         `json:"stage"`
	Severity           string         `json:"severity"`
	CreatedAt          string         `json:"created_at"`
	TraceID            string         `json:"trace_id,omitempty"`
	SpanID             string         `json:"span_id,omitempty"`
	ParentSpanID       string         `json:"parent_span_id,omitempty"`
	AttemptID          string         `json:"attempt_id,omitempty"`
	ActorType          string         `json:"actor_type,omitempty"`
	ActorRef           string         `json:"actor_ref,omitempty"`
	OnBehalfOf         string         `json:"on_behalf_of,omitempty"`
	Model              string         `json:"model,omitempty"`
	RequestedModel     string         `json:"requested_model,omitempty"`
	Provider           string         `json:"provider,omitempty"`
	Channel            string         `json:"channel,omitempty"`
	StatusCode         *int64         `json:"status_code,omitempty"`
	LatencyMS          int64          `json:"latency_ms,omitempty"`
	DecisionKind       string         `json:"decision_kind,omitempty"`
	DecisionResult     string         `json:"decision_result,omitempty"`
	DecisionReason     string         `json:"decision_reason,omitempty"`
	CaptureGap         string         `json:"capture_gap,omitempty"`
	PayloadCaptureMode string         `json:"payload_capture_mode"`
	Metadata           map[string]any `json:"metadata,omitempty"`
}

type ExecutionEvidenceCallLogFact struct {
	SourceRef          string `json:"source_ref"`
	SourceEventHash    string `json:"source_event_hash"`
	CreatedAt          string `json:"created_at"`
	TraceID            string `json:"trace_id,omitempty"`
	AttemptID          string `json:"attempt_id,omitempty"`
	ActorType          string `json:"actor_type,omitempty"`
	ActorRef           string `json:"actor_ref,omitempty"`
	Model              string `json:"model"`
	RequestedModel     string `json:"requested_model,omitempty"`
	Provider           string `json:"provider"`
	Channel            string `json:"channel,omitempty"`
	StatusCode         int64  `json:"status_code"`
	UpstreamStatusCode *int64 `json:"upstream_status_code,omitempty"`
	LatencyMS          int64  `json:"latency_ms"`
	PromptTokens       int64  `json:"prompt_tokens"`
	CompletionTokens   int64  `json:"completion_tokens"`
	CostMicros         string `json:"cost_micros"`
	Currency           string `json:"currency"`
	AmountScale        int64  `json:"amount_scale"`
	UsageSource        string `json:"usage_source,omitempty"`
	UsageAuthority     string `json:"usage_authority,omitempty"`
	UsageFinality      string `json:"usage_finality,omitempty"`
	PayloadCaptureMode string `json:"payload_capture_mode"`
}

type ExecutionEvidenceGap struct {
	Code            string `json:"code"`
	Stage           string `json:"stage"`
	Reason          string `json:"reason"`
	SourceEventHash string `json:"source_event_hash,omitempty"`
}

type ExecutionEvidenceBundle struct {
	SchemaVersion  string                           `json:"schema_version"`
	Profile        ExecutionEvidenceProfileRef      `json:"profile"`
	SourceInstance string                           `json:"source_instance"`
	RequestID      string                           `json:"request_id"`
	OrgID          int64                            `json:"org_id"`
	ProjectID      *int64                           `json:"project_id,omitempty"`
	ClosedAt       string                           `json:"closed_at"`
	Completeness   ExecutionEvidenceCompleteness    `json:"completeness"`
	StageCoverage  []ExecutionEvidenceStageCoverage `json:"stage_coverage"`
	TraceEvents    []ExecutionEvidenceTraceFact     `json:"trace_events"`
	CallLogs       []ExecutionEvidenceCallLogFact   `json:"call_logs"`
	CaptureGaps    []ExecutionEvidenceGap           `json:"capture_gaps"`
}

type ExecutionEvidenceImportReport struct {
	SnapshotImportReport
	Bundle ExecutionEvidenceBundle `json:"-"`
}

type ExecutionEvidenceImportContext struct {
	SelectionMode string
}

func ImportExecutionEvidenceBundle(path, schemaPath string, options SnapshotImportOptions) (ExecutionEvidenceImportReport, error) {
	return ImportExecutionEvidenceBundleWithContext(
		path,
		schemaPath,
		options,
		ExecutionEvidenceImportContext{SelectionMode: ExecutionEvidenceSelectionModeCloseMarker},
	)
}

func ImportExecutionEvidenceBundleWithContext(
	path, schemaPath string,
	options SnapshotImportOptions,
	context ExecutionEvidenceImportContext,
) (ExecutionEvidenceImportReport, error) {
	selectionMode, err := normalizeExecutionEvidenceSelectionMode(context.SelectionMode)
	if err != nil {
		return ExecutionEvidenceImportReport{}, err
	}
	data, err := readSnapshotBytes(path)
	if err != nil {
		return ExecutionEvidenceImportReport{}, err
	}
	return importExecutionEvidenceBundleBytes(data, path, schemaPath, options, selectionMode)
}

func ImportExecutionEvidenceBundleBytesWithContext(
	data []byte,
	schemaPath string,
	options SnapshotImportOptions,
	context ExecutionEvidenceImportContext,
) (ExecutionEvidenceImportReport, error) {
	selectionMode, err := normalizeExecutionEvidenceSelectionMode(context.SelectionMode)
	if err != nil {
		return ExecutionEvidenceImportReport{}, err
	}
	return importExecutionEvidenceBundleBytes(data, "", schemaPath, options, selectionMode)
}

func importExecutionEvidenceBundleBytes(
	data []byte,
	snapshotPath, schemaPath string,
	options SnapshotImportOptions,
	selectionMode string,
) (ExecutionEvidenceImportReport, error) {
	bundle, artifactSHA256, err := loadExecutionEvidenceBundleBytes(data)
	if err != nil {
		return ExecutionEvidenceImportReport{}, err
	}
	if selectionMode == ExecutionEvidenceSelectionModeSignedCampaignPackage {
		if err := validateSignedCampaignPackagePrivacy(bundle); err != nil {
			return ExecutionEvidenceImportReport{}, err
		}
	}
	snapshot := bundleAsSnapshot(bundle, selectionMode)
	mappingOptions := options
	if mappingOptions.SourceSystemID == "" {
		mappingOptions.SourceSystemID = DefaultSourceSystemID
	}
	if mappingOptions.AdapterManifestID == "" {
		mappingOptions.AdapterManifestID = DefaultExecutionEvidenceAdapterID
	}
	if mappingOptions.SourceVersion != "" && mappingOptions.SourceVersion != ExecutionEvidenceBundleSchema {
		return ExecutionEvidenceImportReport{}, fmt.Errorf(
			"source_version %q must match bundle schema_version %q",
			mappingOptions.SourceVersion,
			ExecutionEvidenceBundleSchema,
		)
	}
	mappingOptions.SourceVersion = SnapshotSchemaV2
	events, err := BuildSnapshotEvents(snapshot, mappingOptions)
	if err != nil {
		return ExecutionEvidenceImportReport{}, err
	}
	for _, event := range events {
		event["source_version"] = ExecutionEvidenceBundleSchema
		if raw, ok := event["raw_source_excerpt"].(map[string]any); ok {
			raw["source_profile_id"] = ExecutionEvidenceProfileID
			raw["source_profile_complete"] = bundle.Completeness.SourceProfileComplete
			raw["source_profile_manifest_sha256"] = ExecutionEvidenceProfileManifestSHA256
		}
	}
	if err := validateNormalizedEventBounds(events); err != nil {
		return ExecutionEvidenceImportReport{}, err
	}
	validator, err := evidence.NewSchemaValidator(schemaPath)
	if err != nil {
		return ExecutionEvidenceImportReport{}, err
	}
	ingester := evidence.NewInMemoryIngester(validator)
	report := SnapshotImportReport{
		Source:            "unirouter",
		SourceInstance:    bundle.SourceInstance,
		SnapshotSchema:    ExecutionEvidenceBundleSchema,
		SnapshotPath:      snapshotPath,
		GeneratedAt:       bundle.ClosedAt,
		EventsTotal:       len(events),
		CaptureGapEvents:  len(bundle.CaptureGaps),
		Snapshot:          snapshot,
		ArtifactSHA256:    artifactSHA256,
		SourceSystemID:    mappingOptions.SourceSystemID,
		AdapterManifestID: mappingOptions.AdapterManifestID,
	}
	for _, event := range events {
		result := ingester.Ingest(event)
		report.Results = append(report.Results, resultFromIngest(event, result))
		countOutcome(&report, result.Outcome)
	}
	if options.RerunCheck {
		for _, event := range events {
			result := ingester.Ingest(event)
			switch result.Outcome {
			case evidence.OutcomeDuplicate:
				report.RerunDuplicateNoop++
			case evidence.OutcomeRejected:
				report.RerunRejected++
			case evidence.OutcomeQuarantined:
				report.RerunQuarantined++
			}
		}
	}
	if options.EmitEvents {
		report.Events = events
	}
	return ExecutionEvidenceImportReport{SnapshotImportReport: report, Bundle: bundle}, nil
}

func validateSignedCampaignPackagePrivacy(bundle ExecutionEvidenceBundle) error {
	for index, fact := range bundle.TraceEvents {
		if fact.ActorRef != "" || fact.OnBehalfOf != "" || fact.Channel != "" {
			return fmt.Errorf(
				"signed campaign package trace_events[%d] contains a private actor or access locator",
				index,
			)
		}
	}
	for index, fact := range bundle.CallLogs {
		if fact.ActorRef != "" || fact.Channel != "" {
			return fmt.Errorf(
				"signed campaign package call_logs[%d] contains a private actor or access locator",
				index,
			)
		}
	}
	return nil
}

func normalizeExecutionEvidenceSelectionMode(value string) (string, error) {
	if value == "" {
		return ExecutionEvidenceSelectionModeCloseMarker, nil
	}
	switch value {
	case ExecutionEvidenceSelectionModeCloseMarker,
		ExecutionEvidenceSelectionModeSignedCampaignPackage:
		return value, nil
	default:
		return "", fmt.Errorf("unsupported execution evidence selection mode %q", value)
	}
}

func LoadExecutionEvidenceBundle(path string) (ExecutionEvidenceBundle, error) {
	bundle, _, err := loadExecutionEvidenceBundle(path)
	return bundle, err
}

func loadExecutionEvidenceBundle(path string) (ExecutionEvidenceBundle, string, error) {
	data, err := readSnapshotBytes(path)
	if err != nil {
		return ExecutionEvidenceBundle{}, "", err
	}
	return loadExecutionEvidenceBundleBytes(data)
}

func loadExecutionEvidenceBundleBytes(data []byte) (ExecutionEvidenceBundle, string, error) {
	if len(data) > MaxSnapshotBytes {
		return ExecutionEvidenceBundle{}, "", fmt.Errorf("UniRouter snapshot exceeds %d bytes", MaxSnapshotBytes)
	}
	if !utf8.Valid(data) {
		return ExecutionEvidenceBundle{}, "", errors.New("UniRouter execution evidence bundle must be valid UTF-8")
	}
	if err := scanStrictJSONDocument(
		data,
		"UniRouter execution evidence bundle",
		completedExecutionProhibitedKeys,
	); err != nil {
		return ExecutionEvidenceBundle{}, "", err
	}
	var bundle ExecutionEvidenceBundle
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bundle); err != nil {
		return ExecutionEvidenceBundle{}, "", fmt.Errorf("decode UniRouter execution evidence bundle: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return ExecutionEvidenceBundle{}, "", errors.New("UniRouter execution evidence bundle contains multiple JSON values")
		}
		return ExecutionEvidenceBundle{}, "", fmt.Errorf("decode trailing execution evidence bundle data: %w", err)
	}
	if err := validateExecutionEvidenceBundle(bundle); err != nil {
		return ExecutionEvidenceBundle{}, "", err
	}
	sum := sha256.Sum256(data)
	return bundle, "sha256:" + hex.EncodeToString(sum[:]), nil
}

func validateExecutionEvidenceBundle(bundle ExecutionEvidenceBundle) error {
	if bundle.SchemaVersion != ExecutionEvidenceBundleSchema ||
		bundle.Profile.ID != ExecutionEvidenceProfileID ||
		bundle.Profile.ManifestVersion != ExecutionEvidenceProfileManifestVersion ||
		bundle.Profile.ManifestSHA256 != ExecutionEvidenceProfileManifestSHA256 ||
		bundle.Profile.BundleSchemaSHA256 != ExecutionEvidenceBundleSchemaSHA256 {
		return errors.New("UniRouter execution evidence profile identity or digest mismatch")
	}
	if strings.TrimSpace(bundle.SourceInstance) == "" || len(bundle.SourceInstance) > 128 ||
		strings.TrimSpace(bundle.RequestID) == "" || len(bundle.RequestID) > 64 ||
		bundle.OrgID <= 0 ||
		(bundle.ProjectID != nil && *bundle.ProjectID <= 0) {
		return errors.New("UniRouter execution evidence identity is invalid")
	}
	closedAt, err := time.Parse(time.RFC3339Nano, bundle.ClosedAt)
	if err != nil {
		return fmt.Errorf("execution evidence closed_at: %w", err)
	}
	if len(bundle.TraceEvents) > MaxExecutionEvidenceTraceEvents ||
		len(bundle.CallLogs) > MaxExecutionEvidenceCallLogs ||
		len(bundle.CaptureGaps) > MaxExecutionEvidenceGaps {
		return errors.New("UniRouter execution evidence bundle exceeds bounded profile limits")
	}
	if len(bundle.StageCoverage) != len(executionEvidenceStages) {
		return errors.New("UniRouter execution evidence stage coverage must contain exactly seven stages")
	}
	observedTrace := make([]string, 0, len(bundle.TraceEvents))
	observedCalls := make([]string, 0, len(bundle.CallLogs))
	observedAll := make(map[string]struct{}, len(bundle.TraceEvents)+len(bundle.CallLogs))
	for index, fact := range bundle.TraceEvents {
		if err := validateTraceFact(index, fact, closedAt); err != nil {
			return err
		}
		observedTrace = append(observedTrace, fact.SourceEventHash)
		observedAll[fact.SourceEventHash] = struct{}{}
	}
	for index, fact := range bundle.CallLogs {
		if err := validateCallLogFact(index, fact, closedAt); err != nil {
			return err
		}
		observedCalls = append(observedCalls, fact.SourceEventHash)
		observedAll[fact.SourceEventHash] = struct{}{}
	}
	observedTrace, err = normalizeBundleHashes(observedTrace, MaxExecutionEvidenceTraceEvents)
	if err != nil {
		return err
	}
	observedCalls, err = normalizeBundleHashes(observedCalls, MaxExecutionEvidenceCallLogs)
	if err != nil {
		return err
	}
	if duplicate := duplicateBundleHash(observedTrace, observedCalls); duplicate != "" {
		return fmt.Errorf("source hash %q is used by both trace and call log", duplicate)
	}
	expectedTrace, err := normalizeBundleHashes(bundle.Completeness.ExpectedTraceEventHashes, MaxExecutionEvidenceTraceEvents)
	if err != nil {
		return err
	}
	expectedCalls, err := normalizeBundleHashes(bundle.Completeness.ExpectedCallLogHashes, MaxExecutionEvidenceCallLogs)
	if err != nil {
		return err
	}
	if duplicate := duplicateBundleHash(expectedTrace, expectedCalls); duplicate != "" {
		return fmt.Errorf("source hash %q is declared as both trace and call log", duplicate)
	}
	declaredObservedTrace, err := normalizeBundleHashes(bundle.Completeness.ObservedTraceEventHashes, MaxExecutionEvidenceTraceEvents)
	if err != nil {
		return err
	}
	declaredObservedCalls, err := normalizeBundleHashes(bundle.Completeness.ObservedCallLogHashes, MaxExecutionEvidenceCallLogs)
	if err != nil {
		return err
	}
	if !equalBundleStrings(observedTrace, declaredObservedTrace) || !equalBundleStrings(observedCalls, declaredObservedCalls) {
		return errors.New("UniRouter execution evidence observed hashes do not match bundle facts")
	}
	expectedAll := append(append([]string{}, expectedTrace...), expectedCalls...)
	observedHashes := append(append([]string{}, observedTrace...), observedCalls...)
	if unexpected := differenceStrings(observedHashes, expectedAll); len(unexpected) > 0 {
		return errors.New("UniRouter execution evidence contains facts not declared by the close marker")
	}
	missing := differenceStrings(expectedAll, observedHashes)
	declaredMissing, err := normalizeBundleHashes(bundle.Completeness.MissingEventHashes, MaxExecutionEvidenceTraceEvents+MaxExecutionEvidenceCallLogs)
	if err != nil {
		return err
	}
	if !equalBundleStrings(missing, declaredMissing) {
		return errors.New("UniRouter execution evidence missing hashes contradict expected and observed facts")
	}
	complete := len(missing) == 0 && len(bundle.CaptureGaps) == 0
	if bundle.Completeness.SourceProfileComplete != complete ||
		(complete && bundle.Completeness.State != "complete") ||
		(!complete && bundle.Completeness.State != "partial") {
		return errors.New("UniRouter execution evidence completeness claim is inconsistent")
	}
	for index, coverage := range bundle.StageCoverage {
		if coverage.Stage != executionEvidenceStages[index] {
			return errors.New("UniRouter execution evidence stages are not in canonical order")
		}
		switch coverage.Status {
		case "present", "gap", "not_applicable", "unknown":
		default:
			return fmt.Errorf("stage_coverage[%d] has invalid status", index)
		}
		hashes, err := normalizeBundleHashes(coverage.SourceEventHashes, MaxExecutionEvidenceTraceEvents)
		if err != nil {
			return err
		}
		if (coverage.Status == "present") != (len(hashes) > 0) {
			return fmt.Errorf("stage_coverage[%d] status contradicts its cited facts", index)
		}
		for _, hash := range hashes {
			if _, ok := observedAll[hash]; !ok {
				return fmt.Errorf("stage_coverage[%d] cites an unobserved source hash", index)
			}
		}
	}
	for index, gap := range bundle.CaptureGaps {
		if !requiredBundleStringWithin(gap.Code, 120) ||
			!validBundleGapStage(gap.Stage) ||
			!requiredBundleStringWithin(gap.Reason, 512) {
			return fmt.Errorf("capture_gaps[%d] is incomplete", index)
		}
		if gap.SourceEventHash != "" {
			if _, err := normalizeBundleHashes([]string{gap.SourceEventHash}, 1); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateTraceFact(index int, fact ExecutionEvidenceTraceFact, closedAt time.Time) error {
	createdAt, err := time.Parse(time.RFC3339Nano, fact.CreatedAt)
	if err != nil {
		return fmt.Errorf("trace_events[%d].created_at: %w", index, err)
	}
	if !requiredBundleStringWithin(fact.SourceRef, 512) ||
		!requiredBundleStringWithin(fact.EventType, 120) ||
		!requiredBundleStringWithin(fact.Stage, 80) ||
		createdAt.After(closedAt) ||
		fact.PayloadCaptureMode != "metadata_only" ||
		fact.LatencyMS < 0 ||
		!optionalBundleStringWithin(fact.TraceID, 128) ||
		!optionalBundleStringWithin(fact.SpanID, 128) ||
		!optionalBundleStringWithin(fact.ParentSpanID, 128) ||
		!optionalBundleStringWithin(fact.AttemptID, 128) ||
		!optionalBundleStringWithin(fact.ActorType, 64) ||
		!optionalBundleStringWithin(fact.ActorRef, 256) ||
		!optionalBundleStringWithin(fact.OnBehalfOf, 256) ||
		!optionalBundleStringWithin(fact.Model, 200) ||
		!optionalBundleStringWithin(fact.RequestedModel, 200) ||
		!optionalBundleStringWithin(fact.Provider, 120) ||
		!optionalBundleStringWithin(fact.Channel, 120) ||
		!optionalBundleStringWithin(fact.DecisionKind, 120) ||
		!optionalBundleStringWithin(fact.DecisionResult, 120) ||
		!optionalBundleStringWithin(fact.DecisionReason, 256) ||
		!optionalBundleStringWithin(fact.CaptureGap, 256) ||
		len(fact.Metadata) > 64 {
		return fmt.Errorf("trace_events[%d] violates the metadata-only source profile", index)
	}
	if _, err := normalizeBundleHashes([]string{fact.SourceEventHash}, 1); err != nil {
		return err
	}
	switch fact.Severity {
	case "info", "warning", "error":
	default:
		return fmt.Errorf("trace_events[%d] has invalid severity", index)
	}
	if fact.StatusCode != nil && (*fact.StatusCode < 0 || *fact.StatusCode > 999) {
		return fmt.Errorf("trace_events[%d] has invalid status code", index)
	}
	allowedMetadata := map[string]bool{
		"route_sequence": true, "route_total": true, "estimated_tokens": true,
		"prompt_tokens": true, "completion_tokens": true, "total_tokens": true,
		"cost_micros": true, "currency": true, "amount_scale": true,
		"usage_source": true, "outbox_version": true, "materialization_pending": true,
	}
	for key := range fact.Metadata {
		if !allowedMetadata[key] {
			return fmt.Errorf("trace_events[%d].metadata contains unapproved key %q", index, key)
		}
		if !validBundleMetadataScalar(fact.Metadata[key]) {
			return fmt.Errorf("trace_events[%d].metadata key %q is not scalar", index, key)
		}
	}
	return nil
}

func validateCallLogFact(index int, fact ExecutionEvidenceCallLogFact, closedAt time.Time) error {
	createdAt, err := time.Parse(time.RFC3339Nano, fact.CreatedAt)
	if err != nil {
		return fmt.Errorf("call_logs[%d].created_at: %w", index, err)
	}
	if !requiredBundleStringWithin(fact.SourceRef, 512) ||
		!requiredBundleStringWithin(fact.Model, 200) ||
		!requiredBundleStringWithin(fact.Provider, 120) ||
		createdAt.After(closedAt) ||
		fact.PayloadCaptureMode != "metadata_only" ||
		fact.Currency != MoneyCurrencyCNY || fact.AmountScale != MoneyAmountScale ||
		fact.StatusCode < 0 || fact.StatusCode > 999 ||
		fact.PromptTokens < 0 || fact.CompletionTokens < 0 || fact.LatencyMS < 0 ||
		!optionalBundleStringWithin(fact.TraceID, 128) ||
		!optionalBundleStringWithin(fact.AttemptID, 128) ||
		!optionalBundleStringWithin(fact.ActorType, 64) ||
		!optionalBundleStringWithin(fact.ActorRef, 256) ||
		!optionalBundleStringWithin(fact.RequestedModel, 200) ||
		!optionalBundleStringWithin(fact.Channel, 120) ||
		!optionalBundleStringWithin(fact.UsageSource, 80) ||
		!optionalBundleStringWithin(fact.UsageAuthority, 80) ||
		!optionalBundleStringWithin(fact.UsageFinality, 80) {
		return fmt.Errorf("call_logs[%d] violates the metadata-only money profile", index)
	}
	if fact.UpstreamStatusCode != nil &&
		(*fact.UpstreamStatusCode < 0 || *fact.UpstreamStatusCode > 999) {
		return fmt.Errorf("call_logs[%d] has invalid upstream status code", index)
	}
	if err := validateCostMicros(fact.CostMicros); err != nil {
		return fmt.Errorf("call_logs[%d]: %w", index, err)
	}
	if _, err := normalizeBundleHashes([]string{fact.SourceEventHash}, 1); err != nil {
		return err
	}
	return nil
}

func duplicateBundleHash(left, right []string) string {
	seen := make(map[string]struct{}, len(left))
	for _, value := range left {
		seen[value] = struct{}{}
	}
	for _, value := range right {
		if _, ok := seen[value]; ok {
			return value
		}
	}
	return ""
}

func requiredBundleStringWithin(value string, maximum int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= maximum
}

func optionalBundleStringWithin(value string, maximum int) bool {
	return len(value) <= maximum
}

func validBundleGapStage(value string) bool {
	switch value {
	case "identity", "routing", "provider", "retry_fallback", "billing", "usage", "logging", "source":
		return true
	default:
		return false
	}
}

func validBundleMetadataScalar(value any) bool {
	switch value.(type) {
	case nil, bool, string, float64, json.Number:
		return true
	default:
		return false
	}
}

func bundleAsSnapshot(bundle ExecutionEvidenceBundle, selectionMode string) SnapshotFile {
	snapshot := SnapshotFile{
		SchemaVersion:  SnapshotSchemaV2,
		GeneratedAt:    bundle.ClosedAt,
		Source:         "unirouter",
		SourceInstance: bundle.SourceInstance,
		RequestIDs:     []string{bundle.RequestID},
		Safety: SnapshotSafety{
			SelectionMode:        selectionMode,
			RawPromptsIncluded:   false,
			RawResponsesIncluded: false,
			APIKeysIncluded:      false,
			IPHandling:           "raw IP addresses are not included",
			ErrorHandling:        "raw upstream errors are not included",
			MetadataHandling:     "only the source-profile metadata allowlist is included",
		},
	}
	for index, fact := range bundle.TraceEvents {
		metadataKeys := make([]string, 0, len(fact.Metadata))
		for key := range fact.Metadata {
			metadataKeys = append(metadataKeys, key)
		}
		sort.Strings(metadataKeys)
		snapshot.TraceEvents = append(snapshot.TraceEvents, SnapshotTraceEvent{
			SourceRef: fact.SourceRef, SourceEventHash: fact.SourceEventHash, ID: int64(index + 1),
			RequestID: bundle.RequestID, OrgID: bundle.OrgID, ProjectID: bundle.ProjectID, KeyType: "unknown",
			EventType: fact.EventType, Stage: fact.Stage, Severity: fact.Severity,
			Model: fact.Model, RequestedModel: fact.RequestedModel, Provider: fact.Provider, Channel: fact.Channel,
			StatusCode: fact.StatusCode, LatencyMS: fact.LatencyMS, Metadata: fact.Metadata, MetadataKeys: metadataKeys,
			CreatedAt: fact.CreatedAt, TraceID: nonEmptyStringPointer(fact.TraceID), SpanID: nonEmptyStringPointer(fact.SpanID),
			ParentSpanID: nonEmptyStringPointer(fact.ParentSpanID), CaptureGap: nonEmptyStringPointer(fact.CaptureGap),
			EvidenceRef: nonEmptyStringPointer(fact.SourceRef), DecisionKind: nonEmptyStringPointer(fact.DecisionKind),
			DecisionResult: nonEmptyStringPointer(fact.DecisionResult), DecisionReason: nonEmptyStringPointer(fact.DecisionReason),
			ActorType: nonEmptyStringPointer(fact.ActorType), ActorRef: nonEmptyStringPointer(fact.ActorRef),
			OnBehalfOf: nonEmptyStringPointer(fact.OnBehalfOf), PayloadCaptureMode: fact.PayloadCaptureMode,
			PayloadCaptureReason: "metadata-only execution evidence profile",
			DerivedCaptureMode:   "native", DerivedSchemaVersion: "unirouter.evidence.v1",
			DerivedSourceComponent: fact.Stage,
		})
	}
	for index, fact := range bundle.CallLogs {
		currency := fact.Currency
		amountScale := fact.AmountScale
		moneySchema := MoneySchemaMicroCNY
		costMicros := fact.CostMicros
		snapshot.CallLogs = append(snapshot.CallLogs, SnapshotCallLog{
			SourceRef: fact.SourceRef, SourceEventHash: fact.SourceEventHash, ID: int64(index + 1),
			RequestID: bundle.RequestID, OrgID: bundle.OrgID, ProjectID: bundle.ProjectID, KeyType: "unknown",
			Model: fact.Model, RequestedModel: fact.RequestedModel, Provider: fact.Provider, Channel: fact.Channel,
			PromptTokens: fact.PromptTokens, CompletionTokens: fact.CompletionTokens,
			TotalTokens:     fact.PromptTokens + fact.CompletionTokens,
			EstimatedTokens: fact.PromptTokens + fact.CompletionTokens,
			UsageSource:     fact.UsageSource, UsageConfidence: "1.000",
			CostMicros: &costMicros, Currency: &currency, AmountScale: &amountScale, MoneySchema: &moneySchema,
			LatencyMS: fact.LatencyMS, StatusCode: fact.StatusCode, UpstreamStatusCode: fact.UpstreamStatusCode,
			CreatedAt: fact.CreatedAt, TraceID: nonEmptyStringPointer(fact.TraceID), EvidenceRef: nonEmptyStringPointer(fact.SourceRef),
			ActorType: nonEmptyStringPointer(fact.ActorType), ActorRef: nonEmptyStringPointer(fact.ActorRef),
			PayloadCaptureMode:   fact.PayloadCaptureMode,
			PayloadCaptureReason: "metadata-only execution evidence profile",
			DerivedCaptureMode:   "native", DerivedSchemaVersion: "unirouter.evidence.v1",
			DerivedSourceComponent: "logging",
			costMicrosPresent:      true, currencyPresent: true, amountScalePresent: true, moneySchemaPresent: true,
		})
	}
	for _, gap := range bundle.CaptureGaps {
		snapshot.CaptureGaps = append(snapshot.CaptureGaps, SnapshotCaptureGap{
			RequestID: bundle.RequestID,
			Source:    gap.Stage,
			Gap:       gap.Code,
			Reason:    gap.Reason,
			Generated: true,
		})
	}
	snapshot.Counts = SnapshotCounts{
		RequestedRequestIDs: 1,
		CallLogs:            len(snapshot.CallLogs),
		TraceEvents:         len(snapshot.TraceEvents),
		CaptureGaps:         len(snapshot.CaptureGaps),
	}
	snapshot.Notes = map[string]any{
		"source_profile_id":              ExecutionEvidenceProfileID,
		"source_profile_manifest_sha256": ExecutionEvidenceProfileManifestSHA256,
		"source_profile_complete":        bundle.Completeness.SourceProfileComplete,
	}
	return snapshot
}

func normalizeBundleHashes(values []string, limit int) ([]string, error) {
	if len(values) > limit {
		return nil, fmt.Errorf("execution evidence hash list exceeds %d", limit)
	}
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		if len(value) != 64 || value != strings.ToLower(value) {
			return nil, fmt.Errorf("invalid execution evidence source hash %q", value)
		}
		if _, err := hex.DecodeString(value); err != nil {
			return nil, fmt.Errorf("invalid execution evidence source hash %q", value)
		}
		if _, exists := unique[value]; exists {
			return nil, fmt.Errorf("duplicate execution evidence source hash %q", value)
		}
		unique[value] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func differenceStrings(expected, observed []string) []string {
	seen := make(map[string]struct{}, len(observed))
	for _, value := range observed {
		seen[value] = struct{}{}
	}
	result := make([]string, 0)
	for _, value := range expected {
		if _, ok := seen[value]; !ok {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func equalBundleStrings(left, right []string) bool {
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

func nonEmptyStringPointer(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}
