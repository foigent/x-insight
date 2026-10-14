package hostctl

import (
	"github.com/foigent/x-insight/internal/sourcefacts"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContentServerCredentialReferencesAreCOnly(t *testing.T) {
	root := t.TempDir()
	engine := Engine{Paths: Paths{ServiceUnit: filepath.Join(root, "xinsight.service"), SourceFactsConfig: filepath.Join(root, "source-facts.json")}}
	id := "11111111-2222-3333-4444-555555555555"
	c := sourcefacts.ManagedConfig{Bindings: []sourcefacts.ManagedCredentialBinding{{Name: "legacy", Scope: "legacy", KeyFile: "/etc/x-insight/legacy.key"}, {Name: "content", Scope: "content", SourceSystemID: id, SourceInstance: "source", OrgID: "7", ProjectID: "9", KeyFile: "/etc/x-insight/content.key"}}, Contents: []sourcefacts.ManagedContentConfig{{CollectionID: id, SourceSystemID: id, SourceInstance: "source", OrgID: "7", ProjectID: "9", CredentialBinding: "content"}}}
	if e := engine.installContentServerCredentials(c); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, "xinsight.service.d", "30-content-credentials.conf")
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	want := "LoadCredential=source-content-" + id + ".key:/etc/x-insight/content.key\n"
	if !strings.Contains(string(raw), want) || strings.Contains(string(raw), "legacy.key") || strings.Contains(string(raw), "LoadCredential=\n") || !strings.Contains(string(raw), "Environment=XINSIGHT_CONTENT_CONFIG="+engine.Paths.SourceFactsConfig) {
		t.Fatalf("bad server credential references %s", raw)
	}
	c.Contents = nil
	if e = engine.installContentServerCredentials(c); e != nil {
		t.Fatal(e)
	}
	raw, e = os.ReadFile(path)
	if e != nil || strings.Contains(string(raw), "LoadCredential=") {
		t.Fatal("old C configuration retained", e)
	}
}
