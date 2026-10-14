package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/integration/urmachine"
	"github.com/foigent/x-insight/internal/reviewpolicycontrol"
)

const (
	urPolicyOriginEnv         = "XINSIGHT_UR_POLICY_ORIGIN"
	urPolicyBearerFileEnv     = "XINSIGHT_UR_POLICY_BEARER_FILE"
	urPolicyInstallationEnv   = "XINSIGHT_UR_POLICY_INSTALLATION_ID"
	urPolicySourceInstanceEnv = "XINSIGHT_UR_POLICY_SOURCE_INSTANCE"
	urPolicyOrgEnv            = "XINSIGHT_UR_POLICY_ORG_ID"
	urPolicyCAFileEnv         = "XINSIGHT_UR_POLICY_CA_FILE"
)

// An absent group leaves UR policy management unavailable. Its presence is an
// explicit deployment configuration; no feed or source-enrollment key can be
// reused because NewPolicyClient requires the separate ur-xpm1 purpose.
func loadDelegatedURPolicyClient() (reviewpolicycontrol.URPolicyClient, urmachine.Identity, error) {
	managed, enabled, err := loadManagedURMachineEnvironment()
	if err != nil {
		return nil, urmachine.Identity{}, err
	}
	if enabled {
		identity := urmachine.Identity{InstallationID: managed.InstallationID, SourceInstance: managed.SourceInstance, OrgID: managed.OrgID}
		if managed.PolicyFile == "" {
			return nil, identity, nil
		}
		bearer, httpClient, err := machineFileTransport(managed.PolicyFile, managed.CAFile, "xpm1")
		if err != nil {
			return nil, urmachine.Identity{}, err
		}
		client, err := urmachine.NewPolicyClient(os.Getenv(connectorcontrol.UniRouterInstallOriginEnv), bearer, identity, httpClient)
		return client, identity, err
	}
	origin := strings.TrimSpace(os.Getenv(urPolicyOriginEnv))
	bearerPath := strings.TrimSpace(os.Getenv(urPolicyBearerFileEnv))
	identity := urmachine.Identity{InstallationID: strings.TrimSpace(os.Getenv(urPolicyInstallationEnv)),
		SourceInstance: strings.TrimSpace(os.Getenv(urPolicySourceInstanceEnv)), OrgID: strings.TrimSpace(os.Getenv(urPolicyOrgEnv))}
	caPath := strings.TrimSpace(os.Getenv(urPolicyCAFileEnv))
	if origin == "" && bearerPath == "" && identity == (urmachine.Identity{}) && caPath == "" {
		return nil, identity, nil
	}
	if origin == "" || bearerPath == "" || identity.InstallationID == "" || identity.SourceInstance == "" || identity.OrgID == "" {
		return nil, urmachine.Identity{}, fmt.Errorf("delegated UR policy configuration is incomplete")
	}
	if canonical := os.Getenv(connectorcontrol.UniRouterInstallOriginEnv); canonical != "" && canonical != origin {
		return nil, urmachine.Identity{}, fmt.Errorf("delegated UR policy origin differs from Connector origin")
	}
	bearer, httpClient, err := machineFileTransport(bearerPath, caPath, "xpm1")
	if err != nil {
		return nil, urmachine.Identity{}, fmt.Errorf("delegated UR policy credential is unavailable")
	}
	client, err := urmachine.NewPolicyClient(origin, bearer, identity, httpClient)
	if err != nil {
		return nil, urmachine.Identity{}, fmt.Errorf("delegated UR policy origin or binding is invalid")
	}
	return client, identity, nil
}

func machineFileTransport(bearerPath, caPath, purpose string) (string, *http.Client, error) {
	bearer, err := config.LoadOwnerOnlySecretFile(bearerPath)
	if err != nil || config.ValidateURMachineToken(bearer, purpose) != nil {
		return "", nil, fmt.Errorf("delegated UR machine credential is unavailable")
	}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	if caPath != "" {
		if !filepath.IsAbs(caPath) || filepath.Clean(caPath) != caPath {
			return "", nil, fmt.Errorf("delegated UR policy CA path is invalid")
		}
		ca, readErr := os.ReadFile(caPath)
		if readErr != nil || len(ca) == 0 || len(ca) > 128<<10 {
			return "", nil, fmt.Errorf("delegated UR policy CA is unavailable")
		}
		roots, rootErr := x509.SystemCertPool()
		if rootErr != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(ca) {
			return "", nil, fmt.Errorf("delegated UR policy CA is invalid")
		}
		transport.TLSClientConfig.RootCAs = roots
	}
	return bearer, &http.Client{Transport: transport, Timeout: 10 * time.Second}, nil
}

func loadManagedURMachineEnvironment() (hostctl.URMachineConfig, bool, error) {
	v := map[string]string{}
	for _, key := range []string{config.URMachineInstallationEnv, config.URMachineSourceEnv, config.URMachineOrgEnv, config.URMachineCAEnv, config.URPolicyFileEnv, config.UREnrollFileEnv} {
		v[key] = os.Getenv(key)
	}
	// Keep the legacy development policy environment separate and fail closed
	// if it attempts to override the managed identity or origin.
	managed := v[config.URMachineInstallationEnv] != "" || v[config.URMachineSourceEnv] != "" || v[config.URMachineOrgEnv] != "" || v[config.UREnrollFileEnv] != "" || v[config.URMachineCAEnv] != "" || v[config.URPolicyFileEnv] != "" && os.Getenv(urPolicyOriginEnv) == ""
	if !managed {
		return hostctl.URMachineConfig{}, false, nil
	}
	c, enabled, err := hostctl.ParseURMachineConfig(v)
	if err != nil {
		return c, enabled, err
	}
	origin := os.Getenv(connectorcontrol.UniRouterInstallOriginEnv)
	if !connectorcontrol.IsCanonicalHTTPSOrigin(origin) {
		return c, true, fmt.Errorf("managed UR machine origin is invalid")
	}
	for _, pair := range [][2]string{{urPolicyOriginEnv, origin}, {urPolicyInstallationEnv, c.InstallationID}, {urPolicySourceInstanceEnv, c.SourceInstance}, {urPolicyOrgEnv, c.OrgID}, {urPolicyCAFileEnv, c.CAFile}} {
		if value := os.Getenv(pair[0]); value != "" && value != pair[1] {
			return c, true, fmt.Errorf("legacy UR policy override conflicts with managed configuration")
		}
	}
	return c, enabled, nil
}

// A config loader only: production discovery/registration is B, not enabled by A.
func loadDelegatedUREnrollmentClient() (*urmachine.EnrollmentClient, urmachine.Identity, error) {
	c, enabled, err := loadManagedURMachineEnvironment()
	identity := urmachine.Identity{InstallationID: c.InstallationID, SourceInstance: c.SourceInstance, OrgID: c.OrgID}
	if err != nil {
		return nil, identity, err
	}
	if !enabled || c.EnrollFile == "" {
		return nil, identity, nil
	}
	bearer, httpClient, err := machineFileTransport(c.EnrollFile, c.CAFile, "xse1")
	if err != nil {
		return nil, identity, err
	}
	client, err := urmachine.NewEnrollmentClient(os.Getenv(connectorcontrol.UniRouterInstallOriginEnv), bearer, identity, httpClient)
	return client, identity, err
}
