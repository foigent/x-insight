package connectorcontrol

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestUnixControllerUsesOnlyFixedTypedSocketMessages(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer serverSide.Close()
	client := &UnixController{
		socketPath: LocalControllerSocket,
		timeout:    time.Second,
		dial:       func(context.Context) (net.Conn, error) { return clientSide, nil },
	}
	request := ControllerRequest{
		SchemaVersion:  LocalControllerRequestSchema,
		OperationID:    "xco_AAAAAAAAAAAAAAAAAAAAAA",
		InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA",
		Action:         ControllerActionPrepare,
	}
	done := make(chan error, 1)
	go func() {
		defer close(done)
		var received ControllerRequest
		if err := json.NewDecoder(bufio.NewReader(serverSide)).Decode(&received); err != nil {
			done <- err
			return
		}
		if received != request {
			done <- ErrConflict
			return
		}
		response := ControllerResponse{
			SchemaVersion: LocalControllerResponseSchema,
			OperationID:   request.OperationID, InstallationID: request.InstallationID,
			State: "deferred", ReasonCode: "controller_not_activated",
		}
		done <- json.NewEncoder(serverSide).Encode(response)
	}()
	response, err := client.Execute(context.Background(), request)
	if err != nil || response.State != "deferred" {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestUnixControllerRejectsArbitraryActionsBeforeDial(t *testing.T) {
	dials := 0
	client := &UnixController{
		socketPath: LocalControllerSocket,
		timeout:    time.Second,
		dial:       func(context.Context) (net.Conn, error) { dials++; return nil, nil },
	}
	request := ControllerRequest{
		SchemaVersion:  LocalControllerRequestSchema,
		OperationID:    "xco_AAAAAAAAAAAAAAAAAAAAAA",
		InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA",
		Action:         "run_shell",
	}
	if _, err := client.Execute(context.Background(), request); err == nil || dials != 0 {
		t.Fatalf("arbitrary action err=%v dials=%d", err, dials)
	}
}

func TestUnixControllerIssuesInvitationOnlyThroughFixedTypedMessage(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer serverSide.Close()
	client := &UnixController{
		socketPath: LocalControllerSocket,
		timeout:    time.Second,
		dial:       func(context.Context) (net.Conn, error) { return clientSide, nil },
	}
	now := time.Date(2026, 8, 11, 4, 0, 0, 0, time.UTC)
	token := "xiiv1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	installURL, err := BuildInstallURL("https://unirouter.example", token, "https://xinsight.example")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		defer close(done)
		var received InvitationControllerRequest
		if err := json.NewDecoder(bufio.NewReader(serverSide)).Decode(&received); err != nil {
			done <- err
			return
		}
		if err := ValidateInvitationControllerRequest(received); err != nil ||
			received.ActorSubject != "xs_0123456789abcdef" {
			done <- ErrConflict
			return
		}
		installation := Installation{
			SchemaVersion: InstallationSchema, InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA",
			DeploymentID: received.DeploymentID, ConnectorType: ConnectorTypeUniRouter,
			RequestedEventScope: []string{CompletedEventType}, Status: "pending",
			DesiredState: "active", ObservedState: "awaiting_consent",
			CreatedAt: received.RequestedAt, UpdatedAt: received.RequestedAt, Version: 1,
		}
		response := InvitationControllerResponse{
			SchemaVersion: LocalInvitationResponseSchema, Action: ControllerActionIssueInvite,
			Invitation: InvitationPackage{
				SchemaVersion: InvitationSchema, InvitationID: "xii_AAAAAAAAAAAAAAAAAAAAAA",
				InstallationID: installation.InstallationID, DeploymentID: received.DeploymentID,
				Audience: ConnectorAudience, XInsightOrigin: received.XInsightOrigin,
				RequestedEventScope: []string{CompletedEventType},
				ExpiresAt:           received.RequestedAt.Add(time.Duration(received.ExpiresInSeconds) * time.Second),
				InstallURL:          installURL,
			},
			InvitationToken: token,
			Installation:    installation,
		}
		done <- json.NewEncoder(serverSide).Encode(response)
	}()
	invitation, installation, err := client.CreateInvitation(context.Background(), CreateInvitationInput{
		ActorSubject: "xs_0123456789abcdef",
		Config: InvitationConfig{
			DeploymentID:           "xid_AAAAAAAAAAAAAAAAAAAAAA",
			XInsightOrigin:         "https://xinsight.example",
			UniRouterInstallOrigin: "https://unirouter.example",
		},
		Now: now,
	})
	if err != nil || invitation.Token != token ||
		installation.InstallationID != invitation.InstallationID {
		t.Fatalf("invitation=%+v installation=%+v err=%v", invitation, installation, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
