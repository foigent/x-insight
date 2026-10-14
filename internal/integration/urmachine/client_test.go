package urmachine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPolicyClientBindsIdentityAndKeepsPendingDistinctFromEffective(t *testing.T) {
	const token = "ur-xpm1-synthetic-policy-only"
	const operation = "xipw_0123456789abcdef0123456789abcdef"
	base := prefix + "/payload-policy"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("missing purpose-specific machine bearer")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		switch r.URL.Path {
		case base + "/scopes":
			if r.URL.Query().Get("cursor") != "41" {
				t.Errorf("cursor=%q", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"schema_version":"unirouter.xinsight.policy-scopes.v1","installation_id":"xci_test","source_instance":"ur-a","org_id":"7","grants":[],"projects":[{"project_id":"42","display_name":"Test"}],"next_cursor":""}`))
		case base + "/projects/42":
			if r.Method == http.MethodGet {
				if r.URL.Query().Get("delegation_id") != "xipg_1" {
					t.Errorf("delegation=%q", r.URL.RawQuery)
				}
				_, _ = w.Write([]byte(`{"schema_version":"unirouter.payload-policy.v2","source_instance":"ur-a","org_id":"7","project_id":"42","enterprise":{"policy_ref":"content-policy:7:enterprise"},"version_vector":"enterprise:1","storage_status":"ready"}`))
				return
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"schema_version":"unirouter.xinsight.policy-write-result.v1","operation_ref":"` + operation + `","result":"committed","source_instance":"ur-a","policy_ref":"content-policy:7:42","desired_version":"2","effective_version":"1","application_status":"pending","audit_ref":"5","readback_path":"` + base + `/projects/42"}`))
		case base + "/operations/" + operation:
			_, _ = w.Write([]byte(`{"schema_version":"unirouter.xinsight.policy-write-result.v1","operation_ref":"` + operation + `","source_instance":"ur-a","result":"committed"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	c, err := NewPolicyClient(server.URL, token, Identity{InstallationID: "xci_test", SourceInstance: "ur-a", OrgID: "7"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	scopes, err := c.PolicyScopes(t.Context(), "41")
	if err != nil || len(scopes.Projects) != 1 || scopes.Projects[0].ProjectID != "42" {
		t.Fatalf("scopes=%+v err=%v", scopes, err)
	}
	view, err := c.ReadProjectPolicy(t.Context(), "42", "xipg_1")
	if err != nil || view.VersionVector != "enterprise:1" {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	envelope := json.RawMessage(`{"schema_version":"unirouter.xinsight.policy-write.v1","operation_ref":"` + operation + `","installation_id":"xci_test","source_instance":"ur-a"}`)
	result, err := c.WriteProjectPolicy(t.Context(), "42", operation, envelope)
	if err != nil || result.ApplicationStatus != "pending" || result.DesiredVersion == result.EffectiveVersion {
		t.Fatalf("pending write misread: %+v %v", result, err)
	}
	recovered, err := c.ReadPolicyOperation(t.Context(), operation)
	if err != nil || recovered.OperationRef != operation {
		t.Fatalf("operation=%+v err=%v", recovered, err)
	}
}

func TestPolicyClientRejectsScopeDriftRedirectAndUnconfirmedWrite(t *testing.T) {
	const operation = "xipw_0123456789abcdef0123456789abcdef"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			panic(http.ErrAbortHandler)
		}
		if r.URL.Path == prefix+"/payload-policy/scopes" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write([]byte(`{"schema_version":"unirouter.xinsight.policy-scopes.v1","installation_id":"other-installation","source_instance":"ur-a","org_id":"7","projects":[],"next_cursor":""}`))
			return
		}
		w.Header().Set("Location", "/different-authority")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	c, err := NewPolicyClient(server.URL, "ur-xpm1-synthetic-token", Identity{InstallationID: "xci_test", SourceInstance: "ur-a", OrgID: "7"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.PolicyScopes(context.Background(), ""); !errors.Is(err, ErrContract) {
		t.Fatalf("accepted drift: %v", err)
	}
	if _, err := c.ReadProjectPolicy(context.Background(), "42", "xipg_1"); !errors.Is(err, ErrContract) {
		t.Fatalf("followed redirect: %v", err)
	}
	envelope := json.RawMessage(`{"schema_version":"unirouter.xinsight.policy-write.v1","operation_ref":"` + operation + `","installation_id":"xci_test","source_instance":"ur-a"}`)
	if _, err := c.WriteProjectPolicy(context.Background(), "42", operation, envelope); !errors.Is(err, ErrUnconfirmed) || strings.Contains(err.Error(), "synthetic-token") {
		t.Fatalf("write transport outcome: %v", err)
	}
	if _, err := NewPolicyClient("http://example.invalid", "ur-xpm1-token", c.identity, server.Client()); !errors.Is(err, ErrContract) {
		t.Fatalf("accepted HTTP: %v", err)
	}
	if _, err := NewPolicyClient(server.URL, "ur-xse1-wrong-purpose", c.identity, server.Client()); !errors.Is(err, ErrContract) {
		t.Fatalf("accepted enrollment token for policy: %v", err)
	}
}

func TestPolicyWriteLostReceiptKeepsSameOperationForReadback(t *testing.T) {
	const operation = "xipw_0123456789abcdef0123456789abcdef"
	for _, failure := range []string{"gateway_502", "truncated_receipt"} {
		t.Run(failure, func(t *testing.T) {
			writes := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "no-store")
				if r.Method == http.MethodPut {
					writes++ // Simulate a committed write whose response cannot be verified.
					if failure == "gateway_502" {
						w.WriteHeader(http.StatusBadGateway)
						return
					}
					_, _ = w.Write([]byte(`{"schema_version":"unirouter.xinsight.policy-write-result.v1","operation_ref":`))
					return
				}
				if r.URL.Path != prefix+"/payload-policy/operations/"+operation {
					t.Errorf("readback path=%s", r.URL.Path)
				}
				_, _ = w.Write([]byte(`{"schema_version":"unirouter.xinsight.policy-write-result.v1","operation_ref":"` + operation + `","source_instance":"ur-a","result":"committed"}`))
			}))
			defer server.Close()
			c, err := NewPolicyClient(server.URL, "ur-xpm1-synthetic-token",
				Identity{InstallationID: "xci_test", SourceInstance: "ur-a", OrgID: "7"}, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			envelope := json.RawMessage(`{"schema_version":"unirouter.xinsight.policy-write.v1","operation_ref":"` + operation + `","installation_id":"xci_test","source_instance":"ur-a"}`)
			if _, err = c.WriteProjectPolicy(t.Context(), "42", operation, envelope); !errors.Is(err, ErrUnconfirmed) {
				t.Fatalf("committed lost-receipt write classified as %v", err)
			}
			readback, err := c.ReadPolicyOperation(t.Context(), operation)
			if err != nil || readback.OperationRef != operation || writes != 1 {
				t.Fatalf("same-operation readback: %+v %v writes=%d", readback, err, writes)
			}
		})
	}
}

func TestScopedPolicyClientUsesExactEnterpriseAndUserRoutes(t *testing.T) {
	const operation = "xipw_0123456789abcdef0123456789abcdef"
	base := prefix + "/payload-policy"
	var writes []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("Authorization") != "Bearer ur-xpm1-test" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case base + "/projects/42/users":
			if r.Method != http.MethodGet || r.URL.Query().Get("cursor") != "3" {
				t.Errorf("users request %s %s", r.Method, r.URL.String())
			}
			_, _ = w.Write([]byte(`{"schema_version":"unirouter.xinsight.policy-users.v1","project_id":"42","users":[{"user_id":"5","display_name":"Owner"}],"next_cursor":""}`))
		case base + "/organizations/7", base + "/projects/42/users/5":
			project, user, ref := "", "", "content-policy:7:enterprise"
			if r.URL.Path != base+"/organizations/7" {
				project, user, ref = "42", "5", "content-policy:7:42:5"
			}
			if r.Method == http.MethodGet {
				if r.URL.Query().Get("delegation_id") != "xipg_1" {
					t.Errorf("delegation %s", r.URL.String())
				}
				_, _ = w.Write([]byte(`{"schema_version":"unirouter.payload-policy.v2","source_instance":"ur-a","org_id":"7","project_id":"` + project + `","user_id":"` + user + `","enterprise":{"policy_ref":"content-policy:7:enterprise"},"user":{"policy_ref":"` + ref + `"},"version_vector":"{}","limits":{"max_saved_bytes":16777216},"policy_capabilities":{"supported_protocols":["responses"],"observation_points":["client_request"]}}`))
				return
			}
			writes = append(writes, r.URL.Path)
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"schema_version":"unirouter.xinsight.policy-write-result.v1","operation_ref":"` + operation + `","result":"committed","source_instance":"ur-a","policy_ref":"` + ref + `","desired_version":"1","effective_version":"","application_status":"pending","readback_path":"` + r.URL.Path + `"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	c, err := NewPolicyClient(server.URL, "ur-xpm1-test", Identity{InstallationID: "xci_test", SourceInstance: "ur-a", OrgID: "7"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	users, err := c.PolicyUsers(t.Context(), "42", "3")
	if err != nil || len(users.Users) != 1 || users.Users[0].UserID != "5" {
		t.Fatalf("users %+v %v", users, err)
	}
	enterprise, err := c.ReadEnterprisePolicy(t.Context(), "xipg_1")
	if err != nil || enterprise.ProjectID != "" || enterprise.UserID != "" || enterprise.Limits == nil ||
		enterprise.Limits.MaxSavedBytes != 16777216 || enterprise.PolicyCapabilities == nil ||
		len(enterprise.PolicyCapabilities.SupportedProtocols) != 1 {
		t.Fatalf("enterprise %+v %v", enterprise, err)
	}
	user, err := c.ReadUserPolicy(t.Context(), "42", "5", "xipg_1")
	if err != nil || user.ProjectID != "42" || user.UserID != "5" {
		t.Fatalf("user %+v %v", user, err)
	}
	envelope := json.RawMessage(`{"schema_version":"unirouter.xinsight.policy-write.v1","operation_ref":"` + operation + `","installation_id":"xci_test","source_instance":"ur-a"}`)
	if _, err = c.WriteEnterprisePolicy(t.Context(), operation, envelope); err != nil {
		t.Fatal(err)
	}
	if _, err = c.WriteUserPolicy(t.Context(), "42", "5", operation, envelope); err != nil {
		t.Fatal(err)
	}
	if len(writes) != 2 || writes[0] != base+"/organizations/7" || writes[1] != base+"/projects/42/users/5" {
		t.Fatalf("write routes %v", writes)
	}
}
