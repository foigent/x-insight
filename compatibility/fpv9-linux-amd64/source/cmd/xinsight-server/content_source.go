package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

// Resolve only the existing administrator-selected C registration. Reloading
// the public managed config prevents a stale process from silently following a
// different binding after configuration replacement. No legacy key fallback.
func managedContentSource(ctx context.Context, expected sourcefacts.ContentCollection) (*pullfeed.HTTPSource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := os.Getenv("XINSIGHT_CONTENT_CONFIG")
	if path == "" {
		return nil, sourcefacts.ErrUnavailable
	}
	cfg, err := sourcefacts.LoadManagedConfig(path)
	if err != nil {
		return nil, sourcefacts.ErrUnavailable
	}
	for _, entry := range cfg.Contents {
		if entry.CollectionID != expected.ID {
			continue
		}
		resolved, err := cfg.ResolveContent(entry)
		if err != nil || !sourcefacts.SameContentCollection(resolved.Registration(), expected) {
			return nil, sourcefacts.ErrScopeChanged
		}
		keyPath := resolved.KeyFile
		if dir := os.Getenv(config.CredentialsDirEnv); dir != "" {
			if dir != "/run/credentials/xinsight.service" {
				return nil, sourcefacts.ErrAuthorization
			}
			keyPath = filepath.Join(dir, "source-content-"+entry.CollectionID+".key")
		}
		key, err := config.LoadSourceFactsServiceKey(keyPath)
		if err != nil {
			return nil, sourcefacts.ErrAuthorization
		}
		return pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: resolved.Endpoint, ServiceKey: key, AllowPrivateTunnelHTTP: resolved.AllowPrivateTunnelHTTP})
	}
	return nil, sourcefacts.ErrAuthorization
}
