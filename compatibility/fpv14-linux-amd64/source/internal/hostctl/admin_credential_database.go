package hostctl

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/foigent/x-insight/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/pbkdf2"
)

const (
	adminDatabaseRole              = "xinsight_admin"
	adminCredentialSetMaxFileBytes = int64(config.MaxPostgresDSNBytes + 2)
	postgresSCRAMIterations        = 4096
	adminSessionDrainAttempts      = 20
	adminSessionDrainInterval      = 25 * time.Millisecond
)

type AdminCredentialMemberSummary struct {
	RoleAlias      string `json:"role_alias"`
	IdentitySHA256 string `json:"identity_sha256"`
	ContentSHA256  string `json:"content_sha256"`
	SizeBytes      int64  `json:"size_bytes"`
	Mode           uint32 `json:"mode"`
	OwnerUID       uint32 `json:"owner_uid"`
	OwnerGID       uint32 `json:"owner_gid"`
	LinkCount      uint64 `json:"link_count"`
	Inherited      bool   `json:"inherited"`
}

type AdminCredentialSetSummary struct {
	SetRef              string                         `json:"set_ref"`
	MetadataSHA256      string                         `json:"metadata_sha256"`
	CanonicalSHA256     string                         `json:"canonical_sha256"`
	Members             []AdminCredentialMemberSummary `json:"members"`
	AdminRoleAlias      string                         `json:"admin_role_alias"`
	AdminIdentitySHA256 string                         `json:"admin_identity_sha256"`
}

type AdminDatabaseBoundary struct {
	PostgresMajor       int    `json:"postgres_major"`
	DatabaseSHA256      string `json:"database_sha256"`
	AdminRoleAlias      string `json:"admin_role_alias"`
	AdminIdentitySHA256 string `json:"admin_identity_sha256"`
	AdminSessions       int    `json:"admin_sessions"`
}

type AdminCredentialTransitionState string

const (
	AdminCredentialTransitionPredecessor AdminCredentialTransitionState = "predecessor_active"
	AdminCredentialTransitionSuccessor   AdminCredentialTransitionState = "successor_active"
)

type AdminCredentialTransitionProof struct {
	SuccessorFreshConnection bool `json:"successor_fresh_connection"`
	PredecessorAuthRejected  bool `json:"predecessor_auth_rejected"`
	ValidationSessionClosed  bool `json:"validation_session_closed"`
	AdminSessionsRemaining   int  `json:"admin_sessions_remaining"`
}

type AdminCredentialRotationDatabase interface {
	InspectAdminBoundary(context.Context, DatabaseCredentials) (AdminDatabaseBoundary, error)
	ProbeAdminCredentialTransition(context.Context, DatabaseCredentials, DatabaseCredentials) (AdminCredentialTransitionState, error)
	ChangeAdminVerifier(context.Context, DatabaseCredentials, DatabaseCredentials, io.Reader) error
	TerminateOtherAdminSessions(context.Context, DatabaseCredentials) (int, error)
	ValidateAdminCredentialTransition(context.Context, DatabaseCredentials, DatabaseCredentials) (AdminCredentialTransitionProof, error)
}

type credentialMemberSpec struct {
	role      string
	filename  string
	inherited bool
}

var adminCredentialMemberSpecs = []credentialMemberSpec{
	{role: "xinsight_reader", filename: "postgres-reader.dsn", inherited: true},
	{role: "xinsight_workspace", filename: "postgres-workspace.dsn", inherited: true},
	{role: "xinsight_importer", filename: "postgres-importer.dsn", inherited: true},
	{role: adminDatabaseRole, filename: "postgres-admin.dsn", inherited: false},
}

