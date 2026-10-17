package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/evidence"
	"github.com/foigent/x-insight/internal/exchange"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "xinsight-importer: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
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
		case "import-snapshot":
			return runUniRouterImportSnapshot(args[2:])
		case "gate-a-check":
			return runUniRouterGateACheck(args[2:])
		default:
			return usage()
		}
	default:
		return usage()
	}
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
	schemaPath := fs.String("schema", filepath.Join("schemas", "m0", "evidence-event.schema.json"), "evidence event JSON schema path")
	sourceSystemID := fs.String("source-system-id", unirouter.DefaultSourceSystemID, "X-Insight source_system_id for local import validation")
	adapterManifestID := fs.String("adapter-manifest-id", unirouter.DefaultAdapterManifestID, "X-Insight adapter_manifest_id for local import validation")
	sourceVersion := fs.String("source-version", "", "source version label; defaults to snapshot schema version")
	emitEvents := fs.Bool("emit-events", false, "include mapped evidence events in the JSON report")
	rerunCheck := fs.Bool("rerun-check", true, "ingest the mapped events a second time to verify duplicate_noop behavior")
	persist := fs.Bool("persist", false, "persist accepted mapped events into X-Insight Postgres")
	persistDSN := fs.String("persist-dsn", os.Getenv("XINSIGHT_POSTGRES_DSN"), "X-Insight Postgres DSN used with --persist")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *snapshot == "" {
		return fmt.Errorf("--snapshot is required")
	}

	report, err := unirouter.ImportSnapshot(*snapshot, *schemaPath, unirouter.SnapshotImportOptions{
		SourceSystemID:    *sourceSystemID,
		AdapterManifestID: *adapterManifestID,
		SourceVersion:     *sourceVersion,
		EmitEvents:        *emitEvents,
		RerunCheck:        *rerunCheck,
	})
	if err != nil {
		return err
	}
	if *persist {
		if *persistDSN == "" {
			return fmt.Errorf("--persist requires --persist-dsn or XINSIGHT_POSTGRES_DSN")
		}
		if report.Rejected > 0 || report.Quarantined > 0 {
			return fmt.Errorf("refusing to persist snapshot with rejected=%d quarantined=%d", report.Rejected, report.Quarantined)
		}
		events, err := buildSnapshotEventsWithHashes(*snapshot, *sourceSystemID, *adapterManifestID, *sourceVersion)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store, err := db.OpenEvidenceStore(ctx, *persistDSN)
		if err != nil {
			return err
		}
		defer store.Close()
		persistReport, err := store.PersistEvents(ctx, events)
		if err != nil {
			return err
		}
		report.Persistence = &persistReport
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
	schemaPath := fs.String("schema", filepath.Join("schemas", "m0", "evidence-event.schema.json"), "evidence event JSON schema path")
	sourceSystemID := fs.String("source-system-id", unirouter.DefaultSourceSystemID, "X-Insight source_system_id")
	adapterManifestID := fs.String("adapter-manifest-id", unirouter.DefaultAdapterManifestID, "X-Insight adapter_manifest_id")
	sourceVersion := fs.String("source-version", "", "source version label; defaults to snapshot schema version")
	persist := fs.Bool("persist", false, "persist accepted mapped events into X-Insight Postgres and read them back")
	persistDSN := fs.String("persist-dsn", os.Getenv("XINSIGHT_POSTGRES_DSN"), "X-Insight Postgres DSN used with --persist")
	readbackLimit := fs.Int("readback-limit", 200, "maximum events or gaps to read back per request")
	receiptOutput := fs.String("receipt-output", "", "optional path for a sanitized machine-readable import receipt; refuses to overwrite")
	receiptSchemaPath := fs.String("receipt-schema", filepath.Join("schemas", "integration", "import-receipt.schema.json"), "import receipt JSON schema path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *snapshot == "" {
		return fmt.Errorf("--snapshot is required")
	}

	snapshotFile, err := unirouter.LoadSnapshot(*snapshot)
	if err != nil {
		return err
	}
	snapshotSHA256, err := fileSHA256(*snapshot)
	if err != nil {
		return err
	}
	discovery, err := unirouter.DiscoverSnapshot(*snapshot, snapshotFile.SourceInstance)
	if err != nil {
		return err
	}
	importReport, err := unirouter.ImportSnapshot(*snapshot, *schemaPath, unirouter.SnapshotImportOptions{
		SourceSystemID:    *sourceSystemID,
		AdapterManifestID: *adapterManifestID,
		SourceVersion:     *sourceVersion,
		RerunCheck:        true,
	})
	if err != nil {
		return err
	}
	report := gateACheckReport{
		SnapshotPath:    *snapshot,
		Source:          importReport.Source,
		SourceInstance:  importReport.SourceInstance,
		RequestIDs:      snapshotFile.RequestIDs,
		Discovery:       discovery,
		Import:          importReport,
		Status:          "validated_in_memory",
		RemainingInputs: []string{"run with --persist and XINSIGHT_POSTGRES_DSN to verify Postgres readback"},
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
		if *persistDSN == "" {
			return fmt.Errorf("--persist requires --persist-dsn or XINSIGHT_POSTGRES_DSN")
		}
		events, err := buildSnapshotEventsWithHashes(*snapshot, *sourceSystemID, *adapterManifestID, *sourceVersion)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store, err := db.OpenEvidenceStore(ctx, *persistDSN)
		if err != nil {
			return err
		}
		defer store.Close()
		persistReport, err := store.PersistEvents(ctx, events)
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

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
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

func buildSnapshotEventsWithHashes(snapshotPath string, sourceSystemID string, adapterManifestID string, sourceVersion string) ([]map[string]any, error) {
	snapshotFile, err := unirouter.LoadSnapshot(snapshotPath)
	if err != nil {
		return nil, err
	}
	events := unirouter.BuildSnapshotEvents(snapshotFile, unirouter.SnapshotImportOptions{
		SourceSystemID:    sourceSystemID,
		AdapterManifestID: adapterManifestID,
		SourceVersion:     sourceVersion,
	})
	for _, event := range events {
		hash, err := evidence.GenerateEventHash(event)
		if err != nil {
			return nil, err
		}
		event["event_hash"] = hash
	}
	return events, nil
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
	return fmt.Errorf("usage: xinsight-importer fixtures run [--schema path] [--fixtures dir] | xinsight-importer unirouter discover [--snapshot path | --dsn dsn] | xinsight-importer unirouter import-snapshot --snapshot path | xinsight-importer unirouter gate-a-check --snapshot path [--receipt-output path]")
}
