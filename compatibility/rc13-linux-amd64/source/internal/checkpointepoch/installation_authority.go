package checkpointepoch

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	InstallationEpochBoundarySchema  = "xinsight.connector-installation-epoch-boundary.v1"
	InstallationEpochCommitSchema    = "xinsight.connector-installation-epoch-commit.v1"
	InstallationEpochAuthoritySchema = "xinsight.connector-installation-epoch-authority.v1"

	InstallationEpochRecordActive    = "active"
	InstallationEpochRecordHistory   = "history"
	InstallationEpochRecordOperation = "operation"
)

var (
	installationIDPattern = regexp.MustCompile(`^xci_[A-Za-z0-9_-]{22}$`)
)

// InstallationEpochInput binds a normal Connector installation to its first
// inactive checkpoint state. Unlike EmptyBootstrapInput, it does not depend on
// DATA_RESET evidence and it does not require the whole database to be empty.
type InstallationEpochInput struct {
	Binding
	InstallationID string `json:"installation_id"`
	WorkerUnit     string `json:"worker_unit"`
	SourceInstance string `json:"source_instance"`
	Cursor         string `json:"-"`
	HighWatermark  string `json:"-"`
}

// InstallationEpochRecord is the minimum product-local projection of existing
// checkpoint state, operation and history rows needed to derive a monotonic
// installation epoch without adding another history table. Epoch is the stored
// checkpoint epoch for active/history rows and the reserved next epoch
// (expected_epoch + 1) for operation rows.
type InstallationEpochRecord struct {
	RecordID            string  `json:"record_id"`
	Kind                string  `json:"kind"`
	Status              string  `json:"status,omitempty"`
	Binding             Binding `json:"binding"`
	Epoch               int64   `json:"epoch"`
	AuthorizationRef    string  `json:"authorization_ref"`
	SourceInstance      string  `json:"source_instance"`
	CursorSHA256        string  `json:"cursor_sha256"`
	HighWatermarkSHA256 string  `json:"high_watermark_sha256"`
}

// InstallationEpochBoundary is read from adapter_import_states plus the
// existing connector_checkpoint_epoch_operations/history tables. Active is
// present exactly when the active record exists.
type InstallationEpochBoundary struct {
	SchemaVersion string                    `json:"schema_version"`
	Binding       Binding                   `json:"binding"`
	Records       []InstallationEpochRecord `json:"records"`
	Active        *ActiveStateSummary       `json:"active,omitempty"`
}

// InstallationEpochCommit is a compare-and-set request. A durable adapter must
// compare ExpectedBoundarySHA256 and insert the state in one transaction.
type InstallationEpochCommit struct {
	SchemaVersion          string `json:"schema_version"`
	ExpectedBoundarySHA256 string `json:"expected_boundary_sha256"`
	InstallationID         string `json:"installation_id"`
	RequestSHA256          string `json:"request_sha256"`
	PrepareInput
	Epoch int64 `json:"epoch"`
}

// InstallationEpochPort is deliberately narrow and fakeable. The PostgreSQL
// adapter can be added independently while reusing the current state,
// operation and immutable history tables.
type InstallationEpochPort interface {
	InspectInstallationEpochBoundary(context.Context, Binding) (InstallationEpochBoundary, error)
	CommitInstallationEpoch(context.Context, InstallationEpochCommit, time.Time) (ActiveStateSummary, error)
}

type InstallationEpochAuthority struct {
	SchemaVersion    string             `json:"schema_version"`
	InstallationID   string             `json:"installation_id"`
	AuthorizationRef string             `json:"authorization_ref"`
	BoundarySHA256   string             `json:"boundary_sha256"`
	CommitSHA256     string             `json:"commit_sha256,omitempty"`
	Epoch            int64              `json:"epoch"`
	Replayed         bool               `json:"replayed"`
	Active           ActiveStateSummary `json:"active"`
	AuthoritySHA256  string             `json:"authority_sha256"`
}

func InstallationAuthorizationRef(installationID string) (string, error) {
	if !installationIDPattern.MatchString(installationID) {
		return "", fmt.Errorf("%w: installation_id", ErrInvalid)
	}
	return "xinsight://connector-installations/" + installationID, nil
}

