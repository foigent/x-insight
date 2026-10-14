package sourcefacts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connector"
)

func observationCapabilitiesFor(profile string) Capabilities {
	c := capabilities()
	c.SchemaVersion = "unirouter.source-capabilities.v2"
	c.ProfileVersion = ObservationConnectorVersion
	c.Profiles = []string{profile}
	c.ProfileTransports = map[string][]string{profile: {"incremental_events"}}
	if profile == RuntimeProfile {
		c.ProfileTransports[profile] = append(c.ProfileTransports[profile], "current_snapshot")
	}
	c.EventCoverage = "forward_only"
	c.CurrentRuntimeCoverage = "lease_backed_inventory"
	return c
}

func TestObservationCapabilitiesDoNotAuthorizeD(t *testing.T) {
	c := observationCapabilitiesFor(RuntimeProfile)
	budget := "unirouter.budget-definition.v1"
	c.Profiles = append(c.Profiles, budget)
	c.ProfileTransports[budget] = []string{"budget_catalog", "current_budget_snapshot"}
	raw, _ := json.Marshal(c)
	if _, err := DecodeObservationCapabilities(raw); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeCapabilities(raw); err == nil {
		t.Fatal("v2 capabilities accepted by A v1")
	}
	s := Stream{ID: "00000000-0000-4000-8000-000000000001", SourceSystemID: "00000000-0000-4000-8000-000000000002", SourceInstance: "ur", OrgID: "1", ProjectID: "2", Profile: budget, ScopeGeneration: "1"}
	if ValidRegistration(s) {
		t.Fatal("advertised D became consumable")
	}
	for _, change := range []func(*Capabilities){
		func(c *Capabilities) { c.ProfileTransports[RuntimeProfile] = []string{"incremental_events"} },
		func(c *Capabilities) { c.EventCoverage = "complete" },
		func(c *Capabilities) { c.CurrentRuntimeCoverage = "all_requests" },
		func(c *Capabilities) { c.Profiles = append(c.Profiles, RuntimeProfile) },
	} {
		bad := observationCapabilitiesFor(RuntimeProfile)
		change(&bad)
		raw, _ := json.Marshal(bad)
		if _, err := DecodeObservationCapabilities(raw); err == nil {
			t.Fatal("invalid capabilities accepted")
		}
	}
}

func TestExplicitManagedV2SupportsTwoScopesAndKeepsV1Strict(t *testing.T) {
	c := ManagedConfig{SchemaVersion: ManagedObservationConfigVersion, PollIntervalSeconds: 1, RetryMaximumSeconds: 8}
	profiles := append([]string{ContextProfile, IdentityProfile, CostProfile}, ObservationProfiles()...)
	for scope := 0; scope < 2; scope++ {
		for _, profile := range profiles {
			index := len(c.Profiles)
			c.Profiles = append(c.Profiles, ManagedProfileConfig{StreamID: fmt.Sprintf("00000000-0000-4000-8000-%012d", index+1), SourceSystemID: "00000000-0000-4000-8000-000000000100", SourceInstance: "ur", OrgID: "1", ProjectID: fmt.Sprint(scope + 1), Profile: profile, ScopeGeneration: "1", Endpoint: "https://source.invalid" + ManagedEndpointPath(profile), KeyFile: fmt.Sprintf("/etc/x-insight/source-profile-%d.key", index)})
		}
	}
	raw, _ := json.Marshal(c)
	if _, err := ParseManagedConfig(raw); err != nil {
		t.Fatal(err)
	}
	c.SchemaVersion = ManagedConfigVersion
	if c.Validate() == nil {
		t.Fatal("legacy config accepted sixteen streams")
	}
	c.Profiles = c.Profiles[3:4]
	if c.Validate() == nil {
		t.Fatal("legacy config accepted B")
	}
	c.SchemaVersion = ManagedObservationConfigVersion
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Profiles[0].Endpoint = "https://source.invalid/api/v1/integrations/xinsight/source-facts"
	if c.Validate() == nil {
		t.Fatal("B profile bound to A endpoint")
	}
	c.Profiles = nil
	if c.Validate() == nil {
		t.Fatal("empty config implicitly enabled streams")
	}
}

func TestManagedV2RotatesAllSixteenStreams(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	now := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	var seen []string
	w := ManagedWorker{ConfigVersion: ManagedObservationConfigVersion, PollInterval: time.Second, RetryMaximum: 8 * time.Second, Now: func() time.Time { return now }, Wait: func(_ context.Context, d time.Duration) error { now = now.Add(d); return nil }, Observe: func(id string, err error) {
		if err != nil {
			t.Error(err)
		}
		seen = append(seen, id)
		if len(seen) == 32 {
			cancel()
		}
	}}
	profiles := append([]string{ContextProfile, IdentityProfile, CostProfile}, ObservationProfiles()...)
	for scope := 0; scope < 2; scope++ {
		for _, profile := range profiles {
			id := fmt.Sprintf("00000000-0000-4000-8000-%012d", len(w.Profiles)+1)
			s := Stream{ID: id, SourceSystemID: "00000000-0000-4000-8000-000000000100", SourceInstance: "ur", OrgID: "1", ProjectID: fmt.Sprint(scope + 1), Profile: profile, ScopeGeneration: "1", Status: "active"}
			c := capabilities()
			c.Profiles = []string{profile}
			if ValidObservationProfile(profile) {
				c = observationCapabilitiesFor(profile)
			}
			p := Page{BatchVersion: connector.EventBatchVersionV1, ConnectorID: ConnectorID, ConnectorVersion: c.ProfileVersion, ProducerInstance: "ur", GeneratedAt: now.Format(TimeFormat), Events: []json.RawMessage{}, NextCursor: "start", HighWatermark: "start"}
			capRaw, _ := json.Marshal(c)
			pageRaw, _ := json.Marshal(p)
			w.Profiles = append(w.Profiles, ManagedProfile{StreamID: id, Consumer: Consumer{Store: &consumerMemory{stream: s}, Reader: &readerMemory{cap: capRaw, page: pageRaw}, ExpectedStream: &s}})
		}
	}
	if err := w.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	var order []string
	for _, p := range w.Profiles {
		order = append(order, p.StreamID)
	}
	if len(seen) != 32 || !reflect.DeepEqual(seen[:16], order) || !reflect.DeepEqual(seen[16:], order) {
		t.Fatal("stream starvation", seen)
	}
	w.ConfigVersion = ManagedConfigVersion
	if err := w.Run(t.Context()); !errors.Is(err, ErrInvalid) {
		t.Fatal("v1 worker accepted sixteen streams", err)
	}
}

