package litellmfeed

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/litellm"
	"github.com/foigent/x-insight/internal/connector"
)

func TestWorkerPartialFailureKeepsCheckpointAndReplayConverges(t *testing.T) {
	previous := Checkpoint{
		LastStartTime:     "2026-07-28T02:00:00.000000Z",
		BoundaryRequestID: []string{},
	}
	cursor, err := previous.Encode()
	if err != nil {
		t.Fatal(err)
	}
	rows := []litellm.SanitizedRecord{
		mustSanitizedRecord(t, upstreamRow(
			"request-first",
			"2026-07-28T02:00:00.100000Z",
		)),
		mustSanitizedRecord(t, upstreamRow(
			"request-second",
			"2026-07-28T02:00:00.200000Z",
		)),
	}
	state := &recordingState{
		state: WorkerState{
			SourceSystemID:    litellm.SourceSystemID,
			AdapterManifestID: litellm.AdapterManifestID,
			CursorKey:         litellm.CursorKey,
			SourceInstance:    "litellm-development",
			OrgRef:            "1",
			ProjectRef:        "2",
			Cursor:            cursor,
			HighWatermark:     cursor,
			WorkerStatus:      "idle",
		},
	}
	committer := &recordingCommitter{
		failOnceRequestID: "request-second",
		seen:              map[string]bool{},
	}
	projector, err := litellm.NewProjector()
	if err != nil {
		t.Fatalf("new projector: %v", err)
	}
	now := time.Date(2026, 7, 28, 2, 2, 0, 0, time.UTC)
	worker := mustWorker(t, staticWindowSource{
		window: CompleteWindow{
			Rows:            rows,
			Total:           len(rows),
			EligibleThrough: now.Add(-EligibilityDelay),
		},
	}, state, projector, committer, now)

	if _, err := worker.PollOnce(context.Background()); err == nil {
		t.Fatal("expected injected second-item failure")
	}
	if state.state.Cursor != cursor || len(state.commits) != 0 ||
		state.failures != 1 || len(committer.calls) != 2 {
		t.Fatalf(
			"partial state=%+v commits=%d failures=%d calls=%v",
			state.state,
			len(state.commits),
			state.failures,
			committer.calls,
		)
	}

	report, err := worker.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("replay poll: %v", err)
	}
	if report.Accepted != 2 || report.Inserted != 1 ||
		report.DuplicateNoop != 1 || !report.CheckpointAdvanced ||
		len(state.commits) != 1 {
		t.Fatalf("replay report=%+v commits=%+v", report, state.commits)
	}
	next, err := DecodeCheckpoint(state.state.Cursor)
	if err != nil {
		t.Fatalf("decode committed checkpoint: %v", err)
	}
	if next.LastStartTime != "2026-07-28T02:00:00.200000Z" ||
		len(next.BoundaryRequestID) != 1 ||
		next.BoundaryRequestID[0] != "request-second" {
		t.Fatalf("next checkpoint=%+v", next)
	}
}

func TestWorkerCheckpointFailureRecordsBackoffAndReplayConverges(t *testing.T) {
	previous := Checkpoint{
		LastStartTime:     "2026-07-28T02:00:00.000000Z",
		BoundaryRequestID: []string{},
	}
	cursor, err := previous.Encode()
	if err != nil {
		t.Fatal(err)
	}
	row := mustSanitizedRecord(t, upstreamRow(
		"request-checkpoint-failure",
		"2026-07-28T02:00:00.100000Z",
	))
	state := &recordingState{
		state: WorkerState{
			SourceInstance: "litellm-development",
			OrgRef:         "1",
			ProjectRef:     "2",
			Cursor:         cursor,
			HighWatermark:  cursor,
			WorkerStatus:   "idle",
		},
		failPageOnce: true,
	}
	projector, err := litellm.NewProjector()
	if err != nil {
		t.Fatal(err)
	}
	committer := &recordingCommitter{seen: map[string]bool{}}
	now := time.Date(2026, 7, 28, 2, 2, 0, 0, time.UTC)
	worker := mustWorker(t, staticWindowSource{window: CompleteWindow{
		Rows:            []litellm.SanitizedRecord{row},
		Total:           1,
		EligibleThrough: now.Add(-EligibilityDelay),
	}}, state, projector, committer, now)

	_, err = worker.PollOnce(context.Background())
	var workerErr *WorkerError
	if err == nil ||
		!errors.As(err, &workerErr) ||
		workerErr.Code != "state_page_commit_failed" ||
		!workerErr.Retryable {
		t.Fatalf("checkpoint failure=%v", err)
	}
	if state.state.Cursor != cursor ||
		len(state.commits) != 0 ||
		state.failures != 1 ||
		state.lastFailureCode != "state_page_commit_failed" ||
		!state.lastFailureRetryable {
		t.Fatalf(
			"checkpoint failure state=%+v commits=%d failures=%d code=%q retryable=%t",
			state.state,
			len(state.commits),
			state.failures,
			state.lastFailureCode,
			state.lastFailureRetryable,
		)
	}

	report, err := worker.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("checkpoint replay: %v", err)
	}
	if report.Inserted != 0 ||
		report.DuplicateNoop != 1 ||
		!report.CheckpointAdvanced ||
		len(state.commits) != 1 {
		t.Fatalf("checkpoint replay report=%+v commits=%+v", report, state.commits)
	}
}

