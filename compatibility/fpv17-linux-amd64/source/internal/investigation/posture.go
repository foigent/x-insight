package investigation

import "time"

const PostureSchema = "xinsight.posture.v1"

type PostureQuery struct {
	AsOf           time.Time
	From           time.Time
	To             time.Time
	SourceSystemID string
}

type PostureSnapshot struct {
	SchemaVersion  string                  `json:"schema_version"`
	AsOf           time.Time               `json:"as_of"`
	From           time.Time               `json:"from"`
	To             time.Time               `json:"to"`
	SourceSystemID string                  `json:"source_system_id,omitempty"`
	Risk           PostureRiskSummary      `json:"risk"`
	Executions     PostureExecutionSummary `json:"executions"`
	Coverage       []PostureCoverage       `json:"coverage"`
	Sources        []PostureSourceSummary  `json:"sources"`
	Trend          PostureTrend            `json:"trend"`
	Boundaries     []string                `json:"boundaries"`
}

type PostureRiskSummary struct {
	OpenTotal          int64                `json:"open_total"`
	Critical           int64                `json:"critical"`
	Error              int64                `json:"error"`
	Warning            int64                `json:"warning"`
	Info               int64                `json:"info"`
	AffectedExecutions int64                `json:"affected_executions"`
	Unlinked           int64                `json:"unlinked"`
	Items              []InvestigationAlert `json:"items"`
}

type PostureExecutionSummary struct {
	Total     int64 `json:"total"`
	Success   int64 `json:"success"`
	Failure   int64 `json:"failure"`
	Partial   int64 `json:"partial"`
	Unknown   int64 `json:"unknown"`
	WithGaps  int64 `json:"with_gaps"`
	TotalGaps int64 `json:"total_gaps"`
}

type PostureCoverage struct {
	Dimension       string `json:"dimension"`
	Label           string `json:"label"`
	Available       bool   `json:"available"`
	Numerator       int64  `json:"numerator"`
	Denominator     int64  `json:"denominator"`
	RatioDecimal    string `json:"ratio_decimal,omitempty"`
	Unit            string `json:"unit"`
	UnresolvedCount int64  `json:"unresolved_count,omitempty"`
	AmbiguousCount  int64  `json:"ambiguous_count,omitempty"`
	Basis           string `json:"basis"`
	Limitation      string `json:"limitation"`
}

type PostureSourceSummary struct {
	SourceSystemID     string    `json:"source_system_id"`
	SourceType         string    `json:"source_type"`
	SourceDisplayName  string    `json:"source_display_name"`
	ExecutionTotal     int64     `json:"execution_total"`
	FailureTotal       int64     `json:"failure_total"`
	ExecutionsWithGaps int64     `json:"executions_with_gaps"`
	LastEffectiveAt    time.Time `json:"last_effective_at"`
}

type PostureTrend struct {
	BucketUnit string               `json:"bucket_unit"`
	Items      []PostureTrendBucket `json:"items"`
	Boundary   string               `json:"boundary"`
}

type PostureTrendBucket struct {
	BucketStart        time.Time `json:"bucket_start"`
	ExecutionTotal     int64     `json:"execution_total"`
	FailureTotal       int64     `json:"failure_total"`
	PartialTotal       int64     `json:"partial_total"`
	ExecutionsWithGaps int64     `json:"executions_with_gaps"`
	OpenAlerts         int64     `json:"open_alerts"`
}
