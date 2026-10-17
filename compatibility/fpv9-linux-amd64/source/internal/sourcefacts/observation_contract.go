package sourcefacts

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	ObservationVersion          = "unirouter.source-observation.v1"
	ObservationConnectorVersion = "2.0.0"
	RuntimeProfile              = "unirouter.runtime-observation.v1"
	AttemptProfile              = "unirouter.attempt-observation.v1"
	CapabilityProfile           = "unirouter.capability-snapshot.v1"
	PolicyProfile               = "unirouter.policy-observation.v1"
	DestinationProfile          = "unirouter.destination-observation.v1"
	CurrentRuntimeVersion       = "unirouter.runtime-current.v1"
)

//go:embed contract/observations/source-observation.schema.json
var observationSchemaBytes []byte

//go:embed contract/observations/runtime-current.schema.json
var currentRuntimeSchemaBytes []byte

var compiledObservationSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	return compileObservationContract(observationSchemaBytes, "urn:xinsight:source-observation:v1")
})
var compiledCurrentRuntimeSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	return compileObservationContract(currentRuntimeSchemaBytes, "urn:xinsight:runtime-current:v1")
})

func compileObservationContract(raw []byte, uri string) (*jsonschema.Schema, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	if err := c.AddResource(uri, value); err != nil {
		return nil, err
	}
	return c.Compile(uri)
}

func ObservationProfiles() []string {
	return []string{RuntimeProfile, AttemptProfile, CapabilityProfile, PolicyProfile, DestinationProfile}
}

func ValidObservationProfile(profile string) bool {
	switch profile {
	case RuntimeProfile, AttemptProfile, CapabilityProfile, PolicyProfile, DestinationProfile:
		return true
	}
	return false
}

// DecodeObservation consumes canonical source bytes, not an A Document. The
// frozen schema validates shapes; XI independently checks identity and domain
// relationships which JSON Schema cannot express.
func DecodeObservation(raw []byte) (Observation, error) {
	var d Observation
	if len(raw) == 0 || len(raw) > MaxDocumentBytes {
		return d, ErrInvalid
	}
	m, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		return d, ErrInvalid
	}
	schema, err := compiledObservationSchema()
	if err != nil {
		return d, err
	}
	if schema.Validate(m) != nil || !validObservationFields(m, "") {
		return d, ErrInvalid
	}
	canonical, err := connector.CanonicalizeJSONValue(m)
	if err != nil || !bytes.Equal(raw, canonical) || json.Unmarshal(raw, &d) != nil {
		return d, ErrInvalid
	}
	if !validObservationDomain(d) {
		return Observation{}, ErrInvalid
	}
	return d, nil
}

// Source identifiers and opaque references are bounded by UTF-8 bytes, whereas
// JSON Schema maxLength counts code points. Capability evidence text has its
// own source contract and is checked separately rather than narrowed here.
func validObservationFields(value any, key string) bool {
	switch v := value.(type) {
	case map[string]any:
		for k, child := range v {
			if !validObservationFields(child, k) {
				return false
			}
		}
	case []any:
		for _, child := range v {
			if !validObservationFields(child, key) {
				return false
			}
		}
	case string:
		switch key {
		case "org_id", "project_id", "revision", "previous_revision", "route_sequence", "route_total", "slot_version", "lease_generation":
			return Positive(v)
		case "value":
			n, err := strconv.ParseInt(v, 10, 64)
			return err == nil && n >= 0 && strconv.FormatInt(n, 10) == v
		case "occurred_at", "recorded_at", "verified_at", "observed_at", "last_observed_at", "valid_until":
			return ValidTime(v)
		}
		maximum := 0
		switch key {
		case "source_instance", "subject_id", "attempt_id", "operation", "model_ref", "surface", "id", "correction_reason", "detector_ref":
			maximum = 128
		case "request_id", "provider", "version", "policy_version", "detector_version", "region":
			maximum = 64
		case "evidence_ref", "release_ref", "binding_ref", "probe_ref", "approval_ref", "input_binding_ref":
			maximum = 256
		case "policy_ref":
			maximum = 160
		}
		if maximum != 0 {
			if len(v) == 0 || len(v) > maximum {
				return false
			}
			for _, r := range v {
				if unicode.IsControl(r) {
					return false
				}
			}
		}
	}
	return true
}

