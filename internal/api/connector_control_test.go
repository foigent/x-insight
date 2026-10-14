package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/workspace"
)

func TestConnectorInvitationRequiresOperatorConsentAndBindsFixedScope(t *testing.T) {
	store := &connectorStoreStub{}
	handler, cookie, csrf := connectorTestHandler(t, store, nil, "operator.one", "operator password")
	request := productRequest(http.MethodPost, "/api/v1/connectors/invitations", `{}`, cookie, csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || store.created.ActorSubject != "xs_fedcba9876543210" ||
		store.created.Config != connectorTestConfig() ||
		!strings.Contains(response.Body.String(), `"requested_event_scope":["model.request.completed"]`) ||
		strings.Contains(response.Body.String(), `"project_id"`) || strings.Contains(response.Body.String(), `"token"`) {
		t.Fatalf("status=%d input=%+v body=%s", response.Code, store.created, response.Body.String())
	}

	deniedStore := &connectorStoreStub{}
	deniedHandler, deniedCookie, deniedCSRF := connectorTestHandler(
		t, deniedStore, nil, "investigator.one", "test password",
	)
	denied := productRequest(http.MethodPost, "/api/v1/connectors/invitations", `{}`, deniedCookie, deniedCSRF)
	response = httptest.NewRecorder()
	deniedHandler.ServeHTTP(response, denied)
	if response.Code != http.StatusForbidden || deniedStore.created.ActorSubject != "" {
		t.Fatalf("denied status=%d body=%s created=%+v", response.Code, response.Body.String(), deniedStore.created)
	}
}

func TestConnectorAgentRedeemPollAndResultAreSeparateScopedMachineActions(t *testing.T) {
	store := &connectorStoreStub{}
	agent := &agentAccessStoreStub{
		token: "connector-control-token",
		authentication: workspace.ServiceAuthentication{Identity: workspace.ServiceIdentity{
			ID: "xsi_AAAAAAAAAAAAAAAAAAAAAA", Status: "active",
			Permissions: []string{connectorcontrol.PermissionControlPoll, connectorcontrol.PermissionControlAck},
		}},
	}
	handler, _, _ := connectorTestHandler(t, store, agent, "operator.one", "operator password")

	now := store.now
	redeemBody := `{"invitation_token":"xiiv1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","project_id":"project.fixture","final_event_scope":["model.request.completed"],"consent_sha256":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","confirmed_at":"` + now.Format(time.RFC3339) + `","control_token":"xib_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","control_token_expires_at":"` + now.Add(24*time.Hour).Format(time.RFC3339) + `"}`
	redeem := httptest.NewRequest(
		http.MethodPost, "https://xinsight.local/api/v1/connector-control/invitations/redeem",
		strings.NewReader(redeemBody),
	)
	redeem.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, redeem)
	if response.Code != http.StatusOK || store.redeemed.ProjectID != "project.fixture" ||
		strings.Contains(response.Body.String(), "xib_AAAAA") {
		t.Fatalf("redeem status=%d input=%+v body=%s", response.Code, store.redeemed, response.Body.String())
	}

	poll := httptest.NewRequest(
		http.MethodGet,
		"https://xinsight.local/api/v1/connector-control/operations?installation_id=xci_AAAAAAAAAAAAAAAAAAAAAA",
		nil,
	)
	poll.Header.Set("Authorization", "Bearer "+agent.token)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, poll)
	if response.Code != http.StatusOK || store.polledIdentity != agent.authentication.Identity.ID ||
		!strings.Contains(response.Body.String(), `"kind":"connect"`) {
		t.Fatalf("poll status=%d body=%s identity=%q", response.Code, response.Body.String(), store.polledIdentity)
	}

	resultBody := `{"schema_version":"xinsight.connector-control-result.v2","operation_id":"xco_AAAAAAAAAAAAAAAAAAAAAA","installation_id":"xci_AAAAAAAAAAAAAAAAAAAAAA","phase":"prepare_paused_source","status":"completed","result":{"source_instance":"source.fixture.v1","cursor":"cursor-1","high_watermark":"cursor-1","source_state":"paused","readiness":"paused_source_cursor_ready"},"observed_at":"` + now.Format(time.RFC3339) + `"}`
	result := httptest.NewRequest(
		http.MethodPost,
		"https://xinsight.local/api/v1/connector-control/operations/xco_AAAAAAAAAAAAAAAAAAAAAA",
		strings.NewReader(resultBody),
	)
	result.Header.Set("Authorization", "Bearer "+agent.token)
	result.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, result)
	if response.Code != http.StatusOK || store.completed.Result == nil ||
		store.completed.Result.SourceInstance != "source.fixture.v1" {
		t.Fatalf("result status=%d body=%s completed=%+v", response.Code, response.Body.String(), store.completed)
	}
	store.completed = connectorcontrol.ControlOperationResult{}
	duplicateResult := strings.Replace(
		resultBody,
		`"phase":"prepare_paused_source"`,
		`"phase":"prepare_paused_source","phase":"prepare_paused_source"`,
		1,
	)
	duplicate := httptest.NewRequest(
		http.MethodPost,
		"https://xinsight.local/api/v1/connector-control/operations/xco_AAAAAAAAAAAAAAAAAAAAAA",
		strings.NewReader(duplicateResult),
	)
	duplicate.Header.Set("Authorization", "Bearer "+agent.token)
	duplicate.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, duplicate)
	if response.Code != http.StatusBadRequest || store.completed.OperationID != "" {
		t.Fatalf("duplicate result status=%d body=%s completed=%+v", response.Code, response.Body.String(), store.completed)
	}
}

