package hostctl

import (
	"path/filepath"
	"testing"
)

func TestPathsUseCanonicalDataDiskPostgresRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv(AllowTestRootEnv, "1")
	t.Setenv(TestRootEnv, root)

	paths, err := PathsFromEnvironment()
	if err != nil {
		t.Fatalf("paths from environment: %v", err)
	}
	expected := filepath.Join(root, "data", "x-insight", "postgres")
	if paths.PostgresDataDir != expected {
		t.Fatalf("postgres data dir=%q, expected %q", paths.PostgresDataDir, expected)
	}
	for actual, expected := range map[string]string{
		paths.AccessAccountsFile:    filepath.Join(root, "etc", "x-insight", "access-accounts.json"),
		paths.ReaderDSNFile:         filepath.Join(root, "etc", "x-insight", "database", "current", "postgres-reader.dsn"),
		paths.WorkspaceDSNFile:      filepath.Join(root, "etc", "x-insight", "database", "current", "postgres-workspace.dsn"),
		paths.ImporterDSNFile:       filepath.Join(root, "etc", "x-insight", "database", "current", "postgres-importer.dsn"),
		paths.AdminDSNFile:          filepath.Join(root, "etc", "x-insight", "database", "current", "postgres-admin.dsn"),
		paths.ControllerServiceUnit: filepath.Join(root, "etc", "systemd", "system", "xinsight-connector-controller.service"),
		paths.ControllerSocket:      filepath.Join(root, "run", "x-insight", "connector-controller.sock"),
		paths.ConnectorIdentityEnv:  filepath.Join(root, "etc", "x-insight", "xinsight-connector-id.env"),
		paths.ConnectorEnvironment:  filepath.Join(root, "etc", "x-insight", "xinsight-connector.env"),
		paths.TrustedTLSProxyEnv:    filepath.Join(root, "etc", "x-insight", "trusted-tls-proxy.env"),
		paths.FeedEnvironmentFile:   filepath.Join(root, "etc", "x-insight", "xinsight-feed.env"),
		paths.FeedCredentialFile:    filepath.Join(root, "etc", "x-insight", "unirouter-feed.key"),
		paths.FeedCredentialNext:    filepath.Join(root, "etc", "x-insight", "unirouter-feed.key.next"),
		paths.FeedCredentialState:   filepath.Join(root, "var", "lib", "x-insight", "control", "feed-credential-operation.json"),
	} {
		if actual != expected {
			t.Fatalf("feed credential path=%q expected=%q", actual, expected)
		}
	}
	if err := paths.Validate(); err != nil {
		t.Fatalf("validate paths: %v", err)
	}
}
