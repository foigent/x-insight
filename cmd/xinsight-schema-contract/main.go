package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/foigent/x-insight/internal/hostctl"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "xinsight-schema-contract: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("xinsight-schema-contract", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	output := flags.String("output", "", "generated contract JSON path; stdout when empty")
	verifyExpected := flags.Bool(
		"verify-expected",
		false,
		"compare the database with the embedded release contract and emit a sanitized inspection",
	)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: xinsight-schema-contract [--output FILE] [--verify-expected]")
	}
	if *verifyExpected && *output != "" {
		return fmt.Errorf("--output and --verify-expected are mutually exclusive")
	}
	dsn := os.Getenv("XINSIGHT_SCHEMA_CONTRACT_DSN")
	if dsn == "" {
		return fmt.Errorf("XINSIGHT_SCHEMA_CONTRACT_DSN is required")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("database cannot be opened")
	}
	defer database.Close()
	if err := database.PingContext(ctx); err != nil {
		return fmt.Errorf("database cannot be reached")
	}
	var result any
	if *verifyExpected {
		inspection, inspectErr := hostctl.InspectCleanBaselineSchemaContract(ctx, database)
		if inspectErr != nil {
			return inspectErr
		}
		result = inspection
	} else {
		manifest, generateErr := hostctl.GenerateCleanBaselineSchemaContract(ctx, database)
		if generateErr != nil {
			return generateErr
		}
		result = manifest
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if *verifyExpected {
		if _, err := os.Stdout.Write(payload); err != nil {
			return err
		}
		inspection := result.(hostctl.SchemaContractInspection)
		if !inspection.Valid {
			return fmt.Errorf(
				"embedded release contract mismatch: expected=%s actual=%s mismatches=%d",
				inspection.ExpectedSHA256,
				inspection.ActualSHA256,
				len(inspection.Mismatches),
			)
		}
		return nil
	}
	if *output == "" {
		_, err = os.Stdout.Write(payload)
		return err
	}
	if !filepath.IsAbs(*output) || filepath.Clean(*output) != *output {
		return fmt.Errorf("output path must be clean and absolute")
	}
	return os.WriteFile(*output, payload, 0o644)
}
