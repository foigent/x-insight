package reviewbody

import (
	"bytes"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestDeadlineAADPostgresRoundTrip(t *testing.T) {
	dsn := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set XINSIGHT_POSTGRES_DSN for PostgreSQL round trip")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	connection, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(t.Context(), `CREATE TEMP TABLE reviewbody_deadline_test(expires_at timestamptz NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	identity := Identity{SourceKind: "work_event", SourceSystemID: "11111111-1111-4111-8111-111111111111", SourceInstance: "a",
		OrgID: "1", ProjectID: "2", EventRef: "event", EventRevision: 1, EventHash: "sha256:test",
		ContentKind: "user_input", ContentRef: "body", ByteDomain: "openclaw.hook.utf8.v1",
		PolicyVersion: "1:0", ExpiresAt: time.Date(2026, 10, 1, 0, 0, 0, 123456789, time.UTC)}
	frozen := FreezeIdentity(identity)
	ring := KeyRing{ActiveID: "synthetic", Keys: map[string][]byte{"synthetic": bytes.Repeat([]byte{5}, 32)}}
	sealed, err := ring.Seal(frozen, []byte{}, bytes.NewReader(bytes.Repeat([]byte{2}, 12)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.ExecContext(t.Context(), `INSERT INTO reviewbody_deadline_test VALUES($1)`, frozen.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	var fromDB time.Time
	if err := connection.QueryRowContext(t.Context(), `SELECT expires_at FROM reviewbody_deadline_test`).Scan(&fromDB); err != nil {
		t.Fatal(err)
	}
	if !fromDB.Equal(frozen.ExpiresAt) {
		t.Fatalf("deadline changed through PostgreSQL: %s -> %s", frozen.ExpiresAt, fromDB)
	}
	identity.ExpiresAt = fromDB
	if _, err := ring.Open(identity, sealed); err != nil {
		t.Fatalf("cannot decrypt after PostgreSQL round trip: %v", err)
	}
	identity.ExpiresAt = fromDB.Add(time.Microsecond)
	if _, err := ring.Open(identity, sealed); err != ErrIntegrity {
		t.Fatalf("changed persisted deadline accepted: %v", err)
	}
}
