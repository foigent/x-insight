package connectoractivation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/reviewpolicycontrol"
	"github.com/foigent/x-insight/internal/workinstallcontrol"
)

const (
	localControllerIOTimeout        = 2 * time.Second
	localControllerExecutionTimeout = 30 * time.Second
	maximumLocalControllerMsgBytes  = 16 << 10
)

type peerAuthorizer func(*net.UnixConn) error
type socketConfigurator func(string) error

// UnixServer is the root-owned local privilege boundary for Connector
// installation. It accepts only the fixed invitation and activation messages
// defined in connectorcontrol. Activation accepts no path, unit, SQL, shell or
// URL; invitation origins remain the canonical validated CP1 product fields.
type UnixServer struct {
	issuer             connectorcontrol.InvitationIssuer
	controller         connectorcontrol.Controller
	policyManager      *reviewpolicycontrol.Manager
	workInstallManager *workinstallcontrol.Manager
	managedACK         connectorcontrol.ManagedAcknowledger
	setupController    connectorcontrol.SetupController
	socketPath         string
	timeout            time.Duration
	executionTimeout   time.Duration
	authorizePeer      peerAuthorizer
	configureSocket    socketConfigurator
}

func (server *UnixServer) SetSetupController(controller connectorcontrol.SetupController) error {
	if server == nil || controller == nil || server.setupController != nil {
		return connectorcontrol.ErrInvalid
	}
	server.setupController = controller
	return nil
}

func (server *UnixServer) SetManagedAcknowledger(manager connectorcontrol.ManagedAcknowledger) error {
	if server == nil || manager == nil || server.managedACK != nil {
		return connectorcontrol.ErrInvalid
	}
	server.managedACK = manager
	return nil
}

func (server *UnixServer) SetWorkInstallManager(manager workinstallcontrol.Manager) error {
	if server == nil || manager.Store == nil || server.workInstallManager != nil {
		return fmt.Errorf("work installation controller is invalid")
	}
	server.workInstallManager = &manager
	return nil
}

func NewUnixServer(
	issuer connectorcontrol.InvitationIssuer,
	controller connectorcontrol.Controller,
	managedUID uint32,
	managedGID uint32,
	policyManager ...reviewpolicycontrol.Manager,
) (*UnixServer, error) {
	if issuer == nil || controller == nil || managedUID == 0 || managedGID == 0 {
		return nil, fmt.Errorf("local connector controller dependencies are required")
	}
	server := &UnixServer{
		issuer:           issuer,
		controller:       controller,
		socketPath:       connectorcontrol.LocalControllerSocket,
		timeout:          localControllerIOTimeout,
		executionTimeout: localControllerExecutionTimeout,
		authorizePeer:    rootOrManagedPeer(managedUID),
		configureSocket: func(path string) error {
			return configureRootManagedSocket(path, managedGID)
		},
	}
	if len(policyManager) > 1 {
		return nil, fmt.Errorf("one review policy controller is permitted")
	}
	if len(policyManager) == 1 {
		server.policyManager = &policyManager[0]
	}
	return server, nil
}

// Serve creates the one compiled Unix socket and serves one bounded request
// per connection until the supplied context is cancelled.
func (server *UnixServer) Serve(ctx context.Context) error {
	if server == nil || server.issuer == nil || server.controller == nil ||
		server.socketPath == "" || server.timeout <= 0 || server.executionTimeout <= 0 || server.authorizePeer == nil ||
		server.configureSocket == nil {
		return fmt.Errorf("local connector controller is not initialized")
	}
	if ctx == nil {
		return fmt.Errorf("local connector controller context is required")
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: server.socketPath, Net: "unix"})
	if err != nil {
		return fmt.Errorf("listen on local connector controller socket: %w", err)
	}
	defer listener.Close()
	if err := server.configureSocket(server.socketPath); err != nil {
		return fmt.Errorf("secure local connector controller socket: %w", err)
	}

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()
		case <-stop:
		}
	}()

	for {
		connection, err := listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept local connector controller connection: %w", err)
		}
		server.handleConnection(ctx, connection)
	}
}

