package hostctl

import (
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
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
)

const (
	FeedCredentialRegistrationSchema    = "unirouter.xinsight.feed-credential-registration.v1"
	FeedCredentialTokenFormat           = "ur-xf1-hex256.v1"
	FeedCredentialVerifierScheme        = "receiver_sha256_v1"
	FeedCredentialVerifierDomain        = "unirouter.xinsight.feed-credential.v1"
	FeedCredentialStateSchema           = "xinsight.feed-credential-operation-state.v1"
	FeedCredentialStatusSchema          = "xinsight.feed-credential-status.v1"
	FeedCredentialPromotionSchema       = "xinsight.feed-credential-dormant-promotion.v1"
	FeedPausedAuthReadbackSchema        = "xinsight.feed-paused-auth-readback.v1"
	FeedDormantCredentialReadbackSchema = "xinsight.feed-dormant-credential-readback.v1"
	FeedWorkerActivationSchema          = "xinsight.feed-worker-activation.v1"

	feedCredentialTokenPrefix = "ur-xf1-"
	feedCredentialTokenBytes  = 32
	feedCredentialTokenLength = len(feedCredentialTokenPrefix) + feedCredentialTokenBytes*2

	feedCredentialPendingPreparing        = "preparing"
	feedCredentialPendingPrepared         = "prepared"
	feedCredentialPendingActivating       = "activating"
	feedCredentialPendingPromotingDormant = "promoting_dormant"
	feedCredentialPendingDormant          = "dormant"
	feedCredentialPendingWorkerStarting   = "worker_starting"
)

var (
	feedCredentialTokenPattern     = regexp.MustCompile(`^ur-xf1-[0-9a-f]{64}$`)
	feedCredentialPrefixPattern    = regexp.MustCompile(`^ur-xf1-[0-9a-f]{13}$`)
	feedCredentialVerifierPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	feedCredentialOperationPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	errFeedCredentialAuthPreflight = errors.New("staged feed credential did not authenticate against the paused source")
)

// FeedCredentialRegistration is the exact Frozen Registration Bundle V1.
// Do not add fields: UniRouter rejects unknown registration keys.
type FeedCredentialRegistration struct {
	SchemaVersion  string `json:"schema_version"`
	TokenFormat    string `json:"token_format"`
	VerifierScheme string `json:"verifier_scheme"`
	KeyPrefix      string `json:"key_prefix"`
	VerifierSHA256 string `json:"verifier_sha256"`
	OperationID    string `json:"operation_id"`
}

type feedCredentialPending struct {
	Status                         string                     `json:"status"`
	Registration                   FeedCredentialRegistration `json:"registration"`
	ManagedConfigSHA256            string                     `json:"managed_config_sha256,omitempty"`
	NetworkBindingSHA256           string                     `json:"network_binding_sha256,omitempty"`
	PausedAuthReceiptSHA256        string                     `json:"paused_auth_receipt_sha256,omitempty"`
	DormantCredentialReceiptSHA256 string                     `json:"dormant_credential_receipt_sha256,omitempty"`
	WorkerActivationStarted        bool                       `json:"worker_activation_started,omitempty"`
	StartedAt                      time.Time                  `json:"started_at"`
	PreparedAt                     *time.Time                 `json:"prepared_at,omitempty"`
	UpdatedAt                      time.Time                  `json:"updated_at"`
}

type feedCredentialTerminal struct {
	Status                         string                     `json:"status"`
	Registration                   FeedCredentialRegistration `json:"registration"`
	ManagedConfigSHA256            string                     `json:"managed_config_sha256,omitempty"`
	NetworkBindingSHA256           string                     `json:"network_binding_sha256,omitempty"`
	PausedAuthReceiptSHA256        string                     `json:"paused_auth_receipt_sha256,omitempty"`
	DormantCredentialReceiptSHA256 string                     `json:"dormant_credential_receipt_sha256,omitempty"`
	CompletedAt                    time.Time                  `json:"completed_at"`
}

type feedCredentialOperationState struct {
	SchemaVersion string                      `json:"schema_version"`
	Active        *FeedCredentialRegistration `json:"active,omitempty"`
	Pending       *feedCredentialPending      `json:"pending,omitempty"`
	LastOperation *feedCredentialTerminal     `json:"last_operation,omitempty"`
	UpdatedAt     time.Time                   `json:"updated_at"`
}

type FeedCredentialFileMetadata struct {
	SizeBytes int64  `json:"size_bytes"`
	Mode      string `json:"mode"`
	OwnerUID  uint32 `json:"owner_uid"`
	OwnerGID  uint32 `json:"owner_gid"`
	LinkCount uint64 `json:"link_count"`
}

type FeedCredentialFileStatus struct {
	Present        bool                        `json:"present"`
	Metadata       *FeedCredentialFileMetadata `json:"metadata,omitempty"`
	BindingMatches bool                        `json:"binding_matches"`
}

type FeedCredentialTerminalStatus struct {
	Status                         string                     `json:"status"`
	Registration                   FeedCredentialRegistration `json:"registration"`
	ManagedConfigSHA256            string                     `json:"managed_config_sha256,omitempty"`
	NetworkBindingSHA256           string                     `json:"network_binding_sha256,omitempty"`
	PausedAuthReceiptSHA256        string                     `json:"paused_auth_receipt_sha256,omitempty"`
	DormantCredentialReceiptSHA256 string                     `json:"dormant_credential_receipt_sha256,omitempty"`
	CompletedAt                    time.Time                  `json:"completed_at"`
}

type FeedCredentialPromotionReport struct {
	SchemaVersion                  string                     `json:"schema_version"`
	Registration                   FeedCredentialRegistration `json:"registration"`
	ManagedConfigSHA256            string                     `json:"managed_config_sha256"`
	NetworkBindingSHA256           string                     `json:"network_binding_sha256"`
	PausedAuthReceiptSHA256        string                     `json:"paused_auth_receipt_sha256"`
	DormantCredentialReceiptSHA256 string                     `json:"dormant_credential_receipt_sha256"`
	Credential                     FeedCredentialStatusReport `json:"credential"`
	Replayed                       bool                       `json:"replayed"`
}

type FeedWorkerActivationReport struct {
	SchemaVersion                  string                     `json:"schema_version"`
	Registration                   FeedCredentialRegistration `json:"registration"`
	ManagedConfigSHA256            string                     `json:"managed_config_sha256"`
	NetworkBindingSHA256           string                     `json:"network_binding_sha256"`
	PausedAuthReceiptSHA256        string                     `json:"paused_auth_receipt_sha256"`
	DormantCredentialReceiptSHA256 string                     `json:"dormant_credential_receipt_sha256"`
	Present                        bool                       `json:"present"`
	Active                         bool                       `json:"active"`
	Enabled                        bool                       `json:"enabled"`
	Replayed                       bool                       `json:"replayed"`
}

type feedPausedAuthReadback struct {
	SchemaVersion        string                     `json:"schema_version"`
	Registration         FeedCredentialRegistration `json:"registration"`
	ManagedConfigSHA256  string                     `json:"managed_config_sha256"`
	NetworkBindingSHA256 string                     `json:"network_binding_sha256"`
	Status               string                     `json:"status"`
}

type feedDormantCredentialReadback struct {
	SchemaVersion           string                     `json:"schema_version"`
	Registration            FeedCredentialRegistration `json:"registration"`
	ManagedConfigSHA256     string                     `json:"managed_config_sha256"`
	NetworkBindingSHA256    string                     `json:"network_binding_sha256"`
	PausedAuthReceiptSHA256 string                     `json:"paused_auth_receipt_sha256"`
	CredentialBinding       string                     `json:"credential_binding"`
	Status                  string                     `json:"status"`
}

type FeedCredentialStatusReport struct {
	SchemaVersion       string                        `json:"schema_version"`
	Status              string                        `json:"status"`
	ActiveRegistration  *FeedCredentialRegistration   `json:"active_registration,omitempty"`
	PendingRegistration *FeedCredentialRegistration   `json:"pending_registration,omitempty"`
	PendingPhase        string                        `json:"pending_phase,omitempty"`
	ActiveFile          FeedCredentialFileStatus      `json:"active_file"`
	StagedFile          FeedCredentialFileStatus      `json:"staged_file"`
	LastOperation       *FeedCredentialTerminalStatus `json:"last_operation,omitempty"`
	RestartRequired     bool                          `json:"restart_required"`
	RequiredAction      string                        `json:"required_action,omitempty"`
	GeneratedAt         time.Time                     `json:"generated_at"`
}

type inspectedFeedCredential struct {
	token    string
	metadata FeedCredentialFileMetadata
}

