package main

import (
	"context"
	"crypto/x509"
	"fmt"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/connectorsync"
	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/integration/urmachine"
	"github.com/foigent/x-insight/internal/reviewpolicycontrol"
	"net/http"
	"os"
	"strconv"
)

func managedMachineTransport(ctx context.Context, engine *hostctl.Engine, b connectorcontrol.ManagedBoundary, purpose string) (string, *http.Client, urmachine.Identity, error) {
	var identity urmachine.Identity
	verified, ok := b.VerifiedSource()
	if !ok || b.Installation.Status != "connected" || b.Installation.DesiredState != "active" {
		return "", nil, identity, connectorcontrol.ErrConflict
	}
	phase := b.Phase(connectorcontrol.PhaseDeliverMachineCandidate, purpose)
	if phase == nil || phase.Operation.Status != "completed" || phase.Operation.RequestV3 == nil || phase.Operation.RequestV3.Registration == nil {
		return "", nil, identity, connectorcontrol.ErrConflict
	}
	identity = urmachine.Identity{InstallationID: b.Installation.InstallationID, SourceInstance: verified.SourceInstance, OrgID: strconv.FormatInt(verified.OrgID, 10)}
	if e := engine.ProveControllerURMachineRuntime(ctx, identity.InstallationID); e != nil {
		return "", nil, identity, e
	}
	path, e := engine.MachineCredentialPath(identity.InstallationID, purpose)
	if e != nil {
		return "", nil, identity, e
	}
	c, origin, _, e := engine.ReadURMachineConfig()
	if e != nil || origin != b.UniRouterOrigin {
		return "", nil, identity, connectorcontrol.ErrConflict
	}
	format := "xpm1"
	if purpose == connectorcontrol.EnrollmentMachinePurpose {
		format = "xse1"
	}
	var bearer string
	var transport *http.Client
	e = engine.WithPreparedMachine(ctx, *phase.Operation.RequestV3.Registration, func() error {
		var e error
		bearer, transport, e = machineFileTransport(path, c.CAFile, format)
		return e
	})
	return bearer, connectorsync.RequireClient(transport), identity, e
}
func managedEnrollmentFactory(engine *hostctl.Engine) func(context.Context, connectorcontrol.ManagedBoundary) (*urmachine.EnrollmentClient, error) {
	return func(ctx context.Context, b connectorcontrol.ManagedBoundary) (*urmachine.EnrollmentClient, error) {
		bearer, transport, identity, e := managedMachineTransport(ctx, engine, b, connectorcontrol.EnrollmentMachinePurpose)
		if e != nil {
			return nil, e
		}
		return urmachine.NewEnrollmentClient(b.UniRouterOrigin, bearer, identity, transport)
	}
}
func managedPolicyFactory(engine *hostctl.Engine) func(context.Context, connectorcontrol.ManagedBoundary) (*urmachine.Client, error) {
	return func(ctx context.Context, b connectorcontrol.ManagedBoundary) (*urmachine.Client, error) {
		bearer, transport, identity, e := managedMachineTransport(ctx, engine, b, connectorcontrol.PolicyMachinePurpose)
		if e != nil {
			return nil, e
		}
		return urmachine.NewPolicyClient(b.UniRouterOrigin, bearer, identity, transport)
	}
}
func managedPolicyReloadFactory(engine *hostctl.Engine, legacy reviewpolicycontrol.URPolicyClient, legacyIdentity urmachine.Identity) func(context.Context) (reviewpolicycontrol.URPolicyClient, urmachine.Identity, error) {
	return func(ctx context.Context) (reviewpolicycontrol.URPolicyClient, urmachine.Identity, error) {
		if e := ctx.Err(); e != nil {
			return nil, urmachine.Identity{}, e
		}
		c, origin, enabled, e := engine.ReadURMachineConfig()
		if e != nil {
			return nil, urmachine.Identity{}, e
		}
		if !enabled {
			return legacy, legacyIdentity, nil
		}
		if e := engine.ProveControllerURMachineRuntime(ctx, c.InstallationID); e != nil {
			return nil, urmachine.Identity{}, e
		}
		identity := urmachine.Identity{InstallationID: c.InstallationID, SourceInstance: c.SourceInstance, OrgID: c.OrgID}
		if c.PolicyFile == "" {
			return nil, identity, fmt.Errorf("policy purpose has no prepared custody")
		}
		bearer, transport, e := machineFileTransport(c.PolicyFile, c.CAFile, "xpm1")
		if e != nil {
			return nil, identity, e
		}
		client, e := urmachine.NewPolicyClient(origin, bearer, identity, connectorsync.RequireClient(transport))
		return client, identity, e
	}
}
func managedSourceRoots(engine *hostctl.Engine) (*x509.CertPool, error) {
	c, _, enabled, e := engine.ReadURMachineConfig()
	if e != nil || !enabled || c.CAFile == "" {
		return nil, e
	}
	// The public trust reference is validated by the same root module loader.
	raw, e := os.ReadFile(c.CAFile)
	if e != nil {
		return nil, e
	}
	roots, e := x509.SystemCertPool()
	if e != nil {
		return nil, e
	}
	if !roots.AppendCertsFromPEM(raw) {
		return nil, fmt.Errorf("managed source trust is unavailable")
	}
	return roots, nil
}
