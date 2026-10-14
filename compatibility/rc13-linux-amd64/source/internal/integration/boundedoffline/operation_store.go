package boundedoffline

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/foigent/x-insight/internal/connector"
)

const receiptTimeLayout = "2006-01-02T15:04:05.000000Z"

var (
	connectorIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_-]*)+$`)

	ErrOperationConflict = errors.New(
		"bounded-offline import operation conflicts with its durable request binding or completed receipt",
	)
)

// OperationBinding claims one immutable product-local operation identity. The
// request digest is deliberately opaque to this store: each bounded-offline
// consumer owns the canonical inputs that contribute to it.
type OperationBinding struct {
	ConnectorID   string
	OperationID   string
	RequestSHA256 string
}

// PostgresOperationStore persists connector-neutral bounded-offline operation
// bindings and exact completed receipts in the existing schema-20 tables.
type PostgresOperationStore struct {
	db *sql.DB
}

func NewPostgresOperationStore(database *sql.DB) (*PostgresOperationStore, error) {
	if database == nil {
		return nil, errors.New("bounded-offline operational database is required")
	}
	return &PostgresOperationStore{db: database}, nil
}

func (store *PostgresOperationStore) WithOperation(
	ctx context.Context,
	binding OperationBinding,
	operation func(
		context.Context,
		*connector.SourceNeutralOperationalReceipt,
	) (connector.SourceNeutralOperationalReceipt, error),
) (connector.SourceNeutralOperationalReceipt, error) {
	if store == nil || store.db == nil || operation == nil ||
		!validConnectorID(binding.ConnectorID) ||
		!validOperationalText(binding.OperationID, 128) ||
		!validSHA256Ref(binding.RequestSHA256) {
		return connector.SourceNeutralOperationalReceipt{}, errors.New(
			"bounded-offline receipt operation is invalid",
		)
	}
	if err := store.ensureOperationBinding(ctx, binding); err != nil {
		return connector.SourceNeutralOperationalReceipt{}, err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return connector.SourceNeutralOperationalReceipt{}, fmt.Errorf(
			"begin bounded-offline receipt transaction: %w",
			err,
		)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(
		ctx,
		`SELECT pg_advisory_xact_lock($1)`,
		receiptLockKey(binding.ConnectorID, binding.OperationID),
	); err != nil {
		return connector.SourceNeutralOperationalReceipt{}, fmt.Errorf(
			"lock bounded-offline receipt operation: %w",
			err,
		)
	}
	if err := requireOperationBinding(ctx, tx, binding); err != nil {
		return connector.SourceNeutralOperationalReceipt{}, err
	}
	existing, found, err := loadCompletedReceipt(ctx, tx, binding)
	if err != nil {
		return connector.SourceNeutralOperationalReceipt{}, err
	}
	var existingPointer *connector.SourceNeutralOperationalReceipt
	if found {
		existingPointer = &existing
	}
	result, err := operation(ctx, existingPointer)
	if err != nil {
		return connector.SourceNeutralOperationalReceipt{}, err
	}
	if err := validateStoredReceiptIdentity(
		result,
		binding.ConnectorID,
		binding.OperationID,
	); err != nil {
		return connector.SourceNeutralOperationalReceipt{}, err
	}
	resultBytes, err := result.CanonicalBytes()
	if err != nil {
		return connector.SourceNeutralOperationalReceipt{}, err
	}
	if found {
		existingBytes, err := existing.CanonicalBytes()
		if err != nil {
			return connector.SourceNeutralOperationalReceipt{}, err
		}
		if !bytes.Equal(resultBytes, existingBytes) {
			return connector.SourceNeutralOperationalReceipt{}, ErrOperationConflict
		}
	} else if err := insertCompletedReceipt(
		ctx,
		tx,
		binding,
		result,
		resultBytes,
	); err != nil {
		return connector.SourceNeutralOperationalReceipt{}, err
	}
	if err := tx.Commit(); err != nil {
		return connector.SourceNeutralOperationalReceipt{}, fmt.Errorf(
			"commit bounded-offline receipt transaction: %w",
			err,
		)
	}
	return result, nil
}

func (store *PostgresOperationStore) ensureOperationBinding(
	ctx context.Context,
	binding OperationBinding,
) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin bounded-offline operation-binding transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(
		ctx,
		`SELECT pg_advisory_xact_lock($1)`,
		receiptLockKey(binding.ConnectorID, binding.OperationID),
	); err != nil {
		return fmt.Errorf("lock bounded-offline operation binding: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO connector_import_operation_bindings (
  connector_id,
  operation_id,
  request_binding_sha256
) VALUES ($1, $2, $3)
ON CONFLICT (connector_id, operation_id) DO NOTHING`,
		binding.ConnectorID,
		binding.OperationID,
		binding.RequestSHA256,
	); err != nil {
		return fmt.Errorf("persist bounded-offline operation binding: %w", err)
	}
	if err := requireOperationBinding(ctx, tx, binding); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit bounded-offline operation binding: %w", err)
	}
	return nil
}

