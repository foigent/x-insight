package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/foigent/x-insight/internal/hostctl"
)

func managedAccessCommand(args []string) bool {
	return len(args) > 0 && (args[0] == "bootstrap-owner" || args[0] == "recover-account" || args[0] == "directory")
}
func runManagedAccess(ctx context.Context, engine *hostctl.Engine, args []string, stdout io.Writer, root bool) error {
	if !root {
		return fmt.Errorf("xinsightctl access commands require root")
	}
	if len(args) == 0 {
		return fmt.Errorf("managed account command required")
	}
	if args[0] == "directory" {
		if len(args) != 1 {
			return fmt.Errorf("usage: access directory")
		}
		out, e := engine.ManagedAccountDirectory(ctx)
		return writeResult(stdout, out, e)
	}
	fs := flag.NewFlagSet("access "+args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	r := hostctl.AccountBootstrapRequest{}
	fs.StringVar(&r.Username, "username", "", "first owner username")
	fs.StringVar(&r.SubjectID, "subject", "", "existing stable subject")
	fs.Int64Var(&r.ExpectedRevision, "expected-revision", 0, "current account revision")
	fs.StringVar(&r.Output, "output", "", "new private handoff file")
	fs.BoolVar(&r.CheckOnly, "check", false, "read-only preflight")
	if e := fs.Parse(args[1:]); e != nil || fs.NArg() != 0 {
		return fmt.Errorf("invalid native account handoff arguments")
	}
	if (args[0] == "bootstrap-owner" && (r.Username == "" || r.SubjectID != "" || r.ExpectedRevision != 0)) || (args[0] == "recover-account" && (r.SubjectID == "" || r.Username != "" || r.ExpectedRevision < 1)) {
		return fmt.Errorf("bootstrap-owner requires username; recover-account requires exact subject and expected-revision")
	}
	out, e := engine.AccountBootstrap(ctx, r)
	return writeResult(stdout, out, e)
}
