package api_test

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/reviewbody"
)

// Explicit local-only fixture: existing schema47 managed accounts, an actually
// captured synthetic request and its Runtime observation, exact metadata grant.
// It never calls a model, accesses a real source, or changes production grants.
func TestUnifiedFirstUseBrowserHarness(t *testing.T) {
	if os.Getenv("XI_UNIFIED_BROWSER_HARNESS") != "1" {
		t.Skip("explicit isolated first-use browser qualification only")
	}
	output := os.Getenv("XI_UNIFIED_BROWSER_OUTPUT")
	if !filepath.IsAbs(output) || filepath.Clean(output) != output {
		t.Fatal("exact output required")
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	f := newScopedContentFixtureWithCustody(t, []byte(`{"value":1,"other":2}`), reviewbody.KeyRing{}, access.RoleScopedReader)
	seedCapturedRequestRuntimeObservation(t, f)
	// Fixture seeding deliberately replaces only this test subject's broad
	// content grant. Real users must use their ordinary reviewed grant path.
	if _, err := f.admin.ExecContext(t.Context(), `UPDATE workbench_access_grants SET source_project_ref=$2,allow_summary=false,allow_content=false,allowed_content_purposes='[]',allow_coverage=true,allow_source_context=true,allow_source_runtime=true WHERE subject_id=$1`, scopedContentSubject, "project-"+f.collection.ProjectID); err != nil {
		t.Fatal(err)
	}
	hash, err := access.DerivePasswordHash([]byte(managedPassword))
	if err != nil {
		t.Fatal(err)
	}
	root := db.NewLocalAccountStore(f.admin)
	if err = root.Initialize(t.Context(), access.AccountConfig{SchemaVersion: access.AccountConfigSchemaV3, Accounts: []access.AccountConfigEntry{{SubjectID: managedOwnerID, Username: "owner", Role: access.RolePlatformAdmin, PasswordHash: hash}, {SubjectID: scopedContentSubject, Username: "reader", Role: access.RoleScopedReader, PasswordHash: hash}}}, "synthetic-schema47-existing-authority"); err != nil {
		t.Fatal(err)
	}
	provider, err := access.NewDurableProvider(f.store.LocalAccounts())
	if err != nil {
		t.Fatal(err)
	}
	service, err := access.NewService(provider, access.DiscardAuditor{}, access.ServiceOptions{Now: func() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return time.Now().UTC().Add(f.apiOffset) }})
	if err != nil {
		t.Fatal(err)
	}
	expected, err := db.LoadMigrationChecksums("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	f.server.Close()
	server := httptest.NewTLSServer(api.NewProductionHandler(f.reader, f.store, expected, service, api.TransportSecurity{}, connectorcontrol.InvitationConfig{}, nil, nil))
	t.Cleanup(server.Close)
	// Give owner exactly the same metadata entry through the ordinary native API.
	owner := newManagedHTTPClient(t, server, "owner", managedPassword)
	command := map[string]any{"grant_id": randomTestUUID(t), "subject_id": managedOwnerID, "source_system_id": f.sourceID, "source_project_ref": "project-" + f.collection.ProjectID, "expected_revision": 0, "capabilities": []string{"details", "coverage", "source_context", "source_runtime"}, "check_only": false, "revoke": false, "password": managedPassword}
	if n, _ := owner.call("POST", "/api/v1/access/project-read-grants", command, nil); n != 200 {
		t.Fatal("synthetic owner grant", n)
	}
	raw, _ := json.Marshal(map[string]string{"url": server.URL, "username": "reader", "owner_username": "owner", "password": managedPassword, "source": f.sourceID, "project": "project-" + f.collection.ProjectID, "request_id": f.choice.Reference.RequestID})
	if err = os.WriteFile(filepath.Join(output, "fixture.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("isolated schema47 metadata owner/reader + actual captured request HTTPS fixture ready")
	deadline := time.NewTimer(30 * time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("browser fixture deadline")
		case <-tick.C:
			if _, err = os.Stat(filepath.Join(output, "expire-sessions")); err == nil {
				f.mu.Lock()
				f.apiOffset = 24 * time.Hour
				f.mu.Unlock()
			}
			if _, err = os.Stat(filepath.Join(output, "stop")); err == nil {
				return
			}
		}
	}
}
