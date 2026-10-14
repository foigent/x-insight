package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

const budgetManifestSQL = `SELECT 'sha256:'||encode(sha256(convert_to(r.id||E'\n'||r.owner_subject||E'\n'||r.contract_version||E'\n'||
 (r.cutoff AT TIME ZONE 'UTC')::text||E'\n'||(r.expires_at AT TIME ZONE 'UTC')::text||E'\n'||r.filters::text||E'\n'||r.authorization_revision||E'\n'||r.interpretation_revision||E'\n'||
 COALESCE((SELECT string_agg(m.ordinal::text||':'||m.collection_id::text||':'||m.receipt_catalog_id::text||':'||m.source_reading_id::text||':'||s.reading_sha256||':'||m.profiles::text||':'||m.source_state,E'\n' ORDER BY m.ordinal)
 FROM budget_reading_members m JOIN source_budget_readings s ON s.id=m.source_reading_id WHERE m.reading_id=r.id),''),'UTF8')),'hex')
 FROM budget_readings r WHERE r.id=$1`

const budgetVisibleSQL = `WITH candidates AS (
 SELECT r.*,c.id collection_id,receipt.catalog_id receipt_catalog_id,CASE WHEN c.transport='bounded_offline' THEN 'bounded_offline' ELSE c.status END source_state,ARRAY(
 SELECT p FROM unnest(r.profiles) p WHERE p=ANY(c.granted_profiles) AND EXISTS(
 SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$2 AND g.active AND g.source_system_id=r.source_system_id
 AND (g.source_project_ref IS NULL OR g.source_project_ref='project-'||r.project_id) AND g.cost_project_ref IS NULL
 AND ((p='unirouter.budget-definition.v1' AND g.allow_budget_definition)
 OR (p='unirouter.budget-period-summary.v1' AND g.allow_budget_summary)
 OR (p='unirouter.budget-alert.v1' AND g.allow_budget_alert))) ORDER BY p) visible_profiles
 FROM source_budget_readings r JOIN source_budget_receipts receipt ON receipt.source_reading_id=r.id
 JOIN source_budget_collections c ON c.id=receipt.collection_id
 JOIN source_budget_catalog_entries e ON e.collection_id=c.id AND e.cycle=c.visible_cycle AND e.budget_ref=r.budget_ref
 WHERE r.received_at<=$3 AND receipt.received_at<=$3 AND c.visible_cycle>0 AND NOT c.requires_attention AND c.status<>'revoked'
 AND r.source_system_id=c.source_system_id AND r.source_instance=c.source_instance AND r.org_id=c.org_id AND r.project_id=c.project_id
 AND r.access_generation=c.access_generation AND r.catalog_revision=c.catalog_revision AND r.definition_revision=e.definition_revision AND r.binding_revision=e.binding_revision
 AND (cardinality($4::text[])=0 OR r.source_system_id::text=ANY($4::text[]))
 AND (cardinality($5::text[])=0 OR 'project-'||r.project_id=ANY($5::text[]))
), latest AS (
 SELECT DISTINCT ON(source_system_id,source_instance,org_id,project_id,budget_ref) * FROM candidates
 WHERE cardinality(visible_profiles)>0
 ORDER BY source_system_id,source_instance,org_id,project_id,budget_ref,reading_cutoff DESC,reading_id DESC,collection_id,receipt_catalog_id
) INSERT INTO budget_reading_members(reading_id,source_reading_id,collection_id,receipt_catalog_id,ordinal,profiles,source_state)
 SELECT $1,id,collection_id,receipt_catalog_id,row_number() OVER(ORDER BY source_system_id,source_instance,org_id,project_id,budget_ref),visible_profiles,source_state
 FROM latest LIMIT $6`

