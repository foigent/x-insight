package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/foigent/x-insight/internal/workspace"
	"github.com/jackc/pgx/v5/pgconn"
)

func followupError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.Code == "40001" || pg.Code == "40P01" || pg.Code == "23505") {
		return workspace.ErrConflict
	}
	return workspaceError(err)
}

func readFollowup(ctx context.Context, q workspaceQueryer, caseID, findingID string) (workspace.Followup, error) {
	if err := authorizeScopedCase(ctx, q, caseID); err != nil {
		return workspace.Followup{CaseID: caseID, FindingID: findingID}, err
	}
	return readPersistedFollowup(ctx, q, caseID, findingID)
}

// Internal snapshot reads validate persisted identity without consulting current
// user access. Online callers must authorize the case before using this loader.
func readPersistedFollowup(ctx context.Context, q workspaceQueryer, caseID, findingID string) (workspace.Followup, error) {
	f := workspace.Followup{CaseID: caseID, FindingID: findingID}
	var found bool
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM case_findings WHERE case_id=$1 AND id=$2)`, caseID, findingID).Scan(&found); err != nil {
		return f, err
	}
	if !found {
		return f, workspace.ErrNotFound
	}
	err := q.QueryRowContext(ctx, `SELECT assignee_subject_id, assignee_name, due_at, due_timezone, version, updated_at, updated_by_subject
