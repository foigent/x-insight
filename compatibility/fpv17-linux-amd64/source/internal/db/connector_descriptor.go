package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/checkpointepoch"
	"github.com/foigent/x-insight/internal/connectorcontrol"
)

func (s *WorkspaceStore) SaveManagedSourceDescriptor(ctx context.Context, v connectorcontrol.SourceDescriptorV1, now time.Time) error {
	if s == nil || s.db == nil || now.IsZero() || connectorcontrol.ValidateSourceDescriptor(v) != nil {
		return connectorcontrol.ErrInvalid
	}
	if err := s.requireConnectorDeployment(ctx, v.SourceBinding.InstallationID); err != nil {
		return err
	}

	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if e != nil {
		return e
	}
	defer tx.Rollback()
	i, e := lockedManagedInstallation(ctx, tx, v.SourceBinding.InstallationID)
	if e != nil {
		return e
	}
	identityIssue, e := managedControlIdentityIssue(ctx, tx, i.ControlServiceIdentityID, now)
	if e != nil || identityIssue != "" {
		return connectorcontrol.ErrConflict
	}
	b := connectorcontrol.ManagedBoundary{Installation: i}
	var bootstrap, prior []byte
	var priorSHA sql.NullString
	e = tx.QueryRowContext(ctx, `SELECT b.source_binding_sha256,b.bootstrap,b.descriptor,b.descriptor_sha256,inv.unirouter_install_origin FROM connector_installation_source_bindings b JOIN connector_installation_invitations inv ON inv.installation_id=b.installation_id AND inv.status='consumed' AND inv.acknowledgement_sha256=$2 WHERE b.installation_id=$1 FOR UPDATE OF b`, i.InstallationID, i.InstallationAcknowledgementSHA256).Scan(&b.SourceBindingSHA256, &bootstrap, &prior, &priorSHA, &b.UniRouterOrigin)
	if e != nil || len(bootstrap) > 0 {
		return connectorcontrol.ErrConflict
	}
	resume, result, e := loadCompletedControlPredecessor(ctx, tx, i.InstallationID, v.ResumeOperationID, v.ResumeResultSHA256)
	if e != nil {
		return e
	}
	b.Phases = append(b.Phases, connectorcontrol.ActivationPhase{Operation: resume, Result: &result})
	for n := 0; n < 2; n++ {
		current := b.Phases[len(b.Phases)-1].Operation
		op, result, e := loadCompletedControlPredecessor(ctx, tx, i.InstallationID, current.PredecessorOperationID, current.PredecessorResultSHA256)
		if e != nil {
			return e
		}
		b.Phases = append(b.Phases, connectorcontrol.ActivationPhase{Operation: op, Result: &result})
	}
	if connectorcontrol.ValidateSourceDescriptorProof(b, v) != nil {
		return connectorcontrol.ErrConflict
	}
	if e = qualifyDescriptorCheckpoint(ctx, tx, b); e != nil {
		return e
	}
	h, raw, e := connectorcontrol.CanonicalSHA256(v)
	if e != nil {
		return e
	}
	if len(prior) > 0 {
		old, e := connectorcontrol.DecodeSourceDescriptor(prior)
		if e != nil || !priorSHA.Valid || priorSHA.String != h || !connectorcontrol.MatchesCanonicalDigest(old, h) || old != v {
			return connectorcontrol.ErrConflict
		}
		return tx.Commit()
	}
	_, e = tx.ExecContext(ctx, `UPDATE connector_installation_source_bindings SET descriptor=$2::jsonb,descriptor_sha256=$3 WHERE installation_id=$1 AND descriptor IS NULL AND bootstrap IS NULL`, i.InstallationID, string(raw), h)
	if e != nil {
		return e
	}
	return tx.Commit()
}

func qualifyDescriptorCheckpoint(ctx context.Context, tx *sql.Tx, b connectorcontrol.ManagedBoundary) error {
	register := b.Phase(connectorcontrol.ControlPhaseRegisterReceiver, "")
	prepare := b.Phase(connectorcontrol.ControlPhasePreparePausedSource, "")
	if register == nil || register.Operation.Request == nil || prepare == nil || prepare.Result == nil || prepare.Result.Result == nil {
		return connectorcontrol.ErrConflict
	}
	var id string
	if e := tx.QueryRowContext(ctx, `SELECT id::text FROM adapter_import_states WHERE source_system_id=$1::uuid AND adapter_manifest_id=$2::uuid AND cursor_key=$3`, unirouter.DefaultSourceSystemID, unirouter.DefaultCompletedExecutionAdapterManifestID, connectorcontrol.ControlCursorKey).Scan(&id); e != nil {
		return e
	}
	k, e := readManagedCheckpoint(ctx, tx, id)
	if e != nil {
		return e
	}
	if k.Instance != b.Installation.SourceInstance || k.Epoch != register.Operation.Request.ConsumerEpoch {
		return connectorcontrol.ErrConflict
	}
	err := checkpointepoch.VerifyInstallationCheckpointAuthority(ctx, tx, checkpointepoch.InstallationEpochInput{
		Binding:        checkpointepoch.Binding{SourceSystemID: unirouter.DefaultSourceSystemID, AdapterManifestID: unirouter.DefaultCompletedExecutionAdapterManifestID, CursorKey: connectorcontrol.ControlCursorKey},
		InstallationID: b.Installation.InstallationID, WorkerUnit: checkpointepoch.CompletedExecutionWorkerUnit,
		SourceInstance: b.Installation.SourceInstance, Cursor: prepare.Result.Result.Cursor, HighWatermark: prepare.Result.Result.HighWatermark,
	}, register.Operation.Request.ConsumerEpoch, register.Operation.Request.CheckpointAuthoritySHA256)
	if err != nil {
		return fmt.Errorf("original registration checkpoint authority: %w: %v", connectorcontrol.ErrConflict, err)
	}
	return nil
}
