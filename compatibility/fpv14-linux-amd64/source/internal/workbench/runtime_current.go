package workbench

import (
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

const RuntimeCurrentVersion = "xinsight.runtime.lease-inventory.v1"

type RuntimeCurrentItem struct {
	Ref                string                          `json:"ref"`
	SourceSystemID     string                          `json:"source_system_id"`
	SourceInstance     string                          `json:"source_instance"`
	OrgID              string                          `json:"org_id"`
	ProjectID          string                          `json:"project_id"`
	SnapshotObservedAt time.Time                       `json:"snapshot_observed_at"`
	ReceivedAt         time.Time                       `json:"received_at"`
	SourceSHA256       string                          `json:"source_sha256"`
	CurrentState       string                          `json:"current_state"`
	Entry              sourcefacts.CurrentRuntimeEntry `json:"entry"`
}

type RuntimeCurrentInventory struct {
	Version     string               `json:"version"`
	EvaluatedAt time.Time            `json:"evaluated_at"`
	Coverage    string               `json:"coverage"`
	Uncovered   []string             `json:"uncovered"`
	Items       []RuntimeCurrentItem `json:"items"`
	NextCursor  string               `json:"next_cursor,omitempty"`
}

func ValidateRuntimeCurrentQuery(filters Filters, limit int) error {
	if limit < 1 || limit > MaxPageSize {
		return ErrInvalid
	}
	return validateFilters(filters)
}

// The source attests lease validity, not continuous connectivity or arbitrary
// synchronous requests. A known source failure, restore, expired lease, or
// inconsistent clock cannot be promoted to current execution.
func RuntimeCurrentState(e sourcefacts.CurrentRuntimeEntry, observed, received, now time.Time, sourceReady bool) string {
	if e.Freshness == "last_known" {
		return "last_known"
	}
	last, err := time.Parse(sourcefacts.TimeFormat, e.LastObservedAt)
	until, untilErr := time.Parse(sourcefacts.TimeFormat, e.ValidUntil)
	if !sourceReady || err != nil || untilErr != nil || observed.After(received) || received.After(now) || last.After(observed) || !until.After(now) {
		return "not_current"
	}
	switch e.Freshness {
	case "lease_valid_execution":
		return "lease_valid_execution"
	case "leased_pending":
		return "leased_pending"
	default:
		return "not_current"
	}
}
