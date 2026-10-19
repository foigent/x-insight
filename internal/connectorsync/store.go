package connectorsync

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

type Store struct {
	Audit func(context.Context, *sql.Tx, Request) error
	DB    *sql.DB
	Owner string
}

func New(db *sql.DB) *Store { return &Store{DB: db, Owner: processOwner()} }

func (s *Store) Resolve(ctx context.Context, source, org, project string, feed bool) (string, error) {
	rows, e := s.DB.QueryContext(ctx, `SELECT public.xinsight_sync_scope($1,$2,$3,$4)`, source, org, project, feed)
	if e != nil {
		return "", e
	}
	defer rows.Close()
	var id string
	count := 0
	for rows.Next() {
		if e = rows.Scan(&id); e != nil {
			return "", e
		}
		count++
	}
	if e = rows.Err(); e != nil {
		return "", e
	}
	if count != 1 {
		return "", ErrBinding
	}
	return id, nil
}

func (s *Store) Status(ctx context.Context, id string) (Status, error) {
	out := Status{InstallationID: id, Peer: "unknown", Slots: []Slot{}, Required: []string{}}
	if !installationPattern.MatchString(id) {
		return out, ErrInvalid
	}
	e := s.DB.QueryRowContext(ctx, `SELECT auto_sync,cadence_seconds,revision,last_checked_at,last_data_at,last_error,request_id FROM connector_sync_controls WHERE installation_id=$1`, id).Scan(&out.Auto, &out.Cadence, &out.Revision, &out.Checked, &out.Data, &out.Error, &out.RequestID)
	if e != nil {
		return out, e
	}
	var run Run
	var runResults []byte
	e = s.DB.QueryRowContext(ctx, `SELECT request_id,revision,started_at,deadline,pages,attempts,state,observed_items,results FROM connector_sync_runs WHERE installation_id=$1 ORDER BY revision DESC,started_at DESC LIMIT 1`, id).Scan(&run.RequestID, &run.Revision, &run.Started, &run.Deadline, &run.Pages, &run.Attempts, &run.State, &run.Items, &runResults)
	if e == nil {
		if json.Unmarshal(runResults, &run.Results) != nil {
			return out, ErrInvalid
		}
		if run.State == "running" && !time.Now().Before(run.Deadline) {
			run.State = "expired"
		}
		out.Run = &run
	} else if !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT worker,revision,busy,started_at,ended_at,last_error,owner FROM connector_sync_slots WHERE installation_id=$1 ORDER BY worker`, id)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	quiet := true
	workers := map[string]bool{}
	workerRevisions := map[string]int64{}
	workerAlive := map[string]bool{}
	workerErrors := map[string]string{}
	staleBusy := false
	activeRun := out.Run != nil && out.Run.State == "running" && out.Run.Revision == out.Revision
	unknown := false
	required, e := s.DB.QueryContext(ctx, `SELECT DISTINCT worker FROM public.xinsight_sync_units($1) ORDER BY worker`, id)
	if e != nil {
		return out, e
	}
	for required.Next() {
		var w string
		if e = required.Scan(&w); e != nil {
			required.Close()
			return out, e
		}
		out.Required = append(out.Required, w)
	}
	e = required.Err()
	required.Close()
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var v Slot
		var owner string
		if e = rows.Scan(&v.Worker, &v.Revision, &v.Busy, &v.Started, &v.Ended, &v.Error, &owner); e != nil {
			return out, e
		}
		out.Slots = append(out.Slots, v)
		workers[v.Worker] = true
		workerRevisions[v.Worker] = v.Revision
		workerAlive[v.Worker] = ownerKnownAlive(owner)
		workerErrors[v.Worker] = v.Error
		quiet = quiet && !v.Busy
		staleBusy = staleBusy || v.Busy && (v.Revision != out.Revision || !out.Auto && !activeRun)
		if v.Busy && (time.Since(v.Started) > 70*time.Second || !workerAlive[v.Worker]) {
			unknown = true
		}
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	for _, w := range out.Required {
		quiet = quiet && workers[w] && workerRevisions[w] == out.Revision
	}
	quiet = quiet && len(out.Required) > 0
	switch {
	case unknown:
		out.Effective = "unknown"
	case staleBusy:
		out.Effective = "stopping"
	case activeRun:
		out.Effective = "manual_running"
	case out.Auto:
		if len(out.Required) > 0 {
			out.Effective = "auto_running"
			for _, w := range out.Required {
				if !workers[w] || workerRevisions[w] != out.Revision || !workerAlive[w] {
					out.Effective = "unknown"
				}
			}
		} else {
			out.Effective = "unknown"
		}
		if out.Effective == "auto_running" {
			for _, w := range out.Required {
				if workerErrors[w] != "" {
					out.Effective = "blocked"
				}
			}
		}
	case quiet:
		out.Effective = "quiet"
	default:
		out.Effective = "unknown"
	}
	return out, nil
}

// Change is called only inside the root controller after human authorization.
// The revision CAS also prevents a pruned old idempotency key becoming a new run.
func (s *Store) Change(ctx context.Context, r Request) (Status, error) {
	if !r.Valid() || r.Action == "status" {
		return Status{}, ErrInvalid
	}
	raw, _ := json.Marshal(r)
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Status{}, e
	}
	defer tx.Rollback()
	var rev int64
	var auto bool
	var priorID string
	var prior []byte
	var cadence *int
	e = tx.QueryRowContext(ctx, `SELECT revision,auto_sync,cadence_seconds,request_id,request_body FROM connector_sync_controls WHERE installation_id=$1 FOR UPDATE`, r.InstallationID).Scan(&rev, &auto, &cadence, &priorID, &prior)
	if e != nil {
		return Status{}, e
	}
	if priorID == r.RequestID {
		var p Request
		if json.Unmarshal(prior, &p) != nil || !sameRequest(p, r) {
			return Status{}, ErrConflict
		}
		if e = tx.Commit(); e != nil {
			return Status{}, e
		}
		return s.Status(ctx, r.InstallationID)
	}
	if r.Action == "once" {
		var oldRev int64
		e = tx.QueryRowContext(ctx, `SELECT revision FROM connector_sync_runs WHERE installation_id=$1 AND request_id=$2`, r.InstallationID, r.RequestID).Scan(&oldRev)
		if e == nil {
			if oldRev != r.ExpectedRevision+1 {
				return Status{}, ErrConflict
			}
			if e = tx.Commit(); e != nil {
				return Status{}, e
			}
			return s.Status(ctx, r.InstallationID)
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return Status{}, e
		}
	}

	if r.Action == "once" {
		var bound bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.xinsight_sync_units($1))`, r.InstallationID).Scan(&bound); e != nil {
			return Status{}, e
		}
		if !bound {
			return Status{}, ErrBinding
		}
	}
	if rev != r.ExpectedRevision {
		return Status{}, ErrConflict
	}
	// A deadline stops new work but cannot prove the old worker has returned.
	var busy, active bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM connector_sync_slots WHERE installation_id=$1 AND busy),EXISTS(SELECT 1 FROM connector_sync_runs WHERE installation_id=$1 AND state='running' AND deadline>clock_timestamp())`, r.InstallationID).Scan(&busy, &active); e != nil {
		return Status{}, e
	}
	if (r.Action == "once" || r.Auto) && (busy || active) {
		return Status{}, ErrConflict
	}
	if r.Action == "once" && auto {
		return Status{}, ErrConflict
	}
	if _, e = tx.ExecContext(ctx, `UPDATE connector_sync_runs SET state=CASE WHEN deadline<=clock_timestamp() THEN 'expired' ELSE 'cancelled' END WHERE installation_id=$1 AND state='running'`, r.InstallationID); e != nil {
		return Status{}, e
	}
	if r.Action == "once" {
		auto = false
	} else {
		auto = r.Auto
		if r.Cadence != nil {
			cadence = r.Cadence
		} else if auto && cadence == nil {
			v := DefaultCadence
			cadence = &v
		}
	}
	if _, e = tx.ExecContext(ctx, `UPDATE connector_sync_controls SET auto_sync=$2,cadence_seconds=$3,revision=revision+1,request_id=$4,request_body=$5::jsonb,updated_by=$6,updated_at=clock_timestamp() WHERE installation_id=$1`, r.InstallationID, auto, cadence, r.RequestID, string(raw), r.Actor); e != nil {
		return Status{}, e
	}
	if r.Action == "once" {
		if _, e = tx.ExecContext(ctx, `INSERT INTO connector_sync_runs(installation_id,request_id,revision,started_at,deadline,state,updated_by) VALUES($1,$2,$3,statement_timestamp(),statement_timestamp()+interval '60 seconds','running',$4)`, r.InstallationID, r.RequestID, rev+1, r.Actor); e != nil {
			return Status{}, e
		}
	}
	if e = trimReceipts(ctx, tx, r.InstallationID); e != nil {
		return Status{}, e
	}
	if s.Audit != nil {
		if e = s.Audit(ctx, tx, r); e != nil {
			return Status{}, e
		}
	}
	if e = tx.Commit(); e != nil {
		return Status{}, e
	}
	return s.Status(ctx, r.InstallationID)
}
func sameRequest(a, b Request) bool {
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(aa) == string(bb)
}

// Begin reserves one reusable slot for a logical collection/control step.
// The caller always invokes finish, including after context cancellation.
func (s *Store) Begin(ctx context.Context, id, worker string, base time.Duration, unit ...string) (context.Context, func(error), error) {
	if !installationPattern.MatchString(id) {
		return ctx, nil, ErrBinding
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return ctx, nil, e
	}
	defer tx.Rollback()
	p := &permit{Store: s, Installation: id, Worker: worker, Owner: s.Owner, Deadline: time.Now().Add(time.Minute), Unit: worker}
	if len(unit) > 0 {
		p.Unit = unit[0]
	}
	if existing, _ := ctx.Value(contextKey{}).(*permit); existing != nil && existing.Installation == id {
		return ctx, func(error) {}, nil
	}
	var auto bool
	var cadence *int
	if e = tx.QueryRowContext(ctx, `SELECT auto_sync,cadence_seconds,revision FROM connector_sync_controls WHERE installation_id=$1 FOR UPDATE`, id).Scan(&auto, &cadence, &p.Revision); e != nil {
		return ctx, nil, e
	}
	var bound bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.xinsight_sync_units($1))`, id).Scan(&bound); e != nil {
		return ctx, nil, e
	}
	if !bound {
		return ctx, nil, ErrBinding
	}
	var run string
	var deadline time.Time
	e = tx.QueryRowContext(ctx, `SELECT request_id,deadline FROM connector_sync_runs WHERE installation_id=$1 AND revision=$2 AND request_id NOT LIKE 'control:%' AND state='running' AND deadline>clock_timestamp() AND attempts<64 AND pages<32 FOR UPDATE`, id, p.Revision).Scan(&run, &deadline)
	if e == nil {
		p.Run = run
		p.Deadline = deadline
		var finished bool
		if e = tx.QueryRowContext(ctx, `SELECT COALESCE(results->>$3,'') IN ('done','error') FROM connector_sync_runs WHERE installation_id=$1 AND request_id=$2`, id, run, p.Unit).Scan(&finished); e != nil {
			return ctx, nil, e
		}
		if finished {
			return ctx, nil, ErrPaused
		}
	} else if !errors.Is(e, sql.ErrNoRows) {
		return ctx, nil, e
	}
	var owner string
	var busy bool
	var started time.Time
	var slotRevision int64
	e = tx.QueryRowContext(ctx, `SELECT owner,busy,started_at,revision FROM connector_sync_slots WHERE installation_id=$1 AND worker=$2 FOR UPDATE`, id, worker).Scan(&owner, &busy, &started, &slotRevision)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return ctx, nil, e
	}
	if busy {
		return ctx, nil, ErrConflict
	}
	if !auto && p.Run == "" {
		_, e = tx.ExecContext(ctx, `INSERT INTO connector_sync_slots(installation_id,worker,owner,token,revision,busy,started_at,ended_at) VALUES($1,$2,$3,repeat('0',32),$4,false,clock_timestamp(),clock_timestamp()) ON CONFLICT(installation_id,worker) DO UPDATE SET revision=excluded.revision,owner=excluded.owner,ended_at=excluded.ended_at`, id, worker, s.Owner, p.Revision)
		if e != nil {
			return ctx, nil, e
		}
		if e = tx.Commit(); e != nil {
			return ctx, nil, e
		}
		return ctx, nil, ErrPaused
	}
	// Source profile cadence is applied per job by ManagedWorker; the other
	// classes have one schedule each. NULL preserves existing configured timing.
	if p.Run == "" && cadence != nil && worker != "source" && slotRevision == p.Revision && time.Since(started) < time.Duration(*cadence)*time.Second {
		return ctx, nil, ErrPaused
	}
	b := make([]byte, 16)
	if _, e = rand.Read(b); e != nil {
		return ctx, nil, e
	}
	p.Token = hex.EncodeToString(b)
	_, e = tx.ExecContext(ctx, `INSERT INTO connector_sync_slots(installation_id,worker,owner,token,revision,busy,started_at) VALUES($1,$2,$3,$4,$5,true,clock_timestamp()) ON CONFLICT(installation_id,worker) DO UPDATE SET owner=excluded.owner,token=excluded.token,revision=excluded.revision,busy=true,started_at=excluded.started_at,ended_at=NULL,last_error=''`, id, worker, p.Owner, p.Token, p.Revision)
	if e != nil {
		return ctx, nil, e
	}
	if e = tx.Commit(); e != nil {
		return ctx, nil, e
	}
	child, cancel := context.WithDeadline(context.WithValue(ctx, contextKey{}, p), p.Deadline)
	finish := func(result error) { cancel(); s.finish(p, result) }
	return child, finish, nil
}

