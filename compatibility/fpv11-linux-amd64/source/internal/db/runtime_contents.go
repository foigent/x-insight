package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workchain"
)

const runtimeInterpretationWithContent = runtimeInterpretation + ";" + workbench.ContentInterpretationRevision
const runtimeInterpretationWithContentReceipts = runtimeInterpretationWithContent + ";xinsight.content-receipts.v1"

func contentGrant(g workbench.Grant) bool {
	return g.AllowContent && len(g.AllowedContentPurposes) == 1 && g.AllowedContentPurposes[0] == sourcefacts.ContentPurposeHumanReview
}

func runtimeContentTablesPresent(ctx context.Context, q workspaceQueryer) (bool, error) {
	var present bool
	err := q.QueryRowContext(ctx, `SELECT to_regclass('public.runtime_reading_content_members') IS NOT NULL AND to_regclass('public.runtime_reading_content_refs') IS NOT NULL`).Scan(&present)
	return present, err
}

// m is a manifest, d is either its full identity or a collection identity,
// and f is the exact optional economic anchor. Content never implies details.
const runtimeContentGrantSQL = `EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$2 AND g.active
 AND g.allow_content AND g.allowed_content_purposes='["human_review"]'::jsonb
 AND g.source_system_id=d.source_system_id AND (g.source_project_ref IS NULL OR g.source_project_ref='project-'||d.project_id)
 AND (g.cost_project_ref IS NULL OR g.cost_project_ref=f.cost_project_ref))`

const runtimeContentCandidatesSQL = `WITH latest_content AS (
 SELECT DISTINCT ON(d.source_system_id,d.source_fact_id) d.source_system_id,d.source_instance,d.org_id,d.project_id,d.request_id,d.source_fact_id,d.revision,d.source_sha256,d.source_json,d.occurred_at,f.id economic_fact_id,receipt.id receipt_id,receipt.collection_id receipt_collection_id,receipt.content_scope_revision receipt_scope_revision,receipt.recovery_epoch receipt_epoch
 FROM source_content_manifests d JOIN source_content_receipts receipt USING(source_system_id,source_fact_id,revision)
 JOIN source_content_collections c ON c.id=receipt.collection_id
 LEFT JOIN workbench_economic_facts f ON ` + sourceEconomicJoinSQL + ` AND f.projected_at<=$5 AND EXISTS(SELECT 1 FROM evidence_events received WHERE received.id=f.evidence_event_id AND received.ingested_at<=$5)
 LEFT JOIN evidence_events e ON e.id=f.evidence_event_id
 WHERE receipt.received_at<=$5 AND c.status='active' AND NOT c.requires_attention
 AND ROW(receipt.credential_id,receipt.registration_hash,receipt.content_scope_revision,receipt.recovery_epoch,receipt.coverage_from)=ROW(c.credential_id,c.registration_hash,c.content_scope_revision,c.recovery_epoch,c.coverage_from)
 AND (f.id IS NULL OR (f.projected_at<=$5 AND e.ingested_at<=$5))
 AND (cardinality($6::text[])=0 OR d.source_system_id::text=ANY($6::text[]))
 AND (cardinality($7::text[])=0 OR 'project-'||d.project_id=ANY($7::text[]))
 AND (cardinality($8::text[])=0 OR f.cost_project_ref=ANY($8::text[]))
 AND ` + runtimeContentGrantSQL + `
 ORDER BY d.source_system_id,d.source_fact_id,d.revision DESC,receipt.received_at DESC,receipt.id
), manifest_candidates AS (
 SELECT source_system_id,source_instance,org_id,project_id,request_id,source_fact_id,revision,source_sha256,source_json,occurred_at,economic_fact_id,receipt_id,receipt_collection_id,receipt_scope_revision,receipt_epoch FROM latest_content WHERE occurred_at>=$3 AND occurred_at<$4
), economic_candidates AS (
 SELECT DISTINCT d.source_system_id,d.source_instance,d.org_id,d.project_id,f.request_id,f.id economic_fact_id,f.occurred_at
 FROM workbench_economic_facts f JOIN evidence_events e ON e.id=f.evidence_event_id
 CROSS JOIN LATERAL (SELECT f.source_system_id,f.request_id,
 CASE WHEN left(f.source_ref,char_length('unirouter:'))='unirouter:'
 AND right(f.source_ref,char_length(':completed_execution:'||f.request_id))=':completed_execution:'||f.request_id
 AND char_length(f.source_ref)>char_length('unirouter:')+char_length(':completed_execution:'||f.request_id)
 THEN substring(f.source_ref FROM char_length('unirouter:')+1 FOR char_length(f.source_ref)-char_length('unirouter:')-char_length(':completed_execution:'||f.request_id)) END source_instance,
 substring(e.org_ref FROM char_length('org-')+1) org_id,substring(f.source_project_ref FROM char_length('project-')+1) project_id) d
 WHERE f.occurred_at>=$3 AND f.occurred_at<$4 AND f.projected_at<=$5 AND e.ingested_at<=$5
 AND d.source_instance IS NOT NULL AND d.org_id ~ '^[1-9][0-9]{0,18}$' AND d.project_id ~ '^[1-9][0-9]{0,18}$'
 AND ` + sourceEconomicJoinSQL + `
 AND (cardinality($6::text[])=0 OR d.source_system_id::text=ANY($6::text[]))
 AND (cardinality($7::text[])=0 OR 'project-'||d.project_id=ANY($7::text[]))
 AND (cardinality($8::text[])=0 OR f.cost_project_ref=ANY($8::text[]))
 AND ` + runtimeContentGrantSQL + `
), chosen_qualification AS (
 SELECT DISTINCT ON(source_system_id,source_instance,org_id,project_id,request_id)
 source_system_id,source_instance,org_id,project_id,request_id,member_basis,eligible_at,economic_fact_id
 FROM (
 SELECT source_system_id,source_instance,org_id,project_id,request_id,'content_manifest'::text member_basis,occurred_at eligible_at,economic_fact_id FROM manifest_candidates
 UNION ALL SELECT source_system_id,source_instance,org_id,project_id,request_id,'exact_execution_association',occurred_at,economic_fact_id FROM economic_candidates
 ) all_candidates ORDER BY source_system_id,source_instance,org_id,project_id,request_id,member_basis,eligible_at DESC,economic_fact_id
), qualification AS (
 SELECT d.source_system_id,d.source_instance,d.org_id,d.project_id,d.request_id,d.member_basis,d.eligible_at,d.economic_fact_id,c.id collection_id,c.credential_id,c.registration_hash,c.content_scope_revision,c.recovery_epoch,c.coverage_from
 FROM chosen_qualification d JOIN source_content_collections c ON
 ROW(c.source_system_id,c.source_instance,c.org_id,c.project_id)=ROW(d.source_system_id,d.source_instance,d.org_id,d.project_id)
 WHERE c.status='active' AND NOT c.requires_attention
) `

