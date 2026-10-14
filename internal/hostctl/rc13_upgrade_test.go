package hostctl

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/foigent/x-insight/internal/db"
)

func TestRC13UpgradeFromRC12AdvancesSchema22To23AndCheckIsNonMutating(t *testing.T) {
	fixture := newInstallFixture(t)
	writeSyntheticAccessCredential(t, fixture.paths.AccessAccountsFile)
	controllerUnit := string(mustReadTestFile(t, "../../deploy/xinsight-connector-controller.service"))
	feedUnit := "[Service]\nExecStart=/opt/x-insight/current/bin/xinsight-importer unirouter pull-feed\n"
	fixture.artifact = packVersionedHostRelease(
		t,
		fixture,
		"rc12-current-for-rc13",
		"0.3.0-rc.12",
		"0.3.0-rc.12/manual-r12",
		"1212121212121212121212121212121212121212",
		nil,
		feedUnit,
		withDatabaseSchema(22),
		withLiteLLMUnit(testLiteLLMWorkerUnit),
		withAccessPlane(),
		withConnectorControllerUnit(controllerUnit),
	)
	current, err := fixture.engine.verifyTarget(fixture.artifact)
	if err != nil {
		t.Fatalf("verify rc.12 source: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{
		identity:        current.Identity,
		accessProtected: true,
	}}
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("install rc.12 source: %v", err)
	}

	targetArtifact := packVersionedHostRelease(
		t,
		fixture,
		"rc13-hardening-target",
		"0.3.0-rc.13",
		"0.3.0-rc.13/manual-r13",
		"1313131313131313131313131313131313131313",
		[]string{"0.3.0-rc.12"},
		feedUnit,
		withDatabaseSchema(23),
		withLiteLLMUnit(testLiteLLMWorkerUnit),
		withAccessPlane(),
		withConnectorControllerUnit(controllerUnit),
	)
	target, err := fixture.engine.verifyTarget(targetArtifact)
	if err != nil {
		t.Fatalf("verify rc.13 target: %v", err)
	}
	fixture.engine.HTTPClient = &http.Client{Transport: fixedProductTransport{
		identity:        target.Identity,
		accessProtected: true,
	}}

	journalBefore, err := fixture.engine.readActiveJournal()
	if err != nil {
		t.Fatal(err)
	}
	migrationsBefore := append([]db.MigrationRecord(nil), fixture.database.facts.MigrationLedger...)
	report, err := fixture.engine.CheckUpgrade(context.Background(), targetArtifact)
	if err != nil || !report.Supported || report.Current == nil || report.Target == nil ||
		report.Current.ProductVersion != "0.3.0-rc.12" ||
		report.Target.ProductVersion != "0.3.0-rc.13" || report.Target.DatabaseSchema != "23" {
		t.Fatalf("rc.12 to rc.13 check=%+v err=%v", report, err)
	}
	journalAfter, err := fixture.engine.readActiveJournal()
	if err != nil || !reflect.DeepEqual(journalAfter, journalBefore) ||
		!reflect.DeepEqual(fixture.database.facts.MigrationLedger, migrationsBefore) {
		t.Fatalf("check mutated state journal_equal=%t migrations_equal=%t err=%v",
			reflect.DeepEqual(journalAfter, journalBefore),
			reflect.DeepEqual(fixture.database.facts.MigrationLedger, migrationsBefore),
			err,
		)
	}

	receipt, err := fixture.engine.Upgrade(context.Background(), targetArtifact)
	if err != nil || receipt.Status != "completed" || receipt.Before == nil || receipt.After == nil ||
		receipt.Before.ProductVersion != "0.3.0-rc.12" ||
		receipt.After.ProductVersion != "0.3.0-rc.13" || receipt.After.DatabaseSchema != "23" {
		t.Fatalf("rc.12 to rc.13 receipt=%+v err=%v", receipt, err)
	}
	if len(fixture.database.facts.MigrationLedger) != len(migrationsBefore)+1 {
		t.Fatalf("schema23 upgrade ledger: before=%d after=%+v",
			len(migrationsBefore), fixture.database.facts.MigrationLedger)
	}
}
