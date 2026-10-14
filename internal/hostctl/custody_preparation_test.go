package hostctl

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func automaticCustodyFixture(t *testing.T) (*Engine, *fakeDatabase, DatabaseCredentials) {
	e, _ := managedCustodyFixture(t)
	// Only the synthetic key just created for this fixture is removed.
	if err := os.Remove(filepath.Join(e.Paths.EtcDir, "review-body.key")); err != nil {
		t.Fatal(err)
	}
	database := &fakeDatabase{}
	e.Database = database
	if err := e.ensureControlDirectories(); err != nil {
		t.Fatal(err)
	}
	return e, database, DatabaseCredentials{}
}
func TestAutomaticCustodyGenerateOnceReplaysInterruptedPublication(t *testing.T) {
	e, _, creds := automaticCustodyFixture(t)
	identity := ReleaseIdentity{DatabaseSchema: "44"}
	before := snapshotManagedFiles(t, e.Paths.Root)
	if err := e.checkCustodyPreparation(context.Background(), creds, identity, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, snapshotManagedFiles(t, e.Paths.Root)) {
		t.Fatal("custody check wrote files")
	}
	j, err := e.startJournal("install", "", "", nil, &identity)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.prepareCustody(context.Background(), j, creds, identity); err != nil {
		t.Fatal(err)
	}
	c, active, present, err := e.custodyBinding()
	if err != nil || !active || !present {
		t.Fatal(c, err)
	}
	key, err := os.ReadFile(c.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 65 {
		t.Fatal("invalid generated key")
	}
	e.Random = bytes.NewReader(nil)
	if err = e.prepareCustody(context.Background(), j, creds, identity); err != nil {
		t.Fatal(err)
	}
	next, _ := os.ReadFile(c.KeyFile)
	if !bytes.Equal(key, next) {
		t.Fatal("resume regenerated key")
	}
	if err = os.Remove(e.reviewBodyEnvironment()); err != nil {
		t.Fatal(err)
	}
	if err = e.prepareCustody(context.Background(), j, creds, identity); err != nil {
		t.Fatal("partial publication replay", err)
	}
	next, _ = os.ReadFile(c.KeyFile)
	if !bytes.Equal(key, next) {
		t.Fatal("partial publication changed key")
	}
}
func TestAutomaticCustodyMissingOriginalAndExpiredCiphertextFailClosed(t *testing.T) {
	for _, scenario := range []string{"ciphertext", "binding", "orphan"} {
		t.Run(scenario, func(t *testing.T) {
			e, d, creds := automaticCustodyFixture(t)
			identity := ReleaseIdentity{DatabaseSchema: "44"}
			switch scenario {
			case "ciphertext":
				d.custodyInventoryErr = fmt.Errorf("restore_original_review_body_key_required: existing expired ciphertext")
			case "binding":
				putManagedFile(t, e.reviewBodyEnvironment(), []byte(reviewBodyIDEnv+"=old\n"+reviewBodySourceEnv+"="+filepath.Join(e.Paths.EtcDir, "review-body.key")+"\n"), 0600)
			case "orphan":
				putManagedFile(t, filepath.Join(e.Paths.EtcDir, "review-body.key"), []byte(strings.Repeat("42", 32)+"\n"), 0600)
			}
			before := snapshotManagedFiles(t, e.Paths.Root)
			if e.checkCustodyPreparation(context.Background(), creds, identity, nil) == nil {
				t.Fatal("unsafe generation allowed")
			}
			if !bytes.Equal(before, snapshotManagedFiles(t, e.Paths.Root)) {
				t.Fatal("failed preflight wrote files")
			}
		})
	}
}
