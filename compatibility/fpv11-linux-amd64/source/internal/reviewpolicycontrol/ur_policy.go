package reviewpolicycontrol

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/urmachine"
)

type URPolicyClient interface {
	PolicyScopes(context.Context, string) (urmachine.PolicyScopes, error)
	ReadProjectPolicy(context.Context, string, string) (urmachine.PolicyView, error)
	WriteProjectPolicy(context.Context, string, string, json.RawMessage) (urmachine.WriteResult, error)
	ReadPolicyOperation(context.Context, string) (urmachine.WriteResult, error)
}

type URScopedPolicyClient interface {
	PolicyUsers(context.Context, string, string) (urmachine.PolicyUsers, error)
	ReadEnterprisePolicy(context.Context, string) (urmachine.PolicyView, error)
	ReadUserPolicy(context.Context, string, string, string) (urmachine.PolicyView, error)
	WriteEnterprisePolicy(context.Context, string, json.RawMessage) (urmachine.WriteResult, error)
	WriteUserPolicy(context.Context, string, string, string, json.RawMessage) (urmachine.WriteResult, error)
}

type UROperationStore interface {
	ValidateReviewCaptureURInstallation(context.Context, string, string) error
	CurrentReviewCaptureDesiredRevision(context.Context) (int64, error)
	CreateReviewCaptureUROperation(context.Context, db.ReviewCaptureUROperation) error
	ReadReviewCaptureUROperation(context.Context, string, string) (db.ReviewCaptureUROperation, error)
	ReadLatestReviewCaptureUROperation(context.Context, string, string, string) (db.ReviewCaptureUROperation, error)
	RecordReviewCaptureUROperation(context.Context, string, string, string, json.RawMessage, time.Time) error
}

type URJointStore interface {
	CreateReviewCaptureJointUROperation(context.Context, db.ReviewCaptureUROperation, db.ReviewCaptureSettings, int64, time.Time) error
	CurrentReviewCaptureDesiredRevisionAny(context.Context) (int64, error)
}

func (m Manager) latestURScopeOperation(ctx context.Context, kind string, u *URRequest) (*db.ReviewCaptureUROperation, error) {
	op, err := m.URStore.ReadLatestReviewCaptureUROperation(ctx, m.URIdentity.InstallationID,
		kind, urScopeID(kind, m.URIdentity, u))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &op, nil
}

type projectLayer struct {
	PolicyRef string `json:"policy_ref"`
	Desired   *struct {
		PolicyVersion string `json:"policy_version"`
	} `json:"desired"`
	Effective *struct {
		PolicyVersion string `json:"policy_version"`
	} `json:"effective"`
	ApplicationStatus string `json:"application_status"`
}

func (m Manager) readURScope(ctx context.Context, kind string, u *URRequest) (urmachine.PolicyView, error) {
	var view urmachine.PolicyView
	var err error
	if kind == "project" {
		view, err = m.URClient.ReadProjectPolicy(ctx, u.ProjectID, u.DelegationID)
	} else {
		client, ok := m.URClient.(URScopedPolicyClient)
		if !ok {
			return urmachine.PolicyView{}, urmachine.ErrUnavailable
		}
		if kind == "enterprise" {
			view, err = client.ReadEnterprisePolicy(ctx, u.DelegationID)
		} else {
			view, err = client.ReadUserPolicy(ctx, u.ProjectID, u.UserID, u.DelegationID)
		}
	}
	if err != nil {
		return view, err
	}
	expectedProject, expectedUser := u.ProjectID, u.UserID
	if kind == "enterprise" {
		expectedProject, expectedUser = "", ""
	}
	if kind == "project" {
		expectedUser = ""
	}
	if view.SourceInstance != m.URIdentity.SourceInstance || view.OrgID != m.URIdentity.OrgID ||
		view.ProjectID != expectedProject || view.UserID != expectedUser {
		return urmachine.PolicyView{}, urmachine.ErrContract
	}
	return view, nil
}

