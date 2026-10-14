package pullfeed_test

import (
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/integration/pullfeed"
)

func TestSourceDescriptorFixedHTTPSNoQueryAndResponseBounds(t *testing.T) {
	const id = "xci_AAAAAAAAAAAAAAAAAAAAAA"
	for _, tt := range []struct {
		name, cache, media, body string
		status                   int
		accepted                 bool
	}{
		{"valid", "no-store", "application/json", "{}", 200, true},
		{"missing-no-store", "", "application/json", "{}", 200, false},
		{"wrong-media", "no-store", "text/html", "{}", 200, false},
		{"oversize", "no-store", "application/json", strings.Repeat("x", (16<<10)+1), 200, false},
		{"redirect", "no-store", "application/json", "{}", 302, false},
		{"not-ready", "no-store", "application/json", `{"error":"connector_source_descriptor.not_ready"}`, 409, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/api/v1/integrations/xinsight/connector-installations/"+id+"/source-binding" || r.URL.RawQuery != "" || r.ContentLength != 0 || r.Header.Get("Authorization") != "Bearer fixture-original-feed" {
					t.Error("descriptor transport identity/query/body changed")
				}
				w.Header().Set("Content-Type", tt.media)
				w.Header().Set("Cache-Control", tt.cache)
				w.Header().Set("Location", "https://redirect.invalid")
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			}))
			defer server.Close()
			roots := x509.NewCertPool()
			roots.AddCert(server.Certificate())
			reader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: server.URL + "/api/v1/integrations/xinsight/completed-executions", ServiceKey: "fixture-original-feed", TLSRootCAs: roots})
			if err != nil {
				t.Fatal(err)
			}
			defer reader.CloseContentConnections()
			body, err := reader.FetchConnectorSourceDescriptor(t.Context(), id)
			if (err == nil) != tt.accepted || calls != 1 || tt.accepted && string(body) != tt.body {
				t.Fatal("bounded fixed GET", err, calls)
			}
			if _, err := reader.FetchConnectorSourceDescriptor(t.Context(), id+"/other"); err == nil || calls != 1 {
				t.Fatal("unsafe installation path reached network")
			}
		})
	}
}
