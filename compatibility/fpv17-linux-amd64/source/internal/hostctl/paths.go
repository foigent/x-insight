package hostctl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	AllowTestRootEnv = "XINSIGHTCTL_ALLOW_TEST_ROOT"
	TestRootEnv      = "XINSIGHTCTL_TEST_ROOT"
)

type Paths struct {
	SourceFactsConfig      string `json:"source_facts_config"`
	SourceFactsServiceUnit string `json:"source_facts_service_unit"`
	Root                   string `json:"root,omitempty"`
	IsTestRoot             bool   `json:"is_test_root"`
	ReleasesDir            string `json:"releases_dir"`
	CurrentLink            string `json:"current_link"`
	ControlDir             string `json:"control_dir"`
	ServiceLauncher        string `json:"service_launcher"`
	EtcDir                 string `json:"etc_dir"`
	DatabaseSetsDir        string `json:"database_sets_dir"`
	DatabaseCurrentLink    string `json:"database_current_link"`
	ReaderDSNFile          string `json:"reader_dsn_file"`
	WorkspaceDSNFile       string `json:"workspace_dsn_file"`
	ImporterDSNFile        string `json:"importer_dsn_file"`
	AdminDSNFile           string `json:"admin_dsn_file"`
	TrustStore             string `json:"trust_store"`
	EnvironmentFile        string `json:"environment_file"`
	ConnectorIdentityEnv   string `json:"connector_identity_environment_file"`
	ConnectorEnvironment   string `json:"connector_environment_file"`
	TrustedTLSProxyEnv     string `json:"trusted_tls_proxy_environment_file"`
	AccessAccountsFile     string `json:"access_accounts_file"`
	FeedEnvironmentFile    string `json:"feed_environment_file"`
	FeedCredentialFile     string `json:"feed_credential_file"`
	FeedCredentialNext     string `json:"feed_credential_next"`
	FeedCredentialState    string `json:"feed_credential_state"`
	LiteLLMEnvironment     string `json:"litellm_environment_file"`
	LiteLLMCredential      string `json:"litellm_credential_file"`
	PostgresEnvironment    string `json:"postgres_environment"`
	PostgresCompose        string `json:"postgres_compose"`
	ServiceUnit            string `json:"service_unit"`
	ControllerServiceUnit  string `json:"controller_service_unit"`
	ControllerSocket       string `json:"controller_socket"`
	FeedServiceUnit        string `json:"feed_service_unit"`
	LiteLLMServiceUnit     string `json:"litellm_service_unit"`
	ServiceDropInDir       string `json:"service_drop_in_dir"`
	ManagedServiceDropIn   string `json:"managed_service_drop_in"`
	StateDir               string `json:"state_dir"`
	InstallState           string `json:"install_state"`
	ActiveOperation        string `json:"active_operation"`
	OperationsDir          string `json:"operations_dir"`
	ReceiptsDir            string `json:"receipts_dir"`
	PostgresDataDir        string `json:"postgres_data_dir"`
	BackupsDir             string `json:"backups_dir"`
	OperationLock          string `json:"operation_lock"`
}

