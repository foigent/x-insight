package hostctl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/connectorcontrol"
)

func TestManagedFeedConfigStagePromoteReadbackIsDeterministic(t *testing.T) {
	engine, paths, runner := newFeedCredentialFixture(t)
	const origin = "https://unirouter.example.invalid"

	staged, err := engine.StageManagedFeedConfig(context.Background(), origin)
	if err != nil {
		t.Fatalf("stage managed config: %v", err)
	}
	if staged.Status != "staged" || staged.Active || !staged.Staged || staged.Replayed ||
		staged.TrustedOrigin != origin ||
		staged.Endpoint != origin+config.UniRouterCompletedExecutionsPath ||
		!hostctlSHA256Pattern.MatchString(staged.ConfigSHA256) || staged.NetworkBinding == nil ||
		staged.NetworkBindingSHA256 != staged.NetworkBinding.BindingSHA256 ||
		staged.NetworkBinding.Transport != connectorcontrol.ReceiverNetworkTransport ||
		staged.NetworkBinding.Route != connectorcontrol.ReceiverFeedRoute ||
		!hostctlSHA256Pattern.MatchString(staged.NetworkBinding.BindingSHA256) {
		t.Fatalf("staged report=%+v", staged)
	}
	expectedBindingSHA256, _, err := connectorcontrol.CanonicalSHA256(managedFeedNetworkBindingAuthority{
		SchemaVersion: managedFeedNetworkBindingSchema,
		TrustedOrigin: origin,
		Transport:     connectorcontrol.ReceiverNetworkTransport,
		Route:         connectorcontrol.ReceiverFeedRoute,
	})
	if err != nil || staged.NetworkBindingSHA256 != expectedBindingSHA256 {
		t.Fatalf("network binding digest=%s expected=%s err=%v", staged.NetworkBindingSHA256, expectedBindingSHA256, err)
	}
	if _, err := os.Lstat(paths.FeedEnvironmentFile); !os.IsNotExist(err) {
		t.Fatalf("stage changed active config: %v", err)
	}
	contents, err := os.ReadFile(paths.FeedEnvironmentFile + ".next")
	if err != nil {
		t.Fatal(err)
	}
	expected := strings.Join([]string{
		config.UniRouterFeedEndpointEnv + "=" + origin + config.UniRouterCompletedExecutionsPath,
		config.UniRouterFeedPollIntervalEnv + "=5s",
		config.UniRouterFeedAllowHTTPEnv + "=false",
		config.UniRouterFeedMappingModeEnv + "=legacy",
		"",
	}, "\n")
	if string(contents) != expected {
		t.Fatalf("managed config bytes=%q", contents)
	}
	stagedReplay, err := engine.StageManagedFeedConfig(context.Background(), origin)
	if err != nil || !stagedReplay.Replayed || stagedReplay.ConfigSHA256 != staged.ConfigSHA256 {
		t.Fatalf("stage replay=%+v err=%v", stagedReplay, err)
	}

	active, err := engine.PromoteManagedFeedConfig(context.Background(), staged.ConfigSHA256)
	if err != nil {
		t.Fatalf("promote managed config: %v", err)
	}
	if active.Status != "active" || !active.Active || active.Staged || active.Replayed ||
		active.ConfigSHA256 != staged.ConfigSHA256 ||
		active.NetworkBinding == nil ||
		active.NetworkBindingSHA256 != staged.NetworkBindingSHA256 ||
		active.NetworkBinding.BindingSHA256 != staged.NetworkBinding.BindingSHA256 {
		t.Fatalf("active report=%+v", active)
	}
	status, err := engine.ManagedFeedConfigStatus(context.Background())
	if err != nil || status.ConfigSHA256 != active.ConfigSHA256 || !status.Active {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	replayed, err := engine.PromoteManagedFeedConfig(context.Background(), staged.ConfigSHA256)
	if err != nil || !replayed.Replayed || replayed.ConfigSHA256 != staged.ConfigSHA256 {
		t.Fatalf("promote replay=%+v err=%v", replayed, err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("managed config invoked commands: %+v", runner.commands)
	}
}

func TestManagedFeedConfigRejectsFreeSemanticInputsAndTamper(t *testing.T) {
	for _, origin := range []string{
		"http://unirouter.example.invalid",
		"https://unirouter.example.invalid/",
		"https://user@unirouter.example.invalid",
		"https://unirouter.example.invalid?route=other",
	} {
		engine, _, _ := newFeedCredentialFixture(t)
		if _, err := engine.StageManagedFeedConfig(context.Background(), origin); err == nil {
			t.Fatalf("accepted noncanonical trusted origin %q", origin)
		}
	}

	engine, paths, runner := newFeedCredentialFixture(t)
	staged, err := engine.StageManagedFeedConfig(context.Background(), "https://unirouter.example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	stagedPath := paths.FeedEnvironmentFile + ".next"
	contents, err := os.ReadFile(stagedPath)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(contents), "MAPPING_MODE=legacy", "MAPPING_MODE=shadow", 1)
	if err := os.WriteFile(stagedPath, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.PromoteManagedFeedConfig(context.Background(), staged.ConfigSHA256); err == nil ||
		!strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("tampered managed config error=%v", err)
	}
	if _, err := os.Lstat(paths.FeedEnvironmentFile); !os.IsNotExist(err) {
		t.Fatalf("tamper crossed promotion barrier: %v", err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("tamper path invoked commands: %+v", runner.commands)
	}
}

func TestManagedFeedConfigDoesNotReplaceDifferentActiveAuthority(t *testing.T) {
	engine, paths, runner := newFeedCredentialFixture(t)
	first, err := engine.StageManagedFeedConfig(context.Background(), "https://first.example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.PromoteManagedFeedConfig(context.Background(), first.ConfigSHA256); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(paths.FeedEnvironmentFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.StageManagedFeedConfig(
		context.Background(),
		"https://second.example.invalid",
	); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("different active authority error=%v", err)
	}
	after, err := os.ReadFile(paths.FeedEnvironmentFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("different authority changed active managed config")
	}
	if _, err := os.Lstat(paths.FeedEnvironmentFile + ".next"); !os.IsNotExist(err) {
		t.Fatalf("different authority created staged config: %v", err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("different authority invoked commands: %+v", runner.commands)
	}
}

func TestManagedFeedConfigPromoteRecoversAtomicRenameLostResponse(t *testing.T) {
	engine, paths, _ := newFeedCredentialFixture(t)
	staged, err := engine.StageManagedFeedConfig(context.Background(), "https://unirouter.example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(paths.FeedEnvironmentFile+".next", paths.FeedEnvironmentFile); err != nil {
		t.Fatal(err)
	}
	if err := syncDirectory(filepath.Dir(paths.FeedEnvironmentFile)); err != nil {
		t.Fatal(err)
	}
	recovered, err := engine.PromoteManagedFeedConfig(context.Background(), staged.ConfigSHA256)
	if err != nil || !recovered.Replayed || !recovered.Active || recovered.Staged {
		t.Fatalf("recovered=%+v err=%v", recovered, err)
	}
}
