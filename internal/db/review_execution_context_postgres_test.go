package db_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workchain"
	"github.com/foigent/x-insight/internal/workspace"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"os"
	"strings"
	"testing"
	"time"
)

// Real PostgreSQL only; newSourceFixture skips before opening a database when
// XINSIGHT_POSTGRES_DSN is absent. These tests are compile-only in M4 review.
type executionReviewFixture struct {
	f       *sourceFixture
	scoped  *db.ScopedWorkchainStore
	command workspace.ReviewCreationCommand
	cost    workbench.Reading
	runtime workbench.RuntimeReading
	actor   workspace.Actor
	record  workbench.ResourceAccessRecord
}

func newExecutionReviewFixture(t *testing.T) *executionReviewFixture {
	t.Helper()
	return newExecutionReviewFixtureWithFields(t, workchain.Fields{Context: true, Identity: true, Cost: true})
}

func newExecutionReviewFixtureWithFields(t *testing.T, fields workchain.Fields) *executionReviewFixture {
	t.Helper()
	f := newSourceFixture(t)
	a := f.documents[sourcefacts.ContextProfile]
	known := runtimeGolden(t)[0]
	known.SourceInstance, known.OrgID, known.ProjectID, known.RequestID, known.SubjectID = a.SourceInstance, a.OrgID, a.ProjectID, a.RequestID, a.RequestID
	known.OccurredAt, known.RecordedAt = a.OccurredAt, a.RecordedAt
	var destination sourcefacts.Observation
	for _, v := range runtimeGolden(t) {
		if v.Profile == sourcefacts.DestinationProfile {
			destination = v
			break
		}
	}
	putRuntimeDocuments(t, f, f.source, known)
	f.money()
	f.exec(`UPDATE workbench_access_grants SET allow_source_context=$2,allow_source_identity=$3,allow_source_cost=$4 WHERE subject_id=$1`, f.subject, fields.Context, fields.Identity, fields.Cost)
	f.exec(`UPDATE workbench_access_grants SET allow_source_runtime=true WHERE subject_id=$1`, f.subject)
	// The resource reporter has its own registered synthetic-resource identity;
	// the existing UniRouter source remains the observed execution target.
	reporter := testUUID(t)
	record := resourceForRuntime(f.source, known, destination)
	record.Input = nil
	record.Reporter.SourceSystemID = reporter
	raw, _ := json.Marshal(record)
	obj, e := connector.DecodeStrictJSONObject(raw)
	if e != nil {
		t.Fatal(e)
	}
	raw, e = connector.CanonicalizeJSONValue(obj)
	if e != nil {
		t.Fatal(e)
	}
	importResourceReviewRecord(t, f, raw, time.Now().UTC())
	f.exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_resource) VALUES($1,$2::uuid,$3,true)`, f.subject, reporter, "project-"+record.Reporter.ProjectID)
	ref := workspace.ResourceChoice{Scope: "finding", Reporter: record.Reporter, Target: record.Target, RequestID: record.RequestID, RecordRef: record.RecordRef, Revision: record.Revision, RecordHash: sourcefacts.Hash(raw), Relation: "supports"}
	cost := f.reading()
	runtime, e := f.workspace.CreateRuntimeReading(f.ctx, f.subject, runtimeRequestInput(t, known), time.Now().UTC())
	if e != nil {
		t.Fatal(e)
	}
	var execution string
	if e = f.admin.QueryRowContext(f.ctx, `SELECT execution_id FROM workbench_economic_facts WHERE source_system_id=$1`, f.source).Scan(&execution); e != nil {
		t.Fatal(e)
	}
	link, e := f.workspace.GetRuntimeExecutionRequest(f.ctx, f.subject, runtime.ID, execution, time.Now().UTC())
	if e != nil {
		t.Fatal(e)
	}
	actor, _ := workspace.HumanActor(f.subject)
	command := executionReviewCommand(testUUID(t), ref, cost, runtime, execution, link.Request.Ref)
	preflight := command
	preflight.ExpectedBasisDigest = "sha256:" + strings.Repeat("0", 64)
	if _, err := workspace.NormalizeReviewCreation(preflight); err != nil {
		_, contextErr := workspace.NormalizeExecutionContext(*command.ExecutionContext)
		var storedCutoff time.Time
		readErr := f.admin.QueryRowContext(f.ctx, `SELECT cutoff FROM workbench_readings WHERE id=$1`, cost.ID).Scan(&storedCutoff)
		t.Fatalf("fixture command rejected before preview: error=%v context_error=%v initial_cost_cutoff=%s submicro_nanoseconds=%d stored_cost_cutoff=%s stored_read_error=%v", err, contextErr, cost.Cutoff.Format(time.RFC3339Nano), cost.Cutoff.Nanosecond()%1000, storedCutoff.Format(time.RFC3339Nano), readErr)
	}
	x := &executionReviewFixture{f: f, scoped: db.NewScopedWorkchainStore(f.evidence, f.workspace), command: command, cost: cost, runtime: runtime, actor: actor, record: record}
	if !fields.Complete() {
		x.command.ExpectedBasisDigest = preflight.ExpectedBasisDigest
		return x
	}
	x.scope(t, func(ctx context.Context) {
		b, e := f.workspace.PreviewReviewCreation(ctx, command)
		if e != nil {
			t.Fatal("context preview", e)
		}
		if b.ResolvedExecutionContext == nil || !b.ResolvedExecutionContext.RuntimeCutoff.Equal(runtime.Cutoff) || !b.ResolvedExecutionContext.Context.ExecutionAsOf.Equal(cost.Cutoff) {
			t.Fatal("two cutoffs lost", b)
		}
		x.command.ExpectedBasisDigest = b.Digest
	})
	return x
}

// Shared by the real PG fixture and the offline normalization diagnostic.
func executionReviewCommand(operationID string, ref workspace.ResourceChoice, cost workbench.Reading, runtime workbench.RuntimeReading, execution, requestRef string) workspace.ReviewCreationCommand {
	return workspace.ReviewCreationCommand{Operation: "start_review", OperationID: operationID, NewCase: &workspace.CreateCaseInput{Title: "Execution resource review"}, Finding: workspace.CreateFindingInput{Title: "Resource claim", Statement: "Only this immutable resource revision", Classification: "hypothesis"}, Selection: workspace.BasisSelection{Resources: []workspace.ResourceChoice{ref}}, Review: &workspace.ReviewPayload{Conclusion: "supported", Reason: "Read selected resource"}, Reason: "Start bounded investigation", ExecutionContext: &workspace.ExecutionContext{WorkbenchReadingID: cost.ID, ExecutionID: execution, ExecutionAsOf: cost.Cutoff, RuntimeReadingID: runtime.ID, RuntimeRequestRef: requestRef}}
}

func (x *executionReviewFixture) scope(t *testing.T, fn func(context.Context)) {
	t.Helper()
	ctx, finish, e := x.scoped.Begin(x.f.ctx, x.f.subject, true, func(commit func() error) error { return commit() })
	if e != nil {
		t.Fatal(e)
	}
	defer finish()
	fn(ctx)
}
func (x *executionReviewFixture) snapshot(t *testing.T) string {
	t.Helper()
	var parts []string
	for _, table := range []string{"investigation_cases", "case_findings", "case_resource_links", "case_executions", "case_evidence_links", "case_gap_links", "case_finding_assessments", "case_finding_assessment_actions", "finding_review_creations", "investigation_case_scope_labels"} {
		var v string
		if e := x.f.admin.QueryRowContext(x.f.ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text FROM `+table+` t`).Scan(&v); e != nil {
			t.Fatal(e)
		}
		parts = append(parts, v)
	}
	return sourcefacts.Hash([]byte(strings.Join(parts, "\n")))
}
func (x *executionReviewFixture) create(t *testing.T, c workspace.ReviewCreationCommand) workspace.ReviewCreationReceipt {
	t.Helper()
	var got workspace.ReviewCreationReceipt
	x.scope(t, func(ctx context.Context) {
		var e error
		got, e = x.f.workspace.CreateReview(ctx, x.actor, c, time.Now().UTC(), nil)
		if e != nil {
			t.Fatal(e)
		}
	})
	return got
}

