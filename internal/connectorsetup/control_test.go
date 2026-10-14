package connectorsetup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

type setupStoreStub struct {
	Store
	policy        db.ReviewCaptureDefault
	policyErr     error
	record        db.ConnectorSetupRecord
	recordErr     error
	receiving     connectorcontrol.SetupReceiving
	pending       []string
	prepare       func()
	recordedCode  string
	recordCalls   int
	applied       bool
	originalReads int
}

func (s *setupStoreStub) ReadReviewCaptureDefault(context.Context) (db.ReviewCaptureDefault, error) {
	return s.policy, s.policyErr
}
func (s *setupStoreStub) ReadConnectorSetup(context.Context, string) (db.ConnectorSetupRecord, error) {
	return s.record, s.recordErr
}
func (s *setupStoreStub) ReadConnectorSetupReceiving(context.Context, string, time.Time) (connectorcontrol.SetupReceiving, error) {
	return s.receiving, nil
}
func (s *setupStoreStub) ListPendingConnectorSetups(context.Context) ([]string, error) {
	return s.pending, nil
}
func (s *setupStoreStub) PrepareConnectorSetupPolicy(context.Context, string, string, time.Time) (int64, error) {
	if s.prepare != nil {
		s.prepare()
	}
	return s.policy.DesiredRevision, nil
}
func (s *setupStoreStub) RecordConnectorSetupResult(_ context.Context, _ string, applied bool, code string) error {
	s.recordCalls++
	s.recordedCode = code
	s.applied = applied
	return nil
}

type setupHostStub struct {
	Host
	loadedErr       error
	revoked         bool
	loads           int
	original        sourcefacts.ManagedConfig
	originalPresent bool
	originalErr     error
}

