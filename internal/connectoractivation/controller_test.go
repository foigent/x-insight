package connectoractivation

import (
	"context"
	"errors"
	"testing"

	"github.com/foigent/x-insight/internal/connectorcontrol"
)

func TestControllerKeepsTheLocalAuthorityNarrow(t *testing.T) {
	t.Parallel()

	reconciler := &controllerReconcilerStub{reconcile: Outcome{State: StateDeferred}}
	controller, err := NewController(reconciler)
	if err != nil {
		t.Fatal(err)
	}
	request := connectorcontrol.ControllerRequest{
		SchemaVersion:  connectorcontrol.LocalControllerRequestSchema,
		OperationID:    "xco_AAAAAAAAAAAAAAAAAAAAAA",
		InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA",
		Action:         connectorcontrol.ControllerActionPrepare,
	}
	response, err := controller.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if response.State != "deferred" || response.ReasonCode != "controller_not_activated" ||
		reconciler.reconcileCalls != 1 || reconciler.inspectCalls != 0 {
		t.Fatalf("response=%+v reconciler=%+v", response, reconciler)
	}

	request.Action = connectorcontrol.ControllerActionInspect
	reconciler.inspect = Outcome{State: StateReady}
	response, err = controller.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if response.State != "ready" || response.ReasonCode != "" ||
		reconciler.reconcileCalls != 1 || reconciler.inspectCalls != 1 {
		t.Fatalf("response=%+v reconciler=%+v", response, reconciler)
	}
}

func TestControllerRejectsUnknownCapabilityAndInvalidOutcome(t *testing.T) {
	t.Parallel()

	reconciler := &controllerReconcilerStub{reconcile: Outcome{State: "arbitrary_command"}}
	controller, err := NewController(reconciler)
	if err != nil {
		t.Fatal(err)
	}
	request := connectorcontrol.ControllerRequest{
		SchemaVersion:  connectorcontrol.LocalControllerRequestSchema,
		OperationID:    "xco_AAAAAAAAAAAAAAAAAAAAAA",
		InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA",
		Action:         connectorcontrol.ControllerActionPrepare,
	}
	if _, err := controller.Execute(context.Background(), request); !errors.Is(err, connectorcontrol.ErrInvalid) {
		t.Fatalf("invalid outcome error=%v", err)
	}
	request.Action = "run_shell"
	if _, err := controller.Execute(context.Background(), request); !errors.Is(err, connectorcontrol.ErrInvalid) {
		t.Fatalf("unknown action error=%v", err)
	}
}

type controllerReconcilerStub struct {
	reconcile      Outcome
	inspect        Outcome
	reconcileCalls int
	inspectCalls   int
}

func (stub *controllerReconcilerStub) Reconcile(
	context.Context,
	string,
	string,
) (Outcome, error) {
	stub.reconcileCalls++
	return stub.reconcile, nil
}

func (stub *controllerReconcilerStub) Inspect(
	context.Context,
	string,
	string,
) (Outcome, error) {
	stub.inspectCalls++
	return stub.inspect, nil
}
