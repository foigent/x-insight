package hostctl

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"syscall"
	"time"
	"unicode/utf8"

	foigentcontract "github.com/foigent/x-insight/contracts/foigent"
	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/checkpointepoch"
)

const (
	AdminCredentialRotationRequestSchema              = "xinsight.admin-credential-rotation-request.v1"
	AdminCredentialRotationPreviewSchema              = "xinsight.admin-credential-rotation-preview.v1"
	AdminCredentialRotationReceiptSchema              = "xinsight.admin-credential-rotation-receipt.v1"
	adminCredentialRotationJournalSchema              = "xinsight.admin-credential-rotation-journal.v1"
	AdminCredentialRotationReasonLocalToolingExposure = "local_tooling_exposure"
	adminCredentialRotationKind                       = "admin_credential_rotation"
	adminCredentialRotationRequestMaxBytes            = int64(16 << 10)
)

func AdminCredentialRotationRequestMaxBytes() int64 {
	return adminCredentialRotationRequestMaxBytes
}

var adminRotationAuthorizationPattern = regexp.MustCompile(`^decision://[A-Za-z0-9][A-Za-z0-9._~:/-]{0,223}$`)

type AdminCredentialRotationRequest struct {
	SchemaVersion    string `json:"schema_version"`
	PreviewSHA256    string `json:"preview_sha256"`
	AuthorizationRef string `json:"authorization_ref"`
	ReasonCode       string `json:"reason_code"`
}

type AdminCredentialRotationProtocolIdentity struct {
	Version       string `json:"version"`
	Tag           string `json:"tag"`
	ReleaseDigest string `json:"release_digest"`
}

type AdminCredentialRotationStateFingerprint struct {
	PostgresMajor               int    `json:"postgres_major"`
	DatabaseSHA256              string `json:"database_sha256"`
	PublicTableCount            int    `json:"public_table_count"`
	SchemaContractVersion       string `json:"schema_contract_version"`
	SchemaContractSHA256        string `json:"schema_contract_sha256"`
	SchemaLayoutSHA256          string `json:"schema_layout_sha256"`
	MigrationLedgerSHA256       string `json:"migration_ledger_sha256"`
	ProductStateSHA256          string `json:"product_state_sha256"`
	ConnectorStateSHA256        string `json:"connector_state_sha256"`
	CheckpointStateSHA256       string `json:"checkpoint_state_sha256"`
	ReleaseCatalogSHA256        string `json:"release_catalog_sha256"`
	AccountFileSHA256           string `json:"account_file_sha256"`
	DemoOperatorIdentitySHA256  string `json:"demo_operator_identity_sha256"`
	ConfigurationSHA256         string `json:"configuration_sha256"`
	ConnectorFileMetadataSHA256 string `json:"connector_file_metadata_sha256"`
}

type AdminCredentialRotationPlannedPostState struct {
	ActiveSetIsSuccessor      bool `json:"active_set_is_successor"`
	PredecessorSealedInactive bool `json:"predecessor_sealed_inactive"`
	MainServiceRemainsRunning bool `json:"main_service_remains_running"`
	FeedWorkersRemainDormant  bool `json:"feed_workers_remain_inactive_disabled"`
	ServicesRestarted         int  `json:"services_restarted"`
	ProductWrites             int  `json:"product_writes"`
	PollsOrTraffic            int  `json:"polls_or_traffic"`
}

type AdminCredentialRotationPreviewReport struct {
	SchemaVersion    string                                  `json:"schema_version"`
	Supported        bool                                    `json:"supported"`
	Mutating         bool                                    `json:"mutating"`
	Release          CleanBaselineReleaseIdentity            `json:"release"`
	ConfigSchema     string                                  `json:"config_schema"`
	Protocol         AdminCredentialRotationProtocolIdentity `json:"protocol"`
	ActiveCredential AdminCredentialSetSummary               `json:"active_credential_set"`
	Database         AdminDatabaseBoundary                   `json:"database"`
	State            AdminCredentialRotationStateFingerprint `json:"state"`
	Workers          []CleanBaselineWorkerPreview            `json:"workers"`
	MainServicePID   string                                  `json:"main_service_pid"`
	AuthorizationRef string                                  `json:"authorization_ref"`
	ReasonCode       string                                  `json:"reason_code"`
	PlannedPostState AdminCredentialRotationPlannedPostState `json:"planned_post_state"`
	Checks           []CheckResult                           `json:"checks"`
	PreviewSHA256    string                                  `json:"preview_sha256"`
	GeneratedAt      time.Time                               `json:"generated_at"`
}

type AdminCredentialRotationJournal struct {
	SchemaVersion       string                               `json:"schema_version"`
	Preview             AdminCredentialRotationPreviewReport `json:"preview"`
	RequestSHA256       string                               `json:"request_sha256"`
	SuccessorCredential *AdminCredentialSetSummary           `json:"successor_credential_set,omitempty"`
	VerifierChanged     bool                                 `json:"verifier_changed"`
	SessionsTerminated  int                                  `json:"sessions_terminated"`
	TransitionProof     *AdminCredentialTransitionProof      `json:"transition_proof,omitempty"`
}

type AdminCredentialRotationDurableEvidence struct {
	SchemaVersion         string                                  `json:"schema_version"`
	PreviewSHA256         string                                  `json:"preview_sha256"`
	RequestSHA256         string                                  `json:"request_sha256"`
	AuthorizationRef      string                                  `json:"authorization_ref"`
	ReasonCode            string                                  `json:"reason_code"`
	PredecessorCredential AdminCredentialSetSummary               `json:"predecessor_credential_set"`
	SuccessorCredential   AdminCredentialSetSummary               `json:"successor_credential_set"`
	BeforeState           AdminCredentialRotationStateFingerprint `json:"before_state"`
	AfterState            AdminCredentialRotationStateFingerprint `json:"after_state"`
	Transition            AdminCredentialTransitionProof          `json:"transition"`
	SessionsTerminated    int                                     `json:"sessions_terminated"`
	MainServicePID        string                                  `json:"main_service_pid"`
	Workers               []CleanBaselineWorkerPreview            `json:"workers"`
	PredecessorRetained   bool                                    `json:"predecessor_retained"`
	PredecessorInactive   bool                                    `json:"predecessor_inactive"`
	ServicesRestarted     int                                     `json:"services_restarted"`
	ProductWrites         int                                     `json:"product_writes"`
	PollsOrTraffic        int                                     `json:"polls_or_traffic"`
}

type AdminCredentialRotationReceipt struct {
	SchemaVersion         string                                  `json:"schema_version"`
	OperationID           string                                  `json:"operation_id"`
	Status                string                                  `json:"status"`
	Release               CleanBaselineReleaseIdentity            `json:"release"`
	PreviewSHA256         string                                  `json:"preview_sha256"`
	RequestSHA256         string                                  `json:"request_sha256"`
	PredecessorCredential AdminCredentialSetSummary               `json:"predecessor_credential_set"`
	SuccessorCredential   AdminCredentialSetSummary               `json:"successor_credential_set"`
	Transition            AdminCredentialTransitionProof          `json:"transition"`
	State                 AdminCredentialRotationStateFingerprint `json:"state"`
	Workers               []CleanBaselineWorkerPreview            `json:"workers"`
	Checks                []CheckResult                           `json:"checks"`
	DurableReceiptSHA256  string                                  `json:"durable_receipt_sha256"`
	CompletedAt           time.Time                               `json:"completed_at"`
}

type adminCredentialRotationPlan struct {
	State       InstallState
	Current     verifiedTarget
	Predecessor DatabaseCredentials
	Successor   DatabaseCredentials
	Preview     AdminCredentialRotationPreviewReport
}