// PrepareFeedCredential creates or resumes exactly one receiver-generated
// credential operation. The returned value is the strict public registration
// bundle; the raw token remains only in the root-owned staged file.
func (engine *Engine) PrepareFeedCredential(
	ctx context.Context,
	expectedActiveOperationID string,
	expectedActiveVerifierSHA256 string,
) (FeedCredentialRegistration, error) {
	_ = ctx
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return FeedCredentialRegistration{}, err
	}
	if err := validateOptionalFeedCredentialBinding(expectedActiveOperationID, expectedActiveVerifierSHA256); err != nil {
		return FeedCredentialRegistration{}, err
	}

	var registration FeedCredentialRegistration
	err := engine.withOperationLock(func() error {
		if err := engine.ensureFeedCredentialDirectories(); err != nil {
			return err
		}
		state, exists, err := engine.readFeedCredentialOperationStateOptional()
		if err != nil {
			return err
		}
		if !exists {
			state = feedCredentialOperationState{
				SchemaVersion: FeedCredentialStateSchema,
				UpdatedAt:     engine.now(),
			}
		}
		if err := engine.validateExpectedActiveBinding(
			state,
			expectedActiveOperationID,
			expectedActiveVerifierSHA256,
		); err != nil {
			return err
		}

		if state.Pending != nil {
			switch state.Pending.Status {
			case feedCredentialPendingPreparing:
				staged, present, complete, inspectErr := engine.inspectFeedCredentialForRecoveryOptional(
					engine.Paths.FeedCredentialNext,
				)
				if inspectErr != nil {
					return inspectErr
				}
				if !present {
					return fmt.Errorf("feed credential prepare is incomplete; abort the exact pending operation before retry")
				}
				if !complete {
					return fmt.Errorf("feed credential staged write is incomplete; abort the exact pending operation before retry")
				}
				if err := credentialMatchesRegistration(staged, state.Pending.Registration); err != nil {
					return err
				}
				now := engine.now()
				state.Pending.Status = feedCredentialPendingPrepared
				state.Pending.PreparedAt = &now
				state.Pending.UpdatedAt = now
				state.UpdatedAt = now
				if err := engine.writeFeedCredentialOperationState(state, true); err != nil {
					return err
				}
				registration = state.Pending.Registration
				return nil
			case feedCredentialPendingPrepared:
				staged, present, inspectErr := engine.inspectFeedCredentialOptional(engine.Paths.FeedCredentialNext)
				if inspectErr != nil {
					return inspectErr
				}
				if !present {
					return fmt.Errorf("prepared feed credential staged file is missing")
				}
				if err := credentialMatchesRegistration(staged, state.Pending.Registration); err != nil {
					return err
				}
				registration = state.Pending.Registration
				return nil
			case feedCredentialPendingActivating:
				return fmt.Errorf("feed credential activation is incomplete; resume activate with the exact pending binding")
			default:
				return fmt.Errorf("feed credential pending state is invalid")
			}
		}

		if _, present, inspectErr := engine.inspectFeedCredentialOptional(engine.Paths.FeedCredentialNext); inspectErr != nil {
			return inspectErr
		} else if present {
			return fmt.Errorf("unmanaged staged feed credential exists")
		}

		token, newRegistration, err := engine.generateFeedCredentialRegistration()
		if err != nil {
			return err
		}
		now := engine.now()
		state.Pending = &feedCredentialPending{
			Status:       feedCredentialPendingPreparing,
			Registration: newRegistration,
			StartedAt:    now,
			UpdatedAt:    now,
		}
		state.UpdatedAt = now
		if err := engine.writeFeedCredentialOperationState(state, exists); err != nil {
			return err
		}
		if err := engine.runFeedCredentialPhaseHook("state_preparing"); err != nil {
			return fmt.Errorf("feed credential prepare interrupted after durable operation state: %w", err)
		}
		if _, err := engine.writeFeedCredentialExclusive(engine.Paths.FeedCredentialNext, token); err != nil {
			return fmt.Errorf("create staged feed credential: %w", err)
		}
		if err := engine.runFeedCredentialPhaseHook("staged_secret_written"); err != nil {
			return fmt.Errorf("feed credential prepare interrupted after staged secret write: %w", err)
		}
		preparedAt := engine.now()
		state.Pending.Status = feedCredentialPendingPrepared
		state.Pending.PreparedAt = &preparedAt
		state.Pending.UpdatedAt = preparedAt
		state.UpdatedAt = preparedAt
		if err := engine.writeFeedCredentialOperationState(state, true); err != nil {
			return err
		}
		registration = newRegistration
		return nil
	})
	return registration, err
}

func (engine *Engine) FeedCredentialStatus(ctx context.Context) (FeedCredentialStatusReport, error) {
	_ = ctx
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return FeedCredentialStatusReport{}, err
	}
	var report FeedCredentialStatusReport
	err := engine.withOperationLock(func() error {
		var err error
		report, err = engine.feedCredentialStatusLocked()
		return err
	})
	return report, err
}

// PromoteFeedCredentialDormant verifies the staged receiver credential against
// the paused Source endpoint and atomically installs it without starting or
// enabling the feed worker. The exact config and receipt digests are recovered
// from root-owned readback; no raw credential is returned.
func (engine *Engine) PromoteFeedCredentialDormant(
	ctx context.Context,
	operationID string,
	verifierSHA256 string,
) (FeedCredentialPromotionReport, error) {
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return FeedCredentialPromotionReport{}, err
	}
	if err := validateRequiredFeedCredentialBinding(operationID, verifierSHA256); err != nil {
		return FeedCredentialPromotionReport{}, err
	}

	var report FeedCredentialPromotionReport
	err := engine.withOperationLock(func() error {
		if err := engine.ensureFeedCredentialDirectories(); err != nil {
			return err
		}
		config, present, err := engine.readManagedFeedConfigOptional(engine.Paths.FeedEnvironmentFile)
		if err != nil {
			return err
		}
		if !present {
			return fmt.Errorf("active managed feed configuration is missing")
		}
		if _, staged, err := engine.readManagedFeedConfigOptional(engine.managedFeedConfigStagedPath()); err != nil {
			return err
		} else if staged {
			return fmt.Errorf("managed feed configuration promotion is incomplete")
		}

		state, exists, err := engine.readFeedCredentialOperationStateOptional()
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("no pending feed credential operation exists")
		}
		if state.Pending == nil {
			if state.Active == nil || state.LastOperation == nil || state.LastOperation.Status != "activated" ||
				requireRegistrationBinding(*state.Active, operationID, verifierSHA256) != nil ||
				state.LastOperation.Registration != *state.Active ||
				state.LastOperation.ManagedConfigSHA256 != config.configSHA256 ||
				state.LastOperation.NetworkBindingSHA256 != config.networkBinding.BindingSHA256 ||
				!hostctlSHA256Pattern.MatchString(state.LastOperation.PausedAuthReceiptSHA256) ||
				!hostctlSHA256Pattern.MatchString(state.LastOperation.DormantCredentialReceiptSHA256) {
				return fmt.Errorf("no matching dormant feed credential operation exists")
			}
			active, activePresent, err := engine.inspectFeedCredentialOptional(engine.Paths.FeedCredentialFile)
			if err != nil {
				return err
			}
			if !activePresent || credentialMatchesRegistration(active, *state.Active) != nil {
				return fmt.Errorf("completed dormant feed credential readback mismatch")
			}
			credential, err := engine.feedCredentialStatusLocked()
			if err != nil {
				return err
			}
			report = FeedCredentialPromotionReport{
				SchemaVersion:                  FeedCredentialPromotionSchema,
				Registration:                   *state.Active,
				ManagedConfigSHA256:            state.LastOperation.ManagedConfigSHA256,
				NetworkBindingSHA256:           state.LastOperation.NetworkBindingSHA256,
				PausedAuthReceiptSHA256:        state.LastOperation.PausedAuthReceiptSHA256,
				DormantCredentialReceiptSHA256: state.LastOperation.DormantCredentialReceiptSHA256,
				Credential:                     credential,
				Replayed:                       true,
			}
			return nil
		}
		if err := requireRegistrationBinding(state.Pending.Registration, operationID, verifierSHA256); err != nil {
			return err
		}
		if state.Pending.Status != feedCredentialPendingPrepared &&
			state.Pending.Status != feedCredentialPendingPromotingDormant &&
			state.Pending.Status != feedCredentialPendingDormant {
			return fmt.Errorf("feed credential is not prepared for dormant promotion")
		}

		pausedSHA256, dormantSHA256, err := feedCredentialDormantReceiptDigests(
			state.Pending.Registration,
			config.configSHA256,
			config.networkBinding.BindingSHA256,
		)
		if err != nil {
			return err
		}
		if state.Pending.Status != feedCredentialPendingPrepared &&
			(state.Pending.ManagedConfigSHA256 != config.configSHA256 ||
				state.Pending.NetworkBindingSHA256 != config.networkBinding.BindingSHA256 ||
				state.Pending.PausedAuthReceiptSHA256 != pausedSHA256) {
			return fmt.Errorf("dormant feed credential authority changed")
		}
		if state.Pending.Status == feedCredentialPendingDormant &&
			state.Pending.DormantCredentialReceiptSHA256 != dormantSHA256 {
			return fmt.Errorf("dormant feed credential receipt changed")
		}

		active, activePresent, err := engine.inspectFeedCredentialOptional(engine.Paths.FeedCredentialFile)
		if err != nil {
			return err
		}
		staged, stagedPresent, err := engine.inspectFeedCredentialOptional(engine.Paths.FeedCredentialNext)
		if err != nil {
			return err
		}
		promotionBarrierCrossed := activePresent &&
			credentialMatchesRegistration(active, state.Pending.Registration) == nil
		if !promotionBarrierCrossed {
			if err := validateCredentialAgainstOptionalRegistration(active, activePresent, state.Active); err != nil {
				return err
			}
			if !stagedPresent {
				return fmt.Errorf("prepared feed credential staged file is missing")
			}
			if err := credentialMatchesRegistration(staged, state.Pending.Registration); err != nil {
				return err
			}
		} else if stagedPresent {
			return fmt.Errorf("dormant feed credential has conflicting active and staged files")
		}

		if state.Pending.Status == feedCredentialPendingPrepared {
			if err := engine.verifyStagedFeedCredentialAuthentication(ctx, staged.token); err != nil {
				return err
			}
			now := engine.now()
			state.Pending.Status = feedCredentialPendingPromotingDormant
			state.Pending.ManagedConfigSHA256 = config.configSHA256
			state.Pending.NetworkBindingSHA256 = config.networkBinding.BindingSHA256
			state.Pending.PausedAuthReceiptSHA256 = pausedSHA256
			state.Pending.UpdatedAt = now
			state.UpdatedAt = now
			if err := engine.writeFeedCredentialOperationState(state, true); err != nil {
				return err
			}
			if err := engine.runFeedCredentialPhaseHook("dormant_state_promoting"); err != nil {
				return fmt.Errorf("dormant credential promotion interrupted before atomic replace: %w", err)
			}
		}

		if !promotionBarrierCrossed {
			if err := os.Rename(engine.Paths.FeedCredentialNext, engine.Paths.FeedCredentialFile); err != nil {
				return fmt.Errorf("atomically promote dormant feed credential: %w", err)
			}
			if err := syncDirectory(filepath.Dir(engine.Paths.FeedCredentialFile)); err != nil {
				return fmt.Errorf("sync dormant feed credential directory: %w", err)
			}
			active, activePresent, err = engine.inspectFeedCredentialOptional(engine.Paths.FeedCredentialFile)
			if err != nil {
				return err
			}
			if !activePresent || credentialMatchesRegistration(active, state.Pending.Registration) != nil {
				return fmt.Errorf("dormant feed credential readback mismatch")
			}
		}
		if err := engine.runFeedCredentialPhaseHook("dormant_credential_replaced"); err != nil {
			return fmt.Errorf("dormant credential promotion interrupted after atomic replace: %w", err)
		}

		replayed := state.Pending.Status == feedCredentialPendingDormant
		if !replayed {
			now := engine.now()
			state.Pending.Status = feedCredentialPendingDormant
			state.Pending.DormantCredentialReceiptSHA256 = dormantSHA256
			state.Pending.UpdatedAt = now
			state.UpdatedAt = now
			if err := engine.writeFeedCredentialOperationState(state, true); err != nil {
				return err
			}
		}
		credential, err := engine.feedCredentialStatusLocked()
		if err != nil {
			return err
		}
		report = FeedCredentialPromotionReport{
			SchemaVersion:                  FeedCredentialPromotionSchema,
			Registration:                   state.Pending.Registration,
			ManagedConfigSHA256:            config.configSHA256,
			NetworkBindingSHA256:           config.networkBinding.BindingSHA256,
			PausedAuthReceiptSHA256:        pausedSHA256,
			DormantCredentialReceiptSHA256: dormantSHA256,
			Credential:                     credential,
			Replayed:                       replayed,
		}
		return nil
	})
	return report, err
}