func (m Manager) writeURScope(ctx context.Context, kind string, u *URRequest, envelope json.RawMessage) (urmachine.WriteResult, error) {
	if kind == "project" {
		return m.URClient.WriteProjectPolicy(ctx, u.ProjectID, u.OperationRef, envelope)
	}
	client, ok := m.URClient.(URScopedPolicyClient)
	if !ok {
		return urmachine.WriteResult{}, urmachine.ErrUnavailable
	}
	if kind == "enterprise" {
		return client.WriteEnterprisePolicy(ctx, u.OperationRef, envelope)
	}
	return client.WriteUserPolicy(ctx, u.ProjectID, u.UserID, u.OperationRef, envelope)
}

func urScopeID(kind string, identity urmachine.Identity, u *URRequest) string {
	if kind == "enterprise" {
		return identity.OrgID
	}
	if kind == "user" {
		return u.ProjectID + ":" + u.UserID
	}
	return u.ProjectID
}

type scopedPut struct {
	SchemaVersion     string          `json:"schema_version"`
	ExpectedPolicyRef string          `json:"expected_policy_ref"`
	ExpectedVersion   string          `json:"expected_version"`
	ExpectedVector    string          `json:"expected_vector"`
	Desired           json.RawMessage `json:"desired"`
}

func (m Manager) executeUR(ctx context.Context, request Request) (Response, error) {
	if m.URClient == nil || m.URStore == nil || m.URIdentity.InstallationID == "" ||
		m.URIdentity.SourceInstance == "" || !positiveID.MatchString(m.URIdentity.OrgID) {
		return Response{}, fmt.Errorf("UniRouter policy delegation is not configured")
	}
	if err := m.URStore.ValidateReviewCaptureURInstallation(ctx, m.URIdentity.InstallationID, m.URIdentity.SourceInstance); err != nil {
		return Response{}, err
	}
	response := Response{SchemaVersion: ResponseSchema, Action: request.Action,
		ActorSubject: request.ActorSubject, UR: &URResponse{}}
	u := request.UR
	if u.OrgID != "" && u.OrgID != m.URIdentity.OrgID {
		return Response{}, urmachine.ErrConflict
	}
	switch request.Action {
	case ActionURScopes:
		scopes, err := m.URClient.PolicyScopes(ctx, u.Cursor)
		if err != nil {
			return Response{}, err
		}
		response.State, response.UR.Scopes = "read", &scopes
		return response, nil
	case ActionURUsers:
		client, ok := m.URClient.(URScopedPolicyClient)
		if !ok {
			return Response{}, urmachine.ErrUnavailable
		}
		users, err := client.PolicyUsers(ctx, u.ProjectID, u.Cursor)
		if err != nil {
			return Response{}, err
		}
		response.State, response.UR.Users = "read", &users
		return response, nil
	case ActionURReadEnterprise:
		view, err := m.readURScope(ctx, "enterprise", u)
		if err != nil {
			return Response{}, err
		}
		last, err := m.latestURScopeOperation(ctx, "enterprise", u)
		if err != nil {
			return Response{}, err
		}
		response.UR.LastOperation = last
		response.State, response.UR.View = "read", &view
		return response, nil
	case ActionURReadProject:
		view, err := m.readURScope(ctx, "project", u)
		if err != nil {
			return Response{}, err
		}
		last, err := m.latestURScopeOperation(ctx, "project", u)
		if err != nil {
			return Response{}, err
		}
		response.UR.LastOperation = last
		response.State, response.UR.View = "read", &view
		return response, nil
	case ActionURReadUser:
		view, err := m.readURScope(ctx, "user", u)
		if err != nil {
			return Response{}, err
		}
		last, err := m.latestURScopeOperation(ctx, "user", u)
		if err != nil {
			return Response{}, err
		}
		response.UR.LastOperation = last
		response.State, response.UR.View = "read", &view
		return response, nil
	case ActionURReadOperation:
		op, err := m.URStore.ReadReviewCaptureUROperation(ctx, m.URIdentity.InstallationID, u.OperationRef)
		if err != nil {
			return Response{}, err
		}
		if op.State != "effective" && op.State != "failed" && op.State != "drift" {
			var readback string
			op, readback, err = m.reconcileUROperation(ctx, op)
			if err != nil {
				return Response{}, err
			}
			response.UR.ReadbackStatus = readback
		}
		response.State, response.UR.Operation = op.State, &op
		return response, nil
	case ActionURApplyProject:
		return m.applyURScope(ctx, request, response, "project")
	case ActionURApplyEnterprise:
		return m.applyURScope(ctx, request, response, "enterprise")
	case ActionURApplyUser:
		return m.applyURScope(ctx, request, response, "user")
	}
	return Response{}, fmt.Errorf("invalid UniRouter policy action")
}

