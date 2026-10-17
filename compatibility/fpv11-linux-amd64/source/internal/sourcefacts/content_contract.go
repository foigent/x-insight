package sourcefacts

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"reflect"
	"regexp"
	"strconv"
	"sync"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	ContentProfile             = "unirouter.interaction-content.v1"
	ContentManifestVersion     = "unirouter.content-manifest.v1"
	ContentCapabilitiesVersion = "unirouter.content-capabilities.v1"
	ContentReadMetadataVersion = "unirouter.content-read-metadata.v1"
	ContentManifestPageVersion = "unirouter.content-manifests-page.v1"
	ContentPurposeHumanReview  = "human_review"
)

//go:embed contract/content/c0-contract.schema.json
var contentSchemaBytes []byte

var contentSchemas = sync.OnceValues(func() (map[string]*jsonschema.Schema, error) {
	var raw map[string]any
	if err := json.Unmarshal(contentSchemaBytes, &raw); err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.AssertFormat()
	c.UseRegexpEngine(contentRegexp)
	id := Text(raw, "$id")
	if err := c.AddResource(id, raw); err != nil {
		return nil, err
	}
	result := make(map[string]*jsonschema.Schema)
	for _, name := range []string{"Capabilities", "ContentAccess", "Manifest", "ManifestPage", "FrozenManifestRef", "ReadMetadata", "ContentChoice", "ContentProjection"} {
		s, err := c.Compile(id + "#/$defs/" + name)
		if err != nil {
			return nil, err
		}
		result[name] = s
	}
	return result, nil
})

// The shared ECMA pattern uses Unicode escapes, which Go's regexp parser
// does not accept. Translate only this exact frozen pattern to the identical
// ASCII range; retain the source schema bytes and all validation constraints.
func contentRegexp(pattern string) (jsonschema.Regexp, error) {
	if pattern == `^[^\u0000-\u001f\u007f]+$` {
		pattern = `^[^\x00-\x1f\x7f]+$`
	}
	return regexp.Compile(pattern)
}

type ContentBoundary struct {
	Time          string `json:"time"`
	AfterSequence string `json:"after_sequence"`
}

type ContentLimits struct {
	MaxSavedBytes    int64 `json:"max_saved_bytes"`
	MaxReadBytes     int64 `json:"max_read_bytes"`
	MaxBufferBytes   int64 `json:"max_buffer_bytes"`
	MaxManifestBytes int64 `json:"max_manifest_bytes"`
	MaxManifestItems int   `json:"max_manifest_items"`
}

type ContentEndpoints struct {
	Capabilities string `json:"capabilities"`
	Manifests    string `json:"manifests"`
	Content      string `json:"content"`
}

type ContentCapabilities struct {
	SchemaVersion        string           `json:"schema_version"`
	Profile              string           `json:"profile"`
	SourceInstance       string           `json:"source_instance"`
	OrgID                string           `json:"org_id"`
	ProjectID            string           `json:"project_id"`
	Status               string           `json:"status"`
	CredentialStatus     string           `json:"credential_status"`
	SupportedProtocols   []string         `json:"supported_protocols"`
	ObservationPoints    []string         `json:"observation_points"`
	CaptureModes         []string         `json:"capture_modes"`
	AllowedPurposes      []string         `json:"allowed_purposes"`
	ContentScopeRevision string           `json:"content_scope_revision"`
	RecoveryEpoch        string           `json:"recovery_epoch"`
	CoverageFrom         ContentBoundary  `json:"coverage_from"`
	RetentionFloor       ContentBoundary  `json:"retention_floor"`
	Limits               ContentLimits    `json:"limits"`
	Endpoints            ContentEndpoints `json:"endpoints"`
}

// ContentAccess is a local projection bound to an existing registration hash.
// Decoding it confers no authority and does not compute a registration hash.
type ContentAccess struct {
	RegistrationHash     string           `json:"registration_hash"`
	Profile              string           `json:"profile"`
	Enabled              bool             `json:"enabled"`
	ContentScopeRevision string           `json:"content_scope_revision"`
	AllowedPurposes      []string         `json:"allowed_purposes"`
	CoverageFrom         *ContentBoundary `json:"coverage_from,omitempty"`
}

type ContentCapture struct {
	Mode              string `json:"mode"`
	PolicyRef         string `json:"policy_ref"`
	PolicyVersion     string `json:"policy_version"`
	PolicyEffectiveAt string `json:"policy_effective_at"`
	Completeness      string `json:"completeness"`
	Reason            string `json:"reason"`
}

type ContentVersionRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type ContentRepresentation struct {
	MediaType    string             `json:"media_type"`
	Encoding     string             `json:"encoding"`
	ByteDomain   string             `json:"byte_domain"`
	SavedBytes   int64              `json:"saved_bytes"`
	Digest       string             `json:"digest"`
	RedactionRef *ContentVersionRef `json:"redaction_ref,omitempty"`
}

type ContentManifest struct {
	SchemaVersion    string                 `json:"schema_version"`
	SourceInstance   string                 `json:"source_instance"`
	OrgID            string                 `json:"org_id"`
	ProjectID        string                 `json:"project_id"`
	RequestID        string                 `json:"request_id"`
	Profile          string                 `json:"profile"`
	SourceFactID     string                 `json:"source_fact_id"`
	Revision         string                 `json:"revision"`
	EventID          string                 `json:"event_id"`
	PreviousRevision string                 `json:"previous_revision,omitempty"`
	PreviousHash     string                 `json:"previous_hash,omitempty"`
	CorrectionReason string                 `json:"correction_reason,omitempty"`
	OccurredAt       string                 `json:"occurred_at"`
	RecordedAt       string                 `json:"recorded_at"`
	Protocol         string                 `json:"protocol"`
	ObservationPoint string                 `json:"observation_point"`
	AttemptID        string                 `json:"attempt_id,omitempty"`
	ContentItemID    string                 `json:"content_item_id"`
	Capture          ContentCapture         `json:"capture"`
	ContentRef       string                 `json:"content_ref,omitempty"`
	Representation   *ContentRepresentation `json:"representation,omitempty"`
	CapturedAt       string                 `json:"captured_at,omitempty"`
	ExpiresAt        string                 `json:"expires_at,omitempty"`
}

type ContentAvailability struct {
	State       string `json:"state"`
	EvaluatedAt string `json:"evaluated_at"`
	Retryable   bool   `json:"retryable"`
}

type ContentManifestEntry struct {
	Sequence     string              `json:"sequence"`
	Document     ContentManifest     `json:"document"`
	SourceSHA256 string              `json:"source_sha256"`
	Availability ContentAvailability `json:"availability"`
}

type ContentGap struct {
	Reason          string `json:"reason"`
	RangeKnown      bool   `json:"range_known"`
	AfterSequence   string `json:"after_sequence,omitempty"`
	ThroughSequence string `json:"through_sequence,omitempty"`
}

type ContentManifestPage struct {
	SchemaVersion         string                 `json:"schema_version"`
	Profile               string                 `json:"profile"`
	SourceInstance        string                 `json:"source_instance"`
	OrgID                 string                 `json:"org_id"`
	ProjectID             string                 `json:"project_id"`
	GeneratedAt           string                 `json:"generated_at"`
	ContentScopeRevision  string                 `json:"content_scope_revision"`
	RecoveryEpoch         string                 `json:"recovery_epoch"`
	CoverageFrom          ContentBoundary        `json:"coverage_from"`
	RetentionFloor        ContentBoundary        `json:"retention_floor"`
	AfterSequence         string                 `json:"after_sequence"`
	LastSequence          string                 `json:"last_sequence"`
	HighWatermarkSequence string                 `json:"high_watermark_sequence"`
	NextCursor            string                 `json:"next_cursor"`
	HighWatermark         string                 `json:"high_watermark"`
	HasMore               bool                   `json:"has_more"`
	Coverage              string                 `json:"coverage"`
	Gaps                  []ContentGap           `json:"gaps"`
	Items                 []ContentManifestEntry `json:"items"`
}

type FrozenManifestRef struct {
	SourceSystemID string                 `json:"source_system_id"`
	SourceInstance string                 `json:"source_instance"`
	OrgID          string                 `json:"org_id"`
	ProjectID      string                 `json:"project_id"`
	RequestID      string                 `json:"request_id"`
	SourceFactID   string                 `json:"source_fact_id"`
	Revision       string                 `json:"revision"`
	SourceSHA256   string                 `json:"source_sha256"`
	CaptureStatus  string                 `json:"capture_status"`
	ContentRef     string                 `json:"content_ref,omitempty"`
	Representation *ContentRepresentation `json:"representation,omitempty"`
	ExpiresAt      string                 `json:"expires_at,omitempty"`
}

type ContentReadMetadata struct {
	SchemaVersion  string                `json:"schema_version"`
	SourceInstance string                `json:"source_instance"`
	OrgID          string                `json:"org_id"`
	ProjectID      string                `json:"project_id"`
	RequestID      string                `json:"request_id"`
	SourceFactID   string                `json:"source_fact_id"`
	Revision       string                `json:"revision"`
	SourceSHA256   string                `json:"source_sha256"`
	ContentRef     string                `json:"content_ref"`
	Representation ContentRepresentation `json:"representation"`
	ExpiresAt      string                `json:"expires_at"`
}

