package connectoractivation

import (
	"context"
	"fmt"

	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/hostctl"
)

// HostctlAdapter exposes only the fixed CP2 host lifecycle to the product
// reconciler. It deliberately omits paths, unit names, commands, SQL and raw
// credential material from its interface.
type HostctlAdapter struct {
	engine *hostctl.Engine
}

func NewHostctlAdapter(engine *hostctl.Engine) (*HostctlAdapter, error) {
	if engine == nil {
		return nil, fmt.Errorf("hostctl engine is required")
	}
	return &HostctlAdapter{engine: engine}, nil
}

func (adapter *HostctlAdapter) StageManagedFeedConfig(
	ctx context.Context,
	trustedOrigin string,
) (ManagedConfigReadback, error) {
	report, err := adapter.engine.StageManagedFeedConfig(ctx, trustedOrigin)
	return managedConfigReadback(report, err)
}

func (adapter *HostctlAdapter) PromoteManagedFeedConfig(
	ctx context.Context,
	expectedConfigSHA256 string,
) (ManagedConfigReadback, error) {
	report, err := adapter.engine.PromoteManagedFeedConfig(ctx, expectedConfigSHA256)
	return managedConfigReadback(report, err)
}

func (adapter *HostctlAdapter) PrepareFeedCredential(
	ctx context.Context,
	expectedActiveOperationID string,
	expectedActiveVerifierSHA256 string,
) (connectorcontrol.PublicReceiverRegistrationV1, error) {
	report, err := adapter.engine.PrepareFeedCredential(
		ctx,
		expectedActiveOperationID,
		expectedActiveVerifierSHA256,
	)
	if err != nil {
		return connectorcontrol.PublicReceiverRegistrationV1{}, err
	}
	return connectorcontrol.PublicReceiverRegistrationV1{
		SchemaVersion:  report.SchemaVersion,
		TokenFormat:    report.TokenFormat,
		VerifierScheme: report.VerifierScheme,
		KeyPrefix:      report.KeyPrefix,
		VerifierSHA256: report.VerifierSHA256,
		OperationID:    report.OperationID,
	}, nil
}

func (adapter *HostctlAdapter) PromoteFeedCredentialDormant(
	ctx context.Context,
	operationID string,
	verifierSHA256 string,
) (DormantReceiverReadback, error) {
	report, err := adapter.engine.PromoteFeedCredentialDormant(ctx, operationID, verifierSHA256)
	if err != nil {
		return DormantReceiverReadback{}, err
	}
	return DormantReceiverReadback{
		PausedAuthSHA256:        report.PausedAuthReceiptSHA256,
		DormantCredentialSHA256: report.DormantCredentialReceiptSHA256,
	}, nil
}

func (adapter *HostctlAdapter) ActivateFeedWorkerLast(
	ctx context.Context,
	operationID string,
	verifierSHA256 string,
) (WorkerReadback, error) {
	report, err := adapter.engine.ActivateFeedWorkerLast(ctx, operationID, verifierSHA256)
	if err != nil {
		return WorkerReadback{}, err
	}
	return WorkerReadback{Present: report.Present, Active: report.Active, Enabled: report.Enabled}, nil
}

func managedConfigReadback(
	report hostctl.ManagedFeedConfigStatus,
	err error,
) (ManagedConfigReadback, error) {
	if err != nil {
		return ManagedConfigReadback{}, err
	}
	if report.NetworkBinding == nil || report.NetworkBindingSHA256 != report.NetworkBinding.BindingSHA256 {
		return ManagedConfigReadback{}, fmt.Errorf("managed feed config network binding readback is invalid")
	}
	return ManagedConfigReadback{
		ConfigSHA256:   report.ConfigSHA256,
		NetworkBinding: *report.NetworkBinding,
	}, nil
}
