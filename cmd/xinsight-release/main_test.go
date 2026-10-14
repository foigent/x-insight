package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestReleaseToolReportsBootstrapIdentity(t *testing.T) {
	var output bytes.Buffer
	if err := run([]string{"version", "--json"}, &output); err != nil {
		t.Fatalf("version: %v", err)
	}
	if !strings.Contains(output.String(), `"product_name": "x-insight"`) {
		t.Fatalf("output=%s", output.String())
	}
}

func TestRunRejectsIncompleteCommandsWithoutWritingOutput(t *testing.T) {
	for _, args := range [][]string{{}, {"unknown"}, {"keygen"}, {"pack"}, {"verify"}, {"extract"}, {"campaign-authority"}} {
		var output bytes.Buffer
		if err := run(args, &output); err == nil {
			t.Fatalf("args=%v unexpectedly succeeded", args)
		}
		if output.Len() != 0 {
			t.Fatalf("args=%v output=%q", args, output.String())
		}
	}
}
