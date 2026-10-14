package connectoractivation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connectorcontrol"
)

func TestUnixServerServesOnlyFixedTypedActivationAndInvitationMessages(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 11, 8, 0, 0, 0, time.UTC)
	issuer := &serverInvitationIssuerStub{}
	controller := &serverControllerStub{}
	server, socketPath := newTestUnixServer(t, issuer, controller, func(*net.UnixConn) error { return nil })
	stop := startTestUnixServer(t, server, socketPath)
	defer stop()

	activationRequest := connectorcontrol.ControllerRequest{
		SchemaVersion:  connectorcontrol.LocalControllerRequestSchema,
		OperationID:    "xco_AAAAAAAAAAAAAAAAAAAAAA",
		InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA",
		Action:         connectorcontrol.ControllerActionPrepare,
	}
	var activationResponse connectorcontrol.ControllerResponse
	roundTripControllerMessage(t, socketPath, activationRequest, &activationResponse)
	if connectorcontrol.ValidateControllerResponse(activationRequest, activationResponse) != nil ||
		activationResponse.State != "ready" {
		t.Fatalf("activation response=%+v", activationResponse)
	}

	invitationRequest := connectorcontrol.InvitationControllerRequest{
		SchemaVersion:          connectorcontrol.LocalInvitationRequestSchema,
		Action:                 connectorcontrol.ControllerActionIssueInvite,
		ActorSubject:           "xs_0123456789abcdef",
		DeploymentID:           "xid_AAAAAAAAAAAAAAAAAAAAAA",
		XInsightOrigin:         "https://xinsight.example",
		UniRouterInstallOrigin: "https://unirouter.example",
		RequestedAt:            now,
		ExpiresInSeconds:       int64(connectorcontrol.DefaultInvitationTTL / time.Second),
	}
	var invitationResponse connectorcontrol.InvitationControllerResponse
	roundTripControllerMessage(t, socketPath, invitationRequest, &invitationResponse)
	invitationResponse.Invitation.Token = invitationResponse.InvitationToken
	if connectorcontrol.ValidateInvitationControllerResponse(invitationRequest, invitationResponse) != nil {
		t.Fatalf("invitation response=%+v", invitationResponse)
	}
	if controller.callCount() != 1 || issuer.callCount() != 1 {
		t.Fatalf("controller calls=%d issuer calls=%d", controller.callCount(), issuer.callCount())
	}
	info, err := os.Lstat(socketPath)
	if err != nil || info.Mode().Perm() != 0o660 || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("socket info=%v err=%v", info, err)
	}
}

func TestUnixServerRejectsUnauthorizedPeerBeforeReadingOrDispatch(t *testing.T) {
	t.Parallel()
	issuer := &serverInvitationIssuerStub{}
	controller := &serverControllerStub{}
	server, socketPath := newTestUnixServer(
		t, issuer, controller,
		func(*net.UnixConn) error { return errors.New("unauthorized") },
	)
	stop := startTestUnixServer(t, server, socketPath)
	defer stop()

	connection, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	request := `{"schema_version":"xinsight.local-connector-controller-request.v1","operation_id":"xco_AAAAAAAAAAAAAAAAAAAAAA","installation_id":"xci_AAAAAAAAAAAAAAAAAAAAAA","action":"prepare_installation"}` + "\n"
	// Authorization happens before request I/O, so either a successful local
	// buffer write or an immediate broken pipe is a valid rejection outcome.
	_, _ = connection.Write([]byte(request))
	_ = connection.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := bufio.NewReader(connection).ReadByte(); err == nil {
		t.Fatal("unauthorized peer received a response")
	}
	if controller.callCount() != 0 || issuer.callCount() != 0 {
		t.Fatalf("unauthorized dispatch controller=%d issuer=%d", controller.callCount(), issuer.callCount())
	}
}