// ActivateFeedWorkerLast is the only CP2 activation primitive that enables and
// starts the fixed feed unit. It requires a durable dormant credential and
// converges ambiguous enable/start outcomes by inspecting systemd before each
// side effect and before finalizing the credential state.
func (engine *Engine) ActivateFeedWorkerLast(
	ctx context.Context,
	operationID string,
	verifierSHA256 string,
) (FeedWorkerActivationReport, error) {
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return FeedWorkerActivationReport{}, err
	}
	if err := validateRequiredFeedCredentialBinding(operationID, verifierSHA256); err != nil {
		return FeedWorkerActivationReport{}, err
	}

	var report FeedWorkerActivationReport
	err := engine.withOperationLock(func() error {
		state, exists, err := engine.readFeedCredentialOperationStateOptional()
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("feed credential operation state is missing")
		}
		if state.Pending == nil {
			if state.Active == nil || state.LastOperation == nil ||
				state.LastOperation.Status != "activated" ||
				requireRegistrationBinding(*state.Active, operationID, verifierSHA256) != nil ||
				state.LastOperation.DormantCredentialReceiptSHA256 == "" {
				return fmt.Errorf("no completed worker-last activation exists")
			}
			worker, err := engine.inspectFeedWorkerState(ctx)
			if err != nil {
				return err
			}
			if !worker.present || !worker.active || !worker.enabled {
				return fmt.Errorf("completed feed worker activation readback changed")
			}
			report = feedWorkerActivationReport(*state.LastOperation, worker, true)
			return nil
		}
		if err := requireRegistrationBinding(state.Pending.Registration, operationID, verifierSHA256); err != nil {
			return err
		}
		if state.Pending.Status != feedCredentialPendingDormant &&
			state.Pending.Status != feedCredentialPendingWorkerStarting {
			return fmt.Errorf("feed credential is not dormant for worker activation")
		}
		if err := validateDormantFeedCredentialPending(*state.Pending); err != nil {
			return err
		}
		config, configPresent, err := engine.readManagedFeedConfigOptional(engine.Paths.FeedEnvironmentFile)
		if err != nil {
			return err
		}
		if !configPresent || config.configSHA256 != state.Pending.ManagedConfigSHA256 ||
			config.networkBinding.BindingSHA256 != state.Pending.NetworkBindingSHA256 {
			return fmt.Errorf("managed feed configuration changed after dormant promotion")
		}
		if _, stagedConfig, err := engine.readManagedFeedConfigOptional(engine.managedFeedConfigStagedPath()); err != nil {
			return err
		} else if stagedConfig {
			return fmt.Errorf("unexpected staged managed feed configuration remains")
		}
		active, present, err := engine.inspectFeedCredentialOptional(engine.Paths.FeedCredentialFile)
		if err != nil {
			return err
		}
		if !present || credentialMatchesRegistration(active, state.Pending.Registration) != nil {
			return fmt.Errorf("dormant feed credential file readback mismatch")
		}
		if _, staged, err := engine.inspectFeedCredentialOptional(engine.Paths.FeedCredentialNext); err != nil {
			return err
		} else if staged {
			return fmt.Errorf("unexpected staged feed credential remains")
		}

		worker, err := engine.inspectFeedWorkerState(ctx)
		if err != nil {
			return err
		}
		if !worker.present {
			return fmt.Errorf("feed worker unit is not installed")
		}
		if state.Pending.Status == feedCredentialPendingDormant {
			if worker.active || worker.enabled {
				return fmt.Errorf("feed worker changed before worker-last activation authority")
			}
			now := engine.now()
			state.Pending.Status = feedCredentialPendingWorkerStarting
			state.Pending.WorkerActivationStarted = true
			state.Pending.UpdatedAt = now
			state.UpdatedAt = now
			if err := engine.writeFeedCredentialOperationState(state, true); err != nil {
				return err
			}
			if err := engine.runFeedCredentialPhaseHook("worker_activation_started"); err != nil {
				return fmt.Errorf("feed worker activation interrupted before systemd effects: %w", err)
			}
		}

		if !worker.enabled {
			if err := runSystemctl(ctx, engine.Runner, "enable", feedWorkerUnitName); err != nil {
				return fmt.Errorf("enable feed worker: %w", err)
			}
			worker, err = engine.inspectFeedWorkerState(ctx)
			if err != nil {
				return err
			}
		}
		if !worker.active {
			if err := runSystemctl(ctx, engine.Runner, "start", feedWorkerUnitName); err != nil {
				return fmt.Errorf("start feed worker: %w", err)
			}
		}
		worker, err = engine.inspectFeedWorkerState(ctx)
		if err != nil {
			return err
		}
		if !worker.present || !worker.active || !worker.enabled {
			return fmt.Errorf("feed worker activation readback is incomplete")
		}
		if err := engine.runFeedCredentialPhaseHook("worker_activation_readback"); err != nil {
			return fmt.Errorf("feed worker activation interrupted after readback: %w", err)
		}

		completedAt := engine.now()
		activated := state.Pending.Registration
		terminal := feedCredentialTerminal{
			Status:                         "activated",
			Registration:                   activated,
			ManagedConfigSHA256:            state.Pending.ManagedConfigSHA256,
			NetworkBindingSHA256:           state.Pending.NetworkBindingSHA256,
			PausedAuthReceiptSHA256:        state.Pending.PausedAuthReceiptSHA256,
			DormantCredentialReceiptSHA256: state.Pending.DormantCredentialReceiptSHA256,
			CompletedAt:                    completedAt,
		}
		state.Active = &activated
		state.LastOperation = &terminal
		state.Pending = nil
		state.UpdatedAt = completedAt
		if err := engine.writeFeedCredentialOperationState(state, true); err != nil {
			return err
		}
		report = feedWorkerActivationReport(terminal, worker, false)
		return nil
	})
	return report, err
}

