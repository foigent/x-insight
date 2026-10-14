package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

// This offline command reads explicit public snapshots only. It never uses
// an Engine or follows key_file paths contained in the managed configuration.
func runContentControlCheck(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("source-facts content-wire-check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var in hostctl.ContentControlCheckInput
	var registration, managed, get, caps, request, receipt string
	fs.StringVar(&registration, "registration-file", "", "public source registration")
	fs.StringVar(&managed, "config-file", "", "public managed configuration")
	fs.StringVar(&in.CollectionID, "collection-id", "", "exact content collection")
	fs.StringVar(&get, "get-file", "", "observed public GET response")
	fs.StringVar(&caps, "capabilities-file", "", "independent public capabilities snapshot")
	fs.StringVar(&in.Action, "action", "", "original management request action")
	fs.StringVar(&request, "request-file", "", "original public management request")
	fs.StringVar(&receipt, "receipt-file", "", "observed public POST receipt")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return fmt.Errorf("invalid content-wire-check arguments")
	}
	for _, field := range []struct {
		path   string
		target *[]byte
	}{{registration, &in.Registration}, {managed, &in.ManagedConfig}, {get, &in.GET}, {caps, &in.Capabilities}, {request, &in.Request}, {receipt, &in.Receipt}} {
		if field.path == "" {
			continue
		}
		raw, err := readContentControlPublicFile(field.path)
		if err != nil {
			return err
		}
		*field.target = raw
	}
	result, err := hostctl.CheckContentControl(in)
	return writeResult(out, result, err)
}
func readContentControlPublicFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read public content control document")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > sourcefacts.MaxDocumentBytes {
		return nil, sourcefacts.ErrInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(f, sourcefacts.MaxDocumentBytes+1))
	if err != nil || len(raw) > sourcefacts.MaxDocumentBytes {
		return nil, sourcefacts.ErrInvalid
	}
	return raw, nil
}
