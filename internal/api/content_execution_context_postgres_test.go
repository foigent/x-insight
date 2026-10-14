package api_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workspace"
)

// Reuses the ordinary scoped user and actual content proof handler. No B data,
// B grants, preference grants, or caller-supplied proof bypass is introduced.
func contentContextCommand(t *testing.T, f *scopedContentFixture) workspace.ReviewCreationCommand {
	t.Helper()
	seedScopedContentI01WithoutB(t, f)
	// The shared browser seed advances a synthetic API clock by one second.
	// Use the database wall clock after the projection commit for these TTL
	// cases, so the existing expires_at=clock_timestamp() injection preserves
	// period_end <= cutoff < expires_at without altering either frozen reading.
	var now time.Time
	if err := f.admin.QueryRowContext(f.ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.apiOffset = now.Sub(f.base)
	f.mu.Unlock()
	f.assertI01WithoutB(t)
	input := httpReadingRequest(f.sourceID)
	input.Filters.SourceProjects = []string{"project-" + f.collection.ProjectID}
	var cost workbench.Reading
	var runtime workbench.RuntimeReading
	if err := json.Unmarshal(f.must(t, "POST", "/api/v1/workbench/readings", input, 201, nil), &cost); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(f.must(t, "POST", "/api/v1/runtime/readings", workbench.RuntimeReadingRequest{ContractVersion: workbench.RuntimeContractVersion, MetricVersion: workbench.RuntimeMetricVersion, Period: input.Period, Filters: input.Filters}, 201, nil), &runtime); err != nil || runtime.ObservationCount != 0 {
		t.Fatal("zero B runtime", err)
	}
	var requests workbench.RuntimeRequests
	if err := json.Unmarshal(f.must(t, "GET", "/api/v1/runtime/readings/"+runtime.ID+"/requests?entry_time=all", nil, 200, nil), &requests); err != nil || len(requests.Items) != 1 {
		t.Fatal("exact content request", err)
	}
	var execution string
	if err := f.admin.QueryRowContext(f.ctx, `SELECT execution_id FROM workbench_economic_facts WHERE source_system_id=$1::uuid AND request_id=$2`, f.sourceID, f.choice.Reference.RequestID).Scan(&execution); err != nil {
		t.Fatal(err)
	}
	c := f.creation(t)
	c.ExecutionContext = &workspace.ExecutionContext{WorkbenchReadingID: cost.ID, ExecutionID: execution, ExecutionAsOf: cost.Cutoff, RuntimeReadingID: runtime.ID, RuntimeRequestRef: requests.Items[0].Ref}
	var basis workspace.ReviewBasis
	if err := json.Unmarshal(f.must(t, "POST", "/api/v1/security/reviews/preview", c, 200, nil), &basis); err != nil {
		t.Fatal(err)
	}
	if basis.ResolvedExecutionContext == nil || !workspace.SameAssessmentValue(basis.ResolvedExecutionContext.Contents, c.Selection.Contents) || len(basis.ResolvedExecutionContext.Resources) != 0 {
		t.Fatal("content context missing")
	}
	c.ExpectedBasisDigest = basis.Digest
	return c
}
func contentContextSnapshot(t *testing.T, f *scopedContentFixture) string {
	t.Helper()
	var executions string
	if err := f.admin.QueryRowContext(f.ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text FROM case_executions t`).Scan(&executions); err != nil {
		t.Fatal(err)
	}
	return sourcefacts.Hash([]byte(f.snapshot(t, true) + executions))
}
func TestContentExecutionContextI01WithoutBAtomicHistorySealRestore(t *testing.T) {
	f := newScopedContentFixture(t)
	c := contentContextCommand(t, f)
	f.resetSource()
	var created workspace.ReviewCreationReceipt
	if err := json.Unmarshal(f.must(t, "POST", "/api/v1/security/reviews", c, 200, nil), &created); err != nil {
		t.Fatal(err)
	}
	f.assertSource(t, 2, 1)
	var record workspace.CaseRecord
	if err := json.Unmarshal(f.must(t, "GET", "/api/v1/cases/"+created.CaseID, nil, 200, nil), &record); err != nil || len(record.Executions) != 1 || len(record.Contents) != 1 || len(record.Resources) != 0 || len(record.Evidence) != 0 || len(record.Gaps) != 0 || record.Case.Version != 2 {
		t.Fatal("atomic case execution/content links", err)
	}
	if record.Executions[0].ExecutionID != c.ExecutionContext.ExecutionID || !record.Executions[0].AsOf.Equal(c.ExecutionContext.ExecutionAsOf) {
		t.Fatal("wrong cost origin")
	}
	before := contentContextSnapshot(t, f)
	// A different as_of is a normal execution citation, not the retained origin.
	// It must still require full B access and roll back the entire attempted link.
	f.must(t, "POST", "/api/v1/cases/"+created.CaseID+"/executions", workspace.AddExecutionInput{ExecutionID: c.ExecutionContext.ExecutionID, AsOf: c.ExecutionContext.ExecutionAsOf.Add(time.Microsecond)}, 404, nil)
	if before != contentContextSnapshot(t, f) {
		t.Fatal("unbound execution citation bypassed full B scope")
	}
	// Expire both navigation readings in this disposable fixture; do not remove
	// immutable manifests, triggers, retained links or receipt history.
	for _, v := range []struct{ table, id string }{{"workbench_readings", c.ExecutionContext.WorkbenchReadingID}, {"runtime_readings", c.ExecutionContext.RuntimeReadingID}} {
		if _, err := f.admin.ExecContext(f.ctx, "UPDATE "+v.table+" SET expires_at=clock_timestamp() WHERE id=$1", v.id); err != nil {
			t.Fatal(err)
		}
	}
	f.resetSource()
	f.mu.Lock()
	f.sourceUnavailable = true
	f.apiOffset = 2 * time.Hour
	f.mu.Unlock()
	var replay workspace.ReviewCreationReceipt
	if err := json.Unmarshal(f.must(t, "POST", "/api/v1/security/reviews", c, 200, nil), &replay); err != nil || !workspace.SameAssessmentValue(created, replay) || before != contentContextSnapshot(t, f) {
		t.Fatal("durable replay changed or consulted expired reading", err)
	}
	path := "/api/v1/cases/" + created.CaseID + "/findings/" + created.FindingID + "/assessment"
	var view workspace.AssessmentView
	if err := json.Unmarshal(f.must(t, "GET", path, nil, 200, nil), &view); err != nil || view.Assessment.Review == nil || !workspace.SameAssessmentValue(view.Assessment.Review.Basis.ResolvedExecutionContext, created.Basis.ResolvedExecutionContext) {
		t.Fatal("historical content context missing", err)
	}
	f.assertSource(t, 0, 0)
	var later workspace.ReviewBasis
	if err := json.Unmarshal(f.must(t, "POST", path+"/preview", workspace.BasisSelection{}, 200, nil), &later); err != nil || !workspace.SameAssessmentValue(later.ResolvedExecutionContext, created.Basis.ResolvedExecutionContext) {
		t.Fatal("later selection lost origin", err)
	}
	progress := workspace.AssessmentCommand{Operation: "record_progress", OperationID: randomTestUUID(t), ExpectedVersion: view.Assessment.Version, ExpectedCaseVersion: view.CaseVersion, ExpectedBasisDigest: later.Digest, Selection: later.Selected, Progress: &workspace.ProgressPayload{State: "in_progress", Reason: "Retained content review"}}
	f.must(t, "POST", strings.TrimSuffix(path, "/assessment")+"/assessment-actions", progress, 200, nil)
	// Scoped HTTP deliberately exposes neither seal nor pack export. Exercise
	// the existing scoped store path, as the resource-context PG fixture does;
	// this is store/restore evidence, not a new HTTP permission or product flow.
	ctx, finish, err := db.NewScopedWorkchainStore(f.reader, f.store).Begin(f.ctx, scopedContentSubject, true, func(commit func() error) error { return commit() })
	if err != nil {
		t.Fatal(err)
	}
	actor, err := workspace.HumanActor(scopedContentSubject)
	if err != nil {
		finish()
		t.Fatal(err)
	}
	sealed, err := f.store.SealCase(ctx, created.CaseID, actor, f.base.Add(2*time.Hour))
	finish()
	if err != nil {
		t.Fatal("scoped store seal", err)
	}
	var pack workspace.EvidencePack
	if err := json.Unmarshal(sealed.SealedPack, &pack); err != nil || len(pack.Contents) != 1 || len(pack.Executions) != 1 || workspace.ValidateExecutionContextPack(pack) != nil {
		t.Fatal("seal lost context", err)
	}
	// Offline restore has no user session. Current grants and unavailable body
	// bytes cannot invalidate retained manifests, histories or sealed snapshots.
	if _, err := f.store.GetFindingReview(f.ctx, created.CaseID, created.FindingID); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatal("unscoped online responsibility read admitted content case", err)
	}
	if _, err := f.admin.ExecContext(f.ctx, `UPDATE workbench_access_grants SET active=false WHERE subject_id=$1 AND source_system_id=$2::uuid`, scopedContentSubject, f.sourceID); err != nil {
		t.Fatal(err)
	}
	// Revoking every grant leaves no scope to enter the Case handler: the
	// middleware rejects this request with scope_denied before Case lookup.
	var problem struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(f.must(t, "GET", path, nil, 403, nil), &problem); err != nil || problem.Code != "scope_denied" {
		t.Fatal("fully revoked scope denial", err)
	}
	f.assertSource(t, 0, 0)
	if err := db.ValidateFindingAssessmentRestore(f.ctx, f.admin); err != nil {
		t.Fatal("restore continuity", err)
	}
	if err := db.ValidateFindingResponsibilityRestore(f.ctx, f.admin); err != nil {
		t.Fatal("responsibility restore continuity", err)
	}
	f.assertSource(t, 0, 0)
	f.assertI01WithoutB(t)
}
func TestContentExecutionContextDeniedCreationLeavesNoPartialRows(t *testing.T) {
	for _, name := range []string{"wrong_request", "wrong_execution", "wrong_asof", "cross_project", "cost_owner", "runtime_owner", "cost_expired", "runtime_expired", "content_expired", "content_grant", "late_revoke", "preview_digest"} {
		t.Run(name, func(t *testing.T) {
			f := newScopedContentFixture(t)
			c := contentContextCommand(t, f)
			switch name {
			case "wrong_request":
				c.ExecutionContext.RuntimeRequestRef = "xrr_" + strings.Repeat("e", 64)
			case "wrong_execution":
				c.ExecutionContext.ExecutionID = "xe1_" + strings.Repeat("f", 64)
			case "wrong_asof":
				c.ExecutionContext.ExecutionAsOf = c.ExecutionContext.ExecutionAsOf.Add(time.Microsecond)
			case "cross_project":
				c.Selection.Contents[0].Reference.ProjectID = "other"
			case "cost_owner":
				_, err := f.admin.ExecContext(f.ctx, `UPDATE workbench_readings SET owner_subject='xs_bbbbbbbbbbbbbbbb' WHERE id=$1`, c.ExecutionContext.WorkbenchReadingID)
				if err != nil {
					t.Fatal(err)
				}
			case "runtime_owner":
				_, err := f.admin.ExecContext(f.ctx, `UPDATE runtime_readings SET owner_subject='xs_bbbbbbbbbbbbbbbb' WHERE id=$1`, c.ExecutionContext.RuntimeReadingID)
				if err != nil {
					t.Fatal(err)
				}
			case "cost_expired":
				_, err := f.admin.ExecContext(f.ctx, `UPDATE workbench_readings SET expires_at=clock_timestamp() WHERE id=$1`, c.ExecutionContext.WorkbenchReadingID)
				if err != nil {
					t.Fatal(err)
				}
			case "runtime_expired":
				_, err := f.admin.ExecContext(f.ctx, `UPDATE runtime_readings SET expires_at=clock_timestamp() WHERE id=$1`, c.ExecutionContext.RuntimeReadingID)
				if err != nil {
					t.Fatal(err)
				}
			case "content_expired":
				f.mu.Lock()
				f.apiOffset = 2 * time.Hour
				f.mu.Unlock()
			case "content_grant":
				if err := f.revoke("content-grant", ""); err != nil {
					t.Fatal(err)
				}
			case "late_revoke":
				f.resetSource()
				f.mu.Lock()
				f.hookAt = 2
				f.hook = func() error { return f.revoke("content-grant", "") }
				f.mu.Unlock()
			case "preview_digest":
				c.ExpectedBasisDigest = "sha256:" + strings.Repeat("0", 64)
			}
			before := contentContextSnapshot(t, f)
			status, raw, err := f.do("POST", "/api/v1/security/reviews", c, nil)
			if err != nil || status < 400 || status >= 500 || contentContextSnapshot(t, f) != before {
				t.Fatalf("negative creation status=%d error=%v response=%s", status, err, raw)
			}
		})
	}
}

func TestContentExecutionContextPureCRemainsLegal(t *testing.T) {
	f := newScopedContentFixture(t)
	_, created := f.create(t)
	var record workspace.CaseRecord
	if err := json.Unmarshal(f.must(t, "GET", "/api/v1/cases/"+created.CaseID, nil, 200, nil), &record); err != nil || len(record.Contents) != 1 || len(record.Executions) != 0 || created.Basis.ResolvedExecutionContext != nil {
		t.Fatal("pure C required or fabricated execution", err)
	}
}

func TestContentExecutionContextPreferencesRemainDenied(t *testing.T) {
	f := newScopedContentFixture(t)
	_ = contentContextCommand(t, f)
	var allowed bool
	if err := f.admin.QueryRowContext(f.ctx, `SELECT EXISTS(SELECT 1 FROM workbench_access_grants WHERE subject_id=$1 AND active AND allow_preferences)`, scopedContentSubject).Scan(&allowed); err != nil || allowed {
		t.Fatal("fixture widened preference authority", err)
	}
	before := contentContextSnapshot(t, f)
	var problem struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(f.must(t, "GET", "/api/v1/workbench/saved-filters", nil, 403, nil), &problem); err != nil || problem.Code != "workbench_scope_denied" || before != contentContextSnapshot(t, f) {
		t.Fatal("preference denial boundary changed", err)
	}
	f.assertI01WithoutB(t)
}

// Labels must not turn a retained origin into permanent execution/content access.
func TestContentExecutionContextRetainedOriginRequiresCurrentGrants(t *testing.T) {
	for _, capability := range []string{"content", "details"} {
		t.Run(capability, func(t *testing.T) {
			f := newScopedContentFixture(t)
			c := contentContextCommand(t, f)
			var created workspace.ReviewCreationReceipt
			if err := json.Unmarshal(f.must(t, "POST", "/api/v1/security/reviews", c, 200, nil), &created); err != nil {
				t.Fatal(err)
			}
			var err error
			if capability == "content" {
				err = f.revoke("content-grant", created.CaseID)
			} else {
				_, err = f.admin.ExecContext(f.ctx, `UPDATE workbench_access_grants SET allow_details=false WHERE subject_id=$1`, scopedContentSubject)
			}
			if err != nil {
				t.Fatal(err)
			}
			before := contentContextSnapshot(t, f)
			f.resetSource()
			ctx, finish, err := db.NewScopedWorkchainStore(f.reader, f.store).Begin(f.ctx, scopedContentSubject, true, func(commit func() error) error { return commit() })
			if err != nil {
				t.Fatal(err)
			}
			defer finish()
			if _, err = f.store.GetCase(ctx, created.CaseID); !errors.Is(err, workspace.ErrNotFound) {
				t.Fatal("retained origin ignored revoked current grant", err)
			}
			if _, err = f.store.PreviewFindingBasis(ctx, created.CaseID, created.FindingID, workspace.BasisSelection{}); !errors.Is(err, workspace.ErrNotFound) {
				t.Fatal("empty follow-up ignored revoked current grant", err)
			}
			actor, err := workspace.HumanActor(scopedContentSubject)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.store.CreateReview(ctx, actor, c, f.base, nil); !errors.Is(err, workspace.ErrNotFound) {
				t.Fatal("committed replay ignored revoked current grant", err)
			}
			if before != contentContextSnapshot(t, f) {
				t.Fatal("denied retained origin changed committed state")
			}
			f.assertSource(t, 0, 0)
		})
	}
}

func TestContentExecutionContextEmptyFollowupForPureC(t *testing.T) {
	f := newScopedContentFixture(t)
	_, created := f.create(t)
	path := "/api/v1/cases/" + created.CaseID + "/findings/" + created.FindingID + "/assessment"
	var view workspace.AssessmentView
	if err := json.Unmarshal(f.must(t, "GET", path, nil, 200, nil), &view); err != nil {
		t.Fatal(err)
	}
	f.resetSource()
	f.mu.Lock()
	f.sourceUnavailable = true
	f.apiOffset = 2 * time.Hour
	f.mu.Unlock()
	var basis workspace.ReviewBasis
	if err := json.Unmarshal(f.must(t, "POST", path+"/preview", workspace.BasisSelection{}, 200, nil), &basis); err != nil || len(basis.Known.Contents) != 1 || len(basis.Selected.Contents) != 0 || basis.ResolvedExecutionContext != nil {
		t.Fatal("pure C empty follow-up invented origin or lost known citation", err)
	}
	command := workspace.AssessmentCommand{Operation: "record_progress", OperationID: randomTestUUID(t), ExpectedVersion: view.Assessment.Version, ExpectedCaseVersion: view.CaseVersion, ExpectedBasisDigest: basis.Digest, Selection: basis.Selected, Progress: &workspace.ProgressPayload{State: "in_progress", Reason: "Follow up without adopting new content"}}
	f.must(t, "POST", strings.TrimSuffix(path, "/assessment")+"/assessment-actions", command, 200, nil)
	before := contentContextSnapshot(t, f)
	f.must(t, "POST", "/api/v1/cases/"+created.CaseID+"/findings/xf_"+strings.Repeat("A", 22)+"/assessment/preview", workspace.BasisSelection{}, 404, nil)
	if before != contentContextSnapshot(t, f) {
		t.Fatal("missing finding preview mutated state")
	}
	if err := f.revoke("content-grant", created.CaseID); err != nil {
		t.Fatal(err)
	}
	ctx, finish, err := db.NewScopedWorkchainStore(f.reader, f.store).Begin(f.ctx, scopedContentSubject, true, func(commit func() error) error { return commit() })
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	if _, err = f.store.PreviewFindingBasis(ctx, created.CaseID, created.FindingID, workspace.BasisSelection{}); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatal("pure C empty follow-up ignored current authorization", err)
	}
	f.assertSource(t, 0, 0)
}

// Both the duplicate tuple and a second exact C origin remain legal without B
// fields. Preview must model the pending origin while retaining all old as_ofs.
func TestContentExecutionContextExistingCaseOriginsRemainLegalWithoutB(t *testing.T) {
	f := newScopedContentFixture(t)
	c := contentContextCommand(t, f)
	var created workspace.ReviewCreationReceipt
	if err := json.Unmarshal(f.must(t, "POST", "/api/v1/security/reviews", c, 200, nil), &created); err != nil {
		t.Fatal(err)
	}
	originalAsOf := c.ExecutionContext.ExecutionAsOf
	for _, fresh := range []bool{false, true} {
		var record workspace.CaseRecord
		if err := json.Unmarshal(f.must(t, "GET", "/api/v1/cases/"+created.CaseID, nil, 200, nil), &record); err != nil {
			t.Fatal(err)
		}
		c.NewCase = nil
		c.CaseID = created.CaseID
		c.ExpectedCaseVersion = record.Case.Version
		c.OperationID = randomTestUUID(t)
		if fresh {
			var now time.Time
			if err := f.admin.QueryRowContext(f.ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			f.apiOffset = now.Sub(f.base)
			f.mu.Unlock()
			input := httpReadingRequest(f.sourceID)
			input.Filters.SourceProjects = []string{"project-" + f.collection.ProjectID}
			var cost workbench.Reading
			if err := json.Unmarshal(f.must(t, "POST", "/api/v1/workbench/readings", input, 201, nil), &cost); err != nil {
				t.Fatal(err)
			}
			if cost.Cutoff.Equal(originalAsOf) {
				t.Fatal("second reading must have a distinct cutoff")
			}
			next := *c.ExecutionContext
			next.WorkbenchReadingID = cost.ID
			next.ExecutionAsOf = cost.Cutoff
			c.ExecutionContext = &next
		}
		before := contentContextSnapshot(t, f)
		var basis workspace.ReviewBasis
		if err := json.Unmarshal(f.must(t, "POST", "/api/v1/security/reviews/preview", c, 200, nil), &basis); err != nil {
			t.Fatal(err)
		}
		if contentContextSnapshot(t, f) != before {
			t.Fatal("preview wrote durable rows")
		}
		c.ExpectedBasisDigest = basis.Digest
		var next workspace.ReviewCreationReceipt
		if err := json.Unmarshal(f.must(t, "POST", "/api/v1/security/reviews", c, 200, nil), &next); err != nil {
			t.Fatal(err)
		}
		if next.CaseID != created.CaseID {
			t.Fatal("created another case")
		}
	}
	var record workspace.CaseRecord
	if err := json.Unmarshal(f.must(t, "GET", "/api/v1/cases/"+created.CaseID, nil, 200, nil), &record); err != nil || len(record.Executions) != 2 {
		t.Fatal("expected both exact origins", err, record)
	}
	f.assertI01WithoutB(t)
}