func DecodeAdminCredentialRotationRequest(payload []byte) (AdminCredentialRotationRequest, error) {
	if len(payload) == 0 || int64(len(payload)) > adminCredentialRotationRequestMaxBytes || !utf8.Valid(payload) {
		return AdminCredentialRotationRequest{}, fmt.Errorf("admin credential rotation request encoding is invalid")
	}
	if err := rejectDuplicateAdminRotationJSONFields(payload); err != nil {
		return AdminCredentialRotationRequest{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var request AdminCredentialRotationRequest
	if err := decoder.Decode(&request); err != nil {
		return AdminCredentialRotationRequest{}, fmt.Errorf("admin credential rotation request is invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return AdminCredentialRotationRequest{}, fmt.Errorf("admin credential rotation request contains trailing content")
	}
	if err := validateAdminCredentialRotationRequest(request); err != nil {
		return AdminCredentialRotationRequest{}, err
	}
	return request, nil
}

func validateAdminCredentialRotationRequest(request AdminCredentialRotationRequest) error {
	if request.SchemaVersion != AdminCredentialRotationRequestSchema ||
		!sha256ReferencePattern.MatchString(request.PreviewSHA256) ||
		!adminRotationAuthorizationPattern.MatchString(request.AuthorizationRef) ||
		request.ReasonCode != AdminCredentialRotationReasonLocalToolingExposure {
		return fmt.Errorf("admin credential rotation request identity is invalid")
	}
	return nil
}

func rejectDuplicateAdminRotationJSONFields(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	var scan func(string) error
	scan = func(path string) error {
		token, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("admin credential rotation request is invalid")
		}
		delimiter, composite := token.(json.Delim)
		if !composite {
			return nil
		}
		switch delimiter {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				key, ok := keyToken.(string)
				if err != nil || !ok {
					return fmt.Errorf("admin credential rotation request is invalid")
				}
				if _, exists := seen[key]; exists {
					return fmt.Errorf("admin credential rotation request contains a duplicate field at %s", path)
				}
				seen[key] = struct{}{}
				if err := scan(path + "." + key); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := scan(path + "[]"); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		default:
			return fmt.Errorf("admin credential rotation request is invalid")
		}
	}
	if err := scan("$"); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("admin credential rotation request contains trailing content")
	}
	return nil
}

func (engine *Engine) PreviewAdminCredentialRotation(
	ctx context.Context,
	authorizationRef string,
	reasonCode string,
) (AdminCredentialRotationPreviewReport, error) {
	if err := requireAdminCredentialRotationRoot(engine.Paths); err != nil {
		return AdminCredentialRotationPreviewReport{}, err
	}
	report, _, err := engine.prepareAdminCredentialRotation(ctx, authorizationRef, reasonCode)
	return report, err
}

func (engine *Engine) prepareAdminCredentialRotation(
	ctx context.Context,
	authorizationRef string,
	reasonCode string,
) (AdminCredentialRotationPreviewReport, adminCredentialRotationPlan, error) {
	empty := AdminCredentialRotationPreviewReport{
		SchemaVersion: AdminCredentialRotationPreviewSchema,
		Supported:     false, Mutating: false, GeneratedAt: engine.now(),
	}
	if !adminRotationAuthorizationPattern.MatchString(authorizationRef) ||
		reasonCode != AdminCredentialRotationReasonLocalToolingExposure {
		return empty, adminCredentialRotationPlan{}, fmt.Errorf("admin credential rotation authority is invalid")
	}
	if err := engine.ensureNoOutstandingOperation(); err != nil {
		return empty, adminCredentialRotationPlan{}, err
	}
	if _, err := detectSupportedHost(engine.Paths); err != nil {
		return empty, adminCredentialRotationPlan{}, err
	}
	state, current, err := engine.currentAdminCredentialRotationRelease()
	if err != nil {
		return empty, adminCredentialRotationPlan{}, err
	}
	credentials, err := LoadDatabaseCredentials(engine.Paths)
	if err != nil {
		return empty, adminCredentialRotationPlan{}, err
	}
	credentialSummary, err := InspectAdminCredentialSet(engine.Paths, credentials.SetRef)
	if err != nil {
		return empty, adminCredentialRotationPlan{}, err
	}
	manager, err := engine.adminCredentialRotationDatabase()
	if err != nil {
		return empty, adminCredentialRotationPlan{}, err
	}
	databaseBoundary, err := manager.InspectAdminBoundary(ctx, credentials)
	if err != nil {
		return empty, adminCredentialRotationPlan{}, err
	}
	if credentialSummary.AdminIdentitySHA256 == "" || databaseBoundary.AdminRoleAlias != adminDatabaseRole {
		return empty, adminCredentialRotationPlan{}, fmt.Errorf("admin credential member identity is invalid")
	}
	if databaseBoundary.AdminSessions != 0 {
		return empty, adminCredentialRotationPlan{}, fmt.Errorf("admin credential rotation requires zero concurrent admin sessions")
	}
	fingerprint, err := engine.adminCredentialRotationFingerprint(ctx, current, credentials)
	if err != nil {
		return empty, adminCredentialRotationPlan{}, err
	}
	workers, mainPID, err := engine.adminCredentialRotationRuntimeBoundary(ctx)
	if err != nil {
		return empty, adminCredentialRotationPlan{}, err
	}
	protocol, err := adminCredentialRotationProtocol()
	if err != nil {
		return empty, adminCredentialRotationPlan{}, err
	}
	report := AdminCredentialRotationPreviewReport{
		SchemaVersion: AdminCredentialRotationPreviewSchema,
		Supported:     true, Mutating: false,
		Release: cleanBaselineReleaseIdentity(state.Current), ConfigSchema: state.Current.ConfigSchema,
		Protocol: protocol, ActiveCredential: credentialSummary, Database: databaseBoundary,
		State: fingerprint, Workers: workers, MainServicePID: mainPID,
		AuthorizationRef: authorizationRef, ReasonCode: reasonCode,
		PlannedPostState: AdminCredentialRotationPlannedPostState{
			ActiveSetIsSuccessor: true, PredecessorSealedInactive: true,
			MainServiceRemainsRunning: true, FeedWorkersRemainDormant: true,
		},
		Checks: []CheckResult{
			{Name: "release_identity", Status: "pass", Detail: state.Current.Commit},
			{Name: "schema_contract", Status: "pass", Detail: fingerprint.SchemaContractSHA256},
			{Name: "database_roles", Status: "pass", Detail: databaseBoundary.AdminIdentitySHA256},
			{Name: "credential_set", Status: "pass", Detail: credentialSummary.CanonicalSHA256},
			{Name: "workers", Status: "pass", Detail: "both_inactive_disabled_no_poll"},
			{Name: "planned_mutation", Status: "pass", Detail: "admin_verifier_and_active_database_set_link_only_no_restart_no_product_write"},
		},
		GeneratedAt: empty.GeneratedAt,
	}
	report.PreviewSHA256, err = adminCredentialRotationPreviewDigest(report)
	if err != nil {
		return empty, adminCredentialRotationPlan{}, err
	}
	return report, adminCredentialRotationPlan{
		State: state, Current: current, Predecessor: credentials, Preview: report,
	}, nil
}

func (engine *Engine) ApplyAdminCredentialRotation(
	ctx context.Context,
	request AdminCredentialRotationRequest,
) (AdminCredentialRotationReceipt, error) {
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return AdminCredentialRotationReceipt{}, err
	}
	if err := validateAdminCredentialRotationRequest(request); err != nil {
		return AdminCredentialRotationReceipt{}, err
	}
	var receipt AdminCredentialRotationReceipt
	err := engine.withOperationLock(func() error {
		preview, plan, err := engine.prepareAdminCredentialRotation(
			ctx, request.AuthorizationRef, request.ReasonCode,
		)
		if err != nil {
			return err
		}
		if preview.PreviewSHA256 != request.PreviewSHA256 {
			return fmt.Errorf("admin credential rotation preview digest changed; obtain a new authorization")
		}
		requestSHA256, err := adminCredentialRotationRequestDigest(request)
		if err != nil {
			return err
		}
		before := plan.State.Current
		journal, err := engine.startJournal(adminCredentialRotationKind, before.ArtifactPath, "", &before, &before)
		if err != nil {
			return err
		}
		journal.DatabaseSetBefore = plan.Predecessor.SetRef
		journal.DatabaseSetAfter = filepath.Join(
			engine.Paths.DatabaseSetsDir,
			"admin-rotation-"+journal.OperationID,
		)
		journal.ReasonCode = request.ReasonCode
		journal.ServicePIDBefore = preview.MainServicePID
		journal.AdminCredentialRotation = &AdminCredentialRotationJournal{
			SchemaVersion: adminCredentialRotationJournalSchema,
			Preview:       preview, RequestSHA256: requestSHA256,
		}
		if err := engine.updateJournal(journal); err != nil {
			return err
		}
		if err := stageAdminCredentialRotationRequest(engine.Paths, journal.OperationID, request, requestSHA256); err != nil {
			_ = engine.failJournal(journal, "admin_credential_rotation_request_staging_failed", "recover", "abort")
			return err
		}
		receipt, err = engine.executeAdminCredentialRotation(ctx, journal, plan)
		return err
	})
	return receipt, err
}