func (s *WorkspaceStore) CreateBudgetReading(ctx context.Context, subject string, request workbench.BudgetReadingRequest, now time.Time) (workbench.BudgetReading, error) {
	var result workbench.BudgetReading
	if err := request.Validate(); err != nil {
		return result, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	auth, err := loadWorkbenchAuthorization(ctx, tx, subject)
	if err != nil {
		return result, err
	}
	if !authorizationAllows(auth, request.Filters, func(g workbench.Grant) bool { return g.CostProject == "" && len(workbench.BudgetGrantProfiles(g)) > 0 }) {
		return result, workbench.ErrScopeForbidden
	}
	id, err := workbench.NewID("xbd_")
	if err != nil {
		return result, err
	}
	filters, err := json.Marshal(request.Filters)
	if err != nil {
		return result, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO budget_readings(id,owner_subject,contract_version,cutoff,expires_at,filters,authorization_revision,interpretation_revision,membership_sha256)
 VALUES($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9)`, id, subject, workbench.BudgetContractVersion, now, now.Add(workbench.ReadingTTL), string(filters), auth.BudgetRevision, digestText(workbench.BudgetInterpretation), digestText(""))
	if err != nil {
		return result, err
	}
	inserted, err := tx.ExecContext(ctx, budgetVisibleSQL, id, subject, now, pgTextArray(request.Filters.SourceIDs), pgTextArray(request.Filters.SourceProjects), workbench.MaxMembers+1)
	if err != nil {
		return result, err
	}
	count, err := inserted.RowsAffected()
	if err != nil {
		return result, err
	}
	if count > workbench.MaxMembers {
		return result, workbench.ErrTooLarge
	}
	var digest string
	if err = tx.QueryRowContext(ctx, budgetManifestSQL, id).Scan(&digest); err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE budget_readings SET membership_sha256=$2,budget_count=$3,sealed=true WHERE id=$1`, id, digest, count); err != nil {
		return result, err
	}
	if err = commitWorkspaceTx(ctx, tx); err != nil {
		return result, err
	}
	return workbench.BudgetReading{ID: id, ContractVersion: workbench.BudgetContractVersion, Cutoff: now, ExpiresAt: now.Add(workbench.ReadingTTL), AuthorizationRevision: auth.BudgetRevision, InterpretationRevision: digestText(workbench.BudgetInterpretation), MembershipRef: digest, BudgetCount: count, CalculatedBudgetSourceState: "formal_source_profile_missing", IndependentInvoiceSourceState: "independent_source_not_bound"}, nil
}

func (s *WorkspaceStore) GetBudgetReading(ctx context.Context, subject, id string, now time.Time) (workbench.BudgetReading, error) {
	var r workbench.BudgetReading
	if !workbench.ValidBudgetReadingID(id) {
		return r, workbench.ErrNotFound
	}
	err := s.db.QueryRowContext(ctx, `SELECT id,contract_version,cutoff,expires_at,authorization_revision,interpretation_revision,membership_sha256,budget_count
 FROM budget_readings WHERE id=$1 AND owner_subject=$2 AND sealed`, id, subject).Scan(&r.ID, &r.ContractVersion, &r.Cutoff, &r.ExpiresAt, &r.AuthorizationRevision, &r.InterpretationRevision, &r.MembershipRef, &r.BudgetCount)
	if errors.Is(err, sql.ErrNoRows) {
		return r, workbench.ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if !now.Before(r.ExpiresAt) {
		return workbench.BudgetReading{}, workbench.ErrExpired
	}
	auth, err := loadWorkbenchAuthorization(ctx, s.db, subject)
	if err != nil {
		return workbench.BudgetReading{}, err
	}
	if !authorizationAllows(auth, workbench.Filters{}, func(g workbench.Grant) bool { return g.CostProject == "" && len(workbench.BudgetGrantProfiles(g)) > 0 }) {
		return workbench.BudgetReading{}, workbench.ErrScopeForbidden
	}
	if auth.BudgetRevision != r.AuthorizationRevision {
		return workbench.BudgetReading{}, workbench.ErrAccessChanged
	}
	if r.ContractVersion != workbench.BudgetContractVersion || r.InterpretationRevision != digestText(workbench.BudgetInterpretation) {
		return workbench.BudgetReading{}, workbench.ErrUnsupportedVersion
	}
	var valid bool
	err = s.db.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM budget_reading_members m
 JOIN source_budget_readings s ON s.id=m.source_reading_id JOIN source_budget_collections c ON c.id=m.collection_id
 WHERE m.reading_id=$1 AND (c.status='revoked' OR c.requires_attention OR c.visible_cycle=0 OR NOT m.profiles<@c.granted_profiles
 OR NOT EXISTS(SELECT 1 FROM source_budget_catalog_entries e WHERE e.collection_id=c.id AND e.cycle=c.visible_cycle
 AND e.budget_ref=s.budget_ref AND e.binding_revision=s.binding_revision)))`, id).Scan(&valid)
	if err != nil {
		return workbench.BudgetReading{}, err
	}
	if !valid {
		return workbench.BudgetReading{}, workbench.ErrAccessChanged
	}
	var digest string
	if err = s.db.QueryRowContext(ctx, budgetManifestSQL, id).Scan(&digest); err != nil {
		return workbench.BudgetReading{}, err
	}
	if digest != r.MembershipRef {
		return workbench.BudgetReading{}, fmt.Errorf("budget reading membership is invalid")
	}
	r.Cutoff = r.Cutoff.UTC()
	r.ExpiresAt = r.ExpiresAt.UTC()
	r.CalculatedBudgetSourceState = "formal_source_profile_missing"
	r.IndependentInvoiceSourceState = "independent_source_not_bound"
	return r, nil
}

func (s *WorkspaceStore) GetBudgetItems(ctx context.Context, subject, id string, limit int, cursor string, now time.Time) (workbench.BudgetItems, error) {
	result := workbench.BudgetItems{ReadingID: id, Items: []workbench.BudgetItem{}}
	reading, err := s.GetBudgetReading(ctx, subject, id, now)
	if err != nil {
		return result, err
	}
	if limit < 1 || limit > workbench.MaxPageSize {
		return result, workbench.ErrInvalid
	}
	auth, err := loadWorkbenchAuthorization(ctx, s.db, subject)
	if err != nil {
		return result, err
	}
	if auth.BudgetRevision != reading.AuthorizationRevision {
		return result, workbench.ErrAccessChanged
	}
	offset := 0
	if cursor != "" {
		var ok bool
		offset, ok = parsePageCursor(cursor, id, "budget.items")
		if !ok {
			return result, workbench.ErrInvalid
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.id::text,r.source_system_id::text,r.reading_bytes,r.received_at,array_to_json(m.profiles),m.source_state,p.alerts_input_cursor,CASE WHEN c.transport='bounded_offline' THEN 'bounded_offline' WHEN c.last_error='source_unavailable' THEN 'source_unavailable' ELSE 'available' END
 FROM budget_reading_members m JOIN source_budget_readings r ON r.id=m.source_reading_id
 JOIN source_budget_collections c ON c.id=m.collection_id
 JOIN source_budget_receipts p ON p.collection_id=m.collection_id AND p.source_reading_id=m.source_reading_id AND p.catalog_id=m.receipt_catalog_id
 WHERE m.reading_id=$1 ORDER BY m.ordinal LIMIT $2 OFFSET $3`, id, limit+1, offset)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref, source, state, alertCursor, retrieval string
		var raw, profileRaw []byte
		var received time.Time
		if err = rows.Scan(&ref, &source, &raw, &received, &profileRaw, &state, &alertCursor, &retrieval); err != nil {
			return result, err
		}
		r, err := sourcefacts.DecodeBudgetReading(raw)
		if err != nil {
			return result, err
		}
		var profiles []string
		if err = json.Unmarshal(profileRaw, &profiles); err != nil {
			return result, err
		}
		i := workbench.ProjectBudget(r, budgetViewerProfiles(auth, source, r.ProjectID), profiles)
		i.Ref = ref
		i.SourceSystemID = source
		i.SourceState = state
		i.CurrentRetrievalState = retrieval
		i.ReceivedAt = received.UTC()
		if i.Alerts.State == "available" {
			i.AlertsPage = "first_page"
			if alertCursor != "" {
				i.AlertsPage = "continuation_page"
			}
		}
		result.Items = append(result.Items, i)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		result.NextCursor = pageCursor(id, "budget.items", offset+limit)
	}
	return result, nil
}

