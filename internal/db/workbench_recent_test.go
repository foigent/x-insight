package db

import (
	"testing"

	"github.com/foigent/x-insight/internal/workbench"
)

func TestSameRecentFiltersUsesSetsWithoutWidening(t *testing.T) {
	a := workbench.Filters{SourceIDs: []string{"source-b", "source-a"}, SourceProjects: []string{"project-2", "project-1"}, CostProjects: []string{"cost-1"}}
	b := workbench.Filters{SourceIDs: []string{"source-a", "source-b"}, SourceProjects: []string{"project-1", "project-2"}, CostProjects: []string{"cost-1"}}
	if !sameRecentFilters(a, b) {
		t.Fatal("same scope in a different order rejected")
	}
	b.CostProjects = nil
	if sameRecentFilters(a, b) {
		t.Fatal("different cost scope accepted")
	}
	b.CostProjects = a.CostProjects
	b.SourceIDs = []string{"source-a", "source-a"}
	if sameRecentFilters(a, b) {
		t.Fatal("duplicated source widened the scope")
	}
}
