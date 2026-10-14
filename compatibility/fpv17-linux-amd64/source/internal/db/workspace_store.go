package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/foigent/x-insight/internal/workchain"
	"github.com/foigent/x-insight/internal/workspace"
)

type WorkspaceStore struct {
	db                    *sql.DB
	connectorDeploymentID string
}

type workspaceQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func OpenWorkspaceStore(ctx context.Context, dsn string) (*WorkspaceStore, error) {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	// Interactive case authorization can spend its entire request deadline
	// compiling JIT code even for a small result. Apply this only to connections
	// owned by this pool, including replacements opened after idle expiry.
	config.RuntimeParams["jit"] = "off"
	conn := stdlib.OpenDB(*config)
	if err := conn.PingContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	conn.SetMaxOpenConns(10)
	conn.SetMaxIdleConns(5)
	conn.SetConnMaxLifetime(30 * time.Minute)
	conn.SetConnMaxIdleTime(5 * time.Minute)
	return &WorkspaceStore{db: conn}, nil
}

func NewWorkspaceStore(conn *sql.DB) *WorkspaceStore {
	return &WorkspaceStore{db: conn}
}

func (s *WorkspaceStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *WorkspaceStore) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("workspace store is not initialized")
	}
	return s.db.PingContext(ctx)
}

