package unirouter

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connector"
)

type fixtureCampaignPackageAdapter struct {
	identity CampaignPackageContractIdentity
	opened   OpenedCampaignPackage
	err      error
	called   int
	trust    SourceEvidenceTrustPolicy
}

func (adapter *fixtureCampaignPackageAdapter) ContractIdentity() CampaignPackageContractIdentity {
	return adapter.identity
}

func (adapter *fixtureCampaignPackageAdapter) VerifyAndOpen(
	_ []byte,
	trust SourceEvidenceTrustPolicy,
) (OpenedCampaignPackage, error) {
	adapter.called++
	adapter.trust = trust
	return adapter.opened, adapter.err
}

func TestVerifyCampaignPackageUsesImmutableAdapterAndContentAddressedInventory(t *testing.T) {
	packageBytes, request, adapter := campaignPackageVerifierFixture(t)
	verified, err := VerifyCampaignPackage(packageBytes, request, adapter)
	if err != nil {
		t.Fatal(err)
	}
	projection := verified.Projection()
	memberA, exists := verified.MemberPayload("member-a")
	if adapter.called != 1 || adapter.trust != request.Trust {
		t.Fatalf("adapter calls=%d trust=%+v", adapter.called, adapter.trust)
	}
	if projection.PackageSHA256 != campaignPackageSHA256(packageBytes) ||
		!projection.PackageScopeComplete || !exists {
		t.Fatalf("verified package=%+v member_a_exists=%v", projection, exists)
	}

	// Verification results own their byte slices; an adapter cannot mutate the
	// accepted member bytes after verification.
	adapter.opened.MemberPayloads["member-a"][0] ^= 0xff
	if bytes.Equal(
		memberA,
		adapter.opened.MemberPayloads["member-a"],
	) {
		t.Fatal("verified member payload aliases contract-adapter memory")
	}
}

func TestVerifyCampaignPackageRejectsUpgradeAuthorityBeforeContractParsing(t *testing.T) {
	packageBytes, request, adapter := campaignPackageVerifierFixture(t)
	request.Trust.Purpose = "upgrade"
	if _, err := VerifyCampaignPackage(packageBytes, request, adapter); err == nil ||
		!strings.Contains(err.Error(), "source-evidence trust policy") {
		t.Fatalf("upgrade-purpose error=%v", err)
	}
	if adapter.called != 0 {
		t.Fatalf("producer parser was called under upgrade authority: %d", adapter.called)
	}

	packageBytes, request, adapter = campaignPackageVerifierFixture(t)
	request.Trust.SigningKeyRef = "release-fixture-01"
	if _, err := VerifyCampaignPackage(packageBytes, request, adapter); err == nil ||
		!strings.Contains(err.Error(), "source-evidence trust policy") {
		t.Fatalf("non-Source operational key error=%v", err)
	}
	if adapter.called != 0 {
		t.Fatalf("producer parser was called with a non-Source operational key: %d", adapter.called)
	}

	packageBytes, request, adapter = campaignPackageVerifierFixture(t)
	adapter.opened.Projection.Signature.Purpose = "upgrade"
	if _, err := VerifyCampaignPackage(packageBytes, request, adapter); err == nil ||
		!strings.Contains(err.Error(), "exact source-evidence authority") {
		t.Fatalf("upgrade-signature error=%v", err)
	}

	packageBytes, request, adapter = campaignPackageVerifierFixture(t)
	adapter.opened.Projection.Signature.TrustSetSHA256 = digestFixture("release-trust-set")
	if _, err := VerifyCampaignPackage(packageBytes, request, adapter); err == nil ||
		!strings.Contains(err.Error(), "exact source-evidence authority") {
		t.Fatalf("release-trust-pin error=%v", err)
	}
}

func TestVerifyCampaignPackageRequiresExactThreePartSourceSignerIdentity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SourceEvidenceSignatureIdentity)
	}{
		{
			name: "producer operational key reference",
			mutate: func(identity *SourceEvidenceSignatureIdentity) {
				identity.SigningKeyRef = "source-evidence-fixture-02"
			},
		},
		{
			name: "cryptographic public key identity",
			mutate: func(identity *SourceEvidenceSignatureIdentity) {
				identity.SignerPublicKeyID = "ed25519:" + digestFixture("other-public-key")
			},
		},
		{
			name: "purpose-specific trust set",
			mutate: func(identity *SourceEvidenceSignatureIdentity) {
				identity.TrustSetSHA256 = digestFixture("other-source-evidence-trust-set")
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			packageBytes, request, adapter := campaignPackageVerifierFixture(t)
			testCase.mutate(&adapter.opened.Projection.Signature)
			if _, err := VerifyCampaignPackage(packageBytes, request, adapter); err == nil ||
				!strings.Contains(err.Error(), "exact source-evidence authority") {
				t.Fatalf("three-part signer mismatch error=%v", err)
			}
		})
	}
}

