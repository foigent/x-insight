package hostctl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"unicode/utf8"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/db"
)

func DecodeWorkbenchGrantChange(raw []byte) (db.WorkbenchGrantChange, error) {
	var r db.WorkbenchGrantChange
	if len(raw) == 0 || len(raw) > 16<<10 || !utf8.Valid(raw) {
		return r, fmt.Errorf("workbench grant request size or encoding invalid")
	}
	if err := rejectDuplicateAdminRotationJSONFields(raw); err != nil {
		return r, fmt.Errorf("invalid or duplicate workbench grant request field")
	}
	// encoding/json matches struct fields case-insensitively. Check decoded
	// object keys against the wire contract before permitting struct decoding;
	// aliases must not override an already checked authorization field.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return r, fmt.Errorf("workbench grant request must be an object")
	}
	for key := range fields {
		switch key {
		case "schema_version", "action", "grant_id", "subject_id", "source_system_id", "source_project_ref", "cost_project_ref", "expected_revision", "capabilities", "allow_content", "allowed_content_purposes":
		default:
			return r, fmt.Errorf("invalid or duplicate workbench grant request field")
		}
	}
	var version string
	_ = json.Unmarshal(fields["schema_version"], &version)
	if version != "xinsight.workbench-grant-change.v2" {
		if _, ok := fields["allow_content"]; ok {
			return r, fmt.Errorf("content fields require grant command v2")
		}
		if _, ok := fields["allowed_content_purposes"]; ok {
			return r, fmt.Errorf("content fields require grant command v2")
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return r, fmt.Errorf("invalid workbench grant request")
	}
	return r, r.Validate()
}

// Only host root can configure the existing grant authority. --check bypasses
// the filesystem operation lock and opens a read-only database transaction.
func (e *Engine) ManageWorkbenchGrant(ctx context.Context, path string, check bool) (db.WorkbenchGrantResult, error) {
	var result db.WorkbenchGrantResult
	if err := requireMutationAuthority(e.Paths); err != nil {
		return result, err
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return result, fmt.Errorf("grant file must be an absolute clean path")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return result, fmt.Errorf("cannot open grant request")
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return result, err
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || stat.Nlink != 1 || (!e.Paths.IsTestRoot && stat.Uid != 0) || st.Size() > 16<<10 {
		return result, fmt.Errorf("grant request must be root-owned regular single-link 0600 and at most 16KiB")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 16<<10+1))
	if err != nil {
		return result, err
	}
	r, err := DecodeWorkbenchGrantChange(raw)
	if err != nil {
		return result, err
	}
	run := func() error {
		// Revocation remains available after an account was removed or disabled.
		if r.Action != "revoke" {
			config, err := access.ReadAndValidateAccountFile(e.Paths.AccessAccountsFile)
			if err != nil {
				return err
			}
			found := false
			for _, a := range config.Accounts {
				if a.SubjectID == r.Subject && !a.Disabled {
					for _, p := range access.Permissions(a.Role) {
						if p == access.PermissionWorkbenchSummary {
							found = true
						}
					}
				}
			}
			if !found {
				return fmt.Errorf("subject is not an enabled configured workbench account")
			}
		}
		credentials, err := LoadDatabaseCredentials(e.Paths)
		if err != nil {
			return err
		}
		store, err := db.OpenSourceFactsStore(credentials.AdminDSN)
		if err != nil {
			return err
		}
		defer store.Close()
		result, err = store.ChangeWorkbenchGrant(ctx, r, check)
		return err
	}
	if check {
		err = run()
		return result, err
	}
	err = e.withOperationLock(run)
	return result, err
}

func (e *Engine) InspectWorkbenchGrant(ctx context.Context, id string) (json.RawMessage, error) {
	if err := requireMutationAuthority(e.Paths); err != nil {
		return nil, err
	}
	credentials, err := LoadDatabaseCredentials(e.Paths)
	if err != nil {
		return nil, err
	}
	store, err := db.OpenSourceFactsStore(credentials.AdminDSN)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.InspectWorkbenchGrant(ctx, id)
}
