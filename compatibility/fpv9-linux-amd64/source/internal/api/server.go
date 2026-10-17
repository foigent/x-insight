package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	productaccess "github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/executionid"
	"github.com/foigent/x-insight/internal/investigation"
	"github.com/foigent/x-insight/internal/webui"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workspace"
)

type Store interface {
	Ping(context.Context) error
	ListEvidenceEventsByRequestID(context.Context, string, int) ([]db.EvidenceEventRecord, error)
	ListCaptureGapsByRequestID(context.Context, string, int) ([]db.CaptureGapRecord, error)
}

type InvestigationStore interface {
	SearchExecutions(context.Context, investigation.SearchQuery) ([]investigation.ExecutionSummary, error)
	GetExecution(context.Context, string, time.Time) (investigation.ExecutionDetail, error)
	GetTraceJourney(context.Context, string, time.Time, int) (investigation.TraceJourney, error)
	ListTimeline(context.Context, investigation.TimelineQuery) ([]investigation.TimelineNode, int, error)
	ListExecutionEvents(context.Context, string, time.Time, int) ([]investigation.EvidenceEventDetail, int, error)
	GetExecutionEvent(context.Context, string, string, time.Time) (investigation.EvidenceEventDetail, error)
	ListExecutionGaps(context.Context, investigation.GapQuery) ([]investigation.CaptureGap, int, error)
	ListSources(context.Context, int) ([]investigation.SourceOption, error)
}

type ConnectorTrustStore interface {
	ListConnectorTrust(context.Context, int) ([]investigation.ConnectorTrust, error)
}

type AuthoritativeSessionStore interface {
	ListAuthoritativeSessions(context.Context, int) ([]investigation.AuthoritativeSession, error)
	GetAuthoritativeSession(context.Context, string) (investigation.AuthoritativeSessionDetail, error)
}

type AnalysisExtensionStore interface {
	ListEvaluationFeedback(context.Context, string, time.Time, int) ([]investigation.EvaluationFeedback, error)
	ListExecutionVersionDimensions(context.Context, string, time.Time, int) ([]investigation.ExecutionVersionDimension, error)
	ListInvestigationAlerts(context.Context, time.Time, int) ([]investigation.InvestigationAlert, error)
	ListDiscoveryHypotheses(context.Context, time.Time, int) ([]investigation.DiscoveryHypothesis, error)
}

type PostureStore interface {
	GetPosture(context.Context, investigation.PostureQuery) (investigation.PostureSnapshot, error)
}

type ReadinessStore interface {
	Readiness(context.Context, []db.MigrationChecksum) (db.ReadinessStatus, error)
}

type Server struct {
	contentSource       ContentSourceResolver
	contentCapacity     chan struct{}
	scopedStore         *db.ScopedWorkchainStore
	store               Store
	workspaceStore      workspace.Store
	workbenchStore      WorkbenchStore
	agentAccessStore    workspace.AgentAccessStore
	access              *productaccess.Service
	transportSecurity   TransportSecurity
	now                 func() time.Time
	apiCapacity         chan struct{}
	rateLimiter         *tokenBucket
	serviceLimiterMu    sync.Mutex
	serviceLimiters     map[string]*tokenBucket
	serviceLimiterOrder []string
	expectedMigrations  []db.MigrationChecksum
	connectorStore      connectorcontrol.Store
	connectorIssuer     connectorcontrol.InvitationIssuer
	connectorController connectorcontrol.Controller
	connectorConfig     connectorcontrol.InvitationConfig
}

// ProductEvidenceStore is the complete read-side capability set required by
// the production product handler. The narrower constructors below remain for
// focused tests, while production wiring is checked by the compiler.
type ProductEvidenceStore interface {
	Store
	InvestigationStore
	ConnectorTrustStore
	AuthoritativeSessionStore
	AnalysisExtensionStore
	PostureStore
	ReadinessStore
}

// ProductWorkspaceStore is the complete write/control-side capability set
// required by the production product handler.
type ProductWorkspaceStore interface {
	workspace.Store
	workspace.FollowupStore
	workspace.AgentAccessStore
	connectorcontrol.Store
	WorkbenchStore
}

