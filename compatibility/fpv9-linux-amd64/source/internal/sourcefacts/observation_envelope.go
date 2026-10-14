package sourcefacts

import (
	"bytes"
	"encoding/json"

	"github.com/foigent/x-insight/internal/connector"
)

type ObservationItem struct {
	Document      Observation
	SourceBytes   []byte
	EnvelopeBytes []byte
	SourceHash    string
	EnvelopeHash  string
}

type observationBinding string

func (p observationBinding) Validate() error {
	if !ValidObservationProfile(string(p)) {
		return ErrInvalid
	}
	return nil
}
func (p observationBinding) ProjectionRegistration() connector.ProjectionBinding {
	return connector.ProjectionBinding{ProfileID: string(p)}
}
func (p observationBinding) ConnectorRegistration() connector.PortableConnectorBinding {
	bindings := map[string][2]string{
		RuntimeProfile:     {"execution.runtime.observed", "runtime-observation"},
		AttemptProfile:     {"execution.attempt.observed", "attempt-observation"},
		CapabilityProfile:  {"execution.capability.recorded", "capability-snapshot"},
		PolicyProfile:      {"execution.policy.observed", "policy-observation"},
		DestinationProfile: {"execution.destination.observed", "destination-observation"},
	}
	b := bindings[string(p)]
	return connector.PortableConnectorBinding{ConnectorID: ConnectorID, ConnectorVersion: ObservationConnectorVersion, SourceProduct: "unirouter", EventType: b[0], SchemaRef: "foigent://events/unirouter/" + b[1] + "/v1", SchemaVersion: "1", SourceSchema: ObservationVersion, SourceSchemaSHA256: Hash(observationSchemaBytes)}
}

func ObservationEnvelope(canonical []byte) ([]byte, error) {
	d, err := DecodeObservation(canonical)
	if err != nil {
		return nil, err
	}
	payload, err := connector.DecodeStrictJSONObject(canonical)
	if err != nil {
		return nil, ErrInvalid
	}
	binding := observationBinding(d.Profile)
	reg := binding.ConnectorRegistration()
	key, err := connector.EventIdempotencyKey("unirouter", d.SourceInstance, "", d.OrgID, d.ProjectID, reg.EventType, d.EventID)
	if err != nil {
		return nil, err
	}
	quality := "direct"
	if d.Coverage != "observed" {
		quality = "gap"
	}
	raw := map[string]any{
		"spec_version": connector.EventEnvelopeVersionV1, "event_id": d.EventID, "event_type": reg.EventType, "schema_ref": reg.SchemaRef, "schema_version": "1",
		"producer": map[string]any{"product": "unirouter", "instance": d.SourceInstance, "connector_id": ConnectorID, "connector_version": ObservationConnectorVersion},
		"scope":    map[string]any{"org_ref": d.OrgID, "project_ref": d.ProjectID},
		"subject":  map[string]any{"type": d.SubjectKind, "ref": d.SubjectID}, "actor": map[string]any{"type": "unknown"},
		"time": map[string]any{"occurred_at": d.OccurredAt, "observed_at": d.RecordedAt}, "correlation": map[string]any{"request_ref": d.RequestID},
		"identity": map[string]any{"idempotency_key": key, "source_revision": d.Revision}, "integrity": map[string]any{"source_hash": Hash(canonical)},
		"data_policy": map[string]any{"payload_capture_mode": "metadata_only", "raw_payload_present": false},
		"provenance":  map[string]any{"adapter_fidelity": "F2", "field_groups": map[string]any{"source_fact": quality}}, "payload": payload,
	}
	wire, _, ce := connector.BuildCanonicalEnvelope(raw, binding)
	if ce != nil {
		return nil, ErrInvalid
	}
	return wire, nil
}

// ValidateObservationItem verifies both exact hashes and the full fixed
// projection; a self-consistent envelope cannot invent an actor or permission.
func ValidateObservationItem(raw []byte, profile string) (ObservationItem, error) {
	e, ce := connector.ValidateEnvelope(raw, observationBinding(profile))
	if ce != nil {
		return ObservationItem{}, ErrInvalid
	}
	wire, err := connector.CanonicalizeJSONValue(e.Payload())
	if err != nil {
		return ObservationItem{}, ErrInvalid
	}
	d, err := DecodeObservation(wire)
	if err != nil || d.Profile != profile || Hash(wire) != e.SourceHash {
		return ObservationItem{}, ErrInvalid
	}
	expected, err := ObservationEnvelope(wire)
	if err != nil || !bytes.Equal(expected, e.CanonicalBytes()) {
		return ObservationItem{}, ErrInvalid
	}
	return ObservationItem{Document: d, SourceBytes: wire, EnvelopeBytes: e.CanonicalBytes(), SourceHash: e.SourceHash, EnvelopeHash: e.EnvelopeHash}, nil
}

// DecodeObservationPage uses the existing batch framing and cursor rules while
// keeping B documents outside Page.Items, which is exclusively A.
func DecodeObservationPage(raw []byte, instance, org, project, profile, previous string, limit int) (Page, error) {
	var p Page
	if len(raw) == 0 || len(raw) >= MaxPageBytes || limit < 1 || limit > 50 || !ValidObservationProfile(profile) {
		return p, ErrInvalid
	}
	m, err := connector.DecodeStrictJSONObject(raw)
	if err != nil || len(m) != 9 {
		return p, ErrInvalid
	}
	for _, k := range []string{"batch_version", "connector_id", "connector_version", "producer_instance", "generated_at", "events", "next_cursor", "high_watermark", "has_more"} {
		v, ok := m[k]
		if !ok || k != "events" && !validValues(v, k) {
			return p, ErrInvalid
		}
	}
	if json.Unmarshal(raw, &p) != nil || p.BatchVersion != connector.EventBatchVersionV1 || p.ConnectorID != ConnectorID || p.ConnectorVersion != ObservationConnectorVersion || p.ProducerInstance != instance || !ValidTime(p.GeneratedAt) || p.Events == nil || len(p.Events) > limit || !validCursor(p.NextCursor) || !validCursor(p.HighWatermark) {
		return p, ErrInvalid
	}
	if previous == "" {
		if len(p.Events) != 0 || p.HasMore || p.NextCursor != p.HighWatermark {
			return p, ErrInvalid
		}
	} else if (len(p.Events) > 0 && p.NextCursor == previous) || (len(p.Events) == 0 && (p.NextCursor != previous || p.HasMore)) || (!p.HasMore && p.NextCursor != p.HighWatermark) || (p.HasMore && p.NextCursor == p.HighWatermark) {
		return p, ErrInvalid
	}
	seen := map[string]bool{}
	for _, event := range p.Events {
		item, err := ValidateObservationItem(event, profile)
		if err != nil || item.Document.SourceInstance != instance || item.Document.OrgID != org || item.Document.ProjectID != project || seen[item.Document.EventID] {
			return Page{}, ErrInvalid
		}
		seen[item.Document.EventID] = true
		p.Observations = append(p.Observations, item)
	}
	return p, nil
}
