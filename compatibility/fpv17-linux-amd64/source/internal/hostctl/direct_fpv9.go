package hostctl

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/releasebundle"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

// This is a one-time, exact-source bridge. The old direct-run build has no
// release signature or managed install state; neither is synthesized here.
const (
	directFPV9Commit       = "8ee764accbda21f21b489e01a4ab7751571beed4"
	directFPV9LedgerSHA256 = "sha256:ea8192279484417476268590847a37c5d25f1b07d68590bfa9d37c1cff11056b"
	directFPV9ServerSHA256 = "sha256:80573c95149f5b9827b06f264f8d70e436a29ea1d8e93ddb6f1f46a75f4866d7"
	directFPV9WorkerSHA256 = "sha256:c6b71ded9aa3fac35c8099aba222e13edb8772a77616f6ab3bb53094cbbcfda8"
	directFPV9Kind         = "unmanaged_direct_fpv9"
)

type DirectFPV9SourceProof struct {
	SchemaVersion    string            `json:"schema_version"`
	ProductVersion   string            `json:"product_version"`
	Commit           string            `json:"commit"`
	DatabaseName     string            `json:"database_name"`
	SystemIdentifier string            `json:"system_identifier"`
	EgressAllowCIDR  string            `json:"egress_allow_cidr"`
	ServerBinary     string            `json:"server_binary"`
	WorkerBinary     string            `json:"worker_binary"`
	Files            map[string]string `json:"files"`
}

// Exact role names prevent a proof from silently omitting a service override.
// File SHA-256 values are reviewed input, never executable instructions.
var directFPV9FileRoles = []string{
	"service_unit", "feed_unit", "source_facts_unit",
	"service_content_dropin", "service_delivery_dropin",
	"feed_delivery_dropin", "source_facts_credentials_dropin", "source_facts_delivery_dropin",
	"source_facts_config",
}

func directFPV9Paths(paths Paths) map[string]string {
	return map[string]string{
		"service_unit":                    paths.ServiceUnit,
		"feed_unit":                       paths.FeedServiceUnit,
		"source_facts_unit":               paths.SourceFactsServiceUnit,
		"service_content_dropin":          filepath.Join(paths.ServiceDropInDir, "30-content-credentials.conf"),
		"service_delivery_dropin":         filepath.Join(paths.ServiceDropInDir, "50-github-program-20260927.conf"),
		"feed_delivery_dropin":            filepath.Join(filepath.Dir(paths.FeedServiceUnit), "xinsight-feed.service.d", "50-github-program-20260927.conf"),
		"source_facts_credentials_dropin": filepath.Join(filepath.Dir(paths.SourceFactsServiceUnit), "xinsight-source-facts.service.d", "10-source-credentials.conf"),
		"source_facts_delivery_dropin":    filepath.Join(filepath.Dir(paths.SourceFactsServiceUnit), "xinsight-source-facts.service.d", "50-github-program-20260927.conf"),
		"source_facts_config":             paths.SourceFactsConfig,
	}
}

