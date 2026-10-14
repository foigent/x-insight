package hostctl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/releasebundle"
)

func TestRC10UpgradeRejectsSameVersionAndUnsupportedOldSource(t *testing.T) {
	t.Run("same version different commit", func(t *testing.T) {
		fixture := newInstallFixture(t)
		fixture.artifact = packVersionedHostRelease(
			t,
			fixture,
			"rc9-current",
			"0.2.0-rc.9",
			"0.2.0-rc.9/manual-r9",
			"9999999999999999999999999999999999999991",
			nil,
			"",
		)
		current, err := fixture.engine.verifyTarget(fixture.artifact)
		if err != nil {
			t.Fatalf("verify rc.9 current: %v", err)
		}
		fixture.engine.HTTPClient = &http.Client{
			Transport: fixedProductTransport{identity: current.Identity},
		}
		if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
			t.Fatalf("install rc.9 current: %v", err)
		}
		sameVersion := packVersionedHostRelease(
			t,
			fixture,
			"rc9-different-commit",
			"0.2.0-rc.9",
			"0.2.0-rc.9/manual-r9",
			"9999999999999999999999999999999999999992",
			[]string{"0.2.0-rc.9"},
			"",
		)

		if _, err := fixture.engine.CheckUpgrade(context.Background(), sameVersion); err == nil ||
			!strings.Contains(err.Error(), "target release is already active") {
			t.Fatalf("same-version upgrade error=%v", err)
		}
	})

	t.Run("unsupported old source", func(t *testing.T) {
		fixture := newInstallFixture(t)
		if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
			t.Fatalf("install old source: %v", err)
		}
		rc10 := packVersionedHostRelease(
			t,
			fixture,
			"rc10-old-source-rejection",
			"0.2.0-rc.10",
			"0.2.0-rc.10/manual-r10",
			"1010101010101010101010101010101010101010",
			[]string{"0.2.0-rc.9"},
			"",
		)

		if _, err := fixture.engine.CheckUpgrade(context.Background(), rc10); err == nil ||
			!strings.Contains(err.Error(), "unsupported upgrade path") {
			t.Fatalf("unsupported old-source upgrade error=%v", err)
		}
	})
}