// Pointers preserve zero offsets/lengths and the empty JSON Pointer. The
// schema enforces exactly one location kind, with no surplus fields.
type ContentLocation struct {
	Kind    string  `json:"kind"`
	Offset  *int64  `json:"offset,omitempty"`
	Length  *int64  `json:"length,omitempty"`
	Pointer *string `json:"pointer,omitempty"`
}

type ContentLink struct {
	Kind              string          `json:"kind"`
	Location          ContentLocation `json:"location"`
	MessageRole       string          `json:"message_role,omitempty"`
	ProtocolMessageID string          `json:"protocol_message_id,omitempty"`
	ToolCallID        string          `json:"tool_call_id,omitempty"`
	ToolName          string          `json:"tool_name,omitempty"`
	SessionRef        string          `json:"session_ref,omitempty"`
	AssociationBasis  string          `json:"association_basis"`
	EvidenceLevel     string          `json:"evidence_level"`
}

type ContentProjection struct {
	ParserRef        ContentVersionRef `json:"parser_ref"`
	Links            []ContentLink     `json:"links"`
	ProjectionSHA256 string            `json:"projection_sha256"`
}

type ContentChoice struct {
	Scope     string            `json:"scope"`
	Reference FrozenManifestRef `json:"reference"`
	Location  ContentLocation   `json:"location"`
	Relation  string            `json:"relation"`
}

func decodeContent(raw []byte, name string, limit int, canonicalRequired bool, out any) (map[string]any, error) {
	if len(raw) == 0 || len(raw) > limit {
		return nil, ErrInvalid
	}
	m, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		return nil, ErrInvalid
	}
	schemas, err := contentSchemas()
	if err != nil {
		return nil, err
	}
	if schemas[name].Validate(m) != nil || !validContentFields(m, "") {
		return nil, ErrInvalid
	}
	if canonicalRequired {
		canonical, err := connector.CanonicalizeJSONValue(m)
		if err != nil || !bytes.Equal(raw, canonical) {
			return nil, ErrInvalid
		}
	}
	if json.Unmarshal(raw, out) != nil {
		return nil, ErrInvalid
	}
	return m, nil
}

func validContentFields(value any, key string) bool {
	switch v := value.(type) {
	case nil:
		return false
	case map[string]any:
		for k, child := range v {
			if !validContentFields(child, k) {
				return false
			}
		}
	case []any:
		for _, child := range v {
			if !validContentFields(child, key) {
				return false
			}
		}
	case string:
		switch key {
		case "org_id", "project_id", "revision", "previous_revision", "recovery_epoch", "policy_version", "sequence":
			return Positive(v)
		case "after_sequence", "through_sequence", "last_sequence", "high_watermark_sequence":
			n, err := strconv.ParseInt(v, 10, 64)
			return err == nil && n >= 0 && strconv.FormatInt(n, 10) == v
		case "time", "occurred_at", "recorded_at", "captured_at", "expires_at", "policy_effective_at", "evaluated_at", "generated_at":
			return ValidTime(v)
		}
	}
	return true
}

func DecodeContentCapabilities(raw []byte) (ContentCapabilities, error) {
	var c ContentCapabilities
	_, err := decodeContent(raw, "Capabilities", MaxDocumentBytes, false, &c)
	if err != nil {
		return ContentCapabilities{}, err
	}
	if c.Limits.MaxReadBytes < c.Limits.MaxSavedBytes || c.Limits.MaxBufferBytes > c.Limits.MaxReadBytes {
		return ContentCapabilities{}, ErrInvalid
	}
	return c, nil
}

func DecodeContentAccess(raw []byte) (ContentAccess, error) {
	var a ContentAccess
	_, err := decodeContent(raw, "ContentAccess", MaxDocumentBytes, false, &a)
	if err != nil {
		return ContentAccess{}, err
	}
	return a, nil
}

// DecodeContentManifest requires canonical source bytes. SourceSHA256 always
// hashes these bytes, excluding page sequence and current availability.
func DecodeContentManifest(raw []byte) (ContentManifest, error) {
	var m ContentManifest
	_, err := decodeContent(raw, "Manifest", MaxDocumentBytes, true, &m)
	if err != nil {
		return ContentManifest{}, err
	}
	fact := SourceID(ContentManifestVersion, m.SourceInstance, m.OrgID, m.ProjectID, ContentProfile, m.RequestID, m.ObservationPoint, m.AttemptID, m.ContentItemID)
	if m.SourceFactID != fact || m.EventID != SourceID(ContentManifestVersion+".revision", fact, m.Revision) || m.RecordedAt < m.OccurredAt {
		return ContentManifest{}, ErrInvalid
	}
	if m.Revision != "1" && contentSequence(m.PreviousRevision) != contentSequence(m.Revision)-1 {
		return ContentManifest{}, ErrInvalid
	}
	if m.Representation != nil && m.ExpiresAt <= m.CapturedAt {
		return ContentManifest{}, ErrInvalid
	}
	return m, nil
}

