package api

import (
	"net/http"
	"strconv"

	productaccess "github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/workchain"
	"github.com/foigent/x-insight/internal/workspace"
)

func (s *Server) findingStore(w http.ResponseWriter) (workspace.FollowupStore, bool) {
	store, ok := s.workspaceStore.(workspace.FollowupStore)
	if !ok {
		writeProblem(w, http.StatusServiceUnavailable, "workspace_unavailable", "Finding responsibility is unavailable", true)
	}
	return store, ok
}

func (s *Server) handleFindingReviewPath(w http.ResponseWriter, r *http.Request, caseID, findingID string, parts []string) {
	if !findingIDPattern.MatchString(findingID) {
		writeWorkspaceFailure(w, workspace.ErrNotFound)
		return
	}
	if !s.requireHumanPermission(w, r, productaccess.PermissionEvidenceRead) {
		return
	}
	store, ok := s.findingStore(w)
	if !ok {
		return
	}
	if len(parts) == 1 && parts[0] == "review" {
		if r.Method != http.MethodGet {
			methodNotAllowedFor(w, http.MethodGet)
			return
		}
		if failure := validateQueryKeys(r); failure != nil {
			writeRequestFailure(w, failure)
			return
		}
		review, err := store.GetFindingReview(r.Context(), caseID, findingID)
		if err != nil {
			writeWorkspaceFailure(w, err)
			return
		}
		if review.CaseStatus == "open" && canWriteInvestigation(r) {
			review.AllowedActions = []string{"update_followup"}
		}
		review.AssigneeState = s.currentAssigneeState(review.Followup)
		review.EvaluatedAt = s.now().UTC()
		writeJSON(w, http.StatusOK, review)
		return
	}
	if len(parts) == 2 && parts[0] == "review" && parts[1] == "history" {
		if r.Method != http.MethodGet {
			methodNotAllowedFor(w, http.MethodGet)
			return
		}
		if failure := validateQueryKeys(r, "through_version", "before_version", "limit"); failure != nil {
			writeRequestFailure(w, failure)
			return
		}
		through, e1 := strconv.ParseInt(r.URL.Query().Get("through_version"), 10, 64)
		before := int64(0)
		var e2 error
		if r.URL.Query().Has("before_version") {
			before, e2 = strconv.ParseInt(r.URL.Query().Get("before_version"), 10, 64)
		}
		limit := 20
		var e3 error
		if r.URL.Query().Has("limit") {
			limit, e3 = strconv.Atoi(r.URL.Query().Get("limit"))
		}
		if e1 != nil || e2 != nil || e3 != nil {
			writeWorkspaceFailure(w, workspace.ErrInvalid)
			return
		}
		history, err := store.GetFindingHistory(r.Context(), caseID, findingID, through, before, limit)
		if err != nil {
			writeWorkspaceFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, history)
		return
	}
	if len(parts) == 1 && parts[0] == "review-actions" {
		if r.Method != http.MethodPost {
			methodNotAllowedFor(w, http.MethodPost)
			return
		}
		actor, ok := s.requireHumanWorkspaceWrite(w, r)
		if !ok {
			return
		}
		if failure := validateQueryKeys(r); failure != nil {
			writeRequestFailure(w, failure)
			return
		}
		var command workspace.FollowupCommand
		if err := decodeWorkspaceRequest(w, r, &command); err != nil {
			writeWorkspaceFailure(w, workspace.ErrInvalid)
			return
		}
		var lookup workspace.AssigneeLookup
		if command.Payload.AssigneeSubjectID != nil && s.access != nil {
			items, _ := s.access.LocalSubjects()
			for _, item := range items {
				if item.SubjectID == *command.Payload.AssigneeSubjectID {
					lookup = workspace.AssigneeLookup{SubjectID: item.SubjectID, Name: item.DisplayName, Active: item.State == "active"}
					if workchain.FromContext(r.Context()) != nil {
						scoped, ok := s.workspaceStore.(*db.WorkspaceStore)
						if !ok {
							writeWorkspaceFailure(w, workspace.ErrNotFound)
							return
						}
						allowed, err := scoped.ScopedCaseCandidate(r.Context(), caseID, item.SubjectID)
						if err != nil {
							writeWorkspaceFailure(w, err)
							return
						}
						lookup.Active = (allowed || s.access.LocalSubjectHasGlobalReview(item.SubjectID)) && s.access.LocalSubjectCanReview(item.SubjectID)
					}
					break
				}
			}
		}
		receipt, err := store.ApplyFindingFollowup(r.Context(), caseID, findingID, actor, command, lookup, s.now().UTC())
		if err != nil {
			writeWorkspaceFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, receipt)
		return
	}
	writeProblem(w, http.StatusNotFound, "route_not_found", "Route not found", false)
}

func (s *Server) currentAssigneeState(f workspace.Followup) string {
	if !f.Recorded {
		return "not_recorded"
	}
	if f.AssigneeSubjectID == nil {
		return "unassigned"
	}
	if s.access == nil {
		return "unavailable"
	}
	items, ok := s.access.LocalSubjects()
	if !ok {
		return "unavailable"
	}
	for _, item := range items {
		if item.SubjectID == *f.AssigneeSubjectID {
			return item.State
		}
	}
	return "not_found"
}

func (s *Server) handleAssigneeCandidates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowedFor(w, http.MethodGet)
		return
	}
	if !s.requireHumanPermission(w, r, productaccess.PermissionEvidenceRead) || !s.requireHumanPermission(w, r, productaccess.PermissionInvestigationWrite) {
		return
	}
	if failure := validateQueryKeys(r, "case_id", "finding_id"); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	if !caseIDPattern.MatchString(r.URL.Query().Get("case_id")) || !findingIDPattern.MatchString(r.URL.Query().Get("finding_id")) {
		writeWorkspaceFailure(w, workspace.ErrInvalid)
		return
	}
	store, ok := s.findingStore(w)
	if !ok {
		return
	}
	if _, err := store.GetFindingReview(r.Context(), r.URL.Query().Get("case_id"), r.URL.Query().Get("finding_id")); err != nil {
		writeWorkspaceFailure(w, err)
		return
	}
	items, ok := s.access.LocalSubjects()
	if !ok {
		writeProblem(w, http.StatusServiceUnavailable, "directory_unavailable", "Local account directory is unavailable", false)
		return
	}
	if workchain.FromContext(r.Context()) != nil {
		scoped, ok := s.workspaceStore.(*db.WorkspaceStore)
		if !ok {
			writeWorkspaceFailure(w, workspace.ErrNotFound)
			return
		}
		visible := []productaccess.LocalSubject{}
		for _, item := range items {
			if !s.access.LocalSubjectCanReview(item.SubjectID) {
				continue
			}
			allowed, err := scoped.ScopedCaseCandidate(r.Context(), r.URL.Query().Get("case_id"), item.SubjectID)
			if err != nil {
				writeWorkspaceFailure(w, err)
				return
			}
			if allowed || s.access.LocalSubjectHasGlobalReview(item.SubjectID) {
				visible = append(visible, item)
			}
		}
		items = visible
	}
	writeJSON(w, http.StatusOK, struct {
		Items    []productaccess.LocalSubject `json:"items"`
		Total    int                          `json:"total"`
		Complete bool                         `json:"complete"`
		Basis    string                       `json:"basis"`
	}{items, len(items), true, "loaded_local_account_configuration"})
}
