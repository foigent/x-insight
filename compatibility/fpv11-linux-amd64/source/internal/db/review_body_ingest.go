package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"regexp"
	"strconv"
	"time"

	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/workevent"
	"github.com/foigent/x-insight/internal/workspace"
)

var reviewBodyRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var reviewBodyDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type WorkEventReviewBodyInput struct {
	CollectionID               string
	ExpectedCollectionRevision int64
	ScopeGeneration            string
	EventRef                   string
	EventRevision              int64
	EventHash                  string
	ContentKind                string
	ContentRef                 string
	ByteDomain                 string
	SHA256                     string
	Body                       []byte
}

type ReviewBodyReceipt struct {
	ContentRef string    `json:"content_ref"`
	SHA256     string    `json:"sha256"`
	PlainBytes int64     `json:"plain_bytes"`
	ExpiresAt  time.Time `json:"expires_at"`
	Outcome    string    `json:"outcome"`
}

type reviewBodyPolicy struct {
	Mode      string
	Retention int64
	Quota     int64
	MaxItem   int64
	Ref       string
	Vector    string
}

// Only a source-authenticated account identity may be supplied. Neither the
// OpenClaw public hooks nor an arbitrary UR user name qualifies.
type ReviewBodyAccountIdentity struct{ Authority, ID string }

func reviewBodyPolicyTx(ctx context.Context, tx *sql.Tx, scope workevent.Scope, contentKind string, account *ReviewBodyAccountIdentity, now time.Time) (reviewBodyPolicy, error) {
	var p reviewBodyPolicy
	var defaultVersion int64
	err := tx.QueryRowContext(ctx, `SELECT effective_mode,effective_retention_seconds,effective_project_quota_bytes,
 effective_max_item_bytes,effective_revision FROM review_capture_defaults
 WHERE singleton AND effective_revision>0 AND effective_at<=$1 FOR SHARE`, now).Scan(
		&p.Mode, &p.Retention, &p.Quota, &p.MaxItem, &defaultVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return p, reviewbody.ErrPolicy
	}
	if err != nil {
		return p, err
	}
	p.Ref = "xi:default"
	p.Vector = p.Ref + ":" + strconv.FormatInt(defaultVersion, 10)
	accountAuthority, accountID := "", ""
	if account != nil && account.Authority != "" && account.ID != "" {
		accountAuthority, accountID = account.Authority, account.ID
	}
	var exceptionRef, mode string
	var retention, maxItem, exceptionVersion int64
	err = tx.QueryRowContext(ctx, `SELECT policy_ref,effective_mode,effective_retention_seconds,effective_max_item_bytes,effective_revision
 FROM review_capture_exceptions WHERE effective_revision>0 AND effective_at<=$1 AND
 ((selector_kind='account_category' AND source_system_id=$2::uuid AND source_instance=$3 AND org_id=$4 AND project_id=$5
    AND account_authority=$7 AND account_id=$8 AND $7<>'' AND content_kind=$6) OR
  (selector_kind='account' AND source_system_id=$2::uuid AND source_instance=$3 AND org_id=$4 AND project_id=$5
    AND account_authority=$7 AND account_id=$8 AND $7<>'' AND content_kind='') OR
  (selector_kind='source_project_category' AND source_system_id=$2::uuid AND source_instance=$3 AND org_id=$4 AND project_id=$5 AND content_kind=$6) OR
  (selector_kind='source_project' AND source_system_id=$2::uuid AND source_instance=$3 AND org_id=$4 AND project_id=$5 AND content_kind='') OR
  (selector_kind='tenant_category' AND content_kind=$6))
 ORDER BY CASE selector_kind WHEN 'account_category' THEN 1 WHEN 'account' THEN 2
  WHEN 'source_project_category' THEN 3 WHEN 'source_project' THEN 4 ELSE 5 END LIMIT 1 FOR SHARE`,
		now, scope.SourceSystemID, scope.SourceInstance, scope.OrgID, scope.ProjectID, contentKind, accountAuthority, accountID).
		Scan(&exceptionRef, &mode, &retention, &maxItem, &exceptionVersion)
	if err == nil {
		p.Mode, p.Retention, p.MaxItem = mode, retention, maxItem
		p.Ref = exceptionRef
		p.Vector = "xi:" + exceptionRef + ":" + strconv.FormatInt(exceptionVersion, 10) + ":default:" + strconv.FormatInt(defaultVersion, 10)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return p, err
	}
	return p, nil
}

