//go:build darwin || linux

package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	options := runnerOptions{}
	flags := flag.NewFlagSet("rungrid-evidence", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&options.repositoryRoot, "repository-root", "", "repository root")
	flags.StringVar(&options.evidenceRoot, "evidence-root", "", "evidence root")
	flags.Int64Var(&options.outputLimit, "output-limit-bytes", hardOutputLimit, "maximum output bytes")
	if err := flags.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	options.command = flags.Args()
	if err := options.validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	result, runDirectory, err := runEvidence(options)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evidence runner failed: %v\n", err)
		os.Exit(1)
	}
	if result.FailureKind == "" {
		fmt.Printf("%s evidence: %s\n", result.Result, runDirectory)
	} else {
		fmt.Printf("%s (%s) evidence: %s\n", result.Result, result.FailureKind, runDirectory)
	}
	os.Exit(result.ExitCode)
}
