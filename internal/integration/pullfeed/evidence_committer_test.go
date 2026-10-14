package pullfeed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/investigation"
)

func TestEvidenceCommitterPersistsAcceptedItemAndClassifiesReplay(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	item := mustLiveFeedItem(t)
	page := FeedPage{
		SchemaVersion:  SchemaVersion,
		SourceInstance: "unirouter-development",
		GeneratedAt:    now.Add(-time.Hour),
		Items:          []FeedItem{item},
		NextCursor:     "cursor-1",
		HighWatermark:  "watermark-1",
	}
	persister := &recordingEvidencePersister{reports: []db.PersistenceReport{
		{EventsTotal: 5, Inserted: 5},
		{EventsTotal: 5, DuplicateNoop: 5},
	}}
	committer, err := NewEvidenceCommitter(persister, func() time.Time { return now })
	if err != nil {
		t.Fatalf("new committer: %v", err)
	}

	first, err := committer.CommitLive(context.Background(), page, item)
	if err != nil || first.DuplicateNoop {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replayPage := page
	replayPage.HighWatermark = "watermark-newer"
	second, err := committer.CommitLive(context.Background(), replayPage, item)
	if err != nil || !second.DuplicateNoop {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if len(persister.provenance) != 2 {
		t.Fatalf("provenance calls=%d", len(persister.provenance))
	}
	if !reflect.DeepEqual(persister.provenance[0], persister.provenance[1]) {
		t.Fatalf(
			"identical item replay changed provenance across page high-watermarks: first=%+v second=%+v",
			persister.provenance[0],
			persister.provenance[1],
		)
	}
	for _, provenance := range persister.provenance {
		if provenance.IntegrationMode != investigation.IntegrationPull ||
			provenance.AssuranceMode != investigation.AssuranceContractedPull ||
			provenance.SourcePayloadSHA256 != item.SnapshotSHA256 ||
			provenance.SourceWatermark != "" ||
			provenance.ImportWatermark != "" ||
			provenance.SourceArtifactSHA256 != "" ||
			provenance.ApprovalManifestSHA256 != "" ||
			provenance.ExchangeID != "" {
			t.Fatalf("provenance=%+v", provenance)
		}
	}
}

func TestEvidenceCommitterBindsTerminalTimeAtPostgresPrecision(t *testing.T) {
	innerText := "2026-07-25T04:05:06.123456789Z"
	inner, err := time.Parse(time.RFC3339Nano, innerText)
	if err != nil {
		t.Fatalf("parse inner terminal time: %v", err)
	}
	timezoneEquivalent := inner.In(time.FixedZone("UTC+08", 8*60*60))

	tests := []struct {
		name       string
		outer      time.Time
		wantCode   string
		wantCommit bool
	}{
		{
			name:       "exact equality",
			outer:      inner,
			wantCommit: true,
		},
		{
			name:       "historical sub-microsecond residual",
			outer:      inner.Truncate(time.Microsecond),
			wantCommit: true,
		},
		{
			name:     "one microsecond mismatch",
			outer:    inner.Add(time.Microsecond),
			wantCode: "terminal_time_mismatch",
		},
		{
			name:       "timezone-equivalent instant",
			outer:      timezoneEquivalent,
			wantCommit: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			item := mustLiveFeedItemWithInnerTerminal(t, innerText)
			item.TerminalAvailableAt = test.outer
			page := FeedPage{
				SourceInstance: "unirouter-development",
				GeneratedAt:    inner.Add(time.Minute),
				NextCursor:     "cursor-1",
				HighWatermark:  "watermark-1",
			}
			persister := &recordingEvidencePersister{}
			if test.wantCommit {
				persister.reports = []db.PersistenceReport{{EventsTotal: 5, Inserted: 5}}
			}
			committer, err := NewEvidenceCommitter(persister, func() time.Time {
				return inner.Add(2 * time.Minute)
			})
			if err != nil {
				t.Fatalf("new committer: %v", err)
			}

			_, err = committer.CommitLive(context.Background(), page, item)
			if test.wantCode == "" {
				if err != nil {
					t.Fatalf("commit: %v", err)
				}
				if len(persister.provenance) != 1 {
					t.Fatalf("persistence calls=%d", len(persister.provenance))
				}
				return
			}
			var workerErr *LiveWorkerError
			if !errors.As(err, &workerErr) ||
				workerErr.Retryable ||
				workerErr.Code != test.wantCode {
				t.Fatalf("error=%v", err)
			}
			if len(persister.provenance) != 0 {
				t.Fatal("terminal mismatch reached persistence")
			}
		})
	}
}

func TestLiveWorkerTerminalPrecisionPreservesHashAndCursorSemantics(t *testing.T) {
	innerText := "2026-07-25T04:05:06.123456789Z"
	inner, err := time.Parse(time.RFC3339Nano, innerText)
	if err != nil {
		t.Fatalf("parse inner terminal time: %v", err)
	}
	item := mustLiveFeedItemWithInnerTerminal(t, innerText)
	item.TerminalAvailableAt = inner.Truncate(time.Microsecond)
	originalHash := item.SnapshotSHA256
	page := FeedPage{
		SchemaVersion:  SchemaVersion,
		SourceInstance: "unirouter-development",
		GeneratedAt:    inner.Add(time.Minute),
		Items:          []FeedItem{item},
		NextCursor:     "cursor-after-compatible-item",
		HighWatermark:  "watermark-after-compatible-item",
	}
	persister := &recordingEvidencePersister{
		reports: []db.PersistenceReport{{EventsTotal: 5, Inserted: 5}},
	}
	committer, err := NewEvidenceCommitter(persister, func() time.Time {
		return inner.Add(2 * time.Minute)
	})
	if err != nil {
		t.Fatalf("new committer: %v", err)
	}
	state := activatedMemoryState(inner.Add(3 * time.Minute))
	worker := mustLiveWorker(
		t,
		&liveSequenceSource{payloads: [][]byte{mustLivePage(t, page)}},
		state,
		committer,
		inner.Add(3*time.Minute),
	)

	report, err := worker.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("poll compatible historical item: %v", err)
	}
	if report.ItemsReceived != 1 ||
		report.ItemsCommitted != 1 ||
		report.DuplicateNoop != 0 ||
		state.state.Cursor != page.NextCursor ||
		state.state.HighWatermark != page.HighWatermark ||
		state.commitCalls != 1 {
		t.Fatalf("report=%+v state=%+v commits=%d", report, state.state, state.commitCalls)
	}
	if len(persister.provenance) != 1 ||
		persister.provenance[0].SourcePayloadSHA256 != originalHash ||
		item.SnapshotSHA256 != originalHash {
		t.Fatalf("snapshot identity changed: provenance=%+v item=%s", persister.provenance, item.SnapshotSHA256)
	}
}

