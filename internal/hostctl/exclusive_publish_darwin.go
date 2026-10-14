//go:build darwin

package hostctl

import "golang.org/x/sys/unix"

func publishExclusiveFile(from, to string) error { return unix.RenamexNp(from, to, unix.RENAME_EXCL) }
