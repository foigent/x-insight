package reviewpolicycontrol

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/urmachine"
)

type urStoreStub struct {
	op                db.ReviewCaptureUROperation
	created, recorded int
	jointCreated      int
	paired            bool
	currentRevision   int64
}

func (s *urStoreStub) ValidateReviewCaptureURInstallation(context.Context, string, string) error {
	if !s.paired {
		return errors.New("unpaired installation")
	}
	return nil
}
func (s *urStoreStub) CurrentReviewCaptureDesiredRevision(context.Context) (int64, error) {
	return s.currentRevision, nil
}
func (s *urStoreStub) CurrentReviewCaptureDesiredRevisionAny(context.Context) (int64, error) {
	return s.currentRevision, nil
}
func (s *urStoreStub) CreateReviewCaptureJointUROperation(_ context.Context, op db.ReviewCaptureUROperation,
	settings db.ReviewCaptureSettings, expected int64, _ time.Time) error {
	if expected != s.currentRevision || op.XIDesiredRevision != expected+1 || settings.Mode == "" {
		return errors.New("joint XI CAS failed")
	}
	s.jointCreated++
	s.currentRevision++
	s.op = op
	return nil
}
func (s *urStoreStub) CreateReviewCaptureUROperation(_ context.Context, op db.ReviewCaptureUROperation) error {
	s.created++
	s.op = op
	return nil
}
func (s *urStoreStub) ReadReviewCaptureUROperation(_ context.Context, _, ref string) (db.ReviewCaptureUROperation, error) {
	if s.op.OperationRef != ref {
		return db.ReviewCaptureUROperation{}, sql.ErrNoRows
	}
	return s.op, nil
}
func (s *urStoreStub) ReadLatestReviewCaptureUROperation(_ context.Context, _, kind, scopeID string) (db.ReviewCaptureUROperation, error) {
	if s.op.ScopeKind != kind || s.op.ScopeID != scopeID {
		return db.ReviewCaptureUROperation{}, sql.ErrNoRows
	}
	return s.op, nil
}
func (s *urStoreStub) RecordReviewCaptureUROperation(_ context.Context, _, ref, state string, result json.RawMessage, now time.Time) error {
	if s.op.OperationRef != ref || now.IsZero() {
		return errors.New("wrong operation")
	}
	s.recorded++
	s.op.State = state
	s.op.URResult = result
	return nil
}

type urClientStub struct {
	view              urmachine.PolicyView
	writeErr, readErr error
	writes, reads     int
	lastEnvelope      json.RawMessage
	resultRef         string
}

func (c *urClientStub) PolicyScopes(context.Context, string) (urmachine.PolicyScopes, error) {
	return urmachine.PolicyScopes{}, nil
}
func (c *urClientStub) ReadProjectPolicy(context.Context, string, string) (urmachine.PolicyView, error) {
	return c.view, nil
}
func (c *urClientStub) WriteProjectPolicy(_ context.Context, _ string, _ string, envelope json.RawMessage) (urmachine.WriteResult, error) {
	c.writes++
	c.lastEnvelope = envelope
	return urmachine.WriteResult{}, c.writeErr
}
func (c *urClientStub) ReadPolicyOperation(_ context.Context, ref string) (urmachine.WriteResult, error) {
	c.reads++
	if c.readErr != nil {
		return urmachine.WriteResult{}, c.readErr
	}
	policyRef := c.resultRef
	if policyRef == "" {
		policyRef = "content-policy:7:9"
	}
	return urmachine.WriteResult{SchemaVersion: "unirouter.xinsight.policy-write-result.v1", OperationRef: ref, Result: "committed",
		SourceInstance: "ur-local", PolicyRef: policyRef, DesiredVersion: "2", EffectiveVersion: "2", ApplicationStatus: "applied"}, nil
}

type urScopedClientStub struct {
	*urClientStub
	enterpriseReads, userReads, enterpriseWrites, userWrites int
}

