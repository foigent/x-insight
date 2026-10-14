package sourcefacts

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/connector"
)

type observationVector struct {
	Name       string `json:"name"`
	Canonical  string `json:"canonical_utf8"`
	SourceHash string `json:"source_sha256"`
}

func observationVectors(t *testing.T) (valid, invalid []observationVector) {
	t.Helper()
	raw, err := os.ReadFile("contract/observations/golden-observations.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Valid, Invalid []observationVector }
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture.Valid, fixture.Invalid
}

func observationWire(t *testing.T, name string) []byte {
	t.Helper()
	valid, _ := observationVectors(t)
	for _, v := range valid {
		if v.Name == name {
			return []byte(v.Canonical)
		}
	}
	t.Fatalf("missing frozen vector %q", name)
	return nil
}

func TestObservationFrozenVectorsAndAIsolation(t *testing.T) {
	valid, invalid := observationVectors(t)
	if len(valid) != 10 || len(invalid) != 9 {
		t.Fatal("frozen vector coverage changed")
	}
	for _, v := range valid {
		t.Run(v.Name, func(t *testing.T) {
			raw := []byte(v.Canonical)
			d, err := DecodeObservation(raw)
			if err != nil || Hash(raw) != v.SourceHash {
				t.Fatalf("source validation: %v", err)
			}
			if _, err := DecodeDocument(raw); err == nil {
				t.Fatal("B accepted as A")
			}
			wire, err := ObservationEnvelope(raw)
			if err != nil {
				t.Fatal(err)
			}
			item, err := ValidateObservationItem(wire, d.Profile)
			if err != nil || !bytes.Equal(item.SourceBytes, raw) || item.SourceHash != v.SourceHash {
				t.Fatalf("envelope validation: %v", err)
			}
			if _, err := ValidateItem(wire, d.Profile); err == nil {
				t.Fatal("A envelope decoder accepted B")
			}
			other := RuntimeProfile
			if d.Profile == RuntimeProfile {
				other = AttemptProfile
			}
			if _, err := ValidateObservationItem(wire, other); err == nil {
				t.Fatal("wrong profile accepted")
			}
		})
	}
	for _, v := range invalid {
		t.Run(v.Name, func(t *testing.T) {
			if _, err := DecodeObservation([]byte(v.Canonical)); err == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
	if Hash(observationSchemaBytes) != "sha256:ca0aabfb0c7b40c00cfb910e6bd255691326883b98b38b174afc86f2c4df2260" {
		t.Fatal("observation schema identity changed")
	}
}

func mutateObservation(t *testing.T, name string, change func(map[string]any)) []byte {
	t.Helper()
	m, err := connector.DecodeStrictJSONObject(observationWire(t, name))
	if err != nil {
		t.Fatal(err)
	}
	change(m)
	// Rebind identities so relationship tests do not merely fail an old hash.
	parts := []string{ObservationVersion}
	for _, key := range []string{"source_instance", "org_id", "project_id", "profile", "request_id", "subject_kind", "subject_id", "observation_kind"} {
		parts = append(parts, Text(m, key))
	}
	m["source_fact_id"] = SourceID(parts...)
	m["event_id"] = SourceID(ObservationVersion+".revision", Text(m, "source_fact_id"), Text(m, "revision"))
	wire, err := connector.CanonicalizeJSONValue(m)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func TestObservationRejectsDomainAndByteBoundaryViolations(t *testing.T) {
	cases := []struct {
		name, vector string
		change       func(map[string]any)
	}{
		{"int64-overflow", "request_started", func(m map[string]any) { m["org_id"] = "9223372036854775808" }},
		{"calendar-time", "request_started", func(m map[string]any) { m["occurred_at"] = "2026-02-30T00:00:00.000000Z" }},
		{"request-subject", "request_started", func(m map[string]any) { m["subject_id"] = "another-request" }},
		{"A-stage", "request_started", func(m map[string]any) { m["stage"] = "recorded" }},
		{"wrong-attempt", "attempt_registered", func(m map[string]any) { Object(m, "attempt")["attempt_id"] = "another-attempt" }},
		{"route-order", "attempt_registered", func(m map[string]any) { a := Object(m, "attempt"); a["route_sequence"] = "2"; a["route_total"] = "1" }},
		{"unauthorized-retry", "attempt_registered", func(m map[string]any) { Object(m, "attempt")["retry_authorized"] = true }},
		{"utf8-byte-limit", "selected_capability", func(m map[string]any) { Object(m, "capability")["model_ref"] = strings.Repeat("模", 43) }},
		{"unsorted-capabilities", "selected_capability", func(m map[string]any) { Object(m, "capability")["required_capabilities"] = []any{"tools", "alpha"} }},
		{"default-port", "destination_registration_not_delivery", func(m map[string]any) { Object(m, "destination")["origin"] = "https://provider.invalid:443" }},
		{"missing-region", "destination_registration_not_delivery", func(m map[string]any) {
			d := Object(m, "destination")
			d["region_source"] = "configured"
			delete(d, "region")
		}},
		{"missing-input-binding", "destination_registration_not_delivery", func(m map[string]any) { Object(m, "destination")["input_binding_state"] = "known" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeObservation(mutateObservation(t, tc.vector, tc.change)); err == nil {
				t.Fatal("invalid domain accepted")
			}
		})
	}
	raw := observationWire(t, "request_started")
	for name, wire := range map[string][]byte{"noncanonical": append([]byte(" "), raw...), "duplicate": append([]byte(`{"revision":"1",`), raw[1:]...), "over-limit": bytes.Repeat([]byte(" "), MaxDocumentBytes+1)} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeObservation(wire); err == nil {
				t.Fatal("invalid wire accepted")
			}
		})
	}
}

func TestObservationEnvelopeRejectsRehashedProjection(t *testing.T) {
	raw := observationWire(t, "request_started")
	wire, err := ObservationEnvelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	m, err := connector.DecodeStrictJSONObject(wire)
	if err != nil {
		t.Fatal(err)
	}
	Object(m, "correlation")["request_ref"] = "different-request"
	delete(Object(m, "integrity"), "envelope_hash")
	changed, _, ce := connector.BuildCanonicalEnvelope(m, observationBinding(RuntimeProfile))
	if ce != nil {
		t.Fatal(ce)
	}
	if _, err := ValidateObservationItem(changed, RuntimeProfile); err == nil {
		t.Fatal("self-consistent altered projection accepted")
	}
}

func TestCurrentRuntimeFrozenVectorsAndHash(t *testing.T) {
	raw, err := os.ReadFile("contract/observations/golden-runtime-current.json")
	if err != nil {
		t.Fatal(err)
	}
	type vector struct {
		Name     string
		Document json.RawMessage
	}
	var fixture struct{ Valid, Invalid []vector }
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Valid) != 3 || len(fixture.Invalid) != 5 {
		t.Fatal("snapshot vector coverage changed")
	}
	for _, v := range fixture.Valid {
		t.Run(v.Name, func(t *testing.T) {
			p, err := DecodeCurrentRuntimePage(v.Document, "synthetic-ur", "1", "2")
			if err != nil {
				t.Fatal(err)
			}
			if p.Profile != RuntimeProfile {
				t.Fatal("wrong runtime profile")
			}
			if _, err := DecodeCurrentRuntimePage(v.Document, "synthetic-ur", "1", "3"); err == nil {
				t.Fatal("cross-project snapshot accepted")
			}
			m, err := connector.DecodeStrictJSONObject(v.Document)
			if err != nil {
				t.Fatal(err)
			}
			m["observed_at"] = "2026-09-10T00:00:01.000000Z"
			changed, _ := json.Marshal(m)
			if _, err := DecodeCurrentRuntimePage(changed, "synthetic-ur", "1", "2"); err == nil {
				t.Fatal("altered snapshot hash accepted")
			}
		})
	}
	for _, v := range fixture.Invalid {
		t.Run(v.Name, func(t *testing.T) {
			if _, err := DecodeCurrentRuntimePage(v.Document, "synthetic-ur", "1", "2"); err == nil {
				t.Fatal("invalid snapshot accepted")
			}
		})
	}
	if Hash(currentRuntimeSchemaBytes) != "sha256:ea7f24afa493480643b15432bfa0a5ade7caf99a3bb9b217780fda58501fcb63" {
		t.Fatal("snapshot schema identity changed")
	}
}

func TestObservationPageBoundaries(t *testing.T) {
	raw := observationWire(t, "request_started")
	d, err := DecodeObservation(raw)
	if err != nil {
		t.Fatal(err)
	}
	e, err := ObservationEnvelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	p := Page{BatchVersion: connector.EventBatchVersionV1, ConnectorID: ConnectorID, ConnectorVersion: ObservationConnectorVersion, ProducerInstance: d.SourceInstance, GeneratedAt: d.RecordedAt, Events: []json.RawMessage{e}, NextCursor: "next", HighWatermark: "next"}
	wire, _ := json.Marshal(p)
	got, err := DecodeObservationPage(wire, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile, "previous", 50)
	if err != nil || len(got.Observations) != 1 || len(got.Items) != 0 {
		t.Fatalf("B batch: %v", err)
	}
	if _, err := DecodePage(wire, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile, "previous", 50); err == nil {
		t.Fatal("A batch accepted B")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Page)
	}{
		{"legacy-connector", func(p *Page) { p.ConnectorVersion = ConnectorVersion }},
		{"no-cursor-progress", func(p *Page) { p.NextCursor = "previous"; p.HighWatermark = "previous" }},
		{"duplicate-event", func(p *Page) { p.Events = append(p.Events, p.Events[0]) }},
		{"false-tail", func(p *Page) { p.HasMore = true }},
		{"too-many", func(p *Page) {
			for len(p.Events) < 51 {
				p.Events = append(p.Events, p.Events[0])
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := p
			changed.Events = append([]json.RawMessage{}, p.Events...)
			tc.mutate(&changed)
			wire, _ := json.Marshal(changed)
			if _, err := DecodeObservationPage(wire, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile, "previous", 50); err == nil {
				t.Fatal("invalid page accepted")
			}
		})
	}
	if _, err := DecodeObservationPage(wire, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile, "", 50); err == nil {
		t.Fatal("bootstrap backfill accepted")
	}
}
