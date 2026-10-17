package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workevent"
)

var urAccountID = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)

// The key owner is a UR-authenticated account association, not proof of the
// actual human actor. A missing or ambiguous exact-scope B identity remains
// unknown and falls back to the XI default/source policy.
func urCAccountIdentityTx(ctx context.Context, tx *sql.Tx, ref sourcefacts.FrozenManifestRef) (*ReviewBodyAccountIdentity, bool, error) {
	var keyType string
	contextRows, err := tx.QueryContext(ctx, `SELECT d.source_json->'core'->>'key_type'
 FROM source_fact_documents d WHERE d.source_system_id=$1::uuid AND d.source_instance=$2
 AND d.org_id=$3 AND d.project_id=$4 AND d.request_id=$5
 AND d.profile='unirouter.execution-context.v1' AND d.stage='authenticated'
 AND d.source_json->>'capture_status'='available' AND d.source_json->'core'->>'auth_kind'='api_key'
 AND NOT EXISTS(SELECT 1 FROM source_fact_documents newer WHERE newer.source_system_id=d.source_system_id
   AND newer.source_fact_id=d.source_fact_id AND newer.revision>d.revision)
 ORDER BY d.source_fact_id LIMIT 2`, ref.SourceSystemID, ref.SourceInstance, ref.OrgID, ref.ProjectID, ref.RequestID)
	if err != nil {
		return nil, false, err
	}
	contexts := 0
	for contextRows.Next() {
		contexts++
		if err = contextRows.Scan(&keyType); err != nil {
			contextRows.Close()
			return nil, false, err
		}
	}
	err = contextRows.Err()
	contextRows.Close()
	if err != nil {
		return nil, false, err
	}
	if contexts != 1 {
		return nil, false, nil
	}
	if keyType == "service" || keyType == "shared" {
		return nil, true, nil
	}
	if keyType != "personal" {
		return nil, false, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT d.source_json->'identity'->>'owner_user_id'
 FROM source_fact_documents d WHERE d.source_system_id=$1::uuid AND d.source_instance=$2
 AND d.org_id=$3 AND d.project_id=$4 AND d.request_id=$5
 AND d.profile='unirouter.execution-identity.v1' AND d.stage='authenticated'
 AND d.source_json->>'capture_status'='available'
 AND NOT EXISTS(SELECT 1 FROM source_fact_documents newer WHERE newer.source_system_id=d.source_system_id
   AND newer.source_fact_id=d.source_fact_id AND newer.revision>d.revision)
 ORDER BY d.source_fact_id LIMIT 2`, ref.SourceSystemID, ref.SourceInstance, ref.OrgID, ref.ProjectID, ref.RequestID)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var selected string
	count := 0
	for rows.Next() {
		var value sql.NullString
		if err = rows.Scan(&value); err != nil {
			return nil, false, err
		}
		count++
		if !value.Valid || !urAccountID.MatchString(value.String) || count > 1 {
			return nil, false, nil
		}
		selected = value.String
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	if selected == "" {
		return nil, false, nil
	}
	return &ReviewBodyAccountIdentity{Authority: "ur_key_owner", ID: selected}, true, nil
}

// An unresolved B identity can change an account-level policy decision. Only
// an effective exception in this exact source scope warrants holding C bytes.
func urCAccountExceptionPossibleTx(ctx context.Context, tx *sql.Tx, ref sourcefacts.FrozenManifestRef,
	contentKind string, now time.Time) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM review_capture_exceptions WHERE
 effective_revision>0 AND effective_at<=$1 AND selector_kind IN ('account','account_category')
 AND source_system_id=$2::uuid AND source_instance=$3 AND org_id=$4 AND project_id=$5
 AND account_authority='ur_key_owner' AND (content_kind='' OR content_kind=$6))`,
		now, ref.SourceSystemID, ref.SourceInstance, ref.OrgID, ref.ProjectID, contentKind).Scan(&exists)
	return exists, err
}

