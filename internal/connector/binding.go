package connector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const (
	ProjectionBindingVersionV1     = "xinsight.foigent-projection-binding.v1"
	SourceNeutralBindingVersionV2  = "xinsight.foigent-source-neutral-projection-binding.v2"
	SourceNeutralProtocolVersionV2 = "2.0.2"
)

var (
	digestPattern      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	semverPattern      = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	connectorIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_-]*)+$`)
	eventTypePattern   = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_]*)+$`)
	uuidPattern        = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	gitCommitPattern   = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

type ArtifactPin struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type ProtocolBinding struct {
	Version       string `json:"version"`
	Tag           string `json:"tag"`
	Commit        string `json:"commit"`
	ReleaseDigest string `json:"release_digest"`
}

type SchemaBindings struct {
	EventEnvelope         ArtifactPin `json:"event_envelope"`
	ModelRequestCompleted ArtifactPin `json:"model_request_completed"`
	ConnectorManifest     ArtifactPin `json:"connector_manifest"`
	ConnectorLifecycle    ArtifactPin `json:"connector_lifecycle"`
	EventBatch            ArtifactPin `json:"event_batch"`
	Checkpoint            ArtifactPin `json:"checkpoint"`
	OperationalReceipt    ArtifactPin `json:"operational_receipt"`
	ConnectorError        ArtifactPin `json:"connector_error"`
}

type CompatibilityBindings struct {
	ProjectionContractSHA256  string `json:"projection_contract_sha256"`
	LifecycleMappingSHA256    string `json:"lifecycle_mapping_sha256"`
	IdentityMappingSHA256     string `json:"identity_mapping_sha256"`
	ReferenceVectorSHA256     string `json:"reference_vector_sha256"`
	CanonicalVectorSHA256     string `json:"canonical_vector_sha256"`
	ConformanceContractSHA256 string `json:"conformance_contract_sha256"`
}

type PortableConnectorBinding struct {
	ConnectorID        string `json:"connector_id"`
	ConnectorVersion   string `json:"connector_version"`
	SourceProduct      string `json:"source_product"`
	EventType          string `json:"event_type"`
	SchemaRef          string `json:"schema_ref"`
	SchemaVersion      string `json:"schema_version"`
	SourceSchema       string `json:"source_schema"`
	SourceSchemaSHA256 string `json:"source_schema_sha256"`
}

type ProjectionBinding struct {
	ProfileID         string `json:"profile_id"`
	SourceSystemID    string `json:"source_system_id"`
	AdapterManifestID string `json:"adapter_manifest_id"`
	CursorKey         string `json:"cursor_key"`
}

type ManifestBinding struct {
	BindingVersion    string                   `json:"binding_version"`
	Protocol          ProtocolBinding          `json:"protocol"`
	Schemas           SchemaBindings           `json:"schemas"`
	ReferenceManifest ArtifactPin              `json:"reference_manifest"`
	Compatibility     CompatibilityBindings    `json:"compatibility"`
	Connector         PortableConnectorBinding `json:"connector"`
	Projection        ProjectionBinding        `json:"projection"`
}

// EnvelopeBinding is the smallest source-neutral contract needed by the
// canonical Event Envelope and portable batch validators. A portable batch does
// not authorize checkpoint writes: transport consumers must enforce their own
// registered delivery mode. Legacy pull state operations keep the v1 binding.
type EnvelopeBinding interface {
	Validate() error
	ConnectorRegistration() PortableConnectorBinding
	ProjectionRegistration() ProjectionBinding
}

func (binding ManifestBinding) ConnectorRegistration() PortableConnectorBinding {
	return binding.Connector
}

func (binding ManifestBinding) ProjectionRegistration() ProjectionBinding {
	return binding.Projection
}

