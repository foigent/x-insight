package uniroutercampaign

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/boundedoffline"
	"github.com/foigent/x-insight/internal/integrationcontrol"
	"github.com/foigent/x-insight/internal/investigation"
)

var errInjectedReceiptCommit = errors.New("injected receipt commit failure")

type fixtureContractAdapter struct {
	identity unirouter.CampaignPackageContractIdentity
	opened   unirouter.OpenedCampaignPackage
	called   int
}

func (adapter *fixtureContractAdapter) ContractIdentity() unirouter.CampaignPackageContractIdentity {
	return adapter.identity
}

func (adapter *fixtureContractAdapter) VerifyAndOpen(
	_ []byte,
	_ unirouter.SourceEvidenceTrustPolicy,
) (unirouter.OpenedCampaignPackage, error) {
	adapter.called++
	return adapter.opened, nil
}

type recordingPersister struct {
	seen        map[string]string
	calls       int
	provenances []investigation.ImportProvenance
}

func newRecordingPersister() *recordingPersister {
	return &recordingPersister{seen: make(map[string]string)}
}

func (persister *recordingPersister) PersistEventsWithProvenance(
	_ context.Context,
	events []map[string]any,
	provenance investigation.ImportProvenance,
) (db.PersistenceReport, error) {
	persister.calls++
	persister.provenances = append(persister.provenances, provenance)
	report := db.PersistenceReport{EventsTotal: len(events)}
	for _, event := range events {
		identity := fmt.Sprint(event["idempotency_key"])
		eventHash := fmt.Sprint(event["event_hash"])
		priorHash, exists := persister.seen[identity]
		switch {
		case !exists:
			persister.seen[identity] = eventHash
			report.Inserted++
		case priorHash == eventHash:
			report.DuplicateNoop++
		default:
			report.IdempotencyConflict++
		}
	}
	return report, nil
}

type storedOperation struct {
	binding boundedoffline.OperationBinding
	receipt connector.SourceNeutralOperationalReceipt
}

type memoryOperationStore struct {
	bindings       map[string]boundedoffline.OperationBinding
	receipts       map[string]storedOperation
	failNextCommit bool
}

func newMemoryOperationStore() *memoryOperationStore {
	return &memoryOperationStore{
		bindings: make(map[string]boundedoffline.OperationBinding),
		receipts: make(map[string]storedOperation),
	}
}

func (store *memoryOperationStore) WithOperation(
	ctx context.Context,
	binding boundedoffline.OperationBinding,
	operation func(context.Context, *connector.SourceNeutralOperationalReceipt) (
		connector.SourceNeutralOperationalReceipt,
		error,
	),
) (connector.SourceNeutralOperationalReceipt, error) {
	key := binding.ConnectorID + "\x00" + binding.OperationID
	claimed, claimedExists := store.bindings[key]
	if claimedExists && claimed != binding {
		return connector.SourceNeutralOperationalReceipt{}, boundedoffline.ErrOperationConflict
	}
	store.bindings[key] = binding
	stored, exists := store.receipts[key]
	if exists && stored.binding != binding {
		return connector.SourceNeutralOperationalReceipt{}, boundedoffline.ErrOperationConflict
	}
	var existing *connector.SourceNeutralOperationalReceipt
	if exists {
		copy := stored.receipt
		existing = &copy
	}
	result, err := operation(ctx, existing)
	if err != nil {
		return connector.SourceNeutralOperationalReceipt{}, err
	}
	if store.failNextCommit {
		store.failNextCommit = false
		return connector.SourceNeutralOperationalReceipt{}, errInjectedReceiptCommit
	}
	if exists {
		storedBytes, storedErr := stored.receipt.CanonicalBytes()
		resultBytes, resultErr := result.CanonicalBytes()
		if storedErr != nil || resultErr != nil || !bytes.Equal(storedBytes, resultBytes) {
			return connector.SourceNeutralOperationalReceipt{}, boundedoffline.ErrOperationConflict
		}
	}
	store.receipts[key] = storedOperation{binding: binding, receipt: result}
	return result, nil
}

func TestPrepareImportRejectsUnsealedAuthorityBeforeProducerParserOrDatabase(t *testing.T) {
	packageBytes, request, binding, adapter := campaignImportFixture(t)
	request.Verification.Trust.Purpose = "upgrade"
	if _, err := PrepareImport(
		packageBytes,
		request,
		binding,
		evidenceSchemaPath(),
		adapter,
		integrationcontrol.N1AuthorizationVerification{},
	); err == nil {
		t.Fatal("unsealed authority prepared a source-evidence import")
	}
	if adapter.called != 0 {
		t.Fatalf("producer parser calls under upgrade authority=%d", adapter.called)
	}
}

