package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workchain"
	"github.com/jackc/pgx/v5/pgconn"
)

func purposeGrantSQL(f, subject string, details, review bool) string {
	capability := "g.allow_summary AND COALESCE((to_jsonb(g)->>'allow_purpose_read')::boolean,false)"
	if details {
		capability += " AND g.allow_details"
	}
	if review {
		capability += " AND COALESCE((to_jsonb(g)->>'allow_purpose_review')::boolean,false)"
	}
	return fmt.Sprintf(`EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=%s AND g.active AND %s
 AND g.source_system_id=%s.source_system_id
 AND (g.source_project_ref IS NULL OR g.source_project_ref=%s.source_project_ref)
 AND (g.cost_project_ref IS NULL OR g.cost_project_ref=%s.cost_project_ref))`, subject, capability, f, f, f)
}

func purposeTarget(ctx context.Context, q workspaceQueryer, subject, execution string, review bool) (string, error) {
	var id string
	err := q.QueryRowContext(ctx, `SELECT f.id::text FROM workbench_economic_facts f WHERE f.execution_id=$1 AND `+scopedExecutionSQL("f.execution_id", "$2")+` AND `+purposeGrantSQL("f", "$2", true, review)+` ORDER BY f.id LIMIT 1`, execution, subject).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", workbench.ErrScopeForbidden
	}
	return id, err
}