func InspectAdminCredentialSet(paths Paths, setRef string) (AdminCredentialSetSummary, error) {
	credentials, err := LoadDatabaseCredentialSet(paths, setRef)
	if err != nil {
		return AdminCredentialSetSummary{}, err
	}
	raw, err := readAdminCredentialSetBytes(paths, setRef)
	if err != nil {
		return AdminCredentialSetSummary{}, err
	}
	configs := map[string]*pgx.ConnConfig{
		"xinsight_reader": credentials.Reader, "xinsight_workspace": credentials.Workspace,
		"xinsight_importer": credentials.Importer, adminDatabaseRole: credentials.Admin,
	}
	members := make([]AdminCredentialMemberSummary, 0, len(adminCredentialMemberSpecs))
	metadata := sha256.New()
	canonical := sha256.New()
	adminIdentity := ""
	directoryInfo, err := os.Lstat(setRef)
	if err != nil {
		return AdminCredentialSetSummary{}, fmt.Errorf("inspect database credential set metadata")
	}
	directoryStat, ok := directoryInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return AdminCredentialSetSummary{}, fmt.Errorf("inspect database credential set ownership")
	}
	_, _ = fmt.Fprintf(metadata, "directory\x00%o\x00%d\x00%d\x00%d\n",
		directoryInfo.Mode().Perm(), directoryStat.Uid, directoryStat.Gid, directoryStat.Nlink)
	for _, spec := range adminCredentialMemberSpecs {
		contents := raw[spec.filename]
		path := filepath.Join(setRef, spec.filename)
		info, err := os.Lstat(path)
		if err != nil {
			return AdminCredentialSetSummary{}, fmt.Errorf("inspect database credential member metadata")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return AdminCredentialSetSummary{}, fmt.Errorf("inspect database credential member ownership")
		}
		identity, err := databaseCredentialIdentitySHA256(configs[spec.role])
		if err != nil {
			return AdminCredentialSetSummary{}, err
		}
		contentSHA256 := hashCleanBaselineBytes(contents)
		member := AdminCredentialMemberSummary{
			RoleAlias: spec.role, IdentitySHA256: identity, ContentSHA256: contentSHA256,
			SizeBytes: int64(len(contents)), Mode: uint32(info.Mode().Perm()),
			OwnerUID: stat.Uid, OwnerGID: stat.Gid, LinkCount: uint64(stat.Nlink), Inherited: spec.inherited,
		}
		members = append(members, member)
		_, _ = fmt.Fprintf(metadata, "%s\x00%d\x00%o\x00%d\x00%d\x00%d\n",
			spec.filename, len(contents), info.Mode().Perm(), stat.Uid, stat.Gid, stat.Nlink)
		_, _ = fmt.Fprintf(canonical, "%s\x00%s\x00%s\n", spec.role, identity, contentSHA256)
		if spec.role == adminDatabaseRole {
			adminIdentity = identity
		}
	}
	return AdminCredentialSetSummary{
		SetRef:          setRef,
		MetadataSHA256:  "sha256:" + hex.EncodeToString(metadata.Sum(nil)),
		CanonicalSHA256: "sha256:" + hex.EncodeToString(canonical.Sum(nil)),
		Members:         members, AdminRoleAlias: adminDatabaseRole, AdminIdentitySHA256: adminIdentity,
	}, nil
}

