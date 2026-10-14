package db_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func budgetFixtureReading(t *testing.T, id int, profiles []string, mutate func(map[string]any)) []byte {
	t.Helper()
	raw, err := os.ReadFile("../sourcefacts/contract/budgets/golden-readings.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Positive []json.RawMessage }
	if err = json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	m, err := connector.DecodeStrictJSONObject(v.Positive[0])
	if err != nil {
		t.Fatal(err)
	}
	m["reading_id"] = fmt.Sprintf("ur-budget-reading-%032x", id)
	docs := []any{}
	for _, item := range m["documents"].([]any) {
		w := item.(map[string]any)
		if slices.Contains(profiles, sourcefacts.Text(w, "profile")) {
			docs = append(docs, w)
		}
	}
	m["documents"] = docs
	if mutate != nil {
		mutate(m)
	}
	for _, item := range docs {
		w := item.(map[string]any)
		d := w["document"].(map[string]any)
		for _, key := range []string{"reading_id", "reading_cutoff", "definition_revision", "binding_revision"} {
			d[key] = m[key]
		}
		b, err := connector.CanonicalizeJSONValue(d)
		if err != nil {
			t.Fatal(err)
		}
		w["source_sha256"] = sourcefacts.Hash(b)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func budgetFixtureCatalog(t *testing.T, r sourcefacts.BudgetReading) []byte {
	t.Helper()
	c := sourcefacts.BudgetCatalog{SchemaVersion: sourcefacts.BudgetCatalogVersion, SourceInstance: r.SourceInstance, OrgID: r.OrgID, ProjectID: r.ProjectID, ReadingCutoff: r.ReadingCutoff, AccessGeneration: r.AccessGeneration, CatalogRevision: r.CatalogRevision, Profiles: r.Profiles(), EnterpriseScope: "scope_not_configured", Entries: []sourcefacts.BudgetCatalogEntry{{BudgetRef: r.BudgetRef, Origin: "gateway_project", DefinitionRevision: r.DefinitionRevision, BindingRevision: r.BindingRevision}}}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBudgetSnapshotsAtomicSubsetsAndLifecycle(t *testing.T) {
	f := newSourceFixture(t)
	all := []string{sourcefacts.BudgetDefinitionProfile, sourcefacts.BudgetSummaryProfile, sourcefacts.BudgetAlertProfile}
	now := time.Now().UTC()
	load := func(t *testing.T, id string) sourcefacts.BudgetCollection {
		t.Helper()
		s, err := f.consumerStore.LoadBudgetCollection(f.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	for mask := 1; mask < 8; mask++ {
		t.Run(fmt.Sprint(mask), func(t *testing.T) {
			var profiles []string
			for i, p := range all {
				if mask&(1<<i) != 0 {
					profiles = append(profiles, p)
				}
			}
			raw := budgetFixtureReading(t, mask, profiles, nil)
			r, err := sourcefacts.DecodeBudgetReading(raw)
			if err != nil {
				t.Fatal(err)
			}
			s := sourcefacts.BudgetCollection{ID: testUUID(t), SourceSystemID: f.source, SourceInstance: r.SourceInstance, OrgID: r.OrgID, ProjectID: r.ProjectID, ScopeGeneration: "1", Profiles: all}
			s, err = f.consumerStore.RegisterBudgetCollection(f.ctx, s)
			if err != nil {
				t.Fatal(err)
			}
			if s.Status != "paused" {
				t.Fatal("registration silently activated")
			}
			if err = f.consumerStore.CommitBudgetCatalog(f.ctx, s, budgetFixtureCatalog(t, r), now); !errors.Is(err, sourcefacts.ErrStreamChanged) {
				t.Fatal("paused collection admitted", err)
			}
			s, err = f.consumerStore.ControlBudgetCollection(f.ctx, s.ID, "active", s.Revision, false)
			if err != nil {
				t.Fatal(err)
			}
			if err = f.consumerStore.CommitBudgetCatalog(f.ctx, s, budgetFixtureCatalog(t, r), now); err != nil {
				t.Fatal(err)
			}
			s = load(t, s.ID)
			if err = f.consumerStore.CommitBudgetReading(f.ctx, s, raw, now); err != nil {
				t.Fatal(err)
			}
			s = load(t, s.ID)
			if s.VisibleCycle != 1 || s.CatalogID != "" || !sourcefacts.SameBudgetProfiles(s.GrantedProfiles, profiles) {
				t.Fatal("incomplete catalog visibility", s)
			}
			// Canonical replay in a later poll adds a receipt, not another financial row.
			if err = f.consumerStore.CommitBudgetCatalog(f.ctx, s, budgetFixtureCatalog(t, r), now); err != nil {
				t.Fatal(err)
			}
			s = load(t, s.ID)
			if err = f.consumerStore.CommitBudgetReading(f.ctx, s, raw, now); err != nil {
				t.Fatal(err)
			}
			var count int
			if err = f.admin.QueryRow(`SELECT count(*) FROM source_budget_readings WHERE source_system_id=$1::uuid AND reading_id=$2`, f.source, r.ReadingID).Scan(&count); err != nil || count != 1 {
				t.Fatal("replay duplicated snapshot", count, err)
			}
			// A pause observed before a slow request commits wins without data loss.
			before := load(t, s.ID)
			paused, err := f.consumerStore.ControlBudgetCollection(f.ctx, s.ID, "paused", before.Revision, false)
			if err != nil {
				t.Fatal(err)
			}
			if err = f.consumerStore.CommitBudgetCatalog(f.ctx, before, budgetFixtureCatalog(t, r), now); !errors.Is(err, sourcefacts.ErrStreamChanged) {
				t.Fatal("in-flight request survived pause", err)
			}
			revoked, err := f.consumerStore.ControlBudgetCollection(f.ctx, s.ID, "revoked", paused.Revision, false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.consumerStore.ControlBudgetCollection(f.ctx, s.ID, "active", revoked.Revision, true); !errors.Is(err, sourcefacts.ErrStreamChanged) {
				t.Fatal("revoked collection revived", err)
			}
		})
	}
}

func TestBudgetMissingExpectedConflictAndGrantShrink(t *testing.T) {
	f := newSourceFixture(t)
	now := time.Now().UTC()
	all := []string{sourcefacts.BudgetDefinitionProfile, sourcefacts.BudgetSummaryProfile, sourcefacts.BudgetAlertProfile}
	raw := budgetFixtureReading(t, 10, all, nil)
	r, err := sourcefacts.DecodeBudgetReading(raw)
	if err != nil {
		t.Fatal(err)
	}
	s := sourcefacts.BudgetCollection{ID: testUUID(t), SourceSystemID: f.source, SourceInstance: r.SourceInstance, OrgID: r.OrgID, ProjectID: r.ProjectID, ScopeGeneration: "1", Profiles: all}
	s, err = f.consumerStore.RegisterBudgetCollection(f.ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	s, err = f.consumerStore.ControlBudgetCollection(f.ctx, s.ID, "active", s.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	load := func() {
		t.Helper()
		s, err = f.consumerStore.LoadBudgetCollection(f.ctx, s.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	catalog := budgetFixtureCatalog(t, r)
	if err = f.consumerStore.CommitBudgetCatalog(f.ctx, s, catalog, now); err != nil {
		t.Fatal(err)
	}
	load()
	missing := budgetFixtureReading(t, 11, all[:1], nil)
	if err = f.consumerStore.CommitBudgetReading(f.ctx, s, missing, now); !errors.Is(err, sourcefacts.ErrScopeChanged) {
		t.Fatal("missing expected document accepted", err)
	}
	before := s.Revision
	load()
	if before != s.Revision {
		t.Fatal("rejected reading advanced collection")
	}
	// Concurrent retries serialize under the existing complete-source lock.
	errorsOut := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errorsOut <- f.consumerStore.CommitBudgetReading(f.ctx, s, raw, now) }()
	}
	wg.Wait()
	close(errorsOut)
	accepted, stale := 0, 0
	for err := range errorsOut {
		if err == nil {
			accepted++
		} else if errors.Is(err, sourcefacts.ErrStreamChanged) {
			stale++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || stale != 1 {
		t.Fatal("concurrent snapshot admission", accepted, stale)
	}
	load()
	if err = f.consumerStore.CommitBudgetCatalog(f.ctx, s, catalog, now); err != nil {
		t.Fatal(err)
	}
	load()
	conflict := budgetFixtureReading(t, 10, all, func(m map[string]any) {
		m["documents"].([]any)[2].(map[string]any)["document"].(map[string]any)["body"].(map[string]any)["entries"].([]any)[0].(map[string]any)["status"] = "resolved"
	})
	if err = f.consumerStore.CommitBudgetReading(f.ctx, s, conflict, now); !errors.Is(err, sourcefacts.ErrConflict) {
		t.Fatal("immutable reading identity changed", err)
	}
	if err = f.consumerStore.RecordBudgetFailure(f.ctx, s, "budget_catalog_changed", now); err != nil {
		t.Fatal(err)
	}
	load()
	shrink := budgetFixtureReading(t, 12, all[:1], nil)
	rr, err := sourcefacts.DecodeBudgetReading(shrink)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.consumerStore.CommitBudgetCatalog(f.ctx, s, budgetFixtureCatalog(t, rr), now); !errors.Is(err, sourcefacts.ErrConflict) {
		t.Fatal("same-generation grant set changed", err)
	}
	shrink = budgetFixtureReading(t, 12, all[:1], func(m map[string]any) { m["access_generation"] = "2" })
	rr, err = sourcefacts.DecodeBudgetReading(shrink)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.consumerStore.CommitBudgetCatalog(f.ctx, s, budgetFixtureCatalog(t, rr), now); err != nil {
		t.Fatal("legitimate shrink rejected", err)
	}
	load()
	if s.VisibleCycle != 0 || s.RequiresAttention {
		t.Fatal("new grant generation did not isolate prior visibility")
	}
	if err = f.consumerStore.CommitBudgetReading(f.ctx, s, shrink, now); err != nil {
		t.Fatal(err)
	}
	load()
	if !sourcefacts.SameBudgetProfiles(s.GrantedProfiles, all[:1]) || s.VisibleCycle == 0 {
		t.Fatal("legal subset did not become visible")
	}
}

func TestBudgetFrozenVersionHistoryAndCurrentPermissions(t *testing.T) {
	f := newSourceFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	all := []string{sourcefacts.BudgetDefinitionProfile, sourcefacts.BudgetSummaryProfile, sourcefacts.BudgetAlertProfile}
	raw := budgetFixtureReading(t, 30, all, nil)
	r, err := sourcefacts.DecodeBudgetReading(raw)
	if err != nil {
		t.Fatal(err)
	}
	s := sourcefacts.BudgetCollection{ID: testUUID(t), SourceSystemID: f.source, SourceInstance: r.SourceInstance, OrgID: r.OrgID, ProjectID: r.ProjectID, ScopeGeneration: "1", Profiles: all}
	s, err = f.consumerStore.RegisterBudgetCollection(f.ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	s, err = f.consumerStore.ControlBudgetCollection(f.ctx, s.ID, "active", s.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	put := func(raw []byte, at time.Time) {
		t.Helper()
		r, e := sourcefacts.DecodeBudgetReading(raw)
		if e != nil {
			t.Fatal(e)
		}
		s, e = f.consumerStore.LoadBudgetCollection(f.ctx, s.ID)
		if e != nil {
			t.Fatal(e)
		}
		if e = f.consumerStore.CommitBudgetCatalog(f.ctx, s, budgetFixtureCatalog(t, r), at); e != nil {
			t.Fatal(e)
		}
		s, e = f.consumerStore.LoadBudgetCollection(f.ctx, s.ID)
		if e != nil {
			t.Fatal(e)
		}
		if e = f.consumerStore.CommitBudgetReading(f.ctx, s, raw, at); e != nil {
			t.Fatal(e)
		}
	}
	put(raw, now)
	request := workbench.BudgetReadingRequest{ContractVersion: workbench.BudgetContractVersion}
	if _, err = f.workspace.CreateBudgetReading(f.ctx, f.subject, request, now.Add(time.Second)); !errors.Is(err, workbench.ErrScopeForbidden) {
		t.Fatal("old cost grants acquired D", err)
	}
	f.exec(`UPDATE workbench_access_grants SET allow_budget_definition=true,allow_budget_summary=true,allow_budget_alert=true WHERE subject_id=$1`, f.subject)
	old, err := f.workspace.CreateBudgetReading(f.ctx, f.subject, request, now.Add(time.Second))
	if err != nil || old.BudgetCount != 1 {
		t.Fatal("freeze budget", old.BudgetCount, err)
	}
	items, err := f.workspace.GetBudgetItems(f.ctx, f.subject, old.ID, 10, "", now.Add(2*time.Second))
	if err != nil || len(items.Items) != 1 {
		t.Fatal("read frozen budget", err)
	}
	ref := items.Items[0].Ref
	if got := sourcefacts.Text(sourcefacts.Object(items.Items[0].Definition.Body, "amount"), "value_minor"); got != "1000000000" {
		t.Fatal(got)
	}
	changed := budgetFixtureReading(t, 31, all, func(m map[string]any) {
		m["definition_revision"] = "3"
		m["catalog_revision"] = "3"
		d := m["documents"].([]any)[0].(map[string]any)["document"].(map[string]any)["body"].(map[string]any)
		d["revision"] = "3"
		d["amount"].(map[string]any)["value_minor"] = "1500000000"
		c := m["documents"].([]any)[1].(map[string]any)["document"].(map[string]any)["body"].(map[string]any)["comparison"].(map[string]any)
		c["utilization_denominator"] = "1500000000"
		c["remaining"].(map[string]any)["value_minor"] = "900000000"
	})
	put(changed, now.Add(3*time.Second))
	items, err = f.workspace.GetBudgetItems(f.ctx, f.subject, old.ID, 10, "", now.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if sourcefacts.Text(sourcefacts.Object(items.Items[0].Definition.Body, "amount"), "value_minor") != "1000000000" {
		t.Fatal("later definition rewrote frozen comparison")
	}
	history, err := f.workspace.GetBudgetHistory(f.ctx, f.subject, old.ID, ref, 10, "", now.Add(4*time.Second))
	if err != nil || len(history.Items) != 1 {
		t.Fatal("later arrival filled old history", len(history.Items), err)
	}
	latest, err := f.workspace.CreateBudgetReading(f.ctx, f.subject, request, now.Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	items, err = f.workspace.GetBudgetItems(f.ctx, f.subject, latest.ID, 10, "", now.Add(6*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if sourcefacts.Text(sourcefacts.Object(items.Items[0].Definition.Body, "amount"), "value_minor") != "1500000000" {
		t.Fatal("new budget definition not selected")
	}
	history, err = f.workspace.GetBudgetHistory(f.ctx, f.subject, latest.ID, items.Items[0].Ref, 1, "", now.Add(6*time.Second))
	if err != nil || len(history.Items) != 1 || history.NextCursor == "" {
		t.Fatal("version history pagination", err)
	}
	more, err := f.workspace.GetBudgetHistory(f.ctx, f.subject, latest.ID, items.Items[0].Ref, 1, history.NextCursor, now.Add(6*time.Second))
	if err != nil || len(more.Items) != 1 || more.Items[0].DefinitionRevision != "2" {
		t.Fatal("old definition history", err)
	}
	if _, err = f.workspace.GetBudgetItems(f.ctx, f.subject, latest.ID, 10, history.NextCursor, now.Add(6*time.Second)); !errors.Is(err, workbench.ErrInvalid) {
		t.Fatal("history cursor crossed query")
	}
	if _, err = f.admin.Exec(`INSERT INTO budget_reading_members(reading_id,source_reading_id,collection_id,receipt_catalog_id,ordinal,profiles,source_state) SELECT reading_id,source_reading_id,collection_id,receipt_catalog_id,ordinal+99,profiles,source_state FROM budget_reading_members WHERE reading_id=$1`, old.ID); err == nil {
		t.Fatal("sealed reading accepted new membership")
	}
	if err = db.ValidateWorkbenchRestore(f.ctx, f.admin); err != nil {
		t.Fatal("budget restore consistency", err)
	}
	f.exec(`UPDATE workbench_access_grants SET allow_budget_definition=false,allow_budget_alert=false WHERE subject_id=$1`, f.subject)
	if _, err = f.workspace.GetBudgetItems(f.ctx, f.subject, old.ID, 10, "", now.Add(7*time.Second)); !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatal("revocation left old amounts accessible", err)
	}
	only, err := f.workspace.CreateBudgetReading(f.ctx, f.subject, request, now.Add(8*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	items, err = f.workspace.GetBudgetItems(f.ctx, f.subject, only.ID, 10, "", now.Add(9*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if items.Items[0].Definition.Body != nil || len(sourcefacts.Object(items.Items[0].Summary.Body, "comparison")) != 1 {
		t.Fatal("summary-only reading inferred hidden budget")
	}
	if err = db.SuspendRestoredSourceFacts(f.ctx, f.admin); err != nil {
		t.Fatal(err)
	}
	if _, err = f.workspace.GetBudgetReading(f.ctx, f.subject, only.ID, now.Add(10*time.Second)); !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatal("restored reading bypassed source refresh", err)
	}
	live, err := f.consumerStore.LoadBudgetCollection(f.ctx, s.ID)
	if err != nil || live.Status != "paused" || !live.RequiresAttention {
		t.Fatal("restored D auto-resumed", err)
	}
}
