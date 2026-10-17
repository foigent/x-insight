package config

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// systemd's read-only credential mount uses 0440 plus an ACL granting only the
// named service UID read access. The group has no permission. This exception
// applies to the managed unit's copy, never to the administrator's source file.
func validSourceFactsSystemdCredential(path string, info os.FileInfo, file *os.File) bool {
	const dir = "/run/credentials/xinsight-source-facts.service"
	name := filepath.Base(path)
	if regexp.MustCompile(`^source-content-[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}\.key$`).MatchString(name) {
		return validSystemdCredentialCopy(path, "/run/credentials/xinsight.service", info, file)
	}
	if regexp.MustCompile(`^source-managed-[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}\.key$`).MatchString(name) {
		return validSystemdCredentialCopy(path, dir, info, file)
	}
	index, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "source-profile-"), ".key"))
	if err != nil || index < 0 || index >= 16 || name != "source-profile-"+strconv.Itoa(index)+".key" {
		return false
	}
	return validSystemdCredentialCopy(path, dir, info, file)
}

func validReviewBodySystemdCredential(path string, info os.FileInfo, file *os.File) bool {
	dir := reviewBodyCredentialDirectory(path)
	if dir == "" {
		return false
	}
	if validSystemdCredentialCopy(path, dir, info, file) {
		return true
	}
	// systemd 249 can fall back to service-owned 0400 on its read-only
	// ramfs/tmpfs mount when ACLs cannot be applied. This exception is only
	// for the exact review-body credential, never another secret purpose.
	var fs unix.Statfs_t
	var err error
	if file == nil {
		err = unix.Statfs(path, &fs)
	} else {
		err = unix.Fstatfs(int(file.Fd()), &fs)
	}
	if err != nil {
		return false
	}
	if file == nil {
		_, err = unix.Getxattr(path, "system.posix_acl_access", nil)
	} else {
		_, err = unix.Fgetxattr(int(file.Fd()), "system.posix_acl_access", nil)
	}
	absent := reviewBodyAccessACLAbsent(err)
	return validReviewBodyNativeObservation(path, info, os.Geteuid(), os.Getenv(CredentialsDirEnv), fs.Type, fs.Flags&unix.ST_RDONLY != 0, absent)
}

func reviewBodyAccessACLAbsent(err error) bool {
	return errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ENOTSUP)
}

func validManagedSourceCACopy(path string, info os.FileInfo, file *os.File) bool {
	const dir = "/run/credentials/xinsight-source-facts.service"
	return path == dir+"/source-managed-ca.pem" && validSystemdCredentialCopy(path, dir, info, file)
}

// ValidAccessAccountSystemdCredential accepts only the service's read-only ACL
// copy. Provisioning callers must keep using their strict source-file checks.
func ValidAccessAccountSystemdCredential(path string, info os.FileInfo, file *os.File) bool {
	const dir = "/run/credentials/xinsight.service"
	return filepath.Base(path) == "access-accounts.json" && validSystemdCredentialCopy(path, dir, info, file)
}

func validSystemdCredentialCopy(path string, dir string, info os.FileInfo, file *os.File) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Uid != 0 || stat.Gid != 0 || stat.Nlink != 1 || info.Mode().Perm() != 0440 || os.Getenv(CredentialsDirEnv) != dir || filepath.Dir(path) != dir {
		return false
	}
	var fs unix.Statfs_t
	acl := make([]byte, 44) // ACL header and exactly five entries.
	var n int
	var err error
	if file == nil {
		err = unix.Statfs(path, &fs)
		if err == nil {
			n, err = unix.Getxattr(path, "system.posix_acl_access", acl)
		}
	} else {
		err = unix.Fstatfs(int(file.Fd()), &fs)
		if err == nil {
			n, err = unix.Fgetxattr(int(file.Fd()), "system.posix_acl_access", acl)
		}
	}
	return err == nil && fs.Flags&unix.ST_RDONLY != 0 && n == len(acl) && sourceCredentialACL(acl, uint32(os.Geteuid()))
}

func sourceCredentialACL(acl []byte, uid uint32) bool {
	if len(acl) != 44 || binary.LittleEndian.Uint32(acl) != 2 {
		return false
	}
	for i, expected := range [][3]uint32{{1, 4, ^uint32(0)}, {2, 4, uid}, {4, 0, ^uint32(0)}, {16, 4, ^uint32(0)}, {32, 0, ^uint32(0)}} {
		entry := acl[4+i*8:]
		if uint32(binary.LittleEndian.Uint16(entry)) != expected[0] || uint32(binary.LittleEndian.Uint16(entry[2:])) != expected[1] || binary.LittleEndian.Uint32(entry[4:]) != expected[2] {
			return false
		}
	}
	return true
}
