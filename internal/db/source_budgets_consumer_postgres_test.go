package db_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func TestBudgetConsumerHTTPRecoveryAndAuthorizedSubsetChange(t *testing.T) {
	f := newSourceFixture(t)
	all := []string{sourcefacts.BudgetDefinitionProfile, sourcefacts.BudgetSummaryProfile, sourcefacts.BudgetAlertProfile}
	partial := []string{sourcefacts.BudgetSummaryProfile, sourcefacts.BudgetAlertProfile}
	full := budgetFixtureReading(t, 701, all, nil)
	revised := func(id int, profiles []string, generation string) []byte {
		return budgetFixtureReading(t, id, profiles, func(m map[string]any) { m["access_generation"] = generation; m["catalog_revision"] = generation })
	}
	partialRaw := revised(702, partial, "2")
	missing := revised(703, []string{sourcefacts.BudgetSummaryProfile}, "2")
	later := revised(704, all, "3")
	readings := map[int32][]byte{0: full, 2: partialRaw, 3: missing, 4: later}
	catalogs := map[int32][]byte{}
	for mode, raw := range readings {
		r, err := sourcefacts.DecodeBudgetReading(raw)
		if err != nil {
			t.Fatal(err)
		}
		catalogs[mode] = budgetFixtureCatalog(t, r)
	}
	catalogs[3] = catalogs[2]
	first, err := sourcefacts.DecodeBudgetReading(full)
	if err != nil {
		t.Fatal(err)
	}
	var mode atomic.Int32
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer synthetic-budget-consumer" {
			t.Error("wrong source credential")
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		m := mode.Load()
		if m == 1 {
			w.WriteHeader(503)
			return
		}
		if r.URL.Path == "/api/v1/integrations/xinsight/source/v2/capabilities" {
			profiles := all
			if m == 2 || m == 3 {
				profiles = partial
			}
			transports := map[string][]string{}
			for _, p := range profiles {
				transports[p] = []string{"budget_catalog", "current_budget_snapshot"}
			}
			json.NewEncoder(w).Encode(sourcefacts.Capabilities{SchemaVersion: "unirouter.source-capabilities.v2", SourceInstance: first.SourceInstance, Profiles: profiles, ProfileVersion: sourcefacts.ObservationConnectorVersion, Status: "active", CredentialStatus: "feed_active", ScopeGeneration: "1", RetentionSeconds: 604800, Bootstrap: "forward_only", FieldAuthorization: "fixed_profiles", ProfileTransports: transports, EventCoverage: "forward_only", CurrentRuntimeCoverage: "lease_backed_inventory"})
			return
		}
		if r.Header.Get("Cache-Control") != "no-store" {
			t.Error("snapshot request is cacheable")
		}
		switch r.URL.Path {
		case sourcefacts.BudgetEndpointPath:
			w.Write(catalogs[m])
		case sourcefacts.BudgetEndpointPath + "/current":
			if r.URL.Query().Get("budget_ref") != first.BudgetRef {
				t.Error("wrong source scope")
			}
			w.Write(readings[m])
		default:
			t.Error("budget used event path")
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	reader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: server.URL + sourcefacts.BudgetEndpointPath, ServiceKey: "synthetic-budget-consumer", AllowPrivateTunnelHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	collection, err := f.consumerStore.RegisterBudgetCollection(f.ctx, sourcefacts.BudgetCollection{ID: testUUID(t), SourceSystemID: f.source, SourceInstance: first.SourceInstance, OrgID: first.OrgID, ProjectID: first.ProjectID, ScopeGeneration: "1", Profiles: all})
	if err != nil {
		t.Fatal(err)
	}
	worker := sourcefacts.BudgetConsumer{Store: f.consumerStore, Reader: reader, Expected: collection}
	if _, err = worker.PullOnce(f.ctx); err == nil || calls.Load() != 0 {
		t.Fatal("paused collection contacted source")
	}
	collection, err = f.consumerStore.ControlBudgetCollection(f.ctx, collection.ID, "active", collection.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	pull := func(wantMore bool) {
		t.Helper()
		more, err := worker.PullOnce(f.ctx)
		if err != nil || more != wantMore {
			t.Fatal("consumer progress", more, err)
		}
	}
	pull(true)
	pull(false)
	f.exec(`UPDATE workbench_access_grants SET allow_budget_definition=true,allow_budget_summary=true,allow_budget_alert=true WHERE subject_id=$1`, f.subject)
	create := func() workbench.BudgetReading {
		t.Helper()
		r, err := f.workspace.CreateBudgetReading(f.ctx, f.subject, workbench.BudgetReadingRequest{ContractVersion: workbench.BudgetContractVersion}, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	original := create()
	mode.Store(1)
	if _, err = worker.PullOnce(f.ctx); err == nil {
		t.Fatal("unavailable source accepted")
	}
	items, err := f.workspace.GetBudgetItems(f.ctx, f.subject, original.ID, 25, "", time.Now().UTC())
	if err != nil || len(items.Items) != 1 || items.Items[0].CurrentRetrievalState != "source_unavailable" {
		t.Fatal("outage concealed or historical snapshot lost", items, err)
	}
	mode.Store(0)
	pull(true)
	pull(false)
	items, err = f.workspace.GetBudgetItems(f.ctx, f.subject, original.ID, 25, "", time.Now().UTC())
	if err != nil || items.Items[0].CurrentRetrievalState != "available" {
		t.Fatal("recovery did not clear current outage", err)
	}
	var count int
	if err = f.admin.QueryRow(`SELECT count(*) FROM source_budget_readings WHERE source_system_id=$1`, f.source).Scan(&count); err != nil || count != 1 {
		t.Fatal("replay duplicated source snapshot", count, err)
	}
	mode.Store(2)
	pull(true)
	pull(false)
	if _, err = f.workspace.GetBudgetReading(f.ctx, f.subject, original.ID, time.Now().UTC()); err == nil {
		t.Fatal("old full-profile reading survived source grant shrink")
	}
	subset := create()
	items, err = f.workspace.GetBudgetItems(f.ctx, f.subject, subset.ID, 25, "", time.Now().UTC())
	if err != nil || len(items.Items) != 1 || items.Items[0].Definition.State != "source_profile_not_authorized" {
		t.Fatal("legal source subset not visible", items, err)
	}
	mode.Store(3)
	pull(true)
	if _, err = worker.PullOnce(f.ctx); err == nil {
		t.Fatal("true missing document mistaken for legal shrink")
	}
	collection, err = f.consumerStore.LoadBudgetCollection(f.ctx, collection.ID)
	if err != nil || !collection.RequiresAttention {
		t.Fatal("missing profile not persistent", collection, err)
	}
	before := calls.Load()
	if _, err = worker.PullOnce(f.ctx); err == nil || calls.Load() != before {
		t.Fatal("persistent failure silently retried")
	}
	collection, err = f.consumerStore.ControlBudgetCollection(f.ctx, collection.ID, "active", collection.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	mode.Store(2)
	pull(true)
	mode.Store(4)
	if _, err = worker.PullOnce(f.ctx); err == nil {
		t.Fatal("mid-read source version change accepted")
	}
	collection, err = f.consumerStore.LoadBudgetCollection(f.ctx, collection.ID)
	if err != nil || collection.RequiresAttention || collection.CatalogID != "" {
		t.Fatal("catalog version change did not restart enumeration", collection, err)
	}
	pull(true)
	pull(false)
	collection, err = f.consumerStore.LoadBudgetCollection(f.ctx, collection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.consumerStore.ControlBudgetCollection(f.ctx, collection.ID, "revoked", collection.Revision, false); err != nil {
		t.Fatal(err)
	}
	before = calls.Load()
	if _, err = worker.PullOnce(f.ctx); err == nil || calls.Load() != before {
		t.Fatal("revoked collection contacted source")
	}
}
