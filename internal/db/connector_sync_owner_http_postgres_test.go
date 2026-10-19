package db_test

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
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/connectorsync"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

type syncOwnerProvider struct{}

func (syncOwnerProvider) Authenticate(user string, password []byte) (access.Principal, bool) {
	if string(password) != "synthetic sync test" {
		return access.Principal{}, false
	}
	if user == "sync.owner" {
		return access.Principal{SubjectID: "xs_4646464646464646", Role: access.RolePlatformAdmin}, true
	}
	if user == "sync.reader" {
		return access.Principal{SubjectID: "xs_4747474747474747", Role: access.RoleWorkbenchReader}, true
	}
	return access.Principal{}, false
}

type syncOwnerHost struct{}

func (syncOwnerHost) AuthorizeSetupActor(subject string, recovery bool) error {
	if subject != "xs_4646464646464646" || recovery {
		return connectorcontrol.ErrSetupUnauthorized
	}
	return nil
}

type syncOwnerIssuer struct {
	*db.WorkspaceStore
	manager *connectoractivation.SyncManager
}

func (s syncOwnerIssuer) ExecuteSync(ctx context.Context, r connectorsync.Request) (connectorsync.Response, error) {
	return s.manager.ExecuteSync(ctx, r)
}

func TestDefaultManualRequiresRealOwnerHTTPBeforeActualWorker(t *testing.T) {
	f := newSourceFixture(t)
	gate := f.workspace.SyncStore()
	old, err := gate.Resolve(f.ctx, "ur-demo-01", "17", "101", false)
	if err != nil {
		t.Fatal(err)
	}
	// Retire the earlier fixture binding; the newly inserted connection keeps
	// the real schema46 trigger default. No SQL sets its sync preference.
	f.exec(`UPDATE connector_installations SET status='disconnected',desired_state='disconnected',observed_state='disconnected' WHERE id=$1`, old)
	f.bindSyncScopeWithAuto("ur-demo-01", "17", "101", false)
	id, err := gate.Resolve(f.ctx, "ur-demo-01", "17", "101", false)
	if err != nil || id == old {
		t.Fatal("new exact connection binding", id, err)
	}
	status, err := gate.Status(f.ctx, id)
	if err != nil || status.Auto || status.Revision != 1 || status.Cadence != nil || status.Run != nil {
		t.Fatal("new connection did not default to quiet manual", status, err)
	}
	calls := func() int { f.mu.Lock(); defer f.mu.Unlock(); return f.httpCalls }
	before := calls()
	for i := 0; i < 2; i++ {
		if _, err := f.consumer.PullOnce(f.ctx, f.streams[sourcefacts.ContextProfile]); !errors.Is(err, connectorsync.ErrPaused) {
			t.Fatal("default manual admitted actual worker", err)
		}
	}
	if calls() != before {
		t.Fatal("default manual emitted source HTTP")
	}
	if err := f.workspace.SetConnectorDeploymentID("xid_0000000000000000000000"); err != nil {
		t.Fatal(err)
	}
	service, err := access.NewService(syncOwnerProvider{}, access.DiscardAuditor{}, access.ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	owner, auth, err := service.Login("sync.owner", []byte("synthetic sync test"), "")
	if err != nil {
		t.Fatal(err)
	}
	reader, readerAuth, err := service.Login("sync.reader", []byte("synthetic sync test"), "")
	if err != nil {
		t.Fatal(err)
	}
	wakes := 0
	issuer := syncOwnerIssuer{WorkspaceStore: f.workspace, manager: &connectoractivation.SyncManager{Store: f.workspace, Host: syncOwnerHost{}, Wake: func() { wakes++ }}}
	config := connectorcontrol.InvitationConfig{DeploymentID: "xid_0000000000000000000000", XInsightOrigin: "https://xi.sync.test", UniRouterInstallOrigin: "https://ur.sync.test"}
	handler := api.NewHandlerWithDependencies(api.HandlerDependencies{Store: f.evidence, WorkspaceStore: f.workspace, AccessService: service, ConnectorConfig: config, ConnectorIssuer: issuer})
	body := `{"expected_revision":1,"request_id":"46464646-4646-4646-8646-000000000001","auto_sync":true,"cadence_seconds":5}`
	call := func(token, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPut, config.XInsightOrigin+"/api/v1/connector-control/installations/"+id+"/sync", strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: access.SessionCookieName, Value: token})
		r.Header.Set("Origin", config.XInsightOrigin)
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, denied := range [][2]string{{reader, service.CSRFToken(readerAuth)}, {owner, ""}} {
		if w := call(denied[0], denied[1]); w.Code != http.StatusForbidden {
			t.Fatal("unauthorized owner choice", w.Code, w.Body.String())
		}
	}
	status, err = gate.Status(f.ctx, id)
	if err != nil || status.Auto || status.Revision != 1 || wakes != 0 || calls() != before {
		t.Fatal("denied request changed intent or emitted HTTP", status, err)
	}
	w := call(owner, service.CSRFToken(auth))
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &status) != nil || !status.Auto || status.Cadence == nil || *status.Cadence != 5 || status.Revision != 2 || wakes != 1 {
		t.Fatal("real owner choice was not retained", w.Code, w.Body.String())
	}
	var actor string
	if err := f.admin.QueryRowContext(f.ctx, `SELECT updated_by FROM connector_sync_controls WHERE installation_id=$1`, id).Scan(&actor); err != nil || actor != auth.Session.SubjectID {
		t.Fatal("owner identity did not reach root store", actor, err)
	}
	f.setPage(sourcefacts.ContextProfile, "owner-auto-next", f.documents[sourcefacts.ContextProfile])
	if _, err := f.consumer.PullOnce(f.ctx, f.streams[sourcefacts.ContextProfile]); err != nil {
		t.Fatal("authorized actual worker failed", err)
	}
	after, err := f.consumerStore.LoadSourceFactsStream(f.ctx, f.streams[sourcefacts.ContextProfile])
	if err != nil || after.Cursor != "owner-auto-next" || calls() != before+2 {
		t.Fatal("actual capability/page HTTP budget or commit mismatch", calls()-before, after, err)
	}
}
