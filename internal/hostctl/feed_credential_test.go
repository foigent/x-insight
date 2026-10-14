package hostctl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestFeedCredentialPrepareMatchesFrozenBundleAndNeverEmitsToken(t *testing.T) {
	engine, paths, _ := newFeedCredentialFixture(t)
	entropy := make([]byte, 48)
	for index := 32; index < len(entropy); index++ {
		entropy[index] = 0
	}
	engine.Random = bytes.NewReader(entropy)

	registration, err := engine.PrepareFeedCredential(context.Background(), "", "")
	if err != nil {
		t.Fatalf("prepare feed credential: %v", err)
	}
	expected := FeedCredentialRegistration{
		SchemaVersion:  FeedCredentialRegistrationSchema,
		TokenFormat:    FeedCredentialTokenFormat,
		VerifierScheme: FeedCredentialVerifierScheme,
		KeyPrefix:      "ur-xf1-0000000000000",
		VerifierSHA256: "d14985e8cedc38985529b846e34f152448401b2b2e5e28db0f64bd8727c1ff07",
		OperationID:    "00000000000000000000000000000000",
	}
	if registration != expected {
		t.Fatalf("registration=%+v expected=%+v", registration, expected)
	}

	encoded, err := json.Marshal(registration)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	expectedFields := []string{
		"key_prefix",
		"operation_id",
		"schema_version",
		"token_format",
		"verifier_scheme",
		"verifier_sha256",
	}
	actualFields := make([]string, 0, len(fields))
	for key := range fields {
		actualFields = append(actualFields, key)
	}
	sortStrings(actualFields)
	if !reflect.DeepEqual(actualFields, expectedFields) {
		t.Fatalf("bundle fields=%v expected=%v", actualFields, expectedFields)
	}

	tokenBytes, err := os.ReadFile(paths.FeedCredentialNext)
	if err != nil {
		t.Fatalf("read staged credential in isolated test root: %v", err)
	}
	token := string(tokenBytes)
	if token != "ur-xf1-"+strings.Repeat("00", 32) ||
		len(tokenBytes) != feedCredentialTokenLength ||
		!feedCredentialTokenPattern.Match(tokenBytes) {
		t.Fatal("staged credential does not match the exact 256-bit token grammar")
	}
	stateBytes, err := os.ReadFile(paths.FeedCredentialState)
	if err != nil {
		t.Fatalf("read nonsecret operation state: %v", err)
	}
	status, err := engine.FeedCredentialStatus(context.Background())
	if err != nil {
		t.Fatalf("credential status: %v", err)
	}
	statusBytes, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	for label, contents := range map[string][]byte{
		"registration output": encoded,
		"operation state":     stateBytes,
		"status output":       statusBytes,
	} {
		if bytes.Contains(contents, tokenBytes) {
			t.Fatalf("%s leaked the raw feed credential", label)
		}
	}

	info, err := os.Lstat(paths.FeedCredentialNext)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	if info.Mode().Perm() != 0o600 || uint64(stat.Nlink) != 1 {
		t.Fatalf("staged credential mode=%o nlink=%d", info.Mode().Perm(), stat.Nlink)
	}
}

