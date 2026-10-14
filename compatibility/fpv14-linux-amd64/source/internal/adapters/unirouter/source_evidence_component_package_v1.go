package unirouter

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
	"time"
)

const (
	// These pins identify the immutable matched N2 conformance chain accepted at
	// UniRouter commit d0ec4563d0596c614a5e3171d4d592725973328a. They are consumer
	// compatibility inputs, not an editable copy of the producer schema.
	UniRouterSourceEvidenceV1ProducerCommit = "d0ec4563d0596c614a5e3171d4d592725973328a"
	UniRouterSourceEvidenceV1ProducerTree   = "bebc4bb33568236ac987681dd6a55b4837833c48"
	UniRouterSourceEvidenceV1ContractID     = "unirouter.source-evidence.component-package.v1"
	UniRouterSourceEvidenceV1ContractSHA256 = "sha256:99cfa123a93ec7464cfd0228143d59bf579621095cdf69337109e0d5d4ed803e"
	UniRouterSourceEvidenceV1IndexSHA256    = "sha256:7289e3e160221fd28f0e30397eb9b37dca4a914f5bfda7a0aca422825b552a2b"
	UniRouterSourceEvidenceV1FixtureSHA256  = "sha256:42466d9a588503ca9978e4659ceff7a817cdf2f94dd744083f14b0f4e6899c09"

	uniRouterSourceEvidenceV1ManifestSchema    = "unirouter.source-evidence.component-manifest.v1"
	uniRouterSourceEvidenceV1SignatureSchema   = "unirouter.source-evidence.detached-signature.v1"
	uniRouterSourceEvidenceV1TrustSetSchema    = "unirouter.source-evidence.trust-set.v1"
	uniRouterSourceEvidenceV1RequestSetSchema  = "unirouter.source-evidence.request-set.v1"
	uniRouterSourceEvidenceV1SourceScopeSchema = "unirouter.source-evidence.source-scope.v1"
	uniRouterSourceEvidenceV1SourceFactsSchema = "unirouter.poc-source-facts.v1"

	uniRouterSourceEvidenceV1ManifestPath    = "manifest.json"
	uniRouterSourceEvidenceV1SignaturePath   = "manifest.sig"
	uniRouterSourceEvidenceV1RequestSetPath  = "payload/request-set.json"
	uniRouterSourceEvidenceV1SourceScopePath = "payload/source-scope.json"
	uniRouterSourceEvidenceV1SourceFactsPath = "payload/source-facts.json"

	uniRouterSourceEvidenceV1RequestMediaType = "application/vnd.unirouter.source-evidence.request-set.v1+json"
	uniRouterSourceEvidenceV1ScopeMediaType   = "application/vnd.unirouter.source-evidence.source-scope.v1+json"
	uniRouterSourceEvidenceV1FactsMediaType   = "application/vnd.unirouter.poc-source-facts.v1+json"
	uniRouterSourceEvidenceV1MemberMediaType  = "application/vnd.unirouter.execution-evidence.bundle.v2+json; completeness=partial"

	uniRouterSourceEvidenceV1ClaimState         = "complete"
	uniRouterSourceEvidenceV1NestedProfile      = "partial"
	uniRouterSourceEvidenceV1IdentityAuthority  = "neutral_campaign_authorization"
	uniRouterSourceEvidenceV1RuntimeObservation = "not_claimed"
	uniRouterSourceEvidenceV1FeedSchema         = "unirouter.completed-execution.v1"

	uniRouterSourceEvidenceV1MaxPackageBytes   = 32 << 20
	uniRouterSourceEvidenceV1MaxExpandedBytes  = 32 << 20
	uniRouterSourceEvidenceV1MaxContractBytes  = 8 << 20
	uniRouterSourceEvidenceV1MaxManifestBytes  = 128 << 10
	uniRouterSourceEvidenceV1MaxSignatureBytes = 16 << 10
	uniRouterSourceEvidenceV1MaxRequests       = 20
	uniRouterSourceEvidenceV1MaxFacts          = 200
	uniRouterSourceEvidenceV1MaxEntries        = uniRouterSourceEvidenceV1MaxRequests + 5
)