// ApplyInstallationEpoch derives epoch 1 for a never-used binding or strict
// historical max+1 otherwise. Exact active replay is read-only. Every other
// prior use of the installation or Source identity fails closed.
func ApplyInstallationEpoch(
	ctx context.Context,
	port InstallationEpochPort,
	input InstallationEpochInput,
	installedAt time.Time,
) (InstallationEpochAuthority, error) {
	if port == nil || installedAt.IsZero() {
		return InstallationEpochAuthority{}, fmt.Errorf("%w: installation epoch port or time", ErrInvalid)
	}
	authorizationRef, prepare, err := installationPrepareInput(input)
	if err != nil {
		return InstallationEpochAuthority{}, err
	}
	boundary, err := port.InspectInstallationEpochBoundary(ctx, input.Binding)
	if err != nil {
		return InstallationEpochAuthority{}, err
	}
	boundarySHA256, records, activeRecord, err := validateInstallationEpochBoundary(boundary, input.Binding)
	if err != nil {
		return InstallationEpochAuthority{}, err
	}
	if boundary.Active != nil {
		if activeRecord == nil || !installationActiveMatches(
			*boundary.Active, *activeRecord, input, authorizationRef,
		) {
			return InstallationEpochAuthority{}, fmt.Errorf("%w: active checkpoint belongs to another installation or identity", ErrConflict)
		}
		return sealInstallationEpochAuthority(InstallationEpochAuthority{
			SchemaVersion:    InstallationEpochAuthoritySchema,
			InstallationID:   input.InstallationID,
			AuthorizationRef: authorizationRef,
			BoundarySHA256:   boundarySHA256,
			Epoch:            boundary.Active.Epoch,
			Replayed:         true,
			Active:           *boundary.Active,
		})
	}

	maximum := int64(0)
	for _, record := range records {
		if record.Epoch > maximum {
			maximum = record.Epoch
		}
		if err := rejectInstallationIdentityReuse(record, input); err != nil {
			return InstallationEpochAuthority{}, err
		}
	}
	if maximum == math.MaxInt64 {
		return InstallationEpochAuthority{}, fmt.Errorf("%w: installation checkpoint epoch overflow", ErrConflict)
	}
	nextEpoch := int64(1)
	if len(records) > 0 {
		nextEpoch = maximum + 1
	}
	requestSHA256, err := installationPrepareRequestSHA256(prepare)
	if err != nil {
		return InstallationEpochAuthority{}, err
	}
	commit := InstallationEpochCommit{
		SchemaVersion:          InstallationEpochCommitSchema,
		ExpectedBoundarySHA256: boundarySHA256,
		InstallationID:         input.InstallationID,
		RequestSHA256:          requestSHA256,
		PrepareInput:           prepare,
		Epoch:                  nextEpoch,
	}
	commitSHA256, err := hashJSON(commit)
	if err != nil {
		return InstallationEpochAuthority{}, err
	}
	active, err := port.CommitInstallationEpoch(ctx, commit, normalizedTime(installedAt))
	if err != nil {
		return InstallationEpochAuthority{}, err
	}
	if !installationCommittedStateMatches(active, input, nextEpoch) {
		return InstallationEpochAuthority{}, fmt.Errorf("%w: committed installation checkpoint readback", ErrConflict)
	}
	return sealInstallationEpochAuthority(InstallationEpochAuthority{
		SchemaVersion:    InstallationEpochAuthoritySchema,
		InstallationID:   input.InstallationID,
		AuthorizationRef: authorizationRef,
		BoundarySHA256:   boundarySHA256,
		CommitSHA256:     commitSHA256,
		Epoch:            nextEpoch,
		Active:           active,
	})
}

func installationPrepareInput(input InstallationEpochInput) (string, PrepareInput, error) {
	authorizationRef, err := InstallationAuthorizationRef(input.InstallationID)
	if err != nil {
		return "", PrepareInput{}, err
	}
	prepare := PrepareInput{
		Binding:            input.Binding,
		WorkerUnit:         input.WorkerUnit,
		NextSourceInstance: input.SourceInstance,
		NextCursor:         input.Cursor,
		NextHighWatermark:  input.HighWatermark,
		AuthorizationRef:   authorizationRef,
	}
	if validatePrepareInput(prepare) != nil {
		return "", PrepareInput{}, fmt.Errorf("%w: installation checkpoint input", ErrInvalid)
	}
	return authorizationRef, prepare, nil
}

func installationPrepareRequestSHA256(input PrepareInput) (string, error) {
	return hashJSON(PrepareRequest{
		SchemaVersion:      PrepareRequestSchema,
		Binding:            input.Binding,
		WorkerUnit:         input.WorkerUnit,
		NextSourceInstance: input.NextSourceInstance,
		NextCursor:         input.NextCursor,
		NextHighWatermark:  input.NextHighWatermark,
		AuthorizationRef:   input.AuthorizationRef,
	})
}

