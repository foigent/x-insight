package db_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

func TestIndependentReview27CollectionLifecycleVisible(t *testing.T) {
	f := newSourceFixture(t)
	f.allow()
	for _, status := range []string{"paused", "revoked"} {
		f.exec(`UPDATE source_fact_streams SET revision=revision+1,status=$2 WHERE source_system_id=$1::uuid`, f.source, status)
		if _, err := f.consumer.PullOnce(f.ctx, f.streams[sourcefacts.IdentityProfile]); !errors.Is(err, sourcefacts.ErrUnavailable) {
			t.Fatalf("%s collection unexpectedly available: %v", status, err)
		}
		reading := f.reading()
		raw, err := json.Marshal(reading.SourceCoverage)
		if err != nil {
			t.Fatal(err)
		}
		if reading.SourceCoverage.KnownDocuments != 3 {
			t.Fatal("collection stop erased legitimately readable history")
		}
		t.Logf("collection=%s reading=%s source_coverage=%s", status, reading.ID, raw)
		if !strings.Contains(string(raw), status) {
			t.Errorf("new reading does not disclose known %s collection state", status)
		}
	}
}

func TestIndependentReview27IdentityConflictVisible(t *testing.T) {
	f := newSourceFixture(t)
	f.allow()
	p := sourcefacts.IdentityProfile
	before, err := f.consumerStore.LoadSourceFactsStream(f.ctx, f.streams[p])
	if err != nil {
		t.Fatal(err)
	}
	changed := f.documents[p]
	changed.Identity["owner_user_id"] = "8901"
	f.setPage(p, "conflicting-owner-page", changed)
	if _, err = f.consumer.PullOnce(f.ctx, f.streams[p]); !errors.Is(err, sourcefacts.ErrConflict) {
		t.Fatalf("identity conflict not rejected: %v", err)
	}
	after, err := f.consumerStore.LoadSourceFactsStream(f.ctx, f.streams[p])
	if err != nil {
		t.Fatal(err)
	}
	if before.Cursor != after.Cursor {
		t.Fatal("conflict advanced the checkpoint")
	}
	var code string
	if err = f.admin.QueryRowContext(f.ctx, `SELECT last_error FROM source_fact_streams WHERE id=$1::uuid`, f.streams[p]).Scan(&code); err != nil {
		t.Fatal(err)
	}
	reading := f.reading()
	raw, err := json.Marshal(reading.SourceCoverage)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("conflict rejected, cursor preserved; durable_error=%q reading=%s source_coverage=%s", code, reading.ID, raw)
	if code == "" || !strings.Contains(string(raw), code) {
		t.Error("known rejected identity conflict is absent from durable source status and the new reading")
	}
}

func TestIndependentReview27PendingTerminalDoesNotAdoptOlderIdentity(t *testing.T) {
	f := newSourceFixture(t)
	f.allow()
	f.money()
	p := sourcefacts.IdentityProfile
	parent := f.documents[p]
	parent.Stage = "terminal"
	parent.Identity["owner_user_id"] = "801"
	f.setPage(p, "terminal-parent-not-yet-received", parent)
	f.mu.Lock()
	wire := f.pages[p].Events[0]
	f.mu.Unlock()
	predecessor, err := sourcefacts.ValidateItem(wire, p)
	if err != nil {
		t.Fatal(err)
	}
	child := predecessor.Document
	child.Revision = "2"
	child.PreviousRevision = "1"
	child.PreviousHash = predecessor.SourceHash
	child.CorrectionReason = "correct terminal owner"
	child.Identity["owner_user_id"] = "802"
	f.setPage(p, "terminal-child-first", child)
	f.pull(p)
	pending := f.reading()
	if pending.SourceCoverage.PendingPredecessor != 1 {
		t.Fatalf("pending coverage: %+v", pending.SourceCoverage)
	}
	groups, err := f.workspace.GetWorkbenchContributions(f.ctx, f.subject, pending.ID, "account", 0, 10, "", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range groups.Rows {
		if group.Label != "" {
			t.Errorf("pending terminal adopted account %q", group.Label)
		}
	}
	// Use original canonical parent bytes; child maps are intentionally mutable
	// test data, while the retained source bytes remain immutable.
	parent = predecessor.Document
	parent.Identity["owner_user_id"] = "801"
	f.setPage(p, "terminal-parent-late", parent)
	f.pull(p)
	complete := f.reading()
	groups, err = f.workspace.GetWorkbenchContributions(f.ctx, f.subject, complete.ID, "account", 0, 10, "", time.Now().UTC())
	if err != nil || len(groups.Rows) != 1 || groups.Rows[0].Label != "802" {
		t.Fatalf("complete terminal groups: %+v %v", groups, err)
	}
	old, err := f.workspace.GetWorkbenchSourceFacts(f.ctx, f.subject, pending.ID, 50, "", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range old.Items {
		if item.Profile == p && item.Stage == "terminal" && (item.ChainState != "missing_predecessor" || item.Identity != nil) {
			t.Fatalf("old reading was reinterpreted: %+v", item)
		}
	}
	if f.sum(complete).CalculatedReferenceMicros != "30000000" || f.sum(pending).CalculatedReferenceMicros != "30000000" {
		t.Fatal("identity change altered economic amount")
	}
	t.Log("pending terminal suppresses older account; new reading adopts resolved revision; old reading and economic amount remain unchanged")
}
