package reviewbody

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"syscall"
)

// A transient proof of the loaded in-memory key, bound to a current systemd
// process. It contains no key or key digest and needs no retained log/state DB.
type LoadedProof struct {
	Unit         string `json:"unit"`
	InvocationID string `json:"invocation_id"`
	PID          int    `json:"pid"`
	KeyID        string `json:"key_id"`
	MAC          string `json:"mac"`
}

var invocationPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func (p LoadedProof) payload() ([]byte, error) {
	if (p.Unit != "xinsight.service" && p.Unit != "xinsight-source-facts.service") || !invocationPattern.MatchString(p.InvocationID) || p.PID < 1 || !keyIDPattern.MatchString(p.KeyID) {
		return nil, ErrInvalid
	}
	return []byte("xinsight.loaded-review-key.v1\x00" + p.Unit + "\x00" + p.InvocationID + "\x00" + strconv.Itoa(p.PID) + "\x00" + p.KeyID), nil
}
func (r KeyRing) LoadedProof(unit, invocation string, pid int) (LoadedProof, error) {
	p := LoadedProof{Unit: unit, InvocationID: invocation, PID: pid, KeyID: r.ActiveID}
	raw, err := p.payload()
	if err != nil {
		return p, err
	}
	key, err := r.key(p.KeyID)
	if err != nil {
		return p, err
	}
	h := hmac.New(sha256.New, key)
	h.Write(raw)
	p.MAC = hex.EncodeToString(h.Sum(nil))
	return p, nil
}
func (r KeyRing) VerifyLoadedProof(p LoadedProof) error {
	expected, err := r.LoadedProof(p.Unit, p.InvocationID, p.PID)
	if err != nil {
		return err
	}
	a, err := hex.DecodeString(p.MAC)
	if err != nil {
		return ErrIntegrity
	}
	b, _ := hex.DecodeString(expected.MAC)
	if p.KeyID != r.ActiveID || !hmac.Equal(a, b) {
		return ErrIntegrity
	}
	return nil
}

const WorkerLoadedProofPath = "/run/x-insight-review-source/loaded.json"

func (r KeyRing) WriteWorkerLoadedProof() error {
	if r.ActiveID == "" || os.Getenv("INVOCATION_ID") == "" {
		return nil
	}
	p, err := r.LoadedProof("xinsight-source-facts.service", os.Getenv("INVOCATION_ID"), os.Getpid())
	if err != nil {
		return err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return writeWorkerLoadedProof(WorkerLoadedProofPath, raw)
}

func writeWorkerLoadedProof(path string, raw []byte) error {
	directory := filepath.Dir(path)
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0750 {
		return fmt.Errorf("review body runtime proof directory unavailable")
	}
	identity, ok := info.Sys().(*syscall.Stat_t)
	if !ok || identity.Uid != uint32(os.Geteuid()) || identity.Gid != uint32(os.Getegid()) {
		return fmt.Errorf("review body runtime proof directory identity differs")
	}
	f, err := os.CreateTemp(directory, ".loaded-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(raw); err == nil {
		err = f.Chmod(0640)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
