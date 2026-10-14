package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

func TestContentControlOfflineCLIHasNoCredentialOrStateWrites(t *testing.T) {
	var vectors struct {
		Registrations []struct {
			Registration json.RawMessage `json:"registration"`
		} `json:"registrations"`
		Actions []struct {
			Action  string          `json:"action"`
			Request json.RawMessage `json:"request"`
		} `json:"actions"`
	}
	raw, err := os.ReadFile("../../internal/sourcefacts/contract/content-control/u1-request.json")
	if err != nil || json.Unmarshal(raw, &vectors) != nil {
		t.Fatal(err)
	}
	var responses struct {
		GET map[string]json.RawMessage `json:"get"`
	}
	raw, err = os.ReadFile("../../internal/sourcefacts/contract/content-control/u1-response-v2.json")
	if err != nil || json.Unmarshal(raw, &responses) != nil {
		t.Fatal(err)
	}
	reg, err := sourcefacts.DecodeSourceCredentialRegistration(vectors.Registrations[0].Registration)
	if err != nil {
		t.Fatal(err)
	}
	_, hash, _ := reg.MarshalRegistration()
	get, err := sourcefacts.DecodeContentControlGET(responses.GET["active"])
	if err != nil {
		t.Fatal(err)
	}
	get.Active.RegistrationHash, get.ContentAccess.RegistrationHash = hash, hash
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "never-created-content-key")
	id := "11111111-2222-3333-4444-666666666666"
	source := "11111111-2222-3333-4444-555555555555"
	p := sourcefacts.ManagedContentConfig{CollectionID: id, SourceSystemID: source, SourceInstance: reg.SourceInstance, OrgID: reg.OrgID, ProjectID: reg.ProjectID, CredentialBinding: "content", ContentScopeRevision: "2", RecoveryEpoch: "17", AllowedPurposes: []string{sourcefacts.ContentPurposeHumanReview}, CoverageFrom: *get.ContentAccess.CoverageFrom, Endpoint: "https://source.invalid" + sourcefacts.ContentEndpointPath}
	cfg := sourcefacts.ManagedConfig{SchemaVersion: sourcefacts.ManagedContentConfigVersion, PollIntervalSeconds: 1, RetryMaximumSeconds: 10, Profiles: []sourcefacts.ManagedProfileConfig{}, Bindings: []sourcefacts.ManagedCredentialBinding{{Name: "content", Scope: "content", SourceSystemID: source, SourceInstance: p.SourceInstance, OrgID: p.OrgID, ProjectID: p.ProjectID, CredentialID: get.Active.CredentialID, RegistrationHash: hash, KeyFile: keyPath}}, Contents: []sourcefacts.ManagedContentConfig{p}}
	caps := sourcefacts.ContentCapabilities{SchemaVersion: sourcefacts.ContentCapabilitiesVersion, Profile: sourcefacts.ContentProfile, SourceInstance: p.SourceInstance, OrgID: p.OrgID, ProjectID: p.ProjectID, Status: "active", CredentialStatus: "feed_active", SupportedProtocols: []string{"openai.responses"}, ObservationPoints: []string{"client_request"}, CaptureModes: []string{"disabled", "metadata_only", "full_content"}, AllowedPurposes: p.AllowedPurposes, ContentScopeRevision: p.ContentScopeRevision, RecoveryEpoch: p.RecoveryEpoch, CoverageFrom: p.CoverageFrom, RetentionFloor: p.CoverageFrom, Limits: sourcefacts.ContentLimits{MaxSavedBytes: 100, MaxReadBytes: 100, MaxBufferBytes: 10, MaxManifestBytes: 65536, MaxManifestItems: 100}, Endpoints: sourcefacts.ContentEndpoints{Capabilities: sourcefacts.ContentCapabilitiesPath, Manifests: sourcefacts.ContentEndpointPath, Content: "/api/v1/integrations/xinsight/source/v2/contents/{content_ref}"}}
	files := map[string][]byte{}
	for name, v := range map[string]any{"registration": reg, "config": cfg, "get": get, "capabilities": caps} {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = raw
	}
	files["request"] = vectors.Actions[0].Request
	for name, raw := range files {
		if err := os.WriteFile(filepath.Join(dir, name+".json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	fingerprint := func() map[string]string {
		out := map[string]string{}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			out[e.Name()] = sourcefacts.Hash(raw)
		}
		return out
	}
	before := fingerprint()
	t.Setenv(hostctl.TestRootEnv, dir)
	t.Setenv(hostctl.AllowTestRootEnv, "1")
	getArgs := []string{"content-wire-check", "--registration-file", filepath.Join(dir, "registration.json"), "--config-file", filepath.Join(dir, "config.json"), "--get-file", filepath.Join(dir, "get.json"), "--capabilities-file", filepath.Join(dir, "capabilities.json"), "--collection-id", id}
	var out bytes.Buffer
	if err := runSourceFactsControl(context.Background(), nil, getArgs, &out); err != nil {
		t.Fatal("offline GET check", err)
	}
	var result map[string]any
	if json.Unmarshal(out.Bytes(), &result) != nil || result["check_only"] != true || result["binding_candidate"] != true || result["capabilities_matched"] != true || result["evidence"] != "supplied_public_snapshots_only" {
		t.Fatal("wrong check result", out.String())
	}
	out.Reset()
	if err := run(context.Background(), append([]string{"source-facts"}, getArgs...), &out); err != nil {
		t.Fatal("top-level offline CLI", err)
	}
	out.Reset()
	if err := runSourceFactsControl(context.Background(), nil, []string{"content-wire-check", "--action", "prepare", "--request-file", filepath.Join(dir, "request.json")}, &out); err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(out.Bytes(), &result) != nil || result["binding_candidate"] != false || result["receipt_matched"] != false {
		t.Fatal("request check conferred authority")
	}
	// Invalid snapshot remains an offline failure and cannot call an Engine.
	getArgs[len(getArgs)-1] = "unknown"
	if err := runSourceFactsControl(context.Background(), nil, getArgs, &out); err == nil {
		t.Fatal("unknown collection accepted")
	}
	if !reflect.DeepEqual(before, fingerprint()) {
		t.Fatal("check modified public documents")
	}
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatal("check touched credential path")
	}
}
