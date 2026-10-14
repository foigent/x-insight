package pullfeed

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

func TestBudgetTransportScopedPathsNoStoreAndBoundedBody(t *testing.T) {
	var mode atomic.Int32
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer synthetic-budget-only" {
			t.Error("missing source-only credential")
		}
		if r.URL.Path != sourcefacts.BudgetEndpointPath && r.URL.Path != sourcefacts.BudgetEndpointPath+"/current" && r.URL.Path != "/api/v1/integrations/xinsight/source/v2/capabilities" {
			t.Error("wrong D route")
		}
		if strings.HasSuffix(r.URL.Path, "/current") {
			if r.URL.Query().Get("budget_ref") != "enterprise-project:范围 & 7" || r.URL.Query().Get("alerts_cursor") != "opaque&=/范围" || r.URL.Query().Has("cursor") || r.URL.Query().Has("profile") {
				t.Error("budget/alert position changed")
			}
		} else if r.URL.Path == sourcefacts.BudgetEndpointPath && r.URL.Query().Get("cursor") != "catalog&next" {
			t.Error("catalog cursor changed")
		}
		if r.URL.Path != "/api/v1/integrations/xinsight/source/v2/capabilities" && r.Header.Get("Cache-Control") != "no-store" {
			t.Error("current snapshot request cacheable")
		}
		if mode.Load() != 1 {
			w.Header().Set("Cache-Control", "private, no-store")
		}
		w.Header().Set("Content-Type", "application/json")
		if mode.Load() == 2 {
			fmt.Fprint(w, strings.Repeat(" ", sourcefacts.MaxBudgetReadingBytes+1))
		} else {
			fmt.Fprint(w, `{}`)
		}
	}))
	defer server.Close()
	s, err := NewHTTPSource(HTTPSourceConfig{Endpoint: server.URL + sourcefacts.BudgetEndpointPath, ServiceKey: "synthetic-budget-only", AllowPrivateTunnelHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.FetchSourceCapabilities(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.FetchBudgetCatalog(t.Context(), "catalog&next"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.FetchBudgetReading(t.Context(), "enterprise-project:范围 & 7", "opaque&=/范围"); err != nil {
		t.Fatal(err)
	}
	mode.Store(1)
	if _, err = s.FetchBudgetCatalog(t.Context(), "catalog&next"); err == nil {
		t.Fatal("cacheable catalog accepted")
	}
	mode.Store(2)
	if _, err = s.FetchBudgetReading(t.Context(), "enterprise-project:范围 & 7", "opaque&=/范围"); err == nil {
		t.Fatal("oversized reading accepted")
	}
	before := calls.Load()
	if _, err = s.FetchSourceFacts(t.Context(), sourcefacts.BudgetSummaryProfile, "", 50); err == nil {
		t.Fatal("budget consumed as event feed")
	}
	if _, err = s.FetchBudgetCatalog(t.Context(), strings.Repeat("a", 4097)); err == nil {
		t.Fatal("oversized cursor accepted")
	}
	if calls.Load() != before {
		t.Fatal("invalid request reached source")
	}
	old, err := NewHTTPSource(HTTPSourceConfig{Endpoint: server.URL + "/api/v1/integrations/xinsight/source/v2/events", ServiceKey: "synthetic-b", AllowPrivateTunnelHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = old.FetchBudgetCatalog(t.Context(), ""); err == nil {
		t.Fatal("old B endpoint silently enabled D")
	}
}
