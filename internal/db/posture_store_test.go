package db

import "testing"

func TestPostureCoverageKeepsDenominatorAndUnavailableStateExplicit(t *testing.T) {
	available := postureCoverage("outcome", "Outcome", 3, 4, "executions", "basis", "limitation")
	if !available.Available || available.RatioDecimal != "0.7500" || available.Numerator != 3 || available.Denominator != 4 {
		t.Fatalf("available coverage=%+v", available)
	}

	unavailable := postureCoverage("causal", "Causal", 0, 0, "declared_parent_links", "basis", "limitation")
	if unavailable.Available || unavailable.RatioDecimal != "" || unavailable.Denominator != 0 {
		t.Fatalf("unavailable coverage=%+v", unavailable)
	}
}

func TestPostureCausalCoveragePreservesUnresolvedAndAmbiguousLinks(t *testing.T) {
	coverage := postureCausalCoverage(7, 4, 2, 1)
	if !coverage.Available || coverage.RatioDecimal != "0.5714" ||
		coverage.UnresolvedCount != 2 || coverage.AmbiguousCount != 1 {
		t.Fatalf("causal coverage=%+v", coverage)
	}
}
