package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/urmachine"
	"github.com/foigent/x-insight/internal/reviewpolicycontrol"
)

type reviewPolicyWorkspaceStub struct {
	workspaceStoreStub
	current    db.ReviewCaptureDefault
	catalog    db.ReviewCaptureProjectCatalog
	exceptions db.ReviewCaptureExceptionCatalog
}

func (s *reviewPolicyWorkspaceStub) ReadReviewCaptureDefault(context.Context) (db.ReviewCaptureDefault, error) {
	return s.current, nil
}
func (s *reviewPolicyWorkspaceStub) ListReviewCaptureProjects(context.Context) (db.ReviewCaptureProjectCatalog, error) {
	return s.catalog, nil
}
func (s *reviewPolicyWorkspaceStub) ListReviewCaptureExceptions(context.Context) (db.ReviewCaptureExceptionCatalog, error) {
	return s.exceptions, nil
}
func (s *reviewPolicyWorkspaceStub) ReadReviewCaptureException(_ context.Context, ref string) (db.ReviewCaptureExceptionState, error) {
	for _, item := range s.exceptions.Items {
		if item.PolicyRef == ref {
			return item, nil
		}
	}
	return db.ReviewCaptureExceptionState{}, errors.New("not found")
}

type reviewPolicyControllerStub struct {
	store                               *reviewPolicyWorkspaceStub
	applyCalls, confirmCalls, bindCalls int
	urCalls                             int
	lastUR                              reviewpolicycontrol.Request
	exceptionCalls                      int
}

