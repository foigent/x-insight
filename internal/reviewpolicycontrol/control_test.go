package reviewpolicycontrol

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
)

type policyStoreStub struct {
	requested          int
	confirmed          int
	fail               bool
	exceptionRequested int
	exceptionConfirmed int
}

func (s *policyStoreStub) RequestReviewCaptureException(_ context.Context, p db.ReviewCaptureException,
	expected int64, _ string, _ time.Time) (int64, error) {
	if !reviewExceptionRef.MatchString(p.PolicyRef) {
		return 0, errors.New("missing exception ref")
	}
	s.exceptionRequested++
	return expected + 1, nil
}

func (s *policyStoreStub) ConfirmReviewCaptureException(_ context.Context, _ string, _ int64, _ string, _ time.Time) error {
	s.exceptionConfirmed++
	if s.fail {
		return errors.New("synthetic interrupted exception confirmation")
	}
	return nil
}

func (s *policyStoreStub) RequestReviewCaptureDefault(_ context.Context, _ db.ReviewCaptureSettings,
	expected int64, _ string, _ time.Time) (int64, error) {
	s.requested++
	return expected + 1, nil
}

func (s *policyStoreStub) ConfirmReviewCaptureDefault(_ context.Context, _ int64, _ string, _ time.Time) error {
	s.confirmed++
	if s.fail {
		return errors.New("synthetic interrupted confirmation")
	}
	return nil
}

func (s *policyStoreStub) BindReviewCaptureProject(_ context.Context, _, _, _, _, _, _, _ string, _ time.Time) error {
	return nil
}

func TestPolicyApplyPendingCanBeExplicitlyConfirmed(t *testing.T) {
	store := &policyStoreStub{fail: true}
	manager := Manager{Store: store}
	request := Request{SchemaVersion: RequestSchema, Action: ActionApplyDefault,
		ActorSubject: "xs_1111111111111111", ExpectedRevision: 2,
		Settings: db.ReviewCaptureSettings{Mode: "full_content", RetentionSeconds: 3600,
			ProjectQuotaBytes: 1024, MaxItemBytes: 512}, RequestedAt: time.Now().UTC()}
	result, err := manager.Execute(context.Background(), request)
	if err != nil || result.State != "pending" || result.DesiredRevision != 3 || result.EffectiveRevision != 0 ||
		store.requested != 1 || store.confirmed != 1 {
		t.Fatalf("interrupted activation: %+v %v (%+v)", result, err, store)
	}
	store.fail = false
	confirm := Request{SchemaVersion: RequestSchema, Action: ActionConfirmDefault,
		ActorSubject: request.ActorSubject, ExpectedRevision: result.DesiredRevision, RequestedAt: request.RequestedAt}
	result, err = manager.Execute(context.Background(), confirm)
	if err != nil || result.State != "effective" || result.DesiredRevision != 3 || result.EffectiveRevision != 3 ||
		store.requested != 1 || store.confirmed != 2 {
		t.Fatalf("explicit confirmation: %+v %v (%+v)", result, err, store)
	}
	confirm.Settings = request.Settings
	if ValidRequest(confirm) {
		t.Fatal("confirm accepted a changed desired policy")
	}
}

func TestProjectBindingGeneratesStableResponseIdentity(t *testing.T) {
	manager := Manager{Store: &policyStoreStub{}}
	request := Request{SchemaVersion: RequestSchema, Action: ActionBindProject,
		ActorSubject: "xs_1111111111111111", RequestedAt: time.Now().UTC(),
		Project: &ProjectBinding{SourceSystemID: "11111111-1111-4111-8111-111111111111",
			SourceInstance: "ur-a", OrgID: "7", ProjectID: "9", DisplayName: "XI project"}}
	result, err := manager.Execute(context.Background(), request)
	if err != nil || result.State != "bound" || result.Project == nil || !uuidPattern.MatchString(result.Project.XIProjectID) ||
		request.Project.XIProjectID != "" || !validResponseState(request, result) {
		t.Fatalf("project binding response=%+v error=%v", result, err)
	}
	request.Project.ProjectID = "0"
	if ValidRequest(request) {
		t.Fatal("invalid source project ID admitted")
	}
}

