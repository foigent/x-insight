//go:build integration

package connectoractivation

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/reviewpolicycontrol"
)

// NewScopedJointUnixServer changes only the existing test socket and peer seam.
// It is absent from release builds; the production dispatch/ACK are unchanged.
func NewScopedJointUnixServer(issuer connectorcontrol.InvitationIssuer, controller connectorcontrol.Controller, managed connectorcontrol.ManagedAcknowledger, path string, policy ...reviewpolicycontrol.Manager) (*UnixServer, error) {
	root := os.Getenv("XINSIGHTCTL_TEST_ROOT")
	if os.Getenv("XINSIGHT_JOINT_UR_CONSUMER") != "1" || os.Getenv("XINSIGHT_RESPONSIBILITY_LOCAL_TEST") != "1" || root == "" || !filepath.IsAbs(root) || !strings.HasPrefix(path, root+string(os.PathSeparator)) || filepath.Clean(path) != path || issuer == nil || controller == nil || managed == nil {
		return nil, fmt.Errorf("explicit joint fixture scope required")
	}
	server, err := NewUnixServer(issuer, controller, 1, 1, policy...)
	if err != nil {
		return nil, err
	}
	server.socketPath = path
	server.authorizePeer = func(*net.UnixConn) error { return nil }
	server.configureSocket = func(path string) error { return os.Chmod(path, 0600) }
	if err = server.SetManagedAcknowledger(managed); err != nil {
		return nil, err
	}
	return server, nil
}
