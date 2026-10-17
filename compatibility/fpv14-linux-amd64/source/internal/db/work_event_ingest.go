package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/workevent"
	"github.com/foigent/x-insight/internal/workspace"
)

type WorkEventIngestItem struct {
	CollectionID               string
	ExpectedCollectionRevision int64
	ScopeGeneration            string
	Kind                       string
	Document                   []byte
}

type WorkEventIngestReceipt struct {
	Kind        string          `json:"kind"`
	Reporter    workevent.Scope `json:"reporter"`
	DocumentRef string          `json:"document_ref"`
	Revision    int64           `json:"revision,omitempty"`
	SHA256      string          `json:"sha256"`
	Outcome     string          `json:"outcome"`
}

type WorkEventIngestTarget struct {
	Scope workevent.Scope `json:"scope"`
	Label string          `json:"label"`
}

type WorkEventIngestBindingView struct {
	CollectionID       string                   `json:"collection_id"`
	ProducerProjectKey string                   `json:"producer_project_key"`
	Reporter           workevent.Scope          `json:"reporter"`
	SourceLabel        string                   `json:"source_label"`
	ProjectLabel       string                   `json:"project_label"`
	ScopeGeneration    string                   `json:"scope_generation"`
	CollectionRevision int64                    `json:"collection_revision"`
	Profile            string                   `json:"profile"`
	URTarget           *WorkEventIngestTarget   `json:"ur_target,omitempty"`
	BodyCapture        WorkEventBodyCaptureView `json:"body_capture"`
}

type WorkEventBodyCaptureView struct {
	Mode             string                              `json:"mode"`
	PolicyVersion    string                              `json:"policy_version,omitempty"`
	MaxItemBytes     int64                               `json:"max_item_bytes,omitempty"`
	RetentionSeconds int64                               `json:"retention_seconds,omitempty"`
	ByKind           map[string]WorkEventBodyCaptureRule `json:"by_kind,omitempty"`
}

type WorkEventBodyCaptureRule struct {
	Mode             string `json:"mode"`
	PolicyVersion    string `json:"policy_version,omitempty"`
	MaxItemBytes     int64  `json:"max_item_bytes,omitempty"`
	RetentionSeconds int64  `json:"retention_seconds,omitempty"`
}

type WorkEventIngestConfig struct {
	Version             string                       `json:"version"`
	CredentialExpiresAt time.Time                    `json:"credential_expires_at"`
	ManagedRenewal      bool                         `json:"managed_renewal"`
	InstallationID      string                       `json:"installation_id,omitempty"`
	Items               []WorkEventIngestBindingView `json:"items"`
}

func checkWorkEventIngestIdentity(identity workspace.ServiceIdentity, tokenExpiry time.Time, tokenRevoked *time.Time, now time.Time) error {
	if identity.Status == "revoked" || tokenRevoked != nil {
		return workspace.ErrCredentialRevoked
	}
	if !now.Before(tokenExpiry) || (identity.ExpiresAt != nil && !now.Before(*identity.ExpiresAt)) {
		return workspace.ErrCredentialExpired
	}
	if identity.Status != "active" || len(identity.Permissions) != 1 || identity.Permissions[0] != "work_event.ingest" {
		return workspace.ErrCredentialInvalid
	}
	return nil
}

func loadWorkEventCollectionShared(ctx context.Context, tx *sql.Tx, id string) (WorkEventCollection, error) {
	var c WorkEventCollection
	err := tx.QueryRowContext(ctx, `SELECT id::text,source_system_id::text,source_instance,org_id,project_id,profile,scope_generation,status,revision
FROM work_event_collections WHERE id=$1::uuid FOR SHARE`, id).Scan(&c.ID, &c.Scope.SourceSystemID, &c.Scope.SourceInstance,
		&c.Scope.OrgID, &c.Scope.ProjectID, &c.Profile, &c.ScopeGeneration, &c.Status, &c.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return c, workevent.ErrScopeChanged
	}
	return c, err
}

