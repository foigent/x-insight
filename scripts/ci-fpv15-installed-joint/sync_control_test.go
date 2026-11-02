package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connectorsync"
)

func TestInstalledCollectionIntentIsAnExplicitBoundedOwnerChoice(t *testing.T) {
	const id = "xci_4646464646464646464646"
	for _, tc := range []struct {
		name, wantError  string
		preserved        bool
		before           connectorsync.Status
		getCode, putCode int
		badResponse      bool
		wantCalls        int
	}{
		{name: "new manual owner selects Auto", wantCalls: 2},
		{name: "manual default missing", before: connectorsync.Status{Auto: true}, wantError: "collection_sync_manual_default_missing", wantCalls: 1},
		{name: "wrong connection", before: connectorsync.Status{InstallationID: "xci_0000000000000000000000"}, wantError: "collection_sync_prestate_invalid", wantCalls: 1},
		{name: "owner denied", getCode: 403, wantError: "collection_sync_status_failed", wantCalls: 1},
		{name: "choice conflicts without retry", putCode: 409, wantError: "collection_sync_choice_failed", wantCalls: 2},
		{name: "server did not retain choice", badResponse: true, wantError: "collection_sync_choice_not_retained", wantCalls: 2},
		{name: "existing Auto preserved", preserved: true, before: connectorsync.Status{Auto: true}, wantCalls: 1},
		{name: "migration must not disable Auto", preserved: true, wantError: "collection_sync_migration_changed_intent", wantCalls: 1},
		{name: "unbounded initial run rejected", before: connectorsync.Status{Run: &connectorsync.Run{Started: time.Unix(1, 0), Deadline: time.Unix(62, 0)}}, wantError: "collection_sync_initial_run_unbounded", wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &driver{installationID: id, adminCookie: &http.Cookie{Name: "__Host-xinsight_session", Value: "synthetic-owner"}, adminCSRF: "synthetic-csrf"}
			d.input.XIControlOrigin = "https://xi.synthetic.invalid:9443"
			calls := 0
			d.client = &http.Client{Transport: diagnosticTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != d.input.XIControlOrigin+"/api/v1/connector-control/installations/"+id+"/sync" {
					t.Fatal("fixture changed another connection or contacted UR")
				}
				cookie, err := r.Cookie(d.adminCookie.Name)
				if err != nil || cookie.Value != d.adminCookie.Value {
					t.Fatal("owner session missing")
				}
				code, result := 200, tc.before
				if r.Method == http.MethodGet {
					if calls != 1 {
						t.Fatal("fixture retried status")
					}
					if result.InstallationID == "" {
						result.InstallationID = id
					}
					result.Revision = 1
					if tc.getCode != 0 {
						code = tc.getCode
					}
				} else {
					if r.Method != http.MethodPut || calls != 2 || tc.preserved || r.Header.Get("Origin") != d.input.XIControlOrigin || r.Header.Get("X-CSRF-Token") != d.adminCSRF {
						t.Fatal("fixture bypassed the human mutation boundary")
					}
					var raw map[string]json.RawMessage
					if json.NewDecoder(r.Body).Decode(&raw) != nil || len(raw) != 4 {
						t.Fatal("unexpected owner choice fields")
					}
					wire, _ := json.Marshal(raw)
					var request connectorsync.Request
					if json.Unmarshal(wire, &request) != nil {
						t.Fatal("invalid owner request")
					}
					request.SchemaVersion, request.InstallationID, request.Action = connectorsync.LocalSchema, id, "set"
					if !request.Valid() || !request.Auto || request.Cadence == nil || *request.Cadence != 5 || request.ExpectedRevision != 1 {
						t.Fatal("fixture selected an unbounded or stale preference")
					}
					result = connectorsync.Status{InstallationID: id, Auto: !tc.badResponse, Cadence: request.Cadence, Revision: 2, RequestID: request.RequestID}
					if tc.putCode != 0 {
						code = tc.putCode
					}
				}
				body, _ := json.Marshal(result)
				return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
			})}
			err := d.collectionIntent(context.Background(), tc.preserved)
			if tc.wantError == "" && err != nil || tc.wantError != "" && (err == nil || err.Error() != tc.wantError) || calls != tc.wantCalls {
				t.Fatalf("result=%v calls=%d; want=%q calls=%d", err, calls, tc.wantError, tc.wantCalls)
			}
		})
	}
}

func TestInstalledUpgradePreservesExactPredecessorCollectionIntent(t *testing.T) {
	const id = "xci_4646464646464646464646"
	const request = "46464646-4646-4646-8646-000000000001"
	for _, name := range []string{"schema45 migration", "schema45 manual rejected", "schema46 choice", "changed installation", "changed Auto", "changed revision", "changed request", "changed cadence", "missing cadence", "missing prestate", "unknown schema", "owner denied"} {
		t.Run(name, func(t *testing.T) {
			cadence := 5
			want := connectorsync.Status{InstallationID: id, Auto: true, Cadence: &cadence, Revision: 2, RequestID: request}
			d := &driver{installationID: id, adminCookie: &http.Cookie{Name: "__Host-xinsight_session", Value: "synthetic-owner"}, adminCSRF: "synthetic-csrf", collectionChoice: &want}
			d.input.XIControlOrigin = "https://xi.synthetic.invalid:9443"
			schema, after, code, wantCalls := "46", want, 200, 1
			wantError := "collection_sync_migration_changed_intent"
			switch name {
			case "schema45 migration", "schema45 manual rejected":
				schema, after.Revision, after.Cadence, after.RequestID = "45", 1, nil, ""
				after.Auto = name == "schema45 migration"
			case "changed installation":
				after.InstallationID = "xci_0000000000000000000000"
			case "changed Auto":
				after.Auto = false
			case "changed revision":
				after.Revision++
			case "changed request":
				after.RequestID = "46464646-4646-4646-8646-000000000002"
			case "changed cadence":
				other := 60
				after.Cadence = &other
			case "missing cadence":
				after.Cadence = nil
			case "missing prestate":
				d.collectionChoice, wantCalls, wantError = nil, 0, "collection_sync_prestate_invalid"
			case "unknown schema":
				schema, wantCalls, wantError = "47", 0, "collection_sync_prestate_invalid"
			case "owner denied":
				code, wantError = 403, "collection_sync_status_failed"
			}
			if name == "schema45 migration" || name == "schema46 choice" {
				wantError = ""
			}
			calls := 0
			d.client = &http.Client{Transport: diagnosticTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || r.URL.String() != d.input.XIControlOrigin+"/api/v1/connector-control/installations/"+id+"/sync" {
					t.Fatal("upgrade check mutated preference or contacted another target")
				}
				if cookie, err := r.Cookie(d.adminCookie.Name); err != nil || cookie.Value != d.adminCookie.Value {
					t.Fatal("owner session missing")
				}
				body, _ := json.Marshal(after)
				return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
			})}
			err := d.upgradedCollectionIntent(context.Background(), schema)
			if wantError == "" && err != nil || wantError != "" && (err == nil || err.Error() != wantError) || calls != wantCalls {
				t.Fatalf("result=%v calls=%d; want=%s calls=%d", err, calls, wantError, wantCalls)
			}
		})
	}
}
