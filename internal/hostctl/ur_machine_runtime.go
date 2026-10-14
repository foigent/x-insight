package hostctl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	"github.com/foigent/x-insight/internal/connectorcontrol"
)

// This receipt is written before either configuration file. A controller
// interrupted between atomic publishes can start with an exact pending
// drop-in and finish the same binding without minting another credential.
type urMachineRuntimePending struct {
	InstallationID     string `json:"installation_id"`
	DropInSHA256       string `json:"drop_in_sha256"`
	BeforeInvocationID string `json:"before_invocation_id"`
}

var ErrURMachineRuntimePending = errors.New("UR machine runtime awaits a new controller invocation")

func (e *Engine) urMachineRuntimePendingPath() string {
	return filepath.Join(e.Paths.StateDir, "ur-machine-runtime-pending.json")
}

func (e *Engine) readURMachineRuntimePending() (urMachineRuntimePending, bool, error) {
	path := e.urMachineRuntimePendingPath()
	present, err := fileExists(path)
	if err != nil || !present {
		return urMachineRuntimePending{}, false, err
	}
	if err = e.validateRootReference(path, 0600); err != nil {
		return urMachineRuntimePending{}, true, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return urMachineRuntimePending{}, true, err
	}
	var pending urMachineRuntimePending
	if json.Unmarshal(raw, &pending) != nil || !connectorcontrol.IsInstallationID(pending.InstallationID) ||
		len(pending.DropInSHA256) != 64 || pending.BeforeInvocationID == "" {
		return urMachineRuntimePending{}, true, fmt.Errorf("UR machine runtime pending receipt is invalid")
	}
	return pending, true, nil
}

func (e *Engine) clearURMachineRuntimePending() error {
	if _, present, err := e.readURMachineRuntimePending(); err != nil || !present {
		return err
	}
	if err := os.Remove(e.urMachineRuntimePendingPath()); err != nil {
		return err
	}
	return syncDirectory(e.Paths.StateDir)
}

func machineDropInDigest(files map[string][]byte) (string, error) {
	if len(files) != 1 {
		return "", fmt.Errorf("UR machine drop-in set is invalid")
	}
	for _, body := range files {
		digest := sha256.Sum256(body)
		return hex.EncodeToString(digest[:]), nil
	}
	return "", fmt.Errorf("UR machine drop-in is missing")
}

// Called under the existing host operation lock, before the first publish.
func (e *Engine) stageURMachineRuntime(ctx context.Context, c URMachineConfig, files map[string][]byte) error {
	digest, err := machineDropInDigest(files)
	if err != nil {
		return err
	}
	pending, present, err := e.readURMachineRuntimePending()
	if err != nil {
		return err
	}
	if present {
		if pending.InstallationID != c.InstallationID || pending.DropInSHA256 != digest {
			return fmt.Errorf("another UR machine runtime transition is pending")
		}
		return nil
	}
	invocation, err := e.systemdUnitProperty(ctx, connectorControllerUnitName, "InvocationID")
	if err != nil {
		return err
	}
	if invocation == "" {
		return fmt.Errorf("controller invocation is unavailable before UR machine publication")
	}
	pending = urMachineRuntimePending{c.InstallationID, digest, invocation}
	if err = writeJSONAtomic(e.urMachineRuntimePendingPath(), pending, 0600); err != nil {
		return err
	}
	return e.validateRootReference(e.urMachineRuntimePendingPath(), 0600)
}

// EnsureControllerURMachineRuntime never declares the machine ready from file
// bytes alone. A changed binding needs daemon reload and a new invocation;
// repeated passes can resume from each interruption without rotating tokens.
func (e *Engine) EnsureControllerURMachineRuntime(ctx context.Context, installation string) error {
	if err := requireMutationAuthority(e.Paths); err != nil {
		return err
	}
	return e.withOperationLock(func() error {
		return e.ensureControllerURMachineRuntimeLocked(ctx, installation)
	})
}

