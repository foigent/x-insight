package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/checkpointepoch"
	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/connectoractivation"
	"github.com/foigent/x-insight/internal/connectorsetup"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/discovery"
	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/reviewpolicycontrol"
	"github.com/foigent/x-insight/internal/workinstallcontrol"
	"github.com/foigent/x-insight/internal/workspace"
	"golang.org/x/term"
)

func main() {
	syscall.Umask(0o077)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "xinsightctl: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	if handled, err := buildinfo.HandleArgs(args, stdout); handled {
		return err
	}
	if len(args) == 0 {
		return usage()
	}
	if args[0] == "access" {
		return runAccess(args[1:], os.Stdin, stdout, os.Stderr, os.Geteuid() == 0, false)
	}
	if args[0] == "query" {
		return runPersonnelQuery(ctx, args[1:], stdout, os.Stderr)
	}
	if args[0] == "schema-contract" && len(args) >= 2 && args[1] == "expected" {
		if len(args) == 2 {
			return writeResult(stdout, hostctl.ExpectedCleanBaselineSchemaContract(), nil)
		}
		if len(args) == 3 && args[2] == "--raw" {
			_, err := stdout.Write(hostctl.CleanBaselineSchemaContractBytes())
			return err
		}
		return fmt.Errorf("usage: xinsightctl schema-contract expected [--raw]")
	}
	if args[0] == "connector-controller" {
		return runConnectorController(
			ctx,
			args[1:],
			os.Geteuid() == 0,
			serveConnectorController,
		)
	}
	paths, err := hostctl.PathsFromEnvironment()
	if err != nil {
		return err
	}
	if args[0] == "service-identity" {
		return runServiceIdentity(ctx, paths, args[1:], stdout, os.Geteuid() == 0)
	}
	if args[0] == "discovery" {
		return runDiscovery(ctx, paths, args[1:], stdout, os.Geteuid() == 0)
	}
	engine, err := hostctl.NewEngine(paths)
	if err != nil {
		return err
	}
	switch args[0] {
	case "workbench-access":
		return runWorkbenchAccess(ctx, engine, args[1:], stdout, os.Geteuid() == 0)
	case "source-facts":
		return runSourceFactsControl(ctx, engine, args[1:], stdout)
	case "verify":
		artifact, err := artifactFlags("verify", args[1:])
		if err != nil {
			return err
		}
		report, err := engine.VerifyArtifact(artifact)
		return writeResult(stdout, report, err)
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("usage: xinsightctl status")
		}
		report, err := engine.Status(ctx)
		return writeResult(stdout, report, err)
	case "feed-credential":
		return runFeedCredential(ctx, engine, args[1:], stdout)
	case "connector-epoch":
		return runConnectorEpoch(ctx, engine, args[1:], stdout)
	case "connector-target-config":
		return runConnectorTargetConfig(engine, args[1:], os.Geteuid() == 0)
	case "review-body-config":
		return runReviewBodyConfig(engine, args[1:], os.Geteuid() == 0 || engine.Paths.IsTestRoot)
	case "clean-baseline":
		return runCleanBaseline(ctx, engine, args[1:], stdout)
	case "database-admin-credential":
		return runDatabaseAdminCredential(ctx, engine, args[1:], stdout)
	case "schema-contract":
		if len(args) != 2 || args[1] != "inspect" || os.Geteuid() != 0 {
			return fmt.Errorf("usage: xinsightctl schema-contract expected|inspect (inspect requires root)")
		}
		credentials, err := hostctl.LoadDatabaseCredentials(paths)
		if err != nil {
			return err
		}
		report, err := hostctl.InspectCleanBaselineSchemaContractDatabase(ctx, credentials)
		return writeResult(stdout, report, err)
	case "install":
		return runInstall(ctx, engine, args[1:], stdout)
	case "backup":
		return runBackup(ctx, engine, args[1:], stdout)
	case "adopt":
		return runAdopt(ctx, engine, args[1:], stdout)
	case "upgrade":
		artifact, check, err := artifactCheckFlags("upgrade", args[1:])
		if err != nil {
			return err
		}
		if check {
			report, err := engine.CheckUpgrade(ctx, artifact)
			return writeResult(stdout, report, err)
		}
		receipt, err := engine.Upgrade(ctx, artifact)
		return writeResult(stdout, receipt, err)
	case "upgrade-direct-fpv9":
		flags := flag.NewFlagSet("upgrade-direct-fpv9", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		artifact := flags.String("artifact", "", "signed schema43 target release")
		proof := flags.String("source-proof", "", "reviewed exact direct-run FPV9 source proof")
		check := flags.Bool("check", false, "run read-only preflight")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *artifact == "" || *proof == "" {
			return fmt.Errorf("usage: xinsightctl upgrade-direct-fpv9 [--check] --artifact FILE --source-proof FILE")
		}
		if *check {
			report, err := engine.CheckDirectFPV9Upgrade(ctx, *artifact, *proof)
			return writeResult(stdout, report, err)
		}
		receipt, err := engine.DirectFPV9Upgrade(ctx, *artifact, *proof)
		return writeResult(stdout, receipt, err)
	case "rollback":
		return runRollback(ctx, engine, args[1:], stdout)
	case "restore":
		return runRestore(ctx, engine, args[1:], stdout)
	case "recover":
		if len(args) != 1 {
			return fmt.Errorf("usage: xinsightctl recover")
		}
		report, err := engine.Recover(ctx)
		return writeResult(stdout, report, err)
	case "resume":
		if len(args) != 1 {
			return fmt.Errorf("usage: xinsightctl resume")
		}
		receipt, err := engine.Resume(ctx)
		return writeResult(stdout, receipt, err)
	case "abort":
		if len(args) != 1 {
			return fmt.Errorf("usage: xinsightctl abort")
		}
		receipt, err := engine.Abort(ctx)
		return writeResult(stdout, receipt, err)
	default:
		return usage()
	}
}