func TestFeedCredentialPrepareUsesIndependentEntropyAndResumesAfterCrash(t *testing.T) {
	engine, paths, _ := newFeedCredentialFixture(t)
	entropy := make([]byte, 48)
	for index := range entropy {
		entropy[index] = byte(index + 1)
	}
	engine.Random = bytes.NewReader(entropy)
	failOnce := true
	engine.FeedCredentialPhaseHook = func(phase string) error {
		if phase == "staged_secret_written" && failOnce {
			failOnce = false
			return errors.New("simulated crash")
		}
		return nil
	}

	if _, err := engine.PrepareFeedCredential(context.Background(), "", ""); err == nil ||
		!strings.Contains(err.Error(), "staged secret write") {
		t.Fatalf("expected post-write crash, got %v", err)
	}
	status, err := engine.FeedCredentialStatus(context.Background())
	if err != nil {
		t.Fatalf("status after crash: %v", err)
	}
	if status.Status != feedCredentialPendingPreparing ||
		status.PendingRegistration == nil ||
		status.RequiredAction != "prepare" ||
		!status.StagedFile.BindingMatches {
		t.Fatalf("status after crash=%+v", status)
	}
	stagedBefore, err := os.ReadFile(paths.FeedCredentialNext)
	if err != nil {
		t.Fatal(err)
	}
	if string(stagedBefore) != feedCredentialTokenPrefix+hexOf(entropy[:32]) ||
		status.PendingRegistration.OperationID != hexOf(entropy[32:]) {
		t.Fatalf(
			"credential/operation entropy split is not exact: operation=%s",
			status.PendingRegistration.OperationID,
		)
	}

	engine.FeedCredentialPhaseHook = nil
	resumed, err := engine.PrepareFeedCredential(context.Background(), "", "")
	if err != nil {
		t.Fatalf("resume prepare: %v", err)
	}
	if resumed != *status.PendingRegistration {
		t.Fatalf("resumed registration=%+v pending=%+v", resumed, status.PendingRegistration)
	}
	stagedAfter, err := os.ReadFile(paths.FeedCredentialNext)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stagedBefore, stagedAfter) {
		t.Fatal("prepare recovery generated a replacement secret")
	}
}

func TestFeedCredentialAbortRequiresExactBindingAndCleansOnlyStagedFile(t *testing.T) {
	engine, paths, _ := newFeedCredentialFixture(t)
	registration, err := engine.PrepareFeedCredential(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	wrongVerifier := strings.Repeat("f", 64)
	if wrongVerifier == registration.VerifierSHA256 {
		wrongVerifier = strings.Repeat("e", 64)
	}
	if _, err := engine.AbortFeedCredential(
		context.Background(),
		registration.OperationID,
		wrongVerifier,
	); err == nil {
		t.Fatal("abort accepted a mismatched verifier")
	}
	if _, err := os.Stat(paths.FeedCredentialNext); err != nil {
		t.Fatalf("wrong-binding abort changed staged file: %v", err)
	}

	report, err := engine.AbortFeedCredential(
		context.Background(),
		registration.OperationID,
		registration.VerifierSHA256,
	)
	if err != nil {
		t.Fatalf("abort exact pending operation: %v", err)
	}
	if report.Status != "absent" ||
		report.LastOperation == nil ||
		report.LastOperation.Status != "aborted" ||
		report.ActiveFile.Present ||
		report.StagedFile.Present {
		t.Fatalf("abort report=%+v", report)
	}
	if _, err := os.Lstat(paths.FeedCredentialNext); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged file remains after abort: %v", err)
	}
	if _, err := os.Lstat(paths.FeedCredentialFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("abort created or changed active file: %v", err)
	}
}

func TestFeedCredentialAbortResumesAfterStagedFileRemovalCrash(t *testing.T) {
	engine, paths, _ := newFeedCredentialFixture(t)
	registration, err := engine.PrepareFeedCredential(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	failOnce := true
	engine.FeedCredentialPhaseHook = func(phase string) error {
		if phase == "staged_secret_removed" && failOnce {
			failOnce = false
			return errors.New("simulated crash")
		}
		return nil
	}
	if _, err := engine.AbortFeedCredential(
		context.Background(),
		registration.OperationID,
		registration.VerifierSHA256,
	); err == nil || !strings.Contains(err.Error(), "staged secret removal") {
		t.Fatalf("expected abort interruption, got %v", err)
	}
	if _, err := os.Lstat(paths.FeedCredentialNext); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged file remains after interrupted removal: %v", err)
	}
	status, err := engine.FeedCredentialStatus(context.Background())
	if err != nil {
		t.Fatalf("status interrupted abort: %v", err)
	}
	if status.Status != feedCredentialPendingPrepared ||
		status.RequiredAction != "abort" ||
		status.RestartRequired {
		t.Fatalf("interrupted abort status=%+v", status)
	}

	engine.FeedCredentialPhaseHook = nil
	report, err := engine.AbortFeedCredential(
		context.Background(),
		registration.OperationID,
		registration.VerifierSHA256,
	)
	if err != nil {
		t.Fatalf("resume abort: %v", err)
	}
	if report.Status != "absent" ||
		report.LastOperation == nil ||
		report.LastOperation.Status != "aborted" {
		t.Fatalf("abort report=%+v", report)
	}
}

