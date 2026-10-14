package uniroutercampaign

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/boundedoffline"
	"github.com/foigent/x-insight/internal/investigation"
)

var errPostgresReceiptCrash = errors.New("injected post-Evidence pre-receipt crash")

type persistThenFailOnce struct {
	delegate EvidencePersister
	fail     bool
}

func (persister *persistThenFailOnce) PersistEventsWithProvenance(
	ctx context.Context,
	events []map[string]any,
	provenance investigation.ImportProvenance,
) (db.PersistenceReport, error) {
	report, err := persister.delegate.PersistEventsWithProvenance(ctx, events, provenance)
	if err != nil {
		return report, err
	}
	if persister.fail {
		persister.fail = false
		return report, errPostgresReceiptCrash
	}
	return report, nil
}

func TestPostgres16CampaignPackageLifecycle(t *testing.T) {
	adminDSN := os.Getenv(config.AdminDSNEnv)
	if adminDSN == "" {
		adminDSN = os.Getenv(config.PostgresDSNEnv)
	}
	importerDSN := os.Getenv(config.ImporterDSNEnv)
	if adminDSN == "" || importerDSN == "" {
		t.Skip("set an admin PostgreSQL DSN and XINSIGHT_IMPORTER_POSTGRES_DSN")
	}
	ctx := t.Context()
	if _, err := db.RunMigrations(ctx, adminDSN, filepath.Join("..", "..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("pgx", importerDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(2)
	database.SetMaxIdleConns(1)
	if err := database.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	var postgresMajor int
	var currentRole string
	if err := database.QueryRowContext(ctx, `
SELECT current_setting('server_version_num')::integer / 10000, current_user`,
	).Scan(&postgresMajor, &currentRole); err != nil {
		t.Fatal(err)
	}
	if postgresMajor != 16 || currentRole != "xinsight_importer" {
		t.Fatalf("PostgreSQL/importer boundary=%d/%s", postgresMajor, currentRole)
	}
	expectedMigrations, err := db.LoadMigrationChecksums(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := database.QueryContext(ctx, `SELECT version, checksum_sha256, dirty FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	var appliedMigrations []db.MigrationChecksum
	for rows.Next() {
		var migration db.MigrationChecksum
		var checksum sql.NullString
		var dirty bool
		if err := rows.Scan(&migration.ID, &checksum, &dirty); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if dirty || !checksum.Valid {
			rows.Close()
			t.Fatalf("incomplete or dirty migration: %s", migration.ID)
		}
		migration.SHA256 = checksum.String
		appliedMigrations = append(appliedMigrations, migration)
	}
	rowsErr := rows.Err()
	closeErr := rows.Close()
	if rowsErr != nil {
		t.Fatal(rowsErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if !slices.Equal(appliedMigrations, expectedMigrations) {
		t.Fatalf("schema ledger differs from exact migration IDs and checksums: got=%v want=%v", appliedMigrations, expectedMigrations)
	}

	checkpointBefore := readCheckpointTableCounts(t, ctx, database)
	operations, err := boundedoffline.NewPostgresOperationStore(database)
	if err != nil {
		t.Fatal(err)
	}
	evidenceStore := db.NewEvidenceStore(database)
	token := postgresCampaignToken(t)

	firstPrepared := postgresCampaignPrepared(t, token+"a", "pg16-first-"+token)
	firstImporter, err := NewImporter(
		evidenceStore,
		operations,
		func() time.Time { return time.Date(2026, 8, 11, 1, 2, 3, 456000000, time.UTC) },
	)
	if err != nil {
		t.Fatal(err)
	}
	firstReceipt, err := firstImporter.ImportPrepared(ctx, firstPrepared)
	if err != nil {
		t.Fatal(err)
	}
	if firstReceipt.OperationalReceipt.Counts.Inserted != firstReceipt.OperationalReceipt.Counts.Received ||
		firstReceipt.OperationalReceipt.Counts.DuplicateNoop != 0 {
		t.Fatalf("first import counts=%+v", firstReceipt.OperationalReceipt.Counts)
	}
	assertPostgresCampaignReadback(t, ctx, database, firstReceipt)
	firstCanonical, err := firstReceipt.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	firstReplay, err := firstImporter.ImportPrepared(ctx, firstPrepared)
	if err != nil {
		t.Fatal(err)
	}
	firstReplayCanonical, err := firstReplay.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstCanonical, firstReplayCanonical) {
		t.Fatal("completed PostgreSQL replay changed the typed receipt")
	}

	driftedPrepared := postgresCampaignPrepared(t, token+"d", "pg16-first-"+token)
	if _, err := firstImporter.ImportPrepared(ctx, driftedPrepared); !errors.Is(
		err,
		boundedoffline.ErrOperationConflict,
	) {
		t.Fatalf("request drift error=%v", err)
	}
	assertNoCampaignMemberReadback(t, ctx, database, driftedPrepared.members[0].inventory.SHA256)

	recoveryPrepared := postgresCampaignPrepared(t, token+"r", "pg16-recovery-"+token)
	crashingPersister := &persistThenFailOnce{delegate: evidenceStore, fail: true}
	recoveryImporter, err := NewImporter(
		crashingPersister,
		operations,
		func() time.Time { return time.Date(2026, 8, 11, 1, 2, 4, 567000000, time.UTC) },
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recoveryImporter.ImportPrepared(ctx, recoveryPrepared); !errors.Is(
		err,
		errPostgresReceiptCrash,
	) {
		t.Fatalf("injected recovery error=%v", err)
	}
	assertOperationRows(t, ctx, database, recoveryPrepared.request.OperationID, 1, 0)
	recoveredReceipt, err := recoveryImporter.ImportPrepared(ctx, recoveryPrepared)
	if err != nil {
		t.Fatal(err)
	}
	if recoveredReceipt.OperationalReceipt.Counts.Inserted != 0 ||
		recoveredReceipt.OperationalReceipt.Counts.DuplicateNoop != recoveredReceipt.OperationalReceipt.Counts.Received {
		t.Fatalf("post-Evidence recovery counts=%+v", recoveredReceipt.OperationalReceipt.Counts)
	}
	assertPostgresCampaignReadback(t, ctx, database, recoveredReceipt)
	assertOperationRows(t, ctx, database, recoveryPrepared.request.OperationID, 1, 1)

	checkpointAfter := readCheckpointTableCounts(t, ctx, database)
	if checkpointAfter != checkpointBefore {
		t.Fatalf("checkpoint or epoch state changed: before=%+v after=%+v", checkpointBefore, checkpointAfter)
	}
}

type checkpointTableCounts struct {
	States     int64
	Operations int64
	History    int64
}

func readCheckpointTableCounts(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
) checkpointTableCounts {
	t.Helper()
	var counts checkpointTableCounts
	if err := database.QueryRowContext(ctx, `
SELECT
  (SELECT count(*) FROM adapter_import_states),
  (SELECT count(*) FROM connector_checkpoint_epoch_operations),
  (SELECT count(*) FROM connector_checkpoint_epoch_history)`,
	).Scan(&counts.States, &counts.Operations, &counts.History); err != nil {
		t.Fatal(err)
	}
	return counts
}

func assertPostgresCampaignReadback(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	receipt unirouter.CampaignPackageImportReceipt,
) {
	t.Helper()
	assertOperationRows(t, ctx, database, receipt.OperationID, 1, 1)
	var storedReceiptSHA string
	var storedCanonical []byte
	if err := database.QueryRowContext(ctx, `
SELECT receipt_sha256, canonical_receipt
FROM connector_operational_receipts
WHERE connector_id = $1 AND operation_id = $2`,
		campaignPackageConnectorIDForTest(),
		receipt.OperationID,
	).Scan(&storedReceiptSHA, &storedCanonical); err != nil {
		t.Fatal(err)
	}
	operationalCanonical, err := receipt.OperationalReceipt.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if storedReceiptSHA != receipt.OperationalReceiptSHA256 ||
		!bytes.Equal(storedCanonical, operationalCanonical) {
		t.Fatal("durable operational receipt readback drifted")
	}
	if !receipt.PackageScopeComplete || receipt.AllMemberSourceProfilesComplete ||
		len(receipt.Members) != 1 || receipt.Members[0].SourceProfileComplete ||
		receipt.Members[0].RuntimeCloseMarkerAvailable ||
		!slices.Contains(receipt.Members[0].GapCodes, unirouter.RuntimeCloseMarkerUnavailableGapCode) {
		t.Fatalf("outer/member completeness=%+v", receipt)
	}
	var provenanceRows int64
	var evidenceRows int64
	var gapRows int64
	if err := database.QueryRowContext(ctx, `
SELECT
  count(DISTINCT provenance.id),
  count(DISTINCT links.evidence_event_id),
  count(DISTINCT gaps.id)
FROM import_provenance provenance
LEFT JOIN import_provenance_events links
  ON links.import_provenance_id = provenance.id
LEFT JOIN capture_gaps gaps
  ON gaps.evidence_event_id = links.evidence_event_id
WHERE provenance.source_payload_sha256 = $1`,
		receipt.Members[0].SHA256,
	).Scan(&provenanceRows, &evidenceRows, &gapRows); err != nil {
		t.Fatal(err)
	}
	if provenanceRows != 1 || evidenceRows != receipt.OperationalReceipt.Counts.Received || gapRows < 1 {
		t.Fatalf(
			"Evidence/provenance/Gap readback=%d/%d/%d expected_events=%d",
			provenanceRows,
			evidenceRows,
			gapRows,
			receipt.OperationalReceipt.Counts.Received,
		)
	}
}

func assertNoCampaignMemberReadback(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	payloadSHA256 string,
) {
	t.Helper()
	var rows int64
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM import_provenance WHERE source_payload_sha256 = $1`,
		payloadSHA256,
	).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("request drift persisted %d provenance rows", rows)
	}
}

func assertOperationRows(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	operationID string,
	wantBindings int64,
	wantReceipts int64,
) {
	t.Helper()
	var bindings int64
	var receipts int64
	if err := database.QueryRowContext(ctx, `
SELECT
  (SELECT count(*) FROM connector_import_operation_bindings
   WHERE connector_id = $1 AND operation_id = $2),
  (SELECT count(*) FROM connector_operational_receipts
   WHERE connector_id = $1 AND operation_id = $2)`,
		campaignPackageConnectorIDForTest(),
		operationID,
	).Scan(&bindings, &receipts); err != nil {
		t.Fatal(err)
	}
	if bindings != wantBindings || receipts != wantReceipts {
		t.Fatalf("operation rows=%d/%d want=%d/%d", bindings, receipts, wantBindings, wantReceipts)
	}
}

func postgresCampaignPrepared(t *testing.T, suffix string, operationID string) PreparedImport {
	t.Helper()
	packageBytes, request, binding, adapter := campaignImportFixture(t)
	sourceInstance := "pg16.campaign." + suffix
	memberBytes := partialV2Member(t, sourceInstance)
	packageBytes = []byte("immutable-pg16-campaign-package-" + suffix)
	projection := adapter.opened.Projection
	projection.PackageID = "pg16-campaign-package-" + suffix
	projection.PackageSHA256 = sha256Ref(packageBytes)
	projection.PackageSizeBytes = int64(len(packageBytes))
	projection.SourceInstance = sourceInstance
	projection.Members[0].SHA256 = sha256Ref(memberBytes)
	projection.Members[0].SizeBytes = int64(len(memberBytes))
	adapter.opened.Projection = projection
	adapter.opened.MemberPayloads = map[string][]byte{
		projection.Members[0].MemberID: memberBytes,
	}
	request.OperationID = operationID
	request.Verification.ExpectedPackageID = projection.PackageID
	request.Verification.ExpectedPackageSHA256 = projection.PackageSHA256
	prepared, err := prepareFixtureImport(packageBytes, request, binding, adapter)
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func postgresCampaignToken(t *testing.T) string {
	t.Helper()
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(raw[:])
}

func campaignPackageConnectorIDForTest() string {
	return "foigent.unirouter.poc-source-evidence"
}
