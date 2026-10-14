package discovery

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestAnalyzeAbstainsDeterministicallyBelowEvidenceFloor(t *testing.T) {
	generatedAt := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	input := []Observation{
		{ExecutionID: "xe1_a", EvidenceEventIDs: []string{"event-a"}, CoverageMissing: []string{"causal"}},
		{ExecutionID: "xe1_b", EvidenceEventIDs: []string{"event-b"}},
		{ExecutionID: "xe1_c", EvidenceEventIDs: []string{"event-c"}},
		{ExecutionID: "xe1_d", EvidenceEventIDs: []string{"event-d"}},
	}
	first := Analyze(input, Config{MinimumSample: 100, GeneratedAt: generatedAt})
	second := Analyze(input, Config{MinimumSample: 100, GeneratedAt: generatedAt})
	if !reflect.DeepEqual(first, second) {
		t.Fatal("discovery output is not deterministic")
	}
	if len(first) != 6 {
		t.Fatalf("hypotheses=%d", len(first))
	}
	for _, item := range first {
		if item.Status != "abstained" || item.SupportScoreDecimal != "" ||
			item.AbstentionReason == "" || len(item.Limitations) == 0 {
			t.Fatalf("non-abstaining low-evidence output: %+v", item)
		}
	}
}

func TestAuthorityDriftSelectsStrongestActorDeterministically(t *testing.T) {
	items := make([]Observation, 0, 20)
	for index := 0; index < 12; index++ {
		items = append(items, Observation{
			ExecutionID: "xe1_z_" + string(rune('a'+index)),
			ActorRef:    "actor-z",
			PolicyRef:   []string{"policy-1", "policy-2"}[index%2],
		})
	}
	for index := 0; index < 8; index++ {
		items = append(items, Observation{
			ExecutionID: "xe1_a_" + string(rune('a'+index)),
			ActorRef:    "actor-a",
			PolicyRef:   []string{"policy-1", "policy-2"}[index%2],
		})
	}
	forward := Analyze(items, Config{MinimumSample: 20})[5]
	reversed := append([]Observation(nil), items...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	backward := Analyze(reversed, Config{MinimumSample: 20})[5]
	if !reflect.DeepEqual(forward, backward) ||
		!strings.Contains(forward.Statement, "Actor actor-z") ||
		len(forward.ExecutionIDs) != 12 {
		t.Fatalf("authority-drift selection is not deterministic and support-ranked: forward=%+v backward=%+v", forward, backward)
	}
}

func TestAnomalyThresholdSaturatesAtInt64Boundary(t *testing.T) {
	latency := int64(math.MaxInt64)
	items := make([]Observation, 20)
	for index := range items {
		items[index] = Observation{
			ExecutionID: "xe1_max_" + string(rune('a'+index)),
			LatencyMS:   &latency,
		}
	}
	result := Analyze(items, Config{MinimumSample: 20})[3]
	if result.Status != "abstained" || result.SupportScoreDecimal != "" ||
		result.AbstentionReason == "" {
		t.Fatalf("overflowing latency cohort produced a false anomaly: %+v", result)
	}
}

func TestAnalyzeNeverUsesRawContentAndKeepsEvidenceRefs(t *testing.T) {
	generatedAt := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	latency := int64(10)
	items := make([]Observation, 100)
	for index := range items {
		items[index] = Observation{
			ExecutionID:      "xe1_" + string(rune('a'+index%26)) + string(rune('A'+index%26)),
			EvidenceEventIDs: []string{"event-shared"},
			Sequence:         []string{"route_selected", "execution_completed"},
			ActorRef:         "actor:opaque",
			Provider:         "provider-a",
			PolicyRef:        "policy-v1",
			LatencyMS:        &latency,
		}
	}
	output := Analyze(items, Config{MinimumSample: 100, GeneratedAt: generatedAt})
	if output[1].Status != "candidate" || len(output[1].EvidenceEventIDs) != 1 {
		t.Fatalf("process hypothesis=%+v", output[1])
	}
}

func TestAnalyzeBoundsLongOpaqueStatementsWithoutLosingDeterministicIdentity(t *testing.T) {
	generatedAt := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	sequence := make([]string, 200)
	for index := range sequence {
		sequence[index] = strings.Repeat("界", 50) + string(rune('a'+index%26))
	}
	items := make([]Observation, 20)
	for index := range items {
		items[index] = Observation{
			ExecutionID: "xe1_long_" + string(rune('a'+index)),
			Sequence:    append([]string(nil), sequence...),
		}
	}
	first := Analyze(items, Config{MinimumSample: 20, GeneratedAt: generatedAt})[1]
	second := Analyze(items, Config{MinimumSample: 20, GeneratedAt: generatedAt})[1]
	if first.Status != "candidate" ||
		len(first.Statement) > maxHypothesisStatementBytes ||
		!utf8.ValidString(first.Statement) ||
		!strings.Contains(first.Statement, "[truncated sha256:") ||
		!reflect.DeepEqual(first, second) {
		t.Fatalf("bounded process hypothesis=%+v", first)
	}
}
