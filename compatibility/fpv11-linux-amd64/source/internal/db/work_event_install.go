package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/foigent/x-insight/internal/workevent"
	"github.com/foigent/x-insight/internal/workspace"
)

var workInstallID = regexp.MustCompile(`^xiei_[a-f0-9]{32}$`)
var workInstallToken = regexp.MustCompile(`^xiet_[A-Za-z0-9_-]{43}$`)
var workInstallSubject = regexp.MustCompile(`^xs_[a-z0-9]{16,64}$`)

type WorkInstallProject struct {
	ProjectKey string `json:"project_key"`
	Label      string `json:"label"`
}

type WorkInstallMapping struct {
	ProjectKey    string           `json:"project_key"`
	XIProjectID   string           `json:"xi_project_id,omitempty"`
	XIProjectName string           `json:"xi_project_name,omitempty"`
	URTarget      *workevent.Scope `json:"ur_target,omitempty"`
}

type WorkInstallApprovedBinding struct {
	ProjectKey    string           `json:"project_key"`
	ProjectLabel  string           `json:"project_label"`
	ProjectID     string           `json:"project_id"`
	CollectionID  string           `json:"collection_id"`
	XIProjectID   string           `json:"xi_project_id"`
	XIProjectName string           `json:"xi_project_name"`
	URTarget      *workevent.Scope `json:"ur_target,omitempty"`
}

type WorkInstallInvitation struct {
	InvitationID              string                       `json:"invitation_id"`
	InvitationToken           string                       `json:"invitation_token,omitempty"`
	DisplayName               string                       `json:"display_name"`
	SourceInstance            string                       `json:"source_instance"`
	State                     string                       `json:"state"`
	Projects                  []WorkInstallProject         `json:"projects,omitempty"`
	Bindings                  []WorkInstallApprovedBinding `json:"bindings,omitempty"`
	FutureAgentsAllowed       bool                         `json:"future_agents_allowed"`
	FutureCandidates          []WorkInstallProject         `json:"future_candidates,omitempty"`
	ExcludedFutureProjectKeys []string                     `json:"excluded_future_project_keys,omitempty"`
	SourceSystemID            string                       `json:"source_system_id,omitempty"`
	ServiceIdentityID         string                       `json:"service_identity_id,omitempty"`
	LatestStatus              *WorkInstallStatus           `json:"latest_status,omitempty"`
	StatusReceivedAt          *time.Time                   `json:"status_received_at,omitempty"`
	ExpiresAt                 time.Time                    `json:"expires_at"`
}

func digestWorkInstall(value string) string {
	hash := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(hash[:])
}

func validWorkInstallProjects(projects []WorkInstallProject) bool {
	if len(projects) < 1 || len(projects) > 32 {
		return false
	}
	seen := map[string]bool{}
	for _, project := range projects {
		if seen[project.ProjectKey] || len(project.ProjectKey) < 1 || len(project.ProjectKey) > 256 ||
			strings.TrimSpace(project.ProjectKey) != project.ProjectKey || strings.IndexFunc(project.ProjectKey, unicode.IsControl) >= 0 ||
			len(project.Label) < 1 || len(project.Label) > 128 || strings.TrimSpace(project.Label) != project.Label ||
			strings.IndexFunc(project.Label, unicode.IsControl) >= 0 {
			return false
		}
		seen[project.ProjectKey] = true
	}
	return true
}

