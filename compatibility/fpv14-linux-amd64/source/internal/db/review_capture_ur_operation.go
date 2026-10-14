package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/foigent/x-insight/internal/reviewbody"
)

var urPolicyOperationRef = regexp.MustCompile(`^xipw_[0-9a-f]{32}$`)
var urPolicyPositiveID = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
var urPolicyUserScopeID = regexp.MustCompile(`^[1-9][0-9]{0,18}:[1-9][0-9]{0,18}$`)
var urPolicyDigest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type ReviewCaptureUROperation struct {
	InstallationID    string          `json:"installation_id"`
	OperationRef      string          `json:"operation_ref"`
	SourceInstance    string          `json:"source_instance"`
	OrgID             string          `json:"org_id"`
	ScopeKind         string          `json:"scope_kind"`
	ScopeID           string          `json:"scope_id"`
	OperatorSubject   string          `json:"operator_subject"`
	RequestSHA256     string          `json:"request_sha256"`
	XIDesiredRevision int64           `json:"xi_desired_revision"`
	URExpectedRef     string          `json:"ur_expected_ref"`
	URExpectedVersion int64           `json:"ur_expected_version"`
	URExpectedVector  json.RawMessage `json:"ur_expected_vector"`
	State             string          `json:"state"`
	URResult          json.RawMessage `json:"ur_result,omitempty"`
	ReadbackAt        *time.Time      `json:"readback_at,omitempty"`
}

func validUROperation(op ReviewCaptureUROperation) bool {
	return op.InstallationID != "" && len(op.InstallationID) <= 128 && urPolicyOperationRef.MatchString(op.OperationRef) &&
		op.SourceInstance != "" && len(op.SourceInstance) <= 128 && urPolicyPositiveID.MatchString(op.OrgID) &&
		(op.ScopeKind == "enterprise" && op.ScopeID == op.OrgID ||
			op.ScopeKind == "project" && urPolicyPositiveID.MatchString(op.ScopeID) ||
			op.ScopeKind == "user" && urPolicyUserScopeID.MatchString(op.ScopeID)) &&
		op.OperatorSubject != "" && len(op.OperatorSubject) <= 128 && urPolicyDigest.MatchString(op.RequestSHA256) && op.XIDesiredRevision > 0 &&
		op.URExpectedRef != "" && len(op.URExpectedRef) <= 256 && op.URExpectedVersion >= 0 &&
		len(op.URExpectedVector) > 0 && json.Valid(op.URExpectedVector) && op.State == "requested"
}

func (s *SourceFactsStore) ValidateReviewCaptureURInstallation(ctx context.Context, installationID, sourceInstance string) error {
	if s == nil || s.db == nil || installationID == "" || sourceInstance == "" {
		return reviewbody.ErrInvalid
	}
	var paired bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM connector_installations
 WHERE id=$1 AND source_instance=$2 AND connector_type='unirouter' AND
 installation_acknowledgement_sha256 IS NOT NULL)`, installationID, sourceInstance).Scan(&paired); err != nil {
		return err
	}
	if !paired {
		return reviewbody.ErrScope
	}
	return nil
}

// The operation is durable before a delegated UR write starts. A transport
// failure can therefore be recovered by the same immutable operation ref.
func (s *SourceFactsStore) CreateReviewCaptureUROperation(ctx context.Context, op ReviewCaptureUROperation) error {
	if s == nil || s.db == nil || !validUROperation(op) {
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
	var pairedSource sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT source_instance FROM connector_installations
 WHERE id=$1 AND connector_type='unirouter' AND installation_acknowledgement_sha256 IS NOT NULL FOR SHARE`,
		op.InstallationID).Scan(&pairedSource)
	if errors.Is(err, sql.ErrNoRows) || err == nil && pairedSource.String != op.SourceInstance {
		return reviewbody.ErrScope
	}
	if err != nil {
		return err
	}
	var current int64
	if err = tx.QueryRowContext(ctx, `SELECT desired_revision FROM review_capture_defaults WHERE singleton FOR SHARE`).Scan(&current); err != nil {
		return err
	}
	if current != op.XIDesiredRevision {
		return reviewbody.ErrConflict
	}
	if err = insertReviewCaptureUROperationTx(ctx, tx, op); err != nil {
		return err
	}
	if err = insertAccessAudit(ctx, tx, "human", op.OperatorSubject, "review.policy.ur.request",
		"review_capture_ur_operation", op.OperationRef, "accepted", time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit()
}

