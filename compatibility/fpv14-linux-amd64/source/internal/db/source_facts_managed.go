package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/integration/boundedoffline"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

func requireSourceAdministrator(ctx context.Context, tx *sql.Tx) error {
	var allowed bool
	err := tx.QueryRowContext(ctx, `SELECT pg_has_role(current_user,relowner,'USAGE') FROM pg_class WHERE oid='public.source_fact_streams'::regclass`).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return sourcefacts.ErrAuthorization
	}
	return nil
}
func sourceLock(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "source-facts:"+id)
	return err
}
func sourceManagementEvent(ctx context.Context, tx *sql.Tx, kind, id, action, status string, revision int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO source_fact_management_events(registration_kind,registration_id,revision,action,status,actor) VALUES($1,$2,$3,$4,$5,current_user)`, kind, id, revision, action, status)
	return err
}
func ensureManagedSource(ctx context.Context, tx *sql.Tx, id, instance string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO source_systems(id,source_key,source_type,display_name,deployment_ref) VALUES($1::uuid,$2,'unirouter',$3,$3) ON CONFLICT(id) DO NOTHING`, id, "source-facts:"+id, instance)
	if err != nil {
		return err
	}
	var kind string
	if err = tx.QueryRowContext(ctx, `SELECT source_type FROM source_systems WHERE id=$1::uuid`, id).Scan(&kind); err != nil {
		return err
	}
	if kind != "unirouter" {
		return sourcefacts.ErrConflict
	}
	return nil
}

func (s *SourceFactsStore) RegisterSourceFactsStream(ctx context.Context, r sourcefacts.Stream) (sourcefacts.Stream, error) {
	if !sourcefacts.ValidRegistration(r) {
		return sourcefacts.Stream{}, sourcefacts.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	if err = requireSourceAdministrator(ctx, tx); err != nil {
		return r, err
	}
	if err = sourceLock(ctx, tx, r.SourceSystemID); err != nil {
		return r, err
	}
	if err = ensureManagedSource(ctx, tx, r.SourceSystemID, r.SourceInstance); err != nil {
		return r, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO source_fact_streams(id,source_system_id,source_instance,org_id,project_id,profile,scope_generation) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, r.ID, r.SourceSystemID, r.SourceInstance, r.OrgID, r.ProjectID, r.Profile, r.ScopeGeneration)
	if err != nil {
		return r, err
	}
	live, err := loadSourceStream(ctx, tx, r.ID, true)
	if err != nil {
		return r, sourcefacts.ErrConflict
	}
	if live.SourceSystemID != r.SourceSystemID || live.SourceInstance != r.SourceInstance || live.OrgID != r.OrgID || live.ProjectID != r.ProjectID || live.Profile != r.Profile || live.ScopeGeneration != r.ScopeGeneration {
		return r, sourcefacts.ErrConflict
	}
	n, _ := result.RowsAffected()
	if n == 1 {
		if err = sourceManagementEvent(ctx, tx, "live", r.ID, "register", live.Status, live.Revision); err != nil {
			return r, err
		}
	}
	if err = tx.Commit(); err != nil {
		return r, err
	}
	return live, nil
}

func (s *SourceFactsStore) ControlSourceFactsStream(ctx context.Context, id, status string, revision int64, acknowledge bool) (sourcefacts.Stream, error) {
	var zero sourcefacts.Stream
	if revision < 1 || (status != "active" && status != "paused" && status != "revoked") {
		return zero, sourcefacts.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	if err = requireSourceAdministrator(ctx, tx); err != nil {
		return zero, err
	}
	before, err := loadSourceStream(ctx, tx, id, false)
	if err != nil {
		return zero, err
	}
	if err = sourceLock(ctx, tx, before.SourceSystemID); err != nil {
		return zero, err
	}
	live, err := loadSourceStream(ctx, tx, id, true)
	if err != nil {
		return zero, err
	}
	// Stop may wait behind worker progress. It still applies to this immutable
	// registration and reduces authority; activation must match the exact state
	// that the administrator inspected and preflighted.
	if live.Revision < revision || (status == "active" && live.Revision != revision) || (live.Status == "revoked" && status != "revoked") {
		return zero, sourcefacts.ErrStreamChanged
	}
	if status == "active" && live.RequiresAttention && !acknowledge {
		return zero, sourcefacts.ErrUnavailable
	}
	currentAttention := false
	if live.Profile == sourcefacts.RuntimeProfile {
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM source_runtime_snapshots WHERE stream_id=$1::uuid AND requires_attention)`, id).Scan(&currentAttention); err != nil {
			return zero, err
		}
		if status == "active" && currentAttention && !acknowledge {
			return zero, sourcefacts.ErrUnavailable
		}
		if status == "active" && currentAttention && acknowledge {
			if _, err = tx.ExecContext(ctx, `UPDATE source_runtime_snapshots SET revision=revision+1,generation=generation+1,page_ordinal=1,cursor_value='',visible_generation=NULL,requires_attention=false,last_error='refresh_required',last_success_at=NULL,last_attempt_at=clock_timestamp() WHERE stream_id=$1::uuid`, id); err != nil {
				return zero, err
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM source_runtime_snapshot_pages WHERE stream_id=$1::uuid`, id); err != nil {
				return zero, err
			}
		}
	}
	if live.Status != status || (status == "active" && (live.RequiresAttention || currentAttention)) {
		_, err = tx.ExecContext(ctx, `UPDATE source_fact_streams SET status=$2,revision=revision+1,requires_attention=CASE WHEN $2='active' THEN false ELSE requires_attention END,last_error=CASE WHEN $2='active' THEN '' ELSE last_error END,updated_at=clock_timestamp() WHERE id=$1::uuid`, id, status)
		if err != nil {
			return zero, err
		}
		action := "state_change"
		if acknowledge && (live.RequiresAttention || currentAttention) {
			action = "acknowledge"
		}
		if err = sourceManagementEvent(ctx, tx, "live", id, action, status, live.Revision+1); err != nil {
			return zero, err
		}
	}
	live, err = loadSourceStream(ctx, tx, id, false)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(); err != nil {
		return zero, err
	}
	return live, nil
}