type WorkbenchStore interface {
	CreateWorkbenchReading(context.Context, string, workbench.ReadingRequest, time.Time) (workbench.Reading, error)
	GetWorkbenchReading(context.Context, string, string, time.Time) (workbench.Reading, error)
	GetWorkbenchSummary(context.Context, string, string, time.Time) (workbench.Summary, error)
	GetWorkbenchContributionsOrdered(context.Context, string, string, string, string, int, int, string, time.Time) (workbench.Contributions, error)
	GetWorkbenchContributions(context.Context, string, string, string, int, int, string, time.Time) (workbench.Contributions, error)
	GetWorkbenchSeries(context.Context, string, string, string, int64, time.Time) (workbench.Series, error)
	GetWorkbenchDetails(context.Context, string, string, string, int, string, time.Time) (workbench.Details, error)
	GetWorkbenchSourceStatuses(context.Context, string, string, time.Time) (workbench.SourceStatuses, error)
	CreateWorkbenchSavedFilter(context.Context, string, workbench.SavedFilterCommand, time.Time) (workbench.SavedFilter, error)
	UpdateWorkbenchSavedFilter(context.Context, string, string, workbench.SavedFilterCommand, time.Time) (workbench.SavedFilter, error)
	ListWorkbenchSavedFilters(context.Context, string) (workbench.SavedFilterList, error)
}

type WorkbenchSourceStore interface {
	GetWorkbenchSourceFacts(context.Context, string, string, int, string, time.Time) (workbench.SourceObservations, error)
}

type HandlerDependencies struct {
	ContentSource       ContentSourceResolver
	Store               Store
	WorkspaceStore      workspace.Store
	AgentAccessStore    workspace.AgentAccessStore
	ExpectedMigrations  []db.MigrationChecksum
	AccessService       *productaccess.Service
	TransportSecurity   TransportSecurity
	ConnectorConfig     connectorcontrol.InvitationConfig
	ConnectorIssuer     connectorcontrol.InvitationIssuer
	ConnectorController connectorcontrol.Controller
	Now                 func() time.Time
}

func NewHandler(store Store) http.Handler {
	return NewHandlerWithMigrations(store, nil)
}

func NewHandlerWithMigrations(store Store, expectedMigrations []db.MigrationChecksum) http.Handler {
	return NewHandlerWithDependencies(HandlerDependencies{Store: store, ExpectedMigrations: expectedMigrations})
}

func NewAuthenticatedHandler(
	store Store,
	expectedMigrations []db.MigrationChecksum,
	accessService *productaccess.Service,
) http.Handler {
	return NewHandlerWithDependencies(HandlerDependencies{
		Store: store, ExpectedMigrations: expectedMigrations, AccessService: accessService,
	})
}

func NewAuthenticatedHandlerWithTransport(
	store Store,
	expectedMigrations []db.MigrationChecksum,
	accessService *productaccess.Service,
	transportSecurity TransportSecurity,
) http.Handler {
	return NewHandlerWithDependencies(HandlerDependencies{
		Store: store, ExpectedMigrations: expectedMigrations, AccessService: accessService,
		TransportSecurity: transportSecurity,
	})
}

func NewProductHandlerWithTransport(
	store Store,
	workspaceStore workspace.Store,
	agentAccessStore workspace.AgentAccessStore,
	expectedMigrations []db.MigrationChecksum,
	accessService *productaccess.Service,
	transportSecurity TransportSecurity,
) http.Handler {
	return NewHandlerWithDependencies(HandlerDependencies{
		Store: store, WorkspaceStore: workspaceStore, AgentAccessStore: agentAccessStore,
		ExpectedMigrations: expectedMigrations, AccessService: accessService,
		TransportSecurity: transportSecurity,
	})
}

func NewProductHandlerWithConnectorControl(
	store Store,
	workspaceStore workspace.Store,
	agentAccessStore workspace.AgentAccessStore,
	expectedMigrations []db.MigrationChecksum,
	accessService *productaccess.Service,
	transportSecurity TransportSecurity,
	connectorConfig connectorcontrol.InvitationConfig,
	connectorIssuer connectorcontrol.InvitationIssuer,
	connectorController connectorcontrol.Controller,
) http.Handler {
	return NewHandlerWithDependencies(HandlerDependencies{
		Store: store, WorkspaceStore: workspaceStore, AgentAccessStore: agentAccessStore,
		ExpectedMigrations: expectedMigrations, AccessService: accessService,
		TransportSecurity: transportSecurity, ConnectorConfig: connectorConfig,
		ConnectorIssuer: connectorIssuer, ConnectorController: connectorController,
	})
}

