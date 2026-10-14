package workinstallcontrol

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/workevent"
)

func TestStatusRequestAndControllerResponseBoundary(t *testing.T) {
	zero := int64(0)
	gaps := []db.WorkInstallGap{}
	status := &db.WorkInstallStatus{MetadataPending: &zero, MetadataBlocked: &zero, BodyPending: &zero, Gaps: &gaps}
	id := "xiei_" + strings.Repeat("a", 32)
	request := Request{SchemaVersion: RequestSchema, Action: ActionStatus, RequestedAt: time.Now().UTC(),
		IdentityID: "xsi_" + strings.Repeat("A", 22), TokenDigest: "sha256:" + strings.Repeat("a", 64), Status: status}
	if !ValidRequest(request) {
		t.Fatal("valid status request rejected")
	}
	for _, bad := range []Request{
		func() Request { x := request; x.InvitationID = id; return x }(),
		func() Request { x := request; x.ActorSubject = "xs_1111111111111111"; return x }(),
		func() Request { x := request; x.Status = nil; return x }(),
		func() Request { x := request; x.ListAfter = id; return x }(),
	} {
		if ValidRequest(bad) {
			t.Fatalf("invalid status request accepted: %+v", bad)
		}
	}
	response := Response{SchemaVersion: ResponseSchema, Action: ActionStatus, State: "reported", InvitationID: id}
	if !validResponse(request, response) {
		t.Fatal("minimal status receipt rejected")
	}
	response.Invitation = &db.WorkInstallInvitation{InvitationID: id, State: "approved"}
	if validResponse(request, response) {
		t.Fatal("status receipt leaked invitation")
	}
	response.Invitation = nil
	response.InvitationID = ""
	if validResponse(request, response) {
		t.Fatal("status receipt omitted installation identity")
	}

	host := Request{Action: ActionPickup}
	invitation := db.WorkInstallInvitation{InvitationID: id, State: "approved", LatestStatus: status}
	if validResponse(host, Response{State: "approved", Invitation: &invitation}) {
		t.Fatal("host controller response leaked administrator status")
	}
	listing := Request{SchemaVersion: RequestSchema, Action: ActionList, ActorSubject: "xs_1111111111111111",
		RequestedAt: time.Now().UTC(), ListAfter: id}
	if !ValidRequest(listing) {
		t.Fatal("valid page cursor rejected")
	}
	listing.ListAfter = "invalid"
	if ValidRequest(listing) {
		t.Fatal("invalid page cursor accepted")
	}
}

func TestWorkInstallBoundedControllerResponseFits256KiB(t *testing.T) {
	id := "xiei_" + strings.Repeat("a", 32)
	one := int64(1)
	allCodes := []string{"body_limit", "body_unavailable", "metadata_unavailable", "receiver_unavailable",
		"source_mapping_unconfirmed", "future_scope_unconfirmed", "renewal_unconfirmed", "ur_relation_unconfirmed",
		"outbox_full", "observation_unavailable"}
	gaps := make([]db.WorkInstallGap, 0, len(allCodes))
	for _, code := range allCodes {
		gaps = append(gaps, db.WorkInstallGap{Code: code, Occurrences: 9007199254740991})
	}
	status := &db.WorkInstallStatus{MetadataPending: &one, MetadataBlocked: &one, BodyPending: &one, Gaps: &gaps}
	invitation := db.WorkInstallInvitation{InvitationID: id, DisplayName: strings.Repeat("<", 128),
		SourceInstance: "openclaw-" + strings.Repeat("a", 32), State: "approved", LatestStatus: status}
	target := &workevent.Scope{SourceSystemID: "11111111-1111-1111-1111-111111111111",
		SourceInstance: strings.Repeat("s", 128), OrgID: "9223372036854775807", ProjectID: "9223372036854775807"}
	for i := 0; i < 32; i++ {
		key := strings.Repeat("<", 250) + fmt.Sprintf("%06d", i)
		invitation.Projects = append(invitation.Projects, db.WorkInstallProject{ProjectKey: key, Label: strings.Repeat("<", 128)})
		invitation.Bindings = append(invitation.Bindings, db.WorkInstallApprovedBinding{
			ProjectKey: key, ProjectLabel: strings.Repeat("<", 128),
			ProjectID: "32", CollectionID: "11111111-1111-1111-1111-111111111111",
			XIProjectID: "11111111-1111-1111-1111-111111111111", XIProjectName: strings.Repeat("<", 128), URTarget: target,
		})
	}
	if !target.Valid() {
		t.Fatal("worst-case target is not valid")
	}
	read, err := json.Marshal(Response{SchemaVersion: ResponseSchema, Action: ActionRead, State: "approved", Invitation: &invitation})
	if err != nil || len(read) >= 256<<10 {
		t.Fatalf("bounded installation detail exceeds controller response: %d %v", len(read), err)
	}
	items := make([]db.WorkInstallInvitation, 20)
	for i := range items {
		items[i] = db.WorkInstallInvitation{InvitationID: id, DisplayName: strings.Repeat("<", 128), SourceInstance: invitation.SourceInstance, State: "approved"}
	}
	list, err := json.Marshal(Response{SchemaVersion: ResponseSchema, Action: ActionList, State: "read", Items: items})
	if err != nil || len(list) >= 256<<10 {
		t.Fatalf("bounded installation list exceeds controller response: %d %v", len(list), err)
	}
	t.Logf("maximum-shaped detail=%d bytes, compact 20-item list=%d bytes", len(read), len(list))
}
