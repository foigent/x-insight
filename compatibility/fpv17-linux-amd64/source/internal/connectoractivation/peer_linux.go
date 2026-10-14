//go:build linux

package connectoractivation

import (
	"fmt"
	"net"
	"syscall"
)

func unixPeerUID(connection *net.UnixConn) (uint32, error) {
	if connection == nil {
		return 0, fmt.Errorf("local connector controller connection is required")
	}
	raw, err := connection.SyscallConn()
	if err != nil {
		return 0, err
	}
	var (
		credential *syscall.Ucred
		controlErr error
	)
	if err := raw.Control(func(fd uintptr) {
		credential, controlErr = syscall.GetsockoptUcred(
			int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED,
		)
	}); err != nil {
		return 0, err
	}
	if controlErr != nil || credential == nil {
		return 0, fmt.Errorf("local connector controller peer credential is unavailable")
	}
	return credential.Uid, nil
}