func TestFeedCredentialAbortRecoversCrashBeforeSecretCreation(t *testing.T) {
	engine, paths, _ := newFeedCredentialFixture(t)
	engine.FeedCredentialPhaseHook = func(phase string) error {
		if phase == "state_preparing" {
			return errors.New("simulated crash")
		}
		return nil
	}
	if _, err := engine.PrepareFeedCredential(context.Background(), "", ""); err == nil {
		t.Fatal("expected prepare interruption")
	}
	status, err := engine.FeedCredentialStatus(context.Background())
	if err != nil {
		t.Fatalf("status interrupted prepare: %v", err)
	}
	if status.PendingRegistration == nil ||
		status.RequiredAction != "abort" ||
		status.StagedFile.Present {
		t.Fatalf("interrupted status=%+v", status)
	}
	if _, err := os.Lstat(paths.FeedCredentialNext); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected staged file: %v", err)
	}

	engine.FeedCredentialPhaseHook = nil
	report, err := engine.AbortFeedCredential(
		context.Background(),
		status.PendingRegistration.OperationID,
		status.PendingRegistration.VerifierSHA256,
	)
	if err != nil {
		t.Fatalf("abort interrupted prepare: %v", err)
	}
	if report.Status != "absent" || report.LastOperation == nil || report.LastOperation.Status != "aborted" {
		t.Fatalf("abort report=%+v", report)
	}
}

