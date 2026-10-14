package config

import (
	"crypto/x509"
	"fmt"
	"io"
	"os"
	"syscall"
)

const ManagedSourceCAEnv = "XINSIGHT_SOURCE_MANAGED_CA_FILE"

// A public trust anchor is independent of either machine bearer. Systemd's
// copy is accepted only in the exact source worker credential mount.
func LoadManagedSourceCAPool(path string) (*x509.CertPool, error) {
	if path == "" {
		return nil, nil
	}
	if ValidateManagedReference(path) != nil {
		return nil, fmt.Errorf("managed source CA reference is invalid")
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	valid := func(info os.FileInfo, f *os.File) bool {
		s, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || s.Uid != 0 || s.Nlink != 1 {
			return false
		}
		if info.Mode().Perm() == 0440 {
			return validManagedSourceCACopy(path, info, f)
		}
		return info.Mode().Perm() == 0400 || info.Mode().Perm() == 0600 || info.Mode().Perm() == 0644
	}
	if !valid(before, nil) {
		return nil, fmt.Errorf("managed source CA ownership is invalid")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) || !valid(after, f) {
		return nil, fmt.Errorf("managed source CA changed")
	}
	raw, err := io.ReadAll(io.LimitReader(f, (128<<10)+1))
	if err != nil || len(raw) > 128<<10 {
		return nil, fmt.Errorf("managed source CA size is invalid")
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, err
	}
	if !roots.AppendCertsFromPEM(raw) {
		return nil, fmt.Errorf("managed source CA format is invalid")
	}
	return roots, nil
}
