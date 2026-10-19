package api_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/reviewpolicycontrol"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/testsourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workinstallcontrol"
	"github.com/foigent/x-insight/internal/workspace"
	"github.com/jackc/pgx/v5"
)

// manualWorkbenchBrowserAddress opts a local review into a fixed loopback
// address and explicit stop marker, without changing automatic test limits.
func manualWorkbenchBrowserAddress(getenv func(string) string) (string, error) {
	address := getenv("XINSIGHT_A0_BROWSER_MANUAL_ADDR")
	if address == "" {
		return "", nil
	}
	host, port, err := net.SplitHostPort(address)
	number, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || host != "127.0.0.1" || number < 1 || number > 65535 || strconv.Itoa(number) != port ||
		getenv("XINSIGHT_A0_BROWSER_HARNESS") != "1" || getenv("XINSIGHT_A0_RUNTIME_ROLES") != "1" || getenv("GITHUB_ACTIONS") == "true" {
		return "", errors.New("manual browser review requires an explicit local harness, runtime roles and a canonical 127.0.0.1 port; unavailable in CI")
	}
	return address, nil
}

func TestManualWorkbenchBrowserAddress(t *testing.T) {
	for _, tc := range []struct {
		name, address                             string
		missingHarness, missingRoles, ci, invalid bool
	}{
		{name: "automatic default"},
		{name: "manual loopback", address: "127.0.0.1:61712"},
		{name: "public bind rejected", address: "0.0.0.0:61712", invalid: true},
		{name: "hostname rejected", address: "localhost:61712", invalid: true},
		{name: "ephemeral port rejected", address: "127.0.0.1:0", invalid: true},
		{name: "port overflow rejected", address: "127.0.0.1:65536", invalid: true},
		{name: "noncanonical port rejected", address: "127.0.0.1:061712", invalid: true},
		{name: "without opt in", address: "127.0.0.1:61712", missingHarness: true, invalid: true},
		{name: "without runtime roles", address: "127.0.0.1:61712", missingRoles: true, invalid: true},
		{name: "CI cannot wait indefinitely", address: "127.0.0.1:61712", ci: true, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"XINSIGHT_A0_BROWSER_MANUAL_ADDR": tc.address, "XINSIGHT_A0_BROWSER_HARNESS": "1", "XINSIGHT_A0_RUNTIME_ROLES": "1"}
			if tc.missingHarness {
				delete(env, "XINSIGHT_A0_BROWSER_HARNESS")
			}
			if tc.missingRoles {
				delete(env, "XINSIGHT_A0_RUNTIME_ROLES")
			}
			if tc.ci {
				env["GITHUB_ACTIONS"] = "true"
			}
			got, err := manualWorkbenchBrowserAddress(func(key string) string { return env[key] })
			if (err != nil) != tc.invalid || (!tc.invalid && got != tc.address) {
				t.Fatalf("address=%q err=%v", got, err)
			}
		})
	}
}

