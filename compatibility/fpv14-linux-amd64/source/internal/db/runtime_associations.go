package db

import (
	"context"
	"database/sql"
	"time"

	"github.com/foigent/x-insight/internal/executionid"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

// Resolve only the existing, independently authorized economic association.
// A request string supplied by an execution page cannot select a different
// source/instance/org/project or manufacture an execution for unpriced B data.
func (s *WorkspaceStore) GetRuntimeExecutionRequest(ctx context.Context, subject, id, executionID string, now time.Time) (workbench.RuntimeExecutionRequest, error) {
	result := workbench.RuntimeExecutionRequest{Version: workbench.RuntimeExecutionRequestVersion, ReadingID: id, ExecutionID: executionID}
	r, err := s.GetRuntimeReading(ctx, subject, id, now)
	if err != nil {
		return result, err
	}
	if !executionid.Valid(executionID) {
		return result, workbench.ErrNotFound
	}
	result.MembershipRef = r.MembershipRef
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT q.request_ref,q.source_system_id::text,q.source_instance,q.org_id,q.project_id,q.request_id,q.entry_at,q.last_observed_at,q.observation_count
 FROM runtime_reading_economic_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id
 JOIN runtime_reading_requests q ON q.reading_id=m.reading_id AND q.request_ref=m.request_ref
 WHERE m.reading_id=$1 AND f.execution_id=$2 ORDER BY q.request_ref LIMIT 2`, id, executionID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > 1 {
			return workbench.RuntimeExecutionRequest{}, workbench.ErrConflict
		}
		var at, last sql.NullTime
		q := &result.Request
		if err = rows.Scan(&q.Ref, &q.SourceSystemID, &q.SourceInstance, &q.OrgID, &q.ProjectID, &q.RequestID, &at, &last, &q.ObservationCount); err != nil {
			return result, err
		}
		q.EntryTimeState = "unknown"
		if at.Valid {
			value := at.Time.UTC()
			q.EntryAt = &value
			q.EntryTimeState = "observed"
		}
		if last.Valid {
			q.LastObservedAt = last.Time.UTC()
		}
		if q.ObservationCount == 0 {
			q.MemberBasis = "content_eligible"
		}
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if count == 0 {
		return result, workbench.ErrNotFound
	}
	return result, nil
}

func freezeRuntimeAssociations(ctx context.Context, tx *sql.Tx, id, subject string, filters workbench.Filters, cutoff time.Time) error {
	// Reuse the exact I-01 join, with d now the selected full request identity.
	_, err := tx.ExecContext(ctx, `INSERT INTO runtime_reading_economic_members(reading_id,request_ref,economic_fact_id)
 SELECT $1,d.request_ref,f.id FROM runtime_reading_requests d
 JOIN workbench_economic_facts f ON `+sourceEconomicJoinSQL+`
 JOIN evidence_events e ON e.id=f.evidence_event_id
 WHERE d.reading_id=$1 AND f.projected_at<=$3 AND e.ingested_at<=$3
 AND (cardinality($4::text[])=0 OR f.cost_project_ref=ANY($4::text[]))
 AND EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$2 AND g.active AND g.allow_summary AND g.allow_details
 AND g.source_system_id=f.source_system_id AND (g.source_project_ref IS NULL OR g.source_project_ref=f.source_project_ref)
 AND (g.cost_project_ref IS NULL OR g.cost_project_ref=f.cost_project_ref))`, id, subject, cutoff, pgTextArray(filters.CostProjects))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `WITH RECURSIVE latest AS (
 SELECT DISTINCT ON(d.source_system_id,d.source_fact_id) d.*,q.request_ref FROM source_fact_documents d
 JOIN runtime_reading_requests q ON q.reading_id=$1 AND ROW(q.source_system_id,q.source_instance,q.org_id,q.project_id,q.request_id)
 =ROW(d.source_system_id,d.source_instance,d.org_id,d.project_id,d.request_id)
 LEFT JOIN runtime_reading_economic_members m ON m.reading_id=$1 AND m.request_ref=q.request_ref
 LEFT JOIN workbench_economic_facts f ON f.id=m.economic_fact_id
 WHERE d.received_at<=$3 AND (cardinality($4::text[])=0 OR f.cost_project_ref=ANY($4::text[]))
 AND EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$2 AND g.active AND g.allow_summary AND g.allow_details
 AND g.source_system_id=d.source_system_id AND (g.source_project_ref IS NULL OR g.source_project_ref='project-'||d.project_id)
 AND (g.cost_project_ref IS NULL OR g.cost_project_ref=f.cost_project_ref) AND `+sourceProfileGrantSQL+`)
 ORDER BY d.source_system_id,d.source_fact_id,d.revision DESC
 ), lineage AS (
 SELECT source_system_id,source_fact_id,event_id root_event,revision,previous_hash FROM latest
 UNION ALL SELECT d.source_system_id,d.source_fact_id,l.root_event,d.revision,d.previous_hash FROM lineage l
 JOIN source_fact_documents d ON d.source_system_id=l.source_system_id AND d.source_fact_id=l.source_fact_id
 AND d.revision=l.revision-1 AND d.source_hash=l.previous_hash AND d.received_at<=$3
 ) INSERT INTO runtime_reading_source_members(reading_id,request_ref,source_system_id,event_id,chain_state)
 SELECT $1,d.request_ref,d.source_system_id,d.event_id,CASE WHEN EXISTS(SELECT 1 FROM lineage l
 WHERE l.source_system_id=d.source_system_id AND l.root_event=d.event_id AND l.revision=1) THEN 'complete' ELSE 'missing_predecessor' END
 FROM latest d`, id, subject, cutoff, pgTextArray(filters.CostProjects))
	if err != nil {
		return err
	}
	var sources, economics int64
	err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM runtime_reading_source_members WHERE reading_id=$1),
 (SELECT count(*) FROM runtime_reading_economic_members WHERE reading_id=$1)`, id).Scan(&sources, &economics)
	if err != nil {
		return err
	}
	// Separate bounded memberships do not shrink the existing observation limit.
	if sources > workbench.MaxMembers || economics > workbench.MaxMembers {
		return workbench.ErrTooLarge
	}
	return nil
}

