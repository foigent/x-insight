package connectoractivation

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/checkpointepoch"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/connectorsync"
)

var sha256Pattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type ManagedConfigReadback struct {
	ConfigSHA256   string
	NetworkBinding connectorcontrol.FixedReceiverNetworkBinding
}

type DormantReceiverReadback struct {
	PausedAuthSHA256        string
	DormantCredentialSHA256 string
}

type WorkerReadback struct {
	Present bool
	Active  bool
	Enabled bool
}

// Host is the fixed root-owned host lifecycle consumed by the reconciler. Its
// methods are installation-neutral because the durable installation/phase
// binding remains in ActivationStore; raw credentials never cross this port.
type Host interface {
	StageManagedFeedConfig(context.Context, string) (ManagedConfigReadback, error)
	PromoteManagedFeedConfig(context.Context, string) (ManagedConfigReadback, error)
	PrepareFeedCredential(context.Context, string, string) (connectorcontrol.PublicReceiverRegistrationV1, error)
	PromoteFeedCredentialDormant(context.Context, string, string) (DormantReceiverReadback, error)
	ActivateFeedWorkerLast(context.Context, string, string) (WorkerReadback, error)
}

type ProductReconciler struct {
	store     connectorcontrol.ActivationStore
	epochPort checkpointepoch.InstallationEpochPort
	host      Host
	now       func() time.Time
	random    io.Reader
}

func NewProductReconciler(
	store connectorcontrol.ActivationStore,
	epochPort checkpointepoch.InstallationEpochPort,
	host Host,
	now func() time.Time,
	random io.Reader,
) (*ProductReconciler, error) {
	if store == nil || epochPort == nil || host == nil || now == nil || random == nil {
		return nil, fmt.Errorf("connector activation dependencies are required")
	}
	return &ProductReconciler{
		store: store, epochPort: epochPort, host: host, now: now, random: random,
	}, nil
}

func (reconciler *ProductReconciler) Inspect(
	ctx context.Context,
	installationID string,
	wakeOperationID string,
) (out Outcome, resultErr error) {
	if initial, ok := reconciler.store.(interface {
		BeginInitialConnectorSync(context.Context, string, string) error
	}); ok && wakeOperationID != "" {
		if e := initial.BeginInitialConnectorSync(ctx, installationID, wakeOperationID); e != nil {
			return Outcome{}, e
		}
	}
	ctx, finish, gateErr := connectorsync.Work(ctx, reconciler.store, installationID, "activation")
	if gateErr != nil {
		return Outcome{}, gateErr
	}
	defer func() { connectorsync.Progress(ctx, out.State != StateReady, 0); finish(resultErr) }()
	boundary, err := reconciler.loadBoundary(ctx, installationID, wakeOperationID)
	if err != nil {
		return Outcome{}, fmt.Errorf("inspect original activation boundary: %w", err)
	}
	if boundary.Installation.Status == "connected" {
		return Outcome{State: StateReady}, nil
	}
	if boundary.Installation.Status == "abnormal" || terminalPhaseFailed(boundary) {
		return Outcome{State: StateDrift}, nil
	}
	return Outcome{State: StateDeferred}, nil
}