func runServiceIdentity(
	ctx context.Context,
	paths hostctl.Paths,
	args []string,
	stdout io.Writer,
	rootAuthorized bool,
) error {
	if !rootAuthorized {
		return fmt.Errorf("xinsightctl service-identity commands require root")
	}
	if len(args) == 0 {
		return serviceIdentityUsage()
	}
	credentials, err := hostctl.LoadDatabaseCredentials(paths)
	if err != nil {
		return err
	}
	// Creation is intentionally executed with the root-held administrative
	// credential. The long-running server's workspace role cannot INSERT
	// service identities or token digests.
	store, err := db.OpenWorkspaceStore(ctx, credentials.AdminDSN)
	if err != nil {
		return fmt.Errorf("workspace database is unavailable")
	}
	defer store.Close()
	return runServiceIdentityCommand(ctx, store, args, stdout, time.Now().UTC())
}

func runServiceIdentityCommand(
	ctx context.Context,
	store workspace.AgentAccessStore,
	args []string,
	stdout io.Writer,
	now time.Time,
) error {
	if store == nil {
		return fmt.Errorf("service identity store is unavailable")
	}
	if len(args) == 0 {
		return serviceIdentityUsage()
	}
	switch args[0] {
	case "create":
		flags := flag.NewFlagSet("service-identity create", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		name := flags.String("name", "", "service identity display name")
		expiresAtText := flags.String("expires-at", "", "UTC RFC3339 expiry, at most 365 days")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		expiresAt, parseErr := time.Parse(time.RFC3339, *expiresAtText)
		if flags.NArg() != 0 || *name == "" || parseErr != nil || expiresAt.Location() != time.UTC {
			return fmt.Errorf("usage: xinsightctl service-identity create --name NAME --expires-at UTC_RFC3339")
		}
		credential, err := store.CreateServiceIdentity(
			ctx,
			workspace.Actor{Kind: "root", Ref: "root"},
			workspace.CreateServiceIdentityInput{DisplayName: *name, ExpiresAt: expiresAt},
			now.UTC(),
			nil,
		)
		if err != nil {
			return err
		}
		return writeResult(stdout, credential, nil)
	case "list":
		if len(args) != 1 {
			return fmt.Errorf("usage: xinsightctl service-identity list")
		}
		items, err := store.ListServiceIdentities(ctx, 100)
		return writeResult(stdout, struct {
			Items []workspace.ServiceIdentity `json:"items"`
		}{Items: items}, err)
	case "revoke":
		flags := flag.NewFlagSet("service-identity revoke", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		id := flags.String("id", "", "service identity ID")
		reason := flags.String("reason", "", "bounded revocation reason")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *id == "" || *reason == "" {
			return fmt.Errorf("usage: xinsightctl service-identity revoke --id ID --reason REASON")
		}
		item, err := store.RevokeServiceIdentity(
			ctx, *id, workspace.Actor{Kind: "root", Ref: "root"}, *reason, now.UTC(),
		)
		return writeResult(stdout, item, err)
	default:
		return serviceIdentityUsage()
	}
}

func serviceIdentityUsage() error {
	return fmt.Errorf("usage: xinsightctl service-identity create|list|revoke")
}

func runDiscovery(
	ctx context.Context,
	paths hostctl.Paths,
	args []string,
	stdout io.Writer,
	rootAuthorized bool,
) error {
	if !rootAuthorized {
		return fmt.Errorf("xinsightctl discovery commands require root")
	}
	if len(args) == 0 || args[0] != "refresh" {
		return discoveryUsage()
	}
	flags := flag.NewFlagSet("discovery refresh", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	asOfText := flags.String("as-of", "", "fixed UTC RFC3339 Evidence snapshot")
	limit := flags.Int("limit", 1000, "bounded execution sample, 20-1000")
	minimumSample := flags.Int("minimum-sample", 100, "abstention floor, 20-limit")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	asOf, parseErr := time.Parse(time.RFC3339Nano, *asOfText)
	if flags.NArg() != 0 ||
		parseErr != nil ||
		asOf.Location() != time.UTC ||
		*limit < 20 ||
		*limit > discovery.MaxAnalysisExecutions ||
		*minimumSample < 20 ||
		*minimumSample > *limit {
		return discoveryUsage()
	}
	credentials, err := hostctl.LoadDatabaseCredentials(paths)
	if err != nil {
		return err
	}
	evidenceStore, err := db.OpenEvidenceStore(ctx, credentials.ReaderDSN)
	if err != nil {
		return fmt.Errorf("evidence database is unavailable")
	}
	defer evidenceStore.Close()
	workspaceStore, err := db.OpenWorkspaceStore(ctx, credentials.WorkspaceDSN)
	if err != nil {
		return fmt.Errorf("workspace database is unavailable")
	}
	defer workspaceStore.Close()
	run, err := discovery.BuildRun(
		ctx,
		evidenceStore,
		asOf.UTC(),
		*limit,
		*minimumSample,
	)
	if err != nil {
		return err
	}
	if err := workspaceStore.SaveDiscoveryRun(ctx, run); err != nil {
		return err
	}
	return writeResult(stdout, run, nil)
}

func discoveryUsage() error {
	return fmt.Errorf("usage: xinsightctl discovery refresh --as-of UTC_RFC3339 --limit 20..1000 --minimum-sample 20..limit")
}

func runConnectorController(
	ctx context.Context,
	args []string,
	rootAuthorized bool,
	serve func(context.Context) error,
) error {
	if !rootAuthorized {
		return fmt.Errorf("xinsightctl connector-controller serve requires root")
	}
	if len(args) != 1 || args[0] != "serve" || serve == nil {
		return fmt.Errorf("usage: xinsightctl connector-controller serve")
	}
	return serve(ctx)
}

func runConnectorTargetConfig(engine *hostctl.Engine, args []string, rootAuthorized bool) error {
	if !rootAuthorized {
		return fmt.Errorf("xinsightctl connector-target-config commands require root")
	}
	if len(args) == 1 && args[0] == "validate" {
		return engine.ValidateConnectorTargetConfig()
	}
	if len(args) == 3 && args[0] == "set" && args[1] == "--unirouter-origin" {
		return engine.SetConnectorTargetOrigin(args[2])
	}
	if len(args) > 0 && args[0] == "disable-machines" {
		flags := flag.NewFlagSet("connector-target-config disable-machines", flag.ContinueOnError)
		check := flags.Bool("check", false, "validate retirement without writing")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("unexpected disable argument")
		}
		return engine.DisableURMachineConfig(*check)
	}
	if len(args) > 0 && (args[0] == "set-policy" || args[0] == "set-enrollment") {
		flags := flag.NewFlagSet("connector-target-config "+args[0], flag.ContinueOnError)
		installation := flags.String("installation-id", "", "paired installation")
		source := flags.String("source-instance", "", "paired source instance")
		org := flags.String("org-id", "", "paired organization")
		key := flags.String("credential-file", "", "root-only purpose credential reference")
		ca := flags.String("ca-file", "", "existing approved TLS CA file")
		check := flags.Bool("check", false, "validate without writing or contacting UR")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *installation == "" || *source == "" || *org == "" || *key == "" {
			return fmt.Errorf("machine configuration requires exact identity and credential file")
		}
		c := hostctl.URMachineConfig{InstallationID: *installation, SourceInstance: *source, OrgID: *org, CAFile: *ca}
		if args[0] == "set-policy" {
			c.PolicyFile = *key
		} else {
			c.EnrollFile = *key
		}
		return engine.SetURMachineConfig(c, *check)
	}
	return fmt.Errorf("usage: xinsightctl connector-target-config validate|set --unirouter-origin HTTPS_ORIGIN|set-policy|set-enrollment|disable-machines [--check]")
}

func runReviewBodyConfig(engine *hostctl.Engine, args []string, rootAuthorized bool) error {
	if !rootAuthorized {
		return fmt.Errorf("xinsightctl review-body-config commands require root")
	}
	if len(args) == 1 && args[0] == "validate" {
		return engine.ValidateReviewBodyConfig()
	}
	if len(args) > 0 && args[0] == "disable" {
		flags := flag.NewFlagSet("review-body-config disable", flag.ContinueOnError)
		check := flags.Bool("check", false, "validate retirement without writing")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("unexpected disable argument")
		}
		return engine.DisableReviewBodyConfig(*check)
	}
	if len(args) > 0 && args[0] == "set" {
		flags := flag.NewFlagSet("review-body-config set", flag.ContinueOnError)
		id := flags.String("key-id", "", "custody key ID")
		file := flags.String("key-file", "", "root-only custody key reference")
		check := flags.Bool("check", false, "validate without writing or restarting")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("unexpected review body configuration argument")
		}
		return engine.SetReviewBodyConfig(hostctl.ReviewBodyConfig{KeyID: *id, KeyFile: *file}, *check)
	}
	return fmt.Errorf("usage: xinsightctl review-body-config validate|set --key-id ID --key-file ROOT_FILE [--check]|disable [--check]")
}

