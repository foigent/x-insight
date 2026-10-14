package workbench

import (
	"errors"
	"testing"
	"time"
)

func TestReadingRequestResolveFreezesSupportedRelativeAndExplicitPeriods(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 123456000, time.UTC)
	request := validReadingRequest()
	request.Period.RelativeHours = 168
	period, err := request.Resolve(now)
	if err != nil {
		t.Fatal(err)
	}
	if !period.StartInclusive.Equal(now.Add(-168*time.Hour)) || !period.EndExclusive.Equal(now) || period.Timezone != "Asia/Shanghai" {
		t.Fatalf("relative period=%+v", period)
	}

	start := now.Add(-31 * 24 * time.Hour)
	request.Period = PeriodInput{StartInclusive: &start, EndExclusive: &now, DisplayTimezone: "UTC"}
	period, err = request.Resolve(now)
	if err != nil || !period.StartInclusive.Equal(start) || !period.EndExclusive.Equal(now) {
		t.Fatalf("explicit period=%+v err=%v", period, err)
	}
}

func TestReadingRequestRejectsAmbiguousFutureAndUnsupportedInputs(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	valid := validReadingRequest()
	testCases := []struct {
		name   string
		mutate func(*ReadingRequest)
		want   error
	}{
		{"unsupported contract", func(r *ReadingRequest) { r.ContractVersion = "xinsight.workbench.v2" }, ErrUnsupportedVersion},
		{"unsupported metric", func(r *ReadingRequest) { r.MetricVersion = "xinsight.workbench.cost.v2" }, ErrUnsupportedVersion},
		{"relative and explicit", func(r *ReadingRequest) { start := now.Add(-time.Hour); r.Period.StartInclusive = &start }, ErrInvalid},
		{"future end", func(r *ReadingRequest) {
			start, end := now, now.Add(time.Second)
			r.Period = PeriodInput{StartInclusive: &start, EndExclusive: &end, DisplayTimezone: "UTC"}
		}, ErrInvalid},
		{"duplicate filters", func(r *ReadingRequest) { r.Filters.SourceProjects = []string{"p", "p"} }, ErrInvalid},
		{"unknown timezone", func(r *ReadingRequest) { r.Period.DisplayTimezone = "Mars/Olympus" }, ErrInvalid},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := valid

			testCase.mutate(&request)
			_, err := request.Resolve(now)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("error=%v want=%v", err, testCase.want)
			}
		})
	}
}

func TestSavedFilterValidationUsesProvidedCutoff(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	command := SavedFilterCommand{
		Name:           "Current cost",
		Config:         validReadingRequest(),
		IdempotencyKey: "save:current-1",
	}
	if err := ValidateSavedFilterAt(command, now); err != nil {
		t.Fatal(err)
	}
	command.Config.Period = PeriodInput{StartInclusive: pointerTime(now.Add(-time.Hour)), EndExclusive: pointerTime(now.Add(time.Second)), DisplayTimezone: "UTC"}
	if !errors.Is(ValidateSavedFilterAt(command, now), ErrInvalid) {
		t.Fatal("saved filter accepted a period beyond the supplied cutoff")
	}
	command = SavedFilterCommand{Name: " Current cost", Config: validReadingRequest(), IdempotencyKey: "save:current-1"}
	if !errors.Is(ValidateSavedFilterAt(command, now), ErrInvalid) {
		t.Fatal("saved filter accepted a non-canonical name")
	}
}

func TestMoneySelectionAndIDsRemainExact(t *testing.T) {
	if SelectedMicros(MoneyGatewayCharged, "17", "23") != "17" ||
		SelectedMicros(MoneyCalculatedReference, "17", "23") != "23" {
		t.Fatal("money basis selection drifted")
	}
	for _, prefix := range []string{"xwr_", "xwf_"} {
		id, err := NewID(prefix)
		if err != nil {
			t.Fatal(err)
		}
		if prefix == "xwr_" && !ValidReadingID(id) || prefix == "xwf_" && !ValidSavedFilterID(id) {
			t.Fatalf("invalid generated id %q", id)
		}
	}
}

func validReadingRequest() ReadingRequest {
	return ReadingRequest{
		ContractVersion: ContractVersion,
		Page:            "cost",
		MetricVersion:   MetricVersion,
		Period:          PeriodInput{RelativeHours: 24, DisplayTimezone: "Asia/Shanghai"},
		MoneyBasis:      MoneyGatewayCharged,
		Currency:        "CNY",
		Filters:         Filters{SourceIDs: []string{}, SourceProjects: []string{}, CostProjects: []string{}},
	}
}

func pointerTime(value time.Time) *time.Time { return &value }
