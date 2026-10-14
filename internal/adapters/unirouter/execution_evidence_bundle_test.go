package unirouter

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutionEvidenceProfilePinAndRichBundleImport(t *testing.T) {
	pinPath := filepath.Join("..", "..", "..", "contracts", "execution-evidence", "v2", "unirouter.execution-evidence.v2.pin.json")
	pinBytes, err := os.ReadFile(pinPath)
	if err != nil {
		t.Fatal(err)
	}
	var pin struct {
		ProfileID       string `json:"profile_id"`
		ManifestVersion string `json:"manifest_version"`
		ManifestSHA256  string `json:"manifest_sha256"`
		BundleSchema    struct {
			SchemaVersion string `json:"schema_version"`
			SHA256        string `json:"sha256"`
		} `json:"bundle_schema"`
	}
	if err := json.Unmarshal(pinBytes, &pin); err != nil {
		t.Fatal(err)
	}
	if pin.ProfileID != ExecutionEvidenceProfileID ||
		pin.ManifestVersion != ExecutionEvidenceProfileManifestVersion ||
		pin.ManifestSHA256 != ExecutionEvidenceProfileManifestSHA256 ||
		pin.BundleSchema.SchemaVersion != ExecutionEvidenceBundleSchema ||
		pin.BundleSchema.SHA256 != ExecutionEvidenceBundleSchemaSHA256 {
		t.Fatalf("execution evidence pin drifted: %+v", pin)
	}

	bundle := recoveredFallbackBundle()
	path := writeExecutionEvidenceBundle(t, bundle)
	report, err := ImportExecutionEvidenceBundle(
		path,
		filepath.Join("..", "..", "..", "schemas", "m0", "evidence-event.schema.json"),
		SnapshotImportOptions{EmitEvents: true, RerunCheck: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.SnapshotSchema != ExecutionEvidenceBundleSchema ||
		report.AdapterManifestID != DefaultExecutionEvidenceAdapterID ||
		!report.Bundle.Completeness.SourceProfileComplete {
		t.Fatalf("execution evidence import identity mismatch: %+v", report)
	}
	if report.Accepted != len(report.Events) || report.Rejected != 0 || report.Quarantined != 0 {
		t.Fatalf("execution evidence import outcomes: %+v", report.SnapshotImportReport)
	}
	if report.RerunDuplicateNoop != len(report.Events) {
		t.Fatalf("execution evidence replay was not duplicate-noop: %+v", report.SnapshotImportReport)
	}
	eventTypes := make(map[string]bool)
	serialized, err := json.Marshal(report.Events)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range report.Events {
		eventTypes[stringValue(event["event_type"])] = true
		if event["source_version"] != ExecutionEvidenceBundleSchema {
			t.Fatalf("event did not retain bundle source version: %#v", event)
		}
	}
	for _, required := range []string{
		"provider.request.failed",
		"retry.attempt.scheduled",
		"fallback.route.selected",
		"usage.tokens.recorded",
	} {
		if !eventTypes[required] {
			t.Fatalf("missing normalized event type %s: %+v", required, eventTypes)
		}
	}
	if strings.Contains(string(serialized), "raw-prompt-value") ||
		strings.Contains(string(serialized), "203.0.113.50") {
		t.Fatal("execution evidence adapter exposed forbidden raw content")
	}
}

func TestExecutionEvidenceBundleImportsRuntimeCloseMarkerUnavailableAsPartial(t *testing.T) {
	bundle := runtimeCloseMarkerUnavailableBundle()
	path := writeExecutionEvidenceBundle(t, bundle)
	schemaPath := filepath.Join("..", "..", "..", "schemas", "m0", "evidence-event.schema.json")
	report, err := ImportExecutionEvidenceBundleWithContext(
		path,
		schemaPath,
		SnapshotImportOptions{EmitEvents: true, RerunCheck: true},
		ExecutionEvidenceImportContext{SelectionMode: ExecutionEvidenceSelectionModeSignedCampaignPackage},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Bundle.Completeness.State != "partial" || report.Bundle.Completeness.SourceProfileComplete {
		t.Fatalf("runtime close marker gap was not retained as partial: %+v", report.Bundle.Completeness)
	}
	if report.Snapshot.Safety.SelectionMode != ExecutionEvidenceSelectionModeSignedCampaignPackage {
		t.Fatalf("selection_mode=%q", report.Snapshot.Safety.SelectionMode)
	}
	if report.CaptureGapEvents != 1 || len(report.Snapshot.CaptureGaps) != 1 {
		t.Fatalf("capture gap projection mismatch: report=%+v snapshot=%+v", report.SnapshotImportReport, report.Snapshot.CaptureGaps)
	}
	if report.Accepted != len(report.Events) || report.Rejected != 0 || report.Quarantined != 0 {
		t.Fatalf("partial execution evidence outcomes: %+v", report.SnapshotImportReport)
	}
	if report.RerunDuplicateNoop != len(report.Events) {
		t.Fatalf("partial execution evidence replay was not duplicate-noop: %+v", report.SnapshotImportReport)
	}
	if len(report.Bundle.StageCoverage) != len(executionEvidenceStages) {
		t.Fatalf("stage coverage count=%d", len(report.Bundle.StageCoverage))
	}
	for index, coverage := range report.Bundle.StageCoverage {
		if coverage.Status != "gap" && coverage.Status != "unknown" {
			t.Fatalf("stage_coverage[%d]=%+v, want gap or unknown", index, coverage)
		}
	}

	gapEventFound := false
	for _, event := range report.Events {
		raw, ok := event["raw_source_excerpt"].(map[string]any)
		if !ok {
			t.Fatalf("event lacks raw_source_excerpt: %#v", event)
		}
		if complete, ok := raw["source_profile_complete"].(bool); !ok || complete {
			t.Fatalf("event incorrectly marks source profile complete: %#v", event)
		}
		if stringValue(event["event_type"]) == "gap.capture.detected" &&
			stringValue(raw["capture_gap"]) == "runtime_close_marker_unavailable" {
			gapEventFound = true
		}
	}
	if !gapEventFound {
		t.Fatalf("runtime_close_marker_unavailable did not produce a gap M0 event: %+v", report.Events)
	}
	serialized, err := json.Marshal(report.Events)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(serialized, []byte(`"timeline_complete":true`)) ||
		bytes.Contains(serialized, []byte(`"source_profile_complete":true`)) {
		t.Fatalf("partial bundle was marked complete: %s", serialized)
	}

	looseReport, err := ImportExecutionEvidenceBundle(
		path,
		schemaPath,
		SnapshotImportOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if looseReport.Snapshot.Safety.SelectionMode != ExecutionEvidenceSelectionModeCloseMarker {
		t.Fatalf("loose importer selection_mode=%q", looseReport.Snapshot.Safety.SelectionMode)
	}
}

func TestExecutionEvidenceBundleImportContextRejectsUnknownSelectionMode(t *testing.T) {
	path := writeExecutionEvidenceBundle(t, recoveredFallbackBundle())
	_, err := ImportExecutionEvidenceBundleWithContext(
		path,
		filepath.Join("..", "..", "..", "schemas", "m0", "evidence-event.schema.json"),
		SnapshotImportOptions{},
		ExecutionEvidenceImportContext{SelectionMode: "unreviewed_outer_package"},
	)
	if err == nil || !strings.Contains(err.Error(), "unsupported execution evidence selection mode") {
		t.Fatalf("unknown selection mode error=%v", err)
	}
}

func TestSignedCampaignPackageRejectsPrivateActorAndAccessLocatorsBeforeProjection(t *testing.T) {
	bundle := recoveredFallbackBundle()
	for index := range bundle.TraceEvents {
		bundle.TraceEvents[index].ActorRef = ""
		bundle.TraceEvents[index].OnBehalfOf = ""
		bundle.TraceEvents[index].Channel = ""
	}
	for index := range bundle.CallLogs {
		bundle.CallLogs[index].ActorRef = ""
		bundle.CallLogs[index].Channel = ""
	}
	bundle.TraceEvents[0].ActorRef = "service:private-locator"
	encoded, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	schemaPath := filepath.Join("..", "..", "..", "schemas", "m0", "evidence-event.schema.json")
	if _, err := ImportExecutionEvidenceBundleBytesWithContext(
		encoded,
		schemaPath,
		SnapshotImportOptions{},
		ExecutionEvidenceImportContext{SelectionMode: ExecutionEvidenceSelectionModeSignedCampaignPackage},
	); err == nil || !strings.Contains(err.Error(), "private actor or access locator") {
		t.Fatalf("private package locator error=%v", err)
	}
	if _, err := ImportExecutionEvidenceBundleBytesWithContext(
		encoded,
		schemaPath,
		SnapshotImportOptions{},
		ExecutionEvidenceImportContext{SelectionMode: ExecutionEvidenceSelectionModeCloseMarker},
	); err != nil {
		t.Fatalf("legacy close-marker contract changed: %v", err)
	}
}

func TestExecutionEvidenceBundlePathAndBytesImportsAreIdentical(t *testing.T) {
	bundle := runtimeCloseMarkerUnavailableBundle()
	path := writeExecutionEvidenceBundle(t, bundle)
	memberBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	schemaPath := filepath.Join("..", "..", "..", "schemas", "m0", "evidence-event.schema.json")
	options := SnapshotImportOptions{EmitEvents: true, RerunCheck: true}
	context := ExecutionEvidenceImportContext{SelectionMode: ExecutionEvidenceSelectionModeSignedCampaignPackage}
	pathReport, err := ImportExecutionEvidenceBundleWithContext(path, schemaPath, options, context)
	if err != nil {
		t.Fatal(err)
	}
	bytesReport, err := ImportExecutionEvidenceBundleBytesWithContext(memberBytes, schemaPath, options, context)
	if err != nil {
		t.Fatal(err)
	}
	wantArtifactSHA256 := sha256.Sum256(memberBytes)
	wantArtifact := "sha256:" + hex.EncodeToString(wantArtifactSHA256[:])
	if pathReport.ArtifactSHA256 != wantArtifact || bytesReport.ArtifactSHA256 != wantArtifact {
		t.Fatalf("artifact identity mismatch: path=%q bytes=%q want=%q", pathReport.ArtifactSHA256, bytesReport.ArtifactSHA256, wantArtifact)
	}
	if pathReport.SnapshotPath != path || bytesReport.SnapshotPath != "" {
		t.Fatalf("snapshot path mismatch: path=%q bytes=%q", pathReport.SnapshotPath, bytesReport.SnapshotPath)
	}
	if len(pathReport.Results) != len(bytesReport.Results) {
		t.Fatalf("result count mismatch: path=%d bytes=%d", len(pathReport.Results), len(bytesReport.Results))
	}
	for index := range pathReport.Results {
		pathResult := pathReport.Results[index]
		bytesResult := bytesReport.Results[index]
		if pathResult.SourceRef != bytesResult.SourceRef ||
			pathResult.EventType != bytesResult.EventType ||
			pathResult.IdempotencyKey != bytesResult.IdempotencyKey ||
			pathResult.EventHash != bytesResult.EventHash ||
			pathResult.Outcome != bytesResult.Outcome {
			t.Fatalf("result[%d] identity mismatch: path=%+v bytes=%+v", index, pathResult, bytesResult)
		}
	}
}

func TestExecutionEvidenceBundleBytesFailsClosedOnTamperAndDuplicateFields(t *testing.T) {
	encoded, err := json.Marshal(recoveredFallbackBundle())
	if err != nil {
		t.Fatal(err)
	}
	schemaPath := filepath.Join("..", "..", "..", "schemas", "m0", "evidence-event.schema.json")
	context := ExecutionEvidenceImportContext{SelectionMode: ExecutionEvidenceSelectionModeSignedCampaignPackage}

	tampered := bytes.Replace(
		encoded,
		[]byte(ExecutionEvidenceProfileManifestSHA256),
		[]byte("sha256:"+strings.Repeat("0", 64)),
		1,
	)
	if bytes.Equal(tampered, encoded) {
		t.Fatal("test setup did not tamper with the profile digest")
	}
	if _, err := ImportExecutionEvidenceBundleBytesWithContext(
		tampered,
		schemaPath,
		SnapshotImportOptions{},
		context,
	); err == nil || !strings.Contains(err.Error(), "profile identity or digest mismatch") {
		t.Fatalf("tampered member error=%v", err)
	}

	duplicateNeedle := []byte(`"profile":{"id":"unirouter.execution-evidence.v2",`)
	duplicateReplacement := []byte(`"profile":{"id":"unirouter.execution-evidence.v2","id":"unirouter.execution-evidence.v2",`)
	duplicate := bytes.Replace(encoded, duplicateNeedle, duplicateReplacement, 1)
	if bytes.Equal(duplicate, encoded) {
		t.Fatal("test setup did not insert a duplicate profile field")
	}
	if _, err := ImportExecutionEvidenceBundleBytesWithContext(
		duplicate,
		schemaPath,
		SnapshotImportOptions{},
		context,
	); err == nil || !strings.Contains(err.Error(), "duplicate field at $.profile.id") {
		t.Fatalf("duplicate member error=%v", err)
	}
}

func TestExecutionEvidenceBundleFailsClosedOnDigestUnknownFieldAndContradictoryCompleteness(t *testing.T) {
	base := recoveredFallbackBundle()
	tests := []struct {
		name    string
		mutate  func(map[string]any)
		wantErr string
	}{
		{
			name: "profile digest",
			mutate: func(raw map[string]any) {
				raw["profile"].(map[string]any)["manifest_sha256"] = "sha256:" + strings.Repeat("0", 64)
			},
			wantErr: "profile identity or digest mismatch",
		},
		{
			name: "unknown raw content field",
			mutate: func(raw map[string]any) {
				raw["raw_prompt"] = "raw-prompt-value"
			},
			wantErr: "unknown field",
		},
		{
			name: "contradictory complete claim",
			mutate: func(raw map[string]any) {
				completeness := raw["completeness"].(map[string]any)
				completeness["missing_event_hashes"] = []any{hashText("not-observed")}
			},
			wantErr: "missing hashes contradict",
		},
		{
			name: "capture gap contradicts complete claim",
			mutate: func(raw map[string]any) {
				raw["capture_gaps"] = []any{map[string]any{
					"code":   "runtime_close_marker_unavailable",
					"stage":  "source",
					"reason": "the runtime cannot attest the terminal close marker",
				}}
			},
			wantErr: "completeness claim is inconsistent",
		},
		{
			name: "fact absent from close marker",
			mutate: func(raw map[string]any) {
				extraHash := hashText("unexpected-observed-fact")
				traceEvents := raw["trace_events"].([]any)
				first := traceEvents[0].(map[string]any)
				extra := make(map[string]any, len(first))
				for key, value := range first {
					extra[key] = value
				}
				extra["source_ref"] = "unirouter:trace:unexpected"
				extra["source_event_hash"] = extraHash
				traceEvents = append(traceEvents, extra)
				raw["trace_events"] = traceEvents
				completeness := raw["completeness"].(map[string]any)
				observed := completeness["observed_trace_event_hashes"].([]any)
				completeness["observed_trace_event_hashes"] = append(observed, extraHash)
			},
			wantErr: "not declared by the close marker",
		},
		{
			name: "cross-kind hash reuse",
			mutate: func(raw map[string]any) {
				traceHash := raw["trace_events"].([]any)[0].(map[string]any)["source_event_hash"].(string)
				raw["call_logs"].([]any)[0].(map[string]any)["source_event_hash"] = traceHash
				completeness := raw["completeness"].(map[string]any)
				completeness["expected_call_log_hashes"] = []any{traceHash}
				completeness["observed_call_log_hashes"] = []any{traceHash}
			},
			wantErr: "both trace and call log",
		},
		{
			name: "false present stage coverage",
			mutate: func(raw map[string]any) {
				raw["stage_coverage"].([]any)[0].(map[string]any)["source_event_hashes"] = []any{}
			},
			wantErr: "status contradicts",
		},
		{
			name: "nested metadata scalar",
			mutate: func(raw map[string]any) {
				raw["trace_events"].([]any)[0].(map[string]any)["metadata"].(map[string]any)["route_sequence"] =
					map[string]any{"nested": true}
			},
			wantErr: "not scalar",
		},
		{
			name: "future trace fact",
			mutate: func(raw map[string]any) {
				raw["trace_events"].([]any)[0].(map[string]any)["created_at"] = "2026-07-30T06:00:02Z"
			},
			wantErr: "metadata-only source profile",
		},
		{
			name: "invalid project identity",
			mutate: func(raw map[string]any) {
				raw["project_id"] = float64(0)
			},
			wantErr: "identity is invalid",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			encoded, err := json.Marshal(base)
			if err != nil {
				t.Fatal(err)
			}
			var raw map[string]any
			if err := json.Unmarshal(encoded, &raw); err != nil {
				t.Fatal(err)
			}
			testCase.mutate(raw)
			path := filepath.Join(t.TempDir(), "bundle.json")
			mutated, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, mutated, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadExecutionEvidenceBundle(path); err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("got err=%v want substring %q", err, testCase.wantErr)
			}
		})
	}
}

func TestExecutionEvidenceBundleRejectsAmbiguousSourceBytes(t *testing.T) {
	encoded, err := json.Marshal(recoveredFallbackBundle())
	if err != nil {
		t.Fatal(err)
	}

	duplicateNeedle := []byte(`"profile":{"id":"unirouter.execution-evidence.v2",`)
	duplicateReplacement := []byte(`"profile":{"id":"unirouter.execution-evidence.v2","id":"unirouter.execution-evidence.v2",`)
	duplicate := bytes.Replace(encoded, duplicateNeedle, duplicateReplacement, 1)
	if bytes.Equal(duplicate, encoded) {
		t.Fatal("test setup did not insert a nested duplicate key")
	}
	duplicatePath := filepath.Join(t.TempDir(), "duplicate.json")
	if err := os.WriteFile(duplicatePath, duplicate, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadExecutionEvidenceBundle(duplicatePath); err == nil ||
		!strings.Contains(err.Error(), "duplicate field at $.profile.id") {
		t.Fatalf("duplicate-key error=%v", err)
	}

	invalidUTF8 := append([]byte(nil), encoded...)
	identityOffset := bytes.Index(invalidUTF8, []byte("demo-unirouter"))
	if identityOffset < 0 {
		t.Fatal("test setup did not find the source identity")
	}
	invalidUTF8[identityOffset] = 0xff
	invalidPath := filepath.Join(t.TempDir(), "invalid-utf8.json")
	if err := os.WriteFile(invalidPath, invalidUTF8, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadExecutionEvidenceBundle(invalidPath); err == nil ||
		!strings.Contains(err.Error(), "must be valid UTF-8") {
		t.Fatalf("invalid UTF-8 error=%v", err)
	}
}

func recoveredFallbackBundle() ExecutionEvidenceBundle {
	routeHash := hashText("route")
	providerFailedHash := hashText("provider-failed")
	retryHash := hashText("retry")
	fallbackHash := hashText("fallback")
	billingHash := hashText("billing")
	callHash := hashText("call")
	allTrace := []string{routeHash, providerFailedHash, retryHash, fallbackHash, billingHash}
	return ExecutionEvidenceBundle{
		SchemaVersion: ExecutionEvidenceBundleSchema,
		Profile: ExecutionEvidenceProfileRef{
			ID:                 ExecutionEvidenceProfileID,
			ManifestVersion:    ExecutionEvidenceProfileManifestVersion,
			ManifestSHA256:     ExecutionEvidenceProfileManifestSHA256,
			BundleSchemaSHA256: ExecutionEvidenceBundleSchemaSHA256,
		},
		SourceInstance: "demo-unirouter",
		RequestID:      "req-v2-fallback-demo",
		OrgID:          7,
		ClosedAt:       "2026-07-30T06:00:01Z",
		Completeness: ExecutionEvidenceCompleteness{
			State:                    "complete",
			SourceProfileComplete:    true,
			ExpectedTraceEventHashes: append([]string(nil), allTrace...),
			ExpectedCallLogHashes:    []string{callHash},
			ObservedTraceEventHashes: append([]string(nil), allTrace...),
			ObservedCallLogHashes:    []string{callHash},
			MissingEventHashes:       []string{},
		},
		StageCoverage: []ExecutionEvidenceStageCoverage{
			{Stage: "identity", Status: "present", SourceEventHashes: []string{routeHash}},
			{Stage: "routing", Status: "present", SourceEventHashes: []string{routeHash, retryHash, fallbackHash}},
			{Stage: "provider", Status: "present", SourceEventHashes: []string{providerFailedHash}},
			{Stage: "retry_fallback", Status: "present", SourceEventHashes: []string{retryHash, fallbackHash}},
			{Stage: "billing", Status: "present", SourceEventHashes: []string{billingHash}},
			{Stage: "usage", Status: "present", SourceEventHashes: []string{callHash}},
			{Stage: "logging", Status: "present", SourceEventHashes: []string{callHash}},
		},
		TraceEvents: []ExecutionEvidenceTraceFact{
			{
				SourceRef: "unirouter:trace:route", SourceEventHash: routeHash,
				EventType: "routing.route_selected", Stage: "routing", Severity: "info",
				CreatedAt: "2026-07-30T06:00:00Z", TraceID: "trace-v2", SpanID: "span-route",
				ActorType: "service", ActorRef: "service:demo", Model: "model-a",
				RequestedModel: "model-a", Provider: "provider-a", PayloadCaptureMode: "metadata_only",
				Metadata: map[string]any{"route_sequence": float64(1), "route_total": float64(2)},
			},
			{
				SourceRef: "unirouter:trace:failed", SourceEventHash: providerFailedHash,
				EventType: "provider.request_failed", Stage: "provider", Severity: "warning",
				CreatedAt: "2026-07-30T06:00:00.100Z", TraceID: "trace-v2", SpanID: "span-failed",
				ParentSpanID: "span-route", AttemptID: "attempt-a", Provider: "provider-a",
				StatusCode: int64Pointer(502), LatencyMS: 80, PayloadCaptureMode: "metadata_only",
				Metadata: map[string]any{"route_sequence": float64(1), "route_total": float64(2)},
			},
			{
				SourceRef: "unirouter:trace:retry", SourceEventHash: retryHash,
				EventType: "retry.attempt.scheduled", Stage: "routing", Severity: "info",
				CreatedAt: "2026-07-30T06:00:00.200Z", TraceID: "trace-v2", SpanID: "span-retry",
				ParentSpanID: "span-failed", AttemptID: "attempt-b", Provider: "provider-b",
				PayloadCaptureMode: "metadata_only",
				Metadata:           map[string]any{"route_sequence": float64(2), "route_total": float64(2)},
			},
			{
				SourceRef: "unirouter:trace:fallback", SourceEventHash: fallbackHash,
				EventType: "fallback.route.selected", Stage: "routing", Severity: "info",
				CreatedAt: "2026-07-30T06:00:00.300Z", TraceID: "trace-v2", SpanID: "span-fallback",
				ParentSpanID: "span-retry", AttemptID: "attempt-b", Provider: "provider-b",
				PayloadCaptureMode: "metadata_only",
				Metadata:           map[string]any{"route_sequence": float64(2), "route_total": float64(2)},
			},
			{
				SourceRef: "unirouter:trace:billing", SourceEventHash: billingHash,
				EventType: "billing.settle.succeeded", Stage: "billing", Severity: "info",
				CreatedAt: "2026-07-30T06:00:00.900Z", TraceID: "trace-v2", SpanID: "span-billing",
				ParentSpanID: "span-fallback", AttemptID: "attempt-b", Provider: "provider-b",
				PayloadCaptureMode: "metadata_only", Metadata: map[string]any{"cost_micros": "17"},
			},
		},
		CallLogs: []ExecutionEvidenceCallLogFact{{
			SourceRef: "unirouter:call:req-v2-fallback-demo", SourceEventHash: callHash,
			CreatedAt: "2026-07-30T06:00:01Z", TraceID: "trace-v2", AttemptID: "attempt-b",
			ActorType: "service", ActorRef: "service:demo", Model: "model-a", RequestedModel: "model-a",
			Provider: "provider-b", Channel: "fallback", StatusCode: 200, UpstreamStatusCode: int64Pointer(200),
			LatencyMS: 140, PromptTokens: 7, CompletionTokens: 4, CostMicros: "17",
			Currency: "CNY", AmountScale: 1_000_000, UsageSource: "upstream",
			UsageAuthority: "provider_reported", UsageFinality: "final",
			PayloadCaptureMode: "metadata_only",
		}},
		CaptureGaps: []ExecutionEvidenceGap{},
	}
}

func runtimeCloseMarkerUnavailableBundle() ExecutionEvidenceBundle {
	bundle := recoveredFallbackBundle()
	for index := range bundle.TraceEvents {
		bundle.TraceEvents[index].ActorRef = ""
		bundle.TraceEvents[index].OnBehalfOf = ""
		bundle.TraceEvents[index].Channel = ""
	}
	for index := range bundle.CallLogs {
		bundle.CallLogs[index].ActorRef = ""
		bundle.CallLogs[index].Channel = ""
	}
	bundle.Completeness.State = "partial"
	bundle.Completeness.SourceProfileComplete = false
	bundle.CaptureGaps = []ExecutionEvidenceGap{{
		Code:   "runtime_close_marker_unavailable",
		Stage:  "source",
		Reason: "the runtime cannot attest the terminal close marker",
	}}
	for index := range bundle.StageCoverage {
		bundle.StageCoverage[index].SourceEventHashes = nil
		if index%2 == 0 {
			bundle.StageCoverage[index].Status = "gap"
		} else {
			bundle.StageCoverage[index].Status = "unknown"
		}
	}
	return bundle
}

func writeExecutionEvidenceBundle(t *testing.T, bundle ExecutionEvidenceBundle) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "execution-evidence-v2.json")
	encoded, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func hashText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func int64Pointer(value int64) *int64 {
	return &value
}