func (engine *Engine) executeAdminCredentialRotation(
	ctx context.Context,
	journal *Journal,
	plan adminCredentialRotationPlan,
) (receipt AdminCredentialRotationReceipt, resultErr error) {
	if journal.AdminCredentialRotation == nil {
		return AdminCredentialRotationReceipt{}, fmt.Errorf("admin credential rotation journal evidence is missing")
	}
	manager, err := engine.adminCredentialRotationDatabase()
	if err != nil {
		return AdminCredentialRotationReceipt{}, err
	}
	operationFinalized := false
	simulatedProcessDeath := false
	defer func() {
		if resultErr == nil || operationFinalized || simulatedProcessDeath {
			return
		}
		allowed := []string{"recover", "resume"}
		barrier, barrierErr := engine.adminCredentialRotationBarrierState(ctx, journal, plan)
		if barrierErr != nil {
			_ = engine.failJournal(journal, "admin_credential_rotation_state_ambiguous", allowed...)
			resultErr = fmt.Errorf("admin_credential_rotation_state_ambiguous: %w", resultErr)
			return
		}
		if !barrier {
			allowed = append(allowed, "abort")
		}
		_ = engine.failJournal(journal, "admin_credential_rotation_failed", allowed...)
		resultErr = fmt.Errorf("admin_credential_rotation_failed: %w", resultErr)
	}()

	if err := engine.ensureControlDirectories(); err != nil {
		return AdminCredentialRotationReceipt{}, err
	}

	if !containsString(journal.CompletedPhases, "stage_admin_successor_credential_set") {
		if err := engine.beginAdminCredentialRotationPhase(journal, "stage_admin_successor_credential_set"); err != nil {
			return AdminCredentialRotationReceipt{}, err
		}
	}
	successor, successorSummary, err := StageAdminCredentialSet(
		engine.Paths, plan.Predecessor, journal.DatabaseSetAfter, engine.adminCredentialRotationRandomReader(),
	)
	if err != nil {
		return AdminCredentialRotationReceipt{}, err
	}
	plan.Successor = successor
	journal.AdminCredentialRotation.SuccessorCredential = &successorSummary
	if err := engine.updateJournal(journal); err != nil {
		return AdminCredentialRotationReceipt{}, err
	}
	if !containsString(journal.CompletedPhases, "stage_admin_successor_credential_set") {
		if err := engine.completePhase(journal, "stage_admin_successor_credential_set"); err != nil {
			return AdminCredentialRotationReceipt{}, err
		}
	}

	if !journal.AdminCredentialRotation.VerifierChanged {
		if err := engine.runAdminCredentialRotationPhase(journal, "revalidate_pre_verifier_boundary", func() error {
			active, err := readAbsoluteSymlink(engine.Paths.DatabaseCurrentLink)
			if err != nil || active != journal.DatabaseSetBefore {
				return fmt.Errorf("predecessor database credential set is not active before verifier change")
			}
			predecessorSummary, err := InspectAdminCredentialSet(engine.Paths, journal.DatabaseSetBefore)
			if err != nil || !reflect.DeepEqual(predecessorSummary, plan.Preview.ActiveCredential) {
				return fmt.Errorf("predecessor database credential set changed before verifier change")
			}
			successorReadback, err := InspectAdminCredentialSet(engine.Paths, journal.DatabaseSetAfter)
			if err != nil || !reflect.DeepEqual(successorReadback, successorSummary) ||
				verifyAdminCredentialInheritedClosure(predecessorSummary, successorReadback) != nil {
				return fmt.Errorf("successor database credential set changed before verifier change")
			}
			fingerprint, err := engine.adminCredentialRotationFingerprint(ctx, plan.Current, plan.Predecessor)
			if err != nil || fingerprint != plan.Preview.State {
				return fmt.Errorf("product, schema, account, connector, or checkpoint state changed before verifier change")
			}
			workers, mainPID, err := engine.adminCredentialRotationRuntimeBoundary(ctx)
			if err != nil || mainPID != journal.ServicePIDBefore || !equalAdminRotationWorkers(workers, plan.Preview.Workers) {
				return fmt.Errorf("service or worker boundary changed before verifier change")
			}
			boundary, err := manager.InspectAdminBoundary(ctx, plan.Predecessor)
			if err != nil || boundary.AdminSessions != 0 ||
				boundary.AdminIdentitySHA256 != plan.Preview.Database.AdminIdentitySHA256 {
				return fmt.Errorf("admin credential rotation requires a quiesced unchanged admin boundary")
			}
			return nil
		}); err != nil {
			return AdminCredentialRotationReceipt{}, err
		}
	}

	if !containsString(journal.CompletedPhases, "change_admin_scram_verifier") {
		if err := engine.beginAdminCredentialRotationPhase(journal, "change_admin_scram_verifier"); err != nil {
			return AdminCredentialRotationReceipt{}, err
		}
	}
	transition, err := manager.ProbeAdminCredentialTransition(ctx, plan.Predecessor, plan.Successor)
	if err != nil {
		return AdminCredentialRotationReceipt{}, err
	}
	if transition == AdminCredentialTransitionPredecessor {
		if journal.AdminCredentialRotation.VerifierChanged {
			return AdminCredentialRotationReceipt{}, fmt.Errorf("admin verifier barrier regressed to its predecessor")
		}
		if err := manager.ChangeAdminVerifier(
			ctx, plan.Predecessor, plan.Successor, engine.adminCredentialRotationRandomReader(),
		); err != nil {
			return AdminCredentialRotationReceipt{}, err
		}
		transition, err = manager.ProbeAdminCredentialTransition(ctx, plan.Predecessor, plan.Successor)
		if err != nil {
			return AdminCredentialRotationReceipt{}, err
		}
	}
	if transition != AdminCredentialTransitionSuccessor {
		return AdminCredentialRotationReceipt{}, fmt.Errorf("successor admin verifier did not become authoritative")
	}
	if err := maybeFailAdminCredentialRotation(
		engine.Paths,
		"after_successor_probe_before_journal",
	); err != nil {
		// This test-only boundary emulates the process disappearing after
		// PostgreSQL committed the verifier but before any response-derived
		// journal state can be persisted. Deliberately skip the deferred
		// failure journal update so recovery must infer the external barrier.
		simulatedProcessDeath = true
		return AdminCredentialRotationReceipt{}, err
	}
	journal.AdminCredentialRotation.VerifierChanged = true
	journal.AllowedActions = []string{"recover", "resume"}
	if err := engine.updateJournal(journal); err != nil {
		return AdminCredentialRotationReceipt{}, err
	}
	if !containsString(journal.CompletedPhases, "change_admin_scram_verifier") {
		if err := engine.completePhase(journal, "change_admin_scram_verifier"); err != nil {
			return AdminCredentialRotationReceipt{}, err
		}
	}

	if err := engine.runAdminCredentialRotationPhase(journal, "switch_active_database_credential_set", func() error {
		active, err := readAbsoluteSymlink(engine.Paths.DatabaseCurrentLink)
		if err != nil {
			return err
		}
		switch active {
		case journal.DatabaseSetBefore:
			return atomicSymlink(journal.DatabaseSetAfter, engine.Paths.DatabaseCurrentLink)
		case journal.DatabaseSetAfter:
			return nil
		default:
			return fmt.Errorf("active database credential set changed outside the operation")
		}
	}); err != nil {
		return AdminCredentialRotationReceipt{}, err
	}

	if err := engine.runAdminCredentialRotationPhase(journal, "terminate_pre_rotation_admin_sessions", func() error {
		terminated, err := manager.TerminateOtherAdminSessions(ctx, plan.Successor)
		if err != nil {
			return err
		}
		if journal.AdminCredentialRotation.SessionsTerminated == 0 && terminated > 0 {
			journal.AdminCredentialRotation.SessionsTerminated = terminated
			return engine.updateJournal(journal)
		}
		return nil
	}); err != nil {
		return AdminCredentialRotationReceipt{}, err
	}

	if err := engine.runAdminCredentialRotationPhase(journal, "validate_admin_credential_transition", func() error {
		proof, err := manager.ValidateAdminCredentialTransition(ctx, plan.Predecessor, plan.Successor)
		if err != nil {
			return err
		}
		journal.AdminCredentialRotation.TransitionProof = &proof
		return engine.updateJournal(journal)
	}); err != nil {
		return AdminCredentialRotationReceipt{}, err
	}

	var afterFingerprint AdminCredentialRotationStateFingerprint
	var workers []CleanBaselineWorkerPreview
	if err := engine.runAdminCredentialRotationPhase(journal, "revalidate_admin_rotation_invariants", func() error {
		state, current, err := engine.currentAdminCredentialRotationRelease()
		if err != nil || state.Current != plan.State.Current || current.Identity != plan.Current.Identity {
			return fmt.Errorf("release identity changed during admin credential rotation")
		}
		active, err := readAbsoluteSymlink(engine.Paths.DatabaseCurrentLink)
		if err != nil || active != journal.DatabaseSetAfter {
			return fmt.Errorf("successor database credential set is not active")
		}
		predecessorSummary, err := InspectAdminCredentialSet(engine.Paths, journal.DatabaseSetBefore)
		if err != nil || !reflect.DeepEqual(predecessorSummary, plan.Preview.ActiveCredential) {
			return fmt.Errorf("predecessor database credential set changed")
		}
		successorSummary, err := InspectAdminCredentialSet(engine.Paths, journal.DatabaseSetAfter)
		if err != nil || journal.AdminCredentialRotation.SuccessorCredential == nil ||
			!reflect.DeepEqual(successorSummary, *journal.AdminCredentialRotation.SuccessorCredential) {
			return fmt.Errorf("successor database credential set changed")
		}
		if err := verifyAdminCredentialInheritedClosure(predecessorSummary, successorSummary); err != nil {
			return err
		}
		afterFingerprint, err = engine.adminCredentialRotationFingerprint(ctx, current, plan.Successor)
		if err != nil || afterFingerprint != plan.Preview.State {
			return fmt.Errorf("product, schema, account, connector, or checkpoint state changed during admin credential rotation")
		}
		workers, mainPID, err := engine.adminCredentialRotationRuntimeBoundary(ctx)
		if err != nil || mainPID != journal.ServicePIDBefore || !equalAdminRotationWorkers(workers, plan.Preview.Workers) {
			return fmt.Errorf("service or worker runtime boundary changed during admin credential rotation")
		}
		boundary, err := manager.InspectAdminBoundary(ctx, plan.Successor)
		if err != nil || boundary.AdminSessions != 0 ||
			boundary.AdminIdentitySHA256 != plan.Preview.Database.AdminIdentitySHA256 {
			return fmt.Errorf("admin database readback differs after credential rotation")
		}
		return nil
	}); err != nil {
		return AdminCredentialRotationReceipt{}, err
	}

	if journal.AdminCredentialRotation.TransitionProof == nil ||
		journal.AdminCredentialRotation.SuccessorCredential == nil {
		return AdminCredentialRotationReceipt{}, fmt.Errorf("admin credential rotation durable proof is incomplete")
	}
	checks := []CheckResult{
		{Name: "preview_authorization", Status: "pass", Detail: plan.Preview.PreviewSHA256},
		{Name: "admin_verifier", Status: "pass", Detail: "product_generated_scram_successor"},
		{Name: "predecessor_authentication", Status: "pass", Detail: "expected_authentication_rejection"},
		{Name: "active_database_set", Status: "pass", Detail: journal.AdminCredentialRotation.SuccessorCredential.CanonicalSHA256},
		{Name: "inherited_credentials", Status: "pass", Detail: "reader_workspace_importer_byte_identical"},
		{Name: "product_state", Status: "pass", Detail: afterFingerprint.ProductStateSHA256},
		{Name: "workers", Status: "pass", Detail: "both_inactive_disabled_no_poll"},
		{Name: "service_continuity", Status: "pass", Detail: "main_pid_unchanged_zero_restarts"},
	}
	evidence := &AdminCredentialRotationDurableEvidence{
		SchemaVersion:    AdminCredentialRotationReceiptSchema,
		PreviewSHA256:    plan.Preview.PreviewSHA256,
		RequestSHA256:    journal.AdminCredentialRotation.RequestSHA256,
		AuthorizationRef: plan.Preview.AuthorizationRef, ReasonCode: journal.ReasonCode,
		PredecessorCredential: plan.Preview.ActiveCredential,
		SuccessorCredential:   *journal.AdminCredentialRotation.SuccessorCredential,
		BeforeState:           plan.Preview.State, AfterState: afterFingerprint,
		Transition:         *journal.AdminCredentialRotation.TransitionProof,
		SessionsTerminated: journal.AdminCredentialRotation.SessionsTerminated,
		MainServicePID:     journal.ServicePIDBefore, Workers: workers,
		PredecessorRetained: true, PredecessorInactive: true,
	}
	baseReceipt := OperationReceipt{
		SchemaVersion: ReceiptSchema, OperationID: journal.OperationID,
		Kind: adminCredentialRotationKind, Status: "completed",
		Before: &plan.State.Current, After: &plan.State.Current,
		DatabaseSetRef: journal.DatabaseSetAfter, ReasonCode: journal.ReasonCode,
		AdminCredentialRotation: evidence, Checks: checks,
	}
	if err := engine.finalizeOperation(journal, &baseReceipt); err != nil {
		return AdminCredentialRotationReceipt{}, err
	}
	operationFinalized = true
	durableSHA256, _, err := fileSHA256(
		filepath.Join(engine.Paths.ReceiptsDir, journal.OperationID+".json"),
		maxControlFileBytes,
	)
	if err != nil {
		return AdminCredentialRotationReceipt{}, err
	}
	return AdminCredentialRotationReceipt{
		SchemaVersion: AdminCredentialRotationReceiptSchema,
		OperationID:   journal.OperationID, Status: "completed",
		Release: plan.Preview.Release, PreviewSHA256: plan.Preview.PreviewSHA256,
		RequestSHA256:         journal.AdminCredentialRotation.RequestSHA256,
		PredecessorCredential: plan.Preview.ActiveCredential,
		SuccessorCredential:   *journal.AdminCredentialRotation.SuccessorCredential,
		Transition:            *journal.AdminCredentialRotation.TransitionProof,
		State:                 afterFingerprint, Workers: workers, Checks: checks,
		DurableReceiptSHA256: durableSHA256, CompletedAt: baseReceipt.CompletedAt,
	}, nil
}

