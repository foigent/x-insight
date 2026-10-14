package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func TestWorkbenchContributionDetailsShortCircuit(t *testing.T) {
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dsn := performanceTestDSN(t, ctx, base)
	if _, err := RunMigrations(ctx, dsn, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	cfg.RuntimeParams["jit"] = "off"
	trace := &readModeTrace{}
	cfg.Tracer = trace
	pool := stdlib.OpenDB(*cfg)
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(5)
	pool.SetConnMaxLifetime(30 * time.Minute)
	pool.SetConnMaxIdleTime(5 * time.Minute)
	store := NewWorkspaceStore(pool)
	defer store.Close()
	evidence, err := OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer evidence.Close()
	exec := func(t *testing.T, query string, args ...any) {
		t.Helper()
		if _, err := pool.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	source := performanceTestUUID(t)
	subject := "xs_1122334455667788"
	other := "xs_8877665544332211"
	raw, err := os.ReadFile("../../contracts/completed-execution/v1/fixtures/valid-settled.json")
	if err != nil {
		t.Fatal(err)
	}
	for i, charged := range []string{"17", "9999999999999999999", "0"} {
		var v map[string]any
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		v["request_id"] = fmt.Sprintf("req-details-short-circuit-%d", i)
		v["project_id"] = float64(202 + i)
		model := "shared"
		if i == 2 {
			model = "other"
		}
		v["usage"].(map[string]any)["model"] = model
		v["money"].(map[string]any)["charged_cost_micros"] = charged
		v["money"].(map[string]any)["calculated_cost_micros"] = []string{"34", "2", "5"}[i]
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		mapped, err := unirouter.MapCompletedExecutionV1(b, unirouter.CompletedExecutionMapOptions{
			SourceInstance: "xi-details-short-circuit", SourceSystemID: source,
			AdapterManifestID: unirouter.DefaultCompletedExecutionAdapterManifestID,
			ObservedAt:        time.Now().UTC(), IngestedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := evidence.PersistEvents(ctx, mapped.Events); err != nil {
			t.Fatal(err)
		}
	}
	exec(t, `INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary) VALUES($1,$2::uuid,true)`, subject, source)
	// Neither an inactive grant nor another subject's active grant permits details.
	exec(t, `INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_details,active) VALUES($1,$2::uuid,'project-202',true,false)`, subject, source)
	exec(t, `INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details) VALUES($1,$2::uuid,true,true)`, other, source)
	now := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	request := explicitPerformanceRequest(time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC), now, []string{source}, nil)
	create := func(t *testing.T) workbench.Reading {
		t.Helper()
		r, err := store.CreateWorkbenchReading(ctx, subject, request, now)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	check := func(t *testing.T, r workbench.Reading, sharedAllowed, otherAllowed bool) {
		t.Helper()
		result, err := store.GetWorkbenchContributions(ctx, subject, r.ID, "model", 0, 10, "", now)
		if err != nil {
			t.Fatal(err)
		}
		wantAmount := "10000000000000000016"
		if r.MoneyBasis == workbench.MoneyCalculatedReference {
			wantAmount = "41"
		}
		if len(result.Rows) != 2 || result.DenominatorRequests != 3 || result.DenominatorMicros != wantAmount || result.AuthorizedGroupCount != 2 || result.UnassignedMicros != "0" || result.OtherKnownMicros != "0" || result.UnassignedRequests != 0 || result.OtherKnownRequests != 0 {
			t.Fatalf("grant duplicated or removed members: %+v", result)
		}
		for _, row := range result.Rows {
			allowed := sharedAllowed
			count, charged, calculated := int64(2), "10000000000000000016", "36"
			if row.Label == "other" {
				allowed = otherAllowed
				count = 1
				charged = "0"
				calculated = "5"
			} else if row.Label != "shared" {
				t.Fatal("unexpected model", row.Label)
			}
			if row.DrilldownAllowed != allowed || row.RequestCount != count || row.GatewayChargedMicros != charged || row.CalculatedReferenceMicros != calculated || row.Share.Denominator != wantAmount {
				t.Fatalf("group semantics changed: %+v", row)
			}
			details, err := store.GetWorkbenchDetails(ctx, subject, r.ID, row.Ref, 10, "", now)
			if allowed {
				if err != nil || int64(len(details.Rows)) != count {
					t.Fatalf("allowed details %+v %v", details, err)
				}
			} else if !errors.Is(err, workbench.ErrScopeForbidden) {
				t.Fatalf("denied details: %v", err)
			}
		}
	}
	var reading workbench.Reading
	if !t.Run("no_details_actual_plan", func(t *testing.T) {
		reading = create(t)
		before := len(trace.snapshot())
		check(t, reading, false, false)
		// Explain the actual groups SQL and arguments captured from the Store call.
		// Reuse the existing native EXPLAIN helper; no copied product query or module.
		var query string
		var args []any
		for _, entry := range trace.snapshot()[before:] {
			sql, _ := entry["sql"].(string)
			if strings.Contains(sql, "bool_and(CASE WHEN EXISTS") {
				query = sql
				args = entry["arguments"].([]any)
				break
			}
		}
		if query == "" {
			t.Fatal("actual groups statement not captured")
		}
		plan := explainJSON(t, ctx, pool, query, args...)
		t.Logf("details short-circuit actual plan=%s", plan)
		var documents []struct{ Plan map[string]any }
		if err := json.Unmarshal(plan, &documents); err != nil || len(documents) != 1 {
			t.Fatalf("plan decode: %v", err)
		}
		initChecks, correlatedChecks := 0, 0
		var visit func(map[string]any)
		visit = func(node map[string]any) {
			if node["Relation Name"] == "workbench_access_grants" {
				switch node["Alias"] {
				case "detail_grant":
					initChecks++
					if node["Actual Loops"] != float64(1) || node["Actual Rows"] != float64(0) {
						t.Fatalf("no-details guard was not checked once: %v", node)
					}
				case "g":
					correlatedChecks++
					if node["Actual Loops"] != float64(0) {
						t.Fatalf("per-member details scan executed: %v", node)
					}
				}
			}
			if children, ok := node["Plans"].([]any); ok {
				for _, child := range children {
					visit(child.(map[string]any))
				}
			}
		}
		visit(documents[0].Plan)
		if initChecks != 1 || correlatedChecks != 1 {
			t.Fatalf("missing actual guard/short-circuited subplan: %d/%d", initChecks, correlatedChecks)
		}
	}) {
		return
	}
	if !t.Run("mixed_scope_same_group", func(t *testing.T) {
		exec(t, `UPDATE workbench_access_grants SET active=true,revision=revision+1 WHERE subject_id=$1 AND source_project_ref='project-202'`, subject)
		reading = create(t)
		check(t, reading, false, false)
	}) {
		return
	}
	if !t.Run("different_grants_cover_each_member", func(t *testing.T) {
		exec(t, `INSERT INTO workbench_access_grants(subject_id,source_system_id,source_project_ref,allow_details) VALUES($1,$2::uuid,'project-203',true)`, subject, source)
		reading = create(t)
		check(t, reading, true, false)
	}) {
		return
	}
	if !t.Run("overlapping_grants_do_not_multiply", func(t *testing.T) {
		exec(t, `UPDATE workbench_access_grants SET allow_details=true,revision=revision+1 WHERE subject_id=$1 AND source_project_ref IS NULL`, subject)
		reading = create(t)
		check(t, reading, true, true)
		request.MoneyBasis = workbench.MoneyCalculatedReference
		check(t, create(t), true, true)
		request.MoneyBasis = workbench.MoneyGatewayCharged
	}) {
		return
	}
	t.Run("current_revocation_rejects_old_reading", func(t *testing.T) {
		exec(t, `UPDATE workbench_access_grants SET active=false,revision=revision+1 WHERE subject_id=$1 AND source_project_ref IS NOT NULL`, subject)
		exec(t, `UPDATE workbench_access_grants SET allow_details=false,revision=revision+1 WHERE subject_id=$1 AND source_project_ref IS NULL`, subject)
		if _, err := store.GetWorkbenchContributions(ctx, subject, reading.ID, "model", 0, 10, "", now); !errors.Is(err, workbench.ErrAccessChanged) {
			t.Fatalf("revoked reading served: %v", err)
		}
		check(t, create(t), false, false)
	})
}