// TestWorkbenchProductionBrowserHarness exposes the production HTTP handler,
// embedded UI, access service, Evidence store, and Workspace store. Automatic
// qualification has a 30-minute limit. Manual review uses the same stop marker
// and also requires go test -timeout=0 so the Go test runner does not end it.
func TestWorkbenchProductionBrowserHarness(t *testing.T) {
	if os.Getenv("XINSIGHT_A0_BROWSER_HARNESS") != "1" {
		t.Skip("set XINSIGHT_A0_BROWSER_HARNESS=1 for local browser qualification")
	}
	manualAddress, err := manualWorkbenchBrowserAddress(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	output := os.Getenv("XINSIGHT_A0_BROWSER_OUTPUT")
	if !filepath.IsAbs(output) || filepath.Clean(output) != output {
		t.Fatal("absolute output required")
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	server, admin, _, sourceSystemID := startWorkbenchProduction(t)
	var databaseName string
	if err := admin.QueryRow("SELECT current_database()").Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	a1 := os.Getenv("XINSIGHT_A1_BROWSER_HARNESS") == "1"
	var sourceFactsID, sourceImporter string
	var sourceBundle testsourcefacts.Bundle
	var sourceFirst, sourceLast time.Time
	if a1 {
		u, err := url.Parse(os.Getenv("XINSIGHT_POSTGRES_DSN"))
		if err != nil {
			t.Fatal(err)
		}
		u.Path = "/" + databaseName
		u.User = workbenchRuntimeRoleUser(t, "importer")
		sourceImporter = u.String()
		sourceFactsID = testsourcefacts.RegisterSource(t, admin)
		sourceBundle = testsourcefacts.Load(t, testsourcefacts.Chat)
		sourceFirst, sourceLast = testsourcefacts.RetainedPeriod(t, sourceBundle)
		mediaBundle := testsourcefacts.Load(t, testsourcefacts.Media)
		mediaFirst, mediaLast := testsourcefacts.RetainedPeriod(t, mediaBundle)
		if mediaFirst.Before(sourceFirst) {
			sourceFirst = mediaFirst
		}
		if mediaLast.After(sourceLast) {
			sourceLast = mediaLast
		}
		testsourcefacts.Replay(t, admin, sourceImporter, sourceFactsID, sourceBundle, "1")
		testsourcefacts.Replay(t, admin, sourceImporter, sourceFactsID, mediaBundle, "1")
		if _, err = admin.Exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details,allow_coverage,allow_preferences,allow_source_context,allow_source_identity,allow_source_cost) VALUES('xs_8899aabbccddeeff',$1::uuid,true,true,true,true,true,true,true)`, sourceFactsID); err != nil {
			t.Fatal(err)
		}
	}
	ready := map[string]any{"base_url": server.URL, "database_name": databaseName, "username": "workbench.one", "source_system_id": sourceSystemID, "nonzero_facts": 12, "zero_paging_facts": os.Getenv("XINSIGHT_A0_BROWSER_PAGING_FIXTURE") == "1", "evidence_boundary": "synthetic official V1 through isolated PG16 and production handler"}
	if os.Getenv("XINSIGHT_P2_BROWSER_HARNESS") == "1" {
		var agentSource string
		if err := admin.QueryRow(`SELECT id::text FROM source_systems WHERE display_name='P2 synthetic OpenClaw'`).Scan(&agentSource); err != nil {
			t.Fatal(err)
		}
		// Public fixture CA lets the isolated native installer use real TLS validation.
		if err := os.WriteFile(filepath.Join(output, "xi-fixture-ca.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
			t.Fatal(err)
		}
		ready["p2_agent_source_system_id"] = agentSource
		ready["p2_reviewer_password"] = "workbench password"
		ready["p2_activity"] = "synthetic read/edit/write/failed-read plus a separate completed task with four reads, no writes, and two tool-output review bodies"
		ready["p2_completed_event_ref"] = "oc:p2-completed-task"
		ready["p2_completed_run_id"] = "run-p2-completed"
	}
	var boundedSource string
	if os.Getenv("XINSIGHT_A_MANAGED_BROWSER") == "1" {
		if !a1 {
			t.Fatal("managed browser requires source fixture")
		}
		bounded, stop := startManagedBrowserSource(t, admin, sourceImporter, output)
		defer stop()
		boundedSource = bounded
		ready["bounded_source_id"] = bounded
	}
	if a1 {
		ready["source_facts_id"] = sourceFactsID
		ready["retained_source_documents"] = 26
		// Keep captured bytes intact; browser checks reopen a personal filter
		// covering the original events, rather than a clock-dependent last month.
		ready["source_facts_period"] = map[string]string{"start_inclusive": sourceFirst.Format(time.RFC3339Nano), "end_exclusive": sourceLast.Format(time.RFC3339Nano), "display_timezone": "UTC"}
	}
	payload, _ := json.MarshalIndent(ready, "", "  ")
	if err := os.WriteFile(filepath.Join(output, "ready.json"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var expires <-chan time.Time
	if manualAddress == "" {
		timeout := time.NewTimer(30 * time.Minute)
		defer timeout.Stop()
		expires = timeout.C
	} else {
		t.Log("Manual browser review: waiting for the explicit stop marker; TLS and runtime roles unchanged")
	}
	moneyArrived, identityRevoked, sourceAttention := false, false, false
	boundedReady := false
	for {
		select {
		case <-expires:
			t.Fatal("browser timeout")
		case <-ticker.C:
			if boundedSource != "" && !boundedReady {
				var count int
				if err := admin.QueryRow(`SELECT count(*) FROM source_fact_documents WHERE source_system_id=$1::uuid`, boundedSource).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count == 1800 {
					if err := os.WriteFile(filepath.Join(output, "bounded-ingestion-complete"), []byte("1800\n"), 0600); err != nil {
						t.Fatal(err)
					}
					boundedReady = true
				}
			}
			if a1 {
				if _, err := os.Stat(filepath.Join(output, "source-attention")); err == nil && !sourceAttention {
					var streamID string
					if err = admin.QueryRow(`SELECT id::text FROM source_fact_streams WHERE source_system_id=$1::uuid AND profile=$2 ORDER BY id LIMIT 1`, sourceFactsID, sourcefacts.IdentityProfile).Scan(&streamID); err != nil {
						t.Fatal(err)
					}
					store, openErr := db.OpenSourceFactsStore(sourceImporter)
					if openErr != nil {
						t.Fatal(openErr)
					}
					s, loadErr := store.LoadSourceFactsStream(t.Context(), streamID)
					if loadErr == nil {
						loadErr = store.RecordSourceFactsFailure(t.Context(), s, "cursor_expired", time.Now().UTC())
					}
					_ = store.Close()
					if loadErr != nil {
						t.Fatal(loadErr)
					}
					sourceAttention = true
					if err = os.WriteFile(filepath.Join(output, "source-attention-applied"), []byte(streamID), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := os.Stat(filepath.Join(output, "source-money")); err == nil && !moneyArrived {
					testsourcefacts.PersistMoney(t, t.Context(), sourceImporter, sourceFactsID, sourceBundle)
					moneyArrived = true
				}
				if _, err := os.Stat(filepath.Join(output, "revoke-identity")); err == nil && !identityRevoked {
					if _, err = admin.Exec(`UPDATE workbench_access_grants SET allow_source_identity=false WHERE subject_id='xs_8899aabbccddeeff' AND source_system_id=$1::uuid`, sourceFactsID); err != nil {
						t.Fatal(err)
					}
					identityRevoked = true
				}
			}
			if _, err := os.Stat(filepath.Join(output, "stop")); err == nil {
				return
			}
		}
	}
}

func startWorkbenchProduction(t *testing.T) (*httptest.Server, *sql.DB, *db.WorkspaceStore, string) {
	server, admin, workspace, source, _ := startWorkbenchProductionFixture(t, false)
	return server, admin, workspace, source
}
func startWorkbenchProductionFixture(t *testing.T, scopedMode bool, p3 ...*p3BrowserFixture) (*httptest.Server, *sql.DB, *db.WorkspaceStore, string, func()) {
	t.Helper()
	var interop *p3BrowserFixture
	if len(p3) == 1 {
		interop = p3[0]
	}
	p3Check(t, p3FormalMode(os.Getenv) == nil && (os.Getenv("XI_FORMAL_ACCOUNTS") == "" || interop != nil), "formal mode requires admitted interop fixture")
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN required")
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	control, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	name := "xia0_http_" + strings.ReplaceAll(randomTestUUID(t), "-", "")
	ident := pgx.Identifier{name}.Sanitize()
	if _, err = control.Exec("CREATE DATABASE " + ident); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer control.Close()
		if _, err := control.Exec("DROP DATABASE " + ident + " WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	u.Path = "/" + name
	dsn := u.String()
	parent := context.Background()
	if interop != nil && interop.cc14 {
		parent = interop.ctx
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	t.Cleanup(cancel)
	migrationsDir := filepath.Join("..", "..", "migrations")
	if _, err := db.RunMigrations(ctx, dsn, migrationsDir); err != nil {
		t.Fatal(err)
	}
	expected, err := db.LoadMigrationChecksums(migrationsDir)
	if err != nil {
		t.Fatal(err)
	}
	importerDSN, workspaceDSN, readerDSN := dsn, dsn, dsn
	if os.Getenv("XINSIGHT_A0_RUNTIME_ROLES") == "1" || scopedMode {
		importerDSN, workspaceDSN, readerDSN = prepareWorkbenchRuntimeRoles(t, ctx, dsn)
	}
	evidenceStore, err := db.OpenEvidenceStore(ctx, importerDSN)
	if err != nil {
		t.Fatal(err)
	}
	writerStore := evidenceStore
	t.Cleanup(func() { writerStore.Close() })
	workspaceStore, err := db.OpenWorkspaceStore(ctx, workspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { workspaceStore.Close() })

	sourceSystemID := randomTestUUID(t)
	if interop == nil {
		fixture, err := os.ReadFile(filepath.Join("..", "..", "contracts", "completed-execution", "v1", "fixtures", "valid-settled.json"))
		if err != nil {
			t.Fatal(err)
		}
		events := make([]map[string]any, 0, 36)
		now := time.Now().UTC().Truncate(time.Second)
		eventCount := 12
		weekTrend := os.Getenv("XINSIGHT_A0_BROWSER_WEEK_TREND_FIXTURE") == "1"
		if weekTrend && os.Getenv("XINSIGHT_A0_BROWSER_PAGING_FIXTURE") == "1" {
			t.Fatal("week trend and paging browser fixtures are mutually exclusive")
		}
		// Preserve the twelve formal source facts and spread their event times across
		// seven days. The workbench still returns 28 six-hour buckets, including zeros.
		weekHoursAgo := [...]time.Duration{160, 147, 146, 130, 115, 98, 84, 67, 52, 37, 19, 7}
		if os.Getenv("XINSIGHT_A0_BROWSER_PAGING_FIXTURE") == "1" {
			eventCount = 63
		}
		for index := 0; index < eventCount; index++ {
			var value map[string]any
			if err := json.Unmarshal(fixture, &value); err != nil {
				t.Fatal(err)
			}
			value["request_id"] = "req-browser-workbench-" + twoDigits(index+1)
			value["project_id"] = float64(9000 + index)
			value["terminal"].(map[string]any)["available_at"] = now.Add(-time.Duration(index+1) * time.Minute).Format(time.RFC3339)
			if weekTrend {
				value["terminal"].(map[string]any)["available_at"] = now.Add(-weekHoursAgo[index] * time.Hour).Format(time.RFC3339)
			}
			value["usage"].(map[string]any)["model"] = "browser-model-" + twoDigits(index+1)
			amount := twoDigits(index + 17)
			value["money"].(map[string]any)["charged_cost_micros"] = amount
			value["money"].(map[string]any)["calculated_cost_micros"] = twoDigits(index + 23)
			if index >= 12 && !weekTrend {
				value["terminal"].(map[string]any)["available_at"] = now.Add(-30 * time.Second).Format(time.RFC3339)
				value["project_id"] = float64(9000)
				value["usage"].(map[string]any)["model"] = "browser-model-01"
				value["money"].(map[string]any)["charged_cost_micros"] = "0"
				value["money"].(map[string]any)["calculated_cost_micros"] = "0"
			}
			payload, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			mapped, err := unirouter.MapCompletedExecutionV1(payload, unirouter.CompletedExecutionMapOptions{
				SourceInstance: "xi-a0-browser", SourceSystemID: sourceSystemID,
				AdapterManifestID: unirouter.DefaultCompletedExecutionAdapterManifestID,
				ObservedAt:        now, IngestedAt: now,
			})
			if err != nil {
				t.Fatal(err)
			}
			events = append(events, mapped.Events...)
		}
		result, err := evidenceStore.PersistEvents(ctx, events)
		if err != nil || result.Inserted != len(events) {
			t.Fatalf("persist browser fixture=%+v err=%v", result, err)
		}
	} else {
		sourceSystemID = unirouter.DefaultSourceSystemID
		interop.adminDSN, interop.importerDSN = dsn, importerDSN
	}
	admin, err := openBrowserSQL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	var p2Keys reviewbody.KeyRing
	if os.Getenv("XINSIGHT_P2_BROWSER_HARNESS") == "1" {
		if scopedMode || interop != nil {
			t.Fatal("P2 browser activity requires the isolated base fixture")
		}
		p2Keys = seedP2BrowserActivity(t, admin, dsn, workspaceStore)
	}
	if interop != nil {
		interop.admin = admin
	}
	if interop == nil || interop.formal == nil {
		if _, err := admin.ExecContext(ctx, `
INSERT INTO workbench_access_grants(
 subject_id,source_system_id,allow_summary,allow_details,allow_coverage,allow_preferences
) VALUES('xs_8899aabbccddeeff',$1::uuid,true,true,true,true)`, sourceSystemID); err != nil {
			t.Fatal(err)
		}

	}

	provider := apiTestProvider{
		"workbench.one": {password: "workbench password", principal: access.Principal{SubjectID: "xs_8899aabbccddeeff", Role: access.RoleWorkbenchReader}},
		"policy.admin":  {password: "synthetic policy admin password", principal: access.Principal{SubjectID: "xs_1111111111111111", Role: access.RolePlatformAdmin}},
		// A separate existing role verifies the shared shell without combining permissions.
		"investigator.one": {password: "synthetic investigator password", principal: access.Principal{SubjectID: "xs_7777777777777777", Role: access.RoleInvestigator}},
	}
	for i := 0; i < 10; i++ {
		provider[fmt.Sprintf("reader.%d", i)] = struct {
			password  string
			principal access.Principal
		}{password: "synthetic password", principal: access.Principal{SubjectID: fmt.Sprintf("xs_%016x", i), Role: access.RoleWorkbenchReader}}
	}
	var credentials access.CredentialProvider = provider
	if interop != nil && interop.formal != nil {
		credentials = interop.setupFormalAccounts(t, dsn, importerDSN, workspaceDSN, readerDSN)
	} else if scopedMode {
		hash, e := access.DerivePasswordHash([]byte("synthetic responsibility password"))
		if e != nil {
			t.Fatal(e)
		}
		config := access.AccountConfig{SchemaVersion: access.AccountConfigSchemaV3, Accounts: []access.AccountConfigEntry{
			{SubjectID: "xs_8899aabbccddeeff", Username: "scoped.reviewer", Role: access.RoleScopedReviewer, PasswordHash: hash},
			{SubjectID: "xs_6666666666666666", Username: "scoped.reader", Role: access.RoleScopedReader, PasswordHash: hash},
			{SubjectID: "xs_5555555555555555", Username: "scoped.partial", Role: access.RoleScopedReader, PasswordHash: hash},
			{SubjectID: "xs_4444444444444444", Username: "scoped.other", Role: access.RoleScopedReviewer, PasswordHash: hash},
			{SubjectID: "xs_7777777777777777", Username: "legacy.reviewer", Role: access.RoleInvestigator, PasswordHash: hash},
			{SubjectID: "xs_3333333333333333", Username: "purpose.reviewer", Role: access.RolePurposeReviewer, PasswordHash: hash},
			{SubjectID: "xs_2222222222222222", Username: "legacy.money", Role: access.RoleWorkbenchReader, PasswordHash: hash},
		}}
		raw, e := json.Marshal(config)
		if e != nil {
			t.Fatal(e)
		}
		loaded, e := access.ParseAccountConfig(raw)
		if e != nil {
			t.Fatal(e)
		}
		credentials, e = access.NewLocalProvider(loaded)
		if e != nil {
			t.Fatal(e)
		}
		if interop == nil {
			if _, e = admin.ExecContext(ctx, `UPDATE workbench_access_grants SET source_project_ref=(SELECT source_project_ref FROM workbench_economic_facts WHERE request_id='req-browser-workbench-01' LIMIT 1),allow_source_context=true,allow_source_identity=true,allow_source_cost=true WHERE subject_id='xs_8899aabbccddeeff'`); e != nil {
				t.Fatal(e)
			}
			if _, e = admin.ExecContext(ctx, `INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_summary,allow_details,allow_coverage,allow_preferences,allow_source_context,allow_source_identity,allow_source_cost)
 SELECT v.subject,$1::uuid,(SELECT source_project_ref FROM workbench_economic_facts WHERE request_id=v.request LIMIT 1),true,true,true,true,true,v.complete,v.complete FROM (VALUES ('xs_6666666666666666','req-browser-workbench-01',true),('xs_5555555555555555','req-browser-workbench-01',false),('xs_4444444444444444','req-browser-workbench-02',true)) v(subject,request,complete)`, sourceSystemID); e != nil {
				t.Fatal(e)
			}
		} else {
			// Same ordinary reviewer, restricted to the actual UR project. No template facts.
			_, e = admin.ExecContext(ctx, `UPDATE workbench_access_grants SET source_project_ref=$1,allow_source_context=true,allow_source_identity=true,allow_source_cost=true,allow_source_runtime=true WHERE subject_id='xs_8899aabbccddeeff' AND source_system_id=$2::uuid`, "project-"+interop.source.ProjectID, sourceSystemID)
			p3Check(t, e == nil, "ordinary reviewer scope")
			if interop.j1 || interop.cc14 {
				if interop.cc14 {
					cc14RuntimeApproval(t)
				} else {
					j1Approval(t)
				}
				_, e = admin.ExecContext(ctx, `UPDATE workbench_access_grants SET allow_content=true,allowed_content_purposes='["human_review"]' WHERE subject_id='xs_8899aabbccddeeff' AND source_system_id=$1::uuid AND source_project_ref=$2`, sourceSystemID, "project-"+interop.source.ProjectID)
				p3Check(t, e == nil, "J1 exact ordinary content grant")
			}
			_, e = admin.ExecContext(ctx, `INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_summary,allow_details,allow_coverage,allow_preferences,allow_source_context,allow_source_identity,allow_source_cost,allow_source_runtime)
VALUES('xs_5555555555555555',$1::uuid,$2,false,false,true,true,true,false,false,true),('xs_6666666666666666',$1::uuid,$2,true,true,true,true,true,true,true,true)`, sourceSystemID, "project-"+interop.source.ProjectID)
			p3Check(t, e == nil, "nonempty partial target grants")
		}
	}
	service, err := access.NewService(credentials, access.DiscardAuditor{}, access.ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if readerDSN != dsn {
		reader, err := db.OpenEvidenceStore(ctx, readerDSN)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { reader.Close() })
		evidenceStore = reader
		if interop != nil {
			p3Check(t, writerStore.Close() == nil, "close unused P3 writer store")
		}
	}
	var contentSource api.ContentSourceResolver
	if interop != nil && (interop.j1 || interop.cc14) {
		contentSource = interop.resolveContent
	}
	var handler http.Handler
	if os.Getenv("XINSIGHT_P2_BROWSER_HARNESS") == "1" {
		policyOwner, openErr := db.OpenSourceFactsStore(dsn)
		if openErr != nil {
			t.Fatal(openErr)
		}
		t.Cleanup(func() { policyOwner.Close() })
		handler = api.NewHandlerWithDependencies(api.HandlerDependencies{
			Store: evidenceStore, WorkspaceStore: workspaceStore, AgentAccessStore: workspaceStore,
			ExpectedMigrations: expected, AccessService: service, TransportSecurity: api.TransportSecurity{},
			ConnectorConfig: connectorcontrol.InvitationConfig{}, ContentSource: contentSource,
			WorkInstallController: workinstallcontrol.Manager{Store: policyOwner},
			ReviewBodyKeys:        p2Keys, ReviewPolicyController: p2BrowserPolicyControl{manager: p2BrowserPolicyManager(t, policyOwner, admin)},
		})
	} else {
		handler = api.NewProductionHandler(evidenceStore, workspaceStore, expected, service,
			api.TransportSecurity{}, connectorcontrol.InvitationConfig{}, nil, nil, contentSource)
	}
	var server *httptest.Server
	manualAddress, err := manualWorkbenchBrowserAddress(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if manualAddress != "" {
		if scopedMode || interop != nil || t.Name() != "TestWorkbenchProductionBrowserHarness" {
			t.Fatal("manual browser address is limited to the local production browser fixture")
		}
		listener, err := net.Listen("tcp4", manualAddress)
		if err != nil {
			t.Fatal(err)
		}
		server = httptest.NewUnstartedServer(handler)
		server.Listener.Close()
		server.Listener = listener
		server.StartTLS()
	} else if interop != nil && interop.cc14 {
		server = httptest.NewUnstartedServer(handler)
		server.Config.BaseContext = func(net.Listener) context.Context { return interop.ctx }
		server.StartTLS()
	} else {
		server = httptest.NewTLSServer(handler)
	}
	if interop != nil && (interop.j1 || interop.cc14) {
		// Install immediately, including failures before this helper returns.
		interop.stopContentHTTP = server.Close
	}
	t.Cleanup(server.Close)
	return server, admin, workspaceStore, sourceSystemID, func() { server.Close(); workspaceStore.Close(); evidenceStore.Close(); writerStore.Close() }
}

// The browser fixture injects the real owner manager into the production API
// handler. Separate tests cover the deployed Unix socket privilege boundary.
type p2BrowserPolicyControl struct{ manager reviewpolicycontrol.Manager }

func (p p2BrowserPolicyControl) ApplyDefault(ctx context.Context, request reviewpolicycontrol.Request) (reviewpolicycontrol.Response, error) {
	return p.manager.Execute(ctx, request)
}
func (p p2BrowserPolicyControl) ConfirmDefault(ctx context.Context, request reviewpolicycontrol.Request) (reviewpolicycontrol.Response, error) {
	return p.manager.Execute(ctx, request)
}
func (p p2BrowserPolicyControl) BindProject(ctx context.Context, request reviewpolicycontrol.Request) (reviewpolicycontrol.Response, error) {
	return p.manager.Execute(ctx, request)
}
func (p p2BrowserPolicyControl) ApplyException(ctx context.Context, request reviewpolicycontrol.Request) (reviewpolicycontrol.Response, error) {
	return p.manager.Execute(ctx, request)
}
func (p p2BrowserPolicyControl) ConfirmException(ctx context.Context, request reviewpolicycontrol.Request) (reviewpolicycontrol.Response, error) {
	return p.manager.Execute(ctx, request)
}
func (p p2BrowserPolicyControl) URPolicy(ctx context.Context, request reviewpolicycontrol.Request) (reviewpolicycontrol.Response, error) {
	return p.manager.Execute(ctx, request)
}

func twoDigits(value int) string {
	if value < 10 {
		return "0" + string(rune('0'+value))
	}
	return string(rune('0'+value/10)) + string(rune('0'+value%10))
}

func openBrowserSQL(dsn string) (*sql.DB, error) {
	return sql.Open("pgx", dsn)
}

// Reuse only these process-local synthetic credentials when a fixture opens
// another pool. Omitting the password would only work under trust authentication.
var workbenchRuntimeRolePasswords = sync.OnceValues(func() (map[string]string, error) {
	passwords := map[string]string{}
	for _, role := range []string{"reader", "workspace", "importer"} {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		passwords[role] = hex.EncodeToString(b)
	}
	return passwords, nil
})

func workbenchRuntimeRoleUser(t *testing.T, role string) *url.Userinfo {
	t.Helper()
	passwords, err := workbenchRuntimeRolePasswords()
	if err != nil {
		t.Fatal(err)
	}
	password, ok := passwords[role]
	if !ok {
		t.Fatalf("unknown fixture role %q", role)
	}
	return url.UserPassword("xinsight_"+role, password)
}

func prepareWorkbenchRuntimeRoles(t *testing.T, ctx context.Context, dsn string) (string, string, string) {
	t.Helper()
	c := hostctl.DatabaseCredentials{AdminDSN: dsn}
	var err error
	c.Admin, err = pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"reader", "workspace", "importer"} {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		u.User = workbenchRuntimeRoleUser(t, role)
		v := u.String()
		cfg, err := pgx.ParseConfig(v)
		if err != nil {
			t.Fatal(err)
		}
		switch role {
		case "reader":
			c.ReaderDSN = v
			c.Reader = cfg
		case "workspace":
			c.WorkspaceDSN = v
			c.Workspace = cfg
		case "importer":
			c.ImporterDSN = v
			c.Importer = cfg
		}
	}
	manager := hostctl.PostgresManager{}
	for _, fn := range []func(context.Context, hostctl.DatabaseCredentials) error{manager.EnsureRoles, manager.ApplyGrants, manager.VerifyRoleBoundaries} {
		if err := fn(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	a, err := sql.Open("pgx", c.WorkspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var user, timeout string
	var superuser, writeFacts bool
	if err := a.QueryRowContext(ctx, `SELECT current_user,current_setting('statement_timeout'),rolsuper,has_table_privilege(current_user,'workbench_economic_facts','UPDATE') FROM pg_roles WHERE rolname=current_user`).Scan(&user, &timeout, &superuser, &writeFacts); err != nil {
		t.Fatal(err)
	}
	if user != "xinsight_workspace" || timeout != "10s" || superuser || writeFacts {
		t.Fatalf("runtime role invalid: %s %s %t %t", user, timeout, superuser, writeFacts)
	}
	t.Logf("Production stores: workspace=%s statement_timeout=%s superuser=%t facts_update=%t; importer=xinsight_importer reader=xinsight_reader", user, timeout, superuser, writeFacts)
	return c.ImporterDSN, c.WorkspaceDSN, c.ReaderDSN
}

func TestJ1ContentWorkbenchBrowserHarness(t *testing.T) {
	if os.Getenv("XI_J1_INTEROP") != "1" {
		t.Skip("explicit J1 opt-in required")
	}
	j1Approval(t)
	TestScopedWorkbenchBrowserHarness(t)
}

func TestScopedWorkbenchBrowserHarness(t *testing.T) {
	var interop *p3BrowserFixture
	if os.Getenv("XI_J1_INTEROP") != "" {
		p3Check(t, t.Name() == "TestJ1ContentWorkbenchBrowserHarness", "exclusive J1 test selector")
		j1Approval(t)
		interop = newP3BrowserFixture(t)
		defer interop.close(t)
		interop.startProducer(t)
	}
	if os.Getenv("XI_P3_INTEROP") != "" {
		p3Check(t, os.Getenv("XI_P3_INTEROP") == "1" && os.Getenv("XINSIGHT_SCOPED_BROWSER") == "1", "explicit P3 and scoped opt-ins")
		p3HarnessStage("fixture_admission")
		interop = newP3BrowserFixture(t)
		defer interop.close(t)
		p3HarnessStage("producer_start")
		interop.startProducer(t)
	}

	if os.Getenv("XINSIGHT_SCOPED_BROWSER") != "1" {
		t.Skip("explicit scoped browser harness required")
	}
	output := os.Getenv("XINSIGHT_SCOPED_BROWSER_OUTPUT")
	if !filepath.IsAbs(output) {
		t.Fatal("absolute output required")
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	p3HarnessStage("production_fixture")
	server, admin, _, source, _ := startWorkbenchProductionFixture(t, true, interop)
	if interop != nil {
		p3HarnessStage("import_chain")
		interop.importChain(t, admin)
		p3HarnessStage("ordinary_read")
		interop.verifyOrdinaryRead(t, server)
		if !interop.j1 {
			p3HarnessStage("partial_read")
			interop.verifyPartialReads(t, server)
		}
		if interop.j1 {
			interop.verifyContentReview(t, server)
		}
	}
	if os.Getenv("XINSIGHT_PURPOSE_BROWSER") == "1" {
		if _, err := admin.Exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_summary,allow_details,allow_coverage,allow_preferences,allow_source_context,allow_source_identity,allow_source_cost,allow_purpose_read,allow_purpose_review)
SELECT 'xs_3333333333333333',source_system_id,source_project_ref,true,true,true,true,true,true,true,true,true FROM workbench_access_grants WHERE subject_id='xs_8899aabbccddeeff';
UPDATE workbench_access_grants SET allow_purpose_read=true WHERE subject_id IN ('xs_8899aabbccddeeff','xs_6666666666666666')`); err != nil {
			t.Fatal(err)
		}
	}
	var name string
	if err := admin.QueryRow("SELECT current_database()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	username := "scoped.reviewer"
	if interop != nil {
		username = interop.ordinaryUsername("reviewer")
	}
	raw, _ := json.MarshalIndent(map[string]any{"base_url": server.URL, "database_name": name, "source_system_id": source, "username": username, "evidence_boundary": "synthetic formal v3 account, native PostgreSQL roles, production HTTPS handler, embedded final assets"}, "", "  ")
	if err := os.WriteFile(filepath.Join(output, "ready.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	p3HarnessStage("browser_ready")
	var stopContext <-chan struct{}
	if interop != nil {
		stopContext = interop.ctx.Done()
		if interop.j1 {
			p3JSON(t, filepath.Join(output, "j1-chain.json"), map[string]any{"request_id": interop.requestID, "source_system_id": source, "source_instance": interop.source.SourceInstance, "org_id": interop.source.OrgID, "project_id": interop.source.ProjectID, "content_collection": interop.collection.ID, "management_preparation": "production Store; installed ctl not covered", "license_boundary": "component handlers, not installed services"})
		} else {
			p3JSON(t, filepath.Join(output, "p3-chain.json"), map[string]any{"request_id": interop.requestID, "source_system_id": source, "source_instance": interop.source.SourceInstance, "org_id": interop.source.OrgID, "project_id": interop.source.ProjectID, "resource_records": 1, "management_preparation": "production Store; offline-register/enable CLI not covered", "license_boundary": "component handlers, not installed services"})
		}
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	revoked := false
	for {
		select {
		case <-stopContext:
			t.Fatal("P3 browser cancelled before stop receipt")
		case <-t.Context().Done():
			t.Fatal("browser cancelled")
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(output, "stop")); err == nil {
				if interop != nil && interop.j1 {
					p3Check(t, interop.contentPaused && interop.contentRevoked, "J1 stop requires pause and revoke denial evidence")
				}
				return
			}
			if interop != nil {
				interop.healthy(t)
				if interop.j1 {
					interop.contentNegativeStages(t, server)
				} else {
					interop.negativeStages(t, admin, server)
				}
			}
			if _, err := os.Stat(filepath.Join(output, "revoke")); err == nil && !revoked && (interop == nil || !interop.j1) {
				if interop != nil && interop.formal != nil {
					for _, key := range []string{"reviewer-target", "reviewer-reporter"} {
						r := interop.formal.grants[key]
						var shown struct {
							Revision int64 `json:"revision"`
						}
						p3Check(t, json.Unmarshal(interop.formalCLI(t, "workbench-access", "show", "--grant-id", r.ID), &shown) == nil, "current revoke revision")
						r.Action = "revoke"
						r.ExpectedRevision = &shown.Revision
						r.Capabilities = []string{}
						no := false
						r.AllowContent = &no
						r.AllowedContentPurposes = []string{}
						interop.applyFormalGrant(t, strings.Replace(key, "reviewer-", "reviewer-revoke-", 1), r)
					}
				} else {
					if _, err = admin.Exec("UPDATE workbench_access_grants SET active=false WHERE subject_id='xs_8899aabbccddeeff'"); err != nil {
						t.Fatal(err)
					}
				}
				revoked = true
				if err = os.WriteFile(filepath.Join(output, "revoked"), []byte("grant revocation committed\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := os.Stat(filepath.Join(output, "stop")); err == nil {
				if interop != nil && interop.j1 {
					p3Check(t, interop.contentPaused && interop.contentRevoked, "J1 stop requires pause and revoke denial evidence")
				}
				return
			}
		}
	}
}

// P3 is deliberately opt-in and local to this existing harness. It exercises
// production handlers and importer CLIs, not installed-service/license startup.
const p3Root = "/data/x-insight/validation/ur-xi-20260920"

type p3ProducerReady struct {
	URL            string             `json:"url"`
	SourceInstance string             `json:"source_instance"`
	OrgID          string             `json:"org_id"`
	ProjectID      string             `json:"project_id"`
	CredentialDir  string             `json:"credential_dir"`
	Content        *j1ProducerContent `json:"content,omitempty"`
}
type p3Child struct {
	ctx    context.Context
	cmd    *exec.Cmd
	cancel context.CancelFunc
	done   chan struct{}
}
type p3BrowserFixture struct {
	scope                                                                             p3ExecutionScope
	cc14Scenario                                                                      cc14XIScenario
	formal                                                                            *p3FormalAccounts
	stopContentHTTP                                                                   func()
	j1Stage                                                                           string
	cc14                                                                              bool
	j1                                                                                bool
	contentPaused, contentRevoked                                                     bool
	contentConfig                                                                     string
	collection                                                                        sourcefacts.ContentCollection
	contentClient                                                                     workbenchHTTPClient
	contentBodyPath                                                                   string
	contentRef                                                                        sourcefacts.FrozenManifestRef
	admin                                                                             *sql.DB
	oldMoney                                                                          workbench.Reading
	disconnected, resumed, revised                                                    bool
	catchupRequestID, oldMoneyID, oldContribution                                     string
	catchupEconomic                                                                   workbench.DetailRow
	managed                                                                           sourcefacts.ManagedConfig
	blockedCheckpoint                                                                 pullfeed.PostgresWorkerState
	blockedStreams                                                                    []sourcefacts.Stream
	resource                                                                          workbench.ResourceAccessRecord
	resourceAuthorization                                                             sourcefacts.OfflineAuthorization
	economic                                                                          workbench.DetailRow
	ctx                                                                               context.Context
	cancel                                                                            context.CancelFunc
	output, runDir, credentials, importer, producer, adminDSN, importerDSN, requestID string
	source                                                                            p3ProducerReady
	children                                                                          []*p3Child
}

// Only portable P3 emits these closed markers into the owner's private log.
// Arbitrary assertion text, paths, credentials and driver errors stay private.
func p3HarnessStageLine(stage string) string {
	switch stage {
	case "fixture_admission", "database_identity", "fresh_paths", "formal_accounts",
		"producer_start", "production_fixture", "import_chain", "ordinary_read", "partial_read", "browser_ready":
		return "P3_HARNESS_STAGE=" + stage
	default:
		return "P3_HARNESS_STAGE=unclassified"
	}
}

func p3HarnessStage(stage string) {
	if os.Getenv("P3_PORTABLE") == "1" && os.Getenv("XI_P3_INTEROP") == "1" {
		fmt.Println(p3HarnessStageLine(stage))
	}
}

func TestP3HarnessStageIsClosed(t *testing.T) {
	for _, stage := range []string{"fixture_admission", "database_identity", "fresh_paths", "formal_accounts",
		"producer_start", "production_fixture", "import_chain", "ordinary_read", "partial_read", "browser_ready"} {
		if p3HarnessStageLine(stage) != "P3_HARNESS_STAGE="+stage {
			t.Fatal("known stage not classified")
		}
	}
	for _, stage := range []string{"", "private-password", "producer_start\nprivate-password", "/private/path", "postgres://private.invalid"} {
		if p3HarnessStageLine(stage) != "P3_HARNESS_STAGE=unclassified" {
			t.Fatal("arbitrary stage escaped closed classification")
		}
	}
}

func p3Check(t *testing.T, ok bool, stage string) {
	t.Helper()
	if !ok {
		t.Fatal("P3 fixture: " + stage)
	}
}

// Retain an existing check's closed error category before its normal fatal
// assertion. Only portable P3 writes this private receipt; other fixtures keep
// their original behavior. No driver message or connection string is persisted.
func p3FailureCheckStep(stage string) string {
	switch stage {
	case "administrator store":
		return "administrator_store"
	case "consumer store":
		return "consumer_store"
	case "completed checkpoint store":
		return "completed_checkpoint_store"
	case "source credential input":
		return "source_credential"
	case "source transport":
		return "source_transport"
	case "source registration":
		return "source_registration"
	case "source preflight":
		return "source_preflight"
	case "source enable":
		return "source_enable"
	case "managed profiles":
		return "managed_profiles"
	case "resource database binding":
		return "resource_database"
	case "resource bundle binding":
		return "resource_bundle"
	case "resource registration":
		return "resource_registration"
	case "resource enable":
		return "resource_enable"
	default:
		return "unclassified"
	}
}

func p3CheckError(t *testing.T, err error, stage string) {
	t.Helper()
	if err == nil {
		return
	}
	if os.Getenv("P3_PORTABLE") == "1" && os.Getenv("XI_P3_INTEROP") == "1" {
		var diagnostic p3CLIDiagnostic
		classified := err
		for _, known := range []error{sourcefacts.ErrAuthorization, sourcefacts.ErrInvalid, sourcefacts.ErrScopeChanged, workbench.ErrInvalid, workbench.ErrConflict} {
			if errors.Is(err, known) {
				classified = known
				break
			}
		}
		_, _ = fmt.Fprintf(&diagnostic, "xinsight-importer: %v", classified)
		p3JSON(t, filepath.Join(os.Getenv("XINSIGHT_SCOPED_BROWSER_OUTPUT"), "check-failure.json"), map[string]any{"step": p3FailureCheckStep(stage), "diagnostic": diagnostic.receipt(), "context_timeout": errors.Is(err, context.DeadlineExceeded), "context_cancelled": errors.Is(err, context.Canceled)})
	}
	p3Check(t, false, stage)
}
func p3PrivateDirectory(t *testing.T, path string, private bool) {
	t.Helper()
	p3Check(t, filepath.IsAbs(path) && filepath.Clean(path) == path, "absolute clean directory")
	for current := path; ; current = filepath.Dir(current) {
		info, e := os.Lstat(current)
		p3Check(t, e == nil, "directory exists")
		st, ok := info.Sys().(*syscall.Stat_t)
		p3Check(t, ok && st.Uid == 0 && info.IsDir() && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0022 == 0, "directory owner/mode/link")
		if current == path && private {
			p3Check(t, info.Mode().Perm() == 0700, "private directory mode")
		}
		if current == "/" {
			break
		}
	}
}
func p3JSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, e := json.MarshalIndent(value, "", "  ")
	p3Check(t, e == nil, "encode receipt")
	file, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	p3Check(t, e == nil, "exclusive receipt creation")
	_, e = file.Write(raw)
	ce := file.Close()
	p3Check(t, e == nil && ce == nil, "write receipt")
}
func p3Binary(t *testing.T, key string) string {
	t.Helper()
	scope, err := p3ScopeFromEnv(os.Getenv)
	p3Check(t, err == nil, "binary execution scope")
	path := os.Getenv(key)
	p3Check(t, strings.HasPrefix(path, scope.root+"/") && filepath.Clean(path) == path, "binary path boundary")
	p3PrivateDirectory(t, filepath.Dir(path), false)
	info, e := os.Lstat(path)
	p3Check(t, e == nil, "binary present")
	st, ok := info.Sys().(*syscall.Stat_t)
	p3Check(t, ok && st.Uid == 0 && st.Nlink == 1 && info.Mode().IsRegular() && info.Mode().Perm()&0022 == 0 && info.Mode().Perm()&0100 != 0, "binary owner/mode/link")
	file, e := os.Open(path)
	p3Check(t, e == nil, "open binary")
	hash := sha256.New()
	_, e = io.Copy(hash, file)
	ce := file.Close()
	p3Check(t, e == nil && ce == nil && hex.EncodeToString(hash.Sum(nil)) == os.Getenv(key+"_SHA256"), "exact binary digest")
	return path
}
func newP3BrowserFixture(t *testing.T) *p3BrowserFixture {
	t.Helper()
	scope, scopeErr := p3ScopeFromEnv(os.Getenv)
	p3Check(t, scopeErr == nil, "approved execution scope")
	j1 := os.Getenv("XI_J1_INTEROP") == "1"
	if j1 {
		j1Approval(t)
	}
	p3Check(t, os.Geteuid() == 0, "root required")
	if scope.portable {
		p3PrivateDirectory(t, scope.root, true)
		p3PrivateDirectory(t, filepath.Join(scope.root, "sources/ur/cmd/uniroute"), false)
	}
	for _, key := range []string{"XINSIGHT_PURPOSE_BROWSER", "XINSIGHT_A0_BROWSER_PAGING_FIXTURE", "XINSIGHT_A_MANAGED_BROWSER"} {
		p3Check(t, os.Getenv(key) == "", "P3 excludes other fixture modes")
	}
	p3HarnessStage("database_identity")
	cfg, e := pgx.ParseConfig(os.Getenv("XINSIGHT_POSTGRES_DSN"))
	p3Check(t, e == nil, "XI database configuration")
	p3Check(t, cfg.Host == "127.0.0.1" && cfg.Port == scope.xiPort && len(cfg.Fallbacks) == 0 && cfg.Database == os.Getenv("XI_P3_BASE_DATABASE") && cfg.Database != "", "XI must use exact approved synthetic base")
	if scope.portable {
		p3Check(t, p3PortableDatabaseMatches(cfg, scope.xiPort, os.Getenv("XI_P3_BASE_DATABASE")), "portable XI dedicated base without session overrides")
		ur, err := pgx.ParseConfig(os.Getenv("TEST_DATABASE_URL"))
		p3Check(t, err == nil && p3PortableDatabaseMatches(ur, scope.urPort, os.Getenv("UR_P3_BASE_DATABASE")), "portable UR exact dedicated base without session overrides")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	control, e := pgx.ConnectConfig(ctx, cfg)
	p3Check(t, e == nil, "XI identity connection")
	defer control.Close(context.Background())
	var oid, system string
	e = control.QueryRow(ctx, `SELECT (SELECT oid::text FROM pg_database WHERE datname=current_database()),system_identifier::text FROM pg_control_system()`).Scan(&oid, &system)
	p3Check(t, e == nil && oid == os.Getenv("XI_P3_DATABASE_OID") && system == os.Getenv("XI_P3_SYSTEM_ID"), "XI synthetic cluster/base identity")
	p3HarnessStage("fresh_paths")
	credentials := os.Getenv("P3_CREDENTIAL_DIR")
	output := os.Getenv("XINSIGHT_SCOPED_BROWSER_OUTPUT")
	run := filepath.Dir(output)
	p3Check(t, filepath.Dir(credentials) == scope.root+"/credentials/xi-product-test" && filepath.Clean(credentials) == credentials && strings.HasPrefix(filepath.Base(credentials), "p3-"), "credential scope")
	p3Check(t, filepath.Dir(run) == scope.root+"/evidence" && filepath.Base(run) == filepath.Base(credentials) && filepath.Base(output) == "xi" && filepath.Clean(output) == output, "evidence scope")
	p3PrivateDirectory(t, filepath.Dir(credentials), true)
	p3PrivateDirectory(t, filepath.Dir(run), false)
	_, e = os.Lstat(credentials)
	p3Check(t, os.IsNotExist(e), "credential directory must be fresh")
	producer, importer := p3Binary(t, "P3_UR_TEST_BINARY"), p3Binary(t, "P3_XI_IMPORTER")
	p3Check(t, os.Mkdir(run, 0700) == nil, "run evidence directory must be fresh")
	p3Check(t, os.Mkdir(output, 0700) == nil, "XI output directory must be fresh")
	runCtx, stop := signal.NotifyContext(t.Context(), os.Interrupt, syscall.SIGTERM)
	f := &p3BrowserFixture{scope: scope, j1: j1, ctx: runCtx, cancel: stop, output: output, runDir: run, credentials: credentials, producer: producer, importer: importer}
	t.Cleanup(stop)
	p3HarnessStage("formal_accounts")
	f.prepareFormalAccounts(t)
	return f
}
func (f *p3BrowserFixture) child(t *testing.T, binary, dir, logName string, env []string, args ...string) *p3Child {
	t.Helper()
	// Children have independent cancellation so consumers always stop before UR.
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 15 * time.Second
	var log *os.File
	if logName != "" {
		var e error
		log, e = os.OpenFile(filepath.Join(f.output, logName), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		p3Check(t, e == nil, "exclusive CLI receipt log")
		cmd.Stdout = log
	}
	// Never echo a database driver's connection error or a test's raw assertion.
	cmd.Stderr = io.Discard
	if e := cmd.Start(); e != nil {
		cancel()
		if log != nil {
			_ = log.Close()
		}
		p3JSON(t, filepath.Join(f.output, fmt.Sprintf("child-start-failure-%d.json", len(f.children))), p3ProcessReceipt("child_start", cmd, ctx, false))
		t.Fatal("P3 child could not start")
	}
	child := &p3Child{ctx: ctx, cmd: cmd, cancel: cancel, done: make(chan struct{})}
	f.children = append(f.children, child)
	go func() {
		_ = cmd.Wait()
		if log != nil {
			_ = log.Close()
		}
		close(child.done)
	}()
	return child
}
func (f *p3BrowserFixture) healthy(t *testing.T) {
	t.Helper()
	for i, p := range f.children {
		select {
		case <-p.done:
			p3JSON(t, filepath.Join(f.output, fmt.Sprintf("child-failure-%d.json", i)), p3ProcessReceipt("unexpected_child_exit", p.cmd, p.ctx, false))
			t.Fatal("P3 child exited before stop; inspect retained receipts")
		default:
		}
	}
}
func (f *p3BrowserFixture) wait(t *testing.T, stage string, ready func(context.Context) bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 2*time.Minute)
	defer cancel()
	err := p3Poll(ctx, func() bool { _, e := os.Stat(filepath.Join(f.output, "stop")); return e == nil }, func(ctx context.Context) bool {
		f.healthy(t)
		return ready(ctx)
	})
	if err != nil {
		kind := "cancelled"
		if errors.Is(err, context.DeadlineExceeded) {
			kind = "timeout"
		}
		if errors.Is(err, errP3StopRequested) {
			kind = "stop_requested"
		}
		p3JSON(t, filepath.Join(f.output, "wait-failure.json"), map[string]any{"stage": stage, "reason": kind, "request_id": f.requestID, "catchup_request_id": f.catchupRequestID, "producer_states_before_drain": "../ur/settlement-before-drain.json", "producer_states_after_drain": "../ur/settlement-after-drain.json"})
		t.Fatal("P3 stage incomplete: " + stage + "; inspect wait and settlement receipts")
	}
}

var errP3StopRequested = errors.New("P3 stop requested")

// The monitor only lives for this existing wait. It cancels in-flight DB reads
// as well as polling; it does not dispatch work or alter stage ordering.
func p3Poll(parent context.Context, stop func() bool, ready func(context.Context) bool) error {
	ctx, cancel := context.WithCancelCause(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			if stop() {
				cancel(errP3StopRequested)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	defer func() { cancel(nil); <-done }()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		if stop() {
			return errP3StopRequested
		}
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		if ready(ctx) {
			if ctx.Err() != nil {
				return context.Cause(ctx)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-ticker.C:
		}
	}
}

func (f *p3BrowserFixture) close(t *testing.T) {
	if f.j1 {
		// httptest.Close stops admission and waits for in-flight HTTP handlers.
		// This runs on normal return, Fatal/Goexit and signal cancellation, before
		// any consumer/UR child is cancelled and before registered DB cleanups.
		if f.stopContentHTTP != nil {
			f.stopContentHTTP()
		}
		defer func() {
			p3JSON(t, filepath.Join(f.output, "content-owners-stopped.json"), map[string]any{"last_stage": f.j1Stage, "http_started": f.stopContentHTTP != nil, "http_joined": true, "children_joined": true, "successful": !t.Failed()})
		}()
	}
	// Reverse order: source/completed, then producer and its outbox/database.
	for i := len(f.children) - 1; i >= 0; i-- {
		p := f.children[i]
		p.cancel()
		<-p.done
		if f.j1 {
			// Evidence-write failure must never skip joining remaining owners.
			defer p3JSON(t, filepath.Join(f.output, fmt.Sprintf("child-stop-%d.json", i)), p3ProcessReceipt("child_stop", p.cmd, p.ctx, true))
		} else {
			p3JSON(t, filepath.Join(f.output, fmt.Sprintf("child-stop-%d.json", i)), p3ProcessReceipt("child_stop", p.cmd, p.ctx, true))
		}
		if p.cmd.ProcessState == nil || p.cmd.ProcessState.ExitCode() != 0 {
			t.Error("P3 child did not exit cleanly; retained evidence must be inspected")
		}
	}
	f.cancel()
	// Fatal equality failures must still leave the already-joined child receipt.
	defer func() {
		p3JSON(t, filepath.Join(f.output, "children-stopped.json"), map[string]any{"children": len(f.children), "waited": true, "successful": !t.Failed()})
	}()
	f.recordSettlement(t)
	if !t.Failed() {
		rows := []workbench.DetailRow{f.economic}
		if f.catchupRequestID != "" {
			rows = append(rows, f.catchupEconomic)
		}
		for i, row := range rows {
			name := "economic.json"
			if i == 1 {
				name = "economic-catchup.json"
			}
			raw, e := os.ReadFile(filepath.Join(f.runDir, "ur", name))
			// Decode only fields needed for equality; the full authoritative snapshot is retained.
			var binding struct {
				Snapshot struct {
					RequestID string `json:"request_id"`
					Money     struct {
						Charged    string `json:"charged_cost_micros"`
						Calculated string `json:"calculated_cost_micros"`
					} `json:"money"`
				} `json:"snapshot"`
			}
			p3Check(t, e == nil && json.Unmarshal(raw, &binding) == nil && binding.Snapshot.RequestID == row.RequestID && binding.Snapshot.Money.Charged == row.GatewayChargedMicros && binding.Snapshot.Money.Calculated == row.CalculatedReferenceMicros, "UR/XI exact terminal money equality")
		}
	}
	// UR owns only its two fresh secret files. Unknown files/directories are kept.
}
func (f *p3BrowserFixture) startProducer(t *testing.T) {
	if f.j1 {
		f.j1Stage = "producer-start"
	}
	env := []string{"UR_P3_INTEROP=1", "REQUIRE_TEST_DATABASE=1", "REDIS_DB=15", "REDIS_ADDR=127.0.0.1:" + strconv.Itoa(int(f.scope.redisPort)), "P3_CREDENTIAL_DIR=" + f.credentials, "UR_P3_OUTPUT_DIR=" + filepath.Join(f.runDir, "ur")}
	env = append(env, f.scope.producerScopeEnv()...)
	for _, key := range []string{"TEST_DATABASE_URL", "UR_P3_BASE_DATABASE", "UR_P3_DATABASE_OID", "UR_P3_SYSTEM_ID", "REDIS_PASSWORD"} {
		env = append(env, key+"="+os.Getenv(key))
	}
	selector := "-test.run=^TestSourceObservationsChatAndEmbeddingsAuthHistoryAndReplayIntegration$/^chat$/^legacy_shared$"
	if f.j1 {
		env[0] = "UR_J1_INTEROP=1"
		env = append(env, "J1_APPROVED_RUN="+os.Getenv("J1_APPROVED_RUN"), "J1_APPROVED_ACTIONS="+os.Getenv("J1_APPROVED_ACTIONS"))
		selector = "-test.run=^TestJ1ContentAuthenticatedPullIntegration$"
	}
	f.child(t, f.producer, filepath.Join(f.scope.root, "sources/ur/cmd/uniroute"), "", env, selector, "-test.timeout=30m")
	f.wait(t, "producer ready", func(ctx context.Context) bool {
		raw, e := os.ReadFile(filepath.Join(f.runDir, "ur", "ready.json"))
		return e == nil && json.Unmarshal(raw, &f.source) == nil
	})
	var preflight struct {
		Successful bool `json:"successful"`
	}
	raw, e := os.ReadFile(filepath.Join(f.runDir, "ur", "policy-refresh-preflight.json"))
	p3Check(t, e == nil && json.Unmarshal(raw, &preflight) == nil && preflight.Successful, "producer background refresh preflight")
	u, e := url.Parse(f.source.URL)
	p3Check(t, e == nil && u.Scheme == "http" && u.Hostname() == "127.0.0.1" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && f.source.CredentialDir == f.credentials && f.source.SourceInstance == "synthetic-http" && sourcefacts.Positive(f.source.OrgID) && sourcefacts.Positive(f.source.ProjectID), "producer scope/loopback receipt")
}
func (f *p3BrowserFixture) importerEnv() []string {
	return []string{"XINSIGHT_IMPORTER_POSTGRES_DSN=" + f.importerDSN, config.UniRouterFeedEndpointEnv + "=" + f.source.URL + config.UniRouterCompletedExecutionsPath, config.UniRouterFeedKeyFileEnv + "=" + filepath.Join(f.credentials, "completed-feed.key"), config.UniRouterFeedAllowHTTPEnv + "=true", config.UniRouterFeedPollIntervalEnv + "=1s", config.UniRouterFeedMappingModeEnv + "=legacy"}
}
func (f *p3BrowserFixture) cli(t *testing.T, name string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.importer, args...)
	cmd.Env = f.importerEnv()
	var diagnostic p3CLIDiagnostic
	cmd.Stderr = &diagnostic
	raw, e := cmd.Output()
	if e != nil {
		receipt := p3ProcessReceipt(name, cmd, ctx, false)
		receipt["diagnostic"] = diagnostic.receipt()
		var safe struct {
			Status    string `json:"status"`
			Documents int    `json:"documents"`
		}
		if json.Unmarshal(bytes.TrimSpace(raw), &safe) == nil && (safe.Status == "checked" || safe.Status == "persisted" || safe.Status == "completed") {
			receipt["safe_receipt"] = safe
		}
		p3JSON(t, filepath.Join(f.output, name+"-failure.json"), receipt)
		t.Fatal("P3 formal CLI failed: " + name + "; inspect safe failure receipt")
	}
	// CLI outputs are JSON receipts; errors and DSNs are never written here.
	var receipt any
	p3Check(t, json.Unmarshal(bytes.TrimSpace(raw), &receipt) == nil, "CLI receipt encoding: "+name)
	p3JSON(t, filepath.Join(f.output, name+".json"), receipt)
	return raw
}
func (f *p3BrowserFixture) importChain(t *testing.T, admin *sql.DB) {
	ctx := f.ctx
	owner, e := db.OpenSourceFactsStore(f.adminDSN)
	p3CheckError(t, e, "administrator store")
	defer owner.Close()
	store, e := db.OpenSourceFactsStore(f.importerDSN)
	p3CheckError(t, e, "consumer store")
	defer store.Close()
	installation, e := connectorcontrol.NewID("xci_", nil)
	p3CheckError(t, e, "synthetic sync installation")
	feedKey, e := config.LoadSourceFactsServiceKey(filepath.Join(f.credentials, "completed-feed.key"))
	p3CheckError(t, e, "synthetic sync receiver")
	seedBrowserAutomaticSync(t, admin, installation, f.source.SourceInstance, f.source.OrgID, f.source.ProjectID, feedKey)
	configPath := f.bootstrapSourceProfiles(t, owner, store)
	managed := f.managed
	f.child(t, f.importer, "", "completed-feed.jsonl", f.importerEnv(), "unirouter", "pull-feed")
	stateStore, e := pullfeed.NewPostgresStateStore(admin, unirouter.DefaultSourceSystemID, unirouter.DefaultCompletedExecutionAdapterManifestID)
	p3CheckError(t, e, "completed checkpoint store")
	f.wait(t, "completed bootstrap", func(ctx context.Context) bool {
		s, e := stateStore.Load(ctx)
		return e == nil && s.Cursor != "" && s.SourceInstance == f.source.SourceInstance && s.PagesCommitted > 0 && s.ItemsCommitted == 0
	})
	if f.j1 {
		f.bootstrapContent(t, owner, store)
	}
	p3JSON(t, filepath.Join(f.runDir, "bootstrap.json"), f.source)
	f.requestID = f.dispatch(t, "request", "")
	// Exercise the same UR predicate as outage catchup before any browser work.
	f.wait(t, "producer first terminal preflight", func(ctx context.Context) bool {
		var v struct {
			RequestID string `json:"request_id"`
			Settled   int    `json:"settled"`
			Economic  int    `json:"economic_records"`
			Profiles  int    `json:"source_profiles"`
			Calls     int    `json:"provider_calls"`
		}
		raw, e := os.ReadFile(filepath.Join(f.runDir, "ur", "request-ready.json"))
		return e == nil && json.Unmarshal(raw, &v) == nil && v.RequestID == f.requestID && v.Settled == 1 && v.Economic == 1 && v.Profiles == 3 && v.Calls == 1
	})
	f.child(t, f.importer, "", "source-worker.jsonl", f.importerEnv(), "unirouter", "run-source-facts", "--config", configPath)
	f.wait(t, "same request economic and source facts", func(ctx context.Context) bool {
		s, e := stateStore.Load(ctx)
		if e != nil || s.ItemsCommitted != 1 || s.SourceInstance != f.source.SourceInstance {
			return false
		}
		var economic, documents int
		e = admin.QueryRowContext(ctx, `SELECT count(*) FROM workbench_economic_facts WHERE source_system_id=$1::uuid AND request_id=$2 AND source_project_ref=$3`, unirouter.DefaultSourceSystemID, f.requestID, "project-"+f.source.ProjectID).Scan(&economic)
		if e != nil || economic != 1 {
			return false
		}
		e = admin.QueryRowContext(ctx, `SELECT count(DISTINCT profile) FROM source_fact_documents WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND request_id=$5`, unirouter.DefaultSourceSystemID, f.source.SourceInstance, f.source.OrgID, f.source.ProjectID, f.requestID).Scan(&documents)
		if e != nil || documents != 3 {
			return false
		}
		var runtime, attempts int
		e = admin.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE observation_kind='request_terminal'),count(*) FILTER(WHERE observation_kind='financial_result') FROM source_observation_documents WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND request_id=$5`, unirouter.DefaultSourceSystemID, f.source.SourceInstance, f.source.OrgID, f.source.ProjectID, f.requestID).Scan(&runtime, &attempts)
		return e == nil && runtime == 1 && attempts == 1
	})
	for _, p := range managed.Profiles {
		s, e := store.LoadSourceFactsStream(ctx, p.StreamID)
		p3Check(t, e == nil && s.Status == "active" && !s.RequiresAttention, "source worker healthy")
	}
	p3Check(t, store.Close() == nil, "close P3 source initialization store")
	f.checkImporterHeadroom(t, admin)
	if f.j1 {
		f.consumeContent(t)
	} else {
		f.resources(t, admin, owner)
	}
}
func (f *p3BrowserFixture) resources(t *testing.T, admin *sql.DB, owner *db.SourceFactsStore) {
	var product buildinfo.Info
	raw := f.cli(t, "importer-version", "version", "--json")
	p3Check(t, json.Unmarshal(raw, &product) == nil && product.ProductVersion != "" && product.Commit != "", "importer identity")
	var name, oid, system string
	e := admin.QueryRowContext(f.ctx, `SELECT current_database(),oid::text,public.synthetic_resource_cluster_id() FROM pg_database WHERE datname=current_database()`).Scan(&name, &oid, &system)
	p3CheckError(t, e, "resource database binding")
	machine, e := os.ReadFile("/etc/machine-id")
	p3Check(t, e == nil && len(bytes.TrimSpace(machine)) > 0, "resource host binding")
	reporter := workbench.ResourceScope{SourceSystemID: randomTestUUID(t), SourceInstance: "p3-synthetic-reporter", OrgID: f.source.OrgID, ProjectID: f.source.ProjectID}
	target := workbench.ResourceScope{SourceSystemID: unirouter.DefaultSourceSystemID, SourceInstance: f.source.SourceInstance, OrgID: f.source.OrgID, ProjectID: f.source.ProjectID}
	record := workbench.ResourceAccessRecord{Version: workbench.ResourceAccessVersion, Synthetic: true, RecordRef: "p3-resource-1", Revision: 1, Reporter: reporter, Target: target, RequestID: f.requestID, ApplicationRef: "p3-synthetic-app", ActorRef: "p3-synthetic-actor", OccurredAt: time.Now().UTC().Truncate(time.Microsecond), ReportKind: "source_audit", Resource: workbench.ResourceObject{Ref: "p3-document", Version: "v1", Name: "Synthetic only", Location: "enterprise_service", Ownership: "unknown", Classification: "unknown", DeviceManagement: "unknown"}, Action: "read", AccessOutcome: "unknown", AccessDecision: "unknown"}
	bundlePath := filepath.Join(f.output, "resource-bundle.json")
	p3JSON(t, bundlePath, map[string]any{"schema_version": sourcefacts.ResourceOfflineBundleVersion, "records": []workbench.ResourceAccessRecord{record}})
	bundle, e := os.ReadFile(bundlePath)
	p3CheckError(t, e, "resource bundle binding")
	now := time.Now().UTC()
	run := filepath.Base(f.runDir)
	a := sourcefacts.OfflineAuthorization{SchemaVersion: sourcefacts.ResourceOfflineAuthorizationVersion, AuthorizationID: run, Environment: "test", TargetRef: run, MachineIDSHA256: sourcefacts.Hash(bytes.TrimSpace(machine)), ProductVersion: product.ProductVersion, ProductCommit: product.Commit, SourceSchemaSHA256: sourcefacts.ResourceSchemaSHA256(), SourceSystemID: reporter.SourceSystemID, SourceInstance: reporter.SourceInstance, OrgID: reporter.OrgID, ProjectID: reporter.ProjectID, Profile: sourcefacts.ResourceOfflineProfile, NotBefore: now.Add(-time.Minute).Format(sourcefacts.TimeFormat), ExpiresAt: now.Add(time.Hour).Format(sourcefacts.TimeFormat), BundlePath: bundlePath, BundleSHA256: sourcefacts.Hash(bundle), BundleID: run + "-resource", OperationID: run + "-resource", MaxRecords: 1, PayloadCaptureMode: "metadata_only", TargetDatabaseSchema: buildinfo.DatabaseSchemaMax, TargetDatabaseName: name, TargetDatabaseOID: oid, TargetDatabaseSystemID: system, TargetSourceSystemID: target.SourceSystemID, TargetSourceInstance: target.SourceInstance, TargetOrgID: target.OrgID, TargetProjectID: target.ProjectID}
	authPath := filepath.Join(f.output, "resource-authorization.json")
	f.resource, f.resourceAuthorization = record, a
	p3JSON(t, authPath, a)
	// Same production Store validation/authority/audit as management CLI. The
	// installed-management CLI itself requires four disk DSNs and is not covered.
	grant, e := owner.RegisterSourceOfflineAuthorization(f.ctx, a)
	p3CheckError(t, e, "resource registration")
	_, e = owner.ControlSourceOfflineAuthorization(f.ctx, a.AuthorizationID, "active", grant.Revision, a.Digest(), a)
	p3CheckError(t, e, "resource enable")
	// Reporter and target are separate existing permission domains. The same
	// reviewer gets only this fresh reporter/project's resource-read grant.
	if f.formal != nil {
		f.formalGrant(t, "reviewer-reporter", "reviewer", reporter.SourceSystemID, "project-"+reporter.ProjectID, true, false)
	} else {
		_, e = admin.ExecContext(f.ctx, `INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_resource) VALUES('xs_8899aabbccddeeff',$1::uuid,$2,true),('xs_5555555555555555',$1::uuid,$2,true)`, reporter.SourceSystemID, "project-"+reporter.ProjectID)
		p3Check(t, e == nil, "scoped resource reporter grant")
	}

	checked := f.cli(t, "resource-check", "import-synthetic-resources", "--authorization-file", authPath, "--target-ref", run, "--check")
	var check struct {
		Status    string `json:"status"`
		Documents int    `json:"documents"`
	}
	p3Check(t, json.Unmarshal(checked, &check) == nil && check.Status == "checked" && check.Documents == 1, "resource check receipt")
	imported := f.cli(t, "resource-import", "import-synthetic-resources", "--authorization-file", authPath, "--target-ref", run)
	var receipt struct {
		Status string `json:"status"`
		Counts struct {
			Inserted int `json:"inserted"`
		} `json:"counts"`
	}
	p3Check(t, json.Unmarshal(imported, &receipt) == nil && receipt.Status == "completed" && receipt.Counts.Inserted == 1, "resource import receipt")
}

func (f *p3BrowserFixture) verifyOrdinaryRead(t *testing.T, server *httptest.Server) {
	c := f.ordinaryLogin(t, server, "reviewer")
	c.client.Timeout = 10 * time.Second
	money := httpReadingRequest(unirouter.DefaultSourceSystemID)
	money.Filters.SourceProjects = []string{"project-" + f.source.ProjectID}
	money.MoneyBasis = workbench.MoneyCalculatedReference
	var reading workbench.Reading
	p3Check(t, json.Unmarshal(c.request(t, "POST", "/api/v1/workbench/readings", money, 201), &reading) == nil, "ordinary economic reading")
	var contributions workbench.Contributions
	p3Check(t, json.Unmarshal(c.request(t, "GET", "/api/v1/workbench/readings/"+reading.ID+"/contributions?dimension=source_project", nil, 200), &contributions) == nil && len(contributions.Rows) == 1 && contributions.Rows[0].DrilldownAllowed, "ordinary project drilldown")
	var details workbench.Details
	raw := c.request(t, "GET", "/api/v1/workbench/readings/"+reading.ID+"/details?contribution_ref="+url.QueryEscape(contributions.Rows[0].Ref), nil, 200)
	p3Check(t, json.Unmarshal(raw, &details) == nil && len(details.Rows) == 1, "one ordinary economic result")
	row := details.Rows[0]
	f.economic = row
	f.oldMoneyID, f.oldContribution = reading.ID, contributions.Rows[0].Ref
	f.oldMoney = reading
	p3JSON(t, filepath.Join(f.output, "ordinary-money-reading.json"), map[string]any{"request": money, "reading": reading})
	p3Check(t, row.RequestID == f.requestID && row.SourceSystemID == unirouter.DefaultSourceSystemID && row.SourceProject == "project-"+f.source.ProjectID && row.TerminalStatus == "settled" && row.GatewayChargedMicros == "0" && row.CalculatedReferenceMicros != "" && row.CalculatedReferenceMicros != "0", "exact economic terminal and calculated cost")
	p3JSON(t, filepath.Join(f.output, "ordinary-economic.json"), details)
	var runtime workbench.RuntimeReading
	input := workbench.RuntimeReadingRequest{ContractVersion: workbench.RuntimeContractVersion, MetricVersion: workbench.RuntimeMetricVersion, Period: money.Period, Filters: money.Filters}
	p3Check(t, json.Unmarshal(c.request(t, "POST", "/api/v1/runtime/readings", input, 201), &runtime) == nil, "ordinary runtime reading")
	base := "/api/v1/runtime/readings/" + runtime.ID
	var requests workbench.RuntimeRequests
	p3Check(t, json.Unmarshal(c.request(t, "GET", base+"/requests?entry_time=all", nil, 200), &requests) == nil && len(requests.Items) == 1, "one ordinary runtime request")
	request := requests.Items[0]
	p3Check(t, request.RequestID == f.requestID && request.SourceSystemID == unirouter.DefaultSourceSystemID && request.SourceInstance == f.source.SourceInstance && request.OrgID == f.source.OrgID && request.ProjectID == f.source.ProjectID, "ordinary exact source/request scope")
	if !f.j1 {
		var resources workbench.RuntimeResources
		p3Check(t, json.Unmarshal(c.request(t, "GET", base+"/requests/"+request.Ref+"/resources", nil, 200), &resources) == nil && len(resources.Items) == 1 && resources.Items[0].Record.RequestID == f.requestID, "ordinary nonempty same-request resources")
		p3JSON(t, filepath.Join(f.output, "ordinary-resources.json"), resources)
	}
	p3JSON(t, filepath.Join(f.output, "ordinary-runtime.json"), requests)
}

// Markers remain local, single-run and one-shot. They do not authorize a new
// environment or resource grant; the operator must obtain that approval first.
func (f *p3BrowserFixture) negativeStages(t *testing.T, admin *sql.DB, server *httptest.Server) {
	exists := func(name string) bool { _, e := os.Stat(filepath.Join(f.output, name)); return e == nil }
	if exists("feed-disconnect") && !f.disconnected {
		f.disconnected = true
		p3JSON(t, filepath.Join(f.runDir, "ur", "feed-disconnect"), map[string]any{"run": filepath.Base(f.runDir)})
		state, e := pullfeed.NewPostgresStateStore(admin, unirouter.DefaultSourceSystemID, unirouter.DefaultCompletedExecutionAdapterManifestID)
		p3Check(t, e == nil, "outage checkpoint store")
		store, e := db.OpenSourceFactsStore(f.adminDSN)
		p3Check(t, e == nil, "outage observer")
		defer store.Close()
		f.wait(t, "real completed/source consumer failures", func(ctx context.Context) bool {
			s, e := state.Load(ctx)
			if e != nil || s.LastErrorCode != "source_status_503" || s.ItemsCommitted != 1 {
				return false
			}
			f.blockedCheckpoint = s
			f.blockedStreams = nil
			for _, p := range f.managed.Profiles {
				stream, e := store.LoadSourceFactsStream(ctx, p.StreamID)
				if e != nil || stream.LastError == "" {
					return false
				}
				f.blockedStreams = append(f.blockedStreams, stream)
			}
			return true
		})
		streams := []map[string]any{}
		for _, v := range f.blockedStreams {
			streams = append(streams, map[string]any{"stream_id": v.ID, "profile": v.Profile, "scope_generation": v.ScopeGeneration, "last_error": v.LastError, "cursor_sha256": sourcefacts.Hash([]byte(v.Cursor))})
		}
		p3JSON(t, filepath.Join(f.output, "feed-failure-observed.json"), map[string]any{"completed_error": f.blockedCheckpoint.LastErrorCode, "completed_cursor_sha256": sourcefacts.Hash([]byte(f.blockedCheckpoint.Cursor)), "source_streams": streams, "source_instance": f.source.SourceInstance})
		f.catchupRequestID = f.dispatch(t, "request-catchup", "-catchup")
		f.wait(t, "UR settled backlog during outage", func(ctx context.Context) bool {
			var r struct {
				RequestID string `json:"request_id"`
				Calls     int    `json:"provider_calls"`
				Economic  int    `json:"economic_records"`
			}
			raw, e := os.ReadFile(filepath.Join(f.runDir, "ur", "backlog-ready.json"))
			return e == nil && json.Unmarshal(raw, &r) == nil && r.RequestID == f.catchupRequestID && r.Calls == 2 && r.Economic == 1
		})
		s, e := state.Load(f.ctx)
		p3Check(t, e == nil && s.Cursor == f.blockedCheckpoint.Cursor && s.ItemsCommitted == 1, "blocked checkpoint unchanged")
		for _, before := range f.blockedStreams {
			after, e := store.LoadSourceFactsStream(f.ctx, before.ID)
			p3Check(t, e == nil && after.Cursor == before.Cursor && after.LastError != "", "blocked source cursor unchanged")
		}
		var imported int
		e = admin.QueryRowContext(f.ctx, `SELECT count(*) FROM workbench_economic_facts WHERE request_id=$1`, f.catchupRequestID).Scan(&imported)
		p3Check(t, e == nil && imported == 0, "backlog not imported during outage")
		p3JSON(t, filepath.Join(f.output, "feed-disconnected.json"), map[string]any{"request_id": f.catchupRequestID, "ur_settled": true, "xi_imported": 0, "checkpoint_unchanged": true})
	}
	if exists("feed-resume") && !f.resumed {
		p3Check(t, f.disconnected && f.catchupRequestID != "", "resume follows recorded outage")
		f.resumed = true
		p3JSON(t, filepath.Join(f.runDir, "ur", "feed-resume"), map[string]any{"run": filepath.Base(f.runDir)})
		state, e := pullfeed.NewPostgresStateStore(admin, unirouter.DefaultSourceSystemID, unirouter.DefaultCompletedExecutionAdapterManifestID)
		p3Check(t, e == nil, "recovery checkpoint store")
		store, e := db.OpenSourceFactsStore(f.adminDSN)
		p3Check(t, e == nil, "recovery observer")
		defer store.Close()
		f.wait(t, "original checkpoints catch up", func(ctx context.Context) bool {
			s, e := state.Load(ctx)
			if e != nil || s.ItemsCommitted != 2 || s.LastErrorCode != "" || s.CheckpointEpoch != f.blockedCheckpoint.CheckpointEpoch || s.ID != f.blockedCheckpoint.ID || s.ActivationCursor != f.blockedCheckpoint.ActivationCursor || s.SourceInstance != f.source.SourceInstance {
				return false
			}
			for _, before := range f.blockedStreams {
				after, e := store.LoadSourceFactsStream(ctx, before.ID)
				if e != nil || after.LastError != "" || after.RequiresAttention || after.ScopeGeneration != before.ScopeGeneration || after.SourceInstance != before.SourceInstance {
					return false
				}
			}
			var facts, runtime, financial int
			e = admin.QueryRowContext(ctx, `SELECT (SELECT count(DISTINCT profile) FROM source_fact_documents WHERE request_id=$1),(SELECT count(*) FROM source_observation_documents WHERE request_id=$1 AND observation_kind='request_terminal'),(SELECT count(*) FROM source_observation_documents WHERE request_id=$1 AND observation_kind='financial_result')`, f.catchupRequestID).Scan(&facts, &runtime, &financial)
			return e == nil && facts == 3 && runtime == 1 && financial == 1
		})
		recovered := []map[string]any{}
		for _, v := range f.blockedStreams {
			now, e := store.LoadSourceFactsStream(f.ctx, v.ID)
			p3Check(t, e == nil, "recovered source receipt")
			recovered = append(recovered, map[string]any{"stream_id": now.ID, "profile": now.Profile, "scope_generation": now.ScopeGeneration, "last_error": now.LastError, "cursor_sha256": sourcefacts.Hash([]byte(now.Cursor))})
		}
		// Record checkpoint recovery before the separate frozen-money assertion.
		p3JSON(t, filepath.Join(f.output, "feed-resumed.json"), map[string]any{"requests": 2, "same_source": f.source.SourceInstance, "same_checkpoint_epoch": f.blockedCheckpoint.CheckpointEpoch, "no_checkpoint_reset": true, "source_streams": recovered, "verification_scope": "checkpoint_recovery", "money_receipt": "catchup-money.json"})
		f.verifyMoneyAfterCatchup(t, server)
	}
	if exists("resource-revise") && !f.revised {
		p3Check(t, !f.disconnected || f.resumed, "complete catchup before resource revision")
		// An exact-run opt-in is an execution guard, not a substitute for approval.
		p3Check(t, os.Getenv("P3_RESOURCE_REV2_APPROVED_RUN") == filepath.Base(f.runDir), "new resource authorization requires exact-run approval")
		f.revised = true
		f.reviseResource(t, admin, server)
	}
}

func (f *p3BrowserFixture) dispatch(t *testing.T, name, suffix string) string {
	// One transport attempt per named synthetic request, with no unknown-response retry.
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, e := http.NewRequestWithContext(f.ctx, http.MethodPost, f.source.URL+"/v1/chat/completions", strings.NewReader(`{"model":"company-chat","messages":[{"role":"user","content":"P3 synthetic request 20260920"}],"stream":false}`))
	p3Check(t, e == nil, "request construction")
	req.Header.Set("Authorization", "Bearer ur-synthetic-source-"+strings.Repeat("a", 32))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-UniRouter-Client-Request-ID", filepath.Base(f.runDir)+suffix)
	p3JSON(t, filepath.Join(f.output, name+"-attempt.json"), map[string]any{"attempts": 1, "client_request_id": filepath.Base(f.runDir) + suffix, "retry_permitted": false})
	response, e := client.Do(req)
	p3Check(t, e == nil, "request response unknown; inspect producer receipt, never retry")
	raw, e := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
	ce := response.Body.Close()
	id := response.Header.Get("X-Request-ID")
	if strings.HasPrefix(id, "op_") {
		if suffix == "" {
			f.requestID = id
		} else {
			f.catchupRequestID = id
		}
	}
	responseReceipt := map[string]any{"status": response.StatusCode, "request_id": id, "body_sha256": sourcefacts.Hash(raw)}
	if response.StatusCode >= 400 {
		responseReceipt["error_code"] = p3PublicErrorCode(raw)
	}
	p3JSON(t, filepath.Join(f.output, name+"-response.json"), responseReceipt)
	p3Check(t, e == nil && ce == nil && response.StatusCode == 200 && strings.HasPrefix(id, "op_"), "canonical successful request")
	var receipt struct {
		RequestID string `json:"request_id"`
		Calls     int    `json:"request_provider_calls"`
	}
	f.wait(t, "one provider call for exact request", func(ctx context.Context) bool {
		raw, e := os.ReadFile(filepath.Join(f.runDir, "ur", name+".json"))
		return e == nil && json.Unmarshal(raw, &receipt) == nil && receipt.RequestID == id && receipt.Calls == 1
	})
	return id
}

func (f *p3BrowserFixture) runtimeResourceRead(t *testing.T, c workbenchHTTPClient) (string, workbench.RuntimeResources) {
	money := httpReadingRequest(unirouter.DefaultSourceSystemID)
	money.Filters.SourceProjects = []string{"project-" + f.source.ProjectID}
	input := workbench.RuntimeReadingRequest{ContractVersion: workbench.RuntimeContractVersion, MetricVersion: workbench.RuntimeMetricVersion, Period: money.Period, Filters: money.Filters}
	var r workbench.RuntimeReading
	p3Check(t, json.Unmarshal(c.request(t, "POST", "/api/v1/runtime/readings", input, 201), &r) == nil, "runtime reading")
	base := "/api/v1/runtime/readings/" + r.ID
	var requests workbench.RuntimeRequests
	p3Check(t, json.Unmarshal(c.request(t, "GET", base+"/requests?entry_time=all", nil, 200), &requests) == nil, "runtime request list")
	for _, q := range requests.Items {
		if q.RequestID == f.requestID {
			path := base + "/requests/" + q.Ref + "/resources"
			var resources workbench.RuntimeResources
			p3Check(t, json.Unmarshal(c.request(t, "GET", path, nil, 200), &resources) == nil, "resources response")
			return path, resources
		}
	}
	t.Fatal("P3 primary runtime request absent")
	return "", workbench.RuntimeResources{}
}

func (f *p3BrowserFixture) verifyPartialReads(t *testing.T, server *httptest.Server) {
	if f.formal != nil {
		reader := f.ordinaryLogin(t, server, "reader")
		_, before := f.runtimeResourceRead(t, reader)
		p3Check(t, len(before.Items) == 0, "formal reader lacks reporter authority")
		f.formalGrant(t, "reader-reporter", "reader", f.resource.Reporter.SourceSystemID, "project-"+f.resource.Reporter.ProjectID, true, false)
		reader = f.ordinaryLogin(t, server, "reader")
		_, after := f.runtimeResourceRead(t, reader)
		p3Check(t, len(after.Items) == 1, "formal reader gets exact reporter/target intersection")
		status, _, _ := f.contentHTTP(t, reader, "POST", "/api/v1/security/reviews", map[string]any{})
		p3Check(t, status == 403, "formal reader cannot write review")
		p3JSON(t, filepath.Join(f.output, "formal-reader-resource-fence.json"), map[string]any{"before": before, "after": after, "write_status": 403})
		return
	}
	for _, name := range []string{"scoped.partial", "scoped.reader"} {
		c := loginWorkbenchHTTP(t, server, name, "synthetic responsibility password")
		c.client.Timeout = 10 * time.Second
		money := httpReadingRequest(unirouter.DefaultSourceSystemID)
		money.Filters.SourceProjects = []string{"project-" + f.source.ProjectID}
		if name == "scoped.partial" {
			c.request(t, "POST", "/api/v1/workbench/readings", money, 403)
		}
		_, resources := f.runtimeResourceRead(t, c)
		want := 0
		if name == "scoped.partial" {
			want = 1
		}
		p3Check(t, len(resources.Items) == want, "partial target/reporter intersection")
		p3JSON(t, filepath.Join(f.output, name+"-resources.json"), resources)
	}
}

func (f *p3BrowserFixture) verifyMoneyAfterCatchup(t *testing.T, server *httptest.Server) {
	c := f.ordinaryLogin(t, server, "reviewer")
	c.client.Timeout = 10 * time.Second
	// The original fixed period/cutoff remain untouched. New period includes both dispatches.
	rawReading := c.request(t, "GET", "/api/v1/workbench/readings/"+f.oldMoneyID, nil, 200)
	oldReading, comparison, unchanged := p3FrozenReadingEvidence(f.oldMoney, rawReading)
	// Keep field-level evidence even when the assertion terminates the fixture.
	p3JSON(t, filepath.Join(f.output, "catchup-frozen-reading.json"), comparison)
	p3Check(t, unchanged, "original period/cutoff/members unchanged; inspect catchup-frozen-reading.json")
	old := c.request(t, "GET", "/api/v1/workbench/readings/"+f.oldMoneyID+"/details?contribution_ref="+url.QueryEscape(f.oldContribution), nil, 200)
	var frozen workbench.Details
	p3Check(t, json.Unmarshal(old, &frozen) == nil && len(frozen.Rows) == 1 && frozen.Rows[0].RequestID == f.requestID && frozen.Rows[0].CalculatedReferenceMicros == "700" && frozen.Rows[0].GatewayChargedMicros == "0", "old fixed money reading unchanged")
	money := httpReadingRequest(unirouter.DefaultSourceSystemID)
	money.Filters.SourceProjects = []string{"project-" + f.source.ProjectID}
	money.MoneyBasis = workbench.MoneyCalculatedReference
	var r workbench.Reading
	p3Check(t, json.Unmarshal(c.request(t, "POST", "/api/v1/workbench/readings", money, 201), &r) == nil, "new two-request period")
	p3Check(t, r.Period.EndExclusive.After(oldReading.Period.EndExclusive), "new period explicitly includes later dispatch")
	var contributions workbench.Contributions
	p3Check(t, json.Unmarshal(c.request(t, "GET", "/api/v1/workbench/readings/"+r.ID+"/contributions?dimension=source_project", nil, 200), &contributions) == nil && len(contributions.Rows) == 1, "caught-up contribution")
	var details workbench.Details
	p3Check(t, json.Unmarshal(c.request(t, "GET", "/api/v1/workbench/readings/"+r.ID+"/details?contribution_ref="+url.QueryEscape(contributions.Rows[0].Ref), nil, 200), &details) == nil && len(details.Rows) == 2, "exactly two economic rows")
	seen := map[string]bool{}
	for _, row := range details.Rows {
		p3Check(t, !seen[row.RequestID] && (row.RequestID == f.requestID || row.RequestID == f.catchupRequestID) && row.CalculatedReferenceMicros == "700" && row.GatewayChargedMicros == "0" && row.TerminalStatus == "settled", "each request charged/calculated once")
		seen[row.RequestID] = true
		if row.RequestID == f.catchupRequestID {
			f.catchupEconomic = row
		}
	}
	p3JSON(t, filepath.Join(f.output, "catchup-money.json"), map[string]any{"old_reading": oldReading, "old_details": frozen, "new_period": money.Period, "new_reading": r, "new_details": details})
}

func (f *p3BrowserFixture) reviseResource(t *testing.T, admin *sql.DB, server *httptest.Server) {
	c := f.ordinaryLogin(t, server, "reviewer")
	c.client.Timeout = 10 * time.Second
	var caseID, findingID string
	var reviewCount int
	e := admin.QueryRowContext(f.ctx, `SELECT min(l.case_id),min(l.finding_id),count(*) FROM case_resource_links l JOIN case_finding_assessments a USING(case_id,finding_id) WHERE l.source_system_id=$1::uuid AND l.record_ref=$2 AND l.revision=1`, f.resource.Reporter.SourceSystemID, f.resource.RecordRef).Scan(&caseID, &findingID, &reviewCount)
	p3Check(t, e == nil && reviewCount == 1, "browser must save a resource-based review before revision")
	assessmentPath := "/api/v1/cases/" + caseID + "/findings/" + findingID + "/assessment"
	var before workspace.AssessmentView
	p3Check(t, json.Unmarshal(c.request(t, "GET", assessmentPath, nil, 200), &before) == nil && before.Assessment.Review != nil, "original saved human review")
	oldPath, old := f.runtimeResourceRead(t, c)
	p3Check(t, len(old.Items) == 1 && old.Items[0].Record.Revision == 1, "original resource revision")
	fingerprint := func() string {
		var v string
		e := admin.QueryRowContext(f.ctx, `SELECT md5(COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY request_id)::text FROM workbench_economic_facts r),'[]') || COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY result_version)::text FROM case_finding_assessment_actions r WHERE case_id=$1 AND finding_id=$2),'[]') || COALESCE((SELECT record::text FROM case_finding_assessments WHERE case_id=$1 AND finding_id=$2),'') || COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY execution_id,as_of)::text FROM case_executions r WHERE case_id=$1),'[]'))`, caseID, findingID).Scan(&v)
		p3Check(t, e == nil, "economic/manual fingerprint")
		return v
	}
	beforeHash := fingerprint()
	record := f.resource
	record.Revision = 2
	record.PreviousHash = old.Items[0].RecordHash
	record.Resource.Name = "Synthetic only (corrected label)" // occurrence/identity remain the original fact.
	bundlePath := filepath.Join(f.output, "resource-rev2-bundle.json")
	p3JSON(t, bundlePath, map[string]any{"schema_version": sourcefacts.ResourceOfflineBundleVersion, "records": []workbench.ResourceAccessRecord{record}})
	raw, e := os.ReadFile(bundlePath)
	p3Check(t, e == nil, "new revision bundle")
	a := f.resourceAuthorization
	a.AuthorizationID = filepath.Base(f.runDir) + "-rev2"
	a.OperationID = filepath.Base(f.runDir) + "-resource-rev2"
	a.BundleID = a.OperationID
	a.BundlePath = bundlePath
	a.BundleSHA256 = sourcefacts.Hash(raw)
	// Retain exact original target/product/schema/one-record bound and validity window.
	authPath := filepath.Join(f.output, "resource-rev2-authorization.json")
	p3JSON(t, authPath, a)
	owner, e := db.OpenSourceFactsStore(f.adminDSN)
	p3Check(t, e == nil, "revision authorization owner")
	defer owner.Close()
	grant, e := owner.RegisterSourceOfflineAuthorization(f.ctx, a)
	p3Check(t, e == nil, "revision registration")
	_, e = owner.ControlSourceOfflineAuthorization(f.ctx, a.AuthorizationID, "active", grant.Revision, a.Digest(), a)
	p3Check(t, e == nil, "revision enable")
	f.cli(t, "resource-rev2-check", "import-synthetic-resources", "--authorization-file", authPath, "--target-ref", a.TargetRef, "--check")
	var beforeCheckRows int
	e = admin.QueryRowContext(f.ctx, `SELECT count(*) FROM resource_access_records WHERE source_system_id=$1::uuid AND record_ref=$2`, record.Reporter.SourceSystemID, record.RecordRef).Scan(&beforeCheckRows)
	p3Check(t, e == nil && beforeCheckRows == 1 && fingerprint() == beforeHash, "resource check leaves original rows/money/manual state unchanged")
	f.cli(t, "resource-rev2-import", "import-synthetic-resources", "--authorization-file", authPath, "--target-ref", a.TargetRef)
	var receivedAt, oldCutoff time.Time
	e = admin.QueryRowContext(f.ctx, `SELECT d.received_at,r.cutoff FROM resource_access_records d CROSS JOIN runtime_readings r WHERE d.source_system_id=$1::uuid AND d.record_ref=$2 AND d.revision=2 AND r.id=$3`, record.Reporter.SourceSystemID, record.RecordRef, old.ReadingID).Scan(&receivedAt, &oldCutoff)
	p3Check(t, e == nil && receivedAt.After(oldCutoff), "revision actually arrived after frozen resource cutoff")
	var frozen workbench.RuntimeResources
	p3Check(t, json.Unmarshal(c.request(t, "GET", oldPath, nil, 200), &frozen) == nil && len(frozen.Items) == 1 && frozen.Items[0].RecordHash == old.Items[0].RecordHash && frozen.Items[0].Record.Revision == 1 && frozen.Items[0].ChangedSinceReading, "frozen rev1 with change indication")
	_, latest := f.runtimeResourceRead(t, c)
	p3Check(t, len(latest.Items) == 1 && latest.Items[0].Record.Revision == 2 && latest.Items[0].Record.PreviousHash == old.Items[0].RecordHash && latest.Items[0].ChainState == "complete", "new reading adopts complete rev2")
	var after workspace.AssessmentView
	p3Check(t, json.Unmarshal(c.request(t, "GET", assessmentPath, nil, 200), &after) == nil && after.ReviewBasis.State == "changed", "saved review needs reevaluation")
	b, _ := json.Marshal(before.Assessment)
	d, _ := json.Marshal(after.Assessment)
	p3Check(t, bytes.Equal(b, d) && before.CaseVersion == after.CaseVersion && fingerprint() == beforeHash, "human conclusion/history and financial rows unchanged")
	rows := func() (int, string) {
		var count int
		var digest string
		e := admin.QueryRowContext(f.ctx, `SELECT count(*),md5(COALESCE(string_agg(record_hash,'' ORDER BY revision),'')) FROM resource_access_records WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND record_ref=$5`, record.Reporter.SourceSystemID, record.Reporter.SourceInstance, record.Reporter.OrgID, record.Reporter.ProjectID, record.RecordRef).Scan(&count, &digest)
		p3Check(t, e == nil, "resource revision rows")
		return count, digest
	}
	n, hash := rows()
	p3Check(t, n == 2, "exact two immutable resource revisions")
	// Repeat the original rev1 operation, not a third resource or economic event.
	f.cli(t, "resource-original-replay", "import-synthetic-resources", "--authorization-file", filepath.Join(f.output, "resource-authorization.json"), "--target-ref", a.TargetRef)
	nn, hh := rows()
	p3Check(t, nn == n && hh == hash && fingerprint() == beforeHash, "original replay zero new rows and no financial/manual change")
	p3JSON(t, filepath.Join(f.output, "resource-revised.json"), map[string]any{"old": frozen, "new": latest, "assessment_before": before, "assessment_after": after, "case_id": caseID, "finding_id": findingID, "resource_rows": nn, "replay_new_rows": 0, "economic_and_manual_unchanged": true, "received_at": receivedAt, "old_resource_cutoff": oldCutoff, "occurred_at": record.OccurredAt})
}

// Only bounded metadata is retained on failure, never raw driver/CLI errors.
func p3ProcessReceipt(stage string, cmd *exec.Cmd, ctx context.Context, stopRequested bool) map[string]any {
	exit := -1
	if cmd.ProcessState != nil {
		exit = cmd.ProcessState.ExitCode()
	}
	digest := os.Getenv("P3_XI_IMPORTER_SHA256")
	if cmd.Path == os.Getenv("P3_UR_TEST_BINARY") {
		digest = os.Getenv("P3_UR_TEST_BINARY_SHA256")
	} else if cmd.Path == os.Getenv("P3_XI_CTL") {
		digest = os.Getenv("P3_XI_CTL_SHA256")
	}
	return map[string]any{"stage": stage, "binary": cmd.Path, "binary_sha256": digest, "exit_code": exit, "context_timeout": ctx.Err() == context.DeadlineExceeded, "context_cancelled": ctx.Err() == context.Canceled, "stop_requested": stopRequested}
}

// Consume all stderr without retaining or persisting unbounded driver messages.
// Only closed classifications and a five-character SQLSTATE leave this buffer.
const p3CLIDiagnosticLimit = 8 << 10

var p3SQLState = regexp.MustCompile(`\(SQLSTATE ([0-9A-Z]{5})\)$`)

type p3CLIDiagnostic struct {
	prefix bytes.Buffer
	total  int64
}

func (d *p3CLIDiagnostic) Write(p []byte) (int, error) {
	n := len(p)
	d.total += int64(n)
	if remaining := p3CLIDiagnosticLimit - d.prefix.Len(); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = d.prefix.Write(p)
	}
	return n, nil
}

func (d *p3CLIDiagnostic) receipt() map[string]any {
	truncated := d.total > p3CLIDiagnosticLimit
	r := map[string]any{"error_class": "unknown", "stderr_bytes": d.total, "stderr_truncated": truncated}
	if truncated {
		r["error_class"] = "diagnostic_truncated"
		return r
	}
	message := strings.TrimSpace(d.prefix.String())
	prefix := "xinsight-importer: "
	if strings.HasPrefix(message, "xinsightctl: ") {
		prefix = "xinsightctl: "
	}
	if !strings.HasPrefix(message, prefix) {
		return r
	}
	message = strings.TrimPrefix(message, prefix)
	switch message {
	case sourcefacts.ErrAuthorization.Error():
		r["error_class"] = "source_authorization"
	case sourcefacts.ErrInvalid.Error():
		r["error_class"] = "source_contract"
	case sourcefacts.ErrScopeChanged.Error():
		r["error_class"] = "source_scope"
	case workbench.ErrInvalid.Error():
		r["error_class"] = "resource_contract"
	case workbench.ErrConflict.Error():
		r["error_class"] = "resource_conflict"
	default:
		if code := p3SQLState.FindStringSubmatch(message); len(code) == 2 {
			r["sqlstate"] = code[1]
			r["error_class"] = "postgres_error"
			if code[1] == "53300" {
				r["error_class"] = "postgres_too_many_connections"
			}
		}
	}
	return r
}

func TestP3CLIDiagnosticIsBoundedAndSafe(t *testing.T) {
	const secret = "p3-test-secret-do-not-retain"
	for _, tc := range []struct {
		name, stderr, class, sqlstate string
	}{
		{"control_postgres", "xinsightctl: ERROR: " + secret + " (SQLSTATE 42501)\n", "postgres_error", "42501"},
		{"control_conflict", "xinsightctl: " + workbench.ErrConflict.Error(), "resource_conflict", ""},
		{"role_connections", "xinsight-importer: failed to connect to `user=xinsight_importer password=" + secret + "`: FATAL: too many connections for role (SQLSTATE 53300)\n", "postgres_too_many_connections", "53300"},
		{"postgres", "xinsight-importer: ERROR: " + secret + " (SQLSTATE 42501)\n", "postgres_error", "42501"},
		{"authorization", "xinsight-importer: " + sourcefacts.ErrAuthorization.Error() + "\n", "source_authorization", ""},
		{"source_contract", "xinsight-importer: " + sourcefacts.ErrInvalid.Error(), "source_contract", ""},
		{"scope", "xinsight-importer: " + sourcefacts.ErrScopeChanged.Error(), "source_scope", ""},
		{"resource_contract", "xinsight-importer: " + workbench.ErrInvalid.Error(), "resource_contract", ""},
		{"resource_conflict", "xinsight-importer: " + workbench.ErrConflict.Error(), "resource_conflict", ""},
		{"unknown_sensitive", "xinsight-importer: postgres://user:" + secret + "@127.0.0.1/db", "unknown", ""},
		{"invalid_state", "xinsight-importer: " + secret + " (SQLSTATE 533000)", "unknown", ""},
		{"untrusted_prefix", secret + " (SQLSTATE 53300)", "unknown", ""},
		{"empty", "", "unknown", ""},
		{"exact_bound", strings.Repeat("x", p3CLIDiagnosticLimit), "unknown", ""},
		{"truncated", "xinsight-importer: " + strings.Repeat(secret, p3CLIDiagnosticLimit) + " (SQLSTATE 53300)", "diagnostic_truncated", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var d p3CLIDiagnostic
			// Splits include the SQLSTATE marker; the writer must drain every byte.
			for offset := 0; offset < len(tc.stderr); {
				end := offset + 7
				if end > len(tc.stderr) {
					end = len(tc.stderr)
				}
				n, err := d.Write([]byte(tc.stderr[offset:end]))
				if err != nil || n != end-offset {
					t.Fatal("stderr writer did not consume complete chunk")
				}
				offset = end
			}
			r := d.receipt()
			if r["error_class"] != tc.class || r["stderr_bytes"] != int64(len(tc.stderr)) || r["stderr_truncated"] != (len(tc.stderr) > p3CLIDiagnosticLimit) || d.prefix.Len() > p3CLIDiagnosticLimit {
				t.Fatal("incorrect bounded diagnostic")
			}
			if tc.sqlstate == "" {
				if _, present := r["sqlstate"]; present {
					t.Fatal("unexpected SQLSTATE")
				}
			} else if r["sqlstate"] != tc.sqlstate {
				t.Fatal("SQLSTATE mismatch")
			}
			for key := range r {
				if key != "error_class" && key != "stderr_bytes" && key != "stderr_truncated" && key != "sqlstate" {
					t.Fatal("unapproved diagnostic field")
				}
			}
			encoded, err := json.Marshal(r)
			if err != nil || bytes.Contains(encoded, []byte(secret)) || bytes.Contains(encoded, []byte("postgres://")) || bytes.Contains(encoded, []byte("password=")) {
				t.Fatal("sensitive diagnostic data escaped")
			}
		})
	}
}

// Only public, fixed classifications escape memory; never retain messages,
// arbitrary code strings, body snippets or driver errors in a safe receipt.
func p3PublicErrorCode(raw []byte) string {
	if len(raw) > 8*1024 {
		return "unclassified"
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
			Type string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return "unclassified"
	}
	code := envelope.Error.Code
	if code == "" {
		code = envelope.Error.Type
	}
	switch code {
	case "limit_policy.state_unavailable", "workload_admission.configuration_conflict",
		"source_facts.capture_unavailable", "billing_error.provider_cost_state_unavailable",
		"auth_error.state_unavailable", "auth_error.agent_capability_state_unavailable",
		"server_error.service_unavailable", "server_error.public_execution_unavailable",
		"server_error.model_governance_unavailable", "server_error.model_authorization_unavailable",
		"server_error.payload_capacity_exceeded":
		return code
	default:
		return "unclassified"
	}
}
func TestP3PublicErrorCodeIsBoundedAndSafe(t *testing.T) {
	const secret = "synthetic-secret-must-not-escape"
	for _, tc := range []struct{ name, body, want string }{
		{"current", `{"error":{"code":"limit_policy.state_unavailable","type":"server_error","message":"` + secret + `"}}`, "limit_policy.state_unavailable"},
		{"legacy", `{"error":{"type":"server_error.model_authorization_unavailable","message":"` + secret + `"}}`, "server_error.model_authorization_unavailable"},
		{"unknown", `{"error":{"code":"` + secret + `"}}`, "unclassified"},
		{"prefixed", `{"error":{"code":"limit_policy.state_unavailable ` + secret + `"}}`, "unclassified"},
		{"code-precedes-type", `{"error":{"code":"unknown","type":"limit_policy.state_unavailable"}}`, "unclassified"},
		{"wrong-type", `{"error":{"code":{"secret":"` + secret + `"}}}`, "unclassified"},
		{"malformed", `{"error":`, "unclassified"},
		{"trailing", `{"error":{"code":"limit_policy.state_unavailable"}} {}`, "unclassified"},
		{"no-error", `{"message":"` + secret + `"}`, "unclassified"},
		{"oversized", `{"error":{"code":"limit_policy.state_unavailable","message":"` + strings.Repeat("x", 8192) + `"}}`, "unclassified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := p3PublicErrorCode([]byte(tc.body))
			if got != tc.want || strings.Contains(got, secret) {
				t.Fatal("unsafe or incorrect public error classification")
			}
		})
	}
}

const p3ImporterHeadroomSQL = `SELECT rolconnlimit,(SELECT count(*) FROM pg_stat_activity WHERE usename='xinsight_importer') FROM pg_roles WHERE rolname='xinsight_importer'`

func p3ImporterHasHeadroom(limit, used int) bool { return limit > 0 && used >= 0 && used < limit }
func (f *p3BrowserFixture) checkImporterHeadroom(t *testing.T, admin *sql.DB) {
	ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	var limit, used int
	err := admin.QueryRowContext(ctx, p3ImporterHeadroomSQL).Scan(&limit, &used)
	receipt := map[string]any{"query_ok": err == nil, "needed_extra_connections": 1, "successful": err == nil && p3ImporterHasHeadroom(limit, used)}
	if err == nil {
		receipt["role_connection_limit"] = limit
		receipt["observed_connections"] = used
	}
	p3JSON(t, filepath.Join(f.output, "importer-headroom-preflight.json"), receipt)
	p3Check(t, err == nil && p3ImporterHasHeadroom(limit, used), "importer connection headroom before resource CLI")
}

const p3XISettlementSQL = `SELECT
 (SELECT count(*) FROM workbench_economic_facts WHERE request_id=$1 AND terminal_status='settled'),
 (SELECT count(*) FROM workbench_economic_facts WHERE request_id=$1),
 count(*) FILTER (WHERE stage='terminal' AND profile=$2),
 count(*) FILTER (WHERE stage='terminal' AND profile=$3),
 count(*) FILTER (WHERE stage='terminal' AND profile=$4)
 FROM source_fact_documents WHERE request_id=$1`

func (f *p3BrowserFixture) recordSettlement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	states := []map[string]any{}
	for _, id := range []string{f.requestID, f.catchupRequestID} {
		if id == "" {
			continue
		}
		state := map[string]any{"request_id": id, "observed_at": time.Now().UTC(), "query_ok": false, "counts": nil}
		if f.admin != nil {
			var settled, economic, core, identity, cost int
			err := f.admin.QueryRowContext(ctx, p3XISettlementSQL, id, sourcefacts.ContextProfile, sourcefacts.IdentityProfile, sourcefacts.CostProfile).Scan(&settled, &economic, &core, &identity, &cost)
			if err == nil {
				state["query_ok"] = true
				state["counts"] = map[string]int{"settled": settled, "economic_records": economic, "context_terminal_records": core, "identity_terminal_records": identity, "cost_terminal_records": cost}
			}
		}
		states = append(states, state)
	}
	p3JSON(t, filepath.Join(f.output, "settlement-at-close.json"), states)
}
func TestP3WaitCancellation(t *testing.T) {
	for _, mode := range []string{"ready", "cancelled", "timeout", "stop-before-probe", "stop-during-probe"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			if mode == "timeout" {
				var c context.CancelFunc
				ctx, c = context.WithTimeout(ctx, 20*time.Millisecond)
				defer c()
			}
			stop := make(chan struct{})
			if mode == "stop-before-probe" {
				close(stop)
			}
			entered := false
			start := time.Now()
			err := p3Poll(ctx, func() bool {
				select {
				case <-stop:
					return true
				default:
					return false
				}
			}, func(ctx context.Context) bool {
				entered = true
				if mode == "ready" {
					return true
				}
				if mode == "stop-during-probe" {
					close(stop)
				}
				<-ctx.Done()
				return false
			})
			switch mode {
			case "ready":
				if err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				if !errors.Is(err, context.Canceled) || entered {
					t.Fatal("cancel not propagated")
				}
			case "timeout":
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("timeout not propagated")
				}
			default:
				if !errors.Is(err, errP3StopRequested) {
					t.Fatal("stop not propagated")
				}
			}
			if time.Since(start) > time.Second {
				t.Fatal("wait swallowed cancellation")
			}
			if mode == "stop-before-probe" && entered {
				t.Fatal("probe after stop")
			}
		})
	}
}
func TestP3ImporterHeadroom(t *testing.T) {
	for _, v := range []struct {
		limit, used int
		want        bool
	}{{5, 3, true}, {5, 4, true}, {5, 5, false}, {5, 6, false}, {0, 0, false}, {-1, 0, false}, {5, -1, false}} {
		if p3ImporterHasHeadroom(v.limit, v.used) != v.want {
			t.Fatalf("headroom mismatch: %+v", v)
		}
	}
}

// Short production HTTP/PG round trip; no P3 children, providers or browser.
func TestP3FrozenReadingPostgresRoundTrip(t *testing.T) {
	server, _, _, source := startWorkbenchProduction(t)
	c := loginWorkbenchHTTP(t, server, "workbench.one", "workbench password")
	request := httpReadingRequest(source)
	var before, after workbench.Reading
	p3Check(t, json.Unmarshal(c.request(t, "POST", "/api/v1/workbench/readings", request, 201), &before) == nil, "round trip POST")
	p3Check(t, json.Unmarshal(c.request(t, "GET", "/api/v1/workbench/readings/"+before.ID, nil, 200), &after) == nil, "round trip GET")
	report := map[string]any{"period_struct_equal": before.Period == after.Period, "start_equal": before.Period.StartInclusive.Equal(after.Period.StartInclusive), "end_equal": before.Period.EndExclusive.Equal(after.Period.EndExclusive), "timezone_equal": before.Period.Timezone == after.Period.Timezone, "cutoff_equal": before.Cutoff.Equal(after.Cutoff), "membership_equal": before.MembershipRef == after.MembershipRef, "before_period": before.Period, "after_period": after.Period, "before_cutoff": before.Cutoff, "after_cutoff": after.Cutoff, "before_membership": before.MembershipRef, "after_membership": after.MembershipRef}
	raw, err := json.Marshal(report)
	p3Check(t, err == nil, "round trip evidence")
	t.Log(string(raw))
	wire, err := json.Marshal(after)
	p3Check(t, err == nil, "round trip wire")
	_, _, unchanged := p3FrozenReadingEvidence(before, wire)
	p3Check(t, unchanged, "exact PG frozen semantics")
}

// Compare timestamp instants, not time.Time's Location/monotonic representation.
// No truncation, rounding or tolerance: even a one-nanosecond change is rejected.
func p3FrozenReadingEvidence(before workbench.Reading, raw []byte) (workbench.Reading, map[string]any, bool) {
	project := func(r workbench.Reading) map[string]any {
		return map[string]any{"reading_id": r.ID, "period": r.Period, "cutoff": r.Cutoff, "membership_ref": r.MembershipRef}
	}
	var after workbench.Reading
	decoded := json.Unmarshal(raw, &after) == nil
	evidence := map[string]any{"decoded": decoded, "body_sha256": sourcefacts.Hash(raw), "before": project(before), "after": nil, "checks": nil}
	if !decoded {
		return after, evidence, false
	}
	checks := map[string]bool{
		"reading_id":       after.ID == before.ID,
		"period_start":     after.Period.StartInclusive.Equal(before.Period.StartInclusive),
		"period_end":       after.Period.EndExclusive.Equal(before.Period.EndExclusive),
		"display_timezone": after.Period.Timezone == before.Period.Timezone,
		"cutoff":           after.Cutoff.Equal(before.Cutoff),
		"membership_ref":   after.MembershipRef == before.MembershipRef,
	}
	evidence["after"], evidence["checks"] = project(after), checks
	evidence["period_struct_equal"] = after.Period == before.Period // diagnostic only
	unchanged := true
	for _, equal := range checks {
		unchanged = unchanged && equal
	}
	return after, evidence, unchanged
}
func TestP3FrozenReadingComparison(t *testing.T) {
	instant := time.Date(2026, 9, 21, 3, 10, 45, 708074000, time.UTC)
	before := workbench.Reading{ID: "xwr_synthetic", Period: workbench.Period{StartInclusive: instant.Add(-24 * time.Hour), EndExclusive: instant, Timezone: "UTC"}, Cutoff: instant, MembershipRef: "sha256:" + strings.Repeat("a", 64)}
	for _, mode := range []string{"same", "equivalent-offset", "start-one-ns", "end-one-ns", "cutoff-one-ns", "display-timezone", "membership", "reading-id", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			after := before
			switch mode {
			case "equivalent-offset":
				zone := time.FixedZone("synthetic", 8*60*60)
				after.Period.StartInclusive = after.Period.StartInclusive.In(zone)
				after.Period.EndExclusive = after.Period.EndExclusive.In(zone)
				after.Cutoff = after.Cutoff.In(zone)
			case "start-one-ns":
				after.Period.StartInclusive = after.Period.StartInclusive.Add(time.Nanosecond)
			case "end-one-ns":
				after.Period.EndExclusive = after.Period.EndExclusive.Add(time.Nanosecond)
			case "cutoff-one-ns":
				after.Cutoff = after.Cutoff.Add(time.Nanosecond)
			case "display-timezone":
				after.Period.Timezone = "Asia/Shanghai"
			case "membership":
				after.MembershipRef = "sha256:" + strings.Repeat("b", 64)
			case "reading-id":
				after.ID = "xwr_other"
			}
			raw, err := json.Marshal(after)
			p3Check(t, err == nil, "comparison input")
			if mode == "malformed" {
				raw = []byte(`{"unexpected":"never retain this raw value"`)
			}
			_, evidence, ok := p3FrozenReadingEvidence(before, raw)
			if ok != (mode == "same" || mode == "equivalent-offset") {
				t.Fatal("frozen comparison mismatch")
			}
			wire, err := json.Marshal(evidence)
			if err != nil || strings.Contains(string(wire), "never retain this raw value") {
				t.Fatal("unsafe comparison evidence")
			}
			if mode == "malformed" && (evidence["after"] != nil || evidence["checks"] != nil) {
				t.Fatal("undecoded fields must stay unknown")
			}
			if mode != "same" && mode != "equivalent-offset" && mode != "malformed" {
				field := map[string]string{"start-one-ns": "period_start", "end-one-ns": "period_end", "cutoff-one-ns": "cutoff", "display-timezone": "display_timezone", "membership": "membership_ref", "reading-id": "reading_id"}[mode]
				checks := evidence["checks"].(map[string]bool)
				for name, equal := range checks {
					if equal != (name != field) {
						t.Fatalf("incorrect field-level difference: %s", name)
					}
				}
			}
		})
	}
}

const j1Run = "p3-j1-content-20260922-r3"
const j1ApprovedActions = "synthetic-db-redis,http,credentials,content-binding,full-content-policy,human-review-grant,secret-cleanup"

type j1ProducerContent struct {
	CredentialID       string `json:"credential_id"`
	RegistrationHash   string `json:"registration_hash"`
	CapabilitiesSHA256 string `json:"capabilities_sha256"`
}

func j1Approval(t *testing.T) {
	t.Helper()
	p3Check(t, os.Getenv("XI_J1_INTEROP") == "1" && os.Getenv("XI_P3_INTEROP") == "" && os.Getenv("XINSIGHT_SCOPED_BROWSER") == "1", "exclusive J1 scoped opt-in")
	p3Check(t, os.Getenv("J1_APPROVED_RUN") == j1Run && os.Getenv("J1_APPROVED_ACTIONS") == j1ApprovedActions, "J1 runtime actions require external approval before side effects")
	p3Check(t, validJ1ContentRunPaths(os.Getenv("J1_APPROVED_RUN"), os.Getenv("P3_CREDENTIAL_DIR"), os.Getenv("XINSIGHT_SCOPED_BROWSER_OUTPUT")), "J1 exact approved paths")
}
func (f *p3BrowserFixture) resolveContent(ctx context.Context, expected sourcefacts.ContentCollection) (*pullfeed.HTTPSource, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	cfg, e := sourcefacts.LoadManagedConfig(f.contentConfig)
	if e != nil {
		return nil, sourcefacts.ErrUnavailable
	}
	for _, entry := range cfg.Contents {
		if entry.CollectionID != expected.ID {
			continue
		}
		resolved, e := cfg.ResolveContent(entry)
		if e != nil || !sourcefacts.SameContentCollection(resolved.Registration(), expected) || resolved.KeyFile != filepath.Join(f.credentials, "source-content.key") || resolved.Endpoint != f.source.URL+sourcefacts.ContentEndpointPath {
			return nil, sourcefacts.ErrScopeChanged
		}
		key, e := config.LoadSourceFactsServiceKey(resolved.KeyFile)
		if e != nil {
			return nil, sourcefacts.ErrAuthorization
		}
		return pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: resolved.Endpoint, ServiceKey: key, AllowPrivateTunnelHTTP: resolved.AllowPrivateTunnelHTTP, RequestTimeout: 5 * time.Second})
	}
	return nil, sourcefacts.ErrAuthorization
}
func (f *p3BrowserFixture) bootstrapContent(t *testing.T, owner, store *db.SourceFactsStore) {
	f.j1Stage = "content-bootstrap"
	p3Check(t, f.source.Content != nil, "J1 producer C binding")
	keyPath := filepath.Join(f.credentials, "source-content.key")
	key, e := config.LoadSourceFactsServiceKey(keyPath)
	p3Check(t, e == nil, "J1 independent C credential input")
	reader, e := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: f.source.URL + sourcefacts.ContentEndpointPath, ServiceKey: key, AllowPrivateTunnelHTTP: true, RequestTimeout: 5 * time.Second})
	p3Check(t, e == nil, "J1 content source")
	defer reader.CloseContentConnections()
	raw, e := reader.FetchContentCapabilities(f.ctx)
	p3Check(t, e == nil && sourcefacts.Hash(raw) == f.source.Content.CapabilitiesSHA256, "J1 exact capabilities receipt")
	caps, e := sourcefacts.DecodeContentCapabilities(raw)
	p3Check(t, e == nil, "J1 capabilities decode")
	binding := sourcefacts.ManagedCredentialBinding{Name: "j1-content", Scope: "content", SourceSystemID: unirouter.DefaultSourceSystemID, SourceInstance: f.source.SourceInstance, OrgID: f.source.OrgID, ProjectID: f.source.ProjectID, CredentialID: f.source.Content.CredentialID, RegistrationHash: f.source.Content.RegistrationHash, KeyFile: keyPath}
	entry := sourcefacts.ManagedContentConfig{CollectionID: randomTestUUID(t), SourceSystemID: binding.SourceSystemID, SourceInstance: binding.SourceInstance, OrgID: binding.OrgID, ProjectID: binding.ProjectID, CredentialBinding: binding.Name, ContentScopeRevision: caps.ContentScopeRevision, RecoveryEpoch: caps.RecoveryEpoch, AllowedPurposes: caps.AllowedPurposes, CoverageFrom: caps.CoverageFrom, Endpoint: f.source.URL + sourcefacts.ContentEndpointPath, AllowPrivateTunnelHTTP: true}
	cfg := sourcefacts.ManagedConfig{SchemaVersion: sourcefacts.ManagedContentConfigVersion, PollIntervalSeconds: 1, RetryMaximumSeconds: 10, Profiles: []sourcefacts.ManagedProfileConfig{}, Bindings: []sourcefacts.ManagedCredentialBinding{binding}, Contents: []sourcefacts.ManagedContentConfig{entry}}
	p3Check(t, cfg.Validate() == nil, "J1 C-only managed config")
	resolved, e := cfg.ResolveContent(entry)
	p3Check(t, e == nil, "J1 resolve C binding")
	collection, e := owner.RegisterContentCollection(f.ctx, resolved.Registration())
	p3Check(t, e == nil, "J1 formal collection registration")
	_, e = sourcefacts.PreflightContent(f.ctx, reader, collection)
	p3Check(t, e == nil, "J1 content preflight")
	collection, e = owner.ControlContentCollection(f.ctx, collection.ID, "active", collection.Revision, false, collection)
	p3Check(t, e == nil, "J1 content enable")
	f.collection = collection
	f.contentConfig = filepath.Join(f.output, "content-source-facts.json")
	p3JSON(t, f.contentConfig, cfg)
	f.cli(t, "content-bootstrap", "unirouter", "pull-source-facts", "--config", f.contentConfig, "--content-collection-id", collection.ID)
	current, e := store.LoadContentCollection(f.ctx, collection.ID)
	p3Check(t, e == nil && current.Cursor != "" && current.Status == "active" && !current.RequiresAttention && sourcefacts.SameContentCollection(current, collection), "J1 durable empty C bootstrap")
	p3JSON(t, filepath.Join(f.output, "content-registration.json"), current)
}
func (f *p3BrowserFixture) consumeContent(t *testing.T) {
	f.j1Stage = "content-consume-and-replay"
	f.cli(t, "content-consume", "unirouter", "pull-source-facts", "--config", f.contentConfig, "--content-collection-id", f.collection.ID)
	count := func() [3]int {
		var result [3]int
		e := f.admin.QueryRowContext(f.ctx, `SELECT (SELECT count(*) FROM source_content_manifests WHERE collection_id=$1::uuid),(SELECT count(*) FROM source_content_receipts WHERE collection_id=$1::uuid),(SELECT count(*) FROM (SELECT DISTINCT ON(source_fact_id) source_json FROM source_content_manifests WHERE collection_id=$1::uuid AND request_id=$2 ORDER BY source_fact_id,revision DESC) latest WHERE source_json->'capture'->>'completeness'='complete')`, f.collection.ID, f.requestID).Scan(&result[0], &result[1], &result[2])
		p3Check(t, e == nil, "J1 content count query")
		return result
	}
	before := count()
	p3Check(t, before[0] >= 4 && before[1] == before[0] && before[2] == 4, "J1 four complete imported observations")
	f.cli(t, "content-replay", "unirouter", "pull-source-facts", "--config", f.contentConfig, "--content-collection-id", f.collection.ID)
	after := count()
	p3Check(t, before == after, "J1 replay adds no manifest or receipt")
	store, e := db.OpenSourceFactsStore(f.importerDSN)
	p3Check(t, e == nil, "J1 checkpoint reader")
	defer store.Close()
	current, e := store.LoadContentCollection(f.ctx, f.collection.ID)
	p3Check(t, e == nil && current.Cursor != "" && current.Status == "active" && !current.RequiresAttention && current.LastError == "" && sourcefacts.SameContentCollection(current, f.collection), "J1 C checkpoint")
	p3JSON(t, filepath.Join(f.output, "content-imported.json"), map[string]any{"counts_before": before, "counts_after": after, "collection": current, "cursor_sha256": sourcefacts.Hash([]byte(current.Cursor))})
}

// Adds the current scope header required by content and review endpoints. Never
// persists raw errors, account credentials, session cookies, or downloaded bytes.
func (f *p3BrowserFixture) contentHTTP(t *testing.T, c workbenchHTTPClient, method, path string, value any) (int, []byte, http.Header) {
	var session struct {
		Scope string `json:"scope_revision"`
	}
	p3Check(t, json.Unmarshal(c.request(t, "GET", "/api/v1/access/session", nil, 200), &session) == nil && session.Scope != "", "J1 current ordinary scope")
	var body io.Reader
	if value != nil {
		raw, e := json.Marshal(value)
		p3Check(t, e == nil, "J1 request encoding")
		body = bytes.NewReader(raw)
	}
	req, e := http.NewRequestWithContext(f.ctx, method, c.base+path, body)
	p3Check(t, e == nil, "J1 HTTP request")
	req.Header.Set("Origin", c.base)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", c.csrf)
	req.Header.Set("X-XInsight-Scope-Revision", session.Scope)
	res, e := c.client.Do(req)
	p3Check(t, e == nil, "J1 response unknown, do not retry mutation")
	raw, e := io.ReadAll(io.LimitReader(res.Body, (32<<20)+1))
	ce := res.Body.Close()
	p3Check(t, e == nil && ce == nil && len(raw) <= 32<<20, "J1 bounded response")
	return res.StatusCode, raw, res.Header
}
func (f *p3BrowserFixture) verifyContentReview(t *testing.T, server *httptest.Server) {
	f.j1Stage = "ordinary-content-review"
	c := f.ordinaryLogin(t, server, "reviewer")
	c.client.Timeout = 15 * time.Second
	f.contentClient = c
	input := httpReadingRequest(unirouter.DefaultSourceSystemID)
	input.Filters.SourceProjects = []string{"project-" + f.source.ProjectID}
	var reading workbench.RuntimeReading
	status, raw, _ := f.contentHTTP(t, c, "POST", "/api/v1/runtime/readings", workbench.RuntimeReadingRequest{ContractVersion: workbench.RuntimeContractVersion, MetricVersion: workbench.RuntimeMetricVersion, Period: input.Period, Filters: input.Filters})
	p3Check(t, status == 201 && json.Unmarshal(raw, &reading) == nil, "J1 ordinary runtime reading")
	var requests workbench.RuntimeRequests
	status, raw, _ = f.contentHTTP(t, c, "GET", "/api/v1/runtime/readings/"+reading.ID+"/requests?entry_time=all", nil)
	p3Check(t, status == 200 && json.Unmarshal(raw, &requests) == nil && len(requests.Items) == 1 && requests.Items[0].RequestID == f.requestID, "J1 same ordinary request")
	base := "/api/v1/runtime/readings/" + reading.ID + "/requests/" + requests.Items[0].Ref + "/contents"
	var contents workbench.RuntimeContents
	status, raw, _ = f.contentHTTP(t, c, "GET", base, nil)
	p3Check(t, status == 200 && json.Unmarshal(raw, &contents) == nil && len(contents.Items) == 4 && contents.NextCursor == "", "J1 four content references")
	p3JSON(t, filepath.Join(f.output, "ordinary-contents.json"), contents)
	var points []struct {
		Manifest sourcefacts.ContentManifest `json:"manifest"`
		Hash     string                      `json:"source_sha256"`
	}
	pointRaw, e := os.ReadFile(filepath.Join(f.runDir, "ur", "content-points-0.json"))
	p3Check(t, e == nil && json.Unmarshal(pointRaw, &points) == nil && len(points) == 4, "J1 producer four-point evidence")
	observations := map[string]bool{}
	for _, item := range contents.Items {
		ref := item.Reference
		p3Check(t, ref.SourceSystemID == unirouter.DefaultSourceSystemID && ref.SourceInstance == f.source.SourceInstance && ref.OrgID == f.source.OrgID && ref.ProjectID == f.source.ProjectID && ref.RequestID == f.requestID && ref.CaptureStatus == "complete" && ref.Representation != nil && item.Observation != nil, "J1 exact complete content reference")
		var point *sourcefacts.ContentManifest
		for i := range points {
			if points[i].Manifest.SourceFactID == ref.SourceFactID && points[i].Manifest.Revision == ref.Revision && points[i].Manifest.ContentRef == ref.ContentRef && points[i].Hash == ref.SourceSHA256 {
				point = &points[i].Manifest
			}
		}
		p3Check(t, point != nil && point.ObservationPoint == item.Observation.ObservationPoint && !observations[point.ObservationPoint], "J1 UR/XI point and source hash equality")
		observations[point.ObservationPoint] = true
		path := base + "/" + ref.ContentRef + "?source_fact_id=" + url.QueryEscape(ref.SourceFactID) + "&revision=" + url.QueryEscape(ref.Revision)
		status, body, headers := f.contentHTTP(t, c, "GET", path, nil)
		metadataBytes, e := base64.RawURLEncoding.DecodeString(headers.Get(pullfeed.ContentMetadataHeader))
		var metadata sourcefacts.ContentReadMetadata
		p3Check(t, status == 200 && int64(len(body)) == ref.Representation.SavedBytes && sourcefacts.Hash(body) == ref.Representation.Digest && e == nil && json.Unmarshal(metadataBytes, &metadata) == nil && metadata.SourceFactID == ref.SourceFactID && metadata.SourceSHA256 == ref.SourceSHA256 && metadata.ContentRef == ref.ContentRef && reflect.DeepEqual(metadata.Representation, *ref.Representation), "J1 actual bytes and representation verified")
		p3JSON(t, filepath.Join(f.output, "content-body-"+point.ObservationPoint+".json"), map[string]any{"reference": ref, "metadata": metadata, "downloaded_bytes": len(body), "downloaded_sha256": sourcefacts.Hash(body)})
		if point.ObservationPoint == "client_request" {
			f.contentRef = ref
			f.contentBodyPath = path
		}
	}
	p3Check(t, len(observations) == 4 && f.contentBodyPath != "", "J1 full four-point proof")
	// Same source/project and ordinary read scope, but no content grant.
	denied := f.ordinaryLogin(t, server, "reader")
	denied.client.Timeout = 15 * time.Second
	var deniedReading workbench.RuntimeReading
	status, raw, _ = f.contentHTTP(t, denied, "POST", "/api/v1/runtime/readings", workbench.RuntimeReadingRequest{ContractVersion: workbench.RuntimeContractVersion, MetricVersion: workbench.RuntimeMetricVersion, Period: input.Period, Filters: input.Filters})
	p3Check(t, status == 201 && json.Unmarshal(raw, &deniedReading) == nil, "J1 no-C user owns its reading")
	var deniedRequests workbench.RuntimeRequests
	deniedBase := "/api/v1/runtime/readings/" + deniedReading.ID + "/requests"
	status, raw, _ = f.contentHTTP(t, denied, "GET", deniedBase+"?entry_time=all", nil)
	p3Check(t, status == 200 && json.Unmarshal(raw, &deniedRequests) == nil && len(deniedRequests.Items) == 1 && deniedRequests.Items[0].RequestID == f.requestID, "J1 no-C user's exact B request")
	deniedPath := deniedBase + "/" + deniedRequests.Items[0].Ref + "/contents/" + f.contentRef.ContentRef + "?source_fact_id=" + url.QueryEscape(f.contentRef.SourceFactID) + "&revision=" + url.QueryEscape(f.contentRef.Revision)
	status, _, _ = f.contentHTTP(t, denied, "GET", deniedPath, nil)
	p3Check(t, status == 403 || status == 404, "J1 same-project reader without C must be denied")
	p3JSON(t, filepath.Join(f.output, "content-no-grant-denied.json"), map[string]any{"status": status, "subject": "scoped.reader"})
	pointer := "/messages/0/content"
	choice := workspace.ContentChoice{Scope: "finding", Relation: "supports", Reference: f.contentRef, Location: sourcefacts.ContentLocation{Kind: "json_pointer", Pointer: &pointer}}
	command := workspace.ReviewCreationCommand{Operation: "start_review", OperationID: randomTestUUID(t), NewCase: &workspace.CreateCaseInput{Title: "J1 actual captured request"}, Finding: workspace.CreateFindingInput{Title: "Captured prompt review", Statement: "Reviewed this request's stored prompt", Classification: "hypothesis"}, Selection: workspace.BasisSelection{Contents: []workspace.ContentChoice{choice}}, Review: &workspace.ReviewPayload{Conclusion: "supported", Reason: "Read verified stored bytes"}, Reason: "J1 same-user content review"}
	var basis workspace.ReviewBasis
	status, raw, _ = f.contentHTTP(t, c, "POST", "/api/v1/security/reviews/preview", command)
	p3Check(t, status == 200 && json.Unmarshal(raw, &basis) == nil && basis.Digest != "", "J1 review preview")
	command.ExpectedBasisDigest = basis.Digest
	var created workspace.ReviewCreationReceipt
	status, raw, _ = f.contentHTTP(t, c, "POST", "/api/v1/security/reviews", command)
	p3Check(t, status == 200 && json.Unmarshal(raw, &created) == nil && created.CaseID != "" && created.FindingID != "", "J1 same-user committed review")
	path := "/api/v1/cases/" + created.CaseID + "/findings/" + created.FindingID + "/assessment"
	var reopened workspace.AssessmentView
	status, raw, _ = f.contentHTTP(t, c, "GET", path, nil)
	p3Check(t, status == 200 && json.Unmarshal(raw, &reopened) == nil, "J1 reopen review")
	found := false
	for _, action := range reopened.History.Items {
		for _, stored := range action.Command.Selection.Contents {
			if reflect.DeepEqual(stored, choice) {
				found = true
			}
		}
	}
	p3Check(t, found, "J1 reopened exact immutable content choice")
	p3JSON(t, filepath.Join(f.output, "ordinary-content-review.json"), map[string]any{"receipt": created, "choice": choice, "reopened": reopened, "username": "scoped.reviewer"})
}
func (f *p3BrowserFixture) contentNegativeStages(t *testing.T, server *httptest.Server) {
	for _, stage := range []struct {
		name string
		done *bool
	}{{"pause", &f.contentPaused}, {"revoke", &f.contentRevoked}} {
		if *stage.done {
			continue
		}
		_, e := os.Stat(filepath.Join(f.output, "content-"+stage.name))
		if os.IsNotExist(e) {
			continue
		}
		p3Check(t, e == nil && (stage.name != "revoke" || f.contentPaused), "J1 ordered safety negative")
		f.j1Stage = "content-" + stage.name + "-negative"
		p3JSON(t, filepath.Join(f.runDir, "ur", "content-"+stage.name+"-requested"), map[string]any{"run": j1Run})
		f.wait(t, "C "+stage.name+" committed", func(context.Context) bool {
			_, e := os.Stat(filepath.Join(f.runDir, "ur", "content-"+stage.name+".json"))
			return e == nil
		})
		reader, e := f.resolveContent(f.ctx, f.collection)
		p3Check(t, e == nil, "J1 unchanged exact reader binding")
		_, e = reader.FetchContentCapabilities(f.ctx)
		reader.CloseContentConnections()
		var sourceFailure *pullfeed.HTTPSourceError
		wantStatus, wantCode := 503, "source_paused"
		if stage.name == "revoke" {
			wantStatus, wantCode = 401, "unauthorized"
		}
		p3Check(t, errors.As(e, &sourceFailure) && sourceFailure.StatusCode == wantStatus && sourceFailure.Code == wantCode, "J1 exact upstream lifecycle denial, not transport failure")
		status, raw, _ := f.contentHTTP(t, f.contentClient, "GET", f.contentBodyPath, nil)
		p3Check(t, status == 503, "J1 source safety must deny fresh body")
		var problem struct {
			Code string `json:"code"`
		}
		p3Check(t, json.Unmarshal(raw, &problem) == nil && problem.Code == "content_source_unavailable", "J1 classified source safety failure")
		p3JSON(t, filepath.Join(f.output, "content-"+stage.name+"-denied.json"), map[string]any{"status": status, "code": problem.Code, "source_status": sourceFailure.StatusCode, "source_code": sourceFailure.Code, "request_id": f.requestID, "content_ref": f.contentRef.ContentRef})
		*stage.done = true
	}
}

func validJ1ContentRunPaths(run, credentials, output string) bool {
	return run == j1Run && credentials == p3Root+"/credentials/xi-product-test/"+j1Run && output == p3Root+"/evidence/"+j1Run+"/xi"
}
func TestJ1ContentRunPathsRequireFreshR3(t *testing.T) {
	credentials, output := p3Root+"/credentials/xi-product-test/"+j1Run, p3Root+"/evidence/"+j1Run+"/xi"
	t.Run("exact_r3", func(t *testing.T) {
		if !validJ1ContentRunPaths("p3-j1-content-20260922-r3", credentials, output) {
			t.Fatal("fresh exact run rejected")
		}
	})
	for _, old := range []string{"p3-j1-content-20260922-r1", "p3-j1-content-20260922-r2"} {
		oldCredentials, oldOutput := p3Root+"/credentials/xi-product-test/"+old, p3Root+"/evidence/"+old+"/xi"
		for _, v := range []struct{ name, run, credentials, output string }{
			{"run", old, credentials, output},
			{"credentials", j1Run, oldCredentials, output},
			{"output", j1Run, credentials, oldOutput},
			{"all", old, oldCredentials, oldOutput},
		} {
			t.Run(old+"/"+v.name, func(t *testing.T) {
				if validJ1ContentRunPaths(v.run, v.credentials, v.output) {
					t.Fatal("old run or path accepted")
				}
			})
		}
	}
	for _, v := range []struct{ name, run, output string }{
		{"trailing_slash", j1Run, output + "/"}, {"empty_run", "", output},
	} {
		t.Run(v.name, func(t *testing.T) {
			if validJ1ContentRunPaths(v.run, credentials, v.output) {
				t.Fatal("imprecise run path accepted")
			}
		})
	}
}

// Shared formal B registration and empty cursor bootstrap. Dispatch and worker
// lifecycle remain with each caller; this helper never produces a request.
func (f *p3BrowserFixture) bootstrapSourceProfiles(t *testing.T, owner, store *db.SourceFactsStore) string {
	ctx := f.ctx
	configPath := filepath.Join(f.output, "source-facts.json")
	managed := sourcefacts.ManagedConfig{SchemaVersion: sourcefacts.ManagedObservationConfigVersion, PollIntervalSeconds: 1, RetryMaximumSeconds: 10}
	key, e := config.LoadSourceFactsServiceKey(filepath.Join(f.credentials, "source-facts.key"))
	p3CheckError(t, e, "source credential input")
	for _, profile := range []string{sourcefacts.ContextProfile, sourcefacts.IdentityProfile, sourcefacts.CostProfile, sourcefacts.RuntimeProfile, sourcefacts.AttemptProfile, sourcefacts.CapabilityProfile, sourcefacts.PolicyProfile, sourcefacts.DestinationProfile} {
		entry := sourcefacts.ManagedProfileConfig{StreamID: randomTestUUID(t), SourceSystemID: unirouter.DefaultSourceSystemID, SourceInstance: f.source.SourceInstance, OrgID: f.source.OrgID, ProjectID: f.source.ProjectID, Profile: profile, ScopeGeneration: "1", Endpoint: f.source.URL + sourcefacts.ManagedEndpointPath(profile), KeyFile: filepath.Join(f.credentials, "source-facts.key"), AllowPrivateTunnelHTTP: true}
		reader, e := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: entry.Endpoint, ServiceKey: key, AllowPrivateTunnelHTTP: true, RequestTimeout: 5 * time.Second})
		p3CheckError(t, e, "source transport")
		stream, e := owner.RegisterSourceFactsStream(ctx, entry.Registration())
		p3CheckError(t, e, "source registration")
		p3CheckError(t, sourcefacts.Preflight(ctx, reader, stream), "source preflight")
		_, e = owner.ControlSourceFactsStream(ctx, stream.ID, "active", stream.Revision, false)
		p3CheckError(t, e, "source enable")
		managed.Profiles = append(managed.Profiles, entry)
	}
	p3CheckError(t, managed.Validate(), "managed profiles")
	f.managed = managed
	p3JSON(t, configPath, managed)
	for i, p := range managed.Profiles {
		f.cli(t, fmt.Sprintf("source-bootstrap-%d", i), "unirouter", "pull-source-facts", "--config", configPath, "--stream-id", p.StreamID)
		s, e := store.LoadSourceFactsStream(ctx, p.StreamID)
		p3Check(t, e == nil && s.Cursor != "" && s.Status == "active" && !s.RequiresAttention, "durable source bootstrap")
	}
	if f.formal != nil {
		f.formalGrant(t, "reviewer-target", "reviewer", unirouter.DefaultSourceSystemID, "project-"+f.source.ProjectID, false, f.cc14)
		f.formalGrant(t, "reader-target", "reader", unirouter.DefaultSourceSystemID, "project-"+f.source.ProjectID, false, false)
	}
	return configPath
}

// Formal-account mode consumes owner-prepared CLI credentials; it never creates
// subjects/passwords. The original component fixtures remain unchanged.
type p3FormalLogin struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
type p3FormalAccounts struct {
	accountRaw       []byte
	ctl, input, root string
	config           access.AccountConfig
	logins           map[string]p3FormalLogin
	grants           map[string]db.WorkbenchGrantChange
}

func p3FormalMode(get func(string) string) error {
	mode := get("XI_FORMAL_ACCOUNTS")
	if mode == "" {
		return nil
	}
	modes := 0
	for _, key := range []string{"XI_P3_INTEROP", "XI_CC14_INTEROP", "XI_CC15_INTEROP"} {
		if get(key) == "1" {
			modes++
		}
	}
	if mode != "1" || modes != 1 || get("XI_J1_INTEROP") != "" {
		return errors.New("formal accounts require one explicit P3, CC14 or CC15 mode")
	}
	scope, err := p3ScopeFromEnv(get)
	if err != nil || !cc14Digest(get("P3_XI_CTL_SHA256")) || !strings.HasPrefix(get("P3_XI_CTL"), scope.root+"/") || filepath.Clean(get("P3_XI_CTL")) != get("P3_XI_CTL") {
		return errors.New("formal accounts require exact ctl identity")
	}
	return nil
}

func p3FormalRoles(config access.AccountConfig, logins map[string]p3FormalLogin) error {
	if config.SchemaVersion != access.AccountConfigSchemaV3 || len(config.Accounts) != 2 || len(logins) != 2 {
		return errors.New("exactly two v3 ordinary accounts required")
	}
	seen := map[string]bool{}
	for _, a := range config.Accounts {
		kind := ""
		switch a.Role {
		case access.RoleScopedReviewer:
			kind = "reviewer"
		case access.RoleScopedReader:
			kind = "reader"
		}
		l, ok := logins[kind]
		if kind == "" || seen[kind] || !ok || a.Disabled || l.Username != a.Username || len(l.Password) < access.MinPasswordBytes || len(l.Password) > access.MaxPasswordBytes || len(a.SubjectID) != 35 || !strings.HasPrefix(a.SubjectID, "xs_") || strings.Trim(a.SubjectID[3:], "0123456789abcdef") != "" {
			return errors.New("formal account role/identity mismatch")
		}
		seen[kind] = true
	}
	if config.Accounts[0].SubjectID == config.Accounts[1].SubjectID || config.Accounts[0].Username == config.Accounts[1].Username || logins["reviewer"].Password == logins["reader"].Password {
		return errors.New("formal identities must be distinct")
	}
	return nil
}

func p3ReadFormalFile(t *testing.T, path string) []byte {
	t.Helper()
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	p3Check(t, err == nil, "protected formal input open")
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	p3Check(t, err == nil, "formal input stat")
	st, ok := info.Sys().(*syscall.Stat_t)
	p3Check(t, ok && st.Uid == 0 && st.Nlink == 1 && info.Mode().IsRegular() && info.Mode().Perm() == 0600 && info.Size() <= access.MaxAccountFileBytes, "formal input owner/mode/size")
	raw, err := io.ReadAll(io.LimitReader(f, access.MaxAccountFileBytes+1))
	p3Check(t, err == nil && len(raw) <= access.MaxAccountFileBytes, "formal input read")
	return raw
}

func (f *p3BrowserFixture) prepareFormalAccounts(t *testing.T) {
	t.Helper()
	p3Check(t, p3FormalMode(os.Getenv) == nil, "formal mode admission")
	if os.Getenv("XI_FORMAL_ACCOUNTS") == "" {
		return
	}
	a := &p3FormalAccounts{ctl: p3Binary(t, "P3_XI_CTL"), input: f.credentials + "-accounts", root: filepath.Join(f.credentials, "account-cli-root"), grants: map[string]db.WorkbenchGrantChange{}}
	p3PrivateDirectory(t, a.input, true)
	raw := p3ReadFormalFile(t, filepath.Join(a.input, "access-accounts.json"))
	var err error
	a.accountRaw = raw
	a.config, err = access.ParseAccountConfig(raw)
	p3Check(t, err == nil, "formal v3 account file")
	p3Check(t, cc14Decode(p3ReadFormalFile(t, filepath.Join(a.input, "logins.json")), &a.logins) == nil && p3FormalRoles(a.config, a.logins) == nil, "two approved formal login identities")
	var receipt struct {
		Run       string `json:"run"`
		CTL       string `json:"ctl_sha256"`
		Subjects  bool   `json:"generate_subject_passed"`
		TTY       bool   `json:"hash_password_tty_passed"`
		Validated bool   `json:"validate_passed"`
	}
	p3Check(t, cc14Decode(p3ReadFormalFile(t, filepath.Join(a.input, "prepared.json")), &receipt) == nil && receipt.Run == filepath.Base(f.runDir) && receipt.CTL == os.Getenv("P3_XI_CTL_SHA256") && receipt.Subjects && receipt.TTY && receipt.Validated, "owner CLI preparation receipt")
	f.formal = a
}

func (f *p3BrowserFixture) formalCLI(t *testing.T, args ...string) []byte {
	t.Helper()
	cmd := exec.CommandContext(f.ctx, f.formal.ctl, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", hostctl.AllowTestRootEnv + "=1", hostctl.TestRootEnv + "=" + f.formal.root}
	// Neither CLI errors nor raw output (which could contain identity fields) go
	// to the test log. Only bounded selected success receipts are recorded.
	var diagnostic p3CLIDiagnostic
	portable := os.Getenv("P3_PORTABLE") == "1" && os.Getenv("XI_P3_INTEROP") == "1"
	cmd.Stderr = io.Discard
	if portable {
		cmd.Stderr = &diagnostic
	}
	raw, err := cmd.Output()
	if err != nil && portable {
		receipt := p3ProcessReceipt(p3FormalFailureStep(args), cmd, f.ctx, false)
		receipt["diagnostic"] = diagnostic.receipt()
		p3JSON(t, filepath.Join(f.output, "formal-cli-failure.json"), receipt)
	}
	p3Check(t, err == nil, "formal CLI command succeeded")
	return raw
}

func TestP3FailureReceiptProbe(t *testing.T) {
	// Selected only by the offline fault-injection parent. No DB, HTTP,
	// account, service or harness admission is executed by this probe.
	switch os.Getenv("P3_FAILURE_PROBE_MODE") {
	case "check":
		p3CheckError(t, fmt.Errorf("synthetic private context: %w", sourcefacts.ErrScopeChanged), "source preflight")
	case "sql":
		p3CheckError(t, errors.New("synthetic-private-dsn-token-body (SQLSTATE 42501)"), "resource registration")
	case "formal":
		f := &p3BrowserFixture{ctx: context.Background(), output: os.Getenv("XINSIGHT_SCOPED_BROWSER_OUTPUT"), formal: &p3FormalAccounts{ctl: "/usr/bin/false"}}
		f.formalCLI(t, "workbench-access", "apply", "--file", "/synthetic-unused", "--check")
	case "importer":
		f := &p3BrowserFixture{ctx: context.Background(), output: os.Getenv("XINSIGHT_SCOPED_BROWSER_OUTPUT"), importer: "/usr/bin/false"}
		f.cli(t, "source-bootstrap-3", "unirouter", "pull-source-facts")
	default:
		t.Skip("offline failure probe requires explicit parent selection")
	}
}

func TestP3RealFailureReceiptsAreClosed(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"check", "sql", "formal", "importer"} {
		t.Run(mode, func(t *testing.T) {
			output := t.TempDir()
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestP3FailureReceiptProbe$")
			cmd.Env = []string{"P3_PORTABLE=1", "XI_P3_INTEROP=1", "P3_FAILURE_PROBE_MODE=" + mode, "XINSIGHT_SCOPED_BROWSER_OUTPUT=" + output, "P3_XI_CTL=/usr/bin/false", "P3_XI_CTL_SHA256=" + strings.Repeat("c", 64)}
			if _, err := cmd.CombinedOutput(); err == nil {
				t.Fatal("real injected check/CLI failure unexpectedly passed")
			}
			name, step, class := "check-failure.json", "source_preflight", "source_scope"
			if mode == "sql" {
				step, class = "resource_registration", "postgres_error"
			} else if mode == "formal" {
				name, step, class = "formal-cli-failure.json", "formal_grant_check", "unknown"
			} else if mode == "importer" {
				name, step, class = "source-bootstrap-3-failure.json", "source-bootstrap-3", "unknown"
			}
			raw, err := os.ReadFile(filepath.Join(output, name))
			var receipt struct {
				Step, Stage, BinarySHA256 string
				Diagnostic                struct {
					ErrorClass string `json:"error_class"`
					SQLState   string `json:"sqlstate"`
				}
			}
			// Read using the same exact field spelling as the existing receipts.
			var fields map[string]json.RawMessage
			if err != nil || json.Unmarshal(raw, &fields) != nil || strings.Contains(string(raw), "synthetic-private-dsn-token-body") || strings.Contains(string(raw), "synthetic private context") {
				t.Fatal("real failure receipt absent or raw context retained")
			}
			key := "step"
			if mode == "formal" || mode == "importer" {
				key = "stage"
			}
			if json.Unmarshal(fields[key], &receipt.Step) != nil || receipt.Step != step || json.Unmarshal(fields["diagnostic"], &receipt.Diagnostic) != nil || receipt.Diagnostic.ErrorClass != class {
				t.Fatal("real failure lost its closed step/category")
			}
			if mode == "sql" && receipt.Diagnostic.SQLState != "42501" {
				t.Fatal("SQLSTATE missing")
			}
			if mode == "formal" {
				if json.Unmarshal(fields["binary_sha256"], &receipt.BinarySHA256) != nil || receipt.BinarySHA256 != strings.Repeat("c", 64) {
					t.Fatal("formal CLI digest was confused with importer")
				}
			}
			if mode == "formal" || mode == "importer" {
				var exit int
				if json.Unmarshal(fields["exit_code"], &exit) != nil || exit != 1 {
					t.Fatal("injected command did not actually exit1")
				}
			}
		})
	}
}

func TestP3FormalFailureStepIsClosed(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"access", "validate", "--file", "/private-account"}, "formal_validate"},
		{[]string{"workbench-access", "apply", "--file", "/private-grant", "--check"}, "formal_grant_check"},
		{[]string{"workbench-access", "apply", "--file", "/private-grant"}, "formal_grant_apply"},
		{[]string{"workbench-access", "show", "--grant-id", "private-id"}, "formal_grant_show"},
		{nil, "unclassified"}, {[]string{"private-account"}, "unclassified"},
		{[]string{"workbench-access", "apply", "--file", "/private", "private-option"}, "unclassified"},
	} {
		if p3FormalFailureStep(tc.args) != tc.want {
			t.Fatal("argument escaped closed formal step")
		}
	}
	for _, stage := range []string{"private-password", "source preflight\nprivate", "/private/path"} {
		if p3FailureCheckStep(stage) != "unclassified" {
			t.Fatal("assertion escaped closed check")
		}
	}
}

func p3FormalFailureStep(args []string) string {
	if len(args) == 4 && args[0] == "access" && args[1] == "validate" && args[2] == "--file" {
		return "formal_validate"
	}
	if len(args) >= 4 && args[0] == "workbench-access" && args[1] == "apply" && args[2] == "--file" {
		if len(args) == 5 && args[4] == "--check" {
			return "formal_grant_check"
		}
		if len(args) == 4 {
			return "formal_grant_apply"
		}
	}
	if len(args) == 4 && args[0] == "workbench-access" && args[1] == "show" && args[2] == "--grant-id" {
		return "formal_grant_show"
	}
	return "unclassified"
}

func (f *p3BrowserFixture) setupFormalAccounts(t *testing.T, admin, importer, workspace, reader string) access.CredentialProvider {
	t.Helper()
	a := f.formal
	var owned []string
	// The root must be new. Cleanup removes only exact paths created below;
	// owner-supplied accounts/logins/prepared inputs are retained by the owner.
	p3Check(t, os.Mkdir(a.root, 0700) == nil, "fresh formal CLI root")
	owned = append(owned, a.root)
	t.Cleanup(func() {
		if p3RemoveFormalOwned(owned) {
			t.Error("formal exact-path cleanup failed; retain for owner inspection")
		}
	})
	for _, rel := range []string{"etc", "etc/x-insight", "etc/x-insight/database", "etc/x-insight/database/sets", "etc/x-insight/database/sets/fixture", "run", "run/lock", "grants"} {
		path := filepath.Join(a.root, rel)
		p3Check(t, os.Mkdir(path, 0700) == nil, "formal directory create")
		owned = append(owned, path)
	}
	write := func(rel string, raw []byte) {
		path := filepath.Join(a.root, rel)
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		p3Check(t, err == nil, "formal exclusive file create")
		owned = append(owned, path)
		_, err = file.Write(raw)
		ce := file.Close()
		p3Check(t, err == nil && ce == nil, "formal protected file write")
	}
	for _, v := range []struct{ name, value string }{{"admin", admin}, {"importer", importer}, {"workspace", workspace}, {"reader", reader}} {
		write("etc/x-insight/database/sets/fixture/postgres-"+v.name+".dsn", []byte(v.value))
	}
	link := filepath.Join(a.root, "etc/x-insight/database/current")
	p3Check(t, os.Symlink(filepath.Join(a.root, "etc/x-insight/database/sets/fixture"), link) == nil, "formal exact credential set link")
	owned = append(owned, link)
	accountPath := filepath.Join(a.root, "etc/x-insight/access-accounts.json")
	write("etc/x-insight/access-accounts.json", a.accountRaw)
	// CLI owns only this known lock beneath our exclusively-created root.
	owned = append(owned, filepath.Join(a.root, "run/lock/xinsightctl.lock"))
	// Grant files are a fixed finite list; repeated actions overwrite none.
	owned = append(owned, p3FormalOwnedGrantFiles(a.root)...)
	before := p3FormalFiles(t, a.root)
	var validation struct {
		Schema   string `json:"schema_version"`
		Count    int    `json:"account_count"`
		Reviewer int    `json:"workbench_investigation_reviewer_count"`
		Reader   int    `json:"workbench_investigation_reader_count"`
		Disabled int    `json:"disabled_count"`
	}
	p3Check(t, json.Unmarshal(f.formalCLI(t, "access", "validate", "--file", accountPath), &validation) == nil && validation.Schema == access.AccountConfigSchemaV3 && validation.Count == 2 && validation.Reviewer == 1 && validation.Reader == 1 && validation.Disabled == 0, "formal CLI role counts")
	p3Check(t, before == p3FormalFiles(t, a.root), "access validate has no filesystem writes")
	t.Setenv("CREDENTIALS_DIRECTORY", filepath.Dir(accountPath))
	provider, err := access.LoadLocalProvider(access.DiscardAuditor{})
	p3Check(t, err == nil, "production account file loader")
	p3JSON(t, filepath.Join(f.output, "formal-accounts.json"), map[string]any{"run": filepath.Base(f.runDir), "ctl_sha256": os.Getenv("P3_XI_CTL_SHA256"), "account_count": 2, "reviewer_count": 1, "reader_count": 1, "validated_without_writes": true, "prepared_by_owner": true, "boundary": "CLI and production loader/handler; not installed service restart"})
	return provider
}

func p3FormalOwnedGrantFiles(root string) []string {
	names := []string{"reviewer-target", "reader-target", "reviewer-reporter", "reader-reporter", "reader-content", "reviewer-revoke-target", "reviewer-revoke-reporter", "reviewer-action-reporter", "reader-action-reporter"}
	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join(root, "grants", name+".json"))
	}
	return paths
}

// Remove only exact files and directories created by this fixture. Unknown
// children keep their parent in place and make the cleanup visibly fail.
func p3RemoveFormalOwned(owned []string) (failed bool) {
	for i := len(owned) - 1; i >= 0; i-- {
		if err := os.Remove(owned[i]); err != nil && !os.IsNotExist(err) {
			failed = true
		}
	}
	return failed
}

// A small equality checkpoint for this fixture's protected CLI root, never
// exported: catches new operation files, content changes and metadata changes.
func p3FormalFiles(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s:%d:%d:%d;", strings.TrimPrefix(path, root), info.Mode(), info.Size(), info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			raw, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			fmt.Fprintf(&b, "%x;", sha256.Sum256(raw))
		}
		if info.Mode()&os.ModeSymlink != 0 {
			v, e := os.Readlink(path)
			if e != nil {
				return e
			}
			b.WriteString(v)
		}
		return nil
	})
	p3Check(t, err == nil, "formal configuration checkpoint")
	return b.String()
}

func (f *p3BrowserFixture) formalGrant(t *testing.T, name, kind, source, project string, resource, content bool) {
	t.Helper()
	a := f.formal
	var subject string
	for _, entry := range a.config.Accounts {
		if entry.Username == a.logins[kind].Username {
			subject = entry.SubjectID
		}
	}
	all, zero := "*", int64(0)
	caps := []string{"summary", "details", "coverage", "preferences", "source_context", "source_identity", "source_cost", "source_runtime"}
	if resource {
		caps = []string{"resource"}
	}
	r := db.WorkbenchGrantChange{SchemaVersion: "xinsight.workbench-grant-change.v2", Action: "create", ID: randomTestUUID(t), Subject: subject, Source: source, SourceProject: &project, CostProject: &all, ExpectedRevision: &zero, Capabilities: caps, AllowContent: &content, AllowedContentPurposes: []string{}}
	if content {
		r.AllowedContentPurposes = []string{"human_review"}
	}
	if name == "reader-content" {
		prior := a.grants["reader-target"]
		r.ID = prior.ID
		r.Action = "replace"
		var shown struct {
			Revision int64 `json:"revision"`
		}
		p3Check(t, json.Unmarshal(f.formalCLI(t, "workbench-access", "show", "--grant-id", prior.ID), &shown) == nil && shown.Revision > 0, "reader current content grant revision")
		r.ExpectedRevision = &shown.Revision
	}
	f.applyFormalGrant(t, name, r)
}

func (f *p3BrowserFixture) applyFormalGrant(t *testing.T, name string, r db.WorkbenchGrantChange) {
	t.Helper()
	a := f.formal
	raw, err := json.Marshal(r)
	p3Check(t, err == nil && r.Validate() == nil, "formal grant exact scope")
	path := filepath.Join(a.root, "grants", name+".json")
	p3JSON(t, path, r)
	// Compare all grants and their audit domain; unrelated import auditing is
	// allowed to proceed. Compare full grant/audit
	// state around --check, then compare apply's committed After with show.
	dbState := func() string {
		var state string
		err := f.admin.QueryRowContext(f.ctx, `SELECT json_build_array((SELECT COALESCE(json_agg(g ORDER BY id),'[]'::json) FROM workbench_access_grants g),(SELECT COALESCE(json_agg(a ORDER BY id),'[]'::json) FROM access_audit_records a WHERE resource_kind='workbench_grant'))::text`).Scan(&state)
		p3Check(t, err == nil, "formal grant/audit checkpoint")
		return state
	}
	before, files := dbState(), p3FormalFiles(t, a.root)
	var auditBefore, auditAfter int
	p3Check(t, f.admin.QueryRowContext(f.ctx, "SELECT count(*) FROM access_audit_records WHERE resource_kind='workbench_grant' AND resource_ref=$1", r.ID).Scan(&auditBefore) == nil, "grant audit before")
	var check, applied db.WorkbenchGrantResult
	p3Check(t, json.Unmarshal(f.formalCLI(t, "workbench-access", "apply", "--file", path, "--check"), &check) == nil && check.CheckOnly && !check.Changed, "grant check receipt")
	p3Check(t, before == dbState() && files == p3FormalFiles(t, a.root), "grant check has no DB/audit/filesystem writes")
	p3Check(t, json.Unmarshal(f.formalCLI(t, "workbench-access", "apply", "--file", path), &applied) == nil && applied.Changed && !applied.CheckOnly && applied.ID == r.ID && applied.Revision == *r.ExpectedRevision+1, "formal grant apply revision")
	beforeShow, filesBeforeShow := dbState(), p3FormalFiles(t, a.root)
	var shown, after any
	p3Check(t, json.Unmarshal(f.formalCLI(t, "workbench-access", "show", "--grant-id", r.ID), &shown) == nil && json.Unmarshal(applied.After, &after) == nil && reflect.DeepEqual(shown, after), "formal grant show matches committed state")
	p3Check(t, beforeShow == dbState() && filesBeforeShow == p3FormalFiles(t, a.root), "grant show has no DB/audit/filesystem writes")
	p3Check(t, f.admin.QueryRowContext(f.ctx, "SELECT count(*) FROM access_audit_records WHERE resource_kind='workbench_grant' AND resource_ref=$1 AND actor_kind='root' AND actor_ref='root'", r.ID).Scan(&auditAfter) == nil && auditAfter == auditBefore+1, "grant committed root audit")
	a.grants[name] = r
	p3JSON(t, filepath.Join(f.output, "formal-"+name+".json"), map[string]any{"request": json.RawMessage(raw), "check": check, "applied": applied, "show": shown, "check_no_writes": true})
}

func (f *p3BrowserFixture) ordinaryLogin(t *testing.T, server *httptest.Server, kind string) workbenchHTTPClient {
	if f.formal == nil {
		return loginWorkbenchHTTP(t, server, "scoped."+kind, "synthetic responsibility password")
	}
	l, ok := f.formal.logins[kind]
	p3Check(t, ok, "formal login role present")
	return loginWorkbenchHTTP(t, server, l.Username, l.Password)
}
func (f *p3BrowserFixture) ordinaryUsername(kind string) string {
	if f.formal == nil {
		return "scoped." + kind
	}
	return f.formal.logins[kind].Username
}

func TestP3FormalAccountRolesPure(t *testing.T) {
	fixture := func() (access.AccountConfig, map[string]p3FormalLogin) {
		return access.AccountConfig{SchemaVersion: access.AccountConfigSchemaV3, Accounts: []access.AccountConfigEntry{
			{SubjectID: "xs_" + strings.Repeat("a", 32), Username: "fresh-reviewer", Role: access.RoleScopedReviewer},
			{SubjectID: "xs_" + strings.Repeat("b", 32), Username: "fresh-reader", Role: access.RoleScopedReader},
		}}, map[string]p3FormalLogin{"reviewer": {Username: "fresh-reviewer", Password: "noncredential-reviewer-fixture"}, "reader": {Username: "fresh-reader", Password: "noncredential-reader-fixture"}}
	}
	c, l := fixture()
	if p3FormalRoles(c, l) != nil {
		t.Fatal("two ordinary roles rejected")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*access.AccountConfig, map[string]p3FormalLogin)
	}{
		{"admin_role", func(c *access.AccountConfig, l map[string]p3FormalLogin) {
			c.Accounts[0].Role = access.RolePlatformAdmin
		}},
		{"disabled", func(c *access.AccountConfig, l map[string]p3FormalLogin) { c.Accounts[0].Disabled = true }},
		{"repeated_role", func(c *access.AccountConfig, l map[string]p3FormalLogin) { c.Accounts[1].Role = c.Accounts[0].Role }},
		{"wrong_username", func(c *access.AccountConfig, l map[string]p3FormalLogin) { c.Accounts[0].Username = "other" }},
		{"legacy_subject", func(c *access.AccountConfig, l map[string]p3FormalLogin) {
			c.Accounts[0].SubjectID = "xs_8899aabbccddeeff"
		}},
		{"duplicate_subject", func(c *access.AccountConfig, l map[string]p3FormalLogin) {
			c.Accounts[1].SubjectID = c.Accounts[0].SubjectID
		}},
		{"missing_login", func(c *access.AccountConfig, l map[string]p3FormalLogin) { delete(l, "reader") }},
		{"shared_password", func(c *access.AccountConfig, l map[string]p3FormalLogin) {
			v := l["reader"]
			v.Password = l["reviewer"].Password
			l["reader"] = v
		}},
		{"extra_login", func(c *access.AccountConfig, l map[string]p3FormalLogin) { l["admin"] = l["reader"] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, l := fixture()
			tc.mutate(&c, l)
			if p3FormalRoles(c, l) == nil {
				t.Fatal("invalid formal identities accepted")
			}
		})
	}
}
func TestP3FormalAccountModePure(t *testing.T) {
	if p3FormalMode(func(string) string { return "" }) != nil {
		t.Fatal("ordinary component mode changed")
	}
	for _, mode := range []string{"XI_P3_INTEROP", "XI_CC14_INTEROP"} {
		env := map[string]string{"XI_FORMAL_ACCOUNTS": "1", mode: "1", "P3_XI_CTL": p3Root + "/candidate/xinsightctl", "P3_XI_CTL_SHA256": strings.Repeat("a", 64)}
		if p3FormalMode(func(k string) string { return env[k] }) != nil {
			t.Fatal("explicit admitted mode rejected")
		}
		for _, tc := range []struct{ k, v string }{{"XI_FORMAL_ACCOUNTS", "0"}, {mode, ""}, {"XI_J1_INTEROP", "1"}, {"P3_XI_CTL", "/tmp/ctl"}, {"P3_XI_CTL", p3Root + "/../ctl"}, {"P3_XI_CTL_SHA256", "short"}} {
			old := env[tc.k]
			env[tc.k] = tc.v
			if p3FormalMode(func(k string) string { return env[k] }) == nil {
				t.Fatal("bad formal mode accepted")
			}
			env[tc.k] = old
		}
	}
}
