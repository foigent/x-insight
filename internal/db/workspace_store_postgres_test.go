package db_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/discovery"
	"github.com/foigent/x-insight/internal/evidence"
	"github.com/foigent/x-insight/internal/executionid"
	"github.com/foigent/x-insight/internal/investigation"
	"github.com/foigent/x-insight/internal/workspace"
)

func TestWorkspaceStorePersistsCanonicalCasePackAgentAndDiscoveryState(t *testing.T) {
	baseDSN := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if baseDSN == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dsn := workspaceTestDSN(t, ctx, baseDSN)
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatalf("run production migrations: %v", err)
	}

	evidenceStore, err := db.OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatalf("open Evidence Store: %v", err)
	}
	defer evidenceStore.Close()
	sourceSystemID := testUUID(t)
	adapterManifestID := testUUID(t)
	requestID := "req-workspace-" + sourceSystemID[:8]
	snapshot, err := unirouter.LoadSnapshot(filepath.Join(
		"..", "..", "fixtures", "unirouter_snapshot", "missing_trace_gap.json",
	))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	snapshot.SourceInstance = "workspace-test-" + sourceSystemID[:8]
	snapshot.RequestIDs = []string{requestID}
	for index := range snapshot.Scenarios {
		snapshot.Scenarios[index].RequestID = requestID
	}
	for index := range snapshot.CallLogs {
		snapshot.CallLogs[index].RequestID = requestID
		snapshot.CallLogs[index].SourceRef = ""
	}
	for index := range snapshot.TraceEvents {
		snapshot.TraceEvents[index].RequestID = requestID
		snapshot.TraceEvents[index].SourceRef = ""
	}
	for index := range snapshot.CaptureGaps {
		snapshot.CaptureGaps[index].RequestID = requestID
	}
	events, err := unirouter.BuildSnapshotEvents(snapshot, unirouter.SnapshotImportOptions{
		SourceSystemID:    sourceSystemID,
		AdapterManifestID: adapterManifestID,
	})
	if err != nil {
		t.Fatalf("build fixture events: %v", err)
	}
	for _, event := range events {
		hash, hashErr := evidence.GenerateEventHash(event)
		if hashErr != nil {
			t.Fatalf("hash fixture event: %v", hashErr)
		}
		event["event_hash"] = hash
	}
	report, err := evidenceStore.PersistEvents(ctx, events)
	if err != nil || report.Inserted != 2 || report.CaptureGapsInserted != 1 {
		t.Fatalf("persist fixture report=%+v err=%v", report, err)
	}
	queryDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open verification connection: %v", err)
	}
	defer queryDB.Close()

	var eventID, eventHash, sourceRef string
	if err := queryDB.QueryRowContext(ctx, `
SELECT id::text, event_hash, source_ref
FROM evidence_events
WHERE source_system_id = $1::uuid AND request_id = $2
ORDER BY id
LIMIT 1`, sourceSystemID, requestID).Scan(&eventID, &eventHash, &sourceRef); err != nil {
		t.Fatalf("read persisted event identity: %v", err)
	}
	var gapID, affectedClaim, gapType string
	if err := queryDB.QueryRowContext(ctx, `
SELECT id::text, affected_claim, gap_type
FROM capture_gaps
WHERE source_system_id = $1::uuid AND request_id = $2
ORDER BY id
LIMIT 1`, sourceSystemID, requestID).Scan(&gapID, &affectedClaim, &gapType); err != nil {
		t.Fatalf("read persisted Gap identity: %v", err)
	}
	executionID, err := executionid.New(sourceSystemID, requestID)
	if err != nil {
		t.Fatalf("derive execution identity: %v", err)
	}

	store, err := db.OpenWorkspaceStore(ctx, dsn)
	if err != nil {
		t.Fatalf("open Workspace Store: %v", err)
	}
	defer store.Close()
	actor, err := workspace.HumanActor("xs_abcdefghijklmnop")
	if err != nil {
		t.Fatal(err)
	}
	replayActor, err := workspace.HumanActor("xs_qrstuvwxyzabcdef")
	if err != nil {
		t.Fatal(err)
	}
	baseTime := time.Date(2026, 7, 31, 1, 2, 3, 456789000, time.UTC)
	investigationCase, err := store.CreateCase(
		ctx,
		actor,
		workspace.CreateCaseInput{
			Title:       "Fallback evidence review",
			Description: "User-authored material remains explicit in the sealed pack.",
		},
		baseTime,
		deterministicWorkspaceReader(1),
	)
	if err != nil {
		t.Fatalf("create Case: %v", err)
	}
	executionLink, err := store.AddCaseExecution(
		ctx,
		investigationCase.ID,
		actor,
		workspace.AddExecutionInput{ExecutionID: executionID, AsOf: baseTime.Add(-time.Minute)},
		baseTime.Add(time.Second),
	)
	if err != nil {
		t.Fatalf("link execution: %v", err)
	}
	versionAfterExecution := mustCaseRecord(t, ctx, store, investigationCase.ID).Case.Version
	replayedExecution, err := store.AddCaseExecution(
		ctx,
		investigationCase.ID,
		replayActor,
		workspace.AddExecutionInput{ExecutionID: executionID, AsOf: baseTime.Add(-time.Minute)},
		baseTime.Add(10*time.Second),
	)
	if err != nil {
		t.Fatalf("replay execution link: %v", err)
	}
	if replayedExecution.AddedBySubject != executionLink.AddedBySubject ||
		!replayedExecution.AddedAt.Equal(executionLink.AddedAt) ||
		mustCaseRecord(t, ctx, store, investigationCase.ID).Case.Version != versionAfterExecution {
		t.Fatalf("execution no-op fabricated mutable provenance: first=%+v replay=%+v", executionLink, replayedExecution)
	}

	finding, err := store.CreateFinding(
		ctx,
		investigationCase.ID,
		actor,
		workspace.CreateFindingInput{
			Title:          "Fallback recovered the request",
			Statement:      "The selected evidence event supports a bounded recovery finding.",
			Classification: "evidence_backed",
		},
		baseTime.Add(2*time.Second),
		deterministicWorkspaceReader(2),
	)
	if err != nil {
		t.Fatalf("create finding: %v", err)
	}
	secondFinding, err := store.CreateFinding(
		ctx,
		investigationCase.ID,
		actor,
		workspace.CreateFindingInput{
			Title:          "Alternative explanation",
			Statement:      "A distinct Finding can cite the same immutable Evidence independently.",
			Classification: "hypothesis",
		},
		baseTime.Add(2500*time.Millisecond),
		deterministicWorkspaceReader(8),
	)
	if err != nil {
		t.Fatalf("create second finding: %v", err)
	}
	evidenceInput := workspace.AddEvidenceInput{
		FindingID:       finding.ID,
		EvidenceEventID: eventID,
		Relation:        "supports",
		EventHash:       eventHash,
		SourceRef:       sourceRef,
	}
	evidenceLink, err := store.AddEvidenceLink(
		ctx, investigationCase.ID, actor, evidenceInput, baseTime.Add(3*time.Second),
	)
	if err != nil {
		t.Fatalf("link Evidence: %v", err)
	}
	versionAfterEvidence := mustCaseRecord(t, ctx, store, investigationCase.ID).Case.Version
	replayedEvidence, err := store.AddEvidenceLink(
		ctx, investigationCase.ID, replayActor, evidenceInput, baseTime.Add(11*time.Second),
	)
	if err != nil {
		t.Fatalf("replay Evidence link: %v", err)
	}
	if replayedEvidence.AddedBySubject != evidenceLink.AddedBySubject ||
		!replayedEvidence.AddedAt.Equal(evidenceLink.AddedAt) ||
		mustCaseRecord(t, ctx, store, investigationCase.ID).Case.Version != versionAfterEvidence {
		t.Fatalf("Evidence no-op fabricated mutable provenance: first=%+v replay=%+v", evidenceLink, replayedEvidence)
	}
	secondEvidenceInput := evidenceInput
	secondEvidenceInput.FindingID = secondFinding.ID
	secondEvidence, err := store.AddEvidenceLink(
		ctx, investigationCase.ID, actor, secondEvidenceInput, baseTime.Add(11500*time.Millisecond),
	)
	if err != nil || secondEvidence.FindingID != secondFinding.ID {
		t.Fatalf("link shared Evidence to second Finding=%+v err=%v", secondEvidence, err)
	}
	versionAfterSecondEvidence := mustCaseRecord(t, ctx, store, investigationCase.ID).Case.Version
	if versionAfterSecondEvidence != versionAfterEvidence+1 {
		t.Fatal("distinct Evidence-to-Finding link did not advance Case revision once")
	}

	gapInput := workspace.AddGapInput{
		FindingID:     finding.ID,
		GapID:         gapID,
		Relation:      "limits",
		AffectedClaim: affectedClaim,
		GapType:       gapType,
	}
	gapLink, err := store.AddGapLink(
		ctx, investigationCase.ID, actor, gapInput, baseTime.Add(4*time.Second),
	)
	if err != nil {
		t.Fatalf("link Gap: %v", err)
	}
	versionAfterGap := mustCaseRecord(t, ctx, store, investigationCase.ID).Case.Version
	replayedGap, err := store.AddGapLink(
		ctx, investigationCase.ID, replayActor, gapInput, baseTime.Add(12*time.Second),
	)
	if err != nil {
		t.Fatalf("replay Gap link: %v", err)
	}
	if replayedGap.AddedBySubject != gapLink.AddedBySubject ||
		!replayedGap.AddedAt.Equal(gapLink.AddedAt) ||
		mustCaseRecord(t, ctx, store, investigationCase.ID).Case.Version != versionAfterGap {
		t.Fatalf("Gap no-op fabricated mutable provenance: first=%+v replay=%+v", gapLink, replayedGap)
	}
	secondGapInput := gapInput
	secondGapInput.FindingID = secondFinding.ID
	secondGap, err := store.AddGapLink(
		ctx, investigationCase.ID, actor, secondGapInput, baseTime.Add(12500*time.Millisecond),
	)
	if err != nil || secondGap.FindingID != secondFinding.ID {
		t.Fatalf("link shared Gap to second Finding=%+v err=%v", secondGap, err)
	}
	if mustCaseRecord(t, ctx, store, investigationCase.ID).Case.Version != versionAfterGap+1 {
		t.Fatal("distinct Gap-to-Finding link did not advance Case revision once")
	}
	resolution, err := store.ResolveGap(
		ctx,
		actor,
		gapID,
		workspace.ResolveGapInput{State: "investigating", Note: "Verify upstream span export."},
		baseTime.Add(5*time.Second),
	)
	if err != nil || resolution.Version != 1 || resolution.UpdatedAt.Location() != time.UTC {
		t.Fatalf("resolve Gap=%+v err=%v", resolution, err)
	}
	resolution, err = store.ResolveGap(
		ctx,
		actor,
		gapID,
		workspace.ResolveGapInput{State: "accepted_limitation", Note: "Source export was not enabled."},
		baseTime.Add(6*time.Second),
	)
	if err != nil || resolution.Version != 2 {
		t.Fatalf("update Gap resolution=%+v err=%v", resolution, err)
	}
	versionBeforeInvalidAnnotation := mustCaseRecord(t, ctx, store, investigationCase.ID).Case.Version
	if _, err := store.CreateAnnotation(
		ctx,
		investigationCase.ID,
		actor,
		workspace.CreateAnnotationInput{
			TargetKind: "finding",
			TargetRef:  "xf_AAAAAAAAAAAAAAAAAAAAAA",
			Body:       "must not attach to an absent Finding",
		},
		baseTime.Add(6500*time.Millisecond),
		deterministicWorkspaceReader(11),
	); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("annotation accepted an absent Case target: %v", err)
	}
	if mustCaseRecord(t, ctx, store, investigationCase.ID).Case.Version != versionBeforeInvalidAnnotation {
		t.Fatal("rejected annotation mutated Case revision")
	}
	if _, err := store.CreateAnnotation(
		ctx,
		investigationCase.ID,
		actor,
		workspace.CreateAnnotationInput{
			TargetKind: "finding",
			TargetRef:  finding.ID,
			Body:       "Review note intentionally remains user-authored Case content.",
		},
		baseTime.Add(7*time.Second),
		deterministicWorkspaceReader(3),
	); err != nil {
		t.Fatalf("create annotation: %v", err)
	}
	savedView, err := store.CreateSavedView(
		ctx,
		actor,
		workspace.CreateSavedViewInput{
			Name:         "Fallbacks with gaps",
			Filters:      []byte(`{"has_gaps":true,"outcome":"success"}`),
			Presentation: "causal_graph",
		},
		baseTime.Add(8*time.Second),
		deterministicWorkspaceReader(4),
	)
	if err != nil || savedView.CreatedAt.Location() != time.UTC {
		t.Fatalf("create Saved View=%+v err=%v", savedView, err)
	}
	if _, err := store.CreateSavedView(
		ctx,
		actor,
		workspace.CreateSavedViewInput{
			Name:         "Fallbacks with gaps",
			Filters:      []byte(`{"has_gaps":false}`),
			Presentation: "inbox",
		},
		baseTime.Add(8100*time.Millisecond),
		deterministicWorkspaceReader(10),
	); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("duplicate Saved View name did not conflict: %v", err)
	}
	views, err := store.ListSavedViews(ctx, actor.Ref, 10)
	if err != nil || len(views) != 1 || views[0].ID != savedView.ID ||
		views[0].UpdatedAt.Location() != time.UTC {
		t.Fatalf("list Saved Views=%+v err=%v", views, err)
	}
	alertID := "xal_" + strings.Repeat("a", 64)
	if _, err := queryDB.ExecContext(ctx, `
INSERT INTO investigation_alerts (
  id, source_system_id, source_alert_ref, title, severity, state, execution_id, observed_at
) VALUES ($1, $2::uuid, $3, $4, 'warning', 'open', $5, $6)`,
		alertID, sourceSystemID, "alert-workspace-1", "Observed provider failure",
		executionID, baseTime.Add(8*time.Second)); err != nil {
		t.Fatalf("insert source-owned Alert: %v", err)
	}
	versionBeforeAlert := mustCaseRecord(t, ctx, store, investigationCase.ID).Case.Version
	linkedAt := baseTime.Add(8500 * time.Millisecond)
	alertLink, err := store.LinkAlertToCase(ctx, actor, alertID, investigationCase.ID, linkedAt)
	if err != nil {
		t.Fatalf("link source Alert: %v", err)
	}
	if alertLink.AlertState != "open" || alertLink.LinkedBySubject != actor.Ref ||
		!alertLink.LinkedAt.Equal(linkedAt) ||
		mustCaseRecord(t, ctx, store, investigationCase.ID).Case.Version != versionBeforeAlert+1 {
		t.Fatalf("Alert link lost source/workspace boundary: %+v", alertLink)
	}
	replayedAlert, err := store.LinkAlertToCase(
		ctx, replayActor, alertID, investigationCase.ID, baseTime.Add(8600*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("replay Alert link: %v", err)
	}
	if replayedAlert.LinkedBySubject != alertLink.LinkedBySubject ||
		!replayedAlert.LinkedAt.Equal(alertLink.LinkedAt) ||
		mustCaseRecord(t, ctx, store, investigationCase.ID).Case.Version != versionBeforeAlert+1 {
		t.Fatalf("Alert no-op fabricated mutable provenance: first=%+v replay=%+v", alertLink, replayedAlert)
	}
	otherCase, err := store.CreateCase(
		ctx,
		actor,
		workspace.CreateCaseInput{Title: "Other alert target", Description: "Conflict target."},
		baseTime.Add(8700*time.Millisecond),
		deterministicWorkspaceReader(9),
	)
	if err != nil {
		t.Fatalf("create second Case: %v", err)
	}
	if _, err := store.LinkAlertToCase(
		ctx, actor, alertID, otherCase.ID, baseTime.Add(8800*time.Millisecond),
	); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("Alert relink to another Case did not conflict: %v", err)
	}
	alerts, err := evidenceStore.ListInvestigationAlerts(ctx, baseTime.Add(9*time.Second), 10)
	if err != nil || len(alerts) != 1 || alerts[0].CaseID != investigationCase.ID || alerts[0].State != "open" {
		t.Fatalf("listed Alerts lost source state or product link: %+v err=%v", alerts, err)
	}

	preSeal := mustCaseRecord(t, ctx, store, investigationCase.ID)
	if len(preSeal.Alerts) != 1 || preSeal.Alerts[0] != alertLink {
		t.Fatalf("Case did not retain persisted Alert link: %+v", preSeal.Alerts)
	}
	sealedAt := baseTime.Add(9 * time.Second)
	revision := preSeal.Case.Version + 1
	_, expectedPack, expectedDigest, err := workspace.BuildEvidencePack(
		preSeal, sealedAt, actor.Ref, revision,
	)
	if err != nil {
		t.Fatalf("build expected Evidence Pack: %v", err)
	}
	sealed, err := store.SealCase(ctx, investigationCase.ID, actor, sealedAt)
	if err != nil {
		t.Fatalf("seal Case: %v", err)
	}
	if sealed.Status != "sealed" ||
		sealed.SealedDigest != expectedDigest ||
		!bytes.Equal(sealed.SealedPack, expectedPack) ||
		sealed.CreatedAt.Location() != time.UTC ||
		sealed.UpdatedAt.Location() != time.UTC {
		t.Fatalf("sealed Case does not retain canonical bytes: %+v", sealed)
	}
	digest := sha256.Sum256(sealed.SealedPack)
	if "sha256:"+hex.EncodeToString(digest[:]) != sealed.SealedDigest {
		t.Fatalf("stored Evidence Pack digest does not match exact bytes")
	}
	reloaded := mustCaseRecord(t, ctx, store, investigationCase.ID)
	if !bytes.Equal(reloaded.Case.SealedPack, expectedPack) ||
		reloaded.Executions[0].AddedAt.Location() != time.UTC ||
		reloaded.Findings[0].CreatedAt.Location() != time.UTC ||
		reloaded.Evidence[0].AddedAt.Location() != time.UTC ||
		reloaded.Gaps[0].AddedAt.Location() != time.UTC ||
		reloaded.Alerts[0].LinkedAt.Location() != time.UTC ||
		reloaded.Annotations[0].CreatedAt.Location() != time.UTC {
		t.Fatalf("reloaded Case lost byte or UTC canonicalization: %+v", reloaded)
	}
	if _, err := store.CreateAnnotation(
		ctx,
		investigationCase.ID,
		actor,
		workspace.CreateAnnotationInput{TargetKind: "case", TargetRef: investigationCase.ID, Body: "late"},
		baseTime.Add(13*time.Second),
		deterministicWorkspaceReader(6),
	); !errors.Is(err, workspace.ErrSealed) {
		t.Fatalf("sealed Case accepted mutation: %v", err)
	}

	credential, err := store.CreateServiceIdentity(
		ctx,
		workspace.Actor{Kind: "root", Ref: "root"},
		workspace.CreateServiceIdentityInput{
			DisplayName: "Evidence reviewer",
			ExpiresAt:   baseTime.Add(24 * time.Hour),
		},
		baseTime,
		deterministicWorkspaceReader(5),
	)
	if err != nil {
		t.Fatalf("create service identity: %v", err)
	}
	authentication, err := store.AuthenticateServiceToken(ctx, credential.Token, baseTime.Add(time.Hour))
	if err != nil ||
		authentication.Identity.ID != credential.ServiceIdentity.ID ||
		authentication.Identity.CreatedAt.Location() != time.UTC {
		t.Fatalf("authenticate service identity=%+v err=%v", authentication, err)
	}
	if _, err := store.RevokeServiceIdentity(
		ctx,
		credential.ServiceIdentity.ID,
		actor,
		"review access complete",
		baseTime.Add(2*time.Hour),
	); err != nil {
		t.Fatalf("revoke service identity: %v", err)
	}
	if _, err := store.AuthenticateServiceToken(
		ctx, credential.Token, baseTime.Add(3*time.Hour),
	); !errors.Is(err, workspace.ErrCredentialRevoked) {
		t.Fatalf("revoked credential authentication error=%v", err)
	}

	analysisAsOf := baseTime.Add(4 * time.Hour)
	run, err := discovery.BuildRun(ctx, emptyDiscoveryEvidenceReader{}, analysisAsOf, 20, 20)
	if err != nil {
		t.Fatalf("build empty discovery run: %v", err)
	}
	if err := store.SaveDiscoveryRun(ctx, run); err != nil {
		t.Fatalf("save discovery run: %v", err)
	}
	if err := store.SaveDiscoveryRun(ctx, run); err != nil {
		t.Fatalf("idempotently replay discovery run: %v", err)
	}
	hypotheses, err := evidenceStore.ListDiscoveryHypotheses(ctx, analysisAsOf, 10)
	if err != nil || len(hypotheses) != 6 {
		t.Fatalf("list discovery hypotheses=%+v err=%v", hypotheses, err)
	}
	for _, hypothesis := range hypotheses {
		if hypothesis.AnalysisRunID != run.ID ||
			hypothesis.Status != "abstained" ||
			hypothesis.SampleSize != 0 ||
			hypothesis.InputDigest != run.InputDigest || hypothesis.SupportScoreDecimal != "" {
			t.Fatalf("hypothesis is not bound to the deterministic abstention run: %+v", hypothesis)
		}
	}
	t.Run("assessment_history_and_continuation", func(t *testing.T) {
		testAssessmentStoreHistory(t, ctx, store, queryDB, actor, baseTime.Add(5*time.Hour), workspace.BasisSelection{
			Evidence: []workspace.EvidenceChoice{{Scope: "finding", EvidenceEventID: eventID, Relation: "supports", EventHash: eventHash, SourceRef: sourceRef}},
			Gaps:     []workspace.GapChoice{{Scope: "finding", GapID: gapID, Relation: "limits", AffectedClaim: affectedClaim, GapType: gapType}},
		})
	})
}

