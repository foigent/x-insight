package connector

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	LifecycleVersionV1  = "foigent.connector-lifecycle.v1"
	CheckpointVersionV1 = "foigent.connector-checkpoint.v1"
	ErrorVersionV1      = "foigent.connector-error.v1"
	ReceiptVersionV1    = "foigent.operational-receipt.v1"
)

type ErrorCategory string

const (
	ErrorConfiguration  ErrorCategory = "configuration"
	ErrorAuthentication ErrorCategory = "authentication"
	ErrorAuthorization  ErrorCategory = "authorization"
	ErrorTransport      ErrorCategory = "transport"
	ErrorSource         ErrorCategory = "source"
	ErrorContract       ErrorCategory = "contract"
	ErrorPrivacy        ErrorCategory = "privacy"
	ErrorCursor         ErrorCategory = "cursor"
	ErrorConflict       ErrorCategory = "conflict"
	ErrorPersistence    ErrorCategory = "persistence"
	ErrorInternal       ErrorCategory = "internal"
)

type ErrorPhase string

const (
	PhaseConfigure  ErrorPhase = "configure"
	PhaseActivate   ErrorPhase = "activate"
	PhaseFetch      ErrorPhase = "fetch"
	PhaseDecode     ErrorPhase = "decode"
	PhaseValidate   ErrorPhase = "validate"
	PhaseNormalize  ErrorPhase = "normalize"
	PhaseCommit     ErrorPhase = "commit"
	PhaseCheckpoint ErrorPhase = "checkpoint"
	PhaseStatus     ErrorPhase = "status"
	PhaseShutdown   ErrorPhase = "shutdown"
)

type ErrorRecovery string

const (
	RecoveryAutomaticRetry ErrorRecovery = "automatic_retry"
	RecoveryRestart        ErrorRecovery = "restart"
	RecoveryReauthorize    ErrorRecovery = "reauthorize"
	RecoveryOperatorRepair ErrorRecovery = "operator_repair"
	RecoveryExplicitResync ErrorRecovery = "explicit_resync"
	RecoveryNone           ErrorRecovery = "none"
)

var errorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)

type ConnectorError struct {
	ErrorVersion      string        `json:"error_version"`
	Category          ErrorCategory `json:"category"`
	Code              string        `json:"code"`
	Phase             ErrorPhase    `json:"phase"`
	Operation         string        `json:"operation"`
	Retryable         bool          `json:"retryable"`
	RetryAfterSeconds *int          `json:"retry_after_seconds,omitempty"`
	SafeMessage       string        `json:"safe_message"`
	Recovery          ErrorRecovery `json:"recovery"`
}

func (err *ConnectorError) Error() string {
	if err == nil {
		return "Connector operation failed"
	}
	return err.SafeMessage
}

func NewConnectorError(
	category ErrorCategory,
	code string,
	phase ErrorPhase,
	operation string,
	retryable bool,
	safeMessage string,
	recovery ErrorRecovery,
) *ConnectorError {
	return &ConnectorError{
		ErrorVersion: ErrorVersionV1,
		Category:     category,
		Code:         code,
		Phase:        phase,
		Operation:    operation,
		Retryable:    retryable,
		SafeMessage:  safeMessage,
		Recovery:     recovery,
	}
}

func (err ConnectorError) Validate() error {
	if err.ErrorVersion != ErrorVersionV1 ||
		!validErrorCategory(err.Category) ||
		!validErrorPhase(err.Phase) ||
		!validRecovery(err.Recovery) ||
		!errorCodePattern.MatchString(err.Code) ||
		len(err.Code) > 160 ||
		!errorCodePattern.MatchString(err.Operation) ||
		len(err.Operation) > 128 {
		return errors.New("Connector Error identity is invalid")
	}
	if err.SafeMessage == "" ||
		utf8.RuneCountInString(err.SafeMessage) > 512 ||
		!utf8.ValidString(err.SafeMessage) ||
		containsControl(err.SafeMessage) {
		return errors.New("Connector Error safe_message is invalid")
	}
	if err.RetryAfterSeconds != nil {
		if !err.Retryable || *err.RetryAfterSeconds < 0 || *err.RetryAfterSeconds > 300 {
			return errors.New("Connector Error retry_after_seconds is invalid")
		}
	}
	return nil
}

