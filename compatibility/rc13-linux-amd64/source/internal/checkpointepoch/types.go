// Package checkpointepoch owns source-neutral, operator-mediated transitions
// between durable Connector delivery epochs. Delivery state is deliberately
// separate from normalized evidence identity, event hashes, and provenance.
package checkpointepoch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	PlanSchema   = "xinsight.connector-checkpoint-epoch-plan.v1"
	ReportSchema = "xinsight.connector-checkpoint-epoch-report.v1"

	StatusPrepared  = "prepared"
	StatusActivated = "activated"
	StatusAborted   = "aborted"

	CompletedExecutionWorkerUnit = "xinsight-feed.service"
	LiteLLMWorkerUnit            = "xinsight-litellm-feed.service"

	CompletedExecutionCursorKey = "unirouter.completed-execution.v1"
	LiteLLMCursorKey            = "litellm.spend-logs.v1"

	completedExecutionBoundaryBytes = 512
	liteLLMBoundaryBytes            = 512 << 10
)

var (
	ErrInvalid          = errors.New("connector checkpoint epoch input is invalid")
	ErrNotReady         = errors.New("connector checkpoint state is not ready for an epoch transition")
	ErrConflict         = errors.New("connector checkpoint epoch state changed concurrently")
	ErrOperationMissing = errors.New("connector checkpoint epoch operation was not found")

	uuidPattern      = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	operationPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	sha256Pattern    = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type Binding struct {
	SourceSystemID    string `json:"source_system_id"`
	AdapterManifestID string `json:"adapter_manifest_id"`
	CursorKey         string `json:"cursor_key"`
}

type PrepareInput struct {
	Binding
	WorkerUnit         string `json:"worker_unit"`
	NextSourceInstance string `json:"next_source_instance"`
	NextCursor         string `json:"-"`
	NextHighWatermark  string `json:"-"`
	AuthorizationRef   string `json:"authorization_ref"`
}

type workerProfile struct {
	cursorKey        string
	maxBoundaryBytes int
}

type Plan struct {
	SchemaVersion               string    `json:"schema_version"`
	OperationID                 string    `json:"operation_id"`
	Binding                     Binding   `json:"binding"`
	WorkerUnit                  string    `json:"worker_unit"`
	ExpectedActiveStateSHA256   string    `json:"expected_active_state_sha256"`
	PreviousEpoch               int64     `json:"previous_epoch"`
	PreviousSourceInstance      string    `json:"previous_source_instance"`
	PreviousCursorSHA256        string    `json:"previous_cursor_sha256"`
	PreviousHighWatermarkSHA256 string    `json:"previous_high_watermark_sha256"`
	NextEpoch                   int64     `json:"next_epoch"`
	NextSourceInstance          string    `json:"next_source_instance"`
	NextCursorSHA256            string    `json:"next_cursor_sha256"`
	NextHighWatermarkSHA256     string    `json:"next_high_watermark_sha256"`
	AuthorizationRef            string    `json:"authorization_ref"`
	PreparedAt                  time.Time `json:"prepared_at"`
	PlanSHA256                  string    `json:"plan_sha256"`
}

type ActiveStateSummary struct {
	StateID             string     `json:"state_id"`
	Binding             Binding    `json:"binding"`
	Epoch               int64      `json:"epoch"`
	EpochOperationID    string     `json:"epoch_operation_id,omitempty"`
	SourceInstance      string     `json:"source_instance"`
	CursorSHA256        string     `json:"cursor_sha256"`
	HighWatermarkSHA256 string     `json:"high_watermark_sha256"`
	WorkerStatus        string     `json:"worker_status"`
	LastErrorCode       string     `json:"last_error_code,omitempty"`
	PagesCommitted      int64      `json:"pages_committed"`
	ItemsReceived       int64      `json:"items_received"`
	ItemsCommitted      int64      `json:"items_committed"`
	DuplicateNoop       int64      `json:"duplicate_noop"`
	ConsecutiveFailures int64      `json:"consecutive_failures"`
	LastPollSucceededAt *time.Time `json:"last_poll_succeeded_at,omitempty"`
	StateSHA256         string     `json:"state_sha256"`
}

type OperationSummary struct {
	OperationID string     `json:"operation_id"`
	Status      string     `json:"status"`
	Plan        Plan       `json:"plan"`
	ActivatedAt *time.Time `json:"activated_at,omitempty"`
	AbortedAt   *time.Time `json:"aborted_at,omitempty"`
}

type Report struct {
	SchemaVersion string             `json:"schema_version"`
	Active        ActiveStateSummary `json:"active"`
	Operation     *OperationSummary  `json:"operation,omitempty"`
	GeneratedAt   time.Time          `json:"generated_at"`
}

func ValidateBinding(binding Binding) error {
	if !uuidPattern.MatchString(binding.SourceSystemID) {
		return fmt.Errorf("%w: source_system_id", ErrInvalid)
	}
	if !uuidPattern.MatchString(binding.AdapterManifestID) {
		return fmt.Errorf("%w: adapter_manifest_id", ErrInvalid)
	}
	if err := validateText("cursor_key", binding.CursorKey, 1, 256); err != nil {
		return err
	}
	return nil
}

func ValidateOperationBinding(operationID, planSHA256 string) error {
	if !operationPattern.MatchString(operationID) {
		return fmt.Errorf("%w: operation_id", ErrInvalid)
	}
	if !sha256Pattern.MatchString(planSHA256) {
		return fmt.Errorf("%w: plan_sha256", ErrInvalid)
	}
	return nil
}

func validatePrepareInput(input PrepareInput) error {
	if err := ValidateBinding(input.Binding); err != nil {
		return err
	}
	switch input.WorkerUnit {
	case CompletedExecutionWorkerUnit, LiteLLMWorkerUnit:
	default:
		return fmt.Errorf("%w: worker_unit", ErrInvalid)
	}
	profile := profileForWorker(input.WorkerUnit)
	if input.CursorKey != profile.cursorKey {
		return fmt.Errorf("%w: worker_unit does not own cursor_key", ErrInvalid)
	}
	if err := validateText("next_source_instance", input.NextSourceInstance, 1, 128); err != nil {
		return err
	}
	if err := validateText("next_cursor", input.NextCursor, 1, profile.maxBoundaryBytes); err != nil {
		return err
	}
	if err := validateText("next_high_watermark", input.NextHighWatermark, 1, profile.maxBoundaryBytes); err != nil {
		return err
	}
	if input.NextCursor != input.NextHighWatermark {
		return fmt.Errorf("%w: next cursor must equal the quiescent high-watermark", ErrInvalid)
	}
	if err := validateText("authorization_ref", input.AuthorizationRef, 1, 256); err != nil {
		return err
	}
	return nil
}

func validateText(name, value string, minimum, maximum int) error {
	if !utf8.ValidString(value) || strings.TrimSpace(value) != value ||
		len(value) < minimum || len(value) > maximum ||
		strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("%w: %s", ErrInvalid, name)
	}
	return nil
}

func profileForWorker(workerUnit string) workerProfile {
	switch workerUnit {
	case CompletedExecutionWorkerUnit:
		return workerProfile{
			cursorKey:        CompletedExecutionCursorKey,
			maxBoundaryBytes: completedExecutionBoundaryBytes,
		}
	case LiteLLMWorkerUnit:
		return workerProfile{
			cursorKey:        LiteLLMCursorKey,
			maxBoundaryBytes: liteLLMBoundaryBytes,
		}
	default:
		return workerProfile{}
	}
}

func hashText(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func hashJSON(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func normalizedTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}
