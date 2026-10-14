package releasebundle

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"testing"
	"time"
)

func TestReadEnvelopeDrainsGzipTrailerAfterLargePayload(t *testing.T) {
	// Match the size class of the first repository bundle that exposed the
	// gzip-footer buffering boundary in exact-toolchain CI.
	payload := make([]byte, 26_911_206)
	state := uint64(0x9e3779b97f4a7c15)
	for index := range payload {
		state ^= state << 7
		state ^= state >> 9
		state ^= state << 8
		payload[index] = byte(state)
	}

	bundle, err := buildEnvelope(
		"xinsight-v0.2.0-rc.1-linux-amd64-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
		bytes.Repeat([]byte("m"), 20_250),
		bytes.Repeat([]byte("s"), 387),
		payload,
	)
	if err != nil {
		t.Fatalf("build envelope: %v", err)
	}

	_, files, _, err := readEnvelope(bundle)
	if err != nil {
		t.Fatalf("read envelope: %v", err)
	}
	if len(files[PayloadArtifactPath]) != len(payload) {
		t.Fatalf("payload bytes=%d, want %d", len(files[PayloadArtifactPath]), len(payload))
	}

	withTrailingBytes := append(append([]byte(nil), bundle...), []byte("forbidden-tail")...)
	if _, _, _, err := readEnvelope(withTrailingBytes); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("trailing bytes error=%v, want ErrInvalidBundle", err)
	}

	withSecondMember := append(append([]byte(nil), bundle...), bundle...)
	if _, _, _, err := readEnvelope(withSecondMember); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("second gzip member error=%v, want ErrInvalidBundle", err)
	}

	smallBundle, err := buildEnvelope(
		"xinsight-v0.2.0-rc.1-linux-amd64-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
		[]byte("manifest\n"),
		[]byte("signature\n"),
		[]byte("payload\n"),
	)
	if err != nil {
		t.Fatalf("build small envelope: %v", err)
	}
	if _, _, _, err := readEnvelope(rewriteGzipWithTail(t, smallBundle, make([]byte, 1024))); err != nil {
		t.Fatalf("zero tar padding: %v", err)
	}
	if _, _, _, err := readEnvelope(rewriteGzipWithTail(t, smallBundle, []byte("nonzero"))); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("nonzero in-stream tail error=%v, want ErrInvalidBundle", err)
	}
	if _, _, _, err := readEnvelope(rewriteGzipWithTail(t, smallBundle, make([]byte, maxTarEndPaddingBytes+1))); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("excess zero padding error=%v, want ErrInvalidBundle", err)
	}
}

func rewriteGzipWithTail(t *testing.T, bundle []byte, tail []byte) []byte {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(bundle))
	if err != nil {
		t.Fatal(err)
	}
	uncompressed, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	writer := gzip.NewWriter(&output)
	if _, err := writer.Write(append(uncompressed, tail...)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
