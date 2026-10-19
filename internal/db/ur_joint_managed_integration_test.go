//go:build integration

package db_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/checkpointepoch"
	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/connectoractivation"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/connectorsetup"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/integration/reviewcopy"
	"github.com/foigent/x-insight/internal/integration/urmachine"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/reviewpolicycontrol"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"strings"
)

// The only host substitute is short-lived systemd state/commands. All root
// files, production preparations, control mux/Unix dispatch and DB are real.
type jointManagedRunner struct {
	mu                   sync.Mutex
	active, enabled      map[string]bool
	sourceRestarts       int
	onSourceStart        func() error
	invocation           string
	controllerUnitPath   string
	controllerAllowed    string
	controllerInvocation string
	controllerRestarts   uint64
}

type jointReviewProvider struct{ subject string }

func (p jointReviewProvider) Authenticate(username string, password []byte) (access.Principal, bool) {
	if username == "joint.admin" && string(password) == "synthetic joint admin" {
		return access.Principal{SubjectID: "xs_0123456789abcdef", Role: access.RolePlatformAdmin}, true
	}
	if username != "joint.reviewer" || string(password) != "synthetic joint review" {
		return access.Principal{}, false
	}
	return access.Principal{SubjectID: p.subject, Role: access.RoleWorkbenchReader}, true
}

func (r *jointManagedRunner) Run(_ context.Context, c hostctl.Command) (hostctl.CommandResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c.Name != "systemctl" || len(c.Args) == 0 {
		return hostctl.CommandResult{}, fmt.Errorf("unexpected joint host command")
	}
	if c.Args[0] == "daemon-reload" {
		for _, path := range []string{r.controllerUnitPath + ".d/19-ur-target-network.conf", r.controllerUnitPath + ".d/20-ur-machine-connection.conf"} {
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(raw), "\n") {
				if strings.HasPrefix(line, "IPAddressAllow=") && line != "IPAddressAllow=" && line != "IPAddressAllow=localhost" {
					remote := strings.TrimPrefix(line, "IPAddressAllow=")
					// v249 removes a loopback host entry already covered by
					// localhost. Keep the joint substitute honest about that
					// systemd behavior; hostctl tests verify the full comparison.
					r.controllerAllowed = "127.0.0.0/8 ::1/128"
					if remote != "127.0.0.1/32" && remote != "::1/128" {
						r.controllerAllowed += " " + remote
					}
				}
			}
		}
		return hostctl.CommandResult{}, nil
	}
	if len(c.Args) == 3 && c.Args[0] == "--no-block" && c.Args[1] == "restart" && c.Args[2] == "xinsight-connector-controller.service" {
		r.controllerRestarts++
		r.controllerInvocation = fmt.Sprintf("%032x", r.controllerRestarts)
		r.active[c.Args[2]] = true
		return hostctl.CommandResult{}, nil
	}
	if len(c.Args) < 2 {
		return hostctl.CommandResult{}, fmt.Errorf("invalid joint unit command")
	}
	u := c.Args[1]
	if c.Args[0] == "show" && len(c.Args) == 5 {
		out := ""
		switch c.Args[3] {
		case "LoadState":
			out = "loaded"
		case "ActiveState":
			out = "inactive"
			if r.active[u] {
				out = "active"
			}
		case "InvocationID":
			out = r.invocation
			if u == "xinsight-connector-controller.service" && r.controllerInvocation != "" {
				out = r.controllerInvocation
			}
		case "IPAddressAllow":
			out = r.controllerAllowed
		case "MainPID":
			if r.active[u] {
				out = strconv.Itoa(os.Getpid())
			} else {
				out = "0"
			}
		case "UnitFileState":
			out = "disabled"
			if r.enabled[u] {
				out = "enabled"
			}
		default:
			return hostctl.CommandResult{}, fmt.Errorf("unexpected joint systemd property")
		}
		return hostctl.CommandResult{Output: out}, nil
	}
	switch c.Args[0] {
	case "stop":
		r.active[u] = false
	case "disable":
		r.enabled[u] = false
	case "enable":
		r.enabled[u] = true
	case "start", "restart":
		r.active[u] = true
		if c.Args[0] == "restart" && u == "xinsight-connector-controller.service" {
			r.controllerRestarts++
			r.controllerInvocation = fmt.Sprintf("%032x", r.controllerRestarts)
		}
		if u == "xinsight-source-facts.service" {
			r.sourceRestarts++
			if r.onSourceStart != nil {
				if err := r.onSourceStart(); err != nil {
					return hostctl.CommandResult{}, err
				}
			}
		}
	default:
		return hostctl.CommandResult{}, fmt.Errorf("unexpected joint systemd action")
	}
	return hostctl.CommandResult{}, nil
}

func TestJointControllerRestartPreservesConsumerInvocations(t *testing.T) {
	r := &jointManagedRunner{active: map[string]bool{}, invocation: strings.Repeat("a", 32)}
	consumerInvocation := r.invocation
	readInvocation := func(unit string) string {
		t.Helper()
		result, err := r.Run(t.Context(), hostctl.Command{Name: "systemctl", Args: []string{"show", unit, "--property", "InvocationID", "--value"}})
		if err != nil {
			t.Fatal(err)
		}
		return result.Output
	}
	before := readInvocation("xinsight-connector-controller.service")
	for i := 0; i < 2; i++ {
		if _, err := r.Run(t.Context(), hostctl.Command{Name: "systemctl", Args: []string{"--no-block", "restart", "xinsight-connector-controller.service"}}); err != nil {
			t.Fatal(err)
		}
		after := readInvocation("xinsight-connector-controller.service")
		if after == before {
			t.Fatal("controller restart retained the old invocation")
		}
		for _, consumer := range []string{"xinsight.service", "xinsight-source-facts.service"} {
			if readInvocation(consumer) != consumerInvocation {
				t.Fatal("controller restart changed a body consumer invocation")
			}
		}
		before = after
	}
}

