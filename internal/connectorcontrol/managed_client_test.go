package connectorcontrol

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestManagedACKUnixWirePreservesPrivateInstallationAuthority(t *testing.T) {
	raw, err := os.ReadFile("testdata/control-v3/valid/06-policy-pair-result.json")
	if err != nil {
		t.Fatal(err)
	}
	var result ControlResultV3
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	const identity = "xsi_AAAAAAAAAAAAAAAAAAAAAA"
	now := time.Now().UTC().Truncate(time.Microsecond)
	result.ObservedAt = now
	resultSHA, _, err := CanonicalSHA256(result)
	if err != nil {
		t.Fatal(err)
	}
	installation := Installation{SchemaVersion: InstallationSchema, InstallationID: result.InstallationID, DeploymentID: "xid_AAAAAAAAAAAAAAAAAAAAAA", ConnectorType: ConnectorTypeUniRouter, RequestedEventScope: []string{CompletedEventType}, FinalEventScope: []string{CompletedEventType}, ProjectID: "project.fixture", SourceInstance: result.Result.SourceInstance, SourceInstanceSHA256: RawSHA256(result.Result.SourceInstance), ControlServiceIdentityID: identity, InstallationAcknowledgementSHA256: "sha256:" + strings.Repeat("a", 64), Status: "connected", DesiredState: "active", ObservedState: "connected", CreatedAt: now, UpdatedAt: now, Version: 7}
	if ValidateInstallation(installation) != nil {
		t.Fatal("invalid fixture")
	}
	public, err := json.Marshal(installation)
	if err != nil || strings.Contains(string(public), "control_service_identity_id") || strings.Contains(string(public), `"source_instance":`) {
		t.Fatal("private authority exposed in public Installation")
	}
	for _, scenario := range []string{"valid", "wrong-installation", "wrong-control-identity", "wrong-source-hash", "missing-private-authority", "error-with-installation"} {
		t.Run(scenario, func(t *testing.T) {
			i := installation
			response := ManagedACKResponse{SchemaVersion: LocalManagedACKSchema, Installation: &i, ControlServiceIdentityID: identity, SourceInstance: i.SourceInstance}
			switch scenario {
			case "wrong-installation":
				i.InstallationID = "xci_BBBBBBBBBBBBBBBBBBBBBB"
			case "wrong-control-identity":
				response.ControlServiceIdentityID = "xsi_BBBBBBBBBBBBBBBBBBBBBB"
			case "wrong-source-hash":
				i.SourceInstanceSHA256 = RawSHA256("different")
			case "missing-private-authority":
				response.ControlServiceIdentityID, response.SourceInstance = "", ""
			case "error-with-installation":
				response.Error = "invalid"
			}
			clientSide, serverSide := net.Pipe()
			defer clientSide.Close()
			defer serverSide.Close()
			client := &UnixController{socketPath: LocalControllerSocket, timeout: time.Second, dial: func(context.Context) (net.Conn, error) { return clientSide, nil }}
			done := make(chan error, 1)
			go func() {
				var request ManagedACKRequest
				if e := json.NewDecoder(bufio.NewReader(serverSide)).Decode(&request); e != nil {
					done <- e
					return
				}
				if request.ServiceIdentityID != identity || !MatchesCanonicalDigest(request.Result, resultSHA) {
					done <- ErrInvalid
					return
				}
				done <- json.NewEncoder(serverSide).Encode(response)
			}()
			got, e := client.AcknowledgeManaged(context.Background(), identity, result)
			if scenario == "valid" {
				if e != nil || got.ControlServiceIdentityID != identity || got.SourceInstance != installation.SourceInstance || ValidateInstallation(got) != nil {
					t.Fatalf("valid local receipt rejected: %v", e)
				}
			} else if !errors.Is(e, ErrInvalid) {
				t.Fatalf("invalid receipt accepted: %v", e)
			}
			if e := <-done; e != nil {
				t.Fatal(e)
			}
		})
	}
}
