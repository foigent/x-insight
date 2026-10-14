package sourcefacts

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/foigent/x-insight/internal/connector"
)

// Stream registration is a separate, explicitly scoped collection grant. A
// scope change creates a new registration; existing checkpoints are not reset.
type Stream struct {
	ID                string `json:"id"`
	SourceSystemID    string `json:"source_system_id"`
	SourceInstance    string `json:"source_instance"`
	OrgID             string `json:"org_id"`
	ProjectID         string `json:"project_id"`
	Profile           string `json:"profile"`
	ScopeGeneration   string `json:"scope_generation"`
	Status            string `json:"status"`
	Cursor            string `json:"cursor"`
	HighWatermark     string `json:"high_watermark"`
	Revision          int64  `json:"revision"`
	RequiresAttention bool   `json:"requires_attention"`
	LastError         string `json:"last_error"`
}

type Store interface {
	LoadSourceFactsStream(context.Context, string) (Stream, error)
	CommitSourceFactsPage(context.Context, Stream, Page, time.Time) error
	RecordSourceFactsFailure(context.Context, Stream, string, time.Time) error
}

type HTTPReader interface {
	FetchSourceCapabilities(context.Context) ([]byte, error)
	FetchSourceFacts(context.Context, string, string, int) ([]byte, error)
}

type Capabilities struct {
	SchemaVersion          string              `json:"schema_version"`
	SourceInstance         string              `json:"source_instance"`
	Profiles               []string            `json:"profiles"`
	ProfileVersion         string              `json:"profile_version"`
	Status                 string              `json:"status"`
	CredentialStatus       string              `json:"credential_status"`
	ScopeGeneration        string              `json:"scope_generation"`
	RetentionSeconds       int64               `json:"retention_seconds"`
	Bootstrap              string              `json:"bootstrap"`
	TimelineComplete       bool                `json:"timeline_complete"`
	FieldAuthorization     string              `json:"field_authorization"`
	ProfileTransports      map[string][]string `json:"profile_transports,omitempty"`
	EventCoverage          string              `json:"event_coverage,omitempty"`
	CurrentRuntimeCoverage string              `json:"current_runtime_coverage,omitempty"`
}

func DecodeCapabilities(raw []byte) (Capabilities, error) {
	return decodeCapabilities(raw, false)
}

func DecodeObservationCapabilities(raw []byte) (Capabilities, error) {
	return decodeCapabilities(raw, true)
}

func decodeCapabilities(raw []byte, observations bool) (Capabilities, error) {
	var c Capabilities
	keys := []string{"schema_version", "source_instance", "profiles", "profile_version", "status", "credential_status", "scope_generation", "retention_seconds", "bootstrap", "timeline_complete", "field_authorization"}
	version, schema := ConnectorVersion, "unirouter.source-capabilities.v1"
	if observations {
		keys = append(keys, "profile_transports", "event_coverage", "current_runtime_coverage")
		version, schema = ObservationConnectorVersion, "unirouter.source-capabilities.v2"
	}
	m, e := connector.DecodeStrictJSONObject(raw)
	if e != nil || len(m) != len(keys) || !validValues(m, "") {
		return c, ErrInvalid
	}
	for _, key := range keys {
		if _, ok := m[key]; !ok {
			return c, ErrInvalid
		}
	}
	if json.Unmarshal(raw, &c) != nil || c.SchemaVersion != schema || c.SourceInstance == "" || c.ProfileVersion != version || !Positive(c.ScopeGeneration) || c.RetentionSeconds != 604800 || c.Bootstrap != "forward_only" || c.TimelineComplete || c.FieldAuthorization != "fixed_profiles" || c.Profiles == nil || !slices.Contains([]string{"active", "paused"}, c.Status) || !slices.Contains([]string{"feed_active", "feed_staged"}, c.CredentialStatus) {
		return c, ErrInvalid
	}
	if observations && (c.ProfileTransports == nil || len(c.ProfileTransports) != len(c.Profiles) || c.EventCoverage != "forward_only" || c.CurrentRuntimeCoverage != "lease_backed_inventory") {
		return c, ErrInvalid
	}
	seen := map[string]bool{}
	for _, p := range c.Profiles {
		if seen[p] {
			return c, ErrInvalid
		}
		if observations {
			transports := []string{"incremental_events"}
			switch p {
			case RuntimeProfile:
				transports = append(transports, "current_snapshot")
			case AttemptProfile, CapabilityProfile, PolicyProfile, DestinationProfile:
			case "unirouter.budget-definition.v1", "unirouter.budget-period-summary.v1", "unirouter.budget-alert.v1":
				// The shared source advertises D too. This metadata does not make
				// D a valid XI registration or enable its consumption.
				transports = []string{"budget_catalog", "current_budget_snapshot"}
			default:
				return c, ErrInvalid
			}
			if !slices.Equal(c.ProfileTransports[p], transports) {
				return c, ErrInvalid
			}
		} else if !ValidProfile(p) {
			return c, ErrInvalid
		}
		seen[p] = true
	}
	return c, nil
}

