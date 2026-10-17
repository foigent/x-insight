package hostctl

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

const maxControlFileBytes = 2 << 20

type operationLock struct {
	file *os.File
}

func acquireOperationLock(path string) (*operationLock, error) {
	if err := ensureDirectory(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("another xinsightctl operation is active")
	}
	return &operationLock{file: file}, nil
}

func (lock *operationLock) Close() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	_ = syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN)
	return lock.file.Close()
}

func ensureDirectory(path string, mode os.FileMode) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("directory path must be absolute and clean")
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("control path is not a real directory: %s", path)
		}
		return os.Chmod(path, mode)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("control path is not a real directory: %s", path)
	}
	return os.Chmod(path, mode)
}

func writeJSONExclusive(path string, value any, mode os.FileMode) error {
	if err := ensureDirectory(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	contents, err := marshalJSON(value)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if err := writeSyncClose(file, contents); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func writeJSONAtomic(path string, value any, mode os.FileMode) error {
	if err := ensureDirectory(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	contents, err := marshalJSON(value)
	if err != nil {
		return err
	}
	suffix, err := randomToken(8)
	if err != nil {
		return err
	}
	temporary := path + ".next-" + suffix
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if err := writeSyncClose(file, contents); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func readJSONStrict(path string, value any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxControlFileBytes {
		return fmt.Errorf("control JSON file type or size is invalid")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxControlFileBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("control JSON file contains trailing content")
	}
	return nil
}

func atomicSymlink(target string, link string) error {
	if !filepath.IsAbs(target) || !filepath.IsAbs(link) || filepath.Clean(target) != target || filepath.Clean(link) != link {
		return fmt.Errorf("symlink paths must be absolute and clean")
	}
	if err := ensureDirectory(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	suffix, err := randomToken(8)
	if err != nil {
		return err
	}
	temporary := link + ".next-" + suffix
	if err := os.Symlink(target, temporary); err != nil {
		return err
	}
	if err := os.Rename(temporary, link); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(link))
}

func readAbsoluteSymlink(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return "", fmt.Errorf("managed path is not a symbolic link")
	}
	target, err := os.Readlink(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(target) || filepath.Clean(target) != target {
		return "", fmt.Errorf("managed symbolic link target must be absolute and clean")
	}
	return target, nil
}

func fileSHA256(path string, limit int64) (string, int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > limit {
		return "", 0, fmt.Errorf("file type or size is invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(file, limit+1))
	if err != nil || written != info.Size() {
		return "", 0, fmt.Errorf("file changed while hashing")
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil)), written, nil
}

func marshalJSON(value any) ([]byte, error) {
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(contents, '\n'), nil
}

func writeSyncClose(file *os.File, contents []byte) error {
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func randomToken(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