func (s *SourceFactsStore) IssueWorkInstallInvitation(ctx context.Context, actor, displayName string,
	now time.Time, ttl time.Duration, random io.Reader) (WorkInstallInvitation, error) {
	var result WorkInstallInvitation
	if s == nil || s.db == nil || !workInstallSubject.MatchString(actor) || len(displayName) < 1 || len(displayName) > 128 ||
		strings.TrimSpace(displayName) != displayName || strings.IndexFunc(displayName, unicode.IsControl) >= 0 ||
		now.IsZero() || ttl < 10*time.Minute || ttl > 24*time.Hour {
		return result, workevent.ErrInvalid
	}
	if random == nil {
		random = rand.Reader
	}
	var idBytes, tokenBytes [32]byte
	if _, err := io.ReadFull(random, idBytes[:]); err != nil {
		return result, err
	}
	if _, err := io.ReadFull(random, tokenBytes[:]); err != nil {
		return result, err
	}
	short := hex.EncodeToString(idBytes[:16])
	id, token := "xiei_"+short, "xiet_"+base64.RawURLEncoding.EncodeToString(tokenBytes[:])
	if !workInstallToken.MatchString(token) {
		return result, workevent.ErrInvalid
	}
	result = WorkInstallInvitation{InvitationID: id, InvitationToken: token, DisplayName: displayName,
		SourceInstance: "openclaw-" + short, State: "invited", ExpiresAt: now.UTC().Add(ttl)}
	_, err := s.db.ExecContext(ctx, `INSERT INTO work_event_install_invitations
 (invitation_id,invitation_digest,actor_subject,display_name,source_instance,state,expires_at)
 VALUES($1,$2,$3,$4,$5,'invited',$6)`, id, digestWorkInstall(token), actor, displayName, result.SourceInstance, result.ExpiresAt)
	return result, err
}

func (s *SourceFactsStore) SubmitWorkInstallCandidates(ctx context.Context, invitationToken, ingestToken string,
	projects []WorkInstallProject, now time.Time) (WorkInstallInvitation, error) {
	var result WorkInstallInvitation
	if s == nil || s.db == nil || !workInstallToken.MatchString(invitationToken) ||
		!validWorkInstallProjects(projects) || now.IsZero() {
		return result, workevent.ErrInvalid
	}
	tokenDigest, err := workspace.ServiceTokenDigest(ingestToken)
	if err != nil {
		return result, workevent.ErrInvalid
	}
	canonical, _ := json.Marshal(struct {
		Projects    []WorkInstallProject `json:"projects"`
		TokenDigest string               `json:"token_digest"`
	}{projects, tokenDigest})
	candidateHash := digestWorkInstall(string(canonical))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var state, existingHash, existingToken sql.NullString
	var expiry time.Time
	err = tx.QueryRowContext(ctx, `SELECT invitation_id,display_name,source_instance,state,
 COALESCE(candidate_sha256,''),COALESCE(host_token_digest,''),expires_at
 FROM work_event_install_invitations WHERE invitation_digest=$1 FOR UPDATE`, digestWorkInstall(invitationToken)).
		Scan(&result.InvitationID, &result.DisplayName, &result.SourceInstance, &state, &existingHash, &existingToken, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return result, workevent.ErrUnavailable
	}
	if err != nil {
		return result, err
	}
	if !now.UTC().Before(expiry) && state.String != "approved" {
		return result, workevent.ErrUnavailable
	}
	if (state.String == "candidates" || state.String == "approved") && existingHash.String == candidateHash && existingToken.String == tokenDigest {
		result.State, result.Projects, result.ExpiresAt = state.String, projects, expiry
		return result, tx.Commit()
	}
	if state.String != "invited" {
		return result, workevent.ErrConflict
	}
	projectJSON, _ := json.Marshal(projects)
	_, err = tx.ExecContext(ctx, `UPDATE work_event_install_invitations SET state='candidates',
 candidates=$2::jsonb,candidate_sha256=$3,host_token_digest=$4,host_token_prefix=$5,updated_at=$6
 WHERE invitation_id=$1`, result.InvitationID, projectJSON, candidateHash, tokenDigest, ingestToken[:12], now.UTC())
	if err != nil {
		return result, err
	}
	result.State, result.Projects, result.ExpiresAt = "candidates", projects, expiry
	return result, tx.Commit()
}

