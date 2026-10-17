package hostctl

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/reviewbody"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

const (
	reviewBodyIDEnv     = "XINSIGHT_REVIEW_BODY_KEY_ID"
	reviewBodySourceEnv = "XINSIGHT_REVIEW_BODY_SOURCE_KEY_FILE"
	reviewBodyDropIn    = "20-review-body-credentials.conf"
	machineDropIn       = "20-ur-machine-connection.conf"
	reviewBodyStartup   = "ExecStartPre=+/usr/bin/env -i /opt/x-insight/current/bin/xinsightctl review-body-config validate"
	machineStartup      = "ExecStartPre=+/usr/bin/env -i /opt/x-insight/current/bin/xinsightctl connector-target-config validate"
)

type ReviewBodyConfig struct{ KeyID, KeyFile string }
type URMachineConfig struct{ InstallationID, SourceInstance, OrgID, PolicyFile, EnrollFile, CAFile string }

// Read the root-owned module directly so later pairing is visible without
// restarting the controller or copying its environment snapshot.
func (e *Engine) ReadURMachineConfig() (URMachineConfig, string, bool, error) {
	var empty URMachineConfig
	if err := requireMutationAuthority(e.Paths); err != nil {
		return empty, "", false, err
	}
	if present, err := fileExists(e.Paths.ConnectorEnvironment); err != nil {
		return empty, "", false, err
	} else if !present {
		return empty, "", false, nil
	}
	if err := e.validateRootReference(e.Paths.ConnectorEnvironment, 0600); err != nil {
		return empty, "", false, err
	}
	values, err := readAllowlistedEnvironmentFile(e.Paths.ConnectorEnvironment, true, connectorTargetEnvironmentKeys)
	if err != nil {
		return empty, "", false, err
	}
	origin, err := readConnectorTargetEnvironmentFile(e.Paths.ConnectorEnvironment)
	if err != nil {
		return empty, "", false, err
	}
	c, enabled, err := ParseURMachineConfig(values)
	if err == nil && enabled {
		err = e.validateMachineCandidate(c)
	}
	return c, origin, enabled, err
}

func (e *Engine) reviewBodyEnvironment() string {
	return filepath.Join(e.Paths.EtcDir, "xinsight-review-body.env")
}

func (e *Engine) verifyRestoredReviewBodyKeys(ctx context.Context, credentials DatabaseCredentials) error {
	c, _, enabled, err := e.custodyBinding()
	if err != nil {
		return err
	}
	keys := reviewbody.KeyRing{}
	if enabled {
		keys, err = reviewbody.LoadKeyRingFromFile(c.KeyID, c.KeyFile)
		if err != nil {
			return err
		}
	}
	return e.Database.VerifyReviewBodyRestore(ctx, credentials, keys, e.now())
}

