package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

func GenerateEventHash(event map[string]any) (string, error) {
	normalized, err := cloneMap(event)
	if err != nil {
		return "", err
	}
	delete(normalized, "event_hash")
	delete(normalized, "ingested_at")

	bytes, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(bytes)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func cloneMap(in map[string]any) (map[string]any, error) {
	if in == nil {
		return nil, fmt.Errorf("nil map")
	}
	bytes, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(bytes, &out); err != nil {
		return nil, err
	}
	return out, nil
}
