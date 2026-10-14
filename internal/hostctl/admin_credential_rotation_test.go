package hostctl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
)

type fakeAdminCredentialRotationDatabase struct {
	sessions             int
	transition           AdminCredentialTransitionState
	concurrentAtVerifier bool
	changeCalls          int
	terminationCalls     int
	validationCalls      int
}

func (database *fakeAdminCredentialRotationDatabase) InspectAdminBoundary(
	context.Context,
	DatabaseCredentials,
) (AdminDatabaseBoundary, error) {
	return AdminDatabaseBoundary{
		PostgresMajor: 16, DatabaseSHA256: hashCleanBaselineText("xinsight"),
		AdminRoleAlias:      adminDatabaseRole,
		AdminIdentitySHA256: hashCleanBaselineText("xinsight_admin_superuser_login"),
		AdminSessions:       database.sessions,
	}, nil
}

func (database *fakeAdminCredentialRotationDatabase) ProbeAdminCredentialTransition(
	context.Context,
	DatabaseCredentials,
	DatabaseCredentials,
) (AdminCredentialTransitionState, error) {
	return database.transition, nil
}

func (database *fakeAdminCredentialRotationDatabase) ChangeAdminVerifier(
	_ context.Context,
	_ DatabaseCredentials,
	_ DatabaseCredentials,
	_ io.Reader,
) error {
	database.changeCalls++
	if database.concurrentAtVerifier || database.sessions != 0 {
		return fmt.Errorf("admin verifier transition requires zero concurrent admin sessions")
	}
	database.transition = AdminCredentialTransitionSuccessor
	return nil
}

func (database *fakeAdminCredentialRotationDatabase) TerminateOtherAdminSessions(
	context.Context,
	DatabaseCredentials,
) (int, error) {
	database.terminationCalls++
	terminated := database.sessions
	database.sessions = 0
	return terminated, nil
}

func (database *fakeAdminCredentialRotationDatabase) ValidateAdminCredentialTransition(
	context.Context,
	DatabaseCredentials,
	DatabaseCredentials,
) (AdminCredentialTransitionProof, error) {
	database.validationCalls++
	if database.transition != AdminCredentialTransitionSuccessor || database.sessions != 0 {
		return AdminCredentialTransitionProof{}, fmt.Errorf("admin transition is not complete")
	}
	return AdminCredentialTransitionProof{
		SuccessorFreshConnection: true, PredecessorAuthRejected: true,
		ValidationSessionClosed: true, AdminSessionsRemaining: 0,
	}, nil
}

type adminRotationCleanBaselineStub struct {
	cleanBaselineManagerStub
	preview CleanBaselineDatabasePreview
}

func (stub adminRotationCleanBaselineStub) PreviewCleanBaseline(
	context.Context,
	DatabaseCredentials,
) (CleanBaselineDatabasePreview, error) {
	return stub.preview, nil
}

type adminRotationFixture struct {
	install        installFixture
	manager        *fakeAdminCredentialRotationDatabase
	runner         *fakeRunner
	beforeCommands int
}

