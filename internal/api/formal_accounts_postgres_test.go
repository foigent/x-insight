package api_test

import (
	"net/url"
	"os"
	"testing"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/db"
)

func TestFormalAccountAuthorityBeforeGrantAndDurableProviderPostgres(t *testing.T) {
	server, admin, _, source, _ := startWorkbenchProductionFixture(t, true)
	server.Close()
	var name string
	if err := admin.QueryRow(`SELECT current_database()`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(os.Getenv("XINSIGHT_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	adminDSN := u.String()
	u.User = workbenchRuntimeRoleUser(t, "workspace")
	workspaceDSN := u.String()
	store, err := db.OpenSourceFactsStore(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project, cost, revision, body := "project-1", "*", int64(0), false
	change := db.WorkbenchGrantChange{
		SchemaVersion: "xinsight.workbench-grant-change.v2", Action: "create", ID: randomTestUUID(t),
		Subject: "xs_8899aabbccddeeff", Source: source, SourceProject: &project, CostProject: &cost,
		ExpectedRevision: &revision, Capabilities: []string{"details", "coverage"}, AllowContent: &body,
		AllowedContentPurposes: []string{},
	}
	if _, err := store.ChangeManagedWorkbenchGrant(t.Context(), change, true); err == nil {
		t.Fatal("grant check accepted an uninitialized account authority")
	}
	const password = "synthetic formal account password"
	hash, err := access.DerivePasswordHash([]byte(password))
	if err != nil {
		t.Fatal(err)
	}
	config := access.AccountConfig{SchemaVersion: access.AccountConfigSchemaV3, Accounts: []access.AccountConfigEntry{
		{SubjectID: change.Subject, Username: "formal.reviewer", Role: access.RoleScopedReviewer, PasswordHash: hash},
		{SubjectID: "xs_6666666666666666", Username: "formal.reader", Role: access.RoleScopedReader, PasswordHash: hash},
	}}
	provider := prepareFormalAccountAuthority(t, adminDSN, workspaceDSN, config)
	if _, ok := provider.Authenticate("formal.reviewer", []byte(password)); !ok {
		t.Fatal("native-imported account did not authenticate through workspace authority")
	}
	result, err := store.ChangeManagedWorkbenchGrant(t.Context(), change, true)
	if err != nil || !result.CheckOnly || result.Changed {
		t.Fatalf("initialized native grant check failed: %v", err)
	}
	if _, err := admin.Exec(`UPDATE xinsight_accounts.accounts SET disabled=true WHERE subject_id=$1`, change.Subject); err != nil {
		t.Fatal(err)
	}
	if _, ok := provider.Authenticate("formal.reviewer", []byte(password)); ok {
		t.Fatal("durable provider fell back to the unchanged legacy account input")
	}
	if _, err := store.ChangeManagedWorkbenchGrant(t.Context(), change, true); err == nil {
		t.Fatal("native grant accepted a now-disabled imported account")
	}
}