func TestWorkerRejectsSourceRowBeyondEligibilityFenceWithoutCommit(t *testing.T) {
	previous := Checkpoint{
		LastStartTime:     "2026-07-28T02:00:00.500000Z",
		BoundaryRequestID: []string{},
	}
	cursor, err := previous.Encode()
	if err != nil {
		t.Fatal(err)
	}
	transport := roundTripFunc(func(
		request *http.Request,
	) (*http.Response, error) {
		return jsonResponse(t, request, map[string]any{
			"data": []any{upstreamRow(
				"request-not-final",
				"2026-07-28T02:01:00.000001Z",
			)},
			"total":           1,
			"page":            1,
			"page_size":       100,
			"total_pages":     1,
			"total_is_capped": false,
		}), nil
	})
	source, err := NewHTTPSource(HTTPSourceConfig{
		Endpoint: "https://litellm.test.invalid/spend/logs/v2",
		APIKey:   "test-litellm-key",
	})
	if err != nil {
		t.Fatalf("new HTTP source: %v", err)
	}
	source.client = &http.Client{Transport: transport}
	state := &recordingState{
		state: WorkerState{
			SourceSystemID:    litellm.SourceSystemID,
			AdapterManifestID: litellm.AdapterManifestID,
			CursorKey:         litellm.CursorKey,
			SourceInstance:    "litellm-development",
			OrgRef:            "1",
			ProjectRef:        "2",
			Cursor:            cursor,
			HighWatermark:     cursor,
			WorkerStatus:      "idle",
		},
	}
	projector, err := litellm.NewProjector()
	if err != nil {
		t.Fatalf("new projector: %v", err)
	}
	committer := &recordingCommitter{seen: map[string]bool{}}
	worker := mustWorker(
		t,
		source,
		state,
		projector,
		committer,
		time.Date(2026, 7, 28, 2, 2, 0, 0, time.UTC),
	)
	_, err = worker.PollOnce(context.Background())
	var workerErr *WorkerError
	if err == nil || !errors.As(err, &workerErr) ||
		workerErr.Code != "source_row_outside_requested_window" {
		t.Fatalf("poll error=%v", err)
	}
	if state.state.Cursor != cursor || len(state.commits) != 0 ||
		state.failures != 1 || len(committer.calls) != 0 {
		t.Fatalf(
			"state=%+v commits=%d failures=%d calls=%v",
			state.state,
			len(state.commits),
			state.failures,
			committer.calls,
		)
	}
}

func TestWorkerTreatsHTTP408AsRetryableWithoutCheckpointAdvance(t *testing.T) {
	previous := Checkpoint{
		LastStartTime:     "2026-07-28T02:00:00.000000Z",
		BoundaryRequestID: []string{},
	}
	cursor, err := previous.Encode()
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewHTTPSource(HTTPSourceConfig{
		Endpoint: "https://litellm.test.invalid/spend/logs/v2",
		APIKey:   "test-litellm-key",
	})
	if err != nil {
		t.Fatalf("new HTTP source: %v", err)
	}
	source.client = &http.Client{Transport: roundTripFunc(func(
		request *http.Request,
	) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusRequestTimeout,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       http.NoBody,
			Request:    request,
		}, nil
	})}
	state := &recordingState{
		state: WorkerState{
			SourceSystemID:    litellm.SourceSystemID,
			AdapterManifestID: litellm.AdapterManifestID,
			CursorKey:         litellm.CursorKey,
			SourceInstance:    "litellm-development",
			OrgRef:            "1",
			ProjectRef:        "2",
			Cursor:            cursor,
			HighWatermark:     cursor,
			WorkerStatus:      "idle",
		},
	}
	projector, err := litellm.NewProjector()
	if err != nil {
		t.Fatalf("new projector: %v", err)
	}
	committer := &recordingCommitter{seen: map[string]bool{}}
	worker := mustWorker(
		t,
		source,
		state,
		projector,
		committer,
		time.Date(2026, 7, 28, 2, 2, 0, 0, time.UTC),
	)
	_, err = worker.PollOnce(context.Background())
	var workerErr *WorkerError
	if err == nil || !errors.As(err, &workerErr) ||
		workerErr.Code != "source_status_408" || !workerErr.Retryable {
		t.Fatalf("poll error=%v", err)
	}
	if state.state.Cursor != cursor || len(state.commits) != 0 ||
		state.failures != 1 || len(committer.calls) != 0 {
		t.Fatalf(
			"state=%+v commits=%d failures=%d calls=%v",
			state.state,
			len(state.commits),
			state.failures,
			committer.calls,
		)
	}
}

