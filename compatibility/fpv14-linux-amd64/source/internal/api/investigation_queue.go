package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	productaccess "github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/workspace"
)

func (s *Server) queueStore(w http.ResponseWriter, r *http.Request) (workspace.QueueStore, bool) {
	if !s.requireHumanPermission(w, r, productaccess.PermissionEvidenceRead) {
		return nil, false
	}
	store, ok := s.workspaceStore.(workspace.QueueStore)
	if !ok {
		writeProblem(w, http.StatusServiceUnavailable, "workspace_unavailable", "Investigation queue is unavailable", true)
	}
	return store, ok
}
func (s *Server) handleInvestigationQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowedFor(w, http.MethodGet)
		return
	}
	store, ok := s.queueStore(w, r)
	if !ok {
		return
	}
	if failure := validateQueryKeys(r, "mode", "period", "from", "to", "assignee", "progress", "conclusion", "basis", "case_status", "search", "sort", "limit", "cursor"); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	q := r.URL.Query()
	f := workspace.QueueFilters{Mode: q.Get("mode"), Period: q.Get("period"), Assignee: q.Get("assignee"), Progress: q.Get("progress"), Conclusion: q.Get("conclusion"), Basis: q.Get("basis"), CaseStatus: q.Get("case_status"), Search: q.Get("search"), Sort: q.Get("sort")}
	for key, target := range map[string]**time.Time{"from": &f.From, "to": &f.To} {
		if q.Has(key) {
			v, e := time.Parse(time.RFC3339Nano, q.Get(key))
			if e != nil {
				writeWorkspaceFailure(w, workspace.ErrInvalid)
				return
			}
			*target = &v
		}
	}
	limit, err := strconv.Atoi(defaultString(q.Get("limit"), "50"))
	if err != nil {
		writeWorkspaceFailure(w, workspace.ErrInvalid)
		return
	}
	auth, _ := accessAuthentication(r)
	permissions, _ := json.Marshal(productaccess.Permissions(auth.Session.Role))
	access := workspace.QueueAccess{Subject: auth.Session.SubjectID, Revision: string(auth.Session.Role) + ":" + string(permissions), AssigneeStates: map[string]string{}}
	if s.access != nil {
		subjects, _ := s.access.LocalSubjects()
		for _, subject := range subjects {
			access.AssigneeStates[subject.SubjectID] = subject.State
		}
	}
	page, err := store.ListInvestigationQueue(r.Context(), access, workspace.QueueRequest{Filters: f, Limit: limit, Cursor: q.Get("cursor")}, s.now().UTC())
	if err != nil {
		writeWorkspaceFailure(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, page)
}
func (s *Server) handleQueueFilters(w http.ResponseWriter, r *http.Request) {
	store, ok := s.queueStore(w, r)
	if !ok {
		return
	}
	if !s.requireHumanPermission(w, r, productaccess.PermissionInvestigationPreferences) {
		return
	}
	if failure := validateQueryKeys(r); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	collection := r.URL.Path == "/api/v1/investigation/queue-filters"
	id := ""
	if !collection {
		id = strings.TrimPrefix(r.URL.Path, "/api/v1/investigation/queue-filters/")
		if !workspace.ValidQueueFilterID(id) {
			writeWorkspaceFailure(w, workspace.ErrNotFound)
			return
		}
	}
	auth, _ := accessAuthentication(r)
	if collection && r.Method == http.MethodGet {
		items, err := store.ListQueueFilters(r.Context(), auth.Session.SubjectID)
		if err != nil {
			writeWorkspaceFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	kind := ""
	status := http.StatusOK
	if collection && r.Method == http.MethodPost {
		kind = "create"
		status = http.StatusCreated
	} else if !collection && r.Method == http.MethodPut {
		kind = "update"
	} else if !collection && r.Method == http.MethodDelete {
		kind = "delete"
	} else {
		if collection {
			methodNotAllowedFor(w, http.MethodGet, http.MethodPost)
		} else {
			methodNotAllowedFor(w, http.MethodPut, http.MethodDelete)
		}
		return
	}
	if !s.requireHumanMutationAuthorization(w, r) {
		return
	}
	var command workspace.QueueFilterCommand
	if err := decodeWorkspaceRequest(w, r, &command); err != nil {
		writeWorkspaceFailure(w, workspace.ErrInvalid)
		return
	}
	item, err := store.ApplyQueueFilter(r.Context(), auth.Session.SubjectID, id, kind, command, s.now().UTC())
	if err != nil {
		writeWorkspaceFailure(w, err)
		return
	}
	writeJSON(w, status, item)
}