func TestFeedCredentialAbortRecoversPartialExclusiveWrite(t *testing.T) {
	engine, paths, _ := newFeedCredentialFixture(t)
	engine.FeedCredentialPhaseHook = func(phase string) error {
		if phase == "state_preparing" {
			return errors.New("simulated process death before write")
		}
		return nil
	}
	if _, err := engine.PrepareFeedCredential(context.Background(), "", ""); err == nil {
		t.Fatal("expected prepare interruption")
	}
	if err := os.WriteFile(paths.FeedCredentialNext, []byte("ur-xf1-partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := engine.FeedCredentialStatus(context.Background())
	if err != nil {
		t.Fatalf("status partial staged write: %v", err)
	}
	if status.PendingRegistration == nil ||
		status.Status != feedCredentialPendingPreparing ||
		status.RequiredAction != "abort" ||
		!status.StagedFile.Present ||
		status.StagedFile.BindingMatches ||
		status.StagedFile.Metadata == nil ||
		status.StagedFile.Metadata.SizeBytes != int64(len("ur-xf1-partial")) {
		t.Fatalf("partial-write status=%+v", status)
	}

	engine.FeedCredentialPhaseHook = nil
	report, err := engine.AbortFeedCredential(
		context.Background(),
		status.PendingRegistration.OperationID,
		status.PendingRegistration.VerifierSHA256,
	)
	if err != nil {
		t.Fatalf("abort partial staged write: %v", err)
	}
	if report.Status != "absent" || report.StagedFile.Present {
		t.Fatalf("abort report=%+v", report)
	}
	if _, err := os.Lstat(paths.FeedCredentialNext); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial staged file remains: %v", err)
	}
}

func TestFeedCredentialInitialActivationRequiresExactPausedAuthentication(t *testing.T) {
	engine, paths, runner := newFeedCredentialFixture(t)
	engine.FeedCredentialAuthCheck = nil
	registration, err := engine.PrepareFeedCredential(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	staged, err := os.ReadFile(paths.FeedCredentialNext)
	if err != nil {
		t.Fatal(err)
	}
	// The root-operated legacy flow can already have an exact Connector
	// origin, while the installed predecessor has no managed network files.
	// Its real paused-source authentication must not depend on a different
	// process (the controller) having loaded FPV15 configuration.
	if err := os.WriteFile(paths.ConnectorEnvironment,
		[]byte(connectorTargetEnvironmentContents("https://198.18.0.2:9444")), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+string(staged) {
			t.Fatal("staged authentication did not use the prepared credential")
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte(
			`{"error":{"code":"completed_execution_feed.connection_paused","message":"Paused","retryable":true}}`,
		))
	}))
	defer server.Close()
	writeFeedCredentialRuntimeEnvironment(t, paths, server.URL+"/api/v1/integrations/xinsight/completed-executions")

	report, err := engine.ActivateFeedCredential(
		context.Background(),
		registration.OperationID,
		registration.VerifierSHA256,
	)
	if err != nil {
		t.Fatalf("activate after exact paused proof: %v", err)
	}
	if report.Status != "active" ||
		countCommand(runner.commands, "systemctl", "restart", feedWorkerUnitName) != 1 {
		t.Fatalf("report=%+v commands=%+v", report, runner.commands)
	}
	active, err := os.ReadFile(paths.FeedCredentialFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(active, staged) {
		t.Fatal("paused authentication proof did not preserve the staged bytes")
	}
}

func TestFeedCredentialRotation401PreservesOldAndStagedCredentials(t *testing.T) {
	engine, paths, runner := newFeedCredentialFixture(t)
	engine.FeedCredentialAuthCheck = nil
	var responseStatus atomic.Int64
	responseStatus.Store(http.StatusServiceUnavailable)
	var expectedToken atomic.Value
	expectedToken.Store("")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		expected, _ := expectedToken.Load().(string)
		if expected == "" || request.Header.Get("Authorization") != "Bearer "+expected {
			t.Fatal("authentication preflight used an unexpected credential")
		}
		writer.Header().Set("Content-Type", "application/json")
		status := int(responseStatus.Load())
		writer.WriteHeader(status)
		if status == http.StatusServiceUnavailable {
			_, _ = writer.Write([]byte(
				`{"error":{"code":"completed_execution_feed.connection_paused","message":"Paused","retryable":true}}`,
			))
			return
		}
		_, _ = writer.Write([]byte(
			`{"error":{"code":"auth.invalid","message":"Denied","retryable":false}}`,
		))
	}))
	defer server.Close()
	writeFeedCredentialRuntimeEnvironment(t, paths, server.URL+"/api/v1/integrations/xinsight/completed-executions")

	first, err := engine.PrepareFeedCredential(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	firstToken, err := os.ReadFile(paths.FeedCredentialNext)
	if err != nil {
		t.Fatal(err)
	}
	expectedToken.Store(string(firstToken))
	if _, err := engine.ActivateFeedCredential(
		context.Background(),
		first.OperationID,
		first.VerifierSHA256,
	); err != nil {
		t.Fatalf("activate initial credential: %v", err)
	}

	second, err := engine.PrepareFeedCredential(
		context.Background(),
		first.OperationID,
		first.VerifierSHA256,
	)
	if err != nil {
		t.Fatalf("prepare replacement credential: %v", err)
	}
	secondToken, err := os.ReadFile(paths.FeedCredentialNext)
	if err != nil {
		t.Fatal(err)
	}
	expectedToken.Store(string(secondToken))
	responseStatus.Store(http.StatusUnauthorized)

	if _, err := engine.ActivateFeedCredential(
		context.Background(),
		second.OperationID,
		second.VerifierSHA256,
	); !errors.Is(err, errFeedCredentialAuthPreflight) {
		t.Fatalf("rotation error=%v", err)
	}
	activeAfterFailure, err := os.ReadFile(paths.FeedCredentialFile)
	if err != nil {
		t.Fatal(err)
	}
	stagedAfterFailure, err := os.ReadFile(paths.FeedCredentialNext)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(activeAfterFailure, firstToken) ||
		!bytes.Equal(stagedAfterFailure, secondToken) {
		t.Fatal("401 crossed the active credential replacement barrier")
	}
	status, err := engine.FeedCredentialStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != feedCredentialPendingPrepared ||
		status.PendingRegistration == nil ||
		*status.PendingRegistration != second ||
		status.ActiveRegistration == nil ||
		*status.ActiveRegistration != first ||
		countCommand(runner.commands, "systemctl", "restart", feedWorkerUnitName) != 1 ||
		!runner.feedWorkerActive {
		t.Fatalf("status=%+v commands=%+v", status, runner.commands)
	}
}

