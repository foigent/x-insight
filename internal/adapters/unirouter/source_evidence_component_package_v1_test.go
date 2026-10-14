package unirouter

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	uniRouterSourceEvidenceV1FixtureManifestSHA256               = "sha256:dbb526eb32c89b1f4771b6be265c28235ead650a1ab51557c48250cb04520a5c"
	uniRouterSourceEvidenceV1FixtureAuthorizationSHA256          = "sha256:c05636524e3b5513a01d4705fc70c562b344ba28690aafa12c7c37dc5792d1a3"
	uniRouterSourceEvidenceV1FixtureAuthorizationSignatureSHA256 = "sha256:8f550ed24bdf7a9d8b93c298ff45acc8a9bb2c0c53b389bbd25a1ef869b92ec3"
	uniRouterSourceEvidenceV1FixtureSourceBindingSHA256          = "sha256:c8959a370ae9955df2d732897a9ed24ffff0a2b1d03844dd9ff3dbb8607f9020"
	uniRouterSourceEvidenceV1FixtureTrustSHA256                  = "sha256:9a32b63baca57f9eeef2047c74e52c6a759cc3e336d35abadcc09b4cc30f96d8"
	uniRouterSourceEvidenceV1FixtureKeyRef                       = "source-evidence-conformance-05"
	uniRouterSourceEvidenceV1FixturePublicKeyID                  = "ed25519:sha256:a294e178d8c527e23c15fd2e7d5ff9627d29d0813745eeb35bb13250b704f8d6"
)

type sourceEvidenceV1ConformanceIndex struct {
	SchemaVersion          string                               `json:"schema_version"`
	FixtureAuthority       string                               `json:"fixture_authority"`
	ProducerContractID     string                               `json:"producer_contract_id"`
	ProducerContractSHA256 string                               `json:"producer_contract_sha256"`
	Fixtures               []sourceEvidenceV1ConformanceFixture `json:"fixtures"`
}

type sourceEvidenceV1ConformanceFixture struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Expected string `json:"expected"`
}

func TestUniRouterSourceEvidenceV1AdapterMatchesImmutableConformancePackage(t *testing.T) {
	root := uniRouterSourceEvidenceV1FixtureRoot(t)
	index := readSourceEvidenceV1ConformanceIndex(t, root)
	if index.SchemaVersion != "unirouter.source-evidence.conformance-index.v1" ||
		index.FixtureAuthority != "synthetic_conformance_only" ||
		index.ProducerContractID != UniRouterSourceEvidenceV1ContractID ||
		index.ProducerContractSHA256 != UniRouterSourceEvidenceV1ContractSHA256 {
		t.Fatalf("immutable conformance identity=%+v", index)
	}
	for _, fixture := range index.Fixtures {
		payload := readSourceEvidenceV1Fixture(t, root, fixture.Path)
		if sourceEvidenceV1TestSHA256(payload) != fixture.SHA256 {
			t.Fatalf("fixture %q digest drifted", fixture.Path)
		}
	}

	packageBytes := readSourceEvidenceV1Fixture(t, root, "valid/source-evidence-package.tar.gz")
	trustSetBytes := readSourceEvidenceV1Fixture(t, root, "valid/source-evidence-trust-set.json")
	if sourceEvidenceV1TestSHA256(packageBytes) != UniRouterSourceEvidenceV1FixtureSHA256 ||
		sourceEvidenceV1TestSHA256(trustSetBytes) != uniRouterSourceEvidenceV1FixtureTrustSHA256 {
		t.Fatal("accepted package or purpose-specific trust-set pin drifted")
	}
	adapter, err := NewUniRouterSourceEvidenceV1Adapter(trustSetBytes)
	if err != nil {
		t.Fatal(err)
	}
	request := sourceEvidenceV1FixtureVerificationRequest(packageBytes)
	verified, err := VerifyCampaignPackage(packageBytes, request, adapter)
	if err != nil {
		t.Fatal(err)
	}
	projection := verified.Projection()
	if projection.Contract != adapter.ContractIdentity() || projection.PackageID != request.ExpectedPackageSHA256 ||
		projection.SourceInstance != "unirouter-conformance-source" || !projection.PackageScopeComplete ||
		len(projection.Members) != 1 {
		t.Fatalf("conformance projection=%+v", projection)
	}
	member := projection.Members[0]
	payload, exists := verified.MemberPayload(member.MemberID)
	if !exists || sourceEvidenceV1TestSHA256(payload) != member.SHA256 {
		t.Fatal("verified package did not expose the exact in-memory compatibility member")
	}
	bundle, _, err := loadExecutionEvidenceBundleBytes(payload)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := CampaignPackageMemberCompleteness(member.MemberID, bundle, 0)
	if err != nil || outcome.SourceProfileComplete || outcome.RuntimeCloseMarkerAvailable ||
		!slices.Contains(outcome.GapCodes, RuntimeCloseMarkerUnavailableGapCode) {
		t.Fatalf("outer/member completeness collapsed: outcome=%+v err=%v", outcome, err)
	}
}

