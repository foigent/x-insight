package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/connectorcontrol"
)

type managedCheckpoint struct {
	ID, Instance, Authority, EpochOperation, Cursor, HighWatermark, Status string
	Epoch, Pages                                                           int64
	Started, Completed                                                     sql.NullTime
}

func readManagedCheckpoint(ctx context.Context, tx *sql.Tx, id string) (managedCheckpoint, error) {
	var k managedCheckpoint
	err := tx.QueryRowContext(ctx, `SELECT state.id::text,state.activation_source_instance,state.authorization_ref,
 state.checkpoint_epoch,COALESCE(state.checkpoint_epoch_operation_id,''),COALESCE(state.cursor_value,''),COALESCE(state.high_watermark,''),
 state.worker_status,state.pages_committed,state.last_import_started_at,state.last_import_completed_at
 FROM adapter_import_states state JOIN source_systems source ON source.id=state.source_system_id
 JOIN adapter_manifests adapter ON adapter.id=state.adapter_manifest_id
 WHERE state.id=$1::uuid AND state.source_system_id=$2::uuid AND state.adapter_manifest_id=$3::uuid
 AND state.cursor_key=$4 AND source.status='active' AND adapter.active
 AND state.activated_at IS NOT NULL FOR UPDATE OF state`, id, unirouter.DefaultSourceSystemID,
		unirouter.DefaultCompletedExecutionAdapterManifestID, connectorcontrol.ControlCursorKey).Scan(&k.ID, &k.Instance, &k.Authority, &k.Epoch, &k.EpochOperation, &k.Cursor, &k.HighWatermark, &k.Status, &k.Pages, &k.Started, &k.Completed)
	return k, err
}
func checkpointMatches(k managedCheckpoint, c connectorcontrol.ExistingReceiverCandidate) bool {
	return k.ID == c.Checkpoint.StateID && k.Instance == c.SourceInstance && k.Epoch == c.Checkpoint.ConsumerEpoch && k.EpochOperation == c.Checkpoint.EpochOperationID && connectorcontrol.RawSHA256(k.Authority) == c.Checkpoint.AuthorizationRefSHA256
}
func lockedManagedInstallation(ctx context.Context, tx *sql.Tx, id string) (connectorcontrol.Installation, error) {
	if err := requireConnectorActivationAuthority(ctx, tx); err != nil {
		return connectorcontrol.Installation{}, err
	}
	var locked string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM connector_installations WHERE id=$1 FOR UPDATE`, id).Scan(&locked); err != nil {
		return connectorcontrol.Installation{}, err
	}
	return loadConnectorInstallation(ctx, tx, id)
}

// EnqueueManagedControlOperation is root-owned product authority. The original
// epoch row is inspected/locked, never rebound, reset or advanced here.
func (s *WorkspaceStore) EnqueueManagedControlOperation(ctx context.Context, r connectorcontrol.ControlOperationRequestV3, now time.Time) (connectorcontrol.ControlOperation, error) {
	if s == nil || s.db == nil || connectorcontrol.ValidateControlOperationRequestV3(r) != nil || now.IsZero() {
		return connectorcontrol.ControlOperation{}, connectorcontrol.ErrInvalid
	}
	if err := s.requireConnectorDeployment(ctx, r.InstallationID); err != nil {
		return connectorcontrol.ControlOperation{}, err
	}

	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if e != nil {
		return connectorcontrol.ControlOperation{}, e
	}
	defer tx.Rollback()
	i, e := lockedManagedInstallation(ctx, tx, r.InstallationID)
	if e != nil {
		return connectorcontrol.ControlOperation{}, e
	}
	if i.DesiredState != "active" || i.ProjectID != r.ProjectID || i.ControlServiceIdentityID == "" || i.InstallationAcknowledgementSHA256 == "" {
		return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
	}
	identityIssue, e := managedControlIdentityIssue(ctx, tx, i.ControlServiceIdentityID, now)
	if e != nil || identityIssue != "" {
		return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
	}
	sha, raw, e := connectorcontrol.CanonicalSHA256(r)
	if e != nil {
		return connectorcontrol.ControlOperation{}, e
	}
	var prior string
	e = tx.QueryRowContext(ctx, `SELECT operation_id FROM connector_control_operations WHERE installation_id=$1 AND
 (operation_id=$2 OR (phase=$3 AND phase<>'deliver_machine_candidate') OR
 (phase='deliver_machine_candidate' AND $3='deliver_machine_candidate' AND request_payload->'request'->'registration'->>'purpose'=$4)) FOR UPDATE`, r.InstallationID, r.OperationID, r.Phase, managedPurpose(r)).Scan(&prior)
	if e == nil {
		old, e := loadControlOperation(ctx, tx, prior)
		if e != nil || old.RequestSHA256 != sha {
			return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
		}
		return old, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return connectorcontrol.ControlOperation{}, e
	}
	if r.Phase == connectorcontrol.PhaseBindExistingReceiver {
		c := *r.Request.Candidate
		var sourceSHA string
		var bootstrapRaw []byte
		if e = tx.QueryRowContext(ctx, `SELECT source_binding_sha256,bootstrap FROM connector_installation_source_bindings WHERE installation_id=$1`, r.InstallationID).Scan(&sourceSHA, &bootstrapRaw); e != nil || sourceSHA != c.SourceBindingSHA256 {
			return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
		}
		bootstrap, e := connectorcontrol.BootstrapFromJSON(bootstrapRaw)
		if e != nil || bootstrap.SourceInstance != c.SourceInstance || bootstrap.FeedKeyID != c.FeedKeyID || bootstrap.AllowedIP != c.AllowedIP || bootstrap.KeyPrefix != c.ReceiverRegistration.KeyPrefix || bootstrap.RegistrationOperationID != c.ReceiverRegistration.OperationID {
			return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
		}
		k, e := readManagedCheckpoint(ctx, tx, c.Checkpoint.StateID)
		if e != nil || !checkpointMatches(k, c) {
			return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO connector_receiver_adoptions(installation_id,mode,candidate_sha256,checkpoint_state_id,consumer_epoch,epoch_operation_id,status,created_at,updated_at)
   VALUES($1,'adopt_existing',$2,$3::uuid,$4,$5,'selected',$6,$6)`, r.InstallationID, r.Request.CandidateSHA256, k.ID, k.Epoch, k.EpochOperation, now.UTC()); e != nil {
			return connectorcontrol.ControlOperation{}, e
		}
	} else {
		p, pr, e := loadCompletedControlPredecessor(ctx, tx, r.InstallationID, r.PredecessorOperationID, r.PredecessorResultSHA256)
		if e != nil {
			return connectorcontrol.ControlOperation{}, e
		}
		if p.CompletedAt == nil || now.Before(*p.CompletedAt) || (r.Phase == connectorcontrol.PhaseConfirmExistingReceiver && p.Phase != connectorcontrol.PhaseBindExistingReceiver) || (r.Phase == connectorcontrol.PhaseDeliverMachineCandidate && p.Phase != connectorcontrol.ControlPhaseResumeSource && p.Phase != connectorcontrol.PhaseConfirmExistingReceiver) {
			return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
		}
		if r.Phase == connectorcontrol.PhaseConfirmExistingReceiver {
			if pr.ResultV3 == nil || r.Request.AdoptionBindingSHA256 != pr.ResultV3.Result.AdoptionBindingSHA256 {
				return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
			}
			var saved []byte
			var savedSHA string
			if e = tx.QueryRowContext(ctx, `SELECT local_receipt,local_receipt_sha256 FROM connector_receiver_adoptions WHERE installation_id=$1 AND status='bound' FOR UPDATE`, r.InstallationID).Scan(&saved, &savedSHA); e != nil {
				return connectorcontrol.ControlOperation{}, e
			}
			var receipt connectorcontrol.XIAdoptionReceipt
			if json.Unmarshal(saved, &receipt) != nil || savedSHA != r.Request.XIAdoptionReceiptSHA256 || !reflect.DeepEqual(receipt, *r.Request.XIAdoptionReceipt) {
				return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
			}
			k, e := readManagedCheckpoint(ctx, tx, receipt.StateID)
			if e != nil || k.Epoch != receipt.ConsumerEpoch || k.EpochOperation != receipt.EpochOperationID {
				return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
			}
			candidate := p.RequestV3.Candidate
			if candidate == nil || !checkpointMatches(k, *candidate) {
				return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
			}
			f := r.Request.FeedReadReceipt
			workerSHA, _, _ := connectorcontrol.CanonicalSHA256(candidate.WorkerConfig)
			if f.FeedKeyID != candidate.FeedKeyID || f.ReceiverRegistrationSHA256 != candidate.WorkerConfig.ReceiverRegistrationSHA256 || f.WorkerConfigSHA256 != workerSHA {
				return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
			}
			if k.Status != "idle" || !k.Started.Valid || !k.Completed.Valid || !k.Started.Time.Equal(f.StartedAt) || !k.Completed.Time.Equal(f.CompletedAt) || k.Pages != f.PagesCommitted || connectorcontrol.RawSHA256(k.Cursor) != f.CursorSHA256 || connectorcontrol.RawSHA256(k.HighWatermark) != f.HighWatermarkSHA256 {
				return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
			}
		}
		if r.Kind == connectorcontrol.KindPairMachine {
			var savedSHA string
			var bootstrapRaw, descriptorRaw []byte
			if i.Status != "connected" || i.SourceInstance != r.Request.SourceInstance {
				return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
			}
			if e = tx.QueryRowContext(ctx, `SELECT source_binding_sha256,bootstrap,descriptor FROM connector_installation_source_bindings WHERE installation_id=$1`, i.InstallationID).Scan(&savedSHA, &bootstrapRaw, &descriptorRaw); e != nil || savedSHA != r.Request.SourceBindingSHA256 {
				return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
			}
			allowedIP := ""
			if len(bootstrapRaw) > 0 {
				v, e := connectorcontrol.BootstrapFromJSON(bootstrapRaw)
				if e != nil || p.Phase != connectorcontrol.PhaseConfirmExistingReceiver || v.SourceInstance != r.Request.SourceInstance {
					return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
				}
				allowedIP = v.AllowedIP
			} else {
				v, e := connectorcontrol.DecodeSourceDescriptor(descriptorRaw)
				if e != nil || p.Phase != connectorcontrol.ControlPhaseResumeSource || v.ResumeOperationID != p.OperationID || v.ResumeResultSHA256 != p.ResultSHA256 || v.SourceBindingSHA256 != savedSHA || v.SourceBinding.SourceInstance != r.Request.SourceInstance {
					return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
				}
				allowedIP = v.ReceiverNetwork.AllowedIP
			}
			if r.Request.Registration.AllowedIP != allowedIP {
				return connectorcontrol.ControlOperation{}, connectorcontrol.ErrConflict
			}
		}
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO connector_control_operations(operation_id,installation_id,operation_kind,phase,desired_state,predecessor_operation_id,predecessor_result_sha256,request_sha256,request_payload,status,available_at,created_at,updated_at)
 VALUES($1,$2,$3,$4,'active',NULLIF($5,''),NULLIF($6,''),$7,$8,'pending',$9,$9,$9)`, r.OperationID, r.InstallationID, r.Kind, r.Phase, r.PredecessorOperationID, r.PredecessorResultSHA256, sha, string(raw), now.UTC())
	if e != nil {
		return connectorcontrol.ControlOperation{}, e
	}
	result, e := loadControlOperation(ctx, tx, r.OperationID)
	if e != nil {
		return result, e
	}
	return result, tx.Commit()
}
func managedPurpose(r connectorcontrol.ControlOperationRequestV3) string {
	if r.Request.Registration != nil {
		return r.Request.Registration.Purpose
	}
	return ""
}

// Called only by the authenticated root-local ACK handler after host custody
// readback. Install -> operation -> adoption -> original state is the lock order.
func (s *WorkspaceStore) CompleteManagedControlOperation(ctx context.Context, identity string, v connectorcontrol.ControlResultV3, now time.Time) (connectorcontrol.Installation, error) {
	if s == nil || s.db == nil || !connectorServiceIdentityPattern.MatchString(identity) || connectorcontrol.ValidateControlResultV3(v, now) != nil {
		return connectorcontrol.Installation{}, connectorcontrol.ErrInvalid
	}
	if err := s.requireConnectorDeployment(ctx, v.InstallationID); err != nil {
		return connectorcontrol.Installation{}, err
	}

	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if e != nil {
		return connectorcontrol.Installation{}, e
	}
	defer tx.Rollback()
	i, e := lockedManagedInstallation(ctx, tx, v.InstallationID)
	if e != nil {
		return i, e
	}
	if i.ControlServiceIdentityID != identity {
		return i, connectorcontrol.ErrNotFound
	}
	identityIssue, e := managedControlIdentityIssue(ctx, tx, identity, now)
	if e != nil || identityIssue != "" {
		return i, connectorcontrol.ErrConflict
	}
	var opID string
	if e = tx.QueryRowContext(ctx, `SELECT operation_id FROM connector_control_operations WHERE operation_id=$1 AND installation_id=$2 FOR UPDATE`, v.OperationID, v.InstallationID).Scan(&opID); e != nil {
		return i, e
	}
	o, e := loadControlOperation(ctx, tx, opID)
	if e != nil {
		return i, e
	}
	r, e := connectorcontrol.OperationRequestV3(o)
	// ObservedAt belongs to UR's clock. Creation and commit timestamps belong
	// to XI; exact operation/predecessor authority establishes ordering.
	if e != nil || connectorcontrol.ValidateControlResultV3ForOperation(r, v) != nil {
		return i, connectorcontrol.ErrConflict
	}
	sha, raw, e := connectorcontrol.CanonicalSHA256(v)
	if e != nil {
		return i, e
	}
	if o.Status == "completed" || o.Status == "failed" {
		if o.ResultSHA256 != sha {
			return i, connectorcontrol.ErrConflict
		}
		return i, tx.Commit()
	}
	if o.Status != "running" || i.DesiredState != "active" || i.Status == "disconnected" || i.Status == "paused" {
		return i, connectorcontrol.ErrConflict
	}
	if v.Status == "completed" && v.Phase == connectorcontrol.PhaseBindExistingReceiver {
		c := *r.Request.Candidate
		var bootstrapRaw []byte
		if e = tx.QueryRowContext(ctx, `SELECT bootstrap FROM connector_installation_source_bindings WHERE installation_id=$1`, i.InstallationID).Scan(&bootstrapRaw); e != nil {
			return i, e
		}
		bootstrap, e := connectorcontrol.BootstrapFromJSON(bootstrapRaw)
		if e != nil || v.Result.AdoptionBinding.OrgID != bootstrap.OrgID || v.Result.AdoptionBinding.ProjectID != bootstrap.ProjectID {
			return i, connectorcontrol.ErrConflict
		}
		var selectedSHA string
		if e = tx.QueryRowContext(ctx, `SELECT candidate_sha256 FROM connector_receiver_adoptions WHERE installation_id=$1 AND status='selected' FOR UPDATE`, i.InstallationID).Scan(&selectedSHA); e != nil || selectedSHA != r.Request.CandidateSHA256 {
			return i, connectorcontrol.ErrConflict
		}
		k, e := readManagedCheckpoint(ctx, tx, c.Checkpoint.StateID)
		if e != nil || !checkpointMatches(k, c) {
			return i, connectorcontrol.ErrConflict
		}
		a := connectorcontrol.XIAdoptionReceipt{InstallationID: i.InstallationID, BindOperationID: o.OperationID, BindResultSHA256: sha, CandidateSHA256: selectedSHA, StateID: k.ID, ConsumerEpoch: k.Epoch, EpochOperationID: k.EpochOperation, CommittedCursorSHA256: connectorcontrol.RawSHA256(k.Cursor), CommittedHighWatermarkSHA256: connectorcontrol.RawSHA256(k.HighWatermark), CommittedAt: now.UTC().Truncate(time.Microsecond), PagesCommittedBefore: k.Pages}
		ah, ar, e := connectorcontrol.CanonicalSHA256(a)
		if e != nil {
			return i, e
		}
		if e = managedExactUpdate(ctx, tx, `UPDATE connector_receiver_adoptions SET status='bound',bind_operation_id=$2,bind_result_sha256=$3,local_receipt=$4,local_receipt_sha256=$5,updated_at=$6 WHERE installation_id=$1`, i.InstallationID, o.OperationID, sha, string(ar), ah, now.UTC()); e != nil {
			return i, e
		}
		if e = managedExactUpdate(ctx, tx, `UPDATE connector_installations SET source_instance=$2,source_instance_sha256=$3,status='pending',observed_state='controller_pending',updated_at=$4,last_agent_seen_at=$4,version=version+1 WHERE id=$1 AND (source_instance IS NULL OR source_instance=$2)`, i.InstallationID, c.SourceInstance, connectorcontrol.RawSHA256(c.SourceInstance), now.UTC()); e != nil {
			return i, e
		}
	} else if v.Status == "completed" && v.Phase == connectorcontrol.PhaseConfirmExistingReceiver {
		var localSHA string
		var stateID string
		var epoch int64
		var epochOp string
		if e = tx.QueryRowContext(ctx, `SELECT local_receipt_sha256,checkpoint_state_id::text,consumer_epoch,epoch_operation_id FROM connector_receiver_adoptions WHERE installation_id=$1 AND status='bound' FOR UPDATE`, i.InstallationID).Scan(&localSHA, &stateID, &epoch, &epochOp); e != nil || localSHA != r.Request.XIAdoptionReceiptSHA256 {
			return i, connectorcontrol.ErrConflict
		}
		k, e := readManagedCheckpoint(ctx, tx, stateID)
		bind, e := loadControlOperation(ctx, tx, r.PredecessorOperationID)
		if e != nil || bind.RequestV3 == nil || bind.RequestV3.Candidate == nil || !checkpointMatches(k, *bind.RequestV3.Candidate) || k.Epoch != epoch || k.EpochOperation != epochOp || k.Instance != v.Result.SourceInstance {
			return i, connectorcontrol.ErrConflict
		}
		if e = managedExactUpdate(ctx, tx, `UPDATE connector_receiver_adoptions SET status='confirmed',confirm_operation_id=$2,confirm_result_sha256=$3,updated_at=$4 WHERE installation_id=$1`, i.InstallationID, o.OperationID, sha, now.UTC()); e != nil {
			return i, e
		}
		if e = managedExactUpdate(ctx, tx, `UPDATE connector_installations SET status='connected',observed_state='connected',updated_at=$2,last_agent_seen_at=$2,version=version+1 WHERE id=$1 AND status='pending' AND observed_state='controller_pending'`, i.InstallationID, now.UTC()); e != nil {
			return i, e
		}
	}
	// Pair success/failure and adoption failure never degrade the manual feed or
	// connected installation. Their exact historical outcome remains on the op.
	_, e = tx.ExecContext(ctx, `UPDATE connector_control_operations SET status=$2,lease_expires_at=NULL,result_sha256=$3,result_payload=$4,reason_code=NULLIF($5,''),completed_at=$6,updated_at=$6 WHERE operation_id=$1`, o.OperationID, v.Status, sha, string(raw), v.ReasonCode, now.UTC())
	if e != nil {
		return i, e
	}
	if e = insertAccessAudit(ctx, tx, "system", identity, "connector_control.managed_ack", "connector_installation", i.InstallationID, "accepted", now.UTC()); e != nil {
		return i, e
	}
	i, e = loadConnectorInstallation(ctx, tx, i.InstallationID)
	if e != nil {
		return i, e
	}
	return i, tx.Commit()
}

func managedExactUpdate(ctx context.Context, tx *sql.Tx, query string, args ...any) error {
	r, e := tx.ExecContext(ctx, query, args...)
	if e != nil {
		return e
	}
	return requireOneConnectorRow(r)
}
