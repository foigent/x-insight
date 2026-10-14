package db

import (
	"encoding/json"
	"fmt"
	"github.com/foigent/x-insight/internal/workbench"
	"strconv"
	"testing"

	"github.com/foigent/x-insight/internal/buildinfo"
)

func TestContentGrantDomainChanges(t *testing.T) {
	yes, no, rev, p, all := true, false, int64(8), "p", "*"
	r := WorkbenchGrantChange{SchemaVersion: "xinsight.workbench-grant-change.v2", Action: "replace", Subject: "xs_1111111111111111", Source: "s", SourceProject: &p, CostProject: &all, ExpectedRevision: &rev, Capabilities: []string{"details", "source_runtime"}, AllowContent: &yes, AllowedContentPurposes: []string{"human_review"}}
	before := map[string]any{"subject_id": r.Subject, "source_system_id": r.Source, "source_project_ref": "p", "cost_project_ref": nil, "active": true, "allow_details": true, "allow_source_runtime": true, "allow_content": true, "allowed_content_purposes": []string{"human_review"}}
	check := func(name string, req WorkbenchGrantChange, prior map[string]any, wantLegacy, wantContent, wantErr bool) {
		t.Helper()
		raw, _ := json.Marshal(prior)
		l, c, err := grantRevisionChanges(req, raw)
		if (err != nil) != wantErr || err == nil && (l != wantLegacy || c != wantContent) {
			t.Fatalf("%s: %t/%t/%v", name, l, c, err)
		}
	}
	check("no-op", r, before, false, false, false)
	old := r
	old.SchemaVersion = "xinsight.workbench-grant-change.v1"
	old.AllowContent = nil
	old.AllowedContentPurposes = nil
	check("v1 active content replace", old, before, false, false, true)
	before["active"] = false
	check("v1 inactive content replace", old, before, false, false, true)
	check("reactivation", r, before, true, true, false)
	before["active"] = true
	old.Action = "revoke"
	old.Capabilities = []string{}
	check("v1 full revoke", old, before, true, true, false)
	single := r
	single.AllowContent = &no
	single.AllowedContentPurposes = []string{}
	check("single content revoke", single, before, false, true, false)
	legacy := r
	legacy.Capabilities = []string{"details"}
	check("legacy changes", legacy, before, true, false, false)
	scoped := r
	scoped.Subject = "xs_2222222222222222"
	check("identity changes", scoped, before, true, true, false)
}

func TestContentGrantPreservesLegacyDigestAndFencesABA(t *testing.T) {
	g := workbench.Grant{ID: "old", SubjectID: "subject", SourceSystemID: "source", SourceProject: "project", AllowSummary: true, AllowDetails: true, AllowSourceRuntime: true, AllowResource: true, AllowBudgetSummary: true, Revision: 9, LegacyRevision: 9}
	a := workbenchAuthorizationFromGrants([]workbench.Grant{g})
	// This is the exact pre39 encoding; the current counter must not leak into it.
	oldPart := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%t\x00%t\x00%t\x00%t\x00%d", g.ID, g.SourceSystemID, g.SourceProject, g.CostProject, true, true, false, false, int64(9))
	if a.Revision != digestText(oldPart) || a.SourceRevision != digestText(oldPart+"\x00false\x00false\x00false") || a.PurposeRevision != digestText(oldPart+"\x00false\x00false") || a.ObservationRevision != digestText(oldPart+"\x00false\x00false\x00false\x00true\x00false\x00false\x00false\x00false\x00resource:1") || a.BudgetRevision != digestText(oldPart+"\x00false\x00true\x00false") {
		t.Fatal("old bytes changed")
	}
	legacyEqual := func(b workbench.Authorization) bool {
		return a.Revision == b.Revision && a.SourceRevision == b.SourceRevision && a.PurposeRevision == b.PurposeRevision && a.ObservationRevision == b.ObservationRevision && a.BudgetRevision == b.BudgetRevision
	}
	g.Revision++
	g.ContentRevision = 1
	g.AllowContent = true
	g.AllowedContentPurposes = []string{"human_review"}
	b := workbenchAuthorizationFromGrants([]workbench.Grant{g})
	if !legacyEqual(b) || contentScopeSuffix(a) != "" || contentScopeSuffix(b) == "" || b.ContentRevision == "" {
		t.Fatal("content touched legacy or omitted scope")
	}
	c := workbench.Grant{ID: "content-only", SubjectID: "subject", SourceSystemID: "other", AllowContent: true, AllowedContentPurposes: []string{"human_review"}, Revision: 1, LegacyRevision: 1, ContentRevision: 1}
	if !legacyEqual(workbenchAuthorizationFromGrants([]workbench.Grant{g, c})) {
		t.Fatal("C-only creation touched old membership")
	}
	c.SourceProject = "changed"
	c.Revision++
	c.ContentRevision++
	if !legacyEqual(workbenchAuthorizationFromGrants([]workbench.Grant{g, c})) {
		t.Fatal("C-only edit touched old membership")
	}
	g.AllowContent = false
	g.AllowedContentPurposes = []string{}
	g.Revision++
	g.ContentRevision++
	if !legacyEqual(workbenchAuthorizationFromGrants([]workbench.Grant{g})) {
		t.Fatal("single C revoke touched legacy")
	}
	g.AllowContent = true
	g.AllowedContentPurposes = []string{"human_review"}
	g.ContentRevision++
	g.Revision++
	if workbenchAuthorizationFromGrants([]workbench.Grant{g}).ContentRevision == b.ContentRevision {
		t.Fatal("content ABA")
	}
	g.LegacyRevision++
	g.ContentRevision++
	g.Revision++
	if legacyEqual(workbenchAuthorizationFromGrants([]workbench.Grant{g})) {
		t.Fatal("full revoke/reactivate revived old legacy epoch")
	}
}

// Release metadata is an independent input, so forgetting an entry in the
// explicit runtime compatibility list fails here without starting PostgreSQL.
func TestCurrentReleaseAuthorizationSchemas(t *testing.T) {
	migrations, err := LoadMigrationChecksums("../../migrations")
	if err != nil || len(migrations) == 0 {
		t.Fatalf("read current migration identity: %v", err)
	}
	current, err := strconv.Atoi(buildinfo.DatabaseSchemaMax)
	if err != nil {
		t.Fatal(err)
	}
	version := migrations[len(migrations)-1].ID
	if len(version) < 7 || version[:6] != fmt.Sprintf("%06d", current) {
		t.Fatal("current migration differs from release metadata")
	}
	for _, content := range []bool{false, true} {
		if !workbenchGrantSchemaSupported(version, content) {
			t.Fatalf("current release schema %s lacks grant support (content=%t)", version, content)
		}
		for _, unsupported := range []string{"", "000035_add_source_observations", fmt.Sprintf("%06d_unpublished", current+1)} {
			if workbenchGrantSchemaSupported(unsupported, content) {
				t.Fatalf("unsupported grant schema %s accepted", unsupported)
			}
		}
	}
	if !budgetOfflineSchemaSupported(buildinfo.DatabaseSchemaMax) || budgetOfflineSchemaSupported(strconv.Itoa(current+1)) || budgetOfflineSchemaSupported("35") {
		t.Fatal("budget schema compatibility differs from the current release or accepts unsupported schema")
	}
}