func TestWorkerRejectsMappingScopeDriftBeforeFetchOrCommit(t *testing.T) {
	checkpoint := Checkpoint{
		LastStartTime:     "2026-07-28T02:00:00.000000Z",
		BoundaryRequestID: []string{},
	}
	cursor, err := checkpoint.Encode()
	if err != nil {
		t.Fatal(err)
	}
	source := &recordingWindowSource{}
	state := &recordingState{state: WorkerState{
		SourceInstance: "litellm-development",
		OrgRef:         "1",
		ProjectRef:     "3",
		Cursor:         cursor,
		HighWatermark:  cursor,
		WorkerStatus:   "idle",
	}}
	projector, err := litellm.NewProjector()
	if err != nil {
		t.Fatal(err)
	}
	committer := &recordingCommitter{seen: map[string]bool{}}
	worker := mustWorker(
		t,
		source,
		state,
		projector,
		committer,
		time.Date(2026, 7, 28, 2, 2, 0, 0, time.UTC),
	)
	_, err = worker.PollOnce(context.Background())
	var workerErr *WorkerError
	if err == nil ||
		!errors.As(err, &workerErr) ||
		workerErr.Code != "mapping_scope_mismatch" {
		t.Fatalf("scope drift error=%v", err)
	}
	if source.fetches != 0 ||
		len(committer.calls) != 0 ||
		len(state.commits) != 0 ||
		state.state.Cursor != cursor {
		t.Fatalf(
			"scope drift fetched=%d committed=%d pages=%d cursor=%q",
			source.fetches,
			len(committer.calls),
			len(state.commits),
			state.state.Cursor,
		)
	}
}

func TestWorkerFirstActivationEqualFenceIsNoopThenNextPollProceeds(
	t *testing.T,
) {
	activationTime := time.Date(
		2026,
		7,
		28,
		2,
		1,
		0,
		123456000,
		time.UTC,
	)
	checkpoint := ActivationCheckpoint(activationTime)
	cursor, err := checkpoint.Encode()
	if err != nil {
		t.Fatal(err)
	}
	source := &recordingWindowSource{}
	state := &recordingState{state: WorkerState{
		SourceInstance: "litellm-development",
		OrgRef:         "1",
		ProjectRef:     "2",
		Cursor:         cursor,
		HighWatermark:  cursor,
		WorkerStatus:   "idle",
	}}
	projector, err := litellm.NewProjector()
	if err != nil {
		t.Fatal(err)
	}
	committer := &recordingCommitter{seen: map[string]bool{}}
	now := activationTime
	worker, err := NewWorker(
		source,
		state,
		projector,
		committer,
		WorkerConfig{
			SourceInstance: "litellm-development",
			OrgRef:         "1",
			ProjectRef:     "2",
			PollInterval:   time.Second,
			Now:            func() time.Time { return now },
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	report, err := worker.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("equal-fence first poll: %v", err)
	}
	if report != (PollReport{}) ||
		source.fetches != 0 ||
		len(committer.calls) != 0 ||
		len(state.commits) != 0 ||
		state.state.Cursor != cursor {
		t.Fatalf(
			"equal-fence report=%+v fetches=%d commits=%v pages=%v cursor=%q",
			report,
			source.fetches,
			committer.calls,
			state.commits,
			state.state.Cursor,
		)
	}
	now = now.Add(time.Microsecond)
	report, err = worker.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("later poll: %v", err)
	}
	if source.fetches != 1 ||
		len(committer.calls) != 0 ||
		len(state.commits) != 1 ||
		report.Accepted != 0 ||
		report.CheckpointAdvanced ||
		state.state.Cursor != cursor {
		t.Fatalf(
			"later report=%+v fetches=%d commits=%v pages=%v cursor=%q",
			report,
			source.fetches,
			committer.calls,
			state.commits,
			state.state.Cursor,
		)
	}
}

