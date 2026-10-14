package unirouter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testStrictIPHandling     = "raw IP addresses are not exported; only ip_address_was_present and ip_address_redacted boolean fields are retained"
	testHistoricalIPHandling = "raw IP addresses are not exported; only ip_address_was_present is retained"
)

func TestPreflightSanitizedSnapshotPassesBoundedApprovedV2(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)

	report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot: %v", err)
	}
	if report.Status != "pass" || len(report.Findings) != 0 {
		t.Fatalf("report=%+v", report)
	}
	if report.RequestIDs != 1 || report.EventsTotal != 2 || !report.SingleOrg {
		t.Fatalf("unexpected bounded inventory: %+v", report)
	}
}

func TestPreflightSanitizedSnapshotAcceptsRedactedIPPresenceProof(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	snapshot.Safety.IPHandling = testStrictIPHandling
	snapshot.CallLogs[0].IPAddressWasPresent = true
	snapshot.CallLogs[0].IPAddressRedacted = true
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)

	report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot: %v", err)
	}
	if report.Status != "pass" || len(report.Findings) != 0 {
		t.Fatalf("report=%+v", report)
	}
}

func TestPreflightSanitizedSnapshotAcceptsStrictNoIPProof(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	snapshot.Safety.IPHandling = testStrictIPHandling
	snapshot.CallLogs[0].IPAddressWasPresent = false
	snapshot.CallLogs[0].IPAddressRedacted = false
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)

	report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot: %v", err)
	}
	if report.Status != "pass" || len(report.Findings) != 0 {
		t.Fatalf("report=%+v", report)
	}
}

func TestPreflightSanitizedSnapshotRejectsPresenceWithoutRedactionProof(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	snapshot.Safety.IPHandling = testStrictIPHandling
	snapshot.CallLogs[0].IPAddressWasPresent = true
	snapshot.CallLogs[0].IPAddressRedacted = false
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)

	report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot: %v", err)
	}
	assertSanitizedFinding(t, report.Findings, "$.call_logs[0].ip_address_redacted", "raw_ip_redaction_not_proven")
}

func TestPreflightSanitizedSnapshotRejectsRedactionWithoutPresenceProof(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	snapshot.Safety.IPHandling = testStrictIPHandling
	snapshot.CallLogs[0].IPAddressWasPresent = false
	snapshot.CallLogs[0].IPAddressRedacted = true
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)

	report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot: %v", err)
	}
	assertSanitizedFinding(t, report.Findings, "$.call_logs[0].ip_address_was_present", "raw_ip_presence_not_proven")
}

func TestPreflightSanitizedSnapshotRequiresExactDualBooleanIPHandlingDeclaration(t *testing.T) {
	t.Run("positive proof requires dual declaration", func(t *testing.T) {
		snapshot := approvedSanitizedTestSnapshot(t)
		snapshot.Safety.IPHandling = testHistoricalIPHandling
		snapshot.CallLogs[0].IPAddressWasPresent = true
		snapshot.CallLogs[0].IPAddressRedacted = true
		path, manifestPath := writeSanitizedTestInputs(t, snapshot)

		report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
		if err != nil {
			t.Fatalf("preflight sanitized snapshot: %v", err)
		}
		assertSanitizedFinding(t, report.Findings, "$.safety.ip_handling", "dual_boolean_ip_handling_required")
	})

	t.Run("mutated declaration is rejected", func(t *testing.T) {
		snapshot := approvedSanitizedTestSnapshot(t)
		snapshot.Safety.IPHandling = testStrictIPHandling + "."
		path, manifestPath := writeSanitizedTestInputs(t, snapshot)

		report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
		if err != nil {
			t.Fatalf("preflight sanitized snapshot: %v", err)
		}
		assertSanitizedFinding(t, report.Findings, "$.safety.ip_handling", "unapproved_ip_handling")
	})
}