// Host references are protected by root ownership and exact non-writable modes.
// GID grants no access for0400/0600 and no extra access for public0644; it need
// not be changed by the capless root controller. Native systemd credentials
// retain their separate strict GID/ACL/mount contract.
func (e *Engine) validateRootReference(path string, modes ...os.FileMode) error {
	if err := config.ValidateManagedReference(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 128<<10 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("managed reference type is invalid")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	owner := uint32(0)
	if e.Paths.IsTestRoot {
		owner = uint32(os.Geteuid())
	}
	if !ok || stat.Uid != owner || stat.Nlink != 1 {
		return fmt.Errorf("managed reference ownership or links are invalid")
	}
	if err := e.validateReferenceParent(path); err != nil {
		return err
	}
	for _, mode := range modes {
		if info.Mode().Perm() == mode {
			return nil
		}
	}
	return fmt.Errorf("managed reference permissions are invalid")
}

func (e *Engine) validateReferenceParent(path string) error {
	parent := filepath.Dir(path)
	for {
		info, err := os.Lstat(parent)
		if os.IsNotExist(err) {
			next := filepath.Dir(parent)
			if next == parent {
				return fmt.Errorf("managed reference parent is unavailable")
			}
			parent = next
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("managed reference parent is unsafe")
		}
		owner := uint32(0)
		if e.Paths.IsTestRoot {
			owner = uint32(os.Geteuid())
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != owner {
			return fmt.Errorf("managed reference parent ownership is invalid")
		}
		if e.Paths.IsTestRoot && parent == e.Paths.Root {
			return nil
		}
		next := filepath.Dir(parent)
		if next == parent {
			return nil
		}
		parent = next
	}
}

func (e *Engine) readReviewBodyConfig() (ReviewBodyConfig, bool, error) {
	path := e.reviewBodyEnvironment()
	present, err := fileExists(path)
	if err != nil || !present {
		return ReviewBodyConfig{}, false, err
	}
	if err := e.validateRootReference(path, 0600); err != nil {
		return ReviewBodyConfig{}, true, err
	}
	v, err := readAllowlistedEnvironmentFile(path, true, map[string]struct{}{reviewBodyIDEnv: {}, reviewBodySourceEnv: {}})
	c := ReviewBodyConfig{v[reviewBodyIDEnv], v[reviewBodySourceEnv]}
	if err != nil || len(v) != 2 {
		return c, true, fmt.Errorf("review body configuration is incomplete or invalid")
	}
	return c, true, e.validateReviewBodyCandidate(c)
}

func (e *Engine) validateReviewBodyCandidate(c ReviewBodyConfig) error {
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`).MatchString(c.KeyID) {
		return fmt.Errorf("review body key ID is invalid")
	}
	if err := e.validateRootReference(c.KeyFile, 0600); err != nil {
		return err
	}
	if _, err := config.LoadReviewBodyKeyFile(c.KeyFile); err != nil {
		return err
	}
	if _, err := config.LoadPostgresDSNFile(e.Paths.WorkspaceDSNFile); err != nil {
		return fmt.Errorf("review custody requires the existing workspace database credential")
	}
	return nil
}

func (e *Engine) reviewBodyFiles(c ReviewBodyConfig) map[string][]byte {
	files := map[string][]byte{}
	for _, unit := range []string{e.Paths.ServiceUnit, e.Paths.SourceFactsServiceUnit} {
		body := "[Service]\nLoadCredential=review-body.key:" + c.KeyFile + "\nEnvironment=XINSIGHT_REVIEW_BODY_KEY_FILE=/run/credentials/" + filepath.Base(unit) + "/review-body.key\n"
		if unit == e.Paths.SourceFactsServiceUnit {
			body += "LoadCredential=postgres-workspace.dsn:" + e.Paths.WorkspaceDSNFile + "\n"
		}
		files[filepath.Join(unit+".d", reviewBodyDropIn)] = []byte(body)
	}
	return files
}

func (e *Engine) ValidateReviewBodyConfig() error {
	c, present, err := e.readReviewBodyConfig()
	if err != nil {
		return err
	}
	for _, unit := range []string{e.Paths.ServiceUnit, e.Paths.SourceFactsServiceUnit} {
		path := filepath.Join(unit+".d", reviewBodyDropIn)
		if !present {
			if exists, err := fileExists(path); err != nil {
				return err
			} else if exists {
				return fmt.Errorf("review body drop-in has no configuration authority")
			}
			continue
		}
		if err := e.validateGeneratedReference(path, e.reviewBodyFiles(c)[path]); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) validateGeneratedReference(path string, expected []byte) error {
	if err := e.validateRootReference(path, 0644); err != nil {
		return err
	}
	return validateExactServiceDropIn(path, string(expected))
}

func (e *Engine) SetReviewBodyConfig(c ReviewBodyConfig, check bool) error {
	if err := requireMutationAuthority(e.Paths); err != nil {
		return err
	}
	run := func() error {
		if err := e.ensureNoOutstandingOperation(); err != nil {
			return err
		}
		if err := e.validateReviewBodyCandidate(c); err != nil {
			return err
		}
		old, present, err := e.readReviewBodyConfig()
		if err != nil {
			return err
		}
		// A different key would strand existing ciphertext; rotation is not this command.
		if present && old != c {
			return fmt.Errorf("review body key binding already exists; implicit rotation is forbidden")
		}
		if !present {
			retired := e.reviewBodyEnvironment() + ".disabled"
			if exists, err := fileExists(retired); err != nil {
				return err
			} else if exists {
				if err := e.validateRootReference(retired, 0600); err != nil {
					return err
				}
				v, err := readAllowlistedEnvironmentFile(retired, true, map[string]struct{}{reviewBodyIDEnv: {}, reviewBodySourceEnv: {}})
				if err != nil || len(v) != 2 || (ReviewBodyConfig{v[reviewBodyIDEnv], v[reviewBodySourceEnv]}) != c {
					return fmt.Errorf("retired custody key binding forbids implicit rotation")
				}
			}
		}
		if err := e.requireUnitDirective(e.Paths.ServiceUnit, reviewBodyStartup); err != nil {
			return err
		}
		if err := e.requireUnitDirective(e.Paths.SourceFactsServiceUnit, reviewBodyStartup); err != nil {
			return err
		}
		files := e.reviewBodyFiles(c)
		if err := e.checkReferenceCandidates(files, nil); err != nil {
			return err
		}
		env := []byte(reviewBodyIDEnv + "=" + c.KeyID + "\n" + reviewBodySourceEnv + "=" + c.KeyFile + "\n")
		if err := e.checkReferenceStaging(e.reviewBodyEnvironment(), env, 0600); err != nil {
			return err
		}
		if check {
			return nil
		}
		for path, body := range files {
			if err := e.publishReference(path, body, 0644); err != nil {
				return err
			}
		}
		if err := e.publishReference(e.reviewBodyEnvironment(), env, 0600); err != nil {
			return err
		}
		return e.ValidateReviewBodyConfig()
	}
	if check {
		return run()
	} // No lock file, staging file, reload or network in --check.
	return e.withOperationLock(run)
}

// Retire only this domain's exact references, reversibly. Source keys and
// ciphertext are never removed. Repeat disable resumes a partial retirement.
func (e *Engine) DisableReviewBodyConfig(check bool) error {
	if err := requireMutationAuthority(e.Paths); err != nil {
		return err
	}
	run := func() error {
		if err := e.ensureNoOutstandingOperation(); err != nil {
			return err
		}
		path := e.reviewBodyEnvironment()
		active, err := fileExists(path)
		if err != nil {
			return err
		}
		if !active {
			if retired, err := fileExists(path + ".disabled"); err != nil {
				return err
			} else if !retired {
				return e.ValidateReviewBodyConfig()
			}
			path += ".disabled"
		}
		if err := e.validateRootReference(path, 0600); err != nil {
			return err
		}
		v, err := readAllowlistedEnvironmentFile(path, true, map[string]struct{}{reviewBodyIDEnv: {}, reviewBodySourceEnv: {}})
		if err != nil || len(v) != 2 {
			return fmt.Errorf("retiring review body configuration is invalid")
		}
		c := ReviewBodyConfig{v[reviewBodyIDEnv], v[reviewBodySourceEnv]}
		if err := e.validateReviewBodyCandidate(c); err != nil {
			return err
		}
		files := e.reviewBodyFiles(c)
		for file, body := range files {
			if exists, err := fileExists(file); err != nil {
				return err
			} else if exists {
				if err := e.validateGeneratedReference(file, body); err != nil {
					return err
				}
			}
			if exists, err := fileExists(file + ".disabled"); err != nil {
				return err
			} else if exists {
				if err := e.validateGeneratedReference(file+".disabled", body); err != nil {
					return err
				}
			}
		}
		if check {
			return nil
		}
		// Unit files first, authority last: a partial retirement fails startup.
		for file := range files {
			if err := e.retireReference(file, 0644); err != nil {
				return err
			}
		}
		if active {
			if err := e.retireReference(e.reviewBodyEnvironment(), 0600); err != nil {
				return err
			}
		}
		return e.ValidateReviewBodyConfig()
	}
	if check {
		return run()
	}
	return e.withOperationLock(run)
}

func (e *Engine) retireReference(path string, mode os.FileMode) error {
	present, err := fileExists(path)
	if err != nil || !present {
		return err
	}
	if err := e.validateRootReference(path, mode); err != nil {
		return err
	}
	if exists, err := fileExists(path + ".disabled"); err != nil {
		return err
	} else if exists {
		if err := e.validateRootReference(path+".disabled", mode); err != nil {
			return err
		}
		if err := requireMatchingFiles(path, path+".disabled", maxEnvironmentFileBytes); err != nil {
			return fmt.Errorf("different retired reference exists")
		}
	}
	if err := os.Rename(path, path+".disabled"); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func (e *Engine) requireUnitDirective(path, directive string) error {
	if err := e.validateRootReference(path, 0644); err != nil {
		return err
	}
	contents, err := os.ReadFile(path)
	if err != nil || len(contents) > 128<<10 || !strings.Contains(string(contents), directive+"\n") {
		return fmt.Errorf("target unit does not support managed custody or machine configuration")
	}
	return nil
}

func (e *Engine) checkReferenceCandidates(files, old map[string][]byte) error {
	for path, expected := range files {
		if err := e.validateReferenceParent(path); err != nil {
			return err
		}
		if err := e.checkReferenceStaging(path, expected, 0644); err != nil {
			return err
		}
		present, err := fileExists(path)
		if err != nil {
			return err
		}
		if !present {
			continue
		}
		if err := e.validateRootReference(path, 0644); err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(body, expected) && !bytes.Equal(body, old[path]) {
			return fmt.Errorf("managed reference conflicts with the candidate: %s", filepath.Base(path))
		}
	}
	return nil
}

func (e *Engine) checkReferenceStaging(path string, body []byte, mode os.FileMode) error {
	if err := e.validateReferenceParent(path); err != nil {
		return err
	}
	present, err := fileExists(path + ".next")
	if err != nil || !present {
		return err
	}
	modes := []os.FileMode{mode}
	if mode == 0644 {
		modes = append(modes, 0600)
	} // Interrupted before public-reference chmod.
	if err := e.validateRootReference(path+".next", modes...); err != nil {
		return err
	}
	current, err := os.ReadFile(path + ".next")
	if err != nil || !bytes.Equal(current, body) {
		return fmt.Errorf("different managed configuration staging exists")
	}
	return nil
}

// Domain references are staged under the existing maintenance lock. Authority
// is published last; startup validation rejects a mixed or interrupted set.
func (e *Engine) publishReference(path string, body []byte, mode os.FileMode) error {
	if err := e.checkReferenceStaging(path, body, mode); err != nil {
		return err
	}
	if present, err := fileExists(path); err != nil {
		return err
	} else if present {
		if err := e.validateRootReference(path, mode); err != nil {
			return err
		}
		current, err := os.ReadFile(path)
		if err == nil && bytes.Equal(current, body) {
			return e.ensureManagedReferenceDirectory(path)
		}
	}
	if err := e.ensureManagedReferenceDirectory(path); err != nil {
		return err
	}
	next := path + ".next"
	if exists, err := fileExists(next); err != nil {
		return err
	} else if exists {
		modes := []os.FileMode{mode}
		if mode == 0644 {
			modes = append(modes, 0600)
		}
		if err := e.validateRootReference(next, modes...); err != nil {
			return err
		}
		current, err := os.ReadFile(next)
		if err != nil || !bytes.Equal(current, body) {
			return fmt.Errorf("different managed configuration staging exists")
		}
	} else {
		if err := e.writeConnectorEnvironmentExclusive(next, body); err != nil {
			return err
		}
	}
	if err := os.Chmod(next, mode); err != nil {
		return err
	}
	file, err := os.Open(next)
	if err != nil {
		return err
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(next, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func ParseURMachineConfig(v map[string]string) (URMachineConfig, bool, error) {
	c := URMachineConfig{v[config.URMachineInstallationEnv], v[config.URMachineSourceEnv], v[config.URMachineOrgEnv], v[config.URPolicyFileEnv], v[config.UREnrollFileEnv], v[config.URMachineCAEnv]}
	if c == (URMachineConfig{}) {
		return c, false, nil
	}
	if !connectorcontrol.IsInstallationID(c.InstallationID) || c.SourceInstance == "" || len(c.SourceInstance) > 128 || strings.ContainsAny(c.SourceInstance, " \t\r\n\\\"'%$") || !regexp.MustCompile(`^[1-9][0-9]{0,18}$`).MatchString(c.OrgID) || c.PolicyFile == "" && c.EnrollFile == "" {
		return c, true, fmt.Errorf("machine identity or purpose group is incomplete")
	}
	for _, path := range []string{c.PolicyFile, c.EnrollFile, c.CAFile} {
		if path != "" {
			if err := config.ValidateManagedReference(path); err != nil {
				return c, true, err
			}
		}
	}
	if c.PolicyFile != "" && c.PolicyFile == c.EnrollFile {
		return c, true, fmt.Errorf("machine purposes require independent files")
	}
	return c, true, nil
}

func parseURMachineConfig(v map[string]string) (URMachineConfig, bool, error) {
	return ParseURMachineConfig(v)
}

func (e *Engine) validateMachineCandidate(c URMachineConfig) error {
	for _, purpose := range []struct{ path, prefix string }{{c.PolicyFile, "xpm1"}, {c.EnrollFile, "xse1"}} {
		if purpose.path == "" {
			continue
		}
		if err := e.validateRootReference(purpose.path, 0400, 0600); err != nil {
			return err
		}
		value, err := config.LoadOwnerOnlySecretFile(purpose.path)
		if err != nil {
			return err
		}
		if err := config.ValidateURMachineToken(value, purpose.prefix); err != nil {
			return err
		}
	}
	if c.CAFile != "" {
		if err := e.validateRootReference(c.CAFile, 0400, 0600, 0644); err != nil {
			return err
		}
		body, err := os.ReadFile(c.CAFile)
		if err != nil || len(body) > 128<<10 || !x509.NewCertPool().AppendCertsFromPEM(body) {
			return fmt.Errorf("machine CA file is invalid")
		}
	}
	return nil
}

func (e *Engine) machineFiles(origin string, c URMachineConfig) (map[string][]byte, error) {
	prefix, err := machineEgressPrefix(origin)
	if err != nil {
		return nil, err
	}
	body := "[Service]\nIPAddressAllow=\nIPAddressAllow=localhost\nIPAddressAllow=" + prefix + "\n"
	return map[string][]byte{filepath.Join(e.Paths.ControllerServiceUnit+".d", machineDropIn): []byte(body)}, nil
}

func machineEgressPrefix(origin string) (string, error) {
	u, err := url.Parse(origin)
	if err != nil {
		return "", err
	}
	ip, err := netip.ParseAddr(u.Hostname())
	if err != nil || ip.IsUnspecified() || ip.IsMulticast() || ip.Is4In6() {
		return "", fmt.Errorf("managed machine egress requires one exact IP origin; hostname egress is not supported by this command")
	}
	return netip.PrefixFrom(ip, ip.BitLen()).String(), nil
}

func (e *Engine) ValidateURMachineConfig() error {
	_, present, err := readConnectorTargetEnvironmentFileIfPresent(e.Paths.ConnectorEnvironment)
	if err != nil {
		return err
	}
	path := filepath.Join(e.Paths.ControllerServiceUnit+".d", machineDropIn)
	if !present {
		if exists, err := fileExists(path); err != nil {
			return err
		} else if exists {
			return e.validatePendingURMachineDropIn(path)
		}
		return nil
	}
	if err := e.validateRootReference(e.Paths.ConnectorEnvironment, 0600); err != nil {
		return err
	}
	v, err := readAllowlistedEnvironmentFile(e.Paths.ConnectorEnvironment, true, connectorTargetEnvironmentKeys)
	if err != nil {
		return err
	}
	c, enabled, err := parseURMachineConfig(v)
	if err != nil {
		return err
	}
	if !enabled {
		if exists, err := fileExists(path); err != nil {
			return err
		} else if exists {
			return e.validatePendingURMachineDropIn(path)
		}
		return nil
	}
	if err := e.validateMachineCandidate(c); err != nil {
		return err
	}
	files, err := e.machineFiles(v[connectorcontrol.UniRouterInstallOriginEnv], c)
	if err != nil {
		return err
	}
	return e.validateGeneratedReference(path, files[path])
}

func (e *Engine) validatePendingURMachineDropIn(path string) error {
	pending, staged, err := e.readURMachineRuntimePending()
	if err != nil || !staged {
		return fmt.Errorf("machine drop-in has no configuration authority")
	}
	if err := e.validateRootReference(path, 0644); err != nil {
		return err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(body)
	if hex.EncodeToString(digest[:]) != pending.DropInSHA256 {
		return fmt.Errorf("pending machine drop-in differs from its exact authority")
	}
	return nil
}

func (e *Engine) SetURMachineConfig(c URMachineConfig, check bool) error {
	return e.setURMachineConfig(context.Background(), c, check, false)
}

// The running controller must prove a fresh systemd invocation before using
// a newly published machine binding. The pending proof precedes both files so
// an interruption between their atomic publications remains recoverable.
func (e *Engine) SetURMachineConfigForController(ctx context.Context, c URMachineConfig) error {
	return e.setURMachineConfig(ctx, c, false, true)
}

func (e *Engine) setURMachineConfig(ctx context.Context, c URMachineConfig, check, controller bool) error {
	if err := requireMutationAuthority(e.Paths); err != nil {
		return err
	}
	run := func() error {
		if err := e.ensureNoOutstandingOperation(); err != nil {
			return err
		}
		origin, err := readConnectorTargetEnvironmentFile(e.Paths.ConnectorEnvironment)
		if err != nil {
			return err
		}
		if err := e.validateConnectorBaseConfiguration(false); err != nil {
			return err
		}
		v, err := readAllowlistedEnvironmentFile(e.Paths.ConnectorEnvironment, true, connectorTargetEnvironmentKeys)
		if err != nil {
			return err
		}
		for key, value := range machineConfigValues(c) {
			if value == "" {
				continue
			}
			if v[key] != "" && v[key] != value {
				return fmt.Errorf("active machine binding forbids implicit identity or credential replacement")
			}
			v[key] = value
		}
		parsed, enabled, err := parseURMachineConfig(v)
		if err != nil || !enabled {
			return fmt.Errorf("machine configuration is incomplete or invalid")
		}
		if err := e.validateMachineCandidate(parsed); err != nil {
			return err
		}
		files, err := e.machineFiles(origin, parsed)
		if err != nil {
			return err
		}
		if err := e.requireUnitDirective(e.Paths.ControllerServiceUnit, machineStartup); err != nil {
			return err
		}
		if err := e.checkReferenceCandidates(files, nil); err != nil {
			return err
		}
		body := connectorTargetEnvironmentContents(origin)
		for _, key := range machineConfigKeys() {
			if v[key] != "" {
				body = append(body, []byte(key+"="+v[key]+"\n")...)
			}
		}
		if err := e.checkReferenceStaging(e.Paths.ConnectorEnvironment, body, 0600); err != nil {
			return err
		}
		if check {
			return nil
		}
		if controller {
			if err := e.stageURMachineRuntime(ctx, parsed, files); err != nil {
				return err
			}
		}
		for path, body := range files {
			if err := e.publishReference(path, body, 0644); err != nil {
				return err
			}
		}
		if err := e.publishReference(e.Paths.ConnectorEnvironment, body, 0600); err != nil {
			return err
		}
		return e.ValidateURMachineConfig()
	}
	if check {
		return run()
	}
	return e.withOperationLock(run)
}

func machineConfigKeys() []string {
	return []string{config.URMachineInstallationEnv, config.URMachineSourceEnv, config.URMachineOrgEnv, config.URPolicyFileEnv, config.UREnrollFileEnv, config.URMachineCAEnv}
}
func machineConfigValues(c URMachineConfig) map[string]string {
	return map[string]string{config.URMachineInstallationEnv: c.InstallationID, config.URMachineSourceEnv: c.SourceInstance, config.URMachineOrgEnv: c.OrgID, config.URPolicyFileEnv: c.PolicyFile, config.UREnrollFileEnv: c.EnrollFile, config.URMachineCAEnv: c.CAFile}
}

func (e *Engine) DisableURMachineConfig(check bool) error {
	if err := requireMutationAuthority(e.Paths); err != nil {
		return err
	}
	run := func() error {
		if err := e.ensureNoOutstandingOperation(); err != nil {
			return err
		}
		origin, err := readConnectorTargetEnvironmentFile(e.Paths.ConnectorEnvironment)
		if err != nil {
			return err
		}
		v, err := readAllowlistedEnvironmentFile(e.Paths.ConnectorEnvironment, true, connectorTargetEnvironmentKeys)
		if err != nil {
			return err
		}
		c, enabled, err := parseURMachineConfig(v)
		if err != nil {
			return err
		}
		if !enabled {
			if err := e.ValidateURMachineConfig(); err != nil {
				return err
			}
			// A publication may stop after its exact drop-in, before the env
			// binding. Retire that validated file before removing its authority.
			path := filepath.Join(e.Paths.ControllerServiceUnit+".d", machineDropIn)
			present, err := fileExists(path)
			if err != nil {
				return err
			}
			if present {
				body, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if retired, err := fileExists(path + ".disabled"); err != nil {
					return err
				} else if retired {
					if err := e.validateGeneratedReference(path+".disabled", body); err != nil {
						return err
					}
				}
			}
			if check {
				return nil
			}
			if present {
				if err := e.retireReference(path, 0644); err != nil {
					return err
				}
			}
			if err := e.clearURMachineRuntimePending(); err != nil {
				return err
			}
			return e.ValidateURMachineConfig()
		}
		if err := e.validateMachineCandidate(c); err != nil {
			return err
		}
		files, err := e.machineFiles(origin, c)
		if err != nil {
			return err
		}
		if err := e.checkReferenceCandidates(files, nil); err != nil {
			return err
		}
		body, err := os.ReadFile(e.Paths.ConnectorEnvironment)
		if err != nil {
			return err
		}
		backup := e.Paths.ConnectorEnvironment + ".machine-disabled"
		if exists, err := fileExists(backup); err != nil {
			return err
		} else if exists {
			if err := e.validateRootReference(backup, 0600); err != nil {
				return err
			}
			prior, err := os.ReadFile(backup)
			if err != nil || !bytes.Equal(prior, body) {
				return fmt.Errorf("different retired machine configuration exists")
			}
		}
		if err := e.checkReferenceStaging(backup, body, 0600); err != nil {
			return err
		}
		if err := e.checkReferenceStaging(e.Paths.ConnectorEnvironment, connectorTargetEnvironmentContents(origin), 0600); err != nil {
			return err
		}
		for path, expected := range files {
			if exists, err := fileExists(path + ".disabled"); err != nil {
				return err
			} else if exists {
				if err := e.validateGeneratedReference(path+".disabled", expected); err != nil {
					return err
				}
			}
		}
		if check {
			return nil
		}
		if err := e.publishReference(backup, body, 0600); err != nil {
			return err
		}
		for path := range files {
			if err := e.retireReference(path, 0644); err != nil {
				return err
			}
		}
		if err := e.publishReference(e.Paths.ConnectorEnvironment, connectorTargetEnvironmentContents(origin), 0600); err != nil {
			return err
		}
		if err := e.ValidateURMachineConfig(); err != nil {
			return err
		}
		return e.clearURMachineRuntimePending()
	}
	if check {
		return run()
	}
	return e.withOperationLock(run)
}

func (e *Engine) validateManagedConfigurationRelease(dir string) error {
	return e.validateManagedConfigurationTarget(func(name string) ([]byte, error) { return os.ReadFile(filepath.Join(dir, "deploy", name)) })
}

func (e *Engine) validateManagedConfigurationTarget(read func(string) ([]byte, error)) error {
	if err := e.ValidateReviewBodyConfig(); err != nil {
		return err
	}
	if err := e.ValidateURMachineConfig(); err != nil {
		return err
	}
	partitions, err := fileExists(sourcefacts.ManagedPartitionsDirectory(e.Paths.SourceFactsConfig))
	if err != nil {
		return err
	}
	if partitions {
		configs, err := e.loadManagedProjectPartitions()
		if err != nil {
			return err
		}
		if len(configs) > 0 {
			body, err := read("xinsight-source-facts.service")
			if err != nil || !bytes.Contains(body, []byte("Environment=XINSIGHT_SOURCE_MANAGED_PARTITIONS=xinsight.managed-source-partitions.v1\n")) {
				return fmt.Errorf("enabled managed source partitions are unsupported by the target release")
			}
		}
	}
	_, review, err := e.readReviewBodyConfig()
	if err != nil {
		return err
	}
	machine := false
	if present, err := fileExists(e.Paths.ConnectorEnvironment); err != nil {
		return err
	} else if present {
		v, err := readAllowlistedEnvironmentFile(e.Paths.ConnectorEnvironment, true, connectorTargetEnvironmentKeys)
		if err != nil {
			return err
		}
		_, machine, err = parseURMachineConfig(v)
		if err != nil {
			return err
		}
	}
	if review {
		for _, name := range []string{"xinsight.service", "xinsight-source-facts.service"} {
			body, err := read(name)
			if err != nil || !bytes.Contains(body, []byte(reviewBodyStartup+"\n")) || !bytes.Contains(body, []byte("EnvironmentFile=-/etc/x-insight/xinsight-review-body.env\n")) {
				return fmt.Errorf("enabled review custody is unsupported by the target release; disable explicitly before switching")
			}
		}
	}
	if machine {
		body, err := read("xinsight-connector-controller.service")
		if err != nil || !bytes.Contains(body, []byte(machineStartup+"\n")) || !bytes.Contains(body, []byte("EnvironmentFile=-/etc/x-insight/xinsight-connector.env\n")) {
			return fmt.Errorf("enabled UR machine configuration is unsupported by the target release; disable explicitly before switching")
		}
	}
	return nil
}
