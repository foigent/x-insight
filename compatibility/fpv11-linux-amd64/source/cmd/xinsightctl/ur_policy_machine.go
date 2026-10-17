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
	bearer, err := config.LoadOwnerOnlySecretFile(bearerPath)
	if err != nil {
		return nil, urmachine.Identity{}, fmt.Errorf("delegated UR policy credential is unavailable")
	}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	if caPath != "" {
		if !filepath.IsAbs(caPath) || filepath.Clean(caPath) != caPath {
			return nil, urmachine.Identity{}, fmt.Errorf("delegated UR policy CA path is invalid")
		}
		ca, readErr := os.ReadFile(caPath)
		if readErr != nil || len(ca) == 0 || len(ca) > 128<<10 {
			return nil, urmachine.Identity{}, fmt.Errorf("delegated UR policy CA is unavailable")
		}
		roots, rootErr := x509.SystemCertPool()
		if rootErr != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(ca) {
			return nil, urmachine.Identity{}, fmt.Errorf("delegated UR policy CA is invalid")
		}
		transport.TLSClientConfig.RootCAs = roots
	}
	client, err := urmachine.NewPolicyClient(origin, bearer, identity,
		&http.Client{Transport: transport, Timeout: 10 * time.Second})
	if err != nil {
		return nil, urmachine.Identity{}, fmt.Errorf("delegated UR policy origin or binding is invalid")
	}
	return client, identity, nil
}
