package db

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/integration/boundedoffline"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

type actionResourceItem struct {
	Record workbench.ActionWitness
	Raw    []byte
}

func validateActionContentLinksTx(ctx context.Context, tx *sql.Tx, record workbench.ActionWitness) error {
	for _, link := range record.ContentLinks {
		var present bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM source_content_manifests m
 WHERE m.source_system_id=$1::uuid AND m.source_instance=$2 AND m.org_id=$3 AND m.project_id=$4
 AND m.request_id=$5 AND m.source_fact_id=$6 AND m.revision=$7::numeric AND m.source_sha256=$8
 AND m.source_json->>'content_ref'=$9)`, link.SourceSystemID, link.SourceInstance, link.OrgID, link.ProjectID,
			link.RequestID, link.SourceFactID, link.Revision, link.SourceSHA256, link.ContentRef).Scan(&present); err != nil {
			return err
		}
		if !present {
			return workbench.ErrNotFound
		}
	}
	return nil
}

func validateActionResourceBatch(raw []byte, a sourcefacts.OfflineAuthorization, now time.Time) ([]actionResourceItem, error) {
	if a.SchemaVersion != sourcefacts.ResourceOfflineAuthorizationVersion || a.Profile != sourcefacts.ActionOfflineProfile || a.Validate(now) != nil || len(raw) >= sourcefacts.MaxPageBytes || sourcefacts.Hash(raw) != a.BundleSHA256 {
		return nil, sourcefacts.ErrAuthorization
	}
	object, err := connector.DecodeStrictJSONObject(raw)
	if err != nil || len(object) != 2 || object["schema_version"] != sourcefacts.ActionOfflineBundleVersion {
		return nil, sourcefacts.ErrInvalid
	}
	records, ok := object["records"].([]any)
	if !ok || len(records) == 0 || len(records) > a.MaxRecords {
		return nil, sourcefacts.ErrInvalid
	}
	items := make([]actionResourceItem, 0, len(records))
	hashes := map[string]string{}
	previous := map[string]string{}
	for _, value := range records {
		encoded, err := connector.CanonicalizeJSONValue(value)
		if err != nil {
			return nil, sourcefacts.ErrInvalid
		}
		record, err := workbench.DecodeActionWitnessForImport(encoded)
		if err != nil {
			return nil, err
		}
		if record.Reporter != (workbench.ResourceScope{SourceSystemID: a.SourceSystemID, SourceInstance: a.SourceInstance, OrgID: a.OrgID, ProjectID: a.ProjectID}) || record.Target != (workbench.ResourceScope{SourceSystemID: a.TargetSourceSystemID, SourceInstance: a.TargetSourceInstance, OrgID: a.TargetOrgID, ProjectID: a.TargetProjectID}) {
			return nil, sourcefacts.ErrScopeChanged
		}
		key := record.RecordRef + "\x00" + strconv.FormatInt(record.Revision, 10)
		hash := sourcefacts.Hash(encoded)
		if h, exists := hashes[key]; exists && h != hash {
			return nil, workbench.ErrConflict
		}
		hashes[key] = hash
		previous[key] = record.PreviousHash
		items = append(items, actionResourceItem{record, encoded})
	}
	for _, item := range items {
		r := item.Record
		if h, ok := hashes[r.RecordRef+"\x00"+strconv.FormatInt(r.Revision-1, 10)]; ok && h != r.PreviousHash {
			return nil, workbench.ErrConflict
		}
		if h, ok := previous[r.RecordRef+"\x00"+strconv.FormatInt(r.Revision+1, 10)]; ok && h != sourcefacts.Hash(item.Raw) {
			return nil, workbench.ErrConflict
		}
	}
	return items, nil
}

// CheckResourceActions is read-only: it never creates an operation binding.
// A successful check is a current observation, not a reservation for a later write.
func (s *SourceFactsStore) CheckResourceActions(ctx context.Context, a sourcefacts.OfflineAuthorization, raw []byte, now time.Time) (int, error) {
	items, err := validateActionResourceBatch(raw, a, now)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err = validateSourceOfflineRegistration(ctx, tx, a, false); err != nil {
		return 0, err
	}
	if err = validateResourceIdentities(ctx, tx, a); err != nil {
		return 0, err
	}
	var digest string
	err = tx.QueryRowContext(ctx, `SELECT request_binding_sha256 FROM connector_import_operation_bindings WHERE connector_id=$1 AND operation_id=$2`, sourcefacts.ActionOfflineConnectorID, a.OperationID).Scan(&digest)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if err == nil && digest != a.Digest() {
		return 0, boundedoffline.ErrOperationConflict
	}
	for _, item := range items {
		if err = validateActionContentLinksTx(ctx, tx, item.Record); err != nil {
			return 0, err
		}
		if _, err = storeActionRecordTx(ctx, tx, item.Record, item.Raw, now, true); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return len(items), nil
}

// ImportResourceActions uses the existing importer role, immutable resource
// chain and neutral receipts. All input/authorization failures roll back the
// operation binding as well as resource records and receipt.
func (s *SourceFactsStore) ImportResourceActions(ctx context.Context, a sourcefacts.OfflineAuthorization, raw []byte, now time.Time) (connector.SourceNeutralOperationalReceipt, error) {
	var zero connector.SourceNeutralOperationalReceipt
	items, err := validateActionResourceBatch(raw, a, now)
	if err != nil {
		return zero, err
	}
	store, err := boundedoffline.NewPostgresOperationStore(s.db)
	if err != nil {
		return zero, err
	}
	binding := boundedoffline.OperationBinding{ConnectorID: sourcefacts.ActionOfflineConnectorID, OperationID: a.OperationID, RequestSHA256: a.Digest()}
	authorize := func(ctx context.Context, tx *sql.Tx) error {
		if err := authorizeSourceOffline(ctx, tx, a); err != nil {
			return err
		}
		return validateResourceIdentities(ctx, tx, a)
	}
	return store.WithAtomicOperationTransaction(ctx, binding, authorize, func(ctx context.Context, tx *sql.Tx, existing *connector.SourceNeutralOperationalReceipt) (connector.SourceNeutralOperationalReceipt, error) {
		if existing != nil {
			return *existing, nil
		}
		var received time.Time
		if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&received); err != nil {
			return zero, err
		}
		var inserted int64
		for _, item := range items {
			if err := validateActionContentLinksTx(ctx, tx, item.Record); err != nil {
				return zero, err
			}
			added, err := storeActionRecordTx(ctx, tx, item.Record, item.Raw, received, false)
			if err != nil {
				return zero, err
			}
			if added {
				inserted++
			}
		}
		count := int64(len(items))
		receipt := connector.SourceNeutralOperationalReceipt{ReceiptVersion: connector.SourceNeutralReceiptVersionV2, OperationID: a.OperationID, ConnectorID: sourcefacts.ActionOfflineConnectorID, ProducerInstance: a.SourceInstance, DeliveryMode: connector.DeliveryModeBoundedOffline, Operation: connector.OperationImportBundle, Status: "completed", Counts: connector.ReceiptCounts{Received: count, Inserted: inserted, DuplicateNoop: count - inserted}, DeliveryState: connector.BoundedOfflineDeliveryState{Mode: connector.DeliveryModeBoundedOffline, BundleID: a.BundleID, BundleSHA256: sourcefacts.Hash(raw)}, ObservedAt: received.UTC().Format(sourcefacts.TimeFormat)}
		return receipt, receipt.Validate()
	})
}
