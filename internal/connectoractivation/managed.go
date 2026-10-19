package connectoractivation

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/connectorsync"
	"github.com/foigent/x-insight/internal/hostctl"
	"io"
	"strconv"
	"time"
)

// ManagedReconciler extends the existing controller and maintenance loop.
// Network calls and human activation are never held under a host or DB lock.
type ManagedReconciler struct {
	Store    connectorcontrol.ManagedStore
	Host     *hostctl.Engine
	Legacy   Reconciler
	Now      func() time.Time
	Random   io.Reader
	TLSRoots *x509.CertPool
	SyncWake func()
}

func (m *ManagedReconciler) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC().Truncate(time.Microsecond)
	}
	return time.Now().UTC().Truncate(time.Microsecond)
}
func (m *ManagedReconciler) newRequest(b connectorcontrol.ManagedBoundary, kind, phase string, request connectorcontrol.ControlPhaseRequestV3, p *connectorcontrol.ActivationPhase) (connectorcontrol.ControlOperationRequestV3, error) {
	random := m.Random
	if random == nil {
		random = rand.Reader
	}
	v := make([]byte, 16)
	if _, e := io.ReadFull(random, v); e != nil {
		return connectorcontrol.ControlOperationRequestV3{}, e
	}
	r := connectorcontrol.ControlOperationRequestV3{SchemaVersion: connectorcontrol.ControlOperationSchemaV3, OperationID: "xco_" + base64.RawURLEncoding.EncodeToString(v), InstallationID: b.Installation.InstallationID, ProjectID: b.Installation.ProjectID, EventScope: b.Installation.FinalEventScope, DesiredState: "active", Kind: kind, Phase: phase, Request: request}
	if p != nil {
		r.PredecessorOperationID = p.Operation.OperationID
		r.PredecessorResultSHA256 = p.Operation.ResultSHA256
	}
	return r, connectorcontrol.ValidateControlOperationRequestV3(r)
}
func (m *ManagedReconciler) Tick(ctx context.Context) error {
	if m == nil || m.Store == nil || m.Host == nil {
		return connectorcontrol.ErrInvalid
	}
	ids, e := m.Store.ListManagedConnectorIDs(ctx, 100)
	if e != nil {
		return e
	}
	var failures []error
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, e = m.Reconcile(ctx, id, ""); e != nil {
			failures = append(failures, fmt.Errorf("managed installation %s: %w", id, e))
		}
	}
	return errors.Join(failures...)
}
func (m *ManagedReconciler) Inspect(ctx context.Context, id, wake string) (Outcome, error) {
	b, e := m.Store.InspectManagedConnector(ctx, id)
	if e != nil {
		return Outcome{}, e
	}
	if b.Bootstrap == nil {
		if wake == "" {
			wake, e = legacyWakeOperation(b)
			if e != nil {
				return Outcome{}, e
			}
		}
		return m.Legacy.Inspect(ctx, id, wake)
	}
	if wake != "" && !managedWake(b, wake) {
		return Outcome{}, connectorcontrol.ErrConflict
	}
	for _, p := range b.Phases {
		if p.Operation.Status == "failed" && p.Operation.Kind == connectorcontrol.KindAdoptExistingReceiver {
			return Outcome{State: StateDrift}, nil
		}
	}
	if b.Installation.Status == "connected" {
		return Outcome{State: StateReady}, nil
	}
	return Outcome{State: StateDeferred}, nil
}
func managedWake(b connectorcontrol.ManagedBoundary, wake string) bool {
	for _, p := range b.Phases {
		if p.Operation.OperationID == wake {
			return true
		}
	}
	return false
}
func (m *ManagedReconciler) Reconcile(ctx context.Context, id, wake string) (out Outcome, resultErr error) {
	if initial, ok := m.Store.(interface {
		BeginInitialConnectorSync(context.Context, string, string) error
	}); ok && wake != "" {
		if e := initial.BeginInitialConnectorSync(ctx, id, wake); e != nil {
			return Outcome{}, e
		}
	}
	ctx, finish, gateErr := connectorsync.Work(ctx, m.Store, id, "activation")
	if gateErr != nil {
		return Outcome{}, gateErr
	}
	defer func() { connectorsync.Progress(ctx, out.State != StateReady, 0); finish(resultErr) }()
	b, e := m.Store.InspectManagedConnector(ctx, id)
	if e != nil {
		return Outcome{}, e
	}
	if b.Bootstrap == nil {
		// The periodic maintenance pass has no external wake ID. The original
		// reconciler still requires an exact durable phase, just as Unix wakes do.
		if wake == "" {
			wake, e = legacyWakeOperation(b)
			if e != nil {
				return Outcome{}, e
			}
		}
		out, e := m.Legacy.Reconcile(ctx, id, wake)
		if e != nil {
			return out, fmt.Errorf("reconcile original installation: %w", e)
		}
		b, e = m.Store.InspectManagedConnector(ctx, id)
		if e != nil {
			return Outcome{}, e
		}
		if b.Installation.Status != "connected" {
			return out, nil
		}
		r, worker, e := connectorcontrol.OriginalReceiverProof(b)
		if e != nil {
			return Outcome{State: StateUnavailable}, fmt.Errorf("read completed original receiver proof: %w", e)
		}
		if b.Descriptor == nil {
			v, e := m.Host.FetchOriginalSourceDescriptor(ctx, b, m.TLSRoots)
			if e != nil {
				return Outcome{State: StateUnavailable}, fmt.Errorf("fetch original source descriptor: %w", e)
			}
			e = m.Host.WithExistingFeed(ctx, worker, r, func() error { return m.Store.SaveManagedSourceDescriptor(ctx, v, m.now()) })
			if e != nil {
				return Outcome{}, fmt.Errorf("persist original source descriptor: %w", e)
			}
			b, e = m.Store.InspectManagedConnector(ctx, id)
			if e != nil {
				return Outcome{}, e
			}
		} else if e = m.Host.WithExistingFeed(ctx, worker, r, func() error { return nil }); e != nil {
			return Outcome{State: StateDrift}, e
		}
		return m.prepareMachines(ctx, b, b.Phase(connectorcontrol.ControlPhaseResumeSource, ""))
	}
	if wake != "" && !managedWake(b, wake) {
		return Outcome{}, connectorcontrol.ErrConflict
	}
	if b.Installation.DesiredState != "active" || b.Installation.Status == "disconnected" || b.Installation.Status == "paused" {
		return Outcome{State: StateDrift}, nil
	}
	boot := b.Bootstrap
	bind := b.Phase(connectorcontrol.PhaseBindExistingReceiver, "")
	if bind == nil {
		if b.Checkpoint == nil {
			return Outcome{State: StateUnavailable}, nil
		}
		config, e := m.Host.ManagedFeedConfigStatus(ctx)
		if e != nil {
			return Outcome{}, e
		}
		state, e := m.Host.FeedCredentialStatus(ctx)
		if e != nil || state.ActiveRegistration == nil || state.PendingRegistration != nil || state.StagedFile.Present || !config.Active || config.Staged || config.TrustedOrigin != b.UniRouterOrigin {
			return Outcome{State: StateDrift}, e
		}
		raw := state.ActiveRegistration
		registration := connectorcontrol.PublicReceiverRegistrationV1{SchemaVersion: raw.SchemaVersion, TokenFormat: raw.TokenFormat, VerifierScheme: raw.VerifierScheme, KeyPrefix: raw.KeyPrefix, VerifierSHA256: raw.VerifierSHA256, OperationID: raw.OperationID}
		if registration.KeyPrefix != boot.KeyPrefix || registration.OperationID != boot.RegistrationOperationID {
			return Outcome{State: StateDrift}, nil
		}
		rh, _, _ := connectorcontrol.CanonicalSHA256(registration)
		candidate := connectorcontrol.ExistingReceiverCandidate{SourceInstance: boot.SourceInstance, SourceBindingSHA256: b.SourceBindingSHA256, FeedKeyID: boot.FeedKeyID, ReceiverRegistration: registration, AllowedIP: boot.AllowedIP, WorkerConfig: connectorcontrol.ExistingWorkerConfig{WorkerUnit: "xinsight-feed.service", UROrigin: b.UniRouterOrigin, FeedRoute: connectorcontrol.ReceiverFeedRoute, SourceInstance: boot.SourceInstance, FeedSchema: connectorcontrol.ControlFeedSchema, CursorKey: connectorcontrol.ControlCursorKey, ReceiverRegistrationSHA256: rh}, Checkpoint: *b.Checkpoint}
		h, _, e := connectorcontrol.CanonicalSHA256(candidate)
		if e != nil {
			return Outcome{}, e
		}
		r, e := m.newRequest(b, connectorcontrol.KindAdoptExistingReceiver, connectorcontrol.PhaseBindExistingReceiver, connectorcontrol.ControlPhaseRequestV3{Candidate: &candidate, CandidateSHA256: h}, nil)
		if e != nil {
			return Outcome{}, e
		}
		e = m.Host.WithExistingFeed(ctx, candidate.WorkerConfig, registration, func() error { _, e := m.Store.EnqueueManagedControlOperation(ctx, r, m.now()); return e })
		return Outcome{State: StateDeferred}, e
	}
	if bind.Operation.Status == "failed" {
		return Outcome{State: StateDrift}, nil
	}
	if bind.Operation.Status != "completed" {
		return Outcome{State: StateDeferred}, nil
	}
	confirm := b.Phase(connectorcontrol.PhaseConfirmExistingReceiver, "")
	if confirm == nil {
		if b.LocalReceipt == nil || b.FeedRead == nil {
			return Outcome{State: StateDeferred}, nil
		}
		if bind.Result == nil || bind.Result.ResultV3 == nil {
			return Outcome{}, connectorcontrol.ErrConflict
		}
		ah, _, _ := connectorcontrol.CanonicalSHA256(*b.LocalReceipt)
		fh, _, _ := connectorcontrol.CanonicalSHA256(*b.FeedRead)
		r, e := m.newRequest(b, connectorcontrol.KindAdoptExistingReceiver, connectorcontrol.PhaseConfirmExistingReceiver, connectorcontrol.ControlPhaseRequestV3{AdoptionBindingSHA256: bind.Result.ResultV3.Result.AdoptionBindingSHA256, XIAdoptionReceipt: b.LocalReceipt, XIAdoptionReceiptSHA256: ah, FeedReadReceipt: b.FeedRead, FeedReadReceiptSHA256: fh}, bind)
		if e != nil {
			return Outcome{}, e
		}
		c := bind.Operation.RequestV3.Candidate
		e = m.Host.WithExistingFeed(ctx, c.WorkerConfig, c.ReceiverRegistration, func() error { _, e := m.Store.EnqueueManagedControlOperation(ctx, r, m.now()); return e })
		return Outcome{State: StateDeferred}, e
	}
	if confirm.Operation.Status == "failed" {
		return Outcome{State: StateDrift}, nil
	}
	if confirm.Operation.Status != "completed" || b.Installation.Status != "connected" {
		return Outcome{State: StateDeferred}, nil
	}
	return m.prepareMachines(ctx, b, confirm)
}

