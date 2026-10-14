//go:build integration

package connectorcontrol

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// NewScopedJointUnixController uses the production wire client with only its
// dial target mapped to the integration fixture's owned socket.
func NewScopedJointUnixController(path string) (*UnixController, error) {
	root := os.Getenv("XINSIGHTCTL_TEST_ROOT")
	if os.Getenv("XINSIGHT_JOINT_UR_CONSUMER") != "1" || os.Getenv("XINSIGHT_RESPONSIBILITY_LOCAL_TEST") != "1" || root == "" || !filepath.IsAbs(root) || !strings.HasPrefix(path, root+string(os.PathSeparator)) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("explicit joint fixture scope required")
	}
	client := NewUnixController()
	dialer := net.Dialer{Timeout: client.timeout}
	client.dial = func(ctx context.Context) (net.Conn, error) { return dialer.DialContext(ctx, "unix", path) }
	return client, nil
}
