package uniroutercampaign

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"strconv"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/integration/boundedoffline"
)

const operationBindingVersion = "xinsight.unirouter-campaign-package-import-operation.v1"

func bindImportOperation(
	binding ComponentBinding,
	request ImportRequest,
	projection unirouter.CampaignPackageProjection,
	members []preparedMember,
) (boundedoffline.OperationBinding, error) {
	digest := sha256.New()
	writeBindingPart(digest, "binding_version", operationBindingVersion)
	writeBindingPart(digest, "operation", connector.OperationImportBundle)
	writeBindingPart(digest, "operation_id", request.OperationID)
	writeBindingPart(digest, "connector_id", binding.ConnectorID)
	writeBindingPart(digest, "producer_instance", projection.SourceInstance)
	writeBindingPart(digest, "source_system_id", binding.SourceSystemID)
	writeBindingPart(digest, "adapter_manifest_id", binding.AdapterManifestID)
	writeBindingPart(digest, "contract_id", projection.Contract.ID)
	writeBindingPart(digest, "contract_sha256", projection.Contract.SHA256)
	writeBindingPart(digest, "package_id", projection.PackageID)
	writeBindingPart(digest, "package_sha256", projection.PackageSHA256)
	writeBindingPart(digest, "package_size_bytes", strconv.FormatInt(projection.PackageSizeBytes, 10))
	writeBindingPart(digest, "manifest_sha256", projection.ManifestSHA256)
	writeBindingPart(digest, "package_scope_complete", strconv.FormatBool(projection.PackageScopeComplete))
	writeBindingPart(digest, "source_binding_sha256", projection.SourceBindingSHA256)
	writeBindingPart(digest, "authorization_manifest_sha256", projection.AuthorizationManifestSHA256)
	writeBindingPart(digest, "authorization_sha256", projection.AuthorizationSHA256)
	writeBindingPart(digest, "authorization_signature_sha256", projection.AuthorizationSignatureSHA256)
	writeBindingPart(digest, "signature_purpose", projection.Signature.Purpose)
	writeBindingPart(digest, "signature_domain", projection.Signature.Domain)
	writeBindingPart(digest, "signature_algorithm", projection.Signature.Algorithm)
	writeBindingPart(digest, "signature_signing_key_ref", projection.Signature.SigningKeyRef)
	writeBindingPart(digest, "signature_signer_public_key_id", projection.Signature.SignerPublicKeyID)
	writeBindingPart(digest, "signature_trust_set_sha256", projection.Signature.TrustSetSHA256)
	writeBindingPart(digest, "member_count", strconv.Itoa(len(members)))
	for index, member := range members {
		prefix := "member[" + strconv.Itoa(index) + "]."
		writeBindingPart(digest, prefix+"id", member.inventory.MemberID)
		writeBindingPart(digest, prefix+"path", member.inventory.Path)
		writeBindingPart(digest, prefix+"sha256", member.inventory.SHA256)
		writeBindingPart(digest, prefix+"size_bytes", strconv.FormatInt(member.inventory.SizeBytes, 10))
		writeBindingPart(digest, prefix+"source_instance", member.report.Bundle.SourceInstance)
		writeBindingPart(digest, prefix+"request_id", member.report.Bundle.RequestID)
		writeBindingPart(digest, prefix+"events_expected", strconv.Itoa(member.report.EventsTotal))
		writeBindingPart(digest, prefix+"source_profile_state", member.outcome.SourceProfileState)
		writeBindingPart(digest, prefix+"source_profile_complete", strconv.FormatBool(member.outcome.SourceProfileComplete))
		writeBindingPart(digest, prefix+"runtime_close_marker_available", strconv.FormatBool(member.outcome.RuntimeCloseMarkerAvailable))
		writeBindingPart(digest, prefix+"capture_gap_count", strconv.FormatInt(member.outcome.CaptureGapCount, 10))
		for gapIndex, gapCode := range member.outcome.GapCodes {
			writeBindingPart(
				digest,
				prefix+"gap_code["+strconv.Itoa(gapIndex)+"]",
				gapCode,
			)
		}
	}
	return boundedoffline.OperationBinding{
		ConnectorID:   binding.ConnectorID,
		OperationID:   request.OperationID,
		RequestSHA256: "sha256:" + hex.EncodeToString(digest.Sum(nil)),
	}, nil
}

func writeBindingPart(digest hash.Hash, label string, value string) {
	var length [8]byte
	for _, part := range []string{label, value} {
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write([]byte(part))
	}
}