func TestRC9ToRC10TargetControllerRecoveryPreservesLiveFeedState(t *testing.T) {
	fixture := newInstallFixture(t)
	fixture.artifact = packVersionedHostRelease(
		t,
		fixture,
		"rc9-current",
		"0.2.0-rc.9",
		"0.2.0-rc.9/manual-r9",
		"9999999999999999999999999999999999999991",
		nil,
		"",
	)
	current, err := fixture.engine.verifyTarget(fixture.artifact)
	if err != nil {
		t.Fatalf("verify rc.9 current: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{
		Transport: fixedProductTransport{identity: current.Identity},
	}
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("install rc.9 current: %v", err)
	}

	feedEnvironment := []byte(
		"XINSIGHT_UNIROUTER_FEED_ENDPOINT=https://private.example.invalid/api/v1/integrations/xinsight/completed-executions\n" +
			"XINSIGHT_UNIROUTER_FEED_POLL_INTERVAL=5s\n" +
			"XINSIGHT_UNIROUTER_FEED_ALLOW_PRIVATE_TUNNEL_HTTP=false\n",
	)
	token := "ur-xf1-" + strings.Repeat("00", 32)
	registration := FeedCredentialRegistration{
		SchemaVersion:  FeedCredentialRegistrationSchema,
		TokenFormat:    FeedCredentialTokenFormat,
		VerifierScheme: FeedCredentialVerifierScheme,
		KeyPrefix:      token[:20],
		VerifierSHA256: feedCredentialVerifier(token),
		OperationID:    strings.Repeat("1", 32),
	}
	completedAt := fixture.now.Add(-time.Minute)
	credentialState := feedCredentialOperationState{
		SchemaVersion: FeedCredentialStateSchema,
		Active:        &registration,
		LastOperation: &feedCredentialTerminal{
			Status:       "activated",
			Registration: registration,
			CompletedAt:  completedAt,
		},
		UpdatedAt: completedAt,
	}
	writeHostFile(t, fixture.paths.FeedEnvironmentFile, feedEnvironment, 0o600)
	writeHostFile(t, fixture.paths.FeedCredentialFile, []byte(token), 0o600)
	writeHostJSON(t, fixture.paths.FeedCredentialState, credentialState, 0o600)
	credentialStateBytes, err := os.ReadFile(fixture.paths.FeedCredentialState)
	if err != nil {
		t.Fatalf("read active credential state: %v", err)
	}
	databaseSetBefore, err := readAbsoluteSymlink(fixture.paths.DatabaseCurrentLink)
	if err != nil {
		t.Fatalf("read database set before upgrade: %v", err)
	}
	fixture.database.durableCursor = "bootstrap-cursor-preserved-across-rc9-rc10"
	runner := fixture.engine.Runner.(*fakeRunner)
	runner.feedWorkerLoaded = true
	runner.feedWorkerActive = true
	runner.feedWorkerEnabled = false
	runner.commands = nil

	targetFeedUnit := "[Unit]\nDescription=rc.10 target payload feed worker\n[Service]\nExecStart=/opt/x-insight/current/bin/xinsight-importer unirouter pull-feed\n"
	rc10 := packVersionedHostRelease(
		t,
		fixture,
		"rc10-target",
		"0.2.0-rc.10",
		"0.2.0-rc.10/manual-r10",
		"1010101010101010101010101010101010101010",
		[]string{"0.2.0-rc.9"},
		targetFeedUnit,
	)
	target, err := fixture.engine.verifyTarget(rc10)
	if err != nil {
		t.Fatalf("verify rc.10 target: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{
		Transport: fixedProductTransport{identity: target.Identity},
	}
	check, err := fixture.engine.CheckUpgrade(context.Background(), rc10)
	if err != nil || !check.Supported ||
		check.Current == nil || check.Current.ProductVersion != "0.2.0-rc.9" ||
		check.Target == nil || check.Target.ProductVersion != "0.2.0-rc.10" {
		t.Fatalf("rc.9 to rc.10 preflight=%+v err=%v", check, err)
	}

	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "install_runtime_files")
	if _, err := fixture.engine.Upgrade(context.Background(), rc10); err == nil {
		t.Fatal("expected injected rc.10 upgrade interruption")
	}
	if runner.feedWorkerActive {
		t.Fatal("interrupted upgrade left feed worker active in maintenance")
	}
	journal, err := fixture.engine.readActiveJournal()
	if err != nil {
		t.Fatalf("read interrupted rc.10 journal: %v", err)
	}
	if !journal.FeedWorkerStateCaptured ||
		!journal.FeedWorkerWasPresent ||
		!journal.FeedWorkerWasActive ||
		journal.FeedWorkerWasEnabled {
		t.Fatalf("captured active+disabled feed worker state=%+v", journal)
	}
	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "")
	receipt, err := fixture.engine.Resume(context.Background())
	if err != nil || receipt.Status != "completed" || receipt.Kind != "upgrade" {
		t.Fatalf("resume rc.10 upgrade receipt=%+v err=%v", receipt, err)
	}

	state, err := fixture.engine.readInstallState()
	if err != nil ||
		state.Current.ProductVersion != "0.2.0-rc.10" ||
		state.Current.Commit != "1010101010101010101010101010101010101010" ||
		state.Previous == nil ||
		state.Previous.ProductVersion != "0.2.0-rc.9" ||
		state.DatabaseSchemaCurrent != "13" {
		t.Fatalf("rc.10 install state=%+v err=%v", state, err)
	}
	assertFileContents(t, fixture.paths.FeedServiceUnit, []byte(targetFeedUnit))
	assertFileContents(t, fixture.paths.FeedEnvironmentFile, feedEnvironment)
	assertFileContents(t, fixture.paths.FeedCredentialFile, []byte(token))
	assertFileContents(t, fixture.paths.FeedCredentialState, credentialStateBytes)
	databaseSetAfter, err := readAbsoluteSymlink(fixture.paths.DatabaseCurrentLink)
	if err != nil || databaseSetAfter != databaseSetBefore {
		t.Fatalf(
			"database set after upgrade=%q before=%q err=%v",
			databaseSetAfter,
			databaseSetBefore,
			err,
		)
	}
	if fixture.database.durableCursor != "bootstrap-cursor-preserved-across-rc9-rc10" {
		t.Fatalf("durable cursor changed: %q", fixture.database.durableCursor)
	}
	if !runner.feedWorkerActive || runner.feedWorkerEnabled {
		t.Fatalf(
			"feed worker state not independently restored active=%t enabled=%t",
			runner.feedWorkerActive,
			runner.feedWorkerEnabled,
		)
	}
	if !containsCommand(runner.commands, "systemctl", "stop", feedWorkerUnitName) ||
		!containsCommand(runner.commands, "systemctl", "restart", feedWorkerUnitName) {
		t.Fatalf("feed worker lifecycle commands=%+v", runner.commands)
	}
	if containsCommand(runner.commands, "systemctl", "enable", feedWorkerUnitName) ||
		containsCommand(runner.commands, "systemctl", "enable", "--now", feedWorkerUnitName) {
		t.Fatalf("active but disabled feed worker was unexpectedly enabled: %+v", runner.commands)
	}
}

