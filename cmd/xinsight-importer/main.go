package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	litellmadapter "github.com/foigent/x-insight/internal/adapters/litellm"
	"github.com/foigent/x-insight/internal/adapters/openinference"
	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/connectorsync"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/evidence"
	"github.com/foigent/x-insight/internal/exchange"
	"github.com/foigent/x-insight/internal/integration/litellmfeed"
	"github.com/foigent/x-insight/internal/integration/openinferenceoffline"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/investigation"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "xinsight-importer: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if handled, err := buildinfo.HandleArgs(args, os.Stdout); handled {
		return err
	}
	if len(args) > 0 && args[0] == "import-resource-actions" {
		return runResourceActions(args[1:])
	}
	if len(args) > 0 && args[0] == "import-synthetic-resources" {
		return runSyntheticResources(args[1:])
	}
	if len(args) < 2 {
		return usage()
	}

	switch args[0] {
	case "fixtures":
		if args[1] != "run" {
			return usage()
		}
		return runFixtures(args[2:])
	case "unirouter":
		switch args[1] {
		case "discover":
			return runUniRouterDiscover(args[2:])
		case "preflight-sanitized":
			return runUniRouterPreflightSanitized(args[2:])
		case "import-snapshot":
			return runUniRouterImportSnapshot(args[2:])
		case "import-evidence-v2":
			return runUniRouterImportEvidenceV2(args[2:])
		case "import-campaign-package":
			return runUniRouterImportCampaignPackage(args[2:])
		case "gate-a-check":
			return runUniRouterGateACheck(args[2:])
		case "pull-feed":
			return runUniRouterPullFeed(args[2:])
		case "pull-source-facts":
			return runSourceFacts(args[2:])
		case "run-source-facts":
			return runManagedSourceFacts(args[2:], true)
		case "import-budgets":
			return runOfflineBudgets(args[2:])
		case "import-source-facts":
			return runOfflineSourceFacts(args[2:])
		default:
			return usage()
		}
	case "litellm":
		if args[1] != "pull-spend-logs" {
			return usage()
		}
		return runLiteLLMPullSpendLogs(args[2:])
	case "openinference":
		if args[1] != "import-bundle" {
			return usage()
		}
		return runOpenInferenceImportBundle(args[2:])
	default:
		return usage()
	}
}

const maxOpenInferenceBundleBytes = int64(8 << 20)

func runOpenInferenceImportBundle(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runOpenInferenceImportBundleContext(ctx, args, os.Stdout)
}

