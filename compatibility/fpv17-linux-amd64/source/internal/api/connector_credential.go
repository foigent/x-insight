package api

import (
	"errors"
	"net/http"
	"strings"

	productaccess "github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/connectorcontrol"
)

const credentialInstallationPrefix = "/api/v1/connector-control/installations/"

func credentialRecoverPublicPath(path string) bool {
	parts := strings.Split(strings.TrimPrefix(path, credentialInstallationPrefix), "/")
	return strings.HasPrefix(path, credentialInstallationPrefix) && len(parts) == 3 &&
		connectorcontrol.IsInstallationID(parts[0]) && parts[1] == "credentials" && parts[2] == "recover"
}

func (s *Server) handleConnectorCredentialOrSetup(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.URL.Path, "/credentials/") {
		s.handleConnectorCredential(w, r)
		return
	}
	s.handleConnectorSetup(w, r)
}

func (s *Server) executeCredential(w http.ResponseWriter, r *http.Request, request connectorcontrol.CredentialLocalRequest, status int) {
	if s.credentialController == nil || !s.connectorConfig.Enabled() {
		writeConnectorControlFailure(w, nil)
		return
	}
	response, err := s.credentialController.ExecuteCredential(r.Context(), request)
	if err != nil {
		writeCredentialFailure(w, err)
		return
	}
	if !connectorcontrol.ValidCredentialLocalResponse(request, response) || response.Error != "" {
		writeConnectorControlFailure(w, connectorcontrol.ErrInvalid)
		return
	}
	if response.Recovery != nil {
		writeJSON(w, status, response.Recovery)
	} else {
		writeJSON(w, status, response.Receipt)
	}
}

func writeCredentialFailure(w http.ResponseWriter, err error) {
	if errors.Is(err, connectorcontrol.ErrExpired) {
		writeProblem(w, http.StatusGone, "connector_credential_recovery_expired", "Recovery confirmation expired", false)
		return
	}
	writeConnectorControlFailure(w, err)
}

func (s *Server) handleConnectorCredentialRecoveryIssue(w http.ResponseWriter, r *http.Request, id string) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	if failure := validateQueryKeys(r); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	if !s.requireHumanPermission(w, r, productaccess.PermissionConnectorInstallationsManage) || !s.requireHumanMutationAuthorization(w, r) {
		return
	}
	var body struct {
		RequestID string `json:"request_id"`
	}
	if decodeBoundedWorkspaceRequest(w, r, &body, 4096) != nil || !connectorcontrol.SetupRequestIDPattern.MatchString(body.RequestID) {
		writeConnectorControlFailure(w, connectorcontrol.ErrInvalid)
		return
	}
	auth, _ := accessAuthentication(r)
	s.executeCredential(w, r, connectorcontrol.CredentialLocalRequest{SchemaVersion: connectorcontrol.LocalCredentialRequestSchema, Action: "issue", InstallationID: id, ActorSubject: auth.Session.SubjectID, RequestID: body.RequestID}, http.StatusCreated)
}

func (s *Server) handleConnectorCredentialResolve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	if failure := validateQueryKeys(r); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	if !s.transportSecurity.isHTTPS(r) || len(r.Header.Values("Authorization")) != 0 || len(r.Cookies()) != 0 {
		writeProblem(w, http.StatusForbidden, "connector_transport_rejected", "Connector transport is rejected", false)
		return
	}
	var body struct {
		Ticket string `json:"ticket"`
	}
	if decodeBoundedWorkspaceRequest(w, r, &body, 4096) != nil || !connectorcontrol.IsRecoveryTicket(body.Ticket) {
		writeConnectorControlFailure(w, connectorcontrol.ErrInvalid)
		return
	}
	s.executeCredential(w, r, connectorcontrol.CredentialLocalRequest{SchemaVersion: connectorcontrol.LocalCredentialRequestSchema, Action: "resolve", Ticket: body.Ticket}, http.StatusOK)
}

func (s *Server) handleConnectorCredential(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if failure := validateQueryKeys(r); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, credentialInstallationPrefix), "/")
	if !strings.HasPrefix(r.URL.Path, credentialInstallationPrefix) || len(parts) < 3 || !connectorcontrol.IsInstallationID(parts[0]) || parts[1] != "credentials" {
		writeConnectorControlFailure(w, connectorcontrol.ErrNotFound)
		return
	}
	request := connectorcontrol.CredentialLocalRequest{SchemaVersion: connectorcontrol.LocalCredentialRequestSchema, InstallationID: parts[0]}
	switch {
	case len(parts) == 3 && parts[2] == "rotate" && r.Method == http.MethodPost:
		request.Action = "rotate"
	case len(parts) == 3 && parts[2] == "recover" && r.Method == http.MethodPost:
		request.Action = "recover"
		if !s.transportSecurity.isHTTPS(r) || len(r.Header.Values("Authorization")) != 0 || len(r.Cookies()) != 0 {
			writeProblem(w, http.StatusForbidden, "connector_transport_rejected", "Connector transport is rejected", false)
			return
		}
	case len(parts) == 4 && parts[2] == "operations" && connectorcontrol.IsCredentialOperationID(parts[3]) && r.Method == http.MethodGet:
		request.Action = "query"
		request.OperationID = parts[3]
	case len(parts) == 5 && parts[2] == "operations" && connectorcontrol.IsCredentialOperationID(parts[3]) && parts[4] == "confirm" && r.Method == http.MethodPost:
		request.Action = "confirm"
		request.OperationID = parts[3]
	default:
		writeConnectorControlFailure(w, connectorcontrol.ErrNotFound)
		return
	}
	if request.Action != "recover" {
		auth, ok := serviceAuthentication(r)
		if !ok {
			writeConnectorControlFailure(w, connectorcontrol.ErrSetupUnauthorized)
			return
		}
		request.IdentityID = auth.Identity.ID
		request.BearerDigest = auth.TokenDigest
	}
	if request.Action == "rotate" || request.Action == "recover" {
		var body connectorcontrol.CredentialRequest
		if decodeBoundedWorkspaceRequest(w, r, &body, 8192) != nil || !body.Valid() || body.Kind != request.Action || body.InstallationID != request.InstallationID {
			writeConnectorControlFailure(w, connectorcontrol.ErrInvalid)
			return
		}
		request.Credential = &body
	} else if request.Action == "confirm" {
		var body connectorcontrol.CredentialConfirm
		if decodeBoundedWorkspaceRequest(w, r, &body, 4096) != nil || body.SchemaVersion != connectorcontrol.CredentialRequestSchema || body.OperationID != request.OperationID {
			writeConnectorControlFailure(w, connectorcontrol.ErrInvalid)
			return
		}
		request.Confirm = &body
	}
	if !request.Valid() {
		writeConnectorControlFailure(w, connectorcontrol.ErrInvalid)
		return
	}
	s.executeCredential(w, r, request, http.StatusOK)
}
