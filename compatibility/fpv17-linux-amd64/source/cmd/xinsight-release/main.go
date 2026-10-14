package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/releasebundle"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "xinsight-release: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if handled, err := buildinfo.HandleArgs(args, stdout); handled {
		return err
	}
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "keygen":
		return runKeygen(args[1:], stdout)
	case "pack":
		return runPack(args[1:], stdout)
	case "verify":
		return runVerify(args[1:], stdout)
	case "extract":
		return runExtract(args[1:], stdout)
	case "campaign-authority":
		return runCampaignAuthority(args[1:], stdout)
	default:
		return usage()
	}
}

func runKeygen(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("keygen", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	privateKey := flags.String("private-key", "", "owner-only private key output path")
	trustStore := flags.String("trust-store", "", "public trust store output path")
	createdAt := flags.String("created-at", "", "optional RFC3339 key creation time")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *privateKey == "" || *trustStore == "" {
		return fmt.Errorf("usage: xinsight-release keygen --private-key PATH --trust-store PATH [--created-at RFC3339]")
	}
	now := time.Now().UTC()
	if *createdAt != "" {
		parsed, err := time.Parse(time.RFC3339, *createdAt)
		if err != nil {
			return fmt.Errorf("parse --created-at: %w", err)
		}
		now = parsed.UTC()
	}
	keyID, err := releasebundle.GenerateKeyPair(*privateKey, *trustStore, now)
	if err != nil {
		return err
	}
	return writeOutput(stdout, map[string]any{
		"schema_version": "xinsight.release-keygen-result.v1",
		"key_id":         keyID,
		"private_key":    *privateKey,
		"trust_store":    *trustStore,
	})
}

func runPack(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("pack", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	payload := flags.String("payload", "", "release payload directory")
	config := flags.String("config", "", "release configuration JSON")
	signingKey := flags.String("signing-key", "", "owner-only Ed25519 signing key")
	output := flags.String("output", "", "exclusive-create bundle output path")
	commit := flags.String("commit", "", "exact 40-character Git commit")
	sourceDateEpoch := flags.String("source-date-epoch", "", "deterministic Unix timestamp")
	goVersion := flags.String("go-version", "", "Go toolchain identity")
	nodeVersion := flags.String("node-version", "", "Node toolchain identity")
	npmVersion := flags.String("npm-version", "", "npm toolchain identity")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *payload == "" || *config == "" || *signingKey == "" || *output == "" || *commit == "" ||
		*sourceDateEpoch == "" || *goVersion == "" || *nodeVersion == "" || *npmVersion == "" {
		return fmt.Errorf("usage: xinsight-release pack --payload DIR --config FILE --signing-key FILE --output FILE --commit SHA --source-date-epoch UNIX --go-version VERSION --node-version VERSION --npm-version VERSION")
	}
	epoch, err := strconv.ParseInt(*sourceDateEpoch, 10, 64)
	if err != nil || epoch <= 0 {
		return fmt.Errorf("--source-date-epoch must be a positive Unix timestamp")
	}
	result, err := releasebundle.Pack(releasebundle.PackOptions{
		PayloadDir:      *payload,
		ConfigPath:      *config,
		SigningKeyPath:  *signingKey,
		OutputPath:      *output,
		Commit:          *commit,
		SourceDateEpoch: time.Unix(epoch, 0).UTC(),
		BuildToolchain: releasebundle.BuildToolchain{
			GoVersion:   *goVersion,
			NodeVersion: *nodeVersion,
			NPMVersion:  *npmVersion,
		},
	})
	if err != nil {
		return err
	}
	return writeOutput(stdout, map[string]any{
		"schema_version":  "xinsight.release-pack-result.v1",
		"release_name":    result.ReleaseName,
		"output":          result.OutputPath,
		"bundle_sha256":   result.BundleSHA256,
		"manifest_sha256": result.ManifestSHA256,
		"payload_sha256":  result.PayloadSHA256,
		"key_id":          result.KeyID,
		"product_version": result.Manifest.ProductVersion,
		"commit":          result.Manifest.Commit,
	})
}

func runVerify(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	artifact := flags.String("artifact", "", "release bundle path")
	trustStore := flags.String("trust-store", "", "trusted public key store")
	expectedCommit := flags.String("expected-commit", "", "optional exact commit")
	expectedVersion := flags.String("expected-version", "", "optional exact product version")
	expectedOS := flags.String("expected-os", "linux", "expected target OS")
	expectedArch := flags.String("expected-arch", "amd64", "expected target architecture")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *artifact == "" || *trustStore == "" {
		return fmt.Errorf("usage: xinsight-release verify --artifact FILE --trust-store FILE [--expected-commit SHA] [--expected-version VERSION]")
	}
	verified, err := releasebundle.Verify(*artifact, releasebundle.VerifyOptions{
		TrustStorePath:  *trustStore,
		ExpectedCommit:  *expectedCommit,
		ExpectedVersion: *expectedVersion,
		ExpectedOS:      *expectedOS,
		ExpectedArch:    *expectedArch,
	})
	if err != nil {
		return err
	}
	return writeOutput(stdout, map[string]any{
		"schema_version":  "xinsight.release-verify-result.v1",
		"release_name":    verified.ReleaseName,
		"bundle_sha256":   verified.BundleSHA256,
		"manifest_sha256": verified.ManifestSHA256,
		"payload_sha256":  verified.Manifest.ArtifactDigest,
		"key_id":          verified.Manifest.Signature.KeyID,
		"product_version": verified.Manifest.ProductVersion,
		"commit":          verified.Manifest.Commit,
		"target_os":       verified.Manifest.TargetOS,
		"target_arch":     verified.Manifest.TargetArch,
	})
}

func runExtract(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("extract", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	artifact := flags.String("artifact", "", "release bundle path")
	trustStore := flags.String("trust-store", "", "trusted public key store")
	destination := flags.String("destination", "", "exclusive-create absolute payload destination")
	expectedCommit := flags.String("expected-commit", "", "optional exact commit")
	expectedVersion := flags.String("expected-version", "", "optional exact product version")
	expectedOS := flags.String("expected-os", "linux", "expected target OS")
	expectedArch := flags.String("expected-arch", "amd64", "expected target architecture")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *artifact == "" || *trustStore == "" || *destination == "" {
		return fmt.Errorf("usage: xinsight-release extract --artifact FILE --trust-store FILE --destination ABSOLUTE_PATH [--expected-commit SHA] [--expected-version VERSION]")
	}
	verified, err := releasebundle.Verify(*artifact, releasebundle.VerifyOptions{
		TrustStorePath:  *trustStore,
		ExpectedCommit:  *expectedCommit,
		ExpectedVersion: *expectedVersion,
		ExpectedOS:      *expectedOS,
		ExpectedArch:    *expectedArch,
	})
	if err != nil {
		return err
	}
	if err := releasebundle.ExtractPayload(verified, *destination); err != nil {
		return err
	}
	if err := releasebundle.VerifyExtractedPayload(verified, *destination); err != nil {
		return err
	}
	return writeOutput(stdout, map[string]any{
		"schema_version":  "xinsight.release-extract-result.v1",
		"release_name":    verified.ReleaseName,
		"destination":     *destination,
		"bundle_sha256":   verified.BundleSHA256,
		"manifest_sha256": verified.ManifestSHA256,
		"product_version": verified.Manifest.ProductVersion,
		"commit":          verified.Manifest.Commit,
		"manual_version":  verified.Manifest.Manual.Version,
	})
}

func writeOutput(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func usage() error {
	return fmt.Errorf("usage: xinsight-release keygen|pack|verify|extract|campaign-authority")
}
