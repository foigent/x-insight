package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	productaccess "github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/urmachine"
	"github.com/foigent/x-insight/internal/reviewpolicycontrol"
)

const reviewURPrefix = "/api/v1/review-capture/unirouter/"

var reviewURPositiveID = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
var reviewURDelegationID = regexp.MustCompile(`^xipg_[1-9][0-9]{0,18}$`)
var reviewUROperationRef = regexp.MustCompile(`^xipw_[0-9a-f]{32}$`)

type reviewURController interface {
	URPolicy(context.Context, reviewpolicycontrol.Request) (reviewpolicycontrol.Response, error)
}

func (s *Server) handleReviewCaptureURPolicy(w http.ResponseWriter, r *http.Request) {
	auth, ok := accessAuthentication(r)
	if !ok || !productaccess.HasPermission(auth.Session.Role, productaccess.PermissionReviewPolicyManage) {
		writeProblem(w, http.StatusForbidden, "permission_denied", "Permission is denied", false)
		return
	}
	controller, ok := s.reviewPolicyController.(reviewURController)
	if !ok || controller == nil {
		writeProblem(w, http.StatusServiceUnavailable, "ur_policy_unavailable", "Delegated UniRouter policy is unavailable", true)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	path := strings.TrimPrefix(r.URL.Path, reviewURPrefix)
	scopeKind, orgID, projectID, userID, scopePath := parseReviewURScope(path)
	command := reviewpolicycontrol.Request{SchemaVersion: reviewpolicycontrol.RequestSchema,
		ActorSubject: auth.Session.SubjectID, RequestedAt: s.now().UTC(), UR: &reviewpolicycontrol.URRequest{}}
	switch {
	case path == "scopes" && r.Method == http.MethodGet:
		if failure := validateQueryKeys(r, "cursor"); failure != nil {
			writeRequestFailure(w, failure)
			return
		}
		command.Action, command.UR.Cursor = reviewpolicycontrol.ActionURScopes, r.URL.Query().Get("cursor")
	case scopePath && (r.Method == http.MethodGet || r.Method == http.MethodPut):
		if !reviewURPositiveID.MatchString(orgID) && scopeKind == "enterprise" ||
			!reviewURPositiveID.MatchString(projectID) && scopeKind != "enterprise" ||
			scopeKind == "user" && !reviewURPositiveID.MatchString(userID) {
			writeProblem(w, http.StatusBadRequest, "invalid_ur_policy_scope", "UniRouter scope is invalid", false)
			return
		}
		if scopeKind == "users" {
			if r.Method != http.MethodGet {
				writeProblem(w, http.StatusNotFound, "not_found", "Route was not found", false)
				return
			}
			if failure := validateQueryKeys(r, "cursor"); failure != nil {
				writeRequestFailure(w, failure)
				return
			}
			command.Action, command.UR.ProjectID, command.UR.Cursor = reviewpolicycontrol.ActionURUsers, projectID, r.URL.Query().Get("cursor")
			break
		}
		command.UR.OrgID, command.UR.ProjectID, command.UR.UserID = orgID, projectID, userID
		if r.Method == http.MethodGet {
			if failure := validateQueryKeys(r, "delegation_id"); failure != nil {
				writeRequestFailure(w, failure)
				return
			}
			command.UR.DelegationID = r.URL.Query().Get("delegation_id")
			switch scopeKind {
			case "enterprise":
				command.Action = reviewpolicycontrol.ActionURReadEnterprise
			case "user":
				command.Action = reviewpolicycontrol.ActionURReadUser
			default:
				command.Action = reviewpolicycontrol.ActionURReadProject
			}
		} else {
			if failure := validateQueryKeys(r); failure != nil {
				writeRequestFailure(w, failure)
				return
			}
			if !s.transportSecurity.requireSameOriginHTTPS(r) || !s.access.VerifyCSRF(auth, r.Header.Get(csrfHeaderName)) ||
				!requireJSONContentType(r) {
				writeProblem(w, http.StatusForbidden, "csrf_validation_failed", "Request authorization failed", false)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 8192)
			raw, err := io.ReadAll(r.Body)
			if err != nil || len(raw) == 0 || len(raw) > 8192 {
				writeProblem(w, http.StatusBadRequest, "invalid_ur_policy", "UniRouter policy request is invalid", false)
				return
			}
			if _, err = connector.DecodeStrictJSONObject(raw); err != nil {
				writeProblem(w, http.StatusBadRequest, "invalid_ur_policy", "UniRouter policy request is invalid", false)
				return
			}
			var input struct {
				DelegationID       string                    `json:"delegation_id"`
				DelegationRevision string                    `json:"delegation_revision"`
				OperationRef       string                    `json:"operation_ref"`
				Policy             json.RawMessage           `json:"policy"`
				RetryUnconfirmed   bool                      `json:"retry_unconfirmed"`
				XISettings         *db.ReviewCaptureSettings `json:"xi_settings"`
				XIExpectedRevision int64                     `json:"xi_expected_revision"`
			}
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&input) != nil || !reviewURDelegationID.MatchString(input.DelegationID) ||
				!reviewUROperationRef.MatchString(input.OperationRef) {
				writeProblem(w, http.StatusBadRequest, "invalid_ur_policy", "UniRouter policy request is invalid", false)
				return
			}
			switch scopeKind {
			case "enterprise":
				command.Action = reviewpolicycontrol.ActionURApplyEnterprise
			case "user":
				command.Action = reviewpolicycontrol.ActionURApplyUser
			default:
				command.Action = reviewpolicycontrol.ActionURApplyProject
			}
			command.UR.DelegationID, command.UR.DelegationRevision = input.DelegationID, input.DelegationRevision
			command.UR.Policy, command.UR.OperationRef = input.Policy, input.OperationRef
			command.UR.RetryUnconfirmed = input.RetryUnconfirmed
			command.UR.XISettings, command.UR.XIExpectedRevision = input.XISettings, input.XIExpectedRevision
		}
	case strings.HasPrefix(path, "operations/") && r.Method == http.MethodGet:
		if failure := validateQueryKeys(r); failure != nil {
			writeRequestFailure(w, failure)
			return
		}
		command.Action, command.UR.OperationRef = reviewpolicycontrol.ActionURReadOperation, strings.TrimPrefix(path, "operations/")
		if !reviewUROperationRef.MatchString(command.UR.OperationRef) {
			writeProblem(w, http.StatusBadRequest, "invalid_ur_policy_operation", "UniRouter operation reference is invalid", false)
			return
		}
	default:
		writeProblem(w, http.StatusNotFound, "not_found", "Route was not found", false)
		return
	}
	if !reviewpolicycontrol.ValidRequest(command) {
		writeProblem(w, http.StatusBadRequest, "invalid_ur_policy", "UniRouter policy request is invalid", false)
		return
	}
	result, err := controller.URPolicy(r.Context(), command)
	if err != nil {
		if errors.Is(err, urmachine.ErrConflict) {
			writeProblem(w, http.StatusConflict, "ur_policy_version_conflict", "UniRouter policy version or operation identity changed", false)
			return
		}
		if errors.Is(err, urmachine.ErrContract) {
			writeProblem(w, http.StatusBadRequest, "invalid_ur_policy", "UniRouter policy request is invalid", false)
			return
		}
		// The ref exists before the privileged call. A lost socket or HTTP
		// response must be resolved by reading this exact operation, not PUT again.
		if command.Action == reviewpolicycontrol.ActionURApplyProject ||
			command.Action == reviewpolicycontrol.ActionURApplyEnterprise || command.Action == reviewpolicycontrol.ActionURApplyUser {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"code": "ur_policy_unconfirmed",
				"message": "Read the operation before attempting another write", "operation_ref": command.UR.OperationRef})
			return
		}
		writeProblem(w, http.StatusServiceUnavailable, "ur_policy_unavailable", "Delegated UniRouter policy is unavailable", true)
		return
	}
	if result.UR == nil {
		writeProblem(w, http.StatusServiceUnavailable, "ur_policy_unconfirmed", "UniRouter policy result is unconfirmed", true)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func parseReviewURScope(path string) (kind, orgID, projectID, userID string, recognized bool) {
	parts := strings.Split(path, "/")
	if len(parts) == 2 && parts[0] == "organizations" {
		return "enterprise", parts[1], "", "", true
	}
	if len(parts) == 2 && parts[0] == "projects" {
		return "project", "", parts[1], "", true
	}
	if len(parts) == 3 && parts[0] == "projects" && parts[2] == "users" {
		return "users", "", parts[1], "", true
	}
	if len(parts) == 4 && parts[0] == "projects" && parts[2] == "users" {
		return "user", "", parts[1], parts[3], true
	}
	return "", "", "", "", false
}
