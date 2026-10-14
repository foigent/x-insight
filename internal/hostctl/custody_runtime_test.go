package hostctl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/reviewbody"
)

type loadedConsumerRunner struct {
	invocation string
	pid        int
	active     bool
	calls      int
}

func (r *loadedConsumerRunner) Run(_ context.Context, c Command) (CommandResult, error) {
	r.calls++
	if c.Name != "systemctl" || len(c.Args) != 5 || c.Args[0] != "show" {
		return CommandResult{}, fmt.Errorf("unexpected runtime mutation: %v", c.Args)
	}
	switch c.Args[3] {
	case "ActiveState":
		if r.active {
			return CommandResult{Output: "active\n"}, nil
		}
		return CommandResult{Output: "inactive\n"}, nil
	case "InvocationID":
		return CommandResult{Output: r.invocation + "\n"}, nil
	case "MainPID":
		return CommandResult{Output: fmt.Sprintln(r.pid)}, nil
	}
	return CommandResult{}, fmt.Errorf("unexpected property")
}

type loadedConsumerTransport struct{ proof reviewbody.LoadedProof }

func (r loadedConsumerTransport) RoundTrip(*http.Request) (*http.Response, error) {
	raw, _ := json.Marshal(map[string]any{"status": "ready", "review_body_loaded": r.proof})
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
}
func TestActualLoadedCustodyRejectsSameIDWrongBytesAndStaleProcesses(t *testing.T) {
	e, c := managedCustodyFixture(t)
	if err := e.SetReviewBodyConfig(c, false); err != nil {
		t.Fatal(err)
	}
	ring, err := reviewbody.LoadKeyRingFromFile(c.KeyID, c.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	inv := strings.Repeat("a", 32)
	proof, err := ring.LoadedProof("xinsight.service", inv, 1234)
	if err != nil {
		t.Fatal(err)
	}
	runner := &loadedConsumerRunner{invocation: inv, pid: 1234, active: true}
	e.Runner = runner
	e.HTTPClient = &http.Client{Transport: loadedConsumerTransport{proof}}
	before := snapshotManagedFiles(t, e.Paths.Root)
	if err = e.ReviewBodyLoaded(t.Context()); err != nil {
		t.Fatal("legitimate no-source main capability", err)
	}
	if err = e.EnsureReviewBodyLoaded(t.Context()); err != nil {
		t.Fatal("already loaded routine check restarted", err)
	}
	if string(before) != string(snapshotManagedFiles(t, e.Paths.Root)) {
		t.Fatal("unchanged capability created journal/config")
	}
	putManagedFile(t, c.KeyFile, []byte(strings.Repeat("43", 32)+"\n"), 0600)
	if e.ReviewBodyLoaded(t.Context()) == nil {
		t.Fatal("same key ID with different actual bytes accepted")
	}
	putManagedFile(t, c.KeyFile, []byte(strings.Repeat("42", 32)+"\n"), 0600)
	runner.invocation = strings.Repeat("b", 32)
	if e.ReviewBodyLoaded(t.Context()) == nil {
		t.Fatal("stale invocation accepted")
	}
	runner.invocation = inv
	runner.pid++
	if e.ReviewBodyLoaded(t.Context()) == nil {
		t.Fatal("stale PID accepted")
	}
	runner.pid--
	runner.active = false
	if e.ReviewBodyLoaded(t.Context()) == nil {
		t.Fatal("inactive process accepted")
	}
	runner.active = true
	putManagedFile(t, e.Paths.SourceFactsConfig, []byte("{}"), 0600)
	if e.ReviewBodyLoaded(t.Context()) == nil {
		t.Fatal("configured source without loaded proof accepted")
	}
	worker, err := ring.LoadedProof(sourceFactsWorkerUnit, inv, runner.pid)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(worker)
	path := filepath.Join(e.Paths.Root, reviewbody.WorkerLoadedProofPath)
	putManagedFile(t, path, raw, 0600)
	if err = e.ReviewBodyLoaded(t.Context()); err != nil {
		t.Fatal("configured current source proof", err)
	}
	if err = os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	if err = e.ReviewBodyLoaded(t.Context()); err != nil {
		t.Fatal("group-readable source proof", err)
	}
	for _, mode := range []os.FileMode{0660, 0644, 0602} {
		if err = os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if e.ReviewBodyLoaded(t.Context()) == nil {
			t.Fatalf("unsafe source proof mode %04o accepted", mode)
		}
	}
	if err = os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	if err = os.Link(path, path+".linked"); err != nil {
		t.Fatal(err)
	}
	if e.ReviewBodyLoaded(t.Context()) == nil {
		t.Fatal("hardlinked worker witness accepted")
	}
	if err = os.Remove(path + ".linked"); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path, path+".original"); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(path+".original", path); err != nil {
		t.Fatal(err)
	}
	if e.ReviewBodyLoaded(t.Context()) == nil {
		t.Fatal("symlink worker witness accepted")
	}
}
