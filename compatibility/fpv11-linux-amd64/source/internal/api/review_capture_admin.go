package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	productaccess "github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/reviewpolicycontrol"
)

type reviewCaptureDefaultReader interface {
	ReadReviewCaptureDefault(context.Context) (db.ReviewCaptureDefault, error)
}

type reviewCaptureDefaultInput struct {
	ExpectedRevision int64                    `json:"expected_revision"`
	Settings         db.ReviewCaptureSettings `json:"settings"`
}

type reviewCaptureConfirmInput struct {
	DesiredRevision int64 `json:"desired_revision"`
}

func (s *Server) handleReviewCaptureDefault(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		methodNotAllowedFor(w, http.MethodGet, http.MethodPut)
		return
	}
	auth, ok := accessAuthentication(r)
	if !ok || !productaccess.HasPermission(auth.Session.Role, productaccess.PermissionReviewPolicyManage) {
		writeProblem(w, http.StatusForbidden, "permission_denied", "Permission is denied", false)
		return
	}
	reader, ok := s.workspaceStore.(reviewCaptureDefaultReader)
	if !ok || reader == nil {
		writeProblem(w, http.StatusServiceUnavailable, "review_policy_unavailable", "Review policy is unavailable", true)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		value, err := reader.ReadReviewCaptureDefault(r.Context())
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{"state": "unconfigured", "suggested": db.ReviewCaptureSettings{
				Mode: "full_content", RetentionSeconds: 30 * 86400, ProjectQuotaBytes: 5 << 30, MaxItemBytes: 32 << 20}})
			return
		}
		if err != nil {
			writeProblem(w, http.StatusServiceUnavailable, "review_policy_unavailable", "Review policy is unavailable", true)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"state": "configured", "policy": value,
			"drift": value.DesiredRevision != value.EffectiveRevision})
		return
	}
	if s.reviewPolicyController == nil || !s.transportSecurity.requireSameOriginHTTPS(r) ||
		!s.access.VerifyCSRF(auth, r.Header.Get(csrfHeaderName)) || !requireJSONContentType(r) {
		writeProblem(w, http.StatusForbidden, "csrf_validation_failed", "Request authorization failed", false)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	raw, err := io.ReadAll(r.Body)
	if err != nil || len(raw) == 0 || len(raw) > 4096 {
		writeProblem(w, http.StatusBadRequest, "invalid_review_policy", "Review policy request is invalid", false)
		return
	}
	if _, err = connector.DecodeStrictJSONObject(raw); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_review_policy", "Review policy request is invalid", false)
		return
	}
	var input reviewCaptureDefaultInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_review_policy", "Review policy request is invalid", false)
		return
	}
	command := reviewpolicycontrol.Request{SchemaVersion: reviewpolicycontrol.RequestSchema,
		Action: reviewpolicycontrol.ActionApplyDefault, ActorSubject: auth.Session.SubjectID,
		ExpectedRevision: input.ExpectedRevision, Settings: input.Settings, RequestedAt: s.now().UTC()}
	if !reviewpolicycontrol.ValidRequest(command) {
		writeProblem(w, http.StatusBadRequest, "invalid_review_policy", "Review policy request is invalid", false)
		return
	}
	result, err := s.reviewPolicyController.ApplyDefault(r.Context(), command)
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "review_policy_unconfirmed", "Review policy outcome is unconfirmed; read back before retry", true)
		return
	}
	policy, err := reader.ReadReviewCaptureDefault(r.Context())
	if err != nil || policy.DesiredRevision != result.DesiredRevision ||
		(result.State == "effective" && policy.EffectiveRevision != result.EffectiveRevision) ||
		(result.State == "pending" && policy.EffectiveRevision >= policy.DesiredRevision) {
		writeProblem(w, http.StatusServiceUnavailable, "review_policy_unconfirmed", "Review policy outcome is unconfirmed; read back before retry", true)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": result.State, "policy": policy,
		"drift": policy.DesiredRevision != policy.EffectiveRevision})
}