// CheckURCReviewCopy gates source I/O against the exact managed C collection,
// immutable manifest and current XI effective policy. The commit repeats all
// checks after the network request; no DB transaction spans source I/O.
func (s *WorkspaceStore) CheckURCReviewCopy(ctx context.Context, collectionID string,
	ref sourcefacts.FrozenManifestRef, now time.Time) error {
	if s == nil || s.db == nil || !workEventUUID.MatchString(collectionID) || now.IsZero() ||
		ref.Representation == nil || ref.CaptureStatus != "complete" || ref.Representation.SavedBytes > 32<<20 {
		return reviewbody.ErrInvalid
	}
	rawRef, err := json.Marshal(ref)
	if err != nil {
		return reviewbody.ErrInvalid
	}
	if _, err = sourcefacts.DecodeFrozenManifestRef(rawRef); err != nil {
		return reviewbody.ErrInvalid
	}
	if expiry, err := time.Parse(sourcefacts.TimeFormat, ref.ExpiresAt); err != nil || !now.Before(expiry) {
		return reviewbody.ErrExpired
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = sourceLock(ctx, tx, ref.SourceSystemID); err != nil {
		return err
	}
	var instance, orgID, projectID, status, sourceStatus string
	var attention bool
	var purposes []byte
	if err = tx.QueryRowContext(ctx, `SELECT c.source_instance,c.org_id,c.project_id,c.status,c.requires_attention,
 c.allowed_purposes,s.status FROM source_content_collections c JOIN source_systems s ON s.id=c.source_system_id
 WHERE c.id=$1::uuid AND c.source_system_id=$2::uuid FOR SHARE OF c,s`, collectionID, ref.SourceSystemID).
		Scan(&instance, &orgID, &projectID, &status, &attention, &purposes, &sourceStatus); err != nil {
		return err
	}
	if instance != ref.SourceInstance || orgID != ref.OrgID || projectID != ref.ProjectID ||
		status != "active" || sourceStatus != "active" || attention {
		return reviewbody.ErrScope
	}
	var allowed []string
	if json.Unmarshal(purposes, &allowed) != nil {
		return reviewbody.ErrScope
	}
	humanReview := false
	for _, p := range allowed {
		if p == sourcefacts.ContentPurposeHumanReview {
			humanReview = true
		}
	}
	if !humanReview {
		return reviewbody.ErrScope
	}
	var manifestRaw []byte
	var storedHash string
	if err = tx.QueryRowContext(ctx, `SELECT source_bytes,source_sha256 FROM source_content_manifests WHERE
 collection_id=$1::uuid AND source_system_id=$2::uuid AND source_fact_id=$3 AND revision=$4::numeric FOR SHARE`,
		collectionID, ref.SourceSystemID, ref.SourceFactID, ref.Revision).Scan(&manifestRaw, &storedHash); err != nil {
		return err
	}
	manifest, err := sourcefacts.DecodeContentManifest(manifestRaw)
	if err != nil || storedHash != ref.SourceSHA256 || sourcefacts.MatchContentManifestReference(ref,
		sourcefacts.ContentManifestEntry{Document: manifest, SourceSHA256: storedHash}) != nil {
		return reviewbody.ErrConflict
	}
	account, resolved, err := urCAccountIdentityTx(ctx, tx, ref)
	if err != nil {
		return err
	}
	policy, err := reviewBodyPolicyTx(ctx, tx, workevent.Scope{SourceSystemID: ref.SourceSystemID,
		SourceInstance: instance, OrgID: orgID, ProjectID: projectID}, manifest.ObservationPoint, account, now)
	if err != nil {
		return err
	}
	if !resolved {
		possible, err := urCAccountExceptionPossibleTx(ctx, tx, ref, manifest.ObservationPoint, now)
		if err != nil {
			return err
		}
		if possible {
			return reviewbody.ErrIdentityPending
		}
	}
	if policy.Mode != "full_content" || ref.Representation.SavedBytes > policy.MaxItem {
		return reviewbody.ErrPolicy
	}
	quotaScope, err := reviewBodyQuotaScopeTx(ctx, tx, workevent.Scope{SourceSystemID: ref.SourceSystemID,
		SourceInstance: instance, OrgID: orgID, ProjectID: projectID})
	if err != nil {
		return err
	}
	var stored, reserved int64
	err = tx.QueryRowContext(ctx, `SELECT stored_bytes,reserved_bytes FROM review_body_project_usage
 WHERE quota_scope=$1 FOR SHARE`, quotaScope).Scan(&stored, &reserved)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	charged := ref.Representation.SavedBytes + 16
	if stored > policy.Quota-reserved || charged > policy.Quota-stored-reserved {
		return reviewbody.ErrQuota
	}
	return tx.Commit()
}

// CommitURCReviewBody accepts only a complete C source representation already
// fetched through pullfeed's exact-header/length/hash verifier. It checks the
// immutable manifest and current XI policy again in the committing transaction.
// It never invokes UR execution-temporary retrieval or serves the live source.
func (s *WorkspaceStore) CommitURCReviewBody(ctx context.Context, collectionID string, ref sourcefacts.FrozenManifestRef,
	body []byte, keys reviewbody.KeyRing, now time.Time) (ReviewBodyReceipt, error) {
	var receipt ReviewBodyReceipt
	if s == nil || s.db == nil || !workEventUUID.MatchString(collectionID) || now.IsZero() ||
		ref.CaptureStatus != "complete" || ref.Representation == nil || len(body) > 32<<20 ||
		int64(len(body)) != ref.Representation.SavedBytes || keys.Validate() != nil {
		return receipt, reviewbody.ErrInvalid
	}
	rawRef, err := json.Marshal(ref)
	if err != nil {
		return receipt, reviewbody.ErrInvalid
	}
	if _, err = sourcefacts.DecodeFrozenManifestRef(rawRef); err != nil {
		return receipt, reviewbody.ErrInvalid
	}
	sourceExpiry, err := time.Parse(sourcefacts.TimeFormat, ref.ExpiresAt)
	if err != nil || !now.Before(sourceExpiry) {
		return receipt, reviewbody.ErrExpired
	}
	digest := sha256.Sum256(body)
	if ref.Representation.Digest != "sha256:"+hex.EncodeToString(digest[:]) {
		return receipt, reviewbody.ErrIntegrity
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return receipt, err
	}
	defer tx.Rollback()
	if err = sourceLock(ctx, tx, ref.SourceSystemID); err != nil {
		return receipt, err
	}
	var sourceID, instance, orgID, projectID, status, sourceStatus string
	var attention bool
	var purposes []byte
	err = tx.QueryRowContext(ctx, `SELECT c.source_system_id::text,c.source_instance,c.org_id,c.project_id,c.status,c.requires_attention,
 c.allowed_purposes,s.status FROM source_content_collections c JOIN source_systems s ON s.id=c.source_system_id
 WHERE c.id=$1::uuid FOR SHARE OF c,s`, collectionID).Scan(&sourceID, &instance, &orgID, &projectID, &status, &attention, &purposes, &sourceStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return receipt, reviewbody.ErrScope
	}
	if err != nil {
		return receipt, err
	}
	var allowed []string
	if json.Unmarshal(purposes, &allowed) != nil {
		return receipt, reviewbody.ErrScope
	}
	humanReview := false
	for _, value := range allowed {
		if value == sourcefacts.ContentPurposeHumanReview {
			humanReview = true
		}
	}
	if sourceID != ref.SourceSystemID || instance != ref.SourceInstance || orgID != ref.OrgID || projectID != ref.ProjectID ||
		status != "active" || sourceStatus != "active" || attention || !humanReview {
		return receipt, reviewbody.ErrScope
	}
	var manifestRaw []byte
	var storedHash string
	err = tx.QueryRowContext(ctx, `SELECT source_bytes,source_sha256 FROM source_content_manifests WHERE
 collection_id=$1::uuid AND source_system_id=$2::uuid AND source_fact_id=$3 AND revision=$4::numeric FOR SHARE`,
		collectionID, sourceID, ref.SourceFactID, ref.Revision).Scan(&manifestRaw, &storedHash)
	if errors.Is(err, sql.ErrNoRows) {
		return receipt, reviewbody.ErrScope
	}
	if err != nil {
		return receipt, err
	}
	manifest, err := sourcefacts.DecodeContentManifest(manifestRaw)
	if err != nil || storedHash != ref.SourceSHA256 || sourcefacts.MatchContentManifestReference(ref,
		sourcefacts.ContentManifestEntry{Document: manifest, SourceSHA256: storedHash}) != nil {
		return receipt, reviewbody.ErrConflict
	}
	scope := workevent.Scope{SourceSystemID: sourceID, SourceInstance: instance, OrgID: orgID, ProjectID: projectID}
	account, resolved, err := urCAccountIdentityTx(ctx, tx, ref)
	if err != nil {
		return receipt, err
	}
	policy, err := reviewBodyPolicyTx(ctx, tx, scope, manifest.ObservationPoint, account, now)
	if err != nil {
		return receipt, err
	}
	if !resolved {
		possible, err := urCAccountExceptionPossibleTx(ctx, tx, ref, manifest.ObservationPoint, now)
		if err != nil {
			return receipt, err
		}
		if possible {
			return receipt, reviewbody.ErrIdentityPending
		}
	}
	if policy.Mode != "full_content" || int64(len(body)) > policy.MaxItem {
		return receipt, reviewbody.ErrPolicy
	}
	quotaScope, err := reviewBodyQuotaScopeTx(ctx, tx, scope)
	if err != nil {
		return receipt, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO review_body_project_usage(quota_scope)
 VALUES($1) ON CONFLICT DO NOTHING`, quotaScope); err != nil {
		return receipt, err
	}
	var stored, reserved int64
	if err = tx.QueryRowContext(ctx, `SELECT stored_bytes,reserved_bytes FROM review_body_project_usage WHERE
	 quota_scope=$1 FOR UPDATE`, quotaScope).Scan(&stored, &reserved); err != nil {
		return receipt, err
	}
	var priorSource, priorInstance, priorOrg, priorProject, priorFact, priorRevision, priorHash, priorDigest, priorKind, priorDomain, priorCollection, priorState string
	var priorBytes int64
	var priorExpiry time.Time
	err = tx.QueryRowContext(ctx, `SELECT source_system_id::text,source_instance,org_id,project_id,source_fact_id,
 source_revision::text,source_sha256,digest,content_kind,byte_domain,content_collection_id::text,state,plain_bytes,expires_at
 FROM review_body_operations WHERE content_ref=$1 FOR UPDATE`, ref.ContentRef).Scan(&priorSource, &priorInstance, &priorOrg, &priorProject,
		&priorFact, &priorRevision, &priorHash, &priorDigest, &priorKind, &priorDomain, &priorCollection, &priorState, &priorBytes, &priorExpiry)
	if err == nil {
		if priorSource != sourceID || priorInstance != instance || priorOrg != orgID || priorProject != projectID ||
			priorFact != ref.SourceFactID || priorRevision != ref.Revision || priorHash != ref.SourceSHA256 ||
			priorDigest != ref.Representation.Digest || priorKind != manifest.ObservationPoint ||
			priorDomain != ref.Representation.ByteDomain || priorCollection != collectionID || priorBytes != int64(len(body)) {
			return receipt, reviewbody.ErrConflict
		}
		if priorState != "accepted" || !now.Before(priorExpiry) {
			return receipt, reviewbody.ErrExpired
		}
		if err = tx.Commit(); err != nil {
			return receipt, err
		}
		return ReviewBodyReceipt{ContentRef: ref.ContentRef, SHA256: priorDigest, PlainBytes: priorBytes, ExpiresAt: priorExpiry, Outcome: "identical_replay"}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return receipt, err
	}
	charged := int64(len(body)) + 16
	if stored > policy.Quota-reserved || charged > policy.Quota-stored-reserved {
		return receipt, reviewbody.ErrQuota
	}
	expires := now.Add(time.Duration(policy.Retention) * time.Second).Truncate(time.Microsecond)
	owner := reviewbody.Identity{SourceKind: "ur_c", SourceSystemID: sourceID, SourceInstance: instance, OrgID: orgID, ProjectID: projectID,
		SourceFactID: ref.SourceFactID, SourceRevision: ref.Revision, SourceSHA256: ref.SourceSHA256, ContentKind: manifest.ObservationPoint,
		ContentRef: ref.ContentRef, ByteDomain: ref.Representation.ByteDomain, PolicyVersion: policy.Vector, ExpiresAt: expires}
	sealed, err := keys.Seal(owner, body, nil)
	if err != nil {
		return receipt, err
	}
	if sealed.SHA256 != ref.Representation.Digest || sealed.PlainBytes != int64(len(body)) || int64(len(sealed.Ciphertext)) != charged {
		return receipt, reviewbody.ErrIntegrity
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO review_body_operations(content_ref,source_kind,content_collection_id,
 source_system_id,source_instance,org_id,project_id,quota_scope,source_fact_id,source_revision,source_sha256,content_kind,byte_domain,
 digest,plain_bytes,charged_bytes,policy_ref,policy_version,accepted_at,expires_at,lease_expires_at,state)
 VALUES($1,'ur_c',$2::uuid,$3::uuid,$4,$5,$6,$7,$8,$9::numeric,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$18,'accepted')`,
		ref.ContentRef, collectionID, sourceID, instance, orgID, projectID, quotaScope, ref.SourceFactID, ref.Revision, ref.SourceSHA256,
		manifest.ObservationPoint, ref.Representation.ByteDomain, sealed.SHA256, sealed.PlainBytes, charged, policy.Ref, policy.Vector, now, expires); err != nil {
		return receipt, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO review_body_objects(content_ref,key_id,nonce,ciphertext) VALUES($1,$2,$3,$4)`,
		ref.ContentRef, sealed.KeyID, sealed.Nonce, sealed.Ciphertext); err != nil {
		return receipt, err
	}
	updated, err := tx.ExecContext(ctx, `UPDATE review_body_project_usage SET stored_bytes=stored_bytes+$2 WHERE
 quota_scope=$1`, quotaScope, charged)
	if err != nil {
		return receipt, err
	}
	if affected, rowsErr := updated.RowsAffected(); rowsErr != nil || affected != 1 {
		return receipt, reviewbody.ErrIntegrity
	}
	if err = insertAccessAudit(ctx, tx, "system", "ur_c_copy", "review_body.copy", "review_body", ref.ContentRef, "accepted", now); err != nil {
		return receipt, err
	}
	if err = tx.Commit(); err != nil {
		return receipt, err
	}
	return ReviewBodyReceipt{ContentRef: ref.ContentRef, SHA256: sealed.SHA256, PlainBytes: sealed.PlainBytes, ExpiresAt: expires, Outcome: "accepted"}, nil
}
