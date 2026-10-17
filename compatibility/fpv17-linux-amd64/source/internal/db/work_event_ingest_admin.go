package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"io"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/foigent/x-insight/internal/workevent"
	"github.com/foigent/x-insight/internal/workspace"
)

type WorkEventIngestBindingInput struct {
	CollectionID       string
	ProducerProjectKey string
	ProjectLabel       string
}

type CreateWorkEventIngestIdentityInput struct {
	DisplayName string
	ExpiresAt   time.Time
	Bindings    []WorkEventIngestBindingInput
}

// The administrative credential is required. This method is never called by
// the long-running HTTP server and does not create a credential implicitly.
func (s *WorkspaceStore) CreateWorkEventIngestIdentity(ctx context.Context, actor workspace.Actor, input CreateWorkEventIngestIdentityInput, now time.Time, random io.Reader) (workspace.ServiceCredential, error) {
	var empty workspace.ServiceCredential
	if s == nil || s.db == nil || actor.Kind != "root" || actor.Ref != "root" ||
		workspace.ValidateCreateServiceIdentity(workspace.CreateServiceIdentityInput{DisplayName: input.DisplayName, ExpiresAt: input.ExpiresAt}, now.UTC()) != nil ||
		len(input.Bindings) < 1 || len(input.Bindings) > 32 {
		return empty, workspace.ErrInvalid
	}
	bindings := append([]WorkEventIngestBindingInput(nil), input.Bindings...)
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].CollectionID < bindings[j].CollectionID })
	seenProjectKeys := make(map[string]bool, len(bindings))
	for i, binding := range bindings {
		if !workEventUUID.MatchString(binding.CollectionID) || strings.TrimSpace(binding.ProducerProjectKey) != binding.ProducerProjectKey ||
			len(binding.ProducerProjectKey) < 1 || len(binding.ProducerProjectKey) > 256 ||
			strings.IndexFunc(binding.ProducerProjectKey, unicode.IsControl) >= 0 || seenProjectKeys[binding.ProducerProjectKey] ||
			strings.TrimSpace(binding.ProjectLabel) != binding.ProjectLabel ||
			len(binding.ProjectLabel) < 1 || len(binding.ProjectLabel) > 128 ||
			strings.IndexFunc(binding.ProjectLabel, unicode.IsControl) >= 0 ||
			(i > 0 && binding.CollectionID == bindings[i-1].CollectionID) {
			return empty, workspace.ErrInvalid
		}
		seenProjectKeys[binding.ProducerProjectKey] = true
	}
	if random == nil {
		random = rand.Reader
	}
	identityID, err := workspace.NewID("xsi_", random)
	if err != nil {
		return empty, err
	}
	token, digest, err := workspace.NewServiceToken(random)
	if err != nil {
		return empty, err
	}
	now = now.UTC()
	expires := input.ExpiresAt.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	var admin bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_has_role(current_user,relowner,'USAGE') FROM pg_class WHERE oid='public.work_event_ingest_bindings'::regclass`).Scan(&admin); err != nil {
		return empty, err
	}
	if !admin {
		return empty, workevent.ErrUnavailable
	}
	sourceByCollection := make(map[string]string, len(bindings))
	sourceSet := make(map[string]bool, len(bindings))
	for _, binding := range bindings {
		var sourceID string
		if err = tx.QueryRowContext(ctx, `SELECT source_system_id::text FROM work_event_collections WHERE id=$1::uuid`, binding.CollectionID).Scan(&sourceID); err != nil {
			return empty, err
		}
		sourceByCollection[binding.CollectionID] = sourceID
		sourceSet[sourceID] = true
	}
	sources := make([]string, 0, len(sourceSet))
	for sourceID := range sourceSet {
		sources = append(sources, sourceID)
	}
	sort.Strings(sources)
	for _, sourceID := range sources {
		if err = sourceLock(ctx, tx, sourceID); err != nil {
			return empty, err
		}
	}
	for _, binding := range bindings {
		collection, loadErr := loadWorkEventCollection(ctx, tx, binding.CollectionID, true)
		if loadErr != nil {
			return empty, loadErr
		}
		if collection.Status != "active" || collection.Scope.SourceSystemID != sourceByCollection[binding.CollectionID] {
			return empty, workevent.ErrScopeChanged
		}
		var sourceType, sourceStatus string
		if err = tx.QueryRowContext(ctx, `SELECT source_type,status FROM source_systems WHERE id=$1::uuid FOR SHARE`, collection.Scope.SourceSystemID).Scan(&sourceType, &sourceStatus); err != nil {
			return empty, err
		}
		if sourceType != "agent_runtime" || sourceStatus != "active" {
			return empty, workevent.ErrScopeChanged
		}
	}
	permissions := []string{"work_event.ingest"}
	if _, err = tx.ExecContext(ctx, `INSERT INTO service_identities(id,display_name,status,permissions,created_by_subject,created_at,updated_at,expires_at)
 VALUES($1,$2,'active',$3,'root',$4,$4,$5)`, identityID, input.DisplayName, permissions, now, expires); err != nil {
		return empty, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO service_identity_tokens(token_digest,service_identity_id,token_prefix,created_at,expires_at)
 VALUES($1,$2,$3,$4,$5)`, digest, identityID, token[:12], now, expires); err != nil {
		return empty, err
	}
	for _, binding := range bindings {
		if _, err = tx.ExecContext(ctx, `INSERT INTO work_event_ingest_bindings(service_identity_id,collection_id,producer_project_key,project_label)
 VALUES($1,$2::uuid,$3,$4)`, identityID, binding.CollectionID, binding.ProducerProjectKey, binding.ProjectLabel); err != nil {
			return empty, err
		}
	}
	if err = insertAccessAudit(ctx, tx, "root", actor.Ref, "work_event_ingest_identity.create", "service_identity", identityID, "accepted", now); err != nil {
		return empty, err
	}
	if err = tx.Commit(); err != nil {
		return empty, err
	}
	return workspace.ServiceCredential{ServiceIdentity: workspace.ServiceIdentity{
		ID: identityID, DisplayName: input.DisplayName, Status: "active", Permissions: permissions,
		CreatedBySubject: "root", CreatedAt: now, UpdatedAt: now, ExpiresAt: &expires,
	}, Token: token, TokenPrefix: token[:12], ExpiresAt: expires}, nil
}