func (engine *Engine) ActivateFeedCredential(
	ctx context.Context,
	operationID string,
	verifierSHA256 string,
) (FeedCredentialStatusReport, error) {
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return FeedCredentialStatusReport{}, err
	}
	if err := validateRequiredFeedCredentialBinding(operationID, verifierSHA256); err != nil {
		return FeedCredentialStatusReport{}, err
	}

	var report FeedCredentialStatusReport
	err := engine.withOperationLock(func() error {
		if err := engine.ensureFeedCredentialDirectories(); err != nil {
			return err
		}
		state, exists, err := engine.readFeedCredentialOperationStateOptional()
		if err != nil {
			return err
		}
		if !exists || state.Pending == nil {
			return fmt.Errorf("no pending feed credential operation exists")
		}
		if err := requireRegistrationBinding(state.Pending.Registration, operationID, verifierSHA256); err != nil {
			return err
		}
		if state.Pending.Status != feedCredentialPendingPrepared &&
			state.Pending.Status != feedCredentialPendingActivating {
			return fmt.Errorf("feed credential is not prepared for activation")
		}

		active, activePresent, err := engine.inspectFeedCredentialOptional(engine.Paths.FeedCredentialFile)
		if err != nil {
			return err
		}
		staged, stagedPresent, err := engine.inspectFeedCredentialOptional(engine.Paths.FeedCredentialNext)
		if err != nil {
			return err
		}

		activationBarrierCrossed := activePresent &&
			credentialMatchesRegistration(active, state.Pending.Registration) == nil
		if !activationBarrierCrossed {
			if err := validateCredentialAgainstOptionalRegistration(active, activePresent, state.Active); err != nil {
				return err
			}
			if !stagedPresent {
				return fmt.Errorf("prepared feed credential staged file is missing")
			}
			if err := credentialMatchesRegistration(staged, state.Pending.Registration); err != nil {
				return err
			}
		} else if stagedPresent {
			return fmt.Errorf("feed credential activation has conflicting active and staged files")
		}

		if !activationBarrierCrossed {
			if err := engine.verifyStagedFeedCredentialAuthentication(ctx, staged.token); err != nil {
				return err
			}
		}

		if state.Pending.Status != feedCredentialPendingActivating {
			now := engine.now()
			state.Pending.Status = feedCredentialPendingActivating
			state.Pending.UpdatedAt = now
			state.UpdatedAt = now
			if err := engine.writeFeedCredentialOperationState(state, true); err != nil {
				return err
			}
			if err := engine.runFeedCredentialPhaseHook("state_activating"); err != nil {
				return fmt.Errorf("feed credential activation interrupted before atomic replace: %w", err)
			}
		}

		if !activationBarrierCrossed {
			if err := os.Rename(engine.Paths.FeedCredentialNext, engine.Paths.FeedCredentialFile); err != nil {
				return fmt.Errorf("atomically replace feed credential: %w", err)
			}
			if err := syncDirectory(filepath.Dir(engine.Paths.FeedCredentialFile)); err != nil {
				return fmt.Errorf("sync feed credential directory: %w", err)
			}
			active, activePresent, err = engine.inspectFeedCredentialOptional(engine.Paths.FeedCredentialFile)
			if err != nil {
				return err
			}
			if !activePresent {
				return fmt.Errorf("activated feed credential file is missing")
			}
			if err := credentialMatchesRegistration(active, state.Pending.Registration); err != nil {
				return err
			}
		}
		if err := engine.runFeedCredentialPhaseHook("credential_replaced"); err != nil {
			return fmt.Errorf("feed credential activation interrupted after atomic replace: %w", err)
		}

		if err := runSystemctl(ctx, engine.Runner, "restart", feedWorkerUnitName); err != nil {
			return fmt.Errorf("restart feed worker after credential activation: %w", err)
		}
		workerState, err := engine.inspectFeedWorkerState(ctx)
		if err != nil {
			return fmt.Errorf("inspect feed worker after credential activation: %w", err)
		}
		if !workerState.present || !workerState.active {
			return fmt.Errorf("feed worker did not become active after credential activation")
		}
		if err := engine.runFeedCredentialPhaseHook("worker_restarted"); err != nil {
			return fmt.Errorf("feed credential activation interrupted after worker restart: %w", err)
		}

		completedAt := engine.now()
		activated := state.Pending.Registration
		state.Active = &activated
		state.LastOperation = &feedCredentialTerminal{
			Status:       "activated",
			Registration: activated,
			CompletedAt:  completedAt,
		}
		state.Pending = nil
		state.UpdatedAt = completedAt
		if err := engine.writeFeedCredentialOperationState(state, true); err != nil {
			return err
		}
		report, err = engine.feedCredentialStatusLocked()
		return err
	})
	return report, err
}

func (engine *Engine) AbortFeedCredential(
	ctx context.Context,
	operationID string,
	verifierSHA256 string,
) (FeedCredentialStatusReport, error) {
	_ = ctx
	if err := requireMutationAuthority(engine.Paths); err != nil {
		return FeedCredentialStatusReport{}, err
	}
	if err := validateRequiredFeedCredentialBinding(operationID, verifierSHA256); err != nil {
		return FeedCredentialStatusReport{}, err
	}

	var report FeedCredentialStatusReport
	err := engine.withOperationLock(func() error {
		if err := engine.ensureFeedCredentialDirectories(); err != nil {
			return err
		}
		state, exists, err := engine.readFeedCredentialOperationStateOptional()
		if err != nil {
			return err
		}
		if !exists || state.Pending == nil {
			return fmt.Errorf("no pending feed credential operation exists")
		}
		if err := requireRegistrationBinding(state.Pending.Registration, operationID, verifierSHA256); err != nil {
			return err
		}

		active, activePresent, err := engine.inspectFeedCredentialOptional(engine.Paths.FeedCredentialFile)
		if err != nil {
			return err
		}
		if activePresent && credentialMatchesRegistration(active, state.Pending.Registration) == nil {
			return fmt.Errorf("feed credential activation barrier was crossed; resume activation instead of aborting")
		}
		if err := validateCredentialAgainstOptionalRegistration(active, activePresent, state.Active); err != nil {
			return err
		}

		var staged inspectedFeedCredential
		var stagedPresent bool
		var stagedComplete = true
		if state.Pending.Status == feedCredentialPendingPreparing {
			staged, stagedPresent, stagedComplete, err = engine.inspectFeedCredentialForRecoveryOptional(
				engine.Paths.FeedCredentialNext,
			)
		} else {
			staged, stagedPresent, err = engine.inspectFeedCredentialOptional(engine.Paths.FeedCredentialNext)
		}
		if err != nil {
			return err
		}
		if stagedPresent {
			if !stagedComplete {
				if state.Pending.Status != feedCredentialPendingPreparing {
					return fmt.Errorf("feed credential staged file is incomplete outside prepare recovery")
				}
			} else if err := credentialMatchesRegistration(staged, state.Pending.Registration); err != nil {
				return err
			}
			if err := os.Remove(engine.Paths.FeedCredentialNext); err != nil {
				return fmt.Errorf("remove exact staged feed credential: %w", err)
			}
			if err := syncDirectory(filepath.Dir(engine.Paths.FeedCredentialNext)); err != nil {
				return fmt.Errorf("sync feed credential directory after abort: %w", err)
			}
			if err := engine.runFeedCredentialPhaseHook("staged_secret_removed"); err != nil {
				return fmt.Errorf("feed credential abort interrupted after staged secret removal: %w", err)
			}
		}

		completedAt := engine.now()
		state.LastOperation = &feedCredentialTerminal{
			Status:       "aborted",
			Registration: state.Pending.Registration,
			CompletedAt:  completedAt,
		}
		state.Pending = nil
		state.UpdatedAt = completedAt
		if err := engine.writeFeedCredentialOperationState(state, true); err != nil {
			return err
		}
		report, err = engine.feedCredentialStatusLocked()
		return err
	})
	return report, err
}

