package main

import (
	"fmt"
	"os"

	"github.com/jamesonstone/rungrid/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		code := cmd.ExitCode(err)
		if code != 130 {
			_, _ = fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(code)
	}
}
