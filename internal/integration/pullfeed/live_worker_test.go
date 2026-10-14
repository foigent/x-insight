package pullfeed

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestLiveWorkerActivationIsForwardOnlyAndDurable(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	source := &liveSequenceSource{payloads: [][]byte{
		mustLivePage(t, FeedPage{
			SchemaVersion:  SchemaVersion,
			SourceInstance: "unirouter-development",
			GeneratedAt:    now,
			Items:          []FeedItem{},
			NextCursor:     "cursor-activation",
			HighWatermark:  "watermark-activation",
			HasMore:        false,
		}),
	}}
	state := &liveMemoryState{}
	worker := mustLiveWorker(t, source, state, LiveCommitFunc(func(
		context.Context,
		FeedPage,
		FeedItem,
	) (LiveItemCommitResult, error) {
		t.Fatal("activation must not commit an item")
		return LiveItemCommitResult{}, nil
	}), now)

	activated, err := worker.ensureActivation(context.Background())
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	if !state.exists ||
		activated.Cursor != "cursor-activation" ||
		activated.HighWatermark != "watermark-activation" ||
		activated.SourceInstance != "unirouter-development" ||
		activated.AuthorizationRef != AcceptedLiveContractRef {
		t.Fatalf("activation=%+v", activated)
	}
	if len(source.cursors) != 1 || source.cursors[0] != "" {
		t.Fatalf("activation cursors=%v", source.cursors)
	}
}

func TestLiveWorkerCursorAdvancesOnlyAfterEveryItemConverges(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	item := mustLiveFeedItem(t)
	page := FeedPage{
		SchemaVersion:  SchemaVersion,
		SourceInstance: "unirouter-development",
		GeneratedAt:    now,
		Items:          []FeedItem{item},
		NextCursor:     "cursor-1",
		HighWatermark:  "watermark-1",
	}
	state := activatedMemoryState(now)
	source := &liveSequenceSource{payloads: [][]byte{mustLivePage(t, page), mustLivePage(t, page)}}
	attempt := 0
	worker := mustLiveWorker(t, source, state, LiveCommitFunc(func(
		context.Context,
		FeedPage,
		FeedItem,
	) (LiveItemCommitResult, error) {
		attempt++
		if attempt == 1 {
			return LiveItemCommitResult{}, NewLiveWorkerError("evidence_persist_failed", true, 0, errors.New("temporary"))
		}
		return LiveItemCommitResult{DuplicateNoop: true}, nil
	}), now)

	if _, err := worker.PollOnce(context.Background()); err == nil {
		t.Fatal("expected first item failure")
	}
	if state.state.Cursor != "cursor-0" || state.commitCalls != 0 || state.state.Status != WorkerStatusBackoff {
		t.Fatalf("state after failure=%+v commits=%d", state.state, state.commitCalls)
	}

	report, err := worker.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("replay poll: %v", err)
	}
	if report.ItemsReceived != 1 || report.ItemsCommitted != 0 || report.DuplicateNoop != 1 {
		t.Fatalf("report=%+v", report)
	}
	if state.state.Cursor != "cursor-1" || state.commitCalls != 1 {
		t.Fatalf("state after replay=%+v commits=%d", state.state, state.commitCalls)
	}
}