func (engine *Engine) feedCredentialStatusLocked() (FeedCredentialStatusReport, error) {
	report := FeedCredentialStatusReport{
		SchemaVersion: FeedCredentialStatusSchema,
		Status:        "absent",
		GeneratedAt:   engine.now(),
	}
	state, exists, err := engine.readFeedCredentialOperationStateOptional()
	if err != nil {
		return FeedCredentialStatusReport{}, err
	}
	active, activePresent, err := engine.inspectFeedCredentialOptional(engine.Paths.FeedCredentialFile)
	if err != nil {
		return FeedCredentialStatusReport{}, err
	}
	var staged inspectedFeedCredential
	var stagedPresent bool
	var stagedComplete = true
	if exists && state.Pending != nil && state.Pending.Status == feedCredentialPendingPreparing {
		staged, stagedPresent, stagedComplete, err = engine.inspectFeedCredentialForRecoveryOptional(
			engine.Paths.FeedCredentialNext,
		)
	} else {
		staged, stagedPresent, err = engine.inspectFeedCredentialOptional(engine.Paths.FeedCredentialNext)
	}
	if err != nil {
		return FeedCredentialStatusReport{}, err
	}
	report.ActiveFile = feedCredentialFileStatus(active, activePresent, nil)
	report.StagedFile = feedCredentialFileStatus(staged, stagedPresent, nil)

	if !exists {
		if activePresent || stagedPresent {
			return FeedCredentialStatusReport{}, fmt.Errorf("unmanaged feed credential file exists without durable operation state")
		}
		return report, nil
	}
	if state.Active != nil {
		if err := credentialMatchesRegistrationIfPresent(active, activePresent, *state.Active); err != nil {
			activatingMatch := state.Pending != nil &&
				state.Pending.Status == feedCredentialPendingActivating &&
				activePresent &&
				credentialMatchesRegistration(active, state.Pending.Registration) == nil
			if !activatingMatch {
				return FeedCredentialStatusReport{}, err
			}
		}
		activeRegistration := *state.Active
		report.ActiveRegistration = &activeRegistration
		report.ActiveFile = feedCredentialFileStatus(active, activePresent, state.Active)
		report.Status = "active"
	} else if activePresent {
		if state.Pending == nil ||
			(state.Pending.Status != feedCredentialPendingActivating &&
				state.Pending.Status != feedCredentialPendingPromotingDormant &&
				state.Pending.Status != feedCredentialPendingDormant &&
				state.Pending.Status != feedCredentialPendingWorkerStarting) ||
			credentialMatchesRegistration(active, state.Pending.Registration) != nil {
			return FeedCredentialStatusReport{}, fmt.Errorf("active feed credential is not bound to durable operation state")
		}
	}
	if state.LastOperation != nil {
		report.LastOperation = &FeedCredentialTerminalStatus{
			Status:                         state.LastOperation.Status,
			Registration:                   state.LastOperation.Registration,
			ManagedConfigSHA256:            state.LastOperation.ManagedConfigSHA256,
			NetworkBindingSHA256:           state.LastOperation.NetworkBindingSHA256,
			PausedAuthReceiptSHA256:        state.LastOperation.PausedAuthReceiptSHA256,
			DormantCredentialReceiptSHA256: state.LastOperation.DormantCredentialReceiptSHA256,
			CompletedAt:                    state.LastOperation.CompletedAt,
		}
	}
	if state.Pending == nil {
		if stagedPresent {
			return FeedCredentialStatusReport{}, fmt.Errorf("staged feed credential exists without a pending operation")
		}
		return report, nil
	}

	pendingRegistration := state.Pending.Registration
	report.PendingRegistration = &pendingRegistration
	report.PendingPhase = state.Pending.Status
	report.Status = state.Pending.Status
	report.RestartRequired = state.Pending.Status == feedCredentialPendingPrepared ||
		state.Pending.Status == feedCredentialPendingActivating
	switch state.Pending.Status {
	case feedCredentialPendingPreparing:
		if stagedPresent {
			if !stagedComplete {
				report.StagedFile = feedCredentialFileStatus(staged, true, nil)
				report.RequiredAction = "abort"
				break
			}
			if err := credentialMatchesRegistration(staged, state.Pending.Registration); err != nil {
				return FeedCredentialStatusReport{}, err
			}
			report.StagedFile = feedCredentialFileStatus(staged, true, &state.Pending.Registration)
			report.RequiredAction = "prepare"
		} else {
			report.RequiredAction = "abort"
		}
	case feedCredentialPendingPrepared:
		if !stagedPresent {
			report.RestartRequired = false
			report.RequiredAction = "abort"
			break
		}
		if err := credentialMatchesRegistration(staged, state.Pending.Registration); err != nil {
			return FeedCredentialStatusReport{}, err
		}
		report.StagedFile = feedCredentialFileStatus(staged, true, &state.Pending.Registration)
		report.RequiredAction = "activate_or_abort"
	case feedCredentialPendingActivating:
		switch {
		case activePresent && credentialMatchesRegistration(active, state.Pending.Registration) == nil:
			if stagedPresent {
				return FeedCredentialStatusReport{}, fmt.Errorf("activating feed credential has conflicting active and staged files")
			}
			report.ActiveFile = feedCredentialFileStatus(active, true, &state.Pending.Registration)
			report.RequiredAction = "activate"
		case stagedPresent:
			if err := credentialMatchesRegistration(staged, state.Pending.Registration); err != nil {
				return FeedCredentialStatusReport{}, err
			}
			if err := validateCredentialAgainstOptionalRegistration(active, activePresent, state.Active); err != nil {
				return FeedCredentialStatusReport{}, err
			}
			report.StagedFile = feedCredentialFileStatus(staged, true, &state.Pending.Registration)
			report.RequiredAction = "activate_or_abort"
		default:
			return FeedCredentialStatusReport{}, fmt.Errorf("activating feed credential has no bound staged or active file")
		}
	case feedCredentialPendingPromotingDormant:
		if activePresent && credentialMatchesRegistration(active, state.Pending.Registration) == nil {
			if stagedPresent {
				return FeedCredentialStatusReport{}, fmt.Errorf("dormant promotion has conflicting active and staged files")
			}
			report.ActiveFile = feedCredentialFileStatus(active, true, &state.Pending.Registration)
			report.RequiredAction = "promote_dormant"
			break
		}
		if !stagedPresent || credentialMatchesRegistration(staged, state.Pending.Registration) != nil {
			return FeedCredentialStatusReport{}, fmt.Errorf("dormant promotion has no bound staged credential")
		}
		report.StagedFile = feedCredentialFileStatus(staged, true, &state.Pending.Registration)
		report.RequiredAction = "promote_dormant"
	case feedCredentialPendingDormant, feedCredentialPendingWorkerStarting:
		if err := validateDormantFeedCredentialPending(*state.Pending); err != nil {
			return FeedCredentialStatusReport{}, err
		}
		if !activePresent || credentialMatchesRegistration(active, state.Pending.Registration) != nil || stagedPresent {
			return FeedCredentialStatusReport{}, fmt.Errorf("dormant feed credential files do not match durable state")
		}
		report.ActiveFile = feedCredentialFileStatus(active, true, &state.Pending.Registration)
		report.RequiredAction = "activate_worker_last"
	default:
		return FeedCredentialStatusReport{}, fmt.Errorf("feed credential pending state is invalid")
	}
	return report, nil
}

func (engine *Engine) generateFeedCredentialRegistration() (string, FeedCredentialRegistration, error) {
	tokenBytes := make([]byte, feedCredentialTokenBytes)
	if _, err := io.ReadFull(engine.feedCredentialRandomReader(), tokenBytes); err != nil {
		return "", FeedCredentialRegistration{}, fmt.Errorf("generate feed credential entropy: %w", err)
	}
	operationBytes := make([]byte, 16)
	if _, err := io.ReadFull(engine.feedCredentialRandomReader(), operationBytes); err != nil {
		return "", FeedCredentialRegistration{}, fmt.Errorf("generate feed credential operation identity: %w", err)
	}
	token := feedCredentialTokenPrefix + hex.EncodeToString(tokenBytes)
	registration := FeedCredentialRegistration{
		SchemaVersion:  FeedCredentialRegistrationSchema,
		TokenFormat:    FeedCredentialTokenFormat,
		VerifierScheme: FeedCredentialVerifierScheme,
		KeyPrefix:      token[:20],
		VerifierSHA256: feedCredentialVerifier(token),
		OperationID:    hex.EncodeToString(operationBytes),
	}
	if err := validateFeedCredentialRegistration(registration); err != nil {
		return "", FeedCredentialRegistration{}, err
	}
	return token, registration, nil
}

func (engine *Engine) feedCredentialRandomReader() io.Reader {
	if engine.Random != nil {
		return engine.Random
	}
	return rand.Reader
}

func (engine *Engine) verifyStagedFeedCredentialAuthentication(ctx context.Context, token string) error {
	if !feedCredentialTokenPattern.MatchString(token) {
		return errFeedCredentialAuthPreflight
	}
	if engine.FeedCredentialAuthCheck != nil {
		if err := engine.FeedCredentialAuthCheck(ctx, token); err != nil {
			return errFeedCredentialAuthPreflight
		}
		return nil
	}

	values, err := engine.readFeedCredentialEnvironment()
	if err != nil {
		return errFeedCredentialAuthPreflight
	}
	runtimeConfig, err := config.ParseUniRouterFeedRuntimeConfig(
		values[config.UniRouterFeedEndpointEnv],
		values[config.UniRouterFeedPollIntervalEnv],
		values[config.UniRouterFeedAllowHTTPEnv],
		values[config.UniRouterFeedMappingModeEnv],
	)
	if err != nil {
		return errFeedCredentialAuthPreflight
	}
	source, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{
		Endpoint:               runtimeConfig.Endpoint,
		ServiceKey:             token,
		AllowPrivateTunnelHTTP: runtimeConfig.AllowPrivateTunnelHTTP,
	})
	if err != nil {
		return errFeedCredentialAuthPreflight
	}
	if err := source.VerifyPausedAuthentication(ctx); err != nil {
		return errFeedCredentialAuthPreflight
	}
	return nil
}

