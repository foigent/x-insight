package openinferenceoffline

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/openinference"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/evidence"
)

const (
	testMachineDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testBundleDigest  = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testProductCommit = "cccccccccccccccccccccccccccccccccccccccc"
)

func TestBoundedOfflineAuthorizationBindsHostBuildWindowAndExactImport(t *testing.T) {
	now := time.Date(2026, 8, 2, 3, 0, 0, 0, time.UTC)
	path := writeTestAuthorization(t, validTestAuthorization(now))
	authorization, digest, err := loadBoundedOfflineAuthorization(
		path,
		uint32(os.Geteuid()),
		"aliyun-ecs:i-study",
		testMachineDigest,
		"0.3.0-rc.7",
		testProductCommit,
		now,
	)
	if err != nil {
		t.Fatalf("load authorization: %v", err)
	}
	if !sha256Pattern.MatchString(digest) {
		t.Fatalf("authorization digest=%q", digest)
	}
	request := validAuthorizedImportRequest()
	if err := authorization.ValidateImportBinding(request); err != nil {
		t.Fatalf("validate request binding: %v", err)
	}
	if err := authorization.ValidateImport(request); err != nil {
		t.Fatalf("validate exact import: %v", err)
	}

	mutations := []func(*AuthorizedImportRequest){
		func(value *AuthorizedImportRequest) { value.TargetRef = "aliyun-ecs:other" },
		func(value *AuthorizedImportRequest) { value.BundlePath += ".other" },
		func(value *AuthorizedImportRequest) { value.BundleSHA256 = "sha256:" + strings.Repeat("d", 64) },
		func(value *AuthorizedImportRequest) { value.BundleID += "-other" },
		func(value *AuthorizedImportRequest) { value.OperationID += "-other" },
		func(value *AuthorizedImportRequest) { value.ProducerInstance += "-other" },
		func(value *AuthorizedImportRequest) { tenant := "other"; value.TenantRef = &tenant },
		func(value *AuthorizedImportRequest) { value.OrgRef = "2" },
		func(value *AuthorizedImportRequest) { value.ProjectRef = "8" },
		func(value *AuthorizedImportRequest) { value.RecordCount = 4 },
	}
	for index, mutate := range mutations {
		candidate := validAuthorizedImportRequest()
		mutate(&candidate)
		if err := authorization.ValidateImport(candidate); err == nil {
			t.Fatalf("mismatch %d unexpectedly authorized: %+v", index, candidate)
		}
	}
}

func TestBoundedOfflineAuthorizationAcceptsSchemaBoundedLongAbsoluteBundlePath(t *testing.T) {
	now := time.Date(2026, 8, 2, 3, 0, 0, 0, time.UTC)
	authorization := validTestAuthorization(now)
	authorization.Import.BundlePath = "/" + strings.Repeat("segment/", 40) + "bundle.json"
	path := writeTestAuthorization(t, authorization)
	if _, _, err := loadBoundedOfflineAuthorization(
		path,
		uint32(os.Geteuid()),
		"aliyun-ecs:i-study",
		testMachineDigest,
		"0.3.0-rc.7",
		testProductCommit,
		now,
	); err != nil {
		t.Fatalf("schema-valid long absolute bundle path rejected: %v", err)
	}
}

func TestBoundedOfflineAuthorizationMatchesProductLocalSchema(t *testing.T) {
	authorization := validTestAuthorization(time.Date(2026, 8, 2, 3, 0, 0, 0, time.UTC))
	payload, err := json.Marshal(authorization)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatal(err)
	}
	validator, err := evidence.NewSchemaValidator(filepath.Join(
		"..", "..", "..", "schemas", "integration", "openinference-bounded-offline-authorization-v1.schema.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := validator.Validate(value); err != nil {
		t.Fatalf("authorization schema mismatch: %v", err)
	}
}

