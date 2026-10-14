package db

import (
	"context"
	"fmt"
)

// ValidateWorkbenchRestore checks persisted identity bindings before a restored
// database is returned to the host controller. It does not rebuild facts or
// reinterpret readings. Pre-A0 backups retain their historical restore path.
func ValidateWorkbenchRestore(ctx context.Context, database workspaceQueryer) error {
	if err := ValidateSourceContentsRestore(ctx, database); err != nil {
		return err
	}
	if err := validateContentGrantRestore(ctx, database); err != nil {
		return err
	}
	if err := validateBudgetsRestore(ctx, database); err != nil {
		return err
	}
	if err := validateResourceAccessRestore(ctx, database); err != nil {
		return err
	}
	if err := validateManagedSourceFactsRestore(ctx, database); err != nil {
		return err
	}
	if err := validateSourceObservationsRestore(ctx, database); err != nil {
		return err
	}
	var frozen bool
	if err := database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version='000025_freeze_workbench_facts' AND NOT dirty)`).Scan(&frozen); err != nil {
		return err
	}
	if !frozen {
		return nil
	}
	var valid bool
	var sourceSchema bool
	if err := database.QueryRowContext(ctx, `SELECT to_regclass('public.workbench_source_manifests') IS NOT NULL`).Scan(&sourceSchema); err != nil {
		return err
	}
	interpretationCheck := "r.interpretation_revision<>$1"
	args := []any{digestText(workbenchInterpretationA0)}
	if sourceSchema {
		interpretationCheck = "r.interpretation_revision NOT IN ($1,$2)"
		args = append(args, digestText(workbenchInterpretationA1))
	}
	err := database.QueryRowContext(ctx, `
 SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='workbench_economic_facts'::regclass AND tgname='workbench_economic_facts_immutable' AND tgenabled='A')
 AND NOT EXISTS (
  SELECT 1 FROM workbench_economic_facts f LEFT JOIN evidence_events e ON e.id=f.evidence_event_id
  WHERE e.id IS NULL OR f.event_hash IS DISTINCT FROM e.event_hash
 )
 AND NOT EXISTS (
  SELECT 1 FROM workbench_reading_members m LEFT JOIN workbench_economic_facts f ON f.id=m.economic_fact_id
  LEFT JOIN workbench_readings r ON r.id=m.reading_id WHERE f.id IS NULL OR r.id IS NULL
 )
 AND NOT EXISTS (
  SELECT 1 FROM workbench_readings r WHERE `+interpretationCheck+`
  OR (r.coverage->>'known_items')::bigint IS DISTINCT FROM (SELECT count(*) FROM workbench_reading_members m WHERE m.reading_id=r.id)
  OR r.membership_sha256 IS DISTINCT FROM (
   SELECT 'sha256:'||encode(digest(convert_to(COALESCE(string_agg(m.economic_fact_id::text||':'||f.source_event_hash||':'||f.event_hash,E'\n' ORDER BY m.ordinal),''),'UTF8'),'sha256'),'hex')
   FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id WHERE m.reading_id=r.id
  )
 )
 AND NOT EXISTS (
  SELECT 1 FROM (SELECT ordinal,row_number() OVER (PARTITION BY reading_id ORDER BY f.occurred_at,f.id) expected
   FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id) ordered WHERE ordinal<>expected
 )`, args...).Scan(&valid)
	if err != nil {
		return fmt.Errorf("validate restored workbench bindings: %w", err)
	}
	if !valid {
		return fmt.Errorf("restored workbench identities, interpretation, membership or immutable guard are invalid")
	}
	if sourceSchema {
		if err := validateSourceFactsRestore(ctx, database); err != nil {
			return err
		}
	}
	return validateWorkbenchPurposeRestore(ctx, database)
}
