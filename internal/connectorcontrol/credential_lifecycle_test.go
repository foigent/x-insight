package connectorcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCredentialLifecycleFrozenWireFixtures(t *testing.T) {
	for _, name := range []string{"rotate", "recover", "confirm", "prepared", "prepared-microseconds", "committed"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", "credential-v1", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var marshaled []byte
			switch name {
			case "rotate", "recover":
				v, decodeErr := DecodeCredentialRequest(raw)
				if decodeErr != nil {
					t.Fatal(decodeErr)
				}
				marshaled, err = json.Marshal(v)
			case "confirm":
				v, decodeErr := DecodeCredentialConfirm(raw)
				if decodeErr != nil {
					t.Fatal(decodeErr)
				}
				marshaled, err = json.Marshal(v)
			default:
				v, decodeErr := DecodeCredentialReceipt(raw)
				if decodeErr != nil {
					t.Fatal(decodeErr)
				}
				marshaled, err = json.Marshal(v)
			}
			if err != nil || !bytes.Equal(raw, marshaled) {
				t.Fatalf("wire mismatch: error=%v\nactual=%s", err, marshaled)
			}
		})
	}
}

func TestCredentialClientFailureCategoriesDoNotExposeTransportErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{context.DeadlineExceeded, "context_deadline"},
		{context.Canceled, "context_cancelled"},
		{os.ErrNotExist, "socket_missing"},
		{syscall.ECONNREFUSED, "connection_refused"},
		{io.EOF, "connection_closed"},
		{errors.New("private path or ticket"), "internal"},
	} {
		if got := credentialClientFailureCategory(tc.err); got != tc.want {
			t.Fatalf("transport diagnostic=%q want=%q", got, tc.want)
		}
	}
}

func TestCredentialLifecycleRejectsChangedRecoveryShape(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "credential-v1", "recover.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, changed := range [][]byte{
		bytes.Replace(raw, []byte(`"superseded_operation_id":""`), nil, 1),
		bytes.Replace(raw, []byte(`"kind":"recover"`), []byte(`"kind":"rotate"`), 1),
		bytes.Replace(raw, []byte(`"recovery_id"`), []byte(`"recovery_id":"other","recovery_id"`), 1),
	} {
		if _, err := DecodeCredentialRequest(changed); err == nil {
			t.Fatalf("accepted altered recovery: %s", changed)
		}
	}
}
