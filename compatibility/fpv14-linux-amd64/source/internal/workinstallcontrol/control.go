package workinstallcontrol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"time"

	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/workevent"
)

const RequestSchema = "xinsight.local-work-install-request.v1"
const ResponseSchema = "xinsight.local-work-install-response.v1"
const ActionIssue = "issue"
const ActionCandidates = "candidates"
const ActionRead = "read"
const ActionList = "list"
const ActionApprove = "approve"
const ActionPickup = "pickup"
const ActionRenew = "renew"
const ActionFutureCandidates = "future_candidates"
const ActionFutureDecisions = "future_decisions"
const ActionStatus = "status"

var subject = regexp.MustCompile(`^xs_[a-z0-9]{16,64}$`)
var invitationID = regexp.MustCompile(`^xiei_[a-f0-9]{32}$`)
var invitationToken = regexp.MustCompile(`^xiet_[A-Za-z0-9_-]{43}$`)
var tokenDigest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var serviceIdentity = regexp.MustCompile(`^xsi_[A-Za-z0-9_-]{22}$`)

type Request struct {
	SchemaVersion       string                  `json:"schema_version"`
	Action              string                  `json:"action"`
	ActorSubject        string                  `json:"actor_subject,omitempty"`
	InvitationID        string                  `json:"invitation_id,omitempty"`
	InvitationToken     string                  `json:"invitation_token,omitempty"`
	IngestToken         string                  `json:"ingest_token,omitempty"`
	TokenDigest         string                  `json:"token_digest,omitempty"`
	IdentityID          string                  `json:"identity_id,omitempty"`
	ReplacementToken    string                  `json:"replacement_token,omitempty"`
	DisplayName         string                  `json:"display_name,omitempty"`
	ExpiresInSeconds    int64                   `json:"expires_in_seconds,omitempty"`
	ListAfter           string                  `json:"list_after,omitempty"`
	Projects            []db.WorkInstallProject `json:"projects,omitempty"`
	Status              *db.WorkInstallStatus   `json:"status,omitempty"`
	Mappings            []db.WorkInstallMapping `json:"mappings,omitempty"`
	ExcludedProjectKeys []string                `json:"excluded_project_keys,omitempty"`
	FutureAgentsAllowed bool                    `json:"future_agents_allowed,omitempty"`
	RequestedAt         time.Time               `json:"requested_at"`
}

type Response struct {
	SchemaVersion string                     `json:"schema_version"`
	Action        string                     `json:"action"`
	State         string                     `json:"state"`
	Invitation    *db.WorkInstallInvitation  `json:"invitation,omitempty"`
	Items         []db.WorkInstallInvitation `json:"items"`
	ExpiresAt     *time.Time                 `json:"expires_at,omitempty"`
	InvitationID  string                     `json:"invitation_id,omitempty"`
	NextCursor    string                     `json:"next_cursor,omitempty"`
}

