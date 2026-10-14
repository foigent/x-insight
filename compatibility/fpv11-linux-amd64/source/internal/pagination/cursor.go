package pagination

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"
)

const (
	Version        = "xinsight.cursor.v1"
	MaxCursorBytes = 2048
	MaxCursorAge   = 24 * time.Hour
)

type Kind string

const (
	KindExecutionSearch Kind = "execution_search"
	KindTimeline        Kind = "timeline"
	KindGaps            Kind = "gaps"
)

var (
	ErrInvalid         = errors.New("invalid cursor")
	ErrMismatch        = errors.New("cursor mismatch")
	ErrExpired         = errors.New("cursor expired")
	hashPattern        = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	executionIDPattern = regexp.MustCompile(`^xe1_[a-f0-9]{64}$`)
)

type Cursor struct {
	Version     string    `json:"v"`
	Kind        Kind      `json:"kind"`
	AsOf        time.Time `json:"as_of"`
	FilterHash  string    `json:"filter_hash"`
	ExecutionID string    `json:"execution_id,omitempty"`
	Keys        []string  `json:"keys"`
}

func Encode(cursor Cursor) (string, error) {
	if err := validateShape(cursor); err != nil {
		return "", err
	}
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("%w: encode", ErrInvalid)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	if len(encoded) > MaxCursorBytes {
		return "", fmt.Errorf("%w: token too large", ErrInvalid)
	}
	return encoded, nil
}

func Decode(value string, now time.Time) (Cursor, error) {
	if len(value) == 0 || len(value) > MaxCursorBytes {
		return Cursor{}, fmt.Errorf("%w: token length", ErrInvalid)
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return Cursor{}, fmt.Errorf("%w: base64", ErrInvalid)
	}
	if err := rejectDuplicateFields(payload); err != nil {
		return Cursor{}, err
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor Cursor
	if err := decoder.Decode(&cursor); err != nil {
		return Cursor{}, fmt.Errorf("%w: JSON", ErrInvalid)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Cursor{}, fmt.Errorf("%w: trailing content", ErrInvalid)
	}
	if err := validateShape(cursor); err != nil {
		return Cursor{}, err
	}

	now = now.UTC()
	if cursor.AsOf.After(now.Add(time.Minute)) {
		return Cursor{}, fmt.Errorf("%w: as_of is in the future", ErrInvalid)
	}
	if now.After(cursor.AsOf.Add(MaxCursorAge)) {
		return Cursor{}, ErrExpired
	}
	return cursor, nil
}

func ValidateBinding(cursor Cursor, kind Kind, filterHash string, executionID string) error {
	if cursor.Kind != kind || cursor.FilterHash != filterHash || cursor.ExecutionID != executionID {
		return ErrMismatch
	}
	return nil
}

func HashCanonicalFilters(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal canonical filters: %w", err)
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validateShape(cursor Cursor) error {
	if cursor.Version != Version {
		return fmt.Errorf("%w: version", ErrInvalid)
	}
	switch cursor.Kind {
	case KindExecutionSearch, KindTimeline, KindGaps:
	default:
		return fmt.Errorf("%w: kind", ErrInvalid)
	}
	_, offset := cursor.AsOf.Zone()
	if cursor.AsOf.IsZero() || offset != 0 || !hashPattern.MatchString(cursor.FilterHash) {
		return fmt.Errorf("%w: snapshot or filter hash", ErrInvalid)
	}
	if len(cursor.Keys) == 0 || len(cursor.Keys) > 3 {
		return fmt.Errorf("%w: ordering keys", ErrInvalid)
	}
	for _, key := range cursor.Keys {
		if len(key) == 0 || len(key) > 256 {
			return fmt.Errorf("%w: ordering key", ErrInvalid)
		}
	}
	if cursor.Kind == KindExecutionSearch && cursor.ExecutionID != "" {
		return fmt.Errorf("%w: unexpected execution binding", ErrInvalid)
	}
	if (cursor.Kind == KindTimeline || cursor.Kind == KindGaps) && !executionIDPattern.MatchString(cursor.ExecutionID) {
		return fmt.Errorf("%w: missing execution binding", ErrInvalid)
	}
	return nil
}

func rejectDuplicateFields(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return fmt.Errorf("%w: object", ErrInvalid)
	}
	seen := map[string]struct{}{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("%w: object key", ErrInvalid)
		}
		key, ok := keyToken.(string)
		if !ok {
			return fmt.Errorf("%w: object key", ErrInvalid)
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%w: duplicate field", ErrInvalid)
		}
		seen[key] = struct{}{}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return fmt.Errorf("%w: field value", ErrInvalid)
		}
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("%w: object close", ErrInvalid)
	}
	return nil
}
