package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/db"
)

type fakeStore struct {
	events   []db.EvidenceEventRecord
	gaps     []db.CaptureGapRecord
	eventErr error
	gapErr   error
}

type fakeReadinessStore struct {
	*fakeStore
	readiness db.ReadinessStatus
	err       error
}

func (s *fakeReadinessStore) Readiness(context.Context, []db.MigrationChecksum) (db.ReadinessStatus, error) {
	return s.readiness, s.err
}

func (s *fakeStore) Ping(context.Context) error {
	return nil
}

func TestHealthReadinessAndUnknownRouteSemantics(t *testing.T) {
	handler := api.NewHandler(nil)
	authenticatedHandler := newAuthenticatedHandler(t, nil)
	tests := []struct {
		method       string
		path         string
		wantStatus   int
		wantFragment string
	}{
		{method: http.MethodGet, path: "/healthz", wantStatus: http.StatusOK, wantFragment: `"status":"ok"`},
		{method: http.MethodPost, path: "/healthz", wantStatus: http.StatusMethodNotAllowed},
		{method: http.MethodGet, path: "/readyz", wantStatus: http.StatusServiceUnavailable, wantFragment: `"db":"not_configured"`},
		{method: http.MethodPost, path: "/readyz", wantStatus: http.StatusMethodNotAllowed},
		{method: http.MethodGet, path: "/api/v1/not-a-route", wantStatus: http.StatusNotFound, wantFragment: `"code":"route_not_found"`},
		{method: http.MethodPost, path: "/api/v1/not-a-route", wantStatus: http.StatusMethodNotAllowed},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, test.path, nil)
			requestHandler := handler
			if strings.HasPrefix(test.path, "/api/") {
				requestHandler = authenticatedHandler
			}
			requestHandler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if test.wantFragment != "" && !strings.Contains(response.Body.String(), test.wantFragment) {
				t.Fatalf("body=%s does not contain %s", response.Body.String(), test.wantFragment)
			}
		})
	}
}

func TestProductionHandlerConstructorsAcceptExactDatabaseCapabilities(t *testing.T) {
	var evidenceStore *db.EvidenceStore
	var workspaceStore *db.WorkspaceStore

	if handler := api.NewProductionHandler(evidenceStore, workspaceStore, nil, nil, api.TransportSecurity{}, connectorcontrol.InvitationConfig{}, nil, nil); handler == nil {
		t.Fatal("production handler is nil")
	}
	if handler := api.NewProductHandlerWithConnectorControl(nil, nil, nil, nil, nil, api.TransportSecurity{}, connectorcontrol.InvitationConfig{}, nil, nil); handler == nil {
		t.Fatal("connector-control product handler is nil")
	}
}

func TestReadOnlyCollectionsRejectMutationBeforeStoreAccess(t *testing.T) {
	handler := newAuthenticatedHandler(t, nil)
	for _, path := range []string{
		"/api/v1/connectors",
		"/api/v1/sessions",
		"/api/v1/alerts",
		"/api/v1/discovery/hypotheses",
	} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, path, nil)
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if response.Header().Get("Allow") != http.MethodGet {
				t.Fatalf("allow=%q", response.Header().Get("Allow"))
			}
		})
	}
}

