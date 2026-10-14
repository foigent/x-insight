package connectoractivation

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/integration/urmachine"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

func TestMigratedProjectRevocationKeepsOriginalIdentitiesAndIndependentPurposes(t *testing.T) {
	b := connectorcontrol.ManagedBoundary{Installation: connectorcontrol.Installation{InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA"}, Bootstrap: &connectorcontrol.ReceiverAdoptionBootstrap{SourceInstance: "original-source", OrgID: 2}}
	base := sourcefacts.ManagedConfig{
		Profiles: []sourcefacts.ManagedProfileConfig{
			{StreamID: "original-stream", SourceInstance: "original-source", OrgID: "2", ProjectID: "86"},
			{StreamID: "other-project-stream", SourceInstance: "original-source", OrgID: "2", ProjectID: "87"},
			{StreamID: "other-source-stream", SourceInstance: "foreign-source", OrgID: "2", ProjectID: "86"},
		},
		Budgets:  []sourcefacts.ManagedBudgetConfig{{CollectionID: "original-budget", SourceInstance: "original-source", OrgID: "2", ProjectID: "86"}},
		Contents: []sourcefacts.ManagedContentConfig{{CollectionID: "original-content", SourceInstance: "original-source", OrgID: "2", ProjectID: "86"}},
	}
	enrollment := urmachine.Enrollment{EnrollmentID: "xise_1", FactsStatus: "revoked", ContentStatus: "active", Profiles: []string{sourcefacts.ContextProfile}}
	profiles, budgets, content := managedRevocationTargets(base, b, "86", enrollment)
	if !reflect.DeepEqual(profiles, []string{"original-stream"}) || !reflect.DeepEqual(budgets, []string{"original-budget"}) || len(content) != 0 {
		t.Fatal("facts revocation changed other scope or content", profiles, budgets, content)
	}
	enrollment.FactsStatus, enrollment.ContentStatus = "active", "revoked"
	profiles, budgets, content = managedRevocationTargets(base, b, "86", enrollment)
	if len(profiles)+len(budgets) != 0 || !reflect.DeepEqual(content, []string{"original-content"}) {
		t.Fatal("content revocation changed facts", profiles, budgets, content)
	}
	// A future project has its own generated IDs and cannot select old identities.
	profiles, budgets, content = managedRevocationTargets(base, b, "88", enrollment)
	want := hostctl.ManagedCollectionID(b.Installation.InstallationID, enrollment.EnrollmentID, "88", "collection:content")
	if len(profiles)+len(budgets) != 0 || !reflect.DeepEqual(content, []string{want}) {
		t.Fatal("future project used original collection", profiles, budgets, content)
	}
}

func TestEnrollmentDiscoveryScopesRepeatedGrantByProject(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(fmt.Sprintf("duplicate=%t", duplicate), func(t *testing.T) {
			second := "87"
			if duplicate {
				second = "86"
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "no-store")
				fmt.Fprintf(w, `{"schema_version":"unirouter.xinsight.source-enrollment-projects.v1","installation_id":"xci_test","source_instance":"ur-a","org_id":"7","projects":[{"project_id":"86","display_name":"First","enrollments":[{"enrollment_id":"xise_1","revision":"1","profiles":[],"include_content":true,"facts_status":"not_requested","content_status":"pending"}]},{"project_id":"%s","display_name":"Second","enrollments":[{"enrollment_id":"xise_1","revision":"1","profiles":[],"include_content":true,"facts_status":"not_requested","content_status":"pending"}]}],"next_cursor":""}`, second)
			}))
			defer server.Close()
			client, err := urmachine.NewEnrollmentClient(server.URL, "ur-xse1-synthetic", urmachine.Identity{InstallationID: "xci_test", SourceInstance: "ur-a", OrgID: "7"}, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			projects, err := allEnrollmentProjects(context.Background(), client)
			if duplicate {
				if !errors.Is(err, urmachine.ErrContract) {
					t.Fatal("same project duplicate accepted", err)
				}
			} else if err != nil || len(projects) != 2 {
				t.Fatal("same grant two projects rejected", err)
			}
		})
	}
}
