package connectorcontrol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"time"
)

const (
	LocalControllerRequestSchema  = "xinsight.local-connector-controller-request.v1"
	LocalControllerResponseSchema = "xinsight.local-connector-controller-response.v1"
	LocalInvitationRequestSchema  = "xinsight.local-connector-invitation-request.v1"
	LocalInvitationResponseSchema = "xinsight.local-connector-invitation-response.v1"
	LocalControllerSocket         = "/run/x-insight/connector-controller.sock"
	ControllerActionIssueInvite   = "issue_invitation"
	ControllerActionInspect       = "inspect_installation"
	ControllerActionPrepare       = "prepare_installation"
	maximumControllerMessageBytes = 16 << 10
)

var controllerHumanSubjectPattern = regexp.MustCompile(`^xs_[a-z0-9]{16,64}$`)

type ControllerRequest struct {
	SchemaVersion  string `json:"schema_version"`
	OperationID    string `json:"operation_id"`
	InstallationID string `json:"installation_id"`
	Action         string `json:"action"`
}

type ControllerResponse struct {
	SchemaVersion  string `json:"schema_version"`
	OperationID    string `json:"operation_id"`
	InstallationID string `json:"installation_id"`
	State          string `json:"state"`
	ReasonCode     string `json:"reason_code,omitempty"`
}

type InvitationControllerRequest struct {
	SchemaVersion          string    `json:"schema_version"`
	Action                 string    `json:"action"`
	ActorSubject           string    `json:"actor_subject"`
	DeploymentID           string    `json:"deployment_id"`
	XInsightOrigin         string    `json:"xinsight_origin"`
	UniRouterInstallOrigin string    `json:"unirouter_install_origin"`
	RequestedAt            time.Time `json:"requested_at"`
	ExpiresInSeconds       int64     `json:"expires_in_seconds"`
}

type InvitationControllerResponse struct {
	SchemaVersion   string            `json:"schema_version"`
	Action          string            `json:"action"`
	Invitation      InvitationPackage `json:"invitation"`
	InvitationToken string            `json:"invitation_token"`
	Installation    Installation      `json:"installation"`
}

type Controller interface {
	Execute(context.Context, ControllerRequest) (ControllerResponse, error)
}

type UnixController struct {
	socketPath string
	timeout    time.Duration
	dial       func(context.Context) (net.Conn, error)
}

func NewUnixController() *UnixController {
	dialer := net.Dialer{Timeout: 2 * time.Second}
	return &UnixController{
		socketPath: LocalControllerSocket,
		timeout:    2 * time.Second,
		dial: func(ctx context.Context) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", LocalControllerSocket)
		},
	}
}

