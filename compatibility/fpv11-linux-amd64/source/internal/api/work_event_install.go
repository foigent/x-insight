package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	productaccess "github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/workevent"
	"github.com/foigent/x-insight/internal/workinstallcontrol"
)

const workInstallPrefix = "/api/v1/work-event-install/"

func readWorkInstallJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	if !requireJSONContentType(r) {
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	raw, err := io.ReadAll(r.Body)
	if err != nil || len(raw) == 0 || len(raw) >= 16<<10 {
		return false
	}
	if _, err = connector.DecodeStrictJSONObject(raw); err != nil {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(destination) != nil || decoder.Decode(new(any)) != io.EOF {
		return false
	}
	return true
}

func (s *Server) handleWorkEventInstall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.workInstallController == nil {
		writeProblem(w, http.StatusServiceUnavailable, "work_install_unavailable", "Work event installation is unavailable", true)
		return
	}
	if failure := validateQueryKeys(r, "after"); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, workInstallPrefix)
	if r.URL.RawQuery != "" && !(path == "invitations" && r.Method == http.MethodGet) {
		writeProblem(w, http.StatusBadRequest, "invalid_work_install", "Work installation request is invalid", false)
		return
	}
	command := workinstallcontrol.Request{SchemaVersion: workinstallcontrol.RequestSchema, RequestedAt: s.now().UTC()}
	if path == "renew" || path == "future-candidates" || path == "status" {
		if r.Method != http.MethodPost {
			methodNotAllowedFor(w, http.MethodPost)
			return
		}
		if !s.transportSecurity.isHTTPS(r) || len(r.Cookies()) != 0 {
			writeProblem(w, http.StatusForbidden, "work_install_transport_rejected", "Work installation transport is rejected", false)
			return
		}
		auth, ok := serviceAuthentication(r)
		if !ok || len(auth.Identity.Permissions) != 1 || auth.Identity.Permissions[0] != "work_event.ingest" {
			writeProblem(w, http.StatusForbidden, "agent_scope_denied", "Agent scope does not permit this route", false)
			return
		}
		command.TokenDigest, command.IdentityID = auth.TokenDigest, auth.Identity.ID
		if path == "renew" {
			var input struct {
				ReplacementToken string `json:"replacement_token"`
			}
			if !readWorkInstallJSON(w, r, &input) {
				writeProblem(w, 400, "invalid_work_install", "Work installation request is invalid", false)
				return
			}
			command.Action, command.ReplacementToken = workinstallcontrol.ActionRenew, input.ReplacementToken
		} else if path == "future-candidates" {
			var input struct {
				Projects []db.WorkInstallProject `json:"projects"`
			}
			if !readWorkInstallJSON(w, r, &input) {
				writeProblem(w, 400, "invalid_work_install", "Work installation request is invalid", false)
				return
			}
			command.Action, command.Projects = workinstallcontrol.ActionFutureCandidates, input.Projects
		} else {
			var input struct {
				Status *db.WorkInstallStatus `json:"status"`
			}
			if !readWorkInstallJSON(w, r, &input) || !db.ValidWorkInstallStatus(input.Status) {
				writeProblem(w, 400, "invalid_work_install", "Work installation request is invalid", false)
				return
			}
			command.Action, command.Status = workinstallcontrol.ActionStatus, input.Status
		}
		result, err := s.workInstallController.Execute(r.Context(), command)
		if err != nil {
			writeWorkInstallFailure(w, err)
			return
		}
		if result.State == "conflict" {
			writeProblem(w, http.StatusConflict, "work_install_conflict", "Work installation identity or capacity conflicts", false)
			return
		}
		if result.State == "scope_changed" {
			writeProblem(w, http.StatusForbidden, "work_install_scope_changed", "Work installation scope is unavailable", false)
			return
		}
		if path == "status" {
			writeJSON(w, http.StatusOK, struct {
				SchemaVersion string `json:"schema_version"`
				Action        string `json:"action"`
				State         string `json:"state"`
				InvitationID  string `json:"invitation_id"`
			}{result.SchemaVersion, result.Action, result.State, result.InvitationID})
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	hostAction := path == "candidates" || path == "pickup"
	if hostAction {
		if r.Method != http.MethodPost {
			methodNotAllowedFor(w, http.MethodPost)
			return
		}
		if !s.transportSecurity.isHTTPS(r) || len(r.Header.Values("Authorization")) != 0 || len(r.Cookies()) != 0 {
			writeProblem(w, http.StatusForbidden, "work_install_transport_rejected", "Work installation transport is rejected", false)
			return
		}
		if path == "candidates" {
			var input struct {
				InvitationToken string                  `json:"invitation_token"`
				IngestToken     string                  `json:"ingest_token"`
				Projects        []db.WorkInstallProject `json:"projects"`
			}
			if !readWorkInstallJSON(w, r, &input) {
				writeProblem(w, 400, "invalid_work_install", "Work installation request is invalid", false)
				return
			}
			command.Action, command.InvitationToken, command.IngestToken, command.Projects =
				workinstallcontrol.ActionCandidates, input.InvitationToken, input.IngestToken, input.Projects
		} else {
			var input struct {
				InvitationToken string `json:"invitation_token"`
				IngestToken     string `json:"ingest_token"`
			}
			if !readWorkInstallJSON(w, r, &input) {
				writeProblem(w, 400, "invalid_work_install", "Work installation request is invalid", false)
				return
			}
			command.Action, command.InvitationToken, command.IngestToken = workinstallcontrol.ActionPickup, input.InvitationToken, input.IngestToken
		}
	} else {
		auth, ok := accessAuthentication(r)
		if !ok || !productaccess.HasPermission(auth.Session.Role, productaccess.PermissionConnectorInstallationsManage) ||
			!productaccess.HasPermission(auth.Session.Role, productaccess.PermissionReviewPolicyManage) {
			writeProblem(w, http.StatusForbidden, "permission_denied", "Permission is denied", false)
			return
		}
		command.ActorSubject = auth.Session.SubjectID
		if r.Method == http.MethodPost && (!s.transportSecurity.requireSameOriginHTTPS(r) ||
			!s.access.VerifyCSRF(auth, r.Header.Get(csrfHeaderName))) {
			writeProblem(w, http.StatusForbidden, "csrf_validation_failed", "Request authorization failed", false)
			return
		}
		switch {
		case path == "invitations" && r.Method == http.MethodGet:
			command.Action, command.ListAfter = workinstallcontrol.ActionList, r.URL.Query().Get("after")
		case path == "invitations" && r.Method == http.MethodPost:
			var input struct {
				DisplayName      string `json:"display_name"`
				ExpiresInSeconds int64  `json:"expires_in_seconds"`
			}
			if !readWorkInstallJSON(w, r, &input) {
				writeProblem(w, 400, "invalid_work_install", "Work installation request is invalid", false)
				return
			}
			command.Action, command.DisplayName, command.ExpiresInSeconds = workinstallcontrol.ActionIssue, input.DisplayName, input.ExpiresInSeconds
		case strings.HasPrefix(path, "invitations/") && r.Method == http.MethodGet && !strings.Contains(strings.TrimPrefix(path, "invitations/"), "/"):
			command.Action, command.InvitationID = workinstallcontrol.ActionRead, strings.TrimPrefix(path, "invitations/")
		case strings.HasPrefix(path, "invitations/") && strings.HasSuffix(path, "/approve") && r.Method == http.MethodPost:
			command.InvitationID = strings.TrimSuffix(strings.TrimPrefix(path, "invitations/"), "/approve")
			if strings.Contains(command.InvitationID, "/") {
				writeProblem(w, 404, "not_found", "Route was not found", false)
				return
			}
			var input struct {
				Mappings            []db.WorkInstallMapping `json:"mappings"`
				ExcludedProjectKeys []string                `json:"excluded_project_keys"`
				FutureAgentsAllowed *bool                   `json:"future_agents_allowed"`
			}
			if !readWorkInstallJSON(w, r, &input) || input.FutureAgentsAllowed == nil {
				writeProblem(w, 400, "invalid_work_install", "Work installation request is invalid", false)
				return
			}
			command.Action, command.Mappings, command.ExcludedProjectKeys, command.FutureAgentsAllowed =
				workinstallcontrol.ActionApprove, input.Mappings, input.ExcludedProjectKeys, *input.FutureAgentsAllowed
		case strings.HasPrefix(path, "invitations/") && strings.HasSuffix(path, "/future-decisions") && r.Method == http.MethodPost:
			command.InvitationID = strings.TrimSuffix(strings.TrimPrefix(path, "invitations/"), "/future-decisions")
			if strings.Contains(command.InvitationID, "/") {
				writeProblem(w, 404, "not_found", "Route was not found", false)
				return
			}
			var input struct {
				Mappings            []db.WorkInstallMapping `json:"mappings"`
				ExcludedProjectKeys []string                `json:"excluded_project_keys"`
			}
			if !readWorkInstallJSON(w, r, &input) {
				writeProblem(w, 400, "invalid_work_install", "Work installation request is invalid", false)
				return
			}
			command.Action, command.Mappings, command.ExcludedProjectKeys = workinstallcontrol.ActionFutureDecisions, input.Mappings, input.ExcludedProjectKeys
		default:
			writeProblem(w, 404, "not_found", "Route was not found", false)
			return
		}
	}
	if !workinstallcontrol.ValidRequest(command) {
		writeProblem(w, 400, "invalid_work_install", "Work installation request is invalid", false)
		return
	}
	result, err := s.workInstallController.Execute(r.Context(), command)
	if err != nil {
		writeWorkInstallFailure(w, err)
		return
	}
	status := http.StatusOK
	if command.Action == workinstallcontrol.ActionIssue {
		status = http.StatusCreated
		w.Header().Set("Cache-Control", "private, no-store")
	}
	writeJSON(w, status, result)
}

func writeWorkInstallFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workevent.ErrInvalid):
		writeProblem(w, http.StatusBadRequest, "invalid_work_install", "Work installation request is invalid", false)
	case errors.Is(err, workevent.ErrConflict):
		writeProblem(w, http.StatusConflict, "work_install_conflict", "Work installation identity, mapping, or capacity conflicts", false)
	case errors.Is(err, workevent.ErrScopeChanged):
		writeProblem(w, http.StatusForbidden, "work_install_scope_changed", "Work installation scope is unavailable", false)
	default:
		writeProblem(w, http.StatusServiceUnavailable, "work_install_unconfirmed", "Work installation state is unavailable; read back before retry", true)
	}
}