func NewProductionHandler(
	store ProductEvidenceStore,
	workspaceStore ProductWorkspaceStore,
	expectedMigrations []db.MigrationChecksum,
	accessService *productaccess.Service,
	transportSecurity TransportSecurity,
	connectorConfig connectorcontrol.InvitationConfig,
	connectorIssuer connectorcontrol.InvitationIssuer,
	connectorController connectorcontrol.Controller,
	contentSource ...ContentSourceResolver,
) http.Handler {
	var resolver ContentSourceResolver
	if len(contentSource) > 0 {
		resolver = contentSource[0]
	}
	return NewHandlerWithDependencies(HandlerDependencies{
		Store: store, WorkspaceStore: workspaceStore, AgentAccessStore: workspaceStore, ContentSource: resolver,
		ExpectedMigrations: expectedMigrations, AccessService: accessService,
		TransportSecurity: transportSecurity, ConnectorConfig: connectorConfig,
		ConnectorIssuer: connectorIssuer, ConnectorController: connectorController,
	})
}

func NewHandlerWithDependencies(dependencies HandlerDependencies) http.Handler {
	now := dependencies.Now
	if now == nil {
		now = time.Now
	}
	connectorStore, _ := dependencies.WorkspaceStore.(connectorcontrol.Store)
	workbenchStore, _ := dependencies.WorkspaceStore.(WorkbenchStore)
	server := &Server{
		contentSource:       dependencies.ContentSource,
		contentCapacity:     make(chan struct{}, 1),
		store:               dependencies.Store,
		workspaceStore:      dependencies.WorkspaceStore,
		workbenchStore:      workbenchStore,
		agentAccessStore:    dependencies.AgentAccessStore,
		access:              dependencies.AccessService,
		transportSecurity:   dependencies.TransportSecurity,
		now:                 now,
		apiCapacity:         make(chan struct{}, 32),
		rateLimiter:         newTokenBucket(20, 40),
		serviceLimiters:     make(map[string]*tokenBucket),
		expectedMigrations:  append([]db.MigrationChecksum(nil), dependencies.ExpectedMigrations...),
		connectorStore:      connectorStore,
		connectorIssuer:     dependencies.ConnectorIssuer,
		connectorController: dependencies.ConnectorController,
		connectorConfig:     dependencies.ConnectorConfig,
	}

	if reader, ok := dependencies.Store.(*db.EvidenceStore); ok {
		if writer, ok := dependencies.WorkspaceStore.(*db.WorkspaceStore); ok {
			server.scopedStore = db.NewScopedWorkchainStore(reader, writer)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", server.handleHealth)
	mux.HandleFunc("/readyz", server.handleReady)
	mux.HandleFunc("/api/v1/access/login", server.handleAccessLogin)
	mux.HandleFunc("/api/v1/access/session", server.handleAccessSession)
	mux.HandleFunc("/api/v1/access/providers", server.handleAccessProviders)
	mux.HandleFunc("/api/v1/access/logout", server.handleAccessLogout)
	mux.HandleFunc("/api/v1/access/sessions", server.handleAccessSessions)
	mux.HandleFunc("/api/v1/access/sessions/", server.handleAccessSessionPath)
	mux.HandleFunc("/api/v1/posture", server.handlePosture)
	mux.HandleFunc("/api/v1/executions", server.handleExecutionCollection)
	mux.HandleFunc("/api/v1/executions/", server.handleExecutionPath)
	mux.HandleFunc("/api/v1/sources", server.handleSourceCollection)
	mux.HandleFunc("/api/v1/connectors", server.handleConnectorCollection)
	mux.HandleFunc("/api/v1/connectors/invitations", server.handleConnectorInvitationCollection)
	mux.HandleFunc("/api/v1/connector-control/invitations/redeem", server.handleConnectorInvitationRedeem)
	mux.HandleFunc("/api/v1/connector-control/operations", server.handleConnectorControlOperations)
	mux.HandleFunc("/api/v1/connector-control/operations/", server.handleConnectorControlOperationPath)
	mux.HandleFunc("/api/v1/sessions", server.handleSessionCollection)
	mux.HandleFunc("/api/v1/sessions/", server.handleSessionPath)
	mux.HandleFunc("/api/v1/compare", server.handleExecutionCompare)
	mux.HandleFunc("/api/v1/alerts", server.handleAlertCollection)
	mux.HandleFunc("/api/v1/alerts/", server.handleAlertPath)
	mux.HandleFunc("/api/v1/discovery/hypotheses", server.handleHypothesisCollection)
	mux.HandleFunc("/api/v1/investigation/queue", server.handleInvestigationQueue)
	mux.HandleFunc("/api/v1/investigation/queue-filters", server.handleQueueFilters)
	mux.HandleFunc("/api/v1/investigation/queue-filters/", server.handleQueueFilters)
	mux.HandleFunc("/api/v1/cases", server.handleCaseCollection)
	mux.HandleFunc("/api/v1/cases/", server.handleCasePath)
	mux.HandleFunc("/api/v1/security/assignee-candidates", server.handleAssigneeCandidates)
	mux.HandleFunc("/api/v1/security/reviews", server.handleReviewCreation)
	mux.HandleFunc("/api/v1/security/reviews/preview", server.handleReviewCreation)
	mux.HandleFunc("/api/v1/saved-views", server.handleSavedViewCollection)
	mux.HandleFunc("/api/v1/workbench/executions/", server.handleWorkbenchPurpose)
	mux.HandleFunc("/api/v1/workbench/readings", server.handleWorkbenchReadings)
	mux.HandleFunc("/api/v1/workbench/readings/", server.handleWorkbenchReadingPath)
	mux.HandleFunc("/api/v1/runtime/readings", server.handleRuntimeReadings)
	mux.HandleFunc("/api/v1/workbench/budgets/readings", server.handleBudgetReadings)
	mux.HandleFunc("/api/v1/workbench/budgets/readings/", server.handleBudgetReadingPath)
	mux.HandleFunc("/api/v1/runtime/current", server.handleRuntimeCurrent)
	mux.HandleFunc("/api/v1/runtime/readings/", server.handleRuntimeReadingPath)
	mux.HandleFunc("/api/v1/workbench/saved-filters", server.handleWorkbenchSavedFilters)
	mux.HandleFunc("/api/v1/workbench/saved-filters/", server.handleWorkbenchSavedFilterPath)
	mux.HandleFunc("/api/v1/service-identities", server.handleServiceIdentityCollection)
	mux.HandleFunc("/api/v1/service-identities/", server.handleServiceIdentityPath)
	mux.HandleFunc("/api/v1/gaps/", server.handleGapPath)
	mux.HandleFunc("/api/", server.handleAPINotFound)
	mux.Handle("/", webui.Handler())
	crossOrigin := http.NewCrossOriginProtection()
	crossOrigin.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, http.StatusForbidden, "same_origin_required", "Same-origin HTTPS is required", false)
	}))
	handler := server.withAccessEnforcement(mux)
	handler = crossOrigin.Handler(handler)
	handler = server.withAPILimits(handler)
	return server.withSecurityHeaders(handler)
}