func requireOperationBinding(
	ctx context.Context,
	tx *sql.Tx,
	binding OperationBinding,
) error {
	var requestBinding string
	if err := tx.QueryRowContext(ctx, `
SELECT request_binding_sha256
FROM connector_import_operation_bindings
WHERE connector_id = $1 AND operation_id = $2`,
		binding.ConnectorID,
		binding.OperationID,
	).Scan(&requestBinding); err != nil {
		return fmt.Errorf("load bounded-offline operation binding: %w", err)
	}
	if requestBinding != binding.RequestSHA256 {
		return ErrOperationConflict
	}
	return nil
}

func loadCompletedReceipt(
	ctx context.Context,
	tx *sql.Tx,
	binding OperationBinding,
) (connector.SourceNeutralOperationalReceipt, bool, error) {
	var (
		receiptVersion   string
		storedOperation  string
		storedConnector  string
		requestBinding   string
		producerInstance string
		deliveryMode     string
		operation        string
		status           string
		received         int64
		inserted         int64
		duplicateNoop    int64
		conflict         int64
		rejected         int64
		deliveryState    string
		observedAt       time.Time
		receiptSHA256    string
		canonicalReceipt []byte
	)
	err := tx.QueryRowContext(ctx, `
SELECT
  receipt_version,
  operation_id,
  connector_id,
  request_binding_sha256,
  producer_instance,
  delivery_mode,
  operation,
  status,
  received,
  inserted,
  duplicate_noop,
  conflict,
  rejected,
  delivery_state::text,
  observed_at,
  receipt_sha256,
  canonical_receipt
FROM connector_operational_receipts
WHERE connector_id = $1 AND operation_id = $2`,
		binding.ConnectorID,
		binding.OperationID,
	).Scan(
		&receiptVersion,
		&storedOperation,
		&storedConnector,
		&requestBinding,
		&producerInstance,
		&deliveryMode,
		&operation,
		&status,
		&received,
		&inserted,
		&duplicateNoop,
		&conflict,
		&rejected,
		&deliveryState,
		&observedAt,
		&receiptSHA256,
		&canonicalReceipt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return connector.SourceNeutralOperationalReceipt{}, false, nil
	}
	if err != nil {
		return connector.SourceNeutralOperationalReceipt{}, false, fmt.Errorf(
			"load bounded-offline receipt: %w",
			err,
		)
	}
	if requestBinding != binding.RequestSHA256 {
		return connector.SourceNeutralOperationalReceipt{}, false, ErrOperationConflict
	}
	receipt, err := connector.ParseSourceNeutralOperationalReceipt(canonicalReceipt)
	if err != nil {
		return connector.SourceNeutralOperationalReceipt{}, false, fmt.Errorf(
			"validate stored bounded-offline receipt: %w",
			err,
		)
	}
	computedCanonical, err := receipt.CanonicalBytes()
	if err != nil {
		return connector.SourceNeutralOperationalReceipt{}, false, err
	}
	computedSHA256, err := receipt.SHA256()
	if err != nil {
		return connector.SourceNeutralOperationalReceipt{}, false, err
	}
	var storedDeliveryState connector.BoundedOfflineDeliveryState
	if err := json.Unmarshal([]byte(deliveryState), &storedDeliveryState); err != nil {
		return connector.SourceNeutralOperationalReceipt{}, false, fmt.Errorf(
			"decode stored bounded-offline delivery state: %w",
			err,
		)
	}
	receiptObservedAt, err := time.Parse(receiptTimeLayout, receipt.ObservedAt)
	if err != nil ||
		receiptVersion != receipt.ReceiptVersion ||
		storedOperation != receipt.OperationID ||
		storedConnector != receipt.ConnectorID ||
		producerInstance != receipt.ProducerInstance ||
		deliveryMode != receipt.DeliveryMode ||
		operation != receipt.Operation ||
		status != receipt.Status ||
		received != receipt.Counts.Received ||
		inserted != receipt.Counts.Inserted ||
		duplicateNoop != receipt.Counts.DuplicateNoop ||
		conflict != receipt.Counts.Conflict ||
		rejected != receipt.Counts.Rejected ||
		storedDeliveryState != receipt.DeliveryState ||
		!observedAt.UTC().Equal(receiptObservedAt) ||
		receiptSHA256 != computedSHA256 ||
		!bytes.Equal(canonicalReceipt, computedCanonical) ||
		receipt.Error != nil {
		return connector.SourceNeutralOperationalReceipt{}, false, errors.New(
			"stored bounded-offline receipt columns conflict with canonical bytes",
		)
	}
	return receipt, true, nil
}

