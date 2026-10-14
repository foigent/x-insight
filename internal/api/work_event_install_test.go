package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/workinstallcontrol"
	"github.com/foigent/x-insight/internal/workspace"
)

type workInstallControllerStub struct {
	calls []workinstallcontrol.Request
}

func (s *workInstallControllerStub) Execute(_ context.Context, request workinstallcontrol.Request) (workinstallcontrol.Response, error) {
	s.calls = append(s.calls, request)
	response := workinstallcontrol.Response{SchemaVersion: workinstallcontrol.ResponseSchema, Action: request.Action}
	if request.Action == workinstallcontrol.ActionList {
		response.State = "read"
		response.Items = []db.WorkInstallInvitation{}
		return response, nil
	}
	if request.Action == workinstallcontrol.ActionStatus {
		response.State = "reported"
		response.InvitationID = "xiei_" + strings.Repeat("a", 32)
		return response, nil
	}
	response.State = "candidates"
	response.Invitation = &db.WorkInstallInvitation{InvitationID: "xiei_" + strings.Repeat("a", 32), State: "candidates",
		SourceInstance: "openclaw-" + strings.Repeat("a", 32), ExpiresAt: time.Now().UTC().Add(time.Hour)}
	if request.Action == workinstallcontrol.ActionIssue {
		response.State = "invited"
		response.Invitation.State = "invited"
		response.Invitation.InvitationToken = "xiet_" + strings.Repeat("A", 43)
	}
	if request.Action == workinstallcontrol.ActionRenew {
		expiry := time.Now().UTC().Add(90 * 24 * time.Hour)
		response.State, response.Invitation, response.ExpiresAt = "renewed", nil, &expiry
	}
	if request.Action == workinstallcontrol.ActionFutureCandidates || request.Action == workinstallcontrol.ActionFutureDecisions {
		response.State, response.Invitation.State = "approved", "approved"
	}
	return response, nil
}

func TestWorkInstallManagedBearerAndHumanFutureDecisionRemainSeparate(t *testing.T) {
	controller := &workInstallControllerStub{}
	service := newAPITestAccessService(t, access.ServiceOptions{})
	identityID := "xsi_" + strings.Repeat("A", 22)
	agent := &agentAccessStoreStub{token: "xib_" + strings.Repeat("A", 43), authentication: workspace.ServiceAuthentication{
		Identity:    workspace.ServiceIdentity{ID: identityID, Status: "active", Permissions: []string{"work_event.ingest"}},
		TokenDigest: "sha256:" + strings.Repeat("a", 64),
	}}
	handler := api.NewHandlerWithDependencies(api.HandlerDependencies{Store: &fakeStore{}, WorkspaceStore: &workspaceStoreStub{},
		AgentAccessStore: agent, AccessService: service, WorkInstallController: controller, Now: func() time.Time { return time.Now().UTC() }})
	request := func(path, body string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "https://xinsight.local"+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+agent.token)
		r.Header.Set("Content-Type", "application/json")
		return r
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request("/api/v1/work-event-install/renew", `{"replacement_token":"xib_`+strings.Repeat("B", 43)+`"}`))
	if response.Code != 200 || len(controller.calls) != 1 || controller.calls[0].Action != workinstallcontrol.ActionRenew ||
		controller.calls[0].IdentityID != identityID || strings.Contains(response.Body.String(), "replacement_token") {
		t.Fatalf("managed renewal route %d %s calls=%+v", response.Code, response.Body.String(), controller.calls)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request("/api/v1/work-event-install/future-candidates", `{"projects":[{"project_key":"agent:new","label":"New"}]}`))
	if response.Code != 200 || len(controller.calls) != 2 || controller.calls[1].Action != workinstallcontrol.ActionFutureCandidates {
		t.Fatalf("future bearer route %d %s", response.Code, response.Body.String())
	}
	token, auth, err := service.Login("admin.one", []byte("admin password"), "")
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: access.SessionCookieName, Value: token}
	url := "/api/v1/work-event-install/invitations/xiei_" + strings.Repeat("a", 32) + "/future-decisions"
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, productRequest(http.MethodPost, url, `{"mappings":[],"excluded_project_keys":["agent:new"]}`, cookie, service.CSRFToken(auth)))
	if response.Code != 200 || len(controller.calls) != 3 || controller.calls[2].Action != workinstallcontrol.ActionFutureDecisions ||
		len(controller.calls[2].ExcludedProjectKeys) != 1 {
		t.Fatalf("human future decision %d %s", response.Code, response.Body.String())
	}
}

