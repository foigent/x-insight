package access

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLocalSubjectDirectoryNeverReturnsCredentialOrRole(t *testing.T) {
	provider := &LocalProvider{accounts: map[string]credentialAccount{
		"reader.one":   {principal: Principal{SubjectID: "xs_0000000000000002", Role: RoleWorkbenchReader}},
		"disabled.one": {principal: Principal{SubjectID: "xs_0000000000000001", Role: RoleInvestigator}, disabled: true},
	}}
	items := provider.LocalSubjects()
	if len(items) != 2 || items[0].State != "disabled" || items[1].State != "active" {
		t.Fatalf("unexpected directory: %+v", items)
	}
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"password", "hash", "role", "session", "token"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("directory exposes %s", forbidden)
		}
	}
	items[0].DisplayName = "changed"
	if provider.LocalSubjects()[0].DisplayName != "disabled.one" {
		t.Fatal("caller modified provider state")
	}
}