const runtimeContentRequestRefSQL = `'xrr_'||encode(sha256(convert_to(jsonb_build_array(d.source_system_id::text,d.source_instance,d.org_id,d.project_id,d.request_id)::text,'UTF8')),'hex')`

const runtimeFrozenContentRefSQL = `jsonb_strip_nulls(jsonb_build_object('source_system_id',d.source_system_id::text,'source_instance',d.source_instance,'org_id',d.org_id,'project_id',d.project_id,'request_id',d.request_id,
 'source_fact_id',d.source_fact_id,'revision',d.revision::text,'source_sha256',d.source_sha256,'capture_status',d.source_json#>>'{capture,completeness}',
 'content_ref',d.source_json->'content_ref','representation',d.source_json->'representation','expires_at',d.source_json->'expires_at'))`

func freezeRuntimeContents(ctx context.Context, tx *sql.Tx, args []any, auth workbench.Authorization) (int64, error) {
	if auth.ContentRevision == "" {
		return 0, nil
	}
	present, err := runtimeContentTablesPresent(ctx, tx)
	if err != nil {
		return 0, err
	}
	if !present {
		return 0, workbench.ErrUnsupportedVersion
	}
	_, err = tx.ExecContext(ctx, runtimeContentCandidatesSQL+`INSERT INTO runtime_reading_requests(reading_id,request_ref,ordinal,source_system_id,source_instance,org_id,project_id,request_id,entry_at,last_observed_at,observation_count,content_eligible_at)
 SELECT $1,`+runtimeContentRequestRefSQL+`,
 (SELECT COALESCE(max(ordinal),0) FROM runtime_reading_requests WHERE reading_id=$1)+row_number() OVER(ORDER BY d.eligible_at DESC,d.source_system_id,d.source_instance,d.org_id,d.project_id,d.request_id),
 d.source_system_id,d.source_instance,d.org_id,d.project_id,d.request_id,NULL,NULL,0,d.eligible_at
 FROM qualification d WHERE NOT EXISTS(SELECT 1 FROM runtime_reading_requests q WHERE q.reading_id=$1 AND q.request_ref=`+runtimeContentRequestRefSQL+`)`, args...)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, runtimeContentCandidatesSQL+`INSERT INTO runtime_reading_content_members(reading_id,request_ref,member_basis,eligible_at,economic_fact_id,collection_id,credential_id,registration_hash,content_scope_revision,recovery_epoch,coverage_from)
 SELECT $1,`+runtimeContentRequestRefSQL+`,d.member_basis,d.eligible_at,d.economic_fact_id,d.collection_id,d.credential_id,d.registration_hash,d.content_scope_revision,d.recovery_epoch,d.coverage_from FROM qualification d`, args...)
	if err != nil {
		return 0, err
	}
	// The exact saved representation is frozen, never a pointer to the latest row.
	_, err = tx.ExecContext(ctx, runtimeContentCandidatesSQL+`INSERT INTO runtime_reading_content_refs(reading_id,request_ref,collection_id,source_system_id,source_fact_id,revision,content_scope_revision,recovery_epoch,frozen_ref,receipt_id)
 SELECT $1,`+runtimeContentRequestRefSQL+`,d.receipt_collection_id,d.source_system_id,d.source_fact_id,d.revision,d.receipt_scope_revision,d.receipt_epoch,
 `+runtimeFrozenContentRefSQL+`,d.receipt_id
 FROM manifest_candidates d`, args...)
	if err != nil {
		return 0, err
	}
	var count, refs, total int64
	err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM runtime_reading_content_members WHERE reading_id=$1),(SELECT count(*) FROM runtime_reading_content_refs WHERE reading_id=$1),(SELECT count(*) FROM runtime_reading_requests WHERE reading_id=$1)`, args[0]).Scan(&count, &refs, &total)
	if err != nil {
		return 0, err
	}
	if count > workbench.MaxMembers || refs > workbench.MaxMembers || total > workbench.MaxMembers {
		return 0, workbench.ErrTooLarge
	}
	if count > 0 {
		_, err = tx.ExecContext(ctx, `UPDATE runtime_readings SET content_authorization_revision=$2,interpretation_revision=$3 WHERE id=$1`, args[0], auth.ContentRevision, runtimeContentInterpretation(refs > 0))
	}
	return count, err
}

// Empty content contributes exactly zero bytes, including the I01 qualification
// case: a member with no manifests is still nonempty and binds its own version.
const runtimeContentManifestSQL = `CASE WHEN EXISTS(SELECT 1 FROM runtime_reading_content_members WHERE reading_id=r.id)
 THEN E'\nxinsight.content-membership.v1\n'||r.content_authorization_revision||E'\n'||
 COALESCE((SELECT string_agg(jsonb_build_array(m.request_ref,m.member_basis,q.source_system_id,q.source_instance,q.org_id,q.project_id,q.request_id,m.eligible_at AT TIME ZONE 'UTC',m.economic_fact_id,f.source_event_hash,m.collection_id,m.credential_id,m.registration_hash,m.content_scope_revision,m.recovery_epoch,m.coverage_from)::text,E'\n'
 ORDER BY m.request_ref,m.member_basis,q.source_system_id,q.source_instance,q.org_id,q.project_id,q.request_id)
 FROM runtime_reading_content_members m JOIN runtime_reading_requests q USING(reading_id,request_ref) LEFT JOIN workbench_economic_facts f ON f.id=m.economic_fact_id WHERE m.reading_id=r.id),'')||E'\n'||
 COALESCE((SELECT string_agg(jsonb_build_array(m.request_ref,m.collection_id,m.source_system_id,m.source_fact_id,m.revision,m.content_scope_revision,m.recovery_epoch,m.frozen_ref)::text,E'\n'
 ORDER BY m.request_ref,m.source_system_id,m.source_fact_id,m.revision,m.frozen_ref::text)
 FROM runtime_reading_content_refs m WHERE m.reading_id=r.id),'') ELSE '' END`
const runtimeManifestDigestWithContentSQL = runtimeManifestDigestPrefixSQL + `||` + runtimeResourceManifestSQL + `||` + runtimeContentManifestSQL + runtimeManifestDigestSuffixSQL

const runtimeContentReceiptManifestSQL = `CASE WHEN EXISTS(SELECT 1 FROM runtime_reading_content_refs WHERE reading_id=r.id AND receipt_id IS NOT NULL)
 THEN E'\nxinsight.content-receipts.v1\n'||(SELECT string_agg(jsonb_build_array(m.request_ref,receipt.id,receipt.collection_id,receipt.credential_id,receipt.registration_hash,receipt.content_scope_revision,receipt.recovery_epoch,receipt.coverage_from,receipt.sequence,receipt.source_system_id,receipt.source_fact_id,receipt.revision,receipt.source_sha256,receipt.received_at AT TIME ZONE 'UTC',receipt.availability)::text,E'\n' ORDER BY m.request_ref,receipt.source_system_id,receipt.source_fact_id,receipt.revision,receipt.id)
 FROM runtime_reading_content_refs m JOIN source_content_receipts receipt ON receipt.id=m.receipt_id WHERE m.reading_id=r.id) ELSE '' END`
const runtimeManifestDigestWithContentReceiptsSQL = runtimeManifestDigestPrefixSQL + `||` + runtimeResourceManifestSQL + `||` + runtimeContentManifestSQL + `||` + runtimeContentReceiptManifestSQL + runtimeManifestDigestSuffixSQL

func runtimeContentInterpretation(receipts bool) string {
	if receipts {
		return digestText(runtimeInterpretationWithContentReceipts)
	}
	return digestText(runtimeInterpretationWithContent)
}

func (s *WorkspaceStore) GetRuntimeContents(ctx context.Context, subject, id, requestRef string, limit int, cursor string, now time.Time) (workbench.RuntimeContents, error) {
	result := workbench.RuntimeContents{Version: workbench.RuntimeContentVersion, ReadingID: id, RequestRef: requestRef, Items: []workbench.RuntimeContentItem{}}
	r, err := s.GetRuntimeReading(ctx, subject, id, now)
	if err != nil {
		return result, err
	}
	if !workbench.ValidRuntimeRequestRef(requestRef) || limit < 1 || limit > workbench.MaxPageSize {
		return result, workbench.ErrInvalid
	}
	if r.ContentAuthorizationRevision == "" {
		return result, workbench.ErrNotFound
	}
	result.MembershipRef = r.MembershipRef
	result.ContentInterpretationRevision = workbench.ContentInterpretationRevision
	if err = s.db.QueryRowContext(ctx, `SELECT member_basis FROM runtime_reading_content_members WHERE reading_id=$1 AND request_ref=$2`, id, requestRef).Scan(&result.MemberBasis); errors.Is(err, sql.ErrNoRows) {
		return result, workbench.ErrNotFound
	} else if err != nil {
		return result, err
	}
	offset := 0
	key := "runtime.contents:" + requestRef
	if cursor != "" {
		var ok bool
		offset, ok = parsePageCursor(cursor, id, key)
		if !ok {
			return result, workbench.ErrInvalid
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.frozen_ref,d.source_bytes,d.source_sha256,COALESCE(receipt.availability,d.availability),EXISTS(SELECT 1 FROM source_content_receipts n WHERE n.source_system_id=m.source_system_id AND n.source_fact_id=m.source_fact_id AND n.revision>m.revision
 AND ROW(n.collection_id,n.credential_id,n.registration_hash,n.content_scope_revision,n.recovery_epoch,n.coverage_from)=ROW(cm.collection_id,cm.credential_id,cm.registration_hash,cm.content_scope_revision,cm.recovery_epoch,cm.coverage_from))
 FROM runtime_reading_content_refs m JOIN runtime_reading_content_members cm USING(reading_id,request_ref) JOIN source_content_manifests d USING(source_system_id,source_fact_id,revision)
 LEFT JOIN source_content_receipts receipt ON receipt.id=m.receipt_id
 WHERE m.reading_id=$1 AND m.request_ref=$2 ORDER BY m.source_system_id,m.source_fact_id,m.revision LIMIT $3 OFFSET $4`, id, requestRef, limit+1, offset)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item workbench.RuntimeContentItem
		var ref, manifestBytes, availability []byte
		var manifestHash string
		if err = rows.Scan(&ref, &manifestBytes, &manifestHash, &availability, &item.NewerRevision); err != nil {
			return result, err
		}
		item.Reference, err = sourcefacts.DecodeFrozenManifestRef(ref)
		if err != nil {
			return result, err
		}
		item.Observation, err = runtimeContentObservation(item.Reference, manifestBytes, manifestHash)
		if err != nil {
			return result, err
		}
		if err = json.Unmarshal(availability, &item.Availability); err != nil {
			return result, err
		}
		if item.Reference.ExpiresAt != "" {
			expiry, _ := time.Parse(sourcefacts.TimeFormat, item.Reference.ExpiresAt)
			if !now.Before(expiry) {
				item.Availability = sourcefacts.ContentAvailability{State: "expired", EvaluatedAt: now.UTC().Format(sourcefacts.TimeFormat), Retryable: false}
			}
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

// Read canonical source bytes from the same revision, not JSONB reserialization
// or the latest manifest. The existing frozen hash binds all display metadata.
func runtimeContentObservation(ref sourcefacts.FrozenManifestRef, raw []byte, hash string) (*workbench.RuntimeContentObservation, error) {
	if len(raw) == 0 || len(raw) > sourcefacts.MaxDocumentBytes || hash != ref.SourceSHA256 || sourcefacts.Hash(raw) != hash {
		return nil, workbench.ErrContentIntegrity
	}
	m, err := sourcefacts.DecodeContentManifest(raw)
	if err != nil || sourcefacts.MatchContentManifestReference(ref, sourcefacts.ContentManifestEntry{Document: m, SourceSHA256: hash}) != nil {
		return nil, workbench.ErrContentIntegrity
	}
	return &workbench.RuntimeContentObservation{Protocol: m.Protocol, ObservationPoint: m.ObservationPoint, AttemptID: m.AttemptID, ContentItemID: m.ContentItemID, OccurredAt: m.OccurredAt, CaptureReason: m.Capture.Reason}, nil
}

// ResolveRuntimeContent accepts the route selector (content_ref/source_fact_id/
// revision). Optional extra reference fields are checked, never used as source.
// All database connections/authorization locks are released before returning.
func (s *WorkspaceStore) ResolveRuntimeContent(ctx context.Context, subject, id, requestRef string, selector sourcefacts.FrozenManifestRef, now time.Time) (workbench.RuntimeContentRead, error) {
	var result workbench.RuntimeContentRead
	if !workbench.ValidRuntimeReadingID(id) || !workbench.ValidRuntimeRequestRef(requestRef) || selector.SourceFactID == "" || selector.Revision == "" || selector.ContentRef == "" {
		return result, workbench.ErrNotFound
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelReadCommitted})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `LOCK TABLE workbench_access_grants IN ACCESS SHARE MODE`); err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, WorkchainGrantLock); err != nil {
		return result, err
	}
	present, err := runtimeContentTablesPresent(ctx, tx)
	if err != nil {
		return result, err
	}
	if !present {
		return result, workbench.ErrNotFound
	}
	auth, err := loadWorkbenchAuthorization(ctx, tx, subject)
	if err != nil {
		return result, err
	}
	var raw []byte
	var collectionID, scope, epoch, source, project, cost string
	var expires time.Time
	err = tx.QueryRowContext(ctx, `SELECT m.frozen_ref,m.collection_id::text,m.content_scope_revision,m.recovery_epoch,r.content_authorization_revision,r.membership_sha256,r.expires_at,q.source_system_id::text,'project-'||q.project_id,COALESCE(f.cost_project_ref,''),COALESCE(receipt.sequence,d.sequence)::text,d.occurred_at
 FROM runtime_reading_content_refs m JOIN runtime_reading_content_members cm USING(reading_id,request_ref)
 JOIN runtime_readings r ON r.id=m.reading_id JOIN runtime_reading_requests q ON q.reading_id=m.reading_id AND q.request_ref=m.request_ref
 JOIN source_content_manifests d ON d.source_system_id=m.source_system_id AND d.source_fact_id=m.source_fact_id AND d.revision=m.revision
 LEFT JOIN source_content_receipts receipt ON receipt.id=m.receipt_id
 LEFT JOIN workbench_economic_facts f ON f.id=cm.economic_fact_id
 WHERE m.reading_id=$1 AND m.request_ref=$2 AND r.owner_subject=$3 AND m.source_fact_id=$4 AND m.revision::text=$5 AND m.frozen_ref->>'content_ref'=$6 AND r.interpretation_revision IN ($7,$8)
 AND ((m.receipt_id IS NULL AND m.collection_id=d.collection_id AND m.content_scope_revision=d.content_scope_revision AND m.recovery_epoch=d.recovery_epoch)
 OR (receipt.id IS NOT NULL AND ROW(receipt.source_system_id,receipt.source_fact_id,receipt.revision)=ROW(d.source_system_id,d.source_fact_id,d.revision)
 AND ROW(receipt.collection_id,receipt.credential_id,receipt.registration_hash,receipt.content_scope_revision,receipt.recovery_epoch,receipt.coverage_from)=ROW(cm.collection_id,cm.credential_id,cm.registration_hash,cm.content_scope_revision,cm.recovery_epoch,cm.coverage_from)
 AND receipt.collection_id=m.collection_id AND receipt.content_scope_revision=m.content_scope_revision AND receipt.recovery_epoch=m.recovery_epoch AND receipt.source_sha256=d.source_sha256))
 AND ROW(q.source_system_id,q.source_instance,q.org_id,q.project_id,q.request_id)=ROW(d.source_system_id,d.source_instance,d.org_id,d.project_id,d.request_id)
 AND COALESCE(receipt.received_at,d.received_at)<=r.cutoff AND d.occurred_at>=r.period_start AND d.occurred_at<r.period_end AND m.frozen_ref=`+runtimeFrozenContentRefSQL, id, requestRef, subject, selector.SourceFactID, selector.Revision, selector.ContentRef, digestText(runtimeInterpretationWithContent), digestText(runtimeInterpretationWithContentReceipts)).Scan(&raw, &collectionID, &scope, &epoch, &result.ContentAuthorizationRevision, &result.MembershipRef, &expires, &source, &project, &cost, &result.ManifestSequence, &result.ManifestOccurredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return result, workbench.ErrNotFound
	}
	if err != nil {
		return result, err
	}
	if !now.Before(expires) {
		return result, workbench.ErrExpired
	}
	if auth.ContentRevision == "" || auth.ContentRevision != result.ContentAuthorizationRevision {
		return result, workbench.ErrAccessChanged
	}
	access := &workchain.Access{Authorization: auth}
	if !access.ContentAllowedFor(source, project, cost) {
		return result, workbench.ErrScopeForbidden
	}
	result.Reference, err = sourcefacts.DecodeFrozenManifestRef(raw)
	if err != nil {
		return result, err
	}
	if !contentSelectorMatches(selector, result.Reference) {
		return result, workbench.ErrNotFound
	}
	if result.Reference.Representation == nil {
		return result, workbench.ErrNotFound
	}
	expiry, err := time.Parse(sourcefacts.TimeFormat, result.Reference.ExpiresAt)
	if err != nil || !now.Before(expiry) {
		return result, workbench.ErrContentExpired
	}
	// Collection control uses the source advisory lock. Acquire it only for this
	// short final admission and then read the current collection after the lock.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock_shared(hashtextextended('source-facts:'||$1,0))`, source); err != nil {
		return result, err
	}
	result.Collection, err = loadContentCollection(ctx, tx, collectionID, false)
	if err != nil {
		return result, err
	}
	c := result.Collection
	var sourceActive bool
	if err = tx.QueryRowContext(ctx, `SELECT status='active' FROM source_systems WHERE id=$1::uuid`, source).Scan(&sourceActive); err != nil {
		return result, err
	}
	if !sourceActive {
		return result, workbench.ErrAccessChanged
	}
	if err = validateCurrentRuntimeContentBindings(ctx, tx, id); err != nil {
		return result, err
	}
	if c.Status != "active" || c.RequiresAttention || c.ContentScopeRevision != scope || c.RecoveryEpoch != epoch || c.SourceSystemID != result.Reference.SourceSystemID || c.SourceInstance != result.Reference.SourceInstance || c.OrgID != result.Reference.OrgID || c.ProjectID != result.Reference.ProjectID {
		return result, workbench.ErrAccessChanged
	}
	var digest string
	if err = tx.QueryRowContext(ctx, runtimeManifestDigestWithContentReceiptsSQL, id).Scan(&digest); err != nil {
		return result, err
	}
	if digest != result.MembershipRef {
		return result, fmt.Errorf("runtime content membership is invalid")
	}
	return result, tx.Rollback()
}