func (s *WorkspaceStore) ListCases(ctx context.Context, limit int) ([]workspace.Case, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("workspace store is not initialized")
	}
	if limit < 1 || limit > 100 {
		return nil, workspace.ErrInvalid
	}
	condition, err := unscopedContentCaseCondition(ctx, s.db, "investigation_cases.id")
	if err != nil {
		return nil, err
	}
	args := []any{limit}
	if a := workchain.FromContext(ctx); a != nil {
		condition = scopedCaseSQL("investigation_cases.id", "$2")
		args = append(args, a.Subject)
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, title, description, status, created_by_subject, created_at, updated_at,
       version, sealed_at, sealed_by_subject, sealed_revision, sealed_digest, sealed_pack
FROM investigation_cases WHERE `+condition+`
ORDER BY updated_at DESC, id
LIMIT $1`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]workspace.Case, 0)
	for rows.Next() {
		item, err := scanCase(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *WorkspaceStore) GetCase(ctx context.Context, id string) (workspace.CaseRecord, error) {
	if s == nil || s.db == nil {
		return workspace.CaseRecord{}, fmt.Errorf("workspace store is not initialized")
	}
	if workchain.FromContext(ctx) == nil {
		return loadCaseRecord(ctx, s.db, id)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return workspace.CaseRecord{}, err
	}
	defer tx.Rollback()
	// Scope authorization is latency-sensitive even for externally owned pools.
	// Keep this setting local to the read-only transaction.
	if _, err = tx.ExecContext(ctx, "SET LOCAL jit=off"); err != nil {
		return workspace.CaseRecord{}, err
	}
	if err = authorizeScopedCase(ctx, tx, id); err != nil {
		return workspace.CaseRecord{}, err
	}
	record, err := loadCaseRecord(ctx, tx, id)
	if err != nil {
		return record, err
	}
	return record, commitWorkspaceTx(ctx, tx)
}

func (s *WorkspaceStore) CreateCase(
	ctx context.Context,
	actor workspace.Actor,
	input workspace.CreateCaseInput,
	now time.Time,
	random io.Reader,
) (workspace.Case, error) {
	if s == nil || s.db == nil {
		return workspace.Case{}, fmt.Errorf("workspace store is not initialized")
	}
	if actor.Kind != "human" || workspace.ValidateCreateCase(input) != nil {
		return workspace.Case{}, workspace.ErrInvalid
	}
	id, err := workspace.NewID("xc_", random)
	if err != nil {
		return workspace.Case{}, err
	}
	if workchain.FromContext(ctx) != nil {
		return workspace.Case{}, workspace.ErrInvalid
	}
	return insertCaseRecord(ctx, s.db, id, actor, input, now.UTC())
}

func insertCaseRecord(ctx context.Context, q workspaceQueryer, id string, actor workspace.Actor, input workspace.CreateCaseInput, now time.Time) (workspace.Case, error) {
	row := q.QueryRowContext(ctx, `
INSERT INTO investigation_cases (
  id, title, description, status, created_by_subject, created_at, updated_at
) VALUES ($1, $2, $3, 'open', $4, $5, $5)
RETURNING id, title, description, status, created_by_subject, created_at, updated_at,
          version, sealed_at, sealed_by_subject, sealed_revision, sealed_digest, sealed_pack`,
		id, input.Title, input.Description, actor.Ref, now)
	return scanCase(row)
}

func (s *WorkspaceStore) AddCaseExecution(
	ctx context.Context,
	caseID string,
	actor workspace.Actor,
	input workspace.AddExecutionInput,
	now time.Time,
) (workspace.CaseExecution, error) {
	if s == nil || s.db == nil {
		return workspace.CaseExecution{}, fmt.Errorf("workspace store is not initialized")
	}
	if actor.Kind != "human" || workspace.ValidateAddExecution(input) != nil {
		return workspace.CaseExecution{}, workspace.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return workspace.CaseExecution{}, err
	}
	defer tx.Rollback()
	if err := prepareScopedCase(ctx, tx, caseID); err != nil {
		return workspace.CaseExecution{}, err
	}
	now = now.UTC()
	if err := assertOpenCase(ctx, tx, caseID); err != nil {
		return workspace.CaseExecution{}, err
	}
	item := workspace.CaseExecution{
		CaseID: caseID, ExecutionID: input.ExecutionID, AsOf: input.AsOf.UTC(),
		AddedBySubject: actor.Ref, AddedAt: now,
	}
	result, err := tx.ExecContext(ctx, `
INSERT INTO case_executions (
  case_id, execution_id, as_of, added_by_subject, added_at
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (case_id, execution_id, as_of) DO NOTHING`,
		item.CaseID, item.ExecutionID, item.AsOf, item.AddedBySubject, item.AddedAt)
	if err != nil {
		return workspace.CaseExecution{}, err
	}
	if inserted, err := result.RowsAffected(); err != nil {
		return workspace.CaseExecution{}, err
	} else if inserted > 0 {
		if err := touchOpenCase(ctx, tx, caseID, now); err != nil {
			return workspace.CaseExecution{}, err
		}
	}
	if err := tx.QueryRowContext(ctx, `
SELECT case_id, execution_id, as_of, added_by_subject, added_at
FROM case_executions
WHERE case_id = $1 AND execution_id = $2 AND as_of = $3`,
		item.CaseID, item.ExecutionID, item.AsOf,
	).Scan(
		&item.CaseID, &item.ExecutionID, &item.AsOf,
		&item.AddedBySubject, &item.AddedAt,
	); err != nil {
		return workspace.CaseExecution{}, err
	}
	item.AsOf = item.AsOf.UTC()
	item.AddedAt = item.AddedAt.UTC()
	if err := commitWorkspaceTx(ctx, tx); err != nil {
		return workspace.CaseExecution{}, err
	}
	return item, nil
}

func (s *WorkspaceStore) CreateFinding(
	ctx context.Context,
	caseID string,
	actor workspace.Actor,
	input workspace.CreateFindingInput,
	now time.Time,
	random io.Reader,
) (workspace.Finding, error) {
	if s == nil || s.db == nil {
		return workspace.Finding{}, fmt.Errorf("workspace store is not initialized")
	}
	if actor.Kind != "human" || workspace.ValidateCreateFinding(input) != nil {
		return workspace.Finding{}, workspace.ErrInvalid
	}
	id, err := workspace.NewID("xf_", random)
	if err != nil {
		return workspace.Finding{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return workspace.Finding{}, err
	}
	defer tx.Rollback()
	if err := prepareScopedCase(ctx, tx, caseID); err != nil {
		return workspace.Finding{}, err
	}
	now = now.UTC()
	if err := touchOpenCase(ctx, tx, caseID, now); err != nil {
		return workspace.Finding{}, err
	}
	item, err := insertFindingRecord(ctx, tx, id, caseID, actor, input, now)
	if err != nil {
		return workspace.Finding{}, err
	}
	if err := commitWorkspaceTx(ctx, tx); err != nil {
		return workspace.Finding{}, err
	}
	return item, nil
}

func insertFindingRecord(ctx context.Context, q workspaceQueryer, id, caseID string, actor workspace.Actor, input workspace.CreateFindingInput, now time.Time) (workspace.Finding, error) {
	var item workspace.Finding
	err := q.QueryRowContext(ctx, `
INSERT INTO case_findings (
  id, case_id, title, statement, classification, status,
  created_by_subject, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, 'open', $6, $7, $7)
RETURNING id, case_id, title, statement, classification, status,
          created_by_subject, created_at, updated_at, version`,
		id, caseID, input.Title, input.Statement, input.Classification, actor.Ref, now,
	).Scan(
		&item.ID, &item.CaseID, &item.Title, &item.Statement, &item.Classification,
		&item.Status, &item.CreatedBySubject, &item.CreatedAt, &item.UpdatedAt, &item.Version,
	)
	if err != nil {
		return workspace.Finding{}, err
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	return item, nil
}

func (s *WorkspaceStore) AddEvidenceLink(
	ctx context.Context,
	caseID string,
	actor workspace.Actor,
	input workspace.AddEvidenceInput,
	now time.Time,
) (workspace.EvidenceLink, error) {
	if s == nil || s.db == nil {
		return workspace.EvidenceLink{}, fmt.Errorf("workspace store is not initialized")
	}
	if workchain.FromContext(ctx) != nil && input.SourceRef == "" {
		input.SourceRef = scopedSourceRefPlaceholder
	}
	if actor.Kind != "human" || workspace.ValidateAddEvidence(input) != nil {
		return workspace.EvidenceLink{}, workspace.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return workspace.EvidenceLink{}, err
	}
	defer tx.Rollback()
	if err := prepareScopedCase(ctx, tx, caseID); err != nil {
		return workspace.EvidenceLink{}, err
	}
	if workchain.FromContext(ctx) != nil {
		if _, err := scopedReferenceRequirement(ctx, tx, "event", input.EvidenceEventID); err != nil {
			return workspace.EvidenceLink{}, err
		}
	}
	var actualHash, actualSourceRef string
	if err := tx.QueryRowContext(ctx, `
SELECT event_hash, source_ref FROM evidence_events WHERE id = $1::uuid`,
		input.EvidenceEventID).Scan(&actualHash, &actualSourceRef); err != nil {
		return workspace.EvidenceLink{}, workspaceError(err)
	}
	if workchain.FromContext(ctx) != nil && input.SourceRef == scopedSourceRefPlaceholder {
		input.SourceRef = actualSourceRef
	}
	if actualHash != input.EventHash || actualSourceRef != input.SourceRef {
		return workspace.EvidenceLink{}, workspace.ErrConflict
	}
	if input.FindingID != "" {
		var findingCase string
		if err := tx.QueryRowContext(ctx, `SELECT case_id FROM case_findings WHERE id = $1`, input.FindingID).Scan(&findingCase); err != nil {
			return workspace.EvidenceLink{}, workspaceError(err)
		}
		if findingCase != caseID {
			return workspace.EvidenceLink{}, workspace.ErrConflict
		}
	}
	now = now.UTC()
	if err := assertOpenCase(ctx, tx, caseID); err != nil {
		return workspace.EvidenceLink{}, err
	}
	item := workspace.EvidenceLink{
		CaseID: caseID, FindingID: input.FindingID, EvidenceEventID: input.EvidenceEventID,
		Relation: input.Relation, EventHashSnapshot: actualHash, SourceRefSnapshot: actualSourceRef,
		AddedBySubject: actor.Ref, AddedAt: now,
	}
	result, err := tx.ExecContext(ctx, `
INSERT INTO case_evidence_links (
  case_id, finding_id, evidence_event_id, relation, event_hash_snapshot,
  source_ref_snapshot, added_by_subject, added_at
) VALUES ($1, NULLIF($2, ''), $3::uuid, $4, $5, $6, $7, $8)
ON CONFLICT (case_id, finding_id, evidence_event_id, relation) DO NOTHING`,
		item.CaseID, item.FindingID, item.EvidenceEventID, item.Relation,
		item.EventHashSnapshot, item.SourceRefSnapshot, item.AddedBySubject, item.AddedAt)
	if err != nil {
		return workspace.EvidenceLink{}, err
	}
	if inserted, err := result.RowsAffected(); err != nil {
		return workspace.EvidenceLink{}, err
	} else if inserted > 0 {
		if err := touchOpenCase(ctx, tx, caseID, now); err != nil {
			return workspace.EvidenceLink{}, err
		}
	}
	if err := tx.QueryRowContext(ctx, `
SELECT case_id, COALESCE(finding_id, ''), evidence_event_id::text, relation,
       event_hash_snapshot, source_ref_snapshot, added_by_subject, added_at
FROM case_evidence_links
WHERE case_id = $1
  AND finding_id IS NOT DISTINCT FROM NULLIF($2, '')
  AND evidence_event_id = $3::uuid
  AND relation = $4`,
		item.CaseID, item.FindingID, item.EvidenceEventID, item.Relation,
	).Scan(
		&item.CaseID, &item.FindingID, &item.EvidenceEventID, &item.Relation,
		&item.EventHashSnapshot, &item.SourceRefSnapshot,
		&item.AddedBySubject, &item.AddedAt,
	); err != nil {
		return workspace.EvidenceLink{}, err
	}
	if item.FindingID != input.FindingID ||
		item.EventHashSnapshot != actualHash ||
		item.SourceRefSnapshot != actualSourceRef {
		return workspace.EvidenceLink{}, workspace.ErrConflict
	}
	item.AddedAt = item.AddedAt.UTC()
	if err := commitWorkspaceTx(ctx, tx); err != nil {
		return workspace.EvidenceLink{}, err
	}
	return item, nil
}

func (s *WorkspaceStore) AddGapLink(
	ctx context.Context,
	caseID string,
	actor workspace.Actor,
	input workspace.AddGapInput,
	now time.Time,
) (workspace.GapLink, error) {
	if s == nil || s.db == nil {
		return workspace.GapLink{}, fmt.Errorf("workspace store is not initialized")
	}
	if actor.Kind != "human" || workspace.ValidateAddGap(input) != nil {
		return workspace.GapLink{}, workspace.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return workspace.GapLink{}, err
	}
	defer tx.Rollback()
	if err := prepareScopedCase(ctx, tx, caseID); err != nil {
		return workspace.GapLink{}, err
	}
	if workchain.FromContext(ctx) != nil {
		if _, err := scopedReferenceRequirement(ctx, tx, "gap", input.GapID); err != nil {
			return workspace.GapLink{}, err
		}
	}
	var actualClaim, actualType string
	if err := tx.QueryRowContext(ctx, `
SELECT affected_claim, gap_type FROM capture_gaps WHERE id = $1::uuid`,
		input.GapID).Scan(&actualClaim, &actualType); err != nil {
		return workspace.GapLink{}, workspaceError(err)
	}
	if actualClaim != input.AffectedClaim || actualType != input.GapType {
		return workspace.GapLink{}, workspace.ErrConflict
	}
	if input.FindingID != "" {
		var findingCase string
		if err := tx.QueryRowContext(ctx, `SELECT case_id FROM case_findings WHERE id = $1`, input.FindingID).Scan(&findingCase); err != nil {
			return workspace.GapLink{}, workspaceError(err)
		}
		if findingCase != caseID {
			return workspace.GapLink{}, workspace.ErrConflict
		}
	}
	now = now.UTC()
	if err := assertOpenCase(ctx, tx, caseID); err != nil {
		return workspace.GapLink{}, err
	}
	item := workspace.GapLink{
		CaseID: caseID, FindingID: input.FindingID, GapID: input.GapID, Relation: input.Relation,
		AffectedClaimSnapshot: actualClaim, GapTypeSnapshot: actualType,
		AddedBySubject: actor.Ref, AddedAt: now,
	}
	result, err := tx.ExecContext(ctx, `
INSERT INTO case_gap_links (
  case_id, finding_id, gap_id, relation, affected_claim_snapshot,
  gap_type_snapshot, added_by_subject, added_at
) VALUES ($1, NULLIF($2, ''), $3::uuid, $4, $5, $6, $7, $8)
ON CONFLICT (case_id, finding_id, gap_id, relation) DO NOTHING`,
		item.CaseID, item.FindingID, item.GapID, item.Relation,
		item.AffectedClaimSnapshot, item.GapTypeSnapshot, item.AddedBySubject, item.AddedAt)
	if err != nil {
		return workspace.GapLink{}, err
	}
	if inserted, err := result.RowsAffected(); err != nil {
		return workspace.GapLink{}, err
	} else if inserted > 0 {
		if err := touchOpenCase(ctx, tx, caseID, now); err != nil {
			return workspace.GapLink{}, err
		}
	}
	if err := tx.QueryRowContext(ctx, `
SELECT case_id, COALESCE(finding_id, ''), gap_id::text, relation,
       affected_claim_snapshot, gap_type_snapshot, added_by_subject, added_at
FROM case_gap_links
WHERE case_id = $1
  AND finding_id IS NOT DISTINCT FROM NULLIF($2, '')
  AND gap_id = $3::uuid
  AND relation = $4`,
		item.CaseID, item.FindingID, item.GapID, item.Relation,
	).Scan(
		&item.CaseID, &item.FindingID, &item.GapID, &item.Relation,
		&item.AffectedClaimSnapshot, &item.GapTypeSnapshot,
		&item.AddedBySubject, &item.AddedAt,
	); err != nil {
		return workspace.GapLink{}, err
	}
	if item.FindingID != input.FindingID ||
		item.AffectedClaimSnapshot != actualClaim ||
		item.GapTypeSnapshot != actualType {
		return workspace.GapLink{}, workspace.ErrConflict
	}
	item.AddedAt = item.AddedAt.UTC()
	if err := commitWorkspaceTx(ctx, tx); err != nil {
		return workspace.GapLink{}, err
	}
	return item, nil
}

func (s *WorkspaceStore) CreateAnnotation(
	ctx context.Context,
	caseID string,
	actor workspace.Actor,
	input workspace.CreateAnnotationInput,
	now time.Time,
	random io.Reader,
) (workspace.Annotation, error) {
	if s == nil || s.db == nil {
		return workspace.Annotation{}, fmt.Errorf("workspace store is not initialized")
	}
	if actor.Kind != "human" || workspace.ValidateAnnotation(input) != nil {
		return workspace.Annotation{}, workspace.ErrInvalid
	}
	id, err := workspace.NewID("xn_", random)
	if err != nil {
		return workspace.Annotation{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return workspace.Annotation{}, err
	}
	defer tx.Rollback()
	if err := prepareScopedCase(ctx, tx, caseID); err != nil {
		return workspace.Annotation{}, err
	}
	now = now.UTC()
	if err := touchOpenCase(ctx, tx, caseID, now); err != nil {
		return workspace.Annotation{}, err
	}
	if err := assertAnnotationTarget(ctx, tx, caseID, input.TargetKind, input.TargetRef); err != nil {
		return workspace.Annotation{}, err
	}
	item := workspace.Annotation{
		ID: id, CaseID: caseID, TargetKind: input.TargetKind, TargetRef: input.TargetRef,
		Body: input.Body, CreatedBySubject: actor.Ref, CreatedAt: now,
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO case_annotations (
  id, case_id, target_kind, target_ref, body, created_by_subject, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		item.ID, item.CaseID, item.TargetKind, item.TargetRef,
		item.Body, item.CreatedBySubject, item.CreatedAt); err != nil {
		return workspace.Annotation{}, err
	}
	if err := commitWorkspaceTx(ctx, tx); err != nil {
		return workspace.Annotation{}, err
	}
	return item, nil
}

func (s *WorkspaceStore) SealCase(
	ctx context.Context,
	caseID string,
	actor workspace.Actor,
	now time.Time,
) (workspace.Case, error) {
	if s == nil || s.db == nil {
		return workspace.Case{}, fmt.Errorf("workspace store is not initialized")
	}
	if actor.Kind != "human" {
		return workspace.Case{}, workspace.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return workspace.Case{}, err
	}
	defer tx.Rollback()
	if err := prepareScopedCase(ctx, tx, caseID); err != nil {
		return workspace.Case{}, err
	}
	// All Workspace mutations serialize on the Case before touching children.
	// Acquire that same lock before reading the complete sealed snapshot.
	if err := assertOpenCase(ctx, tx, caseID); err != nil {
		return workspace.Case{}, followupError(err)
	}
	record, err := loadCaseRecord(ctx, tx, caseID)
	if err != nil {
		return workspace.Case{}, err
	}
	if record.Case.Status != "open" {
		return workspace.Case{}, workspace.ErrSealed
	}
	record.Responsibility, err = loadFollowupSnapshot(ctx, tx, caseID)
	if err != nil {
		return workspace.Case{}, err
	}
	record.Assessments, err = loadAssessmentSnapshot(ctx, tx, caseID)
	if err != nil {
		return workspace.Case{}, err
	}
	now = now.UTC()
	revision := record.Case.Version + 1
	_, payload, digest, err := workspace.BuildEvidencePack(record, now, actor.Ref, revision)
	if err != nil {
		return workspace.Case{}, err
	}
	row := tx.QueryRowContext(ctx, `
UPDATE investigation_cases
SET status = 'sealed',
    updated_at = $2,
    version = $3,
    sealed_at = $2,
    sealed_by_subject = $4,
    sealed_revision = $3,
    sealed_digest = $5,
    sealed_pack = $6
WHERE id = $1 AND status = 'open' AND version = $7
RETURNING id, title, description, status, created_by_subject, created_at, updated_at,
          version, sealed_at, sealed_by_subject, sealed_revision, sealed_digest, sealed_pack`,
		caseID, now, revision, actor.Ref, digest, payload, record.Case.Version)
	sealed, err := scanCase(row)
	if errors.Is(err, sql.ErrNoRows) {
		return workspace.Case{}, workspace.ErrConflict
	}
	if err != nil {
		return workspace.Case{}, followupError(err)
	}
	if err := commitWorkspaceTx(ctx, tx); err != nil {
		return workspace.Case{}, followupError(err)
	}
	return sealed, nil
}

func (s *WorkspaceStore) ListSavedViews(ctx context.Context, subject string, limit int) ([]workspace.SavedView, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("workspace store is not initialized")
	}
	if limit < 1 || limit > 100 {
		return nil, workspace.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, owner_subject, name, filters::text, presentation, created_at, updated_at, version
FROM saved_investigation_views
WHERE owner_subject = $1
ORDER BY updated_at DESC, id
LIMIT $2`, subject, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]workspace.SavedView, 0)
	for rows.Next() {
		var item workspace.SavedView
		var filters []byte
		if err := rows.Scan(
			&item.ID, &item.OwnerSubject, &item.Name, &filters, &item.Presentation,
			&item.CreatedAt, &item.UpdatedAt, &item.Version,
		); err != nil {
			return nil, err
		}
		item.CreatedAt = item.CreatedAt.UTC()
		item.UpdatedAt = item.UpdatedAt.UTC()
		item.Filters = append(json.RawMessage(nil), filters...)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *WorkspaceStore) CreateSavedView(
	ctx context.Context,
	actor workspace.Actor,
	input workspace.CreateSavedViewInput,
	now time.Time,
	random io.Reader,
) (workspace.SavedView, error) {
	if s == nil || s.db == nil {
		return workspace.SavedView{}, fmt.Errorf("workspace store is not initialized")
	}
	if actor.Kind != "human" || workspace.ValidateSavedView(input) != nil {
		return workspace.SavedView{}, workspace.ErrInvalid
	}
	id, err := workspace.NewID("xv_", random)
	if err != nil {
		return workspace.SavedView{}, err
	}
	now = now.UTC()
	var item workspace.SavedView
	var filters []byte
	err = s.db.QueryRowContext(ctx, `
INSERT INTO saved_investigation_views (
  id, owner_subject, name, filters, presentation, created_at, updated_at
) VALUES ($1, $2, $3, $4::jsonb, $5, $6, $6)
RETURNING id, owner_subject, name, filters::text, presentation, created_at, updated_at, version`,
		id, actor.Ref, input.Name, []byte(input.Filters), input.Presentation, now,
	).Scan(
		&item.ID, &item.OwnerSubject, &item.Name, &filters, &item.Presentation,
		&item.CreatedAt, &item.UpdatedAt, &item.Version,
	)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return workspace.SavedView{}, workspace.ErrConflict
		}
		return workspace.SavedView{}, err
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	item.Filters = append(json.RawMessage(nil), filters...)
	return item, nil
}