// Test-only fault injection, NOT a supported production cleanup operation.
// The four immutable triggers may be disabled only in this factory-owned
// disposable database and must be ENABLE ALWAYS again before the same
// transaction commits. Any assertion/error rolls back the DDL and deletes.
func injectExecutionReviewReadingAbsence(t *testing.T, x *executionReviewFixture) {
	t.Helper()
	fixtureConfig, err := pgx.ParseConfig(x.f.dsn)
	if err != nil {
		t.Fatal("cannot resolve fixture database identity")
	}
	baseConfig, err := pgx.ParseConfig(os.Getenv("XINSIGHT_POSTGRES_DSN"))
	if err != nil {
		t.Fatal("cannot resolve base database identity")
	}
	name := fixtureConfig.Database
	suffix := strings.TrimPrefix(name, "migration_test_")
	if name == baseConfig.Database || !strings.HasPrefix(name, "migration_test_") || len(suffix) != 32 || strings.Trim(suffix, "0123456789abcdef") != "" {
		t.Fatal("absence injection requires the exact factory-created database, never the base database")
	}
	tx, err := x.f.admin.BeginTx(x.f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var current string
	if err = tx.QueryRowContext(x.f.ctx, `SELECT current_database()`).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if current != name || current == baseConfig.Database {
		t.Fatal("absence injection database identity mismatch")
	}
	// These literal pairs are the entire approved exception; no source documents,
	// purpose revisions, durable links or receipts are deletion targets.
	triggers := []struct{ table, trigger string }{
		{"workbench_source_members", "workbench_source_members_immutable"},
		{"workbench_source_manifests", "workbench_source_manifests_immutable"},
		{"workbench_purpose_members", "workbench_purpose_members_immutable"},
		{"workbench_purpose_manifests", "workbench_purpose_manifests_immutable"},
	}
	for _, v := range triggers {
		var enabled bool
		if err = tx.QueryRowContext(x.f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_trigger WHERE tgrelid=('public.' || $1)::regclass AND tgname=$2 AND NOT tgisinternal AND tgenabled='A')`, v.table, v.trigger).Scan(&enabled); err != nil {
			t.Fatal(err)
		}
		if !enabled {
			t.Fatal("expected immutable trigger is not ENABLE ALWAYS", v.trigger)
		}
		if _, err = tx.ExecContext(x.f.ctx, "ALTER TABLE public."+v.table+" DISABLE TRIGGER "+v.trigger); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.ExecContext(x.f.ctx, "DELETE FROM public."+v.table+" WHERE reading_id=$1", x.cost.ID); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []struct{ table, id string }{{"workbench_readings", x.cost.ID}, {"runtime_readings", x.runtime.ID}} {
		result, err := tx.ExecContext(x.f.ctx, "DELETE FROM public."+v.table+" WHERE id=$1 AND owner_subject=$2", v.id, x.f.subject)
		if err != nil {
			t.Fatal(err)
		}
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			t.Fatal("absence injection must delete exactly one owned reading", v.table, count, err)
		}
	}
	for _, v := range triggers {
		if _, err = tx.ExecContext(x.f.ctx, "ALTER TABLE public."+v.table+" ENABLE ALWAYS TRIGGER "+v.trigger); err != nil {
			t.Fatal(err)
		}
		var enabled bool
		if err = tx.QueryRowContext(x.f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_trigger WHERE tgrelid=('public.' || $1)::regclass AND tgname=$2 AND NOT tgisinternal AND tgenabled='A')`, v.table, v.trigger).Scan(&enabled); err != nil {
			t.Fatal(err)
		}
		if !enabled {
			t.Fatal("immutable trigger was not restored before commit", v.trigger)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var absent bool
	if err = x.f.admin.QueryRowContext(x.f.ctx, `SELECT NOT EXISTS(SELECT 1 FROM workbench_readings WHERE id=$1) AND NOT EXISTS(SELECT 1 FROM runtime_readings WHERE id=$2)`, x.cost.ID, x.runtime.ID).Scan(&absent); err != nil {
		t.Fatal(err)
	}
	if !absent {
		t.Fatal("reading absence injection left a parent reading")
	}
}

func TestReviewExecutionContextAtomicDurableReplayAndRestore(t *testing.T) {
	x := newExecutionReviewFixture(t)
	created := x.create(t, x.command)
	x.scope(t, func(ctx context.Context) {
		record, e := x.f.workspace.GetCase(ctx, created.CaseID)
		if e != nil || len(record.Executions) != 1 || len(record.Resources) != 1 || len(record.Evidence) != 0 || len(record.Gaps) != 0 || record.Case.Version != 2 || !record.Executions[0].AsOf.Equal(x.cost.Cutoff) {
			t.Fatal("atomic relation/version/no inferred evidence", record, e)
		}
	})
	before := x.snapshot(t)
	// The supported read entries reject at their exact TTL boundary. This
	// advances only the injected read clock, without changing frozen records.
	if _, err := x.f.workspace.GetWorkbenchReading(x.f.ctx, x.f.subject, x.cost.ID, x.cost.ExpiresAt); !errors.Is(err, workbench.ErrExpired) {
		t.Fatal("expired cost reading remained readable", err)
	}
	if _, err := x.f.workspace.GetRuntimeReading(x.f.ctx, x.f.subject, x.runtime.ID, x.runtime.ExpiresAt); !errors.Is(err, workbench.ErrExpired) {
		t.Fatal("expired runtime reading remained readable", err)
	}
	injectExecutionReviewReadingAbsence(t, x)
	if x.snapshot(t) != before {
		t.Fatal("reading absence injection changed durable case or receipt")
	}
	replay := x.create(t, x.command)
	if !workspace.SameAssessmentValue(created, replay) || x.snapshot(t) != before {
		t.Fatal("receipt replay mutated durable case")
	}
	x.scope(t, func(ctx context.Context) {
		changed := x.command
		v := *changed.ExecutionContext
		v.ExecutionAsOf = v.ExecutionAsOf.Add(time.Microsecond)
		changed.ExecutionContext = &v
		if _, e := x.f.workspace.CreateReview(ctx, x.actor, changed, time.Now().UTC(), nil); !errors.Is(e, workspace.ErrConflict) {
			t.Fatal("same operation changed context", e)
		}
		view, e := x.f.workspace.GetFindingAssessment(ctx, created.CaseID, created.FindingID, time.Now().UTC())
		if e != nil || view.ReviewBasis.State != "unchanged" || view.ReviewBasis.Current == nil || view.ReviewBasis.Current.Digest != created.Basis.Digest {
			t.Fatal("expired origin became unavailable", view, e)
		}
		for _, op := range []string{"record_review", "record_progress"} {
			view, e = x.f.workspace.GetFindingAssessment(ctx, created.CaseID, created.FindingID, time.Now().UTC())
			if e != nil {
				t.Fatal(e)
			}
			c := workspace.AssessmentCommand{Operation: op, OperationID: testUUID(t), ExpectedVersion: view.Assessment.Version, ExpectedCaseVersion: view.CaseVersion, ExpectedBasisDigest: created.Basis.Digest, Selection: created.Basis.Selected}
			if op == "record_review" {
				c.Review = &workspace.ReviewPayload{Conclusion: "supported", Reason: "Rechecked retained revision"}
			} else {
				c.Progress = &workspace.ProgressPayload{State: "in_progress", Reason: "Continue bounded review"}
			}
			action, e := x.f.workspace.ApplyFindingAssessment(ctx, created.CaseID, created.FindingID, x.actor, c, time.Now().UTC())
			if e != nil || !workspace.SameAssessmentValue(action.Basis.ResolvedExecutionContext, created.Basis.ResolvedExecutionContext) {
				t.Fatal("durable follow-up", op, e)
			}
		}
		if _, e = x.f.workspace.SealCase(ctx, created.CaseID, x.actor, time.Now().UTC()); e != nil {
			t.Fatal("seal after reading absence injection", e)
		}
	})
	if e := db.ValidateFindingAssessmentRestore(x.f.ctx, x.f.admin); e != nil {
		t.Fatal("historical restore", e)
	}
	// Revoke the exact resource-only reporter grant; removing its sole
	// capability would violate the grant contract instead of testing access.
	x.f.exec(`UPDATE workbench_access_grants SET active=false WHERE subject_id=$1 AND source_system_id=$2::uuid AND source_project_ref=$3 AND cost_project_ref IS NULL`, x.f.subject, x.record.Reporter.SourceSystemID, "project-"+x.record.Reporter.ProjectID)
	before = x.snapshot(t)
	x.scope(t, func(ctx context.Context) {
		if _, e := x.f.workspace.CreateReview(ctx, x.actor, x.command, time.Now().UTC(), nil); !errors.Is(e, workspace.ErrNotFound) {
			t.Fatal("replay ignored current grants", e)
		}
	})
	if x.snapshot(t) != before {
		t.Fatal("denied replay wrote")
	}
	if e := db.ValidateFindingAssessmentRestore(x.f.ctx, x.f.admin); e != nil {
		t.Fatal("historical validation depended on current actor grants", e)
	}
}
func TestReviewExecutionContextFailuresLeaveNoPartialRows(t *testing.T) {
	for _, name := range []string{"cost_owner", "runtime_owner", "cost_expired", "runtime_expired", "cost_nonmember", "wrong_execution", "wrong_asof", "wrong_request", "resource_hash", "resource_tuple", "resource_nonmember", "late_revision", "ambiguous_execution", "grant_changed", "runtime_changed", "session_final_revoke"} {
		t.Run(name, func(t *testing.T) {
			x := newExecutionReviewFixture(t)
			c := x.command
			v := *c.ExecutionContext
			c.ExecutionContext = &v
			c.Selection.Resources = append([]workspace.ResourceChoice{}, c.Selection.Resources...)
			switch name {
			case "cost_owner":
				x.f.exec(`UPDATE workbench_readings SET owner_subject='xs_bbbbbbbbbbbbbbbb' WHERE id=$1`, x.cost.ID)
			case "runtime_owner":
				x.f.exec(`UPDATE runtime_readings SET owner_subject='xs_bbbbbbbbbbbbbbbb' WHERE id=$1`, x.runtime.ID)
			case "cost_expired":
				x.f.exec(`UPDATE workbench_readings SET expires_at=clock_timestamp() WHERE id=$1`, x.cost.ID)
			case "runtime_expired":
				x.f.exec(`UPDATE runtime_readings SET expires_at=clock_timestamp() WHERE id=$1`, x.runtime.ID)
			case "cost_nonmember":
				x.f.exec(`DELETE FROM workbench_reading_members WHERE reading_id=$1`, x.cost.ID)
			case "wrong_execution":
				v.ExecutionID = "xe1_" + strings.Repeat("f", 64)
			case "wrong_asof":
				v.ExecutionAsOf = v.ExecutionAsOf.Add(time.Microsecond)
			case "wrong_request":
				v.RuntimeRequestRef = "xrr_" + strings.Repeat("f", 64)
			case "resource_hash":
				c.Selection.Resources[0].RecordHash = "sha256:" + strings.Repeat("f", 64)
			case "resource_tuple":
				c.Selection.Resources[0].Target.OrgID = "999"
			case "resource_nonmember":
				x.f.exec(`DELETE FROM runtime_reading_resource_members WHERE reading_id=$1`, x.runtime.ID)
			case "late_revision":
				revised := x.record
				revised.Revision++
				revised.PreviousHash = c.Selection.Resources[0].RecordHash
				raw, _ := json.Marshal(revised)
				obj, e := connector.DecodeStrictJSONObject(raw)
				if e != nil {
					t.Fatal(e)
				}
				raw, e = connector.CanonicalizeJSONValue(obj)
				if e != nil {
					t.Fatal(e)
				}
				importResourceReviewRecord(t, x.f, raw, time.Now().UTC())
				c.Selection.Resources[0].Revision = revised.Revision
				c.Selection.Resources[0].RecordHash = sourcefacts.Hash(raw)
			case "ambiguous_execution":
				a := x.f.documents[sourcefacts.ContextProfile]
				other := runtimeGolden(t)[0]
				other.SourceInstance, other.OrgID, other.ProjectID, other.RequestID, other.SubjectID = "ur-other-instance", a.OrgID, a.ProjectID, a.RequestID, a.RequestID
				other.OccurredAt, other.RecordedAt = a.OccurredAt, a.RecordedAt
				putRuntimeDocuments(t, x.f, x.f.source, other)
				mapped, e := unirouter.MapCompletedExecutionV1(x.f.i01, unirouter.CompletedExecutionMapOptions{SourceInstance: other.SourceInstance, SourceSystemID: x.f.source, AdapterManifestID: unirouter.DefaultCompletedExecutionAdapterManifestID, ObservedAt: time.Now().UTC(), IngestedAt: time.Now().UTC()})
				if e != nil {
					t.Fatal(e)
				}
				if _, e = x.f.evidence.PersistEvents(x.f.ctx, mapped.Events); e != nil {
					t.Fatal(e)
				}
				reading, e := x.f.workspace.CreateRuntimeReading(x.f.ctx, x.f.subject, runtimeRequestInput(t, other), time.Now().UTC())
				if e != nil {
					t.Fatal(e)
				}
				if _, e = x.f.workspace.GetRuntimeExecutionRequest(x.f.ctx, x.f.subject, reading.ID, v.ExecutionID, time.Now().UTC()); !errors.Is(e, workbench.ErrConflict) {
					t.Fatal("fixture must have ambiguous execution/request mapping", e)
				}
				v.RuntimeReadingID = reading.ID
			case "grant_changed":
				x.f.exec(`UPDATE workbench_access_grants SET active=false WHERE subject_id=$1 AND source_system_id=$2::uuid AND source_project_ref=$3 AND cost_project_ref IS NULL`, x.f.subject, x.record.Reporter.SourceSystemID, "project-"+x.record.Reporter.ProjectID)
			case "runtime_changed":
				x.f.exec(`UPDATE runtime_readings SET membership_sha256='sha256:'||repeat('f',64) WHERE id=$1`, x.runtime.ID)
			}
			before := x.snapshot(t)
			if name == "session_final_revoke" {
				ctx, finish, e := x.scoped.Begin(x.f.ctx, x.f.subject, true, func(func() error) error { return workspace.ErrNotFound })
				if e != nil {
					t.Fatal(e)
				}
				_, e = x.f.workspace.CreateReview(ctx, x.actor, c, time.Now().UTC(), nil)
				_ = finish()
				if e == nil {
					t.Fatal("final session revoke accepted")
				}
			} else {
				x.scope(t, func(ctx context.Context) {
					if _, e := x.f.workspace.CreateReview(ctx, x.actor, c, time.Now().UTC().Add(-time.Hour), nil); e == nil {
						t.Fatal("invalid context accepted")
					}
				})
			}
			if x.snapshot(t) != before {
				t.Fatal("partial case/finding/link/action/label/receipt write")
			}
		})
	}
}
func TestReviewExecutionContextResourceOnlyRemainsLegal(t *testing.T) {
	x := newExecutionReviewFixture(t)
	c := x.command
	c.ExecutionContext = nil
	x.scope(t, func(ctx context.Context) {
		b, e := x.f.workspace.PreviewReviewCreation(ctx, c)
		if e != nil {
			t.Fatal(e)
		}
		if b.ResolvedExecutionContext != nil {
			t.Fatal("invented context")
		}
		c.ExpectedBasisDigest = b.Digest
	})
	r := x.create(t, c)
	x.scope(t, func(ctx context.Context) {
		record, e := x.f.workspace.GetCase(ctx, r.CaseID)
		if e != nil || len(record.Executions) != 0 || len(record.Resources) != 1 {
			t.Fatal(record, e)
		}
	})
}

// Observe the actual PostgreSQL wait, then let database time pass a shortened
// fixture deadline. No production hook, mocked transaction or caller clock.
func waitExecutionReviewLock(t *testing.T, x *executionReviewFixture, blocker *sql.Tx, pattern string) {
	t.Helper()
	var pid int
	if e := blocker.QueryRowContext(x.f.ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	until := time.Now().Add(8 * time.Second)
	for time.Now().Before(until) {
		var waiting bool
		e := x.f.admin.QueryRowContext(x.f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) AND query LIKE $2)`, pid, pattern).Scan(&waiting)
		if e != nil {
			t.Fatal(e)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("creation never reached expected database lock")
}
func TestReviewExecutionContextExpiresDuringLockWait(t *testing.T) {
	for _, kind := range []string{"creation_advisory", "scope_label"} {
		t.Run(kind, func(t *testing.T) {
			x := newExecutionReviewFixture(t)
			c := x.command
			if kind == "scope_label" {
				origin := c
				origin.ExecutionContext = nil
				origin.OperationID = testUUID(t)
				x.scope(t, func(ctx context.Context) {
					b, e := x.f.workspace.PreviewReviewCreation(ctx, origin)
					if e != nil {
						t.Fatal(e)
					}
					origin.ExpectedBasisDigest = b.Digest
				})
				base := x.create(t, origin)
				c.CaseID = base.CaseID
				c.NewCase = nil
				c.ExpectedCaseVersion = base.CaseVersion
				x.scope(t, func(ctx context.Context) {
					b, e := x.f.workspace.PreviewReviewCreation(ctx, c)
					if e != nil {
						t.Fatal(e)
					}
					c.ExpectedBasisDigest = b.Digest
				})
			}
			before := x.snapshot(t)
			blocker, e := x.f.admin.BeginTx(x.f.ctx, nil)
			if e != nil {
				t.Fatal(e)
			}
			defer blocker.Rollback()
			pattern := "SELECT pg_advisory_xact_lock%"
			if kind == "creation_advisory" {
				h := sha256.Sum256([]byte("xinsight.review-creation.v1\x00" + x.actor.Ref + "\x00" + c.Operation + "\x00" + c.OperationID))
				_, e = blocker.ExecContext(x.f.ctx, `SELECT pg_advisory_xact_lock($1)`, int64(binary.BigEndian.Uint64(h[:8])))
			} else {
				_, e = blocker.ExecContext(x.f.ctx, `SELECT case_id FROM investigation_case_scope_labels WHERE case_id=$1 FOR UPDATE`, c.CaseID)
				pattern = "INSERT INTO investigation_case_scope_labels%"
			}
			if e != nil {
				t.Fatal(e)
			}
			// Dedicated scope for the in-flight request; do not call testing.Fatal in it.
			ctx, finish, e := x.scoped.Begin(x.f.ctx, x.f.subject, true, func(commit func() error) error { return commit() })
			if e != nil {
				t.Fatal(e)
			}
			defer finish()
			done := make(chan error, 1)
			go func() {
				_, err := x.f.workspace.CreateReview(ctx, x.actor, c, time.Now().UTC().Add(-time.Hour), nil)
				done <- err
			}()
			waitExecutionReviewLock(t, x, blocker, pattern)
			x.f.exec(`UPDATE workbench_readings SET expires_at=clock_timestamp()+interval '100 milliseconds' WHERE id=$1`, x.cost.ID)
			// Confirm real server time crossed the fixture expiration before release.
			if e = x.f.admin.QueryRowContext(x.f.ctx, `SELECT pg_sleep(0.15)`).Scan(new(any)); e != nil {
				t.Fatal(e)
			}
			var expired bool
			if e = x.f.admin.QueryRowContext(x.f.ctx, `SELECT expires_at<=clock_timestamp() FROM workbench_readings WHERE id=$1`, x.cost.ID).Scan(&expired); e != nil || !expired {
				t.Fatal("deadline not crossed", e)
			}
			if e = blocker.Rollback(); e != nil {
				t.Fatal(e)
			}
			select {
			case e = <-done:
				if !errors.Is(e, workspace.ErrConflict) {
					t.Fatal("expired wait committed or wrong error", e)
				}
			case <-time.After(8 * time.Second):
				t.Fatal("request stuck after releasing lock")
			}
			if x.snapshot(t) != before {
				t.Fatal("late expiration left partial writes")
			}
		})
	}
}

// No database: exercise the exact command constructor used by the PG fixture
// against the current strict preview/create input contract. The real fixture
// intentionally retains the returned cutoff unchanged so this mismatch cannot
// be hidden by a test-only truncation.
func TestReviewExecutionContextFixtureInputPrecision(t *testing.T) {
	scope := workbench.ResourceScope{SourceSystemID: "11111111-1111-4111-8111-111111111111", SourceInstance: "ur-demo-01", OrgID: "17", ProjectID: "101"}
	reporter := scope
	reporter.SourceSystemID = "22222222-2222-4222-8222-222222222222"
	ref := workspace.ResourceChoice{Scope: "finding", Reporter: reporter, Target: scope, RequestID: "request-one", RecordRef: "enterprise-read", Revision: 1, RecordHash: "sha256:" + strings.Repeat("a", 64), Relation: "supports"}
	at := time.Date(2026, 9, 21, 1, 2, 3, 123456789, time.UTC)
	codec := pgtype.NewMap()
	encoded, err := codec.Encode(pgtype.TimestamptzOID, pgtype.BinaryFormatCode, at, nil)
	if err != nil {
		t.Fatal(err)
	}
	var stored time.Time
	if err = codec.Scan(pgtype.TimestamptzOID, pgtype.BinaryFormatCode, encoded, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Equal(at) || !stored.Equal(at.Truncate(time.Microsecond)) {
		t.Fatal("unexpected actual pgx timestamp precision", at, stored)
	}
	runtime := workbench.RuntimeReading{ID: "xrt_" + strings.Repeat("b", 22), Cutoff: at.Truncate(time.Microsecond)}
	for _, precision := range []string{"nanosecond_creation_response", "microsecond_persisted_read"} {
		t.Run(precision, func(t *testing.T) {
			cutoff := at
			if precision == "microsecond_persisted_read" {
				cutoff = stored
			}
			cost := workbench.Reading{ID: "xwr_" + strings.Repeat("c", 22), Cutoff: cutoff}
			c := executionReviewCommand("33333333-3333-4333-8333-333333333333", ref, cost, runtime, "xe1_"+strings.Repeat("d", 64), "xrr_"+strings.Repeat("e", 64))
			c.ExpectedBasisDigest = "sha256:" + strings.Repeat("0", 64) // same placeholder as PreviewReviewCreation
			_, contextErr := workspace.NormalizeExecutionContext(*c.ExecutionContext)
			_, commandErr := workspace.NormalizeReviewCreation(c)
			if precision == "nanosecond_creation_response" {
				if !errors.Is(contextErr, workspace.ErrInvalid) || !errors.Is(commandErr, workspace.ErrInvalid) {
					t.Fatal("expected exact strict precision rejection", contextErr, commandErr)
				}
				c.ExecutionContext = nil
				if _, err := workspace.NormalizeReviewCreation(c); err != nil {
					t.Fatal("resource-only command independently invalid", err)
				}
				t.Log("the otherwise valid shared fixture command is rejected specifically by NormalizeExecutionContext submicrosecond guard")
			} else if contextErr != nil || commandErr != nil {
				t.Fatal("microsecond context/preview/create input invalid", contextErr, commandErr)
			}
		})
	}
}

// Exercise the real PG store with a deterministic submicrosecond creation clock.
// The HTTP test separately checks the exact POST/GET wire contract.
func TestReviewExecutionContextCostReadingPrecision(t *testing.T) {
	f := newSourceFixture(t)
	f.money()
	f.allow()
	now := time.Now().UTC().Truncate(time.Second).Add(123456789 * time.Nanosecond)
	for _, mode := range []string{"relative", "absolute"} {
		t.Run(mode, func(t *testing.T) {
			request := relativeWorkbenchRequest()
			if mode == "absolute" {
				request = f.request()
				start := request.Period.StartInclusive.Add(123456789 * time.Nanosecond)
				end := request.Period.EndExclusive.Add(987654321 * time.Nanosecond)
				request.Period.StartInclusive, request.Period.EndExclusive = &start, &end
			}
			period, err := request.Resolve(now)
			if err != nil {
				t.Fatal(err)
			}
			created, err := f.workspace.CreateWorkbenchReading(f.ctx, f.subject, request, now)
			if err != nil {
				t.Fatal(err)
			}
			read, err := f.workspace.GetWorkbenchReading(f.ctx, f.subject, created.ID, now)
			if err != nil {
				t.Fatal(err)
			}
			for _, pair := range [][3]time.Time{
				{created.Cutoff, read.Cutoff, now.Truncate(time.Microsecond)},
				{created.ExpiresAt, read.ExpiresAt, now.Truncate(time.Microsecond).Add(workbench.ReadingTTL)},
				{created.Period.StartInclusive, read.Period.StartInclusive, period.StartInclusive.Truncate(time.Microsecond)},
				{created.Period.EndExclusive, read.Period.EndExclusive, period.EndExclusive.Truncate(time.Microsecond)},
			} {
				if !pair[0].Equal(pair[1]) || !pair[0].Equal(pair[2]) {
					t.Fatalf("creation/read/expected timestamp mismatch: %v", pair)
				}
			}
			if created.Period.Timezone != read.Period.Timezone || created.MembershipRef != read.MembershipRef || created.SourceManifestRef != read.SourceManifestRef {
				t.Fatal("read changed frozen identity", created, read)
			}
			_, err = workspace.NormalizeExecutionContext(workspace.ExecutionContext{WorkbenchReadingID: created.ID, ExecutionID: "xe1_" + strings.Repeat("a", 64), ExecutionAsOf: created.Cutoff, RuntimeReadingID: "xrt_" + strings.Repeat("b", 22), RuntimeRequestRef: "xrr_" + strings.Repeat("c", 64)})
			if err != nil {
				t.Fatal("initial cutoff cannot enter strict context", err)
			}
		})
	}
	t.Run("collapsed_period_without_writes", func(t *testing.T) {
		snapshot := func() string {
			t.Helper()
			var state string
			if err := f.admin.QueryRowContext(f.ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id),'[]'::jsonb)::text FROM workbench_readings r`).Scan(&state); err != nil {
				t.Fatal(err)
			}
			return state
		}
		before := snapshot()
		request := relativeWorkbenchRequest()
		start := now.Add(-time.Hour).Truncate(time.Second).Add(100 * time.Nanosecond)
		end := start.Add(500 * time.Nanosecond)
		request.Period = workbench.PeriodInput{StartInclusive: &start, EndExclusive: &end, DisplayTimezone: "UTC"}
		if _, err := request.Resolve(now); err != nil {
			t.Fatal("collapse must reach database precision guard", err)
		}
		if _, err := f.workspace.CreateWorkbenchReading(f.ctx, f.subject, request, now); !errors.Is(err, workbench.ErrInvalid) {
			t.Fatal("collapsed period error", err)
		}
		if snapshot() != before {
			t.Fatal("collapsed period changed readings")
		}
	})
}