func (reconciler *ProductReconciler) Reconcile(
	ctx context.Context,
	installationID string,
	wakeOperationID string,
) (Outcome, error) {
	boundary, err := reconciler.loadBoundary(ctx, installationID, wakeOperationID)
	if err != nil {
		return Outcome{}, fmt.Errorf("inspect original activation boundary: %w", err)
	}
	if boundary.Installation.Status == "connected" {
		return Outcome{State: StateReady}, nil
	}
	if boundary.Installation.Status == "abnormal" || terminalPhaseFailed(boundary) {
		return Outcome{State: StateDrift}, nil
	}
	prepare := boundary.PreparePausedSource
	if prepare.Operation.Status != "completed" || prepare.Result == nil {
		return Outcome{State: StateDeferred}, nil
	}
	boundary, err = reconciler.claimActivationMutationAuthority(ctx, boundary)
	if err != nil {
		return Outcome{}, fmt.Errorf("claim original activation authority: %w", err)
	}

	installedAt := reconciler.now().UTC()
	epochAuthority, err := checkpointepoch.ApplyInstallationEpoch(
		ctx,
		reconciler.epochPort,
		checkpointepoch.InstallationEpochInput{
			Binding: checkpointepoch.Binding{
				SourceSystemID:    unirouter.DefaultSourceSystemID,
				AdapterManifestID: unirouter.DefaultCompletedExecutionAdapterManifestID,
				CursorKey:         connectorcontrol.ControlCursorKey,
			},
			InstallationID: boundary.Installation.InstallationID,
			WorkerUnit:     checkpointepoch.CompletedExecutionWorkerUnit,
			SourceInstance: prepare.Result.Result.SourceInstance,
			Cursor:         prepare.Result.Result.Cursor,
			HighWatermark:  prepare.Result.Result.HighWatermark,
		},
		installedAt,
	)
	if err != nil {
		return Outcome{}, fmt.Errorf("apply original installation epoch: %w", err)
	}

	if boundary.RegisterReceiver == nil || boundary.RegisterReceiver.Operation.Status != "completed" {
		config, err := reconciler.host.StageManagedFeedConfig(ctx, boundary.UniRouterOrigin)
		if err != nil {
			return Outcome{}, err
		}
		if validateManagedConfig(config) != nil {
			return Outcome{}, fmt.Errorf("managed feed config readback is invalid")
		}
		registration, err := reconciler.host.PrepareFeedCredential(ctx, "", "")
		if err != nil {
			return Outcome{}, err
		}
		phaseRequest := connectorcontrol.ControlPhaseRequestV2{
			SourceInstance:            prepare.Result.Result.SourceInstance,
			ConsumerEpoch:             epochAuthority.Epoch,
			CheckpointAuthoritySHA256: epochAuthority.Active.StateSHA256,
			ReceiverRegistration:      &registration,
			NetworkBinding:            &config.NetworkBinding,
			RequiredSourceState:       "paused",
		}
		_, err = reconciler.store.EnqueueControlPhase(ctx, connectorcontrol.EnqueueControlPhaseInput{
			InstallationID:          boundary.Installation.InstallationID,
			Phase:                   connectorcontrol.ControlPhaseRegisterReceiver,
			PredecessorOperationID:  prepare.Operation.OperationID,
			PredecessorResultSHA256: prepare.Operation.ResultSHA256,
			Request:                 phaseRequest,
		}, installedAt, reconciler.random)
		if err != nil {
			return Outcome{}, err
		}
		return Outcome{State: StateDeferred}, nil
	}

	register := boundary.RegisterReceiver
	if register.Operation.Status != "completed" || register.Result == nil {
		return Outcome{State: StateDeferred}, nil
	}
	stagedConfig, err := reconciler.host.StageManagedFeedConfig(ctx, boundary.UniRouterOrigin)
	if err != nil {
		return Outcome{}, err
	}
	if validateManagedConfig(stagedConfig) != nil ||
		register.Operation.Request == nil || register.Operation.Request.NetworkBinding == nil ||
		stagedConfig.NetworkBinding != *register.Operation.Request.NetworkBinding ||
		stagedConfig.NetworkBinding.BindingSHA256 != register.Result.Result.NetworkBindingSHA256 {
		return Outcome{}, fmt.Errorf("staged feed config differs from the registered network binding")
	}
	config, err := reconciler.host.PromoteManagedFeedConfig(
		ctx,
		stagedConfig.ConfigSHA256,
	)
	if err != nil {
		return Outcome{}, err
	}
	if validateManagedConfig(config) != nil ||
		config.NetworkBinding.BindingSHA256 != register.Result.Result.NetworkBindingSHA256 {
		return Outcome{}, fmt.Errorf("promoted feed config differs from the registered network binding")
	}
	registration := register.Operation.Request.ReceiverRegistration
	if registration == nil {
		return Outcome{}, fmt.Errorf("registered receiver authority is missing")
	}
	dormant, err := reconciler.host.PromoteFeedCredentialDormant(
		ctx,
		registration.OperationID,
		registration.VerifierSHA256,
	)
	if err != nil {
		return Outcome{}, err
	}
	if !sha256Pattern.MatchString(dormant.PausedAuthSHA256) ||
		!sha256Pattern.MatchString(dormant.DormantCredentialSHA256) {
		return Outcome{}, fmt.Errorf("dormant receiver readback is invalid")
	}

	if boundary.ResumeSource == nil {
		_, err = reconciler.store.EnqueueControlPhase(ctx, connectorcontrol.EnqueueControlPhaseInput{
			InstallationID:          boundary.Installation.InstallationID,
			Phase:                   connectorcontrol.ControlPhaseResumeSource,
			PredecessorOperationID:  register.Operation.OperationID,
			PredecessorResultSHA256: register.Operation.ResultSHA256,
			Request: connectorcontrol.ControlPhaseRequestV2{
				SourceInstance:                 register.Result.Result.SourceInstance,
				ReceiverRegistrationSHA256:     register.Result.Result.ReceiverRegistrationSHA256,
				NetworkBindingSHA256:           register.Result.Result.NetworkBindingSHA256,
				PausedAuthReceiptSHA256:        dormant.PausedAuthSHA256,
				DormantCredentialReceiptSHA256: dormant.DormantCredentialSHA256,
				RequiredSourceState:            "active",
				RequiredReceiverState:          "active",
				Readiness:                      connectorcontrol.ReadinessPausedReceiverAuthenticated,
			},
		}, reconciler.now().UTC(), reconciler.random)
		if err != nil {
			return Outcome{}, err
		}
		return Outcome{State: StateDeferred}, nil
	}

	resume := boundary.ResumeSource
	if resume.Operation.Status != "completed" || resume.Result == nil {
		return Outcome{State: StateDeferred}, nil
	}
	workerAuthority := captureWorkerActivationAuthority(boundary)
	freshBoundary, err := reconciler.loadBoundary(ctx, installationID, wakeOperationID)
	if err != nil {
		return Outcome{}, err
	}
	if workerAuthority.matchesConnectedAdvance(freshBoundary) {
		// Another actual finalizer completed this same immutable phase chain.
		// The stale pass has no remaining authority to mutate the host.
		return Outcome{State: StateReady}, nil
	}
	if validateActivationMutationAuthority(freshBoundary) != nil ||
		!workerAuthority.matches(freshBoundary) {
		return Outcome{}, fmt.Errorf("original worker activation authority changed: %w", connectorcontrol.ErrConflict)
	}
	freshRegister := freshBoundary.RegisterReceiver
	freshResume := freshBoundary.ResumeSource
	if freshRegister == nil || freshRegister.Operation.Request == nil ||
		freshRegister.Operation.Request.ReceiverRegistration == nil || freshResume == nil {
		return Outcome{}, connectorcontrol.ErrConflict
	}
	freshRegistration := freshRegister.Operation.Request.ReceiverRegistration
	worker, err := reconciler.host.ActivateFeedWorkerLast(
		ctx,
		freshRegistration.OperationID,
		freshRegistration.VerifierSHA256,
	)
	if err != nil {
		return Outcome{}, err
	}
	if !worker.Present || !worker.Active || !worker.Enabled {
		return Outcome{}, fmt.Errorf("feed worker activation readback is incomplete")
	}
	connected, err := reconciler.store.FinalizeInstallationConnected(
		ctx,
		freshBoundary.Installation.InstallationID,
		freshBoundary.Installation.Version,
		freshResume.Operation.OperationID,
		freshResume.Operation.ResultSHA256,
		reconciler.now().UTC(),
	)
	if err != nil {
		return Outcome{}, fmt.Errorf("finalize original installation connected: %w", err)
	}
	if connected.Status != "connected" || connected.ObservedState != "connected" ||
		connected.InstallationID != boundary.Installation.InstallationID {
		return Outcome{}, fmt.Errorf("connected installation readback is invalid")
	}
	return Outcome{State: StateReady}, nil
}

