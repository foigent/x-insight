//go:build integration

package reviewpolicycontrol

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// Only an explicit isolated joint test can change the Unix socket location.
// Authentication, typed dispatch and policy handling remain the original path.
func NewScopedJointUnixPolicyClient(path string) (*UnixClient, error) {
	root := os.Getenv("XINSIGHTCTL_TEST_ROOT")
	if os.Getenv("XINSIGHT_JOINT_UR_CONSUMER") != "1" || os.Getenv("XINSIGHT_RESPONSIBILITY_LOCAL_TEST") != "1" || root == "" || !filepath.IsAbs(root) || !strings.HasPrefix(path, root+string(os.PathSeparator)) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("explicit joint fixture scope required")
	}
	c := NewUnixClient()
	dialer := net.Dialer{Timeout: c.Timeout}
	c.Dial = func(ctx context.Context, _ string) (net.Conn, error) { return dialer.DialContext(ctx, "unix", path) }
	return c, nil
}