func TestUniRouterSourceEvidenceV1AdapterRejectsEveryPackageConformanceNegative(t *testing.T) {
	root := uniRouterSourceEvidenceV1FixtureRoot(t)
	trustSetBytes := readSourceEvidenceV1Fixture(t, root, "valid/source-evidence-trust-set.json")
	adapter, err := NewUniRouterSourceEvidenceV1Adapter(trustSetBytes)
	if err != nil {
		t.Fatal(err)
	}
	policy := sourceEvidenceV1FixtureVerificationRequest([]byte("unused")).Trust
	index := readSourceEvidenceV1ConformanceIndex(t, root)
	rejected := 0
	for _, fixture := range index.Fixtures {
		if fixture.Expected != "rejected" || !strings.HasSuffix(fixture.Path, ".tar.gz") {
			continue
		}
		t.Run(filepath.Base(fixture.Path), func(t *testing.T) {
			if _, err := adapter.VerifyAndOpen(readSourceEvidenceV1Fixture(t, root, fixture.Path), policy); err == nil {
				t.Fatalf("immutable negative package %q was accepted", fixture.Path)
			}
		})
		rejected++
	}
	if rejected != 7 {
		t.Fatalf("package negative count=%d want=7", rejected)
	}
	wrongTrustAdapter, err := NewUniRouterSourceEvidenceV1Adapter(
		readSourceEvidenceV1Fixture(t, root, "invalid/source-evidence-trust-set.wrong-key.json"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrongTrustAdapter.VerifyAndOpen(
		readSourceEvidenceV1Fixture(t, root, "valid/source-evidence-package.tar.gz"),
		policy,
	); err == nil {
		t.Fatal("wrong-key source-evidence trust set authorized the accepted package")
	}
}

func TestUniRouterSourceEvidenceV1AdapterRejectsWrongAuthorityBeforePackageDecode(t *testing.T) {
	root := uniRouterSourceEvidenceV1FixtureRoot(t)
	adapter, err := NewUniRouterSourceEvidenceV1Adapter(
		readSourceEvidenceV1Fixture(t, root, "valid/source-evidence-trust-set.json"),
	)
	if err != nil {
		t.Fatal(err)
	}
	policy := sourceEvidenceV1FixtureVerificationRequest([]byte("unused")).Trust
	for _, mutate := range []func(*SourceEvidenceTrustPolicy){
		func(value *SourceEvidenceTrustPolicy) { value.Purpose = "upgrade" },
		func(value *SourceEvidenceTrustPolicy) { value.Domain = "unirouter.upgrade.v1" },
		func(value *SourceEvidenceTrustPolicy) { value.SigningKeyRef = "source-evidence-other-01" },
		func(value *SourceEvidenceTrustPolicy) {
			value.SignerPublicKeyID = "ed25519:sha256:" + strings.Repeat("0", 64)
		},
		func(value *SourceEvidenceTrustPolicy) { value.TrustSetSHA256 = "sha256:" + strings.Repeat("0", 64) },
	} {
		drifted := policy
		mutate(&drifted)
		_, err := adapter.VerifyAndOpen([]byte("not-a-package"), drifted)
		if err == nil || strings.Contains(err.Error(), "open UniRouter source-evidence package") {
			t.Fatalf("authority drift reached package decoder: %v", err)
		}
	}
}

func TestUniRouterMatchedProducerFixturesRemainSyntheticCompatibilityOnly(t *testing.T) {
	root := uniRouterSourceEvidenceV1FixtureRoot(t)
	index := readSourceEvidenceV1ConformanceIndex(t, root)
	if index.FixtureAuthority != "synthetic_conformance_only" {
		t.Fatalf("fixture authority=%q", index.FixtureAuthority)
	}
	for relative, expected := range map[string]string{
		"valid/xinsight/component-contract.json":             "sha256:2f8b817a726356235131fc63b80b1b320bc5a300e8471e64516b7cac523a40e1",
		"valid/xinsight/component-scope-receipt.json":        "sha256:3d94607ebd000558d3877b6b3b9973d62a7a399e23567e1e2960bf3f85e99f23",
		"valid/xinsight/component-verification-receipt.json": "sha256:71bedea6a9f5bdd297ac97cf218ba7a05f751827308b3bc9ab850934535c6a17",
		"valid/xinsight/product-release-identity.json":       "sha256:e391cdbc21a0e4b439c10c32164e749c28c51658269bf2505840e8133b7a1a27",
		"valid/xinsight/verifier-release.json":               "sha256:297ecfeed7a062ab8e52e3ae98ef82d47fa9d8c121d30f051f7d25be7504497e",
		"valid/xinsight-ack.json":                            "sha256:1ec9bc8601e8221bff01b343f4784af8cdd84a1a858facbe71edb9ccc65056d6",
	} {
		if actual := sourceEvidenceV1TestSHA256(readSourceEvidenceV1Fixture(t, root, relative)); actual != expected {
			t.Fatalf("opaque compatibility artifact %q digest=%s", relative, actual)
		}
	}
}

func sourceEvidenceV1FixtureVerificationRequest(packageBytes []byte) CampaignPackageVerificationRequest {
	packageSHA256 := sourceEvidenceV1TestSHA256(packageBytes)
	return CampaignPackageVerificationRequest{
		ExpectedContract: CampaignPackageContractIdentity{
			ID: UniRouterSourceEvidenceV1ContractID, SHA256: UniRouterSourceEvidenceV1ContractSHA256,
		},
		ExpectedPackageID:                    packageSHA256,
		ExpectedPackageSHA256:                packageSHA256,
		ExpectedSourceBindingSHA256:          uniRouterSourceEvidenceV1FixtureSourceBindingSHA256,
		ExpectedAuthorizationManifestSHA256:  uniRouterSourceEvidenceV1FixtureManifestSHA256,
		ExpectedAuthorizationSHA256:          uniRouterSourceEvidenceV1FixtureAuthorizationSHA256,
		ExpectedAuthorizationSignatureSHA256: uniRouterSourceEvidenceV1FixtureAuthorizationSignatureSHA256,
		Trust: SourceEvidenceTrustPolicy{
			Purpose:           SourceEvidencePurpose,
			Domain:            SourceEvidenceDomain,
			Algorithm:         SourceEvidenceSignatureEd25519,
			SigningKeyRef:     uniRouterSourceEvidenceV1FixtureKeyRef,
			SignerPublicKeyID: uniRouterSourceEvidenceV1FixturePublicKeyID,
			TrustSetSHA256:    uniRouterSourceEvidenceV1FixtureTrustSHA256,
		},
	}
}

func uniRouterSourceEvidenceV1FixtureRoot(t *testing.T) string {
	t.Helper()
	root := os.Getenv("XINSIGHT_UNIROUTER_SOURCE_EVIDENCE_V1_ROOT")
	if root == "" {
		t.Skip("XINSIGHT_UNIROUTER_SOURCE_EVIDENCE_V1_ROOT is required for immutable UniRouter compatibility")
	}
	return filepath.Join(root, "contracts", "source-evidence", "v1", "fixtures", "conformance")
}

func readSourceEvidenceV1ConformanceIndex(t *testing.T, root string) sourceEvidenceV1ConformanceIndex {
	t.Helper()
	payload := readSourceEvidenceV1Fixture(t, root, "index.json")
	if sourceEvidenceV1TestSHA256(payload) != UniRouterSourceEvidenceV1IndexSHA256 {
		t.Fatal("immutable UniRouter conformance index digest drifted")
	}
	var index sourceEvidenceV1ConformanceIndex
	if err := json.Unmarshal(payload, &index); err != nil {
		t.Fatal(err)
	}
	return index
}

func readSourceEvidenceV1Fixture(t *testing.T, root, relative string) []byte {
	t.Helper()
	clean := filepath.Clean(relative)
	if clean != relative || filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		t.Fatalf("unsafe fixture path %q", relative)
	}
	payload, err := os.ReadFile(filepath.Join(root, clean))
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func sourceEvidenceV1TestSHA256(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func TestUniRouterSourceEvidenceV1AdapterDoesNotRetainInputBytes(t *testing.T) {
	root := uniRouterSourceEvidenceV1FixtureRoot(t)
	trust := readSourceEvidenceV1Fixture(t, root, "valid/source-evidence-trust-set.json")
	adapter, err := NewUniRouterSourceEvidenceV1Adapter(trust)
	if err != nil {
		t.Fatal(err)
	}
	before := adapter.trustSetSHA256
	for index := range trust {
		trust[index] = 'x'
	}
	if adapter.trustSetSHA256 != before {
		t.Fatal("adapter retained mutable trust-set input bytes")
	}
}

func TestUniRouterSourceEvidenceV1AdapterAcceptsProducerKeyRefWithDot(t *testing.T) {
	adapter, packageBytes, policy := sourceEvidenceV1SignedTestPackage(
		t,
		"source-evidence-conformance.key-04",
		func(body []byte) []byte { return body },
	)
	opened, err := adapter.VerifyAndOpen(packageBytes, policy)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Projection.Signature.SigningKeyRef != policy.SigningKeyRef ||
		!opened.Projection.PackageScopeComplete {
		t.Fatalf("projection=%+v", opened.Projection)
	}
}

func TestUniRouterSourceEvidenceV1AdapterRejectsOutOfContractKeyRefs(t *testing.T) {
	for _, keyRef := range []string{
		"source-evidence-",
		"source-evidence-" + strings.Repeat("a", 49),
		"source-evidence-bad/key",
		"upgrade-source-evidence-key",
	} {
		t.Run(keyRef, func(t *testing.T) {
			trustSet := sourceEvidenceV1EphemeralTrustSet(t, keyRef)
			if _, err := NewUniRouterSourceEvidenceV1Adapter(trustSet); err == nil {
				t.Fatalf("out-of-contract key ref %q was accepted", keyRef)
			}
		})
	}
}

func TestUniRouterSourceEvidenceV1AdapterRejectsSignedInvalidSourceFactsSemantics(t *testing.T) {
	tests := []struct {
		name   string
		want   string
		mutate func(*sourceEvidenceV1SourceFacts)
	}{
		{
			name: "release identity",
			want: "release identity",
			mutate: func(facts *sourceEvidenceV1SourceFacts) {
				facts.Release.ReleaseSequence = 0
			},
		},
		{
			name: "protocol identity",
			want: "protocol components",
			mutate: func(facts *sourceEvidenceV1SourceFacts) {
				facts.Protocol.Components[0].Name = "Invalid-Stage"
			},
		},
		{
			name: "fact bound",
			want: "payload-neutral execution",
			mutate: func(facts *sourceEvidenceV1SourceFacts) {
				call := sourceEvidenceV1ValidCallLog()
				call.Model = strings.Repeat("m", 201)
				call.FactSHA256 = sourceEvidenceV1TestSemanticSHA256(call)
				sourceEvidenceV1SetCallLog(facts, call)
			},
		},
		{
			name: "semantic hash",
			want: "fact_sha256",
			mutate: func(facts *sourceEvidenceV1SourceFacts) {
				call := sourceEvidenceV1ValidCallLog()
				call.FactSHA256 = "sha256:" + strings.Repeat("0", 64)
				sourceEvidenceV1SetCallLog(facts, call)
			},
		},
		{
			name: "access binding",
			want: "access binding",
			mutate: func(facts *sourceEvidenceV1SourceFacts) {
				attempt := sourceEvidenceV1ValidProviderAttempt()
				attempt.AccessBindingType = "unscoped_credential"
				attempt.FactSHA256 = sourceEvidenceV1TestSemanticSHA256(attempt)
				sourceEvidenceV1SetProviderAttempt(facts, attempt)
			},
		},
		{
			name: "failure kind",
			want: "failure_kind",
			mutate: func(facts *sourceEvidenceV1SourceFacts) {
				attempt := sourceEvidenceV1ValidProviderAttempt()
				attempt.FailureKind = "private_provider_error"
				attempt.FactSHA256 = sourceEvidenceV1TestSemanticSHA256(attempt)
				sourceEvidenceV1SetProviderAttempt(facts, attempt)
			},
		},
		{
			name: "call feed consistency",
			want: "terminal facts conflict",
			mutate: func(facts *sourceEvidenceV1SourceFacts) {
				call := sourceEvidenceV1ValidCallLog()
				feed := sourceEvidenceV1ValidCompletedFeed(call)
				feed.ContentSummary.Model = "different-model"
				feed.FactSHA256 = sourceEvidenceV1TestSemanticSHA256(feed)
				sourceEvidenceV1SetCallAndFeed(facts, call, feed)
			},
		},
		{
			name: "capture gap tuple",
			want: "capture_gaps",
			mutate: func(facts *sourceEvidenceV1SourceFacts) {
				facts.Requests[0].CaptureGaps[0].Reason = "producer-defined reason drift"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter, packageBytes, policy := sourceEvidenceV1SignedTypedTestPackage(t, test.mutate)
			if _, err := adapter.VerifyAndOpen(packageBytes, policy); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want substring %q", err, test.want)
			}
		})
	}
}

func TestUniRouterSourceEvidenceV1AdapterRejectsSignedInvalidFactType(t *testing.T) {
	adapter, packageBytes, policy := sourceEvidenceV1SignedTestPackage(
		t,
		"source-evidence-invalid-type",
		func(body []byte) []byte {
			var facts map[string]any
			if err := json.Unmarshal(body, &facts); err != nil {
				t.Fatal(err)
			}
			requests := facts["requests"].([]any)
			request := requests[0].(map[string]any)
			request["call_log"] = map[string]any{"prompt_tokens": "not-an-integer"}
			return sourceEvidenceV1TestMarshal(t, facts)
		},
	)
	if _, err := adapter.VerifyAndOpen(packageBytes, policy); err == nil ||
		!strings.Contains(err.Error(), "cannot unmarshal string") {
		t.Fatalf("error=%v want strict fact type rejection", err)
	}
}

func TestUniRouterSourceEvidenceV1AdapterRejectsPrivateSourceFactLocatorsBeforeImporter(t *testing.T) {
	privateLocators := []string{
		"prompt",
		"response",
		"authorization",
		"api_key",
		"credential",
		"ip_address",
		"upstream_error",
		"actor_ref",
		"on_behalf_of",
		"api_key_id",
		"access_credential_id",
		"credential_ref",
		"access_grant_id",
		"channel_id",
		"channel_key_id",
		"upstream_request_id",
	}
	for _, locator := range privateLocators {
		t.Run(locator, func(t *testing.T) {
			adapter, packageBytes, policy := sourceEvidenceV1SignedTestPackage(
				t,
				"source-evidence-private-locator",
				func(body []byte) []byte {
					var facts map[string]any
					if err := json.Unmarshal(body, &facts); err != nil {
						t.Fatal(err)
					}
					requests := facts["requests"].([]any)
					request := requests[0].(map[string]any)
					request["call_log"] = map[string]any{locator: "private-locator"}
					return sourceEvidenceV1TestMarshal(t, facts)
				},
			)
			if _, err := adapter.VerifyAndOpen(packageBytes, policy); err == nil ||
				!strings.Contains(err.Error(), "prohibited raw field") {
				t.Fatalf("error=%v want pre-import private-locator rejection", err)
			}
		})
	}
}

func sourceEvidenceV1SignedTypedTestPackage(
	t *testing.T,
	mutate func(*sourceEvidenceV1SourceFacts),
) (*UniRouterSourceEvidenceV1Adapter, []byte, SourceEvidenceTrustPolicy) {
	t.Helper()
	return sourceEvidenceV1SignedTestPackage(
		t,
		"source-evidence-semantic-test",
		func(body []byte) []byte {
			facts, err := decodeCanonicalSourceEvidenceV1[sourceEvidenceV1SourceFacts](body, "test Source facts")
			if err != nil {
				t.Fatal(err)
			}
			mutate(&facts)
			return sourceEvidenceV1TestMarshal(t, facts)
		},
	)
}

func sourceEvidenceV1SignedTestPackage(
	t *testing.T,
	keyRef string,
	mutateFacts func([]byte) []byte,
) (*UniRouterSourceEvidenceV1Adapter, []byte, SourceEvidenceTrustPolicy) {
	t.Helper()
	root := uniRouterSourceEvidenceV1FixtureRoot(t)
	entries, order, err := openCanonicalSourceEvidenceV1Container(
		readSourceEvidenceV1Fixture(t, root, "valid/source-evidence-package.tar.gz"),
	)
	if err != nil {
		t.Fatal(err)
	}
	entries[uniRouterSourceEvidenceV1SourceFactsPath] = mutateFacts(
		bytes.Clone(entries[uniRouterSourceEvidenceV1SourceFactsPath]),
	)

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKeyID := sourceEvidenceV1PublicKeyID(publicKey)
	trustSet := sourceEvidenceV1TrustSet{
		SchemaVersion: uniRouterSourceEvidenceV1TrustSetSchema,
		Purpose:       SourceEvidencePurpose,
		Domain:        SourceEvidenceDomain,
		Keys: []sourceEvidenceV1TrustKey{{
			SigningKeyRef:     keyRef,
			SignerPublicKeyID: publicKeyID,
			Algorithm:         SourceEvidenceSignatureEd25519,
			PublicKey:         base64.StdEncoding.EncodeToString(publicKey),
		}},
	}
	trustSetBody := sourceEvidenceV1TestMarshal(t, trustSet)
	trustSetSHA256 := campaignPackageSHA256(trustSetBody)

	manifest, err := decodeCanonicalSourceEvidenceV1[sourceEvidenceV1Manifest](
		entries[uniRouterSourceEvidenceV1ManifestPath],
		"test manifest",
	)
	if err != nil {
		t.Fatal(err)
	}
	manifest.SigningKeyRef = keyRef
	manifest.SignerPublicKeyID = publicKeyID
	manifest.TrustSetSHA256 = trustSetSHA256
	manifest.SourceFactsSHA256 = campaignPackageSHA256(entries[uniRouterSourceEvidenceV1SourceFactsPath])
	for index := range manifest.Artifacts {
		if manifest.Artifacts[index].Path == uniRouterSourceEvidenceV1SourceFactsPath {
			manifest.Artifacts[index].Size = int64(len(entries[uniRouterSourceEvidenceV1SourceFactsPath]))
			manifest.Artifacts[index].SHA256 = manifest.SourceFactsSHA256
		}
	}
	manifestBody := sourceEvidenceV1TestMarshal(t, manifest)
	entries[uniRouterSourceEvidenceV1ManifestPath] = manifestBody

	signature := sourceEvidenceV1Signature{
		SchemaVersion:     uniRouterSourceEvidenceV1SignatureSchema,
		Purpose:           SourceEvidencePurpose,
		Domain:            SourceEvidenceDomain,
		SigningKeyRef:     keyRef,
		SignerPublicKeyID: publicKeyID,
		TrustSetSHA256:    trustSetSHA256,
		Algorithm:         SourceEvidenceSignatureEd25519,
		Value: base64.StdEncoding.EncodeToString(ed25519.Sign(
			privateKey,
			append([]byte(SourceEvidenceDomain+"\x00"+SourceEvidencePurpose+"\x00"), manifestBody...),
		)),
	}
	for index := range privateKey {
		privateKey[index] = 0
	}
	entries[uniRouterSourceEvidenceV1SignaturePath] = sourceEvidenceV1TestMarshal(t, signature)

	createdAt, err := parseSourceEvidenceV1Time(manifest.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	packageBytes := sourceEvidenceV1TestCanonicalPackage(t, order, entries, createdAt)
	adapter, err := NewUniRouterSourceEvidenceV1Adapter(trustSetBody)
	if err != nil {
		t.Fatal(err)
	}
	return adapter, packageBytes, SourceEvidenceTrustPolicy{
		Purpose:           SourceEvidencePurpose,
		Domain:            SourceEvidenceDomain,
		Algorithm:         SourceEvidenceSignatureEd25519,
		SigningKeyRef:     keyRef,
		SignerPublicKeyID: publicKeyID,
		TrustSetSHA256:    trustSetSHA256,
	}
}

func sourceEvidenceV1EphemeralTrustSet(t *testing.T, keyRef string) []byte {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for index := range privateKey {
		privateKey[index] = 0
	}
	return sourceEvidenceV1TestMarshal(t, sourceEvidenceV1TrustSet{
		SchemaVersion: uniRouterSourceEvidenceV1TrustSetSchema,
		Purpose:       SourceEvidencePurpose,
		Domain:        SourceEvidenceDomain,
		Keys: []sourceEvidenceV1TrustKey{{
			SigningKeyRef:     keyRef,
			SignerPublicKeyID: sourceEvidenceV1PublicKeyID(publicKey),
			Algorithm:         SourceEvidenceSignatureEd25519,
			PublicKey:         base64.StdEncoding.EncodeToString(publicKey),
		}},
	})
}

func sourceEvidenceV1TestCanonicalPackage(
	t *testing.T,
	order []string,
	entries map[string][]byte,
	createdAt time.Time,
) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	gzipWriter.Name = ""
	gzipWriter.Comment = ""
	gzipWriter.ModTime = createdAt.UTC()
	gzipWriter.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	for _, name := range order {
		body := entries[name]
		header := &tar.Header{
			Name: name, Mode: 0o600, Size: int64(len(body)), ModTime: createdAt.UTC(),
			Typeflag: tar.TypeReg, Format: tar.FormatUSTAR,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func sourceEvidenceV1TestMarshal(t *testing.T, value any) []byte {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func sourceEvidenceV1ValidCallLog() sourceEvidenceV1CallLogFact {
	fact := sourceEvidenceV1CallLogFact{
		SourceRef:          "call_log:1",
		ID:                 1,
		Model:              "model-a",
		RequestedModel:     "model-a",
		Provider:           "provider-a",
		PromptTokens:       10,
		CompletionTokens:   5,
		TotalTokens:        15,
		CostMicros:         "15",
		Currency:           "CNY",
		AmountScale:        1_000_000,
		UsageSource:        "provider_reported",
		UsageAuthority:     "provider",
		UsageFinality:      "final",
		StatusCode:         200,
		LatencyMS:          1,
		PayloadCaptureMode: "metadata_only",
		CreatedAt:          "2030-01-01T00:00:00Z",
	}
	fact.FactSHA256 = sourceEvidenceV1TestSemanticSHA256(fact)
	return fact
}

func sourceEvidenceV1ValidProviderAttempt() sourceEvidenceV1ProviderAttempt {
	fact := sourceEvidenceV1ProviderAttempt{
		SourceRef:                          "provider_attempt:attempt-1",
		AttemptID:                          "attempt-1",
		AccessBindingType:                  "logical_access_credential",
		LogicalCredentialCorrelationSHA256: "sha256:" + strings.Repeat("7", 64),
		RouteSequence:                      1,
		RouteTotal:                         1,
		Endpoint:                           "provider-endpoint",
		Provider:                           "provider-a",
		State:                              "completed",
		OutcomeCertainty:                   "known",
		StartedAt:                          "2030-01-01T00:00:00Z",
		UpdatedAt:                          "2030-01-01T00:00:01Z",
	}
	fact.FactSHA256 = sourceEvidenceV1TestSemanticSHA256(fact)
	return fact
}

func sourceEvidenceV1ValidCompletedFeed(call sourceEvidenceV1CallLogFact) sourceEvidenceV1CompletedFeed {
	fact := sourceEvidenceV1CompletedFeed{
		SourceRef:           "completed_feed_item:1",
		SequenceID:          1,
		TerminalStatus:      "settled",
		TerminalAvailableAt: "2030-01-01T00:00:02Z",
		Revision:            "1",
		SnapshotSHA256:      "sha256:" + strings.Repeat("8", 64),
		ContentSummary: sourceEvidenceV1FeedContentSummary{
			Model:                call.Model,
			PromptTokens:         int(call.PromptTokens),
			CompletionTokens:     int(call.CompletionTokens),
			UsageAuthority:       call.UsageAuthority,
			UsageFinality:        call.UsageFinality,
			CalculatedCostMicros: call.CostMicros,
			ChargedCostMicros:    call.CostMicros,
			PricingVersion:       "pricing-v1",
		},
	}
	fact.FactSHA256 = sourceEvidenceV1TestSemanticSHA256(fact)
	return fact
}

func sourceEvidenceV1SetCallLog(facts *sourceEvidenceV1SourceFacts, call sourceEvidenceV1CallLogFact) {
	facts.Requests[0].CallLog = &call
	facts.Counts.CallLogs = 1
	facts.Counts.TotalFacts++
}

func sourceEvidenceV1SetProviderAttempt(
	facts *sourceEvidenceV1SourceFacts,
	attempt sourceEvidenceV1ProviderAttempt,
) {
	facts.Requests[0].ProviderAttempts = []sourceEvidenceV1ProviderAttempt{attempt}
	facts.Counts.ProviderAttempts = 1
	facts.Counts.TotalFacts++
}

func sourceEvidenceV1SetCallAndFeed(
	facts *sourceEvidenceV1SourceFacts,
	call sourceEvidenceV1CallLogFact,
	feed sourceEvidenceV1CompletedFeed,
) {
	facts.Requests[0].CallLog = &call
	facts.Requests[0].CompletedFeed = &feed
	facts.Counts.CallLogs = 1
	facts.Counts.CompletedFeedItems = 1
	facts.Counts.TotalFacts += 2
	facts.FeedRange.SelectedItems = 1
	facts.FeedRange.MinSequenceID = &feed.SequenceID
	facts.FeedRange.MaxSequenceID = &feed.SequenceID
}

func sourceEvidenceV1TestSemanticSHA256(value any) string {
	switch fact := value.(type) {
	case sourceEvidenceV1CallLogFact:
		fact.FactSHA256 = ""
		value = fact
	case sourceEvidenceV1TraceFact:
		fact.FactSHA256 = ""
		value = fact
	case sourceEvidenceV1ProviderAttempt:
		fact.FactSHA256 = ""
		value = fact
	case sourceEvidenceV1CompletedFeed:
		fact.FactSHA256 = ""
		value = fact
	}
	return sourceEvidenceV1TestSHA256(sourceEvidenceV1TestMarshalNoError(value))
}

func sourceEvidenceV1TestMarshalNoError(value any) []byte {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return payload
}
