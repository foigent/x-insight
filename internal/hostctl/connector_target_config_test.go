package hostctl

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConnectorIdentityEnvironmentGenerateOnceAndReuse(t *testing.T) {
	engine, paths := newConnectorTargetTestEngine(t)
	engine.Random = bytes.NewReader(bytes.Repeat([]byte{0x44}, 16))
	if err := engine.ensureConnectorIdentityEnvironmentFile(); err != nil {
		t.Fatalf("generate Connector identity environment: %v", err)
	}
	first, err := os.ReadFile(paths.ConnectorIdentityEnv)
	if err != nil {
		t.Fatalf("read generated Connector identity environment: %v", err)
	}
	if string(first) != "XINSIGHT_CONNECTOR_DEPLOYMENT_ID=xid_RERERERERERERERERERERA\n" {
		t.Fatalf("generated Connector identity environment=%q", first)
	}
	info, err := os.Stat(paths.ConnectorIdentityEnv)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("generated Connector identity environment mode=%v err=%v", infoMode(info), err)
	}
	if _, err := os.Lstat(paths.ConnectorEnvironment); !os.IsNotExist(err) {
		t.Fatalf("Connector target environment exists before explicit target configuration: %v", err)
	}

	engine.Random = bytes.NewReader(nil)
	if err := engine.ensureConnectorIdentityEnvironmentFile(); err != nil {
		t.Fatalf("reuse Connector identity environment: %v", err)
	}
	second, err := os.ReadFile(paths.ConnectorIdentityEnv)
	if err != nil || !bytes.Equal(second, first) {
		t.Fatalf("reused Connector identity environment=%q err=%v", second, err)
	}
	if _, err := os.Lstat(paths.ConnectorIdentityEnv + ".staged"); !os.IsNotExist(err) {
		t.Fatalf("Connector staging survived publication: %v", err)
	}
}

func TestConnectorIdentityEnvironmentRecoversInterruptedPublication(t *testing.T) {
	const stagedIdentity = "XINSIGHT_CONNECTOR_DEPLOYMENT_ID=xid_EEEEEEEEEEEEEEEEEEEEEE\n"
	for _, test := range []struct {
		name             string
		stagedContents   string
		publishBeforeRun bool
		wantContents     string
	}{
		{
			name:           "partial staging before publish",
			stagedContents: "XINSIGHT_CONNECTOR_DEPLOYMENT_ID=",
			wantContents:   "XINSIGHT_CONNECTOR_DEPLOYMENT_ID=xid_RUVFRUVFRUVFRUVFRUVFRQ\n",
		},
		{name: "complete staging before publish", stagedContents: stagedIdentity, wantContents: stagedIdentity},
		{name: "published link before staging cleanup", stagedContents: stagedIdentity, publishBeforeRun: true, wantContents: stagedIdentity},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine, paths := newConnectorTargetTestEngine(t)
			engine.Random = bytes.NewReader(bytes.Repeat([]byte{0x45}, 16))
			if err := os.MkdirAll(filepath.Dir(paths.ConnectorIdentityEnv), 0o755); err != nil {
				t.Fatal(err)
			}
			stagingPath := paths.ConnectorIdentityEnv + ".staged"
			if err := os.WriteFile(stagingPath, []byte(test.stagedContents), 0o600); err != nil {
				t.Fatal(err)
			}
			if test.publishBeforeRun {
				if err := os.Link(stagingPath, paths.ConnectorIdentityEnv); err != nil {
					t.Fatal(err)
				}
			}
			if err := engine.ensureConnectorIdentityEnvironmentFile(); err != nil {
				t.Fatalf("recover Connector identity environment: %v", err)
			}
			contents, err := os.ReadFile(paths.ConnectorIdentityEnv)
			if err != nil || string(contents) != test.wantContents {
				t.Fatalf("Connector identity environment=%q err=%v", contents, err)
			}
			if _, err := os.Lstat(stagingPath); !os.IsNotExist(err) {
				t.Fatalf("Connector staging survived recovery: %v", err)
			}
		})
	}
}

