package db

import (
	"context"
	"fmt"
)

// Stale labels caused by legacy edits are retained and remain invisible. A
// restore must preserve the classification schema and synchronization guard.
func validateWorkchainRestore(ctx context.Context, q workspaceQueryer) error {
	var migrated bool
	if err := q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version='000030_add_scoped_workchain' AND NOT dirty)").Scan(&migrated); err != nil {
		return err
	}
	if !migrated {
		return nil
	}
	var valid bool
	err := q.QueryRowContext(ctx, `SELECT to_regclass('public.investigation_case_scope_labels') IS NOT NULL AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='workbench_access_grants'::regclass AND tgname='workchain_grants_synchronize' AND tgenabled='A' AND (tgtype::int & 63)=62)`).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("restored scoped workchain guard is invalid")
	}
	err = q.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM investigation_case_scope_labels l LEFT JOIN investigation_cases c ON c.id=l.case_id WHERE c.id IS NULL OR l.schema_version NOT IN ('xinsight.case-scope.v1','xinsight.case-scope.v2','xinsight.case-scope.v3') OR l.case_version<1 OR l.content_digest !~ '^sha256:[a-f0-9]{64}$' OR jsonb_typeof(l.requirements)<>'array' OR jsonb_array_length(l.requirements)=0 OR EXISTS(SELECT 1 FROM jsonb_array_elements(l.requirements) r WHERE COALESCE(r->>'source','')='' OR r->'fields' IS NULL OR (r->'fields' IS DISTINCT FROM '{"context":true,"identity":true,"cost":true}'::jsonb AND NOT ((l.schema_version IN ('xinsight.case-scope.v2','xinsight.case-scope.v3') AND r->'fields' IN `+resourceRequirementFieldsSQL+`) OR (l.schema_version='xinsight.case-scope.v3' AND r->'fields'='{"context":false,"identity":false,"cost":false,"content":true}'::jsonb)))))`).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("restored scoped case classification is invalid")
	}
	return nil
}
