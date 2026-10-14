package workspace

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEvidencePackIsDeterministicAndRedacted(t *testing.T) {
	sealedAt := time.Date(2026, 7, 31, 8, 0, 0, 123000000, time.UTC)
	record := CaseRecord{
		Case: Case{
			ID:          "xc_AAAAAAAAAAAAAAAAAAAAAA",
			Title:       "Fallback investigation",
			Description: "Evidence-backed recovery review",
		},
		Executions: []CaseExecution{
			{CaseID: "xc_AAAAAAAAAAAAAAAAAAAAAA", ExecutionID: "xe1_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", AsOf: sealedAt},
			{CaseID: "xc_AAAAAAAAAAAAAAAAAAAAAA", ExecutionID: "xe1_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", AsOf: sealedAt},
		},
		Evidence: []EvidenceLink{
			{
				CaseID: "xc_AAAAAAAAAAAAAAAAAAAAAA", EvidenceEventID: "22222222-2222-4222-8222-222222222222",
				Relation: "supports", EventHashSnapshot: "sha256:" + strings.Repeat("b", 64),
				SourceRefSnapshot: "fixture://second",
			},
			{
				CaseID: "xc_AAAAAAAAAAAAAAAAAAAAAA", FindingID: "xf_BBBBBBBBBBBBBBBBBBBBBB", EvidenceEventID: "11111111-1111-4111-8111-111111111111",
				Relation: "supports", EventHashSnapshot: "sha256:" + strings.Repeat("a", 64),
				SourceRefSnapshot: "fixture://first",
			},
			{
				CaseID: "xc_AAAAAAAAAAAAAAAAAAAAAA", FindingID: "xf_AAAAAAAAAAAAAAAAAAAAAA", EvidenceEventID: "11111111-1111-4111-8111-111111111111",
				Relation: "supports", EventHashSnapshot: "sha256:" + strings.Repeat("a", 64),
				SourceRefSnapshot: "fixture://first",
			},
		},
		Alerts: []AlertCaseLink{
			{
				AlertID: "xal_" + strings.Repeat("b", 64), CaseID: "xc_AAAAAAAAAAAAAAAAAAAAAA",
				AlertState: "resolved", LinkedBySubject: "xs_0123456789abcdef", LinkedAt: sealedAt,
			},
			{
				AlertID: "xal_" + strings.Repeat("a", 64), CaseID: "xc_AAAAAAAAAAAAAAAAAAAAAA",
				AlertState: "open", LinkedBySubject: "xs_0123456789abcdef", LinkedAt: sealedAt,
			},
		},
	}
	first, firstBytes, firstDigest, err := BuildEvidencePack(record, sealedAt, "xs_0123456789abcdef", 1)
	if err != nil {
		t.Fatal(err)
	}
	_, secondBytes, secondDigest, err := BuildEvidencePack(record, sealedAt, "xs_0123456789abcdef", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBytes, secondBytes) || firstDigest != secondDigest {
		t.Fatalf("pack is not deterministic: %q %q", firstDigest, secondDigest)
	}
	if first.Executions[0].ExecutionID >= first.Executions[1].ExecutionID ||
		first.Evidence[0].EvidenceEventID != first.Evidence[1].EvidenceEventID ||
		first.Evidence[0].FindingID >= first.Evidence[1].FindingID ||
		first.Evidence[1].EvidenceEventID >= first.Evidence[2].EvidenceEventID ||
		first.Alerts[0].AlertID >= first.Alerts[1].AlertID {
		t.Fatalf("pack arrays are not canonicalized: %+v", first)
	}
	if bytes.Contains(firstBytes, []byte("raw_prompt")) || bytes.Contains(firstBytes, []byte("credential")) {
		t.Fatalf("restricted content escaped into pack: %s", firstBytes)
	}
	if !bytes.Contains(firstBytes, []byte(`"user_content_policy"`)) ||
		!bytes.Contains(firstBytes, []byte("included verbatim")) {
		t.Fatalf("user-authored content boundary is missing: %s", firstBytes)
	}
	html, err := RenderEvidencePackHTML(first, firstDigest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(html, []byte(firstDigest)) || bytes.Contains(html, []byte("<script")) {
		t.Fatalf("unexpected HTML pack output")
	}
	for _, language := range []string{"zh-CN", "en-US", "unsupported"} {
		localized, err := RenderEvidencePackHTMLInLanguage(first, firstDigest, language)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(localized, []byte(firstDigest)) || !bytes.Contains(localized, []byte(first.Title)) {
			t.Fatal("language changed evidence identity or user title")
		}
		if language != "zh-CN" && !bytes.Equal(localized, html) {
			t.Fatal("English fallback differs")
		}
		if language == "zh-CN" && !bytes.Contains(localized, []byte("范围与脱敏")) {
			t.Fatal("Chinese presentation missing")
		}
	}
	after, err := json.Marshal(first)
	if err != nil || !bytes.Equal(after, firstBytes) {
		t.Fatal("rendering changed sealed JSON")
	}
}

func TestSavedViewRejectsUnknownFilterKeys(t *testing.T) {
	invalid := []json.RawMessage{
		json.RawMessage(`{"outcome":"failure","query":"raw"}`),
		json.RawMessage(`{"outcome":"succeeded"}`),
		json.RawMessage(`{"has_gaps":"true"}`),
		json.RawMessage(`{"request_id":"a","request_id_prefix":"abc"}`),
		json.RawMessage(`{"source_system_id":"not-a-uuid"}`),
		json.RawMessage(`{"outcome":null}`),
		json.RawMessage(`{"from":"2026-08-02T00:00:00Z","to":"2026-08-01T00:00:00Z"}`),
		json.RawMessage(`null`),
	}
	for index, filters := range invalid {
		if err := ValidateSavedView(CreateSavedViewInput{
			Name: "Failures", Filters: filters, Presentation: "explorer",
		}); err == nil {
			t.Fatalf("invalid saved-view filters %d were accepted: %s", index, filters)
		}
	}
	if err := ValidateSavedView(CreateSavedViewInput{
		Name: "Failures", Filters: json.RawMessage(`{"outcome":"failure","has_gaps":true,"from":"2026-08-01T00:00:00Z","to":"2026-08-02T00:00:00Z"}`), Presentation: "explorer",
	}); err != nil {
		t.Fatalf("valid saved view rejected: %v", err)
	}
}

func TestNewIDUsesOpaqueBoundedIdentity(t *testing.T) {
	id, err := NewID("xc_", bytes.NewReader(make([]byte, 16)))
	if err != nil {
		t.Fatal(err)
	}
	if id != "xc_AAAAAAAAAAAAAAAAAAAAAA" {
		t.Fatalf("id=%q", id)
	}
}