func ValidRequest(request Request) bool {
	if request.SchemaVersion != RequestSchema || request.RequestedAt.IsZero() || request.RequestedAt.Location() != time.UTC {
		return false
	}
	admin := subject.MatchString(request.ActorSubject)
	if request.Action != ActionRenew && request.Action != ActionFutureCandidates && request.Action != ActionStatus &&
		(request.TokenDigest != "" || request.IdentityID != "") {
		return false
	}
	if request.Action != ActionStatus && request.Status != nil {
		return false
	}
	if request.Action != ActionList && request.ListAfter != "" {
		return false
	}
	if request.Action != ActionRenew && request.ReplacementToken != "" {
		return false
	}
	if request.Action != ActionApprove && request.FutureAgentsAllowed {
		return false
	}
	if request.Action != ActionApprove && request.Action != ActionFutureDecisions && len(request.ExcludedProjectKeys) > 0 {
		return false
	}
	switch request.Action {
	case ActionIssue:
		return admin && request.InvitationID == "" && request.InvitationToken == "" && request.IngestToken == "" &&
			len(request.DisplayName) >= 1 && len(request.DisplayName) <= 128 && request.ExpiresInSeconds >= 600 &&
			request.ExpiresInSeconds <= 86400 && len(request.Projects) == 0 && len(request.Mappings) == 0
	case ActionCandidates:
		return !admin && request.ActorSubject == "" && invitationToken.MatchString(request.InvitationToken) &&
			request.IngestToken != "" && request.InvitationID == "" && request.DisplayName == "" &&
			request.ExpiresInSeconds == 0 && len(request.Projects) >= 1 && len(request.Projects) <= 32 && len(request.Mappings) == 0
	case ActionPickup:
		return !admin && request.ActorSubject == "" && invitationToken.MatchString(request.InvitationToken) &&
			request.InvitationID == "" && request.IngestToken != "" && request.DisplayName == "" &&
			request.ExpiresInSeconds == 0 && len(request.Projects) == 0 && len(request.Mappings) == 0
	case ActionRenew:
		return !admin && request.ActorSubject == "" && request.InvitationID == "" && request.InvitationToken == "" &&
			request.IngestToken == "" && request.DisplayName == "" && request.ExpiresInSeconds == 0 &&
			len(request.Projects) == 0 && len(request.Mappings) == 0 && request.ReplacementToken != "" &&
			tokenDigest.MatchString(request.TokenDigest) && serviceIdentity.MatchString(request.IdentityID)
	case ActionFutureCandidates:
		return !admin && request.ActorSubject == "" && request.InvitationID == "" && request.InvitationToken == "" &&
			request.IngestToken == "" && request.ReplacementToken == "" && request.DisplayName == "" &&
			request.ExpiresInSeconds == 0 && len(request.Mappings) == 0 &&
			len(request.Projects) >= 1 && len(request.Projects) <= 32 &&
			tokenDigest.MatchString(request.TokenDigest) && serviceIdentity.MatchString(request.IdentityID)
	case ActionStatus:
		return !admin && request.ActorSubject == "" && request.InvitationID == "" && request.InvitationToken == "" &&
			request.IngestToken == "" && request.ReplacementToken == "" && request.DisplayName == "" &&
			request.ExpiresInSeconds == 0 && len(request.Projects) == 0 && len(request.Mappings) == 0 &&
			db.ValidWorkInstallStatus(request.Status) && tokenDigest.MatchString(request.TokenDigest) &&
			serviceIdentity.MatchString(request.IdentityID)
	case ActionRead:
		return admin && invitationID.MatchString(request.InvitationID) && request.InvitationToken == "" &&
			request.IngestToken == "" && request.DisplayName == "" && request.ExpiresInSeconds == 0 &&
			len(request.Projects) == 0 && len(request.Mappings) == 0
	case ActionList:
		return admin && request.InvitationID == "" && request.InvitationToken == "" && request.IngestToken == "" &&
			request.DisplayName == "" && request.ExpiresInSeconds == 0 && len(request.Projects) == 0 && len(request.Mappings) == 0 &&
			(request.ListAfter == "" || invitationID.MatchString(request.ListAfter))
	case ActionApprove:
		return admin && invitationID.MatchString(request.InvitationID) && request.InvitationToken == "" &&
			request.IngestToken == "" && request.DisplayName == "" && request.ExpiresInSeconds == 0 &&
			len(request.Projects) == 0 && len(request.Mappings) >= 1 &&
			len(request.Mappings)+len(request.ExcludedProjectKeys) <= 32
	case ActionFutureDecisions:
		return admin && invitationID.MatchString(request.InvitationID) && request.InvitationToken == "" &&
			request.IngestToken == "" && request.ReplacementToken == "" && request.DisplayName == "" &&
			request.ExpiresInSeconds == 0 && len(request.Projects) == 0 &&
			len(request.Mappings)+len(request.ExcludedProjectKeys) >= 1 &&
			len(request.Mappings)+len(request.ExcludedProjectKeys) <= 32
	}
	return false
}