func TestReviewExecutionContextEmptyFollowupUsesExistingFindingScope(t *testing.T) {
	for _, mode := range []string{"resource_origin", "resource_only", "finding_without_creation_receipt"} {
		t.Run(mode, func(t *testing.T) {
			x := newExecutionReviewFixture(t)
			c := x.command
			if mode != "resource_origin" {
				c.ExecutionContext = nil
				x.scope(t, func(ctx context.Context) {
					b, err := x.f.workspace.PreviewReviewCreation(ctx, c)
					if err != nil {
						t.Fatal(err)
					}
					c.ExpectedBasisDigest = b.Digest
				})
			}
			created := x.create(t, c)
			findingID := created.FindingID
			x.scope(t, func(ctx context.Context) {
				if mode == "finding_without_creation_receipt" {
					finding, err := x.f.workspace.CreateFinding(ctx, created.CaseID, x.actor, workspace.CreateFindingInput{Title: "Existing finding", Statement: "Follow up within an authorized case", Classification: "hypothesis"}, time.Now().UTC(), nil)
					if err != nil {
						t.Fatal(err)
					}
					findingID = finding.ID
					var receipts int
					if err = x.f.admin.QueryRowContext(x.f.ctx, `SELECT count(*) FROM finding_review_creations WHERE case_id=$1 AND finding_id=$2`, created.CaseID, findingID).Scan(&receipts); err != nil || receipts != 0 {
						t.Fatal("fixture unexpectedly has a creation receipt", err)
					}
				}
				view, err := x.f.workspace.GetFindingAssessment(ctx, created.CaseID, findingID, time.Now().UTC())
				if err != nil {
					t.Fatal(err)
				}
				basis, err := x.f.workspace.PreviewFindingBasis(ctx, created.CaseID, findingID, workspace.BasisSelection{})
				if err != nil {
					t.Fatal("existing finding empty preview", err)
				}
				if len(basis.Selected.Evidence)+len(basis.Selected.Gaps)+len(basis.Selected.Resources)+len(basis.Selected.Contents) != 0 {
					t.Fatal("empty selection gained citations")
				}
				if !workspace.SameAssessmentValue(basis.ResolvedExecutionContext, created.Basis.ResolvedExecutionContext) {
					t.Fatal("follow-up changed original context")
				}
				command := workspace.AssessmentCommand{Operation: "record_progress", OperationID: testUUID(t), ExpectedVersion: view.Assessment.Version, ExpectedCaseVersion: view.CaseVersion, ExpectedBasisDigest: basis.Digest, Selection: basis.Selected, Progress: &workspace.ProgressPayload{State: "in_progress", Reason: "No new citations"}}
				if mode == "finding_without_creation_receipt" {
					if view.Assessment.Progress != nil || view.Assessment.Recorded {
						t.Fatal("fixture unexpectedly initialized assessment")
					}
					before := x.snapshot(t)
					if _, err = x.f.workspace.ApplyFindingAssessment(ctx, created.CaseID, findingID, x.actor, command, time.Now().UTC()); !errors.Is(err, workspace.ErrConflict) {
						t.Fatal("unrecorded progress skipped not_started", err)
					}
					if before != x.snapshot(t) {
						t.Fatal("rejected state transition mutated persisted state")
					}
					command.OperationID = testUUID(t)
					command.Progress.State = "not_started"
					initialized, err := x.f.workspace.ApplyFindingAssessment(ctx, created.CaseID, findingID, x.actor, command, time.Now().UTC())
					if err != nil || initialized.Current.Progress == nil || initialized.Current.Progress.State != "not_started" || initialized.Current.Review != nil {
						t.Fatal("empty follow-up initialization", err)
					}
					basis, err = x.f.workspace.PreviewFindingBasis(ctx, created.CaseID, findingID, workspace.BasisSelection{})
					if err != nil {
						t.Fatal("initialized empty preview", err)
					}
					command.OperationID = testUUID(t)
					command.ExpectedVersion = initialized.Current.Version
					command.ExpectedCaseVersion = initialized.CaseVersion
					command.ExpectedBasisDigest = basis.Digest
					command.Selection = basis.Selected
					command.Progress = &workspace.ProgressPayload{State: "in_progress", Reason: "Begin explicitly initialized follow-up"}
				}
				action, err := x.f.workspace.ApplyFindingAssessment(ctx, created.CaseID, findingID, x.actor, command, time.Now().UTC())
				if err != nil || action.Current.Progress == nil || action.Current.Progress.State != "in_progress" {
					t.Fatal("empty follow-up action", err)
				}
				before := x.snapshot(t)
				if _, err = x.f.workspace.PreviewFindingBasis(ctx, created.CaseID, "xf_"+strings.Repeat("A", 22), workspace.BasisSelection{}); !errors.Is(err, workspace.ErrNotFound) {
					t.Fatal("missing finding admitted", err)
				}
				if before != x.snapshot(t) {
					t.Fatal("missing finding mutated state")
				}
			})
			// The parent case still carries its resource scope even when this
			// finding has no creation receipt and selects no new citations.
			x.f.exec(`UPDATE workbench_access_grants SET active=false WHERE subject_id=$1 AND source_system_id=$2::uuid`, x.f.subject, x.record.Reporter.SourceSystemID)
			x.scope(t, func(ctx context.Context) {
				if _, err := x.f.workspace.PreviewFindingBasis(ctx, created.CaseID, findingID, workspace.BasisSelection{}); !errors.Is(err, workspace.ErrNotFound) {
					t.Fatal("empty follow-up ignored revoked parent case scope", err)
				}
			})
		})
	}
}

