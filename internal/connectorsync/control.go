package connectorsync

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

var policyOperationPattern = regexp.MustCompile(`^xipw_[a-f0-9]{32}$`)
var actorPattern = regexp.MustCompile(`^xs_[a-z0-9]{16,64}$`)
var digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// BeginControl is ONLY for the privileged, currently authorized human policy
// handler. The immutable existing policy operation is its request identity.
// Importer cannot INSERT a run; ordinary Begin/Schedule never consume control
// runs. This grants only this call stack's credentials slot, never collection.
// A replay may spend the original remaining budget, but cannot renew it.
func (s *Store) BeginControl(ctx context.Context, id, operation, actor, digest string, create bool) (context.Context, func(error), error) {
	if !installationPattern.MatchString(id) || !policyOperationPattern.MatchString(operation) || !actorPattern.MatchString(actor) || !digestPattern.MatchString(digest) {
		return ctx, nil, ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return ctx, nil, e
	}
	defer tx.Rollback()
	// Resuming an existing control also requires the migration owner. Merely
	// knowing its public operation/actor/hash is not importer authority.
	var owner bool
	if e = tx.QueryRowContext(ctx, `SELECT current_user=pg_get_userbyid(relowner) FROM pg_class WHERE oid='public.connector_sync_runs'::regclass`).Scan(&owner); e != nil {
		return ctx, nil, e
	}
	if !owner {
		return ctx, nil, ErrInvalid
	}
	p := &permit{Store: s, Installation: id, Worker: "credentials", Unit: "credentials", Owner: s.Owner, Run: "control:" + operation}
	if e = tx.QueryRowContext(ctx, `SELECT revision FROM connector_sync_controls WHERE installation_id=$1 FOR UPDATE`, id).Scan(&p.Revision); e != nil {
		return ctx, nil, e
	}
	var bound, busy bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.xinsight_sync_units($1)),EXISTS(SELECT 1 FROM connector_sync_slots WHERE installation_id=$1 AND busy)`, id).Scan(&bound, &busy); e != nil {
		return ctx, nil, e
	}
	if !bound {
		return ctx, nil, ErrBinding
	}
	if busy {
		return ctx, nil, ErrConflict
	}
	var priorRevision int64
	var priorActor, priorHash, state string
	var pages, attempts int
	e = tx.QueryRowContext(ctx, `SELECT revision,updated_by,results->>'request_sha256',state,deadline,pages,attempts FROM connector_sync_runs WHERE installation_id=$1 AND request_id=$2 FOR UPDATE`, id, p.Run).Scan(&priorRevision, &priorActor, &priorHash, &state, &p.Deadline, &pages, &attempts)
	if e == nil {
		if priorActor != actor || priorHash != digest {
			return ctx, nil, ErrConflict
		}
		if priorRevision != p.Revision || state != "running" || !time.Now().Before(p.Deadline) || pages >= 32 || attempts >= 64 {
			return ctx, nil, ErrPaused
		}
	} else if errors.Is(e, sql.ErrNoRows) && create {
		var active bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM connector_sync_runs WHERE installation_id=$1 AND state='running' AND deadline>clock_timestamp())`, id).Scan(&active); e != nil {
			return ctx, nil, e
		}
		if active {
			return ctx, nil, ErrConflict
		}
		if _, e = tx.ExecContext(ctx, `UPDATE connector_sync_runs SET state='expired' WHERE installation_id=$1 AND state='running' AND deadline<=clock_timestamp()`, id); e != nil {
			return ctx, nil, e
		}
		results, _ := json.Marshal(map[string]string{"request_sha256": digest})
		if e = tx.QueryRowContext(ctx, `INSERT INTO connector_sync_runs(installation_id,request_id,revision,started_at,deadline,state,updated_by,results) VALUES($1,$2,$3,statement_timestamp(),statement_timestamp()+interval '60 seconds','running',$4,$5::jsonb) RETURNING deadline`, id, p.Run, p.Revision, actor, string(results)).Scan(&p.Deadline); e != nil {
			return ctx, nil, e
		}
		if e = trimReceipts(ctx, tx, id); e != nil {
			return ctx, nil, e
		}
		if s.Audit != nil {
			if e = s.Audit(ctx, tx, Request{Action: "control", InstallationID: id, Actor: actor, RequestID: operation}); e != nil {
				return ctx, nil, e
			}
		}
	} else {
		return ctx, nil, e
	}
	b := make([]byte, 16)
	if _, e = rand.Read(b); e != nil {
		return ctx, nil, e
	}
	p.Token = hex.EncodeToString(b)
	if _, e = tx.ExecContext(ctx, `INSERT INTO connector_sync_slots(installation_id,worker,owner,token,revision,busy,started_at) VALUES($1,'credentials',$2,$3,$4,true,clock_timestamp()) ON CONFLICT(installation_id,worker) DO UPDATE SET owner=excluded.owner,token=excluded.token,revision=excluded.revision,busy=true,started_at=excluded.started_at,ended_at=NULL,last_error=''`, id, p.Owner, p.Token, p.Revision); e != nil {
		return ctx, nil, e
	}
	if e = tx.Commit(); e != nil {
		return ctx, nil, e
	}
	child, cancel := context.WithDeadline(context.WithValue(ctx, contextKey{}, p), p.Deadline)
	return child, func(result error) { cancel(); s.finish(p, result) }, nil
}

// Once keys are protected by their revision CAS. A control key can be evicted
// only after the original durable policy operation can reject its recreation.
// Failed preflight identities remain bounded tombstones; if none are safely
// evictable, reject a new run rather than silently renewing an old identity.
func trimReceipts(ctx context.Context, tx *sql.Tx, id string) error {
	_, e := tx.ExecContext(ctx, `DELETE FROM connector_sync_runs WHERE installation_id=$1 AND request_id IN (
 SELECT r.request_id FROM connector_sync_runs r WHERE r.installation_id=$1 AND r.state<>'running'
 AND (r.request_id NOT LIKE 'control:%' OR EXISTS(SELECT 1 FROM review_capture_ur_operations o
 WHERE o.installation_id=r.installation_id AND 'control:'||o.operation_ref=r.request_id
 AND o.operator_subject=r.updated_by AND o.request_sha256=r.results->>'request_sha256'))
 ORDER BY r.started_at LIMIT GREATEST(0,(SELECT count(*)-32 FROM connector_sync_runs WHERE installation_id=$1)))`, id)
	if e != nil {
		return e
	}
	var bounded bool
	if e = tx.QueryRowContext(ctx, `SELECT count(*)<=32 FROM connector_sync_runs WHERE installation_id=$1`, id).Scan(&bounded); e != nil {
		return e
	}
	if !bounded {
		return ErrConflict
	}
	return nil
}
