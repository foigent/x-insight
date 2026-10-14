package pullfeed_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/integration/pullfeed"
)

type sourceFunc func(context.Context, string, int) ([]byte, error)

func (f sourceFunc) Fetch(ctx context.Context, cursor string, limit int) ([]byte, error) {
	return f(ctx, cursor, limit)
}

func TestConsumerDefaultsDisabledAndRejectsLiveMode(t *testing.T) {
	consumer, err := pullfeed.NewConsumer(pullfeed.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("new disabled consumer: %v", err)
	}
	if _, err := consumer.Poll(context.Background()); !errors.Is(err, pullfeed.ErrDisabled) {
		t.Fatalf("poll error=%v", err)
	}
	if _, err := pullfeed.NewConsumer(pullfeed.Config{Mode: "live"}, nil, nil); !errors.Is(err, pullfeed.ErrLiveContractNotAccepted) {
		t.Fatalf("live mode error=%v", err)
	}
	if _, err := pullfeed.NewConsumer(pullfeed.Config{Mode: pullfeed.ModeMock}, sourceFunc(nil), pullfeed.CommitFunc(nil)); err == nil {
		t.Fatal("expected mock activation validation error")
	}
}

func TestMockConsumerCommitsPageThenAdvancesCursor(t *testing.T) {
	terminal := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	payload := validPage(t, terminal, "cursor-next")
	var fetchedCursor string
	source := sourceFunc(func(_ context.Context, cursor string, limit int) ([]byte, error) {
		fetchedCursor = cursor
		if limit != pullfeed.MaxPageItems {
			t.Fatalf("limit=%d", limit)
		}
		return payload, nil
	})
	committer := pullfeed.CommitFunc(func(_ context.Context, item pullfeed.FeedItem) (pullfeed.CommitResult, error) {
		if item.RequestID != "req-prd2-001" || item.Revision != "1" || item.TimelineComplete {
			t.Fatalf("unexpected item: %+v", item)
		}
		return pullfeed.CommitResult{SearchVisibleAt: terminal.Add(4 * time.Second)}, nil
	})
	consumer, err := pullfeed.NewConsumer(pullfeed.Config{
		Mode:       pullfeed.ModeMock,
		Activation: activation(terminal, "activation-now"),
	}, source, committer)
	if err != nil {
		t.Fatalf("new mock consumer: %v", err)
	}

	report, err := consumer.Poll(context.Background())
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if fetchedCursor != "activation-now" || report.FromCursor != "activation-now" {
		t.Fatalf("fetched=%q report=%+v", fetchedCursor, report)
	}
	if report.ItemsReceived != 1 || report.ItemsCommitted != 1 || report.DuplicateNoop != 0 || report.IncompleteTimelines != 1 {
		t.Fatalf("report=%+v", report)
	}
	if report.NextCursor != "cursor-next" || report.HighWatermark != "cursor-next" || consumer.Cursor() != "cursor-next" || consumer.HighWatermark() != "cursor-next" {
		t.Fatalf("report=%+v cursor=%q", report, consumer.Cursor())
	}
	if got := pullfeed.VisibilityP95(report.VisibilityLatencies); got != 4*time.Second {
		t.Fatalf("p95=%s", got)
	}
	if !pullfeed.MeetsDiagnosticVisibilityTarget(report.VisibilityLatencies) {
		t.Fatalf("diagnostic target not met: %+v", report.VisibilityLatencies)
	}
	if consumer.Activation().AuthorizationRef != "decision://prd2/mock-activation" {
		t.Fatalf("activation=%+v", consumer.Activation())
	}
}

func TestMockConsumerRetainsCursorWhenCommitFails(t *testing.T) {
	terminal := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	source := sourceFunc(func(context.Context, string, int) ([]byte, error) {
		return validPage(t, terminal, "cursor-next"), nil
	})
	commitFailure := errors.New("database unavailable")
	consumer, err := pullfeed.NewConsumer(
		pullfeed.Config{Mode: pullfeed.ModeMock, Activation: activation(terminal, "activation-now")},
		source,
		pullfeed.CommitFunc(func(context.Context, pullfeed.FeedItem) (pullfeed.CommitResult, error) {
			return pullfeed.CommitResult{}, commitFailure
		}),
	)
	if err != nil {
		t.Fatalf("new mock consumer: %v", err)
	}

	if _, err := consumer.Poll(context.Background()); !errors.Is(err, commitFailure) {
		t.Fatalf("poll error=%v", err)
	}
	if consumer.Cursor() != "activation-now" {
		t.Fatalf("cursor advanced to %q", consumer.Cursor())
	}
}