func StageAdminCredentialSet(
	paths Paths,
	current DatabaseCredentials,
	setRef string,
	entropy io.Reader,
) (DatabaseCredentials, AdminCredentialSetSummary, error) {
	if filepath.Dir(setRef) != paths.DatabaseSetsDir || !filepath.IsAbs(setRef) || filepath.Clean(setRef) != setRef ||
		setRef == current.SetRef {
		return DatabaseCredentials{}, AdminCredentialSetSummary{}, fmt.Errorf("admin successor credential set path is invalid")
	}
	if entropy == nil {
		entropy = rand.Reader
	}
	currentRaw, err := readAdminCredentialSetBytes(paths, current.SetRef)
	if err != nil {
		return DatabaseCredentials{}, AdminCredentialSetSummary{}, err
	}
	defer clearAdminCredentialRaw(currentRaw)
	if err := ensureDirectory(paths.DatabaseSetsDir, 0o700); err != nil {
		return DatabaseCredentials{}, AdminCredentialSetSummary{}, err
	}
	if _, err := os.Lstat(setRef); err == nil {
		return loadAndValidateAdminSuccessorCredentialSet(paths, current, currentRaw, setRef)
	} else if !errors.Is(err, os.ErrNotExist) {
		return DatabaseCredentials{}, AdminCredentialSetSummary{}, fmt.Errorf("admin successor credential set cannot be inspected")
	}

	stagingRef, err := createAdminCredentialStagingDirectory(paths, setRef, entropy)
	if err != nil {
		return DatabaseCredentials{}, AdminCredentialSetSummary{}, err
	}
	passwordBytes := make([]byte, 32)
	if _, err := io.ReadFull(entropy, passwordBytes); err != nil {
		return DatabaseCredentials{}, AdminCredentialSetSummary{}, fmt.Errorf("generate successor admin credential")
	}
	password := hex.EncodeToString(passwordBytes)
	clearBytes(passwordBytes)
	parsed, err := url.Parse(current.AdminDSN)
	if err != nil {
		return DatabaseCredentials{}, AdminCredentialSetSummary{}, fmt.Errorf("seed successor admin credential")
	}
	parsed.User = url.UserPassword(adminDatabaseRole, password)
	successorDSN, err := config.ValidatePostgresDSN(parsed.String())
	password = ""
	if err != nil {
		return DatabaseCredentials{}, AdminCredentialSetSummary{}, fmt.Errorf("generated successor admin credential is invalid")
	}
	values := map[string][]byte{
		"postgres-reader.dsn":    currentRaw["postgres-reader.dsn"],
		"postgres-workspace.dsn": currentRaw["postgres-workspace.dsn"],
		"postgres-importer.dsn":  currentRaw["postgres-importer.dsn"],
		"postgres-admin.dsn":     []byte(successorDSN + "\n"),
	}
	defer clearBytes(values["postgres-admin.dsn"])
	successorDSN = ""
	for _, spec := range adminCredentialMemberSpecs {
		if err := writeAdminCredentialStagingMember(paths, stagingRef, spec, values[spec.filename]); err != nil {
			return DatabaseCredentials{}, AdminCredentialSetSummary{}, err
		}
	}
	if err := syncDirectory(stagingRef); err != nil {
		return DatabaseCredentials{}, AdminCredentialSetSummary{}, fmt.Errorf("sync admin successor staging directory")
	}
	if err := maybeFailAdminCredentialStage(paths, "after_directory_sync"); err != nil {
		return DatabaseCredentials{}, AdminCredentialSetSummary{}, err
	}
	if _, _, err := loadAndValidateAdminSuccessorCredentialSet(paths, current, currentRaw, stagingRef); err != nil {
		return DatabaseCredentials{}, AdminCredentialSetSummary{}, fmt.Errorf("validate complete admin successor staging set: %w", err)
	}
	if err := maybeFailAdminCredentialStage(paths, "before_rename"); err != nil {
		return DatabaseCredentials{}, AdminCredentialSetSummary{}, err
	}
	if err := os.Rename(stagingRef, setRef); err != nil {
		return DatabaseCredentials{}, AdminCredentialSetSummary{}, fmt.Errorf("activate complete admin successor credential set")
	}
	if err := maybeFailAdminCredentialStage(paths, "after_rename"); err != nil {
		return DatabaseCredentials{}, AdminCredentialSetSummary{}, err
	}
	if err := syncDirectory(paths.DatabaseSetsDir); err != nil {
		return DatabaseCredentials{}, AdminCredentialSetSummary{}, fmt.Errorf("sync database credential sets directory")
	}
	if err := maybeFailAdminCredentialStage(paths, "after_parent_sync"); err != nil {
		return DatabaseCredentials{}, AdminCredentialSetSummary{}, err
	}
	return loadAndValidateAdminSuccessorCredentialSet(paths, current, currentRaw, setRef)
}

