package workspace

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestQueueStateAndTimeSemantics(t *testing.T) {
	at := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	before := at.Add(-time.Second)
	a := Assessment{}
	f := Followup{}
	f.DueAt = &before
	if !FindingIsOverdue(a, f, at) || FindingProgressState(a) != "unrecorded" {
		t.Fatal("deadline without progress omitted")
	}
	f.DueAt = &at
	if FindingIsOverdue(a, f, at) {
		t.Fatal("equal deadline overdue")
	}
	f.DueAt = &before
	a.Progress = &ProgressRecord{ProgressPayload: ProgressPayload{State: "ended"}}
	if FindingIsOverdue(a, f, at) {
		t.Fatal("ended is overdue")
	}
	for _, tc := range []struct{ review, progress, want string }{{"unrecorded", "unrecorded", "unrecorded"}, {"unchanged", "unrecorded", "unchanged"}, {"unchanged", "changed", "changed"}, {"changed", "unavailable", "unavailable"}} {
		if FindingBasisState(BasisEvaluation{State: tc.review}, BasisEvaluation{State: tc.progress}) != tc.want {
			t.Fatal(tc)
		}
	}
	old := &ReviewBasis{Digest: "old"}
	for _, tc := range []struct {
		digest string
		err    error
		want   string
	}{{"old", nil, "unchanged"}, {"new", nil, "changed"}, {"", ErrNotFound, "unavailable"}, {"", ErrInvalid, "unavailable"}} {
		v, e := EvaluateReviewBasis(old, ReviewBasis{Digest: tc.digest}, tc.err)
		if e != nil || v.State != tc.want {
			t.Fatal(v, e)
		}
	}
	if _, err := EvaluateReviewBasis(old, ReviewBasis{}, errors.New("database unavailable")); err == nil {
		t.Fatal("database failure suppressed")
	}
}
func TestQueueFiltersStrictAndWindow(t *testing.T) {
	at := time.Now().UTC()
	from := at.Add(-time.Hour)
	to := at.Add(time.Hour)
	for _, f := range []QueueFilters{{Mode: "bad"}, {Period: "bad"}, {Period: "custom"}, {From: &from}, {Sort: "x"}, {Assignee: "service:one"}, {Progress: "closed"}, {Conclusion: "resolved"}, {Basis: "fresh"}, {CaseStatus: "ended"}, {Search: "\x00"}} {
		if _, e := NormalizeQueueFilters(f); e == nil {
			t.Fatalf("accepted %#v", f)
		}
	}
	f, e := NormalizeQueueFilters(QueueFilters{Period: "custom", From: &from, To: &to})
	if e != nil {
		t.Fatal(e)
	}
	a, b := f.Window(at)
	if !a.Equal(from) || !b.Equal(to) {
		t.Fatal("custom period")
	}
	f, _ = NormalizeQueueFilters(QueueFilters{Period: "7d"})
	a, b = f.Window(at)
	if b.Sub(a) != 7*24*time.Hour {
		t.Fatal("relative period")
	}
	for _, raw := range []string{`{"mode":"current","period":"24h","sort":"created_desc","unknown":1}`, `{"mode":"current"}`} {
		if json.Unmarshal([]byte(raw), &f) == nil {
			t.Fatal("loose filter input")
		}
	}
}
func TestQueueFilterRevisionAndOwnership(t *testing.T) {
	now := time.Now()
	owner := "xs_1111111111111111"
	id := "xqf_abcdefghijklmnopqrstuv"
	c := QueueFilterCommand{OperationID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Schema: QueueFilterSchema, Name: " My queue ", Filters: QueueFilters{}}
	v, e := EvolveQueueFilter(nil, owner, id, "create", c, now)
	if e != nil || v.Name != "My queue" || v.Revision != 1 {
		t.Fatal(v, e)
	}
	c.ExpectedRevision = 1
	c.Name = "Revised"
	v2, e := EvolveQueueFilter(&v, owner, id, "update", c, now.Add(time.Second))
	if e != nil || v2.Revision != 2 {
		t.Fatal(e)
	}
	if _, e = EvolveQueueFilter(&v2, owner, id, "update", c, now); !errors.Is(e, ErrConflict) {
		t.Fatal("stale update")
	}
	c.ExpectedRevision = 2
	if _, e = EvolveQueueFilter(&v2, "xs_2222222222222222", id, "delete", c, now); !errors.Is(e, ErrNotFound) {
		t.Fatal("cross-owner access")
	}
	v3, e := EvolveQueueFilter(&v2, owner, id, "delete", c, now)
	if e != nil || !v3.Deleted || v3.Revision != 3 || v3.Name != "Revised" {
		t.Fatal(v3, e)
	}
	c.ExpectedRevision = 3
	if _, e = EvolveQueueFilter(&v3, owner, id, "update", c, now); !errors.Is(e, ErrConflict) {
		t.Fatal("resurrected tombstone")
	}
}
