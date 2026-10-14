package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/connectorcontrol"
)

type apiSetupController struct {
	calls   []connectorcontrol.SetupLocalRequest
	created connectorcontrol.SetupCreated
}

func (c *apiSetupController) ExecuteSetup(_ context.Context, r connectorcontrol.SetupLocalRequest) (connectorcontrol.SetupLocalResponse, error) {
	c.calls = append(c.calls, r)
	out := connectorcontrol.SetupLocalResponse{SchemaVersion: connectorcontrol.LocalSetupResponseSchema, Action: r.Action}
	switch r.Action {
	case "preview":
		out.Preview = &connectorcontrol.SetupPreview{SchemaVersion: connectorcontrol.SetupPreviewSchema, DeploymentID: c.created.Consent.DeploymentID, UROrigin: c.created.Consent.UROrigin, CanChangeCustody: true}
	case "create":
		out.Created = &c.created
	case "status":
		out.Status = &connectorcontrol.SetupStatus{SchemaVersion: connectorcontrol.SetupStatusSchema, InstallationID: r.InstallationID, State: "waiting_consent"}
	}
	return out, nil
}

type apiSetupStore struct {
	*connectorStoreStub
	resolved     connectorcontrol.SetupCreated
	resolveCalls int
}

func (s *apiSetupStore) ResolveSetupInvitation(_ context.Context, token string, _ time.Time) (connectorcontrol.SetupCreated, error) {
	s.resolveCalls++
	if token != "xiiv1_"+strings.Repeat("A", 43) {
		return connectorcontrol.SetupCreated{}, connectorcontrol.ErrNotFound
	}
	return s.resolved, nil
}
func setupHTTPFixture(t *testing.T, username, password string) (http.Handler, *http.Cookie, string, *apiSetupController, *apiSetupStore) {
	t.Helper()
	service := newAPITestAccessService(t, access.ServiceOptions{})
	token, auth, err := service.Login(username, []byte(password), "")
	if err != nil {
		t.Fatal(err)
	}
	cfg := connectorTestConfig()
	expires := time.Now().UTC().Add(time.Minute)
	view := connectorcontrol.SetupConsentView{SchemaVersion: connectorcontrol.SetupConsentViewSchema, InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA", DeploymentID: cfg.DeploymentID, XIOrigin: cfg.XInsightOrigin, UROrigin: cfg.UniRouterInstallOrigin, ConsentRevision: 1, ExpiresAt: expires, Custody: connectorcontrol.SetupCustody{Mode: "full_content", RetentionSeconds: 90 * 86400, ProjectQuotaBytes: 5 << 30, MaxItemBytes: 32 << 20}}
	view.ConsentSHA256, _, err = connectorcontrol.CanonicalSHA256(view)
	if err != nil {
		t.Fatal(err)
	}
	installURL, err := connectorcontrol.BuildInstallURL(view.UROrigin, "xiiv1_"+strings.Repeat("A", 43), view.XIOrigin)
	if err != nil {
		t.Fatal(err)
	}
	created := connectorcontrol.SetupCreated{Consent: view, Invitation: connectorcontrol.InvitationPackage{SchemaVersion: connectorcontrol.InvitationSchema, InstallationID: view.InstallationID, InvitationID: "xii_AAAAAAAAAAAAAAAAAAAAAA", DeploymentID: view.DeploymentID, Audience: connectorcontrol.ConnectorAudience, RequestedEventScope: []string{connectorcontrol.CompletedEventType}, XInsightOrigin: view.XIOrigin, ExpiresAt: view.ExpiresAt, InstallURL: installURL}}
	controller := &apiSetupController{created: created}
	store := &apiSetupStore{connectorStoreStub: &connectorStoreStub{}, resolved: created}
	handler := api.NewHandlerWithDependencies(api.HandlerDependencies{Store: &fakeStore{}, WorkspaceStore: store, AccessService: service, ConnectorConfig: cfg, ConnectorIssuer: store, SetupController: controller})
	return handler, &http.Cookie{Name: access.SessionCookieName, Value: token}, service.CSRFToken(auth), controller, store
}
func TestConnectorSetupHTTPSHumanCSRFClosedInputAndStaticPeers(t *testing.T) {
	handler, cookie, csrf, controller, _ := setupHTTPFixture(t, "admin.one", "admin password")
	body := `{"schema_version":"xinsight.connector-setup-consent.v1","request_id":"12345678-1234-4234-8234-123456789abc","expected_custody_revision":0,"custody":{"mode":"full_content","retention_seconds":7776000,"project_quota_bytes":5368709120,"max_item_bytes":33554432}}`
	call := func(target, origin, csrfValue, payload string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("POST", target, strings.NewReader(payload))
		req.AddCookie(cookie)
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrfValue)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	for _, negative := range []struct{ target, origin, csrf, payload string }{{"http://xinsight.local/api/v1/connector-setups", "http://xinsight.local", csrf, body}, {"https://xinsight.local/api/v1/connector-setups", "https://foreign.example", csrf, body}, {"https://xinsight.local/api/v1/connector-setups", "https://xinsight.local", "", body}, {"https://xinsight.local/api/v1/connector-setups", "https://xinsight.local", csrf, strings.TrimSuffix(body, "}") + `,"actor_subject":"xs_2222222222222222"}`}, {"https://xinsight.local/api/v1/connector-setups", "https://xinsight.local", csrf, strings.Replace(body, `"expected_custody_revision":0`, `"expected_custody_revision":0,"expected_custody_revision":0`, 1)}} {
		if w := call(negative.target, negative.origin, negative.csrf, negative.payload); w.Code < 400 {
			t.Fatal("unsafe setup accepted", w.Code)
		}
	}
	if len(controller.calls) != 0 {
		t.Fatal("rejected input reached root")
	}
	w := call("https://xinsight.local/api/v1/connector-setups", "https://xinsight.local", csrf, body)
	if w.Code != 201 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(controller.calls) != 1 || controller.calls[0].ActorSubject != "xs_0011223344556677" {
		t.Fatal("caller chose root actor")
	}
	controller.created.Invitation.InstallURL = "https://foreign.example/steal"
	if w = call("https://xinsight.local/api/v1/connector-setups", "https://xinsight.local", csrf, body); w.Code < 400 {
		t.Fatal("unbound invitation URL returned")
	}
}
func TestConnectorSetupResolveHasNoAmbientAuthorityAndReturnsOriginalPackage(t *testing.T) {
	handler, cookie, _, _, store := setupHTTPFixture(t, "admin.one", "admin password")
	raw, _ := json.Marshal(map[string]string{"invitation_token": "xiiv1_" + strings.Repeat("A", 43)})
	call := func(target string, ambient bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", target, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		if ambient {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if call("http://xinsight.local/api/v1/connector-setups/resolve", false).Code < 400 || call("https://xinsight.local/api/v1/connector-setups/resolve", true).Code < 400 || store.resolveCalls != 0 {
		t.Fatal("ambient/HTTP resolve reached store")
	}
	w := call("https://xinsight.local/api/v1/connector-setups/resolve", false)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	var got connectorcontrol.SetupCreated
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.Invitation.InstallURL != store.resolved.Invitation.InstallURL || got.Consent != store.resolved.Consent {
		t.Fatal("fragment cannot recover exact original package", err)
	}
}

// Exercise the actual authenticated middleware and embedded SPA over HTTPS.
// A recorder-only or client-side route test misses hard navigation/refresh 404s.
func TestConnectorSetupProductRoutesOverAuthenticatedTLS(t *testing.T) {
	handler, _, _, _, _ := setupHTTPFixture(t, "admin.one", "admin password")
	server := httptest.NewTLSServer(handler)
	defer server.Close()
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client.Jar = jar
	route := "/connectors/unirouter/setup"
	for _, target := range []string{route, route + "?installation=xci_AAAAAAAAAAAAAAAAAAAAAA"} {
		response, err := client.Get(server.URL + target)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusSeeOther || !strings.HasPrefix(response.Header.Get("Location"), "/login?return_to=") {
			t.Fatalf("anonymous setup status=%d location=%q", response.StatusCode, response.Header.Get("Location"))
		}
	}
	login := postJSON(t, client, server.URL+"/api/v1/access/login", server.URL, `{"username":"admin.one","password":"admin password"}`, "")
	if login.StatusCode != http.StatusOK {
		t.Fatalf("fixture login status=%d", login.StatusCode)
	}
	login.Body.Close()
	for _, target := range []string{route, route + "/", route + "?installation=xci_AAAAAAAAAAAAAAAAAAAAAA", "/connectors", "/access/review-policy", "/work/activity"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			// Two requests cover a direct load followed by a document refresh.
			for attempt := 0; attempt < 2; attempt++ {
				request, err := http.NewRequest(method, server.URL+target, nil)
				if err != nil {
					t.Fatal(err)
				}
				response, err := client.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				body := readBody(t, response)
				response.Body.Close()
				if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" ||
					!strings.Contains(response.Header.Get("Content-Type"), "text/html") ||
					!strings.Contains(response.Header.Get("Content-Security-Policy"), "default-src 'self'") {
					t.Fatalf("%s %s attempt=%d status=%d", method, target, attempt, response.StatusCode)
				}
				if method == http.MethodHead && body != "" || method == http.MethodGet && !strings.Contains(body, `<html lang="en-US">`) {
					t.Fatalf("%s %s wrong document body", method, target)
				}
			}
		}
	}
	for _, target := range []string{"/connectors/unirouter", "/connectors/unirouter/setup/unknown", "/connectors/unknown", "/api/v1/unknown"} {
		response, err := client.Get(server.URL + target)
		if err != nil {
			t.Fatal(err)
		}
		body := readBody(t, response)
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound || strings.Contains(body, `<html lang="en-US">`) {
			t.Fatalf("unknown route %s status=%d", target, response.StatusCode)
		}
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+route, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed || response.Header.Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST setup status=%d allow=%q", response.StatusCode, response.Header.Get("Allow"))
	}
}
