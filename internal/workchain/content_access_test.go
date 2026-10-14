package workchain

import (
	"github.com/foigent/x-insight/internal/workbench"
	"testing"
)

func TestContentGrantIndependentAndExactScope(t *testing.T) {
	g := workbench.Grant{SourceSystemID: "s", SourceProject: "p", CostProject: "cost", AllowContent: true, AllowedContentPurposes: []string{"human_review"}}
	a := &Access{Authorization: workbench.Authorization{Grants: []workbench.Grant{g}}}
	if !a.ContentAllowedFor("s", "p", "cost") || !a.Allows(Requirement{Source: "s", Project: "p", CostProject: "cost", Fields: Fields{Content: true}}) {
		t.Fatal("content-only grant denied")
	}
	if _, ok := a.FieldsFor("s", "p", "cost"); ok {
		t.Fatal("content implied details")
	}
	if a.ObservationFieldsFor("s", "p", "cost").Any() {
		t.Fatal("content implied B")
	}
	for _, scope := range [][3]string{{"other", "p", "cost"}, {"s", "other", "cost"}, {"s", "p", ""}, {"s", "", "cost"}} {
		if a.ContentAllowedFor(scope[0], scope[1], scope[2]) {
			t.Fatalf("cross-scope access: %v", scope)
		}
	}
	for _, purposes := range [][]string{nil, {}, {"Human_Review"}, {"read"}, {"human_review", "human_review"}} {
		a.Authorization.Grants[0].AllowedContentPurposes = purposes
		if a.ContentAllowedFor("s", "p", "cost") {
			t.Fatal("invalid purpose accepted")
		}
	}
	a.Authorization.Grants[0] = workbench.Grant{SourceSystemID: "s", AllowDetails: true, AllowSourceRuntime: true, AllowPurposeReview: true}
	if a.ContentAllowedFor("s", "p", "") {
		t.Fatal("legacy rights implied content")
	}
	var absent *Access
	if absent.ContentAllowedFor("s", "p", "") {
		t.Fatal("nil authorized")
	}
}
