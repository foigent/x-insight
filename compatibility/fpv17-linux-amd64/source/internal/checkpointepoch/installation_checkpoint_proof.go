package checkpointepoch

import (
	"context"
	"database/sql"
	"fmt"
)

// VerifyInstallationCheckpointAuthority checks the original register-receiver
// digest after the worker has started. StateSHA256 covers the entire dormant
// state, so current polling fields must not be mistaken for that frozen digest.
// The original activation fields and metadata remain mandatory and immutable.
func VerifyInstallationCheckpointAuthority(ctx context.Context, tx *sql.Tx, input InstallationEpochInput, epoch int64, expectedDigest string) error {
	if tx == nil || epoch < 1 || !sha256Pattern.MatchString(expectedDigest) {
		return ErrInvalid
	}
	authority, _, err := installationPrepareInput(input)
	if err != nil {
		return err
	}
	state, err := loadActiveState(ctx, tx, input.Binding, false)
	if err != nil {
		return err
	}
	if state.SourceInstance != input.SourceInstance || state.AuthorizationRef != authority || state.Epoch != epoch || state.EpochOperationID != "" || state.ActivationCursor != input.Cursor || state.ActivationHighWatermark != input.HighWatermark || !state.CreatedAt.Equal(state.ActivatedAt) {
		return fmt.Errorf("%w: original installation activation identity", ErrConflict)
	}
	// Reconstruct only the exact original commit shape defined by
	// CommitInstallationEpoch; do not mutate live cursor/worker statistics.
	state.Cursor, state.HighWatermark = state.ActivationCursor, state.ActivationHighWatermark
	state.LegacyStatus, state.WorkerStatus = "idle", "inactive"
	state.LastPollStartedAt, state.LastPollSucceededAt, state.LastPollFailedAt = nil, nil, nil
	state.LastErrorCode = ""
	state.PagesCommitted, state.ItemsReceived, state.ItemsCommitted = 0, 0, 0
	state.DuplicateNoop, state.ConsecutiveFailures = 0, 0
	state.UpdatedAt = state.ActivatedAt
	if err = validatePostgresInstallationActiveState(ctx, tx, state); err != nil {
		return err
	}
	digest, err := hashActiveState(state)
	if err != nil {
		return err
	}
	if digest != expectedDigest {
		return fmt.Errorf("%w: original installation state digest", ErrConflict)
	}
	return nil
}
