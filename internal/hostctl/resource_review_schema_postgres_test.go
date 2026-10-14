package hostctl

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/db"
)

func TestResourceReviewSchema38RoleMigration(t *testing.T) {
	for _, predecessor := range []int{36, 37, 38} {
		t.Run(string(rune('0'+predecessor/10))+string(rune('0'+predecessor%10)), func(t *testing.T) {
			ctx, c := connectorDigestDatabase(t)
			m := PostgresManager{Runner: ExecRunner{}}
			if err := m.EnsureRoles(ctx, c); err != nil {
				t.Fatal(err)
			}
			if _, err := db.RunMigrations(ctx, c.AdminDSN, connectorDigestMigrations(t, predecessor)); err != nil {
				t.Fatal(err)
			}
			if n, err := db.RunMigrations(ctx, c.AdminDSN, "../../migrations"); err != nil || n != currentMigrationSchema(t)-predecessor {
				t.Fatal("upgrade", n, err)
			}
			if err := m.ApplyGrants(ctx, c); err != nil {
				t.Fatal(err)
			}
			if err := m.VerifyRoleBoundaries(ctx, c); err != nil {
				t.Fatal(err)
			}
			admin := managedSQL(t, c.AdminDSN)
			manifest, err := GenerateCleanBaselineSchemaContract(ctx, admin)
			if err != nil {
				t.Fatal(err)
			}
			if manifest.DatabaseSchema != buildinfo.DatabaseSchemaMax || manifest.MigrationCount != currentMigrationSchema(t) {
				t.Fatal("current schema manifest")
			}
			if output := os.Getenv("XINSIGHT_RESOURCE_SCHEMA_OUTPUT"); output != "" && predecessor == 38 {
				raw, err := json.MarshalIndent(manifest, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(output, append(raw, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