func newAdminRotationFixture(t *testing.T) adminRotationFixture {
	t.Helper()
	fixture := newInstallFixture(t)
	if _, err := fixture.engine.Install(context.Background(), fixture.artifact); err != nil {
		t.Fatalf("install fixture: %v", err)
	}
	runner, ok := fixture.engine.Runner.(*fakeRunner)
	if !ok {
		t.Fatal("fixture runner type changed")
	}
	runner.mainServiceActive = true
	runner.mainServicePID = 4242
	runner.feedWorkerLoaded = true
	runner.feedWorkerActive = false
	runner.feedWorkerEnabled = false
	runner.liteLLMWorkerLoaded = true
	runner.liteLLMWorkerActive = false
	runner.liteLLMWorkerEnabled = false

	fixture.database.facts.PublicTableCount = len(cleanBaselineTables)
	fixture.database.facts.DatabaseName = "xinsight"
	fixture.database.facts.AdminRoleIsSuperuser = true
	writeHostJSON(t, fixture.paths.AccessAccountsFile, access.AccountConfig{
		SchemaVersion: access.AccountConfigSchema,
		Accounts: []access.AccountConfigEntry{{
			SubjectID: "xs_0123456789abcdef", Username: "demo.operator",
			Role: access.RoleOperator, Disabled: false, PasswordHash: cleanBaselineTestPasswordHash,
		}},
	}, 0o600)
	for path, contents := range map[string]string{
		fixture.paths.PostgresEnvironment: "POSTGRES_DATA=/data/x-insight/postgres\n",
		fixture.paths.FeedServiceUnit:     "[Unit]\nDescription=X-Insight Feed\n",
		fixture.paths.LiteLLMServiceUnit:  "[Unit]\nDescription=X-Insight LiteLLM Feed\n",
	} {
		writeHostFile(t, path, []byte(contents), 0o644)
	}

	tables := make([]CleanBaselineTableState, 0, len(cleanBaselineTables))
	for _, table := range cleanBaselineTables {
		tables = append(tables, CleanBaselineTableState{
			Table: table, Disposition: "preserve_exact", CurrentRows: 1, ProtectedRows: 1,
			SemanticSHA256: hashCleanBaselineText("table:" + table),
		})
	}
	preview := CleanBaselineDatabasePreview{
		SchemaContractVersion: CleanBaselineSchemaContractVersion,
		SchemaContractSHA256:  expectedCleanBaselineSchemaContract.SemanticSHA256,
		SchemaLayoutSHA256:    hashCleanBaselineText("schema-layout"), SchemaLayoutMatches: true,
		MigrationSetSHA256: expectedCleanBaselineSchemaContract.MigrationSetSHA256,
		Tables:             tables,
		Catalog: CleanBaselineCatalogState{
			SourceRows: 1, SourceSHA256: cleanBaselineSourceCatalogSHA256,
			AdapterRows: 2, AdapterSHA256: cleanBaselineAdapterCatalogSHA256,
		},
		CheckpointFloor: CleanBaselineCheckpointFloor{
			HistoricalMaxEpoch: 7, PriorIdentitySHA256: hashCleanBaselineText("checkpoint-history"),
			SealedSHA256: hashCleanBaselineText("checkpoint-floor"),
		},
	}
	manager := &fakeAdminCredentialRotationDatabase{transition: AdminCredentialTransitionPredecessor}
	fixture.engine.CleanBaseline = adminRotationCleanBaselineStub{preview: preview}
	fixture.engine.AdminCredentialRotation = manager
	fixture.engine.Random = bytes.NewReader(bytes.Repeat([]byte{0x52, 0xa9, 0x17, 0x6c, 0xe1}, 512))
	return adminRotationFixture{install: fixture, manager: manager, runner: runner, beforeCommands: len(runner.commands)}
}

func (fixture adminRotationFixture) previewAndRequest(t *testing.T) (AdminCredentialRotationPreviewReport, AdminCredentialRotationRequest) {
	t.Helper()
	preview, err := fixture.install.engine.PreviewAdminCredentialRotation(
		context.Background(), "decision://second-integration/admin-dsn-rotation", AdminCredentialRotationReasonLocalToolingExposure,
	)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if !preview.Supported || preview.Mutating || preview.Database.AdminSessions != 0 ||
		preview.PlannedPostState.ServicesRestarted != 0 || preview.PlannedPostState.ProductWrites != 0 ||
		preview.PlannedPostState.PollsOrTraffic != 0 {
		t.Fatalf("preview=%+v", preview)
	}
	return preview, AdminCredentialRotationRequest{
		SchemaVersion:    AdminCredentialRotationRequestSchema,
		PreviewSHA256:    preview.PreviewSHA256,
		AuthorizationRef: preview.AuthorizationRef,
		ReasonCode:       AdminCredentialRotationReasonLocalToolingExposure,
	}
}

func TestAdminCredentialRotationRejectsConcurrentAdminSessions(t *testing.T) {
	fixture := newAdminRotationFixture(t)
	fixture.manager.sessions = 1
	if _, err := fixture.install.engine.PreviewAdminCredentialRotation(
		context.Background(), "decision://second-integration/admin-dsn-rotation", AdminCredentialRotationReasonLocalToolingExposure,
	); err == nil || !strings.Contains(err.Error(), "zero concurrent admin sessions") {
		t.Fatalf("concurrent preview error=%v", err)
	}
	active, err := readAbsoluteSymlink(fixture.install.paths.DatabaseCurrentLink)
	if err != nil || active == "" {
		t.Fatalf("active credential link=%q err=%v", active, err)
	}
}

