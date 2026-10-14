package api_test

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/db"
)

var assetReferencePattern = regexp.MustCompile(`/assets/[A-Za-z0-9][A-Za-z0-9._-]*`)

type apiTestCredential struct {
	password  string
	principal access.Principal
}

type apiTestProvider map[string]apiTestCredential

func (provider apiTestProvider) Authenticate(username string, password []byte) (access.Principal, bool) {
	credential, exists := provider[access.NormalizeUsername(username)]
	matches := subtle.ConstantTimeCompare([]byte(credential.password), password) == 1
	return credential.principal, exists && matches
}

type cookieInjectingHandler struct {
	next   http.Handler
	cookie *http.Cookie
}

func (handler cookieInjectingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cloned := r.Clone(r.Context())
	cloned.Header = r.Header.Clone()
	cloned.AddCookie(handler.cookie)
	handler.next.ServeHTTP(w, cloned)
}

func newAuthenticatedHandler(t *testing.T, store api.Store) http.Handler {
	t.Helper()
	return newAuthenticatedHandlerWithMigrations(t, store, nil)
}

func newAuthenticatedHandlerWithMigrations(
	t *testing.T,
	store api.Store,
	expectedMigrations []db.MigrationChecksum,
) http.Handler {
	t.Helper()
	service := newAPITestAccessService(t, access.ServiceOptions{})
	token, _, err := service.Login("investigator.one", []byte("test password"), "")
	if err != nil {
		t.Fatal(err)
	}
	return cookieInjectingHandler{
		next: api.NewAuthenticatedHandler(store, expectedMigrations, service),
		cookie: &http.Cookie{
			Name:  access.SessionCookieName,
			Value: token,
		},
	}
}

func newAPITestAccessService(t *testing.T, options access.ServiceOptions) *access.Service {
	t.Helper()
	service, err := access.NewService(apiTestProvider{
		"investigator.one": {
			password: "test password",
			principal: access.Principal{
				SubjectID: "xs_0123456789abcdef",
				Role:      access.RoleInvestigator,
			},
		},
		"operator.one": {
			password: "operator password",
			principal: access.Principal{
				SubjectID: "xs_fedcba9876543210",
				Role:      access.RoleOperator,
			},
		},
		"admin.one": {
			password: "admin password",
			principal: access.Principal{
				SubjectID: "xs_0011223344556677",
				Role:      access.RolePlatformAdmin,
			},
		},
		"workbench.one": {
			password: "workbench password",
			principal: access.Principal{
				SubjectID: "xs_8899aabbccddeeff",
				Role:      access.RoleWorkbenchReader,
			},
		},
	}, access.DiscardAuditor{}, options)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestUnauthenticatedUIAndAPIAreDeniedWithoutABypass(t *testing.T) {
	handler := api.NewHandler(nil)
	ui := httptest.NewRecorder()
	handler.ServeHTTP(ui, httptest.NewRequest(http.MethodGet, "/executions", nil))
	if ui.Code != http.StatusSeeOther || ui.Header().Get("Location") != "/login" {
		t.Fatalf("ui status=%d location=%q", ui.Code, ui.Header().Get("Location"))
	}
	if got := ui.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("ui cache-control=%q", got)
	}
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/login", nil))
	if login.Code != http.StatusOK {
		t.Fatalf("login route status=%d", login.Code)
	}
	if csp := login.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "form-action 'self'") {
		t.Fatalf("login route CSP=%q", csp)
	}
	assetPath := assetReferencePattern.FindString(login.Body.String())
	if assetPath == "" {
		t.Fatalf("login route has no current asset reference")
	}
	asset := httptest.NewRecorder()
	handler.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, assetPath, nil))
	if asset.Code != http.StatusOK {
		t.Fatalf("current anonymous asset=%s status=%d body=%s", assetPath, asset.Code, asset.Body.String())
	}
	for _, traversal := range []string{
		"/assets/%2e%2e/executions",
		"/assets/%2e%2e/manual/zh-CN",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, traversal, nil))
		if response.Code != http.StatusSeeOther ||
			response.Header().Get("Location") != "/login" ||
			response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf(
				"encoded asset traversal=%s status=%d location=%q cache-control=%q body=%s",
				traversal,
				response.Code,
				response.Header().Get("Location"),
				response.Header().Get("Cache-Control"),
				response.Body.String(),
			)
		}
	}
	readAPI := httptest.NewRecorder()
	handler.ServeHTTP(readAPI, httptest.NewRequest(http.MethodGet, "/api/v1/executions", nil))
	if readAPI.Code != http.StatusUnauthorized || !strings.Contains(readAPI.Body.String(), `"code":"authentication_required"`) {
		t.Fatalf("api status=%d body=%s", readAPI.Code, readAPI.Body.String())
	}
}

