package workspace

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResponsibilityCommandRequiresCompleteFinitePayload(t *testing.T) {
	valid := `{"operation":"update_followup","operation_id":"12345678-1234-4234-8234-123456789abc","expected_version":0,"expected_case_version":2,"payload":{"assignee_subject_id":null,"due_at":null,"due_timezone":null,"reason":"Keep unassigned while awaiting responsibility decision"}}`
	var c FollowupCommand
	if err := json.Unmarshal([]byte(valid), &c); err != nil {
		t.Fatal(err)
	}
	if _, err := NormalizeFollowupCommand(c); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"reason":"missing nullable fields"}`,
		`{"assignee_subject_id":null,"due_at":null,"due_timezone":null,"reason":"x","actor":"forged"}`,
		`null`,
	} {
		var p FollowupPayload
		if json.Unmarshal([]byte(raw), &p) == nil {
			t.Fatalf("accepted incomplete payload: %s", raw)
		}
	}
	for _, field := range []string{`"expected_version":0`, `"expected_case_version":2`} {
		var invalid FollowupCommand
		if json.Unmarshal([]byte(strings.Replace(valid, field, strings.Split(field, ":")[0]+":null", 1)), &invalid) == nil {
			t.Fatal("accepted null version")
		}
	}
	for _, operation := range []string{"record_review", "verified_fix", "end", "reopen"} {
		c.Operation = operation
		if _, err := NormalizeFollowupCommand(c); err == nil {
			t.Fatalf("unsupported action %s", operation)
		}
	}
}

func TestResponsibilityDeadlineUsesExplicitZoneAndDSTOffset(t *testing.T) {
	for _, tc := range []struct {
		at, zone string
		valid    bool
	}{
		{"2026-09-09T20:00:00+08:00", "Asia/Shanghai", true},
		{"2026-09-09T20:00:00Z", "Asia/Shanghai", false},
		{"2026-03-08T02:30:00-05:00", "America/New_York", false},
		{"2026-11-01T01:30:00-04:00", "America/New_York", true},
		{"2026-11-01T01:30:00-05:00", "America/New_York", true},
		{"2026-09-09T20:00:00Z", "Local", false},
	} {
		t.Run(tc.at+tc.zone, func(t *testing.T) {
			raw := `{"operation":"update_followup","operation_id":"12345678-1234-4234-8234-123456789abc","expected_version":0,"expected_case_version":2,"payload":{"assignee_subject_id":null,"due_at":"` + tc.at + `","due_timezone":"` + tc.zone + `","reason":"deadline"}}`
			var c FollowupCommand
			if err := json.Unmarshal([]byte(raw), &c); err != nil {
				t.Fatal(err)
			}
			got, err := NormalizeFollowupCommand(c)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
			if err == nil {
				again, e := NormalizeFollowupCommand(got)
				if e != nil {
					t.Fatal(e)
				}
				a, _ := json.Marshal(got)
				b, _ := json.Marshal(again)
				if string(a) != string(b) {
					t.Fatal("normalization changed retry digest")
				}
			}
		})
	}
}
