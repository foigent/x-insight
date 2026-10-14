package pullfeed

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const AcceptedLiveContractRef = "unirouter.completed-execution.v1@sha256:4ae2897d4f8470c38624dfe21c54d72eaf6d995d2c9578c4420c94d244424fb2"

type LiveItemCommitResult struct {
	DuplicateNoop         bool
	CompatibilityCompared bool
	CompatibilityMatched  bool
	CompatibilityMismatch bool
	CompatibilityError    bool
	CompatibilityEligible bool
}

type LiveItemCommitter interface {
	CommitLive(context.Context, FeedPage, FeedItem) (LiveItemCommitResult, error)
}

type LiveCommitFunc func(context.Context, FeedPage, FeedItem) (LiveItemCommitResult, error)

func (f LiveCommitFunc) CommitLive(
	ctx context.Context,
	page FeedPage,
	item FeedItem,
) (LiveItemCommitResult, error) {
	return f(ctx, page, item)
}

type LiveWorkerStateStore interface {
	Load(context.Context) (PostgresWorkerState, error)
	Activate(context.Context, DurableActivation) (PostgresWorkerState, error)
	MarkPollStarted(context.Context, string, time.Time) error
	CommitPage(context.Context, string, PageCommit) error
	MarkFailure(context.Context, string, WorkerFailure) error
	MarkStopping(context.Context, string, time.Time) error
	MarkInactive(context.Context, string, time.Time) error
}

type LiveWorkerLogFunc func(event string, fields map[string]any)

func (f LiveWorkerLogFunc) Log(event string, fields map[string]any) {
	if f != nil {
		f(event, fields)
	}
}

type LiveWorkerConfig struct {
	PollInterval     time.Duration
	AuthorizationRef string
	RetryBase        time.Duration
	RetryMaximum     time.Duration
	Now              func() time.Time
	Sleep            func(context.Context, time.Duration) error
	Logger           LiveWorkerLogFunc
}

type LivePollReport struct {
	ItemsReceived         int
	ItemsCommitted        int
	DuplicateNoop         int
	CompatibilityCompared int
	CompatibilityMatched  int
	CompatibilityMismatch int
	CompatibilityError    int
	CompatibilityEligible bool
	HasMore               bool
}

type LiveWorkerError struct {
	Code       string
	Retryable  bool
	RetryAfter time.Duration
	cause      error
}

func (e *LiveWorkerError) Error() string {
	if e == nil {
		return "completed-execution worker failed"
	}
	return fmt.Sprintf("completed-execution worker failed with code %s", e.Code)
}

