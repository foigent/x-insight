package api_test

import (
	"errors"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// These inputs bind the existing P3 harness to an operator-approved disposable
// environment. They do not grant authorization or bypass its root/identity
// checks. J1 and CC14/15 retain their historical qualification identities.
const p3PortableActions = "synthetic-db-redis,http,credentials,source-binding,resource-authorization,human-review-grant,secret-cleanup"

var p3PortableKeys = []string{"P3_PORTABLE", "P3_APPROVED_ROOT", "P3_APPROVED_RUN", "P3_APPROVED_ACTIONS", "P3_XI_DATABASE_PORT", "P3_UR_DATABASE_PORT", "P3_REDIS_PORT"}

type p3ExecutionScope struct {
	root, run                 string
	xiPort, urPort, redisPort uint16
	portable                  bool
}

func p3ScopeFromEnv(get func(string) string) (p3ExecutionScope, error) {
	legacy := p3ExecutionScope{root: p3Root, xiPort: 56134, urPort: 56132, redisPort: 56379}
	present := false
	for _, key := range p3PortableKeys {
		present = present || get(key) != ""
	}
	if !present {
		return legacy, nil
	}
	deny := func() (p3ExecutionScope, error) {
		return p3ExecutionScope{}, errors.New("portable P3 requires an exact approved isolated scope and exclusive P3 mode")
	}
	if get("P3_PORTABLE") != "1" || get("XI_P3_INTEROP") != "1" || get("XINSIGHT_SCOPED_BROWSER") != "1" || get("P3_APPROVED_ACTIONS") != p3PortableActions {
		return deny()
	}
	for _, key := range []string{"XI_J1_INTEROP", "XI_CC14_INTEROP", "XI_CC15_INTEROP", "XI_NO_B_BROWSER", "UR_P3_INTEROP", "UR_J1_INTEROP", "UR_CC14_INTEROP", "UR_CC15_INTEROP", "XINSIGHT_PURPOSE_BROWSER", "XINSIGHT_A0_BROWSER_HARNESS", "XINSIGHT_A0_BROWSER_PAGING_FIXTURE", "XINSIGHT_A_MANAGED_BROWSER"} {
		if get(key) != "" {
			return deny()
		}
	}
	scope := p3ExecutionScope{root: get("P3_APPROVED_ROOT"), run: get("P3_APPROVED_RUN"), portable: true}
	if !regexp.MustCompile(`^p3-public-[a-z0-9][a-z0-9-]{0,63}$`).MatchString(scope.run) || !filepath.IsAbs(scope.root) || filepath.Clean(scope.root) != scope.root || filepath.Base(scope.root) != scope.run || len(strings.Split(strings.TrimPrefix(scope.root, "/"), "/")) < 3 {
		return deny()
	}
	ports := []*uint16{&scope.xiPort, &scope.urPort, &scope.redisPort}
	seen := map[uint64]bool{}
	for i, key := range []string{"P3_XI_DATABASE_PORT", "P3_UR_DATABASE_PORT", "P3_REDIS_PORT"} {
		value := get(key)
		n, err := strconv.ParseUint(value, 10, 16)
		if err != nil || n < 1024 || strconv.FormatUint(n, 10) != value || seen[n] {
			return deny()
		}
		seen[n] = true
		*ports[i] = uint16(n)
	}
	credentials := filepath.Join(scope.root, "credentials/xi-product-test", scope.run)
	output := filepath.Join(scope.root, "evidence", scope.run, "xi")
	if get("P3_CREDENTIAL_DIR") != credentials || get("XINSIGHT_SCOPED_BROWSER_OUTPUT") != output {
		return deny()
	}
	return scope, nil
}

func (s p3ExecutionScope) producerScopeEnv() []string {
	if !s.portable {
		return nil
	}
	return []string{
		"P3_PORTABLE=1", "P3_APPROVED_ROOT=" + s.root, "P3_APPROVED_RUN=" + s.run,
		"P3_APPROVED_ACTIONS=" + p3PortableActions,
		"P3_XI_DATABASE_PORT=" + strconv.Itoa(int(s.xiPort)),
		"P3_UR_DATABASE_PORT=" + strconv.Itoa(int(s.urPort)),
		"P3_REDIS_PORT=" + strconv.Itoa(int(s.redisPort)),
	}
}

func p3PortableDatabaseMatches(cfg *pgx.ConnConfig, port uint16, database string) bool {
	return cfg != nil && cfg.Host == "127.0.0.1" && cfg.Port == port && len(cfg.Fallbacks) == 0 && len(cfg.RuntimeParams) == 0 && cfg.Database == database && database != "" && database != "postgres" && !strings.HasPrefix(database, "template")
}

func TestP3PortableDatabaseBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, dsn, database string
		accepted            bool
	}{
		{"exact", "postgres://synthetic@127.0.0.1:58134/xi_p3_base?sslmode=disable", "xi_p3_base", true},
		{"wrong_port", "postgres://synthetic@127.0.0.1:58132/xi_p3_base?sslmode=disable", "xi_p3_base", false},
		{"remote", "postgres://synthetic@db.example.invalid:58134/xi_p3_base?sslmode=disable", "xi_p3_base", false},
		{"ambiguous_localhost", "postgres://synthetic@localhost:58134/xi_p3_base?sslmode=disable", "xi_p3_base", false},
		{"host_fallback", "postgres://synthetic@127.0.0.1:58134,127.0.0.1:58135/xi_p3_base?sslmode=disable", "xi_p3_base", false},
		{"transport_fallback", "postgres://synthetic@127.0.0.1:58134/xi_p3_base?sslmode=prefer", "xi_p3_base", false},
		{"session_override", "postgres://synthetic@127.0.0.1:58134/xi_p3_base?sslmode=disable&search_path=other", "xi_p3_base", false},
		{"different_base", "postgres://synthetic@127.0.0.1:58134/another_base?sslmode=disable", "xi_p3_base", false},
		{"missing_approved_base", "postgres://synthetic@127.0.0.1:58134/xi_p3_base?sslmode=disable", "", false},
		{"system_base", "postgres://synthetic@127.0.0.1:58134/postgres?sslmode=disable", "postgres", false},
		{"template_base", "postgres://synthetic@127.0.0.1:58134/template1?sslmode=disable", "template1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := pgx.ParseConfig(tc.dsn)
			if err != nil {
				t.Fatal("synthetic test configuration did not parse")
			}
			if p3PortableDatabaseMatches(cfg, 58134, tc.database) != tc.accepted {
				t.Fatal("database scope admission differs")
			}
		})
	}
	if p3PortableDatabaseMatches(nil, 58134, "xi_p3_base") {
		t.Fatal("missing database configuration accepted")
	}
}