func (engine *Engine) resumeAdminCredentialRotationOperation(
	ctx context.Context,
	journal *Journal,
) (OperationReceipt, error) {
	plan, err := engine.resumeAdminCredentialRotationPlan(ctx, journal)
	if err != nil {
		return OperationReceipt{}, err
	}
	if _, err := engine.executeAdminCredentialRotation(ctx, journal, plan); err != nil {
		return OperationReceipt{}, err
	}
	return engine.readAdminCredentialRotationDurableReceipt(*journal)
}

func (engine *Engine) resumeAdminCredentialRotationPlan(
	ctx context.Context,
	journal *Journal,
) (adminCredentialRotationPlan, error) {
	if journal.Kind != adminCredentialRotationKind || journal.Before == nil || journal.Target == nil ||
		journal.AdminCredentialRotation == nil ||
		journal.AdminCredentialRotation.SchemaVersion != adminCredentialRotationJournalSchema ||
		journal.DatabaseSetBefore == "" || journal.DatabaseSetAfter == "" ||
		journal.ReasonCode != AdminCredentialRotationReasonLocalToolingExposure {
		return adminCredentialRotationPlan{}, fmt.Errorf("admin credential rotation journal is invalid")
	}
	expectedRequest := AdminCredentialRotationRequest{
		SchemaVersion:    AdminCredentialRotationRequestSchema,
		PreviewSHA256:    journal.AdminCredentialRotation.Preview.PreviewSHA256,
		AuthorizationRef: journal.AdminCredentialRotation.Preview.AuthorizationRef,
		ReasonCode:       journal.ReasonCode,
	}
	expectedRequestSHA256, err := adminCredentialRotationRequestDigest(expectedRequest)
	if err != nil || expectedRequestSHA256 != journal.AdminCredentialRotation.RequestSHA256 {
		return adminCredentialRotationPlan{}, fmt.Errorf("admin credential rotation request identity differs from its journal")
	}
	if err := stageAdminCredentialRotationRequest(
		engine.Paths, journal.OperationID, expectedRequest, expectedRequestSHA256,
	); err != nil {
		return adminCredentialRotationPlan{}, fmt.Errorf("admin credential rotation request cannot be recovered")
	}
	request, err := readStagedAdminCredentialRotationRequest(
		engine.Paths, journal.OperationID, journal.AdminCredentialRotation.RequestSHA256,
	)
	if err != nil || request.PreviewSHA256 != journal.AdminCredentialRotation.Preview.PreviewSHA256 ||
		request.AuthorizationRef != journal.AdminCredentialRotation.Preview.AuthorizationRef ||
		request.ReasonCode != journal.ReasonCode {
		return adminCredentialRotationPlan{}, fmt.Errorf("admin credential rotation request differs from its journal")
	}
	state, current, err := engine.currentAdminCredentialRotationRelease()
	if err != nil || state.Current != *journal.Before || *journal.Target != *journal.Before ||
		cleanBaselineReleaseIdentity(state.Current) != journal.AdminCredentialRotation.Preview.Release {
		return adminCredentialRotationPlan{}, fmt.Errorf("admin credential rotation release identity changed")
	}
	predecessor, err := LoadDatabaseCredentialSet(engine.Paths, journal.DatabaseSetBefore)
	if err != nil {
		return adminCredentialRotationPlan{}, fmt.Errorf("predecessor admin credential set cannot be loaded")
	}
	predecessorSummary, err := InspectAdminCredentialSet(engine.Paths, journal.DatabaseSetBefore)
	if err != nil || !reflect.DeepEqual(predecessorSummary, journal.AdminCredentialRotation.Preview.ActiveCredential) {
		return adminCredentialRotationPlan{}, fmt.Errorf("predecessor admin credential set changed")
	}
	plan := adminCredentialRotationPlan{
		State: state, Current: current, Predecessor: predecessor,
		Preview: journal.AdminCredentialRotation.Preview,
	}
	if _, err := os.Lstat(journal.DatabaseSetAfter); err == nil {
		plan.Successor, err = LoadDatabaseCredentialSet(engine.Paths, journal.DatabaseSetAfter)
		if err != nil {
			return adminCredentialRotationPlan{}, fmt.Errorf("successor admin credential set cannot be loaded")
		}
		summary, summaryErr := InspectAdminCredentialSet(engine.Paths, journal.DatabaseSetAfter)
		if summaryErr != nil || (journal.AdminCredentialRotation.SuccessorCredential != nil &&
			!reflect.DeepEqual(summary, *journal.AdminCredentialRotation.SuccessorCredential)) {
			return adminCredentialRotationPlan{}, fmt.Errorf("successor admin credential set changed")
		}
	} else if !errors.Is(err, os.ErrNotExist) || journal.AdminCredentialRotation.VerifierChanged ||
		journal.Phase == "change_admin_scram_verifier" || containsString(journal.CompletedPhases, "change_admin_scram_verifier") {
		return adminCredentialRotationPlan{}, fmt.Errorf("successor admin credential set is missing after the verifier barrier")
	}
	active, err := readAbsoluteSymlink(engine.Paths.DatabaseCurrentLink)
	if err != nil || (active != journal.DatabaseSetBefore && active != journal.DatabaseSetAfter) {
		return adminCredentialRotationPlan{}, fmt.Errorf("active database credential set changed outside the operation")
	}
	usable := predecessor
	if plan.Successor.SetRef != "" {
		manager, err := engine.adminCredentialRotationDatabase()
		if err != nil {
			return adminCredentialRotationPlan{}, err
		}
		transition, err := manager.ProbeAdminCredentialTransition(ctx, predecessor, plan.Successor)
		if err != nil {
			return adminCredentialRotationPlan{}, err
		}
		if transition == AdminCredentialTransitionSuccessor {
			journal.AdminCredentialRotation.VerifierChanged = true
			journal.AllowedActions = []string{"recover", "resume"}
			usable = plan.Successor
			if err := engine.updateJournal(journal); err != nil {
				return adminCredentialRotationPlan{}, err
			}
		} else if journal.AdminCredentialRotation.VerifierChanged || active == journal.DatabaseSetAfter {
			return adminCredentialRotationPlan{}, fmt.Errorf("admin credential rotation barrier state regressed")
		}
	}
	fingerprint, err := engine.adminCredentialRotationFingerprint(ctx, current, usable)
	if err != nil || fingerprint != plan.Preview.State {
		return adminCredentialRotationPlan{}, fmt.Errorf("admin credential rotation stable state changed before resume")
	}
	workers, mainPID, err := engine.adminCredentialRotationRuntimeBoundary(ctx)
	if err != nil || mainPID != journal.ServicePIDBefore || !equalAdminRotationWorkers(workers, plan.Preview.Workers) {
		return adminCredentialRotationPlan{}, fmt.Errorf("service or worker boundary changed before resume")
	}
	return plan, nil
}

