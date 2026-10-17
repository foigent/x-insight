package integrationcontrol

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	unirouteradapter "github.com/foigent/x-insight/internal/adapters/unirouter"
)

const (
	FrozenN1Version               = "v0.1.0-candidate.1"
	FrozenN1SourceCommit          = "12f1ede8ae85e9d311a8150694eea54ff6f02950"
	FrozenN1SourceTree            = "e6945fa071afd6a0b9317d3ec862cbcbf47a0154"
	FrozenN1ReleaseCommit         = "c9e5a77dd0416654d8e46efb09f18a6ecca026f8"
	FrozenN1ReleaseTree           = "325863188f625c07e9f10a7a3eae67f9574de198"
	FrozenN1ReleaseManifestSHA256 = "sha256:0604100b8cb8c8a728fc091d1c9269eafbc3cb70449222ab6c10657bca33d3e5"
	FrozenN1ContractsSHA256       = "sha256:a3697170a39aa97f81d690e2532b19383d1f77eb7b59c3273e37a3f04b24e186"
	FrozenN1ValidFixturesSHA256   = "sha256:a5df1bc8c42f01d71f41c2dd6706994549920573a448d95e699fff9d39b2fa46"
	FrozenN1InvalidFixturesSHA256 = "sha256:850df4fb845acc40770cb98fd6d6c9638599dba30955e3efd4b899dbc9fcfad8"

	n1MaximumReleaseManifestBytes = 1 << 20
	n1MaximumInputBytes           = 4 << 20
	n1MaximumReportBytes          = 64 << 10

	n1UniRouterProduct      = "unirouter"
	n1UniRouterProducerRole = "source_evidence_producer"
	n1SourceBindingVersion  = "unirouter.source-evidence.source-scope.v1"
)

type N1ArtifactPin struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256"`
}

type N1ReleasePin struct {
	Version               string          `json:"version"`
	SourceCommit          string          `json:"source_commit"`
	SourceTree            string          `json:"source_tree"`
	ReleaseCommit         string          `json:"release_commit"`
	ReleaseTree           string          `json:"release_tree"`
	ReleaseManifestSHA256 string          `json:"release_manifest_sha256"`
	ContractsSHA256       string          `json:"contracts_sha256"`
	ValidFixturesSHA256   string          `json:"valid_fixtures_sha256"`
	InvalidFixturesSHA256 string          `json:"invalid_fixtures_sha256"`
	CapabilityBoundary    string          `json:"capability_boundary"`
	Artifacts             []N1ArtifactPin `json:"artifacts"`
}

func FrozenN1ReleasePin() N1ReleasePin {
	return N1ReleasePin{
		Version:               FrozenN1Version,
		SourceCommit:          FrozenN1SourceCommit,
		SourceTree:            FrozenN1SourceTree,
		ReleaseCommit:         FrozenN1ReleaseCommit,
		ReleaseTree:           FrozenN1ReleaseTree,
		ReleaseManifestSHA256: FrozenN1ReleaseManifestSHA256,
		ContractsSHA256:       FrozenN1ContractsSHA256,
		ValidFixturesSHA256:   FrozenN1ValidFixturesSHA256,
		InvalidFixturesSHA256: FrozenN1InvalidFixturesSHA256,
		CapabilityBoundary:    CapabilityOfflineFiles,
		Artifacts:             frozenN1ArtifactPins(),
	}
}

func frozenN1ArtifactPins() []N1ArtifactPin {
	return []N1ArtifactPin{
		{
			OS: "darwin", Architecture: "arm64", SizeBytes: 4034066,
			SHA256: "sha256:ba574ac25abd063914d785a34d097b14a1cf09021a953c40803414900c1f3dc1",
		},
		{
			OS: "linux", Architecture: "amd64", SizeBytes: 4132939,
			SHA256: "sha256:b37a9b92ef237844327911b9a4b1a1266b2970f3acbc50de428aa7d518a1fbb8",
		},
		{
			OS: "source", Architecture: "source", SizeBytes: 235520,
			SHA256: "sha256:360cbc70cbbe84361c23e9f89f3aeed2689b235197b2ebcb7b436187c9aedfef",
		},
	}
}