func PathsFromEnvironment() (Paths, error) {
	root := strings.TrimSpace(os.Getenv(TestRootEnv))
	isTest := root != ""
	if isTest {
		if os.Getenv(AllowTestRootEnv) != "1" {
			return Paths{}, fmt.Errorf("%s requires %s=1", TestRootEnv, AllowTestRootEnv)
		}
		if !filepath.IsAbs(root) || filepath.Clean(root) != root || root == string(os.PathSeparator) {
			return Paths{}, fmt.Errorf("test root must be an absolute clean non-root path")
		}
	}
	mapPath := func(absolute string) string {
		if !isTest {
			return absolute
		}
		return filepath.Join(root, strings.TrimPrefix(absolute, string(os.PathSeparator)))
	}
	databaseCurrent := mapPath("/etc/x-insight/database/current")
	serviceDropInDir := mapPath("/etc/systemd/system/xinsight.service.d")
	return Paths{
		SourceFactsConfig:      mapPath("/etc/x-insight/source-facts.json"),
		SourceFactsServiceUnit: mapPath("/etc/systemd/system/xinsight-source-facts.service"),
		Root:                   root,
		IsTestRoot:             isTest,
		ReleasesDir:            mapPath("/opt/x-insight/releases"),
		CurrentLink:            mapPath("/opt/x-insight/current"),
		ControlDir:             mapPath("/opt/x-insight/control"),
		ServiceLauncher:        mapPath("/opt/x-insight/control/xinsight-launcher"),
		EtcDir:                 mapPath("/etc/x-insight"),
		DatabaseSetsDir:        mapPath("/etc/x-insight/database/sets"),
		DatabaseCurrentLink:    databaseCurrent,
		ReaderDSNFile:          filepath.Join(databaseCurrent, "postgres-reader.dsn"),
		WorkspaceDSNFile:       filepath.Join(databaseCurrent, "postgres-workspace.dsn"),
		ImporterDSNFile:        filepath.Join(databaseCurrent, "postgres-importer.dsn"),
		AdminDSNFile:           filepath.Join(databaseCurrent, "postgres-admin.dsn"),
		TrustStore:             mapPath("/etc/x-insight/trusted-release-keys.json"),
		EnvironmentFile:        mapPath("/etc/x-insight/xinsight.env"),
		ConnectorIdentityEnv:   mapPath("/etc/x-insight/xinsight-connector-id.env"),
		ConnectorEnvironment:   mapPath("/etc/x-insight/xinsight-connector.env"),
		TrustedTLSProxyEnv:     mapPath("/etc/x-insight/trusted-tls-proxy.env"),
		AccessAccountsFile:     mapPath("/etc/x-insight/access-accounts.json"),
		FeedEnvironmentFile:    mapPath("/etc/x-insight/xinsight-feed.env"),
		FeedCredentialFile:     mapPath("/etc/x-insight/unirouter-feed.key"),
		FeedCredentialNext:     mapPath("/etc/x-insight/unirouter-feed.key.next"),
		FeedCredentialState:    mapPath("/var/lib/x-insight/control/feed-credential-operation.json"),
		LiteLLMEnvironment:     mapPath("/etc/x-insight/xinsight-litellm-feed.env"),
		LiteLLMCredential:      mapPath("/etc/x-insight/litellm-feed.key"),
		PostgresEnvironment:    mapPath("/etc/x-insight/postgres.env"),
		PostgresCompose:        mapPath("/etc/x-insight/postgres.compose.yml"),
		ServiceUnit:            mapPath("/etc/systemd/system/xinsight.service"),
		ControllerServiceUnit:  mapPath("/etc/systemd/system/xinsight-connector-controller.service"),
		ControllerSocket:       mapPath("/run/x-insight/connector-controller.sock"),
		FeedServiceUnit:        mapPath("/etc/systemd/system/xinsight-feed.service"),
		LiteLLMServiceUnit:     mapPath("/etc/systemd/system/xinsight-litellm-feed.service"),
		ServiceDropInDir:       serviceDropInDir,
		ManagedServiceDropIn:   filepath.Join(serviceDropInDir, "99-xinsight-managed-launcher.conf"),
		StateDir:               mapPath("/var/lib/x-insight/control"),
		InstallState:           mapPath("/var/lib/x-insight/control/install-state.json"),
		ActiveOperation:        mapPath("/var/lib/x-insight/control/active-operation.json"),
		OperationsDir:          mapPath("/var/lib/x-insight/control/operations"),
		ReceiptsDir:            mapPath("/var/lib/x-insight/control/receipts"),
		PostgresDataDir:        mapPath("/data/x-insight/postgres"),
		BackupsDir:             mapPath("/var/backups/x-insight"),
		OperationLock:          mapPath("/run/lock/xinsightctl.lock"),
	}, nil
}

func (paths Paths) Validate() error {
	for name, value := range map[string]string{
		"releases_dir": paths.ReleasesDir, "current_link": paths.CurrentLink, "control_dir": paths.ControlDir,
		"service_launcher": paths.ServiceLauncher, "etc_dir": paths.EtcDir,
		"database_sets_dir": paths.DatabaseSetsDir, "database_current_link": paths.DatabaseCurrentLink,
		"reader_dsn_file": paths.ReaderDSNFile, "workspace_dsn_file": paths.WorkspaceDSNFile,
		"importer_dsn_file": paths.ImporterDSNFile,
		"admin_dsn_file":    paths.AdminDSNFile, "trust_store": paths.TrustStore,
		"environment_file": paths.EnvironmentFile, "connector_identity_environment_file": paths.ConnectorIdentityEnv,
		"connector_environment_file":         paths.ConnectorEnvironment,
		"trusted_tls_proxy_environment_file": paths.TrustedTLSProxyEnv,
		"access_accounts_file":               paths.AccessAccountsFile,
		"feed_environment_file":              paths.FeedEnvironmentFile,
		"feed_credential_file":               paths.FeedCredentialFile,
		"feed_credential_next":               paths.FeedCredentialNext, "feed_credential_state": paths.FeedCredentialState,
		"litellm_environment_file": paths.LiteLLMEnvironment,
		"litellm_credential_file":  paths.LiteLLMCredential,
		"postgres_environment":     paths.PostgresEnvironment,
		"postgres_compose":         paths.PostgresCompose, "service_unit": paths.ServiceUnit,
		"controller_service_unit":   paths.ControllerServiceUnit,
		"controller_socket":         paths.ControllerSocket,
		"feed_service_unit":         paths.FeedServiceUnit,
		"source_facts_config":       paths.SourceFactsConfig,
		"source_facts_service_unit": paths.SourceFactsServiceUnit,
		"litellm_service_unit":      paths.LiteLLMServiceUnit,
		"service_drop_in_dir":       paths.ServiceDropInDir, "managed_service_drop_in": paths.ManagedServiceDropIn,
		"install_state": paths.InstallState, "active_operation": paths.ActiveOperation,
		"operations_dir": paths.OperationsDir, "receipts_dir": paths.ReceiptsDir,
		"postgres_data_dir": paths.PostgresDataDir, "backups_dir": paths.BackupsDir, "operation_lock": paths.OperationLock,
	} {
		if !filepath.IsAbs(value) || filepath.Clean(value) != value {
			return fmt.Errorf("invalid %s path", name)
		}
	}
	return nil
}
