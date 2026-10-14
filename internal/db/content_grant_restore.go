package db

import (
	"context"
	"fmt"
)

func validateContentGrantRestore(ctx context.Context, q workspaceQueryer) error {
	var columns int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='workbench_access_grants' AND column_name IN ('allow_content','allowed_content_purposes','legacy_revision','content_revision')`).Scan(&columns); err != nil {
		return err
	}
	if columns == 0 {
		var content bool
		if err := q.QueryRowContext(ctx, `SELECT to_regclass('public.source_content_collections') IS NOT NULL`).Scan(&content); err != nil {
			return err
		}
		if content {
			return fmt.Errorf("restored content grant columns are absent")
		}
		return nil
	}
	if columns != 4 {
		return fmt.Errorf("restored content grant columns are incomplete")
	}
	var valid bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='public.workbench_access_grants'::regclass AND tgname='workbench_content_grant_domains' AND tgtype=23 AND tgenabled='A' AND NOT tgisinternal) AND NOT EXISTS(SELECT 1 FROM workbench_access_grants WHERE legacy_revision<1 OR legacy_revision>revision OR content_revision<0 OR content_revision>revision OR (allow_content AND (content_revision<1 OR allowed_content_purposes<>'["human_review"]'::jsonb)) OR (NOT allow_content AND allowed_content_purposes<>'[]'::jsonb))`).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("restored content grant domains or guard are invalid")
	}
	return nil
}
