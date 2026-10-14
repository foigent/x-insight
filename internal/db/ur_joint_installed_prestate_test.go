//go:build integration

package db_test

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

// Reuse the existing real human A/C/feed fixture against the installed old
// instance. No connector terminal, loaded proof or paused authorization is
// seeded here: the caller must run the old product's backup and restore next.
func TestXIInstalledOriginalPrestate(t *testing.T) {
	if os.Getenv("XINSIGHT_CI_INSTALLED_PRESTATE") != "1" {
		t.Skip("installed predecessor fixture not selected")
	}
	if runtime.GOOS != "linux" || os.Geteuid() != 0 || os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("XINSIGHT_CI_EPHEMERAL_HOST") != "1" {
		t.Fatal("explicit disposable Linux root fixture required")
	}
	var input urJointConsumerInput
	if json.NewDecoder(io.LimitReader(os.Stdin, 20<<10)).Decode(&input) != nil || input.ManagedMode != "adopt_existing" || input.SourceInstance != "synthetic-source-control" || input.BaseURL != "https://198.18.0.2:9444" {
		t.Fatal("installed predecessor input differs")
	}
	paths, err := hostctl.PathsFromEnvironment()
	if err != nil || paths.IsTestRoot {
		t.Fatal("real installed paths required")
	}
	e, err := hostctl.NewEngine(paths)
	if err != nil {
		t.Fatal("installed engine unavailable")
	}
	status, err := e.Status(t.Context())
	if err != nil || status.InstallState == nil || status.InstallState.Current.ProductVersion != "0.2.0-dev.fpv14" {
		t.Fatal("exact installed FPV14 prestate required")
	}
	dsn, err := config.LoadOwnerOnlySecretFile(paths.AdminDSNFile)
	if err != nil {
		t.Fatal("installed isolated database unavailable")
	}
	f := &sourceFixture{t: t, ctx: t.Context(), dsn: dsn, source: unirouter.DefaultSourceSystemID,
		pages: map[string]sourcefacts.Page{}, documents: map[string]sourcefacts.Document{}, generation: "1"}
	f.admin = openTestSQL(t, dsn)
	t.Cleanup(func() { f.admin.Close() })
	f.consumerStore, err = db.OpenSourceFactsStore(dsn)
	if err != nil {
		t.Fatal("installed source store unavailable")
	}
	t.Cleanup(func() { f.consumerStore.Close() })
	f.evidence, err = db.OpenEvidenceStore(t.Context(), dsn)
	if err != nil {
		t.Fatal("installed evidence store unavailable")
	}
	t.Cleanup(func() { f.evidence.Close() })
	loadSourceFixtureDocuments(t, f)
	roots, err := x509.SystemCertPool()
	if err != nil {
		t.Fatal("installed TLS trust unavailable")
	}
	client := &http.Client{Timeout: 12 * time.Second}
	e.HTTPClient = client
	call := func(method, path string, value any) map[string]json.RawMessage {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal("human input invalid")
		}
		req, err := http.NewRequestWithContext(t.Context(), method, input.BaseURL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal("human request invalid")
		}
		req.Header.Set("Content-Type", "application/json")
		response, err := client.Do(req)
		if err != nil {
			t.Fatal("original human transport failed")
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			t.Fatalf("original human status=%d", response.StatusCode)
		}
		var out map[string]json.RawMessage
		if json.NewDecoder(io.LimitReader(response.Body, 256<<10)).Decode(&out) != nil {
			t.Fatal("original human response invalid")
		}
		return out
	}
	put := func(path string, raw []byte, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal("original fixture directory failed")
		}
		if err := os.WriteFile(path, raw, mode); err != nil {
			t.Fatal("original fixture write failed")
		}
	}
	setupJointOriginalFeed(t, t.Context(), input, roots, e, f, call)
	setupJointOriginalSources(t, t.Context(), input, roots, e, f, call, put)
	// Reproduce the supported direct-connection egress contract across all
	// three consumers. The new-install case must obtain this through the
	// product-managed connection flow instead.
	for _, unit := range []string{paths.ServiceUnit, paths.FeedServiceUnit, paths.SourceFactsServiceUnit} {
		path := filepath.Join(unit+".d", "40-xinsight-reviewed-source-egress.conf")
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("original egress fixture already exists or cannot be inspected")
		}
		put(path, []byte("[Service]\nIPAddressAllow=\nIPAddressAllow=localhost\nIPAddressAllow=198.18.0.2/32\n"), 0644)
	}
	seedJointRestoredOriginalHistory(t, t.Context(), input, f)
	// A second retired source has no new upstream grant. Restore and adoption
	// must leave it paused while the explicitly admitted source resumes.
	for _, profile := range input.Profiles {
		_, err = f.consumerStore.RegisterSourceFactsStream(t.Context(), sourcefacts.Stream{ID: testUUID(t), SourceSystemID: f.source, SourceInstance: "retired-joint-source", OrgID: input.OrgID, ProjectID: "2", Profile: profile, ScopeGeneration: "1"})
		if err != nil {
			t.Fatal("retired stream fixture failed")
		}
	}
	_, err = f.consumerStore.RegisterContentCollection(t.Context(), sourcefacts.ContentCollection{ID: testUUID(t), SourceSystemID: f.source, SourceInstance: "retired-joint-source", OrgID: input.OrgID, ProjectID: "2", CredentialID: "retired-fixture", RegistrationHash: sourcefacts.Hash([]byte("retired-fixture")), ContentScopeRevision: "retired-scope", RecoveryEpoch: "1", AllowedPurposes: []string{sourcefacts.ContentPurposeHumanReview}, CoverageFrom: sourcefacts.ContentBoundary{Time: "2026-09-01T00:00:00.000000Z", AfterSequence: "0"}})
	if err != nil {
		t.Fatal("retired content fixture failed")
	}
}
