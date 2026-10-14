package db

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func TestRuntimeContentRouteSelectorNeverOverridesFrozenRepresentation(t *testing.T) {
	ref := sourcefacts.FrozenManifestRef{SourceSystemID: "source-a", SourceInstance: "instance-a", OrgID: "7", ProjectID: "9", RequestID: "request", SourceFactID: "fact", Revision: "1", ContentRef: "content", SourceSHA256: "hash", CaptureStatus: "complete", ExpiresAt: "expiry", Representation: &sourcefacts.ContentRepresentation{MediaType: "application/octet-stream", SavedBytes: 0, Digest: "digest"}}
	selector := sourcefacts.FrozenManifestRef{SourceFactID: ref.SourceFactID, Revision: ref.Revision, ContentRef: ref.ContentRef}
	if !contentSelectorMatches(selector, ref) || !contentSelectorMatches(ref, ref) {
		t.Fatal("exact route/full reference denied")
	}
	changes := []func(*sourcefacts.FrozenManifestRef){
		func(r *sourcefacts.FrozenManifestRef) { r.SourceSystemID = "other" }, func(r *sourcefacts.FrozenManifestRef) { r.SourceInstance = "other" }, func(r *sourcefacts.FrozenManifestRef) { r.OrgID = "other" }, func(r *sourcefacts.FrozenManifestRef) { r.ProjectID = "other" }, func(r *sourcefacts.FrozenManifestRef) { r.RequestID = "other" }, func(r *sourcefacts.FrozenManifestRef) { r.SourceFactID = "other" }, func(r *sourcefacts.FrozenManifestRef) { r.Revision = "2" }, func(r *sourcefacts.FrozenManifestRef) { r.ContentRef = "other" }, func(r *sourcefacts.FrozenManifestRef) { r.SourceSHA256 = "other" }, func(r *sourcefacts.FrozenManifestRef) { r.CaptureStatus = "partial" }, func(r *sourcefacts.FrozenManifestRef) { r.ExpiresAt = "later" }, func(r *sourcefacts.FrozenManifestRef) {
			r.Representation = &sourcefacts.ContentRepresentation{MediaType: "text/html", SavedBytes: 1, Digest: "other"}
		},
	}
	for i, change := range changes {
		other := selector
		change(&other)
		if contentSelectorMatches(other, ref) {
			t.Fatalf("selector override %d accepted", i)
		}
	}
}