type SourceNeutralSchemaBindings struct {
	EventEnvelope      ArtifactPin `json:"event_envelope"`
	CaptureGap         ArtifactPin `json:"capture_gap"`
	ConnectorManifest  ArtifactPin `json:"connector_manifest"`
	ConnectorLifecycle ArtifactPin `json:"connector_lifecycle"`
	EventBatch         ArtifactPin `json:"event_batch"`
	Checkpoint         ArtifactPin `json:"checkpoint"`
	OperationalReceipt ArtifactPin `json:"operational_receipt"`
	ConnectorError     ArtifactPin `json:"connector_error"`
	DomainCatalog      ArtifactPin `json:"domain_catalog"`
	Source             ArtifactPin `json:"source"`
	Payload            ArtifactPin `json:"payload"`
}

type SourceNeutralMappingBindings struct {
	SourceToSanitized      ArtifactPin `json:"source_to_sanitized"`
	SanitizedToEvent       ArtifactPin `json:"sanitized_to_event"`
	EventToM0              ArtifactPin `json:"event_to_m0"`
	ConnectorCompatibility ArtifactPin `json:"connector_compatibility"`
	ReferenceProjection    ArtifactPin `json:"reference_projection"`
	ConsumerLifecycle      ArtifactPin `json:"consumer_lifecycle"`
	ConsumerIdentity       ArtifactPin `json:"consumer_identity"`
	ReferenceCompatibility ArtifactPin `json:"reference_compatibility"`
	V1ToV2Compatibility    ArtifactPin `json:"v1_to_v2_compatibility"`
}

type SourceNeutralConformanceBindings struct {
	Contract     ArtifactPin `json:"contract"`
	VectorBundle ArtifactPin `json:"vector_bundle"`
	Runner       ArtifactPin `json:"runner"`
}

type SourceNeutralProjectionBinding struct {
	ProfileID         string `json:"profile_id"`
	SourceSystemID    string `json:"source_system_id"`
	AdapterManifestID string `json:"adapter_manifest_id"`
}

// SourceNeutralManifestBinding is the product-owned v2 registration shape.
// It deliberately has no Cursor key: delivery mode and mutable operational
// state belong to the shared Manifest/receipt contracts, not to Event or M0
// identity.
type SourceNeutralManifestBinding struct {
	BindingVersion    string                           `json:"binding_version"`
	Protocol          ProtocolBinding                  `json:"protocol"`
	Schemas           SourceNeutralSchemaBindings      `json:"schemas"`
	ReferenceManifest ArtifactPin                      `json:"reference_manifest"`
	Mappings          SourceNeutralMappingBindings     `json:"mappings"`
	Conformance       SourceNeutralConformanceBindings `json:"conformance"`
	Connector         PortableConnectorBinding         `json:"connector"`
	Projection        SourceNeutralProjectionBinding   `json:"projection"`
}