type ConnectorLifecycle struct {
	LifecycleVersion string `json:"lifecycle_version"`
	Configuration    string `json:"configuration"`
	Authentication   string `json:"authentication"`
	Transport        string `json:"transport"`
	Processing       string `json:"processing"`
	Checkpoint       string `json:"checkpoint"`
	Health           string `json:"health"`
}

type LocalLifecycleState struct {
	SourceStatus        string
	AdapterActive       bool
	Activated           bool
	AuthorizationRef    string
	WorkerStatus        string
	Committing          bool
	PagesCommitted      int64
	ConsecutiveFailures int64
	Retired             bool
	LastError           *ConnectorError
}

func ProjectLifecycle(state LocalLifecycleState) (ConnectorLifecycle, error) {
	if state.LastError != nil {
		if err := state.LastError.Validate(); err != nil {
			return ConnectorLifecycle{}, err
		}
	}
	lifecycle := ConnectorLifecycle{LifecycleVersion: LifecycleVersionV1}
	switch {
	case state.Retired:
		lifecycle.Configuration = "retired"
	case state.SourceStatus == "active" && state.AdapterActive && state.Activated:
		lifecycle.Configuration = "configured"
	default:
		lifecycle.Configuration = "unconfigured"
	}
	switch {
	case state.LastError != nil &&
		(state.LastError.Category == ErrorAuthentication || state.LastError.Category == ErrorAuthorization):
		lifecycle.Authentication = "error"
	case strings.TrimSpace(state.AuthorizationRef) != "":
		lifecycle.Authentication = "valid"
	default:
		lifecycle.Authentication = "absent"
	}
	switch state.WorkerStatus {
	case "inactive", "stopping":
		lifecycle.Transport = "stopped"
	case "backoff":
		lifecycle.Transport = "backoff"
	case "error":
		lifecycle.Transport = "error"
	case "idle", "polling":
		lifecycle.Transport = "ready"
	default:
		return ConnectorLifecycle{}, fmt.Errorf("unsupported local worker status %q", state.WorkerStatus)
	}
	if state.Committing {
		lifecycle.Processing = "committing"
	} else {
		lifecycle.Processing = state.WorkerStatus
	}
	switch {
	case !state.Activated:
		lifecycle.Checkpoint = "absent"
	case state.LastError != nil &&
		(state.LastError.Category == ErrorCursor || state.LastError.Category == ErrorConflict):
		lifecycle.Checkpoint = "error"
	case state.PagesCommitted == 0:
		lifecycle.Checkpoint = "bootstrap"
	default:
		lifecycle.Checkpoint = "steady"
	}
	switch {
	case state.Retired:
		lifecycle.Health = "retired"
	case state.WorkerStatus == "inactive" || state.WorkerStatus == "stopping":
		lifecycle.Health = "stopped"
	case state.WorkerStatus == "error":
		lifecycle.Health = "permanent_error"
	case state.WorkerStatus == "backoff" || state.ConsecutiveFailures > 0:
		lifecycle.Health = "degraded"
	case lifecycle.Configuration == "configured" && state.WorkerStatus == "idle":
		lifecycle.Health = "healthy"
	default:
		lifecycle.Health = "unknown"
	}
	if err := lifecycle.Validate(); err != nil {
		return ConnectorLifecycle{}, err
	}
	return lifecycle, nil
}

func (lifecycle ConnectorLifecycle) Validate() error {
	if lifecycle.LifecycleVersion != LifecycleVersionV1 ||
		!oneOf(lifecycle.Configuration, "unconfigured", "configured", "retired") ||
		!oneOf(lifecycle.Authentication, "absent", "staged", "valid", "revoked", "error") ||
		!oneOf(lifecycle.Transport, "stopped", "starting", "ready", "backoff", "error") ||
		!oneOf(lifecycle.Processing, "inactive", "idle", "polling", "committing", "backoff", "stopping", "error") ||
		!oneOf(lifecycle.Checkpoint, "absent", "bootstrap", "steady", "error") ||
		!oneOf(lifecycle.Health, "unknown", "healthy", "degraded", "permanent_error", "stopped", "retired") {
		return errors.New("Connector Lifecycle projection is invalid")
	}
	return nil
}