func (m Manager) applyURScope(ctx context.Context, request Request, response Response, scopeKind string) (Response, error) {
	u := request.UR
	if u.OrgID != "" && u.OrgID != m.URIdentity.OrgID {
		return Response{}, urmachine.ErrConflict
	}
	scopeID := urScopeID(scopeKind, m.URIdentity, u)
	hashScopeKind := ""
	if scopeKind != "project" {
		hashScopeKind = scopeKind
	}
	identityBytes, err := json.Marshal(struct {
		InstallationID     string                    `json:"installation_id"`
		SourceInstance     string                    `json:"source_instance"`
		OrgID              string                    `json:"org_id"`
		Actor              string                    `json:"actor"`
		ProjectID          string                    `json:"project_id"`
		UserID             string                    `json:"user_id,omitempty"`
		ScopeKind          string                    `json:"scope_kind,omitempty"`
		XISettings         *db.ReviewCaptureSettings `json:"xi_settings,omitempty"`
		XIExpectedRevision int64                     `json:"xi_expected_revision,omitempty"`
		DelegationID       string                    `json:"delegation_id"`
		DelegationRevision string                    `json:"delegation_revision"`
		Policy             json.RawMessage           `json:"policy"`
	}{m.URIdentity.InstallationID, m.URIdentity.SourceInstance, m.URIdentity.OrgID, request.ActorSubject,
		u.ProjectID, u.UserID, hashScopeKind, u.XISettings, u.XIExpectedRevision,
		u.DelegationID, u.DelegationRevision, u.Policy})
	if err != nil {
		return Response{}, err
	}
	digest := sha256.Sum256(identityBytes)
	requestHash := "sha256:" + hex.EncodeToString(digest[:])
	if previous, readErr := m.URStore.ReadReviewCaptureUROperation(ctx, m.URIdentity.InstallationID, u.OperationRef); readErr == nil {
		if previous.RequestSHA256 != requestHash || previous.OperatorSubject != request.ActorSubject ||
			previous.ScopeKind != scopeKind || previous.ScopeID != scopeID || previous.SourceInstance != m.URIdentity.SourceInstance ||
			previous.OrgID != m.URIdentity.OrgID {
			return Response{}, urmachine.ErrConflict
		}
		if u.RetryUnconfirmed && previous.State != "requested" && previous.State != "unconfirmed" {
			return Response{}, urmachine.ErrConflict
		}
		if previous.State != "effective" && previous.State != "failed" && previous.State != "drift" {
			var readback string
			previous, readback, err = m.reconcileUROperation(ctx, previous)
			if err != nil {
				return Response{}, err
			}
			response.UR.ReadbackStatus = readback
			if u.RetryUnconfirmed {
				if readback != "not_found" {
					response.State, response.UR.Operation = previous.State, &previous
					return response, nil
				}
				view, readErr := m.readURScope(ctx, scopeKind, u)
				if readErr != nil {
					return Response{}, readErr
				}
				put, readErr := validateScopedPut(view, u.Policy, scopeKind)
				if readErr != nil || put.ExpectedPolicyRef != previous.URExpectedRef ||
					put.ExpectedVector != string(previous.URExpectedVector) ||
					strconv.FormatInt(previous.URExpectedVersion, 10) != put.ExpectedVersion {
					return Response{}, urmachine.ErrConflict
				}
				var xiRevision int64
				if u.XISettings != nil {
					joint, ok := m.URStore.(URJointStore)
					if !ok {
						return Response{}, urmachine.ErrUnavailable
					}
					xiRevision, readErr = joint.CurrentReviewCaptureDesiredRevisionAny(ctx)
				} else {
					xiRevision, readErr = m.URStore.CurrentReviewCaptureDesiredRevision(ctx)
				}
				if readErr != nil || xiRevision != previous.XIDesiredRevision {
					return Response{}, urmachine.ErrConflict
				}
				envelope, marshalErr := urWriteEnvelope(request, m.URIdentity)
				if marshalErr != nil {
					return Response{}, marshalErr
				}
				_, writeErr := m.writeURScope(ctx, scopeKind, u, envelope)
				if errors.Is(writeErr, urmachine.ErrUnauthorized) || errors.Is(writeErr, urmachine.ErrConflict) || errors.Is(writeErr, urmachine.ErrContract) {
					if err := m.URStore.RecordReviewCaptureUROperation(ctx, previous.InstallationID, previous.OperationRef, "failed", nil, time.Now().UTC()); err != nil {
						return Response{}, err
					}
					previous.State = "failed"
				} else {
					previous, response.UR.ReadbackStatus, err = m.reconcileUROperation(ctx, previous)
					if err != nil {
						return Response{}, err
					}
				}
			}
		}
		response.State, response.UR.Operation = previous.State, &previous
		return response, nil
	} else if !errors.Is(readErr, sql.ErrNoRows) {
		return Response{}, readErr
	}
	if u.RetryUnconfirmed {
		return Response{}, urmachine.ErrConflict
	}
	view, err := m.readURScope(ctx, scopeKind, u)
	if err != nil {
		return Response{}, err
	}
	put, err := validateScopedPut(view, u.Policy, scopeKind)
	if err != nil {
		return Response{}, err
	}
	version, _ := strconv.ParseInt(put.ExpectedVersion, 10, 64)
	var xiRevision int64
	if u.XISettings != nil {
		xiRevision = u.XIExpectedRevision + 1
	} else {
		xiRevision, err = m.URStore.CurrentReviewCaptureDesiredRevision(ctx)
		if err != nil || xiRevision < 1 {
			return Response{}, fmt.Errorf("XI review policy must be configured before delegated UR write")
		}
	}
	ref := u.OperationRef
	op := db.ReviewCaptureUROperation{InstallationID: m.URIdentity.InstallationID,
		OperationRef: ref, SourceInstance: m.URIdentity.SourceInstance, OrgID: m.URIdentity.OrgID,
		ScopeKind: scopeKind, ScopeID: scopeID, OperatorSubject: request.ActorSubject,
		RequestSHA256:     requestHash,
		XIDesiredRevision: xiRevision, URExpectedRef: put.ExpectedPolicyRef,
		URExpectedVersion: version, URExpectedVector: json.RawMessage(put.ExpectedVector), State: "requested"}
	if u.XISettings != nil {
		joint, ok := m.URStore.(URJointStore)
		if !ok {
			return Response{}, urmachine.ErrUnavailable
		}
		err = joint.CreateReviewCaptureJointUROperation(ctx, op, *u.XISettings, u.XIExpectedRevision, request.RequestedAt)
	} else {
		err = m.URStore.CreateReviewCaptureUROperation(ctx, op)
	}
	if err != nil {
		// A concurrent duplicate can only read the already claimed identity;
		// it must never issue a second PUT with possibly changed bytes.
		if existing, readErr := m.URStore.ReadReviewCaptureUROperation(ctx, op.InstallationID, ref); readErr == nil {
			if existing.RequestSHA256 != requestHash {
				return Response{}, urmachine.ErrConflict
			}
			response.State, response.UR.Operation = existing.State, &existing
			return response, nil
		}
		return Response{}, err
	}
	envelope, err := urWriteEnvelope(request, m.URIdentity)
	if err != nil {
		return Response{}, err
	}
	_, writeErr := m.writeURScope(ctx, scopeKind, u, envelope)
	if errors.Is(writeErr, urmachine.ErrUnauthorized) || errors.Is(writeErr, urmachine.ErrConflict) ||
		errors.Is(writeErr, urmachine.ErrContract) {
		if err = m.URStore.RecordReviewCaptureUROperation(ctx, op.InstallationID, ref, "failed", nil, time.Now().UTC()); err != nil {
			return Response{}, err
		}
		op.State = "failed"
	} else {
		op, response.UR.ReadbackStatus, err = m.reconcileUROperation(ctx, op)
		if err != nil {
			return Response{}, err
		}
	}
	response.State, response.UR.Operation = op.State, &op
	return response, nil
}

