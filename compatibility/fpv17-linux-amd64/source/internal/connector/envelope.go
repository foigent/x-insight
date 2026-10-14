package connector

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	EventEnvelopeVersionV1 = "foigent.event-envelope.v1"
	EventBatchVersionV1    = "foigent.event-batch.v1"
	EventIdempotencyV1     = "foigent.event-idempotency.v1"
	MaxEnvelopeBytes       = 128 << 10
	MaxBatchBytes          = 8 << 20
	MaxBatchEvents         = 50
)

var (
	productPattern       = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	schemaRefPattern     = regexp.MustCompile(`^foigent://events/[a-z0-9][a-z0-9/_-]*/v[1-9][0-9]*$`)
	schemaVersionPattern = regexp.MustCompile(`^[1-9][0-9]*$`)
	subjectTypePattern   = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	fieldGroupPattern    = regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)
	positiveRefPattern   = regexp.MustCompile(`^[1-9][0-9]*$`)
)

type EnvelopeScope struct {
	TenantRef  *string
	OrgRef     string
	ProjectRef string
}

type ValidatedEnvelope struct {
	SpecVersion      string
	EventID          string
	EventType        string
	SchemaRef        string
	SchemaVersion    string
	ProducerProduct  string
	ProducerInstance string
	ConnectorID      string
	ConnectorVersion string
	Scope            EnvelopeScope
	SourceRevision   string
	IdempotencyKey   string
	SourceHash       string
	EnvelopeHash     string
	originalBytes    []byte
	canonicalBytes   []byte
	hashInputBytes   []byte
	payload          map[string]any
}

func (envelope ValidatedEnvelope) OriginalBytes() []byte {
	return bytes.Clone(envelope.originalBytes)
}

func (envelope ValidatedEnvelope) CanonicalBytes() []byte {
	return bytes.Clone(envelope.canonicalBytes)
}

func (envelope ValidatedEnvelope) HashInputBytes() []byte {
	return bytes.Clone(envelope.hashInputBytes)
}

func (envelope ValidatedEnvelope) Payload() map[string]any {
	payload, _ := cloneJSONValue(envelope.payload).(map[string]any)
	return payload
}

type StrictEnvelopeValidator struct{}

func (StrictEnvelopeValidator) ValidateEnvelope(
	payload []byte,
	binding EnvelopeBinding,
) (ValidatedEnvelope, *ConnectorError) {
	return ValidateEnvelope(payload, binding)
}