// References are never permissions. Every basis is resolved to its native
// execution anchor and current matching field grant before returning text.
func purposeBasisAllowed(ctx context.Context, q workspaceQueryer, subject string, basis []workbench.PurposeBasis, details bool) error {
	for _, b := range basis {
		var allowed bool
		err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM evidence_events e JOIN workbench_economic_facts f ON f.execution_id=e.execution_id
 WHERE e.id::text=$1 AND e.event_hash=$2 AND e.source_system_id=f.source_system_id AND COALESCE(e.project_ref,'')=COALESCE(f.source_project_ref,'') AND `+purposeGrantSQL("f", "$3", details, false)+`)`, b.EvidenceID, b.EventHash, subject).Scan(&allowed)
		if err != nil {
			return err
		}
		if !allowed {
			return workbench.ErrScopeForbidden
		}
	}
	return nil
}

func purposeStateAllowed(ctx context.Context, q workspaceQueryer, subject string, state workbench.PurposeState, details bool) error {
	for _, claim := range state.Claims {
		if err := purposeBasisAllowed(ctx, q, subject, claim.Basis, details); err != nil {
			return err
		}
	}
	return nil
}

func readPurposeEvent(ctx context.Context, q workspaceQueryer, execution string, revision int64) (workbench.PurposeEvent, error) {
	var raw []byte
	var hash string
	err := q.QueryRowContext(ctx, `SELECT event_bytes,event_sha256 FROM workbench_purpose_revisions WHERE execution_id=$1 AND ($2::bigint=0 OR revision<=$2) ORDER BY revision DESC LIMIT 1`, execution, revision).Scan(&raw, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return workbench.PurposeEvent{ExecutionID: execution, State: workbench.EmptyPurposeState()}, nil
	}
	if err != nil {
		return workbench.PurposeEvent{}, err
	}
	var event workbench.PurposeEvent
	if json.Unmarshal(raw, &event) != nil || digestBytes(raw) != hash {
		return event, fmt.Errorf("invalid purpose event binding")
	}
	event.Hash = hash
	return event, nil
}

func (s *WorkspaceStore) ApplyWorkbenchPurpose(ctx context.Context, subject, execution string, c workbench.PurposeCommand, now time.Time) (workbench.PurposeEvent, error) {
	a := workchain.FromContext(ctx)
	if a == nil || a.Subject != subject || !a.PurposeReviewer || a.CommitCheck == nil {
		return workbench.PurposeEvent{}, workbench.ErrScopeForbidden
	}
	if err := workbench.ValidatePurposeCommand(c); err != nil {
		return workbench.PurposeEvent{}, err
	}
	// The outer scoped reader holds the grant lock through this business commit.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return workbench.PurposeEvent{}, err
	}
	defer tx.Rollback()
	id, err := purposeTarget(ctx, tx, subject, execution, true)
	if err != nil {
		return workbench.PurposeEvent{}, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('xinsight.purpose:'||$1,0))`, execution); err != nil {
		return workbench.PurposeEvent{}, err
	}
	commandBytes, _ := json.Marshal(c)
	commandHash := digestBytes(commandBytes)
	var previousCommand, previousExecution string
	var raw []byte
	var hash string
	err = tx.QueryRowContext(ctx, `SELECT execution_id,command_sha256,event_bytes,event_sha256 FROM workbench_purpose_revisions WHERE actor_subject=$1 AND idempotency_key=$2`, subject, c.IdempotencyKey).Scan(&previousExecution, &previousCommand, &raw, &hash)
	if err == nil {
		if previousExecution != execution || previousCommand != commandHash {
			return workbench.PurposeEvent{}, workbench.ErrConflict
		}
		var replay workbench.PurposeEvent
		if json.Unmarshal(raw, &replay) != nil || digestBytes(raw) != hash {
			return replay, fmt.Errorf("invalid purpose replay")
		}
		replay.Hash = hash
		if err = purposeStateAllowed(ctx, tx, subject, replay.State, true); err != nil {
			return replay, err
		}
		if err = a.CommitCheck(func() error { return nil }); err != nil {
			return workbench.PurposeEvent{}, err
		}
		return replay, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return workbench.PurposeEvent{}, err
	}
	previous, err := readPurposeEvent(ctx, tx, execution, 0)
	if err != nil {
		return previous, err
	}
	if err = purposeStateAllowed(ctx, tx, subject, previous.State, true); err != nil {
		return workbench.PurposeEvent{}, err
	}
	if err = purposeBasisAllowed(ctx, tx, subject, c.Basis, true); err != nil {
		return workbench.PurposeEvent{}, err
	}
	newID := ""
	if c.Action == "declare" || c.Action == "modify" {
		newID, err = workbench.NewID("xpc_")
		if err != nil {
			return workbench.PurposeEvent{}, err
		}
	}
	now = now.UTC().Truncate(time.Microsecond)
	state, err := workbench.ApplyPurpose(previous.State, c, subject, newID, now)
	if err != nil {
		return workbench.PurposeEvent{}, err
	}
	event := workbench.PurposeEvent{ExecutionID: execution, Revision: state.Revision, Actor: subject, RecordedAt: now, Command: c, NewClaimID: newID, PreviousHash: previous.Hash, State: state}
	raw, err = json.Marshal(event)
	if err != nil {
		return event, err
	}
	event.Hash = digestBytes(raw)
	_, err = tx.ExecContext(ctx, `INSERT INTO workbench_purpose_revisions(execution_id,revision,economic_fact_id,actor_subject,idempotency_key,command_sha256,previous_sha256,event_sha256,event_bytes,event_doc,recorded_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11)`, execution, event.Revision, id, subject, c.IdempotencyKey, commandHash, event.PreviousHash, event.Hash, raw, string(raw), now)
	if err != nil {
		var pgerr *pgconn.PgError
		if errors.As(err, &pgerr) && pgerr.Code == "23505" {
			return workbench.PurposeEvent{}, workbench.ErrConflict
		}
		return workbench.PurposeEvent{}, err
	}
	if err = commitWorkspaceTx(ctx, tx); err != nil {
		return workbench.PurposeEvent{}, err
	}
	return event, nil
}