// validateActivationMutationAuthority is deliberately stricter than the
// general read-model validator. Paused, disconnected and earlier pending
// states remain valid values to inspect, but none authorizes host or epoch
// mutation by the activation reconciler.
func validateActivationMutationAuthority(boundary connectorcontrol.ActivationBoundary) error {
	installation := boundary.Installation
	if installation.Status != "pending" || installation.DesiredState != "active" ||
		installation.ObservedState != "controller_pending" {
		return connectorcontrol.ErrConflict
	}
	return nil
}

func (reconciler *ProductReconciler) claimActivationMutationAuthority(
	ctx context.Context,
	boundary connectorcontrol.ActivationBoundary,
) (connectorcontrol.ActivationBoundary, error) {
	if validateActivationMutationAuthority(boundary) == nil {
		return boundary, nil
	}
	installation := boundary.Installation
	prepare := boundary.PreparePausedSource
	if installation.Status != "pending" || installation.DesiredState != "active" ||
		installation.ObservedState != "source_ready" || prepare == nil || prepare.Result == nil ||
		prepare.Operation.Status != "completed" {
		return connectorcontrol.ActivationBoundary{}, connectorcontrol.ErrConflict
	}
	claimed, err := reconciler.store.ClaimInstallationActivation(
		ctx,
		installation.InstallationID,
		installation.Version,
		prepare.Operation.OperationID,
		prepare.Operation.ResultSHA256,
		reconciler.now().UTC(),
	)
	if err != nil {
		return connectorcontrol.ActivationBoundary{}, err
	}
	if connectorcontrol.ValidateActivationBoundary(claimed) != nil ||
		validateActivationMutationAuthority(claimed) != nil ||
		claimed.Installation.Version != installation.Version+1 ||
		claimed.PreparePausedSource == nil ||
		claimed.PreparePausedSource.Operation.OperationID != prepare.Operation.OperationID ||
		claimed.PreparePausedSource.Operation.ResultSHA256 != prepare.Operation.ResultSHA256 {
		return connectorcontrol.ActivationBoundary{}, connectorcontrol.ErrConflict
	}
	return claimed, nil
}