var (
	uniRouterSourceEvidenceV1SourcePattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	uniRouterSourceEvidenceV1PortablePattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)
	uniRouterSourceEvidenceV1KeyRefPattern     = regexp.MustCompile(`^source-evidence-[A-Za-z0-9._-]{1,48}$`)
	uniRouterSourceEvidenceV1MemberPathPattern = regexp.MustCompile(`^payload/compatibility/[0-9]{3}-[0-9a-f]{16}\.json$`)
	uniRouterSourceEvidenceV1HashPattern       = regexp.MustCompile(`^[a-f0-9]{64}$`)
	uniRouterSourceEvidenceV1GitObjectPattern  = regexp.MustCompile(`^[a-f0-9]{40}([a-f0-9]{24})?$`)
	uniRouterSourceEvidenceV1StagePattern      = regexp.MustCompile(`^[a-z][a-z0-9_]{0,79}$`)
	uniRouterSourceEvidenceV1MetadataPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,79}$`)

	uniRouterSourceEvidenceV1ProhibitedKeys = map[string]struct{}{
		"prompt": {}, "response": {}, "authorization": {}, "api_key": {},
		"credential": {}, "ip_address": {}, "upstream_error": {},
		"actor_ref": {}, "on_behalf_of": {}, "api_key_id": {},
		"access_credential_id": {}, "credential_ref": {}, "access_grant_id": {},
		"channel_id": {}, "channel_key_id": {}, "upstream_request_id": {},
	}

	uniRouterSourceEvidenceV1CaptureGaps = map[string]sourceEvidenceV1CaptureGap{
		"call_log_unavailable": {
			Code: "call_log_unavailable", Source: "call_logs",
			Reason: "no call_logs row matched the frozen tenant request identity",
		},
		"request_trace_unavailable": {
			Code: "request_trace_unavailable", Source: "request_trace_events",
			Reason: "no request_trace_events row matched the frozen tenant request identity",
		},
		"provider_attempt_unavailable": {
			Code: "provider_attempt_unavailable", Source: "billing_provider_attempts",
			Reason: "no billing_provider_attempts row matched the frozen tenant request identity",
		},
		"completed_feed_item_unavailable": {
			Code: "completed_feed_item_unavailable", Source: "completed_execution_feed_items",
			Reason: "no completed_execution_feed_items row matched the frozen tenant request identity",
		},
	}

	uniRouterSourceEvidenceV1FailureKinds = map[string]struct{}{
		"": {}, "auth": {}, "channel_failure": {}, "client_request": {},
		"configuration": {}, "delivery_failure": {}, "payload_capacity": {},
		"rate_limit": {}, "request_build": {}, "response_parse": {},
		"response_read": {}, "response_too_large": {}, "transport": {},
		"unclassified": {}, "unknown": {},
	}
)

// UniRouterSourceEvidenceV1Adapter independently verifies the producer-owned
// package bytes. It holds only a purpose-specific public trust set; it has no
// transport, database, worker, credential, Cursor, or Checkpoint capability.
type UniRouterSourceEvidenceV1Adapter struct {
	trustSetSHA256 string
	publicKeys     map[string]ed25519.PublicKey
	publicKeyIDs   map[string]string
}

func NewUniRouterSourceEvidenceV1Adapter(trustSetBytes []byte) (*UniRouterSourceEvidenceV1Adapter, error) {
	trustSet, err := decodeCanonicalSourceEvidenceV1[sourceEvidenceV1TrustSet](
		trustSetBytes,
		"UniRouter source-evidence trust set",
	)
	if err != nil {
		return nil, err
	}
	if trustSet.SchemaVersion != uniRouterSourceEvidenceV1TrustSetSchema ||
		trustSet.Purpose != SourceEvidencePurpose || trustSet.Domain != SourceEvidenceDomain ||
		len(trustSet.Keys) == 0 || len(trustSet.Keys) > 32 {
		return nil, errors.New("UniRouter source-evidence trust set identity is invalid")
	}
	publicKeys := make(map[string]ed25519.PublicKey, len(trustSet.Keys))
	publicKeyIDs := make(map[string]string, len(trustSet.Keys))
	previous := ""
	for index, key := range trustSet.Keys {
		if !uniRouterSourceEvidenceV1KeyRefPattern.MatchString(key.SigningKeyRef) ||
			key.SigningKeyRef <= previous || key.Algorithm != SourceEvidenceSignatureEd25519 ||
			!campaignPackageKeyIDPattern.MatchString(key.SignerPublicKeyID) {
			return nil, fmt.Errorf("UniRouter source-evidence trust set keys[%d] is invalid or not strictly sorted", index)
		}
		decoded, decodeErr := base64.StdEncoding.DecodeString(key.PublicKey)
		if decodeErr != nil || len(decoded) != ed25519.PublicKeySize ||
			base64.StdEncoding.EncodeToString(decoded) != key.PublicKey {
			return nil, fmt.Errorf("UniRouter source-evidence trust set key %q is not canonical Ed25519", key.SigningKeyRef)
		}
		publicKey := ed25519.PublicKey(bytes.Clone(decoded))
		if sourceEvidenceV1PublicKeyID(publicKey) != key.SignerPublicKeyID {
			return nil, fmt.Errorf("UniRouter source-evidence trust set key %q has the wrong public identity", key.SigningKeyRef)
		}
		publicKeys[key.SigningKeyRef] = publicKey
		publicKeyIDs[key.SigningKeyRef] = key.SignerPublicKeyID
		previous = key.SigningKeyRef
	}
	return &UniRouterSourceEvidenceV1Adapter{
		trustSetSHA256: campaignPackageSHA256(trustSetBytes),
		publicKeys:     publicKeys,
		publicKeyIDs:   publicKeyIDs,
	}, nil
}

func (*UniRouterSourceEvidenceV1Adapter) ContractIdentity() CampaignPackageContractIdentity {
	return CampaignPackageContractIdentity{
		ID:     UniRouterSourceEvidenceV1ContractID,
		SHA256: UniRouterSourceEvidenceV1ContractSHA256,
	}
}

func (adapter *UniRouterSourceEvidenceV1Adapter) VerifyAndOpen(
	packageBytes []byte,
	policy SourceEvidenceTrustPolicy,
) (OpenedCampaignPackage, error) {
	envelope, err := adapter.verifySourceEvidenceV1Envelope(packageBytes, policy)
	if err != nil {
		return OpenedCampaignPackage{}, err
	}
	manifest := envelope.manifest
	manifestBody := envelope.manifestBody
	entries := envelope.entries
	order := envelope.order

	facts, err := openSourceEvidenceV1Facts(entries, manifest)
	if err != nil {
		return OpenedCampaignPackage{}, err
	}
	requestSet := facts.requestSet
	sourceScope := facts.sourceScope
	sourceBindingSHA256 := facts.sourceBindingSHA256

	inventories, payloads, err := openSourceEvidenceV1Members(entries, requestSet, sourceScope)
	if err != nil {
		return OpenedCampaignPackage{}, err
	}
	if len(manifest.Artifacts) != len(inventories)+3 {
		return OpenedCampaignPackage{}, errors.New("UniRouter source-evidence package includes artifacts outside the frozen contract")
	}
	createdAt, err := parseSourceEvidenceV1Time(manifest.CreatedAt)
	if err != nil {
		return OpenedCampaignPackage{}, err
	}
	if err := verifyCanonicalSourceEvidenceV1Container(packageBytes, order, entries, createdAt); err != nil {
		return OpenedCampaignPackage{}, err
	}
	packageSHA256 := campaignPackageSHA256(packageBytes)
	return OpenedCampaignPackage{
		Projection: CampaignPackageProjection{
			Contract:                     adapter.ContractIdentity(),
			PackageID:                    packageSHA256,
			PackageSHA256:                packageSHA256,
			PackageSizeBytes:             int64(len(packageBytes)),
			ManifestSHA256:               campaignPackageSHA256(manifestBody),
			PackageCreatedAt:             manifest.CreatedAt,
			PackageScopeComplete:         true,
			SourceInstance:               sourceScope.SourceInstance,
			SourceBindingSHA256:          sourceBindingSHA256,
			AuthorizationManifestSHA256:  manifest.AuthorizationManifestSHA256,
			AuthorizationSHA256:          manifest.AuthorizationStatementSHA256,
			AuthorizationSignatureSHA256: manifest.AuthorizationSignatureSHA256,
			Signature: SourceEvidenceSignatureIdentity{
				Purpose:           manifest.Purpose,
				Domain:            manifest.Domain,
				Algorithm:         manifest.SignatureAlgorithm,
				SigningKeyRef:     manifest.SigningKeyRef,
				SignerPublicKeyID: manifest.SignerPublicKeyID,
				TrustSetSHA256:    manifest.TrustSetSHA256,
			},
			Members: inventories,
		},
		MemberPayloads: payloads,
	}, nil
}

type verifiedSourceEvidenceV1Envelope struct {
	manifest     sourceEvidenceV1Manifest
	manifestBody []byte
	entries      map[string][]byte
	order        []string
}

func openSourceEvidenceV1Members(
	entries map[string][]byte,
	requestSet sourceEvidenceV1RequestSet,
	sourceScope sourceEvidenceV1SourceScope,
) ([]CampaignPackageMemberInventory, map[string][]byte, error) {
	inventories := make([]CampaignPackageMemberInventory, 0, len(requestSet.Requests))
	payloads := make(map[string][]byte, len(requestSet.Requests))
	for index, selection := range requestSet.Requests {
		memberPath := sourceEvidenceV1CompatibilityPath(index, selection)
		body, present := entries[memberPath]
		if !present {
			return nil, nil, fmt.Errorf("UniRouter source-evidence compatibility member %d is missing", index)
		}
		bundle, _, loadErr := loadExecutionEvidenceBundleBytes(body)
		if loadErr != nil {
			return nil, nil, fmt.Errorf("validate UniRouter compatibility member %d: %w", index, loadErr)
		}
		canonicalBundle, marshalErr := json.Marshal(bundle)
		if marshalErr != nil || !bytes.Equal(canonicalBundle, body) {
			return nil, nil, fmt.Errorf("UniRouter compatibility member %d is not canonical JSON", index)
		}
		if err := validateSignedCampaignPackagePrivacy(bundle); err != nil {
			return nil, nil, err
		}
		if bundle.SourceInstance != sourceScope.SourceInstance || bundle.RequestID != selection.RequestID ||
			bundle.OrgID != selection.OrgID || bundle.ProjectID == nil || *bundle.ProjectID != selection.ProjectID {
			return nil, nil, errors.New("UniRouter source-evidence member identity contradicts the sealed Source scope or request set")
		}
		outcome, completenessErr := CampaignPackageMemberCompleteness(memberPath, bundle, 0)
		if completenessErr != nil || outcome.SourceProfileComplete || outcome.SourceProfileState != "partial" ||
			outcome.RuntimeCloseMarkerAvailable {
			return nil, nil, errors.New("UniRouter source-evidence compatibility member does not preserve honest partial-v2 semantics")
		}
		inventories = append(inventories, CampaignPackageMemberInventory{
			MemberID: memberPath, Path: memberPath, SHA256: campaignPackageSHA256(body), SizeBytes: int64(len(body)),
		})
		payloads[memberPath] = bytes.Clone(body)
	}
	return inventories, payloads, nil
}

type openedSourceEvidenceV1Facts struct {
	requestSet          sourceEvidenceV1RequestSet
	sourceScope         sourceEvidenceV1SourceScope
	sourceBindingSHA256 string
}

func openSourceEvidenceV1Facts(
	entries map[string][]byte,
	manifest sourceEvidenceV1Manifest,
) (openedSourceEvidenceV1Facts, error) {
	requestSet, err := decodeCanonicalSourceEvidenceV1[sourceEvidenceV1RequestSet](
		entries[uniRouterSourceEvidenceV1RequestSetPath],
		"UniRouter source-evidence request set",
	)
	if err != nil || validateSourceEvidenceV1RequestSet(requestSet) != nil {
		return openedSourceEvidenceV1Facts{}, errors.New("UniRouter source-evidence request set is invalid")
	}
	sourceScope, err := decodeCanonicalSourceEvidenceV1[sourceEvidenceV1SourceScope](
		entries[uniRouterSourceEvidenceV1SourceScopePath],
		"UniRouter source-evidence Source scope",
	)
	if err != nil || validateSourceEvidenceV1SourceScope(sourceScope) != nil {
		return openedSourceEvidenceV1Facts{}, errors.New("UniRouter source-evidence Source scope is invalid")
	}
	if err := scanStrictJSONDocument(
		entries[uniRouterSourceEvidenceV1SourceFactsPath],
		"UniRouter source-evidence Source facts",
		uniRouterSourceEvidenceV1ProhibitedKeys,
	); err != nil {
		return openedSourceEvidenceV1Facts{}, err
	}
	sourceFacts, err := decodeCanonicalSourceEvidenceV1[sourceEvidenceV1SourceFacts](
		entries[uniRouterSourceEvidenceV1SourceFactsPath],
		"UniRouter source-evidence Source facts",
	)
	if err != nil {
		return openedSourceEvidenceV1Facts{}, err
	}
	requestSetSHA256 := campaignPackageSHA256(entries[uniRouterSourceEvidenceV1RequestSetPath])
	sourceBindingSHA256 := campaignPackageSHA256(entries[uniRouterSourceEvidenceV1SourceScopePath])
	sourceFactsSHA256 := campaignPackageSHA256(entries[uniRouterSourceEvidenceV1SourceFactsPath])
	if manifest.RequestSetSHA256 != requestSetSHA256 || manifest.SourceBindingSHA256 != sourceBindingSHA256 ||
		manifest.SourceFactsSHA256 != sourceFactsSHA256 || sourceScope.SourceInstance != requestSet.SourceInstance ||
		sourceFacts.RequestSetSHA256 != requestSetSHA256 || sourceFacts.SourceBindingSHA256 != sourceBindingSHA256 ||
		sourceFacts.CampaignID != requestSet.CampaignID || sourceFacts.Source.Instance != sourceScope.SourceInstance ||
		sourceFacts.Source != sourceEvidenceV1SourceIdentity(sourceScope) {
		return openedSourceEvidenceV1Facts{}, errors.New("UniRouter source-evidence Source scope, request set, facts, and manifest conflict")
	}
	if err := validateSourceEvidenceV1SourceFacts(sourceFacts, requestSet); err != nil {
		return openedSourceEvidenceV1Facts{}, err
	}
	return openedSourceEvidenceV1Facts{
		requestSet: requestSet, sourceScope: sourceScope, sourceBindingSHA256: sourceBindingSHA256,
	}, nil
}

func (adapter *UniRouterSourceEvidenceV1Adapter) verifySourceEvidenceV1Envelope(
	packageBytes []byte,
	policy SourceEvidenceTrustPolicy,
) (verifiedSourceEvidenceV1Envelope, error) {
	if adapter == nil || len(adapter.publicKeys) == 0 || len(adapter.publicKeyIDs) == 0 {
		return verifiedSourceEvidenceV1Envelope{}, errors.New("UniRouter source-evidence adapter is unavailable")
	}
	if err := policy.validate(); err != nil {
		return verifiedSourceEvidenceV1Envelope{}, err
	}
	publicKey, exists := adapter.publicKeys[policy.SigningKeyRef]
	if !exists || adapter.trustSetSHA256 != policy.TrustSetSHA256 ||
		adapter.publicKeyIDs[policy.SigningKeyRef] != policy.SignerPublicKeyID {
		return verifiedSourceEvidenceV1Envelope{}, errors.New("UniRouter source-evidence trust set does not contain the exact sealed authority")
	}
	entries, order, err := openCanonicalSourceEvidenceV1Container(packageBytes)
	if err != nil {
		return verifiedSourceEvidenceV1Envelope{}, err
	}
	if len(order) < 6 || order[0] != uniRouterSourceEvidenceV1ManifestPath ||
		order[1] != uniRouterSourceEvidenceV1SignaturePath {
		return verifiedSourceEvidenceV1Envelope{}, errors.New("UniRouter source-evidence package metadata order is invalid")
	}
	manifestBody := entries[uniRouterSourceEvidenceV1ManifestPath]
	if len(manifestBody) == 0 || len(manifestBody) > uniRouterSourceEvidenceV1MaxManifestBytes {
		return verifiedSourceEvidenceV1Envelope{}, errors.New("UniRouter source-evidence manifest size is invalid")
	}
	manifest, err := decodeCanonicalSourceEvidenceV1[sourceEvidenceV1Manifest](
		manifestBody,
		"UniRouter source-evidence manifest",
	)
	if err != nil {
		return verifiedSourceEvidenceV1Envelope{}, err
	}
	if err := validateSourceEvidenceV1Manifest(manifest); err != nil {
		return verifiedSourceEvidenceV1Envelope{}, err
	}
	signatureBody := entries[uniRouterSourceEvidenceV1SignaturePath]
	if len(signatureBody) == 0 || len(signatureBody) > uniRouterSourceEvidenceV1MaxSignatureBytes {
		return verifiedSourceEvidenceV1Envelope{}, errors.New("UniRouter source-evidence signature size is invalid")
	}
	signature, err := decodeCanonicalSourceEvidenceV1[sourceEvidenceV1Signature](
		signatureBody,
		"UniRouter source-evidence signature",
	)
	if err != nil {
		return verifiedSourceEvidenceV1Envelope{}, err
	}
	if manifest.Purpose != policy.Purpose || manifest.Domain != policy.Domain ||
		manifest.SignatureAlgorithm != policy.Algorithm || manifest.SigningKeyRef != policy.SigningKeyRef ||
		manifest.SignerPublicKeyID != policy.SignerPublicKeyID || manifest.TrustSetSHA256 != policy.TrustSetSHA256 ||
		signature.SchemaVersion != uniRouterSourceEvidenceV1SignatureSchema ||
		signature.Purpose != manifest.Purpose || signature.Domain != manifest.Domain ||
		signature.Algorithm != manifest.SignatureAlgorithm || signature.SigningKeyRef != manifest.SigningKeyRef ||
		signature.SignerPublicKeyID != manifest.SignerPublicKeyID || signature.TrustSetSHA256 != manifest.TrustSetSHA256 {
		return verifiedSourceEvidenceV1Envelope{}, errors.New("UniRouter source-evidence signature identity contradicts the sealed authority or manifest")
	}
	signatureBytes, err := base64.StdEncoding.DecodeString(signature.Value)
	if err != nil || len(signatureBytes) != ed25519.SignatureSize ||
		base64.StdEncoding.EncodeToString(signatureBytes) != signature.Value {
		return verifiedSourceEvidenceV1Envelope{}, errors.New("UniRouter source-evidence signature encoding is invalid")
	}
	preimage := append([]byte(SourceEvidenceDomain+"\x00"+SourceEvidencePurpose+"\x00"), manifestBody...)
	if !ed25519.Verify(publicKey, preimage, signatureBytes) {
		return verifiedSourceEvidenceV1Envelope{}, errors.New("UniRouter source-evidence signature verification failed")
	}
	if len(manifest.Artifacts)+2 != len(entries) {
		return verifiedSourceEvidenceV1Envelope{}, errors.New("UniRouter source-evidence artifact count does not match the signed manifest")
	}
	for index, artifact := range manifest.Artifacts {
		if order[index+2] != artifact.Path {
			return verifiedSourceEvidenceV1Envelope{}, errors.New("UniRouter source-evidence artifact order does not match the signed manifest")
		}
		body, present := entries[artifact.Path]
		if !present || int64(len(body)) != artifact.Size || campaignPackageSHA256(body) != artifact.SHA256 {
			return verifiedSourceEvidenceV1Envelope{}, fmt.Errorf("UniRouter source-evidence artifact %q does not match its signed identity", artifact.Path)
		}
	}
	return verifiedSourceEvidenceV1Envelope{
		manifest: manifest, manifestBody: manifestBody, entries: entries, order: order,
	}, nil
}

type sourceEvidenceV1Artifact struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	MediaType string `json:"media_type"`
}

type sourceEvidenceV1Manifest struct {
	SchemaVersion                string                     `json:"schema_version"`
	ContractID                   string                     `json:"contract_id"`
	Product                      string                     `json:"product"`
	Purpose                      string                     `json:"purpose"`
	Domain                       string                     `json:"domain"`
	SigningKeyRef                string                     `json:"signing_key_ref"`
	SignerPublicKeyID            string                     `json:"signer_public_key_id"`
	TrustSetSHA256               string                     `json:"trust_set_sha256"`
	SignatureAlgorithm           string                     `json:"signature_algorithm"`
	CreatedAt                    string                     `json:"created_at"`
	ClaimScope                   string                     `json:"claim_scope"`
	ClaimState                   string                     `json:"claim_state"`
	NestedExecutionEvidenceV2    string                     `json:"nested_execution_evidence_v2"`
	RequestSetSHA256             string                     `json:"request_set_sha256"`
	AuthorizationManifestSHA256  string                     `json:"authorization_manifest_sha256"`
	AuthorizationStatementSHA256 string                     `json:"authorization_statement_sha256"`
	AuthorizationSignatureSHA256 string                     `json:"authorization_signature_sha256"`
	SourceBindingSHA256          string                     `json:"source_binding_sha256"`
	SourceFactsSHA256            string                     `json:"source_facts_sha256"`
	CompatibilityProfileID       string                     `json:"compatibility_profile_id"`
	CompatibilityProfileSHA256   string                     `json:"compatibility_profile_sha256"`
	CompatibilitySchemaSHA256    string                     `json:"compatibility_schema_sha256"`
	Artifacts                    []sourceEvidenceV1Artifact `json:"artifacts"`
}

type sourceEvidenceV1Signature struct {
	SchemaVersion     string `json:"schema_version"`
	Purpose           string `json:"purpose"`
	Domain            string `json:"domain"`
	SigningKeyRef     string `json:"signing_key_ref"`
	SignerPublicKeyID string `json:"signer_public_key_id"`
	TrustSetSHA256    string `json:"trust_set_sha256"`
	Algorithm         string `json:"algorithm"`
	Value             string `json:"value"`
}

type sourceEvidenceV1TrustSet struct {
	SchemaVersion string                     `json:"schema_version"`
	Purpose       string                     `json:"purpose"`
	Domain        string                     `json:"domain"`
	Keys          []sourceEvidenceV1TrustKey `json:"keys"`
}

type sourceEvidenceV1TrustKey struct {
	SigningKeyRef     string `json:"signing_key_ref"`
	SignerPublicKeyID string `json:"signer_public_key_id"`
	Algorithm         string `json:"algorithm"`
	PublicKey         string `json:"public_key"`
}

type sourceEvidenceV1RequestSet struct {
	SchemaVersion  string                             `json:"schema_version"`
	CampaignID     string                             `json:"campaign_id"`
	SourceInstance string                             `json:"source_instance"`
	FrozenAt       string                             `json:"frozen_at"`
	Requests       []sourceEvidenceV1RequestSelection `json:"requests"`
	Limits         sourceEvidenceV1RequestLimits      `json:"limits"`
}

type sourceEvidenceV1RequestSelection struct {
	ScenarioID string `json:"scenario_id"`
	RequestID  string `json:"request_id"`
	OrgID      int64  `json:"org_id"`
	ProjectID  int64  `json:"project_id"`
}

type sourceEvidenceV1RequestLimits struct {
	MaxRequests int `json:"max_requests"`
	MaxFacts    int `json:"max_facts"`
}

type sourceEvidenceV1SourceScope struct {
	SchemaVersion      string `json:"schema_version"`
	Product            string `json:"product"`
	SourceInstance     string `json:"source_instance"`
	IdentityAuthority  string `json:"identity_authority"`
	RuntimeObservation string `json:"runtime_observation"`
	FeedSnapshotSchema string `json:"feed_snapshot_schema"`
}

type sourceEvidenceV1SourceIdentityProjection struct {
	Product            string `json:"product"`
	Instance           string `json:"instance"`
	IdentityAuthority  string `json:"identity_authority"`
	RuntimeObservation string `json:"runtime_observation"`
	FeedSnapshotSchema string `json:"feed_snapshot_schema"`
}

type sourceEvidenceV1SourceFacts struct {
	SchemaVersion       string                                   `json:"schema_version"`
	GeneratedAt         string                                   `json:"generated_at"`
	RequestSetSHA256    string                                   `json:"request_set_sha256"`
	SourceBindingSHA256 string                                   `json:"source_binding_sha256"`
	CampaignID          string                                   `json:"campaign_id"`
	Source              sourceEvidenceV1SourceIdentityProjection `json:"source"`
	Release             sourceEvidenceV1Release                  `json:"release"`
	Protocol            sourceEvidenceV1Protocol                 `json:"protocol"`
	Snapshot            sourceEvidenceV1Snapshot                 `json:"snapshot"`
	Requests            []sourceEvidenceV1RequestFacts           `json:"requests"`
	FeedRange           sourceEvidenceV1FeedRange                `json:"feed_range"`
	Counts              sourceEvidenceV1FactCounts               `json:"counts"`
	Completeness        sourceEvidenceV1Completeness             `json:"completeness"`
	Safety              sourceEvidenceV1Safety                   `json:"safety"`
}

type sourceEvidenceV1Release struct {
	ReleaseSequence int64  `json:"release_sequence"`
	Version         string `json:"version"`
	Commit          string `json:"commit"`
	PackageSHA256   string `json:"package_sha256"`
	TrustedKeyID    string `json:"trusted_key_id"`
	RecordedSchema  string `json:"recorded_schema"`
	CurrentSchema   string `json:"current_schema"`
	RecordedAt      string `json:"recorded_at"`
}

type sourceEvidenceV1Protocol struct {
	PinVersion      string                              `json:"pin_version"`
	ProtocolVersion string                              `json:"protocol_version"`
	ReleaseDigest   string                              `json:"release_digest"`
	PinSHA256       string                              `json:"pin_sha256"`
	Components      []sourceEvidenceV1ProtocolComponent `json:"components"`
}

type sourceEvidenceV1ProtocolComponent struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

type sourceEvidenceV1Snapshot struct {
	Isolation             string `json:"isolation"`
	ReadOnly              bool   `json:"read_only"`
	TransactionObservedAt string `json:"transaction_observed_at"`
	TransactionIDAssigned bool   `json:"transaction_id_assigned"`
	SelectionMode         string `json:"selection_mode"`
}

type sourceEvidenceV1RequestFacts struct {
	ScenarioID       string                            `json:"scenario_id"`
	RequestID        string                            `json:"request_id"`
	OrgID            int64                             `json:"org_id"`
	ProjectID        int64                             `json:"project_id"`
	CallLog          *sourceEvidenceV1CallLogFact      `json:"call_log"`
	TraceEvents      []sourceEvidenceV1TraceFact       `json:"trace_events"`
	ProviderAttempts []sourceEvidenceV1ProviderAttempt `json:"provider_attempts"`
	CompletedFeed    *sourceEvidenceV1CompletedFeed    `json:"completed_feed"`
	CaptureGaps      []sourceEvidenceV1CaptureGap      `json:"capture_gaps"`
}

type sourceEvidenceV1CallLogFact struct {
	FactSHA256         string `json:"fact_sha256"`
	SourceRef          string `json:"source_ref"`
	ID                 int64  `json:"id"`
	Model              string `json:"model"`
	RequestedModel     string `json:"requested_model"`
	Provider           string `json:"provider"`
	Channel            string `json:"channel"`
	PromptTokens       int64  `json:"prompt_tokens"`
	CompletionTokens   int64  `json:"completion_tokens"`
	TotalTokens        int64  `json:"total_tokens"`
	CostMicros         string `json:"cost_micros"`
	Currency           string `json:"currency"`
	AmountScale        int64  `json:"amount_scale"`
	UsageSource        string `json:"usage_source"`
	UsageAuthority     string `json:"usage_authority"`
	UsageFinality      string `json:"usage_finality"`
	StatusCode         int64  `json:"status_code"`
	UpstreamStatusCode *int64 `json:"upstream_status_code,omitempty"`
	LatencyMS          int64  `json:"latency_ms"`
	TraceID            string `json:"trace_id,omitempty"`
	AttemptID          string `json:"attempt_id,omitempty"`
	ActorType          string `json:"actor_type,omitempty"`
	PayloadCaptureMode string `json:"payload_capture_mode"`
	UpstreamErrorSeen  bool   `json:"upstream_error_was_present"`
	IPAddressSeen      bool   `json:"ip_address_was_present"`
	CreatedAt          string `json:"created_at"`
}

type sourceEvidenceV1TraceFact struct {
	FactSHA256         string   `json:"fact_sha256"`
	SourceRef          string   `json:"source_ref"`
	ID                 int64    `json:"id"`
	EventType          string   `json:"event_type"`
	Stage              string   `json:"stage"`
	CompatibilityStage string   `json:"compatibility_stage"`
	Severity           string   `json:"severity"`
	Model              string   `json:"model,omitempty"`
	RequestedModel     string   `json:"requested_model,omitempty"`
	Provider           string   `json:"provider,omitempty"`
	Channel            string   `json:"channel,omitempty"`
	StatusCode         *int64   `json:"status_code,omitempty"`
	LatencyMS          int64    `json:"latency_ms"`
	TraceID            string   `json:"trace_id,omitempty"`
	SpanID             string   `json:"span_id,omitempty"`
	ParentSpanID       string   `json:"parent_span_id,omitempty"`
	AttemptID          string   `json:"attempt_id,omitempty"`
	ActorType          string   `json:"actor_type,omitempty"`
	DecisionKind       string   `json:"decision_kind,omitempty"`
	DecisionResult     string   `json:"decision_result,omitempty"`
	DecisionReason     string   `json:"decision_reason,omitempty"`
	CaptureGap         string   `json:"capture_gap,omitempty"`
	MetadataKeys       []string `json:"metadata_keys"`
	PayloadCaptureMode string   `json:"payload_capture_mode"`
	CreatedAt          string   `json:"created_at"`
}

type sourceEvidenceV1ProviderAttempt struct {
	FactSHA256                         string  `json:"fact_sha256"`
	SourceRef                          string  `json:"source_ref"`
	AttemptID                          string  `json:"attempt_id"`
	AccessBindingType                  string  `json:"access_binding_type"`
	LogicalCredentialCorrelationSHA256 string  `json:"logical_credential_correlation_sha256,omitempty"`
	RouteSequence                      int     `json:"route_sequence"`
	RouteTotal                         int     `json:"route_total"`
	Endpoint                           string  `json:"endpoint"`
	Provider                           string  `json:"provider"`
	State                              string  `json:"state"`
	OutcomeCertainty                   string  `json:"outcome_certainty"`
	RetryAuthorized                    bool    `json:"retry_authorized"`
	HTTPStatus                         *int64  `json:"http_status,omitempty"`
	FailureKind                        string  `json:"failure_kind,omitempty"`
	CalculatedCostMicros               *string `json:"calculated_cost_micros,omitempty"`
	ChargedCostMicros                  *string `json:"charged_cost_micros,omitempty"`
	StartedAt                          string  `json:"started_at"`
	ResponseReceivedAt                 string  `json:"response_received_at,omitempty"`
	FinancialResolvedAt                string  `json:"financial_resolved_at,omitempty"`
	UpdatedAt                          string  `json:"updated_at"`
}

type sourceEvidenceV1CompletedFeed struct {
	FactSHA256          string                             `json:"fact_sha256"`
	SourceRef           string                             `json:"source_ref"`
	SequenceID          int64                              `json:"sequence_id"`
	TerminalStatus      string                             `json:"terminal_status"`
	TerminalAvailableAt string                             `json:"terminal_available_at"`
	Revision            string                             `json:"revision"`
	SnapshotSHA256      string                             `json:"snapshot_sha256"`
	ContentSummary      sourceEvidenceV1FeedContentSummary `json:"content_summary"`
}

type sourceEvidenceV1FeedContentSummary struct {
	Model                string `json:"model"`
	PromptTokens         int    `json:"prompt_tokens"`
	CompletionTokens     int    `json:"completion_tokens"`
	UsageAuthority       string `json:"usage_authority"`
	UsageFinality        string `json:"usage_finality"`
	CalculatedCostMicros string `json:"calculated_cost_micros"`
	ChargedCostMicros    string `json:"charged_cost_micros"`
	PricingVersion       string `json:"pricing_version"`
}

type sourceEvidenceV1CaptureGap struct {
	Code   string `json:"code"`
	Source string `json:"source"`
	Reason string `json:"reason"`
}

type sourceEvidenceV1FeedRange struct {
	Selection          string `json:"selection"`
	SelectedItems      int    `json:"selected_items"`
	MinSequenceID      *int64 `json:"min_sequence_id,omitempty"`
	MaxSequenceID      *int64 `json:"max_sequence_id,omitempty"`
	CursorClaim        string `json:"cursor_claim"`
	HighWatermarkClaim string `json:"high_watermark_claim"`
}

type sourceEvidenceV1FactCounts struct {
	RequestedRequests  int `json:"requested_requests"`
	CallLogs           int `json:"call_logs"`
	TraceEvents        int `json:"trace_events"`
	ProviderAttempts   int `json:"provider_attempts"`
	CompletedFeedItems int `json:"completed_feed_items"`
	CaptureGaps        int `json:"capture_gaps"`
	TotalFacts         int `json:"total_facts"`
}

type sourceEvidenceV1Completeness struct {
	ClaimScope                    string `json:"claim_scope"`
	State                         string `json:"state"`
	BoundedRequestSetComplete     bool   `json:"bounded_request_set_complete"`
	ExecutionEvidenceV2Projection string `json:"execution_evidence_v2_projection"`
	RuntimeCloseMarkerAvailable   bool   `json:"runtime_close_marker_available"`
}

type sourceEvidenceV1Safety struct {
	DatabaseMutation       string `json:"database_mutation"`
	AdditionalGatewayCalls int    `json:"additional_gateway_calls"`
	ProviderCalls          int    `json:"provider_calls"`
	FeedCalls              int    `json:"feed_calls"`
	UpstreamCalls          int    `json:"upstream_calls"`
	RawPromptsIncluded     bool   `json:"raw_prompts_included"`
	RawResponsesIncluded   bool   `json:"raw_responses_included"`
	SecretsIncluded        bool   `json:"secrets_included"`
	RawIPsIncluded         bool   `json:"raw_ips_included"`
	RawErrorsIncluded      bool   `json:"raw_errors_included"`
}

func validateSourceEvidenceV1Manifest(manifest sourceEvidenceV1Manifest) error {
	if manifest.SchemaVersion != uniRouterSourceEvidenceV1ManifestSchema ||
		manifest.ContractID != UniRouterSourceEvidenceV1ContractID || manifest.Product != "unirouter" ||
		manifest.Purpose != SourceEvidencePurpose || manifest.Domain != SourceEvidenceDomain ||
		manifest.SignatureAlgorithm != SourceEvidenceSignatureEd25519 ||
		!uniRouterSourceEvidenceV1KeyRefPattern.MatchString(manifest.SigningKeyRef) ||
		!campaignPackageKeyIDPattern.MatchString(manifest.SignerPublicKeyID) ||
		!campaignPackageDigestPattern.MatchString(manifest.TrustSetSHA256) {
		return errors.New("UniRouter source-evidence manifest signing identity is invalid")
	}
	if _, err := parseSourceEvidenceV1Time(manifest.CreatedAt); err != nil {
		return err
	}
	if manifest.ClaimScope != uniRouterSourceEvidenceV1SourceFactsSchema ||
		manifest.ClaimState != uniRouterSourceEvidenceV1ClaimState ||
		manifest.NestedExecutionEvidenceV2 != uniRouterSourceEvidenceV1NestedProfile ||
		manifest.CompatibilityProfileID != ExecutionEvidenceProfileID ||
		manifest.CompatibilityProfileSHA256 != ExecutionEvidenceProfileManifestSHA256 ||
		manifest.CompatibilitySchemaSHA256 != ExecutionEvidenceBundleSchemaSHA256 ||
		!campaignPackageDigestPattern.MatchString(manifest.RequestSetSHA256) ||
		!campaignPackageDigestPattern.MatchString(manifest.AuthorizationManifestSHA256) ||
		!campaignPackageDigestPattern.MatchString(manifest.AuthorizationStatementSHA256) ||
		!campaignPackageDigestPattern.MatchString(manifest.AuthorizationSignatureSHA256) ||
		!campaignPackageDigestPattern.MatchString(manifest.SourceBindingSHA256) ||
		!campaignPackageDigestPattern.MatchString(manifest.SourceFactsSHA256) {
		return errors.New("UniRouter source-evidence manifest contract binding is invalid")
	}
	if len(manifest.Artifacts) < 4 || len(manifest.Artifacts) > uniRouterSourceEvidenceV1MaxRequests+3 {
		return errors.New("UniRouter source-evidence manifest artifact count is invalid")
	}
	previous := ""
	seenRequest, seenScope, seenFacts := false, false, false
	for index, artifact := range manifest.Artifacts {
		expectedMediaType, err := sourceEvidenceV1ArtifactMediaType(artifact.Path)
		if err != nil || artifact.Path <= previous || artifact.Size <= 0 ||
			artifact.Size > uniRouterSourceEvidenceV1MaxContractBytes ||
			!campaignPackageDigestPattern.MatchString(artifact.SHA256) || artifact.MediaType != expectedMediaType {
			return fmt.Errorf("UniRouter source-evidence manifest artifacts[%d] is invalid or not strictly sorted", index)
		}
		previous = artifact.Path
		seenRequest = seenRequest || artifact.Path == uniRouterSourceEvidenceV1RequestSetPath
		seenScope = seenScope || artifact.Path == uniRouterSourceEvidenceV1SourceScopePath
		seenFacts = seenFacts || artifact.Path == uniRouterSourceEvidenceV1SourceFactsPath
	}
	if !seenRequest || !seenScope || !seenFacts {
		return errors.New("UniRouter source-evidence manifest lacks request set, Source scope, or Source facts")
	}
	return nil
}

func validateSourceEvidenceV1RequestSet(requestSet sourceEvidenceV1RequestSet) error {
	if requestSet.SchemaVersion != uniRouterSourceEvidenceV1RequestSetSchema ||
		!sourceEvidenceV1PortableWithin(requestSet.CampaignID, 128) ||
		!uniRouterSourceEvidenceV1SourcePattern.MatchString(requestSet.SourceInstance) ||
		requestSet.Limits.MaxRequests != uniRouterSourceEvidenceV1MaxRequests ||
		requestSet.Limits.MaxFacts != uniRouterSourceEvidenceV1MaxFacts ||
		len(requestSet.Requests) == 0 || len(requestSet.Requests) > uniRouterSourceEvidenceV1MaxRequests {
		return errors.New("UniRouter source-evidence request set identity or limits are invalid")
	}
	if _, err := parseSourceEvidenceV1Time(requestSet.FrozenAt); err != nil {
		return err
	}
	seenScenario := make(map[string]struct{}, len(requestSet.Requests))
	seenRequest := make(map[string]struct{}, len(requestSet.Requests))
	for index, selection := range requestSet.Requests {
		requestKey := fmt.Sprintf("%d/%s", selection.OrgID, selection.RequestID)
		if !sourceEvidenceV1PortableWithin(selection.ScenarioID, 120) ||
			!sourceEvidenceV1PortableWithin(selection.RequestID, 64) ||
			selection.OrgID <= 0 || selection.ProjectID <= 0 {
			return fmt.Errorf("UniRouter source-evidence request set requests[%d] is invalid", index)
		}
		if _, duplicate := seenScenario[selection.ScenarioID]; duplicate {
			return fmt.Errorf("UniRouter source-evidence request set requests[%d] duplicates a scenario", index)
		}
		if _, duplicate := seenRequest[requestKey]; duplicate {
			return fmt.Errorf("UniRouter source-evidence request set requests[%d] duplicates a tenant request", index)
		}
		seenScenario[selection.ScenarioID] = struct{}{}
		seenRequest[requestKey] = struct{}{}
	}
	return nil
}

func validateSourceEvidenceV1SourceScope(scope sourceEvidenceV1SourceScope) error {
	if scope.SchemaVersion != uniRouterSourceEvidenceV1SourceScopeSchema || scope.Product != "unirouter" ||
		!uniRouterSourceEvidenceV1SourcePattern.MatchString(scope.SourceInstance) ||
		scope.IdentityAuthority != uniRouterSourceEvidenceV1IdentityAuthority ||
		scope.RuntimeObservation != uniRouterSourceEvidenceV1RuntimeObservation ||
		scope.FeedSnapshotSchema != uniRouterSourceEvidenceV1FeedSchema {
		return errors.New("UniRouter source-evidence Source scope is invalid")
	}
	return nil
}

func validateSourceEvidenceV1SourceFacts(
	facts sourceEvidenceV1SourceFacts,
	requestSet sourceEvidenceV1RequestSet,
) error {
	if facts.SchemaVersion != uniRouterSourceEvidenceV1SourceFactsSchema ||
		!campaignPackageDigestPattern.MatchString(facts.RequestSetSHA256) ||
		!campaignPackageDigestPattern.MatchString(facts.SourceBindingSHA256) ||
		facts.CampaignID != requestSet.CampaignID || len(facts.Requests) != len(requestSet.Requests) {
		return errors.New("UniRouter source-evidence Source facts identity is invalid")
	}
	if _, err := parseSourceEvidenceV1Time(facts.GeneratedAt); err != nil {
		return err
	}
	if err := validateSourceEvidenceV1Release(facts.Release); err != nil {
		return err
	}
	if err := validateSourceEvidenceV1Protocol(facts.Protocol); err != nil {
		return err
	}
	if _, err := parseSourceEvidenceV1Time(facts.Snapshot.TransactionObservedAt); err != nil {
		return err
	}
	if facts.Snapshot.Isolation != "repeatable read" || !facts.Snapshot.ReadOnly ||
		facts.Snapshot.TransactionIDAssigned || facts.Snapshot.SelectionMode != "bounded_tenant_request_set" {
		return errors.New("UniRouter source-evidence snapshot does not prove bounded read-only capture")
	}
	counts := sourceEvidenceV1FactCounts{RequestedRequests: len(facts.Requests)}
	seen := make(map[string]struct{}, len(facts.Requests))
	for index, request := range facts.Requests {
		selection := requestSet.Requests[index]
		requestKey := fmt.Sprintf("%d/%s", request.OrgID, request.RequestID)
		if !sourceEvidenceV1PortableWithin(request.ScenarioID, 120) ||
			!sourceEvidenceV1PortableWithin(request.RequestID, 64) ||
			request.OrgID <= 0 || request.ProjectID <= 0 ||
			request.ScenarioID != selection.ScenarioID || request.RequestID != selection.RequestID ||
			request.OrgID != selection.OrgID || request.ProjectID != selection.ProjectID {
			return fmt.Errorf("UniRouter source-evidence Source facts request %d contradicts the request set", index)
		}
		if _, duplicate := seen[requestKey]; duplicate {
			return fmt.Errorf("UniRouter source-evidence Source facts request %d is duplicated", index)
		}
		seen[requestKey] = struct{}{}
		if request.CallLog != nil {
			if err := validateSourceEvidenceV1CallLog(*request.CallLog); err != nil {
				return fmt.Errorf("UniRouter source-evidence requests[%d].call_log: %w", index, err)
			}
			counts.CallLogs++
		}
		for traceIndex, trace := range request.TraceEvents {
			if err := validateSourceEvidenceV1Trace(trace); err != nil {
				return fmt.Errorf("UniRouter source-evidence requests[%d].trace_events[%d]: %w", index, traceIndex, err)
			}
		}
		for attemptIndex, attempt := range request.ProviderAttempts {
			if err := validateSourceEvidenceV1ProviderAttempt(attempt); err != nil {
				return fmt.Errorf("UniRouter source-evidence requests[%d].provider_attempts[%d]: %w", index, attemptIndex, err)
			}
		}
		if request.CompletedFeed != nil {
			if err := validateSourceEvidenceV1CompletedFeed(*request.CompletedFeed); err != nil {
				return fmt.Errorf("UniRouter source-evidence requests[%d].completed_feed: %w", index, err)
			}
			counts.CompletedFeedItems++
		}
		if request.CallLog != nil && request.CompletedFeed != nil {
			call := request.CallLog
			feed := request.CompletedFeed.ContentSummary
			if call.Model != feed.Model || call.PromptTokens != int64(feed.PromptTokens) ||
				call.CompletionTokens != int64(feed.CompletionTokens) ||
				call.CostMicros != feed.ChargedCostMicros ||
				call.UsageAuthority != feed.UsageAuthority || call.UsageFinality != feed.UsageFinality {
				return fmt.Errorf("UniRouter source-evidence requests[%d] call/feed terminal facts conflict", index)
			}
		}
		for gapIndex, gap := range request.CaptureGaps {
			if expected, exists := uniRouterSourceEvidenceV1CaptureGaps[gap.Code]; !exists || gap != expected {
				return fmt.Errorf("UniRouter source-evidence requests[%d].capture_gaps[%d] is invalid", index, gapIndex)
			}
		}
		counts.TraceEvents += len(request.TraceEvents)
		counts.ProviderAttempts += len(request.ProviderAttempts)
		counts.CaptureGaps += len(request.CaptureGaps)
	}
	counts.TotalFacts = counts.CallLogs + counts.TraceEvents + counts.ProviderAttempts +
		counts.CompletedFeedItems + counts.CaptureGaps
	if counts.TraceEvents > uniRouterSourceEvidenceV1MaxFacts ||
		counts.ProviderAttempts > uniRouterSourceEvidenceV1MaxFacts ||
		counts.TotalFacts > uniRouterSourceEvidenceV1MaxFacts || counts != facts.Counts {
		return errors.New("UniRouter source-evidence Source facts counts are invalid")
	}
	if facts.FeedRange.Selection != "bounded_request_set" ||
		facts.FeedRange.SelectedItems != counts.CompletedFeedItems ||
		facts.FeedRange.CursorClaim != "not_part_of_source_facts" ||
		facts.FeedRange.HighWatermarkClaim != "not_part_of_source_facts" {
		return errors.New("UniRouter source-evidence Source facts exceed the bounded feed claim")
	}
	if counts.CompletedFeedItems == 0 {
		if facts.FeedRange.MinSequenceID != nil || facts.FeedRange.MaxSequenceID != nil {
			return errors.New("UniRouter source-evidence empty feed range claims sequence bounds")
		}
	} else if facts.FeedRange.MinSequenceID == nil || facts.FeedRange.MaxSequenceID == nil ||
		*facts.FeedRange.MinSequenceID <= 0 || *facts.FeedRange.MinSequenceID > *facts.FeedRange.MaxSequenceID {
		return errors.New("UniRouter source-evidence feed range bounds are invalid")
	}
	if facts.Completeness.ClaimScope != uniRouterSourceEvidenceV1SourceFactsSchema ||
		facts.Completeness.State != "complete" || !facts.Completeness.BoundedRequestSetComplete ||
		facts.Completeness.ExecutionEvidenceV2Projection != "partial" || facts.Completeness.RuntimeCloseMarkerAvailable {
		return errors.New("UniRouter source-evidence outer and nested completeness claims are invalid")
	}
	if facts.Safety.DatabaseMutation != "none" || facts.Safety.AdditionalGatewayCalls != 0 ||
		facts.Safety.ProviderCalls != 0 || facts.Safety.FeedCalls != 0 || facts.Safety.UpstreamCalls != 0 ||
		facts.Safety.RawPromptsIncluded || facts.Safety.RawResponsesIncluded || facts.Safety.SecretsIncluded ||
		facts.Safety.RawIPsIncluded || facts.Safety.RawErrorsIncluded {
		return errors.New("UniRouter source-evidence safety claim is invalid")
	}
	return nil
}

func validateSourceEvidenceV1Release(release sourceEvidenceV1Release) error {
	if release.ReleaseSequence <= 0 || !sourceEvidenceV1PortableWithin(release.Version, 128) ||
		!uniRouterSourceEvidenceV1GitObjectPattern.MatchString(release.Commit) ||
		!uniRouterSourceEvidenceV1HashPattern.MatchString(release.PackageSHA256) ||
		!uniRouterSourceEvidenceV1MetadataPattern.MatchString(release.TrustedKeyID) ||
		!sourceEvidenceV1PortableWithin(release.TrustedKeyID, 64) ||
		release.RecordedSchema == "" || release.CurrentSchema == "" ||
		release.RecordedSchema != release.CurrentSchema {
		return errors.New("UniRouter source-evidence release identity is invalid")
	}
	_, err := parseSourceEvidenceV1Time(release.RecordedAt)
	return err
}

func validateSourceEvidenceV1Protocol(protocol sourceEvidenceV1Protocol) error {
	if protocol.PinVersion == "" || protocol.ProtocolVersion == "" ||
		!campaignPackageDigestPattern.MatchString(protocol.ReleaseDigest) ||
		!campaignPackageDigestPattern.MatchString(protocol.PinSHA256) ||
		len(protocol.Components) == 0 || len(protocol.Components) > 64 {
		return errors.New("UniRouter source-evidence protocol identity is invalid")
	}
	previous := ""
	for index, component := range protocol.Components {
		if !uniRouterSourceEvidenceV1StagePattern.MatchString(component.Name) ||
			!campaignPackageDigestPattern.MatchString(component.SHA256) || component.Name <= previous {
			return fmt.Errorf("UniRouter source-evidence protocol components[%d] is invalid or not strictly sorted", index)
		}
		previous = component.Name
	}
	return nil
}

func validateSourceEvidenceV1CallLog(fact sourceEvidenceV1CallLogFact) error {
	if !campaignPackageDigestPattern.MatchString(fact.FactSHA256) ||
		!sourceEvidenceV1PortableWithin(fact.SourceRef, 512) || fact.ID <= 0 ||
		strings.TrimSpace(fact.Model) == "" || len(fact.Model) > 200 ||
		strings.TrimSpace(fact.Provider) == "" || len(fact.Provider) > 120 ||
		fact.PromptTokens < 0 || fact.CompletionTokens < 0 || fact.TotalTokens < 0 ||
		fact.TotalTokens != fact.PromptTokens+fact.CompletionTokens || fact.Currency != "CNY" ||
		fact.AmountScale != 1_000_000 || !sourceEvidenceV1UnsignedDecimal(fact.CostMicros) ||
		fact.StatusCode < 0 || fact.StatusCode > 999 || fact.LatencyMS < 0 ||
		fact.PayloadCaptureMode != "metadata_only" || !sourceEvidenceV1ActorType(fact.ActorType) {
		return errors.New("call log violates the payload-neutral execution and money contract")
	}
	if fact.UpstreamStatusCode != nil && (*fact.UpstreamStatusCode < 0 || *fact.UpstreamStatusCode > 999) {
		return errors.New("call log upstream status is invalid")
	}
	declared := fact.FactSHA256
	fact.FactSHA256 = ""
	if sourceEvidenceV1SemanticSHA256(fact) != declared {
		return errors.New("call log fact_sha256 does not match canonical fact content")
	}
	_, err := parseSourceEvidenceV1Time(fact.CreatedAt)
	return err
}

func validateSourceEvidenceV1Trace(fact sourceEvidenceV1TraceFact) error {
	if !campaignPackageDigestPattern.MatchString(fact.FactSHA256) ||
		!sourceEvidenceV1PortableWithin(fact.SourceRef, 512) || fact.ID <= 0 ||
		!sourceEvidenceV1PortableWithin(fact.EventType, 120) ||
		!uniRouterSourceEvidenceV1StagePattern.MatchString(fact.Stage) ||
		!sourceEvidenceV1CompatibilityStage(fact.CompatibilityStage) ||
		(fact.Severity != "info" && fact.Severity != "warning" && fact.Severity != "error") ||
		fact.LatencyMS < 0 || fact.PayloadCaptureMode != "metadata_only" ||
		len(fact.MetadataKeys) > 64 || !sourceEvidenceV1ActorType(fact.ActorType) {
		return errors.New("trace fact violates the payload-neutral contract")
	}
	if fact.StatusCode != nil && (*fact.StatusCode < 0 || *fact.StatusCode > 999) {
		return errors.New("trace fact status is invalid")
	}
	for name, value := range map[string]struct {
		text string
		max  int
	}{
		"decision_kind": {fact.DecisionKind, 40}, "decision_result": {fact.DecisionResult, 40},
		"decision_reason": {fact.DecisionReason, 120}, "capture_gap": {fact.CaptureGap, 80},
	} {
		if value.text != "" && !sourceEvidenceV1PortableWithin(value.text, value.max) {
			return fmt.Errorf("trace fact %s is not a bounded machine token", name)
		}
	}
	previous := ""
	for _, key := range fact.MetadataKeys {
		if !uniRouterSourceEvidenceV1MetadataPattern.MatchString(key) || key <= previous {
			return errors.New("trace metadata_keys are not safe and strictly sorted")
		}
		previous = key
	}
	declared := fact.FactSHA256
	fact.FactSHA256 = ""
	if sourceEvidenceV1SemanticSHA256(fact) != declared {
		return errors.New("trace fact_sha256 does not match canonical fact content")
	}
	_, err := parseSourceEvidenceV1Time(fact.CreatedAt)
	return err
}

func validateSourceEvidenceV1ProviderAttempt(fact sourceEvidenceV1ProviderAttempt) error {
	if !campaignPackageDigestPattern.MatchString(fact.FactSHA256) ||
		!sourceEvidenceV1PortableWithin(fact.SourceRef, 512) ||
		!sourceEvidenceV1PortableWithin(fact.AttemptID, 64) ||
		fact.RouteSequence <= 0 || fact.RouteTotal <= 0 || fact.RouteSequence > fact.RouteTotal ||
		strings.TrimSpace(fact.Endpoint) == "" || strings.TrimSpace(fact.Provider) == "" ||
		strings.TrimSpace(fact.State) == "" || strings.TrimSpace(fact.OutcomeCertainty) == "" {
		return errors.New("provider attempt fact is invalid")
	}
	if _, exists := uniRouterSourceEvidenceV1FailureKinds[fact.FailureKind]; !exists {
		return errors.New("provider attempt failure_kind is outside the immutable contract")
	}
	switch fact.AccessBindingType {
	case "logical_access_credential":
		if !campaignPackageDigestPattern.MatchString(fact.LogicalCredentialCorrelationSHA256) {
			return errors.New("provider attempt logical credential correlation is invalid")
		}
	case "legacy_api_key":
		if fact.LogicalCredentialCorrelationSHA256 != "" {
			return errors.New("legacy provider attempt exposes a credential correlation")
		}
	default:
		return errors.New("provider attempt access binding type is invalid")
	}
	for _, amount := range []*string{fact.CalculatedCostMicros, fact.ChargedCostMicros} {
		if amount != nil && !sourceEvidenceV1UnsignedDecimal(*amount) {
			return errors.New("provider attempt cost is invalid")
		}
	}
	for _, instant := range []string{fact.StartedAt, fact.UpdatedAt} {
		if _, err := parseSourceEvidenceV1Time(instant); err != nil {
			return err
		}
	}
	for _, instant := range []string{fact.ResponseReceivedAt, fact.FinancialResolvedAt} {
		if instant != "" {
			if _, err := parseSourceEvidenceV1Time(instant); err != nil {
				return err
			}
		}
	}
	declared := fact.FactSHA256
	fact.FactSHA256 = ""
	if sourceEvidenceV1SemanticSHA256(fact) != declared {
		return errors.New("provider attempt fact_sha256 does not match canonical fact content")
	}
	return nil
}

func validateSourceEvidenceV1CompletedFeed(fact sourceEvidenceV1CompletedFeed) error {
	if !campaignPackageDigestPattern.MatchString(fact.FactSHA256) ||
		!sourceEvidenceV1PortableWithin(fact.SourceRef, 512) || fact.SequenceID <= 0 ||
		(fact.TerminalStatus != "settled" && fact.TerminalStatus != "cancelled") ||
		fact.Revision != "1" || !campaignPackageDigestPattern.MatchString(fact.SnapshotSHA256) ||
		strings.TrimSpace(fact.ContentSummary.Model) == "" || fact.ContentSummary.PromptTokens < 0 ||
		fact.ContentSummary.CompletionTokens < 0 ||
		!sourceEvidenceV1UnsignedDecimal(fact.ContentSummary.CalculatedCostMicros) ||
		!sourceEvidenceV1UnsignedDecimal(fact.ContentSummary.ChargedCostMicros) {
		return errors.New("completed feed fact is invalid")
	}
	declared := fact.FactSHA256
	fact.FactSHA256 = ""
	if sourceEvidenceV1SemanticSHA256(fact) != declared {
		return errors.New("completed feed fact_sha256 does not match canonical fact content")
	}
	_, err := parseSourceEvidenceV1Time(fact.TerminalAvailableAt)
	return err
}

func sourceEvidenceV1SemanticSHA256(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return campaignPackageSHA256(payload)
}

func sourceEvidenceV1UnsignedDecimal(value string) bool {
	if value == "0" {
		return true
	}
	if value == "" || value[0] < '1' || value[0] > '9' {
		return false
	}
	for index := 1; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

func sourceEvidenceV1ActorType(value string) bool {
	switch value {
	case "", "human", "agent", "service", "api_key", "unknown":
		return true
	default:
		return false
	}
}

func sourceEvidenceV1CompatibilityStage(value string) bool {
	for _, stage := range executionEvidenceStages {
		if value == stage {
			return true
		}
	}
	return false
}

func sourceEvidenceV1SourceIdentity(scope sourceEvidenceV1SourceScope) sourceEvidenceV1SourceIdentityProjection {
	return sourceEvidenceV1SourceIdentityProjection{
		Product:            scope.Product,
		Instance:           scope.SourceInstance,
		IdentityAuthority:  scope.IdentityAuthority,
		RuntimeObservation: scope.RuntimeObservation,
		FeedSnapshotSchema: scope.FeedSnapshotSchema,
	}
}

func sourceEvidenceV1ArtifactMediaType(name string) (string, error) {
	switch {
	case name == uniRouterSourceEvidenceV1RequestSetPath:
		return uniRouterSourceEvidenceV1RequestMediaType, nil
	case name == uniRouterSourceEvidenceV1SourceScopePath:
		return uniRouterSourceEvidenceV1ScopeMediaType, nil
	case name == uniRouterSourceEvidenceV1SourceFactsPath:
		return uniRouterSourceEvidenceV1FactsMediaType, nil
	case uniRouterSourceEvidenceV1MemberPathPattern.MatchString(name):
		return uniRouterSourceEvidenceV1MemberMediaType, nil
	default:
		return "", errors.New("UniRouter source-evidence manifest contains an unsupported artifact path")
	}
}

func sourceEvidenceV1CompatibilityPath(index int, selection sourceEvidenceV1RequestSelection) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", selection.OrgID, selection.RequestID)))
	return fmt.Sprintf("payload/compatibility/%03d-%s.json", index+1, hex.EncodeToString(digest[:8]))
}

func openCanonicalSourceEvidenceV1Container(packageBytes []byte) (map[string][]byte, []string, error) {
	if len(packageBytes) == 0 || len(packageBytes) > uniRouterSourceEvidenceV1MaxPackageBytes {
		return nil, nil, errors.New("UniRouter source-evidence package size is invalid")
	}
	compressed := bytes.NewReader(packageBytes)
	gzipReader, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, nil, fmt.Errorf("open UniRouter source-evidence package: %w", err)
	}
	gzipReader.Multistream(false)
	tarReader := tar.NewReader(gzipReader)
	entries := make(map[string][]byte)
	order := make([]string, 0, uniRouterSourceEvidenceV1MaxEntries)
	var expanded int64
	for len(order) <= uniRouterSourceEvidenceV1MaxEntries {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			_ = gzipReader.Close()
			return nil, nil, fmt.Errorf("read UniRouter source-evidence package: %w", nextErr)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			_ = gzipReader.Close()
			return nil, nil, errors.New("UniRouter source-evidence package contains a non-file entry")
		}
		name, normalizeErr := normalizeSourceEvidenceV1PackagePath(header.Name)
		if normalizeErr != nil || name != header.Name || header.Size < 0 ||
			header.Size > uniRouterSourceEvidenceV1MaxContractBytes ||
			expanded > uniRouterSourceEvidenceV1MaxExpandedBytes-header.Size {
			_ = gzipReader.Close()
			return nil, nil, errors.New("UniRouter source-evidence package contains an unsafe entry")
		}
		expanded += header.Size
		if _, duplicate := entries[name]; duplicate {
			_ = gzipReader.Close()
			return nil, nil, errors.New("UniRouter source-evidence package contains duplicate entries")
		}
		body, readErr := io.ReadAll(io.LimitReader(tarReader, header.Size+1))
		if readErr != nil || int64(len(body)) != header.Size {
			_ = gzipReader.Close()
			return nil, nil, errors.New("UniRouter source-evidence package entry size is invalid")
		}
		entries[name] = body
		order = append(order, name)
	}
	if len(order) > uniRouterSourceEvidenceV1MaxEntries {
		_ = gzipReader.Close()
		return nil, nil, errors.New("UniRouter source-evidence package has too many entries")
	}
	trailing, err := io.ReadAll(io.LimitReader(gzipReader, 1))
	if err != nil || len(trailing) != 0 {
		_ = gzipReader.Close()
		return nil, nil, errors.New("UniRouter source-evidence package contains bytes after the canonical tar end")
	}
	if err := gzipReader.Close(); err != nil || compressed.Len() != 0 {
		return nil, nil, errors.New("UniRouter source-evidence package has a concatenated gzip member or trailing bytes")
	}
	return entries, order, nil
}

func normalizeSourceEvidenceV1PackagePath(value string) (string, error) {
	if value == uniRouterSourceEvidenceV1ManifestPath || value == uniRouterSourceEvidenceV1SignaturePath {
		return value, nil
	}
	if value == "" || len(value) > 240 || strings.Contains(value, "\\") ||
		strings.ContainsRune(value, '\x00') || strings.HasPrefix(value, "/") {
		return "", errors.New("UniRouter source-evidence package path is unsafe")
	}
	clean := path.Clean(value)
	if clean != value || clean == "payload" || !strings.HasPrefix(clean, "payload/") ||
		strings.Contains(clean, "../") {
		return "", errors.New("UniRouter source-evidence artifact path is not normalized")
	}
	return clean, nil
}

func verifyCanonicalSourceEvidenceV1Container(
	packageBytes []byte,
	order []string,
	entries map[string][]byte,
	createdAt time.Time,
) error {
	var canonical bytes.Buffer
	gzipWriter := gzip.NewWriter(&canonical)
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
			return err
		}
		if _, err := tarWriter.Write(body); err != nil {
			return err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return err
	}
	if err := gzipWriter.Close(); err != nil {
		return err
	}
	if !bytes.Equal(canonical.Bytes(), packageBytes) {
		return errors.New("UniRouter source-evidence package is not the single canonical container representation")
	}
	return nil
}

func decodeCanonicalSourceEvidenceV1[T any](body []byte, name string) (T, error) {
	var result T
	if len(body) == 0 || len(body) > uniRouterSourceEvidenceV1MaxContractBytes {
		return result, fmt.Errorf("%s size is invalid", name)
	}
	if err := scanStrictJSONDocument(body, name, nil); err != nil {
		return result, err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, fmt.Errorf("decode %s: %w", name, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return result, fmt.Errorf("decode trailing %s data", name)
	}
	canonical, err := json.Marshal(result)
	if err != nil || !bytes.Equal(canonical, body) {
		return result, fmt.Errorf("%s is not canonical JSON", name)
	}
	return result, nil
}

func parseSourceEvidenceV1Time(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Location() != time.UTC || value != parsed.UTC().Format(time.RFC3339Nano) {
		return time.Time{}, errors.New("UniRouter source-evidence timestamp is not canonical UTC")
	}
	return parsed, nil
}

func sourceEvidenceV1PortableWithin(value string, maximum int) bool {
	return len(value) > 0 && len(value) <= maximum && uniRouterSourceEvidenceV1PortablePattern.MatchString(value)
}

func sourceEvidenceV1PublicKeyID(publicKey ed25519.PublicKey) string {
	digest := sha256.Sum256(publicKey)
	return "ed25519:sha256:" + hex.EncodeToString(digest[:])
}