func TestUnixServerControllerErrorClosesConnectionForRetriableWakeFailure(t *testing.T) {
	t.Parallel()
	issuer := &serverInvitationIssuerStub{}
	controller := &serverControllerStub{executeError: connectorcontrol.ErrConflict}
	server, socketPath := newTestUnixServer(t, issuer, controller, func(*net.UnixConn) error { return nil })
	stop := startTestUnixServer(t, server, socketPath)
	defer stop()

	connection, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(time.Second))
	request := connectorcontrol.ControllerRequest{
		SchemaVersion: connectorcontrol.LocalControllerRequestSchema,
		OperationID:   "xco_AAAAAAAAAAAAAAAAAAAAAA", InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA",
		Action: connectorcontrol.ControllerActionPrepare,
	}
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		t.Fatal(err)
	}
	if _, err := bufio.NewReader(connection).ReadByte(); err == nil {
		t.Fatal("controller error was converted into a successful typed response")
	}
}

func TestUnixServerExecutionContextOutlivesSocketIODeadline(t *testing.T) {
	t.Parallel()
	issuer := &serverInvitationIssuerStub{}
	executeStarted := make(chan struct{})
	releaseExecution := make(chan struct{})
	controller := &serverControllerStub{execute: func(ctx context.Context) error {
		close(executeStarted)
		select {
		case <-releaseExecution:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	server, socketPath := newTestUnixServer(t, issuer, controller, func(*net.UnixConn) error { return nil })
	server.timeout = 10 * time.Millisecond
	server.executionTimeout = time.Second
	stop := startTestUnixServer(t, server, socketPath)
	defer stop()

	connection, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	request := connectorcontrol.ControllerRequest{
		SchemaVersion: connectorcontrol.LocalControllerRequestSchema,
		OperationID:   "xco_AAAAAAAAAAAAAAAAAAAAAA", InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA",
		Action: connectorcontrol.ControllerActionPrepare,
	}
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		t.Fatal(err)
	}
	<-executeStarted
	time.Sleep(30 * time.Millisecond)
	close(releaseExecution)
	deadline := time.Now().Add(time.Second)
	for controller.completedCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if controller.completedCount() != 1 {
		t.Fatal("controller execution did not complete")
	}
	if controller.lastError() != nil {
		t.Fatalf("execution was cancelled by socket I/O deadline: %v", controller.lastError())
	}
}

func TestUnixServerGracefulCloseUnlinksSocketAndAllowsRestart(t *testing.T) {
	t.Parallel()
	server, socketPath := newTestUnixServer(
		t, &serverInvitationIssuerStub{}, &serverControllerStub{},
		func(*net.UnixConn) error { return nil },
	)
	stop := startTestUnixServer(t, server, socketPath)
	stop()
	if _, err := os.Lstat(socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket remained after graceful close: %v", err)
	}
	stop = startTestUnixServer(t, server, socketPath)
	stop()
}

func TestUnixServerRejectsDuplicateUnknownTrailingOversizeAndArbitraryInputs(t *testing.T) {
	t.Parallel()
	issuer := &serverInvitationIssuerStub{}
	controller := &serverControllerStub{}
	server, _ := newTestUnixServer(t, issuer, controller, func(*net.UnixConn) error { return nil })
	valid := `{"schema_version":"xinsight.local-connector-controller-request.v1","operation_id":"xco_AAAAAAAAAAAAAAAAAAAAAA","installation_id":"xci_AAAAAAAAAAAAAAAAAAAAAA","action":"prepare_installation"}`
	cases := map[string]string{
		"duplicate": strings.Replace(valid, `"action":"prepare_installation"`, `"action":"prepare_installation","action":"inspect_installation"`, 1),
		"unknown":   strings.Replace(valid, `"action":"prepare_installation"`, `"action":"prepare_installation","path":"/tmp/command"`, 1),
		"trailing":  valid + ` {}`,
		"arbitrary": strings.Replace(valid, "prepare_installation", "run_shell", 1),
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := server.dispatch(context.Background(), []byte(payload)); err == nil {
				t.Fatal("invalid local controller input was accepted")
			}
		})
	}
	if _, err := readControllerMessage(bytes.NewBufferString(
		strings.Repeat("x", maximumLocalControllerMsgBytes) + "\n",
	)); err == nil {
		t.Fatal("oversize local controller input was accepted")
	}
	if _, err := readControllerMessage(bytes.NewBufferString(valid + "\n" + valid + "\n")); err == nil {
		t.Fatal("a second local controller message was accepted on one connection")
	}
	if controller.callCount() != 0 || issuer.callCount() != 0 {
		t.Fatalf("invalid input dispatched controller=%d issuer=%d", controller.callCount(), issuer.callCount())
	}
}

