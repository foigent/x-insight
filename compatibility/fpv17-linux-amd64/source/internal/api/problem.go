package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

type Problem struct {
	Type          string         `json:"type"`
	Title         string         `json:"title"`
	Status        int            `json:"status"`
	Code          string         `json:"code"`
	CorrelationID string         `json:"correlation_id"`
	Retryable     bool           `json:"retryable"`
	Fields        []ProblemField `json:"fields,omitempty"`
}

type ProblemField struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

var correlationSequence atomic.Uint64

func writeProblem(w http.ResponseWriter, status int, code string, title string, retryable bool, fields ...ProblemField) {
	correlationID := newCorrelationID()
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Correlation-ID", correlationID)
	w.WriteHeader(status)
	_ = encodeJSON(w, Problem{
		Type:          "urn:xinsight:error:" + code,
		Title:         title,
		Status:        status,
		Code:          code,
		CorrelationID: correlationID,
		Retryable:     retryable,
		Fields:        fields,
	})
}

func newCorrelationID() string {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		sequence := correlationSequence.Add(1)
		fallback := sha256.Sum256([]byte(fmt.Sprintf("%d:%d", time.Now().UnixNano(), sequence)))
		copy(entropy[:], fallback[:16])
	}
	return "xc_" + hex.EncodeToString(entropy[:])
}
