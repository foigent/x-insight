package api_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/testsourcefacts"
)

// This fixture keeps the production managed worker, HTTP source, PostgreSQL,
// production HTTPS API and real browser active together in the same guest.
// Native CLI/systemd/TLS-source behavior is covered by the separate hostctl test.
func startManagedBrowserSource(t *testing.T, admin *sql.DB, importerDSN, output string) (string, func()) {
	t.Helper()
	var golden struct {
		Valid []struct{ Name, Canonical string }
	}
	raw, err := os.ReadFile("../sourcefacts/contract/golden-source-facts.json")
	if err != nil || json.Unmarshal(raw, &golden) != nil {
		t.Fatal("golden source templates", err)
	}
	pages := map[string][]json.RawMessage{}
	now := time.Now().UTC().Add(-time.Minute)
	for _, template := range golden.Valid {
		if template.Name != "context" && template.Name != "identity" && template.Name != "priced_zero" {
			continue
		}
		byRevision := [3][]json.RawMessage{}
		for request := 0; request < 200; request++ {
			requestID := fmt.Sprintf("bounded-managed-%03d", request)
			var doc map[string]any
			if json.Unmarshal([]byte(strings.ReplaceAll(template.Canonical, "golden-request", requestID)), &doc) != nil {
				t.Fatal("template document")
			}
			profile := sourcefacts.Text(doc, "profile")
			id := sourcefacts.SourceID(sourcefacts.Version, "synthetic-ur", "1", "2", profile, requestID, sourcefacts.Text(doc, "stage"))
			doc["source_fact_id"] = id
			doc["occurred_at"] = now.Format(sourcefacts.TimeFormat)
			previous := ""
			for revision := 1; revision <= 3; revision++ {
				r := strconv.Itoa(revision)
				doc["revision"] = r
				doc["event_id"] = sourcefacts.SourceID(sourcefacts.Version+".revision", id, r)
				doc["recorded_at"] = now.Add(time.Duration(revision) * time.Second).Format(sourcefacts.TimeFormat)
				if revision > 1 {
					doc["previous_revision"], doc["previous_hash"], doc["correction_reason"] = strconv.Itoa(revision-1), previous, "source_correction"
				}
				canonical, e := connector.CanonicalizeJSONValue(doc)
				if e != nil {
					t.Fatal(e)
				}
				d, e := sourcefacts.DecodeDocument(canonical)
				if e != nil {
					t.Fatal(template.Name, revision, e)
				}
				event, e := sourcefacts.Envelope(d, canonical)
				if e != nil {
					t.Fatal(e)
				}
				byRevision[revision-1] = append(byRevision[revision-1], event)
				previous = sourcefacts.Hash(canonical)
			}
			if request == 199 {
				// A later revision arrives before its predecessors.
				pages[profile] = append(append(byRevision[2], byRevision[0]...), byRevision[1]...)
			}
		}
	}
	if len(pages) != 3 {
		t.Fatal("three bounded source profiles required")
	}
	source := testsourcefacts.RegisterSource(t, admin)
	if _, err = admin.Exec(`UPDATE source_systems SET display_name='Bounded managed source' WHERE id=$1::uuid`, source); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details,allow_coverage,allow_source_context,allow_source_identity,allow_source_cost) VALUES('xs_8899aabbccddeeff',$1::uuid,true,true,true,true,true,true)`, source); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer synthetic-bounded-managed" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/integrations/xinsight/source-facts/capabilities" {
			_ = json.NewEncoder(w).Encode(sourcefacts.Capabilities{SchemaVersion: "unirouter.source-capabilities.v1", SourceInstance: "synthetic-ur", Profiles: []string{sourcefacts.ContextProfile, sourcefacts.IdentityProfile, sourcefacts.CostProfile}, ProfileVersion: "1.0.0", Status: "active", CredentialStatus: "feed_active", ScopeGeneration: "1", RetentionSeconds: 604800, Bootstrap: "forward_only", FieldAuthorization: "fixed_profiles"})
			return
		}
		events, ok := pages[r.URL.Query().Get("profile")]
		if r.URL.Path != "/api/v1/integrations/xinsight/source-facts" || !ok || r.URL.Query().Get("limit") != "50" {
			w.WriteHeader(400)
			return
		}
		page := sourcefacts.Page{BatchVersion: connector.EventBatchVersionV1, ConnectorID: sourcefacts.ConnectorID, ConnectorVersion: sourcefacts.ConnectorVersion, ProducerInstance: "synthetic-ur", GeneratedAt: time.Now().UTC().Format(sourcefacts.TimeFormat), Events: []json.RawMessage{}, NextCursor: "bounded:0", HighWatermark: "bounded:0"}
		if cursor := r.URL.Query().Get("cursor"); cursor != "" {
			n, e := strconv.Atoi(strings.TrimPrefix(cursor, "bounded:"))
			if e != nil || n < 0 || n > len(events) {
				w.WriteHeader(409)
				return
			}
			end := min(n+50, len(events))
			page.Events = events[n:end]
			page.NextCursor, page.HighWatermark, page.HasMore = fmt.Sprintf("bounded:%d", end), "bounded:600", end < len(events)
		}
		_ = json.NewEncoder(w).Encode(page)
	}))
	reader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: feed.URL + "/api/v1/integrations/xinsight/source-facts", ServiceKey: "synthetic-bounded-managed", AllowPrivateTunnelHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	store, err := db.OpenSourceFactsStore(importerDSN)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	worker := sourcefacts.ManagedWorker{PollInterval: time.Second, RetryMaximum: 4 * time.Second}
	for _, profile := range []string{sourcefacts.ContextProfile, sourcefacts.IdentityProfile, sourcefacts.CostProfile} {
		var id string
		if err = admin.QueryRow(`INSERT INTO source_fact_streams(source_system_id,source_instance,org_id,project_id,profile,scope_generation,status) VALUES($1::uuid,'synthetic-ur','1','2',$2,'1','active') RETURNING id::text`, source, profile).Scan(&id); err != nil {
			t.Fatal(err)
		}
		worker.Profiles = append(worker.Profiles, sourcefacts.ManagedProfile{StreamID: id, Consumer: sourcefacts.Consumer{Store: store, Reader: reader}})
	}
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	return source, func() {
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Error("managed browser worker", err)
		}
		_ = store.Close()
		feed.Close()
		var count, distinct int
		if err := admin.QueryRow(`SELECT count(*),count(DISTINCT source_fact_id) FROM source_fact_documents WHERE source_system_id=$1::uuid`, source).Scan(&count, &distinct); err != nil || count != 1800 || distinct != 600 {
			t.Error("bounded managed counts", count, distinct, err)
		}
		report, _ := json.MarshalIndent(map[string]any{"documents": count, "source_facts": distinct, "requests": 200, "profiles": 3, "revisions": 3, "source_http_requests": requests.Load(), "same_database_as_browser": true, "source_transport": "explicit private loopback HTTP; separate native TLS-source proof retained"}, "", "  ")
		if err := os.WriteFile(filepath.Join(output, "bounded-managed-result.json"), report, 0600); err != nil {
			t.Error(err)
		}
	}
}
