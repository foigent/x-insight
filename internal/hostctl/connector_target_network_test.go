package hostctl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type targetNetworkRunner struct {
	commands [][]string
	reloaded bool
	restarts int
}

func (r *targetNetworkRunner) Run(_ context.Context, c Command) (CommandResult, error) {
	if c.Name != "systemctl" {
		return CommandResult{}, fmt.Errorf("unexpected command %s", c.Name)
	}
	r.commands = append(r.commands, append([]string(nil), c.Args...))
	if reflect.DeepEqual(c.Args, []string{"daemon-reload"}) {
		r.reloaded = true
		return CommandResult{}, nil
	}
	if reflect.DeepEqual(c.Args, []string{"restart", connectorControllerUnitName}) {
		if !r.reloaded {
			return CommandResult{}, fmt.Errorf("restart before reload")
		}
		r.restarts++
		return CommandResult{}, nil
	}
	if len(c.Args) == 5 && c.Args[0] == "show" && c.Args[2] == "--property" && c.Args[4] == "--value" {
		switch c.Args[3] {
		case "InvocationID":
			return CommandResult{Output: fmt.Sprintf("invocation-%d\n", r.restarts)}, nil
		case "MainPID":
			return CommandResult{Output: "42\n"}, nil
		case "IPAddressAllow":
			if (c.Args[1] == connectorControllerUnitName && r.restarts > 0) ||
				(c.Args[1] == feedWorkerUnitName && r.reloaded) {
				return CommandResult{Output: "localhost 198.18.0.2/32\n"}, nil
			}
			return CommandResult{Output: "localhost\n"}, nil
		}
	}
	return CommandResult{}, fmt.Errorf("unexpected systemctl arguments %v", c.Args)
}

