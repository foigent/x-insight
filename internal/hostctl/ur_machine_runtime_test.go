package hostctl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type machineRuntimeRunner struct {
	invocation string
	allowed    string
	commands   [][]string
}

func (r *machineRuntimeRunner) Run(_ context.Context, c Command) (CommandResult, error) {
	if c.Name != "systemctl" {
		return CommandResult{}, fmt.Errorf("unexpected command %s", c.Name)
	}
	r.commands = append(r.commands, append([]string(nil), c.Args...))
	for key, value := range map[string]string{
		"InvocationID":   r.invocation,
		"MainPID":        "42",
		"IPAddressAllow": r.allowed,
	} {
		if reflect.DeepEqual(c.Args, []string{"show", connectorControllerUnitName, "--property", key, "--value"}) {
			return CommandResult{Output: value + "\n"}, nil
		}
	}
	if reflect.DeepEqual(c.Args, []string{"daemon-reload"}) ||
		reflect.DeepEqual(c.Args, []string{"--no-block", "restart", connectorControllerUnitName}) {
		return CommandResult{}, nil
	}
	return CommandResult{}, fmt.Errorf("unexpected systemctl arguments %v", c.Args)
}

func TestControllerMachineRuntimeRecoversOriginOnlyPublicationAndRestart(t *testing.T) {
	e, _ := managedCustodyFixture(t)
	if err := e.ensureConnectorIdentityEnvironmentFile(); err != nil {
		t.Fatal(err)
	}
	putManagedFile(t, e.Paths.TrustedTLSProxyEnv, []byte("XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback\nXINSIGHT_EXTERNAL_ORIGIN=https://xinsight.example\n"), 0600)
	if err := e.SetConnectorTargetOrigin("https://8.130.18.85"); err != nil {
		t.Fatal(err)
	}
	runner := &machineRuntimeRunner{invocation: "before", allowed: "127.0.0.0/8 ::1/128 10.0.0.0/8"}
	e.Runner = runner
	id := "xci_AAAAAAAAAAAAAAAAAAAAAA"
	c := URMachineConfig{InstallationID: id, SourceInstance: "source", OrgID: "2", PolicyFile: filepath.Join(e.Paths.EtcDir, "ur-policy.key")}
	putManagedFile(t, c.PolicyFile, []byte("ur-xpm1-"+strings.Repeat("a", 64)), 0600)
	files, err := e.machineFiles("https://8.130.18.85", c)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.stageURMachineRuntime(t.Context(), c, files); err != nil {
		t.Fatal(err)
	}
	for path, body := range files {
		if err := e.publishReference(path, body, 0644); err != nil {
			t.Fatal(err)
		}
	}
	// This is the actual first-interruption shape: origin-only connector.env,
	// new exact drop-in, but no machine fields yet.
	if err := e.ValidateURMachineConfig(); err != nil {
		t.Fatal("interrupted startup validation", err)
	}
	if err := e.SetURMachineConfigForController(t.Context(), c); err != nil {
		t.Fatal("resume publication", err)
	}
	if err := e.EnsureControllerURMachineRuntime(t.Context(), id); !errors.Is(err, ErrURMachineRuntimePending) {
		t.Fatal("old process was considered ready", err)
	}
	if err := e.ProveControllerURMachineRuntime(t.Context(), id); !errors.Is(err, ErrURMachineRuntimePending) {
		t.Fatal("network client bypassed pending restart", err)
	}
	runner.invocation = "after"
	runner.allowed = "127.0.0.0/8 ::1/128 8.130.18.85/32"
	if err := e.EnsureControllerURMachineRuntime(t.Context(), id); err != nil {
		t.Fatal("new invocation proof", err)
	}
	if _, present, err := e.readURMachineRuntimePending(); err != nil || present {
		t.Fatal("runtime receipt not finalized", err)
	}
	if err := e.ProveControllerURMachineRuntime(t.Context(), id); err != nil {
		t.Fatal("ready network client", err)
	}
}

