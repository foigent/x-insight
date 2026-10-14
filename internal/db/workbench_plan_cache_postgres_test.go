package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

type planCacheQuery struct {
	Name      string            `json:"name"`
	SQL       string            `json:"sql"`
	Hash      string            `json:"sql_sha256"`
	Types     []string          `json:"expected_parameter_types_to_verify_at_runtime"`
	Arguments []json.RawMessage `json:"arguments_by_position_1_based"`
}

type planCachePrepared struct {
	Name      string        `json:"name"`
	Statement string        `json:"statement"`
	Prepared  string        `json:"prepare_time"`
	FromSQL   bool          `json:"from_sql"`
	OIDs      []json.Number `json:"oids"`
	Generic   int64         `json:"generic_plans"`
	Custom    int64         `json:"custom_plans"`
}

type planCacheState struct {
	PID        int                 `json:"pid"`
	Mode       string              `json:"mode"`
	Readonly   string              `json:"readonly"`
	Statements []planCachePrepared `json:"statements"`
}

func (spec planCacheQuery) args(round int) ([]any, error) {
	args := make([]any, len(spec.Arguments))
	for i, raw := range spec.Arguments {
		switch spec.Types[i] {
		case "text[]":
			var a []string
			if err := json.Unmarshal(raw, &a); err != nil {
				return nil, err
			}
			args[i] = pgTextArray(a)
		case "timestamptz":
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return nil, err
			}
			v, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return nil, err
			}
			args[i] = v
		case "text":
			if len(raw) > 0 && raw[0] == '{' {
				var v struct {
					Sequence   []string `json:"auto_sequence"`
					Comparison string   `json:"comparison"`
				}
				if err := json.Unmarshal(raw, &v); err != nil {
					return nil, err
				}
				if round < 0 {
					args[i] = v.Comparison
				} else if round < len(v.Sequence) {
					args[i] = v.Sequence[round]
				} else {
					return nil, fmt.Errorf("missing reading for round %d", round)
				}
			} else {
				var v string
				if err := json.Unmarshal(raw, &v); err != nil {
					return nil, err
				}
				args[i] = v
			}
		default:
			return nil, fmt.Errorf("unreviewed parameter type %q", spec.Types[i])
		}
	}
	return args, nil
}

func (spec planCacheQuery) readingIndex() (int, error) {
	index := -1
	for i, raw := range spec.Arguments {
		if len(raw) > 0 && raw[0] == '{' {
			if index >= 0 || spec.Types[i] != "text" {
				return -1, fmt.Errorf("ambiguous reading parameter in %s", spec.Name)
			}
			index = i
		}
	}
	if index < 0 {
		return -1, fmt.Errorf("reading parameter missing in %s", spec.Name)
	}
	return index, nil
}

func planCacheValidateCohortInputs(queries, reference []planCacheQuery) error {
	for _, q := range queries {
		var original *planCacheQuery
		for i := range reference {
			if reference[i].Name == q.Name {
				original = &reference[i]
			}
		}
		if original == nil || q.SQL != original.SQL || q.Hash != original.Hash || !reflect.DeepEqual(q.Types, original.Types) {
			return fmt.Errorf("cohort SQL/type reference differs: %s", q.Name)
		}
		index, err := q.readingIndex()
		if err != nil {
			return err
		}
		originalIndex, err := original.readingIndex()
		if err != nil || index != originalIndex {
			return fmt.Errorf("reading position differs: %s", q.Name)
		}
		want, err := original.args(-1)
		if err != nil {
			return err
		}
		want[index] = planCacheCohortIDs[0]
		for _, round := range []int{0, -1} {
			got, err := q.args(round)
			if err != nil || !reflect.DeepEqual(got, want) {
				return fmt.Errorf("cohort non-reading arguments or MCV input differ: %s", q.Name)
			}
		}
	}
	return nil
}

func planCacheSameJSON(value any, expected json.RawMessage) bool {
	encoded, err := json.Marshal(value)
	if err != nil {
		return false
	}
	var got, want any
	return json.Unmarshal(encoded, &got) == nil && json.Unmarshal(expected, &want) == nil && reflect.DeepEqual(got, want)
}

