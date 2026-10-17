package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	productaccess "github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/workchain"
	"github.com/foigent/x-insight/internal/workspace"
)

const maxWorkspaceBodyBytes = 16 << 10

var (
	caseIDPattern            = regexp.MustCompile(`^xc_[A-Za-z0-9_-]{22}$`)
	findingIDPattern         = regexp.MustCompile(`^xf_[A-Za-z0-9_-]{22}$`)
	serviceIdentityIDPattern = regexp.MustCompile(`^xsi_[A-Za-z0-9_-]{22}$`)
	gapIDPattern             = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[1-5][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)
	alertIDPattern           = regexp.MustCompile(`^xal_[a-f0-9]{64}$`)
)

type caseListResponse struct {
	Items []workspace.Case `json:"items"`
}

type savedViewListResponse struct {
	Items []workspace.SavedView `json:"items"`
}

type serviceIdentityListResponse struct {
	Items []workspace.ServiceIdentity `json:"items"`
}

type revokeServiceIdentityRequest struct {
	Reason string `json:"reason"`
}

func (s *Server) handleCaseCollection(w http.ResponseWriter, r *http.Request) {
	if s.workspaceStore == nil {
		writeProblem(w, http.StatusServiceUnavailable, "workspace_unavailable", "Investigation workspace is unavailable", true)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if failure := validateQueryKeys(r); failure != nil {
			writeRequestFailure(w, failure)
			return
		}
		items, err := s.workspaceStore.ListCases(r.Context(), 50)
		if err != nil {
			writeWorkspaceFailure(w, err)
			return
		}
		if items == nil {
			items = []workspace.Case{}
		}
		writeJSON(w, http.StatusOK, caseListResponse{Items: items})
	case http.MethodPost:
		actor, ok := s.requireHumanWorkspaceWrite(w, r)
		if !ok {
			return
		}
		var input workspace.CreateCaseInput
		if err := decodeWorkspaceRequest(w, r, &input); err != nil || workspace.ValidateCreateCase(input) != nil {
			writeProblem(w, http.StatusBadRequest, "invalid_workspace_request", "Workspace request is invalid", false)
			return
		}
		item, err := s.workspaceStore.CreateCase(r.Context(), actor, input, s.now().UTC(), nil)
		if err != nil {
			writeWorkspaceFailure(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, item)
	default:
		methodNotAllowedFor(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleCasePath(w http.ResponseWriter, r *http.Request) {
	if s.workspaceStore == nil {
		writeProblem(w, http.StatusServiceUnavailable, "workspace_unavailable", "Investigation workspace is unavailable", true)
		return
	}
	suffix := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/cases/"), "/")
	parts := strings.Split(suffix, "/")
	if len(parts) == 0 || !caseIDPattern.MatchString(parts[0]) {
		writeProblem(w, http.StatusNotFound, "case_not_found", "Case not found", false)
		return
	}
	caseID := parts[0]
	if len(parts) >= 4 && parts[1] == "findings" {
		if parts[3] == "assessment" || parts[3] == "assessment-actions" || parts[3] == "continuations" {
			s.handleFindingAssessmentPath(w, r, caseID, parts[2], parts[3:])
			return
		}
		s.handleFindingReviewPath(w, r, caseID, parts[2], parts[3:])
		return
	}
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			methodNotAllowedFor(w, http.MethodGet)
			return
		}
		record, err := s.workspaceStore.GetCase(r.Context(), caseID)
		if err != nil {
			writeWorkspaceFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, record)
		return
	}
	if len(parts) != 2 {
		writeProblem(w, http.StatusNotFound, "route_not_found", "Route not found", false)
		return
	}
	switch parts[1] {
	case "executions":
		s.handleCaseExecution(w, r, caseID)
	case "findings":
		s.handleCaseFinding(w, r, caseID)
	case "evidence":
		s.handleCaseEvidence(w, r, caseID)
	case "gaps":
		s.handleCaseGap(w, r, caseID)
	case "annotations":
		s.handleCaseAnnotation(w, r, caseID)
	case "seal":
		s.handleCaseSeal(w, r, caseID)
	case "evidence-pack.json":
		s.handleEvidencePack(w, r, caseID, false)
	case "evidence-pack.html":
		s.handleEvidencePack(w, r, caseID, true)
	default:
		writeProblem(w, http.StatusNotFound, "route_not_found", "Route not found", false)
	}
}

func (s *Server) handleCaseExecution(w http.ResponseWriter, r *http.Request, caseID string) {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	actor, ok := s.requireHumanWorkspaceWrite(w, r)
	if !ok {
		return
	}
	var input workspace.AddExecutionInput
	if err := decodeWorkspaceRequest(w, r, &input); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_workspace_request", "Workspace request is invalid", false)
		return
	}
	input.AsOf = input.AsOf.UTC()
	if workspace.ValidateAddExecution(input) != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_workspace_request", "Workspace request is invalid", false)
		return
	}
	store, ok := s.store.(InvestigationStore)
	if !ok {
		writeProblem(w, http.StatusServiceUnavailable, "database_unavailable", "Database is unavailable", true)
		return
	}
	if _, err := store.GetExecution(r.Context(), input.ExecutionID, input.AsOf); err != nil {
		writeStoreFailure(w, err)
		return
	}
	item, err := s.workspaceStore.AddCaseExecution(r.Context(), caseID, actor, input, s.now().UTC())
	if err != nil {
		writeWorkspaceFailure(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleCaseFinding(w http.ResponseWriter, r *http.Request, caseID string) {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	actor, ok := s.requireHumanWorkspaceWrite(w, r)
	if !ok {
		return
	}
	var input workspace.CreateFindingInput
	if err := decodeWorkspaceRequest(w, r, &input); err != nil || workspace.ValidateCreateFinding(input) != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_workspace_request", "Workspace request is invalid", false)
		return
	}
	item, err := s.workspaceStore.CreateFinding(r.Context(), caseID, actor, input, s.now().UTC(), nil)
	if err != nil {
		writeWorkspaceFailure(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleCaseEvidence(w http.ResponseWriter, r *http.Request, caseID string) {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	actor, ok := s.requireHumanWorkspaceWrite(w, r)
	if !ok {
		return
	}
	var input workspace.AddEvidenceInput
	if err := decodeWorkspaceRequest(w, r, &input); err != nil || workspace.ValidateAddEvidence(input) != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_workspace_request", "Workspace request is invalid", false)
		return
	}
	item, err := s.workspaceStore.AddEvidenceLink(r.Context(), caseID, actor, input, s.now().UTC())
	if err != nil {
		writeWorkspaceFailure(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleCaseGap(w http.ResponseWriter, r *http.Request, caseID string) {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	actor, ok := s.requireHumanWorkspaceWrite(w, r)
	if !ok {
		return
	}
	var input workspace.AddGapInput
	if err := decodeWorkspaceRequest(w, r, &input); err != nil || workspace.ValidateAddGap(input) != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_workspace_request", "Workspace request is invalid", false)
		return
	}
	item, err := s.workspaceStore.AddGapLink(r.Context(), caseID, actor, input, s.now().UTC())
	if err != nil {
		writeWorkspaceFailure(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleCaseAnnotation(w http.ResponseWriter, r *http.Request, caseID string) {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	actor, ok := s.requireHumanWorkspaceWrite(w, r)
	if !ok {
		return
	}
	var input workspace.CreateAnnotationInput
	if err := decodeWorkspaceRequest(w, r, &input); err != nil || workspace.ValidateAnnotation(input) != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_workspace_request", "Workspace request is invalid", false)
		return
	}
	item, err := s.workspaceStore.CreateAnnotation(r.Context(), caseID, actor, input, s.now().UTC(), nil)
	if err != nil {
		writeWorkspaceFailure(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleCaseSeal(w http.ResponseWriter, r *http.Request, caseID string) {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	actor, ok := s.requireHumanWorkspaceWrite(w, r)
	if !ok {
		return
	}
	var request struct{}
	if err := decodeWorkspaceRequest(w, r, &request); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_workspace_request", "Workspace request is invalid", false)
		return
	}
	item, err := s.workspaceStore.SealCase(r.Context(), caseID, actor, s.now().UTC())
	if err != nil {
		writeWorkspaceFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleEvidencePack(w http.ResponseWriter, r *http.Request, caseID string, html bool) {
	if r.Method != http.MethodGet {
		methodNotAllowedFor(w, http.MethodGet)
		return
	}
	record, err := s.workspaceStore.GetCase(r.Context(), caseID)
	if err != nil {
		writeWorkspaceFailure(w, err)
		return
	}
	if record.Case.Status == "open" || len(record.Case.SealedPack) == 0 {
		writeProblem(w, http.StatusConflict, "case_not_sealed", "Case is not sealed", false)
		return
	}
	var pack workspace.EvidencePack
	if err := json.Unmarshal(record.Case.SealedPack, &pack); err != nil || !workspace.SupportedEvidencePack(pack) {
		writeProblem(w, http.StatusServiceUnavailable, "workspace_data_invalid", "Workspace data is invalid", false)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	extension := ".json"
	if html {
		extension = ".html"
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+caseID+`-evidence-pack`+extension+`"`)
	if !html {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(record.Case.SealedPack)
		return
	}
	payload, err := workspace.RenderEvidencePackHTML(pack, record.Case.SealedDigest)
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "workspace_data_invalid", "Workspace data is invalid", false)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func (s *Server) handleSavedViewCollection(w http.ResponseWriter, r *http.Request) {
	if s.workspaceStore == nil {
		writeProblem(w, http.StatusServiceUnavailable, "workspace_unavailable", "Investigation workspace is unavailable", true)
		return
	}
	authentication, ok := accessAuthentication(r)
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "authentication_required", "Authentication is required", false)
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := s.workspaceStore.ListSavedViews(r.Context(), authentication.Session.SubjectID, 50)
		if err != nil {
			writeWorkspaceFailure(w, err)
			return
		}
		if items == nil {
			items = []workspace.SavedView{}
		}
		writeJSON(w, http.StatusOK, savedViewListResponse{Items: items})
	case http.MethodPost:
		actor, ok := s.requireHumanWorkspaceWrite(w, r)
		if !ok {
			return
		}
		var input workspace.CreateSavedViewInput
		if err := decodeWorkspaceRequest(w, r, &input); err != nil || workspace.ValidateSavedView(input) != nil {
			writeProblem(w, http.StatusBadRequest, "invalid_workspace_request", "Workspace request is invalid", false)
			return
		}
		item, err := s.workspaceStore.CreateSavedView(r.Context(), actor, input, s.now().UTC(), nil)
		if err != nil {
			writeWorkspaceFailure(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, item)
	default:
		methodNotAllowedFor(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleServiceIdentityCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowedFor(w, http.MethodGet)
		return
	}
	if !s.requireHumanPermission(w, r, productaccess.PermissionServiceIdentitiesRead) {
		return
	}
	if s.agentAccessStore == nil {
		writeProblem(w, http.StatusServiceUnavailable, "access_unavailable", "Access service is unavailable", true)
		return
	}
	items, err := s.agentAccessStore.ListServiceIdentities(r.Context(), 100)
	if err != nil {
		writeWorkspaceFailure(w, err)
		return
	}
	if items == nil {
		items = []workspace.ServiceIdentity{}
	}
	writeJSON(w, http.StatusOK, serviceIdentityListResponse{Items: items})
}

func (s *Server) handleServiceIdentityPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	if !s.requireHumanPermission(w, r, productaccess.PermissionServiceIdentitiesRevoke) {
		return
	}
	if !s.requireHumanMutationAuthorization(w, r) {
		return
	}
	if s.agentAccessStore == nil {
		writeProblem(w, http.StatusServiceUnavailable, "access_unavailable", "Access service is unavailable", true)
		return
	}
	suffix := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/service-identities/"), "/")
	parts := strings.Split(suffix, "/")
	if len(parts) != 2 || parts[1] != "revoke" || !serviceIdentityIDPattern.MatchString(parts[0]) {
		writeProblem(w, http.StatusNotFound, "service_identity_not_found", "Service identity not found", false)
		return
	}
	var input revokeServiceIdentityRequest
	if err := decodeWorkspaceRequest(w, r, &input); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_workspace_request", "Workspace request is invalid", false)
		return
	}
	authentication, _ := accessAuthentication(r)
	actor, _ := workspace.HumanActor(authentication.Session.SubjectID)
	item, err := s.agentAccessStore.RevokeServiceIdentity(
		r.Context(), parts[0], actor, input.Reason, s.now().UTC(),
	)
	if err != nil {
		writeWorkspaceFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleGapPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	if s.workspaceStore == nil {
		writeProblem(w, http.StatusServiceUnavailable, "workspace_unavailable", "Investigation workspace is unavailable", true)
		return
	}
	suffix := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/gaps/"), "/")
	parts := strings.Split(suffix, "/")
	if len(parts) != 2 || parts[1] != "resolution" || !gapIDPattern.MatchString(parts[0]) {
		writeProblem(w, http.StatusNotFound, "gap_not_found", "Capture Gap not found", false)
		return
	}
	actor, ok := s.requireHumanWorkspaceWrite(w, r)
	if !ok {
		return
	}
	var input workspace.ResolveGapInput
	if err := decodeWorkspaceRequest(w, r, &input); err != nil || workspace.ValidateResolveGap(input) != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_workspace_request", "Workspace request is invalid", false)
		return
	}
	item, err := s.workspaceStore.ResolveGap(r.Context(), actor, parts[0], input, s.now().UTC())
	if err != nil {
		writeWorkspaceFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleAlertPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	if s.workspaceStore == nil {
		writeProblem(w, http.StatusServiceUnavailable, "workspace_unavailable", "Investigation workspace is unavailable", true)
		return
	}
	suffix := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/alerts/"), "/")
	parts := strings.Split(suffix, "/")
	if len(parts) != 2 || parts[1] != "case" || !alertIDPattern.MatchString(parts[0]) {
		writeProblem(w, http.StatusNotFound, "alert_not_found", "Alert not found", false)
		return
	}
	actor, ok := s.requireHumanWorkspaceWrite(w, r)
	if !ok {
		return
	}
	var input struct {
		CaseID string `json:"case_id"`
	}
	if err := decodeWorkspaceRequest(w, r, &input); err != nil || !caseIDPattern.MatchString(input.CaseID) {
		writeProblem(w, http.StatusBadRequest, "invalid_workspace_request", "Workspace request is invalid", false)
		return
	}
	item, err := s.workspaceStore.LinkAlertToCase(
		r.Context(), actor, parts[0], input.CaseID, s.now().UTC(),
	)
	if err != nil {
		writeWorkspaceFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) requireHumanWorkspaceWrite(w http.ResponseWriter, r *http.Request) (workspace.Actor, bool) {
	if !s.requireHumanPermission(w, r, productaccess.PermissionInvestigationWrite) {
		return workspace.Actor{}, false
	}
	if !s.requireHumanMutationAuthorization(w, r) {
		return workspace.Actor{}, false
	}
	authentication, _ := accessAuthentication(r)
	actor, err := workspace.HumanActor(authentication.Session.SubjectID)
	if err != nil {
		writeProblem(w, http.StatusForbidden, "permission_denied", "Permission is denied", false)
		return workspace.Actor{}, false
	}
	return actor, true
}

func (s *Server) requireHumanMutationAuthorization(w http.ResponseWriter, r *http.Request) bool {
	if !s.transportSecurity.requireSameOriginHTTPS(r) {
		writeProblem(w, http.StatusForbidden, "same_origin_required", "Same-origin HTTPS is required", false)
		return false
	}
	authentication, ok := accessAuthentication(r)
	if !ok || s.access == nil || !s.access.VerifyCSRF(authentication, r.Header.Get(csrfHeaderName)) {
		writeProblem(w, http.StatusForbidden, "csrf_validation_failed", "Request authorization failed", false)
		return false
	}
	return true
}

func (s *Server) requireHumanPermission(w http.ResponseWriter, r *http.Request, permission productaccess.Permission) bool {
	authentication, ok := accessAuthentication(r)
	if ok && productaccess.IsScopedRole(authentication.Session.Role) && workchain.FromContext(r.Context()) != nil {
		switch permission {
		case productaccess.PermissionEvidenceRead:
			permission = productaccess.PermissionScopedRead
		case productaccess.PermissionEvidenceCite:
			permission = productaccess.PermissionScopedCite
		case productaccess.PermissionInvestigationWrite:
			permission = productaccess.PermissionScopedWrite
		}
	}
	if !ok || !productaccess.HasPermission(authentication.Session.Role, permission) {
		writeProblem(w, http.StatusForbidden, "permission_denied", "Permission is denied", false)
		return false
	}
	return true
}

func decodeWorkspaceRequest(w http.ResponseWriter, r *http.Request, target any) error {
	return decodeBoundedWorkspaceRequest(w, r, target, maxWorkspaceBodyBytes)
}

func decodeBoundedWorkspaceRequest(w http.ResponseWriter, r *http.Request, target any, maximum int64) error {
	if !requireJSONContentType(r) {
		return workspace.ErrInvalid
	}
	r.Body = http.MaxBytesReader(w, r.Body, maximum)
	payload, err := io.ReadAll(r.Body)
	if err != nil || len(payload) == 0 {
		return workspace.ErrInvalid
	}
	if err := rejectDuplicateJSONFields(payload); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return workspace.ErrInvalid
	}
	return nil
}

func rejectDuplicateJSONFields(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := scanUniqueJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return workspace.ErrInvalid
	}
	return nil
}

func scanUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return workspace.ErrInvalid
	}
	delim, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			key, ok := keyToken.(string)
			if err != nil || !ok {
				return workspace.ErrInvalid
			}
			if _, duplicate := seen[key]; duplicate {
				return workspace.ErrInvalid
			}
			seen[key] = struct{}{}
			if err := scanUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := scanUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return workspace.ErrInvalid
	}
}

func writeWorkspaceFailure(w http.ResponseWriter, err error) {
	if errors.Is(err, workspace.ErrContentLocationProofRequired) {
		if pending, ok := w.(interface{ requireContentLocationVerification() }); ok {
			pending.requireContentLocationVerification()
			return
		}
		writeProblem(w, http.StatusBadRequest, "invalid_content_choice", "Content location verification is required", false)
		return
	}
	switch {
	case errors.Is(err, workspace.ErrInvalid):
		writeProblem(w, http.StatusBadRequest, "invalid_workspace_request", "Workspace request is invalid", false)
	case errors.Is(err, workspace.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "workspace_record_not_found", "Workspace record not found", false)
	case errors.Is(err, workspace.ErrConflict):
		writeProblem(w, http.StatusConflict, "workspace_conflict", "Workspace record conflicts with current Evidence", false)
	case errors.Is(err, workspace.ErrSealed):
		writeProblem(w, http.StatusConflict, "case_sealed", "Case is sealed", false)
	case errors.Is(err, workspace.ErrCredentialRevoked):
		writeProblem(w, http.StatusConflict, "service_identity_revoked", "Service identity is revoked", false)
	case errors.Is(err, workspace.ErrCredentialExpired):
		writeProblem(w, http.StatusConflict, "service_identity_expired", "Service identity is expired", false)
	default:
		writeProblem(w, http.StatusServiceUnavailable, "workspace_unavailable", "Investigation workspace is unavailable", true)
	}
}