func (s *WorkspaceStore) GetWorkbenchPurpose(ctx context.Context, subject, execution, readingID string, before int64, now time.Time) (workbench.PurposeView, error) {
	view := workbench.PurposeView{ExecutionID: execution, ReadingID: readingID, Vocabulary: workbench.PurposeVocabulary, History: []workbench.PurposeHistoryEntry{}}
	if before < 0 {
		return view, workbench.ErrInvalid
	}
	if _, err := purposeTarget(ctx, s.db, subject, execution, false); err != nil {
		return view, err
	}
	maximum := int64(0)
	if readingID != "" {
		reading, err := s.loadValidWorkbenchReading(ctx, subject, readingID, now)
		if err != nil {
			return view, err
		}
		if reading.Purpose == nil || reading.Purpose.Status != "available" {
			return view, workbench.ErrScopeForbidden
		}
		err = s.db.QueryRowContext(ctx, `SELECT COALESCE(purpose_revision,0) FROM workbench_purpose_members WHERE reading_id=$1 AND execution_id=$2 ORDER BY economic_fact_id LIMIT 1`, readingID, execution).Scan(&maximum)
		if errors.Is(err, sql.ErrNoRows) {
			return view, workbench.ErrScopeForbidden
		}
		if err != nil {
			return view, err
		}
		if maximum == 0 {
			view.State = workbench.EmptyPurposeState()
			return view, nil
		}
	}
	event, err := readPurposeEvent(ctx, s.db, execution, maximum)
	if err != nil {
		return view, err
	}
	view.State = event.State
	if err = purposeStateAllowed(ctx, s.db, subject, view.State, true); err != nil {
		return view, err
	}
	if a := workchain.FromContext(ctx); a != nil && a.Subject == subject && a.PurposeReviewer && readingID == "" {
		_, err := purposeTarget(ctx, s.db, subject, execution, true)
		if err != nil && !errors.Is(err, workbench.ErrScopeForbidden) {
			return view, err
		}
		view.CanReview = err == nil
	}
	if event.Revision == 0 {
		return view, nil
	}
	maxRevision := event.Revision
	rows, err := s.db.QueryContext(ctx, `SELECT event_bytes,event_sha256 FROM workbench_purpose_revisions WHERE execution_id=$1 AND revision<=$2 AND ($3::bigint=0 OR revision<$3) ORDER BY revision DESC LIMIT 21`, execution, maxRevision, before)
	if err != nil {
		return view, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var hash string
		if err = rows.Scan(&raw, &hash); err != nil {
			return view, err
		}
		var item workbench.PurposeEvent
		if json.Unmarshal(raw, &item) != nil || digestBytes(raw) != hash {
			return view, fmt.Errorf("invalid purpose history")
		}
		item.Hash = hash
		view.History = append(view.History, item.HistoryEntry())
	}
	if err = rows.Err(); err != nil {
		return view, err
	}
	if len(view.History) > 20 {
		view.History = view.History[:20]
		view.NextBefore = view.History[19].Revision
	}
	return view, nil
}

const purposeManifestDigestSQL = `SELECT 'sha256:'||encode(sha256(convert_to($2||E'\n'||$3||E'\n'||$4||E'\n'||COALESCE(string_agg(m.economic_fact_id::text||':'||m.execution_id||':'||COALESCE(m.purpose_revision::text,'0')||':'||m.status||':'||m.category||':'||m.event_sha256,E'\n' ORDER BY m.economic_fact_id),''),'UTF8')),'hex') FROM workbench_purpose_members m WHERE reading_id=$1`

