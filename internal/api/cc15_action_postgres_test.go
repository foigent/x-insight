package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

// The coordinator supplies one root-owned, exact-path input after the five UR
// rounds and native OpenClaw export finish. It contains references, never a
// credential. This hook runs only in the explicitly selected CC15 fixture.
func cc15ActionProof(t *testing.T, f *p3BrowserFixture, server *httptest.Server, admin *sql.DB, rounds cc14XIRequests) {
	t.Helper()
	specPath := filepath.Join(f.output, "action-assembly-input.json")
	f.wait(t, "CC15 native and checker inputs", func(_ context.Context) bool {
		info, err := os.Lstat(specPath)
		return err == nil && info.Mode().IsRegular() && info.Mode().Perm() == 0600
	})
	bundlePath := filepath.Join(f.output, "action-bundle.json")
	checkedBundle, err := cc15PrepareBundle(specPath, "", true)
	p3Check(t, err == nil, "CC15 read-only native action preparation check")
	_, err = os.Lstat(bundlePath)
	p3Check(t, os.IsNotExist(err), "CC15 preparation check wrote no bundle")
	bundle, err := cc15PrepareBundle(specPath, bundlePath, false)
	p3Check(t, err == nil && sourcefacts.Hash(bundle) == sourcefacts.Hash(checkedBundle), "CC15 prepared action bundle matches check")
	p3JSON(t, filepath.Join(f.output, "action-prepare.json"), map[string]any{"status": "prepared", "documents": 4, "bundle_sha256": sourcefacts.Hash(bundle), "check_wrote_bundle": false})
	var body struct {
		Version string                    `json:"schema_version"`
		Records []workbench.ActionWitness `json:"records"`
	}
	p3Check(t, json.Unmarshal(bundle, &body) == nil && body.Version == sourcefacts.ActionOfflineBundleVersion && len(body.Records) == 4, "CC15 four prepared native actions")
	target := workbench.ResourceScope{SourceSystemID: unirouter.DefaultSourceSystemID, SourceInstance: f.source.SourceInstance, OrgID: f.source.OrgID, ProjectID: f.source.ProjectID}
	reporter := body.Records[0].Reporter
	p3Check(t, reporter.SourceSystemID == "1b14d52b-4439-4b8d-af1c-93899a02993e" && reporter.SourceSystemID != target.SourceSystemID && reporter.SourceInstance == "openclaw-cc15-test" && reporter.OrgID == target.OrgID && reporter.ProjectID == target.ProjectID, "CC15 exact distinct native reporter")
	for i, record := range body.Records {
		p3Check(t, record.Target == target && record.Reporter == reporter && record.RequestID == rounds.Requests[i].RequestID && record.FinalRequestID == rounds.Requests[4].RequestID && record.RunID == body.Records[0].RunID && record.Environment == "test" && !record.Withdrawn, "CC15 exact action target and five-round relation")
		if i == 2 {
			p3Check(t, record.ActionKind == "edit" && record.Acceptance.State == "confirmed" && record.Acceptance.GoalState == "passed", "CC15 separate edit and checker claims")
		} else {
			p3Check(t, record.ActionKind == "read" && record.Acceptance.State == "not_checked", "CC15 read report has no invented checker claim")
		}
	}
	var product buildinfo.Info
	version := f.cli(t, "action-importer-version", "version", "--json")
	p3Check(t, json.Unmarshal(version, &product) == nil && product.ProductVersion != "" && product.Commit != "", "CC15 importer product identity")
	var name, oid, system string
	err = admin.QueryRowContext(f.ctx, `SELECT current_database(),(SELECT oid::text FROM pg_database WHERE datname=current_database()),system_identifier::text FROM pg_control_system()`).Scan(&name, &oid, &system)
	p3Check(t, err == nil && name != os.Getenv("XI_P3_BASE_DATABASE") && cc14Positive(oid) && system == os.Getenv("XI_P3_SYSTEM_ID"), "CC15 isolated XI database identity")
	machine, err := os.ReadFile("/etc/machine-id")
	p3Check(t, err == nil && len(bytes.TrimSpace(machine)) > 0, "CC15 host binding")
	now := time.Now().UTC()
	run := f.cc14Scenario.run
	a := sourcefacts.OfflineAuthorization{SchemaVersion: sourcefacts.ResourceOfflineAuthorizationVersion, AuthorizationID: run + "-actions", Environment: "test", TargetRef: run, MachineIDSHA256: sourcefacts.Hash(bytes.TrimSpace(machine)), ProductVersion: product.ProductVersion, ProductCommit: product.Commit, SourceSchemaSHA256: sourcefacts.ActionSchemaSHA256(), SourceSystemID: reporter.SourceSystemID, SourceInstance: reporter.SourceInstance, OrgID: reporter.OrgID, ProjectID: reporter.ProjectID, Profile: sourcefacts.ActionOfflineProfile, NotBefore: now.Add(-time.Minute).Format(sourcefacts.TimeFormat), ExpiresAt: now.Add(time.Hour).Format(sourcefacts.TimeFormat), BundlePath: bundlePath, BundleSHA256: sourcefacts.Hash(bundle), BundleID: run + "-actions", OperationID: run + "-actions", MaxRecords: 4, PayloadCaptureMode: "metadata_only", TargetDatabaseSchema: buildinfo.DatabaseSchemaMax, TargetDatabaseName: name, TargetDatabaseOID: oid, TargetDatabaseSystemID: system, TargetSourceSystemID: target.SourceSystemID, TargetSourceInstance: target.SourceInstance, TargetOrgID: target.OrgID, TargetProjectID: target.ProjectID}
	p3Check(t, a.Validate(now) == nil, "CC15 bounded action authorization")
	authPath := filepath.Join(f.output, "action-authorization.json")
	p3JSON(t, authPath, a)
	owner, err := db.OpenSourceFactsStore(f.adminDSN)
	p3Check(t, err == nil, "CC15 authorization owner store")
	defer owner.Close()
	grant, err := owner.RegisterSourceOfflineAuthorization(f.ctx, a)
	p3Check(t, err == nil, "CC15 action authorization registration")
	_, err = owner.ControlSourceOfflineAuthorization(f.ctx, a.AuthorizationID, "active", grant.Revision, a.Digest(), a)
	p3Check(t, err == nil, "CC15 action authorization activation")
	if f.formal != nil {
		f.formalGrant(t, "reviewer-action-reporter", "reviewer", reporter.SourceSystemID, "project-"+reporter.ProjectID, true, false)
	} else {
		_, err = admin.ExecContext(f.ctx, `INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_resource) VALUES('xs_8899aabbccddeeff',$1::uuid,$2,true)`, reporter.SourceSystemID, "project-"+reporter.ProjectID)
		p3Check(t, err == nil, "CC15 scoped action reporter grant")
	}
	var check struct {
		Status    string `json:"status"`
		Documents int    `json:"documents"`
	}
	checked := f.cli(t, "action-check", "import-resource-actions", "--authorization-file", authPath, "--target-ref", run, "--check")
	p3Check(t, json.Unmarshal(checked, &check) == nil && check.Status == "checked" && check.Documents == 4, "CC15 action check is complete")
	var receipt struct {
		Status string `json:"status"`
		Counts struct {
			Inserted int `json:"inserted"`
		} `json:"counts"`
	}
	imported := f.cli(t, "action-import", "import-resource-actions", "--authorization-file", authPath, "--target-ref", run)
	p3Check(t, json.Unmarshal(imported, &receipt) == nil && receipt.Status == "completed" && receipt.Counts.Inserted == 4, "CC15 four durable action witnesses")
	input := httpReadingRequest(unirouter.DefaultSourceSystemID)
	input.Filters.SourceProjects = []string{"project-" + f.source.ProjectID}
	runtimeInput := workbench.RuntimeReadingRequest{ContractVersion: workbench.RuntimeContractVersion, MetricVersion: workbench.RuntimeMetricVersion, Period: input.Period, Filters: input.Filters}
	readActions := func(person string) workbench.RuntimeActionWitnesses {
		client := f.ordinaryLogin(t, server, person)
		status, raw, _ := f.contentHTTP(t, client, "POST", "/api/v1/runtime/readings", runtimeInput)
		var reading workbench.RuntimeReading
		p3Check(t, status == 201 && json.Unmarshal(raw, &reading) == nil, "CC15 fresh action reading: "+person)
		base := "/api/v1/runtime/readings/" + reading.ID
		status, raw, _ = f.contentHTTP(t, client, "GET", base+"/requests?entry_time=all", nil)
		var requests workbench.RuntimeRequests
		p3Check(t, status == 200 && json.Unmarshal(raw, &requests) == nil && len(requests.Items) == 5, "CC15 five frozen requests: "+person)
		finalRef := ""
		for _, request := range requests.Items {
			if request.RequestID == rounds.Requests[4].RequestID {
				finalRef = request.Ref
			}
		}
		p3Check(t, finalRef != "", "CC15 exact final request member: "+person)
		status, raw, _ = f.contentHTTP(t, client, "GET", base+"/requests/"+url.PathEscape(finalRef)+"/action-witnesses", nil)
		var actions workbench.RuntimeActionWitnesses
		p3Check(t, status == 200 && json.Unmarshal(raw, &actions) == nil && actions.ReadingID == reading.ID && actions.RequestRef == finalRef, "CC15 ordinary action reading: "+person)
		return actions
	}
	denied := readActions("reader")
	p3Check(t, len(denied.Items) == 0 && denied.NextCursor == "", "CC15 reader without native reporter resource grant sees no actions")
	actions := readActions("reviewer")
	p3Check(t, len(actions.Items) == 4 && actions.NextCursor == "", "CC15 grouped reviewer action reading")
	for i, item := range actions.Items {
		p3Check(t, item.Record.RequestID == rounds.Requests[i].RequestID && item.FinalRequestRef == actions.RequestRef && item.ChainState == "complete" && !item.ChangedSinceReading, fmt.Sprintf("CC15 action %d exact frozen relation", i+1))
		p3Check(t, len(item.ResolvedContentLinks) > 0, fmt.Sprintf("CC15 action %d C reference membership", i+1))
	}
	p3JSON(t, filepath.Join(f.output, "ordinary-actions.json"), actions)
	if f.formal != nil {
		f.formalGrant(t, "reader-action-reporter", "reader", reporter.SourceSystemID, "project-"+reporter.ProjectID, true, false)
	} else {
		_, err = admin.ExecContext(f.ctx, `INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_resource) VALUES('xs_5555555555555555',$1::uuid,$2,true)`, reporter.SourceSystemID, "project-"+reporter.ProjectID)
		p3Check(t, err == nil, "CC15 scoped reader action reporter grant")
	}
	readerActions := readActions("reader")
	p3Check(t, len(readerActions.Items) == 4 && readerActions.NextCursor == "", "CC15 scoped reader sees four actions after new grant and fresh reading")
	p3JSON(t, filepath.Join(f.output, "reader-actions.json"), map[string]any{"before_grant": denied, "after_grant": readerActions, "resource_grant_source_system_id": reporter.SourceSystemID, "content_grant_separate": true})
}