func ValidateEnvelope(
	payload []byte,
	binding EnvelopeBinding,
) (ValidatedEnvelope, *ConnectorError) {
	if err := binding.Validate(); err != nil {
		return ValidatedEnvelope{}, envelopeError(
			"foigent.binding.invalid",
			"projection binding is invalid",
		)
	}
	if len(payload) == 0 || len(payload) > MaxEnvelopeBytes {
		return ValidatedEnvelope{}, envelopeError(
			"foigent.envelope.size_invalid",
			"Event Envelope size is invalid",
		)
	}
	raw, err := decodeStrictJSONObject(payload, "Foigent Event Envelope")
	if err != nil {
		return ValidatedEnvelope{}, envelopeError(
			"foigent.envelope.json_invalid",
			"Event Envelope JSON is invalid",
		)
	}
	fields, err := validateEnvelopeCore(raw)
	if err != nil {
		return ValidatedEnvelope{}, envelopeError(
			"foigent.envelope.contract_invalid",
			"Event Envelope contract is invalid",
		)
	}
	registration := binding.ConnectorRegistration()
	if fields.SpecVersion != EventEnvelopeVersionV1 ||
		fields.EventType != registration.EventType ||
		fields.SchemaRef != registration.SchemaRef ||
		fields.SchemaVersion != registration.SchemaVersion ||
		fields.ProducerProduct != registration.SourceProduct ||
		fields.ConnectorID != registration.ConnectorID ||
		fields.ConnectorVersion != registration.ConnectorVersion {
		return ValidatedEnvelope{}, envelopeError(
			"foigent.envelope.binding_mismatch",
			"Event Envelope does not match the registered binding",
		)
	}
	tenant := ""
	if fields.Scope.TenantRef != nil {
		tenant = *fields.Scope.TenantRef
	}
	expectedIdempotency, err := EventIdempotencyKey(
		fields.ProducerProduct,
		fields.ProducerInstance,
		tenant,
		fields.Scope.OrgRef,
		fields.Scope.ProjectRef,
		fields.EventType,
		fields.EventID,
	)
	if err != nil || fields.IdempotencyKey != expectedIdempotency {
		return ValidatedEnvelope{}, envelopeError(
			"foigent.envelope.idempotency_mismatch",
			"Event Envelope idempotency identity is invalid",
		)
	}

	canonical, err := canonicalJSON(raw)
	if err != nil {
		return ValidatedEnvelope{}, envelopeError(
			"foigent.envelope.canonicalization_failed",
			"Event Envelope canonicalization failed",
		)
	}
	hashInput, _ := cloneJSONValue(raw).(map[string]any)
	integrity := hashInput["integrity"].(map[string]any)
	delete(integrity, "envelope_hash")
	hashInputBytes, err := canonicalJSON(hashInput)
	if err != nil {
		return ValidatedEnvelope{}, envelopeError(
			"foigent.envelope.canonicalization_failed",
			"Event Envelope hash input is invalid",
		)
	}
	digest := sha256.Sum256(hashInputBytes)
	expectedHash := "sha256:" + hex.EncodeToString(digest[:])
	if fields.EnvelopeHash != expectedHash {
		return ValidatedEnvelope{}, envelopeError(
			"foigent.envelope.hash_mismatch",
			"Event Envelope integrity hash is invalid",
		)
	}
	fields.originalBytes = bytes.Clone(payload)
	fields.canonicalBytes = canonical
	fields.hashInputBytes = hashInputBytes
	return fields, nil
}

// BuildCanonicalEnvelope finalizes a source-neutral Envelope hash input and
// immediately re-validates the resulting bytes against the supplied
// structurally valid binding. Product adapters must validate their exact local
// registration before calling. Delivery checkpoint, retry, fetch and ingestion
// state must remain outside hashInput.
func BuildCanonicalEnvelope(
	hashInput map[string]any,
	binding EnvelopeBinding,
) ([]byte, ValidatedEnvelope, *ConnectorError) {
	if err := binding.Validate(); err != nil {
		return nil, ValidatedEnvelope{}, envelopeError(
			"foigent.binding.invalid",
			"projection binding is invalid",
		)
	}
	cloned, ok := cloneJSONValue(hashInput).(map[string]any)
	if !ok {
		return nil, ValidatedEnvelope{}, envelopeError(
			"foigent.envelope.contract_invalid",
			"Event Envelope hash input must be an object",
		)
	}
	integrity, ok := cloned["integrity"].(map[string]any)
	if !ok {
		return nil, ValidatedEnvelope{}, envelopeError(
			"foigent.envelope.contract_invalid",
			"Event Envelope integrity must be an object",
		)
	}
	if _, exists := integrity["envelope_hash"]; exists {
		return nil, ValidatedEnvelope{}, envelopeError(
			"foigent.envelope.contract_invalid",
			"Event Envelope hash input must omit envelope_hash",
		)
	}
	canonicalHashInput, err := canonicalJSON(cloned)
	if err != nil {
		return nil, ValidatedEnvelope{}, envelopeError(
			"foigent.envelope.canonicalization_failed",
			"Event Envelope hash input is invalid",
		)
	}
	digest := sha256.Sum256(canonicalHashInput)
	integrity["envelope_hash"] = "sha256:" + hex.EncodeToString(digest[:])
	canonical, err := canonicalJSON(cloned)
	if err != nil {
		return nil, ValidatedEnvelope{}, envelopeError(
			"foigent.envelope.canonicalization_failed",
			"Event Envelope canonicalization failed",
		)
	}
	validated, connectorErr := ValidateEnvelope(canonical, binding)
	if connectorErr != nil {
		return nil, ValidatedEnvelope{}, connectorErr
	}
	return canonical, validated, nil
}

