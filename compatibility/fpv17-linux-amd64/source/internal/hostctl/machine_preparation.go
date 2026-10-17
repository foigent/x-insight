package hostctl

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/connectorcontrol"
)

// Only public registration/custody metadata crosses the privileged boundary.
// Each installation/purpose has a separate root-owned file and stable operation.
type MachinePreparation struct {
	SourceInstance       string                                     `json:"source_instance"`
	OrgID                string                                     `json:"org_id"`
	Registration         connectorcontrol.PublicMachineRegistration `json:"registration"`
	RegistrationSHA256   string                                     `json:"registration_sha256"`
	CustodyReceipt       connectorcontrol.MachineCustodyReceipt     `json:"custody_receipt"`
	CustodyReceiptSHA256 string                                     `json:"custody_receipt_sha256"`
}

func (e *Engine) MachineCredentialPath(installation, purpose string) (string, error) {
	if !connectorcontrol.IsInstallationID(installation) {
		return "", connectorcontrol.ErrInvalid
	}
	name := ""
	switch purpose {
	case connectorcontrol.PolicyMachinePurpose:
		name = "policy"
	case connectorcontrol.EnrollmentMachinePurpose:
		name = "enrollment"
	default:
		return "", connectorcontrol.ErrInvalid
	}
	return filepath.Join(e.Paths.EtcDir, "connector-machines", installation, name+".key"), nil
}
func (e *Engine) PrepareMachineCredential(ctx context.Context, installation, purpose, source, org, allowedIP string) (MachinePreparation, error) {
	_ = ctx
	var result MachinePreparation
	if requireMutationAuthority(e.Paths) != nil || source == "" || !regexpPositiveID(org) {
		return result, connectorcontrol.ErrInvalid
	}
	path, err := e.MachineCredentialPath(installation, purpose)
	if err != nil {
		return result, err
	}
	prefix := "ur-xpm1-"
	if purpose == connectorcontrol.EnrollmentMachinePurpose {
		prefix = "ur-xse1-"
	}
	probe := connectorcontrol.PublicMachineRegistration{InstallationID: installation, Purpose: purpose, KeyPrefix: prefix + strings.Repeat("0", 12), VerifierSHA256: strings.Repeat("0", 64), OperationID: strings.Repeat("0", 32), AllowedIP: allowedIP}
	if connectorcontrol.ValidatePublicMachineRegistration(probe) != nil {
		return result, connectorcontrol.ErrInvalid
	}
	candidate := URMachineConfig{InstallationID: installation, SourceInstance: source, OrgID: org, PolicyFile: path}
	if purpose == connectorcontrol.EnrollmentMachinePurpose {
		candidate.PolicyFile = ""
		candidate.EnrollFile = path
	}
	if _, _, err := ParseURMachineConfig(machineConfigValues(candidate)); err != nil {
		return result, err
	}

	err = e.withOperationLock(func() error {
		if err := e.ensureNoOutstandingOperation(); err != nil {
			return err
		}
		if err := e.validateReferenceParent(path); err != nil {
			return err
		}
		prefix, domain := "ur-xpm1-", "unirouter.xinsight.policy-manage.v1"
		if purpose == connectorcontrol.EnrollmentMachinePurpose {
			prefix, domain = "ur-xse1-", "unirouter.xinsight.source-enroll.v1"
		}
		present, err := fileExists(path)
		if err != nil {
			return err
		}
		if !present {
			random := io.Reader(rand.Reader)
			if e.Random != nil {
				random = e.Random
			}
			secret := make([]byte, 32)
			if _, err := io.ReadFull(random, secret); err != nil {
				return fmt.Errorf("machine random source unavailable")
			}
			token := prefix + hex.EncodeToString(secret)
			clear(secret)
			if err := e.writeConnectorEnvironmentExclusive(path, []byte(token+"\n")); err != nil {
				return err
			}
		}
		if err := e.validateRootReference(path, 0600); err != nil {
			return err
		}
		token, err := config.LoadOwnerOnlySecretFile(path)
		if err != nil || config.ValidateURMachineToken(token, strings.TrimSuffix(strings.TrimPrefix(prefix, "ur-"), "-")) != nil {
			return fmt.Errorf("prepared machine custody is invalid")
		}
		verifier := sha256.Sum256([]byte(domain + "\x00" + token))
		// No implicit rotation: a single operation belongs to this exact purpose.
		op := sha256.Sum256([]byte("xinsight.machine-preparation.v1\x00" + installation + "\x00" + purpose))
		r := connectorcontrol.PublicMachineRegistration{InstallationID: installation, Purpose: purpose, KeyPrefix: token[:20], VerifierSHA256: hex.EncodeToString(verifier[:]), OperationID: hex.EncodeToString(op[:16]), AllowedIP: allowedIP}
		if connectorcontrol.ValidatePublicMachineRegistration(r) != nil {
			return connectorcontrol.ErrInvalid
		}
		h, _, err := connectorcontrol.CanonicalSHA256(r)
		if err != nil {
			return err
		}
		c := connectorcontrol.MachineCustodyReceipt{InstallationID: installation, Purpose: purpose, OperationID: r.OperationID, RegistrationSHA256: h, CustodyState: "prepared"}
		ch, _, err := connectorcontrol.CanonicalSHA256(c)
		if err != nil {
			return err
		}
		result = MachinePreparation{SourceInstance: source, OrgID: org, Registration: r, RegistrationSHA256: h, CustodyReceipt: c, CustodyReceiptSHA256: ch}
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		state := path + ".prepared.json"
		if exists, err := fileExists(state); err != nil {
			return err
		} else if exists {
			if err := e.validateRootReference(state, 0600); err != nil {
				return err
			}
			prior, err := os.ReadFile(state)
			if err != nil || string(prior) != string(raw) {
				return fmt.Errorf("prepared machine identity conflicts; rotation is not implicit")
			}
		}
		if err := e.publishReference(state, raw, 0600); err != nil {
			return err
		}
		// Readback has validated key format, ownership, links and exact public bytes.
		return e.validateRootReference(path, 0600)
	})
	return result, err
}
func regexpPositiveID(s string) bool {
	if len(s) == 0 || len(s) > 19 || s[0] == '0' {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (e *Engine) WithPreparedMachine(ctx context.Context, r connectorcontrol.PublicMachineRegistration, commit func() error) error {
	_ = ctx
	if requireMutationAuthority(e.Paths) != nil || connectorcontrol.ValidatePublicMachineRegistration(r) != nil || commit == nil {
		return connectorcontrol.ErrInvalid
	}
	path, e2 := e.MachineCredentialPath(r.InstallationID, r.Purpose)
	if e2 != nil {
		return e2
	}
	return e.withOperationLock(func() error {
		if e2 := e.ensureNoOutstandingOperation(); e2 != nil {
			return e2
		}
		if e2 := e.validateRootReference(path, 0600); e2 != nil {
			return e2
		}
		if e2 := e.validateRootReference(path+".prepared.json", 0600); e2 != nil {
			return e2
		}
		raw, e2 := os.ReadFile(path + ".prepared.json")
		if e2 != nil {
			return e2
		}
		var p MachinePreparation
		if json.Unmarshal(raw, &p) != nil || p.Registration != r || !connectorcontrol.MatchesCanonicalDigest(r, p.RegistrationSHA256) || !connectorcontrol.MatchesCanonicalDigest(p.CustodyReceipt, p.CustodyReceiptSHA256) {
			return connectorcontrol.ErrConflict
		}
		token, e2 := config.LoadOwnerOnlySecretFile(path)
		if e2 != nil {
			return e2
		}
		domain, format := "unirouter.xinsight.policy-manage.v1", "xpm1"
		if r.Purpose == connectorcontrol.EnrollmentMachinePurpose {
			domain, format = "unirouter.xinsight.source-enroll.v1", "xse1"
		}
		if config.ValidateURMachineToken(token, format) != nil {
			return connectorcontrol.ErrConflict
		}
		verifier := sha256.Sum256([]byte(domain + "\x00" + token))
		if r.KeyPrefix != token[:20] || r.VerifierSHA256 != hex.EncodeToString(verifier[:]) {
			return connectorcontrol.ErrConflict
		}
		return commit()
	})
}

// Hold the existing host maintenance lock while a short local DB ACK commits.
// Neither a network call nor a human action is permitted in the callback.
func (e *Engine) WithExistingFeed(ctx context.Context, expected connectorcontrol.ExistingWorkerConfig, registration connectorcontrol.PublicReceiverRegistrationV1, commit func() error) error {
	_ = ctx
	if requireMutationAuthority(e.Paths) != nil || commit == nil {
		return connectorcontrol.ErrInvalid
	}
	return e.withOperationLock(func() error {
		if err := e.ensureNoOutstandingOperation(); err != nil {
			return err
		}
		cfg, present, err := e.readManagedFeedConfigOptional(e.Paths.FeedEnvironmentFile)
		if err != nil || !present || cfg.trustedOrigin != expected.UROrigin || expected.WorkerUnit != "xinsight-feed.service" || expected.FeedRoute != connectorcontrol.ReceiverFeedRoute || expected.FeedSchema != connectorcontrol.ControlFeedSchema || expected.CursorKey != connectorcontrol.ControlCursorKey {
			return fmt.Errorf("original feed configuration differs")
		}
		if next, err := fileExists(e.managedFeedConfigStagedPath()); err != nil || next {
			return fmt.Errorf("original feed has staged configuration")
		}
		state, present, err := e.readFeedCredentialOperationStateOptional()
		if err != nil || !present || state.Active == nil || state.Pending != nil {
			return fmt.Errorf("original receiver identity is unavailable")
		}
		active := connectorcontrol.PublicReceiverRegistrationV1{SchemaVersion: state.Active.SchemaVersion, TokenFormat: state.Active.TokenFormat, VerifierScheme: state.Active.VerifierScheme, KeyPrefix: state.Active.KeyPrefix, VerifierSHA256: state.Active.VerifierSHA256, OperationID: state.Active.OperationID}
		if active != registration || !connectorcontrol.MatchesCanonicalDigest(active, expected.ReceiverRegistrationSHA256) {
			return fmt.Errorf("original receiver identity differs")
		}
		report, err := e.feedCredentialStatusLocked()
		if err != nil || !report.ActiveFile.BindingMatches || report.StagedFile.Present {
			return fmt.Errorf("original receiver custody differs")
		}
		return commit()
	})
}
