package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jamesonstone/rungrid/internal/manifest"
)

func TestDoctorChecksLifecycleExecutableWithoutRunningIt(t *testing.T) {
	root := t.TempDir()
	processCompose := filepath.Join(root, "process-compose")
	if err := os.WriteFile(processCompose, []byte("#!/bin/sh\nprintf 'Version: v1.120.0\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(root, "hook")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch lifecycle-ran\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	configuration := `api_version: rungrid/v1
kind: Workspace
project: {name: Example, slug: example, id: example-k7m4q2}
runtime: {process_compose: {executable: process-compose}}
terminal: {mode: headless}
lifecycle:
  before_up:
    - {name: prepare, run: {argv: [./hook]}}
services: []
`
	filename := filepath.Join(root, ".rungrid.yaml")
	if err := os.WriteFile(filename, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := manifest.Load(filename, "")
	if err != nil {
		t.Fatal(err)
	}
	report := Run(context.Background(), loaded, t.TempDir(), false)
	if !report.OK || !hasCheck(report, "lifecycle:before_up:prepare", "ok") {
		t.Fatalf("doctor did not validate lifecycle executable: %#v", report)
	}
	if _, err := os.Stat(filepath.Join(root, "lifecycle-ran")); !os.IsNotExist(err) {
		t.Fatalf("doctor executed lifecycle hook: %v", err)
	}
}

func hasCheck(report Report, name, status string) bool {
	for _, check := range report.Checks {
		if check.Name == name && check.Status == status {
			return true
		}
	}
	return false
}
