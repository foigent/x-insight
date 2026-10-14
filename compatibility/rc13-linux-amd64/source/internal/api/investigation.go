package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/executionid"
	"github.com/foigent/x-insight/internal/investigation"
	"github.com/foigent/x-insight/internal/pagination"
)

const (
	maxRequestURIBytes = 8 << 10
	apiQueryTimeout    = 3 * time.Second
)

var uuidPattern = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)
var authoritativeSessionIDPattern = regexp.MustCompile(`^xss_[a-f0-9]{64}$`)

type page struct {
	AsOf       time.Time `json:"as_of"`
	Count      int       `json:"count"`
	HasMore    bool      `json:"has_more"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

type executionSearchResponse struct {
	Items []investigation.ExecutionSummary `json:"items"`
	Page  page                             `json:"page"`
}

type timelineResponse struct {
	ExecutionID     string                       `json:"execution_id"`
	TotalEventCount int                          `json:"total_event_count"`
	Items           []investigation.TimelineNode `json:"items"`
	Page            page                         `json:"page"`
	Freshness       investigation.Freshness      `json:"freshness"`
}

type gapResponse struct {
	ExecutionID   string                     `json:"execution_id"`
	TotalGapCount int                        `json:"total_gap_count"`
	Items         []investigation.CaptureGap `json:"items"`
	Page          page                       `json:"page"`
	Freshness     investigation.Freshness    `json:"freshness"`
}

type sourceListResponse struct {
	Items []investigation.SourceOption `json:"items"`
}

type requestFailure struct {
	status    int
	code      string
	title     string
	retryable bool
	field     *ProblemField
}

func (s *Server) handleExecutionCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	query, filterHash, requestedLimit, failure := s.parseExecutionSearch(r)
	if failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	store, ok := s.requireInvestigationStore(w)
	if !ok {
		return
	}
	query.Limit = requestedLimit + 1
	items, err := store.SearchExecutions(r.Context(), query)
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	hasMore := len(items) > requestedLimit
	if hasMore {
		items = items[:requestedLimit]
	}
	if items == nil {
		items = make([]investigation.ExecutionSummary, 0)
	}
	responsePage := page{AsOf: query.AsOf.UTC(), Count: len(items), HasMore: hasMore}
	if hasMore {
		last := items[len(items)-1]
		cursor, err := pagination.Encode(pagination.Cursor{
			Version:    pagination.Version,
			Kind:       pagination.KindExecutionSearch,
			AsOf:       query.AsOf.UTC(),
			FilterHash: filterHash,
			Keys: []string{
				last.LastEffectiveAt.UTC().Format(time.RFC3339Nano),
				last.ExecutionID,
			},
		})
		if err != nil {
			writeStoreFailure(w, err)
			return
		}
		responsePage.NextCursor = cursor
	}
	writeJSON(w, http.StatusOK, executionSearchResponse{Items: items, Page: responsePage})
}

func (s *Server) handleInvestigationExecutionPath(w http.ResponseWriter, r *http.Request, parts []string) {
	store, ok := s.requireInvestigationStore(w)
	if !ok {
		return
	}
	executionID := parts[0]
	switch {
	case len(parts) == 1:
		s.handleExecutionDetail(w, r, store, executionID)
	case len(parts) == 2 && parts[1] == "timeline":
		s.handleTimeline(w, r, store, executionID)
	case len(parts) == 2 && parts[1] == "investigation":
		s.handleInvestigationProjection(w, r, store, executionID)
	case len(parts) == 2 && parts[1] == "trace-journey":
		s.handleTraceJourney(w, r, store, executionID)
	case len(parts) == 2 && parts[1] == "gaps":
		s.handleGaps(w, r, store, executionID)
	case len(parts) == 2 && parts[1] == "feedback":
		s.handleExecutionFeedback(w, r, store, executionID)
	case len(parts) == 2 && parts[1] == "versions":
		s.handleExecutionVersions(w, r, store, executionID)
	case len(parts) == 3 && parts[1] == "events":
		s.handleEventDetail(w, r, store, executionID, parts[2])
	default:
		writeProblem(w, http.StatusNotFound, "route_not_found", "Route not found", false)
	}
}

func (s *Server) handleTraceJourney(
	w http.ResponseWriter,
	r *http.Request,
	store InvestigationStore,
	executionID string,
) {
	if failure := validateQueryKeys(r, "as_of"); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	_, asOfPresent := r.URL.Query()["as_of"]
	asOf, failure := s.parseAsOf(r.URL.Query().Get("as_of"), asOfPresent)
	if failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	if _, err := store.GetExecution(r.Context(), executionID, asOf); err != nil {
		writeStoreFailure(w, err)
		return
	}
	journey, err := store.GetTraceJourney(r.Context(), executionID, asOf, 200)
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, journey)
}

func (s *Server) handleExecutionDetail(w http.ResponseWriter, r *http.Request, store InvestigationStore, executionID string) {
	if failure := validateQueryKeys(r, "as_of"); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	_, asOfPresent := r.URL.Query()["as_of"]
	asOf, failure := s.parseAsOf(r.URL.Query().Get("as_of"), asOfPresent)
	if failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	detail, err := store.GetExecution(r.Context(), executionID, asOf)
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleInvestigationProjection(
	w http.ResponseWriter,
	r *http.Request,
	store InvestigationStore,
	executionID string,
) {
	if failure := validateQueryKeys(r, "as_of"); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	_, asOfPresent := r.URL.Query()["as_of"]
	asOf, failure := s.parseAsOf(r.URL.Query().Get("as_of"), asOfPresent)
	if failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	detail, err := store.GetExecution(r.Context(), executionID, asOf)
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	timeline, timelineTotal, err := store.ListTimeline(r.Context(), investigation.TimelineQuery{
		ExecutionID: executionID,
		AsOf:        asOf,
		Limit:       201,
	})
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	events, eventTotal, err := store.ListExecutionEvents(r.Context(), executionID, asOf, 201)
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	if timelineTotal != eventTotal {
		writeStoreFailure(w, errors.New("investigation event views are inconsistent"))
		return
	}
	gaps, gapTotal, err := store.ListExecutionGaps(r.Context(), investigation.GapQuery{
		ExecutionID: executionID,
		AsOf:        asOf,
		Limit:       101,
	})
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, investigation.BuildProjection(investigation.ProjectionInput{
		Detail:          detail,
		Timeline:        timeline,
		Events:          events,
		TotalEventCount: timelineTotal,
		Gaps:            gaps,
		TotalGapCount:   gapTotal,
	}))
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request, store InvestigationStore, executionID string) {
	if failure := validateQueryKeys(r, "as_of", "limit", "cursor"); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	requestedLimit, failure := parseBoundedLimit(r, 100, 200)
	if failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	asOf, after, filterHash, failure := s.parseExecutionPageCursor(r, pagination.KindTimeline, executionID)
	if failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	detail, err := store.GetExecution(r.Context(), executionID, asOf)
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	query := investigation.TimelineQuery{ExecutionID: executionID, AsOf: asOf, Limit: requestedLimit + 1}
	if after != nil {
		effectiveAt, errOne := time.Parse(time.RFC3339Nano, after.Keys[0])
		ingestedAt, errTwo := time.Parse(time.RFC3339Nano, after.Keys[1])
		if errOne != nil || errTwo != nil || !uuidPattern.MatchString(after.Keys[2]) {
			writeRequestFailure(w, invalidCursorFailure())
			return
		}
		query.After = &investigation.TimelineKey{EffectiveAt: effectiveAt.UTC(), IngestedAt: ingestedAt.UTC(), EventID: strings.ToLower(after.Keys[2])}
	}
	items, total, err := store.ListTimeline(r.Context(), query)
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	hasMore := len(items) > requestedLimit
	if hasMore {
		items = items[:requestedLimit]
	}
	if items == nil {
		items = make([]investigation.TimelineNode, 0)
	}
	responsePage := page{AsOf: asOf.UTC(), Count: len(items), HasMore: hasMore}
	if hasMore {
		last := items[len(items)-1]
		responsePage.NextCursor, err = pagination.Encode(pagination.Cursor{
			Version:     pagination.Version,
			Kind:        pagination.KindTimeline,
			AsOf:        asOf.UTC(),
			FilterHash:  filterHash,
			ExecutionID: executionID,
			Keys: []string{
				last.EffectiveAt.UTC().Format(time.RFC3339Nano),
				last.IngestedAt.UTC().Format(time.RFC3339Nano),
				last.EventID,
			},
		})
		if err != nil {
			writeStoreFailure(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, timelineResponse{
		ExecutionID:     executionID,
		TotalEventCount: total,
		Items:           items,
		Page:            responsePage,
		Freshness:       detail.Summary.Freshness,
	})
}

func (s *Server) handleEventDetail(w http.ResponseWriter, r *http.Request, store InvestigationStore, executionID string, eventID string) {
	if !uuidPattern.MatchString(eventID) {
		writeRequestFailure(w, invalidField("event_id", "must_be_a_uuid"))
		return
	}
	if failure := validateQueryKeys(r, "as_of"); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	_, asOfPresent := r.URL.Query()["as_of"]
	asOf, failure := s.parseAsOf(r.URL.Query().Get("as_of"), asOfPresent)
	if failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	detail, err := store.GetExecutionEvent(r.Context(), executionID, strings.ToLower(eventID), asOf)
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleGaps(w http.ResponseWriter, r *http.Request, store InvestigationStore, executionID string) {
	if failure := validateQueryKeys(r, "as_of", "limit", "cursor"); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	requestedLimit, failure := parseBoundedLimit(r, 50, 100)
	if failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	asOf, after, filterHash, failure := s.parseExecutionPageCursor(r, pagination.KindGaps, executionID)
	if failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	detail, err := store.GetExecution(r.Context(), executionID, asOf)
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	query := investigation.GapQuery{ExecutionID: executionID, AsOf: asOf, Limit: requestedLimit + 1}
	if after != nil {
		createdAt, parseErr := time.Parse(time.RFC3339Nano, after.Keys[0])
		if parseErr != nil || !uuidPattern.MatchString(after.Keys[1]) {
			writeRequestFailure(w, invalidCursorFailure())
			return
		}
		query.After = &investigation.GapKey{CreatedAt: createdAt.UTC(), GapID: strings.ToLower(after.Keys[1])}
	}
	items, total, err := store.ListExecutionGaps(r.Context(), query)
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	hasMore := len(items) > requestedLimit
	if hasMore {
		items = items[:requestedLimit]
	}
	if items == nil {
		items = make([]investigation.CaptureGap, 0)
	}
	responsePage := page{AsOf: asOf.UTC(), Count: len(items), HasMore: hasMore}
	if hasMore {
		last := items[len(items)-1]
		responsePage.NextCursor, err = pagination.Encode(pagination.Cursor{
			Version:     pagination.Version,
			Kind:        pagination.KindGaps,
			AsOf:        asOf.UTC(),
			FilterHash:  filterHash,
			ExecutionID: executionID,
			Keys: []string{
				last.CreatedAt.UTC().Format(time.RFC3339Nano),
				last.GapID,
			},
		})
		if err != nil {
			writeStoreFailure(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, gapResponse{
		ExecutionID:   executionID,
		TotalGapCount: total,
		Items:         items,
		Page:          responsePage,
		Freshness:     detail.Summary.Freshness,
	})
}

func (s *Server) handleSourceCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if failure := validateQueryKeys(r, "limit"); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	store, ok := s.requireInvestigationStore(w)
	if !ok {
		return
	}
	limit, failure := parseBoundedLimit(r, 50, 100)
	if failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	items, err := store.ListSources(r.Context(), limit)
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	if items == nil {
		items = make([]investigation.SourceOption, 0)
	}
	writeJSON(w, http.StatusOK, sourceListResponse{Items: items})
}

func (s *Server) handleConnectorCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowedFor(w, http.MethodGet)
		return
	}
	if failure := validateQueryKeys(r); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	store, ok := s.store.(ConnectorTrustStore)
	if !ok {
		writeProblem(w, http.StatusServiceUnavailable, "database_unavailable", "Database is unavailable", true)
		return
	}
	items, err := store.ListConnectorTrust(r.Context(), 100)
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	if items == nil {
		items = []investigation.ConnectorTrust{}
	}
	installations := make([]connectorcontrol.Installation, 0)
	if s.connectorStore != nil {
		installations, err = s.connectorStore.ListInstallations(r.Context(), connectorcontrol.MaximumConnectorListSize)
		if err != nil {
			writeConnectorControlFailure(w, err)
			return
		}
		if installations == nil {
			installations = make([]connectorcontrol.Installation, 0)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":         items,
		"installations": installations,
		"installation_control": map[string]any{
			"enabled":                          s.connectorStore != nil && s.connectorConfig.Enabled(),
			"connector_type":                   connectorcontrol.ConnectorTypeUniRouter,
			"event_scope":                      []string{connectorcontrol.CompletedEventType},
			"ordinary_sync_requires_assurance": false,
		},
		"delivery_state_boundary": "mutable checkpoint, cursor, retry and ingestion state are not M0 Event Evidence",
	})
}

func (s *Server) handleSessionCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowedFor(w, http.MethodGet)
		return
	}
	if failure := validateQueryKeys(r, "limit"); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	store, ok := s.store.(AuthoritativeSessionStore)
	if !ok {
		writeProblem(w, http.StatusServiceUnavailable, "database_unavailable", "Database is unavailable", true)
		return
	}
	limit, failure := parseBoundedLimit(r, 50, 100)
	if failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	items, err := store.ListAuthoritativeSessions(r.Context(), limit)
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	if items == nil {
		items = []investigation.AuthoritativeSession{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":             items,
		"identity_boundary": "only source-declared session or thread identities are listed",
	})
}

func (s *Server) handleSessionPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowedFor(w, http.MethodGet)
		return
	}
	if failure := validateQueryKeys(r); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	sessionID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/"), "/")
	if !authoritativeSessionIDPattern.MatchString(sessionID) {
		writeProblem(w, http.StatusNotFound, "session_not_found", "Authoritative Session not found", false)
		return
	}
	store, ok := s.store.(AuthoritativeSessionStore)
	if !ok {
		writeProblem(w, http.StatusServiceUnavailable, "database_unavailable", "Database is unavailable", true)
		return
	}
	item, err := store.GetAuthoritativeSession(r.Context(), sessionID)
	if errors.Is(err, db.ErrSessionNotFound) {
		writeProblem(w, http.StatusNotFound, "session_not_found", "Authoritative Session not found", false)
		return
	}
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleExecutionCompare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowedFor(w, http.MethodGet)
		return
	}
	if failure := validateQueryKeys(r, "left", "left_as_of", "right", "right_as_of"); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	values := r.URL.Query()
	leftID := values.Get("left")
	rightID := values.Get("right")
	leftAsOfRaw, leftAsOfPresent := values["left_as_of"]
	rightAsOfRaw, rightAsOfPresent := values["right_as_of"]
	if !executionid.Valid(leftID) || !executionid.Valid(rightID) ||
		!leftAsOfPresent || len(leftAsOfRaw) != 1 ||
		!rightAsOfPresent || len(rightAsOfRaw) != 1 {
		writeProblem(w, http.StatusBadRequest, "invalid_argument", "Invalid comparison argument", false)
		return
	}
	leftAsOf, failure := s.parseAsOf(leftAsOfRaw[0], true)
	if failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	rightAsOf, failure := s.parseAsOf(rightAsOfRaw[0], true)
	if failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	store, ok := s.requireInvestigationStore(w)
	if !ok {
		return
	}
	left, err := loadProjection(r.Context(), store, leftID, leftAsOf)
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	right, err := loadProjection(r.Context(), store, rightID, rightAsOf)
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	writeJSON(
		w,
		http.StatusOK,
		investigation.CompareProjections(left, leftAsOf, right, rightAsOf),
	)
}

func (s *Server) handleExecutionFeedback(
	w http.ResponseWriter,
	r *http.Request,
	store InvestigationStore,
	executionID string,
) {
	if failure := validateQueryKeys(r, "as_of"); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	raw, present := r.URL.Query()["as_of"]
	if !present || len(raw) != 1 {
		writeProblem(w, http.StatusBadRequest, "invalid_argument", "Fixed as_of is required", false)
		return
	}
	asOf, failure := s.parseAsOf(raw[0], true)
	if failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	if _, err := store.GetExecution(r.Context(), executionID, asOf); err != nil {
		writeStoreFailure(w, err)
		return
	}
	extensions, ok := s.store.(AnalysisExtensionStore)
	if !ok {
		writeProblem(w, http.StatusServiceUnavailable, "database_unavailable", "Database is unavailable", true)
		return
	}
	items, err := extensions.ListEvaluationFeedback(r.Context(), executionID, asOf, 200)
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	if items == nil {
		items = []investigation.EvaluationFeedback{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"execution_id": executionID,
		"as_of":        asOf,
		"items":        items,
		"boundary":     "external evaluation facts are evidence-linked observations, not X-Insight enforcement",
	})
}

func (s *Server) handleExecutionVersions(
	w http.ResponseWriter,
	r *http.Request,
	store InvestigationStore,
	executionID string,
) {
	if failure := validateQueryKeys(r, "as_of"); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	raw, present := r.URL.Query()["as_of"]
	if !present || len(raw) != 1 {
		writeProblem(w, http.StatusBadRequest, "invalid_argument", "Fixed as_of is required", false)
		return
	}
	asOf, failure := s.parseAsOf(raw[0], true)
	if failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	if _, err := store.GetExecution(r.Context(), executionID, asOf); err != nil {
		writeStoreFailure(w, err)
		return
	}
	extensions, ok := s.store.(AnalysisExtensionStore)
	if !ok {
		writeProblem(w, http.StatusServiceUnavailable, "database_unavailable", "Database is unavailable", true)
		return
	}
	items, err := extensions.ListExecutionVersionDimensions(r.Context(), executionID, asOf, 100)
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	if items == nil {
		items = []investigation.ExecutionVersionDimension{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"execution_id": executionID,
		"as_of":        asOf,
		"items":        items,
	})
}

func (s *Server) handleAlertCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowedFor(w, http.MethodGet)
		return
	}
	if failure := validateQueryKeys(r, "as_of", "limit"); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	raw, present := r.URL.Query()["as_of"]
	if present && len(raw) != 1 {
		writeProblem(w, http.StatusBadRequest, "invalid_argument", "Invalid as_of argument", false)
		return
	}
	asOf, failure := s.parseAsOf(r.URL.Query().Get("as_of"), present && len(raw) == 1)
	if failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	limit, failure := parseBoundedLimit(r, 50, 100)
	if failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	store, ok := s.store.(AnalysisExtensionStore)
	if !ok {
		writeProblem(w, http.StatusServiceUnavailable, "database_unavailable", "Database is unavailable", true)
		return
	}
	items, err := store.ListInvestigationAlerts(r.Context(), asOf, limit)
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	if items == nil {
		items = []investigation.InvestigationAlert{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"as_of": asOf, "items": items})
}

func (s *Server) handleHypothesisCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowedFor(w, http.MethodGet)
		return
	}
	if failure := validateQueryKeys(r, "as_of", "limit"); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	raw, present := r.URL.Query()["as_of"]
	if present && len(raw) != 1 {
		writeProblem(w, http.StatusBadRequest, "invalid_argument", "Invalid as_of argument", false)
		return
	}
	asOf, failure := s.parseAsOf(r.URL.Query().Get("as_of"), present && len(raw) == 1)
	if failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	limit, failure := parseBoundedLimit(r, 50, 100)
	if failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	store, ok := s.store.(AnalysisExtensionStore)
	if !ok {
		writeProblem(w, http.StatusServiceUnavailable, "database_unavailable", "Database is unavailable", true)
		return
	}
	items, err := store.ListDiscoveryHypotheses(r.Context(), asOf, limit)
	if err != nil {
		writeStoreFailure(w, err)
		return
	}
	if items == nil {
		items = []investigation.DiscoveryHypothesis{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"as_of":    asOf,
		"items":    items,
		"boundary": "hypotheses are reviewable and abstaining; they are not M0 facts or automatic enforcement",
	})
}

func loadProjection(
	ctx context.Context,
	store InvestigationStore,
	executionID string,
	asOf time.Time,
) (investigation.InvestigationProjection, error) {
	detail, err := store.GetExecution(ctx, executionID, asOf)
	if err != nil {
		return investigation.InvestigationProjection{}, err
	}
	timeline, timelineTotal, err := store.ListTimeline(ctx, investigation.TimelineQuery{
		ExecutionID: executionID, AsOf: asOf, Limit: 201,
	})
	if err != nil {
		return investigation.InvestigationProjection{}, err
	}
	events, eventTotal, err := store.ListExecutionEvents(ctx, executionID, asOf, 201)
	if err != nil {
		return investigation.InvestigationProjection{}, err
	}
	if timelineTotal != eventTotal {
		return investigation.InvestigationProjection{}, errors.New("investigation event views are inconsistent")
	}
	gaps, gapTotal, err := store.ListExecutionGaps(ctx, investigation.GapQuery{
		ExecutionID: executionID, AsOf: asOf, Limit: 101,
	})
	if err != nil {
		return investigation.InvestigationProjection{}, err
	}
	return investigation.BuildProjection(investigation.ProjectionInput{
		Detail: detail, Timeline: timeline, Events: events, TotalEventCount: timelineTotal,
		Gaps: gaps, TotalGapCount: gapTotal,
	}), nil
}

type searchFilterFingerprint struct {
	RequestID      string `json:"request_id"`
	RequestPrefix  string `json:"request_id_prefix"`
	SourceSystemID string `json:"source_system_id"`
	Outcome        string `json:"outcome"`
	HasGaps        *bool  `json:"has_gaps"`
	From           string `json:"from"`
	To             string `json:"to"`
	Sort           string `json:"sort"`
}

func (s *Server) parseExecutionSearch(r *http.Request) (investigation.SearchQuery, string, int, *requestFailure) {
	if failure := validateQueryKeys(r, "request_id", "request_id_prefix", "source_system_id", "outcome", "has_gaps", "from", "to", "as_of", "limit", "cursor"); failure != nil {
		return investigation.SearchQuery{}, "", 0, failure
	}
	values := r.URL.Query()
	requestID := values.Get("request_id")
	requestPrefix := values.Get("request_id_prefix")
	if requestID != "" && requestPrefix != "" {
		return investigation.SearchQuery{}, "", 0, invalidField("request_id", "mutually_exclusive_with_request_id_prefix")
	}
	if _, exists := values["request_id"]; exists {
		if err := executionid.ValidateRequestID(requestID); err != nil {
			return investigation.SearchQuery{}, "", 0, invalidField("request_id", "invalid_utf8_or_length")
		}
	}
	if _, exists := values["request_id_prefix"]; exists {
		if err := executionid.ValidateRequestIDPrefix(requestPrefix); err != nil {
			return investigation.SearchQuery{}, "", 0, invalidField("request_id_prefix", "invalid_utf8_or_length")
		}
	}
	sourceSystemID := values.Get("source_system_id")
	if _, exists := values["source_system_id"]; exists {
		canonical, err := executionid.CanonicalSourceSystemID(sourceSystemID)
		if err != nil {
			return investigation.SearchQuery{}, "", 0, invalidField("source_system_id", "must_be_a_uuid")
		}
		sourceSystemID = canonical
	}
	outcome := values.Get("outcome")
	if _, exists := values["outcome"]; exists && !validOutcome(outcome) {
		return investigation.SearchQuery{}, "", 0, invalidField("outcome", "unsupported_value")
	}
	var hasGaps *bool
	if _, exists := values["has_gaps"]; exists {
		switch values.Get("has_gaps") {
		case "true":
			value := true
			hasGaps = &value
		case "false":
			value := false
			hasGaps = &value
		default:
			return investigation.SearchQuery{}, "", 0, invalidField("has_gaps", "must_be_true_or_false")
		}
	}
	limit, failure := parseBoundedLimit(r, 50, 100)
	if failure != nil {
		return investigation.SearchQuery{}, "", 0, failure
	}

	now := s.now().UTC()
	asOf := now
	_, asOfPresent := values["as_of"]
	if asOfPresent {
		parsedAsOf, failure := s.parseAsOf(values.Get("as_of"), true)
		if failure != nil {
			return investigation.SearchQuery{}, "", 0, failure
		}
		asOf = parsedAsOf
	}
	var decodedCursor *pagination.Cursor
	if rawCursor, cursorPresent := values["cursor"]; cursorPresent {
		if rawCursor[0] == "" {
			return investigation.SearchQuery{}, "", 0, invalidCursorFailure()
		}
		cursor, err := pagination.Decode(rawCursor[0], now)
		if err != nil {
			return investigation.SearchQuery{}, "", 0, cursorFailure(err)
		}
		if asOfPresent && !asOf.Equal(cursor.AsOf) {
			return investigation.SearchQuery{}, "", 0, cursorFailure(pagination.ErrMismatch)
		}
		decodedCursor = &cursor
		asOf = cursor.AsOf.UTC()
	}
	from := asOf.Add(-24 * time.Hour)
	to := asOf
	if _, exists := values["from"]; exists {
		parsed, failure := parseUTCField("from", values.Get("from"))
		if failure != nil {
			return investigation.SearchQuery{}, "", 0, failure
		}
		from = parsed
	}
	if _, exists := values["to"]; exists {
		parsed, failure := parseUTCField("to", values.Get("to"))
		if failure != nil {
			return investigation.SearchQuery{}, "", 0, failure
		}
		to = parsed
	}
	if !from.Before(to) {
		return investigation.SearchQuery{}, "", 0, invalidField("from", "must_be_before_to")
	}
	if to.Sub(from) > 31*24*time.Hour {
		return investigation.SearchQuery{}, "", 0, invalidField("from", "range_must_not_exceed_31_days")
	}
	if to.After(asOf) {
		return investigation.SearchQuery{}, "", 0, invalidField("to", "must_not_be_after_as_of")
	}
	fingerprint := searchFilterFingerprint{
		RequestID:      requestID,
		RequestPrefix:  requestPrefix,
		SourceSystemID: sourceSystemID,
		Outcome:        outcome,
		HasGaps:        hasGaps,
		From:           from.UTC().Format(time.RFC3339Nano),
		To:             to.UTC().Format(time.RFC3339Nano),
		Sort:           "last_effective_at_desc,execution_id_desc",
	}
	filterHash, err := pagination.HashCanonicalFilters(fingerprint)
	if err != nil {
		return investigation.SearchQuery{}, "", 0, &requestFailure{status: http.StatusInternalServerError, code: "internal_error", title: "Internal server error"}
	}
	query := investigation.SearchQuery{
		AsOf:           asOf,
		From:           from,
		To:             to,
		RequestID:      requestID,
		RequestPrefix:  requestPrefix,
		SourceSystemID: sourceSystemID,
		Outcome:        outcome,
		HasGaps:        hasGaps,
	}
	if decodedCursor != nil {
		if err := pagination.ValidateBinding(*decodedCursor, pagination.KindExecutionSearch, filterHash, ""); err != nil {
			return investigation.SearchQuery{}, "", 0, cursorFailure(err)
		}
		if len(decodedCursor.Keys) != 2 || !executionid.Valid(decodedCursor.Keys[1]) {
			return investigation.SearchQuery{}, "", 0, invalidCursorFailure()
		}
		lastEffectiveAt, err := time.Parse(time.RFC3339Nano, decodedCursor.Keys[0])
		if err != nil {
			return investigation.SearchQuery{}, "", 0, invalidCursorFailure()
		}
		query.After = &investigation.SearchKey{LastEffectiveAt: lastEffectiveAt.UTC(), ExecutionID: decodedCursor.Keys[1]}
	}
	return query, filterHash, limit, nil
}

func (s *Server) parseExecutionPageCursor(r *http.Request, kind pagination.Kind, executionID string) (time.Time, *pagination.Cursor, string, *requestFailure) {
	now := s.now().UTC()
	explicitAsOf, asOfPresent := r.URL.Query()["as_of"]
	rawAsOf := ""
	if asOfPresent {
		rawAsOf = explicitAsOf[0]
	}
	asOf, failure := s.parseAsOf(rawAsOf, asOfPresent)
	if failure != nil {
		return time.Time{}, nil, "", failure
	}
	filterHash, err := pagination.HashCanonicalFilters(struct {
		Sort string `json:"sort"`
	}{Sort: string(kind)})
	if err != nil {
		return time.Time{}, nil, "", &requestFailure{status: http.StatusInternalServerError, code: "internal_error", title: "Internal server error"}
	}
	rawCursorValues, cursorPresent := r.URL.Query()["cursor"]
	if !cursorPresent {
		return asOf, nil, filterHash, nil
	}
	if rawCursorValues[0] == "" {
		return time.Time{}, nil, "", invalidCursorFailure()
	}
	cursor, err := pagination.Decode(rawCursorValues[0], now)
	if err != nil {
		return time.Time{}, nil, "", cursorFailure(err)
	}
	if err := pagination.ValidateBinding(cursor, kind, filterHash, executionID); err != nil {
		return time.Time{}, nil, "", cursorFailure(err)
	}
	if asOfPresent && !asOf.Equal(cursor.AsOf) {
		return time.Time{}, nil, "", cursorFailure(pagination.ErrMismatch)
	}
	expectedKeys := 2
	if kind == pagination.KindTimeline {
		expectedKeys = 3
	}
	if len(cursor.Keys) != expectedKeys {
		return time.Time{}, nil, "", invalidCursorFailure()
	}
	return cursor.AsOf.UTC(), &cursor, filterHash, nil
}

func (s *Server) parseAsOf(raw string, present bool) (time.Time, *requestFailure) {
	now := s.now().UTC()
	if !present {
		return now, nil
	}
	if raw == "" {
		return time.Time{}, invalidField("as_of", "must_be_rfc3339")
	}
	parsed, failure := parseUTCField("as_of", raw)
	if failure != nil {
		return time.Time{}, failure
	}
	if parsed.After(now.Add(time.Minute)) {
		return time.Time{}, invalidField("as_of", "must_not_be_more_than_one_minute_in_future")
	}
	if now.After(parsed.Add(pagination.MaxCursorAge)) {
		return time.Time{}, &requestFailure{status: http.StatusGone, code: "cursor_expired", title: "Cursor has expired"}
	}
	return parsed, nil
}

func parseUTCField(name string, raw string) (time.Time, *requestFailure) {
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, invalidField(name, "must_be_rfc3339")
	}
	_, offset := parsed.Zone()
	if offset != 0 {
		return time.Time{}, invalidField(name, "must_be_utc")
	}
	return parsed.UTC(), nil
}

func parseBoundedLimit(r *http.Request, defaultValue int, maximum int) (int, *requestFailure) {
	values, exists := r.URL.Query()["limit"]
	if !exists {
		return defaultValue, nil
	}
	if len(values) != 1 || values[0] == "" {
		return 0, invalidField("limit", "must_be_an_integer")
	}
	value, err := strconv.Atoi(values[0])
	if err != nil || value < 1 || value > maximum {
		return 0, invalidField("limit", "must_be_between_1_and_"+strconv.Itoa(maximum))
	}
	return value, nil
}

func validateQueryKeys(r *http.Request, allowed ...string) *requestFailure {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return invalidField("query", "must_be_valid_url_encoding")
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	for key, entries := range values {
		if _, ok := allowedSet[key]; !ok {
			return invalidField(key, "unknown_query_parameter")
		}
		if len(entries) != 1 {
			return invalidField(key, "must_appear_once")
		}
	}
	return nil
}

func validOutcome(value string) bool {
	switch value {
	case investigation.OutcomeSuccess, investigation.OutcomeFailure, investigation.OutcomePartial, investigation.OutcomeUnknown:
		return true
	default:
		return false
	}
}

func (s *Server) requireInvestigationStore(w http.ResponseWriter) (InvestigationStore, bool) {
	if s.store == nil {
		writeProblem(w, http.StatusServiceUnavailable, "database_unavailable", "Database is unavailable", true)
		return nil, false
	}
	store, ok := s.store.(InvestigationStore)
	if !ok {
		writeProblem(w, http.StatusServiceUnavailable, "database_unavailable", "Database is unavailable", true)
		return nil, false
	}
	return store, true
}

func invalidField(name string, reason string) *requestFailure {
	field := ProblemField{Name: name, Reason: reason}
	return &requestFailure{
		status: http.StatusBadRequest,
		code:   "invalid_argument",
		title:  "Invalid request argument",
		field:  &field,
	}
}

func invalidCursorFailure() *requestFailure {
	return &requestFailure{status: http.StatusBadRequest, code: "invalid_cursor", title: "Invalid cursor"}
}

func cursorFailure(err error) *requestFailure {
	switch {
	case errors.Is(err, pagination.ErrExpired):
		return &requestFailure{status: http.StatusGone, code: "cursor_expired", title: "Cursor has expired"}
	case errors.Is(err, pagination.ErrMismatch):
		return &requestFailure{status: http.StatusConflict, code: "cursor_mismatch", title: "Cursor does not match request"}
	default:
		return invalidCursorFailure()
	}
}

func writeRequestFailure(w http.ResponseWriter, failure *requestFailure) {
	if failure == nil {
		return
	}
	if failure.field != nil {
		writeProblem(w, failure.status, failure.code, failure.title, failure.retryable, *failure.field)
		return
	}
	writeProblem(w, failure.status, failure.code, failure.title, failure.retryable)
}

func writeStoreFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, db.ErrExecutionNotFound):
		writeProblem(w, http.StatusNotFound, "execution_not_found", "Execution not found", false)
	case errors.Is(err, db.ErrEventNotFound):
		writeProblem(w, http.StatusNotFound, "event_not_found", "Event not found", false)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		writeProblem(w, http.StatusServiceUnavailable, "temporarily_overloaded", "Service is temporarily overloaded", true)
	default:
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Internal server error", false)
	}
}

type tokenBucket struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

func newTokenBucket(rate float64, burst int) *tokenBucket {
	return &tokenBucket{rate: rate, burst: float64(burst), tokens: float64(burst), last: time.Now()}
}

func (bucket *tokenBucket) allow(now time.Time) bool {
	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	elapsed := now.Sub(bucket.last).Seconds()
	if elapsed > 0 {
		bucket.tokens += elapsed * bucket.rate
		if bucket.tokens > bucket.burst {
			bucket.tokens = bucket.burst
		}
		bucket.last = now
	}
	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens--
	return true
}

func (s *Server) withAPILimits(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if len(r.RequestURI) > maxRequestURIBytes {
			writeProblem(w, http.StatusBadRequest, "invalid_argument", "Invalid request argument", false,
				ProblemField{Name: "request_uri", Reason: "must_not_exceed_8192_bytes"})
			return
		}
		if !s.rateLimiter.allow(time.Now()) {
			w.Header().Set("Retry-After", "1")
			writeProblem(w, http.StatusTooManyRequests, "request_rate_limited", "Request rate is limited", true)
			return
		}
		select {
		case s.apiCapacity <- struct{}{}:
			defer func() { <-s.apiCapacity }()
		default:
			writeProblem(w, http.StatusServiceUnavailable, "temporarily_overloaded", "Service is temporarily overloaded", true)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), apiQueryTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
