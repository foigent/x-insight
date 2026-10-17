package db

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"io"
	"slices"
	"sort"
	"time"
)

type ConnectorSetupRecord struct {
	Installation   connectorcontrol.Installation
	Actor          string
	RequestSHA256  string
	Consent        connectorcontrol.SetupConsentView
	ReusesPolicy   bool
	PolicyRevision int64
	AppliedAt      *time.Time
	ErrorCode      string
}

func (s *WorkspaceStore) ReadConnectorSetup(ctx context.Context, id string) (ConnectorSetupRecord, error) {
	var r ConnectorSetupRecord
	if err := s.requireConnectorDeployment(ctx, id); err != nil {
		return r, err
	}
	i, err := loadConnectorInstallation(ctx, s.db, id)
	if err != nil {
		return r, err
	}
	r.Installation = i
	var raw []byte
	var applied sql.NullTime
	var revision sql.NullInt64
	err = s.db.QueryRowContext(ctx, `SELECT setup_actor_subject,setup_request_sha256,setup_consent,setup_reuses_policy,setup_policy_revision,setup_policy_applied_at,COALESCE(setup_error_code,'') FROM connector_installations WHERE id=$1 AND setup_request_id IS NOT NULL`, id).Scan(&r.Actor, &r.RequestSHA256, &raw, &r.ReusesPolicy, &revision, &applied, &r.ErrorCode)
	if errors.Is(err, sql.ErrNoRows) {
		return r, connectorcontrol.ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if err = decodeStrictConnectorJSON(raw, &r.Consent); err != nil {
		return r, err
	}
	r.PolicyRevision = revision.Int64
	if applied.Valid {
		r.AppliedAt = &applied.Time
	}
	return r, nil
}
func (s *WorkspaceStore) CreateSetupInvitation(ctx context.Context, input connectorcontrol.CreateInvitationInput, put connectorcontrol.SetupConsentPut, requestSHA string) (connectorcontrol.SetupCreated, error) {
	var result connectorcontrol.SetupCreated
	if s == nil || s.db == nil || !put.Valid() || !connectorcontrol.IsSHA256Reference(requestSHA) || input.Random == nil {
		return result, connectorcontrol.ErrInvalid
	}
	// The new setup package/consent must match the original PostgreSQL record
	// exactly on initial response and replay, without changing the legacy issuer.
	input.Now = input.Now.UTC().Truncate(time.Microsecond)
	// Original minting receives exactly the same 16+16+32 bytes on a replay.
	raw, err := io.ReadAll(io.LimitReader(input.Random, 65))
	if err != nil || len(raw) != 64 {
		return result, connectorcontrol.ErrInvalid
	}
	defer clear(raw)
	random := bytes.NewReader(raw)
	id, err := connectorcontrol.NewID("xci_", random)
	if err != nil {
		return result, err
	}
	inviteID, err := connectorcontrol.NewID("xii_", random)
	if err != nil {
		return result, err
	}
	token, digest, err := connectorcontrol.NewInvitationToken(random)
	if err != nil {
		return result, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err = requireConnectorActivationAuthority(ctx, tx); err != nil {
		return result, err
	}
	var existingID, actor, hash string
	var consentRaw []byte
	err = tx.QueryRowContext(ctx, `SELECT id,setup_actor_subject,setup_request_sha256,setup_consent FROM connector_installations WHERE setup_request_id=$1::uuid`, put.RequestID).Scan(&existingID, &actor, &hash, &consentRaw)
	if err == nil {
		if existingID != id || actor != input.ActorSubject || hash != requestSHA {
			return result, connectorcontrol.ErrConflict
		}
		if err = decodeStrictConnectorJSON(consentRaw, &result.Consent); err != nil {
			return result, err
		}
		var savedDigest, status string
		var expires time.Time
		if err = tx.QueryRowContext(ctx, `SELECT nonce_digest,status,expires_at FROM connector_installation_invitations WHERE id=$1 AND installation_id=$2`, inviteID, id).Scan(&savedDigest, &status, &expires); err != nil {
			return result, err
		}
		if savedDigest != digest || result.Consent.DeploymentID != input.Config.DeploymentID || result.Consent.XIOrigin != input.Config.XInsightOrigin || result.Consent.UROrigin != input.Config.UniRouterInstallOrigin || !expires.Equal(result.Consent.ExpiresAt) || !input.Now.Before(expires) || status != "active" && status != "consumed" {
			return result, connectorcontrol.ErrConflict
		}
		url, err := connectorcontrol.BuildInstallURL(input.Config.UniRouterInstallOrigin, token, input.Config.XInsightOrigin)
		if err != nil {
			return result, err
		}
		expires = expires.UTC()
		result.Invitation = connectorcontrol.InvitationPackage{SchemaVersion: connectorcontrol.InvitationSchema, InvitationID: inviteID, InstallationID: id, DeploymentID: input.Config.DeploymentID, Audience: connectorcontrol.ConnectorAudience, XInsightOrigin: input.Config.XInsightOrigin, RequestedEventScope: []string{connectorcontrol.CompletedEventType}, ExpiresAt: expires, InstallURL: url, Token: token}
		return result, tx.Commit()
	} else if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	var current, effective int64
	var mode sql.NullString
	var retention, quota, max sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT desired_revision,effective_revision,effective_mode,effective_retention_seconds,effective_project_quota_bytes,effective_max_item_bytes FROM review_capture_defaults WHERE singleton FOR SHARE`).Scan(&current, &effective, &mode, &retention, &quota, &max)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	if current != put.ExpectedCustodyRevision {
		return result, reviewbody.ErrConflict
	}
	settings := put.Custody
	if settings == nil {
		if effective < 1 || effective != current || !mode.Valid {
			return result, reviewbody.ErrPolicy
		}
		settings = &connectorcontrol.SetupCustody{Mode: mode.String, RetentionSeconds: retention.Int64, ProjectQuotaBytes: quota.Int64, MaxItemBytes: max.Int64}
	}
	input.Random = bytes.NewReader(raw)
	invitation, _, err := s.createInvitationTx(ctx, tx, input)
	if err != nil {
		return result, err
	}
	invitation.ExpiresAt = invitation.ExpiresAt.UTC().Truncate(time.Microsecond)
	view := connectorcontrol.SetupConsentView{SchemaVersion: connectorcontrol.SetupConsentViewSchema, InstallationID: id, DeploymentID: input.Config.DeploymentID, XIOrigin: input.Config.XInsightOrigin, UROrigin: input.Config.UniRouterInstallOrigin, ConsentRevision: 1, ExpiresAt: invitation.ExpiresAt, Custody: *settings, ExpectedCustodyRevision: current}
	view.ConsentSHA256, _, err = connectorcontrol.CanonicalSHA256(view)
	if err != nil {
		return result, err
	}
	viewRaw, err := json.Marshal(view)
	if err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE connector_installations SET setup_request_id=$2::uuid,setup_actor_subject=$3,setup_request_sha256=$4,setup_consent=$5::jsonb,setup_reuses_policy=$6 WHERE id=$1`, id, put.RequestID, input.ActorSubject, requestSHA, viewRaw, put.Custody == nil); err != nil {
		return result, err
	}
	if err = insertAccessAudit(ctx, tx, "human", input.ActorSubject, "connector_setup.consent", "connector_installation", id, "accepted", input.Now); err != nil {
		return result, err
	}
	result = connectorcontrol.SetupCreated{Invitation: invitation, Consent: view}
	return result, tx.Commit()
}
func (s *WorkspaceStore) ResolveSetupInvitation(ctx context.Context, token string, now time.Time) (connectorcontrol.SetupCreated, error) {
	var out connectorcontrol.SetupCreated
	digest, err := connectorcontrol.InvitationTokenDigest(token)
	if err != nil || now.IsZero() {
		return out, connectorcontrol.ErrInvalid
	}
	var raw []byte
	var scopeRaw, status, urOrigin string
	i := &out.Invitation
	err = s.db.QueryRowContext(ctx, `SELECT c.setup_consent,i.id,i.installation_id,c.deployment_id,i.audience,i.xinsight_origin,i.unirouter_install_origin,array_to_json(i.requested_event_scope)::text,i.status,i.expires_at
 FROM connector_installation_invitations i JOIN connector_installations c ON c.id=i.installation_id
 WHERE i.nonce_digest=$1 AND c.setup_request_id IS NOT NULL AND i.requested_event_scope=c.requested_event_scope`, digest).Scan(&raw, &i.InvitationID, &i.InstallationID, &i.DeploymentID, &i.Audience, &i.XInsightOrigin, &urOrigin, &scopeRaw, &status, &i.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return out, connectorcontrol.ErrNotFound
	}
	if err != nil {
		return out, err
	}
	i.ExpiresAt = i.ExpiresAt.UTC()
	if s.connectorDeploymentID != "" && i.DeploymentID != s.connectorDeploymentID {
		return out, connectorcontrol.ErrConflict
	}
	if !now.Before(i.ExpiresAt) {
		return out, connectorcontrol.ErrExpired
	}
	if status != "active" && status != "consumed" {
		return out, connectorcontrol.ErrConsumed
	}
	if err = decodeStrictConnectorJSON(raw, &out.Consent); err != nil {
		return out, connectorcontrol.ErrInvalid
	}
	if err = json.Unmarshal([]byte(scopeRaw), &i.RequestedEventScope); err != nil {
		return out, connectorcontrol.ErrInvalid
	}
	i.SchemaVersion = connectorcontrol.InvitationSchema
	i.InstallURL, err = connectorcontrol.BuildInstallURL(urOrigin, token, i.XInsightOrigin)
	if err != nil || i.Audience != connectorcontrol.ConnectorAudience || urOrigin != out.Consent.UROrigin || !connectorcontrol.ValidSetupResponse(connectorcontrol.SetupLocalRequest{Action: "create"}, connectorcontrol.SetupLocalResponse{SchemaVersion: connectorcontrol.LocalSetupResponseSchema, Action: "create", Created: &out}) {
		return connectorcontrol.SetupCreated{}, connectorcontrol.ErrConflict
	}
	return out, nil
}
func (s *WorkspaceStore) ListPendingConnectorSetups(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM connector_installations WHERE setup_request_id IS NOT NULL AND setup_policy_applied_at IS NULL AND installation_acknowledgement_sha256 IS NOT NULL AND deployment_id=$1 AND desired_state='active' AND status NOT IN ('paused','disconnected') ORDER BY created_at,id LIMIT 100`, s.connectorDeploymentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (s *WorkspaceStore) RecordConnectorSetupResult(ctx context.Context, id string, applied bool, code string) error {
	if err := s.requireConnectorDeployment(ctx, id); err != nil {
		return err
	}
	if len(code) > 128 {
		return connectorcontrol.ErrInvalid
	}
	if applied {
		_, err := s.db.ExecContext(ctx, `UPDATE connector_installations SET setup_policy_applied_at=COALESCE(setup_policy_applied_at,clock_timestamp()),setup_error_code=NULL WHERE id=$1 AND setup_policy_revision IS NOT NULL`, id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE connector_installations SET setup_error_code=$2 WHERE id=$1 AND setup_policy_applied_at IS NULL`, id, code)
	return err
}

