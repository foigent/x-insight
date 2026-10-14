package db_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/jackc/pgx/v5"
)

// The same test binary prepares roles once in the disposable seed cluster.
// Subsequent physical clones retain those exact role definitions and grants.
func TestWorkbenchCheckpointPrepareRoles(t *testing.T) {
	if os.Getenv("XINSIGHT_A0_CHECKPOINT_MODE") != "roles" {
		t.Skip("checkpoint experiment only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c := hostctl.DatabaseCredentials{AdminDSN: os.Getenv("XINSIGHT_POSTGRES_DSN")}
	var err error
	c.Admin, err = pgx.ParseConfig(c.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"reader", "workspace", "importer"} {
		u, err := url.Parse(c.AdminDSN)
		if err != nil {
			t.Fatal(err)
		}
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			t.Fatal(err)
		}
		u.User = url.UserPassword("xinsight_"+role, hex.EncodeToString(b))
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
	m := hostctl.PostgresManager{}
	for _, fn := range []func(context.Context, hostctl.DatabaseCredentials) error{m.EnsureRoles, m.ApplyGrants, m.VerifyRoleBoundaries} {
		if err := fn(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("Actual hostctl role preparation and boundary verification passed; synthetic passwords remained in memory")
}
