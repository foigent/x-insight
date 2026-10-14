package workchain

import (
	"testing"

	"github.com/foigent/x-insight/internal/workbench"
)

func TestObservationGrantsStayWithinScopeAndField(t *testing.T) {
	a := &Access{Authorization: workbench.Authorization{Grants: []workbench.Grant{
		{SourceSystemID: "A", SourceProject: "one", AllowDetails: true, AllowSourceRuntime: true},
		{SourceSystemID: "A", SourceProject: "two", AllowDetails: true, AllowSourceAttempt: true, AllowSourceCapability: true},
		{SourceSystemID: "B", SourceProject: "one", AllowDetails: true, AllowSourcePolicy: true, AllowSourceDestination: true},
		{SourceSystemID: "A", SourceProject: "one", CostProject: "paid", AllowDetails: true, AllowSourceDestination: true},
		{SourceSystemID: "A", SourceProject: "one", AllowDetails: false, AllowSourcePolicy: true},
	}}}
	for _, tc := range []struct {
		source, project, cost string
		want                  ObservationFields
	}{
		{"A", "one", "", ObservationFields{Runtime: true, Policy: true}},
		{"A", "one", "paid", ObservationFields{Runtime: true, Policy: true, Destination: true}},
		{"A", "two", "", ObservationFields{Attempt: true, Capability: true}},
		{"B", "one", "", ObservationFields{Policy: true, Destination: true}},
		{"A", "", "", ObservationFields{}},
		{"C", "one", "", ObservationFields{}},
	} {
		if got := a.ObservationFieldsFor(tc.source, tc.project, tc.cost); got != tc.want {
			t.Errorf("%+v: got %+v", tc, got)
		}
	}
	if got, allowed := a.FieldsFor("A", "one", ""); !allowed || got != (Fields{}) {
		t.Fatal("B grants granted A identity, context or money")
	}
	a.Authorization.Grants[0].AllowSourceRuntime = false
	a.Authorization.Grants[4].AllowSourcePolicy = false
	if a.ObservationFieldsFor("A", "one", "").Any() {
		t.Fatal("revoked field stayed visible")
	}
	var absent *Access
	if absent.ObservationFieldsFor("A", "one", "").Any() {
		t.Fatal("missing authorization allowed B")
	}
}