func runOpenInferenceImportBundleContext(
	ctx context.Context,
	args []string,
	output io.Writer,
) error {
	fs := flag.NewFlagSet("openinference import-bundle", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	authorizationPath := fs.String("authorization-file", "", "absolute root-owned expiring authorization JSON path")
	targetRef := fs.String("target-ref", "", "exact non-production target reference")
	bundlePath := fs.String("bundle", "", "absolute bounded OTLP JSON export path")
	operationID := fs.String("operation-id", "", "stable operator-generated import operation ID")
	bundleID := fs.String("bundle-id", "", "stable source bundle ID")
	producerInstance := fs.String("producer-instance", "", "stable OpenInference producer instance")
	tenantRefValue := fs.String("tenant-ref", "", "optional tenant reference")
	orgRef := fs.String("org-ref", "", "organization reference")
	projectRef := fs.String("project-ref", "", "project reference")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("openinference import-bundle does not accept positional arguments")
	}
	for name, value := range map[string]string{
		"--authorization-file": *authorizationPath,
		"--target-ref":         *targetRef,
		"--bundle":             *bundlePath,
		"--operation-id":       *operationID,
		"--bundle-id":          *bundleID,
		"--producer-instance":  *producerInstance,
		"--org-ref":            *orgRef,
		"--project-ref":        *projectRef,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	var tenantRef *string
	fs.Visit(func(candidate *flag.Flag) {
		if candidate.Name == "tenant-ref" {
			tenantRef = tenantRefValue
		}
	})
	product := buildinfo.Current()
	authorization, _, err := openinferenceoffline.LoadBoundedOfflineAuthorization(
		*authorizationPath,
		*targetRef,
		product.ProductVersion,
		product.Commit,
		time.Now().UTC(),
	)
	if err != nil {
		return err
	}
	authorizedRequest := openinferenceoffline.AuthorizedImportRequest{
		TargetRef:        *targetRef,
		BundlePath:       *bundlePath,
		BundleID:         *bundleID,
		OperationID:      *operationID,
		ProducerInstance: *producerInstance,
		TenantRef:        tenantRef,
		OrgRef:           *orgRef,
		ProjectRef:       *projectRef,
	}
	if err := authorization.ValidateImportBinding(authorizedRequest); err != nil {
		return err
	}
	payload, err := readOpenInferenceBundle(*bundlePath)
	if err != nil {
		return err
	}
	if err := openinferenceoffline.ValidateMetadataOnlyOTLP(payload); err != nil {
		return err
	}
	sanitized, err := openinference.SanitizeOTLPExport(payload)
	if err != nil {
		return fmt.Errorf("sanitize authorized bounded OpenInference bundle: %w", err)
	}
	bundleDigest := sha256.Sum256(payload)
	authorizedRequest.BundleSHA256 = "sha256:" + hex.EncodeToString(bundleDigest[:])
	authorizedRequest.RecordCount = len(sanitized.Records)
	if err := authorization.ValidateImport(authorizedRequest); err != nil {
		return err
	}
	importerDSN, err := config.LoadImporterPostgresDSN()
	if err != nil {
		return err
	}
	database, err := sql.Open("pgx", importerDSN)
	if err != nil {
		return fmt.Errorf("open OpenInference importer database: %w", err)
	}
	defer database.Close()
	database.SetMaxOpenConns(2)
	database.SetMaxIdleConns(1)
	database.SetConnMaxLifetime(30 * time.Minute)
	database.SetConnMaxIdleTime(5 * time.Minute)
	pingContext, cancelPing := context.WithTimeout(ctx, 10*time.Second)
	defer cancelPing()
	if err := database.PingContext(pingContext); err != nil {
		return fmt.Errorf("connect OpenInference importer database: %w", err)
	}
	store, err := openinferenceoffline.NewPostgresStore(database)
	if err != nil {
		return err
	}
	committer, err := openinferenceoffline.NewEvidenceCommitter(db.NewEvidenceStore(database))
	if err != nil {
		return err
	}
	projector, err := openinference.NewProjector()
	if err != nil {
		return fmt.Errorf("initialize OpenInference projector: %w", err)
	}
	importer, err := openinferenceoffline.NewImporter(
		projector,
		store,
		committer,
		store,
		nil,
	)
	if err != nil {
		return err
	}
	receipt, err := importer.ImportBundle(ctx, payload, openinferenceoffline.ImportRequest{
		OperationID:      *operationID,
		BundleID:         *bundleID,
		ProducerInstance: *producerInstance,
		TenantRef:        tenantRef,
		OrgRef:           *orgRef,
		ProjectRef:       *projectRef,
	})
	if err != nil {
		return err
	}
	canonicalReceipt, err := receipt.CanonicalBytes()
	if err != nil {
		return fmt.Errorf("encode OpenInference operational receipt: %w", err)
	}
	if output == nil {
		output = io.Discard
	}
	if _, err := output.Write(append(canonicalReceipt, '\n')); err != nil {
		return fmt.Errorf("write OpenInference operational receipt: %w", err)
	}
	return nil
}

func readOpenInferenceBundle(path string) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("--bundle must be an absolute clean path")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open OpenInference bundle: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect OpenInference bundle: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxOpenInferenceBundleBytes {
		return nil, fmt.Errorf("OpenInference bundle must be a regular file between 1 and %d bytes", maxOpenInferenceBundleBytes)
	}
	payload, err := io.ReadAll(io.LimitReader(file, maxOpenInferenceBundleBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read OpenInference bundle: %w", err)
	}
	if int64(len(payload)) != info.Size() || int64(len(payload)) > maxOpenInferenceBundleBytes {
		return nil, fmt.Errorf("OpenInference bundle changed while it was read")
	}
	return payload, nil
}

func runLiteLLMPullSpendLogs(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("litellm pull-spend-logs does not accept command-line arguments")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runLiteLLMPullSpendLogsContext(ctx, os.Stdout)
}

func runLiteLLMPullSpendLogsContext(ctx context.Context, output io.Writer) error {
	runtimeConfig, err := config.LoadLiteLLMFeedRuntimeConfig()
	if err != nil {
		return err
	}
	apiKey, err := config.LoadLiteLLMFeedAPIKey()
	if err != nil {
		return err
	}
	importerDSN, err := config.LoadImporterPostgresDSN()
	if err != nil {
		return err
	}
	source, err := litellmfeed.NewHTTPSource(litellmfeed.HTTPSourceConfig{
		Endpoint: runtimeConfig.Endpoint,
		APIKey:   apiKey,
	})
	if err != nil {
		return err
	}
	database, err := sql.Open("pgx", importerDSN)
	if err != nil {
		return fmt.Errorf("open LiteLLM Connector state database: %w", err)
	}
	defer database.Close()
	database.SetMaxOpenConns(4)
	database.SetMaxIdleConns(2)
	database.SetConnMaxLifetime(30 * time.Minute)
	database.SetConnMaxIdleTime(5 * time.Minute)
	pingContext, cancelPing := context.WithTimeout(ctx, 10*time.Second)
	defer cancelPing()
	if err := database.PingContext(pingContext); err != nil {
		return fmt.Errorf("connect LiteLLM Connector state database: %w", err)
	}
	evidenceStore, err := db.OpenEvidenceStore(ctx, importerDSN)
	if err != nil {
		return fmt.Errorf("open LiteLLM EvidenceStore: %w", err)
	}
	defer evidenceStore.Close()
	stateStore, err := litellmfeed.NewStateStore(database)
	if err != nil {
		return err
	}
	// Reconcile the exact compiled registry contract on every process start,
	// including restarts with an existing durable Checkpoint. Otherwise a
	// coordinated source/adapter metadata drift could bypass the initial
	// activation gate and reach EvidenceStore under the fixed UUID pair.
	if err := stateStore.EnsureRegistry(
		ctx,
		runtimeConfig.SourceInstance,
	); err != nil {
		return fmt.Errorf("verify LiteLLM product registry: %w", err)
	}
	projector, err := litellmadapter.NewProjector()
	if err != nil {
		return fmt.Errorf("initialize LiteLLM projector: %w", err)
	}
	committer, err := litellmfeed.NewEvidenceCommitter(evidenceStore)
	if err != nil {
		return err
	}
	logger := newComponentJSONLogger(output, "litellm_spend_logs_pull")
	worker, err := litellmfeed.NewWorker(
		source,
		stateStore,
		projector,
		committer,
		litellmfeed.WorkerConfig{
			SourceInstance: runtimeConfig.SourceInstance,
			TenantRef:      runtimeConfig.TenantRef,
			OrgRef:         runtimeConfig.OrgRef,
			ProjectRef:     runtimeConfig.ProjectRef,
			PollInterval:   runtimeConfig.PollInterval,
			Logger:         logger.Log,
		},
	)
	if err != nil {
		return err
	}
	logger.Log("worker_starting", map[string]any{
		"connector_id":            litellmadapter.ConnectorID,
		"connector_version":       litellmadapter.ConnectorVersion,
		"protocol_version":        litellmadapter.ProtocolVersion,
		"protocol_release_digest": litellmadapter.ProtocolReleaseSHA,
		"projection_profile":      litellmadapter.ProjectionProfileID,
		"checkpoint_strategy":     "litellm_start_time_boundary_v1",
	})
	return worker.Run(ctx)
}

func runUniRouterPullFeed(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("unirouter pull-feed does not accept command-line arguments")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runUniRouterPullFeedContext(ctx, os.Stdout)
}

func runUniRouterPullFeedContext(ctx context.Context, output io.Writer) error {
	runtimeConfig, err := config.LoadUniRouterFeedRuntimeConfig()
	if err != nil {
		return err
	}
	serviceKey, err := config.LoadUniRouterFeedServiceKey()
	if err != nil {
		return err
	}
	importerDSN, err := config.LoadImporterPostgresDSN()
	if err != nil {
		return err
	}
	source, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{SyncRequired: true,
		Endpoint:               runtimeConfig.Endpoint,
		ServiceKey:             serviceKey,
		AllowPrivateTunnelHTTP: runtimeConfig.AllowPrivateTunnelHTTP,
	})
	if err != nil {
		return err
	}

	database, err := sql.Open("pgx", importerDSN)
	if err != nil {
		return fmt.Errorf("open feed worker state database: %w", err)
	}
	defer database.Close()
	database.SetMaxOpenConns(4)
	database.SetMaxIdleConns(2)
	database.SetConnMaxLifetime(30 * time.Minute)
	database.SetConnMaxIdleTime(5 * time.Minute)
	pingContext, cancelPing := context.WithTimeout(ctx, 10*time.Second)
	defer cancelPing()
	if err := database.PingContext(pingContext); err != nil {
		return fmt.Errorf("connect feed worker state database: %w", err)
	}

	evidenceStore, err := db.OpenEvidenceStore(ctx, importerDSN)
	if err != nil {
		return fmt.Errorf("open feed evidence store: %w", err)
	}
	defer evidenceStore.Close()
	stateStore, err := pullfeed.NewPostgresStateStore(
		database,
		unirouter.DefaultSourceSystemID,
		unirouter.DefaultCompletedExecutionAdapterManifestID,
	)
	if err != nil {
		return err
	}
	logger := newFeedWorkerJSONLogger(output)
	mode := pullfeed.ReferenceMappingMode(runtimeConfig.MappingMode)
	var projector pullfeed.ReferenceItemProjector
	if mode != pullfeed.ReferenceMappingLegacy {
		projector, err = unirouter.NewCompletedExecutionReferenceProjector()
		if err != nil {
			return fmt.Errorf("initialize completed-execution Reference projector: %w", err)
		}
	}
	committer, err := pullfeed.NewModeAwareEvidenceCommitter(
		evidenceStore,
		mode,
		projector,
		nil,
		logger.Log,
	)
	if err != nil {
		return err
	}
	worker, err := pullfeed.NewLiveWorker(source, stateStore, committer, pullfeed.LiveWorkerConfig{

		BeginActivation: func(ctx context.Context) (context.Context, func(error), error) {
			gate := connectorsync.New(database)
			id, e := gate.Receiver(ctx, serviceKey)
			if e != nil {
				return ctx, nil, e
			}
			return gate.Begin(ctx, id, "feed", runtimeConfig.PollInterval, "feed:init")
		},
		BeginSync: func(ctx context.Context, state pullfeed.PostgresWorkerState) (context.Context, func(error), error) {
			gate := connectorsync.New(database)
			id, e := gate.Receiver(ctx, serviceKey)
			if e != nil {
				return ctx, nil, e
			}
			return gate.Begin(ctx, id, "feed", runtimeConfig.PollInterval)
		},
		PollInterval:     runtimeConfig.PollInterval,
		AuthorizationRef: pullfeed.AcceptedLiveContractRef,
		Logger:           logger.Log,
	})
	if err != nil {
		return err
	}
	logger.Log("worker_starting", map[string]any{
		"contract":                 unirouter.CompletedExecutionSchemaV1,
		"mapping_mode":             runtimeConfig.MappingMode,
		"reference_profile":        unirouter.CompletedExecutionReferenceProfileID,
		"protocol_version":         unirouter.CompletedExecutionReferenceProtocolVersion,
		"protocol_release_digest":  unirouter.CompletedExecutionReferenceReleaseDigest,
		"observation_window_reset": true,
	})
	return worker.Run(ctx)
}