func TestConnectorResultCommitsBeforeFixedControllerWake(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		state      string
		reasonCode string
	}{
		{name: "ready", state: "ready"},
		{name: "deferred", state: "deferred", reasonCode: "controller_not_activated"},
		{name: "abnormal", state: "abnormal", reasonCode: "local_state_drift"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			events := []string{}
			store := &connectorStoreStub{events: &events}
			controller := &connectorControllerStub{events: &events}
			controller.execute = func(
				request connectorcontrol.ControllerRequest,
				_ int,
			) (connectorcontrol.ControllerResponse, error) {
				if store.completed.OperationID != request.OperationID || store.completed.InstallationID != request.InstallationID {
					t.Fatalf("wake occurred before durable completion: completed=%+v request=%+v", store.completed, request)
				}
				return connectorcontrol.ControllerResponse{
					SchemaVersion: connectorcontrol.LocalControllerResponseSchema,
					OperationID:   request.OperationID, InstallationID: request.InstallationID,
					State: testCase.state, ReasonCode: testCase.reasonCode,
				}, nil
			}
			handler, token := connectorAgentResultHandler(t, store, controller)
			response := postConnectorResult(handler, token, connectorResultBody(store.now))
			if response.Code != http.StatusOK || len(controller.requests) != 1 ||
				controller.requests[0] != (connectorcontrol.ControllerRequest{
					SchemaVersion: connectorcontrol.LocalControllerRequestSchema,
					OperationID:   "xco_AAAAAAAAAAAAAAAAAAAAAA", InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA",
					Action: connectorcontrol.ControllerActionPrepare,
				}) || strings.Join(events, ",") != "complete,wake" {
				t.Fatalf("status=%d body=%s requests=%+v events=%v", response.Code, response.Body.String(), controller.requests, events)
			}
		})
	}
}

