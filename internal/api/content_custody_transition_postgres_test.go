package api_test

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

// The same authenticated HTTP reference reads UR before transition and XI after
// it. A missing local object must fail closed even while UR remains available.
func TestContentCustodyTransitionPostgresRuntimeHTTPSNoSourceFallback(t *testing.T) {
	body := []byte(`{"value":1,"other":2}`)
	keys := reviewbody.KeyRing{ActiveID: "synthetic", Keys: map[string][]byte{"synthetic": bytes.Repeat([]byte{7}, 32)}}
	f := newScopedContentFixtureWithCustody(t, body, keys)
	seedScopedContentI01WithoutB(t, f)
	owner := db.NewWorkspaceStore(f.admin)
	// apiOffset advances only the reading cutoff past the projection commit.
	// Invitation redemption uses PostgreSQL's real clock and rejects a future
	// creation time, so control operations use the fixture's actual base time.
	now := f.base
	inv, _, err := owner.CreateInvitation(f.ctx, connectorcontrol.CreateInvitationInput{ActorSubject: scopedContentSubject, Config: connectorcontrol.InvitationConfig{DeploymentID: "xid_AAAAAAAAAAAAAAAAAAAAAA", XInsightOrigin: "https://xinsight.example", UniRouterInstallOrigin: "https://unirouter.example"}, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.RedeemInvitation(f.ctx, connectorcontrol.RedeemInvitationInput{InvitationToken: inv.Token, ProjectID: "project.fixture", FinalEventScope: []string{connectorcontrol.CompletedEventType}, ConsentSHA256: sourcefacts.Hash([]byte("original-consent")), ConfirmedAt: now, ControlToken: "xib_" + strings.Repeat("A", 43), ControlTokenExpires: now.Add(time.Hour)}, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.admin.ExecContext(f.ctx, `UPDATE connector_installations SET status='connected',observed_state='connected',source_instance=$2,source_instance_sha256=$3 WHERE id=$1`, inv.InstallationID, f.collection.SourceInstance, sourcefacts.Hash([]byte(f.collection.SourceInstance))); err != nil {
		t.Fatal(err)
	}
	if err = owner.SaveManagedEnrollment(f.ctx, db.ManagedEnrollment{InstallationID: inv.InstallationID, EnrollmentID: "xise_1", SourceInstance: f.collection.SourceInstance, OrgID: f.collection.OrgID, ProjectID: f.collection.ProjectID, Revision: "1", ContentCollectionID: f.collection.ID, Status: "requires_migration", LegacyCCopyPending: true}, now); err != nil {
		t.Fatal(err)
	}
	input := httpReadingRequest(f.sourceID)
	input.Filters.SourceProjects = []string{"project-" + f.collection.ProjectID}
	var reading workbench.RuntimeReading
	if err = json.Unmarshal(f.must(t, "POST", "/api/v1/runtime/readings", workbench.RuntimeReadingRequest{ContractVersion: workbench.RuntimeContractVersion, MetricVersion: workbench.RuntimeMetricVersion, Period: input.Period, Filters: input.Filters}, 201, nil), &reading); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/runtime/readings/" + reading.ID
	var requests workbench.RuntimeRequests
	if err = json.Unmarshal(f.must(t, "GET", base+"/requests?entry_time=all", nil, 200, nil), &requests); err != nil || len(requests.Items) != 1 {
		t.Fatal("historical request missing", err)
	}
	ref := f.choice.Reference
	path := base + "/requests/" + requests.Items[0].Ref + "/contents/" + ref.ContentRef + "?source_fact_id=" + url.QueryEscape(ref.SourceFactID) + "&revision=" + ref.Revision
	f.resetSource()
	if raw := f.must(t, "GET", path, nil, 200, nil); !bytes.Equal(raw, body) {
		t.Fatal("legacy HTTP read changed original bytes")
	}
	// Live reads revalidate capabilities before publication. Their exact count
	// is not the custody contract: prove the source path was used, then require
	// exactly zero source calls after the custody transition below.
	f.mu.Lock()
	caps, bodies, hookErr := f.caps, f.bodies, f.hookErr
	f.mu.Unlock()
	if caps < 1 || bodies != 1 || hookErr != nil {
		t.Fatalf("legacy source calls caps=%d body=%d hook=%v", caps, bodies, hookErr)
	}

	revision, err := f.consumer.RequestReviewCaptureDefault(f.ctx, db.ReviewCaptureSettings{Mode: "full_content", RetentionSeconds: 7200, ProjectQuotaBytes: 4096, MaxItemBytes: 1024}, 0, "xs_admin", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.consumer.ConfirmReviewCaptureDefault(f.ctx, revision, "xs_admin", now); err != nil {
		t.Fatal(err)
	}
	if _, err = f.consumer.EnsureManagedReviewCaptureProject(f.ctx, inv.InstallationID, f.collection.ID, f.sourceID, f.collection.SourceInstance, f.collection.OrgID, f.collection.ProjectID, now); err != nil {
		t.Fatal(err)
	}
	// Commit through the actual restricted workspace identity, as the copy worker
	// does. The owner connection is only for control-plane preparation/transition.
	if receipt, e := f.store.CommitURCReviewBody(f.ctx, f.collection.ID, ref, body, keys, now); e != nil || receipt.Outcome != "accepted" {
		t.Fatalf("restricted copy acceptance: %+v %v", receipt, e)
	}
	if err = owner.FinishLegacyCCustodyTransition(f.ctx, f.collection.ID, now); err != nil {
		t.Fatal(err)
	}
	if managed, e := f.store.IsManagedCustodyCollection(f.ctx, f.collection.ID); e != nil || !managed {
		t.Fatal("custody did not transition", e)
	}
	f.resetSource()
	if raw := f.must(t, "GET", path, nil, 200, nil); !bytes.Equal(raw, body) {
		t.Fatal("same HTTP reference lost local historical bytes")
	}
	f.assertSource(t, 0, 0)

	// Simulate loss of this single synthetic ciphertext object in the disposable
	// fixture DB. Keep its manifest, accepted receipt and available UR unchanged.
	result, err := f.admin.ExecContext(f.ctx, `DELETE FROM review_body_objects WHERE content_ref=$1`, ref.ContentRef)
	if err != nil {
		t.Fatal(err)
	}
	if count, e := result.RowsAffected(); e != nil || count != 1 {
		t.Fatal("synthetic object fault was not injected", count, e)
	}
	f.must(t, "GET", path, nil, 503, nil)
	f.assertSource(t, 0, 0)
}
