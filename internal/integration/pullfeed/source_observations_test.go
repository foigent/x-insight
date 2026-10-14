package pullfeed

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

func TestObservationTransportUsesDistinctPathsAndNoStore(t *testing.T) {
	var paths []string
	var mu sync.Mutex
	var noStore atomic.Bool
	noStore.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer synthetic-b-key" {
			t.Error("missing scoped service key")
		}
		if r.URL.Path == "/api/v1/integrations/xinsight/source/v2/runtime/current" && r.Header.Get("Cache-Control") != "no-store" {
			t.Error("snapshot request can be cached")
		}
		if noStore.Load() {
			w.Header().Set("Cache-Control", "private, no-store")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	s, err := NewHTTPSource(HTTPSourceConfig{Endpoint: server.URL + "/api/v1/integrations/xinsight/source/v2/events", ServiceKey: "synthetic-b-key", AllowPrivateTunnelHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.FetchSourceCapabilities(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FetchSourceFacts(t.Context(), sourcefacts.RuntimeProfile, "cursor", 50); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FetchCurrentRuntime(t.Context(), "", 50); err != nil {
		t.Fatal(err)
	}
	want := []string{"/api/v1/integrations/xinsight/source/v2/capabilities", "/api/v1/integrations/xinsight/source/v2/events", "/api/v1/integrations/xinsight/source/v2/runtime/current"}
	mu.Lock()
	got := append([]string{}, paths...)
	mu.Unlock()
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i, p := range got {
		if p != want[i] {
			t.Fatal(got)
		}
	}
	if _, err := s.FetchSourceFacts(t.Context(), sourcefacts.ContextProfile, "cursor", 50); err == nil {
		t.Fatal("B endpoint used for A")
	}
	if _, err := s.FetchSourceFacts(t.Context(), "unirouter.budget-definition.v1", "cursor", 50); err == nil {
		t.Fatal("D consumption enabled")
	}
	noStore.Store(false)
	if _, err := s.FetchCurrentRuntime(t.Context(), "", 50); err == nil {
		t.Fatal("cacheable current snapshot accepted")
	}
}
