package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/foigent/x-insight/internal/investigation"
)

const (
	MaxAnalysisExecutions = 1000
	MaxEventsPerExecution = 200
	MaxGapsPerExecution   = 100
)

type EvidenceReader interface {
	SearchExecutions(context.Context, investigation.SearchQuery) ([]investigation.ExecutionSummary, error)
	ListExecutionEvents(context.Context, string, time.Time, int) ([]investigation.EvidenceEventDetail, int, error)
	ListExecutionGaps(context.Context, investigation.GapQuery) ([]investigation.CaptureGap, int, error)
}

type AnalysisRun struct {
	ID            string                              `json:"id"`
	MethodVersion string                              `json:"method_version"`
	AnalysisAsOf  time.Time                           `json:"analysis_as_of"`
	SampleSize    int                                 `json:"sample_size"`
	MinimumSample int                                 `json:"minimum_sample"`
	InputDigest   string                              `json:"input_digest"`
	GeneratedAt   time.Time                           `json:"generated_at"`
	Hypotheses    []investigation.DiscoveryHypothesis `json:"hypotheses"`
}

type RunStore interface {
	SaveDiscoveryRun(context.Context, AnalysisRun) error
}

func BuildRun(
	ctx context.Context,
	reader EvidenceReader,
	analysisAsOf time.Time,
	limit int,
	minimumSample int,
) (AnalysisRun, error) {
	if reader == nil ||
		analysisAsOf.IsZero() ||
		analysisAsOf.Location() != time.UTC ||
		limit < 20 ||
		limit > MaxAnalysisExecutions ||
		minimumSample < 20 ||
		minimumSample > limit {
		return AnalysisRun{}, errors.New("discovery analysis configuration is invalid")
	}
	summaries, err := reader.SearchExecutions(ctx, investigation.SearchQuery{
		AsOf:  analysisAsOf,
		From:  time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    analysisAsOf,
		Limit: limit,
	})
	if err != nil {
		return AnalysisRun{}, err
	}
	observations := make([]Observation, 0, len(summaries))
	for _, summary := range summaries {
		events, eventTotal, err := reader.ListExecutionEvents(
			ctx,
			summary.ExecutionID,
			analysisAsOf,
			MaxEventsPerExecution+1,
		)
		if err != nil {
			return AnalysisRun{}, err
		}
		gaps, gapTotal, err := reader.ListExecutionGaps(ctx, investigation.GapQuery{
			ExecutionID: summary.ExecutionID,
			AsOf:        analysisAsOf,
			Limit:       MaxGapsPerExecution + 1,
		})
		if err != nil {
			return AnalysisRun{}, err
		}
		observations = append(observations, observationFromEvidence(
			summary.ExecutionID,
			events,
			eventTotal,
			gaps,
			gapTotal,
		))
	}
	normalizeObservations(observations)
	canonicalInput, err := json.Marshal(struct {
		MethodVersion string        `json:"method_version"`
		AnalysisAsOf  time.Time     `json:"analysis_as_of"`
		MinimumSample int           `json:"minimum_sample"`
		Observations  []Observation `json:"observations"`
	}{
		MethodVersion: MethodVersion,
		AnalysisAsOf:  analysisAsOf,
		MinimumSample: minimumSample,
		Observations:  observations,
	})
	if err != nil {
		return AnalysisRun{}, err
	}
	inputHash := sha256.Sum256(canonicalInput)
	inputDigest := "sha256:" + hex.EncodeToString(inputHash[:])
	runHash := sha256.Sum256([]byte(
		MethodVersion + "\x00" +
			analysisAsOf.Format(time.RFC3339Nano) + "\x00" +
			inputDigest,
	))
	runID := "xdr_" + hex.EncodeToString(runHash[:])
	hypotheses := Analyze(observations, Config{
		MinimumSample: minimumSample,
		GeneratedAt:   analysisAsOf,
		RunID:         runID,
		AnalysisAsOf:  analysisAsOf,
		SampleSize:    len(observations),
		InputDigest:   inputDigest,
	})
	return AnalysisRun{
		ID:            runID,
		MethodVersion: MethodVersion,
		AnalysisAsOf:  analysisAsOf,
		SampleSize:    len(observations),
		MinimumSample: minimumSample,
		InputDigest:   inputDigest,
		GeneratedAt:   analysisAsOf,
		Hypotheses:    hypotheses,
	}, nil
}

func observationFromEvidence(
	executionID string,
	events []investigation.EvidenceEventDetail,
	eventTotal int,
	gaps []investigation.CaptureGap,
	gapTotal int,
) Observation {
	if len(events) > MaxEventsPerExecution {
		events = events[:MaxEventsPerExecution]
	}
	if len(gaps) > MaxGapsPerExecution {
		gaps = gaps[:MaxGapsPerExecution]
	}
	observation := Observation{ExecutionID: executionID}
	for _, event := range events {
		observation.EvidenceEventIDs = append(observation.EvidenceEventIDs, event.EventID)
		observation.Sequence = append(observation.Sequence, event.EventType)
		if observation.ActorRef == "" {
			observation.ActorRef = event.Identity.ActorRef
		}
		if observation.Provider == "" {
			observation.Provider = event.Capability.Provider
		}
		if observation.PolicyRef == "" {
			observation.PolicyRef = event.Decision.PolicyRef
		}
		if event.Usage.LatencyMS != nil &&
			(observation.LatencyMS == nil || *event.Usage.LatencyMS > *observation.LatencyMS) {
			value := *event.Usage.LatencyMS
			observation.LatencyMS = &value
		}
	}
	for _, gap := range gaps {
		observation.CoverageMissing = append(
			observation.CoverageMissing,
			gap.GapType+":"+gap.AffectedClaim,
		)
	}
	if eventTotal > MaxEventsPerExecution {
		observation.CoverageMissing = append(observation.CoverageMissing, "bounded:event_projection")
	}
	if gapTotal > MaxGapsPerExecution {
		observation.CoverageMissing = append(observation.CoverageMissing, "bounded:gap_projection")
	}
	return observation
}

func normalizeObservations(observations []Observation) {
	for index := range observations {
		observations[index].EvidenceEventIDs = sortedUnique(observations[index].EvidenceEventIDs)
		observations[index].CoverageMissing = sortedUnique(observations[index].CoverageMissing)
		if observations[index].Sequence == nil {
			observations[index].Sequence = []string{}
		}
	}
	sort.Slice(observations, func(left, right int) bool {
		return observations[left].ExecutionID < observations[right].ExecutionID
	})
}

func sortedUnique(values []string) []string {
	sort.Strings(values)
	output := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || (len(output) > 0 && output[len(output)-1] == value) {
			continue
		}
		output = append(output, value)
	}
	return output
}
