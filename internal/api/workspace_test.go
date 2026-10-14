package api_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/workspace"
)

func TestWorkspaceMutationsRequireHumanCSRFAndRejectNestedDuplicateKeys(t *testing.T) {
	store := &workspaceStoreStub{}
	handler, cookie, csrf := productTestHandler(
		t, &fakeStore{}, store, nil, "investigator.one", "test password",
	)

	valid := productRequest(
		http.MethodPost,
		"/api/v1/cases",
		`{"title":"Fallback review","description":"Evidence only"}`,
		cookie,
		csrf,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, valid)
	if response.Code != http.StatusCreated ||
		store.createdInput.Title != "Fallback review" ||
		store.createdActor.Kind != "human" {
		t.Fatalf("create status=%d body=%s call=%+v", response.Code, response.Body.String(), store.createdInput)
	}

	noCSRF := productRequest(
		http.MethodPost,
		"/api/v1/cases",
		`{"title":"Rejected","description":""}`,
		cookie,
		"",
	)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, noCSRF)
	if response.Code != http.StatusForbidden ||
		!strings.Contains(response.Body.String(), `"code":"csrf_validation_failed"`) {
		t.Fatalf("missing CSRF status=%d body=%s", response.Code, response.Body.String())
	}

	duplicate := productRequest(
		http.MethodPost,
		"/api/v1/saved-views",
		`{"name":"Failures","filters":{"outcome":"failure","outcome":"success"},"presentation":"explorer"}`,
		cookie,
		csrf,
	)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, duplicate)
	if response.Code != http.StatusBadRequest ||
		!strings.Contains(response.Body.String(), `"code":"invalid_workspace_request"`) ||
		store.savedViewCalls != 0 {
		t.Fatalf("duplicate JSON status=%d body=%s saved-calls=%d", response.Code, response.Body.String(), store.savedViewCalls)
	}
}

func TestSealedEvidencePackJSONDownloadPreservesExactDigestBytes(t *testing.T) {
	sealedAt := time.Date(2026, 7, 31, 4, 0, 0, 123000000, time.UTC)
	record := workspace.CaseRecord{
		Case: workspace.Case{
			ID: "xc_AAAAAAAAAAAAAAAAAAAAAA", Title: "Sealed review", Description: "No raw source",
		},
	}
	_, payload, digest, err := workspace.BuildEvidencePack(
		record, sealedAt, "xs_0123456789abcdef", 2,
	)
	if err != nil {
		t.Fatal(err)
	}
	record.Case.Status = "sealed"
	record.Case.SealedAt = &sealedAt
	record.Case.SealedPack = append(json.RawMessage(nil), payload...)
	record.Case.SealedDigest = digest
	revision := int64(2)
	record.Case.SealedRevision = &revision
	store := &workspaceStoreStub{record: record}
	handler, cookie, _ := productTestHandler(
		t, &fakeStore{}, store, nil, "investigator.one", "test password",
	)
	request := httptest.NewRequest(
		http.MethodGet,
		"https://xinsight.local/api/v1/cases/xc_AAAAAAAAAAAAAAAAAAAAAA/evidence-pack.json",
		nil,
	)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), payload) {
		t.Fatalf("pack status=%d exact=%t body=%s", response.Code, bytes.Equal(response.Body.Bytes(), payload), response.Body.String())
	}
	actual := sha256.Sum256(response.Body.Bytes())
	if "sha256:"+hex.EncodeToString(actual[:]) != digest {
		t.Fatalf("download digest=%s want=%s", hex.EncodeToString(actual[:]), digest)
	}
	if response.Header().Get("Cache-Control") != "private, no-store" ||
		!strings.Contains(response.Header().Get("Content-Disposition"), "evidence-pack.json") {
		t.Fatalf("unsafe download headers=%v", response.Header())
	}
	for _, language := range []string{"", "en-US", "zh-CN", "unsupported"} {
		want := "en-US"
		if language == "zh-CN" {
			want = language
		}
		target := "/api/v1/cases/xc_AAAAAAAAAAAAAAAAAAAAAA/evidence-pack.html?lang=" + language
		r := productRequest(http.MethodGet, target, "", cookie, "")
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		if out.Code != http.StatusOK || out.Header().Get("Content-Language") != want ||
			!strings.Contains(out.Body.String(), `<html lang="`+want+`">`) || !strings.Contains(out.Body.String(), digest) {
			t.Fatalf("HTML locale %s status=%d headers=%v", language, out.Code, out.Header())
		}
		anonymous := httptest.NewRecorder()
		handler.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "https://xinsight.local"+target, nil))
		if anonymous.Code != http.StatusUnauthorized {
			t.Fatalf("locale bypassed authentication: %d", anonymous.Code)
		}
		if !bytes.Equal(store.record.Case.SealedPack, payload) || store.record.Case.SealedDigest != digest {
			t.Fatal("presentation language changed sealed evidence")
		}
	}
}

