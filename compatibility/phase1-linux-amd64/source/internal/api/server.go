package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/db"
)

type Store interface {
	Ping(context.Context) error
	ListEvidenceEventsByRequestID(context.Context, string, int) ([]db.EvidenceEventRecord, error)
	ListCaptureGapsByRequestID(context.Context, string, int) ([]db.CaptureGapRecord, error)
}

type Server struct {
	store Store
}

func NewHandler(store Store) http.Handler {
	server := &Server{store: store}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", server.handleHealth)
	mux.HandleFunc("/readyz", server.handleReady)
	mux.HandleFunc("/api/v1/executions/", server.handleExecutionPath)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if s.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not_ready",
			"db":     "not_configured",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not_ready",
			"db":     "unavailable",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready", "db": "ok"})
}

func (s *Server) handleExecutionPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "XINSIGHT_POSTGRES_DSN is not configured")
		return
	}

	suffix := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/executions/"), "/")
	if suffix == "" {
		writeError(w, http.StatusNotFound, "not_found", "execution route requires request_id")
		return
	}
	parts := strings.Split(suffix, "/")
	requestID := strings.TrimSpace(parts[0])
	if requestID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_id", "request_id is required")
		return
	}

	limit, err := parseLimit(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}

	switch {
	case len(parts) == 1:
		s.handleExecution(w, r, requestID, limit)
	case len(parts) == 2 && parts[1] == "events":
		s.handleExecutionEvents(w, r, requestID, limit)
	case len(parts) == 2 && parts[1] == "gaps":
		s.handleExecutionGaps(w, r, requestID, limit)
	default:
		writeError(w, http.StatusNotFound, "not_found", "execution route not found")
	}
}

func (s *Server) handleExecution(w http.ResponseWriter, r *http.Request, requestID string, limit int) {
	events, err := s.store.ListEvidenceEventsByRequestID(r.Context(), requestID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_events_failed", err.Error())
		return
	}
	gaps, err := s.store.ListCaptureGapsByRequestID(r.Context(), requestID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_gaps_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"request_id":  requestID,
		"event_count": len(events),
		"gap_count":   len(gaps),
		"events":      events,
		"gaps":        gaps,
	})
}

func (s *Server) handleExecutionEvents(w http.ResponseWriter, r *http.Request, requestID string, limit int) {
	events, err := s.store.ListEvidenceEventsByRequestID(r.Context(), requestID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_events_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"request_id":  requestID,
		"event_count": len(events),
		"events":      events,
	})
}

func (s *Server) handleExecutionGaps(w http.ResponseWriter, r *http.Request, requestID string, limit int) {
	gaps, err := s.store.ListCaptureGapsByRequestID(r.Context(), requestID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_gaps_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"request_id": requestID,
		"gap_count":  len(gaps),
		"gaps":       gaps,
	})
}

func parseLimit(r *http.Request) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 200, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("limit must be an integer")
	}
	if limit <= 0 {
		return 0, fmt.Errorf("limit must be greater than zero")
	}
	if limit > 1000 {
		return 1000, nil
	}
	return limit, nil
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed")
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