type workerActivationAuthority struct {
	installationAuthority connectorcontrol.Installation
	uniRouterOrigin       string
	prepareOperationID    string
	prepareRequestSHA     string
	prepareResultSHA      string
	installationID        string
	installationVersion   int64
	registerOperationID   string
	registerRequestSHA    string
	registerResultSHA     string
	resumeOperationID     string
	resumeRequestSHA      string
	resumeResultSHA       string
}

func captureWorkerActivationAuthority(boundary connectorcontrol.ActivationBoundary) workerActivationAuthority {
	authority := workerActivationAuthority{
		installationAuthority: boundary.Installation,
		uniRouterOrigin:       boundary.UniRouterOrigin,
		installationID:        boundary.Installation.InstallationID,
		installationVersion:   boundary.Installation.Version,
	}
	authority.installationAuthority.RequestedEventScope = append([]string(nil), boundary.Installation.RequestedEventScope...)
	authority.installationAuthority.FinalEventScope = append([]string(nil), boundary.Installation.FinalEventScope...)
	if boundary.PreparePausedSource != nil {
		authority.prepareOperationID = boundary.PreparePausedSource.Operation.OperationID
		authority.prepareRequestSHA = boundary.PreparePausedSource.Operation.RequestSHA256
		authority.prepareResultSHA = boundary.PreparePausedSource.Operation.ResultSHA256
	}
	if boundary.RegisterReceiver != nil {
		authority.registerOperationID = boundary.RegisterReceiver.Operation.OperationID
		authority.registerRequestSHA = boundary.RegisterReceiver.Operation.RequestSHA256
		authority.registerResultSHA = boundary.RegisterReceiver.Operation.ResultSHA256
	}
	if boundary.ResumeSource != nil {
		authority.resumeOperationID = boundary.ResumeSource.Operation.OperationID
		authority.resumeRequestSHA = boundary.ResumeSource.Operation.RequestSHA256
		authority.resumeResultSHA = boundary.ResumeSource.Operation.ResultSHA256
	}
	return authority
}

