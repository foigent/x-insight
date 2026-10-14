package litellmfeed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/foigent/x-insight/internal/adapters/litellm"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
)

type WindowSource interface {
	FetchCompleteWindow(context.Context, Checkpoint, time.Time) (CompleteWindow, error)
}

type DurableState interface {
	Load(context.Context) (WorkerState, error)
	Activate(
		context.Context,
		string,
		ScopeBinding,
		Checkpoint,
		time.Time,
	) (WorkerState, error)
	MarkPollStarted(context.Context, string, time.Time) error
	CommitPage(context.Context, PageCommit) error
	MarkFailure(context.Context, string, string, bool, time.Time) error
	MarkInactive(context.Context, string, time.Time) error
}

type ItemProjector interface {
	Project(
		context.Context,
		litellm.SanitizedRecord,
		litellm.MappingContext,
		connector.ProjectionContext,
	) (connector.NormalizedItem, *connector.ConnectorError)
}

type LogFunc func(event string, fields map[string]any)

func (logger LogFunc) Log(event string, fields map[string]any) {
	if logger != nil {
		logger(event, fields)
	}
}

type WorkerConfig struct {
	SourceInstance string
	TenantRef      *string
	OrgRef         string
	ProjectRef     string
	PollInterval   time.Duration
	RetryBase      time.Duration
	RetryMaximum   time.Duration
	Now            func() time.Time
	Sleep          func(context.Context, time.Duration) error
	Logger         LogFunc
}

type PollReport struct {
	WindowRows         int
	Accepted           int
	Inserted           int
	DuplicateNoop      int
	CheckpointAdvanced bool
}

type WorkerError struct {
	Code      string
	Retryable bool
	cause     error
}

func (workerErr *WorkerError) Error() string {
	if workerErr == nil {
		return "LiteLLM Connector worker failed"
	}
	return "LiteLLM Connector worker failed (" + workerErr.Code + ")"
}

func (workerErr *WorkerError) Unwrap() error {
	if workerErr == nil {
		return nil
	}
	return workerErr.cause
}

type Worker struct {
	source    WindowSource
	state     DurableState
	projector ItemProjector
	committer connector.EvidenceCommitPort
	config    WorkerConfig
}