func TestNewUnixServerPinsSocketAndRejectsMissingManagedIdentity(t *testing.T) {
	t.Parallel()
	issuer := &serverInvitationIssuerStub{}
	controller := &serverControllerStub{}
	if _, err := NewUnixServer(issuer, controller, 0, 1000); err == nil {
		t.Fatal("zero managed UID was accepted")
	}
	server, err := NewUnixServer(issuer, controller, 1000, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if server.socketPath != connectorcontrol.LocalControllerSocket {
		t.Fatalf("socket path=%q", server.socketPath)
	}
	if validateLocalControllerPeerUID(0, 1000) != nil ||
		validateLocalControllerPeerUID(1000, 1000) != nil ||
		validateLocalControllerPeerUID(1001, 1000) == nil {
		t.Fatal("root-or-managed peer policy is not fail closed")
	}
}

type serverManagedACKStub struct{ installation connectorcontrol.Installation }

func (s serverManagedACKStub) AcknowledgeManaged(context.Context, string, connectorcontrol.ControlResultV3) (connectorcontrol.Installation, error) {
	return s.installation, nil
}

func TestUnixServerManagedACKPrivateReceiptRoundTrip(t *testing.T) {
	raw, err := os.ReadFile("../connectorcontrol/testdata/control-v3/valid/06-policy-pair-result.json")
	if err != nil {
		t.Fatal(err)
	}
	var result connectorcontrol.ControlResultV3
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	result.ObservedAt = now
	const identity = "xsi_AAAAAAAAAAAAAAAAAAAAAA"
	i := connectorcontrol.Installation{SchemaVersion: connectorcontrol.InstallationSchema, InstallationID: result.InstallationID, DeploymentID: "xid_AAAAAAAAAAAAAAAAAAAAAA", ConnectorType: connectorcontrol.ConnectorTypeUniRouter, RequestedEventScope: []string{connectorcontrol.CompletedEventType}, FinalEventScope: []string{connectorcontrol.CompletedEventType}, ProjectID: "project.fixture", SourceInstance: result.Result.SourceInstance, SourceInstanceSHA256: connectorcontrol.RawSHA256(result.Result.SourceInstance), ControlServiceIdentityID: identity, InstallationAcknowledgementSHA256: "sha256:" + strings.Repeat("a", 64), Status: "connected", DesiredState: "active", ObservedState: "connected", CreatedAt: now, UpdatedAt: now, Version: 7}
	server, socket := newTestUnixServer(t, &serverInvitationIssuerStub{}, &serverControllerStub{}, func(*net.UnixConn) error { return nil })
	if err = server.SetManagedAcknowledger(serverManagedACKStub{i}); err != nil {
		t.Fatal(err)
	}
	stop := startTestUnixServer(t, server, socket)
	defer stop()
	var response connectorcontrol.ManagedACKResponse
	roundTripControllerMessage(t, socket, connectorcontrol.ManagedACKRequest{SchemaVersion: connectorcontrol.LocalManagedACKSchema, ServiceIdentityID: identity, Result: result}, &response)
	if response.Installation == nil || response.Error != "" || response.ControlServiceIdentityID != identity || response.SourceInstance != i.SourceInstance {
		t.Fatal("private local receipt lost authority")
	}
	rehydrated := *response.Installation
	rehydrated.ControlServiceIdentityID = response.ControlServiceIdentityID
	rehydrated.SourceInstance = response.SourceInstance
	if connectorcontrol.ValidateInstallation(rehydrated) != nil {
		t.Fatal("actual server wire does not preserve complete installation authority")
	}
	public, e := json.Marshal(rehydrated)
	if e != nil || strings.Contains(string(public), "control_service_identity_id") || strings.Contains(string(public), `"source_instance":`) {
		t.Fatal("public installation leaked private receipt")
	}
}

func newTestUnixServer(
	t *testing.T,
	issuer connectorcontrol.InvitationIssuer,
	controller connectorcontrol.Controller,
	authorize peerAuthorizer,
) (*UnixServer, string) {
	t.Helper()
	placeholder, err := os.CreateTemp(os.TempDir(), "xinsight-controller-*.sock")
	if err != nil {
		t.Fatal(err)
	}
	path := placeholder.Name()
	if err := placeholder.Close(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("temporary socket placeholder info=%v err=%v", info, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	return &UnixServer{
		issuer: issuer, controller: controller, socketPath: path,
		timeout: time.Second, executionTimeout: time.Second, authorizePeer: authorize,
		configureSocket: func(path string) error { return os.Chmod(path, 0o660) },
	}, path
}

func startTestUnixServer(t *testing.T, server *UnixServer, socketPath string) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	deadline := time.Now().Add(time.Second)
	for {
		select {
		case err := <-done:
			cancel()
			t.Fatalf("local controller stopped before creating socket: %v", err)
		default:
		}
		if info, err := os.Lstat(socketPath); err == nil && info.Mode()&os.ModeSocket != 0 && info.Mode().Perm() == 0660 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("local controller socket was not created")
		}
		time.Sleep(time.Millisecond)
	}
	return func() {
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("serve: %v", err)
		}
	}
}

