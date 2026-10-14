package config

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	PostgresDSNEnv      = "XINSIGHT_POSTGRES_DSN"
	PostgresDSNFileEnv  = "XINSIGHT_POSTGRES_DSN_FILE"
	ReaderDSNEnv        = "XINSIGHT_READER_POSTGRES_DSN"
	ReaderDSNFileEnv    = "XINSIGHT_READER_POSTGRES_DSN_FILE"
	WorkspaceDSNEnv     = "XINSIGHT_WORKSPACE_POSTGRES_DSN"
	WorkspaceDSNFileEnv = "XINSIGHT_WORKSPACE_POSTGRES_DSN_FILE"
	ImporterDSNEnv      = "XINSIGHT_IMPORTER_POSTGRES_DSN"
	ImporterDSNFileEnv  = "XINSIGHT_IMPORTER_POSTGRES_DSN_FILE"
	AdminDSNEnv         = "XINSIGHT_ADMIN_POSTGRES_DSN"
	AdminDSNFileEnv     = "XINSIGHT_ADMIN_POSTGRES_DSN_FILE"
	AllowLegacyRoleEnv  = "XINSIGHT_ALLOW_LEGACY_SINGLE_DATABASE_ROLE"
	CredentialsDirEnv   = "CREDENTIALS_DIRECTORY"
	PostgresCredential  = "postgres.dsn"
	ReaderCredential    = "postgres-reader.dsn"
	WorkspaceCredential = "postgres-workspace.dsn"
	ImporterCredential  = "postgres-importer.dsn"
	AdminCredential     = "postgres-admin.dsn"
	MaxPostgresDSNBytes = 8 << 10
)

var applicationNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// LoadPostgresDSN resolves the development/CI environment form or the
// production systemd credential form. It never includes DSN contents in an
// error so callers can safely log failures.
func LoadPostgresDSN() (string, error) {
	return loadPostgresDSNSource(PostgresDSNEnv, PostgresDSNFileEnv, PostgresCredential)
}

func LoadReaderPostgresDSN() (string, error) {
	return loadRolePostgresDSN("reader", ReaderDSNEnv, ReaderDSNFileEnv, ReaderCredential)
}

func LoadWorkspacePostgresDSN() (string, error) {
	return loadRolePostgresDSN("workspace", WorkspaceDSNEnv, WorkspaceDSNFileEnv, WorkspaceCredential)
}

func LoadImporterPostgresDSN() (string, error) {
	return loadRolePostgresDSN("importer", ImporterDSNEnv, ImporterDSNFileEnv, ImporterCredential)
}

func LoadAdminPostgresDSN() (string, error) {
	return loadRolePostgresDSN("admin", AdminDSNEnv, AdminDSNFileEnv, AdminCredential)
}

func LoadPostgresDSNFile(path string) (string, error) {
	return loadPostgresDSNFile(path)
}

func loadRolePostgresDSN(role string, directEnv string, fileEnv string, credential string) (string, error) {
	value, err := loadPostgresDSNSource(directEnv, fileEnv, credential)
	if err == nil {
		return value, nil
	}
	if os.Getenv(directEnv) != "" || strings.TrimSpace(os.Getenv(fileEnv)) != "" || credentialExists(credential) {
		return "", err
	}
	if os.Getenv(AllowLegacyRoleEnv) == "1" {
		return LoadPostgresDSN()
	}
	return "", fmt.Errorf("%s Postgres credential is required; single-role fallback is disabled", role)
}

func loadPostgresDSNSource(directEnv string, fileEnv string, credential string) (string, error) {
	direct := os.Getenv(directEnv)
	explicitFile := strings.TrimSpace(os.Getenv(fileEnv))
	credentialFile, err := systemdCredentialPath(credential)
	if err != nil {
		return "", err
	}

	filePath := explicitFile
	if filePath == "" {
		filePath = credentialFile
	} else if credentialFile != "" {
		return "", errors.New("configure exactly one explicit or systemd Postgres DSN file")
	}
	if direct != "" && filePath != "" {
		return "", fmt.Errorf("configure exactly one of %s or its Postgres DSN file", directEnv)
	}
	if direct != "" {
		return ValidatePostgresDSN(direct)
	}
	if filePath == "" {
		return "", fmt.Errorf("Postgres DSN is required through %s, %s, or the systemd %s credential", directEnv, fileEnv, credential)
	}
	return loadPostgresDSNFile(filePath)
}