// Historical alert continuations and definition revisions remain separate
// snapshots. The immutable receipt's receive cutoff freezes this listing too;
// later arrivals cannot fill or recolor an earlier XI reading.
func (s *WorkspaceStore) GetBudgetHistory(ctx context.Context, subject, id, ref string, limit int, cursor string, now time.Time) (workbench.BudgetItems, error) {
	result := workbench.BudgetItems{ReadingID: id, Items: []workbench.BudgetItem{}}
	reading, err := s.GetBudgetReading(ctx, subject, id, now)
	if err != nil {
		return result, err
	}
	if !workbench.ValidBudgetItemRef(ref) || limit < 1 || limit > workbench.MaxPageSize {
		return result, workbench.ErrInvalid
	}
	dimension := "budget.history:" + ref
	auth, err := loadWorkbenchAuthorization(ctx, s.db, subject)
	if err != nil {
		return result, err
	}
	if auth.BudgetRevision != reading.AuthorizationRevision {
		return result, workbench.ErrAccessChanged
	}
	offset := 0
	if cursor != "" {
		var ok bool
		offset, ok = parsePageCursor(cursor, id, dimension)
		if !ok {
			return result, workbench.ErrInvalid
		}
	}
	var present bool
	if err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM budget_reading_members WHERE reading_id=$1 AND source_reading_id=$2::uuid)`, id, ref).Scan(&present); err != nil {
		return result, err
	}
	if !present {
		return result, workbench.ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, `SELECT h.id::text,h.source_system_id::text,h.reading_bytes,h.received_at,array_to_json(m.profiles),m.source_state,p.alerts_input_cursor,CASE WHEN c.transport='bounded_offline' THEN 'bounded_offline' WHEN c.last_error='source_unavailable' THEN 'source_unavailable' ELSE 'available' END
 FROM budget_reading_members m JOIN source_budget_readings anchor ON anchor.id=m.source_reading_id
 JOIN source_budget_collections c ON c.id=m.collection_id
 JOIN source_budget_readings h ON h.source_system_id=anchor.source_system_id AND h.source_instance=anchor.source_instance
 AND h.org_id=anchor.org_id AND h.project_id=anchor.project_id AND h.budget_ref=anchor.budget_ref AND h.binding_revision=anchor.binding_revision
 JOIN LATERAL(SELECT p.alerts_input_cursor FROM source_budget_receipts p WHERE p.source_reading_id=h.id AND p.collection_id=m.collection_id AND p.received_at<=$3 ORDER BY p.received_at,p.catalog_id LIMIT 1) p ON true
 WHERE m.reading_id=$1 AND m.source_reading_id=$2::uuid AND h.received_at<=$3
 ORDER BY h.reading_cutoff DESC,h.reading_id DESC LIMIT $4 OFFSET $5`, id, ref, reading.Cutoff, limit+1, offset)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var localRef, source, state, alertCursor, retrieval string
		var raw, profileRaw []byte
		var received time.Time
		if err = rows.Scan(&localRef, &source, &raw, &received, &profileRaw, &state, &alertCursor, &retrieval); err != nil {
			return result, err
		}
		r, err := sourcefacts.DecodeBudgetReading(raw)
		if err != nil {
			return result, err
		}
		var profiles []string
		if err = json.Unmarshal(profileRaw, &profiles); err != nil {
			return result, err
		}
		i := workbench.ProjectBudget(r, budgetViewerProfiles(auth, source, r.ProjectID), profiles)
		i.Ref = localRef
		i.SourceSystemID = source
		i.SourceState = state
		i.CurrentRetrievalState = retrieval
		i.ReceivedAt = received.UTC()
		if i.Alerts.State == "available" {
			i.AlertsPage = "first_page"
			if alertCursor != "" {
				i.AlertsPage = "continuation_page"
			}
		}
		result.Items = append(result.Items, i)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		result.NextCursor = pageCursor(id, dimension, offset+limit)
	}
	return result, nil
}

// The frozen member stores the source/viewer intersection. Keep viewer scope
// separate for the explanation: source omission is not a missing personnel grant.
func budgetViewerProfiles(auth workbench.Authorization, source, project string) []string {
	var result []string
	for _, grant := range auth.Grants {
		if grant.SourceSystemID == source && grant.CostProject == "" && (grant.SourceProject == "" || grant.SourceProject == "project-"+project) {
			result = append(result, workbench.BudgetGrantProfiles(grant)...)
		}
	}
	return result
}
