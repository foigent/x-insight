package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/workevent"
	"github.com/foigent/x-insight/internal/workspace"
)

type workEventIngestStore interface {
	ListWorkEventIngestConfig(context.Context, string, time.Time) (db.WorkEventIngestConfig, error)
	CommitAuthorizedWorkEventItem(context.Context, string, db.WorkEventIngestItem, time.Time) (db.WorkEventIngestReceipt, error)
}

type workEventBodyIngestStore interface {
	CommitAuthorizedWorkEventReviewBody(context.Context, string, db.WorkEventReviewBodyInput, reviewbody.KeyRing, time.Time) (db.ReviewBodyReceipt, error)
}

type workEventBodyMetadata struct {
	CollectionID               string `json:"collection_id"`
	ExpectedCollectionRevision int64  `json:"expected_collection_revision"`
	ScopeGeneration            string `json:"scope_generation"`
	EventRef                   string `json:"event_ref"`
	EventRevision              int64  `json:"event_revision"`
	EventHash                  string `json:"event_hash"`
	ContentKind                string `json:"content_kind"`
	ContentRef                 string `json:"content_ref"`
	ByteDomain                 string `json:"byte_domain"`
	SHA256                     string `json:"sha256"`
	PlainBytes                 int64  `json:"plain_bytes"`
}

type workEventIngestRequest struct {
	CollectionID               string          `json:"collection_id"`
	ExpectedCollectionRevision int64           `json:"expected_collection_revision"`
	ScopeGeneration            string          `json:"scope_generation"`
	Kind                       string          `json:"kind"`
	Document                   json.RawMessage `json:"document"`
}

func (s *Server) handleWorkEventIngest(w http.ResponseWriter, r *http.Request) {
	if !s.transportSecurity.isHTTPS(r) {
		writeProblem(w, http.StatusForbidden, "https_required", "HTTPS is required", false)
		return
	}
	auth, ok := serviceAuthentication(r)
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "agent_authentication_failed", "Agent authentication failed", false)
		return
	}
	store, ok := s.workspaceStore.(workEventIngestStore)
	if !ok {
		writeProblem(w, http.StatusServiceUnavailable, "work_event_ingest_unavailable", "Work event ingest is unavailable", true)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	switch r.URL.Path {
	case "/api/v1/work-event-ingest/contents":
		s.handleWorkEventBodyIngest(w, r, auth.TokenDigest)
	case "/api/v1/work-event-ingest/config":
		if r.Method != http.MethodGet {
			methodNotAllowedFor(w, http.MethodGet)
			return
		}
		if failure := validateQueryKeys(r); failure != nil {
			writeRequestFailure(w, failure)
			return
		}
		result, err := store.ListWorkEventIngestConfig(r.Context(), auth.TokenDigest, s.now().UTC())
		if err != nil {
			writeWorkEventIngestFailure(w, err)
			return
		}
		if s.reviewBodyKeys.ActiveID == "" {
			for i := range result.Items {
				result.Items[i].BodyCapture = db.WorkEventBodyCaptureView{Mode: "disabled"}
			}
		}
		writeJSON(w, http.StatusOK, result)
	case "/api/v1/work-event-ingest/items":
		if r.Method != http.MethodPost {
			methodNotAllowedFor(w, http.MethodPost)
			return
		}
		if failure := validateQueryKeys(r); failure != nil {
			writeRequestFailure(w, failure)
			return
		}
		var request workEventIngestRequest
		if err := decodeBoundedWorkspaceRequest(w, r, &request, 10<<10); err != nil {
			writeProblem(w, http.StatusBadRequest, "invalid_work_event_item", "Work event item is invalid", false)
			return
		}
		receipt, err := store.CommitAuthorizedWorkEventItem(r.Context(), auth.TokenDigest, db.WorkEventIngestItem{
			CollectionID: request.CollectionID, ExpectedCollectionRevision: request.ExpectedCollectionRevision,
			ScopeGeneration: request.ScopeGeneration, Kind: request.Kind, Document: request.Document,
		}, s.now().UTC())
		if err != nil {
			writeWorkEventIngestFailure(w, err)
			return
		}
		status := http.StatusCreated
		if receipt.Outcome == "identical_replay" {
			status = http.StatusOK
		}
		writeJSON(w, status, receipt)
	default:
		writeProblem(w, http.StatusNotFound, "route_not_found", "Route not found", false)
	}
}