// Grants are fixed BEFORE readings are captured: access-fingerprint drift must
// not mask the admission regression as a stale-reading conflict.
func TestReviewExecutionContextMissingFieldsDeniedBeforePreviewAndCreate(t *testing.T) {
	for _, tt := range []struct {
		name   string
		fields workchain.Fields
	}{
		{"all", workchain.Fields{}},
		{"context", workchain.Fields{Identity: true, Cost: true}},
		{"identity", workchain.Fields{Context: true, Cost: true}},
		{"cost", workchain.Fields{Context: true, Identity: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			x := newExecutionReviewFixtureWithFields(t, tt.fields)
			before := x.snapshot(t)
			x.scope(t, func(ctx context.Context) {
				if _, err := x.f.workspace.PreviewReviewCreation(ctx, x.command); !errors.Is(err, workspace.ErrNotFound) {
					t.Fatalf("preview must deny incomplete fields, got %v", err)
				}
				if _, err := x.f.workspace.CreateReview(ctx, x.actor, x.command, time.Now().UTC(), nil); !errors.Is(err, workspace.ErrNotFound) {
					t.Fatalf("direct create must deny incomplete fields, got %v", err)
				}
			})
			if x.snapshot(t) != before {
				t.Fatal("denial wrote case/finding/link/action/label/receipt rows")
			}
			// Explicitly omitting context remains a legal resource-only review
			// with the SAME grant set and readings.
			c := x.command
			c.ExecutionContext = nil
			x.scope(t, func(ctx context.Context) {
				b, err := x.f.workspace.PreviewReviewCreation(ctx, c)
				if err != nil || b.ResolvedExecutionContext != nil {
					t.Fatalf("resource-only preview: %v", err)
				}
				c.ExpectedBasisDigest = b.Digest
			})
			r := x.create(t, c)
			x.scope(t, func(ctx context.Context) {
				record, err := x.f.workspace.GetCase(ctx, r.CaseID)
				if err != nil || len(record.Executions) != 0 || len(record.Resources) != 1 {
					t.Fatal("resource-only result", record, err)
				}
			})
		})
	}
}
