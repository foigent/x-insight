package connectorsync

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var fallbackStarted = time.Now().UnixNano()

func procIdentity(pid int) (string, error) {
	b, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if e != nil {
		return "", e
	}
	i := strings.LastIndexByte(string(b), ')')
	if i < 0 {
		return "", ErrInvalid
	}
	f := strings.Fields(string(b[i+1:]))
	if len(f) < 20 {
		return "", ErrInvalid
	}
	return f[19], nil
}
func processOwner() string {
	boot, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	start, se := procIdentity(os.Getpid())
	if e == nil && se == nil {
		return fmt.Sprintf("%s:%d:%s", strings.TrimSpace(string(boot)), os.Getpid(), start)
	}
	return fmt.Sprintf("unconfirmed:%d:%d", os.Getpid(), fallbackStarted)
}

// ownerExited never infers death from lease expiry, permission failure or a
// reused PID. Unsupported platforms keep the outstanding state unknown.
func ownerExited(owner string) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	parts := strings.Split(owner, ":")
	if len(parts) != 3 || parts[0] == "unconfirmed" {
		return false
	}
	boot, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil {
		return false
	}
	if strings.TrimSpace(string(boot)) != parts[0] {
		return true
	}
	pid, e := strconv.Atoi(parts[1])
	if e != nil || pid <= 0 {
		return false
	}
	start, e := procIdentity(pid)
	return os.IsNotExist(e) || e == nil && start != parts[2]
}

func ownerKnownAlive(owner string) bool {
	if owner == processOwner() {
		return true
	}
	if runtime.GOOS != "linux" {
		return false
	}
	parts := strings.Split(owner, ":")
	if len(parts) != 3 || parts[0] == "unconfirmed" {
		return false
	}
	boot, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil || strings.TrimSpace(string(boot)) != parts[0] {
		return false
	}
	pid, e := strconv.Atoi(parts[1])
	if e != nil || pid <= 0 {
		return false
	}
	start, e := procIdentity(pid)
	return e == nil && start == parts[2]
}
