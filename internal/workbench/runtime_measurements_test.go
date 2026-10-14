package workbench

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

func TestRuntimePercentilesFrozenT05(t *testing.T) {
	raw, err := os.ReadFile("../sourcefacts/contract/observations/measurement-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	type expected struct {
		N   int    `json:"n"`
		P50 string `json:"p50"`
		P95 string `json:"p95"`
	}
	var fixture struct {
		Unit  string
		Cases []struct {
			ID        string
			Completed []int64 `json:"completed_observed"`
			Expected  expected
			WithZero  expected `json:"with_observed_zero"`
			Empty     struct{ State string }
		}
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Unit != "milliseconds" {
		t.Fatal("unexpected vector unit")
	}
	found := false
	for _, v := range fixture.Cases {
		if v.ID != "T05" {
			continue
		}
		found = true
		values := make([]string, len(v.Completed))
		for i, ms := range v.Completed {
			values[i] = strconv.FormatInt(ms*1_000_000, 10)
		}
		check := func(values []string, want expected) {
			t.Helper()
			got, err := ComputeRuntimePercentiles(values)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != "computed" || got.Unit != fixture.Unit || got.SampleCount != want.N || got.P50 != want.P50 || got.P95 != want.P95 {
				t.Fatalf("got %+v; frozen expected %+v", got, want)
			}
		}
		check(values, v.Expected)
		check(append(values, "0"), v.WithZero)
		empty, err := ComputeRuntimePercentiles(nil)
		if err != nil || empty.State != v.Empty.State || empty.SampleCount != 0 || empty.P50 != "" || empty.P95 != "" {
			t.Fatal("empty sample set must not manufacture zero", empty, err)
		}
	}
	if !found {
		t.Fatal("missing frozen T05 vector")
	}
}

func TestRuntimePercentilesKeepNanosecondPrecisionAndInt64Range(t *testing.T) {
	for _, tc := range []struct {
		values   []string
		p50, p95 string
	}{
		{[]string{"1", "0"}, "0.0000005", "0.00000095"},
		{[]string{"9223372036854775807", "9223372036854775807"}, "9223372036854.775807", "9223372036854.775807"},
		{[]string{"0"}, "0", "0"},
	} {
		got, err := ComputeRuntimePercentiles(tc.values)
		if err != nil || got.P50 != tc.p50 || got.P95 != tc.p95 {
			t.Fatalf("%v: %+v %v", tc.values, got, err)
		}
	}
	for _, invalid := range []string{"", "-1", "01", "+1", "1.5", "NaN", "9223372036854775808"} {
		if _, err := ComputeRuntimePercentiles([]string{invalid}); err == nil {
			t.Fatalf("invalid value accepted: %q", invalid)
		}
	}
}
