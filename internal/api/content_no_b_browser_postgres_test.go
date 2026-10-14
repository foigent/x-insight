package api_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const noBBrowserRun = "xi-no-b-browser-20260922-r2"
const noBBrowserOutput = p3Root + "/evidence/" + noBBrowserRun

// Validate the destination before connecting, creating a database or listening.
// Errors deliberately exclude the DSN and all credential-bearing inputs.
func validateNoBBrowserTarget(dsn, output, database string) error {
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() != "127.0.0.1" || u.Port() != "56134" || database == "" || u.Path != "/"+database || output != noBBrowserOutput {
		return errors.New("exact approved no-B browser target required")
	}
	return nil
}

// Opt-in is separate from the normal PG selector: an ordinary test run never
// enters a browser wait. No producer, B documents, B grants or credential files.
func TestContentI01WithoutBBrowserHarness(t *testing.T) {
	if os.Getenv("XI_NO_B_BROWSER") != "1" {
		t.Skip("explicit no-B browser opt-in required")
	}
	p3Check(t, os.Getenv("XINSIGHT_RESPONSIBILITY_LOCAL_TEST") == "1", "responsibility fixture opt-in")
	for _, key := range []string{"XI_J1_INTEROP", "XI_P3_INTEROP", "XINSIGHT_SCOPED_BROWSER", "XINSIGHT_A0_BROWSER_HARNESS", "XINSIGHT_RESPONSIBILITY_BROWSER"} {
		p3Check(t, os.Getenv(key) == "", "exclusive no-B browser mode")
	}
	dsn, output := os.Getenv("XINSIGHT_POSTGRES_DSN"), os.Getenv("XI_NO_B_BROWSER_OUTPUT")
	database := os.Getenv("XI_P3_BASE_DATABASE")
	p3Check(t, validateNoBBrowserTarget(dsn, output, database) == nil, "approved no-B browser destination")
	cfg, err := pgx.ParseConfig(dsn)
	p3Check(t, err == nil && cfg.Host == "127.0.0.1" && cfg.Port == 56134 && cfg.Database == database && len(cfg.Fallbacks) == 0, "single approved PG endpoint")
	p3Check(t, os.Getenv("XI_P3_DATABASE_OID") != "" && os.Getenv("XI_P3_SYSTEM_ID") != "", "explicit synthetic database identity")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	control, err := pgx.ConnectConfig(ctx, cfg)
	p3Check(t, err == nil, "no-B identity connection")
	var oid, system string
	err = control.QueryRow(ctx, `SELECT (SELECT oid::text FROM pg_database WHERE datname=current_database()),system_identifier::text FROM pg_control_system()`).Scan(&oid, &system)
	closeErr := control.Close(context.Background())
	cancel()
	p3Check(t, err == nil && closeErr == nil && oid == os.Getenv("XI_P3_DATABASE_OID") && system == os.Getenv("XI_P3_SYSTEM_ID"), "approved synthetic cluster/base identity")
	p3PrivateDirectory(t, filepath.Dir(output), false)
	p3Check(t, os.Mkdir(output, 0700) == nil, "fresh no-B evidence directory; never reuse a prior run")
	// Catch graceful termination so testing cleanup closes HTTP/pools and drops
	// only responsibilityDatabase's random database, including failed runs.
	runCtx, stop := signal.NotifyContext(t.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if deadline, ok := t.Deadline(); ok {
		var finish context.CancelFunc
		runCtx, finish = context.WithDeadline(runCtx, deadline.Add(-15*time.Second))
		defer finish()
	}
	f := newScopedContentFixture(t)
	seedScopedContentI01WithoutB(t, f)
	f.assertI01WithoutB(t)
	verifyNoBBrowserAssets(t, f, output)
	// Normal tests retain their deterministic clock. Browser interactions use
	// wall time for session, reading and content expiry while the harness is open.
	f.mu.Lock()
	f.browserClock = true
	f.mu.Unlock()
	var executionID string
	p3Check(t, f.admin.QueryRowContext(f.ctx, `SELECT execution_id FROM workbench_economic_facts WHERE source_system_id=$1::uuid AND request_id=$2`, f.sourceID, f.choice.Reference.RequestID).Scan(&executionID) == nil, "single I01 execution identity")
	p3JSON(t, filepath.Join(output, "ready.json"), map[string]any{
		"base_url": f.server.URL, "username": "scoped.content",
		"source_system_id": f.sourceID, "source_instance": f.collection.SourceInstance,
		"org_id": f.collection.OrgID, "project_id": f.collection.ProjectID,
		"source_project_ref": "project-" + f.collection.ProjectID, "request_id": f.choice.Reference.RequestID, "execution_id": executionID,
		"period_start_inclusive": f.base.Add(-2 * time.Hour).Format(time.RFC3339Nano), "period_end_exclusive": f.base.Add(time.Minute).Format(time.RFC3339Nano),
		"i01_facts": 1, "b_observation_documents": 0, "b_legacy_documents": 0, "b_grants": 0,
	})
	p3Check(t, waitNoBBrowserStop(runCtx, output) == nil, "no-B browser cancelled or invalid stop marker")
	// The browser driver must finish its requests before acknowledging stop.
	// Reuse the exact API proof/reopen/late revoke suite rather than copying it.
	f.mu.Lock()
	f.browserClock = false
	f.apiOffset = time.Since(f.base) + time.Second
	f.mu.Unlock()
	verifyScopedContentI01WithoutB(t, f)
	p3JSON(t, filepath.Join(output, "post-stop.json"), map[string]any{"i01_facts": 1, "b_observation_documents": 0, "b_legacy_documents": 0, "b_grants": 0, "api_read_adopt_reopen_late_revoke": "passed"})
	// Success here still relies on t.Cleanup completing the precise DB drop.
}

func waitNoBBrowserStop(ctx context.Context, output string) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := os.Lstat(filepath.Join(output, "stop"))
		if err == nil {
			if !info.Mode().IsRegular() || info.Size() != 0 {
				return errors.New("empty regular stop marker required")
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return errors.New("cannot inspect stop marker")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func verifyNoBBrowserAssets(t *testing.T, f *scopedContentFixture, output string) {
	t.Helper()
	raw, err := os.ReadFile("../../internal/webui/dist/asset-manifest.json")
	p3Check(t, err == nil, "candidate asset manifest")
	var manifest struct {
		Entries []struct {
			Path string `json:"path"`
			Size int    `json:"size_bytes"`
			Hash string `json:"sha256"`
		} `json:"entries"`
	}
	p3Check(t, json.Unmarshal(raw, &manifest) == nil && len(manifest.Entries) > 0, "candidate asset entries")
	index := false
	for _, entry := range manifest.Entries {
		path := "/" + entry.Path
		if entry.Path == "index.html" {
			path = "/workbench/runtime"
			index = true
		}
		response, err := f.client.Get(f.server.URL + path)
		p3Check(t, err == nil, "embedded browser asset response")
		body, err := io.ReadAll(io.LimitReader(response.Body, int64(entry.Size)+1))
		response.Body.Close()
		hash := sha256.Sum256(body)
		p3Check(t, err == nil && response.StatusCode == 200 && len(body) == entry.Size && "sha256:"+hex.EncodeToString(hash[:]) == entry.Hash, "embedded browser asset matches candidate manifest")
	}
	p3Check(t, index, "embedded runtime index checked")
	p3JSON(t, filepath.Join(output, "embedded-assets.json"), manifest)
}

func TestNoBBrowserTargetGuard(t *testing.T) {
	good := "postgres://synthetic:unused@127.0.0.1:56134/approved?sslmode=disable"
	if err := validateNoBBrowserTarget(good, noBBrowserOutput, "approved"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, dsn, output, database string }{
		{"port", "postgres://127.0.0.1:5432/approved", noBBrowserOutput, "approved"},
		{"host", "postgres://localhost:56134/approved", noBBrowserOutput, "approved"},
		{"database", good, noBBrowserOutput, "other"},
		{"missing_database", good, noBBrowserOutput, ""},
		{"other_run", good, p3Root + "/evidence/p3-j1-content-20260921-r2", "approved"},
		{"old_run", good, p3Root + "/evidence/xi-no-b-browser-20260922-r1", "approved"},
		{"malformed", "%", noBBrowserOutput, "approved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if validateNoBBrowserTarget(tc.dsn, tc.output, tc.database) == nil {
				t.Fatal("unapproved target accepted")
			}
		})
	}
}

func TestNoBBrowserStopLifecycle(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if !errors.Is(waitNoBBrowserStop(ctx, t.TempDir()), context.Canceled) {
			t.Fatal("cancellation not returned")
		}
	})
	t.Run("stop", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "stop"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := waitNoBBrowserStop(context.Background(), dir); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "stop"), 0700); err != nil {
			t.Fatal(err)
		}
		if waitNoBBrowserStop(context.Background(), dir) == nil {
			t.Fatal("directory accepted as stop")
		}
	})
	t.Run("symlink", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "target"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(dir, "target"), filepath.Join(dir, "stop")); err != nil {
			t.Fatal(err)
		}
		if waitNoBBrowserStop(context.Background(), dir) == nil {
			t.Fatal("symlink accepted as stop")
		}
	})
}
