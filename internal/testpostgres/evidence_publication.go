package testpostgres

import (
	"context"
	"database/sql"
)

// ExecWithSyntheticEvidencePublication keeps set-based capacity fixtures on the
// same publication boundary as normal ingestion without decoding each row.
func ExecWithSyntheticEvidencePublication(ctx context.Context, db *sql.DB, statement string, args ...any) (sql.Result, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT set_config('xinsight.evidence_publication_id', gen_random_uuid()::text, true)`); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(0x5849455652455631)); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO evidence_publications(snapshot_id, revision)
SELECT current_setting('xinsight.evidence_publication_id')::uuid, revision+1
FROM evidence_publications ORDER BY revision DESC LIMIT 1`); err != nil {
		return nil, err
	}
	return result, tx.Commit()
}
