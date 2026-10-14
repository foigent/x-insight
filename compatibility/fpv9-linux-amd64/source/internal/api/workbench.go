package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	productaccess "github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/workbench"
)

type workbenchRecentStore interface {
	GetWorkbenchRecentRequests(context.Context, string, string, string, int, string, time.Time) (workbench.RecentRequests, error)
}

func (s *Server) handleWorkbenchReadings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	if s.workbenchStore == nil {
		writeProblem(w, http.StatusServiceUnavailable, "workbench_unavailable", "Cost workbench is unavailable", true)
		return
	}
	if failure := validateQueryKeys(r); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	authentication, ok := s.requireWorkbenchMutation(w, r, productaccess.PermissionWorkbenchSummary)
	if !ok {
		return
	}
	var request workbench.ReadingRequest
	if err := decodeWorkspaceRequest(w, r, &request); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_workbench_request", "Workbench request is invalid", false)
		return
	}
	item, err := s.workbenchStore.CreateWorkbenchReading(r.Context(), authentication.Session.SubjectID, request, s.now().UTC())
	if err != nil {
		writeWorkbenchFailure(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleWorkbenchReadingPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowedFor(w, http.MethodGet)
		return
	}
	if s.workbenchStore == nil {
		writeProblem(w, http.StatusServiceUnavailable, "workbench_unavailable", "Cost workbench is unavailable", true)
		return
	}
	authentication, ok := accessAuthentication(r)
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "authentication_required", "Authentication is required", false)
		return
	}
	suffix := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/workbench/readings/"), "/")
	parts := strings.Split(suffix, "/")
	if len(parts) < 1 || !workbench.ValidReadingID(parts[0]) {
		writeWorkbenchFailure(w, workbench.ErrNotFound)
		return
	}
	id := parts[0]
	var item any
	var err error
	if len(parts) == 1 {
		if failure := validateQueryKeys(r); failure != nil {
			writeRequestFailure(w, failure)
			return
		}
		item, err = s.workbenchStore.GetWorkbenchReading(r.Context(), authentication.Session.SubjectID, id, s.now().UTC())
	} else if len(parts) == 2 {
		switch parts[1] {
		case "summary":
			if failure := validateQueryKeys(r); failure != nil {
				writeRequestFailure(w, failure)
				return
			}
			item, err = s.workbenchStore.GetWorkbenchSummary(r.Context(), authentication.Session.SubjectID, id, s.now().UTC())
		case "contributions":
			if failure := validateQueryKeys(r, "dimension", "limit", "top_n", "cursor", "order_by"); failure != nil {
				writeRequestFailure(w, failure)
				return
			}
			dimension := r.URL.Query().Get("dimension")
			orderBy := defaultString(r.URL.Query().Get("order_by"), "amount")
			limit, topN, parseErr := parseContributionWindow(r)
			if parseErr != nil || (orderBy != "amount" && orderBy != "requests") {
				err = workbench.ErrInvalid
			} else {
				item, err = s.workbenchStore.GetWorkbenchContributionsOrdered(r.Context(), authentication.Session.SubjectID, id, dimension, orderBy, limit, topN, r.URL.Query().Get("cursor"), s.now().UTC())
			}
		case "series":
			if failure := validateQueryKeys(r, "metric", "bucket_seconds"); failure != nil {
				writeRequestFailure(w, failure)
				return
			}
			bucket, parseErr := strconv.ParseInt(defaultString(r.URL.Query().Get("bucket_seconds"), "3600"), 10, 64)
			if parseErr != nil {
				err = workbench.ErrInvalid
			} else {
				item, err = s.workbenchStore.GetWorkbenchSeries(r.Context(), authentication.Session.SubjectID, id, defaultString(r.URL.Query().Get("metric"), "amount"), bucket, s.now().UTC())
			}
		case "details":
			if failure := validateQueryKeys(r, "contribution_ref", "limit", "cursor"); failure != nil {
				writeRequestFailure(w, failure)
				return
			}
			if !productaccess.HasPermission(authentication.Session.Role, productaccess.PermissionWorkbenchDetails) {
				writeProblem(w, http.StatusForbidden, "permission_denied", "Permission is denied", false)
				return
			}
			limit, parseErr := strconv.Atoi(defaultString(r.URL.Query().Get("limit"), "50"))
			if parseErr != nil {
				err = workbench.ErrInvalid
			} else {
				item, err = s.workbenchStore.GetWorkbenchDetails(r.Context(), authentication.Session.SubjectID, id, r.URL.Query().Get("contribution_ref"), limit, r.URL.Query().Get("cursor"), s.now().UTC())
			}
		case "recent-requests":
			if failure := validateQueryKeys(r, "runtime_reading_id", "limit", "cursor"); failure != nil {
				writeRequestFailure(w, failure)
				return
			}
			if !productaccess.HasPermission(authentication.Session.Role, productaccess.PermissionWorkbenchDetails) {
				writeProblem(w, http.StatusForbidden, "permission_denied", "Permission is denied", false)
				return
			}
			store, available := s.workbenchStore.(workbenchRecentStore)
			if !available {
				writeProblem(w, http.StatusServiceUnavailable, "workbench_unavailable", "Recent requests are unavailable", true)
				return
			}
			limit, parseErr := strconv.Atoi(defaultString(r.URL.Query().Get("limit"), "10"))
			if parseErr != nil {
				err = workbench.ErrInvalid
			} else {
				item, err = store.GetWorkbenchRecentRequests(r.Context(), authentication.Session.SubjectID, id, r.URL.Query().Get("runtime_reading_id"), limit, r.URL.Query().Get("cursor"), s.now().UTC())
			}
		case "sources":
			if failure := validateQueryKeys(r); failure != nil {
				writeRequestFailure(w, failure)
				return
			}
			if !productaccess.HasPermission(authentication.Session.Role, productaccess.PermissionWorkbenchCoverage) {
				writeProblem(w, http.StatusForbidden, "permission_denied", "Permission is denied", false)
				return
			}
			item, err = s.workbenchStore.GetWorkbenchSourceStatuses(r.Context(), authentication.Session.SubjectID, id, s.now().UTC())
		case "source-facts":
			if failure := validateQueryKeys(r, "limit", "cursor"); failure != nil {
				writeRequestFailure(w, failure)
				return
			}
			if !productaccess.HasPermission(authentication.Session.Role, productaccess.PermissionWorkbenchDetails) {
				writeProblem(w, http.StatusForbidden, "permission_denied", "Permission is denied", false)
				return
			}
			store, available := s.workbenchStore.(WorkbenchSourceStore)
			if !available {
				writeProblem(w, http.StatusServiceUnavailable, "workbench_unavailable", "Source reading is unavailable", true)
				return
			}
			limit, parseErr := strconv.Atoi(defaultString(r.URL.Query().Get("limit"), "50"))
			if parseErr != nil {
				err = workbench.ErrInvalid
			} else {
				item, err = store.GetWorkbenchSourceFacts(r.Context(), authentication.Session.SubjectID, id, limit, r.URL.Query().Get("cursor"), s.now().UTC())
			}
		default:
			err = workbench.ErrNotFound
		}
	} else {
		err = workbench.ErrNotFound
	}
	if err != nil {
		writeWorkbenchFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleWorkbenchSavedFilters(w http.ResponseWriter, r *http.Request) {
	if s.workbenchStore == nil {
		writeProblem(w, http.StatusServiceUnavailable, "workbench_unavailable", "Cost workbench is unavailable", true)
		return
	}
	authentication, ok := accessAuthentication(r)
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "authentication_required", "Authentication is required", false)
		return
	}
	if !productaccess.HasPermission(authentication.Session.Role, productaccess.PermissionWorkbenchPreferences) {
		writeProblem(w, http.StatusForbidden, "permission_denied", "Permission is denied", false)
		return
	}
	if failure := validateQueryKeys(r); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	switch r.Method {
	case http.MethodGet:
		item, err := s.workbenchStore.ListWorkbenchSavedFilters(r.Context(), authentication.Session.SubjectID)
		if err != nil {
			writeWorkbenchFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodPost:
		if _, ok := s.requireWorkbenchMutation(w, r, productaccess.PermissionWorkbenchPreferences); !ok {
			return
		}
		var command workbench.SavedFilterCommand
		if err := decodeWorkspaceRequest(w, r, &command); err != nil {
			writeWorkbenchFailure(w, workbench.ErrInvalid)
			return
		}
		item, err := s.workbenchStore.CreateWorkbenchSavedFilter(r.Context(), authentication.Session.SubjectID, command, s.now().UTC())
		if err != nil {
			writeWorkbenchFailure(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, item)
	default:
		methodNotAllowedFor(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleWorkbenchSavedFilterPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		methodNotAllowedFor(w, http.MethodPut)
		return
	}
	if s.workbenchStore == nil {
		writeProblem(w, http.StatusServiceUnavailable, "workbench_unavailable", "Cost workbench is unavailable", true)
		return
	}
	if failure := validateQueryKeys(r); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	authentication, ok := s.requireWorkbenchMutation(w, r, productaccess.PermissionWorkbenchPreferences)
	if !ok {
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/workbench/saved-filters/"), "/")
	if !workbench.ValidSavedFilterID(id) || strings.Contains(id, "/") {
		writeWorkbenchFailure(w, workbench.ErrNotFound)
		return
	}
	var command workbench.SavedFilterCommand
	if err := decodeWorkspaceRequest(w, r, &command); err != nil {
		writeWorkbenchFailure(w, workbench.ErrInvalid)
		return
	}
	item, err := s.workbenchStore.UpdateWorkbenchSavedFilter(r.Context(), authentication.Session.SubjectID, id, command, s.now().UTC())
	if err != nil {
		writeWorkbenchFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) requireWorkbenchMutation(w http.ResponseWriter, r *http.Request, permission productaccess.Permission) (productaccess.Authentication, bool) {
	authentication, ok := accessAuthentication(r)
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "authentication_required", "Authentication is required", false)
		return productaccess.Authentication{}, false
	}
	if !productaccess.HasPermission(authentication.Session.Role, permission) {
		writeProblem(w, http.StatusForbidden, "permission_denied", "Permission is denied", false)
		return productaccess.Authentication{}, false
	}
	if !s.transportSecurity.requireSameOriginHTTPS(r) || !s.access.VerifyCSRF(authentication, r.Header.Get(csrfHeaderName)) {
		writeProblem(w, http.StatusForbidden, "csrf_validation_failed", "Request authorization failed", false)
		return productaccess.Authentication{}, false
	}
	return authentication, true
}

func writeWorkbenchFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workbench.ErrInvalid), errors.Is(err, workbench.ErrUnsupportedVersion):
		writeProblem(w, http.StatusBadRequest, "invalid_workbench_request", "Workbench request is invalid", false)
	case errors.Is(err, workbench.ErrScopeForbidden):
		writeProblem(w, http.StatusForbidden, "workbench_scope_denied", "Workbench scope is denied", false)
	case errors.Is(err, workbench.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "workbench_record_not_found", "Workbench record not found", false)
	case errors.Is(err, workbench.ErrExpired):
		writeProblem(w, http.StatusGone, "workbench_reading_expired", "Workbench reading expired", false)
	case errors.Is(err, workbench.ErrAccessChanged):
		writeProblem(w, http.StatusConflict, "workbench_access_changed", "Workbench authorization changed; create a new reading", false)
	case errors.Is(err, workbench.ErrTooLarge):
		writeProblem(w, http.StatusUnprocessableEntity, "workbench_reading_too_large", "Workbench reading is too large", false)
	case errors.Is(err, workbench.ErrConflict):
		writeProblem(w, http.StatusConflict, "workbench_conflict", "Workbench record conflicts", false)
	case errors.Is(err, workbench.ErrStaleRevision):
		writeProblem(w, http.StatusConflict, "workbench_revision_stale", "Workbench revision is stale", false)
	default:
		writeProblem(w, http.StatusServiceUnavailable, "workbench_unavailable", "Cost workbench is unavailable", true)
	}
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func parseContributionWindow(r *http.Request) (int, int, error) {
	limitValue, topValue := r.URL.Query().Get("limit"), r.URL.Query().Get("top_n")
	if limitValue != "" && topValue != "" {
		return 0, 0, workbench.ErrInvalid
	}
	if limitValue == "" && topValue == "" {
		topValue = "10"
	}
	if limitValue != "" {
		value, err := strconv.Atoi(limitValue)
		return value, 0, err
	}
	value, err := strconv.Atoi(topValue)
	return 0, value, err
}
