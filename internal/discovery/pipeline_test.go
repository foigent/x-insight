package discovery

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/investigation"
)

func TestBuildRunIsFixedSnapshotDeterministicAndAbstains(t *testing.T) {
	asOf := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	reader := &discoveryReaderStub{
		summaries: []investigation.ExecutionSummary{
			{ExecutionID: "xe1_bbbbbbbb"},
			{ExecutionID: "xe1_aaaaaaaa"},
		},
		events: map[string][]investigation.EvidenceEventDetail{
			"xe1_aaaaaaaa": {
				{
					EventID:    "11111111-1111-4111-8111-111111111111",
					EventType:  "model.request.completed",
					Identity:   investigation.IdentityGroup{ActorRef: "actor:a"},
					Capability: investigation.CapabilityGroup{Provider: "provider-a"},
					Decision:   investigation.DecisionGroup{PolicyRef: "policy-a"},
				},
			},
			"xe1_bbbbbbbb": {
				{
					EventID:   "22222222-2222-4222-8222-222222222222",
					EventType: "model.request.completed",
				},
			},
		},
		gaps: map[string][]investigation.CaptureGap{
			"xe1_bbbbbbbb": {
				{GapType: "not_observed", AffectedClaim: "causal_parent"},
			},
		},
	}
	first, err := BuildRun(context.Background(), reader, asOf, 100, 20)
	if err != nil {
		t.Fatal(err)
	}
	reader.summaries[0], reader.summaries[1] = reader.summaries[1], reader.summaries[0]
	second, err := BuildRun(context.Background(), reader, asOf, 100, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("fixed snapshot run is not deterministic:\n%+v\n%+v", first, second)
	}
	if first.SampleSize != 2 || !strings.HasPrefix(first.ID, "xdr_") ||
		!strings.HasPrefix(first.InputDigest, "sha256:") ||
		len(first.Hypotheses) != 6 {
		t.Fatalf("run=%+v", first)
	}
	for _, item := range first.Hypotheses {
		if item.Status != "abstained" ||
			item.AnalysisRunID != first.ID ||
			item.InputDigest != first.InputDigest ||
			!item.AnalysisAsOf.Equal(asOf) {
			t.Fatalf("hypothesis is not bound to the abstaining run: %+v", item)
		}
	}
}

func TestBuildRunFailsClosedOnInvalidOrUnreadableInput(t *testing.T) {
	asOf := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	if _, err := BuildRun(context.Background(), nil, asOf, 100, 20); err == nil {
		t.Fatal("nil evidence reader was accepted")
	}
	reader := &discoveryReaderStub{searchErr: context.Canceled}
	if _, err := BuildRun(context.Background(), reader, asOf, 100, 20); err == nil {
		t.Fatal("evidence read failure was ignored")
	}
}

type discoveryReaderStub struct {
	summaries []investigation.ExecutionSummary
	events    map[string][]investigation.EvidenceEventDetail
	gaps      map[string][]investigation.CaptureGap
	searchErr error
}

func (reader *discoveryReaderStub) SearchExecutions(
	context.Context,
	investigation.SearchQuery,
) ([]investigation.ExecutionSummary, error) {
	return append([]investigation.ExecutionSummary(nil), reader.summaries...), reader.searchErr
}

func (reader *discoveryReaderStub) ListExecutionEvents(
	_ context.Context,
	executionID string,
	_ time.Time,
	_ int,
) ([]investigation.EvidenceEventDetail, int, error) {
	items := append([]investigation.EvidenceEventDetail(nil), reader.events[executionID]...)
	return items, len(items), nil
}

func (reader *discoveryReaderStub) ListExecutionGaps(
	_ context.Context,
	query investigation.GapQuery,
) ([]investigation.CaptureGap, int, error) {
	items := append([]investigation.CaptureGap(nil), reader.gaps[query.ExecutionID]...)
	return items, len(items), nil
}
