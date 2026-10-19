package pullfeed

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connectorsync"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

func TestContentBytesLiveSyncHTTPBoundary(t *testing.T) {
	for _, mode := range []string{"no_permit", "pause_before_request", "finite_http_budget"} {
		t.Run(mode, func(t *testing.T) {
			dsn := os.Getenv("XINSIGHT_POSTGRES_DSN")
			if dsn == "" {
				t.Skip("scoped synthetic PostgreSQL required")
			}
			if _, err := db.RunMigrations(t.Context(), dsn, "../../../migrations"); err != nil {
				t.Fatal(err)
			}
			database, err := sql.Open("pgx", dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			id := "xci_" + strings.ReplaceAll(stateTestUUID(t), "-", "")[:22]
			if _, err = database.ExecContext(t.Context(), `INSERT INTO connector_installations(id,deployment_id,connector_type,requested_event_scope,status,desired_state,observed_state,created_at,updated_at) VALUES($1,'xid_0000000000000000000000','unirouter',ARRAY['model.request.completed'],'pending','active','awaiting_consent',clock_timestamp(),clock_timestamp())`, id); err != nil {
				t.Fatal(err)
			}
			if _, err = database.ExecContext(t.Context(), `INSERT INTO connector_installation_source_bindings(installation_id,source_binding_sha256) VALUES($1,'sha256:'||repeat('a',64))`, id); err != nil {
				t.Fatal(err)
			}
			gate := connectorsync.New(database)
			request := func(action string, revision int64, auto bool) connectorsync.Request {
				return connectorsync.Request{SchemaVersion: connectorsync.LocalSchema, Action: action, InstallationID: id,
					Actor: "xs_0000000000000000", ExpectedRevision: revision, RequestID: stateTestUUID(t), Auto: auto}
			}
			payload := []byte("synthetic bounded body")
			ref, metadata := contentWireFixture(t, payload)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if strings.Contains(r.URL.Path, "capabilities") {
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("Cache-Control", "no-store")
					_, _ = w.Write([]byte(`{}`))
					return
				}
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
				w.Header().Set("Cache-Control", "no-store")
				w.Header().Set("X-Content-Type-Options", "nosniff")
				w.Header().Set("Content-Disposition", `attachment; filename="content.bin"`)
				w.Header().Set(ContentMetadataHeader, metadata)
				_, _ = w.Write(payload)
			}))
			defer server.Close()
			reader, err := NewHTTPSource(HTTPSourceConfig{SyncRequired: true, Endpoint: server.URL + sourcefacts.ContentEndpointPath,
				ServiceKey: "synthetic-body-boundary", AllowPrivateTunnelHTTP: true})
			if err != nil {
				t.Fatal(err)
			}
			limits := sourcefacts.ContentLimits{MaxReadBytes: 1024, MaxBufferBytes: 64}
			fetch := func(ctx context.Context, checks ...func(context.Context) error) error {
				file, e := reader.FetchContentFile(ctx, ref, limits, 1024, t.TempDir(), checks...)
				if e != nil {
					return e
				}
				defer file.Close()
				return file.Publish(ctx, io.Discard, func(ctx context.Context) error {
					for _, check := range checks {
						if e := check(ctx); e != nil {
							return e
						}
					}
					return nil
				})
			}
			if mode == "no_permit" {
				if err = fetch(t.Context()); !errors.Is(err, connectorsync.ErrBinding) || calls.Load() != 0 {
					t.Fatalf("unpermitted body request: err=%v HTTP=%d", err, calls.Load())
				}
				return
			}
			if mode == "pause_before_request" {
				if _, err = gate.Change(t.Context(), request("set", 1, true)); err != nil {
					t.Fatal(err)
				}
			} else if _, err = gate.Change(t.Context(), request("once", 1, false)); err != nil {
				t.Fatal(err)
			}
			ctx, finish, err := gate.Begin(t.Context(), id, "source", time.Minute, "content:body-boundary")
			if err != nil {
				t.Fatal(err)
			}
			defer finish(nil)
			if mode == "pause_before_request" {
				err = fetch(ctx, func(context.Context) error {
					_, e := gate.Change(t.Context(), request("set", 2, false))
					return e
				})
				if !errors.Is(err, connectorsync.ErrChanged) || calls.Load() != 0 {
					t.Fatalf("paused body request: err=%v HTTP=%d", err, calls.Load())
				}
				return
			}
			for i := 0; i < 32; i++ {
				if _, err = reader.FetchContentCapabilities(ctx); err != nil {
					t.Fatal("capability", i, err)
				}
				if err = fetch(ctx); err != nil {
					t.Fatal("body", i, err)
				}
			}
			if err = fetch(ctx); !errors.Is(err, connectorsync.ErrPaused) || calls.Load() != 64 {
				t.Fatalf("exhausted body budget: err=%v HTTP=%d", err, calls.Load())
			}
			var attempts, pages int
			if err = database.QueryRowContext(t.Context(), `SELECT attempts,pages FROM connector_sync_runs WHERE installation_id=$1 AND state='running'`, id).Scan(&attempts, &pages); err != nil || attempts != 64 || pages != 32 {
				t.Fatalf("durable body budget: attempts=%d pages=%d err=%v", attempts, pages, err)
			}
		})
	}
}