func (s *Server) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeProblem(w, http.StatusNotFound, "route_not_found", "Route not found", false)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if s.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not_ready",
			"db":     "not_configured",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not_ready",
			"db":     "unavailable",
		})
		return
	}
	if s.workspaceStore != nil {
		workspacePinger, ok := s.workspaceStore.(interface {
			Ping(context.Context) error
		})
		if !ok || workspacePinger.Ping(ctx) != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status":       "not_ready",
				"db":           "ok",
				"workspace_db": "unavailable",
			})
			return
		}
	}
	response := map[string]any{
		"status":          "ready",
		"db":              "ok",
		"product_version": buildinfo.ProductVersion,
		"config_schema":   buildinfo.ConfigSchemaVersion,
		"manual_version":  buildinfo.ManualVersion,
	}
	if s.workspaceStore != nil {
		response["workspace_db"] = "ok"
	}
	if len(s.expectedMigrations) > 0 {
		readinessStore, ok := s.store.(ReadinessStore)
		if !ok {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "not_ready",
				"db":     "capability_missing",
			})
			return
		}
		readiness, err := readinessStore.Readiness(ctx, s.expectedMigrations)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "not_ready",
				"db":     "incompatible",
			})
			return
		}
		if readiness.DatabaseSchema != buildinfo.DatabaseSchemaMax ||
			!supportedPostgresMajor(readiness.PostgresMajor) ||
			!readiness.RepresentativeRead {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "not_ready",
				"db":     "unsupported",
			})
			return
		}
		response["database_schema"] = readiness.DatabaseSchema
		response["migration_set_sha256"] = readiness.MigrationSetSHA256
		response["postgres_major"] = readiness.PostgresMajor
		response["representative_read"] = "ok"
	}
	writeJSON(w, http.StatusOK, response)
}