func TestLiveWorkerAcceptsDelayedMaterializationAfterBootstrapCursor(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	item := mustLiveFeedItem(t)
	if !item.TerminalAvailableAt.Before(now) {
		t.Fatalf("fixture terminal time must precede activation: %s", item.TerminalAvailableAt)
	}
	state := activatedMemoryState(now)
	state.state.ActivatedAt = now
	source := &liveSequenceSource{payloads: [][]byte{mustLivePage(t, FeedPage{
		SchemaVersion:  SchemaVersion,
		SourceInstance: state.state.SourceInstance,
		GeneratedAt:    now.Add(-time.Minute),
		Items:          []FeedItem{item},
		NextCursor:     "cursor-after-delayed-materialization",
		HighWatermark:  "watermark-after-delayed-materialization",
	})}}
	committed := 0
	worker := mustLiveWorker(t, source, state, LiveCommitFunc(func(
		_ context.Context,
		_ FeedPage,
		got FeedItem,
	) (LiveItemCommitResult, error) {
		committed++
		if got.RequestID != item.RequestID {
			t.Fatalf("item=%+v", got)
		}
		return LiveItemCommitResult{}, nil
	}), now)

	report, err := worker.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("delayed materialization poll: %v", err)
	}
	if committed != 1 || report.ItemsCommitted != 1 ||
		state.state.Cursor != "cursor-after-delayed-materialization" {
		t.Fatalf("committed=%d report=%+v state=%+v", committed, report, state.state)
	}
}

func TestFreshDatabaseBootstrapRejectsRetiredSourceIdentityBeforeCommit(t *testing.T) {
	now := time.Date(2026, 8, 5, 2, 0, 0, 0, time.UTC)
	state := activatedMemoryState(now)
	state.state.SourceInstance = "ecs.second-integration.v2"
	state.state.CheckpointEpoch = 7
	source := &liveSequenceSource{payloads: [][]byte{mustLivePage(t, FeedPage{
		SchemaVersion:  SchemaVersion,
		SourceInstance: "unirouter-dev-retired",
		GeneratedAt:    now,
		Items:          []FeedItem{},
		NextCursor:     state.state.Cursor,
		HighWatermark:  state.state.HighWatermark,
		HasMore:        false,
	})}}
	worker := mustLiveWorker(t, source, state, LiveCommitFunc(func(
		context.Context,
		FeedPage,
		FeedItem,
	) (LiveItemCommitResult, error) {
		t.Fatal("retired source identity must be rejected before commit")
		return LiveItemCommitResult{}, nil
	}), now)

	_, err := worker.PollOnce(context.Background())
	var workerErr *LiveWorkerError
	if !errors.As(err, &workerErr) ||
		workerErr.Code != "source_instance_mismatch" ||
		workerErr.Retryable ||
		!errors.Is(workerErr, ErrSourceInstanceMismatch) {
		t.Fatalf("error=%v", err)
	}
	if state.state.Cursor != "cursor-0" ||
		state.commitCalls != 0 ||
		state.state.Status != WorkerStatusError ||
		state.state.SourceInstance != "ecs.second-integration.v2" ||
		state.state.CheckpointEpoch != 7 {
		t.Fatalf("state=%+v", state.state)
	}
}

