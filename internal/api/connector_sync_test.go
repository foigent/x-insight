package api_test

import (
	"context"
	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/connectorsync"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type syncAPIStub struct {
	*connectorStoreStub
	calls []connectorsync.Request
}

func (s *syncAPIStub) ExecuteSync(_ context.Context, r connectorsync.Request) (connectorsync.Response, error) {
	s.calls = append(s.calls, r)
	return connectorsync.Response{SchemaVersion: connectorsync.LocalSchema, Status: &connectorsync.Status{InstallationID: r.InstallationID, Revision: 2, Auto: r.Auto, Peer: "unknown", Effective: "manual_running", Slots: []connectorsync.Slot{}}}, nil
}

func TestConnectorSyncHumanPermissionCSRFClosedBodyAndTarget(t *testing.T) {
	service := newAPITestAccessService(t, access.ServiceOptions{})
	stub := &syncAPIStub{connectorStoreStub: &connectorStoreStub{}}
	handler := api.NewHandlerWithDependencies(api.HandlerDependencies{Store: &fakeStore{}, WorkspaceStore: stub, AccessService: service, ConnectorConfig: connectorTestConfig(), ConnectorIssuer: stub})
	token, auth, e := service.Login("admin.one", []byte("admin password"), "")
	if e != nil {
		t.Fatal(e)
	}
	csrf := service.CSRFToken(auth)
	target := "https://xinsight.local/api/v1/connector-control/installations/xci_AAAAAAAAAAAAAAAAAAAAAA/sync-once"
	body := `{"expected_revision":1,"request_id":"46464646-4646-4646-8646-000000000001","auto_sync":false,"cadence_seconds":60}`
	call := func(method, url, origin, anti, login, payload string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, url, strings.NewReader(payload))
		if login != "" {
			req.AddCookie(&http.Cookie{Name: access.SessionCookieName, Value: login})
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("X-CSRF-Token", anti)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	for _, v := range []struct{ origin, anti, login, payload string }{{"https://foreign.example", csrf, token, body}, {"https://xinsight.local", "", token, body}, {"https://xinsight.local", csrf, "", body}, {"https://xinsight.local", csrf, token, strings.TrimSuffix(body, "}") + `,"actor":"root"}`}, {"https://xinsight.local", csrf, token, strings.Replace(body, ":60", ":4", 1)}} {
		if w := call("POST", target, v.origin, v.anti, v.login, v.payload); w.Code < 400 {
			t.Fatal("unsafe request accepted", w.Code, w.Body.String())
		}
	}
	if len(stub.calls) != 0 {
		t.Fatal("rejected requests reached root controller")
	}
	reader, _, e := service.Login("workbench.one", []byte("workbench password"), "")
	if e != nil {
		t.Fatal(e)
	}
	if w := call("GET", strings.TrimSuffix(target, "-once"), "https://xinsight.local", "", reader, ""); w.Code != 403 {
		t.Fatal("read-only account gained sync control", w.Code)
	}
	if w := call("POST", target, "https://xinsight.local", csrf, token, body); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(stub.calls) != 1 || stub.calls[0].Actor != "xs_0011223344556677" || stub.calls[0].Action != "once" || stub.calls[0].InstallationID != "xci_AAAAAAAAAAAAAAAAAAAAAA" {
		t.Fatal("root request lost authenticated identity/target", stub.calls)
	}
	if w := call("GET", strings.TrimSuffix(target, "-once"), "https://xinsight.local", "", token, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if stub.calls[1].Action != "status" {
		t.Fatal("status initiated collection")
	}
}