func (engine *Engine) abortAdminCredentialRotationOperation(
	ctx context.Context,
	journal *Journal,
) (OperationReceipt, error) {
	if journal.AdminCredentialRotation == nil || journal.Before == nil ||
		journal.AdminCredentialRotation.SchemaVersion != adminCredentialRotationJournalSchema {
		return OperationReceipt{}, fmt.Errorf("admin credential rotation journal is invalid")
	}
	plan, err := engine.resumeAdminCredentialRotationPlan(ctx, journal)
	if err != nil {
		return OperationReceipt{}, err
	}
	barrier, err := engine.adminCredentialRotationBarrierState(ctx, journal, plan)
	if err != nil {
		return OperationReceipt{}, err
	}
	if barrier {
		return OperationReceipt{}, fmt.Errorf("admin credential rotation crossed the verifier barrier and is resume-only")
	}
	active, err := readAbsoluteSymlink(engine.Paths.DatabaseCurrentLink)
	if err != nil || active != journal.DatabaseSetBefore {
		return OperationReceipt{}, fmt.Errorf("predecessor database credential set is not active before abort")
	}
	now := engine.now()
	journal.Status = "aborted"
	journal.Phase = "aborted"
	journal.ErrorCode = ""
	journal.AllowedActions = []string{}
	journal.CompletedAt = &now
	if err := engine.updateJournal(journal); err != nil {
		return OperationReceipt{}, err
	}
	receipt := OperationReceipt{
		SchemaVersion: ReceiptSchema, OperationID: journal.OperationID,
		Kind: adminCredentialRotationKind, Status: "aborted",
		Before: journal.Before, After: journal.Before,
		DatabaseSetRef: journal.DatabaseSetBefore, ReasonCode: journal.ReasonCode,
		Checks: []CheckResult{
			{Name: "verifier", Status: "pass", Detail: "predecessor_remains_authoritative"},
			{Name: "staged_successor", Status: "pass", Detail: "retained_sealed_inactive_no_file_deleted"},
			{Name: "services", Status: "pass", Detail: "zero_restart_zero_poll"},
		},
		CompletedAt: now,
	}
	if err := engine.writeReceipt(receipt); err != nil {
		return OperationReceipt{}, err
	}
	return receipt, nil
}

