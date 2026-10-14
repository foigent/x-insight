package evidence

import "testing"

func TestDetectSensitiveAllowsGovernedMetadataCountsAndReferences(t *testing.T) {
	for _, key := range []string{
		"api_keys_included",
		"api_key_id",
		"api_key_ref",
		"authorization_hash",
		"authorization_id",
		"authorization_ref",
		"charged_completion_tokens",
		"completion_latency_ms",
		"completion_tokens",
		"prompt_cache_hit",
		"prompt_tokens",
		"raw_prompt_present",
	} {
		findings := DetectSensitive(map[string]any{key: 1})
		if len(findings) != 0 {
			t.Fatalf("safe metadata key %q produced findings: %+v", key, findings)
		}
	}
}

func TestDetectSensitiveBlocksRawPayloadAndCredentialKeys(t *testing.T) {
	for _, key := range []string{
		"api_key",
		"Authorization",
		"authorization_header",
		"client_secret",
		"input_messages",
		"model_completion",
		"openai_api_key_value",
		"prompt_content",
		"prompt_text",
		"prompt_tokens_and_prompt_text",
		"raw_prompt",
		"request_body",
		"response_body",
		"user_password",
	} {
		findings := DetectSensitive(map[string]any{key: "blocked"})
		if len(findings) != 1 {
			t.Fatalf("sensitive key %q findings=%+v", key, findings)
		}
		if findings[0].Reason != "disallowed_payload_content" {
			t.Fatalf("sensitive key %q reason=%q", key, findings[0].Reason)
		}
	}
}

func TestDetectSensitiveNormalizesKeySeparators(t *testing.T) {
	for _, key := range []string{"response-body", "request.body", "user password"} {
		if findings := DetectSensitive(map[string]any{key: "blocked"}); len(findings) != 1 {
			t.Fatalf("normalized sensitive key %q findings=%+v", key, findings)
		}
	}
}
