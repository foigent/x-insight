package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/foigent/x-insight/internal/draftevidence"
)

func main() {
	repositoryRoot := flag.String("repository-root", ".", "X-Insight repository root")
	reportPath := flag.String("report", "-", "report output path, or - for stdout")
	observedAtText := flag.String("observed-at", "", "RFC3339 observation time; defaults to current UTC")
	flag.Parse()

	observedAt := time.Now().UTC()
	if *observedAtText != "" {
		parsed, err := time.Parse(time.RFC3339, *observedAtText)
		if err != nil {
			exitf("parse observed-at: %v", err)
		}
		observedAt = parsed
	}
	report, runErr := draftevidence.Run(*repositoryRoot, observedAt)
	if err := draftevidence.WriteReport(*reportPath, report); err != nil {
		exitf("write report: %v", err)
	}
	if runErr != nil {
		exitf("%v", runErr)
	}
}

func exitf(format string, values ...any) {
	fmt.Fprintf(os.Stderr, "xinsight-draft-evidence-validator: "+format+"\n", values...)
	os.Exit(1)
}
