package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/exchange"
	"github.com/foigent/x-insight/internal/investigation"
)

func TestOpenInferenceImportBundleFailsClosedBeforeBundleCredentialsOrDatabase(t *testing.T) {
	root := t.TempDir()
	err := run([]string{
		"openinference", "import-bundle",
		"--authorization-file", filepath.Join(root, "missing-authorization.json"),
		"--target-ref", "aliyun-ecs:i-study",
		"--bundle", filepath.Join(root, "missing-bundle.json"),
		"--operation-id", "operation-1",
		"--bundle-id", "bundle-1",
		"--producer-instance", "collector-1",
		"--org-ref", "1",
		"--project-ref", "1",
	})
	if err == nil || !strings.Contains(err.Error(), "authorization file") || strings.Contains(err.Error(), "bundle") {
		t.Fatalf("error=%v", err)
	}

	err = run([]string{
		"openinference", "import-bundle",
		"--target-ref", "aliyun-ecs:i-study",
		"--bundle", filepath.Join(root, "missing-bundle.json"),
		"--operation-id", "operation-1",
		"--bundle-id", "bundle-1",
		"--producer-instance", "collector-1",
		"--org-ref", "1",
		"--project-ref", "1",
	})
	if err == nil || !strings.Contains(err.Error(), "--authorization-file is required") {
		t.Fatalf("missing authorization error=%v", err)
	}
}

func TestPullFeedRouteRejectsArgumentsBeforeRuntimeAccess(t *testing.T) {
	err := run([]string{"unirouter", "pull-feed", "--unexpected"})
	if err == nil || !strings.Contains(err.Error(), "does not accept") {
		t.Fatalf("error=%v", err)
	}
}

func TestSourceFactsRouteRequiresExplicitInputs(t *testing.T) {
	for _, args := range [][]string{
		{"unirouter", "pull-source-facts"},
		{"unirouter", "pull-source-facts", "--unexpected"},
		{"unirouter", "pull-source-facts", "--endpoint", "https://source.invalid/api/v1/integrations/xinsight/events", "--stream-id", "synthetic", "--key-file", "/missing-synthetic-file"},
		{"unirouter", "pull-source-facts", "extra"},
	} {
		if err := run(args); err == nil {
			t.Fatal("missing source-specific inputs accepted")
		}
	}
	t.Setenv("XINSIGHT_IMPORTER_POSTGRES_DSN", "")
	t.Setenv("XINSIGHT_IMPORTER_POSTGRES_DSN_FILE", "")
	t.Setenv("CREDENTIALS_DIRECTORY", "")
	t.Setenv("XINSIGHT_ALLOW_LEGACY_SINGLE_DATABASE_ROLE", "")
	if err := run([]string{"unirouter", "pull-source-facts", "--endpoint", "https://source.invalid/api/v1/integrations/xinsight/source-facts", "--stream-id", "synthetic", "--key-file", "/missing-synthetic-file"}); err == nil || !errors.Is(err, sourcefacts.ErrInvalid) {
		t.Fatalf("managed registration must be configured before credentials or network access: %v", err)
	}
}

func TestFeedWorkerJSONLoggerEmitsOnlySuppliedSafeFields(t *testing.T) {
	var output bytes.Buffer
	logger := newFeedWorkerJSONLogger(&output)
	logger.Log("poll_failed", map[string]any{
		"code":      "source_status_401",
		"retryable": false,
	})
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("decode log: %v", err)
	}
	if record["component"] != "unirouter_completed_execution_pull" ||
		record["event"] != "poll_failed" ||
		record["code"] != "source_status_401" ||
		record["retryable"] != false {
		t.Fatalf("record=%#v", record)
	}
	for _, forbidden := range []string{"endpoint", "cursor", "service_key", "request_id"} {
		if _, exists := record[forbidden]; exists {
			t.Fatalf("log contains forbidden field %q", forbidden)
		}
	}
}

func TestPreflightSanitizedRequiresApprovalManifestFlag(t *testing.T) {
	err := run([]string{"unirouter", "preflight-sanitized", "--snapshot", "/private/tmp/snapshot.json"})
	if err == nil || !strings.Contains(err.Error(), "--approval-manifest is required") {
		t.Fatalf("error=%v", err)
	}
}

func TestOperatorApprovedImportRequiresApprovalManifest(t *testing.T) {
	err := run([]string{
		"unirouter", "import-snapshot",
		"--snapshot", "/private/tmp/snapshot.json",
		"--proof-class", investigation.ProofOperatorApproved,
	})
	if err == nil || !strings.Contains(err.Error(), "--approval-manifest is required") {
		t.Fatalf("error=%v", err)
	}
}

func TestGateAProofClassRequiresSuccessfulPreflight(t *testing.T) {
	if got := gateAProofClass(""); got != investigation.ProofSyntheticFixture {
		t.Fatalf("without preflight proof_class=%s", got)
	}
	if got := gateAProofClass("sha256:" + strings.Repeat("a", 64)); got != investigation.ProofOperatorApproved {
		t.Fatalf("with preflight proof_class=%s", got)
	}
}

