package db

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func validateBudgetsRestore(ctx context.Context, q workspaceQueryer) error {
	var present bool
	if err := q.QueryRowContext(ctx, `SELECT to_regclass('public.source_budget_collections') IS NOT NULL`).Scan(&present); err != nil {
		return err
	}
	if !present {
		return nil
	}
	var valid bool
	err := q.QueryRowContext(ctx, `SELECT
 (SELECT count(*)=6 FROM pg_trigger WHERE tgenabled='A' AND NOT tgisinternal AND (tgrelid,tgname) IN (
 ('workbench_access_grants'::regclass,'workbench_budget_grant_revision'),
 ('source_budget_collections'::regclass,'source_budget_collection_guard'),
 ('source_budget_catalogs'::regclass,'source_budget_catalogs_immutable'),
 ('source_budget_catalog_entries'::regclass,'source_budget_entries_immutable'),
 ('source_budget_readings'::regclass,'source_budget_readings_immutable'),
 ('source_budget_receipts'::regclass,'source_budget_receipts_immutable')))
 AND NOT EXISTS(SELECT 1 FROM source_budget_collections c LEFT JOIN source_budget_catalogs p ON p.id=c.catalog_id WHERE
 NOT EXISTS(SELECT 1 FROM source_fact_management_events e WHERE e.registration_kind='budget' AND e.registration_id=c.id::text AND e.action='register')
 OR c.catalog_id IS NOT NULL AND (p.collection_id<>c.id OR p.cycle<>c.cycle OR c.entry_index>=jsonb_array_length(convert_from(p.catalog_bytes,'UTF8')::jsonb->'entries')))
 AND NOT EXISTS(SELECT 1 FROM source_budget_catalog_entries e JOIN source_budget_catalogs c ON c.id=e.catalog_id
 WHERE e.collection_id<>c.collection_id OR e.cycle<>c.cycle OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(convert_from(c.catalog_bytes,'UTF8')::jsonb->'entries') v
 WHERE v->>'budget_ref'=e.budget_ref AND v->>'origin'=e.origin AND v->>'definition_revision'=e.definition_revision AND COALESCE(v->>'binding_revision','')=e.binding_revision))
 AND NOT EXISTS(SELECT 1 FROM source_budget_catalogs c WHERE
 (SELECT count(*) FROM source_budget_catalog_entries e WHERE e.catalog_id=c.id)<>jsonb_array_length(convert_from(c.catalog_bytes,'UTF8')::jsonb->'entries'))
 AND NOT EXISTS(SELECT 1 FROM source_budget_readings r WHERE
 NOT (convert_from(r.reading_bytes,'UTF8')::jsonb @> jsonb_build_object('source_instance',r.source_instance,'org_id',r.org_id,'project_id',r.project_id,'reading_id',r.reading_id,'budget_ref',r.budget_ref,'definition_revision',r.definition_revision,'access_generation',r.access_generation,'catalog_revision',r.catalog_revision))
 OR COALESCE(convert_from(r.reading_bytes,'UTF8')::jsonb->>'binding_revision','')<>r.binding_revision
 OR (convert_from(r.reading_bytes,'UTF8')::jsonb->>'reading_cutoff')::timestamptz<>r.reading_cutoff
 OR (convert_from(r.reading_bytes,'UTF8')::jsonb->>'observed_at')::timestamptz<>r.observed_at
 OR NOT (r.profiles @> ARRAY(SELECT v->>'profile' FROM jsonb_array_elements(convert_from(r.reading_bytes,'UTF8')::jsonb->'documents') v)
 AND r.profiles <@ ARRAY(SELECT v->>'profile' FROM jsonb_array_elements(convert_from(r.reading_bytes,'UTF8')::jsonb->'documents') v))
 OR NOT EXISTS(SELECT 1 FROM source_budget_receipts p WHERE p.source_reading_id=r.id))
 AND NOT EXISTS(SELECT 1 FROM source_budget_receipts p JOIN source_budget_catalogs c ON c.id=p.catalog_id WHERE c.collection_id<>p.collection_id)`).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("restored budget guards, source bindings or catalog membership are invalid")
	}
	err = q.QueryRowContext(ctx, `SELECT
 (SELECT count(*)=2 FROM pg_trigger WHERE tgenabled='A' AND NOT tgisinternal AND (tgrelid,tgname) IN (
 ('budget_readings'::regclass,'budget_readings_seal'),('budget_reading_members'::regclass,'budget_reading_members_immutable')))
 AND NOT EXISTS(SELECT 1 FROM budget_readings h WHERE NOT h.sealed OR h.contract_version<>$1 OR h.interpretation_revision<>$2
 OR h.budget_count<>(SELECT count(*) FROM budget_reading_members m WHERE m.reading_id=h.id)
 OR h.membership_sha256 IS DISTINCT FROM (`+strings.ReplaceAll(budgetManifestSQL, "$1", "h.id")+`))
 AND NOT EXISTS(SELECT 1 FROM budget_reading_members m JOIN budget_readings h ON h.id=m.reading_id
 JOIN source_budget_readings r ON r.id=m.source_reading_id
 JOIN source_budget_receipts p ON p.collection_id=m.collection_id AND p.source_reading_id=m.source_reading_id AND p.catalog_id=m.receipt_catalog_id
 WHERE r.received_at>h.cutoff OR p.received_at>h.cutoff OR NOT m.profiles<@r.profiles)
 AND NOT EXISTS(SELECT 1 FROM (SELECT ordinal,row_number() OVER(PARTITION BY reading_id ORDER BY ordinal) expected FROM budget_reading_members) p WHERE p.ordinal<>p.expected)`, workbench.BudgetContractVersion, digestText(workbench.BudgetInterpretation)).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("restored frozen budget reading is invalid")
	}
	rows, err := q.QueryContext(ctx, `SELECT p.catalog_bytes,p.catalog_sha256,c.source_instance,c.org_id,c.project_id,array_to_json(c.profiles) FROM source_budget_catalogs p JOIN source_budget_collections c ON c.id=p.collection_id ORDER BY p.id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var raw, configured []byte
		var hash, instance, org, project string
		if err = rows.Scan(&raw, &hash, &instance, &org, &project, &configured); err != nil {
			rows.Close()
			return err
		}
		catalog, e := sourcefacts.DecodeBudgetCatalog(raw)
		var profiles []string
		scopeValid := json.Unmarshal(configured, &profiles) == nil && catalog.SourceInstance == instance && catalog.OrgID == org && catalog.ProjectID == project
		for _, profile := range catalog.Profiles {
			if !slices.Contains(profiles, profile) {
				scopeValid = false
			}
		}
		m, scan := connector.DecodeStrictJSONObject(raw)
		canonical, enc := connector.CanonicalizeJSONValue(m)
		if !scopeValid || e != nil || scan != nil || enc != nil || !bytes.Equal(canonical, raw) || sourcefacts.Hash(raw) != hash {
			rows.Close()
			return fmt.Errorf("restored budget catalog bytes are invalid")
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = q.QueryContext(ctx, `SELECT r.reading_bytes,r.reading_sha256,c.catalog_bytes,b.source_system_id::text,r.source_system_id::text,b.source_instance,b.org_id,b.project_id,array_to_json(b.profiles)
 FROM source_budget_receipts p JOIN source_budget_readings r ON r.id=p.source_reading_id
 JOIN source_budget_catalogs c ON c.id=p.catalog_id JOIN source_budget_collections b ON b.id=p.collection_id
 WHERE c.collection_id=p.collection_id ORDER BY r.id,c.id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw, catalog, profileRaw []byte
		var hash, registeredSource, receivedSource, instance, org, project string
		if err = rows.Scan(&raw, &hash, &catalog, &registeredSource, &receivedSource, &instance, &org, &project, &profileRaw); err != nil {
			return err
		}
		r, e := sourcefacts.DecodeBudgetReading(raw)
		c, ce := sourcefacts.DecodeBudgetCatalog(catalog)
		var profiles []string
		if json.Unmarshal(profileRaw, &profiles) != nil || e != nil || ce != nil || r.SHA256 != hash || !bytes.Equal(raw, r.Canonical) || registeredSource != receivedSource || r.SourceInstance != instance || r.OrgID != org || r.ProjectID != project {
			return fmt.Errorf("restored budget source reading is invalid")
		}
		found := false
		for _, entry := range c.Entries {
			if entry.BudgetRef == r.BudgetRef {
				found = sourcefacts.MatchBudgetReading(r, c, entry, profiles) == nil
				break
			}
		}
		if !found {
			return fmt.Errorf("restored budget receipt crosses source authorization or catalog versions")
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	return validateBudgetOfflineRestore(ctx, q)
}

func suspendRestoredBudgets(ctx context.Context, tx *sql.Tx) error {
	var present bool
	if err := tx.QueryRowContext(ctx, `SELECT to_regclass('public.source_budget_collections') IS NOT NULL`).Scan(&present); err != nil {
		return err
	}
	if !present {
		return nil
	}
	if err := validateBudgetsRestore(ctx, tx); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `WITH changed AS (
 UPDATE source_budget_collections SET status=CASE WHEN status='revoked' THEN status ELSE 'paused' END,
 revision=revision+1,requires_attention=true,last_error='restore_requires_refresh',visible_cycle=0,
 catalog_id=NULL,catalog_cursor='',entry_index=0,alerts_cursor='',last_success_at=NULL
 RETURNING id,revision,status)
 INSERT INTO source_fact_management_events(registration_kind,registration_id,revision,action,status,actor)
 SELECT 'budget',id::text,revision,'state_change',status,current_user FROM changed`)
	return err
}
