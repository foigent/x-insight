package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/foigent/x-insight/internal/reviewbody"
)

// EnsureManagedReviewCaptureProject binds one verified, active managed C
// collection to its own XI project. The caller must have just qualified the
// exact project against the delegated UR enrollment; local state is rechecked
// here before the owner writes the immutable binding. Agent identities and
// source project labels are never used to select an XI project.
func (s *SourceFactsStore) EnsureManagedReviewCaptureProject(ctx context.Context, installationID, collectionID,
	sourceID, instance, orgID, projectID string, now time.Time) (string, error) {
	if s == nil || s.db == nil || installationID == "" || !workEventUUID.MatchString(collectionID) ||
		!workEventUUID.MatchString(sourceID) || instance == "" || !workEventGeneration.MatchString(orgID) ||
		!workEventGeneration.MatchString(projectID) || now.IsZero() {
		return "", reviewbody.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err = reviewCaptureOwnerTx(ctx, tx); err != nil {
		return "", err
	}
	if err = sourceLock(ctx, tx, sourceID); err != nil {
		return "", err
	}
	var installationOK bool
	err = tx.QueryRowContext(ctx, `SELECT connector_type='unirouter' AND status='connected' AND
 desired_state='active' AND source_instance=$2 FROM connector_installations WHERE id=$1 FOR SHARE`,
		installationID, instance).Scan(&installationOK)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !installationOK) {
		return "", reviewbody.ErrScope
	}
	if err != nil {
		return "", err
	}
	var collectionOK bool
	err = tx.QueryRowContext(ctx, `SELECT c.status='active' AND NOT c.requires_attention AND
 c.allowed_purposes='["human_review"]'::jsonb AND s.source_type='unirouter' AND s.status='active'
 FROM source_content_collections c JOIN source_systems s ON s.id=c.source_system_id
 WHERE c.id=$1::uuid AND c.source_system_id=$2::uuid AND c.source_instance=$3
 AND c.org_id=$4 AND c.project_id=$5 FOR SHARE OF c,s`,
		collectionID, sourceID, instance, orgID, projectID).Scan(&collectionOK)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !collectionOK) {
		return "", reviewbody.ErrScope
	}
	if err != nil {
		return "", err
	}
	var xiProjectID string
	err = tx.QueryRowContext(ctx, `SELECT xi_project_id::text FROM review_capture_project_bindings
 WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 FOR SHARE`,
		sourceID, instance, orgID, projectID).Scan(&xiProjectID)
	if err == nil {
		if err = tx.Commit(); err != nil {
			return "", err
		}
		return xiProjectID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if err = tx.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&xiProjectID); err != nil {
		return "", err
	}
	name := "UniRouter project " + orgID + "/" + projectID
	if err = bindReviewCaptureProjectTx(ctx, tx, sourceID, instance, orgID, projectID, xiProjectID, name); err != nil {
		return "", err
	}
	if err = insertAccessAudit(ctx, tx, "system", installationID, "review.policy.project.auto_bind",
		"review_capture_project", xiProjectID, "accepted", now.UTC()); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return xiProjectID, nil
}
