package hostctl

import (
	"encoding/json"
	"strings"
	"testing"
)

const grantRequest = `{"schema_version":"xinsight.workbench-grant-change.v1","action":"create","grant_id":"11111111-1111-1111-1111-111111111111","subject_id":"xs_1111111111111111","source_system_id":"22222222-2222-2222-2222-222222222222","source_project_ref":"42","cost_project_ref":"*","expected_revision":0,"capabilities":["summary","details","budget_summary"]}`

func TestWorkbenchGrantRequestFailsClosed(t *testing.T) {
	if _, err := DecodeWorkbenchGrantChange([]byte(grantRequest)); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"budget with cost-project allocation": strings.Replace(grantRequest, `"cost_project_ref":"*"`, `"cost_project_ref":"cost-project-42"`, 1),
		"missing project":                     strings.Replace(grantRequest, `"source_project_ref":"42",`, "", 1),
		"null project":                        strings.Replace(grantRequest, `"source_project_ref":"42"`, `"source_project_ref":null`, 1),
		"empty project":                       strings.Replace(grantRequest, `"source_project_ref":"42"`, `"source_project_ref":""`, 1),
		"missing revision":                    strings.Replace(grantRequest, `"expected_revision":0,`, "", 1),
		"duplicate field":                     strings.Replace(grantRequest, `"expected_revision":0`, `"expected_revision":0,"expected_revision":0`, 1),
		"unknown field":                       strings.Replace(grantRequest, `"action":"create"`, `"extra":true,"action":"create"`, 1),
		"duplicate capability":                strings.Replace(grantRequest, `"summary","details"`, `"summary","summary"`, 1),
		"unknown capability":                  strings.Replace(grantRequest, `"details"`, `"admin"`, 1),
		"missing capabilities":                strings.Replace(grantRequest, `,"capabilities":["summary","details","budget_summary"]`, "", 1),
		"empty capabilities":                  strings.Replace(grantRequest, `["summary","details","budget_summary"]`, `[]`, 1),
		"update without revision":             strings.Replace(grantRequest, `"create"`, `"replace"`, 1),
		"trailing object":                     grantRequest + `{}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeWorkbenchGrantChange([]byte(raw)); err == nil {
				t.Fatal("accepted unsafe request")
			}
		})
	}
	revoke := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(grantRequest, `"create"`, `"revoke"`), `"expected_revision":0`, `"expected_revision":1`), `["summary","details","budget_summary"]`, `[]`)
	if _, err := DecodeWorkbenchGrantChange([]byte(revoke)); err != nil {
		t.Fatal(err)
	}
}

func TestWorkbenchGrantRequestExactWireKeys(t *testing.T) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(grantRequest), &fields); err != nil {
		t.Fatal(err)
	}
	for key, value := range fields {
		for _, alias := range []string{strings.ToUpper(key), strings.ToUpper(key[:1]) + key[1:]} {
			member := `"` + alias + `":` + string(value)
			for placement, raw := range map[string]string{
				"replacement": strings.Replace(grantRequest, `"`+key+`":`, `"`+alias+`":`, 1),
				"after":       strings.TrimSuffix(grantRequest, "}") + "," + member + "}",
				"before":      "{" + member + "," + strings.TrimPrefix(grantRequest, "{"),
			} {
				t.Run(key+"/"+alias+"/"+placement, func(t *testing.T) {
					if _, err := DecodeWorkbenchGrantChange([]byte(raw)); err == nil {
						t.Fatal("accepted non-contract or semantically duplicate field")
					}
				})
			}
		}
	}
	for name, raw := range map[string]string{
		"unicode-case-fold": strings.Replace(grantRequest, `"subject_id"`, `"ſubject_id"`, 1),
		"escaped-duplicate": strings.TrimSuffix(grantRequest, "}") + `,"source_\u0070roject_ref":"*"}`,
		"not-object":        "[" + grantRequest + "]",
		"null":              "null",
		"invalid-UTF8":      strings.Replace(grantRequest, `"42"`, "\""+string([]byte{0xff})+"\"", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeWorkbenchGrantChange([]byte(raw)); err == nil {
				t.Fatal("accepted ambiguous encoding")
			}
		})
	}
	// JSON escapes and whitespace preserve an exact decoded contract key.
	escaped := " \n" + strings.Replace(grantRequest, `"source_project_ref"`, `"source_\u0070roject_ref"`, 1) + "\t"
	if _, err := DecodeWorkbenchGrantChange([]byte(escaped)); err != nil {
		t.Fatal("valid escaped contract key rejected", err)
	}
}

func TestWorkbenchGrantV2ExplicitContent(t *testing.T) {
	base := strings.Replace(grantRequest, "workbench-grant-change.v1", "workbench-grant-change.v2", 1)
	base = strings.TrimSuffix(base, "}") + `,"allow_content":true,"allowed_content_purposes":["human_review"]}`
	contentOnly := strings.Replace(base, `["summary","details","budget_summary"]`, `[]`, 1)
	for _, raw := range []string{base, contentOnly, strings.Replace(strings.Replace(base, `"allow_content":true`, `"allow_content":false`, 1), `["human_review"]`, `[]`, 1)} {
		if _, err := DecodeWorkbenchGrantChange([]byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	for name, raw := range map[string]string{
		"missing bool":         strings.Replace(base, `,"allow_content":true`, "", 1),
		"missing purposes":     strings.Replace(base, `,"allowed_content_purposes":["human_review"]`, "", 1),
		"null bool":            strings.Replace(base, `"allow_content":true`, `"allow_content":null`, 1),
		"null purposes":        strings.Replace(base, `["human_review"]`, `null`, 1),
		"bool mismatch":        strings.Replace(base, `"allow_content":true`, `"allow_content":false`, 1),
		"unknown purpose":      strings.Replace(base, `human_review`, `read`, 1),
		"purpose alias":        strings.Replace(base, `human_review`, `Human_Review`, 1),
		"duplicate purpose":    strings.Replace(base, `["human_review"]`, `["human_review","human_review"]`, 1),
		"bool field alias":     strings.Replace(base, `"allow_content"`, `"ALLOW_CONTENT"`, 1),
		"purposes field alias": strings.Replace(base, `"allowed_content_purposes"`, `"Allowed_content_purposes"`, 1),
		"duplicate bool":       strings.Replace(base, `"allow_content":true`, `"allow_content":true,"allow_content":false`, 1),
		"v1 extension":         strings.Replace(base, "workbench-grant-change.v2", "workbench-grant-change.v1", 1),
		"v1 null extension":    strings.TrimSuffix(grantRequest, "}") + `,"allow_content":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeWorkbenchGrantChange([]byte(raw)); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestWorkbenchGrantV1BytesAndV2RoundTrip(t *testing.T) {
	old, err := DecodeWorkbenchGrantChange([]byte(grantRequest))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(old)
	if err != nil || string(raw) != grantRequest {
		t.Fatalf("legacy wire changed: %s %v", raw, err)
	}
	no := false
	old.SchemaVersion = "xinsight.workbench-grant-change.v2"
	old.AllowContent = &no
	old.AllowedContentPurposes = []string{}
	raw, err = json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeWorkbenchGrantChange(raw); err != nil {
		t.Fatalf("v2 empty purposes lost: %s %v", raw, err)
	}
}
