package hostctl

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/checkpointepoch"
)

func TestCheckpointBootstrapRequiresExactStrictEmptyDataResetReceiptAndSeparateAuthority(t *testing.T) {
	fixture, _, successor, accountPreview := newCleanBaselineRecoveryFixture(t)
	accountPayload, err := marshalJSON(successor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.paths.AccessAccountsFile, accountPayload, 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := fixture.engine.readInstallState()
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := LoadDatabaseCredentials(fixture.paths)
	if err != nil {
		t.Fatal(err)
	}
	const dataResetOperationID = "20260805T040000Z-abcdef123456"
	const digest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	backupRestore := testCleanBaselineBackupRestoreProof(
		t,
		digest,
		cleanBaselineRestoreDatabaseName(dataResetOperationID),
	)
	floor, err := sealCleanBaselineCheckpointFloor(CleanBaselineCheckpointFloor{
		Binding:                   cleanBaselineBinding(),
		PriorIdentityRows:         1,
		PriorIdentitySHA256:       digest,
		HistoricalMaxEpoch:        5,
		PriorSourceInstanceSHA256: []string{hashCleanBaselineText("old-source")},
		PriorCursorSHA256:         []string{hashCleanBaselineText("old-cursor")},
		PriorHighWatermarkSHA256:  []string{hashCleanBaselineText("old-watermark")},
		RollbackBackupSHA256:      digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	databaseProof := CleanBaselineDatabaseProof{
		PostgresMajor:         16,
		MigrationCount:        expectedCleanBaselineSchemaContract.MigrationCount,
		SchemaContractVersion: CleanBaselineSchemaContractVersion,
		SchemaContractSHA256:  expectedCleanBaselineSchemaContract.SemanticSHA256,
		SchemaLayoutSHA256:    expectedCleanBaselineSchemaContract.ColumnLayoutSHA256,
		SchemaLayoutMatches:   true,
		MigrationSetSHA256:    expectedCleanBaselineSchemaContract.MigrationSetSHA256,
		SourceCatalogRows:     1,
		AdapterCatalogRows:    2,
	}
	dataResetReceipt := OperationReceipt{
		SchemaVersion:  ReceiptSchema,
		OperationID:    dataResetOperationID,
		Kind:           "clean_baseline_data_reset",
		Status:         "completed",
		Before:         &state.Current,
		After:          &state.Current,
		BackupPath:     "/var/backups/x-insight/synthetic-schema20.dump",
		BackupSHA256:   digest,
		DatabaseSetRef: credentials.SetRef,
		ReasonCode:     cleanBaselineReasonCode,
		DataReset: &CleanBaselineDurableEvidence{
			SchemaVersion:             CleanBaselineReceiptSchema,
			Release:                   cleanBaselineReleaseIdentity(state.Current),
			PreviewSHA256:             digest,
			RollbackBackupSHA256:      digest,
			RollbackDatabaseSetSHA256: digest,
			ActiveDatabaseSetSHA256:   cleanBaselineDatabaseSetDigest(credentials.SetRef),
			PredecessorDatabaseSHA256: hashCleanBaselineText("xinsight"),
			BackupRestore:             backupRestore,
			Account:                   accountPreview,
			CheckpointFloor:           floor,
			Credential: CleanBaselineCredentialProof{
				ActiveAbsent:             true,
				StagedAbsent:             true,
				OperationStateAbsent:     true,
				LiteLLMCredentialAbsent:  true,
				LiteLLMEnvironmentAbsent: true,
			},
			Database: databaseProof,
		},
		Checks:      []CheckResult{{Name: "strict_empty_checkpoint", Status: "pass", Detail: "state_zero_operations_zero_history_zero"}},
		CompletedAt: time.Date(2026, 8, 5, 4, 0, 0, 0, time.UTC),
	}
	if err := fixture.engine.writeReceipt(dataResetReceipt); err != nil {
		t.Fatal(err)
	}
	dataResetReceiptPath := filepath.Join(fixture.paths.ReceiptsDir, dataResetOperationID+".json")
	dataResetReceiptSHA256, _, err := fileSHA256(dataResetReceiptPath, cleanBaselineReceiptMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	input := checkpointepoch.PrepareInput{
		Binding:            cleanBaselineBinding(),
		WorkerUnit:         checkpointepoch.CompletedExecutionWorkerUnit,
		NextSourceInstance: "new-source",
		NextCursor:         "new-empty-cursor",
		NextHighWatermark:  "new-empty-cursor",
		AuthorizationRef:   "decision://second-integration-checkpoint-bootstrap",
	}
	active := checkpointepoch.ActiveStateSummary{
		StateID:             "33333333-3333-4333-8333-333333333333",
		Binding:             input.Binding,
		Epoch:               6,
		SourceInstance:      input.NextSourceInstance,
		CursorSHA256:        hashCleanBaselineText(input.NextCursor),
		HighWatermarkSHA256: hashCleanBaselineText(input.NextHighWatermark),
		WorkerStatus:        "inactive",
		StateSHA256:         digest,
	}
	fixture.engine.CleanBaseline = cleanBaselineManagerStub{databaseProof: databaseProof, active: active}
	if err := runSystemctl(context.Background(), fixture.engine.Runner, "start", "xinsight.service"); err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.engine.PreviewCheckpointBootstrap(
		context.Background(), "20260805T040001Z-abcdef123456", digest, input,
	); err == nil {
		t.Fatal("checkpoint bootstrap preview accepted a missing DATA_RESET receipt")
	}
	oldIdentity := input
	oldIdentity.NextSourceInstance = "old-source"
	if _, err := fixture.engine.PreviewCheckpointBootstrap(
		context.Background(), dataResetOperationID, dataResetReceiptSHA256, oldIdentity,
	); err == nil {
		t.Fatal("checkpoint bootstrap preview accepted an old source identity")
	}
	writeHostFile(
		t,
		fixture.paths.LiteLLMEnvironment,
		[]byte("XINSIGHT_LITELLM_FEED_SOURCE_INSTANCE=synthetic-residual-campaign\n"),
		0o600,
	)
	if _, err := fixture.engine.PreviewCheckpointBootstrap(
		context.Background(), dataResetOperationID, dataResetReceiptSHA256, input,
	); err == nil {
		t.Fatal("checkpoint bootstrap preview accepted residual LiteLLM campaign config")
	}
	if err := os.Remove(fixture.paths.LiteLLMEnvironment); err != nil {
		t.Fatal(err)
	}
	preview, err := fixture.engine.PreviewCheckpointBootstrap(
		context.Background(), dataResetOperationID, dataResetReceiptSHA256, input,
	)
	if err != nil {
		t.Fatal(err)
	}
	if preview.NextEpoch != 6 || preview.CheckpointFloorSHA256 != floor.SealedSHA256 {
		t.Fatalf("checkpoint bootstrap preview=%+v", preview)
	}
	changedAuthority := input
	changedAuthority.AuthorizationRef = "decision://different-checkpoint-bootstrap"
	changedPreview, err := fixture.engine.PreviewCheckpointBootstrap(
		context.Background(), dataResetOperationID, dataResetReceiptSHA256, changedAuthority,
	)
	if err != nil {
		t.Fatal(err)
	}
	if changedPreview.RequestSHA256 == preview.RequestSHA256 ||
		changedPreview.PreviewSHA256 == preview.PreviewSHA256 {
		t.Fatal("checkpoint bootstrap preview did not bind the exact authorization reference")
	}
	if _, err := fixture.engine.ApplyCheckpointBootstrap(
		context.Background(), dataResetOperationID, dataResetReceiptSHA256, changedAuthority,
		preview.PreviewSHA256, checkpointBootstrapReasonCode,
	); err == nil {
		t.Fatal("checkpoint bootstrap apply accepted a request outside the authorized preview")
	}
	receipt, err := fixture.engine.ApplyCheckpointBootstrap(
		context.Background(), dataResetOperationID, dataResetReceiptSHA256, input,
		preview.PreviewSHA256, checkpointBootstrapReasonCode,
	)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.OperationID == dataResetOperationID || receipt.Active.Epoch != 6 ||
		receipt.Active.WorkerStatus != "inactive" || receipt.DataResetReceiptSHA256 != dataResetReceiptSHA256 ||
		receipt.RequestSHA256 != preview.RequestSHA256 {
		t.Fatalf("checkpoint bootstrap receipt=%+v", receipt)
	}
	if err := proveCleanBaselineCredentialAbsent(fixture.paths, dataResetOperationID, true); err != nil {
		t.Fatal(err)
	}
}
