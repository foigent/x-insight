package workbench

const RuntimeSummaryVersion = "xinsight.runtime.request-outcomes.v1"

// These signals come only from authorized request-level runtime observations
// in the frozen reading. One failed provider attempt is not a failed request.
type RuntimeRequestSignals struct {
	Completed, WriteFailed, TerminalProviderRejected bool
	AdmissionRejected, OutputHeld, Interrupted       bool
	ContextDone, MissingPredecessor                  bool
}

func ClassifyRuntimeRequest(s RuntimeRequestSignals) string {
	if s.MissingPredecessor {
		return "missing_predecessor"
	}
	flags := []struct {
		set     bool
		outcome string
	}{
		{s.Completed, "local_delivery_completed"},
		{s.WriteFailed || s.TerminalProviderRejected, "technical_failure"},
		{s.AdmissionRejected, "admission_rejected"},
		{s.OutputHeld, "output_held"},
		{s.Interrupted, "interrupted"},
	}
	outcome := ""
	for _, f := range flags {
		if f.set {
			if outcome != "" {
				return "conflicting"
			}
			outcome = f.outcome
		}
	}
	if outcome != "" {
		return outcome
	}
	if s.ContextDone {
		return "context_done_unresolved"
	}
	return "unresolved"
}

type RuntimeOutcomeCount struct {
	Outcome  string `json:"outcome"`
	Requests int64  `json:"requests"`
}

type RuntimeDiagnosticFailure struct {
	Numerator        int64  `json:"numerator"`
	Denominator      int64  `json:"denominator"`
	ExcludedRequests int64  `json:"excluded_requests"`
	State            string `json:"state"`
	Basis            string `json:"basis"`
}

type RuntimeSummary struct {
	ReadingID              string                   `json:"reading_id"`
	SummaryVersion         string                   `json:"summary_version"`
	InterpretationRevision string                   `json:"interpretation_revision"`
	EntryTime              string                   `json:"entry_time"`
	Requests               int64                    `json:"requests"`
	Outcomes               []RuntimeOutcomeCount    `json:"outcomes"`
	DiagnosticFailure      RuntimeDiagnosticFailure `json:"diagnostic_failure"`
	Coverage               RuntimeCoverage          `json:"coverage"`
}
