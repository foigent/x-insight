package executionid_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/foigent/x-insight/internal/executionid"
)

func TestExecutionIDGoldenAndSourceQualification(t *testing.T) {
	first, err := executionid.New("11111111-1111-1111-1111-111111111111", "req-prd2-001")
	if err != nil {
		t.Fatalf("new execution ID: %v", err)
	}
	const expected = "xe1_17caf65705120fd3ab9a9acb6664e45382d3835962a8d9cb4ba77762a7da6746"
	if first != expected {
		t.Fatalf("execution_id=%q want=%q", first, expected)
	}
	if !executionid.Valid(first) {
		t.Fatalf("generated execution ID is invalid: %q", first)
	}

	second, err := executionid.New("33333333-3333-3333-3333-333333333333", "req-prd2-001")
	if err != nil {
		t.Fatalf("new second execution ID: %v", err)
	}
	if first == second {
		t.Fatal("same request ID from different sources merged")
	}
}

func TestExecutionIDCanonicalizesUUIDCaseButNotRequestID(t *testing.T) {
	lower, err := executionid.New("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "Request-1")
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	upper, err := executionid.New("AAAAAAAA-AAAA-AAAA-AAAA-AAAAAAAAAAAA", "Request-1")
	if err != nil {
		t.Fatalf("upper: %v", err)
	}
	if lower != upper {
		t.Fatalf("UUID case changed identity: lower=%q upper=%q", lower, upper)
	}
	differentRequestCase, err := executionid.New("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "request-1")
	if err != nil {
		t.Fatalf("request case: %v", err)
	}
	if lower == differentRequestCase {
		t.Fatal("request ID was case-folded")
	}
}

func TestExecutionIDRejectsAmbiguousOrOversizedInputs(t *testing.T) {
	for _, testCase := range []struct {
		source  string
		request string
	}{
		{"11111111-1111-1111-1111-111111111111", " request"},
		{"11111111-1111-1111-1111-111111111111", "request\x00hidden"},
		{"11111111-1111-1111-1111-111111111111", string([]byte{utf8.RuneSelf, 0xff})},
		{"11111111-1111-1111-1111-111111111111", ""},
		{"not-a-uuid", "request"},
		{"11111111-1111-1111-1111-111111111111", strings.Repeat("a", executionid.MaxRequestIDBytes+1)},
	} {
		if _, err := executionid.New(testCase.source, testCase.request); err == nil {
			t.Fatalf("source=%q request length=%d unexpectedly accepted", testCase.source, len(testCase.request))
		}
	}
}

func TestRequestIDPrefixValidation(t *testing.T) {
	for _, value := range []string{"req", "请求-", "%_\\"} {
		if err := executionid.ValidateRequestIDPrefix(value); err != nil {
			t.Fatalf("prefix=%q error=%v", value, err)
		}
	}
	for _, value := range []string{"ab", " req", "req\nnext", strings.Repeat("a", 129)} {
		if err := executionid.ValidateRequestIDPrefix(value); err == nil {
			t.Fatalf("prefix=%q unexpectedly accepted", value)
		}
	}
}
