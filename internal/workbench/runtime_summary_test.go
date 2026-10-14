package workbench

import "testing"

func TestRuntimeRequestOutcomeDoesNotConflateContextDeliveryOrConflicts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		signals RuntimeRequestSignals
		want    string
	}{
		{"no evidence", RuntimeRequestSignals{}, "unresolved"},
		{"context only", RuntimeRequestSignals{ContextDone: true}, "context_done_unresolved"},
		{"completed then context done", RuntimeRequestSignals{Completed: true, ContextDone: true}, "local_delivery_completed"},
		{"two failure signals one outcome", RuntimeRequestSignals{WriteFailed: true, TerminalProviderRejected: true}, "technical_failure"},
		{"rejected is not technical failure", RuntimeRequestSignals{AdmissionRejected: true}, "admission_rejected"},
		{"held output", RuntimeRequestSignals{OutputHeld: true}, "output_held"},
		{"interrupted", RuntimeRequestSignals{Interrupted: true, ContextDone: true}, "interrupted"},
		{"conflicting terminal facts", RuntimeRequestSignals{Completed: true, WriteFailed: true}, "conflicting"},
		{"incomplete corrected evidence", RuntimeRequestSignals{Completed: true, MissingPredecessor: true}, "missing_predecessor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyRuntimeRequest(tc.signals); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
