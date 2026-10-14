package hostctl

import (
	"context"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"os"
	"path/filepath"
	"testing"
)

func TestMachinePreparationIndependentCustodyAndRecoverableCandidate(t *testing.T) {
	e, _ := managedCustodyFixture(t)
	id := "xci_AAAAAAAAAAAAAAAAAAAAAA"
	a, err := e.PrepareMachineCredential(context.Background(), id, connectorcontrol.PolicyMachinePurpose, "source", "2", "192.0.2.10/32")
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.PrepareMachineCredential(context.Background(), id, connectorcontrol.EnrollmentMachinePurpose, "source", "2", "192.0.2.10/32")
	if err != nil {
		t.Fatal(err)
	}
	if a.Registration.OperationID == b.Registration.OperationID || a.Registration.VerifierSHA256 == b.Registration.VerifierSHA256 {
		t.Fatal("purposes share custody")
	}
	path, _ := e.MachineCredentialPath(id, connectorcontrol.PolicyMachinePurpose)
	if err := os.Rename(path+".prepared.json", path+".fixture.json"); err != nil {
		t.Fatal(err)
	}
	replay, err := e.PrepareMachineCredential(context.Background(), id, connectorcontrol.PolicyMachinePurpose, "source", "2", "192.0.2.10/32")
	if err != nil || a != replay {
		t.Fatal("custody-to-state recovery changed candidate", err)
	}
	before := snapshotManagedFiles(t, e.Paths.Root)
	if _, err := e.PrepareMachineCredential(context.Background(), id, connectorcontrol.PolicyMachinePurpose, "source", "2", "192.0.2.11/32"); err == nil {
		t.Fatal("implicit candidate rotation")
	}
	if string(before) != string(snapshotManagedFiles(t, e.Paths.Root)) {
		t.Fatal("rejected change mutated files")
	}
}

func TestManagedReferenceWritesKeepPrivateParentsAndPublicConfiguration(t *testing.T) {
	e, _ := managedCustodyFixture(t)
	id := "xci_AAAAAAAAAAAAAAAAAAAAAA"
	private := filepath.Join(e.Paths.EtcDir, "connector-machines")
	dir := filepath.Join(private, id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := e.PrepareMachineCredential(t.Context(), id, connectorcontrol.PolicyMachinePurpose, "source", "2", "192.0.2.10/32"); err != nil {
		t.Fatal(err)
	}
	// A completed old candidate can be replayed without rewriting its key or
	// metadata, while repairing the former0755 private directory.
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := e.PrepareMachineCredential(t.Context(), id, connectorcontrol.PolicyMachinePurpose, "source", "2", "192.0.2.10/32"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.PrepareSourceCredential(t.Context(), id, "xise_1", "facts", "source", "2", "86", []string{sourcefacts.CostProfile}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{private, dir, filepath.Join(dir, "sources")} {
		info, err := os.Lstat(path)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("private directory widened: %s %v", path, err)
		}
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	path, _ := e.MachineCredentialPath(id, connectorcontrol.PolicyMachinePurpose)
	if err := e.ensureManagedReferenceDirectory(path + ".prepared.json.next"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0500 {
		t.Fatal("stricter existing custody directory changed", err)
	}
	// Return only this fixture directory to its original writable mode.
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	public := filepath.Join(e.Paths.EtcDir, "fixture-public.conf")
	if err := e.publishReference(public, []byte("public\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(e.Paths.EtcDir); err != nil || info.Mode().Perm() != 0755 {
		t.Fatal("public configuration directory changed", err)
	}
}