func serveConnectorController(ctx context.Context) error {
	paths, err := hostctl.PathsFromEnvironment()
	if err != nil {
		return err
	}
	adminDSN, err := config.LoadAdminPostgresDSN()
	if err != nil {
		return fmt.Errorf("connector controller database credential is unavailable")
	}
	database, err := sql.Open("pgx", adminDSN)
	if err != nil {
		return fmt.Errorf("connector controller database is unavailable")
	}
	defer database.Close()
	if err := database.PingContext(ctx); err != nil {
		return fmt.Errorf("connector controller database is unavailable")
	}
	database.SetMaxOpenConns(10)
	database.SetMaxIdleConns(5)
	database.SetConnMaxLifetime(30 * time.Minute)
	database.SetConnMaxIdleTime(5 * time.Minute)

	engine, err := hostctl.NewEngine(paths)
	if err != nil {
		return err
	}
	store := db.NewWorkspaceStore(database)
	deployment, err := engine.ConnectorDeploymentID()
	if err != nil {
		return err
	}
	if err = store.SetConnectorDeploymentID(deployment); err != nil {
		return err
	}
	epochPort, err := checkpointepoch.NewPostgresInstallationEpochPort(database)
	if err != nil {
		return err
	}
	host, err := connectoractivation.NewHostctlAdapter(engine)
	if err != nil {
		return err
	}
	reconciler, err := connectoractivation.NewProductReconciler(
		store, epochPort, host, time.Now, rand.Reader,
	)
	if err != nil {
		return err
	}
	var managedAvailable bool
	if err = database.QueryRowContext(ctx, `SELECT to_regclass('public.connector_receiver_adoptions') IS NOT NULL`).Scan(&managedAvailable); err != nil {
		return err
	}
	var composed connectoractivation.Reconciler = reconciler
	var managed *connectoractivation.ManagedReconciler
	if managedAvailable {
		managed = &connectoractivation.ManagedReconciler{Store: store, Host: engine, Legacy: reconciler, Now: time.Now, Random: rand.Reader}
		composed = managed
	}
	controller, err := connectoractivation.NewController(composed)
	if err != nil {
		return err
	}
	managedUID, managedGID, err := lookupManagedXInsightIdentity()
	if err != nil {
		return err
	}
	owner, err := db.OpenSourceFactsStore(adminDSN)
	if err != nil {
		return err
	}
	defer owner.Close()
	expiryAvailable, err := owner.ReviewBodyExpiryAvailable(ctx)
	if err != nil {
		return fmt.Errorf("review body maintenance schema probe failed: %w", err)
	}
	var expirer reviewBodyExpirer
	if expiryAvailable {
		expirer = owner
	} else {
		fmt.Fprintln(os.Stderr, "xinsightctl: review body physical expiry unavailable until custody schema is installed")
	}
	urPolicyClient, urIdentity, err := loadDelegatedURPolicyClient()
	if err != nil {
		return err
	}
	policyManager := reviewpolicycontrol.Manager{Store: owner, EnsureCustody: engine.EnsureReviewBodyLoaded, URStore: owner, URClient: urPolicyClient, URIdentity: urIdentity, URClientFactory: managedPolicyReloadFactory(engine, urPolicyClient, urIdentity)}
	server, err := connectoractivation.NewUnixServer(store, controller, managedUID, managedGID, policyManager)
	if err != nil {
		return err
	}
	if err = server.SetWorkInstallManager(workinstallcontrol.Manager{Store: owner}); err != nil {
		return err
	}
	var maintenance []func(context.Context) error
	if managedAvailable {
		setup := &connectorsetup.Manager{Store: store, Host: engine, Policy: policyManager, Now: time.Now}
		if err = server.SetSetupController(setup); err != nil {
			return err
		}
		maintenance = append(maintenance, setup.Tick)
	}
	if managed != nil {
		if err = server.SetManagedAcknowledger(managed); err != nil {
			return err
		}
		maintenance = append(maintenance, managed.Tick)
		roots, err := managedSourceRoots(engine)
		if err != nil {
			return err
		}
		projects := &connectoractivation.ManagedProjects{Store: store, Sources: owner, Host: engine, EnrollmentClient: managedEnrollmentFactory(engine), PolicyClient: managedPolicyFactory(engine), TLSRoots: roots, Now: time.Now}
		maintenance = append(maintenance, projects.Refresh)
	}
	return serveControllerWithReviewBodyExpiry(ctx, server.Serve, expirer, time.Minute, time.Now,
		func(err error) { fmt.Fprintf(os.Stderr, "xinsightctl: controller maintenance pending: %v\n", err) }, maintenance...)
}