func (pin N1ReleasePin) Validate() error {
	if pin.Version != FrozenN1Version ||
		pin.SourceCommit != FrozenN1SourceCommit ||
		pin.SourceTree != FrozenN1SourceTree ||
		pin.ReleaseCommit != FrozenN1ReleaseCommit ||
		pin.ReleaseTree != FrozenN1ReleaseTree ||
		pin.ReleaseManifestSHA256 != FrozenN1ReleaseManifestSHA256 ||
		pin.ContractsSHA256 != FrozenN1ContractsSHA256 ||
		pin.ValidFixturesSHA256 != FrozenN1ValidFixturesSHA256 ||
		pin.InvalidFixturesSHA256 != FrozenN1InvalidFixturesSHA256 ||
		pin.CapabilityBoundary != CapabilityOfflineFiles || len(pin.Artifacts) != 3 {
		return errors.New("neutral N1 release pin is invalid")
	}
	expected := frozenN1ArtifactPins()
	seen := make(map[string]struct{}, len(pin.Artifacts))
	for index, artifact := range pin.Artifacts {
		key := artifact.OS + "/" + artifact.Architecture
		if _, duplicate := seen[key]; duplicate || artifact.SizeBytes <= 0 ||
			!digestPattern.MatchString(artifact.SHA256) || expected[index] != artifact {
			return errors.New("neutral N1 artifact pin is invalid")
		}
		seen[key] = struct{}{}
	}
	for _, artifact := range expected {
		required := artifact.OS + "/" + artifact.Architecture
		if _, exists := seen[required]; !exists {
			return errors.New("neutral N1 release pin is incomplete")
		}
	}
	return nil
}

func (pin N1ReleasePin) Artifact(osName, architecture string) (N1ArtifactPin, error) {
	if err := pin.Validate(); err != nil {
		return N1ArtifactPin{}, err
	}
	for _, artifact := range pin.Artifacts {
		if artifact.OS == osName && artifact.Architecture == architecture {
			return artifact, nil
		}
	}
	return N1ArtifactPin{}, errors.New("neutral N1 has no artifact for the requested platform")
}

// N1Verifier is a fixed, verify-only adapter. Paths are process-local inputs;
// they are never serialized into X-Insight contracts or receipts.
type N1Verifier struct {
	binaryPath string
	artifact   N1ArtifactPin
}

