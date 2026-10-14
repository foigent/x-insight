package webui

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"strings"
)

const AssetManifestSchema = "xinsight.webui-asset-manifest.v1"

var assetDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type AssetManifest struct {
	SchemaVersion string               `json:"schema_version"`
	Entries       []AssetManifestEntry `json:"entries"`
}

type AssetManifestEntry struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

// EmbeddedAssetManifest returns the exact generated manifest bytes that were
// linked into the current runtime. Loading it also proves that every declared
// index/JS/CSS byte matches the committed digest.
func EmbeddedAssetManifest() ([]byte, error) {
	assets, err := fs.Sub(embedded, "dist")
	if err != nil {
		return nil, fmt.Errorf("embedded dist directory is unavailable")
	}
	contents, _, err := loadAssetManifest(assets)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), contents...), nil
}

func loadAssetManifest(assets fs.FS) ([]byte, map[string]struct{}, error) {
	contents, err := fs.ReadFile(assets, "asset-manifest.json")
	if err != nil || len(contents) == 0 || len(contents) > 64<<10 {
		return nil, nil, fmt.Errorf("embedded asset manifest is unavailable or invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var manifest AssetManifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, nil, fmt.Errorf("decode embedded asset manifest: %w", err)
	}
	if err := rejectTrailingManifestJSON(decoder); err != nil {
		return nil, nil, err
	}
	if manifest.SchemaVersion != AssetManifestSchema || len(manifest.Entries) != 3 {
		return nil, nil, fmt.Errorf("embedded asset manifest identity or entry count is invalid")
	}

	references, err := indexAssetReferences(assets)
	if err != nil {
		return nil, nil, err
	}
	expectedPaths := []string{"index.html"}
	for reference := range references {
		expectedPaths = append(expectedPaths, strings.TrimPrefix(reference, "/"))
	}
	if expectedPaths[1] > expectedPaths[2] {
		expectedPaths[1], expectedPaths[2] = expectedPaths[2], expectedPaths[1]
	}

	allowed := make(map[string]struct{}, 2)
	for index, entry := range manifest.Entries {
		if entry.Path != expectedPaths[index] ||
			path.Clean(entry.Path) != entry.Path ||
			strings.HasPrefix(entry.Path, "/") ||
			entry.SizeBytes <= 0 ||
			!assetDigestPattern.MatchString(entry.SHA256) {
			return nil, nil, fmt.Errorf("embedded asset manifest entry is invalid")
		}
		asset, err := fs.ReadFile(assets, entry.Path)
		if err != nil || int64(len(asset)) != entry.SizeBytes {
			return nil, nil, fmt.Errorf("embedded release asset size does not match manifest: %s", entry.Path)
		}
		digest := sha256.Sum256(asset)
		if "sha256:"+hex.EncodeToString(digest[:]) != entry.SHA256 {
			return nil, nil, fmt.Errorf("embedded release asset digest does not match manifest: %s", entry.Path)
		}
		if entry.Path != "index.html" {
			allowed["/"+entry.Path] = struct{}{}
		}
	}
	return contents, allowed, nil
}

func indexAssetReferences(assets fs.FS) (map[string]struct{}, error) {
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		return nil, fmt.Errorf("embedded index is unavailable")
	}
	references := assetReferencePattern.FindAllString(string(index), -1)
	if len(references) != 2 {
		return nil, fmt.Errorf("embedded index must contain exactly two asset references")
	}
	result := make(map[string]struct{}, len(references))
	for _, reference := range references {
		if _, duplicate := result[reference]; duplicate {
			return nil, fmt.Errorf("embedded index contains a duplicate asset reference")
		}
		result[reference] = struct{}{}
	}
	return result, nil
}

func rejectTrailingManifestJSON(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	}
	return fmt.Errorf("embedded asset manifest contains trailing JSON")
}