func TestWorkInstallHTTPSeparatesHumanAdminFromBearerHost(t *testing.T) {
	controller := &workInstallControllerStub{}
	service := newAPITestAccessService(t, access.ServiceOptions{})
	handler := api.NewHandlerWithDependencies(api.HandlerDependencies{Store: &fakeStore{}, WorkspaceStore: &workspaceStoreStub{},
		AccessService: service, WorkInstallController: controller, Now: func() time.Time { return time.Now().UTC() }})
	login := func(user, password string) (*http.Cookie, string) {
		token, auth, err := service.Login(user, []byte(password), "")
		if err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: access.SessionCookieName, Value: token}, service.CSRFToken(auth)
	}
	admin, csrf := login("admin.one", "admin password")
	operator, operatorCSRF := login("operator.one", "operator password")
	issue := `{"display_name":"Managed OpenClaw","expires_in_seconds":3600}`
	for _, item := range []struct {
		cookie *http.Cookie
		csrf   string
		want   int
	}{
		{operator, operatorCSRF, 403}, {admin, "", 403},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, productRequest(http.MethodPost, "/api/v1/work-event-install/invitations", issue, item.cookie, item.csrf))
		if response.Code != item.want || len(controller.calls) != 0 {
			t.Fatalf("issue admission %d calls=%d", response.Code, len(controller.calls))
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, productRequest(http.MethodPost, "/api/v1/work-event-install/invitations", issue, admin, csrf))
	if response.Code != 201 || len(controller.calls) != 1 || controller.calls[0].Action != workinstallcontrol.ActionIssue ||
		!strings.Contains(response.Body.String(), `"invitation_token":"xiet_`) {
		t.Fatalf("admin issue %d %s", response.Code, response.Body.String())
	}
	hostBody := `{"invitation_token":"xiet_` + strings.Repeat("A", 43) + `","ingest_token":"xib_` + strings.Repeat("A", 43) + `","projects":[{"project_key":"agent:main","label":"Main"}]}`
	host := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "https://xinsight.local/api/v1/work-event-install/candidates", strings.NewReader(hostBody))
		r.Header.Set("Content-Type", "application/json")
		return r
	}
	request := host()
	request.AddCookie(admin)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 403 || len(controller.calls) != 1 {
		t.Fatalf("cookie reached bearer endpoint %d", response.Code)
	}
	request = host()
	request.Header.Set("Authorization", "Bearer xib_invalid")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 403 || len(controller.calls) != 1 {
		t.Fatalf("other auth reached bearer endpoint %d", response.Code)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, host())
	if response.Code != 200 || len(controller.calls) != 2 || controller.calls[1].Action != workinstallcontrol.ActionCandidates ||
		controller.calls[1].ActorSubject != "" {
		t.Fatalf("host candidate %d %s", response.Code, response.Body.String())
	}
	pickupBody := `{"invitation_token":"xiet_` + strings.Repeat("A", 43) + `","ingest_token":"xib_` + strings.Repeat("A", 43) + `"}`
	request = httptest.NewRequest(http.MethodPost, "https://xinsight.local/api/v1/work-event-install/pickup", strings.NewReader(pickupBody))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 || len(controller.calls) != 3 || controller.calls[2].IngestToken == "" {
		t.Fatalf("host pickup %d %s", response.Code, response.Body.String())
	}
}