func (authority workerActivationAuthority) matchesConnectedAdvance(boundary connectorcontrol.ActivationBoundary) bool {
	i, before := boundary.Installation, authority.installationAuthority
	if i.Status != "connected" || i.ObservedState != "connected" || i.DesiredState != "active" || i.Version != authority.installationVersion+1 ||
		i.DeploymentID != before.DeploymentID || i.ConnectorType != before.ConnectorType || i.ProjectID != before.ProjectID ||
		i.ControlServiceIdentityID != before.ControlServiceIdentityID || i.InstallationAcknowledgementSHA256 != before.InstallationAcknowledgementSHA256 ||
		i.SourceInstance != before.SourceInstance || i.SourceInstanceSHA256 != before.SourceInstanceSHA256 || !i.CreatedAt.Equal(before.CreatedAt) {
		return false
	}
	if !reflect.DeepEqual(i.RequestedEventScope, before.RequestedEventScope) || !reflect.DeepEqual(i.FinalEventScope, before.FinalEventScope) {
		return false
	}
	p := boundary.PreparePausedSource
	if boundary.UniRouterOrigin != authority.uniRouterOrigin || p == nil || p.Result == nil || p.Operation.Status != "completed" ||
		p.Operation.OperationID != authority.prepareOperationID || p.Operation.RequestSHA256 != authority.prepareRequestSHA || p.Operation.ResultSHA256 != authority.prepareResultSHA {
		return false
	}
	authority.installationVersion = i.Version
	return authority.matches(boundary)
}

func (authority workerActivationAuthority) matches(boundary connectorcontrol.ActivationBoundary) bool {
	register := boundary.RegisterReceiver
	resume := boundary.ResumeSource
	return boundary.Installation.InstallationID == authority.installationID &&
		boundary.Installation.Version == authority.installationVersion &&
		register != nil && register.Operation.Status == "completed" && register.Result != nil &&
		register.Operation.OperationID == authority.registerOperationID &&
		register.Operation.RequestSHA256 == authority.registerRequestSHA &&
		register.Operation.ResultSHA256 == authority.registerResultSHA &&
		resume != nil && resume.Operation.Status == "completed" && resume.Result != nil &&
		resume.Operation.OperationID == authority.resumeOperationID &&
		resume.Operation.RequestSHA256 == authority.resumeRequestSHA &&
		resume.Operation.ResultSHA256 == authority.resumeResultSHA
}

func (reconciler *ProductReconciler) loadBoundary(
	ctx context.Context,
	installationID string,
	wakeOperationID string,
) (connectorcontrol.ActivationBoundary, error) {
	if reconciler == nil || reconciler.store == nil || reconciler.epochPort == nil || reconciler.host == nil ||
		!connectorcontrol.IsInstallationID(installationID) || !connectorcontrol.IsOperationID(wakeOperationID) {
		return connectorcontrol.ActivationBoundary{}, connectorcontrol.ErrInvalid
	}
	boundary, err := reconciler.store.InspectActivationBoundary(ctx, installationID)
	if err != nil {
		return connectorcontrol.ActivationBoundary{}, err
	}
	if connectorcontrol.ValidateActivationBoundary(boundary) != nil || !boundaryHasOperation(boundary, wakeOperationID) {
		return connectorcontrol.ActivationBoundary{}, connectorcontrol.ErrConflict
	}
	return boundary, nil
}

func boundaryHasOperation(boundary connectorcontrol.ActivationBoundary, operationID string) bool {
	for _, phase := range []*connectorcontrol.ActivationPhase{
		boundary.PreparePausedSource,
		boundary.RegisterReceiver,
		boundary.ResumeSource,
	} {
		if phase != nil && phase.Operation.OperationID == operationID {
			return true
		}
	}
	return false
}

func terminalPhaseFailed(boundary connectorcontrol.ActivationBoundary) bool {
	for _, phase := range []*connectorcontrol.ActivationPhase{
		boundary.PreparePausedSource,
		boundary.RegisterReceiver,
		boundary.ResumeSource,
	} {
		if phase != nil && phase.Operation.Status == "failed" {
			return true
		}
	}
	return false
}

func validateManagedConfig(value ManagedConfigReadback) error {
	if !sha256Pattern.MatchString(value.ConfigSHA256) ||
		value.NetworkBinding.Transport != connectorcontrol.ReceiverNetworkTransport ||
		value.NetworkBinding.Route != connectorcontrol.ReceiverFeedRoute ||
		!sha256Pattern.MatchString(value.NetworkBinding.BindingSHA256) {
		return connectorcontrol.ErrInvalid
	}
	return nil
}
