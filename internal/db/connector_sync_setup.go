package db

import (
	"context"
)

// The first validated completed Connect phase may continue the user's original
// finite setup action once. It never sets Auto=true, renews the deadline or
// grants later periodic maintenance permission. A changed manual preference
// always wins. Both the phase and its human-created consent are checked here.
func (s *WorkspaceStore) BeginInitialConnectorSync(ctx context.Context, id, wake string) error {
	if wake == "" {
		return nil
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	// Acquire the common control lock before taking the admission snapshot.
	// At READ COMMITTED, a SELECT waiting for this lock retains its statement
	// snapshot. BeginControl can insert a receipt without updating c, so putting
	// these predicates in the locking SELECT can miss its committed new row.
	var lockedID string
	if e = tx.QueryRowContext(ctx, `SELECT installation_id FROM connector_sync_controls WHERE installation_id=$1 FOR UPDATE`, id).Scan(&lockedID); e != nil {
		return e
	}
	var eligible bool
	e = tx.QueryRowContext(ctx, `SELECT NOT c.setup_used AND NOT c.auto_sync AND c.request_id='' AND i.status IN ('pending','connected')
 AND (SELECT count(*) FROM connector_sync_runs r WHERE r.installation_id=i.id)<32
 AND NOT EXISTS(SELECT 1 FROM connector_sync_runs r WHERE r.installation_id=i.id AND r.state='running')
 AND EXISTS(SELECT 1 FROM connector_control_operations o WHERE o.installation_id=i.id AND o.operation_id=$2 AND o.status='completed' AND o.operation_kind IN ('connect','adopt_existing_receiver'))
 AND EXISTS(SELECT 1 FROM connector_installation_invitations v WHERE v.installation_id=i.id AND v.status='consumed' AND v.created_by_subject<>'')
 FROM connector_sync_controls c JOIN connector_installations i ON i.id=c.installation_id WHERE i.id=$1`, id, wake).Scan(&eligible)
	if e != nil {
		return e
	}
	if eligible {
		if _, e = tx.ExecContext(ctx, `UPDATE connector_sync_controls SET setup_used=true WHERE installation_id=$1`, id); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO connector_sync_runs(installation_id,request_id,revision,started_at,deadline,state,updated_by) SELECT installation_id,'initial:'||installation_id,revision,statement_timestamp(),statement_timestamp()+interval '60 seconds','running','initial_connect' FROM connector_sync_controls WHERE installation_id=$1`, id); e != nil {
			return e
		}
	}
	return tx.Commit()
}