func packVersionedHostRelease(
	t *testing.T,
	fixture installFixture,
	label string,
	productVersion string,
	manualVersion string,
	commit string,
	supportedUpgradeSources []string,
	feedUnit string,
	options ...versionedHostReleaseOption,
) string {
	t.Helper()
	settings := versionedHostReleaseSettings{}
	for _, option := range options {
		option(&settings)
	}
	databaseSchema := 13
	if productVersion == "0.2.0-rc.12" || productVersion == "0.2.0-rc.13" {
		databaseSchema = 14
	}
	if productVersion == "0.2.0-rc.14" || productVersion == "0.2.0-rc.15" {
		databaseSchema = 15
	}
	if settings.databaseSchema != 0 {
		databaseSchema = settings.databaseSchema
	}
	payload := filepath.Join(fixture.paths.Root, "artifacts", "payload-"+label)
	writeHostPayload(t, payload)
	for index := 2; index <= databaseSchema; index++ {
		writeHostFile(
			t,
			filepath.Join(
				payload,
				"migrations",
				fmt.Sprintf("%06d_rc10_compatibility_test.up.sql", index),
			),
			[]byte(fmt.Sprintf(
				"CREATE TABLE rc10_compatibility_test_%02d (id bigint PRIMARY KEY);\n",
				index,
			)),
			0o644,
		)
	}
	if feedUnit != "" {
		writeHostFile(
			t,
			filepath.Join(payload, "deploy", "xinsight-feed.service"),
			[]byte(feedUnit),
			0o644,
		)
	}
	if settings.liteLLMUnit != "" {
		writeHostFile(
			t,
			filepath.Join(payload, "deploy", "xinsight-litellm-feed.service"),
			[]byte(settings.liteLLMUnit),
			0o644,
		)
		writeHostFile(
			t,
			filepath.Join(payload, "deploy", "xinsight-litellm-feed.env.example"),
			[]byte(
				"XINSIGHT_LITELLM_FEED_ENDPOINT=https://private.example.invalid/spend/logs/v2\n"+
					"XINSIGHT_LITELLM_FEED_POLL_INTERVAL=30s\n"+
					"XINSIGHT_LITELLM_FEED_SOURCE_INSTANCE=litellm-test\n"+
					"XINSIGHT_LITELLM_FEED_TENANT_REF=\n"+
					"XINSIGHT_LITELLM_FEED_ORG_REF=1\n"+
					"XINSIGHT_LITELLM_FEED_PROJECT_REF=2\n",
			),
			0o644,
		)
	}
	if settings.connectorControllerUnit != "" {
		writeHostFile(
			t,
			filepath.Join(payload, "deploy", connectorControllerUnitName),
			[]byte(settings.connectorControllerUnit),
			0o644,
		)
	}
	if productVersion == "0.3.0-rc.10" ||
		productVersion == "0.3.0-rc.11" ||
		productVersion == "0.3.0-rc.12" ||
		productVersion == "0.3.0-rc.13" {
		writeHostFile(
			t,
			filepath.Join(
				payload,
				"schemas",
				"integration",
				"connector-checkpoint-epoch-prepare-v1.schema.json",
			),
			[]byte("{}\n"),
			0o644,
		)
	}
	apiVersions := []string{"xinsight.investigation.v1"}
	if settings.accessPlane {
		apiVersions = append(apiVersions, "xinsight.access.v1")
		writeHostFile(
			t,
			filepath.Join(payload, "deploy", "xinsight.service"),
			[]byte(
				"[Service]\n"+
					"LoadCredential=postgres-reader.dsn:/etc/x-insight/database/current/postgres-reader.dsn\n"+
					"LoadCredential=access-accounts.json:/etc/x-insight/access-accounts.json\n",
			),
			0o644,
		)
	}
	var schemaContract releasebundle.SchemaContractIdentity
	if productVersion == "0.3.0-rc.10" ||
		productVersion == "0.3.0-rc.11" ||
		productVersion == "0.3.0-rc.12" ||
		productVersion == "0.3.0-rc.13" {
		migrations, err := db.LoadMigrationChecksums(filepath.Join(payload, "migrations"))
		if err != nil {
			t.Fatalf("load %s migration checksums: %v", label, err)
		}
		migrationDigest := sha256.New()
		for _, migration := range migrations {
			_, _ = fmt.Fprintf(migrationDigest, "%s\t%s\n", migration.ID, migration.SHA256)
		}
		migrationSetSHA256 := "sha256:" + hex.EncodeToString(migrationDigest.Sum(nil))
		const semanticSHA256 = "sha256:2b7556417ee2d2c7b3c36919db9d95db7dd4ae4418b7eee592deecf71535317d"
		schemaContract = releasebundle.SchemaContractIdentity{
			SchemaVersion:      "xinsight.schema-contract.v2",
			SemanticSHA256:     semanticSHA256,
			MigrationSetSHA256: migrationSetSHA256,
		}
		writeHostJSON(t, filepath.Join(payload, "release", "schema-contract.json"), map[string]any{
			"schema_version":       schemaContract.SchemaVersion,
			"database_schema":      strconv.Itoa(databaseSchema),
			"postgres_major":       16,
			"migration_count":      len(migrations),
			"migration_set_sha256": migrationSetSHA256,
			"semantic_sha256":      semanticSHA256,
			"column_layout_sha256": "sha256:" + strings.Repeat("3", 64),
			"objects": map[string]int{
				"namespaces":  1,
				"tables":      0,
				"columns":     0,
				"constraints": 0,
				"indexes":     0,
				"triggers":    0,
				"functions":   0,
				"extensions":  0,
				"types":       0,
				"relations":   0,
			},
			"entries": []map[string]string{{
				"kind":            "N",
				"namespace":       "public",
				"name":            "public",
				"semantic_sha256": "sha256:" + strings.Repeat("2", 64),
			}},
		}, 0o644)
	}
	configPath := filepath.Join(fixture.paths.Root, "artifacts", "release-config-"+label+".json")
	writeHostJSON(t, configPath, releasebundle.ReleaseConfig{
		SchemaVersion:                releasebundle.ConfigSchemaVersion,
		ProductName:                  "x-insight",
		ProductVersion:               productVersion,
		ReleaseChannel:               "release_candidate",
		TrustReference:               releasebundle.DefaultTrustReference,
		SupportedUpgradeFromVersions: append([]string(nil), supportedUpgradeSources...),
		SupportedLegacySources:       append([]releasebundle.LegacySource(nil), settings.supportedLegacySources...),
		ApplicationCompatibility: releasebundle.ApplicationCompatibility{
			DatabaseSchemaMin: strconv.Itoa(databaseSchema),
			DatabaseSchemaMax: strconv.Itoa(databaseSchema),
			ConfigSchemaMin:   "xinsight.config.v1",
			ConfigSchemaMax:   "xinsight.config.v1",
		},
		DatabaseSchema: releasebundle.SchemaCompatibility{
			Current: strconv.Itoa(databaseSchema),
			Min:     strconv.Itoa(databaseSchema),
			Max:     strconv.Itoa(databaseSchema),
		},
		ConfigSchema: releasebundle.SchemaCompatibility{
			Current: "xinsight.config.v1",
			Min:     "xinsight.config.v1",
			Max:     "xinsight.config.v1",
		},
		APIVersions:            apiVersions,
		EvidenceSchemaVersions: []string{"m0.1"},
		ReceiptSchemaVersions:  []string{"xinsight.import-receipt.v1"},
		Manual: releasebundle.ManualIdentity{
			Language: "zh-CN",
			Version:  manualVersion,
		},
		Rollback: releasebundle.RollbackPolicy{
			ApplicationRollbackCompatible: settings.rollbackCompatible,
			Barriers: []string{
				"no_destructive_or_contract_migration",
				"database_restore_is_explicit",
			},
		},
		SchemaContract: schemaContract,
	}, 0o644)
	artifact := filepath.Join(
		fixture.paths.Root,
		"artifacts",
		"xinsight-"+label+"-linux-amd64.tar.gz",
	)
	if _, err := releasebundle.Pack(releasebundle.PackOptions{
		PayloadDir:      payload,
		ConfigPath:      configPath,
		SigningKeyPath:  fixture.privateKey,
		OutputPath:      artifact,
		Commit:          commit,
		SourceDateEpoch: fixture.now.Add(-30 * time.Minute),
		BuildToolchain: releasebundle.BuildToolchain{
			GoVersion:   "go1.25.11",
			NodeVersion: "v22.23.1",
			NPMVersion:  "10.9.8",
		},
	}); err != nil {
		t.Fatalf("pack %s release: %v", label, err)
	}
	return artifact
}

