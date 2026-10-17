// Package sourcefacts consumes UR's three immutable metadata profiles. It
// deliberately does not map them into completed-execution Evidence or money.
package sourcefacts

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	Version          = "unirouter.source-fact.v1"
	ContextProfile   = "unirouter.execution-context.v1"
	IdentityProfile  = "unirouter.execution-identity.v1"
	CostProfile      = "unirouter.cost-basis.v1"
	ConnectorID      = "unirouter.workfacts"
	ConnectorVersion = "1.0.0"
	MaxDocumentBytes = 64 << 10
	MaxPageBytes     = 8 << 20
	TimeFormat       = "2006-01-02T15:04:05.000000Z"
)

var (
	ErrInvalid       = errors.New("source facts contract is invalid")
	ErrConflict      = errors.New("source facts immutable identity or revision conflicts")
	ErrStreamChanged = errors.New("source facts stream changed since this request; read its current state")
	ErrUnavailable   = errors.New("source facts stream is unavailable")
	ErrScopeChanged  = errors.New("source facts scope changed; explicit registration is required")
)

//go:embed contract/source-fact.schema.json
var schemaBytes []byte

var compiledSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	var raw any
	if err := json.Unmarshal(schemaBytes, &raw); err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	if err := c.AddResource("urn:xinsight:ur-source-facts:v1", raw); err != nil {
		return nil, err
	}
	return c.Compile("urn:xinsight:ur-source-facts:v1")
})

type Document struct {
	SchemaVersion    string         `json:"schema_version"`
	SourceInstance   string         `json:"source_instance"`
	OrgID            string         `json:"org_id"`
	ProjectID        string         `json:"project_id"`
	RequestID        string         `json:"request_id"`
	Profile          string         `json:"profile"`
	Stage            string         `json:"stage"`
	SourceFactID     string         `json:"source_fact_id"`
	Revision         string         `json:"revision"`
	EventID          string         `json:"event_id"`
	PreviousRevision string         `json:"previous_revision,omitempty"`
	PreviousHash     string         `json:"previous_hash,omitempty"`
	CorrectionReason string         `json:"correction_reason,omitempty"`
	OccurredAt       string         `json:"occurred_at"`
	RecordedAt       string         `json:"recorded_at"`
	CaptureStatus    string         `json:"capture_status"`
	Core             map[string]any `json:"core,omitempty"`
	Identity         map[string]any `json:"identity,omitempty"`
	Cost             map[string]any `json:"cost,omitempty"`
}

type Item struct {
	Document      Document
	SourceBytes   []byte
	EnvelopeBytes []byte
	SourceHash    string
	EnvelopeHash  string
}

func ValidProfile(p string) bool {
	return p == ContextProfile || p == IdentityProfile || p == CostProfile
}
func Hash(raw []byte) string                           { s := sha256.Sum256(raw); return "sha256:" + hex.EncodeToString(s[:]) }
func Text(m map[string]any, k string) string           { s, _ := m[k].(string); return s }
func Object(m map[string]any, k string) map[string]any { s, _ := m[k].(map[string]any); return s }
func Positive(v string) bool {
	n, e := strconv.ParseInt(v, 10, 64)
	return e == nil && n > 0 && strconv.FormatInt(n, 10) == v
}
func ValidTime(v string) bool {
	t, e := time.Parse(TimeFormat, v)
	return e == nil && t.Format(TimeFormat) == v
}

func SourceID(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(p)))
		h.Write(n[:])
		h.Write([]byte(p))
	}
	return "uf1_" + hex.EncodeToString(h.Sum(nil))
}

func DecodeDocument(raw []byte) (Document, error) {
	var d Document
	if len(raw) == 0 || len(raw) > MaxDocumentBytes {
		return d, ErrInvalid
	}
	m, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		return d, ErrInvalid
	}
	schema, err := compiledSchema()
	if err != nil {
		return d, err
	}
	if schema.Validate(m) != nil || !validValues(m, "") {
		return d, ErrInvalid
	}
	canonical, err := connector.CanonicalizeJSONValue(m)
	if err != nil || !bytes.Equal(raw, canonical) {
		return d, ErrInvalid
	}
	if json.Unmarshal(raw, &d) != nil {
		return d, ErrInvalid
	}
	if d.SourceFactID != SourceID(Version, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile, d.RequestID, d.Stage) || d.EventID != SourceID(Version+".revision", d.SourceFactID, d.Revision) {
		return d, ErrInvalid
	}
	if d.Revision == "1" {
		if d.PreviousRevision != "" || d.PreviousHash != "" || d.CorrectionReason != "" {
			return d, ErrInvalid
		}
	} else {
		r, _ := strconv.ParseInt(d.Revision, 10, 64)
		p, _ := strconv.ParseInt(d.PreviousRevision, 10, 64)
		if p != r-1 || d.PreviousHash == "" || d.CorrectionReason == "" {
			return d, ErrInvalid
		}
	}
	if d.Core != nil && (Text(d.Core, "org_id") != d.OrgID || Text(d.Core, "project_id") != d.ProjectID || Text(d.Core, "request_id") != d.RequestID) {
		return d, ErrInvalid
	}
	if d.Cost != nil {
		r := Object(d.Cost, "economic_reference")
		if Text(r, "source_instance") != d.SourceInstance || Text(r, "org_id") != d.OrgID || Text(r, "project_id") != d.ProjectID || Text(r, "request_id") != d.RequestID {
			return d, ErrInvalid
		}
	}
	return d, nil
}