func scanWorkInstall(row *sql.Row) (WorkInstallInvitation, error) {
	var result WorkInstallInvitation
	var projects, bindings, futureCandidates, exclusions, latestStatus []byte
	var sourceID, identityID sql.NullString
	var statusReceivedAt sql.NullTime
	err := row.Scan(&result.InvitationID, &result.DisplayName, &result.SourceInstance, &result.State,
		&projects, &sourceID, &identityID, &bindings, &result.ExpiresAt, &result.FutureAgentsAllowed, &futureCandidates, &exclusions,
		&latestStatus, &statusReceivedAt)
	if err != nil {
		return result, err
	}
	if len(projects) > 0 {
		if json.Unmarshal(projects, &result.Projects) != nil {
			return result, workevent.ErrUnavailable
		}
	}
	if len(bindings) > 0 {
		if json.Unmarshal(bindings, &result.Bindings) != nil {
			return result, workevent.ErrUnavailable
		}
	}
	if len(futureCandidates) > 0 && json.Unmarshal(futureCandidates, &result.FutureCandidates) != nil {
		return result, workevent.ErrUnavailable
	}
	if len(exclusions) > 0 && json.Unmarshal(exclusions, &result.ExcludedFutureProjectKeys) != nil {
		return result, workevent.ErrUnavailable
	}
	if len(latestStatus) > 0 {
		if json.Unmarshal(latestStatus, &result.LatestStatus) != nil || !ValidWorkInstallStatus(result.LatestStatus) || !statusReceivedAt.Valid {
			return result, workevent.ErrUnavailable
		}
		at := statusReceivedAt.Time.UTC()
		result.StatusReceivedAt = &at
	} else if statusReceivedAt.Valid {
		return result, workevent.ErrUnavailable
	}
	result.SourceSystemID, result.ServiceIdentityID = sourceID.String, identityID.String
	return result, nil
}

const workInstallReadSQL = `SELECT invitation_id,display_name,source_instance,state,candidates,
 approved_source_system_id::text,approved_service_identity_id,approved_bindings,expires_at,
 future_agents_allowed,future_candidates,excluded_future_project_keys,latest_status,status_received_at
 FROM work_event_install_invitations`

func (s *SourceFactsStore) ReadWorkInstallInvitation(ctx context.Context, id string) (WorkInstallInvitation, error) {
	if s == nil || s.db == nil || !workInstallID.MatchString(id) {
		return WorkInstallInvitation{}, workevent.ErrInvalid
	}
	return scanWorkInstall(s.db.QueryRowContext(ctx, workInstallReadSQL+` WHERE invitation_id=$1`, id))
}

func (s *SourceFactsStore) PickupWorkInstallInvitation(ctx context.Context, token, ingestToken string, now time.Time) (WorkInstallInvitation, error) {
	if s == nil || s.db == nil || !workInstallToken.MatchString(token) || now.IsZero() {
		return WorkInstallInvitation{}, workevent.ErrInvalid
	}
	identityDigest, err := workspace.ServiceTokenDigest(ingestToken)
	if err != nil {
		return WorkInstallInvitation{}, workevent.ErrInvalid
	}
	result, err := scanWorkInstall(s.db.QueryRowContext(ctx, workInstallReadSQL+` WHERE invitation_digest=$1
 AND host_token_digest=$2`, digestWorkInstall(token), identityDigest))
	if errors.Is(err, sql.ErrNoRows) {
		return result, workevent.ErrUnavailable
	}
	if err != nil {
		return result, err
	}
	if result.State != "approved" && !now.UTC().Before(result.ExpiresAt) {
		result.State = "expired"
	}
	// Candidate names and mappings are disclosed only to the bearer that
	// reported them. No credential secret is returned by pickup.
	return result, nil
}