func (s *WorkspaceStore) ResolveGap(
	ctx context.Context,
	actor workspace.Actor,
	gapID string,
	input workspace.ResolveGapInput,
	now time.Time,
) (workspace.GapResolution, error) {
	if s == nil || s.db == nil {
		return workspace.GapResolution{}, fmt.Errorf("workspace store is not initialized")
	}
	if actor.Kind != "human" || workspace.ValidateResolveGap(input) != nil {
		return workspace.GapResolution{}, workspace.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return workspace.GapResolution{}, err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM capture_gaps WHERE id = $1::uuid)`, gapID).Scan(&exists); err != nil {
		return workspace.GapResolution{}, err
	}
	if !exists {
		return workspace.GapResolution{}, workspace.ErrNotFound
	}
	now = now.UTC()
	var item workspace.GapResolution
	err = tx.QueryRowContext(ctx, `
INSERT INTO capture_gap_resolutions (
  gap_id, state, note, updated_by_subject, updated_at
) VALUES ($1::uuid, $2, $3, $4, $5)
ON CONFLICT (gap_id) DO UPDATE SET
  state = EXCLUDED.state,
  note = EXCLUDED.note,
  updated_by_subject = EXCLUDED.updated_by_subject,
  updated_at = EXCLUDED.updated_at,
  version = capture_gap_resolutions.version + 1
RETURNING gap_id::text, state, note, updated_by_subject, updated_at, version`,
		gapID, input.State, input.Note, actor.Ref, now,
	).Scan(
		&item.GapID, &item.State, &item.Note, &item.UpdatedBySubject,
		&item.UpdatedAt, &item.Version,
	)
	if err != nil {
		return workspace.GapResolution{}, err
	}
	item.UpdatedAt = item.UpdatedAt.UTC()
	if err := commitWorkspaceTx(ctx, tx); err != nil {
		return workspace.GapResolution{}, err
	}
	return item, nil
}

func (s *WorkspaceStore) LinkAlertToCase(
	ctx context.Context,
	actor workspace.Actor,
	alertID string,
	caseID string,
	now time.Time,
) (workspace.AlertCaseLink, error) {
	if s == nil || s.db == nil {
		return workspace.AlertCaseLink{}, fmt.Errorf("workspace store is not initialized")
	}
	if actor.Kind != "human" || alertID == "" || caseID == "" {
		return workspace.AlertCaseLink{}, workspace.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return workspace.AlertCaseLink{}, err
	}
	defer tx.Rollback()
	if err := assertOpenCase(ctx, tx, caseID); err != nil {
		return workspace.AlertCaseLink{}, err
	}
	now = now.UTC()
	var state string
	err = tx.QueryRowContext(ctx, `
SELECT state FROM investigation_alerts WHERE id = $1 FOR KEY SHARE`, alertID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return workspace.AlertCaseLink{}, workspace.ErrNotFound
	}
	if err != nil {
		return workspace.AlertCaseLink{}, err
	}
	result, err := tx.ExecContext(ctx, `
INSERT INTO investigation_alert_case_links (
  alert_id, case_id, linked_by_subject, linked_at
) VALUES ($1, $2, $3, $4)
ON CONFLICT (alert_id) DO NOTHING`, alertID, caseID, actor.Ref, now)
	if err != nil {
		return workspace.AlertCaseLink{}, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return workspace.AlertCaseLink{}, err
	}
	var item workspace.AlertCaseLink
	if err := tx.QueryRowContext(ctx, `
SELECT l.alert_id, l.case_id, a.state, l.linked_by_subject, l.linked_at
FROM investigation_alert_case_links l
JOIN investigation_alerts a ON a.id = l.alert_id
WHERE l.alert_id = $1`, alertID).Scan(
		&item.AlertID, &item.CaseID, &item.AlertState,
		&item.LinkedBySubject, &item.LinkedAt,
	); err != nil {
		return workspace.AlertCaseLink{}, err
	}
	if item.CaseID != caseID {
		return workspace.AlertCaseLink{}, workspace.ErrConflict
	}
	if inserted > 0 {
		if err := touchOpenCase(ctx, tx, caseID, now); err != nil {
			return workspace.AlertCaseLink{}, err
		}
	}
	if err := commitWorkspaceTx(ctx, tx); err != nil {
		return workspace.AlertCaseLink{}, err
	}
	item.LinkedAt = item.LinkedAt.UTC()
	return item, nil
}

func assertAnnotationTarget(
	ctx context.Context,
	tx *sql.Tx,
	caseID string,
	targetKind string,
	targetRef string,
) error {
	var (
		exists bool
		err    error
	)
	switch targetKind {
	case "case":
		exists = targetRef == caseID
	case "execution":
		err = tx.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1 FROM case_executions WHERE case_id = $1 AND execution_id = $2
)`, caseID, targetRef).Scan(&exists)
	case "finding":
		err = tx.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1 FROM case_findings WHERE case_id = $1 AND id = $2
)`, caseID, targetRef).Scan(&exists)
	case "evidence":
		err = tx.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1 FROM case_evidence_links
  WHERE case_id = $1 AND evidence_event_id::text = $2
)`, caseID, targetRef).Scan(&exists)
	case "gap":
		err = tx.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1 FROM case_gap_links WHERE case_id = $1 AND gap_id::text = $2
)`, caseID, targetRef).Scan(&exists)
	default:
		return workspace.ErrInvalid
	}
	if err != nil {
		return err
	}
	if !exists {
		return workspace.ErrNotFound
	}
	return nil
}

func assertOpenCase(ctx context.Context, tx *sql.Tx, caseID string) error {
	var status string
	err := tx.QueryRowContext(
		ctx,
		`SELECT status FROM investigation_cases WHERE id = $1 FOR UPDATE`,
		caseID,
	).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return workspace.ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "open" {
		return workspace.ErrSealed
	}
	return nil
}

func touchOpenCase(ctx context.Context, tx *sql.Tx, caseID string, now time.Time) error {
	var status string
	err := tx.QueryRowContext(ctx, `