type Checkpoint struct {
	CheckpointVersion string `json:"checkpoint_version"`
	ConnectorID       string `json:"connector_id"`
	ConnectorVersion  string `json:"connector_version"`
	ProducerInstance  string `json:"producer_instance"`
	Cursor            string `json:"cursor"`
	HighWatermark     string `json:"high_watermark"`
	Phase             string `json:"phase"`
	UpdatedAt         string `json:"updated_at"`
}

type LocalCheckpointState struct {
	ProducerInstance string
	Cursor           string
	HighWatermark    string
	Phase            string
	UpdatedAt        time.Time
}

func ProjectCheckpoint(binding ManifestBinding, state LocalCheckpointState) (Checkpoint, error) {
	if err := binding.Validate(); err != nil {
		return Checkpoint{}, err
	}
	updatedAt, err := formatMicrosecondTime(state.UpdatedAt)
	if err != nil {
		return Checkpoint{}, err
	}
	checkpoint := Checkpoint{
		CheckpointVersion: CheckpointVersionV1,
		ConnectorID:       binding.Connector.ConnectorID,
		ConnectorVersion:  binding.Connector.ConnectorVersion,
		ProducerInstance:  state.ProducerInstance,
		Cursor:            state.Cursor,
		HighWatermark:     state.HighWatermark,
		Phase:             state.Phase,
		UpdatedAt:         updatedAt,
	}
	if err := checkpoint.Validate(); err != nil {
		return Checkpoint{}, err
	}
	return checkpoint, nil
}

func (checkpoint Checkpoint) Validate() error {
	if checkpoint.CheckpointVersion != CheckpointVersionV1 ||
		!connectorIDPattern.MatchString(checkpoint.ConnectorID) ||
		len(checkpoint.ConnectorID) > 160 ||
		!semverPattern.MatchString(checkpoint.ConnectorVersion) ||
		!validOpaqueText(checkpoint.ProducerInstance, 1, 128) ||
		!validateBoundedOperationalText(checkpoint.Cursor, 512) ||
		!validateBoundedOperationalText(checkpoint.HighWatermark, 512) ||
		!oneOf(checkpoint.Phase, "bootstrap", "steady", "error") ||
		!validMicrosecondTime(checkpoint.UpdatedAt) {
		return errors.New("Connector Checkpoint is invalid")
	}
	return nil
}

type ReceiptCounts struct {
	Received      int64 `json:"received"`
	Inserted      int64 `json:"inserted"`
	DuplicateNoop int64 `json:"duplicate_noop"`
	Conflict      int64 `json:"conflict"`
	Rejected      int64 `json:"rejected"`
}

type OperationalReceipt struct {
	ReceiptVersion   string          `json:"receipt_version"`
	OperationID      string          `json:"operation_id"`
	ConnectorID      string          `json:"connector_id"`
	ProducerInstance string          `json:"producer_instance"`
	Operation        string          `json:"operation"`
	Status           string          `json:"status"`
	Counts           ReceiptCounts   `json:"counts"`
	CheckpointBefore string          `json:"checkpoint_before,omitempty"`
	CheckpointAfter  string          `json:"checkpoint_after,omitempty"`
	ObservedAt       string          `json:"observed_at"`
	Error            *ConnectorError `json:"error,omitempty"`
}

type OperationalReceiptInput struct {
	OperationID      string
	ProducerInstance string
	Operation        string
	Status           string
	Counts           ReceiptCounts
	CheckpointBefore string
	CheckpointAfter  string
	ObservedAt       time.Time
	Error            *ConnectorError
}

func ProjectOperationalReceipt(
	binding ManifestBinding,
	input OperationalReceiptInput,
) (OperationalReceipt, error) {
	if err := binding.Validate(); err != nil {
		return OperationalReceipt{}, err
	}
	observedAt, err := formatMicrosecondTime(input.ObservedAt)
	if err != nil {
		return OperationalReceipt{}, err
	}
	receipt := OperationalReceipt{
		ReceiptVersion:   ReceiptVersionV1,
		OperationID:      input.OperationID,
		ConnectorID:      binding.Connector.ConnectorID,
		ProducerInstance: input.ProducerInstance,
		Operation:        input.Operation,
		Status:           input.Status,
		Counts:           input.Counts,
		CheckpointBefore: input.CheckpointBefore,
		CheckpointAfter:  input.CheckpointAfter,
		ObservedAt:       observedAt,
		Error:            input.Error,
	}
	if err := receipt.Validate(); err != nil {
		return OperationalReceipt{}, err
	}
	return receipt, nil
}