func readDirectFPV9Proof(path string) (DirectFPV9SourceProof, string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return DirectFPV9SourceProof{}, "", fmt.Errorf("source proof path must be absolute and clean")
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > maxControlFileBytes ||
		before.Mode().Perm()&0o077 != 0 || before.Mode().Perm()&0o200 == 0 {
		return DirectFPV9SourceProof{}, "", fmt.Errorf("source proof file metadata is invalid")
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || (int(stat.Uid) != 0 && int(stat.Uid) != os.Geteuid()) || stat.Nlink != 1 {
		return DirectFPV9SourceProof{}, "", fmt.Errorf("source proof file owner or link count is invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return DirectFPV9SourceProof{}, "", err
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return DirectFPV9SourceProof{}, "", fmt.Errorf("source proof file changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxControlFileBytes+1))
	if err != nil || int64(len(raw)) != before.Size() {
		return DirectFPV9SourceProof{}, "", fmt.Errorf("source proof file changed while reading")
	}
	if _, err := connector.DecodeStrictJSONObject(raw); err != nil {
		return DirectFPV9SourceProof{}, "", fmt.Errorf("source proof JSON is not strict: %w", err)
	}
	digestBytes := sha256.Sum256(raw)
	digest := "sha256:" + hex.EncodeToString(digestBytes[:])
	var proof DirectFPV9SourceProof
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proof); err != nil {
		return DirectFPV9SourceProof{}, "", err
	}
	if proof.SchemaVersion != "xinsight.unmanaged-fpv9-source.v1" ||
		proof.ProductVersion != "0.2.0-dev.fpv9" || proof.Commit != directFPV9Commit ||
		proof.DatabaseName == "" || proof.SystemIdentifier == "" {
		return DirectFPV9SourceProof{}, "", fmt.Errorf("unmanaged FPV9 source identity is not allowlisted")
	}
	if _, err := strconv.ParseUint(proof.SystemIdentifier, 10, 64); err != nil {
		return DirectFPV9SourceProof{}, "", fmt.Errorf("source PostgreSQL system identifier is invalid")
	}
	egress, err := netip.ParsePrefix(proof.EgressAllowCIDR)
	if err != nil || !egress.IsValid() || egress != egress.Masked() || !egress.Addr().IsGlobalUnicast() ||
		(egress.Addr().Is4() && egress.Bits() != 32) || (egress.Addr().Is6() && egress.Bits() != 128) ||
		egress.Addr().IsPrivate() || egress.Addr().IsLoopback() || egress.Addr().IsUnspecified() {
		return DirectFPV9SourceProof{}, "", fmt.Errorf("source egress allow CIDR must be one exact public IP")
	}
	if len(proof.Files) != len(directFPV9FileRoles) {
		return DirectFPV9SourceProof{}, "", fmt.Errorf("source file proof must contain the exact known roles")
	}
	for _, role := range directFPV9FileRoles {
		if !sha256ReferencePattern.MatchString(proof.Files[role]) {
			return DirectFPV9SourceProof{}, "", fmt.Errorf("source file proof for %s is invalid", role)
		}
	}
	if !filepath.IsAbs(proof.ServerBinary) || filepath.Clean(proof.ServerBinary) != proof.ServerBinary ||
		!filepath.IsAbs(proof.WorkerBinary) || filepath.Clean(proof.WorkerBinary) != proof.WorkerBinary ||
		filepath.Base(proof.ServerBinary) != "xinsight-server" || filepath.Base(proof.WorkerBinary) != "xinsight-importer" ||
		filepath.Dir(proof.ServerBinary) != filepath.Dir(proof.WorkerBinary) {
		return DirectFPV9SourceProof{}, "", fmt.Errorf("source binary paths are invalid")
	}
	return proof, digest, nil
}

func directFPV9LedgerDigest(records []db.MigrationRecord) (string, error) {
	if len(records) != 40 {
		return "", fmt.Errorf("unmanaged FPV9 requires exactly 40 migration records")
	}
	h := sha256.New()
	for i, record := range records {
		if record.Dirty || len(record.ID) < 6 || record.ID[:6] != fmt.Sprintf("%06d", i+1) ||
			!sha256ReferencePattern.MatchString(record.SHA256) {
			return "", fmt.Errorf("unmanaged FPV9 migration ledger is invalid")
		}
		_, _ = h.Write([]byte(record.ID + "\n" + record.SHA256 + "\n"))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func (engine *Engine) verifyDirectFPV9Source(ctx context.Context, proof DirectFPV9SourceProof, facts DatabaseFacts, requireLive bool) error {
	if facts.PostgresMajor != 16 || facts.DatabaseName != proof.DatabaseName ||
		facts.SystemIdentifier != proof.SystemIdentifier || !facts.AdminRoleIsSuperuser ||
		!facts.ReaderRoleExists || !facts.WorkspaceRoleExists || !facts.ImporterRoleExists {
		return fmt.Errorf("unmanaged FPV9 PostgreSQL identity or role boundary changed")
	}
	ledgerDigest, err := directFPV9LedgerDigest(facts.MigrationLedger)
	if err != nil || ledgerDigest != directFPV9LedgerSHA256 {
		return fmt.Errorf("unmanaged FPV9 migration ledger does not match the closed source")
	}
	for path, expected := range map[string]string{
		proof.ServerBinary: directFPV9ServerSHA256,
		proof.WorkerBinary: directFPV9WorkerSHA256,
	} {
		actual, _, err := fileSHA256(path, 256<<20)
		if err != nil || actual != expected {
			return fmt.Errorf("unmanaged FPV9 binary identity changed")
		}
	}
	for role, path := range directFPV9Paths(engine.Paths) {
		actual, _, err := fileSHA256(path, 1<<20)
		if err != nil || actual != proof.Files[role] {
			return fmt.Errorf("unmanaged FPV9 %s identity changed", role)
		}
	}
	if err := engine.verifyDirectFPV9CredentialDropIns(proof.EgressAllowCIDR); err != nil {
		return err
	}
	if err := engine.verifyDirectFPV9Egress(proof); err != nil {
		return err
	}
	for _, unit := range []string{"xinsight.service", "xinsight-feed.service", "xinsight-source-facts.service"} {
		if err := verifyExactDirectDropIns(engine.Paths, unit); err != nil {
			return err
		}
	}
	if !requireLive {
		return nil
	}
	for _, service := range []struct{ unit, binary string }{
		{"xinsight.service", proof.ServerBinary},
		{"xinsight-feed.service", proof.WorkerBinary},
		{"xinsight-source-facts.service", proof.WorkerBinary},
	} {
		if err := engine.verifyDirectServiceProcess(ctx, service.unit, service.binary); err != nil {
			return err
		}
	}
	return nil
}

func (engine *Engine) verifyDirectFPV9CredentialDropIns(expectedCIDR string) error {
	config, err := loadDirectManagedSourceConfig(engine.Paths)
	if err != nil {
		return fmt.Errorf("unmanaged source-facts configuration is invalid: %w", err)
	}
	address, err := directSourceEndpointIP(config)
	if err != nil {
		return err
	}
	if expectedCIDR != "" && netip.PrefixFrom(address, address.BitLen()).String() != expectedCIDR {
		return fmt.Errorf("reviewed egress IP differs from source endpoint")
	}
	workerExpected := "[Service]\nLoadCredential=\nLoadCredential=postgres-importer.dsn:" + engine.Paths.ImporterDSNFile + "\n"
	for i, keyFile := range config.CredentialFiles() {
		if strings.ContainsAny(keyFile, "\\\"%\r\n") {
			return fmt.Errorf("unmanaged source-facts credential path is invalid")
		}
		workerExpected += fmt.Sprintf("LoadCredential=source-profile-%d.key:%s\n", i, keyFile)
	}
	if err := validateExactServiceDropIn(directFPV9Paths(engine.Paths)["source_facts_credentials_dropin"], workerExpected); err != nil {
		return fmt.Errorf("unmanaged source-facts credential references changed: %w", err)
	}
	contentExpected := "[Service]\nEnvironment=XINSIGHT_CONTENT_CONFIG=" + engine.Paths.SourceFactsConfig + "\n"
	for _, content := range config.Contents {
		resolved, err := config.ResolveContent(content)
		if err != nil {
			return err
		}
		contentExpected += "LoadCredential=source-content-" + content.CollectionID + ".key:" + resolved.KeyFile + "\n"
	}
	if err := validateExactServiceDropIn(directFPV9Paths(engine.Paths)["service_content_dropin"], contentExpected); err != nil {
		return fmt.Errorf("unmanaged content credential references changed: %w", err)
	}
	return nil
}

func loadDirectManagedSourceConfig(paths Paths) (sourcefacts.ManagedConfig, error) {
	if !paths.IsTestRoot {
		return sourcefacts.LoadManagedConfig(paths.SourceFactsConfig)
	}
	// The existing sourcefacts loader correctly requires uid 0 on a real host.
	// Isolated test roots are owned by the local test process instead.
	info, err := os.Lstat(paths.SourceFactsConfig)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 || info.Size() <= 0 || info.Size() > 65536 {
		return sourcefacts.ManagedConfig{}, fmt.Errorf("test source configuration metadata is invalid")
	}
	raw, err := os.ReadFile(paths.SourceFactsConfig)
	if err != nil {
		return sourcefacts.ManagedConfig{}, err
	}
	return sourcefacts.ParseManagedConfig(raw)
}

func (engine *Engine) verifyDirectFPV9ProxyEnvironment() error {
	raw, err := os.ReadFile(engine.Paths.ServiceUnit)
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		return fmt.Errorf("old service proxy environment cannot be inspected")
	}
	inline := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		for _, name := range []string{api.TrustedTLSProxyModeEnv, api.ExternalOriginEnv} {
			prefix := "Environment=" + name + "="
			if strings.HasPrefix(line, prefix) {
				if _, exists := inline[name]; exists {
					return fmt.Errorf("old service proxy environment is ambiguous")
				}
				inline[name] = strings.TrimPrefix(line, prefix)
			}
		}
	}
	if len(inline) != 2 {
		return fmt.Errorf("old service proxy environment is incomplete")
	}
	if present, err := validateTrustedTLSProxyEnvironmentFileIfPresent(engine.Paths.TrustedTLSProxyEnv); err != nil || !present {
		return fmt.Errorf("managed trusted TLS proxy environment must be provisioned before direct upgrade")
	}
	managed, err := readAllowlistedEnvironmentFile(engine.Paths.TrustedTLSProxyEnv, false, trustedTLSProxyEnvironmentKeys)
	if err != nil || managed[api.TrustedTLSProxyModeEnv] != inline[api.TrustedTLSProxyModeEnv] ||
		managed[api.ExternalOriginEnv] != inline[api.ExternalOriginEnv] {
		return fmt.Errorf("managed trusted TLS proxy environment differs from old service")
	}
	return nil
}

func directSourceEndpointIP(config sourcefacts.ManagedConfig) (netip.Addr, error) {
	var expected netip.Addr
	check := func(raw string) error {
		parsed, err := url.Parse(raw)
		if err != nil {
			return err
		}
		address, err := netip.ParseAddr(parsed.Hostname())
		if err != nil || !address.IsGlobalUnicast() || address.IsPrivate() {
			return fmt.Errorf("direct FPV9 source endpoint is not one public IP")
		}
		if expected.IsValid() && address != expected {
			return fmt.Errorf("direct FPV9 source endpoints span multiple public IPs")
		}
		expected = address
		return nil
	}
	for _, profile := range config.Profiles {
		if err := check(profile.Endpoint); err != nil {
			return netip.Addr{}, err
		}
	}
	for _, content := range config.Contents {
		if err := check(content.Endpoint); err != nil {
			return netip.Addr{}, err
		}
	}
	if !expected.IsValid() {
		return netip.Addr{}, fmt.Errorf("direct FPV9 source endpoints are missing")
	}
	return expected, nil
}

func verifyExactDirectDropIns(paths Paths, unit string) error {
	var dir string
	var expected map[string]bool
	switch unit {
	case "xinsight.service":
		dir = paths.ServiceDropInDir
		expected = map[string]bool{"30-content-credentials.conf": true, "50-github-program-20260927.conf": true}
	case "xinsight-feed.service":
		dir = filepath.Join(filepath.Dir(paths.FeedServiceUnit), unit+".d")
		expected = map[string]bool{"50-github-program-20260927.conf": true}
	case "xinsight-source-facts.service":
		dir = filepath.Join(filepath.Dir(paths.SourceFactsServiceUnit), unit+".d")
		expected = map[string]bool{"10-source-credentials.conf": true, "50-github-program-20260927.conf": true}
	default:
		return fmt.Errorf("unknown unmanaged service")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".conf") && !expected[entry.Name()] {
			return fmt.Errorf("unknown active unmanaged drop-in for %s", unit)
		}
	}
	return nil
}

func (engine *Engine) verifyDirectServiceProcess(ctx context.Context, unit string, binary string) error {
	paths := directFPV9Paths(engine.Paths)
	var fragment string
	var dropIns []string
	switch unit {
	case "xinsight.service":
		fragment = paths["service_unit"]
		dropIns = []string{paths["service_content_dropin"], paths["service_delivery_dropin"]}
	case "xinsight-feed.service":
		fragment = paths["feed_unit"]
		dropIns = []string{paths["feed_delivery_dropin"]}
	case "xinsight-source-facts.service":
		fragment = paths["source_facts_unit"]
		dropIns = []string{paths["source_facts_credentials_dropin"], paths["source_facts_delivery_dropin"]}
	default:
		return fmt.Errorf("unknown direct FPV9 service")
	}
	for property, expected := range map[string]string{
		"FragmentPath": fragment, "DropInPaths": strings.Join(dropIns, " "), "UnitFileState": "enabled",
	} {
		actual, err := engine.systemdUnitProperty(ctx, unit, property)
		if err != nil || actual != expected {
			return fmt.Errorf("unmanaged service %s %s changed", unit, property)
		}
	}
	active, err := engine.Runner.Run(ctx, Command{Name: "systemctl", Args: []string{"show", unit, "--property", "ActiveState", "--value"}})
	if err != nil || strings.TrimSpace(active.Output) != "active" {
		return fmt.Errorf("unmanaged service %s is not active", unit)
	}
	pidResult, err := engine.Runner.Run(ctx, Command{Name: "systemctl", Args: []string{"show", unit, "--property", "MainPID", "--value"}})
	if err != nil {
		return fmt.Errorf("unmanaged service %s PID is unavailable", unit)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(pidResult.Output))
	if err != nil || pid <= 1 {
		return fmt.Errorf("unmanaged service %s PID is invalid", unit)
	}
	procRoot := "/proc"
	if engine.Paths.IsTestRoot {
		procRoot = filepath.Join(engine.Paths.Root, "proc")
	}
	exe, err := os.Readlink(filepath.Join(procRoot, strconv.Itoa(pid), "exe"))
	if err != nil || exe != binary {
		return fmt.Errorf("unmanaged service %s executable changed", unit)
	}
	return nil
}

func directFPV9RetiredPath(path string, operationID string) string {
	return path + ".retired-by-" + operationID
}

func directFPV9DropInRoles() []string {
	return []string{"service_delivery_dropin", "feed_delivery_dropin", "source_facts_delivery_dropin"}
}

func managedDirectEgressPath(oldPath string) string {
	return filepath.Join(filepath.Dir(oldPath), "40-xinsight-reviewed-source-egress.conf")
}

func managedDirectEgressContent(cidr string) string {
	return "[Service]\nIPAddressAllow=\nIPAddressAllow=localhost\nIPAddressAllow=" + cidr + "\n"
}

func verifyOldDirectEgress(path string, expectedCIDR string) error {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		return fmt.Errorf("old program override cannot be inspected")
	}
	count := 0
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found || key != "IPAddressAllow" || value == "" {
			continue
		}
		var address netip.Addr
		if prefix, err := netip.ParsePrefix(value); err == nil {
			if prefix.Bits() != prefix.Addr().BitLen() {
				return fmt.Errorf("old program override permits a broad network")
			}
			address = prefix.Addr()
		} else {
			address, err = netip.ParseAddr(value)
			if err != nil {
				return fmt.Errorf("old program override has unknown egress")
			}
		}
		if netip.PrefixFrom(address, address.BitLen()).String() != expectedCIDR {
			return fmt.Errorf("old program override egress differs from reviewed endpoint")
		}
		count++
	}
	if count != 1 {
		return fmt.Errorf("old program override needs one exact reviewed egress IP")
	}
	return nil
}