func TestLiveWorkerPartialPageFailureDoesNotOverclaimCursorInProvenance(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	first := mustLiveFeedItemFromFixture(t, "valid-settled.json")
	second := mustLiveFeedItemFromFixture(t, "valid-cancelled.json")
	page := FeedPage{
		SchemaVersion:  SchemaVersion,
		SourceInstance: "unirouter-development",
		GeneratedAt:    now,
		Items:          []FeedItem{first, second},
		NextCursor:     "cursor-after-page",
		HighWatermark:  "watermark-after-page",
	}
	persister := &recordingEvidencePersister{
		reports: []db.PersistenceReport{{EventsTotal: 5, Inserted: 5}},
		errs:    []error{nil, errors.New("second item persistence failed")},
	}
	committer, err := NewEvidenceCommitter(persister, func() time.Time { return now })
	if err != nil {
		t.Fatalf("new committer: %v", err)
	}
	state := activatedMemoryState(now)
	worker := mustLiveWorker(
		t,
		&liveSequenceSource{payloads: [][]byte{mustLivePage(t, page)}},
		state,
		committer,
		now,
	)

	if _, err := worker.PollOnce(context.Background()); err == nil {
		t.Fatal("expected partial-page persistence failure")
	}
	if state.state.Cursor != "cursor-0" || state.commitCalls != 0 {
		t.Fatalf("cursor advanced after partial page: state=%+v commits=%d", state.state, state.commitCalls)
	}
	if len(persister.provenance) != 2 {
		t.Fatalf("provenance attempts=%d", len(persister.provenance))
	}
	for _, provenance := range persister.provenance {
		if provenance.SourceWatermark != "" || provenance.ImportWatermark != "" {
			t.Fatalf("per-item provenance overclaimed cursor: %+v", provenance)
		}
	}
}