func EventIdempotencyKey(parts ...string) (string, error) {
	if len(parts) != 7 {
		return "", errors.New("Event idempotency requires seven identity components")
	}
	var input bytes.Buffer
	input.WriteString(EventIdempotencyV1)
	for _, part := range parts {
		if !utf8.ValidString(part) {
			return "", errors.New("Event idempotency component must be valid UTF-8")
		}
		if uint64(len(part)) > uint64(^uint32(0)) {
			return "", errors.New("Event idempotency component exceeds uint32 bytes")
		}
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(part)))
		input.Write(length[:])
		input.WriteString(part)
	}
	digest := sha256.Sum256(input.Bytes())
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validateEnvelopeCore(raw map[string]any) (ValidatedEnvelope, error) {
	if err := exactObjectFields(raw, "$", []string{
		"spec_version", "event_id", "event_type", "schema_ref", "schema_version",
		"producer", "scope", "subject", "actor", "time", "correlation",
		"identity", "integrity", "data_policy", "provenance", "payload",
	}, nil); err != nil {
		return ValidatedEnvelope{}, err
	}
	specVersion, err := requiredString(raw, "spec_version", "$")
	if err != nil || !validProtocolText(specVersion, 1, 128) {
		return ValidatedEnvelope{}, errors.New("$.spec_version is invalid")
	}
	eventID, err := requiredString(raw, "event_id", "$")
	if err != nil || !validOpaqueText(eventID, 1, 128) {
		return ValidatedEnvelope{}, errors.New("$.event_id is invalid")
	}
	eventType, err := requiredString(raw, "event_type", "$")
	if err != nil || len(eventType) > 128 || !eventTypePattern.MatchString(eventType) ||
		!norm.NFC.IsNormalString(eventType) {
		return ValidatedEnvelope{}, errors.New("$.event_type is invalid")
	}
	schemaRef, err := requiredString(raw, "schema_ref", "$")
	if err != nil || len(schemaRef) > 200 || !schemaRefPattern.MatchString(schemaRef) ||
		!norm.NFC.IsNormalString(schemaRef) {
		return ValidatedEnvelope{}, errors.New("$.schema_ref is invalid")
	}
	schemaVersion, err := requiredString(raw, "schema_version", "$")
	if err != nil || len(schemaVersion) > 20 ||
		!schemaVersionPattern.MatchString(schemaVersion) {
		return ValidatedEnvelope{}, errors.New("$.schema_version is invalid")
	}

	producer, err := requiredObject(raw, "producer", "$")
	if err != nil {
		return ValidatedEnvelope{}, err
	}
	if err := exactObjectFields(producer, "$.producer",
		[]string{"product", "instance", "connector_id", "connector_version"}, nil,
	); err != nil {
		return ValidatedEnvelope{}, err
	}
	product, err := requiredString(producer, "product", "$.producer")
	if err != nil || len(product) > 64 || !productPattern.MatchString(product) ||
		!norm.NFC.IsNormalString(product) {
		return ValidatedEnvelope{}, errors.New("$.producer.product is invalid")
	}
	instance, err := requiredString(producer, "instance", "$.producer")
	if err != nil || !validOpaqueText(instance, 1, 128) {
		return ValidatedEnvelope{}, errors.New("$.producer.instance is invalid")
	}
	connectorID, err := requiredString(producer, "connector_id", "$.producer")
	if err != nil || len(connectorID) > 160 ||
		!connectorIDPattern.MatchString(connectorID) ||
		!norm.NFC.IsNormalString(connectorID) {
		return ValidatedEnvelope{}, errors.New("$.producer.connector_id is invalid")
	}
	connectorVersion, err := requiredString(producer, "connector_version", "$.producer")
	if err != nil || !semverPattern.MatchString(connectorVersion) {
		return ValidatedEnvelope{}, errors.New("$.producer.connector_version is invalid")
	}

	scopeObject, err := requiredObject(raw, "scope", "$")
	if err != nil {
		return ValidatedEnvelope{}, err
	}
	if err := exactObjectFields(scopeObject, "$.scope",
		[]string{"org_ref", "project_ref"}, []string{"tenant_ref"},
	); err != nil {
		return ValidatedEnvelope{}, err
	}
	orgRef, err := requiredString(scopeObject, "org_ref", "$.scope")
	if err != nil || len(orgRef) > 20 || !positiveRefPattern.MatchString(orgRef) {
		return ValidatedEnvelope{}, errors.New("$.scope.org_ref is invalid")
	}
	projectRef, err := requiredString(scopeObject, "project_ref", "$.scope")
	if err != nil || len(projectRef) > 20 || !positiveRefPattern.MatchString(projectRef) {
		return ValidatedEnvelope{}, errors.New("$.scope.project_ref is invalid")
	}
	var tenantRef *string
	if rawTenant, exists := scopeObject["tenant_ref"]; exists {
		tenant, ok := rawTenant.(string)
		if !ok || !validOpaqueText(tenant, 1, 128) {
			return ValidatedEnvelope{}, errors.New("$.scope.tenant_ref is invalid")
		}
		tenantRef = &tenant
	}

	if err := validateSubjectActorTimeCorrelation(raw); err != nil {
		return ValidatedEnvelope{}, err
	}
	identity, err := requiredObject(raw, "identity", "$")
	if err != nil {
		return ValidatedEnvelope{}, err
	}
	if err := exactObjectFields(identity, "$.identity",
		[]string{"idempotency_key", "source_revision"}, nil,
	); err != nil {
		return ValidatedEnvelope{}, err
	}
	idempotency, err := requiredString(identity, "idempotency_key", "$.identity")
	if err != nil || !digestPattern.MatchString(idempotency) {
		return ValidatedEnvelope{}, errors.New("$.identity.idempotency_key is invalid")
	}
	sourceRevision, err := requiredString(identity, "source_revision", "$.identity")
	if err != nil || !validProtocolText(sourceRevision, 1, 64) {
		return ValidatedEnvelope{}, errors.New("$.identity.source_revision is invalid")
	}

	integrity, err := requiredObject(raw, "integrity", "$")
	if err != nil {
		return ValidatedEnvelope{}, err
	}
	if err := exactObjectFields(integrity, "$.integrity",
		[]string{"envelope_hash"}, []string{"source_hash"},
	); err != nil {
		return ValidatedEnvelope{}, err
	}
	envelopeHash, err := requiredString(integrity, "envelope_hash", "$.integrity")
	if err != nil || !digestPattern.MatchString(envelopeHash) {
		return ValidatedEnvelope{}, errors.New("$.integrity.envelope_hash is invalid")
	}
	sourceHash := ""
	if rawSourceHash, exists := integrity["source_hash"]; exists {
		var ok bool
		sourceHash, ok = rawSourceHash.(string)
		if !ok || !digestPattern.MatchString(sourceHash) {
			return ValidatedEnvelope{}, errors.New("$.integrity.source_hash is invalid")
		}
	}
	if err := validateDataPolicyAndProvenance(raw); err != nil {
		return ValidatedEnvelope{}, err
	}
	payload, ok := raw["payload"].(map[string]any)
	if !ok {
		return ValidatedEnvelope{}, errors.New("$.payload must be an object")
	}

	return ValidatedEnvelope{
		SpecVersion:      specVersion,
		EventID:          eventID,
		EventType:        eventType,
		SchemaRef:        schemaRef,
		SchemaVersion:    schemaVersion,
		ProducerProduct:  product,
		ProducerInstance: instance,
		ConnectorID:      connectorID,
		ConnectorVersion: connectorVersion,
		Scope: EnvelopeScope{
			TenantRef:  tenantRef,
			OrgRef:     orgRef,
			ProjectRef: projectRef,
		},
		SourceRevision: sourceRevision,
		IdempotencyKey: idempotency,
		SourceHash:     sourceHash,
		EnvelopeHash:   envelopeHash,
		payload:        payload,
	}, nil
}