func TestExceptionApplyGeneratesRefAndRequiresExactConfirmation(t *testing.T) {
	store := &policyStoreStub{fail: true}
	manager := Manager{Store: store}
	request := Request{SchemaVersion: RequestSchema, Action: ActionApplyException,
		ActorSubject: "xs_1111111111111111", RequestedAt: time.Now().UTC(),
		Exception: &db.ReviewCaptureException{SelectorKind: "tenant_category", ContentKind: "model_input",
			Mode: "disabled", RetentionSeconds: 3600, MaxItemBytes: 0, Provenance: "xi_admin"}}
	result, err := manager.Execute(context.Background(), request)
	if err != nil || result.State != "pending" || !reviewExceptionRef.MatchString(result.ExceptionRef) ||
		result.DesiredRevision != 1 || store.exceptionRequested != 1 || store.exceptionConfirmed != 1 ||
		!validResponseState(request, result) {
		t.Fatalf("exception pending: %+v %v", result, err)
	}
	store.fail = false
	confirm := Request{SchemaVersion: RequestSchema, Action: ActionConfirmException,
		ActorSubject: request.ActorSubject, RequestedAt: request.RequestedAt,
		ExceptionRef: result.ExceptionRef, ExpectedRevision: result.DesiredRevision}
	confirmed, err := manager.Execute(context.Background(), confirm)
	if err != nil || confirmed.State != "effective" || !validResponseState(confirm, confirmed) ||
		store.exceptionRequested != 1 || store.exceptionConfirmed != 2 {
		t.Fatalf("exception confirmation: %+v %v", confirmed, err)
	}
	confirm.ExceptionRef = "xipe_invalid"
	if ValidRequest(confirm) {
		t.Fatal("invalid exception ref accepted")
	}
}

type custodyPolicyStore struct {
	policyStoreStub
	needs bool
}

func (s *custodyPolicyStore) ReviewCaptureDesiredNeedsCustody(context.Context, string, int64) (bool, error) {
	return s.needs, nil
}
func TestFullContentEffectiveRequiresActualLoadedCustodyButDisabledDoesNot(t *testing.T) {
	s := &custodyPolicyStore{needs: true}
	calls := 0
	m := Manager{Store: s, EnsureCustody: func(context.Context) error { calls++; return errors.New("actual reader pending") }}
	req := Request{SchemaVersion: RequestSchema, Action: ActionApplyDefault, ActorSubject: "xs_1111111111111111", ExpectedRevision: 0, Settings: db.ReviewCaptureSettings{Mode: "full_content", RetentionSeconds: 3600, ProjectQuotaBytes: 1024, MaxItemBytes: 512}, RequestedAt: time.Now().UTC()}
	out, err := m.Execute(context.Background(), req)
	if err != nil || out.State != "pending" || s.requested != 1 || s.confirmed != 0 || calls != 1 {
		t.Fatal("full content false effective", out, err)
	}
	confirm := Request{SchemaVersion: RequestSchema, Action: ActionConfirmDefault, ActorSubject: req.ActorSubject, ExpectedRevision: 1, RequestedAt: req.RequestedAt}
	if _, err = m.Execute(context.Background(), confirm); err == nil || s.confirmed != 0 {
		t.Fatal("explicit confirm bypassed actual load", err)
	}
	s.needs = false
	req.Settings.Mode = "disabled"
	req.ExpectedRevision = 1
	before := calls
	out, err = m.Execute(context.Background(), req)
	if err != nil || out.State != "effective" || calls != before || s.confirmed != 1 {
		t.Fatal("disabled was unnecessarily gated", out, err)
	}
}
