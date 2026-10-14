package db

import (
	"testing"

	"github.com/foigent/x-insight/internal/workbench"
)

func TestObservationScopeSuffixPreservesPriorScope(t *testing.T) {
	a := workbench.Authorization{ObservationRevision: "b1", Grants: []workbench.Grant{{AllowDetails: true, AllowSourceContext: true, AllowSourceCost: true}}}
	if observationScopeSuffix(a) != "" {
		t.Fatal("B changed an existing A-only scope digest")
	}
	a.Grants[0].AllowSourceRuntime = true
	if observationScopeSuffix(a) != "\nb1" {
		t.Fatal("B permission revision was omitted")
	}
	a.ObservationRevision = "b2"
	if observationScopeSuffix(a) != "\nb2" {
		t.Fatal("B permission change reused old digest")
	}
	a.Grants[0].AllowSourceRuntime = false
	if observationScopeSuffix(a) != "" {
		t.Fatal("revoked B permission remains in scope")
	}
}