func (engine *Engine) readFeedCredentialEnvironment() (map[string]string, error) {
	path := engine.Paths.FeedEnvironmentFile
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("feed environment file path is invalid")
	}
	before, err := os.Lstat(path)
	if err != nil ||
		!before.Mode().IsRegular() ||
		before.Size() <= 0 ||
		before.Size() > maxEnvironmentFileBytes {
		return nil, fmt.Errorf("feed environment file type or size is invalid")
	}
	permissions := before.Mode().Perm()
	if permissions&0o200 == 0 ||
		permissions&0o022 != 0 ||
		before.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return nil, fmt.Errorf("feed environment file permissions are invalid")
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	expectedUID, _ := engine.feedCredentialExpectedOwner()
	if !ok || stat.Uid != expectedUID || stat.Nlink != 1 {
		return nil, fmt.Errorf("feed environment file ownership or link count is invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("feed environment file cannot be opened")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return nil, fmt.Errorf("feed environment file changed while opening")
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxEnvironmentFileBytes+1))
	if err != nil || len(contents) > maxEnvironmentFileBytes {
		return nil, fmt.Errorf("feed environment file cannot be read safely")
	}

	allowed := map[string]struct{}{
		config.UniRouterFeedEndpointEnv:     {},
		config.UniRouterFeedPollIntervalEnv: {},
		config.UniRouterFeedAllowHTTPEnv:    {},
		config.UniRouterFeedMappingModeEnv:  {},
	}
	values := make(map[string]string, len(allowed))
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, present := strings.Cut(line, "=")
		if !present ||
			key == "" ||
			value == "" ||
			strings.TrimSpace(key) != key ||
			strings.TrimSpace(value) != value {
			return nil, fmt.Errorf("feed environment file contains an invalid assignment")
		}
		if _, supported := allowed[key]; !supported {
			return nil, fmt.Errorf("feed environment file contains an unsupported key")
		}
		if _, duplicate := values[key]; duplicate {
			return nil, fmt.Errorf("feed environment file contains a duplicate key")
		}
		values[key] = value
	}
	for _, required := range []string{
		config.UniRouterFeedEndpointEnv,
		config.UniRouterFeedPollIntervalEnv,
		config.UniRouterFeedAllowHTTPEnv,
	} {
		if _, exists := values[required]; !exists {
			return nil, fmt.Errorf("feed environment file does not contain the required runtime keys")
		}
	}
	return values, nil
}

func feedCredentialVerifier(token string) string {
	digest := sha256.New()
	_, _ = digest.Write([]byte(FeedCredentialVerifierDomain))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(token))
	return hex.EncodeToString(digest.Sum(nil))
}

func validateFeedCredentialRegistration(registration FeedCredentialRegistration) error {
	if registration.SchemaVersion != FeedCredentialRegistrationSchema ||
		registration.TokenFormat != FeedCredentialTokenFormat ||
		registration.VerifierScheme != FeedCredentialVerifierScheme ||
		!feedCredentialPrefixPattern.MatchString(registration.KeyPrefix) ||
		!feedCredentialVerifierPattern.MatchString(registration.VerifierSHA256) ||
		!feedCredentialOperationPattern.MatchString(registration.OperationID) {
		return fmt.Errorf("feed credential registration bundle is invalid")
	}
	return nil
}

func validateOptionalFeedCredentialBinding(operationID string, verifierSHA256 string) error {
	if operationID == "" && verifierSHA256 == "" {
		return nil
	}
	return validateRequiredFeedCredentialBinding(operationID, verifierSHA256)
}

func validateRequiredFeedCredentialBinding(operationID string, verifierSHA256 string) error {
	if !feedCredentialOperationPattern.MatchString(operationID) ||
		!feedCredentialVerifierPattern.MatchString(verifierSHA256) {
		return fmt.Errorf("feed credential operation requires exact lowercase operation_id and verifier_sha256")
	}
	return nil
}

func feedCredentialDormantReceiptDigests(
	registration FeedCredentialRegistration,
	managedConfigSHA256 string,
	networkBindingSHA256 string,
) (string, string, error) {
	if err := validateFeedCredentialRegistration(registration); err != nil {
		return "", "", err
	}
	if !hostctlSHA256Pattern.MatchString(managedConfigSHA256) ||
		!hostctlSHA256Pattern.MatchString(networkBindingSHA256) {
		return "", "", fmt.Errorf("dormant feed credential authority digest is invalid")
	}
	pausedSHA256, _, err := connectorcontrol.CanonicalSHA256(feedPausedAuthReadback{
		SchemaVersion:        FeedPausedAuthReadbackSchema,
		Registration:         registration,
		ManagedConfigSHA256:  managedConfigSHA256,
		NetworkBindingSHA256: networkBindingSHA256,
		Status:               "paused_authenticated",
	})
	if err != nil {
		return "", "", err
	}
	registrationSHA256, _, err := connectorcontrol.CanonicalSHA256(registration)
	if err != nil {
		return "", "", err
	}
	dormantSHA256, _, err := connectorcontrol.CanonicalSHA256(feedDormantCredentialReadback{
		SchemaVersion:           FeedDormantCredentialReadbackSchema,
		Registration:            registration,
		ManagedConfigSHA256:     managedConfigSHA256,
		NetworkBindingSHA256:    networkBindingSHA256,
		PausedAuthReceiptSHA256: pausedSHA256,
		CredentialBinding:       registrationSHA256,
		Status:                  "dormant",
	})
	if err != nil {
		return "", "", err
	}
	return pausedSHA256, dormantSHA256, nil
}

func validateDormantFeedCredentialPending(pending feedCredentialPending) error {
	if pending.Status != feedCredentialPendingDormant &&
		pending.Status != feedCredentialPendingWorkerStarting {
		return fmt.Errorf("feed credential is not in a dormant activation phase")
	}
	if !hostctlSHA256Pattern.MatchString(pending.ManagedConfigSHA256) ||
		!hostctlSHA256Pattern.MatchString(pending.NetworkBindingSHA256) ||
		!hostctlSHA256Pattern.MatchString(pending.PausedAuthReceiptSHA256) ||
		!hostctlSHA256Pattern.MatchString(pending.DormantCredentialReceiptSHA256) ||
		(pending.Status == feedCredentialPendingDormant && pending.WorkerActivationStarted) ||
		(pending.Status == feedCredentialPendingWorkerStarting && !pending.WorkerActivationStarted) {
		return fmt.Errorf("dormant feed credential state is invalid")
	}
	pausedSHA256, dormantSHA256, err := feedCredentialDormantReceiptDigests(
		pending.Registration,
		pending.ManagedConfigSHA256,
		pending.NetworkBindingSHA256,
	)
	if err != nil || pausedSHA256 != pending.PausedAuthReceiptSHA256 ||
		dormantSHA256 != pending.DormantCredentialReceiptSHA256 {
		return fmt.Errorf("dormant feed credential receipts do not match")
	}
	return nil
}

func feedWorkerActivationReport(
	terminal feedCredentialTerminal,
	worker feedWorkerState,
	replayed bool,
) FeedWorkerActivationReport {
	return FeedWorkerActivationReport{
		SchemaVersion:                  FeedWorkerActivationSchema,
		Registration:                   terminal.Registration,
		ManagedConfigSHA256:            terminal.ManagedConfigSHA256,
		NetworkBindingSHA256:           terminal.NetworkBindingSHA256,
		PausedAuthReceiptSHA256:        terminal.PausedAuthReceiptSHA256,
		DormantCredentialReceiptSHA256: terminal.DormantCredentialReceiptSHA256,
		Present:                        worker.present,
		Active:                         worker.active,
		Enabled:                        worker.enabled,
		Replayed:                       replayed,
	}
}

func requireRegistrationBinding(
	registration FeedCredentialRegistration,
	operationID string,
	verifierSHA256 string,
) error {
	if registration.OperationID != operationID || registration.VerifierSHA256 != verifierSHA256 {
		return fmt.Errorf("feed credential operation binding does not match")
	}
	return nil
}

func credentialMatchesRegistration(
	credential inspectedFeedCredential,
	registration FeedCredentialRegistration,
) error {
	if err := validateFeedCredentialRegistration(registration); err != nil {
		return err
	}
	if len(credential.token) != feedCredentialTokenLength ||
		!feedCredentialTokenPattern.MatchString(credential.token) {
		return fmt.Errorf("feed credential file grammar is invalid")
	}
	if credential.token[:20] != registration.KeyPrefix ||
		feedCredentialVerifier(credential.token) != registration.VerifierSHA256 {
		return fmt.Errorf("feed credential file does not match the expected verifier and operation binding")
	}
	return nil
}