func TestAgentBearerReadsTheSameEvidenceContractButCannotMutateOrMixCookie(t *testing.T) {
	fixture := newInvestigationFixture(t)
	agent := &agentAccessStoreStub{
		token: "agent-token",
		authentication: workspace.ServiceAuthentication{
			Identity: workspace.ServiceIdentity{
				ID: "xsi_0123456789abcdefghijkl", DisplayName: "review-agent", Status: "active",
				Permissions: []string{"evidence.read", "evidence.cite"},
			},
			TokenDigest: "sha256:" + strings.Repeat("a", 64),
		},
	}
	handler, cookie, _ := productTestHandler(
		t, fixture, nil, agent, "investigator.one", "test password",
	)

	request := httptest.NewRequest(http.MethodGet, "https://xinsight.local/api/v1/executions?limit=1", nil)
	request.Header.Set("Authorization", "Bearer "+agent.token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"execution_id"`) ||
		strings.Contains(response.Body.String(), agent.token) {
		t.Fatalf("Agent read status=%d body=%s", response.Code, response.Body.String())
	}

	mixed := httptest.NewRequest(http.MethodGet, "https://xinsight.local/api/v1/executions?limit=1", nil)
	mixed.Header.Set("Authorization", "Bearer "+agent.token)
	mixed.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, mixed)
	if response.Code != http.StatusUnauthorized ||
		!strings.Contains(response.Body.String(), `"code":"agent_authentication_failed"`) {
		t.Fatalf("mixed credential status=%d body=%s", response.Code, response.Body.String())
	}

	mutation := httptest.NewRequest(
		http.MethodPost,
		"https://xinsight.local/api/v1/cases",
		strings.NewReader(`{"title":"forbidden","description":""}`),
	)
	mutation.Header.Set("Authorization", "Bearer "+agent.token)
	mutation.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, mutation)
	if response.Code != http.StatusForbidden ||
		!strings.Contains(response.Body.String(), `"code":"agent_scope_denied"`) {
		t.Fatalf("Agent mutation status=%d body=%s", response.Code, response.Body.String())
	}

	agent.authenticationErr = context.DeadlineExceeded
	unavailable := httptest.NewRequest(http.MethodGet, "https://xinsight.local/api/v1/executions?limit=1", nil)
	unavailable.Header.Set("Authorization", "Bearer "+agent.token)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, unavailable)
	if response.Code != http.StatusServiceUnavailable ||
		!strings.Contains(response.Body.String(), `"code":"agent_access_unavailable"`) {
		t.Fatalf("Agent store outage status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestProviderCapabilitiesArePublicAndWorkspaceReadinessFailsClosed(t *testing.T) {
	store := &workspaceStoreStub{pingErr: workspace.ErrConflict}
	handler, _, _ := productTestHandler(
		t, &fakeStore{}, store, nil, "investigator.one", "test password",
	)

	response := httptest.NewRecorder()
	handler.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "https://xinsight.local/api/v1/access/providers", nil),
	)
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"oidc_runtime_state":"not_configured"`) ||
		!strings.Contains(response.Body.String(), `"oidc_contract":"xinsight.oidc-provider.v1"`) {
		t.Fatalf("provider capabilities status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "https://xinsight.local/readyz", nil),
	)
	if response.Code != http.StatusServiceUnavailable ||
		!strings.Contains(response.Body.String(), `"workspace_db":"unavailable"`) {
		t.Fatalf("workspace readiness status=%d body=%s", response.Code, response.Body.String())
	}
}

func productTestHandler(
	t *testing.T,
	store api.Store,
	workspaceStore workspace.Store,
	agentStore workspace.AgentAccessStore,
	username string,
	password string,
) (http.Handler, *http.Cookie, string) {
	t.Helper()
	service := newAPITestAccessService(t, access.ServiceOptions{})
	token, authentication, err := service.Login(username, []byte(password), "")
	if err != nil {
		t.Fatal(err)
	}
	handler := api.NewProductHandlerWithTransport(
		store, workspaceStore, agentStore, nil, service, api.TransportSecurity{},
	)
	return handler, &http.Cookie{Name: access.SessionCookieName, Value: token}, service.CSRFToken(authentication)
}

func productRequest(
	method string,
	path string,
	body string,
	cookie *http.Cookie,
	csrf string,
) *http.Request {
	request := httptest.NewRequest(method, "https://xinsight.local"+path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://xinsight.local")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	request.AddCookie(cookie)
	return request
}

type workspaceStoreStub struct {
	record         workspace.CaseRecord
	pingErr        error
	createdActor   workspace.Actor
	createdInput   workspace.CreateCaseInput
	savedViewCalls int
}

func (store *workspaceStoreStub) Ping(context.Context) error { return store.pingErr }

func (*workspaceStoreStub) ListCases(context.Context, int) ([]workspace.Case, error) {
	return []workspace.Case{}, nil
}

func (store *workspaceStoreStub) GetCase(context.Context, string) (workspace.CaseRecord, error) {
	if store.record.Case.ID == "" {
		return workspace.CaseRecord{}, workspace.ErrNotFound
	}
	return store.record, nil
}

func (store *workspaceStoreStub) CreateCase(
	_ context.Context,
	actor workspace.Actor,
	input workspace.CreateCaseInput,
	now time.Time,
	_ io.Reader,
) (workspace.Case, error) {
	store.createdActor = actor
	store.createdInput = input
	return workspace.Case{
		ID: "xc_AAAAAAAAAAAAAAAAAAAAAA", Title: input.Title, Description: input.Description,
		Status: "open", CreatedBySubject: actor.Ref, CreatedAt: now, UpdatedAt: now, Version: 1,
	}, nil
}

func (*workspaceStoreStub) AddCaseExecution(
	context.Context, string, workspace.Actor, workspace.AddExecutionInput, time.Time,
) (workspace.CaseExecution, error) {
	return workspace.CaseExecution{}, nil
}

func (*workspaceStoreStub) CreateFinding(
	context.Context, string, workspace.Actor, workspace.CreateFindingInput, time.Time, io.Reader,
) (workspace.Finding, error) {
	return workspace.Finding{}, nil
}

func (*workspaceStoreStub) AddEvidenceLink(
	context.Context, string, workspace.Actor, workspace.AddEvidenceInput, time.Time,
) (workspace.EvidenceLink, error) {
	return workspace.EvidenceLink{}, nil
}

func (*workspaceStoreStub) AddGapLink(
	context.Context, string, workspace.Actor, workspace.AddGapInput, time.Time,
) (workspace.GapLink, error) {
	return workspace.GapLink{}, nil
}

func (*workspaceStoreStub) CreateAnnotation(
	context.Context, string, workspace.Actor, workspace.CreateAnnotationInput, time.Time, io.Reader,
) (workspace.Annotation, error) {
	return workspace.Annotation{}, nil
}

func (*workspaceStoreStub) SealCase(
	context.Context, string, workspace.Actor, time.Time,
) (workspace.Case, error) {
	return workspace.Case{}, nil
}

func (*workspaceStoreStub) ListSavedViews(
	context.Context, string, int,
) ([]workspace.SavedView, error) {
	return []workspace.SavedView{}, nil
}

func (store *workspaceStoreStub) CreateSavedView(
	context.Context, workspace.Actor, workspace.CreateSavedViewInput, time.Time, io.Reader,
) (workspace.SavedView, error) {
	store.savedViewCalls++
	return workspace.SavedView{}, nil
}

func (*workspaceStoreStub) ResolveGap(
	context.Context, workspace.Actor, string, workspace.ResolveGapInput, time.Time,
) (workspace.GapResolution, error) {
	return workspace.GapResolution{}, nil
}

func (*workspaceStoreStub) LinkAlertToCase(
	context.Context, workspace.Actor, string, string, time.Time,
) (workspace.AlertCaseLink, error) {
	return workspace.AlertCaseLink{}, nil
}

type agentAccessStoreStub struct {
	token             string
	authentication    workspace.ServiceAuthentication
	authenticationErr error
}

func (*agentAccessStoreStub) CreateServiceIdentity(
	context.Context,
	workspace.Actor,
	workspace.CreateServiceIdentityInput,
	time.Time,
	io.Reader,
) (workspace.ServiceCredential, error) {
	return workspace.ServiceCredential{}, workspace.ErrInvalid
}

func (store *agentAccessStoreStub) AuthenticateServiceToken(
	_ context.Context,
	token string,
	_ time.Time,
) (workspace.ServiceAuthentication, error) {
	if store.authenticationErr != nil {
		return workspace.ServiceAuthentication{}, store.authenticationErr
	}
	if token != store.token {
		return workspace.ServiceAuthentication{}, workspace.ErrCredentialInvalid
	}
	return store.authentication, nil
}

func (*agentAccessStoreStub) ListServiceIdentities(
	context.Context,
	int,
) ([]workspace.ServiceIdentity, error) {
	return []workspace.ServiceIdentity{}, nil
}

func (*agentAccessStoreStub) RevokeServiceIdentity(
	context.Context,
	string,
	workspace.Actor,
	string,
	time.Time,
) (workspace.ServiceIdentity, error) {
	return workspace.ServiceIdentity{}, workspace.ErrNotFound
}
