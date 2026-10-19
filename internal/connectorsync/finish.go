package connectorsync

import (
	"context"
	"database/sql"
	"time"
)

func (s *Store) finish(p *permit, result error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return
	}
	defer tx.Rollback()
	var rev int64
	if e = tx.QueryRowContext(ctx, `SELECT revision FROM connector_sync_controls WHERE installation_id=$1 FOR UPDATE`, p.Installation).Scan(&rev); e != nil {
		return
	}
	code := ""
	if result != nil && !IsControlStop(result) {
		code = "sync_failed"
	}
	r, e := tx.ExecContext(ctx, `UPDATE connector_sync_slots SET busy=false,ended_at=clock_timestamp(),last_error=$5 WHERE installation_id=$1 AND worker=$2 AND token=$3 AND owner=$4`, p.Installation, p.Worker, p.Token, p.Owner, code)
	if e != nil {
		return
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return
	}
	if rev == p.Revision {
		if _, e = tx.ExecContext(ctx, `UPDATE connector_sync_controls SET last_error=$2 WHERE installation_id=$1`, p.Installation, code); e != nil {
			return
		}
		if p.Run != "" {
			state := "done"
			if result != nil {
				state = "error"
			}
			if p.More && result == nil {
				state = "more"
			}
			// Stop/cancel is not remote corruption, and an expired run cannot resume.
			if _, e = tx.ExecContext(ctx, `UPDATE connector_sync_runs SET results=jsonb_set(results,ARRAY[$3],to_jsonb($4::text)),observed_items=observed_items+$5 WHERE installation_id=$1 AND request_id=$2 AND state='running'`, p.Installation, p.Run, p.Unit, state, p.Items); e != nil {
				return
			}
			if e = settle(ctx, tx, p.Installation); e != nil {
				return
			}
		}
	}
	_ = tx.Commit()
}

func settle(ctx context.Context, tx *sql.Tx, id string) error {
	_, e := tx.ExecContext(ctx, `UPDATE connector_sync_runs r SET state=CASE
 WHEN request_id LIKE 'control:%' AND results->>'credentials' IN ('done','error') THEN CASE WHEN results->>'credentials'='error' THEN 'error' ELSE 'completed' END
 WHEN deadline<=clock_timestamp() OR pages>=32 OR attempts>=64 THEN CASE WHEN attempts=0 THEN 'expired' ELSE 'partial' END
 WHEN NOT EXISTS(SELECT 1 FROM public.xinsight_sync_units($1) u WHERE COALESCE(r.results->>u.unit,'') NOT IN ('done','error'))
 THEN CASE WHEN EXISTS(SELECT 1 FROM jsonb_each_text(r.results) j WHERE j.value='error') THEN CASE WHEN observed_items>0 THEN 'partial' ELSE 'error' END
 WHEN observed_items=0 THEN 'empty' ELSE 'completed' END
 ELSE state END
 WHERE installation_id=$1 AND state='running'
 AND NOT EXISTS(SELECT 1 FROM connector_sync_slots s WHERE s.installation_id=$1 AND s.busy)`, id)
	return e
}

// Reconcile is local maintenance only. It never sends or starts a request.
// It reclaims a slot only with a positive original-process-exit proof.
func (s *Store) Reconcile(ctx context.Context, id string) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var revision int64
	if e = tx.QueryRowContext(ctx, `SELECT revision FROM connector_sync_controls WHERE installation_id=$1 FOR UPDATE`, id).Scan(&revision); e != nil {
		return e
	}
	rows, e := tx.QueryContext(ctx, `SELECT worker,owner FROM connector_sync_slots WHERE installation_id=$1 AND busy FOR UPDATE`, id)
	if e != nil {
		return e
	}
	type old struct{ worker, owner string }
	var dead []old
	for rows.Next() {
		var o old
		if e = rows.Scan(&o.worker, &o.owner); e != nil {
			rows.Close()
			return e
		}
		if ownerExited(o.owner) {
			dead = append(dead, o)
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, o := range dead {
		if _, e = tx.ExecContext(ctx, `UPDATE connector_sync_slots SET busy=false,ended_at=clock_timestamp(),last_error='owner_exited',revision=$4 WHERE installation_id=$1 AND worker=$2 AND owner=$3`, id, o.worker, o.owner, revision); e != nil {
			return e
		}
	}
	if len(dead) > 0 {
		if _, e = tx.ExecContext(ctx, `UPDATE connector_sync_runs SET state='interrupted' WHERE installation_id=$1 AND state='running'`, id); e != nil {
			return e
		}
	}
	if e = settle(ctx, tx, id); e != nil {
		return e
	}
	return tx.Commit()
}

// Work is optional only for test stores; production WorkspaceStore supplies it.
type Provider interface{ SyncStore() *Store }

func Work(ctx context.Context, provider any, id, worker string) (context.Context, func(error), error) {
	if p, ok := provider.(Provider); ok {
		return p.SyncStore().Begin(ctx, id, worker, time.Minute)
	}
	return ctx, func(error) {}, nil
}

// Keeps database/sql's transaction contract visible at this boundary.
var _ *sql.Tx

// LocalCommit is for an existing native file-publication critical section,
// after its host operation lock is acquired. It contains no network I/O. A
// pause either wins before publication or waits for its definite completion.
func LocalCommit(ctx context.Context, publish func() error) error {
	p, _ := ctx.Value(contextKey{}).(*permit)
	if p == nil {
		return publish()
	}
	tx, e := p.Store.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = Fence(ctx, tx); e != nil {
		return e
	}
	if e = publish(); e != nil {
		return e
	}
	return tx.Commit()
}
