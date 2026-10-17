package db

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

func validateBudgetSchema(ctx context.Context, q workspaceQueryer, target string) error {
	if target != "36" && target != "37" && target != "38" && target != "39" && target != "40" && target != "41" && target != "42" && target != "43" {
		return sourcefacts.ErrAuthorization
	}
	var valid bool
	err := q.QueryRowContext(ctx, `SELECT COALESCE(split_part(max(version),'_',1)=lpad($1,6,'0') AND bool_and(NOT dirty AND checksum_sha256 IS NOT NULL),false) FROM schema_migrations`, target).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return sourcefacts.ErrAuthorization
	}
	return nil
}
func (s *SourceFactsStore) RegisterBudgetOffline(ctx context.Context, a sourcefacts.BudgetOfflineAuthorization) (sourcefacts.BudgetCollection, error) {
	var zero sourcefacts.BudgetCollection
	if err := a.Validate(time.Now().UTC()); err != nil {
		return zero, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	if err = requireSourceAdministrator(ctx, tx); err != nil {
		return zero, err
	}
	if err = sourceLock(ctx, tx, a.SourceSystemID); err != nil {
		return zero, err
	}
	if err = ensureManagedSource(ctx, tx, a.SourceSystemID, a.SourceInstance); err != nil {
		return zero, err
	}
	if err = validateBudgetSchema(ctx, tx, a.TargetDatabaseSchema); err != nil {
		return zero, err
	}
	expected := a.Collection()
	res, err := tx.ExecContext(ctx, `INSERT INTO source_budget_collections(id,source_system_id,source_instance,org_id,project_id,scope_generation,transport,profiles)
 VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,'bounded_offline',ARRAY(SELECT jsonb_array_elements_text($7::jsonb))) ON CONFLICT DO NOTHING`, a.CollectionID, a.SourceSystemID, a.SourceInstance, a.OrgID, a.ProjectID, a.ScopeGeneration, budgetProfileJSON(a.Profiles))
	if err != nil {
		return zero, err
	}
	live, err := loadBudgetCollection(ctx, tx, a.CollectionID, true)
	if err != nil {
		return zero, err
	}
	if !sourcefacts.SameBudgetCollection(live, expected) {
		return zero, sourcefacts.ErrConflict
	}
	raw, _ := json.Marshal(a)
	if _, err = tx.ExecContext(ctx, `INSERT INTO source_budget_offline_authorizations(collection_id,authorization_bytes,authorization_sha256) VALUES($1::uuid,$2,$3) ON CONFLICT DO NOTHING`, a.CollectionID, raw, a.Digest()); err != nil {
		return zero, err
	}
	if err = checkBudgetOfflineAuthority(ctx, tx, a); err != nil {
		return zero, err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		if err = sourceManagementEvent(ctx, tx, "budget", a.CollectionID, "register", live.Status, live.Revision); err != nil {
			return zero, err
		}
	}
	return live, tx.Commit()
}
func checkBudgetOfflineAuthority(ctx context.Context, q workspaceQueryer, a sourcefacts.BudgetOfflineAuthorization) error {
	var current time.Time
	if err := q.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&current); err != nil {
		return err
	}
	if err := a.Validate(current.UTC()); err != nil {
		return err
	}
	var raw []byte
	var digest string
	if err := q.QueryRowContext(ctx, `SELECT authorization_bytes,authorization_sha256 FROM source_budget_offline_authorizations WHERE collection_id=$1::uuid`, a.CollectionID).Scan(&raw, &digest); err != nil {
		return sourcefacts.ErrAuthorization
	}
	expected, _ := json.Marshal(a)
	if !bytes.Equal(raw, expected) || digest != a.Digest() {
		return sourcefacts.ErrAuthorization
	}
	return validateBudgetSchema(ctx, q, a.TargetDatabaseSchema)
}
func (s *SourceFactsStore) ControlBudgetOffline(ctx context.Context, a sourcefacts.BudgetOfflineAuthorization, revision int64, acknowledge bool) (sourcefacts.BudgetCollection, error) {
	return s.controlBudgetCollection(ctx, a.CollectionID, "active", revision, acknowledge, &a)
}

