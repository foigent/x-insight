package api_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/workspace"
)

// An excluded Finding remains excluded and changes none of the displayed
// filtered statistics, membership or basis. It must not interrupt that queue.
func TestManagerQueueExcludedChange(t *testing.T) {
	f := startResponsibilityProduction(t)
	reader := queueReader(t, f)
	seedQueueFindings(t, f, 1, 3)
	first := queuePage(t, reader, "limit=1&search=Queue+item")
	if first.Total != 3 || first.NextCursor == "" {
		t.Fatal("fixture incomplete")
	}
	if _, err := f.admin.Exec(`UPDATE case_findings SET title='Still excluded',version=version+1 WHERE id=$1`, f.finding.ID); err != nil {
		t.Fatal(err)
	}
	status, raw := reader.call(t, "GET", "/api/v1/investigation/queue?limit=1&search=Queue+item&cursor="+url.QueryEscape(first.NextCursor), nil, nil)
	if status != 200 {
		t.Fatalf("unrelated excluded Finding interrupted unchanged filtered queue: HTTP %d %s", status, raw)
	}
	var next workspace.QueuePage
	if json.Unmarshal(raw, &next) != nil || next.Statistics != first.Statistics || next.Version != first.Version {
		t.Fatal("unchanged filtered queue drifted")
	}
	if _, err := f.admin.Exec(`UPDATE case_findings SET title='Queue item newly matched',version=version+1 WHERE id=$1`, f.finding.ID); err != nil {
		t.Fatal(err)
	}
	if status, _ := reader.call(t, "GET", "/api/v1/investigation/queue?limit=1&search=Queue+item&cursor="+url.QueryEscape(first.NextCursor), nil, nil); status != 409 {
		t.Fatal("newly matching row did not invalidate filtered queue")
	}
}