func (s *SourceFactsStore) ListWorkInstallInvitations(ctx context.Context, now time.Time, afterID string) ([]WorkInstallInvitation, error) {
	if s == nil || s.db == nil || now.IsZero() || (afterID != "" && !workInstallID.MatchString(afterID)) {
		return nil, workevent.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, workInstallReadSQL+` WHERE ((state IN ('invited','candidates') AND expires_at>$1)
 OR state='approved')
 AND ($2='' OR (created_at,invitation_id) < (SELECT created_at,invitation_id
    FROM work_event_install_invitations WHERE invitation_id=$2))
 ORDER BY created_at DESC, invitation_id DESC LIMIT 21`, now.UTC(), afterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []WorkInstallInvitation{}
	for rows.Next() {
		var item WorkInstallInvitation
		var projects, bindings, futureCandidates, exclusions, latestStatus []byte
		var sourceID, identityID sql.NullString
		var statusReceivedAt sql.NullTime
		if err = rows.Scan(&item.InvitationID, &item.DisplayName, &item.SourceInstance, &item.State, &projects,
			&sourceID, &identityID, &bindings, &item.ExpiresAt, &item.FutureAgentsAllowed, &futureCandidates, &exclusions,
			&latestStatus, &statusReceivedAt); err != nil {
			return nil, err
		}
		if len(latestStatus) > 0 {
			if !statusReceivedAt.Valid {
				return nil, workevent.ErrUnavailable
			}
			at := statusReceivedAt.Time.UTC()
			item.StatusReceivedAt = &at
		} else if statusReceivedAt.Valid {
			return nil, workevent.ErrUnavailable
		}
		// The list is deliberately compact. Read one invitation for candidates.
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *SourceFactsStore) ApproveWorkInstallInvitation(ctx context.Context, id, actor string,
	mappings []WorkInstallMapping, excludedKeys []string, futureAgentsAllowed bool, now time.Time, random io.Reader) (WorkInstallInvitation, error) {
	var result WorkInstallInvitation
	if s == nil || s.db == nil || !workInstallID.MatchString(id) || !workInstallSubject.MatchString(actor) ||
		len(mappings) < 1 || len(mappings)+len(excludedKeys) > 32 || now.IsZero() {
		return result, workevent.ErrInvalid
	}
	if random == nil {
		random = rand.Reader
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var candidateJSON []byte
	var hostDigest, hostPrefix string
	err = tx.QueryRowContext(ctx, `SELECT display_name,source_instance,state,candidates,host_token_digest,host_token_prefix,expires_at
 FROM work_event_install_invitations WHERE invitation_id=$1 FOR UPDATE`, id).
		Scan(&result.DisplayName, &result.SourceInstance, &result.State, &candidateJSON, &hostDigest, &hostPrefix, &result.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return result, workevent.ErrUnavailable
	}
	if err != nil {
		return result, err
	}
	result.InvitationID = id
	if result.State != "candidates" || !now.UTC().Before(result.ExpiresAt) {
		return result, workevent.ErrConflict
	}
	if json.Unmarshal(candidateJSON, &result.Projects) != nil || len(mappings)+len(excludedKeys) != len(result.Projects) {
		return result, workevent.ErrInvalid
	}
	byKey := map[string]WorkInstallProject{}
	for _, project := range result.Projects {
		byKey[project.ProjectKey] = project
	}
	seen := map[string]bool{}
	for _, mapping := range mappings {
		if _, ok := byKey[mapping.ProjectKey]; !ok || seen[mapping.ProjectKey] ||
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
		if _, ok := byKey[key]; !ok || seen[key] {
			return result, workevent.ErrInvalid
		}
		seen[key] = true
	}
	result.ExcludedFutureProjectKeys = append([]string{}, excludedKeys...)
	// Candidate order is the host-observed identity order. Project numbers are
	// installation-local and never inferred from same-looking UR numeric IDs.
	byMapping := map[string]WorkInstallMapping{}
	for _, mapping := range mappings {
		byMapping[mapping.ProjectKey] = mapping
	}
	var sourceID string
	err = tx.QueryRowContext(ctx, `INSERT INTO source_systems(source_key,source_type,display_name,deployment_ref)
 VALUES($1,'agent_runtime',$2,$3) RETURNING id::text`, "openclaw:"+id, result.DisplayName, result.SourceInstance).Scan(&sourceID)
	if err != nil {
		return result, err
	}
	identityID, err := workspace.NewID("xsi_", random)
	if err != nil {
		return result, err
	}
	credentialExpiry := now.UTC().Add(90 * 24 * time.Hour)
	_, err = tx.ExecContext(ctx, `INSERT INTO service_identities(id,display_name,status,permissions,created_by_subject,created_at,updated_at,expires_at)
 VALUES($1,$2,'active',ARRAY['work_event.ingest']::text[],'root',$3,$3,$4)`, identityID, result.DisplayName+" ingest", now.UTC(), credentialExpiry)
	if err != nil {
		return result, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO service_identity_tokens(token_digest,service_identity_id,token_prefix,created_at,expires_at)
 VALUES($1,$2,$3,$4,$5)`, hostDigest, identityID, hostPrefix, now.UTC(), credentialExpiry)
	if err != nil {
		return result, err
	}
	result.Bindings = []WorkInstallApprovedBinding{}
	for _, project := range result.Projects {
		if byMapping[project.ProjectKey].ProjectKey == "" {
			continue
		}
		mapping := byMapping[project.ProjectKey]
		xiID, xiName := mapping.XIProjectID, mapping.XIProjectName
		if xiID == "" {
			err = tx.QueryRowContext(ctx, `INSERT INTO review_capture_projects(xi_project_id,display_name)
 VALUES(gen_random_uuid(),$1) RETURNING xi_project_id::text`, xiName).Scan(&xiID)
			if err != nil {
				return result, err
			}
		} else {
			err = tx.QueryRowContext(ctx, `SELECT display_name FROM review_capture_projects WHERE xi_project_id=$1::uuid FOR SHARE`, xiID).Scan(&xiName)
			if err != nil {
				return result, err
			}
		}
		projectID := strconv.Itoa(len(result.Bindings) + 1)
		var collectionID string
		err = tx.QueryRowContext(ctx, `INSERT INTO work_event_collections(id,source_system_id,source_instance,org_id,project_id,profile,scope_generation,status)
 VALUES(gen_random_uuid(),$1::uuid,$2,'1',$3,$4,'1','active') RETURNING id::text`, sourceID, result.SourceInstance, projectID, workevent.CollectionProfile).Scan(&collectionID)
		if err != nil {
			return result, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO work_event_ingest_bindings(service_identity_id,collection_id,producer_project_key,project_label)
 VALUES($1,$2::uuid,$3,$4)`, identityID, collectionID, project.ProjectKey, project.Label)
		if err != nil {
			return result, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO review_body_ingest_bindings(service_identity_id,collection_id,status,revision)
 VALUES($1,$2::uuid,'active',1)`, identityID, collectionID)
		if err != nil {
			return result, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO review_capture_project_bindings(source_system_id,source_instance,org_id,project_id,xi_project_id,revision)
 VALUES($1::uuid,$2,'1',$3,$4::uuid,1)`, sourceID, result.SourceInstance, projectID, xiID)
		if err != nil {
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
			_, err = tx.ExecContext(ctx, `INSERT INTO work_event_ur_targets(collection_id,target_source_system_id,target_source_instance,
 target_org_id,target_project_id,target_label) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6)`, collectionID,
				mapping.URTarget.SourceSystemID, mapping.URTarget.SourceInstance, mapping.URTarget.OrgID,
				mapping.URTarget.ProjectID, "UniRouter project "+mapping.URTarget.ProjectID)
			if err != nil {
				return result, err
			}
		}
		result.Bindings = append(result.Bindings, WorkInstallApprovedBinding{ProjectKey: project.ProjectKey,
			ProjectLabel: project.Label, ProjectID: projectID, CollectionID: collectionID, XIProjectID: xiID,
			XIProjectName: xiName, URTarget: mapping.URTarget})
	}
	bindingsJSON, _ := json.Marshal(result.Bindings)
	exclusionsJSON, _ := json.Marshal(result.ExcludedFutureProjectKeys)
	_, err = tx.ExecContext(ctx, `UPDATE work_event_install_invitations SET state='approved',
 approved_source_system_id=$2::uuid,approved_service_identity_id=$3,approved_bindings=$4::jsonb,
 future_agents_allowed=$5,excluded_future_project_keys=$6::jsonb,updated_at=$7 WHERE invitation_id=$1`,
		id, sourceID, identityID, bindingsJSON, futureAgentsAllowed, exclusionsJSON, now.UTC())
	if err != nil {
		return result, err
	}
	if err = insertAccessAudit(ctx, tx, "human", actor, "work_event_install.approve", "source_system", sourceID, "accepted", now.UTC()); err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	result.State, result.SourceSystemID, result.ServiceIdentityID, result.FutureAgentsAllowed = "approved", sourceID, identityID, futureAgentsAllowed
	return result, nil
}
