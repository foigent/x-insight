package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workchain"
	"github.com/foigent/x-insight/internal/workspace"
)

// Runs the actual Store entry points. This intentionally refuses every DML and
// every unmodelled query, including current body/collection availability reads.
// It proves local call ordering, not PostgreSQL execution or concurrency.
type reviewReplayState struct {
	document                       []byte
	hash, requestHash, operationID string
	receipt                        []byte
	found, caseAllowed             bool
	findings                       map[string]string
	commits, dml                   int
}
type reviewReplayDriver struct{ s *reviewReplayState }
type reviewReplayConn struct{ s *reviewReplayState }
type reviewReplayTx struct{ s *reviewReplayState }

func (d reviewReplayDriver) Open(string) (driver.Conn, error) { return &reviewReplayConn{d.s}, nil }
func (c *reviewReplayConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *reviewReplayConn) Close() error              { return nil }
func (c *reviewReplayConn) Begin() (driver.Tx, error) { return reviewReplayTx{c.s}, nil }
func (t reviewReplayTx) Commit() error                { t.s.commits++; return nil }
func (t reviewReplayTx) Rollback() error              { return nil }
func (c *reviewReplayConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, "SELECT pg_advisory_xact_lock(") {
		return driver.RowsAffected(1), nil
	}
	c.s.dml++
	return nil, fmt.Errorf("unexpected mutation or lock: %.80s", q)
}
func (c *reviewReplayConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	s := c.s
	switch {
	case strings.HasPrefix(q, "SELECT statement,version FROM case_findings WHERE case_id=$1 AND id=$2"):
		statement, found := s.findings[args[0].Value.(string)+":"+args[1].Value.(string)]
		return &receiptTestRows{values: []driver.Value{statement, int64(1)}, done: !found}, nil
	case strings.HasPrefix(q, "SELECT COALESCE(f.cost_project_ref,'')"):
		return &receiptTestRows{values: []driver.Value{""}}, nil
	case strings.HasPrefix(q, "SELECT source_bytes,source_sha256 FROM source_content_manifests"):
		return &receiptTestRows{values: []driver.Value{s.document, s.hash}}, nil
	case strings.HasPrefix(q, "SELECT request_sha256,receipt FROM"):
		if !s.found || args[len(args)-1].Value != s.operationID {
			return &receiptTestRows{values: []driver.Value{nil, nil}, done: true}, nil
		}
		return &receiptTestRows{values: []driver.Value{s.requestHash, s.receipt}}, nil
	case strings.HasPrefix(q, "SELECT ") && strings.Contains(q, "investigation_cases"):
		return &receiptTestRows{values: []driver.Value{s.caseAllowed}}, nil
	default:
		return nil, fmt.Errorf("unexpected query: %.100s", q)
	}
}

func reviewReplayFixture(t *testing.T) (*WorkspaceStore, *reviewReplayState, context.Context, workspace.ContentChoice, workspace.Actor, time.Time) {
	t.Helper()
	_, old, page, canonical := receiptFixture(t)
	d := page.Items[0].Document
	d.Capture.Mode = "full_content"
	d.Capture.Completeness = "complete"
	d.Capture.Reason = "none"
	d.ContentRef = "uc1_" + strings.Repeat("a", 64)
	d.Representation = &sourcefacts.ContentRepresentation{MediaType: "application/json", Encoding: "identity", ByteDomain: "stored_representation_before_http_content_encoding", SavedBytes: 2, Digest: sourcefacts.Hash([]byte("{}"))}
	d.CapturedAt = d.OccurredAt
	d.ExpiresAt = "2026-09-15T00:01:00.000000Z"
	raw := canonical(d)
	ref, err := sourcefacts.FrozenContentManifestRef(old.collection.SourceSystemID, sourcefacts.ContentManifestEntry{Document: d, SourceSHA256: sourcefacts.Hash(raw)})
	if err != nil {
		t.Fatal(err)
	}
	pointer := ""
	choice := workspace.ContentChoice{Scope: "finding", Relation: "supports", Reference: ref, Location: sourcefacts.ContentLocation{Kind: "json_pointer", Pointer: &pointer}}
	if !workspace.ValidContentChoice(choice) {
		t.Fatal("invalid test content choice")
	}
	state := &reviewReplayState{document: raw, hash: sourcefacts.Hash(raw), found: true, caseAllowed: true}
	name := fmt.Sprintf("review-replay-%d", receiptDriverCounter.Add(1))
	sql.Register(name, reviewReplayDriver{state})
	pool, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	actor, err := workspace.HumanActor("xs_1122334455667788")
	if err != nil {
		t.Fatal(err)
	}
	a := &workchain.Access{Subject: actor.Ref, Reviewer: true, CommitCheck: func(f func() error) error { return f() }, Authorization: workbench.Authorization{Grants: []workbench.Grant{{SourceSystemID: ref.SourceSystemID, SourceProject: "project-" + ref.ProjectID, AllowContent: true, AllowedContentPurposes: []string{sourcefacts.ContentPurposeHumanReview}}}}}
	return &WorkspaceStore{db: pool}, state, workchain.WithAccess(context.Background(), a), choice, actor, time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
}

