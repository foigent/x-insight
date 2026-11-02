package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/connectoractivation"
	"github.com/foigent/x-insight/internal/connectorsync"
	"github.com/foigent/x-insight/internal/db"
)

type syncChoiceHost struct{}

func (syncChoiceHost) AuthorizeSetupActor(actor string, check bool) error {
	if actor != "xs_0011223344556677" || check {
		return errors.New("synthetic owner required")
	}
	return nil
}

type nativeSyncChoiceIssuer struct {
	*connectorStoreStub
	manager *connectoractivation.SyncManager
}

func (s *nativeSyncChoiceIssuer) ExecuteSync(ctx context.Context, r connectorsync.Request) (connectorsync.Response, error) {
	return s.manager.ExecuteSync(ctx, r)
}

// This exercises the shipped human HTTP route, native controller, real durable
// preference and worker gate together. It never writes Auto using fixture SQL.
func TestConnectorSyncNativeOwnerChoiceOpensManualWorkerPostgres(t *testing.T) {
	server, admin, _, _, _ := startWorkbenchProductionFixture(t, true)
	server.Close()
	const id = "xci_4646464646464646464646"
	const requestID = "46464646-4646-4646-8646-000000000001"
	if _, err := admin.Exec(`INSERT INTO connector_installations(id,deployment_id,connector_type,requested_event_scope,status,desired_state,observed_state,created_at,updated_at) VALUES($1,'xid_AAAAAAAAAAAAAAAAAAAAAA','unirouter',ARRAY['model.request.completed'],'pending','active','awaiting_consent',statement_timestamp(),statement_timestamp())`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(`INSERT INTO connector_installation_source_bindings(installation_id,source_binding_sha256) VALUES($1,'sha256:'||repeat('a',64))`, id); err != nil {
		t.Fatal(err)
	}
	store := db.NewWorkspaceStore(admin)
	service := newAPITestAccessService(t, access.ServiceOptions{})
	issuer := &nativeSyncChoiceIssuer{connectorStoreStub: &connectorStoreStub{}, manager: &connectoractivation.SyncManager{Store: store, Host: syncChoiceHost{}}}
	handler := api.NewHandlerWithDependencies(api.HandlerDependencies{Store: &fakeStore{}, WorkspaceStore: store, AccessService: service, ConnectorConfig: connectorTestConfig(), ConnectorIssuer: issuer})
	owner, auth, err := service.Login("admin.one", []byte("admin password"), "")
	if err != nil {
		t.Fatal(err)
	}
	reader, _, err := service.Login("workbench.one", []byte("workbench password"), "")
	if err != nil {
		t.Fatal(err)
	}
	target := "https://xinsight.local/api/v1/connector-control/installations/" + id + "/sync"
	call := func(method, token, csrf, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, target, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: access.SessionCookieName, Value: token})
		r.Header.Set("Origin", "https://xinsight.local")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	status := func() connectorsync.Status {
		w := call(http.MethodGet, owner, "", "")
		var out connectorsync.Status
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatal("native status", w.Code, w.Body.String())
		}
		return out
	}
	before := status()
	if before.Auto || before.Cadence != nil || before.Revision != 1 || before.RequestID != "" {
		t.Fatal("manual default changed", before)
	}
	if _, _, err := connectorsync.Work(t.Context(), store, id, "activation"); !errors.Is(err, connectorsync.ErrPaused) {
		t.Fatal("manual worker was allowed", err)
	} else {
		t.Logf("exact manual worker rejection: %s", err)
	}
	body := `{"expected_revision":1,"request_id":"` + requestID + `","auto_sync":true,"cadence_seconds":5}`
	for _, token := range []string{reader, owner} {
		if w := call(http.MethodPut, token, "", body); w.Code != 403 {
			t.Fatal("reader or missing CSRF changed preference", w.Code)
		}
	}
	if got := status(); got.Auto || got.Revision != 1 {
		t.Fatal("denied mutation changed manual preference")
	}
	if w := call(http.MethodPut, owner, service.CSRFToken(auth), body); w.Code != 200 {
		t.Fatal("native owner choice", w.Code, w.Body.String())
	}
	after := status()
	if !after.Auto || after.Revision != 2 || after.RequestID != requestID || after.Cadence == nil || *after.Cadence != 5 {
		t.Fatal("owner choice not durably retained", after)
	}
	_, finish, err := connectorsync.Work(t.Context(), store, id, "activation")
	if err != nil {
		t.Fatal("explicit owner choice did not admit worker", err)
	}
	finish(nil)
	t.Log("native owner HTTP + CSRF -> root SyncManager -> durable revision2/Auto/cadence5/request identity -> worker admitted; no SQL preference mutation")
}
