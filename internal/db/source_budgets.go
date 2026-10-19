package db

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/foigent/x-insight/internal/connectorsync"
	"math/big"
	"slices"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

func budgetProfileJSON(p []string) string { b, _ := json.Marshal(p); return string(b) }

func (s *SourceFactsStore) LoadBudgetCollection(ctx context.Context, id string) (sourcefacts.BudgetCollection, error) {
	return loadBudgetCollection(ctx, s.db, id, false)
}

func loadBudgetCollection(ctx context.Context, q workspaceQueryer, id string, lock bool) (sourcefacts.BudgetCollection, error) {
	var r sourcefacts.BudgetCollection
	var configured, granted []byte
	query := `SELECT c.id::text,c.source_system_id::text,c.source_instance,c.org_id,c.project_id,c.scope_generation,
 array_to_json(c.profiles),c.status,c.revision,c.requires_attention,c.last_error,c.access_generation,c.catalog_revision,
 array_to_json(c.granted_profiles),COALESCE(p.catalog_bytes,''::bytea),c.catalog_cursor,c.entry_index,c.alerts_cursor,
 COALESCE(c.catalog_id::text,''),c.cycle,c.visible_cycle,c.transport
 FROM source_budget_collections c LEFT JOIN source_budget_catalogs p ON p.id=c.catalog_id WHERE c.id=$1::uuid`
	if lock {
		query += " FOR UPDATE OF c"
	}
	err := q.QueryRowContext(ctx, query, id).Scan(&r.ID, &r.SourceSystemID, &r.SourceInstance, &r.OrgID, &r.ProjectID, &r.ScopeGeneration, &configured, &r.Status, &r.Revision, &r.RequiresAttention, &r.LastError, &r.AccessGeneration, &r.CatalogRevision, &granted, &r.CatalogRaw, &r.CatalogCursor, &r.EntryIndex, &r.AlertsCursor, &r.CatalogID, &r.Cycle, &r.VisibleCycle, &r.Transport)
	if errors.Is(err, sql.ErrNoRows) {
		return r, sourcefacts.ErrUnavailable
	}
	if err != nil {
		return r, err
	}
	if json.Unmarshal(configured, &r.Profiles) != nil || json.Unmarshal(granted, &r.GrantedProfiles) != nil {
		return r, sourcefacts.ErrInvalid
	}
	return r, nil
}