func mustWorker(
	t *testing.T,
	source WindowSource,
	state DurableState,
	projector ItemProjector,
	committer connector.EvidenceCommitPort,
	now time.Time,
) *Worker {
	t.Helper()
	worker, err := NewWorker(source, state, projector, committer, WorkerConfig{
		SourceInstance: "litellm-development",
		OrgRef:         "1",
		ProjectRef:     "2",
		PollInterval:   time.Second,
		Now:            func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	return worker
}

func mustSanitizedRecord(t *testing.T, row map[string]any) litellm.SanitizedRecord {
	t.Helper()
	payload, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	record, err := litellm.SanitizeUpstreamRowBytes(payload)
	if err != nil {
		t.Fatalf("sanitize row: %v", err)
	}
	return record
}

type staticWindowSource struct {
	window CompleteWindow
	err    error
}

type recordingWindowSource struct {
	fetches int
}

func (source *recordingWindowSource) FetchCompleteWindow(
	context.Context,
	Checkpoint,
	time.Time,
) (CompleteWindow, error) {
	source.fetches++
	return CompleteWindow{}, nil
}

func (source staticWindowSource) FetchCompleteWindow(
	context.Context,
	Checkpoint,
	time.Time,
) (CompleteWindow, error) {
	return source.window, source.err
}

type recordingState struct {
	state                WorkerState
	commits              []PageCommit
	failures             int
	failPageOnce         bool
	lastFailureCode      string
	lastFailureRetryable bool
}

func (state *recordingState) Load(context.Context) (WorkerState, error) {
	if state.state.Cursor == "" {
		return WorkerState{}, ErrStateNotFound
	}
	return state.state, nil
}

func (state *recordingState) Activate(
	_ context.Context,
	sourceInstance string,
	scope ScopeBinding,
	checkpoint Checkpoint,
	_ time.Time,
) (WorkerState, error) {
	cursor, err := checkpoint.Encode()
	if err != nil {
		return WorkerState{}, err
	}
	state.state = WorkerState{
		SourceInstance: sourceInstance,
		OrgRef:         scope.OrgRef,
		ProjectRef:     scope.ProjectRef,
		Cursor:         cursor,
		HighWatermark:  cursor,
	}
	if scope.TenantRef != nil {
		state.state.TenantRefPresent = true
		state.state.TenantRef = *scope.TenantRef
	}
	return state.state, nil
}

func (*recordingState) MarkPollStarted(context.Context, string, time.Time) error {
	return nil
}

func (state *recordingState) CommitPage(_ context.Context, commit PageCommit) error {
	if commit.ExpectedCursor != state.state.Cursor {
		return ErrStateConflict
	}
	if state.failPageOnce {
		state.failPageOnce = false
		return errors.New("injected checkpoint persistence failure")
	}
	state.commits = append(state.commits, commit)
	state.state.Cursor = commit.NextCursor
	state.state.HighWatermark = commit.NextCursor
	return nil
}

func (state *recordingState) MarkFailure(
	_ context.Context,
	_ string,
	code string,
	retryable bool,
	_ time.Time,
) error {
	state.failures++
	state.lastFailureCode = code
	state.lastFailureRetryable = retryable
	return nil
}

func (*recordingState) MarkInactive(context.Context, string, time.Time) error {
	return nil
}

type recordingCommitter struct {
	failOnceRequestID string
	failed            bool
	seen              map[string]bool
	calls             []string
}

func (committer *recordingCommitter) CommitItem(
	_ context.Context,
	item connector.NormalizedItem,
) (connector.ItemConvergence, *connector.ConnectorError) {
	requestID := item.Envelope.EventID
	committer.calls = append(committer.calls, requestID)
	if requestID == committer.failOnceRequestID && !committer.failed {
		committer.failed = true
		return "", connector.NewConnectorError(
			connector.ErrorPersistence,
			"injected_item_failure",
			connector.PhaseCommit,
			"commit_item",
			true,
			"injected item failure",
			connector.RecoveryAutomaticRetry,
		)
	}
	if committer.seen[requestID] {
		return connector.ConvergenceDuplicateNoop, nil
	}
	committer.seen[requestID] = true
	return connector.ConvergenceInserted, nil
}