func legacyWakeOperation(b connectorcontrol.ManagedBoundary) (string, error) {
	for _, phase := range []string{connectorcontrol.ControlPhaseResumeSource, connectorcontrol.ControlPhaseRegisterReceiver, connectorcontrol.ControlPhasePreparePausedSource} {
		if p := b.Phase(phase, ""); p != nil && p.Operation.SchemaVersion == connectorcontrol.ControlOperationSchemaV2 {
			return p.Operation.OperationID, nil
		}
	}
	return "", connectorcontrol.ErrConflict
}
func (m *ManagedReconciler) prepareMachines(ctx context.Context, b connectorcontrol.ManagedBoundary, predecessor *connectorcontrol.ActivationPhase) (Outcome, error) {
	verified, ok := b.VerifiedSource()
	if !ok || predecessor == nil {
		return Outcome{}, fmt.Errorf("machine preparation source/predecessor authority: %w", connectorcontrol.ErrConflict)
	}
	id := b.Installation.InstallationID
	for _, purpose := range []string{connectorcontrol.PolicyMachinePurpose, connectorcontrol.EnrollmentMachinePurpose} {
		if b.Phase(connectorcontrol.PhaseDeliverMachineCandidate, purpose) != nil {
			continue
		}
		prepared, e := m.Host.PrepareMachineCredential(ctx, id, purpose, verified.SourceInstance, strconv.FormatInt(verified.OrgID, 10), verified.AllowedIP)
		if e != nil {
			return Outcome{}, fmt.Errorf("prepare protected machine candidate: %w", e)
		}
		r, e := m.newRequest(b, connectorcontrol.KindPairMachine, connectorcontrol.PhaseDeliverMachineCandidate, connectorcontrol.ControlPhaseRequestV3{SourceInstance: verified.SourceInstance, SourceBindingSHA256: b.SourceBindingSHA256, Registration: &prepared.Registration, RegistrationSHA256: prepared.RegistrationSHA256, CustodyReceipt: &prepared.CustodyReceipt, CustodyReceiptSHA256: prepared.CustodyReceiptSHA256}, predecessor)
		if e != nil {
			return Outcome{}, fmt.Errorf("construct closed machine operation: %w", e)
		}
		if _, e = m.Store.EnqueueManagedControlOperation(ctx, r, m.now()); e != nil {
			return Outcome{}, fmt.Errorf("enqueue protected machine candidate: %w", e)
		}
	}
	policyFile, e := m.Host.MachineCredentialPath(id, connectorcontrol.PolicyMachinePurpose)
	if e != nil {
		return Outcome{}, fmt.Errorf("resolve policy root credential: %w", e)
	}
	enrollFile, e := m.Host.MachineCredentialPath(id, connectorcontrol.EnrollmentMachinePurpose)
	if e != nil {
		return Outcome{}, fmt.Errorf("resolve enrollment root credential: %w", e)
	}
	current, _, enabled, e := m.Host.ReadURMachineConfig()
	if e != nil {
		return Outcome{}, fmt.Errorf("read current machine configuration: %w", e)
	}
	if !enabled || current.InstallationID == id {
		candidate := hostctl.URMachineConfig{InstallationID: id, SourceInstance: verified.SourceInstance, OrgID: strconv.FormatInt(verified.OrgID, 10), PolicyFile: policyFile, EnrollFile: enrollFile, CAFile: current.CAFile}
		if candidate != current {
			if e = m.Host.SetURMachineConfigForController(ctx, candidate); e != nil {
				return Outcome{}, fmt.Errorf("publish machine configuration: %w", e)
			}
		}
	}
	if e = m.Host.EnsureControllerURMachineRuntime(ctx, id); e != nil {
		return Outcome{State: StateDeferred}, fmt.Errorf("load machine runtime: %w", e)
	}
	return Outcome{State: StateReady}, nil
}
func (m *ManagedReconciler) AcknowledgeManaged(ctx context.Context, identity string, result connectorcontrol.ControlResultV3) (connectorcontrol.Installation, error) {
	b, e := m.Store.InspectManagedConnector(ctx, result.InstallationID)
	if e != nil {
		return connectorcontrol.Installation{}, e
	}
	var phase *connectorcontrol.ActivationPhase
	for i := range b.Phases {
		if b.Phases[i].Operation.OperationID == result.OperationID {
			phase = &b.Phases[i]
			break
		}
	}
	if phase == nil || phase.Operation.RequestV3 == nil {
		return connectorcontrol.Installation{}, connectorcontrol.ErrConflict
	}
	var installation connectorcontrol.Installation
	commit := func() error {
		var e error
		installation, e = m.Store.CompleteManagedControlOperation(ctx, identity, result, m.now())
		return e
	}
	if result.Status == "failed" {
		e = commit()
	} else if result.Kind == connectorcontrol.KindPairMachine {
		r := phase.Operation.RequestV3.Registration
		e = m.Host.WithPreparedMachine(ctx, *r, commit)
	} else {
		bind := b.Phase(connectorcontrol.PhaseBindExistingReceiver, "")
		if bind == nil || bind.Operation.RequestV3 == nil || bind.Operation.RequestV3.Candidate == nil {
			return installation, connectorcontrol.ErrConflict
		}
		c := bind.Operation.RequestV3.Candidate
		e = m.Host.WithExistingFeed(ctx, c.WorkerConfig, c.ReceiverRegistration, commit)
	}
	if e == nil {
		if initial, ok := m.Store.(interface {
			BeginInitialConnectorSync(context.Context, string, string) error
		}); ok {
			if localErr := initial.BeginInitialConnectorSync(ctx, result.InstallationID, result.OperationID); localErr == nil && m.SyncWake != nil {
				m.SyncWake()
			}
		}
	}

	return installation, e
}
