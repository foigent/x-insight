package hostctl

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/db"
	"github.com/jackc/pgx/v5"
)

// Only systemd/host process identity is simulated by the existing fixture.
// The supported Engine install/backup/RestoreBundle/Resume, SQL authority,
// full private dump, pg_restore, migrations and restricted roles are real.
type accountRetirementCrashManager struct {
	PostgresManager
	failAfterCommit bool
}

func (m *accountRetirementCrashManager) RetireRestoredAccountLinks(ctx context.Context, c DatabaseCredentials, operation string) error {
	if err := m.PostgresManager.RetireRestoredAccountLinks(ctx, c, operation); err != nil {
		return err
	}
	if m.failAfterCommit {
		m.failAfterCommit = false
		return errors.New("synthetic interruption after account retirement commit before journal completion")
	}
	return nil
}
func TestManagedAccountsNativePrivateBackupRestoreCommitGapResumePostgres(t *testing.T) {
	ctx, c := connectorDigestDatabase(t)
	f := newInstallFixture(t)
	c = digestSyntheticCredentials(t, f, c)
	manager := &accountRetirementCrashManager{PostgresManager: PostgresManager{Runner: ExecRunner{}}}
	f.engine.Database = manager
	const owner = "xs_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const reader = "xs_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const password = "synthetic native account password"
	hash, err := access.DerivePasswordHash([]byte(password))
	if err != nil {
		t.Fatal(err)
	}
	config := access.AccountConfig{SchemaVersion: access.AccountConfigSchemaV3, Accounts: []access.AccountConfigEntry{{SubjectID: owner, Username: "owner", Role: access.RolePlatformAdmin, PasswordHash: hash}, {SubjectID: reader, Username: "reader", Role: access.RoleScopedReader, PasswordHash: hash}}}
	writeHostJSON(t, f.paths.AccessAccountsFile, config, 0600)
	artifact := digestSyntheticArtifact(t, f, "0.2.0-dev.fpv21", 47, nil, nil)
	target, err := f.engine.verifyTarget(artifact)
	if err != nil {
		t.Fatal(err)
	}
	runner := f.engine.Runner.(*fakeRunner)
	f.engine.Runner = managedResumeRuntimeRunner{base: runner}
	f.engine.HTTPClient = &http.Client{Transport: managedResumeProductTransport{base: fixedProductTransport{identity: target.Identity, accessProtected: true}, engine: f.engine, runner: runner}}
	if _, err = f.engine.Install(ctx, artifact); err != nil {
		t.Fatal("native install", err)
	}
	store, err := db.OpenLocalAccountStore(ctx, c.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	actor := access.Principal{SubjectID: owner, Role: access.RolePlatformAdmin, AuthRevision: 1}
	token := "xi-ac1_" + strings.Repeat("B", 43)
	if _, err = store.ChangeAccount(ctx, actor, access.AccountChange{Action: "recover", SubjectID: reader, ExpectedRevision: 1}, access.TicketDigest(token)); err != nil {
		t.Fatal(err)
	}
	a, err := sql.Open("pgx", c.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	const source = "11111111-1111-4111-8111-111111111111"
	const grant = "22222222-2222-4222-8222-222222222222"
	if _, err = a.ExecContext(ctx, `INSERT INTO source_systems(id,source_key,source_type,display_name) VALUES($1,'native-account-fixture','unirouter','Synthetic account source')`, source); err != nil {
		t.Fatal(err)
	}
	if _, err = a.ExecContext(ctx, `INSERT INTO workbench_access_grants(id,subject_id,source_system_id,source_project_ref,allow_details,allow_coverage,allow_source_context) VALUES($1,$2,$3,'project-2',true,true,true)`, grant, reader, source); err != nil {
		t.Fatal(err)
	}
	var originalGrant string
	if err = a.QueryRowContext(ctx, `SELECT to_jsonb(g)::text FROM workbench_access_grants g WHERE id=$1`, grant).Scan(&originalGrant); err != nil {
		t.Fatal(err)
	}
	expected, err := store.Inventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	a.Close()
	path := filepath.Join(f.paths.BackupsDir, "account-authority.private.tar")
	receipt, err := f.engine.Backup(ctx, path)
	if err != nil {
		t.Fatal("native backup", err)
	}
	bundle, err := f.engine.inspectPrivateBackup(ctx, path, receipt.BackupSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Manifest.SchemaVersion != privateAccountBackupSchema || bundle.Manifest.Dump.LocalAccounts == nil || *bundle.Manifest.Dump.LocalAccounts != expected {
		t.Fatal("private backup lost exact bound account authority")
	}
	bundle.Clear()
	// Spend the original link AFTER the snapshot. Restoring that snapshot must
	// never revive it, even though the restored ticket row is still unconsumed.
	store, err = db.OpenLocalAccountStore(ctx, c.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := access.DerivePasswordHash([]byte("synthetic holder selected replacement"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CompletePassword(ctx, access.TicketDigest(token), replacement); err != nil {
		t.Fatal(err)
	}
	beforeCurrent, err := store.Inventory(ctx)
	store.Close()
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately stale legacy file: it is preserved, never a recovery authority.
	config.Accounts[1].Disabled = true
	writeHostJSON(t, f.paths.AccessAccountsFile, config, 0600)
	manager.failAfterCommit = true
	name := fmt.Sprintf("xinsight_restore_accounts_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		a, e := sql.Open("pgx", c.AdminDSN)
		if e != nil {
			t.Error(e)
			return
		}
		defer a.Close()
		if _, e = a.Exec("DROP DATABASE IF EXISTS " + pgx.Identifier{name}.Sanitize() + " WITH (FORCE)"); e != nil {
			t.Error(e)
		}
	})
	if _, err = f.engine.RestoreBundle(ctx, path, receipt.BackupSHA256, name, "field_validation"); err == nil || !strings.Contains(err.Error(), "restored_account_authority_validation_failed") {
		t.Fatal("expected commit-gap interruption", err)
	}
	j, err := f.engine.readActiveJournal()
	if err != nil {
		t.Fatal(err)
	}
	if containsString(j.CompletedPhases, "retire_restored_activation_links") || containsString(j.CompletedPhases, "switch_database_credentials") {
		t.Fatal("did not stop in exact DB-commit/journal gap")
	}
	current, err := LoadDatabaseCredentials(f.paths)
	if err != nil || current.SetRef != c.SetRef {
		t.Fatal("interrupted restore switched original credentials", err)
	}
	restored, err := LoadDatabaseCredentialSet(f.paths, j.DatabaseSetAfter)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := db.OpenLocalAccountStore(ctx, restored.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	retired, err := rs.Inventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if retired.LastRestoreOperation != j.OperationID || retired.TicketGeneration == expected.TicketGeneration || retired.AccountsSHA256 != expected.AccountsSHA256 || retired.TicketsSHA256 != expected.TicketsSHA256 {
		t.Fatal("retirement receipt or preserved rows mismatch")
	}
	if _, err = rs.CompletePassword(ctx, access.TicketDigest(token), replacement); !errors.Is(err, access.ErrAccountInvalid) {
		t.Fatal("snapshot resurrected spent activation link", err)
	}
	rs.Close()
	// A receipt for this operation is not permission to ignore unrelated drift.
	ra, err := sql.Open("pgx", restored.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	var legacy string
	if err = ra.QueryRow(`SELECT legacy_sha256 FROM xinsight_accounts.authority`).Scan(&legacy); err != nil {
		t.Fatal(err)
	}
	if _, err = ra.Exec(`UPDATE xinsight_accounts.authority SET legacy_sha256='synthetic-corruption'`); err != nil {
		t.Fatal(err)
	}
	ra.Close()
	if _, err = f.engine.Resume(ctx); err == nil || !strings.Contains(err.Error(), "restored_account_authority_validation_failed") {
		t.Fatal("same-operation receipt accepted unrelated authority corruption", err)
	}
	ra, err = sql.Open("pgx", restored.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ra.Exec(`UPDATE xinsight_accounts.authority SET legacy_sha256=$1`, legacy); err != nil {
		t.Fatal(err)
	}
	ra.Close()
	resumed, err := f.engine.Resume(ctx)
	if err != nil || resumed.Status != "completed" {
		t.Fatal("exact commit-gap resume failed", err)
	}
	final, err := manager.AccountInventory(ctx, restored)
	if err != nil || *final != retired {
		t.Fatal("resume reissued retirement or changed restored account rows", err)
	}
	original, err := manager.AccountInventory(ctx, c)
	if err != nil || *original != beforeCurrent {
		t.Fatal("restore changed original account authority", err)
	}
	ra, err = sql.Open("pgx", restored.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer ra.Close()
	var recoveredGrant string
	if err = ra.QueryRow(`SELECT to_jsonb(g)::text FROM workbench_access_grants g WHERE id=$1`, grant).Scan(&recoveredGrant); err != nil || recoveredGrant != originalGrant {
		t.Fatal("restore changed original subject/grant", err)
	}
	var n int
	if err = ra.QueryRow(`SELECT count(*) FROM access_audit_records WHERE action='access.account_authority.restore'`).Scan(&n); err != nil || n != 1 {
		t.Fatal("retirement not audited exactly once", n, err)
	}
	t.Log("real native private backup + isolated RestoreBundle + exact commit-before-journal Resume passed; systemd and binary identity remain fixture-only")
}

func TestManagedAccountsNativeSchema46UpgradeAndLegacyBackupForwardRestorePostgres(t *testing.T) {
	ctx, c := connectorDigestDatabase(t)
	f := newInstallFixture(t)
	c = digestSyntheticCredentials(t, f, c)
	manager := PostgresManager{Runner: ExecRunner{}}
	f.engine.Database = manager
	const owner = "xs_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const reader = "xs_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	hash, err := access.DerivePasswordHash([]byte("synthetic legacy account password"))
	if err != nil {
		t.Fatal(err)
	}
	config := access.AccountConfig{SchemaVersion: access.AccountConfigSchemaV3, Accounts: []access.AccountConfigEntry{{SubjectID: owner, Username: "owner", Role: access.RolePlatformAdmin, PasswordHash: hash}, {SubjectID: reader, Username: "reader", Role: access.RoleScopedReader, PasswordHash: hash}}}
	writeHostJSON(t, f.paths.AccessAccountsFile, config, 0600)
	oldArtifact := digestSyntheticArtifact(t, f, "0.2.0-dev.fpv20", 46, nil, nil)
	runner := f.engine.Runner.(*fakeRunner)
	f.engine.Runner = managedResumeRuntimeRunner{base: runner}
	runtime := func(artifact string) {
		t.Helper()
		target, e := f.engine.verifyTarget(artifact)
		if e != nil {
			t.Fatal(e)
		}
		f.engine.HTTPClient = &http.Client{Transport: managedResumeProductTransport{base: fixedProductTransport{identity: target.Identity, accessProtected: true}, engine: f.engine, runner: runner}}
	}
	runtime(oldArtifact)
	if _, err = f.engine.Install(ctx, oldArtifact); err != nil {
		t.Fatal("native schema46 installation", err)
	}
	a, err := sql.Open("pgx", c.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	const source = "33333333-3333-4333-8333-333333333333"
	const grant = "44444444-4444-4444-8444-444444444444"
	if _, err = a.Exec(`INSERT INTO source_systems(id,source_key,source_type,display_name) VALUES($1,'account-upgrade-fixture','unirouter','Synthetic upgrade source')`, source); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Exec(`INSERT INTO workbench_access_grants(id,subject_id,source_system_id,source_project_ref,allow_details,allow_coverage) VALUES($1,$2,$3,'project-2',true,true)`, grant, reader, source); err != nil {
		t.Fatal(err)
	}
	var originalGrant string
	if err = a.QueryRow(`SELECT to_jsonb(g)::text FROM workbench_access_grants g WHERE id=$1`, grant).Scan(&originalGrant); err != nil {
		t.Fatal(err)
	}
	a.Close()
	backupPath := filepath.Join(f.paths.BackupsDir, "pre-accounts-schema46.private.tar")
	backup, err := f.engine.Backup(ctx, backupPath)
	if err != nil {
		t.Fatal("schema46 private backup", err)
	}
	b, err := f.engine.inspectPrivateBackup(ctx, backupPath, backup.BackupSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if b.Manifest.SchemaVersion != privateBackupSchema || b.Manifest.Dump.LocalAccounts != nil {
		t.Fatal("historical v1 backup shape changed")
	}
	b.Clear()
	artifact := digestSyntheticArtifact(t, f, "0.2.0-dev.fpv21", 47, []string{"0.2.0-dev.fpv20"}, nil)
	runtime(artifact)
	if _, err = f.engine.Upgrade(ctx, artifact); err != nil {
		t.Fatal("actual schema46/v3 forward upgrade", err)
	}
	store, err := db.OpenLocalAccountStore(ctx, c.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	for _, old := range config.Accounts {
		actual, e := store.CurrentAccount(ctx, old.SubjectID)
		credential, ce := store.ManagedCredential(ctx, old.Username)
		if e != nil || ce != nil || actual.Role != old.Role || actual.SubjectID != old.SubjectID || actual.Disabled != old.Disabled || credential.PasswordHash != old.PasswordHash {
			t.Fatal("upgrade lost stable v3 account", e, ce)
		}
	}
	actor := access.Principal{SubjectID: owner, Role: access.RolePlatformAdmin, AuthRevision: 1}
	token := "xi-ac1_" + strings.Repeat("C", 43)
	if _, err = store.ChangeAccount(ctx, actor, access.AccountChange{Action: "recover", SubjectID: reader, ExpectedRevision: 1}, access.TicketDigest(token)); err != nil {
		t.Fatal(err)
	}
	before, err := store.Inventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	config.Accounts[0].Disabled = true
	config.Accounts[1].Disabled = true
	writeHostJSON(t, f.paths.AccessAccountsFile, config, 0600)
	name := fmt.Sprintf("xinsight_restore_legacy_accounts_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		a, e := sql.Open("pgx", c.AdminDSN)
		if e != nil {
			t.Error(e)
			return
		}
		defer a.Close()
		if _, e = a.Exec("DROP DATABASE IF EXISTS " + pgx.Identifier{name}.Sanitize() + " WITH (FORCE)"); e != nil {
			t.Error(e)
		}
	})
	if _, err = f.engine.RestoreBundle(ctx, backupPath, backup.BackupSHA256, name, "field_validation"); err != nil {
		t.Fatal("schema46/v1 backup forward restore on schema47", err)
	}
	restored, err := LoadDatabaseCredentials(f.paths)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := db.OpenLocalAccountStore(ctx, restored.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	after, err := rs.Inventory(ctx)
	if err != nil || after.AccountsSHA256 != before.AccountsSHA256 || after.TicketsSHA256 != before.TicketsSHA256 || after.AuthorityStateSHA256 != before.AuthorityStateSHA256 {
		t.Fatal("legacy restore did not retain current durable accounts", err)
	}
	current, err := manager.AccountInventory(ctx, c)
	if err != nil || *current != before {
		t.Fatal("legacy restore changed original current authority", err)
	}
	if _, err = rs.CompletePassword(ctx, access.TicketDigest(token), hash); !errors.Is(err, access.ErrAccountInvalid) {
		t.Fatal("legacy carry revived pre-restore link", err)
	}
	account, err := rs.CurrentAccount(ctx, owner)
	if err != nil || account.Disabled {
		t.Fatal("restore used stale owner file", err)
	}
	ra, err := sql.Open("pgx", restored.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer ra.Close()
	var actualGrant string
	if err = ra.QueryRow(`SELECT to_jsonb(g)::text FROM workbench_access_grants g WHERE id=$1`, grant).Scan(&actualGrant); err != nil || actualGrant != originalGrant {
		t.Fatal("legacy business Grant was changed", err)
	}
	t.Log("actual schema46/v3 native upgrade imports once; legacy v1 backup forward-restores current authority without obsolete file fallback")
}

func TestManagedAccountsNativeFirstOwnerPrivateHandoffPostgres(t *testing.T) {
	ctx, c := connectorDigestDatabase(t)
	f := newInstallFixture(t)
	c = digestSyntheticCredentials(t, f, c)
	manager := PostgresManager{Runner: ExecRunner{}}
	f.engine.Database = manager
	if err := manager.EnsureRoles(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RunMigrations(ctx, c.AdminDSN, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	if err := manager.ApplyGrants(ctx, c); err != nil {
		t.Fatal(err)
	}
	store, err := db.OpenLocalAccountStore(ctx, c.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Initialize(ctx, access.AccountConfig{SchemaVersion: access.AccountConfigSchemaV3}, "bootstrap_required"); err != nil {
		t.Fatal(err)
	}
	if err = f.engine.ensureControlDirectories(); err != nil {
		t.Fatal(err)
	}
	writeHostFile(t, f.paths.TrustedTLSProxyEnv, []byte("XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback\nXINSIGHT_EXTERNAL_ORIGIN=https://xi.synthetic.example\n"), 0600)
	private := filepath.Join(f.paths.Root, "private-account-handoff")
	if err = os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(private, "owner.html")
	request := AccountBootstrapRequest{Username: "owner", Output: output, CheckOnly: true}
	before, err := store.Inventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.engine.AccountBootstrap(ctx, request); err != nil {
		t.Fatal("native check", err)
	}
	after, err := store.Inventory(ctx)
	if err != nil || after != before {
		t.Fatal("native check changed account authority", err)
	}
	if _, err = os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("native check wrote private output", err)
	}
	request.CheckOnly = false
	result, err := f.engine.AccountBootstrap(ctx, request)
	if err != nil {
		t.Fatal("native first owner", err)
	}
	safe, err := json.Marshal(result)
	if err != nil || bytes.Contains(safe, []byte("xi-ac1_")) || bytes.Contains(safe, []byte("password_hash")) {
		t.Fatal("native result exposed bearer or hash")
	}
	info, err := os.Lstat(output)
	if err != nil || info.Mode().Perm() != 0600 || !info.Mode().IsRegular() {
		t.Fatal("private handoff mode/type invalid", err)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	link := regexp.MustCompile(`https://xi\.synthetic\.example/access/activate#xi-ac1_[A-Za-z0-9_-]{43}`).Find(raw)
	if len(link) == 0 {
		t.Fatal("private handoff did not use exact configured HTTPS activation page")
	}
	token := strings.SplitN(string(link), "#", 2)[1]
	hash, err := access.DerivePasswordHash([]byte("synthetic first-owner holder password"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CompletePassword(ctx, access.TicketDigest(token), hash); err != nil {
		t.Fatal("holder completion", err)
	}
	writer, err := db.OpenWorkspaceStore(ctx, c.WorkspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	provider, err := access.NewDurableProvider(writer.LocalAccounts())
	if err != nil {
		t.Fatal(err)
	}
	principal, err := provider.AuthenticateCurrent("owner", []byte("synthetic first-owner holder password"))
	if err != nil || principal.Role != access.RolePlatformAdmin {
		t.Fatal("first owner cannot sign in through restricted workspace", err)
	}
	if _, err = f.engine.AccountBootstrap(ctx, AccountBootstrapRequest{Username: "another.owner", Output: filepath.Join(private, "second.html"), CheckOnly: true}); err == nil {
		t.Fatal("native path admitted another first owner")
	}
	t.Log("actual native first-owner check is read-only, private0600 handoff is configured-origin-only, stdout metadata is non-secret, holder selects usable password")
}
