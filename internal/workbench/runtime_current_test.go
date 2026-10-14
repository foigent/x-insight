package workbench

import (
	"github.com/foigent/x-insight/internal/sourcefacts"
	"testing"
	"time"
)

func TestRuntimeCurrentLeaseIsNotContinuousConnectionOrCompletion(t *testing.T) {
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	observed, received := now.Add(-2*time.Second), now.Add(-time.Second)
	entry := sourcefacts.CurrentRuntimeEntry{Freshness: "lease_valid_execution", LastObservedAt: observed.Format(sourcefacts.TimeFormat), ValidUntil: now.Add(time.Second).Format(sourcefacts.TimeFormat)}
	for _, tc := range []struct {
		name  string
		at    time.Time
		ready bool
		want  string
	}{
		{"valid lease", now, true, "lease_valid_execution"},
		{"known disconnected", now, false, "not_current"},
		{"expiry equality", now.Add(time.Second), true, "not_current"},
		{"expired", now.Add(2 * time.Second), true, "not_current"},
		{"future receipt", observed, true, "not_current"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := RuntimeCurrentState(entry, observed, received, tc.at, tc.ready); got != tc.want {
				t.Fatal(got, tc.want)
			}
		})
	}
	pending := entry
	pending.Freshness = "leased_pending"
	if RuntimeCurrentState(pending, observed, received, now, true) != "leased_pending" {
		t.Fatal("pending promoted to execution")
	}
	if RuntimeCurrentState(entry, now.Add(time.Minute), received, now, true) != "not_current" {
		t.Fatal("future observation trusted")
	}
	later := entry
	later.LastObservedAt = received.Format(sourcefacts.TimeFormat)
	if RuntimeCurrentState(later, observed, received, now, true) != "not_current" {
		t.Fatal("future last observation trusted")
	}
	last := sourcefacts.CurrentRuntimeEntry{Freshness: "last_known"}
	if RuntimeCurrentState(last, observed, received, now, true) != "last_known" {
		t.Fatal("historical state promoted")
	}
}