func (c *urScopedClientStub) PolicyUsers(context.Context, string, string) (urmachine.PolicyUsers, error) {
	return urmachine.PolicyUsers{SchemaVersion: "unirouter.xinsight.policy-users.v1", ProjectID: "42",
		Users: []urmachine.PolicyUser{{UserID: "5", DisplayName: "Owner"}}}, nil
}
func (c *urScopedClientStub) ReadEnterprisePolicy(context.Context, string) (urmachine.PolicyView, error) {
	c.enterpriseReads++
	return c.view, nil
}
func (c *urScopedClientStub) ReadUserPolicy(context.Context, string, string, string) (urmachine.PolicyView, error) {
	c.userReads++
	return c.view, nil
}
func (c *urScopedClientStub) WriteEnterprisePolicy(_ context.Context, _ string, envelope json.RawMessage) (urmachine.WriteResult, error) {
	c.enterpriseWrites++
	c.lastEnvelope = envelope
	return urmachine.WriteResult{}, c.writeErr
}
func (c *urScopedClientStub) WriteUserPolicy(_ context.Context, _, _, _ string, envelope json.RawMessage) (urmachine.WriteResult, error) {
	c.userWrites++
	c.lastEnvelope = envelope
	return urmachine.WriteResult{}, c.writeErr
}