func (server *UnixServer) handleConnection(parent context.Context, connection *net.UnixConn) {
	defer connection.Close()
	readDeadline := time.Now().Add(server.timeout)
	if parentDeadline, ok := parent.Deadline(); ok && parentDeadline.Before(readDeadline) {
		readDeadline = parentDeadline
	}
	if err := connection.SetReadDeadline(readDeadline); err != nil || server.authorizePeer(connection) != nil {
		return
	}
	requestPayload, err := readControllerMessage(connection)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, server.executionTimeout)
	defer cancel()
	response, err := server.dispatch(ctx, requestPayload)
	if err != nil {
		return
	}
	responsePayload, err := json.Marshal(response)
	responseLimit := maximumLocalControllerMsgBytes
	if policyResponse, ok := response.(reviewpolicycontrol.Response); ok && policyResponse.UR != nil {
		responseLimit = 64 << 10
	}
	if _, ok := response.(workinstallcontrol.Response); ok {
		responseLimit = 256 << 10
	}
	if err != nil || len(responsePayload) > responseLimit-1 {
		return
	}
	writeDeadline := time.Now().Add(server.timeout)
	if parentDeadline, ok := parent.Deadline(); ok && parentDeadline.Before(writeDeadline) {
		writeDeadline = parentDeadline
	}
	if err := connection.SetWriteDeadline(writeDeadline); err != nil {
		return
	}
	_, _ = connection.Write(append(responsePayload, '\n'))
}

