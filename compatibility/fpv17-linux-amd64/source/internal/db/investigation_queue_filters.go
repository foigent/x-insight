package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/foigent/x-insight/internal/workspace"
)

func queueFilterRequestHash(kind, id string, c workspace.QueueFilterCommand) string {
	return assessmentRequestHash(struct {
		Kind    string
		ID      string
		Command workspace.QueueFilterCommand
	}{kind, id, c})
}
func (s *WorkspaceStore) ListQueueFilters(ctx context.Context, subject string) ([]workspace.QueueFilter, error) {
	items := []workspace.QueueFilter{}
	if _, err := workspace.HumanActor(subject); err != nil {
		return items, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT record FROM investigation_queue_filters WHERE owner_subject=$1 AND record->>'deleted'='false' ORDER BY record->>'name',id`, subject)
	if err != nil {
		return items, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var item workspace.QueueFilter
		if err = rows.Scan(&raw); err != nil {
			return items, err
		}
		if err = json.Unmarshal(raw, &item); err != nil {
			return items, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *WorkspaceStore) ApplyQueueFilter(ctx context.Context, subject, id, kind string, c workspace.QueueFilterCommand, now time.Time) (workspace.QueueFilter, error) {
	var empty workspace.QueueFilter
	if _, err := workspace.HumanActor(subject); err != nil {
		return empty, err
	}
	if (kind == "create" && id != "") || (kind != "create" && !workspace.ValidQueueFilterID(id)) {
		return empty, workspace.ErrInvalid
	}
	var err error
	c, err = workspace.NormalizeQueueFilterCommand(c)
	if err != nil {
		return empty, err
	}
	if (kind == "create" && c.ExpectedRevision != 0) || (kind != "create" && c.ExpectedRevision < 1) {
		return empty, workspace.ErrInvalid
	}
	if kind != "create" && kind != "update" && kind != "delete" {
		return empty, workspace.ErrInvalid
	}
	hash := queueFilterRequestHash(kind, id, c)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, subject+":"+c.OperationID); err != nil {
		return empty, err
	}
	var existingHash string
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT request_sha256,receipt FROM investigation_queue_filter_operations WHERE owner_subject=$1 AND operation_id=$2::uuid`, subject, c.OperationID).Scan(&existingHash, &raw)
	if err == nil {
		if existingHash != hash {
			return empty, workspace.ErrConflict
		}
		var receipt workspace.QueueFilterReceipt
		if err = json.Unmarshal(raw, &receipt); err != nil {
			return empty, err
		}
		return receipt.Current, commitWorkspaceTx(ctx, tx)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return empty, err
	}
	var previous *workspace.QueueFilter
	if kind == "create" {
		id, err = workspace.NewID("xqf_", nil)
		if err != nil {
			return empty, err
		}
	} else {
		if err = tx.QueryRowContext(ctx, `SELECT record FROM investigation_queue_filters WHERE id=$1 AND owner_subject=$2 FOR UPDATE`, id, subject).Scan(&raw); err != nil {
			return empty, workspaceError(err)
		}
		previous = &workspace.QueueFilter{}
		if err = json.Unmarshal(raw, previous); err != nil {
			return empty, err
		}
	}
	current, err := workspace.EvolveQueueFilter(previous, subject, id, kind, c, now)
	if err != nil {
		return empty, err
	}
	raw, err = json.Marshal(current)
	if err != nil {
		return empty, err
	}
	if kind == "create" {
		_, err = tx.ExecContext(ctx, `INSERT INTO investigation_queue_filters(id,owner_subject,revision,record) VALUES($1,$2,$3,$4::jsonb)`, id, subject, current.Revision, string(raw))
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE investigation_queue_filters SET revision=$3,record=$4::jsonb WHERE id=$1 AND owner_subject=$2`, id, subject, current.Revision, string(raw))
	}
	if err != nil {
		return empty, followupError(err)
	}
	receipt := workspace.QueueFilterReceipt{Kind: kind, Command: c, Previous: previous, Current: current}
	raw, err = json.Marshal(receipt)
	if err != nil {
		return empty, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO investigation_queue_filter_operations(owner_subject,operation_id,filter_id,result_revision,request_sha256,receipt) VALUES($1,$2::uuid,$3,$4,$5,$6::jsonb)`, subject, c.OperationID, id, current.Revision, hash, string(raw)); err != nil {
		return empty, followupError(err)
	}
	return current, commitWorkspaceTx(ctx, tx)
}

// Restore validates the complete operation chain and current projection. A
// deleted preference remains a tombstone so an unknown-result retry is stable.
func ValidateInvestigationQueueRestore(ctx context.Context, database interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) error {
	var present bool
	if err := database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version='000028_add_investigation_queue_filters' AND NOT dirty)`).Scan(&present); err != nil || !present {
		return err
	}
	rows, err := database.QueryContext(ctx, `SELECT owner_subject,operation_id::text,filter_id,result_revision,request_sha256,receipt FROM investigation_queue_filter_operations ORDER BY filter_id,result_revision`)
	if err != nil {
		return err
	}
	latest := map[string]workspace.QueueFilter{}
	invalid := errors.New("restored investigation queue preferences or receipts are invalid")
	for rows.Next() {
		var subject, operation, id, hash string
		var revision int64
		var raw []byte
		var r workspace.QueueFilterReceipt
		if err = rows.Scan(&subject, &operation, &id, &revision, &hash, &raw); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(raw, &r); err != nil {
			rows.Close()
			return invalid
		}
		c, e := workspace.NormalizeQueueFilterCommand(r.Command)
		if e != nil || !workspace.SameQueueValue(c, r.Command) || c.OperationID != operation {
			rows.Close()
			return invalid
		}
		target := id
		if r.Kind == "create" {
			target = ""
		}
		if queueFilterRequestHash(r.Kind, target, c) != hash {
			rows.Close()
			return invalid
		}
		var previous *workspace.QueueFilter
		if v, ok := latest[id]; ok {
			previous = &v
		}
		want, e := workspace.EvolveQueueFilter(previous, subject, id, r.Kind, c, r.Current.UpdatedAt)
		if e != nil || !workspace.SameQueueValue(previous, r.Previous) || !workspace.SameQueueValue(want, r.Current) || want.Revision != revision {
			rows.Close()
			return invalid
		}
		latest[id] = want
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = database.QueryContext(ctx, `SELECT id,owner_subject,revision,record FROM investigation_queue_filters`)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id, subject string
		var revision int64
		var raw []byte
		var current workspace.QueueFilter
		if err = rows.Scan(&id, &subject, &revision, &raw); err != nil {
			return err
		}
		if json.Unmarshal(raw, &current) != nil {
			return invalid
		}
		want, ok := latest[id]
		if !ok || want.OwnerSubject != subject || want.Revision != revision || !workspace.SameQueueValue(want, current) {
			return invalid
		}
		count++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if count != len(latest) {
		return invalid
	}
	return nil
}
