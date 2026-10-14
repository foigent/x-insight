package reviewpolicycontrol

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"time"

	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/urmachine"
)

const RequestSchema = "xinsight.local-review-policy-request.v1"
const ResponseSchema = "xinsight.local-review-policy-response.v1"
const ActionApplyDefault = "apply_default"
const ActionConfirmDefault = "confirm_default"
const ActionBindProject = "bind_project"
const ActionApplyException = "apply_exception"
const ActionConfirmException = "confirm_exception"
const ActionURScopes = "ur_scopes"
const ActionURUsers = "ur_users"
const ActionURReadEnterprise = "ur_read_enterprise"
const ActionURApplyEnterprise = "ur_apply_enterprise"
const ActionURReadProject = "ur_read_project"
const ActionURApplyProject = "ur_apply_project"
const ActionURReadUser = "ur_read_user"
const ActionURApplyUser = "ur_apply_user"
const ActionURReadOperation = "ur_read_operation"

var subjectPattern = regexp.MustCompile(`^xs_[a-z0-9]{16,64}$`)
var reviewExceptionRef = regexp.MustCompile(`^xipe_[a-f0-9]{32}$`)

type Request struct {
	SchemaVersion    string                     `json:"schema_version"`
	Action           string                     `json:"action"`
	ActorSubject     string                     `json:"actor_subject"`
	ExpectedRevision int64                      `json:"expected_revision"`
	Settings         db.ReviewCaptureSettings   `json:"settings"`
	RequestedAt      time.Time                  `json:"requested_at"`
	Project          *ProjectBinding            `json:"project,omitempty"`
	Exception        *db.ReviewCaptureException `json:"exception,omitempty"`
	ExceptionRef     string                     `json:"exception_ref,omitempty"`
	UR               *URRequest                 `json:"ur,omitempty"`
}

type URRequest struct {
	Cursor             string                    `json:"cursor,omitempty"`
	OrgID              string                    `json:"org_id,omitempty"`
	ProjectID          string                    `json:"project_id,omitempty"`
	UserID             string                    `json:"user_id,omitempty"`
	DelegationID       string                    `json:"delegation_id,omitempty"`
	DelegationRevision string                    `json:"delegation_revision,omitempty"`
	OperationRef       string                    `json:"operation_ref,omitempty"`
	Policy             json.RawMessage           `json:"policy,omitempty"`
	RetryUnconfirmed   bool                      `json:"retry_unconfirmed,omitempty"`
	XISettings         *db.ReviewCaptureSettings `json:"xi_settings,omitempty"`
	XIExpectedRevision int64                     `json:"xi_expected_revision,omitempty"`
}

type URResponse struct {
	Scopes         *urmachine.PolicyScopes      `json:"scopes,omitempty"`
	Users          *urmachine.PolicyUsers       `json:"users,omitempty"`
	View           *urmachine.PolicyView        `json:"view,omitempty"`
	Operation      *db.ReviewCaptureUROperation `json:"operation,omitempty"`
	LastOperation  *db.ReviewCaptureUROperation `json:"last_operation,omitempty"`
	ReadbackStatus string                       `json:"readback_status,omitempty"`
}

type ProjectBinding struct {
	SourceSystemID string `json:"source_system_id"`
	SourceInstance string `json:"source_instance"`
	OrgID          string `json:"org_id"`
	ProjectID      string `json:"project_id"`
	XIProjectID    string `json:"xi_project_id,omitempty"`
	DisplayName    string `json:"display_name"`
}

type Response struct {
	SchemaVersion     string          `json:"schema_version"`
	Action            string          `json:"action"`
	ActorSubject      string          `json:"actor_subject"`
	DesiredRevision   int64           `json:"desired_revision"`
	EffectiveRevision int64           `json:"effective_revision"`
	State             string          `json:"state"`
	Project           *ProjectBinding `json:"project,omitempty"`
	ExceptionRef      string          `json:"exception_ref,omitempty"`
	UR                *URResponse     `json:"ur,omitempty"`
}

