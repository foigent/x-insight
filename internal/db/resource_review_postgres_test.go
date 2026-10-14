package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workspace"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResourceReviewFullChainAndHistoricalRestore(t *testing.T) {
	for _, spelling := range []string{"normal", "empty_previous_hash", "utc_offset", "empty_owner_ref"} {
		t.Run(spelling, func(t *testing.T) { testResourceReviewFullChain(t, spelling) })
	}
}
func testResourceReviewFullChain(t *testing.T, spelling string) {
	f := newSourceFixture(t)
	gold := runtimeGolden(t)
	known := gold[0]
	var destination sourcefacts.Observation
	for _, d := range gold {
		if d.Profile == sourcefacts.DestinationProfile {
			destination = d
			break
		}
	}
	putRuntimeDocuments(t, f, f.source, known, destination)
	f.exec(`UPDATE workbench_access_grants SET allow_source_runtime=true WHERE subject_id=$1`, f.subject)
	reporter := testUUID(t)
	record := resourceForRuntime(f.source, known, destination)
	record.Input = nil
	record.Reporter.SourceSystemID = reporter
	now := time.Now().UTC().Truncate(time.Microsecond)
	raw, _ := json.Marshal(record)
	v, _ := connector.DecodeStrictJSONObject(raw)
	switch spelling {
	case "empty_previous_hash":
		v["previous_hash"] = ""
	case "utc_offset":
		v["occurred_at"] = strings.TrimSuffix(v["occurred_at"].(string), "Z") + "+00:00"
	case "empty_owner_ref":
		v["resource"].(map[string]any)["ownership"] = "unknown"
		v["resource"].(map[string]any)["owner_ref"] = ""
	}
	raw, _ = connector.CanonicalizeJSONValue(v)
	if _, err := workbench.DecodeResourceAccessForImport(raw); err != nil {
		t.Fatal("supported source representation", err)
	}
	importResourceReviewRecord(t, f, raw, now)
	ref := workspace.ResourceChoice{Scope: "finding", Reporter: record.Reporter, Target: record.Target, RequestID: record.RequestID, RecordRef: record.RecordRef, Revision: record.Revision, RecordHash: sourcefacts.Hash(raw), Relation: "supports"}
	actor, _ := workspace.HumanActor(f.subject)
	command := workspace.ReviewCreationCommand{Operation: "start_review", OperationID: testUUID(t), NewCase: &workspace.CreateCaseInput{Title: "Resource investigation"}, Finding: workspace.CreateFindingInput{Title: "Resource assertion", Statement: "The selected synthetic report supports this bounded assertion", Classification: "hypothesis"}, Selection: workspace.BasisSelection{Resources: []workspace.ResourceChoice{ref}}, Review: &workspace.ReviewPayload{Conclusion: "supported", Reason: "Inspected the named revision"}, Reason: "Resource cross-layer validation"}
	scoped := db.NewScopedWorkchainStore(f.evidence, f.workspace)
	withScope := func(fn func(context.Context)) {
		t.Helper()
		ctx, finish, err := scoped.Begin(f.ctx, f.subject, true, func(f func() error) error { return f() })
		if err != nil {
			t.Fatal(err)
		}
		defer finish()
		fn(ctx)
	}
	withScope(func(ctx context.Context) {
		if _, err := f.workspace.PreviewReviewCreation(ctx, command); !errors.Is(err, workspace.ErrNotFound) {
			t.Fatal("missing reporter grant", err)
		}
	})
	f.exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_resource) VALUES($1,$2,$3,true)`, f.subject, reporter, "project-"+record.Reporter.ProjectID)
	var created workspace.ReviewCreationReceipt
	withScope(func(ctx context.Context) {
		b, err := f.workspace.PreviewReviewCreation(ctx, command)
		if err != nil {
			t.Fatal("preview", err)
		}
		if b.Schema != workspace.ResourceReviewBasisSchema || len(b.AdoptedResources) != 1 {
			t.Fatal("resource basis missing")
		}
		command.ExpectedBasisDigest = b.Digest
		for _, change := range []func(*workspace.ResourceChoice){func(r *workspace.ResourceChoice) { r.RecordHash = "sha256:" + strings.Repeat("f", 64) }, func(r *workspace.ResourceChoice) { r.Target.OrgID = "999" }, func(r *workspace.ResourceChoice) { r.Reporter.ProjectID = "999" }} {
			bad := command
			bad.Selection.Resources = append([]workspace.ResourceChoice{}, command.Selection.Resources...)
			change(&bad.Selection.Resources[0])
			if _, e := f.workspace.PreviewReviewCreation(ctx, bad); e == nil {
				t.Fatal("cross-layer identity/hash mismatch accepted")
			}
		}
		created, err = f.workspace.CreateReview(ctx, actor, command, now, nil)
		if err != nil {
			t.Fatal("create", err)
		}
		replay, err := f.workspace.CreateReview(ctx, actor, command, now, nil)
		if err != nil || !workspace.SameAssessmentValue(replay, created) {
			t.Fatal("replay", err)
		}
	})
	// A malformed persisted classification must fail closed both online and
	// during restore; missing JSON fields produce SQL NULL rather than false.
	if err := db.ValidateWorkbenchRestore(f.ctx, f.admin); err != nil {
		t.Fatal("valid resource workbench restore", err)
	}
	var requirements []byte
	if err := f.admin.QueryRowContext(f.ctx, `SELECT requirements FROM investigation_case_scope_labels WHERE case_id=$1`, created.CaseID).Scan(&requirements); err != nil {
		t.Fatal(err)
	}
	for _, malformed := range []string{
		`SELECT jsonb_agg(req-'fields') FROM jsonb_array_elements($2::jsonb) req`,
		`SELECT jsonb_agg(jsonb_set(req,'{fields}','null'::jsonb)) FROM jsonb_array_elements($2::jsonb) req`,
	} {
		f.exec(`UPDATE investigation_case_scope_labels SET requirements=(`+malformed+`) WHERE case_id=$1`, created.CaseID, requirements)
		withScope(func(ctx context.Context) {
			if _, err := f.workspace.GetCase(ctx, created.CaseID); !errors.Is(err, workspace.ErrNotFound) {
				t.Fatal("malformed scope classification exposed case", err)
			}
			queue, err := f.workspace.ListInvestigationQueue(ctx, workspace.QueueAccess{Subject: f.subject, Revision: "malformed-resource-label"}, workspace.QueueRequest{Limit: 10}, now)
			if err != nil || len(queue.Items) != 0 {
				t.Fatal("malformed scope classification exposed queue", err)
			}
		})
		if err := db.ValidateWorkbenchRestore(f.ctx, f.admin); err == nil || !strings.Contains(err.Error(), "scoped case classification is invalid") {
			t.Fatal("restore accepted malformed scope classification", err)
		}
		f.exec(`UPDATE investigation_case_scope_labels SET requirements=$2::jsonb WHERE case_id=$1`, created.CaseID, requirements)
	}
	withScope(func(ctx context.Context) {
		c, err := f.workspace.GetCase(ctx, created.CaseID)
		if err != nil || len(c.Resources) != 1 {
			t.Fatal("case", err)
		}
		queue, err := f.workspace.ListInvestigationQueue(ctx, workspace.QueueAccess{Subject: f.subject, Revision: "resource-test"}, workspace.QueueRequest{Limit: 10}, now)
		if err != nil || len(queue.Items) != 1 || queue.Items[0].ReviewBasis != "unchanged" {
			t.Fatal("queue", err, queue)
		}
		view, err := f.workspace.GetFindingAssessment(ctx, created.CaseID, created.FindingID, now)
		if err != nil {
			t.Fatal("assessment", err)
		}
		b := created.Basis
		progress := workspace.AssessmentCommand{Operation: "record_progress", OperationID: testUUID(t), ExpectedVersion: 1, ExpectedCaseVersion: view.CaseVersion, ExpectedBasisDigest: b.Digest, Selection: b.Selected, Progress: &workspace.ProgressPayload{State: "ended", Reason: "Human verified", Closure: &workspace.Closure{Kind: "verified_remediation", Explanation: "Bounded synthetic check", Scope: "This resource report", VerifiedAt: &now, VerificationResources: []workspace.ResourceChoice{ref}}}}
		if _, err = f.workspace.ApplyFindingAssessment(ctx, created.CaseID, created.FindingID, actor, progress, now); err != nil {
			t.Fatal("closure", err)
		}
	})
	// Later withdrawal is a new immutable report, not a rewrite of an adopted one.
	record.PreviousHash = ref.RecordHash
	for _, state := range []string{"revised", "withdrawn"} {
		record.Revision++
		record.Withdrawn = state == "withdrawn"
		nextRaw, _ := json.Marshal(record)
		obj, _ := connector.DecodeStrictJSONObject(nextRaw)
		nextRaw, _ = connector.CanonicalizeJSONValue(obj)
		importResourceReviewRecord(t, f, nextRaw, now)
		record.PreviousHash = sourcefacts.Hash(nextRaw)
		withScope(func(ctx context.Context) {
			view, err := f.workspace.GetFindingAssessment(ctx, created.CaseID, created.FindingID, now)
			if err != nil || view.ReviewBasis.State != "changed" || len(view.ReviewBasis.ResourceUpdates) != 1 || view.ReviewBasis.ResourceUpdates[0].State != state || view.ReviewBasis.ResourceUpdates[0].LatestRevision != record.Revision || view.ReviewBasis.Current.Digest != created.Basis.Digest || view.Assessment.Review.Basis.Digest != created.Basis.Digest {
				t.Fatal("current resource signal and frozen basis", state, err, view.ReviewBasis)
			}
			queue, err := f.workspace.ListInvestigationQueue(ctx, workspace.QueueAccess{Subject: f.subject, Revision: "resource-update"}, workspace.QueueRequest{Filters: workspace.QueueFilters{Mode: "reassessment"}, Limit: 10}, now)
			if err != nil || len(queue.Items) != 1 || queue.Items[0].ReviewBasis != "changed" || len(queue.Items[0].ResourceUpdates) != 1 || queue.Items[0].ResourceUpdates[0].State != state {
				t.Fatal("queue resource signal", state, err, queue)
			}
		})
	}
	var sealed workspace.Case
	withScope(func(ctx context.Context) {
		view, err := f.workspace.GetFindingAssessment(ctx, created.CaseID, created.FindingID, now)
		if err != nil || view.Assessment.Review.Basis.Digest != created.Basis.Digest {
			t.Fatal("historical revision changed", err)
		}
		sealed, err = f.workspace.SealCase(ctx, created.CaseID, actor, now.Add(time.Second))
		if err != nil {
			t.Fatal("seal", err)
		}
		saved, err := f.workspace.GetCase(ctx, created.CaseID)
		if err != nil {
			t.Fatal("read sealed", err)
		}
		var pack workspace.EvidencePack
		if err = json.Unmarshal(saved.Case.SealedPack, &pack); err != nil {
			t.Fatal(err)
		}
		html, err := workspace.RenderEvidencePackHTML(pack, sealed.SealedDigest)
		if err != nil || !strings.Contains(string(html), ref.RecordHash) {
			t.Fatal("resource evidence HTML", err)
		}
		next := command
		next.Operation = "continue_finding"
		next.OperationID = testUUID(t)
		next.Review = nil
		next.From = &workspace.SealedFindingReference{FindingReference: workspace.FindingReference{CaseID: created.CaseID, FindingID: created.FindingID}, Revision: *sealed.SealedRevision, Digest: sealed.SealedDigest}
		b, err := f.workspace.PreviewReviewCreation(ctx, next)
		if err != nil {
			t.Fatal("continue preview", err)
		}
		next.ExpectedBasisDigest = b.Digest
		if _, err = f.workspace.CreateReview(ctx, actor, next, now.Add(2*time.Second), nil); err != nil {
			t.Fatal("continue", err)
		}
	})
	// A later record can name another target scope. Its details must not leak
	// through the current signal while the originally adopted record stays readable.
	record.Revision++
	record.Target.ProjectID = "999999"
	unreadable, _ := json.Marshal(record)
	obj, _ := connector.DecodeStrictJSONObject(unreadable)
	unreadable, _ = connector.CanonicalizeJSONValue(obj)
	importResourceReviewRecord(t, f, unreadable, now)
	withScope(func(ctx context.Context) {
		view, err := f.workspace.GetFindingAssessment(ctx, created.CaseID, created.FindingID, now)
		if err != nil || view.ReviewBasis.State != "unavailable" || len(view.ReviewBasis.ResourceUpdates) != 1 || view.ReviewBasis.ResourceUpdates[0].State != "unavailable" || view.ReviewBasis.ResourceUpdates[0].LatestRevision != 0 || view.ReviewBasis.ResourceUpdates[0].LatestHash != "" {
			t.Fatal("unauthorized successor metadata disclosed", err)
		}
		if view.Assessment.Review.Basis.Digest != created.Basis.Digest {
			t.Fatal("new target rewrote historical basis")
		}
		queue, err := f.workspace.ListInvestigationQueue(ctx, workspace.QueueAccess{Subject: f.subject, Revision: "resource-current-denied"}, workspace.QueueRequest{Filters: workspace.QueueFilters{Mode: "reassessment"}, Limit: 10}, now)
		if err != nil || len(queue.Items) != 1 || queue.Items[0].Basis != "unavailable" || len(queue.Items[0].ResourceUpdates) != 1 || queue.Items[0].ResourceUpdates[0].LatestRevision != 0 || queue.Items[0].ResourceUpdates[0].LatestHash != "" {
			t.Fatal("queue disclosed unauthorized successor", err)
		}
	})
	for _, source := range []string{reporter, f.source} {
		f.exec(`UPDATE workbench_access_grants SET active=false WHERE subject_id=$1 AND source_system_id=$2`, f.subject, source)
		withScope(func(ctx context.Context) {
			if _, err := f.workspace.GetCase(ctx, created.CaseID); !errors.Is(err, workspace.ErrNotFound) {
				t.Fatal("revoked case", err)
			}
			q, e := f.workspace.ListInvestigationQueue(ctx, workspace.QueueAccess{Subject: f.subject, Revision: "resource-revoked"}, workspace.QueueRequest{Limit: 10}, now)
			if e != nil || len(q.Items) != 0 {
				t.Fatal("revoked queue", e)
			}
		})
		if err := db.ValidateFindingAssessmentRestore(f.ctx, f.admin); err != nil {
			t.Fatal("restore must not require actor's current grants", err)
		}
		if source == reporter {
			restoredDSN := workspaceTestDSN(t, f.ctx, os.Getenv("XINSIGHT_POSTGRES_DSN"))
			backup := filepath.Join(t.TempDir(), "resource-review.dump")
			runPG := func(dsn, tool string, args ...string) {
				t.Helper()
				u, e := url.Parse(dsn)
				if e != nil {
					t.Fatal(e)
				}
				password, _ := u.User.Password()
				cmd := exec.CommandContext(f.ctx, tool, args...)
				cmd.Env = append(os.Environ(), "PGHOST="+u.Hostname(), "PGPORT="+u.Port(), "PGUSER="+u.User.Username(), "PGPASSWORD="+password, "PGDATABASE="+strings.TrimPrefix(u.Path, "/"), "PGSSLMODE=disable")
				if output, e := cmd.CombinedOutput(); e != nil {
					t.Fatalf("%s failed: %v (%d bytes retained by command)", tool, e, len(output))
				}
			}
			runPG(f.dsn, "pg_dump", "-Fc", "-f", backup)
			runPG(restoredDSN, "pg_restore", "--exit-on-error", "-d", func() string { u, _ := url.Parse(restoredDSN); return strings.TrimPrefix(u.Path, "/") }(), backup)
			restored := openTestSQL(t, restoredDSN)
			defer restored.Close()
			if e := db.ValidateWorkbenchRestore(f.ctx, restored); e != nil {
				t.Fatal("new database restored workbench", e)
			}
			if e := db.ValidateFindingAssessmentRestore(f.ctx, restored); e != nil {
				t.Fatal("new database restored history", e)
			}
			var records, links int
			if e := restored.QueryRow(`SELECT (SELECT count(*) FROM resource_access_records),(SELECT count(*) FROM case_resource_links)`).Scan(&records, &links); e != nil || records != 4 || links != 2 {
				t.Fatal("restored immutable resource chain and citations", records, links, e)
			}
		}
		f.exec(`UPDATE workbench_access_grants SET active=true WHERE subject_id=$1 AND source_system_id=$2`, f.subject, source)
	}
}

// Use the same registered, hash-bound importer entry point as the installed
// CLI, preserving the original JSON spellings through actual PG persistence.
func importResourceReviewRecord(t *testing.T, f *sourceFixture, raw []byte, now time.Time) {
	t.Helper()
	record, err := workbench.DecodeResourceAccessForImport(raw)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := json.Marshal(map[string]any{"schema_version": sourcefacts.ResourceOfflineBundleVersion, "records": []json.RawMessage{raw}})
	if err != nil {
		t.Fatal(err)
	}
	var name, oid, system string
	if err = f.admin.QueryRow(`SELECT current_database(),(SELECT oid::text FROM pg_database WHERE datname=current_database()),public.synthetic_resource_cluster_id()`).Scan(&name, &oid, &system); err != nil {
		t.Fatal(err)
	}
	a := sourcefacts.OfflineAuthorization{SchemaVersion: sourcefacts.ResourceOfflineAuthorizationVersion, AuthorizationID: testUUID(t), Environment: "test", TargetRef: "resource-review-local", MachineIDSHA256: sourcefacts.Hash([]byte("machine")), ProductVersion: "dev", ProductCommit: "resource-review", SourceSchemaSHA256: sourcefacts.ResourceSchemaSHA256(), SourceSystemID: record.Reporter.SourceSystemID, SourceInstance: record.Reporter.SourceInstance, OrgID: record.Reporter.OrgID, ProjectID: record.Reporter.ProjectID, Profile: sourcefacts.ResourceOfflineProfile, NotBefore: now.Add(-time.Minute).Format(sourcefacts.TimeFormat), ExpiresAt: now.Add(time.Hour).Format(sourcefacts.TimeFormat), BundlePath: "/synthetic/resource.json", BundleSHA256: sourcefacts.Hash(bundle), BundleID: testUUID(t), OperationID: testUUID(t), MaxRecords: 1, PayloadCaptureMode: "metadata_only", TargetDatabaseSchema: buildinfo.DatabaseSchemaMax, TargetDatabaseName: name, TargetDatabaseOID: oid, TargetDatabaseSystemID: system, TargetSourceSystemID: record.Target.SourceSystemID, TargetSourceInstance: record.Target.SourceInstance, TargetOrgID: record.Target.OrgID, TargetProjectID: record.Target.ProjectID}
	registered, err := f.consumerStore.RegisterSourceOfflineAuthorization(f.ctx, a)
	if err != nil {
		t.Fatal("resource registration", err)
	}
	if _, err = f.consumerStore.ControlSourceOfflineAuthorization(f.ctx, a.AuthorizationID, "active", registered.Revision, a.Digest(), a); err != nil {
		t.Fatal("resource enable", err)
	}
	if n, err := f.consumerStore.CheckSyntheticResources(f.ctx, a, bundle, now); err != nil || n != 1 {
		t.Fatal("resource check", n, err)
	}
	if _, err = f.consumerStore.ImportSyntheticResources(f.ctx, a, bundle, now); err != nil {
		t.Fatal("resource import", err)
	}
	var persisted []byte
	var hash string
	if err = f.admin.QueryRow(`SELECT record_bytes,record_hash FROM resource_access_records WHERE source_system_id=$1 AND record_ref=$2 AND revision=$3`, record.Reporter.SourceSystemID, record.RecordRef, record.Revision).Scan(&persisted, &hash); err != nil || string(persisted) != string(raw) || hash != sourcefacts.Hash(raw) {
		t.Fatal("source bytes changed at intake", err)
	}
}
