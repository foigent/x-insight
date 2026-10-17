package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/access"
	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/webui"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "xinsight-server: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if handled, err := buildinfo.HandleArgs(args, os.Stdout); handled {
		return err
	}
	if len(args) == 0 {
		return usage()
	}

	switch args[0] {
	case "webui-manifest":
		if len(args) != 2 || args[1] != "--json" {
			return usage()
		}
		contents, err := webui.EmbeddedAssetManifest()
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(contents)
		return err
	case "serve":
		addr := "127.0.0.1:8080"
		if len(args) > 1 {
			addr = args[1]
		}
		return serve(addr)
	case "migrate":
		if len(args) == 2 && args[1] == "up" {
			dsn, err := config.LoadAdminPostgresDSN()
			if err != nil {
				return err
			}
			dir := os.Getenv("XINSIGHT_MIGRATIONS_DIR")
			if dir == "" {
				dir = "migrations"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			applied, err := db.RunMigrations(ctx, dsn, dir)
			if err != nil {
				return err
			}
			fmt.Printf("migrations applied=%d dir=%s\n", applied, dir)
			return nil
		}
		return usage()
	default:
		return usage()
	}
}

func serve(addr string) error {
	if err := validateLoopbackAddress(addr); err != nil {
		return err
	}
	transportSecurity, err := api.NewTransportSecurity(
		os.Getenv(api.TrustedTLSProxyModeEnv),
		os.Getenv(api.ExternalOriginEnv),
	)
	if err != nil {
		return err
	}
	connectorConfig, err := loadConnectorInvitationConfig()
	if err != nil {
		return err
	}
	auditor := access.NewWriterAuditor(os.Stdout)
	provider, err := access.LoadLocalProvider(auditor)
	if err != nil {
		return err
	}
	accessService, err := access.NewService(provider, auditor, access.ServiceOptions{})
	if err != nil {
		return err
	}
	dsn, err := config.LoadReaderPostgresDSN()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store, err := db.OpenEvidenceStore(ctx, dsn)
	if err != nil {
		return err
	}
	defer store.Close()
	workspaceDSN, err := config.LoadWorkspacePostgresDSN()
	if err != nil {
		return err
	}
	workspaceStore, err := db.OpenWorkspaceStore(ctx, workspaceDSN)
	if err != nil {
		return err
	}
	defer workspaceStore.Close()
	migrationsDir := os.Getenv("XINSIGHT_MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "migrations"
	}
	expectedMigrations, err := db.LoadMigrationChecksums(migrationsDir)
	if err != nil {
		return fmt.Errorf("load release migration identity: %w", err)
	}

	localConnectorController := connectorcontrol.NewUnixController()
	server := &http.Server{
		Addr: addr,
		Handler: api.NewProductionHandler(
			store,
			workspaceStore,
			expectedMigrations,
			accessService,
			transportSecurity,
			connectorConfig,
			localConnectorController,
			localConnectorController,
		),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	fmt.Printf("xinsight-server listening on %s\n", addr)
	return server.ListenAndServe()
}

func loadConnectorInvitationConfig() (connectorcontrol.InvitationConfig, error) {
	config := connectorcontrol.InvitationConfig{
		DeploymentID:           strings.TrimSpace(os.Getenv(connectorcontrol.DeploymentIDEnv)),
		XInsightOrigin:         strings.TrimSpace(os.Getenv(api.ExternalOriginEnv)),
		UniRouterInstallOrigin: strings.TrimSpace(os.Getenv(connectorcontrol.UniRouterInstallOriginEnv)),
	}
	if config.UniRouterInstallOrigin == "" {
		if config.DeploymentID != "" && !connectorcontrol.IsDeploymentID(config.DeploymentID) {
			return connectorcontrol.InvitationConfig{}, fmt.Errorf("connector deployment identity is invalid")
		}
		return connectorcontrol.InvitationConfig{}, nil
	}
	if err := config.Validate(); err != nil {
		return connectorcontrol.InvitationConfig{}, fmt.Errorf("connector installation configuration is invalid")
	}
	return config, nil
}

func validateLoopbackAddress(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("listen address must be loopback-only")
	}
	return nil
}

func usage() error {
	return fmt.Errorf("usage: xinsight-server serve [addr] | xinsight-server migrate up | xinsight-server webui-manifest --json")
}