func TestConnectorIdentityEnvironmentValidation(t *testing.T) {
	const deployment = "XINSIGHT_CONNECTOR_DEPLOYMENT_ID=xid_AAAAAAAAAAAAAAAAAAAAAA\n"
	for _, test := range []struct {
		name     string
		contents string
		wantErr  bool
	}{
		{name: "generated identity", contents: deployment},
		{name: "all empty", contents: "# no identity\n", wantErr: true},
		{name: "target key is not identity", contents: "XINSIGHT_UNIROUTER_INSTALL_ORIGIN=https://unirouter.example\n", wantErr: true},
		{name: "invalid identity", contents: "XINSIGHT_CONNECTOR_DEPLOYMENT_ID=xid_short\n", wantErr: true},
		{name: "unsupported key", contents: deployment + "UNSUPPORTED=value\n", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "xinsight-connector-id.env")
			if err := os.WriteFile(path, []byte(test.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readConnectorIdentityEnvironmentFile(path); (err != nil) != test.wantErr {
				t.Fatalf("validation error=%v", err)
			}
		})
	}
}

func TestConnectorTargetEnvironmentValidation(t *testing.T) {
	for _, test := range []struct {
		name     string
		contents string
		wantErr  bool
	}{
		{name: "canonical HTTPS origin", contents: "XINSIGHT_UNIROUTER_INSTALL_ORIGIN=https://unirouter.example\n"},
		{name: "all empty", contents: "# no target\n", wantErr: true},
		{name: "identity key is not target", contents: "XINSIGHT_CONNECTOR_DEPLOYMENT_ID=xid_AAAAAAAAAAAAAAAAAAAAAA\n", wantErr: true},
		{name: "non HTTPS target", contents: "XINSIGHT_UNIROUTER_INSTALL_ORIGIN=http://unirouter.example\n", wantErr: true},
		{name: "target with path", contents: "XINSIGHT_UNIROUTER_INSTALL_ORIGIN=https://unirouter.example/install\n", wantErr: true},
		{name: "unsupported key", contents: "UNSUPPORTED=value\n", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "xinsight-connector.env")
			if err := os.WriteFile(path, []byte(test.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readConnectorTargetEnvironmentFile(path); (err != nil) != test.wantErr {
				t.Fatalf("validation error=%v", err)
			}
		})
	}
}

func TestManagedServerLoadsConnectorEnvironmentWithoutInlineAuthority(t *testing.T) {
	unitPath := filepath.Join("..", "..", "deploy", "xinsight.service")
	contents, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("read server unit: %v", err)
	}
	unit := string(contents)
	for _, required := range []string{
		"EnvironmentFile=/etc/x-insight/xinsight.env",
		"EnvironmentFile=-/etc/x-insight/trusted-tls-proxy.env",
		"EnvironmentFile=-/etc/x-insight/xinsight-connector-id.env",
		"EnvironmentFile=-/etc/x-insight/xinsight-connector.env",
		"ExecStartPre=+/usr/bin/env -i /opt/x-insight/current/bin/xinsightctl connector-target-config validate",
	} {
		if !strings.Contains(unit, required) {
			t.Fatalf("server unit is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"Environment=XINSIGHT_CONNECTOR_DEPLOYMENT_ID",
		"Environment=XINSIGHT_UNIROUTER_INSTALL_ORIGIN",
	} {
		if strings.Contains(unit, forbidden) {
			t.Fatalf("server unit contains inline Connector authority %q", forbidden)
		}
	}

	examplePath := filepath.Join("..", "..", "deploy", "xinsight-connector.env.example")
	example, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("read Connector environment example: %v", err)
	}
	wantExample := "# Reference only; do not install this placeholder file.\n" +
		"# Use xinsightctl connector-target-config set; the installer owns the separate identity file.\n" +
		"XINSIGHT_UNIROUTER_INSTALL_ORIGIN=https://REPLACE_WITH_APPROVED_UNIROUTER_ORIGIN\n"
	if string(example) != wantExample {
		t.Fatalf("Connector environment example=%q", example)
	}
}

