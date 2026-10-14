package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	productaccess "github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/workbench"
)

type RuntimeReadingStore interface {
	CreateRuntimeReading(context.Context, string, workbench.RuntimeReadingRequest, time.Time) (workbench.RuntimeReading, error)
	GetRuntimeReading(context.Context, string, string, time.Time) (workbench.RuntimeReading, error)
	GetRuntimeRequests(context.Context, string, string, string, int, string, time.Time) (workbench.RuntimeRequests, error)
	GetRuntimeObservations(context.Context, string, string, string, string, int, string, time.Time) (workbench.RuntimeObservations, error)
	GetRuntimeMeasurements(context.Context, string, string, string, int, string, time.Time) (workbench.RuntimeMeasurements, error)
	GetRuntimeSummary(context.Context, string, string, string, time.Time) (workbench.RuntimeSummary, error)
	GetRuntimeAssociations(context.Context, string, string, string, int, string, time.Time) (workbench.RuntimeAssociations, error)
	GetRuntimeResources(context.Context, string, string, string, int, string, time.Time) (workbench.RuntimeResources, error)
	GetRuntimeActionWitnesses(context.Context, string, string, string, int, string, time.Time) (workbench.RuntimeActionWitnesses, error)
	GetRuntimeExecutionRequest(context.Context, string, string, string, time.Time) (workbench.RuntimeExecutionRequest, error)
	GetRuntimeDimensions(context.Context, string, string, string, string, int, string, time.Time) (workbench.RuntimeDimensions, error)
}

func (s *Server) handleRuntimeReadings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	store, ok := s.workbenchStore.(RuntimeReadingStore)
	if !ok {
		writeProblem(w, http.StatusServiceUnavailable, "runtime_reading_unavailable", "Runtime reading is unavailable", true)
		return
	}
	if failure := validateQueryKeys(r); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	auth, ok := s.requireWorkbenchMutation(w, r, productaccess.PermissionWorkbenchDetails)
	if !ok {
		return
	}
	var input workbench.RuntimeReadingRequest
	if err := decodeWorkspaceRequest(w, r, &input); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_runtime_reading", "Runtime reading request is invalid", false)
		return
	}
	result, err := store.CreateRuntimeReading(r.Context(), auth.Session.SubjectID, input, s.now().UTC())
	if err != nil {
		writeWorkbenchFailure(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) handleRuntimeReadingPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowedFor(w, http.MethodGet)
		return
	}
	store, ok := s.workbenchStore.(RuntimeReadingStore)
	if !ok {
		writeProblem(w, http.StatusServiceUnavailable, "runtime_reading_unavailable", "Runtime reading is unavailable", true)
		return
	}
	auth, ok := accessAuthentication(r)
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "authentication_required", "Authentication is required", false)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/runtime/readings/"), "/")
	if len(parts) < 1 || !workbench.ValidRuntimeReadingID(parts[0]) {
		writeWorkbenchFailure(w, workbench.ErrNotFound)
		return
	}
	id, subject, now := parts[0], auth.Session.SubjectID, s.now().UTC()
	if len(parts) == 2 && parts[1] == "summary" {
		if failure := validateQueryKeys(r, "entry_time"); failure != nil {
			writeRequestFailure(w, failure)
			return
		}
		result, err := store.GetRuntimeSummary(r.Context(), subject, id, defaultString(r.URL.Query().Get("entry_time"), "known"), now)
		if err != nil {
			writeWorkbenchFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	if len(parts) == 1 || len(parts) == 2 && parts[1] == "sources" {
		if failure := validateQueryKeys(r); failure != nil {
			writeRequestFailure(w, failure)
			return
		}
		result, err := store.GetRuntimeReading(r.Context(), subject, id, now)
		if err != nil {
			writeWorkbenchFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	if len(parts) == 3 && parts[1] == "executions" {
		if failure := validateQueryKeys(r); failure != nil {
			writeRequestFailure(w, failure)
			return
		}
		result, err := store.GetRuntimeExecutionRequest(r.Context(), subject, id, parts[2], now)
		if err != nil {
			writeWorkbenchFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	queryKeys := []string{"limit", "cursor"}
	if len(parts) == 2 && (parts[1] == "requests" || parts[1] == "measurements") || len(parts) == 3 && parts[1] == "dimensions" {
		queryKeys = append(queryKeys, "entry_time")
	}
	if failure := validateQueryKeys(r, queryKeys...); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	limit, err := strconv.Atoi(defaultString(r.URL.Query().Get("limit"), "50"))
	if err != nil {
		writeWorkbenchFailure(w, workbench.ErrInvalid)
		return
	}
	cursor := r.URL.Query().Get("cursor")
	if len(parts) == 4 && parts[1] == "requests" && parts[3] == "resources" {
		result, err := store.GetRuntimeResources(r.Context(), subject, id, parts[2], limit, cursor, now)
		if err != nil {
			writeWorkbenchFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	if len(parts) == 4 && parts[1] == "requests" && parts[3] == "action-witnesses" {
		result, err := store.GetRuntimeActionWitnesses(r.Context(), subject, id, parts[2], limit, cursor, now)
		if err != nil {
			writeWorkbenchFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	if len(parts) == 3 && parts[1] == "dimensions" {
		result, err := store.GetRuntimeDimensions(r.Context(), subject, id, parts[2], defaultString(r.URL.Query().Get("entry_time"), "known"), limit, cursor, now)
		if err != nil {
			writeWorkbenchFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	if len(parts) == 2 && parts[1] == "measurements" {
		result, err := store.GetRuntimeMeasurements(r.Context(), subject, id, defaultString(r.URL.Query().Get("entry_time"), "known"), limit, cursor, now)
		if err != nil {
			writeWorkbenchFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	if len(parts) == 2 && parts[1] == "requests" {
		result, err := store.GetRuntimeRequests(r.Context(), subject, id, defaultString(r.URL.Query().Get("entry_time"), "all"), limit, cursor, now)
		if err != nil {
			writeWorkbenchFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	if len(parts) == 4 && parts[1] == "requests" && parts[3] == "associations" {
		result, err := store.GetRuntimeAssociations(r.Context(), subject, id, parts[2], limit, cursor, now)
		if err != nil {
			writeWorkbenchFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	profile, requestRef := "", ""
	if len(parts) == 3 && parts[1] == "requests" && workbench.ValidRuntimeRequestRef(parts[2]) {
		requestRef = parts[2]
	} else if len(parts) == 2 {
		var valid bool
		profile, valid = workbench.RuntimeCollectionProfile(parts[1])
		if !valid {
			writeWorkbenchFailure(w, workbench.ErrNotFound)
			return
		}
	} else {
		writeWorkbenchFailure(w, workbench.ErrNotFound)
		return
	}
	result, err := store.GetRuntimeObservations(r.Context(), subject, id, requestRef, profile, limit, cursor, now)
	if err != nil {
		writeWorkbenchFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
