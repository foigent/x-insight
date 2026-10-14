package checkpointepoch

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPrepareValidationRequiresAQuiescentBoundaryAndKnownWorker(t *testing.T) {
	valid := PrepareInput{
		Binding: Binding{
			SourceSystemID:    "11111111-1111-4111-8111-111111111111",
			AdapterManifestID: "22222222-2222-4222-8222-222222222222",
			CursorKey:         CompletedExecutionCursorKey,
		},
		WorkerUnit:         CompletedExecutionWorkerUnit,
		NextSourceInstance: "source-campaign-2",
		NextCursor:         "cursor-2",
		NextHighWatermark:  "cursor-2",
		AuthorizationRef:   "decision://second-integration/epoch-2",
	}
	if err := validatePrepareInput(valid); err != nil {
		t.Fatalf("valid prepare input: %v", err)
	}
	invalidBoundary := valid
	invalidBoundary.NextHighWatermark = "watermark-other"
	if err := validatePrepareInput(invalidBoundary); err == nil {
		t.Fatal("non-quiescent next boundary was accepted")
	}
	invalidWorker := valid
	invalidWorker.WorkerUnit = "arbitrary.service"
	if err := validatePrepareInput(invalidWorker); err == nil {
		t.Fatal("unmanaged worker unit was accepted")
	}
	mismatchedWorker := valid
	mismatchedWorker.WorkerUnit = LiteLLMWorkerUnit
	if err := validatePrepareInput(mismatchedWorker); err == nil {
		t.Fatal("worker/cursor binding mismatch was accepted")
	}
	liteLLM := valid
	liteLLM.WorkerUnit = LiteLLMWorkerUnit
	liteLLM.CursorKey = LiteLLMCursorKey
	liteLLM.NextCursor = strings.Repeat("x", completedExecutionBoundaryBytes+1)
	liteLLM.NextHighWatermark = liteLLM.NextCursor
	if err := validatePrepareInput(liteLLM); err != nil {
		t.Fatalf("LiteLLM boundary above completed-execution limit: %v", err)
	}
	if err := ValidateOperationBinding(strings.Repeat("a", 32), "sha256:"+strings.Repeat("b", 64)); err != nil {
		t.Fatalf("valid operation binding: %v", err)
	}
}

func TestReportHashesOpaqueDeliveryStateInsteadOfExposingIt(t *testing.T) {
	now := time.Date(2026, 8, 5, 1, 2, 3, 456789000, time.UTC)
	state := activeState{
		ID: "33333333-3333-4333-8333-333333333333",
		Binding: Binding{
			SourceSystemID:    "11111111-1111-4111-8111-111111111111",
			AdapterManifestID: "22222222-2222-4222-8222-222222222222",
			CursorKey:         "source.delivery.v1",
		},
		Epoch:                   2,
		EpochOperationID:        strings.Repeat("c", 32),
		SourceInstance:          "source-campaign-2",
		ActivationCursor:        "opaque-activation-cursor",
		ActivationHighWatermark: "opaque-activation-watermark",
		ActivatedAt:             now.Add(-time.Hour),
		AuthorizationRef:        "decision://second-integration/epoch-2",
		Cursor:                  "opaque-live-cursor",
		HighWatermark:           "opaque-live-cursor",
		LegacyStatus:            "idle",
		WorkerStatus:            "inactive",
		PagesCommitted:          4,
		ItemsReceived:           4,
		ItemsCommitted:          4,
		CreatedAt:               now.Add(-2 * time.Hour),
		UpdatedAt:               now,
	}
	report, err := reportFrom(state, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(payload)
	for _, forbidden := range []string{
		"opaque-live-cursor", "opaque-activation-cursor", "opaque-activation-watermark",
	} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("report exposed delivery token %q: %s", forbidden, encoded)
		}
	}
	if report.Active.CursorSHA256 != hashText(state.Cursor) || report.Active.StateSHA256 == "" {
		t.Fatalf("report hashes=%+v", report.Active)
	}
}

func TestActiveStateHashChangesForMutableDeliveryState(t *testing.T) {
	now := time.Date(2026, 8, 5, 1, 2, 3, 0, time.UTC)
	state := activeState{
		ID: "33333333-3333-4333-8333-333333333333",
		Binding: Binding{
			SourceSystemID:    "11111111-1111-4111-8111-111111111111",
			AdapterManifestID: "22222222-2222-4222-8222-222222222222",
			CursorKey:         "source.delivery.v1",
		},
		Epoch: 1, SourceInstance: "source-campaign-1",
		ActivationCursor: "cursor-1", ActivatedAt: now,
		AuthorizationRef: "decision://campaign-1", Cursor: "cursor-1", HighWatermark: "cursor-1",
		LegacyStatus: "idle", WorkerStatus: "inactive", CreatedAt: now, UpdatedAt: now,
	}
	first, err := hashActiveState(state)
	if err != nil {
		t.Fatal(err)
	}
	state.UpdatedAt = state.UpdatedAt.Add(time.Microsecond)
	second, err := hashActiveState(state)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("active state hash ignored mutable delivery state")
	}
}