type feedWorkerJSONLogger struct {
	component string
	encoder   *json.Encoder
}

func newFeedWorkerJSONLogger(output io.Writer) *feedWorkerJSONLogger {
	return newComponentJSONLogger(output, "unirouter_completed_execution_pull")
}

func newComponentJSONLogger(
	output io.Writer,
	component string,
) *feedWorkerJSONLogger {
	if output == nil {
		output = io.Discard
	}
	return &feedWorkerJSONLogger{
		component: component,
		encoder:   json.NewEncoder(output),
	}
}

func (logger *feedWorkerJSONLogger) Log(event string, fields map[string]any) {
	if logger == nil || logger.encoder == nil {
		return
	}
	record := map[string]any{
		"component": logger.component,
		"event":     event,
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	}
	for key, value := range fields {
		record[key] = value
	}
	_ = logger.encoder.Encode(record)
}

func runUniRouterPreflightSanitized(args []string) error {
	fs := flag.NewFlagSet("unirouter preflight-sanitized", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	snapshot := fs.String("snapshot", "", "UniRouter PRD5 snapshot v2 JSON path")
	approvalManifest := fs.String("approval-manifest", "", "exact UniRouter approval manifest JSON path")
	maxRequestIDs := fs.Int("max-request-ids", unirouter.DefaultSanitizedPreflightMaxRequestIDs, "maximum approved request IDs")
	maxEvents := fs.Int("max-events", unirouter.DefaultSanitizedPreflightMaxEvents, "maximum call-log, trace-event, and capture-gap rows")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *snapshot == "" {
		return fmt.Errorf("--snapshot is required")
	}
	if *approvalManifest == "" {
		return fmt.Errorf("--approval-manifest is required")
	}

	report, err := unirouter.PreflightSanitizedSnapshot(*snapshot, *approvalManifest, unirouter.SanitizedPreflightOptions{
		MaxRequestIDs: *maxRequestIDs,
		MaxEvents:     *maxEvents,
	})
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return err
	}
	if report.Status != "pass" {
		return fmt.Errorf("sanitized snapshot preflight blocked with %d finding(s)", len(report.Findings))
	}
	return nil
}