func TestConnectorResultWakeLossIsRetriableAndExactReplayRewakes(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		firstResponse func(connectorcontrol.ControllerRequest) (connectorcontrol.ControllerResponse, error)
	}{
		{
			name: "socket_error",
			firstResponse: func(connectorcontrol.ControllerRequest) (connectorcontrol.ControllerResponse, error) {
				return connectorcontrol.ControllerResponse{}, errors.New("socket unavailable")
			},
		},
		{
			name: "local_state_unavailable",
			firstResponse: func(request connectorcontrol.ControllerRequest) (connectorcontrol.ControllerResponse, error) {
				return connectorcontrol.ControllerResponse{
					SchemaVersion: connectorcontrol.LocalControllerResponseSchema,
					OperationID:   request.OperationID, InstallationID: request.InstallationID,
					State: "abnormal", ReasonCode: "local_state_unavailable",
				}, nil
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			events := []string{}
			store := &connectorStoreStub{events: &events}
			controller := &connectorControllerStub{events: &events}
			controller.execute = func(request connectorcontrol.ControllerRequest, call int) (connectorcontrol.ControllerResponse, error) {
				if call == 1 {
					return testCase.firstResponse(request)
				}
				return connectorReadyResponse(request), nil
			}
			handler, token := connectorAgentResultHandler(t, store, controller)
			body := connectorResultBody(store.now)
			first := postConnectorResult(handler, token, body)
			second := postConnectorResult(handler, token, body)
			if first.Code != http.StatusServiceUnavailable || second.Code != http.StatusOK ||
				store.completeCalls != 2 || len(controller.requests) != 2 ||
				controller.requests[0] != controller.requests[1] ||
				strings.Join(events, ",") != "complete,wake,complete,wake" {
				t.Fatalf(
					"first=%d second=%d complete=%d requests=%+v events=%v",
					first.Code, second.Code, store.completeCalls, controller.requests, events,
				)
			}
		})
	}
}

func TestConnectorResultDoesNotWakeBeforeDurableCompletion(t *testing.T) {
	store := &connectorStoreStub{completeErr: connectorcontrol.ErrConflict}
	controller := &connectorControllerStub{}
	handler, token := connectorAgentResultHandler(t, store, controller)
	response := postConnectorResult(handler, token, connectorResultBody(store.now))
	if response.Code != http.StatusConflict || store.completeCalls != 1 || len(controller.requests) != 0 {
		t.Fatalf(
			"status=%d body=%s complete=%d requests=%+v",
			response.Code, response.Body.String(), store.completeCalls, controller.requests,
		)
	}
}

func connectorAgentResultHandler(
	t *testing.T,
	store *connectorStoreStub,
	controller connectorcontrol.Controller,
) (http.Handler, string) {
	t.Helper()
	agent := &agentAccessStoreStub{
		token: "connector-control-token",
		authentication: workspace.ServiceAuthentication{Identity: workspace.ServiceIdentity{
			ID: "xsi_AAAAAAAAAAAAAAAAAAAAAA", Status: "active",
			Permissions: []string{connectorcontrol.PermissionControlPoll, connectorcontrol.PermissionControlAck},
		}},
	}
	handler, _, _ := connectorTestHandler(t, store, agent, "operator.one", "operator password", controller)
	return handler, agent.token
}

