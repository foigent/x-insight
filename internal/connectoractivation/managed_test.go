package connectoractivation

import (
	"context"
	"errors"
	"testing"

	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/hostctl"
)

type maintenanceBoundaryStore struct {
	connectorcontrol.ManagedStore
	b connectorcontrol.ManagedBoundary
}

func (s maintenanceBoundaryStore) ListManagedConnectorIDs(context.Context, int) ([]string, error) {
	return []string{s.b.Installation.InstallationID}, nil
}
func (s maintenanceBoundaryStore) InspectManagedConnector(context.Context, string) (connectorcontrol.ManagedBoundary, error) {
	return s.b, nil
}

type exactLegacyWake struct {
	wake string
	err  error
}

func (l *exactLegacyWake) Reconcile(_ context.Context, _, wake string) (Outcome, error) {
	if !connectorcontrol.IsOperationID(wake) {
		return Outcome{}, connectorcontrol.ErrInvalid
	}
	l.wake = wake
	if l.err != nil {
		return Outcome{}, l.err
	}
	return Outcome{State: StateDeferred}, nil
}
func (l *exactLegacyWake) Inspect(ctx context.Context, id, wake string) (Outcome, error) {
	return l.Reconcile(ctx, id, wake)
}

func TestManagedMaintenancePreservesExactOriginalV2Wake(t *testing.T) {
	phases := []string{connectorcontrol.ControlPhasePreparePausedSource, connectorcontrol.ControlPhaseRegisterReceiver, connectorcontrol.ControlPhaseResumeSource}
	ids := []string{"xco_AAAAAAAAAAAAAAAAAAAAAA", "xco_BBBBBBBBBBBBBBBBBBBBBB", "xco_CCCCCCCCCCCCCCCCCCCCCC"}
	for n := range phases {
		t.Run(phases[n], func(t *testing.T) {
			b := connectorcontrol.ManagedBoundary{Installation: connectorcontrol.Installation{InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA", Status: "pending"}}
			for i := 0; i <= n; i++ {
				b.Phases = append(b.Phases, connectorcontrol.ActivationPhase{Operation: connectorcontrol.ControlOperation{SchemaVersion: connectorcontrol.ControlOperationSchemaV2, OperationID: ids[i], Phase: phases[i]}})
			}
			// A newer machine phase cannot replace the original wake authority.
			b.Phases = append(b.Phases, connectorcontrol.ActivationPhase{Operation: connectorcontrol.ControlOperation{SchemaVersion: connectorcontrol.ControlOperationSchemaV3, OperationID: "xco_DDDDDDDDDDDDDDDDDDDDDD", Phase: connectorcontrol.PhaseDeliverMachineCandidate}})
			legacy := &exactLegacyWake{}
			m := ManagedReconciler{Store: maintenanceBoundaryStore{b: b}, Host: &hostctl.Engine{}, Legacy: legacy}
			if err := m.Tick(t.Context()); err != nil || legacy.wake != ids[n] {
				t.Fatalf("maintenance wake=%q err=%v", legacy.wake, err)
			}
			legacy.wake = ""
			if _, err := m.Inspect(t.Context(), b.Installation.InstallationID, ""); err != nil || legacy.wake != ids[n] {
				t.Fatalf("inspection wake=%q err=%v", legacy.wake, err)
			}
			explicit := "xco_EEEEEEEEEEEEEEEEEEEEEE"
			if _, err := m.Reconcile(t.Context(), b.Installation.InstallationID, explicit); err != nil || legacy.wake != explicit {
				t.Fatal("external exact wake was replaced", err)
			}
		})
	}
	if _, err := legacyWakeOperation(connectorcontrol.ManagedBoundary{}); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatal("missing original authority accepted", err)
	}
}

func TestManagedMaintenancePreservesTypedHostContention(t *testing.T) {
	b := connectorcontrol.ManagedBoundary{Installation: connectorcontrol.Installation{InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA", Status: "pending"}, Phases: []connectorcontrol.ActivationPhase{{Operation: connectorcontrol.ControlOperation{SchemaVersion: connectorcontrol.ControlOperationSchemaV2, OperationID: "xco_AAAAAAAAAAAAAAAAAAAAAA", Phase: connectorcontrol.ControlPhasePreparePausedSource}}}}
	legacy := &exactLegacyWake{err: hostctl.ErrOperationBusy}
	m := ManagedReconciler{Store: maintenanceBoundaryStore{b: b}, Host: &hostctl.Engine{}, Legacy: legacy}
	if err := m.Tick(t.Context()); !errors.Is(err, hostctl.ErrOperationBusy) {
		t.Fatal("host contention classification lost", err)
	}
	if outcome, err := m.Reconcile(t.Context(), b.Installation.InstallationID, ""); !errors.Is(err, hostctl.ErrOperationBusy) || outcome.State == StateReady {
		t.Fatal("busy was acknowledged as ready", outcome, err)
	}
	legacy.err = errors.New("different host failure")
	if err := m.Tick(t.Context()); !errors.Is(err, legacy.err) {
		t.Fatal("nonbusy failure suppressed", err)
	}
	legacy.err = nil
	if err := m.Tick(t.Context()); err != nil || legacy.wake != "xco_AAAAAAAAAAAAAAAAAAAAAA" {
		t.Fatal("released host did not reread exact authority", err)
	}
}
