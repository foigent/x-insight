package integrationcontrol

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	unirouteradapter "github.com/foigent/x-insight/internal/adapters/unirouter"
)

func TestSyntheticProductArtifactsAreDeterministicAndAcyclic(t *testing.T) {
	artifacts := syntheticProductArtifacts(t)
	for name, artifact := range artifacts.canonical {
		fixture := readSyntheticFixture(t, name)
		if !bytes.Equal(fixture, artifact) {
			t.Fatalf("fixture %s drifted\nwant=%s\ngot =%s", name, artifact, fixture)
		}
	}

	if _, err := ParseComponentContract(artifacts.canonical["component-contract.json"]); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseVerifierRelease(artifacts.canonical["verifier-release.json"]); err != nil {
		t.Fatal(err)
	}
	scope, err := ParseComponentScopeReceipt(artifacts.canonical["component-scope-receipt.json"])
	if err != nil {
		t.Fatal(err)
	}
	verification, err := ParseComponentVerificationReceipt(
		artifacts.canonical["component-verification-receipt.json"],
	)
	if err != nil {
		t.Fatal(err)
	}
	acknowledgement, err := ParseComponentAcknowledgement(
		artifacts.canonical["component-acknowledgement.json"],
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verification.CanonicalBytes(); err == nil {
		t.Fatal("parsed accepted verification was re-emitted without lifecycle authority")
	}
	if _, err := acknowledgement.CanonicalBytes(); err == nil {
		t.Fatal("parsed accepted acknowledgement was re-emitted without lifecycle authority")
	}
	if err := verification.ValidateAgainstScope(scope); err != nil {
		t.Fatal(err)
	}
	if err := verification.ValidateLifecycle(scope, artifacts.manifest); err != nil {
		t.Fatal(err)
	}
	if err := acknowledgement.ValidateAgainstVerification(verification); err != nil {
		t.Fatal(err)
	}

	assertAbsentJSONKeys(t, artifacts.canonical["component-scope-receipt.json"], []string{
		"campaign_manifest_sha256", "authorization_sha256", "authorization_signature_sha256",
		"package_sha256", "import_receipt_sha256", "result_sha256",
	})
	assertAbsentJSONKeys(t, artifacts.canonical["component-verification-receipt.json"], []string{
		"authorization_sha256", "authorization_signature_sha256", "package_sha256",
		"import_receipt_sha256", "result_sha256",
	})

	rebuilt := syntheticProductArtifacts(t)
	for name, first := range artifacts.canonical {
		if !bytes.Equal(first, rebuilt.canonical[name]) {
			t.Fatalf("synthetic artifact %s is nondeterministic", name)
		}
	}
}

func TestProductArtifactParsersRejectUnknownDuplicateAndNoncanonicalJSON(t *testing.T) {
	artifacts := syntheticProductArtifacts(t)
	digest := bytesSHA256([]byte("forbidden-cycle"))
	scope := artifacts.canonical["component-scope-receipt.json"]
	unknown := append(bytes.Clone(scope[:len(scope)-1]), []byte(
		`,"authorization_sha256":"`+digest+`"}`,
	)...)
	duplicate := bytes.Replace(
		scope,
		[]byte(`"status":"accepted"`),
		[]byte(`"status":"accepted","status":"accepted"`),
		1,
	)
	var indented bytes.Buffer
	if err := json.Indent(&indented, scope, "", "  "); err != nil {
		t.Fatal(err)
	}
	for _, payload := range [][]byte{unknown, duplicate, indented.Bytes()} {
		if _, err := ParseComponentScopeReceipt(payload); err == nil {
			t.Fatalf("ambiguous scope receipt was accepted: %s", payload)
		}
	}
}

func TestComponentVerificationRejectsScopeDriftAndTimeRegression(t *testing.T) {
	artifacts := syntheticProductArtifacts(t)
	scope := artifacts.scope
	verification := artifacts.verification

	drifted := scope
	drifted.ProductRelease.SHA256 = bytesSHA256([]byte("other-release"))
	if err := verification.ValidateAgainstScope(drifted); err == nil ||
		!strings.Contains(err.Error(), "contradicts") {
		t.Fatalf("scope drift error=%v", err)
	}

	verification.VerifiedAt = "2029-12-31T21:00:00Z"
	if err := verification.ValidateAgainstScope(scope); err == nil ||
		!strings.Contains(err.Error(), "predates") {
		t.Fatalf("chronology error=%v", err)
	}

	verification = artifacts.verification
	manifest := artifacts.manifest
	manifest.FrozenAt = "2029-12-31T22:00:00Z"
	sealManifestVerification(&manifest)
	if err := verification.ValidateLifecycle(scope, manifest); err == nil ||
		!strings.Contains(err.Error(), "scope-before-M") {
		t.Fatalf("scope-after-M chronology error=%v", err)
	}

	manifestDrifts := []struct {
		name   string
		mutate func(*N1ManifestVerification)
	}{
		{"invalid report", func(report *N1ManifestVerification) { report.Valid = false }},
		{"campaign drift", func(report *N1ManifestVerification) { report.CampaignID = "other-campaign" }},
		{"manifest drift", func(report *N1ManifestVerification) {
			report.CampaignManifestSHA256 = bytesSHA256([]byte("other-manifest"))
		}},
		{"noncanonical freeze time", func(report *N1ManifestVerification) {
			report.FrozenAt = "2029-12-31T23:00:00+00:00"
		}},
	}
	for _, test := range manifestDrifts {
		t.Run(test.name, func(t *testing.T) {
			report := artifacts.manifest
			test.mutate(&report)
			sealManifestVerification(&report)
			if err := artifacts.verification.ValidateLifecycle(scope, report); err == nil {
				t.Fatal("manifest binding drift was accepted")
			}
		})
	}

	productTupleDrifts := []struct {
		name   string
		mutate func(*N1ComponentIdentity)
	}{
		{"contract digest", func(component *N1ComponentIdentity) {
			component.Contract.SHA256 = bytesSHA256([]byte("other-contract"))
		}},
		{"release schema", func(component *N1ComponentIdentity) {
			component.Release.SchemaVersion = "xinsight.release-identity.other.v1"
		}},
		{"release digest", func(component *N1ComponentIdentity) {
			component.Release.SHA256 = bytesSHA256([]byte("other-release"))
		}},
		{"scope schema", func(component *N1ComponentIdentity) {
			component.ScopeReceipt.SchemaVersion = "xinsight.scope-receipt.other.v1"
		}},
		{"scope digest", func(component *N1ComponentIdentity) {
			component.ScopeReceipt.SHA256 = bytesSHA256([]byte("other-scope"))
		}},
	}
	for _, test := range productTupleDrifts {
		t.Run("M X component "+test.name, func(t *testing.T) {
			report := artifacts.manifest
			test.mutate(&report.XInsightComponent)
			sealManifestVerification(&report)
			if _, err := NewComponentVerificationLifecycle(
				scope,
				report,
				time.Date(2029, 12, 31, 23, 31, 0, 0, time.UTC),
			); err == nil ||
				!strings.Contains(err.Error(), "contract, release, or scope") {
				t.Fatalf("product tuple drift error=%v", err)
			}
		})
	}

	manifest = artifacts.manifest
	manifest.FrozenAt = "2029-12-31T23:45:00Z"
	sealManifestVerification(&manifest)
	if err := artifacts.verification.ValidateLifecycle(scope, manifest); err == nil ||
		!strings.Contains(err.Error(), "scope-before-M") {
		t.Fatalf("verification-before-M chronology error=%v", err)
	}
	if _, err := NewComponentAcknowledgement(VerifiedComponentLifecycle{}); err == nil {
		t.Fatal("accepted acknowledgement was emitted without lifecycle authority")
	}
}

func TestPackageAuthorityRejectsManifestAuthorizationAndSignerDriftBeforeProducerParsing(t *testing.T) {
	artifacts := syntheticProductArtifacts(t)
	authorization := syntheticAuthorizationVerification(t, artifacts.manifest)
	if err := authorization.ValidateAgainstManifest(artifacts.manifest); err != nil {
		t.Fatal(err)
	}
	packageBytes := []byte("synthetic-observed-package")
	request := unirouteradapter.CampaignPackageVerificationRequest{
		ExpectedContract: unirouteradapter.CampaignPackageContractIdentity{
			ID:     artifacts.manifest.UniRouterComponent.Contract.ID,
			SHA256: artifacts.manifest.UniRouterComponent.Contract.SHA256,
		},
		ExpectedPackageID:                    "synthetic-observed-package-01",
		ExpectedPackageSHA256:                bytesSHA256(packageBytes),
		ExpectedSourceBindingSHA256:          artifacts.manifest.SourceBinding.SHA256,
		ExpectedAuthorizationManifestSHA256:  authorization.CampaignManifestSHA256,
		ExpectedAuthorizationSHA256:          authorization.AuthorizationSHA256,
		ExpectedAuthorizationSignatureSHA256: authorization.AuthorizationSignatureSHA256,
		Trust:                                artifacts.manifest.SourceEvidenceSigning.trustPolicy(),
	}

	drifts := []struct {
		name   string
		mutate func(*unirouteradapter.CampaignPackageVerificationRequest)
	}{
		{"producer contract", func(value *unirouteradapter.CampaignPackageVerificationRequest) {
			value.ExpectedContract.SHA256 = bytesSHA256([]byte("other-producer-contract"))
		}},
		{"M digest", func(value *unirouteradapter.CampaignPackageVerificationRequest) {
			value.ExpectedAuthorizationManifestSHA256 = bytesSHA256([]byte("other-manifest"))
		}},
		{"A digest", func(value *unirouteradapter.CampaignPackageVerificationRequest) {
			value.ExpectedAuthorizationSHA256 = bytesSHA256([]byte("other-authorization"))
		}},
		{"A signature digest", func(value *unirouteradapter.CampaignPackageVerificationRequest) {
			value.ExpectedAuthorizationSignatureSHA256 = bytesSHA256([]byte("other-authorization-signature"))
		}},
		{"Source binding", func(value *unirouteradapter.CampaignPackageVerificationRequest) {
			value.ExpectedSourceBindingSHA256 = bytesSHA256([]byte("other-source-binding"))
		}},
		{"Source purpose", func(value *unirouteradapter.CampaignPackageVerificationRequest) {
			value.Trust.Purpose = "upgrade"
		}},
		{"Source domain", func(value *unirouteradapter.CampaignPackageVerificationRequest) {
			value.Trust.Domain = "unirouter.source-evidence.other.v1"
		}},
		{"Source key ref", func(value *unirouteradapter.CampaignPackageVerificationRequest) {
			value.Trust.SigningKeyRef = "source-evidence-synthetic-02"
		}},
		{"Source public key", func(value *unirouteradapter.CampaignPackageVerificationRequest) {
			value.Trust.SignerPublicKeyID = "ed25519:" + bytesSHA256([]byte("other-source-public-key"))
		}},
		{"Source trust set", func(value *unirouteradapter.CampaignPackageVerificationRequest) {
			value.Trust.TrustSetSHA256 = bytesSHA256([]byte("other-source-trust-set"))
		}},
	}
	for _, test := range drifts {
		t.Run(test.name, func(t *testing.T) {
			drifted := request
			test.mutate(&drifted)
			adapter := &countingCampaignPackageAdapter{contract: request.ExpectedContract}
			if _, err := VerifyPreauthorizedCampaignPackage(packageBytes, drifted, adapter, authorization); err == nil {
				t.Fatal("preauthorization drift was accepted")
			}
			if adapter.identityCalls != 0 || adapter.parseCalls != 0 {
				t.Fatalf("producer adapter was reached before authority closure: %+v", adapter)
			}
		})
	}

	tamperedAuthorization := authorization
	tamperedAuthorization.SourceEvidenceSigning.SigningKeyRef = "source-evidence-synthetic-02"
	adapter := &countingCampaignPackageAdapter{contract: request.ExpectedContract}
	if _, err := VerifyPreauthorizedCampaignPackage(packageBytes, request, adapter, tamperedAuthorization); err == nil {
		t.Fatal("tampered sealed authorization was accepted")
	}
	if adapter.identityCalls != 0 || adapter.parseCalls != 0 {
		t.Fatal("producer adapter was reached for tampered sealed authorization")
	}

	adapter = &countingCampaignPackageAdapter{contract: request.ExpectedContract}
	if _, err := VerifyPreauthorizedCampaignPackage(packageBytes, request, adapter, authorization); !errors.Is(err, errSyntheticProducerReached) {
		t.Fatalf("matching authority did not reach the producer adapter: %v", err)
	}
	if adapter.identityCalls != 1 || adapter.parseCalls != 1 {
		t.Fatalf("matching authority adapter calls=%+v", adapter)
	}
}

func TestFrozenN1ReleasePinContainsNoPathOrMutableAuthority(t *testing.T) {
	pin := FrozenN1ReleasePin()
	if err := pin.Validate(); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(pin)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"origin/main", "upgrade", "worktree", "repository_path"} {
		if bytes.Contains(payload, []byte(forbidden)) {
			t.Fatalf("N1 pin contains forbidden mutable/path authority %q", forbidden)
		}
	}
	if pin.ReleaseManifestSHA256 != FrozenN1ReleaseManifestSHA256 ||
		pin.CapabilityBoundary != CapabilityOfflineFiles {
		t.Fatalf("N1 pin=%+v", pin)
	}
	drifted := pin
	drifted.Artifacts = append([]N1ArtifactPin(nil), pin.Artifacts...)
	drifted.Artifacts[0].SHA256 = bytesSHA256([]byte("unreviewed-verifier"))
	if err := drifted.Validate(); err == nil {
		t.Fatal("N1 release pin accepted an unreviewed artifact digest")
	}
}

