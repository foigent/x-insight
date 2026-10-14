package connector

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	SourceNeutralReceiptVersionV2 = "foigent.operational-receipt.v2"
	SourceNeutralErrorVersionV2   = "foigent.connector-error.v2"
	DeliveryModeBoundedOffline    = "bounded_offline"
	OperationImportBundle         = "import_bundle"
	OperationCommitBatch          = "commit_batch"
)

type BoundedOfflineDeliveryState struct {
	Mode         string `json:"mode"`
	BundleID     string `json:"bundle_id"`
	BundleSHA256 string `json:"bundle_sha256"`
}

type SourceNeutralConnectorError struct {
	ErrorVersion      string        `json:"error_version"`
	Category          ErrorCategory `json:"category"`
	Code              string        `json:"code"`
	Phase             string        `json:"phase"`
	Operation         string        `json:"operation"`
	Retryable         bool          `json:"retryable"`
	RetryAfterSeconds *int          `json:"retry_after_seconds,omitempty"`
	SafeMessage       string        `json:"safe_message"`
	Recovery          string        `json:"recovery"`
}

type SourceNeutralOperationalReceipt struct {
	ReceiptVersion   string                       `json:"receipt_version"`
	OperationID      string                       `json:"operation_id"`
	ConnectorID      string                       `json:"connector_id"`
	ProducerInstance string                       `json:"producer_instance"`
	DeliveryMode     string                       `json:"delivery_mode"`
	Operation        string                       `json:"operation"`
	Status           string                       `json:"status"`
	Counts           ReceiptCounts                `json:"counts"`
	DeliveryState    BoundedOfflineDeliveryState  `json:"delivery_state"`
	ObservedAt       string                       `json:"observed_at"`
	Error            *SourceNeutralConnectorError `json:"error,omitempty"`
}

type SourceNeutralOperationalReceiptInput struct {
	OperationID      string
	ProducerInstance string
	Operation        string
	Status           string
	Counts           ReceiptCounts
	BundleID         string
	BundleSHA256     string
	ObservedAt       time.Time
	Error            *SourceNeutralConnectorError
}

func ProjectBoundedOfflineOperationalReceipt(
	binding SourceNeutralManifestBinding,
	input SourceNeutralOperationalReceiptInput,
) (SourceNeutralOperationalReceipt, error) {
	if err := binding.Validate(); err != nil {
		return SourceNeutralOperationalReceipt{}, err
	}
	observedAt, err := formatMicrosecondTime(input.ObservedAt)
	if err != nil {
		return SourceNeutralOperationalReceipt{}, err
	}
	receipt := SourceNeutralOperationalReceipt{
		ReceiptVersion:   SourceNeutralReceiptVersionV2,
		OperationID:      input.OperationID,
		ConnectorID:      binding.Connector.ConnectorID,
		ProducerInstance: input.ProducerInstance,
		DeliveryMode:     DeliveryModeBoundedOffline,
		Operation:        input.Operation,
		Status:           input.Status,
		Counts:           input.Counts,
		DeliveryState: BoundedOfflineDeliveryState{
			Mode:         DeliveryModeBoundedOffline,
			BundleID:     input.BundleID,
			BundleSHA256: input.BundleSHA256,
		},
		ObservedAt: observedAt,
		Error:      input.Error,
	}
	if err := receipt.Validate(); err != nil {
		return SourceNeutralOperationalReceipt{}, err
	}
	return receipt, nil
}

