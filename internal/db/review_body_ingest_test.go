package db

import "testing"

func TestOpenClawBodyDomainCannotClaimURWireBytes(t *testing.T) {
	for _, tc := range []struct {
		kind, domain string
		valid        bool
	}{
		{"model_input", "openclaw.llm_input.utf8.v1", true},
		{"model_input", "openclaw.llm_input.canonical-json.v1", true},
		{"tool_output", "openclaw.after_tool_call.canonical-json.v1", true},
		{"model_input", "unirouter.client_request.raw.v1", false},
		{"model_output", "openclaw.llm_input.utf8.v1", false},
		{"resource_after", "openclaw.after_tool_call.canonical-json.v1", false},
	} {
		if got := validOpenClawByteDomain(tc.kind, tc.domain); got != tc.valid {
			t.Fatalf("%s %s: valid=%t", tc.kind, tc.domain, got)
		}
	}
}
