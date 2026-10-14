package connector

import (
	"testing"
	"time"
)

func TestPhase2BOperationalTypesAndLifecycleProjection(t *testing.T) {
	retryAfter := 7
	retryable := NewConnectorError(
		ErrorTransport,
		"source_status_503",
		PhaseFetch,
		"poll",
		true,
		"Source is temporarily unavailable",
		RecoveryAutomaticRetry,
	)
	retryable.RetryAfterSeconds = &retryAfter
	if err := retryable.Validate(); err != nil {
		t.Fatalf("validate retryable error: %v", err)
	}
	permanent := NewConnectorError(
		ErrorCursor,
		"source.cursor_expired",
		PhaseCheckpoint,
		"poll",
		false,
		"Source cursor expired",
		RecoveryExplicitResync,
	)
	if err := permanent.Validate(); err != nil {
		t.Fatalf("validate permanent error: %v", err)
	}

	lifecycle, err := ProjectLifecycle(LocalLifecycleState{
		SourceStatus:        "active",
		AdapterActive:       true,
		Activated:           true,
		AuthorizationRef:    "contract-ref",
		WorkerStatus:        "backoff",
		PagesCommitted:      2,
		ConsecutiveFailures: 1,
		LastError:           retryable,
	})
	if err != nil {
		t.Fatalf("project lifecycle: %v", err)
	}
	if lifecycle.Configuration != "configured" ||
		lifecycle.Authentication != "valid" ||
		lifecycle.Transport != "backoff" ||
		lifecycle.Processing != "backoff" ||
		lifecycle.Checkpoint != "steady" ||
		lifecycle.Health != "degraded" {
		t.Fatalf("lifecycle=%+v", lifecycle)
	}

	lifecycle, err = ProjectLifecycle(LocalLifecycleState{
		SourceStatus:     "active",
		AdapterActive:    true,
		Activated:        true,
		AuthorizationRef: "contract-ref",
		WorkerStatus:     "error",
		PagesCommitted:   2,
		LastError:        permanent,
	})
	if err != nil {
		t.Fatalf("project permanent lifecycle: %v", err)
	}
	if lifecycle.Checkpoint != "error" ||
		lifecycle.Health != "permanent_error" {
		t.Fatalf("permanent lifecycle=%+v", lifecycle)
	}
}

