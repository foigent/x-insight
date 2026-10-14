package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

const workbenchInterpretationA1 = "xinsight.source-reading.v1"

func sourceProfiles(auth workbench.Authorization, filters workbench.Filters) []string {
	set := map[string]bool{}
	for _, g := range auth.Grants {
		if !g.AllowSummary || g.CostProject != "" || len(filters.CostProjects) > 0 ||
			(len(filters.SourceIDs) > 0 && !slices.Contains(filters.SourceIDs, g.SourceSystemID)) ||
			(g.SourceProject != "" && len(filters.SourceProjects) > 0 && !slices.Contains(filters.SourceProjects, g.SourceProject)) {
			continue
		}
		if g.AllowSourceContext {
			set[sourcefacts.ContextProfile] = true
		}
		if g.AllowSourceIdentity {
			set[sourcefacts.IdentityProfile] = true
		}
		if g.AllowSourceCost {
			set[sourcefacts.CostProfile] = true
		}
	}
	result := []string{}
	for _, p := range []string{sourcefacts.ContextProfile, sourcefacts.IdentityProfile, sourcefacts.CostProfile} {
		if set[p] {
			result = append(result, p)
		}
	}
	return result
}

const sourceProfileGrantSQL = `((d.profile='unirouter.execution-context.v1' AND g.allow_source_context) OR (d.profile='unirouter.execution-identity.v1' AND g.allow_source_identity) OR (d.profile='unirouter.cost-basis.v1' AND g.allow_source_cost))`

// The economic identity is the existing I-01 source identity plus its actual
// scoped Evidence anchor. A profile revision is never the economic revision.
const sourceEconomicJoinSQL = `f.source_system_id=d.source_system_id AND f.source_version='unirouter.completed-execution.v1'
 AND f.source_ref='unirouter:'||d.source_instance||':completed_execution:'||d.request_id
 AND f.request_id=d.request_id AND f.source_project_ref='project-'||d.project_id
 AND EXISTS(SELECT 1 FROM evidence_events e WHERE e.id=f.evidence_event_id AND e.org_ref='org-'||d.org_id)`

const sourceManifestDigestSQL = `SELECT 'sha256:'||encode(sha256(convert_to($2||E'\n'||$3||E'\n'||($4::jsonb)::text||E'\n'||COALESCE(string_agg(
 sm.source_system_id::text||':'||d.event_id||':'||d.source_hash||':'||d.envelope_hash||':'||sm.chain_state||':'||sm.economic_state||':'||COALESCE(sm.economic_fact_id::text,''),E'\n' ORDER BY sm.source_system_id,d.event_id),''),'UTF8')),'hex')
 FROM workbench_source_members sm JOIN source_fact_documents d USING(source_system_id,event_id) WHERE sm.reading_id=$1`

