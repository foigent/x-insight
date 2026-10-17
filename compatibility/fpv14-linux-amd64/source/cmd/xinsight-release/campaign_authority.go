package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/foigent/x-insight/internal/integrationcontrol"
	"github.com/foigent/x-insight/internal/releasebundle"
)

const (
	productReleaseIdentityFilename = "xinsight.release-identity.v1.json"
	verifierReleaseFilename        = "xinsight.signed-campaign-package-consumer-verifier.v1.json"
)

type campaignReleaseAuthorityDeriver func(
	artifactPath string,
	trustStorePath string,
	expectedCommit string,
	expectedVersion string,
) (integrationcontrol.ReleaseAuthority, error)

func runCampaignAuthority(args []string, stdout io.Writer) error {
	return runCampaignAuthorityWithDeriver(args, stdout, deriveCampaignReleaseAuthority)
}

func runCampaignAuthorityWithDeriver(
	args []string,
	stdout io.Writer,
	derive campaignReleaseAuthorityDeriver,
) error {
	if derive == nil {
		return errors.New("campaign release authority verifier is unavailable")
	}
	if len(args) == 0 {
		return campaignAuthorityUsage()
	}
	switch args[0] {
	case "derive":
		return runCampaignAuthorityDerive(args[1:], stdout, derive)
	case "verify":
		return runCampaignAuthorityVerify(args[1:], stdout, derive)
	default:
		return campaignAuthorityUsage()
	}
}

