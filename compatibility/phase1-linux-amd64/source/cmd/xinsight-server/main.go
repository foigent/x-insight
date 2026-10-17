package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/foigent/x-insight/internal/api"
	"github.com/foigent/x-insight/internal/db"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "xinsight-server: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}

	switch args[0] {
	case "serve":
		addr := ":8080"
		if len(args) > 1 {
			addr = args[1]
		}
		return serve(addr)
	case "migrate":
		if len(args) == 2 && args[1] == "up" {
			dsn := os.Getenv("XINSIGHT_POSTGRES_DSN")
			if dsn == "" {
				return fmt.Errorf("XINSIGHT_POSTGRES_DSN is required")
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
	var store *db.EvidenceStore
	dsn := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if dsn != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		opened, err := db.OpenEvidenceStore(ctx, dsn)
		if err != nil {
			return err
		}
		defer opened.Close()
		store = opened
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           api.NewHandler(store),
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Printf("xinsight-server listening on %s\n", addr)
	return server.ListenAndServe()
}

func usage() error {
	return fmt.Errorf("usage: xinsight-server serve [addr] | xinsight-server migrate up")
}
