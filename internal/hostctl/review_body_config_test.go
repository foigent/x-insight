package hostctl

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

func managedCustodyFixture(t *testing.T) (*Engine, ReviewBodyConfig) {
	t.Helper()
	e, p := newConnectorTargetTestEngine(t)
	for _, unit := range []string{p.ServiceUnit, p.SourceFactsServiceUnit, p.ControllerServiceUnit} {
		body, err := os.ReadFile(filepath.Join("..", "..", "deploy", filepath.Base(unit)))
		if err != nil {
			t.Fatal(err)
		}
		putManagedFile(t, unit, body, 0644)
	}
	c := ReviewBodyConfig{"local-custody", filepath.Join(p.EtcDir, "review-body.key")}
	putManagedFile(t, c.KeyFile, []byte(strings.Repeat("42", 32)+"\n"), 0600)
	putManagedFile(t, p.WorkspaceDSNFile, []byte("postgres://xinsight_workspace:test@127.0.0.1/workspace?sslmode=disable\n"), 0600)
	return e, c
}
func putManagedFile(t *testing.T, path string, body []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestManagedCustodyCheckPublishRepeatAndRetire(t *testing.T) {
	e, c := managedCustodyFixture(t)
	if err := e.ValidateReviewBodyConfig(); err != nil {
		t.Fatal("disabled", err)
	}
	before := snapshotManagedFiles(t, e.Paths.Root)
	if err := e.SetReviewBodyConfig(c, true); err != nil {
		t.Fatal("check", err)
	}
	if after := snapshotManagedFiles(t, e.Paths.Root); !bytes.Equal(before, after) {
		t.Fatal("check changed files")
	}
	if err := e.SetReviewBodyConfig(c, false); err != nil {
		t.Fatal("set", err)
	}
	for _, unit := range []string{e.Paths.ServiceUnit, e.Paths.SourceFactsServiceUnit} {
		path := filepath.Join(unit+".d", reviewBodyDropIn)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(body, []byte("LoadCredential=review-body.key:"+c.KeyFile+"\n")) || !bytes.Contains(body, []byte("/run/credentials/"+filepath.Base(unit)+"/review-body.key")) {
			t.Fatal("wrong key or unit reference")
		}
		if bytes.Contains(body, []byte("LoadCredential=\n")) {
			t.Fatal("new drop-in resets credentials")
		}
		workspace := bytes.Contains(body, []byte("LoadCredential=postgres-workspace.dsn:"))
		if workspace != (unit == e.Paths.SourceFactsServiceUnit) {
			t.Fatal("workspace appended to wrong unit")
		}
	}
	if err := e.ValidateReviewBodyConfig(); err != nil {
		t.Fatal(err)
	}
	before = snapshotManagedFiles(t, e.Paths.Root)
	if err := e.SetReviewBodyConfig(c, false); err != nil {
		t.Fatal("repeat", err)
	}
	if after := snapshotManagedFiles(t, e.Paths.Root); !bytes.Equal(before, after) {
		t.Fatal("repeat changed bytes")
	}
	if err := e.SetReviewBodyConfig(ReviewBodyConfig{"new-id", c.KeyFile}, false); err == nil {
		t.Fatal("implicit key rotation accepted")
	}
	if err := e.DisableReviewBodyConfig(true); err != nil {
		t.Fatal(err)
	}
	if after := snapshotManagedFiles(t, e.Paths.Root); !bytes.Equal(before, after) {
		t.Fatal("disable check wrote files")
	}
	if err := e.DisableReviewBodyConfig(false); err != nil {
		t.Fatal("disable", err)
	}
	if err := e.DisableReviewBodyConfig(false); err != nil {
		t.Fatal("repeat disable", err)
	}
	if _, err := os.Stat(c.KeyFile); err != nil {
		t.Fatal("key removed", err)
	}
	if err := e.SetReviewBodyConfig(c, false); err != nil {
		t.Fatal("reenable same binding", err)
	}
	if err := e.DisableReviewBodyConfig(false); err != nil {
		t.Fatal("second retirement", err)
	}
}

// Include directories and file bytes; --check must not create its lock/staging.
func snapshotManagedFiles(t *testing.T, root string) []byte {
	t.Helper()
	var result []byte
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		result = append(result, []byte(path+info.Mode().String())...)
		if entry.Type().IsRegular() {
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			result = append(result, body...)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestManagedCustodyInterruptedPublicationFailsClosedAndResumes(t *testing.T) {
	e, c := managedCustodyFixture(t)
	files := e.reviewBodyFiles(c)
	path := filepath.Join(e.Paths.ServiceUnit+".d", reviewBodyDropIn)
	putManagedFile(t, path+".next", files[path], 0600) // Interrupted before public chmod.
	before := snapshotManagedFiles(t, e.Paths.Root)
	if err := e.SetReviewBodyConfig(c, true); err != nil {
		t.Fatal("matching protected staging", err)
	}
	if !bytes.Equal(before, snapshotManagedFiles(t, e.Paths.Root)) {
		t.Fatal("staging check mutated files")
	}
	putManagedFile(t, path, files[path], 0644) // Interrupted before worker+authority.
	if err := e.ValidateReviewBodyConfig(); err == nil {
		t.Fatal("orphan accepted")
	}
	if err := e.SetReviewBodyConfig(c, false); err != nil {
		t.Fatal("same candidate recovery", err)
	}
	if err := os.Rename(path, path+".disabled"); err != nil {
		t.Fatal(err)
	}
	if err := e.ValidateReviewBodyConfig(); err == nil {
		t.Fatal("partial retirement accepted")
	}
	if err := e.DisableReviewBodyConfig(false); err != nil {
		t.Fatal("retirement recovery", err)
	}
}

func TestManagedCustodyRejectsUnsafeSourceAndUnknownDropIn(t *testing.T) {
	for _, kind := range []string{"group-readable", "symlink", "hardlink", "bad-format", "partial-config", "foreign-dropin", "bad-staging", "unsafe-parent", "unsafe-ancestor", "symlink-ancestor", "missing-workspace", "unsupported-unit"} {
		t.Run(kind, func(t *testing.T) {
			e, c := managedCustodyFixture(t)
			switch kind {
			case "group-readable":
				if err := os.Chmod(c.KeyFile, 0640); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				link := c.KeyFile + ".link"
				if err := os.Symlink(c.KeyFile, link); err != nil {
					t.Fatal(err)
				}
				c.KeyFile = link
			case "hardlink":
				if err := os.Link(c.KeyFile, c.KeyFile+".link"); err != nil {
					t.Fatal(err)
				}
			case "bad-format":
				putManagedFile(t, c.KeyFile, []byte("invalid"), 0600)
			case "partial-config":
				putManagedFile(t, e.reviewBodyEnvironment(), []byte(reviewBodyIDEnv+"=local-custody\n"), 0600)
			case "foreign-dropin":
				putManagedFile(t, filepath.Join(e.Paths.ServiceUnit+".d", reviewBodyDropIn), []byte("[Service]\nUser=root\n"), 0644)
			case "bad-staging":
				putManagedFile(t, e.reviewBodyEnvironment()+".next", []byte("different candidate"), 0600)
			case "unsafe-parent":
				if err := os.Chmod(e.Paths.EtcDir, 0777); err != nil {
					t.Fatal(err)
				}
			case "unsafe-ancestor":
				if err := os.Chmod(filepath.Dir(e.Paths.EtcDir), 0777); err != nil {
					t.Fatal(err)
				}
			case "symlink-ancestor":
				link := filepath.Join(e.Paths.Root, "linked-etc")
				if err := os.Symlink(e.Paths.EtcDir, link); err != nil {
					t.Fatal(err)
				}
				c.KeyFile = filepath.Join(link, "nested", "review-body.key")
				putManagedFile(t, filepath.Join(e.Paths.EtcDir, "nested", "review-body.key"), []byte(strings.Repeat("42", 32)), 0600)
			case "missing-workspace":
				if err := os.Rename(e.Paths.WorkspaceDSNFile, e.Paths.WorkspaceDSNFile+".absent"); err != nil {
					t.Fatal(err)
				}
			case "unsupported-unit":
				putManagedFile(t, e.Paths.SourceFactsServiceUnit, []byte("[Service]\nExecStart=old\n"), 0644)
			}
			if err := e.SetReviewBodyConfig(c, false); err == nil {
				t.Fatal("unsafe set accepted")
			}
		})
	}
}

func TestManagedCustodyPreservesLegacyCredentialsAndRejectsUnsupportedTarget(t *testing.T) {
	e, c := managedCustodyFixture(t)
	raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "source-facts.json.example"))
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := sourcefacts.ParseManagedConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.installSourceFactsCredentials(legacy); err != nil {
		t.Fatal(err)
	}
	ten := filepath.Join(e.Paths.SourceFactsServiceUnit+".d", "10-source-credentials.conf")
	before, err := os.ReadFile(ten)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetReviewBodyConfig(c, false); err != nil {
		t.Fatal(err)
	}
	if err := e.installSourceFactsCredentials(legacy); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(ten)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("legacy credential bytes changed", err)
	}
	if !bytes.Contains(after, []byte("LoadCredential=\nLoadCredential=postgres-importer.dsn:")) {
		t.Fatal("legacy reset/importer contract changed")
	}
	if err := e.validateManagedConfigurationTarget(func(name string) ([]byte, error) { return os.ReadFile(filepath.Join("..", "..", "deploy", name)) }); err != nil {
		t.Fatal("new target", err)
	}
	if err := e.validateManagedConfigurationTarget(func(string) ([]byte, error) { return []byte("old unit"), nil }); err == nil {
		t.Fatal("old unsupported target accepted")
	}
	if err := e.DisableReviewBodyConfig(false); err != nil {
		t.Fatal(err)
	}
	if err := e.validateManagedConfigurationTarget(func(string) ([]byte, error) { t.Fatal("disabled looked for new capability"); return nil, nil }); err != nil {
		t.Fatal("old disabled target", err)
	}
}

