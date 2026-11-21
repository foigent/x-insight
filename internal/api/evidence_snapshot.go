package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/investigation"
	"github.com/foigent/x-insight/internal/pagination"
)

type EvidenceRevisionStore interface {
	ResolveEvidenceRevision(context.Context, string) (investigation.EvidenceRevision, error)
}

func (s *Server) evidenceReadContext(w http.ResponseWriter, r *http.Request, field string, cursor *pagination.Cursor) (context.Context, string, bool) {
	ctx := r.Context()
	requested := r.URL.Query().Get(field)
	if r.URL.Query().Has(field) && !uuidPattern.MatchString(requested) {
		writeRequestFailure(w, invalidField(field, "must_be_a_uuid"))
		return ctx, "", false
	}
	requested = strings.ToLower(requested)
	if cursor != nil {
		if requested != "" && requested != cursor.EvidenceRevision {
			writeRequestFailure(w, cursorFailure(pagination.ErrMismatch))
			return ctx, "", false
		}
		requested = cursor.EvidenceRevision
	}
	if requested == "" && (cursor != nil || r.URL.Query().Has("as_of") || field != "evidence_revision") {
		return ctx, "", true
	}
	return s.resolveEvidenceRead(w, ctx, requested)
}

func (s *Server) resolveEvidenceRead(w http.ResponseWriter, ctx context.Context, requested string) (context.Context, string, bool) {
	resolver, ok := s.store.(EvidenceRevisionStore)
	if !ok {
		if requested != "" {
			writeProblem(w, http.StatusServiceUnavailable, "database_unavailable", "Evidence revisions are unavailable", true)
			return ctx, "", false
		}
		return ctx, "", true
	}
	revision, err := resolver.ResolveEvidenceRevision(ctx, requested)
	if errors.Is(err, db.ErrEvidenceRevisionNotFound) {
		writeProblem(w, http.StatusGone, "evidence_revision_unavailable", "Evidence revision is unavailable", false)
		return ctx, "", false
	}
	if err != nil {
		writeStoreFailure(w, err)
		return ctx, "", false
	}
	return investigation.WithEvidenceRevision(ctx, revision), revision.ID, true
}

func (s *Server) encodeEvidenceCursor(cursor pagination.Cursor, revision string) (string, error) {
	if revision != "" {
		now := s.now().UTC()
		cursor.Version = pagination.SnapshotVersion
		cursor.EvidenceRevision = revision
		cursor.IssuedAt = &now
	}
	return pagination.Encode(cursor)
}

func (s *Server) executionEvidenceRead(w http.ResponseWriter, r *http.Request) (context.Context, time.Time, bool) {
	values := r.URL.Query()
	asOf, failure := s.parseInvestigationAsOf(values.Get("as_of"), values.Has("as_of"), values.Has("evidence_revision"))
	if failure != nil {
		writeRequestFailure(w, failure)
		return r.Context(), time.Time{}, false
	}
	ctx, _, ok := s.evidenceReadContext(w, r, "evidence_revision", nil)
	return ctx, asOf, ok
}