type versionedHostReleaseSettings struct {
	liteLLMUnit             string
	connectorControllerUnit string
	rollbackCompatible      bool
	databaseSchema          int
	accessPlane             bool
	supportedLegacySources  []releasebundle.LegacySource
}

type versionedHostReleaseOption func(*versionedHostReleaseSettings)

func withLiteLLMUnit(unit string) versionedHostReleaseOption {
	return func(settings *versionedHostReleaseSettings) {
		settings.liteLLMUnit = unit
	}
}

func withConnectorControllerUnit(unit string) versionedHostReleaseOption {
	return func(settings *versionedHostReleaseSettings) {
		settings.connectorControllerUnit = unit
	}
}

func withApplicationRollbackCompatible() versionedHostReleaseOption {
	return func(settings *versionedHostReleaseSettings) {
		settings.rollbackCompatible = true
	}
}

func withDatabaseSchema(schema int) versionedHostReleaseOption {
	return func(settings *versionedHostReleaseSettings) {
		settings.databaseSchema = schema
	}
}

func withAccessPlane() versionedHostReleaseOption {
	return func(settings *versionedHostReleaseSettings) {
		settings.accessPlane = true
	}
}

func withSupportedLegacySources(
	sources []releasebundle.LegacySource,
) versionedHostReleaseOption {
	return func(settings *versionedHostReleaseSettings) {
		settings.supportedLegacySources = append(
			[]releasebundle.LegacySource(nil),
			sources...,
		)
	}
}