func TestPreflightSanitizedSnapshotAcceptsHistoricalIPDeclarationOnlyForFalseFalse(t *testing.T) {
	testCases := []struct {
		name     string
		present  bool
		redacted bool
		wantPass bool
		wantPath string
		wantCode string
	}{
		{name: "false false", wantPass: true},
		{name: "true true", present: true, redacted: true, wantPath: "$.safety.ip_handling", wantCode: "dual_boolean_ip_handling_required"},
		{name: "true false", present: true, wantPath: "$.safety.ip_handling", wantCode: "dual_boolean_ip_handling_required"},
		{name: "false true", redacted: true, wantPath: "$.safety.ip_handling", wantCode: "dual_boolean_ip_handling_required"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			snapshot := approvedSanitizedTestSnapshot(t)
			snapshot.Safety.IPHandling = testHistoricalIPHandling
			snapshot.CallLogs[0].IPAddressWasPresent = testCase.present
			snapshot.CallLogs[0].IPAddressRedacted = testCase.redacted
			path, manifestPath := writeSanitizedTestInputs(t, snapshot)

			report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
			if err != nil {
				t.Fatalf("preflight sanitized snapshot: %v", err)
			}
			if testCase.wantPass {
				if report.Status != "pass" || len(report.Findings) != 0 {
					t.Fatalf("historical false/false report=%+v", report)
				}
				return
			}
			assertSanitizedFinding(t, report.Findings, testCase.wantPath, testCase.wantCode)
		})
	}
}

func TestPreflightSanitizedSnapshotDoesNotAllowRaisingFrozenCaps(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)

	if _, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{MaxRequestIDs: 21}); err == nil {
		t.Fatal("request cap above 20 unexpectedly accepted")
	}
	if _, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{MaxEvents: 201}); err == nil {
		t.Fatal("event cap above 200 unexpectedly accepted")
	}
}

func TestPreflightSanitizedSnapshotRequiresExplicitSafetyFlags(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode snapshot map: %v", err)
	}
	safety := raw["safety"].(map[string]any)
	delete(safety, "raw_prompts_included")
	data, err = json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatalf("marshal snapshot map: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("rewrite snapshot: %v", err)
	}

	report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot: %v", err)
	}
	assertSanitizedFinding(t, report.Findings, "$.safety", "required_safety_field_missing")
}

func TestPreflightSanitizedSnapshotBlocksSensitiveValuesAndBounds(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	actor := "user:owner@example.com"
	snapshot.CallLogs[0].ActorRef = &actor
	snapshot.RequestIDs = append(snapshot.RequestIDs, "req-second")
	snapshot.Scenarios = append(snapshot.Scenarios, SnapshotScenario{Label: "second", RequestID: "req-second"})
	snapshot.Counts.RequestedRequestIDs = 2
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)

	report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{MaxRequestIDs: 1})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot: %v", err)
	}
	if report.Status != "blocked" {
		t.Fatalf("status=%s", report.Status)
	}
	assertSanitizedFinding(t, report.Findings, "$.request_ids", "request_id_limit_exceeded")
	assertSanitizedFinding(t, report.Findings, "$.call_logs[0].actor_ref", "email_like_value")
	for _, finding := range report.Findings {
		if strings.Contains(finding.Path, "owner@example.com") || strings.Contains(finding.Code, "owner@example.com") {
			t.Fatalf("finding leaked sensitive value: %+v", finding)
		}
	}
}

