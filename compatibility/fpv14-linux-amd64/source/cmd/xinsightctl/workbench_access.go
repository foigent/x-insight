package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/foigent/x-insight/internal/hostctl"
	"io"
)

func runWorkbenchAccess(ctx context.Context, e *hostctl.Engine, args []string, out io.Writer, root bool) error {
	if !root {
		return fmt.Errorf("workbench access configuration requires root")
	}
	if len(args) > 0 && args[0] == "show" {
		fs := flag.NewFlagSet("workbench-access show", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("grant-id", "", "exact grant id")
		if fs.Parse(args[1:]) != nil || fs.NArg() != 0 || *id == "" {
			return fmt.Errorf("invalid workbench-access show arguments")
		}
		result, err := e.InspectWorkbenchGrant(ctx, *id)
		return writeResult(out, result, err)
	}
	if len(args) == 0 || args[0] != "apply" {
		return fmt.Errorf("usage: xinsightctl workbench-access apply --file /absolute/grant.json [--check]")
	}
	fs := flag.NewFlagSet("workbench-access apply", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("file", "", "root-owned exact grant request")
	check := fs.Bool("check", false, "read-only validation against current source and revision")
	if fs.Parse(args[1:]) != nil || fs.NArg() != 0 || *file == "" {
		return fmt.Errorf("invalid workbench-access arguments")
	}
	result, err := e.ManageWorkbenchGrant(ctx, *file, *check)
	return writeResult(out, result, err)
}
