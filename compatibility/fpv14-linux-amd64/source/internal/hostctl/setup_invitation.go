package hostctl

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"io"
	"path/filepath"
	"time"
)

type SetupInvitationSeed struct {
	RequestID     string    `json:"request_id"`
	ActorSubject  string    `json:"actor_subject"`
	DeploymentID  string    `json:"deployment_id"`
	RequestSHA256 string    `json:"request_sha256"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	Seed          string    `json:"seed"`
}

func (e *Engine) SetupPeerConfig() (connectorcontrol.InvitationConfig, error) {
	id, err := e.ConnectorDeploymentID()
	if err != nil {
		return connectorcontrol.InvitationConfig{}, err
	}
	origin, err := readConnectorTargetEnvironmentFile(e.Paths.ConnectorEnvironment)
	if err != nil {
		return connectorcontrol.InvitationConfig{}, err
	}
	v, err := readTrustedTLSProxyEnvironmentFile(e.Paths.TrustedTLSProxyEnv)
	if err != nil {
		return connectorcontrol.InvitationConfig{}, err
	}
	c := connectorcontrol.InvitationConfig{DeploymentID: id, UniRouterInstallOrigin: origin, XInsightOrigin: v[api.ExternalOriginEnv]}
	return c, c.Validate()
}
func (e *Engine) AuthorizeSetupActor(subject string, change bool) error {
	if err := e.validateRootReference(e.Paths.AccessAccountsFile, 0600); err != nil {
		return err
	}
	cfg, err := access.ReadAndValidateAccountFile(e.Paths.AccessAccountsFile)
	if err != nil {
		return err
	}
	for _, account := range cfg.Accounts {
		if account.SubjectID == subject && !account.Disabled && access.HasPermission(account.Role, access.PermissionConnectorInstallationsManage) && (!change || access.HasPermission(account.Role, access.PermissionReviewPolicyManage)) {
			return nil
		}
	}
	return connectorcontrol.ErrSetupUnauthorized
}

// Seed exists only inside the root-only existing credential boundary. Only the
// derived original invitation crosses the Unix response; no consumer gets seed.
func (e *Engine) WithSetupInvitationSeed(ctx context.Context, actor, hash string, put connectorcontrol.SetupConsentPut, existing bool, run func(SetupInvitationSeed, io.Reader) error) error {
	if requireMutationAuthority(e.Paths) != nil || !put.Valid() || !sha256ReferencePattern.MatchString(hash) || run == nil {
		return connectorcontrol.ErrInvalid
	}
	return e.withOperationLock(func() error {
		if err := e.ensureNoOutstandingOperation(); err != nil {
			return err
		}
		if err := e.AuthorizeSetupActor(actor, true); err != nil {
			return err
		}
		peer, err := e.SetupPeerConfig()
		if err != nil {
			return err
		}
		dir := filepath.Join(e.Paths.EtcDir, "connector-invitations")
		path := filepath.Join(dir, put.RequestID+".seed.json")
		if err = e.validateReferenceParent(path); err != nil {
			return err
		}
		var seed SetupInvitationSeed
		if present, err := fileExists(path); err != nil {
			return err
		} else if present {
			if err = e.validateRootReference(path, 0600); err != nil {
				return err
			}
			if err = readJSONStrict(path, &seed); err != nil {
				return err
			}
			if seed.RequestID != put.RequestID || seed.ActorSubject != actor || seed.RequestSHA256 != hash || seed.DeploymentID != peer.DeploymentID {
				return connectorcontrol.ErrConflict
			}
		} else {
			if existing {
				return connectorcontrol.ErrConflict
			}
			raw := make([]byte, 64)
			reader := io.Reader(rand.Reader)
			if e.Random != nil {
				reader = e.Random
			}
			if _, err = io.ReadFull(reader, raw); err != nil {
				return err
			}
			defer clear(raw)
			now := e.now().UTC().Truncate(time.Microsecond)
			seed = SetupInvitationSeed{RequestID: put.RequestID, ActorSubject: actor, DeploymentID: peer.DeploymentID, RequestSHA256: hash, CreatedAt: now, ExpiresAt: now.Add(connectorcontrol.DefaultInvitationTTL), Seed: hex.EncodeToString(raw)}
			if err = ensureDirectory(dir, 0700); err != nil {
				return err
			}
			if err = writeJSONAtomicExclusive(path, seed, 0600); err != nil {
				return err
			}
		}
		raw, err := hex.DecodeString(seed.Seed)
		if err != nil || len(raw) != 64 {
			return fmt.Errorf("setup invitation seed invalid")
		}
		defer clear(raw)
		if !e.now().Before(seed.ExpiresAt) {
			return connectorcontrol.ErrExpired
		}
		if seed.ExpiresAt.Sub(seed.CreatedAt) != connectorcontrol.DefaultInvitationTTL {
			return connectorcontrol.ErrConflict
		}
		return run(seed, bytes.NewReader(raw))
	})
}
