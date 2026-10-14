package main

import (
	"testing"

	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/connectorcontrol"
)

func TestValidateLoopbackAddress(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8080", "localhost:8080", "[::1]:8080"} {
		if err := validateLoopbackAddress(address); err != nil {
			t.Fatalf("address=%q error=%v", address, err)
		}
	}
}

func TestConnectorInvitationConfigIsAllOrNothing(t *testing.T) {
	const deploymentID = "xid_AAAAAAAAAAAAAAAAAAAAAA"
	for _, test := range []struct {
		name                   string
		deploymentID           string
		xInsightOrigin         string
		uniRouterOrigin        string
		wantEnabled            bool
		wantConfigurationError bool
	}{
		{name: "all empty"},
		{name: "TLS only", xInsightOrigin: "https://xinsight.example"},
		{name: "generated identity only", deploymentID: deploymentID},
		{name: "generated identity and TLS", deploymentID: deploymentID, xInsightOrigin: "https://xinsight.example"},
		{name: "target only", uniRouterOrigin: "https://unirouter.example", wantConfigurationError: true},
		{name: "identity and target", deploymentID: deploymentID, uniRouterOrigin: "https://unirouter.example", wantConfigurationError: true},
		{name: "TLS and target", xInsightOrigin: "https://xinsight.example", uniRouterOrigin: "https://unirouter.example", wantConfigurationError: true},
		{name: "invalid dormant identity", deploymentID: "xid_invalid", wantConfigurationError: true},
		{
			name:            "full",
			deploymentID:    deploymentID,
			xInsightOrigin:  "https://xinsight.example",
			uniRouterOrigin: "https://unirouter.example",
			wantEnabled:     true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(connectorcontrol.DeploymentIDEnv, test.deploymentID)
			t.Setenv(api.ExternalOriginEnv, test.xInsightOrigin)
			t.Setenv(connectorcontrol.UniRouterInstallOriginEnv, test.uniRouterOrigin)
			proxyMode := ""
			if test.xInsightOrigin != "" {
				proxyMode = "loopback"
			}
			t.Setenv(api.TrustedTLSProxyModeEnv, proxyMode)
			if _, err := api.NewTransportSecurity(proxyMode, test.xInsightOrigin); err != nil {
				t.Fatalf("transport security: %v", err)
			}
			config, err := loadConnectorInvitationConfig()
			if (err != nil) != test.wantConfigurationError {
				t.Fatalf("config=%+v err=%v", config, err)
			}
			if err == nil && config.Enabled() != test.wantEnabled {
				t.Fatalf("config=%+v enabled=%t expected=%t", config, config.Enabled(), test.wantEnabled)
			}
		})
	}
}

func TestValidateLoopbackAddressRejectsWildcardAndPrivateInterfaces(t *testing.T) {
	for _, address := range []string{":8080", "0.0.0.0:8080", "192.168.1.10:8080", "[::]:8080"} {
		if err := validateLoopbackAddress(address); err == nil {
			t.Fatalf("address=%q unexpectedly accepted", address)
		}
	}
}
