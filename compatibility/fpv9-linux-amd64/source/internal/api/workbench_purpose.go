package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	productaccess "github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/executionid"
	"github.com/foigent/x-insight/internal/workbench"
)

type purposeStore interface {
	GetWorkbenchPurpose(context.Context, string, string, string, int64, time.Time) (workbench.PurposeView, error)
	ApplyWorkbenchPurpose(context.Context, string, string, workbench.PurposeCommand, time.Time) (workbench.PurposeEvent, error)
}

func (s *Server) handleWorkbenchPurpose(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/workbench/executions/"), "/")
	if len(parts) != 2 || !executionid.Valid(parts[0]) || parts[1] != "purpose" {
		writeProblem(w, http.StatusNotFound, "not_found", "Resource not found", false)
		return
	}
	store, ok := s.workbenchStore.(purposeStore)
	if !ok {
		writeProblem(w, http.StatusServiceUnavailable, "workbench_unavailable", "Workbench is unavailable", true)
		return
	}
	if !s.requireHumanPermission(w, r, productaccess.PermissionWorkbenchSummary) {
		return
	}
	auth, _ := accessAuthentication(r)
	switch r.Method {
	case http.MethodGet:
		if failure := validateQueryKeys(r, "reading_id", "before_revision"); failure != nil {
			writeRequestFailure(w, failure)
			return
		}
		before := int64(0)
		if value := r.URL.Query().Get("before_revision"); value != "" {
			var err error
			before, err = strconv.ParseInt(value, 10, 64)
			if err != nil || before < 1 || strconv.FormatInt(before, 10) != value {
				writeWorkbenchFailure(w, workbench.ErrInvalid)
				return
			}
		}
		item, err := store.GetWorkbenchPurpose(r.Context(), auth.Session.SubjectID, parts[0], r.URL.Query().Get("reading_id"), before, s.now().UTC())
		if err != nil {
			writeWorkbenchFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodPost:
		if failure := validateQueryKeys(r); failure != nil {
			writeRequestFailure(w, failure)
			return
		}
		if !s.requireHumanPermission(w, r, productaccess.PermissionPurposeReview) || !s.requireHumanMutationAuthorization(w, r) {
			return
		}
		var command workbench.PurposeCommand
		if err := decodeWorkspaceRequest(w, r, &command); err != nil {
			writeWorkbenchFailure(w, workbench.ErrInvalid)
			return
		}
		item, err := store.ApplyWorkbenchPurpose(r.Context(), auth.Session.SubjectID, parts[0], command, s.now().UTC())
		if err != nil {
			writeWorkbenchFailure(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, item)
	default:
		methodNotAllowedFor(w, http.MethodGet, http.MethodPost)
	}
}
