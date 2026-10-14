//go:build ignore

// This is run only on an explicitly disposable qualification VM, after the
// exact FPV9 migrations and before the three old services are started.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "check-fixture" {
		c := fixtureCollection("11111111-1111-4111-8111-111111111111", "7996279c-df2b-4c0d-aaba-7c2a3b3d510b")
		if _, err := fixturePage(c, time.Now().UTC()); err != nil {
			panic(err)
		}
		fmt.Println("fpv9_content_fixture_contract=verified_offline")
		return
	}
	if os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("XINSIGHT_CI_EPHEMERAL_HOST") != "1" || os.Geteuid() != 0 {
		panic("source role setup requires the disposable root-owned CI host")
	}
	if len(os.Args) == 4 && os.Args[1] == "seed-content" {
		if err := seedContent(os.Args[2], os.Args[3]); err != nil {
			panic(err)
		}
		fmt.Println("fpv9_full_content_manifest_and_receipt=verified")
		return
	}
	if len(os.Args) != 1 {
		panic("usage: fpv9-source-roles [seed-content SOURCE_ID COLLECTION_ID]")
	}
	paths, err := hostctl.PathsFromEnvironment()
	if err != nil {
		panic(err)
	}
	credentials, err := hostctl.LoadDatabaseCredentials(paths)
	if err != nil {
		panic(err)
	}
	manager := hostctl.PostgresManager{Runner: hostctl.ExecRunner{}}
	ctx := context.Background()
	if err := manager.EnsureRoles(ctx, credentials); err != nil {
		panic(err)
	}
	if err := manager.ApplyGrants(ctx, credentials); err != nil {
		panic(err)
	}
	if err := manager.VerifyUpgradeSourceRoleBoundaries(ctx, credentials, "0.2.0-dev.fpv9"); err != nil {
		panic(err)
	}
	fmt.Println("fpv9_source_roles=verified")
}

func canonical(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	obj, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		return nil, err
	}
	return connector.CanonicalizeJSONValue(obj)
}

func seedContent(sourceID, collectionID string) error {
	dsn, err := config.LoadAdminPostgresDSN()
	if err != nil {
		return err
	}
	store, err := db.OpenSourceFactsStore(dsn)
	if err != nil {
		return err
	}
	defer store.Close()
	ctx := context.Background()
	c := fixtureCollection(sourceID, collectionID)
	c, err = store.RegisterContentCollection(ctx, c)
	if err != nil {
		return err
	}
	c, err = store.ControlContentCollection(ctx, c.ID, "active", c.Revision, false, c)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	rawPage, err := fixturePage(c, now)
	if err != nil {
		return err
	}
	if err := store.CommitContentManifests(ctx, c, rawPage, now); err != nil {
		return err
	}
	loaded, err := store.LoadContentCollection(ctx, c.ID)
	if err != nil {
		return err
	}
	if loaded.Status != "active" || loaded.Cursor != "ci-content-cursor" || loaded.LastSequence != "1" {
		return fmt.Errorf("content checkpoint not persisted")
	}
	return nil
}

func fixtureCollection(sourceID, collectionID string) sourcefacts.ContentCollection {
	const at = "2026-09-25T00:00:00.000000Z"
	c := sourcefacts.ContentCollection{
		ID: collectionID, SourceSystemID: sourceID, SourceInstance: "ci-source", OrgID: "2", ProjectID: "86",
		CredentialID: "ci-content-credential", RegistrationHash: "sha256:" + strings.Repeat("a", 64),
		ContentScopeRevision: "2", RecoveryEpoch: "1", AllowedPurposes: []string{sourcefacts.ContentPurposeHumanReview},
		CoverageFrom: sourcefacts.ContentBoundary{Time: at, AfterSequence: "0"},
	}
	return c
}

func fixturePage(c sourcefacts.ContentCollection, now time.Time) ([]byte, error) {
	manifestAt := now.Add(-time.Minute).Format(sourcefacts.TimeFormat)
	d := sourcefacts.ContentManifest{
		SchemaVersion: sourcefacts.ContentManifestVersion, SourceInstance: c.SourceInstance, OrgID: c.OrgID,
		ProjectID: c.ProjectID, RequestID: "ci-old-request", Profile: sourcefacts.ContentProfile,
		Revision: "1", OccurredAt: manifestAt, RecordedAt: manifestAt, Protocol: "openai.responses",
		ObservationPoint: "client_request", ContentItemID: "body",
		Capture: sourcefacts.ContentCapture{Mode: "full_content", PolicyRef: "ci-policy", PolicyVersion: "1",
			PolicyEffectiveAt: manifestAt, Completeness: "complete", Reason: "none"},
		ContentRef: "uc1_" + strings.Repeat("c", 64),
		Representation: &sourcefacts.ContentRepresentation{MediaType: "application/json", Encoding: "identity",
			ByteDomain: "stored_representation_before_http_content_encoding", SavedBytes: 2, Digest: sourcefacts.Hash([]byte("{}"))},
		CapturedAt: manifestAt, ExpiresAt: now.Add(24 * time.Hour).Format(sourcefacts.TimeFormat),
	}
	d.SourceFactID = sourcefacts.SourceID(sourcefacts.ContentManifestVersion, d.SourceInstance, d.OrgID, d.ProjectID,
		d.Profile, d.RequestID, d.ObservationPoint, d.AttemptID, d.ContentItemID)
	d.EventID = sourcefacts.SourceID(sourcefacts.ContentManifestVersion+".revision", d.SourceFactID, d.Revision)
	rawDoc, err := canonical(d)
	if err != nil {
		return nil, err
	}
	page := sourcefacts.ContentManifestPage{
		SchemaVersion: sourcefacts.ContentManifestPageVersion, Profile: sourcefacts.ContentProfile,
		SourceInstance: c.SourceInstance, OrgID: c.OrgID, ProjectID: c.ProjectID,
		GeneratedAt: now.Format(sourcefacts.TimeFormat), ContentScopeRevision: c.ContentScopeRevision,
		RecoveryEpoch: c.RecoveryEpoch, CoverageFrom: c.CoverageFrom, RetentionFloor: c.CoverageFrom,
		AfterSequence: "0", LastSequence: "1", HighWatermarkSequence: "1", NextCursor: "ci-content-cursor",
		HighWatermark: "ci-content-hwm", Coverage: "bounded", Gaps: []sourcefacts.ContentGap{},
		Items: []sourcefacts.ContentManifestEntry{{Sequence: "1", Document: d, SourceSHA256: sourcefacts.Hash(rawDoc),
			Availability: sourcefacts.ContentAvailability{State: "available", EvaluatedAt: manifestAt}}},
	}
	rawPage, err := canonical(page)
	if err != nil {
		return nil, err
	}
	if _, err := sourcefacts.DecodeContentManifestPage(rawPage); err != nil {
		return nil, fmt.Errorf("synthetic content page fails the source contract: %w", err)
	}
	return rawPage, nil
}