func TestMockConsumerReplaysAcceptedRevisionAsNoopThenCommitsLaterCompleteRevision(t *testing.T) {
	terminal := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	pages := [][]byte{
		pageWithItems(t, terminal, "cursor-1", "watermark-2", false, item(t, terminal, "req-prd2-replay", "1", false, "partial")),
		pageWithItems(t, terminal, "cursor-2", "watermark-2", false, item(t, terminal, "req-prd2-replay", "1", false, "partial")),
		pageWithItems(t, terminal, "cursor-3", "watermark-3", false, item(t, terminal, "req-prd2-replay", "2", true, "complete")),
	}
	call := 0
	source := sourceFunc(func(_ context.Context, _ string, _ int) ([]byte, error) {
		payload := pages[call]
		call++
		return payload, nil
	})
	commits := 0
	consumer, err := pullfeed.NewConsumer(
		pullfeed.Config{Mode: pullfeed.ModeMock, Activation: activation(terminal, "activation-now")},
		source,
		pullfeed.CommitFunc(func(_ context.Context, _ pullfeed.FeedItem) (pullfeed.CommitResult, error) {
			commits++
			return pullfeed.CommitResult{SearchVisibleAt: terminal.Add(2 * time.Second)}, nil
		}),
	)
	if err != nil {
		t.Fatalf("new consumer: %v", err)
	}

	first, err := consumer.Poll(context.Background())
	if err != nil || first.ItemsCommitted != 1 || first.IncompleteTimelines != 1 {
		t.Fatalf("first report=%+v err=%v", first, err)
	}
	second, err := consumer.Poll(context.Background())
	if err != nil || second.ItemsCommitted != 0 || second.DuplicateNoop != 1 || second.IncompleteTimelines != 1 {
		t.Fatalf("second report=%+v err=%v", second, err)
	}
	third, err := consumer.Poll(context.Background())
	if err != nil || third.ItemsCommitted != 1 || third.IncompleteTimelines != 0 {
		t.Fatalf("third report=%+v err=%v", third, err)
	}
	if commits != 2 || consumer.Cursor() != "cursor-3" || consumer.HighWatermark() != "watermark-3" {
		t.Fatalf("commits=%d cursor=%q watermark=%q", commits, consumer.Cursor(), consumer.HighWatermark())
	}
}

func TestMockConsumerKeepsPerItemProgressWhenLaterItemFails(t *testing.T) {
	terminal := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	payload := pageWithItems(
		t,
		terminal,
		"cursor-next",
		"cursor-next",
		false,
		item(t, terminal, "req-prd2-first", "1", true, "first"),
		item(t, terminal, "req-prd2-second", "1", true, "second"),
	)
	source := sourceFunc(func(context.Context, string, int) ([]byte, error) { return payload, nil })
	secondAttempts := 0
	firstCommits := 0
	consumer, err := pullfeed.NewConsumer(
		pullfeed.Config{Mode: pullfeed.ModeMock, Activation: activation(terminal, "activation-now")},
		source,
		pullfeed.CommitFunc(func(_ context.Context, feedItem pullfeed.FeedItem) (pullfeed.CommitResult, error) {
			if feedItem.RequestID == "req-prd2-first" {
				firstCommits++
			}
			if feedItem.RequestID == "req-prd2-second" {
				secondAttempts++
				if secondAttempts == 1 {
					return pullfeed.CommitResult{}, errors.New("injected item failure")
				}
			}
			return pullfeed.CommitResult{SearchVisibleAt: terminal.Add(time.Second)}, nil
		}),
	)
	if err != nil {
		t.Fatalf("new consumer: %v", err)
	}

	first, err := consumer.Poll(context.Background())
	if err == nil || first.ItemsCommitted != 1 || consumer.Cursor() != "activation-now" {
		t.Fatalf("first report=%+v cursor=%q err=%v", first, consumer.Cursor(), err)
	}
	second, err := consumer.Poll(context.Background())
	if err != nil || second.DuplicateNoop != 1 || second.ItemsCommitted != 1 || consumer.Cursor() != "cursor-next" {
		t.Fatalf("second report=%+v cursor=%q err=%v", second, consumer.Cursor(), err)
	}
	if firstCommits != 1 || secondAttempts != 2 {
		t.Fatalf("first_commits=%d second_attempts=%d", firstCommits, secondAttempts)
	}
}