// JSON Schema handles field shapes. These checks add the source's int64,
// control-character and actual calendar-time requirements without float casts.
func validValues(v any, key string) bool {
	switch x := v.(type) {
	case nil:
		return false
	case map[string]any:
		for k, v := range x {
			if !validValues(v, k) {
				return false
			}
		}
	case []any:
		for _, v := range x {
			if !validValues(v, key) {
				return false
			}
		}
	case string:
		for _, r := range x {
			if unicode.IsControl(r) {
				return false
			}
		}
		if key == "occurred_at" || key == "recorded_at" || key == "captured_at" {
			return ValidTime(x)
		}
		switch key {
		case "org_id", "project_id", "revision", "previous_revision", "api_key_id", "credential_id", "workload_application_id", "owner_user_id", "grant_id", "grant_revision", "grant_subject_user_id", "grant_subject_workload_id", "sponsor_user_id", "rate_card_version", "unit_quantity":
			return Positive(x)
		case "auth_epoch", "input_rate_micros_per_million", "cache_read_rate_micros_per_million", "cache_creation_rate_micros_per_million", "output_rate_micros_per_million", "amount_minor":
			n, e := strconv.ParseInt(x, 10, 64)
			return e == nil && n >= 0 && strconv.FormatInt(n, 10) == x
		}
	}
	return true
}

type profileBinding string

func (p profileBinding) Validate() error {
	if !ValidProfile(string(p)) {
		return ErrInvalid
	}
	return nil
}
func (p profileBinding) ProjectionRegistration() connector.ProjectionBinding {
	return connector.ProjectionBinding{ProfileID: string(p)}
}
func (p profileBinding) ConnectorRegistration() connector.PortableConnectorBinding {
	names := map[string][2]string{ContextProfile: {"execution.context.recorded", "execution-context"}, IdentityProfile: {"execution.identity.recorded", "execution-identity"}, CostProfile: {"execution.cost_basis.recorded", "cost-basis"}}
	n := names[string(p)]
	return connector.PortableConnectorBinding{ConnectorID: ConnectorID, ConnectorVersion: ConnectorVersion, SourceProduct: "unirouter", EventType: n[0], SchemaRef: "foigent://events/unirouter/" + n[1] + "/v1", SchemaVersion: "1", SourceSchema: Version, SourceSchemaSHA256: Hash(schemaBytes)}
}

// ValidateItem also reprojects the complete expected Envelope. A valid hash
// alone must not authorize an altered actor, capture policy or source scope.
func ValidateItem(raw []byte, profile string) (Item, error) {
	e, ce := connector.ValidateEnvelope(raw, profileBinding(profile))
	if ce != nil {
		return Item{}, ErrInvalid
	}
	p := e.Payload()
	wire, err := connector.CanonicalizeJSONValue(p)
	if err != nil {
		return Item{}, ErrInvalid
	}
	d, err := DecodeDocument(wire)
	if err != nil || d.Profile != profile || Hash(wire) != e.SourceHash {
		return Item{}, ErrInvalid
	}
	expected, err := Envelope(d, wire)
	if err != nil || !bytes.Equal(expected, e.CanonicalBytes()) {
		return Item{}, ErrInvalid
	}
	return Item{Document: d, SourceBytes: wire, EnvelopeBytes: e.CanonicalBytes(), SourceHash: e.SourceHash, EnvelopeHash: e.EnvelopeHash}, nil
}