func DecodeFrozenManifestRef(raw []byte) (FrozenManifestRef, error) {
	var r FrozenManifestRef
	_, err := decodeContent(raw, "FrozenManifestRef", MaxDocumentBytes, false, &r)
	if err != nil {
		return FrozenManifestRef{}, err
	}
	return r, nil
}

func DecodeContentReadMetadata(raw []byte) (ContentReadMetadata, error) {
	var r ContentReadMetadata
	_, err := decodeContent(raw, "ReadMetadata", MaxDocumentBytes, true, &r)
	if err != nil {
		return ContentReadMetadata{}, err
	}
	return r, nil
}

func DecodeContentChoice(raw []byte) (ContentChoice, error) {
	var c ContentChoice
	_, err := decodeContent(raw, "ContentChoice", MaxDocumentBytes, false, &c)
	if err != nil {
		return ContentChoice{}, err
	}
	if !contentLocationWithin(c.Location, c.Reference.Representation.SavedBytes) {
		return ContentChoice{}, ErrInvalid
	}
	return c, nil
}

// Decoding a projection validates its shape and own digest, not the existence
// of JSON Pointer targets, byte contents, or protocol associations in a source.
func DecodeContentProjection(raw []byte) (ContentProjection, error) {
	var p ContentProjection
	m, err := decodeContent(raw, "ContentProjection", MaxDocumentBytes, false, &p)
	if err != nil {
		return ContentProjection{}, err
	}
	delete(m, "projection_sha256")
	canonical, err := connector.CanonicalizeJSONValue(m)
	if err != nil || Hash(canonical) != p.ProjectionSHA256 {
		return ContentProjection{}, ErrInvalid
	}
	return p, nil
}

func DecodeContentManifestPage(raw []byte) (ContentManifestPage, error) {
	var p ContentManifestPage
	m, err := decodeContent(raw, "ManifestPage", MaxPageBytes, false, &p)
	if err != nil {
		return ContentManifestPage{}, err
	}
	previous, high := contentSequence(p.AfterSequence), contentSequence(p.HighWatermarkSequence)
	if previous > high || previous < contentSequence(p.CoverageFrom.AfterSequence) || previous < contentSequence(p.RetentionFloor.AfterSequence) {
		return ContentManifestPage{}, ErrInvalid
	}
	for i, entry := range p.Items {
		d := entry.Document
		sequence := contentSequence(entry.Sequence)
		if sequence <= previous || sequence > high || d.SourceInstance != p.SourceInstance || d.OrgID != p.OrgID || d.ProjectID != p.ProjectID || d.Profile != p.Profile {
			return ContentManifestPage{}, ErrInvalid
		}
		canonical, err := connector.CanonicalizeJSONValue(m["items"].([]any)[i].(map[string]any)["document"])
		if err != nil || Hash(canonical) != entry.SourceSHA256 {
			return ContentManifestPage{}, ErrInvalid
		}
		if _, err = DecodeContentManifest(canonical); err != nil {
			return ContentManifestPage{}, err
		}
		if !validContentAvailability(d, entry.Availability) {
			return ContentManifestPage{}, ErrInvalid
		}
		previous = sequence
	}
	if previous != contentSequence(p.LastSequence) || p.HasMore != (previous < high) {
		return ContentManifestPage{}, ErrInvalid
	}
	retentionGap := false
	for _, gap := range p.Gaps {
		if gap.Reason == "retention_floor_ahead" {
			retentionGap = true
		}
		if gap.RangeKnown && contentSequence(gap.AfterSequence) >= contentSequence(gap.ThroughSequence) {
			return ContentManifestPage{}, ErrInvalid
		}
	}
	if contentSequence(p.RetentionFloor.AfterSequence) > contentSequence(p.CoverageFrom.AfterSequence) && !retentionGap {
		return ContentManifestPage{}, ErrInvalid
	}
	return p, nil
}

func contentSequence(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }

func validContentAvailability(m ContentManifest, a ContentAvailability) bool {
	saved := m.Representation != nil
	switch a.State {
	case "available":
		return saved && a.EvaluatedAt < m.ExpiresAt
	case "expired":
		return saved && a.EvaluatedAt >= m.ExpiresAt
	case "pending":
		return m.Capture.Completeness == "pending"
	case "not_captured":
		return m.Capture.Completeness == "not_captured"
	case "deleted", "missing", "corrupt":
		return saved
	default:
		return true // source_unavailable does not rewrite historical capture.
	}
}

// canonicalContentValue is for already typed values, never arbitrary source
// input: strict raw decoding must happen before a value reaches these helpers.
func canonicalContentValue(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, ErrInvalid
	}
	m, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		return nil, ErrInvalid
	}
	return connector.CanonicalizeJSONValue(m)
}

func validatedContentEntry(entry ContentManifestEntry) (ContentManifest, error) {
	raw, err := canonicalContentValue(entry.Document)
	if err != nil || Hash(raw) != entry.SourceSHA256 {
		return ContentManifest{}, ErrInvalid
	}
	return DecodeContentManifest(raw)
}

// FrozenContentManifestRef freezes a validated document and its exact source
// hash. sourceSystemID comes from XI's managed source, never the UR payload.
// No current authorization or availability decision is implied by freezing.
func FrozenContentManifestRef(sourceSystemID string, entry ContentManifestEntry) (FrozenManifestRef, error) {
	m, err := validatedContentEntry(entry)
	if err != nil {
		return FrozenManifestRef{}, err
	}
	r := FrozenManifestRef{SourceSystemID: sourceSystemID, SourceInstance: m.SourceInstance, OrgID: m.OrgID, ProjectID: m.ProjectID, RequestID: m.RequestID, SourceFactID: m.SourceFactID, Revision: m.Revision, SourceSHA256: entry.SourceSHA256, CaptureStatus: m.Capture.Completeness, ContentRef: m.ContentRef, Representation: m.Representation, ExpiresAt: m.ExpiresAt}
	raw, err := canonicalContentValue(r)
	if err != nil {
		return FrozenManifestRef{}, err
	}
	return DecodeFrozenManifestRef(raw)
}

func MatchContentManifestReference(ref FrozenManifestRef, entry ContentManifestEntry) error {
	expected, err := FrozenContentManifestRef(ref.SourceSystemID, entry)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(ref, expected) {
		return ErrInvalid
	}
	return nil
}

func MatchContentReadMetadata(metadata ContentReadMetadata, ref FrozenManifestRef) error {
	raw, err := canonicalContentValue(ref)
	if err != nil {
		return ErrInvalid
	}
	if _, err := DecodeFrozenManifestRef(raw); err != nil {
		return err
	}
	if ref.Representation == nil {
		return ErrInvalid
	}
	raw, err = canonicalContentValue(metadata)
	if err != nil {
		return ErrInvalid
	}
	if _, err := DecodeContentReadMetadata(raw); err != nil {
		return err
	}
	if metadata.SourceInstance != ref.SourceInstance || metadata.OrgID != ref.OrgID || metadata.ProjectID != ref.ProjectID || metadata.RequestID != ref.RequestID || metadata.SourceFactID != ref.SourceFactID || metadata.Revision != ref.Revision || metadata.SourceSHA256 != ref.SourceSHA256 || metadata.ContentRef != ref.ContentRef || metadata.ExpiresAt != ref.ExpiresAt || !reflect.DeepEqual(metadata.Representation, *ref.Representation) {
		return ErrInvalid
	}
	return nil
}

// Successor validation requires actual predecessor evidence. A missing
// predecessor must stay a chain gap; it cannot be proved by the revision alone.
func ValidateContentManifestSuccessor(previous, current ContentManifestEntry) error {
	p, err := validatedContentEntry(previous)
	if err != nil {
		return err
	}
	c, err := validatedContentEntry(current)
	if err != nil {
		return err
	}
	if c.SourceFactID != p.SourceFactID || c.PreviousRevision != p.Revision || c.PreviousHash != previous.SourceSHA256 || c.RecordedAt < p.RecordedAt {
		return ErrInvalid
	}
	if c.ContentRef != "" && c.ContentRef == p.ContentRef && !reflect.DeepEqual(c.Representation, p.Representation) {
		return ErrInvalid
	}
	return nil
}

func contentLocationWithin(location ContentLocation, savedBytes int64) bool {
	if location.Kind == "json_pointer" {
		return true
	} // actual bytes resolve the pointer later.
	return location.Kind == "byte_range" && location.Offset != nil && location.Length != nil && *location.Offset >= 0 && *location.Length >= 0 && *location.Offset <= savedBytes && *location.Length <= savedBytes-*location.Offset
}