func validateScopedPut(view urmachine.PolicyView, policy json.RawMessage, scopeKind string) (scopedPut, error) {
	var layer projectLayer
	var put scopedPut
	layerRaw := view.Project
	if scopeKind == "enterprise" {
		layerRaw = view.Enterprise
	}
	if scopeKind == "user" {
		layerRaw = view.User
	}
	if json.Unmarshal(layerRaw, &layer) != nil || layer.PolicyRef == "" ||
		json.Unmarshal(policy, &put) != nil || put.SchemaVersion != "unirouter.payload-policy.v2" ||
		put.ExpectedPolicyRef != layer.PolicyRef || put.ExpectedVector != view.VersionVector ||
		len(put.Desired) == 0 || !json.Valid(put.Desired) {
		return put, urmachine.ErrConflict
	}
	currentVersion := "0"
	if layer.Desired != nil {
		currentVersion = layer.Desired.PolicyVersion
	}
	if put.ExpectedVersion != currentVersion {
		return put, urmachine.ErrConflict
	}
	version, err := strconv.ParseInt(currentVersion, 10, 64)
	if err != nil || version < 0 || !json.Valid([]byte(view.VersionVector)) {
		return put, urmachine.ErrContract
	}
	return put, nil
}

func urWriteEnvelope(request Request, identity urmachine.Identity) (json.RawMessage, error) {
	u := request.UR
	ref := u.OperationRef
	return json.Marshal(struct {
		SchemaVersion              string `json:"schema_version"`
		OperationRef               string `json:"operation_ref"`
		InstallationID             string `json:"installation_id"`
		SourceInstance             string `json:"source_instance"`
		DelegationID               string `json:"delegation_id"`
		ExpectedDelegationRevision string `json:"expected_delegation_revision"`
		XIOperator                 struct {
			Kind         string `json:"kind"`
			SubjectRef   string `json:"subject_ref"`
			XIRequestRef string `json:"xi_request_ref"`
		} `json:"xi_operator"`
		Policy json.RawMessage `json:"policy"`
	}{SchemaVersion: "unirouter.xinsight.policy-write.v1", OperationRef: ref,
		InstallationID: identity.InstallationID, SourceInstance: identity.SourceInstance,
		DelegationID: u.DelegationID, ExpectedDelegationRevision: u.DelegationRevision,
		XIOperator: struct {
			Kind         string `json:"kind"`
			SubjectRef   string `json:"subject_ref"`
			XIRequestRef string `json:"xi_request_ref"`
		}{Kind: "human", SubjectRef: request.ActorSubject, XIRequestRef: ref}, Policy: u.Policy})
}