func createAdminCredentialStagingDirectory(paths Paths, setRef string, entropy io.Reader) (string, error) {
	for attempt := 0; attempt < 8; attempt++ {
		suffixBytes := make([]byte, 8)
		if _, err := io.ReadFull(entropy, suffixBytes); err != nil {
			return "", fmt.Errorf("generate admin successor staging identity")
		}
		suffix := hex.EncodeToString(suffixBytes)
		clearBytes(suffixBytes)
		stagingRef := setRef + ".staging-" + suffix
		if filepath.Dir(stagingRef) != paths.DatabaseSetsDir {
			return "", fmt.Errorf("admin successor staging path is invalid")
		}
		if err := os.Mkdir(stagingRef, 0o700); err == nil {
			if err := maybeFailAdminCredentialStage(paths, "after_staging_directory_create"); err != nil {
				return "", err
			}
			return stagingRef, nil
		} else if !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("create admin successor staging directory")
		}
	}
	return "", fmt.Errorf("unique admin successor staging directory cannot be created")
}

func writeAdminCredentialStagingMember(
	paths Paths,
	stagingRef string,
	spec credentialMemberSpec,
	contents []byte,
) error {
	path := filepath.Join(stagingRef, spec.filename)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create admin successor staging member")
	}
	if err := maybeFailAdminCredentialStage(paths, "after_"+spec.role+"_create"); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return fmt.Errorf("write admin successor staging member")
	}
	if err := maybeFailAdminCredentialStage(paths, "after_"+spec.role+"_write"); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync admin successor staging member")
	}
	if err := maybeFailAdminCredentialStage(paths, "after_"+spec.role+"_sync"); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close admin successor staging member")
	}
	return nil
}

func loadAndValidateAdminSuccessorCredentialSet(
	paths Paths,
	current DatabaseCredentials,
	currentRaw map[string][]byte,
	setRef string,
) (DatabaseCredentials, AdminCredentialSetSummary, error) {
	info, err := os.Lstat(setRef)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return DatabaseCredentials{}, AdminCredentialSetSummary{}, fmt.Errorf("admin successor credential set target is invalid")
	}
	entries, err := os.ReadDir(setRef)
	if err != nil || len(entries) != len(adminCredentialMemberSpecs) {
		return DatabaseCredentials{}, AdminCredentialSetSummary{}, fmt.Errorf("admin successor credential set membership is invalid")
	}
	for _, entry := range entries {
		known := false
		for _, spec := range adminCredentialMemberSpecs {
			if entry.Name() == spec.filename {
				known = true
				break
			}
		}
		if !known {
			return DatabaseCredentials{}, AdminCredentialSetSummary{}, fmt.Errorf("admin successor credential set contains an unknown member")
		}
	}
	credentials, err := LoadDatabaseCredentialSet(paths, setRef)
	if err != nil {
		return DatabaseCredentials{}, AdminCredentialSetSummary{}, err
	}
	stagedRaw, err := readAdminCredentialSetBytes(paths, setRef)
	if err != nil {
		return DatabaseCredentials{}, AdminCredentialSetSummary{}, err
	}
	defer clearAdminCredentialRaw(stagedRaw)
	for _, spec := range adminCredentialMemberSpecs[:3] {
		if !bytes.Equal(stagedRaw[spec.filename], currentRaw[spec.filename]) {
			return DatabaseCredentials{}, AdminCredentialSetSummary{}, fmt.Errorf("admin rotation changed an inherited database credential member")
		}
	}
	if credentials.Admin.Password == current.Admin.Password || !sameAdminCredentialIdentity(current.AdminDSN, credentials.AdminDSN) {
		return DatabaseCredentials{}, AdminCredentialSetSummary{}, fmt.Errorf("admin successor credential changed identity or reused its predecessor")
	}
	summary, err := InspectAdminCredentialSet(paths, setRef)
	return credentials, summary, err
}