// New review copies require an administrator-established XI project. A
// source tuple or similar-looking numeric ID cannot silently start a second
// quota ledger for the same real project.
func reviewBodyQuotaScopeTx(ctx context.Context, tx *sql.Tx, scope workevent.Scope) (string, error) {
	var projectID string
	err := tx.QueryRowContext(ctx, `SELECT xi_project_id::text FROM review_capture_project_bindings WHERE
 source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 FOR SHARE`,
		scope.SourceSystemID, scope.SourceInstance, scope.OrgID, scope.ProjectID).Scan(&projectID)
	if err == nil {
		return "project:" + projectID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return "", reviewbody.ErrProjectBindingPending
}

func reviewBodySourceQuotaScope(sourceID, instance, orgID, projectID string) string {
	digest := sha256.Sum256([]byte(sourceID + "\x00" + instance + "\x00" + orgID + "\x00" + projectID))
	return "source:" + hex.EncodeToString(digest[:])
}

func authorizeWorkEventBodyTx(ctx context.Context, tx *sql.Tx, identityID string, item WorkEventReviewBodyInput) (workevent.Scope, error) {
	var scope workevent.Scope
	var bindingStatus, bodyStatus, sourceStatus, sourceType string
	var sourceID string
	err := tx.QueryRowContext(ctx, `SELECT source_system_id::text FROM work_event_collections WHERE id=$1::uuid`, item.CollectionID).Scan(&sourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return scope, reviewbody.ErrScope
	}
	if err != nil {
		return scope, err
	}
	if err = sourceLock(ctx, tx, sourceID); err != nil {
		return scope, err
	}
	err = tx.QueryRowContext(ctx, `SELECT c.source_system_id::text,c.source_instance,c.org_id,c.project_id,b.status,rb.status,s.status,s.source_type
 FROM work_event_ingest_bindings b JOIN review_body_ingest_bindings rb
 ON rb.service_identity_id=b.service_identity_id AND rb.collection_id=b.collection_id
 JOIN work_event_collections c ON c.id=b.collection_id JOIN source_systems s ON s.id=c.source_system_id
 WHERE b.service_identity_id=$1 AND b.collection_id=$2::uuid AND c.source_system_id=$5::uuid AND c.status='active' AND
 c.revision=$3 AND c.scope_generation=$4 FOR SHARE OF b,rb,c,s`, identityID, item.CollectionID,
		item.ExpectedCollectionRevision, item.ScopeGeneration, sourceID).Scan(&scope.SourceSystemID, &scope.SourceInstance,
		&scope.OrgID, &scope.ProjectID, &bindingStatus, &bodyStatus, &sourceStatus, &sourceType)
	if errors.Is(err, sql.ErrNoRows) {
		return scope, reviewbody.ErrScope
	}
	if err != nil {
		return scope, err
	}
	if bindingStatus != "active" || bodyStatus != "active" || sourceStatus != "active" || sourceType != "agent_runtime" {
		return scope, reviewbody.ErrScope
	}
	var storedHash string
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT event_hash,event_bytes FROM work_event_documents WHERE
 source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND event_ref=$5 AND revision=$6`,
		scope.SourceSystemID, scope.SourceInstance, scope.OrgID, scope.ProjectID, item.EventRef, item.EventRevision).Scan(&storedHash, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return scope, reviewbody.ErrScope
	}
	if err != nil {
		return scope, err
	}
	if storedHash != item.EventHash {
		return scope, reviewbody.ErrConflict
	}
	event, err := workevent.DecodeEvent(raw)
	if err != nil {
		return scope, err
	}
	linked := false
	for _, ref := range event.ContentRefs {
		if ref.Kind == item.ContentKind && ref.Ref == item.ContentRef && ref.SHA256 == item.SHA256 {
			linked = true
			break
		}
	}
	if !linked {
		return scope, reviewbody.ErrScope
	}
	return scope, nil
}

func validateWorkEventReviewBody(item WorkEventReviewBodyInput) error {
	if !workEventUUID.MatchString(item.CollectionID) || item.ExpectedCollectionRevision < 1 ||
		!workEventGeneration.MatchString(item.ScopeGeneration) || !reviewBodyRefPattern.MatchString(item.EventRef) ||
		item.EventRevision < 1 || !reviewBodyDigestPattern.MatchString(item.EventHash) ||
		!reviewBodyRefPattern.MatchString(item.ContentRef) || !reviewBodyDigestPattern.MatchString(item.SHA256) ||
		len(item.Body) > 32<<20 || !validOpenClawByteDomain(item.ContentKind, item.ByteDomain) {
		return reviewbody.ErrInvalid
	}
	digest := sha256.Sum256(item.Body)
	if item.SHA256 != "sha256:"+hex.EncodeToString(digest[:]) {
		return reviewbody.ErrIntegrity
	}
	return nil
}

func validOpenClawByteDomain(kind, domain string) bool {
	switch kind {
	case "user_input":
		return domain == "openclaw.message_received.utf8.v1"
	case "user_output":
		return domain == "openclaw.message_sent.utf8.v1"
	case "model_input":
		return domain == "openclaw.llm_input.utf8.v1" || domain == "openclaw.llm_input.canonical-json.v1"
	case "model_output":
		return domain == "openclaw.llm_output.utf8.v1" || domain == "openclaw.llm_output.canonical-json.v1"
	case "tool_input":
		return domain == "openclaw.before_tool_call.canonical-json.v1"
	case "tool_output":
		return domain == "openclaw.after_tool_call.canonical-json.v1"
	default:
		return false // resource bytes require a separately verified source
	}
}

type bodyReservation struct {
	Identity reviewbody.Identity
	Receipt  ReviewBodyReceipt
	Accepted bool
}

// CommitAuthorizedWorkEventReviewBody first commits a fixed-deadline quota
// reservation, then seals and atomically publishes the copy. A retry reuses
// the first reservation; an expired lease cannot be revived.
func (s *WorkspaceStore) CommitAuthorizedWorkEventReviewBody(ctx context.Context, digest string, item WorkEventReviewBodyInput, keys reviewbody.KeyRing, now time.Time) (ReviewBodyReceipt, error) {
	if s == nil || s.db == nil || now.IsZero() {
		return ReviewBodyReceipt{}, reviewbody.ErrInvalid
	}
	if err := validateWorkEventReviewBody(item); err != nil {
		return ReviewBodyReceipt{}, err
	}
	reserved, err := s.reserveWorkEventReviewBody(ctx, digest, item, now.UTC().Truncate(time.Microsecond))
	if err != nil {
		return ReviewBodyReceipt{}, err
	}
	if reserved.Accepted {
		return reserved.Receipt, nil
	}
	sealed, err := keys.Seal(reserved.Identity, item.Body, nil)
	if err != nil {
		return ReviewBodyReceipt{}, err
	}
	return s.publishWorkEventReviewBody(ctx, digest, item, reserved, sealed, time.Now().UTC().Truncate(time.Microsecond))
}

func (s *WorkspaceStore) reserveWorkEventReviewBody(ctx context.Context, digest string, item WorkEventReviewBodyInput, now time.Time) (bodyReservation, error) {
	var result bodyReservation
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	identity, expiry, revoked, err := queryServiceIdentityByDigestLock(ctx, tx, digest, true)
	if errors.Is(err, sql.ErrNoRows) {
		return result, workspace.ErrCredentialInvalid
	}
	if err != nil {
		return result, err
	}
	if err = checkWorkEventIngestIdentity(identity, expiry, revoked, now); err != nil {
		return result, err
	}
	scope, err := authorizeWorkEventBodyTx(ctx, tx, identity.ID, item)
	if err != nil {
		return result, err
	}
	policy, err := reviewBodyPolicyTx(ctx, tx, scope, item.ContentKind, nil, now)
	if err != nil {
		return result, err
	}
	if policy.Mode != "full_content" || int64(len(item.Body)) > policy.MaxItem {
		return result, reviewbody.ErrPolicy
	}
	quotaScope, err := reviewBodyQuotaScopeTx(ctx, tx, scope)
	if err != nil {
		return result, err
	}
	charged := int64(len(item.Body)) + 16
	if _, err = tx.ExecContext(ctx, `INSERT INTO review_body_project_usage(quota_scope)
 VALUES($1) ON CONFLICT DO NOTHING`, quotaScope); err != nil {
		return result, err
	}
	var stored, reserved int64
	if err = tx.QueryRowContext(ctx, `SELECT stored_bytes,reserved_bytes FROM review_body_project_usage WHERE
	 quota_scope=$1 FOR UPDATE`, quotaScope).Scan(&stored, &reserved); err != nil {
		return result, err
	}
	var state, priorDigest, priorEventHash, priorKind, priorDomain, priorVector string
	var priorSourceID, priorInstance, priorOrg, priorProject, priorEventRef, priorCollection string
	var priorLen, priorEventRevision int64
	var acceptedAt, expiresAt, lease time.Time
	err = tx.QueryRowContext(ctx, `SELECT state,digest,event_hash,content_kind,byte_domain,policy_version,plain_bytes,accepted_at,expires_at,lease_expires_at,
 source_system_id::text,source_instance,org_id,project_id,event_ref,event_revision,work_event_collection_id::text
 FROM review_body_operations WHERE content_ref=$1 FOR UPDATE`, item.ContentRef).Scan(&state, &priorDigest, &priorEventHash,
		&priorKind, &priorDomain, &priorVector, &priorLen, &acceptedAt, &expiresAt, &lease,
		&priorSourceID, &priorInstance, &priorOrg, &priorProject, &priorEventRef, &priorEventRevision, &priorCollection)
	if err == nil {
		if priorDigest != item.SHA256 || priorEventHash != item.EventHash || priorKind != item.ContentKind ||
			priorDomain != item.ByteDomain || priorLen != int64(len(item.Body)) || priorSourceID != scope.SourceSystemID ||
			priorInstance != scope.SourceInstance || priorOrg != scope.OrgID || priorProject != scope.ProjectID ||
			priorEventRef != item.EventRef || priorEventRevision != item.EventRevision || priorCollection != item.CollectionID {
			return result, reviewbody.ErrConflict
		}
		if state == "accepted" && now.Before(expiresAt) {
			result.Receipt = ReviewBodyReceipt{ContentRef: item.ContentRef, SHA256: item.SHA256,
				PlainBytes: priorLen, ExpiresAt: expiresAt, Outcome: "identical_replay"}
			result.Accepted = true
			return result, tx.Commit()
		}
		if state != "reserved" || !now.Before(lease) || !now.Before(expiresAt) {
			return result, reviewbody.ErrExpired
		}
		result.Identity = reviewbody.Identity{SourceKind: "work_event", SourceSystemID: scope.SourceSystemID,
			SourceInstance: scope.SourceInstance, OrgID: scope.OrgID, ProjectID: scope.ProjectID,
			EventRef: item.EventRef, EventRevision: item.EventRevision, EventHash: item.EventHash,
			ContentKind: item.ContentKind, ContentRef: item.ContentRef, ByteDomain: item.ByteDomain,
			PolicyVersion: priorVector, ExpiresAt: expiresAt}
		return result, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	if stored > policy.Quota-reserved || charged > policy.Quota-stored-reserved {
		return result, reviewbody.ErrQuota
	}
	expiresAt = now.Add(time.Duration(policy.Retention) * time.Second)
	lease = now.Add(10 * time.Minute)
	if lease.After(expiresAt) {
		lease = expiresAt
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO review_body_operations(content_ref,source_kind,work_event_collection_id,
 source_system_id,source_instance,org_id,project_id,quota_scope,event_ref,event_revision,event_hash,content_kind,byte_domain,
 digest,plain_bytes,charged_bytes,policy_ref,policy_version,accepted_at,expires_at,lease_expires_at,state)
 VALUES($1,'work_event',$2::uuid,$3::uuid,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,'reserved')`,
		item.ContentRef, item.CollectionID, scope.SourceSystemID, scope.SourceInstance, scope.OrgID, scope.ProjectID,
		quotaScope, item.EventRef, item.EventRevision, item.EventHash, item.ContentKind, item.ByteDomain, item.SHA256,
		len(item.Body), charged, policy.Ref, policy.Vector, now, expiresAt, lease)
	if err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE review_body_project_usage SET reserved_bytes=reserved_bytes+$2 WHERE quota_scope=$1`,
		quotaScope, charged); err != nil {
		return result, err
	}
	result.Identity = reviewbody.Identity{SourceKind: "work_event", SourceSystemID: scope.SourceSystemID,
		SourceInstance: scope.SourceInstance, OrgID: scope.OrgID, ProjectID: scope.ProjectID,
		EventRef: item.EventRef, EventRevision: item.EventRevision, EventHash: item.EventHash,
		ContentKind: item.ContentKind, ContentRef: item.ContentRef, ByteDomain: item.ByteDomain,
		PolicyVersion: policy.Vector, ExpiresAt: expiresAt}
	return result, tx.Commit()
}

func (s *WorkspaceStore) publishWorkEventReviewBody(ctx context.Context, digest string, item WorkEventReviewBodyInput,
	reserved bodyReservation, sealed reviewbody.Sealed, now time.Time) (ReviewBodyReceipt, error) {
	var receipt ReviewBodyReceipt
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return receipt, err
	}
	defer tx.Rollback()
	identity, expiry, revoked, err := queryServiceIdentityByDigestLock(ctx, tx, digest, true)
	if errors.Is(err, sql.ErrNoRows) {
		return receipt, workspace.ErrCredentialInvalid
	}
	if err != nil {
		return receipt, err
	}
	if err = checkWorkEventIngestIdentity(identity, expiry, revoked, now); err != nil {
		return receipt, err
	}
	scope, err := authorizeWorkEventBodyTx(ctx, tx, identity.ID, item)
	if err != nil {
		return receipt, err
	}
	policy, err := reviewBodyPolicyTx(ctx, tx, scope, item.ContentKind, nil, now)
	if err != nil {
		return receipt, err
	}
	if policy.Mode != "full_content" || policy.Vector != reserved.Identity.PolicyVersion {
		return receipt, reviewbody.ErrPolicy
	}
	quotaScope, err := reviewBodyQuotaScopeTx(ctx, tx, scope)
	if err != nil {
		return receipt, err
	}
	var held int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM review_body_project_usage WHERE quota_scope=$1 FOR UPDATE`, quotaScope).Scan(&held); err != nil {
		return receipt, err
	}
	var state, vector string
	var lease, expires time.Time
	var charged int64
	err = tx.QueryRowContext(ctx, `SELECT state,policy_version,lease_expires_at,expires_at,charged_bytes FROM review_body_operations WHERE content_ref=$1 FOR UPDATE`,
		item.ContentRef).Scan(&state, &vector, &lease, &expires, &charged)
	if err != nil {
		return receipt, err
	}
	if state != "reserved" || vector != reserved.Identity.PolicyVersion || !expires.Equal(reserved.Identity.ExpiresAt) ||
		!now.Before(lease) || !now.Before(expires) {
		return receipt, reviewbody.ErrExpired
	}
	if sealed.SHA256 != item.SHA256 || sealed.PlainBytes != int64(len(item.Body)) || int64(len(sealed.Ciphertext)) != charged {
		return receipt, reviewbody.ErrIntegrity
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO review_body_objects(content_ref,key_id,nonce,ciphertext) VALUES($1,$2,$3,$4)`,
		item.ContentRef, sealed.KeyID, sealed.Nonce, sealed.Ciphertext); err != nil {
		return receipt, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE review_body_operations SET state='accepted' WHERE content_ref=$1`, item.ContentRef); err != nil {
		return receipt, err
	}
	updated, err := tx.ExecContext(ctx, `UPDATE review_body_project_usage SET reserved_bytes=reserved_bytes-$2,stored_bytes=stored_bytes+$2 WHERE
 quota_scope=$1 AND reserved_bytes>=$2`, quotaScope, charged)
	if err != nil {
		return receipt, err
	}
	if affected, rowsErr := updated.RowsAffected(); rowsErr != nil || affected != 1 {
		return receipt, reviewbody.ErrIntegrity
	}
	if err = insertAccessAudit(ctx, tx, "service", identity.ID, "review_body.ingest", "review_body", item.ContentRef, "accepted", now); err != nil {
		return receipt, err
	}
	if err = tx.Commit(); err != nil {
		return receipt, err
	}
	return ReviewBodyReceipt{ContentRef: item.ContentRef, SHA256: item.SHA256, PlainBytes: int64(len(item.Body)),
		ExpiresAt: expires, Outcome: "accepted"}, nil
}
