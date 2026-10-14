package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

type reviewBodyReadStore interface {
	ReadReviewBody(context.Context, string, string, reviewbody.KeyRing, time.Time) (db.ReviewBodyRead, error)
	ReauthorizeReviewBodyRead(context.Context, string, string, time.Time) error
	AuditReviewBodyRead(context.Context, string, string, string, time.Time) error
}

// The copied body is verified before response headers are committed. Every
// subsequent block checks current session scope and current content grant.
func (s *Server) handleReviewBodyBytes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowedFor(w, http.MethodGet)
		return
	}
	if len(r.Header.Values("Range")) > 0 {
		writeProblem(w, http.StatusBadRequest, "unsupported_range", "Partial body reads are not supported", false)
		return
	}
	if failure := validateQueryKeys(r); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	ref := strings.TrimPrefix(r.URL.Path, "/api/v1/work-activity/contents/")
	if ref == "" || strings.Contains(ref, "/") {
		writeWorkbenchFailure(w, workbench.ErrNotFound)
		return
	}
	auth, ok := accessAuthentication(r)
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "authentication_required", "Authentication is required", false)
		return
	}
	store, ok := s.workbenchStore.(reviewBodyReadStore)
	if !ok || s.reviewBodyKeys.ActiveID == "" {
		writeProblem(w, http.StatusServiceUnavailable, "review_body_unavailable", "Review body custody is unavailable", true)
		return
	}
	select {
	case s.reviewBodyCapacity <- struct{}{}:
		defer func() { <-s.reviewBodyCapacity }()
	default:
		writeProblem(w, http.StatusServiceUnavailable, "review_body_busy", "Review body custody is busy", true)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	scope, err := s.contentCurrentScope(ctx, auth)
	if err != nil {
		writeContentFailure(w, err)
		return
	}
	w.Header().Set("X-XInsight-Scope-Revision", scope)
	if expected := r.Header.Get("X-XInsight-Scope-Revision"); expected == "" || expected != scope {
		writeProblem(w, http.StatusConflict, "scope_revision_changed", "Authorization changed; refresh the session", false)
		return
	}
	body, err := store.ReadReviewBody(ctx, auth.Session.SubjectID, ref, s.reviewBodyKeys, s.now().UTC())
	if err != nil {
		if auditErr := store.AuditReviewBodyRead(ctx, auth.Session.SubjectID, ref, "denied", s.now().UTC()); auditErr != nil {
			writeProblem(w, http.StatusServiceUnavailable, "review_body_audit_unavailable", "Review body audit is unavailable", true)
			return
		}
		writeReviewBodyReadFailure(w, err)
		return
	}
	check := func() error {
		current, err := s.contentCurrentScope(ctx, auth)
		if err != nil {
			return err
		}
		if current != scope {
			return workbench.ErrAccessChanged
		}
		if !s.now().Before(body.ExpiresAt) {
			return reviewbody.ErrExpired
		}
		return store.ReauthorizeReviewBodyRead(ctx, auth.Session.SubjectID, ref, s.now().UTC())
	}
	if err := check(); err != nil {
		if auditErr := store.AuditReviewBodyRead(ctx, auth.Session.SubjectID, ref, "denied", s.now().UTC()); auditErr != nil {
			writeProblem(w, http.StatusServiceUnavailable, "review_body_audit_unavailable", "Review body audit is unavailable", true)
			return
		}
		writeReviewBodyReadFailure(w, err)
		return
	}
	if err := store.AuditReviewBodyRead(ctx, auth.Session.SubjectID, ref, "accepted", s.now().UTC()); err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "review_body_audit_unavailable", "Review body audit is unavailable", true)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(body.Bytes)))
	w.Header().Set("X-XInsight-Content-SHA256", body.SHA256)
	w.Header().Set("X-XInsight-Content-Expires-At", body.ExpiresAt.UTC().Format(sourcefacts.TimeFormat))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="review-body.bin"`)
	if len(body.Bytes) == 0 {
		w.WriteHeader(http.StatusOK)
		return
	}
	controller := http.NewResponseController(w)
	for offset := 0; offset < len(body.Bytes); {
		if err := check(); err != nil {
			panic(http.ErrAbortHandler)
		}
		if err := controller.SetWriteDeadline(s.now().Add(10 * time.Second)); err != nil {
			panic(http.ErrAbortHandler)
		}
		end := offset + 1<<20
		if end > len(body.Bytes) {
			end = len(body.Bytes)
		}
		n, err := w.Write(body.Bytes[offset:end])
		if err != nil || n <= 0 {
			panic(http.ErrAbortHandler)
		}
		offset += n
	}
}

func writeReviewBodyReadFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workbench.ErrNotFound), errors.Is(err, reviewbody.ErrExpired):
		writeProblem(w, http.StatusNotFound, "review_body_not_available", "Review body is not available", false)
	case errors.Is(err, reviewbody.ErrIntegrity):
		writeProblem(w, http.StatusServiceUnavailable, "review_body_integrity_failed", "Review body integrity verification failed", false)
	case errors.Is(err, reviewbody.ErrKeyUnavailable):
		writeProblem(w, http.StatusServiceUnavailable, "review_body_key_unavailable", "Review body key is unavailable", true)
	default:
		writeContentFailure(w, err)
	}
}