func TestDecodeLivePageAcceptsCrossHostPageClockSkew(t *testing.T) {
	item := mustLiveFeedItem(t)
	page, err := DecodeLivePage(mustLivePage(t, FeedPage{
		SchemaVersion:  SchemaVersion,
		SourceInstance: "unirouter-development",
		GeneratedAt:    item.TerminalAvailableAt.Add(-time.Minute),
		Items:          []FeedItem{item},
		NextCursor:     "cursor-after-item",
		HighWatermark:  "watermark-after-item",
	}))
	if err != nil {
		t.Fatalf("decode live page with source clock skew: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].RequestID != item.RequestID {
		t.Fatalf("page=%+v", page)
	}
}

func TestLiveWorkerRejectsNonemptyPageWithoutCursorProgress(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	state := activatedMemoryState(now)
	source := &liveSequenceSource{payloads: [][]byte{mustLivePage(t, FeedPage{
		SchemaVersion:  SchemaVersion,
		SourceInstance: state.state.SourceInstance,
		GeneratedAt:    now,
		Items:          []FeedItem{mustLiveFeedItem(t)},
		NextCursor:     state.state.Cursor,
		HighWatermark:  state.state.HighWatermark,
		HasMore:        false,
	})}}
	worker := mustLiveWorker(t, source, state, LiveCommitFunc(func(
		context.Context,
		FeedPage,
		FeedItem,
	) (LiveItemCommitResult, error) {
		t.Fatal("non-progressing page must not call committer")
		return LiveItemCommitResult{}, nil
	}), now)

	_, err := worker.PollOnce(context.Background())
	var workerErr *LiveWorkerError
	if !errors.As(err, &workerErr) || workerErr.Code != "cursor_no_progress" || workerErr.Retryable {
		t.Fatalf("error=%v", err)
	}
	if state.state.Cursor != "cursor-0" || state.state.Status != WorkerStatusError {
		t.Fatalf("state=%+v", state.state)
	}
}

func TestLiveWorkerRejectsPostActivationEmptyPageCursorAdvance(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	state := activatedMemoryState(now)
	source := &liveSequenceSource{payloads: [][]byte{mustLivePage(t, FeedPage{
		SchemaVersion:  SchemaVersion,
		SourceInstance: state.state.SourceInstance,
		GeneratedAt:    now,
		Items:          []FeedItem{},
		NextCursor:     "cursor-skipped-without-item",
		HighWatermark:  "watermark-skipped-without-item",
		HasMore:        false,
	})}}
	worker := mustLiveWorker(t, source, state, LiveCommitFunc(func(
		context.Context,
		FeedPage,
		FeedItem,
	) (LiveItemCommitResult, error) {
		t.Fatal("empty page must not call committer")
		return LiveItemCommitResult{}, nil
	}), now)

	_, err := worker.PollOnce(context.Background())
	var workerErr *LiveWorkerError
	if !errors.As(err, &workerErr) ||
		workerErr.Code != "empty_page_cursor_advance" ||
		workerErr.Retryable ||
		!errors.Is(workerErr, ErrEmptyPageCursorAdvance) {
		t.Fatalf("error=%v", err)
	}
	if state.state.Cursor != "cursor-0" ||
		state.commitCalls != 0 ||
		state.state.Status != WorkerStatusError {
		t.Fatalf("state=%+v commits=%d", state.state, state.commitCalls)
	}
}

func TestLiveWorkerAuthFailureIsPermanentAndCursorStaysPut(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	state := activatedMemoryState(now)
	source := &liveSequenceSource{errs: []error{&HTTPSourceError{
		Disposition: HTTPErrorPermanent,
		Category:    HTTPErrorCategoryStatus,
		StatusCode:  401,
	}}}
	worker := mustLiveWorker(t, source, state, LiveCommitFunc(func(
		context.Context,
		FeedPage,
		FeedItem,
	) (LiveItemCommitResult, error) {
		t.Fatal("auth failure must not call committer")
		return LiveItemCommitResult{}, nil
	}), now)

	_, err := worker.PollOnce(context.Background())
	var workerErr *LiveWorkerError
	if !errors.As(err, &workerErr) || workerErr.Retryable || workerErr.Code != "source_status_401" {
		t.Fatalf("error=%v", err)
	}
	if state.state.Cursor != "cursor-0" ||
		state.state.Status != WorkerStatusError ||
		state.state.LastErrorCode != "source_status_401" {
		t.Fatalf("state=%+v", state.state)
	}
}

func TestLiveWorkerRestartAfterCredential401PreservesCursorAndRecovers(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	state := activatedMemoryState(now)
	item := mustLiveFeedItem(t)
	source := &liveSequenceSource{
		errs: []error{&HTTPSourceError{
			Disposition: HTTPErrorPermanent,
			Category:    HTTPErrorCategoryStatus,
			StatusCode:  401,
		}, nil},
		payloads: [][]byte{nil, mustLivePage(t, FeedPage{
			SchemaVersion:  SchemaVersion,
			SourceInstance: state.state.SourceInstance,
			GeneratedAt:    now,
			Items:          []FeedItem{item},
			NextCursor:     "cursor-after-credential-restart",
			HighWatermark:  "watermark-after-credential-restart",
		})},
	}
	committed := 0
	committer := LiveCommitFunc(func(
		_ context.Context,
		_ FeedPage,
		got FeedItem,
	) (LiveItemCommitResult, error) {
		if got.RequestID != item.RequestID {
			t.Fatalf("item=%+v", got)
		}
		committed++
		return LiveItemCommitResult{}, nil
	})

	firstProcess := mustLiveWorker(t, source, state, committer, now)
	if _, err := firstProcess.PollOnce(context.Background()); err == nil {
		t.Fatal("expected the old credential to receive a permanent 401")
	}
	if state.state.Cursor != "cursor-0" || committed != 0 {
		t.Fatalf("state before restart=%+v committed=%d", state.state, committed)
	}

	restartedProcess := mustLiveWorker(t, source, state, committer, now.Add(time.Second))
	report, err := restartedProcess.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("poll after credential restart: %v", err)
	}
	if report.ItemsCommitted != 1 ||
		committed != 1 ||
		state.state.Cursor != "cursor-after-credential-restart" ||
		!reflect.DeepEqual(source.cursors, []string{"cursor-0", "cursor-0"}) {
		t.Fatalf(
			"report=%+v committed=%d state=%+v cursors=%v",
			report,
			committed,
			state.state,
			source.cursors,
		)
	}
}