func maybeFailAdminCredentialStage(paths Paths, point string) error {
	if paths.IsTestRoot && os.Getenv("XINSIGHTCTL_FAIL_ADMIN_CREDENTIAL_STAGE_AT") == point {
		return fmt.Errorf("injected admin credential stage failure at %s", point)
	}
	return nil
}

func readAdminCredentialSetBytes(paths Paths, setRef string) (map[string][]byte, error) {
	if !filepath.IsAbs(setRef) || filepath.Clean(setRef) != setRef || !pathWithin(paths.DatabaseSetsDir, setRef) || setRef == paths.DatabaseSetsDir {
		return nil, fmt.Errorf("database credential set path is invalid")
	}
	info, err := os.Lstat(setRef)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return nil, fmt.Errorf("database credential set directory metadata is invalid")
	}
	result := make(map[string][]byte, len(adminCredentialMemberSpecs))
	for _, spec := range adminCredentialMemberSpecs {
		contents, err := readCredentialMemberBytes(filepath.Join(setRef, spec.filename))
		if err != nil {
			return nil, err
		}
		result[spec.filename] = contents
	}
	return result, nil
}

func readCredentialMemberBytes(path string) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm() != 0o600 ||
		before.Size() <= 0 || before.Size() > adminCredentialSetMaxFileBytes {
		return nil, fmt.Errorf("database credential member metadata is invalid")
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || uint64(stat.Nlink) != 1 || (int(stat.Uid) != 0 && int(stat.Uid) != os.Geteuid()) {
		return nil, fmt.Errorf("database credential member ownership is invalid")
	}
	descriptor, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open database credential member safely")
	}
	file := os.NewFile(uintptr(descriptor), filepath.Base(path))
	if file == nil {
		_ = syscall.Close(descriptor)
		return nil, fmt.Errorf("open database credential member handle")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return nil, fmt.Errorf("database credential member changed while opening")
	}
	contents, err := io.ReadAll(io.LimitReader(file, adminCredentialSetMaxFileBytes+1))
	if err != nil || int64(len(contents)) > adminCredentialSetMaxFileBytes {
		return nil, fmt.Errorf("read database credential member safely")
	}
	final, err := file.Stat()
	if err != nil || !os.SameFile(before, final) || final.Size() != before.Size() ||
		!final.ModTime().Equal(before.ModTime()) {
		return nil, fmt.Errorf("database credential member changed while reading")
	}
	return contents, nil
}

func databaseCredentialIdentitySHA256(connection *pgx.ConnConfig) (string, error) {
	if connection == nil {
		return "", fmt.Errorf("database credential member identity is missing")
	}
	identity := struct {
		Host            string `json:"host"`
		Port            uint16 `json:"port"`
		Database        string `json:"database"`
		User            string `json:"user"`
		ConnectTimeout  int64  `json:"connect_timeout_nanoseconds"`
		ApplicationName string `json:"application_name"`
	}{
		Host: connection.Host, Port: connection.Port, Database: connection.Database,
		User: connection.User, ConnectTimeout: int64(connection.ConnectTimeout),
		ApplicationName: connection.RuntimeParams["application_name"],
	}
	payload, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	return hashCleanBaselineBytes(payload), nil
}

func sameAdminCredentialIdentity(predecessor string, successor string) bool {
	left, leftErr := url.Parse(predecessor)
	right, rightErr := url.Parse(successor)
	if leftErr != nil || rightErr != nil || left.User == nil || right.User == nil ||
		left.User.Username() != adminDatabaseRole || right.User.Username() != adminDatabaseRole {
		return false
	}
	left.User = url.User(adminDatabaseRole)
	right.User = url.User(adminDatabaseRole)
	return left.String() == right.String()
}