// RegisterWorkEventURTarget records the exact source pair. Repointing requires
// a new collection generation; an existing pairing is never overwritten.
func (s *SourceFactsStore) RegisterWorkEventURTarget(ctx context.Context, collectionID string, target workevent.Scope, label string) error {
	if s == nil || s.db == nil || !workEventUUID.MatchString(collectionID) || !target.Valid() ||
		strings.TrimSpace(label) != label || len(label) < 1 || len(label) > 128 {
		return workevent.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var admin bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_has_role(current_user,relowner,'USAGE') FROM pg_class WHERE oid='public.work_event_ur_targets'::regclass`).Scan(&admin); err != nil {
		return err
	}
	if !admin {
		return workevent.ErrUnavailable
	}
	var sourceID string
	if err = tx.QueryRowContext(ctx, `SELECT source_system_id::text FROM work_event_collections WHERE id=$1::uuid`, collectionID).Scan(&sourceID); err != nil {
		return err
	}
	if err = sourceLock(ctx, tx, sourceID); err != nil {
		return err
	}
	collection, err := loadWorkEventCollection(ctx, tx, collectionID, true)
	if err != nil {
		return err
	}
	if collection.Status != "active" {
		return workevent.ErrScopeChanged
	}
	if err = sourceLock(ctx, tx, target.SourceSystemID); err != nil {
		return err
	}
	var targetType, targetStatus string
	if err = tx.QueryRowContext(ctx, `SELECT source_type,status FROM source_systems WHERE id=$1::uuid FOR SHARE`, target.SourceSystemID).Scan(&targetType, &targetStatus); err != nil {
		return err
	}
	if targetType != "unirouter" || targetStatus != "active" {
		return workevent.ErrScopeChanged
	}
	var registered bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM source_fact_streams
 WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND status='active')`,
		target.SourceSystemID, target.SourceInstance, target.OrgID, target.ProjectID).Scan(&registered); err != nil {
		return err
	}
	if !registered {
		return workevent.ErrScopeChanged
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO work_event_ur_targets(collection_id,target_source_system_id,target_source_instance,target_org_id,target_project_id,target_label)
 VALUES($1::uuid,$2::uuid,$3,$4,$5,$6) ON CONFLICT(collection_id) DO NOTHING`, collectionID, target.SourceSystemID,
		target.SourceInstance, target.OrgID, target.ProjectID, label)
	if err != nil {
		return err
	}
	var existing workevent.Scope
	var existingLabel, status string
	err = tx.QueryRowContext(ctx, `SELECT target_source_system_id::text,target_source_instance,target_org_id,target_project_id,target_label,status
 FROM work_event_ur_targets WHERE collection_id=$1::uuid FOR UPDATE`, collectionID).Scan(
		&existing.SourceSystemID, &existing.SourceInstance, &existing.OrgID, &existing.ProjectID, &existingLabel, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return workevent.ErrUnavailable
	}
	if err != nil {
		return err
	}
	if existing != target || existingLabel != label || status != "active" {
		return workevent.ErrConflict
	}
	return tx.Commit()
}
