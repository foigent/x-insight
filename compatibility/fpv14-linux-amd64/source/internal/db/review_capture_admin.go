package db

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"time"

	"github.com/foigent/x-insight/internal/reviewbody"
)

type ReviewCaptureSettings struct {
	Mode              string `json:"mode"`
	RetentionSeconds  int64  `json:"retention_seconds"`
	ProjectQuotaBytes int64  `json:"project_quota_bytes"`
	MaxItemBytes      int64  `json:"max_item_bytes"`
}

type ReviewCaptureDefault struct {
	Desired           ReviewCaptureSettings  `json:"desired"`
	DesiredRevision   int64                  `json:"desired_revision"`
	Effective         *ReviewCaptureSettings `json:"effective,omitempty"`
	EffectiveRevision int64                  `json:"effective_revision"`
	RequestedAt       time.Time              `json:"requested_at"`
	EffectiveAt       *time.Time             `json:"effective_at,omitempty"`
}

func validReviewCaptureSettings(p ReviewCaptureSettings) bool {
	return (p.Mode == "disabled" || p.Mode == "full_content") && p.RetentionSeconds >= 1 &&
		p.RetentionSeconds <= 31536000 && p.ProjectQuotaBytes >= 1 && p.ProjectQuotaBytes <= 1<<40 &&
		p.MaxItemBytes >= 0 && p.MaxItemBytes <= 32<<20
}

func reviewCaptureOwnerTx(ctx context.Context, tx *sql.Tx) error {
	var owner bool
	if err := tx.QueryRowContext(ctx, `SELECT pg_has_role(current_user,relowner,'USAGE') FROM pg_class
 WHERE oid='public.review_capture_defaults'::regclass`).Scan(&owner); err != nil {
		return err
	}
	if !owner {
		return reviewbody.ErrScope
	}
	return nil
}

// RequestReviewCaptureDefault records intent only. A configured/default form
// value never enables new copies until ConfirmReviewCaptureDefault commits it.
func (s *SourceFactsStore) RequestReviewCaptureDefault(ctx context.Context, policy ReviewCaptureSettings,
	expectedRevision int64, operator string, now time.Time) (int64, error) {
	if s == nil || s.db == nil || !validReviewCaptureSettings(policy) || expectedRevision < 0 ||
		operator == "" || now.IsZero() {
		return 0, reviewbody.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	revision, err := requestReviewCaptureDefaultTx(ctx, tx, policy, expectedRevision, operator, now)
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return revision, nil
}

// Shared by the setup transaction so the original policy CAS and installation
// progress marker cannot be separated by a crash or lost response.
func requestReviewCaptureDefaultTx(ctx context.Context, tx *sql.Tx, policy ReviewCaptureSettings, expectedRevision int64, operator string, now time.Time) (int64, error) {
	if tx == nil || !validReviewCaptureSettings(policy) || expectedRevision < 0 || operator == "" || now.IsZero() {
		return 0, reviewbody.ErrInvalid
	}
	if err := reviewCaptureOwnerTx(ctx, tx); err != nil {
		return 0, err
	}
	var current int64
	err := tx.QueryRowContext(ctx, `SELECT desired_revision FROM review_capture_defaults WHERE singleton FOR UPDATE`).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		current = 0
	} else if err != nil {
		return 0, err
	}
	if current != expectedRevision {
		return 0, reviewbody.ErrConflict
	}
	now = now.UTC().Truncate(time.Microsecond)
	if current == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO review_capture_defaults(singleton,desired_revision,desired_mode,
 desired_retention_seconds,desired_project_quota_bytes,desired_max_item_bytes,requested_at)
 VALUES(true,1,$1,$2,$3,$4,$5)`, policy.Mode, policy.RetentionSeconds, policy.ProjectQuotaBytes, policy.MaxItemBytes, now)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE review_capture_defaults SET desired_revision=desired_revision+1,
 desired_mode=$1,desired_retention_seconds=$2,desired_project_quota_bytes=$3,desired_max_item_bytes=$4,
 requested_at=$5,updated_at=clock_timestamp() WHERE singleton`, policy.Mode, policy.RetentionSeconds,
			policy.ProjectQuotaBytes, policy.MaxItemBytes, now)
	}
	if err != nil {
		return 0, err
	}
	if err = insertAccessAudit(ctx, tx, "human", operator, "review.policy.request", "review_capture_default", "xi-tenant", "accepted", now); err != nil {
		return 0, err
	}
	return current + 1, nil
}

