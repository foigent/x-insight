package api_test

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func TestRuntimeCurrentHTTPImporterWorkspaceAndRevocation(t *testing.T) {
	t.Setenv("XINSIGHT_A0_RUNTIME_ROLES", "1")
	server, admin, _, source := startWorkbenchProduction(t)
	c := loginWorkbenchHTTP(t, server, "workbench.one", "workbench password")
	c.request(t, "GET", "/api/v1/runtime/current", nil, 403)
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
	s, err := owner.RegisterSourceFactsStream(t.Context(), sourcefacts.Stream{ID: "12345678-1234-4234-8234-123456789abc", SourceSystemID: source, SourceInstance: "synthetic-ur", OrgID: "1", ProjectID: "2", Profile: sourcefacts.RuntimeProfile, ScopeGeneration: "1"})
	if err != nil {
		t.Fatal(err)
	}
	s, err = owner.ControlSourceFactsStream(t.Context(), s.ID, "active", s.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	u.User = workbenchRuntimeRoleUser(t, "importer")
	importer, err := db.OpenSourceFactsStore(u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer importer.Close()
	now := time.Now().UTC().Add(-time.Second).Truncate(time.Microsecond)
	entry := sourcefacts.CurrentRuntimeEntry{RequestID: "native-current", Family: "stream", SubjectKind: "provider_attempt", SubjectID: "attempt-native", Operation: "openai.chat_completions.v1", Freshness: "lease_valid_execution", OriginalState: "streaming", LastObservedAt: now.Format(sourcefacts.TimeFormat), ValidUntil: now.Add(time.Hour).Format(sourcefacts.TimeFormat), LeaseGeneration: "1", EvidenceRef: "uf1_" + strings.Repeat("a", 64)}
	p := sourcefacts.CurrentRuntimePage{SchemaVersion: sourcefacts.CurrentRuntimeVersion, SourceInstance: s.SourceInstance, OrgID: s.OrgID, ProjectID: s.ProjectID, Profile: sourcefacts.RuntimeProfile, ObservedAt: now.Format(sourcefacts.TimeFormat), Coverage: "lease_backed_inventory", Uncovered: []string{"synchronous_requests_without_execution_lease", "progress_between_lease_observations"}, Entries: []sourcefacts.CurrentRuntimeEntry{entry}}
	wire, _ := json.Marshal(p)
	m, err := connector.DecodeStrictJSONObject(wire)
	if err != nil {
		t.Fatal(err)
	}
	unsigned, err := connector.CanonicalizeJSONValue(m)
	if err != nil {
		t.Fatal(err)
	}
	p.SourceSHA256 = sourcefacts.Hash(unsigned)
	wire, _ = json.Marshal(p)
	cur, err := importer.LoadCurrentRuntimeCursor(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	if err = importer.CommitCurrentRuntimePage(t.Context(), s, cur, wire, time.Now().UTC()); err != nil {
		t.Fatal("actual importer commit/prune", err)
	}
	if _, err = admin.Exec(`UPDATE workbench_access_grants SET allow_source_runtime=true WHERE subject_id='xs_8899aabbccddeeff'`); err != nil {
		t.Fatal(err)
	}
	raw := c.request(t, "GET", "/api/v1/runtime/current", nil, 200)
	var result workbench.RuntimeCurrentInventory
	if err = json.Unmarshal(raw, &result); err != nil || len(result.Items) != 1 || result.Items[0].CurrentState != "lease_valid_execution" {
		t.Fatal("workspace current", string(raw), err)
	}
	c.request(t, "GET", "/api/v1/runtime/current?entry_time=all", nil, 400)
	if err = importer.RecordSourceFactsFailure(t.Context(), s, "cursor_expired", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	historicalGap, err := importer.LoadSourceFactsStream(t.Context(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	cur, err = importer.LoadCurrentRuntimeCursor(t.Context(), historicalGap)
	if err != nil {
		t.Fatal(err)
	}
	if err = importer.CommitCurrentRuntimePage(t.Context(), historicalGap, cur, wire, time.Now().UTC()); err != nil {
		t.Fatal("historical gap blocked independent current snapshot", err)
	}
	raw = c.request(t, "GET", "/api/v1/runtime/current", nil, 200)
	if err = json.Unmarshal(raw, &result); err != nil || len(result.Items) != 1 || result.Items[0].CurrentState != "lease_valid_execution" {
		t.Fatal("historical gap hid valid lease", string(raw), err)
	}
	resumed, err := owner.ControlSourceFactsStream(t.Context(), s.ID, "active", historicalGap.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = importer.RecordSourceFactsFailure(t.Context(), resumed, "source_fact_conflict", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	raw = c.request(t, "GET", "/api/v1/runtime/current", nil, 200)
	if err = json.Unmarshal(raw, &result); err != nil || len(result.Items) != 1 || result.Items[0].CurrentState != "not_current" {
		t.Fatal("shared trust failure ignored", string(raw), err)
	}
	c.request(t, "GET", "/api/v1/runtime/current?limit=0", nil, 400)
	c.request(t, "GET", "/api/v1/runtime/current?source_projects=project-2,project-2", nil, 400)
	if _, err = admin.Exec(`UPDATE workbench_access_grants SET allow_source_runtime=false WHERE subject_id='xs_8899aabbccddeeff'`); err != nil {
		t.Fatal(err)
	}
	c.request(t, "GET", "/api/v1/runtime/current", nil, 403)
	// The new DELETE grant is confined to disposable inventory pages.
	var forbidden bool
	if err = admin.QueryRow(`SELECT bool_or(has_table_privilege('xinsight_importer',x,'DELETE')) FROM unnest(ARRAY['source_observation_documents','source_fact_documents','source_fact_streams','source_runtime_snapshots','workbench_economic_facts']) x`).Scan(&forbidden); err != nil || forbidden {
		t.Fatal("importer deletion widened beyond cache", forbidden, err)
	}
}