func NewWorker(
	source WindowSource,
	state DurableState,
	projector ItemProjector,
	committer connector.EvidenceCommitPort,
	config WorkerConfig,
) (*Worker, error) {
	if source == nil || state == nil || projector == nil || committer == nil {
		return nil, errors.New("LiteLLM worker dependencies are required")
	}
	if !validSourceInstance(config.SourceInstance) ||
		config.OrgRef == "" || config.ProjectRef == "" {
		return nil, errors.New("LiteLLM worker mapping identity is invalid")
	}
	if config.PollInterval <= 0 || config.PollInterval > time.Minute {
		return nil, errors.New("LiteLLM worker poll interval is invalid")
	}
	if config.RetryBase == 0 {
		config.RetryBase = time.Second
	}
	if config.RetryMaximum == 0 {
		config.RetryMaximum = time.Minute
	}
	if config.RetryBase <= 0 ||
		config.RetryMaximum < config.RetryBase ||
		config.RetryMaximum > 5*time.Minute {
		return nil, errors.New("LiteLLM worker retry bounds are invalid")
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.Sleep == nil {
		config.Sleep = sleepContext
	}
	return &Worker{
		source:    source,
		state:     state,
		projector: projector,
		committer: committer,
		config:    config,
	}, nil
}

func (worker *Worker) Run(ctx context.Context) error {
	if worker == nil {
		return errors.New("LiteLLM worker is nil")
	}
	failures := 0
	for {
		if ctx.Err() != nil {
			worker.stop()
			return nil
		}
		if _, err := worker.ensureActivation(ctx); err != nil {
			classified := classifyWorkerError(err, "activation_failed")
			worker.config.Logger.Log("activation_failed", map[string]any{
				"code": classified.Code, "retryable": classified.Retryable,
			})
			if !classified.Retryable {
				<-ctx.Done()
				worker.stop()
				return nil
			}
			failures++
			if err := worker.config.Sleep(ctx, worker.retryDelay(failures)); err != nil {
				worker.stop()
				return nil
			}
			continue
		}
		report, err := worker.PollOnce(ctx)
		if err != nil {
			if ctx.Err() != nil {
				worker.stop()
				return nil
			}
			classified := classifyWorkerError(err, "poll_failed")
			worker.config.Logger.Log("poll_failed", map[string]any{
				"code": classified.Code, "retryable": classified.Retryable,
			})
			if !classified.Retryable {
				<-ctx.Done()
				worker.stop()
				return nil
			}
			failures++
			if err := worker.config.Sleep(ctx, worker.retryDelay(failures)); err != nil {
				worker.stop()
				return nil
			}
			continue
		}
		failures = 0
		worker.config.Logger.Log("window_committed", map[string]any{
			"window_rows":         report.WindowRows,
			"accepted":            report.Accepted,
			"inserted":            report.Inserted,
			"duplicate_noop":      report.DuplicateNoop,
			"checkpoint_advanced": report.CheckpointAdvanced,
		})
		if err := worker.config.Sleep(ctx, worker.config.PollInterval); err != nil {
			worker.stop()
			return nil
		}
	}
}

func (worker *Worker) PollOnce(ctx context.Context) (PollReport, error) {
	state, err := worker.state.Load(ctx)
	if err != nil {
		return PollReport{}, &WorkerError{Code: "state_load_failed", Retryable: true, cause: err}
	}
	if err := worker.validateStateBinding(state); err != nil {
		return PollReport{}, err
	}
	checkpoint, err := DecodeCheckpoint(state.Cursor)
	if err != nil {
		return PollReport{}, &WorkerError{Code: "checkpoint_invalid", cause: err}
	}
	started := worker.config.Now().UTC()
	eligibleThrough := started.
		Add(-EligibilityDelay).
		Truncate(time.Microsecond)
	checkpointTime := mustParseCheckpointTime(checkpoint.LastStartTime)
	if eligibleThrough.Before(checkpointTime) {
		return PollReport{}, &WorkerError{Code: "eligibility_window_invalid"}
	}
	// A newly activated worker can land in the exact same microsecond as its
	// forward-only start-time eligibility fence. This is a valid empty interval, not a
	// permanent Source failure. Avoid both the Source request and state/item
	// commits; the next later poll will enter the normal path.
	if eligibleThrough.Equal(checkpointTime) {
		return PollReport{}, nil
	}
	if err := worker.state.MarkPollStarted(ctx, state.Cursor, started); err != nil {
		return PollReport{}, &WorkerError{Code: "state_poll_start_failed", Retryable: true, cause: err}
	}
	fail := func(input error) (PollReport, error) {
		classified := classifyWorkerError(input, "poll_failed")
		if ctx.Err() == nil {
			if markErr := worker.state.MarkFailure(
				ctx,
				state.Cursor,
				classified.Code,
				classified.Retryable,
				worker.config.Now().UTC(),
			); markErr != nil {
				return PollReport{}, &WorkerError{
					Code:      "state_failure_record_failed",
					Retryable: true,
					cause:     markErr,
				}
			}
		}
		return PollReport{}, classified
	}
	window, err := worker.source.FetchCompleteWindow(ctx, checkpoint, eligibleThrough)
	if err != nil {
		return fail(err)
	}
	selection, err := SelectWindow(checkpoint, window.Rows)
	if err != nil {
		return fail(&WorkerError{Code: "checkpoint_selection_failed", cause: err})
	}
	report := PollReport{
		WindowRows:         window.Total,
		Accepted:           len(selection.Accepted),
		CheckpointAdvanced: selection.Advances,
	}
	mapping := litellm.MappingContext{
		ProducerInstance: worker.config.SourceInstance,
		TenantRef:        worker.config.TenantRef,
		OrgRef:           worker.config.OrgRef,
		ProjectRef:       worker.config.ProjectRef,
	}
	for _, source := range selection.Accepted {
		item, connectorErr := worker.projector.Project(
			ctx,
			source,
			mapping,
			connector.ProjectionContext{
				IngestedAt: worker.config.Now().UTC(),
			},
		)
		if connectorErr != nil {
			return fail(connectorErr)
		}
		convergence, connectorErr := worker.committer.CommitItem(ctx, item)
		if connectorErr != nil {
			return fail(connectorErr)
		}
		switch convergence {
		case connector.ConvergenceInserted:
			report.Inserted++
		case connector.ConvergenceDuplicateNoop:
			report.DuplicateNoop++
		default:
			return fail(&WorkerError{Code: "item_convergence_invalid"})
		}
	}
	nextCursor, err := selection.Next.Encode()
	if err != nil {
		return fail(&WorkerError{Code: "checkpoint_encode_failed", cause: err})
	}
	if err := worker.state.CommitPage(ctx, PageCommit{
		ExpectedCursor: state.Cursor,
		NextCursor:     nextCursor,
		CompletedAt:    worker.config.Now().UTC(),
		ItemsReceived:  int64(report.Accepted),
		ItemsInserted:  int64(report.Inserted),
		DuplicateNoop:  int64(report.DuplicateNoop),
	}); err != nil {
		return fail(&WorkerError{
			Code:      "state_page_commit_failed",
			Retryable: true,
			cause:     err,
		})
	}
	return report, nil
}

func (worker *Worker) ensureActivation(ctx context.Context) (WorkerState, error) {
	state, err := worker.state.Load(ctx)
	if err == nil {
		if err := worker.validateStateBinding(state); err != nil {
			return WorkerState{}, err
		}
		return state, nil
	}
	if !errors.Is(err, ErrStateNotFound) {
		return WorkerState{}, &WorkerError{Code: "state_load_failed", Retryable: true, cause: err}
	}
	checkpoint := ActivationCheckpoint(worker.config.Now())
	state, err = worker.state.Activate(
		ctx,
		worker.config.SourceInstance,
		worker.scopeBinding(),
		checkpoint,
		worker.config.Now().UTC(),
	)
	if err != nil {
		return WorkerState{}, &WorkerError{Code: "state_activation_failed", Retryable: true, cause: err}
	}
	worker.config.Logger.Log("worker_activated", map[string]any{
		"connector_id": litellm.ConnectorID,
	})
	return state, nil
}

func (worker *Worker) scopeBinding() ScopeBinding {
	return ScopeBinding{
		TenantRef:  worker.config.TenantRef,
		OrgRef:     worker.config.OrgRef,
		ProjectRef: worker.config.ProjectRef,
	}
}

func (worker *Worker) validateStateBinding(state WorkerState) *WorkerError {
	if state.SourceInstance != worker.config.SourceInstance {
		return &WorkerError{Code: "source_instance_mismatch"}
	}
	expected := worker.scopeBinding()
	if state.OrgRef != expected.OrgRef ||
		state.ProjectRef != expected.ProjectRef ||
		state.TenantRefPresent != (expected.TenantRef != nil) ||
		(state.TenantRefPresent && state.TenantRef != *expected.TenantRef) {
		return &WorkerError{Code: "mapping_scope_mismatch"}
	}
	return nil
}

func (worker *Worker) stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	state, err := worker.state.Load(ctx)
	if err != nil {
		return
	}
	_ = worker.state.MarkInactive(ctx, state.Cursor, worker.config.Now().UTC())
}