func validateSubjectActorTimeCorrelation(raw map[string]any) error {
	subject, err := requiredObject(raw, "subject", "$")
	if err != nil {
		return err
	}
	if err := exactObjectFields(subject, "$.subject", []string{"type", "ref"}, nil); err != nil {
		return err
	}
	subjectType, err := requiredString(subject, "type", "$.subject")
	if err != nil || len(subjectType) > 64 ||
		!subjectTypePattern.MatchString(subjectType) ||
		!norm.NFC.IsNormalString(subjectType) {
		return errors.New("$.subject.type is invalid")
	}
	subjectRef, err := requiredString(subject, "ref", "$.subject")
	if err != nil || !validOpaqueText(subjectRef, 1, 128) {
		return errors.New("$.subject.ref is invalid")
	}

	actor, err := requiredObject(raw, "actor", "$")
	if err != nil {
		return err
	}
	if err := exactObjectFields(actor, "$.actor",
		[]string{"type"}, []string{"ref", "on_behalf_of"},
	); err != nil {
		return err
	}
	actorType, err := requiredString(actor, "type", "$.actor")
	if err != nil || !oneOf(actorType, "system", "service", "user", "unknown") {
		return errors.New("$.actor.type is invalid")
	}
	for _, field := range []string{"ref", "on_behalf_of"} {
		if value, exists := actor[field]; exists {
			text, ok := value.(string)
			if !ok || !validOpaqueText(text, 1, 128) {
				return fmt.Errorf("$.actor.%s is invalid", field)
			}
		}
	}

	timeObject, err := requiredObject(raw, "time", "$")
	if err != nil {
		return err
	}
	if err := exactObjectFields(timeObject, "$.time", nil,
		[]string{"occurred_at", "observed_at", "terminal_at"},
	); err != nil {
		return err
	}
	if len(timeObject) == 0 {
		return errors.New("$.time requires at least one timestamp")
	}
	for field, value := range timeObject {
		text, ok := value.(string)
		if !ok || !validMicrosecondTime(text) {
			return fmt.Errorf("$.time.%s is invalid", field)
		}
	}

	correlation, err := requiredObject(raw, "correlation", "$")
	if err != nil {
		return err
	}
	if err := exactObjectFields(correlation, "$.correlation", nil,
		[]string{"request_ref", "trace_ref", "span_ref", "causation_event_id"},
	); err != nil {
		return err
	}
	for field, value := range correlation {
		text, ok := value.(string)
		if !ok || !validOpaqueText(text, 1, 128) {
			return fmt.Errorf("$.correlation.%s is invalid", field)
		}
	}
	return nil
}