func TestFeedCredentialActivationIsAtomicRestartOnlyAndCrashResumable(t *testing.T) {
	engine, paths, runner := newFeedCredentialFixture(t)
	registration, err := engine.PrepareFeedCredential(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	staged, err := os.ReadFile(paths.FeedCredentialNext)
	if err != nil {
		t.Fatal(err)
	}
	engine.FeedCredentialPhaseHook = func(phase string) error {
		if phase == "credential_replaced" {
			return errors.New("simulated crash")
		}
		return nil
	}
	if _, err := engine.ActivateFeedCredential(
		context.Background(),
		registration.OperationID,
		registration.VerifierSHA256,
	); err == nil {
		t.Fatal("expected activation interruption after atomic replace")
	}
	if _, err := os.Lstat(paths.FeedCredentialNext); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged file exists after atomic replace: %v", err)
	}
	active, err := os.ReadFile(paths.FeedCredentialFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(active, staged) {
		t.Fatal("atomic replace changed credential bytes")
	}
	status, err := engine.FeedCredentialStatus(context.Background())
	if err != nil {
		t.Fatalf("status interrupted activation: %v", err)
	}
	if status.Status != feedCredentialPendingActivating ||
		status.RequiredAction != "activate" ||
		!status.ActiveFile.BindingMatches {
		t.Fatalf("activation recovery status=%+v", status)
	}
	if containsCommand(runner.commands, "systemctl", "restart", feedWorkerUnitName) {
		t.Fatal("worker restarted before the injected post-replace barrier")
	}

	engine.FeedCredentialPhaseHook = nil
	report, err := engine.ActivateFeedCredential(
		context.Background(),
		registration.OperationID,
		registration.VerifierSHA256,
	)
	if err != nil {
		t.Fatalf("resume activation: %v", err)
	}
	if report.Status != "active" ||
		report.ActiveRegistration == nil ||
		*report.ActiveRegistration != registration ||
		report.RestartRequired ||
		!report.ActiveFile.BindingMatches {
		t.Fatalf("activation report=%+v", report)
	}
	if !containsCommand(runner.commands, "systemctl", "restart", feedWorkerUnitName) {
		t.Fatal("activation did not restart the feed worker")
	}
	for _, command := range runner.commands {
		if command.Name != "systemctl" {
			t.Fatalf("credential activation invoked unexpected command: %+v", command)
		}
		if reflect.DeepEqual(command.Args, []string{"enable", "--now", feedWorkerUnitName}) {
			t.Fatal("credential activation enabled the dormant-by-default unit")
		}
		for _, argument := range command.Args {
			if strings.Contains(string(active), argument) || strings.Contains(argument, string(active)) {
				t.Fatal("credential appeared in process arguments")
			}
		}
	}
	if runner.feedWorkerEnabled {
		t.Fatal("credential activation changed the unit enablement state")
	}
}

func TestFeedCredentialActivationResumesAfterWorkerRestartCrash(t *testing.T) {
	engine, _, runner := newFeedCredentialFixture(t)
	registration, err := engine.PrepareFeedCredential(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	failOnce := true
	engine.FeedCredentialPhaseHook = func(phase string) error {
		if phase == "worker_restarted" && failOnce {
			failOnce = false
			return errors.New("simulated crash")
		}
		return nil
	}
	if _, err := engine.ActivateFeedCredential(
		context.Background(),
		registration.OperationID,
		registration.VerifierSHA256,
	); err == nil || !strings.Contains(err.Error(), "worker restart") {
		t.Fatalf("expected post-restart interruption, got %v", err)
	}
	if countCommand(runner.commands, "systemctl", "restart", feedWorkerUnitName) != 1 {
		t.Fatalf("commands after interruption=%+v", runner.commands)
	}

	engine.FeedCredentialPhaseHook = nil
	report, err := engine.ActivateFeedCredential(
		context.Background(),
		registration.OperationID,
		registration.VerifierSHA256,
	)
	if err != nil {
		t.Fatalf("resume post-restart activation: %v", err)
	}
	if report.Status != "active" ||
		countCommand(runner.commands, "systemctl", "restart", feedWorkerUnitName) != 2 {
		t.Fatalf("report=%+v commands=%+v", report, runner.commands)
	}
}

func TestFeedCredentialRotationPreservesOldCredentialUntilExactActivation(t *testing.T) {
	engine, paths, runner := newFeedCredentialFixture(t)
	first, err := engine.PrepareFeedCredential(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ActivateFeedCredential(context.Background(), first.OperationID, first.VerifierSHA256); err != nil {
		t.Fatal(err)
	}
	firstToken, err := os.ReadFile(paths.FeedCredentialFile)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := engine.PrepareFeedCredential(context.Background(), first.OperationID, strings.Repeat("0", 64)); err == nil {
		t.Fatal("rotation accepted the wrong active verifier")
	}
	second, err := engine.PrepareFeedCredential(
		context.Background(),
		first.OperationID,
		first.VerifierSHA256,
	)
	if err != nil {
		t.Fatalf("prepare replacement: %v", err)
	}
	if second == first {
		t.Fatal("replacement reused the active registration")
	}
	activeBeforeSwitch, err := os.ReadFile(paths.FeedCredentialFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(activeBeforeSwitch, firstToken) {
		t.Fatal("prepare changed the active credential")
	}
	secondToken, err := os.ReadFile(paths.FeedCredentialNext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(secondToken, firstToken) {
		t.Fatal("replacement reused active credential bytes")
	}

	if _, err := engine.ActivateFeedCredential(context.Background(), second.OperationID, second.VerifierSHA256); err != nil {
		t.Fatalf("activate replacement: %v", err)
	}
	activeAfterSwitch, err := os.ReadFile(paths.FeedCredentialFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(activeAfterSwitch, secondToken) {
		t.Fatal("replacement activation did not atomically install staged bytes")
	}
	if _, err := os.Lstat(paths.FeedCredentialNext); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replacement staged file remains: %v", err)
	}
	if countCommand(runner.commands, "systemctl", "restart", feedWorkerUnitName) != 2 {
		t.Fatalf("restart commands=%+v", runner.commands)
	}
}

func TestFeedCredentialRejectsSymlinkAndHardLinkFiles(t *testing.T) {
	t.Run("symlink staged path", func(t *testing.T) {
		engine, paths, _ := newFeedCredentialFixture(t)
		if err := engine.ensureFeedCredentialDirectories(); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(paths.EtcDir, "unrelated.key")
		if err := os.WriteFile(target, []byte("ur-xf1-"+strings.Repeat("a", 64)), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, paths.FeedCredentialNext); err != nil {
			t.Fatal(err)
		}
		if _, err := engine.PrepareFeedCredential(context.Background(), "", ""); err == nil ||
			!strings.Contains(err.Error(), "metadata") {
			t.Fatalf("symlink staged file error=%v", err)
		}
	})

	t.Run("hard-linked staged file", func(t *testing.T) {
		engine, paths, _ := newFeedCredentialFixture(t)
		registration, err := engine.PrepareFeedCredential(context.Background(), "", "")
		if err != nil {
			t.Fatal(err)
		}
		other := filepath.Join(paths.EtcDir, "hard-link")
		if err := os.Link(paths.FeedCredentialNext, other); err != nil {
			t.Fatal(err)
		}
		if _, err := engine.ActivateFeedCredential(
			context.Background(),
			registration.OperationID,
			registration.VerifierSHA256,
		); err == nil || !strings.Contains(err.Error(), "link count") {
			t.Fatalf("hard-linked staged file error=%v", err)
		}
	})
}

func TestFeedCredentialOperationStateRejectsUnknownFieldsAndSymlinks(t *testing.T) {
	engine, paths, _ := newFeedCredentialFixture(t)
	if _, err := engine.PrepareFeedCredential(context.Background(), "", ""); err != nil {
		t.Fatal(err)
	}
	stateBytes, err := os.ReadFile(paths.FeedCredentialState)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	state["unknown"] = true
	modified, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.FeedCredentialState, modified, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.FeedCredentialStatus(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown operation state field error=%v", err)
	}

	if err := os.Remove(paths.FeedCredentialState); err != nil {
		t.Fatal(err)
	}
	backing := filepath.Join(paths.StateDir, "feed-state.backing")
	if err := os.WriteFile(backing, stateBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(backing, paths.FeedCredentialState); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.FeedCredentialStatus(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "metadata") {
		t.Fatalf("symlink operation state error=%v", err)
	}
}

func newFeedCredentialFixture(t *testing.T) (*Engine, Paths, *fakeRunner) {
	t.Helper()
	root := t.TempDir()
	t.Setenv(AllowTestRootEnv, "1")
	t.Setenv(TestRootEnv, root)
	paths, err := PathsFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine(paths)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{feedWorkerLoaded: true}
	engine.Runner = runner
	engine.Database = nil
	engine.FeedCredentialAuthCheck = func(context.Context, string) error {
		return nil
	}
	engine.Now = func() time.Time {
		return time.Date(2026, 7, 26, 4, 0, 0, 0, time.UTC)
	}
	return engine, paths, runner
}

func writeFeedCredentialRuntimeEnvironment(t *testing.T, paths Paths, endpoint string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(paths.FeedEnvironmentFile), 0o755); err != nil {
		t.Fatal(err)
	}
	contents := strings.Join([]string{
		"XINSIGHT_UNIROUTER_FEED_ENDPOINT=" + endpoint,
		"XINSIGHT_UNIROUTER_FEED_POLL_INTERVAL=5s",
		"XINSIGHT_UNIROUTER_FEED_ALLOW_PRIVATE_TUNNEL_HTTP=true",
		"",
	}, "\n")
	if err := os.WriteFile(paths.FeedEnvironmentFile, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func countCommand(commands []Command, name string, args ...string) int {
	count := 0
	for _, command := range commands {
		if command.Name == name && reflect.DeepEqual(command.Args, args) {
			count++
		}
	}
	return count
}

func sortStrings(values []string) {
	for index := 1; index < len(values); index++ {
		for current := index; current > 0 && values[current] < values[current-1]; current-- {
			values[current], values[current-1] = values[current-1], values[current]
		}
	}
}

func hexOf(value []byte) string {
	const digits = "0123456789abcdef"
	output := make([]byte, len(value)*2)
	for index, current := range value {
		output[index*2] = digits[current>>4]
		output[index*2+1] = digits[current&0x0f]
	}
	return string(output)
}