func TestAdminCredentialRotationPreviewDigestIsDeterministic(t *testing.T) {
	fixture := newAdminRotationFixture(t)
	first, _ := fixture.previewAndRequest(t)
	*fixture.install.now = fixture.install.now.Add(3 * time.Hour)
	second, _ := fixture.previewAndRequest(t)
	if first.PreviewSHA256 != second.PreviewSHA256 || first.GeneratedAt.Equal(second.GeneratedAt) {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
}

func TestAdminCredentialRotationRejectsSessionAppearingBeforeVerifierBarrier(t *testing.T) {
	fixture := newAdminRotationFixture(t)
	_, request := fixture.previewAndRequest(t)
	fixture.manager.concurrentAtVerifier = true
	if _, err := fixture.install.engine.ApplyAdminCredentialRotation(context.Background(), request); err == nil ||
		!strings.Contains(err.Error(), "zero concurrent admin sessions") {
		t.Fatalf("verifier quiescence error=%v", err)
	}
	journal, err := fixture.install.engine.readActiveJournal()
	if err != nil || journal.AdminCredentialRotation == nil || journal.AdminCredentialRotation.VerifierChanged ||
		!containsString(journal.AllowedActions, "abort") {
		t.Fatalf("journal=%+v err=%v", journal, err)
	}
	fixture.manager.concurrentAtVerifier = false
	aborted, err := fixture.install.engine.Abort(context.Background())
	if err != nil || aborted.Status != "aborted" {
		t.Fatalf("abort=%+v err=%v", aborted, err)
	}
}

func TestAdminCredentialRotationRecoversLostVerifierResponseBeforeJournal(t *testing.T) {
	fixture := newAdminRotationFixture(t)
	preview, request := fixture.previewAndRequest(t)
	t.Setenv(
		"XINSIGHTCTL_FAIL_ADMIN_CREDENTIAL_ROTATION_AT",
		"after_successor_probe_before_journal",
	)
	if _, err := fixture.install.engine.ApplyAdminCredentialRotation(context.Background(), request); err == nil ||
		!strings.Contains(err.Error(), "process death") {
		t.Fatalf("lost-response fault error=%v", err)
	}

	journal, err := fixture.install.engine.readActiveJournal()
	if err != nil || journal.AdminCredentialRotation == nil || journal.AdminCredentialRotation.VerifierChanged ||
		journal.Phase != "change_admin_scram_verifier" ||
		containsString(journal.CompletedPhases, "change_admin_scram_verifier") {
		t.Fatalf("pre-recovery journal=%+v err=%v", journal, err)
	}
	if fixture.manager.transition != AdminCredentialTransitionSuccessor || fixture.manager.changeCalls != 1 {
		t.Fatalf("external transition=%s change_calls=%d", fixture.manager.transition, fixture.manager.changeCalls)
	}

	if _, err := fixture.install.engine.Abort(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "resume-only") {
		t.Fatalf("lost-response abort error=%v", err)
	}
	inferred, err := fixture.install.engine.readActiveJournal()
	if err != nil || inferred.AdminCredentialRotation == nil || !inferred.AdminCredentialRotation.VerifierChanged ||
		containsString(inferred.AllowedActions, "abort") || !containsString(inferred.AllowedActions, "resume") {
		t.Fatalf("inferred journal=%+v err=%v", inferred, err)
	}
	if fixture.manager.changeCalls != 1 {
		t.Fatalf("abort repeated verifier change: calls=%d", fixture.manager.changeCalls)
	}

	t.Setenv("XINSIGHTCTL_FAIL_ADMIN_CREDENTIAL_ROTATION_AT", "")
	receipt, err := fixture.install.engine.Resume(context.Background())
	if err != nil || receipt.Status != "completed" || receipt.AdminCredentialRotation == nil {
		t.Fatalf("resume receipt=%+v err=%v", receipt, err)
	}
	if fixture.manager.changeCalls != 1 {
		t.Fatalf("resume repeated verifier change: calls=%d", fixture.manager.changeCalls)
	}
	evidence := receipt.AdminCredentialRotation
	if evidence.BeforeState != preview.State || evidence.AfterState != preview.State ||
		evidence.PreviewSHA256 != preview.PreviewSHA256 || !evidence.Transition.PredecessorAuthRejected ||
		!evidence.Transition.SuccessorFreshConnection || !evidence.Transition.ValidationSessionClosed {
		t.Fatalf("no-drift evidence=%+v", evidence)
	}
	active, err := readAbsoluteSymlink(fixture.install.paths.DatabaseCurrentLink)
	if err != nil || active != journal.DatabaseSetAfter {
		t.Fatalf("active successor=%q want=%q err=%v", active, journal.DatabaseSetAfter, err)
	}
	assertAdminRotationNoServiceMutation(t, fixture)
	assertAdminRotationEvidenceContainsNoCredentials(t, fixture, journal.OperationID)
}

func TestAdminCredentialRotationCrashBoundariesAreAbortableBeforeVerifier(t *testing.T) {
	points := []string{"after_staging_directory_create", "after_directory_sync", "before_rename", "after_rename", "after_parent_sync"}
	for _, spec := range adminCredentialMemberSpecs {
		points = append(points,
			"after_"+spec.role+"_create",
			"after_"+spec.role+"_write",
			"after_"+spec.role+"_sync",
		)
	}
	for _, point := range points {
		t.Run(point, func(t *testing.T) {
			fixture := newAdminRotationFixture(t)
			_, request := fixture.previewAndRequest(t)
			t.Setenv("XINSIGHTCTL_FAIL_ADMIN_CREDENTIAL_STAGE_AT", point)
			if _, err := fixture.install.engine.ApplyAdminCredentialRotation(context.Background(), request); err == nil {
				t.Fatalf("fault point %s did not interrupt apply", point)
			}
			journal, err := fixture.install.engine.readActiveJournal()
			if err != nil || journal.AdminCredentialRotation == nil || journal.AdminCredentialRotation.VerifierChanged ||
				!containsString(journal.AllowedActions, "abort") {
				t.Fatalf("point=%s journal=%+v err=%v", point, journal, err)
			}
			if _, statErr := os.Lstat(journal.DatabaseSetAfter); statErr == nil {
				if point != "after_rename" && point != "after_parent_sync" {
					t.Fatalf("point=%s created a partial final set", point)
				}
				if _, err := LoadDatabaseCredentialSet(fixture.install.paths, journal.DatabaseSetAfter); err != nil {
					t.Fatalf("point=%s final set is incomplete: %v", point, err)
				}
			} else if !os.IsNotExist(statErr) {
				t.Fatal(statErr)
			}
			t.Setenv("XINSIGHTCTL_FAIL_ADMIN_CREDENTIAL_STAGE_AT", "")
			receipt, err := fixture.install.engine.Abort(context.Background())
			if err != nil || receipt.Status != "aborted" {
				t.Fatalf("point=%s abort=%+v err=%v", point, receipt, err)
			}
			active, err := readAbsoluteSymlink(fixture.install.paths.DatabaseCurrentLink)
			if err != nil || active != journal.DatabaseSetBefore || fixture.manager.transition != AdminCredentialTransitionPredecessor {
				t.Fatalf("point=%s active=%q transition=%s err=%v", point, active, fixture.manager.transition, err)
			}
		})
	}
}

func TestAdminCredentialRotationPreVerifierPhasesRemainAbortable(t *testing.T) {
	for _, phase := range []string{"stage_admin_successor_credential_set", "revalidate_pre_verifier_boundary"} {
		t.Run(phase, func(t *testing.T) {
			fixture := newAdminRotationFixture(t)
			_, request := fixture.previewAndRequest(t)
			t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", phase)
			if _, err := fixture.install.engine.ApplyAdminCredentialRotation(context.Background(), request); err == nil {
				t.Fatalf("completed phase %s did not interrupt apply", phase)
			}
			journal, err := fixture.install.engine.readActiveJournal()
			if err != nil || journal.AdminCredentialRotation == nil || journal.AdminCredentialRotation.VerifierChanged ||
				!containsString(journal.CompletedPhases, phase) || !containsString(journal.AllowedActions, "abort") {
				t.Fatalf("phase=%s journal=%+v err=%v", phase, journal, err)
			}
			t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "")
			if receipt, err := fixture.install.engine.Abort(context.Background()); err != nil || receipt.Status != "aborted" {
				t.Fatalf("phase=%s abort=%+v err=%v", phase, receipt, err)
			}
		})
	}
}

