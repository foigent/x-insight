package pullfeed

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/investigation"
)

func TestModeAwareCommitterShadowNeverChangesLegacyAvailability(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	page, item := referenceTestPage(t, now)
	base := mustReferenceItem(t, page, item, now)

	testCases := []struct {
		name              string
		project           func() (connector.NormalizedItem, *connector.ConnectorError)
		wantMatched       bool
		wantMismatch      bool
		wantError         bool
		wantMismatchClass string
	}{
		{
			name:        "match",
			project:     func() (connector.NormalizedItem, *connector.ConnectorError) { return base, nil },
			wantMatched: true,
		},
		{
			name: "mismatch",
			project: func() (connector.NormalizedItem, *connector.ConnectorError) {
				mutated := base
				mutated.Events = append([]map[string]any(nil), base.Events...)
				mutated.Events[0] = cloneEventMap(base.Events[0])
				mutated.Events[0]["event_hash"] = "sha256:incompatible"
				return mutated, nil
			},
			wantMismatch:      true,
			wantMismatchClass: "event_identity",
		},
		{
			name: "neutral error",
			project: func() (connector.NormalizedItem, *connector.ConnectorError) {
				return connector.NormalizedItem{}, connector.NewConnectorError(
					connector.ErrorContract,
					"foigent.reference.fixture_error",
					connector.PhaseNormalize,
					"normalize_item",
					false,
					"fixture error",
					connector.RecoveryOperatorRepair,
				)
			},
			wantError: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &modeRecordingPersister{
				reports: []db.PersistenceReport{{EventsTotal: 5, Inserted: 5}},
			}
			projector := &recordingReferenceProjector{project: testCase.project}
			var logs []referenceTestLog
			committer, err := NewModeAwareEvidenceCommitter(
				store,
				ReferenceMappingShadow,
				projector,
				func() time.Time { return now },
				func(event string, fields map[string]any) {
					logs = append(logs, referenceTestLog{event: event, fields: fields})
				},
			)
			if err != nil {
				t.Fatalf("new shadow committer: %v", err)
			}
			state := activatedMemoryState(now)
			worker := mustLiveWorker(
				t,
				&liveSequenceSource{payloads: [][]byte{mustLivePage(t, page)}},
				state,
				committer,
				now,
			)
			report, err := worker.PollOnce(context.Background())
			if err != nil {
				t.Fatalf("shadow poll changed legacy availability: %v", err)
			}
			if store.calls != 1 ||
				state.commitCalls != 1 ||
				state.state.Cursor != page.NextCursor ||
				report.ItemsCommitted != 1 ||
				report.CompatibilityCompared != 1 ||
				report.CompatibilityMatched != boolInt(testCase.wantMatched) ||
				report.CompatibilityMismatch != boolInt(testCase.wantMismatch) ||
				report.CompatibilityError != boolInt(testCase.wantError) ||
				report.CompatibilityEligible != testCase.wantMatched {
				t.Fatalf(
					"store=%d state=%+v report=%+v",
					store.calls,
					state.state,
					report,
				)
			}
			assertReferenceTelemetry(
				t,
				logs,
				testCase.name,
				testCase.wantMismatchClass,
				testCase.wantMatched,
			)
		})
	}
}

