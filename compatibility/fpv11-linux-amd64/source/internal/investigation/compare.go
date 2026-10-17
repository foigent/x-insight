package investigation

import (
	"strconv"
	"strings"
	"time"
)

const ComparisonSchema = "xinsight.execution-comparison.v1"

type ComparisonValue struct {
	State            string   `json:"state"`
	Value            string   `json:"value,omitempty"`
	Unit             string   `json:"unit,omitempty"`
	EvidenceEventIDs []string `json:"evidence_event_ids,omitempty"`
}

type ComparisonDimension struct {
	Dimension string          `json:"dimension"`
	Left      ComparisonValue `json:"left"`
	Right     ComparisonValue `json:"right"`
	Relation  string          `json:"relation"`
}

type ExecutionComparison struct {
	SchemaVersion string                `json:"schema_version"`
	LeftAsOf      time.Time             `json:"left_as_of"`
	RightAsOf     time.Time             `json:"right_as_of"`
	Left          ExecutionSummary      `json:"left"`
	Right         ExecutionSummary      `json:"right"`
	Dimensions    []ComparisonDimension `json:"dimensions"`
}

func CompareProjections(
	left InvestigationProjection,
	leftAsOf time.Time,
	right InvestigationProjection,
	rightAsOf time.Time,
) ExecutionComparison {
	leftTerminal := terminalNode(left.Nodes)
	rightTerminal := terminalNode(right.Nodes)
	leftPathComplete := comparisonPathComplete(left)
	rightPathComplete := comparisonPathComplete(right)
	dimensions := []ComparisonDimension{
		compareDimension("outcome", observed(left.Execution.Outcome, ""), observed(right.Execution.Outcome, "")),
		compareDimension("provider_path", providerPath(left.Nodes, leftPathComplete), providerPath(right.Nodes, rightPathComplete)),
		compareDimension("model_path", modelPath(left.Nodes, leftPathComplete), modelPath(right.Nodes, rightPathComplete)),
		compareDimension("total_tokens", nodeIntValue(leftTerminal, "tokens", leftPathComplete), nodeIntValue(rightTerminal, "tokens", rightPathComplete)),
		compareDimension("cost_micro_cny", nodeStringValue(leftTerminal, "cost", leftPathComplete), nodeStringValue(rightTerminal, "cost", rightPathComplete)),
		compareDimension("latency_ms", nodeIntValue(leftTerminal, "latency", leftPathComplete), nodeIntValue(rightTerminal, "latency", rightPathComplete)),
		compareDimension("duration_microseconds", durationValue(left.Execution, leftPathComplete), durationValue(right.Execution, rightPathComplete)),
		compareDimension(
			"recorded_gap_count",
			observed(strconv.Itoa(left.Execution.GapCount), "count"),
			observed(strconv.Itoa(right.Execution.GapCount), "count"),
		),
	}
	return ExecutionComparison{
		SchemaVersion: ComparisonSchema,
		LeftAsOf:      leftAsOf.UTC(),
		RightAsOf:     rightAsOf.UTC(),
		Left:          left.Execution,
		Right:         right.Execution,
		Dimensions:    dimensions,
	}
}

func comparisonPathComplete(projection InvestigationProjection) bool {
	return projection.Integrity.SourceProfileComplete &&
		projection.Integrity.ImportComplete &&
		projection.Integrity.ProjectionComplete
}

func compareDimension(dimension string, left ComparisonValue, right ComparisonValue) ComparisonDimension {
	relation := "unavailable"
	if left.State == "observed" && right.State == "observed" {
		relation = "different"
		if left.Value == right.Value && left.Unit == right.Unit {
			relation = "equal"
		}
	}
	return ComparisonDimension{Dimension: dimension, Left: left, Right: right, Relation: relation}
}

func observed(value string, unit string, evidenceIDs ...string) ComparisonValue {
	if value == "" {
		return ComparisonValue{State: "missing"}
	}
	return ComparisonValue{
		State: "observed", Value: value, Unit: unit,
		EvidenceEventIDs: append([]string(nil), evidenceIDs...),
	}
}

func visibleComparisonValue(
	value string,
	unit string,
	complete bool,
	evidenceIDs ...string,
) ComparisonValue {
	item := observed(value, unit, evidenceIDs...)
	if item.State == "observed" && !complete {
		item.State = "partial"
	}
	return item
}

func terminalNode(nodes []InvestigationNode) *InvestigationNode {
	for index := len(nodes) - 1; index >= 0; index-- {
		if nodes[index].Kind == "execution_completed" || nodes[index].Kind == "provider_succeeded" {
			return &nodes[index]
		}
	}
	return nil
}

func nodeIntValue(node *InvestigationNode, field string, complete bool) ComparisonValue {
	if node == nil {
		return ComparisonValue{State: "missing"}
	}
	switch field {
	case "tokens":
		if node.TotalTokens != nil {
			return visibleComparisonValue(strconv.FormatInt(*node.TotalTokens, 10), "tokens", complete, node.EventID)
		}
	case "latency":
		if node.LatencyMS != nil {
			return visibleComparisonValue(strconv.FormatInt(*node.LatencyMS, 10), "milliseconds", complete, node.EventID)
		}
	}
	return ComparisonValue{State: "missing"}
}

func nodeStringValue(node *InvestigationNode, field string, complete bool) ComparisonValue {
	if node == nil {
		return ComparisonValue{State: "missing"}
	}
	if field == "cost" && node.CostMicros != "" {
		return visibleComparisonValue(node.CostMicros, "micro-CNY", complete, node.EventID)
	}
	return ComparisonValue{State: "missing"}
}

func durationValue(summary ExecutionSummary, complete bool) ComparisonValue {
	if summary.FirstEffectiveAt.IsZero() || summary.LastEffectiveAt.IsZero() ||
		summary.LastEffectiveAt.Before(summary.FirstEffectiveAt) {
		return ComparisonValue{State: "missing"}
	}
	return visibleComparisonValue(
		strconv.FormatInt(summary.LastEffectiveAt.Sub(summary.FirstEffectiveAt).Microseconds(), 10),
		"microseconds",
		complete,
	)
}

func providerPath(nodes []InvestigationNode, complete bool) ComparisonValue {
	return orderedPath(nodes, complete, func(node InvestigationNode) string { return node.Provider })
}

func modelPath(nodes []InvestigationNode, complete bool) ComparisonValue {
	return orderedPath(nodes, complete, func(node InvestigationNode) string { return node.Model })
}

func orderedPath(nodes []InvestigationNode, complete bool, value func(InvestigationNode) string) ComparisonValue {
	values := make([]string, 0)
	evidenceIDs := make([]string, 0)
	for _, node := range nodes {
		current := value(node)
		if current == "" || (len(values) > 0 && values[len(values)-1] == current) {
			continue
		}
		values = append(values, current)
		evidenceIDs = append(evidenceIDs, node.EventID)
	}
	if len(values) == 0 {
		return ComparisonValue{State: "missing"}
	}
	return visibleComparisonValue(strings.Join(values, " → "), "ordered_path", complete, evidenceIDs...)
}
