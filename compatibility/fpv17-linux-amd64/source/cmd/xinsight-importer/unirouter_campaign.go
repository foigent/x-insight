package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"syscall"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/boundedoffline"
	"github.com/foigent/x-insight/internal/integration/uniroutercampaign"
	"github.com/foigent/x-insight/internal/integrationcontrol"
	"github.com/foigent/x-insight/internal/releasebundle"
)

const (
	campaignPackageConnectorID      = "foigent.unirouter.poc-source-evidence"
	maxCampaignControlArtifactBytes = int64(4 << 20)
	maxCampaignImporterBytes        = int64(128 << 20)
)

var campaignOperationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type campaignPackageCommandInput struct {
	OperationID                  string
	PackagePath                  string
	N1ReleaseManifestPath        string
	N1VerifierPath               string
	CampaignManifestPath         string
	UniRouterAcknowledgementPath string
	XInsightAcknowledgementPath  string
	AuthorizationPath            string
	AuthorizationSignaturePath   string
	AuthorizationTrustSetPath    string
	SourceEvidenceTrustSetPath   string
	ComponentScopeReceiptPath    string
	ComponentVerificationPath    string
	VerifierReleasePath          string
	ProductReleasePath           string
	ReleaseArtifactPath          string
	ReleaseTrustStorePath        string
	EvidenceSchemaPath           string
}

type campaignPackageCommandDependencies struct {
	prepare func(context.Context, campaignPackageCommandInput) (
		uniroutercampaign.PreparedImport,
		error,
	)
	loadImporterDSN func() (string, error)
	persist         func(context.Context, string, uniroutercampaign.PreparedImport) ([]byte, error)
}

func runUniRouterImportCampaignPackage(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runUniRouterImportCampaignPackageContext(
		ctx,
		args,
		os.Stdout,
		defaultCampaignPackageCommandDependencies(),
	)
}

func runUniRouterImportCampaignPackageContext(
	ctx context.Context,
	args []string,
	output io.Writer,
	dependencies campaignPackageCommandDependencies,
) error {
	input, err := parseCampaignPackageCommandInput(args)
	if err != nil {
		return err
	}
	if dependencies.prepare == nil || dependencies.loadImporterDSN == nil || dependencies.persist == nil {
		return errors.New("UniRouter campaign package command dependencies are unavailable")
	}
	prepared, err := dependencies.prepare(ctx, input)
	if err != nil {
		return fmt.Errorf("prepare authorized UniRouter campaign package offline: %w", err)
	}
	importerDSN, err := dependencies.loadImporterDSN()
	if err != nil {
		return err
	}
	canonicalReceipt, err := dependencies.persist(ctx, importerDSN, prepared)
	if err != nil {
		return err
	}
	if len(canonicalReceipt) == 0 {
		return errors.New("UniRouter campaign package import receipt is empty")
	}
	if output == nil {
		output = io.Discard
	}
	if _, err := output.Write(append(bytes.Clone(canonicalReceipt), '\n')); err != nil {
		return fmt.Errorf("write UniRouter campaign package import receipt: %w", err)
	}
	return nil
}