func (manager PostgresManager) InspectAdminBoundary(
	ctx context.Context,
	credentials DatabaseCredentials,
) (AdminDatabaseBoundary, error) {
	if err := waitForAdminSessionDrain(ctx, credentials.ReaderDSN); err != nil {
		return AdminDatabaseBoundary{}, err
	}
	// Observe the admin boundary through the restricted reader. The observer
	// must not itself consume an xinsight_admin session, otherwise an exact
	// zero-session preflight can race PostgreSQL backend teardown from a prior
	// controlled probe and misclassify its own connection as external work.
	database, err := sql.Open("pgx", credentials.ReaderDSN)
	if err != nil {
		return AdminDatabaseBoundary{}, errors.New("admin database boundary cannot be opened")
	}
	defer database.Close()
	database.SetMaxIdleConns(0)
	var (
		versionNumber int
		databaseName  string
		observerRole  string
		roleName      string
		superuser     bool
		canLogin      bool
		createDB      bool
		createRole    bool
		replication   bool
		bypassRLS     bool
		sessions      int
	)
	if err := database.QueryRowContext(ctx, `
SELECT current_setting('server_version_num')::integer,
	   current_database(), current_user, role.rolname,
	   role.rolsuper, role.rolcanlogin, role.rolcreatedb, role.rolcreaterole,
	   role.rolreplication, role.rolbypassrls,
	   (SELECT count(*) FROM pg_stat_activity
	    WHERE usename = 'xinsight_admin' AND backend_type = 'client backend')
FROM pg_roles AS role
WHERE role.rolname = 'xinsight_admin'`).Scan(
		&versionNumber, &databaseName, &observerRole, &roleName, &superuser, &canLogin,
		&createDB, &createRole, &replication, &bypassRLS, &sessions,
	); err != nil {
		return AdminDatabaseBoundary{}, errors.New("admin database boundary inspection failed")
	}
	if versionNumber/10000 != 16 || observerRole != "xinsight_reader" ||
		roleName != adminDatabaseRole || !superuser || !canLogin {
		return AdminDatabaseBoundary{}, fmt.Errorf("admin database boundary identity is invalid")
	}
	roleIdentity, err := json.Marshal(struct {
		Role        string `json:"role"`
		Superuser   bool   `json:"superuser"`
		CanLogin    bool   `json:"can_login"`
		CreateDB    bool   `json:"create_db"`
		CreateRole  bool   `json:"create_role"`
		Replication bool   `json:"replication"`
		BypassRLS   bool   `json:"bypass_rls"`
	}{roleName, superuser, canLogin, createDB, createRole, replication, bypassRLS})
	if err != nil {
		return AdminDatabaseBoundary{}, err
	}
	return AdminDatabaseBoundary{
		PostgresMajor: 16, DatabaseSHA256: hashCleanBaselineText(databaseName),
		AdminRoleAlias: roleName, AdminIdentitySHA256: hashCleanBaselineBytes(roleIdentity),
		AdminSessions: sessions,
	}, nil
}

func (manager PostgresManager) ProbeAdminCredentialTransition(
	ctx context.Context,
	predecessor DatabaseCredentials,
	successor DatabaseCredentials,
) (AdminCredentialTransitionState, error) {
	predecessorOK, err := probeAdminCredential(ctx, predecessor.AdminDSN)
	if err != nil {
		return "", err
	}
	successorOK, err := probeAdminCredential(ctx, successor.AdminDSN)
	if err != nil {
		return "", err
	}
	switch {
	case predecessorOK && !successorOK:
		return AdminCredentialTransitionPredecessor, nil
	case !predecessorOK && successorOK:
		return AdminCredentialTransitionSuccessor, nil
	default:
		return "", fmt.Errorf("admin credential transition state is ambiguous")
	}
}