func (e *Engine) ensureControllerURMachineRuntimeLocked(ctx context.Context, installation string) error {
	if !connectorcontrol.IsInstallationID(installation) {
		return connectorcontrol.ErrInvalid
	}
	if err := e.ensureNoOutstandingOperation(); err != nil {
		return err
	}
	c, origin, enabled, err := e.ReadURMachineConfig()
	if err != nil {
		return err
	}
	if !enabled || c.InstallationID != installation {
		return fmt.Errorf("UR machine binding is not active for this installation")
	}
	files, err := e.machineFiles(origin, c)
	if err != nil {
		return err
	}
	if err = e.ValidateURMachineConfig(); err != nil {
		return err
	}
	digest, err := machineDropInDigest(files)
	if err != nil {
		return err
	}
	pending, present, err := e.readURMachineRuntimePending()
	if err != nil {
		return err
	}
	if present && (pending.InstallationID != installation || pending.DropInSHA256 != digest) {
		return fmt.Errorf("UR machine runtime receipt differs from the active binding")
	}
	current, err := e.systemdUnitProperty(ctx, connectorControllerUnitName, "InvocationID")
	if err != nil {
		return err
	}
	if current == "" {
		return fmt.Errorf("controller invocation cannot be proved")
	}
	pid, err := e.systemdUnitProperty(ctx, connectorControllerUnitName, "MainPID")
	if err != nil {
		return err
	}
	if pid == "" || pid == "0" {
		return fmt.Errorf("controller process cannot be proved")
	}
	allowed, err := e.systemdUnitProperty(ctx, connectorControllerUnitName, "IPAddressAllow")
	if err != nil {
		return err
	}
	egressReady, err := machineEgressMatches(origin, allowed)
	if err != nil {
		return err
	}
	if present && current != pending.BeforeInvocationID && egressReady {
		if err = os.Remove(e.urMachineRuntimePendingPath()); err != nil {
			return err
		}
		return syncDirectory(e.Paths.StateDir)
	}
	if !present && egressReady {
		return nil
	}
	if !present {
		if err = e.stageURMachineRuntime(ctx, c, files); err != nil {
			return err
		}
	}
	if err = runSystemctl(ctx, e.Runner, "daemon-reload"); err != nil {
		return err
	}
	if err = runSystemctl(ctx, e.Runner, "--no-block", "restart", connectorControllerUnitName); err != nil {
		return err
	}
	return ErrURMachineRuntimePending
}

// Network clients use a read-only gate. The maintenance pass owns reload and
// restart; no request may treat a pending transition as an active credential.
func (e *Engine) ProveControllerURMachineRuntime(ctx context.Context, installation string) error {
	if !connectorcontrol.IsInstallationID(installation) {
		return connectorcontrol.ErrInvalid
	}
	if _, present, err := e.readURMachineRuntimePending(); err != nil {
		return err
	} else if present {
		return ErrURMachineRuntimePending
	}
	c, origin, enabled, err := e.ReadURMachineConfig()
	if err != nil {
		return err
	}
	if !enabled || c.InstallationID != installation {
		return fmt.Errorf("UR machine binding is not active for this installation")
	}
	if err = e.ValidateURMachineConfig(); err != nil {
		return err
	}
	allowed, err := e.systemdUnitProperty(ctx, connectorControllerUnitName, "IPAddressAllow")
	if err != nil {
		return err
	}
	ready, err := machineEgressMatches(origin, allowed)
	if err != nil {
		return err
	}
	if !ready {
		return ErrURMachineRuntimePending
	}
	return nil
}

func machineEgressMatches(origin, allowed string) (bool, error) {
	ip, err := machineEgressPrefix(origin)
	if err != nil {
		return false, err
	}
	remote, _ := netip.ParsePrefix(ip)
	expected := reduceMachineEgressPrefixes([]netip.Prefix{
		netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128"), remote,
	})
	observed, ok := parseMachineEgressPrefixes(allowed)
	if !ok || len(observed) != len(expected) {
		return false, nil
	}
	for prefix := range expected {
		if _, present := observed[prefix]; !present {
			return false, nil
		}
	}
	return true, nil
}

func parseMachineEgressPrefixes(allowed string) (map[netip.Prefix]struct{}, bool) {
	var prefixes []netip.Prefix
	for _, word := range strings.Fields(allowed) {
		if word == "localhost" {
			prefixes = append(prefixes, netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128"))
			continue
		}
		prefix, err := netip.ParsePrefix(word)
		if err != nil {
			address, addressErr := netip.ParseAddr(word)
			if addressErr != nil {
				return nil, false
			}
			prefix = netip.PrefixFrom(address, address.BitLen())
		}
		if !prefix.IsValid() || prefix.Addr().Zone() != "" || prefix.Addr().Is4In6() {
			return nil, false
		}
		prefixes = append(prefixes, prefix)
	}
	return reduceMachineEgressPrefixes(prefixes), len(prefixes) != 0
}

// Match systemd's v249 reduction: mask each prefix and discard entries fully
// covered by another. The comparison remains fail-closed for any wider or
// additional address range, while accepting redundant loopback host entries.
func reduceMachineEgressPrefixes(prefixes []netip.Prefix) map[netip.Prefix]struct{} {
	unique := make(map[netip.Prefix]struct{}, len(prefixes))
	for _, prefix := range prefixes {
		unique[prefix.Masked()] = struct{}{}
	}
	reduced := make(map[netip.Prefix]struct{}, len(unique))
	for prefix := range unique {
		covered := false
		for other := range unique {
			if other != prefix && other.Addr().Is4() == prefix.Addr().Is4() &&
				other.Bits() <= prefix.Bits() && other.Contains(prefix.Addr()) {
				covered = true
				break
			}
		}
		if !covered {
			reduced[prefix] = struct{}{}
		}
	}
	return reduced
}