FROM case_finding_followups WHERE case_id=$1 AND finding_id=$2`, caseID, findingID).Scan(&f.AssigneeSubjectID, &f.AssigneeName, &f.DueAt, &f.DueTimezone, &f.Version, &f.UpdatedAt, &f.UpdatedBySubject)
	if errors.Is(err, sql.ErrNoRows) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	f.Recorded = true
	if f.DueAt != nil {
		at := f.DueAt.UTC()
		f.DueAt = &at
	}
	if f.UpdatedAt != nil {
		at := f.UpdatedAt.UTC()
		f.UpdatedAt = &at
	}
	return f, nil
}

func readFollowupHistory(ctx context.Context, q workspaceQueryer, caseID, findingID string, through, before int64, limit int) (workspace.FollowupHistory, error) {
	h := workspace.FollowupHistory{Items: []workspace.FollowupAction{}, ThroughVersion: through}
	if through < 0 || before < 0 || limit < 1 || limit > 100 {
		return h, workspace.ErrInvalid
	}
	if before > 0 && before-1 > through {
		return h, workspace.ErrInvalid
	}
	rows, err := q.QueryContext(ctx, `SELECT receipt FROM case_finding_actions WHERE case_id=$1 AND finding_id=$2 AND result_version<=$3 AND ($4::bigint=0 OR result_version<$4) ORDER BY result_version DESC LIMIT $5`, caseID, findingID, through, before, limit+1)
	if err != nil {
		return h, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var a workspace.FollowupAction
		if err = rows.Scan(&raw); err != nil {
			return h, err
		}
		if err = json.Unmarshal(raw, &a); err != nil {
			return h, err
		}
		h.Items = append(h.Items, a)
	}
	if err = rows.Err(); err != nil {
		return h, err
	}
	if len(h.Items) > limit {
		h.Items = h.Items[:limit]
		v := h.Items[limit-1].Current.Version
		h.NextBefore = &v
	}
	return h, nil
}

func (s *WorkspaceStore) GetFindingReview(ctx context.Context, caseID, findingID string) (workspace.FollowupReview, error) {
	r := workspace.FollowupReview{AllowedActions: []string{}}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, `SELECT version,status FROM investigation_cases WHERE id=$1`, caseID).Scan(&r.CaseVersion, &r.CaseStatus); err != nil {
		return r, workspaceError(err)
	}
	if r.Followup, err = readFollowup(ctx, tx, caseID, findingID); err != nil {
		return r, err
	}
	if r.History, err = readFollowupHistory(ctx, tx, caseID, findingID, r.Followup.Version, 0, 20); err != nil {
		return r, err
	}
	return r, commitWorkspaceTx(ctx, tx)
}

func (s *WorkspaceStore) GetFindingHistory(ctx context.Context, caseID, findingID string, through, before int64, limit int) (workspace.FollowupHistory, error) {
	var h workspace.FollowupHistory
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return h, err
	}
	defer tx.Rollback()
	f, err := readFollowup(ctx, tx, caseID, findingID)
	if err != nil {
		return h, err
	}
	if through > f.Version {
		return h, workspace.ErrInvalid
	}
	h, err = readFollowupHistory(ctx, tx, caseID, findingID, through, before, limit)
	if err != nil {
		return h, err
	}
	return h, commitWorkspaceTx(ctx, tx)
}

func (s *WorkspaceStore) ApplyFindingFollowup(ctx context.Context, caseID, findingID string, actor workspace.Actor, command workspace.FollowupCommand, assignee workspace.AssigneeLookup, now time.Time) (workspace.FollowupAction, error) {
	var result workspace.FollowupAction
	if actor.Kind != "human" || now.IsZero() {
		return result, workspace.ErrInvalid
	}
	if _, err := workspace.HumanActor(actor.Ref); err != nil {
		return result, err
	}
	c, err := workspace.NormalizeFollowupCommand(command)
	if err != nil {
		return result, err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return result, err
	}
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err := prepareScopedCase(ctx, tx, caseID); err != nil {
		return result, err
	}
	var status string
	var version int64
	if err = tx.QueryRowContext(ctx, `SELECT status,version FROM investigation_cases WHERE id=$1 FOR UPDATE`, caseID).Scan(&status, &version); err != nil {
		return result, followupError(err)
	}
	previous, err := readFollowup(ctx, tx, caseID, findingID)
	if err != nil {
		return result, err
	}
	var existingHash string
	var receipt []byte
	err = tx.QueryRowContext(ctx, `SELECT request_sha256,receipt FROM case_finding_actions WHERE case_id=$1 AND finding_id=$2 AND actor_subject_id=$3 AND operation_id=$4::uuid`, caseID, findingID, actor.Ref, c.OperationID).Scan(&existingHash, &receipt)
	if err == nil {
		if existingHash != digest {
			return result, workspace.ErrConflict
		}
		if err = json.Unmarshal(receipt, &result); err != nil {
			return result, err
		}
		return result, commitWorkspaceTx(ctx, tx)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	if status != "open" {
		return result, workspace.ErrSealed
	}
	if version != c.ExpectedCaseVersion || previous.Version != c.ExpectedVersion {
		return result, workspace.ErrConflict
	}
	name := ""
	if id := c.Payload.AssigneeSubjectID; id != nil {
		if previous.AssigneeSubjectID != nil && *id == *previous.AssigneeSubjectID {
			name = previous.AssigneeName
		} else if assignee.Active && assignee.SubjectID == *id && assignee.Name != "" && len(assignee.Name) <= 64 {
			name = assignee.Name
		} else {
			return result, workspace.ErrInvalid
		}
	}
	now = now.UTC().Truncate(time.Microsecond)
	current := workspace.Followup{CaseID: caseID, FindingID: findingID, Recorded: true, Version: previous.Version + 1, Responsibility: workspace.Responsibility{AssigneeSubjectID: c.Payload.AssigneeSubjectID, AssigneeName: name, DueAt: c.Payload.DueAt, DueTimezone: c.Payload.DueTimezone}, UpdatedAt: &now, UpdatedBySubject: actor.Ref}
	if current.DueAt != nil {
		at := current.DueAt.UTC()
		current.DueAt = &at
	}
	result = workspace.FollowupAction{OperationID: c.OperationID, Operation: c.Operation, ActorSubjectID: actor.Ref, At: now, ExpectedVersion: c.ExpectedVersion, ExpectedCaseVersion: c.ExpectedCaseVersion, CaseVersion: version + 1, Reason: c.Payload.Reason, Previous: previous, Current: current}
	_, err = tx.ExecContext(ctx, `INSERT INTO case_finding_followups(case_id,finding_id,assignee_subject_id,assignee_name,due_at,due_timezone,version,updated_at,updated_by_subject) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT(case_id,finding_id) DO UPDATE SET assignee_subject_id=EXCLUDED.assignee_subject_id,assignee_name=EXCLUDED.assignee_name,due_at=EXCLUDED.due_at,due_timezone=EXCLUDED.due_timezone,version=EXCLUDED.version,updated_at=EXCLUDED.updated_at,updated_by_subject=EXCLUDED.updated_by_subject`, caseID, findingID, current.AssigneeSubjectID, current.AssigneeName, current.DueAt, current.DueTimezone, current.Version, now, actor.Ref)
	if err != nil {
		return result, followupError(err)
	}
	raw, err = json.Marshal(result)
	if err != nil {
		return result, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO case_finding_actions(case_id,finding_id,actor_subject_id,operation_id,request_sha256,result_version,created_at,receipt) VALUES($1,$2,$3,$4::uuid,$5,$6,$7,$8::jsonb)`, caseID, findingID, actor.Ref, c.OperationID, digest, current.Version, now, string(raw))
	if err != nil {
		return result, followupError(err)
	}
	if err = touchOpenCase(ctx, tx, caseID, now); err != nil {
		return result, err
	}
	return result, followupError(commitWorkspaceTx(ctx, tx))
}

// Used by offline restore validators and by SealCase after prepareScopedCase
// and loadCaseRecord have authorized the case in the same transaction.
func loadFollowupSnapshot(ctx context.Context, q workspaceQueryer, caseID string) (*workspace.FollowupSnapshot, error) {
	s := &workspace.FollowupSnapshot{Followups: []workspace.Followup{}, Actions: []workspace.FollowupAction{}}
	rows, err := q.QueryContext(ctx, `SELECT id FROM case_findings WHERE case_id=$1 ORDER BY id`, caseID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for _, id := range ids {
		f, e := readPersistedFollowup(ctx, q, caseID, id)
		if e != nil {
			return nil, e
		}
		s.Followups = append(s.Followups, f)
	}
	rows, err = q.QueryContext(ctx, `SELECT receipt FROM case_finding_actions WHERE case_id=$1 ORDER BY finding_id,result_version`, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var a workspace.FollowupAction
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
		s.Actions = append(s.Actions, a)
	}
	return s, rows.Err()
}