func postConnectorResult(handler http.Handler, token string, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(
		http.MethodPost,
		"https://xinsight.local/api/v1/connector-control/operations/xco_AAAAAAAAAAAAAAAAAAAAAA",
		strings.NewReader(body),
	)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func connectorResultBody(now time.Time) string {
	return `{"schema_version":"xinsight.connector-control-result.v2","operation_id":"xco_AAAAAAAAAAAAAAAAAAAAAA","installation_id":"xci_AAAAAAAAAAAAAAAAAAAAAA","phase":"prepare_paused_source","status":"completed","result":{"source_instance":"source.fixture.v1","cursor":"cursor-1","high_watermark":"cursor-1","source_state":"paused","readiness":"paused_source_cursor_ready"},"observed_at":"` + now.Format(time.RFC3339) + `"}`
}

func TestConnectorInvitationRedeemRejectsInsecureOrAmbientBrowserCredentials(t *testing.T) {
	store := &connectorStoreStub{}
	handler, _, _ := connectorTestHandler(t, store, nil, "operator.one", "operator password")
	body := `{"invitation_token":"xiiv1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","project_id":"project.fixture","final_event_scope":["model.request.completed"],"consent_sha256":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","confirmed_at":"2026-08-11T03:00:00Z","control_token":"xib_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","control_token_expires_at":"2026-08-12T03:00:00Z"}`
	for _, testCase := range []struct {
		target    string
		configure func(*http.Request)
	}{
		{"http://xinsight.local/api/v1/connector-control/invitations/redeem", func(*http.Request) {}},
		{"https://xinsight.local/api/v1/connector-control/invitations/redeem", func(request *http.Request) { request.Header.Set("Authorization", "Bearer ambient") }},
		{"https://xinsight.local/api/v1/connector-control/invitations/redeem", func(request *http.Request) { request.AddCookie(&http.Cookie{Name: "ambient", Value: "browser"}) }},
	} {
		request := httptest.NewRequest(http.MethodPost, testCase.target, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		testCase.configure(request)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden || store.redeemed.InvitationToken != "" {
			t.Fatalf("status=%d body=%s redeemed=%+v", response.Code, response.Body.String(), store.redeemed)
		}
	}
}

func TestEvidenceIdentityCannotUseConnectorControlRoute(t *testing.T) {
	store := &connectorStoreStub{}
	agent := &agentAccessStoreStub{
		token: "evidence-agent-token",
		authentication: workspace.ServiceAuthentication{Identity: workspace.ServiceIdentity{
			ID: "xsi_BBBBBBBBBBBBBBBBBBBBBB", Status: "active",
			Permissions: []string{"evidence.read", "evidence.cite"},
		}},
	}
	handler, _, _ := connectorTestHandler(t, store, agent, "operator.one", "operator password")
	request := httptest.NewRequest(
		http.MethodGet,
		"https://xinsight.local/api/v1/connector-control/operations?installation_id=xci_AAAAAAAAAAAAAAAAAAAAAA",
		nil,
	)
	request.Header.Set("Authorization", "Bearer "+agent.token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || store.polledIdentity != "" {
		t.Fatalf("status=%d body=%s poll=%q", response.Code, response.Body.String(), store.polledIdentity)
	}
}

func connectorTestHandler(
	t *testing.T,
	store *connectorStoreStub,
	agent workspace.AgentAccessStore,
	username string,
	password string,
	controllers ...connectorcontrol.Controller,
) (http.Handler, *http.Cookie, string) {
	t.Helper()
	service := newAPITestAccessService(t, access.ServiceOptions{})
	token, authentication, err := service.Login(username, []byte(password), "")
	if err != nil {
		t.Fatal(err)
	}
	store.now = time.Date(2026, 8, 11, 3, 0, 0, 0, time.UTC)
	var controller connectorcontrol.Controller = &connectorControllerStub{}
	if len(controllers) == 1 && controllers[0] != nil {
		controller = controllers[0]
	}
	handler := api.NewHandlerWithDependencies(api.HandlerDependencies{
		Store:               &fakeStore{},
		WorkspaceStore:      store,
		AgentAccessStore:    agent,
		AccessService:       service,
		ConnectorConfig:     connectorTestConfig(),
		ConnectorIssuer:     store,
		ConnectorController: controller,
		Now:                 func() time.Time { return store.now },
	})
	return handler, &http.Cookie{Name: access.SessionCookieName, Value: token}, service.CSRFToken(authentication)
}

func connectorTestConfig() connectorcontrol.InvitationConfig {
	return connectorcontrol.InvitationConfig{
		DeploymentID:           "xid_AAAAAAAAAAAAAAAAAAAAAA",
		XInsightOrigin:         "https://xinsight.local",
		UniRouterInstallOrigin: "https://unirouter.local",
	}
}

type connectorStoreStub struct {
	workspaceStoreStub
	now            time.Time
	created        connectorcontrol.CreateInvitationInput
	redeemed       connectorcontrol.RedeemInvitationInput
	polledIdentity string
	completed      connectorcontrol.ControlOperationResult
	completeErr    error
	completeCalls  int
	events         *[]string
	pollOperation  *connectorcontrol.ControlOperation
}

func (store *connectorStoreStub) CreateInvitation(
	_ context.Context,
	input connectorcontrol.CreateInvitationInput,
) (connectorcontrol.InvitationPackage, connectorcontrol.Installation, error) {
	store.created = input
	installation := connectorFixtureInstallation(store.now)
	return connectorcontrol.InvitationPackage{
		SchemaVersion:       connectorcontrol.InvitationSchema,
		InvitationID:        "xii_AAAAAAAAAAAAAAAAAAAAAA",
		InstallationID:      installation.InstallationID,
		DeploymentID:        input.Config.DeploymentID,
		Audience:            connectorcontrol.ConnectorAudience,
		XInsightOrigin:      input.Config.XInsightOrigin,
		RequestedEventScope: []string{connectorcontrol.CompletedEventType},
		ExpiresAt:           store.now.Add(connectorcontrol.DefaultInvitationTTL),
		InstallURL:          "https://unirouter.local/connectors/x-insight/install#invitation=redacted",
		Token:               "not-serialized",
	}, installation, nil
}

func (*connectorStoreStub) ListInstallations(context.Context, int) ([]connectorcontrol.Installation, error) {
	return []connectorcontrol.Installation{}, nil
}

func (store *connectorStoreStub) RedeemInvitation(
	_ context.Context,
	input connectorcontrol.RedeemInvitationInput,
	_ time.Time,
	_ io.Reader,
) (connectorcontrol.RegistrationResult, error) {
	store.redeemed = input
	installation := connectorFixtureInstallation(store.now)
	installation.ProjectID = input.ProjectID
	installation.FinalEventScope = input.FinalEventScope
	installation.ControlServiceIdentityID = "xsi_AAAAAAAAAAAAAAAAAAAAAA"
	installation.InstallationAcknowledgementSHA256 = "sha256:" + strings.Repeat("c", 64)
	installation.ObservedState = "agent_registered"
	operation := connectorFixtureOperation(store.now)
	if store.pollOperation != nil {
		operation = *store.pollOperation
	}
	return connectorcontrol.RegistrationResult{
		Installation: installation,
		Operation:    operation,
	}, nil
}

func (store *connectorStoreStub) PollControlOperation(
	_ context.Context,
	identityID string,
	_ string,
	_ time.Time,
) (connectorcontrol.ControlOperation, error) {
	store.polledIdentity = identityID
	operation := connectorFixtureOperation(store.now)
	if store.pollOperation != nil {
		operation = *store.pollOperation
	}
	lease := store.now.Add(30 * time.Second)
	operation.Status = "running"
	operation.AttemptCount = 1
	operation.LeaseExpiresAt = &lease
	return operation, nil
}

func (store *connectorStoreStub) CompleteControlOperation(
	_ context.Context,
	_ string,
	result connectorcontrol.ControlOperationResult,
	_ time.Time,
) (connectorcontrol.Installation, error) {
	store.completeCalls++
	if store.events != nil {
		*store.events = append(*store.events, "complete")
	}
	if store.completeErr != nil {
		return connectorcontrol.Installation{}, store.completeErr
	}
	store.completed = result
	installation := connectorFixtureInstallation(store.now)
	installation.ProjectID = "project.fixture"
	installation.FinalEventScope = []string{connectorcontrol.CompletedEventType}
	installation.ControlServiceIdentityID = "xsi_AAAAAAAAAAAAAAAAAAAAAA"
	installation.InstallationAcknowledgementSHA256 = "sha256:" + strings.Repeat("c", 64)
	installation.SourceInstance = result.SourceInstance
	installation.SourceInstanceSHA256 = connectorcontrol.RawSHA256(result.SourceInstance)
	installation.ObservedState = "source_ready"
	return installation, nil
}

type connectorControllerStub struct {
	requests []connectorcontrol.ControllerRequest
	execute  func(connectorcontrol.ControllerRequest, int) (connectorcontrol.ControllerResponse, error)
	events   *[]string
}

func (controller *connectorControllerStub) Execute(
	_ context.Context,
	request connectorcontrol.ControllerRequest,
) (connectorcontrol.ControllerResponse, error) {
	controller.requests = append(controller.requests, request)
	if controller.events != nil {
		*controller.events = append(*controller.events, "wake")
	}
	if controller.execute != nil {
		return controller.execute(request, len(controller.requests))
	}
	return connectorReadyResponse(request), nil
}

func connectorReadyResponse(request connectorcontrol.ControllerRequest) connectorcontrol.ControllerResponse {
	return connectorcontrol.ControllerResponse{
		SchemaVersion:  connectorcontrol.LocalControllerResponseSchema,
		OperationID:    request.OperationID,
		InstallationID: request.InstallationID,
		State:          "ready",
	}
}

func connectorFixtureInstallation(now time.Time) connectorcontrol.Installation {
	return connectorcontrol.Installation{
		SchemaVersion:       connectorcontrol.InstallationSchema,
		InstallationID:      "xci_AAAAAAAAAAAAAAAAAAAAAA",
		DeploymentID:        "xid_AAAAAAAAAAAAAAAAAAAAAA",
		ConnectorType:       connectorcontrol.ConnectorTypeUniRouter,
		RequestedEventScope: []string{connectorcontrol.CompletedEventType},
		Status:              "pending", DesiredState: "active", ObservedState: "awaiting_consent",
		CreatedAt: now, UpdatedAt: now, Version: 1,
	}
}

func connectorFixtureOperation(now time.Time) connectorcontrol.ControlOperation {
	return connectorcontrol.ControlOperation{
		SchemaVersion:  connectorcontrol.ControlOperationSchema,
		OperationID:    "xco_AAAAAAAAAAAAAAAAAAAAAA",
		InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA",
		Kind:           "connect", ProjectID: "project.fixture",
		EventScope: []string{connectorcontrol.CompletedEventType}, DesiredState: "active",
		RequestSHA256: "sha256:" + strings.Repeat("d", 64), Status: "pending",
		AvailableAt: now, CreatedAt: now, UpdatedAt: now,
	}
}

func TestConnectorPollUsesFrozenPublicV2V3HTTPAuthority(t *testing.T) {
	prepare := connectorcontrol.ControlOperation{
		SchemaVersion: connectorcontrol.ControlOperationSchemaV2,
		OperationID:   "xco_AAAAAAAAAAAAAAAAAAAAAA", InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA",
		ProjectID: "2", EventScope: []string{connectorcontrol.CompletedEventType}, DesiredState: "active",
		Kind: "connect", Phase: connectorcontrol.ControlPhasePreparePausedSource,
		Request: &connectorcontrol.ControlPhaseRequestV2{FeedSchema: connectorcontrol.ControlFeedSchema, CursorKey: connectorcontrol.ControlCursorKey, RequiredSourceState: "paused"},
	}
	fixtures := []struct {
		name      string
		operation connectorcontrol.ControlOperation
		expected  any
	}{}
	request, _, _, err := connectorcontrol.CanonicalControlOperationRequestV2(prepare)
	if err != nil {
		t.Fatal(err)
	}
	fixtures = append(fixtures, struct {
		name      string
		operation connectorcontrol.ControlOperation
		expected  any
	}{"v2", prepare, request})
	for _, name := range []string{"01-bind-existing-operation.json", "03-confirm-existing-operation.json", "05-policy-pair-operation.json", "07-source-enroll-pair-operation.json"} {
		raw, err := os.ReadFile("../connectorcontrol/testdata/control-v3/valid/" + name)
		if err != nil {
			t.Fatal(err)
		}
		v, err := connectorcontrol.DecodeControlOperationRequestV3(raw)
		if err != nil {
			t.Fatal(err)
		}
		o := connectorcontrol.ControlOperation{SchemaVersion: v.SchemaVersion, OperationID: v.OperationID, InstallationID: v.InstallationID, ProjectID: v.ProjectID, EventScope: v.EventScope, DesiredState: v.DesiredState, Kind: v.Kind, Phase: v.Phase, PredecessorOperationID: v.PredecessorOperationID, PredecessorResultSHA256: v.PredecessorResultSHA256, RequestV3: &v.Request}
		fixtures = append(fixtures, struct {
			name      string
			operation connectorcontrol.ControlOperation
			expected  any
		}{name, o, v})
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			fixture.operation.RequestSHA256 = "sha256:" + strings.Repeat("a", 64)
			fixture.operation.ResultSHA256 = "sha256:" + strings.Repeat("b", 64)
			fixture.operation.ReasonCode = "internal_retry_detail"
			store := &connectorStoreStub{pollOperation: &fixture.operation}
			agent := &agentAccessStoreStub{token: "synthetic-poll", authentication: workspace.ServiceAuthentication{Identity: workspace.ServiceIdentity{ID: "xsi_AAAAAAAAAAAAAAAAAAAAAA", Status: "active", Permissions: []string{connectorcontrol.PermissionControlPoll}}}}
			handler, _, _ := connectorTestHandler(t, store, agent, "operator.one", "operator password")
			req := httptest.NewRequest(http.MethodGet, "https://xinsight.local/api/v1/connector-control/operations?installation_id="+fixture.operation.InstallationID, nil)
			req.Header.Set("Authorization", "Bearer "+agent.token)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != http.StatusOK {
				t.Fatalf("HTTP status=%d body=%s", response.Code, response.Body.String())
			}
			wire := response.Body.Bytes()
			if fixture.operation.SchemaVersion == connectorcontrol.ControlOperationSchemaV2 {
				if _, err := connectorcontrol.DecodeControlOperationRequestV2(wire); err != nil {
					t.Fatal("closed HTTP v2", err)
				}
			} else if _, err := connectorcontrol.DecodeControlOperationRequestV3(wire); err != nil {
				t.Fatal("closed HTTP v3", err)
			}
			for _, field := range []string{"request_sha256", "status", "attempt_count", "lease_expires_at", "available_at", "result_sha256", "reason_code", "created_at", "updated_at"} {
				var object map[string]json.RawMessage
				if json.Unmarshal(wire, &object) != nil || object[field] != nil {
					t.Fatalf("storage field crossed wire: %s", field)
				}
			}
			if !connectorcontrol.MatchesCanonicalDigest(json.RawMessage(wire), mustControlDigest(t, fixture.expected)) {
				t.Fatal("HTTP authority changed shared canonical bytes")
			}
			// Initial redeem returns an operation too; apply the same projection
			// inside its original two-field result, not a transport wrapper.
			redeemInput := connectorcontrol.RedeemInvitationInput{InvitationToken: "xiiv1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", ProjectID: fixture.operation.ProjectID, FinalEventScope: fixture.operation.EventScope, ConsentSHA256: "sha256:" + strings.Repeat("a", 64), ConfirmedAt: store.now, ControlToken: "xib_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", ControlTokenExpires: store.now.Add(24 * time.Hour)}
			body, err := json.Marshal(redeemInput)
			if err != nil {
				t.Fatal(err)
			}
			redeem := httptest.NewRequest(http.MethodPost, "https://xinsight.local/api/v1/connector-control/invitations/redeem", bytes.NewReader(body))
			redeem.Header.Set("Content-Type", "application/json")
			response = httptest.NewRecorder()
			handler.ServeHTTP(response, redeem)
			var result map[string]json.RawMessage
			if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || len(result) != 2 || result["installation"] == nil || !connectorcontrol.MatchesCanonicalDigest(result["operation"], mustControlDigest(t, fixture.expected)) {
				t.Fatalf("redeem HTTP authority status=%d body=%s", response.Code, response.Body.String())
			}
			if fixture.operation.SchemaVersion == connectorcontrol.ControlOperationSchemaV2 {
				if _, err = connectorcontrol.DecodeControlOperationRequestV2(result["operation"]); err != nil {
					t.Fatal(err)
				}
			} else if _, err = connectorcontrol.DecodeControlOperationRequestV3(result["operation"]); err != nil {
				t.Fatal(err)
			}
			// Invalid persisted request cannot be projected into a successful response.
			fixture.operation.Request, fixture.operation.RequestV3 = nil, nil
			response = httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code == http.StatusOK {
				t.Fatal("invalid authority returned HTTP200")
			}
		})
	}
}
func mustControlDigest(t *testing.T, value any) string {
	t.Helper()
	digest, _, err := connectorcontrol.CanonicalSHA256(value)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

type connectorManagedACKStub struct {
	connectorControllerStub
	calls  int
	result connectorcontrol.ControlResultV3
	fail   error
}

func (s *connectorManagedACKStub) AcknowledgeManaged(_ context.Context, _ string, result connectorcontrol.ControlResultV3) (connectorcontrol.Installation, error) {
	s.calls++
	s.result = result
	return connectorFixtureInstallation(result.ObservedAt), s.fail
}

func TestConnectorHTTPACKDispatchesUnchangedV2V3ResultGoldens(t *testing.T) {
	cases := []struct {
		path string
		v3   bool
	}{
		{"../../contracts/connector-control/v2/fixtures/valid/02-prepare-paused-source-result.json", false},
		{"../../contracts/connector-control/v2/fixtures/valid/04-register-receiver-result.json", false},
		{"../../contracts/connector-control/v2/fixtures/valid/06-resume-source-result.json", false},
		{"../connectorcontrol/testdata/control-v3/valid/02-bind-existing-result.json", true},
		{"../connectorcontrol/testdata/control-v3/valid/04-confirm-existing-result.json", true},
		{"../connectorcontrol/testdata/control-v3/valid/06-policy-pair-result.json", true},
		{"../connectorcontrol/testdata/control-v3/valid/08-source-enroll-pair-result.json", true},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			raw, e := os.ReadFile(c.path)
			if e != nil {
				t.Fatal(e)
			}
			var result connectorcontrol.ControlOperationResult
			if e = json.Unmarshal(raw, &result); e != nil {
				t.Fatal(e)
			}
			store := &connectorStoreStub{}
			controller := &connectorManagedACKStub{}
			handler, token := connectorAgentResultHandler(t, store, controller)
			store.now = result.ObservedAt.Add(time.Second)
			post := func(body []byte) *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPost, "https://xinsight.local/api/v1/connector-control/operations/"+result.OperationID, bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+token)
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, req)
				return response
			}
			response := post(raw)
			if response.Code != http.StatusOK {
				t.Fatalf("unchanged golden ACK status=%d code=%s", response.Code, response.Body.String())
			}
			if c.v3 {
				if controller.calls != 1 || store.completeCalls != 0 || controller.result.OperationID != result.OperationID || controller.result.Phase != result.Phase {
					t.Fatal("v3 managed dispatch differs")
				}
				controller.fail = connectorcontrol.ErrInvalid
				response = post(raw)
				if response.Code != http.StatusBadRequest || controller.calls != 2 {
					t.Fatal("controller invalid result not distinguished from public decode")
				}
			} else if store.completeCalls != 1 || controller.calls != 0 || store.completed.OperationID != result.OperationID {
				t.Fatal("original v2 durable complete/wake dispatch differs")
			}
			oldStoreCalls, oldManagedCalls := store.completeCalls, controller.calls
			bad := append([]byte(`{"unrecognized_authority":"synthetic",`), raw[1:]...)
			response = post(bad)
			if response.Code != http.StatusBadRequest || store.completeCalls != oldStoreCalls || controller.calls != oldManagedCalls {
				t.Fatal("public invalid body reached controller/store")
			}
		})
	}
}
