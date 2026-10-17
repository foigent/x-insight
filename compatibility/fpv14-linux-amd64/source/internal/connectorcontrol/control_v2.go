package connectorcontrol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ControlOperationSchemaV2 = "xinsight.connector-control-operation.v2"
	ControlResultSchemaV2    = "xinsight.connector-control-result.v2"

	ControlPhasePreparePausedSource = "prepare_paused_source"
	ControlPhaseRegisterReceiver    = "register_receiver"
	ControlPhaseResumeSource        = "resume_source"

	ControlFeedSchema                    = "unirouter.completed-execution.v1"
	ControlCursorKey                     = "unirouter.completed-execution.v1"
	ReceiverRegistrationSchema           = "unirouter.xinsight.feed-credential-registration.v1"
	ReceiverTokenFormat                  = "ur-xf1-hex256.v1"
	ReceiverVerifierScheme               = "receiver_sha256_v1"
	ReceiverNetworkTransport             = "https"
	ReceiverFeedRoute                    = "/api/v1/integrations/xinsight/completed-executions"
	ReadinessPausedSourceCursorReady     = "paused_source_cursor_ready"
	ReadinessPausedReceiverRegistered    = "paused_receiver_registered"
	ReadinessPausedReceiverAuthenticated = "paused_receiver_authenticated"
	ReadinessSourceResumedReceiverActive = "source_resumed_receiver_active"
)

