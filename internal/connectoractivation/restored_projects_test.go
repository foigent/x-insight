package connectoractivation

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

type restoreStore struct {
	calls []string
	fail  string
}

func (s *restoreStore) check(id string, revision int64, acknowledge bool) error {
	if revision != 7 || !acknowledge {
		return errors.New("CAS/ack not retained")
	}
	s.calls = append(s.calls, id)
	if id == s.fail {
		return sourcefacts.ErrStreamChanged
	}
	return nil
}
func (s *restoreStore) ControlSourceFactsStream(_ context.Context, id, status string, rev int64, ack bool) (sourcefacts.Stream, error) {
	return sourcefacts.Stream{}, s.check(id, rev, ack)
}
func (s *restoreStore) ControlBudgetCollection(_ context.Context, id, status string, rev int64, ack bool) (sourcefacts.BudgetCollection, error) {
	return sourcefacts.BudgetCollection{}, s.check(id, rev, ack)
}
func (s *restoreStore) ControlContentCollection(_ context.Context, id, status string, rev int64, ack bool, expected ...sourcefacts.ContentCollection) (sourcefacts.ContentCollection, error) {
	if len(expected) != 1 || expected[0].ID != id || expected[0].Cursor != "original-cursor" || expected[0].RecoveryEpoch != "4" {
		return sourcefacts.ContentCollection{}, errors.New("exact content preflight snapshot lost")
	}
	return sourcefacts.ContentCollection{}, s.check(id, rev, ack)
}
func restoredPlan() originalResume {
	return originalResume{Streams: []sourcefacts.Stream{{ID: "daily-project86", Status: "paused", RequiresAttention: true, LastError: "restore_requires_authorization", Revision: 7, Cursor: "old-cursor", HighWatermark: "old-high"}}, Contents: []sourcefacts.ContentCollection{{ID: "daily-content86", Status: "paused", RequiresAttention: true, LastError: "restore_requires_authorization", Revision: 7, Cursor: "original-cursor", RecoveryEpoch: "4"}}}
}
func TestOriginalRestoreRequiresFreshAuthorityAndCustodyBeforeExactCAS(t *testing.T) {
	for _, scenario := range []string{"success", "initial_denial", "custody_pending", "revocation_after_start", "first_CAS_conflict"} {
		t.Run(scenario, func(t *testing.T) {
			store := &restoreStore{}
			events := []string{}
			count := 0
			authorize := func() error {
				events = append(events, "authorize")
				count++
				if scenario == "initial_denial" || scenario == "revocation_after_start" && count == 2 {
					return sourcefacts.ErrAuthorization
				}
				return nil
			}
			start := func() error { events = append(events, "start"); return nil }
			ready := func() error {
				events = append(events, "ready")
				if scenario == "custody_pending" {
					return ErrOriginalReadinessPending
				}
				return nil
			}
			if scenario == "first_CAS_conflict" {
				store.fail = "daily-project86"
			}
			plan := restoredPlan()
			before := plan.Streams[0]
			err := resumeOriginalRegistrations(t.Context(), store, plan, authorize, start, ready)
			if scenario == "success" {
				if err != nil || !reflect.DeepEqual(store.calls, []string{"daily-project86", "daily-content86"}) || !reflect.DeepEqual(events, []string{"authorize", "start", "ready", "authorize"}) {
					t.Fatal(err, store.calls, events)
				}
			} else if scenario == "first_CAS_conflict" {
				if !errors.Is(err, sourcefacts.ErrStreamChanged) || !reflect.DeepEqual(store.calls, []string{"daily-project86"}) {
					t.Fatal("CAS failure did not stop content activation", err, store.calls)
				}
			} else if err == nil || len(store.calls) != 0 {
				t.Fatal("failed readiness/authority resumed a collection", err, store.calls)
			}
			if plan.Streams[0] != before {
				t.Fatal("original snapshot checkpoint was rewritten")
			}
		})
	}
}
func TestOriginalRestoreRejectsOtherAttentionAndOrdinaryPauseBeforeStartup(t *testing.T) {
	for _, state := range []struct {
		status, reason string
		attention      bool
	}{{"paused", "", false}, {"paused", "scope_changed", true}, {"revoked", "restore_requires_authorization", true}, {"active", "source_authorization_denied", true}} {
		plan := restoredPlan()
		plan.Streams[0].Status = state.status
		plan.Streams[0].LastError = state.reason
		plan.Streams[0].RequiresAttention = state.attention
		store := &restoreStore{}
		called := false
		stage := func() error { called = true; return nil }
		err := resumeOriginalRegistrations(t.Context(), store, plan, stage, stage, stage)
		if !errors.Is(err, sourcefacts.ErrScopeChanged) || called || len(store.calls) != 0 {
			t.Fatal("unapproved prestate had effects", state, err)
		}
	}
}
