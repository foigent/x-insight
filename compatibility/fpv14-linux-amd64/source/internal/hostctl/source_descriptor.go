package hostctl

import (
	"context"
	"crypto/x509"
	"fmt"
	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"os"
)

// Reads the original protected credential under a short host qualification;
// the bounded fixed-origin HTTPS call is outside that lock.
func (e *Engine) FetchOriginalSourceDescriptor(ctx context.Context, b connectorcontrol.ManagedBoundary, roots *x509.CertPool) (connectorcontrol.SourceDescriptorV1, error) {
	var v connectorcontrol.SourceDescriptorV1
	r, worker, err := connectorcontrol.OriginalReceiverProof(b)
	if err != nil {
		return v, err
	}
	if roots == nil {
		machine, _, _, err := e.ReadURMachineConfig()
		if err != nil {
			return v, err
		}
		if machine.CAFile != "" {
			raw, err := os.ReadFile(machine.CAFile)
			if err != nil {
				return v, err
			}
			roots, err = x509.SystemCertPool()
			if err != nil {
				return v, err
			}
			if !roots.AppendCertsFromPEM(raw) {
				return v, connectorcontrol.ErrConflict
			}
		}
	}
	var source *pullfeed.HTTPSource
	err = e.WithExistingFeed(ctx, worker, r, func() error {
		cfg, present, err := e.readManagedFeedConfigOptional(e.Paths.FeedEnvironmentFile)
		register := b.Phase(connectorcontrol.ControlPhaseRegisterReceiver, "")
		if err != nil || !present || cfg.networkBinding.BindingSHA256 != register.Operation.Request.NetworkBinding.BindingSHA256 {
			return fmt.Errorf("original feed network binding: %w", connectorcontrol.ErrConflict)
		}
		key, err := config.LoadOwnerOnlySecretFile(e.Paths.FeedCredentialFile)
		if err != nil {
			return err
		}
		source, err = pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: worker.UROrigin + worker.FeedRoute, ServiceKey: key, TLSRootCAs: roots})
		return err
	})
	if err != nil {
		return v, err
	}
	defer source.CloseContentConnections()
	raw, err := source.FetchConnectorSourceDescriptor(ctx, b.Installation.InstallationID)
	if err != nil {
		return v, err
	}
	v, err = connectorcontrol.DecodeSourceDescriptor(raw)
	if err != nil {
		return v, fmt.Errorf("decode closed source descriptor: %w", connectorcontrol.ErrConflict)
	}
	if connectorcontrol.ValidateSourceDescriptorProof(b, v) != nil {
		return v, fmt.Errorf("source descriptor differs from original receiver proof: %w", connectorcontrol.ErrConflict)
	}
	err = e.WithExistingFeed(ctx, worker, r, func() error { return nil })
	return v, err
}
