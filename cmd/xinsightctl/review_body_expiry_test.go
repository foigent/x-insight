package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type expiryFixture struct {
	calls atomic.Int64
	err   error
}

func (f *expiryFixture) ExpireReviewBodies(context.Context, time.Time, int) (int, error) {
	f.calls.Add(1)
	return 0, f.err
}

func TestControllerRunsReviewBodyExpiryOnStartAndAfterInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fixture := &expiryFixture{}
	done := make(chan error, 1)
	go func() {
		done <- serveControllerWithReviewBodyExpiry(ctx, func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		}, fixture, 5*time.Millisecond, time.Now, func(error) { t.Error("unexpected expiry failure") })
	}()
	deadline := time.After(time.Second)
	for fixture.calls.Load() < 2 {
		select {
		case <-deadline:
			t.Fatal("controller did not repeat bounded expiry")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestControllerReportsExpiryFailureAndKeepsSocketAvailable(t *testing.T) {
	fixture := &expiryFixture{err: errors.New("synthetic expiry failure")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var reports atomic.Int64
	done := make(chan error, 1)
	go func() {
		done <- serveControllerWithReviewBodyExpiry(ctx, func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		}, fixture, 5*time.Millisecond, time.Now, func(error) { reports.Add(1) })
	}()
	deadline := time.After(time.Second)
	// A started sweep does not imply its error has reached the report callback.
	for reports.Load() < 2 {
		select {
		case <-done:
			t.Fatal("controller socket stopped after a recoverable expiry failure")
		case <-deadline:
			t.Fatalf("expiry did not retry and report on the existing cycle: calls=%d reports=%d", fixture.calls.Load(), reports.Load())
		case <-time.After(time.Millisecond):
		}
	}
	if fixture.calls.Load() < 2 {
		t.Fatalf("expiry did not repeat before reporting: %d", fixture.calls.Load())
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestControllerRunsCustodySweepAfterEnrollmentRefreshFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var refreshed, swept, reported atomic.Int64
	done := make(chan error, 1)
	go func() {
		done <- serveControllerWithReviewBodyExpiry(ctx, func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		}, nil, 5*time.Millisecond, time.Now, func(error) { reported.Add(1) },
			func(context.Context) error {
				refreshed.Add(1)
				return errors.New("synthetic enrollment source unavailable")
			},
			func(context.Context) error {
				swept.Add(1)
				return nil
			})
	}()
	deadline := time.After(time.Second)
	for swept.Load() < 2 || reported.Load() < 2 {
		select {
		case err := <-done:
			t.Fatalf("controller stopped after enrollment refresh failure: %v", err)
		case <-deadline:
			t.Fatalf("custody sweep did not continue: refresh=%d sweep=%d reports=%d", refreshed.Load(), swept.Load(), reported.Load())
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
