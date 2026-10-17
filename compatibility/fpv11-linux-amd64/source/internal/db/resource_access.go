package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

var resourceQualificationDatabase = regexp.MustCompile(`^(migration_test_|xia0_http_)[a-f0-9]{32}$`)

// StoreSyntheticResourceRecord is an internal domain qualification entry point.
// No worker, HTTP import, or CLI command calls it. Both the typed record and
// database must be explicit synthetic test inputs; production I-03 stays closed.
// The existing importer pool and immutable-record permissions are reused.
// Normal inputs may have missing predecessors, but must remain restorable.
// Deliberate corruption belongs in privileged test fixtures, not this writer.
func (s *SourceFactsStore) StoreSyntheticResourceRecord(ctx context.Context, record workbench.ResourceAccessRecord, receivedAt time.Time) (bool, error) {
	raw, err := json.Marshal(record)
	if err != nil {
		return false, workbench.ErrInvalid
	}
	if _, err = workbench.DecodeResourceAccessForImport(raw); err != nil {
		return false, err
	}
	value, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		return false, err
	}
	raw, err = connector.CanonicalizeJSONValue(value)
	if err != nil {
		return false, err
	}
	if receivedAt.IsZero() {
		return false, workbench.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var database string
	if err := tx.QueryRowContext(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		return false, err
	}
	if !resourceQualificationDatabase.MatchString(database) {
		return false, workbench.ErrScopeForbidden
	}
	// Reuse the source transaction lock so concurrent neighbouring revisions
	// cannot both pass the absence check and commit contradictory hashes.
	if err = sourceLock(ctx, tx, record.Reporter.SourceSystemID); err != nil {
		return false, err
	}
	inserted, err := storeResourceRecordTx(ctx, tx, record, raw, receivedAt, false)
	if err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return inserted, nil
}

type resourceRecordEnvelope struct {
	Reporter     workbench.ResourceScope
	Target       workbench.ResourceScope
	RecordRef    string
	Revision     int64
	PreviousHash string
	RequestID    string
	OccurredAt   time.Time
}

func storeResourceRecordTx(ctx context.Context, tx *sql.Tx, record workbench.ResourceAccessRecord, raw []byte, receivedAt time.Time, check bool) (bool, error) {
	return storeResourceEnvelopeTx(ctx, tx, resourceRecordEnvelope{record.Reporter, record.Target, record.RecordRef, record.Revision, record.PreviousHash, record.RequestID, record.OccurredAt}, raw, receivedAt, check)
}

func storeActionRecordTx(ctx context.Context, tx *sql.Tx, record workbench.ActionWitness, raw []byte, receivedAt time.Time, check bool) (bool, error) {
	return storeResourceEnvelopeTx(ctx, tx, resourceRecordEnvelope{record.Reporter, record.Target, record.RecordRef, record.Revision, record.PreviousHash, record.RequestID, record.OccurredAt}, raw, receivedAt, check)
}

func storeResourceEnvelopeTx(ctx context.Context, tx *sql.Tx, identity resourceRecordEnvelope, raw []byte, receivedAt time.Time, check bool) (bool, error) {
	hash := sourcefacts.Hash(raw)
	var conflict bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM resource_access_records
 WHERE source_system_id=$1 AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND record_ref=$5
 AND ((revision=$6::bigint-1 AND record_hash<>$7)
   OR (revision=$6::bigint+1 AND previous_hash<>$8)))`, identity.Reporter.SourceSystemID,
		identity.Reporter.SourceInstance, identity.Reporter.OrgID, identity.Reporter.ProjectID,
		identity.RecordRef, identity.Revision, identity.PreviousHash, hash).Scan(&conflict); err != nil {
		return false, err
	}
	if conflict {
		return false, workbench.ErrConflict
	}
	if check {
		var persisted string
		err := tx.QueryRowContext(ctx, `SELECT record_hash FROM resource_access_records WHERE source_system_id=$1 AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND record_ref=$5 AND revision=$6`, identity.Reporter.SourceSystemID, identity.Reporter.SourceInstance, identity.Reporter.OrgID, identity.Reporter.ProjectID, identity.RecordRef, identity.Revision).Scan(&persisted)
		if err == sql.ErrNoRows {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if persisted != hash {
			return false, workbench.ErrConflict
		}
		return false, nil
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO resource_access_records(
 source_system_id,record_ref,revision,previous_hash,record_hash,source_instance,org_id,project_id,
 target_source_system_id,target_source_instance,target_org_id,target_project_id,request_id,
 occurred_at,received_at,record_bytes,record_json)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17::jsonb)
 ON CONFLICT(source_system_id,source_instance,org_id,project_id,record_ref,revision) DO NOTHING`, identity.Reporter.SourceSystemID, identity.RecordRef, identity.Revision, identity.PreviousHash, hash,
		identity.Reporter.SourceInstance, identity.Reporter.OrgID, identity.Reporter.ProjectID, identity.Target.SourceSystemID,
		identity.Target.SourceInstance, identity.Target.OrgID, identity.Target.ProjectID, identity.RequestID, identity.OccurredAt.UTC().Truncate(time.Microsecond),
		receivedAt.UTC().Truncate(time.Microsecond), raw, string(raw))
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	var persisted string
	if err := tx.QueryRowContext(ctx, `SELECT record_hash FROM resource_access_records WHERE source_system_id=$1 AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND record_ref=$5 AND revision=$6`, identity.Reporter.SourceSystemID, identity.Reporter.SourceInstance, identity.Reporter.OrgID, identity.Reporter.ProjectID, identity.RecordRef, identity.Revision).Scan(&persisted); err != nil {
		return false, err
	}
	if persisted != hash {
		return false, workbench.ErrConflict
	}

	return count == 1, nil
}