func (c *reviewPolicyControllerStub) ApplyException(_ context.Context, request reviewpolicycontrol.Request) (reviewpolicycontrol.Response, error) {
	c.exceptionCalls++
	item := db.ReviewCaptureExceptionState{ReviewCaptureException: *request.Exception,
		DesiredRevision: request.ExpectedRevision + 1, EffectiveRevision: request.ExpectedRevision + 1,
		EffectiveMode: request.Exception.Mode}
	if item.PolicyRef == "" {
		item.PolicyRef = "xipe_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	}
	c.store.exceptions.Items = append(c.store.exceptions.Items, item)
	return reviewpolicycontrol.Response{SchemaVersion: reviewpolicycontrol.ResponseSchema,
		Action: request.Action, ActorSubject: request.ActorSubject, ExceptionRef: item.PolicyRef,
		DesiredRevision: item.DesiredRevision, EffectiveRevision: item.EffectiveRevision, State: "effective"}, nil
}
func (c *reviewPolicyControllerStub) ConfirmException(_ context.Context, request reviewpolicycontrol.Request) (reviewpolicycontrol.Response, error) {
	return reviewpolicycontrol.Response{SchemaVersion: reviewpolicycontrol.ResponseSchema,
		Action: request.Action, ActorSubject: request.ActorSubject, ExceptionRef: request.ExceptionRef,
		DesiredRevision: request.ExpectedRevision, EffectiveRevision: request.ExpectedRevision, State: "effective"}, nil
}

func (c *reviewPolicyControllerStub) ApplyDefault(_ context.Context, request reviewpolicycontrol.Request) (reviewpolicycontrol.Response, error) {
	c.applyCalls++
	c.store.current.Desired = request.Settings
	c.store.current.DesiredRevision = request.ExpectedRevision + 1
	return reviewpolicycontrol.Response{SchemaVersion: reviewpolicycontrol.ResponseSchema, Action: request.Action,
		ActorSubject: request.ActorSubject, DesiredRevision: c.store.current.DesiredRevision, State: "pending"}, nil
}
func (c *reviewPolicyControllerStub) ConfirmDefault(_ context.Context, request reviewpolicycontrol.Request) (reviewpolicycontrol.Response, error) {
	c.confirmCalls++
	c.store.current.Effective = &c.store.current.Desired
	c.store.current.EffectiveRevision = request.ExpectedRevision
	return reviewpolicycontrol.Response{SchemaVersion: reviewpolicycontrol.ResponseSchema, Action: request.Action,
		ActorSubject: request.ActorSubject, DesiredRevision: request.ExpectedRevision,
		EffectiveRevision: request.ExpectedRevision, State: "effective"}, nil
}
func (c *reviewPolicyControllerStub) BindProject(_ context.Context, request reviewpolicycontrol.Request) (reviewpolicycontrol.Response, error) {
	c.bindCalls++
	for i := range c.store.catalog.Items {
		item := &c.store.catalog.Items[i]
		if item.SourceSystemID == request.Project.SourceSystemID && item.SourceInstance == request.Project.SourceInstance &&
			item.OrgID == request.Project.OrgID && item.ProjectID == request.Project.ProjectID {
			item.XIProjectID, item.XIProjectName = request.Project.XIProjectID, request.Project.DisplayName
		}
	}
	return reviewpolicycontrol.Response{SchemaVersion: reviewpolicycontrol.ResponseSchema, Action: request.Action,
		ActorSubject: request.ActorSubject, State: "bound", Project: request.Project}, nil
}

func (c *reviewPolicyControllerStub) URPolicy(_ context.Context, request reviewpolicycontrol.Request) (reviewpolicycontrol.Response, error) {
	c.urCalls++
	c.lastUR = request
	response := reviewpolicycontrol.Response{SchemaVersion: reviewpolicycontrol.ResponseSchema,
		Action: request.Action, ActorSubject: request.ActorSubject, UR: &reviewpolicycontrol.URResponse{}}
	if request.Action == reviewpolicycontrol.ActionURScopes {
		response.State = "read"
		response.UR.Scopes = &urmachine.PolicyScopes{SchemaVersion: "unirouter.xinsight.policy-scopes.v1",
			InstallationID: "xci_synthetic", SourceInstance: "ur-a", OrgID: "7"}
	} else {
		response.State = "requested"
		response.UR.Operation = &db.ReviewCaptureUROperation{OperationRef: request.UR.OperationRef, State: "requested"}
	}
	return response, nil
}

func TestReviewCaptureDefaultAdminRequiresRoleCSRFAndExplicitConfirmation(t *testing.T) {
	store := &reviewPolicyWorkspaceStub{current: db.ReviewCaptureDefault{
		DesiredRevision: 1, EffectiveRevision: 1, Desired: db.ReviewCaptureSettings{
			Mode: "disabled", RetentionSeconds: 3600, ProjectQuotaBytes: 1024, MaxItemBytes: 0},
		RequestedAt: time.Now().UTC(),
	}, catalog: db.ReviewCaptureProjectCatalog{Items: []db.ReviewCaptureProjectChoice{{
		SourceSystemID: "11111111-1111-4111-8111-111111111111", SourceInstance: "ur-a", OrgID: "7", ProjectID: "9",
		SourceName: "Synthetic UR", SourceType: "unirouter"}}}}
	controller := &reviewPolicyControllerStub{store: store}
	service := newAPITestAccessService(t, access.ServiceOptions{})
	handler := api.NewHandlerWithDependencies(api.HandlerDependencies{Store: &fakeStore{}, WorkspaceStore: store,
		AccessService: service, ReviewPolicyController: controller, Now: func() time.Time { return time.Now().UTC() }})
	login := func(user, password string) (*http.Cookie, string) {
		token, auth, err := service.Login(user, []byte(password), "")
		if err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: access.SessionCookieName, Value: token}, service.CSRFToken(auth)
	}
	adminCookie, csrf := login("admin.one", "admin password")
	operatorCookie, operatorCSRF := login("operator.one", "operator password")
	body := `{"expected_revision":1,"settings":{"mode":"full_content","retention_seconds":7200,"project_quota_bytes":2048,"max_item_bytes":1024}}`
	for _, item := range []struct {
		name   string
		cookie *http.Cookie
		csrf   string
	}{
		{"operator", operatorCookie, operatorCSRF}, {"missing_csrf", adminCookie, ""},
	} {
		t.Run(item.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, productRequest(http.MethodPut, "/api/v1/review-capture/default", body, item.cookie, item.csrf))
			if response.Code != http.StatusForbidden || controller.applyCalls != 0 {
				t.Fatalf("status=%d calls=%d", response.Code, controller.applyCalls)
			}
		})
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, productRequest(http.MethodPut, "/api/v1/review-capture/default", body, adminCookie, csrf))
	if response.Code != http.StatusOK || controller.applyCalls != 1 || controller.confirmCalls != 0 ||
		!strings.Contains(response.Body.String(), `"drift":true`) {
		t.Fatalf("apply status=%d body=%s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, productRequest(http.MethodPost, "/api/v1/review-capture/default/confirm", `{"desired_revision":2}`, adminCookie, csrf))
	if response.Code != http.StatusOK || controller.confirmCalls != 1 ||
		!strings.Contains(response.Body.String(), `"drift":false`) {
		t.Fatalf("confirm status=%d body=%s", response.Code, response.Body.String())
	}
	projectBody := `{"source_system_id":"11111111-1111-4111-8111-111111111111","source_instance":"ur-a","org_id":"7","project_id":"9","xi_project_id":"22222222-2222-4222-8222-222222222222","display_name":"Synthetic XI project"}`
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, productRequest(http.MethodPost, "/api/v1/review-capture/projects", projectBody, operatorCookie, operatorCSRF))
	if response.Code != http.StatusForbidden || controller.bindCalls != 0 {
		t.Fatalf("operator bound XI project: %d", response.Code)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, productRequest(http.MethodPost, "/api/v1/review-capture/projects", projectBody, adminCookie, ""))
	if response.Code != http.StatusForbidden || controller.bindCalls != 0 {
		t.Fatalf("missing CSRF bound XI project: %d", response.Code)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, productRequest(http.MethodPost, "/api/v1/review-capture/projects", projectBody, adminCookie, csrf))
	if response.Code != http.StatusOK || controller.bindCalls != 1 ||
		!strings.Contains(response.Body.String(), `"xi_project_id":"22222222-2222-4222-8222-222222222222"`) {
		t.Fatalf("admin binding readback status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestReviewCaptureURAdminRequiresRoleAndCSRF(t *testing.T) {
	store := &reviewPolicyWorkspaceStub{}
	controller := &reviewPolicyControllerStub{store: store}
	service := newAPITestAccessService(t, access.ServiceOptions{})
	handler := api.NewHandlerWithDependencies(api.HandlerDependencies{Store: &fakeStore{}, WorkspaceStore: store,
		AccessService: service, ReviewPolicyController: controller, Now: func() time.Time { return time.Now().UTC() }})
	login := func(user, password string) (*http.Cookie, string) {
		token, auth, err := service.Login(user, []byte(password), "")
		if err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: access.SessionCookieName, Value: token}, service.CSRFToken(auth)
	}
	admin, csrf := login("admin.one", "admin password")
	operator, operatorCSRF := login("operator.one", "operator password")
	for _, item := range []struct {
		cookie             *http.Cookie
		csrf               string
		method, path, body string
		want               int
	}{
		{operator, operatorCSRF, http.MethodGet, "/api/v1/review-capture/unirouter/scopes", "", http.StatusForbidden},
		{operator, operatorCSRF, http.MethodPut, "/api/v1/review-capture/unirouter/projects/9", `{}`, http.StatusForbidden},
		{admin, "", http.MethodPut, "/api/v1/review-capture/unirouter/projects/9", `{}`, http.StatusForbidden},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, productRequest(item.method, item.path, item.body, item.cookie, item.csrf))
		if response.Code != item.want || controller.urCalls != 0 {
			t.Fatalf("UR authority status=%d calls=%d", response.Code, controller.urCalls)
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, productRequest(http.MethodGet, "/api/v1/review-capture/unirouter/scopes", "", admin, ""))
	if response.Code != http.StatusOK || controller.urCalls != 1 || controller.lastUR.Action != reviewpolicycontrol.ActionURScopes {
		t.Fatalf("UR discovery status=%d calls=%d body=%s", response.Code, controller.urCalls, response.Body.String())
	}
	response = httptest.NewRecorder()
	policy := `{"schema_version":"unirouter.payload-policy.v2","expected_policy_ref":"content-policy:7:9","expected_version":"1","expected_vector":"{\"enterprise\":1,\"project\":1}","desired":{"mode":"metadata_only","supported_protocols":["openai.chat"],"observation_points":["client_request"],"allowed_purposes":[]},"execution_storage_mode":"temporary_allowed"}`
	body := `{"delegation_id":"xipg_1","delegation_revision":"1","operation_ref":"xipw_11111111111111111111111111111111","policy":` + policy + `}`
	handler.ServeHTTP(response, productRequest(http.MethodPut, "/api/v1/review-capture/unirouter/projects/9", body, admin, csrf))
	if response.Code != http.StatusOK || controller.urCalls != 2 || !reviewpolicycontrol.ValidRequest(controller.lastUR) ||
		controller.lastUR.UR.OperationRef != "xipw_11111111111111111111111111111111" || controller.lastUR.UR.ProjectID != "9" {
		t.Fatalf("UR delegated write status=%d calls=%d body=%s", response.Code, controller.urCalls, response.Body.String())
	}
	response = httptest.NewRecorder()
	body = `{"delegation_id":"xipg_1","delegation_revision":"1","operation_ref":"xipw_11111111111111111111111111111111","policy":` + policy + `,"retry_unconfirmed":true}`
	handler.ServeHTTP(response, productRequest(http.MethodPut, "/api/v1/review-capture/unirouter/projects/9", body, admin, csrf))
	if response.Code != http.StatusOK || controller.urCalls != 3 || !controller.lastUR.UR.RetryUnconfirmed {
		t.Fatalf("explicit same-ref retry flag lost: status=%d calls=%d", response.Code, controller.urCalls)
	}
}

func TestReviewCaptureURScopedAdminRoutes(t *testing.T) {
	store := &reviewPolicyWorkspaceStub{}
	controller := &reviewPolicyControllerStub{store: store}
	service := newAPITestAccessService(t, access.ServiceOptions{})
	handler := api.NewHandlerWithDependencies(api.HandlerDependencies{Store: &fakeStore{}, WorkspaceStore: store,
		AccessService: service, ReviewPolicyController: controller, Now: func() time.Time { return time.Now().UTC() }})
	token, auth, err := service.Login("admin.one", []byte("admin password"), "")
	if err != nil {
		t.Fatal(err)
	}
	admin := &http.Cookie{Name: access.SessionCookieName, Value: token}
	csrf := service.CSRFToken(auth)
	policy := `{"schema_version":"unirouter.payload-policy.v2","expected_policy_ref":"content-policy:7:enterprise","expected_version":"1","expected_vector":"{\"enterprise\":1}","desired":{"mode":"metadata_only"},"execution_storage_mode":"temporary_allowed"}`
	body := `{"delegation_id":"xipg_1","delegation_revision":"1","operation_ref":"xipw_11111111111111111111111111111111","policy":` + policy + `}`
	for _, test := range []struct{ method, path, body, action, org, project, user string }{
		{http.MethodGet, "/api/v1/review-capture/unirouter/organizations/7?delegation_id=xipg_1", "", reviewpolicycontrol.ActionURReadEnterprise, "7", "", ""},
		{http.MethodPut, "/api/v1/review-capture/unirouter/organizations/7", body, reviewpolicycontrol.ActionURApplyEnterprise, "7", "", ""},
		{http.MethodGet, "/api/v1/review-capture/unirouter/projects/9/users?cursor=5", "", reviewpolicycontrol.ActionURUsers, "", "9", ""},
		{http.MethodGet, "/api/v1/review-capture/unirouter/projects/9/users/5?delegation_id=xipg_1", "", reviewpolicycontrol.ActionURReadUser, "", "9", "5"},
		{http.MethodPut, "/api/v1/review-capture/unirouter/projects/9/users/5", body, reviewpolicycontrol.ActionURApplyUser, "", "9", "5"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, productRequest(test.method, test.path, test.body, admin, csrf))
		if response.Code != http.StatusOK || controller.lastUR.Action != test.action || controller.lastUR.UR.OrgID != test.org ||
			controller.lastUR.UR.ProjectID != test.project || controller.lastUR.UR.UserID != test.user {
			t.Fatalf("route %s %s status=%d command=%+v body=%s", test.method, test.path, response.Code, controller.lastUR, response.Body.String())
		}
	}
	for _, path := range []string{"/api/v1/review-capture/unirouter/organizations/0?delegation_id=xipg_1",
		"/api/v1/review-capture/unirouter/projects/9/users/0?delegation_id=xipg_1"} {
		before := controller.urCalls
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, productRequest(http.MethodGet, path, "", admin, ""))
		if response.Code != http.StatusBadRequest || controller.urCalls != before {
			t.Fatalf("invalid route %s admitted: %d", path, response.Code)
		}
	}
	joint := `{"delegation_id":"xipg_1","delegation_revision":"1","operation_ref":"xipw_22222222222222222222222222222222","policy":` + policy +
		`,"xi_expected_revision":0,"xi_settings":{"mode":"disabled","retention_seconds":3600,"project_quota_bytes":1048576,"max_item_bytes":1024}}`
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, productRequest(http.MethodPut, "/api/v1/review-capture/unirouter/organizations/7", joint, admin, csrf))
	if response.Code != http.StatusOK || controller.lastUR.UR.XISettings == nil || controller.lastUR.UR.XISettings.Mode != "disabled" {
		t.Fatalf("enterprise joint request lost: status=%d body=%s", response.Code, response.Body.String())
	}
	before := controller.urCalls
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, productRequest(http.MethodPut, "/api/v1/review-capture/unirouter/projects/9", joint, admin, csrf))
	if response.Code != http.StatusBadRequest || controller.urCalls != before {
		t.Fatalf("project joint modified global intent: status=%d calls=%d", response.Code, controller.urCalls)
	}
}