func TestTLSLoginIssuesExactHostCookieAndRestoresSession(t *testing.T) {
	service := newAPITestAccessService(t, access.ServiceOptions{})
	server := httptest.NewTLSServer(api.NewAuthenticatedHandler(nil, nil, service))
	defer server.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client()
	client.Jar = jar
	login := postJSON(t, client, server.URL+"/api/v1/access/login", server.URL, `{"username":"investigator.one","password":"test password"}`, "")
	defer login.Body.Close()
	if login.StatusCode != http.StatusOK {
		t.Fatalf("login status=%d body=%s", login.StatusCode, readBody(t, login))
	}
	if hsts := login.Header.Get("Strict-Transport-Security"); hsts != "max-age=31536000" {
		t.Fatalf("TLS login HSTS=%q", hsts)
	}
	cookies := login.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("set cookies=%d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != access.SessionCookieName ||
		!cookie.Secure ||
		!cookie.HttpOnly ||
		cookie.SameSite != http.SameSiteStrictMode ||
		cookie.Path != "/" ||
		cookie.Domain != "" {
		t.Fatalf("cookie=%+v", cookie)
	}
	var session sessionEnvelope
	if err := json.NewDecoder(login.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	if session.Session.Role != access.RoleInvestigator || session.CSRFToken == "" {
		t.Fatalf("session=%+v", session)
	}
	state, err := client.Get(server.URL + "/api/v1/access/session")
	if err != nil {
		t.Fatal(err)
	}
	defer state.Body.Close()
	if state.StatusCode != http.StatusOK {
		t.Fatalf("state status=%d body=%s", state.StatusCode, readBody(t, state))
	}

	httpServer := httptest.NewServer(api.NewAuthenticatedHandler(nil, nil, service))
	defer httpServer.Close()
	httpClient := httpServer.Client()
	httpClient.Jar = jar
	insecureState, err := httpClient.Get(httpServer.URL + "/api/v1/access/session")
	if err != nil {
		t.Fatal(err)
	}
	defer insecureState.Body.Close()
	if insecureState.StatusCode != http.StatusUnauthorized {
		t.Fatalf("Secure cookie traveled over HTTP: status=%d", insecureState.StatusCode)
	}
	if hsts := insecureState.Header.Get("Strict-Transport-Security"); hsts != "" {
		t.Fatalf("HTTP response advertised HSTS=%q", hsts)
	}
}

func TestLoginRejectsHTTPCrossOriginDuplicateAndInvalidCredentials(t *testing.T) {
	service := newAPITestAccessService(t, access.ServiceOptions{})
	handler := api.NewAuthenticatedHandler(nil, nil, service)
	for _, testCase := range []struct {
		name   string
		target string
		origin string
		body   string
		status int
		code   string
	}{
		{"http", "http://xinsight.local/api/v1/access/login", "https://xinsight.local", `{"username":"investigator.one","password":"test password"}`, http.StatusForbidden, "same_origin_required"},
		{"cross-origin", "https://xinsight.local/api/v1/access/login", "https://attacker.invalid", `{"username":"investigator.one","password":"test password"}`, http.StatusForbidden, "same_origin_required"},
		{"duplicate", "https://xinsight.local/api/v1/access/login", "https://xinsight.local", `{"username":"investigator.one","username":"operator.one","password":"test password"}`, http.StatusBadRequest, "invalid_authentication_request"},
		{"wrong-password", "https://xinsight.local/api/v1/access/login", "https://xinsight.local", `{"username":"investigator.one","password":"wrong"}`, http.StatusUnauthorized, "authentication_failed"},
		{"unknown-user", "https://xinsight.local/api/v1/access/login", "https://xinsight.local", `{"username":"unknown.user","password":"wrong"}`, http.StatusUnauthorized, "authentication_failed"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, testCase.target, strings.NewReader(testCase.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", testCase.origin)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != testCase.status || !strings.Contains(response.Body.String(), `"code":"`+testCase.code+`"`) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "investigator.one") ||
				strings.Contains(response.Body.String(), "unknown.user") ||
				strings.Contains(response.Body.String(), "wrong") {
				t.Fatalf("authentication response leaked input: %s", response.Body.String())
			}
		})
	}
}

func TestLogoutRequiresSessionBoundCSRFAndInvalidatesServerSession(t *testing.T) {
	service := newAPITestAccessService(t, access.ServiceOptions{})
	server := httptest.NewTLSServer(api.NewAuthenticatedHandler(nil, nil, service))
	defer server.Close()
	client := tlsClientWithJar(t, server)
	login := postJSON(t, client, server.URL+"/api/v1/access/login", server.URL, `{"username":"investigator.one","password":"test password"}`, "")
	var session sessionEnvelope
	if err := json.NewDecoder(login.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	login.Body.Close()

	withoutCSRF := postJSON(t, client, server.URL+"/api/v1/access/logout", server.URL, `{}`, "")
	withoutCSRF.Body.Close()
	if withoutCSRF.StatusCode != http.StatusForbidden {
		t.Fatalf("logout without CSRF status=%d", withoutCSRF.StatusCode)
	}
	logout := postJSON(t, client, server.URL+"/api/v1/access/logout", server.URL, `{}`, session.CSRFToken)
	logout.Body.Close()
	if logout.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status=%d", logout.StatusCode)
	}
	state, err := client.Get(server.URL + "/api/v1/access/session")
	if err != nil {
		t.Fatal(err)
	}
	defer state.Body.Close()
	if state.StatusCode != http.StatusUnauthorized {
		t.Fatalf("logged-out session status=%d", state.StatusCode)
	}
}

func TestOperatorCanListAndRevokeWhileInvestigatorGets403(t *testing.T) {
	service := newAPITestAccessService(t, access.ServiceOptions{})
	server := httptest.NewTLSServer(api.NewAuthenticatedHandler(nil, nil, service))
	defer server.Close()
	investigatorClient := tlsClientWithJar(t, server)
	operatorClient := tlsClientWithJar(t, server)
	investigatorLogin := postJSON(t, investigatorClient, server.URL+"/api/v1/access/login", server.URL, `{"username":"investigator.one","password":"test password"}`, "")
	var investigator sessionEnvelope
	if err := json.NewDecoder(investigatorLogin.Body).Decode(&investigator); err != nil {
		t.Fatal(err)
	}
	investigatorLogin.Body.Close()
	operatorLogin := postJSON(t, operatorClient, server.URL+"/api/v1/access/login", server.URL, `{"username":"operator.one","password":"operator password"}`, "")
	var operator sessionEnvelope
	if err := json.NewDecoder(operatorLogin.Body).Decode(&operator); err != nil {
		t.Fatal(err)
	}
	operatorLogin.Body.Close()

	denied, err := investigatorClient.Get(server.URL + "/api/v1/access/sessions")
	if err != nil {
		t.Fatal(err)
	}
	denied.Body.Close()
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("investigator list status=%d", denied.StatusCode)
	}
	listed, err := operatorClient.Get(server.URL + "/api/v1/access/sessions")
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Items []access.SessionView `json:"items"`
	}
	if err := json.NewDecoder(listed.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	listed.Body.Close()
	if listed.StatusCode != http.StatusOK || len(list.Items) != 2 {
		t.Fatalf("operator list status=%d items=%d", listed.StatusCode, len(list.Items))
	}
	revoke := postJSON(
		t,
		operatorClient,
		server.URL+"/api/v1/access/sessions/"+investigator.Session.SessionRef+"/revoke",
		server.URL,
		`{}`,
		operator.CSRFToken,
	)
	revoke.Body.Close()
	if revoke.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke status=%d", revoke.StatusCode)
	}
	state, err := investigatorClient.Get(server.URL + "/api/v1/access/session")
	if err != nil {
		t.Fatal(err)
	}
	defer state.Body.Close()
	if state.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked investigator status=%d", state.StatusCode)
	}
}