func insertCompletedReceipt(
	ctx context.Context,
	tx *sql.Tx,
	binding OperationBinding,
	receipt connector.SourceNeutralOperationalReceipt,
	canonicalReceipt []byte,
) error {
	deliveryState, err := json.Marshal(receipt.DeliveryState)
	if err != nil {
		return fmt.Errorf("encode bounded-offline receipt delivery state: %w", err)
	}
	receiptSHA256, err := receipt.SHA256()
	if err != nil {
		return err
	}
	observedAt, err := time.Parse(receiptTimeLayout, receipt.ObservedAt)
	if err != nil {
		return fmt.Errorf("parse bounded-offline receipt time: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
INSERT INTO connector_operational_receipts (
  receipt_version,
  operation_id,
  connector_id,
  request_binding_sha256,
  producer_instance,
  delivery_mode,
  operation,
  status,
  received,
  inserted,
  duplicate_noop,
  conflict,
  rejected,
  delivery_state,
  observed_at,
  receipt_sha256,
  canonical_receipt
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8,
  $9, $10, $11, $12, $13,
  $14::jsonb, $15, $16, $17
)`,
		receipt.ReceiptVersion,
		receipt.OperationID,
		receipt.ConnectorID,
		binding.RequestSHA256,
		receipt.ProducerInstance,
		receipt.DeliveryMode,
		receipt.Operation,
		receipt.Status,
		receipt.Counts.Received,
		receipt.Counts.Inserted,
		receipt.Counts.DuplicateNoop,
		receipt.Counts.Conflict,
		receipt.Counts.Rejected,
		string(deliveryState),
		observedAt,
		receiptSHA256,
		canonicalReceipt,
	)
	if err != nil {
		return fmt.Errorf("persist bounded-offline receipt: %w", err)
	}
	return requireOneRow(result, "bounded-offline completed receipt")
}

func validateStoredReceiptIdentity(
	receipt connector.SourceNeutralOperationalReceipt,
	connectorID string,
	operationID string,
) error {
	if err := receipt.Validate(); err != nil ||
		receipt.ConnectorID != connectorID ||
		receipt.OperationID != operationID ||
		receipt.DeliveryMode != connector.DeliveryModeBoundedOffline ||
		receipt.Status != "completed" ||
		receipt.Error != nil {
		return errors.New("only exact completed bounded-offline receipts are durable")
	}
	return nil
}

func receiptLockKey(connectorID string, operationID string) int64 {
	digest := sha256.Sum256([]byte(fmt.Sprintf(
		"xinsight.connector-receipt.v1\x00%d:%s%d:%s",
		len(connectorID),
		connectorID,
		len(operationID),
		operationID,
	)))
	return int64(binary.BigEndian.Uint64(digest[:8]))
}

func requireOneRow(result sql.Result, label string) error {
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read %s result: %w", label, err)
	}
	if rowsAffected != 1 {
		return fmt.Errorf("%s conflicts with an existing immutable identity", label)
	}
	return nil
}

func validConnectorID(value string) bool {
	return len(value) <= 160 && connectorIDPattern.MatchString(value)
}

func validOperationalText(value string, maximum int) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 1 ||
		utf8.RuneCountInString(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func validSHA256Ref(value string) bool {
	if len(value) != len("sha256:")+sha256.Size*2 ||
		value[:len("sha256:")] != "sha256:" {
		return false
	}
	decoded, err := hex.DecodeString(value[len("sha256:"):])
	return err == nil && len(decoded) == sha256.Size &&
		value == "sha256:"+hex.EncodeToString(decoded)
}
