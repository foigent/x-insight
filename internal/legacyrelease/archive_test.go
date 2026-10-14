package legacyrelease

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"testing"
)

func TestInspectArchiveConsumesValidTarPadding(t *testing.T) {
	artifact := legacyArchiveForTest(t, nil)
	root, files, err := inspectArchive(artifact)
	if err != nil {
		t.Fatalf("inspect archive: %v", err)
	}
	if root != "release" || string(files["release-manifest.json"]) != "{}\n" {
		t.Fatalf("root=%q files=%v", root, files)
	}
}

func TestInspectArchiveRejectsNonZeroBytesAfterTarTerminator(t *testing.T) {
	artifact := legacyArchiveForTest(t, []byte{1})
	if _, _, err := inspectArchive(artifact); err == nil {
		t.Fatal("expected non-zero trailing tar bytes rejection")
	}
}

func legacyArchiveForTest(t *testing.T, afterTar []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, header := range []*tar.Header{
		{Name: "release/", Typeflag: tar.TypeDir, Mode: 0o755, Uid: 0, Gid: 0, Uname: "root", Gname: "root"},
		{Name: "release/release-manifest.json", Typeflag: tar.TypeReg, Mode: 0o644, Size: 3, Uid: 0, Gid: 0, Uname: "root", Gname: "root"},
	} {
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			if _, err := tarWriter.Write([]byte("{}\n")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if len(afterTar) > 0 {
		if _, err := gzipWriter.Write(afterTar); err != nil {
			t.Fatal(err)
		}
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