func TestManagedURMachinePurposesShareIdentityAndExactOrigin(t *testing.T) {
	e, _ := managedCustodyFixture(t)
	if err := e.ensureConnectorIdentityEnvironmentFile(); err != nil {
		t.Fatal(err)
	}
	putManagedFile(t, e.Paths.TrustedTLSProxyEnv, []byte("XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback\nXINSIGHT_EXTERNAL_ORIGIN=https://xinsight.example\n"), 0600)
	if err := e.SetConnectorTargetOrigin("https://8.130.18.85"); err != nil {
		t.Fatal(err)
	}
	c := URMachineConfig{InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA", SourceInstance: "source-instance", OrgID: "2", PolicyFile: filepath.Join(e.Paths.EtcDir, "ur-policy.key")}
	putManagedFile(t, c.PolicyFile, []byte("ur-xpm1-"+strings.Repeat("a", 64)), 0600)
	before := snapshotManagedFiles(t, e.Paths.Root)
	if err := e.SetURMachineConfig(c, true); err != nil {
		t.Fatal("check", err)
	}
	if after := snapshotManagedFiles(t, e.Paths.Root); !bytes.Equal(before, after) {
		t.Fatal("machine check changed files")
	}
	if err := e.SetURMachineConfig(c, false); err != nil {
		t.Fatal("policy", err)
	}
	enroll := c
	enroll.PolicyFile = ""
	enroll.EnrollFile = filepath.Join(e.Paths.EtcDir, "ur-enroll.key")
	putManagedFile(t, enroll.EnrollFile, []byte("ur-xse1-"+strings.Repeat("b", 64)), 0400)
	if err := e.SetURMachineConfig(enroll, false); err != nil {
		t.Fatal("enrollment", err)
	}
	v, err := readAllowlistedEnvironmentFile(e.Paths.ConnectorEnvironment, true, connectorTargetEnvironmentKeys)
	if err != nil {
		t.Fatal(err)
	}
	if v[config.URPolicyFileEnv] != c.PolicyFile || v[config.UREnrollFileEnv] != enroll.EnrollFile || v[connectorcontrol.UniRouterInstallOriginEnv] != "https://8.130.18.85" {
		t.Fatal("purpose or origin lost")
	}
	body, err := os.ReadFile(filepath.Join(e.Paths.ControllerServiceUnit+".d", machineDropIn))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "[Service]\nIPAddressAllow=\nIPAddressAllow=localhost\nIPAddressAllow=8.130.18.85/32\n" || bytes.Contains(body, []byte("LoadCredential")) {
		t.Fatal("machine egress or root-only reference changed")
	}
	if err := e.SetConnectorTargetOrigin("https://8.130.18.85"); err != nil {
		t.Fatal(err)
	}
	if err := e.SetConnectorTargetOrigin("https://8.130.18.86"); err == nil {
		t.Fatal("implicit origin migration accepted")
	}
	wrong := enroll
	wrong.SourceInstance = "other"
	if err := e.SetURMachineConfig(wrong, false); err == nil {
		t.Fatal("identity override accepted")
	}
	if err := e.ValidateConnectorTargetConfig(); err != nil {
		t.Fatal("combined validation", err)
	}
	for _, blocked := range []string{e.Paths.ConnectorEnvironment + ".machine-disabled", e.Paths.ConnectorEnvironment + ".next"} {
		putManagedFile(t, blocked, []byte("foreign retirement candidate"), 0600)
		before := snapshotManagedFiles(t, e.Paths.Root)
		for _, check := range []bool{true, false} {
			if err := e.DisableURMachineConfig(check); err == nil {
				t.Fatal("foreign retirement/staging accepted")
			}
			if !bytes.Equal(before, snapshotManagedFiles(t, e.Paths.Root)) {
				t.Fatal("rejected retirement changed files")
			}
		}
		if err := os.Rename(blocked, blocked+".fixture"); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.DisableURMachineConfig(true); err != nil {
		t.Fatal(err)
	}
	if err := e.DisableURMachineConfig(false); err != nil {
		t.Fatal(err)
	}
	if err := e.ValidateConnectorTargetConfig(); err != nil {
		t.Fatal("disabled", err)
	}
	if _, err := os.Stat(c.PolicyFile); err != nil {
		t.Fatal("policy key lost")
	}
	if _, err := os.Stat(enroll.EnrollFile); err != nil {
		t.Fatal("enrollment key lost")
	}
}

func TestManagedURMachineRejectsHalfGroupSharedKeyAndHostnameEgress(t *testing.T) {
	e, _ := managedCustodyFixture(t)
	for _, v := range []map[string]string{{config.URMachineInstallationEnv: "xci_AAAAAAAAAAAAAAAAAAAAAA"}, {config.URPolicyFileEnv: "/etc/x-insight/policy.key"}, {config.URMachineInstallationEnv: "xci_AAAAAAAAAAAAAAAAAAAAAA", config.URMachineSourceEnv: "source", config.URMachineOrgEnv: "2", config.URPolicyFileEnv: "/etc/x-insight/same.key", config.UREnrollFileEnv: "/etc/x-insight/same.key"}} {
		if _, _, err := parseURMachineConfig(v); err == nil {
			t.Fatal("unsafe group accepted")
		}
	}
	if _, err := e.machineFiles("https://example.com", URMachineConfig{}); err == nil {
		t.Fatal("unreviewed hostname egress accepted")
	}
	if _, err := e.machineFiles("https://[2001:db8::1]", URMachineConfig{}); err != nil {
		t.Fatal("exact IPv6 origin", err)
	}
}
