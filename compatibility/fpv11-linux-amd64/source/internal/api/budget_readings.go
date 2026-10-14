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

type BudgetReadingStore interface {
	CreateBudgetReading(context.Context, string, workbench.BudgetReadingRequest, time.Time) (workbench.BudgetReading, error)
	GetBudgetReading(context.Context, string, string, time.Time) (workbench.BudgetReading, error)
	GetBudgetItems(context.Context, string, string, int, string, time.Time) (workbench.BudgetItems, error)
	GetBudgetHistory(context.Context, string, string, string, int, string, time.Time) (workbench.BudgetItems, error)
}

func (s *Server) handleBudgetReadings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
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
	store, ok := s.workbenchStore.(BudgetReadingStore)
	if !ok {
		writeProblem(w, 503, "budget_reading_unavailable", "Budget reading is unavailable", true)
		return
	}
	var input workbench.BudgetReadingRequest
	if err := decodeWorkspaceRequest(w, r, &input); err != nil {
		writeWorkbenchFailure(w, workbench.ErrInvalid)
		return
	}
	result, err := store.CreateBudgetReading(r.Context(), auth.Session.SubjectID, input, s.now().UTC())
	if err != nil {
		writeWorkbenchFailure(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) handleBudgetReadingPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowedFor(w, http.MethodGet)
		return
	}
	auth, ok := accessAuthentication(r)
	if !ok {
		writeProblem(w, 401, "authentication_required", "Authentication is required", false)
		return
	}
	store, ok := s.workbenchStore.(BudgetReadingStore)
	if !ok {
		writeProblem(w, 503, "budget_reading_unavailable", "Budget reading is unavailable", true)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/workbench/budgets/readings/"), "/")
	if len(parts) < 1 || !workbench.ValidBudgetReadingID(parts[0]) {
		writeWorkbenchFailure(w, workbench.ErrNotFound)
		return
	}
	if len(parts) == 1 {
		if failure := validateQueryKeys(r); failure != nil {
			writeRequestFailure(w, failure)
			return
		}
		result, err := store.GetBudgetReading(r.Context(), auth.Session.SubjectID, parts[0], s.now().UTC())
		if err != nil {
			writeWorkbenchFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	history := len(parts) == 4 && parts[1] == "items" && workbench.ValidBudgetItemRef(parts[2]) && parts[3] == "history"
	if len(parts) == 2 && parts[1] == "items" || history {
		if failure := validateQueryKeys(r, "limit", "cursor"); failure != nil {
			writeRequestFailure(w, failure)
			return
		}
		limit := 25
		if raw := r.URL.Query().Get("limit"); raw != "" {
			var err error
			limit, err = strconv.Atoi(raw)
			if err != nil {
				writeWorkbenchFailure(w, workbench.ErrInvalid)
				return
			}
		}
		var result workbench.BudgetItems
		var err error
		if history {
			result, err = store.GetBudgetHistory(r.Context(), auth.Session.SubjectID, parts[0], parts[2], limit, r.URL.Query().Get("cursor"), s.now().UTC())
		} else {
			result, err = store.GetBudgetItems(r.Context(), auth.Session.SubjectID, parts[0], limit, r.URL.Query().Get("cursor"), s.now().UTC())
		}
		if err != nil {
			writeWorkbenchFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	writeWorkbenchFailure(w, workbench.ErrNotFound)
}