func (worker *Worker) retryDelay(failures int) time.Duration {
	delay := worker.config.RetryBase
	for index := 1; index < failures && delay < worker.config.RetryMaximum; index++ {
		if delay > worker.config.RetryMaximum/2 {
			return worker.config.RetryMaximum
		}
		delay *= 2
	}
	if delay > worker.config.RetryMaximum {
		return worker.config.RetryMaximum
	}
	return delay
}

func classifyWorkerError(err error, fallback string) *WorkerError {
	var workerErr *WorkerError
	if errors.As(err, &workerErr) {
		return workerErr
	}
	var sourceErr *SourceError
	if errors.As(err, &sourceErr) {
		return &WorkerError{
			Code:      sourceErr.Code,
			Retryable: sourceErr.Retryable,
			cause:     sourceErr,
		}
	}
	var connectorErr *connector.ConnectorError
	if errors.As(err, &connectorErr) {
		return &WorkerError{
			Code:      connectorErr.Code,
			Retryable: connectorErr.Retryable,
			cause:     connectorErr,
		}
	}
	if fallback == "" {
		fallback = "unclassified_failure"
	}
	return &WorkerError{Code: fallback, Retryable: true, cause: err}
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

type EvidenceCommitter struct {
	store *db.EvidenceStore
}

func NewEvidenceCommitter(store *db.EvidenceStore) (*EvidenceCommitter, error) {
	if store == nil {
		return nil, errors.New("LiteLLM EvidenceStore is required")
	}
	return &EvidenceCommitter{store: store}, nil
}

func (committer *EvidenceCommitter) CommitItem(
	ctx context.Context,
	item connector.NormalizedItem,
) (connector.ItemConvergence, *connector.ConnectorError) {
	if committer == nil || committer.store == nil {
		return "", commitConnectorError("evidence_committer_unavailable", false)
	}
	if err := connector.ValidateNormalizedItem(item); err != nil {
		return "", commitConnectorError("normalized_item_invalid", false)
	}
	report, err := committer.store.PersistEventsWithProvenance(
		ctx,
		item.Events,
		item.Provenance,
	)
	if err != nil {
		return "", commitConnectorError("evidence_persistence_failed", true)
	}
	convergence, connectorErr := connector.ClassifyItemConvergence(
		len(item.Events),
		connector.EvidenceCommitReport{
			EventsTotal:         report.EventsTotal,
			Inserted:            report.Inserted,
			DuplicateNoop:       report.DuplicateNoop,
			IdempotencyConflict: report.IdempotencyConflict,
			Rejected:            report.Rejected,
		},
	)
	if connectorErr != nil {
		return "", connectorErr
	}
	return convergence, nil
}

func commitConnectorError(code string, retryable bool) *connector.ConnectorError {
	recovery := connector.RecoveryOperatorRepair
	if retryable {
		recovery = connector.RecoveryAutomaticRetry
	}
	return connector.NewConnectorError(
		connector.ErrorPersistence,
		code,
		connector.PhaseCommit,
		"commit_item",
		retryable,
		"LiteLLM Evidence commit failed",
		recovery,
	)
}

var _ connector.EvidenceCommitPort = (*EvidenceCommitter)(nil)
var _ ItemProjector = (*litellm.Projector)(nil)

func debugWorkerState(state WorkerState) string {
	return fmt.Sprintf("%s/%s", state.WorkerStatus, state.CursorKey)
}
