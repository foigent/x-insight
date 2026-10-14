package api_test

import (
	"encoding/json"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/testsourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func TestSourceFactsProductionHTTPCurrentFieldScope(t *testing.T) {
	t.Setenv("XINSIGHT_A0_RUNTIME_ROLES", "1")
	server, admin, _, _ := startWorkbenchProduction(t)
	var name string
	if e := admin.QueryRow(`SELECT current_database()`).Scan(&name); e != nil {
		t.Fatal(e)
	}
	u, e := url.Parse(os.Getenv("XINSIGHT_POSTGRES_DSN"))
	if e != nil {
		t.Fatal(e)
	}
	u.Path = "/" + name
	u.User = workbenchRuntimeRoleUser(t, "importer")
	source := testsourcefacts.RegisterSource(t, admin)
	b := testsourcefacts.Load(t, testsourcefacts.Chat)
	if _, e = admin.Exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details,allow_coverage,allow_preferences) VALUES('xs_8899aabbccddeeff',$1::uuid,true,true,true,true)`, source); e != nil {
		t.Fatal(e)
	}
	testsourcefacts.Replay(t, admin, u.String(), source, b, "1")
	c := loginWorkbenchHTTP(t, server, "workbench.one", "workbench password")
	request := httpReadingRequest(source)
	first, last := testsourcefacts.RetainedPeriod(t, b)
	request.Period.RelativeHours = 0
	request.Period.StartInclusive = &first
	request.Period.EndExclusive = &last
	read := func() workbench.Reading {
		var r workbench.Reading
		if json.Unmarshal(c.request(t, "POST", "/api/v1/workbench/readings", request, 201), &r) != nil {
			t.Fatal("reading")
		}
		return r
	}
	old := read()
	var oldStored string
	if err := admin.QueryRow(`SELECT to_jsonb(r)::text FROM workbench_readings r WHERE id=$1`, old.ID).Scan(&oldStored); err != nil {
		t.Fatal(err)
	}
	if old.SourceCoverage != nil || old.SourceManifestRef != "" {
		t.Fatal("default grant exposed new fields")
	}
	path := func(r workbench.Reading) string { return "/api/v1/workbench/readings/" + r.ID }
	c.request(t, "GET", path(old)+"/source-facts", nil, 403)
	if _, e = admin.Exec(`UPDATE workbench_access_grants SET allow_source_context=true,allow_source_identity=true,allow_source_cost=true WHERE subject_id='xs_8899aabbccddeeff' AND source_system_id=$1::uuid`, source); e != nil {
		t.Fatal(e)
	}
	pending := read()
	if pending.SourceCoverage == nil || pending.SourceCoverage.KnownDocuments != 15 || pending.SourceCoverage.EconomicNotReceived != 15 {
		t.Fatalf("source first: %+v", pending.SourceCoverage)
	}
	if !slices.Equal(pending.SourceCoverage.CollectionStates, []string{"active"}) {
		t.Fatal("active collection missing")
	}
	if _, e = admin.Exec(`UPDATE source_fact_streams SET revision=revision+1,status='paused',last_error='source_fact_conflict' WHERE source_system_id=$1::uuid AND profile=$2`, source, sourcefacts.IdentityProfile); e != nil {
		t.Fatal(e)
	}
	stopped := read()
	if !slices.Contains(stopped.SourceCoverage.CollectionStates, "paused") || !slices.Contains(stopped.SourceCoverage.Gaps, "source_fact_conflict") {
		t.Fatal("HTTP hides stopped collection/conflict")
	}
	var unchanged workbench.Reading
	json.Unmarshal(c.request(t, "GET", path(pending), nil, 200), &unchanged)
	if !slices.Equal(unchanged.SourceCoverage.CollectionStates, []string{"active"}) || slices.Contains(unchanged.SourceCoverage.Gaps, "source_fact_conflict") {
		t.Fatal("old HTTP reading backfilled")
	}
	if _, e = admin.Exec(`UPDATE source_fact_streams SET revision=revision+1,status='active',last_error='' WHERE source_system_id=$1::uuid`, source); e != nil {
		t.Fatal(e)
	}
	// Adding capabilities advances the grant revision. The earlier reading
	// remains immutable but cannot be served under the new authorization.
	c.request(t, "GET", path(old), nil, 409)
	var oldAfter string
	if err := admin.QueryRow(`SELECT to_jsonb(r)::text FROM workbench_readings r WHERE id=$1`, old.ID).Scan(&oldAfter); err != nil || oldAfter != oldStored {
		t.Fatal("authorization change rewrote the earlier reading", err)
	}
	var sum workbench.Summary
	json.Unmarshal(c.request(t, "GET", path(pending)+"/summary", nil, 200), &sum)
	if sum.RequestCount != 0 || sum.SelectedAmountMicros != "0" || sum.AmountComplete {
		t.Fatal("unreceived money treated as complete", sum)
	}
	c.request(t, "GET", path(pending)+"/source-facts?limit=0", nil, 400)
	c.request(t, "GET", path(pending)+"/source-facts?limit=x", nil, 400)
	c.request(t, "GET", path(pending)+"/source-facts?unknown=1", nil, 400)
	c.request(t, "GET", path(pending)+"/source-facts?cursor=unbound", nil, 400)
	total := 0
	cursor := ""
	for {
		var p workbench.SourceObservations
		json.Unmarshal(c.request(t, "GET", path(pending)+"/source-facts?limit=2&cursor="+url.QueryEscape(cursor), nil, 200), &p)
		total += len(p.Items)
		if p.ManifestRef != pending.SourceManifestRef {
			t.Fatal("mixed manifest")
		}
		cursor = p.NextCursor
		if cursor == "" {
			break
		}
	}
	if total != 15 {
		t.Fatal("source pagination", total)
	}
	testsourcefacts.PersistMoney(t, t.Context(), u.String(), source, b)
	testsourcefacts.Replay(t, admin, u.String(), source, b, "2") // restart/rotation into a new explicit generation
	current := read()
	json.Unmarshal(c.request(t, "GET", path(current)+"/summary", nil, 200), &sum)
	if sum.RequestCount != 1 || sum.SelectedAmountMicros != "700" || sum.CalculatedReferenceMicros != "700" {
		t.Fatalf("economic once: %+v", sum)
	}
	var prior workbench.Summary
	json.Unmarshal(c.request(t, "GET", path(pending)+"/summary", nil, 200), &prior)
	if prior.RequestCount != 0 || prior.SourceCoverage.EconomicNotReceived != 15 {
		t.Fatal("old reading changed")
	}
	if _, e = admin.Exec(`UPDATE source_fact_streams SET revision=revision+1,status='revoked',last_error='source_fact_conflict' WHERE source_system_id=$1::uuid AND profile=$2`, source, sourcefacts.IdentityProfile); e != nil {
		t.Fatal(e)
	}
	if _, e = admin.Exec(`UPDATE workbench_access_grants SET allow_source_identity=false WHERE subject_id='xs_8899aabbccddeeff' AND source_system_id=$1::uuid`, source); e != nil {
		t.Fatal(e)
	}
	for _, endpoint := range []string{"", "/summary", "/source-facts", "/contributions?dimension=account", "/series", "/sources"} {
		c.request(t, "GET", path(current)+endpoint, nil, 409)
	}
	denied := read()
	if !slices.Equal(denied.SourceCoverage.CollectionStates, []string{"active"}) || slices.Contains(denied.SourceCoverage.Gaps, "source_fact_conflict") {
		t.Fatal("hidden profile status leak")
	}
	if denied.SourceCoverage.KnownDocuments != 8 {
		t.Fatal("denied identity count leak")
	}
	c.request(t, "GET", path(denied)+"/contributions?dimension=account", nil, 403)
	raw := c.request(t, "GET", path(denied)+"/source-facts", nil, 200)
	if strings.Contains(string(raw), sourcefacts.IdentityProfile) || strings.Contains(string(raw), "owner_user_id") {
		t.Fatal("identity detail leak")
	}
	request.Filters.SourceProjects = []string{"project-999"}
	empty := read()
	if len(empty.SourceCoverage.CollectionStates) != 0 || slices.Contains(empty.SourceCoverage.Gaps, "source_fact_conflict") {
		t.Fatal("cross project status leak")
	}
	if empty.SourceCoverage.KnownDocuments != 0 {
		t.Fatal("cross project source leak")
	}
	other := loginWorkbenchHTTP(t, server, "reader.0", "synthetic password")
	other.request(t, "GET", path(denied)+"/source-facts", nil, 404)
	for _, endpoint := range []string{"/api/v1/cases", "/api/v1/executions", "/api/v1/saved-views"} {
		c.request(t, "GET", endpoint, nil, 403)
	}
	t.Log("15 captured source documents via real HTTP reader/importer role; zero-money reading; replay with explicit generation; exactly one 700-micro I-01; old reading stable; default-deny/current revoke/project/principal/export boundaries")
}