func validateInstallationEpochBoundary(
	boundary InstallationEpochBoundary,
	binding Binding,
) (string, []InstallationEpochRecord, *InstallationEpochRecord, error) {
	if boundary.SchemaVersion != InstallationEpochBoundarySchema || boundary.Binding != binding {
		return "", nil, nil, fmt.Errorf("%w: installation epoch boundary identity", ErrConflict)
	}
	records := make([]InstallationEpochRecord, len(boundary.Records))
	copy(records, boundary.Records)
	seenRecordIDs := make(map[string]struct{}, len(records))
	installationCase := make(map[string]string)
	sourceCase := make(map[string]string)
	installationSource := make(map[string]string)
	installationEpoch := make(map[string]int64)
	sourceOwner := make(map[string]string)
	durableEpoch := make(map[int64]InstallationEpochRecord)
	var activeRecord *InstallationEpochRecord
	for index := range records {
		record := records[index]
		installationID, installationBound, err := validateInstallationEpochRecord(record, binding)
		if err != nil {
			return "", nil, nil, err
		}
		if _, exists := seenRecordIDs[record.RecordID]; exists {
			return "", nil, nil, fmt.Errorf("%w: duplicate installation epoch record", ErrConflict)
		}
		seenRecordIDs[record.RecordID] = struct{}{}
		if installationBound {
			if previous, exists := installationCase[strings.ToLower(installationID)]; exists && previous != installationID {
				return "", nil, nil, fmt.Errorf("%w: installation identity case collision", ErrConflict)
			}
			installationCase[strings.ToLower(installationID)] = installationID
			if previous, exists := installationSource[installationID]; exists && previous != record.SourceInstance {
				return "", nil, nil, fmt.Errorf("%w: installation history changes Source identity", ErrConflict)
			}
			installationSource[installationID] = record.SourceInstance
			if previous, exists := installationEpoch[installationID]; exists && previous != record.Epoch {
				return "", nil, nil, fmt.Errorf("%w: installation history changes checkpoint epoch", ErrConflict)
			}
			installationEpoch[installationID] = record.Epoch
		}
		if previous, exists := sourceCase[strings.ToLower(record.SourceInstance)]; exists && previous != record.SourceInstance {
			return "", nil, nil, fmt.Errorf("%w: Source identity case collision", ErrConflict)
		}
		sourceCase[strings.ToLower(record.SourceInstance)] = record.SourceInstance
		sourceAuthority := "legacy"
		if installationBound {
			sourceAuthority = installationID
		}
		if previous, exists := sourceOwner[record.SourceInstance]; exists && previous != sourceAuthority {
			return "", nil, nil, fmt.Errorf("%w: Source identity is bound across authorities", ErrConflict)
		}
		sourceOwner[record.SourceInstance] = sourceAuthority
		if record.Kind != InstallationEpochRecordOperation {
			if previous, exists := durableEpoch[record.Epoch]; exists &&
				(previous.AuthorizationRef != record.AuthorizationRef || previous.SourceInstance != record.SourceInstance ||
					previous.CursorSHA256 != record.CursorSHA256 || previous.HighWatermarkSHA256 != record.HighWatermarkSHA256) {
				return "", nil, nil, fmt.Errorf("%w: checkpoint history epoch identity conflict", ErrConflict)
			}
			durableEpoch[record.Epoch] = record
		}
		if record.Kind == InstallationEpochRecordActive {
			if activeRecord != nil {
				return "", nil, nil, fmt.Errorf("%w: multiple active installation checkpoint records", ErrConflict)
			}
			activeRecordValue := record
			activeRecord = &activeRecordValue
		}
	}
	if (boundary.Active == nil) != (activeRecord == nil) {
		return "", nil, nil, fmt.Errorf("%w: active installation checkpoint projection", ErrConflict)
	}
	if boundary.Active != nil && !activeSummaryMatchesRecord(*boundary.Active, *activeRecord) {
		return "", nil, nil, fmt.Errorf("%w: active installation checkpoint summary", ErrConflict)
	}
	sort.Slice(records, func(left, right int) bool {
		first, second := records[left], records[right]
		if first.Epoch != second.Epoch {
			return first.Epoch < second.Epoch
		}
		if first.Kind != second.Kind {
			return first.Kind < second.Kind
		}
		return first.RecordID < second.RecordID
	})
	canonical := boundary
	canonical.Records = records
	digest, err := hashJSON(canonical)
	return digest, records, activeRecord, err
}

