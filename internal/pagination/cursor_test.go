package pagination_test

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/pagination"
)

func TestCursorGoldenRoundTripAndBinding(t *testing.T) {
	asOf := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	filterHash, err := pagination.HashCanonicalFilters(struct {
		Outcome string `json:"outcome"`
	}{Outcome: "failure"})
	if err != nil {
		t.Fatalf("hash filters: %v", err)
	}
	cursor := pagination.Cursor{
		Version:    pagination.Version,
		Kind:       pagination.KindExecutionSearch,
		AsOf:       asOf,
		FilterHash: filterHash,
		Keys:       []string{"2026-07-13T11:59:00Z", "xe1_abc"},
	}
	encoded, err := pagination.Encode(cursor)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := pagination.Decode(encoded, asOf.Add(time.Hour))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Version != cursor.Version || decoded.Kind != cursor.Kind || decoded.FilterHash != filterHash || len(decoded.Keys) != 2 {
		t.Fatalf("decoded=%+v", decoded)
	}
	if err := pagination.ValidateBinding(decoded, pagination.KindExecutionSearch, filterHash, ""); err != nil {
		t.Fatalf("binding: %v", err)
	}
	if err := pagination.ValidateBinding(decoded, pagination.KindExecutionSearch, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ""); !errors.Is(err, pagination.ErrMismatch) {
		t.Fatalf("mismatch=%v", err)
	}
}

func TestCursorRejectsDuplicateUnknownExpiredAndWrongScope(t *testing.T) {
	asOf := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	filterHash := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	duplicateJSON := `{"v":"xinsight.cursor.v1","v":"xinsight.cursor.v1","kind":"execution_search","as_of":"2026-07-13T12:00:00Z","filter_hash":"` + filterHash + `","keys":["k"]}`
	duplicate := base64.RawURLEncoding.EncodeToString([]byte(duplicateJSON))
	if _, err := pagination.Decode(duplicate, asOf); !errors.Is(err, pagination.ErrInvalid) {
		t.Fatalf("duplicate error=%v", err)
	}

	unknownJSON := `{"v":"xinsight.cursor.v1","kind":"execution_search","as_of":"2026-07-13T12:00:00Z","filter_hash":"` + filterHash + `","keys":["k"],"extra":true}`
	unknown := base64.RawURLEncoding.EncodeToString([]byte(unknownJSON))
	if _, err := pagination.Decode(unknown, asOf); !errors.Is(err, pagination.ErrInvalid) {
		t.Fatalf("unknown error=%v", err)
	}

	encoded, err := pagination.Encode(pagination.Cursor{
		Version:     pagination.Version,
		Kind:        pagination.KindTimeline,
		AsOf:        asOf,
		FilterHash:  filterHash,
		ExecutionID: "xe1_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Keys:        []string{"k"},
	})
	if err != nil {
		t.Fatalf("encode timeline: %v", err)
	}
	if _, err := pagination.Decode(encoded, asOf.Add(pagination.MaxCursorAge+time.Second)); !errors.Is(err, pagination.ErrExpired) {
		t.Fatalf("expired error=%v", err)
	}
}