type Store interface {
	IssueWorkInstallInvitation(context.Context, string, string, time.Time, time.Duration, io.Reader) (db.WorkInstallInvitation, error)
	SubmitWorkInstallCandidates(context.Context, string, string, []db.WorkInstallProject, time.Time) (db.WorkInstallInvitation, error)
	ReadWorkInstallInvitation(context.Context, string) (db.WorkInstallInvitation, error)
	ListWorkInstallInvitations(context.Context, time.Time, string) ([]db.WorkInstallInvitation, error)
	ApproveWorkInstallInvitation(context.Context, string, string, []db.WorkInstallMapping, []string, bool, time.Time, io.Reader) (db.WorkInstallInvitation, error)
	PickupWorkInstallInvitation(context.Context, string, string, time.Time) (db.WorkInstallInvitation, error)
	RenewWorkInstallCredential(context.Context, string, string, string, time.Time) (time.Time, error)
	ReportFutureWorkInstallProjects(context.Context, string, string, []db.WorkInstallProject, time.Time) (db.WorkInstallInvitation, error)
	DecideFutureWorkInstallProjects(context.Context, string, string, []db.WorkInstallMapping, []string, time.Time) (db.WorkInstallInvitation, error)
	ReportWorkInstallStatus(context.Context, string, string, db.WorkInstallStatus, time.Time) (string, error)
}

type Manager struct{ Store Store }

func (m Manager) Execute(ctx context.Context, request Request) (Response, error) {
	if m.Store == nil || !ValidRequest(request) {
		return Response{}, workevent.ErrInvalid
	}
	response := Response{SchemaVersion: ResponseSchema, Action: request.Action}
	var invitation db.WorkInstallInvitation
	var err error
	switch request.Action {
	case ActionIssue:
		invitation, err = m.Store.IssueWorkInstallInvitation(ctx, request.ActorSubject, request.DisplayName,
			request.RequestedAt, time.Duration(request.ExpiresInSeconds)*time.Second, nil)
	case ActionCandidates:
		invitation, err = m.Store.SubmitWorkInstallCandidates(ctx, request.InvitationToken, request.IngestToken,
			request.Projects, request.RequestedAt)
	case ActionRead:
		invitation, err = m.Store.ReadWorkInstallInvitation(ctx, request.InvitationID)
	case ActionPickup:
		invitation, err = m.Store.PickupWorkInstallInvitation(ctx, request.InvitationToken, request.IngestToken, request.RequestedAt)
	case ActionApprove:
		invitation, err = m.Store.ApproveWorkInstallInvitation(ctx, request.InvitationID, request.ActorSubject,
			request.Mappings, request.ExcludedProjectKeys, request.FutureAgentsAllowed, request.RequestedAt, nil)
	case ActionList:
		response.Items, err = m.Store.ListWorkInstallInvitations(ctx, request.RequestedAt, request.ListAfter)
		if err == nil && len(response.Items) > 20 {
			response.NextCursor = response.Items[19].InvitationID
			response.Items = response.Items[:20]
		}
	case ActionRenew:
		var expiry time.Time
		expiry, err = m.Store.RenewWorkInstallCredential(ctx, request.IdentityID, request.TokenDigest, request.ReplacementToken, request.RequestedAt)
		response.ExpiresAt = &expiry
	case ActionFutureCandidates:
		invitation, err = m.Store.ReportFutureWorkInstallProjects(ctx, request.IdentityID, request.TokenDigest, request.Projects, request.RequestedAt)
	case ActionFutureDecisions:
		invitation, err = m.Store.DecideFutureWorkInstallProjects(ctx, request.InvitationID, request.ActorSubject,
			request.Mappings, request.ExcludedProjectKeys, request.RequestedAt)
	case ActionStatus:
		response.InvitationID, err = m.Store.ReportWorkInstallStatus(ctx, request.IdentityID, request.TokenDigest,
			*request.Status, request.RequestedAt)
	}
	if err != nil {
		if errors.Is(err, workevent.ErrConflict) && (request.Action == ActionFutureCandidates || request.Action == ActionRenew) {
			response.State = "conflict"
			return response, nil
		}
		if errors.Is(err, workevent.ErrScopeChanged) && (request.Action == ActionFutureCandidates || request.Action == ActionRenew || request.Action == ActionStatus) {
			response.State = "scope_changed"
			return response, nil
		}
		return Response{}, err
	}
	if request.Action == ActionStatus {
		response.State = "reported"
	} else if request.Action == ActionRenew {
		response.State = "renewed"
	} else if request.Action == ActionList {
		response.State = "read"
	} else {
		if request.ActorSubject == "" {
			invitation.LatestStatus = nil
			invitation.StatusReceivedAt = nil
		}
		response.State = invitation.State
		response.Invitation = &invitation
	}
	return response, nil
}

