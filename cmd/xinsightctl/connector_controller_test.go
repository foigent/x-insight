package main

import (
	"context"
	"strings"
	"testing"
)

func TestConnectorControllerServeIsRootOnlyAndHasNoFreeRuntimeInputs(t *testing.T) {
	t.Parallel()
	calls := 0
	serve := func(context.Context) error {
		calls++
		return nil
	}
	if err := runConnectorController(
		context.Background(), []string{"serve"}, false, serve,
	); err == nil || !strings.Contains(err.Error(), "requires root") || calls != 0 {
		t.Fatalf("non-root err=%v calls=%d", err, calls)
	}
	for _, args := range [][]string{
		nil,
		{"serve", "--socket", "/tmp/arbitrary.sock"},
		{"serve", "--dsn", "postgres://arbitrary"},
		{"run-shell"},
	} {
		if err := runConnectorController(
			context.Background(), args, true, serve,
		); err == nil || !strings.Contains(err.Error(), "connector-controller serve") {
			t.Fatalf("args=%v err=%v", args, err)
		}
	}
	if err := runConnectorController(
		context.Background(), []string{"serve"}, true, serve,
	); err != nil || calls != 1 {
		t.Fatalf("serve err=%v calls=%d", err, calls)
	}
}