func TestP3PortableScope(t *testing.T) {
	get := func(env map[string]string) func(string) string { return func(key string) string { return env[key] } }
	legacy, err := p3ScopeFromEnv(get(nil))
	if err != nil || legacy.portable || legacy.root != p3Root || legacy.xiPort != 56134 || legacy.urPort != 56132 || legacy.redisPort != 56379 || len(legacy.producerScopeEnv()) != 0 {
		t.Fatal("legacy qualification defaults changed")
	}
	run := "p3-public-contributor-20261011-r1"
	root := "/var/lib/foigent-validation/" + run
	valid := func() map[string]string {
		return map[string]string{
			"P3_PORTABLE": "1", "XI_P3_INTEROP": "1", "XINSIGHT_SCOPED_BROWSER": "1",
			"P3_APPROVED_ROOT": root, "P3_APPROVED_RUN": run, "P3_APPROVED_ACTIONS": p3PortableActions,
			"P3_XI_DATABASE_PORT": "58134", "P3_UR_DATABASE_PORT": "58132", "P3_REDIS_PORT": "58379",
			"P3_CREDENTIAL_DIR":              root + "/credentials/xi-product-test/" + run,
			"XINSIGHT_SCOPED_BROWSER_OUTPUT": root + "/evidence/" + run + "/xi",
		}
	}
	t.Run("exact_scope_and_child_forwarding", func(t *testing.T) {
		scope, err := p3ScopeFromEnv(get(valid()))
		if err != nil || !scope.portable || scope.root != root || scope.run != run || scope.xiPort != 58134 || scope.urPort != 58132 || scope.redisPort != 58379 {
			t.Fatal("valid isolated scope rejected")
		}
		forwarded := map[string]string{}
		for _, item := range scope.producerScopeEnv() {
			key, value, ok := strings.Cut(item, "=")
			if !ok || forwarded[key] != "" || value != valid()[key] {
				t.Fatal("producer scope differs from admitted scope")
			}
			forwarded[key] = value
		}
		if len(forwarded) != len(p3PortableKeys) {
			t.Fatal("incomplete producer scope")
		}
	})
	t.Run("formal_cli_keeps_exact_binary_boundary", func(t *testing.T) {
		env := valid()
		env["XI_FORMAL_ACCOUNTS"] = "1"
		env["P3_XI_CTL"] = root + "/candidate/xinsightctl"
		env["P3_XI_CTL_SHA256"] = strings.Repeat("a", 64)
		if err := p3FormalMode(get(env)); err != nil {
			t.Fatal("scoped formal CLI rejected")
		}
		for _, path := range []string{p3Root + "/candidate/xinsightctl", root + "-other/xinsightctl", root + "/../xinsightctl"} {
			env["P3_XI_CTL"] = path
			if err := p3FormalMode(get(env)); err == nil {
				t.Fatal("formal CLI escaped the approved root")
			}
		}
	})
	for _, key := range append(append([]string{}, p3PortableKeys...), "XI_P3_INTEROP", "XINSIGHT_SCOPED_BROWSER", "P3_CREDENTIAL_DIR", "XINSIGHT_SCOPED_BROWSER_OUTPUT") {
		t.Run("missing_"+key, func(t *testing.T) {
			env := valid()
			delete(env, key)
			if _, err := p3ScopeFromEnv(get(env)); err == nil {
				t.Fatal("partial scope accepted")
			}
		})
	}
	for _, tc := range []struct{ key, value string }{
		{"P3_PORTABLE", "true"}, {"P3_APPROVED_ACTIONS", "http"},
		{"P3_APPROVED_RUN", j1Run}, {"P3_APPROVED_RUN", "p3-public-../other"},
		{"P3_APPROVED_ROOT", p3Root}, {"P3_APPROVED_ROOT", root + "/"},
		{"P3_APPROVED_ROOT", "/" + run}, {"P3_APPROVED_ROOT", "relative/" + run},
		{"P3_APPROVED_ROOT", "/var/lib/../foigent-validation/" + run},
		{"P3_CREDENTIAL_DIR", root + "/credentials/xi-product-test/previous"},
		{"XINSIGHT_SCOPED_BROWSER_OUTPUT", root + "/evidence/" + run + "/ur"},
		{"P3_XI_DATABASE_PORT", "0"}, {"P3_XI_DATABASE_PORT", "543"},
		{"P3_XI_DATABASE_PORT", "058134"}, {"P3_XI_DATABASE_PORT", "65536"},
		{"P3_XI_DATABASE_PORT", "127.0.0.1:58134"}, {"P3_UR_DATABASE_PORT", "58134"},
		{"P3_REDIS_PORT", "58132"}, {"XI_J1_INTEROP", "1"}, {"XI_CC14_INTEROP", "1"},
		{"XI_CC15_INTEROP", "1"}, {"XI_NO_B_BROWSER", "1"}, {"UR_P3_INTEROP", "1"},
		{"XINSIGHT_A0_BROWSER_HARNESS", "1"}, {"XINSIGHT_PURPOSE_BROWSER", "1"},
	} {
		t.Run("reject_"+tc.key+"_"+tc.value, func(t *testing.T) {
			env := valid()
			env[tc.key] = tc.value
			if _, err := p3ScopeFromEnv(get(env)); err == nil {
				t.Fatal("unapproved or ambiguous scope accepted")
			}
		})
	}
}