func TestPrepareVerifiedImportRejectsSourceInstanceDrift(t *testing.T) {
	packageBytes, request, binding, adapter := campaignImportFixture(t)
	adapter.opened.Projection.SourceInstance = "other-source-instance"
	verified, err := unirouter.VerifyCampaignPackage(packageBytes, request.Verification, adapter)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareVerifiedImport(request, binding, evidenceSchemaPath(), verified); err == nil ||
		!strings.Contains(err.Error(), "contradicts its frozen Source") {
		t.Fatalf("source_instance drift error=%v", err)
	}
}

func TestImportPreparedKeepsPackageScopeSeparateFromPartialSourceProfile(t *testing.T) {
	packageBytes, request, binding, adapter := campaignImportFixture(t)
	prepared, err := prepareFixtureImport(packageBytes, request, binding, adapter)
	if err != nil {
		t.Fatal(err)
	}
	persister := newRecordingPersister()
	operations := newMemoryOperationStore()
	importer, err := NewImporter(
		persister,
		operations,
		func() time.Time { return time.Date(2026, 8, 10, 8, 9, 10, 123000000, time.UTC) },
	)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := importer.ImportPrepared(context.Background(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.PackageScopeComplete || receipt.AllMemberSourceProfilesComplete ||
		receipt.Counts.SourceProfilePartialMembers != 1 ||
		receipt.Counts.SourceProfileCompleteMembers != 0 {
		t.Fatalf("package/source completeness collapsed: %+v", receipt)
	}
	member := receipt.Members[0]
	if member.SourceProfileComplete || member.RuntimeCloseMarkerAvailable ||
		member.SourceProfileState != "partial" ||
		!slices.Contains(member.GapCodes, unirouter.RuntimeCloseMarkerUnavailableGapCode) {
		t.Fatalf("partial v2 member lost its explicit gap: %+v", member)
	}
	if receipt.OperationalReceipt.Counts.Received == 0 ||
		receipt.OperationalReceipt.Counts.Inserted != receipt.OperationalReceipt.Counts.Received ||
		receipt.OperationalReceipt.Counts.DuplicateNoop != 0 {
		t.Fatalf("first import convergence=%+v", receipt.OperationalReceipt.Counts)
	}
	if persister.calls != 1 || len(persister.provenances) != 1 {
		t.Fatalf("persistence calls=%d provenance=%d", persister.calls, len(persister.provenances))
	}
	provenance := persister.provenances[0]
	if provenance.IntegrationMode != investigation.IntegrationBoundedOffline ||
		provenance.ProofClass != investigation.ProofOperatorApproved ||
		provenance.AssuranceMode != investigation.AssuranceBoundedOffline ||
		provenance.SourcePayloadSHA256 != receipt.Members[0].SHA256 ||
		provenance.SourceWatermark != "" || provenance.ImportWatermark != "" ||
		provenance.TimelineComplete {
		t.Fatalf("bounded-offline provenance=%+v", provenance)
	}
	firstSHA, err := receipt.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	replay, err := importer.ImportPrepared(context.Background(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	replaySHA, err := replay.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	if replaySHA != firstSHA || persister.calls != 1 {
		t.Fatalf("completed replay digest=%s/%s persistence_calls=%d", firstSHA, replaySHA, persister.calls)
	}
	canonical, err := receipt.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	for _, payloadFact := range [][]byte{
		[]byte(`"provider-a"`),
		[]byte(`"model-a"`),
		[]byte(`"req-v2-fallback-demo"`),
	} {
		if bytes.Contains(canonical, payloadFact) {
			t.Fatalf("typed receipt leaked member payload fact %s", payloadFact)
		}
	}
}

func TestImportPreparedCrashAfterEvidenceBeforeReceiptConvergesOnReplay(t *testing.T) {
	packageBytes, request, binding, adapter := campaignImportFixture(t)
	prepared, err := prepareFixtureImport(packageBytes, request, binding, adapter)
	if err != nil {
		t.Fatal(err)
	}
	persister := newRecordingPersister()
	operations := newMemoryOperationStore()
	operations.failNextCommit = true
	times := []time.Time{
		time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 10, 9, 0, 1, 0, time.UTC),
	}
	index := 0
	importer, err := NewImporter(persister, operations, func() time.Time {
		value := times[index]
		index++
		return value
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := importer.ImportPrepared(
		context.Background(),
		prepared,
	); !errors.Is(err, errInjectedReceiptCommit) {
		t.Fatalf("injected receipt crash error=%v", err)
	}
	if persister.calls != 1 || len(operations.bindings) != 1 || len(operations.receipts) != 0 {
		t.Fatalf(
			"after crash persistence=%d bindings=%d receipts=%d",
			persister.calls,
			len(operations.bindings),
			len(operations.receipts),
		)
	}
	receipt, err := importer.ImportPrepared(context.Background(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.OperationalReceipt.Counts.Inserted != 0 ||
		receipt.OperationalReceipt.Counts.DuplicateNoop != receipt.OperationalReceipt.Counts.Received ||
		persister.calls != 2 || len(operations.receipts) != 1 {
		t.Fatalf(
			"retry convergence=%+v persistence=%d receipts=%d",
			receipt.OperationalReceipt.Counts,
			persister.calls,
			len(operations.receipts),
		)
	}
	canonical, err := receipt.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	replay, err := importer.ImportPrepared(context.Background(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	replayCanonical, err := replay.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, replayCanonical) || persister.calls != 2 || index != 2 {
		t.Fatalf("completed replay was not side-effect free: persistence=%d clock=%d", persister.calls, index)
	}
}

func TestCompletedCampaignImportReadbackRejectsPostImportReceiptTamper(t *testing.T) {
	packageBytes, request, binding, adapter := campaignImportFixture(t)
	prepared, err := prepareFixtureImport(packageBytes, request, binding, adapter)
	if err != nil {
		t.Fatal(err)
	}
	persister := newRecordingPersister()
	operations := newMemoryOperationStore()
	importer, err := NewImporter(
		persister,
		operations,
		func() time.Time { return time.Date(2030, 1, 1, 2, 0, 0, 0, time.UTC) },
	)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := importer.ImportPrepared(context.Background(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	if err := readbackCompletedCampaignImport(
		context.Background(), operations, prepared.operationBinding, receipt.OperationalReceipt,
	); err != nil {
		t.Fatalf("exact completed receipt readback: %v", err)
	}
	key := prepared.operationBinding.ConnectorID + "\x00" + prepared.operationBinding.OperationID
	stored := operations.receipts[key]
	stored.receipt.Counts.Inserted--
	stored.receipt.Counts.DuplicateNoop++
	operations.receipts[key] = stored
	if err := readbackCompletedCampaignImport(
		context.Background(), operations, prepared.operationBinding, receipt.OperationalReceipt,
	); !errors.Is(err, boundedoffline.ErrOperationConflict) {
		t.Fatalf("post-import durable receipt tamper error=%v", err)
	}
}

func TestTerminalProjectionRejectsZeroManualAndTamperedImportAuthority(t *testing.T) {
	packageBytes, request, binding, adapter := campaignImportFixture(t)
	prepared, err := prepareFixtureImport(packageBytes, request, binding, adapter)
	if err != nil {
		t.Fatal(err)
	}
	operations := newMemoryOperationStore()
	importer, err := NewImporter(
		newRecordingPersister(),
		operations,
		func() time.Time { return time.Date(2030, 1, 1, 2, 0, 0, 0, time.UTC) },
	)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := importer.ImportPrepared(context.Background(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	manual := VerifiedCampaignImportAuthority{
		receipt:          receipt,
		verifiedPackage:  prepared.verified,
		operationBinding: prepared.operationBinding,
	}
	tampered := manual
	tampered.verificationSeal = digestRef("tampered-authority-seal")
	for name, candidate := range map[string]VerifiedCampaignImportAuthority{
		"zero":     {},
		"manual":   manual,
		"tampered": tampered,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ProjectCampaignTerminalProjection(
				candidate,
				integrationcontrol.VerifiedComponentLifecycle{},
				integrationcontrol.ReleaseAuthority{},
			); err == nil {
				t.Fatal("unsealed import authority reached terminal projection")
			}
		})
	}
}

func TestImportPreparedVerifiedRejectsPlainUnsealedPreparationBeforePersistence(t *testing.T) {
	packageBytes, request, binding, adapter := campaignImportFixture(t)
	prepared, err := prepareFixtureImport(packageBytes, request, binding, adapter)
	if err != nil {
		t.Fatal(err)
	}
	persister := newRecordingPersister()
	operations := newMemoryOperationStore()
	importer, err := NewImporter(persister, operations, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := importer.ImportPreparedVerified(context.Background(), prepared); err == nil ||
		!strings.Contains(err.Error(), "preauthorized") {
		t.Fatalf("unsealed prepared authority error=%v", err)
	}
	if persister.calls != 0 || len(operations.bindings) != 0 || len(operations.receipts) != 0 {
		t.Fatalf(
			"unsealed import mutated persistence=%d bindings=%d receipts=%d",
			persister.calls,
			len(operations.bindings),
			len(operations.receipts),
		)
	}
}

func TestOperationBindingSealsPackageSourceAuthorizationTrustAndMemberFacts(t *testing.T) {
	packageBytes, request, binding, adapter := campaignImportFixture(t)
	prepared, err := prepareFixtureImport(packageBytes, request, binding, adapter)
	if err != nil {
		t.Fatal(err)
	}
	base := prepared.operationBinding.RequestSHA256
	mutations := []func(*unirouter.CampaignPackageProjection){
		func(projection *unirouter.CampaignPackageProjection) {
			projection.SourceBindingSHA256 = digestRef("changed-source")
		},
		func(projection *unirouter.CampaignPackageProjection) {
			projection.AuthorizationManifestSHA256 = digestRef("changed-authorization-manifest")
		},
		func(projection *unirouter.CampaignPackageProjection) {
			projection.AuthorizationSHA256 = digestRef("changed-authorization")
		},
		func(projection *unirouter.CampaignPackageProjection) {
			projection.AuthorizationSignatureSHA256 = digestRef("changed-authorization-signature")
		},
		func(projection *unirouter.CampaignPackageProjection) {
			projection.Signature.TrustSetSHA256 = digestRef("changed-trust")
		},
	}
	for index, mutate := range mutations {
		projection := prepared.verified.Projection()
		mutate(&projection)
		changed, err := bindImportOperation(binding, request, projection, prepared.members)
		if err != nil {
			t.Fatal(err)
		}
		if changed.RequestSHA256 == base {
			t.Fatalf("operation binding mutation %d was not sealed", index)
		}
	}
	changedMembers := append([]preparedMember(nil), prepared.members...)
	changedMembers[0].inventory.SHA256 = digestRef("changed-member")
	changed, err := bindImportOperation(
		binding,
		request,
		prepared.verified.Projection(),
		changedMembers,
	)
	if err != nil {
		t.Fatal(err)
	}
	if changed.RequestSHA256 == base {
		t.Fatal("operation binding member content identity was not sealed")
	}
}

func TestProductLocalEntryHasNoTransportCredentialCursorOrWorkerInput(t *testing.T) {
	assertStructFields(t, reflect.TypeOf(ImportRequest{}), []string{
		"OperationID",
		"Verification",
	})
	assertStructFields(t, reflect.TypeOf(ComponentBinding{}), []string{
		"ConnectorID",
		"SourceSystemID",
		"AdapterManifestID",
	})
}

func TestMatchedCampaignTerminalProjectionFromImmutableUniRouterChain(t *testing.T) {
	receiptPayload, projectionPayload := matchedCampaignTerminalFixture(t)
	assertMatchedCampaignFixture(t, "import-receipt.valid.json", receiptPayload)
	assertMatchedCampaignFixture(t, "terminal-projection.valid.json", projectionPayload)
}

func matchedCampaignTerminalFixture(t *testing.T) ([]byte, []byte) {
	t.Helper()
	producerRoot := os.Getenv("XINSIGHT_UNIROUTER_SOURCE_EVIDENCE_V1_ROOT")
	neutralRoot := os.Getenv("XINSIGHT_N1_RELEASE_ASSET_DIRECTORY")
	if producerRoot == "" || neutralRoot == "" {
		t.Skip("immutable UniRouter and Neutral N1 fixture roots are required")
	}
	validRoot := filepath.Join(
		producerRoot, "contracts", "source-evidence", "v1", "fixtures", "conformance", "valid",
	)
	const (
		manifestSHA256      = "sha256:dbb526eb32c89b1f4771b6be265c28235ead650a1ab51557c48250cb04520a5c"
		authorizationSHA256 = "sha256:c05636524e3b5513a01d4705fc70c562b344ba28690aafa12c7c37dc5792d1a3"
		signatureSHA256     = "sha256:8f550ed24bdf7a9d8b93c298ff45acc8a9bb2c0c53b389bbd25a1ef869b92ec3"
		packageSHA256       = "sha256:42466d9a588503ca9978e4659ceff7a817cdf2f94dd744083f14b0f4e6899c09"
		xInsightAckSHA256   = "sha256:1ec9bc8601e8221bff01b343f4784af8cdd84a1a858facbe71edb9ccc65056d6"
	)
	exact := func(relative, expected string) []byte {
		t.Helper()
		path := filepath.Join(validRoot, filepath.FromSlash(relative))
		payload, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if actual := sha256Ref(payload); actual != expected {
			t.Fatalf("immutable fixture %s digest=%s want=%s", relative, actual, expected)
		}
		return payload
	}
	manifestPath := filepath.Join(validRoot, "manifest.json")
	exact("manifest.json", manifestSHA256)
	uniRouterAckPath := filepath.Join(validRoot, "unirouter-ack.json")
	exact(
		"unirouter-ack.json",
		"sha256:b420aeac0b384456bb3986630cd9120e9516ae9b2e11e138b874d59ebcc44566",
	)
	xInsightAckPath := filepath.Join(validRoot, "xinsight-ack.json")
	exact("xinsight-ack.json", xInsightAckSHA256)
	authorizationPath := filepath.Join(validRoot, "authorization.json")
	exact("authorization.json", authorizationSHA256)
	signaturePath := filepath.Join(validRoot, "authorization-signature.json")
	exact("authorization-signature.json", signatureSHA256)
	authorizationTrustPath := filepath.Join(validRoot, "authorization-trust-set.json")
	sourceTrustPath := filepath.Join(validRoot, "source-evidence-trust-set.json")
	exact("source-scope.json", "sha256:c8959a370ae9955df2d732897a9ed24ffff0a2b1d03844dd9ff3dbb8607f9020")
	packagePayload := exact("source-evidence-package.tar.gz", packageSHA256)
	if len(packagePayload) != 2618 {
		t.Fatalf("immutable package bytes=%d want=2618", len(packagePayload))
	}

	n1, err := integrationcontrol.NewN1Verifier(
		filepath.Join(neutralRoot, "foigent.integration-control.release.v1.json"),
		filepath.Join(
			neutralRoot,
			fmt.Sprintf(
				"foigent-integration-control-%s-%s-%s",
				integrationcontrol.FrozenN1Version,
				runtime.GOOS,
				runtime.GOARCH,
			),
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := n1.VerifySourceArchive(filepath.Join(
		neutralRoot,
		"foigent-integration-control-"+integrationcontrol.FrozenN1Version+"-source.tar",
	)); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	manifest, err := n1.VerifyManifest(ctx, manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	ackReport, err := n1.VerifyAcknowledgement(ctx, manifestPath, xInsightAckPath)
	if err != nil || !ackReport.Valid || ackReport.AcknowledgementSHA256 != xInsightAckSHA256 {
		t.Fatalf("X acknowledgement report=%+v error=%v", ackReport, err)
	}
	authorization, err := n1.VerifyAuthorization(ctx, integrationcontrol.N1AuthorizationFiles{
		Manifest:                 manifestPath,
		UniRouterAcknowledgement: uniRouterAckPath,
		XInsightAcknowledgement:  xInsightAckPath,
		Authorization:            authorizationPath,
		Signature:                signaturePath,
		AuthorizationTrustSet:    authorizationTrustPath,
		SourceEvidenceTrustSet:   sourceTrustPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := authorization.ValidateAgainstManifest(manifest); err != nil {
		t.Fatal(err)
	}

	contract, err := integrationcontrol.ParseComponentContract(exact(
		"xinsight/component-contract.json",
		"sha256:2f8b817a726356235131fc63b80b1b320bc5a300e8471e64516b7cac523a40e1",
	))
	if err != nil {
		t.Fatal(err)
	}
	productRelease, err := integrationcontrol.ParseProductReleaseIdentity(exact(
		"xinsight/product-release-identity.json",
		"sha256:e391cdbc21a0e4b439c10c32164e749c28c51658269bf2505840e8133b7a1a27",
	))
	if err != nil {
		t.Fatal(err)
	}
	verifierRelease, err := integrationcontrol.ParseVerifierRelease(exact(
		"xinsight/verifier-release.json",
		"sha256:297ecfeed7a062ab8e52e3ae98ef82d47fa9d8c121d30f051f7d25be7504497e",
	))
	if err != nil {
		t.Fatal(err)
	}
	release := integrationcontrol.ReleaseAuthority{
		ProductRelease: productRelease,
		Verifier:       verifierRelease,
	}
	if err := release.Validate(); err != nil {
		t.Fatal(err)
	}
	contractSHA256, err := contract.SHA256()
	if err != nil || productRelease.ComponentContractSHA256 != contractSHA256 ||
		!strings.HasSuffix(productRelease.ProductVersion, "-synthetic-conformance") {
		t.Fatalf("synthetic X release authority does not converge: error=%v", err)
	}
	scope, err := integrationcontrol.ParseComponentScopeReceipt(exact(
		"xinsight/component-scope-receipt.json",
		"sha256:3d94607ebd000558d3877b6b3b9973d62a7a399e23567e1e2960bf3f85e99f23",
	))
	if err != nil {
		t.Fatal(err)
	}
	verification, err := integrationcontrol.ParseComponentVerificationReceipt(exact(
		"xinsight/component-verification-receipt.json",
		"sha256:71bedea6a9f5bdd297ac97cf218ba7a05f751827308b3bc9ab850934535c6a17",
	))
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := integrationcontrol.VerifyComponentLifecycle(scope, manifest, verification)
	if err != nil {
		t.Fatal(err)
	}
	xInsightAck, err := integrationcontrol.ParseComponentAcknowledgement(exact(
		"xinsight-ack.json", xInsightAckSHA256,
	))
	if err != nil || xInsightAck.ValidateAgainstVerification(verification) != nil {
		t.Fatalf("X acknowledgement lifecycle error=%v", err)
	}

	trustPayload := exact(
		"source-evidence-trust-set.json",
		"sha256:9a32b63baca57f9eeef2047c74e52c6a759cc3e336d35abadcc09b4cc30f96d8",
	)
	adapter, err := unirouter.NewUniRouterSourceEvidenceV1Adapter(trustPayload)
	if err != nil {
		t.Fatal(err)
	}
	request := ImportRequest{
		OperationID: "source-evidence-conformance-20300101-import-001",
		Verification: unirouter.CampaignPackageVerificationRequest{
			ExpectedContract: unirouter.CampaignPackageContractIdentity{
				ID:     authorization.UniRouterComponent.Contract.ID,
				SHA256: authorization.UniRouterComponent.Contract.SHA256,
			},
			ExpectedPackageID:                    packageSHA256,
			ExpectedPackageSHA256:                packageSHA256,
			ExpectedSourceBindingSHA256:          authorization.SourceBinding.SHA256,
			ExpectedAuthorizationManifestSHA256:  authorization.CampaignManifestSHA256,
			ExpectedAuthorizationSHA256:          authorization.AuthorizationSHA256,
			ExpectedAuthorizationSignatureSHA256: authorization.AuthorizationSignatureSHA256,
			Trust: unirouter.SourceEvidenceTrustPolicy{
				Purpose:           authorization.SourceEvidenceSigning.Purpose,
				Domain:            authorization.SourceEvidenceSigning.Domain,
				Algorithm:         unirouter.SourceEvidenceSignatureEd25519,
				SigningKeyRef:     authorization.SourceEvidenceSigning.SigningKeyRef,
				SignerPublicKeyID: authorization.SourceEvidenceSigning.SignerPublicKeyID,
				TrustSetSHA256:    authorization.SourceEvidenceSigning.TrustSetSHA256,
			},
		},
	}
	prepared, err := PrepareImport(
		packagePayload,
		request,
		ComponentBinding{
			ConnectorID:       "foigent.unirouter.poc-source-evidence",
			SourceSystemID:    unirouter.DefaultSourceSystemID,
			AdapterManifestID: unirouter.DefaultExecutionEvidenceAdapterID,
		},
		evidenceSchemaPath(),
		adapter,
		authorization,
	)
	if err != nil {
		t.Fatal(err)
	}
	persister := newRecordingPersister()
	operations := newMemoryOperationStore()
	importer, err := NewImporter(
		persister,
		operations,
		func() time.Time { return time.Date(2030, 1, 1, 0, 6, 0, 0, time.UTC) },
	)
	if err != nil {
		t.Fatal(err)
	}
	receipt, authority, err := importer.ImportPreparedVerified(ctx, prepared)
	if err != nil {
		t.Fatal(err)
	}
	tamperedCompatibilityReceipt := receipt
	tamperedCompatibilityReceipt.Members = append(
		[]unirouter.CampaignPackageMemberImportReceipt(nil), receipt.Members...,
	)
	tamperedCompatibilityReceipt.Members[0].GapCodes = nil
	tamperedCompatibilityReceipt.Counts.CaptureGaps = 0
	if _, err := tamperedCompatibilityReceipt.CanonicalBytes(); err == nil {
		t.Fatal("post-import compatibility receipt field tamper remained self-consistent")
	}
	if !receipt.PackageScopeComplete || receipt.AllMemberSourceProfilesComplete ||
		receipt.ManifestSHA256 != "sha256:52f87dc3d666a28662888541225a09440d7a767e443d5432b917084afd69fa81" ||
		receipt.Counts.SourceProfilePartialMembers != 1 || receipt.Counts.CaptureGaps != 5 ||
		len(receipt.Members) != 1 || !slices.Equal(receipt.Members[0].GapCodes, []string{
		"call_log_unavailable",
		"completed_feed_item_unavailable",
		"provider_attempt_unavailable",
		"request_trace_unavailable",
		unirouter.RuntimeCloseMarkerUnavailableGapCode,
	}) {
		t.Fatalf("matched receipt completeness=%+v", receipt)
	}
	receiptPayload, err := receipt.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	projection, err := ProjectCampaignTerminalProjection(authority, lifecycle, release)
	if err != nil {
		t.Fatal(err)
	}
	if projection.Completeness.CaptureGapCount != 5 || len(projection.Completeness.GapCodes) != 5 {
		t.Fatal("caller-side compatibility receipt tamper reached sealed terminal authority")
	}
	projectionPayload, err := projection.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyCampaignTerminalProjection(
		projectionPayload, authority, lifecycle, release,
	); err != nil {
		t.Fatal(err)
	}

	replayedReceipt, replayedAuthority, err := importer.ImportPreparedVerified(ctx, prepared)
	if err != nil {
		t.Fatal(err)
	}
	replayedPayload, err := replayedReceipt.CanonicalBytes()
	if err != nil || !bytes.Equal(receiptPayload, replayedPayload) || persister.calls != 1 {
		t.Fatalf("durable replay drifted: persistence=%d error=%v", persister.calls, err)
	}
	replayedProjection, err := ProjectCampaignTerminalProjection(
		replayedAuthority, lifecycle, release,
	)
	if err != nil {
		t.Fatal(err)
	}
	replayedProjectionPayload, err := replayedProjection.CanonicalBytes()
	if err != nil || !bytes.Equal(projectionPayload, replayedProjectionPayload) {
		t.Fatalf("terminal replay drifted: error=%v", err)
	}
	return receiptPayload, projectionPayload
}

func assertMatchedCampaignFixture(t *testing.T, name string, canonical []byte) {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(
		"..", "..", "integrationcontrol", "testdata", "n2-terminal-projection", name,
	))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSuffix(payload, []byte("\n")), canonical) {
		t.Fatalf("matched fixture %s does not equal the sealed durable import output", name)
	}
}

func assertStructFields(t *testing.T, value reflect.Type, expected []string) {
	t.Helper()
	actual := make([]string, value.NumField())
	for index := 0; index < value.NumField(); index++ {
		actual[index] = value.Field(index).Name
	}
	if !slices.Equal(actual, expected) {
		t.Fatalf("%s fields=%v want=%v", value.Name(), actual, expected)
	}
}

func prepareFixtureImport(
	packageBytes []byte,
	request ImportRequest,
	binding ComponentBinding,
	adapter unirouter.CampaignPackageContractAdapter,
) (PreparedImport, error) {
	verified, err := unirouter.VerifyCampaignPackage(packageBytes, request.Verification, adapter)
	if err != nil {
		return PreparedImport{}, err
	}
	return prepareVerifiedImport(request, binding, evidenceSchemaPath(), verified)
}

func campaignImportFixture(t *testing.T) (
	[]byte,
	ImportRequest,
	ComponentBinding,
	*fixtureContractAdapter,
) {
	t.Helper()
	memberBytes := partialV2Member(t, "ecs.second-integration.v2")
	packageBytes := []byte("immutable-signed-campaign-package-fixture")
	contract := unirouter.CampaignPackageContractIdentity{
		ID:     "unirouter.poc-source-package.v1",
		SHA256: digestRef("contract"),
	}
	trust := unirouter.SourceEvidenceTrustPolicy{
		Purpose:           unirouter.SourceEvidencePurpose,
		Domain:            unirouter.SourceEvidenceDomain,
		Algorithm:         unirouter.SourceEvidenceSignatureEd25519,
		SigningKeyRef:     "source-evidence-fixture-01",
		SignerPublicKeyID: "ed25519:" + digestRef("key"),
		TrustSetSHA256:    digestRef("trust-set"),
	}
	projection := unirouter.CampaignPackageProjection{
		Contract:                     contract,
		PackageID:                    "second-integration-poc-source-001",
		PackageSHA256:                sha256Ref(packageBytes),
		PackageSizeBytes:             int64(len(packageBytes)),
		ManifestSHA256:               digestRef("manifest"),
		PackageCreatedAt:             "2030-01-01T00:01:00Z",
		PackageScopeComplete:         true,
		SourceInstance:               "ecs.second-integration.v2",
		SourceBindingSHA256:          digestRef("source-binding"),
		AuthorizationManifestSHA256:  digestRef("authorization-manifest"),
		AuthorizationSHA256:          digestRef("authorization"),
		AuthorizationSignatureSHA256: digestRef("authorization-signature"),
		Signature: unirouter.SourceEvidenceSignatureIdentity{
			Purpose:           trust.Purpose,
			Domain:            trust.Domain,
			Algorithm:         trust.Algorithm,
			SigningKeyRef:     trust.SigningKeyRef,
			SignerPublicKeyID: trust.SignerPublicKeyID,
			TrustSetSHA256:    trust.TrustSetSHA256,
		},
		Members: []unirouter.CampaignPackageMemberInventory{{
			MemberID:  "request-001",
			Path:      "members/request-001.execution-evidence.v2.json",
			SHA256:    sha256Ref(memberBytes),
			SizeBytes: int64(len(memberBytes)),
		}},
	}
	adapter := &fixtureContractAdapter{
		identity: contract,
		opened: unirouter.OpenedCampaignPackage{
			Projection: projection,
			MemberPayloads: map[string][]byte{
				"request-001": memberBytes,
			},
		},
	}
	request := ImportRequest{
		OperationID: "campaign-import-operation-001",
		Verification: unirouter.CampaignPackageVerificationRequest{
			ExpectedContract:                     contract,
			ExpectedPackageID:                    projection.PackageID,
			ExpectedPackageSHA256:                projection.PackageSHA256,
			ExpectedSourceBindingSHA256:          projection.SourceBindingSHA256,
			ExpectedAuthorizationManifestSHA256:  projection.AuthorizationManifestSHA256,
			ExpectedAuthorizationSHA256:          projection.AuthorizationSHA256,
			ExpectedAuthorizationSignatureSHA256: projection.AuthorizationSignatureSHA256,
			Trust:                                trust,
		},
	}
	return packageBytes, request, ComponentBinding{
		ConnectorID:       "foigent.unirouter.poc-source-evidence",
		SourceSystemID:    unirouter.DefaultSourceSystemID,
		AdapterManifestID: unirouter.DefaultExecutionEvidenceAdapterID,
	}, adapter
}

func partialV2Member(t *testing.T, sourceInstance string) []byte {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(
		"..",
		"..",
		"..",
		"fixtures",
		"unirouter_execution_evidence",
		"recovered_fallback.v2.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(payload, &document); err != nil {
		t.Fatal(err)
	}
	document["source_instance"] = sourceInstance
	for _, collection := range []string{"trace_events", "call_logs"} {
		for _, rawFact := range document[collection].([]any) {
			fact := rawFact.(map[string]any)
			delete(fact, "actor_ref")
			delete(fact, "on_behalf_of")
			delete(fact, "channel")
		}
	}
	completeness := document["completeness"].(map[string]any)
	completeness["state"] = "partial"
	completeness["source_profile_complete"] = false
	for index, rawCoverage := range document["stage_coverage"].([]any) {
		coverage := rawCoverage.(map[string]any)
		if index%2 == 0 {
			coverage["status"] = "gap"
		} else {
			coverage["status"] = "unknown"
		}
		coverage["source_event_hashes"] = []any{}
	}
	document["capture_gaps"] = []any{map[string]any{
		"code":   unirouter.RuntimeCloseMarkerUnavailableGapCode,
		"stage":  "source",
		"reason": "the runtime cannot attest the terminal close marker",
	}}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func evidenceSchemaPath() string {
	return filepath.Join("..", "..", "..", "schemas", "m0", "evidence-event.schema.json")
}

func digestRef(value string) string {
	return sha256Ref([]byte(value))
}

func sha256Ref(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}
