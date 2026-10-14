package db

import (
	"database/sql"
	"net/url"
	"os"
	"testing"
)

func TestWorkspaceOwnedPoolJITIsolation(t *testing.T) {
	dsn := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("PG required")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid synthetic database address")
	}
	query := u.Query()
	query.Set("jit", "on")
	u.RawQuery = query.Encode()
	external, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal("cannot open synthetic external pool")
	}
	defer external.Close()
	wrapped := NewWorkspaceStore(external)
	if wrapped.db != external {
		t.Fatal("external pool was replaced")
	}
	type settings struct{ Role, Superuser, Timeout, ReadOnly, JIT string }
	read := func(conn *sql.Conn) (int, settings) {
		t.Helper()
		var pid int
		var got settings
		if err := conn.QueryRowContext(t.Context(), `SELECT pg_backend_pid(),current_user,current_setting('is_superuser'),current_setting('statement_timeout'),current_setting('default_transaction_read_only'),current_setting('jit')`).Scan(&pid, &got.Role, &got.Superuser, &got.Timeout, &got.ReadOnly, &got.JIT); err != nil {
			t.Fatal(err)
		}
		return pid, got
	}
	control, err := external.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	_, baseline := read(control)
	if baseline.JIT != "on" {
		t.Fatal("external pool setting changed", baseline)
	}
	owned, err := OpenWorkspaceStore(t.Context(), u.String())
	if err != nil {
		t.Fatal("cannot open synthetic owned pool")
	}
	defer owned.Close()
	first, err := owned.db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := owned.db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	firstPID, firstSettings := read(first)
	secondPID, secondSettings := read(second)
	expected := baseline
	expected.JIT = "off"
	if firstPID == secondPID || firstSettings != expected || secondSettings != expected {
		t.Fatalf("owned physical connections: distinct=%t first=%+v second=%+v expected=%+v", firstPID != secondPID, firstSettings, secondSettings, expected)
	}
	if _, after := read(control); after != baseline {
		t.Fatal("owned pool changed external connection settings")
	}
	t.Log("two distinct owned connections disable JIT; identity, statement timeout and read-only settings unchanged; supplied pool remains JIT-on")
}
