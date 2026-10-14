// Package connectoractivation composes the product-local Connector activation
// lifecycle. It does not define the cross-product control wire and it does not
// provide arbitrary host execution capabilities.
package connectoractivation

import (
	"context"
	"fmt"

	"github.com/foigent/x-insight/internal/connectorcontrol"
)

type State string

const (
	StateReady       State = "ready"
	StateDeferred    State = "deferred"
	StateUnavailable State = "unavailable"
	StateDrift       State = "drift"
)

type Outcome struct {
	State State
}

// Reconciler is deliberately installation-scoped. The local controller never
// accepts a path, unit, SQL statement, shell command, URL or credential.
type Reconciler interface {
	Reconcile(context.Context, string, string) (Outcome, error)
	Inspect(context.Context, string, string) (Outcome, error)
}

type Controller struct {
	reconciler Reconciler
}

func NewController(reconciler Reconciler) (*Controller, error) {
	if reconciler == nil {
		return nil, fmt.Errorf("connector activation reconciler is required")
	}
	return &Controller{reconciler: reconciler}, nil
}

func (controller *Controller) Execute(
	ctx context.Context,
	request connectorcontrol.ControllerRequest,
) (connectorcontrol.ControllerResponse, error) {
	if controller == nil || controller.reconciler == nil ||
		connectorcontrol.ValidateControllerRequest(request) != nil {
		return connectorcontrol.ControllerResponse{}, connectorcontrol.ErrInvalid
	}

	var (
		outcome Outcome
		err     error
	)
	switch request.Action {
	case connectorcontrol.ControllerActionPrepare:
		outcome, err = controller.reconciler.Reconcile(
			ctx,
			request.InstallationID,
			request.OperationID,
		)
	case connectorcontrol.ControllerActionInspect:
		outcome, err = controller.reconciler.Inspect(
			ctx,
			request.InstallationID,
			request.OperationID,
		)
	default:
		return connectorcontrol.ControllerResponse{}, connectorcontrol.ErrInvalid
	}
	if err != nil {
		return connectorcontrol.ControllerResponse{}, err
	}

	response := connectorcontrol.ControllerResponse{
		SchemaVersion:  connectorcontrol.LocalControllerResponseSchema,
		OperationID:    request.OperationID,
		InstallationID: request.InstallationID,
	}
	switch outcome.State {
	case StateReady:
		response.State = "ready"
	case StateDeferred:
		response.State = "deferred"
		response.ReasonCode = "controller_not_activated"
	case StateUnavailable:
		response.State = "abnormal"
		response.ReasonCode = "local_state_unavailable"
	case StateDrift:
		response.State = "abnormal"
		response.ReasonCode = "local_state_drift"
	default:
		return connectorcontrol.ControllerResponse{}, connectorcontrol.ErrInvalid
	}
	if connectorcontrol.ValidateControllerResponse(request, response) != nil {
		return connectorcontrol.ControllerResponse{}, connectorcontrol.ErrInvalid
	}
	return response, nil
}