func TestDecodeLivePageRejectsMutableOrMismatchedEnvelope(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	item := mustLiveFeedItem(t)
	item.Revision = "2"
	payload := mustLivePage(t, FeedPage{
		SchemaVersion:  SchemaVersion,
		SourceInstance: "unirouter-development",
		GeneratedAt:    now,
		Items:          []FeedItem{item},
		NextCursor:     "cursor-1",
		HighWatermark:  "watermark-1",
	})
	if _, err := DecodeLivePage(payload); err == nil {
		t.Fatal("expected immutable revision rejection")
	}

	item = mustLiveFeedItem(t)
	item.TimelineComplete = false
	payload = mustLivePage(t, FeedPage{
		SchemaVersion:  SchemaVersion,
		SourceInstance: "unirouter-development",
		GeneratedAt:    now,
		Items:          []FeedItem{item},
		NextCursor:     "cursor-1",
		HighWatermark:  "watermark-1",
	})
	if _, err := DecodeLivePage(payload); err == nil {
		t.Fatal("expected incomplete timeline rejection")
	}
}

type liveSequenceSource struct {
	payloads [][]byte
	errs     []error
	cursors  []string
	calls    int
}

func (s *liveSequenceSource) Fetch(_ context.Context, cursor string, _ int) ([]byte, error) {
	s.cursors = append(s.cursors, cursor)
	index := s.calls
	s.calls++
	if index < len(s.errs) && s.errs[index] != nil {
		return nil, s.errs[index]
	}
	if index >= len(s.payloads) {
		return nil, errors.New("unexpected source fetch")
	}
	return s.payloads[index], nil
}

type liveMemoryState struct {
	exists      bool
	state       PostgresWorkerState
	commitCalls int
}

func activatedMemoryState(now time.Time) *liveMemoryState {
	return &liveMemoryState{
		exists: true,
		state: PostgresWorkerState{
			SourceInstance:          "unirouter-development",
			ActivationCursor:        "cursor-0",
			ActivationHighWatermark: "watermark-0",
			ActivatedAt:             now.Add(-time.Hour),
			AuthorizationRef:        AcceptedLiveContractRef,
			Cursor:                  "cursor-0",
			HighWatermark:           "watermark-0",
			Status:                  WorkerStatusIdle,
		},
	}
}

func (s *liveMemoryState) Load(context.Context) (PostgresWorkerState, error) {
	if !s.exists {
		return PostgresWorkerState{}, ErrWorkerStateNotFound
	}
	return s.state, nil
}

func (s *liveMemoryState) Activate(_ context.Context, activation DurableActivation) (PostgresWorkerState, error) {
	if s.exists {
		return PostgresWorkerState{}, ErrWorkerStateActivationConflict
	}
	s.exists = true
	s.state = PostgresWorkerState{
		SourceInstance:          activation.SourceInstance,
		ActivationCursor:        activation.Cursor,
		ActivationHighWatermark: activation.HighWatermark,
		ActivatedAt:             activation.ActivatedAt,
		AuthorizationRef:        activation.AuthorizationRef,
		Cursor:                  activation.Cursor,
		HighWatermark:           activation.HighWatermark,
		Status:                  WorkerStatusIdle,
	}
	return s.state, nil
}