func (manager PostgresManager) ChangeAdminVerifier(
	ctx context.Context,
	predecessor DatabaseCredentials,
	successor DatabaseCredentials,
	entropy io.Reader,
) error {
	if predecessor.Admin == nil || successor.Admin == nil ||
		predecessor.Admin.User != adminDatabaseRole || successor.Admin.User != adminDatabaseRole ||
		predecessor.Admin.Password == successor.Admin.Password ||
		!sameAdminCredentialIdentity(predecessor.AdminDSN, successor.AdminDSN) {
		return fmt.Errorf("admin verifier transition credentials are invalid")
	}
	if err := waitForAdminSessionDrain(ctx, predecessor.ReaderDSN); err != nil {
		return err
	}
	verifier, err := postgresSCRAMVerifier(successor.Admin.Password, entropy)
	if err != nil {
		return err
	}
	database, err := sql.Open("pgx", predecessor.AdminDSN)
	if err != nil {
		return errors.New("predecessor admin database connection cannot be opened")
	}
	defer database.Close()
	database.SetMaxIdleConns(0)
	transaction, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return errors.New("admin verifier transaction cannot begin")
	}
	defer transaction.Rollback()
	var role string
	var superuser bool
	var otherSessions int
	if err := transaction.QueryRowContext(ctx, `
SELECT current_user, role.rolsuper,
	   (SELECT count(*) FROM pg_stat_activity
		WHERE usename = 'xinsight_admin' AND backend_type = 'client backend'
		  AND pid <> pg_backend_pid())
FROM pg_roles AS role
WHERE role.rolname = current_user`).Scan(&role, &superuser, &otherSessions); err != nil ||
		role != adminDatabaseRole || !superuser {
		return errors.New("predecessor admin database boundary cannot be proved")
	}
	if otherSessions != 0 {
		return errors.New("admin verifier transition requires zero concurrent admin sessions")
	}
	statement := `ALTER ROLE xinsight_admin PASSWORD ` + quotePostgresLiteral(verifier)
	verifier = ""
	if _, err := transaction.ExecContext(ctx, statement); err != nil {
		return errors.New("admin SCRAM verifier update failed")
	}
	if err := transaction.Commit(); err != nil {
		return errors.New("admin SCRAM verifier update did not commit")
	}
	return nil
}

func (manager PostgresManager) TerminateOtherAdminSessions(
	ctx context.Context,
	successor DatabaseCredentials,
) (int, error) {
	database, err := sql.Open("pgx", successor.AdminDSN)
	if err != nil {
		return 0, errors.New("successor admin database connection cannot be opened")
	}
	defer database.Close()
	database.SetMaxIdleConns(0)
	var terminated int
	if err := database.QueryRowContext(ctx, `
WITH targets AS MATERIALIZED (
  SELECT pid
  FROM pg_stat_activity
  WHERE usename = 'xinsight_admin'
    AND backend_type = 'client backend'
    AND pid <> pg_backend_pid()
)
SELECT count(*) FROM targets WHERE pg_terminate_backend(pid)`).Scan(&terminated); err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) {
			return 0, fmt.Errorf("pre-rotation admin session termination failed (sqlstate %s)", postgresError.Code)
		}
		return 0, errors.New("pre-rotation admin session termination failed")
	}
	var remaining int
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE usename = 'xinsight_admin' AND backend_type = 'client backend'
  AND pid <> pg_backend_pid()`).Scan(&remaining); err != nil || remaining != 0 {
		return 0, errors.New("admin sessions remain after credential rotation")
	}
	return terminated, nil
}

func (manager PostgresManager) ValidateAdminCredentialTransition(
	ctx context.Context,
	predecessor DatabaseCredentials,
	successor DatabaseCredentials,
) (AdminCredentialTransitionProof, error) {
	database, err := sql.Open("pgx", successor.AdminDSN)
	if err != nil {
		return AdminCredentialTransitionProof{}, errors.New("successor validation session cannot be opened")
	}
	database.SetMaxIdleConns(0)
	var role string
	var sessions int
	if err := database.QueryRowContext(ctx, `