// A client-supplied continuation cannot change the first-page evaluation time
// while retaining the original request fingerprint and version.
func TestManagerQueueCursorTimeBinding(t *testing.T) {
	f := startResponsibilityProduction(t)
	seedQueueFindings(t, f, 1, 2)
	at := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := f.admin.Exec(`UPDATE case_findings SET created_at=$1::timestamptz-interval '1 hour' WHERE id=$2`, at, f.finding.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(`UPDATE case_findings SET created_at=$1::timestamptz-interval '23 hours 50 minutes' WHERE id='xf_0000000000000000000001'`, at); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(`UPDATE case_findings SET created_at=$1::timestamptz-interval '24 hours 10 minutes' WHERE id='xf_0000000000000000000002'`, at); err != nil {
		t.Fatal(err)
	}
	access := workspace.QueueAccess{Subject: "xs_1111111111111111", Revision: "manager-boundary"}
	req := workspace.QueueRequest{Filters: workspace.QueueFilters{Mode: "created"}, Limit: 1}
	first, err := f.store.ListInvestigationQueue(context.Background(), access, req, at)
	if err != nil || first.Total != 2 || first.NextCursor == "" {
		t.Fatalf("fixture: total=%d err=%v", first.Total, err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(first.NextCursor)
	var cursor map[string]any
	if json.Unmarshal(raw, &cursor) != nil {
		t.Fatal("decode")
	}
	cursor["at"] = at.Add(-20 * time.Minute).Format(time.RFC3339Nano)
	raw, _ = json.Marshal(cursor)
	req.Cursor = base64.RawURLEncoding.EncodeToString(raw)
	next, err := f.store.ListInvestigationQueue(context.Background(), access, req, at.Add(time.Minute))
	if !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("changed evaluation time accepted under original version: first total=%d next total=%d same_version=%v evaluated_at_changed=%v", first.Total, next.Total, first.Version == next.Version, !first.EvaluatedAt.Equal(next.EvaluatedAt))
	}
	// Equivalent timezone spelling and elapsed wall time retain the fixed instant.
	raw, _ = base64.RawURLEncoding.DecodeString(first.NextCursor)
	_ = json.Unmarshal(raw, &cursor)
	cursor["at"] = at.In(time.FixedZone("CST", 8*3600)).Format(time.RFC3339Nano)
	raw, _ = json.Marshal(cursor)
	req.Cursor = base64.RawURLEncoding.EncodeToString(raw)
	continued, err := f.store.ListInvestigationQueue(context.Background(), access, req, at.Add(10*time.Minute))
	if err != nil || continued.Version != first.Version || !continued.EvaluatedAt.Equal(at) {
		t.Fatal("fixed instant/timezone continuation failed", err)
	}
	// A custom period has unchanged membership/statistics when At changes. The
	// time itself must still be bound, not only its effect on rolling statistics.
	from, to := at.Add(-48*time.Hour), at.Add(time.Hour)
	req = workspace.QueueRequest{Filters: workspace.QueueFilters{Mode: "created", Period: "custom", From: &from, To: &to}, Limit: 1}
	custom, err := f.store.ListInvestigationQueue(context.Background(), access, req, at)
	if err != nil || custom.Total != 3 {
		t.Fatal("custom period fixture", err)
	}
	raw, _ = base64.RawURLEncoding.DecodeString(custom.NextCursor)
	_ = json.Unmarshal(raw, &cursor)
	cursor["at"] = at.Add(-time.Nanosecond).Format(time.RFC3339Nano)
	raw, _ = json.Marshal(cursor)
	req.Cursor = base64.RawURLEncoding.EncodeToString(raw)
	if _, err = f.store.ListInvestigationQueue(context.Background(), access, req, at.Add(time.Minute)); !errors.Is(err, workspace.ErrConflict) {
		t.Fatal("changed At accepted when membership is unchanged", err)
	}
}

func TestQueueStatisticsOnlyChangeInvalidatesCursor(t *testing.T) {
	f := startResponsibilityProduction(t)
	reader := queueReader(t, f)
	seedQueueFindings(t, f, 1, 4)
	first := queuePage(t, reader, "limit=1&mode=created&search=Queue+item")
	if first.Total != 2 || first.NextCursor == "" {
		t.Fatal("fixture incomplete")
	}
	id := "xf_0000000000000000000001" // Outside created-period items, inside current statistics.
	applyAssessment(t, f, f.caseItem.ID, id, assessmentCommand(t, f, f.caseItem.ID, id, workspace.BasisSelection{}, nil, &workspace.ProgressPayload{State: "not_started", Reason: "Explicit old-item progress"}))
	applyAssessment(t, f, f.caseItem.ID, id, assessmentCommand(t, f, f.caseItem.ID, id, workspace.BasisSelection{}, nil, &workspace.ProgressPayload{State: "ended", Reason: "End old-item followup", Closure: &workspace.Closure{Kind: "end_followup", Explanation: "Bounded followup ended", RemainingUnknowns: "External source not observed"}}))
	if status, _ := reader.call(t, "GET", "/api/v1/investigation/queue?limit=1&mode=created&search=Queue+item&cursor="+url.QueryEscape(first.NextCursor), nil, nil); status != 409 {
		t.Fatal("changed visible statistics did not invalidate cursor")
	}
	fresh := queuePage(t, reader, "limit=1&mode=created&search=Queue+item")
	if fresh.Total != first.Total || fresh.Statistics.Current != first.Statistics.Current-1 {
		t.Fatal("wrong statistics-only fixture")
	}
}

func TestManagerQueueUnrecordedDueBoundary(t *testing.T) {
	f := startResponsibilityProduction(t)
	actor, command := responsibilityCommand(t, f, "xs_1111111111111111")
	at := time.Now().UTC().Truncate(time.Microsecond)
	zone := "UTC"
	command.Payload.DueAt, command.Payload.DueTimezone = &at, &zone
	writer := responsibilityLogin(t, f, "reviewer.one")
	if status, raw := writer.call(t, "POST", f.reviewPath()+"-actions", command, nil); status != 200 {
		t.Fatalf("followup %d %s", status, raw)
	}
	access := workspace.QueueAccess{Subject: actor.Ref, Revision: "manager-boundary", AssigneeStates: map[string]string{actor.Ref: "active"}}
	for _, delta := range []time.Duration{0, time.Microsecond} {
		page, err := f.store.ListInvestigationQueue(context.Background(), access, workspace.QueueRequest{Limit: 10}, at.Add(delta))
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("queue %v", err)
		}
		detail, err := f.store.GetFindingAssessment(context.Background(), f.caseItem.ID, f.finding.ID, at.Add(delta))
		if err != nil || page.Items[0].Progress != "unrecorded" || page.Items[0].Overdue != (delta > 0) || detail.Overdue != page.Items[0].Overdue {
			t.Fatalf("unrecorded due boundary mismatch: delta=%v page=%+v detail=%+v err=%v", delta, page.Items[0], detail, err)
		}
	}
	t.Log("unrecorded due exactly at boundary is not overdue; one microsecond later both detail and queue are overdue")
}
