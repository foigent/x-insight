package db_test

import (
	"github.com/foigent/x-insight/internal/sourcefacts"
	"testing"
	"time"
)

func TestSourceFactsCurrentStatusUsesFieldAndProjectAuthorization(t *testing.T) {
	f := newSourceFixture(t)
	f.allow()
	r := f.reading()
	status, e := f.workspace.GetWorkbenchSourceStatuses(f.ctx, f.subject, r.ID, time.Now())
	if e != nil || len(status.Items) != 1 || len(status.Items[0].SourceFacts) != 3 {
		t.Fatal("scoped managed statuses", status, e)
	}
	for _, v := range status.Items[0].SourceFacts {
		if v.OrgID != "17" || v.ProjectID != "101" || v.Status != "active" || v.LastReceivedAt == nil {
			t.Fatal("missing actual scope/receipt", v)
		}
	}
	p := sourcefacts.IdentityProfile
	stream, e := f.consumerStore.LoadSourceFactsStream(f.ctx, f.streams[p])
	if e != nil {
		t.Fatal(e)
	}
	if e = f.consumerStore.RecordSourceFactsFailure(f.ctx, stream, "cursor_expired", time.Now()); e != nil {
		t.Fatal(e)
	}
	status, e = f.workspace.GetWorkbenchSourceStatuses(f.ctx, f.subject, r.ID, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, v := range status.Items[0].SourceFacts {
		if v.Profile == p {
			found = v.RequiresAttention && v.LastError == "cursor_expired"
		}
	}
	if !found {
		t.Fatal("persistent current attention missing")
	}
	f.exec(`UPDATE workbench_access_grants SET allow_source_identity=false WHERE subject_id=$1`, f.subject)
	r = f.reading()
	status, e = f.workspace.GetWorkbenchSourceStatuses(f.ctx, f.subject, r.ID, time.Now())
	if e != nil || len(status.Items) != 1 || len(status.Items[0].SourceFacts) != 2 {
		t.Fatal("field status authorization", status, e)
	}
	for _, v := range status.Items[0].SourceFacts {
		if v.Profile == p || v.RequiresAttention || v.LastError != "" {
			t.Fatal("hidden profile leaked", v)
		}
	}
	f.exec(`INSERT INTO source_fact_streams(source_system_id,source_instance,org_id,project_id,profile,scope_generation,status,last_error,requires_attention) VALUES($1::uuid,'hidden-project','17','999',$2,'1','revoked','source_authorization_denied',true)`, f.source, sourcefacts.ContextProfile)
	f.exec(`UPDATE workbench_access_grants SET source_project_ref='project-101' WHERE subject_id=$1`, f.subject)
	r = f.reading()
	status, e = f.workspace.GetWorkbenchSourceStatuses(f.ctx, f.subject, r.ID, time.Now())
	if e != nil || len(status.Items) != 1 || len(status.Items[0].SourceFacts) != 2 {
		t.Fatal("project status authorization", status, e)
	}
	for _, v := range status.Items[0].SourceFacts {
		if v.ProjectID != "101" {
			t.Fatal("other project status leaked")
		}
	}
}