func TestModeAwareCommitterNeutralCanaryIsSingleWriterAndFailsBeforeCheckpoint(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	page, item := referenceTestPage(t, now)
	base := mustReferenceItem(t, page, item, now)

	t.Run("match writes neutral once", func(t *testing.T) {
		store := &modeRecordingPersister{
			reports: []db.PersistenceReport{{EventsTotal: 5, DuplicateNoop: 5}},
		}
		committer, err := NewModeAwareEvidenceCommitter(
			store,
			ReferenceMappingNeutralCanary,
			&recordingReferenceProjector{
				project: func() (connector.NormalizedItem, *connector.ConnectorError) {
					return base, nil
				},
			},
			func() time.Time { return now },
			nil,
		)
		if err != nil {
			t.Fatalf("new canary committer: %v", err)
		}
		state := activatedMemoryState(now)
		worker := mustLiveWorker(
			t,
			&liveSequenceSource{payloads: [][]byte{mustLivePage(t, page)}},
			state,
			committer,
			now,
		)
		report, err := worker.PollOnce(context.Background())
		if err != nil {
			t.Fatalf("canary poll: %v", err)
		}
		if store.calls != 1 ||
			report.ItemsCommitted != 0 ||
			report.DuplicateNoop != 1 ||
			report.CompatibilityMatched != 1 ||
			!report.CompatibilityEligible ||
			state.commitCalls != 1 {
			t.Fatalf("store=%d report=%+v commits=%d", store.calls, report, state.commitCalls)
		}
		if !reflect.DeepEqual(store.events[0], base.Events) ||
			!reflect.DeepEqual(store.provenance[0], base.Provenance) {
			t.Fatal("canary did not write the neutral item")
		}
	})

	for _, testCase := range []struct {
		name     string
		project  func() (connector.NormalizedItem, *connector.ConnectorError)
		wantCode string
	}{
		{
			name: "mismatch",
			project: func() (connector.NormalizedItem, *connector.ConnectorError) {
				mutated := base
				mutated.Provenance.SourceRevision = "different"
				return mutated, nil
			},
			wantCode: "reference_projection_mismatch",
		},
		{
			name: "neutral error",
			project: func() (connector.NormalizedItem, *connector.ConnectorError) {
				return connector.NormalizedItem{}, connector.NewConnectorError(
					connector.ErrorContract,
					"foigent.reference.fixture_error",
					connector.PhaseNormalize,
					"normalize_item",
					false,
					"fixture error",
					connector.RecoveryOperatorRepair,
				)
			},
			wantCode: "reference_projection_invalid",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			store := &modeRecordingPersister{}
			committer, err := NewModeAwareEvidenceCommitter(
				store,
				ReferenceMappingNeutralCanary,
				&recordingReferenceProjector{project: testCase.project},
				func() time.Time { return now },
				nil,
			)
			if err != nil {
				t.Fatalf("new canary committer: %v", err)
			}
			state := activatedMemoryState(now)
			worker := mustLiveWorker(
				t,
				&liveSequenceSource{payloads: [][]byte{mustLivePage(t, page)}},
				state,
				committer,
				now,
			)
			_, err = worker.PollOnce(context.Background())
			var workerErr *LiveWorkerError
			if !errors.As(err, &workerErr) ||
				workerErr.Retryable ||
				workerErr.Code != testCase.wantCode {
				t.Fatalf("error=%v", err)
			}
			if store.calls != 0 ||
				state.commitCalls != 0 ||
				state.state.Cursor != "cursor-0" ||
				state.state.Status != WorkerStatusError {
				t.Fatalf("store=%d state=%+v commits=%d", store.calls, state.state, state.commitCalls)
			}
		})
	}
}

