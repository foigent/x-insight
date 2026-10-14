package hostctl

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connectorcontrol"
)

func TestOriginalDescriptorHostCustodyBeforeAfterGETAndNetworkOutsideLock(t *testing.T) {
	e, _, _ := newFeedCredentialFixture(t)
	ctx := context.Background()
	var descriptor connectorcontrol.SourceDescriptorV1
	var registration FeedCredentialRegistration
	rotate := false
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.RawQuery != "" || r.URL.Path != "/api/v1/integrations/xinsight/connector-installations/xci_AAAAAAAAAAAAAAAAAAAAAA/source-binding" || r.Header.Get("Authorization") == "" {
			t.Error("fixed original receiver path")
		}
		if rotate {
			// This takes the host operation lock inside the remote response. A
			// network call holding that lock would deadlock/fail its timeout.
			if _, err := e.PrepareFeedCredential(ctx, registration.OperationID, registration.VerifierSHA256); err != nil {
				t.Error("concurrent supported rotation", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(descriptor)
	}))
	defer server.Close()
	staged, err := e.StageManagedFeedConfig(ctx, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.PromoteManagedFeedConfig(ctx, staged.ConfigSHA256); err != nil {
		t.Fatal(err)
	}
	registration, err = e.PrepareFeedCredential(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.ActivateFeedCredential(ctx, registration.OperationID, registration.VerifierSHA256); err != nil {
		t.Fatal(err)
	}
	public := connectorcontrol.PublicReceiverRegistrationV1{SchemaVersion: registration.SchemaVersion, TokenFormat: registration.TokenFormat, VerifierScheme: registration.VerifierScheme, KeyPrefix: registration.KeyPrefix, VerifierSHA256: registration.VerifierSHA256, OperationID: registration.OperationID}
	root := filepath.Join("..", "..", "contracts", "connector-control", "v2", "fixtures", "valid")
	b := connectorcontrol.ManagedBoundary{Installation: connectorcontrol.Installation{InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA", DeploymentID: "xid_ZZZZZZZZZZZZZZZZZZZZZZ", SourceInstance: "ecs.second-integration.v2", Status: "connected", ObservedState: "connected", DesiredState: "active", InstallationAcknowledgementSHA256: "sha256:" + strings.Repeat("a", 64)}, UniRouterOrigin: server.URL}
	var predecessorID, predecessorSHA string
	registrationSHA, _, _ := connectorcontrol.CanonicalSHA256(public)
	for _, files := range [][2]string{{"01-prepare-paused-source-operation.json", "02-prepare-paused-source-result.json"}, {"03-register-receiver-operation.json", "04-register-receiver-result.json"}, {"05-resume-source-operation.json", "06-resume-source-result.json"}} {
		raw, err := os.ReadFile(filepath.Join(root, files[0]))
		if err != nil {
			t.Fatal(err)
		}
		var request connectorcontrol.ControlOperationRequestV2
		if json.Unmarshal(raw, &request) != nil {
			t.Fatal("fixture request")
		}
		raw, err = os.ReadFile(filepath.Join(root, files[1]))
		if err != nil {
			t.Fatal(err)
		}
		var result connectorcontrol.ControlOperationResult
		if json.Unmarshal(raw, &result) != nil {
			t.Fatal("fixture result")
		}
		request.PredecessorOperationID, request.PredecessorResultSHA256 = predecessorID, predecessorSHA
		if request.Phase == connectorcontrol.ControlPhaseRegisterReceiver {
			request.Request.ReceiverRegistration = &public
			request.Request.NetworkBinding = staged.NetworkBinding
			result.Result.ReceiverRegistrationSHA256 = registrationSHA
			result.Result.NetworkBindingSHA256 = staged.NetworkBindingSHA256
		} else if request.Phase == connectorcontrol.ControlPhaseResumeSource {
			request.Request.ReceiverRegistrationSHA256 = registrationSHA
			request.Request.NetworkBindingSHA256 = staged.NetworkBindingSHA256
		}
		at := result.ObservedAt.Add(-time.Second)
		r := request.Request
		op := connectorcontrol.ControlOperation{SchemaVersion: request.SchemaVersion, OperationID: request.OperationID, InstallationID: request.InstallationID, ProjectID: request.ProjectID, EventScope: request.EventScope, DesiredState: request.DesiredState, Kind: request.Kind, Phase: request.Phase, PredecessorOperationID: predecessorID, PredecessorResultSHA256: predecessorSHA, Request: &r, Status: "completed", AvailableAt: at, CreatedAt: at, UpdatedAt: result.ObservedAt, CompletedAt: &result.ObservedAt}
		op.RequestSHA256, _, _ = connectorcontrol.CanonicalSHA256(request)
		op.ResultSHA256, _, _ = connectorcontrol.CanonicalSHA256(result)
		b.Installation.ProjectID = request.ProjectID
		b.Phases = append(b.Phases, connectorcontrol.ActivationPhase{Operation: op, Result: &result})
		predecessorID, predecessorSHA = op.OperationID, op.ResultSHA256
	}
	scope := connectorcontrol.SourceBindingV1{SchemaVersion: connectorcontrol.SourceBindingSchema, InstallationID: b.Installation.InstallationID, DeploymentID: b.Installation.DeploymentID, OrgID: 2, ProjectID: 86, SourceInstance: b.Installation.SourceInstance, EventScope: connectorcontrol.CompletedEventType, FeedSchema: connectorcontrol.SourceBindingFeedSchema}
	b.SourceBindingSHA256, _, _ = connectorcontrol.CanonicalSHA256(scope)
	network := connectorcontrol.ReceiverNetworkV1{FeedKeyID: "105", KeyPrefix: public.KeyPrefix, RegistrationOperationID: public.OperationID, VerifierScheme: public.VerifierScheme, AllowedIP: "192.0.2.10/32"}
	networkSHA, _, _ := connectorcontrol.CanonicalSHA256(network)
	descriptor = connectorcontrol.SourceDescriptorV1{SchemaVersion: connectorcontrol.SourceDescriptorSchema, SourceBinding: scope, SourceBindingSHA256: b.SourceBindingSHA256, ReceiverNetwork: network, ReceiverNetworkSHA256: networkSHA, ResumeOperationID: predecessorID, ResumeResultSHA256: predecessorSHA}
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	got, err := e.FetchOriginalSourceDescriptor(ctx, b, roots)
	if err != nil || got != descriptor || calls != 1 {
		t.Fatal("qualified read", err, calls)
	}
	rotate = true
	if _, err = e.FetchOriginalSourceDescriptor(ctx, b, roots); err == nil || calls != 2 {
		t.Fatal("credential rotation during GET was trusted", err, calls)
	}
	if _, err = e.FetchOriginalSourceDescriptor(ctx, b, roots); err == nil || calls != 2 {
		t.Fatal("staged credential reached network", err, calls)
	}
}
