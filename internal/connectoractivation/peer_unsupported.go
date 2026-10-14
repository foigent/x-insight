//go:build !linux

package connectoractivation

import (
	"fmt"
	"net"
)

func unixPeerUID(*net.UnixConn) (uint32, error) {
	return 0, fmt.Errorf("local connector controller peer credentials require Linux")
}