func (s *Store) attempt(ctx context.Context, p *permit) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = Fence(ctx, tx); e != nil {
		return e
	}
	if p.Run != "" {
		res, e := tx.ExecContext(ctx, `UPDATE connector_sync_runs SET attempts=attempts+1 WHERE installation_id=$1 AND request_id=$2 AND revision=$3 AND state='running' AND deadline>clock_timestamp() AND attempts<64 AND pages<32`, p.Installation, p.Run, p.Revision)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return ErrPaused
		}
	}
	if _, e = tx.ExecContext(ctx, `UPDATE connector_sync_controls SET last_checked_at=clock_timestamp() WHERE installation_id=$1`, p.Installation); e != nil {
		return e
	}
	return tx.Commit()
}

// Page counts each data/control page before its request, independently of
// capability attempts. No caller may start page 33 even if a response fails.
func Page(ctx context.Context) error {
	p, _ := ctx.Value(contextKey{}).(*permit)
	if p == nil || p.Run == "" {
		return nil
	}
	tx, e := p.Store.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = Fence(ctx, tx); e != nil {
		return e
	}
	r, e := tx.ExecContext(ctx, `UPDATE connector_sync_runs SET pages=pages+1 WHERE installation_id=$1 AND request_id=$2 AND state='running' AND pages<32 AND deadline>clock_timestamp()`, p.Installation, p.Run)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrPaused
	}
	return tx.Commit()
}