func validateDataPolicyAndProvenance(raw map[string]any) error {
	dataPolicy, err := requiredObject(raw, "data_policy", "$")
	if err != nil {
		return err
	}
	if err := exactObjectFields(dataPolicy, "$.data_policy",
		[]string{"payload_capture_mode", "raw_payload_present"},
		[]string{"redaction_profile"},
	); err != nil {
		return err
	}
	captureMode, err := requiredString(dataPolicy, "payload_capture_mode", "$.data_policy")
	if err != nil || !oneOf(captureMode, "none", "metadata_only", "redacted", "full") {
		return errors.New("$.data_policy.payload_capture_mode is invalid")
	}
	if _, ok := dataPolicy["raw_payload_present"].(bool); !ok {
		return errors.New("$.data_policy.raw_payload_present is invalid")
	}
	if value, exists := dataPolicy["redaction_profile"]; exists {
		text, ok := value.(string)
		if !ok || !validProtocolText(text, 1, 128) {
			return errors.New("$.data_policy.redaction_profile is invalid")
		}
	}

	provenance, err := requiredObject(raw, "provenance", "$")
	if err != nil {
		return err
	}
	if err := exactObjectFields(provenance, "$.provenance",
		[]string{"adapter_fidelity", "field_groups"}, nil,
	); err != nil {
		return err
	}
	fidelity, err := requiredString(provenance, "adapter_fidelity", "$.provenance")
	if err != nil || !oneOf(fidelity, "F0", "F1", "F2", "F3") {
		return errors.New("$.provenance.adapter_fidelity is invalid")
	}
	groups, err := requiredObject(provenance, "field_groups", "$.provenance")
	if err != nil || len(groups) == 0 {
		return errors.New("$.provenance.field_groups is invalid")
	}
	for key, value := range groups {
		classification, ok := value.(string)
		if !ok || !fieldGroupPattern.MatchString(key) ||
			!norm.NFC.IsNormalString(key) ||
			!oneOf(classification,
				"direct", "derived", "declared", "suppressed",
				"gap", "not_applicable", "unknown",
			) {
			return fmt.Errorf("$.provenance.field_groups.%s is invalid", key)
		}
	}
	return nil
}