func workspaceTestDSN(t *testing.T, ctx context.Context, baseDSN string) string {
	t.Helper()
	dsn := migrationTestDSN(t, ctx, baseDSN)
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse workspace test DSN: %v", err)
	}
	query := parsed.Query()
	options := strings.TrimSpace(query.Get("options") + " -cTimeZone=Asia/Shanghai")
	query.Set("options", options)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func deterministicWorkspaceReader(value byte) *bytes.Reader {
	return bytes.NewReader(bytes.Repeat([]byte{value}, 96))
}

func mustCaseRecord(
	t *testing.T,
	ctx context.Context,
	store *db.WorkspaceStore,
	caseID string,
) workspace.CaseRecord {
	t.Helper()
	record, err := store.GetCase(ctx, caseID)
	if err != nil {
		t.Fatalf("read Case %s: %v", caseID, err)
	}
	return record
}

type emptyDiscoveryEvidenceReader struct{}

func (emptyDiscoveryEvidenceReader) SearchExecutions(
	context.Context,
	investigation.SearchQuery,
) ([]investigation.ExecutionSummary, error) {
	return []investigation.ExecutionSummary{}, nil
}

func (emptyDiscoveryEvidenceReader) ListExecutionEvents(
	context.Context,
	string,
	time.Time,
	int,
) ([]investigation.EvidenceEventDetail, int, error) {
	return []investigation.EvidenceEventDetail{}, 0, nil
}

func (emptyDiscoveryEvidenceReader) ListExecutionGaps(
	context.Context,
	investigation.GapQuery,
) ([]investigation.CaptureGap, int, error) {
	return []investigation.CaptureGap{}, 0, nil
}