func (engine *Engine) verifyDirectFPV9Egress(proof DirectFPV9SourceProof) error {
	paths := directFPV9Paths(engine.Paths)
	for _, role := range directFPV9DropInRoles() {
		if err := verifyOldDirectEgress(paths[role], proof.EgressAllowCIDR); err != nil {
			return fmt.Errorf("%s: %w", role, err)
		}
		if exists, err := fileExists(managedDirectEgressPath(paths[role])); err != nil || exists {
			return fmt.Errorf("reviewed managed egress drop-in already exists")
		}
	}
	return nil
}

func validateDirectManagedEgressDropIns(paths Paths) error {
	config, err := loadDirectManagedSourceConfig(paths)
	if err != nil {
		return err
	}
	address, err := directSourceEndpointIP(config)
	if err != nil {
		return err
	}
	expected := managedDirectEgressContent(netip.PrefixFrom(address, address.BitLen()).String())
	for _, role := range directFPV9DropInRoles() {
		path := managedDirectEgressPath(directFPV9Paths(paths)[role])
		if err := validateExactServiceDropIn(path, expected); err != nil {
			return fmt.Errorf("reviewed managed egress %s changed: %w", role, err)
		}
	}
	return nil
}

func (engine *Engine) retireDirectFPV9DropIns(operationID string, proof DirectFPV9SourceProof) error {
	paths := directFPV9Paths(engine.Paths)
	for _, role := range directFPV9DropInRoles() {
		path := paths[role]
		retired := directFPV9RetiredPath(path, operationID)
		if exists, err := fileExists(retired); err != nil {
			return err
		} else if exists {
			if digest, _, err := fileSHA256(retired, 1<<20); err != nil || digest != proof.Files[role] {
				return fmt.Errorf("retired FPV9 drop-in %s changed", role)
			}
			if exists, err := fileExists(path); err != nil || exists {
				return fmt.Errorf("old FPV9 drop-in %s was reactivated", role)
			}
		} else {
			if digest, _, err := fileSHA256(path, 1<<20); err != nil || digest != proof.Files[role] {
				return fmt.Errorf("FPV9 drop-in %s changed before retirement", role)
			}
			if err := verifyOldDirectEgress(path, proof.EgressAllowCIDR); err != nil {
				return err
			}
			if err := os.Rename(path, retired); err != nil {
				return err
			}
			if err := syncDirectory(filepath.Dir(path)); err != nil {
				return err
			}
		}
		egressPath := managedDirectEgressPath(path)
		if exists, err := fileExists(egressPath); err != nil {
			return err
		} else if exists {
			if err := validateExactServiceDropIn(egressPath, managedDirectEgressContent(proof.EgressAllowCIDR)); err != nil {
				return err
			}
		} else if err := writeSourceFactsConfiguration(egressPath, []byte(managedDirectEgressContent(proof.EgressAllowCIDR))); err != nil {
			return err
		}
	}
	return nil
}

