package db_test

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/workevent"
	"github.com/foigent/x-insight/internal/workspace"
)

func TestWorkInstallInvitationBindsOneHostCredentialAfterHumanMapping(t *testing.T) {
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("isolated PG required")
	}
	ctx := t.Context()
	dsn := migrationTestDSN(t, ctx, base)
	if _, err := db.RunMigrations(ctx, dsn, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	owner, err := db.OpenSourceFactsStore(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	reader, err := db.OpenWorkspaceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	now := time.Now().UTC().Truncate(time.Second)
	issued, err := owner.IssueWorkInstallInvitation(ctx, "xs_1111111111111111", "Synthetic managed OpenClaw", now, time.Hour, nil)
	if err != nil || issued.State != "invited" || !strings.HasPrefix(issued.InvitationToken, "xiet_") {
		t.Fatalf("issue %+v %v", issued, err)
	}
	ingestToken, digest, err := workspace.NewServiceToken(nil)
	if err != nil {
		t.Fatal(err)
	}
	projects := []db.WorkInstallProject{{ProjectKey: "agent:main", Label: "Main agent"}}
	if _, err = owner.PickupWorkInstallInvitation(ctx, issued.InvitationToken, ingestToken, now); !errors.Is(err, workevent.ErrUnavailable) {
		t.Fatalf("pickup before candidate accepted: %v", err)
	}
	candidate, err := owner.SubmitWorkInstallCandidates(ctx, issued.InvitationToken, ingestToken, projects, now)
	if err != nil || candidate.State != "candidates" {
		t.Fatalf("candidate %+v %v", candidate, err)
	}
	if replay, err := owner.SubmitWorkInstallCandidates(ctx, issued.InvitationToken, ingestToken, projects, now); err != nil || replay.State != "candidates" {
		t.Fatalf("same candidate replay %+v %v", replay, err)
	}
	if _, err = owner.SubmitWorkInstallCandidates(ctx, issued.InvitationToken, ingestToken,
		[]db.WorkInstallProject{{ProjectKey: "agent:other", Label: "Other"}}, now); !errors.Is(err, workevent.ErrConflict) {
		t.Fatalf("changed candidate replay: %v", err)
	}
	if _, err = owner.ApproveWorkInstallInvitation(ctx, issued.InvitationID, "xs_1111111111111111",
		[]db.WorkInstallMapping{{ProjectKey: "agent:missing", XIProjectName: "No"}}, nil, true, now, nil); !errors.Is(err, workevent.ErrInvalid) {
		t.Fatalf("unlisted candidate approved: %v", err)
	}
	var sourcesBefore int
	if err = admin.QueryRowContext(ctx, `SELECT count(*) FROM source_systems WHERE source_key=$1`, "openclaw:"+issued.InvitationID).Scan(&sourcesBefore); err != nil || sourcesBefore != 0 {
		t.Fatalf("failed approval wrote source: %d %v", sourcesBefore, err)
	}
	approved, err := owner.ApproveWorkInstallInvitation(ctx, issued.InvitationID, "xs_1111111111111111",
		[]db.WorkInstallMapping{{ProjectKey: "agent:main", XIProjectName: "Synthetic XI project"}}, nil, true, now, nil)
	if err != nil || approved.State != "approved" || len(approved.Bindings) != 1 || approved.SourceSystemID == "" ||
		approved.ServiceIdentityID == "" || approved.Bindings[0].ProjectID != "1" {
		t.Fatalf("approval %+v %v", approved, err)
	}
	zero, one, two := int64(0), int64(1), int64(2)
	gaps := []db.WorkInstallGap{{Code: "body_limit", Occurrences: 2}}
	status := db.WorkInstallStatus{MetadataPending: &zero, MetadataBlocked: &one, BodyPending: &zero, Gaps: &gaps}
	firstReport := now.Add(2 * time.Hour) // Approved installation remains valid after invitation expiry.
	if id, statusErr := owner.ReportWorkInstallStatus(ctx, approved.ServiceIdentityID, digest, status, firstReport); statusErr != nil || id != issued.InvitationID {
		t.Fatalf("installation status after invitation expiry: %s %v", id, statusErr)
	}
	if id, statusErr := owner.ReportWorkInstallStatus(ctx, approved.ServiceIdentityID, digest, status, firstReport.Add(time.Minute)); statusErr != nil || id != issued.InvitationID {
		t.Fatalf("idempotent status replacement: %s %v", id, statusErr)
	}
	readStatus, err := owner.ReadWorkInstallInvitation(ctx, issued.InvitationID)
	if err != nil || readStatus.LatestStatus == nil || readStatus.StatusReceivedAt == nil ||
		*readStatus.LatestStatus.MetadataBlocked != 1 || (*readStatus.LatestStatus.Gaps)[0].Occurrences != 2 ||
		!readStatus.StatusReceivedAt.Equal(firstReport.Add(time.Minute)) {
		t.Fatalf("status readback: %+v %v", readStatus, err)
	}
	if listed, listErr := owner.ListWorkInstallInvitations(ctx, now.Add(2*time.Hour), ""); listErr != nil ||
		len(listed) == 0 || listed[0].InvitationID != issued.InvitationID || listed[0].StatusReceivedAt == nil ||
		listed[0].LatestStatus != nil {
		t.Fatalf("approved installation list summary: %+v %v", listed, listErr)
	}
	// Force the same created_at for more than one page. The installation ID
	// tie-breaker must keep every row reachable without duplication.
	tie := now.Add(-time.Minute)
	if _, err = admin.ExecContext(ctx, `UPDATE work_event_install_invitations SET created_at=$2 WHERE invitation_id=$1`,
		issued.InvitationID, tie); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 21; i++ {
		key := fmt.Sprintf("%032x", i+1000)
		if _, err = admin.ExecContext(ctx, `INSERT INTO work_event_install_invitations
			(invitation_id,invitation_digest,actor_subject,display_name,source_instance,state,expires_at,created_at)
			VALUES($1,$2,'xs_1111111111111111','Same-time page',$3,'invited',$4,$5)`,
			"xiei_"+key, "sha256:"+fmt.Sprintf("%064x", i+1000), "openclaw-"+key, now.Add(time.Hour), tie); err != nil {
			t.Fatal(err)
		}
	}
	firstPage, err := owner.ListWorkInstallInvitations(ctx, now.Add(10*time.Minute), "")
	if err != nil || len(firstPage) != 21 {
		t.Fatalf("first installation page: %d %v", len(firstPage), err)
	}
	secondPage, err := owner.ListWorkInstallInvitations(ctx, now.Add(10*time.Minute), firstPage[19].InvitationID)
	if err != nil || len(secondPage) != 2 {
		t.Fatalf("second installation page: %d %v", len(secondPage), err)
	}
	seenInstallations := map[string]bool{}
	for _, item := range append(firstPage[:20], secondPage...) {
		if seenInstallations[item.InvitationID] {
			t.Fatalf("duplicate installation page item: %s", item.InvitationID)
		}
		seenInstallations[item.InvitationID] = true
	}
	if len(seenInstallations) != 22 || !seenInstallations[issued.InvitationID] {
		t.Fatalf("installation cursor omitted row: %d", len(seenInstallations))
	}
	if _, statusErr := owner.ReportWorkInstallStatus(ctx, approved.ServiceIdentityID,
		"sha256:"+strings.Repeat("0", 64), status, firstReport); !errors.Is(statusErr, workevent.ErrScopeChanged) {
		t.Fatalf("wrong digest reported status: %v", statusErr)
	}
	if _, statusErr := owner.ReportWorkInstallStatus(ctx, "xsi_"+strings.Repeat("B", 22), digest,
		status, firstReport); !errors.Is(statusErr, workevent.ErrScopeChanged) {
		t.Fatalf("wrong identity reported status: %v", statusErr)
	}
	status.MetadataPending = &two
	if _, statusErr := owner.ReportWorkInstallStatus(ctx, approved.ServiceIdentityID, digest,
		status, now.Add(91*24*time.Hour)); !errors.Is(statusErr, workevent.ErrScopeChanged) {
		t.Fatalf("expired token reported status: %v", statusErr)
	}
	if replay, err := owner.SubmitWorkInstallCandidates(ctx, issued.InvitationToken, ingestToken, projects, now.Add(time.Minute)); err != nil || replay.State != "approved" {
		t.Fatalf("lost candidate response recovery %+v %v", replay, err)
	}
	otherToken, _, err := workspace.NewServiceToken(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.PickupWorkInstallInvitation(ctx, issued.InvitationToken, otherToken, now); !errors.Is(err, workevent.ErrUnavailable) {
		t.Fatalf("wrong host token picked up: %v", err)
	}
	pickup, err := owner.PickupWorkInstallInvitation(ctx, issued.InvitationToken, ingestToken, now)
	if err != nil || pickup.State != "approved" || len(pickup.Bindings) != 1 || pickup.InvitationToken != "" {
		t.Fatalf("pickup %+v %v", pickup, err)
	}
	config, err := reader.ListWorkEventIngestConfig(ctx, digest, now.Add(time.Second))
	if err != nil || len(config.Items) != 1 || config.Items[0].ProducerProjectKey != "agent:main" ||
		config.Items[0].Reporter.SourceSystemID != approved.SourceSystemID || config.Items[0].BodyCapture.Mode != "disabled" ||
		!config.ManagedRenewal || config.InstallationID != issued.InvitationID {
		t.Fatalf("new host credential config %+v %v", config, err)
	}
	futureProject := []db.WorkInstallProject{{ProjectKey: "agent:new", Label: "New agent"}}
	future, err := owner.ReportFutureWorkInstallProjects(ctx, approved.ServiceIdentityID, digest, futureProject, now.Add(time.Minute))
	if err != nil || len(future.Bindings) != 2 || future.Bindings[1].ProjectKey != "agent:new" || future.Bindings[1].URTarget != nil ||
		future.Bindings[1].XIProjectID == approved.Bindings[0].XIProjectID || !future.FutureAgentsAllowed {
		t.Fatalf("future-agent auto binding %+v %v", future, err)
	}
	if replay, replayErr := owner.ReportFutureWorkInstallProjects(ctx, approved.ServiceIdentityID, digest, futureProject, now.Add(2*time.Minute)); replayErr != nil || len(replay.Bindings) != 2 {
		t.Fatalf("future-agent idempotency %+v %v", replay, replayErr)
	}
	if replay, replayErr := owner.ReportFutureWorkInstallProjects(ctx, approved.ServiceIdentityID, digest,
		[]db.WorkInstallProject{{ProjectKey: "agent:new", Label: "Renamed without approval"}}, now.Add(3*time.Minute)); replayErr != nil ||
		len(replay.Bindings) != 2 || replay.Bindings[1].ProjectLabel != "New agent" {
		t.Fatalf("label drift blocked or changed binding: %+v %v", replay, replayErr)
	}
	config, err = reader.ListWorkEventIngestConfig(ctx, digest, now.Add(4*time.Minute))
	if err != nil || len(config.Items) != 2 {
		t.Fatalf("future source not visible in ingest config: %+v %v", config, err)
	}
	replacementToken, replacementDigest, err := workspace.NewServiceToken(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.RenewWorkInstallCredential(ctx, approved.ServiceIdentityID, digest, replacementToken, now.Add(40*24*time.Hour)); !errors.Is(err, workevent.ErrScopeChanged) {
		t.Fatalf("early renewal accepted: %v", err)
	}
	if _, err = owner.RenewWorkInstallCredential(ctx, approved.ServiceIdentityID, strings.Repeat("x", len(digest)), replacementToken, now.Add(60*24*time.Hour)); !errors.Is(err, workevent.ErrInvalid) {
		t.Fatalf("invalid renewal digest accepted: %v", err)
	}
	newExpiry, err := owner.RenewWorkInstallCredential(ctx, approved.ServiceIdentityID, digest, replacementToken, now.Add(60*24*time.Hour))
	if err != nil || !newExpiry.Equal(now.Add(150*24*time.Hour)) || replacementToken == ingestToken {
		t.Fatalf("managed token renewal: expiry=%s err=%v", newExpiry, err)
	}
	if replayExpiry, replayErr := owner.RenewWorkInstallCredential(ctx, approved.ServiceIdentityID, digest, replacementToken, now.Add(61*24*time.Hour)); replayErr != nil || !replayExpiry.Equal(newExpiry) {
		t.Fatalf("same replacement did not replay: %s %v", replayExpiry, replayErr)
	}
	otherReplacement, _, err := workspace.NewServiceToken(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.RenewWorkInstallCredential(ctx, approved.ServiceIdentityID, digest, otherReplacement, now.Add(61*24*time.Hour)); !errors.Is(err, workevent.ErrConflict) {
		t.Fatalf("different replacement accepted: %v", err)
	}
	if _, err = reader.ListWorkEventIngestConfig(ctx, replacementDigest, now.Add(91*24*time.Hour)); err != nil {
		t.Fatalf("replacement token failed after old expiry: %v", err)
	}
	if _, err = reader.AuthenticateServiceToken(ctx, ingestToken, now.Add(91*24*time.Hour)); !errors.Is(err, workspace.ErrCredentialExpired) {
		t.Fatalf("expired old token accepted: %v", err)
	}
	if _, err = reader.AuthenticateServiceToken(ctx, replacementToken, now.Add(91*24*time.Hour)); err != nil {
		t.Fatalf("old-token expiry disabled renewed identity: %v", err)
	}
	if id, statusErr := owner.ReportWorkInstallStatus(ctx, approved.ServiceIdentityID, replacementDigest,
		status, now.Add(91*24*time.Hour)); statusErr != nil || id != issued.InvitationID {
		t.Fatalf("renewed token status: %s %v", id, statusErr)
	}
	manualInvite, err := owner.IssueWorkInstallInvitation(ctx, "xs_1111111111111111", "Synthetic manual future", now, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	manualToken, manualDigest, err := workspace.NewServiceToken(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.SubmitWorkInstallCandidates(ctx, manualInvite.InvitationToken, manualToken,
		[]db.WorkInstallProject{{ProjectKey: "agent:main", Label: "Manual main"}}, now); err != nil {
		t.Fatal(err)
	}
	manualApproval, err := owner.ApproveWorkInstallInvitation(ctx, manualInvite.InvitationID, "xs_1111111111111111",
		[]db.WorkInstallMapping{{ProjectKey: "agent:main", XIProjectName: "Manual XI"}}, nil, false, now, nil)
	if err != nil || manualApproval.FutureAgentsAllowed {
		t.Fatalf("manual future approval: %+v %v", manualApproval, err)
	}
	pending, err := owner.ReportFutureWorkInstallProjects(ctx, manualApproval.ServiceIdentityID, manualDigest,
		[]db.WorkInstallProject{{ProjectKey: "agent:new", Label: "Manual new"}, {ProjectKey: "agent:exclude", Label: "Exclude me"}}, now.Add(time.Minute))
	if err != nil || len(pending.FutureCandidates) != 2 || len(pending.Bindings) != 1 {
		t.Fatalf("future queue %+v %v", pending, err)
	}
	decided, err := owner.DecideFutureWorkInstallProjects(ctx, manualInvite.InvitationID, "xs_1111111111111111",
		[]db.WorkInstallMapping{{ProjectKey: "agent:new", XIProjectName: "Manual new XI"}}, []string{"agent:exclude"}, now.Add(2*time.Minute))
	if err != nil || len(decided.FutureCandidates) != 0 || len(decided.Bindings) != 2 || len(decided.ExcludedFutureProjectKeys) != 1 {
		t.Fatalf("future manual decision %+v %v", decided, err)
	}
	ignored, err := owner.ReportFutureWorkInstallProjects(ctx, manualApproval.ServiceIdentityID, manualDigest,
		[]db.WorkInstallProject{{ProjectKey: "agent:exclude", Label: "Exclude me"}}, now.Add(3*time.Minute))
	if err != nil || len(ignored.Bindings) != 2 || len(ignored.FutureCandidates) != 0 {
		t.Fatalf("excluded future candidate reopened: %+v %v", ignored, err)
	}
	// The 32-key budget includes excluded historical keys. Repeated manual
	// exclusions must not grow the invitation row without bound.
	bulk := make([]db.WorkInstallProject, 29)
	bulkKeys := make([]string, 29)
	for i := range bulk {
		bulkKeys[i] = fmt.Sprintf("agent:bulk-%d", i)
		bulk[i] = db.WorkInstallProject{ProjectKey: bulkKeys[i], Label: "Bulk candidate"}
	}
	if full, fullErr := owner.ReportFutureWorkInstallProjects(ctx, manualApproval.ServiceIdentityID, manualDigest,
		bulk, now.Add(4*time.Minute)); fullErr != nil || len(full.FutureCandidates) != 29 {
		t.Fatalf("cumulative 32-key queue: %+v %v", full, fullErr)
	}
	if _, err = owner.DecideFutureWorkInstallProjects(ctx, manualInvite.InvitationID, "xs_1111111111111111",
		nil, bulkKeys, now.Add(5*time.Minute)); err != nil {
		t.Fatalf("cumulative 32-key exclusions: %v", err)
	}
	if _, err = owner.ReportFutureWorkInstallProjects(ctx, manualApproval.ServiceIdentityID, manualDigest,
		[]db.WorkInstallProject{{ProjectKey: "agent:overflow", Label: "Overflow"}}, now.Add(6*time.Minute)); !errors.Is(err, workevent.ErrConflict) {
		t.Fatalf("excluded historical keys did not count toward 32-key budget: %v", err)
	}
	var invitationRow string
	if err = admin.QueryRowContext(ctx, `SELECT to_jsonb(i)::text FROM work_event_install_invitations i WHERE invitation_id=$1`, issued.InvitationID).Scan(&invitationRow); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(invitationRow, issued.InvitationToken) || strings.Contains(invitationRow, ingestToken) {
		t.Fatal("installation table retained a plaintext bearer")
	}
	for _, role := range []string{"xinsight_workspace", "xinsight_importer", "xinsight_reader"} {
		tx, txErr := admin.BeginTx(ctx, nil)
		if txErr != nil {
			t.Fatal(txErr)
		}
		if _, txErr = tx.ExecContext(ctx, "SET LOCAL ROLE "+role); txErr != nil {
			tx.Rollback()
			t.Fatal(txErr)
		}
		var count int
		if role == "xinsight_workspace" {
			var managedID string
			if functionErr := tx.QueryRowContext(ctx, `SELECT public.xinsight_managed_work_install_id($1)`, approved.ServiceIdentityID).Scan(&managedID); functionErr != nil || managedID != issued.InvitationID {
				tx.Rollback()
				t.Fatalf("workspace managed identity projection: %s %v", managedID, functionErr)
			}
		}
		readErr := tx.QueryRowContext(ctx, `SELECT count(*) FROM work_event_install_invitations`).Scan(&count)
		tx.Rollback()
		if readErr == nil {
			t.Fatalf("%s read owner invitation table", role)
		}
	}
	if _, err = admin.ExecContext(ctx, `UPDATE service_identity_tokens SET revoked_at=$2 WHERE token_digest=$1`,
		replacementDigest, now.Add(92*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, statusErr := owner.ReportWorkInstallStatus(ctx, approved.ServiceIdentityID, replacementDigest,
		status, now.Add(93*24*time.Hour)); !errors.Is(statusErr, workevent.ErrScopeChanged) {
		t.Fatalf("revoked token reported status: %v", statusErr)
	}
}
