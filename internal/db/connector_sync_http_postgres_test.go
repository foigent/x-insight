package db_test

import (
	"context"
	"errors"
	"github.com/foigent/x-insight/internal/connectorsync"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"testing"
)

type syncResponseHold struct {
	sourcefacts.HTTPReader
	received chan struct{}
	release  chan struct{}
}

func (h syncResponseHold) FetchSourceFacts(ctx context.Context, profile, cursor string, limit int) ([]byte, error) {
	raw, e := h.HTTPReader.FetchSourceFacts(ctx, profile, cursor, limit)
	if e == nil {
		close(h.received)
		select {
		case <-h.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return raw, e
}
func syncMutation(id string, rev int64, n string, action string) connectorsync.Request {
	return connectorsync.Request{SchemaVersion: connectorsync.LocalSchema, InstallationID: id, ExpectedRevision: rev, RequestID: "46464646-4646-4646-8646-" + n, Action: action, Actor: "xs_4646464646464646"}
}

func TestConnectionSyncRealHTTPManualOnceAndPauseAfterResponse(t *testing.T) {
	f := newSourceFixture(t)
	gate := f.workspace.SyncStore()
	id, e := gate.Resolve(f.ctx, "ur-demo-01", "17", "101", false)
	if e != nil {
		t.Fatal(e)
	}
	status, e := gate.Change(f.ctx, syncMutation(id, 1, "000000000001", "set"))
	if e != nil {
		t.Fatal(e)
	}
	calls := func() int { f.mu.Lock(); defer f.mu.Unlock(); return f.httpCalls }
	before := calls()
	for i := 0; i < 2; i++ {
		if _, e = f.consumer.PullOnce(f.ctx, f.streams[sourcefacts.ContextProfile]); !errors.Is(e, connectorsync.ErrPaused) {
			t.Fatal("manual read escaped gate", e)
		}
	}
	if calls() != before {
		t.Fatal("manual emitted actual HTTP")
	}
	status, e = gate.Change(f.ctx, syncMutation(id, status.Revision, "000000000002", "once"))
	if e != nil {
		t.Fatal(e)
	}
	f.setPage(sourcefacts.ContextProfile, "sync-once-cursor", f.documents[sourcefacts.ContextProfile])
	if _, e = f.consumer.PullOnce(f.ctx, f.streams[sourcefacts.ContextProfile]); e != nil {
		t.Fatal(e)
	}
	if calls() != before+2 {
		t.Fatalf("once should make capability + page requests, got %d", calls()-before)
	}
	if _, e = f.consumer.PullOnce(f.ctx, f.streams[sourcefacts.ContextProfile]); !errors.Is(e, connectorsync.ErrPaused) {
		t.Fatal("completed category repeated", e)
	}
	if calls() != before+2 {
		t.Fatal("completed category emitted more HTTP")
	}
	// Cancel the remaining once categories, then issue a distinct finite run.
	status, e = gate.Change(f.ctx, syncMutation(id, status.Revision, "000000000003", "set"))
	if e != nil {
		t.Fatal(e)
	}
	status, e = gate.Change(f.ctx, syncMutation(id, status.Revision, "000000000004", "once"))
	if e != nil {
		t.Fatal(e)
	}
	streamBefore, e := f.consumerStore.LoadSourceFactsStream(f.ctx, f.streams[sourcefacts.ContextProfile])
	if e != nil {
		t.Fatal(e)
	}
	f.setPage(sourcefacts.ContextProfile, "must-not-be-published", f.documents[sourcefacts.ContextProfile])
	hold := syncResponseHold{HTTPReader: f.consumer.Reader, received: make(chan struct{}), release: make(chan struct{})}
	consumer := f.consumer
	consumer.Reader = hold
	result := make(chan error, 1)
	go func() { _, e := consumer.PullOnce(f.ctx, streamBefore.ID); result <- e }()
	<-hold.received
	active, e := gate.Status(f.ctx, id)
	if e != nil || active.Effective != "manual_running" {
		t.Fatal("normal once reported stopping", active.Effective, e)
	}
	stopped, e := gate.Change(f.ctx, syncMutation(id, status.Revision, "000000000005", "set"))
	if e != nil || stopped.Effective != "stopping" {
		t.Fatal("pause did not expose draining", stopped.Effective, e)
	}
	close(hold.release)
	if e = <-result; !errors.Is(e, connectorsync.ErrChanged) {
		t.Fatal("response crossed pause commit boundary", e)
	}
	after, e := f.consumerStore.LoadSourceFactsStream(f.ctx, streamBefore.ID)
	if e != nil || after.Cursor != streamBefore.Cursor || after.Revision != streamBefore.Revision || after.LastError != streamBefore.LastError {
		t.Fatal("stale response mutated source state", after, e)
	}
}