func ValidRequest(request Request) bool {
	settings := request.Settings
	if request.SchemaVersion != RequestSchema || !subjectPattern.MatchString(request.ActorSubject) ||
		request.RequestedAt.IsZero() || request.RequestedAt.Location() != time.UTC {
		return false
	}
	if request.Action == ActionApplyException {
		p := request.Exception
		return request.ExpectedRevision >= 0 && request.Project == nil && request.UR == nil &&
			request.ExceptionRef == "" && settings == (db.ReviewCaptureSettings{}) && p != nil &&
			(p.PolicyRef == "" && request.ExpectedRevision == 0 || reviewExceptionRef.MatchString(p.PolicyRef)) &&
			(p.Mode == "disabled" || p.Mode == "full_content") && p.RetentionSeconds >= 1 &&
			p.RetentionSeconds <= 31536000 && p.MaxItemBytes >= 0 && p.MaxItemBytes <= 32<<20 &&
			len(p.Provenance) > 0 && len(p.Provenance) <= 256
	}
	if request.Action == ActionConfirmException {
		return request.ExpectedRevision > 0 && request.Project == nil && request.UR == nil &&
			request.Exception == nil && reviewExceptionRef.MatchString(request.ExceptionRef) &&
			settings == (db.ReviewCaptureSettings{})
	}
	if request.Action == ActionConfirmDefault {
		return request.ExpectedRevision > 0 && settings == (db.ReviewCaptureSettings{}) && request.Project == nil && request.UR == nil &&
			request.Exception == nil && request.ExceptionRef == ""
	}
	if request.Action == ActionBindProject {
		p := request.Project
		return request.ExpectedRevision == 0 && settings == (db.ReviewCaptureSettings{}) && p != nil && request.UR == nil &&
			request.Exception == nil && request.ExceptionRef == "" &&
			uuidPattern.MatchString(p.SourceSystemID) && p.SourceInstance != "" && len(p.SourceInstance) <= 128 &&
			positiveID.MatchString(p.OrgID) && positiveID.MatchString(p.ProjectID) &&
			(p.XIProjectID == "" || uuidPattern.MatchString(p.XIProjectID)) &&
			len(p.DisplayName) >= 1 && len(p.DisplayName) <= 128
	}
	if request.UR != nil && request.Project == nil && request.Exception == nil && request.ExceptionRef == "" && request.ExpectedRevision == 0 &&
		settings == (db.ReviewCaptureSettings{}) {
		u := request.UR
		jointValid := u.XISettings == nil && u.XIExpectedRevision == 0 ||
			u.XISettings != nil && u.XIExpectedRevision >= 0 && validJointSettings(*u.XISettings)
		if request.Action != ActionURApplyEnterprise && request.Action != ActionURApplyProject &&
			request.Action != ActionURApplyUser && (u.XISettings != nil || u.XIExpectedRevision != 0) {
			return false
		}
		switch request.Action {
		case ActionURScopes:
			return (u.Cursor == "" || positiveID.MatchString(u.Cursor)) && u.ProjectID == "" &&
				u.OrgID == "" && u.UserID == "" && u.DelegationID == "" && u.DelegationRevision == "" && u.OperationRef == "" && len(u.Policy) == 0 && !u.RetryUnconfirmed
		case ActionURUsers:
			return positiveID.MatchString(u.ProjectID) && (u.Cursor == "" || positiveID.MatchString(u.Cursor)) &&
				u.OrgID == "" && u.UserID == "" && u.DelegationID == "" && u.DelegationRevision == "" && u.OperationRef == "" && len(u.Policy) == 0 && !u.RetryUnconfirmed
		case ActionURReadEnterprise:
			return positiveID.MatchString(u.OrgID) && urDelegationRef.MatchString(u.DelegationID) && u.Cursor == "" && u.ProjectID == "" && u.UserID == "" &&
				u.DelegationRevision == "" && u.OperationRef == "" && len(u.Policy) == 0 && !u.RetryUnconfirmed
		case ActionURReadProject:
			return positiveID.MatchString(u.ProjectID) && urDelegationRef.MatchString(u.DelegationID) &&
				u.Cursor == "" && u.OrgID == "" && u.UserID == "" && u.DelegationRevision == "" && u.OperationRef == "" && len(u.Policy) == 0 && !u.RetryUnconfirmed
		case ActionURReadUser:
			return positiveID.MatchString(u.ProjectID) && positiveID.MatchString(u.UserID) && urDelegationRef.MatchString(u.DelegationID) &&
				u.Cursor == "" && u.OrgID == "" && u.DelegationRevision == "" && u.OperationRef == "" && len(u.Policy) == 0 && !u.RetryUnconfirmed
		case ActionURApplyEnterprise, ActionURApplyProject, ActionURApplyUser:
			if u.XISettings != nil && request.Action != ActionURApplyEnterprise {
				return false
			}
			scopeValid := request.Action == ActionURApplyEnterprise && positiveID.MatchString(u.OrgID) && u.ProjectID == "" && u.UserID == "" ||
				request.Action == ActionURApplyProject && u.OrgID == "" && positiveID.MatchString(u.ProjectID) && u.UserID == "" ||
				request.Action == ActionURApplyUser && u.OrgID == "" && positiveID.MatchString(u.ProjectID) && positiveID.MatchString(u.UserID)
			return jointValid && scopeValid && urDelegationRef.MatchString(u.DelegationID) &&
				positiveID.MatchString(u.DelegationRevision) && u.Cursor == "" && urOperationRef.MatchString(u.OperationRef) &&
				len(u.Policy) > 0 && len(u.Policy) <= 8192 && json.Valid(u.Policy)
		case ActionURReadOperation:
			return urOperationRef.MatchString(u.OperationRef) && u.Cursor == "" && u.OrgID == "" && u.ProjectID == "" && u.UserID == "" &&
				u.DelegationID == "" && u.DelegationRevision == "" && len(u.Policy) == 0 && !u.RetryUnconfirmed
		}
	}
	return request.Action == ActionApplyDefault && request.ExpectedRevision >= 0 &&
		request.Project == nil && request.Exception == nil && request.ExceptionRef == "" && request.UR == nil &&
		(settings.Mode == "disabled" || settings.Mode == "full_content") && settings.RetentionSeconds >= 1 &&
		settings.RetentionSeconds <= 31536000 && settings.ProjectQuotaBytes >= 1 &&
		settings.ProjectQuotaBytes <= 1<<40 && settings.MaxItemBytes >= 0 && settings.MaxItemBytes <= 32<<20
}