func TestReviewCaptureExceptionAdminBoundary(t *testing.T) {
	store := &reviewPolicyWorkspaceStub{exceptions: db.ReviewCaptureExceptionCatalog{
		Items: []db.ReviewCaptureExceptionState{}, Accounts: []db.VerifiedReviewAccount{}}}
	controller := &reviewPolicyControllerStub{store: store}
	service := newAPITestAccessService(t, access.ServiceOptions{})
	handler := api.NewHandlerWithDependencies(api.HandlerDependencies{Store: &fakeStore{}, WorkspaceStore: store,
		AccessService: service, ReviewPolicyController: controller, Now: func() time.Time { return time.Now().UTC() }})
	login := func(user, password string) (*http.Cookie, string) {
		token, auth, err := service.Login(user, []byte(password), "")
		if err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: access.SessionCookieName, Value: token}, service.CSRFToken(auth)
	}
	admin, csrf := login("admin.one", "admin password")
	operator, operatorCSRF := login("operator.one", "operator password")
	body := `{"expected_revision":0,"exception":{"selector_kind":"tenant_category","content_kind":"model_input",` +
		`"mode":"disabled","retention_seconds":3600,"max_item_bytes":0}}`
	for _, item := range []struct {
		cookie *http.Cookie
		csrf   string
	}{{operator, operatorCSRF}, {admin, ""}} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, productRequest(http.MethodPut, "/api/v1/review-capture/exceptions", body, item.cookie, item.csrf))
		if response.Code != http.StatusForbidden || controller.exceptionCalls != 0 {
			t.Fatalf("unprivileged exception write: status=%d calls=%d", response.Code, controller.exceptionCalls)
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, productRequest(http.MethodPut, "/api/v1/review-capture/exceptions", body, admin, csrf))
	if response.Code != http.StatusOK || controller.exceptionCalls != 1 ||
		!strings.Contains(response.Body.String(), `"policy_ref":"xipe_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`) {
		t.Fatalf("exception write readback: status=%d body=%s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, productRequest(http.MethodGet, "/api/v1/review-capture/exceptions", "", admin, ""))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"verified_accounts":[]`) {
		t.Fatalf("exception catalog: status=%d body=%s", response.Code, response.Body.String())
	}
}