func credentialMatchesRegistrationIfPresent(
	credential inspectedFeedCredential,
	present bool,
	registration FeedCredentialRegistration,
) error {
	if !present {
		return fmt.Errorf("active feed credential file is missing")
	}
	return credentialMatchesRegistration(credential, registration)
}

func validateCredentialAgainstOptionalRegistration(
	credential inspectedFeedCredential,
	present bool,
	registration *FeedCredentialRegistration,
) error {
	if registration == nil {
		if present {
			return fmt.Errorf("unexpected active feed credential file exists")
		}
		return nil
	}
	return credentialMatchesRegistrationIfPresent(credential, present, *registration)
}

func (engine *Engine) validateExpectedActiveBinding(
	state feedCredentialOperationState,
	operationID string,
	verifierSHA256 string,
) error {
	active, present, err := engine.inspectFeedCredentialOptional(engine.Paths.FeedCredentialFile)
	if err != nil {
		return err
	}
	if state.Active == nil {
		if operationID != "" || verifierSHA256 != "" {
			return fmt.Errorf("expected active feed credential binding was supplied but no managed active credential exists")
		}
		if present {
			return fmt.Errorf("unmanaged active feed credential exists")
		}
		return nil
	}
	if err := requireRegistrationBinding(*state.Active, operationID, verifierSHA256); err != nil {
		return err
	}
	return credentialMatchesRegistrationIfPresent(active, present, *state.Active)
}

func (engine *Engine) ensureFeedCredentialDirectories() error {
	if err := ensureDirectory(engine.Paths.EtcDir, 0o755); err != nil {
		return err
	}
	if err := engine.validateFeedCredentialDirectory(engine.Paths.EtcDir); err != nil {
		return err
	}
	if err := ensureDirectory(filepath.Dir(engine.Paths.FeedCredentialState), 0o700); err != nil {
		return err
	}
	return engine.validateFeedCredentialDirectory(filepath.Dir(engine.Paths.FeedCredentialState))
}

func (engine *Engine) validateFeedCredentialDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("feed credential parent directory is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("feed credential parent directory ownership is unavailable")
	}
	expectedUID, expectedGID := engine.feedCredentialExpectedOwner()
	if uint32(stat.Uid) != expectedUID || uint32(stat.Gid) != expectedGID {
		return fmt.Errorf("feed credential parent directory ownership is invalid")
	}
	return nil
}

func (engine *Engine) feedCredentialExpectedOwner() (uint32, uint32) {
	if engine.Paths.IsTestRoot {
		return uint32(os.Geteuid()), uint32(os.Getegid())
	}
	return 0, 0
}

func (engine *Engine) writeFeedCredentialExclusive(path string, token string) (FeedCredentialFileMetadata, error) {
	if !feedCredentialTokenPattern.MatchString(token) || len(token) != feedCredentialTokenLength {
		return FeedCredentialFileMetadata{}, fmt.Errorf("generated feed credential grammar is invalid")
	}
	flags := syscall.O_WRONLY | syscall.O_CREAT | syscall.O_EXCL | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	descriptor, err := syscall.Open(path, flags, 0o600)
	if err != nil {
		return FeedCredentialFileMetadata{}, err
	}
	file := os.NewFile(uintptr(descriptor), filepath.Base(path))
	if file == nil {
		_ = syscall.Close(descriptor)
		return FeedCredentialFileMetadata{}, fmt.Errorf("create feed credential file handle")
	}
	if !engine.Paths.IsTestRoot {
		if err := file.Chown(0, 0); err != nil {
			_ = file.Close()
			return FeedCredentialFileMetadata{}, err
		}
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return FeedCredentialFileMetadata{}, err
	}
	if _, err := io.WriteString(file, token); err != nil {
		_ = file.Close()
		return FeedCredentialFileMetadata{}, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return FeedCredentialFileMetadata{}, err
	}
	if err := file.Close(); err != nil {
		return FeedCredentialFileMetadata{}, err
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return FeedCredentialFileMetadata{}, err
	}
	credential, present, err := engine.inspectFeedCredentialOptional(path)
	if err != nil {
		return FeedCredentialFileMetadata{}, err
	}
	if !present || credential.token != token {
		return FeedCredentialFileMetadata{}, fmt.Errorf("staged feed credential changed after creation")
	}
	return credential.metadata, nil
}

func (engine *Engine) inspectFeedCredentialOptional(path string) (inspectedFeedCredential, bool, error) {
	before, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return inspectedFeedCredential{}, false, nil
	}
	if err != nil {
		return inspectedFeedCredential{}, false, fmt.Errorf("inspect feed credential file: %w", err)
	}
	metadata, err := engine.validateFeedCredentialFileMetadata(before)
	if err != nil {
		return inspectedFeedCredential{}, false, err
	}
	descriptor, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return inspectedFeedCredential{}, false, fmt.Errorf("open feed credential file safely")
	}
	file := os.NewFile(uintptr(descriptor), filepath.Base(path))
	if file == nil {
		_ = syscall.Close(descriptor)
		return inspectedFeedCredential{}, false, fmt.Errorf("open feed credential file handle")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return inspectedFeedCredential{}, false, fmt.Errorf("feed credential file changed while opening")
	}
	contents, err := io.ReadAll(io.LimitReader(file, int64(feedCredentialTokenLength+1)))
	if err != nil || len(contents) != feedCredentialTokenLength ||
		!feedCredentialTokenPattern.Match(contents) {
		return inspectedFeedCredential{}, false, fmt.Errorf("feed credential file grammar is invalid")
	}
	return inspectedFeedCredential{token: string(contents), metadata: metadata}, true, nil
}

func (engine *Engine) inspectFeedCredentialForRecoveryOptional(
	path string,
) (inspectedFeedCredential, bool, bool, error) {
	before, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return inspectedFeedCredential{}, false, false, nil
	}
	if err != nil {
		return inspectedFeedCredential{}, false, false, fmt.Errorf("inspect feed credential file: %w", err)
	}
	metadata, err := engine.validateFeedCredentialFileRecoveryMetadata(before)
	if err != nil {
		return inspectedFeedCredential{}, false, false, err
	}
	descriptor, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return inspectedFeedCredential{}, false, false, fmt.Errorf("open feed credential file safely")
	}
	file := os.NewFile(uintptr(descriptor), filepath.Base(path))
	if file == nil {
		_ = syscall.Close(descriptor)
		return inspectedFeedCredential{}, false, false, fmt.Errorf("open feed credential file handle")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return inspectedFeedCredential{}, false, false, fmt.Errorf("feed credential file changed while opening")
	}
	contents, err := io.ReadAll(io.LimitReader(file, int64(feedCredentialTokenLength+1)))
	if err != nil || int64(len(contents)) != before.Size() {
		return inspectedFeedCredential{}, false, false, fmt.Errorf("feed credential recovery file changed while reading")
	}
	if len(contents) == feedCredentialTokenLength {
		if !feedCredentialTokenPattern.Match(contents) {
			return inspectedFeedCredential{}, false, false, fmt.Errorf("feed credential file grammar is invalid")
		}
		return inspectedFeedCredential{
			token:    string(contents),
			metadata: metadata,
		}, true, true, nil
	}
	return inspectedFeedCredential{metadata: metadata}, true, false, nil
}

func (engine *Engine) validateFeedCredentialFileMetadata(info os.FileInfo) (FeedCredentialFileMetadata, error) {
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm() != 0o600 || info.Size() != int64(feedCredentialTokenLength) ||
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return FeedCredentialFileMetadata{}, fmt.Errorf("feed credential file metadata is invalid")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return FeedCredentialFileMetadata{}, fmt.Errorf("feed credential file ownership is unavailable")
	}
	expectedUID, expectedGID := engine.feedCredentialExpectedOwner()
	if uint32(stat.Uid) != expectedUID || uint32(stat.Gid) != expectedGID || uint64(stat.Nlink) != 1 {
		return FeedCredentialFileMetadata{}, fmt.Errorf("feed credential file ownership or link count is invalid")
	}
	return FeedCredentialFileMetadata{
		SizeBytes: info.Size(),
		Mode:      "0600",
		OwnerUID:  uint32(stat.Uid),
		OwnerGID:  uint32(stat.Gid),
		LinkCount: uint64(stat.Nlink),
	}, nil
}

func (engine *Engine) validateFeedCredentialFileRecoveryMetadata(
	info os.FileInfo,
) (FeedCredentialFileMetadata, error) {
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm() != 0o600 || info.Size() < 0 ||
		info.Size() > int64(feedCredentialTokenLength) ||
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return FeedCredentialFileMetadata{}, fmt.Errorf("feed credential recovery file metadata is invalid")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return FeedCredentialFileMetadata{}, fmt.Errorf("feed credential recovery file ownership is unavailable")
	}
	expectedUID, expectedGID := engine.feedCredentialExpectedOwner()
	if uint32(stat.Uid) != expectedUID || uint32(stat.Gid) != expectedGID || uint64(stat.Nlink) != 1 {
		return FeedCredentialFileMetadata{}, fmt.Errorf("feed credential recovery file ownership or link count is invalid")
	}
	return FeedCredentialFileMetadata{
		SizeBytes: info.Size(),
		Mode:      "0600",
		OwnerUID:  uint32(stat.Uid),
		OwnerGID:  uint32(stat.Gid),
		LinkCount: uint64(stat.Nlink),
	}, nil
}