func freezeWorkbenchPurposes(ctx context.Context, tx *sql.Tx, id, subject string, auth workbench.Authorization) (*workbench.PurposeReading, error) {
	enabled := false
	for _, g := range auth.Grants {
		enabled = enabled || g.AllowPurposeRead
	}
	if !enabled {
		return nil, nil
	}
	var available bool
	if err := tx.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id WHERE m.reading_id=$1 AND NOT (`+purposeGrantSQL("f", "$2", false, false)+`))`, id, subject).Scan(&available); err != nil {
		return nil, err
	}
	// Check all referenced evidence, including prior rejected claims retained in
	// the history. No hidden candidate changes a visible bucket or count.
	if available {
		rows, err := tx.QueryContext(ctx, `SELECT DISTINCT ON (p.execution_id) p.event_bytes FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id JOIN workbench_purpose_revisions p ON p.execution_id=f.execution_id WHERE m.reading_id=$1 AND p.recorded_at<=(SELECT cutoff FROM workbench_readings WHERE id=$1) ORDER BY p.execution_id,p.revision DESC`, id)
		if err != nil {
			return nil, err
		}
		states := []workbench.PurposeState{}
		for rows.Next() {
			var raw []byte
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return nil, err
			}
			var e workbench.PurposeEvent
			if json.Unmarshal(raw, &e) != nil {
				rows.Close()
				return nil, fmt.Errorf("invalid purpose state")
			}
			states = append(states, e.State)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		for _, state := range states {
			err = purposeStateAllowed(ctx, tx, subject, state, false)
			if errors.Is(err, workbench.ErrScopeForbidden) {
				available = false
				break
			}
			if err != nil {
				return nil, err
			}
		}
	}
	status := "unavailable"
	if available {
		status = "available"
		_, err := tx.ExecContext(ctx, `INSERT INTO workbench_purpose_members(reading_id,economic_fact_id,execution_id,purpose_revision,status,category,event_sha256)
 SELECT m.reading_id,f.id,f.execution_id,p.revision,COALESCE(p.event_doc#>>'{state,status}','unknown'),COALESCE(p.event_doc#>>'{state,category}',''),COALESCE(p.event_sha256,'')
 FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id
 LEFT JOIN LATERAL(SELECT revision,event_doc,event_sha256 FROM workbench_purpose_revisions WHERE execution_id=f.execution_id AND recorded_at<=(SELECT cutoff FROM workbench_readings WHERE id=$1) ORDER BY revision DESC LIMIT 1)p ON true WHERE m.reading_id=$1`, id)
		if err != nil {
			return nil, err
		}
	}
	var digest string
	if err := tx.QueryRowContext(ctx, purposeManifestDigestSQL, id, workbench.PurposeRule, auth.PurposeRevision, status).Scan(&digest); err != nil {
		return nil, err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO workbench_purpose_manifests(reading_id,rule_version,status,authorization_sha256,manifest_sha256) VALUES($1,$2,$3,$4,$5)`, id, workbench.PurposeRule, status, auth.PurposeRevision, digest)
	return &workbench.PurposeReading{Status: status, RuleVersion: workbench.PurposeRule, ManifestRef: digest}, err
}

func (s *WorkspaceStore) loadWorkbenchPurposeManifest(ctx context.Context, reading *workbench.Reading, auth workbench.Authorization) error {
	return loadWorkbenchPurposeManifest(ctx, s.db, reading, auth)
}

func loadWorkbenchPurposeManifest(ctx context.Context, q workspaceQueryer, reading *workbench.Reading, auth workbench.Authorization) error {
	// Older schema snapshots are used by supported upgrade/recovery checks.
	var exists bool
	if err := q.QueryRowContext(ctx, `SELECT to_regclass('public.workbench_purpose_manifests') IS NOT NULL`).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return nil
	}
	var p workbench.PurposeReading
	var authorization string
	err := q.QueryRowContext(ctx, `SELECT rule_version,status,authorization_sha256,manifest_sha256 FROM workbench_purpose_manifests WHERE reading_id=$1`, reading.ID).Scan(&p.RuleVersion, &p.Status, &authorization, &p.ManifestRef)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if p.RuleVersion != workbench.PurposeRule {
		return workbench.ErrUnsupportedVersion
	}
	if authorization != auth.PurposeRevision {
		return workbench.ErrAccessChanged
	}
	reading.Purpose = &p
	if p.Status == "available" {
		reading.Dimensions = append(reading.Dimensions, "purpose")
	}
	return nil
}

// Preserve the original scoped digest bytes while new capabilities are absent.
func purposeScopeSuffix(auth workbench.Authorization) string {
	for _, g := range auth.Grants {
		if g.AllowPurposeRead || g.AllowPurposeReview {
			return "\n" + auth.PurposeRevision
		}
	}
	return ""
}

func purposeDimensionSQL() string {
	return `(SELECT CASE WHEN p.status='adopted' THEN p.category ELSE p.status END FROM workbench_purpose_members p WHERE p.reading_id=m.reading_id AND p.economic_fact_id=f.id)`
}

func purposeStateDigest(state workbench.PurposeState) string {
	raw, _ := json.Marshal(state)
	return digestBytes(raw)
}
