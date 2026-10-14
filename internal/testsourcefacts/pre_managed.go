package testsourcefacts

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

// SeedPreManaged is only a migration predecessor fixture. Current imports use
// Replay and the real consumer; this admin helper does not claim transport proof.
func SeedPreManaged(t *testing.T, admin *sql.DB, source string, bundle Bundle, generation string) {
	t.Helper()
	var managed bool
	if err := admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='source_fact_streams' AND column_name='requires_attention')`).Scan(&managed); err != nil || managed {
		t.Fatal("pre-managed schema fixture required", err)
	}
	streams := map[string]string{}
	for _, captured := range bundle.Pages {
		var wire sourcefacts.Page
		if err := json.Unmarshal(captured.Page, &wire); err != nil {
			t.Fatal(err)
		}
		page, err := sourcefacts.DecodePage(captured.Page, wire.ProducerInstance, captured.Org, captured.Project, captured.Profile, captured.Previous, 50)
		if err != nil {
			t.Fatal("retained predecessor page", err)
		}
		if streams[captured.Profile] == "" {
			var id string
			if err = admin.QueryRow(`INSERT INTO source_fact_streams(source_system_id,source_instance,org_id,project_id,profile,scope_generation,status,cursor_value) VALUES($1::uuid,$2,$3,$4,$5,$6,'active',$7) RETURNING id::text`, source, wire.ProducerInstance, captured.Org, captured.Project, captured.Profile, generation, captured.Previous).Scan(&id); err != nil {
				t.Fatal(err)
			}
			streams[captured.Profile] = id
		}
		for _, item := range page.Items {
			d := item.Document
			if _, err = admin.Exec(`INSERT INTO source_fact_documents(source_system_id,source_instance,org_id,project_id,request_id,profile,stage,source_fact_id,revision,event_id,previous_hash,source_hash,envelope_hash,source_bytes,envelope_bytes,source_json,occurred_at,recorded_at) VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16::jsonb,$17,$18)`, source, d.SourceInstance, d.OrgID, d.ProjectID, d.RequestID, d.Profile, d.Stage, d.SourceFactID, d.Revision, d.EventID, d.PreviousHash, item.SourceHash, item.EnvelopeHash, item.SourceBytes, item.EnvelopeBytes, string(item.SourceBytes), d.OccurredAt, d.RecordedAt); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = admin.Exec(`UPDATE source_fact_streams SET cursor_value=$2,high_watermark=$3,revision=revision+1,last_received_at=$4 WHERE id=$1::uuid`, streams[captured.Profile], page.NextCursor, page.HighWatermark, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
}
