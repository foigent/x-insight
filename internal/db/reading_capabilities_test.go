package db

import (
	"github.com/foigent/x-insight/internal/workbench"
	"testing"
)

func TestReadingCapabilitiesMatchActualReadingPredicates(t *testing.T) {
	tests := []struct {
		name   string
		grants []workbench.Grant
		want   workbench.ReadingCapabilities
	}{
		{name: "no grants"},
		{name: "details alone is not runtime authority", grants: []workbench.Grant{{AllowDetails: true, AllowCoverage: true}}, want: workbench.ReadingCapabilities{Coverage: true}},
		{name: "project runtime without economic or activity authority", grants: []workbench.Grant{{SourceProject: "project-2", AllowDetails: true, AllowCoverage: true, AllowSourceRuntime: true}}, want: workbench.ReadingCapabilities{Runtime: true, Coverage: true}},
		{name: "cost-only resource grant cannot open work activity", grants: []workbench.Grant{{AllowSummary: true, AllowResource: true, CostProject: "cost-2"}}, want: workbench.ReadingCapabilities{Summary: true}},
		{name: "project work activity is independent of summary", grants: []workbench.Grant{{AllowResource: true, SourceProject: "project-2"}}, want: workbench.ReadingCapabilities{WorkActivity: true}},
		{name: "unqualified content purpose is not body runtime", grants: []workbench.Grant{{AllowContent: true, AllowedContentPurposes: []string{"unsupported"}}}},
		{name: "human review content runtime", grants: []workbench.Grant{{AllowContent: true, AllowedContentPurposes: []string{"human_review"}}}, want: workbench.ReadingCapabilities{Runtime: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := readingCapabilities(workbench.Authorization{Grants: tt.grants}); got != tt.want {
				t.Fatalf("got%+v want%+v", got, tt.want)
			}
		})
	}
}
