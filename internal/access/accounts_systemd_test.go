package access

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAccountRuntimeExceptionDoesNotRelaxProvisioningOrOrdinaryFiles(t *testing.T) {
	contents, err := json.Marshal(validAccountConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := persistentAccessTestDirectory(t)
	path := filepath.Join(dir, AccountCredential)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CREDENTIALS_DIRECTORY", dir)
	for _, mode := range []os.FileMode{0o400, 0o600, 0o440, 0o640, 0o444, 0o644} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		_, provisionErr := ReadAndValidateAccountFile(path)
		_, runtimeErr := LoadLocalProvider(nil)
		allowed := mode == 0o400 || mode == 0o600
		if (provisionErr == nil) != allowed || (runtimeErr == nil) != allowed {
			t.Fatalf("mode %04o: provisioning=%v runtime=%v", mode, provisionErr, runtimeErr)
		}
	}
}