func (s *WorkspaceStore) ListWorkEventIngestConfig(ctx context.Context, digest string, now time.Time) (WorkEventIngestConfig, error) {
	result := WorkEventIngestConfig{Version: "xinsight.work-event-ingest-config.v1", Items: []WorkEventIngestBindingView{}}
	if s == nil || s.db == nil || now.IsZero() {
		return result, workevent.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	identity, expires, revoked, err := queryServiceIdentityByDigestLock(ctx, tx, digest, true)
	if errors.Is(err, sql.ErrNoRows) {
		return result, workspace.ErrCredentialInvalid
	}
	if err != nil {
		return result, err
	}
	if err = checkWorkEventIngestIdentity(identity, expires, revoked, now.UTC()); err != nil {
		return result, err
	}
	result.CredentialExpiresAt = expires.UTC()
	var installationID sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT public.xinsight_managed_work_install_id($1)`, identity.ID).Scan(&installationID); err != nil {
		return result, err
	}
	result.InstallationID, result.ManagedRenewal = installationID.String, installationID.Valid
	rows, err := tx.QueryContext(ctx, `SELECT c.id::text,c.source_system_id::text,c.source_instance,c.org_id,c.project_id,
 s.display_name,b.producer_project_key,b.project_label,c.scope_generation,c.revision,c.profile,
 t.target_source_system_id::text,t.target_source_instance,t.target_org_id,t.target_project_id,t.target_label
 FROM work_event_ingest_bindings b
 JOIN work_event_collections c ON c.id=b.collection_id
 JOIN source_systems s ON s.id=c.source_system_id
 LEFT JOIN work_event_ur_targets t ON t.collection_id=c.id AND t.status='active'
   AND EXISTS(SELECT 1 FROM source_systems ur WHERE ur.id=t.target_source_system_id
              AND ur.source_type='unirouter' AND ur.status='active')
   AND EXISTS(SELECT 1 FROM source_fact_streams ur_stream WHERE ur_stream.source_system_id=t.target_source_system_id
              AND ur_stream.source_instance=t.target_source_instance AND ur_stream.org_id=t.target_org_id
              AND ur_stream.project_id=t.target_project_id AND ur_stream.status='active')
 WHERE b.service_identity_id=$1 AND b.status='active' AND c.status='active' AND s.status='active'
 ORDER BY s.display_name,b.project_label,c.id`, identity.ID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var item WorkEventIngestBindingView
		var targetID, targetInstance, targetOrg, targetProject, targetLabel sql.NullString
		if err = rows.Scan(&item.CollectionID, &item.Reporter.SourceSystemID, &item.Reporter.SourceInstance,
			&item.Reporter.OrgID, &item.Reporter.ProjectID, &item.SourceLabel, &item.ProducerProjectKey, &item.ProjectLabel,
			&item.ScopeGeneration, &item.CollectionRevision, &item.Profile,
			&targetID, &targetInstance, &targetOrg, &targetProject, &targetLabel); err != nil {
			return result, err
		}
		if targetID.Valid {
			item.URTarget = &WorkEventIngestTarget{Scope: workevent.Scope{
				SourceSystemID: targetID.String, SourceInstance: targetInstance.String,
				OrgID: targetOrg.String, ProjectID: targetProject.String,
			}, Label: targetLabel.String}
		}
		item.BodyCapture.Mode = "disabled"
		result.Items = append(result.Items, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	if err = rows.Close(); err != nil {
		return result, err
	}
	for i := range result.Items {
		item := &result.Items[i]
		var binding bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM review_body_ingest_bindings WHERE
 service_identity_id=$1 AND collection_id=$2::uuid AND status='active')`, identity.ID, item.CollectionID).Scan(&binding); err != nil {
			return result, err
		}
		if !binding {
			continue
		}
		policy, policyErr := reviewBodyPolicyTx(ctx, tx, item.Reporter, "", nil, now.UTC())
		if errors.Is(policyErr, reviewbody.ErrPolicy) {
			continue
		}
		if policyErr != nil {
			return result, policyErr
		}
		item.BodyCapture = WorkEventBodyCaptureView{Mode: policy.Mode, PolicyVersion: policy.Vector,
			MaxItemBytes: policy.MaxItem, RetentionSeconds: policy.Retention}
		for _, kind := range []string{"user_input", "user_output", "model_input", "model_output", "tool_input", "tool_output"} {
			selected, selectErr := reviewBodyPolicyTx(ctx, tx, item.Reporter, kind, nil, now.UTC())
			if selectErr != nil {
				return result, selectErr
			}
			if selected.Mode == policy.Mode && selected.Vector == policy.Vector && selected.MaxItem == policy.MaxItem &&
				selected.Retention == policy.Retention {
				continue
			}
			if item.BodyCapture.ByKind == nil {
				item.BodyCapture.ByKind = map[string]WorkEventBodyCaptureRule{}
			}
			item.BodyCapture.ByKind[kind] = WorkEventBodyCaptureRule{Mode: selected.Mode, PolicyVersion: selected.Vector,
				MaxItemBytes: selected.MaxItem, RetentionSeconds: selected.Retention}
		}
	}
	return result, tx.Commit()
}

