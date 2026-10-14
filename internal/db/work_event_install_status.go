package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/foigent/x-insight/internal/workevent"
)

const maxWorkInstallStatusCount int64 = 1<<53 - 1

// Status is a bounded host report, not a claim that all source events were captured.
type WorkInstallStatus struct {
	MetadataPending *int64            `json:"metadata_pending"`
	MetadataBlocked *int64            `json:"metadata_blocked"`
	BodyPending     *int64            `json:"body_pending"`
	Gaps            *[]WorkInstallGap `json:"gaps"`
}

type WorkInstallGap struct {
	Code        string `json:"code"`
	Occurrences int64  `json:"occurrences"`
}

var workInstallGapCodes = map[string]bool{
	"body_limit":                 true,
	"body_unavailable":           true,
	"metadata_unavailable":       true,
	"receiver_unavailable":       true,
	"source_mapping_unconfirmed": true,
	"future_scope_unconfirmed":   true,
	"renewal_unconfirmed":        true,
	"ur_relation_unconfirmed":    true,
	"outbox_full":                true,
	"observation_unavailable":    true,
}

func ValidWorkInstallStatus(status *WorkInstallStatus) bool {
	if status == nil || status.MetadataPending == nil || status.MetadataBlocked == nil ||
		status.BodyPending == nil || status.Gaps == nil || len(*status.Gaps) > 64 {
		return false
	}
	for _, count := range []*int64{status.MetadataPending, status.MetadataBlocked, status.BodyPending} {
		if *count < 0 || *count > maxWorkInstallStatusCount {
			return false
		}
	}
	seen := make(map[string]bool, len(*status.Gaps))
	for _, gap := range *status.Gaps {
		if !workInstallGapCodes[gap.Code] || seen[gap.Code] || gap.Occurrences < 1 ||
			gap.Occurrences > maxWorkInstallStatusCount {
			return false
		}
		seen[gap.Code] = true
	}
	return true
}

// The owner resolves installation identity from the current service bearer;
// the host cannot select another installation through the request body.
func (s *SourceFactsStore) ReportWorkInstallStatus(ctx context.Context, identityID, digest string,
	status WorkInstallStatus, now time.Time) (string, error) {
	if s == nil || s.db == nil || !serviceIdentityPattern.MatchString(identityID) ||
		!serviceTokenDigestPattern.MatchString(digest) || !ValidWorkInstallStatus(&status) || now.IsZero() {
		return "", workevent.ErrInvalid
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		return "", workevent.ErrInvalid
	}
	var invitationID string
	err = s.db.QueryRowContext(ctx, `UPDATE work_event_install_invitations w
SET latest_status=$3::jsonb,status_received_at=$4
WHERE w.approved_service_identity_id=$1 AND w.state='approved'
AND EXISTS (SELECT 1 FROM service_identities i
 JOIN service_identity_tokens t ON t.service_identity_id=i.id
 WHERE i.id=$1 AND i.status='active' AND i.permissions=ARRAY['work_event.ingest']::text[]
 AND i.expires_at>$4 AND t.token_digest=$2 AND t.revoked_at IS NULL AND t.expires_at>$4)
RETURNING w.invitation_id`, identityID, digest, string(encoded), now.UTC()).Scan(&invitationID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", workevent.ErrScopeChanged
		}
		return "", err
	}
	return invitationID, nil
}