func TestModeAwareCommitterCapturesOneProjectionTimeAndKeepsLegacyDefault(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 123456000, time.UTC)
	page, item := referenceTestPage(t, now)

	legacyStore := &modeRecordingPersister{
		reports: []db.PersistenceReport{{EventsTotal: 5, Inserted: 5}},
	}
	legacy, err := NewModeAwareEvidenceCommitter(
		legacyStore,
		ReferenceMappingLegacy,
		nil,
		func() time.Time { return now },
		nil,
	)
	if err != nil {
		t.Fatalf("new legacy committer: %v", err)
	}
	if _, err := legacy.CommitLive(context.Background(), page, item); err != nil {
		t.Fatalf("legacy commit: %v", err)
	}

	var projected unirouter.CompletedExecutionMapOptions
	shadowStore := &modeRecordingPersister{
		reports: []db.PersistenceReport{{EventsTotal: 5, Inserted: 5}},
	}
	projector := &recordingReferenceProjector{
		projectWithOptions: func(options unirouter.CompletedExecutionMapOptions) (
			connector.NormalizedItem,
			*connector.ConnectorError,
		) {
			projected = options
			actual, err := unirouter.NewCompletedExecutionReferenceProjector()
			if err != nil {
				t.Fatalf("new actual projector: %v", err)
			}
			return actual.Project(context.Background(), item.Snapshot, options)
		},
	}
	shadow, err := NewModeAwareEvidenceCommitter(
		shadowStore,
		ReferenceMappingShadow,
		projector,
		func() time.Time { return now },
		nil,
	)
	if err != nil {
		t.Fatalf("new shadow committer: %v", err)
	}
	if _, err := shadow.CommitLive(context.Background(), page, item); err != nil {
		t.Fatalf("shadow commit: %v", err)
	}
	if !projected.ObservedAt.Equal(page.GeneratedAt) ||
		!projected.IngestedAt.Equal(now) ||
		!reflect.DeepEqual(legacyStore.events, shadowStore.events) ||
		!reflect.DeepEqual(legacyStore.provenance, shadowStore.provenance) {
		t.Fatal("legacy/shadow projection time or persistence drift")
	}
}