func validJointSettings(settings db.ReviewCaptureSettings) bool {
	return (settings.Mode == "disabled" || settings.Mode == "full_content") &&
		settings.RetentionSeconds >= 1 && settings.RetentionSeconds <= 31536000 &&
		settings.ProjectQuotaBytes >= 1 && settings.ProjectQuotaBytes <= 1<<40 &&
		settings.MaxItemBytes >= 0 && settings.MaxItemBytes <= 32<<20
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var positiveID = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
var urDelegationRef = regexp.MustCompile(`^xipg_[1-9][0-9]{0,18}$`)
var urOperationRef = regexp.MustCompile(`^xipw_[0-9a-f]{32}$`)

type OwnerStore interface {
	RequestReviewCaptureDefault(context.Context, db.ReviewCaptureSettings, int64, string, time.Time) (int64, error)
	ConfirmReviewCaptureDefault(context.Context, int64, string, time.Time) error
	BindReviewCaptureProject(context.Context, string, string, string, string, string, string, string, time.Time) error
}

type Manager struct {
	EnsureCustody   func(context.Context) error
	Store           OwnerStore
	URStore         UROperationStore
	URClient        URPolicyClient
	URIdentity      urmachine.Identity
	URClientFactory func(context.Context) (URPolicyClient, urmachine.Identity, error)
}

func (m Manager) Execute(ctx context.Context, request Request) (Response, error) {
	if m.Store == nil || !ValidRequest(request) {
		return Response{}, fmt.Errorf("invalid review policy request")
	}
	if request.UR != nil {
		return m.executeUR(ctx, request)
	}
	if request.Action == ActionApplyException || request.Action == ActionConfirmException {
		return m.executeException(ctx, request)
	}
	if request.Action == ActionBindProject {
		binding := *request.Project
		if binding.XIProjectID == "" {
			var raw [16]byte
			if _, err := rand.Read(raw[:]); err != nil {
				return Response{}, err
			}
			raw[6] = raw[6]&0x0f | 0x40
			raw[8] = raw[8]&0x3f | 0x80
			binding.XIProjectID = fmt.Sprintf("%x-%x-%x-%x-%x", raw[:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
		}
		if err := m.Store.BindReviewCaptureProject(ctx, binding.SourceSystemID, binding.SourceInstance,
			binding.OrgID, binding.ProjectID, binding.XIProjectID, binding.DisplayName,
			request.ActorSubject, request.RequestedAt); err != nil {
			return Response{}, err
		}
		return Response{SchemaVersion: ResponseSchema, Action: request.Action, ActorSubject: request.ActorSubject,
			State: "bound", Project: &binding}, nil
	}
	if request.Action == ActionConfirmDefault {
		if err := m.checkCustody(ctx, "", request.ExpectedRevision); err != nil {
			return Response{}, err
		}
		if err := m.Store.ConfirmReviewCaptureDefault(ctx, request.ExpectedRevision,
			request.ActorSubject, request.RequestedAt); err != nil {
			return Response{}, err
		}
		return Response{SchemaVersion: ResponseSchema, Action: request.Action, ActorSubject: request.ActorSubject,
			DesiredRevision: request.ExpectedRevision, EffectiveRevision: request.ExpectedRevision, State: "effective"}, nil
	}
	revision, err := m.Store.RequestReviewCaptureDefault(ctx, request.Settings, request.ExpectedRevision,
		request.ActorSubject, request.RequestedAt)
	if err != nil {
		return Response{}, err
	}
	result := Response{SchemaVersion: ResponseSchema, Action: request.Action,
		ActorSubject: request.ActorSubject, DesiredRevision: revision, State: "pending"}
	if err = m.checkCustody(ctx, "", revision); err != nil {
		return result, nil
	}
	if err = m.Store.ConfirmReviewCaptureDefault(ctx, revision, request.ActorSubject, request.RequestedAt); err != nil {
		// Desired was durably saved. The browser can read back this revision;
		// a lost confirmation cannot be represented as effective.
		return result, nil
	}
	result.EffectiveRevision, result.State = revision, "effective"
	return result, nil
}

func (m Manager) executeException(ctx context.Context, request Request) (Response, error) {
	store, ok := m.Store.(interface {
		RequestReviewCaptureException(context.Context, db.ReviewCaptureException, int64, string, time.Time) (int64, error)
		ConfirmReviewCaptureException(context.Context, string, int64, string, time.Time) error
	})
	if !ok {
		return Response{}, fmt.Errorf("review exception controller unavailable")
	}
	ref := request.ExceptionRef
	if request.Action == ActionConfirmException {
		if err := m.checkCustody(ctx, ref, request.ExpectedRevision); err != nil {
			return Response{}, err
		}
		if err := store.ConfirmReviewCaptureException(ctx, ref, request.ExpectedRevision,
			request.ActorSubject, request.RequestedAt); err != nil {
			return Response{}, err
		}
		return Response{SchemaVersion: ResponseSchema, Action: request.Action, ActorSubject: request.ActorSubject,
			ExceptionRef: ref, DesiredRevision: request.ExpectedRevision,
			EffectiveRevision: request.ExpectedRevision, State: "effective"}, nil
	}
	exception := *request.Exception
	if exception.PolicyRef == "" {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return Response{}, err
		}
		exception.PolicyRef = fmt.Sprintf("xipe_%x", raw[:])
	}
	ref = exception.PolicyRef
	revision, err := store.RequestReviewCaptureException(ctx, exception, request.ExpectedRevision,
		request.ActorSubject, request.RequestedAt)
	if err != nil {
		return Response{}, err
	}
	result := Response{SchemaVersion: ResponseSchema, Action: request.Action, ActorSubject: request.ActorSubject,
		ExceptionRef: ref, DesiredRevision: revision, State: "pending"}
	if err = m.checkCustody(ctx, ref, revision); err != nil {
		return result, nil
	}
	if err = store.ConfirmReviewCaptureException(ctx, ref, revision,
		request.ActorSubject, request.RequestedAt); err == nil {
		result.EffectiveRevision, result.State = revision, "effective"
	}
	return result, nil
}

func (m Manager) checkCustody(ctx context.Context, ref string, revision int64) error {
	if m.EnsureCustody == nil {
		return nil
	} // Optional only for in-process test/legacy managers.
	store, ok := m.Store.(interface {
		ReviewCaptureDesiredNeedsCustody(context.Context, string, int64) (bool, error)
	})
	if !ok {
		return fmt.Errorf("custody policy state unavailable")
	}
	required, err := store.ReviewCaptureDesiredNeedsCustody(ctx, ref, revision)
	if err != nil {
		return err
	}
	if required {
		return m.EnsureCustody(ctx)
	}
	return nil
}

type UnixClient struct {
	SocketPath string
	Timeout    time.Duration
	Dial       func(context.Context, string) (net.Conn, error)
}

func NewUnixClient() *UnixClient {
	dialer := net.Dialer{Timeout: 2 * time.Second}
	return &UnixClient{SocketPath: connectorcontrol.LocalControllerSocket, Timeout: 2 * time.Second,
		Dial: func(ctx context.Context, path string) (net.Conn, error) { return dialer.DialContext(ctx, "unix", path) }}
}

func (c *UnixClient) ApplyDefault(ctx context.Context, request Request) (Response, error) {
	return c.send(ctx, request)
}

func (c *UnixClient) ConfirmDefault(ctx context.Context, request Request) (Response, error) {
	return c.send(ctx, request)
}

func (c *UnixClient) BindProject(ctx context.Context, request Request) (Response, error) {
	return c.send(ctx, request)
}

func (c *UnixClient) ApplyException(ctx context.Context, request Request) (Response, error) {
	return c.send(ctx, request)
}

func (c *UnixClient) ConfirmException(ctx context.Context, request Request) (Response, error) {
	return c.send(ctx, request)
}

func (c *UnixClient) URPolicy(ctx context.Context, request Request) (Response, error) {
	return c.send(ctx, request)
}

func (c *UnixClient) send(ctx context.Context, request Request) (Response, error) {
	if c == nil || c.SocketPath != connectorcontrol.LocalControllerSocket || c.Timeout <= 0 || c.Dial == nil || !ValidRequest(request) {
		return Response{}, fmt.Errorf("invalid review policy controller request")
	}
	payload, err := json.Marshal(request)
	if err != nil || len(payload) > 16<<10-1 {
		return Response{}, fmt.Errorf("invalid review policy controller request")
	}
	conn, err := c.Dial(ctx, c.SocketPath)
	if err != nil {
		return Response{}, fmt.Errorf("review policy controller unavailable")
	}
	defer conn.Close()
	deadline := time.Now().Add(c.Timeout)
	if request.UR != nil {
		// UR machine readback can use the controller's bounded execution window.
		deadline = time.Now().Add(32 * time.Second)
	}
	if fromCtx, ok := ctx.Deadline(); ok && fromCtx.Before(deadline) {
		deadline = fromCtx
	}
	if err = conn.SetDeadline(deadline); err != nil {
		return Response{}, err
	}
	if _, err = conn.Write(append(payload, '\n')); err != nil {
		return Response{}, err
	}
	responseLimit := 16 << 10
	if request.UR != nil {
		responseLimit = 64 << 10
	}
	raw, err := bufio.NewReader(io.LimitReader(conn, int64(responseLimit+1))).ReadBytes('\n')
	if err != nil || len(raw) < 2 || len(raw) > responseLimit {
		return Response{}, fmt.Errorf("review policy controller response unavailable")
	}
	var result Response
	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimSuffix(raw, []byte{'\n'})))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) ||
		result.SchemaVersion != ResponseSchema || result.Action != request.Action || result.ActorSubject != request.ActorSubject ||
		result.DesiredRevision != expectedResultRevision(request) || !validResponseState(request, result) {
		return Response{}, fmt.Errorf("review policy controller response invalid")
	}
	return result, nil
}

