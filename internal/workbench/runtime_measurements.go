package workbench

import (
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
)

const RuntimeMeasurementVersion = "xinsight.runtime.measurements.linear-interpolation.v1"

type RuntimePercentiles struct {
	Version     string `json:"version"`
	State       string `json:"state"`
	Unit        string `json:"unit"`
	SampleCount int    `json:"sample_count"`
	P50         string `json:"p50,omitempty"`
	P95         string `json:"p95,omitempty"`
}

// ComputeRuntimePercentiles implements the frozen UR T05 interpolation rule.
// The caller must first restrict the authorized reading to one operation,
// stream mode, measurement kind/version and completion-evidence group, selecting
// only eligible observed values. It must never average percentiles across groups.
// Source values are canonical int64 nanoseconds, including genuine observed zero;
// the exact interpolated result is returned as decimal milliseconds.
func ComputeRuntimePercentiles(nanoseconds []string) (RuntimePercentiles, error) {
	result := RuntimePercentiles{Version: RuntimeMeasurementVersion, State: "not_computable", Unit: "milliseconds", SampleCount: len(nanoseconds)}
	values := make([]int64, len(nanoseconds))
	for i, raw := range nanoseconds {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v < 0 || strconv.FormatInt(v, 10) != raw {
			return RuntimePercentiles{}, fmt.Errorf("%w: invalid observed runtime measurement", ErrInvalid)
		}
		values[i] = v
	}
	if len(values) == 0 {
		return result, nil
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	result.State = "computed"
	result.P50 = runtimeInterpolatedMilliseconds(values, 50)
	result.P95 = runtimeInterpolatedMilliseconds(values, 95)
	return result, nil
}

func runtimeInterpolatedMilliseconds(sorted []int64, percent int) string {
	// Split the index multiplication to avoid overflowing int for large slices.
	n := len(sorted) - 1
	index := (n/100)*percent + (n%100)*percent/100
	fraction := (n % 100) * percent % 100
	numerator := new(big.Int).Mul(big.NewInt(sorted[index]), big.NewInt(100))
	if fraction != 0 {
		delta := new(big.Int).Sub(big.NewInt(sorted[index+1]), big.NewInt(sorted[index]))
		numerator.Add(numerator, delta.Mul(delta, big.NewInt(int64(fraction))))
	}
	// Division by 100 (interpolation) and 1e6 (ns -> ms) terminates at 8 places.
	value := new(big.Rat).SetFrac(numerator, big.NewInt(100_000_000)).FloatString(8)
	return strings.TrimRight(strings.TrimRight(value, "0"), ".")
}
