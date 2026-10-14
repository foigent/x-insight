package db

import (
	"context"
	"database/sql"
	"fmt"
)

func validateManagedSourceFactsRestore(ctx context.Context, q workspaceQueryer) error {
	var present bool
	if err := q.QueryRowContext(ctx, `SELECT to_regclass('public.source_fact_offline_authorizations') IS NOT NULL`).Scan(&present); err != nil {
		return err
	}
	if !present {
		return nil
	}
	var valid bool
	err := q.QueryRowContext(ctx, `SELECT
 (SELECT count(*)=3 FROM pg_trigger WHERE tgenabled='A' AND NOT tgisinternal AND
  (tgrelid,tgname) IN (('public.source_fact_streams'::regclass,'source_fact_stream_management'),
   ('public.source_fact_offline_authorizations'::regclass,'source_fact_offline_authorization_identity'),
   ('public.source_fact_management_events'::regclass,'source_fact_management_events_immutable')))
 AND NOT EXISTS(SELECT 1 FROM source_fact_offline_authorizations a WHERE NOT EXISTS(
  SELECT 1 FROM source_fact_management_events e WHERE e.registration_kind='offline' AND e.registration_id=a.id AND e.revision=a.revision AND e.status=a.status))`).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("restored source management guards or authorization history are invalid")
	}
	return nil
}

// SuspendRestoredSourceFacts changes only the newly restored database. Original
// checkpoints, immutable facts, receipts and revoked registrations are retained.
// The restored target needs an explicit current administrator enable operation.
func SuspendRestoredSourceFacts(ctx context.Context, database *sql.DB) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var present bool
	if err = tx.QueryRowContext(ctx, `SELECT to_regclass('public.source_fact_offline_authorizations') IS NOT NULL`).Scan(&present); err != nil {
		return err
	}
	if !present {
		return nil
	}
	if err = requireSourceAdministrator(ctx, tx); err != nil {
		return err
	}
	if err = validateManagedSourceFactsRestore(ctx, tx); err != nil {
		return err
	}
	if err = SuspendRestoredContents(ctx, tx); err != nil {
		return err
	}
	if err = suspendRestoredBudgets(ctx, tx); err != nil {
		return err
	}
	var currentPresent bool
	if err = tx.QueryRowContext(ctx, `SELECT to_regclass('public.source_runtime_snapshots') IS NOT NULL`).Scan(&currentPresent); err != nil {
		return err
	}
	if currentPresent {
		if _, err = tx.ExecContext(ctx, `UPDATE source_runtime_snapshots SET revision=revision+1,generation=generation+1,page_ordinal=1,cursor_value='',visible_generation=NULL,requires_attention=false,last_error='restore_requires_refresh',last_success_at=NULL,last_attempt_at=clock_timestamp()`); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM source_runtime_snapshot_pages`); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `WITH changed AS (
 UPDATE source_fact_streams SET status='paused',requires_attention=true,last_error='restore_requires_authorization',revision=revision+1,updated_at=clock_timestamp()
 WHERE status<>'revoked' RETURNING id,revision,status)
 INSERT INTO source_fact_management_events(registration_kind,registration_id,revision,action,status,actor)
 SELECT 'live',id::text,revision,'state_change',status,current_user FROM changed`)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `WITH changed AS (
 UPDATE source_fact_offline_authorizations SET status='disabled',revision=revision+1,updated_at=clock_timestamp()
 WHERE status<>'revoked' RETURNING id,revision,status)
 INSERT INTO source_fact_management_events(registration_kind,registration_id,revision,action,status,actor)
 SELECT 'offline',id,revision,'state_change',status,current_user FROM changed`)
	if err != nil {
		return err
	}
	return tx.Commit()
}
