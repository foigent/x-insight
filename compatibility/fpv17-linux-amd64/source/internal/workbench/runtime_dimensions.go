package workbench

// Dimensions describe statements in authorized frozen observations, not a
// current model catalogue or proof of successful delivery. A request can occur
// in several groups (fallbacks, operations or required capabilities).
const RuntimeDimensionsVersion = "xinsight.runtime.dimensions.v1"

func ValidRuntimeDimension(d string) bool {
	switch d {
	case "operation", "selected_model", "selected_provider", "required_capability", "enabled_capability", "destination_provider", "destination_origin":
		return true
	}
	return false
}

type RuntimeDimensionGroup struct {
	Value            string `json:"value"`
	EvidenceState    string `json:"evidence_state"`
	Requests         int64  `json:"requests"`
	Subjects         int64  `json:"subjects"`
	ObservationCount int64  `json:"observation_count"`
}

type RuntimeDimensions struct {
	ReadingID              string                  `json:"reading_id"`
	DimensionsVersion      string                  `json:"dimensions_version"`
	InterpretationRevision string                  `json:"interpretation_revision"`
	Dimension              string                  `json:"dimension"`
	EntryTime              string                  `json:"entry_time"`
	Requests               int64                   `json:"requests"`
	RequestsWithValues     int64                   `json:"requests_with_values"`
	RequestsWithoutValues  int64                   `json:"requests_without_values"`
	MissingPredecessors    int64                   `json:"missing_predecessors"`
	GroupsMayOverlap       bool                    `json:"groups_may_overlap"`
	Items                  []RuntimeDimensionGroup `json:"items"`
	NextCursor             string                  `json:"next_cursor,omitempty"`
}