func (s *Store) Schedule(ctx context.Context, id string, base time.Duration) (time.Duration, bool, bool, error) {
	var auto bool
	var cadence *int
	var pending bool
	e := s.DB.QueryRowContext(ctx, `SELECT c.auto_sync,c.cadence_seconds,EXISTS(SELECT 1 FROM connector_sync_runs r WHERE r.installation_id=c.installation_id AND r.revision=c.revision AND r.request_id NOT LIKE 'control:%' AND r.state='running' AND r.deadline>clock_timestamp() AND r.pages<32 AND r.attempts<64) FROM connector_sync_controls c WHERE c.installation_id=$1`, id).Scan(&auto, &cadence, &pending)
	if e != nil {
		return base, false, false, e
	}
	if cadence != nil {
		base = time.Duration(*cadence) * time.Second
	}
	return base, pending, cadence != nil, nil
}

// Receiver resolves a public, domain-separated verifier from the supplied
// existing runtime credential. Neither the credential nor digest is logged.
func (s *Store) Receiver(ctx context.Context, credential string) (string, error) {
	h := sha256.New()
	h.Write([]byte("unirouter.xinsight.feed-credential.v1"))
	h.Write([]byte{0})
	h.Write([]byte(credential))
	digest := hex.EncodeToString(h.Sum(nil))
	rows, e := s.DB.QueryContext(ctx, `SELECT public.xinsight_sync_receiver($1)`, digest)
	if e != nil {
		return "", e
	}
	defer rows.Close()
	var id string
	count := 0
	for rows.Next() {
		if e = rows.Scan(&id); e != nil {
			return "", e
		}
		count++
	}
	if e = rows.Err(); e != nil {
		return "", e
	}
	if count != 1 {
		return "", ErrBinding
	}
	return id, nil
}
