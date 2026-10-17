package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/workbench"
)

type RuntimeCurrentStore interface {
	GetRuntimeCurrent(context.Context, string, workbench.Filters, int, string, time.Time) (workbench.RuntimeCurrentInventory, error)
}

func (s *Server) handleRuntimeCurrent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowedFor(w, http.MethodGet)
		return
	}
	store, ok := s.workbenchStore.(RuntimeCurrentStore)
	if !ok {
		writeProblem(w, http.StatusServiceUnavailable, "runtime_current_unavailable", "Current runtime is unavailable", true)
		return
	}
	auth, ok := accessAuthentication(r)
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "authentication_required", "Authentication is required", false)
		return
	}
	if failure := validateQueryKeys(r, "source_ids", "source_projects", "cost_projects", "limit", "cursor"); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	split := func(key string) []string {
		v := r.URL.Query().Get(key)
		if v == "" {
			return nil
		}
		return strings.Split(v, ",")
	}
	filters := workbench.Filters{SourceIDs: split("source_ids"), SourceProjects: split("source_projects"), CostProjects: split("cost_projects")}
	limit, err := strconv.Atoi(defaultString(r.URL.Query().Get("limit"), "50"))
	if err != nil {
		writeWorkbenchFailure(w, workbench.ErrInvalid)
		return
	}
	result, err := store.GetRuntimeCurrent(r.Context(), auth.Session.SubjectID, filters, limit, r.URL.Query().Get("cursor"), s.now().UTC())
	if err != nil {
		writeWorkbenchFailure(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, result)
}
