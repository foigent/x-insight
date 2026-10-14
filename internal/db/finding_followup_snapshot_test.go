package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/workchain"
	"github.com/foigent/x-insight/internal/workspace"
)

// Offline model of a content-linked case. Only persisted reads and the online
// deny predicate are supported; unexpected reads, DML and network access fail.
type followupSnapshotState struct {
	missingFinding, recorded, corruptAction, missingManifest bool
	document                                                 []byte
	hash                                                     string
	onlineChecks, commits                                    int
}
type followupSnapshotDriver struct{ s *followupSnapshotState }
type followupSnapshotConn struct{ s *followupSnapshotState }
type followupSnapshotTx struct{ s *followupSnapshotState }

func (d followupSnapshotDriver) Open(string) (driver.Conn, error) {
	return &followupSnapshotConn{d.s}, nil
}
func (c *followupSnapshotConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *followupSnapshotConn) Close() error { return nil }
func (c *followupSnapshotConn) Begin() (driver.Tx, error) {
	return nil, errors.New("restore must use explicit read-only transaction")
}
func (c *followupSnapshotConn) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if !opts.ReadOnly || opts.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
		return nil, errors.New("restore transaction contract changed")
	}
	return followupSnapshotTx{c.s}, nil
}
func (t followupSnapshotTx) Commit() error   { t.s.commits++; return nil }
func (t followupSnapshotTx) Rollback() error { return nil }
func (c *followupSnapshotConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	s := c.s
	row := func(values ...driver.Value) (driver.Rows, error) { return &receiptTestRows{values: values}, nil }
	empty := func(n int) (driver.Rows, error) {
		return &receiptTestRows{values: make([]driver.Value, n), done: true}, nil
	}
	switch {
	case q == `SELECT to_regclass('public.case_content_links') IS NOT NULL`:
		return row(true)
	case q == `SELECT `+unscopedContentCaseSQL("$1"), q == "SELECT "+scopedCaseSQL("$1", "$2"):
		s.onlineChecks++
		return row(false)
	case q == `SELECT id,version,status,sealed_pack,sealed_digest,sealed_revision FROM investigation_cases ORDER BY id`:
		return row("case", int64(1), "open", []byte(nil), nil, nil)
	case q == `SELECT id FROM case_findings WHERE case_id=$1 ORDER BY id`:
		return row("finding")
	case q == `SELECT EXISTS(SELECT 1 FROM case_findings WHERE case_id=$1 AND id=$2)`:
		return row(!s.missingFinding && args[0].Value == "case" && args[1].Value == "finding")
	case strings.HasPrefix(q, "SELECT assignee_subject_id, assignee_name, due_at, due_timezone, version, updated_at, updated_by_subject"):
		if !s.recorded {
			return empty(7)
		}
		return row(nil, "", nil, nil, int64(1), time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC), "xs_1122334455667788")
	case q == `SELECT receipt FROM case_finding_actions WHERE case_id=$1 ORDER BY finding_id,result_version`:
		if s.corruptAction {
			return row([]byte(`{invalid`))
		}
		return empty(1)
	case q == `SELECT receipt,request_sha256,created_at FROM case_finding_actions WHERE case_id=$1 AND finding_id=$2 ORDER BY result_version`:
		return empty(3)
	case q == `SELECT source_bytes,source_sha256 FROM source_content_manifests WHERE `+contentIdentityWhere:
		if s.missingManifest {
			return empty(2)
		}
		return row(s.document, s.hash)
	default:
		return nil, fmt.Errorf("unexpected query: %.100s", q)
	}
}
func followupSnapshotDB(t *testing.T, state *followupSnapshotState) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("followup-snapshot-%d", receiptDriverCounter.Add(1))
	sql.Register(name, followupSnapshotDriver{state})
	pool, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}
func TestFollowupRestoreSnapshotPreservesOnlineScopeAndIntegrity(t *testing.T) {
	for _, scenario := range []string{"valid_without_session", "online_unscoped_denied", "online_scoped_denied", "missing_finding", "missing_action_chain", "malformed_action"} {
		t.Run(scenario, func(t *testing.T) {
			state := &followupSnapshotState{}
			pool := followupSnapshotDB(t, state)
			ctx := context.Background()
			if strings.HasPrefix(scenario, "online_") {
				if scenario == "online_scoped_denied" {
					ctx = workchain.WithAccess(ctx, &workchain.Access{Subject: "xs_1122334455667788"})
				}
				if _, err := readFollowup(ctx, pool, "case", "finding"); !errors.Is(err, workspace.ErrNotFound) {
					t.Fatal("online content case disclosed", err)
				}
				if state.onlineChecks != 1 || state.commits != 0 {
					t.Fatal("online guard changed", state)
				}
				return
			}
			state.missingFinding = scenario == "missing_finding"
			state.recorded = scenario == "missing_action_chain"
			state.corruptAction = scenario == "malformed_action"
			err := ValidateFindingResponsibilityRestore(ctx, pool)
			switch scenario {
			case "valid_without_session":
				if err != nil || state.commits != 1 {
					t.Fatal("valid persisted snapshot rejected", err)
				}
			case "missing_finding":
				if !errors.Is(err, workspace.ErrNotFound) {
					t.Fatal("missing persisted identity ignored", err)
				}
			case "missing_action_chain":
				if !errors.Is(err, errResponsibilityRestore) {
					t.Fatal("projection without action history accepted", err)
				}
			default:
				if err == nil {
					t.Fatal("corrupt persisted receipt accepted")
				}
			}
			if state.onlineChecks != 0 || (err != nil && state.commits != 0) {
				t.Fatal("restore consulted live access or committed failure", state)
			}
		})
	}
}
func TestContentRestoreReadsRetainedManifestWithoutSession(t *testing.T) {
	for _, scenario := range []string{"valid_expired", "missing_manifest", "corrupt_hash", "mismatched_reference"} {
		t.Run(scenario, func(t *testing.T) {
			_, old, _, choice, _, now := reviewReplayFixture(t)
			expires, err := time.Parse(time.RFC3339Nano, choice.Reference.ExpiresAt)
			if err != nil || !expires.Before(now) {
				t.Fatal("fixture manifest must be expired", err)
			}
			state := &followupSnapshotState{document: old.document, hash: old.hash}
			switch scenario {
			case "missing_manifest":
				state.missingManifest = true
			case "corrupt_hash":
				state.hash = "sha256:" + strings.Repeat("0", 64)
			case "mismatched_reference":
				choice.Reference.RequestID = "different-request"
			}
			err = readContentChoice(context.Background(), followupSnapshotDB(t, state), choice)
			if scenario == "valid_expired" {
				if err != nil {
					t.Fatal("historical immutable manifest required online context", err)
				}
			} else if scenario == "missing_manifest" {
				if !errors.Is(err, workspace.ErrNotFound) {
					t.Fatal("missing manifest accepted", err)
				}
			} else if !errors.Is(err, workspace.ErrConflict) {
				t.Fatal("corrupt or mismatched manifest accepted", err)
			}
			if state.onlineChecks != 0 || state.commits != 0 {
				t.Fatal("historical manifest performed online authorization or writes")
			}
		})
	}
}