func validObservationDomain(d Observation) bool {
	factID := SourceID(ObservationVersion, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile, d.RequestID, d.SubjectKind, d.SubjectID, d.Kind)
	if d.SourceFactID != factID || d.EventID != SourceID(ObservationVersion+".revision", factID, d.Revision) || d.SubjectKind == "request" && d.SubjectID != d.RequestID {
		return false
	}
	if d.Revision != "1" {
		revision, _ := strconv.ParseInt(d.Revision, 10, 64)
		previous, _ := strconv.ParseInt(d.PreviousRevision, 10, 64)
		if previous != revision-1 {
			return false
		}
	}
	if a := d.Attempt; a != nil {
		if a.AttemptID != d.SubjectID || a.RetryAuthorized && a.Provider != "rejected" && d.SubjectKind != "media_attempt" || (a.RouteSequence == "") != (a.RouteTotal == "") {
			return false
		}
		if a.RouteSequence != "" {
			sequence, _ := strconv.ParseInt(a.RouteSequence, 10, 64)
			total, _ := strconv.ParseInt(a.RouteTotal, 10, 64)
			if sequence > total {
				return false
			}
		}
		if a.HTTPStatus != "" {
			status, err := strconv.Atoi(a.HTTPStatus)
			if err != nil || status < 100 || status > 599 || strconv.Itoa(status) != a.HTTPStatus {
				return false
			}
		}
	}
	if c := d.Capability; c != nil {
		if !strictlySorted(c.RequiredCapabilities) {
			return false
		}
		for _, id := range c.RequiredCapabilities {
			if !validCapabilityText(id, 80) {
				return false
			}
		}
		for i, entry := range c.Capabilities {
			if !validCapabilityText(entry.CapabilityID, 80) || !validCapabilityText(entry.EvidenceState, 32) || i > 0 && entry.CapabilityID <= c.Capabilities[i-1].CapabilityID {
				return false
			}
		}
	}
	if p := d.Policy; p != nil {
		if !strictlySorted(p.DetectorOutcomes) || !strictlySorted(p.StageDecisions) || p.Mode == "shadow" && len(p.Effects) != 0 {
			return false
		}
	}
	if v := d.Destination; v != nil {
		if !validObservationOrigin(v.Origin) || (v.RegionSource == "configured") != (v.Region != "") || (v.InputBindingState == "known") != (v.InputBindingRef != "") {
			return false
		}
	}
	return true
}

func strictlySorted(values []string) bool {
	for i := 1; i < len(values); i++ {
		if values[i] <= values[i-1] {
			return false
		}
	}
	return true
}

func validCapabilityText(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\x00\r\n")
}

// Accept only the source's disclosed canonical origin. Never preserve userinfo,
// a path, a query or a fragment as destination evidence.
func validObservationOrigin(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host, port := strings.ToLower(u.Hostname()), u.Port()
	if host == "" || strings.ContainsAny(host, "\r\n\t /\\%") {
		return false
	}
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
			return false
		}
	}
	if u.Scheme == "http" && port == "80" || u.Scheme == "https" && port == "443" {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return raw == u.Scheme+"://"+host
}

// DecodeCurrentRuntimePage verifies the separate snapshot contract and exact
// source hash. Receipt never changes an incremental event cursor or establishes
// present liveness: callers must still enforce no-store, current grants and the
// entry's lease deadline at the time of display.
func DecodeCurrentRuntimePage(raw []byte, instance, org, project string) (CurrentRuntimePage, error) {
	var p CurrentRuntimePage
	if len(raw) == 0 || len(raw) >= MaxPageBytes {
		return p, ErrInvalid
	}
	m, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		return p, ErrInvalid
	}
	schema, err := compiledCurrentRuntimeSchema()
	if err != nil {
		return p, err
	}
	if schema.Validate(m) != nil || !validObservationFields(m, "") || json.Unmarshal(raw, &p) != nil {
		return p, ErrInvalid
	}
	if p.SourceInstance != instance || p.OrgID != org || p.ProjectID != project {
		return CurrentRuntimePage{}, ErrInvalid
	}
	delete(m, "source_sha256")
	wire, err := connector.CanonicalizeJSONValue(m)
	if err != nil || Hash(wire) != p.SourceSHA256 {
		return CurrentRuntimePage{}, ErrInvalid
	}
	return p, nil
}
