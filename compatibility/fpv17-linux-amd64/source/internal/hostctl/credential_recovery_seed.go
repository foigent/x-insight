package hostctl

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/foigent/x-insight/internal/connectorcontrol"
)

type CredentialRecoverySeed struct {
	RequestID      string    `json:"request_id"`
	ActorSubject   string    `json:"actor_subject"`
	InstallationID string    `json:"installation_id"`
	RecoveryID     string    `json:"recovery_id"`
	Ticket         string    `json:"ticket"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
}

func credentialRecoveryFailureClass(err error) (string, string) {
	switch {
	case errors.Is(err, ErrOperationBusy):
		return "operation_busy", "none"
	case errors.Is(err, connectorcontrol.ErrSetupUnauthorized):
		return "unauthorized", "none"
	case errors.Is(err, connectorcontrol.ErrConflict):
		return "conflict", "none"
	case errors.Is(err, connectorcontrol.ErrExpired):
		return "expired", "none"
	case errors.Is(err, context.DeadlineExceeded):
		return "context_deadline", "none"
	case errors.Is(err, context.Canceled):
		return "context_cancelled", "none"
	case errors.Is(err, os.ErrPermission):
		return "file_permission", "none"
	case errors.Is(err, os.ErrNotExist):
		return "file_missing", "none"
	}
	var sqlError interface{ SQLState() string }
	if errors.As(err, &sqlError) {
		switch sqlError.SQLState() {
		case "23505", "40001", "42501", "55P03", "57014":
			return "database_sqlstate", sqlError.SQLState()
		}
	}
	return "internal", "none"
}

// A request ID has one root-owned random ticket. A lost HTTP response reuses
// these exact bytes; PostgreSQL stores only their digest.
func (e *Engine) WithCredentialRecoverySeed(_ context.Context, actor, installationID, requestID string, run func(CredentialRecoverySeed) error) (result error) {
	phase := "validate"
	defer func() {
		if result != nil {
			category, sqlState := credentialRecoveryFailureClass(result)
			log.Printf("connector credential recovery action=issue phase=%s category=%s sqlstate=%s", phase, category, sqlState)
		}
	}()
	if requireMutationAuthority(e.Paths) != nil || !connectorcontrol.IsInstallationID(installationID) ||
		!connectorcontrol.SetupRequestIDPattern.MatchString(requestID) || run == nil {
		return connectorcontrol.ErrInvalid
	}
	phase = "host_lock"
	return e.withOperationLock(func() error {
		phase = "host_operation"
		if err := e.ensureNoOutstandingOperation(); err != nil {
			return err
		}
		phase = "host_authorize"
		if err := e.AuthorizeSetupActor(actor, false); err != nil {
			return connectorcontrol.ErrSetupUnauthorized
		}
		phase = "seed_file"
		root := filepath.Join(e.Paths.EtcDir, "connector-credential-recoveries")
		file := filepath.Join(root, requestID+".seed.json")
		if err := e.validateReferenceParent(file); err != nil {
			return err
		}
		var seed CredentialRecoverySeed
		if exists, err := fileExists(file); err != nil {
			return err
		} else if exists {
			if err := e.validateRootReference(file, 0600); err != nil {
				return err
			}
			if err := readJSONStrict(file, &seed); err != nil {
				return err
			}
			if seed.RequestID != requestID || seed.ActorSubject != actor || seed.InstallationID != installationID {
				return connectorcontrol.ErrConflict
			}
		} else {
			reader := io.Reader(rand.Reader)
			if e.Random != nil {
				reader = e.Random
			}
			idRaw, ticketRaw := make([]byte, 16), make([]byte, 32)
			if _, err := io.ReadFull(reader, idRaw); err != nil {
				return err
			}
			if _, err := io.ReadFull(reader, ticketRaw); err != nil {
				return err
			}
			now := e.now().UTC().Truncate(time.Microsecond)
			seed = CredentialRecoverySeed{RequestID: requestID, ActorSubject: actor, InstallationID: installationID,
				RecoveryID: "xrr_" + base64.RawURLEncoding.EncodeToString(idRaw),
				Ticket:     "xirt_" + base64.RawURLEncoding.EncodeToString(ticketRaw), CreatedAt: now,
				ExpiresAt: now.Add(connectorcontrol.ControlRecoveryTTL)}
			clear(idRaw)
			clear(ticketRaw)
			if err := ensureDirectory(root, 0700); err != nil {
				return err
			}
			if err := writeJSONAtomicExclusive(file, seed, 0600); err != nil {
				return err
			}
		}
		phase = "seed_integrity"
		if !connectorcontrol.IsRecoveryID(seed.RecoveryID) || !connectorcontrol.IsRecoveryTicket(seed.Ticket) ||
			seed.ExpiresAt.Sub(seed.CreatedAt) != connectorcontrol.ControlRecoveryTTL {
			return fmt.Errorf("credential recovery seed invalid")
		}
		if !e.now().Before(seed.ExpiresAt) {
			return connectorcontrol.ErrExpired
		}
		phase = "store_callback"
		return run(seed)
	})
}