// ValidatePostgresDSN validates the single-node PRD2 database boundary. Only
// an explicit PostgreSQL URL with a loopback host is accepted; libpq service,
// passfile, fallback-host and implicit environment behaviors are excluded.
func ValidatePostgresDSN(value string) (string, error) {
	if len(value) == 0 || len(value) > MaxPostgresDSNBytes {
		return "", errors.New("Postgres DSN length is invalid")
	}
	if strings.TrimSpace(value) != value {
		return "", errors.New("Postgres DSN must not contain surrounding whitespace")
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return "", errors.New("Postgres DSN contains a control character")
		}
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", errors.New("Postgres DSN is not a valid URL")
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return "", errors.New("Postgres DSN must use the postgres or postgresql URL scheme")
	}
	if parsed.Opaque != "" || parsed.Fragment != "" || parsed.User == nil {
		return "", errors.New("Postgres DSN URL structure is invalid")
	}
	username := parsed.User.Username()
	if username == "" || !safeCredentialComponent(username, 63) {
		return "", errors.New("Postgres DSN must include a database role")
	}
	password, present := parsed.User.Password()
	if !present || password == "" || !safeCredentialComponent(password, 1024) {
		return "", errors.New("Postgres DSN must include an explicit password")
	}
	if strings.Contains(parsed.Host, ",") || !loopbackHost(parsed.Hostname()) {
		return "", errors.New("Postgres DSN host must be loopback-only")
	}
	if port := parsed.Port(); port != "" {
		portNumber, convertErr := strconv.Atoi(port)
		if convertErr != nil || portNumber < 1 || portNumber > 65535 {
			return "", errors.New("Postgres DSN port is invalid")
		}
	}
	databaseName := strings.TrimPrefix(parsed.Path, "/")
	if databaseName == "" || strings.Contains(databaseName, "/") || !safeCredentialComponent(databaseName, 63) {
		return "", errors.New("Postgres DSN must name exactly one database")
	}
	if err := validatePostgresQuery(parsed.Query()); err != nil {
		return "", err
	}

	configuration, err := pgx.ParseConfigWithOptions(value, pgx.ParseConfigOptions{
		ParseConfigOptions: pgconn.ParseConfigOptions{
			ConnStringAllowedKeys: []string{
				"host", "port", "user", "password", "dbname", "database",
				"sslmode", "connect_timeout", "application_name",
			},
		},
	})
	if err != nil {
		return "", errors.New("Postgres DSN cannot be parsed")
	}
	if !loopbackHost(configuration.Host) {
		return "", errors.New("Postgres DSN resolved host must be loopback-only")
	}
	for _, fallback := range configuration.Fallbacks {
		if !loopbackHost(fallback.Host) {
			return "", errors.New("Postgres DSN fallback host must be loopback-only")
		}
	}
	return value, nil
}

func loadPostgresDSNFile(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", errors.New("Postgres DSN file path must be absolute and clean")
	}
	before, err := os.Lstat(path)
	if err != nil {
		return "", errors.New("Postgres DSN file cannot be inspected")
	}
	if !before.Mode().IsRegular() {
		return "", errors.New("Postgres DSN file must be a regular file, not a link")
	}
	if err := validateSecretFileMetadata(before); err != nil {
		return "", err
	}

	file, err := os.Open(path)
	if err != nil {
		return "", errors.New("Postgres DSN file cannot be opened")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return "", errors.New("Postgres DSN file changed while opening")
	}
	contents, err := io.ReadAll(io.LimitReader(file, MaxPostgresDSNBytes+1))
	if err != nil {
		return "", errors.New("Postgres DSN file cannot be read")
	}
	if len(contents) > MaxPostgresDSNBytes {
		return "", errors.New("Postgres DSN file is too large")
	}
	value := strings.TrimSuffix(string(contents), "\n")
	value = strings.TrimSuffix(value, "\r")
	return ValidatePostgresDSN(value)
}

func validateSecretFileMetadata(info os.FileInfo) error {
	permissions := info.Mode().Perm()
	if permissions&0o400 == 0 || permissions&0o007 != 0 || permissions&0o030 != 0 || permissions&0o100 != 0 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return errors.New("Postgres DSN file permissions must be owner-read/write and optionally group-read only")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("Postgres DSN file ownership cannot be verified")
	}
	owner := int(stat.Uid)
	if owner != 0 && owner != os.Geteuid() {
		return errors.New("Postgres DSN file must be owned by root or the current service user")
	}
	return nil
}

func systemdCredentialPath(credential string) (string, error) {
	directory := strings.TrimSpace(os.Getenv(CredentialsDirEnv))
	if directory == "" {
		return "", nil
	}
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return "", errors.New("systemd credentials directory must be absolute and clean")
	}
	path := filepath.Join(directory, credential)
	if _, err := os.Lstat(path); err == nil {
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("systemd Postgres credential cannot be inspected")
	}
	return "", nil
}

func credentialExists(credential string) bool {
	path, err := systemdCredentialPath(credential)
	return err == nil && path != ""
}

func validatePostgresQuery(values url.Values) error {
	for key, entries := range values {
		if len(entries) != 1 || entries[0] == "" {
			return errors.New("Postgres DSN query parameters must have one non-empty value")
		}
		switch key {
		case "sslmode":
			switch entries[0] {
			case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
			default:
				return errors.New("Postgres DSN sslmode is invalid")
			}
		case "connect_timeout":
			seconds, err := strconv.Atoi(entries[0])
			if err != nil || seconds < 1 || seconds > 30 {
				return errors.New("Postgres DSN connect_timeout must be between 1 and 30 seconds")
			}
		case "application_name":
			if !applicationNamePattern.MatchString(entries[0]) {
				return errors.New("Postgres DSN application_name is invalid")
			}
		default:
			return errors.New("Postgres DSN query parameter is not allowed")
		}
	}
	return nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func safeCredentialComponent(value string, maxBytes int) bool {
	if value == "" || len(value) > maxBytes {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}
