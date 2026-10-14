package evidence

import (
	"path/filepath"
	"testing"
)

func TestFixtureRunner(t *testing.T) {
	runner, err := NewFixtureRunner(filepath.Join("..", "..", "schemas", "m0", "evidence-event.schema.json"))
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	report, err := runner.RunDir(filepath.Join("..", "..", "fixtures", "m0"))
	if err != nil {
		t.Fatalf("run fixtures: %v", err)
	}
	if report.Failed > 0 {
		for _, result := range report.Results {
			if !result.Pass {
				t.Errorf("%s expected=%s actual=%s reason=%s", result.Name, result.Expected, result.Actual, result.Reason)
			}
		}
	}
}

func TestEventHashIgnoresIngestionTime(t *testing.T) {
	event := map[string]any{
		"id":                   "00000000-0000-0000-0000-000000000001",
		"schema_version":       "m0.1",
		"source_system_id":     "11111111-1111-1111-1111-111111111111",
		"adapter_manifest_id":  "22222222-2222-2222-2222-222222222222",
		"source_type":          "unirouter",
		"source_ref":           "unirouter:local:request_trace_events:1",
		"idempotency_key":      "key",
		"event_type":           "gateway.request.accepted",
		"stage":                "gateway",
		"severity":             "info",
		"capture_mode":         "db_import",
		"payload_capture_mode": "metadata_only",
		"ingested_at":          "2026-07-09T00:00:00Z",
		"correlation":          map[string]any{"request_id": "req-1"},
		"payload_policy":       map[string]any{"payload_capture_mode": "metadata_only"},
		"field_provenance":     map[string]any{"correlation": "direct"},
	}
	first, err := GenerateEventHash(event)
	if err != nil {
		t.Fatalf("first hash: %v", err)
	}
	event["ingested_at"] = "2026-07-09T00:01:00Z"
	second, err := GenerateEventHash(event)
	if err != nil {
		t.Fatalf("second hash: %v", err)
	}
	if first != second {
		t.Fatalf("hash changed with ingested_at: %s != %s", first, second)
	}
}