func TestLoginRateLimitIsBoundedAndSecurityHeadersCoverFailures(t *testing.T) {
	service := newAPITestAccessService(t, access.ServiceOptions{
		PrincipalFailureLimit: 2,
		GlobalFailureLimit:    10,
	})
	handler := api.NewAuthenticatedHandler(nil, nil, service)
	for attempt := 0; attempt < 3; attempt++ {
		request := httptest.NewRequest(
			http.MethodPost,
			"https://xinsight.local/api/v1/access/login",
			strings.NewReader(`{"username":"unknown.user","password":"wrong"}`),
		)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "https://xinsight.local")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		expected := http.StatusUnauthorized
		if attempt == 2 {
			expected = http.StatusTooManyRequests
		}
		if response.Code != expected {
			t.Fatalf("attempt=%d status=%d body=%s", attempt, response.Code, response.Body.String())
		}
		for _, header := range []string{
			"Cache-Control",
			"Content-Security-Policy",
			"Referrer-Policy",
			"Permissions-Policy",
			"X-Content-Type-Options",
			"X-Frame-Options",
		} {
			if response.Header().Get(header) == "" {
				t.Fatalf("attempt=%d missing %s", attempt, header)
			}
		}
	}
}

type sessionEnvelope struct {
	Session   access.SessionView `json:"session"`
	CSRFToken string             `json:"csrf_token"`
}

func postJSON(
	t *testing.T,
	client *http.Client,
	target string,
	origin string,
	body string,
	csrf string,
) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, target, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", origin)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func tlsClientWithJar(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := *server.Client()
	client.Jar = jar
	return &client
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