SELECT current_user,
	   (SELECT count(*) FROM pg_stat_activity
		WHERE usename = 'xinsight_admin' AND backend_type = 'client backend'
		  AND pid <> pg_backend_pid())`).Scan(&role, &sessions); err != nil || role != adminDatabaseRole || sessions != 0 {
		_ = database.Close()
		return AdminCredentialTransitionProof{}, errors.New("successor validation session boundary failed")
	}
	if err := database.Close(); err != nil {
		return AdminCredentialTransitionProof{}, errors.New("successor validation session did not close")
	}
	predecessorOK, err := probeAdminCredential(ctx, predecessor.AdminDSN)
	if err != nil || predecessorOK {
		return AdminCredentialTransitionProof{}, errors.New("predecessor admin credential was not rejected")
	}
	boundary, err := manager.InspectAdminBoundary(ctx, successor)
	if err != nil || boundary.AdminSessions != 0 {
		return AdminCredentialTransitionProof{}, errors.New("admin session readback after validation failed")
	}
	return AdminCredentialTransitionProof{
		SuccessorFreshConnection: true, PredecessorAuthRejected: true,
		ValidationSessionClosed: true, AdminSessionsRemaining: 0,
	}, nil
}

func probeAdminCredential(ctx context.Context, dsn string) (bool, error) {
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		return false, errors.New("admin credential probe cannot be opened")
	}
	defer database.Close()
	database.SetMaxIdleConns(0)
	if err := database.PingContext(ctx); err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "28P01" {
			return false, nil
		}
		return false, errors.New("admin credential probe failed outside the authentication boundary")
	}
	return true, nil
}

func waitForAdminSessionDrain(ctx context.Context, readerDSN string) error {
	for attempt := 0; attempt < adminSessionDrainAttempts; attempt++ {
		database, err := sql.Open("pgx", readerDSN)
		if err != nil {
			return errors.New("admin session observer cannot be opened")
		}
		database.SetMaxIdleConns(0)
		var sessions int
		err = database.QueryRowContext(ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE usename = 'xinsight_admin' AND backend_type = 'client backend'`).Scan(&sessions)
		closeErr := database.Close()
		if err != nil || closeErr != nil {
			return errors.New("admin session observer failed")
		}
		if sessions == 0 {
			return nil
		}
		if attempt+1 == adminSessionDrainAttempts {
			break
		}
		timer := time.NewTimer(adminSessionDrainInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return errors.New("admin verifier transition context ended during session quiescence")
		case <-timer.C:
		}
	}
	return errors.New("admin verifier transition requires zero concurrent admin sessions")
}

func postgresSCRAMVerifier(password string, entropy io.Reader) (string, error) {
	if password == "" || entropy == nil {
		return "", fmt.Errorf("successor admin credential entropy is invalid")
	}
	salt := make([]byte, 16)
	if _, err := io.ReadFull(entropy, salt); err != nil {
		return "", fmt.Errorf("generate admin SCRAM salt")
	}
	passwordBytes := []byte(password)
	saltedPassword := pbkdf2.Key(passwordBytes, salt, postgresSCRAMIterations, sha256.Size, sha256.New)
	clientMAC := hmac.New(sha256.New, saltedPassword)
	_, _ = clientMAC.Write([]byte("Client Key"))
	clientKey := clientMAC.Sum(nil)
	storedKey := sha256.Sum256(clientKey)
	serverMAC := hmac.New(sha256.New, saltedPassword)
	_, _ = serverMAC.Write([]byte("Server Key"))
	serverKey := serverMAC.Sum(nil)
	verifier := fmt.Sprintf(
		"SCRAM-SHA-256$%d:%s$%s:%s",
		postgresSCRAMIterations,
		base64.StdEncoding.EncodeToString(salt),
		base64.StdEncoding.EncodeToString(storedKey[:]),
		base64.StdEncoding.EncodeToString(serverKey),
	)
	clearBytes(passwordBytes)
	clearBytes(salt)
	clearBytes(saltedPassword)
	clearBytes(clientKey)
	clearBytes(serverKey)
	return verifier, nil
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func clearAdminCredentialRaw(raw map[string][]byte) {
	for _, contents := range raw {
		clearBytes(contents)
	}
}
