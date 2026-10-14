package api_test

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

// This is a CC15 qualification fixture, not a product importer command.
// The check branch verifies every input and produces only in-memory bytes.
type cc15ActionAssemblyFile struct {
	SchemaVersion     string                                   `json:"schema_version"`
	ManifestFile      string                                   `json:"manifest_file"`
	EventsFile        string                                   `json:"events_file"`
	RequestsReadyFile string                                   `json:"requests_ready_file"`
	RelationshipFile  string                                   `json:"relationship_file"`
	CheckerBeforeFile string                                   `json:"checker_before_file"`
	CheckerAfterFile  string                                   `json:"checker_after_file"`
	BeforeFiles       map[string]string                        `json:"before_files"`
	AfterFiles        map[string]string                        `json:"after_files"`
	Reporter          workbench.ResourceScope                  `json:"reporter"`
	Target            workbench.ResourceScope                  `json:"target"`
	AllowedObjects    map[string]workbench.ActionObject        `json:"allowed_objects"`
	ContentLinks      map[string][]workbench.ActionContentLink `json:"content_links"`
}

func cc15PrepareBundle(specPath, outputPath string, check bool) ([]byte, error) {
	owner := uint32(os.Getuid())
	read := func(path string, limit int64) ([]byte, error) { return sourcefacts.ReadOwnedFile(path, limit, owner) }
	raw, err := read(specPath, 64<<10)
	if err != nil {
		return nil, err
	}
	var spec cc15ActionAssemblyFile
	if err = json.Unmarshal(raw, &spec); err != nil || spec.SchemaVersion != "xinsight.cc15-action-assembly-input.v1" || len(spec.BeforeFiles) != 3 || len(spec.AfterFiles) != 3 || len(spec.AllowedObjects) != 2 {
		return nil, sourcefacts.ErrInvalid
	}
	in := cc15ActionAssemblyInput{Reporter: spec.Reporter, Target: spec.Target, AllowedObjects: spec.AllowedObjects, ContentLinks: spec.ContentLinks, BeforeFiles: map[string][]byte{}, AfterFiles: map[string][]byte{}}
	for _, entry := range []struct {
		path   string
		target *[]byte
		limit  int64
	}{
		{spec.ManifestFile, &in.ManifestRaw, 16 << 10}, {spec.EventsFile, &in.EventsRaw, 4 << 20}, {spec.RequestsReadyFile, &in.RequestsReadyRaw, 1 << 20},
		{spec.RelationshipFile, &in.RelationshipRaw, 1 << 20}, {spec.CheckerBeforeFile, &in.CheckerBeforeRaw, 1 << 20}, {spec.CheckerAfterFile, &in.CheckerAfterRaw, 1 << 20},
	} {
		*entry.target, err = read(entry.path, entry.limit)
		if err != nil {
			return nil, err
		}
	}
	for _, name := range []string{"input.md", "report.md", "task.md"} {
		if in.BeforeFiles[name], err = read(spec.BeforeFiles[name], 64<<10); err != nil {
			return nil, err
		}
		if in.AfterFiles[name], err = read(spec.AfterFiles[name], 64<<10); err != nil {
			return nil, err
		}
	}
	records, err := cc15AssembleActionWitnesses(in)
	if err != nil {
		return nil, err
	}
	bundle, err := json.Marshal(struct {
		Version string                    `json:"schema_version"`
		Records []workbench.ActionWitness `json:"records"`
	}{sourcefacts.ActionOfflineBundleVersion, records})
	if err != nil {
		return nil, err
	}
	if check {
		return bundle, nil
	}
	if !filepath.IsAbs(outputPath) || filepath.Clean(outputPath) != outputPath {
		return nil, sourcefacts.ErrInvalid
	}
	f, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	_, writeErr := f.Write(bundle)
	closeErr := f.Close()
	if writeErr != nil {
		return nil, writeErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return bundle, nil
}