// Confirmation is a separate administrator action. Concurrent uploads hold
// FOR SHARE on this row, so the old effective version survives a new request.
func (s *SourceFactsStore) ConfirmReviewCaptureDefault(ctx context.Context, desiredRevision int64,
	operator string, now time.Time) error {
	if s == nil || s.db == nil || desiredRevision < 1 || operator == "" || now.IsZero() {
		return reviewbody.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = reviewCaptureOwnerTx(ctx, tx); err != nil {
		return err
	}
	var desired, effective int64
	if err = tx.QueryRowContext(ctx, `SELECT desired_revision,effective_revision FROM review_capture_defaults WHERE singleton FOR UPDATE`).Scan(&desired, &effective); err != nil {
		return err
	}
	if desired != desiredRevision || effective >= desiredRevision {
		return reviewbody.ErrConflict
	}
	now = now.UTC().Truncate(time.Microsecond)
	if _, err = tx.ExecContext(ctx, `UPDATE review_capture_defaults SET effective_revision=desired_revision,
 effective_mode=desired_mode,effective_retention_seconds=desired_retention_seconds,
 effective_project_quota_bytes=desired_project_quota_bytes,effective_max_item_bytes=desired_max_item_bytes,
 effective_at=$1,updated_at=clock_timestamp() WHERE singleton`, now); err != nil {
		return err
	}
	if err = insertAccessAudit(ctx, tx, "human", operator, "review.policy.confirm", "review_capture_default", "xi-tenant", "accepted", now); err != nil {
		return err
	}
	return tx.Commit()
}

// Only the exact saved revision can be tested; the subsequent confirmation
// still takes its existing exclusive gate and CAS. Disabled policies need no key.
func (s *SourceFactsStore) ReviewCaptureDesiredNeedsCustody(ctx context.Context, ref string, revision int64) (bool, error) {
	if s == nil || s.db == nil || revision < 1 {
		return false, reviewbody.ErrInvalid
	}
	var mode string
	var err error
	if ref == "" {
		err = s.db.QueryRowContext(ctx, `SELECT desired_mode FROM review_capture_defaults WHERE singleton AND desired_revision=$1`, revision).Scan(&mode)
	} else {
		err = s.db.QueryRowContext(ctx, `SELECT desired_mode FROM review_capture_exceptions WHERE policy_ref=$1 AND desired_revision=$2`, ref, revision).Scan(&mode)
	}
	return mode == "full_content", err
}

func (s *WorkspaceStore) ReadReviewCaptureDefault(ctx context.Context) (ReviewCaptureDefault, error) {
	var result ReviewCaptureDefault
	if s == nil || s.db == nil {
		return result, reviewbody.ErrInvalid
	}
	var mode sql.NullString
	var retention, quota, maxItem sql.NullInt64
	var effectiveAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT desired_revision,desired_mode,desired_retention_seconds,
 desired_project_quota_bytes,desired_max_item_bytes,effective_revision,effective_mode,
 effective_retention_seconds,effective_project_quota_bytes,effective_max_item_bytes,
 requested_at,effective_at FROM review_capture_defaults WHERE singleton`).Scan(&result.DesiredRevision,
		&result.Desired.Mode, &result.Desired.RetentionSeconds, &result.Desired.ProjectQuotaBytes,
		&result.Desired.MaxItemBytes, &result.EffectiveRevision, &mode, &retention, &quota, &maxItem,
		&result.RequestedAt, &effectiveAt)
	if err != nil {
		return result, err
	}
	if result.EffectiveRevision > 0 {
		result.Effective = &ReviewCaptureSettings{Mode: mode.String, RetentionSeconds: retention.Int64,
			ProjectQuotaBytes: quota.Int64, MaxItemBytes: maxItem.Int64}
		result.EffectiveAt = &effectiveAt.Time
	}
	return result, nil
}

var reviewExceptionRef = regexp.MustCompile(`^xipe_[a-f0-9]{32}$`)

type ReviewCaptureException struct {
	PolicyRef        string `json:"policy_ref"`
	SelectorKind     string `json:"selector_kind"`
	SourceSystemID   string `json:"source_system_id,omitempty"`
	SourceInstance   string `json:"source_instance,omitempty"`
	OrgID            string `json:"org_id,omitempty"`
	ProjectID        string `json:"project_id,omitempty"`
	AccountAuthority string `json:"account_authority,omitempty"`
	AccountID        string `json:"account_id,omitempty"`
	ContentKind      string `json:"content_kind,omitempty"`
	Mode             string `json:"mode"`
	RetentionSeconds int64  `json:"retention_seconds"`
	MaxItemBytes     int64  `json:"max_item_bytes"`
	Provenance       string `json:"provenance"`
}

func validReviewCaptureException(p ReviewCaptureException) bool {
	if !reviewExceptionRef.MatchString(p.PolicyRef) || (p.Mode != "disabled" && p.Mode != "full_content") ||
		p.RetentionSeconds < 1 || p.RetentionSeconds > 31536000 || p.MaxItemBytes < 0 || p.MaxItemBytes > 32<<20 ||
		p.Provenance == "" || len(p.Provenance) > 256 {
		return false
	}
	category := p.ContentKind == "" || p.ContentKind == "user_input" || p.ContentKind == "user_output" ||
		p.ContentKind == "model_input" || p.ContentKind == "model_output" || p.ContentKind == "tool_input" ||
		p.ContentKind == "tool_output" || p.ContentKind == "client_request" || p.ContentKind == "client_response" ||
		p.ContentKind == "provider_request" || p.ContentKind == "provider_response"
	if !category {
		return false
	}
	if p.SelectorKind == "tenant_category" {
		return p.SourceSystemID == "" && p.SourceInstance == "" && p.OrgID == "" && p.ProjectID == "" &&
			p.AccountAuthority == "" && p.AccountID == "" && p.ContentKind != ""
	}
	if !workEventUUID.MatchString(p.SourceSystemID) || p.SourceInstance == "" ||
		!workEventGeneration.MatchString(p.OrgID) || !workEventGeneration.MatchString(p.ProjectID) {
		return false
	}
	switch p.SelectorKind {
	case "source_project":
		return p.AccountAuthority == "" && p.AccountID == "" && p.ContentKind == ""
	case "source_project_category":
		return p.AccountAuthority == "" && p.AccountID == "" && p.ContentKind != ""
	case "account":
		return p.AccountAuthority != "" && p.AccountID != "" && p.ContentKind == ""
	case "account_category":
		return p.AccountAuthority != "" && p.AccountID != "" && p.ContentKind != ""
	}
	return false
}

// RequestReviewCaptureException is desired-only. The exact selector is
// immutable for a ref; callers use a new ref for a different scope.
func (s *SourceFactsStore) RequestReviewCaptureException(ctx context.Context, p ReviewCaptureException,
	expectedRevision int64, operator string, now time.Time) (int64, error) {
	if s == nil || s.db == nil || !validReviewCaptureException(p) || expectedRevision < 0 || operator == "" || now.IsZero() {
		return 0, reviewbody.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err = reviewCaptureOwnerTx(ctx, tx); err != nil {
		return 0, err
	}
	if err = verifiedReviewExceptionSelectorTx(ctx, tx, p); err != nil {
		return 0, err
	}
	var current int64
	err = tx.QueryRowContext(ctx, `SELECT desired_revision FROM review_capture_exceptions WHERE policy_ref=$1 FOR UPDATE`, p.PolicyRef).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		current = 0
	} else if err != nil {
		return 0, err
	}
	if current != expectedRevision {
		return 0, reviewbody.ErrConflict
	}
	now = now.UTC().Truncate(time.Microsecond)
	if current == 0 {
		var source any
		if p.SourceSystemID != "" {
			source = p.SourceSystemID
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO review_capture_exceptions(policy_ref,selector_kind,source_system_id,
 source_instance,org_id,project_id,account_authority,account_id,content_kind,desired_revision,desired_mode,
 desired_retention_seconds,desired_max_item_bytes,provenance,requested_at)
 VALUES($1,$2,$3::uuid,$4,$5,$6,$7,$8,$9,1,$10,$11,$12,$13,$14)`, p.PolicyRef, p.SelectorKind, source,
			p.SourceInstance, p.OrgID, p.ProjectID, p.AccountAuthority, p.AccountID, p.ContentKind, p.Mode,
			p.RetentionSeconds, p.MaxItemBytes, p.Provenance, now)
	} else {
		var existing ReviewCaptureException
		var source sql.NullString
		err = tx.QueryRowContext(ctx, `SELECT selector_kind,source_system_id::text,source_instance,org_id,project_id,
 account_authority,account_id,content_kind FROM review_capture_exceptions WHERE policy_ref=$1`, p.PolicyRef).
			Scan(&existing.SelectorKind, &source, &existing.SourceInstance, &existing.OrgID, &existing.ProjectID,
				&existing.AccountAuthority, &existing.AccountID, &existing.ContentKind)
		if err != nil {
			return 0, err
		}
		existing.SourceSystemID = source.String
		if existing.SelectorKind != p.SelectorKind || existing.SourceSystemID != p.SourceSystemID ||
			existing.SourceInstance != p.SourceInstance || existing.OrgID != p.OrgID || existing.ProjectID != p.ProjectID ||
			existing.AccountAuthority != p.AccountAuthority || existing.AccountID != p.AccountID || existing.ContentKind != p.ContentKind {
			return 0, reviewbody.ErrConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE review_capture_exceptions SET desired_revision=desired_revision+1,
 desired_mode=$2,desired_retention_seconds=$3,desired_max_item_bytes=$4,provenance=$5,requested_at=$6,
 updated_at=clock_timestamp() WHERE policy_ref=$1`, p.PolicyRef, p.Mode, p.RetentionSeconds,
			p.MaxItemBytes, p.Provenance, now)
	}
	if err != nil {
		return 0, err
	}
	if err = insertAccessAudit(ctx, tx, "human", operator, "review.policy.exception.request", "review_capture_exception", p.PolicyRef, "accepted", now); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return current + 1, nil
}

func (s *SourceFactsStore) ConfirmReviewCaptureException(ctx context.Context, ref string, desiredRevision int64,
	operator string, now time.Time) error {
	if s == nil || s.db == nil || !reviewExceptionRef.MatchString(ref) || desiredRevision < 1 || operator == "" || now.IsZero() {
		return reviewbody.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = reviewCaptureOwnerTx(ctx, tx); err != nil {
		return err
	}
	// Every upload locks the default row before selecting an effective exception.
	// Take the same gate exclusively before activating an exception, including
	// its first activation when no matching row was visible to that upload.
	var defaultRevision int64
	if err = tx.QueryRowContext(ctx, `SELECT effective_revision FROM review_capture_defaults WHERE singleton FOR UPDATE`).Scan(&defaultRevision); err != nil {
		return err
	}
	var desired, effective int64
	var selector ReviewCaptureException
	var source sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT desired_revision,effective_revision,selector_kind,
 source_system_id::text,source_instance,org_id,project_id,account_authority,account_id,content_kind
 FROM review_capture_exceptions WHERE policy_ref=$1 FOR UPDATE`, ref).Scan(&desired, &effective,
		&selector.SelectorKind, &source, &selector.SourceInstance, &selector.OrgID, &selector.ProjectID,
		&selector.AccountAuthority, &selector.AccountID, &selector.ContentKind); err != nil {
		return err
	}
	if desired != desiredRevision || effective >= desiredRevision {
		return reviewbody.ErrConflict
	}
	selector.SourceSystemID = source.String
	if err = verifiedReviewExceptionSelectorTx(ctx, tx, selector); err != nil {
		return err
	}
	now = now.UTC().Truncate(time.Microsecond)
	if _, err = tx.ExecContext(ctx, `UPDATE review_capture_exceptions SET effective_revision=desired_revision,
 effective_mode=desired_mode,effective_retention_seconds=desired_retention_seconds,
 effective_max_item_bytes=desired_max_item_bytes,effective_at=$2,updated_at=clock_timestamp()
 WHERE policy_ref=$1`, ref, now); err != nil {
		return err
	}
	if err = insertAccessAudit(ctx, tx, "human", operator, "review.policy.exception.confirm", "review_capture_exception", ref, "accepted", now); err != nil {
		return err
	}
	return tx.Commit()
}

