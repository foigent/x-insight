package sourcefacts

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connector"
)

type currentMemory struct {
	consumerMemory
	position           CurrentRuntimeCursor
	currentCommits     int
	currentCodes       []string
	currentCommitError error
}

func TestManagedCurrentInventoryContinuesAcrossHistoricalGap(t *testing.T) {
	raw, err := os.ReadFile("contract/observations/golden-runtime-current.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		Valid []struct{ Document json.RawMessage }
	}
	if err = json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	var page CurrentRuntimePage
	if err = json.Unmarshal(vectors.Valid[0].Document, &page); err != nil {
		t.Fatal(err)
	}
	page.SourceSHA256 = ""
	page.HasMore = true
	page.NextCursor = "inventory-page-2"
	raw, _ = json.Marshal(page)
	m, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	unsigned, err := connector.CanonicalizeJSONValue(m)
	if err != nil {
		t.Fatal(err)
	}
	page.SourceSHA256 = Hash(unsigned)
	raw, _ = json.Marshal(page)
	cap := observationCapabilitiesFor(RuntimeProfile)
	cap.SourceInstance = "synthetic-ur"
	caps, _ := json.Marshal(cap)
	store := &currentMemory{consumerMemory: consumerMemory{stream: Stream{ID: "11111111-1111-4111-8111-111111111111", SourceSystemID: "22222222-2222-4222-8222-222222222222", SourceInstance: "synthetic-ur", OrgID: "1", ProjectID: "2", Profile: RuntimeProfile, ScopeGeneration: "1", Status: "active", Cursor: "historical-cursor", RequiresAttention: true, LastError: "cursor_expired"}}, position: CurrentRuntimeCursor{Generation: 1, PageOrdinal: 1}}
	reader := &currentReaderMemory{readerMemory: readerMemory{cap: caps}, current: raw}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	waits := 0
	w := ManagedWorker{ConfigVersion: ManagedObservationConfigVersion, Profiles: []ManagedProfile{{StreamID: store.stream.ID, Consumer: Consumer{Store: store, Reader: reader}, CollectCurrentRuntime: true}}, PollInterval: time.Second, RetryMaximum: 8 * time.Second, Wait: func(context.Context, time.Duration) error { waits++; cancel(); return ctx.Err() }, Observe: func(string, error) {
		if store.currentCommits == 2 {
			cancel()
		}
	}}
	if err = w.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if store.currentCommits != 2 || store.commits != 0 || waits != 0 {
		t.Fatal("historical gap throttled or blocked current pagination", store.currentCommits, store.commits, waits)
	}
}

func (s *currentMemory) LoadCurrentRuntimeCursor(context.Context, Stream) (CurrentRuntimeCursor, error) {
	return s.position, nil
}
func (s *currentMemory) CommitCurrentRuntimePage(context.Context, Stream, CurrentRuntimeCursor, []byte, time.Time) error {
	s.currentCommits++
	return s.currentCommitError
}
func (s *currentMemory) RecordCurrentRuntimeFailure(_ context.Context, _ Stream, _ CurrentRuntimeCursor, code string, _ time.Time) error {
	s.currentCodes = append(s.currentCodes, code)
	return nil
}

type currentReaderMemory struct {
	readerMemory
	current      []byte
	currentError error
	currentCalls int
	cursor       string
}

func (r *currentReaderMemory) FetchCurrentRuntime(_ context.Context, cursor string, limit int) ([]byte, error) {
	r.currentCalls++
	r.cursor = cursor
	if limit != 50 {
		return nil, ErrInvalid
	}
	return r.current, r.currentError
}

func TestCurrentRuntimeConsumerUsesSeparatePositionAndCurrentScope(t *testing.T) {
	wire, err := os.ReadFile("contract/observations/golden-runtime-current.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		Valid []struct{ Document json.RawMessage }
	}
	if err = json.Unmarshal(wire, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name             string
		change           func(*currentMemory, *currentReaderMemory)
		commits, fetches int
		code             string
	}{
		{"valid", func(_ *currentMemory, _ *currentReaderMemory) {}, 1, 1, ""},
		{"historical cursor expired", func(s *currentMemory, _ *currentReaderMemory) {
			s.stream.RequiresAttention = true
			s.stream.LastError = "cursor_expired"
		}, 1, 1, ""},
		{"shared trust failure", func(s *currentMemory, _ *currentReaderMemory) {
			s.stream.RequiresAttention = true
			s.stream.LastError = "source_fact_conflict"
		}, 0, 0, ""},
		{"paused registration", func(s *currentMemory, _ *currentReaderMemory) { s.stream.Status = "paused" }, 0, 0, ""},
		{"scope changed", func(_ *currentMemory, r *currentReaderMemory) {
			c := observationCapabilitiesFor(RuntimeProfile)
			c.SourceInstance = "other"
			r.cap, _ = json.Marshal(c)
		}, 0, 0, "scope_changed"},
		{"unavailable", func(_ *currentMemory, r *currentReaderMemory) { r.currentError = errors.New("synthetic disconnected") }, 0, 1, "source_unavailable"},
		{"bad hash", func(_ *currentMemory, r *currentReaderMemory) {
			var p map[string]any
			json.Unmarshal(r.current, &p)
			p["observed_at"] = "2026-09-13T01:00:00.000000Z"
			r.current, _ = json.Marshal(p)
		}, 0, 1, "invalid_page"},
		{"repeated cursor or bound exceeded", func(s *currentMemory, _ *currentReaderMemory) { s.currentCommitError = ErrInvalid }, 1, 1, "invalid_page"},
		{"concurrent management", func(s *currentMemory, _ *currentReaderMemory) { s.currentCommitError = ErrStreamChanged }, 1, 1, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cap := observationCapabilitiesFor(RuntimeProfile)
			cap.SourceInstance = "synthetic-ur"
			caps, _ := json.Marshal(cap)
			store := &currentMemory{consumerMemory: consumerMemory{stream: Stream{ID: "11111111-1111-4111-8111-111111111111", SourceSystemID: "22222222-2222-4222-8222-222222222222", SourceInstance: "synthetic-ur", OrgID: "1", ProjectID: "2", Profile: RuntimeProfile, ScopeGeneration: "1", Status: "active", Cursor: "historical-cursor", HighWatermark: "historical-watermark"}}, position: CurrentRuntimeCursor{Generation: 2, PageOrdinal: 2, Revision: 1, Cursor: "inventory-only-cursor"}}
			reader := &currentReaderMemory{readerMemory: readerMemory{cap: caps}, current: append([]byte{}, vectors.Valid[0].Document...)}
			tc.change(store, reader)
			before := store.stream
			_, err := (Consumer{Store: store, Reader: reader}).PullCurrentRuntimeOnce(t.Context(), store.stream.ID)
			valid := tc.name == "valid" || tc.name == "historical cursor expired"
			if valid && err != nil {
				t.Fatal(err)
			}
			if !valid && err == nil {
				t.Fatal("failed input accepted")
			}
			if store.currentCommits != tc.commits || reader.currentCalls != tc.fetches || store.commits != 0 || store.stream != before {
				t.Fatal("inventory affected event path")
			}
			if reader.currentCalls > 0 && reader.cursor != store.position.Cursor {
				t.Fatal("event cursor used for inventory")
			}
			if tc.code != "" && (len(store.currentCodes) != 1 || store.currentCodes[0] != tc.code) {
				t.Fatal(store.currentCodes)
			}
			if tc.code == "" && len(store.currentCodes) != 0 {
				t.Fatal("unexpected failure mutation", store.currentCodes)
			}
		})
	}
}