func (engine *Engine) adminCredentialRotationBarrierState(
	ctx context.Context,
	journal *Journal,
	plan adminCredentialRotationPlan,
) (bool, error) {
	if journal.AdminCredentialRotation == nil {
		return false, fmt.Errorf("admin credential rotation journal evidence is missing")
	}
	if plan.Successor.SetRef == "" {
		if journal.AdminCredentialRotation.VerifierChanged || journal.Phase == "change_admin_scram_verifier" ||
			containsString(journal.CompletedPhases, "change_admin_scram_verifier") {
			return false, fmt.Errorf("successor credential is missing at the verifier barrier")
		}
		return false, nil
	}
	if !journal.AdminCredentialRotation.VerifierChanged && journal.Phase != "change_admin_scram_verifier" &&
		!containsString(journal.CompletedPhases, "change_admin_scram_verifier") {
		return false, nil
	}
	manager, err := engine.adminCredentialRotationDatabase()
	if err != nil {
		return false, err
	}
	transition, err := manager.ProbeAdminCredentialTransition(ctx, plan.Predecessor, plan.Successor)
	if err != nil {
		return false, err
	}
	switch transition {
	case AdminCredentialTransitionSuccessor:
		journal.AdminCredentialRotation.VerifierChanged = true
		journal.AllowedActions = []string{"recover", "resume"}
		if err := engine.updateJournal(journal); err != nil {
			return false, err
		}
		return true, nil
	case AdminCredentialTransitionPredecessor:
		if journal.AdminCredentialRotation.VerifierChanged || containsString(journal.CompletedPhases, "change_admin_scram_verifier") {
			return false, fmt.Errorf("admin verifier barrier regressed")
		}
		return false, nil
	default:
		return false, fmt.Errorf("admin credential transition is ambiguous")
	}
}

func (engine *Engine) runAdminCredentialRotationPhase(
	journal *Journal,
	phase string,
	action func() error,
) error {
	completed := containsString(journal.CompletedPhases, phase)
	if !completed {
		if err := engine.beginAdminCredentialRotationPhase(journal, phase); err != nil {
			return err
		}
	}
	if err := action(); err != nil {
		return err
	}
	if completed {
		return nil
	}
	return engine.completePhase(journal, phase)
}

func (engine *Engine) beginAdminCredentialRotationPhase(journal *Journal, phase string) error {
	journal.Status = "active"
	journal.Phase = phase
	journal.ErrorCode = ""
	journal.AllowedActions = []string{"recover", "resume"}
	if journal.AdminCredentialRotation != nil && !journal.AdminCredentialRotation.VerifierChanged {
		journal.AllowedActions = append(journal.AllowedActions, "abort")
	}
	return engine.updateJournal(journal)
}

func (engine *Engine) readAdminCredentialRotationDurableReceipt(journal Journal) (OperationReceipt, error) {
	path := filepath.Join(engine.Paths.ReceiptsDir, journal.OperationID+".json")
	var receipt OperationReceipt
	if err := readJSONStrict(path, &receipt); err != nil {
		return OperationReceipt{}, err
	}
	if receipt.SchemaVersion != ReceiptSchema || receipt.OperationID != journal.OperationID ||
		receipt.Kind != adminCredentialRotationKind || receipt.Status != "completed" ||
		receipt.AdminCredentialRotation == nil || journal.AdminCredentialRotation == nil ||
		receipt.AdminCredentialRotation.SchemaVersion != AdminCredentialRotationReceiptSchema ||
		receipt.AdminCredentialRotation.PreviewSHA256 != journal.AdminCredentialRotation.Preview.PreviewSHA256 ||
		receipt.AdminCredentialRotation.RequestSHA256 != journal.AdminCredentialRotation.RequestSHA256 ||
		receipt.DatabaseSetRef != journal.DatabaseSetAfter || receipt.ReasonCode != journal.ReasonCode ||
		receipt.Before == nil || journal.Before == nil || *receipt.Before != *journal.Before ||
		receipt.After == nil || journal.Target == nil || *receipt.After != *journal.Target {
		return OperationReceipt{}, fmt.Errorf("admin credential rotation durable receipt does not match its journal")
	}
	return receipt, nil
}

func (engine *Engine) currentAdminCredentialRotationRelease() (InstallState, verifiedTarget, error) {
	state, err := engine.readInstallState()
	if err != nil || state.Current.LegacyReleaseID != "" {
		return InstallState{}, verifiedTarget{}, fmt.Errorf("admin credential rotation requires a managed SemVer release")
	}
	current, err := engine.verifyStateV2(state.Current)
	if err != nil {
		return InstallState{}, verifiedTarget{}, err
	}
	if err := releasePayloadAndRuntimePreflight(current, engine.Paths); err != nil {
		return InstallState{}, verifiedTarget{}, err
	}
	if state.DatabaseSchemaCurrent != state.Current.DatabaseSchema ||
		(!engine.Paths.IsTestRoot && state.Current.DatabaseSchema != buildinfo.DatabaseSchemaMax) {
		return InstallState{}, verifiedTarget{}, fmt.Errorf("admin credential rotation requires exact schema %s", buildinfo.DatabaseSchemaMax)
	}
	if !engine.Paths.IsTestRoot {
		runtime := buildinfo.Current()
		if runtime.ProductVersion != state.Current.ProductVersion || runtime.Commit != state.Current.Commit ||
			runtime.ManualVersion != state.Current.ManualVersion || runtime.ConfigSchemaVersion != state.Current.ConfigSchema {
			return InstallState{}, verifiedTarget{}, fmt.Errorf("admin credential rotation controller differs from the active release")
		}
	}
	return state, current, nil
}

