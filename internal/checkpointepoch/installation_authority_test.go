package checkpointepoch

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

const (
	testInstallationA = "xci_AAAAAAAAAAAAAAAAAAAAAA"
	testInstallationB = "xci_BBBBBBBBBBBBBBBBBBBBBB"
)

func TestInstallationEpochAuthorityDerivesFirstAndHistoricalEpoch(t *testing.T) {
	input := testInstallationEpochInput(testInstallationA, "source.installation.a")
	installedAt := time.Date(2026, 8, 11, 8, 0, 0, 123456000, time.UTC)

	t.Run("first epoch and exact replay", func(t *testing.T) {
		port := &fakeInstallationEpochPort{boundary: testInstallationBoundary(input.Binding)}
		authority, err := ApplyInstallationEpoch(context.Background(), port, input, installedAt)
		if err != nil {
			t.Fatal(err)
		}
		if authority.Epoch != 1 || authority.Replayed || port.commitCalls != 1 ||
			port.commit.Epoch != 1 || port.commit.InstallationID != input.InstallationID ||
			port.commit.PrepareInput.AuthorizationRef != authority.AuthorizationRef ||
			!sha256Pattern.MatchString(port.commit.RequestSHA256) ||
			!sha256Pattern.MatchString(authority.BoundarySHA256) ||
			!sha256Pattern.MatchString(authority.CommitSHA256) ||
			!sha256Pattern.MatchString(authority.AuthoritySHA256) {
			t.Fatalf("authority=%+v commit=%+v calls=%d", authority, port.commit, port.commitCalls)
		}

		activeRecord := testInstallationRecord(
			InstallationEpochRecordActive,
			"state-a",
			1,
			authority.AuthorizationRef,
			input.SourceInstance,
			input.Cursor,
		)
		replayPort := &fakeInstallationEpochPort{boundary: InstallationEpochBoundary{
			SchemaVersion: InstallationEpochBoundarySchema,
			Binding:       input.Binding,
			Records:       []InstallationEpochRecord{activeRecord},
			Active:        activeSummaryFromRecord(activeRecord),
		}}
		replay, err := ApplyInstallationEpoch(context.Background(), replayPort, input, installedAt.Add(time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if !replay.Replayed || replay.Epoch != 1 || replay.CommitSHA256 != "" || replayPort.commitCalls != 0 {
			t.Fatalf("replay=%+v calls=%d", replay, replayPort.commitCalls)
		}
	})

	t.Run("legacy and installation history contribute to max floor", func(t *testing.T) {
		legacy := testInstallationRecord(
			InstallationEpochRecordHistory,
			"legacy-history",
			3,
			"decision://legacy-checkpoint-bootstrap",
			"legacy.source",
			"legacy-cursor",
		)
		priorRef, err := InstallationAuthorizationRef(testInstallationB)
		if err != nil {
			t.Fatal(err)
		}
		operation := testInstallationRecord(
			InstallationEpochRecordOperation,
			"prior-operation",
			5,
			priorRef,
			"source.installation.b",
			"prior-operation-cursor",
		)
		operation.Status = StatusAborted
		port := &fakeInstallationEpochPort{boundary: InstallationEpochBoundary{
			SchemaVersion: InstallationEpochBoundarySchema,
			Binding:       input.Binding,
			Records:       []InstallationEpochRecord{operation, legacy},
		}}
		authority, err := ApplyInstallationEpoch(context.Background(), port, input, installedAt)
		if err != nil {
			t.Fatal(err)
		}
		if authority.Epoch != 6 || port.commit.Epoch != 6 {
			t.Fatalf("authority=%+v commit=%+v", authority, port.commit)
		}
	})
}

func TestInstallationEpochAuthorityFailsClosedOnIdentityHistoryAndOverflow(t *testing.T) {
	input := testInstallationEpochInput(testInstallationA, "source.installation.a")
	installedAt := time.Date(2026, 8, 11, 8, 0, 0, 0, time.UTC)
	authorizationA, err := InstallationAuthorizationRef(testInstallationA)
	if err != nil {
		t.Fatal(err)
	}
	authorizationB, err := InstallationAuthorizationRef(testInstallationB)
	if err != nil {
		t.Fatal(err)
	}
	base := testInstallationRecord(
		InstallationEpochRecordHistory,
		"history-a",
		4,
		authorizationB,
		"source.installation.b",
		"history-cursor",
	)

	tests := []struct {
		name     string
		boundary InstallationEpochBoundary
	}{
		{
			name: "installation identity reuse",
			boundary: boundaryWithRecords(input.Binding, func() []InstallationEpochRecord {
				record := base
				record.AuthorizationRef = authorizationA
				return []InstallationEpochRecord{record}
			}()),
		},
		{
			name: "legacy Source identity reuse",
			boundary: boundaryWithRecords(input.Binding, func() []InstallationEpochRecord {
				record := base
				record.AuthorizationRef = "decision://legacy"
				record.SourceInstance = input.SourceInstance
				return []InstallationEpochRecord{record}
			}()),
		},
		{
			name: "Source identity case collision",
			boundary: boundaryWithRecords(input.Binding, func() []InstallationEpochRecord {
				record := base
				record.SourceInstance = strings.ToUpper(input.SourceInstance)
				return []InstallationEpochRecord{record}
			}()),
		},
		{
			name: "installation identity case collision",
			boundary: boundaryWithRecords(input.Binding, func() []InstallationEpochRecord {
				record := base
				record.AuthorizationRef = "xinsight://connector-installations/" + strings.ToLower(testInstallationA)
				return []InstallationEpochRecord{record}
			}()),
		},
		{
			name: "cross installation Source conflict in durable history",
			boundary: boundaryWithRecords(input.Binding, []InstallationEpochRecord{
				base,
				testInstallationRecord(
					InstallationEpochRecordHistory,
					"history-cross-install",
					5,
					authorizationA,
					base.SourceInstance,
					"other-cursor",
				),
			}),
		},
		{
			name: "one installation changes Source identity in history",
			boundary: boundaryWithRecords(input.Binding, []InstallationEpochRecord{
				base,
				testInstallationRecord(
					InstallationEpochRecordHistory,
					"history-same-install",
					5,
					authorizationB,
					"different.source.for.b",
					"other-cursor",
				),
			}),
		},
		{
			name: "one installation changes checkpoint epoch in history",
			boundary: boundaryWithRecords(input.Binding, []InstallationEpochRecord{
				base,
				testInstallationRecord(
					InstallationEpochRecordHistory,
					"history-same-install-new-epoch",
					base.Epoch+1,
					authorizationB,
					base.SourceInstance,
					"other-cursor",
				),
			}),
		},
		{
			name: "legacy and installation authority share Source",
			boundary: boundaryWithRecords(input.Binding, []InstallationEpochRecord{
				base,
				testInstallationRecord(
					InstallationEpochRecordHistory,
					"legacy-history-same-source",
					base.Epoch+1,
					"decision://legacy-checkpoint-bootstrap",
					base.SourceInstance,
					"legacy-cursor",
				),
			}),
		},
		{
			name: "conflicting durable epoch identity",
			boundary: boundaryWithRecords(input.Binding, []InstallationEpochRecord{
				base,
				testInstallationRecord(
					InstallationEpochRecordHistory,
					"history-conflicting-epoch",
					base.Epoch,
					authorizationA,
					"different.source",
					"other-cursor",
				),
			}),
		},
		{
			name: "malformed installation authorization",
			boundary: boundaryWithRecords(input.Binding, func() []InstallationEpochRecord {
				record := base
				record.AuthorizationRef = "xinsight://connector-installations/not-an-installation"
				return []InstallationEpochRecord{record}
			}()),
		},
		{
			name: "epoch overflow",
			boundary: boundaryWithRecords(input.Binding, func() []InstallationEpochRecord {
				record := base
				record.Epoch = math.MaxInt64
				return []InstallationEpochRecord{record}
			}()),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			port := &fakeInstallationEpochPort{boundary: test.boundary}
			if _, err := ApplyInstallationEpoch(context.Background(), port, input, installedAt); !errors.Is(err, ErrConflict) {
				t.Fatalf("error=%v", err)
			}
			if port.commitCalls != 0 {
				t.Fatal("conflicting boundary reached commit")
			}
		})
	}
}

func TestInstallationEpochAuthorityRejectsActiveAndCommitDrift(t *testing.T) {
	input := testInstallationEpochInput(testInstallationA, "source.installation.a")
	installedAt := time.Date(2026, 8, 11, 8, 0, 0, 0, time.UTC)
	authorization, err := InstallationAuthorizationRef(input.InstallationID)
	if err != nil {
		t.Fatal(err)
	}
	activeRecord := testInstallationRecord(
		InstallationEpochRecordActive,
		"state-a",
		1,
		authorization,
		"different.source",
		input.Cursor,
	)
	activePort := &fakeInstallationEpochPort{boundary: InstallationEpochBoundary{
		SchemaVersion: InstallationEpochBoundarySchema,
		Binding:       input.Binding,
		Records:       []InstallationEpochRecord{activeRecord},
		Active:        activeSummaryFromRecord(activeRecord),
	}}
	if _, err := ApplyInstallationEpoch(context.Background(), activePort, input, installedAt); !errors.Is(err, ErrConflict) {
		t.Fatalf("active conflict error=%v", err)
	}

	commitPort := &fakeInstallationEpochPort{
		boundary:     testInstallationBoundary(input.Binding),
		tamperCommit: true,
	}
	if _, err := ApplyInstallationEpoch(context.Background(), commitPort, input, installedAt); !errors.Is(err, ErrConflict) {
		t.Fatalf("commit drift error=%v", err)
	}
	if commitPort.commitCalls != 1 {
		t.Fatalf("commit calls=%d", commitPort.commitCalls)
	}

	expectedErr := errors.New("compare-and-set boundary changed")
	casPort := &fakeInstallationEpochPort{
		boundary:  testInstallationBoundary(input.Binding),
		commitErr: expectedErr,
	}
	if _, err := ApplyInstallationEpoch(context.Background(), casPort, input, installedAt); !errors.Is(err, expectedErr) {
		t.Fatalf("CAS error=%v", err)
	}
}

type fakeInstallationEpochPort struct {
	boundary     InstallationEpochBoundary
	commit       InstallationEpochCommit
	commitAt     time.Time
	commitCalls  int
	commitErr    error
	tamperCommit bool
}

func (port *fakeInstallationEpochPort) InspectInstallationEpochBoundary(
	_ context.Context,
	_ Binding,
) (InstallationEpochBoundary, error) {
	return port.boundary, nil
}

func (port *fakeInstallationEpochPort) CommitInstallationEpoch(
	_ context.Context,
	commit InstallationEpochCommit,
	installedAt time.Time,
) (ActiveStateSummary, error) {
	port.commitCalls++
	port.commit = commit
	port.commitAt = installedAt
	if port.commitErr != nil {
		return ActiveStateSummary{}, port.commitErr
	}
	active := ActiveStateSummary{
		StateID:             "state-committed",
		Binding:             commit.Binding,
		Epoch:               commit.Epoch,
		SourceInstance:      commit.NextSourceInstance,
		CursorSHA256:        hashText(commit.NextCursor),
		HighWatermarkSHA256: hashText(commit.NextHighWatermark),
		WorkerStatus:        "inactive",
		StateSHA256:         "sha256:" + strings.Repeat("a", 64),
	}
	if port.tamperCommit {
		active.Epoch++
	}
	return active, nil
}

func testInstallationEpochInput(installationID string, sourceInstance string) InstallationEpochInput {
	return InstallationEpochInput{
		Binding: Binding{
			SourceSystemID:    "11111111-1111-4111-8111-111111111111",
			AdapterManifestID: "22222222-2222-4222-8222-222222222222",
			CursorKey:         CompletedExecutionCursorKey,
		},
		InstallationID: installationID,
		WorkerUnit:     CompletedExecutionWorkerUnit,
		SourceInstance: sourceInstance,
		Cursor:         "empty-cursor-for-" + sourceInstance,
		HighWatermark:  "empty-cursor-for-" + sourceInstance,
	}
}

func testInstallationBoundary(binding Binding) InstallationEpochBoundary {
	return InstallationEpochBoundary{
		SchemaVersion: InstallationEpochBoundarySchema,
		Binding:       binding,
		Records:       []InstallationEpochRecord{},
	}
}

func boundaryWithRecords(binding Binding, records []InstallationEpochRecord) InstallationEpochBoundary {
	return InstallationEpochBoundary{
		SchemaVersion: InstallationEpochBoundarySchema,
		Binding:       binding,
		Records:       records,
	}
}

func testInstallationRecord(
	kind string,
	recordID string,
	epoch int64,
	authorizationRef string,
	sourceInstance string,
	cursor string,
) InstallationEpochRecord {
	return InstallationEpochRecord{
		RecordID: recordID,
		Kind:     kind,
		Binding: Binding{
			SourceSystemID:    "11111111-1111-4111-8111-111111111111",
			AdapterManifestID: "22222222-2222-4222-8222-222222222222",
			CursorKey:         CompletedExecutionCursorKey,
		},
		Epoch:               epoch,
		AuthorizationRef:    authorizationRef,
		SourceInstance:      sourceInstance,
		CursorSHA256:        hashText(cursor),
		HighWatermarkSHA256: hashText(cursor),
	}
}

func activeSummaryFromRecord(record InstallationEpochRecord) *ActiveStateSummary {
	return &ActiveStateSummary{
		StateID:             record.RecordID,
		Binding:             record.Binding,
		Epoch:               record.Epoch,
		SourceInstance:      record.SourceInstance,
		CursorSHA256:        record.CursorSHA256,
		HighWatermarkSHA256: record.HighWatermarkSHA256,
		WorkerStatus:        "inactive",
		StateSHA256:         "sha256:" + strings.Repeat("b", 64),
	}
}