// PrepareConnectorSetupPolicy records the original policy intent and its exact
// target revision in one transaction. Retrying cannot request a second revision.
func (s *WorkspaceStore) PrepareConnectorSetupPolicy(ctx context.Context, id, actor string, now time.Time) (int64, error) {
	if err := s.requireConnectorDeployment(ctx, id); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err = requireConnectorActivationAuthority(ctx, tx); err != nil {
		return 0, err
	}
	var savedActor, desired, status string
	var raw []byte
	var revision sql.NullInt64
	var reuse bool
	var ack sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT setup_actor_subject,setup_consent,setup_reuses_policy,setup_policy_revision,desired_state,status,installation_acknowledgement_sha256 FROM connector_installations WHERE id=$1 AND setup_request_id IS NOT NULL FOR UPDATE`, id).Scan(&savedActor, &raw, &reuse, &revision, &desired, &status, &ack)
	if err != nil {
		return 0, err
	}
	if savedActor != actor || desired != "active" || status == "paused" || status == "disconnected" || !ack.Valid {
		return 0, connectorcontrol.ErrConflict
	}
	var consent connectorcontrol.SetupConsentView
	if err = decodeStrictConnectorJSON(raw, &consent); err != nil {
		return 0, err
	}
	if revision.Valid {
		return revision.Int64, tx.Commit()
	}
	policy := ReviewCaptureSettings{Mode: consent.Custody.Mode, RetentionSeconds: consent.Custody.RetentionSeconds, ProjectQuotaBytes: consent.Custody.ProjectQuotaBytes, MaxItemBytes: consent.Custody.MaxItemBytes}
	var target int64
	if reuse {
		var current, effective int64
		var actual ReviewCaptureSettings
		err = tx.QueryRowContext(ctx, `SELECT desired_revision,effective_revision,effective_mode,effective_retention_seconds,effective_project_quota_bytes,effective_max_item_bytes FROM review_capture_defaults WHERE singleton FOR SHARE`).Scan(&current, &effective, &actual.Mode, &actual.RetentionSeconds, &actual.ProjectQuotaBytes, &actual.MaxItemBytes)
		if err != nil {
			return 0, err
		}
		if current != consent.ExpectedCustodyRevision || effective != current || actual != policy {
			return 0, reviewbody.ErrConflict
		}
		target = current
	} else {
		target, err = requestReviewCaptureDefaultTx(ctx, tx, policy, consent.ExpectedCustodyRevision, actor, now)
		if err != nil {
			return 0, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE connector_installations SET setup_policy_revision=$2,setup_error_code=NULL WHERE id=$1`, id, target); err != nil {
		return 0, err
	}
	return target, tx.Commit()
}