type BudgetOfflineReceipt struct {
	CollectionID   string `json:"collection_id"`
	Transport      string `json:"transport"`
	BundleSHA256   string `json:"bundle_sha256"`
	SourceReadings int    `json:"source_readings"`
	Duplicate      bool   `json:"duplicate"`
}

func (s *SourceFactsStore) CheckBudgetOffline(ctx context.Context, a sourcefacts.BudgetOfflineAuthorization, raw []byte, now time.Time) error {
	bundle, err := sourcefacts.ValidateBudgetOfflineBundle(raw, a, now)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = checkBudgetOfflineAuthority(ctx, tx, a); err != nil {
		return err
	}
	live, err := loadBudgetCollection(ctx, tx, a.CollectionID, false)
	if err != nil || !sourcefacts.SameBudgetCollection(live, a.Collection()) || live.Status != "active" || live.RequiresAttention {
		return sourcefacts.ErrAuthorization
	}
	for _, v := range bundle.Readings {
		r, _ := sourcefacts.DecodeBudgetReading(v.Reading)
		var prior []byte
		err = tx.QueryRowContext(ctx, `SELECT reading_bytes FROM source_budget_readings WHERE source_system_id=$1::uuid AND reading_id=$2`, a.SourceSystemID, r.ReadingID).Scan(&prior)
		if err == nil && !bytes.Equal(prior, r.Canonical) {
			return sourcefacts.ErrConflict
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	return nil // Rollback read-only transaction; no sequence, receipt or progress write.
}
func (s *SourceFactsStore) ImportBudgetOffline(ctx context.Context, a sourcefacts.BudgetOfflineAuthorization, raw []byte, now time.Time) (BudgetOfflineReceipt, error) {
	var zero BudgetOfflineReceipt
	bundle, err := sourcefacts.ValidateBudgetOfflineBundle(raw, a, now)
	if err != nil {
		return zero, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	if err = sourceLock(ctx, tx, a.SourceSystemID); err != nil {
		return zero, err
	}
	if err = checkBudgetOfflineAuthority(ctx, tx, a); err != nil {
		return zero, err
	}
	live, err := loadBudgetCollection(ctx, tx, a.CollectionID, true)
	if err != nil || !sourcefacts.SameBudgetCollection(live, a.Collection()) || live.Status != "active" || live.RequiresAttention {
		return zero, sourcefacts.ErrAuthorization
	}
	receipt := BudgetOfflineReceipt{CollectionID: a.CollectionID, Transport: "bounded_offline", BundleSHA256: a.BundleSHA256, SourceReadings: len(bundle.Readings)}
	var prior []byte
	err = tx.QueryRowContext(ctx, `SELECT bundle_bytes FROM source_budget_offline_imports WHERE collection_id=$1::uuid`, a.CollectionID).Scan(&prior)
	if err == nil {
		if !bytes.Equal(prior, raw) {
			return zero, sourcefacts.ErrConflict
		}
		receipt.Duplicate = true
		return receipt, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return zero, err
	}
	// Capture receiving time after waiting for the source/authorization lock.
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return zero, err
	}
	if err = commitBudgetCatalogTx(ctx, tx, live, bundle.Catalog, now); err != nil {
		return zero, err
	}
	var catalogID string
	if err = tx.QueryRowContext(ctx, `SELECT id::text FROM source_budget_catalogs WHERE collection_id=$1::uuid ORDER BY cycle DESC LIMIT 1`, a.CollectionID).Scan(&catalogID); err != nil {
		return zero, err
	}
	for _, v := range bundle.Readings {
		live, err = loadBudgetCollection(ctx, tx, a.CollectionID, true)
		if err != nil {
			return zero, err
		}
		if live.AlertsCursor != v.AlertsInputCursor {
			return zero, sourcefacts.ErrConflict
		}
		if err = commitBudgetReadingTx(ctx, tx, live, v.Reading, now); err != nil {
			return zero, err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO source_budget_offline_imports(collection_id,catalog_id,bundle_bytes,bundle_sha256,source_reading_count,received_at) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6)`, a.CollectionID, catalogID, raw, a.BundleSHA256, len(bundle.Readings), now.UTC())
	if err != nil {
		return zero, err
	}
	return receipt, tx.Commit()
}
