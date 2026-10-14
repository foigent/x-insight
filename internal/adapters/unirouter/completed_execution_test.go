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
	"time"

	"github.com/foigent/x-insight/internal/evidence"
)

func TestCompletedExecutionAcceptedContractIdentities(t *testing.T) {
	expected := map[string]struct {
		sha256 string
		bytes  int
	}{
		"unirouter.completed-execution.v1.schema.json": {
			sha256: AcceptedCompletedExecutionSchemaSHA256,
			bytes:  4089,
		},
		filepath.Join("fixtures", "valid-settled.json"): {
			sha256: "5e9eda0a2630d52c5589d38b8ec480604c47c2cc77391b75a57b2a159db6dd27",
			bytes:  1196,
		},
		filepath.Join("fixtures", "valid-cancelled.json"): {
			sha256: "31cbfeb23968c07d158b591c28efb3dd40cffba1fd6d1fd4202f43d63bfe9f2d",
			bytes:  1201,
		},
		filepath.Join("fixtures", "redaction-rejected.json"): {
			sha256: "c23fd255c139f94e729ecca70622d6e6341cf7a257c21fbccf592964b0e3853e",
			bytes:  1227,
		},
		filepath.Join("fixtures", "error-cursor-expired.json"): {
			sha256: "9f85ec830e0e61d0ac75656a980793d6d5c4020265d42ed192693659e1540437",
			bytes:  162,
		},
	}
	for relativePath, identity := range expected {
		t.Run(filepath.Base(relativePath), func(t *testing.T) {
			payload := readCompletedExecutionContractFile(t, relativePath)
			digest := sha256.Sum256(payload)
			if got := hex.EncodeToString(digest[:]); got != identity.sha256 {
				t.Fatalf("sha256=%s want=%s", got, identity.sha256)
			}
			if len(payload) != identity.bytes {
				t.Fatalf("bytes=%d want=%d", len(payload), identity.bytes)
			}
		})
	}
}

func TestMapCompletedExecutionV1GoldenFixtures(t *testing.T) {
	testCases := []struct {
		name          string
		status        CompletedExecutionTerminalStatus
		eventType     string
		outcomeStatus string
	}{
		{
			name:          "valid-settled.json",
			status:        CompletedExecutionTerminalSettled,
			eventType:     "billing.execution.settled",
			outcomeStatus: "success",
		},
		{
			name:          "valid-cancelled.json",
			status:        CompletedExecutionTerminalCancelled,
			eventType:     "billing.execution.cancelled",
			outcomeStatus: "failure",
		},
	}
	validator, err := evidence.NewSchemaValidator(filepath.Join("..", "..", "..", "schemas", "m0", "evidence-event.schema.json"))
	if err != nil {
		t.Fatalf("load evidence schema: %v", err)
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			payload := readCompletedExecutionContractFile(t, filepath.Join("fixtures", testCase.name))
			pageObservedAt := time.Date(2026, 7, 25, 5, 0, 0, 0, time.UTC)
			result, mapErr := MapCompletedExecutionV1(payload, CompletedExecutionMapOptions{
				SourceInstance: "unirouter-development",
				ObservedAt:     pageObservedAt,
				IngestedAt:     time.Date(2026, 7, 25, 5, 0, 1, 0, time.UTC),
			})
			if mapErr != nil {
				t.Fatalf("map completed execution: %v", mapErr)
			}
			if result.TerminalStatus != testCase.status {
				t.Fatalf("status=%s want=%s", result.TerminalStatus, testCase.status)
			}
			if result.ObservedAt != pageObservedAt {
				t.Fatalf("observed_at=%s want=%s", result.ObservedAt, pageObservedAt)
			}
			if len(result.Events) != 5 {
				t.Fatalf("events=%d want=5", len(result.Events))
			}
			mainEvent := result.Events[0]
			if mainEvent["event_type"] != testCase.eventType {
				t.Fatalf("event_type=%v want=%s", mainEvent["event_type"], testCase.eventType)
			}
			outcome, _ := mainEvent["outcome"].(map[string]any)
			if outcome["outcome_status"] != testCase.outcomeStatus {
				t.Fatalf("outcome=%#v want=%s", outcome, testCase.outcomeStatus)
			}
			fieldProvenance, _ := mainEvent["field_provenance"].(map[string]any)
			if fieldProvenance["outcome"] != "direct" ||
				fieldProvenance["payload_policy"] != "declared" {
				t.Fatalf("field provenance=%#v", fieldProvenance)
			}
			usage, _ := mainEvent["usage"].(map[string]any)
			if usage["cost_micros"] != result.Snapshot.Money.ChargedCostMicros {
				t.Fatalf("normalized charged cost=%#v snapshot=%s", usage["cost_micros"], result.Snapshot.Money.ChargedCostMicros)
			}
			for index, event := range result.Events {
				if event["source_version"] != CompletedExecutionSchemaV1 ||
					event["capture_mode"] != completedExecutionImportMode ||
					event["adapter_manifest_id"] != DefaultCompletedExecutionAdapterManifestID {
					t.Fatalf("event[%d] identity=%#v", index, event)
				}
				if event["occurred_at"] != result.TerminalAvailableAt.Format(time.RFC3339Nano) ||
					event["observed_at"] != result.TerminalAvailableAt.Format(time.RFC3339Nano) {
					t.Fatalf("event[%d] uses unstable time: occurred=%v observed=%v", index, event["occurred_at"], event["observed_at"])
				}
				if err := validator.Validate(event); err != nil {
					t.Fatalf("event[%d] schema: %v\nevent=%#v", index, err, event)
				}
				if findings := evidence.DetectSensitive(event["raw_source_excerpt"]); len(findings) != 0 {
					t.Fatalf("event[%d] sensitive finding=%+v", index, findings[0])
				}
				if index > 0 && event["event_type"] != "gap.capture.detected" {
					t.Fatalf("event[%d] type=%v want=gap.capture.detected", index, event["event_type"])
				}
			}
		})
	}
}