UPDATE investigation_cases
SET updated_at = $2, version = version + 1
WHERE id = $1 AND status = 'open'
RETURNING status`, caseID, now).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		var existing string
		lookupErr := tx.QueryRowContext(ctx, `SELECT status FROM investigation_cases WHERE id = $1`, caseID).Scan(&existing)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			return workspace.ErrNotFound
		}
		if lookupErr != nil {
			return lookupErr
		}
		return workspace.ErrSealed
	}
	return err
}

func loadCaseRecord(ctx context.Context, queryer workspaceQueryer, caseID string) (workspace.CaseRecord, error) {
	if err := authorizeScopedCase(ctx, queryer, caseID); err != nil {
		return workspace.CaseRecord{}, err
	}
	item, err := scanCase(queryer.QueryRowContext(ctx, `
SELECT id, title, description, status, created_by_subject, created_at, updated_at,
       version, sealed_at, sealed_by_subject, sealed_revision, sealed_digest, sealed_pack
FROM investigation_cases
WHERE id = $1`, caseID))
	if err != nil {
		return workspace.CaseRecord{}, workspaceError(err)
	}
	record := workspace.CaseRecord{Case: item}
	record.Contents, err = loadContentLinks(ctx, queryer, []string{caseID})
	if err != nil {
		return workspace.CaseRecord{}, err
	}
	record.Resources, err = loadResourceLinks(ctx, queryer, []string{caseID})
	if err != nil {
		return record, err
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT case_id, execution_id, as_of, added_by_subject, added_at
FROM case_executions
WHERE case_id = $1
ORDER BY execution_id, as_of`, caseID)
	if err != nil {
		return workspace.CaseRecord{}, err
	}
	for rows.Next() {
		var current workspace.CaseExecution
		if err := rows.Scan(&current.CaseID, &current.ExecutionID, &current.AsOf, &current.AddedBySubject, &current.AddedAt); err != nil {
			rows.Close()
			return workspace.CaseRecord{}, err
		}
		current.AsOf = current.AsOf.UTC()
		current.AddedAt = current.AddedAt.UTC()
		record.Executions = append(record.Executions, current)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return workspace.CaseRecord{}, err
	}
	if err := rows.Close(); err != nil {
		return workspace.CaseRecord{}, err
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT id, case_id, title, statement, classification, status,
       created_by_subject, created_at, updated_at, version
FROM case_findings
WHERE case_id = $1
ORDER BY id`, caseID)
	if err != nil {
		return workspace.CaseRecord{}, err
	}
	for rows.Next() {
		var current workspace.Finding
		if err := rows.Scan(
			&current.ID, &current.CaseID, &current.Title, &current.Statement,
			&current.Classification, &current.Status, &current.CreatedBySubject,
			&current.CreatedAt, &current.UpdatedAt, &current.Version,
		); err != nil {
			rows.Close()
			return workspace.CaseRecord{}, err
		}
		current.CreatedAt = current.CreatedAt.UTC()
		current.UpdatedAt = current.UpdatedAt.UTC()
		record.Findings = append(record.Findings, current)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return workspace.CaseRecord{}, err
	}
	if err := rows.Close(); err != nil {
		return workspace.CaseRecord{}, err
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT case_id, COALESCE(finding_id, ''), evidence_event_id::text, relation,
       event_hash_snapshot, source_ref_snapshot, added_by_subject, added_at
FROM case_evidence_links
WHERE case_id = $1
ORDER BY evidence_event_id, relation, COALESCE(finding_id, '')`, caseID)
	if err != nil {
		return workspace.CaseRecord{}, err
	}
	for rows.Next() {
		var current workspace.EvidenceLink
		if err := rows.Scan(
			&current.CaseID, &current.FindingID, &current.EvidenceEventID, &current.Relation,
			&current.EventHashSnapshot, &current.SourceRefSnapshot,
			&current.AddedBySubject, &current.AddedAt,
		); err != nil {
			rows.Close()
			return workspace.CaseRecord{}, err
		}
		current.AddedAt = current.AddedAt.UTC()
		record.Evidence = append(record.Evidence, current)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return workspace.CaseRecord{}, err
	}
	if err := rows.Close(); err != nil {
		return workspace.CaseRecord{}, err
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT case_id, COALESCE(finding_id, ''), gap_id::text, relation,
       affected_claim_snapshot, gap_type_snapshot, added_by_subject, added_at
FROM case_gap_links
WHERE case_id = $1
ORDER BY gap_id, relation, COALESCE(finding_id, '')`, caseID)
	if err != nil {
		return workspace.CaseRecord{}, err
	}
	for rows.Next() {
		var current workspace.GapLink
		if err := rows.Scan(
			&current.CaseID, &current.FindingID, &current.GapID, &current.Relation,
			&current.AffectedClaimSnapshot, &current.GapTypeSnapshot,
			&current.AddedBySubject, &current.AddedAt,
		); err != nil {
			rows.Close()
			return workspace.CaseRecord{}, err
		}
		current.AddedAt = current.AddedAt.UTC()
		record.Gaps = append(record.Gaps, current)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return workspace.CaseRecord{}, err
	}
	if err := rows.Close(); err != nil {
		return workspace.CaseRecord{}, err
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT l.alert_id, l.case_id, a.state, l.linked_by_subject, l.linked_at
FROM investigation_alert_case_links l
JOIN investigation_alerts a ON a.id = l.alert_id
WHERE l.case_id = $1
ORDER BY l.alert_id`, caseID)
	if err != nil {
		return workspace.CaseRecord{}, err
	}
	for rows.Next() {
		var current workspace.AlertCaseLink
		if err := rows.Scan(
			&current.AlertID, &current.CaseID, &current.AlertState,
			&current.LinkedBySubject, &current.LinkedAt,
		); err != nil {
			rows.Close()
			return workspace.CaseRecord{}, err
		}
		current.LinkedAt = current.LinkedAt.UTC()
		record.Alerts = append(record.Alerts, current)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return workspace.CaseRecord{}, err
	}
	if err := rows.Close(); err != nil {
		return workspace.CaseRecord{}, err
	}

	rows, err = queryer.QueryContext(ctx, `
SELECT id, case_id, target_kind, target_ref, body, created_by_subject, created_at
FROM case_annotations
WHERE case_id = $1
ORDER BY id`, caseID)
	if err != nil {
		return workspace.CaseRecord{}, err
	}
	for rows.Next() {
		var current workspace.Annotation
		if err := rows.Scan(
			&current.ID, &current.CaseID, &current.TargetKind, &current.TargetRef,
			&current.Body, &current.CreatedBySubject, &current.CreatedAt,
		); err != nil {
			rows.Close()
			return workspace.CaseRecord{}, err
		}
		current.CreatedAt = current.CreatedAt.UTC()
		record.Annotations = append(record.Annotations, current)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return workspace.CaseRecord{}, err
	}
	if err := rows.Close(); err != nil {
		return workspace.CaseRecord{}, err
	}
	if record.Executions == nil {
		record.Executions = []workspace.CaseExecution{}
	}
	if record.Findings == nil {
		record.Findings = []workspace.Finding{}
	}
	if record.Evidence == nil {
		record.Evidence = []workspace.EvidenceLink{}
	}
	if record.Gaps == nil {
		record.Gaps = []workspace.GapLink{}
	}
	if record.Alerts == nil {
		record.Alerts = []workspace.AlertCaseLink{}
	}
	if record.Annotations == nil {
		record.Annotations = []workspace.Annotation{}
	}
	return record, nil
}

func scanCase(scanner rowScanner) (workspace.Case, error) {
	var item workspace.Case
	var sealedAt sql.NullTime
	var sealedBy, sealedDigest sql.NullString
	var sealedRevision sql.NullInt64
	var sealedPack []byte
	err := scanner.Scan(
		&item.ID, &item.Title, &item.Description, &item.Status,
		&item.CreatedBySubject, &item.CreatedAt, &item.UpdatedAt, &item.Version,
		&sealedAt, &sealedBy, &sealedRevision, &sealedDigest, &sealedPack,
	)
	if err != nil {
		return workspace.Case{}, err
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	if sealedAt.Valid {
		value := sealedAt.Time.UTC()
		item.SealedAt = &value
	}
	item.SealedBySubject = sealedBy.String
	if sealedRevision.Valid {
		value := sealedRevision.Int64
		item.SealedRevision = &value
	}
	item.SealedDigest = sealedDigest.String
	if len(sealedPack) > 0 {
		item.SealedPack = append(json.RawMessage(nil), sealedPack...)
	}
	return item, nil
}

func workspaceError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return workspace.ErrNotFound
	}
	return err
}
