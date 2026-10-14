package db_test

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func TestWorkbenchRecentRequestsFrozenEconomicAndRuntime(t *testing.T) {
	f := newSourceFixture(t)
	a := f.documents[sourcefacts.ContextProfile]
	b := runtimeGolden(t)[0]
	b.SourceInstance, b.OrgID, b.ProjectID, b.RequestID, b.SubjectID = a.SourceInstance, a.OrgID, a.ProjectID, a.RequestID, a.RequestID
	b.OccurredAt, b.RecordedAt = a.OccurredAt, a.RecordedAt
	b.Kind = "request_started"
	runtime := *b.Runtime
	runtime.LocalDelivery = "completed"
	b.Runtime = &runtime
	putRuntimeDocuments(t, f, f.source, b)
	f.money()
	f.allow()
	f.exec(`UPDATE workbench_access_grants SET allow_source_runtime=true WHERE subject_id=$1`, f.subject)
	now := time.Now().UTC().Add(time.Minute)
	input := f.request()
	economic, err := f.workspace.CreateWorkbenchReading(f.ctx, f.subject, input, now)
	if err != nil {
		t.Fatal(err)
	}
	runtimeInput := workbench.RuntimeReadingRequest{ContractVersion: workbench.RuntimeContractVersion, MetricVersion: workbench.RuntimeMetricVersion, Period: input.Period, Filters: input.Filters}
	observed, err := f.workspace.CreateRuntimeReading(f.ctx, f.subject, runtimeInput, now)
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.workspace.GetWorkbenchRecentRequests(f.ctx, f.subject, economic.ID, observed.ID, 1, "", now)
	if err != nil || len(page.Items) != 1 || page.NextCursor != "" || page.ScopeBasis != "economic_reading_members" || page.Items[0].Runtime.MatchState != "matched" || page.Items[0].Runtime.Outcome != "local_delivery_completed" || page.Items[0].Runtime.RequestRef == "" {
		t.Fatalf("recent page %+v: %v", page, err)
	}
	if page.Items[0].GatewayChargedMicros == "" || page.Items[0].CalculatedReferenceMicros == "" || page.MembershipRef != economic.MembershipRef || page.RuntimeMembershipRef != observed.MembershipRef {
		t.Fatal("scope or money missing", page)
	}
	if page.Items[0].ApplicationState == "known" && page.Items[0].ApplicationID == "" {
		t.Fatal("known application lost its source ID", page.Items[0])
	}
	standalone, err := f.workspace.GetWorkbenchRecentRequests(f.ctx, f.subject, economic.ID, "", 1, "", now)
	if err != nil || len(standalone.Items) != 1 || standalone.Items[0].Runtime.MatchState != "unavailable" {
		t.Fatal("independent economic page", standalone, err)
	}
	if _, err = f.workspace.GetWorkbenchRecentRequests(f.ctx, "other-subject", economic.ID, observed.ID, 1, "", now); !errors.Is(err, workbench.ErrNotFound) {
		t.Fatal("cross-owner", err)
	}
	if _, err = f.workspace.GetWorkbenchRecentRequests(f.ctx, f.subject, economic.ID, observed.ID, 1, "bad-cursor", now); !errors.Is(err, workbench.ErrInvalid) {
		t.Fatal("cursor", err)
	}
	boundCursor := base64.RawURLEncoding.EncodeToString([]byte(economic.ID + "\x00overview.recent:" + observed.ID + "\x001"))
	if _, err = f.workspace.GetWorkbenchRecentRequests(f.ctx, f.subject, economic.ID, "", 1, boundCursor, now); !errors.Is(err, workbench.ErrInvalid) {
		t.Fatal("cross-runtime cursor", err)
	}
	shifted := runtimeInput
	newEnd := shifted.Period.EndExclusive.Add(time.Minute)
	shifted.Period.EndExclusive = &newEnd
	otherRuntime, createErr := f.workspace.CreateRuntimeReading(f.ctx, f.subject, shifted, now)
	if createErr != nil {
		t.Fatal(createErr)
	}
	if _, err = f.workspace.GetWorkbenchRecentRequests(f.ctx, f.subject, economic.ID, otherRuntime.ID, 1, "", now); !errors.Is(err, workbench.ErrInvalid) {
		t.Fatal("mismatched period", err)
	}
	filtered := runtimeInput
	filtered.Filters = workbench.Filters{SourceIDs: []string{f.source}}
	otherRuntime, createErr = f.workspace.CreateRuntimeReading(f.ctx, f.subject, filtered, now)
	if createErr != nil {
		t.Fatal(createErr)
	}
	if _, err = f.workspace.GetWorkbenchRecentRequests(f.ctx, f.subject, economic.ID, otherRuntime.ID, 1, "", now); !errors.Is(err, workbench.ErrInvalid) {
		t.Fatal("mismatched filters", err)
	}
	if _, err = f.workspace.GetWorkbenchRecentRequests(f.ctx, f.subject, economic.ID, observed.ID, 1, "", economic.ExpiresAt); !errors.Is(err, workbench.ErrExpired) {
		t.Fatal("expiry", err)
	}
	f.exec(`UPDATE workbench_access_grants SET allow_details=false WHERE subject_id=$1`, f.subject)
	if _, err = f.workspace.GetWorkbenchRecentRequests(f.ctx, f.subject, economic.ID, observed.ID, 1, "", now); !errors.Is(err, workbench.ErrAccessChanged) {
		t.Fatal("revocation", err)
	}
}
