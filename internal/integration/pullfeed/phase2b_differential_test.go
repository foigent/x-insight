package pullfeed

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connector"
)

func TestPhase2BCheckpointStateDifferential(t *testing.T) {
	binding := readPhase2BPullfeedBinding(t)
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	item := mustLiveFeedItem(t)
	for _, testCase := range []struct {
		name      string
		duplicate bool
	}{
		{name: "inserted"},
		{name: "duplicate_noop", duplicate: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			state := activatedMemoryState(now)
			before := projectPhase2BCheckpoint(t, binding, state.state, now)
			page := FeedPage{
				SchemaVersion:  SchemaVersion,
				SourceInstance: state.state.SourceInstance,
				GeneratedAt:    now,
				Items:          []FeedItem{item},
				NextCursor:     "cursor-1",
				HighWatermark:  "watermark-1",
			}
			source := &liveSequenceSource{payloads: [][]byte{mustLivePage(t, page)}}
			worker := mustLiveWorker(t, source, state, LiveCommitFunc(func(
				context.Context,
				FeedPage,
				FeedItem,
			) (LiveItemCommitResult, error) {
				return LiveItemCommitResult{DuplicateNoop: testCase.duplicate}, nil
			}), now)
			report, err := worker.PollOnce(context.Background())
			if err != nil {
				t.Fatalf("poll current worker: %v", err)
			}
			outcome := connector.BatchConvergence{
				ProducerInstance: state.state.SourceInstance,
				NextCursor:       page.NextCursor,
				HighWatermark:    page.HighWatermark,
				Received:         report.ItemsReceived,
				Inserted:         report.ItemsCommitted,
				DuplicateNoop:    report.DuplicateNoop,
			}
			neutral, connectorErr := connector.ProjectConvergedCheckpoint(
				binding,
				before,
				outcome,
				now,
			)
			if connectorErr != nil {
				t.Fatalf("project neutral Checkpoint: %v", connectorErr)
			}
			if state.state.Cursor != neutral.Cursor ||
				state.state.HighWatermark != neutral.HighWatermark ||
				state.state.ItemsReceived != int64(outcome.Received) ||
				state.state.ItemsCommitted != int64(outcome.Inserted) ||
				state.state.DuplicateNoop != int64(outcome.DuplicateNoop) ||
				state.state.Status != WorkerStatusIdle {
				t.Fatalf(
					"current/neutral state drift current=%+v neutral=%+v outcome=%+v",
					state.state,
					neutral,
					outcome,
				)
			}
		})
	}
}

func TestPhase2BPartialPageReplayStateDifferential(t *testing.T) {
	binding := readPhase2BPullfeedBinding(t)
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	state := activatedMemoryState(now)
	before := projectPhase2BCheckpoint(t, binding, state.state, now)
	page := FeedPage{
		SchemaVersion:  SchemaVersion,
		SourceInstance: state.state.SourceInstance,
		GeneratedAt:    now,
		Items: []FeedItem{
			mustLiveFeedItemFromFixture(t, "valid-settled.json"),
			mustLiveFeedItemFromFixture(t, "valid-cancelled.json"),
		},
		NextCursor:    "cursor-after-replay",
		HighWatermark: "watermark-after-replay",
	}
	source := &liveSequenceSource{payloads: [][]byte{
		mustLivePage(t, page),
		mustLivePage(t, page),
	}}
	attempt := 0
	worker := mustLiveWorker(t, source, state, LiveCommitFunc(func(
		context.Context,
		FeedPage,
		FeedItem,
	) (LiveItemCommitResult, error) {
		attempt++
		switch attempt {
		case 1:
			return LiveItemCommitResult{}, nil
		case 2:
			return LiveItemCommitResult{}, NewLiveWorkerError(
				"evidence_persist_failed",
				true,
				0,
				errors.New("temporary"),
			)
		case 3:
			return LiveItemCommitResult{DuplicateNoop: true}, nil
		default:
			return LiveItemCommitResult{}, nil
		}
	}), now)

	if _, err := worker.PollOnce(context.Background()); err == nil {
		t.Fatal("partial page unexpectedly converged")
	}
	if state.state.Cursor != before.Cursor ||
		state.state.HighWatermark != before.HighWatermark ||
		state.commitCalls != 0 ||
		state.state.Status != WorkerStatusBackoff {
		t.Fatalf("current worker advanced after partial page: %+v", state.state)
	}
	unchanged, connectorErr := connector.ProjectConvergedCheckpoint(
		binding,
		before,
		connector.BatchConvergence{
			ProducerInstance: state.state.SourceInstance,
			NextCursor:       page.NextCursor,
			HighWatermark:    page.HighWatermark,
			Received:         2,
			Inserted:         1,
		},
		now,
	)
	if connectorErr == nil ||
		connectorErr.Category != connector.ErrorConflict ||
		unchanged != before {
		t.Fatalf("neutral partial-page result=%+v err=%+v", unchanged, connectorErr)
	}

	report, err := worker.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("replay current worker: %v", err)
	}
	neutral, connectorErr := connector.ProjectConvergedCheckpoint(
		binding,
		before,
		connector.BatchConvergence{
			ProducerInstance: state.state.SourceInstance,
			NextCursor:       page.NextCursor,
			HighWatermark:    page.HighWatermark,
			Received:         report.ItemsReceived,
			Inserted:         report.ItemsCommitted,
			DuplicateNoop:    report.DuplicateNoop,
		},
		now,
	)
	if connectorErr != nil {
		t.Fatalf("project replay Checkpoint: %v", connectorErr)
	}
	if report.ItemsReceived != 2 ||
		report.ItemsCommitted != 1 ||
		report.DuplicateNoop != 1 ||
		state.state.Cursor != neutral.Cursor ||
		state.state.HighWatermark != neutral.HighWatermark ||
		state.commitCalls != 1 ||
		state.state.Status != WorkerStatusIdle {
		t.Fatalf(
			"current/neutral replay drift report=%+v current=%+v neutral=%+v",
			report,
			state.state,
			neutral,
		)
	}
}

func readPhase2BPullfeedBinding(t *testing.T) connector.ManifestBinding {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "contracts", "foigent", "xinsight-projection-binding.v1.json",
	))
	if err != nil {
		t.Fatalf("read Phase 2B binding: %v", err)
	}
	binding, err := connector.ParseManifestBinding(payload)
	if err != nil {
		t.Fatalf("parse Phase 2B binding: %v", err)
	}
	return binding
}

func projectPhase2BCheckpoint(
	t *testing.T,
	binding connector.ManifestBinding,
	state PostgresWorkerState,
	updatedAt time.Time,
) connector.Checkpoint {
	t.Helper()
	checkpoint, err := connector.ProjectCheckpoint(
		binding,
		connector.LocalCheckpointState{
			ProducerInstance: state.SourceInstance,
			Cursor:           state.Cursor,
			HighWatermark:    state.HighWatermark,
			Phase:            "steady",
			UpdatedAt:        updatedAt,
		},
	)
	if err != nil {
		t.Fatalf("project current Checkpoint: %v", err)
	}
	return checkpoint
}