func (h *setupHostStub) AuthorizeSetupActor(string, bool) error {
	if h.revoked {
		return connectorcontrol.ErrSetupUnauthorized
	}
	return nil
}
func (h *setupHostStub) ReviewBodyLoaded(context.Context) error       { return h.loadedErr }
func (h *setupHostStub) EnsureReviewBodyLoaded(context.Context) error { h.loads++; return h.loadedErr }
func (h *setupHostStub) WithSetupInvitationSeed(context.Context, string, string, connectorcontrol.SetupConsentPut, bool, func(hostctl.SetupInvitationSeed, io.Reader) error) error {
	panic("status must not mint invitation")
}
func setupFixture() (*Manager, *setupStoreStub, *setupHostStub) {
	now := time.Now().UTC()
	settings := db.ReviewCaptureSettings{Mode: "full_content", RetentionSeconds: 90 * 86400, ProjectQuotaBytes: 5 << 30, MaxItemBytes: 32 << 20}
	s := &setupStoreStub{policy: db.ReviewCaptureDefault{DesiredRevision: 1, EffectiveRevision: 1, Effective: &settings}, record: db.ConnectorSetupRecord{Actor: "xs_1111111111111111", Installation: connectorcontrol.Installation{InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA", DeploymentID: "xid_AAAAAAAAAAAAAAAAAAAAAA", DesiredState: "active", Status: "connected", InstallationAcknowledgementSHA256: "sha256:ack", CreatedAt: now}, Consent: connectorcontrol.SetupConsentView{ConsentRevision: 1, Custody: *custody(settings), ExpiresAt: now.Add(time.Minute)}, PolicyRevision: 1, AppliedAt: &now}, receiving: connectorcontrol.SetupReceiving{ProfilesReceived: []string{}}}
	h := &setupHostStub{}
	return &Manager{Store: s, Host: h, Now: func() time.Time { return now }}, s, h
}

type setupDiagnosticSQLState string

func (e setupDiagnosticSQLState) Error() string    { return "private SQL parameter" }
func (e setupDiagnosticSQLState) SQLState() string { return string(e) }

func TestSetupStatusFailureLogsOnlyFixedCategories(t *testing.T) {
	for _, tc := range []struct {
		name, phase, category, sqlstate string
		err                             error
	}{
		{"record unknown", "read_record", "internal", "none", errors.New("private record path")},
		{"record missing", "read_record", "not_found", "none", errors.Join(connectorcontrol.ErrNotFound, errors.New("private subject"))},
		{"record SQL conflict", "read_record", "database_sqlstate", "40001", setupDiagnosticSQLState("40001")},
		{"policy SQL permission", "read_policy", "database_sqlstate", "42501", setupDiagnosticSQLState("42501")},
		{"policy unknown SQL", "read_policy", "internal", "none", setupDiagnosticSQLState("XX000")},
		{"policy context", "read_policy", "context_deadline", "none", context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, s, _ := setupFixture()
			if tc.phase == "read_record" {
				s.recordErr = tc.err
			} else {
				s.policyErr = tc.err
			}
			var logs bytes.Buffer
			old := log.Writer()
			log.SetOutput(&logs)
			defer log.SetOutput(old)
			_, err := m.ExecuteSetup(t.Context(), connectorcontrol.SetupLocalRequest{SchemaVersion: connectorcontrol.LocalSetupRequestSchema, Action: "status", ActorSubject: s.record.Actor, InstallationID: s.record.Installation.InstallationID})
			if !errors.Is(err, tc.err) {
				t.Fatalf("original error changed: %v", err)
			}
			want := "connector setup status action=status phase=" + tc.phase + " category=" + tc.category + " sqlstate=" + tc.sqlstate
			if !strings.Contains(logs.String(), want) || strings.Count(logs.String(), "connector setup status") != 1 {
				t.Fatalf("missing fixed status diagnostic: %q", logs.String())
			}
			for _, private := range []string{"private", s.record.Actor, s.record.Installation.InstallationID, "XX000"} {
				if strings.Contains(logs.String(), private) {
					t.Fatalf("status diagnostic exposed private error: %q", logs.String())
				}
			}
		})
	}
}
func TestAppliedSetupProjectsCurrentAuthorizedDisableAndReenable(t *testing.T) {
	m, s, h := setupFixture()
	stamp := m.now()
	s.receiving.LastSuccessAt = &stamp
	read := func(want string) {
		t.Helper()
		out, err := m.ExecuteSetup(t.Context(), connectorcontrol.SetupLocalRequest{SchemaVersion: connectorcontrol.LocalSetupRequestSchema, Action: "status", ActorSubject: s.record.Actor, InstallationID: s.record.Installation.InstallationID})
		if err != nil || out.Status.State != want {
			t.Fatalf("status %v %v; want %s", out.Status, err, want)
		}
		for _, i := range out.Status.Issues {
			if i.Code == "custody_revision_changed" {
				t.Fatal("initial consent became permanent policy freeze")
			}
		}
	}
	read("ready")
	disabled := *s.policy.Effective
	disabled.Mode = "disabled"
	s.policy = db.ReviewCaptureDefault{DesiredRevision: 2, EffectiveRevision: 2, Effective: &disabled}
	h.loadedErr = errors.New("no running body consumer")
	read("ready")
	enabled := disabled
	enabled.Mode = "full_content"
	s.policy = db.ReviewCaptureDefault{DesiredRevision: 3, EffectiveRevision: 3, Effective: &enabled}
	read("provisioning")
	h.loadedErr = nil
	read("ready")
	s.policy.DesiredRevision = 4
	read("provisioning")
	if h.loads != 0 {
		t.Fatal("read-only status mutated runtime")
	}
}
func TestSetupTickRechecksRevokedActorBeforeEffectiveConfirmation(t *testing.T) {
	m, s, h := setupFixture()
	s.record.AppliedAt = nil
	s.policy.EffectiveRevision = 0
	s.pending = []string{s.record.Installation.InstallationID}
	s.prepare = func() { h.revoked = true }
	// Policy.Store is deliberately absent: reaching confirmation would panic/fail.
	if err := m.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s.applied || s.recordedCode != "xi_actor_permission_changed" || h.loads != 0 {
		t.Fatal("revoked actor continued", s.recordedCode, h.loads)
	}
}
func TestSetupConsentDoesNotClaimAppliedBeforeRedeemOrReceiving(t *testing.T) {
	m, s, _ := setupFixture()
	s.record.Installation.InstallationAcknowledgementSHA256 = ""
	s.record.AppliedAt = nil
	out, err := m.status(t.Context(), s.record)
	if err != nil || out.State != "waiting_consent" {
		t.Fatal(out, err)
	}
	s.record.Installation.InstallationAcknowledgementSHA256 = "sha256:ack"
	out, err = m.status(t.Context(), s.record)
	if err != nil || out.State != "provisioning" {
		t.Fatal(out, err)
	}
	at := m.now()
	s.record.AppliedAt = &at
	out, err = m.status(t.Context(), s.record)
	if err != nil || out.State != "waiting_data" {
		t.Fatal(out, err)
	}
}

func TestControlIdentityFailurePrecedesCustodyAndStopsSetupMutation(t *testing.T) {
	for _, issue := range []string{"control_identity_expired", "control_identity_revoked", "control_identity_unavailable"} {
		t.Run(issue, func(t *testing.T) {
			m, s, h := setupFixture()
			s.record.ControlIdentityIssue = issue
			s.record.ErrorCode = "custody_loading"
			s.record.AppliedAt = nil
			s.pending = []string{s.record.Installation.InstallationID}
			s.prepare = func() { t.Fatal("expired or revoked control authority prepared policy") }
			out, err := m.ExecuteSetup(t.Context(), connectorcontrol.SetupLocalRequest{SchemaVersion: connectorcontrol.LocalSetupRequestSchema, Action: "status", ActorSubject: s.record.Actor, InstallationID: s.record.Installation.InstallationID})
			if err != nil || out.Status == nil || out.Status.State != "needs_action" || len(out.Status.Issues) != 1 || out.Status.Issues[0].Code != issue {
				t.Fatalf("control issue was hidden by custody: %+v, %v", out.Status, err)
			}
			if err := m.Tick(t.Context()); err != nil || s.applied || s.recordCalls != 0 || h.loads != 0 {
				t.Fatalf("control issue continued setup: applied=%t writes=%d loads=%d err=%v", s.applied, s.recordCalls, h.loads, err)
			}
		})
	}
}

func (h *setupHostStub) ReadOriginalSourceConfig(context.Context) (sourcefacts.ManagedConfig, bool, error) {
	return h.original, h.originalPresent, h.originalErr
}
func (s *setupStoreStub) ReadConnectorSetupReceivingWithOriginalConfig(context.Context, string, time.Time, sourcefacts.ManagedConfig) (connectorcontrol.SetupReceiving, error) {
	s.originalReads++
	return s.receiving, nil
}

func TestSetupStatusUsesRootQualifiedOriginalSnapshotAndFailsClosed(t *testing.T) {
	m, s, h := setupFixture()
	now := m.now()
	s.receiving.LastSuccessAt = &now
	h.originalPresent = true
	request := connectorcontrol.SetupLocalRequest{SchemaVersion: connectorcontrol.LocalSetupRequestSchema, Action: "status", ActorSubject: s.record.Actor, InstallationID: s.record.Installation.InstallationID}
	out, err := m.ExecuteSetup(t.Context(), request)
	if err != nil || out.Status.State != "ready" || s.originalReads != 1 || h.loads != 0 {
		t.Fatal("original status did not use read-only qualified path", err)
	}
	h.originalErr = errors.New("root config no longer qualified")
	if _, err = m.ExecuteSetup(t.Context(), request); !errors.Is(err, h.originalErr) || s.originalReads != 1 {
		t.Fatal("root qualification error fell back to guessed IDs", err)
	}
}
