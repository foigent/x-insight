package db_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func canonicalResourceFixture(t *testing.T, r workbench.ResourceAccessRecord) []byte {
	t.Helper()
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	v, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = connector.CanonicalizeJSONValue(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestRuntimeResourceRevisionChainAdmissionAndFrozenRestore(t *testing.T) {
	f := newSourceFixture(t)
	gold := runtimeGolden(t)
	known := gold[0]
	var destination sourcefacts.Observation
	for _, d := range gold {
		if d.Destination != nil {
			destination = d
			break
		}
	}
	if destination.Destination == nil {
		t.Fatal("destination fixture absent")
	}
	putRuntimeDocuments(t, f, f.source, known)
	f.exec(`UPDATE workbench_access_grants SET allow_source_runtime=true,allow_resource=true WHERE subject_id=$1`, f.subject)
	now := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
	base := resourceForRuntime(f.source, known, destination)
	base.Input = nil
	badHash := "sha256:" + strings.Repeat("0", 64)
	put := func(t *testing.T, r workbench.ResourceAccessRecord, at time.Time) {
		t.Helper()
		if inserted, err := f.consumerStore.StoreSyntheticResourceRecord(f.ctx, r, at); err != nil || !inserted {
			t.Fatal("resource admission", inserted, err)
		}
	}
	fingerprint := func(t *testing.T) string {
		t.Helper()
		var digest string
		if err := f.admin.QueryRow(`SELECT md5(COALESCE(string_agg(to_jsonb(r)::text,'' ORDER BY to_jsonb(r)::text),'')) FROM resource_access_records r`).Scan(&digest); err != nil {
			t.Fatal(err)
		}
		return digest
	}
	conflict := func(t *testing.T, r workbench.ResourceAccessRecord) {
		t.Helper()
		before := fingerprint(t)
		if inserted, err := f.consumerStore.StoreSyntheticResourceRecord(f.ctx, r, now); inserted || !errors.Is(err, workbench.ErrConflict) {
			t.Fatal("known adjacent hash conflict accepted", inserted, err)
		}
		if before != fingerprint(t) {
			t.Fatal("rejected conflict changed immutable resource history")
		}
	}
	validRestore := func(t *testing.T) {
		t.Helper()
		if err := db.ValidateWorkbenchRestore(f.ctx, f.admin); err != nil {
			t.Fatal("accepted normal resource history is not restorable", err)
		}
	}

	t.Run("correct-chain-and-idempotency", func(t *testing.T) {
		r1 := base
		r1.RecordRef = "correct-chain"
		r2 := r1
		r2.Revision = 2
		r2.PreviousHash = sourcefacts.Hash(canonicalResourceFixture(t, r1))
		put(t, r1, now)
		put(t, r2, now)
		if inserted, err := f.consumerStore.StoreSyntheticResourceRecord(f.ctx, r2, now.Add(time.Second)); inserted || err != nil {
			t.Fatal("identical retry", inserted, err)
		}
		changed := r2
		changed.ActorRef = "changed-actor"
		conflict(t, changed)
		validRestore(t)
	})
	t.Run("known-predecessor-conflict", func(t *testing.T) {
		r1 := base
		r1.RecordRef = "known-predecessor"
		r2 := r1
		r2.Revision = 2
		r2.PreviousHash = badHash
		put(t, r1, now)
		conflict(t, r2)
		r2.PreviousHash = sourcefacts.Hash(canonicalResourceFixture(t, r1))
		put(t, r2, now)
		validRestore(t)
	})
	t.Run("known-successor-conflict-and-matching-fill", func(t *testing.T) {
		r1 := base
		r1.RecordRef = "known-successor"
		matching := r1
		matching.ActorRef = "actual-predecessor"
		r2 := r1
		r2.Revision = 2
		r2.PreviousHash = sourcefacts.Hash(canonicalResourceFixture(t, matching))
		put(t, r2, now)
		validRestore(t)
		conflict(t, r1)
		put(t, matching, now.Add(time.Second))
		validRestore(t)
	})
	t.Run("missing-predecessor-late-fill-preserves-frozen-chain", func(t *testing.T) {
		r1 := base
		r1.RecordRef = "late-predecessor"
		r2 := r1
		r2.Revision = 2
		r2.PreviousHash = sourcefacts.Hash(canonicalResourceFixture(t, r1))
		put(t, r2, now.Add(-time.Second))
		makeReading := func(at time.Time) workbench.RuntimeReading {
			r, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, runtimeRequestInput(t, known), at)
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
		chain := func(r workbench.RuntimeReading) workbench.RuntimeResourceItem {
			requests, err := f.workspace.GetRuntimeRequests(f.ctx, f.subject, r.ID, "all", 10, "", now)
			if err != nil || len(requests.Items) != 1 {
				t.Fatal(requests, err)
			}
			items, err := f.workspace.GetRuntimeResources(f.ctx, f.subject, r.ID, requests.Items[0].Ref, 50, "", now)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range items.Items {
				if item.Record.RecordRef == r1.RecordRef {
					return item
				}
			}
			t.Fatal("frozen resource absent")
			return workbench.RuntimeResourceItem{}
		}
		old := makeReading(now)
		before := chain(old)
		if before.ChainState != "missing_predecessor" {
			t.Fatal("genuine gap not recorded", before)
		}
		validRestore(t)
		put(t, r1, now.Add(time.Second))
		after := chain(old)
		if after.ChainState != "missing_predecessor" || after.RecordHash != before.RecordHash {
			t.Fatal("late fill rewrote frozen chain", after)
		}
		if latest := chain(makeReading(now.Add(2 * time.Second))); latest.ChainState != "complete" {
			t.Fatal("new reading did not resolve complete chain", latest)
		}
		validRestore(t)
	})
	t.Run("full-reporter-identity", func(t *testing.T) {
		r1 := base
		r1.RecordRef = "separate-reporter"
		foreign := r1
		foreign.Reporter.OrgID = "different-org"
		foreign.Revision = 2
		foreign.PreviousHash = badHash
		put(t, foreign, now)
		put(t, r1, now)
		validRestore(t)
	})
	t.Run("concurrent-conflicting-neighbours", func(t *testing.T) {
		r1 := base
		r1.RecordRef = "concurrent-neighbours"
		r2 := r1
		r2.Revision = 2
		r2.PreviousHash = badHash
		start := make(chan struct{})
		results := make(chan error, 2)
		for _, r := range []workbench.ResourceAccessRecord{r1, r2} {
			go func(r workbench.ResourceAccessRecord) {
				<-start
				_, err := f.consumerStore.StoreSyntheticResourceRecord(f.ctx, r, now)
				results <- err
			}(r)
		}
		close(start)
		a, b := <-results, <-results
		if !((a == nil && errors.Is(b, workbench.ErrConflict)) || (b == nil && errors.Is(a, workbench.ErrConflict))) {
			t.Fatal("concurrent conflicting chain committed", a, b)
		}
		validRestore(t)
	})
	t.Run("deliberately-damaged-fixture-still-rejected", func(t *testing.T) {
		validRestore(t)
		r1 := base
		r1.RecordRef = "deliberate-corruption"
		put(t, r1, now)
		r2 := r1
		r2.Revision = 2
		r2.PreviousHash = badHash
		raw := canonicalResourceFixture(t, r2)
		// Explicit privileged corruption fixture only. The normal domain writer
		// must never be used to manufacture a database that cannot be restored.
		f.exec(`INSERT INTO resource_access_records(source_system_id,record_ref,revision,previous_hash,record_hash,source_instance,org_id,project_id,target_source_system_id,target_source_instance,target_org_id,target_project_id,request_id,occurred_at,received_at,record_bytes,record_json)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17::jsonb)`, r2.Reporter.SourceSystemID, r2.RecordRef, r2.Revision, r2.PreviousHash, sourcefacts.Hash(raw), r2.Reporter.SourceInstance, r2.Reporter.OrgID, r2.Reporter.ProjectID, r2.Target.SourceSystemID, r2.Target.SourceInstance, r2.Target.OrgID, r2.Target.ProjectID, r2.RequestID, r2.OccurredAt.UTC().Truncate(time.Microsecond), now, raw, string(raw))
		if err := db.ValidateWorkbenchRestore(f.ctx, f.admin); err == nil || !strings.Contains(err.Error(), "resource") {
			t.Fatal("restore accepted deliberately damaged chain", err)
		}
	})
}