func urManagerRequest() Request {
	return Request{SchemaVersion: RequestSchema, Action: ActionURApplyProject, ActorSubject: "xs_1111111111111111",
		RequestedAt: time.Now().UTC(), UR: &URRequest{ProjectID: "9", DelegationID: "xipg_1", DelegationRevision: "1",
			OperationRef: "xipw_11111111111111111111111111111111",
			Policy:       json.RawMessage(`{"schema_version":"unirouter.payload-policy.v2","expected_policy_ref":"content-policy:7:9","expected_version":"1","expected_vector":"{\"enterprise\":1,\"project\":1}","desired":{"mode":"full_content"},"execution_storage_mode":"temporary_allowed"}`)}}
}
func urManagerFixture() (Manager, *urStoreStub, *urClientStub) {
	store := &urStoreStub{paired: true, currentRevision: 2}
	client := &urClientStub{view: urmachine.PolicyView{SchemaVersion: "unirouter.payload-policy.v2", SourceInstance: "ur-local", OrgID: "7", ProjectID: "9",
		Project: json.RawMessage(`{"policy_ref":"content-policy:7:9","desired":{"policy_version":"1"}}`), VersionVector: `{"enterprise":1,"project":1}`}}
	manager := Manager{Store: &policyStoreStub{}, URStore: store, URClient: client,
		URIdentity: urmachine.Identity{InstallationID: "xci_abcdefghijklmnopqrstuv", SourceInstance: "ur-local", OrgID: "7"}}
	return manager, store, client
}
func TestURPolicyExactRetryNeedsFresh404AndSameCurrentCAS(t *testing.T) {
	manager, store, client := urManagerFixture()
	request := urManagerRequest()
	client.writeErr, client.readErr = urmachine.ErrUnconfirmed, urmachine.ErrNotFound
	if _, err := manager.Execute(t.Context(), request); err != nil || client.writes != 1 {
		t.Fatalf("initial attempt %v writes=%d", err, client.writes)
	}
	request.UR.RetryUnconfirmed = true
	client.readErr = urmachine.ErrUnavailable
	result, err := manager.Execute(t.Context(), request)
	if err != nil || result.UR.ReadbackStatus != "unavailable" || client.writes != 1 {
		t.Fatalf("unavailable read retried: %+v %v", result, err)
	}
	client.readErr = urmachine.ErrNotFound
	store.currentRevision = 3
	if _, err := manager.Execute(t.Context(), request); !errors.Is(err, urmachine.ErrConflict) || client.writes != 1 {
		t.Fatalf("changed XI policy retried: %v", err)
	}
	store.currentRevision = 2
	result, err = manager.Execute(t.Context(), request)
	if err != nil || result.UR.ReadbackStatus != "not_found" || client.writes != 2 || store.created != 1 {
		t.Fatalf("exact retry failed: %+v %v store=%+v client=%+v", result, err, store, client)
	}
	client.readErr = nil
	result, err = manager.Execute(t.Context(), request)
	if err != nil || result.State != "effective" || client.writes != 2 {
		t.Fatalf("found operation retried: %+v %v", result, err)
	}
	if _, err = manager.Execute(t.Context(), request); !errors.Is(err, urmachine.ErrConflict) || client.writes != 2 {
		t.Fatalf("terminal retry accepted: %v", err)
	}
}
func TestURPolicyOperationIsDurableBeforeWriteAndRecoveredWithoutRetry(t *testing.T) {
	manager, store, client := urManagerFixture()
	request := urManagerRequest()
	client.writeErr = urmachine.ErrUnconfirmed
	client.readErr = urmachine.ErrNotFound
	result, err := manager.Execute(t.Context(), request)
	if err != nil || result.State != "unconfirmed" || store.created != 1 || store.recorded != 1 || client.writes != 1 || client.reads != 1 {
		t.Fatalf("unconfirmed operation %+v %v store=%+v client=%+v", result, err, store, client)
	}
	var envelope struct {
		OperationRef string `json:"operation_ref"`
		XIOperator   struct {
			Kind         string `json:"kind"`
			SubjectRef   string `json:"subject_ref"`
			XIRequestRef string `json:"xi_request_ref"`
		} `json:"xi_operator"`
	}
	if json.Unmarshal(client.lastEnvelope, &envelope) != nil || envelope.OperationRef != request.UR.OperationRef || envelope.XIOperator.Kind != "human" ||
		envelope.XIOperator.SubjectRef != request.ActorSubject || envelope.XIOperator.XIRequestRef != request.UR.OperationRef {
		t.Fatalf("delegated envelope lacks exact human assertion: %+v", envelope)
	}
	client.readErr = nil
	replayed, replayErr := manager.Execute(t.Context(), request)
	if replayErr != nil || replayed.State != "effective" || client.writes != 1 || store.created != 1 {
		t.Fatalf("same-ref browser retry sent a second write: %+v %v", replayed, replayErr)
	}
	changed := request
	changed.UR = &URRequest{ProjectID: request.UR.ProjectID, DelegationID: request.UR.DelegationID,
		DelegationRevision: request.UR.DelegationRevision, OperationRef: request.UR.OperationRef,
		Policy: json.RawMessage(`{"schema_version":"unirouter.payload-policy.v2","expected_policy_ref":"content-policy:7:9","expected_version":"1","expected_vector":"{\"enterprise\":1,\"project\":1}","desired":{"mode":"metadata_only"},"execution_storage_mode":"temporary_allowed"}`)}
	if _, conflictErr := manager.Execute(t.Context(), changed); !errors.Is(conflictErr, urmachine.ErrConflict) || client.writes != 1 {
		t.Fatalf("changed body reused operation identity: %v", conflictErr)
	}
	read := Request{SchemaVersion: RequestSchema, Action: ActionURReadOperation, ActorSubject: request.ActorSubject,
		RequestedAt: time.Now().UTC(), UR: &URRequest{OperationRef: request.UR.OperationRef}}
	result, err = manager.Execute(t.Context(), read)
	if err != nil || result.State != "effective" || client.writes != 1 || client.reads != 2 || store.recorded != 2 {
		t.Fatalf("operation recovery retried write or failed: %+v %v store=%+v client=%+v", result, err, store, client)
	}
}
func TestURPolicyStaleCASOrUnpairedInstallationDoesNotWrite(t *testing.T) {
	manager, store, client := urManagerFixture()
	request := urManagerRequest()
	request.UR.Policy = json.RawMessage(`{"schema_version":"unirouter.payload-policy.v2","expected_policy_ref":"content-policy:7:9","expected_version":"0","expected_vector":"{\"enterprise\":1,\"project\":1}","desired":{"mode":"full_content"},"execution_storage_mode":"temporary_allowed"}`)
	if _, err := manager.Execute(t.Context(), request); !errors.Is(err, urmachine.ErrConflict) || store.created != 0 || client.writes != 0 {
		t.Fatalf("stale UR CAS admitted: %v %+v %+v", err, store, client)
	}
	store.paired = false
	request = urManagerRequest()
	if _, err := manager.Execute(t.Context(), request); err == nil || store.created != 0 || client.writes != 0 {
		t.Fatalf("unpaired installation admitted: %v %+v %+v", err, store, client)
	}
}

