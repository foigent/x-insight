package api

import (
	"errors"
	productaccess "github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/connectorsync"
	"net/http"
	"strings"
)

func (s *Server) handleConnectorSync(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, credentialInstallationPrefix), "/")
	if len(parts) != 2 || !connectorcontrol.IsInstallationID(parts[0]) || (parts[1] != "sync" && parts[1] != "sync-once") {
		http.NotFound(w, r)
		return
	}
	if !s.requireHumanPermission(w, r, productaccess.PermissionConnectorInstallationsManage) {
		return
	}
	if failure := validateQueryKeys(r); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	if !s.connectorConfig.Enabled() {
		writeProblem(w, 503, "connector_sync_unavailable", "Sync control unavailable", true)
		return
	}
	c, ok := s.connectorIssuer.(connectorsync.Controller)
	if !ok {
		writeProblem(w, 503, "connector_sync_unavailable", "Sync control unavailable", true)
		return
	}
	auth, _ := accessAuthentication(r)
	request := connectorsync.Request{SchemaVersion: connectorsync.LocalSchema, InstallationID: parts[0], Actor: auth.Session.SubjectID, Action: "status"}
	status := http.StatusOK
	if r.Method != http.MethodGet || parts[1] != "sync" {
		if !(r.Method == http.MethodPut && parts[1] == "sync" || r.Method == http.MethodPost && parts[1] == "sync-once") {
			if parts[1] == "sync-once" {
				methodNotAllowedFor(w, http.MethodPost)
			} else {
				methodNotAllowedFor(w, http.MethodGet, http.MethodPut)
			}
			return
		}
		if !s.requireHumanMutationAuthorization(w, r) {
			return
		}
		var body struct {
			Expected  int64  `json:"expected_revision"`
			RequestID string `json:"request_id"`
			Auto      bool   `json:"auto_sync"`
			Cadence   *int   `json:"cadence_seconds,omitempty"`
		}
		if decodeBoundedWorkspaceRequest(w, r, &body, 4096) != nil {
			writeProblem(w, 400, "invalid_connector_sync", "Invalid sync request", false)
			return
		}
		request.ExpectedRevision = body.Expected
		request.RequestID = body.RequestID
		request.Auto = body.Auto
		request.Cadence = body.Cadence
		request.Action = "set"
		if parts[1] == "sync-once" {
			request.Action = "once"
			status = http.StatusAccepted
		}
	}
	if !request.Valid() {
		writeProblem(w, 400, "invalid_connector_sync", "Invalid sync request", false)
		return
	}
	response, e := c.ExecuteSync(r.Context(), request)
	if e != nil {
		if errors.Is(e, connectorsync.ErrBinding) {
			writeProblem(w, 400, "connector_sync_binding_required", "Verified connection binding required", false)
		} else if errors.Is(e, connectorsync.ErrConflict) {
			writeProblem(w, 409, "connector_sync_conflict", "Refresh current sync state before retrying", false)
		} else {
			writeProblem(w, 503, "connector_sync_unavailable", "Sync control unavailable", true)
		}
		return
	}
	if response.Status == nil {
		writeProblem(w, 503, "connector_sync_unavailable", "Sync status unavailable", true)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, status, response.Status)
}