func TestJointManagedRunnerPublishesFormalTargetNetwork(t *testing.T) {
	t.Setenv(hostctl.AllowTestRootEnv, "1")
	t.Setenv(hostctl.TestRootEnv, t.TempDir())
	paths, err := hostctl.PathsFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	e, err := hostctl.NewEngine(paths)
	if err != nil {
		t.Fatal(err)
	}
	runner := &jointManagedRunner{active: map[string]bool{"xinsight-connector-controller.service": true}, enabled: map[string]bool{},
		invocation: strings.Repeat("a", 32), controllerUnitPath: paths.ControllerServiceUnit, controllerAllowed: "127.0.0.0/8 ::1/128"}
	e.Runner = runner
	for path, body := range map[string]string{
		paths.ConnectorIdentityEnv: connectorcontrol.DeploymentIDEnv + "=xid_AAAAAAAAAAAAAAAAAAAAAA\n",
		paths.TrustedTLSProxyEnv:   api.TrustedTLSProxyModeEnv + "=loopback\n" + api.ExternalOriginEnv + "=https://127.0.0.1:9443\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	const origin = "https://127.0.0.1:9444"
	if err := e.SetConnectorTargetOriginWithNetwork(t.Context(), origin); err != nil {
		t.Fatal(err)
	}
	for _, unit := range []string{paths.ControllerServiceUnit, paths.FeedServiceUnit} {
		if _, err := os.Stat(unit + ".d/19-ur-target-network.conf"); err != nil {
			t.Fatal("formal target network missing", err)
		}
	}
	if runner.controllerRestarts != 1 || runner.controllerInvocation == "" {
		t.Fatal("controller network publication lacked a new invocation")
	}
}

type jointReadyTransport struct {
	origin    string
	transport http.RoundTripper
}

func (t jointReadyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != "GET" || r.URL.String() != "http://127.0.0.1:8080/readyz" {
		return nil, fmt.Errorf("unexpected joint readiness request")
	}
	clone := r.Clone(r.Context())
	u, err := url.Parse(t.origin + "/readyz")
	if err != nil {
		return nil, err
	}
	clone.URL = u
	clone.Host = u.Host
	return t.transport.RoundTrip(clone)
}

func runXIManagedJoint(t *testing.T, input urJointConsumerInput, pool *x509.CertPool, client *http.Client) {
	t.Helper()
	restoredOriginal := os.Getenv("XINSIGHT_JOINT_RESTORED_ORIGINAL") == "1"
	if restoredOriginal && (!input.ManagedNormalSetup || input.ManagedMode != "adopt_existing") {
		t.Fatal("restored original selector requires existing normal adopt scenario")
	}
	if input.ManagedFutureProjects && input.ManagedMode != "new_install" {
		t.Fatal("future projects requires new install")
	}
	if input.ManagedMode != "new_install" && input.ManagedMode != "adopt_existing" {
		t.Fatal("unknown managed joint scenario")
	}
	controlURL, err := url.Parse(input.XIControlOrigin)
	if err != nil || controlURL.Scheme != "https" || controlURL.Host == "" || controlURL.Path != "" || controlURL.RawQuery != "" || controlURL.Fragment != "" || controlURL.User != nil {
		t.Fatal("invalid XI control origin")
	}
	host, _, err := net.SplitHostPort(controlURL.Host)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || input.ReceiverAllowedIP != "127.0.0.1/32" || len(input.Profiles) != 8 {
		t.Fatal("managed joint scope incomplete")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Second)
	defer cancel()
	if os.Getenv("XINSIGHT_POSTGRES_DSN") == "" {
		t.Skip("explicit PostgreSQL fixture required")
	}
	createConnectorTestRoles(t, ctx, os.Getenv("XINSIGHT_POSTGRES_DSN"))
	f := newSourceFixture(t)
	// Production opens the workspace store with the root-managed deployment
	// identity. The generic source fixture leaves it unset; setup maintenance
	// filters by this identity and would otherwise select no installations.
	if err := f.workspace.SetConnectorDeploymentID("xid_AAAAAAAAAAAAAAAAAAAAAA"); err != nil {
		t.Fatal("joint workspace deployment identity", err)
	}
	copyCredentials := reviewCopyWorkspaceCredentials(t, ctx, f.dsn)
	copyStore, err := db.OpenWorkspaceStore(ctx, copyCredentials.WorkspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer copyStore.Close()
	copyDB := openTestSQL(t, copyCredentials.WorkspaceDSN)
	defer copyDB.Close()
	var copyRole string
	if err = copyDB.QueryRowContext(ctx, `SELECT current_user`).Scan(&copyRole); err != nil || copyRole != "xinsight_workspace" {
		t.Fatalf("managed C copy role=%q error=%v", copyRole, err)
	}
	// Default registered source/adapter is normal product startup state; no
	// installation, checkpoint, phase or receipt completion is inserted here.
	f.exec(`INSERT INTO source_systems(id,source_key,source_type,display_name,status) VALUES($1,'unirouter','unirouter','Joint UR','active') ON CONFLICT DO NOTHING`, unirouter.DefaultSourceSystemID)
	f.exec(`INSERT INTO adapter_manifests(id,adapter_id,adapter_version,source_type,fidelity_level,payload_behavior,idempotency_strategy) VALUES($1,'synthetic-joint-ur','1.0.0','unirouter','F2','metadata_only_default','source_ref') ON CONFLICT DO NOTHING`, unirouter.DefaultCompletedExecutionAdapterManifestID)
	f.source = unirouter.DefaultSourceSystemID
	f.exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details,allow_coverage,allow_preferences,allow_content,allowed_content_purposes) VALUES($1,$2,true,true,true,true,true,'["human_review"]') ON CONFLICT ON CONSTRAINT workbench_access_grants_scope_unique DO UPDATE SET allow_content=true,allowed_content_purposes='["human_review"]'`, f.subject, f.source)
	root := t.TempDir()
	t.Setenv(hostctl.AllowTestRootEnv, "1")
	t.Setenv(hostctl.TestRootEnv, root)
	t.Setenv("XINSIGHT_RESPONSIBILITY_LOCAL_TEST", "1")
	paths, err := hostctl.PathsFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	e, err := hostctl.NewEngine(paths)
	if err != nil {
		t.Fatal(err)
	}
	runner := &jointManagedRunner{active: map[string]bool{}, enabled: map[string]bool{}, invocation: strings.Repeat("a", 32), controllerUnitPath: paths.ControllerServiceUnit, controllerAllowed: "127.0.0.0/8 ::1/128 10.0.0.0/8"}
	runner.active["xinsight-connector-controller.service"] = true
	e.Runner = runner
	put := func(path string, body []byte, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, mode); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{paths.ServiceUnit, paths.ControllerServiceUnit, paths.FeedServiceUnit, paths.SourceFactsServiceUnit} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", filepath.Base(path)))
		if err != nil {
			t.Fatal(err)
		}
		put(path, raw, 0644)
	}
	put(paths.ConnectorIdentityEnv, []byte(connectorcontrol.DeploymentIDEnv+"=xid_AAAAAAAAAAAAAAAAAAAAAA\n"), 0600)
	put(paths.TrustedTLSProxyEnv, []byte(api.TrustedTLSProxyModeEnv+"=loopback\n"+api.ExternalOriginEnv+"="+input.XIControlOrigin+"\n"), 0644)
	if err = e.SetConnectorTargetOriginWithNetwork(ctx, input.BaseURL); err != nil {
		t.Fatal("joint original host target", err)
	}
	// Retain real paused-authentication HTTPS verification with the test CA;
	// this is the existing host auth-check seam, not a successful fake response.
	e.FeedCredentialAuthCheck = func(ctx context.Context, token string) error {
		reader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: input.BaseURL + connectorcontrol.ReceiverFeedRoute, ServiceKey: token, TLSRootCAs: pool})
		if err != nil {
			return err
		}
		defer reader.CloseContentConnections()
		return reader.VerifyPausedAuthentication(ctx)
	}
	keys := reviewbody.KeyRing{ActiveID: "joint", Keys: map[string][]byte{"joint": bytes.Repeat([]byte{9}, 32)}}
	if input.ManagedNormalSetup {
		t.Setenv("INVOCATION_ID", runner.invocation)
		// Isolated host startup fixture. This is not Linux credential-mount evidence.
		workspaceDSN, err := jointNormalWorkspaceCredential(f.dsn)
		if err != nil {
			t.Fatal("normal workspace credential validation", err)
		}
		put(paths.WorkspaceDSNFile, []byte(workspaceDSN+"\n"), 0600)
		keyPath := filepath.Join(paths.EtcDir, "review-body.key")
		put(keyPath, []byte(strings.Repeat("09", 32)+"\n"), 0600)
		if err = e.SetReviewBodyConfig(hostctl.ReviewBodyConfig{KeyID: keys.ActiveID, KeyFile: keyPath}, false); err != nil {
			t.Fatal(err)
		}
		passwordHash, err := access.DerivePasswordHash([]byte("synthetic joint admin"))
		if err != nil {
			t.Fatal(err)
		}
		accounts := access.AccountConfig{SchemaVersion: access.AccountConfigSchemaV3, Accounts: []access.AccountConfigEntry{{SubjectID: "xs_0123456789abcdef", Username: "joint.admin", Role: access.RolePlatformAdmin, PasswordHash: passwordHash}}}
		raw, err := json.Marshal(accounts)
		if err != nil {
			t.Fatal(err)
		}
		put(paths.AccessAccountsFile, raw, 0600)
		runner.active["xinsight.service"] = true
		runner.enabled["xinsight.service"] = true
	}
	policyManager := reviewpolicycontrol.Manager{Store: f.consumerStore, URStore: f.consumerStore, EnsureCustody: e.EnsureReviewBodyLoaded,
		URClientFactory: func(ctx context.Context) (reviewpolicycontrol.URPolicyClient, urmachine.Identity, error) {
			cfg, origin, enabled, err := e.ReadURMachineConfig()
			if err != nil || !enabled {
				return nil, urmachine.Identity{}, urmachine.ErrUnavailable
			}
			key, err := config.LoadOwnerOnlySecretFile(cfg.PolicyFile)
			if err != nil {
				return nil, urmachine.Identity{}, err
			}
			identity := urmachine.Identity{InstallationID: cfg.InstallationID, SourceInstance: cfg.SourceInstance, OrgID: cfg.OrgID}
			actual, err := urmachine.NewPolicyClient(origin, key, identity, client)
			return actual, identity, err
		}}
	setupManager := &connectorsetup.Manager{Store: f.workspace, Host: e, Policy: policyManager}
	epoch, err := checkpointepoch.NewPostgresInstallationEpochPort(f.admin)
	if err != nil {
		t.Fatal(err)
	}
	hostAdapter, err := connectoractivation.NewHostctlAdapter(e)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := connectoractivation.NewProductReconciler(f.workspace, epoch, hostAdapter, time.Now, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	managed := &connectoractivation.ManagedReconciler{Store: f.workspace, Host: e, Legacy: legacy, Now: time.Now, Random: rand.Reader, TLSRoots: pool}
	controller, err := connectoractivation.NewController(managed)
	if err != nil {
		t.Fatal(err)
	}
	// Unix sockaddr lengths are bounded: keep this fixture socket inside the
	// owned test root but do not reproduce the long managed host prefix.
	socket := filepath.Join(root, "controller.sock")
	unixServer, err := connectoractivation.NewScopedJointUnixServer(f.workspace, controller, jointManagedAckDiagnostic{t: t, actual: managed}, socket, policyManager)
	if err != nil {
		t.Fatal(err)
	}
	if err = unixServer.SetSetupController(jointSetupDiagnostic{t: t, actual: setupManager}); err != nil {
		t.Fatal(err)
	}
	unixCtx, stopUnix := context.WithCancel(ctx)
	done := make(chan error, 1)
	unixFinished := false
	go func() { done <- unixServer.Serve(unixCtx) }()
	defer func() {
		stopUnix()
		if unixFinished {
			return
		}
		if err := <-done; err != nil {
			t.Error("joint Unix server", err)
		}
	}()
	for {
		if _, err = os.Lstat(socket); err == nil {
			break
		}
		select {
		case err = <-done:
			unixFinished = true
			t.Fatal("joint Unix startup", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	unixClient, err := connectorcontrol.NewScopedJointUnixController(socket)
	if err != nil {
		t.Fatal(err)
	}
	invConfig := connectorcontrol.InvitationConfig{DeploymentID: "xid_AAAAAAAAAAAAAAAAAAAAAA", XInsightOrigin: input.XIControlOrigin, UniRouterInstallOrigin: input.BaseURL}
	listener, err := net.Listen("tcp", controlURL.Host)
	if err != nil {
		t.Fatal("joint control listener", err)
	}
	accessService, err := access.NewService(jointReviewProvider{subject: f.subject}, access.DiscardAuditor{}, access.ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := accessService.Login("joint.reviewer", []byte("synthetic joint review"), "")
	if err != nil {
		t.Fatal(err)
	}
	policyClient, err := reviewpolicycontrol.NewScopedJointUnixPolicyClient(socket)
	if err != nil {
		t.Fatal(err)
	}
	mux := api.NewHandlerWithDependencies(api.HandlerDependencies{Store: f.evidence, WorkspaceStore: f.workspace, AgentAccessStore: f.workspace, AccessService: accessService, ReviewBodyKeys: keys, ConnectorConfig: invConfig, ConnectorIssuer: unixClient, SetupController: unixClient, ReviewPolicyController: policyClient, ConnectorController: jointManagedClientDiagnostic{UnixController: unixClient, t: t}})
	server := httptest.NewUnstartedServer(jointACKDiagnosticHandler(t, mux))
	server.Listener.Close()
	server.Listener = listener
	server.StartTLS()
	defer server.Close()
	if input.ManagedNormalSetup {
		e.HTTPClient = &http.Client{Transport: jointReadyTransport{origin: server.URL, transport: server.Client().Transport}}
	}
	if server.URL != input.XIControlOrigin {
		t.Fatal("XI control origin drift")
	}
	call := func(method, path string, value any) map[string]json.RawMessage {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(ctx, method, input.BaseURL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		response, err := client.Do(req)
		if err != nil {
			t.Fatal("joint human transport", err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(io.LimitReader(response.Body, 256<<10))
		if err != nil || response.StatusCode < 200 || response.StatusCode > 299 {
			t.Fatalf("joint human %s %s status=%d", method, path, response.StatusCode)
		}
		var result map[string]json.RawMessage
		if err = json.Unmarshal(body, &result); err != nil {
			t.Fatal("joint human shape", err)
		}
		return result
	}
	projectPath := "/api/v1/data-exchange/xinsight/connections/" + input.ProjectID
	var originalWorker *pullfeed.LiveWorker
	var originalState *pullfeed.PostgresStateStore
	var originalCheckpoint pullfeed.PostgresWorkerState
	var originalConfig sourcefacts.ManagedConfig
	var originalRoot []byte
	var restoredStreams []sourcefacts.Stream
	var restoredContent sourcefacts.ContentCollection
	var retiredStreams []sourcefacts.Stream
	var retiredContent sourcefacts.ContentCollection
	var restoredHistory string
	immutableOriginalHistory := func() string {
		var out string
		if err := f.admin.QueryRowContext(ctx, `SELECT COALESCE(jsonb_agg(row ORDER BY row::text)::text,'[]') FROM(SELECT to_jsonb(t) AS row FROM source_fact_documents t WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 UNION ALL SELECT to_jsonb(t) FROM source_observation_documents t WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 UNION ALL SELECT to_jsonb(t) FROM source_content_manifests t WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4) q`, f.source, input.SourceInstance, input.OrgID, input.ProjectID).Scan(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	if input.ManagedMode == "adopt_existing" {
		originalWorker, originalState = setupJointOriginalFeed(t, ctx, input, pool, e, f, call)
		originalCheckpoint, err = originalState.Load(ctx)
		if err != nil || originalCheckpoint.CheckpointEpoch != 2 || originalCheckpoint.PagesCommitted < 1 {
			t.Fatal("original used epoch2 boundary", err)
		}
		originalConfig = setupJointOriginalSources(t, ctx, input, pool, e, f, call, put)
		originalRoot, err = os.ReadFile(paths.SourceFactsConfig)
		if err != nil {
			t.Fatal(err)
		}
		// This fixture substitutes systemd only; original root/store/HTTP state
		// was qualified above. Linux credential mounts remain a separate check.
		runner.active["xinsight-source-facts.service"] = true
		runner.enabled["xinsight-source-facts.service"] = true
	}
	if restoredOriginal {
		seedJointRestoredOriginalHistory(t, ctx, input, f)
		// Only existing fixture state is changed: no second upstream source,
		// grant, key or configuration is invented for the retired scope.
		for _, profile := range input.Profiles {
			stream, err := f.consumerStore.RegisterSourceFactsStream(ctx, sourcefacts.Stream{ID: testUUID(t), SourceSystemID: f.source, SourceInstance: "retired-joint-source", OrgID: input.OrgID, ProjectID: "2", Profile: profile, ScopeGeneration: "1"})
			if err != nil {
				t.Fatal(err)
			}
			retiredStreams = append(retiredStreams, stream)
		}
		retiredContent, err = f.consumerStore.RegisterContentCollection(ctx, sourcefacts.ContentCollection{ID: testUUID(t), SourceSystemID: f.source, SourceInstance: "retired-joint-source", OrgID: input.OrgID, ProjectID: "2", CredentialID: "retired-fixture", RegistrationHash: sourcefacts.Hash([]byte("retired-fixture")), ContentScopeRevision: "retired-scope", RecoveryEpoch: "1", AllowedPurposes: []string{sourcefacts.ContentPurposeHumanReview}, CoverageFrom: sourcefacts.ContentBoundary{Time: "2026-09-01T00:00:00.000000Z", AfterSequence: "0"}})
		if err != nil {
			t.Fatal(err)
		}
		if err = db.SuspendRestoredSourceFacts(ctx, f.admin); err != nil {
			t.Fatal(err)
		}
		for _, profile := range originalConfig.Profiles {
			live, err := f.consumerStore.LoadSourceFactsStream(ctx, profile.StreamID)
			if err != nil {
				t.Fatal(err)
			}
			restoredStreams = append(restoredStreams, live)
		}
		restoredContent, err = f.consumerStore.LoadContentCollection(ctx, originalConfig.Contents[0].CollectionID)
		if err != nil {
			t.Fatal(err)
		}
		for i, stream := range retiredStreams {
			retiredStreams[i], err = f.consumerStore.LoadSourceFactsStream(ctx, stream.ID)
			if err != nil {
				t.Fatal(err)
			}
		}
		retiredContent, err = f.consumerStore.LoadContentCollection(ctx, retiredContent.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, stream := range append(append([]sourcefacts.Stream{}, restoredStreams...), retiredStreams...) {
			if stream.Status != "paused" || !stream.RequiresAttention || stream.LastError != "restore_requires_authorization" {
				t.Fatal("incomplete restored stream prestate")
			}
		}
		for _, collection := range []sourcefacts.ContentCollection{restoredContent, retiredContent} {
			if collection.Status != "paused" || !collection.RequiresAttention || collection.LastError != "restore_requires_authorization" {
				t.Fatal("incomplete restored content prestate")
			}
		}
		restoredHistory = immutableOriginalHistory()
		if restoredHistory == "[]" {
			t.Fatal("restored original scope must contain actual nonempty protected history")
		}
		runner.active["xinsight-source-facts.service"] = false
		runner.enabled["xinsight-source-facts.service"] = false
		// Existing systemd substitute now projects key readiness at the actual
		// consumer-start call, rather than asserting a worker was prestarted.
		runner.onSourceStart = func() error {
			loaded, err := reviewbody.LoadKeyRingFromFile(keys.ActiveID, filepath.Join(paths.EtcDir, "review-body.key"))
			if err != nil {
				return err
			}
			proof, err := loaded.LoadedProof("xinsight-source-facts.service", runner.invocation, os.Getpid())
			if err != nil {
				return err
			}
			raw, err := json.Marshal(proof)
			if err != nil {
				return err
			}
			put(filepath.Join(root, reviewbody.WorkerLoadedProofPath), raw, 0600)
			return nil
		}
	}
	var inv connectorcontrol.InvitationPackage
	var normalPreview map[string]json.RawMessage
	var normalXICall func(string, string, any) map[string]json.RawMessage
	if input.ManagedNormalSetup {
		adminSession, auth, err := accessService.Login("joint.admin", []byte("synthetic joint admin"), "")
		if err != nil {
			t.Fatal(err)
		}
		normalXICall = func(method, path string, value any) map[string]json.RawMessage {
			t.Helper()
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			req, err := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			req.AddCookie(&http.Cookie{Name: access.SessionCookieName, Value: adminSession})
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", server.URL)
			req.Header.Set("X-CSRF-Token", accessService.CSRFToken(auth))
			response, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
			if err != nil || response.StatusCode < 200 || response.StatusCode > 299 {
				t.Fatalf("normal XI %s status=%d", path, response.StatusCode)
			}
			var out map[string]json.RawMessage
			if json.Unmarshal(body, &out) != nil {
				t.Fatal("normal XI shape")
			}
			return out
		}
		normalXICall("GET", "/api/v1/connector-setups/preview", nil)
		created := normalXICall("POST", "/api/v1/connector-setups", connectorcontrol.SetupConsentPut{SchemaVersion: connectorcontrol.SetupConsentSchema, RequestID: testUUID(t), Custody: &connectorcontrol.SetupCustody{Mode: "full_content", RetentionSeconds: 90 * 86400, ProjectQuotaBytes: 5 << 30, MaxItemBytes: 32 << 20}})
		if json.Unmarshal(created["invitation"], &inv) != nil {
			t.Fatal("normal original invitation")
		}
		fragmentURL, err := url.Parse(inv.InstallURL)
		if err != nil {
			t.Fatal(err)
		}
		fragment, err := url.ParseQuery(fragmentURL.Fragment)
		if err != nil || fragment.Get("xinsight_origin") != server.URL {
			t.Fatal("normal static fragment origin")
		}
		normalPreview = call("POST", "/api/v2/organizations/"+input.OrgID+"/xinsight/setup-previews", map[string]string{"project_id": input.ProjectID, "invitation_token": fragment.Get("invitation")})
		// Match the normal UI default even when this run does not create a
		// second project. Future discovery is a separate acceptance selector.
		coverage := "all_current_future"
		var previewHash string
		var consent connectorcontrol.SetupConsentView
		if json.Unmarshal(normalPreview["preview_sha256"], &previewHash) != nil || json.Unmarshal(normalPreview["xi_consent"], &consent) != nil {
			t.Fatal("normal UR preview shape")
		}
		var policyView urmachine.PolicyView
		var enterprise struct {
			PolicyRef string          `json:"policy_ref"`
			Explicit  bool            `json:"explicit"`
			Desired   json.RawMessage `json:"desired"`
			Effective json.RawMessage `json:"effective"`
		}
		if json.Unmarshal(normalPreview["ur_policy"], &policyView) != nil ||
			json.Unmarshal(policyView.Enterprise, &enterprise) != nil {
			t.Fatal("normal UR policy preview shape")
		}
		var urPolicy any
		if len(enterprise.Desired) == 0 || bytes.Equal(enterprise.Desired, []byte("null")) {
			// With no enterprise policy, the real human final consent must
			// explicitly establish it using only the observed CAS/capabilities.
			// Existing project and user overrides are never written here.
			if enterprise.Explicit || len(enterprise.Effective) != 0 && !bytes.Equal(enterprise.Effective, []byte("null")) ||
				enterprise.PolicyRef == "" || policyView.VersionVector == "" || policyView.PolicyCapabilities == nil ||
				len(policyView.PolicyCapabilities.SupportedProtocols) == 0 || len(policyView.PolicyCapabilities.ObservationPoints) == 0 {
				t.Fatal("normal absent enterprise policy has incomplete preview authority")
			}
			urPolicy = map[string]any{"scope": "enterprise", "put": map[string]any{
				"schema_version": "unirouter.payload-policy.v2", "expected_policy_ref": enterprise.PolicyRef,
				"expected_version": "0", "expected_vector": `{"enterprise":0}`,
				"execution_storage_mode": "temporary_allowed", "project_quota_bytes": int64(5 << 30),
				"desired": map[string]any{"mode": "full_content", "retention_seconds": 30 * 86400,
					"quota_bytes": int64(5 << 30), "max_saved_bytes": 32 << 20,
					"supported_protocols": policyView.PolicyCapabilities.SupportedProtocols,
					"observation_points":  policyView.PolicyCapabilities.ObservationPoints,
					"allowed_purposes":    []string{"human_review"}, "on_limit": "record_capture_gap"},
			}}
		}
		call("POST", "/api/v2/organizations/"+input.OrgID+"/xinsight/setups", map[string]any{"schema_version": "unirouter.xinsight.setup-consent.v1", "request_id": testUUID(t), "project_id": input.ProjectID, "invitation": inv, "expected_preview_sha256": previewHash, "expected_xi_consent_sha256": consent.ConsentSHA256, "receiver_mode": input.ManagedMode, "existing_binding_sha256": normalPreview["existing_binding_sha256"], "coverage": coverage, "excluded_project_ids": []string{}, "profiles": input.Profiles, "include_content": true, "policy_access": []map[string]any{{"target_kind": "enterprise_default", "coverage": "exact", "read": true, "write": true}, {"target_kind": "project_policy", "coverage": "all_current_future", "read": true, "write": true}}, "ur_policy": urPolicy})
	} else {
		inv, _, err = unixClient.CreateInvitation(ctx, connectorcontrol.CreateInvitationInput{ActorSubject: f.subject, Config: invConfig, Now: time.Now().UTC()})
		if err != nil {
			t.Fatal("joint root invitation", err)
		}
		call("POST", projectPath+"/install", map[string]any{"invitation": inv, "receiver_mode": input.ManagedMode})
	}
	await := func(label string, check func() (bool, error)) {
		t.Helper()
		for {
			ok, err := check()
			if err != nil {
				t.Fatalf("joint %s: %v", label, err)
			}
			if ok {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatalf("joint %s: %v", label, ctx.Err())
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	await("original v2/descriptor/pairs", func() (bool, error) {
		if input.ManagedNormalSetup {
			if err := setupManager.Tick(ctx); err != nil {
				if errors.Is(err, hostctl.ErrOperationBusy) {
					return false, nil
				}
				if !jointSetupSerializationPending(err) {
					return false, fmt.Errorf("actual setup maintenance: %w", err)
				}
				// Match the formal supervisor: report this rolled-back step, then
				// continue managed maintenance and the original next bounded pass.
				t.Log("joint actual setup maintenance pending SQLSTATE=40001; transaction rolled back, continuing original maintenance")
			}
		}
		if err := managed.Tick(ctx); err != nil {
			// Production maintenance reports pending and takes its next pass.
			// Match typed lock contention and the newly scheduled controller
			// restart; arbitrary product failures must still fail this fixture.
			if errors.Is(err, hostctl.ErrOperationBusy) || errors.Is(err, hostctl.ErrURMachineRuntimePending) {
				return false, nil
			}
			if current, readErr := f.workspace.InspectManagedConnector(ctx, inv.InstallationID); readErr == nil {
				t.Logf("joint maintenance boundary status=%s observed=%s version=%d", current.Installation.Status, current.Installation.ObservedState, current.Installation.Version)
				for _, phase := range current.Phases {
					t.Logf("joint maintenance phase=%s status=%s", phase.Operation.Phase, phase.Operation.Status)
				}
			} else {
				t.Logf("joint strict boundary unavailable: %v", readErr)
				var status, observed string
				var version int64
				if err := f.admin.QueryRowContext(ctx, `SELECT status,observed_state,version FROM connector_installations WHERE id=$1`, inv.InstallationID).Scan(&status, &observed, &version); err == nil {
					t.Logf("joint raw installation status=%s observed=%s version=%d", status, observed, version)
				}
				rows, err := f.admin.QueryContext(ctx, `SELECT phase,status,EXTRACT(epoch FROM (updated_at-completed_at)) FROM connector_control_operations WHERE installation_id=$1 ORDER BY created_at`, inv.InstallationID)
				if err == nil {
					defer rows.Close()
					for rows.Next() {
						var phase, state string
						var delta sql.NullFloat64
						if rows.Scan(&phase, &state, &delta) == nil {
							t.Logf("joint raw phase=%s status=%s terminal_update_delta_seconds=%g", phase, state, delta.Float64)
						}
					}
				}
			}
			return false, err
		}
		b, err := f.workspace.InspectManagedConnector(ctx, inv.InstallationID)
		if err != nil {
			return false, err
		}
		if originalWorker != nil && b.LocalReceipt != nil && b.Installation.Status != "connected" {
			// Confirmation needs a committed real feed read AFTER durable ACK.
			if _, err = originalWorker.PollOnce(ctx); err != nil {
				return false, err
			}
		}
		policy, enroll := b.Phase(connectorcontrol.PhaseDeliverMachineCandidate, connectorcontrol.PolicyMachinePurpose), b.Phase(connectorcontrol.PhaseDeliverMachineCandidate, connectorcontrol.EnrollmentMachinePurpose)
		return b.Installation.Status == "connected" && (b.Descriptor != nil || b.Bootstrap != nil && b.LocalReceipt != nil && b.FeedRead != nil) && policy != nil && policy.Operation.Status == "completed" && enroll != nil && enroll.Operation.Status == "completed", nil
	})
	boundary, err := f.workspace.InspectManagedConnector(ctx, inv.InstallationID)
	if err != nil {
		t.Fatal(err)
	}
	if input.ManagedMode == "new_install" && (boundary.Bootstrap != nil || boundary.LocalReceipt != nil) {
		t.Fatal("new install fabricated adoption")
	}
	register := boundary.Phase(connectorcontrol.ControlPhaseRegisterReceiver, "")
	if input.ManagedMode == "new_install" && (register == nil || register.Operation.Request == nil || register.Operation.Request.NetworkBinding == nil || *register.Operation.Request.NetworkBinding != input.ReceiverNetworkBinding) {
		t.Fatal("actual original v2 network binding differs from UR fixture")
	}
	orgPath := "/api/v2/organizations/" + input.OrgID
	var enrollment string
	if input.ManagedNormalSetup {
		path, err := e.MachineCredentialPath(inv.InstallationID, connectorcontrol.EnrollmentMachinePurpose)
		if err != nil {
			t.Fatal(err)
		}
		key, err := config.LoadOwnerOnlySecretFile(path)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := urmachine.NewEnrollmentClient(input.BaseURL, key, urmachine.Identity{InstallationID: inv.InstallationID, SourceInstance: input.SourceInstance, OrgID: input.OrgID}, client)
		if err != nil {
			t.Fatal(err)
		}
		await("normal enrollment created by approved setup", func() (bool, error) {
			rows, err := actual.Projects(ctx, "")
			if errors.Is(err, urmachine.ErrUnauthorized) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			for _, project := range rows.Projects {
				if project.ProjectID == input.ProjectID {
					for _, row := range project.Enrollments {
						if row.FactsStatus != "revoked" {
							enrollment = row.EnrollmentID
							return true, nil
						}
					}
				}
			}
			return false, nil
		})
	} else {
		for _, purpose := range []string{connectorcontrol.PolicyMachinePurpose, connectorcontrol.EnrollmentMachinePurpose} {
			phase := boundary.Phase(connectorcontrol.PhaseDeliverMachineCandidate, purpose)
			registration := phase.Operation.RequestV3.Registration
			response := call("POST", orgPath+"/xinsight-policy-credentials", registration)
			var key string
			if json.Unmarshal(response["key_id"], &key) != nil || key == "" {
				t.Fatal("joint machine registration readback")
			}
			call("POST", orgPath+"/xinsight-policy-credentials/"+key+"/activate", map[string]any{})
		}
		policyGrant := map[string]any{"installation_id": inv.InstallationID, "target_kind": "project_policy", "coverage": "exact", "project_id": input.ProjectID, "read": true, "write": true}
		sourceGrant := map[string]any{"installation_id": inv.InstallationID, "coverage": "exact", "project_id": input.ProjectID, "profiles": input.Profiles, "include_content": true}
		if input.ManagedFutureProjects {
			policyGrant["coverage"], sourceGrant["coverage"] = "all_current_future", "all_current_future"
			delete(policyGrant, "project_id")
			delete(sourceGrant, "project_id")
		}
		call("POST", orgPath+"/xinsight-policy-delegations", policyGrant)
		response := call("POST", orgPath+"/xinsight-source-enrollments", sourceGrant)
		if json.Unmarshal(response["enrollment_id"], &enrollment) != nil || enrollment == "" {
			t.Fatal("joint enrollment readback")
		}
	}
	// The normal setup controller has already bound the accepted original keys.
	// Manual bind remains only the original legacy acceptance branch; normal
	// readback below qualifies exact original registrations/root bytes/status.
	if input.ManagedMode == "adopt_existing" && !input.ManagedNormalSetup {
		bindPath := orgPath + "/xinsight-source-enrollments/" + enrollment + "/projects/" + input.ProjectID + "/bind-existing"
		for _, binding := range originalConfig.Bindings {
			kind := "facts"
			if binding.Scope == "content" {
				kind = "content"
			}
			candidates := call("GET", bindPath, nil)
			var rows []map[string]json.RawMessage
			if json.Unmarshal(candidates["data"], &rows) != nil {
				t.Fatal("original bind candidate list")
			}
			var candidate map[string]json.RawMessage
			for _, row := range rows {
				var actualKind string
				if json.Unmarshal(row["kind"], &actualKind) == nil && actualKind == kind {
					candidate = row
				}
			}
			var hash string
			if json.Unmarshal(candidate["state_sha256"], &hash) != nil || hash == "" {
				t.Fatal("original bind candidate")
			}
			var operation [16]byte
			if _, err = rand.Read(operation[:]); err != nil {
				t.Fatal(err)
			}
			call("POST", bindPath, map[string]any{"schema_version": "unirouter.xinsight.bind-existing.v1", "kind": kind, "credential_id": binding.CredentialID, "expected_state_sha256": hash, "operation_id": hex.EncodeToString(operation[:])})
		}
	}
	machineClient := func(purpose string) string {
		t.Helper()
		path, err := e.MachineCredentialPath(inv.InstallationID, purpose)
		if err != nil {
			t.Fatal(err)
		}
		key, err := config.LoadOwnerOnlySecretFile(path)
		if err != nil {
			t.Fatal("joint root machine custody", err)
		}
		return key
	}
	identity := urmachine.Identity{InstallationID: inv.InstallationID, SourceInstance: input.SourceInstance, OrgID: input.OrgID}
	// This opt-in isolated legacy lifecycle fixture intentionally tests ongoing
	// collection after setup. Declare that intent for its exact synthetic pair;
	// the separate sync tests cover the new production manual/finite defaults.
	f.exec(`UPDATE connector_sync_controls SET auto_sync=true WHERE installation_id=$1`, inv.InstallationID)
	projects := &connectoractivation.ManagedProjects{Store: f.workspace, Sources: f.consumerStore, Host: e, TLSRoots: pool, EnrollmentClient: func(context.Context, connectorcontrol.ManagedBoundary) (*urmachine.EnrollmentClient, error) {
		return urmachine.NewEnrollmentClient(input.BaseURL, machineClient(connectorcontrol.EnrollmentMachinePurpose), identity, client)
	}, PolicyClient: func(context.Context, connectorcontrol.ManagedBoundary) (*urmachine.Client, error) {
		return urmachine.NewPolicyClient(input.BaseURL, machineClient(connectorcontrol.PolicyMachinePurpose), identity, client)
	}}
	await("production discovery and source publish", func() (bool, error) {
		if restoredOriginal {
			if err := setupManager.Tick(ctx); err != nil && !jointSetupSerializationPending(err) && !errors.Is(err, hostctl.ErrOperationBusy) {
				return false, err
			}
			if err := managed.Tick(ctx); err != nil && !errors.Is(err, hostctl.ErrOperationBusy) && !errors.Is(err, hostctl.ErrURMachineRuntimePending) {
				return false, err
			}
		}
		if err := projects.Refresh(ctx); err != nil {
			if restoredOriginal && errors.Is(err, connectoractivation.ErrOriginalReadinessPending) {
				return false, nil
			}
			if errors.Is(err, hostctl.ErrOperationBusy) {
				return false, nil
			}
			// Bounded non-secret readback must not depend on the strict refresh
			// succeeding. Preserve the actual persisted enrollment state.
			readCtx, cancelRead := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer cancelRead()
			if current, readErr := f.workspace.ManagedEnrollment(readCtx, inv.InstallationID, enrollment, input.ProjectID); readErr == nil {
				t.Logf("joint actual managed enrollment status=%s revision=%s has_config_digest=%t has_content_collection=%t legacy_copy_pending=%t", current.Status, current.Revision, current.ConfigurationSHA256 != "", current.ContentCollectionID != "", current.LegacyCCopyPending)
			}
			return false, err
		}
		r, err := f.workspace.ManagedEnrollment(ctx, inv.InstallationID, enrollment, input.ProjectID)
		if errors.Is(err, connectorcontrol.ErrNotFound) {
			return false, nil
		}
		if err == nil && input.ManagedMode == "new_install" && r.Status == "requires_migration" {
			t.Logf("joint actual unexpected new-install enrollment status=%s revision=%s has_config_digest=%t has_content_collection=%t", r.Status, r.Revision, r.ConfigurationSHA256 != "", r.ContentCollectionID != "")
			return false, fmt.Errorf("new-install discovery unexpectedly requires migration: %w", connectorcontrol.ErrConflict)
		}
		return r.Status == "active" || input.ManagedMode == "adopt_existing" && r.LegacyCCopyPending && r.Status == "requires_migration", err
	})
	if restoredOriginal {
		for _, before := range restoredStreams {
			after, err := f.consumerStore.LoadSourceFactsStream(ctx, before.ID)
			if err != nil || !sourcefacts.SameRegistration(before, after) || after.Status != "active" || after.RequiresAttention || after.Cursor != before.Cursor || after.HighWatermark != before.HighWatermark {
				t.Fatal("normal maintenance lost original stream checkpoint", err)
			}
		}
		after, err := f.consumerStore.LoadContentCollection(ctx, restoredContent.ID)
		if err != nil || !sourcefacts.SameContentCollection(after, restoredContent) || after.Status != "active" || after.RequiresAttention || after.Cursor != restoredContent.Cursor || after.LastSequence != restoredContent.LastSequence || after.HighWatermark != restoredContent.HighWatermark || after.HighWatermarkSequence != restoredContent.HighWatermarkSequence {
			t.Fatal("normal maintenance lost original C checkpoint", err)
		}
		for _, before := range retiredStreams {
			after, err := f.consumerStore.LoadSourceFactsStream(ctx, before.ID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("normal maintenance changed retired source", err)
			}
		}
		retiredAfter, err := f.consumerStore.LoadContentCollection(ctx, retiredContent.ID)
		if err != nil || !reflect.DeepEqual(retiredContent, retiredAfter) {
			t.Fatal("normal maintenance changed retired C", err)
		}
		if immutableOriginalHistory() != restoredHistory {
			t.Fatal("normal maintenance rewrote immutable original history")
		}
		setup, err := f.workspace.ReadConnectorSetup(ctx, inv.InstallationID)
		if err != nil || setup.AppliedAt == nil || setup.ErrorCode != "" {
			t.Fatal("setup/project maintenance still mutually pending", err)
		}
		t.Log("restored_original_maintenance=pass scope_filter=pass checkpoints_history=preserved retired_source=paused")
	}
	// Host PublishManagedProjects already checks fixture custody and exact
	// generated path. Parse the published bytes with the production closed
	// decoder; root UID/systemd importer mount is a separate Linux qualifier.
	configPath := filepath.Join(sourcefacts.ManagedPartitionsDirectory(paths.SourceFactsConfig), inv.InstallationID+"-"+enrollment+"-project-"+input.ProjectID+".json")
	if input.ManagedMode == "adopt_existing" {
		configPath = paths.SourceFactsConfig
	}
	rawConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal("joint published partition", err)
	}
	cfg, err := sourcefacts.ParseManagedConfig(rawConfig)
	if err != nil {
		t.Fatal("joint published contract", err)
	}
	if len(cfg.Profiles) != 8 || len(cfg.Contents) != 1 {
		t.Fatal("joint original eight/C contract lost")
	}
	if input.ManagedNormalSetup {
		configured := map[string]bool{}
		for _, profile := range cfg.Profiles {
			expectedID := connectorcontrol.ManagedCollectionID(inv.InstallationID, enrollment, input.ProjectID, "profile:"+profile.Profile)
			if input.ManagedMode == "adopt_existing" {
				expectedID = ""
				for _, original := range originalConfig.Profiles {
					if original.Profile == profile.Profile && sourcefacts.SameRegistration(original.Registration(), profile.Registration()) {
						expectedID = original.StreamID
						break
					}
				}
			}
			stream, err := f.consumerStore.LoadSourceFactsStream(ctx, profile.StreamID)
			if err != nil || configured[profile.Profile] || !slices.Contains(input.Profiles, profile.Profile) ||
				!sourcefacts.SameRegistration(stream, profile.Registration()) || stream.Status != "active" ||
				stream.ID != expectedID {
				t.Fatal("normal eight-profile registration/configuration mismatch", profile.Profile)
			}
			configured[profile.Profile] = true
		}
	}
	runner.mu.Lock()
	restarts := runner.sourceRestarts
	runner.mu.Unlock()
	expectedRestarts := 1
	if input.ManagedMode == "adopt_existing" {
		expectedRestarts = 0
		if restoredOriginal {
			expectedRestarts = 1
		}
	}
	if restarts != expectedRestarts {
		t.Fatalf("joint source worker refresh count=%d", restarts)
	}
	now := time.Now().UTC()
	if !input.ManagedNormalSetup {
		revision, err := f.consumerStore.RequestReviewCaptureDefault(ctx, db.ReviewCaptureSettings{Mode: "full_content", RetentionSeconds: 3600, ProjectQuotaBytes: 1 << 20, MaxItemBytes: 1 << 20}, 0, f.subject, now)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.consumerStore.ConfirmReviewCaptureDefault(ctx, revision, f.subject, now); err != nil {
			t.Fatal(err)
		}
	}
	var originalXIProject string
	if err = f.admin.QueryRowContext(ctx, `SELECT xi_project_id::text FROM review_capture_project_bindings
 WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4`,
		f.source, input.SourceInstance, input.OrgID, input.ProjectID).Scan(&originalXIProject); err != nil || originalXIProject == "" {
		t.Fatal("managed C project was not bound before custody worker", err)
	}
	if repeated, ensureErr := f.consumerStore.EnsureManagedReviewCaptureProject(ctx, inv.InstallationID,
		cfg.Contents[0].CollectionID, f.source, input.SourceInstance, input.OrgID, input.ProjectID, now); ensureErr != nil || repeated != originalXIProject {
		t.Fatal("managed C binding was not idempotent", ensureErr)
	}
	var bindAudits int
	if err = f.admin.QueryRowContext(ctx, `SELECT count(*) FROM access_audit_records WHERE actor_kind='system'
 AND actor_ref=$1 AND action='review.policy.project.auto_bind' AND resource_ref=$2`,
		inv.InstallationID, originalXIProject).Scan(&bindAudits); err != nil || bindAudits != 1 {
		t.Fatal("managed C owner binding audit missing or duplicated", err)
	}
	if _, err = f.consumerStore.EnsureManagedReviewCaptureProject(ctx, inv.InstallationID,
		cfg.Contents[0].CollectionID, f.source, input.SourceInstance, input.OrgID, input.ForeignProjectID, now); !errors.Is(err, reviewbody.ErrScope) {
		t.Fatalf("foreign project reused the managed C collection: %v", err)
	}
	var foreignBindings int
	if err = f.admin.QueryRowContext(ctx, `SELECT count(*) FROM review_capture_project_bindings
 WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4`,
		f.source, input.SourceInstance, input.OrgID, input.ForeignProjectID).Scan(&foreignBindings); err != nil || foreignBindings != 0 {
		t.Fatal("foreign project acquired XI binding", err)
	}
	worker := jointManagedSourceWorker(t, f, copyStore, cfg, pool, keys)
	// Read the actual durable custody stages, including item failures that do
	// not fail an independently committed manifest poll. Never log source bytes,
	// credentials, request identifiers, content refs or hashes.
	lastCustodyDiagnostic := ""
	custodyDiagnostic := func(stage string) {
		if !input.ManagedNormalSetup {
			return
		}
		readCtx, cancelRead := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancelRead()
		state, readErr := jointManagedCustodyDiagnostic(readCtx, f.admin, inv.InstallationID, cfg.Contents[0].CollectionID, f.source, input)
		if readErr != nil {
			t.Logf("joint actual custody stage=%s diagnostic_error_type=%T", stage, readErr)
			return
		}
		if state != lastCustodyDiagnostic || stage == "final" {
			t.Logf("joint actual custody stage=%s state=%s", stage, state)
			lastCustodyDiagnostic = state
		}
	}
	custodyDiagnostic("before_worker")
	defer custodyDiagnostic("final")
	var initialPollMu sync.Mutex
	initialPolls := map[string]bool{}
	if input.ManagedNormalSetup {
		labels := map[string]string{cfg.Contents[0].CollectionID: "content"}
		for _, profile := range cfg.Profiles {
			labels[profile.StreamID] = profile.Profile
		}
		firstOutcome := map[string]bool{}
		worker.Observe = func(id string, err error) {
			initialPollMu.Lock()
			first := !firstOutcome[id]
			firstOutcome[id] = true
			firstSuccess := err == nil && !initialPolls[id]
			if err == nil {
				initialPolls[id] = true
			}
			initialPollMu.Unlock()
			if first || firstSuccess {
				category := "success"
				if err != nil {
					category = "source_error"
					for _, known := range []struct {
						err  error
						code string
					}{
						{sourcefacts.ErrInvalid, "contract_invalid"}, {sourcefacts.ErrConflict, "conflict"},
						{sourcefacts.ErrStreamChanged, "stream_changed"}, {sourcefacts.ErrScopeChanged, "scope_changed"},
						{sourcefacts.ErrUnavailable, "unavailable"}, {context.Canceled, "canceled"}, {context.DeadlineExceeded, "deadline"},
					} {
						if errors.Is(err, known.err) {
							category = known.code
							break
						}
					}
				}
				t.Logf("normal actual worker profile=%s outcome=%s error_type=%T", labels[id], category, err)
			}
		}
	}
	if input.ManagedNormalSetup {
		proof, err := keys.LoadedProof("xinsight-source-facts.service", runner.invocation, os.Getpid())
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(proof)
		if err != nil {
			t.Fatal(err)
		}
		put(filepath.Join(root, reviewbody.WorkerLoadedProofPath), raw, 0600)
		if err = e.ReviewBodyLoaded(ctx); err != nil {
			t.Fatal("normal loaded reader/source", err)
		}
	}
	maintainSetup := func() error {
		if input.ManagedNormalSetup {
			if err := setupManager.Tick(ctx); err != nil {
				if !errors.Is(err, hostctl.ErrOperationBusy) && !jointSetupSerializationPending(err) {
					return fmt.Errorf("actual setup maintenance: %w", err)
				}
			}
		}
		if input.ManagedMode != "adopt_existing" {
			return nil
		}
		// This is the existing owner maintenance boundary. The restricted copy
		// worker never finalizes a legacy C transition itself.
		_, err := f.workspace.SweepLegacyCCustodyTransitions(ctx, time.Now().UTC(), 100)
		return err
	}
	if input.ManagedNormalSetup && input.ManagedMode == "adopt_existing" {
		// The formal supervisor continues setup maintenance after connection.
		// The original source worker's retained-key proof is now available, so
		// let that actual path confirm the consent before its first C copy.
		await("actual adopted setup effective custody", func() (bool, error) {
			if err := maintainSetup(); err != nil {
				return false, err
			}
			custodyDiagnostic("await_effective_custody")
			record, err := f.workspace.ReadConnectorSetup(ctx, inv.InstallationID)
			if err != nil {
				return false, err
			}
			current, err := f.workspace.ReadReviewCaptureDefault(ctx)
			if err != nil {
				return false, err
			}
			if record.AppliedAt == nil || record.ErrorCode != "" || record.PolicyRevision < 1 ||
				current.DesiredRevision != record.PolicyRevision || current.EffectiveRevision != record.PolicyRevision ||
				current.Effective == nil || current.EffectiveAt == nil || current.EffectiveAt.After(time.Now().UTC()) {
				return false, nil
			}
			want := record.Consent.Custody
			if current.Effective.Mode != want.Mode || current.Effective.RetentionSeconds != want.RetentionSeconds ||
				current.Effective.ProjectQuotaBytes != want.ProjectQuotaBytes || current.Effective.MaxItemBytes != want.MaxItemBytes {
				return false, errors.New("actual adopted custody differs from original consent")
			}
			return true, e.ReviewBodyLoaded(ctx)
		})
	}

	workCtx, stopWork := context.WithCancel(ctx)
	workDone := make(chan error, 1)
	workExited := false
	go func() { workDone <- worker.Run(workCtx) }()
	defer func() {
		stopWork()
		if workExited {
			return
		}
		err := <-workDone
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Error("joint worker", err)
		}
	}()
	var contentRef string
	await("production worker encrypted C", func() (bool, error) {
		if err := maintainSetup(); err != nil {
			return false, err
		}
		custodyDiagnostic("await_body")
		select {
		case err := <-workDone:
			workExited = true
			if err == nil {
				err = errors.New("worker exited before custody")
			}
			return false, fmt.Errorf("actual source worker exited: %w", err)
		default:
		}
		err := f.admin.QueryRowContext(ctx, `SELECT content_ref FROM review_body_operations WHERE source_system_id=$1 AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND digest=$5 AND state='accepted' LIMIT 1`, f.source, input.SourceInstance, input.OrgID, input.ProjectID, "sha256:"+input.ExpectedContent.SHA256).Scan(&contentRef)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return contentRef != "", err
	})
	read, err := f.workspace.ReadReviewBody(ctx, f.subject, contentRef, keys, time.Now().UTC())
	if err != nil {
		t.Fatal("joint independent body read", err)
	}
	digest := sha256.Sum256(read.Bytes)
	if hex.EncodeToString(digest[:]) != input.ExpectedContent.SHA256 {
		t.Fatal("joint body digest differs")
	}
	// Exercise the server's authenticated own-body route through real TLS.
	readHTTP := func(path, revision string) *http.Response {
		t.Helper()
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+path, nil)
		if e != nil {
			t.Fatal(e)
		}
		req.AddCookie(&http.Cookie{Name: access.SessionCookieName, Value: session})
		if revision != "" {
			req.Header.Set("X-XInsight-Scope-Revision", revision)
		}
		response, e := client.Do(req)
		if e != nil {
			t.Fatal("joint XI body transport", e)
		}
		return response
	}
	accessResponse := readHTTP("/api/v1/access/session", "")
	accessRaw, readErr := io.ReadAll(io.LimitReader(accessResponse.Body, 64<<10))
	accessResponse.Body.Close()
	if readErr != nil || accessResponse.StatusCode != 200 {
		t.Fatal("joint XI session read")
	}
	var sessionScope struct {
		Revision string `json:"scope_revision"`
	}
	if json.Unmarshal(accessRaw, &sessionScope) != nil || sessionScope.Revision == "" {
		t.Fatal("joint XI body scope")
	}
	bodyResponse := readHTTP("/api/v1/work-activity/contents/"+url.PathEscape(contentRef), sessionScope.Revision)
	bodyRaw, readErr := io.ReadAll(io.LimitReader(bodyResponse.Body, 1<<20))
	bodyResponse.Body.Close()
	if readErr != nil || bodyResponse.StatusCode != 200 || !bytes.Equal(bodyRaw, read.Bytes) {
		t.Fatalf("joint XI own-body HTTP status=%d", bodyResponse.StatusCode)
	}
	deadline, err := time.Parse(sourcefacts.TimeFormat, bodyResponse.Header.Get("X-XInsight-Content-Expires-At"))
	if err != nil || !deadline.Equal(read.ExpiresAt) {
		t.Fatal("XI own-body independent expiry header", err)
	}
	if input.ManagedNormalSetup {
		// This fixture calls CaptureAuthenticated with observations enabled,
		// then captures C. It emits authenticated context/identity and runtime
		// request_started; no terminal cost or other observation producer runs.
		expectedReceived := []string{sourcefacts.ContextProfile, sourcefacts.IdentityProfile, sourcefacts.RuntimeProfile}
		lastReceivingProgress := ""
		await("normal actual authenticated documents and all eight worker polls", func() (bool, error) {
			select {
			case err := <-workDone:
				workExited = true
				if err == nil {
					err = errors.New("worker exited before receiving readback")
				}
				return false, err
			default:
			}
			var contextCount, identityCount, runtimeCount, unexpectedCount int
			err := f.admin.QueryRowContext(ctx, `SELECT
			 (SELECT count(*) FROM source_fact_documents WHERE source_system_id=$1 AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND request_id=$5 AND profile=$6),
			 (SELECT count(*) FROM source_fact_documents WHERE source_system_id=$1 AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND request_id=$5 AND profile=$7),
			 (SELECT count(*) FROM source_observation_documents WHERE source_system_id=$1 AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND request_id=$5 AND profile=$8 AND observation_kind='request_started'),
			 (SELECT count(*) FROM source_fact_documents WHERE source_system_id=$1 AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND request_id=$5 AND profile=$9)
			 +(SELECT count(*) FROM source_observation_documents WHERE source_system_id=$1 AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND request_id=$5 AND profile<>$8)`, f.source, input.SourceInstance, input.OrgID, input.ProjectID, input.ExpectedContent.RequestID, sourcefacts.ContextProfile, sourcefacts.IdentityProfile, sourcefacts.RuntimeProfile, sourcefacts.CostProfile).Scan(&contextCount, &identityCount, &runtimeCount, &unexpectedCount)
			if err != nil {
				return false, err
			}
			if unexpectedCount != 0 {
				return false, errors.New("fixture unexpectedly produced unrequested terminal/observation events")
			}
			initialPollMu.Lock()
			contentPolled := initialPolls[cfg.Contents[0].CollectionID]
			missing := []string{}
			for _, profile := range cfg.Profiles {
				if !initialPolls[profile.StreamID] {
					missing = append(missing, profile.Profile)
				}
			}
			initialPollMu.Unlock()
			progress := fmt.Sprintf("context=%d identity=%d runtime_started=%d unexpected=%d content_poll=%t missing_successful_profiles=%v", contextCount, identityCount, runtimeCount, unexpectedCount, contentPolled, missing)
			if progress != lastReceivingProgress {
				t.Log("normal actual persisted/poll progress", progress)
				lastReceivingProgress = progress
			}
			return contextCount > 0 && identityCount > 0 && runtimeCount > 0 && contentPolled && len(missing) == 0, nil
		})
		t.Log("normal actual eight-profile polls succeeded; persisted context/identity/runtime request_started and C; cost/attempt/capability/policy/destination not produced")
		readXIStatus := func() connectorcontrol.SetupStatus {
			raw, err := json.Marshal(normalXICall("GET", "/api/v1/connector-setups/"+inv.InstallationID, nil))
			var status connectorcontrol.SetupStatus
			if err != nil || json.Unmarshal(raw, &status) != nil || status.InstallationID != inv.InstallationID || status.SchemaVersion != connectorcontrol.SetupStatusSchema {
				t.Fatal("normal XI setup status shape")
			}
			return status
		}
		assertReceiving := func(status connectorcontrol.SetupStatus) {
			t.Helper()
			if status.Receiving.LastSuccessAt == nil || status.Receiving.ContentReceivedAt == nil || status.Receiving.ContentReadVerifiedAt == nil ||
				status.Receiving.ContentReadVerifiedAt.Before(*status.Receiving.ContentReceivedAt) {
				t.Fatal("normal status lacks actual receiving/body-read evidence")
			}
			if len(status.Receiving.ProfilesReceived) != len(expectedReceived) {
				t.Fatal("normal status claimed profiles beyond actual emitted documents", status.Receiving.ProfilesReceived)
			}
			for _, profile := range expectedReceived {
				if !slices.Contains(status.Receiving.ProfilesReceived, profile) {
					t.Fatal("normal status lacks actual committed profile", profile)
				}
			}
		}
		readURStatus := func(expectedVersion string) connectorcontrol.SetupStatus {
			out := call("GET", "/api/v2/organizations/"+input.OrgID+"/xinsight/setups/"+inv.InstallationID, nil)
			var schema, state string
			var ready bool
			var xi connectorcontrol.SetupStatus
			if json.Unmarshal(out["schema_version"], &schema) != nil || schema != "unirouter.xinsight.setup-status.v1" ||
				json.Unmarshal(out["state"], &state) != nil || json.Unmarshal(out["configuration_ready"], &ready) != nil ||
				json.Unmarshal(out["xi"], &xi) != nil || xi.InstallationID != inv.InstallationID || state != "ready" || !ready {
				t.Fatal("normal UR setup status is not actually ready")
			}
			var policy struct {
				State         string `json:"state"`
				PolicyRef     string `json:"policy_ref"`
				PolicyVersion string `json:"policy_version"`
				VersionVector string `json:"version_vector"`
			}
			if json.Unmarshal(out["ur_policy_status"], &policy) != nil || policy.State != "effective" || policy.PolicyRef == "" || policy.PolicyVersion == "" || policy.VersionVector == "" || expectedVersion != "" && policy.PolicyVersion != expectedVersion {
				t.Fatal("normal UR status lacks current effective policy version")
			}
			assertReceiving(xi)
			return xi
		}
		initial := readXIStatus()
		if initial.State != "ready" || !initial.Custody.Loaded || initial.Custody.Settings == nil || initial.Custody.Settings.Mode != "full_content" || initial.Custody.EffectiveRevision != initial.Custody.DesiredRevision {
			t.Fatal("normal XI setup not effective after actual body read")
		}
		assertReceiving(initial)
		if urXI := readURStatus(""); urXI.Custody.EffectiveRevision != initial.Custody.EffectiveRevision {
			t.Fatal("normal UR/XI current revision mismatch")
		}

		decodeUR := func(out map[string]json.RawMessage) reviewpolicycontrol.URResponse {
			var ur reviewpolicycontrol.URResponse
			if json.Unmarshal(out["ur"], &ur) != nil {
				t.Fatal("normal delegated policy response shape")
			}
			return ur
		}
		scopes := decodeUR(normalXICall("GET", "/api/v1/review-capture/unirouter/scopes", nil)).Scopes
		if scopes == nil || scopes.InstallationID != inv.InstallationID || scopes.OrgID != input.OrgID {
			t.Fatal("normal actual policy scopes")
		}
		var enterpriseGrant, projectGrant *urmachine.Grant
		for index := range scopes.Grants {
			g := &scopes.Grants[index]
			if g.TargetKind == "enterprise_default" && slices.Contains(g.Actions, "read") && slices.Contains(g.Actions, "write") {
				enterpriseGrant = g
			}
			if g.TargetKind == "project_policy" && slices.Contains(g.Actions, "read") && (g.Coverage == "all_current_future" || g.ProjectID == input.ProjectID) {
				projectGrant = g
			}
		}
		if enterpriseGrant == nil || projectGrant == nil {
			t.Fatal("normal final consent did not grant actual enterprise read/write")
		}
		enterprisePath := "/api/v1/review-capture/unirouter/organizations/" + input.OrgID
		readEnterprise := func() urmachine.PolicyView {
			view := decodeUR(normalXICall("GET", enterprisePath+"?delegation_id="+enterpriseGrant.GrantID, nil)).View
			if view == nil {
				t.Fatal("normal current enterprise view")
			}
			return *view
		}
		readProject := func() json.RawMessage {
			view := decodeUR(normalXICall("GET", "/api/v1/review-capture/unirouter/projects/"+input.ProjectID+"?delegation_id="+projectGrant.GrantID, nil)).View
			if view == nil {
				t.Fatal("normal current project view")
			}
			return view.Project
		}
		type policyLayer struct {
			PolicyRef string `json:"policy_ref"`
			Explicit  bool   `json:"explicit"`
			Desired   *struct {
				PolicyVersion string          `json:"policy_version"`
				Config        json.RawMessage `json:"config"`
			} `json:"desired"`
		}
		var originalProject policyLayer
		if json.Unmarshal(readProject(), &originalProject) != nil {
			t.Fatal("normal project override shape")
		}
		var originalEnterprise policyLayer
		if json.Unmarshal(readEnterprise().Enterprise, &originalEnterprise) != nil || originalEnterprise.Desired == nil {
			t.Fatal("normal original enterprise policy")
		}
		var originalConfig map[string]any
		if json.Unmarshal(originalEnterprise.Desired.Config, &originalConfig) != nil || originalConfig["mode"] != "full_content" {
			t.Fatal("normal original full-content policy")
		}
		var originalExecution struct {
			Mode  string `json:"desired_execution_storage_mode"`
			Quota int64  `json:"desired_default_project_quota_bytes"`
		}
		if json.Unmarshal(readEnterprise().Enterprise, &originalExecution) != nil || originalExecution.Mode == "" || originalExecution.Quota < 1 {
			t.Fatal("normal enterprise storage/quota shape")
		}
		for _, mode := range []string{"disabled", "full_content"} {
			view := readEnterprise()
			var layer policyLayer
			if json.Unmarshal(view.Enterprise, &layer) != nil || layer.Desired == nil {
				t.Fatal("normal current enterprise CAS")
			}
			configDraft := map[string]any{}
			for k, v := range originalConfig {
				configDraft[k] = v
			}
			if mode == "disabled" {
				configDraft["mode"] = "metadata_only"
				configDraft["allowed_purposes"] = []string{}
				for _, field := range []string{"retention_seconds", "quota_bytes", "max_saved_bytes", "on_limit"} {
					delete(configDraft, field)
				}
			}
			xiDraft := *initial.Custody.Settings
			xiDraft.Mode = mode
			current := readXIStatus()
			ref := "xipw_" + strings.ReplaceAll(testUUID(t), "-", "")
			decodeUR(normalXICall("PUT", enterprisePath, map[string]any{
				"delegation_id": enterpriseGrant.GrantID, "delegation_revision": enterpriseGrant.Revision,
				"operation_ref": ref, "retry_unconfirmed": false,
				"policy": map[string]any{"schema_version": "unirouter.payload-policy.v2", "expected_policy_ref": layer.PolicyRef,
					"expected_version": layer.Desired.PolicyVersion, "expected_vector": view.VersionVector, "desired": configDraft,
					"execution_storage_mode": originalExecution.Mode, "project_quota_bytes": originalExecution.Quota},
				"xi_settings": xiDraft, "xi_expected_revision": current.Custody.DesiredRevision,
			}))
			await("normal enterprise joint "+mode, func() (bool, error) {
				op := decodeUR(normalXICall("GET", "/api/v1/review-capture/unirouter/operations/"+ref, nil)).Operation
				if op == nil || op.OperationRef != ref {
					return false, fmt.Errorf("normal operation identity mismatch")
				}
				if op.State == "failed" || op.State == "drift" {
					return false, fmt.Errorf("normal enterprise operation %s", op.State)
				}
				return op.State == "effective", nil
			})
			pending := readXIStatus()
			if pending.Custody.DesiredRevision != current.Custody.DesiredRevision+1 {
				t.Fatal("joint did not advance current XI revision")
			}
			normalXICall("POST", "/api/v1/review-capture/default/confirm", map[string]int64{"desired_revision": pending.Custody.DesiredRevision})
			after := readXIStatus()
			assertReceiving(after)
			if after.State != "ready" || !after.Custody.Loaded || after.Custody.Settings == nil || after.Custody.Settings.Mode != mode ||
				after.Custody.DesiredRevision != pending.Custody.DesiredRevision || after.Custody.EffectiveRevision != pending.Custody.DesiredRevision || after.ConsentRevision != initial.ConsentRevision {
				t.Fatal("normal setup incorrectly froze original consent instead of current policy")
			}
			updated := readEnterprise()
			var updatedLayer policyLayer
			if updated.VersionVector == view.VersionVector || json.Unmarshal(updated.Enterprise, &updatedLayer) != nil || updatedLayer.Desired == nil {
				t.Fatal("actual UR enterprise version did not advance")
			}
			if urXI := readURStatus(updatedLayer.Desired.PolicyVersion); urXI.Custody.EffectiveRevision != after.Custody.EffectiveRevision || urXI.Custody.Settings == nil || urXI.Custody.Settings.Mode != mode {
				t.Fatal("UR did not read current XI joint policy")
			}
			var project policyLayer
			if json.Unmarshal(readProject(), &project) != nil || project.Explicit != originalProject.Explicit ||
				project.Explicit && !reflect.DeepEqual(project.Desired, originalProject.Desired) {
				t.Fatal("enterprise joint write overwrote project exception")
			}
		}
	}
	if input.ManagedFutureProjects {
		stopWork()
		if err = <-workDone; err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal("current worker stop", err)
		}
		workExited = true
		machineFiles := map[string][]byte{}
		for _, purpose := range []string{connectorcontrol.PolicyMachinePurpose, connectorcontrol.EnrollmentMachinePurpose} {
			path, e := e.MachineCredentialPath(inv.InstallationID, purpose)
			if e != nil {
				t.Fatal(e)
			}
			value, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			machineFiles[path] = value
		}
		orgID, err := strconv.ParseInt(input.OrgID, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		created := call("POST", "/api/v1/projects", map[string]any{"org_id": orgID, "name": "Joint future project", "slug": "joint-future-" + strconv.FormatInt(time.Now().UnixNano(), 10)})
		var futureID, createdOrg int64
		if json.Unmarshal(created["id"], &futureID) != nil || json.Unmarshal(created["org_id"], &createdOrg) != nil || futureID < 1 || createdOrg != orgID || strconv.FormatInt(futureID, 10) == input.ProjectID || strconv.FormatInt(futureID, 10) == input.ForeignProjectID {
			t.Fatal("actual future project readback")
		}
		futureProject := strconv.FormatInt(futureID, 10)
		await("future project production discovery", func() (bool, error) {
			err := projects.Refresh(ctx)
			if errors.Is(err, hostctl.ErrOperationBusy) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			r, err := f.workspace.ManagedEnrollment(ctx, inv.InstallationID, enrollment, futureProject)
			if errors.Is(err, connectorcontrol.ErrNotFound) {
				return false, nil
			}
			return err == nil && r.Status == "active", err
		})
		futurePath := filepath.Join(sourcefacts.ManagedPartitionsDirectory(paths.SourceFactsConfig), inv.InstallationID+"-"+enrollment+"-project-"+futureProject+".json")
		futureRaw, err := os.ReadFile(futurePath)
		if err != nil {
			t.Fatal(err)
		}
		futureCfg, err := sourcefacts.ParseManagedConfig(futureRaw)
		if err != nil || len(futureCfg.Profiles) != 8 || len(futureCfg.Contents) != 1 {
			t.Fatal("future actual eight/C partition", err)
		}
		var futureXIProject, stableOriginal string
		if err = f.admin.QueryRowContext(ctx, `SELECT xi_project_id::text FROM review_capture_project_bindings
 WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4`,
			f.source, input.SourceInstance, input.OrgID, futureProject).Scan(&futureXIProject); err != nil || futureXIProject == "" || futureXIProject == originalXIProject {
			t.Fatal("future managed C project must have an independent XI binding", err)
		}
		if err = projects.Refresh(ctx); err != nil {
			t.Fatal("idempotent managed project refresh", err)
		}
		if err = f.admin.QueryRowContext(ctx, `SELECT xi_project_id::text FROM review_capture_project_bindings
 WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4`,
			f.source, input.SourceInstance, input.OrgID, input.ProjectID).Scan(&stableOriginal); err != nil || stableOriginal != originalXIProject {
			t.Fatal("original managed XI binding changed on future refresh", err)
		}
		anchorRaw, err := os.ReadFile(configPath)
		if err != nil || !bytes.Equal(anchorRaw, rawConfig) {
			t.Fatal("future discovery changed anchor config", err)
		}
		for path, value := range machineFiles {
			current, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(value, current) {
				t.Fatal("future discovery rotated machine custody", err)
			}
		}
		runner.mu.Lock()
		futureRestarts := runner.sourceRestarts
		runner.mu.Unlock()
		if futureRestarts != restarts+1 {
			t.Fatalf("future batch source refresh count=%d", futureRestarts)
		}
		entries, err := os.ReadDir(sourcefacts.ManagedPartitionsDirectory(paths.SourceFactsConfig))
		if err != nil {
			t.Fatal(err)
		}
		partitionCount := 0
		for _, entry := range entries {
			if sourcefacts.ValidManagedPartitionName(entry.Name()) {
				partitionCount++
			}
		}
		if partitionCount != 2 {
			t.Fatalf("actual published partition count=%d", partitionCount)
		}
		var foreignRows int
		if err = f.admin.QueryRowContext(ctx, `SELECT count(*) FROM connector_source_enrollments WHERE installation_id=$1 AND project_id=$2`, inv.InstallationID, input.ForeignProjectID).Scan(&foreignRows); err != nil || foreignRows != 0 {
			t.Fatal("foreign project received local enrollment", err)
		}
		// The existing worker rotates both real published partitions. No extra
		// receiver, machine pair, grant, worker runner or per-config limit.
		rotating := jointManagedSourceWorker(t, f, copyStore, cfg, pool, keys)
		rotating.Partitions = []sourcefacts.ManagedWorker{jointManagedSourceWorker(t, f, copyStore, futureCfg, pool, keys)}
		observed := make(chan string, 18)
		jobLabels := map[string]string{}
		for index, partition := range []sourcefacts.ManagedConfig{cfg, futureCfg} {
			for _, profile := range partition.Profiles {
				jobLabels[profile.StreamID] = fmt.Sprintf("project_group=%d profile=%s", index, profile.Profile)
			}
			for _, content := range partition.Contents {
				jobLabels[content.CollectionID] = fmt.Sprintf("project_group=%d profile=content", index)
			}
		}
		logged := map[string]bool{}
		rotating.Observe = func(id string, err error) {
			// One actual outcome per job exposes a gap without recording root
			// IDs, credentials, source hashes or raw page/body data.
			if !logged[id] {
				logged[id] = true
				t.Logf("joint actual rotation %s success=%t error=%v", jobLabels[id], err == nil, err)
			}
			if err == nil {
				select {
				case observed <- id:
				default:
				}
			}
		}
		rotateCtx, stopRotate := context.WithCancel(ctx)
		rotateDone := make(chan error, 1)
		go func() { rotateDone <- rotating.Run(rotateCtx) }()
		rotateExited := false
		defer func() {
			stopRotate()
			if !rotateExited {
				if err := <-rotateDone; err != nil && !errors.Is(err, context.Canceled) {
					t.Error("future rotation", err)
				}
			}
		}()
		seen := map[string]bool{}
		await("all eighteen real HTTPS commits", func() (bool, error) {
			select {
			case err := <-rotateDone:
				rotateExited = true
				return false, fmt.Errorf("future worker exited before commits: %w", err)
			default:
			}
			for {
				select {
				case id := <-observed:
					seen[id] = true
				default:
					return len(seen) == 18, nil
				}
			}
		})
		for _, partition := range []sourcefacts.ManagedConfig{cfg, futureCfg} {
			for _, profile := range partition.Profiles {
				current, err := f.consumerStore.LoadSourceFactsStream(ctx, profile.StreamID)
				if err != nil || current.Cursor == "" || current.Status != "active" || current.RequiresAttention || current.LastError != "" {
					t.Fatal("actual facts page commit", err)
				}
			}
			for _, content := range partition.Contents {
				current, err := f.consumerStore.LoadContentCollection(ctx, content.CollectionID)
				if err != nil || current.Cursor == "" || current.Status != "active" || current.RequiresAttention || current.LastError != "" {
					t.Fatal("actual C page commit", err)
				}
			}
		}
	}
	if originalState != nil {
		await("original C custody completion", func() (bool, error) {
			if err := maintainSetup(); err != nil {
				return false, err
			}
			r, e := f.workspace.ManagedEnrollment(ctx, inv.InstallationID, enrollment, input.ProjectID)
			return e == nil && r.Status == "active" && !r.LegacyCCopyPending, e
		})
		current, e := originalState.Load(ctx)
		if e != nil || current.ID != originalCheckpoint.ID || current.CheckpointEpoch != 2 || current.CheckpointOperationID != originalCheckpoint.CheckpointOperationID || current.AuthorizationRef != originalCheckpoint.AuthorizationRef || current.Cursor != originalCheckpoint.Cursor || current.HighWatermark != originalCheckpoint.HighWatermark || current.PagesCommitted <= originalCheckpoint.PagesCommitted {
			t.Fatal("adoption reset original feed", e)
		}
		var history int
		if e = f.admin.QueryRowContext(ctx, `SELECT count(*) FROM connector_checkpoint_epoch_history`).Scan(&history); e != nil || history != 1 {
			t.Fatal("adoption changed original history", e)
		}
		root, e := os.ReadFile(paths.SourceFactsConfig)
		if e != nil || !bytes.Equal(root, originalRoot) {
			t.Fatal("adoption rewrote original config", e)
		}
		if _, e = os.Lstat(filepath.Join(sourcefacts.ManagedPartitionsDirectory(paths.SourceFactsConfig), inv.InstallationID+"-"+enrollment+"-project-"+input.ProjectID+".json")); !os.IsNotExist(e) {
			t.Fatal("adoption duplicated original source partition")
		}
	}
}

func jointManagedSourceWorker(t *testing.T, f *sourceFixture, copyStore *db.WorkspaceStore, cfg sourcefacts.ManagedConfig, pool *x509.CertPool, keys reviewbody.KeyRing) sourcefacts.ManagedWorker {
	t.Helper()
	worker := sourcefacts.ManagedWorker{ConfigVersion: cfg.SchemaVersion, PollInterval: time.Duration(cfg.PollIntervalSeconds) * time.Second, RetryMaximum: time.Duration(cfg.RetryMaximumSeconds) * time.Second}
	for _, profile := range cfg.Profiles {
		key, err := config.LoadOwnerOnlySecretFile(profile.KeyFile)
		if err != nil {
			t.Fatal(err)
		}
		reader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: profile.Endpoint, ServiceKey: key, TLSRootCAs: pool})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(reader.CloseContentConnections)
		stream := profile.Registration()
		worker.Profiles = append(worker.Profiles, sourcefacts.ManagedProfile{StreamID: profile.StreamID, Consumer: sourcefacts.Consumer{Store: f.consumerStore, Reader: reader, ExpectedStream: &stream}, CollectCurrentRuntime: profile.Profile == sourcefacts.RuntimeProfile})
	}
	content := cfg.Contents[0]
	resolved, err := cfg.ResolveContent(content)
	if err != nil {
		t.Fatal(err)
	}
	key, err := config.LoadOwnerOnlySecretFile(resolved.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: content.Endpoint, ServiceKey: key, TLSRootCAs: pool})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.CloseContentConnections)
	worker.Contents = []sourcefacts.ContentConsumer{{Store: f.consumerStore, Reader: reader, Expected: resolved.Registration(), AfterCommit: func(ctx context.Context, c sourcefacts.ContentCollection, limits sourcefacts.ContentLimits) error {
		return reviewcopy.PollURCCopies(ctx, reader, copyStore, c.ID, limits, keys, time.Now)
	}}}
	return worker
}

// Strip only the test fixture timezone option at the real host credential
// boundary. Internal DB assertions keep their original timezone and DSN.
func jointNormalWorkspaceCredential(dsn string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("normal workspace credential URL invalid")
	}
	q := u.Query()
	q.Del("options")
	u.RawQuery = q.Encode()
	return config.ValidatePostgresDSN(u.String())
}
func TestJointNormalWorkspaceCredentialPreflight(t *testing.T) {
	raw := "postgres://xinsight_admin:synthetic@127.0.0.1:65439/xi_p3_fixture_child?sslmode=disable&options=-cTimeZone%3DAsia%2FShanghai"
	prepared, err := jointNormalWorkspaceCredential(raw)
	if err != nil {
		t.Fatal("synthetic credential preflight", err)
	}
	before, _ := url.Parse(raw)
	after, _ := url.Parse(prepared)
	if before.Host != after.Host || before.Path != after.Path || before.User.String() != after.User.String() || after.Query().Get("sslmode") != "disable" || after.Query().Has("options") {
		t.Fatal("normal credential scope drift")
	}
	if _, err = jointNormalWorkspaceCredential("postgres://xinsight_admin@127.0.0.1:65439/xi_p3_fixture_child?sslmode=disable"); err == nil {
		t.Fatal("missing explicit password accepted")
	}
	if _, err = jointNormalWorkspaceCredential(raw + "&unexpected=1"); err == nil {
		t.Fatal("non-timezone unsupported option accepted")
	}
}

// Diagnostics remain inside this original acceptance fixture and never print
// request bodies, tokens, URLs, DSNs, database error details or key material.
type jointSetupDiagnostic struct {
	t      *testing.T
	actual connectorcontrol.SetupController
}

func (d jointSetupDiagnostic) ExecuteSetup(ctx context.Context, request connectorcontrol.SetupLocalRequest) (connectorcontrol.SetupLocalResponse, error) {
	out, err := d.actual.ExecuteSetup(ctx, request)
	if err != nil {
		var sqlError interface{ SQLState() string }
		state := "none"
		if errors.As(err, &sqlError) {
			state = sqlError.SQLState()
		}
		d.t.Logf("actual XI root setup action=%s error_class=%T sqlstate=%s", request.Action, err, state)
	}
	return out, err
}

func jointSetupSerializationPending(err error) bool {
	var failure *pgconn.PgError
	return errors.As(err, &failure) && failure.Code == "40001"
}

func jointManagedCustodyDiagnostic(ctx context.Context, store *sql.DB, installationID, collectionID, sourceID string, input urJointConsumerInput) (string, error) {
	var state string
	err := store.QueryRowContext(ctx, `SELECT jsonb_build_object(
 'setup', (SELECT jsonb_build_object('policy_revision',setup_policy_revision,'applied',setup_policy_applied_at IS NOT NULL,'error_code',setup_error_code) FROM connector_installations WHERE id=$1),
 'default', (SELECT jsonb_build_object('desired_revision',desired_revision,'effective_revision',effective_revision,'desired_mode',desired_mode,'effective_mode',effective_mode,'effective_now',effective_at<=clock_timestamp()) FROM review_capture_defaults WHERE singleton),
 'project_binding', EXISTS(SELECT 1 FROM review_capture_project_bindings WHERE source_system_id=$3::uuid AND source_instance=$4 AND org_id=$5 AND project_id=$6),
 'collection', (SELECT jsonb_build_object('status',status,'attention',requires_attention,'last_sequence',last_sequence,'high_watermark_sequence',high_watermark_sequence,'has_cursor',cursor_value<>'','has_success',last_success_at IS NOT NULL) FROM source_content_collections WHERE id=$2::uuid),
 'enrollment', (SELECT jsonb_build_object('status',status,'legacy_copy_pending',legacy_c_copy_pending) FROM connector_source_enrollments WHERE installation_id=$1 AND content_collection_id=$2::uuid LIMIT 1),
 'manifests', (SELECT count(*) FROM source_content_manifests WHERE collection_id=$2::uuid),
 'expected_manifests', (SELECT count(*) FROM source_content_manifests WHERE collection_id=$2::uuid AND request_id=$7 AND source_json->'representation'->>'digest'=$8),
 'attempts', (SELECT COALESCE(jsonb_agg(jsonb_build_object('status',status,'reason',reason,'items',items,'attempts',attempts) ORDER BY status,reason),'[]'::jsonb) FROM (SELECT status,reason,count(*) AS items,sum(attempts) AS attempts FROM review_body_copy_attempts WHERE collection_id=$2::uuid GROUP BY status,reason) a),
 'expected_accepted', (SELECT count(*) FROM review_body_operations o JOIN source_content_manifests d ON d.collection_id=o.content_collection_id AND d.source_system_id=o.source_system_id AND d.source_fact_id=o.source_fact_id AND d.revision=o.source_revision WHERE o.content_collection_id=$2::uuid AND o.source_system_id=$3::uuid AND o.source_instance=$4 AND o.org_id=$5 AND o.project_id=$6 AND d.request_id=$7 AND o.digest=$8 AND o.state='accepted')
 )::text`, installationID, collectionID, sourceID, input.SourceInstance, input.OrgID, input.ProjectID, input.ExpectedContent.RequestID, "sha256:"+input.ExpectedContent.SHA256).Scan(&state)
	return state, err
}

func TestJointManagedCustodyDiagnosticPostgres(t *testing.T) {
	f := newSourceFixture(t)
	input := urJointConsumerInput{SourceInstance: "joint-diagnostic", OrgID: "1", ProjectID: "2"}
	input.ExpectedContent.RequestID = "synthetic-joint-request"
	input.ExpectedContent.SHA256 = strings.Repeat("a", 64)
	state, err := jointManagedCustodyDiagnostic(f.ctx, f.admin, "synthetic-installation", testUUID(t), f.source, input)
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		Setup             json.RawMessage `json:"setup"`
		ProjectBinding    bool            `json:"project_binding"`
		Manifests         int             `json:"manifests"`
		ExpectedManifests int             `json:"expected_manifests"`
		ExpectedAccepted  int             `json:"expected_accepted"`
		Attempts          []any           `json:"attempts"`
	}
	if json.Unmarshal([]byte(state), &summary) != nil || string(summary.Setup) != "null" || summary.ProjectBinding || summary.Manifests != 0 || summary.ExpectedManifests != 0 || summary.ExpectedAccepted != 0 || len(summary.Attempts) != 0 {
		t.Fatal("diagnostic invented an absent custody stage")
	}
	for _, secret := range []string{input.ExpectedContent.RequestID, input.ExpectedContent.SHA256, f.source, f.dsn} {
		if strings.Contains(state, secret) {
			t.Fatal("diagnostic exposed source or credential identity")
		}
	}
}
func TestJointSetupSerializationPendingClassification(t *testing.T) {
	original := &pgconn.PgError{Code: "40001", Message: "synthetic rolled-back serialization"}
	if !jointSetupSerializationPending(original) || !jointSetupSerializationPending(fmt.Errorf("stage: %w", original)) {
		t.Fatal("exact PG serialization error not pending")
	}
	for _, err := range []error{nil, &pgconn.PgError{Code: "40P01"}, connectorcontrol.ErrConflict, reviewbody.ErrConflict, errors.New("SQLSTATE 40001"), context.DeadlineExceeded} {
		if jointSetupSerializationPending(err) {
			t.Fatal("non-serialization error ignored")
		}
	}
}

// Seed synthetic historical metadata through the supported bounded-offline
// Store path. The UR fixture publishes its new content only after enrollment;
// this independent prestate never fabricates an upstream page or alters the
// original online stream/content cursor, HWM, epoch, key or registration.
func seedJointRestoredOriginalHistory(t *testing.T, ctx context.Context, input urJointConsumerInput, f *sourceFixture) {
	t.Helper()
	d := f.documents[sourcefacts.ContextProfile]
	d.Core = copySourceMap(d.Core)
	d.SourceInstance, d.OrgID, d.ProjectID = input.SourceInstance, input.OrgID, input.ProjectID
	d.RequestID = "synthetic-restored-original-history"
	d.Core["org_id"], d.Core["project_id"], d.Core["request_id"] = d.OrgID, d.ProjectID, d.RequestID
	f.setPage(sourcefacts.ContextProfile, "", d)
	f.mu.Lock()
	page := f.pages[sourcefacts.ContextProfile]
	f.mu.Unlock()
	page.ProducerInstance = input.SourceInstance
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile("../sourcefacts/contract/source-fact.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	a := sourcefacts.OfflineAuthorization{
		SchemaVersion: sourcefacts.OfflineAuthorizationVersion, AuthorizationID: testUUID(t),
		Environment: "test", TargetRef: "joint-restored-original-history", MachineIDSHA256: sourcefacts.Hash([]byte("synthetic-joint-machine")),
		ProductVersion: "0.2.0-dev.fpv14", ProductCommit: "synthetic-restored-original-fixture",
		ProtocolVersion: "2.0.2", ProtocolCommit: sourcefacts.ProtocolCommit, ProtocolReleaseDigest: sourcefacts.ProtocolReleaseDigest,
		SourceSchemaSHA256: sourcefacts.Hash(schema), SourceSystemID: f.source, SourceInstance: input.SourceInstance,
		OrgID: input.OrgID, ProjectID: input.ProjectID, Profile: sourcefacts.ContextProfile,
		NotBefore: now.Add(-time.Minute).Format(sourcefacts.TimeFormat), ExpiresAt: now.Add(time.Hour).Format(sourcefacts.TimeFormat),
		BundlePath: "/synthetic/joint-restored-original-history.json", BundleSHA256: sourcefacts.Hash(raw),
		BundleID: testUUID(t), OperationID: testUUID(t), MaxRecords: 1, PayloadCaptureMode: "metadata_only",
	}
	registration, err := f.consumerStore.RegisterSourceOfflineAuthorization(ctx, a)
	if err != nil {
		t.Fatal("original history registration", err)
	}
	if _, err = f.consumerStore.ControlSourceOfflineAuthorization(ctx, registration.ID, "active", registration.Revision, a.Digest(), a); err != nil {
		t.Fatal("original history authorization", err)
	}
	receipt, err := f.consumerStore.ImportSourceFactsOffline(ctx, a, raw, now)
	if err != nil || receipt.Counts.Inserted != 1 {
		t.Fatal("original history import", err)
	}
}

func TestJointRestoredOriginalHistoryUsesAuthorizedOfflineStore(t *testing.T) {
	f := newSourceFixture(t)
	input := urJointConsumerInput{SourceInstance: "synthetic-restored-original", OrgID: "17", ProjectID: "86"}
	before, err := f.admin.QueryContext(f.ctx, `SELECT to_jsonb(t)::text FROM source_fact_streams t ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	var streams []string
	for before.Next() {
		var row string
		if err := before.Scan(&row); err != nil {
			t.Fatal(err)
		}
		streams = append(streams, row)
	}
	if err = before.Err(); err != nil {
		t.Fatal(err)
	}
	before.Close()
	seedJointRestoredOriginalHistory(t, f.ctx, input, f)
	var count int
	if err = f.admin.QueryRowContext(f.ctx, `SELECT count(*) FROM source_fact_documents WHERE source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4`, f.source, input.SourceInstance, input.OrgID, input.ProjectID).Scan(&count); err != nil || count != 1 {
		t.Fatal("scoped nonempty synthetic history", count, err)
	}
	after, err := f.admin.QueryContext(f.ctx, `SELECT to_jsonb(t)::text FROM source_fact_streams t ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	var current []string
	for after.Next() {
		var row string
		if err := after.Scan(&row); err != nil {
			t.Fatal(err)
		}
		current = append(current, row)
	}
	if err = after.Err(); err != nil {
		t.Fatal(err)
	}
	after.Close()
	if !reflect.DeepEqual(streams, current) {
		t.Fatal("offline history seed altered original online streams")
	}
}