func roundTripControllerMessage(t *testing.T, socketPath string, request any, response any) {
	t.Helper()
	connection, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(time.Second))
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bufio.NewReader(connection))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(response); err != nil {
		t.Fatal(err)
	}
}

type serverControllerStub struct {
	mu           sync.Mutex
	calls        int
	execute      func(context.Context) error
	executeError error
	observedErr  error
	completed    int
}

func (stub *serverControllerStub) Execute(
	ctx context.Context,
	request connectorcontrol.ControllerRequest,
) (connectorcontrol.ControllerResponse, error) {
	stub.mu.Lock()
	stub.calls++
	execute := stub.execute
	executeError := stub.executeError
	stub.mu.Unlock()
	if execute != nil {
		executeError = execute(ctx)
	}
	stub.mu.Lock()
	stub.observedErr = executeError
	stub.completed++
	stub.mu.Unlock()
	if executeError != nil {
		return connectorcontrol.ControllerResponse{}, executeError
	}
	return connectorcontrol.ControllerResponse{
		SchemaVersion: connectorcontrol.LocalControllerResponseSchema,
		OperationID:   request.OperationID, InstallationID: request.InstallationID,
		State: "ready",
	}, nil
}

func (stub *serverControllerStub) callCount() int {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return stub.calls
}

func (stub *serverControllerStub) lastError() error {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return stub.observedErr
}

func (stub *serverControllerStub) completedCount() int {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return stub.completed
}

type serverInvitationIssuerStub struct {
	mu    sync.Mutex
	calls int
}

func (stub *serverInvitationIssuerStub) CreateInvitation(
	_ context.Context,
	input connectorcontrol.CreateInvitationInput,
) (connectorcontrol.InvitationPackage, connectorcontrol.Installation, error) {
	stub.mu.Lock()
	stub.calls++
	stub.mu.Unlock()
	token := "xiiv1_" + strings.Repeat("A", 43)
	installURL, err := connectorcontrol.BuildInstallURL(
		input.Config.UniRouterInstallOrigin, token, input.Config.XInsightOrigin,
	)
	if err != nil {
		return connectorcontrol.InvitationPackage{}, connectorcontrol.Installation{}, err
	}
	installation := connectorcontrol.Installation{
		SchemaVersion:  connectorcontrol.InstallationSchema,
		InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA", DeploymentID: input.Config.DeploymentID,
		ConnectorType:       connectorcontrol.ConnectorTypeUniRouter,
		RequestedEventScope: []string{connectorcontrol.CompletedEventType}, Status: "pending",
		DesiredState: "active", ObservedState: "awaiting_consent",
		CreatedAt: input.Now, UpdatedAt: input.Now, Version: 1,
	}
	return connectorcontrol.InvitationPackage{
		SchemaVersion: connectorcontrol.InvitationSchema,
		InvitationID:  "xii_AAAAAAAAAAAAAAAAAAAAAA", InstallationID: installation.InstallationID,
		DeploymentID: input.Config.DeploymentID, Audience: connectorcontrol.ConnectorAudience,
		XInsightOrigin:      input.Config.XInsightOrigin,
		RequestedEventScope: []string{connectorcontrol.CompletedEventType},
		ExpiresAt:           input.Now.Add(input.TTL), InstallURL: installURL, Token: token,
	}, installation, nil
}

func (stub *serverInvitationIssuerStub) callCount() int {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return stub.calls
}