func TestReadinessReportsReleaseCompatibilityAndRejectsUnsupportedDatabase(t *testing.T) {
	expected := []db.MigrationChecksum{{ID: "000019_current", SHA256: "sha256:" + strings.Repeat("a", 64)}}
	compatible := &fakeReadinessStore{
		fakeStore: &fakeStore{},
		readiness: db.ReadinessStatus{
			DatabaseSchema:     buildinfo.DatabaseSchemaMax,
			MigrationCount:     19,
			MigrationSetSHA256: "sha256:" + strings.Repeat("b", 64),
			PostgresMajor:      16,
			RepresentativeRead: true,
		},
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	api.NewHandlerWithMigrations(compatible, expected).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var readyBody map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &readyBody); err != nil {
		t.Fatalf("decode readiness: %v", err)
	}
	if readyBody["database_schema"] != buildinfo.DatabaseSchemaMax ||
		readyBody["postgres_major"] != float64(16) ||
		readyBody["migration_set_sha256"] == "" ||
		readyBody["representative_read"] != "ok" {
		t.Fatalf("readiness body=%+v", readyBody)
	}

	compatible.readiness.PostgresMajor = 18
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	api.NewHandlerWithMigrations(compatible, expected).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"db":"unsupported"`) {
		t.Fatalf("unsupported status=%d body=%s", response.Code, response.Body.String())
	}

	compatible.readiness.PostgresMajor = 16
	compatible.err = errors.New("representative reader grant is incomplete")
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	api.NewHandlerWithMigrations(compatible, expected).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable ||
		!strings.Contains(response.Body.String(), `"status":"not_ready"`) ||
		!strings.Contains(response.Body.String(), `"db":"incompatible"`) {
		t.Fatalf("reader grant drift status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestReadinessFailsClosedWhenMigrationCapabilityIsMissing(t *testing.T) {
	expected := []db.MigrationChecksum{{ID: "000019_current", SHA256: "sha256:" + strings.Repeat("a", 64)}}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	api.NewHandlerWithMigrations(&fakeStore{}, expected).ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable ||
		!strings.Contains(response.Body.String(), `"status":"not_ready"`) ||
		!strings.Contains(response.Body.String(), `"db":"capability_missing"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func (s *fakeStore) ListEvidenceEventsByRequestID(_ context.Context, requestID string, limit int) ([]db.EvidenceEventRecord, error) {
	if s.eventErr != nil {
		return nil, s.eventErr
	}
	var records []db.EvidenceEventRecord
	for _, event := range s.events {
		if event.RequestID == requestID {
			records = append(records, event)
		}
	}
	if len(records) > limit {
		return records[:limit], nil
	}
	return records, nil
}

func (s *fakeStore) ListCaptureGapsByRequestID(_ context.Context, requestID string, limit int) ([]db.CaptureGapRecord, error) {
	if s.gapErr != nil {
		return nil, s.gapErr
	}
	var records []db.CaptureGapRecord
	for _, gap := range s.gaps {
		if gap.RequestID == requestID {
			records = append(records, gap)
		}
	}
	if len(records) > limit {
		return records[:limit], nil
	}
	return records, nil
}

func TestExecutionEndpoints(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	handler := newAuthenticatedHandler(t, &fakeStore{
		events: []db.EvidenceEventRecord{
			{
				ID:                 "event-1",
				SchemaVersion:      "0.1.0",
				SourceSystemID:     "11111111-1111-1111-1111-111111111111",
				AdapterManifestID:  "22222222-2222-2222-2222-222222222222",
				SourceType:         "unirouter",
				SourceRef:          "snapshot:request:req-123",
				EventType:          "llm.request.completed",
				Stage:              "gateway",
				Severity:           "info",
				CaptureMode:        "snapshot_import",
				IngestedAt:         now,
				RequestID:          "req-123",
				PayloadCaptureMode: "metadata_only",
				EventHash:          "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				CreatedAt:          now,
			},
		},
		gaps: []db.CaptureGapRecord{
			{
				ID:                "gap-1",
				SourceSystemID:    "11111111-1111-1111-1111-111111111111",
				AdapterManifestID: "22222222-2222-2222-2222-222222222222",
				RequestID:         "req-123",
				GapType:           "not_available_from_source",
				CandidateGapTypes: []string{"sampled_out", "retention_expired"},
				AffectedClaim:     "request_trace_events",
				GapReason:         "trace row missing",
				Severity:          "warning",
				CreatedAt:         now,
			},
		},
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/executions/req-123", nil)
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["request_id"] != "req-123" || body["event_count"].(float64) != 1 || body["gap_count"].(float64) != 1 {
		t.Fatalf("unexpected response: %#v", body)
	}
}

func TestExecutionGapsEndpointReturnsCandidateGapTypes(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	handler := newAuthenticatedHandler(t, &fakeStore{
		gaps: []db.CaptureGapRecord{
			{
				ID:                "gap-1",
				SourceSystemID:    "11111111-1111-1111-1111-111111111111",
				AdapterManifestID: "22222222-2222-2222-2222-222222222222",
				RequestID:         "req-123",
				GapType:           "not_available_from_source",
				CandidateGapTypes: []string{"sampled_out", "retention_expired"},
				AffectedClaim:     "request_trace_events",
				GapReason:         "trace row missing",
				Severity:          "warning",
				CreatedAt:         now,
			},
		},
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/executions/req-123/gaps", nil)
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		GapCount int                   `json:"gap_count"`
		Gaps     []db.CaptureGapRecord `json:"gaps"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.GapCount != 1 || len(body.Gaps) != 1 || len(body.Gaps[0].CandidateGapTypes) != 2 {
		t.Fatalf("unexpected response: %+v", body)
	}
}

func TestExecutionEventsEndpointCapsLimit(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	handler := newAuthenticatedHandler(t, &fakeStore{
		events: []db.EvidenceEventRecord{
			{ID: "event-1", RequestID: "req-123", IngestedAt: now, CreatedAt: now},
			{ID: "event-2", RequestID: "req-123", IngestedAt: now, CreatedAt: now},
		},
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/executions/req-123/events?limit=1", nil)
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		EventCount int                      `json:"event_count"`
		Events     []db.EvidenceEventRecord `json:"events"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.EventCount != 1 || len(body.Events) != 1 || body.Events[0].ID != "event-1" {
		t.Fatalf("unexpected response: %+v", body)
	}
}

func TestLegacyExecutionEndpointRejectsAmbiguousQueryAndInvalidRequestID(t *testing.T) {
	handler := newAuthenticatedHandler(t, &fakeStore{})
	for _, target := range []string{
		"/api/v1/executions/req-123/events?unknown=true",
		"/api/v1/executions/req-123/events?limit=1&limit=2",
		"/api/v1/executions/%20req-123/events",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("target=%q status=%d body=%s", target, response.Code, response.Body.String())
		}
	}
}

func TestReadEndpointsRequireConfiguredStore(t *testing.T) {
	handler := newAuthenticatedHandler(t, nil)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/executions/req-123/events", nil)
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestHandlerServesEmbeddedProductShellWithoutInterceptingAPI(t *testing.T) {
	handler := api.NewHandler(nil)

	productResponse := httptest.NewRecorder()
	productRequest := httptest.NewRequest(http.MethodGet, "/executions", nil)
	handler.ServeHTTP(productResponse, productRequest)
	if productResponse.Code != http.StatusSeeOther || productResponse.Header().Get("Location") != "/login" {
		t.Fatalf("product status=%d body=%s", productResponse.Code, productResponse.Body.String())
	}

	apiResponse := httptest.NewRecorder()
	apiRequest := httptest.NewRequest(http.MethodGet, "/api/v1/executions", nil)
	handler.ServeHTTP(apiResponse, apiRequest)
	if apiResponse.Code != http.StatusUnauthorized {
		t.Fatalf("api status=%d body=%s", apiResponse.Code, apiResponse.Body.String())
	}

	authenticated := newAuthenticatedHandler(t, nil)
	productResponse = httptest.NewRecorder()
	authenticated.ServeHTTP(productResponse, httptest.NewRequest(http.MethodGet, "/executions", nil))
	if productResponse.Code != http.StatusOK || productResponse.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("authenticated product status=%d body=%s", productResponse.Code, productResponse.Body.String())
	}
}

func TestAPIErrorsUseStableProblemAndDeclareAllowedMethod(t *testing.T) {
	handler := newAuthenticatedHandler(t, nil)

	for _, testCase := range []struct {
		method string
		path   string
		status int
		code   string
	}{
		{http.MethodGet, "/api/v1/unknown", http.StatusNotFound, "route_not_found"},
		{http.MethodPost, "/api/v1/executions", http.StatusMethodNotAllowed, "method_not_allowed"},
		{http.MethodPost, "/api/v1/sources", http.StatusMethodNotAllowed, "method_not_allowed"},
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(testCase.method, testCase.path, nil)
		handler.ServeHTTP(response, request)

		if response.Code != testCase.status {
			t.Fatalf("%s %s status=%d body=%s", testCase.method, testCase.path, response.Code, response.Body.String())
		}
		if response.Header().Get("Content-Type") != "application/problem+json" {
			t.Fatalf("%s %s content-type=%q", testCase.method, testCase.path, response.Header().Get("Content-Type"))
		}
		var problem api.Problem
		if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
			t.Fatalf("decode %s %s: %v", testCase.method, testCase.path, err)
		}
		if problem.Code != testCase.code {
			t.Fatalf("%s %s problem=%+v", testCase.method, testCase.path, problem)
		}
		if testCase.status == http.StatusMethodNotAllowed && response.Header().Get("Allow") != http.MethodGet {
			t.Fatalf("%s %s allow=%q", testCase.method, testCase.path, response.Header().Get("Allow"))
		}
	}
}

func TestReadFailureUsesStableProblemWithoutLeakingInternalError(t *testing.T) {
	handler := newAuthenticatedHandler(t, &fakeStore{eventErr: errors.New("internal table evidence_events at host=secret.invalid")})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/executions/req-123/events", nil)
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("content-type=%q", response.Header().Get("Content-Type"))
	}
	if response.Header().Get("X-Correlation-ID") == "" {
		t.Fatal("missing correlation ID")
	}
	if body := response.Body.String(); strings.Contains(body, "secret.invalid") || strings.Contains(body, "evidence_events") {
		t.Fatalf("internal error leaked: %s", body)
	}
	var problem api.Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != "internal_error" || problem.Retryable || problem.Status != http.StatusInternalServerError {
		t.Fatalf("problem=%+v", problem)
	}
}