func freezeWorkbenchSources(ctx context.Context, tx *sql.Tx, id, subject string, auth workbench.Authorization, filters workbench.Filters, period workbench.Period, cutoff time.Time) (string, *workbench.SourceCoverage, error) {
	profiles := sourceProfiles(auth, filters)
	if len(profiles) == 0 {
		return "", nil, nil
	}
	// Latest is chosen before period filtering; a correction cannot make an
	// older revision silently current. Only a new reading observes late input.
	query := `WITH RECURSIVE latest AS (
 SELECT DISTINCT ON(d.source_system_id,d.source_fact_id) d.* FROM source_fact_documents d
 WHERE d.received_at<=$5 AND (cardinality($6::text[])=0 OR d.source_system_id::text=ANY($6::text[]))
 AND (cardinality($7::text[])=0 OR 'project-'||d.project_id=ANY($7::text[]))
 AND cardinality($8::text[])=0
 AND EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$2 AND g.active AND g.allow_summary
 AND g.source_system_id=d.source_system_id AND (g.source_project_ref IS NULL OR g.source_project_ref='project-'||d.project_id)
 AND g.cost_project_ref IS NULL AND ` + sourceProfileGrantSQL + `)
 ORDER BY d.source_system_id,d.source_fact_id,d.revision DESC
 ), lineage AS (
 SELECT d.source_system_id,d.source_fact_id,d.event_id root_event,d.revision,d.previous_hash FROM latest d
 UNION ALL SELECT d.source_system_id,d.source_fact_id,l.root_event,d.revision,d.previous_hash
 FROM lineage l JOIN source_fact_documents d ON d.source_system_id=l.source_system_id AND d.source_fact_id=l.source_fact_id
 AND d.revision=l.revision-1 AND d.source_hash=l.previous_hash AND d.received_at<=$5
 ), selected AS (
 SELECT d.*,f.id fact_id,f.source_event_hash fact_hash,
 CASE WHEN EXISTS(SELECT 1 FROM lineage l WHERE l.source_system_id=d.source_system_id AND l.root_event=d.event_id AND l.revision=1) THEN 'complete' ELSE 'missing_predecessor' END chain
 FROM latest d LEFT JOIN workbench_economic_facts f ON ` + sourceEconomicJoinSQL + `
 WHERE (d.occurred_at>=$3 AND d.occurred_at<$4) OR EXISTS(SELECT 1 FROM workbench_reading_members m WHERE m.reading_id=$1 AND m.economic_fact_id=f.id)
 ), classified AS (
 SELECT d.*, CASE
 WHEN d.profile='unirouter.cost-basis.v1' AND d.source_json#>>'{cost,economic_reference,source}'='async_media_charge_fact'
 THEN CASE WHEN d.source_json#>>'{cost,media_amount_state}'='not_eligible' THEN 'media_not_eligible' ELSE 'media_pricing_pending' END
 WHEN d.fact_id IS NULL THEN 'not_received'
 WHEN d.profile='unirouter.cost-basis.v1' AND (d.source_json#>>'{cost,economic_reference,revision}'<>'1' OR d.source_json#>>'{cost,economic_reference,snapshot_sha256}'<>d.fact_hash) THEN 'hash_mismatch'
 ELSE 'received' END economic FROM selected d
 )
 INSERT INTO workbench_source_members(reading_id,source_system_id,event_id,chain_state,economic_state,economic_fact_id)
 SELECT $1,source_system_id,event_id,chain,economic,CASE WHEN economic='received' THEN fact_id ELSE NULL END FROM classified`
	result, err := tx.ExecContext(ctx, query, id, subject, period.StartInclusive, period.EndExclusive, cutoff, pgTextArray(filters.SourceIDs), pgTextArray(filters.SourceProjects), pgTextArray(filters.CostProjects))
	if err != nil {
		return "", nil, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return "", nil, err
	}
	if n > workbench.MaxMembers {
		return "", nil, workbench.ErrTooLarge
	}
	coverage := &workbench.SourceCoverage{Profiles: profiles, Gaps: []string{"forward_only_capture", "timeline_completeness_unknown"}}
	err = tx.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE sm.chain_state='missing_predecessor'),count(*) FILTER(WHERE sm.economic_state='not_received'),count(*) FILTER(WHERE sm.economic_state='hash_mismatch'),count(*) FILTER(WHERE sm.economic_state IN ('media_pricing_pending','media_not_eligible')),count(*) FILTER(WHERE d.source_json->>'capture_status'='not_captured') FROM workbench_source_members sm JOIN source_fact_documents d USING(source_system_id,event_id) WHERE sm.reading_id=$1`, id).Scan(&coverage.KnownDocuments, &coverage.PendingPredecessor, &coverage.EconomicNotReceived, &coverage.EconomicHashConflicts, &coverage.MediaPending, &coverage.NotCaptured)
	if err != nil {
		return "", nil, err
	}
	// Registration state and failures are observations, not reader grants.
	// Freeze only authorized scopes, including paused/revoked registrations;
	// collection revocation does not erase legitimately readable history.
	// Historical pre-managed schemas have no attention column. This read-only
	// observation preserves inspection of their migration fixtures and readings.
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT s.status,s.last_error,COALESCE((to_jsonb(s)->>'requires_attention')::boolean,false) FROM source_fact_streams s WHERE true
 AND (cardinality($2::text[])=0 OR s.source_system_id::text=ANY($2::text[]))
 AND (cardinality($3::text[])=0 OR 'project-'||s.project_id=ANY($3::text[]))
 AND EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$1 AND g.active AND g.allow_summary
 AND g.source_system_id=s.source_system_id AND (g.source_project_ref IS NULL OR g.source_project_ref='project-'||s.project_id) AND g.cost_project_ref IS NULL
	 AND ((s.profile='unirouter.execution-context.v1' AND g.allow_source_context) OR (s.profile='unirouter.execution-identity.v1' AND g.allow_source_identity) OR (s.profile='unirouter.cost-basis.v1' AND g.allow_source_cost))) ORDER BY 1,2,3`, subject, pgTextArray(filters.SourceIDs), pgTextArray(filters.SourceProjects))
	if err != nil {
		return "", nil, err
	}
	for rows.Next() {
		var status, code string
		var attention bool
		if err = rows.Scan(&status, &code, &attention); err != nil {
			rows.Close()
			return "", nil, err
		}
		coverage.RequiresAttention = coverage.RequiresAttention || attention
		if !slices.Contains(coverage.CollectionStates, status) {
			coverage.CollectionStates = append(coverage.CollectionStates, status)
		}
		if code != "" && !slices.Contains(coverage.Gaps, code) {
			coverage.Gaps = append(coverage.Gaps, code)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", nil, err
	}
	raw, err := json.Marshal(coverage)
	if err != nil {
		return "", nil, err
	}
	var digest string
	if err = tx.QueryRowContext(ctx, sourceManifestDigestSQL, id, workbenchInterpretationA1, auth.SourceRevision, string(raw)).Scan(&digest); err != nil {
		return "", nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO workbench_source_manifests(reading_id,rule_version,authorization_sha256,coverage,manifest_sha256) VALUES($1,$2,$3,$4::jsonb,$5)`, id, workbenchInterpretationA1, auth.SourceRevision, string(raw), digest)
	return digest, coverage, err
}

func (s *WorkspaceStore) loadWorkbenchSourceManifest(ctx context.Context, reading *workbench.Reading, auth workbench.Authorization) error {
	return loadWorkbenchSourceManifest(ctx, s.db, reading, auth)
}

func loadWorkbenchSourceManifest(ctx context.Context, q workspaceQueryer, reading *workbench.Reading, auth workbench.Authorization) error {
	var rule, authorization string
	var raw []byte
	err := q.QueryRowContext(ctx, `SELECT rule_version,authorization_sha256,coverage,manifest_sha256 FROM workbench_source_manifests WHERE reading_id=$1`, reading.ID).Scan(&rule, &authorization, &raw, &reading.SourceManifestRef)
	if err != nil {
		return err
	}
	if rule != workbenchInterpretationA1 {
		return workbench.ErrUnsupportedVersion
	}
	if authorization != auth.SourceRevision {
		return workbench.ErrAccessChanged
	}
	var coverage workbench.SourceCoverage
	if err = json.Unmarshal(raw, &coverage); err != nil {
		return err
	}
	reading.SourceCoverage = &coverage
	if slices.Contains(coverage.Profiles, sourcefacts.ContextProfile) {
		reading.Dimensions = append(reading.Dimensions, "application")
	}
	if slices.Contains(coverage.Profiles, sourcefacts.IdentityProfile) {
		reading.Dimensions = append(reading.Dimensions, "account")
	}
	return nil
}

// Source dimensions use the same immutable selected members as source details.
// Pending predecessors are visible in coverage but never adopted as dimensions.
func workbenchDimensionSQL(dimension string) string {
	if dimension == "purpose" {
		return purposeDimensionSQL()
	}
	if c := map[string]string{"model": "model_ref", "source_project": "source_project_ref", "cost_project": "cost_project_ref"}[dimension]; c != "" {
		return "f." + c
	}
	profile, path := "", ""
	switch dimension {
	case "application":
		profile = sourcefacts.ContextProfile
		path = "{core,workload_application_id}"
	case "account":
		profile = sourcefacts.IdentityProfile
		path = "{identity,owner_user_id}"
	default:
		return ""
	}
	return fmt.Sprintf(`(SELECT CASE WHEN sm.chain_state='complete' THEN d.source_json#>>'%s' END FROM workbench_source_members sm JOIN source_fact_documents d USING(source_system_id,event_id)
 WHERE sm.reading_id=m.reading_id AND sm.economic_fact_id=f.id AND d.profile='%s'
 ORDER BY CASE d.stage WHEN 'terminal' THEN 4 WHEN 'dispatch' THEN 3 WHEN 'admission' THEN 2 ELSE 1 END DESC,d.recorded_at DESC,d.source_fact_id LIMIT 1)`, path, profile)
}

func (s *WorkspaceStore) GetWorkbenchSourceFacts(ctx context.Context, subject, id string, limit int, cursor string, now time.Time) (workbench.SourceObservations, error) {
	r, err := s.loadValidWorkbenchReading(ctx, subject, id, now)
	if err != nil {
		return workbench.SourceObservations{}, err
	}
	if r.SourceManifestRef == "" {
		return workbench.SourceObservations{}, workbench.ErrScopeForbidden
	}
	if limit < 1 || limit > workbench.MaxPageSize {
		return workbench.SourceObservations{}, workbench.ErrInvalid
	}
	offset := 0
	if cursor != "" {
		var ok bool
		offset, ok = parsePageCursor(cursor, id, "source_facts")
		if !ok {
			return workbench.SourceObservations{}, workbench.ErrInvalid
		}
	}
	items, err := s.workbenchSourceObservations(ctx, subject, id, nil, limit+1, offset)
	if err != nil {
		return workbench.SourceObservations{}, err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = pageCursor(id, "source_facts", offset+limit)
	}
	return workbench.SourceObservations{ReadingID: id, ManifestRef: r.SourceManifestRef, Items: items, NextCursor: next}, nil
}

func (s *WorkspaceStore) workbenchSourceObservations(ctx context.Context, subject, id string, economicIDs []string, limit, offset int) ([]workbench.SourceObservation, error) {
	// An empty monetary membership still has independently visible source rows.
	rows, err := s.db.QueryContext(ctx, `SELECT d.source_system_id::text,d.source_bytes,d.source_hash,d.envelope_hash,sm.chain_state,sm.economic_state,COALESCE(sm.economic_fact_id::text,'')
 FROM workbench_source_members sm JOIN source_fact_documents d USING(source_system_id,event_id)
 WHERE sm.reading_id=$1 AND (cardinality($3::text[])=0 OR sm.economic_fact_id::text=ANY($3::text[]))
 AND EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$2 AND g.active AND g.allow_details
 AND g.source_system_id=d.source_system_id AND (g.source_project_ref IS NULL OR g.source_project_ref='project-'||d.project_id) AND g.cost_project_ref IS NULL AND `+sourceProfileGrantSQL+`)
 ORDER BY d.occurred_at,d.source_system_id,d.event_id LIMIT $4 OFFSET $5`, id, subject, pgTextArray(economicIDs), limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []workbench.SourceObservation{}
	for rows.Next() {
		var item workbench.SourceObservation
		var raw []byte
		if err = rows.Scan(&item.SourceSystemID, &raw, &item.SourceHash, &item.EnvelopeHash, &item.ChainState, &item.EconomicState, &item.EconomicFactID); err != nil {
			return nil, err
		}
		d, err := sourcefacts.DecodeDocument(raw)
		if err != nil {
			return nil, err
		}
		item.RequestID = d.RequestID
		item.Profile = d.Profile
		item.Stage = d.Stage
		item.SourceFactID = d.SourceFactID
		item.Revision = d.Revision
		item.CaptureStatus = d.CaptureStatus
		item.OccurredAt, _ = time.Parse(sourcefacts.TimeFormat, d.OccurredAt)
		item.RecordedAt, _ = time.Parse(sourcefacts.TimeFormat, d.RecordedAt)
		if item.ChainState == "complete" {
			item.Context = d.Core
			item.Identity = d.Identity
			item.CostBasis = d.Cost
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *WorkspaceStore) addWorkbenchDetailSources(ctx context.Context, subject, id string, items []workbench.DetailRow) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.EconomicFactID)
	}
	// One bounded query for the whole detail page, not one round trip per row.
	observations, err := s.workbenchSourceObservations(ctx, subject, id, ids, 12*len(items), 0)
	if err != nil {
		return err
	}
	rank := map[string]int{"authenticated": 1, "admission": 2, "dispatch": 3, "terminal": 4}
	selected := map[string]map[string]workbench.SourceObservation{}
	for _, d := range observations {
		if selected[d.EconomicFactID] == nil {
			selected[d.EconomicFactID] = map[string]workbench.SourceObservation{}
		}
		old, ok := selected[d.EconomicFactID][d.Profile]
		if !ok || rank[d.Stage] > rank[old.Stage] || (rank[d.Stage] == rank[old.Stage] && (d.RecordedAt.After(old.RecordedAt) || (d.RecordedAt.Equal(old.RecordedAt) && d.SourceFactID < old.SourceFactID))) {
			selected[d.EconomicFactID][d.Profile] = d
		}
	}
	for i := range items {
		for _, profile := range []string{sourcefacts.ContextProfile, sourcefacts.IdentityProfile, sourcefacts.CostProfile} {
			d, ok := selected[items[i].EconomicFactID][profile]
			if !ok {
				continue
			}
			items[i].SourceFacts = append(items[i].SourceFacts, d)
			if d.ChainState == "complete" {
				if sourcefacts.Text(d.Identity, "owner_user_id") != "" {
					items[i].AccountState = "known_account_owner"
				}
				if sourcefacts.Text(d.Context, "workload_application_id") != "" {
					items[i].ApplicationState = "known"
				}
			}
		}
	}
	return nil
}

func workbenchHasDimension(reading workbench.Reading, dimension string) bool {
	return slices.Contains(reading.Dimensions, dimension)
}
