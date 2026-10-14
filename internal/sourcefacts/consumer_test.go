package sourcefacts

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type consumerMemory struct {
	stream      Stream
	codes       []string
	commits     int
	fail        bool
	commitError error
}

func (s *consumerMemory) LoadSourceFactsStream(context.Context, string) (Stream, error) {
	return s.stream, nil
}
func (s *consumerMemory) RecordSourceFactsFailure(_ context.Context, _ Stream, code string, _ time.Time) error {
	s.codes = append(s.codes, code)
	if s.fail {
		return ErrConflict
	}
	return nil
}
func (s *consumerMemory) CommitSourceFactsPage(context.Context, Stream, Page, time.Time) error {
	s.commits++
	return s.commitError
}

type readerMemory struct {
	cap, page           []byte
	capError, pageError error
	calls               int
}

func (r *readerMemory) FetchSourceCapabilities(context.Context) ([]byte, error) {
	r.calls++
	return r.cap, r.capError
}
func (r *readerMemory) FetchSourceFacts(context.Context, string, string, int) ([]byte, error) {
	r.calls++
	return r.page, r.pageError
}

type httpFailure struct {
	status int
	code   string
}

func (h httpFailure) Error() string           { return "synthetic HTTP failure" }
func (h httpFailure) HTTPStatusCode() int     { return h.status }
func (h httpFailure) SourceErrorCode() string { return h.code }
func capabilities() Capabilities {
	return Capabilities{SchemaVersion: "unirouter.source-capabilities.v1", SourceInstance: "ur", Profiles: []string{ContextProfile}, ProfileVersion: "1.0.0", Status: "active", CredentialStatus: "feed_active", ScopeGeneration: "1", RetentionSeconds: 604800, Bootstrap: "forward_only", FieldAuthorization: "fixed_profiles"}
}
func TestConsumerStopsWithoutReset(t *testing.T) {
	for _, tt := range []struct {
		name, code string
		change     func(*Capabilities)
		failure    error
	}{
		{"epoch", "cursor_or_epoch_conflict", nil, httpFailure{400, "invalid_cursor"}},
		{"ahead", "cursor_or_epoch_conflict", nil, httpFailure{409, "cursor_ahead"}},
		{"expired", "cursor_expired", nil, httpFailure{410, "cursor_expired"}},
		{"revoked", "source_authorization_denied", nil, httpFailure{401, "invalid_token"}},
		{"denied", "source_authorization_denied", nil, httpFailure{403, "forbidden"}},
		{"transient", "source_unavailable", nil, errors.New("offline")},
		{"source pause", "source_paused", nil, httpFailure{503, "source_paused"}},
		{"pause", "source_paused", func(c *Capabilities) { c.Status = "paused" }, nil},
		{"staged", "source_paused", func(c *Capabilities) { c.CredentialStatus = "feed_staged" }, nil},
		{"generation", "scope_changed", func(c *Capabilities) { c.ScopeGeneration = "2" }, nil},
		{"producer", "scope_changed", func(c *Capabilities) { c.SourceInstance = "other" }, nil},
		{"profile", "scope_changed", func(c *Capabilities) { c.Profiles = []string{} }, nil},
		{"invalid capabilities", "invalid_capabilities", func(c *Capabilities) { c.TimelineComplete = true }, nil},
		{"invalid page", "invalid_page", nil, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cap := capabilities()
			if tt.change != nil {
				tt.change(&cap)
			}
			raw, _ := json.Marshal(cap)
			store := &consumerMemory{stream: Stream{ID: "s", SourceInstance: "ur", ScopeGeneration: "1", Profile: ContextProfile, Status: "active", Cursor: "keep"}}
			reader := &readerMemory{cap: raw, page: []byte("{}"), pageError: tt.failure}
			if _, err := (Consumer{Store: store, Reader: reader}).PullOnce(t.Context(), "s"); err == nil {
				t.Fatal("accepted failure")
			}
			if store.commits != 0 || store.stream.Cursor != "keep" || len(store.codes) != 1 || store.codes[0] != tt.code {
				t.Fatalf("failure state: %+v", store)
			}
		})
	}
}
func TestCapabilitiesStrictAndConsumerBootstrap(t *testing.T) {
	cap := capabilities()
	raw, _ := json.Marshal(cap)
	for _, bad := range [][]byte{[]byte(`{"schema_version":"a","schema_version":"b"}`), []byte(`null`), append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"extra":true}`)...)} {
		if _, e := DecodeCapabilities(bad); e == nil {
			t.Fatal("invalid capabilities")
		}
	}
	for _, change := range []func(*Capabilities){func(c *Capabilities) { c.Profiles = append(c.Profiles, ContextProfile) }, func(c *Capabilities) { c.Profiles = []string{"other"} }, func(c *Capabilities) { c.ScopeGeneration = "01" }, func(c *Capabilities) { c.SourceInstance = "\x01" }} {
		c := cap
		change(&c)
		v, _ := json.Marshal(c)
		if _, e := DecodeCapabilities(v); e == nil {
			t.Fatal("invalid value")
		}
	}
	store := &consumerMemory{stream: Stream{ID: "s", SourceInstance: "ur", ScopeGeneration: "1", Profile: ContextProfile, Status: "active"}}
	page, _ := json.Marshal(Page{BatchVersion: "foigent.event-batch.v1", ConnectorID: ConnectorID, ConnectorVersion: ConnectorVersion, ProducerInstance: "ur", GeneratedAt: "2026-09-10T00:00:00.000000Z", Events: []json.RawMessage{}, NextCursor: "bootstrap", HighWatermark: "bootstrap"})
	reader := &readerMemory{cap: raw, page: page}
	if _, e := (Consumer{Store: store, Reader: reader, Now: func() time.Time { return time.Now() }}).PullOnce(t.Context(), "s"); e != nil || store.commits != 1 {
		t.Fatal("bootstrap", e)
	}
	for _, failure := range []error{ErrConflict, ErrStreamChanged, errors.New("database unavailable")} {
		store.commitError, store.codes = failure, nil
		if _, e := (Consumer{Store: store, Reader: reader}).PullOnce(t.Context(), "s"); !errors.Is(e, failure) {
			t.Fatal("lost commit failure", e)
		}
		if failure == ErrConflict {
			if len(store.codes) != 1 || store.codes[0] != "source_fact_conflict" {
				t.Fatal("source conflict not persisted")
			}
		} else if len(store.codes) != 0 {
			t.Fatal("local concurrency/environment error became source conflict")
		}
	}
	store.commitError = nil
	store.stream.Status = "revoked"
	calls := reader.calls
	if _, e := (Consumer{Store: store, Reader: reader}).PullOnce(t.Context(), "s"); !errors.Is(e, ErrUnavailable) || reader.calls != calls {
		t.Fatal("local revoke performed HTTP")
	}
	if _, e := (Consumer{}).PullOnce(t.Context(), "s"); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	store.stream.Status = "active"
	store.fail = true
	reader.capError = errors.New("offline")
	if _, e := (Consumer{Store: store, Reader: reader}).PullOnce(t.Context(), "s"); !errors.Is(e, ErrConflict) {
		t.Fatal("lost failure recording conflict")
	}
}
