package urmachine

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

func TestEnrollmentDiscoveryAndRegistrationStayPurposeAndScopeBound(t *testing.T) {
	registration := json.RawMessage(`{"schema_version":"unirouter.xinsight.source-credential-registration.v1","token_format":"ur-xf1-hex256.v1","verifier_scheme":"receiver_sha256_v1","key_prefix":"ur-xf1-0000000000001","verifier_sha256":"d9c4bb0d61dc341aba6b3bec7c2dfbede896504f0ec64d362c55803a4bb41f4d","operation_id":"00000000000000000000000000000001","source_instance":"ur-a","org_id":"7","project_id":"42","profiles":["unirouter.execution-context.v1"]}`)
	registrationHash := sourcefacts.Hash(registration)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ur-xse1-synthetic-enrollment" {
			t.Errorf("wrong machine purpose")
			w.WriteHeader(403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		switch r.URL.Path {
		case prefix + "/source-enrollments/projects":
			_, _ = w.Write([]byte(`{"schema_version":"unirouter.xinsight.source-enrollment-projects.v1","installation_id":"xci_test","source_instance":"ur-a","org_id":"7","projects":[{"project_id":"42","display_name":"Test","enrollments":[{"enrollment_id":"xise_2","revision":"1","profiles":["unirouter.execution-context.v1"],"include_content":true,"facts_status":"not_requested","content_status":"requires_migration"}]}],"next_cursor":""}`))
		case prefix + "/source-enrollments/projects/42/registrations":
			var request struct {
				EnrollmentID string `json:"enrollment_id"`
				Kind         string `json:"kind"`
				Registration struct {
					ProjectID string `json:"project_id"`
				} `json:"registration"`
			}
			if json.NewDecoder(r.Body).Decode(&request) != nil || request.EnrollmentID != "xise_2" || request.Kind != "facts" || request.Registration.ProjectID != "42" {
				t.Errorf("registration scope drift")
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"enrollment_id":"xise_2","kind":"facts","credential_id":"314","status":"active","registration_hash":"` + registrationHash + `"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	identity := Identity{InstallationID: "xci_test", SourceInstance: "ur-a", OrgID: "7"}
	if _, err := NewEnrollmentClient(server.URL, "ur-xpm1-policy-token", identity, server.Client()); !errors.Is(err, ErrContract) {
		t.Fatalf("accepted policy bearer: %v", err)
	}
	c, err := NewEnrollmentClient(server.URL, "ur-xse1-synthetic-enrollment", identity, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	projects, err := c.Projects(t.Context(), "")
	if err != nil || len(projects.Projects) != 1 || projects.Projects[0].Enrollments[0].ContentStatus != "requires_migration" {
		t.Fatalf("discovery=%+v err=%v", projects, err)
	}
	receipt, err := c.Register(t.Context(), "42", "xise_2", "facts", registration)
	if err != nil || receipt.CredentialID != "314" {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	if _, err := c.Register(t.Context(), "43", "xise_2", "facts", registration); !errors.Is(err, ErrContract) {
		t.Fatalf("accepted project mismatch: %v", err)
	}
}
