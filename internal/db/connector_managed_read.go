package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"time"
)

func (s *WorkspaceStore) ListManagedConnectorIDs(ctx context.Context, limit int) ([]string, error) {
	if s == nil || s.db == nil || limit < 1 || limit > 100 {
		return nil, connectorcontrol.ErrInvalid
	}
	rows, e := s.db.QueryContext(ctx, `SELECT c.id FROM connector_installations c JOIN connector_installation_source_bindings b ON b.installation_id=c.id WHERE ($2='' OR c.deployment_id=$2) AND c.desired_state='active' AND c.status NOT IN ('paused','disconnected') ORDER BY c.created_at,c.id LIMIT $1`, limit, s.connectorDeploymentID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (s *WorkspaceStore) InspectManagedConnector(ctx context.Context, id string) (connectorcontrol.ManagedBoundary, error) {
	var b connectorcontrol.ManagedBoundary
	if s == nil || s.db == nil || !connectorcontrol.IsInstallationID(id) {
		return b, connectorcontrol.ErrInvalid
	}
	if err := s.requireConnectorDeployment(ctx, id); err != nil {
		return b, err
	}

	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if e != nil {
		return b, e
	}
	defer tx.Rollback()
	if e = requireConnectorActivationAuthority(ctx, tx); e != nil {
		return b, e
	}
	b.Installation, e = loadConnectorInstallation(ctx, tx, id)
	if e != nil {
		return b, e
	}
	var bootstrap []byte
	var descriptor []byte
	var descriptorSHA sql.NullString
	e = tx.QueryRowContext(ctx, `SELECT b.source_binding_sha256,b.bootstrap,b.descriptor,b.descriptor_sha256,i.unirouter_install_origin FROM connector_installation_source_bindings b
 JOIN connector_installation_invitations i ON i.installation_id=b.installation_id AND i.status='consumed'
 AND i.acknowledgement_sha256=$2 WHERE b.installation_id=$1`, id, b.Installation.InstallationAcknowledgementSHA256).Scan(&b.SourceBindingSHA256, &bootstrap, &descriptor, &descriptorSHA, &b.UniRouterOrigin)
	if errors.Is(e, sql.ErrNoRows) {
		return b, tx.Commit()
	}
	if e != nil {
		return b, e
	}
	identityIssue, e := managedControlIdentityIssue(ctx, tx, b.Installation.ControlServiceIdentityID, time.Now().UTC())
	if e != nil {
		return b, e
	}
	if identityIssue != "" {
		return b, fmt.Errorf("managed control identity is inactive, expired or lacks poll/ack: %w", connectorcontrol.ErrConflict)
	}
	if len(bootstrap) > 0 {
		v, e := connectorcontrol.BootstrapFromJSON(bootstrap)
		if e != nil {
			return b, e
		}
		b.Bootstrap = &v
	}
	rows, e := tx.QueryContext(ctx, `SELECT operation_id FROM connector_control_operations WHERE installation_id=$1 ORDER BY created_at,operation_id`, id)
	if e != nil {
		return b, e
	}
	ids := []string{}
	for rows.Next() {
		var op string
		if e = rows.Scan(&op); e != nil {
			rows.Close()
			return b, e
		}
		ids = append(ids, op)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return b, e
	}
	for _, op := range ids {
		o, e := loadControlOperation(ctx, tx, op)
		if e != nil {
			return b, fmt.Errorf("read managed phase authority: %w", e)
		}
		p := connectorcontrol.ActivationPhase{Operation: o}
		if o.Status == "completed" || o.Status == "failed" {
			r, e := loadTerminalControlOperationResult(ctx, tx, o)
			if e != nil {
				return b, fmt.Errorf("read terminal managed phase %s: %w", o.Phase, e)
			}
			p.Result = &r
		}
		b.Phases = append(b.Phases, p)
	}
	if len(descriptor) > 0 {
		v, err := connectorcontrol.DecodeSourceDescriptor(descriptor)
		if err != nil || !descriptorSHA.Valid || !connectorcontrol.MatchesCanonicalDigest(v, descriptorSHA.String) || connectorcontrol.ValidateSourceDescriptorProof(b, v) != nil {
			return b, fmt.Errorf("qualify stored source descriptor: %w", connectorcontrol.ErrConflict)
		}
		b.Descriptor = &v
	}
	if b.Bootstrap == nil && b.Installation.Status == "connected" {
		if _, _, e = connectorcontrol.OriginalReceiverProof(b); e != nil {
			return b, fmt.Errorf("qualify original receiver proof: %w", e)
		}
		if e = qualifyDescriptorCheckpoint(ctx, tx, b); e != nil {
			return b, fmt.Errorf("qualify original descriptor checkpoint: %w", e)
		}
	}
	var receipt []byte
	e = tx.QueryRowContext(ctx, `SELECT local_receipt FROM connector_receiver_adoptions WHERE installation_id=$1`, id).Scan(&receipt)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return b, e
	}
	if len(receipt) > 0 {
		var a connectorcontrol.XIAdoptionReceipt
		if json.Unmarshal(receipt, &a) != nil {
			return b, connectorcontrol.ErrConflict
		}
		b.LocalReceipt = &a
	}
	if b.Bootstrap != nil {
		var stateID string
		e = tx.QueryRowContext(ctx, `SELECT id::text FROM adapter_import_states WHERE source_system_id=$1::uuid AND adapter_manifest_id=$2::uuid AND cursor_key=$3`, unirouter.DefaultSourceSystemID, unirouter.DefaultCompletedExecutionAdapterManifestID, connectorcontrol.ControlCursorKey).Scan(&stateID)
		if e != nil {
			return b, e
		}
		k, e := readManagedCheckpoint(ctx, tx, stateID)
		if e != nil {
			return b, e
		}
		b.Checkpoint = &connectorcontrol.ExistingCheckpoint{StateID: k.ID, ConsumerEpoch: k.Epoch, EpochOperationID: k.EpochOperation, AuthorizationRefSHA256: connectorcontrol.RawSHA256(k.Authority), CursorSHA256: connectorcontrol.RawSHA256(k.Cursor), HighWatermarkSHA256: connectorcontrol.RawSHA256(k.HighWatermark)}
		if b.LocalReceipt != nil && k.Status == "idle" && k.Started.Valid && k.Completed.Valid && k.Started.Time.After(b.LocalReceipt.CommittedAt) && k.Pages > b.LocalReceipt.PagesCommittedBefore {
			bind := b.Phase(connectorcontrol.PhaseBindExistingReceiver, "")
			if bind != nil && bind.Operation.RequestV3 != nil && bind.Operation.RequestV3.Candidate != nil {
				c := bind.Operation.RequestV3.Candidate
				if checkpointMatches(k, *c) {
					wh, _, _ := connectorcontrol.CanonicalSHA256(c.WorkerConfig)
					b.FeedRead = &connectorcontrol.FeedReadReceipt{FeedKeyID: c.FeedKeyID, ReceiverRegistrationSHA256: c.WorkerConfig.ReceiverRegistrationSHA256, WorkerConfigSHA256: wh, CheckpointStateID: k.ID, ConsumerEpoch: k.Epoch, EpochOperationID: k.EpochOperation, StartedAt: k.Started.Time.UTC(), CompletedAt: k.Completed.Time.UTC(), PagesCommitted: k.Pages, CursorSHA256: connectorcontrol.RawSHA256(k.Cursor), HighWatermarkSHA256: connectorcontrol.RawSHA256(k.HighWatermark), ResponseStatus: 200}
				}
			}
		}
	}
	return b, tx.Commit()
}
