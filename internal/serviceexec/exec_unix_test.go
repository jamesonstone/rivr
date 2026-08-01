//go:build darwin || linux

package serviceexec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jamesonstone/rungrid/internal/manifest"
)

func TestComposeShutdownUsesExactConfiguredArguments(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "arguments.log")
	executable := filepath.Join(root, "fake-compose")
	script := "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$@\" > \"$RUNGRID_ARG_LOG\"\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	service := &manifest.Service{
		Name:             "database",
		Source:           "compose",
		WorkingDirectory: ".",
		Environment: manifest.Environment{Values: map[string]string{
			"RUNGRID_ARG_LOG": logPath,
		}},
		Compose: &manifest.Compose{
			File:        "compose.yaml",
			ProjectName: "example-workspace",
			Service:     "database",
			Profiles:    []string{"development", "metrics"},
			DownArgv:    []string{executable, "compose"},
		},
	}

	if err := ComposeShutdown(service, root, context.Background()); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"compose",
		"--file", "compose.yaml",
		"--project-name", "example-workspace",
		"--profile", "development",
		"--profile", "metrics",
		"stop", "database",
		"",
	}, "\n")
	if string(content) != want {
		t.Fatalf("unexpected arguments:\n%s\nwant:\n%s", content, want)
	}
}