func TestVerifyCampaignPackageFailsClosedOnContractAndInventoryDrift(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func([]byte, *CampaignPackageVerificationRequest, *fixtureCampaignPackageAdapter) []byte
		wantErr string
	}{
		{
			name: "contract pin",
			mutate: func(payload []byte, request *CampaignPackageVerificationRequest, _ *fixtureCampaignPackageAdapter) []byte {
				request.ExpectedContract.SHA256 = digestFixture("other-contract")
				return payload
			},
			wantErr: "adapter identity",
		},
		{
			name: "package object digest",
			mutate: func(payload []byte, _ *CampaignPackageVerificationRequest, _ *fixtureCampaignPackageAdapter) []byte {
				return append(payload, '!')
			},
			wantErr: "object digest",
		},
		{
			name: "incomplete outer scope",
			mutate: func(payload []byte, _ *CampaignPackageVerificationRequest, adapter *fixtureCampaignPackageAdapter) []byte {
				adapter.opened.Projection.PackageScopeComplete = false
				return payload
			},
			wantErr: "frozen package scope",
		},
		{
			name: "authorization manifest binding",
			mutate: func(payload []byte, _ *CampaignPackageVerificationRequest, adapter *fixtureCampaignPackageAdapter) []byte {
				adapter.opened.Projection.AuthorizationManifestSHA256 = digestFixture("other-authorization-manifest")
				return payload
			},
			wantErr: "preauthorization or observed package identity",
		},
		{
			name: "authorization statement binding",
			mutate: func(payload []byte, _ *CampaignPackageVerificationRequest, adapter *fixtureCampaignPackageAdapter) []byte {
				adapter.opened.Projection.AuthorizationSHA256 = digestFixture("other-authorization")
				return payload
			},
			wantErr: "preauthorization or observed package identity",
		},
		{
			name: "authorization signature binding",
			mutate: func(payload []byte, _ *CampaignPackageVerificationRequest, adapter *fixtureCampaignPackageAdapter) []byte {
				adapter.opened.Projection.AuthorizationSignatureSHA256 = digestFixture("other-authorization-signature")
				return payload
			},
			wantErr: "preauthorization or observed package identity",
		},
		{
			name: "member tamper",
			mutate: func(payload []byte, _ *CampaignPackageVerificationRequest, adapter *fixtureCampaignPackageAdapter) []byte {
				adapter.opened.MemberPayloads["member-b"] = []byte("tampered")
				return payload
			},
			wantErr: "content-addressed inventory",
		},
		{
			name: "unmanifested member",
			mutate: func(payload []byte, _ *CampaignPackageVerificationRequest, adapter *fixtureCampaignPackageAdapter) []byte {
				adapter.opened.MemberPayloads["member-extra"] = []byte("extra")
				return payload
			},
			wantErr: "inventory is incomplete",
		},
		{
			name: "unsafe member path",
			mutate: func(payload []byte, _ *CampaignPackageVerificationRequest, adapter *fixtureCampaignPackageAdapter) []byte {
				adapter.opened.Projection.Members[0].Path = "../member.json"
				return payload
			},
			wantErr: "member[0]",
		},
		{
			name: "control character in member path",
			mutate: func(payload []byte, _ *CampaignPackageVerificationRequest, adapter *fixtureCampaignPackageAdapter) []byte {
				adapter.opened.Projection.Members[0].Path = "members/member\n.json"
				return payload
			},
			wantErr: "member[0]",
		},
		{
			name: "noncanonical inventory order",
			mutate: func(payload []byte, _ *CampaignPackageVerificationRequest, adapter *fixtureCampaignPackageAdapter) []byte {
				adapter.opened.Projection.Members[0], adapter.opened.Projection.Members[1] =
					adapter.opened.Projection.Members[1], adapter.opened.Projection.Members[0]
				return payload
			},
			wantErr: "strict path order",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			packageBytes, request, adapter := campaignPackageVerifierFixture(t)
			packageBytes = testCase.mutate(packageBytes, &request, adapter)
			if _, err := VerifyCampaignPackage(packageBytes, request, adapter); err == nil ||
				!strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("error=%v want substring %q", err, testCase.wantErr)
			}
		})
	}
}