func TestModeAwareCommitterIsolatesImmutableItemBytesFromNeutralProjector(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	page, item := referenceTestPage(t, now)
	original := bytes.Clone(item.Snapshot)
	base := mustReferenceItem(t, page, item, now)
	store := &modeRecordingPersister{
		reports: []db.PersistenceReport{{EventsTotal: 5, Inserted: 5}},
	}
	committer, err := NewModeAwareEvidenceCommitter(
		store,
		ReferenceMappingShadow,
		&recordingReferenceProjector{
			projectWithInput: func(
				source []byte,
				_ unirouter.CompletedExecutionMapOptions,
			) (connector.NormalizedItem, *connector.ConnectorError) {
				for index := range source {
					source[index] = 'x'
				}
				return base, nil
			},
		},
		func() time.Time { return now },
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := committer.CommitLive(context.Background(), page, item)
	if err != nil || !result.CompatibilityMatched || store.calls != 1 {
		t.Fatalf("result=%+v store=%d err=%v", result, store.calls, err)
	}
	if !bytes.Equal(item.Snapshot, original) ||
		!bytes.Equal(page.Items[0].Snapshot, original) {
		t.Fatal("neutral projector mutated immutable Feed item bytes")
	}
	legacy, workerErr := prepareLegacyEvidenceItem(page, item, now)
	if workerErr != nil {
		t.Fatal(workerErr)
	}
	if !reflect.DeepEqual(store.events[0], legacy.events) ||
		!reflect.DeepEqual(store.provenance[0], legacy.provenance) {
		t.Fatal("shadow persistence drifted after hostile neutral projection")
	}
}

func TestModeAwareCommitterShadowTelemetryPanicCannotBlockLegacyConvergence(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	page, item := referenceTestPage(t, now)
	base := mustReferenceItem(t, page, item, now)
	store := &modeRecordingPersister{
		reports: []db.PersistenceReport{{EventsTotal: 5, Inserted: 5}},
	}
	committer, err := NewModeAwareEvidenceCommitter(
		store,
		ReferenceMappingShadow,
		&recordingReferenceProjector{
			project: func() (connector.NormalizedItem, *connector.ConnectorError) {
				mutated := cloneNormalizedItem(t, base)
				mutated.Events[0]["request_id"] = "different"
				return mutated, nil
			},
		},
		func() time.Time { return now },
		func(string, map[string]any) {
			panic("telemetry sink failed")
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	state := activatedMemoryState(now)
	worker := mustLiveWorker(
		t,
		&liveSequenceSource{payloads: [][]byte{mustLivePage(t, page)}},
		state,
		committer,
		now,
	)
	report, err := worker.PollOnce(context.Background())
	if err != nil ||
		store.calls != 1 ||
		state.commitCalls != 1 ||
		state.state.Cursor != page.NextCursor ||
		report.ItemsCommitted != 1 ||
		report.CompatibilityMismatch != 1 ||
		report.CompatibilityEligible {
		t.Fatalf("report=%+v store=%d state=%+v err=%v", report, store.calls, state.state, err)
	}
}

func TestModeAwareCommitterContainsNeutralProjectorPanicWithoutSensitiveOutput(t *testing.T) {
	const sensitivePanic = "request=req-secret source_hash=sha256:do-not-log"
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	page, _ := referenceTestPage(t, now)

	for _, mode := range []ReferenceMappingMode{
		ReferenceMappingShadow,
		ReferenceMappingNeutralCanary,
	} {
		t.Run(string(mode), func(t *testing.T) {
			store := &modeRecordingPersister{}
			if mode == ReferenceMappingShadow {
				store.reports = []db.PersistenceReport{{EventsTotal: 5, Inserted: 5}}
			}
			var logs []referenceTestLog
			committer, err := NewModeAwareEvidenceCommitter(
				store,
				mode,
				&recordingReferenceProjector{
					project: func() (connector.NormalizedItem, *connector.ConnectorError) {
						panic(sensitivePanic)
					},
				},
				func() time.Time { return now },
				func(event string, fields map[string]any) {
					logs = append(logs, referenceTestLog{event: event, fields: fields})
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			state := activatedMemoryState(now)
			worker := mustLiveWorker(
				t,
				&liveSequenceSource{payloads: [][]byte{mustLivePage(t, page)}},
				state,
				committer,
				now,
			)
			report, pollErr := worker.PollOnce(context.Background())
			if strings.Contains(fmt.Sprint(pollErr), sensitivePanic) ||
				strings.Contains(fmt.Sprint(logs), sensitivePanic) {
				t.Fatal("projector panic content escaped into observable output")
			}
			if len(logs) != 1 ||
				logs[0].fields["result"] != "neutral_error" ||
				logs[0].fields["promotion_eligible"] != false {
				t.Fatalf("logs=%+v", logs)
			}
			if mode == ReferenceMappingShadow {
				if pollErr != nil ||
					store.calls != 1 ||
					state.commitCalls != 1 ||
					report.ItemsCommitted != 1 ||
					report.CompatibilityError != 1 ||
					report.CompatibilityEligible {
					t.Fatalf(
						"report=%+v store=%d state=%+v err=%v",
						report,
						store.calls,
						state.state,
						pollErr,
					)
				}
				return
			}
			var workerErr *LiveWorkerError
			if !errors.As(pollErr, &workerErr) ||
				workerErr.Code != "reference_projection_invalid" ||
				workerErr.Retryable ||
				store.calls != 0 ||
				state.commitCalls != 0 {
				t.Fatalf(
					"store=%d state=%+v error=%v",
					store.calls,
					state.state,
					pollErr,
				)
			}
		})
	}
}

func TestReferenceProjectionComparatorClassifiesEveryFrozenSurface(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	page, item := referenceTestPage(t, now)
	legacy, workerErr := prepareLegacyEvidenceItem(page, item, now)
	if workerErr != nil {
		t.Fatalf("prepare legacy: %v", workerErr)
	}
	base := mustReferenceItem(t, page, item, now)

	tests := []struct {
		name string
		want string
		edit func(*connector.NormalizedItem)
	}{
		{
			name: "envelope identity",
			want: "envelope_identity",
			edit: func(item *connector.NormalizedItem) {
				item.Envelope.EventID = "different"
			},
		},
		{
			name: "event count",
			want: "event_count",
			edit: func(item *connector.NormalizedItem) {
				item.Events = item.Events[:len(item.Events)-1]
			},
		},
		{
			name: "event identity",
			want: "event_identity",
			edit: func(item *connector.NormalizedItem) {
				item.Events[0]["event_hash"] = "sha256:different"
			},
		},
		{
			name: "event shape",
			want: "event_shape",
			edit: func(item *connector.NormalizedItem) {
				item.Events[0]["severity"] = "critical"
			},
		},
		{
			name: "field provenance",
			want: "field_provenance",
			edit: func(item *connector.NormalizedItem) {
				item.Events[0]["field_provenance"] = map[string]any{"drift": "derived"}
			},
		},
		{
			name: "event payload",
			want: "event_payload",
			edit: func(item *connector.NormalizedItem) {
				item.Events[0]["request_id"] = "different"
			},
		},
		{
			name: "provenance",
			want: "provenance",
			edit: func(item *connector.NormalizedItem) {
				item.Provenance.SourceRevision = "different"
			},
		},
		{
			name: "gap order",
			want: "event_identity",
			edit: func(item *connector.NormalizedItem) {
				item.Events[1], item.Events[2] = item.Events[2], item.Events[1]
			},
		},
		{
			name: "gap linkage",
			want: "event_payload",
			edit: func(item *connector.NormalizedItem) {
				excerpt := item.Events[1]["raw_source_excerpt"].(map[string]any)
				excerpt["gap_source"] = "different"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			neutral := cloneNormalizedItem(t, base)
			test.edit(&neutral)
			if got := compareReferenceProjection(legacy, neutral); got != test.want {
				t.Fatalf("mismatch class=%q want=%q", got, test.want)
			}
		})
	}
	if got := compareReferenceProjection(legacy, base); got != "" {
		t.Fatalf("exact projection mismatch=%q", got)
	}
}

func TestModeAwareCommitterPromotionEligibilityIsProcessLocalLatch(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	page, item := referenceTestPage(t, now)
	base := mustReferenceItem(t, page, item, now)
	projectCalls := 0
	projector := &recordingReferenceProjector{
		project: func() (connector.NormalizedItem, *connector.ConnectorError) {
			projectCalls++
			if projectCalls == 1 {
				mutated := cloneNormalizedItem(t, base)
				mutated.Events[0]["request_id"] = "different"
				return mutated, nil
			}
			return base, nil
		},
	}
	store := &modeRecordingPersister{
		reports: []db.PersistenceReport{
			{EventsTotal: 5, Inserted: 5},
			{EventsTotal: 5, Inserted: 5},
			{EventsTotal: 5, Inserted: 5},
		},
	}
	committer, err := NewModeAwareEvidenceCommitter(
		store,
		ReferenceMappingShadow,
		projector,
		func() time.Time { return now },
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	first, err := committer.CommitLive(context.Background(), page, item)
	if err != nil || first.CompatibilityEligible || !first.CompatibilityMismatch {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := committer.CommitLive(context.Background(), page, item)
	if err != nil || second.CompatibilityEligible || !second.CompatibilityMatched {
		t.Fatalf("second=%+v err=%v", second, err)
	}

	restarted, err := NewModeAwareEvidenceCommitter(
		store,
		ReferenceMappingShadow,
		&recordingReferenceProjector{
			project: func() (connector.NormalizedItem, *connector.ConnectorError) {
				return base, nil
			},
		},
		func() time.Time { return now },
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	afterRestart, err := restarted.CommitLive(context.Background(), page, item)
	if err != nil || !afterRestart.CompatibilityEligible || !afterRestart.CompatibilityMatched {
		t.Fatalf("after restart=%+v err=%v", afterRestart, err)
	}
}

func TestModeAwareCommitterNeutralCanaryPreservesPersistenceFailureSemantics(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	page, item := referenceTestPage(t, now)
	base := mustReferenceItem(t, page, item, now)

	tests := []struct {
		name      string
		store     *modeRecordingPersister
		wantCode  string
		retryable bool
	}{
		{
			name: "retryable postgres outage",
			store: &modeRecordingPersister{
				err: &pgconn.PgError{Code: "08006"},
			},
			wantCode:  "evidence_database_unavailable",
			retryable: true,
		},
		{
			name: "idempotency conflict",
			store: &modeRecordingPersister{
				reports: []db.PersistenceReport{{
					EventsTotal: 5, IdempotencyConflict: 1,
				}},
			},
			wantCode: "evidence_conflict",
		},
		{
			name: "partial convergence",
			store: &modeRecordingPersister{
				reports: []db.PersistenceReport{{
					EventsTotal: 5, Inserted: 4, DuplicateNoop: 1,
				}},
			},
			wantCode: "evidence_partial_convergence",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			committer, err := NewModeAwareEvidenceCommitter(
				test.store,
				ReferenceMappingNeutralCanary,
				&recordingReferenceProjector{
					project: func() (connector.NormalizedItem, *connector.ConnectorError) {
						return base, nil
					},
				},
				func() time.Time { return now },
				nil,
			)
			if err != nil {
				t.Fatal(err)
			}
			state := activatedMemoryState(now)
			worker := mustLiveWorker(
				t,
				&liveSequenceSource{payloads: [][]byte{mustLivePage(t, page)}},
				state,
				committer,
				now,
			)
			_, err = worker.PollOnce(context.Background())
			var workerErr *LiveWorkerError
			if !errors.As(err, &workerErr) ||
				workerErr.Code != test.wantCode ||
				workerErr.Retryable != test.retryable {
				t.Fatalf("error=%v", err)
			}
			if state.commitCalls != 0 || state.state.Cursor != "cursor-0" {
				t.Fatalf("state=%+v commitCalls=%d", state.state, state.commitCalls)
			}
		})
	}
}

func TestModeAwareCommitterTwoItemPageModeSemantics(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	first := mustLiveFeedItemFromFixture(t, "valid-settled.json")
	second := mustLiveFeedItemFromFixture(t, "valid-cancelled.json")
	page := FeedPage{
		SchemaVersion:  SchemaVersion,
		SourceInstance: "unirouter-development",
		GeneratedAt:    now.Add(-time.Second),
		Items:          []FeedItem{first, second},
		NextCursor:     "cursor-two-item-runtime",
		HighWatermark:  "watermark-two-item-runtime",
	}
	payload := mustLivePage(t, page)

	t.Run("neutral canary partial page replays first item as duplicate", func(t *testing.T) {
		store := &modeRecordingPersister{
			reports: []db.PersistenceReport{
				{EventsTotal: 5, Inserted: 5},
				{EventsTotal: 5, DuplicateNoop: 5},
			},
		}
		projectCalls := 0
		projector := &recordingReferenceProjector{
			projectWithInput: func(
				source []byte,
				options unirouter.CompletedExecutionMapOptions,
			) (connector.NormalizedItem, *connector.ConnectorError) {
				projectCalls++
				actual, err := unirouter.NewCompletedExecutionReferenceProjector()
				if err != nil {
					t.Fatal(err)
				}
				normalized, connectorErr := actual.Project(context.Background(), source, options)
				if connectorErr != nil {
					return normalized, connectorErr
				}
				if projectCalls%2 == 0 {
					normalized = cloneNormalizedItem(t, normalized)
					normalized.Provenance.SourceRevision = "different"
				}
				return normalized, nil
			},
		}
		committer, err := NewModeAwareEvidenceCommitter(
			store,
			ReferenceMappingNeutralCanary,
			projector,
			func() time.Time { return now },
			nil,
		)
		if err != nil {
			t.Fatal(err)
		}
		state := activatedMemoryState(now)
		worker := mustLiveWorker(
			t,
			&liveSequenceSource{payloads: [][]byte{payload, payload}},
			state,
			committer,
			now,
		)
		for attempt := 0; attempt < 2; attempt++ {
			_, err := worker.PollOnce(context.Background())
			var workerErr *LiveWorkerError
			if !errors.As(err, &workerErr) ||
				workerErr.Code != "reference_projection_mismatch" ||
				workerErr.Retryable {
				t.Fatalf("attempt=%d error=%v", attempt, err)
			}
			if state.commitCalls != 0 || state.state.Cursor != "cursor-0" {
				t.Fatalf("attempt=%d state=%+v commits=%d", attempt, state.state, state.commitCalls)
			}
		}
		if store.calls != 2 ||
			len(store.reports) != 0 ||
			store.provenance[0].SourcePayloadSHA256 != first.SnapshotSHA256 ||
			store.provenance[1].SourcePayloadSHA256 != first.SnapshotSHA256 {
			t.Fatalf("store=%+v", store)
		}
	})

	t.Run("shadow mismatch keeps both legacy items and commits page", func(t *testing.T) {
		store := &modeRecordingPersister{
			reports: []db.PersistenceReport{
				{EventsTotal: 5, Inserted: 5},
				{EventsTotal: 5, Inserted: 5},
			},
		}
		projectCalls := 0
		projector := &recordingReferenceProjector{
			projectWithInput: func(
				source []byte,
				options unirouter.CompletedExecutionMapOptions,
			) (connector.NormalizedItem, *connector.ConnectorError) {
				projectCalls++
				actual, err := unirouter.NewCompletedExecutionReferenceProjector()
				if err != nil {
					t.Fatal(err)
				}
				normalized, connectorErr := actual.Project(context.Background(), source, options)
				if connectorErr == nil && projectCalls == 2 {
					normalized = cloneNormalizedItem(t, normalized)
					normalized.Provenance.SourceRevision = "different"
				}
				return normalized, connectorErr
			},
		}
		committer, err := NewModeAwareEvidenceCommitter(
			store,
			ReferenceMappingShadow,
			projector,
			func() time.Time { return now },
			nil,
		)
		if err != nil {
			t.Fatal(err)
		}
		state := activatedMemoryState(now)
		worker := mustLiveWorker(
			t,
			&liveSequenceSource{payloads: [][]byte{payload}},
			state,
			committer,
			now,
		)
		report, err := worker.PollOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if store.calls != 2 ||
			state.commitCalls != 1 ||
			state.state.Cursor != page.NextCursor ||
			report.ItemsCommitted != 2 ||
			report.CompatibilityMatched != 1 ||
			report.CompatibilityMismatch != 1 ||
			report.CompatibilityEligible {
			t.Fatalf("store=%d state=%+v report=%+v", store.calls, state.state, report)
		}
	})
}

func referenceTestPage(t *testing.T, now time.Time) (FeedPage, FeedItem) {
	t.Helper()
	item := mustLiveFeedItem(t)
	return FeedPage{
		SchemaVersion:  SchemaVersion,
		SourceInstance: "unirouter-development",
		GeneratedAt:    now.Add(-time.Second),
		Items:          []FeedItem{item},
		NextCursor:     "cursor-reference-runtime",
		HighWatermark:  "watermark-reference-runtime",
	}, item
}

func mustReferenceItem(
	t *testing.T,
	page FeedPage,
	item FeedItem,
	ingestedAt time.Time,
) connector.NormalizedItem {
	t.Helper()
	projector, err := unirouter.NewCompletedExecutionReferenceProjector()
	if err != nil {
		t.Fatalf("new Reference projector: %v", err)
	}
	normalized, connectorErr := projector.Project(
		context.Background(),
		item.Snapshot,
		unirouter.CompletedExecutionMapOptions{
			SourceInstance: page.SourceInstance,
			ObservedAt:     page.GeneratedAt,
			IngestedAt:     ingestedAt,
		},
	)
	if connectorErr != nil {
		t.Fatalf("project Reference item: %v", connectorErr)
	}
	return normalized
}

type recordingReferenceProjector struct {
	project            func() (connector.NormalizedItem, *connector.ConnectorError)
	projectWithOptions func(unirouter.CompletedExecutionMapOptions) (
		connector.NormalizedItem,
		*connector.ConnectorError,
	)
	projectWithInput func(
		[]byte,
		unirouter.CompletedExecutionMapOptions,
	) (connector.NormalizedItem, *connector.ConnectorError)
}

func (projector *recordingReferenceProjector) Project(
	_ context.Context,
	source []byte,
	options unirouter.CompletedExecutionMapOptions,
) (connector.NormalizedItem, *connector.ConnectorError) {
	if projector.projectWithInput != nil {
		return projector.projectWithInput(source, options)
	}
	if projector.projectWithOptions != nil {
		return projector.projectWithOptions(options)
	}
	return projector.project()
}

type modeRecordingPersister struct {
	reports    []db.PersistenceReport
	err        error
	calls      int
	events     [][]map[string]any
	provenance []investigation.ImportProvenance
}

func (store *modeRecordingPersister) PersistEventsWithProvenance(
	_ context.Context,
	events []map[string]any,
	provenance investigation.ImportProvenance,
) (db.PersistenceReport, error) {
	store.calls++
	store.events = append(store.events, events)
	store.provenance = append(store.provenance, provenance)
	if store.err != nil {
		return db.PersistenceReport{}, store.err
	}
	if len(store.reports) == 0 {
		return db.PersistenceReport{}, errors.New("unexpected persistence call")
	}
	report := store.reports[0]
	store.reports = store.reports[1:]
	return report, nil
}

type referenceTestLog struct {
	event  string
	fields map[string]any
}

func assertReferenceTelemetry(
	t *testing.T,
	logs []referenceTestLog,
	name string,
	mismatchClass string,
	promotionEligible bool,
) {
	t.Helper()
	if len(logs) != 1 {
		t.Fatalf("telemetry=%+v", logs)
	}
	allowed := map[string]struct{}{
		"mode":                    {},
		"profile":                 {},
		"protocol_version":        {},
		"protocol_release_digest": {},
		"result":                  {},
		"promotion_eligible":      {},
		"mismatch_class":          {},
	}
	for key := range logs[0].fields {
		if _, exists := allowed[key]; !exists {
			t.Fatalf("telemetry leaked per-item field %q", key)
		}
	}
	if logs[0].event != "reference_projection_comparison" ||
		logs[0].fields["mode"] != "shadow" ||
		logs[0].fields["profile"] != unirouter.CompletedExecutionReferenceProfileID ||
		logs[0].fields["protocol_release_digest"] !=
			unirouter.CompletedExecutionReferenceReleaseDigest ||
		logs[0].fields["promotion_eligible"] != promotionEligible {
		t.Fatalf("telemetry=%+v", logs)
	}
	wantResult := map[string]string{
		"match":         "match",
		"mismatch":      "projection_mismatch",
		"neutral error": "neutral_error",
	}[name]
	if logs[0].fields["result"] != wantResult {
		t.Fatalf("result=%v want=%s", logs[0].fields["result"], wantResult)
	}
	if mismatchClass == "" {
		if _, exists := logs[0].fields["mismatch_class"]; exists {
			t.Fatal("telemetry included an unexpected mismatch class")
		}
		return
	}
	if logs[0].fields["mismatch_class"] != mismatchClass {
		t.Fatalf("mismatch_class=%v want=%s", logs[0].fields["mismatch_class"], mismatchClass)
	}
}

func cloneEventMap(source map[string]any) map[string]any {
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func cloneNormalizedItem(
	t *testing.T,
	source connector.NormalizedItem,
) connector.NormalizedItem {
	t.Helper()
	events := make([]map[string]any, len(source.Events))
	for index, event := range source.Events {
		events[index] = cloneReferenceValue(event).(map[string]any)
	}
	cloned := source
	cloned.Events = events
	return cloned
}

func cloneReferenceValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, nested := range typed {
			cloned[key] = cloneReferenceValue(nested)
		}
		return cloned
	case []any:
		cloned := make([]any, len(typed))
		for index, nested := range typed {
			cloned[index] = cloneReferenceValue(nested)
		}
		return cloned
	case []string:
		return append([]string(nil), typed...)
	default:
		return typed
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