func TestControllerMachineRuntimeRejectsUnboundDropInAndWrongDigest(t *testing.T) {
	e, _ := managedCustodyFixture(t)
	if err := e.ensureConnectorIdentityEnvironmentFile(); err != nil {
		t.Fatal(err)
	}
	putManagedFile(t, e.Paths.TrustedTLSProxyEnv, []byte("XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback\nXINSIGHT_EXTERNAL_ORIGIN=https://xinsight.example\n"), 0600)
	if err := e.SetConnectorTargetOrigin("https://8.130.18.85"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.Paths.ControllerServiceUnit+".d", machineDropIn)
	putManagedFile(t, path, []byte("[Service]\nIPAddressAllow=0.0.0.0/0\n"), 0644)
	if err := e.ValidateURMachineConfig(); err == nil {
		t.Fatal("unbound machine drop-in accepted")
	}
	runner := &machineRuntimeRunner{invocation: "before"}
	e.Runner = runner
	c := URMachineConfig{InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA", SourceInstance: "source", OrgID: "2", PolicyFile: filepath.Join(e.Paths.EtcDir, "ur-policy.key")}
	files, err := e.machineFiles("https://8.130.18.85", c)
	if err != nil || e.stageURMachineRuntime(t.Context(), c, files) != nil {
		t.Fatal("stage fixture", err)
	}
	if err := e.ValidateURMachineConfig(); err == nil {
		t.Fatal("wrong pending machine drop-in digest accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func TestMachineEgressMatchesReducedAddressSet(t *testing.T) {
	for _, test := range []struct {
		name, origin, allowed string
		want                  bool
	}{
		{"public_exact_reordered", "https://198.18.0.1:9443", "198.18.0.1/32 ::1/128 127.0.0.0/8", true},
		{"public_exact_shorthand", "https://198.18.0.1:9443", "localhost 198.18.0.1", true},
		{"ipv4_loopback_reduced", "https://127.0.0.1:9443", "127.0.0.0/8 ::1/128", true},
		{"ipv4_loopback_redundant", "https://127.0.0.1:9443", "localhost 127.0.0.1/32", true},
		{"ipv6_loopback_reduced", "https://[::1]:9443", "::1/128 127.0.0.0/8", true},
		{"ipv6_loopback_redundant", "https://[::1]:9443", "localhost ::1/128", true},
		{"ipv6_public_exact", "https://[2001:db8::1]:9443", "localhost 2001:db8::1/128", true},
		{"old_private_baseline", "https://198.18.0.1:9443", "localhost 10.0.0.0/8", false},
		{"broader_remote_prefix", "https://198.18.0.1:9443", "localhost 198.18.0.0/24", false},
		{"broader_loopback_prefix", "https://198.18.0.1:9443", "127.0.0.0/7 ::1/128 198.18.0.1/32", false},
		{"extra_address", "https://198.18.0.1:9443", "localhost 198.18.0.1/32 198.18.0.2/32", false},
		{"extra_ipv6_address", "https://198.18.0.1:9443", "localhost 198.18.0.1/32 ::2/128", false},
		{"missing_ipv6_loopback", "https://198.18.0.1:9443", "127.0.0.0/8 198.18.0.1/32", false},
		{"any_shortcut", "https://198.18.0.1:9443", "localhost any", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := machineEgressMatches(test.origin, test.allowed)
			if err != nil || got != test.want {
				t.Fatalf("egress proof=%t, error=%v; want %t", got, err, test.want)
			}
		})
	}
}

func TestDisableMachineBindingClearsPendingRuntimeOnlyAfterPublication(t *testing.T) {
	e, _ := managedCustodyFixture(t)
	if err := e.ensureConnectorIdentityEnvironmentFile(); err != nil {
		t.Fatal(err)
	}
	putManagedFile(t, e.Paths.TrustedTLSProxyEnv, []byte("XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback\nXINSIGHT_EXTERNAL_ORIGIN=https://xinsight.example\n"), 0600)
	if err := e.SetConnectorTargetOrigin("https://8.130.18.85"); err != nil {
		t.Fatal(err)
	}
	e.Runner = &machineRuntimeRunner{invocation: "before"}
	c := URMachineConfig{InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA", SourceInstance: "source", OrgID: "2", PolicyFile: filepath.Join(e.Paths.EtcDir, "ur-policy.key")}
	putManagedFile(t, c.PolicyFile, []byte("ur-xpm1-"+strings.Repeat("a", 64)), 0600)
	if err := e.SetURMachineConfigForController(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	if err := e.DisableURMachineConfig(true); err != nil {
		t.Fatal("disable check", err)
	}
	if _, present, err := e.readURMachineRuntimePending(); err != nil || !present {
		t.Fatal("disable check changed pending runtime", err)
	}
	if err := e.DisableURMachineConfig(false); err != nil {
		t.Fatal("disable", err)
	}
	if _, present, err := e.readURMachineRuntimePending(); err != nil || present {
		t.Fatal("retired binding retained pending runtime", err)
	}
}

func TestDisableMachineBindingRetiresInterruptedDropInBeforeClearingPending(t *testing.T) {
	e, _ := managedCustodyFixture(t)
	if err := e.ensureConnectorIdentityEnvironmentFile(); err != nil {
		t.Fatal(err)
	}
	putManagedFile(t, e.Paths.TrustedTLSProxyEnv, []byte("XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback\nXINSIGHT_EXTERNAL_ORIGIN=https://xinsight.example\n"), 0600)
	if err := e.SetConnectorTargetOrigin("https://8.130.18.85"); err != nil {
		t.Fatal(err)
	}
	e.Runner = &machineRuntimeRunner{invocation: "before"}
	c := URMachineConfig{InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA", SourceInstance: "source", OrgID: "2", PolicyFile: filepath.Join(e.Paths.EtcDir, "ur-policy.key")}
	putManagedFile(t, c.PolicyFile, []byte("ur-xpm1-"+strings.Repeat("a", 64)), 0600)
	files, err := e.machineFiles("https://8.130.18.85", c)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.withOperationLock(func() error {
		if err := e.stageURMachineRuntime(t.Context(), c, files); err != nil {
			return err
		}
		for path, body := range files {
			if err := e.publishReference(path, body, 0644); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The pending receipt and drop-in exist, but the machine fields have not
	// been published to connector.env. Startup validation accepts this state.
	if err := e.ValidateURMachineConfig(); err != nil {
		t.Fatal("interrupted publication is invalid", err)
	}
	for path := range files {
		putManagedFile(t, path+".disabled", []byte("different retired configuration\n"), 0644)
		before := snapshotManagedFiles(t, e.Paths.Root)
		for _, check := range []bool{true, false} {
			if err := e.DisableURMachineConfig(check); err == nil {
				t.Fatal("disable accepted a conflicting retired configuration")
			}
			if after := snapshotManagedFiles(t, e.Paths.Root); !bytes.Equal(before, after) {
				t.Fatal("rejected disable changed interrupted publication")
			}
		}
		if err := os.Remove(path + ".disabled"); err != nil {
			t.Fatal(err)
		}
	}
	before := snapshotManagedFiles(t, e.Paths.Root)
	if err := e.DisableURMachineConfig(true); err != nil {
		t.Fatal("disable check", err)
	}
	if after := snapshotManagedFiles(t, e.Paths.Root); !bytes.Equal(before, after) {
		t.Fatal("disable check changed interrupted publication")
	}
	if err := e.DisableURMachineConfig(false); err != nil {
		t.Fatal("disable interrupted publication", err)
	}
	if err := e.ValidateURMachineConfig(); err != nil {
		t.Fatal("disabled configuration fails startup validation", err)
	}
	if _, present, err := e.readURMachineRuntimePending(); err != nil || present {
		t.Fatal("disabled configuration retained pending runtime", err)
	}
	for path := range files {
		if exists, err := fileExists(path); err != nil || exists {
			t.Fatal("disabled configuration retained active machine drop-in", err)
		}
	}
}

func TestControllerWritableSystemdDirectoriesAreExactAndSafe(t *testing.T) {
	e, _ := managedCustodyFixture(t)
	if err := e.ensureControlDirectories(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{e.Paths.ControllerServiceUnit + ".d", e.Paths.SourceFactsServiceUnit + ".d", e.Paths.ServiceDropInDir} {
		if err := e.validateReferenceParent(filepath.Join(path, "probe")); err != nil {
			t.Fatal(path, err)
		}
	}
	if err := os.Remove(e.Paths.ControllerServiceUnit + ".d"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(e.Paths.EtcDir, e.Paths.ControllerServiceUnit+".d"); err != nil {
		t.Fatal(err)
	}
	if err := e.ensureControlDirectories(); err == nil {
		t.Fatal("linked writable systemd directory accepted")
	}
}