func TestBuildGateAImportReceiptPersisted(t *testing.T) {
	report := gateACheckReport{
		Source:         "unirouter",
		SourceInstance: "dev",
		Status:         "persisted_and_read_back",
		Import: unirouter.SnapshotImportReport{
			EventsTotal:        3,
			CaptureGapEvents:   1,
			Accepted:           3,
			RerunDuplicateNoop: 3,
		},
		Persistence: &db.PersistenceReport{
			EventsTotal:         3,
			Inserted:            2,
			DuplicateNoop:       1,
			IdempotencyConflict: 0,
		},
		Readback: []gateAReadback{
			{RequestID: "req-1", EventCount: 2, GapCount: 1},
			{RequestID: "req-2", EventCount: 1, GapCount: 0},
		},
	}
	snapshot := unirouter.SnapshotFile{
		SchemaVersion:          "unirouter.prd5.xinsight.snapshot.v1",
		ExchangeID:             "exchange-1",
		ApprovalManifestSHA256: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		GeneratedAt:            "2026-07-10T01:00:00Z",
		RequestIDs:             []string{"req-1", "req-2"},
	}

	receipt := buildGateAImportReceipt(
		report,
		snapshot,
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		time.Date(2026, 7, 10, 2, 0, 0, 0, time.UTC),
	)

	if receipt.Result.Status != "persisted" {
		t.Fatalf("status=%s", receipt.Result.Status)
	}
	if receipt.Result.PersistedInserted == nil || *receipt.Result.PersistedInserted != 2 {
		t.Fatalf("persisted_inserted=%v", receipt.Result.PersistedInserted)
	}
	if receipt.Result.ReadbackEvents == nil || *receipt.Result.ReadbackEvents != 3 {
		t.Fatalf("readback_events=%v", receipt.Result.ReadbackEvents)
	}
	if receipt.Result.ReadbackCaptureGaps == nil || *receipt.Result.ReadbackCaptureGaps != 1 {
		t.Fatalf("readback_capture_gaps=%v", receipt.Result.ReadbackCaptureGaps)
	}
	if !slices.Equal(receipt.WarningCodes, []string{"source_capture_gaps_present"}) {
		t.Fatalf("warning_codes=%v", receipt.WarningCodes)
	}

	assertReceiptSchema(t, receipt)
}

func TestBuildGateAImportReceiptReportsMissingSourceReconciliation(t *testing.T) {
	report := gateACheckReport{
		Source:         "unirouter",
		SourceInstance: "dev",
		Status:         "validated_in_memory",
		Import: unirouter.SnapshotImportReport{
			EventsTotal:        2,
			Accepted:           2,
			RerunDuplicateNoop: 2,
		},
	}
	snapshot := unirouter.SnapshotFile{
		SchemaVersion: "unirouter.prd5.xinsight.snapshot.v1",
		GeneratedAt:   "2026-07-10T01:00:00Z",
		RequestIDs:    []string{"req-1"},
	}

	receipt := buildGateAImportReceipt(
		report,
		snapshot,
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		time.Date(2026, 7, 10, 2, 0, 0, 0, time.UTC),
	)

	wantWarnings := []string{
		"approval_manifest_hash_missing",
		"persistence_not_requested",
		"source_exchange_id_missing",
	}
	if !slices.Equal(receipt.WarningCodes, wantWarnings) {
		t.Fatalf("warning_codes=%v want=%v", receipt.WarningCodes, wantWarnings)
	}
	if receipt.Result.PersistedInserted != nil || receipt.Result.ReadbackEvents != nil {
		t.Fatalf("persistence fields should be omitted: %+v", receipt.Result)
	}

	assertReceiptSchema(t, receipt)
}

func TestWriteJSONExclusiveRefusesOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receipt.json")
	value := map[string]string{"status": "validated"}
	if err := writeJSONExclusive(path, value); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := writeJSONExclusive(path, value); err == nil {
		t.Fatalf("expected overwrite refusal")
	}
}

func TestFileSHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	hash, err := fileSHA256(path)
	if err != nil {
		t.Fatalf("hash artifact: %v", err)
	}
	if hash != "sha256:ca3d163bab055381827226140568f3bef7eaac187cebd76878e0b63e9e442356" {
		t.Fatalf("hash=%s", hash)
	}
	oversized := filepath.Join(t.TempDir(), "oversized.json")
	file, err := os.OpenFile(oversized, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("create oversized artifact: %v", err)
	}
	if err := file.Truncate(unirouter.MaxSnapshotBytes + 1); err != nil {
		_ = file.Close()
		t.Fatalf("truncate oversized artifact: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close oversized artifact: %v", err)
	}
	if _, err := fileSHA256(oversized); err == nil {
		t.Fatal("oversized artifact hash unexpectedly succeeded")
	}
}

