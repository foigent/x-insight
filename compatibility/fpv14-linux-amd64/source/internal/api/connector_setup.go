package api

import (
	"context"
	"errors"
	productaccess "github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/reviewbody"
	"net/http"
	"strings"
	"time"
)

const setupPrefix = "/api/v1/connector-setups"

type setupResolver interface {
	ResolveSetupInvitation(context.Context, string, time.Time) (connectorcontrol.SetupCreated, error)
}

func (s *Server) handleConnectorSetup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if failure := validateQueryKeys(r); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	if r.URL.Path == setupPrefix+"/resolve" {
		if r.Method != http.MethodPost {
			methodNotAllowedFor(w, http.MethodPost)
			return
		}
		if !s.transportSecurity.isHTTPS(r) || len(r.Header.Values("Authorization")) != 0 || len(r.Cookies()) != 0 {
			writeProblem(w, http.StatusForbidden, "connector_transport_rejected", "Connector transport is rejected", false)
			return
		}
		store, ok := s.connectorStore.(setupResolver)
		if !ok || !s.connectorConfig.Enabled() {
			writeConnectorControlFailure(w, errors.New("setup unavailable"))
			return
		}
		var input struct {
			InvitationToken string `json:"invitation_token"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if decodeWorkspaceRequest(w, r, &input) != nil {
			writeConnectorControlFailure(w, connectorcontrol.ErrInvalid)
			return
		}
		consent, err := store.ResolveSetupInvitation(r.Context(), input.InvitationToken, s.now().UTC())
		if err != nil {
			writeSetupFailure(w, err)
			return
		}
		if !connectorcontrol.ValidSetupResponse(connectorcontrol.SetupLocalRequest{Action: "create"}, connectorcontrol.SetupLocalResponse{SchemaVersion: connectorcontrol.LocalSetupResponseSchema, Action: "create", Created: &consent}) {
			writeSetupFailure(w, errors.New("invalid saved consent"))
			return
		}
		writeJSON(w, http.StatusOK, consent)
		return
	}
	if s.setupController == nil || !s.connectorConfig.Enabled() {
		writeConnectorControlFailure(w, errors.New("setup unavailable"))
		return
	}
	request := connectorcontrol.SetupLocalRequest{SchemaVersion: connectorcontrol.LocalSetupRequestSchema}
	if strings.HasPrefix(r.URL.Path, "/api/v1/connector-control/installations/") {
		if r.Method != http.MethodGet {
			methodNotAllowedFor(w, http.MethodGet)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/api/v1/connector-control/installations/")
		parts := strings.Split(rest, "/")
		auth, ok := serviceAuthentication(r)
		if !ok || len(parts) != 2 || parts[1] != "setup" || !connectorcontrol.IsInstallationID(parts[0]) {
			writeSetupFailure(w, connectorcontrol.ErrSetupUnauthorized)
			return
		}
		request.Action = "status"
		request.InstallationID = parts[0]
		request.ControlIdentityID = auth.Identity.ID
	} else {
		if !s.requireHumanPermission(w, r, productaccess.PermissionConnectorInstallationsManage) {
			return
		}
		auth, _ := accessAuthentication(r)
		request.ActorSubject = auth.Session.SubjectID
		switch {
		case r.URL.Path == setupPrefix+"/preview":
			if r.Method != http.MethodGet {
				methodNotAllowedFor(w, http.MethodGet)
				return
			}
			request.Action = "preview"
		case r.URL.Path == setupPrefix:
			if r.Method != http.MethodPost {
				methodNotAllowedFor(w, http.MethodPost)
				return
			}
			if !s.requireHumanMutationAuthorization(w, r) {
				return
			}
			var put connectorcontrol.SetupConsentPut
			if decodeWorkspaceRequest(w, r, &put) != nil || !put.Valid() {
				writeSetupFailure(w, connectorcontrol.ErrInvalid)
				return
			}
			// Current roles have no separate policy-read permission. Explicitly
			// confirming existing custody values also requires that permission.
			if !s.requireHumanPermission(w, r, productaccess.PermissionReviewPolicyManage) {
				return
			}
			request.Action = "create"
			request.Consent = &put
		default:
			if r.Method != http.MethodGet {
				methodNotAllowedFor(w, http.MethodGet)
				return
			}
			id := strings.TrimPrefix(r.URL.Path, setupPrefix+"/")
			if !connectorcontrol.IsInstallationID(id) {
				writeSetupFailure(w, connectorcontrol.ErrNotFound)
				return
			}
			request.Action = "status"
			request.InstallationID = id
		}
	}
	response, err := s.setupController.ExecuteSetup(r.Context(), request)
	if err != nil {
		writeSetupFailure(w, err)
		return
	}
	if !connectorcontrol.ValidSetupResponse(request, response) || response.Error != "" {
		writeSetupFailure(w, errors.New("invalid setup response"))
		return
	}
	switch request.Action {
	case "preview":
		writeJSON(w, http.StatusOK, response.Preview)
	case "create":
		writeJSON(w, http.StatusCreated, response.Created)
	case "status":
		writeJSON(w, http.StatusOK, response.Status)
	}
}
func writeSetupFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, connectorcontrol.ErrSetupUnauthorized):
		writeProblem(w, http.StatusForbidden, "permission_denied", "XI setup permission is denied", false)
	case errors.Is(err, reviewbody.ErrConflict), errors.Is(err, reviewbody.ErrPolicy):
		writeConnectorControlFailure(w, connectorcontrol.ErrConflict)
	default:
		writeConnectorControlFailure(w, err)
	}
}