func insertReviewCaptureUROperationTx(ctx context.Context, tx *sql.Tx, op ReviewCaptureUROperation) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO review_capture_ur_operations(installation_id,operation_ref,source_instance,
 org_id,scope_kind,scope_id,operator_subject,request_sha256,xi_desired_revision,ur_expected_ref,ur_expected_version,
 ur_expected_vector,state) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,'requested')`,
		op.InstallationID, op.OperationRef, op.SourceInstance, op.OrgID, op.ScopeKind, op.ScopeID,
		op.OperatorSubject, op.RequestSHA256, op.XIDesiredRevision, op.URExpectedRef, op.URExpectedVersion, op.URExpectedVector)
	return err
}

// A combined save records XI desired intent and the immutable UR operation
// identity in one owner transaction. XI effective settings remain unchanged
// until the administrator confirms the exact desired revision after UR readback.
func (s *SourceFactsStore) CreateReviewCaptureJointUROperation(ctx context.Context, op ReviewCaptureUROperation,
	settings ReviewCaptureSettings, expectedRevision int64, now time.Time) error {
	if s == nil || s.db == nil || !validUROperation(op) || !validReviewCaptureSettings(settings) ||
		op.ScopeKind != "enterprise" || expectedRevision < 0 || op.XIDesiredRevision != expectedRevision+1 || now.IsZero() {
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
	var pairedSource string
	if err = tx.QueryRowContext(ctx, `SELECT source_instance FROM connector_installations
 WHERE id=$1 AND connector_type='unirouter' AND installation_acknowledgement_sha256 IS NOT NULL FOR SHARE`,
		op.InstallationID).Scan(&pairedSource); err != nil {
		return err
	}
	if pairedSource != op.SourceInstance {
		return reviewbody.ErrScope
	}
	var current int64
	err = tx.QueryRowContext(ctx, `SELECT desired_revision FROM review_capture_defaults WHERE singleton FOR UPDATE`).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		current = 0
	} else if err != nil {
		return err
	}
	if current != expectedRevision {
		return reviewbody.ErrConflict
	}
	now = now.UTC().Truncate(time.Microsecond)
	if current == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO review_capture_defaults(singleton,desired_revision,desired_mode,
 desired_retention_seconds,desired_project_quota_bytes,desired_max_item_bytes,requested_at)
 VALUES(true,1,$1,$2,$3,$4,$5)`, settings.Mode, settings.RetentionSeconds,
			settings.ProjectQuotaBytes, settings.MaxItemBytes, now)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE review_capture_defaults SET desired_revision=desired_revision+1,
 desired_mode=$1,desired_retention_seconds=$2,desired_project_quota_bytes=$3,desired_max_item_bytes=$4,
 requested_at=$5,updated_at=clock_timestamp() WHERE singleton`, settings.Mode, settings.RetentionSeconds,
			settings.ProjectQuotaBytes, settings.MaxItemBytes, now)
	}
	if err != nil {
		return err
	}
	if err = insertReviewCaptureUROperationTx(ctx, tx, op); err != nil {
		return err
	}
	if err = insertAccessAudit(ctx, tx, "human", op.OperatorSubject, "review.policy.request",
		"review_capture_default", "xi-tenant", "accepted", now); err != nil {
		return err
	}
	if err = insertAccessAudit(ctx, tx, "human", op.OperatorSubject, "review.policy.ur.request",
		"review_capture_ur_operation", op.OperationRef, "accepted", now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SourceFactsStore) CurrentReviewCaptureDesiredRevisionAny(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil {
		return 0, reviewbody.ErrInvalid
	}
	var revision int64
	err := s.db.QueryRowContext(ctx, `SELECT desired_revision FROM review_capture_defaults WHERE singleton`).Scan(&revision)
	return revision, err
}

func (s *SourceFactsStore) ReadReviewCaptureUROperation(ctx context.Context, installationID, operationRef string) (ReviewCaptureUROperation, error) {
	var op ReviewCaptureUROperation
	if s == nil || s.db == nil || installationID == "" || !urPolicyOperationRef.MatchString(operationRef) {
		return op, reviewbody.ErrInvalid
	}
	var readback sql.NullTime
	var result []byte
	err := s.db.QueryRowContext(ctx, `SELECT installation_id,operation_ref,source_instance,org_id,scope_kind,scope_id,
 operator_subject,request_sha256,xi_desired_revision,ur_expected_ref,ur_expected_version,ur_expected_vector,state,ur_result,readback_at
 FROM review_capture_ur_operations WHERE installation_id=$1 AND operation_ref=$2`, installationID, operationRef).
		Scan(&op.InstallationID, &op.OperationRef, &op.SourceInstance, &op.OrgID, &op.ScopeKind, &op.ScopeID,
			&op.OperatorSubject, &op.RequestSHA256, &op.XIDesiredRevision, &op.URExpectedRef, &op.URExpectedVersion,
			&op.URExpectedVector, &op.State, &result, &readback)
	op.URResult = json.RawMessage(result)
	if readback.Valid {
		op.ReadbackAt = &readback.Time
	}
	return op, err
}

// This is historical management evidence, not proof of current UR state.
func (s *SourceFactsStore) ReadLatestReviewCaptureUROperation(ctx context.Context, installationID, scopeKind, scopeID string) (ReviewCaptureUROperation, error) {
	var op ReviewCaptureUROperation
	if s == nil || s.db == nil || installationID == "" ||
		(scopeKind != "enterprise" && scopeKind != "project" && scopeKind != "user") || scopeID == "" {
		return op, reviewbody.ErrInvalid
	}
	var readback sql.NullTime
	var result []byte
	err := s.db.QueryRowContext(ctx, `SELECT installation_id,operation_ref,source_instance,org_id,scope_kind,scope_id,
 operator_subject,request_sha256,xi_desired_revision,ur_expected_ref,ur_expected_version,ur_expected_vector,state,ur_result,readback_at
 FROM review_capture_ur_operations WHERE installation_id=$1 AND scope_kind=$2 AND scope_id=$3
 ORDER BY created_at DESC,operation_ref DESC LIMIT 1`, installationID, scopeKind, scopeID).Scan(
		&op.InstallationID, &op.OperationRef, &op.SourceInstance, &op.OrgID, &op.ScopeKind, &op.ScopeID,
		&op.OperatorSubject, &op.RequestSHA256, &op.XIDesiredRevision, &op.URExpectedRef, &op.URExpectedVersion,
		&op.URExpectedVector, &op.State, &result, &readback)
	op.URResult = json.RawMessage(result)
	if readback.Valid {
		op.ReadbackAt = &readback.Time
	}
	return op, err
}

func (s *SourceFactsStore) RecordReviewCaptureUROperation(ctx context.Context, installationID, operationRef, state string,
	result json.RawMessage, now time.Time) error {
	if s == nil || s.db == nil || installationID == "" || !urPolicyOperationRef.MatchString(operationRef) ||
		state != "unconfirmed" && state != "pending" && state != "effective" && state != "failed" && state != "drift" ||
		len(result) > 16<<10 || len(result) > 0 && !json.Valid(result) || now.IsZero() {
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
	var previous, actor string
	if err = tx.QueryRowContext(ctx, `SELECT state,operator_subject FROM review_capture_ur_operations
 WHERE installation_id=$1 AND operation_ref=$2 FOR UPDATE`, installationID, operationRef).Scan(&previous, &actor); err != nil {
		return err
	}
	if previous == "effective" && state != "effective" || previous == "failed" && state != "failed" ||
		previous == "drift" && state != "drift" {
		return reviewbody.ErrConflict
	}
	if len(result) == 0 {
		_, err = tx.ExecContext(ctx, `UPDATE review_capture_ur_operations SET state=$3,readback_at=$4
 WHERE installation_id=$1 AND operation_ref=$2`, installationID, operationRef, state, now.UTC())
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE review_capture_ur_operations SET state=$3,ur_result=$4::jsonb,readback_at=$5
 WHERE installation_id=$1 AND operation_ref=$2`, installationID, operationRef, state, result, now.UTC())
	}
	if err != nil {
		return err
	}
	if previous != state {
		// The action records the exact UR state. Audit result describes whether
		// that observed transition confirmed a successful delegated outcome.
		auditResult := "error"
		if state == "pending" || state == "effective" {
			auditResult = "accepted"
		}
		if err = insertAccessAudit(ctx, tx, "human", actor, "review.policy.ur."+state,
			"review_capture_ur_operation", operationRef, auditResult, now.UTC()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SourceFactsStore) CurrentReviewCaptureDesiredRevision(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil {
		return 0, reviewbody.ErrInvalid
	}
	var revision int64
	err := s.db.QueryRowContext(ctx, `SELECT desired_revision FROM review_capture_defaults
 WHERE singleton AND desired_revision=effective_revision AND effective_revision>0`).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, reviewbody.ErrInvalid
	}
	return revision, err
}