func ParseSourceNeutralManifestBinding(payload []byte) (SourceNeutralManifestBinding, error) {
	if _, err := decodeStrictJSONObject(payload, "source-neutral projection binding"); err != nil {
		return SourceNeutralManifestBinding{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var binding SourceNeutralManifestBinding
	if err := decoder.Decode(&binding); err != nil {
		return SourceNeutralManifestBinding{}, fmt.Errorf("decode source-neutral projection binding: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return SourceNeutralManifestBinding{}, errors.New("source-neutral projection binding contains trailing content")
	}
	if err := binding.Validate(); err != nil {
		return SourceNeutralManifestBinding{}, err
	}
	return binding, nil
}

func (binding SourceNeutralManifestBinding) Validate() error {
	if binding.BindingVersion != SourceNeutralBindingVersionV2 ||
		binding.Protocol.Version != SourceNeutralProtocolVersionV2 ||
		binding.Protocol.Tag != "v"+SourceNeutralProtocolVersionV2 ||
		!gitCommitPattern.MatchString(binding.Protocol.Commit) ||
		!digestPattern.MatchString(binding.Protocol.ReleaseDigest) {
		return errors.New("source-neutral protocol binding identity is invalid")
	}
	pins := []ArtifactPin{
		binding.Schemas.EventEnvelope,
		binding.Schemas.CaptureGap,
		binding.Schemas.ConnectorManifest,
		binding.Schemas.ConnectorLifecycle,
		binding.Schemas.EventBatch,
		binding.Schemas.Checkpoint,
		binding.Schemas.OperationalReceipt,
		binding.Schemas.ConnectorError,
		binding.Schemas.DomainCatalog,
		binding.Schemas.Source,
		binding.Schemas.Payload,
		binding.ReferenceManifest,
		binding.Mappings.SourceToSanitized,
		binding.Mappings.SanitizedToEvent,
		binding.Mappings.EventToM0,
		binding.Mappings.ConnectorCompatibility,
		binding.Mappings.ReferenceProjection,
		binding.Mappings.ConsumerLifecycle,
		binding.Mappings.ConsumerIdentity,
		binding.Mappings.ReferenceCompatibility,
		binding.Mappings.V1ToV2Compatibility,
		binding.Conformance.Contract,
		binding.Conformance.VectorBundle,
		binding.Conformance.Runner,
	}
	seenPaths := make(map[string]struct{}, len(pins))
	for _, pin := range pins {
		if !validArtifactPin(pin) {
			return errors.New("source-neutral projection binding artifact pin is invalid")
		}
		if _, duplicate := seenPaths[pin.Path]; duplicate {
			return errors.New("source-neutral projection binding artifact path is duplicated")
		}
		seenPaths[pin.Path] = struct{}{}
	}
	if err := validatePortableConnector(binding.Connector); err != nil {
		return err
	}
	if !validProtocolText(binding.Projection.ProfileID, 1, 200) ||
		!uuidPattern.MatchString(binding.Projection.SourceSystemID) ||
		!uuidPattern.MatchString(binding.Projection.AdapterManifestID) {
		return errors.New("source-neutral product projection binding is invalid")
	}
	return nil
}

func (binding SourceNeutralManifestBinding) ConnectorRegistration() PortableConnectorBinding {
	return binding.Connector
}

func (binding SourceNeutralManifestBinding) ProjectionRegistration() ProjectionBinding {
	return ProjectionBinding{
		ProfileID:         binding.Projection.ProfileID,
		SourceSystemID:    binding.Projection.SourceSystemID,
		AdapterManifestID: binding.Projection.AdapterManifestID,
	}
}

func ValidateExactSourceNeutralRegistration(
	binding SourceNeutralManifestBinding,
	expected SourceNeutralManifestBinding,
) error {
	if err := binding.Validate(); err != nil {
		return err
	}
	if err := expected.Validate(); err != nil {
		return fmt.Errorf("expected source-neutral projection binding is invalid: %w", err)
	}
	if binding != expected {
		return errors.New("source-neutral projection binding does not match the exact registered binding")
	}
	return nil
}

func ParseManifestBinding(payload []byte) (ManifestBinding, error) {
	binding, err := parseManifestBinding(payload)
	if err != nil {
		return ManifestBinding{}, err
	}
	if err := binding.Validate(); err != nil {
		return ManifestBinding{}, err
	}
	return binding, nil
}

func parseManifestBinding(payload []byte) (ManifestBinding, error) {
	if _, err := decodeStrictJSONObject(payload, "projection binding"); err != nil {
		return ManifestBinding{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var binding ManifestBinding
	if err := decoder.Decode(&binding); err != nil {
		return ManifestBinding{}, fmt.Errorf("decode projection binding: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ManifestBinding{}, errors.New("projection binding contains trailing content")
	}
	return binding, nil
}

func (binding ManifestBinding) Validate() error {
	return validateManifestBindingShape(binding)
}

// ValidateExactRegistration compares one structurally valid binding with an
// injected product-owned registry entry. The neutral kernel does not own or
// enumerate Connector registrations.
func ValidateExactRegistration(binding ManifestBinding, expected ManifestBinding) error {
	if err := binding.Validate(); err != nil {
		return err
	}
	if err := expected.Validate(); err != nil {
		return fmt.Errorf("expected projection binding is invalid: %w", err)
	}
	if binding != expected {
		return errors.New("projection binding does not match the exact registered binding")
	}
	return nil
}

func validateManifestBindingShape(binding ManifestBinding) error {
	if binding.BindingVersion != ProjectionBindingVersionV1 {
		return errors.New("projection binding version is invalid")
	}
	if !semverPattern.MatchString(binding.Protocol.Version) ||
		binding.Protocol.Tag != "v"+binding.Protocol.Version ||
		!gitCommitPattern.MatchString(binding.Protocol.Commit) ||
		!digestPattern.MatchString(binding.Protocol.ReleaseDigest) {
		return errors.New("protocol binding identity is invalid")
	}
	schemaPins := []ArtifactPin{
		binding.Schemas.EventEnvelope,
		binding.Schemas.ModelRequestCompleted,
		binding.Schemas.ConnectorManifest,
		binding.Schemas.ConnectorLifecycle,
		binding.Schemas.EventBatch,
		binding.Schemas.Checkpoint,
		binding.Schemas.OperationalReceipt,
		binding.Schemas.ConnectorError,
	}
	for _, pin := range schemaPins {
		if !validArtifactPin(pin) {
			return errors.New("projection binding artifact pin is invalid")
		}
	}
	digests := []string{
		binding.ReferenceManifest.SHA256,
		binding.Compatibility.ProjectionContractSHA256,
		binding.Compatibility.LifecycleMappingSHA256,
		binding.Compatibility.IdentityMappingSHA256,
		binding.Compatibility.ReferenceVectorSHA256,
		binding.Compatibility.CanonicalVectorSHA256,
		binding.Compatibility.ConformanceContractSHA256,
		binding.Connector.SourceSchemaSHA256,
	}
	for _, digest := range digests {
		if !digestPattern.MatchString(digest) {
			return errors.New("projection binding compatibility digest is invalid")
		}
	}
	if binding.ReferenceManifest.Path == "" {
		return errors.New("projection binding reference manifest is invalid")
	}
	if err := validatePortableConnector(binding.Connector); err != nil {
		return err
	}
	if !validProtocolText(binding.Projection.ProfileID, 1, 200) ||
		!uuidPattern.MatchString(binding.Projection.SourceSystemID) ||
		!uuidPattern.MatchString(binding.Projection.AdapterManifestID) ||
		!validProtocolText(binding.Projection.CursorKey, 1, 160) {
		return errors.New("product-local projection binding is invalid")
	}
	return nil
}

func validatePortableConnector(binding PortableConnectorBinding) error {
	if !connectorIDPattern.MatchString(binding.ConnectorID) ||
		len(binding.ConnectorID) > 160 ||
		!semverPattern.MatchString(binding.ConnectorVersion) ||
		!productPattern.MatchString(binding.SourceProduct) ||
		len(binding.SourceProduct) > 64 ||
		!eventTypePattern.MatchString(binding.EventType) ||
		len(binding.EventType) > 128 ||
		!schemaRefPattern.MatchString(binding.SchemaRef) ||
		len(binding.SchemaRef) > 200 ||
		!schemaVersionPattern.MatchString(binding.SchemaVersion) ||
		len(binding.SchemaVersion) > 20 ||
		!validProtocolText(binding.SourceSchema, 1, 200) ||
		!digestPattern.MatchString(binding.SourceSchemaSHA256) {
		return errors.New("portable Connector binding is invalid")
	}
	return nil
}

func validArtifactPin(pin ArtifactPin) bool {
	return pin.Path != "" &&
		!strings.HasPrefix(pin.Path, "/") &&
		!strings.Contains(pin.Path, `\`) &&
		!strings.Contains(pin.Path, "..") &&
		digestPattern.MatchString(pin.SHA256)
}