func TestManagedConnectorTargetSetPreservesDeploymentIdentity(t *testing.T) {
	engine, paths := newConnectorTargetTestEngine(t)
	engine.Random = bytes.NewReader(bytes.Repeat([]byte{0x46}, 16))
	if err := engine.ensureConnectorIdentityEnvironmentFile(); err != nil {
		t.Fatalf("generate Connector identity environment: %v", err)
	}
	identity, err := os.ReadFile(paths.ConnectorIdentityEnv)
	if err != nil {
		t.Fatalf("read generated Connector identity: %v", err)
	}
	if err := engine.SetConnectorTargetOrigin("https://unirouter.example"); err == nil {
		t.Fatal("target was accepted without the managed TLS external origin")
	}
	if err := os.WriteFile(
		paths.TrustedTLSProxyEnv,
		[]byte("XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback\nXINSIGHT_EXTERNAL_ORIGIN=https://xinsight.example\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := engine.SetConnectorTargetOrigin("https://unirouter.example"); err != nil {
		t.Fatalf("set Connector target: %v", err)
	}
	want := "XINSIGHT_UNIROUTER_INSTALL_ORIGIN=https://unirouter.example\n"
	contents, err := os.ReadFile(paths.ConnectorEnvironment)
	if err != nil || string(contents) != want {
		t.Fatalf("Connector target environment=%q err=%v", contents, err)
	}
	identityAfterSet, err := os.ReadFile(paths.ConnectorIdentityEnv)
	if err != nil || !bytes.Equal(identityAfterSet, identity) {
		t.Fatalf("Connector identity changed while setting target: contents=%q err=%v", identityAfterSet, err)
	}
	if err := engine.ValidateConnectorTargetConfig(); err != nil {
		t.Fatalf("validate Connector target: %v", err)
	}
	if err := os.WriteFile(
		paths.ConnectorEnvironment,
		[]byte("XINSIGHT_CONNECTOR_DEPLOYMENT_ID=xid_ZZZZZZZZZZZZZZZZZZZZZZ\nXINSIGHT_UNIROUTER_INSTALL_ORIGIN=https://unirouter.example\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := engine.ValidateConnectorTargetConfig(); err == nil || !strings.Contains(err.Error(), "unsupported key") {
		t.Fatalf("target environment identity override error=%v", err)
	}
}

func TestManagedConnectorConfigurationAllowsTLSOnlyAndRejectsPartialTarget(t *testing.T) {
	engine, paths := newConnectorTargetTestEngine(t)
	if err := engine.validateConnectorConfiguration(false); err != nil {
		t.Fatalf("empty legacy Connector configuration: %v", err)
	}
	engine.Random = bytes.NewReader(bytes.Repeat([]byte{0x47}, 16))
	if err := engine.ensureConnectorIdentityEnvironmentFile(); err != nil {
		t.Fatalf("generate Connector identity: %v", err)
	}
	if err := os.WriteFile(
		paths.TrustedTLSProxyEnv,
		[]byte("XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback\nXINSIGHT_EXTERNAL_ORIGIN=https://xinsight.example\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := engine.ValidateConnectorTargetConfig(); err != nil {
		t.Fatalf("TLS-only Connector configuration: %v", err)
	}
	if err := os.WriteFile(
		paths.ConnectorEnvironment,
		[]byte("XINSIGHT_UNIROUTER_INSTALL_ORIGIN=https://unirouter.example\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(paths.TrustedTLSProxyEnv); err != nil {
		t.Fatal(err)
	}
	if err := engine.ValidateConnectorTargetConfig(); err == nil || !strings.Contains(err.Error(), "requires the managed trusted TLS") {
		t.Fatalf("partial Connector target error=%v", err)
	}
}

func newConnectorTargetTestEngine(t *testing.T) (*Engine, Paths) {
	t.Helper()
	root := t.TempDir()
	t.Setenv(AllowTestRootEnv, "1")
	t.Setenv(TestRootEnv, root)
	paths, err := PathsFromEnvironment()
	if err != nil {
		t.Fatalf("paths: %v", err)
	}
	engine, err := NewEngine(paths)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	return engine, paths
}
