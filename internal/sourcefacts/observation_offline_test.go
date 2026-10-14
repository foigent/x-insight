package sourcefacts

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/buildinfo"
)

func TestObservationOfflineFiveProfilesAndLegacyIdentity(t *testing.T) {
	if ObservationOfflineDatabaseSchema != buildinfo.DatabaseSchemaMax {
		t.Fatal("observation authorization default differs from current release schema")
	}
	a, _, _ := offlineVector(t)
	legacy, _ := json.Marshal(a)
	if bytes.Contains(legacy, []byte("target_database_schema")) {
		t.Fatal("A v1 serialized identity changed")
	}
	parsed, err := ParseOfflineAuthorization(legacy, time.Now())
	if err != nil || parsed.Digest() != Hash(legacy) {
		t.Fatal("A v1 digest changed", err)
	}
	valid, _ := observationVectors(t)
	seen := map[string]bool{}
	for _, vector := range valid {
		d, err := DecodeObservation([]byte(vector.Canonical))
		if err != nil {
			t.Fatal(err)
		}
		if seen[d.Profile] {
			continue
		}
		seen[d.Profile] = true
		t.Run(d.Profile, func(t *testing.T) {
			wire, err := ObservationEnvelope([]byte(vector.Canonical))
			if err != nil {
				t.Fatal(err)
			}
			p := Page{BatchVersion: "foigent.event-batch.v1", ConnectorID: ConnectorID, ConnectorVersion: ObservationConnectorVersion, ProducerInstance: d.SourceInstance, GeneratedAt: time.Now().UTC().Format(TimeFormat), Events: []json.RawMessage{wire}}
			raw, _ := json.Marshal(p)
			grant := a
			grant.SchemaVersion = ObservationOfflineAuthorizationVersion
			grant.TargetDatabaseSchema = ObservationOfflineDatabaseSchema
			grant.SourceSchemaSHA256 = Hash(observationSchemaBytes)
			grant.SourceInstance, grant.OrgID, grant.ProjectID, grant.Profile = d.SourceInstance, d.OrgID, d.ProjectID, d.Profile
			grant.BundleSHA256 = Hash(raw)
			for _, schema := range append([]string{"35", "36", "37", "38", "39", "40", "41", "42", "43", "44"}, buildinfo.DatabaseSchemaMax) {
				v := grant
				v.TargetDatabaseSchema = schema
				if err := v.Validate(time.Now()); err != nil {
					t.Fatalf("supported observation schema %s: %v", schema, err)
				}
			}
			auth, _ := json.Marshal(grant)
			got, err := ParseOfflineAuthorization(auth, time.Now())
			if err != nil || got.Digest() != grant.Digest() {
				t.Fatal("B explicit authorization", err)
			}
			batch, err := ValidateOfflineBatch(raw, got, time.Now())
			if err != nil || len(batch.Items) != 0 || len(batch.Observations) != 1 || batch.Count() != 1 || !bytes.Equal(batch.Observations[0].SourceBytes, []byte(vector.Canonical)) {
				t.Fatal("B portable batch", err)
			}
			for name, change := range map[string]func(*OfflineAuthorization){
				"A-format": func(v *OfflineAuthorization) {
					v.SchemaVersion = OfflineAuthorizationVersion
					v.TargetDatabaseSchema = ""
				},
				"A-schema":              func(v *OfflineAuthorization) { v.SourceSchemaSHA256 = Hash(schemaBytes) },
				"A-profile":             func(v *OfflineAuthorization) { v.Profile = ContextProfile },
				"wrong-target-schema":   func(v *OfflineAuthorization) { v.TargetDatabaseSchema = "32" },
				"prior-B-target-schema": func(v *OfflineAuthorization) { v.TargetDatabaseSchema = "33" },
				"pre-resource-schema":   func(v *OfflineAuthorization) { v.TargetDatabaseSchema = "34" },
				"future-target-schema":  func(v *OfflineAuthorization) { v.TargetDatabaseSchema = "46" },
				"missing-target-schema": func(v *OfflineAuthorization) { v.TargetDatabaseSchema = "" },
				"other-project":         func(v *OfflineAuthorization) { v.ProjectID = "999" },
				"other-instance":        func(v *OfflineAuthorization) { v.SourceInstance = "other-instance" },
			} {
				wrong := grant
				change(&wrong)
				if _, err := ValidateOfflineBatch(raw, wrong, time.Now()); err == nil {
					t.Fatal(name, "accepted")
				}
			}
			for _, bad := range []string{
				strings.Replace(string(auth), `,"target_database_schema":"`+ObservationOfflineDatabaseSchema+`"`, "", 1),
				strings.Replace(string(auth), `"target_database_schema":"`+ObservationOfflineDatabaseSchema+`"`, `"target_database_schema":null`, 1),
				strings.Replace(string(auth), `"target_database_schema":"`+ObservationOfflineDatabaseSchema+`"`, `"target_database_schema":"`+ObservationOfflineDatabaseSchema+`","target_database_schema":"`+ObservationOfflineDatabaseSchema+`"`, 1),
			} {
				if _, err := ParseOfflineAuthorization([]byte(bad), time.Now()); err == nil {
					t.Fatal("omitted/null/duplicate target schema accepted")
				}
			}
			p.ConnectorVersion = ConnectorVersion
			wrong, _ := json.Marshal(p)
			grant.BundleSHA256 = Hash(wrong)
			if _, err := ValidateOfflineBatch(wrong, grant, time.Now()); err == nil {
				t.Fatal("A connector version accepted for B")
			}
		})
	}
	if len(seen) != 5 {
		t.Fatal("missing observation profile coverage", seen)
	}
	// Even an empty or null B-only field must not alter a legacy grant's wire.
	for _, field := range []string{`"target_database_schema":""`, `"target_database_schema":null`, `"target_database_schema":"35"`} {
		raw := append(append([]byte{}, legacy[:len(legacy)-1]...), []byte(","+field+"}")...)
		if _, err := ParseOfflineAuthorization(raw, time.Now()); err == nil {
			t.Fatal("legacy format accepted B-only field")
		}
	}
}