func TestMockConsumerRejectsSourceRevisionTimelineAndCursorRegressions(t *testing.T) {
	terminal := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)

	t.Run("source instance", func(t *testing.T) {
		payload := pageWithItems(t, terminal, "cursor-next", "cursor-next", false, item(t, terminal, "req-source", "1", true, "source"))
		payload = bytes.Replace(payload, []byte("unirouter-field-mock"), []byte("different-instance"), 1)
		consumer := mustConsumer(t, terminal, sourceFunc(func(context.Context, string, int) ([]byte, error) { return payload, nil }))
		if _, err := consumer.Poll(context.Background()); !errors.Is(err, pullfeed.ErrSourceInstanceMismatch) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("cursor progress", func(t *testing.T) {
		payload := pageWithItems(t, terminal, "activation-now", "watermark", true, item(t, terminal, "req-cursor", "1", true, "cursor"))
		consumer := mustConsumer(t, terminal, sourceFunc(func(context.Context, string, int) ([]byte, error) { return payload, nil }))
		if _, err := consumer.Poll(context.Background()); !errors.Is(err, pullfeed.ErrCursorNoProgress) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("same revision conflict", func(t *testing.T) {
		pages := [][]byte{
			pageWithItems(t, terminal, "cursor-1", "watermark", false, item(t, terminal, "req-conflict", "1", false, "original")),
			pageWithItems(t, terminal, "cursor-2", "watermark", false, item(t, terminal, "req-conflict", "1", false, "changed")),
		}
		call := 0
		consumer := mustConsumer(t, terminal, sourceFunc(func(context.Context, string, int) ([]byte, error) {
			payload := pages[call]
			call++
			return payload, nil
		}))
		if _, err := consumer.Poll(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := consumer.Poll(context.Background()); !errors.Is(err, pullfeed.ErrRevisionConflict) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("unseen older revision", func(t *testing.T) {
		pages := [][]byte{
			pageWithItems(t, terminal, "cursor-1", "watermark", false, item(t, terminal, "req-regress", "2", false, "two")),
			pageWithItems(t, terminal, "cursor-2", "watermark", false, item(t, terminal, "req-regress", "1", false, "one")),
		}
		call := 0
		consumer := mustConsumer(t, terminal, sourceFunc(func(context.Context, string, int) ([]byte, error) {
			payload := pages[call]
			call++
			return payload, nil
		}))
		_, _ = consumer.Poll(context.Background())
		if _, err := consumer.Poll(context.Background()); !errors.Is(err, pullfeed.ErrRevisionRegression) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("timeline completeness", func(t *testing.T) {
		pages := [][]byte{
			pageWithItems(t, terminal, "cursor-1", "watermark", false, item(t, terminal, "req-timeline", "1", true, "complete")),
			pageWithItems(t, terminal, "cursor-2", "watermark", false, item(t, terminal, "req-timeline", "2", false, "incomplete")),
		}
		call := 0
		consumer := mustConsumer(t, terminal, sourceFunc(func(context.Context, string, int) ([]byte, error) {
			payload := pages[call]
			call++
			return payload, nil
		}))
		_, _ = consumer.Poll(context.Background())
		if _, err := consumer.Poll(context.Background()); !errors.Is(err, pullfeed.ErrTimelineRegression) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("pre-activation item", func(t *testing.T) {
		payload := pageWithItems(
			t,
			terminal,
			"cursor-next",
			"watermark",
			false,
			item(t, terminal.Add(-2*time.Minute), "req-pre-activation", "1", true, "old"),
		)
		consumer := mustConsumer(t, terminal, sourceFunc(func(context.Context, string, int) ([]byte, error) { return payload, nil }))
		if _, err := consumer.Poll(context.Background()); !errors.Is(err, pullfeed.ErrPreActivationItem) {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestMockConsumerRejectsConcurrentPoll(t *testing.T) {
	terminal := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	entered := make(chan struct{})
	release := make(chan struct{})
	source := sourceFunc(func(context.Context, string, int) ([]byte, error) {
		close(entered)
		<-release
		return validPage(t, terminal, "cursor-next"), nil
	})
	consumer := mustConsumer(t, terminal, source)
	result := make(chan error, 1)
	go func() {
		_, err := consumer.Poll(context.Background())
		result <- err
	}()
	<-entered
	if _, err := consumer.Poll(context.Background()); !errors.Is(err, pullfeed.ErrPollInProgress) {
		t.Fatalf("concurrent poll error=%v", err)
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatalf("first poll: %v", err)
	}
}

func TestDecodePageRejectsHashMismatchAndUnknownFields(t *testing.T) {
	terminal := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	var page map[string]any
	if err := json.Unmarshal(validPage(t, terminal, "cursor-next"), &page); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	items := page["items"].([]any)
	items[0].(map[string]any)["snapshot_sha256"] = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	mismatched, _ := json.Marshal(page)
	if _, err := pullfeed.DecodePage(mismatched); err == nil {
		t.Fatal("expected hash mismatch")
	}

	page["unexpected"] = true
	unknown, _ := json.Marshal(page)
	if _, err := pullfeed.DecodePage(unknown); err == nil {
		t.Fatal("expected unknown-field rejection")
	}
}

func TestDecodePageRejectsControlCharacters(t *testing.T) {
	terminal := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	var page map[string]any
	if err := json.Unmarshal(validPage(t, terminal, "cursor-next"), &page); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	page["source_instance"] = "unirouter\u0000hidden"
	payload, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("marshal page: %v", err)
	}
	if _, err := pullfeed.DecodePage(payload); err == nil {
		t.Fatal("expected control-character rejection")
	}
}

func TestDecodePageRejectsSensitiveSnapshotAndUnorderedRevisions(t *testing.T) {
	terminal := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	sensitiveSnapshot := json.RawMessage(`{"schema_version":"unirouter.snapshot.mock.v1","request_ids":["req-sensitive"],"safety":{"raw_prompts_included":false,"raw_responses_included":false,"api_keys_included":false},"raw_prompt":"blocked"}`)
	digest := sha256.Sum256(sensitiveSnapshot)
	sensitiveItem := pullfeed.FeedItem{
		RequestID:           "req-sensitive",
		TerminalAvailableAt: terminal,
		Revision:            "1",
		TimelineComplete:    true,
		SnapshotSHA256:      "sha256:" + hex.EncodeToString(digest[:]),
		Snapshot:            sensitiveSnapshot,
	}
	if _, err := pullfeed.DecodePage(pageWithItems(t, terminal, "cursor-next", "watermark", false, sensitiveItem)); err == nil {
		t.Fatal("expected sensitive snapshot rejection")
	}

	unordered := pageWithItems(
		t,
		terminal,
		"cursor-next",
		"watermark",
		false,
		item(t, terminal, "req-unordered", "2", false, "two"),
		item(t, terminal, "req-unordered", "1", false, "one"),
	)
	if _, err := pullfeed.DecodePage(unordered); err == nil {
		t.Fatal("expected unordered revision rejection")
	}
}

func validPage(t *testing.T, terminal time.Time, nextCursor string) []byte {
	t.Helper()
	return pageWithItems(t, terminal, nextCursor, nextCursor, false, item(t, terminal, "req-prd2-001", "1", false, "default"))
}

func activation(terminal time.Time, cursor string) pullfeed.Activation {
	return pullfeed.Activation{
		SourceInstance:   "unirouter-field-mock",
		ActivatedAt:      terminal.Add(-time.Minute),
		Cursor:           cursor,
		AuthorizationRef: "decision://prd2/mock-activation",
	}
}

func mustConsumer(t *testing.T, terminal time.Time, source sourceFunc) *pullfeed.Consumer {
	t.Helper()
	consumer, err := pullfeed.NewConsumer(
		pullfeed.Config{Mode: pullfeed.ModeMock, Activation: activation(terminal, "activation-now")},
		source,
		pullfeed.CommitFunc(func(_ context.Context, _ pullfeed.FeedItem) (pullfeed.CommitResult, error) {
			return pullfeed.CommitResult{SearchVisibleAt: terminal.Add(time.Second)}, nil
		}),
	)
	if err != nil {
		t.Fatalf("new consumer: %v", err)
	}
	return consumer
}

func item(t *testing.T, terminal time.Time, requestID string, revision string, complete bool, marker string) pullfeed.FeedItem {
	t.Helper()
	snapshot := json.RawMessage(`{"schema_version":"unirouter.snapshot.mock.v1","request_ids":["` + requestID + `"],"safety":{"raw_prompts_included":false,"raw_responses_included":false,"api_keys_included":false},"marker":"` + marker + `"}`)
	digest := sha256.Sum256(snapshot)
	return pullfeed.FeedItem{
		RequestID:           requestID,
		TerminalAvailableAt: terminal,
		Revision:            revision,
		TimelineComplete:    complete,
		SnapshotSHA256:      "sha256:" + hex.EncodeToString(digest[:]),
		Snapshot:            snapshot,
	}
}

func pageWithItems(t *testing.T, terminal time.Time, nextCursor string, highWatermark string, hasMore bool, items ...pullfeed.FeedItem) []byte {
	t.Helper()
	page := map[string]any{
		"schema_version":  pullfeed.SchemaVersion,
		"source_instance": "unirouter-field-mock",
		"generated_at":    terminal.Add(time.Second),
		"items":           items,
		"next_cursor":     nextCursor,
		"high_watermark":  highWatermark,
		"has_more":        hasMore,
	}
	payload, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("marshal page: %v", err)
	}
	return payload
}