func TestWorkInstallStatusUsesOnlyManagedBearerAndBoundedSnapshot(t *testing.T) {
	controller := &workInstallControllerStub{}
	service := newAPITestAccessService(t, access.ServiceOptions{})
	identityID := "xsi_" + strings.Repeat("A", 22)
	agent := &agentAccessStoreStub{token: "xib_" + strings.Repeat("A", 43), authentication: workspace.ServiceAuthentication{
		Identity:    workspace.ServiceIdentity{ID: identityID, Status: "active", Permissions: []string{"work_event.ingest"}},
		TokenDigest: "sha256:" + strings.Repeat("a", 64),
	}}
	handler := api.NewHandlerWithDependencies(api.HandlerDependencies{Store: &fakeStore{}, WorkspaceStore: &workspaceStoreStub{},
		AgentAccessStore: agent, AccessService: service, WorkInstallController: controller, Now: func() time.Time { return time.Now().UTC() }})
	valid := `{"status":{"metadata_pending":0,"metadata_blocked":1,"body_pending":0,"gaps":[{"code":"body_limit","occurrences":2}]}}`
	request := func(origin, body string, bearer bool) *http.Request {
		r := httptest.NewRequest(http.MethodPost, origin+"/api/v1/work-event-install/status", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if bearer {
			r.Header.Set("Authorization", "Bearer "+agent.token)
		}
		return r
	}
	for _, body := range []string{
		`{}`, `{"status":null}`,
		`{"status":{"metadata_pending":0,"metadata_blocked":0,"body_pending":0}}`,
		`{"status":{"metadata_pending":0,"metadata_blocked":0,"body_pending":0,"gaps":null}}`,
		`{"status":{"metadata_pending":0,"metadata_blocked":0,"body_pending":0,"gaps":[{"code":"body_item_limit","occurrences":1}]}}`,
		`{"status":{"metadata_pending":0,"metadata_blocked":0,"body_pending":0,"gaps":[{"code":"body_limit","occurrences":1},{"code":"body_limit","occurrences":2}]}}`,
		`{"status":{"metadata_pending":9007199254740992,"metadata_blocked":0,"body_pending":0,"gaps":[]}}`,
		`{"status":{"metadata_pending":0,"metadata_pending":1,"metadata_blocked":0,"body_pending":0,"gaps":[]}}`,
		`{"status":{"metadata_pending":0,"metadata_blocked":0,"body_pending":0,"gaps":[],"message":"secret"}}`,
		`{"installation_id":"xiei_` + strings.Repeat("b", 32) + `","status":{"metadata_pending":0,"metadata_blocked":0,"body_pending":0,"gaps":[]}}`,
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request("https://xinsight.local", body, true))
		if response.Code != http.StatusBadRequest || len(controller.calls) != 0 {
			t.Fatalf("invalid status reached controller: %d %s", response.Code, body)
		}
	}
	for _, r := range []*http.Request{
		request("https://xinsight.local", valid, false),
		request("http://xinsight.local", valid, true),
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		if response.Code < 400 || len(controller.calls) != 0 {
			t.Fatalf("transport/auth accepted: %d", response.Code)
		}
	}
	withCookie := request("https://xinsight.local", valid, true)
	withCookie.AddCookie(&http.Cookie{Name: access.SessionCookieName, Value: "unexpected"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, withCookie)
	if response.Code < 400 || len(controller.calls) != 0 {
		t.Fatalf("cookie accepted: %d", response.Code)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request("https://xinsight.local", valid, true))
	if response.Code != 200 || len(controller.calls) != 1 || controller.calls[0].Action != workinstallcontrol.ActionStatus ||
		controller.calls[0].IdentityID != identityID || controller.calls[0].TokenDigest != agent.authentication.TokenDigest ||
		controller.calls[0].InvitationID != "" || !strings.Contains(response.Body.String(), `"state":"reported"`) ||
		!strings.Contains(response.Body.String(), `"invitation_id":"xiei_`) || strings.Contains(response.Body.String(), `"invitation":`) {
		t.Fatalf("status admission %d %s calls=%+v", response.Code, response.Body.String(), controller.calls)
	}
}