func (s *Server) handleWorkEventBodyIngest(w http.ResponseWriter, r *http.Request, digest string) {
	if r.Method != http.MethodPost {
		methodNotAllowedFor(w, http.MethodPost)
		return
	}
	if failure := validateQueryKeys(r); failure != nil {
		writeRequestFailure(w, failure)
		return
	}
	store, ok := s.workspaceStore.(workEventBodyIngestStore)
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
	if r.Header.Get("Content-Type") != "application/octet-stream" || r.ContentLength < 0 || r.ContentLength > 32<<20 {
		writeProblem(w, http.StatusBadRequest, "invalid_review_body", "Review body request is invalid", false)
		return
	}
	header := r.Header.Get("X-XI-Body-Metadata")
	if len(header) == 0 || len(header) > 4096 {
		writeProblem(w, http.StatusBadRequest, "invalid_review_body", "Review body metadata is invalid", false)
		return
	}
	metadataBytes, err := base64.RawURLEncoding.DecodeString(header)
	if err != nil || len(metadataBytes) > 3072 {
		writeProblem(w, http.StatusBadRequest, "invalid_review_body", "Review body metadata is invalid", false)
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(metadataBytes))
	decoder.DisallowUnknownFields()
	var metadata workEventBodyMetadata
	if decoder.Decode(&metadata) != nil || decoder.Decode(new(any)) != io.EOF || metadata.PlainBytes != r.ContentLength {
		writeProblem(w, http.StatusBadRequest, "invalid_review_body", "Review body metadata is invalid", false)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<20))
	if err != nil || int64(len(body)) != metadata.PlainBytes {
		writeProblem(w, http.StatusBadRequest, "invalid_review_body", "Review body bytes are invalid", false)
		return
	}
	receipt, err := store.CommitAuthorizedWorkEventReviewBody(r.Context(), digest, db.WorkEventReviewBodyInput{
		CollectionID: metadata.CollectionID, ExpectedCollectionRevision: metadata.ExpectedCollectionRevision,
		ScopeGeneration: metadata.ScopeGeneration, EventRef: metadata.EventRef, EventRevision: metadata.EventRevision,
		EventHash: metadata.EventHash, ContentKind: metadata.ContentKind, ContentRef: metadata.ContentRef,
		ByteDomain: metadata.ByteDomain, SHA256: metadata.SHA256, Body: body,
	}, s.reviewBodyKeys, s.now().UTC())
	if err != nil {
		writeReviewBodyFailure(w, err)
		return
	}
	status := http.StatusCreated
	if receipt.Outcome == "identical_replay" {
		status = http.StatusOK
	}
	writeJSON(w, status, receipt)
}

func writeReviewBodyFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workspace.ErrCredentialRevoked), errors.Is(err, workspace.ErrCredentialExpired), errors.Is(err, workspace.ErrCredentialInvalid):
		writeProblem(w, http.StatusUnauthorized, "agent_authentication_failed", "Agent authentication failed", false)
	case errors.Is(err, reviewbody.ErrScope), errors.Is(err, reviewbody.ErrPolicy):
		writeProblem(w, http.StatusForbidden, "review_body_not_authorized", "Review body capture is not authorized", false)
	case errors.Is(err, reviewbody.ErrQuota):
		writeProblem(w, http.StatusInsufficientStorage, "review_body_quota_exhausted", "Review body project quota is exhausted", false)
	case errors.Is(err, reviewbody.ErrExpired):
		writeProblem(w, http.StatusGone, "review_body_expired", "Review body reservation expired", false)
	case errors.Is(err, reviewbody.ErrConflict):
		writeProblem(w, http.StatusConflict, "review_body_conflict", "Review body identity conflicts", false)
	case errors.Is(err, reviewbody.ErrInvalid), errors.Is(err, reviewbody.ErrIntegrity):
		writeProblem(w, http.StatusBadRequest, "invalid_review_body", "Review body is invalid", false)
	default:
		writeProblem(w, http.StatusServiceUnavailable, "review_body_unavailable", "Review body custody is unavailable", true)
	}
}

func writeWorkEventIngestFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workspace.ErrCredentialRevoked), errors.Is(err, workspace.ErrCredentialExpired), errors.Is(err, workspace.ErrCredentialInvalid):
		writeProblem(w, http.StatusUnauthorized, "agent_authentication_failed", "Agent authentication failed", false)
	case errors.Is(err, workevent.ErrInvalid):
		writeProblem(w, http.StatusBadRequest, "invalid_work_event_item", "Work event item is invalid", false)
	case errors.Is(err, workevent.ErrConflict):
		writeProblem(w, http.StatusConflict, "work_event_conflict", "Work event conflicts with accepted evidence", false)
	case errors.Is(err, workevent.ErrScopeChanged):
		writeProblem(w, http.StatusForbidden, "work_event_scope_changed", "Work event scope is unavailable", false)
	case errors.Is(err, workevent.ErrUnavailable):
		writeProblem(w, http.StatusConflict, "work_event_dependency_pending", "Work event predecessor is unavailable", true)
	default:
		writeProblem(w, http.StatusServiceUnavailable, "work_event_ingest_unavailable", "Work event ingest is unavailable", true)
	}
}