func (s *WorkspaceStore) GetRuntimeAssociations(ctx context.Context, subject, id, requestRef string, limit int, cursor string, now time.Time) (workbench.RuntimeAssociations, error) {
	result := workbench.RuntimeAssociations{ReadingID: id, RequestRef: requestRef, AssociationsVersion: workbench.RuntimeAssociationsVersion,
		Basis: "same_source_request_identity", Items: []workbench.RuntimeAssociation{}}
	r, err := s.GetRuntimeReading(ctx, subject, id, now)
	if err != nil {
		return result, err
	}
	if !workbench.ValidRuntimeRequestRef(requestRef) || limit < 1 || limit > workbench.MaxPageSize {
		return result, workbench.ErrInvalid
	}
	result.MembershipRef = r.MembershipRef
	offset := 0
	key := "runtime.associations:" + requestRef
	if cursor != "" {
		var ok bool
		offset, ok = parsePageCursor(cursor, id, key)
		if !ok {
			return result, workbench.ErrInvalid
		}
	}
	var present bool
	if err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runtime_reading_requests WHERE reading_id=$1 AND request_ref=$2)`, id, requestRef).Scan(&present); err != nil {
		return result, err
	}
	if !present {
		return result, workbench.ErrNotFound
	}
	// A reading's authorization revision includes A, B and economic grants.
	// No live join can add a late source or economic member to this frozen page.
	rows, err := s.db.QueryContext(ctx, `SELECT kind,source_system_id,raw,source_hash,envelope_hash,chain_state,economic_id,execution_id,event_hash FROM (
 SELECT 'source'::text kind,d.source_system_id::text,d.source_bytes raw,d.source_hash,d.envelope_hash,m.chain_state,
 ''::text economic_id,''::text execution_id,''::text event_hash,d.event_id sort_id
 FROM runtime_reading_source_members m JOIN source_fact_documents d USING(source_system_id,event_id)
 WHERE m.reading_id=$1 AND m.request_ref=$2
 UNION ALL SELECT 'economic',f.source_system_id::text,NULL::bytea,'','','',f.id::text,f.execution_id,f.source_event_hash,f.id::text
 FROM runtime_reading_economic_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id
 WHERE m.reading_id=$1 AND m.request_ref=$2
 ) all_members ORDER BY kind COLLATE "C",source_system_id COLLATE "C",sort_id COLLATE "C" LIMIT $3 OFFSET $4`, id, requestRef, limit+1, offset)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var raw []byte
		var src workbench.SourceObservation
		var econ workbench.RuntimeEconomicLink
		if err = rows.Scan(&kind, &src.SourceSystemID, &raw, &src.SourceHash, &src.EnvelopeHash, &src.ChainState, &econ.EconomicFactID, &econ.ExecutionID, &econ.SourceEventHash); err != nil {
			return result, err
		}
		item := workbench.RuntimeAssociation{Kind: kind}
		if kind == "economic" {
			item.Economic = &econ
		} else {
			d, e := sourcefacts.DecodeDocument(raw)
			if e != nil {
				return result, e
			}
			src.RequestID = d.RequestID
			src.Profile = d.Profile
			src.Stage = d.Stage
			src.SourceFactID = d.SourceFactID
			src.Revision = d.Revision
			src.CaptureStatus = d.CaptureStatus
			src.OccurredAt, _ = time.Parse(sourcefacts.TimeFormat, d.OccurredAt)
			src.RecordedAt, _ = time.Parse(sourcefacts.TimeFormat, d.RecordedAt)
			// A request identity join does not validate a cost-basis economic reference.
			src.EconomicState = "not_evaluated"
			if src.ChainState == "complete" {
				src.Context = d.Core
				src.Identity = d.Identity
				src.CostBasis = d.Cost
			}
			item.Source = &src
		}
		result.Items = append(result.Items, item)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		result.NextCursor = pageCursor(id, key, offset+limit)
	}
	return result, nil
}