func TestCampaignPackageReceiptKeepsOuterAndMemberCompletenessIndependent(t *testing.T) {
	packageBytes, request, adapter := campaignPackageVerifierFixture(t)
	verified, err := VerifyCampaignPackage(packageBytes, request, adapter)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := ProjectCampaignPackageImportReceipt(CampaignPackageImportReceiptInput{
		OperationID: "operation-fixture-001",
		Package:     verified,
		OperationalReceipt: campaignPackageOperationalReceipt(
			t, "operation-fixture-001", verified.Projection().PackageID,
			verified.Projection().PackageSHA256, 5, 3, 2,
		),
		Members: []CampaignPackageMemberImportOutcome{
			{
				MemberID:                    "member-b",
				SourceProfileState:          "complete",
				SourceProfileComplete:       true,
				RuntimeCloseMarkerAvailable: true,
				Received:                    2,
			},
			{
				MemberID:                    "member-a",
				SourceProfileState:          "partial",
				SourceProfileComplete:       false,
				RuntimeCloseMarkerAvailable: false,
				CaptureGapCount:             1,
				GapCodes:                    []string{RuntimeCloseMarkerUnavailableGapCode},
				Received:                    3,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.PackageScopeComplete || receipt.AllMemberSourceProfilesComplete ||
		receipt.Counts.SourceProfilePartialMembers != 1 || receipt.Counts.CaptureGaps != 1 ||
		receipt.Counts.EvidenceItemsExpected != 5 ||
		receipt.OperationalReceipt.Counts.Inserted != 3 ||
		receipt.OperationalReceipt.Counts.DuplicateNoop != 2 {
		t.Fatalf("receipt completeness/counts=%+v all_complete=%v", receipt.Counts, receipt.AllMemberSourceProfilesComplete)
	}
	canonical, err := receipt.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(canonical, []byte(`"package_scope_complete":true`)) ||
		!bytes.Contains(canonical, []byte(`"all_member_source_profiles_complete":false`)) ||
		!bytes.Contains(canonical, []byte(RuntimeCloseMarkerUnavailableGapCode)) {
		t.Fatalf("canonical receipt lost independent completeness: %s", canonical)
	}
	parsed, err := ParseCampaignPackageImportReceipt(canonical)
	if err != nil {
		t.Fatal(err)
	}
	firstSHA, err := receipt.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	secondSHA, err := parsed.SHA256()
	if err != nil || firstSHA != secondSHA {
		t.Fatalf("receipt SHA first=%q second=%q err=%v", firstSHA, secondSHA, err)
	}
	rebuilt, err := ProjectCampaignPackageImportReceipt(CampaignPackageImportReceiptInput{
		OperationID:        parsed.OperationID,
		Package:            verified,
		OperationalReceipt: parsed.OperationalReceipt,
		Members: []CampaignPackageMemberImportOutcome{
			{
				MemberID: "member-b", SourceProfileState: "complete",
				SourceProfileComplete: true, RuntimeCloseMarkerAvailable: true,
				Received: 2,
			},
			{
				MemberID: "member-a", SourceProfileState: "partial",
				RuntimeCloseMarkerAvailable: false, CaptureGapCount: 1,
				GapCodes: []string{RuntimeCloseMarkerUnavailableGapCode}, Received: 3,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rebuiltCanonical, err := rebuilt.CanonicalBytes()
	if err != nil || !bytes.Equal(rebuiltCanonical, canonical) {
		t.Fatalf("durable replay did not rebuild the same wrapper: err=%v\nfirst=%s\nrebuilt=%s", err, canonical, rebuiltCanonical)
	}
	tampered := parsed
	tampered.OperationalReceipt.Counts.Inserted--
	tampered.OperationalReceipt.Counts.DuplicateNoop++
	if err := tampered.Validate(); err == nil ||
		!strings.Contains(err.Error(), "operational receipt digest") {
		t.Fatalf("tampered durable receipt error=%v", err)
	}
}

func TestCampaignPackageReceiptRejectsOperationalReceiptForAnotherPackage(t *testing.T) {
	packageBytes, request, adapter := campaignPackageVerifierFixture(t)
	verified, err := VerifyCampaignPackage(packageBytes, request, adapter)
	if err != nil {
		t.Fatal(err)
	}
	operational := campaignPackageOperationalReceipt(
		t, "operation-fixture-001", "other-package",
		verified.Projection().PackageSHA256, 2, 2, 0,
	)
	_, err = ProjectCampaignPackageImportReceipt(CampaignPackageImportReceiptInput{
		OperationID:        "operation-fixture-001",
		Package:            verified,
		OperationalReceipt: operational,
		Members: []CampaignPackageMemberImportOutcome{
			{
				MemberID: "member-a", SourceProfileState: "partial",
				RuntimeCloseMarkerAvailable: false, CaptureGapCount: 1,
				GapCodes: []string{RuntimeCloseMarkerUnavailableGapCode}, Received: 1,
			},
			{
				MemberID: "member-b", SourceProfileState: "complete",
				SourceProfileComplete: true, RuntimeCloseMarkerAvailable: true,
				Received: 1,
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "does not match its durable operational receipt") {
		t.Fatalf("cross-package operational receipt error=%v", err)
	}
}

func TestCampaignPackageReceiptRejectsPartialProfileWithoutExplicitGap(t *testing.T) {
	packageBytes, request, adapter := campaignPackageVerifierFixture(t)
	verified, err := VerifyCampaignPackage(packageBytes, request, adapter)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ProjectCampaignPackageImportReceipt(CampaignPackageImportReceiptInput{
		OperationID: "operation-fixture-001",
		Package:     verified,
		OperationalReceipt: campaignPackageOperationalReceipt(
			t, "operation-fixture-001", verified.Projection().PackageID,
			verified.Projection().PackageSHA256, 2, 2, 0,
		),
		Members: []CampaignPackageMemberImportOutcome{
			{MemberID: "member-a", SourceProfileState: "partial", Received: 1},
			{MemberID: "member-b", SourceProfileState: "complete", SourceProfileComplete: true, RuntimeCloseMarkerAvailable: true, Received: 1},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "explicit capture gaps") {
		t.Fatalf("partial-without-gap error=%v", err)
	}
}

func TestCampaignPackageMemberCompletenessPreservesRuntimeCloseMarkerGap(t *testing.T) {
	bundle := recoveredFallbackBundle()
	bundle.Completeness.State = "partial"
	bundle.Completeness.SourceProfileComplete = false
	bundle.CaptureGaps = []ExecutionEvidenceGap{{
		Code:   RuntimeCloseMarkerUnavailableGapCode,
		Stage:  "source",
		Reason: "the current runtime cannot emit a v2 close marker",
	}}
	outcome, err := CampaignPackageMemberCompleteness("member-a", bundle, 3)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.SourceProfileComplete || outcome.RuntimeCloseMarkerAvailable ||
		outcome.SourceProfileState != "partial" || outcome.CaptureGapCount != 1 ||
		len(outcome.GapCodes) != 1 || outcome.GapCodes[0] != RuntimeCloseMarkerUnavailableGapCode {
		t.Fatalf("partial member outcome=%+v", outcome)
	}

	complete, err := CampaignPackageMemberCompleteness("member-b", recoveredFallbackBundle(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !complete.SourceProfileComplete || !complete.RuntimeCloseMarkerAvailable ||
		complete.SourceProfileState != "complete" || complete.CaptureGapCount != 0 {
		t.Fatalf("complete member outcome=%+v", complete)
	}
}

func TestParseCampaignPackageReceiptRejectsUnknownAndDuplicateFields(t *testing.T) {
	for _, payload := range [][]byte{
		[]byte(`{"receipt_version":"xinsight.unirouter-campaign-package-import-receipt.v1","unknown":true}`),
		[]byte(`{"receipt_version":"one","receipt_version":"two"}`),
		[]byte(`{"counts":{"members":1,"members":1}}`),
	} {
		if _, err := ParseCampaignPackageImportReceipt(payload); err == nil {
			t.Fatalf("ambiguous receipt was accepted: %s", payload)
		}
	}
}

func campaignPackageVerifierFixture(
	t *testing.T,
) ([]byte, CampaignPackageVerificationRequest, *fixtureCampaignPackageAdapter) {
	t.Helper()
	packageBytes := []byte("opaque producer package fixture")
	contract := CampaignPackageContractIdentity{
		ID:     "contract-fixture-v1",
		SHA256: digestFixture("contract"),
	}
	trust := SourceEvidenceTrustPolicy{
		Purpose:           SourceEvidencePurpose,
		Domain:            SourceEvidenceDomain,
		Algorithm:         SourceEvidenceSignatureEd25519,
		SigningKeyRef:     "source-evidence-fixture-01",
		SignerPublicKeyID: "ed25519:" + digestFixture("public-key"),
		TrustSetSHA256:    digestFixture("trust-set"),
	}
	members := []CampaignPackageMemberInventory{
		{MemberID: "member-a", Path: "members/request-a.json", SHA256: digestFixture("member-a"), SizeBytes: int64(len("member-a"))},
		{MemberID: "member-b", Path: "members/request-b.json", SHA256: digestFixture("member-b"), SizeBytes: int64(len("member-b"))},
	}
	projection := CampaignPackageProjection{
		Contract:                     contract,
		PackageID:                    "package-fixture-001",
		PackageSHA256:                campaignPackageSHA256(packageBytes),
		PackageSizeBytes:             int64(len(packageBytes)),
		ManifestSHA256:               digestFixture("manifest"),
		PackageCreatedAt:             "2030-01-01T00:01:00Z",
		PackageScopeComplete:         true,
		SourceInstance:               "source-fixture",
		SourceBindingSHA256:          digestFixture("source-binding"),
		AuthorizationManifestSHA256:  digestFixture("authorization-manifest"),
		AuthorizationSHA256:          digestFixture("authorization"),
		AuthorizationSignatureSHA256: digestFixture("authorization-signature"),
		Signature: SourceEvidenceSignatureIdentity{
			Purpose:           trust.Purpose,
			Domain:            trust.Domain,
			Algorithm:         trust.Algorithm,
			SigningKeyRef:     trust.SigningKeyRef,
			SignerPublicKeyID: trust.SignerPublicKeyID,
			TrustSetSHA256:    trust.TrustSetSHA256,
		},
		Members: members,
	}
	request := CampaignPackageVerificationRequest{
		ExpectedContract:                     contract,
		ExpectedPackageID:                    projection.PackageID,
		ExpectedPackageSHA256:                projection.PackageSHA256,
		ExpectedSourceBindingSHA256:          projection.SourceBindingSHA256,
		ExpectedAuthorizationManifestSHA256:  projection.AuthorizationManifestSHA256,
		ExpectedAuthorizationSHA256:          projection.AuthorizationSHA256,
		ExpectedAuthorizationSignatureSHA256: projection.AuthorizationSignatureSHA256,
		Trust:                                trust,
	}
	adapter := &fixtureCampaignPackageAdapter{
		identity: contract,
		opened: OpenedCampaignPackage{
			Projection: projection,
			MemberPayloads: map[string][]byte{
				"member-a": []byte("member-a"),
				"member-b": []byte("member-b"),
			},
		},
	}
	return packageBytes, request, adapter
}

func digestFixture(value string) string {
	return campaignPackageSHA256([]byte(value))
}

func campaignPackageOperationalReceipt(
	t *testing.T,
	operationID string,
	packageID string,
	packageSHA256 string,
	received int64,
	inserted int64,
	duplicateNoop int64,
) connector.SourceNeutralOperationalReceipt {
	t.Helper()
	receipt := connector.SourceNeutralOperationalReceipt{
		ReceiptVersion:   connector.SourceNeutralReceiptVersionV2,
		OperationID:      operationID,
		ConnectorID:      "foigent.unirouter.poc-source-evidence",
		ProducerInstance: "source-fixture",
		DeliveryMode:     connector.DeliveryModeBoundedOffline,
		Operation:        connector.OperationImportBundle,
		Status:           "completed",
		Counts: connector.ReceiptCounts{
			Received:      received,
			Inserted:      inserted,
			DuplicateNoop: duplicateNoop,
		},
		DeliveryState: connector.BoundedOfflineDeliveryState{
			Mode:         connector.DeliveryModeBoundedOffline,
			BundleID:     packageID,
			BundleSHA256: packageSHA256,
		},
		ObservedAt: time.Date(2026, 8, 10, 10, 11, 12, 13000, time.UTC).
			Format("2006-01-02T15:04:05.000000Z"),
	}
	if err := receipt.Validate(); err != nil {
		t.Fatalf("invalid operational receipt fixture: %v", err)
	}
	return receipt
}

var _ CampaignPackageContractAdapter = (*fixtureCampaignPackageAdapter)(nil)