func validateInstallationEpochRecord(record InstallationEpochRecord, binding Binding) (string, bool, error) {
	if validateText("record_id", record.RecordID, 1, 128) != nil || record.Binding != binding || record.Epoch < 1 ||
		validateText("source_instance", record.SourceInstance, 1, 128) != nil || !sha256Pattern.MatchString(record.CursorSHA256) ||
		!sha256Pattern.MatchString(record.HighWatermarkSHA256) {
		return "", false, fmt.Errorf("%w: installation epoch record", ErrConflict)
	}
	if err := validateText("authorization_ref", record.AuthorizationRef, 1, 256); err != nil {
		return "", false, fmt.Errorf("%w: checkpoint history authorization reference", ErrConflict)
	}
	switch record.Kind {
	case InstallationEpochRecordActive, InstallationEpochRecordHistory:
		if record.Status != "" {
			return "", false, fmt.Errorf("%w: installation epoch record status", ErrConflict)
		}
	case InstallationEpochRecordOperation:
		switch record.Status {
		case StatusPrepared, StatusActivated, StatusAborted:
		default:
			return "", false, fmt.Errorf("%w: installation epoch operation status", ErrConflict)
		}
	default:
		return "", false, fmt.Errorf("%w: installation epoch record kind", ErrConflict)
	}
	return installationIDFromAuthorizationRef(record.AuthorizationRef)
}

func installationIDFromAuthorizationRef(value string) (string, bool, error) {
	const prefix = "xinsight://connector-installations/"
	if !strings.HasPrefix(value, prefix) {
		return "", false, nil
	}
	installationID := strings.TrimPrefix(value, prefix)
	expected, err := InstallationAuthorizationRef(installationID)
	if err != nil || expected != value {
		return "", false, fmt.Errorf("%w: installation authorization identity", ErrConflict)
	}
	return installationID, true, nil
}

func activeSummaryMatchesRecord(active ActiveStateSummary, record InstallationEpochRecord) bool {
	return active.StateID == record.RecordID && active.Binding == record.Binding && active.Epoch == record.Epoch &&
		active.SourceInstance == record.SourceInstance && active.CursorSHA256 == record.CursorSHA256 &&
		active.HighWatermarkSHA256 == record.HighWatermarkSHA256 && sha256Pattern.MatchString(active.StateSHA256) &&
		active.PagesCommitted >= 0 && active.ItemsReceived >= 0 && active.ItemsCommitted >= 0 &&
		active.DuplicateNoop >= 0 && active.ConsecutiveFailures >= 0 &&
		active.ItemsCommitted+active.DuplicateNoop == active.ItemsReceived
}

func installationActiveMatches(
	active ActiveStateSummary,
	record InstallationEpochRecord,
	input InstallationEpochInput,
	authorizationRef string,
) bool {
	return record.AuthorizationRef == authorizationRef && record.SourceInstance == input.SourceInstance &&
		record.CursorSHA256 == hashText(input.Cursor) && record.HighWatermarkSHA256 == hashText(input.HighWatermark) &&
		active.Binding == input.Binding && active.SourceInstance == input.SourceInstance &&
		installationCommittedStateMatches(active, input, active.Epoch)
}

func rejectInstallationIdentityReuse(record InstallationEpochRecord, input InstallationEpochInput) error {
	installationID, installationBound, err := installationIDFromAuthorizationRef(record.AuthorizationRef)
	if err != nil {
		return err
	}
	if installationBound && installationID == input.InstallationID {
		return fmt.Errorf("%w: installation identity was already used", ErrConflict)
	}
	if installationBound && strings.EqualFold(installationID, input.InstallationID) {
		return fmt.Errorf("%w: installation identity case collision", ErrConflict)
	}
	if record.SourceInstance == input.SourceInstance {
		return fmt.Errorf("%w: Source identity belongs to another installation", ErrConflict)
	}
	if strings.EqualFold(record.SourceInstance, input.SourceInstance) {
		return fmt.Errorf("%w: Source identity case collision", ErrConflict)
	}
	return nil
}

func installationCommittedStateMatches(active ActiveStateSummary, input InstallationEpochInput, epoch int64) bool {
	return active.StateID != "" && active.Binding == input.Binding && active.Epoch == epoch &&
		active.EpochOperationID == "" && active.SourceInstance == input.SourceInstance &&
		active.CursorSHA256 == hashText(input.Cursor) && active.HighWatermarkSHA256 == hashText(input.HighWatermark) &&
		active.WorkerStatus == "inactive" && active.LastErrorCode == "" &&
		active.PagesCommitted == 0 && active.ItemsReceived == 0 && active.ItemsCommitted == 0 &&
		active.DuplicateNoop == 0 && active.ConsecutiveFailures == 0 && active.LastPollSucceededAt == nil &&
		sha256Pattern.MatchString(active.StateSHA256)
}

func sealInstallationEpochAuthority(authority InstallationEpochAuthority) (InstallationEpochAuthority, error) {
	authority.AuthoritySHA256 = ""
	digest, err := hashJSON(authority)
	if err != nil {
		return InstallationEpochAuthority{}, err
	}
	authority.AuthoritySHA256 = digest
	return authority, nil
}
