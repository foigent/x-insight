package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/workspace"
)

type credentialControllerStub struct {
	*apiSetupStore
	calls []connectorcontrol.CredentialLocalRequest
	fail  error
}

func (s *credentialControllerStub) ExecuteCredential(_ context.Context, r connectorcontrol.CredentialLocalRequest) (connectorcontrol.CredentialLocalResponse, error) {
	s.calls = append(s.calls, r)
	if s.fail != nil {
		return connectorcontrol.CredentialLocalResponse{}, s.fail
	}
	return connectorcontrol.CredentialLocalResponse{}, connectorcontrol.ErrConflict
}

func TestCredentialHTTPExactAuthorizationAndDispatch(t *testing.T) {
	service := newAPITestAccessService(t, access.ServiceOptions{})
	token, authentication, err := service.Login("admin.one", []byte("admin password"), "")
	if err != nil {
		t.Fatal(err)
	}
	issuer := &credentialControllerStub{apiSetupStore: &apiSetupStore{connectorStoreStub: &connectorStoreStub{}}}
	oldBearer := "xib_" + strings.Repeat("A", 43)
	agent := &agentAccessStoreStub{token: oldBearer, authentication: workspace.ServiceAuthentication{Identity: workspace.ServiceIdentity{ID: "xsi_" + strings.Repeat("A", 22), Status: "active", Permissions: []string{connectorcontrol.PermissionControlPoll, connectorcontrol.PermissionControlAck}}, TokenDigest: "sha256:" + strings.Repeat("a", 64)}}
	handler := api.NewHandlerWithDependencies(api.HandlerDependencies{Store: &fakeStore{}, WorkspaceStore: issuer.apiSetupStore, AgentAccessStore: agent, AccessService: service, ConnectorConfig: connectorTestConfig(), ConnectorIssuer: issuer, Now: func() time.Time { return time.Now().UTC() }})
	base := "https://xinsight.local"
	issuePath := "/api/v1/connector-setups/xci_AQEBAQEBAQEBAQEBAQEBAQ/credential-recovery"
	request := func(method, path, body string) *http.Request {
		r := httptest.NewRequest(method, base+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		return r
	}
	call := func(r *http.Request) int { w := httptest.NewRecorder(); handler.ServeHTTP(w, r); return w.Code }
	issue := request("POST", issuePath, `{"request_id":"12345678-1234-4234-8234-123456789abc"}`)
	issue.AddCookie(&http.Cookie{Name: access.SessionCookieName, Value: token})
	issue.Header.Set("Origin", base)
	issue.Header.Set("X-CSRF-Token", service.CSRFToken(authentication))
	if code := call(issue); code != http.StatusConflict || len(issuer.calls) != 1 || issuer.calls[0].ActorSubject != authentication.Session.SubjectID {
		t.Fatal("human issue", code, issuer.calls)
	}
	before := len(issuer.calls)
	bad := request("POST", issuePath, `{"request_id":"12345678-1234-4234-8234-123456789abc"}`)
	bad.AddCookie(&http.Cookie{Name: access.SessionCookieName, Value: token})
	if code := call(bad); code != http.StatusForbidden || len(issuer.calls) != before {
		t.Fatal("CSRF bypass", code)
	}
	resolve := request("POST", "/api/v1/connector-control/credential-recoveries/resolve", `{"ticket":"xirt_`+strings.Repeat("T", 43)+`"}`)
	if code := call(resolve); code != http.StatusConflict || len(issuer.calls) != before+1 || issuer.calls[before].Action != "resolve" {
		t.Fatal("resolve", code)
	}
	httpResolve := httptest.NewRequest("POST", "http://xinsight.local/api/v1/connector-control/credential-recoveries/resolve", strings.NewReader(`{"ticket":"xirt_`+strings.Repeat("T", 43)+`"}`))
	httpResolve.Header.Set("Content-Type", "application/json")
	if code := call(httpResolve); code != http.StatusForbidden {
		t.Fatal("HTTP resolve", code)
	}
	for _, name := range []string{"rotate", "recover"} {
		raw, readErr := os.ReadFile(filepath.Join("..", "connectorcontrol", "testdata", "credential-v1", name+".json"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		path := "/api/v1/connector-control/installations/xci_AQEBAQEBAQEBAQEBAQEBAQ/credentials/" + name
		r := request("POST", path, string(raw))
		if name == "rotate" {
			r.Header.Set("Authorization", "Bearer "+oldBearer)
		}
		if code := call(r); code != http.StatusConflict || issuer.calls[len(issuer.calls)-1].Action != name {
			t.Fatal(name, code)
		}
		if name == "recover" {
			r.AddCookie(&http.Cookie{Name: access.SessionCookieName, Value: token})
			if code := call(r); code != http.StatusForbidden {
				t.Fatal("ambient recover", code)
			}
		}
	}
	operation := "xcr_AwMDAwMDAwMDAwMDAwMDAw"
	query := request("GET", "/api/v1/connector-control/installations/xci_AQEBAQEBAQEBAQEBAQEBAQ/credentials/operations/"+operation, "")
	query.Header.Set("Authorization", "Bearer "+oldBearer)
	if code := call(query); code != http.StatusConflict || issuer.calls[len(issuer.calls)-1].Action != "query" {
		t.Fatal("query", code)
	}
	confirmRaw, err := os.ReadFile(filepath.Join("..", "connectorcontrol", "testdata", "credential-v1", "confirm.json"))
	if err != nil {
		t.Fatal(err)
	}
	confirm := request("POST", "/api/v1/connector-control/installations/xci_AQEBAQEBAQEBAQEBAQEBAQ/credentials/operations/"+operation+"/confirm", string(confirmRaw))
	confirm.Header.Set("Authorization", "Bearer "+oldBearer)
	if code := call(confirm); code != http.StatusConflict || issuer.calls[len(issuer.calls)-1].Action != "confirm" {
		t.Fatal("confirm", code)
	}
	denied := request("POST", "/api/v1/connector-control/installations/xci_AQEBAQEBAQEBAQEBAQEBAQ/credentials/operations/"+operation+"/other", string(confirmRaw))
	denied.Header.Set("Authorization", "Bearer "+oldBearer)
	if code := call(denied); code != http.StatusForbidden {
		t.Fatal("unexpected route", code)
	}
	issuer.fail = connectorcontrol.ErrExpired
	expired := request("POST", issuePath, `{"request_id":"12345678-1234-4234-8234-123456789abc"}`)
	expired.AddCookie(&http.Cookie{Name: access.SessionCookieName, Value: token})
	expired.Header.Set("Origin", base)
	expired.Header.Set("X-CSRF-Token", service.CSRFToken(authentication))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, expired)
	if w.Code != http.StatusGone || !strings.Contains(w.Body.String(), `"code":"connector_credential_recovery_expired"`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("expired recovery must request new human confirmation", w.Code, w.Body.String())
	}
}
