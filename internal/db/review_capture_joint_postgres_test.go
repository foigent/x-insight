package db_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/reviewbody"
)

func TestReviewCaptureJointEnterpriseIntentAndUROperationAreAtomic(t *testing.T) {
	if os.Getenv("XINSIGHT_POSTGRES_DSN") == "" {
		t.Skip("explicit PostgreSQL fixture required")
	}
	f := newSourceFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	serviceID := "xsi_" + strings.Repeat("s", 22)
	installationID := "xci_" + strings.Repeat("i", 22)
	sourceInstance := "ur-joint-synthetic"
	if _, err := f.admin.ExecContext(f.ctx, `INSERT INTO service_identities(id,display_name,status,permissions,created_by_subject,created_at,updated_at,expires_at)
 VALUES($1,'Synthetic paired UR control','active',ARRAY['connector.control.poll','connector.control.ack']::text[],'system',$2,$2,$3)`,
		serviceID, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	sourceHash := sha256.Sum256([]byte(sourceInstance))
	if _, err := f.admin.ExecContext(f.ctx, `INSERT INTO connector_installations(id,deployment_id,connector_type,requested_event_scope,final_event_scope,
 project_id,source_instance,source_instance_sha256,control_service_identity_id,installation_acknowledgement_sha256,
 status,desired_state,observed_state,created_at,updated_at)
 VALUES($1,'xid_AAAAAAAAAAAAAAAAAAAAAA','unirouter',ARRAY['model.request.completed']::text[],ARRAY['model.request.completed']::text[],
 '7',$2,$3,$4,$5,'connected','active','connected',$6,$6)`, installationID, sourceInstance,
		"sha256:"+hex.EncodeToString(sourceHash[:]), serviceID, "sha256:"+strings.Repeat("a", 64), now); err != nil {
		t.Fatal(err)
	}
	settings := db.ReviewCaptureSettings{Mode: "full_content", RetentionSeconds: 3600, ProjectQuotaBytes: 1 << 20, MaxItemBytes: 1024}
	op := db.ReviewCaptureUROperation{InstallationID: installationID, OperationRef: "xipw_" + strings.Repeat("a", 32),
		SourceInstance: sourceInstance, OrgID: "7", ScopeKind: "enterprise", ScopeID: "7",
		OperatorSubject: "xs_" + strings.Repeat("a", 16), RequestSHA256: "sha256:" + strings.Repeat("b", 64),
		XIDesiredRevision: 1, URExpectedRef: "content-policy:7:enterprise", URExpectedVersion: 0,
		URExpectedVector: []byte(`{"enterprise":0}`), State: "requested"}
	if err := f.consumerStore.CreateReviewCaptureJointUROperation(f.ctx, op, settings, 0, now); err != nil {
		t.Fatal(err)
	}
	var desired, effective int64
	if err := f.admin.QueryRowContext(f.ctx, `SELECT desired_revision,effective_revision FROM review_capture_defaults WHERE singleton`).Scan(&desired, &effective); err != nil || desired != 1 || effective != 0 {
		t.Fatalf("joint XI desired=%d effective=%d err=%v", desired, effective, err)
	}
	if revision, err := f.consumerStore.CurrentReviewCaptureDesiredRevisionAny(f.ctx); err != nil || revision != 1 {
		t.Fatalf("pending revision=%d %v", revision, err)
	}
	if _, err := f.consumerStore.CurrentReviewCaptureDesiredRevision(f.ctx); err == nil {
		t.Fatal("pending XI revision admitted as effective")
	}
	stored, err := f.consumerStore.ReadReviewCaptureUROperation(f.ctx, installationID, op.OperationRef)
	if err != nil || stored.XIDesiredRevision != 1 || stored.ScopeKind != "enterprise" {
		t.Fatalf("joint UR operation %+v %v", stored, err)
	}
	latest, err := f.consumerStore.ReadLatestReviewCaptureUROperation(f.ctx, installationID, "enterprise", "7")
	if err != nil || latest.OperationRef != op.OperationRef {
		t.Fatalf("latest enterprise operation %+v %v", latest, err)
	}
	if _, err = f.consumerStore.ReadLatestReviewCaptureUROperation(f.ctx, installationID, "project", "9"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-scope operation leaked: %v", err)
	}
	duplicate := op
	duplicate.XIDesiredRevision = 2
	if err := f.consumerStore.CreateReviewCaptureJointUROperation(f.ctx, duplicate, settings, 1, now); err == nil {
		t.Fatal("duplicate operation ref accepted")
	}
	if err := f.admin.QueryRowContext(f.ctx, `SELECT desired_revision,effective_revision FROM review_capture_defaults WHERE singleton`).Scan(&desired, &effective); err != nil || desired != 1 || effective != 0 {
		t.Fatalf("duplicate operation leaked XI revision desired=%d effective=%d err=%v", desired, effective, err)
	}
	duplicate.OperationRef = "xipw_" + strings.Repeat("c", 32)
	duplicate.ScopeKind = "project"
	duplicate.ScopeID = "9"
	if err := f.consumerStore.CreateReviewCaptureJointUROperation(f.ctx, duplicate, settings, 1, now); !errors.Is(err, reviewbody.ErrInvalid) {
		t.Fatalf("project joint update classification: %v", err)
	}
	if _, err := f.consumerStore.ReadReviewCaptureUROperation(f.ctx, installationID, duplicate.OperationRef); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("project joint operation persisted: %v", err)
	}
	for index, test := range []struct {
		name   string
		states []string
	}{
		{"direct-effective", []string{"effective"}},
		{"pending-then-effective", []string{"pending", "effective"}},
		{"unconfirmed-then-effective", []string{"unconfirmed", "effective"}},
		{"failed", []string{"failed"}},
		{"drift", []string{"drift"}},
	} {
		t.Run("audit-"+test.name, func(t *testing.T) {
			candidate := op
			candidate.OperationRef = fmt.Sprintf("xipw_%032x", index+1)
			if err := f.consumerStore.CreateReviewCaptureUROperation(f.ctx, candidate); err != nil {
				t.Fatal(err)
			}
			for _, state := range test.states {
				var result json.RawMessage
				if state == "pending" || state == "effective" || state == "drift" {
					result = json.RawMessage(`{"source":"synthetic-readback"}`)
				}
				if err := f.consumerStore.RecordReviewCaptureUROperation(f.ctx, installationID,
					candidate.OperationRef, state, result, now); err != nil {
					t.Fatalf("record %s: %v", state, err)
				}
				stored, err := f.consumerStore.ReadReviewCaptureUROperation(f.ctx, installationID, candidate.OperationRef)
				if err != nil || stored.State != state || stored.XIDesiredRevision != 1 {
					t.Fatalf("readback %s: %+v %v", state, stored, err)
				}
				wantAudit := "error"
				if state == "pending" || state == "effective" {
					wantAudit = "accepted"
				}
				var auditResult string
				err = f.admin.QueryRowContext(f.ctx, `SELECT result FROM access_audit_records
 WHERE action=$1 AND resource_kind='review_capture_ur_operation' AND resource_ref=$2`,
					"review.policy.ur."+state, candidate.OperationRef).Scan(&auditResult)
				if err != nil || auditResult != wantAudit {
					t.Fatalf("audit %s result=%s want=%s err=%v", state, auditResult, wantAudit, err)
				}
			}
		})
	}
}
