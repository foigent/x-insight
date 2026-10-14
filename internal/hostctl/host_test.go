package hostctl

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestWaitForLoopbackListenerRetriesStartupGap(t *testing.T) {
	runner := &scriptedListenerRunner{outputs: []string{
		"",
		"",
		"LISTEN 0 4096 127.0.0.1:8080 0.0.0.0:*\n",
	}}
	waits := 0
	engine := &Engine{
		Runner: runner,
		Wait: func(context.Context, time.Duration) error {
			waits++
			return nil
		},
	}
	if err := engine.waitForLoopbackListener(context.Background()); err != nil {
		t.Fatalf("wait for listener: %v", err)
	}
	if runner.calls != 3 || waits != 2 {
		t.Fatalf("listener calls=%d waits=%d", runner.calls, waits)
	}
}

func TestWaitForLoopbackListenerRejectsPublicBindingImmediately(t *testing.T) {
	runner := &scriptedListenerRunner{outputs: []string{
		"LISTEN 0 4096 0.0.0.0:8080 0.0.0.0:*\n",
	}}
	waits := 0
	engine := &Engine{
		Runner: runner,
		Wait: func(context.Context, time.Duration) error {
			waits++
			return nil
		},
	}
	if err := engine.waitForLoopbackListener(context.Background()); err == nil {
		t.Fatal("expected public listener rejection")
	}
	if runner.calls != 1 || waits != 0 {
		t.Fatalf("public listener calls=%d waits=%d", runner.calls, waits)
	}
}

func TestWaitForActivationProofRetriesTransientReadiness(t *testing.T) {
	attempts := 0
	waits := 0
	engine := &Engine{Wait: func(context.Context, time.Duration) error {
		waits++
		return nil
	}}
	err := engine.waitForActivationProof(context.Background(), func() error {
		attempts++
		if attempts < 3 {
			return errors.New("not ready")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("wait for activation proof: %v", err)
	}
	if attempts != 3 || waits != 2 {
		t.Fatalf("activation attempts=%d waits=%d", attempts, waits)
	}
}

type scriptedListenerRunner struct {
	outputs []string
	calls   int
}

func (runner *scriptedListenerRunner) Run(_ context.Context, command Command) (CommandResult, error) {
	if command.Name != "ss" || len(command.Args) != 1 || command.Args[0] != "-ltnH" {
		return CommandResult{}, fmt.Errorf("unexpected command: %+v", command)
	}
	if runner.calls >= len(runner.outputs) {
		return CommandResult{}, fmt.Errorf("listener script exhausted")
	}
	output := runner.outputs[runner.calls]
	runner.calls++
	return CommandResult{Output: output}, nil
}
