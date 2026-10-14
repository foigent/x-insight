package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

// This driver exercises the real CommitContentManifests control flow offline.
// It models an immutable existing fact, and records receipt/commit/rollback
// operations. It is not evidence that PostgreSQL constraints have executed.
type receiptTestDriver struct{ state *receiptTestState }
type receiptTestState struct {
	collection                 sourcefacts.ContentCollection
	bytes                      []byte
	hash                       string
	pending, receipts, commits int
	failReceipt                bool
}
type receiptTestConn struct{ state *receiptTestState }
type receiptTestTx struct{ state *receiptTestState }
type receiptTestRows struct {
	values []driver.Value
	done   bool
}

var receiptDriverCounter atomic.Uint64

func (d receiptTestDriver) Open(string) (driver.Conn, error) { return &receiptTestConn{d.state}, nil }
func (c *receiptTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}
func (c *receiptTestConn) Close() error { return nil }
func (c *receiptTestConn) Begin() (driver.Tx, error) {
	c.state.pending = 0
	return receiptTestTx{c.state}, nil
}
func (c *receiptTestConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}
func (t receiptTestTx) Commit() error {
	t.state.receipts += t.state.pending
	t.state.pending = 0
	t.state.commits++
	return nil
}
func (t receiptTestTx) Rollback() error { t.state.pending = 0; return nil }
func (r *receiptTestRows) Columns() []string {
	cols := make([]string, len(r.values))
	for i := range cols {
		cols[i] = fmt.Sprint(i)
	}
	return cols
}
func (r *receiptTestRows) Close() error { return nil }
func (r *receiptTestRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	copy(dest, r.values)
	return nil
}
func (c *receiptTestConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	switch {
	case strings.HasPrefix(q, "INSERT INTO source_content_manifests"):
		return driver.RowsAffected(0), nil // the source fact already exists
	case strings.HasPrefix(q, "INSERT INTO source_content_receipts"):
		if args[0].Value != c.state.collection.ID || args[1].Value != c.state.collection.CredentialID || args[2].Value != c.state.collection.RegistrationHash || args[3].Value != c.state.collection.ContentScopeRevision || args[4].Value != c.state.collection.RecoveryEpoch {
			return nil, errors.New("receipt used a stale binding")
		}
		if c.state.failReceipt {
			return nil, errors.New("receipt write rejected")
		}
		c.state.pending++
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(q, "UPDATE source_content_collections"), strings.Contains(q, "pg_advisory_xact_lock"):
		return driver.RowsAffected(1), nil
	default:
		return nil, fmt.Errorf("unexpected exec: %s", q)
	}
}
func (c *receiptTestConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	s := c.state
	switch {
	case strings.HasPrefix(q, "SELECT id::text"):
		v := s.collection
		purposes, _ := json.Marshal(v.AllowedPurposes)
		coverage, _ := json.Marshal(v.CoverageFrom)
		return &receiptTestRows{values: []driver.Value{v.ID, v.SourceSystemID, v.SourceInstance, v.OrgID, v.ProjectID, v.CredentialID, v.RegistrationHash, v.ContentScopeRevision, v.RecoveryEpoch, purposes, coverage, v.Cursor, v.LastSequence, v.HighWatermarkSequence, v.HighWatermark, v.Status, v.Revision, v.RequiresAttention, v.LastError, []byte(`{}`), "unknown", []byte(`[]`)}}, nil
	case strings.HasPrefix(q, "SELECT source_bytes FROM source_content_manifests"):
		if args[4].Value != s.hash {
			return &receiptTestRows{values: []driver.Value{nil}, done: true}, nil
		}
		return &receiptTestRows{values: []driver.Value{s.bytes}}, nil
	case strings.HasPrefix(q, "SELECT EXISTS(SELECT 1 FROM source_content_receipts"):
		return &receiptTestRows{values: []driver.Value{s.pending == 1}}, nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", q)
	}
}
func receiptFixture(t *testing.T) (*SourceFactsStore, *receiptTestState, sourcefacts.ContentManifestPage, func(any) []byte) {
	t.Helper()
	at := "2026-09-15T00:00:00.000000Z"
	canonical := func(v any) []byte {
		t.Helper()
		raw, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		obj, e := connector.DecodeStrictJSONObject(raw)
		if e != nil {
			t.Fatal(e)
		}
		raw, e = connector.CanonicalizeJSONValue(obj)
		if e != nil {
			t.Fatal(e)
		}
		return raw
	}
	c := sourcefacts.ContentCollection{ID: "11111111-1111-1111-1111-111111111111", SourceSystemID: "22222222-2222-2222-2222-222222222222", SourceInstance: "source", OrgID: "7", ProjectID: "9", CredentialID: "new-credential", RegistrationHash: sourcefacts.Hash([]byte("new-registration")), ContentScopeRevision: "new-scope", RecoveryEpoch: "2", AllowedPurposes: []string{sourcefacts.ContentPurposeHumanReview}, CoverageFrom: sourcefacts.ContentBoundary{Time: at, AfterSequence: "0"}, LastSequence: "0", HighWatermarkSequence: "0", Status: "active", Revision: 7}
	d := sourcefacts.ContentManifest{SchemaVersion: sourcefacts.ContentManifestVersion, SourceInstance: c.SourceInstance, OrgID: c.OrgID, ProjectID: c.ProjectID, RequestID: "old-fact", Profile: sourcefacts.ContentProfile, Revision: "1", OccurredAt: at, RecordedAt: at, Protocol: "openai.responses", ObservationPoint: "client_request", ContentItemID: "body", Capture: sourcefacts.ContentCapture{Mode: "metadata_only", PolicyRef: "p", PolicyVersion: "1", PolicyEffectiveAt: at, Completeness: "not_captured", Reason: "policy_metadata_only"}}
	d.SourceFactID = sourcefacts.SourceID(sourcefacts.ContentManifestVersion, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile, d.RequestID, d.ObservationPoint, d.AttemptID, d.ContentItemID)
	d.EventID = sourcefacts.SourceID(sourcefacts.ContentManifestVersion+".revision", d.SourceFactID, d.Revision)
	b := canonical(d)
	hash := sourcefacts.Hash(b)
	page := sourcefacts.ContentManifestPage{SchemaVersion: sourcefacts.ContentManifestPageVersion, Profile: sourcefacts.ContentProfile, SourceInstance: c.SourceInstance, OrgID: c.OrgID, ProjectID: c.ProjectID, GeneratedAt: at, ContentScopeRevision: c.ContentScopeRevision, RecoveryEpoch: c.RecoveryEpoch, CoverageFrom: c.CoverageFrom, RetentionFloor: c.CoverageFrom, AfterSequence: "0", LastSequence: "1", HighWatermarkSequence: "1", NextCursor: "next", HighWatermark: "high", Coverage: "bounded", Gaps: []sourcefacts.ContentGap{}, Items: []sourcefacts.ContentManifestEntry{{Sequence: "1", Document: d, SourceSHA256: hash, Availability: sourcefacts.ContentAvailability{State: "not_captured", EvaluatedAt: at}}}}
	state := &receiptTestState{collection: c, bytes: b, hash: hash}
	name := fmt.Sprintf("receipt-test-%d", receiptDriverCounter.Add(1))
	sql.Register(name, receiptTestDriver{state})
	database, e := sql.Open(name, "")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { database.Close() })
	return &SourceFactsStore{db: database}, state, page, canonical
}
func TestContentReceiptReplayRunsRealCommitPath(t *testing.T) {
	for _, kind := range []string{"new-credential", "new-epoch", "new-collection"} {
		t.Run(kind, func(t *testing.T) {
			store, s, p, canonical := receiptFixture(t)
			switch kind {
			case "new-credential":
				s.collection.CredentialID = "replacement-credential"
				s.collection.RegistrationHash = sourcefacts.Hash([]byte("replacement-registration"))
			case "new-epoch":
				s.collection.RecoveryEpoch = "3"
				p.RecoveryEpoch = "3"
			case "new-collection":
				s.collection.ID = "33333333-3333-3333-3333-333333333333"
			}
			before := string(s.bytes)
			if err := store.CommitContentManifests(t.Context(), s.collection, canonical(p), time.Now()); err != nil {
				t.Fatal(err)
			}
			if s.receipts != 1 || s.commits != 1 || string(s.bytes) != before {
				t.Fatal("replay failed to record receipt or changed immutable fact")
			}
		})
	}
}
func TestContentReceiptRejectsInvalidOrStaleReplay(t *testing.T) {
	for _, kind := range []string{"late-old-binding", "outside-time", "outside-sequence", "wrong-hash", "wrong-identity", "conflicting-bytes", "receipt-write-failure"} {
		t.Run(kind, func(t *testing.T) {
			store, s, p, canonical := receiptFixture(t)
			expected := s.collection
			switch kind {
			case "late-old-binding":
				expected.CredentialID = "old-credential"
			case "outside-time":
				expected.CoverageFrom.Time = "2026-09-15T01:00:00.000000Z"
				s.collection = expected
				p.CoverageFrom = expected.CoverageFrom
				p.RetentionFloor = expected.CoverageFrom
			case "outside-sequence":
				expected.CoverageFrom.AfterSequence = "1"
				s.collection = expected
				p.CoverageFrom = expected.CoverageFrom
				p.RetentionFloor = expected.CoverageFrom
			case "wrong-hash":
				p.Items[0].SourceSHA256 = sourcefacts.Hash([]byte("wrong"))
			case "wrong-identity":
				p.Items[0].Document.ProjectID = "99"
			case "conflicting-bytes":
				p.Items[0].Document.Capture.PolicyRef = "changed-without-revision"
				p.Items[0].SourceSHA256 = sourcefacts.Hash(canonical(p.Items[0].Document))
			case "receipt-write-failure":
				s.failReceipt = true
			}
			if err := store.CommitContentManifests(t.Context(), expected, canonical(p), time.Now()); err == nil {
				t.Fatal("invalid replay accepted")
			}
			if s.receipts != 0 || s.commits != 0 || s.pending != 0 {
				t.Fatal("failed replay advanced progress or receipt")
			}
		})
	}
}
