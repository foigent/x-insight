package hostctl

import (
	"crypto/sha1" // The original Git blob identity, not a release signature.
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

type publishedResumeSourceBinding struct {
	commit, tree, manifest, manifestSHA256, source string
}

var publishedResumeSourceBindings = map[string]publishedResumeSourceBinding{
	"FPV11": {
		"9c58e07f011918c4a0aed2f876ce4baa9fb12b5e", "b832769cccf7b91a6e9fd3ac4a27b217fb30b85f",
		"compatibility/fpv11-linux-amd64/source-manifest.json", "a28706219000d441df9415622e55de392ee3c898dd001752371f60598f559f0f",
		"compatibility/fpv11-linux-amd64/source",
	},
	"FPV12": {
		"b1b6067dbd5bce1c9e9988fc12c1e1f28d979952", "1ba09661e25f3a946d67954f2095f7179b4dc81d",
		"internal/hostctl/testdata/published-fpv12-resume/source-manifest.json", "86027020554f6412ed9a87cec2ac665c8e8c47d979c12309c27697c4417a6914",
		"internal/hostctl/testdata/published-fpv12-resume/source",
	},
}

var publishedResumeArchiveInputs = []string{
	"migrations", "deploy", "schemas", "fixtures", "internal/webui/manual/zh-CN",
	"release/d-validation-config.json", "release/d-validation-compatibility-matrix.json",
	"release/d-validation-notes.md", "internal/hostctl/development_schema_contract.json",
}

// The old test used git archive on these exact versions. Validate the retained
// original inputs, then copy the same selection into an independent test fixture.
// No current release metadata, network fetch or private Git history is a fallback.
func copyPublishedResumeSource(repo, profile, commit, destination string) error {
	binding, ok := publishedResumeSourceBindings[profile]
	if !ok || commit != binding.commit {
		return fmt.Errorf("unbound published resume identity: %s %s", profile, commit)
	}
	repo, err := filepath.Abs(repo)
	if err != nil {
		return err
	}
	regular := func(path string) (fs.FileInfo, error) {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Sys().(*syscall.Stat_t).Nlink != 1 {
			return nil, fmt.Errorf("published resume input is not regular single-link: %s", path)
		}
		return info, nil
	}
	manifestPath := filepath.Join(repo, binding.manifest)
	if _, err := regular(manifestPath); err != nil {
		return err
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	if fmt.Sprintf("%x", sha256.Sum256(raw)) != binding.manifestSHA256 {
		return fmt.Errorf("published resume manifest digest: %s", profile)
	}
	var manifest struct {
		OriginCommit string `json:"origin_commit"`
		OriginTree   string `json:"origin_tree"`
		Files        []struct {
			Path       string `json:"path"`
			Bytes      int64  `json:"bytes"`
			SHA256     string `json:"sha256"`
			GitMode    string `json:"git_mode"`
			OriginBlob string `json:"origin_blob"`
		} `json:"files"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return err
	}
	if manifest.OriginCommit != commit || manifest.OriginTree != binding.tree || len(manifest.Files) == 0 {
		return fmt.Errorf("published resume manifest origin: %s", profile)
	}
	source := filepath.Join(repo, binding.source)
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil || resolved != source {
		return fmt.Errorf("published resume source path: %s", source)
	}
	expected, directories := map[string]bool{}, map[string]bool{".": true}
	selected := map[string][]byte{}
	modes := map[string]fs.FileMode{}
	matched := map[string]bool{}
	for _, file := range manifest.Files {
		name := file.Path
		if !fs.ValidPath(name) || strings.Contains(name, "\\") || expected[name] || (file.GitMode != "100644" && file.GitMode != "100755") {
			return fmt.Errorf("published resume manifest path or mode: %s", name)
		}
		expected[name] = true
		for parent := filepath.Dir(name); parent != "."; parent = filepath.Dir(parent) {
			directories[parent] = true
		}
		path := filepath.Join(source, name)
		info, err := regular(path)
		if err != nil {
			return err
		}
		if info.Size() != file.Bytes || (info.Mode()&0o111 != 0) != (file.GitMode == "100755") {
			return fmt.Errorf("published resume input size or mode: %s", name)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		gitBlob := sha1.New()
		fmt.Fprintf(gitBlob, "blob %d\x00", len(content))
		gitBlob.Write(content)
		if fmt.Sprintf("%x", sha256.Sum256(content)) != file.SHA256 || fmt.Sprintf("%x", gitBlob.Sum(nil)) != file.OriginBlob {
			return fmt.Errorf("published resume original input digest: %s", name)
		}
		for _, input := range publishedResumeArchiveInputs {
			if name == input || strings.HasPrefix(name, input+"/") {
				selected[name], matched[input] = content, true
				modes[name] = 0o644
				if file.GitMode == "100755" {
					modes[name] = 0o755
				}
			}
		}
	}
	actual := 0
	if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if entry.IsDir() && directories[name] {
			return nil
		}
		if !entry.Type().IsRegular() || !expected[name] {
			return fmt.Errorf("published resume unexpected input: %s", name)
		}
		actual++
		return nil
	}); err != nil {
		return err
	}
	if actual != len(expected) || len(matched) != len(publishedResumeArchiveInputs) {
		return fmt.Errorf("published resume input set is incomplete: %s", profile)
	}
	entries, err := os.ReadDir(destination)
	if err != nil || len(entries) != 0 {
		return fmt.Errorf("published resume destination is not empty")
	}
	for name, content := range selected {
		path := filepath.Join(destination, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, content, modes[name]); err != nil {
			return err
		}
	}
	return nil
}

func retainedPublishedResumeSource(t *testing.T, profile, commit string) string {
	t.Helper()
	destination := t.TempDir()
	if err := copyPublishedResumeSource("../..", profile, commit, destination); err != nil {
		t.Fatalf("read exact retained %s source: %v", profile, err)
	}
	return destination
}

func TestPublishedResumeSourceBindingsWithoutGit(t *testing.T) {
	for profile, binding := range publishedResumeSourceBindings {
		t.Run(profile, func(t *testing.T) {
			destination := t.TempDir()
			if err := copyPublishedResumeSource("../..", profile, binding.commit, destination); err != nil {
				t.Fatal(err)
			}
			var config struct {
				ProductVersion string `json:"product_version"`
			}
			raw, err := os.ReadFile(filepath.Join(destination, "release/d-validation-config.json"))
			if err != nil || json.Unmarshal(raw, &config) != nil || config.ProductVersion != "0.2.0-dev."+strings.ToLower(profile) {
				t.Fatal("retained historical profile changed", config, err)
			}
		})
	}
}

func TestPublishedResumeSourceRejectsUnboundIdentityAndManifest(t *testing.T) {
	for _, profile := range []string{"unknown", "FPV11", "FPV12"} {
		destination := t.TempDir()
		if err := copyPublishedResumeSource("../..", profile, strings.Repeat("0", 40), destination); err == nil {
			t.Fatal("unbound historical identity accepted", profile)
		}
		if entries, err := os.ReadDir(destination); err != nil || len(entries) != 0 {
			t.Fatal("rejection wrote a fixture", entries, err)
		}
	}
	root := t.TempDir()
	binding := publishedResumeSourceBindings["FPV12"]
	manifestPath := filepath.Join(root, binding.manifest)
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyPublishedResumeSource(root, "FPV12", binding.commit, t.TempDir()); err == nil || !strings.Contains(err.Error(), "manifest digest") {
		t.Fatal("unbound source manifest accepted", err)
	}
}
