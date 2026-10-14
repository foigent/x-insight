package testpostgres

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestScopeRejectsBeforeAnyConnection(t *testing.T) {
	scope := Scope{Host: "127.0.0.1", Port: 49998, Database: "xic_queue_reg_20260910", User: "xinsight_admin", DatabaseOID: "42", SystemID: "123"}
	good := "postgres://xinsight_admin@127.0.0.1:49998/xic_queue_reg_20260910?sslmode=disable"
	for _, target := range []string{
		"postgres", "template0", "template1", "xic_cov_reg_20260910", "xinsight_clean_qualification_7eee4153b294",
	} {
		t.Run(target, func(t *testing.T) {
			raw, _ := json.Marshal(scope)
			env := map[string]string{"XINSIGHT_RESPONSIBILITY_LOCAL_TEST": "1", ScopeEnv: "scope.json", "XINSIGHT_POSTGRES_DSN": "postgres://xinsight_admin@127.0.0.1:49998/" + target + "?sslmode=disable"}
			err := check(func(k string) string { return env[k] }, func(string) ([]byte, error) { return raw, nil }, func(*pgx.ConnConfig, Scope) error { t.Fatal("rejected target reached connection"); return nil })
			if err == nil {
				t.Fatal("unsafe target accepted")
			}
		})
	}
	for _, dsn := range []string{good + "&dbname=postgres", good + "&host=example.invalid", good + "&port=5432", good + "&user=other", good + "&options=-csearch_path=other", "not a DSN"} {
		if _, err := validate(dsn, scope); err == nil {
			t.Fatal("override or invalid DSN accepted")
		}
	}
	for _, database := range []string{"", "postgres", "template0", "template1"} {
		changed := scope
		changed.Database = database
		if _, err := validate(good, changed); err == nil {
			t.Fatal("invalid scope accepted")
		}
	}
	raw, _ := json.Marshal(scope)
	env := map[string]string{"XINSIGHT_RESPONSIBILITY_LOCAL_TEST": "1", ScopeEnv: "scope.json", "XINSIGHT_POSTGRES_DSN": good}
	get := func(k string) string { return env[k] }
	read := func(string) ([]byte, error) { return raw, nil }
	called := false
	if err := check(get, read, func(c *pgx.ConnConfig, s Scope) error {
		called = true
		if c.Database != scope.Database || s != scope {
			t.Fatal("scope changed")
		}
		return nil
	}); err != nil || !called {
		t.Fatalf("recorded target refused: %v", err)
	}
	if err := check(get, read, func(*pgx.ConnConfig, Scope) error { return errors.New("identity mismatch") }); err == nil {
		t.Fatal("live identity mismatch ignored")
	}
	if err := check(get, func(string) ([]byte, error) { return nil, os.ErrNotExist }, nil); err == nil {
		t.Fatal("missing scope accepted")
	}
	if err := check(get, func(string) ([]byte, error) { return []byte("broken"), nil }, nil); err == nil {
		t.Fatal("malformed scope accepted")
	}
	env["XINSIGHT_HOSTCTL_ADMIN_DSN"] = good
	if err := check(get, read, nil); err == nil {
		t.Fatal("inherited qualification target accepted")
	}
}

func TestMainRefusesBeforeTestBody(t *testing.T) {
	// This test simulates local execution, independent of the CI role DSNs.
	t.Setenv("XINSIGHT_REQUIRE_POSTGRES_TESTS", "")
	for _, role := range []string{"ADMIN", "READER", "WORKSPACE", "IMPORTER"} {
		t.Setenv("XINSIGHT_HOSTCTL_"+role+"_DSN", "")
	}
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("XINSIGHT_RESPONSIBILITY_LOCAL_TEST", "1")
	t.Setenv("XINSIGHT_POSTGRES_DSN", "postgres://synthetic-secret@127.0.0.1:49998/postgres?sslmode=disable")
	t.Setenv(ScopeEnv, "")
	if code := Main(func() int { t.Fatal("unsafe target reached m.Run"); return 0 }); code != 2 {
		t.Fatalf("exit=%d", code)
	}
	t.Setenv("XINSIGHT_POSTGRES_DSN", "")
	if code := Main(func() int { return 7 }); code != 7 {
		t.Fatalf("offline exit=%d", code)
	}
	t.Setenv("XINSIGHT_RESPONSIBILITY_LOCAL_TEST", "")
	if code := Main(func() int { return 8 }); code != 8 {
		t.Fatalf("normal exit=%d", code)
	}
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("XINSIGHT_RESPONSIBILITY_LOCAL_TEST", "1")
	if code := Main(func() int { return 9 }); code != 9 {
		t.Fatalf("existing CI exit=%d", code)
	}
}

func TestRequiredDatabaseRejectsBeforeTestBody(t *testing.T) {
	t.Setenv("XINSIGHT_REQUIRE_POSTGRES_TESTS", "1")
	t.Setenv("XINSIGHT_POSTGRES_DSN", "")
	t.Setenv(ScopeEnv, "")
	for _, ci := range []string{"", "true"} {
		t.Run("github_actions="+ci, func(t *testing.T) {
			t.Setenv("GITHUB_ACTIONS", ci)
			if code := Main(func() int { t.Error("mandatory database tests reached m.Run without a database"); return 0 }); code != 2 {
				t.Fatalf("exit=%d, want refusal", code)
			}
		})
	}
	// The existing CI service remains valid; this guard must not create a
	// second connection or redefine its established isolated target contract.
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("XINSIGHT_POSTGRES_DSN", "postgres://ci-fixture@example.invalid/test")
	if code := Main(func() int { return 7 }); code != 7 {
		t.Fatalf("configured CI exit=%d", code)
	}
}

func TestLiveRecordedIdentity(t *testing.T) {
	if os.Getenv(ScopeEnv) == "" {
		t.Skip("recorded local target required")
	}
	if err := check(os.Getenv, os.ReadFile, probe); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(os.Getenv(ScopeEnv))
	if err != nil {
		t.Fatal(err)
	}
	var scope Scope
	if err = json.Unmarshal(raw, &scope); err != nil {
		t.Fatal(err)
	}
	c, err := validate(os.Getenv("XINSIGHT_POSTGRES_DSN"), scope)
	if err != nil {
		t.Fatal(err)
	}
	scope.DatabaseOID = "0"
	if err = probe(c, scope); err == nil {
		t.Fatal("live changed database identity accepted")
	}
}
