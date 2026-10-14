package api_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/workspace"
)

func queueReader(t *testing.T, f *responsibilityFixture) *responsibilityClient {
	t.Helper()
	config := f.config
	config.Accounts = append(append([]access.AccountConfigEntry{}, config.Accounts...), access.AccountConfigEntry{SubjectID: "xs_5555555555555555", Username: "investigation.reader", Role: access.RoleInvestigationReader, PasswordHash: config.Accounts[0].PasswordHash})
	f.reloadAccounts(t, config)
	return responsibilityLogin(t, f, "investigation.reader")
}
func queuePage(t *testing.T, c *responsibilityClient, query string) workspace.QueuePage {
	t.Helper()
	status, raw := c.call(t, "GET", "/api/v1/investigation/queue?"+query, nil, nil)
	var p workspace.QueuePage
	if status != 200 || json.Unmarshal(raw, &p) != nil {
		t.Fatalf("queue %d: %s", status, raw)
	}
	return p
}
func seedQueueFindings(t *testing.T, f *responsibilityFixture, first, last int) {
	t.Helper()
	_, err := f.admin.Exec(`INSERT INTO case_findings(id,case_id,title,statement,classification,status,created_by_subject,created_at,updated_at) SELECT 'xf_'||lpad(i::text,22,'0'),$1,'Queue item '||i,'Synthetic human assertion','hypothesis','open','xs_1111111111111111',now()-(i%2)*interval '48 hours',now() FROM generate_series($2::int,$3::int) i`, f.caseItem.ID, first, last)
	if err != nil {
		t.Fatal(err)
	}
}
func TestInvestigationQueueCompletePagingAndBudget(t *testing.T) {
	f := startResponsibilityProduction(t)
	reader := queueReader(t, f)
	seedQueueFindings(t, f, 1, 200)
	first := queuePage(t, reader, "limit=50")
	if first.Total != 201 || first.Statistics.Current != 201 || first.Statistics.Created != 101 || first.Statistics.BasisUnrecorded != 201 {
		t.Fatalf("complete statistics: %+v", first.Statistics)
	}
	seen := map[string]bool{}
	p := first
	pages := 0
	for {
		pages++
		for _, item := range p.Items {
			if seen[item.Finding.ID] {
				t.Fatal("duplicate item")
			}
			seen[item.Finding.ID] = true
		}
		if p.NextCursor == "" {
			break
		}
		p = queuePage(t, reader, "limit=50&cursor="+url.QueryEscape(p.NextCursor))
		if p.Version != first.Version || !p.EvaluatedAt.Equal(first.EvaluatedAt) || p.Statistics != first.Statistics {
			t.Fatal("page boundary changed without data change")
		}
	}
	if len(seen) != 201 || pages != 5 {
		t.Fatalf("lost pages/items: %d/%d", pages, len(seen))
	}
	accessScope := workspace.QueueAccess{Subject: "xs_5555555555555555", Revision: "clock-test"}
	req := workspace.QueueRequest{Limit: 50}
	before := time.Now().UTC()
	clockFirst, err := f.store.ListInvestigationQueue(context.Background(), accessScope, req, before)
	if err != nil {
		t.Fatal(err)
	}
	req.Cursor = clockFirst.NextCursor
	clockNext, err := f.store.ListInvestigationQueue(context.Background(), accessScope, req, before.Add(5*time.Minute))
	if err != nil || !clockNext.EvaluatedAt.Equal(before) || clockNext.Version != clockFirst.Version {
		t.Fatal("wall clock invalidated page", err)
	}
	if status, _ := reader.call(t, "GET", "/api/v1/investigation/queue?limit=50&mode=created&cursor="+url.QueryEscape(first.NextCursor), nil, nil); status != 400 {
		t.Fatal("cursor accepted different filters")
	}
	other := responsibilityLogin(t, f, "reviewer.one")
	if status, _ := other.call(t, "GET", "/api/v1/investigation/queue?cursor="+url.QueryEscape(first.NextCursor), nil, nil); status != 400 {
		t.Fatal("cursor accepted other subject")
	}
	if _, err = f.admin.Exec(`UPDATE case_findings SET title='Changed item',version=version+1 WHERE id=$1`, f.finding.ID); err != nil {
		t.Fatal(err)
	}
	if status, _ := reader.call(t, "GET", "/api/v1/investigation/queue?cursor="+url.QueryEscape(first.NextCursor), nil, nil); status != 409 {
		t.Fatal("stale data cursor accepted")
	}
	seedQueueFindings(t, f, 201, 1999)
	selection := assessmentFixtureSources(t, f)
	creation := previewAssessmentCreation(t, f, newAssessmentCreation(t, selection))
	if _, err = f.store.CreateReview(context.Background(), assessmentActor(), creation, time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"xf_0000000000000000000001", "xf_0000000000000000000002"} {
		applyAssessment(t, f, f.caseItem.ID, id, assessmentCommand(t, f, f.caseItem.ID, id, workspace.BasisSelection{}, nil, &workspace.ProgressPayload{State: "not_started", Reason: "Begin explicit follow-up lifecycle"}))
		applyAssessment(t, f, f.caseItem.ID, id, assessmentCommand(t, f, f.caseItem.ID, id, workspace.BasisSelection{}, nil, &workspace.ProgressPayload{State: "ended", Reason: "Synthetic completed follow-up", Closure: &workspace.Closure{Kind: "end_followup", Explanation: "Bounded attempt ended", RemainingUnknowns: "Unobserved sources"}}))
	}
	if _, err = f.store.ResolveGap(context.Background(), assessmentActor(), selection.Gaps[0].GapID, workspace.ResolveGapInput{State: "investigating", Note: "Changed source in mixed performance fixture"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	// Actual normal HTTP request with 2,001 retained Findings and mixed periods.
	started := time.Now()
	large := queuePage(t, reader, "limit=100")
	elapsed := time.Since(started)
	if large.Total != 1999 || large.Statistics.Created != 1001 || large.Statistics.Reassessment != 1 || elapsed >= 3*time.Second {
		t.Fatalf("full set/budget: total=%d stats=%+v elapsed=%v", large.Total, large.Statistics, elapsed)
	}
	t.Logf("201 unique items / 5 pages; stable time and explicit stale rejection; 2001 mixed-period/state/basis rows (1999 current,1001 created,1 changed) actual HTTP elapsed=%s", elapsed)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = f.store.ListInvestigationQueue(ctx, accessScope, workspace.QueueRequest{Limit: 50}, time.Now()); err == nil {
		t.Fatal("cancel ignored")
	}
}
func TestInvestigationQueueBasisSealedAndOverdue(t *testing.T) {
	f := startResponsibilityProduction(t)
	reader := queueReader(t, f)
	writer := responsibilityLogin(t, f, "reviewer.one")
	_, due := responsibilityCommand(t, f, "xs_1111111111111111")
	at := time.Now().UTC().Add(-time.Hour)
	zone := "UTC"
	due.Payload.DueAt = &at
	due.Payload.DueTimezone = &zone
	if status, raw := writer.call(t, "POST", f.reviewPath()+"-actions", due, nil); status != 200 {
		t.Fatalf("due %d %s", status, raw)
	}
	view := assessmentCurrent(t, f, f.caseItem.ID, f.finding.ID)
	p := queuePage(t, reader, "")
	if !view.Overdue || p.Statistics.Overdue != 1 || p.Items[0].Progress != "unrecorded" || !p.Items[0].Overdue {
		t.Fatal("detail/queue nil progress deadline discrepancy")
	}
	f.config.Accounts[2].Disabled = true
	reader = queueReader(t, f)
	inactive := queuePage(t, reader, "assignee=inactive")
	if inactive.Total != 1 || inactive.Items[0].AssigneeState != "disabled" {
		t.Fatal("historical inactive assignee omitted")
	}
	selection := assessmentFixtureSources(t, f)
	creation := previewAssessmentCreation(t, f, newAssessmentCreation(t, selection))
	receipt, err := f.store.CreateReview(context.Background(), assessmentActor(), creation, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p = queuePage(t, reader, "")
	var reviewItem workspace.QueueItem
	for _, v := range p.Items {
		if v.Finding.ID == receipt.FindingID {
			reviewItem = v
		}
	}
	if reviewItem.Basis != "unchanged" {
		t.Fatalf("initial basis=%s", reviewItem.Basis)
	}
	if _, err = f.store.ResolveGap(context.Background(), assessmentActor(), selection.Gaps[0].GapID, workspace.ResolveGapInput{State: "investigating", Note: "Later source resolution"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.SealCase(context.Background(), receipt.CaseID, assessmentActor(), time.Now()); err != nil {
		t.Fatal(err)
	}
	p = queuePage(t, reader, "mode=reassessment")
	if p.Total != 1 || p.Items[0].CaseStatus != "sealed" || p.Items[0].Basis != "changed" {
		t.Fatalf("sealed changed basis %+v", p)
	}
	detail := assessmentCurrent(t, f, receipt.CaseID, receipt.FindingID)
	if p.Items[0].ReviewBasis != detail.ReviewBasis.State {
		t.Fatal("batch/single resolver diverged")
	}
	// Corruption is confined to the disposable fixture and must show unavailable.
	if _, err = f.admin.Exec(`UPDATE case_evidence_links SET event_hash_snapshot='sha256:'||repeat('a',64) WHERE finding_id=$1`, receipt.FindingID); err != nil {
		t.Fatal(err)
	}
	p = queuePage(t, reader, "mode=reassessment")
	if p.Total != 1 || p.Items[0].Basis != "unavailable" {
		t.Fatal("missing/mismatched basis silently accepted")
	}
	t.Log("nil-progress overdue agrees with detail; selected Gap changes; sealed remains current; corrupt selected link becomes unavailable")
}
func TestInvestigationQueuePersonalFiltersHTTPAndRestore(t *testing.T) {
	f := startResponsibilityProduction(t)
	reader := queueReader(t, f)
	other := responsibilityLogin(t, f, "reviewer.two")
	denied := responsibilityLogin(t, f, "assignee.reader")
	base := "/api/v1/investigation/queue-filters"
	cmd := workspace.QueueFilterCommand{OperationID: randomTestUUID(t), Name: "My pending", Schema: workspace.QueueFilterSchema, Filters: workspace.QueueFilters{Mode: "current", Period: "7d", Sort: "due_asc"}}
	if status, _ := denied.call(t, "GET", "/api/v1/investigation/queue", nil, nil); status != 403 {
		t.Fatal("workbench reader queue access")
	}
	if status, _ := denied.call(t, "POST", base, cmd, nil); status != 403 {
		t.Fatal("workbench reader preferences access")
	}
	if status, _ := reader.call(t, "POST", base, cmd, map[string]string{"X-CSRF-Token": ""}); status != 403 {
		t.Fatal("missing CSRF accepted")
	}
	status, raw := reader.call(t, "POST", base, cmd, nil)
	var v workspace.QueueFilter
	if status != 201 || json.Unmarshal(raw, &v) != nil || v.OwnerSubject != "xs_5555555555555555" || v.Revision != 1 {
		t.Fatalf("create %d %s", status, raw)
	}
	first := append([]byte{}, raw...)
	if status, replay := reader.call(t, "POST", base, cmd, nil); status != 201 || !bytes.Equal(first, replay) {
		t.Fatal("create replay changed")
	}
	cmd.Name = "different"
	if status, _ := reader.call(t, "POST", base, cmd, nil); status != 409 {
		t.Fatal("same key different payload")
	}
	cmd.OperationID = randomTestUUID(t)
	cmd.ExpectedRevision = 1
	if status, _ := other.call(t, "PUT", base+"/"+v.ID, cmd, nil); status != 404 {
		t.Fatal("other owner modified")
	}
	if status, raw := other.call(t, "GET", base, nil, nil); status != 200 || string(raw) != "{\"items\":[]}\n" {
		t.Fatalf("other owner listed %s", raw)
	}
	status, raw = reader.call(t, "PUT", base+"/"+v.ID, cmd, nil)
	if status != 200 || json.Unmarshal(raw, &v) != nil || v.Revision != 2 {
		t.Fatalf("update %d %s", status, raw)
	}
	stale := cmd
	stale.OperationID = randomTestUUID(t)
	if status, _ := reader.call(t, "PUT", base+"/"+v.ID, stale, nil); status != 409 {
		t.Fatal("stale revision accepted")
	}
	cmd.ExpectedRevision = 2
	cmd.OperationID = randomTestUUID(t)
	status, deleted := reader.call(t, "DELETE", base+"/"+v.ID, cmd, nil)
	if status != 200 || json.Unmarshal(deleted, &v) != nil || !v.Deleted || v.Revision != 3 {
		t.Fatalf("delete %d %s", status, deleted)
	}
	if status, replay := reader.call(t, "DELETE", base+"/"+v.ID, cmd, nil); status != 200 || !bytes.Equal(deleted, replay) {
		t.Fatal("deleted retry changed")
	}
	if err := db.ValidateInvestigationQueueRestore(context.Background(), f.admin); err != nil {
		t.Fatal(err)
	}
	runtime, err := sql.Open("pgx", f.workspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	for _, query := range []string{`UPDATE investigation_queue_filter_operations SET request_sha256=repeat('a',64)`, `DELETE FROM investigation_queue_filter_operations`, `DELETE FROM investigation_queue_filters`} {
		if _, err = runtime.Exec(query); err == nil {
			t.Fatal("runtime history/delete grant exceeded")
		}
	}
	if _, err = f.admin.Exec(`UPDATE investigation_queue_filters SET record=jsonb_set(record,'{name}','"tampered"') WHERE id=$1`, v.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.ValidateInvestigationQueueRestore(context.Background(), f.admin); err == nil {
		t.Fatal("tampered projection restored")
	}
	if status, _ := reader.call(t, "POST", "/api/v1/cases/"+f.caseItem.ID+"/findings", map[string]string{"title": "Forbidden", "statement": "Forbidden", "classification": "hypothesis"}, nil); status != 403 {
		t.Fatal("preferences granted Finding writes")
	}
	for _, body := range []string{`{"operation_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","expected_revision":0,"schema_version":"xinsight.investigation-queue-filter.v1","name":"Forged","filters":{"mode":"current","period":"24h","sort":"created_desc"},"owner_subject":"xs_2222222222222222"}`, `{"operation_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","expected_revision":0,"schema_version":"xinsight.investigation-queue-filter.v1","name":"Duplicate","filters":{"mode":"current","mode":"created","period":"24h","sort":"created_desc"}}`} {
		if status, _ := reader.call(t, "POST", base, []byte(body), nil); status != 400 {
			t.Fatal("forged owner or duplicate input accepted")
		}
	}
	if status, _ := reader.call(t, "POST", "/api/v1/access/logout", map[string]any{}, nil); status != 204 {
		t.Fatal("logout failed")
	}
	for _, path := range []string{base, "/api/v1/investigation/queue", "/api/v1/cases/" + f.caseItem.ID, "/api/v1/cases/" + f.caseItem.ID + "/evidence-pack.json"} {
		if status, _ := reader.call(t, "GET", path, nil, nil); status != 401 {
			t.Fatal("expired session access", path, status)
		}
	}
	t.Log("real reader own CRUD, other-owner isolation, CSRF, stale/conflicting retry, tombstone replay, least grants and restore corruption rejection")
}
func TestInvestigationQueueFilterConcurrentOperations(t *testing.T) {
	f := startResponsibilityProduction(t)
	ctx := context.Background()
	owner := "xs_1111111111111111"
	c := workspace.QueueFilterCommand{OperationID: randomTestUUID(t), Name: "Parallel", Schema: workspace.QueueFilterSchema}
	var wg sync.WaitGroup
	results := make(chan workspace.QueueFilter, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := f.store.ApplyQueueFilter(ctx, owner, "", "create", c, time.Now())
			results <- v
			errs <- e
		}()
	}
	wg.Wait()
	a, b := <-results, <-results
	if e := <-errs; e != nil {
		t.Fatal(e)
	}
	if e := <-errs; e != nil {
		t.Fatal(e)
	}
	if !workspace.SameQueueValue(a, b) {
		t.Fatal("same key multiple receipts")
	}
	results = make(chan workspace.QueueFilter, 2)
	errs = make(chan error, 2)
	for i := 0; i < 2; i++ {
		next := c
		next.OperationID = randomTestUUID(t)
		next.ExpectedRevision = 1
		next.Name = fmt.Sprintf("Revision %d", i)
		wg.Add(1)
		go func(command workspace.QueueFilterCommand) {
			defer wg.Done()
			v, e := f.store.ApplyQueueFilter(ctx, owner, a.ID, "update", command, time.Now())
			results <- v
			errs <- e
		}(next)
	}
	wg.Wait()
	success, conflict := 0, 0
	for i := 0; i < 2; i++ {
		e := <-errs
		if e == nil {
			success++
		} else if errors.Is(e, workspace.ErrConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("concurrent revisions", success, conflict)
	}
	if err := db.ValidateInvestigationQueueRestore(ctx, f.admin); err != nil {
		t.Fatal(err)
	}
}

func TestInvestigationQueueBrowserHarness(t *testing.T) {
	if os.Getenv("XINSIGHT_QUEUE_BROWSER") != "1" {
		t.Skip("explicit queue browser harness required")
	}
	output := os.Getenv("XINSIGHT_QUEUE_BROWSER_OUTPUT")
	if !filepath.IsAbs(output) {
		t.Fatal("absolute output required")
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	f := startResponsibilityProduction(t)
	queueReader(t, f)
	seedQueueFindings(t, f, 1, 200)
	selection := assessmentFixtureSources(t, f)
	creation := newAssessmentCreation(t, selection)
	creation.NewCase.Title = "队列：可复核的调查依据"
	creation = previewAssessmentCreation(t, f, creation)
	receipt, err := f.store.CreateReview(context.Background(), assessmentActor(), creation, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ResolveGap(context.Background(), assessmentActor(), selection.Gaps[0].GapID, workspace.ResolveGapInput{State: "investigating", Note: "Local synthetic later source resolution for queue review"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(map[string]any{"base_url": f.server.URL, "case_id": receipt.CaseID, "finding_id": receipt.FindingID, "original_case_id": f.caseItem.ID, "original_finding_id": f.finding.ID, "evidence_boundary": "local synthetic / native PG16 / production TLS handler / embedded frontend"}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(output, "ready.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-t.Context().Done():
			t.Fatal("browser cancelled")
		case <-ticker.C:
			if _, err = os.Stat(filepath.Join(output, "stop")); err == nil {
				return
			}
		}
	}
}

func TestInvestigationQueueFinalProductionReadinessAndAssets(t *testing.T) {
	f := startResponsibilityProduction(t)
	response, err := f.server.Client().Get(f.server.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || !bytes.Contains(raw, []byte(`"database_schema":"`+buildinfo.DatabaseSchemaMax+`"`)) {
		t.Fatalf("final readiness=%d %s err=%v", response.StatusCode, raw, err)
	}
	manifestRaw, err := os.ReadFile("../../internal/webui/dist/asset-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Entries []struct {
			Path string `json:"path"`
			Size int    `json:"size_bytes"`
			Hash string `json:"sha256"`
		}
	}
	if json.Unmarshal(manifestRaw, &manifest) != nil || len(manifest.Entries) != 3 {
		t.Fatal("manifest")
	}
	for _, entry := range manifest.Entries {
		path := "/" + entry.Path
		if entry.Path == "index.html" {
			path = "/investigation/queue?mode=created"
		}
		response, err = f.server.Client().Get(f.server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, e := io.ReadAll(response.Body)
		response.Body.Close()
		digest := sha256.Sum256(body)
		if e != nil || response.StatusCode != 200 || len(body) != entry.Size || fmt.Sprintf("sha256:%x", digest) != entry.Hash {
			t.Fatalf("embedded asset mismatch: %s", entry.Path)
		}
		t.Logf("actual production HTTP %s size=%d SHA=%s", path, len(body), entry.Hash)
	}
	t.Log("schema31 readyz=200; deep-link HTML and final embedded CSS/JS match the pinned-runtime build manifest")
}
