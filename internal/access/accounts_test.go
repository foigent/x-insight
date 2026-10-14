package access

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccountConfigIsStrictCanonicalAndProviderIsFailClosed(t *testing.T) {
	config := validAccountConfig(t)
	contents, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseAccountConfig(contents)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	provider, err := NewLocalProvider(parsed)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	principal, ok := provider.Authenticate("  INVESTIGATOR.ONE ", []byte("correct horse battery staple"))
	if !ok || principal.SubjectID != "xs_0123456789abcdef" || principal.Role != RoleInvestigator {
		t.Fatalf("principal=%+v accepted=%t", principal, ok)
	}
	for _, attempt := range []struct {
		username string
		password string
	}{
		{"investigator.one", "wrong password"},
		{"unknown.user", "correct horse battery staple"},
		{"disabled.operator", "another valid password"},
	} {
		if _, accepted := provider.Authenticate(attempt.username, []byte(attempt.password)); accepted {
			t.Fatalf("invalid authentication accepted for %q", attempt.username)
		}
	}
}

func TestAccountConfigRejectsUnknownDuplicateAndNonCanonicalFields(t *testing.T) {
	config := validAccountConfig(t)
	contents, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	withUnknown := bytes.Replace(contents, []byte(`"schema_version"`), []byte(`"unexpected":true,"schema_version"`), 1)
	if _, err := ParseAccountConfig(withUnknown); err == nil {
		t.Fatal("unknown field was accepted")
	}
	withDuplicate := bytes.Replace(
		contents,
		[]byte(`"username":"investigator.one"`),
		[]byte(`"username":"investigator.one","username":"operator.one"`),
		1,
	)
	if _, err := ParseAccountConfig(withDuplicate); err == nil {
		t.Fatal("duplicate object field was accepted")
	}

	config.Accounts[1].Username = config.Accounts[0].Username
	duplicate, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseAccountConfig(duplicate); err == nil {
		t.Fatal("duplicate normalized username was accepted")
	}

	config = validAccountConfig(t)
	config.Accounts[0].Username = "Investigator.One"
	nonCanonical, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseAccountConfig(nonCanonical); err == nil {
		t.Fatal("non-canonical username was accepted")
	}
	if _, err := ParseAccountConfig(append(contents, []byte("\n{}")...)); err == nil {
		t.Fatal("trailing JSON was accepted")
	}
	withoutDisabled := bytes.Replace(contents, []byte(`,"disabled":false`), nil, 1)
	if _, err := ParseAccountConfig(withoutDisabled); err == nil {
		t.Fatal("account without required disabled state was accepted")
	}
	nullDisabled := bytes.Replace(contents, []byte(`"disabled":false`), []byte(`"disabled":null`), 1)
	if _, err := ParseAccountConfig(nullDisabled); err == nil {
		t.Fatal("account with null disabled state was accepted")
	}
}

func TestWorkbenchReaderRequiresV2AndDoesNotChangeExistingRolePermissions(t *testing.T) {
	config := validAccountConfig(t)
	config.Accounts[0].Role = RoleWorkbenchReader
	contents, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseAccountConfig(contents); err == nil {
		t.Fatal("v1 account configuration accepted the new workbench role")
	}
	config.SchemaVersion = AccountConfigSchemaV2
	contents, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseAccountConfig(contents); err != nil {
		t.Fatalf("v2 account configuration rejected the workbench role: %v", err)
	}
	if got := Permissions(RoleWorkbenchReader); !equalPermissions(got, []Permission{
		PermissionWorkbenchSummary,
		PermissionWorkbenchDetails,
		PermissionWorkbenchCoverage,
		PermissionWorkbenchPreferences,
	}) {
		t.Fatalf("workbench permissions=%v", got)
	}
	if HasPermission(RoleInvestigator, PermissionWorkbenchSummary) ||
		HasPermission(RolePlatformAdmin, PermissionWorkbenchSummary) {
		t.Fatal("existing roles silently gained workbench authority")
	}
	if got := Permissions(RolePlatformAdmin); !equalPermissions(got, []Permission{
		PermissionEvidenceRead,
		PermissionInvestigationPreferences,
		PermissionEvidenceCite,
		PermissionInvestigationWrite,
		PermissionAccessSessionsRead,
		PermissionAccessSessionsRevoke,
		PermissionServiceIdentitiesRead,
		PermissionServiceIdentitiesRevoke,
		PermissionConnectorInstallationsManage,
		PermissionReviewPolicyManage,
	}) {
		t.Fatalf("platform_admin permissions changed=%v", got)
	}
}

func TestInvestigationReaderCanReadAndSaveOwnFiltersAndRequiresV2(t *testing.T) {
	config := validAccountConfig(t)
	config.Accounts[0].Role = RoleInvestigationReader
	raw, _ := json.Marshal(config)
	if _, err := ParseAccountConfig(raw); err == nil {
		t.Fatal("v1 unexpectedly accepted a new role")
	}
	config.SchemaVersion = AccountConfigSchemaV2
	raw, _ = json.Marshal(config)
	parsed, err := ParseAccountConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewLocalProvider(parsed)
	if err != nil {
		t.Fatal(err)
	}
	principal, ok := provider.Authenticate("investigator.one", []byte("correct horse battery staple"))
	if !ok || principal.Role != RoleInvestigationReader || !equalPermissions(Permissions(principal.Role), []Permission{PermissionEvidenceRead, PermissionInvestigationPreferences}) {
		t.Fatal("reader authentication or exact permission boundary")
	}
}

