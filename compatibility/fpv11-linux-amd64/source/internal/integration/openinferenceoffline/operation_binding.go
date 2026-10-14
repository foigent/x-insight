package openinferenceoffline

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/integration/boundedoffline"
)

const operationBindingVersion = "xinsight.openinference-import-operation.v1"

// OperationBinding remains an OpenInference API alias while the durable store
// is shared by product-local bounded-offline consumers.
type OperationBinding = boundedoffline.OperationBinding

// ErrOperationConflict remains the exact OpenInference error identity.
var ErrOperationConflict = boundedoffline.ErrOperationConflict

func bindImportOperation(
	binding connector.SourceNeutralManifestBinding,
	request ImportRequest,
	bundleSHA256 string,
) OperationBinding {
	digest := sha256.New()
	writeBindingPart(digest, "binding_version", operationBindingVersion)
	writeBindingPart(digest, "operation", connector.OperationImportBundle)
	writeBindingPart(digest, "operation_id", request.OperationID)
	writeBindingPart(digest, "bundle_id", request.BundleID)
	writeBindingPart(digest, "bundle_sha256", bundleSHA256)
	writeBindingPart(digest, "producer_instance", request.ProducerInstance)
	if request.TenantRef == nil {
		writeBindingPart(digest, "tenant_ref_present", "false")
		writeBindingPart(digest, "tenant_ref", "")
	} else {
		writeBindingPart(digest, "tenant_ref_present", "true")
		writeBindingPart(digest, "tenant_ref", *request.TenantRef)
	}
	writeBindingPart(digest, "org_ref", request.OrgRef)
	writeBindingPart(digest, "project_ref", request.ProjectRef)
	writeBindingPart(digest, "protocol_version", binding.Protocol.Version)
	writeBindingPart(digest, "protocol_tag", binding.Protocol.Tag)
	writeBindingPart(digest, "protocol_commit", binding.Protocol.Commit)
	writeBindingPart(digest, "protocol_release_digest", binding.Protocol.ReleaseDigest)
	writeBindingPart(digest, "connector_id", binding.Connector.ConnectorID)
	writeBindingPart(digest, "connector_version", binding.Connector.ConnectorVersion)
	writeBindingPart(digest, "source_schema_sha256", binding.Connector.SourceSchemaSHA256)
	writeBindingPart(digest, "projection_profile", binding.Projection.ProfileID)
	writeBindingPart(digest, "source_system_id", binding.Projection.SourceSystemID)
	writeBindingPart(digest, "adapter_manifest_id", binding.Projection.AdapterManifestID)
	writeBindingPart(digest, "reference_manifest_sha256", binding.ReferenceManifest.SHA256)
	writeBindingPart(digest, "source_to_sanitized_sha256", binding.Mappings.SourceToSanitized.SHA256)
	writeBindingPart(digest, "sanitized_to_event_sha256", binding.Mappings.SanitizedToEvent.SHA256)
	writeBindingPart(digest, "event_to_m0_sha256", binding.Mappings.EventToM0.SHA256)
	return OperationBinding{
		ConnectorID:   binding.Connector.ConnectorID,
		OperationID:   request.OperationID,
		RequestSHA256: "sha256:" + hex.EncodeToString(digest.Sum(nil)),
	}
}

func writeBindingPart(digest hash.Hash, label string, value string) {
	var length [8]byte
	for _, part := range []string{label, value} {
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write([]byte(part))
	}
}