func TestBoundedOfflineAuthorizationRejectsSemanticAndJSONDrift(t *testing.T) {
	now := time.Date(2026, 8, 2, 3, 0, 0, 0, time.UTC)
	base := validTestAuthorization(now)
	tests := []struct {
		name   string
		mutate func(*BoundedOfflineAuthorization)
	}{
		{"production", func(value *BoundedOfflineAuthorization) { value.Environment = "production" }},
		{"wrong purpose", func(value *BoundedOfflineAuthorization) { value.Purpose = "general_activation" }},
		{"expired", func(value *BoundedOfflineAuthorization) { value.ExpiresAt = now.Format(time.RFC3339Nano) }},
		{"long window", func(value *BoundedOfflineAuthorization) {
			value.ExpiresAt = now.Add(3 * time.Hour).Format(time.RFC3339Nano)
		}},
		{"wrong machine", func(value *BoundedOfflineAuthorization) {
			value.Target.MachineIDSHA256 = "sha256:" + strings.Repeat("d", 64)
		}},
		{"wrong build", func(value *BoundedOfflineAuthorization) { value.Product.Version = "0.3.0-rc.5" }},
		{"too many records", func(value *BoundedOfflineAuthorization) { value.Import.MaxRecords = 11 }},
		{"raw payload", func(value *BoundedOfflineAuthorization) { value.DataPolicy.RawPayloadPresent = true }},
		{"listener", func(value *BoundedOfflineAuthorization) { value.RuntimeBoundary.NetworkListener = true }},
		{"feed", func(value *BoundedOfflineAuthorization) { value.RuntimeBoundary.FeedActivation = true }},
		{"checkpoint", func(value *BoundedOfflineAuthorization) { value.RuntimeBoundary.CursorCheckpoint = true }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := base
			test.mutate(&candidate)
			path := writeTestAuthorization(t, candidate)
			if _, _, err := loadBoundedOfflineAuthorization(
				path,
				uint32(os.Geteuid()),
				"aliyun-ecs:i-study",
				testMachineDigest,
				"0.3.0-rc.7",
				testProductCommit,
				now,
			); err == nil {
				t.Fatal("semantic drift unexpectedly authorized")
			}
		})
	}

	canonical, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	invalidJSON := [][]byte{
		[]byte(strings.Replace(string(canonical), `"environment":"nonproduction"`, `"environment":"nonproduction","environment":"nonproduction"`, 1)),
		[]byte(strings.Replace(string(canonical), `"purpose":"openinference_trace_study"`, `"purpose":"openinference_trace_study","unknown":true`, 1)),
		append(append([]byte(nil), canonical...), []byte(` {}`)...),
	}
	for index, payload := range invalidJSON {
		if _, err := parseBoundedOfflineAuthorization(payload); err == nil {
			t.Fatalf("invalid JSON %d unexpectedly parsed", index)
		}
	}

	for objectName, fields := range map[string][]string{
		"data_policy":      {"raw_payload_present"},
		"runtime_boundary": {"network_listener", "feed_activation", "cursor_checkpoint"},
	} {
		for _, field := range fields {
			var candidate map[string]any
			if err := json.Unmarshal(canonical, &candidate); err != nil {
				t.Fatal(err)
			}
			delete(candidate[objectName].(map[string]any), field)
			payload, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parseBoundedOfflineAuthorization(payload); err == nil {
				t.Fatalf("missing %s.%s unexpectedly parsed", objectName, field)
			}
		}
	}
}

