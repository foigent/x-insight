package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workevent"
)

type WorkActivityStore interface {
	ListWorkEventScopes(context.Context, string) ([]db.WorkEventScopeChoice, error)
	ListWorkEvents(context.Context, string, db.WorkEventQuery, time.Time) (db.WorkEventPage, error)
	GetWorkEvent(context.Context, string, workevent.Scope, string) (db.WorkEventDetail, error)
	GetWorkActivitySummary(context.Context, string, db.WorkActivitySummaryQuery, time.Time) (db.WorkActivitySummary, error)
}

func boundedCSV(raw string) ([]string, bool) {
	if raw == "" {
		return []string{}, true
	}
	parts := strings.Split(raw, ",")
	if len(parts) > 16 {
		return nil, false
	}
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		if part == "" || strings.TrimSpace(part) != part || seen[part] {
			return nil, false
		}
		seen[part] = true
	}
	return parts, true
}

func (s *Server) handleWorkActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowedFor(w, http.MethodGet)
		return
	}
	store, ok := s.workbenchStore.(WorkActivityStore)
	if !ok {
		writeProblem(w, http.StatusServiceUnavailable, "work_activity_unavailable", "Work activity is unavailable", true)
		return
	}
	auth, ok := accessAuthentication(r)
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "authentication_required", "Authentication is required", false)
		return
	}
	if r.URL.Path == "/api/v1/work-activity/scopes" {
		if failure := validateQueryKeys(r); failure != nil {
			writeRequestFailure(w, failure)
			return
		}
		choices, err := store.ListWorkEventScopes(r.Context(), auth.Session.SubjectID)
		if err != nil {
			writeWorkbenchFailure(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]any{"version": "xinsight.work-activity-scopes.v1", "items": choices})
		return
	}
	if r.URL.Path == "/api/v1/work-activity/summary" {
		if failure := validateQueryKeys(r, "from", "to", "source_ids", "project_ids"); failure != nil {
			writeRequestFailure(w, failure)
			return
		}
		from, errFrom := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
		to, errTo := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
		sources, sourceOK := boundedCSV(r.URL.Query().Get("source_ids"))
		projects, projectOK := boundedCSV(r.URL.Query().Get("project_ids"))
		if errFrom != nil || errTo != nil || !sourceOK || !projectOK {
			writeWorkbenchFailure(w, workbench.ErrInvalid)
			return
		}
		summary, err := store.GetWorkActivitySummary(r.Context(), auth.Session.SubjectID,
			db.WorkActivitySummaryQuery{From: from.UTC(), To: to.UTC(), SourceIDs: sources, ProjectIDs: projects}, s.now().UTC())
		if err != nil {
			writeWorkbenchFailure(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, summary)
		return
	}
	if r.URL.Path == "/api/v1/work-activity" {
		if failure := validateQueryKeys(r, "source_id", "project_id", "source_ids", "project_ids", "resource_ref", "resource_name", "from", "to", "limit", "cursor"); failure != nil {
			writeRequestFailure(w, failure)
			return
		}
		from, errFrom := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
		to, errTo := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
		limit, errLimit := strconv.Atoi(defaultString(r.URL.Query().Get("limit"), "50"))
		sources, sourceOK := boundedCSV(r.URL.Query().Get("source_ids"))
		projects, projectOK := boundedCSV(r.URL.Query().Get("project_ids"))
		if errFrom != nil || errTo != nil || errLimit != nil || !sourceOK || !projectOK {
			writeWorkbenchFailure(w, workbench.ErrInvalid)
			return
		}
		page, err := store.ListWorkEvents(r.Context(), auth.Session.SubjectID, db.WorkEventQuery{
			SourceID: r.URL.Query().Get("source_id"), ProjectID: r.URL.Query().Get("project_id"),
			SourceIDs: sources, ProjectIDs: projects,
			ResourceRef: r.URL.Query().Get("resource_ref"), ResourceName: r.URL.Query().Get("resource_name"), From: from.UTC(), To: to.UTC(),
			Limit: limit, Cursor: r.URL.Query().Get("cursor"),
		}, s.now().UTC())
		if err != nil {
			writeWorkbenchFailure(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, page)
		return
	}
	if failure := validateQueryKeys(r); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/work-activity/events/"), "/")
	if len(parts) != 5 {
		writeWorkbenchFailure(w, workbench.ErrNotFound)
		return
	}
	detail, err := store.GetWorkEvent(r.Context(), auth.Session.SubjectID, workevent.Scope{
		SourceSystemID: parts[0], SourceInstance: parts[1], OrgID: parts[2], ProjectID: parts[3],
	}, parts[4])
	if err != nil {
		writeWorkbenchFailure(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, detail)
}
