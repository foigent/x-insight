package sourcefacts

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/buildinfo"
)

func TestSyntheticResourceAuthorizationSeparatesInternalAndUR(t *testing.T) {
	now := time.Now().UTC()
	a := OfflineAuthorization{SchemaVersion: ResourceOfflineAuthorizationVersion, AuthorizationID: "resource-test", Environment: "test", TargetRef: "target", MachineIDSHA256: Hash([]byte("machine")), ProductVersion: "dev", ProductCommit: "local", SourceSchemaSHA256: ResourceSchemaSHA256(), SourceSystemID: "11111111-1111-4111-8111-111111111111", SourceInstance: "reporter", OrgID: "1", ProjectID: "2", Profile: ResourceOfflineProfile, NotBefore: now.Add(-time.Minute).Format(TimeFormat), ExpiresAt: now.Add(time.Hour).Format(TimeFormat), BundlePath: "/synthetic/bundle.json", BundleSHA256: Hash([]byte("bundle")), BundleID: "bundle", OperationID: "operation", MaxRecords: 50, PayloadCaptureMode: "metadata_only", TargetDatabaseSchema: "37", TargetDatabaseName: "xi_synthetic", TargetDatabaseOID: "123", TargetDatabaseSystemID: "987654321", TargetSourceSystemID: "22222222-2222-4222-8222-222222222222", TargetSourceInstance: "target", TargetOrgID: "3", TargetProjectID: "4"}
	for _, schema := range append([]string{"37", "38", "39", "40", "41", "42", "43", "44"}, buildinfo.DatabaseSchemaMax) {
		v := a
		v.TargetDatabaseSchema = schema
		if err := v.Validate(now); err != nil {
			t.Fatalf("supported resource schema %s: %v", schema, err)
		}
	}
	raw, _ := json.Marshal(a)
	parsed, err := ParseOfflineAuthorization(raw, now)
	if err != nil || parsed.Digest() != a.Digest() {
		t.Fatal("strict v3 roundtrip", err)
	}
	for name, change := range map[string]func(*OfflineAuthorization){
		"UR-protocol":       func(x *OfflineAuthorization) { x.ProtocolVersion = "2.0.2" },
		"UR-profile":        func(x *OfflineAuthorization) { x.Profile = ContextProfile },
		"production":        func(x *OfflineAuthorization) { x.Environment = "production" },
		"database-missing":  func(x *OfflineAuthorization) { x.TargetDatabaseName = "" },
		"database-wildcard": func(x *OfflineAuthorization) { x.TargetDatabaseName = "*" },
		"OID-missing":       func(x *OfflineAuthorization) { x.TargetDatabaseOID = "" },
		"target-wildcard":   func(x *OfflineAuthorization) { x.TargetSourceInstance = "*" },
		"schema36":          func(x *OfflineAuthorization) { x.TargetDatabaseSchema = "36" },
		"future-schema46":   func(x *OfflineAuthorization) { x.TargetDatabaseSchema = "46" },
		"expired":           func(x *OfflineAuthorization) { x.ExpiresAt = now.Format(TimeFormat) },
		"too-long":          func(x *OfflineAuthorization) { x.ExpiresAt = now.Add(3 * time.Hour).Format(TimeFormat) },
		"schema-hash":       func(x *OfflineAuthorization) { x.SourceSchemaSHA256 = Hash([]byte("other")) },
	} {
		t.Run(name, func(t *testing.T) {
			v := a
			change(&v)
			encoded, _ := json.Marshal(v)
			if _, err := ParseOfflineAuthorization(encoded, now); err == nil {
				t.Fatal("invalid binding accepted")
			}
		})
	}
	for _, bad := range []string{strings.Replace(string(raw), `"target_database_name":"xi_synthetic"`, `"TARGET_DATABASE_NAME":"xi_synthetic"`, 1), strings.Replace(string(raw), `"target_database_name":"xi_synthetic"`, `"target_database_name":"xi_synthetic","TARGET_DATABASE_NAME":"other"`, 1), strings.Replace(string(raw), `"target_database_oid":"123"`, `"target_database_oid":null`, 1)} {
		if _, err := ParseOfflineAuthorization([]byte(bad), now); err == nil {
			t.Fatal("ambiguous authorization accepted")
		}
	}
	if _, err := ValidateOfflineBatch([]byte("bundle"), a, now); err == nil {
		t.Fatal("UR input decoder accepted XI internal contract")
	}
	legacy, _, _ := offlineVector(t)
	old, _ := json.Marshal(legacy)
	if strings.Contains(string(old), "target_database_name") {
		t.Fatal("v1 bytes changed")
	}
	legacy.TargetDatabaseName = "xi_synthetic"
	if err := legacy.Validate(now); err == nil {
		t.Fatal("v3 binding smuggled into v1")
	}
}

func TestActionAuthorizationIsDistinctAndTestOnly(t *testing.T) {
	now := time.Now().UTC()
	a := OfflineAuthorization{SchemaVersion: ResourceOfflineAuthorizationVersion, AuthorizationID: "action-test", Environment: "test", TargetRef: "target", MachineIDSHA256: Hash([]byte("machine")), ProductVersion: "dev", ProductCommit: "local", SourceSchemaSHA256: ActionSchemaSHA256(), SourceSystemID: "11111111-1111-4111-8111-111111111111", SourceInstance: "openclaw-test", OrgID: "1", ProjectID: "2", Profile: ActionOfflineProfile, NotBefore: now.Add(-time.Minute).Format(TimeFormat), ExpiresAt: now.Add(time.Hour).Format(TimeFormat), BundlePath: "/action/bundle.json", BundleSHA256: Hash([]byte("bundle")), BundleID: "bundle", OperationID: "operation", MaxRecords: 5, PayloadCaptureMode: "metadata_only", TargetDatabaseSchema: "40", TargetDatabaseName: "xi_action", TargetDatabaseOID: "123", TargetDatabaseSystemID: "987654321", TargetSourceSystemID: "22222222-2222-4222-8222-222222222222", TargetSourceInstance: "ur-test", TargetOrgID: "3", TargetProjectID: "4"}
	for _, schema := range append([]string{"40", "41", "42", "43", "44"}, buildinfo.DatabaseSchemaMax) {
		a.TargetDatabaseSchema = schema
		if err := a.Validate(now); err != nil {
			t.Fatalf("supported action schema %s: %v", schema, err)
		}
	}
	for _, mutate := range []func(*OfflineAuthorization){
		func(x *OfflineAuthorization) { x.Profile = ResourceOfflineProfile },
		func(x *OfflineAuthorization) { x.TargetDatabaseSchema = "39" },
		func(x *OfflineAuthorization) { x.TargetDatabaseSchema = "46" },
		func(x *OfflineAuthorization) { x.Environment = "integration" },
		func(x *OfflineAuthorization) { x.SourceSchemaSHA256 = ResourceSchemaSHA256() },
	} {
		bad := a
		mutate(&bad)
		if bad.Validate(now) == nil {
			t.Fatal("cross-profile or environment admission")
		}
	}
}