func exactObjectFields(
	object map[string]any,
	path string,
	required []string,
	optional []string,
) error {
	allowed := make(map[string]struct{}, len(required)+len(optional))
	for _, field := range required {
		allowed[field] = struct{}{}
		value, exists := object[field]
		if !exists || value == nil {
			return fmt.Errorf("%s.%s is required", path, field)
		}
	}
	for _, field := range optional {
		allowed[field] = struct{}{}
	}
	for field, value := range object {
		if _, exists := allowed[field]; !exists {
			return fmt.Errorf("%s contains unknown field %q", path, field)
		}
		if value == nil {
			return fmt.Errorf("%s.%s must not be null", path, field)
		}
	}
	return nil
}

func requiredObject(parent map[string]any, field, path string) (map[string]any, error) {
	value, exists := parent[field]
	if !exists {
		return nil, fmt.Errorf("%s.%s is required", path, field)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s.%s must be an object", path, field)
	}
	return object, nil
}

func requiredString(parent map[string]any, field, path string) (string, error) {
	value, exists := parent[field]
	if !exists {
		return "", fmt.Errorf("%s.%s is required", path, field)
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s.%s must be a string", path, field)
	}
	return text, nil
}

func validOpaqueText(value string, minimum, maximum int) bool {
	if !utf8.ValidString(value) {
		return false
	}
	length := utf8.RuneCountInString(value)
	if length < minimum || length > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func validProtocolText(value string, minimum, maximum int) bool {
	return validOpaqueText(value, minimum, maximum) && norm.NFC.IsNormalString(value)
}

func envelopeError(code, safeMessage string) *ConnectorError {
	return NewConnectorError(
		ErrorContract,
		code,
		PhaseValidate,
		"validate_envelope",
		false,
		safeMessage,
		RecoveryOperatorRepair,
	)
}

func batchError(code, safeMessage string) *ConnectorError {
	return NewConnectorError(
		ErrorContract,
		code,
		PhaseValidate,
		"validate_batch",
		false,
		safeMessage,
		RecoveryOperatorRepair,
	)
}

type eventBatchWire struct {
	BatchVersion     string            `json:"batch_version"`
	ConnectorID      string            `json:"connector_id"`
	ConnectorVersion string            `json:"connector_version"`
	ProducerInstance string            `json:"producer_instance"`
	GeneratedAt      string            `json:"generated_at"`
	Events           []json.RawMessage `json:"events"`
	NextCursor       string            `json:"next_cursor"`
	HighWatermark    string            `json:"high_watermark"`
	HasMore          bool              `json:"has_more"`
}

type ValidatedBatch struct {
	BatchVersion     string
	ConnectorID      string
	ConnectorVersion string
	ProducerInstance string
	GeneratedAt      string
	Events           []ValidatedEnvelope
	NextCursor       string
	HighWatermark    string
	HasMore          bool
}

func ValidateEventBatch(
	payload []byte,
	binding ManifestBinding,
) (ValidatedBatch, *ConnectorError) {
	return ValidatePortableEventBatch(payload, binding)
}

// ValidatePortableEventBatch validates against the supplied product registration.
// The legacy entrypoint keeps its concrete binding type.
func ValidatePortableEventBatch(payload []byte, binding EnvelopeBinding) (ValidatedBatch, *ConnectorError) {
	if binding == nil {
		return ValidatedBatch{}, batchError("foigent.binding.invalid", "projection binding is required")
	}
	if err := binding.Validate(); err != nil {
		return ValidatedBatch{}, batchError(
			"foigent.binding.invalid",
			"projection binding is invalid",
		)
	}
	if len(payload) == 0 || len(payload) > MaxBatchBytes ||
		!utf8.Valid(payload) {
		return ValidatedBatch{}, batchError(
			"foigent.batch.size_invalid",
			"Event Batch size is invalid",
		)
	}
	if err := validateJSONUnicodeEscapes(payload, "Foigent Event Batch"); err != nil {
		return ValidatedBatch{}, batchError(
			"foigent.batch.json_invalid",
			"Event Batch JSON is invalid",
		)
	}
	if err := scanStrictJSON(payload, "Foigent Event Batch"); err != nil {
		return ValidatedBatch{}, batchError(
			"foigent.batch.json_invalid",
			"Event Batch JSON is invalid",
		)
	}
	raw, err := decodeStrictJSONObject(payload, "Foigent Event Batch")
	if err != nil {
		return ValidatedBatch{}, batchError(
			"foigent.batch.json_invalid",
			"Event Batch JSON is invalid",
		)
	}
	if err := exactObjectFields(raw, "$", []string{
		"batch_version", "connector_id", "connector_version", "producer_instance",
		"generated_at", "events", "next_cursor", "high_watermark", "has_more",
	}, nil); err != nil {
		return ValidatedBatch{}, batchError(
			"foigent.batch.contract_invalid",
			"Event Batch contract is invalid",
		)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var wire eventBatchWire
	if err := decoder.Decode(&wire); err != nil {
		return ValidatedBatch{}, batchError(
			"foigent.batch.contract_invalid",
			"Event Batch contract is invalid",
		)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ValidatedBatch{}, batchError(
			"foigent.batch.json_invalid",
			"Event Batch contains trailing content",
		)
	}
	registration := binding.ConnectorRegistration()
	if wire.BatchVersion != EventBatchVersionV1 ||
		wire.ConnectorID != registration.ConnectorID ||
		wire.ConnectorVersion != registration.ConnectorVersion ||
		!validOpaqueText(wire.ProducerInstance, 1, 128) ||
		!validMicrosecondTime(wire.GeneratedAt) ||
		len(wire.Events) > MaxBatchEvents ||
		len(wire.NextCursor) > 512 ||
		len(wire.HighWatermark) > 512 {
		return ValidatedBatch{}, batchError(
			"foigent.batch.binding_mismatch",
			"Event Batch does not match the registered binding",
		)
	}
	events := make([]ValidatedEnvelope, 0, len(wire.Events))
	for _, rawEvent := range wire.Events {
		event, connectorErr := ValidateEnvelope(rawEvent, binding)
		if connectorErr != nil || event.ProducerInstance != wire.ProducerInstance {
			return ValidatedBatch{}, batchError(
				"foigent.batch.event_invalid",
				"Event Batch contains an invalid Event Envelope",
			)
		}
		events = append(events, event)
	}
	return ValidatedBatch{
		BatchVersion:     wire.BatchVersion,
		ConnectorID:      wire.ConnectorID,
		ConnectorVersion: wire.ConnectorVersion,
		ProducerInstance: wire.ProducerInstance,
		GeneratedAt:      wire.GeneratedAt,
		Events:           events,
		NextCursor:       wire.NextCursor,
		HighWatermark:    wire.HighWatermark,
		HasMore:          wire.HasMore,
	}, nil
}

func validateBoundedOperationalText(value string, maximum int) bool {
	return utf8.ValidString(value) &&
		utf8.RuneCountInString(value) <= maximum &&
		!strings.ContainsRune(value, '\x00')
}
