package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/connectorsync"
	"github.com/foigent/x-insight/internal/hostctl"
	"io"
	"os"
)

func runConnectorSync(ctx context.Context, _ *hostctl.Engine, args []string, out io.Writer) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("connector-sync requires the local administrator")
	}
	if len(args) == 0 {
		return fmt.Errorf("connector-sync status|auto-off|auto-on|once --installation-id ID --actor SUBJECT [--expected-revision N --request-id UUID --cadence-seconds N]")
	}
	fs := flag.NewFlagSet("connector-sync", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	r := connectorsync.Request{SchemaVersion: connectorsync.LocalSchema}
	fs.StringVar(&r.InstallationID, "installation-id", "", "exact connection")
	fs.StringVar(&r.Actor, "actor", "", "existing authorized local account subject")
	fs.Int64Var(&r.ExpectedRevision, "expected-revision", 0, "observed control revision")
	fs.StringVar(&r.RequestID, "request-id", "", "idempotency UUID")
	cadence := fs.Int("cadence-seconds", 0, "5..3600; preserve if omitted")
	if fs.Parse(args[1:]) != nil || fs.NArg() != 0 {
		return connectorsync.ErrInvalid
	}
	switch args[0] {
	case "status":
		r.Action = "status"
	case "auto-off":
		r.Action = "set"
	case "auto-on":
		r.Action = "set"
		r.Auto = true
	case "once":
		r.Action = "once"
	default:
		return connectorsync.ErrInvalid
	}
	if *cadence != 0 {
		r.Cadence = cadence
	}
	if r.Actor == "" || !r.Valid() {
		return connectorsync.ErrInvalid
	}
	result, e := connectorcontrol.NewUnixController().ExecuteSync(ctx, r)
	return writeResult(out, result, e)
}