func TestPreflightSanitizedSnapshotBlocksFreeTextMetadataAndCrossOrgRows(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	status := int64(200)
	snapshot.TraceEvents = []SnapshotTraceEvent{{
		SourceRef:            "unirouter:internal-dev:request_trace_events:2",
		SourceEventHash:      strings.Repeat("c", 64),
		ID:                   2,
		RequestID:            snapshot.RequestIDs[0],
		OrgID:                2,
		EventType:            "provider.response",
		Stage:                "provider",
		Severity:             "info",
		StatusCode:           &status,
		Metadata:             map[string]any{"routing_selection_reason": "selected for Alice"},
		MetadataKeys:         []string{"routing_selection_reason"},
		CreatedAt:            "2026-07-19T06:00:01Z",
		PayloadCaptureMode:   "metadata_only",
		PayloadCaptureReason: approvedPayloadCaptureReason,
	}}
	snapshot.Counts.TraceEvents = 1
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)

	report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot: %v", err)
	}
	assertSanitizedFinding(t, report.Findings, "$", "single_org_source_rows_required")
	assertSanitizedFinding(t, report.Findings, "$.request_trace_events[0].metadata[0].value", "code_value_required")
}

func TestPreflightSanitizedSnapshotBlocksUnsafeMetadataKey(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	status := int64(200)
	snapshot.TraceEvents = []SnapshotTraceEvent{{
		SourceRef:            "unirouter:internal-dev:request_trace_events:2",
		SourceEventHash:      strings.Repeat("c", 64),
		ID:                   2,
		RequestID:            snapshot.RequestIDs[0],
		OrgID:                1,
		EventType:            "provider.response",
		Stage:                "provider",
		Severity:             "info",
		StatusCode:           &status,
		Metadata:             map[string]any{"prompt": "hidden"},
		MetadataKeys:         []string{"prompt"},
		CreatedAt:            "2026-07-19T06:00:01Z",
		PayloadCaptureMode:   "metadata_only",
		PayloadCaptureReason: approvedPayloadCaptureReason,
	}}
	snapshot.Counts.TraceEvents = 1
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)

	report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot: %v", err)
	}
	assertSanitizedFinding(t, report.Findings, "$", "unsafe_snapshot_content")
}

func TestPreflightSanitizedSnapshotRequiresPrivateRegularFile(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod snapshot: %v", err)
	}
	report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot: %v", err)
	}
	assertSanitizedFinding(t, report.Findings, "$", "artifact_permissions_not_private")
	if err := os.Chmod(manifestPath, 0o644); err != nil {
		t.Fatalf("chmod approval manifest: %v", err)
	}
	report, err = PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot with public manifest: %v", err)
	}
	assertSanitizedFinding(t, report.Findings, "$manifest", "manifest_permissions_not_private")

	symlinkPath := filepath.Join(t.TempDir(), "snapshot-link.json")
	if err := os.Symlink(path, symlinkPath); err != nil {
		t.Fatalf("symlink snapshot: %v", err)
	}
	if _, err := PreflightSanitizedSnapshot(symlinkPath, manifestPath, SanitizedPreflightOptions{}); err == nil {
		t.Fatal("symlink snapshot unexpectedly accepted")
	}
	if _, err := PreflightSanitizedSnapshot("relative-snapshot.json", manifestPath, SanitizedPreflightOptions{}); err == nil {
		t.Fatal("relative snapshot path unexpectedly accepted")
	}
}

func TestReadSanitizedSnapshotBytesDetectsInPlaceModification(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.json")
	if err := os.WriteFile(path, []byte("original\n"), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	_, _, err := readSanitizedSnapshotBytesWithHook(path, func() error {
		return os.WriteFile(path, []byte("modified\n"), 0o600)
	})
	if err == nil {
		t.Fatal("in-place modification during read unexpectedly accepted")
	}
}

func TestReadSanitizedSnapshotBytesDetectsPathSwap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input.json")
	replacement := filepath.Join(dir, "replacement.json")
	if err := os.WriteFile(path, []byte("original\n"), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	if err := os.WriteFile(replacement, []byte("replaced\n"), 0o600); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	_, _, err := readSanitizedSnapshotBytesWithHook(path, func() error {
		return os.Rename(replacement, path)
	})
	if err == nil {
		t.Fatal("path swap during read unexpectedly accepted")
	}
}

func TestPreflightSanitizedSnapshotDoesNotEchoSensitiveFindingValues(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	actor := "user:owner@example.com"
	snapshot.CallLogs[0].ActorRef = &actor
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)

	report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot: %v", err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if strings.Contains(string(encoded), "owner@example.com") {
		t.Fatalf("report echoed sensitive value: %s", encoded)
	}
}

