package sourcefacts

import (
	"os"
	"path/filepath"
	"regexp"
	"syscall"
)

var managedPartitionName = regexp.MustCompile(`^xci_[A-Za-z0-9_-]{22}-xise_[1-9][0-9]{0,18}-project-[1-9][0-9]{0,18}\.json$`)

func ManagedPartitionsDirectory(base string) string {
	return filepath.Join(filepath.Dir(base), "source-facts.d")
}
func ValidManagedPartitionName(name string) bool { return managedPartitionName.MatchString(name) }

// Each project retains the existing configuration schema and hard maximum.
// The importer loads the partitions into its existing page rotation loop.
func LoadManagedPartitions(base string) ([]ManagedConfig, error) {
	if !filepath.IsAbs(base) || filepath.Clean(base) != base {
		return nil, ErrInvalid
	}
	dir := ManagedPartitionsDirectory(base)
	info, e := os.Lstat(dir)
	if os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
		return nil, ErrAuthorization
	}
	files, e := os.ReadDir(dir)
	if e != nil {
		return nil, e
	}
	out := []ManagedConfig{}
	for _, f := range files {
		if !ValidManagedPartitionName(f.Name()) {
			continue
		}
		if !f.Type().IsRegular() {
			return nil, ErrAuthorization
		}
		cfg, e := LoadManagedConfig(filepath.Join(dir, f.Name()))
		if e != nil {
			return nil, e
		}
		out = append(out, cfg)
	}
	return out, nil
}

func ManagedCredentialName(path string) (string, error) {
	name := filepath.Base(path)
	if !regexp.MustCompile(`^source-managed-[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}\.key$`).MatchString(name) {
		return "", ErrInvalid
	}
	return name, nil
}

// ManagedProjectConfig preserves the original project's serialization order and
// the existing independent facts/content purpose selection. It reads no keys.
func ManagedProjectConfig(base ManagedConfig, source, org, project string, facts, content bool) ManagedConfig {
	cfg := base
	cfg.Profiles = nil
	cfg.Budgets = nil
	cfg.Contents = nil
	cfg.Bindings = nil
	if facts {
		for _, p := range base.Profiles {
			if p.SourceInstance == source && p.OrgID == org && p.ProjectID == project {
				cfg.Profiles = append(cfg.Profiles, p)
			}
		}
		for _, p := range base.Budgets {
			if p.SourceInstance == source && p.OrgID == org && p.ProjectID == project {
				cfg.Budgets = append(cfg.Budgets, p)
			}
		}
	}
	if content {
		for _, p := range base.Contents {
			if p.SourceInstance == source && p.OrgID == org && p.ProjectID == project {
				cfg.Contents = append(cfg.Contents, p)
			}
		}
	}
	if len(cfg.Contents) > 0 {
		for _, p := range base.Bindings {
			if p.SourceInstance == source && p.OrgID == org && p.ProjectID == project {
				cfg.Bindings = append(cfg.Bindings, p)
			}
		}
	} else {
		cfg.SchemaVersion = ManagedObservationConfigVersion
		if len(cfg.Budgets) > 0 {
			cfg.SchemaVersion = ManagedBudgetConfigVersion
		}
	}
	return cfg
}
