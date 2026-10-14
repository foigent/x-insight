package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	productaccess "github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/connectorcontrol"
)

type createConnectorInvitationRequest struct {
	ExpiresInSeconds *int64 `json:"expires_in_seconds,omitempty"`
}

type connectorInvitationResponse struct {
	Invitation   connectorcontrol.InvitationPackage `json:"invitation"`
	Installation connectorcontrol.Installation      `json:"installation"`
}

func (s *Server) handleConnectorInvitationCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	if s.connectorIssuer == nil || s.connectorStore == nil || !s.connectorConfig.Enabled() {
		writeProblem(w, http.StatusServiceUnavailable, "connector_control_unavailable", "Connector installation is unavailable", true)
		return
	}
	if failure := validateQueryKeys(r); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	if !s.requireHumanPermission(w, r, productaccess.PermissionConnectorInstallationsManage) ||
		!s.requireHumanMutationAuthorization(w, r) {
		return
	}
	var request createConnectorInvitationRequest
	if err := decodeWorkspaceRequest(w, r, &request); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_connector_request", "Connector request is invalid", false)
		return
	}
	ttl := connectorcontrol.DefaultInvitationTTL
	if request.ExpiresInSeconds != nil {
		ttl = time.Duration(*request.ExpiresInSeconds) * time.Second
	}
	authentication, _ := accessAuthentication(r)
	invitation, installation, err := s.connectorIssuer.CreateInvitation(r.Context(), connectorcontrol.CreateInvitationInput{
		ActorSubject: authentication.Session.SubjectID,
		Config:       s.connectorConfig,
		Now:          s.now().UTC(),
		TTL:          ttl,
	})
	if err != nil {
		writeConnectorControlFailure(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusCreated, connectorInvitationResponse{
		Invitation: invitation, Installation: installation,
	})
}

func (s *Server) handleConnectorInvitationRedeem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	if s.connectorStore == nil || !s.connectorConfig.Enabled() {
		writeProblem(w, http.StatusServiceUnavailable, "connector_control_unavailable", "Connector installation is unavailable", true)
		return
	}
	if !s.transportSecurity.isHTTPS(r) || len(r.Header.Values("Authorization")) != 0 || len(r.Cookies()) != 0 {
		writeProblem(w, http.StatusForbidden, "connector_transport_rejected", "Connector transport is rejected", false)
		return
	}
	if failure := validateQueryKeys(r); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	var raw json.RawMessage
	if err := decodeWorkspaceRequest(w, r, &raw); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_connector_request", "Connector request is invalid", false)
		return
	}
	schema, err := connectorcontrol.RedeemSchema(raw)
	if err != nil {
		writeConnectorControlFailure(w, err)
		return
	}
	if schema == connectorcontrol.RedeemInvitationSchemaV2 {
		store, ok := s.connectorStore.(connectorcontrol.AdoptionInvitationStore)
		if !ok {
			writeConnectorControlFailure(w, errors.New("managed connector schema is unavailable"))
			return
		}
		input, err := connectorcontrol.DecodeRedeemInvitationV2(raw, s.now().UTC())
		if err != nil {
			writeConnectorControlFailure(w, err)
			return
		}
		result, err := store.RedeemAdoptionInvitation(r.Context(), input, s.now().UTC(), nil)
		if err != nil {
			writeConnectorControlFailure(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, result)
		return
	}
	var input connectorcontrol.RedeemInvitationInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil ||
		connectorcontrol.ValidateRedeemInvitation(input, s.now().UTC()) != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_connector_request", "Connector request is invalid", false)
		return
	}
	result, err := s.connectorStore.RedeemInvitation(r.Context(), input, s.now().UTC(), nil)
	if err != nil {
		writeConnectorControlFailure(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	public, err := publicConnectorOperation(result.Operation)
	if err != nil {
		writeConnectorControlFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Installation connectorcontrol.Installation `json:"installation"`
		Operation    any                           `json:"operation"`
	}{result.Installation, public})
}