func TestAdminCredentialRotationResumesAfterPartialInertStaging(t *testing.T) {
	fixture := newAdminRotationFixture(t)
	_, request := fixture.previewAndRequest(t)
	t.Setenv("XINSIGHTCTL_FAIL_ADMIN_CREDENTIAL_STAGE_AT", "after_xinsight_admin_write")
	if _, err := fixture.install.engine.ApplyAdminCredentialRotation(context.Background(), request); err == nil {
		t.Fatal("partial staging fault did not interrupt apply")
	}
	journal, err := fixture.install.engine.readActiveJournal()
	if err != nil || journal.AdminCredentialRotation == nil || journal.AdminCredentialRotation.VerifierChanged ||
		!containsString(journal.AllowedActions, "abort") {
		t.Fatalf("journal=%+v err=%v", journal, err)
	}
	if _, err := os.Lstat(journal.DatabaseSetAfter); !os.IsNotExist(err) {
		t.Fatalf("partial final successor exists: %v", err)
	}
	t.Setenv("XINSIGHTCTL_FAIL_ADMIN_CREDENTIAL_STAGE_AT", "")
	receipt, err := fixture.install.engine.Resume(context.Background())
	if err != nil || receipt.Status != "completed" || receipt.AdminCredentialRotation == nil {
		t.Fatalf("resume receipt=%+v err=%v", receipt, err)
	}
	if _, err := LoadDatabaseCredentialSet(fixture.install.paths, journal.DatabaseSetAfter); err != nil {
		t.Fatalf("resumed final successor is invalid: %v", err)
	}
	assertAdminRotationNoServiceMutation(t, fixture)
}