type SourceOfflineRegistration struct {
	ID                  string    `json:"id"`
	Status              string    `json:"status"`
	Revision            int64     `json:"revision"`
	AuthorizationSHA256 string    `json:"authorization_sha256"`
	ExpiresAt           time.Time `json:"expires_at"`
}

func loadOfflineRegistration(ctx context.Context, q workspaceQueryer, id string, lock bool) (SourceOfflineRegistration, error) {
	var r SourceOfflineRegistration
	query := `SELECT id,status,revision,authorization_sha256,expires_at FROM source_fact_offline_authorizations WHERE id=$1`
	if lock {
		query += " FOR UPDATE"
	}
	err := q.QueryRowContext(ctx, query, id).Scan(&r.ID, &r.Status, &r.Revision, &r.AuthorizationSHA256, &r.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, sourcefacts.ErrAuthorization
	}
	return r, err
}
func (s *SourceFactsStore) LoadSourceOfflineRegistration(ctx context.Context, id string) (SourceOfflineRegistration, error) {
	return loadOfflineRegistration(ctx, s.db, id, false)
}

func (s *SourceFactsStore) RegisterSourceOfflineAuthorization(ctx context.Context, a sourcefacts.OfflineAuthorization) (SourceOfflineRegistration, error) {
	var zero SourceOfflineRegistration
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	if err = requireSourceAdministrator(ctx, tx); err != nil {
		return zero, err
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return zero, err
	}
	if err = a.Validate(now); err != nil {
		return zero, err
	}
	if err = validateOfflineTargetSchema(ctx, tx, a); err != nil {
		return zero, err
	}
	if err = sourceLock(ctx, tx, a.SourceSystemID); err != nil {
		return zero, err
	}
	if a.SchemaVersion == sourcefacts.ResourceOfflineAuthorizationVersion {
		err = ensureResourceReporter(ctx, tx, a)
	} else {
		err = ensureManagedSource(ctx, tx, a.SourceSystemID, a.SourceInstance)
	}
	if err != nil {
		return zero, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return zero, err
	}
	if err = a.Validate(now); err != nil {
		return zero, err
	}
	if err = validateOfflineTargetSchema(ctx, tx, a); err != nil {
		return zero, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO source_fact_offline_authorizations(id,source_system_id,source_instance,org_id,project_id,profile,authorization_sha256,target_ref,product_version,product_commit,not_before,expires_at) VALUES($1,$2::uuid,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(id) DO NOTHING`, a.AuthorizationID, a.SourceSystemID, a.SourceInstance, a.OrgID, a.ProjectID, a.Profile, a.Digest(), a.TargetRef, a.ProductVersion, a.ProductCommit, a.NotBefore, a.ExpiresAt)
	if err != nil {
		return zero, err
	}
	row, err := loadOfflineRegistration(ctx, tx, a.AuthorizationID, true)
	if err != nil {
		return zero, err
	}
	if row.AuthorizationSHA256 != a.Digest() {
		return zero, sourcefacts.ErrConflict
	}
	n, _ := result.RowsAffected()
	if n == 1 {
		if err = sourceManagementEvent(ctx, tx, "offline", row.ID, "register", row.Status, row.Revision); err != nil {
			return zero, err
		}
	}
	if err = tx.Commit(); err != nil {
		return zero, err
	}
	return row, nil
}
func (s *SourceFactsStore) ControlSourceOfflineAuthorization(ctx context.Context, id, status string, revision int64, authorizationDigest string, bindings ...sourcefacts.OfflineAuthorization) (SourceOfflineRegistration, error) {
	var zero SourceOfflineRegistration
	if revision < 1 || (status != "active" && status != "disabled" && status != "revoked") {
		return zero, sourcefacts.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	if err = requireSourceAdministrator(ctx, tx); err != nil {
		return zero, err
	}
	row, err := loadOfflineRegistration(ctx, tx, id, true)
	if err != nil {
		return zero, err
	}
	if row.Revision != revision || (row.Status == "revoked" && status != "revoked") {
		return zero, sourcefacts.ErrStreamChanged
	}
	if status == "active" {
		var profile string
		if err = tx.QueryRowContext(ctx, `SELECT profile FROM source_fact_offline_authorizations WHERE id=$1`, id).Scan(&profile); err != nil {
			return zero, err
		}
		if profile == sourcefacts.ResourceOfflineProfile || profile == sourcefacts.ActionOfflineProfile {
			if len(bindings) != 1 || bindings[0].AuthorizationID != id || bindings[0].Digest() != authorizationDigest {
				return zero, sourcefacts.ErrAuthorization
			}
			if err = validateResourceDatabaseBinding(ctx, tx, bindings[0]); err != nil {
				return zero, err
			}
			if err = validateResourceIdentities(ctx, tx, bindings[0]); err != nil {
				return zero, err
			}
		}
		var valid bool
		err = tx.QueryRowContext(ctx, `SELECT authorization_sha256=$2 AND clock_timestamp()>=not_before AND clock_timestamp()<expires_at FROM source_fact_offline_authorizations WHERE id=$1`, id, authorizationDigest).Scan(&valid)
		if err != nil {
			return zero, err
		}
		if !valid {
			return zero, sourcefacts.ErrAuthorization
		}
	}
	if row.Status != status {
		if _, err = tx.ExecContext(ctx, `UPDATE source_fact_offline_authorizations SET status=$2,revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, id, status); err != nil {
			return zero, err
		}
		if err = sourceManagementEvent(ctx, tx, "offline", id, "state_change", status, revision+1); err != nil {
			return zero, err
		}
	}
	row, err = loadOfflineRegistration(ctx, tx, id, false)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(); err != nil {
		return zero, err
	}
	return row, nil
}