func runFixtures(args []string) error {
	fs := flag.NewFlagSet("fixtures run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	schemaPath := fs.String("schema", filepath.Join("schemas", "m0", "evidence-event.schema.json"), "evidence event JSON schema path")
	fixturesDir := fs.String("fixtures", filepath.Join("fixtures", "m0"), "fixture directory")
	if err := fs.Parse(args); err != nil {
		return err
	}

	runner, err := evidence.NewFixtureRunner(*schemaPath)
	if err != nil {
		return err
	}
	report, err := runner.RunDir(*fixturesDir)
	if err != nil {
		return err
	}

	for _, result := range report.Results {
		status := "PASS"
		if !result.Pass {
			status = "FAIL"
		}
		fmt.Printf("%s %-18s expected=%s actual=%s reason=%s\n", status, result.Name, result.Expected, result.Actual, result.Reason)
	}
	fmt.Printf("summary total=%d passed=%d failed=%d\n", report.Total, report.Passed, report.Failed)

	if report.Failed > 0 {
		return fmt.Errorf("fixture conformance failed")
	}
	return nil
}

func runUniRouterDiscover(args []string) error {
	fs := flag.NewFlagSet("unirouter discover", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	dsn := fs.String("dsn", os.Getenv("UNIROUTER_POSTGRES_DSN"), "UniRouter read-only Postgres DSN")
	snapshot := fs.String("snapshot", "", "UniRouter PRD5 snapshot JSON path")
	sourceInstance := fs.String("source-instance", "unknown", "non-secret source instance label")
	includeCounts := fs.Bool("include-counts", false, "count source table rows when using --dsn")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var (
		report unirouter.DiscoveryReport
		err    error
	)
	switch {
	case *snapshot != "":
		report, err = unirouter.DiscoverSnapshot(*snapshot, *sourceInstance)
	case *dsn != "":
		report, err = unirouter.DiscoverPostgres(*dsn, *sourceInstance, *includeCounts)
	default:
		return fmt.Errorf("either --snapshot or --dsn/UNIROUTER_POSTGRES_DSN is required")
	}
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func runUniRouterImportSnapshot(args []string) error {
	fs := flag.NewFlagSet("unirouter import-snapshot", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	snapshot := fs.String("snapshot", "", "UniRouter PRD5 snapshot JSON path")
	approvalManifest := fs.String("approval-manifest", "", "optional exact manifest; enables bounded sanitized preflight and byte binding")
	schemaPath := fs.String("schema", filepath.Join("schemas", "m0", "evidence-event.schema.json"), "evidence event JSON schema path")
	sourceSystemID := fs.String("source-system-id", unirouter.DefaultSourceSystemID, "X-Insight source_system_id for local import validation")
	adapterManifestID := fs.String("adapter-manifest-id", "", "X-Insight adapter_manifest_id; defaults by snapshot schema version")
	sourceVersion := fs.String("source-version", "", "source version label; defaults to snapshot schema version")
	emitEvents := fs.Bool("emit-events", false, "include mapped evidence events in the JSON report")
	rerunCheck := fs.Bool("rerun-check", true, "ingest the mapped events a second time to verify duplicate_noop behavior")
	persist := fs.Bool("persist", false, "persist accepted mapped events into X-Insight Postgres")
	persistDSN := fs.String("persist-dsn", "", "development-only explicit X-Insight Postgres DSN used with --persist")
	proofClass := fs.String("proof-class", investigation.ProofSyntheticFixture, "import proof class: synthetic_fixture or operator_approved_integration")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *snapshot == "" {
		return fmt.Errorf("--snapshot is required")
	}
	if *proofClass == investigation.ProofOperatorApproved && *approvalManifest == "" {
		return fmt.Errorf("--approval-manifest is required for operator_approved_integration")
	}
	preflightArtifactSHA256 := ""
	if *approvalManifest != "" {
		preflight, err := unirouter.PreflightSanitizedSnapshot(*snapshot, *approvalManifest, unirouter.SanitizedPreflightOptions{})
		if err != nil {
			return err
		}
		if preflight.Status != "pass" {
			return fmt.Errorf("sanitized snapshot preflight blocked with %d finding(s)", len(preflight.Findings))
		}
		preflightArtifactSHA256 = preflight.ArtifactSHA256
	}

	report, err := unirouter.ImportSnapshot(*snapshot, *schemaPath, unirouter.SnapshotImportOptions{
		SourceSystemID:    *sourceSystemID,
		AdapterManifestID: *adapterManifestID,
		SourceVersion:     *sourceVersion,
		EmitEvents:        *emitEvents || *persist,
		RerunCheck:        *rerunCheck,
	})
	if err != nil {
		return err
	}
	if preflightArtifactSHA256 != "" && report.ArtifactSHA256 != preflightArtifactSHA256 {
		return fmt.Errorf("snapshot changed after sanitized preflight")
	}
	if *persist {
		resolvedDSN, err := resolvePersistenceDSN(*persistDSN)
		if err != nil {
			return err
		}
		if report.Rejected > 0 || report.Quarantined > 0 {
			return fmt.Errorf("refusing to persist snapshot with rejected=%d quarantined=%d", report.Rejected, report.Quarantined)
		}
		provenance, err := buildSnapshotImportProvenance(report.Snapshot, report.ArtifactSHA256, report.SourceSystemID, report.AdapterManifestID, *proofClass)
		if err != nil {
			return err
		}
		events := report.Events
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store, err := db.OpenEvidenceStore(ctx, resolvedDSN)
		if err != nil {
			return err
		}
		defer store.Close()
		persistReport, err := store.PersistEventsWithProvenance(ctx, events, provenance)
		if err != nil {
			return err
		}
		report.Persistence = &persistReport
	}
	if !*emitEvents {
		report.Events = nil
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func runUniRouterImportEvidenceV2(args []string) error {
	fs := flag.NewFlagSet("unirouter import-evidence-v2", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	bundlePath := fs.String("bundle", "", "UniRouter execution-evidence V2 bundle JSON path")
	schemaPath := fs.String("schema", filepath.Join("schemas", "m0", "evidence-event.schema.json"), "evidence event JSON schema path")
	sourceSystemID := fs.String("source-system-id", unirouter.DefaultSourceSystemID, "X-Insight source_system_id")
	adapterManifestID := fs.String("adapter-manifest-id", unirouter.DefaultExecutionEvidenceAdapterID, "digest-pinned execution-evidence adapter_manifest_id")
	emitEvents := fs.Bool("emit-events", false, "include mapped evidence events in the JSON report")
	rerunCheck := fs.Bool("rerun-check", true, "verify duplicate_noop behavior in memory")
	persist := fs.Bool("persist", false, "persist accepted mapped events into X-Insight Postgres")
	persistDSN := fs.String("persist-dsn", "", "development-only explicit X-Insight Postgres DSN used with --persist")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*bundlePath) == "" {
		return fmt.Errorf("--bundle is required")
	}
	report, err := unirouter.ImportExecutionEvidenceBundle(
		*bundlePath,
		*schemaPath,
		unirouter.SnapshotImportOptions{
			SourceSystemID:    *sourceSystemID,
			AdapterManifestID: *adapterManifestID,
			EmitEvents:        *emitEvents || *persist,
			RerunCheck:        *rerunCheck,
		},
	)
	if err != nil {
		return err
	}
	if *persist {
		if report.Rejected > 0 || report.Quarantined > 0 {
			return fmt.Errorf("refusing to persist bundle with rejected=%d quarantined=%d", report.Rejected, report.Quarantined)
		}
		resolvedDSN, err := resolvePersistenceDSN(*persistDSN)
		if err != nil {
			return err
		}
		provenance, err := buildExecutionEvidenceImportProvenance(
			report.Bundle,
			report.ArtifactSHA256,
			report.SourceSystemID,
			report.AdapterManifestID,
		)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store, err := db.OpenEvidenceStore(ctx, resolvedDSN)
		if err != nil {
			return err
		}
		defer store.Close()
		persistReport, err := store.PersistEventsWithProvenance(ctx, report.Events, provenance)
		if err != nil {
			return err
		}
		report.Persistence = &persistReport
	}
	if !*emitEvents {
		report.Events = nil
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

type gateACheckReport struct {
	SnapshotPath    string                         `json:"snapshot_path"`
	Source          string                         `json:"source"`
	SourceInstance  string                         `json:"source_instance"`
	RequestIDs      []string                       `json:"request_ids"`
	Discovery       unirouter.DiscoveryReport      `json:"discovery"`
	Import          unirouter.SnapshotImportReport `json:"import"`
	Persistence     *db.PersistenceReport          `json:"persistence,omitempty"`
	Readback        []gateAReadback                `json:"readback,omitempty"`
	Receipt         exchange.ImportReceipt         `json:"receipt"`
	Status          string                         `json:"status"`
	RemainingInputs []string                       `json:"remaining_inputs,omitempty"`
}

type gateAReadback struct {
	RequestID         string   `json:"request_id"`
	EventCount        int      `json:"event_count"`
	GapCount          int      `json:"gap_count"`
	EventTypes        []string `json:"event_types"`
	SourceEventTypes  []string `json:"source_event_types,omitempty"`
	GapTypes          []string `json:"gap_types,omitempty"`
	CandidateGapTypes []string `json:"candidate_gap_types,omitempty"`
}

func runUniRouterGateACheck(args []string) error {
	fs := flag.NewFlagSet("unirouter gate-a-check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	snapshot := fs.String("snapshot", "", "UniRouter PRD5 snapshot JSON path")
	approvalManifest := fs.String("approval-manifest", "", "optional exact manifest; enables bounded sanitized preflight and byte binding")
	schemaPath := fs.String("schema", filepath.Join("schemas", "m0", "evidence-event.schema.json"), "evidence event JSON schema path")
	sourceSystemID := fs.String("source-system-id", unirouter.DefaultSourceSystemID, "X-Insight source_system_id")
	adapterManifestID := fs.String("adapter-manifest-id", "", "X-Insight adapter_manifest_id; defaults by snapshot schema version")
	sourceVersion := fs.String("source-version", "", "source version label; defaults to snapshot schema version")
	persist := fs.Bool("persist", false, "persist accepted mapped events into X-Insight Postgres and read them back")
	persistDSN := fs.String("persist-dsn", "", "development-only explicit X-Insight Postgres DSN used with --persist")
	readbackLimit := fs.Int("readback-limit", 200, "maximum events or gaps to read back per request")
	receiptOutput := fs.String("receipt-output", "", "optional path for a sanitized machine-readable import receipt; refuses to overwrite")
	receiptSchemaPath := fs.String("receipt-schema", filepath.Join("schemas", "integration", "import-receipt.schema.json"), "import receipt JSON schema path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *snapshot == "" {
		return fmt.Errorf("--snapshot is required")
	}
	preflightArtifactSHA256 := ""
	if *approvalManifest != "" {
		preflight, err := unirouter.PreflightSanitizedSnapshot(*snapshot, *approvalManifest, unirouter.SanitizedPreflightOptions{})
		if err != nil {
			return err
		}
		if preflight.Status != "pass" {
			return fmt.Errorf("sanitized snapshot preflight blocked with %d finding(s)", len(preflight.Findings))
		}
		preflightArtifactSHA256 = preflight.ArtifactSHA256
	}

	importReport, err := unirouter.ImportSnapshot(*snapshot, *schemaPath, unirouter.SnapshotImportOptions{
		SourceSystemID:    *sourceSystemID,
		AdapterManifestID: *adapterManifestID,
		SourceVersion:     *sourceVersion,
		EmitEvents:        *persist,
		RerunCheck:        true,
	})
	if err != nil {
		return err
	}
	if preflightArtifactSHA256 != "" && importReport.ArtifactSHA256 != preflightArtifactSHA256 {
		return fmt.Errorf("snapshot changed after sanitized preflight")
	}
	snapshotFile := importReport.Snapshot
	snapshotSHA256 := importReport.ArtifactSHA256
	discovery, err := unirouter.DiscoverSnapshotFile(snapshotFile, snapshotFile.SourceInstance)
	if err != nil {
		return err
	}
	preparedEvents := importReport.Events
	importReport.Events = nil
	report := gateACheckReport{
		SnapshotPath:    *snapshot,
		Source:          importReport.Source,
		SourceInstance:  importReport.SourceInstance,
		RequestIDs:      snapshotFile.RequestIDs,
		Discovery:       discovery,
		Import:          importReport,
		Status:          "validated_in_memory",
		RemainingInputs: []string{"run with --persist and a configured X-Insight Postgres DSN credential to verify Postgres readback"},
	}
	if importReport.Rejected > 0 || importReport.Quarantined > 0 {
		report.Status = "failed_import_validation"
		report.Receipt = buildGateAImportReceipt(report, snapshotFile, snapshotSHA256, time.Now().UTC())
		if err := emitGateACheckReport(report, *receiptOutput, *receiptSchemaPath); err != nil {
			return fmt.Errorf("snapshot import rejected=%d quarantined=%d; emit report: %w", importReport.Rejected, importReport.Quarantined, err)
		}
		return fmt.Errorf("snapshot import rejected=%d quarantined=%d", importReport.Rejected, importReport.Quarantined)
	}

	if *persist {
		resolvedDSN, err := resolvePersistenceDSN(*persistDSN)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store, err := db.OpenEvidenceStore(ctx, resolvedDSN)
		if err != nil {
			return err
		}
		defer store.Close()
		proofClass := gateAProofClass(preflightArtifactSHA256)
		provenance, err := buildSnapshotImportProvenance(snapshotFile, snapshotSHA256, importReport.SourceSystemID, importReport.AdapterManifestID, proofClass)
		if err != nil {
			return err
		}
		persistReport, err := store.PersistEventsWithProvenance(ctx, preparedEvents, provenance)
		if err != nil {
			return err
		}
		report.Persistence = &persistReport
		readback, err := readBackRequests(ctx, store, snapshotFile.RequestIDs, *readbackLimit)
		if err != nil {
			return err
		}
		report.Readback = readback
		report.Status = "persisted_and_read_back"
		report.RemainingInputs = nil
	}

	report.Receipt = buildGateAImportReceipt(report, snapshotFile, snapshotSHA256, time.Now().UTC())
	return emitGateACheckReport(report, *receiptOutput, *receiptSchemaPath)
}

func gateAProofClass(preflightArtifactSHA256 string) string {
	if preflightArtifactSHA256 != "" {
		return investigation.ProofOperatorApproved
	}
	return investigation.ProofSyntheticFixture
}

func resolvePersistenceDSN(explicit string) (string, error) {
	if explicit != "" {
		return config.ValidatePostgresDSN(explicit)
	}
	return config.LoadImporterPostgresDSN()
}

func buildGateAImportReceipt(report gateACheckReport, snapshot unirouter.SnapshotFile, snapshotSHA256 string, checkedAt time.Time) exchange.ImportReceipt {
	status := exchange.ReceiptStatusValidated
	if report.Status == "persisted_and_read_back" {
		status = exchange.ReceiptStatusPersisted
	}
	if report.Status == "failed_import_validation" {
		status = exchange.ReceiptStatusRejected
	}

	result := exchange.ReceiptResult{
		Status:             status,
		RequestIDs:         len(snapshot.RequestIDs),
		InputEvents:        report.Import.EventsTotal,
		Accepted:           report.Import.Accepted,
		DuplicateNoop:      report.Import.DuplicateNoop,
		Rejected:           report.Import.Rejected,
		Quarantined:        report.Import.Quarantined,
		RerunDuplicateNoop: report.Import.RerunDuplicateNoop,
		CaptureGapEvents:   report.Import.CaptureGapEvents,
	}
	if report.Persistence != nil {
		result.PersistedInserted = intPointer(report.Persistence.Inserted)
		result.PersistedDuplicateNoop = intPointer(report.Persistence.DuplicateNoop)
		result.PersistenceConflicts = intPointer(report.Persistence.IdempotencyConflict)
		readbackEvents, readbackGaps := readbackCounts(report.Readback)
		result.ReadbackEvents = intPointer(readbackEvents)
		result.ReadbackCaptureGaps = intPointer(readbackGaps)
	}

	warnings := make([]string, 0, 4)
	if snapshot.ExchangeID == "" {
		warnings = append(warnings, exchange.WarningSourceExchangeIDMissing)
	}
	if snapshot.ApprovalManifestSHA256 == "" {
		warnings = append(warnings, exchange.WarningApprovalManifestHashMissing)
	}
	if report.Persistence == nil && status != exchange.ReceiptStatusRejected {
		warnings = append(warnings, exchange.WarningPersistenceNotRequested)
	}
	if report.Import.CaptureGapEvents > 0 {
		warnings = append(warnings, exchange.WarningSourceCaptureGapsPresent)
	}
	sort.Strings(warnings)

	return exchange.ImportReceipt{
		SchemaVersion: exchange.ImportReceiptSchemaVersion,
		ExchangeID:    snapshot.ExchangeID,
		Source: exchange.ReceiptSource{
			System:   report.Source,
			Instance: report.SourceInstance,
		},
		Artifact: exchange.ReceiptArtifact{
			SchemaVersion:          snapshot.SchemaVersion,
			SHA256:                 snapshotSHA256,
			ApprovalManifestSHA256: snapshot.ApprovalManifestSHA256,
			GeneratedAt:            snapshot.GeneratedAt,
		},
		Result:       result,
		CheckedAt:    checkedAt.UTC().Format(time.RFC3339Nano),
		WarningCodes: warnings,
	}
}

func emitGateACheckReport(report gateACheckReport, receiptOutput string, receiptSchemaPath string) error {
	if err := validateImportReceipt(report.Receipt, receiptSchemaPath); err != nil {
		return err
	}
	if receiptOutput != "" {
		if receiptOutput == "-" {
			return fmt.Errorf("--receipt-output must be a file path because the full report uses stdout")
		}
		if err := writeJSONExclusive(receiptOutput, report.Receipt); err != nil {
			return err
		}
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func validateImportReceipt(receipt exchange.ImportReceipt, schemaPath string) error {
	data, err := json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("marshal import receipt: %w", err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("decode import receipt: %w", err)
	}
	validator, err := evidence.NewSchemaValidator(schemaPath)
	if err != nil {
		return err
	}
	if err := validator.Validate(value); err != nil {
		return fmt.Errorf("validate import receipt: %w", err)
	}
	return nil
}

func writeJSONExclusive(path string, value any) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create receipt output %s: %w", path, err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	encodeErr := encoder.Encode(value)
	closeErr := file.Close()
	if encodeErr != nil {
		_ = os.Remove(path)
		return fmt.Errorf("write receipt output %s: %w", path, encodeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close receipt output %s: %w", path, closeErr)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() > unirouter.MaxSnapshotBytes {
		return "", fmt.Errorf("snapshot exceeds %d bytes", unirouter.MaxSnapshotBytes)
	}

	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, unirouter.MaxSnapshotBytes+1))
	if err != nil {
		return "", err
	}
	if written > unirouter.MaxSnapshotBytes {
		return "", fmt.Errorf("snapshot exceeds %d bytes", unirouter.MaxSnapshotBytes)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func intPointer(value int) *int {
	return &value
}

func readbackCounts(readback []gateAReadback) (int, int) {
	events := 0
	gaps := 0
	for _, row := range readback {
		events += row.EventCount
		gaps += row.GapCount
	}
	return events, gaps
}

func buildSnapshotImportProvenance(snapshot unirouter.SnapshotFile, artifactSHA256 string, sourceSystemID string, adapterManifestID string, proofClass string) (investigation.ImportProvenance, error) {
	generatedAt, err := parseOptionalRFC3339(snapshot.GeneratedAt)
	if err != nil {
		return investigation.ImportProvenance{}, fmt.Errorf("snapshot generated_at: %w", err)
	}
	provenance := investigation.ImportProvenance{
		SourceSystemID:         sourceSystemID,
		AdapterManifestID:      adapterManifestID,
		ProofClass:             proofClass,
		SourceArtifactSHA256:   artifactSHA256,
		ApprovalManifestSHA256: snapshot.ApprovalManifestSHA256,
		ExchangeID:             snapshot.ExchangeID,
		SourceGeneratedAt:      generatedAt,
		SourceRevision:         snapshot.SchemaVersion,
		ImportedRevision:       artifactSHA256,
		SourceWatermark:        artifactSHA256,
		ImportWatermark:        artifactSHA256,
		TimelineComplete:       snapshotTimelineComplete(snapshot),
	}
	switch proofClass {
	case investigation.ProofSyntheticFixture:
		provenance.IntegrationMode = investigation.IntegrationFixture
		provenance.AssuranceMode = investigation.AssuranceSyntheticFixture
		provenance.ApprovalManifestSHA256 = ""
		provenance.ExchangeID = ""
	case investigation.ProofOperatorApproved:
		if snapshot.ExchangeID == "" || snapshot.ApprovalManifestSHA256 == "" {
			return investigation.ImportProvenance{}, fmt.Errorf("operator-approved import requires snapshot exchange_id and approval_manifest_sha256")
		}
		provenance.IntegrationMode = investigation.IntegrationSidecar
		provenance.AssuranceMode = investigation.AssuranceOperatorAttested
	default:
		return investigation.ImportProvenance{}, fmt.Errorf("unsupported --proof-class %q", proofClass)
	}
	return provenance, nil
}

func buildExecutionEvidenceImportProvenance(
	bundle unirouter.ExecutionEvidenceBundle,
	artifactSHA256 string,
	sourceSystemID string,
	adapterManifestID string,
) (investigation.ImportProvenance, error) {
	closedAt, err := parseOptionalRFC3339(bundle.ClosedAt)
	if err != nil {
		return investigation.ImportProvenance{}, fmt.Errorf("bundle closed_at: %w", err)
	}
	if closedAt == nil {
		return investigation.ImportProvenance{}, fmt.Errorf("bundle closed_at is required")
	}
	return investigation.ImportProvenance{
		SourceSystemID:       sourceSystemID,
		AdapterManifestID:    adapterManifestID,
		IntegrationMode:      investigation.IntegrationFixture,
		ProofClass:           investigation.ProofSyntheticFixture,
		AssuranceMode:        investigation.AssuranceSyntheticFixture,
		SourceArtifactSHA256: artifactSHA256,
		SourceGeneratedAt:    closedAt,
		SourceTerminalAt:     closedAt,
		SourceRevision:       unirouter.ExecutionEvidenceProfileID + "@" + unirouter.ExecutionEvidenceProfileManifestSHA256,
		ImportedRevision:     artifactSHA256,
		SourceWatermark:      artifactSHA256,
		ImportWatermark:      artifactSHA256,
		TimelineComplete:     bundle.Completeness.SourceProfileComplete,
	}, nil
}

func snapshotTimelineComplete(snapshot unirouter.SnapshotFile) bool {
	if len(snapshot.CaptureGaps) > 0 || snapshot.Counts.CaptureGaps > 0 {
		return false
	}
	for _, event := range snapshot.TraceEvents {
		if event.CaptureGap != nil && strings.TrimSpace(*event.CaptureGap) != "" {
			return false
		}
	}
	return true
}

func parseOptionalRFC3339(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, err
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func readBackRequests(ctx context.Context, store *db.EvidenceStore, requestIDs []string, limit int) ([]gateAReadback, error) {
	readback := make([]gateAReadback, 0, len(requestIDs))
	for _, requestID := range requestIDs {
		events, err := store.ListEvidenceEventsByRequestID(ctx, requestID, limit)
		if err != nil {
			return nil, err
		}
		gaps, err := store.ListCaptureGapsByRequestID(ctx, requestID, limit)
		if err != nil {
			return nil, err
		}
		row := gateAReadback{
			RequestID:         requestID,
			EventCount:        len(events),
			GapCount:          len(gaps),
			EventTypes:        uniqueStrings(eventTypes(events)),
			SourceEventTypes:  uniqueStrings(sourceEventTypes(events)),
			GapTypes:          uniqueStrings(gapTypes(gaps)),
			CandidateGapTypes: uniqueStrings(candidateGapTypes(gaps)),
		}
		readback = append(readback, row)
	}
	return readback, nil
}

func eventTypes(events []db.EvidenceEventRecord) []string {
	values := make([]string, 0, len(events))
	for _, event := range events {
		values = append(values, event.EventType)
	}
	return values
}

func sourceEventTypes(events []db.EvidenceEventRecord) []string {
	values := make([]string, 0, len(events))
	for _, event := range events {
		values = append(values, event.SourceEventType)
	}
	return values
}

func gapTypes(gaps []db.CaptureGapRecord) []string {
	values := make([]string, 0, len(gaps))
	for _, gap := range gaps {
		values = append(values, gap.GapType)
	}
	return values
}

func candidateGapTypes(gaps []db.CaptureGapRecord) []string {
	var values []string
	for _, gap := range gaps {
		values = append(values, gap.CandidateGapTypes...)
	}
	return values
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		unique = append(unique, value)
	}
	sort.Strings(unique)
	return unique
}

func usage() error {
	return fmt.Errorf("usage: xinsight-importer import-resource-actions --authorization-file absolute-path --target-ref target [--check] | xinsight-importer import-synthetic-resources --authorization-file absolute-path --target-ref target [--check] | xinsight-importer fixtures run [--schema path] [--fixtures dir] | xinsight-importer unirouter discover [--snapshot path | --dsn dsn] | xinsight-importer unirouter preflight-sanitized --snapshot path --approval-manifest path [--max-request-ids 20] [--max-events 200] | xinsight-importer unirouter import-snapshot --snapshot path [--approval-manifest path] | xinsight-importer unirouter import-evidence-v2 --bundle path [--persist] | xinsight-importer unirouter import-campaign-package --operation-id id --package absolute-path --n1-release-manifest absolute-path --n1-verifier absolute-path --campaign-manifest absolute-path --unirouter-ack absolute-path --xinsight-ack absolute-path --authorization absolute-path --authorization-signature absolute-path --authorization-trust-set absolute-path --source-evidence-trust-set absolute-path --component-scope-receipt absolute-path --component-verification-receipt absolute-path --verifier-release absolute-path --product-release absolute-path --evidence-schema absolute-path | xinsight-importer unirouter gate-a-check --snapshot path [--approval-manifest path] [--receipt-output path] | xinsight-importer unirouter pull-feed | xinsight-importer unirouter pull-source-facts --stream-id uuid [--config absolute-path] | xinsight-importer unirouter run-source-facts [--config absolute-path] | xinsight-importer unirouter import-source-facts --authorization-file absolute-path --target-ref target [--check] | xinsight-importer litellm pull-spend-logs | xinsight-importer openinference import-bundle --authorization-file absolute-path --target-ref target --bundle absolute-path --operation-id id --bundle-id id --producer-instance id --org-ref id --project-ref id [--tenant-ref id]")
}