func TestAdminCredentialRotationFailsClosedOnActiveLinkDrift(t *testing.T) {
	fixture := newAdminRotationFixture(t)
	_, request := fixture.previewAndRequest(t)
	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "stage_admin_successor_credential_set")
	if _, err := fixture.install.engine.ApplyAdminCredentialRotation(context.Background(), request); err == nil {
		t.Fatal("staging phase did not interrupt apply")
	}
	journal, err := fixture.install.engine.readActiveJournal()
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicSymlink(journal.DatabaseSetAfter, fixture.install.paths.DatabaseCurrentLink); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "")
	if _, err := fixture.install.engine.Resume(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "barrier state regressed") {
		t.Fatalf("active-link drift resume error=%v", err)
	}
	if _, err := fixture.install.engine.Abort(context.Background()); err == nil {
		t.Fatal("active-link drift was accepted by abort")
	}
}

func TestAdminCredentialRotationIsResumeOnlyAfterVerifierBarrier(t *testing.T) {
	phases := []string{
		"change_admin_scram_verifier", "switch_active_database_credential_set",
		"terminate_pre_rotation_admin_sessions", "validate_admin_credential_transition",
		"revalidate_admin_rotation_invariants", "write_receipt",
	}
	for _, phase := range phases {
		t.Run(phase, func(t *testing.T) {
			fixture := newAdminRotationFixture(t)
			_, request := fixture.previewAndRequest(t)
			t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", phase)
			if _, err := fixture.install.engine.ApplyAdminCredentialRotation(context.Background(), request); err == nil {
				t.Fatalf("phase %s did not interrupt apply", phase)
			}
			journal, err := fixture.install.engine.readActiveJournal()
			if err != nil || journal.AdminCredentialRotation == nil || !journal.AdminCredentialRotation.VerifierChanged ||
				containsString(journal.AllowedActions, "abort") || !containsString(journal.AllowedActions, "resume") {
				t.Fatalf("phase=%s journal=%+v err=%v", phase, journal, err)
			}
			if _, err := fixture.install.engine.Abort(context.Background()); err == nil || !strings.Contains(err.Error(), "resume-only") {
				t.Fatalf("phase=%s abort error=%v", phase, err)
			}
			t.Setenv("XINSIGHTCTL_FAIL_AFTER_PHASE", "")
			receipt, err := fixture.install.engine.Resume(context.Background())
			if err != nil || receipt.Kind != adminCredentialRotationKind || receipt.Status != "completed" ||
				receipt.AdminCredentialRotation == nil {
				t.Fatalf("phase=%s receipt=%+v err=%v", phase, receipt, err)
			}
			assertAdminRotationNoServiceMutation(t, fixture)
			assertAdminRotationEvidenceContainsNoCredentials(t, fixture, journal.OperationID)
		})
	}
}

