// Package testsourcefacts replays retained synthetic UR production-Handler
// pages through XI's actual HTTP reader. It never starts or contacts UR.
package testsourcefacts

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

const Chat = "TestCoordinatorPrepaidSourceAndMoneyIntegration_chat_legacy_shared.json"
const Media = "TestSourceFactsMediaHTTPIntegration_grant_success_replay.json"

type CapturedPage struct {
	Org      string          `json:"org_id"`
	Project  string          `json:"project_id"`
	Profile  string          `json:"profile"`
	Previous string          `json:"previous_cursor"`
	Page     json.RawMessage `json:"page"`
}
type Bundle struct {
	Pages []CapturedPage `json:"pages"`
	I01   []struct {
		Instance string `json:"source_instance"`
		Wire     string `json:"snapshot_wire"`
	} `json:"i01"`
}

func Load(t *testing.T, name string) Bundle {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	raw, e := os.ReadFile(filepath.Join(filepath.Dir(file), "../sourcefacts/contract/captured", name))
	if e != nil {
		t.Fatal(e)
	}
	var b Bundle
	if e = json.Unmarshal(raw, &b); e != nil {
		t.Fatal(e)
	}
	if len(b.Pages) == 0 {
		t.Fatal("empty retained export")
	}
	return b
}

// RetainedPeriod covers the fixture's original event times without rewriting
// captured bytes or depending on the day the suite happens to run.
func RetainedPeriod(t *testing.T, b Bundle) (time.Time, time.Time) {
	t.Helper()
	var first, last time.Time
	for _, captured := range b.Pages {
		var page sourcefacts.Page
		if err := json.Unmarshal(captured.Page, &page); err != nil {
			t.Fatal(err)
		}
		for _, event := range page.Events {
			item, err := sourcefacts.ValidateItem(event, captured.Profile)
			if err != nil {
				t.Fatal(err)
			}
			occurred, err := time.Parse(time.RFC3339Nano, item.Document.OccurredAt)
			if err != nil {
				t.Fatal(err)
			}
			if first.IsZero() || occurred.Before(first) {
				first = occurred
			}
			if last.IsZero() || occurred.After(last) {
				last = occurred
			}
		}
	}
	if first.IsZero() || last.IsZero() {
		t.Fatal("retained fixture has no source timestamps")
	}
	return first.Add(-time.Second), last.Add(time.Second)
}
func RegisterSource(t *testing.T, admin *sql.DB) string {
	t.Helper()
	var id string
	e := admin.QueryRow(`INSERT INTO source_systems(source_key,source_type,display_name,source_version,status) VALUES('a1-captured:'||gen_random_uuid(),'unirouter','Retained synthetic Source Facts','unirouter.completed-execution.v1','active') RETURNING id::text`).Scan(&id)
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func Replay(t *testing.T, admin *sql.DB, importerDSN, source string, bundle Bundle, generation string) {
	t.Helper()
	ctx := t.Context()
	var first sourcefacts.Page
	if json.Unmarshal(bundle.Pages[0].Page, &first) != nil {
		t.Fatal("page")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer retained-synthetic-source-only" {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/api/v1/integrations/xinsight/source-facts/capabilities" {
			if r.URL.RawQuery != "" {
				w.WriteHeader(400)
				return
			}
			json.NewEncoder(w).Encode(sourcefacts.Capabilities{SchemaVersion: "unirouter.source-capabilities.v1", SourceInstance: first.ProducerInstance, Profiles: []string{sourcefacts.ContextProfile, sourcefacts.IdentityProfile, sourcefacts.CostProfile}, ProfileVersion: "1.0.0", Status: "active", CredentialStatus: "feed_active", ScopeGeneration: generation, RetentionSeconds: 604800, Bootstrap: "forward_only", FieldAuthorization: "fixed_profiles"})
			return
		}
		if r.URL.Path != "/api/v1/integrations/xinsight/source-facts" || r.URL.Query().Get("limit") != "50" {
			w.WriteHeader(400)
			return
		}
		for _, p := range bundle.Pages {
			if p.Profile == r.URL.Query().Get("profile") && p.Previous == r.URL.Query().Get("cursor") {
				w.Write(p.Page)
				return
			}
		}
		w.WriteHeader(409)
	}))
	defer server.Close()
	reader, e := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: server.URL + "/api/v1/integrations/xinsight/source-facts", ServiceKey: "retained-synthetic-source-only", AllowPrivateTunnelHTTP: true})
	if e != nil {
		t.Fatal(e)
	}
	store, e := db.OpenSourceFactsStore(importerDSN)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	streams := map[string]string{}
	for _, p := range bundle.Pages {
		BindSyncScope(t, admin, first.ProducerInstance, p.Org, p.Project, true)
		if streams[p.Profile] == "" {
			// This is a new, explicit fixture registration at the captured stream's
			// original position, not a reset of an existing consumer checkpoint.
			var id string
			e = admin.QueryRowContext(ctx, `INSERT INTO source_fact_streams(source_system_id,source_instance,org_id,project_id,profile,scope_generation,status,cursor_value) VALUES($1::uuid,$2,$3,$4,$5,$6,'active',$7) RETURNING id::text`, source, first.ProducerInstance, p.Org, p.Project, p.Profile, generation, p.Previous).Scan(&id)
			if e != nil {
				t.Fatal(e)
			}
			streams[p.Profile] = id
		}
		if _, e = (sourcefacts.Consumer{Store: store, Reader: reader}).PullOnce(ctx, streams[p.Profile]); e != nil {
			t.Fatalf("retained %s page: %v", p.Profile, e)
		}
	}
}
func PersistMoney(t *testing.T, ctx context.Context, importerDSN, source string, b Bundle) {
	t.Helper()
	store, e := db.OpenEvidenceStore(ctx, importerDSN)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	for _, v := range b.I01 {
		mapped, e := unirouter.MapCompletedExecutionV1([]byte(v.Wire), unirouter.CompletedExecutionMapOptions{SourceSystemID: source, SourceInstance: v.Instance, AdapterManifestID: unirouter.DefaultCompletedExecutionAdapterManifestID, ObservedAt: time.Now().UTC(), IngestedAt: time.Now().UTC()})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = store.PersistEvents(ctx, mapped.Events); e != nil {
			t.Fatal(e)
		}
	}
}