func (engine *Engine) adminCredentialRotationFingerprint(
	ctx context.Context,
	current verifiedTarget,
	credentials DatabaseCredentials,
) (AdminCredentialRotationStateFingerprint, error) {
	facts, err := engine.Database.Inspect(ctx, credentials)
	if err != nil || facts.PostgresMajor != 16 || facts.PublicTableCount != len(cleanBaselineTables) ||
		validateLedgerPrefix(facts.MigrationLedger, current.Bundle.Manifest.MigrationChecksums, false) != nil {
		return AdminCredentialRotationStateFingerprint{}, fmt.Errorf("admin credential rotation database identity is invalid")
	}
	if err := engine.Database.VerifyRoleBoundaries(ctx, credentials); err != nil {
		return AdminCredentialRotationStateFingerprint{}, err
	}
	if engine.CleanBaseline == nil {
		return AdminCredentialRotationStateFingerprint{}, fmt.Errorf("clean-baseline database manager is unavailable")
	}
	preview, err := engine.CleanBaseline.PreviewCleanBaseline(ctx, credentials)
	if err != nil || len(preview.Tables) != len(cleanBaselineTables) ||
		preview.SchemaContractVersion != CleanBaselineSchemaContractVersion ||
		preview.SchemaContractSHA256 != expectedCleanBaselineSchemaContract.SemanticSHA256 ||
		preview.MigrationSetSHA256 != expectedCleanBaselineSchemaContract.MigrationSetSHA256 {
		return AdminCredentialRotationStateFingerprint{}, fmt.Errorf("admin credential rotation schema or product state cannot be authorized")
	}
	for index, table := range preview.Tables {
		if index >= len(cleanBaselineTables) || table.Table != cleanBaselineTables[index] ||
			!sha256ReferencePattern.MatchString(table.SemanticSHA256) {
			return AdminCredentialRotationStateFingerprint{}, fmt.Errorf("admin credential rotation table fingerprint is invalid")
		}
	}
	accountFileSHA256, _, err := fileSHA256(engine.Paths.AccessAccountsFile, access.MaxAccountFileBytes)
	if err != nil {
		return AdminCredentialRotationStateFingerprint{}, fmt.Errorf("access account file fingerprint failed")
	}
	demoIdentity, err := adminRotationDemoOperatorIdentity(engine.Paths.AccessAccountsFile)
	if err != nil {
		return AdminCredentialRotationStateFingerprint{}, err
	}
	configurationSHA256, err := adminRotationConfigurationDigest(engine.Paths)
	if err != nil {
		return AdminCredentialRotationStateFingerprint{}, err
	}
	connectorMetadataSHA256, err := adminRotationConnectorMetadataDigest(engine.Paths)
	if err != nil {
		return AdminCredentialRotationStateFingerprint{}, err
	}
	migrationSHA256, err := hashAdminRotationJSON(facts.MigrationLedger)
	if err != nil {
		return AdminCredentialRotationStateFingerprint{}, err
	}
	productSHA256, err := hashAdminRotationJSON(preview.Tables)
	if err != nil {
		return AdminCredentialRotationStateFingerprint{}, err
	}
	connectorTables := make([]CleanBaselineTableState, 0)
	checkpointTables := make([]CleanBaselineTableState, 0)
	for _, table := range preview.Tables {
		switch table.Table {
		case "adapter_import_states", "adapter_manifests", "source_systems", "import_provenance",
			"import_provenance_events", "evidence_events", "capture_gaps", "connector_import_operation_bindings",
			"connector_operational_receipts", "ingestion_quarantine", "connector_checkpoint_epoch_operations",
			"connector_checkpoint_epoch_history":
			connectorTables = append(connectorTables, table)
		}
		switch table.Table {
		case "adapter_import_states", "connector_checkpoint_epoch_operations", "connector_checkpoint_epoch_history":
			checkpointTables = append(checkpointTables, table)
		}
	}
	connectorSHA256, err := hashAdminRotationJSON(connectorTables)
	if err != nil {
		return AdminCredentialRotationStateFingerprint{}, err
	}
	checkpointSHA256, err := hashAdminRotationJSON(struct {
		Tables []CleanBaselineTableState    `json:"tables"`
		Floor  CleanBaselineCheckpointFloor `json:"floor"`
	}{checkpointTables, preview.CheckpointFloor})
	if err != nil {
		return AdminCredentialRotationStateFingerprint{}, err
	}
	catalogSHA256, err := hashAdminRotationJSON(preview.Catalog)
	if err != nil {
		return AdminCredentialRotationStateFingerprint{}, err
	}
	return AdminCredentialRotationStateFingerprint{
		PostgresMajor: facts.PostgresMajor, DatabaseSHA256: hashCleanBaselineText(facts.DatabaseName),
		PublicTableCount:      facts.PublicTableCount,
		SchemaContractVersion: preview.SchemaContractVersion,
		SchemaContractSHA256:  preview.SchemaContractSHA256,
		SchemaLayoutSHA256:    preview.SchemaLayoutSHA256,
		MigrationLedgerSHA256: migrationSHA256, ProductStateSHA256: productSHA256,
		ConnectorStateSHA256: connectorSHA256, CheckpointStateSHA256: checkpointSHA256,
		ReleaseCatalogSHA256: catalogSHA256,
		AccountFileSHA256:    accountFileSHA256, DemoOperatorIdentitySHA256: demoIdentity,
		ConfigurationSHA256:         configurationSHA256,
		ConnectorFileMetadataSHA256: connectorMetadataSHA256,
	}, nil
}

func (engine *Engine) adminCredentialRotationRuntimeBoundary(
	ctx context.Context,
) ([]CleanBaselineWorkerPreview, string, error) {
	feed, err := engine.requireCheckpointWorkerDormant(ctx, checkpointepoch.CompletedExecutionWorkerUnit)
	if err != nil {
		return nil, "", err
	}
	liteLLM, err := engine.requireCheckpointWorkerDormant(ctx, checkpointepoch.LiteLLMWorkerUnit)
	if err != nil {
		return nil, "", err
	}
	active, err := engine.systemdUnitProperty(ctx, "xinsight.service", "ActiveState")
	if err != nil || active != "active" {
		return nil, "", fmt.Errorf("admin credential rotation requires the main service to remain active")
	}
	mainPID, err := engine.systemdUnitProperty(ctx, "xinsight.service", "MainPID")
	pid, parseErr := strconv.ParseInt(mainPID, 10, 64)
	if err != nil || parseErr != nil || pid <= 0 {
		return nil, "", fmt.Errorf("admin credential rotation main service process identity is invalid")
	}
	return []CleanBaselineWorkerPreview{
		{Unit: feed.Unit, Present: feed.Present, Active: feed.Active, Enabled: feed.Enabled},
		{Unit: liteLLM.Unit, Present: liteLLM.Present, Active: liteLLM.Active, Enabled: liteLLM.Enabled},
	}, mainPID, nil
}

func adminRotationDemoOperatorIdentity(path string) (string, error) {
	config, err := access.ReadAndValidateAccountFile(path)
	if err != nil {
		return "", fmt.Errorf("demo.operator account configuration is invalid")
	}
	for _, account := range config.Accounts {
		if account.Username != "demo.operator" {
			continue
		}
		if account.Disabled || account.Role != access.RoleOperator {
			return "", fmt.Errorf("demo.operator account identity is invalid")
		}
		return hashAdminRotationJSON(struct {
			SubjectID string      `json:"subject_id"`
			Username  string      `json:"username"`
			Role      access.Role `json:"role"`
			Disabled  bool        `json:"disabled"`
		}{account.SubjectID, account.Username, account.Role, account.Disabled})
	}
	return "", fmt.Errorf("enabled demo.operator account is required")
}

