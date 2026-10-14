package db

import (
	"bytes"
	"context"
	"fmt"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

func validateSourceObservationsRestore(ctx context.Context, q workspaceQueryer) error {
	var present, migrated bool
	if err := q.QueryRowContext(ctx, `SELECT to_regclass('public.source_observation_documents') IS NOT NULL,
 EXISTS(SELECT 1 FROM schema_migrations WHERE version='000033_add_source_observations' AND NOT dirty)`).Scan(&present, &migrated); err != nil {
		return err
	}
	if !present && !migrated {
		return nil
	}
	if present != migrated {
		return fmt.Errorf("restored source observation schema and migration ledger disagree")
	}
	var valid bool
	err := q.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='public.source_observation_documents'::regclass
   AND tgname='source_observation_documents_immutable' AND tgenabled='A' AND NOT tgisinternal)
 AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='public.workbench_access_grants'::regclass
   AND tgname='workbench_observation_grant_revision' AND tgenabled='A' AND NOT tgisinternal)
 AND NOT EXISTS(SELECT 1 FROM source_observation_documents d WHERE
   d.source_json IS DISTINCT FROM convert_from(d.source_bytes,'UTF8')::jsonb
   OR NOT (d.source_json @> jsonb_build_object('schema_version','unirouter.source-observation.v1',
     'source_instance',d.source_instance,'org_id',d.org_id,'project_id',d.project_id,'request_id',d.request_id,
     'profile',d.profile,'subject_kind',d.subject_kind,'subject_id',d.subject_id,'observation_kind',d.observation_kind,
     'source_fact_id',d.source_fact_id,'revision',d.revision::text,'event_id',d.event_id))
   OR coalesce(d.source_json->>'previous_hash','') IS DISTINCT FROM d.previous_hash
   OR (d.source_json->>'occurred_at')::timestamptz IS DISTINCT FROM d.occurred_at
   OR (d.source_json->>'recorded_at')::timestamptz IS DISTINCT FROM d.recorded_at
   OR d.source_hash IS DISTINCT FROM 'sha256:'||encode(sha256(d.source_bytes),'hex')
   OR EXISTS(SELECT 1 FROM source_observation_documents p WHERE p.source_system_id=d.source_system_id
     AND p.source_fact_id=d.source_fact_id AND p.revision=d.revision-1 AND p.source_hash<>d.previous_hash))`).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("restored source observation guards, bindings or revision links are invalid")
	}
	rows, err := q.QueryContext(ctx, `SELECT profile,source_bytes,envelope_bytes,source_hash,envelope_hash FROM source_observation_documents ORDER BY source_system_id,event_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var profile, sourceHash, envelopeHash string
		var source, envelope []byte
		if err := rows.Scan(&profile, &source, &envelope, &sourceHash, &envelopeHash); err != nil {
			return err
		}
		item, err := sourcefacts.ValidateObservationItem(envelope, profile)
		if err != nil || !bytes.Equal(item.SourceBytes, source) || !bytes.Equal(item.EnvelopeBytes, envelope) || item.SourceHash != sourceHash || item.EnvelopeHash != envelopeHash {
			return fmt.Errorf("restored source observation bytes or fixed projection are invalid")
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err = validateRuntimeReadingsRestore(ctx, q); err != nil {
		return err
	}
	return validateRuntimeCurrentRestore(ctx, q)
}
