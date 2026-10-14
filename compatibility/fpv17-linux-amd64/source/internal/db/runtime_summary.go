package db

import (
	"context"
	"sort"
	"time"

	"github.com/foigent/x-insight/internal/workbench"
)

// Membership and request identity were already scoped and frozen by the
// reading. Do not derive a request outcome from an attempt, lease inventory,
// economic status, an incomplete revision or an ungranted runtime profile.
const runtimeRequestSignalsSQL = `SELECT q.request_ref,
 COALESCE(bool_or(d.source_json#>>'{runtime,local_delivery}'='completed') FILTER(WHERE m.chain_state='complete'),false),
 COALESCE(bool_or(d.source_json#>>'{runtime,local_delivery}'='write_failed') FILTER(WHERE m.chain_state='complete'),false),
 COALESCE(bool_or(d.observation_kind='request_terminal' AND d.source_json#>>'{runtime,provider}'='rejected') FILTER(WHERE m.chain_state='complete'),false),
 COALESCE(bool_or(d.source_json#>>'{runtime,admission}'='rejected') FILTER(WHERE m.chain_state='complete'),false),
 COALESCE(bool_or(d.observation_kind='output_held') FILTER(WHERE m.chain_state='complete'),false),
 COALESCE(bool_or(d.source_json#>>'{runtime,local_delivery}'='interrupted') FILTER(WHERE m.chain_state='complete'),false),
 COALESCE(bool_or(d.observation_kind='request_context_done') FILTER(WHERE m.chain_state='complete'),false),
 COALESCE(bool_or(m.chain_state<>'complete' AND d.event_id IS NOT NULL),false),
 COALESCE(bool_or(d.event_id IS NOT NULL),false)
 FROM runtime_reading_requests q
 LEFT JOIN runtime_reading_members m ON m.reading_id=q.reading_id AND m.request_ref=q.request_ref
 LEFT JOIN source_observation_documents d ON d.source_system_id=m.source_system_id AND d.event_id=m.event_id
  AND d.profile='unirouter.runtime-observation.v1' AND d.subject_kind='request' AND d.source_json ? 'runtime'
 WHERE q.reading_id=$1 AND q.observation_count>0 AND ($2='all' OR ($2='known' AND q.entry_at IS NOT NULL) OR ($2='unknown' AND q.entry_at IS NULL))
 AND (cardinality($3::text[])=0 OR q.request_ref=ANY($3::text[]))
 GROUP BY q.request_ref ORDER BY q.request_ref`

func (s *WorkspaceStore) GetRuntimeSummary(ctx context.Context, subject, id, entry string, now time.Time) (workbench.RuntimeSummary, error) {
	result := workbench.RuntimeSummary{ReadingID: id, SummaryVersion: workbench.RuntimeSummaryVersion, EntryTime: entry, Outcomes: []workbench.RuntimeOutcomeCount{}}
	r, err := s.GetRuntimeReading(ctx, subject, id, now)
	if err != nil {
		return result, err
	}
	if entry != "all" && entry != "known" && entry != "unknown" {
		return result, workbench.ErrInvalid
	}
	result.InterpretationRevision, result.Coverage = r.InterpretationRevision, r.Coverage
	rows, err := s.db.QueryContext(ctx, runtimeRequestSignalsSQL, id, entry, pgTextArray(nil))
	if err != nil {
		return result, err
	}
	defer rows.Close()
	counts := map[string]int64{}
	for rows.Next() {
		var ref string
		var signals workbench.RuntimeRequestSignals
		var requestObservation bool
		if err := rows.Scan(&ref, &signals.Completed, &signals.WriteFailed, &signals.TerminalProviderRejected, &signals.AdmissionRejected, &signals.OutputHeld, &signals.Interrupted, &signals.ContextDone, &signals.MissingPredecessor, &requestObservation); err != nil {
			return result, err
		}
		counts[workbench.ClassifyRuntimeRequest(signals)]++
		result.Requests++
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	for outcome, count := range counts {
		result.Outcomes = append(result.Outcomes, workbench.RuntimeOutcomeCount{Outcome: outcome, Requests: count})
	}
	sort.Slice(result.Outcomes, func(i, j int) bool { return result.Outcomes[i].Outcome < result.Outcomes[j].Outcome })
	failure := &result.DiagnosticFailure
	failure.Numerator = counts["technical_failure"]
	failure.Denominator = failure.Numerator + counts["local_delivery_completed"]
	failure.ExcludedRequests = result.Requests - failure.Denominator
	failure.State = "not_computable"
	if failure.Denominator > 0 {
		failure.State = "computed"
	}
	failure.Basis = "authorized_request_level_local_delivery_completed_or_explicit_technical_failure;not_global_failure_rate_or_slo"
	return result, nil
}