func TestBoundedOfflineAuthorizationFileIdentityFailsClosed(t *testing.T) {
	now := time.Date(2026, 8, 2, 3, 0, 0, 0, time.UTC)
	path := writeTestAuthorization(t, validTestAuthorization(now))
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadBoundedOfflineAuthorization(
		path,
		uint32(os.Geteuid()),
		"aliyun-ecs:i-study",
		testMachineDigest,
		"0.3.0-rc.7",
		testProductCommit,
		now,
	); err == nil {
		t.Fatal("world-readable authorization unexpectedly accepted")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(t.TempDir(), "authorization-link.json")
	if err := os.Symlink(path, symlink); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadBoundedOfflineAuthorization(
		symlink,
		uint32(os.Geteuid()),
		"aliyun-ecs:i-study",
		testMachineDigest,
		"0.3.0-rc.7",
		testProductCommit,
		now,
	); err == nil {
		t.Fatal("authorization symlink unexpectedly accepted")
	}
	wrongUID := uint32(os.Geteuid() + 1)
	if _, _, err := loadBoundedOfflineAuthorization(
		path,
		wrongUID,
		"aliyun-ecs:i-study",
		testMachineDigest,
		"0.3.0-rc.7",
		testProductCommit,
		now,
	); err == nil {
		t.Fatal("wrong authorization owner unexpectedly accepted")
	}
}

func TestMetadataOnlyOTLPRejectsPromptAndResponseAttributes(t *testing.T) {
	metadataOnly := []byte(`{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"agent"}}]},"scopeSpans":[]}]}`)
	if err := ValidateMetadataOnlyOTLP(metadataOnly); err != nil {
		t.Fatalf("metadata-only payload rejected: %v", err)
	}
	for _, key := range []string{
		"input.value",
		"output.value",
		"llm.input_messages.0.message.content",
		"llm.output_messages.0.message.content",
		"llm.prompt_template.variables",
	} {
		payload := []byte(`{"resourceSpans":[{"resource":{"attributes":[]},"scopeSpans":[{"spans":[{"attributes":[{"key":"` + key + `","value":{"stringValue":"secret"}}]}]}]}]}`)
		if err := ValidateMetadataOnlyOTLP(payload); err == nil {
			t.Fatalf("raw attribute %q unexpectedly accepted", key)
		}
	}
	duplicateKey := []byte(`{"resourceSpans":[{"resource":{"attributes":[]},"scopeSpans":[{"spans":[{"attributes":[{"key":"input.value","key":"service.name","value":{"stringValue":"secret"}}]}]}]}]}`)
	if err := ValidateMetadataOnlyOTLP(duplicateKey); err == nil {
		t.Fatal("duplicate OTLP attribute key unexpectedly accepted")
	}
}

func TestTraceJourneyStudyBundleIsThreeMetadataOnlySourceDeclaredSpans(t *testing.T) {
	payload, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "contracts", "openinference", "trace-journey-study.v1.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateMetadataOnlyOTLP(payload); err != nil {
		t.Fatalf("study bundle crossed raw-payload boundary: %v", err)
	}
	batch, err := openinference.SanitizeOTLPExport(payload)
	if err != nil {
		t.Fatalf("sanitize study bundle: %v", err)
	}
	if len(batch.Records) != 3 || batch.SkippedNonLLM != 0 {
		t.Fatalf("batch=%+v", batch)
	}
	spanIDs := map[string]bool{}
	projector, err := openinference.NewProjector()
	if err != nil {
		t.Fatalf("initialize projector: %v", err)
	}
	projectionContext := connector.ProjectionContext{
		ObservedAt: time.Date(2026, 8, 2, 4, 0, 0, 0, time.UTC),
		IngestedAt: time.Date(2026, 8, 2, 4, 0, 0, 0, time.UTC),
	}
	mappingContext := openinference.MappingContext{
		ProducerInstance: "xinsight-openinference-trace-study",
		OrgRef:           "1",
		ProjectRef:       "7",
	}
	totalGapEvents := 0
	firstProjectionIdentity := map[string]string{}
	for _, record := range batch.Records {
		if record.Record.TraceID != "a1a2a3a4a5a6a7a8b1b2b3b4b5b6b7b8" || record.Record.MissingParent {
			t.Fatalf("record=%+v", record.Record)
		}
		if bytes.Contains(record.CanonicalBytes, []byte("input.value")) ||
			bytes.Contains(record.CanonicalBytes, []byte("output.value")) {
			t.Fatalf("raw attribute survived in record=%+v", record.Record)
		}
		spanIDs[record.Record.SpanID] = true
		item, connectorErr := projector.Project(
			context.Background(),
			record,
			mappingContext,
			projectionContext,
		)
		if connectorErr != nil {
			t.Fatalf("project study span %s: %v", record.Record.SpanID, connectorErr)
		}
		if len(item.Events) != 5 {
			t.Fatalf("span %s events=%d, want one M0 event plus four Gap events", record.Record.SpanID, len(item.Events))
		}
		for _, event := range item.Events {
			id, _ := event["id"].(string)
			hash, _ := event["event_hash"].(string)
			if id == "" || hash == "" {
				t.Fatalf("span %s projected event lacks identity: %+v", record.Record.SpanID, event)
			}
			firstProjectionIdentity[id] = hash
			if event["event_type"] == "gap.capture.detected" {
				totalGapEvents++
			}
		}
	}
	for _, expected := range []string{"1111111111111111", "2222222222222222", "3333333333333333"} {
		if !spanIDs[expected] {
			t.Fatalf("study span %s missing: %+v", expected, spanIDs)
		}
	}
	if totalGapEvents != 12 || len(firstProjectionIdentity) != 15 {
		t.Fatalf("study projection identities=%d gaps=%d", len(firstProjectionIdentity), totalGapEvents)
	}
	for _, record := range batch.Records {
		item, connectorErr := projector.Project(
			context.Background(),
			record,
			mappingContext,
			projectionContext,
		)
		if connectorErr != nil {
			t.Fatalf("reproject study span %s: %v", record.Record.SpanID, connectorErr)
		}
		for _, event := range item.Events {
			id, _ := event["id"].(string)
			hash, _ := event["event_hash"].(string)
			if firstProjectionIdentity[id] != hash {
				t.Fatalf("study projection identity drifted for %s: %s", id, hash)
			}
		}
	}
}

