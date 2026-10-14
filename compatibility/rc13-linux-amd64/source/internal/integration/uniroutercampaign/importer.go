package uniroutercampaign

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/boundedoffline"
	"github.com/foigent/x-insight/internal/integrationcontrol"
	"github.com/foigent/x-insight/internal/investigation"
)

var campaignConnectorIDPattern = regexp.MustCompile(
	`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_-]*)+$`,
)

// ComponentBinding is the X-Insight-owned projection target for one immutable
// producer contract. ConnectorID is supplied by that contract binding; the M0
// source and adapter identities remain the existing execution-evidence V2
// product registrations.
type ComponentBinding struct {
	ConnectorID       string
	SourceSystemID    string
	AdapterManifestID string
}

type ImportRequest struct {
	OperationID  string
	Verification unirouter.CampaignPackageVerificationRequest
}

type EvidencePersister interface {
	PersistEventsWithProvenance(
		context.Context,
		[]map[string]any,
		investigation.ImportProvenance,
	) (db.PersistenceReport, error)
}

type ImportOperationStore interface {
	WithOperation(
		context.Context,
		boundedoffline.OperationBinding,
		func(context.Context, *connector.SourceNeutralOperationalReceipt) (
			connector.SourceNeutralOperationalReceipt,
			error,
		),
	) (connector.SourceNeutralOperationalReceipt, error)
}

type preparedMember struct {
	inventory  unirouter.CampaignPackageMemberInventory
	report     unirouter.ExecutionEvidenceImportReport
	outcome    unirouter.CampaignPackageMemberImportOutcome
	provenance investigation.ImportProvenance
}

// PreparedImport is sealed by this package. It permits a command entry point
// to finish package signature and member validation before it loads a database
// DSN or constructs any persistence dependency.
type PreparedImport struct {
	binding           ComponentBinding
	request           ImportRequest
	verified          unirouter.VerifiedCampaignPackage
	authorization     integrationcontrol.N1AuthorizationVerification
	authorizationSeal string
	producerInstance  string
	members           []preparedMember
	operationBinding  boundedoffline.OperationBinding
}

type Importer struct {
	persister  EvidencePersister
	operations ImportOperationStore
	now        func() time.Time
}

