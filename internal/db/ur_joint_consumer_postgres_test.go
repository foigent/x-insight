package db_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/integration/reviewcopy"
	"github.com/foigent/x-insight/internal/integration/urmachine"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

type urJointConsumerInput struct {
	ManagedNormalSetup     bool                                         `json:"managed_normal_setup,omitempty"`
	ManagedFutureProjects  bool                                         `json:"managed_future_projects,omitempty"`
	ManagedMode            string                                       `json:"managed_mode"`
	XIControlOrigin        string                                       `json:"xi_control_origin"`
	ReceiverNetworkBinding connectorcontrol.FixedReceiverNetworkBinding `json:"receiver_network_binding"`
	ReceiverAllowedIP      string                                       `json:"receiver_allowed_ip"`
	Profiles               []string                                     `json:"profiles"`
	BaseURL                string                                       `json:"base_url"`
	TLSCAPEM               string                                       `json:"tls_ca_pem"`
	InstallationID         string                                       `json:"installation_id"`
	SourceInstance         string                                       `json:"source_instance"`
	OrgID                  string                                       `json:"org_id"`
	ProjectID              string                                       `json:"project_id"`
	PolicyBearer           string                                       `json:"policy_bearer"`
	RevokedPolicyBearer    string                                       `json:"revoked_policy_bearer"`
	EnrollmentBearer       string                                       `json:"enrollment_bearer"`
	ForeignProjectID       string                                       `json:"foreign_project_id"`
	SourceFactsBearer      string                                       `json:"source_facts_bearer"`
	SourceContentBearer    string                                       `json:"source_content_bearer"`
	PostgresDSN            string                                       `json:"postgres_dsn"`
	ExpectedContent        struct {
		RequestID  string `json:"request_id"`
		ContentRef string `json:"content_ref"`
		SHA256     string `json:"sha256"`
	} `json:"expected_content"`
}