func NewN1Verifier(releaseManifestPath, binaryPath string) (*N1Verifier, error) {
	pin := FrozenN1ReleasePin()
	if err := pin.Validate(); err != nil {
		return nil, err
	}
	if _, err := readExactFile(
		releaseManifestPath,
		n1MaximumReleaseManifestBytes,
		0,
		pin.ReleaseManifestSHA256,
	); err != nil {
		return nil, fmt.Errorf("verify neutral N1 release manifest: %w", err)
	}
	artifact, err := pin.Artifact(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	if _, err := readExactFile(binaryPath, artifact.SizeBytes, artifact.SizeBytes, artifact.SHA256); err != nil {
		return nil, fmt.Errorf("verify neutral N1 verifier artifact: %w", err)
	}
	return &N1Verifier{binaryPath: binaryPath, artifact: artifact}, nil
}

func (verifier *N1Verifier) VerifySourceArchive(path string) error {
	pin := FrozenN1ReleasePin()
	artifact, err := pin.Artifact("source", "source")
	if err != nil {
		return err
	}
	_, err = readExactFile(path, artifact.SizeBytes, artifact.SizeBytes, artifact.SHA256)
	return err
}

type N1ManifestVerification struct {
	Valid                  bool                            `json:"valid"`
	CampaignManifestSHA256 string                          `json:"campaign_manifest_sha256"`
	CampaignID             string                          `json:"-"`
	FrozenAt               string                          `json:"-"`
	XInsightComponent      N1ComponentIdentity             `json:"-"`
	UniRouterComponent     N1ComponentIdentity             `json:"-"`
	SourceBinding          ArtifactReference               `json:"-"`
	SourceEvidenceSigning  N1SourceEvidenceSigningIdentity `json:"-"`
	verificationSeal       string
}

// N1ComponentIdentity is the narrow post-verification projection X-Insight
// needs from one component entry in M. It is not an editable copy of the
// neutral manifest schema.
type N1ComponentIdentity struct {
	Product      string            `json:"product"`
	Role         string            `json:"role"`
	Contract     ContractReference `json:"contract"`
	Release      ArtifactReference `json:"release"`
	ScopeReceipt ArtifactReference `json:"scope_receipt"`
}

// N1SourceEvidenceSigningIdentity retains all Source authority facts fixed by
// the exact N1-verified M. The Ed25519 algorithm is implied by the public-key
// identifier and by X-Insight's fixed package-verifier policy.
type N1SourceEvidenceSigningIdentity struct {
	Purpose           string `json:"purpose"`
	Domain            string `json:"domain"`
	SigningKeyRef     string `json:"signing_key_ref"`
	SignerPublicKeyID string `json:"signer_public_key_id"`
	TrustSetSHA256    string `json:"trust_set_sha256"`
}

type N1AcknowledgementVerification struct {
	Valid                 bool   `json:"valid"`
	Product               string `json:"product"`
	AcknowledgementSHA256 string `json:"acknowledgement_sha256"`
}

type N1AuthorizationFiles struct {
	Manifest                 string
	UniRouterAcknowledgement string
	XInsightAcknowledgement  string
	Authorization            string
	Signature                string
	AuthorizationTrustSet    string
	SourceEvidenceTrustSet   string
}

type N1AuthorizationVerification struct {
	Valid                        bool                            `json:"valid"`
	CampaignManifestSHA256       string                          `json:"campaign_manifest_sha256"`
	AcknowledgementSHA256        map[string]string               `json:"acknowledgement_sha256"`
	AuthorizationSHA256          string                          `json:"authorization_sha256"`
	AuthorizationSignatureSHA256 string                          `json:"authorization_signature_sha256"`
	AuthorizationTrustSetSHA256  string                          `json:"authorization_trust_set_sha256"`
	SourceEvidenceTrustSetSHA256 string                          `json:"source_evidence_trust_set_sha256"`
	CapabilityBoundary           string                          `json:"capability_boundary"`
	UniRouterComponent           N1ComponentIdentity             `json:"-"`
	SourceBinding                ArtifactReference               `json:"-"`
	SourceEvidenceSigning        N1SourceEvidenceSigningIdentity `json:"-"`
	AuthorizationWindow          N1AuthorizationWindow           `json:"-"`
	verificationSeal             string
}

// N1AuthorizationWindow is the only chronology slice X-Insight retains from
// an authorization that the immutable N1 verifier has already accepted. The
// half-open interval is sealed into N1AuthorizationVerification and is never
// accepted as an independent operator input.
type N1AuthorizationWindow struct {
	NotBefore string `json:"not_before"`
	NotAfter  string `json:"not_after"`
}

func (verifier *N1Verifier) VerifyManifest(
	ctx context.Context,
	manifestPath string,
) (N1ManifestVerification, error) {
	manifest, err := readInputArtifact(manifestPath)
	if err != nil {
		return N1ManifestVerification{}, errors.New("read neutral N1 manifest input")
	}
	output, err := verifier.run(ctx, "manifest", "verify", "--manifest", manifestPath)
	if err != nil {
		return N1ManifestVerification{}, err
	}
	report, err := decodeN1Report[N1ManifestVerification](output)
	if err != nil {
		return N1ManifestVerification{}, err
	}
	if !report.Valid || report.CampaignManifestSHA256 != bytesSHA256(manifest) {
		return N1ManifestVerification{}, errors.New("neutral N1 manifest report contradicts its exact input")
	}
	authority, err := projectN1ManifestAuthority(manifest)
	if err != nil {
		return N1ManifestVerification{}, err
	}
	report.CampaignID = authority.CampaignID
	report.FrozenAt = authority.FrozenAt
	report.XInsightComponent = authority.XInsightComponent
	report.UniRouterComponent = authority.UniRouterComponent
	report.SourceBinding = authority.SourceBinding
	report.SourceEvidenceSigning = authority.SourceEvidenceSigning
	report.verificationSeal = report.expectedSeal()
	if err := report.validateVerified(); err != nil {
		return N1ManifestVerification{}, err
	}
	return report, nil
}

func (verifier *N1Verifier) VerifyAcknowledgement(
	ctx context.Context,
	manifestPath string,
	acknowledgementPath string,
) (N1AcknowledgementVerification, error) {
	if _, err := readInputArtifact(manifestPath); err != nil {
		return N1AcknowledgementVerification{}, errors.New("read neutral N1 manifest input")
	}
	acknowledgement, err := readInputArtifact(acknowledgementPath)
	if err != nil {
		return N1AcknowledgementVerification{}, errors.New("read neutral N1 acknowledgement input")
	}
	output, err := verifier.run(
		ctx,
		"ack", "verify",
		"--manifest", manifestPath,
		"--ack", acknowledgementPath,
	)
	if err != nil {
		return N1AcknowledgementVerification{}, err
	}
	report, err := decodeN1Report[N1AcknowledgementVerification](output)
	if err != nil {
		return N1AcknowledgementVerification{}, err
	}
	if !report.Valid || report.Product != ProductXInsight ||
		report.AcknowledgementSHA256 != bytesSHA256(acknowledgement) {
		return N1AcknowledgementVerification{}, errors.New("neutral N1 acknowledgement report contradicts its exact input")
	}
	return report, nil
}

func (verifier *N1Verifier) VerifyAuthorization(
	ctx context.Context,
	files N1AuthorizationFiles,
) (N1AuthorizationVerification, error) {
	manifest, err := readInputArtifact(files.Manifest)
	if err != nil {
		return N1AuthorizationVerification{}, errors.New("read neutral N1 manifest input")
	}
	uniRouterAck, err := readInputArtifact(files.UniRouterAcknowledgement)
	if err != nil {
		return N1AuthorizationVerification{}, errors.New("read neutral N1 UniRouter acknowledgement input")
	}
	xInsightAck, err := readInputArtifact(files.XInsightAcknowledgement)
	if err != nil {
		return N1AuthorizationVerification{}, errors.New("read neutral N1 X-Insight acknowledgement input")
	}
	authorization, err := readInputArtifact(files.Authorization)
	if err != nil {
		return N1AuthorizationVerification{}, errors.New("read neutral N1 authorization input")
	}
	signature, err := readInputArtifact(files.Signature)
	if err != nil {
		return N1AuthorizationVerification{}, errors.New("read neutral N1 authorization signature input")
	}
	authorizationTrust, err := readInputArtifact(files.AuthorizationTrustSet)
	if err != nil {
		return N1AuthorizationVerification{}, errors.New("read neutral N1 authorization trust-set input")
	}
	sourceEvidenceTrust, err := readInputArtifact(files.SourceEvidenceTrustSet)
	if err != nil {
		return N1AuthorizationVerification{}, errors.New("read neutral N1 Source-evidence trust-set input")
	}
	output, err := verifier.run(
		ctx,
		"authorization", "verify",
		"--manifest", files.Manifest,
		"--unirouter-ack", files.UniRouterAcknowledgement,
		"--xinsight-ack", files.XInsightAcknowledgement,
		"--authorization", files.Authorization,
		"--signature", files.Signature,
		"--trust-set", files.AuthorizationTrustSet,
	)
	if err != nil {
		return N1AuthorizationVerification{}, err
	}
	report, err := decodeN1Report[N1AuthorizationVerification](output)
	if err != nil {
		return N1AuthorizationVerification{}, err
	}
	manifestAuthority, err := projectN1ManifestAuthority(manifest)
	if err != nil {
		return N1AuthorizationVerification{}, err
	}
	authorizationWindow, err := projectN1AuthorizationWindow(authorization)
	if err != nil {
		return N1AuthorizationVerification{}, err
	}
	if !report.Valid || report.CapabilityBoundary != CapabilityOfflineFiles ||
		report.CampaignManifestSHA256 != bytesSHA256(manifest) ||
		report.AuthorizationSHA256 != bytesSHA256(authorization) ||
		report.AuthorizationSignatureSHA256 != bytesSHA256(signature) ||
		report.AuthorizationTrustSetSHA256 != bytesSHA256(authorizationTrust) ||
		report.SourceEvidenceTrustSetSHA256 != bytesSHA256(sourceEvidenceTrust) ||
		report.SourceEvidenceTrustSetSHA256 != manifestAuthority.SourceEvidenceSigning.TrustSetSHA256 ||
		len(report.AcknowledgementSHA256) != 2 ||
		report.AcknowledgementSHA256["unirouter"] != bytesSHA256(uniRouterAck) ||
		report.AcknowledgementSHA256[ProductXInsight] != bytesSHA256(xInsightAck) {
		return N1AuthorizationVerification{}, errors.New("neutral N1 authorization report contradicts its exact inputs")
	}
	report.UniRouterComponent = manifestAuthority.UniRouterComponent
	report.SourceBinding = manifestAuthority.SourceBinding
	report.SourceEvidenceSigning = manifestAuthority.SourceEvidenceSigning
	report.AuthorizationWindow = authorizationWindow
	report.verificationSeal = report.expectedSeal()
	if err := report.validateVerified(); err != nil {
		return N1AuthorizationVerification{}, err
	}
	return report, nil
}

// projectN1AuthorizationWindow runs only after the fixed N1 executable has
// accepted the exact authorization bytes. N1 remains the neutral schema and
// interval validator; X-Insight retains only the two canonical endpoints it
// needs to authorize terminal chronology.
func projectN1AuthorizationWindow(payload []byte) (N1AuthorizationWindow, error) {
	if err := rejectDuplicateJSONKeys(payload); err != nil {
		return N1AuthorizationWindow{}, fmt.Errorf("neutral N1 authorization authority: %w", err)
	}
	var document struct {
		AuthorizationWindow N1AuthorizationWindow `json:"authorization_window"`
	}
	if err := json.Unmarshal(payload, &document); err != nil {
		return N1AuthorizationWindow{}, errors.New("neutral N1 authorization authority is invalid")
	}
	if err := document.AuthorizationWindow.validate(); err != nil {
		return N1AuthorizationWindow{}, err
	}
	return document.AuthorizationWindow, nil
}

func (window N1AuthorizationWindow) validate() error {
	if validateCanonicalTimestamp(window.NotBefore) != nil ||
		validateCanonicalTimestamp(window.NotAfter) != nil {
		return errors.New("neutral N1 authorization window is not canonical UTC")
	}
	notBefore, _ := time.Parse(time.RFC3339, window.NotBefore)
	notAfter, _ := time.Parse(time.RFC3339, window.NotAfter)
	if !notBefore.Before(notAfter) {
		return errors.New("neutral N1 authorization window is empty or reversed")
	}
	return nil
}

func (verifier *N1Verifier) run(ctx context.Context, arguments ...string) ([]byte, error) {
	if verifier == nil || verifier.binaryPath == "" {
		return nil, errors.New("neutral N1 verifier is unavailable")
	}
	if _, err := readExactFile(
		verifier.binaryPath,
		verifier.artifact.SizeBytes,
		verifier.artifact.SizeBytes,
		verifier.artifact.SHA256,
	); err != nil {
		return nil, errors.New("neutral N1 verifier artifact changed after initialization")
	}
	command := exec.CommandContext(ctx, verifier.binaryPath, arguments...)
	command.Env = []string{"LANG=C", "LC_ALL=C", "PATH=/usr/bin:/bin"}
	stdout := newBoundedOutput(n1MaximumReportBytes)
	stderr := newBoundedOutput(n1MaximumReportBytes)
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return nil, errors.New("neutral N1 verifier rejected the supplied offline artifacts")
	}
	return bytes.Clone(stdout.Bytes()), nil
}