func TestPhase2BCheckpointReceiptAndConvergence(t *testing.T) {
	binding := sampleManifestBinding()
	updatedAt := time.Date(2026, 7, 28, 8, 1, 2, 345678000, time.UTC)
	checkpointState := LocalCheckpointState{
		ProducerInstance: "source-development",
		Cursor:           "cursor-0",
		HighWatermark:    "watermark-0",
		Phase:            "steady",
		UpdatedAt:        updatedAt,
	}
	checkpoint, err := ProjectCheckpoint(binding, checkpointState)
	if err != nil {
		t.Fatalf("project checkpoint: %v", err)
	}
	if checkpoint.UpdatedAt != "2026-07-28T08:01:02.345678Z" {
		t.Fatalf("checkpoint=%+v", checkpoint)
	}
	checkpointState.UpdatedAt = updatedAt.Add(time.Nanosecond)
	if _, err := ProjectCheckpoint(binding, checkpointState); err == nil {
		t.Fatal("sub-microsecond Checkpoint time accepted")
	}

	inserted, connectorErr := ClassifyItemConvergence(5, EvidenceCommitReport{
		EventsTotal: 5,
		Inserted:    5,
	})
	if connectorErr != nil || inserted != ConvergenceInserted {
		t.Fatalf("insert convergence=%s err=%v", inserted, connectorErr)
	}
	duplicate, connectorErr := ClassifyItemConvergence(5, EvidenceCommitReport{
		EventsTotal:   5,
		DuplicateNoop: 5,
	})
	if connectorErr != nil || duplicate != ConvergenceDuplicateNoop {
		t.Fatalf("duplicate convergence=%s err=%v", duplicate, connectorErr)
	}
	if _, connectorErr := ClassifyItemConvergence(5, EvidenceCommitReport{
		EventsTotal:   5,
		Inserted:      4,
		DuplicateNoop: 1,
	}); connectorErr == nil ||
		connectorErr.Category != ErrorConflict ||
		connectorErr.Code != "foigent.commit.partial_convergence" {
		t.Fatalf("partial convergence error=%+v", connectorErr)
	}

	next, connectorErr := ProjectConvergedCheckpoint(
		binding,
		checkpoint,
		BatchConvergence{
			ProducerInstance: "source-development",
			NextCursor:       "cursor-1",
			HighWatermark:    "watermark-1",
			Received:         2,
			Inserted:         1,
			DuplicateNoop:    1,
		},
		updatedAt.Add(time.Second),
	)
	if connectorErr != nil ||
		next.Cursor != "cursor-1" ||
		next.HighWatermark != "watermark-1" {
		t.Fatalf("next checkpoint=%+v err=%v", next, connectorErr)
	}

	unchanged, connectorErr := ProjectConvergedCheckpoint(
		binding,
		checkpoint,
		BatchConvergence{
			ProducerInstance: "source-development",
			NextCursor:       "cursor-1",
			HighWatermark:    "watermark-1",
			Received:         2,
			Inserted:         1,
		},
		updatedAt.Add(time.Second),
	)
	if connectorErr == nil || unchanged != checkpoint {
		t.Fatalf("partial batch advanced checkpoint=%+v err=%v", unchanged, connectorErr)
	}

	emptyCheckpoint, err := ProjectCheckpoint(binding, LocalCheckpointState{
		ProducerInstance: "source-development",
		Phase:            "bootstrap",
		UpdatedAt:        updatedAt,
	})
	if err != nil {
		t.Fatalf("project empty Checkpoint: %v", err)
	}
	emptyOutcome := BatchConvergence{
		ProducerInstance: "source-development",
	}
	if err := emptyOutcome.Validate(); err != nil {
		t.Fatalf("validate empty cursor zero-item convergence: %v", err)
	}
	emptyNext, connectorErr := ProjectConvergedCheckpoint(
		binding,
		emptyCheckpoint,
		emptyOutcome,
		updatedAt.Add(time.Second),
	)
	if connectorErr != nil ||
		emptyNext.Cursor != "" ||
		emptyNext.HighWatermark != "" {
		t.Fatalf("empty cursor zero-item convergence=%+v err=%v", emptyNext, connectorErr)
	}

	receiptInput := OperationalReceiptInput{
		OperationID:      "phase2b-commit-1",
		ProducerInstance: "source-development",
		Operation:        "commit_page",
		Status:           "completed",
		Counts: ReceiptCounts{
			Received:      2,
			Inserted:      1,
			DuplicateNoop: 1,
		},
		CheckpointBefore: checkpoint.Cursor,
		CheckpointAfter:  next.Cursor,
		ObservedAt:       updatedAt.Add(time.Second),
	}
	receipt, err := ProjectOperationalReceipt(binding, receiptInput)
	if err != nil {
		t.Fatalf("project receipt: %v", err)
	}
	if receipt.Error != nil ||
		receipt.Counts.DuplicateNoop != 1 ||
		receipt.ObservedAt != "2026-07-28T08:01:03.345678Z" {
		t.Fatalf("receipt=%+v", receipt)
	}
	receiptInput.ObservedAt = receiptInput.ObservedAt.Add(time.Nanosecond)
	if _, err := ProjectOperationalReceipt(binding, receiptInput); err == nil {
		t.Fatal("sub-microsecond Operational Receipt time accepted")
	}

	invalid := receipt
	invalid.Status = "retryable_error"
	if err := invalid.Validate(); err == nil {
		t.Fatal("retryable receipt without error accepted")
	}
}