// A binding is immutable. A second verified source may join the same XI
// project only by an explicit owner operation, never by matching an ID/name.
func (s *SourceFactsStore) BindReviewCaptureProject(ctx context.Context, sourceID, instance, orgID, projectID,
	xiProjectID, displayName, operator string, now time.Time) error {
	if s == nil || s.db == nil || !workEventUUID.MatchString(sourceID) || instance == "" ||
		!workEventGeneration.MatchString(orgID) || !workEventGeneration.MatchString(projectID) ||
		!workEventUUID.MatchString(xiProjectID) || displayName == "" || operator == "" || now.IsZero() {
		return reviewbody.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = reviewCaptureOwnerTx(ctx, tx); err != nil {
		return err
	}
	if err = sourceLock(ctx, tx, sourceID); err != nil {
		return err
	}
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM source_systems WHERE id=$1::uuid FOR SHARE`, sourceID).Scan(&status); err != nil {
		return err
	}
	if status != "active" {
		return reviewbody.ErrScope
	}
	// Mapping an active source-namespaced ledger would split one quota.
	var live bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM review_body_project_usage WHERE
 quota_scope=$1 AND (stored_bytes>0 OR reserved_bytes>0))`,
		reviewBodySourceQuotaScope(sourceID, instance, orgID, projectID)).Scan(&live); err != nil {
		return err
	}
	if live {
		return reviewbody.ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO review_capture_projects(xi_project_id,display_name) VALUES($1::uuid,$2)
 ON CONFLICT(xi_project_id) DO NOTHING`, xiProjectID, displayName); err != nil {
		return err
	}
	var storedName string
	if err = tx.QueryRowContext(ctx, `SELECT display_name FROM review_capture_projects WHERE xi_project_id=$1::uuid FOR SHARE`, xiProjectID).Scan(&storedName); err != nil {
		return err
	}
	if storedName != displayName {
		return reviewbody.ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO review_capture_project_bindings(source_system_id,source_instance,org_id,
 project_id,xi_project_id,revision) VALUES($1::uuid,$2,$3,$4,$5::uuid,1) ON CONFLICT DO NOTHING`,
		sourceID, instance, orgID, projectID, xiProjectID); err != nil {
		return err
	}
	var storedProject string
	if err = tx.QueryRowContext(ctx, `SELECT xi_project_id::text FROM review_capture_project_bindings WHERE
 source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 FOR SHARE`,
		sourceID, instance, orgID, projectID).Scan(&storedProject); err != nil {
		return err
	}
	if storedProject != xiProjectID {
		return reviewbody.ErrConflict
	}
	if err = insertAccessAudit(ctx, tx, "human", operator, "review.policy.project.bind", "review_capture_project", xiProjectID, "accepted", now.UTC()); err != nil {
		return err
	}
	return tx.Commit()
}