func feedCredentialFileStatus(
	credential inspectedFeedCredential,
	present bool,
	registration *FeedCredentialRegistration,
) FeedCredentialFileStatus {
	status := FeedCredentialFileStatus{Present: present}
	if !present {
		return status
	}
	metadata := credential.metadata
	status.Metadata = &metadata
	status.BindingMatches = registration != nil &&
		credentialMatchesRegistration(credential, *registration) == nil
	return status
}

func (engine *Engine) readFeedCredentialOperationStateOptional() (feedCredentialOperationState, bool, error) {
	before, err := os.Lstat(engine.Paths.FeedCredentialState)
	if errors.Is(err, os.ErrNotExist) {
		return feedCredentialOperationState{}, false, nil
	}
	if err != nil {
		return feedCredentialOperationState{}, false, err
	}
	if _, err := engine.validateFeedCredentialStateMetadata(before); err != nil {
		return feedCredentialOperationState{}, false, err
	}
	descriptor, err := syscall.Open(
		engine.Paths.FeedCredentialState,
		syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,
		0,
	)
	if err != nil {
		return feedCredentialOperationState{}, false, fmt.Errorf("open feed credential operation state safely")
	}
	file := os.NewFile(uintptr(descriptor), filepath.Base(engine.Paths.FeedCredentialState))
	if file == nil {
		_ = syscall.Close(descriptor)
		return feedCredentialOperationState{}, false, fmt.Errorf("open feed credential operation state handle")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return feedCredentialOperationState{}, false, fmt.Errorf("feed credential operation state changed while opening")
	}
	var state feedCredentialOperationState
	decoder := json.NewDecoder(io.LimitReader(file, maxControlFileBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return feedCredentialOperationState{}, false, fmt.Errorf("decode feed credential operation state: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return feedCredentialOperationState{}, false, fmt.Errorf("feed credential operation state contains trailing content")
	}
	if err := validateFeedCredentialOperationState(state); err != nil {
		return feedCredentialOperationState{}, false, err
	}
	return state, true, nil
}

func (engine *Engine) validateFeedCredentialStateMetadata(info os.FileInfo) (FeedCredentialFileMetadata, error) {
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm() != 0o600 || info.Size() <= 0 || info.Size() > maxControlFileBytes ||
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return FeedCredentialFileMetadata{}, fmt.Errorf("feed credential operation state metadata is invalid")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return FeedCredentialFileMetadata{}, fmt.Errorf("feed credential operation state ownership is unavailable")
	}
	expectedUID, expectedGID := engine.feedCredentialExpectedOwner()
	if uint32(stat.Uid) != expectedUID || uint32(stat.Gid) != expectedGID || uint64(stat.Nlink) != 1 {
		return FeedCredentialFileMetadata{}, fmt.Errorf("feed credential operation state ownership or link count is invalid")
	}
	return FeedCredentialFileMetadata{
		SizeBytes: info.Size(),
		Mode:      "0600",
		OwnerUID:  uint32(stat.Uid),
		OwnerGID:  uint32(stat.Gid),
		LinkCount: uint64(stat.Nlink),
	}, nil
}

func (engine *Engine) writeFeedCredentialOperationState(
	state feedCredentialOperationState,
	exists bool,
) error {
	state.SchemaVersion = FeedCredentialStateSchema
	if err := validateFeedCredentialOperationState(state); err != nil {
		return err
	}
	var err error
	if exists {
		err = writeJSONAtomic(engine.Paths.FeedCredentialState, state, 0o600)
	} else {
		err = writeJSONExclusive(engine.Paths.FeedCredentialState, state, 0o600)
	}
	if err != nil {
		return err
	}
	info, err := os.Lstat(engine.Paths.FeedCredentialState)
	if err != nil {
		return err
	}
	_, err = engine.validateFeedCredentialStateMetadata(info)
	return err
}

func validateFeedCredentialOperationState(state feedCredentialOperationState) error {
	if state.SchemaVersion != FeedCredentialStateSchema || state.UpdatedAt.IsZero() {
		return fmt.Errorf("feed credential operation state is invalid")
	}
	if state.Active != nil {
		if err := validateFeedCredentialRegistration(*state.Active); err != nil {
			return err
		}
	}
	if state.Pending != nil {
		if err := validateFeedCredentialRegistration(state.Pending.Registration); err != nil {
			return err
		}
		if state.Pending.Status != feedCredentialPendingPreparing &&
			state.Pending.Status != feedCredentialPendingPrepared &&
			state.Pending.Status != feedCredentialPendingActivating &&
			state.Pending.Status != feedCredentialPendingPromotingDormant &&
			state.Pending.Status != feedCredentialPendingDormant &&
			state.Pending.Status != feedCredentialPendingWorkerStarting {
			return fmt.Errorf("feed credential pending phase is invalid")
		}
		if state.Pending.StartedAt.IsZero() || state.Pending.UpdatedAt.IsZero() {
			return fmt.Errorf("feed credential pending timestamps are invalid")
		}
		if (state.Pending.Status == feedCredentialPendingPrepared ||
			state.Pending.Status == feedCredentialPendingActivating ||
			state.Pending.Status == feedCredentialPendingPromotingDormant ||
			state.Pending.Status == feedCredentialPendingDormant ||
			state.Pending.Status == feedCredentialPendingWorkerStarting) &&
			state.Pending.PreparedAt == nil {
			return fmt.Errorf("feed credential prepared timestamp is missing")
		}
		switch state.Pending.Status {
		case feedCredentialPendingPreparing, feedCredentialPendingPrepared, feedCredentialPendingActivating:
			if state.Pending.ManagedConfigSHA256 != "" || state.Pending.NetworkBindingSHA256 != "" ||
				state.Pending.PausedAuthReceiptSHA256 != "" || state.Pending.DormantCredentialReceiptSHA256 != "" ||
				state.Pending.WorkerActivationStarted {
				return fmt.Errorf("legacy feed credential phase contains dormant activation authority")
			}
		case feedCredentialPendingPromotingDormant:
			pausedSHA256, _, err := feedCredentialDormantReceiptDigests(
				state.Pending.Registration,
				state.Pending.ManagedConfigSHA256,
				state.Pending.NetworkBindingSHA256,
			)
			if err != nil || pausedSHA256 != state.Pending.PausedAuthReceiptSHA256 ||
				state.Pending.DormantCredentialReceiptSHA256 != "" || state.Pending.WorkerActivationStarted {
				return fmt.Errorf("promoting dormant feed credential authority is invalid")
			}
		case feedCredentialPendingDormant, feedCredentialPendingWorkerStarting:
			if err := validateDormantFeedCredentialPending(*state.Pending); err != nil {
				return err
			}
		}
		if state.Active != nil &&
			state.Active.OperationID == state.Pending.Registration.OperationID {
			return fmt.Errorf("active and pending feed credential operation identities collide")
		}
	}
	if state.LastOperation != nil {
		if state.LastOperation.Status != "activated" && state.LastOperation.Status != "aborted" {
			return fmt.Errorf("feed credential terminal status is invalid")
		}
		if err := validateFeedCredentialRegistration(state.LastOperation.Registration); err != nil {
			return err
		}
		if state.LastOperation.CompletedAt.IsZero() {
			return fmt.Errorf("feed credential terminal timestamp is invalid")
		}
		hasDormantAuthority := state.LastOperation.ManagedConfigSHA256 != "" ||
			state.LastOperation.NetworkBindingSHA256 != "" ||
			state.LastOperation.PausedAuthReceiptSHA256 != "" ||
			state.LastOperation.DormantCredentialReceiptSHA256 != ""
		if state.LastOperation.Status == "aborted" && hasDormantAuthority {
			return fmt.Errorf("aborted feed credential contains dormant activation authority")
		}
		if hasDormantAuthority {
			pausedSHA256, dormantSHA256, err := feedCredentialDormantReceiptDigests(
				state.LastOperation.Registration,
				state.LastOperation.ManagedConfigSHA256,
				state.LastOperation.NetworkBindingSHA256,
			)
			if err != nil || pausedSHA256 != state.LastOperation.PausedAuthReceiptSHA256 ||
				dormantSHA256 != state.LastOperation.DormantCredentialReceiptSHA256 {
				return fmt.Errorf("terminal dormant feed credential authority is invalid")
			}
		}
	}
	return nil
}

func (engine *Engine) runFeedCredentialPhaseHook(phase string) error {
	if engine.FeedCredentialPhaseHook == nil {
		return nil
	}
	return engine.FeedCredentialPhaseHook(phase)
}
