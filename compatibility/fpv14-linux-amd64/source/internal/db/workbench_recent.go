package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/foigent/x-insight/internal/workchain"
)

// GetWorkbenchRecentRequests pages only detail-authorized members in one
// economic reading. A runtime reading is optional and independently validated.
func (s *WorkspaceStore) GetWorkbenchRecentRequests(ctx context.Context, subject, id, runtimeID string, limit int, cursor string, now time.Time) (workbench.RecentRequests, error) {
	result := workbench.RecentRequests{Version: workbench.RecentRequestsVersion, ScopeBasis: "economic_reading_members", DetailScope: "currently_authorized_details_only", ReadingID: id, Items: []workbench.RecentRequest{}}
	reading, err := s.loadValidWorkbenchReading(ctx, subject, id, now)
	if err != nil {
		return result, err
	}
	if limit < 1 || limit > workbench.MaxPageSize {
		return result, workbench.ErrInvalid
	}
	result.MembershipRef, result.Cutoff = reading.MembershipRef, reading.Cutoff
	if runtimeID != "" {
		runtimeReading, e := s.GetRuntimeReading(ctx, subject, runtimeID, now)
		if e != nil {
			return result, e
		}
		var economicFilters, runtimeFilters []byte
		if e = s.db.QueryRowContext(ctx, `SELECT filters FROM workbench_readings WHERE id=$1 AND owner_subject=$2`, id, subject).Scan(&economicFilters); e != nil {
			return result, e
		}
		if e = s.db.QueryRowContext(ctx, `SELECT filters FROM runtime_readings WHERE id=$1 AND owner_subject=$2`, runtimeID, subject).Scan(&runtimeFilters); e != nil {
			return result, e
		}
		var a, b workbench.Filters
		if e = json.Unmarshal(economicFilters, &a); e != nil {
			return result, e
		}
		if e = json.Unmarshal(runtimeFilters, &b); e != nil {
			return result, e
		}
		if !reading.Period.StartInclusive.Equal(runtimeReading.Period.StartInclusive) || !reading.Period.EndExclusive.Equal(runtimeReading.Period.EndExclusive) || !sameRecentFilters(a, b) {
			return result, workbench.ErrInvalid
		}
		result.RuntimeReadingID, result.RuntimeMembershipRef = runtimeID, runtimeReading.MembershipRef
		cutoff := runtimeReading.Cutoff
		result.RuntimeCutoff = &cutoff
	}
	ref := "overview.recent:" + runtimeID
	after := int64(0)
	if cursor != "" {
		var ok bool
		after, ok = parseDetailCursor(cursor, id, ref)
		if !ok || after == 0 {
			return result, workbench.ErrInvalid
		}
	}
	purposeState := `'unavailable'`
	if reading.Purpose != nil && reading.Purpose.Status == "available" {
		purposeState = `COALESCE(p.status,'unknown')`
	}
	pageBoundary := ""
	queryArgs := []any{id, subject, limit + 1}
	if after > 0 {
		pageBoundary = " AND m.ordinal<$4"
		queryArgs = append(queryArgs, after)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.ordinal,f.id::text,f.request_id,f.execution_id,f.evidence_event_id::text,f.source_system_id::text,
	 COALESCE(f.model_ref,''),COALESCE(f.source_project_ref,''),COALESCE(f.cost_project_ref,''),f.occurred_at,
	 f.gateway_charged_micros::text,f.calculated_reference_micros::text,`+purposeState+`,COALESCE(p.category,'')
	 FROM workbench_reading_members m JOIN workbench_economic_facts f ON f.id=m.economic_fact_id
	 LEFT JOIN workbench_purpose_members p ON p.reading_id=m.reading_id AND p.economic_fact_id=f.id
	 WHERE m.reading_id=$1`+pageBoundary+`
	 AND EXISTS(SELECT 1 FROM workbench_access_grants g WHERE g.subject_id=$2 AND g.active AND g.allow_details
	 AND g.source_system_id=f.source_system_id AND (g.source_project_ref IS NULL OR g.source_project_ref=f.source_project_ref)
	 AND (g.cost_project_ref IS NULL OR g.cost_project_ref=f.cost_project_ref))
	 ORDER BY m.ordinal DESC LIMIT $3`, queryArgs...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	ordinals := make([]int64, 0, limit+1)
	factIDs := make([]string, 0, limit+1)
	for rows.Next() {
		var item workbench.RecentRequest
		var ordinal int64
		var factID string
		if err = rows.Scan(&ordinal, &factID, &item.RequestID, &item.ExecutionID, &item.EvidenceEventID, &item.SourceSystemID, &item.Model, &item.SourceProject, &item.CostProject, &item.OccurredAt, &item.GatewayChargedMicros, &item.CalculatedReferenceMicros, &item.PurposeState, &item.PurposeCategory); err != nil {
			return result, err
		}
		item.AccountState, item.ApplicationState = "unknown", "unknown"
		item.OccurredAt = item.OccurredAt.UTC()
		item.Runtime.MatchState = "unavailable"
		result.Items = append(result.Items, item)
		ordinals = append(ordinals, ordinal)
		factIDs = append(factIDs, factID)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if err = rows.Close(); err != nil {
		return result, err
	}
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		factIDs = factIDs[:limit]
		ordinals = ordinals[:limit]
		result.NextCursor = detailCursor(id, ref, ordinals[len(ordinals)-1])
	}
	if reading.SourceManifestRef != "" {
		details := make([]workbench.DetailRow, len(result.Items))
		for i := range result.Items {
			details[i].EconomicFactID = factIDs[i]
			details[i].AccountState, details[i].ApplicationState = "unknown", "unknown"
		}
		if err = s.addWorkbenchDetailSources(ctx, subject, id, details); err != nil {
			return result, err
		}
		for i := range details {
			result.Items[i].AccountState = details[i].AccountState
			result.Items[i].ApplicationState = details[i].ApplicationState
			for _, fact := range details[i].SourceFacts {
				if fact.ChainState == "complete" && fact.Profile == sourcefacts.ContextProfile {
					result.Items[i].ApplicationID = sourcefacts.Text(fact.Context, "workload_application_id")
				}
			}
		}
	}
	if workchain.FromContext(ctx) != nil {
		rows.Close()
		for i := range result.Items {
			_, scopeErr := scopedExecutionRequirement(ctx, s.db, result.Items[i].ExecutionID)
			available := scopeErr == nil
			if scopeErr != nil && !errors.Is(scopeErr, workchain.ErrDenied) {
				return result, scopeErr
			}
			result.Items[i].InvestigationAvailable = &available
		}
	}
	if runtimeID == "" || len(result.Items) == 0 {
		return result, nil
	}
	for i := range result.Items {
		result.Items[i].Runtime.MatchState = "not_in_runtime_reading"
	}
	matches, err := s.db.QueryContext(ctx, `SELECT rem.economic_fact_id::text,q.request_ref,q.entry_at,q.observation_count
	 FROM runtime_reading_economic_members rem JOIN runtime_reading_requests q ON q.reading_id=rem.reading_id AND q.request_ref=rem.request_ref
	 WHERE rem.reading_id=$1 AND rem.economic_fact_id=ANY($2::uuid[]) ORDER BY rem.economic_fact_id,q.request_ref`, runtimeID, pgTextArray(factIDs))
	if err != nil {
		return result, err
	}
	index := make(map[string]int, len(factIDs))
	for i, factID := range factIDs {
		index[factID] = i
	}
	refs := make([]string, 0, len(factIDs))
	for matches.Next() {
		var factID, requestRef string
		var entry sql.NullTime
		var count int64
		if err = matches.Scan(&factID, &requestRef, &entry, &count); err != nil {
			matches.Close()
			return result, err
		}
		i, ok := index[factID]
		if !ok {
			continue
		}
		item := &result.Items[i].Runtime
		if item.MatchState == "matched" || item.MatchState == "no_request_observation" {
			item.MatchState = "ambiguous"
			item.RequestRef = ""
			item.EntryAt = nil
			item.Outcome = ""
			continue
		}
		if item.MatchState == "ambiguous" {
			continue
		}
		item.MatchState = "matched"
		item.RequestRef = requestRef
		if entry.Valid {
			at := entry.Time.UTC()
			item.EntryAt = &at
		}
		if count == 0 {
			item.MatchState = "no_request_observation"
		} else {
			refs = append(refs, requestRef)
		}
	}
	if err = matches.Err(); err != nil {
		matches.Close()
		return result, err
	}
	matches.Close()
	if len(refs) == 0 {
		return result, nil
	}
	// Reuse exactly the aggregate's source/chain predicates, restricted to the
	// page's frozen request refs. Never infer a request result from attempts.
	signals, err := s.db.QueryContext(ctx, runtimeRequestSignalsSQL, runtimeID, "all", pgTextArray(refs))
	if err != nil {
		return result, err
	}
	outcomes := map[string]string{}
	for signals.Next() {
		var requestRef string
		var s workbench.RuntimeRequestSignals
		var requestObservation bool
		if err = signals.Scan(&requestRef, &s.Completed, &s.WriteFailed, &s.TerminalProviderRejected, &s.AdmissionRejected, &s.OutputHeld, &s.Interrupted, &s.ContextDone, &s.MissingPredecessor, &requestObservation); err != nil {
			signals.Close()
			return result, err
		}
		if requestObservation {
			outcomes[requestRef] = workbench.ClassifyRuntimeRequest(s)
		}
	}
	if err = signals.Err(); err != nil {
		signals.Close()
		return result, err
	}
	signals.Close()
	for i := range result.Items {
		r := &result.Items[i].Runtime
		if r.MatchState != "matched" {
			continue
		}
		if outcome, ok := outcomes[r.RequestRef]; ok {
			r.Outcome = outcome
		} else {
			r.MatchState = "no_request_observation"
		}
	}
	return result, nil
}

func sameRecentFilters(a, b workbench.Filters) bool {
	match := func(x, y []string) bool {
		if len(x) != len(y) {
			return false
		}
		x = append([]string(nil), x...)
		y = append([]string(nil), y...)
		sort.Strings(x)
		sort.Strings(y)
		for i := range x {
			if x[i] != y[i] {
				return false
			}
		}
		return true
	}
	return match(a.SourceIDs, b.SourceIDs) && match(a.SourceProjects, b.SourceProjects) && match(a.CostProjects, b.CostProjects)
}
