package connectorcontrol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func descriptorFixture(t *testing.T) (SourceDescriptorV1, ManagedBoundary, []byte) {
	t.Helper()
	raw, err := os.ReadFile("testdata/source-descriptor-v1/valid.json")
	if err != nil {
		t.Fatal(err)
	}
	v, err := DecodeSourceDescriptor(raw)
	if err != nil {
		t.Fatal(err)
	}
	b := ManagedBoundary{Installation: Installation{InstallationID: v.SourceBinding.InstallationID, DeploymentID: v.SourceBinding.DeploymentID, SourceInstance: v.SourceBinding.SourceInstance, ProjectID: "project-86", Status: "connected", ObservedState: "connected", DesiredState: "active", InstallationAcknowledgementSHA256: "sha256:" + strings.Repeat("a", 64)}, SourceBindingSHA256: v.SourceBindingSHA256, UniRouterOrigin: "https://ur.example"}
	for _, files := range [][2]string{{"01-prepare-paused-source-operation.json", "02-prepare-paused-source-result.json"}, {"03-register-receiver-operation.json", "04-register-receiver-result.json"}, {"05-resume-source-operation.json", "06-resume-source-result.json"}} {
		root := filepath.Join("..", "..", "contracts", "connector-control", "v2", "fixtures", "valid")
		raw, err := os.ReadFile(filepath.Join(root, files[0]))
		if err != nil {
			t.Fatal(err)
		}
		r, err := DecodeControlOperationRequestV2(raw)
		if err != nil {
			t.Fatal(err)
		}
		raw, err = os.ReadFile(filepath.Join(root, files[1]))
		if err != nil {
			t.Fatal(err)
		}
		result, err := DecodeControlResultV2(raw, time.Date(2030, 1, 1, 1, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		at := result.ObservedAt.Add(-time.Second)
		request := r.Request
		op := ControlOperation{SchemaVersion: r.SchemaVersion, OperationID: r.OperationID, InstallationID: r.InstallationID, ProjectID: r.ProjectID, EventScope: r.EventScope, DesiredState: r.DesiredState, Kind: r.Kind, Phase: r.Phase, PredecessorOperationID: r.PredecessorOperationID, PredecessorResultSHA256: r.PredecessorResultSHA256, Request: &request, Status: "completed", AvailableAt: at, CreatedAt: at, UpdatedAt: result.ObservedAt, CompletedAt: &result.ObservedAt}
		op.RequestSHA256, _, _ = CanonicalSHA256(r)
		op.ResultSHA256, _, _ = CanonicalSHA256(result)
		b.Installation.ProjectID = r.ProjectID
		b.Phases = append(b.Phases, ActivationPhase{Operation: op, Result: &result})
	}
	return v, b, raw
}
func TestSourceDescriptorFrozenGoldenAndOriginalV2Proof(t *testing.T) {
	v, b, _ := descriptorFixture(t)
	raw, err := os.ReadFile("testdata/source-descriptor-v1/valid.json")
	if err != nil {
		t.Fatal(err)
	}
	if RawSHA256(string(raw)) != "sha256:b75f590075c0098de400f0980bb8313027eb65632acf7e8a62364af6f7a1b264" {
		t.Fatal("golden drift")
	}
	_, canonical, err := CanonicalSHA256(v)
	if err != nil || !bytes.Equal(bytes.TrimSpace(raw), canonical) {
		t.Fatal("typed canonical mismatch", err)
	}
	if err := ValidateSourceDescriptorProof(b, v); err != nil {
		t.Fatal("original v2 binding", err)
	}
	b.Descriptor = &v
	if _, ok := b.VerifiedSource(); !ok || b.Bootstrap != nil {
		t.Fatal("new install became adoption")
	}
	for _, mutate := range []func(*ManagedBoundary){func(b *ManagedBoundary) { b.Installation.DeploymentID = "xid_AAAAAAAAAAAAAAAAAAAAAA" }, func(b *ManagedBoundary) { b.SourceBindingSHA256 = "sha256:" + strings.Repeat("f", 64) }, func(b *ManagedBoundary) { b.Installation.Status = "paused" }, func(b *ManagedBoundary) { b.Installation.SourceInstance = "other" }} {
		bad := b
		mutate(&bad)
		if ValidateSourceDescriptorProof(bad, v) == nil {
			t.Fatal("changed accepted identity was trusted")
		}
	}
}
func TestSourceDescriptorStrictShapeAndIndependentMachineIPQualification(t *testing.T) {
	v, _, _ := descriptorFixture(t)
	_, raw, err := CanonicalSHA256(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{string(raw) + "{}", strings.Replace(string(raw), `"org_id":2`, `"org_id":2.0`, 1), strings.Replace(string(raw), `"org_id":2`, `"org_id":2e0`, 1), strings.Replace(string(raw), `"org_id":2`, `"org_id":9223372036854775808`, 1), strings.Replace(string(raw), `"org_id":2`, `"org_id":null`, 1), strings.Replace(string(raw), `"org_id":2`, `"org_id":2,"org_id":2`, 1), strings.Replace(string(raw), `"org_id":2`, `"Org_id":2`, 1), strings.Replace(string(raw), `"org_id":2,`, "", 1), strings.Replace(string(raw), `"org_id":2`, `"org_id":"2"`, 1), strings.Replace(string(raw), `"schema_version":`, `"unknown":null,"schema_version":`, 1)} {
		if _, err := DecodeSourceDescriptor([]byte(bad)); err == nil {
			t.Fatal("invalid descriptor accepted", bad)
		}
	}
	for _, ip := range []string{"0.0.0.0/32", "::/128", "::ffff:192.0.2.10/128", "224.0.0.1/32"} {
		copy := v
		copy.ReceiverNetwork.AllowedIP = ip
		copy.ReceiverNetworkSHA256, _, _ = CanonicalSHA256(copy.ReceiverNetwork)
		wire, _ := json.Marshal(copy)
		if _, err := DecodeSourceDescriptor(wire); err != nil {
			t.Fatal("r1 address shape restricted", err)
		}
		if v3IP(ip) {
			t.Fatal("descriptor bypassed independent machine qualification")
		}
	}
	bad := v
	bad.ReceiverNetwork.FeedKeyID = "9223372036854775808"
	bad.ReceiverNetworkSHA256, _, _ = CanonicalSHA256(bad.ReceiverNetwork)
	if ValidateSourceDescriptor(bad) == nil {
		t.Fatal("feed key int64 overflow")
	}
}