func (engine *Engine) restoreDirectFPV9DropIns(operationID string, proof DirectFPV9SourceProof) error {
	paths := directFPV9Paths(engine.Paths)
	for _, role := range directFPV9DropInRoles() {
		path := paths[role]
		retired := directFPV9RetiredPath(path, operationID)
		egressPath := managedDirectEgressPath(path)
		if exists, err := fileExists(egressPath); err != nil {
			return err
		} else if exists {
			if err := validateExactServiceDropIn(egressPath, managedDirectEgressContent(proof.EgressAllowCIDR)); err != nil {
				return err
			}
			if err := os.Remove(egressPath); err != nil {
				return err
			}
			if err := syncDirectory(filepath.Dir(egressPath)); err != nil {
				return err
			}
		}
		if exists, err := fileExists(retired); err != nil {
			return err
		} else if !exists {
			if digest, _, err := fileSHA256(path, 1<<20); err != nil || digest != proof.Files[role] {
				return fmt.Errorf("FPV9 drop-in %s cannot be restored", role)
			}
			continue
		}
		if exists, err := fileExists(path); err != nil || exists {
			return fmt.Errorf("FPV9 drop-in %s restoration collides with active file", role)
		}
		if digest, _, err := fileSHA256(retired, 1<<20); err != nil || digest != proof.Files[role] {
			return fmt.Errorf("retired FPV9 drop-in %s changed", role)
		}
		if err := os.Rename(retired, path); err != nil {
			return err
		}
		if err := syncDirectory(filepath.Dir(path)); err != nil {
			return err
		}
	}
	return nil
}

