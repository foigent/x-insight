package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"time"

	"github.com/foigent/x-insight/internal/workbench"
)

// Resource authority belongs to the reporter's source/project. An economic
// project grant cannot stand in for a resource grant on another authority.
const runtimeResourceGrantSQL = `EXISTS(SELECT 1 FROM workbench_access_grants g
 WHERE g.subject_id=$2 AND g.active AND g.allow_resource AND g.source_system_id=d.source_system_id
 AND (g.source_project_ref IS NULL OR g.source_project_ref='project-'||d.project_id) AND g.cost_project_ref IS NULL)`

func freezeRuntimeResources(ctx context.Context, tx *sql.Tx, readingID, subject string, cutoff time.Time) error {
	_, err := tx.ExecContext(ctx, `WITH RECURSIVE latest AS (
 SELECT DISTINCT ON(d.source_system_id,d.source_instance,d.org_id,d.project_id,d.record_ref)
 d.source_system_id,d.source_instance,d.org_id,d.project_id,d.record_ref,d.revision,d.previous_hash,
 d.target_source_system_id,d.target_source_instance,d.target_org_id,d.target_project_id,d.request_id
 FROM resource_access_records d WHERE d.received_at<=$3 AND `+runtimeResourceGrantSQL+`
 ORDER BY d.source_system_id,d.source_instance,d.org_id,d.project_id,d.record_ref,d.revision DESC
), lineage AS (
 SELECT source_system_id,source_instance,org_id,project_id,record_ref,revision root_revision,revision,previous_hash FROM latest
 UNION ALL SELECT d.source_system_id,d.source_instance,d.org_id,d.project_id,d.record_ref,l.root_revision,d.revision,d.previous_hash
 FROM lineage l JOIN resource_access_records d ON
 ROW(d.source_system_id,d.source_instance,d.org_id,d.project_id,d.record_ref)=ROW(l.source_system_id,l.source_instance,l.org_id,l.project_id,l.record_ref)
 AND d.revision=l.revision-1 AND d.record_hash=l.previous_hash AND d.received_at<=$3
) INSERT INTO runtime_reading_resource_members(reading_id,request_ref,source_system_id,source_instance,org_id,project_id,record_ref,revision,chain_state)
 SELECT $1,q.request_ref,d.source_system_id,d.source_instance,d.org_id,d.project_id,d.record_ref,d.revision,
 CASE WHEN EXISTS(SELECT 1 FROM lineage l WHERE
 ROW(l.source_system_id,l.source_instance,l.org_id,l.project_id,l.record_ref,l.root_revision)=
 ROW(d.source_system_id,d.source_instance,d.org_id,d.project_id,d.record_ref,d.revision) AND l.revision=1)
 THEN 'complete' ELSE 'missing_predecessor' END
 FROM latest d JOIN runtime_reading_requests q ON q.reading_id=$1 AND
 ROW(q.source_system_id,q.source_instance,q.org_id,q.project_id,q.request_id)=
 ROW(d.target_source_system_id,d.target_source_instance,d.target_org_id,d.target_project_id,d.request_id)`, readingID, subject, cutoff)
	if err != nil {
		return err
	}
	var count int64
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM runtime_reading_resource_members WHERE reading_id=$1`, readingID).Scan(&count); err != nil {
		return err
	}
	if count > workbench.MaxMembers {
		return workbench.ErrTooLarge
	}
	return nil
}

// An empty resource membership contributes no bytes, preserving pre-resource
// reading digests through the additive migration. New members bind full identity.
const runtimeResourceManifestSQL = `CASE WHEN EXISTS(SELECT 1 FROM runtime_reading_resource_members WHERE reading_id=r.id)
 THEN E'\n'||(SELECT string_agg(jsonb_build_array(m.request_ref,m.source_system_id,m.source_instance,m.org_id,m.project_id,
 m.record_ref,m.revision,m.chain_state,d.record_hash)::text,E'\n'
 ORDER BY m.source_system_id,m.source_instance,m.org_id,m.project_id,m.record_ref)
 FROM runtime_reading_resource_members m JOIN resource_access_records d
 USING(source_system_id,source_instance,org_id,project_id,record_ref,revision) WHERE m.reading_id=r.id) ELSE '' END`

func (s *WorkspaceStore) GetRuntimeResources(ctx context.Context, subject, id, requestRef string, limit int, cursor string, now time.Time) (workbench.RuntimeResources, error) {
	result := workbench.RuntimeResources{Version: workbench.RuntimeResourcesVersion, ReadingID: id, RequestRef: requestRef,
		Coverage: "synthetic_internal_records_only", Items: []workbench.RuntimeResourceItem{}}
	r, err := s.GetRuntimeReading(ctx, subject, id, now)
	if err != nil {
		return result, err
	}
	if !workbench.ValidRuntimeRequestRef(requestRef) || limit < 1 || limit > workbench.MaxPageSize {
		return result, workbench.ErrInvalid
	}
	result.MembershipRef = r.MembershipRef
	offset := 0
	if cursor != "" {
		var ok bool
		offset, ok = parsePageCursor(cursor, id, "runtime.resources:"+requestRef)
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
	rows, err := s.db.QueryContext(ctx, `SELECT d.record_bytes,d.record_hash,m.chain_state,
 EXISTS(SELECT 1 FROM resource_access_records newer WHERE
 ROW(newer.source_system_id,newer.source_instance,newer.org_id,newer.project_id,newer.record_ref)=
 ROW(d.source_system_id,d.source_instance,d.org_id,d.project_id,d.record_ref) AND newer.revision>d.revision)
 FROM runtime_reading_resource_members m JOIN resource_access_records d
 USING(source_system_id,source_instance,org_id,project_id,record_ref,revision)
 WHERE m.reading_id=$1 AND m.request_ref=$2
 AND d.record_json->>'schema_version'='xinsight.resource-access.internal.v1'
 ORDER BY m.source_system_id,m.source_instance COLLATE "C",m.org_id COLLATE "C",m.project_id COLLATE "C",m.record_ref COLLATE "C"
 LIMIT $3 OFFSET $4`, id, requestRef, limit+1, offset)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item workbench.RuntimeResourceItem
		var raw []byte
		if err = rows.Scan(&raw, &item.RecordHash, &item.ChainState, &item.ChangedSinceReading); err != nil {
			return result, err
		}
		item.Record, err = workbench.DecodeResourceAccess(raw)
		if err != nil {
			return result, err
		}
		item.ContentVerification = "not_performed"
		item.InputEvidenceBasis = "frozen_reference_witnesses"
		item.CapturedInputRefs = []string{}
		item.InputState = workbench.ResourceInputState(item.Record, "")
		result.Items = append(result.Items, item)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if err = rows.Close(); err != nil {
		return result, err
	}
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		result.NextCursor = pageCursor(id, "runtime.resources:"+requestRef, offset+limit)
	}
	// Compare only frozen destination observations. At most two witnesses per
	// item explain agreement/conflict without expanding the page without bound.
	type inputClaim struct {
		Item        int    `json:"item"`
		AttemptID   string `json:"attempt_id"`
		AttemptKind string `json:"attempt_kind"`
		InputRef    string `json:"input_ref"`
	}
	claims := []inputClaim{}
	attempts := []string{}
	for index := range result.Items {
		item := &result.Items[index]
		if item.ChangedSinceReading {
			item.InputState = "reassessment_required"
			continue
		}
		if item.ChainState != "complete" {
			item.InputState = "missing_predecessor"
			continue
		}
		if item.Record.Input == nil || item.Record.Withdrawn || item.Record.AccessOutcome != "read" {
			continue
		}
		claims = append(claims, inputClaim{index, item.Record.Input.AttemptID, item.Record.Input.AttemptKind, item.Record.Input.ReportedInputRef})
		attempts = append(attempts, item.Record.Input.AttemptID)
	}
	if len(claims) == 0 {
		return result, nil
	}
	rawClaims, err := json.Marshal(claims)
	if err != nil {
		return result, err
	}
	bindings, err := s.db.QueryContext(ctx, `WITH claims AS (
 SELECT c.item,c.attempt_id,c.attempt_kind,c.input_ref FROM jsonb_to_recordset($3::jsonb) AS c(item integer,attempt_id text,attempt_kind text,input_ref text)
), captured AS (
 SELECT d.subject_id,d.subject_kind,d.event_id,d.source_json#>>'{destination,input_binding_ref}' input_ref
 FROM runtime_reading_members m JOIN source_observation_documents d USING(source_system_id,event_id)
 WHERE m.reading_id=$1 AND m.request_ref=$2 AND m.chain_state='complete'
 AND d.profile='unirouter.destination-observation.v1' AND d.subject_id=ANY($4::text[])
 AND d.source_json#>>'{destination,input_binding_state}'='known'
)
SELECT c.item,COALESCE(bool_or(b.input_ref=c.input_ref),false),COALESCE(bool_or(b.input_ref<>c.input_ref),false),
 COALESCE(min(b.event_id) FILTER(WHERE b.input_ref=c.input_ref),''),COALESCE(min(b.event_id) FILTER(WHERE b.input_ref<>c.input_ref),'')
 FROM claims c LEFT JOIN captured b ON b.subject_id=c.attempt_id AND b.subject_kind=c.attempt_kind
 GROUP BY c.item ORDER BY c.item`, id, requestRef, string(rawClaims), pgTextArray(attempts))
	if err != nil {
		return result, err
	}
	defer bindings.Close()
	for bindings.Next() {
		var index int
		var matched, mismatched bool
		var matchWitness, mismatchWitness string
		if err = bindings.Scan(&index, &matched, &mismatched, &matchWitness, &mismatchWitness); err != nil {
			return result, err
		}
		item := &result.Items[index]
		if matchWitness != "" {
			item.CapturedInputRefs = append(item.CapturedInputRefs, matchWitness)
		}
		if mismatchWitness != "" {
			item.CapturedInputRefs = append(item.CapturedInputRefs, mismatchWitness)
		}
		sort.Strings(item.CapturedInputRefs)
		switch {
		case matched && mismatched:
			item.InputState = "conflicting_references"
		case matched:
			item.InputState = "reference_matched"
		case mismatched:
			item.InputState = "reference_mismatch"
		}
	}
	return result, bindings.Err()
}
