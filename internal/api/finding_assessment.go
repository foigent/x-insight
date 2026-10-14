package api

import (
	"net/http"
	"strconv"
	"strings"

	productaccess "github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/workspace"
)

const maxAssessmentBodyBytes = 64 << 10

func (s *Server) assessmentStore(w http.ResponseWriter) (workspace.AssessmentStore, bool) {
	store, ok := s.workspaceStore.(workspace.AssessmentStore)
	if !ok {
		writeProblem(w, http.StatusServiceUnavailable, "workspace_unavailable", "Finding assessment is unavailable", true)
	}
	return store, ok
}

func (s *Server) handleFindingAssessmentPath(w http.ResponseWriter, r *http.Request, caseID, findingID string, parts []string) {
	if !findingIDPattern.MatchString(findingID) {
		writeWorkspaceFailure(w, workspace.ErrNotFound)
		return
	}
	// Explicit on every new route, including read-only POST previews. The
	// /security collection below must not rely on Case-prefix middleware.
	if !s.requireHumanPermission(w, r, productaccess.PermissionEvidenceRead) {
		return
	}
	store, ok := s.assessmentStore(w)
	if !ok {
		return
	}
	path := strings.Join(parts, "/")
	switch path {
	case "assessment":
		if r.Method != http.MethodGet {
			methodNotAllowedFor(w, http.MethodGet)
			return
		}
		if failure := validateQueryKeys(r); failure != nil {
			writeRequestFailure(w, failure)
			return
		}
		v, err := store.GetFindingAssessment(r.Context(), caseID, findingID, s.now().UTC())
		if err != nil {
			writeWorkspaceFailure(w, err)
			return
		}
		v.AllowedActions = []string{}
		if canWriteInvestigation(r) {
			if v.CaseStatus == "open" {
				v.AllowedActions = []string{"record_review", "record_progress"}
			} else if v.CaseStatus == "sealed" {
				v.AllowedActions = []string{"continue_finding"}
			}
		}
		writeJSON(w, http.StatusOK, v)
	case "assessment/history":
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
		var e2, e3 error
		limit := 20
		if r.URL.Query().Has("before_version") {
			before, e2 = strconv.ParseInt(r.URL.Query().Get("before_version"), 10, 64)
		}
		if r.URL.Query().Has("limit") {
			limit, e3 = strconv.Atoi(r.URL.Query().Get("limit"))
		}
		if e1 != nil || e2 != nil || e3 != nil {
			writeWorkspaceFailure(w, workspace.ErrInvalid)
			return
		}
		h, err := store.GetAssessmentHistory(r.Context(), caseID, findingID, through, before, limit)
		if err != nil {
			writeWorkspaceFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, h)
	case "assessment/preview":
		if !s.requireAssessmentPost(w, r, false) {
			return
		}
		var selection workspace.BasisSelection
		if decodeBoundedWorkspaceRequest(w, r, &selection, maxAssessmentBodyBytes) != nil {
			writeWorkspaceFailure(w, workspace.ErrInvalid)
			return
		}
		basis, err := store.PreviewFindingBasis(r.Context(), caseID, findingID, selection)
		if err != nil {
			writeWorkspaceFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, basis)
	case "assessment-actions":
		if !s.requireAssessmentPost(w, r, true) {
			return
		}
		actor, ok := s.requireHumanWorkspaceWrite(w, r)
		if !ok {
			return
		}
		var command workspace.AssessmentCommand
		if decodeBoundedWorkspaceRequest(w, r, &command, maxAssessmentBodyBytes) != nil {
			writeWorkspaceFailure(w, workspace.ErrInvalid)
			return
		}
		receipt, err := store.ApplyFindingAssessment(r.Context(), caseID, findingID, actor, command, s.now().UTC())
		if err != nil {
			writeWorkspaceFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, receipt)
	case "continuations", "continuations/preview":
		s.handleReviewCreationCommand(w, r, store, path == "continuations/preview", &workspace.FindingReference{CaseID: caseID, FindingID: findingID})
	default:
		writeProblem(w, http.StatusNotFound, "route_not_found", "Route not found", false)
	}
}

func (s *Server) requireAssessmentPost(w http.ResponseWriter, r *http.Request, write bool) bool {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return false
	}
	if failure := validateQueryKeys(r); failure != nil {
		writeRequestFailure(w, failure)
		return false
	}
	if !s.requireHumanPermission(w, r, productaccess.PermissionEvidenceRead) {
		return false
	}
	if write && !s.requireHumanPermission(w, r, productaccess.PermissionInvestigationWrite) {
		return false
	}
	return s.requireHumanMutationAuthorization(w, r)
}

func (s *Server) handleReviewCreation(w http.ResponseWriter, r *http.Request) {
	if !s.requireHumanPermission(w, r, productaccess.PermissionEvidenceRead) {
		return
	}
	store, ok := s.assessmentStore(w)
	if !ok {
		return
	}
	s.handleReviewCreationCommand(w, r, store, r.URL.Path == "/api/v1/security/reviews/preview", nil)
}

func (s *Server) handleReviewCreationCommand(w http.ResponseWriter, r *http.Request, store workspace.AssessmentStore, preview bool, source *workspace.FindingReference) {
	if !s.requireAssessmentPost(w, r, !preview) {
		return
	}
	var command workspace.ReviewCreationCommand
	if decodeBoundedWorkspaceRequest(w, r, &command, maxAssessmentBodyBytes) != nil {
		writeWorkspaceFailure(w, workspace.ErrInvalid)
		return
	}
	if source == nil {
		if command.Operation != "start_review" || command.From != nil {
			writeWorkspaceFailure(w, workspace.ErrInvalid)
			return
		}
	} else if command.Operation != "continue_finding" || command.From == nil || command.From.FindingReference != *source {
		writeWorkspaceFailure(w, workspace.ErrInvalid)
		return
	}
	if preview {
		basis, err := store.PreviewReviewCreation(r.Context(), command)
		if err != nil {
			writeWorkspaceFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, basis)
		return
	}
	actor, ok := s.requireHumanWorkspaceWrite(w, r)
	if !ok {
		return
	}
	receipt, err := store.CreateReview(r.Context(), actor, command, s.now().UTC(), nil)
	if err != nil {
		writeWorkspaceFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, receipt)
}
