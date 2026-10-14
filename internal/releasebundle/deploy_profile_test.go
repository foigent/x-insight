package releasebundle_test

import (
	"os"
	"strings"
	"testing"
)

func TestPostgresComposeMatchesCanonicalSingleNodeTarget(t *testing.T) {
	contents, err := os.ReadFile("../../deploy/postgres.compose.yml")
	if err != nil {
		t.Fatalf("read PostgreSQL Compose profile: %v", err)
	}
	profile := string(contents)
	for _, required := range []string{
		"container_name: xinsight-postgres",
		"PGDATA: /var/lib/postgresql/data",
		"source: /data/x-insight/postgres",
		"target: /var/lib/postgresql/data",
		`"127.0.0.1:5432:5432"`,
	} {
		if !strings.Contains(profile, required) {
			t.Fatalf("PostgreSQL Compose profile is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"/var/lib/postgresql/data/pgdata",
		"source: /var/lib/x-insight/postgres",
		`"0.0.0.0:5432:5432"`,
	} {
		if strings.Contains(profile, forbidden) {
			t.Fatalf("PostgreSQL Compose profile contains unsafe value %q", forbidden)
		}
	}
}