func validResponseState(request Request, result Response) bool {
	if request.Action == ActionApplyException || request.Action == ActionConfirmException {
		return reviewExceptionRef.MatchString(result.ExceptionRef) && result.Project == nil && result.UR == nil &&
			(request.Action != ActionConfirmException || result.ExceptionRef == request.ExceptionRef) &&
			(request.Action != ActionConfirmException && result.State == "pending" || result.State == "effective") &&
			(result.State == "effective") == (result.EffectiveRevision == result.DesiredRevision)
	}
	if request.Action == ActionBindProject {
		return result.State == "bound" && result.EffectiveRevision == 0 && result.Project != nil &&
			result.Project.SourceSystemID == request.Project.SourceSystemID &&
			result.Project.SourceInstance == request.Project.SourceInstance && result.Project.OrgID == request.Project.OrgID &&
			result.Project.ProjectID == request.Project.ProjectID && uuidPattern.MatchString(result.Project.XIProjectID) &&
			result.Project.DisplayName == request.Project.DisplayName &&
			(request.Project.XIProjectID == "" || result.Project.XIProjectID == request.Project.XIProjectID)
	}
	if request.UR != nil {
		if result.UR == nil || result.Project != nil || result.DesiredRevision != 0 || result.EffectiveRevision != 0 {
			return false
		}
		switch request.Action {
		case ActionURScopes:
			return result.State == "read" && result.UR.Scopes != nil && result.UR.Users == nil && result.UR.View == nil && result.UR.Operation == nil
		case ActionURUsers:
			return result.State == "read" && result.UR.Users != nil && result.UR.Scopes == nil && result.UR.View == nil && result.UR.Operation == nil
		case ActionURReadEnterprise, ActionURReadProject, ActionURReadUser:
			return result.State == "read" && result.UR.View != nil && result.UR.Scopes == nil && result.UR.Users == nil && result.UR.Operation == nil
		case ActionURApplyEnterprise, ActionURApplyProject, ActionURApplyUser, ActionURReadOperation:
			return result.UR.Operation != nil && result.UR.Operation.OperationRef == request.UR.OperationRef &&
				result.UR.View == nil && result.UR.Scopes == nil && result.UR.Users == nil && result.State == result.UR.Operation.State
		}
		return false
	}
	return (result.State == "pending" || result.State == "effective") &&
		(result.State == "effective") == (result.EffectiveRevision == result.DesiredRevision) && result.Project == nil
}

func expectedResultRevision(request Request) int64 {
	if request.Action == ActionConfirmException {
		return request.ExpectedRevision
	}
	if request.Action == ActionBindProject || request.UR != nil {
		return 0
	}
	if request.Action == ActionConfirmDefault {
		return request.ExpectedRevision
	}
	return request.ExpectedRevision + 1
}