func Envelope(d Document, canonical []byte) ([]byte, error) {
	validated, err := DecodeDocument(canonical)
	if err != nil || validated.EventID != d.EventID {
		return nil, ErrInvalid
	}
	d = validated
	var payload map[string]any
	if json.Unmarshal(canonical, &payload) != nil {
		return nil, ErrInvalid
	}
	b := profileBinding(d.Profile)
	reg := b.ConnectorRegistration()
	key, err := connector.EventIdempotencyKey("unirouter", d.SourceInstance, "", d.OrgID, d.ProjectID, reg.EventType, d.EventID)
	if err != nil {
		return nil, err
	}
	quality := "direct"
	if d.CaptureStatus == "not_captured" {
		quality = "gap"
	}
	raw := map[string]any{"spec_version": connector.EventEnvelopeVersionV1, "event_id": d.EventID, "event_type": reg.EventType, "schema_ref": reg.SchemaRef, "schema_version": "1",
		"producer": map[string]any{"product": "unirouter", "instance": d.SourceInstance, "connector_id": ConnectorID, "connector_version": ConnectorVersion},
		"scope":    map[string]any{"org_ref": d.OrgID, "project_ref": d.ProjectID}, "subject": map[string]any{"type": "execution", "ref": d.RequestID}, "actor": map[string]any{"type": "unknown"},
		"time": map[string]any{"occurred_at": d.OccurredAt, "observed_at": d.RecordedAt}, "correlation": map[string]any{"request_ref": d.RequestID},
		"identity": map[string]any{"idempotency_key": key, "source_revision": d.Revision}, "integrity": map[string]any{"source_hash": Hash(canonical)},
		"data_policy": map[string]any{"payload_capture_mode": "metadata_only", "raw_payload_present": false}, "provenance": map[string]any{"adapter_fidelity": "F2", "field_groups": map[string]any{"source_fact": quality}}, "payload": payload}
	wire, _, ce := connector.BuildCanonicalEnvelope(raw, b)
	if ce != nil {
		return nil, ErrInvalid
	}
	return wire, nil
}

type Page struct {
	BatchVersion     string            `json:"batch_version"`
	ConnectorID      string            `json:"connector_id"`
	ConnectorVersion string            `json:"connector_version"`
	ProducerInstance string            `json:"producer_instance"`
	GeneratedAt      string            `json:"generated_at"`
	Events           []json.RawMessage `json:"events"`
	NextCursor       string            `json:"next_cursor"`
	HighWatermark    string            `json:"high_watermark"`
	HasMore          bool              `json:"has_more"`
	Items            []Item            `json:"-"`
	Observations     []ObservationItem `json:"-"`
}

func DecodePage(raw []byte, instance, org, project, profile, previous string, limit int) (Page, error) {
	var p Page
	if len(raw) == 0 || len(raw) >= MaxPageBytes || limit < 1 || limit > 50 || !ValidProfile(profile) {
		return p, ErrInvalid
	}
	m, err := connector.DecodeStrictJSONObject(raw)
	if err != nil || len(m) != 9 || !validValues(m, "") {
		return p, ErrInvalid
	}
	for _, k := range []string{"batch_version", "connector_id", "connector_version", "producer_instance", "generated_at", "events", "next_cursor", "high_watermark", "has_more"} {
		if _, ok := m[k]; !ok {
			return p, ErrInvalid
		}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&p) != nil {
		return p, ErrInvalid
	}
	if p.BatchVersion != connector.EventBatchVersionV1 || p.ConnectorID != ConnectorID || p.ConnectorVersion != ConnectorVersion || p.ProducerInstance != instance || !ValidTime(p.GeneratedAt) || p.Events == nil || len(p.Events) > limit || !validCursor(p.NextCursor) || !validCursor(p.HighWatermark) {
		return p, ErrInvalid
	}
	if previous == "" {
		if len(p.Events) != 0 || p.HasMore || p.NextCursor != p.HighWatermark {
			return p, ErrInvalid
		}
	} else {
		if (len(p.Events) > 0 && p.NextCursor == previous) || (len(p.Events) == 0 && (p.NextCursor != previous || p.HasMore)) || (!p.HasMore && p.NextCursor != p.HighWatermark) || (p.HasMore && p.NextCursor == p.HighWatermark) {
			return p, ErrInvalid
		}
	}
	seen := map[string]bool{}
	for _, raw := range p.Events {
		item, e := ValidateItem(raw, profile)
		if e != nil || item.Document.SourceInstance != instance || item.Document.OrgID != org || item.Document.ProjectID != project || seen[item.Document.EventID] {
			return p, ErrInvalid
		}
		seen[item.Document.EventID] = true
		p.Items = append(p.Items, item)
	}
	return p, nil
}

func validCursor(v string) bool {
	return v != "" && len(v) <= 8192 && strings.TrimSpace(v) == v && validValues(v, "")
}