func planCacheCoverageBaseline(raw []byte, args []any) (json.RawMessage, error) {
	var old struct {
		Samples []struct {
			Query     string          `json:"query"`
			Phase     string          `json:"phase"`
			Arguments json.RawMessage `json:"arguments"`
			Result    json.RawMessage `json:"result"`
			Success   bool            `json:"success"`
			Checked   bool            `json:"result_check"`
		} `json:"samples"`
	}
	if err := json.Unmarshal(raw, &old); err != nil {
		return nil, err
	}
	var result json.RawMessage
	for _, s := range old.Samples {
		if s.Query != "coverage" || s.Phase != "auto" || !planCacheSameJSON(args, s.Arguments) {
			continue
		}
		if !s.Success || !s.Checked || result != nil {
			return nil, fmt.Errorf("ambiguous or invalid coverage baseline")
		}
		var rows struct {
			Columns []string `json:"columns"`
			Rows    [][]any  `json:"rows"`
		}
		if err := json.Unmarshal(s.Result, &rows); err != nil {
			return nil, err
		}
		if len(rows.Columns) != 9 || len(rows.Rows) != 1 || len(rows.Rows[0]) != 9 || rows.Rows[0][0] != float64(1) {
			return nil, fmt.Errorf("coverage baseline must have one authorized source")
		}
		result = s.Result
	}
	if result == nil {
		return nil, fmt.Errorf("matching coverage baseline missing")
	}
	return result, nil
}

func TestWorkbenchPlanCacheCoverageSemanticResult(t *testing.T) {
	expected := json.RawMessage(`{"columns":["count","watermark","connected"],"rows":[[1,"fixed",false]]}`)
	if !planCacheSameJSON(map[string]any{"columns": []string{"count", "watermark", "connected"}, "rows": [][]any{{int64(1), "fixed", false}}}, expected) {
		t.Fatal("equal SQL result rejected")
	}
	for _, row := range [][]any{{int64(0), "fixed", false}, {int64(1), "changed", false}, {int64(1), "fixed", true}} {
		if planCacheSameJSON(map[string]any{"columns": []string{"count", "watermark", "connected"}, "rows": [][]any{row}}, expected) {
			t.Fatal("changed coverage result accepted")
		}
	}
}

func TestWorkbenchPlanCacheCohortArgumentContract(t *testing.T) {
	reading, _ := json.Marshal(map[string]any{"auto_sequence": []string{planCacheCohortIDs[0]}, "comparison": planCacheCohortIDs[0]})
	ref := planCacheQuery{Name: "coverage", SQL: "coverage", Hash: "same", Types: []string{"text", "text", "text[]"}, Arguments: []json.RawMessage{json.RawMessage(`"subject"`), reading, json.RawMessage(`["source"]`)}}
	if err := planCacheValidateCohortInputs([]planCacheQuery{ref}, []planCacheQuery{ref}); err != nil {
		t.Fatal(err)
	}
	for _, position := range []int{0, 2} {
		bad := ref
		bad.Arguments = append([]json.RawMessage(nil), ref.Arguments...)
		if position == 0 {
			bad.Arguments[0] = reading
		} else {
			bad.Arguments[2] = json.RawMessage(`[]`)
		}
		if planCacheValidateCohortInputs([]planCacheQuery{bad}, []planCacheQuery{ref}) == nil {
			t.Fatal("changed subject/filter accepted")
		}
	}
	index, err := ref.readingIndex()
	if err != nil || index != 1 {
		t.Fatal("coverage reading must be parameter 2")
	}
}

