package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/foigent/x-insight/internal/workevent"
)

// A future Agent is scoped to one already approved managed installation. The
// administrator's one-time future grant permits only a new independent XI
// source project; it never guesses or inherits a UniRouter target.
func (s *SourceFactsStore) ReportFutureWorkInstallProjects(ctx context.Context, identityID, digest string,
	projects []WorkInstallProject, now time.Time) (WorkInstallInvitation, error) {
	var result WorkInstallInvitation
	if s == nil || s.db == nil || !serviceIdentityPattern.MatchString(identityID) ||
		!serviceTokenDigestPattern.MatchString(digest) || !validWorkInstallProjects(projects) || now.IsZero() {
		return result, workevent.ErrInvalid
	}
	now = now.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var oldProjects, bindings, future, exclusions []byte
	var tokenExpiry time.Time
	var identityExpiry sql.NullTime
	var identityStatus string
	err = tx.QueryRowContext(ctx, `SELECT w.invitation_id,w.display_name,w.source_instance,w.state,w.candidates,
w.approved_source_system_id::text,w.approved_service_identity_id,w.approved_bindings,w.expires_at,
w.future_agents_allowed,w.future_candidates,w.excluded_future_project_keys,t.expires_at,i.expires_at,i.status
FROM work_event_install_invitations w
JOIN service_identities i ON i.id=w.approved_service_identity_id
JOIN service_identity_tokens t ON t.service_identity_id=i.id
WHERE i.id=$1 AND t.token_digest=$2 AND t.revoked_at IS NULL AND i.permissions=ARRAY['work_event.ingest']::text[]
FOR UPDATE OF w,t,i`, identityID, digest).Scan(&result.InvitationID, &result.DisplayName, &result.SourceInstance,
		&result.State, &oldProjects, &result.SourceSystemID, &result.ServiceIdentityID, &bindings,
		&result.ExpiresAt, &result.FutureAgentsAllowed, &future, &exclusions, &tokenExpiry, &identityExpiry, &identityStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return result, workevent.ErrScopeChanged
	}
	if err != nil {
		return result, err
	}
	if result.State != "approved" || identityStatus != "active" || !now.Before(tokenExpiry) ||
		!identityExpiry.Valid || !now.Before(identityExpiry.Time) {
		return result, workevent.ErrScopeChanged
	}
	if json.Unmarshal(oldProjects, &result.Projects) != nil || json.Unmarshal(bindings, &result.Bindings) != nil ||
		json.Unmarshal(future, &result.FutureCandidates) != nil || json.Unmarshal(exclusions, &result.ExcludedFutureProjectKeys) != nil {
		return result, workevent.ErrUnavailable
	}
	if len(result.Bindings)+len(result.FutureCandidates)+len(result.ExcludedFutureProjectKeys) > 32 {
		return result, workevent.ErrConflict
	}
	existing := map[string]string{}
	for _, binding := range result.Bindings {
		existing[binding.ProjectKey] = binding.ProjectLabel
	}
	pending := map[string]string{}
	for _, candidate := range result.FutureCandidates {
		pending[candidate.ProjectKey] = candidate.Label
	}
	excluded := map[string]bool{}
	for _, key := range result.ExcludedFutureProjectKeys {
		excluded[key] = true
	}
	for _, project := range projects {
		if excluded[project.ProjectKey] {
			continue
		}
		if _, ok := existing[project.ProjectKey]; ok {
			continue
		}
		if _, ok := pending[project.ProjectKey]; ok {
			continue
		}
		if len(result.Bindings)+len(result.FutureCandidates)+len(result.ExcludedFutureProjectKeys) >= 32 {
			return result, workevent.ErrConflict
		}
		if !result.FutureAgentsAllowed {
			result.FutureCandidates = append(result.FutureCandidates, project)
			pending[project.ProjectKey] = project.Label
			continue
		}
		var xiID, collectionID string
		if err = tx.QueryRowContext(ctx, `INSERT INTO review_capture_projects(xi_project_id,display_name)
VALUES(gen_random_uuid(),$1) RETURNING xi_project_id::text`, project.Label).Scan(&xiID); err != nil {
			return result, err
		}
		projectID := strconv.Itoa(len(result.Bindings) + 1)
		if err = tx.QueryRowContext(ctx, `INSERT INTO work_event_collections(id,source_system_id,source_instance,org_id,project_id,profile,scope_generation,status)
VALUES(gen_random_uuid(),$1::uuid,$2,'1',$3,$4,'1','active') RETURNING id::text`, result.SourceSystemID,
			result.SourceInstance, projectID, workevent.CollectionProfile).Scan(&collectionID); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO work_event_ingest_bindings(service_identity_id,collection_id,producer_project_key,project_label)
VALUES($1,$2::uuid,$3,$4)`, identityID, collectionID, project.ProjectKey, project.Label); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO review_body_ingest_bindings(service_identity_id,collection_id,status,revision)
VALUES($1,$2::uuid,'active',1)`, identityID, collectionID); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO review_capture_project_bindings(source_system_id,source_instance,org_id,project_id,xi_project_id,revision)
VALUES($1::uuid,$2,'1',$3,$4::uuid,1)`, result.SourceSystemID, result.SourceInstance, projectID, xiID); err != nil {
			return result, err
		}
		result.Bindings = append(result.Bindings, WorkInstallApprovedBinding{ProjectKey: project.ProjectKey,
			ProjectLabel: project.Label, ProjectID: projectID, CollectionID: collectionID, XIProjectID: xiID, XIProjectName: project.Label})
		existing[project.ProjectKey] = project.Label
	}
	bindingsJSON, _ := json.Marshal(result.Bindings)
	futureJSON, _ := json.Marshal(result.FutureCandidates)
	if _, err = tx.ExecContext(ctx, `UPDATE work_event_install_invitations SET approved_bindings=$2::jsonb,
future_candidates=$3::jsonb,updated_at=$4 WHERE invitation_id=$1`, result.InvitationID, bindingsJSON, futureJSON, now); err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

func (s *SourceFactsStore) DecideFutureWorkInstallProjects(ctx context.Context, id, actor string,
	mappings []WorkInstallMapping, excludedKeys []string, now time.Time) (WorkInstallInvitation, error) {
	var result WorkInstallInvitation
	if s == nil || s.db == nil || !workInstallID.MatchString(id) || !workInstallSubject.MatchString(actor) ||
		now.IsZero() || len(mappings)+len(excludedKeys) < 1 || len(mappings)+len(excludedKeys) > 32 {
		return result, workevent.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var pendingJSON, bindingsJSON, excludedJSON []byte
	err = tx.QueryRowContext(ctx, `SELECT invitation_id,display_name,source_instance,state,
approved_source_system_id::text,approved_service_identity_id,approved_bindings,future_agents_allowed,
future_candidates,excluded_future_project_keys,expires_at FROM work_event_install_invitations
WHERE invitation_id=$1 FOR UPDATE`, id).Scan(&result.InvitationID, &result.DisplayName, &result.SourceInstance,
		&result.State, &result.SourceSystemID, &result.ServiceIdentityID, &bindingsJSON, &result.FutureAgentsAllowed,
		&pendingJSON, &excludedJSON, &result.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return result, workevent.ErrScopeChanged
	}
	if err != nil {
		return result, err
	}
	if result.State != "approved" || result.FutureAgentsAllowed {
		return result, workevent.ErrConflict
	}
	if json.Unmarshal(bindingsJSON, &result.Bindings) != nil || json.Unmarshal(pendingJSON, &result.FutureCandidates) != nil ||
		json.Unmarshal(excludedJSON, &result.ExcludedFutureProjectKeys) != nil {
		return result, workevent.ErrUnavailable
	}
	if len(result.FutureCandidates) != len(mappings)+len(excludedKeys) ||
		len(result.Bindings)+len(result.FutureCandidates)+len(result.ExcludedFutureProjectKeys) > 32 {
		return result, workevent.ErrConflict
	}
	pending := map[string]WorkInstallProject{}
	for _, project := range result.FutureCandidates {
		pending[project.ProjectKey] = project
	}
	seen := map[string]bool{}
	for _, mapping := range mappings {
		if _, ok := pending[mapping.ProjectKey]; !ok || seen[mapping.ProjectKey] ||
			(mapping.XIProjectID == "") == (mapping.XIProjectName == "") ||
			(mapping.XIProjectID != "" && !workEventUUID.MatchString(mapping.XIProjectID)) ||
			len(mapping.XIProjectName) > 128 || strings.TrimSpace(mapping.XIProjectName) != mapping.XIProjectName ||
			strings.IndexFunc(mapping.XIProjectName, unicode.IsControl) >= 0 ||
			(mapping.URTarget != nil && !mapping.URTarget.Valid()) {
			return result, workevent.ErrInvalid
		}
		seen[mapping.ProjectKey] = true
	}
	for _, key := range excludedKeys {
		if _, ok := pending[key]; !ok || seen[key] {
			return result, workevent.ErrInvalid
		}
		seen[key] = true
	}
	for _, project := range result.FutureCandidates {
		var mapping *WorkInstallMapping
		for i := range mappings {
			if mappings[i].ProjectKey == project.ProjectKey {
				mapping = &mappings[i]
				break
			}
		}
		if mapping == nil {
			result.ExcludedFutureProjectKeys = append(result.ExcludedFutureProjectKeys, project.ProjectKey)
			continue
		}
		xiID, xiName := mapping.XIProjectID, mapping.XIProjectName
		if xiID == "" {
			if err = tx.QueryRowContext(ctx, `INSERT INTO review_capture_projects(xi_project_id,display_name)
VALUES(gen_random_uuid(),$1) RETURNING xi_project_id::text`, xiName).Scan(&xiID); err != nil {
				return result, err
			}
		} else if err = tx.QueryRowContext(ctx, `SELECT display_name FROM review_capture_projects WHERE xi_project_id=$1::uuid FOR SHARE`, xiID).Scan(&xiName); err != nil {
			return result, err
		}
		projectID := strconv.Itoa(len(result.Bindings) + 1)
		var collectionID string
		if err = tx.QueryRowContext(ctx, `INSERT INTO work_event_collections(id,source_system_id,source_instance,org_id,project_id,profile,scope_generation,status)
VALUES(gen_random_uuid(),$1::uuid,$2,'1',$3,$4,'1','active') RETURNING id::text`, result.SourceSystemID, result.SourceInstance, projectID, workevent.CollectionProfile).Scan(&collectionID); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO work_event_ingest_bindings(service_identity_id,collection_id,producer_project_key,project_label)
VALUES($1,$2::uuid,$3,$4)`, result.ServiceIdentityID, collectionID, project.ProjectKey, project.Label); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO review_body_ingest_bindings(service_identity_id,collection_id,status,revision)
VALUES($1,$2::uuid,'active',1)`, result.ServiceIdentityID, collectionID); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO review_capture_project_bindings(source_system_id,source_instance,org_id,project_id,xi_project_id,revision)
VALUES($1::uuid,$2,'1',$3,$4::uuid,1)`, result.SourceSystemID, result.SourceInstance, projectID, xiID); err != nil {
			return result, err
		}
		if mapping.URTarget != nil {
			var valid bool
			err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM source_systems s JOIN source_fact_streams f ON f.source_system_id=s.id
WHERE s.id=$1::uuid AND s.source_type='unirouter' AND s.status='active' AND f.status='active'
AND f.source_instance=$2 AND f.org_id=$3 AND f.project_id=$4)`, mapping.URTarget.SourceSystemID,
				mapping.URTarget.SourceInstance, mapping.URTarget.OrgID, mapping.URTarget.ProjectID).Scan(&valid)
			if err != nil || !valid {
				return result, workevent.ErrScopeChanged
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO work_event_ur_targets(collection_id,target_source_system_id,target_source_instance,
target_org_id,target_project_id,target_label) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6)`, collectionID,
				mapping.URTarget.SourceSystemID, mapping.URTarget.SourceInstance, mapping.URTarget.OrgID, mapping.URTarget.ProjectID,
				"UniRouter project "+mapping.URTarget.ProjectID); err != nil {
				return result, err
			}
		}
		result.Bindings = append(result.Bindings, WorkInstallApprovedBinding{ProjectKey: project.ProjectKey, ProjectLabel: project.Label,
			ProjectID: projectID, CollectionID: collectionID, XIProjectID: xiID, XIProjectName: xiName, URTarget: mapping.URTarget})
	}
	result.FutureCandidates = []WorkInstallProject{}
	newBindings, _ := json.Marshal(result.Bindings)
	newExclusions, _ := json.Marshal(result.ExcludedFutureProjectKeys)
	if _, err = tx.ExecContext(ctx, `UPDATE work_event_install_invitations SET approved_bindings=$2::jsonb,
future_candidates='[]'::jsonb,excluded_future_project_keys=$3::jsonb,updated_at=$4 WHERE invitation_id=$1`,
		id, newBindings, newExclusions, now.UTC()); err != nil {
		return result, err
	}
	if err = insertAccessAudit(ctx, tx, "human", actor, "work_event_install.future_decide", "source_system", result.SourceSystemID, "accepted", now.UTC()); err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}
