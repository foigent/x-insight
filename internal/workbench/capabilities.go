package workbench

// ReadingCapabilities describe available entry points, not authorization for a
// selected project. Every reading still checks its exact filters and fields.
type ReadingCapabilities struct {
	Summary      bool `json:"summary"`
	Runtime      bool `json:"runtime"`
	Coverage     bool `json:"coverage"`
	WorkActivity bool `json:"work_activity"`
}
