package api_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/workspace"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func responsibilityDigest(raw []byte) string {
	hash := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(hash[:])
}

func TestResponsibilityUpgrade25PreservesV1BytesAndMeaning(t *testing.T) {
	ctx, dsn, admin := responsibilityDatabase(t)
	migrations := filepath.Join("..", "..", "migrations")
	old := t.TempDir()
	entries, err := os.ReadDir(migrations)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") || entry.Name()[:6] > "000025" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(migrations, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(old, entry.Name()), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.RunMigrations(ctx, dsn, old); err != nil {
		t.Fatal(err)
	}
	store, err := db.OpenWorkspaceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	actor, _ := workspace.HumanActor("xs_1111111111111111")
	item, err := store.CreateCase(ctx, actor, workspace.CreateCaseInput{Title: "Historical v1 Case", Description: "Preserve old meaning and exact bytes"}, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	finding, err := store.CreateFinding(ctx, item.ID, actor, workspace.CreateFindingInput{Title: "Unreviewed legacy finding", Statement: "An old human hypothesis", Classification: "hypothesis"}, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.GetCase(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	pack, _, _, err := workspace.BuildEvidencePack(record, now, actor.Ref, record.Case.Version+1)
	if err != nil {
		t.Fatal(err)
	}
	pack.SchemaVersion, pack.Responsibility, pack.Assessments = workspace.EvidencePackSchemaV1, nil, nil
	oldBytes, err := json.MarshalIndent(pack, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	oldBytes = append(oldBytes, '\n') // deliberately noncanonical legacy bytes
	digest := responsibilityDigest(oldBytes)
	if _, err := admin.Exec(`UPDATE investigation_cases SET status='sealed',version=version+1,sealed_at=$2,sealed_by_subject=$3,sealed_revision=$6,sealed_digest=$4,sealed_pack=$5 WHERE id=$1`, item.ID, now, actor.Ref, digest, oldBytes, pack.Revision); err != nil {
		t.Fatal(err)
	}
	oldHTML, err := workspace.RenderEvidencePackHTML(pack, digest)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(oldHTML, []byte("Responsibility and due dates")) {
		t.Fatal("v1 inferred responsibility")
	}
	if _, err := db.RunMigrations(ctx, dsn, migrations); err != nil {
		t.Fatal(err)
	}
	_, workspaceDSN, readerDSN := prepareWorkbenchRuntimeRoles(t, ctx, dsn)
	runtimeStore, err := db.OpenWorkspaceStore(ctx, workspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeStore.Close()
	reader, err := db.OpenEvidenceStore(ctx, readerDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	hash, err := access.DerivePasswordHash([]byte("synthetic responsibility password"))
	if err != nil {
		t.Fatal(err)
	}
	f := &responsibilityFixture{store: runtimeStore, reader: reader, caseItem: item, finding: finding}
	f.reloadAccounts(t, access.AccountConfig{SchemaVersion: access.AccountConfigSchemaV2, Accounts: []access.AccountConfigEntry{{SubjectID: actor.Ref, Username: "reviewer.one", Role: access.RoleInvestigator, PasswordHash: hash}}})
	client := responsibilityLogin(t, f, "reviewer.one")
	path := "/api/v1/cases/" + item.ID + "/evidence-pack"
	if status, raw := client.call(t, "GET", path+".json", nil, nil); status != 200 || !bytes.Equal(raw, oldBytes) {
		t.Fatalf("v1 original bytes changed after upgrade: %d", status)
	}
	if status, raw := client.call(t, "GET", path+".html", nil, nil); status != 200 || !bytes.Equal(raw, oldHTML) {
		t.Fatalf("v1 HTML meaning changed: %d", status)
	}
	current, err := runtimeStore.GetFindingReview(ctx, item.ID, finding.ID)
	if err != nil || current.Followup.Recorded || current.Followup.AssigneeSubjectID != nil || len(current.History.Items) != 0 {
		t.Fatal("legacy finding gained inferred responsibility")
	}
	after, err := runtimeStore.GetCase(ctx, item.ID)
	if err != nil || after.Findings[0].Classification != finding.Classification || after.Findings[0].Status != finding.Status || !bytes.Equal(after.Case.SealedPack, oldBytes) || responsibilityDigest(after.Case.SealedPack) != digest {
		t.Fatal("old classification/status/package changed")
	}
	if err := db.ValidateFindingResponsibilityRestore(ctx, admin); err != nil {
		t.Fatal(err)
	}
	pack.SchemaVersion = "xinsight.evidence-pack.v999"
	unknown, _ := json.Marshal(pack)
	if _, err := admin.Exec(`UPDATE investigation_cases SET sealed_pack=$2,sealed_digest=$3 WHERE id=$1`, item.ID, unknown, responsibilityDigest(unknown)); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{".json", ".html"} {
		if status, _ := client.call(t, "GET", path+suffix, nil, nil); status == 200 {
			t.Fatal("unknown package version accepted")
		}
	}
	if err := db.ValidateFindingResponsibilityRestore(ctx, admin); err == nil {
		t.Fatal("restore accepted unknown package version")
	}
	t.Log("Actual schema 25 to 27 upgrade preserves v1 original bytes/hash/HTML and Finding meaning; no owner backfill; unknown JSON/HTML/restore rejected")
}

func responsibilityCredentials(t *testing.T, dsn string) hostctl.DatabaseCredentials {
	t.Helper()
	c := hostctl.DatabaseCredentials{AdminDSN: dsn}
	var err error
	c.Admin, err = pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"reader", "workspace", "importer"} {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		u.User = workbenchRuntimeRoleUser(t, role)
		cfg, err := pgx.ParseConfig(u.String())
		if err != nil {
			t.Fatal(err)
		}
		switch role {
		case "reader":
			c.ReaderDSN, c.Reader = u.String(), cfg
		case "workspace":
			c.WorkspaceDSN, c.Workspace = u.String(), cfg
		case "importer":
			c.ImporterDSN, c.Importer = u.String(), cfg
		}
	}
	return c
}

func TestResponsibilityBackupRestoreAndRejection(t *testing.T) {
	f := startResponsibilityProduction(t)
	ctx := context.Background()
	actor, command := responsibilityCommand(t, f, "xs_1111111111111111")
	zone := "America/New_York"
	due, err := time.Parse(time.RFC3339Nano, "2026-11-01T01:30:12.123456-05:00")
	if err != nil {
		t.Fatal(err)
	}
	command.Payload.DueAt, command.Payload.DueTimezone = &due, &zone
	first, err := f.store.ApplyFindingFollowup(ctx, f.caseItem.ID, f.finding.ID, actor, command, workspace.AssigneeLookup{SubjectID: "xs_3333333333333333", Name: "assignee.reader", Active: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SealCase(ctx, f.caseItem.ID, actor, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.ValidateFindingResponsibilityRestore(ctx, f.admin); err != nil {
		t.Fatalf("source chain validation: %v", err)
	}
	manager := hostctl.PostgresManager{Runner: hostctl.ExecRunner{}}
	credentials := responsibilityCredentials(t, f.dsn)
	// Supported hostctl preview and schema inspection must not mutate the
	// current Case, its atomic history or the immutable evidence fingerprint.
	before, err := f.store.GetFindingReview(ctx, f.caseItem.ID, f.finding.ID)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := manager.EvidenceFingerprint(ctx, credentials)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.PreviewCleanBaseline(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	inspection, err := hostctl.InspectCleanBaselineSchemaContractDatabase(ctx, credentials)
	if err != nil || !inspection.Valid {
		t.Fatalf("current schema inspection: %v", err)
	}
	after, err := f.store.GetFindingReview(ctx, f.caseItem.ID, f.finding.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	fingerprintAfter, err := manager.EvidenceFingerprint(ctx, credentials)
	if err != nil || fingerprint != fingerprintAfter || !bytes.Equal(beforeJSON, afterJSON) {
		t.Fatal("preview changed source responsibility or evidence")
	}
	backup, err := manager.Backup(ctx, credentials, filepath.Join(t.TempDir(), "responsibility.dump"))
	if err != nil {
		t.Fatal(err)
	}
	name := "xinsight_restore_xic_" + strings.ReplaceAll(randomTestUUID(t), "-", "")
	u, err := url.Parse(f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	restored := responsibilityCredentials(t, u.String())
	t.Cleanup(func() {
		if _, err := f.admin.Exec("DROP DATABASE IF EXISTS " + pgx.Identifier{name}.Sanitize() + " WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	expected, err := db.LoadMigrationChecksums(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RestoreToNewDatabase(ctx, credentials, restored, backup.Path, filepath.Join("..", "..", "migrations"), expected); err != nil {
		t.Fatal(err)
	}
	if err := manager.VerifyRoleBoundaries(ctx, restored); err != nil {
		t.Fatal(err)
	}
	restoredStore, err := db.OpenWorkspaceStore(ctx, restored.WorkspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer restoredStore.Close()
	replay, err := restoredStore.ApplyFindingFollowup(ctx, f.caseItem.ID, f.finding.ID, actor, command, workspace.AssigneeLookup{}, time.Now())
	firstBytes, _ := json.Marshal(first)
	replayBytes, _ := json.Marshal(replay)
	if err != nil || !bytes.Equal(firstBytes, replayBytes) {
		t.Fatalf("restored sealed retry: %v", err)
	}
	source, err := f.store.GetCase(ctx, f.caseItem.ID)
	if err != nil {
		t.Fatal(err)
	}
	target, err := restoredStore.GetCase(ctx, f.caseItem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(source.Case.SealedPack, target.Case.SealedPack) {
		t.Fatal("restore changed sealed pack bytes")
	}
	database, err := sql.Open("pgx", restored.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	// The actual validator succeeds even when the connection defaults to read-only.
	if _, err := database.Exec(`ALTER DATABASE ` + pgx.Identifier{name}.Sanitize() + ` SET default_transaction_read_only=on`); err != nil {
		t.Fatal(err)
	}
	readonly, err := sql.Open("pgx", restored.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ValidateFindingResponsibilityRestore(ctx, readonly); err != nil {
		t.Fatal(err)
	}
	readonly.Close()
	if _, err := database.Exec(`ALTER DATABASE ` + pgx.Identifier{name}.Sanitize() + ` RESET default_transaction_read_only`); err != nil {
		t.Fatal(err)
	}
	// Corrupt only this newly restored synthetic database, restoring each exact value.
	for _, pair := range [][2]string{
		{`UPDATE case_finding_followups SET assignee_name='tampered'`, `UPDATE case_finding_followups SET assignee_name='assignee.reader'`},
		{`UPDATE case_finding_actions SET request_sha256=repeat('0',64)`, `UPDATE case_finding_actions SET request_sha256=$1`},
		{`UPDATE investigation_cases SET sealed_digest='sha256:'||repeat('0',64)`, `UPDATE investigation_cases SET sealed_digest=$1`},
	} {
		var digest string
		if strings.Contains(pair[0], "request_sha256") {
			if err := database.QueryRow(`SELECT request_sha256 FROM case_finding_actions`).Scan(&digest); err != nil {
				t.Fatal(err)
			}
		}
		if strings.Contains(pair[0], "sealed_digest") {
			if err := database.QueryRow(`SELECT sealed_digest FROM investigation_cases`).Scan(&digest); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := database.Exec(pair[0]); err != nil {
			t.Fatal(err)
		}
		if err := db.ValidateFindingResponsibilityRestore(ctx, database); err == nil {
			t.Fatal("corrupt restore silently accepted")
		}
		var args []any
		if strings.Contains(pair[1], "$1") {
			args = []any{digest}
		}
		if _, err := database.Exec(pair[1], args...); err != nil {
			t.Fatal(err)
		}
		if err := db.ValidateFindingResponsibilityRestore(ctx, database); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("Native PG16 backup/verify/new-database restore passed (%d bytes); grants, exact sealed bytes, receipt replay, read-only validation and corrupted projection/request hash/pack hash rejection passed", backup.SizeBytes)
}

func TestResponsibilityActualDatabaseRolesAndAgentDenial(t *testing.T) {
	f := startResponsibilityProduction(t)
	for _, role := range []string{"workspace", "reader", "importer"} {
		c := responsibilityCredentials(t, f.dsn)
		dsn := c.WorkspaceDSN
		if role == "reader" {
			dsn = c.ReaderDSN
		}
		if role == "importer" {
			dsn = c.ImporterDSN
		}
		database, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatal(err)
		}
		statements := []string{`UPDATE case_finding_actions SET receipt=receipt WHERE false`, `DELETE FROM case_finding_actions WHERE false`, `TRUNCATE case_finding_actions`}
		if role != "workspace" {
			statements = append(statements, `INSERT INTO case_finding_actions SELECT * FROM case_finding_actions WHERE false`, `INSERT INTO case_finding_followups SELECT * FROM case_finding_followups WHERE false`, `UPDATE case_finding_followups SET version=version WHERE false`)
		}
		for _, statement := range statements {
			tx, err := database.Begin()
			if err != nil {
				t.Fatal(err)
			}
			_, err = tx.Exec(statement)
			tx.Rollback()
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || (pg.Code != "42501" && !(role == "reader" && pg.Code == "25006")) {
				t.Fatalf("role %s expected permission denial: %v", role, err)
			}
			// The reader also defaults to read-only transactions. Verify the
			// underlying ACL independently without weakening that runtime default.
			table := "case_finding_actions"
			if strings.Contains(statement, "case_finding_followups") {
				table = "case_finding_followups"
			}
			var granted bool
			if err := database.QueryRow(`SELECT has_table_privilege(current_user,$1,$2)`, table, strings.Fields(statement)[0]).Scan(&granted); err != nil || granted {
				t.Fatalf("role %s unexpectedly has table privilege: %v", role, err)
			}
		}
		database.Close()
	}
	adminStore, err := db.OpenWorkspaceStore(context.Background(), f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer adminStore.Close()
	credential, err := adminStore.CreateServiceIdentity(context.Background(), workspace.Actor{Kind: "root", Ref: "root"}, workspace.CreateServiceIdentityInput{DisplayName: "synthetic responsibility denial", ExpiresAt: time.Now().Add(time.Hour)}, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	client := f.server.Client()
	for _, path := range []string{f.reviewPath(), f.reviewPath() + "/history?through_version=0", "/api/v1/security/assignee-candidates?case_id=" + f.caseItem.ID + "&finding_id=" + f.finding.ID, f.reviewPath() + "-actions"} {
		method := "GET"
		if strings.HasSuffix(path, "-actions") {
			method = "POST"
		}
		request, err := http.NewRequest(method, f.server.URL+path, strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+credential.Token)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 403 {
			t.Fatalf("service token new responsibility route status %d", response.StatusCode)
		}
	}
	t.Log("Actual role UPDATE/DELETE/TRUNCATE denial and non-human bearer rejection on all new routes passed")
}
