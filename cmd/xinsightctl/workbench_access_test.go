package main

import (
	"io"
	"strings"
	"testing"
)

func TestWorkbenchAccessRequiresRootBeforeAnyIO(t *testing.T) {
	for _, args := range [][]string{{"apply", "--file", "/not/read", "--check"}, {"show", "--grant-id", "any"}} {
		err := runWorkbenchAccess(t.Context(), nil, args, io.Discard, false)
		if err == nil || !strings.Contains(err.Error(), "requires root") {
			t.Fatalf("root authority not enforced: %v", err)
		}
	}
}
