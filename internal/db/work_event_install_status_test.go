package db

import "testing"

func TestValidWorkInstallStatusBoundsAndCategories(t *testing.T) {
	value := func(n int64) *int64 { return &n }
	gaps := []WorkInstallGap{{Code: "body_limit", Occurrences: 1}}
	status := &WorkInstallStatus{MetadataPending: value(0), MetadataBlocked: value(0), BodyPending: value(0), Gaps: &gaps}
	if !ValidWorkInstallStatus(status) {
		t.Fatal("valid status rejected")
	}
	for _, altered := range []WorkInstallStatus{
		{MetadataBlocked: value(0), BodyPending: value(0), Gaps: &gaps},
		{MetadataPending: value(-1), MetadataBlocked: value(0), BodyPending: value(0), Gaps: &gaps},
		{MetadataPending: value(maxWorkInstallStatusCount + 1), MetadataBlocked: value(0), BodyPending: value(0), Gaps: &gaps},
		{MetadataPending: value(0), MetadataBlocked: value(0), BodyPending: value(0)},
	} {
		if ValidWorkInstallStatus(&altered) {
			t.Fatalf("invalid status accepted: %+v", altered)
		}
	}
	for _, bad := range [][]WorkInstallGap{
		{{Code: "body_item_limit", Occurrences: 1}},
		{{Code: "body_limit", Occurrences: 0}},
		{{Code: "body_limit", Occurrences: maxWorkInstallStatusCount + 1}},
		{{Code: "body_limit", Occurrences: 1}, {Code: "body_limit", Occurrences: 2}},
	} {
		candidate := *status
		candidate.Gaps = &bad
		if ValidWorkInstallStatus(&candidate) {
			t.Fatalf("invalid gaps accepted: %+v", bad)
		}
	}
}
