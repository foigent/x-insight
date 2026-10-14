package investigation

import (
	"testing"
	"time"
)

func TestCompareProjectionsPreservesMissingDistinctFromZero(t *testing.T) {
	start := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)
	zero := int64(0)
	comparison := CompareProjections(
		InvestigationProjection{
			Execution: ExecutionSummary{
				ExecutionID: "xe1_left", Outcome: OutcomeSuccess,
				FirstEffectiveAt: start, LastEffectiveAt: start.Add(time.Millisecond),
			},
			Nodes: []InvestigationNode{
				{
					EventID: "left-terminal", Kind: "execution_completed",
					TotalTokens: &zero, Provider: "provider-a",
				},
			},
			Integrity: completeComparisonIntegrity(),
		},
		start.Add(time.Hour),
		InvestigationProjection{
			Execution: ExecutionSummary{
				ExecutionID: "xe1_right", Outcome: OutcomeSuccess,
				FirstEffectiveAt: start, LastEffectiveAt: start.Add(2 * time.Millisecond),
			},
			Nodes:     []InvestigationNode{{EventID: "right-terminal", Kind: "execution_completed"}},
			Integrity: completeComparisonIntegrity(),
		},
		start.Add(2*time.Hour),
	)
	tokens := comparison.Dimensions[3]
	if tokens.Left.State != "observed" || tokens.Left.Value != "0" ||
		tokens.Right.State != "missing" || tokens.Relation != "unavailable" {
		t.Fatalf("tokens=%+v", tokens)
	}
	if comparison.Dimensions[6].Left.Value != "1000" ||
		comparison.Dimensions[6].Right.Value != "2000" {
		t.Fatalf("duration=%+v", comparison.Dimensions[6])
	}
	if !comparison.LeftAsOf.Equal(start.Add(time.Hour)) ||
		!comparison.RightAsOf.Equal(start.Add(2*time.Hour)) {
		t.Fatalf("comparison snapshot identity=%+v", comparison)
	}
}

func TestCompareProjectionsMarksVisiblePathValuesPartialWhenCompletenessIsUnproved(t *testing.T) {
	start := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)
	tokens := int64(17)
	comparison := CompareProjections(
		InvestigationProjection{
			Execution: ExecutionSummary{
				ExecutionID: "xe1_left", Outcome: OutcomeSuccess,
				FirstEffectiveAt: start, LastEffectiveAt: start.Add(time.Millisecond),
			},
			Nodes: []InvestigationNode{{
				EventID: "left-terminal", Kind: "execution_completed",
				Provider: "provider-a", Model: "model-a", TotalTokens: &tokens,
			}},
			Integrity: InvestigationIntegrity{
				SourceProfileComplete: true,
				ImportComplete:        false,
				ProjectionComplete:    true,
			},
		},
		start,
		InvestigationProjection{
			Execution: ExecutionSummary{
				ExecutionID: "xe1_right", Outcome: OutcomeSuccess,
				FirstEffectiveAt: start, LastEffectiveAt: start.Add(time.Millisecond),
			},
			Nodes: []InvestigationNode{{
				EventID: "right-terminal", Kind: "execution_completed",
				Provider: "provider-a", Model: "model-a", TotalTokens: &tokens,
			}},
			Integrity: completeComparisonIntegrity(),
		},
		start,
	)
	for _, dimensionIndex := range []int{1, 2, 3, 6} {
		dimension := comparison.Dimensions[dimensionIndex]
		if dimension.Left.State != "partial" || dimension.Right.State != "observed" || dimension.Relation != "unavailable" {
			t.Fatalf("dimension=%+v", dimension)
		}
	}
	if comparison.Dimensions[0].Relation != "equal" {
		t.Fatalf("recorded summary outcome should remain comparable: %+v", comparison.Dimensions[0])
	}
}

func completeComparisonIntegrity() InvestigationIntegrity {
	return InvestigationIntegrity{
		SourceProfileComplete: true,
		ImportComplete:        true,
		ProjectionComplete:    true,
	}
}
