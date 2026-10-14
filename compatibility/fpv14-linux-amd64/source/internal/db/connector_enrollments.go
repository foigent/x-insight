package db

import (
	"context"
	"database/sql"
	"errors"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"regexp"
	"time"
)

type ManagedEnrollment struct {
	InstallationID, EnrollmentID, SourceInstance, OrgID, ProjectID, Revision, ConfigurationSHA256, ContentCollectionID, Status string
	LegacyCCopyPending                                                                                                         bool
}

func (s *WorkspaceStore) ManagedEnrollment(ctx context.Context, installation, enrollment, project string) (ManagedEnrollment, error) {
	var r ManagedEnrollment
	e := s.db.QueryRowContext(ctx, `SELECT installation_id,enrollment_id,source_instance,org_id,project_id,revision,COALESCE(configuration_sha256,''),COALESCE(content_collection_id::text,''),status,legacy_c_copy_pending FROM connector_source_enrollments WHERE installation_id=$1 AND enrollment_id=$2 AND project_id=$3`, installation, enrollment, project).Scan(&r.InstallationID, &r.EnrollmentID, &r.SourceInstance, &r.OrgID, &r.ProjectID, &r.Revision, &r.ConfigurationSHA256, &r.ContentCollectionID, &r.Status, &r.LegacyCCopyPending)
	if errors.Is(e, sql.ErrNoRows) {
		e = connectorcontrol.ErrNotFound
	}
	return r, e
}
func (s *WorkspaceStore) SaveManagedEnrollment(ctx context.Context, r ManagedEnrollment, now time.Time) error {
	positive := regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	if s == nil || s.db == nil || !connectorcontrol.IsInstallationID(r.InstallationID) || !regexp.MustCompile(`^xise_[1-9][0-9]{0,18}$`).MatchString(r.EnrollmentID) || !positive.MatchString(r.OrgID) || !positive.MatchString(r.ProjectID) || !positive.MatchString(r.Revision) || r.SourceInstance == "" || now.IsZero() || (r.ConfigurationSHA256 != "" && !connectorcontrol.IsSHA256Reference(r.ConfigurationSHA256)) || (r.Status != "pending" && r.Status != "active" && r.Status != "revoked" && r.Status != "requires_migration") {
		return connectorcontrol.ErrInvalid
	}
	if r.LegacyCCopyPending && (r.ContentCollectionID == "" || r.Status != "requires_migration" && r.Status != "revoked") {
		return connectorcontrol.ErrInvalid
	}
	if err := s.requireConnectorDeployment(ctx, r.InstallationID); err != nil {
		return err
	}

	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if e != nil {
		return e
	}
	defer tx.Rollback()
	i, e := lockedManagedInstallation(ctx, tx, r.InstallationID)
	if e != nil {
		return e
	}
	if i.Status != "connected" || i.DesiredState != "active" || i.SourceInstance != r.SourceInstance {
		return connectorcontrol.ErrConflict
	}
	result, e := tx.ExecContext(ctx, `INSERT INTO connector_source_enrollments(installation_id,enrollment_id,source_instance,org_id,project_id,revision,configuration_sha256,content_collection_id,status,updated_at,legacy_c_copy_pending)
 VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,'')::uuid,$9,$10,$11)
 ON CONFLICT(installation_id,enrollment_id,project_id) DO UPDATE SET revision=excluded.revision,
 configuration_sha256=COALESCE(excluded.configuration_sha256,connector_source_enrollments.configuration_sha256),
 content_collection_id=COALESCE(excluded.content_collection_id,connector_source_enrollments.content_collection_id),status=excluded.status,updated_at=excluded.updated_at,
 legacy_c_copy_pending=connector_source_enrollments.legacy_c_copy_pending OR excluded.legacy_c_copy_pending
 WHERE connector_source_enrollments.source_instance=excluded.source_instance AND connector_source_enrollments.org_id=excluded.org_id AND connector_source_enrollments.project_id=excluded.project_id
 AND connector_source_enrollments.revision::numeric<=excluded.revision::numeric
 AND (NOT excluded.legacy_c_copy_pending OR connector_source_enrollments.legacy_c_copy_pending OR connector_source_enrollments.content_collection_id IS NULL)
 AND (connector_source_enrollments.content_collection_id IS NULL OR excluded.content_collection_id IS NULL OR connector_source_enrollments.content_collection_id=excluded.content_collection_id)
 AND (connector_source_enrollments.status<>'revoked' OR connector_source_enrollments.status=excluded.status)`, r.InstallationID, r.EnrollmentID, r.SourceInstance, r.OrgID, r.ProjectID, r.Revision, r.ConfigurationSHA256, r.ContentCollectionID, r.Status, now.UTC(), r.LegacyCCopyPending)
	if e != nil {
		return e
	}
	if e = requireOneConnectorRow(result); e != nil {
		return e
	}
	return tx.Commit()
}