func decodeStreamCapabilities(raw []byte, profile string) (Capabilities, error) {
	if ValidObservationProfile(profile) {
		return DecodeObservationCapabilities(raw)
	}
	if ValidProfile(profile) {
		return DecodeCapabilities(raw)
	}
	return Capabilities{}, ErrInvalid
}

func decodeStreamPage(raw []byte, s Stream, limit int) (Page, error) {
	if ValidObservationProfile(s.Profile) {
		return DecodeObservationPage(raw, s.SourceInstance, s.OrgID, s.ProjectID, s.Profile, s.Cursor, limit)
	}
	return DecodePage(raw, s.SourceInstance, s.OrgID, s.ProjectID, s.Profile, s.Cursor, limit)
}

type Consumer struct {
	Store          Store
	Reader         HTTPReader
	Now            func() time.Time
	ExpectedStream *Stream
}

// PullOnce commits all valid documents and their page position atomically.
// Pending interpretation is durable receipt, so a predecessor on a later page
// cannot deadlock the feed. There is no hidden reset or retry on a gap.
func (c Consumer) PullOnce(ctx context.Context, streamID string) (Page, error) {
	if c.Store == nil || c.Reader == nil {
		return Page{}, ErrUnavailable
	}
	s, e := c.Store.LoadSourceFactsStream(ctx, streamID)
	if e != nil {
		return Page{}, e
	}
	if s.Status != "active" || s.RequiresAttention {
		return Page{}, ErrUnavailable
	}
	if c.ExpectedStream != nil && !SameRegistration(*c.ExpectedStream, s) {
		return Page{}, ErrScopeChanged
	}
	now := time.Now().UTC()
	if c.Now != nil {
		now = c.Now().UTC()
	}
	fail := func(code string, err error) (Page, error) {
		if e := c.Store.RecordSourceFactsFailure(ctx, s, code, now); e != nil {
			return Page{}, e
		}
		return Page{}, err
	}
	raw, e := c.Reader.FetchSourceCapabilities(ctx)
	if e != nil {
		return fail(sourceFailure(e), e)
	}
	cap, e := decodeStreamCapabilities(raw, s.Profile)
	if e != nil {
		return fail("invalid_capabilities", e)
	}
	if cap.SourceInstance != s.SourceInstance || cap.ScopeGeneration != s.ScopeGeneration || !slices.Contains(cap.Profiles, s.Profile) {
		return fail("scope_changed", ErrScopeChanged)
	}
	if cap.Status != "active" || cap.CredentialStatus != "feed_active" {
		return fail("source_paused", ErrUnavailable)
	}
	raw, e = c.Reader.FetchSourceFacts(ctx, s.Profile, s.Cursor, 50)
	if e != nil {
		return fail(sourceFailure(e), e)
	}
	p, e := decodeStreamPage(raw, s, 50)
	if e != nil {
		return fail("invalid_page", e)
	}
	if e = c.Store.CommitSourceFactsPage(ctx, s, p, now); e != nil {
		// Only contradictory immutable source bytes/links are a source failure.
		// A stale request is local concurrency; it must not overwrite progress.
		if errors.Is(e, ErrConflict) {
			return fail("source_fact_conflict", e)
		}
		return Page{}, e
	}
	return p, nil
}

// NeedsAttention distinguishes durable administrator decisions from transient
// transport observations. Restarting either entrypoint cannot clear this state.
func NeedsAttention(code string) bool {
	switch code {
	case "invalid_capabilities", "scope_changed", "source_paused", "invalid_page", "cursor_or_epoch_conflict", "cursor_expired", "source_authorization_denied", "source_fact_conflict":
		return true
	}
	return false
}

func sourceFailure(err error) string {
	var h interface {
		error
		HTTPStatusCode() int
		SourceErrorCode() string
	}
	if errors.As(err, &h) {
		switch h.HTTPStatusCode() {
		case 400:
			if h.SourceErrorCode() == "invalid_cursor" {
				return "cursor_or_epoch_conflict"
			}
		case 409:
			return "cursor_or_epoch_conflict"
		case 410:
			return "cursor_expired"
		case 401, 403:
			return "source_authorization_denied"
		}
		if h.SourceErrorCode() == "source_paused" {
			return "source_paused"
		}
	}
	return "source_unavailable"
}