func authorizeSourceOffline(ctx context.Context, tx *sql.Tx, a sourcefacts.OfflineAuthorization) error {
	if err := sourceLock(ctx, tx, a.SourceSystemID); err != nil {
		return err
	}
	return validateSourceOfflineRegistration(ctx, tx, a, true)
}

func validateSourceOfflineRegistration(ctx context.Context, tx *sql.Tx, a sourcefacts.OfflineAuthorization, lock bool) error {
	var status, digest string
	query := `SELECT status,authorization_sha256 FROM source_fact_offline_authorizations WHERE id=$1`
	if lock {
		query += " FOR SHARE"
	}
	err := tx.QueryRowContext(ctx, query, a.AuthorizationID).Scan(&status, &digest)
	if errors.Is(err, sql.ErrNoRows) {
		return sourcefacts.ErrAuthorization
	}
	if err != nil {
		return err
	}
	if status != "active" || digest != a.Digest() {
		return sourcefacts.ErrAuthorization
	}
	// A SELECT target expression can be evaluated before its row lock is acquired.
	// Read the clock separately, after every authorization lock has been obtained.
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	if err := a.Validate(now); err != nil {
		return err
	}
	return validateOfflineTargetSchema(ctx, tx, a)
}

// A v1 retains its original compatibility. B v2 pins the database generation;
// registration, check, reservation and completion each verify the live ledger.
func validateOfflineTargetSchema(ctx context.Context, tx *sql.Tx, a sourcefacts.OfflineAuthorization) error {
	if a.SchemaVersion == sourcefacts.ResourceOfflineAuthorizationVersion {
		return validateResourceDatabaseBinding(ctx, tx, a)
	}
	if a.SchemaVersion != sourcefacts.ObservationOfflineAuthorizationVersion {
		return nil
	}
	var valid bool
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(split_part(max(version),'_',1)=lpad($1,6,'0') AND bool_and(NOT dirty AND checksum_sha256 IS NOT NULL),false)
 AND to_regclass('public.source_observation_documents') IS NOT NULL FROM schema_migrations`, a.TargetDatabaseSchema).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return sourcefacts.ErrAuthorization
	}
	return nil
}

// CheckSourceFactsOffline validates the exact input and current authorization
// without reserving an operation or obtaining permission for a later import.
// Read-only mode also prevents accidental writes added to this path in future.
func (s *SourceFactsStore) CheckSourceFactsOffline(ctx context.Context, a sourcefacts.OfflineAuthorization, raw []byte, now time.Time) error {
	if _, err := sourcefacts.ValidateOfflineBatch(raw, a, now); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var digest string
	err = tx.QueryRowContext(ctx, `SELECT request_binding_sha256 FROM connector_import_operation_bindings WHERE connector_id=$1 AND operation_id=$2`, sourcefacts.ConnectorID, a.OperationID).Scan(&digest)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && digest != a.Digest() {
		return boundedoffline.ErrOperationConflict
	}
	if err := validateSourceOfflineRegistration(ctx, tx, a, false); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SourceFactsStore) ImportSourceFactsOffline(ctx context.Context, a sourcefacts.OfflineAuthorization, raw []byte, now time.Time) (connector.SourceNeutralOperationalReceipt, error) {
	var zero connector.SourceNeutralOperationalReceipt
	batch, err := sourcefacts.ValidateOfflineBatch(raw, a, now)
	if err != nil {
		return zero, err
	}
	store, err := boundedoffline.NewPostgresOperationStore(s.db)
	if err != nil {
		return zero, err
	}
	binding := boundedoffline.OperationBinding{ConnectorID: sourcefacts.ConnectorID, OperationID: a.OperationID, RequestSHA256: a.Digest()}
	return store.WithOperationTransaction(ctx, binding, func(ctx context.Context, tx *sql.Tx) error { return authorizeSourceOffline(ctx, tx, a) }, func(ctx context.Context, tx *sql.Tx, existing *connector.SourceNeutralOperationalReceipt) (connector.SourceNeutralOperationalReceipt, error) {
		if existing != nil {
			return *existing, nil
		}
		if err := sourceLock(ctx, tx, a.SourceSystemID); err != nil {
			return zero, err
		}
		// The shared source lock may have waited beyond the authorization deadline.
		if err := authorizeSourceOffline(ctx, tx, a); err != nil {
			return zero, err
		}
		var inserted int64
		var err error
		if a.SchemaVersion == sourcefacts.ObservationOfflineAuthorizationVersion {
			inserted, err = insertSourceObservations(ctx, tx, a.SourceSystemID, batch.Observations)
		} else {
			inserted, err = insertSourceFactItems(ctx, tx, a.SourceSystemID, batch.Items)
		}
		if err != nil {
			return zero, err
		}
		var observed time.Time
		if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&observed); err != nil {
			return zero, err
		}
		count := int64(batch.Count())
		receipt := connector.SourceNeutralOperationalReceipt{ReceiptVersion: connector.SourceNeutralReceiptVersionV2, OperationID: a.OperationID, ConnectorID: sourcefacts.ConnectorID, ProducerInstance: a.SourceInstance, DeliveryMode: connector.DeliveryModeBoundedOffline, Operation: connector.OperationImportBundle, Status: "completed", Counts: connector.ReceiptCounts{Received: count, Inserted: inserted, DuplicateNoop: count - inserted}, DeliveryState: connector.BoundedOfflineDeliveryState{Mode: connector.DeliveryModeBoundedOffline, BundleID: a.BundleID, BundleSHA256: batch.SHA256}, ObservedAt: observed.UTC().Format(sourcefacts.TimeFormat)}
		return receipt, receipt.Validate()
	})
}
