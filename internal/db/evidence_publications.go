package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/foigent/x-insight/internal/investigation"
)

const evidencePublicationLock int64 = 0x5849455652455631

var ErrEvidenceRevisionNotFound = errors.New("evidence revision not found")

func (s *EvidenceStore) ResolveEvidenceRevision(ctx context.Context, id string) (investigation.EvidenceRevision, error) {
	if s == nil || s.db == nil {
		return investigation.EvidenceRevision{}, fmt.Errorf("evidence store is not initialized")
	}
	var revision investigation.EvidenceRevision
	const selectRevision = `
SELECT snapshot_id::text, revision
FROM evidence_publications`
	var row *sql.Row
	if id == "" {
		row = s.db.QueryRowContext(ctx, selectRevision+" ORDER BY revision DESC LIMIT 1")
	} else {
		row = s.db.QueryRowContext(ctx, selectRevision+" WHERE snapshot_id = $1::uuid", id)
	}
	err := row.Scan(&revision.ID, &revision.Order)
	if errors.Is(err, sql.ErrNoRows) {
		return investigation.EvidenceRevision{}, ErrEvidenceRevisionNotFound
	}
	return revision, err
}

func beginEvidencePublication(ctx context.Context, tx *sql.Tx) (string, error) {
	var random [16]byte
	rand.Read(random[:])
	random[6] = (random[6] & 0x0f) | 0x40
	random[8] = (random[8] & 0x3f) | 0x80
	id := fmt.Sprintf("%x-%x-%x-%x-%x", random[:4], random[4:6], random[6:8], random[8:10], random[10:])
	// Historical databases are still used by pre-upgrade validation. A
	// database claiming the new schema must have the publication table.
	var activeID sql.NullString
	err := tx.QueryRowContext(ctx, `
SELECT CASE
  WHEN to_regclass('public.evidence_publications') IS NOT NULL
    THEN set_config('xinsight.evidence_publication_id', $1, true)
  WHEN EXISTS (SELECT 1 FROM schema_migrations WHERE version >= '000048_')
    THEN ''
END`, id).Scan(&activeID)
	if err == nil && activeID.Valid && activeID.String == "" {
		return "", errors.New("evidence publication table is missing at schema 48 or later")
	}
	return activeID.String, err
}

func publishEvidenceTransaction(ctx context.Context, tx *sql.Tx, id string) error {
	// Take the lock only for publication, after evidence writes. The next
	// READ COMMITTED statement gets a fresh head and the lock orders commits.
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", evidencePublicationLock); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO evidence_publications (snapshot_id, revision)
SELECT $1::uuid, revision + 1
FROM evidence_publications
ORDER BY revision DESC LIMIT 1`, id)
	return err
}

type evidenceSnapshotQueryer struct {
	workspaceQueryer
}

func (q evidenceSnapshotQueryer) QueryContext(ctx context.Context, statement string, args ...any) (*sql.Rows, error) {
	statement, args = bindEvidenceSnapshot(ctx, statement, args)
	return q.workspaceQueryer.QueryContext(ctx, statement, args...)
}

func (q evidenceSnapshotQueryer) QueryRowContext(ctx context.Context, statement string, args ...any) *sql.Row {
	statement, args = bindEvidenceSnapshot(ctx, statement, args)
	return q.workspaceQueryer.QueryRowContext(ctx, statement, args...)
}

// The read CTEs shadow the four evidence tables for an investigation query.
// NOT MATERIALIZED leaves execution and timeline index predicates available to
// the planner. Historical time-only readers retain their original SQL.
func bindEvidenceSnapshot(ctx context.Context, statement string, args []any) (string, []any) {
	revision, ok := investigation.EvidenceRevisionFromContext(ctx)
	if !ok {
		return statement, args
	}
	parameter := len(args) + 1
	tables := [...]string{"evidence_events", "capture_gaps", "import_provenance", "import_provenance_events"}
	var query strings.Builder
	query.WriteString("WITH ")
	for index, table := range tables {
		if index > 0 {
			query.WriteString(",\n")
		}
		fmt.Fprintf(&query, `%[1]s AS NOT MATERIALIZED (
  SELECT entry.* FROM public.%[1]s entry
  WHERE entry.publication_id IS NULL OR entry.publication_id NOT IN (
    SELECT snapshot_id FROM evidence_publications WHERE revision > $%[2]d
  )
)`, table, parameter)
	}
	body := strings.TrimSpace(statement)
	if tail, hasCTEs := strings.CutPrefix(body, "WITH "); hasCTEs {
		query.WriteString(",\n")
		body = tail
	} else {
		query.WriteByte('\n')
	}
	query.WriteString(body)
	return query.String(), append(args, revision.Order)
}
