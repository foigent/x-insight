package api_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/investigation"
)

// The handler uses its actual reader connection. Raw checkpoint values remain
// only in the synthetic database and test inputs, never in qualification logs.
func TestConnectorDigestReaderHTTPAndRedaction(t *testing.T) {
	f := startResponsibilityProduction(t)
	ctx := context.Background()
	limited := responsibilityLogin(t, f, "assignee.reader")
	if status, _ := limited.call(t, "GET", "/api/v1/connectors", nil, nil); status != 403 {
		t.Fatal("workbench-only account gained connector access")
	}
	human := responsibilityLogin(t, f, "reviewer.two")
	read := func() ([]investigation.ConnectorTrust, []byte) {
		t.Helper()
		status, raw := human.call(t, "GET", "/api/v1/connectors", nil, nil)
		var response struct {
			Items []investigation.ConnectorTrust
		}
		if status != 200 || json.Unmarshal(raw, &response) != nil || response.Items == nil {
			t.Fatalf("human Connectors read failed: status=%d", status)
		}
		return response.Items, raw
	}
	if rows, _ := read(); len(rows) != 0 {
		t.Fatal("new database must return a real empty list")
	}
	var sourceID, adapterID string
	if err := f.admin.QueryRow(`INSERT INTO source_systems(source_key,source_type,display_name)
VALUES ('synthetic-digest','synthetic','Synthetic digest semantics') RETURNING id::text`).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := f.admin.QueryRow(`INSERT INTO adapter_manifests(adapter_id,adapter_version,source_type,fidelity_level,
ingestion_modes,payload_behavior,idempotency_strategy)
VALUES ('synthetic-digest','1.0.0','synthetic','F1',ARRAY['db_import'],'metadata_only_default','source_ref_event_type_source_event_id')
RETURNING id::text`).Scan(&adapterID); err != nil {
		t.Fatal(err)
	}
	values := []any{nil, "", "synthetic-private-ascii-value", "合成游标：中文é🙂", "synthetic\n\t'\"\\/%?&=#"}
	for i, value := range values {
		key := []string{"null", "empty", "ascii", "unicode", "special"}[i]
		if _, err := f.admin.Exec(`INSERT INTO adapter_import_states(source_system_id,adapter_manifest_id,cursor_key,cursor_value,high_watermark)
VALUES ($1,$2,$3,$4,$5)`, sourceID, adapterID, key, value, values[len(values)-1-i]); err != nil {
			t.Fatal(err)
		}
	}
	items, raw := read()
	if len(items) != len(values) {
		t.Fatal("checkpoint cases missing")
	}
	adminStore, err := db.OpenEvidenceStore(ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer adminStore.Close()
	adminItems, err := adminStore.ListConnectorTrust(ctx, 100)
	if err != nil || !reflect.DeepEqual(items, adminItems) {
		t.Fatal("actual reader and administrator projections differ")
	}
	for _, item := range items {
		i := map[string]int{"null": 0, "empty": 1, "ascii": 2, "unicode": 3, "special": 4}[item.Checkpoint.CursorKey]
		for _, check := range []struct {
			input   any
			present bool
			digest  string
		}{
			{values[i], item.Checkpoint.CursorPresent, item.Checkpoint.CursorDigest},
			{values[len(values)-1-i], item.Checkpoint.HighWatermarkPresent, item.Checkpoint.HighWatermarkDigest},
		} {
			want := ""
			if check.input != nil {
				sum := sha256.Sum256([]byte(check.input.(string)))
				want = "sha256:" + hex.EncodeToString(sum[:])
			}
			if check.present != (check.input != nil) || check.digest != want {
				t.Fatalf("existence or independent UTF-8 SHA-256 mismatch for case %s", item.Checkpoint.CursorKey)
			}
		}
	}
	assertNoRaw := func(raw []byte) {
		t.Helper()
		for _, value := range values[2:] {
			encoded, _ := json.Marshal(value)
			if bytes.Contains(raw, encoded) || bytes.Contains(raw, []byte(value.(string))) {
				t.Fatal("raw synthetic checkpoint leaked")
			}
		}
		for _, field := range []string{`"cursor_value"`, `"high_watermark"`, `"last_error"`} {
			if bytes.Contains(raw, []byte(field)) {
				t.Fatal("raw checkpoint field exposed")
			}
		}
	}
	assertNoRaw(raw)
	if _, err := f.admin.Exec(`REVOKE EXECUTE ON FUNCTION public.digest(text,text) FROM xinsight_reader`); err != nil {
		t.Fatal(err)
	}
	status, failure := human.call(t, "GET", "/api/v1/connectors", nil, nil)
	if status != 500 || bytes.Contains(failure, []byte(`"items"`)) || bytes.Contains(failure, []byte("digest")) {
		t.Fatalf("missing permission must remain a sanitized failure, status=%d", status)
	}
	assertNoRaw(failure)
	prepareWorkbenchRuntimeRoles(t, ctx, f.dsn) // formal ApplyGrants repairs the controlled revoke
	read()
	if output := os.Getenv("XINSIGHT_CONNECTOR_BROWSER_OUT"); output != "" {
		if !filepath.IsAbs(output) || strings.Contains(output, "..") {
			t.Fatal("absolute browser evidence directory required")
		}
		if err := os.MkdirAll(output, 0700); err != nil {
			t.Fatal(err)
		}
		metadata, _ := json.Marshal(map[string]string{"url": f.server.URL, "scope": "Local synthetic actual reader TLS production handler"})
		if err := os.WriteFile(filepath.Join(output, "server.json"), metadata, 0600); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(6 * time.Minute)
		for {
			if _, err := os.Stat(filepath.Join(output, "stop")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("browser did not finish before fixture deadline")
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	t.Log("empty/NULL/empty-string/ASCII/Unicode/special checkpoints: exact reader/admin/Go SHA-256 agreement; human HTTP 200, sanitized 500 on revoke, formal repair, no raw fields")
}
