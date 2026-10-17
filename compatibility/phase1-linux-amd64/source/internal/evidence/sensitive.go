package evidence

import (
	"fmt"
	"strings"
)

type SensitiveFinding struct {
	Path   string
	Reason string
}

var sensitiveCredentialKeyFragments = []string{
	"access_token",
	"refresh_token",
	"secret",
	"password",
	"response_body",
	"request_body",
}

var sensitiveExactKeys = map[string]bool{
	"api_key":       true,
	"apikey":        true,
	"authorization": true,
	"completion":    true,
	"messages":      true,
	"prompt":        true,
	"request":       true,
	"response":      true,
}

var safeEvidenceMetadataKeys = map[string]bool{
	"api_key_id":         true,
	"api_key_ref":        true,
	"authorization_hash": true,
	"authorization_id":   true,
	"authorization_ref":  true,
	"completion_tokens":  true,
	"prompt_tokens":      true,
}

func DetectSensitive(value any) []SensitiveFinding {
	var findings []SensitiveFinding
	walkSensitive("$", value, &findings)
	return findings
}

func walkSensitive(path string, value any, findings *[]SensitiveFinding) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if isSensitiveKey(key) {
				*findings = append(*findings, SensitiveFinding{
					Path:   path + "." + key,
					Reason: "disallowed_payload_content",
				})
				return
			}
			walkSensitive(path+"."+key, child, findings)
			if len(*findings) > 0 {
				return
			}
		}
	case []any:
		for idx, child := range v {
			walkSensitive(fmt.Sprintf("%s[%d]", path, idx), child, findings)
			if len(*findings) > 0 {
				return
			}
		}
	}
}

func isSensitiveKey(key string) bool {
	normalized := normalizeSensitiveKey(key)
	if safeEvidenceMetadataKeys[normalized] {
		return false
	}
	if sensitiveExactKeys[normalized] {
		return true
	}
	if strings.Contains(normalized, "api_key") || strings.Contains(normalized, "apikey") || strings.Contains(normalized, "authorization") {
		return true
	}
	for _, fragment := range sensitiveCredentialKeyFragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	if containsRawPayloadSubject(normalized, "prompt") || containsRawPayloadSubject(normalized, "completion") {
		return true
	}
	for _, suffix := range []string{"_api_key", "_authorization", "_completion", "_messages", "_prompt", "_request", "_response"} {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	return false
}

func containsRawPayloadSubject(key string, subject string) bool {
	if !strings.Contains(key, subject) {
		return false
	}
	parts := strings.Split(key, "_")
	for _, dangerous := range []string{"body", "content", "data", "message", "messages", "text", "value"} {
		if containsString(parts, dangerous) {
			return true
		}
	}
	for _, safe := range []string{"cache", "count", "duration", "hash", "included", "latency", "length", "present", "redacted", "ref", "time", "token", "tokens"} {
		if containsString(parts, safe) {
			return false
		}
	}
	return true
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func normalizeSensitiveKey(key string) string {
	normalized := strings.ToLower(strings.TrimSpace(key))
	replacer := strings.NewReplacer("-", "_", ".", "_", " ", "_")
	return replacer.Replace(normalized)
}
