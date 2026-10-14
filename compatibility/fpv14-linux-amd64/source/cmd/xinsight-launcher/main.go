package main

import (
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/config"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "xinsight-launcher: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if handled, err := buildinfo.HandleArgs(args, stdout); handled {
		return err
	}
	if len(args) != 0 {
		return fmt.Errorf("launcher does not accept arguments")
	}
	current := "/opt/x-insight/current"
	if override := os.Getenv("XINSIGHT_LAUNCHER_CURRENT"); override != "" {
		if os.Getenv("XINSIGHT_LAUNCHER_ALLOW_TEST_SERVER") != "1" {
			return fmt.Errorf("launcher current override is disabled")
		}
		current = override
	}
	binary := current + "/bin/xinsight-server"
	if override := os.Getenv("XINSIGHT_LAUNCHER_SERVER"); override != "" {
		if os.Getenv("XINSIGHT_LAUNCHER_ALLOW_TEST_SERVER") != "1" {
			return fmt.Errorf("launcher server override is disabled")
		}
		binary = override
	}
	modern, err := isManagedRelease(current)
	if err != nil {
		return err
	}
	environment, err := launchEnvironment(modern, os.Environ())
	if err != nil {
		return err
	}
	return syscall.Exec(binary, []string{binary, "serve", "127.0.0.1:8080"}, environment)
}

func isManagedRelease(current string) (bool, error) {
	info, err := os.Lstat(current + "/bin/xinsightctl")
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("managed release marker cannot be inspected")
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("managed release marker is not a regular file")
	}
	return true, nil
}

func launchEnvironment(modern bool, inherited []string) ([]string, error) {
	environment := make([]string, 0, len(os.Environ())+3)
	for _, value := range inherited {
		if hasEnvironmentKey(value, config.ReaderDSNEnv) ||
			hasEnvironmentKey(value, config.ReaderDSNFileEnv) ||
			hasEnvironmentKey(value, config.WorkspaceDSNEnv) ||
			hasEnvironmentKey(value, config.WorkspaceDSNFileEnv) ||
			hasEnvironmentKey(value, config.PostgresDSNEnv) ||
			hasEnvironmentKey(value, config.PostgresDSNFileEnv) ||
			hasEnvironmentKey(value, config.AllowLegacyRoleEnv) {
			continue
		}
		if !modern && hasEnvironmentKey(value, config.CredentialsDirEnv) {
			continue
		}
		environment = append(environment, value)
	}
	if modern {
		return environment, nil
	}
	dsn, err := config.LoadReaderPostgresDSN()
	if err != nil {
		return nil, err
	}
	environment = append(environment,
		config.PostgresDSNEnv+"="+dsn,
		config.AllowLegacyRoleEnv+"=1",
	)
	return environment, nil
}

func hasEnvironmentKey(value string, key string) bool {
	return len(value) > len(key) && value[:len(key)] == key && value[len(key)] == '='
}