func (client *UnixController) CreateInvitation(
	ctx context.Context,
	input CreateInvitationInput,
) (InvitationPackage, Installation, error) {
	ttl := input.TTL
	if ttl == 0 {
		ttl = DefaultInvitationTTL
	}
	input.Now = input.Now.UTC()
	request := InvitationControllerRequest{
		SchemaVersion:          LocalInvitationRequestSchema,
		Action:                 ControllerActionIssueInvite,
		ActorSubject:           input.ActorSubject,
		DeploymentID:           input.Config.DeploymentID,
		XInsightOrigin:         input.Config.XInsightOrigin,
		UniRouterInstallOrigin: input.Config.UniRouterInstallOrigin,
		RequestedAt:            input.Now,
		ExpiresInSeconds:       int64(ttl / time.Second),
	}
	if client == nil || client.socketPath != LocalControllerSocket || client.timeout <= 0 || client.dial == nil ||
		input.Random != nil || ttl%time.Second != 0 || ValidateInvitationControllerRequest(request) != nil {
		return InvitationPackage{}, Installation{}, ErrInvalid
	}
	payload, err := json.Marshal(request)
	if err != nil || len(payload) > maximumControllerMessageBytes-1 {
		return InvitationPackage{}, Installation{}, ErrInvalid
	}
	connection, err := client.dial(ctx)
	if err != nil {
		return InvitationPackage{}, Installation{}, fmt.Errorf("local connector controller is unavailable")
	}
	defer connection.Close()
	deadline := time.Now().Add(client.timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return InvitationPackage{}, Installation{}, fmt.Errorf("local connector controller deadline failed")
	}
	if _, err := connection.Write(append(payload, '\n')); err != nil {
		return InvitationPackage{}, Installation{}, fmt.Errorf("local connector controller request failed")
	}
	reader := bufio.NewReader(io.LimitReader(connection, maximumControllerMessageBytes+1))
	responsePayload, err := reader.ReadBytes('\n')
	if err != nil || len(responsePayload) == 0 || len(responsePayload) > maximumControllerMessageBytes {
		return InvitationPackage{}, Installation{}, fmt.Errorf("local connector controller response failed")
	}
	responsePayload = bytes.TrimSuffix(responsePayload, []byte{'\n'})
	var response InvitationControllerResponse
	decoder := json.NewDecoder(bytes.NewReader(responsePayload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return InvitationPackage{}, Installation{}, fmt.Errorf("local connector controller response is invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return InvitationPackage{}, Installation{}, fmt.Errorf("local connector controller response is invalid")
	}
	response.Invitation.Token = response.InvitationToken
	if ValidateInvitationControllerResponse(request, response) != nil {
		return InvitationPackage{}, Installation{}, fmt.Errorf("local connector controller response is invalid")
	}
	return response.Invitation, response.Installation, nil
}

func (client *UnixController) Execute(ctx context.Context, request ControllerRequest) (ControllerResponse, error) {
	if client == nil || client.socketPath != LocalControllerSocket || client.timeout <= 0 || client.dial == nil ||
		ValidateControllerRequest(request) != nil {
		return ControllerResponse{}, ErrInvalid
	}
	payload, err := json.Marshal(request)
	if err != nil || len(payload) > maximumControllerMessageBytes-1 {
		return ControllerResponse{}, ErrInvalid
	}
	connection, err := client.dial(ctx)
	if err != nil {
		return ControllerResponse{}, fmt.Errorf("local connector controller is unavailable")
	}
	defer connection.Close()
	deadline := time.Now().Add(client.timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return ControllerResponse{}, fmt.Errorf("local connector controller deadline failed")
	}
	if _, err := connection.Write(append(payload, '\n')); err != nil {
		return ControllerResponse{}, fmt.Errorf("local connector controller request failed")
	}
	reader := bufio.NewReader(io.LimitReader(connection, maximumControllerMessageBytes+1))
	responsePayload, err := reader.ReadBytes('\n')
	if err != nil || len(responsePayload) == 0 || len(responsePayload) > maximumControllerMessageBytes {
		return ControllerResponse{}, fmt.Errorf("local connector controller response failed")
	}
	responsePayload = bytes.TrimSuffix(responsePayload, []byte{'\n'})
	var response ControllerResponse
	decoder := json.NewDecoder(bytes.NewReader(responsePayload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return ControllerResponse{}, fmt.Errorf("local connector controller response is invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) ||
		ValidateControllerResponse(request, response) != nil {
		return ControllerResponse{}, fmt.Errorf("local connector controller response is invalid")
	}
	return response, nil
}

func ValidateControllerRequest(request ControllerRequest) error {
	if request.SchemaVersion != LocalControllerRequestSchema ||
		!operationIDPattern.MatchString(request.OperationID) ||
		!installationIDPattern.MatchString(request.InstallationID) {
		return ErrInvalid
	}
	if request.Action != ControllerActionInspect && request.Action != ControllerActionPrepare {
		return ErrInvalid
	}
	return nil
}

func ValidateControllerResponse(request ControllerRequest, response ControllerResponse) error {
	if ValidateControllerRequest(request) != nil ||
		response.SchemaVersion != LocalControllerResponseSchema ||
		response.OperationID != request.OperationID ||
		response.InstallationID != request.InstallationID {
		return ErrInvalid
	}
	switch response.State {
	case "ready":
		if response.ReasonCode != "" {
			return ErrInvalid
		}
	case "deferred":
		if response.ReasonCode != "controller_not_activated" {
			return ErrInvalid
		}
	case "abnormal":
		if response.ReasonCode != "local_state_unavailable" && response.ReasonCode != "local_state_drift" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func ValidateInvitationControllerRequest(request InvitationControllerRequest) error {
	config := InvitationConfig{
		DeploymentID:           request.DeploymentID,
		XInsightOrigin:         request.XInsightOrigin,
		UniRouterInstallOrigin: request.UniRouterInstallOrigin,
	}
	if request.SchemaVersion != LocalInvitationRequestSchema ||
		request.Action != ControllerActionIssueInvite ||
		!controllerHumanSubjectPattern.MatchString(request.ActorSubject) ||
		config.Validate() != nil || request.RequestedAt.IsZero() ||
		request.ExpiresInSeconds <= 0 ||
		request.ExpiresInSeconds > int64(MaximumInvitationTTL/time.Second) {
		return ErrInvalid
	}
	return nil
}

func ValidateInvitationControllerResponse(
	request InvitationControllerRequest,
	response InvitationControllerResponse,
) error {
	if ValidateInvitationControllerRequest(request) != nil ||
		response.SchemaVersion != LocalInvitationResponseSchema ||
		response.Action != ControllerActionIssueInvite ||
		ValidateInstallation(response.Installation) != nil ||
		response.Installation.DeploymentID != request.DeploymentID ||
		response.Installation.Status != "pending" ||
		response.Installation.DesiredState != "active" ||
		response.Installation.ObservedState != "awaiting_consent" ||
		!response.Installation.CreatedAt.Equal(request.RequestedAt) ||
		!response.Installation.UpdatedAt.Equal(request.RequestedAt) {
		return ErrInvalid
	}
	invitation := response.Invitation
	expectedInstallURL, err := BuildInstallURL(
		request.UniRouterInstallOrigin,
		response.InvitationToken,
		request.XInsightOrigin,
	)
	if err != nil || invitation.SchemaVersion != InvitationSchema ||
		!invitationIDPattern.MatchString(invitation.InvitationID) ||
		invitation.InstallationID != response.Installation.InstallationID ||
		invitation.DeploymentID != request.DeploymentID ||
		invitation.Audience != ConnectorAudience ||
		invitation.XInsightOrigin != request.XInsightOrigin ||
		!equalEventScope(invitation.RequestedEventScope, []string{CompletedEventType}) ||
		!invitation.ExpiresAt.Equal(request.RequestedAt.Add(time.Duration(request.ExpiresInSeconds)*time.Second)) ||
		invitation.InstallURL != expectedInstallURL || invitation.Token != response.InvitationToken {
		return ErrInvalid
	}
	return nil
}