type reviewBodyExpirer interface {
	ExpireReviewBodies(context.Context, time.Time, int) (int, error)
}

// The existing privileged controller owns one bounded expiry pass per minute.
// It never scans files or starts a separate maintenance daemon. Startup runs
// the same pass so a controller restart resumes fixed-deadline cleanup.
func serveControllerWithReviewBodyExpiry(ctx context.Context, serve func(context.Context) error,
	expirer reviewBodyExpirer, interval time.Duration, now func() time.Time, report func(error), maintenance ...func(context.Context) error) error {
	if serve == nil || interval <= 0 || now == nil || report == nil {
		return fmt.Errorf("review body expiry controller is not initialized")
	}
	ownedCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- serve(ownedCtx) }()
	sweep := func() error {
		if ownedCtx.Err() != nil {
			return nil
		}
		for _, step := range maintenance {
			if step == nil {
				return fmt.Errorf("controller maintenance is invalid")
			}
			stepCtx, stop := context.WithTimeout(ownedCtx, 30*time.Second)
			err := step(stepCtx)
			stop()
			if err != nil {
				report(err)
			}
		}
		if expirer == nil {
			return nil
		}
		for batch := 0; batch < 10; batch++ {
			stepCtx, stop := context.WithTimeout(ownedCtx, 15*time.Second)
			count, err := expirer.ExpireReviewBodies(stepCtx, now().UTC(), 100)
			stop()
			if err != nil {
				if ownedCtx.Err() != nil {
					return nil
				}
				return fmt.Errorf("review body expiry failed: %w", err)
			}
			if count < 100 {
				break
			}
		}
		return nil
	}
	if expirer != nil || len(maintenance) > 0 {
		if err := sweep(); err != nil {
			report(err)
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case err := <-finished:
			return err
		case <-ctx.Done():
			return <-finished
		case <-ticker.C:
			if expirer != nil || len(maintenance) > 0 {
				if err := sweep(); err != nil {
					report(err)
				}
			}
		}
	}
}

func lookupManagedXInsightIdentity() (uint32, uint32, error) {
	account, err := user.Lookup("xinsight")
	if err != nil {
		return 0, 0, fmt.Errorf("managed xinsight operating-system identity is unavailable")
	}
	uid, uidErr := strconv.ParseUint(account.Uid, 10, 32)
	gid, gidErr := strconv.ParseUint(account.Gid, 10, 32)
	if uidErr != nil || gidErr != nil || uid == 0 || gid == 0 {
		return 0, 0, fmt.Errorf("managed xinsight operating-system identity is invalid")
	}
	return uint32(uid), uint32(gid), nil
}

func runAccess(
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	rootAuthorized bool,
	allowNonInteractivePasswordForTest bool,
) error {
	if !rootAuthorized {
		return fmt.Errorf("xinsightctl access commands require root")
	}
	if len(args) == 0 {
		return accessUsage()
	}
	switch args[0] {
	case "hash-password":
		if len(args) != 1 {
			return fmt.Errorf("usage: xinsightctl access hash-password")
		}
		password, confirmation, err := readPasswordPair(
			stdin,
			stderr,
			allowNonInteractivePasswordForTest,
		)
		if err != nil {
			return err
		}
		defer clearSecret(password)
		defer clearSecret(confirmation)
		if subtle.ConstantTimeCompare(password, confirmation) != 1 {
			return fmt.Errorf("password confirmation does not match")
		}
		encoded, err := access.DerivePasswordHash(password)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, encoded)
		return err
	case "generate-subject":
		if len(args) != 1 {
			return fmt.Errorf("usage: xinsightctl access generate-subject")
		}
		var entropy [16]byte
		if _, err := rand.Read(entropy[:]); err != nil {
			return fmt.Errorf("subject ID generation failed")
		}
		_, err := fmt.Fprintf(stdout, "xs_%s\n", hex.EncodeToString(entropy[:]))
		return err
	case "validate":
		flags := flag.NewFlagSet("access validate", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		path := flags.String("file", "", "absolute root-owned local account file")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *path == "" || !filepath.IsAbs(*path) || filepath.Clean(*path) != *path {
			return fmt.Errorf("usage: xinsightctl access validate --file ABSOLUTE_PATH")
		}
		config, err := access.ReadAndValidateAccountFile(*path)
		if err != nil {
			return err
		}
		report := struct {
			SchemaVersion            string `json:"schema_version"`
			AccountCount             int    `json:"account_count"`
			InvestigatorCount        int    `json:"investigator_count"`
			OperatorCount            int    `json:"operator_count"`
			AccessOperatorCount      int    `json:"access_operator_count"`
			PlatformAdminCount       int    `json:"platform_admin_count"`
			WorkbenchReaderCount     int    `json:"workbench_reader_count"`
			InvestigationReaderCount int    `json:"investigation_reader_count"`
			ScopedReaderCount        int    `json:"workbench_investigation_reader_count"`
			ScopedReviewerCount      int    `json:"workbench_investigation_reviewer_count"`
			PurposeReviewerCount     int    `json:"workbench_purpose_reviewer_count"`
			DisabledCount            int    `json:"disabled_count"`
		}{
			SchemaVersion: config.SchemaVersion,
			AccountCount:  len(config.Accounts),
		}
		for _, account := range config.Accounts {
			switch account.Role {
			case access.RoleInvestigator:
				report.InvestigatorCount++
			case access.RoleOperator:
				report.OperatorCount++
			case access.RoleAccessOperator:
				report.AccessOperatorCount++
			case access.RolePlatformAdmin:
				report.PlatformAdminCount++
			case access.RoleWorkbenchReader:
				report.WorkbenchReaderCount++
			case access.RoleInvestigationReader:
				report.InvestigationReaderCount++
			case access.RoleScopedReader:
				report.ScopedReaderCount++
			case access.RoleScopedReviewer:
				report.ScopedReviewerCount++
			case access.RolePurposeReviewer:
				report.PurposeReviewerCount++
			}
			if account.Disabled {
				report.DisabledCount++
			}
		}
		return writeResult(stdout, report, nil)
	default:
		return accessUsage()
	}
}