func TestN1ReportDecoderRejectsUnknownDuplicateAndOversizedOutput(t *testing.T) {
	valid := []byte(`{"valid":true,"campaign_manifest_sha256":"` + bytesSHA256([]byte("manifest")) + `"}`)
	if _, err := decodeN1Report[N1ManifestVerification](valid); err != nil {
		t.Fatal(err)
	}
	for _, payload := range [][]byte{
		bytes.Replace(valid, []byte(`"valid":true`), []byte(`"valid":true,"valid":true`), 1),
		append(bytes.Clone(valid[:len(valid)-1]), []byte(`,"unknown":true}`)...),
		bytes.Repeat([]byte("x"), n1MaximumReportBytes+1),
	} {
		if _, err := decodeN1Report[N1ManifestVerification](payload); err == nil {
			t.Fatal("invalid N1 report was accepted")
		}
	}
}

func TestFrozenN1OfflineVerifierAndFixtures(t *testing.T) {
	assetDirectory := os.Getenv("XINSIGHT_N1_RELEASE_ASSET_DIRECTORY")
	if assetDirectory == "" {
		t.Skip("XINSIGHT_N1_RELEASE_ASSET_DIRECTORY is required for immutable N1 compatibility")
	}
	manifestPath := filepath.Join(assetDirectory, "foigent.integration-control.release.v1.json")
	binaryPath := filepath.Join(
		assetDirectory,
		fmt.Sprintf("foigent-integration-control-%s-%s-%s", FrozenN1Version, runtime.GOOS, runtime.GOARCH),
	)
	sourceArchive := filepath.Join(
		assetDirectory, "foigent-integration-control-"+FrozenN1Version+"-source.tar",
	)
	verifier, err := NewN1Verifier(manifestPath, binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.VerifySourceArchive(sourceArchive); err != nil {
		t.Fatal(err)
	}

	required := []string{
		"fixtures/v1/valid/manifest.json",
		"fixtures/v1/valid/unirouter-ack.json",
		"fixtures/v1/valid/xinsight-ack.json",
		"fixtures/v1/valid/authorization.json",
		"fixtures/v1/valid/authorization-signature.json",
		"fixtures/v1/valid/authorization-trust-set.json",
		"fixtures/v1/valid/source-evidence-trust-set.json",
		"fixtures/v1/invalid/manifest-wrong-source-identity.json",
		"fixtures/v1/invalid/manifest-wrong-request-set-identity.json",
		"fixtures/v1/invalid/component-ack-rejected.json",
		"fixtures/v1/invalid/authorization-signature-source-key-ref.json",
		"fixtures/v1/invalid/authorization-trust-set-wrong-purpose.json",
	}
	fixtures := extractN1Fixtures(t, sourceArchive, required)
	ctx := context.Background()
	manifestReport, err := verifier.VerifyManifest(ctx, fixtures[required[0]])
	if err != nil || !manifestReport.Valid || manifestReport.CampaignID == "" || manifestReport.FrozenAt == "" {
		t.Fatalf("manifest report=%+v err=%v", manifestReport, err)
	}
	ackReport, err := verifier.VerifyAcknowledgement(ctx, fixtures[required[0]], fixtures[required[2]])
	if err != nil || ackReport.Product != ProductXInsight {
		t.Fatalf("ack report=%+v err=%v", ackReport, err)
	}
	authorizationFiles := N1AuthorizationFiles{
		Manifest:                 fixtures[required[0]],
		UniRouterAcknowledgement: fixtures[required[1]],
		XInsightAcknowledgement:  fixtures[required[2]],
		Authorization:            fixtures[required[3]],
		Signature:                fixtures[required[4]],
		AuthorizationTrustSet:    fixtures[required[5]],
		SourceEvidenceTrustSet:   fixtures[required[6]],
	}
	authorizationReport, err := verifier.VerifyAuthorization(ctx, authorizationFiles)
	if err != nil || !authorizationReport.Valid ||
		authorizationReport.CapabilityBoundary != CapabilityOfflineFiles {
		t.Fatalf("authorization report=%+v err=%v", authorizationReport, err)
	}
	if err := authorizationReport.ValidateAgainstManifest(manifestReport); err != nil {
		t.Fatal(err)
	}

	for _, invalidManifest := range []string{required[7], required[8]} {
		if _, err := verifier.VerifyManifest(ctx, fixtures[invalidManifest]); err == nil {
			t.Fatalf("N1 accepted invalid manifest %s", invalidManifest)
		}
	}
	if _, err := verifier.VerifyAcknowledgement(
		ctx,
		fixtures[required[0]],
		fixtures[required[9]],
	); err == nil {
		t.Fatal("N1 accepted rejected component acknowledgement")
	}

	invalidSignature := authorizationFiles
	invalidSignature.Signature = fixtures[required[10]]
	if _, err := verifier.VerifyAuthorization(ctx, invalidSignature); err == nil {
		t.Fatal("N1 accepted an authorization signature using a Source key reference")
	}
	invalidTrust := authorizationFiles
	invalidTrust.AuthorizationTrustSet = fixtures[required[11]]
	if _, err := verifier.VerifyAuthorization(ctx, invalidTrust); err == nil {
		t.Fatal("N1 accepted a wrong-purpose authorization trust set")
	}

	otherSourceTrustPath := filepath.Join(t.TempDir(), "source-evidence-trust-set.json")
	if err := os.WriteFile(
		otherSourceTrustPath,
		[]byte(`{"schema_version":"unirouter.source-evidence.trust-set.v1"}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	otherSourceTrust := authorizationFiles
	otherSourceTrust.SourceEvidenceTrustSet = otherSourceTrustPath
	if _, err := verifier.VerifyAuthorization(ctx, otherSourceTrust); err == nil ||
		!strings.Contains(err.Error(), "contradicts") {
		t.Fatalf("unbound Source-evidence trust-set error=%v", err)
	}

	verifyProductAcknowledgementAgainstN1(t, verifier, fixtures[required[0]])
}

type syntheticArtifacts struct {
	canonical       map[string][]byte
	scope           ComponentScopeReceipt
	manifest        N1ManifestVerification
	verification    ComponentVerificationReceipt
	acknowledgement ComponentAcknowledgement
}

func syntheticProductArtifacts(t *testing.T) syntheticArtifacts {
	t.Helper()
	contract := NewComponentContract()
	contractBody, err := contract.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	contractSHA256 := bytesSHA256(contractBody)
	productReleaseBody := []byte("synthetic-product-release")
	productRelease := ArtifactReference{
		SchemaVersion: "xinsight.release-identity.synthetic.v1",
		SHA256:        bytesSHA256(productReleaseBody),
	}
	verifierRelease, err := NewVerifierRelease(VerifierReleaseInput{
		Commit:                      strings.Repeat("3", 40),
		Version:                     "0.0.0-synthetic",
		ComponentContractSHA256:     contractSHA256,
		ProductReleaseSHA256:        productRelease.SHA256,
		SignedReleaseManifestSHA256: bytesSHA256([]byte("synthetic-signed-release-manifest")),
		ImporterExecutableSHA256:    bytesSHA256([]byte("synthetic-importer")),
		M0EvidenceSchemaSHA256:      M0EvidenceSchemaSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	verifierReleaseBody, err := verifierRelease.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	verifierReleaseSHA256 := bytesSHA256(verifierReleaseBody)
	scope, err := NewComponentScopeReceipt(ComponentScopeInput{
		CampaignID:              "synthetic-campaign-20260811",
		ComponentContractSHA256: contractSHA256,
		ProductRelease:          productRelease,
		VerifierReleaseSHA256:   verifierReleaseSHA256,
		IssuedAt:                time.Date(2029, 12, 31, 22, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	scopeBody, err := scope.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	manifest := syntheticManifestVerification(
		t,
		scope,
		bytesSHA256([]byte("synthetic-campaign-manifest")),
		"2029-12-31T23:00:00Z",
	)
	lifecycle, err := NewComponentVerificationLifecycle(
		scope,
		manifest,
		time.Date(2029, 12, 31, 23, 31, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	verification, err := lifecycle.VerificationReceipt()
	if err != nil {
		t.Fatal(err)
	}
	verificationBody, err := verification.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	acknowledgement, err := NewComponentAcknowledgement(lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	acknowledgementBody, err := acknowledgement.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	return syntheticArtifacts{
		canonical: map[string][]byte{
			"component-contract.json":             contractBody,
			"verifier-release.json":               verifierReleaseBody,
			"component-scope-receipt.json":        scopeBody,
			"component-verification-receipt.json": verificationBody,
			"component-acknowledgement.json":      acknowledgementBody,
		},
		scope:           scope,
		manifest:        manifest,
		verification:    verification,
		acknowledgement: acknowledgement,
	}
}

func syntheticManifestVerification(
	t *testing.T,
	scope ComponentScopeReceipt,
	manifestSHA256 string,
	frozenAt string,
) N1ManifestVerification {
	t.Helper()
	scopeSHA256, err := scope.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	report := N1ManifestVerification{
		Valid:                  true,
		CampaignManifestSHA256: manifestSHA256,
		CampaignID:             scope.CampaignID,
		FrozenAt:               frozenAt,
		XInsightComponent: N1ComponentIdentity{
			Product:      ProductXInsight,
			Role:         ComponentRole,
			Contract:     scope.ComponentContract,
			Release:      scope.ProductRelease,
			ScopeReceipt: ArtifactReference{SchemaVersion: ComponentScopeVersion, SHA256: scopeSHA256},
		},
		UniRouterComponent: N1ComponentIdentity{
			Product: n1UniRouterProduct,
			Role:    n1UniRouterProducerRole,
			Contract: ContractReference{
				ID:     "unirouter.source-evidence.component-package.v1",
				SHA256: bytesSHA256([]byte("synthetic-producer-contract")),
			},
			Release: ArtifactReference{
				SchemaVersion: "unirouter.release-state-identity.synthetic.v1",
				SHA256:        bytesSHA256([]byte("synthetic-producer-release")),
			},
			ScopeReceipt: ArtifactReference{
				SchemaVersion: "unirouter.source-evidence-scope-receipt.synthetic.v1",
				SHA256:        bytesSHA256([]byte("synthetic-producer-scope")),
			},
		},
		SourceBinding: ArtifactReference{
			SchemaVersion: n1SourceBindingVersion,
			SHA256:        bytesSHA256([]byte("synthetic-source-binding")),
		},
		SourceEvidenceSigning: N1SourceEvidenceSigningIdentity{
			Purpose:           "source_evidence",
			Domain:            "unirouter.source-evidence.component.v1",
			SigningKeyRef:     "source-evidence-synthetic-01",
			SignerPublicKeyID: "ed25519:" + bytesSHA256([]byte("synthetic-source-public-key")),
			TrustSetSHA256:    bytesSHA256([]byte("synthetic-source-trust-set")),
		},
	}
	sealManifestVerification(&report)
	if err := report.validateVerified(); err != nil {
		t.Fatal(err)
	}
	return report
}

func sealManifestVerification(report *N1ManifestVerification) {
	report.verificationSeal = report.expectedSeal()
}

func syntheticAuthorizationVerification(
	t *testing.T,
	manifest N1ManifestVerification,
) N1AuthorizationVerification {
	t.Helper()
	report := N1AuthorizationVerification{
		Valid:                  true,
		CampaignManifestSHA256: manifest.CampaignManifestSHA256,
		AcknowledgementSHA256: map[string]string{
			n1UniRouterProduct: bytesSHA256([]byte("synthetic-unirouter-ack")),
			ProductXInsight:    bytesSHA256([]byte("synthetic-xinsight-ack")),
		},
		AuthorizationSHA256:          bytesSHA256([]byte("synthetic-authorization")),
		AuthorizationSignatureSHA256: bytesSHA256([]byte("synthetic-authorization-signature")),
		AuthorizationTrustSetSHA256:  bytesSHA256([]byte("synthetic-authorization-trust-set")),
		SourceEvidenceTrustSetSHA256: manifest.SourceEvidenceSigning.TrustSetSHA256,
		CapabilityBoundary:           CapabilityOfflineFiles,
		UniRouterComponent:           manifest.UniRouterComponent,
		SourceBinding:                manifest.SourceBinding,
		SourceEvidenceSigning:        manifest.SourceEvidenceSigning,
		AuthorizationWindow: N1AuthorizationWindow{
			NotBefore: "2030-01-01T00:00:00Z",
			NotAfter:  "2030-01-01T12:00:00Z",
		},
	}
	report.verificationSeal = report.expectedSeal()
	if err := report.validateVerified(); err != nil {
		t.Fatal(err)
	}
	return report
}

var errSyntheticProducerReached = errors.New("synthetic producer adapter reached")

type countingCampaignPackageAdapter struct {
	contract      unirouteradapter.CampaignPackageContractIdentity
	identityCalls int
	parseCalls    int
}

func (adapter *countingCampaignPackageAdapter) ContractIdentity() unirouteradapter.CampaignPackageContractIdentity {
	adapter.identityCalls++
	return adapter.contract
}

func (adapter *countingCampaignPackageAdapter) VerifyAndOpen(
	[]byte,
	unirouteradapter.SourceEvidenceTrustPolicy,
) (unirouteradapter.OpenedCampaignPackage, error) {
	adapter.parseCalls++
	return unirouteradapter.OpenedCampaignPackage{}, errSyntheticProducerReached
}

func verifyProductAcknowledgementAgainstN1(
	t *testing.T,
	verifier *N1Verifier,
	n1ManifestPath string,
) {
	t.Helper()
	artifacts := syntheticProductArtifacts(t)
	manifest, err := os.ReadFile(n1ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	replacements := [][2]string{
		{"fixture-campaign-20260811", artifacts.scope.CampaignID},
		{
			"sha256:6f8a1cf45567781783c3eae587bf52d46b38d3a977a074ccbf087a449080adad",
			bytesSHA256(artifacts.canonical["component-contract.json"]),
		},
		{
			"sha256:6247d648d7546fc8418c30db74611376e427520d30dcc3ca08ddf4479ee0cda6",
			artifacts.scope.ProductRelease.SHA256,
		},
		{"xinsight.release-identity.v1", artifacts.scope.ProductRelease.SchemaVersion},
		{"xinsight.source-evidence-import-scope-receipt.v1", ComponentScopeVersion},
		{
			"sha256:89f5beab36e73ed42893b7ae1a71bee0b742546c1a917d2f779abffa3b6f2ba2",
			bytesSHA256(artifacts.canonical["component-scope-receipt.json"]),
		},
	}
	for _, replacement := range replacements {
		if !bytes.Contains(manifest, []byte(replacement[0])) {
			t.Fatalf("N1 compatibility manifest lacks expected fixture value %q", replacement[0])
		}
		manifest = bytes.Replace(manifest, []byte(replacement[0]), []byte(replacement[1]), 1)
	}

	directory := t.TempDir()
	manifestPath := filepath.Join(directory, "manifest.json")
	if err := os.WriteFile(manifestPath, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	manifestReport, err := verifier.VerifyManifest(context.Background(), manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := NewComponentVerificationLifecycle(
		artifacts.scope,
		manifestReport,
		time.Date(2029, 12, 31, 23, 31, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	acknowledgement, err := NewComponentAcknowledgement(lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	acknowledgementBody, err := acknowledgement.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	acknowledgementPath := filepath.Join(directory, "xinsight-ack.json")
	if err := os.WriteFile(acknowledgementPath, acknowledgementBody, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.VerifyAcknowledgement(
		context.Background(),
		manifestPath,
		acknowledgementPath,
	); err != nil {
		t.Fatal(err)
	}
}

func readSyntheticFixture(t *testing.T, name string) []byte {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return bytes.TrimSuffix(payload, []byte("\n"))
}

func assertAbsentJSONKeys(t *testing.T, payload []byte, keys []string) {
	t.Helper()
	for _, key := range keys {
		if bytes.Contains(payload, []byte(`"`+key+`"`)) {
			t.Fatalf("artifact contains forbidden backward binding key %q: %s", key, payload)
		}
	}
}

func extractN1Fixtures(t *testing.T, archivePath string, required []string) map[string]string {
	t.Helper()
	wanted := make(map[string]struct{}, len(required))
	for _, name := range required {
		wanted[name] = struct{}{}
	}
	file, err := os.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader := tar.NewReader(file)
	directory := t.TempDir()
	result := make(map[string]string, len(required))
	prefix := "foigent-integration-control-" + FrozenN1Version + "/"
	for {
		header, readErr := reader.Next()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !strings.HasPrefix(header.Name, prefix) || header.Typeflag != tar.TypeReg || header.Size < 0 || header.Size > n1MaximumInputBytes {
			continue
		}
		name := strings.TrimPrefix(header.Name, prefix)
		if _, exists := wanted[name]; !exists {
			continue
		}
		payload, readErr := io.ReadAll(io.LimitReader(reader, n1MaximumInputBytes+1))
		if readErr != nil || int64(len(payload)) != header.Size {
			t.Fatalf("read immutable N1 fixture %s: %v", name, readErr)
		}
		path := filepath.Join(directory, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			t.Fatal(err)
		}
		result[name] = path
	}
	if len(result) != len(required) {
		t.Fatalf("immutable N1 fixture closure=%d want=%d", len(result), len(required))
	}
	return result
}
