package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/foigent/x-insight/internal/releasebundle"
	"github.com/klauspost/compress/zstd"
)

func runMaterializeDelta(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("materialize-delta", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options releasebundle.DeltaOptions
	flags.StringVar(&options.PlanPath, "plan", "", "reviewed delta inventory")
	flags.StringVar(&options.PlanSHA256, "plan-sha256", "", "sha256: digest from the independently reviewed authority")
	flags.StringVar(&options.BasisPath, "basis", "", "exact cached signed basis bundle")
	flags.StringVar(&options.PatchPath, "patch", "", "standard Zstd raw-tar patch")
	flags.StringVar(&options.ManifestPath, "manifest", "", "exact signed target manifest bytes")
	flags.StringVar(&options.SignaturePath, "signature", "", "exact detached target signature record")
	flags.StringVar(&options.TrustStorePath, "trust-store", "", "existing trusted release public keys")
	flags.StringVar(&options.OutputPath, "output", "", "new full bundle path in private staging")
	check := flags.Bool("check", false, "verify the complete reconstruction without writing a bundle")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || options.PlanPath == "" || options.PlanSHA256 == "" || options.BasisPath == "" ||
		options.PatchPath == "" || options.ManifestPath == "" || options.SignaturePath == "" || options.TrustStorePath == "" ||
		(*check == (options.OutputPath != "")) {
		return fmt.Errorf("usage: xinsight-release materialize-delta --plan FILE --plan-sha256 SHA256_REFERENCE --basis FILE --patch FILE --manifest FILE --signature FILE --trust-store FILE (--check | --output ABSOLUTE_PATH)")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := releasebundle.MaterializeDelta(ctx, options, openZstdDelta)
	if err != nil {
		return err
	}
	return writeOutput(stdout, map[string]any{
		"schema_version": "xinsight.release-delta-result.v1", "checked_only": *check,
		"release_name": result.ReleaseName, "bundle_sha256": result.BundleSHA256,
		"manifest_sha256": result.ManifestSHA256, "payload_sha256": result.Manifest.ArtifactDigest,
		"commit": result.Manifest.Commit, "product_version": result.Manifest.ProductVersion,
	})
}

func openZstdDelta(basis, patch []byte, rawBytes int64) (io.ReadCloser, error) {
	var header zstd.Header
	if err := header.Decode(patch); err != nil {
		return nil, err
	}
	window := header.WindowSize
	if header.SingleSegment {
		window = header.FrameContentSize
	}
	if header.Skippable || header.DictionaryID != 0 || !header.HasFCS || !header.HasCheckSum ||
		header.FrameContentSize != uint64(rawBytes) || window > releasebundle.MaxDeltaWindow {
		return nil, fmt.Errorf("Zstd frame does not match the reviewed raw-tar size, checksum, dictionary or window profile")
	}
	decoder, err := zstd.NewReader(bytes.NewReader(patch), zstd.WithDecoderDictRaw(0, basis),
		zstd.WithDecoderConcurrency(1), zstd.WithDecoderLowmem(true), zstd.WithDecoderMaxWindow(releasebundle.MaxDeltaWindow),
		zstd.WithDecoderMaxMemory(releasebundle.MaxDeltaWindow), zstd.WithDecodeBuffersBelow(0))
	if err != nil {
		return nil, err
	}
	return decoder.IOReadCloser(), nil
}
