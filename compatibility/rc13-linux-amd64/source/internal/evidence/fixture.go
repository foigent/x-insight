package evidence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Outcome string

const (
	OutcomeAccepted    Outcome = "accepted"
	OutcomeRejected    Outcome = "rejected"
	OutcomeQuarantined Outcome = "quarantined"
	OutcomeDuplicate   Outcome = "duplicate_noop"
)

type Fixture struct {
	Name     string         `json:"name"`
	Expected Outcome        `json:"expected"`
	Event    map[string]any `json:"event"`
}

type FixtureResult struct {
	Name     string
	Expected Outcome
	Actual   Outcome
	Reason   string
	Pass     bool
}

type FixtureReport struct {
	Results []FixtureResult
	Total   int
	Passed  int
	Failed  int
}

type FixtureRunner struct {
	ingester *InMemoryIngester
}

func NewFixtureRunner(schemaPath string) (*FixtureRunner, error) {
	validator, err := NewSchemaValidator(schemaPath)
	if err != nil {
		return nil, err
	}
	return &FixtureRunner{ingester: NewInMemoryIngester(validator)}, nil
}

func (r *FixtureRunner) RunDir(dir string) (FixtureReport, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return FixtureReport{}, err
	}

	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(paths)

	report := FixtureReport{}
	for _, path := range paths {
		result := r.runFile(path)
		report.Results = append(report.Results, result)
		report.Total++
		if result.Pass {
			report.Passed++
		} else {
			report.Failed++
		}
	}
	return report, nil
}

func (r *FixtureRunner) runFile(path string) FixtureResult {
	data, err := os.ReadFile(path)
	name := filepath.Base(path)
	if err != nil {
		return FixtureResult{Name: name, Actual: OutcomeRejected, Reason: err.Error()}
	}

	var fixture Fixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		return FixtureResult{Name: name, Actual: OutcomeRejected, Reason: err.Error()}
	}
	if fixture.Name != "" {
		name = fixture.Name
	}

	result := r.ingester.Ingest(fixture.Event)
	return FixtureResult{
		Name:     name,
		Expected: fixture.Expected,
		Actual:   result.Outcome,
		Reason:   result.Reason,
		Pass:     fixture.Expected == result.Outcome,
	}
}

func (o Outcome) Valid() bool {
	switch o {
	case OutcomeAccepted, OutcomeRejected, OutcomeQuarantined, OutcomeDuplicate:
		return true
	default:
		return false
	}
}

func (f Fixture) Validate() error {
	if f.Name == "" {
		return fmt.Errorf("fixture name is required")
	}
	if !f.Expected.Valid() {
		return fmt.Errorf("invalid fixture expected outcome: %s", f.Expected)
	}
	if f.Event == nil {
		return fmt.Errorf("fixture event is required")
	}
	return nil
}