func (s *liveMemoryState) MarkPollStarted(_ context.Context, expected string, _ time.Time) error {
	if expected != s.state.Cursor {
		return ErrWorkerStateConflict
	}
	s.state.Status = WorkerStatusPolling
	return nil
}

func (s *liveMemoryState) CommitPage(_ context.Context, expected string, page PageCommit) error {
	if expected != s.state.Cursor || s.state.Status != WorkerStatusPolling {
		return ErrWorkerStateConflict
	}
	if err := validatePageCommit(page); err != nil {
		return err
	}
	s.commitCalls++
	s.state.Cursor = page.NextCursor
	s.state.HighWatermark = page.HighWatermark
	s.state.Status = WorkerStatusIdle
	s.state.ItemsReceived += page.ItemsReceived
	s.state.ItemsCommitted += page.ItemsCommitted
	s.state.DuplicateNoop += page.DuplicateNoop
	return nil
}

func (s *liveMemoryState) MarkFailure(_ context.Context, expected string, failure WorkerFailure) error {
	if expected != s.state.Cursor || s.state.Status != WorkerStatusPolling {
		return ErrWorkerStateConflict
	}
	if failure.Retryable {
		s.state.Status = WorkerStatusBackoff
	} else {
		s.state.Status = WorkerStatusError
	}
	s.state.LastErrorCode = failure.Code
	return nil
}

func (s *liveMemoryState) MarkStopping(_ context.Context, expected string, _ time.Time) error {
	if expected != s.state.Cursor {
		return ErrWorkerStateConflict
	}
	s.state.Status = WorkerStatusStopping
	return nil
}

func (s *liveMemoryState) MarkInactive(_ context.Context, expected string, _ time.Time) error {
	if expected != s.state.Cursor {
		return ErrWorkerStateConflict
	}
	s.state.Status = WorkerStatusInactive
	return nil
}

func mustLiveWorker(
	t *testing.T,
	source Source,
	state LiveWorkerStateStore,
	committer LiveItemCommitter,
	now time.Time,
) *LiveWorker {
	t.Helper()
	worker, err := NewLiveWorker(source, state, committer, LiveWorkerConfig{
		PollInterval:     time.Second,
		AuthorizationRef: AcceptedLiveContractRef,
		Now:              func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new live worker: %v", err)
	}
	return worker
}

func mustLiveFeedItem(t *testing.T) FeedItem {
	t.Helper()
	return mustLiveFeedItemFromFixture(t, "valid-settled.json")
}

func mustLiveFeedItemFromFixture(t *testing.T, fixture string) FeedItem {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "contracts", "completed-execution", "v1", "fixtures", fixture,
	))
	if err != nil {
		t.Fatalf("read live fixture: %v", err)
	}
	var snapshot struct {
		RequestID string `json:"request_id"`
		Terminal  struct {
			AvailableAt time.Time `json:"available_at"`
		} `json:"terminal"`
	}
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatalf("decode live fixture: %v", err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, payload); err != nil {
		t.Fatalf("compact live fixture: %v", err)
	}
	payload = compact.Bytes()
	digest := sha256.Sum256(payload)
	return FeedItem{
		RequestID:           snapshot.RequestID,
		TerminalAvailableAt: snapshot.Terminal.AvailableAt,
		Revision:            LiveSnapshotRevision,
		TimelineComplete:    true,
		SnapshotSHA256:      "sha256:" + hex.EncodeToString(digest[:]),
		Snapshot:            payload,
	}
}

func mustLivePage(t *testing.T, page FeedPage) []byte {
	t.Helper()
	payload, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("marshal live page: %v", err)
	}
	return payload
}
