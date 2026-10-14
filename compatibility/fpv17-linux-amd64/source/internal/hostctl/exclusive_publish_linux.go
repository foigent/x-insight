//go:build linux

package hostctl

import "golang.org/x/sys/unix"

func publishExclusiveFile(from, to string) error {
	return unix.Renameat2(unix.AT_FDCWD, from, unix.AT_FDCWD, to, unix.RENAME_NOREPLACE)
}
