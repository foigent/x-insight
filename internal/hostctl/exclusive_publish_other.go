//go:build !linux && !darwin

package hostctl

import "fmt"

func publishExclusiveFile(from, to string) error {
	return fmt.Errorf("exclusive atomic publication unsupported on this platform")
}