func supportedPostgresMajor(actual int) bool {
	minimum, minimumErr := strconv.Atoi(buildinfo.PostgresMajorMin)
	maximum, maximumErr := strconv.Atoi(buildinfo.PostgresMajorMax)
	return minimumErr == nil && maximumErr == nil && actual >= minimum && actual <= maximum
}

func (s *Server) handleExecutionPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	suffix := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/executions/"), "/")
	if suffix == "" {
		writeProblem(w, http.StatusNotFound, "route_not_found", "Route not found", false)
		return
	}
	parts := strings.Split(suffix, "/")
	if executionid.Valid(parts[0]) {
		s.handleInvestigationExecutionPath(w, r, parts)
		return
	}
	if strings.HasPrefix(parts[0], executionid.Prefix) {
		writeProblem(w, http.StatusBadRequest, "invalid_argument", "Invalid request argument", false,
			ProblemField{Name: "execution_id", Reason: "must_match_xe1_format"})
		return
	}
	if failure := validateQueryKeys(r, "limit"); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	if s.store == nil {
		writeProblem(w, http.StatusServiceUnavailable, "database_unavailable", "Database is unavailable", true)
		return
	}
	requestID := parts[0]
	if executionid.ValidateRequestID(requestID) != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_argument", "Invalid request argument", false,
			ProblemField{Name: "request_id", Reason: "invalid_utf8_or_length"})
		return
	}

	limit, err := parseLimit(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_argument", "Invalid request argument", false,
			ProblemField{Name: "limit", Reason: "must_be_an_integer_between_1_and_1000"})
		return
	}

	switch {
	case len(parts) == 1:
		s.handleExecution(w, r, requestID, limit)
	case len(parts) == 2 && parts[1] == "events":
		s.handleExecutionEvents(w, r, requestID, limit)
	case len(parts) == 2 && parts[1] == "gaps":
		s.handleExecutionGaps(w, r, requestID, limit)
	default:
		writeProblem(w, http.StatusNotFound, "route_not_found", "Route not found", false)
	}
}

func (s *Server) handleExecution(w http.ResponseWriter, r *http.Request, requestID string, limit int) {
	events, err := s.store.ListEvidenceEventsByRequestID(r.Context(), requestID, limit)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Internal server error", false)
		return
	}
	gaps, err := s.store.ListCaptureGapsByRequestID(r.Context(), requestID, limit)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Internal server error", false)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"request_id":  requestID,
		"event_count": len(events),
		"gap_count":   len(gaps),
		"events":      events,
		"gaps":        gaps,
	})
}

func (s *Server) handleExecutionEvents(w http.ResponseWriter, r *http.Request, requestID string, limit int) {
	events, err := s.store.ListEvidenceEventsByRequestID(r.Context(), requestID, limit)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Internal server error", false)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"request_id":  requestID,
		"event_count": len(events),
		"events":      events,
	})
}

func (s *Server) handleExecutionGaps(w http.ResponseWriter, r *http.Request, requestID string, limit int) {
	gaps, err := s.store.ListCaptureGapsByRequestID(r.Context(), requestID, limit)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Internal server error", false)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"request_id": requestID,
		"gap_count":  len(gaps),
		"gaps":       gaps,
	})
}

func parseLimit(r *http.Request) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 200, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("limit must be an integer")
	}
	if limit <= 0 {
		return 0, fmt.Errorf("limit must be greater than zero")
	}
	if limit > 1000 {
		return 1000, nil
	}
	return limit, nil
}

func methodNotAllowed(w http.ResponseWriter) {
	methodNotAllowedFor(w, http.MethodGet)
}

func methodNotAllowedFor(w http.ResponseWriter, allowed ...string) {
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	writeProblem(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method is not allowed", false)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("content-type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = encodeJSON(w, value)
}

func encodeJSON(w http.ResponseWriter, value any) error {
	return json.NewEncoder(w).Encode(value)
}

func (s *Server) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", webui.ContentSecurityPolicy)
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		if s.transportSecurity.isHTTPS(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}