func TestContentReceiptRetryPreservesSuccessfulCommandWithoutBody(t *testing.T) {
	for _, kind := range []string{"creation", "review", "closure"} {
		for _, scenario := range []string{"expired_or_unavailable_body", "new_operation", "changed_pointer", "changed_relation", "revoked_content", "denied_case", "revoked_session"} {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				store, state, ctx, choice, actor, now := reviewReplayFixture(t)
				operationID := "11111111-1111-4111-8111-111111111111"
				selection := workspace.BasisSelection{Contents: []workspace.ContentChoice{choice}}
				action := workspace.AssessmentCommand{Operation: "record_review", OperationID: operationID, ExpectedVersion: 1, ExpectedCaseVersion: 2, ExpectedBasisDigest: sourcefacts.Hash(nil), Selection: selection, Review: &workspace.ReviewPayload{Conclusion: "supported", Reason: "Examined exact content"}}
				if kind == "closure" {
					action.Operation = "record_progress"
					action.Review = nil
					action.Progress = &workspace.ProgressPayload{State: "ended", Reason: "Verified", Closure: &workspace.Closure{Kind: "verified_remediation", Explanation: "Verified exact reference", Scope: "This finding", VerifiedAt: &now, VerificationContents: []workspace.ContentChoice{choice}}}
				}
				create := workspace.ReviewCreationCommand{Operation: "start_review", OperationID: operationID, NewCase: &workspace.CreateCaseInput{Title: "Review"}, Finding: workspace.CreateFindingInput{Title: "Claim", Statement: "Bounded assertion", Classification: "hypothesis"}, Selection: selection, ExpectedBasisDigest: sourcefacts.Hash(nil), Review: &workspace.ReviewPayload{Conclusion: "supported", Reason: "Examined exact content"}, Reason: "Begin review"}
				var err error
				if kind == "creation" {
					create, err = workspace.NormalizeReviewCreation(create)
					state.requestHash = assessmentRequestHash(create)
					state.receipt, _ = json.Marshal(workspace.ReviewCreationReceipt{OperationID: operationID, Operation: kind, CaseID: "xc_1122334455667788", FindingID: "xf_1122334455667788", Command: create})
				} else {
					action, err = workspace.NormalizeAssessmentCommand(action)
					state.requestHash = assessmentRequestHash(action)
					state.receipt, _ = json.Marshal(workspace.AssessmentAction{OperationID: operationID, Command: action})
				}
				if err != nil {
					t.Fatal(err)
				}
				state.operationID = operationID
				expected := error(nil)
				switch scenario {
				case "new_operation":
					create.OperationID = "22222222-2222-4222-8222-222222222222"
					action.OperationID = create.OperationID
					expected = workspace.ErrContentLocationProofRequired
				case "changed_pointer":
					p := "/changed"
					create.Selection.Contents[0].Location.Pointer = &p
					action.Selection.Contents[0].Location.Pointer = &p
					expected = workspace.ErrConflict
				case "changed_relation":
					create.Selection.Contents[0].Relation = "contradicts"
					action.Selection.Contents[0].Relation = "contradicts"
					expected = workspace.ErrConflict
				case "revoked_content":
					workchain.FromContext(ctx).Authorization.Grants = nil
					expected = workspace.ErrNotFound
				case "denied_case":
					state.caseAllowed = false
					expected = workspace.ErrNotFound
				case "revoked_session":
					workchain.FromContext(ctx).CommitCheck = func(func() error) error { return workchain.ErrDenied }
					expected = workchain.ErrDenied
				}
				var got []byte
				if kind == "creation" {
					v, e := store.CreateReview(ctx, actor, create, now, nil)
					err = e
					got, _ = json.Marshal(v)
				} else {
					v, e := store.ApplyFindingAssessment(ctx, "xc_1122334455667788", "xf_1122334455667788", actor, action, now)
					err = e
					got, _ = json.Marshal(v)
				}
				if !errors.Is(err, expected) {
					t.Fatalf("got %v want %v", err, expected)
				}
				if err == nil && string(got) != string(state.receipt) {
					t.Fatal("receipt changed")
				}
				if state.commits != 0 || state.dml != 0 {
					t.Fatalf("retry wrote state: commits=%d dml=%d", state.commits, state.dml)
				}
			})
		}
	}
}
