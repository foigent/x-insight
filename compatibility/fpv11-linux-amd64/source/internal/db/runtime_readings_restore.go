package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/foigent/x-insight/internal/workbench"
)

func validateRuntimeReadingsRestore(ctx context.Context, q workspaceQueryer) error {
	var present bool
	if err := q.QueryRowContext(ctx, `SELECT to_regclass('public.runtime_readings') IS NOT NULL AND to_regclass('public.runtime_reading_requests') IS NOT NULL AND to_regclass('public.runtime_reading_members') IS NOT NULL AND to_regclass('public.runtime_reading_source_members') IS NOT NULL AND to_regclass('public.runtime_reading_economic_members') IS NOT NULL`).Scan(&present); err != nil {
		return err
	}
	if !present {
		return fmt.Errorf("restored runtime reading tables are missing")
	}
	var resourcesPresent bool
	if err := q.QueryRowContext(ctx, `SELECT to_regclass('public.resource_access_records') IS NOT NULL`).Scan(&resourcesPresent); err != nil {
		return err
	}
	manifestSQL := runtimeManifestDigestBeforeResourcesSQL
	interpretation := runtimeInterpretationBeforeResources
	if resourcesPresent {
		manifestSQL = runtimeManifestDigestSQL
		interpretation = runtimeInterpretation
	}
	contentPresent, err := runtimeContentTablesPresent(ctx, q)
	if err != nil {
		return err
	}
	thirdInterpretation := digestText(interpretation)
	fourthInterpretation := thirdInterpretation
	orderExpr := "COALESCE(entry_at,last_observed_at)"
	if contentPresent {
		manifestSQL = runtimeManifestDigestWithContentReceiptsSQL
		thirdInterpretation = digestText(runtimeInterpretationWithContent)
		fourthInterpretation = digestText(runtimeInterpretationWithContentReceipts)
		orderExpr = "COALESCE(entry_at,last_observed_at,content_eligible_at)"
		if err := validateRuntimeContentsRestore(ctx, q); err != nil {
			return err
		}
	}
	manifest := strings.Replace(manifestSQL, "WHERE r.id=$1", "WHERE r.id=outer_r.id", 1)
	var valid bool
	err = q.QueryRowContext(ctx, `SELECT NOT EXISTS(
 SELECT 1 FROM runtime_readings outer_r WHERE outer_r.contract_version<>$1 OR outer_r.metric_version<>$2 OR outer_r.interpretation_revision NOT IN ($3,$4,$5,$6)
 OR outer_r.known_entry_requests<>(SELECT count(*) FROM runtime_reading_requests x WHERE x.reading_id=outer_r.id AND x.entry_at IS NOT NULL AND x.observation_count>0)
 OR outer_r.unknown_entry_requests<>(SELECT count(*) FROM runtime_reading_requests x WHERE x.reading_id=outer_r.id AND x.entry_at IS NULL AND x.observation_count>0)
 OR outer_r.observation_count<>(SELECT count(*) FROM runtime_reading_members x WHERE x.reading_id=outer_r.id)
 OR outer_r.membership_sha256 IS DISTINCT FROM (`+manifest+`)
 ) AND NOT EXISTS(
 SELECT 1 FROM runtime_reading_members m LEFT JOIN runtime_readings r ON r.id=m.reading_id
 LEFT JOIN runtime_reading_requests x ON x.reading_id=m.reading_id AND x.request_ref=m.request_ref
 LEFT JOIN source_observation_documents d ON d.source_system_id=m.source_system_id AND d.event_id=m.event_id
 WHERE r.id IS NULL OR x.request_ref IS NULL OR d.event_id IS NULL OR d.received_at>r.cutoff
 OR ROW(x.source_system_id,x.source_instance,x.org_id,x.project_id,x.request_id)
 IS DISTINCT FROM ROW(d.source_system_id,d.source_instance,d.org_id,d.project_id,d.request_id)
 ) AND NOT EXISTS(
 SELECT 1 FROM runtime_reading_source_members m LEFT JOIN runtime_readings r ON r.id=m.reading_id
 LEFT JOIN runtime_reading_requests x ON x.reading_id=m.reading_id AND x.request_ref=m.request_ref
 LEFT JOIN source_fact_documents d ON d.source_system_id=m.source_system_id AND d.event_id=m.event_id
 WHERE r.id IS NULL OR x.request_ref IS NULL OR d.event_id IS NULL OR d.received_at>r.cutoff
 OR ROW(x.source_system_id,x.source_instance,x.org_id,x.project_id,x.request_id)
 IS DISTINCT FROM ROW(d.source_system_id,d.source_instance,d.org_id,d.project_id,d.request_id)
 ) AND NOT EXISTS(
 SELECT 1 FROM runtime_reading_economic_members m LEFT JOIN runtime_readings r ON r.id=m.reading_id
 LEFT JOIN runtime_reading_requests d ON d.reading_id=m.reading_id AND d.request_ref=m.request_ref
 LEFT JOIN workbench_economic_facts f ON f.id=m.economic_fact_id
 LEFT JOIN evidence_events e ON e.id=f.evidence_event_id
 WHERE r.id IS NULL OR d.request_ref IS NULL OR f.id IS NULL OR e.id IS NULL
 OR f.projected_at>r.cutoff OR e.ingested_at>r.cutoff
 OR NOT (`+sourceEconomicJoinSQL+`)
 ) AND NOT EXISTS(
 SELECT 1 FROM runtime_reading_requests x JOIN runtime_readings r ON r.id=x.reading_id
 WHERE x.request_ref IS DISTINCT FROM 'xrr_'||encode(sha256(convert_to(jsonb_build_array(x.source_system_id::text,x.source_instance,x.org_id,x.project_id,x.request_id)::text,'UTF8')),'hex')
 OR x.observation_count<>(SELECT count(*) FROM runtime_reading_members m WHERE m.reading_id=x.reading_id AND m.request_ref=x.request_ref)
 OR x.entry_at IS DISTINCT FROM (SELECT min(d.occurred_at) FROM runtime_reading_members m JOIN source_observation_documents d USING(source_system_id,event_id)
 WHERE m.reading_id=x.reading_id AND m.request_ref=x.request_ref AND d.profile='unirouter.runtime-observation.v1' AND d.subject_kind='request' AND d.observation_kind='request_started' AND m.chain_state='complete')
 OR (x.observation_count>0 AND x.last_observed_at IS DISTINCT FROM (SELECT max(d.occurred_at) FROM runtime_reading_members m JOIN source_observation_documents d USING(source_system_id,event_id) WHERE m.reading_id=x.reading_id AND m.request_ref=x.request_ref))
 OR (x.entry_at IS NOT NULL AND NOT(x.entry_at>=r.period_start AND x.entry_at<r.period_end))
 OR (x.entry_at IS NULL AND x.observation_count>0 AND NOT EXISTS(SELECT 1 FROM runtime_reading_members m JOIN source_observation_documents d USING(source_system_id,event_id)
 WHERE m.reading_id=x.reading_id AND m.request_ref=x.request_ref AND d.occurred_at>=r.period_start AND d.occurred_at<r.period_end))
 ) AND NOT EXISTS(
 SELECT 1 FROM (SELECT ordinal,row_number() OVER(PARTITION BY reading_id ORDER BY (observation_count=0),`+orderExpr+` DESC,source_system_id,source_instance,org_id,project_id,request_id) expected FROM runtime_reading_requests) x WHERE ordinal<>expected
 )`, workbench.RuntimeContractVersion, workbench.RuntimeMetricVersion, digestText(interpretation), digestText(runtimeInterpretationBeforeResources), thirdInterpretation, fourthInterpretation).Scan(&valid)
	if err != nil {
		return fmt.Errorf("validate restored runtime reading: %w", err)
	}
	if !valid {
		return fmt.Errorf("restored runtime reading membership, times or hashes are invalid")
	}
	return nil
}