// CommitAuthorizedWorkEventItem rechecks the service credential and exact
// administrator binding in the same transaction that admits the document.
// Middleware authentication is not relied upon after its transaction ends.
func (s *WorkspaceStore) CommitAuthorizedWorkEventItem(ctx context.Context, digest string, item WorkEventIngestItem, now time.Time) (WorkEventIngestReceipt, error) {
	var receipt WorkEventIngestReceipt
	if s == nil || s.db == nil || !workEventUUID.MatchString(item.CollectionID) ||
		item.ExpectedCollectionRevision < 1 || !workEventGeneration.MatchString(item.ScopeGeneration) ||
		(item.Kind != "event" && item.Kind != "relation") || now.IsZero() {
		return receipt, workevent.ErrInvalid
	}
	var event workevent.Event
	var relation workevent.RelationAssertion
	var err error
	if item.Kind == "event" {
		event, err = workevent.DecodeEvent(item.Document)
	} else {
		relation, err = workevent.DecodeRelation(item.Document)
	}
	if err != nil {
		return receipt, workevent.ErrInvalid
	}
	now = now.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return receipt, err
	}
	defer tx.Rollback()
	identity, tokenExpiry, tokenRevoked, err := queryServiceIdentityByDigestLock(ctx, tx, digest, true)
	if errors.Is(err, sql.ErrNoRows) {
		return receipt, workspace.ErrCredentialInvalid
	}
	if err != nil {
		return receipt, err
	}
	if err = checkWorkEventIngestIdentity(identity, tokenExpiry, tokenRevoked, now); err != nil {
		return receipt, err
	}
	var sourceID, bindingStatus string
	err = tx.QueryRowContext(ctx, `SELECT c.source_system_id::text,b.status FROM work_event_ingest_bindings b
JOIN work_event_collections c ON c.id=b.collection_id
WHERE b.service_identity_id=$1 AND b.collection_id=$2::uuid FOR SHARE OF b`, identity.ID, item.CollectionID).Scan(&sourceID, &bindingStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return receipt, workevent.ErrScopeChanged
	}
	if err != nil {
		return receipt, err
	}
	if bindingStatus != "active" {
		return receipt, workevent.ErrScopeChanged
	}
	if err = sourceLock(ctx, tx, sourceID); err != nil {
		return receipt, err
	}
	collection, err := loadWorkEventCollectionShared(ctx, tx, item.CollectionID)
	if err != nil {
		return receipt, err
	}
	if collection.Status != "active" || collection.Revision != item.ExpectedCollectionRevision ||
		collection.ScopeGeneration != item.ScopeGeneration || collection.Scope.SourceSystemID != sourceID {
		return receipt, workevent.ErrScopeChanged
	}
	var sourceType, sourceStatus string
	if err = tx.QueryRowContext(ctx, `SELECT source_type,status FROM source_systems WHERE id=$1::uuid FOR SHARE`, sourceID).Scan(&sourceType, &sourceStatus); err != nil {
		return receipt, err
	}
	if sourceType != "agent_runtime" || sourceStatus != "active" {
		return receipt, workevent.ErrScopeChanged
	}
	unambiguous, err := workEventSourceScopeUnambiguous(ctx, tx, collection.Scope)
	if err != nil || !unambiguous {
		if err != nil {
			return receipt, err
		}
		return receipt, workevent.ErrScopeChanged
	}
	var inserted bool
	if item.Kind == "event" {
		if event.Reporter != collection.Scope {
			return receipt, workevent.ErrScopeChanged
		}
		inserted, err = insertWorkEventTx(ctx, tx, event, item.Document, now)
		receipt.DocumentRef, receipt.Revision = event.EventRef, event.Revision
	} else {
		if relation.Reporter != collection.Scope {
			return receipt, workevent.ErrScopeChanged
		}
		if err = sourceLock(ctx, tx, relation.URTarget.SourceSystemID); err != nil {
			return receipt, err
		}
		var target workevent.Scope
		var targetStatus, targetType, targetSourceStatus string
		err = tx.QueryRowContext(ctx, `SELECT t.target_source_system_id::text,t.target_source_instance,t.target_org_id,t.target_project_id,
 t.status,s.source_type,s.status FROM work_event_ur_targets t
 JOIN source_systems s ON s.id=t.target_source_system_id
 WHERE t.collection_id=$1::uuid FOR SHARE OF t,s`, item.CollectionID).Scan(
			&target.SourceSystemID, &target.SourceInstance, &target.OrgID, &target.ProjectID,
			&targetStatus, &targetType, &targetSourceStatus)
		if errors.Is(err, sql.ErrNoRows) {
			return receipt, workevent.ErrScopeChanged
		}
		if err != nil {
			return receipt, err
		}
		if targetStatus != "active" || targetType != "unirouter" ||
			targetSourceStatus != "active" || target != relation.URTarget {
			return receipt, workevent.ErrScopeChanged
		}
		var targetRegistered bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM source_fact_streams
 WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND status='active')`,
			target.SourceSystemID, target.SourceInstance, target.OrgID, target.ProjectID).Scan(&targetRegistered); err != nil {
			return receipt, err
		}
		if !targetRegistered {
			return receipt, workevent.ErrScopeChanged
		}
		inserted, err = insertWorkEventRelationTx(ctx, tx, relation, item.Document, now)
		receipt.DocumentRef = relation.RelationRef
	}
	if err != nil {
		return receipt, err
	}
	receipt.Kind, receipt.Reporter, receipt.SHA256, receipt.Outcome = item.Kind, collection.Scope, workEventHash(item.Document), "identical_replay"
	if inserted {
		receipt.Outcome = "accepted"
	}
	if err = insertAccessAudit(ctx, tx, "service", identity.ID, "work_event.ingest", "work_event", receipt.DocumentRef, "accepted", now); err != nil {
		return WorkEventIngestReceipt{}, err
	}
	return receipt, tx.Commit()
}