func readPasswordPair(
	input io.Reader,
	prompt io.Writer,
	allowNonInteractiveForTest bool,
) ([]byte, []byte, error) {
	if file, ok := input.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		if _, err := fmt.Fprint(prompt, "Password: "); err != nil {
			return nil, nil, err
		}
		password, err := term.ReadPassword(int(file.Fd()))
		if _, writeErr := fmt.Fprintln(prompt); err == nil {
			err = writeErr
		}
		if err != nil {
			return nil, nil, fmt.Errorf("password input failed")
		}
		if _, err := fmt.Fprint(prompt, "Confirm password: "); err != nil {
			clearSecret(password)
			return nil, nil, err
		}
		confirmation, err := term.ReadPassword(int(file.Fd()))
		if _, writeErr := fmt.Fprintln(prompt); err == nil {
			err = writeErr
		}
		if err != nil {
			clearSecret(password)
			return nil, nil, fmt.Errorf("password confirmation input failed")
		}
		return password, confirmation, nil
	}
	if !allowNonInteractiveForTest {
		return nil, nil, fmt.Errorf("password derivation requires an interactive terminal")
	}
	reader := bufio.NewReader(io.LimitReader(input, int64(2*(access.MaxPasswordBytes+2)+1)))
	password, err := readPasswordLine(reader)
	if err != nil {
		return nil, nil, err
	}
	confirmation, err := readPasswordLine(reader)
	if err != nil {
		clearSecret(password)
		return nil, nil, err
	}
	if trailing, err := reader.ReadByte(); err == nil || !errors.Is(err, io.EOF) {
		clearSecret(password)
		clearSecret(confirmation)
		if err == nil {
			_ = trailing
		}
		return nil, nil, fmt.Errorf("password input contains trailing data")
	}
	return password, confirmation, nil
}

func readPasswordLine(reader *bufio.Reader) ([]byte, error) {
	value, err := reader.ReadBytes('\n')
	if err != nil && len(value) == 0 {
		return nil, fmt.Errorf("two password input lines are required")
	}
	if err != nil && !errors.Is(err, io.EOF) {
		clearSecret(value)
		return nil, fmt.Errorf("password input failed")
	}
	value = bytesTrimLineEnding(value)
	if len(value) < access.MinPasswordBytes || len(value) > access.MaxPasswordBytes {
		clearSecret(value)
		return nil, fmt.Errorf("password input length or content is invalid")
	}
	return value, nil
}

func bytesTrimLineEnding(value []byte) []byte {
	if len(value) > 0 && value[len(value)-1] == '\n' {
		value = value[:len(value)-1]
	}
	if len(value) > 0 && value[len(value)-1] == '\r' {
		value = value[:len(value)-1]
	}
	return value
}

func clearSecret(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func accessUsage() error {
	return fmt.Errorf("usage: xinsightctl access hash-password|generate-subject|validate")
}

func runFeedCredential(ctx context.Context, engine *hostctl.Engine, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return feedCredentialUsage()
	}
	switch args[0] {
	case "prepare":
		flags := flag.NewFlagSet("feed-credential prepare", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		expectedOperationID := flags.String(
			"expected-active-operation-id",
			"",
			"exact current active credential operation identifier for rotation",
		)
		expectedVerifier := flags.String(
			"expected-active-verifier-sha256",
			"",
			"exact current active credential verifier for rotation",
		)
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return fmt.Errorf(
				"usage: xinsightctl feed-credential prepare [--expected-active-operation-id ID --expected-active-verifier-sha256 HEX]",
			)
		}
		registration, err := engine.PrepareFeedCredential(ctx, *expectedOperationID, *expectedVerifier)
		return writeResult(stdout, registration, err)
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("usage: xinsightctl feed-credential status")
		}
		report, err := engine.FeedCredentialStatus(ctx)
		return writeResult(stdout, report, err)
	case "activate", "abort":
		flags := flag.NewFlagSet("feed-credential "+args[0], flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		operationID := flags.String("operation-id", "", "exact pending credential operation identifier")
		verifier := flags.String("verifier-sha256", "", "exact pending credential verifier")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *operationID == "" || *verifier == "" {
			return fmt.Errorf(
				"usage: xinsightctl feed-credential %s --operation-id ID --verifier-sha256 HEX",
				args[0],
			)
		}
		if args[0] == "activate" {
			report, err := engine.ActivateFeedCredential(ctx, *operationID, *verifier)
			return writeResult(stdout, report, err)
		}
		report, err := engine.AbortFeedCredential(ctx, *operationID, *verifier)
		return writeResult(stdout, report, err)
	default:
		return feedCredentialUsage()
	}
}

func feedCredentialUsage() error {
	return fmt.Errorf("usage: xinsightctl feed-credential prepare|status|activate|abort")
}