type adminRotationFileDigestEntry struct {
	Name          string `json:"name"`
	Present       bool   `json:"present"`
	Mode          uint32 `json:"mode,omitempty"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
	ContentSHA256 string `json:"content_sha256,omitempty"`
	UID           uint32 `json:"uid,omitempty"`
	GID           uint32 `json:"gid,omitempty"`
	Device        uint64 `json:"device,omitempty"`
	Inode         uint64 `json:"inode,omitempty"`
	ModifiedNanos int64  `json:"modified_nanos,omitempty"`
}

func adminRotationConfigurationDigest(paths Paths) (string, error) {
	return digestAdminRotationFiles([]struct {
		name    string
		path    string
		content bool
		require bool
	}{
		{"managed_environment", paths.EnvironmentFile, true, true},
		{"access_accounts", paths.AccessAccountsFile, true, true},
		{"release_trust", paths.TrustStore, true, true},
		{"postgres_environment", paths.PostgresEnvironment, false, true},
		{"postgres_compose", paths.PostgresCompose, true, true},
		{"main_unit", paths.ServiceUnit, true, true},
		{"feed_unit", paths.FeedServiceUnit, true, true},
		{"litellm_unit", paths.LiteLLMServiceUnit, true, true},
		{"managed_dropin", paths.ManagedServiceDropIn, true, true},
	})
}

func adminRotationConnectorMetadataDigest(paths Paths) (string, error) {
	return digestAdminRotationFiles([]struct {
		name    string
		path    string
		content bool
		require bool
	}{
		{"unirouter_active", paths.FeedCredentialFile, false, false},
		{"unirouter_staged", paths.FeedCredentialNext, false, false},
		{"unirouter_operation", paths.FeedCredentialState, false, false},
		{"litellm_credential", paths.LiteLLMCredential, false, false},
		{"litellm_environment", paths.LiteLLMEnvironment, false, false},
	})
}

func digestAdminRotationFiles(files []struct {
	name    string
	path    string
	content bool
	require bool
}) (string, error) {
	entries := make([]adminRotationFileDigestEntry, 0, len(files))
	for _, file := range files {
		info, err := os.Lstat(file.path)
		if errors.Is(err, os.ErrNotExist) && !file.require {
			entries = append(entries, adminRotationFileDigestEntry{Name: file.name})
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("admin credential rotation file boundary is invalid: %s", file.name)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || uint64(stat.Nlink) != 1 {
			return "", fmt.Errorf("admin credential rotation file metadata is unavailable: %s", file.name)
		}
		entry := adminRotationFileDigestEntry{
			Name: file.name, Present: true, Mode: uint32(info.Mode().Perm()), SizeBytes: info.Size(),
			UID: stat.Uid, GID: stat.Gid, Device: uint64(stat.Dev), Inode: uint64(stat.Ino),
			ModifiedNanos: info.ModTime().UnixNano(),
		}
		if file.content {
			digest, _, err := fileSHA256(file.path, maxControlFileBytes)
			if err != nil {
				return "", fmt.Errorf("admin credential rotation file digest failed: %s", file.name)
			}
			entry.ContentSHA256 = digest
		}
		entries = append(entries, entry)
	}
	return hashAdminRotationJSON(entries)
}

func adminCredentialRotationProtocol() (AdminCredentialRotationProtocolIdentity, error) {
	pin, err := foigentcontract.EmbeddedProtocolPin()
	if err != nil {
		return AdminCredentialRotationProtocolIdentity{}, err
	}
	return AdminCredentialRotationProtocolIdentity{
		Version: pin.ProtocolVersion, Tag: pin.Authority.Tag, ReleaseDigest: pin.ReleaseDigest,
	}, nil
}

func verifyAdminCredentialInheritedClosure(
	predecessor AdminCredentialSetSummary,
	successor AdminCredentialSetSummary,
) error {
	if len(predecessor.Members) != 4 || len(successor.Members) != 4 ||
		predecessor.AdminIdentitySHA256 != successor.AdminIdentitySHA256 ||
		predecessor.CanonicalSHA256 == successor.CanonicalSHA256 {
		return fmt.Errorf("admin credential set transition closure is invalid")
	}
	for index := 0; index < 3; index++ {
		if predecessor.Members[index] != successor.Members[index] || !predecessor.Members[index].Inherited {
			return fmt.Errorf("admin credential rotation changed an inherited role member")
		}
	}
	if predecessor.Members[3].RoleAlias != adminDatabaseRole ||
		successor.Members[3].RoleAlias != adminDatabaseRole ||
		predecessor.Members[3].IdentitySHA256 != successor.Members[3].IdentitySHA256 ||
		predecessor.Members[3].ContentSHA256 == successor.Members[3].ContentSHA256 {
		return fmt.Errorf("admin credential rotation successor member is invalid")
	}
	return nil
}

func equalAdminRotationWorkers(left, right []CleanBaselineWorkerPreview) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func adminCredentialRotationPreviewDigest(report AdminCredentialRotationPreviewReport) (string, error) {
	report.PreviewSHA256 = ""
	report.GeneratedAt = time.Time{}
	return hashAdminRotationJSON(report)
}

func adminCredentialRotationRequestDigest(request AdminCredentialRotationRequest) (string, error) {
	payload, err := marshalJSON(request)
	if err != nil {
		return "", err
	}
	return hashCleanBaselineBytes(payload), nil
}

func hashAdminRotationJSON(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func stageAdminCredentialRotationRequest(
	paths Paths,
	operationID string,
	request AdminCredentialRotationRequest,
	expectedSHA256 string,
) error {
	path := adminCredentialRotationRequestPath(paths, operationID)
	if exists, err := fileExists(path); err != nil {
		return err
	} else if !exists {
		if err := writeJSONAtomic(path, request, 0o600); err != nil {
			return err
		}
	}
	digest, _, err := fileSHA256(path, adminCredentialRotationRequestMaxBytes)
	if err != nil || digest != expectedSHA256 {
		return fmt.Errorf("staged admin credential rotation request digest differs")
	}
	return nil
}

func readStagedAdminCredentialRotationRequest(
	paths Paths,
	operationID string,
	expectedSHA256 string,
) (AdminCredentialRotationRequest, error) {
	path := adminCredentialRotationRequestPath(paths, operationID)
	digest, _, err := fileSHA256(path, adminCredentialRotationRequestMaxBytes)
	if err != nil || digest != expectedSHA256 {
		return AdminCredentialRotationRequest{}, fmt.Errorf("staged admin credential rotation request digest differs")
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return AdminCredentialRotationRequest{}, fmt.Errorf("read staged admin credential rotation request")
	}
	return DecodeAdminCredentialRotationRequest(payload)
}

func adminCredentialRotationRequestPath(paths Paths, operationID string) string {
	return filepath.Join(paths.OperationsDir, operationID+"-admin-credential-rotation-request.json")
}

func (engine *Engine) adminCredentialRotationDatabase() (AdminCredentialRotationDatabase, error) {
	if engine.AdminCredentialRotation == nil {
		return nil, fmt.Errorf("admin credential rotation database manager is unavailable")
	}
	return engine.AdminCredentialRotation, nil
}

func (engine *Engine) adminCredentialRotationRandomReader() io.Reader {
	if engine.Random != nil {
		return engine.Random
	}
	return rand.Reader
}

func maybeFailAdminCredentialRotation(paths Paths, point string) error {
	if paths.IsTestRoot && os.Getenv("XINSIGHTCTL_FAIL_ADMIN_CREDENTIAL_ROTATION_AT") == point {
		return fmt.Errorf("injected admin credential rotation process death at %s", point)
	}
	return nil
}

func requireAdminCredentialRotationRoot(paths Paths) error {
	if paths.IsTestRoot {
		return nil
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("admin credential rotation commands require root")
	}
	return nil
}