// This capability wire uses the exact pure serializer and registered profile set
// of UR 1b49266bc5112f885350d32d9ec23506d8308082's P3 observation fixture.
// It is an offline contract vector, not an authenticated runtime receipt.
func TestPinnedProducerCapabilitiesKeepGatewayProfileMetadataOnly(t *testing.T) {
	raw, err := os.ReadFile("testdata/pinned-producer-capabilities-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	const advertised = "unirouter.runtime-observation.v2"
	c, err := DecodeObservationCapabilities(raw)
	if err != nil || len(c.Profiles) != 6 || !reflect.DeepEqual(c.ProfileTransports[advertised], []string{"incremental_events"}) {
		t.Fatal("pinned capabilities rejected", err)
	}
	if _, err = DecodeCapabilities(raw); !errors.Is(err, ErrInvalid) {
		t.Fatal("V1 decoder accepted V2 metadata", err)
	}
	for _, profile := range ObservationProfiles() {
		t.Run(profile, func(t *testing.T) {
			s := Stream{ID: "00000000-0000-4000-8000-000000000001", SourceSystemID: "00000000-0000-4000-8000-000000000002", SourceInstance: c.SourceInstance, OrgID: "1", ProjectID: "2", ScopeGeneration: "1", Profile: profile}
			r := &readerMemory{cap: raw}
			if err := Preflight(t.Context(), r, s); err != nil {
				t.Fatal(err)
			}
			if r.calls != 1 {
				t.Fatal("preflight must read only capabilities once")
			}
		})
	}
	s := Stream{ID: "00000000-0000-4000-8000-000000000001", SourceSystemID: "00000000-0000-4000-8000-000000000002", SourceInstance: c.SourceInstance, OrgID: "1", ProjectID: "2", ScopeGeneration: "1", Profile: advertised}
	if ValidObservationProfile(advertised) || ValidProfile(advertised) || ValidRegistration(s) || ManagedEndpointPath(advertised) != "" {
		t.Fatal("metadata enabled an unsupported XI stream")
	}
	r := &readerMemory{cap: raw}
	if err = Preflight(t.Context(), r, s); !errors.Is(err, ErrInvalid) || r.calls != 0 {
		t.Fatal("unsupported registration fetched capabilities", err)
	}
	if _, err = decodeStreamCapabilities(raw, advertised); !errors.Is(err, ErrInvalid) {
		t.Fatal("unsupported stream became consumable", err)
	}
	mutations := map[string]func(map[string]any){
		"unknown_profile": func(m map[string]any) {
			p := "unirouter.unknown.v1"
			m["profiles"] = append(m["profiles"].([]any), p)
			m["profile_transports"].(map[string]any)[p] = []string{"incremental_events"}
		},
		"gateway_snapshot_transport": func(m map[string]any) {
			m["profile_transports"].(map[string]any)[advertised] = []string{"incremental_events", "current_snapshot"}
		},
		"gateway_unknown_transport": func(m map[string]any) { m["profile_transports"].(map[string]any)[advertised] = []string{"all_events"} },
		"gateway_missing_transport": func(m map[string]any) { delete(m["profile_transports"].(map[string]any), advertised) },
		"extra_transport": func(m map[string]any) {
			m["profile_transports"].(map[string]any)["unirouter.unknown.v1"] = []string{"incremental_events"}
		},
		"duplicate_profile": func(m map[string]any) { m["profiles"] = append(m["profiles"].([]any), advertised) },
		"wrong_runtime_v1_transport": func(m map[string]any) {
			m["profile_transports"].(map[string]any)[RuntimeProfile] = []string{"incremental_events"}
		},
		"complete_coverage": func(m map[string]any) { m["event_coverage"] = "complete" },
		"missing_field":     func(m map[string]any) { delete(m, "status") },
		"extra_field":       func(m map[string]any) { m["new_field"] = true },
		"wrong_version":     func(m map[string]any) { m["profile_version"] = "3.0.0" },
		"null_profiles":     func(m map[string]any) { m["profiles"] = nil },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			mutate(m)
			bad, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = DecodeObservationCapabilities(bad); !errors.Is(err, ErrInvalid) {
				t.Fatal("invalid capabilities accepted", err)
			}
		})
	}
}