var (
	receiverKeyPrefixPattern = regexp.MustCompile(`^ur-xf1-[0-9a-f]{13}$`)
	receiverDigestPattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	receiverOperationPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

// ControlOperationRequestV2 is the exact persisted request-authority subset.
// Delivery lease/status metadata remains on ControlOperation and is excluded
// from this canonical identity.
type ControlOperationRequestV2 struct {
	SchemaVersion           string                `json:"schema_version"`
	OperationID             string                `json:"operation_id"`
	InstallationID          string                `json:"installation_id"`
	ProjectID               string                `json:"project_id"`
	EventScope              []string              `json:"event_scope"`
	DesiredState            string                `json:"desired_state"`
	Kind                    string                `json:"kind"`
	Phase                   string                `json:"phase"`
	PredecessorOperationID  string                `json:"predecessor_operation_id,omitempty"`
	PredecessorResultSHA256 string                `json:"predecessor_result_sha256,omitempty"`
	Request                 ControlPhaseRequestV2 `json:"request"`
}

// ControlPhaseRequestV2 is a closed, phase-discriminated union. Validation
// requires the exact field set for the selected phase.
type ControlPhaseRequestV2 struct {
	FeedSchema                     string                        `json:"feed_schema,omitempty"`
	CursorKey                      string                        `json:"cursor_key,omitempty"`
	SourceInstance                 string                        `json:"source_instance,omitempty"`
	ConsumerEpoch                  int64                         `json:"consumer_epoch,omitempty"`
	CheckpointAuthoritySHA256      string                        `json:"checkpoint_authority_sha256,omitempty"`
	ReceiverRegistration           *PublicReceiverRegistrationV1 `json:"receiver_registration,omitempty"`
	NetworkBinding                 *FixedReceiverNetworkBinding  `json:"network_binding,omitempty"`
	ReceiverRegistrationSHA256     string                        `json:"receiver_registration_sha256,omitempty"`
	NetworkBindingSHA256           string                        `json:"network_binding_sha256,omitempty"`
	PausedAuthReceiptSHA256        string                        `json:"paused_auth_receipt_sha256,omitempty"`
	DormantCredentialReceiptSHA256 string                        `json:"dormant_credential_receipt_sha256,omitempty"`
	RequiredSourceState            string                        `json:"required_source_state"`
	RequiredReceiverState          string                        `json:"required_receiver_state,omitempty"`
	Readiness                      string                        `json:"readiness,omitempty"`
}

type PublicReceiverRegistrationV1 struct {
	SchemaVersion  string `json:"schema_version"`
	TokenFormat    string `json:"token_format"`
	VerifierScheme string `json:"verifier_scheme"`
	KeyPrefix      string `json:"key_prefix"`
	VerifierSHA256 string `json:"verifier_sha256"`
	OperationID    string `json:"operation_id"`
}

type FixedReceiverNetworkBinding struct {
	Transport     string `json:"transport"`
	Route         string `json:"route"`
	BindingSHA256 string `json:"binding_sha256"`
}

type EnqueueControlPhaseInput struct {
	InstallationID          string
	Phase                   string
	PredecessorOperationID  string
	PredecessorResultSHA256 string
	Request                 ControlPhaseRequestV2
}

type ControlPhaseResultV2 struct {
	SourceInstance             string `json:"source_instance"`
	Cursor                     string `json:"cursor,omitempty"`
	HighWatermark              string `json:"high_watermark,omitempty"`
	ReceiverRegistrationSHA256 string `json:"receiver_registration_sha256,omitempty"`
	NetworkBindingSHA256       string `json:"network_binding_sha256,omitempty"`
	SourceState                string `json:"source_state"`
	ReceiverState              string `json:"receiver_state,omitempty"`
	Readiness                  string `json:"readiness"`
}

func DecodeControlOperationRequestV2(payload []byte) (ControlOperationRequestV2, error) {
	var value ControlOperationRequestV2
	if err := decodeStrictControlJSON(payload, &value); err != nil || ValidateControlOperationRequestV2(value) != nil {
		return ControlOperationRequestV2{}, ErrInvalid
	}
	return value, nil
}

func DecodeControlResultV2(payload []byte, now time.Time) (ControlOperationResult, error) {
	var value ControlOperationResult
	if err := decodeStrictControlJSON(payload, &value); err != nil || ValidateControlResultV2(value, now) != nil {
		return ControlOperationResult{}, ErrInvalid
	}
	return value, nil
}

func CanonicalControlOperationRequestV2(value ControlOperation) (ControlOperationRequestV2, string, []byte, error) {
	if value.SchemaVersion != ControlOperationSchemaV2 || value.Request == nil {
		return ControlOperationRequestV2{}, "", nil, ErrInvalid
	}
	request := ControlOperationRequestV2{
		SchemaVersion: value.SchemaVersion, OperationID: value.OperationID,
		InstallationID: value.InstallationID, ProjectID: value.ProjectID,
		EventScope: value.EventScope, DesiredState: value.DesiredState,
		Kind: value.Kind, Phase: value.Phase,
		PredecessorOperationID:  value.PredecessorOperationID,
		PredecessorResultSHA256: value.PredecessorResultSHA256,
		Request:                 *value.Request,
	}
	if err := ValidateControlOperationRequestV2(request); err != nil {
		return ControlOperationRequestV2{}, "", nil, err
	}
	digest, payload, err := CanonicalSHA256(request)
	return request, digest, payload, err
}

func CanonicalControlResultV2(value ControlOperationResult) (string, []byte, error) {
	if value.SchemaVersion != ControlResultSchemaV2 {
		return "", nil, ErrInvalid
	}
	return CanonicalSHA256(value)
}

func ValidateControlOperationRequestV2(value ControlOperationRequestV2) error {
	if value.SchemaVersion != ControlOperationSchemaV2 ||
		!operationIDPattern.MatchString(value.OperationID) ||
		!installationIDPattern.MatchString(value.InstallationID) ||
		!boundedIdentityPattern.MatchString(value.ProjectID) ||
		!equalEventScope(value.EventScope, []string{CompletedEventType}) ||
		value.DesiredState != "active" || value.Kind != "connect" {
		return ErrInvalid
	}
	if err := validatePhasePredecessor(value.Phase, value.PredecessorOperationID, value.PredecessorResultSHA256); err != nil {
		return err
	}
	return validatePhaseRequest(value.Phase, value.Request)
}

func ValidateControlOperationV2(value ControlOperation) error {
	request, digest, _, err := CanonicalControlOperationRequestV2(value)
	if err != nil || value.RequestSHA256 != digest || request.Phase != value.Phase {
		return ErrInvalid
	}
	if value.AttemptCount < 0 || value.AvailableAt.IsZero() || value.CreatedAt.IsZero() ||
		value.UpdatedAt.Before(value.CreatedAt) {
		return ErrInvalid
	}
	return validateControlDeliveryState(value)
}

func ValidateControlResultV2(value ControlOperationResult, now time.Time) error {
	if value.SchemaVersion != ControlResultSchemaV2 ||
		!operationIDPattern.MatchString(value.OperationID) ||
		!installationIDPattern.MatchString(value.InstallationID) ||
		!validControlPhase(value.Phase) || value.SourceInstance != "" ||
		value.ObservedAt.IsZero() || value.ObservedAt.After(now.Add(time.Minute)) {
		return ErrInvalid
	}
	switch value.Status {
	case "completed":
		if value.Result == nil || value.ReasonCode != "" {
			return ErrInvalid
		}
		return validatePhaseResult(value.Phase, *value.Result)
	case "failed":
		if value.Result != nil || !validReasonCode(value.ReasonCode) {
			return ErrInvalid
		}
		return nil
	default:
		return ErrInvalid
	}
}

func ValidateControlResultV2ForOperation(operation ControlOperation, result ControlOperationResult) error {
	if ValidateControlOperationV2(operation) != nil ||
		ValidateControlResultV2(result, result.ObservedAt) != nil ||
		result.OperationID != operation.OperationID ||
		result.InstallationID != operation.InstallationID || result.Phase != operation.Phase ||
		result.ObservedAt.Before(operation.CreatedAt) {
		return ErrInvalid
	}
	if result.Status != "completed" {
		return nil
	}
	switch operation.Phase {
	case ControlPhaseRegisterReceiver:
		registrationSHA, _, err := CanonicalSHA256(*operation.Request.ReceiverRegistration)
		if err != nil || result.Result.SourceInstance != operation.Request.SourceInstance ||
			result.Result.ReceiverRegistrationSHA256 != registrationSHA ||
			result.Result.NetworkBindingSHA256 != operation.Request.NetworkBinding.BindingSHA256 {
			return ErrInvalid
		}
	case ControlPhaseResumeSource:
		if result.Result.SourceInstance != operation.Request.SourceInstance {
			return ErrInvalid
		}
	}
	return nil
}

func ValidateControlOperationV2Predecessor(operation ControlOperationRequestV2, predecessor ControlOperationResult) error {
	if ValidateControlOperationRequestV2(operation) != nil ||
		ValidateControlResultV2(predecessor, predecessor.ObservedAt) != nil || predecessor.Status != "completed" {
		return ErrInvalid
	}
	expectedPhase := ""
	switch operation.Phase {
	case ControlPhasePreparePausedSource:
		return ErrInvalid
	case ControlPhaseRegisterReceiver:
		expectedPhase = ControlPhasePreparePausedSource
	case ControlPhaseResumeSource:
		expectedPhase = ControlPhaseRegisterReceiver
	}
	digest, _, err := CanonicalControlResultV2(predecessor)
	if err != nil || predecessor.Phase != expectedPhase ||
		predecessor.OperationID != operation.PredecessorOperationID ||
		digest != operation.PredecessorResultSHA256 ||
		predecessor.InstallationID != operation.InstallationID {
		return ErrInvalid
	}
	if predecessor.Result != nil && predecessor.Result.SourceInstance != "" &&
		operation.Request.SourceInstance != predecessor.Result.SourceInstance {
		return ErrInvalid
	}
	if operation.Phase == ControlPhaseResumeSource &&
		(operation.Request.ReceiverRegistrationSHA256 != predecessor.Result.ReceiverRegistrationSHA256 ||
			operation.Request.NetworkBindingSHA256 != predecessor.Result.NetworkBindingSHA256) {
		return ErrInvalid
	}
	return nil
}

func validatePhasePredecessor(phase, operationID, resultSHA string) error {
	switch phase {
	case ControlPhasePreparePausedSource:
		if operationID != "" || resultSHA != "" {
			return ErrInvalid
		}
	case ControlPhaseRegisterReceiver, ControlPhaseResumeSource:
		if !operationIDPattern.MatchString(operationID) || !hashPattern.MatchString(resultSHA) {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func validatePhaseRequest(phase string, value ControlPhaseRequestV2) error {
	switch phase {
	case ControlPhasePreparePausedSource:
		if value.FeedSchema != ControlFeedSchema || value.CursorKey != ControlCursorKey ||
			value.RequiredSourceState != "paused" || !value.onlyPrepareFields() {
			return ErrInvalid
		}
	case ControlPhaseRegisterReceiver:
		if !boundedIdentityPattern.MatchString(value.SourceInstance) || value.ConsumerEpoch <= 0 ||
			!hashPattern.MatchString(value.CheckpointAuthoritySHA256) ||
			value.ReceiverRegistration == nil || validatePublicReceiverRegistration(*value.ReceiverRegistration) != nil ||
			value.NetworkBinding == nil || validateFixedReceiverNetworkBinding(*value.NetworkBinding) != nil ||
			value.RequiredSourceState != "paused" || !value.onlyRegisterFields() {
			return ErrInvalid
		}
	case ControlPhaseResumeSource:
		if !boundedIdentityPattern.MatchString(value.SourceInstance) ||
			!hashPattern.MatchString(value.ReceiverRegistrationSHA256) ||
			!hashPattern.MatchString(value.NetworkBindingSHA256) ||
			!hashPattern.MatchString(value.PausedAuthReceiptSHA256) ||
			!hashPattern.MatchString(value.DormantCredentialReceiptSHA256) ||
			value.RequiredSourceState != "active" || value.RequiredReceiverState != "active" ||
			value.Readiness != ReadinessPausedReceiverAuthenticated || !value.onlyResumeFields() {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func validatePhaseResult(phase string, value ControlPhaseResultV2) error {
	if !boundedIdentityPattern.MatchString(value.SourceInstance) {
		return ErrInvalid
	}
	switch phase {
	case ControlPhasePreparePausedSource:
		if !validOpaqueControlText(value.Cursor, 512) || value.Cursor != value.HighWatermark ||
			value.SourceState != "paused" || value.Readiness != ReadinessPausedSourceCursorReady ||
			value.ReceiverRegistrationSHA256 != "" || value.NetworkBindingSHA256 != "" || value.ReceiverState != "" {
			return ErrInvalid
		}
	case ControlPhaseRegisterReceiver:
		if value.Cursor != "" || value.HighWatermark != "" ||
			!hashPattern.MatchString(value.ReceiverRegistrationSHA256) ||
			!hashPattern.MatchString(value.NetworkBindingSHA256) || value.SourceState != "paused" ||
			value.ReceiverState != "feed_staged" || value.Readiness != ReadinessPausedReceiverRegistered {
			return ErrInvalid
		}
	case ControlPhaseResumeSource:
		if value.Cursor != "" || value.HighWatermark != "" || value.ReceiverRegistrationSHA256 != "" ||
			value.NetworkBindingSHA256 != "" || value.SourceState != "active" ||
			value.ReceiverState != "active" || value.Readiness != ReadinessSourceResumedReceiverActive {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func validatePublicReceiverRegistration(value PublicReceiverRegistrationV1) error {
	if value.SchemaVersion != ReceiverRegistrationSchema || value.TokenFormat != ReceiverTokenFormat ||
		value.VerifierScheme != ReceiverVerifierScheme || !receiverKeyPrefixPattern.MatchString(value.KeyPrefix) ||
		!receiverDigestPattern.MatchString(value.VerifierSHA256) ||
		!receiverOperationPattern.MatchString(value.OperationID) {
		return ErrInvalid
	}
	return nil
}

func validateFixedReceiverNetworkBinding(value FixedReceiverNetworkBinding) error {
	if value.Transport != ReceiverNetworkTransport || value.Route != ReceiverFeedRoute ||
		!hashPattern.MatchString(value.BindingSHA256) {
		return ErrInvalid
	}
	return nil
}

func (value ControlPhaseRequestV2) onlyPrepareFields() bool {
	return value.SourceInstance == "" && value.ConsumerEpoch == 0 && value.CheckpointAuthoritySHA256 == "" &&
		value.ReceiverRegistration == nil && value.NetworkBinding == nil && value.ReceiverRegistrationSHA256 == "" &&
		value.NetworkBindingSHA256 == "" && value.PausedAuthReceiptSHA256 == "" &&
		value.DormantCredentialReceiptSHA256 == "" && value.RequiredReceiverState == "" && value.Readiness == ""
}

func (value ControlPhaseRequestV2) onlyRegisterFields() bool {
	return value.FeedSchema == "" && value.CursorKey == "" && value.ReceiverRegistrationSHA256 == "" &&
		value.NetworkBindingSHA256 == "" && value.PausedAuthReceiptSHA256 == "" &&
		value.DormantCredentialReceiptSHA256 == "" && value.RequiredReceiverState == "" && value.Readiness == ""
}

func (value ControlPhaseRequestV2) onlyResumeFields() bool {
	return value.FeedSchema == "" && value.CursorKey == "" && value.ConsumerEpoch == 0 &&
		value.CheckpointAuthoritySHA256 == "" && value.ReceiverRegistration == nil && value.NetworkBinding == nil
}

func validControlPhase(value string) bool {
	return value == ControlPhasePreparePausedSource || value == ControlPhaseRegisterReceiver || value == ControlPhaseResumeSource
}

func validOpaqueControlText(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && utf8.ValidString(value) && strings.TrimSpace(value) == value &&
		!strings.ContainsAny(value, "\x00\r\n")
}

func validateControlDeliveryState(value ControlOperation) error {
	switch value.Status {
	case "pending":
		if value.AttemptCount != 0 || value.LeaseExpiresAt != nil || value.CompletedAt != nil || value.ResultSHA256 != "" || value.ReasonCode != "" {
			return ErrInvalid
		}
	case "running":
		if value.AttemptCount < 1 || value.LeaseExpiresAt == nil || value.CompletedAt != nil || value.ResultSHA256 != "" || value.ReasonCode != "" {
			return ErrInvalid
		}
	case "completed":
		if value.LeaseExpiresAt != nil || value.CompletedAt == nil || !hashPattern.MatchString(value.ResultSHA256) || value.ReasonCode != "" {
			return ErrInvalid
		}
	case "failed":
		if value.LeaseExpiresAt != nil || value.CompletedAt == nil || !hashPattern.MatchString(value.ResultSHA256) || !validReasonCode(value.ReasonCode) {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func decodeStrictControlJSON(payload []byte, target any) error {
	if len(payload) == 0 || len(payload) > maximumControllerMessageBytes || rejectDuplicateControlJSON(payload) != nil {
		return ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return ErrInvalid
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrInvalid
	}
	return nil
}

func rejectDuplicateControlJSON(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := consumeUniqueJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return ErrInvalid
	}
	return nil
}

func consumeUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return ErrInvalid
			}
			if _, duplicate := seen[key]; duplicate {
				return ErrInvalid
			}
			seen[key] = struct{}{}
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return ErrInvalid
		}
	case '[':
		for decoder.More() {
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return ErrInvalid
		}
	default:
		return fmt.Errorf("%w: JSON delimiter", ErrInvalid)
	}
	return nil
}
