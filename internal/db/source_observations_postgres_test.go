package db_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

func TestObservationHTTPPostgresReceiptAndRevocation(t *testing.T) {
	f := newSourceFixture(t)
	var aBefore, moneyBefore int
	if err := f.admin.QueryRow(`SELECT (SELECT count(*) FROM source_fact_documents),(SELECT count(*) FROM workbench_economic_facts)`).Scan(&aBefore, &moneyBefore); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../sourcefacts/contract/observations/golden-observations.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Valid []struct {
			Canonical string `json:"canonical_utf8"`
		}
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	documents := map[string][]json.RawMessage{}
	var sample sourcefacts.Observation
	for _, v := range fixture.Valid {
		d, err := sourcefacts.DecodeObservation([]byte(v.Canonical))
		if err != nil {
			t.Fatal(err)
		}
		sample = d
		e, err := sourcefacts.ObservationEnvelope([]byte(v.Canonical))
		if err != nil {
			t.Fatal(err)
		}
		documents[d.Profile] = append(documents[d.Profile], e)
	}
	var mu sync.Mutex
	var entered, release chan struct{}
	suffix := "next"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-b-only" {
			t.Error("wrong credential")
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/api/v1/integrations/xinsight/source/v2/capabilities" {
			transports := map[string][]string{}
			for _, p := range sourcefacts.ObservationProfiles() {
				transports[p] = []string{"incremental_events"}
			}
			transports[sourcefacts.RuntimeProfile] = append(transports[sourcefacts.RuntimeProfile], "current_snapshot")
			json.NewEncoder(w).Encode(sourcefacts.Capabilities{SchemaVersion: "unirouter.source-capabilities.v2", SourceInstance: sample.SourceInstance, Profiles: sourcefacts.ObservationProfiles(), ProfileVersion: sourcefacts.ObservationConnectorVersion, Status: "active", CredentialStatus: "feed_active", ScopeGeneration: "1", RetentionSeconds: 604800, Bootstrap: "forward_only", FieldAuthorization: "fixed_profiles", ProfileTransports: transports, EventCoverage: "forward_only", CurrentRuntimeCoverage: "lease_backed_inventory"})
			return
		}
		if r.URL.Path != "/api/v1/integrations/xinsight/source/v2/events" || r.URL.Query().Get("limit") != "50" {
			t.Error("wrong source path or bound")
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		events := append([]json.RawMessage{}, documents[r.URL.Query().Get("profile")]...)
		position := suffix
		started, wait := entered, release
		mu.Unlock()
		if r.URL.Query().Get("cursor") == "" {
			events = []json.RawMessage{}
			position = "start"
		} else if started != nil {
			close(started)
			<-wait
		}
		json.NewEncoder(w).Encode(sourcefacts.Page{BatchVersion: connector.EventBatchVersionV1, ConnectorID: sourcefacts.ConnectorID, ConnectorVersion: sourcefacts.ObservationConnectorVersion, ProducerInstance: sample.SourceInstance, GeneratedAt: sample.RecordedAt, Events: events, NextCursor: position, HighWatermark: position})
	}))
	defer server.Close()
	reader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: server.URL + "/api/v1/integrations/xinsight/source/v2/events", ServiceKey: "synthetic-b-only", AllowPrivateTunnelHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	consumer := sourcefacts.Consumer{Store: f.consumerStore, Reader: reader}
	streams := map[string]sourcefacts.Stream{}
	for _, profile := range sourcefacts.ObservationProfiles() {
		s, err := f.consumerStore.RegisterSourceFactsStream(t.Context(), sourcefacts.Stream{ID: testUUID(t), SourceSystemID: f.source, SourceInstance: sample.SourceInstance, OrgID: sample.OrgID, ProjectID: sample.ProjectID, Profile: profile, ScopeGeneration: "1"})
		if err != nil {
			t.Fatal(err)
		}
		s, err = f.consumerStore.ControlSourceFactsStream(t.Context(), s.ID, "active", s.Revision, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = consumer.PullOnce(t.Context(), s.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = consumer.PullOnce(t.Context(), s.ID); err != nil {
			t.Fatal(err)
		}
		streams[profile] = s
	}
	count := func() int {
		var n int
		if err := f.admin.QueryRow(`SELECT count(*) FROM source_observation_documents`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count() != len(fixture.Valid) {
		t.Fatal("lost observations")
	}
	var aCount, moneyCount int
	if err := f.admin.QueryRow(`SELECT (SELECT count(*) FROM source_fact_documents),(SELECT count(*) FROM workbench_economic_facts)`).Scan(&aCount, &moneyCount); err != nil {
		t.Fatal(err)
	}
	if aCount != aBefore || moneyCount != moneyBefore {
		t.Fatal("B receipt wrote A or money")
	}
	var granted bool
	if err := f.admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM workbench_access_grants WHERE allow_source_runtime OR allow_source_attempt OR allow_source_capability OR allow_source_policy OR allow_source_destination)`).Scan(&granted); err != nil {
		t.Fatal(err)
	}
	if granted {
		t.Fatal("migration granted B fields by default")
	}
	mu.Lock()
	suffix = "replay"
	mu.Unlock()
	for _, s := range streams {
		if _, err := consumer.PullOnce(t.Context(), s.ID); err != nil {
			t.Fatal(err)
		}
	}
	if count() != len(fixture.Valid) {
		t.Fatal("replay duplicated observations")
	}

	// A response which began under active authority cannot commit after pause.
	s, err := f.consumerStore.LoadSourceFactsStream(t.Context(), streams[sourcefacts.RuntimeProfile].ID)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	entered = make(chan struct{})
	release = make(chan struct{})
	suffix = "after-pause"
	started, unblock := entered, release
	mu.Unlock()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := consumer.PullOnce(ctx, s.ID); done <- err }()
	<-started
	paused, err := f.consumerStore.ControlSourceFactsStream(t.Context(), s.ID, "paused", s.Revision, false)
	if err != nil {
		close(unblock)
		t.Fatal(err)
	}
	close(unblock)
	if err := <-done; !errors.Is(err, sourcefacts.ErrStreamChanged) {
		t.Fatalf("late response committed: %v", err)
	}
	live, err := f.consumerStore.LoadSourceFactsStream(t.Context(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if live != paused || live.Cursor != s.Cursor || count() != len(fixture.Valid) {
		t.Fatal("late response changed receipt or cursor")
	}
	mu.Lock()
	entered, release = nil, nil
	mu.Unlock()

	// Same identity with different valid bytes is a source conflict; the failed
	// page must retain its old checkpoint and all prior immutable bytes.
	profile := sourcefacts.DestinationProfile
	s, err = f.consumerStore.LoadSourceFactsStream(t.Context(), streams[profile].ID)
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte{}, documents[profile][0]...)
	item, err := sourcefacts.ValidateObservationItem(before, profile)
	if err != nil {
		t.Fatal(err)
	}
	m, err := connector.DecodeStrictJSONObject(item.SourceBytes)
	if err != nil {
		t.Fatal(err)
	}
	sourcefacts.Object(m, "destination")["origin"] = "https://changed.invalid"
	changed, _ := connector.CanonicalizeJSONValue(m)
	envelope, err := sourcefacts.ObservationEnvelope(changed)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	documents[profile] = []json.RawMessage{envelope}
	suffix = "conflict"
	mu.Unlock()
	if _, err := consumer.PullOnce(t.Context(), s.ID); !errors.Is(err, sourcefacts.ErrConflict) {
		t.Fatal("conflict not rejected", err)
	}
	live, err = f.consumerStore.LoadSourceFactsStream(t.Context(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	var retained []byte
	if err := f.admin.QueryRow(`SELECT envelope_bytes FROM source_observation_documents WHERE source_system_id=$1::uuid AND event_id=$2`, f.source, item.Document.EventID).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if live.Cursor != s.Cursor || !live.RequiresAttention || !bytes.Equal(retained, before) || count() != len(fixture.Valid) {
		t.Fatal("conflicting page mutated immutable evidence or checkpoint")
	}

	// The restore check must work on a read-only transaction with real B bytes.
	tx, err := f.admin.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	err = db.ValidateWorkbenchRestore(t.Context(), tx)
	_ = tx.Rollback()
	if err != nil {
		t.Fatal("valid restore rejected", err)
	}
	for _, tc := range []struct{ name, damage string }{
		{"missing-B-table", `ALTER TABLE source_observation_documents RENAME TO source_observation_documents_missing`},
		{"disabled-immutable-guard", `ALTER TABLE source_observation_documents DISABLE TRIGGER source_observation_documents_immutable`},
		{"disabled-grant-revision", `ALTER TABLE workbench_access_grants DISABLE TRIGGER workbench_observation_grant_revision`},
		{"changed-projection-bytes", `ALTER TABLE source_observation_documents DISABLE TRIGGER source_observation_documents_immutable; UPDATE source_observation_documents SET envelope_bytes=envelope_bytes||decode('20','hex'); ALTER TABLE source_observation_documents ENABLE ALWAYS TRIGGER source_observation_documents_immutable`},
		{"changed-observed-time", `ALTER TABLE source_observation_documents DISABLE TRIGGER source_observation_documents_immutable; UPDATE source_observation_documents SET occurred_at=occurred_at+interval '1 second'; ALTER TABLE source_observation_documents ENABLE ALWAYS TRIGGER source_observation_documents_immutable`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := f.admin.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err = tx.ExecContext(t.Context(), tc.damage); err != nil {
				t.Fatal(err)
			}
			if err = db.ValidateWorkbenchRestore(t.Context(), tx); err == nil {
				t.Fatal("damaged B restore passed")
			}
		})
	}
	if count() != len(fixture.Valid) {
		t.Fatal("restore checks changed source facts")
	}
}

func TestObservationGrantChangesInvalidateCurrentScope(t *testing.T) {
	f := newSourceFixture(t)
	scoped := db.NewScopedWorkchainStore(f.evidence, f.workspace)
	revision := func() string {
		r, err := scoped.CurrentRevision(t.Context(), f.subject, false)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	previous := revision()
	for _, column := range []string{"allow_source_runtime", "allow_source_attempt", "allow_source_capability", "allow_source_policy", "allow_source_destination"} {
		for _, value := range []bool{true, false} {
			var oldGrant, newGrant int64
			if err := f.admin.QueryRow(`SELECT revision FROM workbench_access_grants WHERE subject_id=$1`, f.subject).Scan(&oldGrant); err != nil {
				t.Fatal(err)
			}
			f.exec(`UPDATE workbench_access_grants SET `+column+`=$1 WHERE subject_id=$2`, value, f.subject)
			if err := f.admin.QueryRow(`SELECT revision FROM workbench_access_grants WHERE subject_id=$1`, f.subject).Scan(&newGrant); err != nil {
				t.Fatal(err)
			}
			if newGrant != oldGrant+1 {
				t.Fatal("B permission change did not advance grant revision", column)
			}
			current := revision()
			if current == previous {
				t.Fatal("B permission change reused current scope", column)
			}
			previous = current
		}
	}
}