func validTestAuthorization(now time.Time) BoundedOfflineAuthorization {
	return BoundedOfflineAuthorization{
		SchemaVersion:   AuthorizationSchemaVersion,
		AuthorizationID: "openinference-study-20260802",
		Environment:     "nonproduction",
		Purpose:         "openinference_trace_study",
		NotBefore:       now.Add(-time.Minute).Format(time.RFC3339Nano),
		ExpiresAt:       now.Add(time.Hour).Format(time.RFC3339Nano),
		Target: AuthorizationTarget{
			TargetRef:       "aliyun-ecs:i-study",
			MachineIDSHA256: testMachineDigest,
		},
		Product: AuthorizationProduct{Version: "0.3.0-rc.7", Commit: testProductCommit},
		Import: AuthorizationImport{
			BundlePath:       "/var/lib/x-insight/openinference-study.json",
			BundleSHA256:     testBundleDigest,
			BundleID:         "openinference-study-bundle-1",
			OperationID:      "openinference-study-operation-1",
			ProducerInstance: "otel-study-collector",
			OrgRef:           "1",
			ProjectRef:       "7",
			MaxRecords:       3,
		},
		DataPolicy: AuthorizationDataPolicy{
			PayloadCaptureMode: "metadata_only",
			RawPayloadPresent:  false,
		},
		RuntimeBoundary: AuthorizationRuntimeBoundary{
			DeliveryMode:     "bounded_offline",
			NetworkListener:  false,
			FeedActivation:   false,
			CursorCheckpoint: false,
		},
	}
}

func validAuthorizedImportRequest() AuthorizedImportRequest {
	return AuthorizedImportRequest{
		TargetRef:        "aliyun-ecs:i-study",
		BundlePath:       "/var/lib/x-insight/openinference-study.json",
		BundleSHA256:     testBundleDigest,
		BundleID:         "openinference-study-bundle-1",
		OperationID:      "openinference-study-operation-1",
		ProducerInstance: "otel-study-collector",
		OrgRef:           "1",
		ProjectRef:       "7",
		RecordCount:      3,
	}
}

func writeTestAuthorization(t *testing.T, authorization BoundedOfflineAuthorization) string {
	t.Helper()
	payload, err := json.Marshal(authorization)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "authorization.json")
	if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
