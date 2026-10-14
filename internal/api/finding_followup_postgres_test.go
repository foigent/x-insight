package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/workspace"
	"github.com/jackc/pgx/v5"
)

type responsibilityFixture struct {
	server       *httptest.Server
	admin        *sql.DB
	store        *db.WorkspaceStore
	service      *access.Service
	caseItem     workspace.Case
	finding      workspace.Finding
	dsn          string
	workspaceDSN string
	reader       *db.EvidenceStore
	config       access.AccountConfig
}

type lostResponsibilityResponse struct {
	base    http.RoundTripper
	mu      sync.Mutex
	dropped bool
	body    []byte
}

func (transport *lostResponsibilityResponse) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if !transport.dropped && request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/review-actions") && response.StatusCode == 200 {
		transport.body, err = io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			return nil, err
		}
		transport.dropped = true
		return nil, errors.New("synthetic connection loss after server committed the response")
	}
	return response, nil
}

func TestResponsibilityRetryAfterCommittedResponseLoss(t *testing.T) {
	f := startResponsibilityProduction(t)
	client := responsibilityLogin(t, f, "reviewer.one")
	_, command := responsibilityCommand(t, f, "xs_1111111111111111")
	transport := &lostResponsibilityResponse{base: client.client.Transport}
	client.client.Transport = transport
	raw, _ := json.Marshal(command)
	request, err := http.NewRequest("POST", client.base+f.reviewPath()+"-actions", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", client.base)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", client.csrf)
	if response, err := client.client.Do(request); err == nil {
		response.Body.Close()
		t.Fatal("expected controlled response loss")
	}
	var count int
	if err = f.admin.QueryRow(`SELECT count(*) FROM case_finding_actions WHERE case_id=$1`, f.caseItem.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("commit before loss count=%d err=%v", count, err)
	}
	status, replay := client.call(t, "POST", f.reviewPath()+"-actions", command, nil)
	if status != 200 || !bytes.Equal(replay, transport.body) {
		t.Fatal("lost response retry changed original receipt")
	}
	if err = f.admin.QueryRow(`SELECT count(*) FROM case_finding_actions WHERE case_id=$1`, f.caseItem.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("retry duplicated action")
	}
	t.Log("Actual HTTP commit verified before controlled response loss; same operation retry returned exact original bytes, history count stayed 1")
}