func decodeN1Report[T any](payload []byte) (T, error) {
	var zero T
	if len(payload) == 0 || len(payload) > n1MaximumReportBytes {
		return zero, errors.New("neutral N1 verifier report size is invalid")
	}
	if err := rejectDuplicateJSONKeys(payload); err != nil {
		return zero, fmt.Errorf("neutral N1 verifier report: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var report T
	if err := decoder.Decode(&report); err != nil {
		return zero, errors.New("neutral N1 verifier report is invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return zero, errors.New("neutral N1 verifier report contains trailing content")
	}
	return report, nil
}

type n1ManifestAuthorityProjection struct {
	CampaignID            string
	FrozenAt              string
	XInsightComponent     N1ComponentIdentity
	UniRouterComponent    N1ComponentIdentity
	SourceBinding         ArtifactReference
	SourceEvidenceSigning N1SourceEvidenceSigningIdentity
}

// projectN1ManifestAuthority runs only after the fixed N1 executable has
// accepted the exact manifest bytes. It deliberately projects only the facts
// X-Insight must retain; N1 remains the sole neutral schema validator.
func projectN1ManifestAuthority(payload []byte) (n1ManifestAuthorityProjection, error) {
	if err := rejectDuplicateJSONKeys(payload); err != nil {
		return n1ManifestAuthorityProjection{}, fmt.Errorf("neutral N1 manifest authority: %w", err)
	}
	var document struct {
		CampaignID            string                          `json:"campaign_id"`
		FrozenAt              string                          `json:"frozen_at"`
		Components            []N1ComponentIdentity           `json:"components"`
		SourceBinding         ArtifactReference               `json:"source_binding"`
		SourceEvidenceSigning N1SourceEvidenceSigningIdentity `json:"source_evidence_signing"`
	}
	if err := json.Unmarshal(payload, &document); err != nil {
		return n1ManifestAuthorityProjection{}, errors.New("neutral N1 manifest authority is invalid")
	}
	if !validPortableText(document.CampaignID, 128) {
		return n1ManifestAuthorityProjection{}, errors.New("neutral N1 manifest campaign identity is invalid")
	}
	if validateCanonicalTimestamp(document.FrozenAt) != nil {
		return n1ManifestAuthorityProjection{}, errors.New("neutral N1 manifest freeze time is invalid")
	}
	var xInsightComponent, uniRouterComponent N1ComponentIdentity
	for _, component := range document.Components {
		switch component.Product {
		case ProductXInsight:
			if xInsightComponent.Product != "" {
				return n1ManifestAuthorityProjection{}, errors.New("neutral N1 manifest repeats the X-Insight component")
			}
			xInsightComponent = component
		case n1UniRouterProduct:
			if uniRouterComponent.Product != "" {
				return n1ManifestAuthorityProjection{}, errors.New("neutral N1 manifest repeats the UniRouter component")
			}
			uniRouterComponent = component
		}
	}
	if err := xInsightComponent.validate(ProductXInsight, ComponentRole); err != nil {
		return n1ManifestAuthorityProjection{}, err
	}
	if xInsightComponent.Contract.ID != ComponentContractVersion {
		return n1ManifestAuthorityProjection{}, errors.New("neutral N1 manifest uses an unknown X-Insight component contract")
	}
	if err := uniRouterComponent.validate(n1UniRouterProduct, n1UniRouterProducerRole); err != nil {
		return n1ManifestAuthorityProjection{}, err
	}
	if err := document.SourceBinding.validate(); err != nil ||
		document.SourceBinding.SchemaVersion != n1SourceBindingVersion {
		return n1ManifestAuthorityProjection{}, errors.New("neutral N1 manifest Source binding is invalid")
	}
	if err := document.SourceEvidenceSigning.validate(); err != nil {
		return n1ManifestAuthorityProjection{}, err
	}
	return n1ManifestAuthorityProjection{
		CampaignID:            document.CampaignID,
		FrozenAt:              document.FrozenAt,
		XInsightComponent:     xInsightComponent,
		UniRouterComponent:    uniRouterComponent,
		SourceBinding:         document.SourceBinding,
		SourceEvidenceSigning: document.SourceEvidenceSigning,
	}, nil
}

func (identity N1ComponentIdentity) validate(product, role string) error {
	if identity.Product != product || identity.Role != role {
		return errors.New("neutral N1 manifest component identity is invalid")
	}
	if err := identity.Contract.validate(); err != nil {
		return err
	}
	if err := identity.Release.validate(); err != nil {
		return err
	}
	return identity.ScopeReceipt.validate()
}

func (identity N1SourceEvidenceSigningIdentity) validate() error {
	publicKeyDigest := strings.TrimPrefix(identity.SignerPublicKeyID, "ed25519:")
	if identity.Purpose != unirouteradapter.SourceEvidencePurpose ||
		identity.Domain != unirouteradapter.SourceEvidenceDomain ||
		!validPortableText(identity.SigningKeyRef, 160) ||
		!strings.HasPrefix(identity.SigningKeyRef, "source-evidence-") ||
		!digestPattern.MatchString(publicKeyDigest) ||
		!digestPattern.MatchString(identity.TrustSetSHA256) {
		return errors.New("neutral N1 manifest Source signer identity is invalid")
	}
	return nil
}

func (identity N1SourceEvidenceSigningIdentity) trustPolicy() unirouteradapter.SourceEvidenceTrustPolicy {
	return unirouteradapter.SourceEvidenceTrustPolicy{
		Purpose:           identity.Purpose,
		Domain:            identity.Domain,
		Algorithm:         unirouteradapter.SourceEvidenceSignatureEd25519,
		SigningKeyRef:     identity.SigningKeyRef,
		SignerPublicKeyID: identity.SignerPublicKeyID,
		TrustSetSHA256:    identity.TrustSetSHA256,
	}
}

func (report N1ManifestVerification) expectedSeal() string {
	payload, _ := json.Marshal(struct {
		Valid                  bool                            `json:"valid"`
		CampaignManifestSHA256 string                          `json:"campaign_manifest_sha256"`
		CampaignID             string                          `json:"campaign_id"`
		FrozenAt               string                          `json:"frozen_at"`
		XInsightComponent      N1ComponentIdentity             `json:"xinsight_component"`
		UniRouterComponent     N1ComponentIdentity             `json:"unirouter_component"`
		SourceBinding          ArtifactReference               `json:"source_binding"`
		SourceEvidenceSigning  N1SourceEvidenceSigningIdentity `json:"source_evidence_signing"`
	}{
		Valid:                  report.Valid,
		CampaignManifestSHA256: report.CampaignManifestSHA256,
		CampaignID:             report.CampaignID,
		FrozenAt:               report.FrozenAt,
		XInsightComponent:      report.XInsightComponent,
		UniRouterComponent:     report.UniRouterComponent,
		SourceBinding:          report.SourceBinding,
		SourceEvidenceSigning:  report.SourceEvidenceSigning,
	})
	return bytesSHA256(payload)
}

func (report N1ManifestVerification) validateVerified() error {
	if !report.Valid || !digestPattern.MatchString(report.CampaignManifestSHA256) ||
		!validPortableText(report.CampaignID, 128) || validateCanonicalTimestamp(report.FrozenAt) != nil ||
		report.verificationSeal == "" || report.verificationSeal != report.expectedSeal() {
		return errors.New("neutral N1 manifest verification is not sealed")
	}
	if err := report.XInsightComponent.validate(ProductXInsight, ComponentRole); err != nil ||
		report.XInsightComponent.Contract.ID != ComponentContractVersion {
		return errors.New("neutral N1 manifest X-Insight component projection is invalid")
	}
	if err := report.UniRouterComponent.validate(n1UniRouterProduct, n1UniRouterProducerRole); err != nil {
		return err
	}
	if err := report.SourceBinding.validate(); err != nil || report.SourceBinding.SchemaVersion != n1SourceBindingVersion {
		return errors.New("neutral N1 manifest Source binding projection is invalid")
	}
	return report.SourceEvidenceSigning.validate()
}

func (report N1AuthorizationVerification) expectedSeal() string {
	payload, _ := json.Marshal(struct {
		Valid                        bool                            `json:"valid"`
		CampaignManifestSHA256       string                          `json:"campaign_manifest_sha256"`
		AcknowledgementSHA256        map[string]string               `json:"acknowledgement_sha256"`
		AuthorizationSHA256          string                          `json:"authorization_sha256"`
		AuthorizationSignatureSHA256 string                          `json:"authorization_signature_sha256"`
		AuthorizationTrustSetSHA256  string                          `json:"authorization_trust_set_sha256"`
		SourceEvidenceTrustSetSHA256 string                          `json:"source_evidence_trust_set_sha256"`
		CapabilityBoundary           string                          `json:"capability_boundary"`
		UniRouterComponent           N1ComponentIdentity             `json:"unirouter_component"`
		SourceBinding                ArtifactReference               `json:"source_binding"`
		SourceEvidenceSigning        N1SourceEvidenceSigningIdentity `json:"source_evidence_signing"`
		AuthorizationWindow          N1AuthorizationWindow           `json:"authorization_window"`
	}{
		Valid:                        report.Valid,
		CampaignManifestSHA256:       report.CampaignManifestSHA256,
		AcknowledgementSHA256:        report.AcknowledgementSHA256,
		AuthorizationSHA256:          report.AuthorizationSHA256,
		AuthorizationSignatureSHA256: report.AuthorizationSignatureSHA256,
		AuthorizationTrustSetSHA256:  report.AuthorizationTrustSetSHA256,
		SourceEvidenceTrustSetSHA256: report.SourceEvidenceTrustSetSHA256,
		CapabilityBoundary:           report.CapabilityBoundary,
		UniRouterComponent:           report.UniRouterComponent,
		SourceBinding:                report.SourceBinding,
		SourceEvidenceSigning:        report.SourceEvidenceSigning,
		AuthorizationWindow:          report.AuthorizationWindow,
	})
	return bytesSHA256(payload)
}

func (report N1AuthorizationVerification) validateVerified() error {
	if !report.Valid || report.CapabilityBoundary != CapabilityOfflineFiles ||
		!digestPattern.MatchString(report.CampaignManifestSHA256) ||
		!digestPattern.MatchString(report.AuthorizationSHA256) ||
		!digestPattern.MatchString(report.AuthorizationSignatureSHA256) ||
		!digestPattern.MatchString(report.AuthorizationTrustSetSHA256) ||
		!digestPattern.MatchString(report.SourceEvidenceTrustSetSHA256) ||
		len(report.AcknowledgementSHA256) != 2 ||
		!digestPattern.MatchString(report.AcknowledgementSHA256[n1UniRouterProduct]) ||
		!digestPattern.MatchString(report.AcknowledgementSHA256[ProductXInsight]) ||
		report.verificationSeal == "" || report.verificationSeal != report.expectedSeal() {
		return errors.New("neutral N1 authorization verification is not sealed")
	}
	if err := report.UniRouterComponent.validate(n1UniRouterProduct, n1UniRouterProducerRole); err != nil {
		return err
	}
	if err := report.SourceBinding.validate(); err != nil || report.SourceBinding.SchemaVersion != n1SourceBindingVersion {
		return errors.New("neutral N1 authorization Source binding projection is invalid")
	}
	if err := report.SourceEvidenceSigning.validate(); err != nil {
		return err
	}
	if err := report.AuthorizationWindow.validate(); err != nil {
		return err
	}
	if report.SourceEvidenceTrustSetSHA256 != report.SourceEvidenceSigning.TrustSetSHA256 {
		return errors.New("neutral N1 authorization Source trust does not match M")
	}
	return nil
}

// Validate preserves the opaque N1 verification seal while allowing later
// product-local lifecycle stages to recheck the accepted authorization without
// exposing any constructor for that authority.
func (report N1AuthorizationVerification) Validate() error {
	return report.validateVerified()
}

func (report N1AuthorizationVerification) ValidateAgainstManifest(
	manifest N1ManifestVerification,
) error {
	if err := manifest.validateVerified(); err != nil {
		return err
	}
	if err := report.validateVerified(); err != nil {
		return err
	}
	if report.CampaignManifestSHA256 != manifest.CampaignManifestSHA256 ||
		report.UniRouterComponent != manifest.UniRouterComponent ||
		report.SourceBinding != manifest.SourceBinding ||
		report.SourceEvidenceSigning != manifest.SourceEvidenceSigning {
		return errors.New("neutral N1 authorization contradicts its sealed campaign manifest")
	}
	return nil
}

// VerifyPreauthorizedCampaignPackage is the product lifecycle entry point for
// a post-authorization package. Every preauthorized input is checked against
// one sealed N1 M/A result before the producer adapter can inspect bytes.
func VerifyPreauthorizedCampaignPackage(
	packageBytes []byte,
	request unirouteradapter.CampaignPackageVerificationRequest,
	adapter unirouteradapter.CampaignPackageContractAdapter,
	authorization N1AuthorizationVerification,
) (unirouteradapter.VerifiedCampaignPackage, error) {
	if err := authorization.validateVerified(); err != nil {
		return unirouteradapter.VerifiedCampaignPackage{}, err
	}
	expectedContract := unirouteradapter.CampaignPackageContractIdentity{
		ID:     authorization.UniRouterComponent.Contract.ID,
		SHA256: authorization.UniRouterComponent.Contract.SHA256,
	}
	if request.ExpectedContract != expectedContract ||
		request.ExpectedSourceBindingSHA256 != authorization.SourceBinding.SHA256 ||
		request.ExpectedAuthorizationManifestSHA256 != authorization.CampaignManifestSHA256 ||
		request.ExpectedAuthorizationSHA256 != authorization.AuthorizationSHA256 ||
		request.ExpectedAuthorizationSignatureSHA256 != authorization.AuthorizationSignatureSHA256 ||
		request.Trust != authorization.SourceEvidenceSigning.trustPolicy() {
		return unirouteradapter.VerifiedCampaignPackage{}, errors.New("campaign package request contradicts the sealed N1 authorization authority")
	}
	return unirouteradapter.VerifyCampaignPackage(packageBytes, request, adapter)
}

func readInputArtifact(path string) ([]byte, error) {
	return readExactFile(path, n1MaximumInputBytes, 0, "")
}

func readExactFile(path string, maximum, exactSize int64, expectedSHA256 string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximum ||
		(exactSize > 0 && info.Size() != exactSize) {
		return nil, errors.New("artifact is not an exact bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("open exact artifact")
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(payload)) != info.Size() || int64(len(payload)) > maximum {
		return nil, errors.New("read exact artifact")
	}
	if expectedSHA256 != "" && bytesSHA256(payload) != expectedSHA256 {
		return nil, errors.New("artifact digest does not match the immutable pin")
	}
	return payload, nil
}

func bytesSHA256(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

type boundedOutput struct {
	buffer  bytes.Buffer
	maximum int
}

func newBoundedOutput(maximum int) *boundedOutput {
	return &boundedOutput{maximum: maximum}
}

func (output *boundedOutput) Write(payload []byte) (int, error) {
	remaining := output.maximum - output.buffer.Len()
	if remaining <= 0 {
		return 0, errors.New("neutral N1 verifier output exceeded its bound")
	}
	if len(payload) > remaining {
		written, _ := output.buffer.Write(payload[:remaining])
		return written, errors.New("neutral N1 verifier output exceeded its bound")
	}
	return output.buffer.Write(payload)
}

func (output *boundedOutput) Bytes() []byte {
	return output.buffer.Bytes()
}