func (engine *Engine) resumeDirectFPV9UpgradePlan(ctx context.Context, journal Journal) (upgradePlan, error) {
	if journal.Kind != "upgrade_direct_fpv9" || journal.Before == nil || journal.Target == nil ||
		journal.Before.SourceKind != directFPV9Kind || journal.SourceProofSHA256 == "" {
		return upgradePlan{}, fmt.Errorf("direct FPV9 journal identity is incomplete")
	}
	if journal.BeforeEvidenceFingerprintSHA256 == "" &&
		(journal.Phase == "migrate_database" || containsString(journal.CompletedPhases, "migrate_database")) {
		return upgradePlan{}, fmt.Errorf("direct FPV9 evidence baseline is missing after migration began")
	}
	proof, digest, err := readDirectFPV9Proof(journal.SourceProofPath)
	if err != nil || digest != journal.SourceProofSHA256 {
		return upgradePlan{}, fmt.Errorf("direct FPV9 source proof changed")
	}
	if journal.Before.Commit != proof.Commit || journal.Before.ProductVersion != proof.ProductVersion ||
		journal.Before.ReleaseDir != filepath.Dir(proof.ServerBinary) {
		return upgradePlan{}, fmt.Errorf("direct FPV9 source identity changed")
	}
	target, err := engine.verifyTarget(journal.ArtifactPath)
	if err != nil || target.Identity != *journal.Target {
		return upgradePlan{}, fmt.Errorf("direct FPV9 target identity changed")
	}
	credentials, needsBridge, err := LoadUpgradeDatabaseCredentialSet(engine.Paths, journal.DatabaseSetBefore, false)
	if err != nil || needsBridge {
		return upgradePlan{}, fmt.Errorf("direct FPV9 credential set changed")
	}
	active, err := readAbsoluteSymlink(engine.Paths.DatabaseCurrentLink)
	if err != nil || active != journal.DatabaseSetBefore {
		return upgradePlan{}, fmt.Errorf("direct FPV9 active credential set changed")
	}
	facts, err := engine.Database.Inspect(ctx, credentials)
	if err != nil || facts.PostgresMajor != 16 || facts.DatabaseName != proof.DatabaseName ||
		facts.SystemIdentifier != proof.SystemIdentifier || !facts.AdminRoleIsSuperuser {
		return upgradePlan{}, fmt.Errorf("direct FPV9 database identity changed")
	}
	if len(facts.MigrationLedger) < 40 || len(facts.MigrationLedger) > len(target.Bundle.Manifest.MigrationChecksums) ||
		validateLedgerPrefix(facts.MigrationLedger, target.Bundle.Manifest.MigrationChecksums[:len(facts.MigrationLedger)], false) != nil {
		return upgradePlan{}, fmt.Errorf("direct FPV9 migration ledger changed")
	}
	for path, expected := range map[string]string{proof.ServerBinary: directFPV9ServerSHA256, proof.WorkerBinary: directFPV9WorkerSHA256} {
		if digest, _, err := fileSHA256(path, 256<<20); err != nil || digest != expected {
			return upgradePlan{}, fmt.Errorf("direct FPV9 source binary changed")
		}
	}
	for role, path := range directFPV9Paths(engine.Paths) {
		originalPath := path
		if containsString(directFPV9DropInRoles(), role) {
			if containsString(journal.CompletedPhases, "retire_direct_dropins") || journal.Phase == "retire_direct_dropins" {
				retired := directFPV9RetiredPath(path, journal.OperationID)
				if exists, err := fileExists(retired); err != nil {
					return upgradePlan{}, err
				} else if exists {
					path = retired
					egressPath := managedDirectEgressPath(originalPath)
					if present, err := fileExists(egressPath); err != nil {
						return upgradePlan{}, err
					} else if present {
						if err := validateExactServiceDropIn(egressPath, managedDirectEgressContent(proof.EgressAllowCIDR)); err != nil {
							return upgradePlan{}, err
						}
					} else if containsString(journal.CompletedPhases, "retire_direct_dropins") {
						return upgradePlan{}, fmt.Errorf("completed egress transfer is incomplete")
					}
				} else if containsString(journal.CompletedPhases, "retire_direct_dropins") {
					return upgradePlan{}, fmt.Errorf("completed drop-in retirement is incomplete")
				}
			}
		} else if (role == "service_unit" || role == "feed_unit" || role == "source_facts_unit") &&
			(containsString(journal.CompletedPhases, "install_runtime_files") || journal.Phase == "install_runtime_files") {
			backup := path + ".before-" + journal.OperationID
			if exists, err := fileExists(backup); err != nil {
				return upgradePlan{}, err
			} else if exists {
				path = backup
			} else if containsString(journal.CompletedPhases, "install_runtime_files") {
				return upgradePlan{}, fmt.Errorf("completed runtime installation lacks old unit backup")
			}
		}
		if digest, _, err := fileSHA256(path, 1<<20); err != nil || digest != proof.Files[role] {
			return upgradePlan{}, fmt.Errorf("direct FPV9 source file %s changed", role)
		}
	}
	if containsString(journal.CompletedPhases, "backup_database") {
		if journal.BackupPath == "" || journal.BackupSHA256 == "" {
			return upgradePlan{}, fmt.Errorf("direct FPV9 backup identity missing")
		}
		if _, err := engine.Database.VerifyBackup(ctx, journal.BackupPath, journal.BackupSHA256); err != nil {
			return upgradePlan{}, err
		}
	}
	if containsString(journal.CompletedPhases, "stage_release") {
		if err := releasebundle.VerifyExtractedPayload(target.Bundle, target.Identity.ReleaseDir); err != nil {
			return upgradePlan{}, err
		}
	}
	if containsString(journal.CompletedPhases, "write_install_state") {
		managed, err := engine.readInstallState()
		if err != nil || managed.Current != *journal.Target || managed.Previous != nil ||
			managed.LastOperationID != journal.OperationID || !managed.ManagedAt.Equal(journal.StartedAt) ||
			managed.DatabaseSchemaCurrent != journal.Target.DatabaseSchema {
			return upgradePlan{}, fmt.Errorf("direct FPV9 managed state changed after activation")
		}
		if err := validateDirectManagedEgressDropIns(engine.Paths); err != nil {
			return upgradePlan{}, err
		}
	}
	state := InstallState{Current: *journal.Before, ManagedAt: journal.StartedAt}
	return upgradePlan{State: state, DirectSource: &proof, Target: target, Credentials: credentials, Facts: facts}, nil
}