func TestPreflightSanitizedSnapshotDoesNotEchoUntrustedTopLevelValues(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	snapshot.SourceInstance = "owner@example.com"
	snapshot.Safety.SelectionMode = "Bearer secret-token-value"
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)

	report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot: %v", err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	for _, forbidden := range []string{"owner@example.com", "Bearer secret-token-value"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("report echoed untrusted top-level value %q: %s", forbidden, encoded)
		}
	}

	loaded, err := LoadSnapshot(path)
	if err != nil {
		t.Fatalf("reload snapshot: %v", err)
	}
	loaded.SchemaVersion = "sk-abcdefghijklmnop"
	writeSanitizedSnapshotFile(t, path, loaded)
	_, err = PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err == nil || strings.Contains(err.Error(), loaded.SchemaVersion) {
		t.Fatalf("unsafe schema error=%v", err)
	}
}

func TestPreflightSanitizedSnapshotChecksAllPersistedStringClasses(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	snapshot.CallLogs[0].DerivedSourceComponent = "selected for Alice"
	actorType := "alice@example.com"
	snapshot.CallLogs[0].ActorType = &actorType
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)

	report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot: %v", err)
	}
	assertSanitizedFinding(t, report.Findings, "$.call_logs[0].source_component", "code_value_required")
	assertSanitizedFinding(t, report.Findings, "$.call_logs[0].actor_type", "email_like_value")
}

func TestPreflightSanitizedSnapshotBlocksWhitespaceOnlyOptionalValues(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	snapshot.CallLogs[0].Model = " "
	snapshot.CallLogs[0].ActorRef = stringPointer("\t")
	snapshot.CallLogs[0].PayloadRef = stringPointer(" ")
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)

	report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot: %v", err)
	}
	assertSanitizedFinding(t, report.Findings, "$.call_logs[0].model", "identifier_value_required")
	assertSanitizedFinding(t, report.Findings, "$.call_logs[0].actor_ref", "code_value_required")
	assertSanitizedFinding(t, report.Findings, "$.call_logs[0].payload_ref", "payload_reference_not_approved")
}

func TestPreflightSanitizedSnapshotBlocksPhoneCredentialAndIPLikeValues(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	snapshot.CallLogs[0].SessionID = stringPointer("+1 (415) 555-1212")
	snapshot.CallLogs[0].UpstreamRequestID = stringPointer("Bearer secret-token-value")
	snapshot.CallLogs[0].EvidenceRef = stringPointer("10.0.0.8")
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)

	report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot: %v", err)
	}
	assertSanitizedFinding(t, report.Findings, "$.call_logs[0].session_id", "phone_like_value")
	assertSanitizedFinding(t, report.Findings, "$.call_logs[0].upstream_request_id", "credential_like_value")
	assertSanitizedFinding(t, report.Findings, "$.call_logs[0].evidence_ref", "ip_like_value")
}

func TestPreflightSanitizedSnapshotBindsSourceRefToTableAndRowID(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	snapshot.CallLogs[0].SourceRef = "unirouter:internal-dev:request_trace_events:1"
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)

	report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot: %v", err)
	}
	assertSanitizedFinding(t, report.Findings, "$.call_logs[0].source_ref", "invalid_source_ref")
}

func TestPreflightSanitizedSnapshotRequiresOrgBoundSourceRowPerRequest(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	snapshot.RequestIDs = append(snapshot.RequestIDs, "req-second")
	snapshot.Scenarios = append(snapshot.Scenarios, SnapshotScenario{Label: "second", RequestID: "req-second"})
	snapshot.Counts.RequestedRequestIDs = 2
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)

	report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot: %v", err)
	}
	assertSanitizedFinding(t, report.Findings, "$.request_ids[1]", "source_row_required_for_org_binding")
}