func parseCampaignPackageCommandInput(args []string) (campaignPackageCommandInput, error) {
	fs := flag.NewFlagSet("unirouter import-campaign-package", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var input campaignPackageCommandInput
	fs.StringVar(&input.OperationID, "operation-id", "", "stable operator-generated import operation ID")
	fs.StringVar(&input.PackagePath, "package", "", "absolute signed campaign package path")
	fs.StringVar(&input.N1ReleaseManifestPath, "n1-release-manifest", "", "absolute immutable N1 release manifest path")
	fs.StringVar(&input.N1VerifierPath, "n1-verifier", "", "absolute immutable N1 verifier path")
	fs.StringVar(&input.CampaignManifestPath, "campaign-manifest", "", "absolute neutral campaign manifest path")
	fs.StringVar(&input.UniRouterAcknowledgementPath, "unirouter-ack", "", "absolute UniRouter acknowledgement path")
	fs.StringVar(&input.XInsightAcknowledgementPath, "xinsight-ack", "", "absolute X-Insight acknowledgement path")
	fs.StringVar(&input.AuthorizationPath, "authorization", "", "absolute campaign authorization path")
	fs.StringVar(&input.AuthorizationSignaturePath, "authorization-signature", "", "absolute detached campaign authorization signature path")
	fs.StringVar(&input.AuthorizationTrustSetPath, "authorization-trust-set", "", "absolute campaign authorization trust-set path")
	fs.StringVar(&input.SourceEvidenceTrustSetPath, "source-evidence-trust-set", "", "absolute Source-evidence trust-set path")
	fs.StringVar(&input.ComponentScopeReceiptPath, "component-scope-receipt", "", "absolute X-Insight component scope receipt path")
	fs.StringVar(&input.ComponentVerificationPath, "component-verification-receipt", "", "absolute X-Insight component verification receipt path")
	fs.StringVar(&input.VerifierReleasePath, "verifier-release", "", "absolute X-Insight verifier release identity path")
	fs.StringVar(&input.ProductReleasePath, "product-release", "", "absolute X-Insight product release identity path")
	fs.StringVar(&input.ReleaseArtifactPath, "release-artifact", "", "absolute verified X-Insight signed release path")
	fs.StringVar(&input.ReleaseTrustStorePath, "release-trust-store", "", "absolute X-Insight release trust store path")
	fs.StringVar(&input.EvidenceSchemaPath, "evidence-schema", "", "absolute M0 Evidence schema path")
	if err := fs.Parse(args); err != nil {
		return campaignPackageCommandInput{}, err
	}
	if fs.NArg() != 0 {
		return campaignPackageCommandInput{}, errors.New("unirouter import-campaign-package does not accept positional arguments")
	}
	if !campaignOperationIDPattern.MatchString(input.OperationID) {
		return campaignPackageCommandInput{}, errors.New("--operation-id is required and must be a stable portable identity")
	}
	paths := []struct {
		flag string
		path string
	}{
		{"--package", input.PackagePath},
		{"--n1-release-manifest", input.N1ReleaseManifestPath},
		{"--n1-verifier", input.N1VerifierPath},
		{"--campaign-manifest", input.CampaignManifestPath},
		{"--unirouter-ack", input.UniRouterAcknowledgementPath},
		{"--xinsight-ack", input.XInsightAcknowledgementPath},
		{"--authorization", input.AuthorizationPath},
		{"--authorization-signature", input.AuthorizationSignaturePath},
		{"--authorization-trust-set", input.AuthorizationTrustSetPath},
		{"--source-evidence-trust-set", input.SourceEvidenceTrustSetPath},
		{"--component-scope-receipt", input.ComponentScopeReceiptPath},
		{"--component-verification-receipt", input.ComponentVerificationPath},
		{"--verifier-release", input.VerifierReleasePath},
		{"--product-release", input.ProductReleasePath},
		{"--release-artifact", input.ReleaseArtifactPath},
		{"--release-trust-store", input.ReleaseTrustStorePath},
		{"--evidence-schema", input.EvidenceSchemaPath},
	}
	for _, candidate := range paths {
		if !filepath.IsAbs(candidate.path) || filepath.Clean(candidate.path) != candidate.path {
			return campaignPackageCommandInput{}, fmt.Errorf("%s is required and must be an absolute clean path", candidate.flag)
		}
	}
	return input, nil
}

func defaultCampaignPackageCommandDependencies() campaignPackageCommandDependencies {
	return campaignPackageCommandDependencies{
		prepare:         prepareUniRouterCampaignPackageImport,
		loadImporterDSN: config.LoadImporterPostgresDSN,
		persist:         persistUniRouterCampaignPackageImport,
	}
}

func prepareUniRouterCampaignPackageImport(
	ctx context.Context,
	input campaignPackageCommandInput,
) (uniroutercampaign.PreparedImport, error) {
	if err := verifyCampaignEvidenceSchema(input.EvidenceSchemaPath); err != nil {
		return uniroutercampaign.PreparedImport{}, err
	}
	expectedRelease, runtimeIdentity, err := verifyRunningCampaignReleaseAuthority(input)
	if err != nil {
		return uniroutercampaign.PreparedImport{}, err
	}
	packageBytes, err := readCampaignArtifact(
		"signed campaign package",
		input.PackagePath,
		unirouter.DefaultCampaignPackageMaxBytes,
	)
	if err != nil {
		return uniroutercampaign.PreparedImport{}, err
	}
	sourceTrustBytes, err := readCampaignArtifact(
		"Source-evidence trust set",
		input.SourceEvidenceTrustSetPath,
		maxCampaignControlArtifactBytes,
	)
	if err != nil {
		return uniroutercampaign.PreparedImport{}, err
	}
	scopeBytes, err := readCampaignArtifact(
		"X-Insight component scope receipt",
		input.ComponentScopeReceiptPath,
		maxCampaignControlArtifactBytes,
	)
	if err != nil {
		return uniroutercampaign.PreparedImport{}, err
	}
	verificationBytes, err := readCampaignArtifact(
		"X-Insight component verification receipt",
		input.ComponentVerificationPath,
		maxCampaignControlArtifactBytes,
	)
	if err != nil {
		return uniroutercampaign.PreparedImport{}, err
	}
	xInsightAckBytes, err := readCampaignArtifact(
		"X-Insight component acknowledgement",
		input.XInsightAcknowledgementPath,
		maxCampaignControlArtifactBytes,
	)
	if err != nil {
		return uniroutercampaign.PreparedImport{}, err
	}
	verifierReleaseBytes, err := readCampaignArtifact(
		"X-Insight verifier release",
		input.VerifierReleasePath,
		maxCampaignControlArtifactBytes,
	)
	if err != nil {
		return uniroutercampaign.PreparedImport{}, err
	}
	productReleaseBytes, err := readCampaignArtifact(
		"X-Insight product release identity",
		input.ProductReleasePath,
		maxCampaignControlArtifactBytes,
	)
	if err != nil {
		return uniroutercampaign.PreparedImport{}, err
	}
	verifier, err := integrationcontrol.NewN1Verifier(
		input.N1ReleaseManifestPath,
		input.N1VerifierPath,
	)
	if err != nil {
		return uniroutercampaign.PreparedImport{}, err
	}
	manifest, err := verifier.VerifyManifest(ctx, input.CampaignManifestPath)
	if err != nil {
		return uniroutercampaign.PreparedImport{}, err
	}
	if err := validateRealCampaignProductLifecycle(
		manifest,
		scopeBytes,
		verificationBytes,
		xInsightAckBytes,
		verifierReleaseBytes,
		productReleaseBytes,
		expectedRelease,
		runtimeIdentity,
	); err != nil {
		return uniroutercampaign.PreparedImport{}, err
	}
	authorization, err := verifier.VerifyAuthorization(ctx, integrationcontrol.N1AuthorizationFiles{
		Manifest:                 input.CampaignManifestPath,
		UniRouterAcknowledgement: input.UniRouterAcknowledgementPath,
		XInsightAcknowledgement:  input.XInsightAcknowledgementPath,
		Authorization:            input.AuthorizationPath,
		Signature:                input.AuthorizationSignaturePath,
		AuthorizationTrustSet:    input.AuthorizationTrustSetPath,
		SourceEvidenceTrustSet:   input.SourceEvidenceTrustSetPath,
	})
	if err != nil {
		return uniroutercampaign.PreparedImport{}, err
	}
	if err := authorization.ValidateAgainstManifest(manifest); err != nil {
		return uniroutercampaign.PreparedImport{}, err
	}
	if authorization.AcknowledgementSHA256[integrationcontrol.ProductXInsight] != campaignArtifactSHA256(xInsightAckBytes) ||
		authorization.SourceEvidenceTrustSetSHA256 != campaignArtifactSHA256(sourceTrustBytes) {
		return uniroutercampaign.PreparedImport{}, errors.New(
			"sealed N1 authorization does not bind the exact product acknowledgement or Source trust input",
		)
	}

	adapter, err := unirouter.NewUniRouterSourceEvidenceV1Adapter(sourceTrustBytes)
	if err != nil {
		return uniroutercampaign.PreparedImport{}, err
	}
	packageSHA256 := campaignArtifactSHA256(packageBytes)
	request := uniroutercampaign.ImportRequest{
		OperationID: input.OperationID,
		Verification: unirouter.CampaignPackageVerificationRequest{
			ExpectedContract: unirouter.CampaignPackageContractIdentity{
				ID:     unirouter.UniRouterSourceEvidenceV1ContractID,
				SHA256: unirouter.UniRouterSourceEvidenceV1ContractSHA256,
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
	return uniroutercampaign.PrepareImport(
		packageBytes,
		request,
		uniroutercampaign.ComponentBinding{
			ConnectorID:       campaignPackageConnectorID,
			SourceSystemID:    unirouter.DefaultSourceSystemID,
			AdapterManifestID: unirouter.DefaultExecutionEvidenceAdapterID,
		},
		input.EvidenceSchemaPath,
		adapter,
		authorization,
	)
}

type campaignRuntimeIdentity struct {
	ProductVersion              string
	Commit                      string
	ManualVersion               string
	DatabaseSchemaMin           string
	DatabaseSchemaMax           string
	ImporterExecutableSHA256    string
	ImporterExecutableSizeBytes int64
}

func verifyRunningCampaignReleaseAuthority(
	input campaignPackageCommandInput,
) (integrationcontrol.ReleaseAuthority, campaignRuntimeIdentity, error) {
	product := buildinfo.Current()
	if product.Commit == "" || product.Commit == "unknown" || product.ProductVersion == "" {
		return integrationcontrol.ReleaseAuthority{}, campaignRuntimeIdentity{},
			errors.New("real campaign import requires an exact running X-Insight release build")
	}
	verified, err := releasebundle.Verify(input.ReleaseArtifactPath, releasebundle.VerifyOptions{
		TrustStorePath:  input.ReleaseTrustStorePath,
		ExpectedCommit:  product.Commit,
		ExpectedVersion: product.ProductVersion,
		ExpectedOS:      "linux",
		ExpectedArch:    "amd64",
	})
	if err != nil {
		return integrationcontrol.ReleaseAuthority{}, campaignRuntimeIdentity{}, err
	}
	authority, err := integrationcontrol.NewReleaseAuthorityFromVerifiedBundle(verified)
	if err != nil {
		return integrationcontrol.ReleaseAuthority{}, campaignRuntimeIdentity{}, err
	}
	importerSHA256, importerSize, err := currentImporterExecutableIdentity()
	if err != nil {
		return integrationcontrol.ReleaseAuthority{}, campaignRuntimeIdentity{}, err
	}
	return authority, campaignRuntimeIdentity{
		ProductVersion:              product.ProductVersion,
		Commit:                      product.Commit,
		ManualVersion:               product.ManualVersion,
		DatabaseSchemaMin:           product.DatabaseSchemaMin,
		DatabaseSchemaMax:           product.DatabaseSchemaMax,
		ImporterExecutableSHA256:    importerSHA256,
		ImporterExecutableSizeBytes: importerSize,
	}, nil
}

func verifyCampaignEvidenceSchema(path string) error {
	payload, err := readCampaignArtifact(
		"M0 Evidence schema",
		path,
		maxCampaignControlArtifactBytes,
	)
	if err != nil {
		return err
	}
	if campaignArtifactSHA256(payload) != integrationcontrol.M0EvidenceSchemaSHA256 {
		return errors.New("M0 Evidence schema does not match the compiled X-Insight product pin")
	}
	return nil
}

func validateRealCampaignProductLifecycle(
	manifest integrationcontrol.N1ManifestVerification,
	scopeBytes []byte,
	verificationBytes []byte,
	xInsightAckBytes []byte,
	verifierReleaseBytes []byte,
	productReleaseBytes []byte,
	expectedRelease integrationcontrol.ReleaseAuthority,
	runtimeIdentity campaignRuntimeIdentity,
) error {
	scope, err := integrationcontrol.ParseComponentScopeReceipt(scopeBytes)
	if err != nil {
		return err
	}
	if scope.ProductRelease.SchemaVersion != integrationcontrol.ProductReleaseIdentityVersion {
		return errors.New("real campaign import requires an authoritative X-Insight product release identity")
	}
	if err := expectedRelease.Validate(); err != nil {
		return err
	}
	if scope.ProductRelease.SHA256 != campaignArtifactSHA256(productReleaseBytes) {
		return errors.New("X-Insight product release identity does not match the component scope receipt")
	}
	productRelease, err := integrationcontrol.ParseProductReleaseIdentity(productReleaseBytes)
	if err != nil {
		return err
	}
	contractSHA256, err := integrationcontrol.NewComponentContract().SHA256()
	if err != nil {
		return err
	}
	if scope.ComponentContract.SHA256 != contractSHA256 ||
		productRelease.ComponentContractSHA256 != contractSHA256 {
		return errors.New("X-Insight component scope does not bind the compiled consumer contract")
	}
	verifierRelease, err := integrationcontrol.ParseVerifierRelease(verifierReleaseBytes)
	if err != nil {
		return err
	}
	verifierReleaseSHA256, err := verifierRelease.SHA256()
	if err != nil {
		return err
	}
	if scope.VerifierRelease.SHA256 != verifierReleaseSHA256 {
		return errors.New("X-Insight verifier release does not match the component scope or compiled contract")
	}
	actualRelease := integrationcontrol.ReleaseAuthority{
		ProductRelease: productRelease,
		Verifier:       verifierRelease,
	}
	if err := actualRelease.Validate(); err != nil {
		return err
	}
	expectedProductBytes, _ := expectedRelease.ProductRelease.CanonicalBytes()
	expectedVerifierBytes, _ := expectedRelease.Verifier.CanonicalBytes()
	if !bytes.Equal(productReleaseBytes, expectedProductBytes) ||
		!bytes.Equal(verifierReleaseBytes, expectedVerifierBytes) {
		return errors.New("X-Insight product authority does not match the verified signed release manifest")
	}
	if productRelease.Commit != runtimeIdentity.Commit ||
		productRelease.ProductVersion != runtimeIdentity.ProductVersion ||
		productRelease.ManualVersion != runtimeIdentity.ManualVersion ||
		productRelease.DatabaseSchema != runtimeIdentity.DatabaseSchemaMin ||
		productRelease.DatabaseSchema != runtimeIdentity.DatabaseSchemaMax {
		return errors.New("X-Insight verifier release does not match the running importer build identity")
	}
	if productRelease.ImporterExecutableSHA256 != runtimeIdentity.ImporterExecutableSHA256 ||
		productRelease.ImporterExecutableSizeBytes != runtimeIdentity.ImporterExecutableSizeBytes {
		return errors.New("X-Insight product release identity does not match the running importer executable")
	}
	verification, err := integrationcontrol.ParseComponentVerificationReceipt(verificationBytes)
	if err != nil {
		return err
	}
	lifecycle, err := integrationcontrol.VerifyComponentLifecycle(scope, manifest, verification)
	if err != nil {
		return err
	}
	providedAck, err := integrationcontrol.ParseComponentAcknowledgement(xInsightAckBytes)
	if err != nil {
		return err
	}
	if err := providedAck.ValidateAgainstVerification(verification); err != nil {
		return err
	}
	expectedAck, err := integrationcontrol.NewComponentAcknowledgement(lifecycle)
	if err != nil {
		return err
	}
	expectedAckBytes, err := expectedAck.CanonicalBytes()
	if err != nil {
		return err
	}
	if !bytes.Equal(expectedAckBytes, xInsightAckBytes) {
		return errors.New("X-Insight acknowledgement is not the canonical sealed product lifecycle result")
	}
	return nil
}

func currentImporterExecutableIdentity() (string, int64, error) {
	path := "/proc/self/exe"
	if runtime.GOOS != "linux" {
		var err error
		path, err = os.Executable()
		if err != nil {
			return "", 0, errors.New("resolve running importer executable")
		}
	}
	file, err := os.Open(path)
	if err != nil {
		return "", 0, errors.New("open running importer executable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxCampaignImporterBytes {
		return "", 0, errors.New("running importer executable identity is invalid")
	}
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(file, maxCampaignImporterBytes+1))
	if err != nil || written != info.Size() || written > maxCampaignImporterBytes {
		return "", 0, errors.New("hash exact running importer executable")
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil)), info.Size(), nil
}

func persistUniRouterCampaignPackageImport(
	ctx context.Context,
	importerDSN string,
	prepared uniroutercampaign.PreparedImport,
) ([]byte, error) {
	database, err := sql.Open("pgx", importerDSN)
	if err != nil {
		return nil, errors.New("open UniRouter campaign importer database")
	}
	defer database.Close()
	database.SetMaxOpenConns(2)
	database.SetMaxIdleConns(1)
	database.SetConnMaxLifetime(30 * time.Minute)
	database.SetConnMaxIdleTime(5 * time.Minute)
	pingContext, cancelPing := context.WithTimeout(ctx, 10*time.Second)
	defer cancelPing()
	if err := database.PingContext(pingContext); err != nil {
		return nil, errors.New("connect UniRouter campaign importer database")
	}
	operations, err := boundedoffline.NewPostgresOperationStore(database)
	if err != nil {
		return nil, err
	}
	importer, err := uniroutercampaign.NewImporter(db.NewEvidenceStore(database), operations, nil)
	if err != nil {
		return nil, err
	}
	receipt, err := importer.ImportPrepared(ctx, prepared)
	if err != nil {
		return nil, err
	}
	canonical, err := receipt.CanonicalBytes()
	if err != nil {
		return nil, fmt.Errorf("encode UniRouter campaign package import receipt: %w", err)
	}
	return canonical, nil
}

func readCampaignArtifact(label string, path string, maximum int64) ([]byte, error) {
	if maximum < 1 || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("%s path is invalid", label)
	}
	pathInfo, err := os.Lstat(path)
	if err != nil || !pathInfo.Mode().IsRegular() || pathInfo.Size() < 1 || pathInfo.Size() > maximum {
		return nil, fmt.Errorf("%s must be a bounded regular file", label)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s", label)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(pathInfo, openedInfo) {
		return nil, fmt.Errorf("%s changed while opening", label)
	}
	payload, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(payload)) != openedInfo.Size() || int64(len(payload)) > maximum {
		return nil, fmt.Errorf("read exact %s", label)
	}
	return payload, nil
}

func campaignArtifactSHA256(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}
