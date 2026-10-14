package openinferenceoffline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/foigent/x-insight/internal/adapters/openinference"
	"github.com/foigent/x-insight/internal/connector"
)

type ImportRequest struct {
	OperationID      string
	BundleID         string
	ProducerInstance string
	TenantRef        *string
	OrgRef           string
	ProjectRef       string
}

type ItemProjector interface {
	Binding() connector.SourceNeutralManifestBinding
	Project(
		context.Context,
		openinference.SanitizedRecord,
		openinference.MappingContext,
		connector.ProjectionContext,
	) (connector.NormalizedItem, *connector.ConnectorError)
}

type RegistryPort interface {
	EnsureRegistry(context.Context, string) error
}

type ImportOperationStore interface {
	WithOperation(
		context.Context,
		OperationBinding,
		func(context.Context, *connector.SourceNeutralOperationalReceipt) (
			connector.SourceNeutralOperationalReceipt,
			error,
		),
	) (connector.SourceNeutralOperationalReceipt, error)
}

type Importer struct {
	projector  ItemProjector
	registry   RegistryPort
	committer  connector.EvidenceCommitPort
	operations ImportOperationStore
	now        func() time.Time
}

func NewImporter(
	projector ItemProjector,
	registry RegistryPort,
	committer connector.EvidenceCommitPort,
	operations ImportOperationStore,
	now func() time.Time,
) (*Importer, error) {
	if projector == nil || registry == nil || committer == nil || operations == nil {
		return nil, errors.New("bounded-offline importer dependencies are required")
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	binding := projector.Binding()
	if err := binding.Validate(); err != nil ||
		binding.Connector.ConnectorID != openinference.ConnectorID ||
		binding.Connector.ConnectorVersion != openinference.ConnectorVersion ||
		binding.Projection.SourceSystemID != openinference.SourceSystemID ||
		binding.Projection.AdapterManifestID != openinference.AdapterManifestID {
		return nil, errors.New("bounded-offline importer binding is invalid")
	}
	return &Importer{
		projector:  projector,
		registry:   registry,
		committer:  committer,
		operations: operations,
		now:        now,
	}, nil
}

func (importer *Importer) ImportBundle(
	ctx context.Context,
	payload []byte,
	request ImportRequest,
) (connector.SourceNeutralOperationalReceipt, error) {
	if importer == nil || importer.projector == nil || importer.registry == nil ||
		importer.committer == nil || importer.operations == nil || importer.now == nil {
		return connector.SourceNeutralOperationalReceipt{}, errors.New(
			"bounded-offline importer is unavailable",
		)
	}
	if err := ctx.Err(); err != nil {
		return connector.SourceNeutralOperationalReceipt{}, err
	}
	binding := importer.projector.Binding()
	bundleDigest := sha256.Sum256(payload)
	bundleSHA256 := "sha256:" + hex.EncodeToString(bundleDigest[:])
	operationBinding := bindImportOperation(binding, request, bundleSHA256)
	preparedAt := exactMicrosecond(importer.now())
	if _, err := connector.ProjectBoundedOfflineOperationalReceipt(
		binding,
		connector.SourceNeutralOperationalReceiptInput{
			OperationID:      request.OperationID,
			ProducerInstance: request.ProducerInstance,
			Operation:        connector.OperationImportBundle,
			Status:           "accepted",
			BundleID:         request.BundleID,
			BundleSHA256:     bundleSHA256,
			ObservedAt:       preparedAt,
		},
	); err != nil {
		return connector.SourceNeutralOperationalReceipt{}, fmt.Errorf(
			"validate bounded-offline import identity: %w",
			err,
		)
	}
	return importer.operations.WithOperation(
		ctx,
		operationBinding,
		func(
			operationContext context.Context,
			existing *connector.SourceNeutralOperationalReceipt,
		) (connector.SourceNeutralOperationalReceipt, error) {
			if existing != nil {
				if err := validateReplayReceipt(
					*existing,
					binding,
					request,
					bundleSHA256,
				); err != nil {
					return connector.SourceNeutralOperationalReceipt{}, err
				}
				return *existing, nil
			}
			batch, err := openinference.SanitizeOTLPExport(payload)
			if err != nil {
				return connector.SourceNeutralOperationalReceipt{}, fmt.Errorf(
					"sanitize bounded OpenInference bundle: %w",
					err,
				)
			}
			mapping := openinference.MappingContext{
				ProducerInstance: request.ProducerInstance,
				TenantRef:        request.TenantRef,
				OrgRef:           request.OrgRef,
				ProjectRef:       request.ProjectRef,
			}
			items := make([]connector.NormalizedItem, 0, len(batch.Records))
			for _, record := range batch.Records {
				item, connectorErr := importer.projector.Project(
					operationContext,
					record,
					mapping,
					connector.ProjectionContext{
						ObservedAt: preparedAt,
						IngestedAt: preparedAt,
					},
				)
				if connectorErr != nil {
					return connector.SourceNeutralOperationalReceipt{}, connectorErr
				}
				items = append(items, item)
			}
			if err := importer.registry.EnsureRegistry(
				operationContext,
				request.ProducerInstance,
			); err != nil {
				return connector.SourceNeutralOperationalReceipt{}, fmt.Errorf(
					"ensure OpenInference product registry: %w",
					err,
				)
			}
			counts := connector.ReceiptCounts{Received: int64(len(items))}
			for _, item := range items {
				if err := operationContext.Err(); err != nil {
					return connector.SourceNeutralOperationalReceipt{}, err
				}
				convergence, connectorErr := importer.committer.CommitItem(
					operationContext,
					item,
				)
				if connectorErr != nil {
					return connector.SourceNeutralOperationalReceipt{}, connectorErr
				}
				switch convergence {
				case connector.ConvergenceInserted:
					counts.Inserted++
				case connector.ConvergenceDuplicateNoop:
					counts.DuplicateNoop++
				default:
					return connector.SourceNeutralOperationalReceipt{}, errors.New(
						"OpenInference Evidence item returned an invalid convergence",
					)
				}
			}
			return connector.ProjectBoundedOfflineOperationalReceipt(
				binding,
				connector.SourceNeutralOperationalReceiptInput{
					OperationID:      request.OperationID,
					ProducerInstance: request.ProducerInstance,
					Operation:        connector.OperationImportBundle,
					Status:           "completed",
					Counts:           counts,
					BundleID:         request.BundleID,
					BundleSHA256:     bundleSHA256,
					ObservedAt:       exactMicrosecond(importer.now()),
				},
			)
		},
	)
}

func validateReplayReceipt(
	receipt connector.SourceNeutralOperationalReceipt,
	binding connector.SourceNeutralManifestBinding,
	request ImportRequest,
	bundleSHA256 string,
) error {
	if err := receipt.Validate(); err != nil ||
		receipt.ConnectorID != binding.Connector.ConnectorID ||
		receipt.OperationID != request.OperationID ||
		receipt.ProducerInstance != request.ProducerInstance ||
		receipt.DeliveryMode != connector.DeliveryModeBoundedOffline ||
		receipt.Operation != connector.OperationImportBundle ||
		receipt.Status != "completed" ||
		receipt.DeliveryState.Mode != connector.DeliveryModeBoundedOffline ||
		receipt.DeliveryState.BundleID != request.BundleID ||
		receipt.DeliveryState.BundleSHA256 != bundleSHA256 {
		return ErrOperationConflict
	}
	return nil
}

func exactMicrosecond(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}