func TestEvidenceCommitterFailsClosedOnOuterInnerConflict(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	item := mustLiveFeedItem(t)
	item.RequestID = "different-request"
	persister := &recordingEvidencePersister{}
	committer, err := NewEvidenceCommitter(persister, func() time.Time { return now })
	if err != nil {
		t.Fatalf("new committer: %v", err)
	}

	_, err = committer.CommitLive(context.Background(), FeedPage{
		SourceInstance: "unirouter-development",
		GeneratedAt:    now,
		NextCursor:     "cursor-1",
		HighWatermark:  "watermark-1",
	}, item)
	var workerErr *LiveWorkerError
	if !errors.As(err, &workerErr) || workerErr.Retryable || workerErr.Code != "request_identity_mismatch" {
		t.Fatalf("error=%v", err)
	}
	if len(persister.provenance) != 0 {
		t.Fatal("outer/inner conflict reached persistence")
	}
}

func TestEvidenceCommitterFailsClosedOnPersistenceConflictOrPartialMix(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	item := mustLiveFeedItem(t)
	page := FeedPage{
		SourceInstance: "unirouter-development",
		GeneratedAt:    now,
		NextCursor:     "cursor-1",
		HighWatermark:  "watermark-1",
	}
	for name, report := range map[string]db.PersistenceReport{
		"conflict": {
			EventsTotal:         5,
			DuplicateNoop:       4,
			IdempotencyConflict: 1,
		},
		"partial mix": {
			EventsTotal:   5,
			Inserted:      1,
			DuplicateNoop: 4,
		},
	} {
		t.Run(name, func(t *testing.T) {
			persister := &recordingEvidencePersister{reports: []db.PersistenceReport{report}}
			committer, err := NewEvidenceCommitter(persister, func() time.Time { return now })
			if err != nil {
				t.Fatalf("new committer: %v", err)
			}
			_, err = committer.CommitLive(context.Background(), page, item)
			var workerErr *LiveWorkerError
			if !errors.As(err, &workerErr) || workerErr.Retryable {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func mustLiveFeedItemWithInnerTerminal(t *testing.T, availableAt string) FeedItem {
	t.Helper()
	item := mustLiveFeedItem(t)
	var snapshot map[string]any
	if err := json.Unmarshal(item.Snapshot, &snapshot); err != nil {
		t.Fatalf("decode live snapshot: %v", err)
	}
	terminal, ok := snapshot["terminal"].(map[string]any)
	if !ok {
		t.Fatal("live snapshot terminal is not an object")
	}
	terminal["available_at"] = availableAt
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal live snapshot: %v", err)
	}
	digest := sha256.Sum256(payload)
	item.Snapshot = payload
	item.SnapshotSHA256 = "sha256:" + hex.EncodeToString(digest[:])
	return item
}

type recordingEvidencePersister struct {
	reports    []db.PersistenceReport
	errs       []error
	err        error
	provenance []investigation.ImportProvenance
}

func (p *recordingEvidencePersister) PersistEventsWithProvenance(
	_ context.Context,
	_ []map[string]any,
	provenance investigation.ImportProvenance,
) (db.PersistenceReport, error) {
	p.provenance = append(p.provenance, provenance)
	if len(p.errs) > 0 {
		err := p.errs[0]
		p.errs = p.errs[1:]
		if err != nil {
			return db.PersistenceReport{}, err
		}
	}
	if p.err != nil {
		return db.PersistenceReport{}, p.err
	}
	if len(p.reports) == 0 {
		return db.PersistenceReport{}, errors.New("unexpected persistence call")
	}
	report := p.reports[0]
	p.reports = p.reports[1:]
	return report, nil
}
