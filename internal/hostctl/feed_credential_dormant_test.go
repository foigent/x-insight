package hostctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestFeedCredentialDormantMissingTargetNetworkDoesNotAuthenticateOrMutate(t *testing.T) {
	engine, paths, _ := newFeedCredentialFixture(t)
	runner := &workerLastRunner{loaded: true}
	engine.Runner = runner
	ctx := context.Background()
	const origin = "https://198.18.0.2:9444"
	cfg, err := engine.StageManagedFeedConfig(ctx, origin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = engine.PromoteManagedFeedConfig(ctx, cfg.ConfigSHA256); err != nil {
		t.Fatal(err)
	}
	registration, err := engine.PrepareFeedCredential(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(paths.ConnectorEnvironment, []byte(connectorTargetEnvironmentContents(origin)), 0600); err != nil {
		t.Fatal(err)
	}
	authCalls := 0
	engine.FeedCredentialAuthCheck = func(context.Context, string) error { authCalls++; return nil }
	before := map[string][]byte{}
	for _, path := range []string{paths.FeedCredentialState, paths.FeedCredentialNext} {
		before[path], err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = engine.PromoteFeedCredentialDormant(ctx, registration.OperationID, registration.VerifierSHA256)
	if err == nil || !strings.Contains(err.Error(), "target network publication is incomplete") {
		t.Fatalf("missing controller network gate: %v", err)
	}
	if authCalls != 0 || len(runner.effects()) != 0 {
		t.Fatal("unprepared controller performed authentication or activated a worker")
	}
	for path, value := range before {
		after, err := os.ReadFile(path)
		if err != nil || !reflect.DeepEqual(value, after) {
			t.Fatal("network rejection changed the prepared credential state")
		}
	}
}

func TestFeedCredentialDormantPromotionKeepsWorkerOffUntilLast(t *testing.T) {
	engine, paths, _ := newFeedCredentialFixture(t)
	runner := &workerLastRunner{loaded: true}
	engine.Runner = runner
	ctx := context.Background()

	config, err := engine.StageManagedFeedConfig(ctx, "https://unirouter.example")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.PromoteManagedFeedConfig(ctx, config.ConfigSHA256); err != nil {
		t.Fatal(err)
	}
	registration, err := engine.PrepareFeedCredential(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	promotion, err := engine.PromoteFeedCredentialDormant(
		ctx,
		registration.OperationID,
		registration.VerifierSHA256,
	)
	if err != nil {
		t.Fatal(err)
	}
	if promotion.Registration != registration || promotion.Replayed ||
		!hostctlSHA256Pattern.MatchString(promotion.PausedAuthReceiptSHA256) ||
		!hostctlSHA256Pattern.MatchString(promotion.DormantCredentialReceiptSHA256) ||
		promotion.Credential.PendingPhase != feedCredentialPendingDormant ||
		!promotion.Credential.ActiveFile.BindingMatches {
		t.Fatalf("promotion=%+v", promotion)
	}
	if runner.enabled || runner.active || len(runner.effects()) != 0 {
		t.Fatalf("dormant promotion changed worker: %+v", runner.commands)
	}
	if _, err := os.Lstat(paths.FeedCredentialNext); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged credential remains after dormant promotion: %v", err)
	}

	replayed, err := engine.PromoteFeedCredentialDormant(
		ctx,
		registration.OperationID,
		registration.VerifierSHA256,
	)
	if err != nil || !replayed.Replayed || replayed.PausedAuthReceiptSHA256 != promotion.PausedAuthReceiptSHA256 ||
		replayed.DormantCredentialReceiptSHA256 != promotion.DormantCredentialReceiptSHA256 {
		t.Fatalf("dormant replay=%+v err=%v", replayed, err)
	}

	activation, err := engine.ActivateFeedWorkerLast(
		ctx,
		registration.OperationID,
		registration.VerifierSHA256,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !activation.Present || !activation.Active || !activation.Enabled || activation.Replayed ||
		!reflect.DeepEqual(runner.effects(), [][]string{
			{"enable", feedWorkerUnitName},
			{"start", feedWorkerUnitName},
		}) {
		t.Fatalf("activation=%+v commands=%+v", activation, runner.commands)
	}
	commandCount := len(runner.commands)
	activation, err = engine.ActivateFeedWorkerLast(
		ctx,
		registration.OperationID,
		registration.VerifierSHA256,
	)
	if err != nil || !activation.Replayed || len(runner.commands) != commandCount+3 {
		// Replay performs only the three read-only systemd property reads.
		t.Fatalf("activation replay=%+v commands=%+v err=%v", activation, runner.commands, err)
	}
	postWorkerPromotion, err := engine.PromoteFeedCredentialDormant(
		ctx,
		registration.OperationID,
		registration.VerifierSHA256,
	)
	if err != nil || !postWorkerPromotion.Replayed ||
		postWorkerPromotion.PausedAuthReceiptSHA256 != promotion.PausedAuthReceiptSHA256 ||
		postWorkerPromotion.DormantCredentialReceiptSHA256 != promotion.DormantCredentialReceiptSHA256 {
		t.Fatalf("post-worker dormant replay=%+v err=%v", postWorkerPromotion, err)
	}

	encoded, err := json.Marshal(struct {
		Promotion  FeedCredentialPromotionReport `json:"promotion"`
		Activation FeedWorkerActivationReport    `json:"activation"`
	}{promotion, activation})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(paths.FeedCredentialFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), string(raw)) {
		t.Fatal("public dormant/worker reports contain the raw receiver credential")
	}
}

func TestFeedWorkerLastRecoversAmbiguousStartByReadback(t *testing.T) {
	engine, _, _ := newFeedCredentialFixture(t)
	runner := &workerLastRunner{loaded: true}
	engine.Runner = runner
	ctx := context.Background()
	config, err := engine.StageManagedFeedConfig(ctx, "https://unirouter.example")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.PromoteManagedFeedConfig(ctx, config.ConfigSHA256); err != nil {
		t.Fatal(err)
	}
	registration, err := engine.PrepareFeedCredential(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.PromoteFeedCredentialDormant(ctx, registration.OperationID, registration.VerifierSHA256); err != nil {
		t.Fatal(err)
	}
	failOnce := true
	engine.FeedCredentialPhaseHook = func(phase string) error {
		if phase == "worker_activation_readback" && failOnce {
			failOnce = false
			return errors.New("simulated lost response")
		}
		return nil
	}
	if _, err := engine.ActivateFeedWorkerLast(ctx, registration.OperationID, registration.VerifierSHA256); err == nil ||
		!strings.Contains(err.Error(), "after readback") {
		t.Fatalf("expected ambiguous worker activation, got %v", err)
	}
	if !runner.active || !runner.enabled || len(runner.effects()) != 2 {
		t.Fatalf("ambiguous effects=%+v", runner.commands)
	}
	engine.FeedCredentialPhaseHook = nil
	activation, err := engine.ActivateFeedWorkerLast(ctx, registration.OperationID, registration.VerifierSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if activation.Replayed || !activation.Active || !activation.Enabled || len(runner.effects()) != 2 {
		t.Fatalf("recovered activation=%+v commands=%+v", activation, runner.commands)
	}
}

func TestFeedWorkerLastRejectsManagedConfigDriftBeforeSystemd(t *testing.T) {
	engine, paths, _ := newFeedCredentialFixture(t)
	runner := &workerLastRunner{loaded: true}
	engine.Runner = runner
	ctx := context.Background()
	config, err := engine.StageManagedFeedConfig(ctx, "https://unirouter.example")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.PromoteManagedFeedConfig(ctx, config.ConfigSHA256); err != nil {
		t.Fatal(err)
	}
	registration, err := engine.PrepareFeedCredential(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.PromoteFeedCredentialDormant(ctx, registration.OperationID, registration.VerifierSHA256); err != nil {
		t.Fatal(err)
	}
	drifted, err := buildManagedFeedConfig("https://other-unirouter.example")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.FeedEnvironmentFile, drifted.contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ActivateFeedWorkerLast(ctx, registration.OperationID, registration.VerifierSHA256); err == nil ||
		!strings.Contains(err.Error(), "changed after dormant") {
		t.Fatalf("managed config drift error=%v", err)
	}
	if len(runner.effects()) != 0 || runner.active || runner.enabled {
		t.Fatalf("config drift reached worker effects: %+v", runner.commands)
	}
}

type workerLastRunner struct {
	commands []Command
	loaded   bool
	active   bool
	enabled  bool
}

func (runner *workerLastRunner) Run(_ context.Context, command Command) (CommandResult, error) {
	runner.commands = append(runner.commands, command)
	if command.Name != "systemctl" {
		return CommandResult{}, fmt.Errorf("unexpected command %s", command.Name)
	}
	switch {
	case reflect.DeepEqual(command.Args, []string{"show", feedWorkerUnitName, "--property", "LoadState", "--value"}):
		if runner.loaded {
			return CommandResult{Output: "loaded\n"}, nil
		}
		return CommandResult{Output: "not-found\n"}, nil
	case reflect.DeepEqual(command.Args, []string{"show", feedWorkerUnitName, "--property", "ActiveState", "--value"}):
		if runner.active {
			return CommandResult{Output: "active\n"}, nil
		}
		return CommandResult{Output: "inactive\n"}, nil
	case reflect.DeepEqual(command.Args, []string{"show", feedWorkerUnitName, "--property", "UnitFileState", "--value"}):
		if runner.enabled {
			return CommandResult{Output: "enabled\n"}, nil
		}
		return CommandResult{Output: "disabled\n"}, nil
	case reflect.DeepEqual(command.Args, []string{"enable", feedWorkerUnitName}):
		runner.enabled = true
		return CommandResult{}, nil
	case reflect.DeepEqual(command.Args, []string{"start", feedWorkerUnitName}):
		runner.active = true
		return CommandResult{}, nil
	default:
		return CommandResult{}, fmt.Errorf("unexpected systemctl args %v", command.Args)
	}
}

func (runner *workerLastRunner) effects() [][]string {
	var effects [][]string
	for _, command := range runner.commands {
		if len(command.Args) > 0 && (command.Args[0] == "enable" || command.Args[0] == "start") {
			effects = append(effects, append([]string(nil), command.Args...))
		}
	}
	return effects
}
