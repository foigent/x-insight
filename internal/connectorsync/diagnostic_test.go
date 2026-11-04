package connectorsync

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestSafeFailureVocabularyAndOriginalCause(t *testing.T) {
	private := "https://private.invalid/?token=SECRET /private/path bearer SECRET"
	original := errors.New(private)
	f := WithFailure("enrollment_list", "authorization", original, "private-project")
	if !errors.Is(f, original) || f.Error() != "enrollment_list:authorization" {
		t.Fatal("classification changed failure", f)
	}
	if got := WithFailure("inspect", "unknown", f); got != f {
		t.Fatal("inner failing boundary lost")
	}
	if got := WithFailure(private, private, original); got.Error() != "unknown:unknown" {
		t.Fatal("unsafe printable value", got)
	}
	var output bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	p := &permit{Installation: "private-installation", Run: "private-run", Unit: "private-unit", Worker: "projects", Revision: 8}
	logFailures(context.Background(), p, errors.Join(f, WithFailure("project_publish", "local_permission", original, "private-second")))
	raw := output.String()
	for _, secret := range []string{private, "SECRET", "private-project", "private-installation", "private-run", "private-unit", "private-second"} {
		if strings.Contains(raw, secret) {
			t.Fatal("diagnostic leaked input")
		}
	}
	if strings.Count(raw, "connector_sync_failure") != 2 || !strings.Contains(raw, `"revision":8`) || !strings.Contains(raw, `"stage":"enrollment_list"`) || !strings.Contains(raw, "sha256:") {
		t.Fatal("missing per-subunit correlation", raw)
	}
	if got := diagnosticCode(errors.Join(ErrChanged, f)); got != "enrollment_list:authorization" {
		t.Fatal("control stop masked another unit failure", got)
	}
	output.Reset()
	logFailures(context.Background(), p, ErrChanged)
	if output.Len() != 0 {
		t.Fatal("control stop logged as source failure")
	}
}