func runConnectorEpoch(
	ctx context.Context,
	engine *hostctl.Engine,
	args []string,
	stdout io.Writer,
) error {
	if len(args) == 0 {
		return connectorEpochUsage()
	}
	switch args[0] {
	case "prepare":
		flags := flag.NewFlagSet("connector-epoch prepare", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		requestFile := flags.String("request-file", "", "root-owned 0400/0600 epoch request JSON")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *requestFile == "" {
			return connectorEpochUsage()
		}
		input, err := readCheckpointEpochPrepareFile(*requestFile)
		if err != nil {
			return err
		}
		report, err := engine.PrepareCheckpointEpoch(ctx, input)
		return writeResult(stdout, report, err)
	case "status":
		flags := flag.NewFlagSet("connector-epoch status", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		operationID := flags.String("operation-id", "", "exact checkpoint epoch operation")
		sourceSystemID := flags.String("source-system-id", "", "exact product-local source identity")
		adapterManifestID := flags.String("adapter-manifest-id", "", "exact product-local adapter identity")
		cursorKey := flags.String("cursor-key", "", "durable Connector cursor key")
		workerUnit := flags.String("worker-unit", "", "isolated Connector worker systemd unit")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return connectorEpochUsage()
		}
		var binding *checkpointepoch.Binding
		if *sourceSystemID != "" || *adapterManifestID != "" || *cursorKey != "" {
			binding = &checkpointepoch.Binding{
				SourceSystemID: *sourceSystemID, AdapterManifestID: *adapterManifestID, CursorKey: *cursorKey,
			}
		}
		report, err := engine.CheckpointEpochStatus(ctx, binding, *operationID, *workerUnit)
		return writeResult(stdout, report, err)
	case "activate", "abort":
		flags := flag.NewFlagSet("connector-epoch "+args[0], flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		operationID := flags.String("operation-id", "", "exact checkpoint epoch operation")
		planSHA256 := flags.String("plan-sha256", "", "exact prepared plan digest")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *operationID == "" || *planSHA256 == "" {
			return connectorEpochUsage()
		}
		if args[0] == "activate" {
			report, err := engine.ActivateCheckpointEpoch(ctx, *operationID, *planSHA256)
			return writeResult(stdout, report, err)
		}
		report, err := engine.AbortCheckpointEpoch(ctx, *operationID, *planSHA256)
		return writeResult(stdout, report, err)
	default:
		return connectorEpochUsage()
	}
}

func connectorEpochUsage() error {
	return fmt.Errorf("usage: xinsightctl connector-epoch prepare --request-file ABSOLUTE_PATH | status | activate | abort")
}

func runCleanBaseline(
	ctx context.Context,
	engine *hostctl.Engine,
	args []string,
	stdout io.Writer,
) error {
	if len(args) == 0 {
		return cleanBaselineUsage()
	}
	switch args[0] {
	case "data-reset-preview", "data-reset-apply":
		flags := flag.NewFlagSet("clean-baseline "+args[0], flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		backup := flags.String("rollback-backup", "", "fresh schema-current PostgreSQL custom backup")
		backupSHA256 := flags.String("rollback-backup-sha256", "", "exact rollback backup SHA-256")
		newDatabase := flags.String("new-database", "", "unused xinsight_clean_* database name")
		previewSHA256 := flags.String("preview-sha256", "", "exact authorized DATA_RESET preview digest")
		reasonCode := flags.String("reason-code", "", "second_integration_data_reset")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *backup == "" || *backupSHA256 == "" || *newDatabase == "" {
			return cleanBaselineUsage()
		}
		if args[0] == "data-reset-preview" {
			if *previewSHA256 != "" || *reasonCode != "" {
				return cleanBaselineUsage()
			}
			report, err := engine.PreviewCleanBaseline(ctx, *backup, *backupSHA256, *newDatabase)
			return writeResult(stdout, report, err)
		}
		if *previewSHA256 == "" || *reasonCode == "" {
			return cleanBaselineUsage()
		}
		receipt, err := engine.ApplyCleanBaselineDataReset(
			ctx, *backup, *backupSHA256, *newDatabase, *previewSHA256, *reasonCode,
		)
		return writeResult(stdout, receipt, err)
	case "checkpoint-bootstrap-preview", "checkpoint-bootstrap-apply":
		flags := flag.NewFlagSet("clean-baseline "+args[0], flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		dataResetOperationID := flags.String("data-reset-operation-id", "", "completed DATA_RESET operation identity")
		dataResetReceiptSHA256 := flags.String("data-reset-receipt-sha256", "", "exact DATA_RESET durable receipt SHA-256")
		requestFile := flags.String("checkpoint-request-file", "", "root-owned 0400/0600 empty checkpoint request")
		previewSHA256 := flags.String("preview-sha256", "", "exact authorized checkpoint bootstrap preview digest")
		reasonCode := flags.String("reason-code", "", "second_integration_checkpoint_bootstrap")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *dataResetOperationID == "" ||
			*dataResetReceiptSHA256 == "" || *requestFile == "" {
			return cleanBaselineUsage()
		}
		input, err := readCheckpointEpochPrepareFile(*requestFile)
		if err != nil {
			return err
		}
		if args[0] == "checkpoint-bootstrap-preview" {
			if *previewSHA256 != "" || *reasonCode != "" {
				return cleanBaselineUsage()
			}
			report, err := engine.PreviewCheckpointBootstrap(
				ctx, *dataResetOperationID, *dataResetReceiptSHA256, input,
			)
			return writeResult(stdout, report, err)
		}
		if *previewSHA256 == "" || *reasonCode == "" {
			return cleanBaselineUsage()
		}
		receipt, err := engine.ApplyCheckpointBootstrap(
			ctx, *dataResetOperationID, *dataResetReceiptSHA256, input,
			*previewSHA256, *reasonCode,
		)
		return writeResult(stdout, receipt, err)
	default:
		return cleanBaselineUsage()
	}
}

func runDatabaseAdminCredential(
	ctx context.Context,
	engine *hostctl.Engine,
	args []string,
	stdout io.Writer,
) error {
	if len(args) == 0 {
		return databaseAdminCredentialUsage()
	}
	switch args[0] {
	case "preview":
		flags := flag.NewFlagSet("database-admin-credential preview", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		authorizationRef := flags.String("authorization-ref", "", "exact authorization decision reference")
		reasonCode := flags.String("reason-code", "", "local_tooling_exposure")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *authorizationRef == "" || *reasonCode == "" {
			return databaseAdminCredentialUsage()
		}
		report, err := engine.PreviewAdminCredentialRotation(ctx, *authorizationRef, *reasonCode)
		return writeResult(stdout, report, err)
	case "apply":
		flags := flag.NewFlagSet("database-admin-credential apply", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		requestFile := flags.String("request-file", "", "root-owned 0400/0600 rotation request")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *requestFile == "" {
			return databaseAdminCredentialUsage()
		}
		request, err := readAdminCredentialRotationRequestFile(*requestFile)
		if err != nil {
			return err
		}
		receipt, err := engine.ApplyAdminCredentialRotation(ctx, request)
		return writeResult(stdout, receipt, err)
	default:
		return databaseAdminCredentialUsage()
	}
}

func databaseAdminCredentialUsage() error {
	return fmt.Errorf("usage: xinsightctl database-admin-credential preview --authorization-ref REF --reason-code local_tooling_exposure | database-admin-credential apply --request-file ABSOLUTE_PATH")
}

func readAdminCredentialRotationRequestFile(path string) (hostctl.AdminCredentialRotationRequest, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return hostctl.AdminCredentialRotationRequest{}, fmt.Errorf("admin credential rotation request path must be clean and absolute")
	}
	before, err := os.Lstat(path)
	if err != nil {
		return hostctl.AdminCredentialRotationRequest{}, fmt.Errorf("inspect admin credential rotation request file")
	}
	if err := validateProtectedRequestFileMetadata(before, hostctl.AdminCredentialRotationRequestMaxBytes()); err != nil {
		return hostctl.AdminCredentialRotationRequest{}, err
	}
	descriptor, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return hostctl.AdminCredentialRotationRequest{}, fmt.Errorf("open admin credential rotation request safely")
	}
	file := os.NewFile(uintptr(descriptor), filepath.Base(path))
	if file == nil {
		_ = syscall.Close(descriptor)
		return hostctl.AdminCredentialRotationRequest{}, fmt.Errorf("open admin credential rotation request handle")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return hostctl.AdminCredentialRotationRequest{}, fmt.Errorf("admin credential rotation request changed while opening")
	}
	if err := validateProtectedRequestFileMetadata(after, hostctl.AdminCredentialRotationRequestMaxBytes()); err != nil {
		return hostctl.AdminCredentialRotationRequest{}, err
	}
	payload, err := io.ReadAll(io.LimitReader(file, hostctl.AdminCredentialRotationRequestMaxBytes()+1))
	if err != nil || len(payload) == 0 || int64(len(payload)) > hostctl.AdminCredentialRotationRequestMaxBytes() {
		return hostctl.AdminCredentialRotationRequest{}, fmt.Errorf("admin credential rotation request file size or read is invalid")
	}
	request, decodeErr := hostctl.DecodeAdminCredentialRotationRequest(payload)
	for index := range payload {
		payload[index] = 0
	}
	return request, decodeErr
}

func validateProtectedRequestFileMetadata(info os.FileInfo, maxBytes int64) error {
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		(info.Mode().Perm() != 0o400 && info.Mode().Perm() != 0o600) ||
		info.Size() <= 0 || info.Size() > maxBytes ||
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("protected request file metadata is invalid")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() || int(stat.Gid) != os.Getegid() || uint64(stat.Nlink) != 1 {
		return fmt.Errorf("protected request file ownership or link count is invalid")
	}
	return nil
}

func cleanBaselineUsage() error {
	return fmt.Errorf("usage: xinsightctl clean-baseline data-reset-preview --rollback-backup FILE --rollback-backup-sha256 SHA256 --new-database NAME | clean-baseline data-reset-apply [same flags] --preview-sha256 SHA256 --reason-code second_integration_data_reset | clean-baseline checkpoint-bootstrap-preview --data-reset-operation-id ID --data-reset-receipt-sha256 SHA256 --checkpoint-request-file FILE | clean-baseline checkpoint-bootstrap-apply [same flags] --preview-sha256 SHA256 --reason-code second_integration_checkpoint_bootstrap")
}

const checkpointEpochPrepareFileMaxBytes = int64(1 << 20)

func readCheckpointEpochPrepareFile(path string) (checkpointepoch.PrepareInput, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return checkpointepoch.PrepareInput{}, fmt.Errorf("checkpoint epoch request path must be clean and absolute")
	}
	before, err := os.Lstat(path)
	if err != nil {
		return checkpointepoch.PrepareInput{}, fmt.Errorf("inspect checkpoint epoch request file")
	}
	if err := validateCheckpointEpochPrepareFileMetadata(before); err != nil {
		return checkpointepoch.PrepareInput{}, err
	}
	descriptor, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return checkpointepoch.PrepareInput{}, fmt.Errorf("open checkpoint epoch request file safely")
	}
	file := os.NewFile(uintptr(descriptor), filepath.Base(path))
	if file == nil {
		_ = syscall.Close(descriptor)
		return checkpointepoch.PrepareInput{}, fmt.Errorf("open checkpoint epoch request file handle")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return checkpointepoch.PrepareInput{}, fmt.Errorf("checkpoint epoch request file changed while opening")
	}
	if err := validateCheckpointEpochPrepareFileMetadata(after); err != nil {
		return checkpointepoch.PrepareInput{}, err
	}
	payload, err := io.ReadAll(io.LimitReader(file, checkpointEpochPrepareFileMaxBytes+1))
	if err != nil || len(payload) == 0 || int64(len(payload)) > checkpointEpochPrepareFileMaxBytes {
		return checkpointepoch.PrepareInput{}, fmt.Errorf("checkpoint epoch request file size or read is invalid")
	}
	input, err := checkpointepoch.DecodePrepareRequest(payload)
	for index := range payload {
		payload[index] = 0
	}
	if err != nil {
		return checkpointepoch.PrepareInput{}, err
	}
	return input, nil
}

func validateCheckpointEpochPrepareFileMetadata(info os.FileInfo) error {
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		(info.Mode().Perm() != 0o400 && info.Mode().Perm() != 0o600) ||
		info.Size() <= 0 || info.Size() > checkpointEpochPrepareFileMaxBytes ||
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("checkpoint epoch request file metadata is invalid")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() || int(stat.Gid) != os.Getegid() || uint64(stat.Nlink) != 1 {
		return fmt.Errorf("checkpoint epoch request file ownership or link count is invalid")
	}
	return nil
}

func runAdopt(ctx context.Context, engine *hostctl.Engine, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("adopt", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	artifact := flags.String("artifact", "", "signed target release artifact")
	legacyArtifact := flags.String("legacy-artifact", "", "exact allowlisted legacy release artifact")
	check := flags.Bool("check", false, "run read-only preflight")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *artifact == "" || *legacyArtifact == "" {
		return fmt.Errorf("usage: xinsightctl adopt [--check] --artifact FILE --legacy-artifact FILE")
	}
	if *check {
		report, err := engine.CheckAdopt(ctx, *artifact, *legacyArtifact)
		return writeResult(stdout, report, err)
	}
	receipt, err := engine.Adopt(ctx, *artifact, *legacyArtifact)
	return writeResult(stdout, receipt, err)
}

func runRollback(ctx context.Context, engine *hostctl.Engine, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("rollback", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	previous := flags.Bool("previous", false, "reactivate exact previous release")
	check := flags.Bool("check", false, "run read-only preflight")
	reason := flags.String("reason-code", "", "failed_readiness, field_validation, or operator_request")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || !*previous {
		return fmt.Errorf("usage: xinsightctl rollback [--check] --previous [--reason-code CODE]")
	}
	if *check {
		report, err := engine.CheckRollback(ctx)
		return writeResult(stdout, report, err)
	}
	receipt, err := engine.Rollback(ctx, *reason)
	return writeResult(stdout, receipt, err)
}

func runInstall(ctx context.Context, engine *hostctl.Engine, args []string, stdout io.Writer) error {
	f := flag.NewFlagSet("install", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	artifact := f.String("artifact", "", "signed release artifact")
	check := f.Bool("check", false, "read-only preflight")
	recovery := f.String("recovery-bundle", "", "private complete backup for original body key preparation only")
	hash := f.String("recovery-bundle-sha256", "", "exact private bundle SHA256")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 || *artifact == "" || (*recovery == "") != (*hash == "") {
		return fmt.Errorf("install requires artifact and paired recovery-bundle flags")
	}
	if *check {
		r, err := engine.CheckInstallWithRecovery(ctx, *artifact, *recovery, *hash)
		return writeResult(stdout, r, err)
	}
	r, err := engine.InstallWithRecovery(ctx, *artifact, *recovery, *hash)
	return writeResult(stdout, r, err)
}
func runBackup(ctx context.Context, engine *hostctl.Engine, args []string, stdout io.Writer) error {
	f := flag.NewFlagSet("backup", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	output := f.String("output", "", "exact new owner-only private backup path")
	check := f.Bool("check", false, "read-only preflight")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 || *output == "" {
		return fmt.Errorf("backup requires --output EXACT")
	}
	if *check {
		r, err := engine.CheckBackup(ctx, *output)
		return writeResult(stdout, r, err)
	}
	r, err := engine.Backup(ctx, *output)
	return writeResult(stdout, r, err)
}

func runRestore(ctx context.Context, engine *hostctl.Engine, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("restore", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	backup := flags.String("backup", "", "exact PostgreSQL custom-format backup path")
	backupSHA256 := flags.String("backup-sha256", "", "exact sha256: backup digest")
	bundle := flags.String("recovery-bundle", "", "private complete backup path")
	bundleHash := flags.String("recovery-bundle-sha256", "", "exact complete backup SHA256")
	newDatabase := flags.String("new-database", "", "new xinsight_restore_* database name")
	check := flags.Bool("check", false, "run read-only preflight")
	reason := flags.String("reason-code", "", "database_recovery, field_validation, or operator_request")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *newDatabase == "" || (*backup == "") != (*backupSHA256 == "") || (*bundle == "") != (*bundleHash == "") || (*backup == "") == (*bundle == "") {
		return fmt.Errorf("usage: xinsightctl restore [--check] --backup FILE --backup-sha256 SHA256 --new-database NAME [--reason-code CODE]")
	}
	if *bundle != "" {
		if *check {
			r, err := engine.CheckRestoreBundle(ctx, *bundle, *bundleHash, *newDatabase)
			return writeResult(stdout, r, err)
		}
		r, err := engine.RestoreBundle(ctx, *bundle, *bundleHash, *newDatabase, *reason)
		return writeResult(stdout, r, err)
	}
	if *check {
		report, err := engine.CheckRestore(ctx, *backup, *backupSHA256, *newDatabase)
		return writeResult(stdout, report, err)
	}
	receipt, err := engine.Restore(ctx, *backup, *backupSHA256, *newDatabase, *reason)
	return writeResult(stdout, receipt, err)
}

func artifactFlags(name string, args []string) (string, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	artifact := flags.String("artifact", "", "signed release artifact")
	if err := flags.Parse(args); err != nil {
		return "", err
	}
	if flags.NArg() != 0 || *artifact == "" {
		return "", fmt.Errorf("usage: xinsightctl %s --artifact FILE", name)
	}
	return *artifact, nil
}

func artifactCheckFlags(name string, args []string) (string, bool, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	artifact := flags.String("artifact", "", "signed release artifact")
	check := flags.Bool("check", false, "run read-only preflight")
	if err := flags.Parse(args); err != nil {
		return "", false, err
	}
	if flags.NArg() != 0 || *artifact == "" {
		return "", false, fmt.Errorf("usage: xinsightctl %s [--check] --artifact FILE", name)
	}
	return *artifact, *check, nil
}

func writeResult(writer io.Writer, value any, err error) error {
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func usage() error {
	return fmt.Errorf("usage: xinsightctl version|access|workbench-access|source-facts|query|service-identity|discovery|connector-controller|connector-target-config|review-body-config|verify|status|feed-credential|connector-epoch|clean-baseline|database-admin-credential|schema-contract|install|adopt|upgrade|upgrade-direct-fpv9|rollback|backup|restore|recover|resume|abort")
}
