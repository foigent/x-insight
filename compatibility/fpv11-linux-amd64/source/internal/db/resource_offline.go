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

type syntheticResourceItem struct {
	Record workbench.ResourceAccessRecord
	Raw    []byte
}

func validateSyntheticResourceBatch(raw []byte, a sourcefacts.OfflineAuthorization, now time.Time) ([]syntheticResourceItem, error) {
	if a.SchemaVersion != sourcefacts.ResourceOfflineAuthorizationVersion || a.Validate(now) != nil || len(raw) >= sourcefacts.MaxPageBytes || sourcefacts.Hash(raw) != a.BundleSHA256 {
		return nil, sourcefacts.ErrAuthorization
	}
	object, err := connector.DecodeStrictJSONObject(raw)
	if err != nil || len(object) != 2 || object["schema_version"] != sourcefacts.ResourceOfflineBundleVersion {
		return nil, sourcefacts.ErrInvalid
	}
	records, ok := object["records"].([]any)
	if !ok || len(records) == 0 || len(records) > a.MaxRecords {
		return nil, sourcefacts.ErrInvalid
	}
	items := make([]syntheticResourceItem, 0, len(records))
	hashes := map[string]string{}
	previous := map[string]string{}
	for _, value := range records {
		encoded, err := connector.CanonicalizeJSONValue(value)
		if err != nil {
			return nil, sourcefacts.ErrInvalid
		}
		record, err := workbench.DecodeResourceAccessForImport(encoded)
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
		items = append(items, syntheticResourceItem{record, encoded})
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

func validateResourceDatabaseBinding(ctx context.Context, tx *sql.Tx, a sourcefacts.OfflineAuthorization) error {
	if a.SchemaVersion != sourcefacts.ResourceOfflineAuthorizationVersion {
		return sourcefacts.ErrAuthorization
	}
	var valid bool
	err := tx.QueryRowContext(ctx, `SELECT current_database()=$1 AND (SELECT oid::text FROM pg_database WHERE datname=current_database())=$2
 AND public.synthetic_resource_cluster_id()=$3
 AND COALESCE((SELECT split_part(max(version),'_',1)=lpad($4,6,'0') AND bool_and(NOT dirty AND checksum_sha256 IS NOT NULL) FROM schema_migrations),false)
 AND to_regclass('public.resource_access_records') IS NOT NULL`, a.TargetDatabaseName, a.TargetDatabaseOID, a.TargetDatabaseSystemID, a.TargetDatabaseSchema).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return sourcefacts.ErrAuthorization
	}
	return nil
}

func ensureResourceReporter(ctx context.Context, tx *sql.Tx, a sourcefacts.OfflineAuthorization) error {
	key, typ := resourceReporterIdentity(a)
	_, err := tx.ExecContext(ctx, `INSERT INTO source_systems(id,source_key,source_type,display_name,deployment_ref) VALUES($1::uuid,$2,$3,$4,$4) ON CONFLICT(id) DO NOTHING`, a.SourceSystemID, key, typ, a.SourceInstance)
	if err != nil {
		return err
	}
	return validateResourceIdentities(ctx, tx, a)
}
func validateResourceIdentities(ctx context.Context, tx *sql.Tx, a sourcefacts.OfflineAuthorization) error {
	key, typ := resourceReporterIdentity(a)
	var valid bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM source_systems WHERE id=$1::uuid AND source_key=$2 AND source_type=$3 AND deployment_ref=$4 AND status='active')
 AND EXISTS(SELECT 1 FROM source_systems WHERE id=$5::uuid AND status='active')`, a.SourceSystemID, key, typ, a.SourceInstance, a.TargetSourceSystemID).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return sourcefacts.ErrScopeChanged
	}
	return nil
}

func resourceReporterIdentity(a sourcefacts.OfflineAuthorization) (string, string) {
	if a.Profile == sourcefacts.ActionOfflineProfile {
		return "resource-action:" + a.SourceSystemID, sourcefacts.ActionOfflineSourceType
	}
	return "synthetic-resource:" + a.SourceSystemID, sourcefacts.ResourceOfflineSourceType
}

// CheckSyntheticResources is read-only: it never creates an operation binding.
// A successful check is a current observation, not a reservation for a later write.
func (s *SourceFactsStore) CheckSyntheticResources(ctx context.Context, a sourcefacts.OfflineAuthorization, raw []byte, now time.Time) (int, error) {
	items, err := validateSyntheticResourceBatch(raw, a, now)
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
	err = tx.QueryRowContext(ctx, `SELECT request_binding_sha256 FROM connector_import_operation_bindings WHERE connector_id=$1 AND operation_id=$2`, sourcefacts.ResourceOfflineConnectorID, a.OperationID).Scan(&digest)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if err == nil && digest != a.Digest() {
		return 0, boundedoffline.ErrOperationConflict
	}
	for _, item := range items {
		if _, err = storeResourceRecordTx(ctx, tx, item.Record, item.Raw, now, true); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return len(items), nil
}

// ImportSyntheticResources uses the existing importer role, immutable resource
// chain and neutral receipts. All input/authorization failures roll back the
// operation binding as well as resource records and receipt.
func (s *SourceFactsStore) ImportSyntheticResources(ctx context.Context, a sourcefacts.OfflineAuthorization, raw []byte, now time.Time) (connector.SourceNeutralOperationalReceipt, error) {
	var zero connector.SourceNeutralOperationalReceipt
	items, err := validateSyntheticResourceBatch(raw, a, now)
	if err != nil {
		return zero, err
	}
	store, err := boundedoffline.NewPostgresOperationStore(s.db)
	if err != nil {
		return zero, err
	}
	binding := boundedoffline.OperationBinding{ConnectorID: sourcefacts.ResourceOfflineConnectorID, OperationID: a.OperationID, RequestSHA256: a.Digest()}
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
			added, err := storeResourceRecordTx(ctx, tx, item.Record, item.Raw, received, false)
			if err != nil {
				return zero, err
			}
			if added {
				inserted++
			}
		}
		count := int64(len(items))
		receipt := connector.SourceNeutralOperationalReceipt{ReceiptVersion: connector.SourceNeutralReceiptVersionV2, OperationID: a.OperationID, ConnectorID: sourcefacts.ResourceOfflineConnectorID, ProducerInstance: a.SourceInstance, DeliveryMode: connector.DeliveryModeBoundedOffline, Operation: connector.OperationImportBundle, Status: "completed", Counts: connector.ReceiptCounts{Received: count, Inserted: inserted, DuplicateNoop: count - inserted}, DeliveryState: connector.BoundedOfflineDeliveryState{Mode: connector.DeliveryModeBoundedOffline, BundleID: a.BundleID, BundleSHA256: sourcefacts.Hash(raw)}, ObservedAt: received.UTC().Format(sourcefacts.TimeFormat)}
		return receipt, receipt.Validate()
	})
}