func NewImporter(
	persister EvidencePersister,
	operations ImportOperationStore,
	now func() time.Time,
) (*Importer, error) {
	if persister == nil || operations == nil {
		return nil, errors.New("UniRouter campaign package importer dependencies are required")
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Importer{persister: persister, operations: operations, now: now}, nil
}

// PrepareImport is the only exported preparation entry point. It requires one
// sealed N1 M/A authority before the producer adapter can parse untrusted
// bytes, and it completes all package/member checks before any DB dependency.
func PrepareImport(
	packageBytes []byte,
	request ImportRequest,
	binding ComponentBinding,
	evidenceSchemaPath string,
	adapter unirouter.CampaignPackageContractAdapter,
	authorization integrationcontrol.N1AuthorizationVerification,
) (PreparedImport, error) {
	if err := validateComponentBinding(binding); err != nil {
		return PreparedImport{}, err
	}
	if !validCampaignOperationText(request.OperationID, 128) {
		return PreparedImport{}, errors.New("campaign package import operation identity is invalid")
	}
	if !validCampaignOperationText(evidenceSchemaPath, 2048) {
		return PreparedImport{}, errors.New("campaign package Evidence schema path is invalid")
	}
	verified, err := integrationcontrol.VerifyPreauthorizedCampaignPackage(
		packageBytes,
		request.Verification,
		adapter,
		authorization,
	)
	if err != nil {
		return PreparedImport{}, err
	}
	prepared, err := prepareVerifiedImport(request, binding, evidenceSchemaPath, verified)
	if err != nil {
		return PreparedImport{}, err
	}
	prepared.authorization = cloneCampaignAuthorization(authorization)
	prepared.authorizationSeal, err = prepared.expectedAuthorizationSeal()
	if err != nil {
		return PreparedImport{}, err
	}
	return prepared, nil
}

func prepareVerifiedImport(
	request ImportRequest,
	binding ComponentBinding,
	evidenceSchemaPath string,
	verified unirouter.VerifiedCampaignPackage,
) (PreparedImport, error) {
	if err := validateComponentBinding(binding); err != nil {
		return PreparedImport{}, err
	}
	if !validCampaignOperationText(request.OperationID, 128) ||
		!validCampaignOperationText(evidenceSchemaPath, 2048) {
		return PreparedImport{}, errors.New("campaign package import preparation identity is invalid")
	}
	projection := verified.Projection()
	if !validCampaignOperationText(projection.PackageID, 128) ||
		!validCampaignOperationText(projection.SourceInstance, 128) {
		return PreparedImport{}, errors.New("campaign package ID cannot be represented by the durable operational receipt")
	}
	members := make([]preparedMember, 0, len(projection.Members))
	seenEventIdentities := make(map[string]string)
	for _, inventory := range projection.Members {
		payload, exists := verified.MemberPayload(inventory.MemberID)
		if !exists {
			return PreparedImport{}, errors.New("verified campaign package lost a manifested member")
		}
		report, err := unirouter.ImportExecutionEvidenceBundleBytesWithContext(
			payload,
			evidenceSchemaPath,
			unirouter.SnapshotImportOptions{
				SourceSystemID:    binding.SourceSystemID,
				AdapterManifestID: binding.AdapterManifestID,
				EmitEvents:        true,
				RerunCheck:        false,
			},
			unirouter.ExecutionEvidenceImportContext{
				SelectionMode: unirouter.ExecutionEvidenceSelectionModeSignedCampaignPackage,
			},
		)
		if err != nil {
			return PreparedImport{}, fmt.Errorf(
				"validate campaign package member %q: %w",
				inventory.MemberID,
				err,
			)
		}
		if report.Bundle.SourceInstance != projection.SourceInstance ||
			report.ArtifactSHA256 != inventory.SHA256 ||
			report.EventsTotal < 1 || report.EventsTotal != len(report.Events) ||
			report.Accepted != report.EventsTotal || report.DuplicateNoop != 0 ||
			report.Quarantined != 0 || report.Rejected != 0 {
			return PreparedImport{}, errors.New(
				"campaign package member projection contradicts its frozen Source or accepted event set",
			)
		}
		for _, result := range report.Results {
			if priorMember, duplicate := seenEventIdentities[result.IdempotencyKey]; duplicate {
				return PreparedImport{}, fmt.Errorf(
					"campaign package members %q and %q repeat one M0 idempotency identity",
					priorMember,
					inventory.MemberID,
				)
			}
			seenEventIdentities[result.IdempotencyKey] = inventory.MemberID
		}
		outcome, err := unirouter.CampaignPackageMemberCompleteness(
			inventory.MemberID,
			report.Bundle,
			int64(report.EventsTotal),
		)
		if err != nil {
			return PreparedImport{}, err
		}
		provenance, err := campaignMemberProvenance(
			binding,
			inventory,
			report.Bundle,
		)
		if err != nil {
			return PreparedImport{}, err
		}
		members = append(members, preparedMember{
			inventory:  inventory,
			report:     report,
			outcome:    outcome,
			provenance: provenance,
		})
	}
	operationBinding, err := bindImportOperation(binding, request, projection, members)
	if err != nil {
		return PreparedImport{}, err
	}
	prepared := PreparedImport{
		binding:          binding,
		request:          request,
		verified:         verified,
		producerInstance: projection.SourceInstance,
		members:          members,
		operationBinding: operationBinding,
	}
	if err := prepared.validateSeal(); err != nil {
		return PreparedImport{}, err
	}
	return prepared, nil
}

func (importer *Importer) ImportPrepared(
	ctx context.Context,
	prepared PreparedImport,
) (unirouter.CampaignPackageImportReceipt, error) {
	if importer == nil || importer.persister == nil || importer.operations == nil || importer.now == nil {
		return unirouter.CampaignPackageImportReceipt{}, errors.New(
			"UniRouter campaign package importer is unavailable",
		)
	}
	if err := ctx.Err(); err != nil {
		return unirouter.CampaignPackageImportReceipt{}, err
	}
	if err := prepared.validateSeal(); err != nil {
		return unirouter.CampaignPackageImportReceipt{}, err
	}
	expectedEvents := int64(0)
	outcomes := make([]unirouter.CampaignPackageMemberImportOutcome, 0, len(prepared.members))
	for _, member := range prepared.members {
		expectedEvents += int64(member.report.EventsTotal)
		outcomes = append(outcomes, member.outcome)
	}
	// Validate every durable receipt identity before the operation store can
	// claim an operation row or Evidence can be written.
	if _, err := completedOperationalReceipt(
		prepared,
		connector.ReceiptCounts{Received: expectedEvents, Inserted: expectedEvents},
		time.Unix(0, 0).UTC(),
	); err != nil {
		return unirouter.CampaignPackageImportReceipt{}, err
	}
	operationalReceipt, err := importer.operations.WithOperation(
		ctx,
		prepared.operationBinding,
		func(
			operationContext context.Context,
			existing *connector.SourceNeutralOperationalReceipt,
		) (connector.SourceNeutralOperationalReceipt, error) {
			if existing != nil {
				if err := validateReplayReceipt(*existing, prepared); err != nil {
					return connector.SourceNeutralOperationalReceipt{}, err
				}
				return *existing, nil
			}
			observedAt := importer.now().UTC().Truncate(time.Microsecond)
			if observedAt.IsZero() {
				return connector.SourceNeutralOperationalReceipt{}, errors.New(
					"campaign package operational receipt time is required",
				)
			}
			counts := connector.ReceiptCounts{Received: expectedEvents}
			for _, member := range prepared.members {
				if err := operationContext.Err(); err != nil {
					return connector.SourceNeutralOperationalReceipt{}, err
				}
				report, err := importer.persister.PersistEventsWithProvenance(
					operationContext,
					member.report.Events,
					member.provenance,
				)
				if err != nil {
					return connector.SourceNeutralOperationalReceipt{}, fmt.Errorf(
						"persist campaign package member %q: %w",
						member.inventory.MemberID,
						err,
					)
				}
				if report.EventsTotal != member.report.EventsTotal ||
					report.IdempotencyConflict != 0 || report.Rejected != 0 ||
					report.Inserted+report.DuplicateNoop != member.report.EventsTotal {
					return connector.SourceNeutralOperationalReceipt{}, errors.New(
						"campaign package member did not converge as inserted or duplicate_noop",
					)
				}
				counts.Inserted += int64(report.Inserted)
				counts.DuplicateNoop += int64(report.DuplicateNoop)
			}
			return completedOperationalReceipt(prepared, counts, observedAt)
		},
	)
	if err != nil {
		return unirouter.CampaignPackageImportReceipt{}, err
	}
	return unirouter.ProjectCampaignPackageImportReceipt(
		unirouter.CampaignPackageImportReceiptInput{
			OperationID:        prepared.request.OperationID,
			Package:            prepared.verified,
			Members:            outcomes,
			OperationalReceipt: operationalReceipt,
		},
	)
}

// ImportPreparedVerified preserves ImportPrepared as the plain typed-receipt
// compatibility entry point, then performs one completed operation-store
// readback before returning authority to construct a terminal projection.
func (importer *Importer) ImportPreparedVerified(
	ctx context.Context,
	prepared PreparedImport,
) (
	unirouter.CampaignPackageImportReceipt,
	VerifiedCampaignImportAuthority,
	error,
) {
	if err := prepared.validateAuthorizedSeal(); err != nil {
		return unirouter.CampaignPackageImportReceipt{}, VerifiedCampaignImportAuthority{}, err
	}
	receipt, err := importer.ImportPrepared(ctx, prepared)
	if err != nil {
		return unirouter.CampaignPackageImportReceipt{}, VerifiedCampaignImportAuthority{}, err
	}
	authority, err := sealVerifiedCampaignImportAuthority(
		ctx,
		importer.operations,
		prepared,
		receipt,
	)
	if err != nil {
		return unirouter.CampaignPackageImportReceipt{}, VerifiedCampaignImportAuthority{}, err
	}
	return receipt, authority, nil
}

func (prepared PreparedImport) validateSeal() error {
	if err := validateComponentBinding(prepared.binding); err != nil ||
		!validCampaignOperationText(prepared.request.OperationID, 128) ||
		!validCampaignOperationText(prepared.producerInstance, 128) ||
		len(prepared.members) == 0 {
		return errors.New("prepared campaign package import is invalid")
	}
	projection := prepared.verified.Projection()
	if projection.SourceInstance != prepared.producerInstance ||
		len(projection.Members) != len(prepared.members) {
		return errors.New("prepared campaign package import member closure is incomplete")
	}
	rebound, err := bindImportOperation(
		prepared.binding,
		prepared.request,
		projection,
		prepared.members,
	)
	if err != nil || rebound != prepared.operationBinding {
		return errors.New("prepared campaign package import seal is invalid")
	}
	return nil
}

func completedOperationalReceipt(
	prepared PreparedImport,
	counts connector.ReceiptCounts,
	observedAt time.Time,
) (connector.SourceNeutralOperationalReceipt, error) {
	projection := prepared.verified.Projection()
	receipt := connector.SourceNeutralOperationalReceipt{
		ReceiptVersion:   connector.SourceNeutralReceiptVersionV2,
		OperationID:      prepared.request.OperationID,
		ConnectorID:      prepared.binding.ConnectorID,
		ProducerInstance: prepared.producerInstance,
		DeliveryMode:     connector.DeliveryModeBoundedOffline,
		Operation:        connector.OperationImportBundle,
		Status:           "completed",
		Counts:           counts,
		DeliveryState: connector.BoundedOfflineDeliveryState{
			Mode:         connector.DeliveryModeBoundedOffline,
			BundleID:     projection.PackageID,
			BundleSHA256: projection.PackageSHA256,
		},
		ObservedAt: observedAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
	}
	if observedAt.IsZero() || observedAt.Nanosecond()%int(time.Microsecond) != 0 {
		return connector.SourceNeutralOperationalReceipt{}, errors.New(
			"campaign package operational receipt time must have exact microsecond precision",
		)
	}
	if err := receipt.Validate(); err != nil {
		return connector.SourceNeutralOperationalReceipt{}, err
	}
	return receipt, nil
}

func validateReplayReceipt(
	receipt connector.SourceNeutralOperationalReceipt,
	prepared PreparedImport,
) error {
	projection := prepared.verified.Projection()
	if err := receipt.Validate(); err != nil ||
		receipt.ConnectorID != prepared.binding.ConnectorID ||
		receipt.OperationID != prepared.request.OperationID ||
		receipt.ProducerInstance != prepared.producerInstance ||
		receipt.DeliveryMode != connector.DeliveryModeBoundedOffline ||
		receipt.Operation != connector.OperationImportBundle ||
		receipt.Status != "completed" || receipt.Error != nil ||
		receipt.DeliveryState.Mode != connector.DeliveryModeBoundedOffline ||
		receipt.DeliveryState.BundleID != projection.PackageID ||
		receipt.DeliveryState.BundleSHA256 != projection.PackageSHA256 {
		return boundedoffline.ErrOperationConflict
	}
	return nil
}

func campaignMemberProvenance(
	binding ComponentBinding,
	inventory unirouter.CampaignPackageMemberInventory,
	bundle unirouter.ExecutionEvidenceBundle,
) (investigation.ImportProvenance, error) {
	terminalAt, err := time.Parse(time.RFC3339Nano, bundle.ClosedAt)
	if err != nil {
		return investigation.ImportProvenance{}, fmt.Errorf(
			"campaign package member closed_at: %w",
			err,
		)
	}
	terminalAt = terminalAt.UTC()
	return investigation.ImportProvenance{
		SourceSystemID:      binding.SourceSystemID,
		AdapterManifestID:   binding.AdapterManifestID,
		IntegrationMode:     investigation.IntegrationBoundedOffline,
		ProofClass:          investigation.ProofOperatorApproved,
		AssuranceMode:       investigation.AssuranceBoundedOffline,
		SourcePayloadSHA256: inventory.SHA256,
		SourceTerminalAt:    &terminalAt,
		SourceRevision:      unirouter.ExecutionEvidenceProfileID + "@" + unirouter.ExecutionEvidenceProfileManifestSHA256,
		ImportedRevision:    inventory.SHA256,
		TimelineComplete:    bundle.Completeness.SourceProfileComplete,
	}, nil
}

func validateComponentBinding(binding ComponentBinding) error {
	if len(binding.ConnectorID) > 160 ||
		!campaignConnectorIDPattern.MatchString(binding.ConnectorID) ||
		binding.SourceSystemID != unirouter.DefaultSourceSystemID ||
		binding.AdapterManifestID != unirouter.DefaultExecutionEvidenceAdapterID {
		return errors.New("UniRouter campaign package component binding is invalid")
	}
	return nil
}

func validCampaignOperationText(value string, maximum int) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 1 ||
		utf8.RuneCountInString(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