func (s *SourceFactsStore) RegisterBudgetCollection(ctx context.Context, r sourcefacts.BudgetCollection) (sourcefacts.BudgetCollection, error) {
	if !sourcefacts.ValidBudgetCollection(r) || sourcefacts.BudgetTransport(r) != "live" {
		return r, sourcefacts.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	if syncErr := connectorsync.Fence(ctx, tx); syncErr != nil {
		return r, syncErr
	}
	if err = requireSourceAdministrator(ctx, tx); err != nil {
		return r, err
	}
	if err = sourceLock(ctx, tx, r.SourceSystemID); err != nil {
		return r, err
	}
	if err = ensureManagedSource(ctx, tx, r.SourceSystemID, r.SourceInstance); err != nil {
		return r, err
	}
	profiles := slices.Clone(r.Profiles)
	slices.Sort(profiles)
	result, err := tx.ExecContext(ctx, `INSERT INTO source_budget_collections(id,source_system_id,source_instance,org_id,project_id,scope_generation,profiles)
 VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,ARRAY(SELECT jsonb_array_elements_text($7::jsonb))) ON CONFLICT DO NOTHING`, r.ID, r.SourceSystemID, r.SourceInstance, r.OrgID, r.ProjectID, r.ScopeGeneration, budgetProfileJSON(profiles))
	if err != nil {
		return r, err
	}
	live, err := loadBudgetCollection(ctx, tx, r.ID, true)
	if err != nil || !sourcefacts.SameBudgetCollection(live, r) {
		return r, sourcefacts.ErrConflict
	}
	if n, _ := result.RowsAffected(); n == 1 {
		if err = sourceManagementEvent(ctx, tx, "budget", r.ID, "register", live.Status, live.Revision); err != nil {
			return r, err
		}
	}
	return live, tx.Commit()
}

func (s *SourceFactsStore) ControlBudgetCollection(ctx context.Context, id, status string, revision int64, acknowledge bool) (sourcefacts.BudgetCollection, error) {
	return s.controlBudgetCollection(ctx, id, status, revision, acknowledge, nil)
}

func (s *SourceFactsStore) controlBudgetCollection(ctx context.Context, id, status string, revision int64, acknowledge bool, offline *sourcefacts.BudgetOfflineAuthorization) (sourcefacts.BudgetCollection, error) {
	var zero sourcefacts.BudgetCollection
	if revision < 1 || !slices.Contains([]string{"active", "paused", "revoked"}, status) {
		return zero, sourcefacts.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	if syncErr := connectorsync.Fence(ctx, tx); syncErr != nil {
		return zero, syncErr
	}
	if err = requireSourceAdministrator(ctx, tx); err != nil {
		return zero, err
	}
	before, err := loadBudgetCollection(ctx, tx, id, false)
	if err != nil {
		return zero, err
	}
	if err = sourceLock(ctx, tx, before.SourceSystemID); err != nil {
		return zero, err
	}
	live, err := loadBudgetCollection(ctx, tx, id, true)
	if err != nil {
		return zero, err
	}
	if status == "active" && sourcefacts.BudgetTransport(live) == "bounded_offline" {
		if offline == nil || !sourcefacts.SameBudgetCollection(live, offline.Collection()) {
			return zero, sourcefacts.ErrAuthorization
		}
		if err = checkBudgetOfflineAuthority(ctx, tx, *offline); err != nil {
			return zero, err
		}
		var imported bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM source_budget_offline_imports WHERE collection_id=$1::uuid)`, id).Scan(&imported); err != nil {
			return zero, err
		}
		if imported && (live.Status != "active" || live.RequiresAttention) {
			return zero, sourcefacts.ErrUnavailable
		}
	}
	if live.Revision < revision || status == "active" && live.Revision != revision || live.Status == "revoked" && status != "revoked" {
		return zero, sourcefacts.ErrStreamChanged
	}
	if status == "active" && live.RequiresAttention && !acknowledge {
		return zero, sourcefacts.ErrUnavailable
	}
	if status != live.Status || status == "active" && live.RequiresAttention {
		_, err = tx.ExecContext(ctx, `UPDATE source_budget_collections SET status=$2,revision=revision+1,
 requires_attention=CASE WHEN $2='active' THEN false ELSE requires_attention END,
 last_error=CASE WHEN $2='active' THEN 'refresh_required' ELSE last_error END,
 catalog_id=NULL,catalog_cursor='',entry_index=0,alerts_cursor='',visible_cycle=0 WHERE id=$1::uuid`, id, status)
		if err != nil {
			return zero, err
		}
		action := "state_change"
		if acknowledge && live.RequiresAttention {
			action = "acknowledge"
		}
		if err = sourceManagementEvent(ctx, tx, "budget", id, action, status, live.Revision+1); err != nil {
			return zero, err
		}
	}
	live, err = loadBudgetCollection(ctx, tx, id, false)
	if err != nil {
		return zero, err
	}
	return live, tx.Commit()
}

func lockBudgetCollection(ctx context.Context, tx *sql.Tx, expected sourcefacts.BudgetCollection) (sourcefacts.BudgetCollection, error) {
	if !sourcefacts.ValidBudgetCollection(expected) {
		return expected, sourcefacts.ErrInvalid
	}
	if err := sourceLock(ctx, tx, expected.SourceSystemID); err != nil {
		return expected, err
	}
	live, err := loadBudgetCollection(ctx, tx, expected.ID, true)
	if err != nil {
		return live, err
	}
	if !sourcefacts.SameBudgetCollection(live, expected) || live.Revision != expected.Revision || live.Status != "active" || live.RequiresAttention {
		return live, sourcefacts.ErrStreamChanged
	}
	return live, nil
}

func budgetRevisionCompare(a, b string) int {
	if b == "" {
		return 1
	}
	x, ok := new(big.Int).SetString(a, 10)
	y, other := new(big.Int).SetString(b, 10)
	if !ok || !other {
		return -1
	}
	return x.Cmp(y)
}

func (s *SourceFactsStore) CommitBudgetCatalog(ctx context.Context, expected sourcefacts.BudgetCollection, raw []byte, now time.Time) error {
	if sourcefacts.BudgetTransport(expected) != "live" {
		return sourcefacts.ErrAuthorization
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if syncErr := connectorsync.CommitFence(ctx, tx); syncErr != nil {
		return syncErr
	}
	if err = commitBudgetCatalogTx(ctx, tx, expected, raw, now); err != nil {
		return err
	}
	return tx.Commit()
}

func commitBudgetCatalogTx(ctx context.Context, tx *sql.Tx, expected sourcefacts.BudgetCollection, raw []byte, now time.Time) error {
	c, err := sourcefacts.DecodeBudgetCatalog(raw)
	if err != nil {
		return err
	}
	live, err := lockBudgetCollection(ctx, tx, expected)
	if err != nil {
		return err
	}
	if live.CatalogID != "" {
		return sourcefacts.ErrStreamChanged
	}
	if c.SourceInstance != live.SourceInstance || c.OrgID != live.OrgID || c.ProjectID != live.ProjectID {
		return sourcefacts.ErrScopeChanged
	}
	for _, p := range c.Profiles {
		if !slices.Contains(live.Profiles, p) {
			return sourcefacts.ErrScopeChanged
		}
	}
	ac, cc := budgetRevisionCompare(c.AccessGeneration, live.AccessGeneration), budgetRevisionCompare(c.CatalogRevision, live.CatalogRevision)
	if ac < 0 || cc < 0 {
		return sourcefacts.ErrConflict
	}
	if ac == 0 && !sourcefacts.SameBudgetProfiles(c.Profiles, live.GrantedProfiles) {
		return sourcefacts.ErrConflict
	}
	if live.CatalogCursor != "" && (ac != 0 || cc != 0) {
		return sourcefacts.ErrScopeChanged
	}
	if c.HasMore && c.NextCursor == live.CatalogCursor {
		return sourcefacts.ErrInvalid
	}
	if live.CatalogCursor != "" && len(c.Entries) > 0 {
		var last string
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(budget_ref),'') FROM source_budget_catalog_entries WHERE collection_id=$1::uuid AND cycle=$2`, live.ID, live.Cycle).Scan(&last); err != nil {
			return err
		}
		if last >= c.Entries[0].BudgetRef {
			return sourcefacts.ErrConflict
		}
	}
	m, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		return sourcefacts.ErrInvalid
	}
	canonical, err := connector.CanonicalizeJSONValue(m)
	if err != nil {
		return sourcefacts.ErrInvalid
	}
	cycle := live.Cycle
	if live.CatalogCursor == "" {
		cycle++
	}
	var catalogID string
	err = tx.QueryRowContext(ctx, `INSERT INTO source_budget_catalogs(collection_id,cycle,input_cursor,catalog_bytes,catalog_sha256,received_at)
 VALUES($1::uuid,$2,$3,$4,$5,$6) RETURNING id::text`, live.ID, cycle, live.CatalogCursor, canonical, sourcefacts.Hash(canonical), now.UTC()).Scan(&catalogID)
	if err != nil {
		return err
	}
	for _, e := range c.Entries {
		_, err = tx.ExecContext(ctx, `INSERT INTO source_budget_catalog_entries(collection_id,cycle,budget_ref,catalog_id,origin,definition_revision,binding_revision) VALUES($1::uuid,$2,$3,$4::uuid,$5,$6,$7)`, live.ID, cycle, e.BudgetRef, catalogID, e.Origin, e.DefinitionRevision, e.BindingRevision)
		if err != nil {
			return err
		}
	}
	visible := live.VisibleCycle
	if ac != 0 || cc != 0 {
		visible = 0
	}
	cursor := live.CatalogCursor
	if len(c.Entries) == 0 {
		catalogID = ""
		cursor = c.NextCursor
		if !c.HasMore {
			visible = cycle
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE source_budget_collections SET revision=revision+1,access_generation=$2,catalog_revision=$3,
 granted_profiles=ARRAY(SELECT jsonb_array_elements_text($4::jsonb)),cycle=$5,visible_cycle=$6,catalog_id=NULLIF($7,'')::uuid,
 catalog_cursor=$8,entry_index=0,alerts_cursor='',last_error='',last_attempt_at=$9,
 last_success_at=CASE WHEN $10 THEN $9 ELSE last_success_at END WHERE id=$1::uuid`, live.ID, c.AccessGeneration, c.CatalogRevision, budgetProfileJSON(c.Profiles), cycle, visible, catalogID, cursor, now.UTC(), len(c.Entries) == 0 && !c.HasMore)
	if err != nil {
		return err
	}
	return nil
}

func (s *SourceFactsStore) CommitBudgetReading(ctx context.Context, expected sourcefacts.BudgetCollection, raw []byte, now time.Time) error {
	if sourcefacts.BudgetTransport(expected) != "live" {
		return sourcefacts.ErrAuthorization
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if syncErr := connectorsync.CommitFence(ctx, tx); syncErr != nil {
		return syncErr
	}
	if err = commitBudgetReadingTx(ctx, tx, expected, raw, now); err != nil {
		return err
	}
	return tx.Commit()
}

func commitBudgetReadingTx(ctx context.Context, tx *sql.Tx, expected sourcefacts.BudgetCollection, raw []byte, now time.Time) error {
	r, err := sourcefacts.DecodeBudgetReading(raw)
	if err != nil {
		return err
	}
	live, err := lockBudgetCollection(ctx, tx, expected)
	if err != nil {
		return err
	}
	c, err := sourcefacts.DecodeBudgetCatalog(live.CatalogRaw)
	if err != nil || live.EntryIndex < 0 || live.EntryIndex >= len(c.Entries) {
		return sourcefacts.ErrInvalid
	}
	if err = sourcefacts.MatchBudgetReading(r, c, c.Entries[live.EntryIndex], live.Profiles); err != nil {
		return err
	}
	next := sourcefacts.Text(r.Body(sourcefacts.BudgetAlertProfile), "next_cursor")
	if next != "" && next == live.AlertsCursor {
		return sourcefacts.ErrInvalid
	}
	var id string
	var previous []byte
	err = tx.QueryRowContext(ctx, `SELECT id::text,reading_bytes FROM source_budget_readings WHERE source_system_id=$1::uuid AND reading_id=$2`, live.SourceSystemID, r.ReadingID).Scan(&id, &previous)
	if err == nil {
		if !bytes.Equal(previous, r.Canonical) {
			return sourcefacts.ErrConflict
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `INSERT INTO source_budget_readings(source_system_id,source_instance,org_id,project_id,reading_id,budget_ref,reading_cutoff,observed_at,definition_revision,binding_revision,access_generation,catalog_revision,profiles,reading_bytes,reading_sha256,received_at)
 VALUES($1::uuid,$2,$3,$4,$5,$6,$7::timestamptz,$8::timestamptz,$9,$10,$11,$12,ARRAY(SELECT jsonb_array_elements_text($13::jsonb)),$14,$15,$16) RETURNING id::text`, live.SourceSystemID, r.SourceInstance, r.OrgID, r.ProjectID, r.ReadingID, r.BudgetRef, r.ReadingCutoff, r.ObservedAt, r.DefinitionRevision, r.BindingRevision, r.AccessGeneration, r.CatalogRevision, budgetProfileJSON(r.Profiles()), r.Canonical, r.SHA256, now.UTC()).Scan(&id)
		if err != nil {
			return err
		}
	} else {
		return err
	}
	var priorCursor string
	err = tx.QueryRowContext(ctx, `SELECT alerts_input_cursor FROM source_budget_receipts WHERE collection_id=$1::uuid AND source_reading_id=$2::uuid AND catalog_id=$3::uuid`, live.ID, id, live.CatalogID).Scan(&priorCursor)
	if err == nil && priorCursor != live.AlertsCursor {
		return sourcefacts.ErrConflict
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO source_budget_receipts(collection_id,source_reading_id,catalog_id,received_at,alerts_input_cursor) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5) ON CONFLICT DO NOTHING`, live.ID, id, live.CatalogID, now.UTC(), live.AlertsCursor)
	if err != nil {
		return err
	}
	index, catalogID, cursor, visible := live.EntryIndex, live.CatalogID, live.CatalogCursor, live.VisibleCycle
	complete := false
	if next == "" {
		index++
		if index == len(c.Entries) {
			index = 0
			catalogID = ""
			cursor = c.NextCursor
			if !c.HasMore {
				visible = live.Cycle
				complete = true
			}
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE source_budget_collections SET revision=revision+1,catalog_id=NULLIF($2,'')::uuid,
 catalog_cursor=$3,entry_index=$4,alerts_cursor=$5,visible_cycle=$6,last_error='',last_attempt_at=$7,
 last_success_at=CASE WHEN $8 THEN $7 ELSE last_success_at END WHERE id=$1::uuid`, live.ID, catalogID, cursor, index, next, visible, now.UTC(), complete)
	if err != nil {
		return err
	}
	return nil
}

func (s *SourceFactsStore) RecordBudgetFailure(ctx context.Context, expected sourcefacts.BudgetCollection, code string, now time.Time) error {
	restart := slices.Contains([]string{"budget_catalog_changed", "cursor_or_epoch_conflict", "cursor_expired"}, code)
	attention := sourcefacts.NeedsAttention(code) && !restart || slices.Contains([]string{"invalid_budget_catalog", "invalid_budget_position", "invalid_budget_reading"}, code)
	if !attention && !restart && code != "source_unavailable" {
		return sourcefacts.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if syncErr := connectorsync.Fence(ctx, tx); syncErr != nil {
		return syncErr
	}
	live, err := lockBudgetCollection(ctx, tx, expected)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE source_budget_collections SET revision=revision+1,last_error=$2,requires_attention=$3,last_attempt_at=$4,
 catalog_id=CASE WHEN $5 THEN NULL ELSE catalog_id END,catalog_cursor=CASE WHEN $5 THEN '' ELSE catalog_cursor END,
 entry_index=CASE WHEN $5 THEN 0 ELSE entry_index END,alerts_cursor=CASE WHEN $5 THEN '' ELSE alerts_cursor END,
 visible_cycle=CASE WHEN $5 OR $3 THEN 0 ELSE visible_cycle END WHERE id=$1::uuid`, live.ID, code, attention, now.UTC(), restart)
	if err != nil {
		return err
	}
	return tx.Commit()
}