func TestPreflightSanitizedSnapshotRequiresExactManifestBinding(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)

	loaded, err := LoadSnapshot(path)
	if err != nil {
		t.Fatalf("reload snapshot: %v", err)
	}
	loaded.ApprovalManifestSHA256 = "sha256:" + strings.Repeat("d", 64)
	writeSanitizedSnapshotFile(t, path, loaded)

	report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot: %v", err)
	}
	assertSanitizedFinding(t, report.Findings, "$.approval_manifest_sha256", "approval_manifest_sha256_mismatch")
}

func TestPreflightSanitizedSnapshotRequiresApprovedRequiredManifestRows(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)
	manifest := sanitizedApprovalManifest{
		SchemaVersion:  sanitizedApprovalManifestSchema,
		SourceInstance: snapshot.SourceInstance,
		Requests: []sanitizedApprovalManifestRequest{{
			ScenarioLabel:  snapshot.Scenarios[0].Label,
			RequestID:      snapshot.Scenarios[0].RequestID,
			ApprovalStatus: "pending",
			Required:       false,
		}},
	}
	writeSanitizedManifestFile(t, manifestPath, manifest)

	report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot: %v", err)
	}
	assertSanitizedFinding(t, report.Findings, "$manifest.requests[0].approval_status", "approved_status_required")
	assertSanitizedFinding(t, report.Findings, "$manifest.requests[0].required", "required_true_required")
	assertSanitizedFinding(t, report.Findings, "$.request_ids[0]", "request_not_manifest_approved")
}

func TestPreflightSanitizedSnapshotDoesNotEchoSensitiveMetadataKeys(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	status := int64(200)
	sensitiveKey := "sk-abcdefghijklmnop"
	snapshot.TraceEvents = []SnapshotTraceEvent{{
		SourceRef:              "unirouter:internal-dev:request_trace_events:2",
		SourceEventHash:        strings.Repeat("c", 64),
		ID:                     2,
		RequestID:              snapshot.RequestIDs[0],
		OrgID:                  1,
		EventType:              "provider.response",
		Stage:                  "provider",
		Severity:               "info",
		StatusCode:             &status,
		Metadata:               map[string]any{sensitiveKey: true},
		MetadataKeys:           []string{sensitiveKey},
		CreatedAt:              "2026-07-19T06:00:01Z",
		PayloadCaptureMode:     "metadata_only",
		PayloadCaptureReason:   approvedPayloadCaptureReason,
		DerivedCaptureMode:     "db_import",
		DerivedSchemaVersion:   "unirouter.evidence.v1",
		DerivedSourceComponent: "trace",
	}}
	snapshot.Counts.TraceEvents = 1
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)

	report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot: %v", err)
	}
	assertSanitizedFinding(t, report.Findings, "$.request_trace_events[0].metadata[0].key", "credential_like_value")
	assertSanitizedFinding(t, report.Findings, "$.request_trace_events[0].metadata[0].key", "metadata_key_not_approved")
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if strings.Contains(string(encoded), sensitiveKey) {
		t.Fatalf("report echoed sensitive metadata key: %s", encoded)
	}
}

func TestPreflightSanitizedSnapshotBlocksNumericIdentifierLikeMetadata(t *testing.T) {
	snapshot := approvedSanitizedTestSnapshot(t)
	status := int64(200)
	snapshot.TraceEvents = []SnapshotTraceEvent{{
		SourceRef:              "unirouter:internal-dev:request_trace_events:2",
		SourceEventHash:        strings.Repeat("c", 64),
		ID:                     2,
		RequestID:              snapshot.RequestIDs[0],
		OrgID:                  1,
		EventType:              "provider.response",
		Stage:                  "provider",
		Severity:               "info",
		StatusCode:             &status,
		Metadata:               map[string]any{"route_count": float64(13800138000)},
		MetadataKeys:           []string{"route_count"},
		CreatedAt:              "2026-07-19T06:00:01Z",
		PayloadCaptureMode:     "metadata_only",
		PayloadCaptureReason:   approvedPayloadCaptureReason,
		DerivedCaptureMode:     "db_import",
		DerivedSchemaVersion:   "unirouter.evidence.v1",
		DerivedSourceComponent: "trace",
	}}
	snapshot.Counts.TraceEvents = 1
	path, manifestPath := writeSanitizedTestInputs(t, snapshot)

	report, err := PreflightSanitizedSnapshot(path, manifestPath, SanitizedPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight sanitized snapshot: %v", err)
	}
	assertSanitizedFinding(t, report.Findings, "$.request_trace_events[0].metadata[0].value", "numeric_identifier_like_value")
}

