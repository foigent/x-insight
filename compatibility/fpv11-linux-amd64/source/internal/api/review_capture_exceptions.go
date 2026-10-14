package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	productaccess "github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/reviewpolicycontrol"
)

type reviewCaptureExceptionReader interface {
	ListReviewCaptureExceptions(context.Context) (db.ReviewCaptureExceptionCatalog, error)
	ReadReviewCaptureException(context.Context, string) (db.ReviewCaptureExceptionState, error)
}

type reviewCaptureExceptionController interface {
	ApplyException(context.Context, reviewpolicycontrol.Request) (reviewpolicycontrol.Response, error)
	ConfirmException(context.Context, reviewpolicycontrol.Request) (reviewpolicycontrol.Response, error)
}

func (s *Server) handleReviewCaptureExceptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		methodNotAllowedFor(w, http.MethodGet, http.MethodPut)
		return
	}
	auth, ok := accessAuthentication(r)
	if !ok || !productaccess.HasPermission(auth.Session.Role, productaccess.PermissionReviewPolicyManage) {
		writeProblem(w, http.StatusForbidden, "permission_denied", "Permission is denied", false)
		return
	}
	reader, ok := s.workspaceStore.(reviewCaptureExceptionReader)
	if !ok || reader == nil {
		writeProblem(w, http.StatusServiceUnavailable, "review_policy_unavailable", "Review policy is unavailable", true)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		if failure := validateQueryKeys(r); failure != nil {
			writeRequestFailure(w, failure)
			return
		}
		catalog, err := reader.ListReviewCaptureExceptions(r.Context())
		if err != nil {
			writeProblem(w, http.StatusServiceUnavailable, "review_policy_unavailable", "Review policy is unavailable", true)
			return
		}
		writeJSON(w, http.StatusOK, catalog)
		return
	}
	controller, ok := s.reviewPolicyController.(reviewCaptureExceptionController)
	if !ok || controller == nil || !s.transportSecurity.requireSameOriginHTTPS(r) ||
		!s.access.VerifyCSRF(auth, r.Header.Get(csrfHeaderName)) || !requireJSONContentType(r) {
		writeProblem(w, http.StatusForbidden, "csrf_validation_failed", "Request authorization failed", false)
		return
	}
	var input struct {
		ExpectedRevision int64                     `json:"expected_revision"`
		Exception        db.ReviewCaptureException `json:"exception"`
	}
	if !decodeReviewExceptionBody(w, r, &input) {
		return
	}
	input.Exception.Provenance = "xi_admin"
	command := reviewpolicycontrol.Request{SchemaVersion: reviewpolicycontrol.RequestSchema,
		Action: reviewpolicycontrol.ActionApplyException, ActorSubject: auth.Session.SubjectID,
		ExpectedRevision: input.ExpectedRevision, Exception: &input.Exception, RequestedAt: s.now().UTC()}
	if !reviewpolicycontrol.ValidRequest(command) {
		writeProblem(w, http.StatusBadRequest, "invalid_review_exception", "Review exception is invalid", false)
		return
	}
	result, err := controller.ApplyException(r.Context(), command)
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "review_policy_unconfirmed", "Review exception outcome is unconfirmed; read back before retry", true)
		return
	}
	state, err := reader.ReadReviewCaptureException(r.Context(), result.ExceptionRef)
	if err != nil || state.DesiredRevision != result.DesiredRevision ||
		(result.State == "effective" && state.EffectiveRevision != result.EffectiveRevision) {
		writeProblem(w, http.StatusServiceUnavailable, "review_policy_unconfirmed", "Review exception outcome is unconfirmed; read back before retry", true)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleReviewCaptureExceptionConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	auth, ok := accessAuthentication(r)
	if !ok || !productaccess.HasPermission(auth.Session.Role, productaccess.PermissionReviewPolicyManage) {
		writeProblem(w, http.StatusForbidden, "permission_denied", "Permission is denied", false)
		return
	}
	reader, ok := s.workspaceStore.(reviewCaptureExceptionReader)
	controller, controlOK := s.reviewPolicyController.(reviewCaptureExceptionController)
	if !ok || reader == nil || !controlOK || controller == nil || !s.transportSecurity.requireSameOriginHTTPS(r) ||
		!s.access.VerifyCSRF(auth, r.Header.Get(csrfHeaderName)) || !requireJSONContentType(r) {
		writeProblem(w, http.StatusForbidden, "csrf_validation_failed", "Request authorization failed", false)
		return
	}
	var input struct {
		PolicyRef       string `json:"policy_ref"`
		DesiredRevision int64  `json:"desired_revision"`
	}
	if !decodeReviewExceptionBody(w, r, &input) {
		return
	}
	command := reviewpolicycontrol.Request{SchemaVersion: reviewpolicycontrol.RequestSchema,
		Action: reviewpolicycontrol.ActionConfirmException, ActorSubject: auth.Session.SubjectID,
		ExceptionRef: input.PolicyRef, ExpectedRevision: input.DesiredRevision, RequestedAt: s.now().UTC()}
	if !reviewpolicycontrol.ValidRequest(command) {
		writeProblem(w, http.StatusBadRequest, "invalid_review_exception", "Review exception is invalid", false)
		return
	}
	result, err := controller.ConfirmException(r.Context(), command)
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "review_policy_unconfirmed", "Review exception outcome is unconfirmed; read back before retry", true)
		return
	}
	state, err := reader.ReadReviewCaptureException(r.Context(), result.ExceptionRef)
	if err != nil || state.DesiredRevision != result.DesiredRevision || state.EffectiveRevision != result.EffectiveRevision {
		writeProblem(w, http.StatusServiceUnavailable, "review_policy_unconfirmed", "Review exception outcome is unconfirmed; read back before retry", true)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, state)
}

func decodeReviewExceptionBody(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	raw, err := io.ReadAll(r.Body)
	if err != nil || len(raw) == 0 || len(raw) > 4096 {
		writeProblem(w, http.StatusBadRequest, "invalid_review_exception", "Review exception is invalid", false)
		return false
	}
	if _, err = connector.DecodeStrictJSONObject(raw); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_review_exception", "Review exception is invalid", false)
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(destination) != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_review_exception", "Review exception is invalid", false)
		return false
	}
	return true
}
