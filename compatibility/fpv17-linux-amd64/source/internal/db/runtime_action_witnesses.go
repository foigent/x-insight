package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/foigent/x-insight/internal/workbench"
)

// GetRuntimeActionWitnesses reads only members frozen in this reading. The
// final request is an explicit relationship target, not a claimed trigger.
func (s *WorkspaceStore) GetRuntimeActionWitnesses(ctx context.Context, subject, id, requestRef string, limit int, cursor string, now time.Time) (workbench.RuntimeActionWitnesses, error) {
	result := workbench.RuntimeActionWitnesses{Version: workbench.RuntimeActionWitnessesVersion, ReadingID: id, RequestRef: requestRef, Coverage: "test_development_action_witnesses_only", Items: []workbench.RuntimeActionWitnessItem{}}
	reading, err := s.GetRuntimeReading(ctx, subject, id, now)
	if err != nil {
		return result, err
	}
	if !workbench.ValidRuntimeRequestRef(requestRef) || limit < 1 || limit > workbench.MaxPageSize {
		return result, workbench.ErrInvalid
	}
	result.MembershipRef = reading.MembershipRef
	offset := 0
	if cursor != "" {
		var ok bool
		offset, ok = parsePageCursor(cursor, id, "runtime.action-witnesses:"+requestRef)
		if !ok {
			return result, workbench.ErrInvalid
		}
	}
	var present bool
	err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runtime_reading_requests WHERE reading_id=$1 AND request_ref=$2)`, id, requestRef).Scan(&present)
	if err != nil {
		return result, err
	}
	if !present {
		return result, workbench.ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.record_bytes,d.record_hash,m.chain_state,m.request_ref,COALESCE(q_final.request_ref,''),
 EXISTS(SELECT 1 FROM resource_access_records newer WHERE
 ROW(newer.source_system_id,newer.source_instance,newer.org_id,newer.project_id,newer.record_ref)=
 ROW(d.source_system_id,d.source_instance,d.org_id,d.project_id,d.record_ref) AND newer.revision>d.revision)
 FROM runtime_reading_resource_members m JOIN resource_access_records d
 USING(source_system_id,source_instance,org_id,project_id,record_ref,revision)
 LEFT JOIN runtime_reading_requests q_final ON q_final.reading_id=m.reading_id
 AND q_final.request_id=d.record_json->>'final_request_id'
 AND ROW(q_final.source_system_id,q_final.source_instance,q_final.org_id,q_final.project_id)=
 ROW(d.target_source_system_id,d.target_source_instance,d.target_org_id,d.target_project_id)
 JOIN runtime_reading_requests q_trigger ON q_trigger.reading_id=m.reading_id AND q_trigger.request_ref=m.request_ref
 WHERE m.reading_id=$1 AND d.record_json->>'schema_version'='xinsight.resource-action.v1'
 AND (m.request_ref=$2 OR q_final.request_ref=$2)
 AND ROW(d.target_source_system_id,d.target_source_instance,d.target_org_id,d.target_project_id,d.request_id)=
 ROW(q_trigger.source_system_id,q_trigger.source_instance,q_trigger.org_id,q_trigger.project_id,q_trigger.request_id)
 AND EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$3 AND g.active AND g.allow_resource
 AND g.source_system_id=d.source_system_id AND (g.source_project_ref IS NULL OR g.source_project_ref='project-'||d.project_id)
 AND g.cost_project_ref IS NULL)
 ORDER BY m.source_system_id,m.source_instance COLLATE "C",m.org_id COLLATE "C",m.project_id COLLATE "C",m.record_ref COLLATE "C"
 LIMIT $4 OFFSET $5`, id, requestRef, subject, limit+1, offset)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item workbench.RuntimeActionWitnessItem
		var raw []byte
		if err = rows.Scan(&raw, &item.RecordHash, &item.ChainState, &item.TriggerRequestRef, &item.FinalRequestRef, &item.ChangedSinceReading); err != nil {
			return result, err
		}
		item.Record, err = workbench.DecodeActionWitnessForImport(raw)
		if err != nil {
			return result, err
		}
		item.ResolvedContentLinks = []workbench.ActionResolvedContentLink{}
		for _, link := range item.Record.ContentLinks {
			var linkedRef string
			err = s.db.QueryRowContext(ctx, `SELECT request_ref FROM runtime_reading_content_refs
 WHERE reading_id=$1 AND source_system_id=$2::uuid AND source_fact_id=$3 AND revision=$4::numeric
 AND frozen_ref->>'source_instance'=$5 AND frozen_ref->>'org_id'=$6 AND frozen_ref->>'project_id'=$7
 AND frozen_ref->>'request_id'=$8 AND frozen_ref->>'content_ref'=$9 AND frozen_ref->>'source_sha256'=$10`,
				id, link.SourceSystemID, link.SourceFactID, link.Revision, link.SourceInstance, link.OrgID, link.ProjectID,
				link.RequestID, link.ContentRef, link.SourceSHA256).Scan(&linkedRef)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return result, err
			}
			item.ResolvedContentLinks = append(item.ResolvedContentLinks, workbench.ActionResolvedContentLink{Purpose: link.Purpose, RequestRef: linkedRef})
		}
		result.Items = append(result.Items, item)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		result.NextCursor = pageCursor(id, "runtime.action-witnesses:"+requestRef, offset+limit)
	}
	return result, nil
}