func (s *Server) handleConnectorControlOperations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowedFor(w, http.MethodGet)
		return
	}
	if s.connectorStore == nil {
		writeProblem(w, http.StatusServiceUnavailable, "connector_control_unavailable", "Connector control is unavailable", true)
		return
	}
	if failure := validateQueryKeys(r, "installation_id"); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	installationID := r.URL.Query().Get("installation_id")
	if !connectorcontrol.IsInstallationID(installationID) {
		writeProblem(w, http.StatusBadRequest, "invalid_connector_request", "Connector request is invalid", false)
		return
	}
	authentication, ok := serviceAuthentication(r)
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "agent_authentication_failed", "Agent authentication failed", false)
		return
	}
	operation, err := s.connectorStore.PollControlOperation(
		r.Context(), authentication.Identity.ID, installationID, s.now().UTC(),
	)
	if errors.Is(err, connectorcontrol.ErrNotFound) {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeConnectorControlFailure(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	// The persisted operation also contains lease, retry and result metadata.
	// V2/V3 consumers accept only the frozen request authority on this route.
	public, err := publicConnectorOperation(operation)
	if err != nil {
		writeConnectorControlFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, public)
}

func publicConnectorOperation(operation connectorcontrol.ControlOperation) (any, error) {
	switch operation.SchemaVersion {
	case connectorcontrol.ControlOperationSchemaV2:
		request, _, _, err := connectorcontrol.CanonicalControlOperationRequestV2(operation)
		return request, err
	case connectorcontrol.ControlOperationSchemaV3:
		return connectorcontrol.OperationRequestV3(operation)
	default:
		return operation, nil
	}
}

func (s *Server) handleConnectorControlOperationPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	if s.connectorStore == nil {
		writeProblem(w, http.StatusServiceUnavailable, "connector_control_unavailable", "Connector control is unavailable", true)
		return
	}
	operationID := strings.TrimPrefix(r.URL.Path, "/api/v1/connector-control/operations/")
	if !connectorcontrol.IsOperationID(operationID) || strings.Contains(operationID, "/") {
		writeProblem(w, http.StatusNotFound, "connector_operation_not_found", "Connector operation was not found", false)
		return
	}
	if failure := validateQueryKeys(r); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	var result connectorcontrol.ControlOperationResult
	if err := decodeWorkspaceRequest(w, r, &result); err != nil ||
		result.OperationID != operationID ||
		connectorcontrol.ValidateControlResult(result, s.now().UTC()) != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_connector_request", "Connector request is invalid", false)
		return
	}
	authentication, ok := serviceAuthentication(r)
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "agent_authentication_failed", "Agent authentication failed", false)
		return
	}
	if result.SchemaVersion == connectorcontrol.ControlResultSchemaV3 {
		ack, ok := s.connectorController.(connectorcontrol.ManagedAcknowledger)
		if !ok || result.ResultV3 == nil {
			writeConnectorControlFailure(w, errors.New("managed controller unavailable"))
			return
		}
		installation, err := ack.AcknowledgeManaged(r.Context(), authentication.Identity.ID, *result.ResultV3)
		if err != nil {
			writeConnectorControlFailure(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, installation)
		return
	}
	installation, err := s.connectorStore.CompleteControlOperation(
		r.Context(), authentication.Identity.ID, result, s.now().UTC(),
	)
	if err != nil {
		writeConnectorControlFailure(w, err)
		return
	}
	wakeRequest := connectorcontrol.ControllerRequest{
		SchemaVersion:  connectorcontrol.LocalControllerRequestSchema,
		OperationID:    result.OperationID,
		InstallationID: result.InstallationID,
		Action:         connectorcontrol.ControllerActionPrepare,
	}
	if s.connectorController == nil {
		writeConnectorControlFailure(w, errors.New("local connector controller is unavailable"))
		return
	}
	wakeResponse, err := s.connectorController.Execute(r.Context(), wakeRequest)
	if err != nil || connectorcontrol.ValidateControllerResponse(wakeRequest, wakeResponse) != nil ||
		(wakeResponse.State == "abnormal" && wakeResponse.ReasonCode == "local_state_unavailable") {
		writeConnectorControlFailure(w, errors.New("local connector controller is unavailable"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, installation)
}

func writeConnectorControlFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, connectorcontrol.ErrInvalid):
		writeProblem(w, http.StatusBadRequest, "invalid_connector_request", "Connector request is invalid", false)
	case errors.Is(err, connectorcontrol.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "connector_not_found", "Connector was not found", false)
	case errors.Is(err, connectorcontrol.ErrExpired):
		writeProblem(w, http.StatusGone, "connector_invitation_expired", "Connector invitation expired", false)
	case errors.Is(err, connectorcontrol.ErrConsumed), errors.Is(err, connectorcontrol.ErrConflict):
		writeProblem(w, http.StatusConflict, "connector_state_conflict", "Connector state conflicts with the request", false)
	case errors.Is(err, io.EOF):
		writeProblem(w, http.StatusBadRequest, "invalid_connector_request", "Connector request is invalid", false)
	default:
		writeProblem(w, http.StatusServiceUnavailable, "connector_control_unavailable", "Connector control is unavailable", true)
	}
}