// The product server crosses only the existing root-owned Unix control socket.
type UnixClient struct {
	SocketPath string
	Dial       func(context.Context, string) (net.Conn, error)
}

func NewUnixClient() *UnixClient {
	dialer := net.Dialer{Timeout: 2 * time.Second}
	return &UnixClient{SocketPath: connectorcontrol.LocalControllerSocket,
		Dial: func(ctx context.Context, path string) (net.Conn, error) { return dialer.DialContext(ctx, "unix", path) }}
}
func (c *UnixClient) Execute(ctx context.Context, request Request) (Response, error) {
	if c == nil || c.SocketPath != connectorcontrol.LocalControllerSocket || c.Dial == nil || !ValidRequest(request) {
		return Response{}, workevent.ErrInvalid
	}
	wire, err := json.Marshal(request)
	if err != nil || len(wire) >= 16<<10 {
		return Response{}, workevent.ErrInvalid
	}
	conn, err := c.Dial(ctx, c.SocketPath)
	if err != nil {
		return Response{}, workevent.ErrUnavailable
	}
	defer conn.Close()
	deadline := time.Now().Add(28 * time.Second)
	if bound, ok := ctx.Deadline(); ok && bound.Before(deadline) {
		deadline = bound
	}
	if err = conn.SetDeadline(deadline); err != nil {
		return Response{}, err
	}
	if _, err = conn.Write(append(wire, '\n')); err != nil {
		return Response{}, workevent.ErrUnavailable
	}
	raw, err := bufio.NewReader(io.LimitReader(conn, 256<<10+1)).ReadBytes('\n')
	if err != nil || len(raw) < 2 || len(raw) > 256<<10 {
		return Response{}, workevent.ErrUnavailable
	}
	var response Response
	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimSuffix(raw, []byte{'\n'})))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&response) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) ||
		response.SchemaVersion != ResponseSchema || response.Action != request.Action || !validResponse(request, response) {
		return Response{}, fmt.Errorf("invalid work installation controller response")
	}
	return response, nil
}
func validResponse(request Request, response Response) bool {
	if (request.Action == ActionFutureCandidates || request.Action == ActionRenew || request.Action == ActionStatus) && response.Invitation == nil &&
		(response.State == "conflict" || response.State == "scope_changed") {
		return true
	}
	if request.Action == ActionStatus {
		return response.State == "reported" && response.Invitation == nil && response.ExpiresAt == nil &&
			invitationID.MatchString(response.InvitationID)
	}
	if request.Action == ActionRenew {
		return response.State == "renewed" && response.Invitation == nil && response.ExpiresAt != nil &&
			!response.ExpiresAt.IsZero()
	}
	if request.Action == ActionList {
		return response.State == "read" && response.Invitation == nil && response.Items != nil && len(response.Items) <= 20 &&
			(response.NextCursor == "" || (len(response.Items) == 20 && response.NextCursor == response.Items[19].InvitationID))
	}
	i := response.Invitation
	if i == nil || i.State != response.State || !invitationID.MatchString(i.InvitationID) {
		return false
	}
	if request.Action == ActionIssue {
		return invitationToken.MatchString(i.InvitationToken) && i.State == "invited"
	}
	if i.InvitationToken != "" {
		return false
	}
	if request.ActorSubject == "" && (i.LatestStatus != nil || i.StatusReceivedAt != nil) {
		return false
	}
	if request.Action == ActionRead || request.Action == ActionApprove || request.Action == ActionFutureDecisions {
		return i.InvitationID == request.InvitationID
	}
	return response.State == "invited" || response.State == "candidates" || response.State == "approved" || response.State == "expired"
}