func contentSelectorMatches(selector, ref sourcefacts.FrozenManifestRef) bool {
	if selector.ContentRef != ref.ContentRef || selector.SourceFactID != ref.SourceFactID || selector.Revision != ref.Revision {
		return false
	}
	for _, p := range [][2]string{{selector.SourceSystemID, ref.SourceSystemID}, {selector.SourceInstance, ref.SourceInstance}, {selector.OrgID, ref.OrgID}, {selector.ProjectID, ref.ProjectID}, {selector.RequestID, ref.RequestID}, {selector.SourceSHA256, ref.SourceSHA256}, {selector.CaptureStatus, ref.CaptureStatus}, {selector.ExpiresAt, ref.ExpiresAt}} {
		if p[0] != "" && p[0] != p[1] {
			return false
		}
	}
	return selector.Representation == nil || reflect.DeepEqual(selector.Representation, ref.Representation)
}

// Restoration validates historical frozen facts and counters, never today's
// user grants or collection activation state.
func validateRuntimeContentsRestore(ctx context.Context, q workspaceQueryer) error {
	var valid bool
	err := q.QueryRowContext(ctx, `SELECT NOT EXISTS(
 SELECT 1 FROM runtime_readings r WHERE
 (r.content_authorization_revision<>'') IS DISTINCT FROM EXISTS(SELECT 1 FROM runtime_reading_content_members m WHERE m.reading_id=r.id)
 OR (r.content_authorization_revision<>'' AND (r.content_authorization_revision !~ '^sha256:[0-9a-f]{64}$' OR r.interpretation_revision NOT IN ($1,$2)))
 OR (r.content_authorization_revision='' AND r.interpretation_revision IN ($1,$2))
 OR (r.interpretation_revision=$2) IS DISTINCT FROM EXISTS(SELECT 1 FROM runtime_reading_content_refs cr WHERE cr.reading_id=r.id AND cr.receipt_id IS NOT NULL)
 ) AND NOT EXISTS(
 SELECT 1 FROM runtime_reading_content_refs m LEFT JOIN runtime_reading_content_members cm USING(reading_id,request_ref)
 LEFT JOIN runtime_reading_requests x USING(reading_id,request_ref) LEFT JOIN runtime_readings r ON r.id=m.reading_id
 LEFT JOIN source_content_manifests d ON d.source_system_id=m.source_system_id AND d.source_fact_id=m.source_fact_id AND d.revision=m.revision
 LEFT JOIN source_content_receipts receipt ON receipt.id=m.receipt_id
 WHERE cm.request_ref IS NULL OR x.request_ref IS NULL OR r.id IS NULL OR d.source_fact_id IS NULL
 OR (m.receipt_id IS NULL AND (m.collection_id<>d.collection_id OR m.content_scope_revision<>d.content_scope_revision OR m.recovery_epoch<>d.recovery_epoch))
 OR (m.receipt_id IS NOT NULL AND (receipt.id IS NULL OR ROW(receipt.source_system_id,receipt.source_fact_id,receipt.revision) IS DISTINCT FROM ROW(m.source_system_id,m.source_fact_id,m.revision)
 OR ROW(receipt.collection_id,receipt.credential_id,receipt.registration_hash,receipt.content_scope_revision,receipt.recovery_epoch,receipt.coverage_from) IS DISTINCT FROM ROW(cm.collection_id,cm.credential_id,cm.registration_hash,cm.content_scope_revision,cm.recovery_epoch,cm.coverage_from)))
 OR m.collection_id<>cm.collection_id OR m.content_scope_revision<>cm.content_scope_revision OR m.recovery_epoch<>cm.recovery_epoch
 OR ROW(x.source_system_id,x.source_instance,x.org_id,x.project_id,x.request_id) IS DISTINCT FROM ROW(d.source_system_id,d.source_instance,d.org_id,d.project_id,d.request_id)
 OR COALESCE(receipt.received_at,d.received_at)>r.cutoff OR d.occurred_at<r.period_start OR d.occurred_at>=r.period_end
 OR m.frozen_ref IS DISTINCT FROM `+runtimeFrozenContentRefSQL+`
 OR (m.receipt_id IS NULL AND EXISTS(SELECT 1 FROM source_content_manifests n WHERE n.source_system_id=d.source_system_id AND n.source_fact_id=d.source_fact_id AND n.revision>d.revision AND n.received_at<=r.cutoff))
 OR (m.receipt_id IS NOT NULL AND EXISTS(SELECT 1 FROM source_content_receipts n WHERE n.source_system_id=d.source_system_id AND n.source_fact_id=d.source_fact_id AND n.revision>d.revision AND n.received_at<=r.cutoff
 AND ROW(n.collection_id,n.credential_id,n.registration_hash,n.content_scope_revision,n.recovery_epoch,n.coverage_from)=ROW(cm.collection_id,cm.credential_id,cm.registration_hash,cm.content_scope_revision,cm.recovery_epoch,cm.coverage_from)))
 ) AND NOT EXISTS(
 SELECT 1 FROM runtime_reading_content_members m LEFT JOIN runtime_reading_requests d USING(reading_id,request_ref)
 LEFT JOIN runtime_readings r ON r.id=m.reading_id LEFT JOIN workbench_economic_facts f ON f.id=m.economic_fact_id
 LEFT JOIN source_content_collections c ON c.id=m.collection_id
 LEFT JOIN evidence_events e ON e.id=f.evidence_event_id
 WHERE d.request_ref IS NULL OR r.id IS NULL OR c.id IS NULL
 OR ROW(d.source_system_id,d.source_instance,d.org_id,d.project_id) IS DISTINCT FROM ROW(c.source_system_id,c.source_instance,c.org_id,c.project_id)
 OR m.credential_id='' OR m.registration_hash !~ '^sha256:[0-9a-f]{64}$' OR m.content_scope_revision='' OR m.recovery_epoch !~ '^[1-9][0-9]{0,18}$'
 OR jsonb_typeof(m.coverage_from)<>'object'
 OR m.eligible_at<r.period_start OR m.eligible_at>=r.period_end
 OR (m.economic_fact_id IS NOT NULL AND (f.id IS NULL OR e.id IS NULL OR f.projected_at>r.cutoff OR e.ingested_at>r.cutoff OR NOT (`+sourceEconomicJoinSQL+`)))
 OR (m.member_basis='content_manifest' AND m.eligible_at IS DISTINCT FROM
 (SELECT max(cm.occurred_at) FROM runtime_reading_content_refs cr JOIN source_content_manifests cm USING(source_system_id,source_fact_id,revision) WHERE cr.reading_id=m.reading_id AND cr.request_ref=m.request_ref))
 OR (m.member_basis='exact_execution_association' AND (f.id IS NULL OR m.eligible_at IS DISTINCT FROM f.occurred_at OR EXISTS(SELECT 1 FROM runtime_reading_content_refs cr WHERE cr.reading_id=m.reading_id AND cr.request_ref=m.request_ref)))
 OR m.member_basis NOT IN ('content_manifest','exact_execution_association')
 ) AND NOT EXISTS(
 SELECT 1 FROM runtime_reading_requests q LEFT JOIN runtime_reading_content_members m USING(reading_id,request_ref)
 WHERE (q.observation_count=0 AND (q.entry_at IS NOT NULL OR q.last_observed_at IS NOT NULL OR q.content_eligible_at IS NULL OR m.request_ref IS NULL OR q.content_eligible_at IS DISTINCT FROM m.eligible_at))
 OR (q.observation_count>0 AND (q.last_observed_at IS NULL OR q.content_eligible_at IS NOT NULL))
 ) AND (SELECT count(*)=3 FROM pg_trigger WHERE (tgrelid,tgname) IN (
 ('runtime_reading_requests'::regclass,'runtime_content_request_eligibility'),
 ('runtime_reading_content_members'::regclass,'runtime_content_member_eligibility'),
 ('runtime_reading_content_refs'::regclass,'runtime_content_ref_eligibility'))
 AND tgenabled='A' AND tgdeferrable AND tginitdeferred)`, digestText(runtimeInterpretationWithContent), digestText(runtimeInterpretationWithContentReceipts)).Scan(&valid)
	if err != nil {
		return fmt.Errorf("validate restored content membership: %w", err)
	}
	if !valid {
		return fmt.Errorf("restored content eligibility, frozen references or authorization domains are invalid")
	}
	return nil
}

// Every member, including manifest-free I01 membership, freezes one exact
// active binding. The ingestion schema permits only one active full scope.
func validateCurrentRuntimeContentBindings(ctx context.Context, q workspaceQueryer, id string) error {
	var valid bool
	err := q.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM runtime_reading_content_members m
 LEFT JOIN source_content_collections c ON c.id=m.collection_id LEFT JOIN source_systems s ON s.id=c.source_system_id
 WHERE m.reading_id=$1 AND (c.id IS NULL OR c.status<>'active' OR c.requires_attention OR s.status<>'active'
 OR ROW(m.credential_id,m.registration_hash,m.content_scope_revision,m.recovery_epoch,m.coverage_from)
 IS DISTINCT FROM ROW(c.credential_id,c.registration_hash,c.content_scope_revision,c.recovery_epoch,c.coverage_from)))`, id).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return workbench.ErrAccessChanged
	}
	return nil
}