func TestURScopedWritesPersistExactEnterpriseAndUserIdentityBeforeDelegation(t *testing.T) {
	for _, test := range []struct {
		name, action, scopeID, ref, layer string
	}{
		{"enterprise", ActionURApplyEnterprise, "7", "content-policy:7:enterprise", "enterprise"},
		{"user", ActionURApplyUser, "42:5", "content-policy:7:42:5", "user"},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager, store, base := urManagerFixture()
			client := &urScopedClientStub{urClientStub: base}
			manager.URClient = client
			base.resultRef = test.ref
			base.view.ProjectID = ""
			base.view.Project = nil
			base.view.Enterprise = json.RawMessage(`{"policy_ref":"content-policy:7:enterprise","desired":{"policy_version":"1"}}`)
			base.view.User = nil
			if test.name == "user" {
				base.view.ProjectID, base.view.UserID = "42", "5"
				base.view.User = json.RawMessage(`{"policy_ref":"content-policy:7:42:5","desired":{"policy_version":"1"}}`)
			}
			base.view.VersionVector = `{"enterprise":1,"project":1,"user":1}`
			request := urManagerRequest()
			request.Action = test.action
			request.UR.ProjectID = ""
			request.UR.OrgID = ""
			request.UR.UserID = ""
			if test.name == "enterprise" {
				request.UR.OrgID = "7"
			} else {
				request.UR.ProjectID, request.UR.UserID = "42", "5"
			}
			request.UR.Policy = json.RawMessage(`{"schema_version":"unirouter.payload-policy.v2","expected_policy_ref":"` + test.ref + `","expected_version":"1","expected_vector":"{\"enterprise\":1,\"project\":1,\"user\":1}","desired":{"mode":"metadata_only"},"execution_storage_mode":"temporary_allowed"}`)
			result, err := manager.Execute(t.Context(), request)
			if err != nil || result.State != "effective" || store.op.ScopeKind != test.name || store.op.ScopeID != test.scopeID ||
				store.op.URExpectedRef != test.ref || store.created != 1 {
				t.Fatalf("scoped write %+v %v operation %+v", result, err, store.op)
			}
			if test.name == "enterprise" && (client.enterpriseReads != 1 || client.enterpriseWrites != 1) ||
				test.name == "user" && (client.userReads != 1 || client.userWrites != 1) {
				t.Fatalf("wrong delegated route %+v", client)
			}
			if test.name == "enterprise" {
				request.UR.OrgID = "8"
			} else {
				request.UR.UserID = "6"
			}
			if _, err = manager.Execute(t.Context(), request); err == nil || store.created != 1 {
				t.Fatalf("scope drift accepted: %v", err)
			}
		})
	}
}

func TestURJointWriteClaimsXIIntentAndOperationBeforeUnconfirmedRemoteCall(t *testing.T) {
	manager, store, client := urManagerFixture()
	scoped := &urScopedClientStub{urClientStub: client}
	manager.URClient = scoped
	client.view.ProjectID = ""
	client.view.Project = nil
	client.view.Enterprise = json.RawMessage(`{"policy_ref":"content-policy:7:enterprise","desired":{"policy_version":"1"}}`)
	request := urManagerRequest()
	request.Action = ActionURApplyEnterprise
	request.UR.ProjectID, request.UR.OrgID = "", "7"
	request.UR.Policy = json.RawMessage(`{"schema_version":"unirouter.payload-policy.v2","expected_policy_ref":"content-policy:7:enterprise","expected_version":"1","expected_vector":"{\"enterprise\":1,\"project\":1}","desired":{"mode":"metadata_only"},"execution_storage_mode":"temporary_allowed"}`)
	request.UR.XISettings = &db.ReviewCaptureSettings{Mode: "disabled", RetentionSeconds: 3600,
		ProjectQuotaBytes: 1 << 20, MaxItemBytes: 1024}
	request.UR.XIExpectedRevision = 2
	client.writeErr, client.readErr = urmachine.ErrUnconfirmed, urmachine.ErrNotFound
	result, err := manager.Execute(t.Context(), request)
	if err != nil || result.State != "unconfirmed" || store.jointCreated != 1 || store.created != 0 ||
		store.op.XIDesiredRevision != 3 || store.currentRevision != 3 || scoped.enterpriseWrites != 1 {
		t.Fatalf("joint initial %+v %v store=%+v client=%+v", result, err, store, client)
	}
	request.UR.RetryUnconfirmed = true
	result, err = manager.Execute(t.Context(), request)
	if err != nil || result.UR.ReadbackStatus != "not_found" || scoped.enterpriseWrites != 2 || store.jointCreated != 1 {
		t.Fatalf("exact joint retry %+v %v store=%+v client=%+v", result, err, store, client)
	}
	request.UR.XISettings.Mode = "full_content"
	if _, err = manager.Execute(t.Context(), request); !errors.Is(err, urmachine.ErrConflict) || scoped.enterpriseWrites != 2 {
		t.Fatalf("mutated XI intent reused operation: %v", err)
	}
}
