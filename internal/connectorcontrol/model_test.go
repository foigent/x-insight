package connectorcontrol

import (
	"strings"
	"testing"
	"time"
)

func TestInvitationConfigAndInstallLinkBindFixedHTTPSOrigins(t *testing.T) {
	config := InvitationConfig{
		DeploymentID:           "xid_AAAAAAAAAAAAAAAAAAAAAA",
		XInsightOrigin:         "https://xinsight.example",
		UniRouterInstallOrigin: "https://unirouter.example",
	}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	token, _, err := NewInvitationToken(strings.NewReader(strings.Repeat("a", 32)))
	if err != nil {
		t.Fatal(err)
	}
	link, err := BuildInstallURL(config.UniRouterInstallOrigin, token, config.XInsightOrigin)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(link, "https://unirouter.example/connectors/x-insight/install#") ||
		!strings.Contains(link, "invitation=") || !strings.Contains(link, "xinsight_origin=") ||
		strings.Contains(link, "project") {
		t.Fatalf("unexpected bounded install link %q", link)
	}
	for _, invalid := range []InvitationConfig{
		{DeploymentID: config.DeploymentID, XInsightOrigin: "http://xinsight.example", UniRouterInstallOrigin: config.UniRouterInstallOrigin},
		{DeploymentID: config.DeploymentID, XInsightOrigin: config.XInsightOrigin + "/path", UniRouterInstallOrigin: config.UniRouterInstallOrigin},
		{DeploymentID: config.DeploymentID, XInsightOrigin: config.XInsightOrigin, UniRouterInstallOrigin: "https://unirouter.example?next=https://other.example"},
	} {
		if invalid.Validate() == nil {
			t.Fatalf("accepted invalid config %+v", invalid)
		}
	}
}

func TestInstallationAndControlContractsKeepSourceAndDeliveryStateSeparate(t *testing.T) {
	now := time.Date(2026, 8, 11, 2, 0, 0, 0, time.UTC)
	installation := Installation{
		SchemaVersion:                     InstallationSchema,
		InstallationID:                    "xci_AAAAAAAAAAAAAAAAAAAAAA",
		DeploymentID:                      "xid_AAAAAAAAAAAAAAAAAAAAAA",
		ConnectorType:                     ConnectorTypeUniRouter,
		RequestedEventScope:               []string{CompletedEventType},
		FinalEventScope:                   []string{CompletedEventType},
		ProjectID:                         "project.fixture",
		SourceInstance:                    "source.fixture.v1",
		SourceInstanceSHA256:              RawSHA256("source.fixture.v1"),
		ControlServiceIdentityID:          "xsi_AAAAAAAAAAAAAAAAAAAAAA",
		InstallationAcknowledgementSHA256: "sha256:" + strings.Repeat("a", 64),
		Status:                            "pending", DesiredState: "active", ObservedState: "source_ready",
		CreatedAt: now, UpdatedAt: now, Version: 2,
	}
	if err := ValidateInstallation(installation); err != nil {
		t.Fatal(err)
	}
	tampered := installation
	tampered.SourceInstanceSHA256 = RawSHA256("other")
	if ValidateInstallation(tampered) == nil {
		t.Fatal("Source digest drift was accepted")
	}
	operation := ControlOperation{
		SchemaVersion:  ControlOperationSchema,
		OperationID:    "xco_AAAAAAAAAAAAAAAAAAAAAA",
		InstallationID: installation.InstallationID,
		Kind:           "connect", ProjectID: installation.ProjectID,
		EventScope: []string{CompletedEventType}, DesiredState: "active",
		RequestSHA256: "sha256:" + strings.Repeat("b", 64), Status: "pending",
		AvailableAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := ValidateControlOperation(operation); err != nil {
		t.Fatal(err)
	}
	operation.EventScope = []string{"provider.request"}
	if ValidateControlOperation(operation) == nil {
		t.Fatal("unsupported event scope was accepted")
	}
}