func (server *UnixServer) dispatch(ctx context.Context, payload []byte) (any, error) {
	if err := rejectDuplicateControllerJSONFields(payload); err != nil {
		return nil, err
	}
	var envelope struct {
		SchemaVersion string `json:"schema_version"`
		Action        string `json:"action"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, connectorcontrol.ErrInvalid
	}
	switch envelope.SchemaVersion {
	case connectorcontrol.LocalSetupRequestSchema:
		var request connectorcontrol.SetupLocalRequest
		if server.setupController == nil || strictControllerDecode(payload, &request) != nil || !request.Valid() {
			return nil, connectorcontrol.ErrInvalid
		}
		response, err := server.setupController.ExecuteSetup(ctx, request)
		if err != nil {
			response = connectorcontrol.SetupLocalResponse{SchemaVersion: connectorcontrol.LocalSetupResponseSchema, Action: request.Action}
			switch {
			case errors.Is(err, connectorcontrol.ErrInvalid):
				response.Error = "invalid"
			case errors.Is(err, connectorcontrol.ErrNotFound):
				response.Error = "not_found"
			case errors.Is(err, connectorcontrol.ErrExpired):
				response.Error = "expired"
			case errors.Is(err, connectorcontrol.ErrConflict):
				response.Error = "conflict"
			case errors.Is(err, connectorcontrol.ErrSetupUnauthorized):
				response.Error = "unauthorized"
			default:
				response.Error = "unavailable"
			}
		}
		if !connectorcontrol.ValidSetupResponse(request, response) {
			return nil, connectorcontrol.ErrInvalid
		}
		return response, nil
	case connectorcontrol.LocalManagedACKSchema:
		var request connectorcontrol.ManagedACKRequest
		if server.managedACK == nil || strictControllerDecode(payload, &request) != nil || connectorcontrol.ValidateControlResultV3(request.Result, time.Now().UTC()) != nil {
			return nil, connectorcontrol.ErrInvalid
		}
		installation, err := server.managedACK.AcknowledgeManaged(ctx, request.ServiceIdentityID, request.Result)
		response := connectorcontrol.ManagedACKResponse{SchemaVersion: connectorcontrol.LocalManagedACKSchema}
		if err != nil {
			switch {
			case errors.Is(err, connectorcontrol.ErrInvalid):
				response.Error = "invalid"
			case errors.Is(err, connectorcontrol.ErrNotFound):
				response.Error = "not_found"
			case errors.Is(err, connectorcontrol.ErrConflict):
				response.Error = "conflict"
			default:
				response.Error = "unavailable"
			}
		} else {
			response.Installation = &installation
			response.ControlServiceIdentityID = installation.ControlServiceIdentityID
			response.SourceInstance = installation.SourceInstance
		}
		return response, nil
	case workinstallcontrol.RequestSchema:
		if server.workInstallManager == nil {
			return nil, connectorcontrol.ErrInvalid
		}
		var request workinstallcontrol.Request
		if strictControllerDecode(payload, &request) != nil || !workinstallcontrol.ValidRequest(request) {
			return nil, connectorcontrol.ErrInvalid
		}
		return server.workInstallManager.Execute(ctx, request)
	case reviewpolicycontrol.RequestSchema:
		if server.policyManager == nil {
			return nil, connectorcontrol.ErrInvalid
		}
		var request reviewpolicycontrol.Request
		if strictControllerDecode(payload, &request) != nil || !reviewpolicycontrol.ValidRequest(request) {
			return nil, connectorcontrol.ErrInvalid
		}
		return server.policyManager.Execute(ctx, request)
	case connectorcontrol.LocalInvitationRequestSchema:
		var request connectorcontrol.InvitationControllerRequest
		if strictControllerDecode(payload, &request) != nil ||
			connectorcontrol.ValidateInvitationControllerRequest(request) != nil {
			return nil, connectorcontrol.ErrInvalid
		}
		invitation, installation, err := server.issuer.CreateInvitation(
			ctx,
			connectorcontrol.CreateInvitationInput{
				ActorSubject: request.ActorSubject,
				Config: connectorcontrol.InvitationConfig{
					DeploymentID:           request.DeploymentID,
					XInsightOrigin:         request.XInsightOrigin,
					UniRouterInstallOrigin: request.UniRouterInstallOrigin,
				},
				Now: request.RequestedAt.UTC(),
				TTL: time.Duration(request.ExpiresInSeconds) * time.Second,
			},
		)
		if err != nil {
			return nil, err
		}
		response := connectorcontrol.InvitationControllerResponse{
			SchemaVersion:   connectorcontrol.LocalInvitationResponseSchema,
			Action:          connectorcontrol.ControllerActionIssueInvite,
			Invitation:      invitation,
			InvitationToken: invitation.Token,
			Installation:    installation,
		}
		if connectorcontrol.ValidateInvitationControllerResponse(request, response) != nil {
			return nil, connectorcontrol.ErrConflict
		}
		return response, nil
	case connectorcontrol.LocalControllerRequestSchema:
		var request connectorcontrol.ControllerRequest
		if strictControllerDecode(payload, &request) != nil ||
			connectorcontrol.ValidateControllerRequest(request) != nil {
			return nil, connectorcontrol.ErrInvalid
		}
		response, err := server.controller.Execute(ctx, request)
		if err != nil {
			return nil, err
		}
		return response, nil
	default:
		return nil, connectorcontrol.ErrInvalid
	}
}

func readControllerMessage(reader io.Reader) ([]byte, error) {
	bounded := bufio.NewReader(io.LimitReader(
		reader,
		int64(maximumLocalControllerMsgBytes+1),
	))
	payload, err := bounded.ReadBytes('\n')
	if err != nil || len(payload) < 2 || len(payload) > maximumLocalControllerMsgBytes {
		return nil, connectorcontrol.ErrInvalid
	}
	if bounded.Buffered() != 0 {
		return nil, connectorcontrol.ErrInvalid
	}
	payload = bytes.TrimSuffix(payload, []byte{'\n'})
	if len(payload) == 0 {
		return nil, connectorcontrol.ErrInvalid
	}
	return payload, nil
}

func strictControllerDecode(payload []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return connectorcontrol.ErrInvalid
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return connectorcontrol.ErrInvalid
	}
	return nil
}

func rejectDuplicateControllerJSONFields(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	var scan func() error
	scan = func() error {
		token, err := decoder.Token()
		if err != nil {
			return connectorcontrol.ErrInvalid
		}
		delimiter, composite := token.(json.Delim)
		if !composite {
			return nil
		}
		switch delimiter {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				key, ok := keyToken.(string)
				if err != nil || !ok {
					return connectorcontrol.ErrInvalid
				}
				if _, duplicate := seen[key]; duplicate {
					return connectorcontrol.ErrInvalid
				}
				seen[key] = struct{}{}
				if err := scan(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := scan(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		default:
			return connectorcontrol.ErrInvalid
		}
	}
	if err := scan(); err != nil {
		return connectorcontrol.ErrInvalid
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return connectorcontrol.ErrInvalid
	}
	return nil
}

func rootOrManagedPeer(managedUID uint32) peerAuthorizer {
	return func(connection *net.UnixConn) error {
		uid, err := unixPeerUID(connection)
		if err != nil || validateLocalControllerPeerUID(uid, managedUID) != nil {
			return fmt.Errorf("local connector controller peer is not authorized")
		}
		return nil
	}
}

func validateLocalControllerPeerUID(uid uint32, managedUID uint32) error {
	if managedUID == 0 || (uid != 0 && uid != managedUID) {
		return connectorcontrol.ErrInvalid
	}
	return nil
}

func configureRootManagedSocket(path string, managedGID uint32) error {
	before, err := os.Lstat(path)
	if err != nil || before.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("local connector controller path is not a socket")
	}
	if err := os.Chown(path, 0, int(managedGID)); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o660); err != nil {
		return err
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) || after.Mode()&os.ModeSocket == 0 ||
		after.Mode().Perm() != 0o660 {
		return fmt.Errorf("local connector controller socket metadata readback failed")
	}
	stat, ok := after.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != managedGID {
		return fmt.Errorf("local connector controller socket ownership readback failed")
	}
	return nil
}