func ParseSourceNeutralOperationalReceipt(
	payload []byte,
) (SourceNeutralOperationalReceipt, error) {
	if _, err := decodeStrictJSONObject(payload, "source-neutral operational receipt"); err != nil {
		return SourceNeutralOperationalReceipt{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var receipt SourceNeutralOperationalReceipt
	if err := decoder.Decode(&receipt); err != nil {
		return SourceNeutralOperationalReceipt{}, fmt.Errorf(
			"decode source-neutral operational receipt: %w",
			err,
		)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return SourceNeutralOperationalReceipt{}, errors.New(
			"source-neutral operational receipt contains trailing content",
		)
	}
	if err := receipt.Validate(); err != nil {
		return SourceNeutralOperationalReceipt{}, err
	}
	return receipt, nil
}

func (receipt SourceNeutralOperationalReceipt) Validate() error {
	if receipt.ReceiptVersion != SourceNeutralReceiptVersionV2 ||
		!validOpaqueText(receipt.OperationID, 1, 128) ||
		!connectorIDPattern.MatchString(receipt.ConnectorID) ||
		len(receipt.ConnectorID) > 160 ||
		!validOpaqueText(receipt.ProducerInstance, 1, 128) ||
		receipt.DeliveryMode != DeliveryModeBoundedOffline ||
		!oneOf(receipt.Operation, OperationImportBundle, OperationCommitBatch) ||
		!oneOf(receipt.Status, "accepted", "completed", "retryable_error", "permanent_error") ||
		!validMicrosecondTime(receipt.ObservedAt) {
		return errors.New("bounded-offline Operational Receipt identity is invalid")
	}
	if receipt.DeliveryState.Mode != DeliveryModeBoundedOffline ||
		!validOpaqueText(receipt.DeliveryState.BundleID, 1, 128) ||
		!digestPattern.MatchString(receipt.DeliveryState.BundleSHA256) {
		return errors.New("bounded-offline Operational Receipt delivery state is invalid")
	}
	counts := receipt.Counts
	if !validReceiptCount(counts.Received) ||
		!validReceiptCount(counts.Inserted) ||
		!validReceiptCount(counts.DuplicateNoop) ||
		!validReceiptCount(counts.Conflict) ||
		!validReceiptCount(counts.Rejected) {
		return errors.New("bounded-offline Operational Receipt counts are invalid")
	}
	outcomes := counts.Inserted + counts.DuplicateNoop + counts.Conflict + counts.Rejected
	switch receipt.Status {
	case "accepted":
		if receipt.Error != nil || outcomes != 0 {
			return errors.New("accepted bounded-offline Operational Receipt has outcomes or an error")
		}
	case "completed":
		if receipt.Error != nil ||
			counts.Conflict != 0 ||
			counts.Rejected != 0 ||
			outcomes != counts.Received {
			return errors.New("completed bounded-offline Operational Receipt did not fully converge")
		}
	case "retryable_error":
		if receipt.Error == nil || !receipt.Error.Retryable || outcomes != counts.Received {
			return errors.New("retryable bounded-offline Operational Receipt is inconsistent")
		}
	case "permanent_error":
		if receipt.Error == nil || receipt.Error.Retryable || outcomes != counts.Received {
			return errors.New("permanent bounded-offline Operational Receipt is inconsistent")
		}
	}
	if receipt.Error != nil {
		if err := receipt.Error.Validate(); err != nil {
			return err
		}
		if receipt.Error.Operation != receipt.Operation {
			return errors.New("bounded-offline Operational Receipt error operation is inconsistent")
		}
	}
	return nil
}

func (err SourceNeutralConnectorError) Validate() error {
	if err.ErrorVersion != SourceNeutralErrorVersionV2 ||
		!validErrorCategory(err.Category) ||
		!oneOf(err.Phase,
			"configure", "activate", "fetch", "receive", "import", "decode",
			"validate", "normalize", "commit", "acknowledge", "checkpoint",
			"status", "shutdown",
		) ||
		!oneOf(err.Recovery,
			"automatic_retry", "restart", "reauthorize", "operator_repair",
			"explicit_resync", "resubmit_request", "reimport_bundle", "none",
		) ||
		!errorCodePattern.MatchString(err.Code) ||
		len(err.Code) > 160 ||
		!errorCodePattern.MatchString(err.Operation) ||
		len(err.Operation) > 128 {
		return errors.New("source-neutral Connector Error identity is invalid")
	}
	if !validProtocolText(err.SafeMessage, 1, 512) {
		return errors.New("source-neutral Connector Error safe_message is invalid")
	}
	if err.RetryAfterSeconds != nil &&
		(!err.Retryable || *err.RetryAfterSeconds < 0 || *err.RetryAfterSeconds > 300) {
		return errors.New("source-neutral Connector Error retry_after_seconds is invalid")
	}
	return nil
}

func (receipt SourceNeutralOperationalReceipt) CanonicalBytes() ([]byte, error) {
	if err := receipt.Validate(); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(receipt)
	if err != nil {
		return nil, fmt.Errorf("marshal source-neutral Operational Receipt: %w", err)
	}
	value, err := decodeStrictJSONObject(payload, "source-neutral operational receipt")
	if err != nil {
		return nil, err
	}
	return canonicalJSON(value)
}

func (receipt SourceNeutralOperationalReceipt) SHA256() (string, error) {
	canonical, err := receipt.CanonicalBytes()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validReceiptCount(value int64) bool {
	return value >= 0 && value <= maxSafeJSONInteger
}