func newTargetNetworkFixture(t *testing.T) (*Engine, Paths, *targetNetworkRunner) {
	t.Helper()
	e, paths := newConnectorTargetTestEngine(t)
	if err := e.ensureConnectorIdentityEnvironmentFile(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.TrustedTLSProxyEnv,
		[]byte("XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback\nXINSIGHT_EXTERNAL_ORIGIN=https://xinsight.example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r := &targetNetworkRunner{}
	e.Runner = r
	return e, paths, r
}

func TestTargetNetworkSetProvesBothUnitsAndFreshInvocationOnRetry(t *testing.T) {
	e, paths, runner := newTargetNetworkFixture(t)
	const origin = "https://198.18.0.2:9444"
	if err := e.SetConnectorTargetOriginWithNetwork(context.Background(), origin); err != nil {
		t.Fatal(err)
	}
	files, err := e.targetNetworkFiles(origin)
	if err != nil || len(files) != 2 {
		t.Fatalf("files=%v err=%v", files, err)
	}
	for path, body := range files {
		if err := validateExactServiceDropIn(path, body); err != nil {
			t.Fatal(err)
		}
	}
	if !runner.reloaded || runner.restarts != 1 {
		t.Fatalf("network not loaded: %v", runner.commands)
	}
	first := len(runner.commands)
	if err := e.SetConnectorTargetOriginWithNetwork(context.Background(), origin); err != nil {
		t.Fatal(err)
	}
	if runner.restarts != 2 || len(runner.commands) <= first {
		t.Fatalf("same-target replay missed invocation barrier: %v", runner.commands[first:])
	}
	if err := e.ValidateConnectorTargetConfig(); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(paths.ConnectorEnvironment); err != nil || !strings.Contains(string(raw), origin) {
		t.Fatalf("target=%q err=%v", raw, err)
	}
}

func TestTargetNetworkRejectsConflictBeforeTargetWriteAndResumesPartial(t *testing.T) {
	e, paths, _ := newTargetNetworkFixture(t)
	const origin = "https://198.18.0.2:9444"
	files, _ := e.targetNetworkFiles(origin)
	feed := filepath.Join(paths.FeedServiceUnit+".d", targetNetworkDropIn)
	if err := os.MkdirAll(filepath.Dir(feed), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(feed, []byte(managedDirectEgressContent("198.18.0.3/32")), 0644); err != nil {
		t.Fatal(err)
	}
	if err := e.SetConnectorTargetOriginWithNetwork(context.Background(), origin); err == nil {
		t.Fatal("conflicting feed route was accepted")
	}
	if _, err := os.Lstat(paths.ConnectorEnvironment); !os.IsNotExist(err) {
		t.Fatalf("target written before conflict check: %v", err)
	}
	if err := os.Remove(feed); err != nil {
		t.Fatal(err)
	}
	if err := e.SetConnectorTargetOrigin(origin); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(feed, []byte(files[feed]), 0644); err != nil {
		t.Fatal(err)
	}
	if err := e.ValidateConnectorTargetConfig(); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("partial validation=%v", err)
	}
	if err := e.SetConnectorTargetOriginWithNetwork(context.Background(), origin); err != nil {
		t.Fatalf("same-origin completion: %v", err)
	}
	if err := e.ValidateConnectorTargetConfig(); err != nil {
		t.Fatal(err)
	}
}

func TestTargetNetworkRejectsConflictingOldFeedRule(t *testing.T) {
	for _, test := range []struct{ name, cidr string }{
		{"different", "198.18.0.3/32"},
		{"wider", "198.18.0.0/24"},
	} {
		t.Run(test.name, func(t *testing.T) {
			e, paths, _ := newTargetNetworkFixture(t)
			path := filepath.Join(paths.FeedServiceUnit+".d", "40-xinsight-reviewed-source-egress.conf")
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(managedDirectEgressContent(test.cidr)), 0644); err != nil {
				t.Fatal(err)
			}
			if err := e.SetConnectorTargetOriginWithNetwork(context.Background(), "https://198.18.0.2:9444"); err == nil {
				t.Fatal("old conflicting egress was accepted")
			}
			if _, err := os.Lstat(paths.ConnectorEnvironment); !os.IsNotExist(err) {
				t.Fatalf("target written before old-rule conflict check: %v", err)
			}
		})
	}
}

func TestTargetNetworkAcceptsExactOldControllerAndFeedRules(t *testing.T) {
	e, paths, _ := newTargetNetworkFixture(t)
	for _, path := range []string{
		filepath.Join(paths.ControllerServiceUnit+".d", machineDropIn),
		filepath.Join(paths.FeedServiceUnit+".d", "40-xinsight-reviewed-source-egress.conf"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(managedDirectEgressContent("198.18.0.2/32")), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.validateTargetNetworkFiles("https://198.18.0.2:9444"); err != nil {
		t.Fatalf("exact old network rules rejected: %v", err)
	}
}

func TestTargetNetworkResumesPrivateStagingWithoutChangingTarget(t *testing.T) {
	e, paths, _ := newTargetNetworkFixture(t)
	const origin = "https://198.18.0.2:9444"
	if err := e.SetConnectorTargetOrigin(origin); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(paths.ConnectorEnvironment)
	if err != nil {
		t.Fatal(err)
	}
	files, _ := e.targetNetworkFiles(origin)
	controller := filepath.Join(paths.ControllerServiceUnit+".d", targetNetworkDropIn)
	if err := os.MkdirAll(filepath.Dir(controller), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(controller+".next", []byte(files[controller]), 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.SetConnectorTargetOriginWithNetwork(context.Background(), origin); err != nil {
		t.Fatalf("recover owner-only staging: %v", err)
	}
	if err := validateExactServiceDropIn(controller, files[controller]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(controller + ".next"); !os.IsNotExist(err) {
		t.Fatalf("staging remains: %v", err)
	}
	after, err := os.ReadFile(paths.ConnectorEnvironment)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("target changed on replay: %q err=%v", after, err)
	}
}

func TestTargetNetworkRejectsLinkedReferenceBeforeChangingTarget(t *testing.T) {
	e, paths, runner := newTargetNetworkFixture(t)
	const origin = "https://198.18.0.2:9444"
	files, _ := e.targetNetworkFiles(origin)
	controller := filepath.Join(paths.ControllerServiceUnit+".d", targetNetworkDropIn)
	if err := os.MkdirAll(filepath.Dir(controller), 0755); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(t.TempDir(), "original.conf")
	if err := os.WriteFile(original, []byte(files[controller]), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(original, controller); err != nil {
		t.Fatal(err)
	}
	if err := e.SetConnectorTargetOriginWithNetwork(context.Background(), origin); err == nil {
		t.Fatal("shared network reference accepted")
	}
	if _, err := os.Lstat(paths.ConnectorEnvironment); !os.IsNotExist(err) {
		t.Fatalf("target changed before reference validation: %v", err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("invalid reference changed systemd: %v", runner.commands)
	}
}
