package workbench

import (
	"errors"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"time"
)

var ErrContentExpired = errors.New("content representation expired")
var ErrContentIntegrity = errors.New("content manifest does not match frozen reference")

const RuntimeContentVersion = "xinsight.runtime-content.v1"
const ContentInterpretationRevision = "xinsight.content-membership.v1"

// RuntimeContentObservation is derived from the exact hash-bound manifest. It
// never becomes a second frozen identity or a grant to read the body.
type RuntimeContentObservation struct {
	Protocol         string `json:"protocol"`
	ObservationPoint string `json:"observation_point"`
	AttemptID        string `json:"attempt_id,omitempty"`
	ContentItemID    string `json:"content_item_id"`
	OccurredAt       string `json:"occurred_at"`
	CaptureReason    string `json:"capture_reason"`
}

type RuntimeContentItem struct {
	Observation   *RuntimeContentObservation      `json:"observation,omitempty"`
	Reference     sourcefacts.FrozenManifestRef   `json:"reference"`
	Availability  sourcefacts.ContentAvailability `json:"availability"`
	NewerRevision bool                            `json:"newer_revision"`
}

type RuntimeContents struct {
	Version                       string               `json:"version"`
	ReadingID                     string               `json:"reading_id"`
	RequestRef                    string               `json:"request_ref"`
	MembershipRef                 string               `json:"membership_ref"`
	ContentInterpretationRevision string               `json:"content_interpretation_revision"`
	MemberBasis                   string               `json:"member_basis"`
	Items                         []RuntimeContentItem `json:"items"`
	NextCursor                    string               `json:"next_cursor,omitempty"`
}

// Internal read admission, never a browser credential or byte response. The
// current source capabilities supply limits; the database does not invent them.
type RuntimeContentRead struct {
	ReadingExpiresAt             time.Time
	ManifestSequence             string
	ManifestOccurredAt           time.Time
	Reference                    sourcefacts.FrozenManifestRef
	Collection                   sourcefacts.ContentCollection
	ContentAuthorizationRevision string
	MembershipRef                string
}