func TestWorkbenchPlanCacheCohortInputFiles(t *testing.T) {
	path := os.Getenv("XINSIGHT_A0_PLAN_VALIDATE_INPUT")
	if path == "" {
		t.Skip("explicit offline cohort input validation only")
	}
	var inputs [2]struct {
		Queries []planCacheQuery `json:"queries"`
	}
	for i, p := range []string{path, os.Getenv("XINSIGHT_A0_PLAN_REFERENCE")} {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(raw, &inputs[i]); err != nil {
			t.Fatal(err)
		}
	}
	if err := planCacheValidateCohortInputs(inputs[0].Queries, inputs[1].Queries); err != nil {
		t.Fatal(err)
	}
	if baseline := os.Getenv("XINSIGHT_A0_COVERAGE_BASELINE"); baseline != "" {
		raw, err := os.ReadFile(baseline)
		if err != nil {
			t.Fatal(err)
		}
		for _, q := range inputs[0].Queries {
			if q.Name == "coverage" {
				args, err := q.args(0)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = planCacheCoverageBaseline(raw, args); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

func planCacheInspect(t *testing.T, ctx context.Context, q workspaceQueryer, spec planCacheQuery, pid int) (json.RawMessage, planCacheState) {
	t.Helper()
	raw := planCacheJSON(t, ctx, q, `SELECT jsonb_build_object('pid',pg_backend_pid(),'mode',current_setting('plan_cache_mode'),'readonly',current_setting('transaction_read_only'),'statements',COALESCE((SELECT jsonb_agg(jsonb_build_object('name',name,'statement',statement,'parameter_types',parameter_types::text[],'oids',parameter_types::oid[],'from_sql',from_sql,'prepare_time',prepare_time,'generic_plans',generic_plans,'custom_plans',custom_plans)) FROM pg_prepared_statements WHERE statement=$1),'[]'::jsonb))`, spec.SQL)
	var state planCacheState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	if state.PID != pid {
		t.Fatalf("pinned connection changed PID %d to %d", pid, state.PID)
	}
	for _, p := range state.Statements {
		if p.Statement != spec.SQL || p.FromSQL || p.Prepared == "" {
			t.Fatal("prepared SQL differs")
		}
		want := make([]json.Number, len(spec.Types))
		for i, v := range spec.Types {
			want[i] = json.Number(fmt.Sprint(map[string]int{"text": 25, "text[]": 1009, "timestamptz": 1184}[v]))
		}
		if !reflect.DeepEqual(p.OIDs, want) {
			t.Fatalf("parameter OIDs %v != %v", p.OIDs, want)
		}
	}
	return raw, state
}

func TestWorkbenchPlanCachePreparedOIDs(t *testing.T) {
	// PostgreSQL jsonb serializes oid[] elements as strings on this fixture.
	// Accept that representation and numeric JSON without weakening OID checks.
	for _, raw := range []string{`{"oids":["25","1184","1009"]}`, `{"oids":[25,1184,1009]}`} {
		var prepared planCachePrepared
		if err := json.Unmarshal([]byte(raw), &prepared); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(prepared.OIDs, []json.Number{"25", "1184", "1009"}) {
			t.Fatalf("parameter OIDs changed: %v", prepared.OIDs)
		}
	}
}

var planCacheCohortIDs = []string{"xwr_vzD1s7lPgy1RE3-Trd5V5Q", "xwr_XMf3SdgKSIV7KJ_kxrItBg"}

func planCacheCheckCohorts(raw json.RawMessage) error {
	var stats []struct {
		Table  string          `json:"tablename"`
		Column string          `json:"attname"`
		Values json.RawMessage `json:"most_common_vals"`
	}
	if err := json.Unmarshal(raw, &stats); err != nil {
		return err
	}
	for _, s := range stats {
		if s.Table != "workbench_reading_members" || s.Column != "reading_id" {
			continue
		}
		var values []string
		if err := json.Unmarshal(s.Values, &values); err != nil {
			return err
		}
		found := [2]bool{}
		for _, id := range values {
			for i, want := range planCacheCohortIDs {
				found[i] = found[i] || id == want
			}
		}
		if found != [2]bool{true, false} {
			return fmt.Errorf("MCV/non-MCV classification changed: %v", found)
		}
		return nil
	}
	return fmt.Errorf("reading_id statistics missing")
}

func TestWorkbenchPlanCacheCohortPreconditions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []string
		valid  bool
	}{
		{"approved", []string{planCacheCohortIDs[0]}, true},
		{"non_mcv_became_common", planCacheCohortIDs, false},
		{"mcv_disappeared", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal([]any{
				map[string]any{"tablename": "workbench_access_grants", "attname": "allow_summary", "most_common_vals": []bool{true}},
				map[string]any{"tablename": "workbench_economic_facts", "attname": "gateway_charged_micros", "most_common_vals": []int{0, 100}},
				map[string]any{"tablename": "workbench_reading_members", "attname": "reading_id", "most_common_vals": tc.values},
			})
			if (planCacheCheckCohorts(raw) == nil) != tc.valid {
				t.Fatal("invalid classification acceptance")
			}
		})
	}
	if planCacheCheckCohorts(json.RawMessage(`[]`)) == nil {
		t.Fatal("missing statistics accepted")
	}
	if planCacheCheckCohorts(json.RawMessage(`[{"tablename":"workbench_reading_members","attname":"reading_id","most_common_vals":[true]}]`)) == nil {
		t.Fatal("invalid reading_id value type accepted")
	}
}

// The existing snapshot helper accepts no arguments; this parameterized variant
// keeps session-local inspection on the exact connection executing the target.
func planCacheJSON(t *testing.T, ctx context.Context, q workspaceQueryer, query string, args ...any) json.RawMessage {
	t.Helper()
	var raw []byte
	if err := q.QueryRowContext(ctx, query, args...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func planCacheRows(ctx context.Context, q workspaceQueryer, query string, args []any) (map[string]any, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	data := [][]any{}
	for rows.Next() {
		values := make([]any, len(cols))
		dest := make([]any, len(cols))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		for i, v := range values {
			if b, ok := v.([]byte); ok {
				values[i] = string(b)
			}
		}
		data = append(data, values)
	}
	return map[string]any{"columns": cols, "rows": data}, rows.Err()
}

func planCacheStats(t *testing.T, ctx context.Context, q workspaceQueryer) json.RawMessage {
	return planCacheJSON(t, ctx, q, `SELECT jsonb_build_object('at',clock_timestamp(),'tables',(SELECT jsonb_agg(jsonb_build_object('table',s.relname,'reltuples',c.reltuples,'relpages',c.relpages,'last_analyze',s.last_analyze,'last_autoanalyze',s.last_autoanalyze,'last_autovacuum',s.last_autovacuum,'n_mod_since_analyze',s.n_mod_since_analyze,'n_live_tup',s.n_live_tup,'n_dead_tup',s.n_dead_tup)) FROM pg_stat_user_tables s JOIN pg_class c ON c.oid=s.relid WHERE s.relname=ANY(ARRAY['workbench_reading_members','workbench_economic_facts','evidence_events','workbench_access_grants','source_systems','adapter_import_states','capture_gaps'])))`)
}

func planCacheResource(t *testing.T, ctx context.Context) json.RawMessage {
	t.Helper()
	cmd := exec.CommandContext(ctx, "python3", os.Getenv("XINSIGHT_A0_PLAN_RUNNER"), "--resource-check", os.Getenv("XINSIGHT_A0_PLAN_RESOURCES"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("resource check stopped target queries: %v %s", err, out)
	}
	if !json.Valid(out) {
		t.Fatalf("invalid resource observation: %s", out)
	}
	return json.RawMessage(out)
}

func TestWorkbenchPlanCacheDiagnostic(t *testing.T) {
	if os.Getenv("XINSIGHT_A0_PLAN_CACHE") != "1" {
		t.Skip("explicit local plan-cache diagnostic only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	cohort := os.Getenv("XINSIGHT_A0_PLAN_COHORT") == "1"
	coverageOnly := os.Getenv("XINSIGHT_A0_COVERAGE_ONLY") == "1"
	if coverageOnly {
		cohort = true
	}
	wantQueries, autoRounds, targetLimit := 3, 7, 36
	cohortLimitBytes := int64(4192 * 1024 * 1024)
	if cohort {
		wantQueries, autoRounds, targetLimit = 2, 1, 18
	}
	if coverageOnly {
		wantQueries, autoRounds, targetLimit = 1, 1, 9
		cohortLimitBytes = 4256 * 1024 * 1024
	}
	out := os.Getenv("XINSIGHT_A0_PLAN_OUTPUT")
	report := map[string]any{"scope": "Read-only prepared-plan diagnosis; no INSERT/HTTP/A0 qualification", "started_at": time.Now().UTC(), "samples": []map[string]any{}, "complete": false}
	report["cohort_comparison"], report["target_limit"] = cohort, targetLimit
	report["coverage_only"] = coverageOnly
	defer func() { report["finished_at"] = time.Now().UTC(); checkpointWriteJSON(t, out, report) }()
	var input struct {
		Queries []planCacheQuery `json:"queries"`
	}
	b, err := os.ReadFile(os.Getenv("XINSIGHT_A0_PLAN_QUERIES"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &input); err != nil {
		t.Fatal(err)
	}
	if len(input.Queries) != wantQueries {
		t.Fatal("unexpected reviewed query count")
	}
	if cohort && !coverageOnly && (input.Queries[0].Name != "membership_digest" || input.Queries[1].Name != "coverage") {
		t.Fatal("cohort query scope differs")
	}
	if coverageOnly && input.Queries[0].Name != "coverage" {
		t.Fatal("coverage-only query scope differs")
	}
	if cohort {
		raw, e := os.ReadFile(os.Getenv("XINSIGHT_A0_PLAN_REFERENCE"))
		if e != nil {
			t.Fatal(e)
		}
		var ref struct {
			Queries []planCacheQuery `json:"queries"`
		}
		if e = json.Unmarshal(raw, &ref); e != nil {
			t.Fatal(e)
		}
		if e = planCacheValidateCohortInputs(input.Queries, ref.Queries); e != nil {
			t.Fatal(e)
		}
	}
	var coverageBaseline json.RawMessage
	if coverageOnly {
		args, e := input.Queries[0].args(0)
		if e != nil {
			t.Fatal(e)
		}
		path := os.Getenv("XINSIGHT_A0_COVERAGE_BASELINE")
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		coverageBaseline, e = planCacheCoverageBaseline(raw, args)
		if e != nil {
			t.Fatal(e)
		}
		report["coverage_semantic_baseline"] = map[string]any{"source": path, "arguments": args, "result": coverageBaseline}
	}
	source, err := os.ReadFile("workbench_store.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range input.Queries {
		h := sha256.Sum256([]byte(q.SQL))
		if hex.EncodeToString(h[:]) != q.Hash || !strings.Contains(string(source), q.SQL) {
			t.Fatal("query does not match current source")
		}
	}
	report["queries"] = input.Queries
	u, err := url.Parse(os.Getenv("XINSIGHT_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.User("xinsight_workspace")
	store, err := OpenWorkspaceStore(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.db.SetMaxOpenConns(1)
	store.db.SetMaxIdleConns(1)
	conn, err := store.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var pid int
	if err = conn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if err = conn.Raw(func(dc any) error {
		pg := dc.(*stdlib.Conn).Conn()
		c := pg.Config()
		report["driver"] = map[string]any{"pid": pg.PgConn().PID(), "statement_cache_capacity": c.StatementCacheCapacity, "mode": c.DefaultQueryExecMode.String()}
		if c.StatementCacheCapacity != 512 || c.DefaultQueryExecMode != pgx.QueryExecModeCacheStatement {
			return fmt.Errorf("unexpected driver mode")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	report["identity"] = planCacheJSON(t, ctx, conn, `SELECT jsonb_build_object('pid',pg_backend_pid(),'session_user',session_user,'current_user',current_user,'superuser',(SELECT rolsuper FROM pg_roles WHERE rolname=current_user),'statement_timeout',current_setting('statement_timeout'),'plan_cache_mode',current_setting('plan_cache_mode'),'search_path',current_setting('search_path'),'facts_update',has_table_privilege(current_user,'workbench_economic_facts','UPDATE'),'members_insert',has_table_privilege(current_user,'workbench_reading_members','INSERT'))`)
	var role struct {
		Session string `json:"session_user"`
		User    string `json:"current_user"`
		Super   bool   `json:"superuser"`
		Timeout string `json:"statement_timeout"`
		Mode    string `json:"plan_cache_mode"`
		Facts   bool   `json:"facts_update"`
	}
	if err = json.Unmarshal(report["identity"].(json.RawMessage), &role); err != nil {
		t.Fatal(err)
	}
	if role.Session != "xinsight_workspace" || role.User != role.Session || role.Super || role.Timeout != "10s" || role.Mode != "auto" || role.Facts {
		t.Fatal("unexpected workspace identity/settings")
	}
	report["settings"] = planCacheJSON(t, ctx, conn, `SELECT jsonb_object_agg(name,jsonb_build_object('setting',setting,'unit',unit,'source',source)) FROM pg_settings WHERE name=ANY(ARRAY['server_version','shared_buffers','work_mem','wal_buffers','max_wal_size','fsync','full_page_writes','synchronous_commit','autovacuum','plan_cache_mode','track_io_timing','track_wal_io_timing','max_parallel_workers_per_gather','temp_file_limit','temp_tablespaces'])`)
	var settings map[string]struct {
		Setting string `json:"setting"`
	}
	if err = json.Unmarshal(report["settings"].(json.RawMessage), &settings); err != nil {
		t.Fatal(err)
	}
	for k, w := range map[string]string{"shared_buffers": "16384", "work_mem": "4096", "wal_buffers": "512", "max_wal_size": "1024", "fsync": "on", "full_page_writes": "on", "synchronous_commit": "on", "autovacuum": "on", "plan_cache_mode": "auto", "track_io_timing": "on", "track_wal_io_timing": "on", "max_parallel_workers_per_gather": "2", "temp_file_limit": "-1", "temp_tablespaces": ""} {
		if settings[k].Setting != w {
			t.Fatalf("unexpected %s=%s", k, settings[k].Setting)
		}
	}
	report["baseline_resources"] = planCacheResource(t, ctx)
	base, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer base.Rollback()
	var headers, members int64
	if err = base.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM workbench_readings),(SELECT count(*) FROM workbench_reading_members)`).Scan(&headers, &members); err != nil {
		base.Rollback()
		t.Fatal(err)
	}
	report["baseline_counts"] = map[string]int64{"headers": headers, "members": members}
	if headers != 19 || members != 5700000 {
		base.Rollback()
		t.Fatal("late-state baseline differs")
	}
	ids := append([]string(nil), planCacheCohortIDs...)
	if !cohort {
		ids = make([]string, 7)
		for i := range ids {
			a, e := input.Queries[1].args(i)
			if e != nil {
				t.Fatal(e)
			}
			ids[i] = a[0].(string)
		}
	} else {
		for _, q := range input.Queries {
			a, e := q.args(0)
			index, indexErr := q.readingIndex()
			if e != nil || indexErr != nil || a[index] != ids[0] {
				t.Fatal("cohort preparation input differs")
			}
		}
	}
	var old struct {
		Samples []struct {
			Success bool `json:"success"`
			Reading struct {
				ID       string         `json:"reading_id"`
				Ref      string         `json:"membership_ref"`
				Coverage map[string]any `json:"coverage"`
			} `json:"reading"`
		} `json:"samples"`
	}
	b, err = os.ReadFile(os.Getenv("XINSIGHT_A0_PLAN_ORIGINAL"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &old); err != nil {
		t.Fatal(err)
	}
	wants := map[string]string{}
	for _, s := range old.Samples {
		if s.Success {
			wants[s.Reading.ID] = s.Reading.Ref
		}
	}
	verified := []map[string]any{}
	for _, id := range ids {
		var n int64
		var ref string
		if err = base.QueryRowContext(ctx, `SELECT r.membership_sha256,(SELECT count(*) FROM workbench_reading_members m WHERE m.reading_id=r.id) FROM workbench_readings r WHERE r.id=$1`, id).Scan(&ref, &n); err != nil {
			base.Rollback()
			t.Fatal(err)
		}
		verified = append(verified, map[string]any{"id": id, "members": n, "membership_ref": ref})
		report["baseline_readings"] = verified
		if n != 300000 || ref == "" || ref != wants[id] || cohort && ref != wants[ids[0]] {
			base.Rollback()
			t.Fatal("reading baseline differs")
		}
	}
	report["baseline_readings"] = verified
	base.Rollback()
	report["before_stats"] = planCacheStats(t, ctx, conn)
	report["pg_stats"] = planCacheJSON(t, ctx, conn, `SELECT COALESCE(jsonb_agg(to_jsonb(s)),'[]'::jsonb) FROM pg_stats s WHERE schemaname='public' AND tablename=ANY(ARRAY['workbench_reading_members','workbench_economic_facts','evidence_events','workbench_access_grants','source_systems','adapter_import_states','capture_gaps'])`)
	if cohort {
		if err := planCacheCheckCohorts(report["pg_stats"].(json.RawMessage)); err != nil {
			t.Fatal(err)
		}
		report["cohort_ids"] = ids
	}
	samples := []map[string]any{}
	roundStats := []map[string]any{}
	disabled := map[string]bool{}
	var firstCoverage any
	persist := func() {
		report["samples"] = samples
		report["target_executions"] = len(samples)
		report["round_stats"] = roundStats
		checkpointWriteJSON(t, out, report)
	}
	checkCohorts := func() json.RawMessage {
		if !cohort {
			return nil
		}
		raw := planCacheJSON(t, ctx, conn, `SELECT COALESCE(jsonb_agg(to_jsonb(s)),'[]'::jsonb) FROM pg_stats s WHERE schemaname='public' AND tablename='workbench_reading_members' AND attname='reading_id'`)
		if err := planCacheCheckCohorts(raw); err != nil {
			report["invalid_cohort_state"] = raw
			persist()
			t.Fatal(err)
		}
		return raw
	}
	for round := 0; round < autoRounds; round++ {
		var tx *sql.Tx
		for _, spec := range input.Queries {
			if disabled[spec.Name] {
				continue
			}
			// End each cohort preparation transaction before inspecting through conn.
			if cohort && tx != nil {
				tx.Rollback()
				tx = nil
			}
			classification := checkCohorts()
			res := planCacheResource(t, ctx)
			if cohort && len(samples) == 0 {
				report["first_target_resources"] = res
				var budget struct {
					New  int64 `json:"new_bytes"`
					Free int64 `json:"free_bytes"`
				}
				if err := json.Unmarshal(res, &budget); err != nil {
					t.Fatal(err)
				}
				if cohortLimitBytes-budget.New < 512*1024*1024 || budget.Free < 23085449216 {
					persist()
					t.Fatal("first cohort target reserve not met")
				}
			}
			if tx == nil {
				tx, err = conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
			}
			args, e := spec.args(round)
			if e != nil {
				t.Fatal(e)
			}
			before, state := planCacheInspect(t, ctx, tx, spec, pid)
			if state.Readonly != "on" || state.Mode != "auto" {
				t.Fatal("target is not readonly/auto")
			}
			row := map[string]any{"phase": "auto", "round": round + 1, "query": spec.Name, "arguments": args, "resources_before": res, "prepared_before": before, "started_at": time.Now().UTC()}
			if cohort {
				row["cohort"], row["cohort_stats_before"] = "mcv", classification
			}
			samples = append(samples, row)
			persist()
			start := time.Now()
			result, queryErr := planCacheRows(ctx, tx, spec.SQL, args)
			row["duration_ns"], row["ended_at"], row["success"], row["result"] = time.Since(start).Nanoseconds(), time.Now().UTC(), queryErr == nil, result
			var inspector workspaceQueryer = tx
			if queryErr != nil {
				row["error"] = queryErr.Error()
				var pe *pgconn.PgError
				if errors.As(queryErr, &pe) {
					row["sqlstate"] = pe.Code
				}
				tx.Rollback()
				tx = nil
				inspector = conn
				disabled[spec.Name] = true
				t.Errorf("%s auto round %d failed: %v", spec.Name, round+1, queryErr)
			}
			after, _ := planCacheInspect(t, ctx, inspector, spec, pid)
			row["prepared_after"] = after
			if queryErr == nil {
				data := result["rows"].([][]any)
				valid := len(data) == 1
				if valid {
					switch spec.Name {
					case "eligible_count":
						valid = fmt.Sprint(data[0][0]) == "300000"
					case "membership_digest":
						valid = data[0][0] == wants[args[0].(string)]
					case "coverage":
						if coverageOnly {
							valid = planCacheSameJSON(result, coverageBaseline)
							row["semantic_baseline_check"] = valid
						} else {
							if firstCoverage == nil {
								firstCoverage = data
							}
							valid = reflect.DeepEqual(data, firstCoverage)
						}
					}
				}
				row["result_check"] = valid
				if !valid {
					persist()
					t.Fatal("target result mismatch")
				}
			}
			persist()
		}
		if tx != nil {
			tx.Rollback()
		}
		roundStats = append(roundStats, map[string]any{"phase": "auto", "round": round + 1, "stats": planCacheStats(t, ctx, conn)})
		persist()
	}
	for _, spec := range input.Queries {
		if disabled[spec.Name] {
			continue
		}
		args, e := spec.args(-1)
		if e != nil {
			t.Fatal(e)
		}
		modes := []string{"auto", "force_generic_plan", "force_custom_plan", "force_custom_plan", "force_generic_plan"}
		cohorts := []int{0, 1, 0, 1, 1, 0, 1, 0}
		if cohort {
			modes = []string{"force_generic_plan", "force_generic_plan", "force_custom_plan", "force_custom_plan", "force_custom_plan", "force_custom_plan", "force_generic_plan", "force_generic_plan"}
		}
		for i, mode := range modes {
			classification := checkCohorts()
			if cohort {
				index, e := spec.readingIndex()
				if e != nil {
					t.Fatal(e)
				}
				args[index] = ids[cohorts[i]]
			}
			res := planCacheResource(t, ctx)
			tx, e := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback()
			if _, e = tx.ExecContext(ctx, "SET LOCAL plan_cache_mode="+mode); e != nil {
				tx.Rollback()
				t.Fatal(e)
			}
			before, state := planCacheInspect(t, ctx, tx, spec, pid)
			if len(state.Statements) != 1 || state.Readonly != "on" || state.Mode != mode {
				tx.Rollback()
				t.Fatal("prepared identity/mode lost before EXPLAIN")
			}
			var name string
			if e = tx.QueryRowContext(ctx, `SELECT quote_ident($1::text)`, state.Statements[0].Name).Scan(&name); e != nil {
				tx.Rollback()
				t.Fatal(e)
			}
			literals := make([]string, len(args))
			for j, arg := range args {
				var quoted string
				if e = tx.QueryRowContext(ctx, "SELECT quote_literal($1::"+spec.Types[j]+")", arg).Scan(&quoted); e != nil {
					tx.Rollback()
					t.Fatal(e)
				}
				literals[j] = quoted + "::" + spec.Types[j]
			}
			query := "EXPLAIN (ANALYZE, BUFFERS, SETTINGS, FORMAT JSON) EXECUTE " + name + "(" + strings.Join(literals, ",") + ")"
			row := map[string]any{"phase": "explain", "comparison_index": i, "query": spec.Name, "mode": mode, "arguments": args, "explain_sql": query, "resources_before": res, "prepared_before": before, "stats_before": planCacheStats(t, ctx, tx), "started_at": time.Now().UTC()}
			if cohort {
				row["cohort"], row["cohort_stats_before"] = []string{"mcv", "non_mcv"}[cohorts[i]], classification
				// Each persisted row owns its bound arguments; later cohorts must not mutate it.
				row["arguments"] = append([]any(nil), args...)
			}
			samples = append(samples, row)
			persist()
			start := time.Now()
			var plan []byte
			e = tx.QueryRowContext(ctx, query).Scan(&plan)
			row["duration_ns"], row["ended_at"], row["success"] = time.Since(start).Nanoseconds(), time.Now().UTC(), e == nil
			if e == nil {
				row["plan"] = json.RawMessage(plan)
			} else {
				row["error"] = e.Error()
				var pe *pgconn.PgError
				if errors.As(e, &pe) {
					row["sqlstate"] = pe.Code
				}
			}
			var after json.RawMessage
			if e == nil {
				var next planCacheState
				after, next = planCacheInspect(t, ctx, tx, spec, pid)
				if cohort {
					old := state.Statements[0]
					valid := len(next.Statements) == 1
					if valid {
						n := next.Statements[0]
						g, c := int64(0), int64(1)
						if mode == "force_generic_plan" {
							g, c = 1, 0
						}
						valid = n.Name == old.Name && n.Prepared == old.Prepared && n.Generic-old.Generic == g && n.Custom-old.Custom == c
					}
					row["prepared_identity_counter_check"] = valid
					if !valid {
						row["prepared_after"] = after
						persist()
						t.Fatal("prepared identity/counter changed")
					}
				}
				row["stats_after"] = planCacheStats(t, ctx, tx)
			}
			tx.Rollback()
			if e != nil {
				after, _ = planCacheInspect(t, ctx, conn, spec, pid)
			}
			row["prepared_after"] = after
			var restored string
			if err = conn.QueryRowContext(ctx, "SHOW plan_cache_mode").Scan(&restored); err != nil {
				t.Fatal(err)
			}
			row["restored_mode"] = restored
			if cohort {
				row["cohort_stats_after"] = checkCohorts()
			}
			if restored != "auto" {
				t.Fatal("SET LOCAL did not restore auto")
			}
			persist()
			if e != nil {
				disabled[spec.Name] = true
				t.Errorf("%s %s EXPLAIN failed: %v", spec.Name, mode, e)
				break
			}
		}
	}
	report["after_stats"] = planCacheStats(t, ctx, conn)
	report["disabled_queries"] = disabled
	report["target_executions"] = len(samples)
	report["complete"] = len(samples) == targetLimit && len(disabled) == 0
	persist()
	if len(samples) > targetLimit {
		t.Fatal("too many target executions")
	}
}