func equalPermissions(left, right []Permission) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestSystemdAccountCredentialLoadRequiresRestrictedRegularFileAndMinimizedAudit(t *testing.T) {
	config := validAccountConfig(t)
	contents, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	directory := persistentAccessTestDirectory(t)
	path := filepath.Join(directory, AccountCredential)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CREDENTIALS_DIRECTORY", directory)
	var audit bytes.Buffer
	provider, err := LoadLocalProvider(NewWriterAuditor(&audit))
	if err != nil || provider == nil {
		t.Fatalf("load provider: %v", err)
	}
	if !strings.Contains(audit.String(), `"event":"access.config_load"`) ||
		strings.Contains(audit.String(), config.Accounts[0].Username) ||
		strings.Contains(audit.String(), config.Accounts[0].PasswordHash) {
		t.Fatalf("unsafe audit=%s", audit.String())
	}

	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	audit.Reset()
	if _, err := LoadLocalProvider(NewWriterAuditor(&audit)); err == nil {
		t.Fatal("world-readable account credential was accepted")
	}
	if !strings.Contains(audit.String(), `"event":"access.config_reject"`) ||
		strings.Contains(audit.String(), config.Accounts[0].Username) ||
		strings.Contains(audit.String(), config.Accounts[0].PasswordHash) {
		t.Fatalf("unsafe reject audit=%s", audit.String())
	}
}

func TestAccountCredentialRejectsLinksAndOpenTimePathReplacement(t *testing.T) {
	config := validAccountConfig(t)
	contents, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	directory := persistentAccessTestDirectory(t)
	path := filepath.Join(directory, "accounts.json")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	hardLink := filepath.Join(directory, "accounts-hard-link.json")
	if err := os.Link(path, hardLink); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAndValidateAccountFile(path); err == nil {
		t.Fatal("hard-linked account credential was accepted")
	}

	symlink := filepath.Join(directory, "accounts-symlink.json")
	if err := os.Symlink(path, symlink); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAndValidateAccountFile(symlink); err == nil {
		t.Fatal("symlinked account credential was accepted")
	}

	replacementPath := filepath.Join(directory, "accounts-replacement.json")
	if err := os.WriteFile(replacementPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	openedPath := filepath.Join(directory, "accounts-opened.json")
	if err := os.WriteFile(openedPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	previousPath := filepath.Join(directory, "accounts-opened.previous.json")
	_, err = readRestrictedAccountFileWithHook(openedPath, func() {
		if renameErr := os.Rename(openedPath, previousPath); renameErr != nil {
			t.Fatalf("move opened credential: %v", renameErr)
		}
		if renameErr := os.Rename(replacementPath, openedPath); renameErr != nil {
			t.Fatalf("move replacement credential: %v", renameErr)
		}
	})
	if err == nil {
		t.Fatal("account path replacement during open was accepted")
	}
}

func validAccountConfig(t *testing.T) AccountConfig {
	t.Helper()
	first, err := DerivePasswordHash([]byte("correct horse battery staple"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := DerivePasswordHash([]byte("another valid password"))
	if err != nil {
		t.Fatal(err)
	}
	return AccountConfig{
		SchemaVersion: AccountConfigSchema,
		Accounts: []AccountConfigEntry{
			{
				SubjectID:    "xs_0123456789abcdef",
				Username:     "investigator.one",
				Role:         RoleInvestigator,
				PasswordHash: first,
			},
			{
				SubjectID:    "xs_fedcba9876543210",
				Username:     "disabled.operator",
				Role:         RoleOperator,
				Disabled:     true,
				PasswordHash: second,
			},
		},
	}
}

func persistentAccessTestDirectory(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp("", "xinsight-access-test-")
	if err != nil {
		t.Fatal(err)
	}
	return directory
}

func TestScopedRolesRequireV3WithoutGlobalInvestigationPermissions(t *testing.T) {
	config := validAccountConfig(t)
	for _, role := range []Role{RoleScopedReader, RoleScopedReviewer, RolePurposeReviewer} {
		config.Accounts[0].Role = role
		for _, schema := range []string{AccountConfigSchema, AccountConfigSchemaV2, AccountConfigSchemaV3} {
			config.SchemaVersion = schema
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := ParseAccountConfig(raw)
			if schema != AccountConfigSchemaV3 {
				if err == nil {
					t.Fatalf("legacy schema accepted %s", role)
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			provider, err := NewLocalProvider(parsed)
			if err != nil {
				t.Fatal(err)
			}
			principal, ok := provider.Authenticate("investigator.one", []byte("correct horse battery staple"))
			if !ok || principal.Role != role {
				t.Fatal("formal role changed")
			}
			for _, global := range []Permission{PermissionEvidenceRead, PermissionEvidenceCite, PermissionInvestigationWrite} {
				if HasPermission(role, global) {
					t.Fatalf("%s got global %s", role, global)
				}
			}
			if HasPermission(role, PermissionPurposeReview) != (role == RolePurposeReviewer) {
				t.Fatal("purpose authority mixed with Case authority")
			}
			if !HasPermission(role, PermissionScopedRead) || !HasPermission(role, PermissionWorkbenchSummary) || HasPermission(role, PermissionScopedWrite) != (role == RoleScopedReviewer) {
				t.Fatal("scoped capability mismatch")
			}
		}
	}
}
