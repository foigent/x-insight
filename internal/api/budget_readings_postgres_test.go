package api_test

import (
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func TestBudgetHTTPSourceImporterWorkspaceAndCurrentPermissions(t *testing.T) {
	t.Setenv("XINSIGHT_A0_RUNTIME_ROLES", "1")
	server, admin, _, source := startWorkbenchProduction(t)
	client := loginWorkbenchHTTP(t, server, "workbench.one", "workbench password")
	request := workbench.BudgetReadingRequest{ContractVersion: workbench.BudgetContractVersion}
	path := "/api/v1/workbench/budgets/readings"
	client.request(t, "POST", path, request, 403)
	var name string
	if err := admin.QueryRow(`SELECT current_database()`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(os.Getenv("XINSIGHT_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	owner, err := db.OpenSourceFactsStore(u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	u.User = workbenchRuntimeRoleUser(t, "importer")
	importer, err := db.OpenSourceFactsStore(u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer importer.Close()
	raw, err := os.ReadFile("../sourcefacts/contract/budgets/golden-readings.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct{ Positive []json.RawMessage }
	if err = json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	wire := vectors.Positive[0]
	r, err := sourcefacts.DecodeBudgetReading(wire)
	if err != nil {
		t.Fatal(err)
	}
	c := sourcefacts.BudgetCollection{ID: randomTestUUID(t), SourceSystemID: source, SourceInstance: r.SourceInstance, OrgID: r.OrgID, ProjectID: r.ProjectID, ScopeGeneration: "1", Profiles: r.Profiles()}
	if _, err = importer.RegisterBudgetCollection(t.Context(), c); err == nil {
		t.Fatal("importer registered collection")
	}
	c, err = owner.RegisterBudgetCollection(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	c, err = owner.ControlBudgetCollection(t.Context(), c.ID, "active", c.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	catalog := sourcefacts.BudgetCatalog{SchemaVersion: sourcefacts.BudgetCatalogVersion, SourceInstance: r.SourceInstance, OrgID: r.OrgID, ProjectID: r.ProjectID, ReadingCutoff: r.ReadingCutoff, AccessGeneration: r.AccessGeneration, CatalogRevision: r.CatalogRevision, Profiles: r.Profiles(), EnterpriseScope: "scope_not_configured", Entries: []sourcefacts.BudgetCatalogEntry{{BudgetRef: r.BudgetRef, Origin: "gateway_project", DefinitionRevision: r.DefinitionRevision, BindingRevision: r.BindingRevision}}}
	catalogRaw, _ := json.Marshal(catalog)
	if err = importer.CommitBudgetCatalog(t.Context(), c, catalogRaw, time.Now().UTC()); err != nil {
		t.Fatal("actual importer catalog", err)
	}
	c, err = importer.LoadBudgetCollection(t.Context(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = importer.CommitBudgetReading(t.Context(), c, wire, time.Now().UTC()); err != nil {
		t.Fatal("actual importer reading", err)
	}
	if _, err = admin.Exec(`UPDATE workbench_access_grants SET allow_budget_definition=true,allow_budget_summary=true,allow_budget_alert=true WHERE subject_id='xs_8899aabbccddeeff'`); err != nil {
		t.Fatal(err)
	}
	noCSRF := client
	noCSRF.csrf = ""
	noCSRF.request(t, "POST", path, request, 403)
	var reading workbench.BudgetReading
	if err = json.Unmarshal(client.request(t, "POST", path, request, 201), &reading); err != nil {
		t.Fatal(err)
	}
	path += "/" + reading.ID
	var items workbench.BudgetItems
	if err = json.Unmarshal(client.request(t, "GET", path+"/items", nil, 200), &items); err != nil {
		t.Fatal(err)
	}
	if len(items.Items) != 1 || items.Items[0].SourceReadingID != r.ReadingID || items.Items[0].Definition.SourceHash == "" {
		t.Fatal("real source receipt was not visible", items)
	}
	client.request(t, "GET", path+"/items?limit=101", nil, 400)
	client.request(t, "GET", path+"/items?cost_projects=private", nil, 400)
	client.request(t, "GET", path+"/items/"+items.Items[0].Ref+"/history", nil, 200)
	client.request(t, "GET", path, nil, 200)
	if _, err = admin.Exec(`UPDATE workbench_access_grants SET allow_budget_definition=false WHERE subject_id='xs_8899aabbccddeeff'`); err != nil {
		t.Fatal(err)
	}
	client.request(t, "GET", path, nil, 409)
	client.request(t, "GET", path+"/items", nil, 409)
	if err = json.Unmarshal(client.request(t, "POST", "/api/v1/workbench/budgets/readings", request, 201), &reading); err != nil {
		t.Fatal(err)
	}
	body := client.request(t, "GET", "/api/v1/workbench/budgets/readings/"+reading.ID+"/items", nil, 200)
	if json.Unmarshal(body, &items) != nil || len(items.Items) != 1 || items.Items[0].Definition.State != "not_authorized" {
		t.Fatal(string(body))
	}
	summary := items.Items[0].Summary.Body
	comparison, _ := summary["comparison"].(map[string]any)
	if comparison["remaining"] != nil || comparison["utilization_denominator"] != nil {
		t.Fatal("definition permission leaked via comparison")
	}
	// Budget fields do not require or confer access to economic detail.
	if _, err = admin.Exec(`UPDATE workbench_access_grants SET allow_summary=false,allow_details=false WHERE subject_id='xs_8899aabbccddeeff'`); err != nil {
		t.Fatal(err)
	}
	client.request(t, "POST", "/api/v1/workbench/readings", httpReadingRequest(source), 403)
	if err = json.Unmarshal(client.request(t, "POST", "/api/v1/workbench/budgets/readings", request, 201), &reading); err != nil {
		t.Fatal(err)
	}
	client.request(t, "GET", "/api/v1/workbench/budgets/readings/"+reading.ID+"/items", nil, 200)
	// Check the actual read-only role cannot append source facts or grant access.
	u.User = workbenchRuntimeRoleUser(t, "reader")
	reader, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err = reader.Exec(`UPDATE workbench_access_grants SET allow_budget_definition=true`); err == nil {
		t.Fatal("reader widened grants")
	}
	current, err := owner.LoadBudgetCollection(t.Context(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.ControlBudgetCollection(t.Context(), c.ID, "revoked", current.Revision, false); err != nil {
		t.Fatal(err)
	}
	client.request(t, "GET", "/api/v1/workbench/budgets/readings/"+reading.ID+"/items", nil, 409)
	// The same actual importer can consume explicitly registered offline input,
	// while registration remains an administrator-only action.
	bundle, _ := json.Marshal(sourcefacts.BudgetOfflineBundle{SchemaVersion: sourcefacts.BudgetOfflineBundleVersion, Catalog: catalogRaw, Readings: []sourcefacts.BudgetOfflineEntry{{Reading: wire}}})
	now := time.Now().UTC()
	a := sourcefacts.BudgetOfflineAuthorization{SchemaVersion: sourcefacts.BudgetOfflineAuthorizationVersion, CollectionID: randomTestUUID(t), Environment: "test", TargetRef: "xi-http-offline", MachineIDSHA256: sourcefacts.Hash([]byte("synthetic-machine")), ProductVersion: "dev-d", ProductCommit: strings.Repeat("a", 40), TargetDatabaseSchema: "36", SourceCommit: sourcefacts.BudgetSourceCommit, SourceManifestSHA256: sourcefacts.BudgetManifestSHA256(), SourceSystemID: source, SourceInstance: r.SourceInstance, OrgID: r.OrgID, ProjectID: r.ProjectID, ScopeGeneration: "1", Profiles: r.Profiles(), NotBefore: now.Add(-time.Minute).Format(sourcefacts.TimeFormat), ExpiresAt: now.Add(time.Hour).Format(sourcefacts.TimeFormat), BundlePath: "/test/controlled-budget.json", BundleSHA256: sourcefacts.Hash(bundle)}
	if _, err = importer.RegisterBudgetOffline(t.Context(), a); err == nil {
		t.Fatal("importer registered offline authority")
	}
	// The old schema36 authorization must not bind to this current database.
	if a.TargetDatabaseSchema != buildinfo.DatabaseSchemaMax {
		if _, err = owner.RegisterBudgetOffline(t.Context(), a); err == nil {
			t.Fatal("old schema authorization registered on the current target")
		}
	}
	a.TargetDatabaseSchema = buildinfo.DatabaseSchemaMax
	if _, err = importer.RegisterBudgetOffline(t.Context(), a); err == nil {
		t.Fatal("importer registered current-schema offline authority")
	}
	offline, err := owner.RegisterBudgetOffline(t.Context(), a)
	if err != nil {
		t.Fatal(err)
	}
	offline, err = owner.ControlBudgetOffline(t.Context(), a, offline.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = importer.CheckBudgetOffline(t.Context(), a, bundle, now); err != nil {
		t.Fatal("actual importer offline check", err)
	}
	if _, err = importer.ImportBudgetOffline(t.Context(), a, bundle, now); err != nil {
		t.Fatal("actual importer offline import", err)
	}
	if err = json.Unmarshal(client.request(t, "POST", "/api/v1/workbench/budgets/readings", request, 201), &reading); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(client.request(t, "GET", "/api/v1/workbench/budgets/readings/"+reading.ID+"/items", nil, 200), &items); err != nil || len(items.Items) != 1 || items.Items[0].SourceState != "bounded_offline" {
		t.Fatal("offline source label", err)
	}
	if err = db.ValidateWorkbenchRestore(t.Context(), admin); err != nil {
		t.Fatal("offline restore validation", err)
	}
	// A malformed replay never advances progress or mutates the frozen source bytes.
	m, _ := connector.DecodeStrictJSONObject(wire)
	m["reading_id"] = "ur-budget-reading-" + strings.Repeat("f", 32)
	changed, _ := json.Marshal(m)
	if err = importer.CommitBudgetReading(t.Context(), current, changed, time.Now().UTC()); err == nil {
		t.Fatal("revoked or mixed reading accepted")
	}
}
