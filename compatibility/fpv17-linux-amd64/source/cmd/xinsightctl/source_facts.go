package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/foigent/x-insight/internal/hostctl"
	"io"
)

func runSourceFactsControl(ctx context.Context, e *hostctl.Engine, args []string, out io.Writer) error {
	if len(args) > 0 && args[0] == "content-wire-check" {
		return runContentControlCheck(args[1:], out)
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: xinsightctl source-facts configure|register|preflight|enable|pause|resume|revoke|status|worker-start|worker-stop|worker-status|offline-register|offline-enable|offline-disable|offline-revoke|offline-status")
	}
	r := hostctl.SourceFactsRequest{Action: args[0]}
	fs := flag.NewFlagSet("source-facts "+r.Action, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	switch r.Action {
	case "budget-offline-register", "budget-offline-enable":
		fs.StringVar(&r.AuthorizationFile, "authorization-file", "", "root-owned budget offline authorization")
		fs.StringVar(&r.TargetRef, "target-ref", "", "exact target reference")
		if r.Action == "budget-offline-enable" {
			fs.Int64Var(&r.Revision, "revision", 0, "observed collection revision")
			fs.BoolVar(&r.Acknowledge, "acknowledge", false, "acknowledge corrected persistent failure")
		}
	case "configure":
		fs.StringVar(&r.ConfigFile, "file", "", "root-owned managed configuration")
	case "worker-start", "worker-stop", "worker-status":
	case "register", "preflight", "status":
		fs.StringVar(&r.ID, "stream-id", "", "exact stream ID")
	case "budget-register", "budget-preflight", "budget-status", "content-register", "content-preflight", "content-status":
		fs.StringVar(&r.ID, "collection-id", "", "exact budget collection ID")
	case "budget-enable", "budget-pause", "budget-resume", "budget-revoke", "content-enable", "content-pause", "content-resume", "content-revoke", "content-rebind":
		fs.StringVar(&r.ID, "collection-id", "", "exact budget collection ID")
		fs.Int64Var(&r.Revision, "revision", 0, "observed registration revision")
		if r.Action == "budget-enable" || r.Action == "budget-resume" || r.Action == "content-enable" || r.Action == "content-resume" {
			fs.BoolVar(&r.Acknowledge, "acknowledge", false, "acknowledge a corrected persistent failure")
		}
	case "enable", "pause", "resume", "revoke":
		fs.StringVar(&r.ID, "stream-id", "", "exact stream ID")
		fs.Int64Var(&r.Revision, "revision", 0, "observed registration revision")
		if r.Action == "enable" || r.Action == "resume" {
			fs.BoolVar(&r.Acknowledge, "acknowledge", false, "explicitly acknowledge a persistent source failure after correction")
		}
	case "offline-register", "offline-enable":
		fs.StringVar(&r.AuthorizationFile, "authorization-file", "", "root-owned current offline authorization")
		fs.StringVar(&r.TargetRef, "target-ref", "", "exact target reference")
		if r.Action == "offline-enable" {
			fs.Int64Var(&r.Revision, "revision", 0, "observed offline registration revision")
		}
	case "offline-status", "offline-disable", "offline-revoke":
		fs.StringVar(&r.ID, "authorization-id", "", "exact offline authorization ID")
		if r.Action != "offline-status" {
			fs.Int64Var(&r.Revision, "revision", 0, "observed offline registration revision")
		}
	default:
		return fmt.Errorf("unknown source-facts action")
	}
	if fs.Parse(args[1:]) != nil || fs.NArg() != 0 {
		return fmt.Errorf("invalid source-facts arguments")
	}
	result, err := e.ManageSourceFacts(ctx, r)
	return writeResult(out, result, err)
}