func TestBuildSnapshotImportProvenanceSeparatesSyntheticAndApprovedProof(t *testing.T) {
	artifactHash := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	manifestHash := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	snapshot := unirouter.SnapshotFile{
		SchemaVersion:          "unirouter.snapshot.v1",
		ExchangeID:             "exchange-1",
		ApprovalManifestSHA256: manifestHash,
		GeneratedAt:            "2026-07-13T12:00:00Z",
	}
	synthetic, err := buildSnapshotImportProvenance(snapshot, artifactHash, unirouter.DefaultSourceSystemID, unirouter.DefaultAdapterManifestID, investigation.ProofSyntheticFixture)
	if err != nil {
		t.Fatalf("synthetic provenance: %v", err)
	}
	if synthetic.IntegrationMode != investigation.IntegrationFixture || synthetic.AssuranceMode != investigation.AssuranceSyntheticFixture || synthetic.ExchangeID != "" || synthetic.ApprovalManifestSHA256 != "" {
		t.Fatalf("synthetic=%+v", synthetic)
	}
	if !synthetic.TimelineComplete {
		t.Fatalf("gap-free synthetic snapshot marked incomplete: %+v", synthetic)
	}
	approved, err := buildSnapshotImportProvenance(snapshot, artifactHash, unirouter.DefaultSourceSystemID, unirouter.DefaultAdapterManifestID, investigation.ProofOperatorApproved)
	if err != nil {
		t.Fatalf("approved provenance: %v", err)
	}
	if approved.IntegrationMode != investigation.IntegrationSidecar || approved.AssuranceMode != investigation.AssuranceOperatorAttested || approved.ExchangeID != snapshot.ExchangeID || approved.ApprovalManifestSHA256 != manifestHash {
		t.Fatalf("approved=%+v", approved)
	}
	snapshot.ExchangeID = ""
	if _, err := buildSnapshotImportProvenance(snapshot, artifactHash, unirouter.DefaultSourceSystemID, unirouter.DefaultAdapterManifestID, investigation.ProofOperatorApproved); err == nil {
		t.Fatal("operator-approved provenance without exchange ID was accepted")
	}
	snapshot.CaptureGaps = []unirouter.SnapshotCaptureGap{{Gap: "not_available_from_source"}}
	incomplete, err := buildSnapshotImportProvenance(snapshot, artifactHash, unirouter.DefaultSourceSystemID, unirouter.DefaultAdapterManifestID, investigation.ProofSyntheticFixture)
	if err != nil {
		t.Fatalf("incomplete synthetic provenance: %v", err)
	}
	if incomplete.TimelineComplete {
		t.Fatalf("snapshot with an explicit capture gap marked complete: %+v", incomplete)
	}
	snapshot.CaptureGaps = nil
	embeddedGap := "permission_denied"
	snapshot.TraceEvents = []unirouter.SnapshotTraceEvent{{CaptureGap: &embeddedGap}}
	incomplete, err = buildSnapshotImportProvenance(snapshot, artifactHash, unirouter.DefaultSourceSystemID, unirouter.DefaultAdapterManifestID, investigation.ProofSyntheticFixture)
	if err != nil {
		t.Fatalf("embedded-gap provenance: %v", err)
	}
	if incomplete.TimelineComplete {
		t.Fatalf("snapshot with an event capture gap marked complete: %+v", incomplete)
	}
}

func TestBuildExecutionEvidenceImportProvenanceUsesPinnedCompleteness(t *testing.T) {
	artifactHash := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	bundle := unirouter.ExecutionEvidenceBundle{
		ClosedAt: "2026-07-30T06:00:00Z",
		Completeness: unirouter.ExecutionEvidenceCompleteness{
			State:                 "complete",
			SourceProfileComplete: true,
		},
	}
	provenance, err := buildExecutionEvidenceImportProvenance(
		bundle,
		artifactHash,
		unirouter.DefaultSourceSystemID,
		unirouter.DefaultExecutionEvidenceAdapterID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !provenance.TimelineComplete ||
		provenance.ProofClass != investigation.ProofSyntheticFixture ||
		provenance.SourceRevision != unirouter.ExecutionEvidenceProfileID+"@"+unirouter.ExecutionEvidenceProfileManifestSHA256 {
		t.Fatalf("execution evidence provenance mismatch: %+v", provenance)
	}
	bundle.Completeness.SourceProfileComplete = false
	provenance, err = buildExecutionEvidenceImportProvenance(
		bundle,
		artifactHash,
		unirouter.DefaultSourceSystemID,
		unirouter.DefaultExecutionEvidenceAdapterID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if provenance.TimelineComplete {
		t.Fatal("partial source profile was marked timeline-complete")
	}
}

func assertReceiptSchema(t *testing.T, receipt exchange.ImportReceipt) {
	t.Helper()
	if err := validateImportReceipt(receipt, filepath.Join("..", "..", "schemas", "integration", "import-receipt.schema.json")); err != nil {
		t.Fatalf("validate receipt: %v", err)
	}
}