func TestAdminCredentialRotationCompletesWithoutProductOrRuntimeDrift(t *testing.T) {
	fixture := newAdminRotationFixture(t)
	preview, request := fixture.previewAndRequest(t)
	predecessor, err := LoadDatabaseCredentials(fixture.install.paths)
	if err != nil {
		t.Fatal(err)
	}
	accountBefore, err := os.ReadFile(fixture.install.paths.AccessAccountsFile)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := fixture.install.engine.ApplyAdminCredentialRotation(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != "completed" || receipt.PreviewSHA256 != preview.PreviewSHA256 ||
		receipt.State != preview.State || !receipt.Transition.SuccessorFreshConnection ||
		!receipt.Transition.PredecessorAuthRejected || !receipt.Transition.ValidationSessionClosed {
		t.Fatalf("receipt=%+v", receipt)
	}
	successor, err := LoadDatabaseCredentials(fixture.install.paths)
	if err != nil {
		t.Fatal(err)
	}
	if successor.SetRef == predecessor.SetRef || successor.ReaderDSN != predecessor.ReaderDSN ||
		successor.WorkspaceDSN != predecessor.WorkspaceDSN || successor.ImporterDSN != predecessor.ImporterDSN ||
		successor.AdminDSN == predecessor.AdminDSN {
		t.Fatal("four-role credential closure is invalid")
	}
	accountAfter, err := os.ReadFile(fixture.install.paths.AccessAccountsFile)
	if err != nil || !bytes.Equal(accountBefore, accountAfter) {
		t.Fatalf("demo.operator credential file changed: %v", err)
	}
	if _, err := os.Lstat(predecessor.SetRef); err != nil {
		t.Fatalf("predecessor credential set was not retained: %v", err)
	}
	assertAdminRotationNoServiceMutation(t, fixture)
	assertAdminRotationEvidenceContainsNoCredentials(t, fixture, receipt.OperationID)
}

func TestAdminCredentialRotationRequestIsStrictAndMetadataOnly(t *testing.T) {
	valid := []byte(`{"schema_version":"xinsight.admin-credential-rotation-request.v1","preview_sha256":"sha256:0000000000000000000000000000000000000000000000000000000000000000","authorization_ref":"decision://rotation","reason_code":"local_tooling_exposure"}`)
	if _, err := DecodeAdminCredentialRotationRequest(valid); err != nil {
		t.Fatal(err)
	}
	duplicate := bytes.Replace(valid, []byte(`"reason_code":`), []byte(`"reason_code":"local_tooling_exposure","reason_code":`), 1)
	if _, err := DecodeAdminCredentialRotationRequest(duplicate); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate request error=%v", err)
	}
	unknown := bytes.Replace(valid, []byte(`"reason_code":`), []byte(`"unexpected":true,"reason_code":`), 1)
	if _, err := DecodeAdminCredentialRotationRequest(unknown); err == nil {
		t.Fatal("unknown request field was accepted")
	}
}

func assertAdminRotationNoServiceMutation(t *testing.T, fixture adminRotationFixture) {
	t.Helper()
	for _, command := range fixture.runner.commands[fixture.beforeCommands:] {
		if command.Name != "systemctl" {
			continue
		}
		if len(command.Args) > 0 {
			switch command.Args[0] {
			case "start", "stop", "restart", "enable", "disable", "daemon-reload":
				t.Fatalf("admin credential rotation mutated a service: %+v", command)
			}
		}
	}
}

func assertAdminRotationEvidenceContainsNoCredentials(t *testing.T, fixture adminRotationFixture, operationID string) {
	t.Helper()
	predecessor, err := LoadDatabaseCredentialSet(fixture.install.paths, filepath.Join(fixture.install.paths.DatabaseSetsDir, "initial"))
	if err != nil {
		t.Fatal(err)
	}
	successor, err := LoadDatabaseCredentials(fixture.install.paths)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{
		fixture.install.engine.journalPath(operationID),
		filepath.Join(fixture.install.paths.ReceiptsDir, operationID+".json"),
		adminCredentialRotationRequestPath(fixture.install.paths, operationID),
	}
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{
			predecessor.ReaderDSN, predecessor.WorkspaceDSN, predecessor.ImporterDSN,
			predecessor.AdminDSN, predecessor.Admin.Password, successor.AdminDSN, successor.Admin.Password,
		} {
			if forbidden != "" && bytes.Contains(contents, []byte(forbidden)) {
				t.Fatalf("credential material appeared in %s", filepath.Base(path))
			}
		}
		if bytes.Contains(contents, []byte("SCRAM-SHA-256$")) || bytes.Contains(contents, []byte("postgres://")) {
			t.Fatalf("secret-shaped value appeared in %s", filepath.Base(path))
		}
		var decoded any
		if err := json.Unmarshal(contents, &decoded); err != nil {
			t.Fatalf("invalid evidence JSON %s: %v", path, err)
		}
	}
}

var _ AdminCredentialRotationDatabase = (*fakeAdminCredentialRotationDatabase)(nil)