// A saved desired policy can remain pending after a controller/DB interruption.
// The administrator can explicitly confirm that exact revision after readback.
func (s *Server) handleReviewCaptureDefaultConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	auth, ok := accessAuthentication(r)
	if !ok || !productaccess.HasPermission(auth.Session.Role, productaccess.PermissionReviewPolicyManage) {
		writeProblem(w, http.StatusForbidden, "permission_denied", "Permission is denied", false)
		return
	}
	reader, ok := s.workspaceStore.(reviewCaptureDefaultReader)
	if !ok || reader == nil || s.reviewPolicyController == nil {
		writeProblem(w, http.StatusServiceUnavailable, "review_policy_unavailable", "Review policy is unavailable", true)
		return
	}
	if !s.transportSecurity.requireSameOriginHTTPS(r) || !s.access.VerifyCSRF(auth, r.Header.Get(csrfHeaderName)) ||
		!requireJSONContentType(r) {
		writeProblem(w, http.StatusForbidden, "csrf_validation_failed", "Request authorization failed", false)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	raw, err := io.ReadAll(r.Body)
	if err != nil || len(raw) == 0 || len(raw) > 4096 {
		writeProblem(w, http.StatusBadRequest, "invalid_review_policy", "Review policy request is invalid", false)
		return
	}
	if _, err = connector.DecodeStrictJSONObject(raw); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_review_policy", "Review policy request is invalid", false)
		return
	}
	var input reviewCaptureConfirmInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_review_policy", "Review policy request is invalid", false)
		return
	}
	command := reviewpolicycontrol.Request{SchemaVersion: reviewpolicycontrol.RequestSchema,
		Action: reviewpolicycontrol.ActionConfirmDefault, ActorSubject: auth.Session.SubjectID,
		ExpectedRevision: input.DesiredRevision, RequestedAt: s.now().UTC()}
	if !reviewpolicycontrol.ValidRequest(command) {
		writeProblem(w, http.StatusBadRequest, "invalid_review_policy", "Review policy request is invalid", false)
		return
	}
	result, err := s.reviewPolicyController.ConfirmDefault(r.Context(), command)
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "review_policy_unconfirmed", "Review policy outcome is unconfirmed; read back before retry", true)
		return
	}
	policy, err := reader.ReadReviewCaptureDefault(r.Context())
	if err != nil || policy.DesiredRevision != result.DesiredRevision || policy.EffectiveRevision != result.EffectiveRevision {
		writeProblem(w, http.StatusServiceUnavailable, "review_policy_unconfirmed", "Review policy outcome is unconfirmed; read back before retry", true)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"state": result.State, "policy": policy, "drift": false})
}

type reviewCaptureProjectReader interface {
	ListReviewCaptureProjects(context.Context) (db.ReviewCaptureProjectCatalog, error)
}

func (s *Server) handleReviewCaptureProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodGet, http.MethodPost)
		return
	}
	auth, ok := accessAuthentication(r)
	if !ok || !productaccess.HasPermission(auth.Session.Role, productaccess.PermissionReviewPolicyManage) {
		writeProblem(w, http.StatusForbidden, "permission_denied", "Permission is denied", false)
		return
	}
	reader, ok := s.workspaceStore.(reviewCaptureProjectReader)
	if !ok || reader == nil {
		writeProblem(w, http.StatusServiceUnavailable, "review_policy_unavailable", "Review policy is unavailable", true)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		catalog, err := reader.ListReviewCaptureProjects(r.Context())
		if err != nil {
			writeProblem(w, http.StatusServiceUnavailable, "review_policy_unavailable", "Review policy is unavailable", true)
			return
		}
		writeJSON(w, http.StatusOK, catalog)
		return
	}
	if s.reviewPolicyController == nil || !s.transportSecurity.requireSameOriginHTTPS(r) ||
		!s.access.VerifyCSRF(auth, r.Header.Get(csrfHeaderName)) || !requireJSONContentType(r) {
		writeProblem(w, http.StatusForbidden, "csrf_validation_failed", "Request authorization failed", false)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	raw, err := io.ReadAll(r.Body)
	if err != nil || len(raw) == 0 || len(raw) > 4096 {
		writeProblem(w, http.StatusBadRequest, "invalid_review_project", "Project binding request is invalid", false)
		return
	}
	if _, err = connector.DecodeStrictJSONObject(raw); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_review_project", "Project binding request is invalid", false)
		return
	}
	var input reviewpolicycontrol.ProjectBinding
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_review_project", "Project binding request is invalid", false)
		return
	}
	command := reviewpolicycontrol.Request{SchemaVersion: reviewpolicycontrol.RequestSchema,
		Action: reviewpolicycontrol.ActionBindProject, ActorSubject: auth.Session.SubjectID,
		RequestedAt: s.now().UTC(), Project: &input}
	if !reviewpolicycontrol.ValidRequest(command) {
		writeProblem(w, http.StatusBadRequest, "invalid_review_project", "Project binding request is invalid", false)
		return
	}
	result, err := s.reviewPolicyController.BindProject(r.Context(), command)
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "review_project_unconfirmed", "Project binding outcome is unconfirmed; read back before retry", true)
		return
	}
	catalog, err := reader.ListReviewCaptureProjects(r.Context())
	if err != nil || result.Project == nil {
		writeProblem(w, http.StatusServiceUnavailable, "review_project_unconfirmed", "Project binding outcome is unconfirmed; read back before retry", true)
		return
	}
	for _, choice := range catalog.Items {
		if choice.SourceSystemID == input.SourceSystemID && choice.SourceInstance == input.SourceInstance &&
			choice.OrgID == input.OrgID && choice.ProjectID == input.ProjectID &&
			choice.XIProjectID == result.Project.XIProjectID && choice.XIProjectName == input.DisplayName {
			writeJSON(w, http.StatusOK, catalog)
			return
		}
	}
	writeProblem(w, http.StatusServiceUnavailable, "review_project_unconfirmed", "Project binding outcome is unconfirmed; read back before retry", true)
}