func (e *LiveWorkerError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func NewLiveWorkerError(code string, retryable bool, retryAfter time.Duration, cause error) *LiveWorkerError {
	if !failureCodePattern.MatchString(code) {
		code = "unclassified_failure"
	}
	if retryAfter < 0 {
		retryAfter = 0
	}
	if retryAfter > MaxRetryAfter {
		retryAfter = MaxRetryAfter
	}
	return &LiveWorkerError{
		Code:       code,
		Retryable:  retryable,
		RetryAfter: retryAfter,
		cause:      cause,
	}
}

type LiveWorker struct {
	source    Source
	state     LiveWorkerStateStore
	committer LiveItemCommitter
	config    LiveWorkerConfig
}

func NewLiveWorker(
	source Source,
	state LiveWorkerStateStore,
	committer LiveItemCommitter,
	config LiveWorkerConfig,
) (*LiveWorker, error) {
	if source == nil {
		return nil, fmt.Errorf("live feed source is required")
	}
	if state == nil {
		return nil, fmt.Errorf("live feed state store is required")
	}
	if committer == nil {
		return nil, fmt.Errorf("live feed committer is required")
	}
	if config.PollInterval <= 0 || config.PollInterval > time.Minute {
		return nil, fmt.Errorf("live feed poll interval must be between 1ns and 1m")
	}
	if config.AuthorizationRef == "" {
		config.AuthorizationRef = AcceptedLiveContractRef
	}
	if err := validateBoundedText("live contract authorization ref", config.AuthorizationRef, 1, MaxAuthorizationRefBytes); err != nil {
		return nil, err
	}
	if config.RetryBase == 0 {
		config.RetryBase = time.Second
	}
	if config.RetryMaximum == 0 {
		config.RetryMaximum = time.Minute
	}
	if config.RetryBase <= 0 || config.RetryMaximum < config.RetryBase || config.RetryMaximum > MaxRetryAfter {
		return nil, fmt.Errorf("live feed retry bounds are invalid")
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.Sleep == nil {
		config.Sleep = sleepContext
	}
	return &LiveWorker{
		source:    source,
		state:     state,
		committer: committer,
		config:    config,
	}, nil
}

func (w *LiveWorker) Run(ctx context.Context) error {
	if w == nil {
		return fmt.Errorf("live feed worker is nil")
	}
	failures := 0
	for {
		if err := ctx.Err(); err != nil {
			w.stop()
			return nil
		}
		if _, err := w.ensureActivation(ctx); err != nil {
			workerErr := classifyLiveWorkerError(err, "activation_failed")
			w.logFailure("activation_failed", workerErr)
			if !workerErr.Retryable {
				if waitErr := waitForStop(ctx); waitErr == nil {
					w.stop()
					return nil
				}
			}
			failures++
			if err := w.config.Sleep(ctx, w.retryDelay(failures, workerErr.RetryAfter)); err != nil {
				w.stop()
				return nil
			}
			continue
		}

		report, err := w.PollOnce(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) && ctx.Err() != nil {
				w.stop()
				return nil
			}
			workerErr := classifyLiveWorkerError(err, "poll_failed")
			w.logFailure("poll_failed", workerErr)
			if !workerErr.Retryable {
				if waitErr := waitForStop(ctx); waitErr == nil {
					w.stop()
					return nil
				}
			}
			failures++
			if err := w.config.Sleep(ctx, w.retryDelay(failures, workerErr.RetryAfter)); err != nil {
				w.stop()
				return nil
			}
			continue
		}

		failures = 0
		w.config.Logger.Log("page_committed", map[string]any{
			"items_received":         report.ItemsReceived,
			"items_committed":        report.ItemsCommitted,
			"duplicate_noop":         report.DuplicateNoop,
			"compatibility_compared": report.CompatibilityCompared,
			"compatibility_matched":  report.CompatibilityMatched,
			"compatibility_mismatch": report.CompatibilityMismatch,
			"compatibility_error":    report.CompatibilityError,
			"compatibility_eligible": report.CompatibilityEligible,
			"has_more":               report.HasMore,
		})
		if report.HasMore {
			continue
		}
		if err := w.config.Sleep(ctx, w.config.PollInterval); err != nil {
			w.stop()
			return nil
		}
	}
}

func (w *LiveWorker) PollOnce(ctx context.Context) (LivePollReport, error) {
	state, err := w.state.Load(ctx)
	if err != nil {
		return LivePollReport{}, NewLiveWorkerError("state_load_failed", true, 0, err)
	}
	startedAt := w.config.Now().UTC()
	if err := w.state.MarkPollStarted(ctx, state.Cursor, startedAt); err != nil {
		return LivePollReport{}, NewLiveWorkerError("state_poll_start_failed", true, 0, err)
	}

	fail := func(workerErr *LiveWorkerError) (LivePollReport, error) {
		if ctx.Err() == nil {
			if markErr := w.state.MarkFailure(ctx, state.Cursor, WorkerFailure{
				OccurredAt: w.config.Now().UTC(),
				Code:       workerErr.Code,
				Retryable:  workerErr.Retryable,
			}); markErr != nil {
				return LivePollReport{}, NewLiveWorkerError("state_failure_record_failed", true, 0, markErr)
			}
		}
		return LivePollReport{}, workerErr
	}

	payload, err := w.source.Fetch(ctx, state.Cursor, MaxPageItems)
	if err != nil {
		return fail(classifyLiveWorkerError(err, "source_fetch_failed"))
	}
	page, err := DecodeLivePage(payload)
	if err != nil {
		return fail(NewLiveWorkerError("contract_page_invalid", false, 0, err))
	}
	if page.SourceInstance != state.SourceInstance {
		return fail(NewLiveWorkerError("source_instance_mismatch", false, 0, ErrSourceInstanceMismatch))
	}
	if len(page.Items) == 0 && page.NextCursor != state.Cursor {
		return fail(NewLiveWorkerError("empty_page_cursor_advance", false, 0, ErrEmptyPageCursorAdvance))
	}
	if (page.HasMore || len(page.Items) > 0) && page.NextCursor == state.Cursor {
		return fail(NewLiveWorkerError("cursor_no_progress", false, 0, ErrCursorNoProgress))
	}

	report := LivePollReport{
		ItemsReceived: len(page.Items),
		HasMore:       page.HasMore,
	}
	for _, item := range page.Items {
		result, commitErr := w.committer.CommitLive(ctx, page, item)
		if commitErr != nil {
			return fail(classifyLiveWorkerError(commitErr, "item_commit_failed"))
		}
		if result.DuplicateNoop {
			report.DuplicateNoop++
		} else {
			report.ItemsCommitted++
		}
		if result.CompatibilityCompared {
			report.CompatibilityCompared++
		}
		if result.CompatibilityMatched {
			report.CompatibilityMatched++
		}
		if result.CompatibilityMismatch {
			report.CompatibilityMismatch++
		}
		if result.CompatibilityError {
			report.CompatibilityError++
		}
		if result.CompatibilityCompared {
			if report.CompatibilityCompared == 1 {
				report.CompatibilityEligible = result.CompatibilityEligible
			} else {
				report.CompatibilityEligible =
					report.CompatibilityEligible && result.CompatibilityEligible
			}
		}
	}

	if err := w.state.CommitPage(ctx, state.Cursor, PageCommit{
		NextCursor:     page.NextCursor,
		HighWatermark:  page.HighWatermark,
		CompletedAt:    w.config.Now().UTC(),
		ItemsReceived:  int64(report.ItemsReceived),
		ItemsCommitted: int64(report.ItemsCommitted),
		DuplicateNoop:  int64(report.DuplicateNoop),
	}); err != nil {
		return LivePollReport{}, NewLiveWorkerError("state_page_commit_failed", true, 0, err)
	}
	return report, nil
}

func (w *LiveWorker) ensureActivation(ctx context.Context) (PostgresWorkerState, error) {
	state, err := w.state.Load(ctx)
	if err == nil {
		return state, nil
	}
	if !errors.Is(err, ErrWorkerStateNotFound) {
		return PostgresWorkerState{}, NewLiveWorkerError("state_load_failed", true, 0, err)
	}

	payload, err := w.source.Fetch(ctx, "", MaxPageItems)
	if err != nil {
		return PostgresWorkerState{}, classifyLiveWorkerError(err, "activation_source_fetch_failed")
	}
	page, err := DecodeLivePage(payload)
	if err != nil {
		return PostgresWorkerState{}, NewLiveWorkerError("activation_contract_invalid", false, 0, err)
	}
	if len(page.Items) != 0 || page.HasMore {
		return PostgresWorkerState{}, NewLiveWorkerError("activation_not_forward_only", false, 0, nil)
	}
	state, err = w.state.Activate(ctx, DurableActivation{
		SourceInstance:   page.SourceInstance,
		Cursor:           page.NextCursor,
		HighWatermark:    page.HighWatermark,
		ActivatedAt:      w.config.Now().UTC(),
		AuthorizationRef: w.config.AuthorizationRef,
	})
	if err != nil {
		return PostgresWorkerState{}, NewLiveWorkerError("state_activation_failed", true, 0, err)
	}
	w.config.Logger.Log("worker_activated", map[string]any{
		"contract": LiveSnapshotSchemaVersion,
	})
	return state, nil
}

func (w *LiveWorker) retryDelay(failures int, retryAfter time.Duration) time.Duration {
	delay := w.config.RetryBase
	for index := 1; index < failures && delay < w.config.RetryMaximum; index++ {
		if delay > w.config.RetryMaximum/2 {
			delay = w.config.RetryMaximum
			break
		}
		delay *= 2
	}
	if delay > w.config.RetryMaximum {
		delay = w.config.RetryMaximum
	}
	if retryAfter > delay {
		delay = retryAfter
	}
	if delay > MaxRetryAfter {
		delay = MaxRetryAfter
	}
	return delay
}

func (w *LiveWorker) stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	state, err := w.state.Load(ctx)
	if err != nil {
		return
	}
	now := w.config.Now().UTC()
	if err := w.state.MarkStopping(ctx, state.Cursor, now); err != nil {
		return
	}
	_ = w.state.MarkInactive(ctx, state.Cursor, w.config.Now().UTC())
}

func (w *LiveWorker) logFailure(event string, err *LiveWorkerError) {
	w.config.Logger.Log(event, map[string]any{
		"code":        err.Code,
		"retryable":   err.Retryable,
		"retry_after": err.RetryAfter.String(),
	})
}

func classifyLiveWorkerError(err error, fallbackCode string) *LiveWorkerError {
	var workerErr *LiveWorkerError
	if errors.As(err, &workerErr) {
		return workerErr
	}
	var sourceErr *HTTPSourceError
	if errors.As(err, &sourceErr) {
		code := "source_" + string(sourceErr.Category)
		if sourceErr.Code != "" {
			code = sourceErr.Code
		} else if sourceErr.Category == HTTPErrorCategoryStatus {
			code = fmt.Sprintf("source_status_%d", sourceErr.StatusCode)
		}
		return NewLiveWorkerError(code, sourceErr.IsRetryable(), sourceErr.RetryAfter, err)
	}
	return NewLiveWorkerError(fallbackCode, true, 0, err)
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func waitForStop(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