func (m Manager) reconcileUROperation(ctx context.Context, op db.ReviewCaptureUROperation) (db.ReviewCaptureUROperation, string, error) {
	result, err := m.URClient.ReadPolicyOperation(ctx, op.OperationRef)
	state := "unconfirmed"
	readback := "found"
	var raw json.RawMessage
	if err == nil {
		raw, _ = json.Marshal(result)
		if result.SchemaVersion != "unirouter.xinsight.policy-write-result.v1" || result.OperationRef != op.OperationRef ||
			result.SourceInstance != op.SourceInstance || result.PolicyRef != op.URExpectedRef || result.Result != "committed" {
			state = "drift"
		} else if result.ApplicationStatus == "applied" && result.EffectiveVersion != "" &&
			result.EffectiveVersion == result.DesiredVersion {
			state = "effective"
		} else {
			state = "pending"
		}
	} else if errors.Is(err, urmachine.ErrNotFound) {
		readback = "not_found"
	} else if errors.Is(err, urmachine.ErrUnavailable) {
		readback = "unavailable"
	} else if errors.Is(err, urmachine.ErrUnauthorized) || errors.Is(err, urmachine.ErrConflict) ||
		!errors.Is(err, urmachine.ErrNotFound) && !errors.Is(err, urmachine.ErrUnavailable) {
		state = "drift"
		readback = "error"
	}
	if err = m.URStore.RecordReviewCaptureUROperation(ctx, op.InstallationID, op.OperationRef, state, raw, time.Now().UTC()); err != nil {
		return op, readback, err
	}
	op, err = m.URStore.ReadReviewCaptureUROperation(ctx, op.InstallationID, op.OperationRef)
	return op, readback, err
}