// Receiving is constrained by the original per-installation enrollment and
// exact deterministic stream IDs. A similar source tuple alone is insufficient.
func (s *WorkspaceStore) ReadConnectorSetupReceiving(ctx context.Context, id string, since time.Time) (connectorcontrol.SetupReceiving, error) {
	return s.readConnectorSetupReceiving(ctx, id, since, nil)
}

// Only the privileged setup manager supplies this root-qualified snapshot.
// Public requests never select collection IDs or supply a configuration.
func (s *WorkspaceStore) ReadConnectorSetupReceivingWithOriginalConfig(ctx context.Context, id string, since time.Time, original sourcefacts.ManagedConfig) (connectorcontrol.SetupReceiving, error) {
	if err := original.Validate(); err != nil {
		return connectorcontrol.SetupReceiving{}, err
	}
	return s.readConnectorSetupReceiving(ctx, id, since, &original)
}
func (s *WorkspaceStore) readConnectorSetupReceiving(ctx context.Context, id string, since time.Time, original *sourcefacts.ManagedConfig) (connectorcontrol.SetupReceiving, error) {
	out := connectorcontrol.SetupReceiving{ProfilesReceived: []string{}}
	type matchedConfig struct {
		cfg sourcefacts.ManagedConfig
		ok  bool
	}
	matches := map[string]matchedConfig{}
	projectConfig := func(digest, source, org, project string) (sourcefacts.ManagedConfig, bool) {
		key := digest + "\x00" + source + "\x00" + org + "\x00" + project
		if v, present := matches[key]; present {
			return v.cfg, v.ok
		}
		v := matchedConfig{}
		if original != nil && digest != "" {
			for _, facts := range []bool{true, false} {
				for _, content := range []bool{true, false} {
					cfg := sourcefacts.ManagedProjectConfig(*original, source, org, project, facts, content)
					raw, err := json.Marshal(cfg)
					if err == nil && cfg.Validate() == nil && sourcefacts.Hash(raw) == digest {
						v = matchedConfig{cfg: cfg, ok: true}
					}
				}
			}
		}
		matches[key] = v
		return v.cfg, v.ok
	}

	if err := s.requireConnectorDeployment(ctx, id); err != nil {
		return out, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.enrollment_id,e.project_id,COALESCE(e.configuration_sha256,''),f.id::text,f.source_system_id::text,f.source_instance,f.org_id,f.profile,f.scope_generation,GREATEST(f.last_received_at,(SELECT s.last_success_at FROM source_runtime_snapshots s WHERE s.stream_id=f.id)),(EXISTS(SELECT 1 FROM source_fact_documents d WHERE d.source_system_id=f.source_system_id AND d.source_instance=f.source_instance AND d.org_id=f.org_id AND d.project_id=f.project_id AND d.profile=f.profile AND d.received_at>=$2) OR EXISTS(SELECT 1 FROM source_observation_documents d WHERE d.source_system_id=f.source_system_id AND d.source_instance=f.source_instance AND d.org_id=f.org_id AND d.project_id=f.project_id AND d.profile=f.profile AND d.received_at>=$2) OR EXISTS(SELECT 1 FROM source_runtime_snapshot_pages p WHERE p.stream_id=f.id AND p.received_at>=$2)) FROM connector_source_enrollments e JOIN source_fact_streams f ON f.source_instance=e.source_instance AND f.org_id=e.org_id AND f.project_id=e.project_id WHERE e.installation_id=$1 AND e.status='active' AND f.status='active'`, id, since)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var enrollment, project, digest, stream, system, source, org, profile, generation string
		var at sql.NullTime
		var received bool
		if err = rows.Scan(&enrollment, &project, &digest, &stream, &system, &source, &org, &profile, &generation, &at, &received); err != nil {
			return out, err
		}
		if stream != connectorcontrol.ManagedCollectionID(id, enrollment, project, "profile:"+profile) {
			cfg, matched := projectConfig(digest, source, org, project)
			allowed := false
			if matched {
				for _, p := range cfg.Profiles {
					if p.Matches(sourcefacts.Stream{ID: stream, SourceSystemID: system, SourceInstance: source, OrgID: org, ProjectID: project, Profile: profile, ScopeGeneration: generation}) {
						allowed = true
						break
					}
				}
			}
			if !allowed {
				continue
			}
		}
		if at.Valid && !at.Time.Before(since) && (out.LastSuccessAt == nil || at.Time.After(*out.LastSuccessAt)) {
			v := at.Time
			out.LastSuccessAt = &v
		}
		if received && !seen[profile] {
			seen[profile] = true
			out.ProfilesReceived = append(out.ProfilesReceived, profile)
		}
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	rows.Close()
	budgets, err := s.db.QueryContext(ctx, `SELECT e.enrollment_id,e.project_id,COALESCE(e.configuration_sha256,''),b.id::text,b.source_system_id::text,b.source_instance,b.org_id,b.scope_generation,array_to_json(b.profiles)::text,b.last_success_at,p.profile,
 EXISTS(SELECT 1 FROM source_budget_receipts r JOIN source_budget_readings d ON d.id=r.source_reading_id WHERE r.collection_id=b.id AND r.received_at>=$2 AND p.profile=ANY(d.profiles))
 FROM connector_source_enrollments e JOIN source_budget_collections b ON b.source_instance=e.source_instance AND b.org_id=e.org_id AND b.project_id=e.project_id CROSS JOIN LATERAL unnest(b.profiles) p(profile)
 WHERE e.installation_id=$1 AND e.status='active' AND b.status='active'`, id, since)
	if err != nil {
		return out, err
	}
	for budgets.Next() {
		var enrollment, project, digest, collection, system, source, org, generation, profile, profilesJSON string
		var at sql.NullTime
		var received bool
		if err = budgets.Scan(&enrollment, &project, &digest, &collection, &system, &source, &org, &generation, &profilesJSON, &at, &profile, &received); err != nil {
			budgets.Close()
			return out, err
		}
		if collection != connectorcontrol.ManagedCollectionID(id, enrollment, project, "budget") {
			var profiles []string
			if err = json.Unmarshal([]byte(profilesJSON), &profiles); err != nil {
				budgets.Close()
				return out, err
			}
			cfg, matched := projectConfig(digest, source, org, project)
			allowed := false
			if matched {
				for _, b := range cfg.Budgets {
					if slices.Contains(b.Profiles, profile) && sourcefacts.SameBudgetCollection(b.Registration(), sourcefacts.BudgetCollection{ID: collection, SourceSystemID: system, SourceInstance: source, OrgID: org, ProjectID: project, ScopeGeneration: generation, Profiles: profiles}) {
						allowed = true
						break
					}
				}
			}
			if !allowed {
				continue
			}
		}
		if at.Valid && !at.Time.Before(since) && (out.LastSuccessAt == nil || at.Time.After(*out.LastSuccessAt)) {
			v := at.Time
			out.LastSuccessAt = &v
		}
		if received && !seen[profile] {
			seen[profile] = true
			out.ProfilesReceived = append(out.ProfilesReceived, profile)
		}
	}
	if err = budgets.Err(); err != nil {
		budgets.Close()
		return out, err
	}
	budgets.Close()
	var received, read, success sql.NullTime
	err = s.db.QueryRowContext(ctx, `SELECT max(o.accepted_at) FILTER(WHERE o.accepted_at>=$2 AND o.state IN ('accepted','expired')),max(a.occurred_at) FILTER(WHERE a.occurred_at>=$2),max(c.last_success_at) FILTER(WHERE c.last_success_at>=$2) FROM connector_source_enrollments e JOIN source_content_collections c ON c.id=e.content_collection_id AND c.source_instance=e.source_instance AND c.org_id=e.org_id AND c.project_id=e.project_id LEFT JOIN review_body_operations o ON o.content_collection_id=c.id LEFT JOIN access_audit_records a ON a.resource_ref=o.content_ref AND a.resource_kind='review_body' AND a.action='review_body.read' AND a.result='accepted' WHERE e.installation_id=$1 AND e.status='active' AND NOT e.legacy_c_copy_pending`, id, since).Scan(&received, &read, &success)
	if err != nil {
		return out, err
	}
	if received.Valid {
		out.ContentReceivedAt = &received.Time
	}
	if read.Valid {
		out.ContentReadVerifiedAt = &read.Time
	}
	if success.Valid && (out.LastSuccessAt == nil || success.Time.After(*out.LastSuccessAt)) {
		out.LastSuccessAt = &success.Time
	}
	sort.Strings(out.ProfilesReceived)
	return out, nil
}

func (s *WorkspaceStore) CheckConnectorSetupControlAccess(ctx context.Context, id, identity string, now time.Time) error {
	if err := s.requireConnectorDeployment(ctx, id); err != nil {
		return err
	}
	var ok bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM connector_installations c JOIN service_identities i ON i.id=c.control_service_identity_id WHERE c.id=$1 AND i.id=$2 AND c.installation_acknowledgement_sha256 IS NOT NULL AND c.desired_state='active' AND c.status NOT IN ('paused','disconnected') AND i.status='active' AND i.expires_at>$3 AND i.permissions @> ARRAY['connector.control.poll']::text[])`, id, identity, now).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return connectorcontrol.ErrSetupUnauthorized
	}
	return nil
}

func (s *WorkspaceStore) ConnectorSetupRequestExists(ctx context.Context, request string) (bool, error) {
	var exists bool
	if !connectorcontrol.SetupRequestIDPattern.MatchString(request) {
		return false, connectorcontrol.ErrInvalid
	}
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM connector_installations WHERE setup_request_id=$1::uuid)`, request).Scan(&exists)
	return exists, err
}