func approvedSanitizedTestSnapshot(t *testing.T) SnapshotFile {
	t.Helper()
	snapshot, err := LoadSnapshot(filepath.Join("..", "..", "..", "fixtures", "unirouter_snapshot", "micro_money_v2_generated.json"))
	if err != nil {
		t.Fatalf("load v2 fixture: %v", err)
	}
	snapshot.SourceInstance = "internal-dev"
	snapshot.ExchangeID = "uxchg_internal_20260719_001"
	snapshot.ApprovalManifestSHA256 = ""
	snapshot.Safety.SelectionMode = "approved_request_id_manifest"
	snapshot.Scenarios = []SnapshotScenario{{Label: "usage_success", RequestID: snapshot.RequestIDs[0]}}
	snapshot.Notes = map[string]any{}
	for key, value := range approvedSnapshotNotes {
		snapshot.Notes[key] = value
	}
	for index := range snapshot.CallLogs {
		snapshot.CallLogs[index].SourceRef = "unirouter:internal-dev:call_logs:1"
		snapshot.CallLogs[index].SourceEventHash = strings.Repeat("b", 64)
	}
	return snapshot
}

func writeSanitizedTestInputs(t *testing.T, snapshot SnapshotFile) (string, string) {
	t.Helper()
	dir := t.TempDir()
	manifest := sanitizedApprovalManifest{
		SchemaVersion:  sanitizedApprovalManifestSchema,
		SourceInstance: snapshot.SourceInstance,
		Requests:       make([]sanitizedApprovalManifestRequest, 0, len(snapshot.Scenarios)),
	}
	for _, scenario := range snapshot.Scenarios {
		manifest.Requests = append(manifest.Requests, sanitizedApprovalManifestRequest{
			ScenarioLabel:  scenario.Label,
			RequestID:      scenario.RequestID,
			ApprovalStatus: "approved",
			Required:       true,
		})
	}
	manifestPath := filepath.Join(dir, "approval-manifest.json")
	manifestData := writeSanitizedManifestFile(t, manifestPath, manifest)
	_, manifestSHA256, err := loadSanitizedApprovalManifest(manifestData)
	if err != nil {
		t.Fatalf("load test approval manifest: %v", err)
	}
	snapshot.ApprovalManifestSHA256 = manifestSHA256
	snapshotPath := filepath.Join(dir, "snapshot.json")
	writeSanitizedSnapshotFile(t, snapshotPath, snapshot)
	return snapshotPath, manifestPath
}

func writeSanitizedManifestFile(t *testing.T, path string, manifest sanitizedApprovalManifest) []byte {
	t.Helper()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal approval manifest: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("write approval manifest: %v", err)
	}
	return append(data, '\n')
}

func writeSanitizedSnapshotFile(t *testing.T, path string, snapshot SnapshotFile) {
	t.Helper()
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
}

func assertSanitizedFinding(t *testing.T, findings []SanitizedPreflightFinding, path string, code string) {
	t.Helper()
	for _, finding := range findings {
		if finding.Path == path && finding.Code == code {
			return
		}
	}
	t.Fatalf("missing finding path=%s code=%s in %+v", path, code, findings)
}

func stringPointer(value string) *string {
	return &value
}