func responsibilityLockGate(t *testing.T, f *responsibilityFixture, trigger string) (func(), func(int)) {
	t.Helper()
	ctx := context.Background()
	connection, err := f.admin.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = connection.ExecContext(ctx, `SELECT pg_advisory_lock(8362619)`); err != nil {
		connection.Close()
		t.Fatal(err)
	}
	_, err = f.admin.Exec(`CREATE FUNCTION xi_c_test_gate() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(8362619); RETURN NEW; END $$;` + trigger)
	if err != nil {
		connection.ExecContext(ctx, `SELECT pg_advisory_unlock(8362619)`)
		connection.Close()
		t.Fatal(err)
	}
	var once sync.Once
	release := func() {
		once.Do(func() { connection.ExecContext(ctx, `SELECT pg_advisory_unlock(8362619)`); connection.Close() })
	}
	t.Cleanup(release)
	wait := func(want int) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for {
			var count int
			err := f.admin.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND usename='xinsight_workspace' AND wait_event_type='Lock'`).Scan(&count)
			if err != nil {
				t.Fatal(err)
			}
			if count >= want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("actual workspace lock gate not reached")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	return release, wait
}

func TestResponsibilitySealAndLegacyAnnotationRaces(t *testing.T) {
	t.Run("responsibility commits before seal snapshot", func(t *testing.T) {
		f := startResponsibilityProduction(t)
		ctx := context.Background()
		actor, command := responsibilityCommand(t, f, "xs_1111111111111111")
		release, wait := responsibilityLockGate(t, f, `CREATE TRIGGER xi_c_test_gate AFTER INSERT ON case_finding_actions FOR EACH ROW EXECUTE FUNCTION xi_c_test_gate()`)
		defer release()
		changed := make(chan error, 1)
		sealed := make(chan error, 1)
		go func() {
			_, e := f.store.ApplyFindingFollowup(ctx, f.caseItem.ID, f.finding.ID, actor, command, workspace.AssigneeLookup{SubjectID: "xs_3333333333333333", Name: "assignee.reader", Active: true}, time.Now())
			changed <- e
		}()
		wait(1)
		go func() { _, e := f.store.SealCase(ctx, f.caseItem.ID, actor, time.Now()); sealed <- e }()
		wait(2)
		release()
		if err := <-changed; err != nil {
			t.Fatal(err)
		}
		if err := <-sealed; err != nil {
			if !errors.Is(err, workspace.ErrConflict) {
				t.Fatal(err)
			}
			if _, e := f.store.GetCase(ctx, f.caseItem.ID); e != nil {
				t.Fatal(e)
			}
			if _, e := f.store.SealCase(ctx, f.caseItem.ID, actor, time.Now()); e != nil {
				t.Fatal(e)
			}
			t.Log("explicit fresh seal after serializable conflict")
		}
		record, e := f.store.GetCase(ctx, f.caseItem.ID)
		if e != nil {
			t.Fatal(e)
		}
		var pack workspace.EvidencePack
		if json.Unmarshal(record.Case.SealedPack, &pack) != nil || len(pack.Responsibility.Actions) != 1 || pack.Responsibility.Followups[0].Version != 1 {
			t.Fatal("seal omitted committed responsibility or history")
		}
	})
	t.Run("seal commits before new responsibility", func(t *testing.T) {
		f := startResponsibilityProduction(t)
		ctx := context.Background()
		actor, command := responsibilityCommand(t, f, "xs_1111111111111111")
		release, wait := responsibilityLockGate(t, f, `CREATE TRIGGER xi_c_test_gate AFTER UPDATE ON investigation_cases FOR EACH ROW WHEN (NEW.status='sealed') EXECUTE FUNCTION xi_c_test_gate()`)
		defer release()
		sealed := make(chan error, 1)
		changed := make(chan error, 1)
		go func() { _, e := f.store.SealCase(ctx, f.caseItem.ID, actor, time.Now()); sealed <- e }()
		wait(1)
		go func() {
			_, e := f.store.ApplyFindingFollowup(ctx, f.caseItem.ID, f.finding.ID, actor, command, workspace.AssigneeLookup{SubjectID: "xs_3333333333333333", Name: "assignee.reader", Active: true}, time.Now())
			changed <- e
		}()
		wait(2)
		release()
		if err := <-sealed; err != nil {
			t.Fatal(err)
		}
		if err := <-changed; !errors.Is(err, workspace.ErrSealed) {
			t.Fatalf("new action after seal: %v", err)
		}
		review, e := f.store.GetFindingReview(ctx, f.caseItem.ID, f.finding.ID)
		if e != nil || review.Followup.Recorded || len(review.History.Items) != 0 {
			t.Fatal("rejected action left partial state")
		}
	})
	t.Run("legacy note shares the parent lock", func(t *testing.T) {
		f := startResponsibilityProduction(t)
		ctx := context.Background()
		actor, _ := responsibilityCommand(t, f, "xs_1111111111111111")
		release, wait := responsibilityLockGate(t, f, `CREATE TRIGGER xi_c_test_gate AFTER UPDATE ON investigation_cases FOR EACH ROW WHEN (NEW.status='open') EXECUTE FUNCTION xi_c_test_gate()`)
		defer release()
		noted := make(chan error, 1)
		sealed := make(chan error, 1)
		go func() {
			_, e := f.store.CreateAnnotation(ctx, f.caseItem.ID, actor, workspace.CreateAnnotationInput{TargetKind: "case", TargetRef: f.caseItem.ID, Body: "Legacy note remains independently meaningful"}, time.Now(), nil)
			noted <- e
		}()
		wait(1)
		go func() { _, e := f.store.SealCase(ctx, f.caseItem.ID, actor, time.Now()); sealed <- e }()
		wait(2)
		release()
		if err := <-noted; err != nil {
			t.Fatal(err)
		}
		if err := <-sealed; err != nil {
			if !errors.Is(err, workspace.ErrConflict) {
				t.Fatal(err)
			}
			if _, e := f.store.GetCase(ctx, f.caseItem.ID); e != nil {
				t.Fatal(e)
			}
			if _, e := f.store.SealCase(ctx, f.caseItem.ID, actor, time.Now()); e != nil {
				t.Fatal(e)
			}
		}
		record, e := f.store.GetCase(ctx, f.caseItem.ID)
		if e != nil {
			t.Fatal(e)
		}
		var pack workspace.EvidencePack
		if json.Unmarshal(record.Case.SealedPack, &pack) != nil || len(pack.Annotations) != 1 {
			t.Fatal("legacy note omitted from seal")
		}
	})
}

func TestResponsibilityCancellationRollsBackActionAndVersion(t *testing.T) {
	f := startResponsibilityProduction(t)
	actor, command := responsibilityCommand(t, f, "xs_1111111111111111")
	release, wait := responsibilityLockGate(t, f, `CREATE TRIGGER xi_c_test_gate AFTER INSERT ON case_finding_actions FOR EACH ROW EXECUTE FUNCTION xi_c_test_gate()`)
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := f.store.ApplyFindingFollowup(ctx, f.caseItem.ID, f.finding.ID, actor, command, workspace.AssigneeLookup{SubjectID: "xs_3333333333333333", Name: "assignee.reader", Active: true}, time.Now())
		done <- err
	}()
	wait(1)
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancelled write succeeded")
	}
	release()
	review, err := f.store.GetFindingReview(context.Background(), f.caseItem.ID, f.finding.ID)
	if err != nil || review.Followup.Recorded || review.CaseVersion != command.ExpectedCaseVersion || len(review.History.Items) != 0 {
		t.Fatal("cancelled action partially persisted")
	}
}

type responsibilityClient struct {
	client     *http.Client
	base, csrf string
}

func responsibilityLogin(t *testing.T, f *responsibilityFixture, username string) *responsibilityClient {
	t.Helper()
	clientValue := *f.server.Client()
	client := &clientValue
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client.Jar = jar
	c := &responsibilityClient{client: client, base: f.server.URL}
	status, raw := c.call(t, "POST", "/api/v1/access/login", map[string]string{"username": username, "password": "synthetic responsibility password"}, nil)
	if status != 200 {
		t.Fatalf("synthetic login status=%d", status)
	}
	var view struct {
		CSRF string `json:"csrf_token"`
	}
	if json.Unmarshal(raw, &view) != nil || view.CSRF == "" {
		t.Fatal("missing CSRF")
	}
	c.csrf = view.CSRF
	return c
}

func (c *responsibilityClient) call(t *testing.T, method, path string, body any, override map[string]string) (int, []byte) {
	t.Helper()
	var raw []byte
	var err error
	if body != nil {
		if data, ok := body.([]byte); ok {
			raw = data
		} else {
			raw, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	req, err := http.NewRequest(method, c.base+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", c.base)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", c.csrf)
	for k, v := range override {
		req.Header.Set(k, v)
	}
	response, err := c.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err = io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, raw
}

func (f *responsibilityFixture) reviewPath() string {
	return "/api/v1/cases/" + f.caseItem.ID + "/findings/" + f.finding.ID + "/review"
}

func responsibilityCommand(t *testing.T, f *responsibilityFixture, actorID string) (workspace.Actor, workspace.FollowupCommand) {
	t.Helper()
	review, err := f.store.GetFindingReview(context.Background(), f.caseItem.ID, f.finding.ID)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := workspace.HumanActor(actorID)
	if err != nil {
		t.Fatal(err)
	}
	id := "xs_3333333333333333"
	return actor, workspace.FollowupCommand{Operation: "update_followup", OperationID: randomTestUUID(t), ExpectedVersion: review.Followup.Version, ExpectedCaseVersion: review.CaseVersion, Payload: workspace.FollowupPayload{AssigneeSubjectID: &id, Reason: "Synthetic responsibility decision"}}
}

func TestResponsibilityActualHTTPPermissionsAndReceipts(t *testing.T) {
	f := startResponsibilityProduction(t)
	one := responsibilityLogin(t, f, "reviewer.one")
	reader := responsibilityLogin(t, f, "assignee.reader")
	_, command := responsibilityCommand(t, f, "xs_1111111111111111")
	status, raw := one.call(t, "GET", f.reviewPath(), nil, nil)
	var before workspace.FollowupReview
	if status != 200 || json.Unmarshal(raw, &before) != nil || before.Followup.Recorded || before.Followup.Version != 0 {
		t.Fatalf("legacy Finding read: status=%d recorded=%t version=%d", status, before.Followup.Recorded, before.Followup.Version)
	}
	candidates := "/api/v1/security/assignee-candidates?case_id=" + f.caseItem.ID + "&finding_id=" + f.finding.ID
	status, raw = one.call(t, "GET", candidates, nil, nil)
	if status != 200 {
		t.Fatalf("candidates status=%d", status)
	}
	for _, forbidden := range []string{"password", "hash", "csrf", "token", "session", "role"} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatalf("directory exposed %s", forbidden)
		}
	}
	var directory struct {
		Items    []access.LocalSubject
		Total    int
		Complete bool
	}
	if json.Unmarshal(raw, &directory) != nil || directory.Total != 4 || !directory.Complete {
		t.Fatal("directory is not complete and bounded")
	}
	for _, path := range []string{f.reviewPath(), candidates} {
		if status, _ = reader.call(t, "GET", path, nil, nil); status != 403 {
			t.Fatalf("assigned reader obtained Case access: %d", status)
		}
	}
	for _, headers := range []map[string]string{{"X-CSRF-Token": ""}, {"X-CSRF-Token": "invalid"}, {"Origin": "https://unrelated.invalid"}} {
		if status, _ = one.call(t, "POST", f.reviewPath()+"-actions", command, headers); status != 403 {
			t.Fatalf("mutation authorization bypass: %d", status)
		}
	}
	if status, _ = reader.call(t, "POST", f.reviewPath()+"-actions", command, nil); status != 403 {
		t.Fatalf("reader wrote: %d", status)
	}
	malformed := map[string]any{"operation": command.Operation, "operation_id": command.OperationID, "expected_version": 0, "expected_case_version": command.ExpectedCaseVersion, "payload": command.Payload, "actor_subject_id": "xs_2222222222222222"}
	if status, _ = one.call(t, "POST", f.reviewPath()+"-actions", malformed, nil); status != 400 {
		t.Fatalf("forged actor accepted: %d", status)
	}
	disabled := command
	id := "xs_4444444444444444"
	disabled.Payload.AssigneeSubjectID = &id
	if status, _ = one.call(t, "POST", f.reviewPath()+"-actions", disabled, nil); status != 400 {
		t.Fatalf("disabled new assignee: %d", status)
	}
	status, first := one.call(t, "POST", f.reviewPath()+"-actions", command, nil)
	if status != 200 {
		t.Fatalf("save status=%d body=%s", status, first)
	}
	var receipt workspace.FollowupAction
	if json.Unmarshal(first, &receipt) != nil || receipt.Current.Version != 1 || receipt.ActorSubjectID != "xs_1111111111111111" {
		t.Fatal("invalid receipt")
	}
	if receipt.Current.AssigneeName != "assignee.reader" {
		t.Fatal("missing non-secret snapshot")
	}
	_, second := responsibilityCommand(t, f, "xs_1111111111111111")
	second.Payload.AssigneeSubjectID = nil
	second.Payload.Reason = "Explicitly leave unassigned"
	if status, _ = one.call(t, "POST", f.reviewPath()+"-actions", second, nil); status != 200 {
		t.Fatalf("second action=%d", status)
	}
	status, replay := one.call(t, "POST", f.reviewPath()+"-actions", command, nil)
	if status != 200 || !bytes.Equal(first, replay) {
		t.Fatal("retry did not return original exact receipt")
	}
	status, raw = one.call(t, "GET", f.reviewPath(), nil, nil)
	var current workspace.FollowupReview
	if status != 200 || json.Unmarshal(raw, &current) != nil || current.Followup.Version != 2 || current.Followup.AssigneeSubjectID != nil {
		t.Fatal("retry replaced current projection")
	}
	changed := command
	changed.Payload.Reason = "different content"
	if status, _ = one.call(t, "POST", f.reviewPath()+"-actions", changed, nil); status != 409 {
		t.Fatalf("same key changed content accepted: %d", status)
	}
	status, raw = one.call(t, "GET", f.reviewPath()+"/history?through_version=2&limit=1", nil, nil)
	var history workspace.FollowupHistory
	if status != 200 || json.Unmarshal(raw, &history) != nil || len(history.Items) != 1 || history.NextBefore == nil || *history.NextBefore != 2 {
		t.Fatal("history first page")
	}
	_, appended := responsibilityCommand(t, f, "xs_1111111111111111")
	appended.Payload.Reason = "A later append must not move the fixed history boundary"
	if status, _ = one.call(t, "POST", f.reviewPath()+"-actions", appended, nil); status != 200 {
		t.Fatal("concurrent history append failed")
	}
	status, raw = one.call(t, "GET", f.reviewPath()+"/history?through_version=2&before_version=2&limit=1", nil, nil)
	if status != 200 || json.Unmarshal(raw, &history) != nil || len(history.Items) != 1 || history.Items[0].Current.Version != 1 || history.NextBefore != nil {
		t.Fatal("history second page")
	}
	if status, _ = one.call(t, "GET", f.reviewPath()+"/history?through_version=999", nil, nil); status != 400 {
		t.Fatal("future history bound accepted")
	}
	other, err := f.store.CreateCase(context.Background(), workspace.Actor{Kind: "human", Ref: "xs_1111111111111111"}, workspace.CreateCaseInput{Title: "Other Case"}, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	wrong := strings.Replace(f.reviewPath(), f.caseItem.ID, other.ID, 1)
	if status, _ = one.call(t, "GET", wrong, nil, nil); status != 404 {
		t.Fatalf("cross-Case Finding read=%d", status)
	}
	if status, _ = one.call(t, "POST", wrong+"-actions", command, nil); status != 404 {
		t.Fatalf("cross-Case Finding write=%d", status)
	}
	if status, _ = one.call(t, "POST", "/api/v1/cases/"+f.caseItem.ID+"/seal", map[string]any{}, nil); status != 200 {
		t.Fatalf("seal status=%d", status)
	}
	if status, replay = one.call(t, "POST", f.reviewPath()+"-actions", command, nil); status != 200 || !bytes.Equal(first, replay) {
		t.Fatal("sealed committed retry rejected or changed")
	}
	_, newCommand := responsibilityCommand(t, f, "xs_1111111111111111")
	if status, _ = one.call(t, "POST", f.reviewPath()+"-actions", newCommand, nil); status != 409 {
		t.Fatal("sealed new action accepted")
	}
	status, raw = one.call(t, "GET", "/api/v1/cases/"+f.caseItem.ID+"/evidence-pack.json", nil, nil)
	var pack workspace.EvidencePack
	if status != 200 || json.Unmarshal(raw, &pack) != nil || pack.SchemaVersion != workspace.EvidencePackSchema || len(pack.Responsibility.Actions) != 3 {
		t.Fatal("seal omitted responsibility history")
	}
	sessionURL, _ := url.Parse(one.base)
	oldCookies := one.client.Jar.Cookies(sessionURL)
	if status, _ = one.call(t, "POST", "/api/v1/access/logout", map[string]any{}, nil); status != 204 {
		t.Fatalf("logout=%d", status)
	}
	one.client.Jar.SetCookies(sessionURL, oldCookies) // replay the actual revoked session, not merely an empty cookie jar
	if status, _ = one.call(t, "POST", f.reviewPath()+"-actions", command, nil); status != 401 {
		t.Fatalf("revoked session replay=%d", status)
	}
	t.Log("Actual TLS/human/CSRF, role denial, finite directory, persistence, original receipt replay, history pagination, cross-Case denial and v2 seal passed")
}

func TestResponsibilityConcurrentKeysAndCommitRollback(t *testing.T) {
	f := startResponsibilityProduction(t)
	ctx := context.Background()
	actor, command := responsibilityCommand(t, f, "xs_1111111111111111")
	lookup := workspace.AssigneeLookup{SubjectID: "xs_3333333333333333", Name: "assignee.reader", Active: true}
	type answer struct {
		result workspace.FollowupAction
		err    error
	}
	runPair := func(a, b workspace.FollowupCommand, other workspace.Actor) []answer {
		barrier, err := f.admin.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer barrier.Rollback()
		if _, err = barrier.Exec(`SELECT id FROM investigation_cases WHERE id=$1 FOR UPDATE`, f.caseItem.ID); err != nil {
			t.Fatal(err)
		}
		ch := make(chan answer, 2)
		for i, c := range []workspace.FollowupCommand{a, b} {
			person := actor
			if i == 1 {
				person = other
			}
			go func(command workspace.FollowupCommand, who workspace.Actor) {
				v, e := f.store.ApplyFindingFollowup(ctx, f.caseItem.ID, f.finding.ID, who, command, lookup, time.Now())
				ch <- answer{v, e}
			}(c, person)
		}
		deadline := time.Now().Add(8 * time.Second)
		for {
			var count int
			if err = f.admin.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND usename='xinsight_workspace' AND wait_event_type='Lock'`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count >= 2 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("two actual workspace connections did not reach lock barrier")
			}
			time.Sleep(20 * time.Millisecond)
		}
		if err = barrier.Commit(); err != nil {
			t.Fatal(err)
		}
		return []answer{<-ch, <-ch}
	}
	answers := runPair(command, command, actor)
	for _, a := range answers {
		if a.err != nil || a.result.Current.Version != 1 {
			t.Fatalf("same-key parallel result: %v", a.err)
		}
	}
	one, _ := json.Marshal(answers[0].result)
	two, _ := json.Marshal(answers[1].result)
	if !bytes.Equal(one, two) {
		t.Fatal("parallel same key receipts differ")
	}
	actor, command = responsibilityCommand(t, f, actor.Ref)
	different := command
	different.OperationID = randomTestUUID(t)
	other := workspace.Actor{Kind: "human", Ref: "xs_2222222222222222"}
	answers = runPair(command, different, other)
	success, conflicts := 0, 0
	for _, a := range answers {
		if a.err == nil {
			success++
		} else if errors.Is(a.err, workspace.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(a.err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("different-key outcomes success=%d conflicts=%d", success, conflicts)
	}
	actor, command = responsibilityCommand(t, f, actor.Ref)
	changedContent := command
	changedContent.Payload.Reason = "Concurrent different content with the same operation identity"
	answers = runPair(command, changedContent, actor)
	success, conflicts = 0, 0
	for _, a := range answers {
		if a.err == nil {
			success++
		} else if errors.Is(a.err, workspace.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(a.err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("same key different content: success=%d conflicts=%d", success, conflicts)
	}
	before, err := f.store.GetFindingReview(ctx, f.caseItem.ID, f.finding.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.admin.Exec(`CREATE FUNCTION xi_c_test_commit_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic deferred commit failure'; END $$;
CREATE CONSTRAINT TRIGGER xi_c_test_commit_failure AFTER INSERT ON case_finding_actions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION xi_c_test_commit_failure()`)
	if err != nil {
		t.Fatal(err)
	}
	actor, command = responsibilityCommand(t, f, actor.Ref)
	if _, err = f.store.ApplyFindingFollowup(ctx, f.caseItem.ID, f.finding.ID, actor, command, lookup, time.Now()); err == nil {
		t.Fatal("deferred commit failure was ignored")
	}
	if _, err = f.admin.Exec(`DROP TRIGGER xi_c_test_commit_failure ON case_finding_actions; DROP FUNCTION xi_c_test_commit_failure()`); err != nil {
		t.Fatal(err)
	}
	after, err := f.store.GetFindingReview(ctx, f.caseItem.ID, f.finding.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.CaseVersion != before.CaseVersion || after.Followup.Version != before.Followup.Version || len(after.History.Items) != len(before.History.Items) {
		t.Fatal("failed commit left partial projection/history/version")
	}
	if _, err = f.store.ApplyFindingFollowup(ctx, f.caseItem.ID, f.finding.ID, actor, command, lookup, time.Now()); err != nil {
		t.Fatalf("retry after rolled-back operation failed: %v", err)
	}
	connection, err := sql.Open("pgx", f.workspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	for _, statement := range []string{`UPDATE case_finding_actions SET request_sha256=repeat('a',64)`, `DELETE FROM case_finding_actions`, `TRUNCATE case_finding_actions`} {
		if _, err = connection.Exec(statement); err == nil {
			t.Fatalf("workspace could mutate history with %s", strings.Fields(statement)[0])
		}
	}
	var count int
	if err = f.admin.QueryRow(`SELECT count(*) FROM case_finding_actions WHERE case_id=$1`, f.caseItem.ID).Scan(&count); err != nil || count != 4 {
		t.Fatalf("history count=%d err=%v", count, err)
	}
	t.Log(fmt.Sprintf("Actual two-connection lock barriers: same key 1 commit/2 identical receipts; distinct keys and same-key different content each 1 success/1 conflict; deferred commit rollback; final %d history rows", count))
}

func responsibilityDatabase(t *testing.T) (context.Context, string, *sql.DB) {
	t.Helper()
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN required")
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	// This fixture may operate only on the explicitly selected local synthetic instance.
	if u.Hostname() != "127.0.0.1" || os.Getenv("XINSIGHT_RESPONSIBILITY_LOCAL_TEST") != "1" {
		t.Fatal("dedicated loopback responsibility test instance required")
	}
	control, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	name := "xic_test_" + strings.ReplaceAll(randomTestUUID(t), "-", "")
	ident := pgx.Identifier{name}.Sanitize()
	if _, err = control.Exec("CREATE DATABASE " + ident); err != nil {
		control.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer control.Close()
		if _, e := control.Exec("DROP DATABASE " + ident + " WITH (FORCE)"); e != nil {
			t.Error(e)
		}
	})
	u.Path = "/" + name
	dsn := u.String()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	return ctx, dsn, admin
}

func startResponsibilityProduction(t *testing.T) *responsibilityFixture {
	t.Helper()
	ctx, dsn, admin := responsibilityDatabase(t)
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	_, workspaceDSN, readerDSN := prepareWorkbenchRuntimeRoles(t, ctx, dsn)
	store, err := db.OpenWorkspaceStore(ctx, workspaceDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	reader, err := db.OpenEvidenceStore(ctx, readerDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close() })
	hash, err := access.DerivePasswordHash([]byte("synthetic responsibility password"))
	if err != nil {
		t.Fatal(err)
	}
	config := access.AccountConfig{SchemaVersion: access.AccountConfigSchemaV2, Accounts: []access.AccountConfigEntry{
		{SubjectID: "xs_1111111111111111", Username: "reviewer.one", Role: access.RoleInvestigator, PasswordHash: hash},
		{SubjectID: "xs_2222222222222222", Username: "reviewer.two", Role: access.RoleOperator, PasswordHash: hash},
		{SubjectID: "xs_3333333333333333", Username: "assignee.reader", Role: access.RoleWorkbenchReader, PasswordHash: hash},
		{SubjectID: "xs_4444444444444444", Username: "disabled.owner", Role: access.RoleInvestigator, Disabled: true, PasswordHash: hash},
	}}
	actor, _ := workspace.HumanActor("xs_1111111111111111")
	item, err := store.CreateCase(ctx, actor, workspace.CreateCaseInput{Title: "责任验证 Case", Description: "Synthetic local responsibility fixture"}, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	finding, err := store.CreateFinding(ctx, item.ID, actor, workspace.CreateFindingInput{Title: "待补证事项", Statement: "Only a synthetic human assertion", Classification: "hypothesis"}, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	f := &responsibilityFixture{admin: admin, store: store, reader: reader, config: config, caseItem: item, finding: finding, dsn: dsn, workspaceDSN: workspaceDSN}
	f.reloadAccounts(t, config)
	return f
}

// A service replacement is the supported local account loading boundary; it
// deliberately does not mutate an in-use provider or reload secret files.
func (f *responsibilityFixture) reloadAccounts(t *testing.T, config access.AccountConfig) {
	t.Helper()
	provider, err := access.NewLocalProvider(config)
	if err != nil {
		t.Fatal(err)
	}
	service, err := access.NewService(provider, access.DiscardAuditor{}, access.ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	expected, err := db.LoadMigrationChecksums(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(api.NewProductionHandler(f.reader, f.store, expected, service, api.TransportSecurity{}, connectorcontrol.InvitationConfig{}, nil, nil))
	t.Cleanup(server.Close)
	f.server, f.service = server, service
}

func TestResponsibilityAccountLoadingAndHistoricalIdentity(t *testing.T) {
	f := startResponsibilityProduction(t)
	originalServer := f.server
	one := responsibilityLogin(t, f, "reviewer.one")
	_, command := responsibilityCommand(t, f, "xs_1111111111111111")
	status, first := one.call(t, "POST", f.reviewPath()+"-actions", command, nil)
	if status != 200 {
		t.Fatalf("initial assignment: %d", status)
	}
	config := f.config
	config.Accounts = append([]access.AccountConfigEntry(nil), config.Accounts...)
	config.Accounts[2].Disabled = true
	// Editing the config value does not mutate the already loaded provider.
	status, raw := one.call(t, "GET", f.reviewPath(), nil, nil)
	var review workspace.FollowupReview
	if status != 200 || json.Unmarshal(raw, &review) != nil || review.AssigneeState != "active" {
		t.Fatal("loaded account snapshot changed without load")
	}
	for _, state := range []string{"disabled", "not_found"} {
		if state == "not_found" {
			config.Accounts = append(config.Accounts[:2:2], config.Accounts[3:]...)
		}
		f.reloadAccounts(t, config)
		client := responsibilityLogin(t, f, "reviewer.one")
		status, raw = client.call(t, "GET", f.reviewPath(), nil, nil)
		if status != 200 || json.Unmarshal(raw, &review) != nil || review.AssigneeState != state || review.Followup.AssigneeName != "assignee.reader" || review.Followup.AssigneeSubjectID == nil || *review.Followup.AssigneeSubjectID != "xs_3333333333333333" {
			t.Fatalf("historic subject/name after %s: status=%d", state, status)
		}
		if status, replay := client.call(t, "POST", f.reviewPath()+"-actions", command, nil); status != 200 || !bytes.Equal(first, replay) {
			t.Fatal("account invalidation changed original receipt replay")
		}
		_, next := responsibilityCommand(t, f, "xs_1111111111111111")
		zone := "America/New_York"
		due, err := time.Parse(time.RFC3339Nano, "2026-11-01T01:30:12.123456-05:00")
		if err != nil {
			t.Fatal(err)
		}
		next.Payload.DueAt, next.Payload.DueTimezone, next.Payload.Reason = &due, &zone, "Keep historical owner; change deadline after "+state
		if status, _ := client.call(t, "POST", f.reviewPath()+"-actions", next, nil); status != 200 {
			t.Fatalf("unchanged historic owner cannot change deadline: %d", status)
		}
		path := "/api/v1/security/assignee-candidates?case_id=" + f.caseItem.ID + "&finding_id=" + f.finding.ID
		status, raw = client.call(t, "GET", path, nil, nil)
		var directory struct{ Items []access.LocalSubject }
		if status != 200 || json.Unmarshal(raw, &directory) != nil {
			t.Fatal("reload directory read")
		}
		found := false
		for _, item := range directory.Items {
			if item.SubjectID == "xs_3333333333333333" {
				found = true
				if item.State != state {
					t.Fatal("stale directory status")
				}
			}
		}
		if found != (state == "disabled") {
			t.Fatal("directory does not match loaded config")
		}
	}
	f.server = originalServer
	status, raw = one.call(t, "GET", f.reviewPath(), nil, nil)
	if status != 200 || json.Unmarshal(raw, &review) != nil || review.AssigneeState != "active" {
		t.Fatal("old loaded service secretly reloaded accounts")
	}
	t.Log("Actual service replacement reflects disabled/removed account; loaded old service unchanged; historical identity, deadline edits and original receipt replay preserved")
}

// The harness exposes only synthetic local data and uses the actual production
// handler, TLS, sessions, runtime roles and embedded frontend. A browser driver
// completes verification before writing the explicit stop marker.
func TestResponsibilityProductionBrowserHarness(t *testing.T) {
	if os.Getenv("XINSIGHT_RESPONSIBILITY_BROWSER") != "1" {
		t.Skip("explicit local browser harness required")
	}
	output := os.Getenv("XINSIGHT_RESPONSIBILITY_BROWSER_OUTPUT")
	if !filepath.IsAbs(output) || filepath.Clean(output) != output {
		t.Fatal("absolute clean browser output required")
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	f := startResponsibilityProduction(t)
	actor, _ := workspace.HumanActor("xs_1111111111111111")
	// A second Finding gives the browser a real history longer than one page,
	// while the original starts unrecorded for the initial registration flow.
	paged, err := f.store.CreateFinding(context.Background(), f.caseItem.ID, actor, workspace.CreateFindingInput{Title: "分页历史验证", Statement: "Synthetic history with explicit owner and deadline changes", Classification: "hypothesis"}, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 23; i++ {
		review, err := f.store.GetFindingReview(context.Background(), f.caseItem.ID, paged.ID)
		if err != nil {
			t.Fatal(err)
		}
		command := workspace.FollowupCommand{Operation: "update_followup", OperationID: randomTestUUID(t), ExpectedVersion: review.Followup.Version, ExpectedCaseVersion: review.CaseVersion, Payload: workspace.FollowupPayload{Reason: fmt.Sprintf("历史依据 %02d", i+1)}}
		if _, err := f.store.ApplyFindingFollowup(context.Background(), f.caseItem.ID, paged.ID, actor, command, workspace.AssigneeLookup{}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	// A historical assignment remains visible after the next account load disables it.
	active := f.config
	active.Accounts = append([]access.AccountConfigEntry(nil), active.Accounts...)
	active.Accounts[3].Disabled = false
	f.reloadAccounts(t, active)
	disabled, err := f.store.CreateFinding(context.Background(), f.caseItem.ID, actor, workspace.CreateFindingInput{Title: "原负责人失效验证", Statement: "The assignee was active when this responsibility was recorded", Classification: "hypothesis"}, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	current, err := f.store.GetFindingReview(context.Background(), f.caseItem.ID, disabled.ID)
	if err != nil {
		t.Fatal(err)
	}
	id := "xs_4444444444444444"
	command := workspace.FollowupCommand{Operation: "update_followup", OperationID: randomTestUUID(t), ExpectedVersion: 0, ExpectedCaseVersion: current.CaseVersion, Payload: workspace.FollowupPayload{AssigneeSubjectID: &id, Reason: "Historical assignment before account disable"}}
	if _, err := f.store.ApplyFindingFollowup(context.Background(), f.caseItem.ID, disabled.ID, actor, command, workspace.AssigneeLookup{SubjectID: id, Name: "disabled.owner", Active: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	f.reloadAccounts(t, f.config)
	other, err := f.store.CreateCase(context.Background(), actor, workspace.CreateCaseInput{Title: "导航竞态验证 Case"}, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	otherFinding, err := f.store.CreateFinding(context.Background(), other.ID, actor, workspace.CreateFindingInput{Title: "独立 Case 的事项", Statement: "This Finding must not display another Case's late responsibility", Classification: "hypothesis"}, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var databaseName string
	if err := f.admin.QueryRow("SELECT current_database()").Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	ready := map[string]any{"base_url": f.server.URL, "database_name": databaseName, "case_id": f.caseItem.ID, "finding_id": f.finding.ID, "paged_finding_id": paged.ID, "history_rows": 23, "disabled_finding_id": disabled.ID, "other_case_id": other.ID, "other_finding_id": otherFinding.ID, "usernames": []string{"reviewer.one", "reviewer.two", "assignee.reader"}, "evidence_boundary": "synthetic dedicated PG16 / production HTTPS human sessions / embedded UI"}
	raw, err := json.MarshalIndent(ready, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "ready.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-t.Context().Done():
			t.Fatal("browser harness cancelled")
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(output, "stop")); err == nil {
				return
			}
		}
	}
}

func TestResponsibilitySchemaPreparation(t *testing.T) {
	output := os.Getenv("XINSIGHT_RESPONSIBILITY_SCHEMA_OUTPUT")
	if output == "" {
		t.Skip("explicit schema output required")
	}
	ctx, dsn, admin := responsibilityDatabase(t)
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	manifest, err := hostctl.GenerateCleanBaselineSchemaContract(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.DatabaseSchema != "32" || manifest.MigrationCount != 32 {
		t.Fatal("wrong schema identity")
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(output, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("Generated local schema 28 contract: %s", manifest.SemanticSHA256)
}
