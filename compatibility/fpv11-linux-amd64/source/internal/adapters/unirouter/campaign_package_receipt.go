package unirouter

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"

	"github.com/foigent/x-insight/internal/connector"
)

const (
	CampaignPackageImportReceiptVersion  = "xinsight.unirouter-campaign-package-import-receipt.v1"
	RuntimeCloseMarkerUnavailableGapCode = "runtime_close_marker_unavailable"
)

var campaignPackageGapCodePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,119}$`)

type CampaignPackageMemberImportOutcome struct {
	MemberID                    string
	SourceProfileState          string
	SourceProfileComplete       bool
	RuntimeCloseMarkerAvailable bool
	CaptureGapCount             int64
	GapCodes                    []string
	Received                    int64
}

type CampaignPackageMemberImportReceipt struct {
	MemberID                    string   `json:"member_id"`
	Path                        string   `json:"path"`
	SHA256                      string   `json:"sha256"`
	SizeBytes                   int64    `json:"size_bytes"`
	SourceProfileState          string   `json:"source_profile_state"`
	SourceProfileComplete       bool     `json:"source_profile_complete"`
	RuntimeCloseMarkerAvailable bool     `json:"runtime_close_marker_available"`
	CaptureGapCount             int64    `json:"capture_gap_count"`
	GapCodes                    []string `json:"gap_codes"`
	Received                    int64    `json:"received"`
}

type CampaignPackageImportCounts struct {
	Members                      int64 `json:"members"`
	SourceProfileCompleteMembers int64 `json:"source_profile_complete_members"`
	SourceProfilePartialMembers  int64 `json:"source_profile_partial_members"`
	CaptureGaps                  int64 `json:"capture_gaps"`
	EvidenceItemsExpected        int64 `json:"evidence_items_expected"`
}

type CampaignPackageImportReceipt struct {
	ReceiptVersion                  string                                    `json:"receipt_version"`
	OperationID                     string                                    `json:"operation_id"`
	Status                          string                                    `json:"status"`
	Contract                        CampaignPackageContractIdentity           `json:"contract"`
	PackageID                       string                                    `json:"package_id"`
	PackageSHA256                   string                                    `json:"package_sha256"`
	PackageSizeBytes                int64                                     `json:"package_size_bytes"`
	ManifestSHA256                  string                                    `json:"manifest_sha256"`
	PackageScopeComplete            bool                                      `json:"package_scope_complete"`
	AllMemberSourceProfilesComplete bool                                      `json:"all_member_source_profiles_complete"`
	SourceBindingSHA256             string                                    `json:"source_binding_sha256"`
	AuthorizationManifestSHA256     string                                    `json:"authorization_manifest_sha256"`
	AuthorizationSHA256             string                                    `json:"authorization_sha256"`
	AuthorizationSignatureSHA256    string                                    `json:"authorization_signature_sha256"`
	Signature                       SourceEvidenceSignatureIdentity           `json:"signature"`
	Counts                          CampaignPackageImportCounts               `json:"counts"`
	Members                         []CampaignPackageMemberImportReceipt      `json:"members"`
	OperationalReceipt              connector.SourceNeutralOperationalReceipt `json:"operational_receipt"`
	OperationalReceiptSHA256        string                                    `json:"operational_receipt_sha256"`
}

type CampaignPackageImportReceiptInput struct {
	OperationID        string
	Package            VerifiedCampaignPackage
	Members            []CampaignPackageMemberImportOutcome
	OperationalReceipt connector.SourceNeutralOperationalReceipt
}

func ProjectCampaignPackageImportReceipt(
	input CampaignPackageImportReceiptInput,
) (CampaignPackageImportReceipt, error) {
	if !validCampaignPackageOpaqueText(input.OperationID, 1, 128) {
		return CampaignPackageImportReceipt{}, errors.New("campaign package receipt operation identity is invalid")
	}
	projection := input.Package.Projection()
	if len(projection.Members) == 0 || len(input.Members) != len(projection.Members) {
		return CampaignPackageImportReceipt{}, errors.New("campaign package receipt member outcomes are incomplete")
	}
	outcomes := make(map[string]CampaignPackageMemberImportOutcome, len(input.Members))
	for _, outcome := range input.Members {
		if _, exists := outcomes[outcome.MemberID]; exists {
			return CampaignPackageImportReceipt{}, errors.New("campaign package receipt contains duplicate member outcomes")
		}
		outcomes[outcome.MemberID] = outcome
	}
	inventory := sortedCampaignPackageMemberInventory(projection.Members)
	receipt := CampaignPackageImportReceipt{
		ReceiptVersion:               CampaignPackageImportReceiptVersion,
		OperationID:                  input.OperationID,
		Status:                       "completed",
		Contract:                     projection.Contract,
		PackageID:                    projection.PackageID,
		PackageSHA256:                projection.PackageSHA256,
		PackageSizeBytes:             projection.PackageSizeBytes,
		ManifestSHA256:               projection.ManifestSHA256,
		PackageScopeComplete:         projection.PackageScopeComplete,
		SourceBindingSHA256:          projection.SourceBindingSHA256,
		AuthorizationManifestSHA256:  projection.AuthorizationManifestSHA256,
		AuthorizationSHA256:          projection.AuthorizationSHA256,
		AuthorizationSignatureSHA256: projection.AuthorizationSignatureSHA256,
		Signature:                    projection.Signature,
		OperationalReceipt:           input.OperationalReceipt,
	}
	operationalReceiptSHA256, err := input.OperationalReceipt.SHA256()
	if err != nil {
		return CampaignPackageImportReceipt{}, fmt.Errorf("validate durable operational receipt: %w", err)
	}
	receipt.OperationalReceiptSHA256 = operationalReceiptSHA256
	receipt.Members = make([]CampaignPackageMemberImportReceipt, 0, len(inventory))
	for _, member := range inventory {
		outcome, exists := outcomes[member.MemberID]
		if !exists {
			return CampaignPackageImportReceipt{}, errors.New("campaign package receipt is missing a manifested member outcome")
		}
		delete(outcomes, member.MemberID)
		gapCodes := append([]string(nil), outcome.GapCodes...)
		sort.Strings(gapCodes)
		receipt.Members = append(receipt.Members, CampaignPackageMemberImportReceipt{
			MemberID:                    member.MemberID,
			Path:                        member.Path,
			SHA256:                      member.SHA256,
			SizeBytes:                   member.SizeBytes,
			SourceProfileState:          outcome.SourceProfileState,
			SourceProfileComplete:       outcome.SourceProfileComplete,
			RuntimeCloseMarkerAvailable: outcome.RuntimeCloseMarkerAvailable,
			CaptureGapCount:             outcome.CaptureGapCount,
			GapCodes:                    gapCodes,
			Received:                    outcome.Received,
		})
	}
	if len(outcomes) != 0 {
		return CampaignPackageImportReceipt{}, errors.New("campaign package receipt contains an unmanifested member outcome")
	}
	receipt.Counts = aggregateCampaignPackageReceiptCounts(receipt.Members)
	receipt.AllMemberSourceProfilesComplete =
		receipt.Counts.SourceProfileCompleteMembers == receipt.Counts.Members
	if err := receipt.Validate(); err != nil {
		return CampaignPackageImportReceipt{}, err
	}
	return receipt, nil
}

func CampaignPackageMemberCompleteness(
	memberID string,
	bundle ExecutionEvidenceBundle,
	received int64,
) (CampaignPackageMemberImportOutcome, error) {
	if !validCampaignPackageOpaqueText(memberID, 1, 160) {
		return CampaignPackageMemberImportOutcome{}, errors.New("campaign package member identity is invalid")
	}
	if err := validateExecutionEvidenceBundle(bundle); err != nil {
		return CampaignPackageMemberImportOutcome{}, err
	}
	gapCodes := make([]string, 0, len(bundle.CaptureGaps))
	runtimeCloseMarkerAvailable := true
	for _, gap := range bundle.CaptureGaps {
		gapCodes = append(gapCodes, gap.Code)
		if gap.Code == RuntimeCloseMarkerUnavailableGapCode {
			runtimeCloseMarkerAvailable = false
		}
	}
	gapCodes = uniqueSortedCampaignPackageGapCodes(gapCodes)
	outcome := CampaignPackageMemberImportOutcome{
		MemberID:                    memberID,
		SourceProfileState:          bundle.Completeness.State,
		SourceProfileComplete:       bundle.Completeness.SourceProfileComplete,
		RuntimeCloseMarkerAvailable: runtimeCloseMarkerAvailable,
		CaptureGapCount:             int64(len(bundle.CaptureGaps)),
		GapCodes:                    gapCodes,
		Received:                    received,
	}
	if err := validateCampaignPackageMemberOutcome(outcome); err != nil {
		return CampaignPackageMemberImportOutcome{}, err
	}
	return outcome, nil
}

func (receipt CampaignPackageImportReceipt) Validate() error {
	if receipt.ReceiptVersion != CampaignPackageImportReceiptVersion ||
		!validCampaignPackageOpaqueText(receipt.OperationID, 1, 128) ||
		receipt.Status != "completed" ||
		receipt.Contract.validate() != nil ||
		!validCampaignPackageOpaqueText(receipt.PackageID, 1, 160) ||
		!campaignPackageDigestPattern.MatchString(receipt.PackageSHA256) ||
		receipt.PackageSizeBytes <= 0 || receipt.PackageSizeBytes > DefaultCampaignPackageMaxBytes ||
		!campaignPackageDigestPattern.MatchString(receipt.ManifestSHA256) ||
		!receipt.PackageScopeComplete ||
		!campaignPackageDigestPattern.MatchString(receipt.SourceBindingSHA256) ||
		!campaignPackageDigestPattern.MatchString(receipt.AuthorizationManifestSHA256) ||
		!campaignPackageDigestPattern.MatchString(receipt.AuthorizationSHA256) ||
		!campaignPackageDigestPattern.MatchString(receipt.AuthorizationSignatureSHA256) ||
		!campaignPackageDigestPattern.MatchString(receipt.OperationalReceiptSHA256) {
		return errors.New("campaign package import receipt identity is invalid")
	}
	policy := SourceEvidenceTrustPolicy{
		Purpose:           receipt.Signature.Purpose,
		Domain:            receipt.Signature.Domain,
		Algorithm:         receipt.Signature.Algorithm,
		SigningKeyRef:     receipt.Signature.SigningKeyRef,
		SignerPublicKeyID: receipt.Signature.SignerPublicKeyID,
		TrustSetSHA256:    receipt.Signature.TrustSetSHA256,
	}
	if err := policy.validate(); err != nil {
		return err
	}
	if err := receipt.OperationalReceipt.Validate(); err != nil ||
		receipt.OperationalReceipt.OperationID != receipt.OperationID ||
		receipt.OperationalReceipt.DeliveryMode != connector.DeliveryModeBoundedOffline ||
		receipt.OperationalReceipt.Operation != connector.OperationImportBundle ||
		receipt.OperationalReceipt.Status != "completed" ||
		receipt.OperationalReceipt.DeliveryState.BundleID != receipt.PackageID ||
		receipt.OperationalReceipt.DeliveryState.BundleSHA256 != receipt.PackageSHA256 {
		return errors.New("campaign package receipt does not match its durable operational receipt")
	}
	operationalReceiptSHA256, err := receipt.OperationalReceipt.SHA256()
	if err != nil || operationalReceiptSHA256 != receipt.OperationalReceiptSHA256 {
		return errors.New("campaign package durable operational receipt digest is invalid")
	}
	if len(receipt.Members) == 0 || len(receipt.Members) > DefaultCampaignPackageMaxMembers {
		return errors.New("campaign package import receipt member count is invalid")
	}
	seenIDs := make(map[string]struct{}, len(receipt.Members))
	seenPaths := make(map[string]struct{}, len(receipt.Members))
	previousPath := ""
	for index, member := range receipt.Members {
		if err := validateCampaignPackageMemberReceipt(member); err != nil {
			return fmt.Errorf("campaign package receipt member[%d]: %w", index, err)
		}
		if previousPath != "" && previousPath >= member.Path {
			return errors.New("campaign package receipt members are not in strict path order")
		}
		previousPath = member.Path
		if _, exists := seenIDs[member.MemberID]; exists {
			return errors.New("campaign package receipt contains a duplicate member identity")
		}
		seenIDs[member.MemberID] = struct{}{}
		if _, exists := seenPaths[member.Path]; exists {
			return errors.New("campaign package receipt contains a duplicate member path")
		}
		seenPaths[member.Path] = struct{}{}
	}
	expected := aggregateCampaignPackageReceiptCounts(receipt.Members)
	if receipt.Counts != expected ||
		receipt.AllMemberSourceProfilesComplete !=
			(receipt.Counts.SourceProfileCompleteMembers == receipt.Counts.Members) ||
		receipt.OperationalReceipt.Counts.Received != receipt.Counts.EvidenceItemsExpected {
		return errors.New("campaign package receipt aggregates contradict member evidence")
	}
	return nil
}

func validateCampaignPackageMemberReceipt(member CampaignPackageMemberImportReceipt) error {
	if !validCampaignPackageOpaqueText(member.MemberID, 1, 160) ||
		!validCampaignPackageMemberPath(member.Path) ||
		!campaignPackageDigestPattern.MatchString(member.SHA256) ||
		member.SizeBytes <= 0 || member.SizeBytes > DefaultCampaignPackageMaxMemberBytes {
		return errors.New("member content identity is invalid")
	}
	return validateCampaignPackageMemberOutcome(CampaignPackageMemberImportOutcome{
		MemberID:                    member.MemberID,
		SourceProfileState:          member.SourceProfileState,
		SourceProfileComplete:       member.SourceProfileComplete,
		RuntimeCloseMarkerAvailable: member.RuntimeCloseMarkerAvailable,
		CaptureGapCount:             member.CaptureGapCount,
		GapCodes:                    member.GapCodes,
		Received:                    member.Received,
	})
}

func validateCampaignPackageMemberOutcome(outcome CampaignPackageMemberImportOutcome) error {
	if !validCampaignPackageOpaqueText(outcome.MemberID, 1, 160) ||
		outcome.CaptureGapCount < 0 || outcome.Received < 0 ||
		outcome.CaptureGapCount > MaxExecutionEvidenceGaps ||
		outcome.Received > MaxExecutionEvidenceTraceEvents+MaxExecutionEvidenceCallLogs+MaxExecutionEvidenceGaps ||
		int64(len(outcome.GapCodes)) > outcome.CaptureGapCount {
		return errors.New("member import outcome counts are invalid")
	}
	if !strictSortedUniqueCampaignPackageGapCodes(outcome.GapCodes) {
		return errors.New("member import outcome gap codes are invalid or not canonical")
	}
	hasRuntimeMarkerGap := false
	for _, code := range outcome.GapCodes {
		if code == RuntimeCloseMarkerUnavailableGapCode {
			hasRuntimeMarkerGap = true
		}
	}
	if outcome.SourceProfileComplete {
		if outcome.SourceProfileState != "complete" || outcome.CaptureGapCount != 0 ||
			len(outcome.GapCodes) != 0 || !outcome.RuntimeCloseMarkerAvailable {
			return errors.New("complete member source profile contradicts its gaps or close marker")
		}
	} else if outcome.SourceProfileState != "partial" || outcome.CaptureGapCount < 1 ||
		len(outcome.GapCodes) == 0 ||
		(outcome.RuntimeCloseMarkerAvailable && hasRuntimeMarkerGap) ||
		(!outcome.RuntimeCloseMarkerAvailable && !hasRuntimeMarkerGap) {
		return errors.New("partial member source profile does not preserve its explicit capture gaps")
	}
	return nil
}

func aggregateCampaignPackageReceiptCounts(
	members []CampaignPackageMemberImportReceipt,
) CampaignPackageImportCounts {
	counts := CampaignPackageImportCounts{Members: int64(len(members))}
	for _, member := range members {
		if member.SourceProfileComplete {
			counts.SourceProfileCompleteMembers++
		} else {
			counts.SourceProfilePartialMembers++
		}
		counts.CaptureGaps += member.CaptureGapCount
		counts.EvidenceItemsExpected += member.Received
	}
	return counts
}

func uniqueSortedCampaignPackageGapCodes(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func strictSortedUniqueCampaignPackageGapCodes(values []string) bool {
	previous := ""
	for _, value := range values {
		if !campaignPackageGapCodePattern.MatchString(value) ||
			(previous != "" && previous >= value) {
			return false
		}
		previous = value
	}
	return true
}

func ParseCampaignPackageImportReceipt(
	payload []byte,
) (CampaignPackageImportReceipt, error) {
	if len(payload) == 0 || len(payload) > 1<<20 {
		return CampaignPackageImportReceipt{}, errors.New("campaign package import receipt size is invalid")
	}
	if err := scanStrictJSONDocument(payload, "campaign package import receipt", nil); err != nil {
		return CampaignPackageImportReceipt{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var receipt CampaignPackageImportReceipt
	if err := decoder.Decode(&receipt); err != nil {
		return CampaignPackageImportReceipt{}, fmt.Errorf("decode campaign package import receipt: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return CampaignPackageImportReceipt{}, errors.New("campaign package import receipt contains trailing content")
	}
	if err := receipt.Validate(); err != nil {
		return CampaignPackageImportReceipt{}, err
	}
	return receipt, nil
}

func (receipt CampaignPackageImportReceipt) CanonicalBytes() ([]byte, error) {
	if err := receipt.Validate(); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return connector.CanonicalizeJSONValue(value)
}

func (receipt CampaignPackageImportReceipt) SHA256() (string, error) {
	canonical, err := receipt.CanonicalBytes()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