func (receipt OperationalReceipt) Validate() error {
	if receipt.ReceiptVersion != ReceiptVersionV1 ||
		!validOpaqueText(receipt.OperationID, 1, 128) ||
		!connectorIDPattern.MatchString(receipt.ConnectorID) ||
		len(receipt.ConnectorID) > 160 ||
		!validOpaqueText(receipt.ProducerInstance, 1, 128) ||
		!oneOf(receipt.Operation, "activate", "poll", "commit_page", "pause", "resume", "stop", "start", "rotate", "retire") ||
		!oneOf(receipt.Status, "accepted", "completed", "retryable_error", "permanent_error") ||
		!validateBoundedOperationalText(receipt.CheckpointBefore, 512) ||
		!validateBoundedOperationalText(receipt.CheckpointAfter, 512) ||
		!validMicrosecondTime(receipt.ObservedAt) {
		return errors.New("Operational Receipt identity is invalid")
	}
	counts := receipt.Counts
	if counts.Received < 0 || counts.Inserted < 0 || counts.DuplicateNoop < 0 ||
		counts.Conflict < 0 || counts.Rejected < 0 {
		return errors.New("Operational Receipt counts are invalid")
	}
	switch receipt.Status {
	case "completed":
		if receipt.Error != nil || counts.Conflict != 0 || counts.Rejected != 0 ||
			counts.Inserted+counts.DuplicateNoop != counts.Received {
			return errors.New("completed Operational Receipt did not fully converge")
		}
	case "retryable_error":
		if receipt.Error == nil || !receipt.Error.Retryable {
			return errors.New("retryable Operational Receipt requires a retryable error")
		}
	case "permanent_error":
		if receipt.Error == nil || receipt.Error.Retryable {
			return errors.New("permanent Operational Receipt requires a permanent error")
		}
	}
	if receipt.Error != nil {
		if err := receipt.Error.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func validErrorCategory(category ErrorCategory) bool {
	return oneOf(string(category),
		string(ErrorConfiguration), string(ErrorAuthentication), string(ErrorAuthorization),
		string(ErrorTransport), string(ErrorSource), string(ErrorContract), string(ErrorPrivacy),
		string(ErrorCursor), string(ErrorConflict), string(ErrorPersistence), string(ErrorInternal),
	)
}

func validErrorPhase(phase ErrorPhase) bool {
	return oneOf(string(phase),
		string(PhaseConfigure), string(PhaseActivate), string(PhaseFetch), string(PhaseDecode),
		string(PhaseValidate), string(PhaseNormalize), string(PhaseCommit), string(PhaseCheckpoint),
		string(PhaseStatus), string(PhaseShutdown),
	)
}

func validRecovery(recovery ErrorRecovery) bool {
	return oneOf(string(recovery),
		string(RecoveryAutomaticRetry), string(RecoveryRestart), string(RecoveryReauthorize),
		string(RecoveryOperatorRepair), string(RecoveryExplicitResync), string(RecoveryNone),
	)
}

func oneOf(value string, accepted ...string) bool {
	for _, candidate := range accepted {
		if value == candidate {
			return true
		}
	}
	return false
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func formatMicrosecondTime(value time.Time) (string, error) {
	if value.IsZero() {
		return "", errors.New("operational time is required")
	}
	if value.Nanosecond()%int(time.Microsecond) != 0 {
		return "", errors.New("operational time must have exact microsecond precision")
	}
	return value.UTC().Format("2006-01-02T15:04:05.000000Z"), nil
}

func validMicrosecondTime(value string) bool {
	const layout = "2006-01-02T15:04:05.000000Z"
	parsed, err := time.Parse(layout, value)
	return err == nil && parsed.Format(layout) == value
}