func runCampaignAuthorityDerive(
	args []string,
	stdout io.Writer,
	derive campaignReleaseAuthorityDeriver,
) error {
	flags := flag.NewFlagSet("campaign-authority derive", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	artifact := flags.String("artifact", "", "exact signed X-Insight release bundle")
	trustStore := flags.String("trust-store", "", "exact X-Insight release trust store")
	commit := flags.String("expected-commit", "", "exact expected release commit")
	version := flags.String("expected-version", "", "exact expected product version")
	outputDirectory := flags.String("output-directory", "", "exclusive-create absolute output directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || !absoluteCleanPath(*artifact) || !absoluteCleanPath(*trustStore) ||
		!absoluteCleanPath(*outputDirectory) {
		return campaignAuthorityUsage()
	}
	authority, err := derive(*artifact, *trustStore, *commit, *version)
	if err != nil {
		return err
	}
	productBytes, err := authority.ProductRelease.CanonicalBytes()
	if err != nil {
		return err
	}
	verifierBytes, err := authority.Verifier.CanonicalBytes()
	if err != nil {
		return err
	}
	if err := os.Mkdir(*outputDirectory, 0o755); err != nil {
		return fmt.Errorf("create campaign release authority directory: %w", err)
	}
	productPath := filepath.Join(*outputDirectory, productReleaseIdentityFilename)
	verifierPath := filepath.Join(*outputDirectory, verifierReleaseFilename)
	if err := writeExclusiveAuthority(productPath, productBytes); err != nil {
		return err
	}
	if err := writeExclusiveAuthority(verifierPath, verifierBytes); err != nil {
		return err
	}
	productSHA256, _ := authority.ProductRelease.SHA256()
	verifierSHA256, _ := authority.Verifier.SHA256()
	return writeOutput(stdout, map[string]any{
		"schema_version":                     "xinsight.campaign-release-authority-derive-result.v1",
		"product_release":                    productPath,
		"product_release_sha256":             productSHA256,
		"verifier_release":                   verifierPath,
		"verifier_release_sha256":            verifierSHA256,
		"signed_release_manifest_sha256":     authority.ProductRelease.SignedReleaseManifestSHA256,
		"release_signing_key_id":             authority.ProductRelease.ReleaseSigningKeyID,
		"database_schema":                    authority.ProductRelease.DatabaseSchema,
		"manual_version":                     authority.ProductRelease.ManualVersion,
		"importer_sha256":                    authority.ProductRelease.ImporterExecutableSHA256,
		"m0_evidence_schema_sha256":          authority.ProductRelease.M0EvidenceSchemaSHA256,
		"neutral_n1_release_manifest_sha256": authority.ProductRelease.NeutralN1ReleaseManifestSHA256,
		"commit":                             authority.ProductRelease.Commit,
		"product_version":                    authority.ProductRelease.ProductVersion,
	})
}

func runCampaignAuthorityVerify(
	args []string,
	stdout io.Writer,
	derive campaignReleaseAuthorityDeriver,
) error {
	flags := flag.NewFlagSet("campaign-authority verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	artifact := flags.String("artifact", "", "exact signed X-Insight release bundle")
	trustStore := flags.String("trust-store", "", "exact X-Insight release trust store")
	productRelease := flags.String("product-release", "", "canonical product release identity")
	verifierRelease := flags.String("verifier-release", "", "canonical verifier release identity")
	commit := flags.String("expected-commit", "", "expected exact release commit")
	version := flags.String("expected-version", "", "expected exact product version")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || !absoluteCleanPath(*artifact) || !absoluteCleanPath(*trustStore) ||
		!absoluteCleanPath(*productRelease) || !absoluteCleanPath(*verifierRelease) {
		return campaignAuthorityUsage()
	}
	expected, err := derive(*artifact, *trustStore, *commit, *version)
	if err != nil {
		return err
	}
	productBytes, err := readAuthorityFile(*productRelease, 1<<20)
	if err != nil {
		return err
	}
	product, err := integrationcontrol.ParseProductReleaseIdentity(productBytes)
	if err != nil {
		return err
	}
	verifierBytes, err := readAuthorityFile(*verifierRelease, 1<<20)
	if err != nil {
		return err
	}
	verifier, err := integrationcontrol.ParseVerifierRelease(verifierBytes)
	if err != nil {
		return err
	}
	actual := integrationcontrol.ReleaseAuthority{ProductRelease: product, Verifier: verifier}
	if err := actual.Validate(); err != nil {
		return err
	}
	expectedProduct, _ := expected.ProductRelease.CanonicalBytes()
	expectedVerifier, _ := expected.Verifier.CanonicalBytes()
	if !bytes.Equal(productBytes, expectedProduct) || !bytes.Equal(verifierBytes, expectedVerifier) {
		return errors.New("campaign release authority does not match the verified signed release manifest")
	}
	productSHA256, _ := product.SHA256()
	verifierSHA256, _ := verifier.SHA256()
	return writeOutput(stdout, map[string]any{
		"schema_version":                 "xinsight.campaign-release-authority-verify-result.v1",
		"product_release_sha256":         productSHA256,
		"verifier_release_sha256":        verifierSHA256,
		"signed_release_manifest_sha256": product.SignedReleaseManifestSHA256,
		"importer_sha256":                product.ImporterExecutableSHA256,
		"database_schema":                product.DatabaseSchema,
		"manual_version":                 product.ManualVersion,
		"commit":                         product.Commit,
		"product_version":                product.ProductVersion,
		"valid":                          true,
	})
}

func deriveCampaignReleaseAuthority(
	artifactPath string,
	trustStorePath string,
	expectedCommit string,
	expectedVersion string,
) (integrationcontrol.ReleaseAuthority, error) {
	verified, err := releasebundle.Verify(artifactPath, releasebundle.VerifyOptions{
		TrustStorePath:  trustStorePath,
		ExpectedCommit:  expectedCommit,
		ExpectedVersion: expectedVersion,
		ExpectedOS:      "linux",
		ExpectedArch:    "amd64",
	})
	if err != nil {
		return integrationcontrol.ReleaseAuthority{}, err
	}
	return integrationcontrol.NewReleaseAuthorityFromVerifiedBundle(verified)
}

func readAuthorityFile(path string, maximum int64) ([]byte, error) {
	if maximum < 1 || !absoluteCleanPath(path) {
		return nil, errors.New("campaign release authority input path is invalid")
	}
	pathInfo, err := os.Lstat(path)
	if err != nil || !pathInfo.Mode().IsRegular() || pathInfo.Size() < 1 || pathInfo.Size() > maximum {
		return nil, errors.New("campaign release authority input must be a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("open campaign release authority input")
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(pathInfo, openedInfo) {
		return nil, errors.New("campaign release authority input changed while opening")
	}
	payload, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(payload)) != openedInfo.Size() || int64(len(payload)) > maximum {
		return nil, errors.New("read exact campaign release authority input")
	}
	return payload, nil
}

func writeExclusiveAuthority(path string, payload []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create campaign release authority: %w", err)
	}
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return fmt.Errorf("write campaign release authority: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync campaign release authority: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close campaign release authority: %w", err)
	}
	return nil
}

func absoluteCleanPath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path
}

func campaignAuthorityUsage() error {
	return errors.New("usage: xinsight-release campaign-authority derive|verify")
}
