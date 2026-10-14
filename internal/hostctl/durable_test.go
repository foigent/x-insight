package hostctl

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestOperationLockContentionHasTypedBusyAndRecovers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operation.lock")
	first, err := acquireOperationLock(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	if second, err := acquireOperationLock(path); !errors.Is(err, ErrOperationBusy) || second != nil {
		t.Fatal("contention not typed busy", err)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := acquireOperationLock(path)
	if err != nil {
		t.Fatal("released operation did not recover", err)
	}
	if err = second.Close(); err != nil {
		t.Fatal(err)
	}
}
