package unirouter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSnapshot(t *testing.T) {
	report, err := DiscoverSnapshot(filepath.Join("..", "..", "..", "fixtures", "unirouter_snapshot", "minimal.json"), "test")
	if err != nil {
		t.Fatalf("discover snapshot: %v", err)
	}
	if report.Mode != "snapshot" {
		t.Fatalf("mode=%s", report.Mode)
	}
	if len(report.Tables) != 2 {
		t.Fatalf("tables=%d", len(report.Tables))
	}
	for _, table := range report.Tables {
		if !table.Present {
			t.Fatalf("table %s not present", table.Name)
		}
		if table.RowCount == nil || *table.RowCount == 0 {
			t.Fatalf("table %s row count missing", table.Name)
		}
	}
	if report.Safety.RawPromptsIncluded {
		t.Fatalf("raw prompts should be false")
	}
	if len(report.Safety.UnsafeKeysObserved) != 0 {
		t.Fatalf("unexpected unsafe keys: %v", report.Safety.UnsafeKeysObserved)
	}
}

func TestDiscoverSnapshotFileUsesBoundSnapshot(t *testing.T) {
	path := filepath.Join("..", "..", "..", "fixtures", "unirouter_snapshot", "minimal.json")
	snapshot, err := LoadSnapshot(path)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	report, err := DiscoverSnapshotFile(snapshot, snapshot.SourceInstance)
	if err != nil {
		t.Fatalf("discover bound snapshot: %v", err)
	}
	if report.Snapshot == nil || len(report.Snapshot.RequestIDs) != len(snapshot.RequestIDs) {
		t.Fatalf("snapshot summary=%+v", report.Snapshot)
	}
}

func TestDiscoverSnapshotFlagsUnsafeKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unsafe.json")
	data := []byte(`{
		"source": "unirouter",
		"source_instance": "test",
		"safety": {
			"raw_prompts_included": false,
			"raw_responses_included": false,
			"api_keys_included": false
		},
		"call_logs": [{"request_id": "req-1", "api_key_id": 1}],
		"request_trace_events": [{"request_id": "req-1", "raw_prompt": "blocked"}]
	}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	report, err := DiscoverSnapshot(path, "test")
	if err != nil {
		t.Fatalf("discover snapshot: %v", err)
	}
	if len(report.Safety.UnsafeKeysObserved) == 0 {
		t.Fatalf("expected unsafe keys")
	}
	if len(report.Warnings) == 0 {
		t.Fatalf("expected safety warning")
	}
}
