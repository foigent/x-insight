package buildinfo_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/buildinfo"
)

func TestVersionOutputContainsCompatibilityIdentity(t *testing.T) {
	var text bytes.Buffer
	if err := buildinfo.WriteText(&text); err != nil {
		t.Fatalf("write text: %v", err)
	}
	for _, expected := range []string{
		"x-insight 0.2.0-dev",
		"db_schema=46..46",
		"postgres=16..16",
		"config=xinsight.config.v1",
		"api=xinsight.investigation.v1",
		"access_api=xinsight.access.v1",
		"manual=unreleased/manual-r0",
	} {
		if !strings.Contains(text.String(), expected) {
			t.Fatalf("text=%q missing=%q", text.String(), expected)
		}
	}

	var encoded bytes.Buffer
	if err := buildinfo.WriteJSON(&encoded); err != nil {
		t.Fatalf("write JSON: %v", err)
	}
	var info buildinfo.Info
	if err := json.Unmarshal(encoded.Bytes(), &info); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if info.ProductName != "x-insight" ||
		info.DatabaseSchemaMin != "46" || info.DatabaseSchemaMax != "46" ||
		info.AccessAPIVersion != "xinsight.access.v1" ||
		info.ManualVersion == "" {
		t.Fatalf("info=%+v", info)
	}
}

func TestHandleArgsSupportsTextAndJSON(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"version"}, {"version", "--json"}} {
		var output bytes.Buffer
		handled, err := buildinfo.HandleArgs(args, &output)
		if err != nil || !handled || output.Len() == 0 {
			t.Fatalf("args=%v handled=%t output=%q err=%v", args, handled, output.String(), err)
		}
	}
	if handled, err := buildinfo.HandleArgs([]string{"serve"}, &bytes.Buffer{}); err != nil || handled {
		t.Fatalf("unrelated handled=%t err=%v", handled, err)
	}
}