// Run only as a compiled test binary owned by UR's one-time TLS/PG fixture.
// Stdin carries synthetic test credentials in memory; TestMain still guards
// the same exact XI base database through its recorded local scope.
func TestXIJointURConsumer(t *testing.T) {
	if os.Getenv("XINSIGHT_JOINT_UR_CONSUMER") != "1" {
		t.Skip("one-time UR joint fixture not selected")
	}
	var input urJointConsumerInput
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		t.Fatalf("joint fixture stdin is invalid: %v", err)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		t.Fatal("joint fixture stdin has trailing data")
	}
	endpoint, err := url.Parse(input.BaseURL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.Path != "" ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		t.Fatal("joint fixture requires one exact HTTPS origin")
	}
	host, _, err := net.SplitHostPort(endpoint.Host)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() ||
		input.PostgresDSN == "" || input.PostgresDSN != os.Getenv("XINSIGHT_POSTGRES_DSN") ||
		input.ProjectID == "" || input.ExpectedContent.RequestID == "" ||
		len(input.ExpectedContent.SHA256) != 64 {
		t.Fatal("joint fixture scope or expected content is incomplete")
	}
	if _, err = hex.DecodeString(input.ExpectedContent.SHA256); err != nil || strings.ToLower(input.ExpectedContent.SHA256) != input.ExpectedContent.SHA256 {
		t.Fatal("joint fixture expected digest is invalid")
	}
	expectedDigest := "sha256:" + input.ExpectedContent.SHA256
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(input.TLSCAPEM)) {
		t.Fatal("joint fixture TLS CA is invalid")
	}
	httpClient := &http.Client{Transport: &http.Transport{Proxy: nil,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}}, Timeout: 10 * time.Second}
	if input.ManagedMode != "" {
		runXIManagedJoint(t, input, pool, httpClient)
		return
	}
	identity := urmachine.Identity{InstallationID: input.InstallationID,
		SourceInstance: input.SourceInstance, OrgID: input.OrgID}
	policy, err := urmachine.NewPolicyClient(input.BaseURL, input.PolicyBearer, identity, httpClient)
	if err != nil {
		t.Fatalf("joint policy client: %v", err)
	}
	policyScopes, err := policy.PolicyScopes(t.Context(), "")
	if err != nil {
		t.Fatalf("joint policy discovery: %v", err)
	}
	policyProject := false
	for _, p := range policyScopes.Projects {
		if p.ProjectID == input.ProjectID {
			policyProject = true
		}
	}
	if !policyProject {
		t.Fatal("joint policy discovery omitted expected project")
	}
	if input.ForeignProjectID != "" {
		for _, project := range policyScopes.Projects {
			if project.ProjectID == input.ForeignProjectID {
				t.Fatal("foreign project leaked into policy discovery")
			}
		}
	}
	if input.RevokedPolicyBearer != "" {
		revoked, err := urmachine.NewPolicyClient(input.BaseURL, input.RevokedPolicyBearer, identity, httpClient)
		if err != nil {
			t.Fatalf("joint revoked policy client shape: %v", err)
		}
		if _, err = revoked.PolicyScopes(t.Context(), ""); !errors.Is(err, urmachine.ErrUnauthorized) {
			t.Fatalf("revoked policy authority was not rejected: %v", err)
		}
	}
	var policyGrant *urmachine.Grant
	for i := range policyScopes.Grants {
		grant := &policyScopes.Grants[i]
		if grant.TargetKind == "project_policy" && grant.ProjectID == input.ProjectID {
			policyGrant = grant
			break
		}
	}
	if policyGrant == nil {
		t.Fatal("joint policy discovery omitted exact project delegation")
	}
	beforePolicy, err := policy.ReadProjectPolicy(t.Context(), input.ProjectID, policyGrant.GrantID)
	if err != nil {
		t.Fatalf("joint scoped policy read: %v", err)
	}
	var projectLayer struct {
		PolicyRef string `json:"policy_ref"`
		Desired   *struct {
			PolicyVersion string          `json:"policy_version"`
			Config        json.RawMessage `json:"config"`
		} `json:"desired"`
		DesiredExecutionMode string `json:"desired_execution_storage_mode"`
		ProjectQuotaBytes    *int64 `json:"project_quota_bytes"`
	}
	if json.Unmarshal(beforePolicy.Project, &projectLayer) != nil || projectLayer.PolicyRef == "" ||
		projectLayer.Desired == nil || projectLayer.Desired.PolicyVersion == "" || len(projectLayer.Desired.Config) == 0 {
		t.Fatal("joint scoped policy lacks a project desired version")
	}
	storageMode := projectLayer.DesiredExecutionMode
	if storageMode == "" {
		storageMode = "temporary_allowed"
	}
	operationRef := "xipw_" + strings.ReplaceAll(testUUID(t), "-", "")
	policyBody := map[string]any{"schema_version": "unirouter.payload-policy.v2",
		"expected_policy_ref": projectLayer.PolicyRef, "expected_version": projectLayer.Desired.PolicyVersion,
		"expected_vector": beforePolicy.VersionVector, "desired": projectLayer.Desired.Config,
		"execution_storage_mode": storageMode}
	if projectLayer.ProjectQuotaBytes != nil {
		policyBody["project_quota_bytes"] = *projectLayer.ProjectQuotaBytes
	}
	policyWrite, err := json.Marshal(struct {
		SchemaVersion              string `json:"schema_version"`
		OperationRef               string `json:"operation_ref"`
		InstallationID             string `json:"installation_id"`
		SourceInstance             string `json:"source_instance"`
		DelegationID               string `json:"delegation_id"`
		ExpectedDelegationRevision string `json:"expected_delegation_revision"`
		XIOperator                 struct {
			Kind         string `json:"kind"`
			SubjectRef   string `json:"subject_ref"`
			XIRequestRef string `json:"xi_request_ref"`
		} `json:"xi_operator"`
		Policy any `json:"policy"`
	}{SchemaVersion: "unirouter.xinsight.policy-write.v1", OperationRef: operationRef,
		InstallationID: input.InstallationID, SourceInstance: input.SourceInstance,
		DelegationID: policyGrant.GrantID, ExpectedDelegationRevision: policyGrant.Revision,
		XIOperator: struct {
			Kind         string `json:"kind"`
			SubjectRef   string `json:"subject_ref"`
			XIRequestRef string `json:"xi_request_ref"`
		}{
			Kind: "human", SubjectRef: "xs_0011223344556677", XIRequestRef: "joint-" + operationRef},
		Policy: policyBody})
	if err != nil {
		t.Fatalf("joint policy request serialization: %v", err)
	}
	written, err := policy.WriteProjectPolicy(t.Context(), input.ProjectID, operationRef, policyWrite)
	if err != nil {
		t.Fatalf("joint policy write: %v", err)
	}
	readback, err := policy.ReadPolicyOperation(t.Context(), operationRef)
	if err != nil || readback.OperationRef != operationRef || readback.DesiredVersion != written.DesiredVersion ||
		readback.EffectiveVersion != written.EffectiveVersion || readback.ApplicationStatus != written.ApplicationStatus {
		t.Fatalf("joint policy operation readback differs: %+v %+v %v", written, readback, err)
	}
	if readback.ApplicationStatus == "applied" && readback.DesiredVersion != readback.EffectiveVersion {
		t.Fatal("joint policy marked applied with divergent desired and effective versions")
	}
	afterPolicy, err := policy.ReadProjectPolicy(t.Context(), input.ProjectID, policyGrant.GrantID)
	if err != nil {
		t.Fatalf("joint project policy scope readback: %v", err)
	}
	var afterLayer struct {
		Desired *struct {
			PolicyVersion string `json:"policy_version"`
		} `json:"desired"`
		Effective *struct {
			PolicyVersion string `json:"policy_version"`
		} `json:"effective"`
		ApplicationStatus string `json:"application_status"`
	}
	if json.Unmarshal(afterPolicy.Project, &afterLayer) != nil || afterLayer.Desired == nil ||
		afterLayer.Desired.PolicyVersion != readback.DesiredVersion ||
		(afterLayer.Effective == nil && readback.EffectiveVersion != "0") ||
		(afterLayer.Effective != nil && afterLayer.Effective.PolicyVersion != readback.EffectiveVersion) ||
		afterLayer.ApplicationStatus != readback.ApplicationStatus {
		t.Fatalf("joint project desired/effective readback differs from operation: %+v %+v", readback, afterLayer)
	}
	enroll, err := urmachine.NewEnrollmentClient(input.BaseURL, input.EnrollmentBearer, identity, httpClient)
	if err != nil {
		t.Fatalf("joint enrollment client: %v", err)
	}
	projects, err := enroll.Projects(t.Context(), "")
	if err != nil {
		t.Fatalf("joint enrollment discovery: %v", err)
	}
	var selected *urmachine.Enrollment
	for _, p := range projects.Projects {
		if p.ProjectID == input.ProjectID && len(p.Enrollments) == 1 {
			selected = &p.Enrollments[0]
		}
	}
	if selected == nil || !selected.IncludeContent || len(selected.Profiles) == 0 {
		t.Fatal("joint enrollment lacks one expected A/C scope")
	}
	factsRegistration := jointRegistration(t, input.SourceFactsBearer, input.SourceInstance,
		input.OrgID, input.ProjectID, selected.Profiles)
	contentRegistration := jointRegistration(t, input.SourceContentBearer, input.SourceInstance,
		input.OrgID, input.ProjectID, []string{sourcefacts.ContentProfile})
	var contentReceipt urmachine.RegistrationReceipt
	for _, registration := range []struct {
		kind string
		raw  []byte
	}{{"facts", factsRegistration}, {"content", contentRegistration}} {
		first, err := enroll.Register(t.Context(), input.ProjectID, selected.EnrollmentID, registration.kind, registration.raw)
		if err != nil || first.Replayed {
			t.Fatalf("joint %s first registration: %+v %v", registration.kind, first, err)
		}
		second, err := enroll.Register(t.Context(), input.ProjectID, selected.EnrollmentID, registration.kind, registration.raw)
		if err != nil || !second.Replayed || second.CredentialID != first.CredentialID || second.RegistrationHash != first.RegistrationHash {
			t.Fatalf("joint %s registration readback mismatch: %+v %+v %v", registration.kind, first, second, err)
		}
		if registration.kind == "content" {
			contentReceipt = second
		}
	}
	f := newSourceFixture(t)
	profile := ""
	for _, p := range selected.Profiles {
		if p == sourcefacts.ContextProfile {
			profile = p
			break
		}
	}
	if profile == "" {
		t.Fatal("joint enrollment omitted execution context A profile")
	}
	aReader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{
		Endpoint: input.BaseURL + sourcefacts.ManagedEndpointPath(profile), ServiceKey: input.SourceFactsBearer, TLSRootCAs: pool})
	if err != nil {
		t.Fatalf("joint A reader: %v", err)
	}
	aCapRaw, err := aReader.FetchSourceCapabilities(t.Context())
	if err != nil {
		t.Fatalf("joint A capabilities: %v", err)
	}
	aCap, err := sourcefacts.DecodeCapabilities(aCapRaw)
	if err != nil {
		t.Fatalf("joint A capabilities contract: %v", err)
	}
	stream := sourcefacts.Stream{ID: testUUID(t), SourceSystemID: f.source,
		SourceInstance: input.SourceInstance, OrgID: input.OrgID, ProjectID: input.ProjectID,
		Profile: profile, ScopeGeneration: aCap.ScopeGeneration}
	stream, err = f.consumerStore.RegisterSourceFactsStream(t.Context(), stream)
	if err != nil {
		t.Fatalf("joint A local registration: %v", err)
	}
	if err = sourcefacts.Preflight(t.Context(), aReader, stream); err != nil {
		t.Fatalf("joint A preflight: %v", err)
	}
	stream, err = f.consumerStore.ControlSourceFactsStream(t.Context(), stream.ID, "active", stream.Revision, false)
	if err != nil {
		t.Fatalf("joint A activation: %v", err)
	}
	aConsumer := sourcefacts.Consumer{Store: f.consumerStore, Reader: aReader, ExpectedStream: &stream}
	for i := 0; i < 3; i++ {
		if _, err = aConsumer.PullOnce(t.Context(), stream.ID); err != nil {
			t.Fatalf("joint A pull: %v", err)
		}
	}
	var aFacts int
	if err = f.admin.QueryRowContext(t.Context(), `SELECT count(*) FROM source_fact_documents WHERE
 source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND request_id=$5
 AND profile=$6`, f.source, input.SourceInstance, input.OrgID, input.ProjectID,
		input.ExpectedContent.RequestID, sourcefacts.ContextProfile).Scan(&aFacts); err != nil || aFacts == 0 {
		t.Fatalf("joint A request fact missing: count=%d error=%v", aFacts, err)
	}
	cReader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{
		Endpoint: input.BaseURL + sourcefacts.ContentEndpointPath, ServiceKey: input.SourceContentBearer, TLSRootCAs: pool})
	if err != nil {
		t.Fatalf("joint C reader: %v", err)
	}
	cCapRaw, err := cReader.FetchContentCapabilities(t.Context())
	if err != nil {
		t.Fatalf("joint C capabilities: %v", err)
	}
	cCap, err := sourcefacts.DecodeContentCapabilities(cCapRaw)
	if err != nil {
		t.Fatalf("joint C capabilities contract: %v", err)
	}
	if cCap.SourceInstance != input.SourceInstance || cCap.OrgID != input.OrgID || cCap.ProjectID != input.ProjectID {
		t.Fatal("joint C capabilities scope drift")
	}
	content := sourcefacts.ContentCollection{ID: testUUID(t), SourceSystemID: f.source,
		SourceInstance: input.SourceInstance, OrgID: input.OrgID, ProjectID: input.ProjectID,
		CredentialID: contentReceipt.CredentialID, RegistrationHash: contentReceipt.RegistrationHash,
		ContentScopeRevision: cCap.ContentScopeRevision, RecoveryEpoch: cCap.RecoveryEpoch,
		AllowedPurposes: []string{sourcefacts.ContentPurposeHumanReview}, CoverageFrom: cCap.CoverageFrom}
	content, err = f.consumerStore.RegisterContentCollection(t.Context(), content)
	if err != nil {
		t.Fatalf("joint C local registration: %v", err)
	}
	if _, err = sourcefacts.PreflightContent(t.Context(), cReader, content); err != nil {
		t.Fatalf("joint C preflight: %v", err)
	}
	content, err = f.consumerStore.ControlContentCollection(t.Context(), content.ID, "active", content.Revision, false, content)
	if err != nil {
		t.Fatalf("joint C activation: %v", err)
	}
	now := time.Now().UTC()
	revision, err := f.consumerStore.RequestReviewCaptureDefault(t.Context(), db.ReviewCaptureSettings{
		Mode: "full_content", RetentionSeconds: 3600, ProjectQuotaBytes: 1 << 20, MaxItemBytes: 1 << 20}, 0, "xs_admin", now)
	if err != nil {
		t.Fatalf("joint XI policy request: %v", err)
	}
	if err = f.consumerStore.ConfirmReviewCaptureDefault(t.Context(), revision, "xs_admin", now); err != nil {
		t.Fatalf("joint XI policy confirm: %v", err)
	}
	if err = f.consumerStore.BindReviewCaptureProject(t.Context(), f.source, input.SourceInstance,
		input.OrgID, input.ProjectID, testUUID(t), "Joint synthetic XI project", "xs_admin", now); err != nil {
		t.Fatalf("joint XI project binding: %v", err)
	}
	keys := reviewbody.KeyRing{ActiveID: "joint-synthetic", Keys: map[string][]byte{"joint-synthetic": bytes.Repeat([]byte{9}, 32)}}
	cConsumer := sourcefacts.ContentConsumer{Store: f.consumerStore, Reader: cReader, Expected: content,
		AfterCommit: func(_ctx context.Context, _c sourcefacts.ContentCollection, limits sourcefacts.ContentLimits) error {
			return reviewcopy.PollURCCopies(_ctx, cReader, f.workspace, content.ID, limits, keys, time.Now)
		}}
	for i := 0; i < 3; i++ {
		if _, err = cConsumer.PullOnce(t.Context()); err != nil {
			t.Fatalf("joint C pull: %v", err)
		}
	}
	var manifestContentRef string
	if err = f.admin.QueryRowContext(t.Context(), `SELECT source_json->>'content_ref' FROM source_content_manifests WHERE
 source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4
 AND source_json->>'request_id'=$5 AND source_json->'representation'->>'digest'=$6
 ORDER BY sequence DESC LIMIT 1`, f.source, input.SourceInstance, input.OrgID, input.ProjectID,
		input.ExpectedContent.RequestID, expectedDigest).Scan(&manifestContentRef); err != nil || manifestContentRef == "" {
		t.Fatalf("joint C manifest not discovered from request and digest: %v", err)
	}
	if input.ExpectedContent.ContentRef != "" && input.ExpectedContent.ContentRef != manifestContentRef {
		t.Fatal("joint C manifest content reference differs from optional expectation")
	}
	var state, digest string
	if err = f.admin.QueryRowContext(t.Context(), `SELECT state,digest FROM review_body_operations WHERE
 source_system_id=$1::uuid AND source_instance=$2 AND org_id=$3 AND project_id=$4 AND content_ref=$5`,
		f.source, input.SourceInstance, input.OrgID, input.ProjectID, manifestContentRef).Scan(&state, &digest); err != nil ||
		state != "accepted" || digest != expectedDigest {
		t.Fatalf("joint C encrypted custody missing: state=%q digest=%q error=%v", state, digest, err)
	}
	f.exec(`UPDATE workbench_access_grants SET allow_content=true,
 allowed_content_purposes='["human_review"]' WHERE subject_id=$1`, f.subject)
	read, err := f.workspace.ReadReviewBody(t.Context(), f.subject, manifestContentRef, keys, time.Now().UTC())
	if err != nil {
		t.Fatalf("joint XI independent review-body read: %v", err)
	}
	actualDigest := sha256.Sum256(read.Bytes)
	if hex.EncodeToString(actualDigest[:]) != input.ExpectedContent.SHA256 {
		t.Fatal("joint XI decrypted review-body bytes differ from UR captured payload")
	}
}

func jointRegistration(t *testing.T, bearer, instance, orgID, projectID string, profiles []string) []byte {
	t.Helper()
	if !strings.HasPrefix(bearer, "ur-xf1-") || len(bearer) < 20 || len(profiles) == 0 {
		t.Fatal("joint source credential has invalid shape")
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		t.Fatal(err)
	}
	verifier := sha256.Sum256([]byte("unirouter.xinsight.feed-credential.v1\x00" + bearer))
	registration := sourcefacts.SourceCredentialRegistration{
		SchemaVersion: sourcefacts.SourceCredentialRegistrationVersion, TokenFormat: "ur-xf1-hex256.v1",
		VerifierScheme: "receiver_sha256_v1", KeyPrefix: bearer[:20], VerifierSHA256: hex.EncodeToString(verifier[:]),
		OperationID: hex.EncodeToString(id[:]), SourceInstance: instance, OrgID: orgID, ProjectID: projectID, Profiles: profiles,
	}
	raw, err := json.Marshal(registration)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 64<<10 {
		t.Fatal("joint registration exceeds machine limit")
	}
	return raw
}
