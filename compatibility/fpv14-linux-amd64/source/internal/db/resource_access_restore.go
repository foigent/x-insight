package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func validateResourceAccessRestore(ctx context.Context, q workspaceQueryer) error {
	var records, members, migrated bool
	if err := q.QueryRowContext(ctx, `SELECT to_regclass('public.resource_access_records') IS NOT NULL,
 to_regclass('public.runtime_reading_resource_members') IS NOT NULL,
 EXISTS(SELECT 1 FROM schema_migrations WHERE version='000035_add_resource_access_domain' AND NOT dirty)`).Scan(&records, &members, &migrated); err != nil {
		return err
	}
	if !records && !members && !migrated {
		return nil
	}
	if !records || !members || !migrated {
		return fmt.Errorf("restored resource schema and migration ledger disagree")
	}
	var valid bool
	err := q.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='public.resource_access_records'::regclass
 AND tgname='resource_access_records_immutable' AND tgenabled='A' AND NOT tgisinternal)
 AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='public.workbench_access_grants'::regclass
 AND tgname='workbench_resource_grant_revision' AND tgenabled='A' AND NOT tgisinternal)
 AND NOT EXISTS(SELECT 1 FROM resource_access_records d WHERE
 d.record_json IS DISTINCT FROM convert_from(d.record_bytes,'UTF8')::jsonb
 OR d.record_hash IS DISTINCT FROM 'sha256:'||encode(sha256(d.record_bytes),'hex')
 OR ROW(d.source_system_id::text,d.source_instance,d.org_id,d.project_id,d.record_ref,d.revision::text,d.previous_hash,
 d.target_source_system_id::text,d.target_source_instance,d.target_org_id,d.target_project_id,d.request_id)
 IS DISTINCT FROM ROW(d.record_json#>>'{reporter,source_system_id}',d.record_json#>>'{reporter,source_instance}',
 d.record_json#>>'{reporter,org_id}',d.record_json#>>'{reporter,project_id}',d.record_json->>'record_ref',
 d.record_json->>'revision',COALESCE(d.record_json->>'previous_hash',''),d.record_json#>>'{target,source_system_id}',
 d.record_json#>>'{target,source_instance}',d.record_json#>>'{target,org_id}',d.record_json#>>'{target,project_id}',d.record_json->>'request_id')
 OR EXISTS(SELECT 1 FROM resource_access_records p WHERE
 ROW(p.source_system_id,p.source_instance,p.org_id,p.project_id,p.record_ref)=
 ROW(d.source_system_id,d.source_instance,d.org_id,d.project_id,d.record_ref)
 AND p.revision=d.revision-1 AND p.record_hash<>d.previous_hash))`).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("restored resource guards, identity, hashes or revision links are invalid")
	}
	rows, err := q.QueryContext(ctx, `SELECT record_bytes,record_hash,occurred_at FROM resource_access_records
 ORDER BY source_system_id,source_instance,org_id,project_id,record_ref,revision`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var hash string
		var occurredAt time.Time
		if err = rows.Scan(&raw, &hash, &occurredAt); err != nil {
			return err
		}
		var version struct {
			Version string `json:"schema_version"`
		}
		if json.Unmarshal(raw, &version) != nil || sourcefacts.Hash(raw) != hash {
			return fmt.Errorf("restored resource record, bytes or time are invalid")
		}
		var at time.Time
		var e error
		switch version.Version {
		case workbench.ResourceAccessVersion:
			var record workbench.ResourceAccessRecord
			record, e = workbench.DecodeResourceAccess(raw)
			at = record.OccurredAt
		case workbench.ActionWitnessVersion:
			var record workbench.ActionWitness
			record, e = workbench.DecodeActionWitnessForImport(raw)
			at = record.OccurredAt
		default:
			e = workbench.ErrUnsupportedVersion
		}
		if e != nil || !at.UTC().Truncate(time.Microsecond).Equal(occurredAt) {
			return fmt.Errorf("restored resource record, bytes or time are invalid")
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	// Historical membership is checked against its frozen cutoff and identity,
	// never against today's grants. Current reads still recheck authorization.
	err = q.QueryRowContext(ctx, `WITH RECURSIVE lineage AS (
 SELECT m.reading_id,m.source_system_id,m.source_instance,m.org_id,m.project_id,m.record_ref,
 m.revision root_revision,d.revision,d.previous_hash,r.cutoff
 FROM runtime_reading_resource_members m JOIN runtime_readings r ON r.id=m.reading_id
 JOIN resource_access_records d USING(source_system_id,source_instance,org_id,project_id,record_ref,revision)
 UNION ALL SELECT l.reading_id,d.source_system_id,d.source_instance,d.org_id,d.project_id,d.record_ref,
 l.root_revision,d.revision,d.previous_hash,l.cutoff FROM lineage l JOIN resource_access_records d ON
 ROW(d.source_system_id,d.source_instance,d.org_id,d.project_id,d.record_ref)=
 ROW(l.source_system_id,l.source_instance,l.org_id,l.project_id,l.record_ref)
 AND d.revision=l.revision-1 AND d.record_hash=l.previous_hash AND d.received_at<=l.cutoff
)
SELECT NOT EXISTS(SELECT 1 FROM runtime_reading_resource_members m
 LEFT JOIN runtime_readings r ON r.id=m.reading_id
 LEFT JOIN runtime_reading_requests q ON q.reading_id=m.reading_id AND q.request_ref=m.request_ref
 LEFT JOIN resource_access_records d ON
 ROW(d.source_system_id,d.source_instance,d.org_id,d.project_id,d.record_ref,d.revision)=
 ROW(m.source_system_id,m.source_instance,m.org_id,m.project_id,m.record_ref,m.revision)
 WHERE r.id IS NULL OR q.request_ref IS NULL OR d.record_ref IS NULL OR d.received_at>r.cutoff
 OR ROW(q.source_system_id,q.source_instance,q.org_id,q.project_id,q.request_id)
 IS DISTINCT FROM ROW(d.target_source_system_id,d.target_source_instance,d.target_org_id,d.target_project_id,d.request_id)
 OR EXISTS(SELECT 1 FROM resource_access_records newer WHERE
 ROW(newer.source_system_id,newer.source_instance,newer.org_id,newer.project_id,newer.record_ref)=
 ROW(d.source_system_id,d.source_instance,d.org_id,d.project_id,d.record_ref)
 AND newer.revision>d.revision AND newer.received_at<=r.cutoff)
 OR m.chain_state IS DISTINCT FROM CASE WHEN EXISTS(SELECT 1 FROM lineage l WHERE
 ROW(l.reading_id,l.source_system_id,l.source_instance,l.org_id,l.project_id,l.record_ref,l.root_revision)=
 ROW(m.reading_id,m.source_system_id,m.source_instance,m.org_id,m.project_id,m.record_ref,m.revision)
 AND l.revision=1) THEN 'complete' ELSE 'missing_predecessor' END)`).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("restored resource membership, cutoff or lineage is invalid")
	}
	return nil
}
