package sourcefacts

import (
	"context"
	"errors"
	"slices"
	"time"
)

func ValidRegistration(s Stream) bool {
	return uuidPattern.MatchString(s.ID) && uuidPattern.MatchString(s.SourceSystemID) && boundedText(s.SourceInstance, 128) && Positive(s.OrgID) && Positive(s.ProjectID) && (ValidProfile(s.Profile) || ValidObservationProfile(s.Profile)) && Positive(s.ScopeGeneration)
}

func SameRegistration(a, b Stream) bool {
	return a.ID == b.ID && a.SourceSystemID == b.SourceSystemID && a.SourceInstance == b.SourceInstance && a.OrgID == b.OrgID && a.ProjectID == b.ProjectID && a.Profile == b.Profile && a.ScopeGeneration == b.ScopeGeneration
}

// Preflight reads capabilities only. It cannot register, activate or pull facts.
func Preflight(ctx context.Context, reader HTTPReader, s Stream) error {
	if reader == nil || !ValidRegistration(s) {
		return ErrInvalid
	}
	raw, err := reader.FetchSourceCapabilities(ctx)
	if err != nil {
		return err
	}
	c, err := decodeStreamCapabilities(raw, s.Profile)
	if err != nil {
		return err
	}
	if c.SourceInstance != s.SourceInstance || c.ScopeGeneration != s.ScopeGeneration || !slices.Contains(c.Profiles, s.Profile) {
		return ErrScopeChanged
	}
	if c.Status != "active" || c.CredentialStatus != "feed_active" {
		return ErrUnavailable
	}
	return nil
}

type ManagedProfile struct {
	StreamID              string
	Consumer              Consumer
	CollectCurrentRuntime bool
}
type ManagedWorker struct {
	ConfigVersion string
	Profiles      []ManagedProfile
	Budgets       []BudgetConsumer
	Contents      []ContentConsumer
	PollInterval  time.Duration
	RetryMaximum  time.Duration
	Now           func() time.Time
	Wait          func(context.Context, time.Duration) error
	Observe       func(string, error)
}

// Run rotates after each page. Per-profile backoff and attention checks prevent
// a busy or failed profile from monopolizing the worker or reviving on restart.
func (w ManagedWorker) Run(ctx context.Context) error {
	maximum := 3
	if w.ConfigVersion == ManagedObservationConfigVersion || w.ConfigVersion == ManagedBudgetConfigVersion || w.ConfigVersion == ManagedContentConfigVersion {
		maximum = 16
	} else if w.ConfigVersion != "" && w.ConfigVersion != ManagedConfigVersion {
		return ErrInvalid
	}
	if len(w.Profiles)+len(w.Budgets)+len(w.Contents) == 0 || len(w.Profiles)+len(w.Budgets)+len(w.Contents) > maximum || (len(w.Budgets) > 0 && w.ConfigVersion != ManagedBudgetConfigVersion && w.ConfigVersion != ManagedContentConfigVersion) || (len(w.Contents) > 0 && w.ConfigVersion != ManagedContentConfigVersion) || w.PollInterval < time.Second || w.RetryMaximum < w.PollInterval || w.RetryMaximum > time.Hour {
		return ErrInvalid
	}
	seen := map[string]bool{}
	type poll struct {
		id  string
		run func(context.Context) (bool, error)
	}
	jobs := make([]poll, 0, len(w.Profiles)+len(w.Budgets)+len(w.Contents))
	for _, p := range w.Profiles {
		if !uuidPattern.MatchString(p.StreamID) || seen[p.StreamID] || p.Consumer.Store == nil || p.Consumer.Reader == nil {
			return ErrInvalid
		}
		seen[p.StreamID] = true
		jobs = append(jobs, poll{p.StreamID, func(ctx context.Context) (bool, error) {
			page, err := p.Consumer.PullOnce(ctx, p.StreamID)
			more := page.HasMore
			if p.CollectCurrentRuntime {
				current, currentErr := p.Consumer.PullCurrentRuntimeOnce(ctx, p.StreamID)
				more = more || current.HasMore
				if err == nil {
					err = currentErr
				}
				// Preserve independent inventory progress after an event gap.
				if currentErr == nil && current.HasMore {
					return true, err
				}
			}
			return err == nil && more, err
		}})
	}
	for _, b := range w.Budgets {
		if !ValidBudgetCollection(b.Expected) || seen[b.Expected.ID] || b.Store == nil || b.Reader == nil {
			return ErrInvalid
		}
		seen[b.Expected.ID] = true
		jobs = append(jobs, poll{b.Expected.ID, b.PullOnce})
	}
	for _, c := range w.Contents {
		if !ValidContentCollection(c.Expected) || seen[c.Expected.ID] || c.Store == nil || c.Reader == nil {
			return ErrInvalid
		}
		seen[c.Expected.ID] = true
		jobs = append(jobs, poll{c.Expected.ID, c.PullOnce})
	}
	now := w.Now
	if now == nil {
		now = time.Now
	}
	wait := w.Wait
	if wait == nil {
		wait = func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		}
	}
	due := make([]time.Time, len(jobs))
	failures := make([]int, len(jobs))
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		for i, p := range jobs {
			if err := ctx.Err(); err != nil {
				return err
			}
			if now().Before(due[i]) {
				continue
			}
			more, err := p.run(ctx)
			if w.Observe != nil {
				w.Observe(p.id, err)
			}
			delay := w.PollInterval
			switch {
			case err == nil:
				failures[i] = 0
				if more {
					delay = 0
				}
			case errors.Is(err, ErrUnavailable), errors.Is(err, ErrStreamChanged), errors.Is(err, ErrScopeChanged), errors.Is(err, ErrConflict), errors.Is(err, ErrInvalid):
				failures[i] = 0
			default:
				failures[i]++
				for j := 1; j < failures[i] && delay < w.RetryMaximum; j++ {
					delay *= 2
					if delay > w.RetryMaximum {
						delay = w.RetryMaximum
					}
				}
			}
			if more && err != nil && i < len(w.Profiles) && w.Profiles[i].CollectCurrentRuntime {
				delay = 0
			}
			due[i] = now().Add(delay)
		}
		earliest := due[0]
		for _, d := range due[1:] {
			if d.Before(earliest) {
				earliest = d
			}
		}
		delay := earliest.Sub(now())
		if delay > 0 {
			if err := wait(ctx, delay); err != nil {
				return err
			}
		}
	}
}