func TestCompletedExecutionReplayHashesIgnorePollAndIngestTimes(t *testing.T) {
	payload := readCompletedExecutionContractFile(t, filepath.Join("fixtures", "valid-settled.json"))
	first, err := MapCompletedExecutionV1(payload, CompletedExecutionMapOptions{
		SourceInstance: "unirouter-development",
		ObservedAt:     time.Date(2026, 7, 25, 5, 0, 0, 0, time.UTC),
		IngestedAt:     time.Date(2026, 7, 25, 5, 0, 1, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("first map: %v", err)
	}
	second, err := MapCompletedExecutionV1(payload, CompletedExecutionMapOptions{
		SourceInstance: "unirouter-development",
		ObservedAt:     time.Date(2026, 7, 25, 6, 0, 0, 0, time.UTC),
		IngestedAt:     time.Date(2026, 7, 25, 6, 0, 1, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("second map: %v", err)
	}
	for index := range first.Events {
		if first.Events[index]["idempotency_key"] != second.Events[index]["idempotency_key"] {
			t.Fatalf("event[%d] idempotency changed", index)
		}
		if first.Events[index]["event_hash"] != second.Events[index]["event_hash"] {
			t.Fatalf("event[%d] hash changed: first=%v second=%v", index, first.Events[index]["event_hash"], second.Events[index]["event_hash"])
		}
	}
}

func TestCompletedExecutionMappingTreatsSourceClockAsMetadata(t *testing.T) {
	payload := readCompletedExecutionContractFile(t, filepath.Join("fixtures", "valid-settled.json"))
	sourceGeneratedAt := time.Date(2026, 7, 25, 3, 0, 0, 0, time.UTC)
	result, err := MapCompletedExecutionV1(payload, CompletedExecutionMapOptions{
		SourceInstance: "unirouter-development",
		ObservedAt:     sourceGeneratedAt,
		IngestedAt:     time.Date(2026, 7, 25, 6, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("map source-clock-skewed page: %v", err)
	}
	if !result.ObservedAt.Equal(sourceGeneratedAt) ||
		!result.TerminalAvailableAt.After(sourceGeneratedAt) {
		t.Fatalf("result observed=%s terminal=%s", result.ObservedAt, result.TerminalAvailableAt)
	}
	for index, event := range result.Events {
		if event["observed_at"] != result.TerminalAvailableAt.Format(time.RFC3339Nano) {
			t.Fatalf("event[%d] observed_at=%v", index, event["observed_at"])
		}
	}
}

func TestDecodeCompletedExecutionV1RejectsRedactionFixture(t *testing.T) {
	payload := readCompletedExecutionContractFile(t, filepath.Join("fixtures", "redaction-rejected.json"))
	_, _, err := DecodeCompletedExecutionV1(payload)
	if err == nil || !strings.Contains(err.Error(), "prohibited raw field") {
		t.Fatalf("error=%v want prohibited raw field", err)
	}
}

func TestCompletedExecutionMoneyCanonicalDigitBounds(t *testing.T) {
	payload := readCompletedExecutionContractFile(t, filepath.Join("fixtures", "valid-settled.json"))
	var snapshot map[string]any
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	money := snapshot["money"].(map[string]any)
	money["calculated_cost_micros"] = "9999999999999999999"
	money["charged_cost_micros"] = "9999999999999999999"
	nineteenDigits, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal 19 digits: %v", err)
	}
	if _, _, err := DecodeCompletedExecutionV1(nineteenDigits); err != nil {
		t.Fatalf("19 digits rejected: %v", err)
	}

	money["charged_cost_micros"] = "10000000000000000000"
	twentyDigits, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal 20 digits: %v", err)
	}
	if _, _, err := DecodeCompletedExecutionV1(twentyDigits); err == nil ||
		!strings.Contains(err.Error(), "at most 19 digits") {
		t.Fatalf("20 digit error=%v", err)
	}
}

func TestDecodeCompletedExecutionV1RejectsStrictShapeAndCrossFieldFailures(t *testing.T) {
	payload := readCompletedExecutionContractFile(t, filepath.Join("fixtures", "valid-settled.json"))
	testCases := []struct {
		name    string
		mutate  func(map[string]any)
		wantErr string
	}{
		{
			name: "unknown",
			mutate: func(snapshot map[string]any) {
				snapshot["unexpected"] = true
			},
			wantErr: "unknown field",
		},
		{
			name: "required null",
			mutate: func(snapshot map[string]any) {
				snapshot["request_id"] = nil
			},
			wantErr: "request_id is required",
		},
		{
			name: "prompt partition",
			mutate: func(snapshot map[string]any) {
				usage := snapshot["usage"].(map[string]any)
				usage["uncached_prompt_tokens"] = float64(6)
			},
			wantErr: "prompt token partition",
		},
		{
			name: "duplicate gap",
			mutate: func(snapshot map[string]any) {
				gaps := snapshot["capture_gaps"].([]any)
				gaps[3].(map[string]any)["source"] = "call_log"
			},
			wantErr: "duplicate or unsupported",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var snapshot map[string]any
			if err := json.Unmarshal(payload, &snapshot); err != nil {
				t.Fatalf("decode fixture: %v", err)
			}
			testCase.mutate(snapshot)
			mutated, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatalf("marshal mutation: %v", err)
			}
			if _, _, err := DecodeCompletedExecutionV1(mutated); err == nil ||
				!strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("error=%v want substring %q", err, testCase.wantErr)
			}
		})
	}
}

func TestDecodeCompletedExecutionV1RejectsDuplicateAndOversizedBytes(t *testing.T) {
	duplicate := []byte(`{"schema_version":"unirouter.completed-execution.v1","schema_version":"unirouter.completed-execution.v1"}`)
	if _, _, err := DecodeCompletedExecutionV1(duplicate); err == nil || !strings.Contains(err.Error(), "duplicate field") {
		t.Fatalf("duplicate error=%v", err)
	}
	oversized := bytes.Repeat([]byte{' '}, CompletedExecutionMaxBytes+1)
	if _, _, err := DecodeCompletedExecutionV1(oversized); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized error=%v", err)
	}
}

func readCompletedExecutionContractFile(t *testing.T, relativePath string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "contracts", "completed-execution", "v1", relativePath)
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read contract %s: %v", relativePath, err)
	}
	return payload
}