func TestRuntimeContentEmptyExtensionsPreserveWireBytes(t *testing.T) {

	// All new extension fields are absent, not null or empty markers.
	raw, err := json.Marshal(workbench.RuntimeReading{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "content_") {
		t.Fatalf("empty reading extension contributes bytes: %s", raw)
	}
	q := workbench.RuntimeRequest{Ref: "q", SourceSystemID: "s", SourceInstance: "i", OrgID: "7", ProjectID: "9", RequestID: "r", EntryTimeState: "unknown", MemberBasis: "content_eligible"}
	raw, err = json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "last_observed_at") || strings.Contains(string(raw), "entry_at") {
		t.Fatal("C-only invented B timestamp", string(raw))
	}
	oldRequest := workbench.RuntimeRequest{Ref: "q", SourceSystemID: "s", SourceInstance: "i", OrgID: "7", ProjectID: "9", RequestID: "r", EntryTimeState: "unknown", LastObservedAt: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC), ObservationCount: 1}
	raw, err = json.Marshal(oldRequest)
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"request_ref":"q","source_system_id":"s","source_instance":"i","org_id":"7","project_id":"9","request_id":"r","entry_time_state":"unknown","last_observed_at":"2026-09-15T00:00:00Z","observation_count":1}`
	if string(raw) != expected {
		t.Fatalf("old B request bytes changed: %s", raw)
	}
	if runtimeManifestDigestSQL != runtimeManifestDigestPrefixSQL+`||`+runtimeResourceManifestSQL+runtimeManifestDigestSuffixSQL {
		t.Fatal("old digest changed")
	}
	if digestText(runtimeInterpretationWithContent) == digestText(runtimeInterpretation) || workbench.ContentInterpretationRevision == "" {
		t.Fatal("content interpretation collides")
	}
}

func TestRuntimeContentReceiptInterpretationPreservesLegacy(t *testing.T) {
	if runtimeContentInterpretation(false) != digestText(runtimeInterpretationWithContent) {
		t.Fatal("reading without receipts changed interpretation")
	}
	if runtimeContentInterpretation(true) == runtimeContentInterpretation(false) {
		t.Fatal("receipt membership failed to bind its interpretation")
	}
	// The extension wraps the exact prior C digest inputs; its absent branch adds
	// zero bytes rather than an empty-receipt marker to historical readings.
	if runtimeManifestDigestWithContentReceiptsSQL != runtimeManifestDigestPrefixSQL+`||`+runtimeResourceManifestSQL+`||`+runtimeContentManifestSQL+`||`+runtimeContentReceiptManifestSQL+runtimeManifestDigestSuffixSQL || !strings.HasSuffix(runtimeContentReceiptManifestSQL, " ELSE '' END") {
		t.Fatal("receipt digest changed historical empty-extension contract")
	}
}

func TestRuntimeContentObservationBindsCanonicalFrozenManifest(t *testing.T) {
	_, state, page, canonical := receiptFixture(t)
	base := page.Items[0].Document
	for _, point := range []string{"client_request", "provider_request", "provider_response", "client_response", "client_request_explicit_attempt"} {
		t.Run(point, func(t *testing.T) {
			m := base
			m.Protocol = "openai.chat_completions.v1"
			m.ObservationPoint = point
			m.ContentItemID = "body-0"
			if point == "client_request_explicit_attempt" {
				m.ObservationPoint = "client_request"
				m.AttemptID = "explicit-client-attempt"
			}
			if strings.HasPrefix(point, "provider_") {
				m.AttemptID = "actual-attempt-2"
			}
			m.SourceFactID = sourcefacts.SourceID(sourcefacts.ContentManifestVersion, m.SourceInstance, m.OrgID, m.ProjectID, m.Profile, m.RequestID, m.ObservationPoint, m.AttemptID, m.ContentItemID)
			m.EventID = sourcefacts.SourceID(sourcefacts.ContentManifestVersion+".revision", m.SourceFactID, m.Revision)
			raw := canonical(m)
			hash := sourcefacts.Hash(raw)
			ref, err := sourcefacts.FrozenContentManifestRef(state.collection.SourceSystemID, sourcefacts.ContentManifestEntry{Document: m, SourceSHA256: hash})
			if err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(ref)
			got, err := runtimeContentObservation(ref, raw, hash)
			if err != nil || got.Protocol != m.Protocol || got.ObservationPoint != m.ObservationPoint || got.AttemptID != m.AttemptID || got.ContentItemID != m.ContentItemID || got.OccurredAt != m.OccurredAt || got.CaptureReason != m.Capture.Reason {
				t.Fatalf("observation %+v: %v", got, err)
			}
			after, _ := json.Marshal(ref)
			if string(before) != string(after) {
				t.Fatal("projection modified frozen reference")
			}
			for _, which := range []string{"raw", "hash", "scope", "revision", "status", "representation"} {
				t.Run(which, func(t *testing.T) {
					badRef, badRaw, badHash := ref, raw, hash
					switch which {
					case "raw":
						badRaw = append(append([]byte{}, raw...), ' ')
					case "hash":
						badHash = sourcefacts.Hash(nil)
					case "scope":
						badRef.ProjectID = "88"
					case "revision":
						badRef.Revision = "2"
					case "status":
						badRef.CaptureStatus = "pending"
					case "representation":
						badRef.ContentRef = "uc1_" + strings.Repeat("a", 64)
					}
					if got, err := runtimeContentObservation(badRef, badRaw, badHash); err != workbench.ErrContentIntegrity || got != nil {
						t.Fatalf("mismatch accepted: %+v %v", got, err)
					}
				})
			}
			// A valid hash alone cannot make malformed canonical data authoritative.
			malformed := []byte(`{"protocol":"openai.chat_completions.v1"}`)
			badRef := ref
			badRef.SourceSHA256 = sourcefacts.Hash(malformed)
			if _, err := runtimeContentObservation(badRef, malformed, badRef.SourceSHA256); err != workbench.ErrContentIntegrity {
				t.Fatal("invalid manifest accepted", err)
			}
		})
	}
}

func TestRuntimeContentObservationOmissionPreservesLegacyItemBytes(t *testing.T) {
	ref := sourcefacts.FrozenManifestRef{SourceFactID: "old", Revision: "1"}
	old := struct {
		Reference     sourcefacts.FrozenManifestRef   `json:"reference"`
		Availability  sourcefacts.ContentAvailability `json:"availability"`
		NewerRevision bool                            `json:"newer_revision"`
	}{Reference: ref}
	expected, _ := json.Marshal(old)
	actual, _ := json.Marshal(workbench.RuntimeContentItem{Reference: ref})
	if string(expected) != string(actual) {
		t.Fatalf("legacy item serialization changed: %s", actual)
	}
}
